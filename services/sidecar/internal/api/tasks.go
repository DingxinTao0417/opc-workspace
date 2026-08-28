package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createTaskEndpoint = "POST /api/v1/tasks"

var (
	errLifecycleCommandRequired = errors.New("tasks must be created in todo status; use a lifecycle command after creation")
	validTaskStatuses           = map[string]struct{}{
		"todo": {}, "in_progress": {}, "blocked": {}, "waiting_review": {}, "done": {}, "cancelled": {},
	}
	validPriorities = map[string]struct{}{"P0": {}, "P1": {}, "P2": {}, "P3": {}}
	validTaskKinds  = map[string]struct{}{"work": {}, "review": {}, "followup": {}, "reminder": {}}
)

type createTaskRequest struct {
	Title              string   `json:"title"`
	Description        *string  `json:"description"`
	Kind               *string  `json:"kind"`
	Status             *string  `json:"status"`
	Priority           *string  `json:"priority"`
	ProjectID          *string  `json:"project_id"`
	ParentTaskID       *string  `json:"parent_task_id"`
	CompletionCriteria *string  `json:"completion_criteria"`
	TagIDs             []string `json:"tag_ids"`
	DueDate            *string  `json:"due_date"`
	PlannedDate        *string  `json:"planned_date"`
	EstimatedMinutes   *int     `json:"estimated_minutes"`
	ManualOrder        *int     `json:"manual_order"`
	ReviewPolicy       *string  `json:"review_policy"`
}

type nullableStringPatch struct {
	Set   bool
	Value *string
}

func (field *nullableStringPatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	if string(data) == "null" {
		field.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	field.Value = &value
	return nil
}

type nullableIntPatch struct {
	Set   bool
	Value *int
}

type stringSlicePatch struct {
	Set   bool
	Value []string
}

func (field *stringSlicePatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	if string(data) == "null" {
		field.Value = []string{}
		return nil
	}
	return json.Unmarshal(data, &field.Value)
}

func (field *nullableIntPatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	if string(data) == "null" {
		field.Value = nil
		return nil
	}
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	field.Value = &value
	return nil
}

type updateTaskRequest struct {
	Title              *string             `json:"title"`
	Description        *string             `json:"description"`
	Kind               *string             `json:"kind"`
	Priority           *string             `json:"priority"`
	ProjectID          nullableStringPatch `json:"project_id"`
	ParentTaskID       nullableStringPatch `json:"parent_task_id"`
	CompletionCriteria *string             `json:"completion_criteria"`
	TagIDs             stringSlicePatch    `json:"tag_ids"`
	DueDate            nullableStringPatch `json:"due_date"`
	PlannedDate        nullableStringPatch `json:"planned_date"`
	EstimatedMinutes   nullableIntPatch    `json:"estimated_minutes"`
	ReviewPolicy       *string             `json:"review_policy"`
}

type pageMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type taskListFilters struct {
	Status       string
	Priority     string
	Kind         string
	ProjectID    string
	PlannedDate  string
	PlannedFrom  string
	PlannedTo    string
	DueFrom      string
	DueTo        string
	TagIDs       []string
	ParentTaskID string
	RootOnly     bool
	Search       string
	Sort         string
}

func (a *API) listTasks(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}

	filters, ok := taskFiltersFromRequest(c)
	if !ok {
		return
	}

	var total int64
	var tasks []models.Task
	invalidSort := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := applyTaskFilters(tx.Model(&models.Task{}), filters)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyTaskSort(query, filters.Sort)
		if !valid {
			invalidSort = true
			return errors.New("invalid task sort")
		}
		if err := withTaskProject(ordered).Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error; err != nil {
			return err
		}
		return hydrateTaskTags(tx, tasks)
	}, &sql.TxOptions{ReadOnly: true})
	if invalidSort {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	normalizeTasks(tasks)
	c.JSON(http.StatusOK, gin.H{"data": tasks, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func taskFiltersFromRequest(c *gin.Context) (taskListFilters, bool) {
	filters := taskListFilters{
		Status:      strings.TrimSpace(c.Query("status")),
		Priority:    strings.TrimSpace(c.Query("priority")),
		Kind:        strings.TrimSpace(c.Query("kind")),
		ProjectID:   strings.TrimSpace(c.Query("project_id")),
		PlannedDate: strings.TrimSpace(c.Query("planned_date")),
		PlannedFrom: strings.TrimSpace(c.Query("planned_from")),
		PlannedTo:   strings.TrimSpace(c.Query("planned_to")),
		DueFrom:     strings.TrimSpace(c.Query("due_from")),
		DueTo:       strings.TrimSpace(c.Query("due_to")),
		Search:      strings.TrimSpace(c.Query("q")),
		Sort:        strings.TrimSpace(c.Query("sort")),
	}
	if filters.Status != "" {
		if _, valid := validTaskStatuses[filters.Status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return taskListFilters{}, false
		}
	}
	if filters.Priority != "" {
		if _, valid := validPriorities[filters.Priority]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "priority filter is invalid")
			return taskListFilters{}, false
		}
	}
	if filters.Kind != "" {
		if _, valid := validTaskKinds[filters.Kind]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "kind filter is invalid")
			return taskListFilters{}, false
		}
	}
	if filters.ProjectID != "" {
		if _, err := uuid.Parse(filters.ProjectID); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "project_id filter must be a UUID")
			return taskListFilters{}, false
		}
	}
	if filters.PlannedDate != "" && !validDate(filters.PlannedDate) {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "planned_date filter must use YYYY-MM-DD")
		return taskListFilters{}, false
	}
	for _, filter := range []struct {
		name  string
		value string
	}{
		{name: "planned_from", value: filters.PlannedFrom},
		{name: "planned_to", value: filters.PlannedTo},
		{name: "due_from", value: filters.DueFrom},
		{name: "due_to", value: filters.DueTo},
	} {
		if filter.value != "" {
			if !validDate(filter.value) {
				writeError(c, http.StatusBadRequest, "INVALID_FILTER", filter.name+" must use YYYY-MM-DD")
				return taskListFilters{}, false
			}
		}
	}
	seenTags := make(map[string]struct{})
	for _, rawTagID := range c.QueryArray("tag_id") {
		tagID := strings.TrimSpace(rawTagID)
		if tagID == "" {
			continue
		}
		if _, err := uuid.Parse(tagID); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "tag_id filter must be a UUID")
			return taskListFilters{}, false
		}
		if _, exists := seenTags[tagID]; !exists {
			filters.TagIDs = append(filters.TagIDs, tagID)
			seenTags[tagID] = struct{}{}
		}
	}
	filters.ParentTaskID = strings.TrimSpace(c.Query("parent_task_id"))
	if filters.ParentTaskID != "" {
		if _, err := uuid.Parse(filters.ParentTaskID); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "parent_task_id filter must be a UUID")
			return taskListFilters{}, false
		}
	}
	if rootOnly := strings.TrimSpace(c.Query("root_only")); rootOnly != "" {
		if rootOnly != "true" && rootOnly != "false" {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "root_only must be true or false")
			return taskListFilters{}, false
		}
		filters.RootOnly = rootOnly == "true"
	}
	return filters, true
}

