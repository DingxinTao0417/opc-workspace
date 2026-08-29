package api

import (
	"database/sql"
	"encoding/json"
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

const (
	roadmapMilestoneDateLayout = "2006-01-02"
	roadmapMilestoneOrderStep  = 1024
)

var roadmapMilestoneStatuses = map[string]struct{}{
	"planned": {}, "active": {}, "achieved": {}, "archived": {},
}

type createRoadmapMilestoneRequest struct {
	Title       string   `json:"title"`
	Description *string  `json:"description"`
	Year        int      `json:"year"`
	Quarter     int      `json:"quarter"`
	TargetDate  string   `json:"target_date"`
	Status      string   `json:"status"`
	ProjectIDs  []string `json:"project_ids"`
}

type nullableStringSlicePatch struct {
	Set   bool
	Value []string
}

func (field *nullableStringSlicePatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	if string(data) == "null" {
		field.Value = nil
		return nil
	}
	return json.Unmarshal(data, &field.Value)
}

type updateRoadmapMilestoneRequest struct {
	Title       nullableStringPatch      `json:"title"`
	Description nullableStringPatch      `json:"description"`
	Year        nullableInt64Patch       `json:"year"`
	Quarter     nullableInt64Patch       `json:"quarter"`
	TargetDate  nullableStringPatch      `json:"target_date"`
	Status      nullableStringPatch      `json:"status"`
	ProjectIDs  nullableStringSlicePatch `json:"project_ids"`
}

type reorderRoadmapMilestonesRequest struct {
	Items []reorderRoadmapMilestoneItem `json:"items"`
}

type reorderRoadmapMilestoneItem struct {
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type roadmapProjectResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type roadmapTaskSummary struct {
	Total           int64 `json:"total"`
	Completed       int64 `json:"completed"`
	InProgress      int64 `json:"in_progress"`
	ProgressPercent int   `json:"progress_percent"`
}

type roadmapMilestoneResponse struct {
	ID                 string                   `json:"id"`
	Title              string                   `json:"title"`
	Description        *string                  `json:"description"`
	Year               int                      `json:"year"`
	Quarter            int                      `json:"quarter"`
	TargetDate         string                   `json:"target_date"`
	Status             string                   `json:"status"`
	ManualOrder        int64                    `json:"manual_order"`
	ArchivedFromStatus *string                  `json:"archived_from_status"`
	Version            int64                    `json:"version"`
	CreatedAt          string                   `json:"created_at"`
	UpdatedAt          string                   `json:"updated_at"`
	Projects           []roadmapProjectResponse `json:"projects"`
	TaskSummary        roadmapTaskSummary       `json:"task_summary"`
}

func (a *API) listRoadmapMilestones(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	year, quarter, status, projectID, includeArchived, ok := roadmapMilestoneListFilters(c)
	if !ok {
		return
	}

	var total int64
	var items []models.RoadmapMilestone
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := roadmapMilestoneFilteredQuery(tx, year, quarter, status, projectID, includeArchived)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("roadmap_milestones.year ASC").Order("roadmap_milestones.quarter ASC").
			Order("roadmap_milestones.manual_order ASC").Order("roadmap_milestones.target_date ASC").
			Order("roadmap_milestones.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	responses, err := loadRoadmapMilestoneResponses(a.db.WithContext(c.Request.Context()), items)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createRoadmapMilestone(c *gin.Context) {
	var input createRoadmapMilestoneRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	milestone, projectIDs, err := roadmapMilestoneFromCreateRequest(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	var response roadmapMilestoneResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := requireRoadmapProjects(tx, projectIDs); err != nil {
			return err
		}
		order, err := nextRoadmapMilestoneOrder(tx, milestone.Year, milestone.Quarter)
		if err != nil {
			return err
		}
		milestone.ManualOrder = order
		if err := tx.Create(&milestone).Error; err != nil {
			return err
		}
		if err := replaceRoadmapMilestoneProjects(tx, milestone.ID, projectIDs, milestone.CreatedAt); err != nil {
			return err
		}
		response, err = loadRoadmapMilestoneResponse(tx, milestone.ID)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setRoadmapMilestoneETag(c, response.Version)
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func (a *API) getRoadmapMilestone(c *gin.Context) {
	id, ok := roadmapMilestoneID(c)
	if !ok {
		return
	}
	response, err := loadRoadmapMilestoneResponse(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	setRoadmapMilestoneETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateRoadmapMilestone(c *gin.Context) {
	id, ok := roadmapMilestoneID(c)
	if !ok {
		return
	}
	expectedVersion, ok := roadmapMilestoneIfMatch(c)
	if !ok {
		return
	}
	var input updateRoadmapMilestoneRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !roadmapMilestoneUpdateHasFields(input) {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "At least one editable field is required")
		return
	}
	var response roadmapMilestoneResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var milestone models.RoadmapMilestone
		if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
			}
			return err
		}
		if milestone.Version != expectedVersion {
			return roadmapMilestoneVersionConflict()
		}
		if milestone.Status == "archived" {
			return newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_ARCHIVED", "Restore the roadmap milestone before editing it")
		}
		updates, projectIDs, replaceProjects, err := roadmapMilestoneUpdates(milestone, input)
		if err != nil {
			return err
		}
		if input.Status.Set && input.Status.Value != nil && *input.Status.Value == "archived" {
			return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Use the archive endpoint to archive a roadmap milestone")
		}
		if replaceProjects {
			if err := requireRoadmapProjects(tx, projectIDs); err != nil {
				return err
			}
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		updates["updated_at"] = updatedAt
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.RoadmapMilestone{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return roadmapMilestoneVersionConflict()
		}
		if replaceProjects {
			if err := replaceRoadmapMilestoneProjects(tx, id, projectIDs, updatedAt); err != nil {
				return err
			}
		}
		loaded, err := loadRoadmapMilestoneResponse(tx, id)
		if err != nil {
			return err
		}
		response = loaded
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setRoadmapMilestoneETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) archiveRoadmapMilestone(c *gin.Context) { a.transitionRoadmapMilestoneArchive(c, true) }
func (a *API) restoreRoadmapMilestone(c *gin.Context) { a.transitionRoadmapMilestoneArchive(c, false) }

func (a *API) transitionRoadmapMilestoneArchive(c *gin.Context, archive bool) {
	id, ok := roadmapMilestoneID(c)
	if !ok {
		return
	}
	expectedVersion, ok := roadmapMilestoneIfMatch(c)
	if !ok {
		return
	}
	var response roadmapMilestoneResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var milestone models.RoadmapMilestone
		if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
			}
			return err
		}
		if milestone.Version != expectedVersion {
			return roadmapMilestoneVersionConflict()
		}
		updates := map[string]any{"updated_at": time.Now().UTC().Format(time.RFC3339Nano), "version": gorm.Expr("version + 1")}
		if archive {
			if milestone.Status == "archived" {
				return newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is already archived")
			}
			updates["status"] = "archived"
			updates["archived_from_status"] = milestone.Status
		} else {
			if milestone.Status != "archived" || milestone.ArchivedFromStatus == nil {
				return newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_STATE_INVALID", "Roadmap milestone is not archived")
			}
			updates["status"] = *milestone.ArchivedFromStatus
			updates["archived_from_status"] = nil
		}
		result := tx.Model(&models.RoadmapMilestone{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return roadmapMilestoneVersionConflict()
		}
		loaded, err := loadRoadmapMilestoneResponse(tx, id)
		if err != nil {
			return err
		}
		response = loaded
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setRoadmapMilestoneETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteRoadmapMilestone(c *gin.Context) {
	id, ok := roadmapMilestoneID(c)
	if !ok {
		return
	}
	expectedVersion, ok := roadmapMilestoneIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Permanent roadmap milestone deletion requires confirm=true")
		return
	}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var milestone models.RoadmapMilestone
		if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
			}
			return err
		}
		if milestone.Version != expectedVersion {
			return roadmapMilestoneVersionConflict()
		}
		if milestone.Status != "archived" {
			return newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_NOT_ARCHIVED", "Only archived roadmap milestones can be permanently deleted")
		}
		result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.RoadmapMilestone{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return roadmapMilestoneVersionConflict()
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

func (a *API) reorderRoadmapMilestones(c *gin.Context) {
	var input reorderRoadmapMilestonesRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if len(input.Items) == 0 || len(input.Items) > 100 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "items must contain between 1 and 100 roadmap milestones")
		return
	}
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		parsed, err := uuid.Parse(strings.TrimSpace(item.ID))
		if err != nil || item.ExpectedVersion < 1 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "every item requires a UUID id and positive expected_version")
			return
		}
		if _, exists := seen[parsed.String()]; exists {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "items cannot repeat a roadmap milestone")
			return
		}
		input.Items[len(seen)].ID = parsed.String()
		seen[parsed.String()] = struct{}{}
	}
	var response []roadmapMilestoneResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var periodYear, periodQuarter int
		for index, item := range input.Items {
			var milestone models.RoadmapMilestone
			if err := tx.First(&milestone, "id = ?", item.ID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
				}
				return err
			}
			if milestone.Version != item.ExpectedVersion {
				return roadmapMilestoneVersionConflict()
			}
			if milestone.Status == "archived" {
				return newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_ARCHIVED", "Archived roadmap milestones cannot be reordered")
			}
			if index == 0 {
				periodYear, periodQuarter = milestone.Year, milestone.Quarter
			} else if milestone.Year != periodYear || milestone.Quarter != periodQuarter {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "all reordered milestones must be in the same year and quarter")
			}
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		for index, item := range input.Items {
			result := tx.Model(&models.RoadmapMilestone{}).Where("id = ? AND version = ?", item.ID, item.ExpectedVersion).Updates(map[string]any{
				"manual_order": int64(index+1) * roadmapMilestoneOrderStep,
				"updated_at":   updatedAt,
				"version":      gorm.Expr("version + 1"),
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return roadmapMilestoneVersionConflict()
			}
		}
		var milestones []models.RoadmapMilestone
		for _, item := range input.Items {
			var milestone models.RoadmapMilestone
			if err := tx.First(&milestone, "id = ?", item.ID).Error; err != nil {
				return err
			}
			milestones = append(milestones, milestone)
		}
		loaded, err := loadRoadmapMilestoneResponses(tx, milestones)
		if err != nil {
			return err
		}
		response = loaded
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func roadmapMilestoneListFilters(c *gin.Context) (int, int, string, string, bool, bool) {
	year := 0
	if raw := strings.TrimSpace(c.Query("year")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 2000 || value > 2100 {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "year filter must be between 2000 and 2100")
			return 0, 0, "", "", false, false
		}
		year = value
	}
	quarter := 0
	if raw := strings.TrimSpace(c.Query("quarter")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 4 {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "quarter filter must be between 1 and 4")
			return 0, 0, "", "", false, false
		}
		quarter = value
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, exists := roadmapMilestoneStatuses[status]; !exists {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return 0, 0, "", "", false, false
		}
	}
	projectID := ""
	if raw := strings.TrimSpace(c.Query("project_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "project_id filter must be a UUID")
			return 0, 0, "", "", false, false
		}
		projectID = parsed.String()
	}
	includeArchived, err := optionalBooleanQuery(c, "include_archived")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return 0, 0, "", "", false, false
	}
	return year, quarter, status, projectID, includeArchived, true
}

