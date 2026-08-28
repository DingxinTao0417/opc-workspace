package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	migrationAssignmentReason = "schema_v7_migration_inferred_owner"
	taskCompletedReason       = "Task completed"
)

var validAssignmentRoles = map[string]struct{}{
	"assignee": {},
	"reviewer": {},
}

type createAssignmentRequest struct {
	Role    string `json:"role"`
	ActorID string `json:"actor_id"`
}

type reassignAssignmentRequest struct {
	Role    string `json:"role"`
	ActorID string `json:"actor_id"`
	Reason  string `json:"reason"`
}

type endAssignmentRequest struct {
	Reason string `json:"reason"`
}

type assignmentActorSummary struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	IsBuiltin   bool   `json:"is_builtin"`
	Version     int64  `json:"version"`
}

type assignmentResponse struct {
	ID                string                 `json:"id"`
	TaskID            string                 `json:"task_id"`
	Role              string                 `json:"role"`
	ActorID           string                 `json:"actor_id"`
	Actor             assignmentActorSummary `json:"actor"`
	AssignedByActorID string                 `json:"assigned_by_actor_id"`
	AssignedByActor   assignmentActorSummary `json:"assigned_by_actor"`
	AssignedAt        string                 `json:"assigned_at"`
	UnassignedAt      *string                `json:"unassigned_at"`
	Reason            *string                `json:"reason"`
	IsActive          bool                   `json:"is_active"`
	Inferred          bool                   `json:"inferred"`
}

type assignmentRow struct {
	ID                     string  `gorm:"column:id"`
	TaskID                 string  `gorm:"column:task_id"`
	Role                   string  `gorm:"column:role"`
	ActorID                string  `gorm:"column:actor_id"`
	AssignedByActorID      string  `gorm:"column:assigned_by_actor_id"`
	AssignedAt             string  `gorm:"column:assigned_at"`
	UnassignedAt           *string `gorm:"column:unassigned_at"`
	Reason                 string  `gorm:"column:reason"`
	ActorType              string  `gorm:"column:actor_type"`
	ActorDisplayName       string  `gorm:"column:actor_display_name"`
	ActorStatus            string  `gorm:"column:actor_status"`
	ActorIsBuiltin         bool    `gorm:"column:actor_is_builtin"`
	ActorVersion           int64   `gorm:"column:actor_version"`
	AssignedByType         string  `gorm:"column:assigned_by_type"`
	AssignedByDisplayName  string  `gorm:"column:assigned_by_display_name"`
	AssignedByStatus       string  `gorm:"column:assigned_by_status"`
	AssignedByIsBuiltin    bool    `gorm:"column:assigned_by_is_builtin"`
	AssignedByActorVersion int64   `gorm:"column:assigned_by_actor_version"`
	Inferred               bool    `gorm:"column:inferred"`
}

type activeAssignmentsResponse struct {
	Assignee *assignmentResponse `json:"assignee"`
	Reviewer *assignmentResponse `json:"reviewer"`
}

type assignmentListData struct {
	Active  activeAssignmentsResponse `json:"active"`
	History []assignmentResponse      `json:"history"`
}

type assignmentListMeta struct {
	Page        int   `json:"page"`
	PageSize    int   `json:"page_size"`
	Total       int64 `json:"total"`
	TaskVersion int64 `json:"task_version"`
}

type assignmentMutationResponse struct {
	Assignment assignmentResponse `json:"assignment"`
	Task       models.Task        `json:"task"`
}

type reassignMutationResponse struct {
	PreviousAssignment assignmentResponse `json:"previous_assignment"`
	Assignment         assignmentResponse `json:"assignment"`
	Task               models.Task        `json:"task"`
}

type reassignEventCurrent struct {
	EndedAssignment assignmentResponse `json:"ended_assignment"`
	Assignment      assignmentResponse `json:"assignment"`
}

type assignmentCommandHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	Role            string `json:"role,omitempty"`
	ActorID         string `json:"actor_id,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

func (a *API) listTaskAssignments(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	role := strings.TrimSpace(c.Query("role"))
	if role != "" {
		if _, valid := validAssignmentRoles[role]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "role filter must be assignee or reviewer")
			return
		}
	}
	sortValue := strings.TrimSpace(c.Query("sort"))
	if sortValue != "" && sortValue != "assigned_at" && sortValue != "-assigned_at" {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort must be assigned_at or -assigned_at")
		return
	}

	var task models.Task
	var activeRows []assignmentRow
	var historyRows []assignmentRow
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&task, "id = ?", taskIDValue).Error; err != nil {
			return err
		}
		if err := assignmentRowsQuery(tx).
			Where("assignment.unassigned_at IS NULL").
			Where("assignment.task_id = ?", taskIDValue).
			Order("CASE assignment.role WHEN 'assignee' THEN 0 ELSE 1 END ASC").
			Find(&activeRows).Error; err != nil {
			return err
		}
		historyQuery := tx.Model(&models.TaskAssignment{}).
			Where("task_id = ? AND unassigned_at IS NOT NULL", taskIDValue)
		if role != "" {
			historyQuery = historyQuery.Where("role = ?", role)
		}
		if err := historyQuery.Count(&total).Error; err != nil {
			return err
		}
		rowsQuery := assignmentRowsQuery(tx).
			Where("assignment.task_id = ? AND assignment.unassigned_at IS NOT NULL", taskIDValue)
		if role != "" {
			rowsQuery = rowsQuery.Where("assignment.role = ?", role)
		}
		if sortValue == "assigned_at" {
			rowsQuery = rowsQuery.Order("assignment.assigned_at ASC").Order("assignment.id ASC")
		} else {
			rowsQuery = rowsQuery.Order("assignment.assigned_at DESC").Order("assignment.id DESC")
		}
		return rowsQuery.Offset((page - 1) * pageSize).Limit(pageSize).Find(&historyRows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}

	data := assignmentListData{History: assignmentResponsesFromRows(historyRows)}
	for _, row := range activeRows {
		response := assignmentResponseFromRow(row)
		switch response.Role {
		case "assignee":
			data.Active.Assignee = &response
		case "reviewer":
			data.Active.Reviewer = &response
		}
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{
		"data": data,
		"meta": assignmentListMeta{Page: page, PageSize: pageSize, Total: total, TaskVersion: task.Version},
	})
}

func (a *API) createTaskAssignment(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input createAssignmentRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	role, err := validateAssignmentRole(input.Role)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	actorIDValue, err := validateAssignmentActorID(input.ActorID)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := assignmentIdempotencyInput(c, assignmentCommandHash{
		ExpectedVersion: expectedVersion,
		Role:            role,
		ActorID:         actorIDValue,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/assignments", taskIDValue)

	statusCode := http.StatusCreated
	replayed := false
	var response assignmentMutationResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayedResponse, replayStatus, replayErr := replayAssignmentCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replayedResponse {
			replayed = true
			statusCode = replayStatus
			return nil
		}

		task, err := loadAssignmentTask(tx, taskIDValue, expectedVersion, true)
		if err != nil {
			return err
		}
		if err := requireAssignmentActor(tx, actorIDValue, role); err != nil {
			return err
		}
		var activeCount int64
		if err := tx.Model(&models.TaskAssignment{}).
			Where("task_id = ? AND role = ? AND unassigned_at IS NULL", taskIDValue, role).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return assignmentAlreadyActiveError()
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := bumpTaskForAssignment(tx, taskIDValue, expectedVersion, now); err != nil {
			return err
		}
		assignment := models.TaskAssignment{
			ID: uuid.NewString(), TaskID: taskIDValue, ActorID: actorIDValue, Role: role,
			AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "",
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return mapAssignmentConstraintError(err)
		}
		created, err := loadAssignmentResponse(tx, assignment.ID)
		if err != nil {
			return err
		}
		loadedTask, err := loadTask(tx, task.ID)
		if err != nil {
			return err
		}
		response = assignmentMutationResponse{Assignment: created, Task: loadedTask}
		if err := recordAssignmentWorkflowEvent(
			tx, "assignment_created", taskIDValue, assignment.ID, nil, created,
			requestIDFromContext(c), now,
		); err != nil {
			return err
		}
		return recordAssignmentIdempotency(
			tx, idempotencyKey, endpoint, assignment.ID, requestHash,
			http.StatusCreated, response, now,
		)
	})
	if err != nil {
		writeAssignmentError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) reassignTask(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input reassignAssignmentRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	role, err := validateAssignmentRole(input.Role)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	actorIDValue, err := validateAssignmentActorID(input.ActorID)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	reason, err := validateAssignmentReason(input.Reason)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := assignmentIdempotencyInput(c, assignmentCommandHash{
		ExpectedVersion: expectedVersion,
		Role:            role,
		ActorID:         actorIDValue,
		Reason:          reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/reassign", taskIDValue)

	statusCode := http.StatusOK
	replayed := false
	var response reassignMutationResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayedResponse, replayStatus, replayErr := replayAssignmentCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replayedResponse {
			replayed = true
			statusCode = replayStatus
			return nil
		}

		task, err := loadAssignmentTask(tx, taskIDValue, expectedVersion, true)
		if err != nil {
			return err
		}
		if err := requireAssignmentActor(tx, actorIDValue, role); err != nil {
			return err
		}
		var current models.TaskAssignment
		if err := tx.Where("task_id = ? AND role = ? AND unassigned_at IS NULL", taskIDValue, role).
			Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "There is no active assignment for this role")
			}
			return err
		}
		if current.ActorID == actorIDValue {
			return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_UNCHANGED", "The selected actor already has this assignment")
		}
		previous, err := loadAssignmentResponse(tx, current.ID)
		if err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := bumpTaskForAssignment(tx, taskIDValue, expectedVersion, now); err != nil {
			return err
		}
		result := tx.Model(&models.TaskAssignment{}).
			Where("id = ? AND unassigned_at IS NULL", current.ID).
			Updates(map[string]any{"unassigned_at": now, "reason": reason})
		if result.Error != nil {
			return mapAssignmentConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
		}
		newAssignment := models.TaskAssignment{
			ID: uuid.NewString(), TaskID: taskIDValue, ActorID: actorIDValue, Role: role,
			AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "",
		}
		if err := tx.Create(&newAssignment).Error; err != nil {
			return mapAssignmentConstraintError(err)
		}
		ended, err := loadAssignmentResponse(tx, current.ID)
		if err != nil {
			return err
		}
		created, err := loadAssignmentResponse(tx, newAssignment.ID)
		if err != nil {
			return err
		}
		loadedTask, err := loadTask(tx, task.ID)
		if err != nil {
			return err
		}
		response = reassignMutationResponse{PreviousAssignment: ended, Assignment: created, Task: loadedTask}
		if err := recordAssignmentWorkflowEvent(
			tx, "assignment_reassigned", taskIDValue, newAssignment.ID,
			previous, reassignEventCurrent{EndedAssignment: ended, Assignment: created},
			requestIDFromContext(c), now,
		); err != nil {
			return err
		}
		return recordAssignmentIdempotency(
			tx, idempotencyKey, endpoint, newAssignment.ID, requestHash,
			http.StatusOK, response, now,
		)
	})
	if err != nil {
		writeAssignmentError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) endAssignment(c *gin.Context) {
	assignmentIDValue, ok := assignmentID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input endAssignmentRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := validateAssignmentReason(input.Reason)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := assignmentIdempotencyInput(c, assignmentCommandHash{
		ExpectedVersion: expectedVersion,
		Reason:          reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/assignments/%s/end", assignmentIDValue)

	statusCode := http.StatusOK
	replayed := false
	var response assignmentMutationResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayedResponse, replayStatus, replayErr := replayAssignmentCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replayedResponse {
			replayed = true
			statusCode = replayStatus
			return nil
		}

		var assignment models.TaskAssignment
		if err := tx.First(&assignment, "id = ?", assignmentIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ASSIGNMENT_NOT_FOUND", "Assignment not found")
			}
			return err
		}
		task, err := loadAssignmentTask(tx, assignment.TaskID, expectedVersion, false)
		if err != nil {
			return err
		}
		if assignment.UnassignedAt != nil {
			return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
		}
		previous, err := loadAssignmentResponse(tx, assignment.ID)
		if err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := bumpTaskForAssignment(tx, task.ID, expectedVersion, now); err != nil {
			return err
		}
		result := tx.Model(&models.TaskAssignment{}).
			Where("id = ? AND unassigned_at IS NULL", assignment.ID).
			Updates(map[string]any{"unassigned_at": now, "reason": reason})
		if result.Error != nil {
			return mapAssignmentConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
		}
		ended, err := loadAssignmentResponse(tx, assignment.ID)
		if err != nil {
			return err
		}
		loadedTask, err := loadTask(tx, task.ID)
		if err != nil {
			return err
		}
		response = assignmentMutationResponse{Assignment: ended, Task: loadedTask}
		if err := recordAssignmentWorkflowEvent(
			tx, "assignment_ended", task.ID, assignment.ID, previous, ended,
			requestIDFromContext(c), now,
		); err != nil {
			return err
		}
		return recordAssignmentIdempotency(
			tx, idempotencyKey, endpoint, assignment.ID, requestHash,
			http.StatusOK, response, now,
		)
	})
	if err != nil {
		writeAssignmentError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func assignmentRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("task_assignments AS assignment").
		Select(`
			assignment.id,
			assignment.task_id,
			assignment.role,
			assignment.actor_id,
			assignment.assigned_by_actor_id,
			assignment.assigned_at,
			assignment.unassigned_at,
			assignment.reason,
			actor.type AS actor_type,
			actor.display_name AS actor_display_name,
			actor.status AS actor_status,
			actor.is_builtin AS actor_is_builtin,
			actor.version AS actor_version,
			assigned_by.type AS assigned_by_type,
			assigned_by.display_name AS assigned_by_display_name,
			assigned_by.status AS assigned_by_status,
			assigned_by.is_builtin AS assigned_by_is_builtin,
			assigned_by.version AS assigned_by_actor_version,
			EXISTS (
				SELECT 1
				FROM workflow_events AS migration_event
				WHERE migration_event.aggregate_type = 'task'
					AND migration_event.aggregate_id = assignment.task_id
					AND migration_event.action = 'migration_assignment_backfill'
					AND migration_event.assignment_id = assignment.id
			) AS inferred
		`).
		Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
		Joins("JOIN actors AS assigned_by ON assigned_by.id = assignment.assigned_by_actor_id")
}

func loadAssignmentResponse(db *gorm.DB, id string) (assignmentResponse, error) {
	var row assignmentRow
	if err := assignmentRowsQuery(db).Where("assignment.id = ?", id).Take(&row).Error; err != nil {
		return assignmentResponse{}, err
	}
	return assignmentResponseFromRow(row), nil
}

func assignmentResponsesFromRows(rows []assignmentRow) []assignmentResponse {
	responses := make([]assignmentResponse, len(rows))
	for index := range rows {
		responses[index] = assignmentResponseFromRow(rows[index])
	}
	return responses
}

func assignmentResponseFromRow(row assignmentRow) assignmentResponse {
	assignedAt := normalizeTimestamp(row.AssignedAt)
	var unassignedAt *string
	if row.UnassignedAt != nil {
		normalized := normalizeTimestamp(*row.UnassignedAt)
		unassignedAt = &normalized
	}
	var reason *string
	if row.Reason != "" && !(row.Inferred && row.Reason == migrationAssignmentReason) {
		value := row.Reason
		reason = &value
	}
	return assignmentResponse{
		ID: row.ID, TaskID: row.TaskID, Role: row.Role,
		ActorID: row.ActorID,
		Actor: assignmentActorSummary{
			ID: row.ActorID, Type: row.ActorType, DisplayName: row.ActorDisplayName,
			Status: row.ActorStatus, IsBuiltin: row.ActorIsBuiltin, Version: row.ActorVersion,
		},
		AssignedByActorID: row.AssignedByActorID,
		AssignedByActor: assignmentActorSummary{
			ID: row.AssignedByActorID, Type: row.AssignedByType, DisplayName: row.AssignedByDisplayName,
			Status: row.AssignedByStatus, IsBuiltin: row.AssignedByIsBuiltin, Version: row.AssignedByActorVersion,
		},
		AssignedAt: assignedAt, UnassignedAt: unassignedAt, Reason: reason,
		IsActive: unassignedAt == nil, Inferred: row.Inferred,
	}
}

func validateAssignmentRole(value string) (string, error) {
	role := strings.TrimSpace(value)
	if _, valid := validAssignmentRoles[role]; !valid {
		return "", errors.New("role must be assignee or reviewer")
	}
	return role, nil
}

func validateAssignmentActorID(value string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("actor_id must be a UUID")
	}
	return parsed.String(), nil
}

func validateAssignmentReason(value string) (string, error) {
	reason := strings.TrimSpace(value)
	if length := utf8.RuneCountInString(reason); length < 1 || length > 1_000 {
		return "", errors.New("reason must contain 1 to 1000 characters")
	}
	for _, character := range reason {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return "", errors.New("reason cannot contain unsupported control characters")
		}
	}
	return reason, nil
}

func assignmentID(c *gin.Context) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ASSIGNMENT_ID", "Assignment id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func requireAssignmentActor(tx *gorm.DB, actorIDValue, role string) error {
	var actor models.Actor
	if err := tx.First(&actor, "id = ?", actorIDValue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newProjectRequestError(http.StatusUnprocessableEntity, "ACTOR_NOT_FOUND", "actor_id does not reference an existing Actor")
		}
		return err
	}
	if role == "reviewer" && actor.Type != "owner" {
		return newProjectRequestError(http.StatusUnprocessableEntity, "ASSIGNMENT_REVIEWER_MUST_BE_OWNER", "The v0.1 reviewer must be the owner")
	}
	if role == "assignee" && actor.Type != "owner" && actor.Type != "person" {
		return newProjectRequestError(http.StatusUnprocessableEntity, "ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED", "The v0.1 assignee must be an owner or person")
	}
	if actor.Status != "active" {
		return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_ACTOR_NOT_ACTIVE", "The selected Actor is inactive")
	}
	return nil
}

func loadAssignmentTask(tx *gorm.DB, id string, expectedVersion int64, requireAssignable bool) (models.Task, error) {
	var task models.Task
	if err := tx.Select("id", "status", "version").First(&task, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Task{}, newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return models.Task{}, err
	}
	if task.Version != expectedVersion {
		return models.Task{}, taskVersionConflict()
	}
	if requireAssignable && task.Status == "done" {
		return models.Task{}, newProjectRequestError(http.StatusConflict, "TASK_NOT_ASSIGNABLE", "Completed tasks cannot be assigned or reassigned")
	}
	return task, nil
}

func bumpTaskForAssignment(tx *gorm.DB, taskIDValue string, expectedVersion int64, updatedAt string) error {
	result := tx.Model(&models.Task{}).
		Where("id = ? AND version = ?", taskIDValue, expectedVersion).
		Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": updatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return taskVersionConflict()
	}
	return nil
}

func assignmentAlreadyActiveError() error {
	return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_ALREADY_ACTIVE", "This task role already has an active assignment")
}

func assignmentIdempotencyInput(c *gin.Context, payload assignmentCommandHash) (string, string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return "", "", false
	}
	if key == "" {
		return "", "", true
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeDatabaseError(c)
		return "", "", false
	}
	digest := sha256.Sum256(encoded)
	return key, "v1:" + fmt.Sprintf("%x", digest), true
}

func replayAssignmentCommand(
	tx *gorm.DB,
	key string,
	endpoint string,
	requestHash string,
	response any,
) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	var existing models.IdempotencyKey
	err := tx.Where("key = ? AND endpoint = ?", key, endpoint).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("read assignment idempotency key: %w", err)
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, newProjectRequestError(
			http.StatusConflict,
			"IDEMPOTENCY_REPLAY_UNAVAILABLE",
			"This legacy Idempotency-Key cannot be replayed safely; use a new key",
		)
	}
	if *existing.RequestHash != requestHash {
		return false, 0, newProjectRequestError(
			http.StatusConflict,
			"IDEMPOTENCY_CONFLICT",
			"Idempotency-Key was already used with a different assignment request",
		)
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, fmt.Errorf("decode idempotent assignment response: %w", err)
	}
	return true, *existing.ResponseStatus, nil
}

func recordAssignmentIdempotency(
	tx *gorm.DB,
	key string,
	endpoint string,
	resourceID string,
	requestHash string,
	status int,
	response any,
	createdAt string,
) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotent assignment response: %w", err)
	}
	responseText := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID,
		RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &status,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&record).Error; err != nil {
		return fmt.Errorf("record assignment idempotency key: %w", err)
	}
	return nil
}

func recordAssignmentWorkflowEvent(
	tx *gorm.DB,
	action string,
	taskIDValue string,
	assignmentIDValue string,
	previous any,
	current any,
	requestID string,
	createdAt string,
) error {
	var previousJSON *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return fmt.Errorf("encode previous assignment workflow event: %w", err)
		}
		value := string(encoded)
		previousJSON = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("encode current assignment workflow event: %w", err)
	}
	currentJSON := string(encoded)
	actorIDValue := models.BuiltinOwnerActorID
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: taskIDValue,
		Action: action, ActorID: &actorIDValue, AssignmentID: &assignmentIDValue,
		RequestID: &requestID, PreviousJSON: previousJSON, CurrentJSON: &currentJSON,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record assignment workflow event: %w", err)
	}
	return nil
}

func closeActiveAssignmentsForCompletedTask(
	tx *gorm.DB,
	taskIDValue string,
	requestID string,
	closedAt string,
) error {
	var rows []assignmentRow
	if err := assignmentRowsQuery(tx).
		Where("assignment.task_id = ? AND assignment.unassigned_at IS NULL", taskIDValue).
		Order("assignment.role ASC").
		Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		previous := assignmentResponseFromRow(row)
		result := tx.Model(&models.TaskAssignment{}).
			Where("id = ? AND unassigned_at IS NULL", row.ID).
			Updates(map[string]any{"unassigned_at": closedAt, "reason": taskCompletedReason})
		if result.Error != nil {
			return mapAssignmentConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_NOT_ACTIVE", "The assignment is no longer active")
		}
		ended, err := loadAssignmentResponse(tx, row.ID)
		if err != nil {
			return err
		}
		if err := recordAssignmentWorkflowEvent(
			tx, "assignment_ended", taskIDValue, row.ID, previous, ended,
			requestID, closedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func mapAssignmentConstraintError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "ASSIGNMENT_ACTOR_NOT_ACTIVE"):
		return newProjectRequestError(http.StatusConflict, "ASSIGNMENT_ACTOR_NOT_ACTIVE", "The selected Actor is inactive")
	case strings.Contains(message, "ASSIGNMENT_REVIEWER_MUST_BE_OWNER"):
		return newProjectRequestError(http.StatusUnprocessableEntity, "ASSIGNMENT_REVIEWER_MUST_BE_OWNER", "The v0.1 reviewer must be the owner")
	case strings.Contains(message, "UNIQUE constraint failed: task_assignments.task_id, task_assignments.role"):
		return assignmentAlreadyActiveError()
	default:
		return err
	}
}

func writeAssignmentError(c *gin.Context, err error) {
	err = mapAssignmentConstraintError(err)
	if writeProjectRequestError(c, err) {
		return
	}
	writeDatabaseError(c)
}