func applyTaskFilters(query *gorm.DB, filters taskListFilters) *gorm.DB {
	if filters.Status != "" {
		query = query.Where("tasks.status = ?", filters.Status)
	}
	if filters.Priority != "" {
		query = query.Where("tasks.priority = ?", filters.Priority)
	}
	if filters.Kind != "" {
		query = query.Where("tasks.kind = ?", filters.Kind)
	}
	if filters.ProjectID != "" {
		query = query.Where("tasks.project_id = ?", filters.ProjectID)
	}
	if filters.PlannedDate != "" {
		query = query.Where("tasks.planned_date = ?", filters.PlannedDate)
	}
	for _, filter := range []struct {
		column   string
		operator string
		value    string
	}{
		{column: "tasks.planned_date", operator: ">=", value: filters.PlannedFrom},
		{column: "tasks.planned_date", operator: "<=", value: filters.PlannedTo},
		{column: "substr(tasks.due_date, 1, 10)", operator: ">=", value: filters.DueFrom},
		{column: "substr(tasks.due_date, 1, 10)", operator: "<=", value: filters.DueTo},
	} {
		if filter.value != "" {
			query = query.Where(filter.column+" "+filter.operator+" ?", filter.value)
		}
	}
	for _, tagID := range filters.TagIDs {
		query = query.Where("EXISTS (SELECT 1 FROM task_tags WHERE task_tags.task_id = tasks.id AND task_tags.tag_id = ?)", tagID)
	}
	if filters.ParentTaskID != "" {
		query = query.Where("tasks.parent_task_id = ?", filters.ParentTaskID)
	}
	if filters.RootOnly {
		query = query.Where("tasks.parent_task_id IS NULL")
	}
	if filters.Search != "" {
		like := "%" + escapeLike(filters.Search) + "%"
		query = query.Where("(tasks.title LIKE ? ESCAPE '\\' OR tasks.description LIKE ? ESCAPE '\\')", like, like)
	}
	return query
}