func roadmapMilestoneFilteredQuery(tx *gorm.DB, year, quarter int, status, projectID string, includeArchived bool) *gorm.DB {
	query := tx.Model(&models.RoadmapMilestone{})
	if year != 0 {
		query = query.Where("roadmap_milestones.year = ?", year)
	}
	if quarter != 0 {
		query = query.Where("roadmap_milestones.quarter = ?", quarter)
	}
	if status != "" {
		query = query.Where("roadmap_milestones.status = ?", status)
	} else if !includeArchived {
		query = query.Where("roadmap_milestones.status <> 'archived'")
	}
	if projectID != "" {
		query = query.Joins("JOIN roadmap_milestone_projects ON roadmap_milestone_projects.milestone_id = roadmap_milestones.id").Where("roadmap_milestone_projects.project_id = ?", projectID)
	}
	return query
}

func roadmapMilestoneFromCreateRequest(input createRoadmapMilestoneRequest) (models.RoadmapMilestone, []string, error) {
	title, description, year, quarter, targetDate, status, err := validateRoadmapMilestoneFields(input.Title, input.Description, input.Year, input.Quarter, input.TargetDate, input.Status)
	if err != nil {
		return models.RoadmapMilestone{}, nil, err
	}
	if status == "archived" {
		return models.RoadmapMilestone{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Use the archive endpoint after creating a roadmap milestone")
	}
	projectIDs, err := canonicalRoadmapProjectIDs(input.ProjectIDs)
	if err != nil {
		return models.RoadmapMilestone{}, nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.RoadmapMilestone{ID: uuid.NewString(), Title: title, Description: description, Year: year, Quarter: quarter, TargetDate: targetDate, Status: status, Version: 1, CreatedAt: now, UpdatedAt: now}, projectIDs, nil
}

func roadmapMilestoneUpdates(current models.RoadmapMilestone, input updateRoadmapMilestoneRequest) (map[string]any, []string, bool, error) {
	title, description := current.Title, current.Description
	year, quarter, targetDate, status := current.Year, current.Quarter, current.TargetDate, current.Status
	if input.Title.Set {
		if input.Title.Value == nil {
			return nil, nil, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title cannot be null")
		}
		title = *input.Title.Value
	}
	if input.Description.Set {
		description = input.Description.Value
	}
	if input.Year.Set {
		if input.Year.Value == nil {
			return nil, nil, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "year cannot be null")
		}
		year = int(*input.Year.Value)
	}
	if input.Quarter.Set {
		if input.Quarter.Value == nil {
			return nil, nil, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "quarter cannot be null")
		}
		quarter = int(*input.Quarter.Value)
	}
	if input.TargetDate.Set {
		if input.TargetDate.Value == nil {
			return nil, nil, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "target_date cannot be null")
		}
		targetDate = *input.TargetDate.Value
	}
	if input.Status.Set {
		if input.Status.Value == nil {
			return nil, nil, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status cannot be null")
		}
		status = *input.Status.Value
	}
	title, description, year, quarter, targetDate, status, err := validateRoadmapMilestoneFields(title, description, year, quarter, targetDate, status)
	if err != nil {
		return nil, nil, false, err
	}
	updates := map[string]any{"title": title, "description": description, "year": year, "quarter": quarter, "target_date": targetDate, "status": status}
	projectIDs, replaceProjects := []string(nil), input.ProjectIDs.Set
	if replaceProjects {
		var err error
		projectIDs, err = canonicalRoadmapProjectIDs(input.ProjectIDs.Value)
		if err != nil {
			return nil, nil, false, err
		}
	}
	return updates, projectIDs, replaceProjects, nil
}

