package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var contentItemStatuses = map[string]struct{}{
	"draft": {}, "in_review": {}, "scheduled": {}, "published": {}, "cancelled": {}, "archived": {},
}

type createContentItemRequest struct {
	Title             string  `json:"title"`
	Platform          string  `json:"platform"`
	Status            string  `json:"status"`
	ScheduledAt       *string `json:"scheduled_at"`
	ScheduledTimezone *string `json:"scheduled_timezone"`
	ProjectID         *string `json:"project_id"`
	Notes             *string `json:"notes"`
	ExternalLink      *string `json:"external_link"`
}

type updateContentItemRequest struct {
	Title        nullableStringPatch `json:"title"`
	Platform     nullableStringPatch `json:"platform"`
	Status       nullableStringPatch `json:"status"`
	ProjectID    nullableStringPatch `json:"project_id"`
	Notes        nullableStringPatch `json:"notes"`
	ExternalLink nullableStringPatch `json:"external_link"`
}

type scheduleContentItemRequest struct {
	ScheduledAt       nullableStringPatch `json:"scheduled_at"`
	ScheduledTimezone nullableStringPatch `json:"scheduled_timezone"`
}

type publishContentItemRequest struct {
	PublishedAt  *string             `json:"published_at"`
	ExternalLink nullableStringPatch `json:"external_link"`
}

type linkContentItemTaskRequest struct {
	TaskID     string `json:"task_id"`
	IsRequired *bool  `json:"is_required"`
}

type contentItemTaskResponse struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	IsRequired bool   `json:"is_required"`
}

type contentItemResponse struct {
	ID                 string                    `json:"id"`
	Title              string                    `json:"title"`
	Platform           string                    `json:"platform"`
	Status             string                    `json:"status"`
	ScheduledAt        *string                   `json:"scheduled_at"`
	ScheduledTimezone  *string                   `json:"scheduled_timezone"`
	PublishedAt        *string                   `json:"published_at"`
	ProjectID          *string                   `json:"project_id"`
	Notes              *string                   `json:"notes"`
	ExternalLink       *string                   `json:"external_link"`
	ManualOrder        int64                     `json:"manual_order"`
	ArchivedFromStatus *string                   `json:"archived_from_status"`
	Version            int64                     `json:"version"`
	CreatedAt          string                    `json:"created_at"`
	UpdatedAt          string                    `json:"updated_at"`
	Tasks              []contentItemTaskResponse `json:"tasks"`
	RequiredTaskTotal  int64                     `json:"required_task_total"`
	RequiredTaskDone   int64                     `json:"required_task_done"`
}