func (a *API) createTask(c *gin.Context) {
	var input createTaskRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	task, err := taskFromCreateRequest(input)
	if err != nil {
		if errors.Is(err, errLifecycleCommandRequired) {
			writeError(c, http.StatusUnprocessableEntity, "LIFECYCLE_COMMAND_REQUIRED", err.Error())
			return
		}
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	tagIDs, err := validateTaskTagIDs(input.TagIDs)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	legacyV2RequestHash := ""
	legacyV1RequestHash := ""
	legacyV2Compatible := task.ReviewPolicy == "none"
	legacyV1Compatible := legacyV2Compatible && task.Kind == "work" && task.ParentTaskID == nil && task.CompletionCriteria == "" && len(tagIDs) == 0
	if idempotencyKey != "" {
		requestHash, err = taskCreateRequestHash(task, tagIDs)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		if legacyV2Compatible {
			legacyV2RequestHash, err = legacyV2TaskCreateRequestHash(task, tagIDs)
			if err != nil {
				writeDatabaseError(c)
				return
			}
		}
		if legacyV1Compatible {
			legacyV1RequestHash, err = legacyTaskCreateRequestHash(task)
			if err != nil {
				writeDatabaseError(c)
				return
			}
		}
	}

	replayed := false
	statusCode := http.StatusCreated
	var response models.Task
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createTaskEndpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_REPLAY_UNAVAILABLE",
						"This legacy Idempotency-Key cannot be replayed safely; use a new key",
					)
				}
				legacyReplay := (legacyV2Compatible && *existing.RequestHash == legacyV2RequestHash) ||
					(legacyV1Compatible && *existing.RequestHash == legacyV1RequestHash)
				if *existing.RequestHash != requestHash && !legacyReplay {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_CONFLICT",
						"Idempotency-Key was already used with a different task request",
					)
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent task response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read idempotency key: %w", err)
			}
		}
		if task.ProjectID != nil {
			if err := requireAssignableProject(tx, *task.ProjectID); err != nil {
				return err
			}
		}
		if task.ParentTaskID != nil {
			if err := requireValidTaskParent(tx, task.ID, *task.ParentTaskID); err != nil {
				return err
			}
		}
		if err := requireTaskTags(tx, tagIDs); err != nil {
			return err
		}
		if err := tx.Create(&task).Error; err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		if err := replaceTaskTags(tx, task.ID, tagIDs); err != nil {
			return err
		}
		response, err = loadTask(tx, task.ID)
		if err != nil {
			return fmt.Errorf("load created task: %w", err)
		}
		if idempotencyKey != "" {
			responseBody, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent task response: %w", err)
			}
			responseText := string(responseBody)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: createTaskEndpoint, ResourceID: task.ID,
				RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &responseStatus,
				CreatedAt: task.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record idempotency key: %w", err)
			}
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
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response)
	version := response.Version
	if version < 1 {
		version = 1
	}
	setProjectETag(c, version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getTask(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	var task models.Task
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var loadErr error
		task, loadErr = loadTask(tx, id)
		return loadErr
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (a *API) updateTask(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateTaskRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	updates := make(map[string]any)
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if length := utf8.RuneCountInString(title); length < 2 || length > 200 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title must contain 2 to 200 characters")
			return
		}
		updates["title"] = title
	}
	if input.Description != nil {
		if utf8.RuneCountInString(*input.Description) > 10_000 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "description cannot exceed 10000 characters")
			return
		}
		updates["description"] = *input.Description
	}
	if input.Kind != nil {
		kind := strings.TrimSpace(*input.Kind)
		if _, valid := validTaskKinds[kind]; !valid {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "kind must be work, review, followup, or reminder")
			return
		}
		updates["kind"] = kind
	}
	if input.Priority != nil {
		priority := strings.TrimSpace(*input.Priority)
		if _, valid := validPriorities[priority]; !valid {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "priority must be P0, P1, P2, or P3")
			return
		}
		updates["priority"] = priority
	}
	if input.ReviewPolicy != nil {
		reviewPolicy := strings.TrimSpace(*input.ReviewPolicy)
		if reviewPolicy != "none" && reviewPolicy != "manual" {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "review_policy must be none or manual")
			return
		}
		updates["review_policy"] = reviewPolicy
	}
	if input.ProjectID.Set {
		if input.ProjectID.Value == nil {
			updates["project_id"] = nil
		} else {
			projectID := strings.TrimSpace(*input.ProjectID.Value)
			if _, err := uuid.Parse(projectID); err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_id must be a UUID")
				return
			}
			updates["project_id"] = projectID
		}
	}
	if input.ParentTaskID.Set {
		if input.ParentTaskID.Value == nil {
			updates["parent_task_id"] = nil
		} else {
			parentTaskID := strings.TrimSpace(*input.ParentTaskID.Value)
			if _, err := uuid.Parse(parentTaskID); err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "parent_task_id must be a UUID")
				return
			}
			updates["parent_task_id"] = parentTaskID
		}
	}
	if input.CompletionCriteria != nil {
		if utf8.RuneCountInString(*input.CompletionCriteria) > 10_000 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "completion_criteria cannot exceed 10000 characters")
			return
		}
		updates["completion_criteria"] = *input.CompletionCriteria
	}
	if input.DueDate.Set {
		if input.DueDate.Value == nil {
			updates["due_date"] = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.DueDate.Value))
			if err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date must be an RFC 3339 timestamp")
				return
			}
			updates["due_date"] = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	if input.PlannedDate.Set {
		if input.PlannedDate.Value == nil {
			updates["planned_date"] = nil
		} else {
			plannedDate := strings.TrimSpace(*input.PlannedDate.Value)
			if !validDate(plannedDate) {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "planned_date must use YYYY-MM-DD")
				return
			}
			updates["planned_date"] = plannedDate
		}
	}
	if input.EstimatedMinutes.Set {
		if input.EstimatedMinutes.Value == nil {
			updates["estimated_minutes"] = nil
		} else {
			if *input.EstimatedMinutes.Value < 0 {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "estimated_minutes cannot be negative")
				return
			}
			updates["estimated_minutes"] = *input.EstimatedMinutes.Value
		}
	}
	var tagIDs []string
	if input.TagIDs.Set {
		var err error
		tagIDs, err = validateTaskTagIDs(input.TagIDs.Value)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
	}
	if len(updates) == 0 && !input.TagIDs.Set {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable task field is required")
		return
	}

	var task models.Task
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current models.Task
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		if current.Version != expectedVersion {
			return taskVersionConflict()
		}
		if input.ReviewPolicy != nil {
			targetPolicy := updates["review_policy"].(string)
			if targetPolicy != current.ReviewPolicy {
				if current.Status != "todo" {
					return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_POLICY_LOCKED", "review_policy can only be changed while the Task is todo")
				}
				var submissionCount int64
				if err := tx.Model(&models.TaskSubmission{}).Where("task_id = ?", id).Count(&submissionCount).Error; err != nil {
					return err
				}
				if submissionCount != 0 {
					return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_POLICY_LOCKED", "review_policy cannot change after a submission exists")
				}
			}
		}
		if input.ProjectID.Set {
			if target, ok := updates["project_id"].(string); ok {
				unchanged := current.ProjectID != nil && *current.ProjectID == target
				if !unchanged {
					if err := requireAssignableProject(tx, target); err != nil {
						return err
					}
				}
			}
		}
		if input.ParentTaskID.Set {
			if target, ok := updates["parent_task_id"].(string); ok {
				if err := requireValidTaskParent(tx, id, target); err != nil {
					return err
				}
			}
		}
		if input.TagIDs.Set {
			if err := requireTaskTags(tx, tagIDs); err != nil {
				return err
			}
		}
		if input.PlannedDate.Set {
			var target *string
			if plannedDate, ok := updates["planned_date"].(string); ok {
				target = &plannedDate
			}
			changed := (current.PlannedDate == nil) != (target == nil)
			if !changed && target != nil {
				changed = *current.PlannedDate != *target
			}
			if changed {
				updates["manual_order"] = nil
			}
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		updates["updated_at"] = now
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		if input.TagIDs.Set {
			if err := replaceTaskTags(tx, id, tagIDs); err != nil {
				return err
			}
		}
		loaded, err := loadTask(tx, id)
		task = loaded
		if err != nil {
			return err
		}
		if input.ReviewPolicy != nil && current.ReviewPolicy != task.ReviewPolicy {
			_, err = recordTaskLifecycleEvent(
				tx, "task_review_policy_changed", id,
				taskLifecycleSnapshot(current, ""), taskLifecycleSnapshot(task, ""),
				requestIDFromContext(c), now, 1,
			)
		}
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (a *API) updateTaskStatus(c *gin.Context) {
	writeError(
		c,
		http.StatusGone,
		"TASK_STATUS_ENDPOINT_DEPRECATED",
		"Use an explicit task lifecycle command endpoint",
	)
}

func (a *API) deleteTask(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var movedArtifactFiles []trashedArtifactFile
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		if task.Version != expectedVersion {
			return taskVersionConflict()
		}
		var openFocusSessions int64
		if err := tx.Model(&models.FocusSession{}).
			Where("task_id = ? AND status IN ?", id, []string{"active", "paused", "recovery_pending"}).
			Count(&openFocusSessions).Error; err != nil {
			return err
		}
		if openFocusSessions > 0 {
			return newProjectRequestError(
				http.StatusConflict,
				"TASK_HAS_OPEN_FOCUS_SESSION",
				"Stop, cancel, or recover the open Focus Session before deleting this task",
			)
		}
		deletedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
		var err error
		movedArtifactFiles, err = a.trashTaskArtifactFiles(tx, id, deletedAt)
		if err != nil {
			return err
		}
		result := tx.Delete(&models.Task{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		return nil
	})
	if err != nil {
		if restoreErr := a.restoreTaskArtifactFiles(movedArtifactFiles); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("Task delete Artifact compensation failed task_id=%s error=%v", id, restoreErr)
		}
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	for _, moved := range movedArtifactFiles {
		a.artifactStore.purgeTrashedFile(moved)
	}
	c.Status(http.StatusNoContent)
}

func requireAssignableProject(db *gorm.DB, id string) error {
	var project struct {
		Status string `gorm:"column:status"`
	}
	if err := db.Table("projects").Select("status").Where("id = ?", id).Take(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newProjectRequestError(
				http.StatusUnprocessableEntity,
				"PROJECT_NOT_FOUND",
				"project_id does not reference an existing project",
			)
		}
		return err
	}
	if project.Status == "archived" {
		return newProjectRequestError(
			http.StatusConflict,
			"PROJECT_ARCHIVED",
			"Archived projects cannot accept new task links",
		)
	}
	return nil
}