func validateRoadmapMilestoneFields(title string, description *string, year, quarter int, targetDate, status string) (string, *string, int, int, string, string, error) {
	title = strings.TrimSpace(title)
	if length := utf8.RuneCountInString(title); length < 1 || length > 200 {
		return "", nil, 0, 0, "", "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title must contain between 1 and 200 characters")
	}
	if description != nil {
		value := strings.TrimSpace(*description)
		if length := utf8.RuneCountInString(value); length < 1 || length > 4000 {
			return "", nil, 0, 0, "", "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "description must contain between 1 and 4000 characters when provided")
		}
		description = &value
	}
	if year < 2000 || year > 2100 || quarter < 1 || quarter > 4 {
		return "", nil, 0, 0, "", "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "year and quarter are invalid")
	}
	targetDate = strings.TrimSpace(targetDate)
	parsedDate, err := time.Parse(roadmapMilestoneDateLayout, targetDate)
	if err != nil || parsedDate.Format(roadmapMilestoneDateLayout) != targetDate || parsedDate.Year() != year || int((parsedDate.Month()-1)/3)+1 != quarter {
		return "", nil, 0, 0, "", "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "target_date must be a valid date in the selected year and quarter")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "planned"
	}
	if _, valid := roadmapMilestoneStatuses[status]; !valid {
		return "", nil, 0, 0, "", "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status is invalid")
	}
	return title, description, year, quarter, targetDate, status, nil
}