func (a *API) listContentItems(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	start, end, platform, status, projectID, includeArchived, ok := contentItemListFilters(c)
	if !ok {
		return
	}
	var total int64
	var items []models.ContentItem
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := contentItemFilteredQuery(tx, start, end, platform, status, projectID, includeArchived)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("CASE WHEN content_items.scheduled_at IS NULL THEN 1 ELSE 0 END ASC").Order("content_items.scheduled_at ASC").Order("content_items.manual_order ASC").Order("content_items.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	responses, err := loadContentItemResponses(a.db.WithContext(c.Request.Context()), items)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createContentItem(c *gin.Context) {
	var input createContentItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	item, err := contentItemFromCreateRequest(input)
	if err != nil {
		writeProjectRequestError(c, err)
		return
	}
	var response contentItemResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := requireContentItemProject(tx, item.ProjectID); err != nil {
			return err
		}
		order, err := nextContentItemOrder(tx, item.ScheduledAt)
		if err != nil {
			return err
		}
		item.ManualOrder = order
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		response, err = loadContentItemResponse(tx, item.ID)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func (a *API) getContentItem(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	response, err := loadContentItemResponse(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CONTENT_ITEM_NOT_FOUND", "Content item not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateContentItem(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	var input updateContentItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !(input.Title.Set || input.Platform.Set || input.Status.Set || input.ProjectID.Set || input.Notes.Set || input.ExternalLink.Set) {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "At least one editable field is required")
		return
	}
	var response contentItemResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		updates, err := contentItemUpdates(item, input)
		if err != nil {
			return err
		}
		if projectID, exists := updates["project_id"].(*string); exists {
			if err := requireContentItemProject(tx, projectID); err != nil {
				return err
			}
		}
		updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", id, expected).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		response, err = loadContentItemResponse(tx, id)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) scheduleContentItem(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	var input scheduleContentItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !input.ScheduledAt.Set || !input.ScheduledTimezone.Set {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "scheduled_at and scheduled_timezone must be supplied together")
		return
	}
	scheduledAt, timezone, err := validateContentSchedule(input.ScheduledAt.Value, input.ScheduledTimezone.Value)
	if err != nil {
		writeProjectRequestError(c, err)
		return
	}
	var response contentItemResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		if item.Status == "archived" || item.Status == "published" {
			return newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Archived or published content cannot be rescheduled")
		}
		status := item.Status
		if scheduledAt != nil {
			status = "scheduled"
		} else if status == "scheduled" {
			status = "draft"
		}
		result := tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", id, expected).Updates(map[string]any{"scheduled_at": scheduledAt, "scheduled_timezone": timezone, "status": status, "updated_at": time.Now().UTC().Format(time.RFC3339Nano), "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		response, err = loadContentItemResponse(tx, id)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) confirmContentItemPublished(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	var input publishContentItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	publishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if input.PublishedAt != nil {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.PublishedAt))
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "published_at must be RFC 3339 when provided")
			return
		}
		publishedAt = parsed.UTC().Format(time.RFC3339Nano)
	}
	link, err := normalizeContentOptional(input.ExternalLink.Value, 2048, "external_link")
	if err != nil {
		writeProjectRequestError(c, err)
		return
	}
	var response contentItemResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		if item.Status == "archived" || item.Status == "cancelled" {
			return newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Archived or cancelled content cannot be published")
		}
		updates := map[string]any{"status": "published", "published_at": publishedAt, "updated_at": time.Now().UTC().Format(time.RFC3339Nano), "version": gorm.Expr("version + 1")}
		if input.ExternalLink.Set {
			updates["external_link"] = link
		}
		result := tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", id, expected).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		response, err = loadContentItemResponse(tx, id)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) listContentItemTasks(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	response, err := loadContentItemResponse(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CONTENT_ITEM_NOT_FOUND", "Content item not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response.Tasks})
}

