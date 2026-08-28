package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

const createTaskEndpoint = "POST /api/v1/tasks"

var (
	validTaskStatuses = map[string]struct{}{"todo": {}, "in_progress": {}, "done": {}}
	validPriorities   = map[string]struct{}{"P0": {}, "P1": {}, "P2": {}, "P3": {}}
)

type createTaskRequest struct {
	Title            string  `json:"title"`
	Description      *string `json:"description"`
	Status           *string `json:"status"`
	Priority         *string `json:"priority"`
	ProjectID        *string `json:"project_id"`
	DueDate          *string `json:"due_date"`
	PlannedDate      *string `json:"planned_date"`
	EstimatedMinutes *int    `json:"estimated_minutes"`
	ManualOrder      *int    `json:"manual_order"`
}

type updateTaskStatusRequest struct {
	Status string `json:"status"`
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
	Title            *string             `json:"title"`
	Description      *string             `json:"description"`
	Priority         *string             `json:"priority"`
	ProjectID        nullableStringPatch `json:"project_id"`
	DueDate          nullableStringPatch `json:"due_date"`
	PlannedDate      nullableStringPatch `json:"planned_date"`
	EstimatedMinutes nullableIntPatch    `json:"estimated_minutes"`
}

type pageMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
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

	query := a.db.WithContext(c.Request.Context()).Model(&models.Task{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if _, valid := validTaskStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
		query = query.Where("tasks.status = ?", status)
	}
	if priority := strings.TrimSpace(c.Query("priority")); priority != "" {
		if _, valid := validPriorities[priority]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "priority filter is invalid")
			return
		}
		query = query.Where("tasks.priority = ?", priority)
	}
	if projectID := strings.TrimSpace(c.Query("project_id")); projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "project_id filter must be a UUID")
			return
		}
		query = query.Where("tasks.project_id = ?", projectID)
	}
	if plannedDate := strings.TrimSpace(c.Query("planned_date")); plannedDate != "" {
		if !validDate(plannedDate) {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "planned_date filter must use YYYY-MM-DD")
			return
		}
		query = query.Where("tasks.planned_date = ?", plannedDate)
	}
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + escapeLike(search) + "%"
		query = query.Where("(tasks.title LIKE ? ESCAPE '\\' OR tasks.description LIKE ? ESCAPE '\\')", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	ordered, ok := applyTaskSort(query, c.Query("sort"))
	if !ok {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	var tasks []models.Task
	if err := withTaskProject(ordered).Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	normalizeTasks(tasks)
	c.JSON(http.StatusOK, gin.H{"data": tasks, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createTask(c *gin.Context) {
	var input createTaskRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	task, err := taskFromCreateRequest(input)
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
	if idempotencyKey != "" {
		requestHash, err = taskCreateRequestHash(task)
		if err != nil {
			writeDatabaseError(c)
			return
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
				if *existing.RequestHash != requestHash {
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
		if err := tx.Create(&task).Error; err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		if err := withTaskProject(tx).First(&response, "tasks.id = ?", task.ID).Error; err != nil {
			return fmt.Errorf("load created task: %w", err)
		}
		normalizeTask(&response)
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
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getTask(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	var task models.Task
	if err := withTaskProject(a.db.WithContext(c.Request.Context())).First(&task, "tasks.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	normalizeTask(&task)
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (a *API) updateTask(c *gin.Context) {
	id, ok := taskID(c)
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
	if input.Priority != nil {
		priority := strings.TrimSpace(*input.Priority)
		if _, valid := validPriorities[priority]; !valid {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "priority must be P0, P1, P2, or P3")
			return
		}
		updates["priority"] = priority
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
	if len(updates) == 0 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable task field is required")
		return
	}

	var task models.Task
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if input.ProjectID.Set {
			var current models.Task
			if err := tx.Select("id", "project_id").First(&current, "id = ?", id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
				}
				return err
			}
			if target, ok := updates["project_id"].(string); ok {
				unchanged := current.ProjectID != nil && *current.ProjectID == target
				if !unchanged {
					if err := requireAssignableProject(tx, target); err != nil {
						return err
					}
				}
			}
		}
		result := tx.Model(&models.Task{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return withTaskProject(tx).First(&task, "tasks.id = ?", id).Error
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	normalizeTask(&task)
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (a *API) updateTaskStatus(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	var input updateTaskStatusRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if _, valid := validTaskStatuses[input.Status]; !valid {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status must be todo, in_progress, or done")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updates := map[string]any{"status": input.Status, "updated_at": now}
	if input.Status == "done" {
		updates["completed_at"] = now
	} else {
		updates["completed_at"] = nil
	}
	result := a.db.WithContext(c.Request.Context()).Model(&models.Task{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		writeDatabaseError(c)
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		return
	}
	var task models.Task
	if err := withTaskProject(a.db.WithContext(c.Request.Context())).First(&task, "tasks.id = ?", id).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	normalizeTask(&task)
	c.JSON(http.StatusOK, gin.H{"data": task})
}

func (a *API) deleteTask(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	result := a.db.WithContext(c.Request.Context()).Delete(&models.Task{}, "id = ?", id)
	if result.Error != nil {
		writeDatabaseError(c)
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		return
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
	if _, valid := validTaskStatuses[status]; !valid {
		return models.Task{}, errors.New("status must be todo, in_progress, or done")
	}
	priority := "P2"
	if input.Priority != nil {
		priority = strings.TrimSpace(*input.Priority)
	}
	if _, valid := validPriorities[priority]; !valid {
		return models.Task{}, errors.New("priority must be P0, P1, P2, or P3")
	}
	if input.ProjectID != nil {
		projectID := strings.TrimSpace(*input.ProjectID)
		if _, err := uuid.Parse(projectID); err != nil {
			return models.Task{}, errors.New("project_id must be a UUID")
		}
		input.ProjectID = &projectID
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
	description := ""
	if input.Description != nil {
		if utf8.RuneCountInString(*input.Description) > 10_000 {
			return models.Task{}, errors.New("description cannot exceed 10000 characters")
		}
		description = *input.Description
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if status == "done" {
		completedAt = &now
	}
	return models.Task{
		ID: uuid.NewString(), Title: title, Description: description, Status: status, Priority: priority,
		ProjectID: input.ProjectID, DueDate: input.DueDate, PlannedDate: input.PlannedDate,
		EstimatedMinutes: input.EstimatedMinutes, ActualMinutes: 0, ManualOrder: input.ManualOrder,
		CreatedAt: now, UpdatedAt: now, CompletedAt: completedAt,
	}, nil
}

func taskCreateRequestHash(task models.Task) (string, error) {
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
		return "", fmt.Errorf("encode task request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func taskID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_TASK_ID", "Task id must be a UUID")
		return "", false
	}
	return id, true
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
	if strings.TrimSpace(raw) == "" {
		return query.
			Order("CASE WHEN tasks.manual_order IS NULL THEN 1 ELSE 0 END ASC").
			Order("tasks.manual_order ASC").
			Order("CASE tasks.priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END ASC").
			Order("CASE WHEN tasks.due_date IS NULL THEN 1 ELSE 0 END ASC").
			Order("tasks.due_date ASC").
			Order("tasks.created_at ASC"), true
	}
	allowed := map[string]string{
		"manual_order": "tasks.manual_order", "priority": "tasks.priority", "due_date": "tasks.due_date",
		"planned_date": "tasks.planned_date", "created_at": "tasks.created_at", "updated_at": "tasks.updated_at",
		"title": "tasks.title", "status": "tasks.status",
	}
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
		query = query.Order(column + " " + direction)
	}
	return query, true
}

func withTaskProject(query *gorm.DB) *gorm.DB {
	return query.
		Model(&models.Task{}).
		Select("tasks.*, projects.name AS project_name").
		Joins("LEFT JOIN projects ON projects.id = tasks.project_id")
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
}

func normalizeTimestamp(value string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return value
}