func canonicalRoadmapProjectIDs(input []string) ([]string, error) {
	if len(input) > 100 {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_ids cannot contain more than 100 projects")
	}
	output := make([]string, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_ids must contain only UUIDs")
		}
		id := parsed.String()
		if _, exists := seen[id]; exists {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_ids cannot contain duplicates")
		}
		seen[id] = struct{}{}
		output = append(output, id)
	}
	return output, nil
}

func requireRoadmapProjects(tx *gorm.DB, projectIDs []string) error {
	if len(projectIDs) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&models.Project{}).Where("id IN ?", projectIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(projectIDs)) {
		return newProjectRequestError(http.StatusUnprocessableEntity, "PROJECT_NOT_FOUND", "project_ids must reference existing projects")
	}
	return nil
}

func nextRoadmapMilestoneOrder(tx *gorm.DB, year, quarter int) (int64, error) {
	var maximum int64
	if err := tx.Model(&models.RoadmapMilestone{}).Where("year = ? AND quarter = ? AND status <> 'archived'", year, quarter).Select("COALESCE(MAX(manual_order), 0)").Scan(&maximum).Error; err != nil {
		return 0, err
	}
	return maximum + roadmapMilestoneOrderStep, nil
}

func replaceRoadmapMilestoneProjects(tx *gorm.DB, milestoneID string, projectIDs []string, linkedAt string) error {
	if err := tx.Where("milestone_id = ?", milestoneID).Delete(&models.RoadmapMilestoneProject{}).Error; err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		link := models.RoadmapMilestoneProject{MilestoneID: milestoneID, ProjectID: projectID, LinkedAt: linkedAt}
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
	}
	return nil
}