func (a *API) linkContentItemTask(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	var input linkContentItemTaskRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	taskID, err := contentItemUUID(input.TaskID, "task_id")
	if err != nil {
		writeProjectRequestError(c, err)
		return
	}
	required := true
	if input.IsRequired != nil {
		required = *input.IsRequired
	}
	var response contentItemResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		var count int64
		if err := tx.Model(&models.Task{}).Where("id = ?", taskID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return newProjectRequestError(http.StatusUnprocessableEntity, "TASK_NOT_FOUND", "task_id must reference an existing task")
		}
		link := models.ContentItemTask{ContentItemID: id, TaskID: taskID, IsRequired: required, LinkedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := tx.Where("content_item_id = ? AND task_id = ?", id, taskID).Assign(map[string]any{"is_required": required, "linked_at": link.LinkedAt}).FirstOrCreate(&link).Error; err != nil {
			return err
		}
		result := tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", id, expected).Updates(map[string]any{"updated_at": link.LinkedAt, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		response, err = loadContentItemResponse(tx, id)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func (a *API) unlinkContentItemTask(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	taskID, err := contentItemUUID(c.Param("taskId"), "task_id")
	if err != nil {
		writeProjectRequestError(c, err)
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	var response contentItemResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		result := tx.Where("content_item_id = ? AND task_id = ?", id, taskID).Delete(&models.ContentItemTask{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_TASK_NOT_FOUND", "Content item task link not found")
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		result = tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", id, expected).Updates(map[string]any{"updated_at": updatedAt, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		response, err = loadContentItemResponse(tx, id)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setContentItemETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteContentItem(c *gin.Context) {
	id, ok := contentItemID(c)
	if !ok {
		return
	}
	expected, ok := contentItemIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Permanent content item deletion requires confirm=true")
		return
	}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			return contentItemNotFoundError(err)
		}
		if item.Version != expected {
			return contentItemVersionConflict()
		}
		if item.Status != "archived" {
			return newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_NOT_ARCHIVED", "Only archived content items can be permanently deleted")
		}
		result := tx.Where("id = ? AND version = ?", id, expected).Delete(&models.ContentItem{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return contentItemVersionConflict()
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted_id": id}})
}

func contentItemListFilters(c *gin.Context) (*string, *string, string, string, string, bool, bool) {
	parseBound := func(key string) (*string, bool) {
		raw := strings.TrimSpace(c.Query(key))
		if raw == "" {
			return nil, true
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", key+" must be RFC 3339")
			return nil, false
		}
		value := parsed.UTC().Format(time.RFC3339Nano)
		return &value, true
	}
	start, ok := parseBound("scheduled_from")
	if !ok {
		return nil, nil, "", "", "", false, false
	}
	end, ok := parseBound("scheduled_to")
	if !ok {
		return nil, nil, "", "", "", false, false
	}
	if start != nil && end != nil && *start >= *end {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "scheduled_from must be before scheduled_to")
		return nil, nil, "", "", "", false, false
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if utf8.RuneCountInString(platform) > 64 {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "platform filter is too long")
		return nil, nil, "", "", "", false, false
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, exists := contentItemStatuses[status]; !exists {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return nil, nil, "", "", "", false, false
		}
	}
	projectID := ""
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "project_id filter must be a UUID")
			return nil, nil, "", "", "", false, false
		}
		projectID = parsed.String()
	}
	includeArchived, err := optionalBooleanQuery(c, "include_archived")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return nil, nil, "", "", "", false, false
	}
	return start, end, platform, status, projectID, includeArchived, true
}

func contentItemFilteredQuery(tx *gorm.DB, start, end *string, platform, status, projectID string, includeArchived bool) *gorm.DB {
	query := tx.Model(&models.ContentItem{})
	if start != nil {
		query = query.Where("content_items.scheduled_at >= ?", *start)
	}
	if end != nil {
		query = query.Where("content_items.scheduled_at < ?", *end)
	}
	if platform != "" {
		query = query.Where("content_items.platform = ?", platform)
	}
	if status != "" {
		query = query.Where("content_items.status = ?", status)
	} else if !includeArchived {
		query = query.Where("content_items.status <> 'archived'")
	}
	if projectID != "" {
		query = query.Where("content_items.project_id = ?", projectID)
	}
	return query
}