func taskFromCreateRequest(input createTaskRequest) (models.Task, error) {
	title := strings.TrimSpace(input.Title)
	if length := utf8.RuneCountInString(title); length < 2 || length > 200 {
		return models.Task{}, errors.New("title must contain 2 to 200 characters")
	}
	status := "todo"
	if input.Status != nil {
		status = strings.TrimSpace(*input.Status)
	}
	if status != "todo" {
		return models.Task{}, errLifecycleCommandRequired
	}
	kind := "work"
	if input.Kind != nil {
		kind = strings.TrimSpace(*input.Kind)
	}
	if _, valid := validTaskKinds[kind]; !valid {
		return models.Task{}, errors.New("kind must be work, review, followup, or reminder")
	}
	priority := "P2"
	if input.Priority != nil {
		priority = strings.TrimSpace(*input.Priority)
	}
	if _, valid := validPriorities[priority]; !valid {
		return models.Task{}, errors.New("priority must be P0, P1, P2, or P3")
	}
	reviewPolicy := "none"
	if input.ReviewPolicy != nil {
		reviewPolicy = strings.TrimSpace(*input.ReviewPolicy)
	}
	if reviewPolicy != "none" && reviewPolicy != "manual" {
		return models.Task{}, errors.New("review_policy must be none or manual")
	}
	if input.ProjectID != nil {
		projectID := strings.TrimSpace(*input.ProjectID)
		if _, err := uuid.Parse(projectID); err != nil {
			return models.Task{}, errors.New("project_id must be a UUID")
		}
		input.ProjectID = &projectID
	}
	if input.ParentTaskID != nil {
		parentTaskID := strings.TrimSpace(*input.ParentTaskID)
		if _, err := uuid.Parse(parentTaskID); err != nil {
			return models.Task{}, errors.New("parent_task_id must be a UUID")
		}
		input.ParentTaskID = &parentTaskID
	}
	if input.DueDate != nil {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.DueDate))
		if err != nil {
			return models.Task{}, errors.New("due_date must be an RFC 3339 timestamp")
		}
		normalized := parsed.UTC().Format(time.RFC3339Nano)
		input.DueDate = &normalized
	}
	if input.PlannedDate != nil {
		plannedDate := strings.TrimSpace(*input.PlannedDate)
		if !validDate(plannedDate) {
			return models.Task{}, errors.New("planned_date must use YYYY-MM-DD")
		}
		input.PlannedDate = &plannedDate
	}
	if input.EstimatedMinutes != nil && *input.EstimatedMinutes < 0 {
		return models.Task{}, errors.New("estimated_minutes cannot be negative")
	}
	if input.ManualOrder != nil && *input.ManualOrder < 0 {
		return models.Task{}, errors.New("manual_order cannot be negative")
	}
	description := ""
	if input.Description != nil {
		if utf8.RuneCountInString(*input.Description) > 10_000 {
			return models.Task{}, errors.New("description cannot exceed 10000 characters")
		}
		description = *input.Description
	}
	completionCriteria := ""
	if input.CompletionCriteria != nil {
		if utf8.RuneCountInString(*input.CompletionCriteria) > 10_000 {
			return models.Task{}, errors.New("completion_criteria cannot exceed 10000 characters")
		}
		completionCriteria = *input.CompletionCriteria
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.Task{
		ID: uuid.NewString(), Title: title, Description: description, Kind: kind, Status: status, Priority: priority,
		ReviewPolicy: reviewPolicy,
		ProjectID:    input.ProjectID, ParentTaskID: input.ParentTaskID, CompletionCriteria: completionCriteria,
		DueDate: input.DueDate, PlannedDate: input.PlannedDate,
		EstimatedMinutes: input.EstimatedMinutes, ActualMinutes: 0, ManualOrder: input.ManualOrder,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func taskCreateRequestHash(task models.Task, tagIDs []string) (string, error) {
	payload := struct {
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		Kind               string   `json:"kind"`
		Status             string   `json:"status"`
		Priority           string   `json:"priority"`
		ProjectID          *string  `json:"project_id"`
		ParentTaskID       *string  `json:"parent_task_id"`
		CompletionCriteria string   `json:"completion_criteria"`
		TagIDs             []string `json:"tag_ids"`
		DueDate            *string  `json:"due_date"`
		PlannedDate        *string  `json:"planned_date"`
		EstimatedMinutes   *int     `json:"estimated_minutes"`
		ManualOrder        *int     `json:"manual_order"`
		ReviewPolicy       string   `json:"review_policy"`
	}{
		Title: task.Title, Description: task.Description, Kind: task.Kind, Status: task.Status, Priority: task.Priority,
		ProjectID: task.ProjectID, ParentTaskID: task.ParentTaskID,
		CompletionCriteria: task.CompletionCriteria, TagIDs: tagIDs,
		DueDate: task.DueDate, PlannedDate: task.PlannedDate,
		EstimatedMinutes: task.EstimatedMinutes, ManualOrder: task.ManualOrder,
		ReviewPolicy: task.ReviewPolicy,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode task request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "v3:" + fmt.Sprintf("%x", digest), nil
}

func legacyV2TaskCreateRequestHash(task models.Task, tagIDs []string) (string, error) {
	payload := struct {
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		Kind               string   `json:"kind"`
		Status             string   `json:"status"`
		Priority           string   `json:"priority"`
		ProjectID          *string  `json:"project_id"`
		ParentTaskID       *string  `json:"parent_task_id"`
		CompletionCriteria string   `json:"completion_criteria"`
		TagIDs             []string `json:"tag_ids"`
		DueDate            *string  `json:"due_date"`
		PlannedDate        *string  `json:"planned_date"`
		EstimatedMinutes   *int     `json:"estimated_minutes"`
		ManualOrder        *int     `json:"manual_order"`
	}{
		Title: task.Title, Description: task.Description, Kind: task.Kind, Status: task.Status, Priority: task.Priority,
		ProjectID: task.ProjectID, ParentTaskID: task.ParentTaskID,
		CompletionCriteria: task.CompletionCriteria, TagIDs: tagIDs,
		DueDate: task.DueDate, PlannedDate: task.PlannedDate,
		EstimatedMinutes: task.EstimatedMinutes, ManualOrder: task.ManualOrder,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode legacy v2 task request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "v2:" + fmt.Sprintf("%x", digest), nil
}

func legacyTaskCreateRequestHash(task models.Task) (string, error) {
	payload := struct {
		Title            string  `json:"title"`
		Description      string  `json:"description"`
		Status           string  `json:"status"`
		Priority         string  `json:"priority"`
		ProjectID        *string `json:"project_id"`
		DueDate          *string `json:"due_date"`
		PlannedDate      *string `json:"planned_date"`
		EstimatedMinutes *int    `json:"estimated_minutes"`
		ManualOrder      *int    `json:"manual_order"`
	}{
		Title: task.Title, Description: task.Description, Status: task.Status, Priority: task.Priority,
		ProjectID: task.ProjectID, DueDate: task.DueDate, PlannedDate: task.PlannedDate,
		EstimatedMinutes: task.EstimatedMinutes, ManualOrder: task.ManualOrder,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode legacy task request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func taskID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(id)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_TASK_ID", "Task id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func queryInt(c *gin.Context, key string, fallback, minimum, maximum int) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		writeError(c, http.StatusBadRequest, "INVALID_PAGINATION", fmt.Sprintf("%s must be between %d and %d", key, minimum, maximum))
		return 0, false
	}
	return value, true
}

func applyTaskSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	// The explicit manual mode must retain the same deterministic fallback as
	// the default list. Otherwise clearing manual_order would expose UUID order
	// instead of returning to the documented priority/due-date order.
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "manual_order" {
		return query.
			Order("CASE WHEN tasks.manual_order IS NULL THEN 1 ELSE 0 END ASC").
			Order("tasks.manual_order ASC").
			Order("CASE tasks.priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END ASC").
			Order("CASE WHEN tasks.due_date IS NULL THEN 1 ELSE 0 END ASC").
			Order("tasks.due_date ASC").
			Order("tasks.created_at ASC").
			Order("tasks.id ASC"), true
	}
	allowed := map[string]string{
		"manual_order": "tasks.manual_order", "priority": "tasks.priority", "due_date": "tasks.due_date",
		"planned_date": "tasks.planned_date", "created_at": "tasks.created_at", "updated_at": "tasks.updated_at",
		"title": "tasks.title", "status": "tasks.status", "kind": "tasks.kind",
	}
	nullable := map[string]struct{}{"manual_order": {}, "due_date": {}, "planned_date": {}}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		direction := "ASC"
		if strings.HasPrefix(field, "-") {
			direction = "DESC"
			field = strings.TrimPrefix(field, "-")
		}
		column, ok := allowed[field]
		if !ok {
			return query, false
		}
		if _, ok := nullable[field]; ok {
			query = query.Order("CASE WHEN " + column + " IS NULL THEN 1 ELSE 0 END ASC")
		}
		query = query.Order(column + " " + direction)
	}
	return query.Order("tasks.id ASC"), true
}

func withTaskProject(query *gorm.DB) *gorm.DB {
	return query.
		Model(&models.Task{}).
		Select(`
			tasks.*,
			projects.name AS project_name,
			parent_tasks.title AS parent_task_title,
			(SELECT COUNT(*) FROM tasks AS subtasks WHERE subtasks.parent_task_id = tasks.id) AS subtask_total,
			(SELECT COUNT(*) FROM tasks AS subtasks WHERE subtasks.parent_task_id = tasks.id AND subtasks.status = 'done') AS subtask_completed
		`).
		Joins("LEFT JOIN projects ON projects.id = tasks.project_id").
		Joins("LEFT JOIN tasks AS parent_tasks ON parent_tasks.id = tasks.parent_task_id")
}

func loadTask(db *gorm.DB, id string) (models.Task, error) {
	var task models.Task
	if err := withTaskProject(db).First(&task, "tasks.id = ?", id).Error; err != nil {
		return models.Task{}, err
	}
	tasks := []models.Task{task}
	if err := hydrateTaskTags(db, tasks); err != nil {
		return models.Task{}, err
	}
	normalizeTask(&tasks[0])
	return tasks[0], nil
}

func hydrateTaskTags(db *gorm.DB, tasks []models.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	indices := make(map[string]int, len(tasks))
	ids := make([]string, len(tasks))
	for index := range tasks {
		tasks[index].Tags = make([]models.Tag, 0)
		indices[tasks[index].ID] = index
		ids[index] = tasks[index].ID
	}
	type taskTagRow struct {
		TaskID    string `gorm:"column:task_id"`
		ID        string `gorm:"column:id"`
		Name      string `gorm:"column:name"`
		Color     string `gorm:"column:color"`
		Version   int64  `gorm:"column:version"`
		CreatedAt string `gorm:"column:created_at"`
	}
	var rows []taskTagRow
	if err := db.Table("task_tags").
		Select("task_tags.task_id, tags.id, tags.name, tags.color, tags.version, tags.created_at").
		Joins("JOIN tags ON tags.id = task_tags.tag_id").
		Where("task_tags.task_id IN ?", ids).
		Order("LOWER(tags.name) ASC").
		Order("tags.id ASC").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indices[row.TaskID]
		if !ok {
			continue
		}
		tag := models.Tag{ID: row.ID, Name: row.Name, Color: row.Color, Version: row.Version, CreatedAt: row.CreatedAt}
		normalizeTag(&tag)
		tasks[index].Tags = append(tasks[index].Tags, tag)
	}
	return nil
}

func validateTaskTagIDs(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, errors.New("a task cannot have more than 20 tags")
	}
	seen := make(map[string]struct{}, len(values))
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if _, err := uuid.Parse(id); err != nil {
			return nil, errors.New("tag_ids must contain UUID values")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func requireTaskTags(db *gorm.DB, tagIDs []string) error {
	if len(tagIDs) == 0 {
		return nil
	}
	var count int64
	if err := db.Model(&models.Tag{}).Where("id IN ?", tagIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(tagIDs)) {
		return newProjectRequestError(http.StatusUnprocessableEntity, "TAG_NOT_FOUND", "tag_ids contains a tag that does not exist")
	}
	return nil
}

func replaceTaskTags(db *gorm.DB, taskID string, tagIDs []string) error {
	if err := db.Exec("DELETE FROM task_tags WHERE task_id = ?", taskID).Error; err != nil {
		return fmt.Errorf("clear task tags: %w", err)
	}
	for _, tagID := range tagIDs {
		if err := db.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", taskID, tagID).Error; err != nil {
			return fmt.Errorf("link task tag: %w", err)
		}
	}
	return nil
}

func requireValidTaskParent(db *gorm.DB, taskID, parentTaskID string) error {
	if taskID == parentTaskID {
		return newProjectRequestError(http.StatusUnprocessableEntity, "TASK_PARENT_CYCLE", "A task cannot be its own parent")
	}
	var count int64
	if err := db.Model(&models.Task{}).Where("id = ?", parentTaskID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return newProjectRequestError(http.StatusUnprocessableEntity, "PARENT_TASK_NOT_FOUND", "parent_task_id does not reference an existing task")
	}
	if err := db.Raw(`
		WITH RECURSIVE ancestors(id, parent_task_id) AS (
			SELECT id, parent_task_id FROM tasks WHERE id = ?
			UNION
			SELECT tasks.id, tasks.parent_task_id
			FROM tasks
			JOIN ancestors ON tasks.id = ancestors.parent_task_id
		)
		SELECT COUNT(*) FROM ancestors WHERE id = ?
	`, parentTaskID, taskID).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return newProjectRequestError(http.StatusUnprocessableEntity, "TASK_PARENT_CYCLE", "parent_task_id would create a task cycle")
	}
	return nil
}

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "%", "\\%")
	return strings.ReplaceAll(value, "_", "\\_")
}