func loadRoadmapMilestoneResponses(tx *gorm.DB, milestones []models.RoadmapMilestone) ([]roadmapMilestoneResponse, error) {
	responses := make([]roadmapMilestoneResponse, len(milestones))
	for index, milestone := range milestones {
		response, err := loadRoadmapMilestoneResponse(tx, milestone.ID)
		if err != nil {
			return nil, err
		}
		responses[index] = response
	}
	return responses, nil
}

func loadRoadmapMilestoneResponse(tx *gorm.DB, id string) (roadmapMilestoneResponse, error) {
	var milestone models.RoadmapMilestone
	if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
		return roadmapMilestoneResponse{}, err
	}
	var projects []roadmapProjectResponse
	if err := tx.Table("roadmap_milestone_projects").Select("projects.id, projects.name, projects.status").
		Joins("JOIN projects ON projects.id = roadmap_milestone_projects.project_id").Where("roadmap_milestone_projects.milestone_id = ?", id).
		Order("projects.name ASC").Order("projects.id ASC").Scan(&projects).Error; err != nil {
		return roadmapMilestoneResponse{}, err
	}
	projectIDs := make([]string, len(projects))
	for index, project := range projects {
		projectIDs[index] = project.ID
	}
	summary := roadmapTaskSummary{}
	if len(projectIDs) > 0 {
		if err := tx.Table("tasks").Select(`
			COUNT(*) AS total,
			SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN status = 'in_progress' THEN 1 ELSE 0 END) AS in_progress
		`).Where("project_id IN ?", projectIDs).Scan(&summary).Error; err != nil {
			return roadmapMilestoneResponse{}, err
		}
	}
	if summary.Total > 0 {
		summary.ProgressPercent = int(summary.Completed * 100 / summary.Total)
	}
	return roadmapMilestoneResponse{ID: milestone.ID, Title: milestone.Title, Description: milestone.Description, Year: milestone.Year, Quarter: milestone.Quarter, TargetDate: milestone.TargetDate, Status: milestone.Status, ManualOrder: milestone.ManualOrder, ArchivedFromStatus: milestone.ArchivedFromStatus, Version: milestone.Version, CreatedAt: milestone.CreatedAt, UpdatedAt: milestone.UpdatedAt, Projects: projects, TaskSummary: summary}, nil
}

func roadmapMilestoneUpdateHasFields(input updateRoadmapMilestoneRequest) bool {
	return input.Title.Set || input.Description.Set || input.Year.Set || input.Quarter.Set || input.TargetDate.Set || input.Status.Set || input.ProjectIDs.Set
}

func roadmapMilestoneID(c *gin.Context) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ROADMAP_MILESTONE_ID", "Roadmap milestone id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func roadmapMilestoneIfMatch(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	if raw == "" {
		writeError(c, http.StatusPreconditionRequired, "VERSION_REQUIRED", "If-Match is required")
		return 0, false
	}
	if strings.HasPrefix(raw, "\"") || strings.HasSuffix(raw, "\"") {
		if len(raw) < 3 || !strings.HasPrefix(raw, "\"") || !strings.HasSuffix(raw, "\"") {
			writeError(c, http.StatusBadRequest, "INVALID_VERSION", "If-Match must contain one positive integer version")
			return 0, false
		}
		raw = raw[1 : len(raw)-1]
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version < 1 {
		writeError(c, http.StatusBadRequest, "INVALID_VERSION", "If-Match must contain one positive integer version")
		return 0, false
	}
	return version, true
}

func setRoadmapMilestoneETag(c *gin.Context, version int64) {
	c.Header("ETag", strconv.Quote(strconv.FormatInt(version, 10)))
}

func roadmapMilestoneVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Roadmap milestone has changed; refresh and retry")
}