func contentItemFromCreateRequest(input createContentItemRequest) (models.ContentItem, error) {
	title, platform, notes, link, err := validateContentItemText(input.Title, input.Platform, input.Notes, input.ExternalLink)
	if err != nil {
		return models.ContentItem{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}
	if _, exists := contentItemStatuses[status]; !exists || status == "archived" || status == "published" {
		return models.ContentItem{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status is invalid for content item creation")
	}
	scheduledAt, timezone, err := validateContentSchedule(input.ScheduledAt, input.ScheduledTimezone)
	if err != nil {
		return models.ContentItem{}, err
	}
	if status == "scheduled" && scheduledAt == nil {
		return models.ContentItem{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "scheduled status requires scheduled_at and scheduled_timezone")
	}
	projectID, err := normalizeContentProjectID(input.ProjectID)
	if err != nil {
		return models.ContentItem{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.ContentItem{ID: uuid.NewString(), Title: title, Platform: platform, Status: status, ScheduledAt: scheduledAt, ScheduledTimezone: timezone, ProjectID: projectID, Notes: notes, ExternalLink: link, Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func contentItemUpdates(current models.ContentItem, input updateContentItemRequest) (map[string]any, error) {
	title, platform, notes, link, projectID, status := current.Title, current.Platform, current.Notes, current.ExternalLink, current.ProjectID, current.Status
	if input.Title.Set {
		if input.Title.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title cannot be null")
		}
		title = *input.Title.Value
	}
	if input.Platform.Set {
		if input.Platform.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "platform cannot be null")
		}
		platform = *input.Platform.Value
	}
	if input.Notes.Set {
		notes = input.Notes.Value
	}
	if input.ExternalLink.Set {
		link = input.ExternalLink.Value
	}
	if input.ProjectID.Set {
		var err error
		projectID, err = normalizeContentProjectID(input.ProjectID.Value)
		if err != nil {
			return nil, err
		}
	}
	if input.Status.Set {
		if input.Status.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status cannot be null")
		}
		status = strings.TrimSpace(*input.Status.Value)
	}
	title, platform, notes, link, err := validateContentItemText(title, platform, notes, link)
	if err != nil {
		return nil, err
	}
	if _, exists := contentItemStatuses[status]; !exists {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status is invalid")
	}
	updates := map[string]any{"title": title, "platform": platform, "project_id": projectID, "notes": notes, "external_link": link}
	if input.Status.Set {
		if status == "published" {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Use the publish-confirmation endpoint to mark content as published")
		}
		if status == "archived" {
			if current.Status == "published" {
				return nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Published content cannot be archived until published-history archival is implemented")
			}
			if current.Status == "archived" {
				return nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Content item is already archived")
			}
			updates["status"] = "archived"
			updates["archived_from_status"] = current.Status
		} else if current.Status == "archived" {
			updates["status"] = status
			updates["archived_from_status"] = nil
		} else {
			updates["status"] = status
		}
	}
	return updates, nil
}

func validateContentItemText(title, platform string, notes, link *string) (string, string, *string, *string, error) {
	title = strings.TrimSpace(title)
	if length := utf8.RuneCountInString(title); length < 1 || length > 200 {
		return "", "", nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title must contain between 1 and 200 characters")
	}
	platform = strings.TrimSpace(platform)
	if length := utf8.RuneCountInString(platform); length < 1 || length > 64 {
		return "", "", nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "platform must contain between 1 and 64 characters")
	}
	var err error
	notes, err = normalizeContentOptional(notes, 4000, "notes")
	if err != nil {
		return "", "", nil, nil, err
	}
	link, err = normalizeContentOptional(link, 2048, "external_link")
	if err != nil {
		return "", "", nil, nil, err
	}
	return title, platform, notes, link, nil
}

func normalizeContentOptional(value *string, maximum int, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if length := utf8.RuneCountInString(normalized); length < 1 || length > maximum {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", field+" must contain between 1 and "+strconv.Itoa(maximum)+" characters when provided")
	}
	return &normalized, nil
}
func normalizeContentProjectID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*value))
	if err != nil {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_id must be a UUID when provided")
	}
	normalized := parsed.String()
	return &normalized, nil
}
func validateContentSchedule(scheduledAt, timezone *string) (*string, *string, error) {
	if scheduledAt == nil && timezone == nil {
		return nil, nil, nil
	}
	if scheduledAt == nil || timezone == nil {
		return nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "scheduled_at and scheduled_timezone must be supplied together")
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*scheduledAt))
	if err != nil {
		return nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "scheduled_at must be RFC 3339")
	}
	zone := strings.TrimSpace(*timezone)
	if _, err := time.LoadLocation(zone); err != nil {
		return nil, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "scheduled_timezone must be an IANA timezone")
	}
	at := parsed.UTC().Format(time.RFC3339Nano)
	return &at, &zone, nil
}
func requireContentItemProject(tx *gorm.DB, projectID *string) error {
	if projectID == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&models.Project{}).Where("id = ?", *projectID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return newProjectRequestError(http.StatusUnprocessableEntity, "PROJECT_NOT_FOUND", "project_id must reference an existing project")
	}
	return nil
}
func nextContentItemOrder(tx *gorm.DB, scheduledAt *string) (int64, error) {
	var maximum int64
	query := tx.Model(&models.ContentItem{}).Where("status <> 'archived'")
	if scheduledAt == nil {
		query = query.Where("scheduled_at IS NULL")
	} else {
		query = query.Where("scheduled_at = ?", *scheduledAt)
	}
	if err := query.Select("COALESCE(MAX(manual_order), 0)").Scan(&maximum).Error; err != nil {
		return 0, err
	}
	return maximum + 1024, nil
}
func loadContentItemResponses(tx *gorm.DB, items []models.ContentItem) ([]contentItemResponse, error) {
	responses := make([]contentItemResponse, len(items))
	for index, item := range items {
		response, err := loadContentItemResponse(tx, item.ID)
		if err != nil {
			return nil, err
		}
		responses[index] = response
	}
	return responses, nil
}
func loadContentItemResponse(tx *gorm.DB, id string) (contentItemResponse, error) {
	var item models.ContentItem
	if err := tx.First(&item, "id = ?", id).Error; err != nil {
		return contentItemResponse{}, err
	}
	var tasks []contentItemTaskResponse
	if err := tx.Table("content_item_tasks").Select("tasks.id, tasks.title, tasks.status, content_item_tasks.is_required").Joins("JOIN tasks ON tasks.id = content_item_tasks.task_id").Where("content_item_tasks.content_item_id = ?", id).Order("content_item_tasks.linked_at ASC").Scan(&tasks).Error; err != nil {
		return contentItemResponse{}, err
	}
	var summary struct {
		Total int64
		Done  int64
	}
	if err := tx.Table("content_item_tasks").Select("COUNT(*) AS total, SUM(CASE WHEN tasks.status = 'done' THEN 1 ELSE 0 END) AS done").Joins("JOIN tasks ON tasks.id = content_item_tasks.task_id").Where("content_item_tasks.content_item_id = ? AND content_item_tasks.is_required = 1", id).Scan(&summary).Error; err != nil {
		return contentItemResponse{}, err
	}
	return contentItemResponse{ID: item.ID, Title: item.Title, Platform: item.Platform, Status: item.Status, ScheduledAt: item.ScheduledAt, ScheduledTimezone: item.ScheduledTimezone, PublishedAt: item.PublishedAt, ProjectID: item.ProjectID, Notes: item.Notes, ExternalLink: item.ExternalLink, ManualOrder: item.ManualOrder, ArchivedFromStatus: item.ArchivedFromStatus, Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Tasks: tasks, RequiredTaskTotal: summary.Total, RequiredTaskDone: summary.Done}, nil
}
func contentItemID(c *gin.Context) (string, bool) {
	id, err := contentItemUUID(c.Param("id"), "content item id")
	if err != nil {
		writeProjectRequestError(c, err)
		return "", false
	}
	return id, true
}
func contentItemUUID(raw, field string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", newProjectRequestError(http.StatusBadRequest, "INVALID_CONTENT_ITEM_ID", field+" must be a UUID")
	}
	return parsed.String(), nil
}
func contentItemIfMatch(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	if raw == "" {
		writeError(c, http.StatusPreconditionRequired, "VERSION_REQUIRED", "If-Match is required")
		return 0, false
	}
	raw = strings.Trim(raw, "\"")
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version < 1 {
		writeError(c, http.StatusBadRequest, "INVALID_VERSION", "If-Match must contain one positive integer version")
		return 0, false
	}
	return version, true
}
func setContentItemETag(c *gin.Context, version int64) {
	c.Header("ETag", strconv.Quote(strconv.FormatInt(version, 10)))
}
func contentItemVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Content item has changed; refresh and retry")
}
func contentItemNotFoundError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_NOT_FOUND", "Content item not found")
	}
	return err
}