func validateIdempotencyKey(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 128 {
		return errors.New("Idempotency-Key cannot exceed 128 bytes")
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return errors.New("Idempotency-Key must contain printable ASCII without spaces")
		}
	}
	return nil
}

func normalizeTasks(tasks []models.Task) {
	for index := range tasks {
		normalizeTask(&tasks[index])
	}
}

func normalizeTask(task *models.Task) {
	if task.Kind == "" {
		task.Kind = "work"
	}
	if task.Version < 1 {
		task.Version = 1
	}
	if task.ReviewPolicy == "" {
		task.ReviewPolicy = "none"
	}
	if task.Tags == nil {
		task.Tags = make([]models.Tag, 0)
	}
	task.CreatedAt = normalizeTimestamp(task.CreatedAt)
	task.UpdatedAt = normalizeTimestamp(task.UpdatedAt)
	if task.DueDate != nil {
		normalized := normalizeTimestamp(*task.DueDate)
		task.DueDate = &normalized
	}
	if task.CompletedAt != nil {
		normalized := normalizeTimestamp(*task.CompletedAt)
		task.CompletedAt = &normalized
	}
	for _, field := range []**string{
		&task.BlockedAt,
		&task.SubmittedAt,
		&task.ReviewedAt,
	} {
		if *field != nil {
			normalized := normalizeTimestamp(**field)
			*field = &normalized
		}
	}
}

func normalizeTimestamp(value string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return value
}
