package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	taskSavedViewSchemaVersion = 1
	maxTaskSavedViews          = 20
)

type taskSavedViewDefinition struct {
	Query       string   `json:"q"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Kind        string   `json:"kind"`
	ProjectID   string   `json:"project_id"`
	ClientID    string   `json:"client_id"`
	TagIDs      []string `json:"tag_ids"`
	PlannedDate string   `json:"planned_date"`
	PlannedFrom string   `json:"planned_from"`
	PlannedTo   string   `json:"planned_to"`
	DueFrom     string   `json:"due_from"`
	DueTo       string   `json:"due_to"`
	Sort        string   `json:"sort"`
}

type taskSavedViewResponse struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Definition    taskSavedViewDefinition `json:"definition"`
	SchemaVersion int                     `json:"schema_version"`
	Version       int64                   `json:"version"`
	CreatedAt     string                  `json:"created_at"`
	UpdatedAt     string                  `json:"updated_at"`
}

type createTaskSavedViewRequest struct {
	Name       string                  `json:"name"`
	Definition taskSavedViewDefinition `json:"definition"`
}

type updateTaskSavedViewRequest struct {
	Name       *string                  `json:"name"`
	Definition *taskSavedViewDefinition `json:"definition"`
}

func (a *API) listTaskSavedViews(c *gin.Context) {
	var rows []models.TaskSavedView
	if err := a.db.WithContext(c.Request.Context()).Order("lower(name) ASC, id ASC").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]taskSavedViewResponse, 0, len(rows))
	for _, row := range rows {
		response, err := taskSavedViewFromModel(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		responses = append(responses, response)
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) createTaskSavedView(c *gin.Context) {
	var input createTaskSavedViewRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	name, err := validateTaskSavedViewName(input.Name)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	definition, definitionJSON, err := normalizeTaskSavedViewDefinition(input.Definition)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	row := models.TaskSavedView{
		ID: uuid.NewString(), Name: name, DefinitionJSON: definitionJSON,
		SchemaVersion: taskSavedViewSchemaVersion, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.TaskSavedView{}).Count(&count).Error; err != nil {
			return err
		}
		if count >= maxTaskSavedViews {
			return newProjectRequestError(http.StatusConflict, "TASK_SAVED_VIEW_LIMIT_REACHED", "At most 20 task saved views are allowed")
		}
		if err := requireUniqueTaskSavedViewName(tx, name, ""); err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	response := taskSavedViewResponse{
		ID: row.ID, Name: row.Name, Definition: definition, SchemaVersion: row.SchemaVersion,
		Version: row.Version, CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func (a *API) updateTaskSavedView(c *gin.Context) {
	id, ok := taskSavedViewID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateTaskSavedViewRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if input.Name == nil && input.Definition == nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name or definition is required")
		return
	}

	var response taskSavedViewResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current models.TaskSavedView
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_SAVED_VIEW_NOT_FOUND", "Task saved view not found")
			}
			return err
		}
		if current.Version != expectedVersion {
			return taskVersionConflict()
		}
		updates := map[string]any{}
		if input.Name != nil {
			name, err := validateTaskSavedViewName(*input.Name)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
			if err := requireUniqueTaskSavedViewName(tx, name, id); err != nil {
				return err
			}
			updates["name"] = name
		}
		if input.Definition != nil {
			_, encoded, err := normalizeTaskSavedViewDefinition(*input.Definition)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
			updates["definition_json"] = encoded
			updates["schema_version"] = taskSavedViewSchemaVersion
		}
		updates["version"] = gorm.Expr("version + 1")
		updates["updated_at"] = a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.TaskSavedView{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return taskVersionConflict()
		}
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		parsed, err := taskSavedViewFromModel(current)
		if err != nil {
			return err
		}
		response = parsed
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteTaskSavedView(c *gin.Context) {
	id, ok := taskSavedViewID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if c.Query("confirm") != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Task saved view deletion requires confirm=true")
		return
	}
	result := a.db.WithContext(c.Request.Context()).Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.TaskSavedView{})
	if result.Error != nil {
		writeDatabaseError(c)
		return
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := a.db.WithContext(c.Request.Context()).Model(&models.TaskSavedView{}).Where("id = ?", id).Count(&count).Error; err != nil {
			writeDatabaseError(c)
			return
		}
		if count == 0 {
			writeError(c, http.StatusNotFound, "TASK_SAVED_VIEW_NOT_FOUND", "Task saved view not found")
			return
		}
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted_id": id}})
}

func normalizeTaskSavedViewDefinition(input taskSavedViewDefinition) (taskSavedViewDefinition, string, error) {
	input.Query = strings.TrimSpace(input.Query)
	input.Status = strings.TrimSpace(input.Status)
	input.Priority = strings.TrimSpace(input.Priority)
	input.Kind = strings.TrimSpace(input.Kind)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.PlannedDate = strings.TrimSpace(input.PlannedDate)
	input.PlannedFrom = strings.TrimSpace(input.PlannedFrom)
	input.PlannedTo = strings.TrimSpace(input.PlannedTo)
	input.DueFrom = strings.TrimSpace(input.DueFrom)
	input.DueTo = strings.TrimSpace(input.DueTo)
	input.Sort = strings.TrimSpace(input.Sort)
	if utf8.RuneCountInString(input.Query) > 200 {
		return input, "", errors.New("q must not exceed 200 characters")
	}
	if input.Status != "" {
		if _, valid := validTaskStatuses[input.Status]; !valid && input.Status != "active" {
			return input, "", errors.New("status is invalid")
		}
	}
	if input.Priority != "" {
		if _, valid := validPriorities[input.Priority]; !valid {
			return input, "", errors.New("priority is invalid")
		}
	}
	if input.Kind != "" {
		if _, valid := validTaskKinds[input.Kind]; !valid {
			return input, "", errors.New("kind is invalid")
		}
	}
	for _, field := range []struct{ name, value string }{{"project_id", input.ProjectID}, {"client_id", input.ClientID}} {
		if field.value != "" {
			if _, err := uuid.Parse(field.value); err != nil {
				return input, "", fmt.Errorf("%s must be a UUID", field.name)
			}
		}
	}
	seenTags := make(map[string]struct{}, len(input.TagIDs))
	tags := make([]string, 0, len(input.TagIDs))
	for _, raw := range input.TagIDs {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			return input, "", errors.New("tag_ids must contain only UUIDs")
		}
		if _, exists := seenTags[id]; !exists {
			seenTags[id] = struct{}{}
			tags = append(tags, id)
		}
	}
	input.TagIDs = tags
	for _, field := range []struct{ name, value string }{
		{"planned_date", input.PlannedDate}, {"planned_from", input.PlannedFrom}, {"planned_to", input.PlannedTo},
		{"due_from", input.DueFrom}, {"due_to", input.DueTo},
	} {
		if field.value != "" && !validDate(field.value) {
			return input, "", fmt.Errorf("%s must use YYYY-MM-DD", field.name)
		}
	}
	if input.PlannedDate != "" && (input.PlannedFrom != "" || input.PlannedTo != "") {
		return input, "", errors.New("planned_date cannot be combined with a planned range")
	}
	if input.PlannedFrom != "" && input.PlannedTo != "" && input.PlannedFrom > input.PlannedTo {
		return input, "", errors.New("planned_from must not be after planned_to")
	}
	if input.DueFrom != "" && input.DueTo != "" && input.DueFrom > input.DueTo {
		return input, "", errors.New("due_from must not be after due_to")
	}
	if !validTaskSavedViewSort(input.Sort) {
		return input, "", errors.New("sort contains an unsupported field")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return input, "", fmt.Errorf("encode saved view definition: %w", err)
	}
	if len(encoded) > 16384 {
		return input, "", errors.New("saved view definition is too large")
	}
	return input, string(encoded), nil
}

func validTaskSavedViewSort(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "manual_order" {
		return true
	}
	allowed := map[string]struct{}{
		"manual_order": {}, "priority": {}, "due_date": {}, "planned_date": {}, "created_at": {},
		"updated_at": {}, "title": {}, "status": {}, "kind": {},
	}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "-"))
		if _, ok := allowed[field]; !ok {
			return false
		}
	}
	return true
}

func taskSavedViewFromModel(row models.TaskSavedView) (taskSavedViewResponse, error) {
	if row.SchemaVersion != taskSavedViewSchemaVersion {
		return taskSavedViewResponse{}, fmt.Errorf("unsupported task saved view schema %d", row.SchemaVersion)
	}
	var definition taskSavedViewDefinition
	if err := json.Unmarshal([]byte(row.DefinitionJSON), &definition); err != nil {
		return taskSavedViewResponse{}, fmt.Errorf("decode task saved view %s: %w", row.ID, err)
	}
	normalized, _, err := normalizeTaskSavedViewDefinition(definition)
	if err != nil {
		return taskSavedViewResponse{}, fmt.Errorf("validate task saved view %s: %w", row.ID, err)
	}
	return taskSavedViewResponse{
		ID: row.ID, Name: row.Name, Definition: normalized, SchemaVersion: row.SchemaVersion,
		Version: row.Version, CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}, nil
}

func validateTaskSavedViewName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if count := utf8.RuneCountInString(name); count < 1 || count > 80 {
		return "", errors.New("name must contain 1 to 80 characters")
	}
	return name, nil
}

func requireUniqueTaskSavedViewName(tx *gorm.DB, name, excludedID string) error {
	query := tx.Model(&models.TaskSavedView{}).Where("lower(name) = lower(?)", name)
	if excludedID != "" {
		query = query.Where("id <> ?", excludedID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return newProjectRequestError(http.StatusConflict, "TASK_SAVED_VIEW_NAME_EXISTS", "A task saved view with this name already exists")
	}
	return nil
}

func taskSavedViewID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ID", "Task saved view id must be a UUID")
		return "", false
	}
	return id, true
}
