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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	taskLifecycleStart    = "start"
	taskLifecycleBlock    = "block"
	taskLifecycleUnblock  = "unblock"
	taskLifecycleComplete = "complete"
	taskLifecycleCancel   = "cancel"
	taskLifecycleReopen   = "reopen"
)

var taskLifecycleActions = map[string]string{
	taskLifecycleStart:    "task_started",
	taskLifecycleBlock:    "task_blocked",
	taskLifecycleUnblock:  "task_unblocked",
	taskLifecycleComplete: "task_completed",
	taskLifecycleCancel:   "task_cancelled",
	taskLifecycleReopen:   "task_reopened",
}

type taskLifecycleReasonRequest struct {
	Reason *string `json:"reason"`
}

type emptyTaskLifecycleRequest struct{}

func (*emptyTaskLifecycleRequest) UnmarshalJSON(data []byte) error {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("request body must be an object")
	}
	if len(value) != 0 {
		return errors.New("request body cannot contain fields")
	}
	return nil
}

type taskLifecycleCommandHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type taskLifecycleResponse struct {
	Task  models.Task             `json:"task"`
	Event taskWorkflowEventOutput `json:"event"`
}

type taskWorkflowEventOutput struct {
	ID           string                  `json:"id"`
	Action       string                  `json:"action"`
	Actor        *assignmentActorSummary `json:"actor"`
	AssignmentID *string                 `json:"assignment_id"`
	SubmissionID *string                 `json:"submission_id"`
	ArtifactID   *string                 `json:"artifact_id"`
	RequestID    *string                 `json:"request_id"`
	CommandSeq   *int                    `json:"command_seq"`
	Previous     map[string]any          `json:"previous"`
	Current      map[string]any          `json:"current"`
	Reason       *string                 `json:"reason"`
	CreatedAt    string                  `json:"created_at"`
}

type taskWorkflowEventRow struct {
	ID             string  `gorm:"column:id"`
	Action         string  `gorm:"column:action"`
	ActorID        *string `gorm:"column:actor_id"`
	AssignmentID   *string `gorm:"column:assignment_id"`
	SubmissionID   *string `gorm:"column:submission_id"`
	ArtifactID     *string `gorm:"column:artifact_id"`
	RequestID      *string `gorm:"column:request_id"`
	CommandSeq     *int    `gorm:"column:command_seq"`
	PreviousJSON   *string `gorm:"column:previous_json"`
	CurrentJSON    *string `gorm:"column:current_json"`
	CreatedAt      string  `gorm:"column:created_at"`
	ActorType      *string `gorm:"column:actor_type"`
	ActorName      *string `gorm:"column:actor_display_name"`
	ActorStatus    *string `gorm:"column:actor_status"`
	ActorIsBuiltin *bool   `gorm:"column:actor_is_builtin"`
	ActorVersion   *int64  `gorm:"column:actor_version"`
}

type taskWorkflowEventMeta struct {
	Page        int   `json:"page"`
	PageSize    int   `json:"page_size"`
	Total       int64 `json:"total"`
	TaskVersion int64 `json:"task_version"`
}

func (a *API) startTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleStart)
}

func (a *API) blockTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleBlock)
}

func (a *API) unblockTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleUnblock)
}

func (a *API) completeTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleComplete)
}

func (a *API) cancelTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleCancel)
}

func (a *API) reopenTask(c *gin.Context) {
	a.executeTaskLifecycle(c, taskLifecycleReopen)
}

func (a *API) executeTaskLifecycle(c *gin.Context, command string) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	reason, ok := taskLifecycleReason(c, command)
	if !ok {
		return
	}
	idempotencyKey, requestHash, ok := taskLifecycleIdempotencyInput(c, taskLifecycleCommandHash{
		ExpectedVersion: expectedVersion,
		Reason:          reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/%s", taskIDValue, command)

	statusCode := http.StatusOK
	replayed := false
	var response taskLifecycleResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayedResponse, replayStatus, replayErr := replayTaskLifecycleCommand(
			tx, idempotencyKey, endpoint, requestHash, &response,
		)
		if replayErr != nil {
			return replayErr
		}
		if replayedResponse {
			replayed = true
			statusCode = replayStatus
			return nil
		}

		var current models.Task
		if err := tx.First(&current, "id = ?", taskIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		if current.Version != expectedVersion {
			return taskVersionConflict()
		}
		normalizeTask(&current)
		if err := validateTaskLifecycleTransition(tx, current, command); err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		requestID := requestIDFromContext(c)
		updated, event, err := applyValidatedTaskLifecycleTransition(
			tx, current, command, reason, requestID, now,
		)
		if err != nil {
			return err
		}
		if command == taskLifecycleComplete || command == taskLifecycleCancel || command == taskLifecycleReopen {
			if err := reconcileTaskParentChain(tx, updated.ParentTaskID, requestID, now); err != nil {
				return taskParentProgressError("reconcile lifecycle Task parent", err)
			}
		}
		if command == taskLifecycleStart || command == taskLifecycleUnblock {
			updated, err = reconcileTaskParentProgress(tx, updated.ID, requestID, now)
			if err != nil {
				return taskParentProgressError("reconcile lifecycle parent Task", err)
			}
		}
		response = taskLifecycleResponse{Task: updated, Event: event}
		return recordTaskLifecycleIdempotency(
			tx,
			idempotencyKey,
			endpoint,
			taskIDValue,
			requestHash,
			statusCode,
			response,
			now,
		)
	})
	if err != nil {
		if writeProjectRequestError(c, mapTaskWorkflowConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response.Task)
	response.Event.CreatedAt = normalizeTimestamp(response.Event.CreatedAt)
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func applyValidatedTaskLifecycleTransition(
	tx *gorm.DB,
	current models.Task,
	command string,
	reason string,
	requestID string,
	now string,
) (models.Task, taskWorkflowEventOutput, error) {
	updates, closeReason, err := taskLifecycleUpdates(current, command, reason, now)
	if err != nil {
		return models.Task{}, taskWorkflowEventOutput{}, err
	}
	commandSequence := 1
	if command == taskLifecycleCancel && taskHasPendingReview(current) {
		commandSequence, err = withdrawCurrentSubmissionForCancellation(
			tx, current, requestID, now, reason, commandSequence,
		)
		if err != nil {
			return models.Task{}, taskWorkflowEventOutput{}, err
		}
	}
	if closeReason != "" {
		commandSequence, err = closeActiveAssignmentsForTerminalTask(
			tx, current.ID, requestID, now, closeReason, commandSequence,
		)
		if err != nil {
			return models.Task{}, taskWorkflowEventOutput{}, err
		}
	}

	updates["updated_at"] = now
	updates["version"] = gorm.Expr("version + 1")
	result := tx.Model(&models.Task{}).
		Where("id = ? AND version = ?", current.ID, current.Version).
		Updates(updates)
	if result.Error != nil {
		return models.Task{}, taskWorkflowEventOutput{}, mapTaskWorkflowConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Task{}, taskWorkflowEventOutput{}, taskVersionConflict()
	}

	updated, err := loadTask(tx, current.ID)
	if err != nil {
		return models.Task{}, taskWorkflowEventOutput{}, err
	}
	event, err := recordTaskLifecycleEvent(
		tx,
		taskLifecycleActions[command],
		current.ID,
		taskLifecycleSnapshot(current, ""),
		taskLifecycleSnapshot(updated, reasonForTaskEvent(command, reason)),
		requestID,
		now,
		commandSequence,
	)
	if err != nil {
		return models.Task{}, taskWorkflowEventOutput{}, err
	}
	if command == taskLifecycleBlock {
		if err := projectTaskBlockedInboxItem(tx, updated, requestID, now); err != nil {
			return models.Task{}, taskWorkflowEventOutput{}, err
		}
	}
	if err := reconcileInboxItemsForTask(tx, current.ID, requestID, now); err != nil {
		return models.Task{}, taskWorkflowEventOutput{}, err
	}
	return updated, event, nil
}

func taskLifecycleReason(c *gin.Context, command string) (string, bool) {
	if command == taskLifecycleBlock || command == taskLifecycleCancel {
		var input taskLifecycleReasonRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
			return "", false
		}
		if input.Reason == nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason is required")
			return "", false
		}
		reason, err := validateAssignmentReason(*input.Reason)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return "", false
		}
		return reason, true
	}
	var input emptyTaskLifecycleRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return "", false
	}
	return "", true
}

func validateTaskLifecycleTransition(tx *gorm.DB, task models.Task, command string) error {
	allowed := false
	switch command {
	case taskLifecycleStart:
		allowed = task.Status == "todo"
	case taskLifecycleBlock:
		allowed = task.Status == "todo" || task.Status == "in_progress" || task.Status == "waiting_review"
	case taskLifecycleUnblock:
		allowed = task.Status == "blocked"
	case taskLifecycleComplete:
		allowed = task.Status == "todo" || task.Status == "in_progress"
	case taskLifecycleCancel:
		allowed = task.Status == "todo" || task.Status == "in_progress" || task.Status == "blocked" || task.Status == "waiting_review"
	case taskLifecycleReopen:
		allowed = task.Status == "done" || task.Status == "cancelled"
	}
	if !allowed {
		return newProjectRequestError(
			http.StatusConflict,
			"TASK_TRANSITION_NOT_ALLOWED",
			fmt.Sprintf("The %s command is not allowed from %s status", command, task.Status),
		)
	}
	if command == taskLifecycleStart {
		var count int64
		if err := tx.Table("task_assignments AS assignment").
			Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
			Where("assignment.task_id = ? AND assignment.role = 'assignee' AND assignment.unassigned_at IS NULL", task.ID).
			Where("actor.status = 'active'").
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return newProjectRequestError(http.StatusConflict, "TASK_ASSIGNEE_REQUIRED", "An active assignee is required before starting the task")
		}
	}
	if command == taskLifecycleComplete && task.ReviewPolicy != "none" {
		return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_REQUIRED", "This task must be submitted and reviewed before completion")
	}
	return nil
}

func taskLifecycleUpdates(task models.Task, command, reason, now string) (map[string]any, string, error) {
	updates := make(map[string]any)
	closeReason := ""
	switch command {
	case taskLifecycleStart:
		updates["status"] = "in_progress"
	case taskLifecycleBlock:
		updates["status"] = "blocked"
		updates["blocked_reason"] = reason
		updates["blocked_at"] = now
		updates["blocked_from_status"] = task.Status
	case taskLifecycleUnblock:
		if task.BlockedFromStatus == nil {
			return nil, "", newProjectRequestError(http.StatusConflict, "TASK_BLOCK_STATE_INVALID", "The blocked task does not have a restorable status")
		}
		updates["status"] = *task.BlockedFromStatus
		updates["blocked_reason"] = nil
		updates["blocked_at"] = nil
		updates["blocked_from_status"] = nil
	case taskLifecycleComplete:
		updates["status"] = "done"
		updates["completed_at"] = now
		closeReason = taskCompletedReason
	case taskLifecycleCancel:
		updates["status"] = "cancelled"
		updates["completed_at"] = nil
		updates["blocked_reason"] = nil
		updates["blocked_at"] = nil
		updates["blocked_from_status"] = nil
		closeReason = taskCancelledReason
	case taskLifecycleReopen:
		updates["status"] = "todo"
		updates["completed_at"] = nil
		updates["blocked_reason"] = nil
		updates["blocked_at"] = nil
		updates["blocked_from_status"] = nil
		updates["submitted_at"] = nil
		updates["reviewed_at"] = nil
		updates["current_submission_id"] = nil
	default:
		return nil, "", errors.New("unsupported task lifecycle command")
	}
	return updates, closeReason, nil
}

func reasonForTaskEvent(command, reason string) string {
	if command == taskLifecycleBlock || command == taskLifecycleCancel {
		return reason
	}
	return ""
}

func taskLifecycleSnapshot(task models.Task, reason string) map[string]any {
	snapshot := map[string]any{
		"status":                task.Status,
		"review_policy":         task.ReviewPolicy,
		"blocked_reason":        task.BlockedReason,
		"blocked_at":            task.BlockedAt,
		"blocked_from_status":   task.BlockedFromStatus,
		"completed_at":          task.CompletedAt,
		"submitted_at":          task.SubmittedAt,
		"reviewed_at":           task.ReviewedAt,
		"current_submission_id": task.CurrentSubmissionID,
		"version":               task.Version,
	}
	if reason != "" {
		snapshot["reason"] = reason
	}
	return snapshot
}

func taskLifecycleIdempotencyInput(c *gin.Context, payload taskLifecycleCommandHash) (string, string, bool) {
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

func replayTaskLifecycleCommand(
	tx *gorm.DB,
	key string,
	endpoint string,
	requestHash string,
	response *taskLifecycleResponse,
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
		return false, 0, fmt.Errorf("read task lifecycle idempotency key: %w", err)
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
			"Idempotency-Key was already used with a different task lifecycle request",
		)
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, fmt.Errorf("decode idempotent task lifecycle response: %w", err)
	}
	return true, *existing.ResponseStatus, nil
}

func recordTaskLifecycleIdempotency(
	tx *gorm.DB,
	key string,
	endpoint string,
	resourceID string,
	requestHash string,
	status int,
	response taskLifecycleResponse,
	createdAt string,
) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotent task lifecycle response: %w", err)
	}
	responseText := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID,
		RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &status,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&record).Error; err != nil {
		return fmt.Errorf("record task lifecycle idempotency key: %w", err)
	}
	return nil
}

func recordTaskLifecycleEvent(
	tx *gorm.DB,
	action string,
	taskIDValue string,
	previous map[string]any,
	current map[string]any,
	requestID string,
	createdAt string,
	commandSequence int,
) (taskWorkflowEventOutput, error) {
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return taskWorkflowEventOutput{}, fmt.Errorf("encode previous task workflow state: %w", err)
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return taskWorkflowEventOutput{}, fmt.Errorf("encode current task workflow state: %w", err)
	}
	previousText := string(previousJSON)
	currentText := string(currentJSON)
	actorID := models.BuiltinOwnerActorID
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: taskIDValue,
		Action: action, ActorID: &actorID, RequestID: &requestID, CommandSeq: &commandSequence,
		PreviousJSON: &previousText, CurrentJSON: &currentText, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return taskWorkflowEventOutput{}, fmt.Errorf("record task workflow event: %w", err)
	}
	return loadTaskWorkflowEvent(tx, event.ID)
}

func (a *API) listTaskWorkflowEvents(c *gin.Context) {
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

	var task models.Task
	var rows []taskWorkflowEventRow
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&task, "id = ?", taskIDValue).Error; err != nil {
			return err
		}
		base := tx.Model(&models.WorkflowEvent{}).
			Where("aggregate_type = 'task' AND aggregate_id = ?", taskIDValue)
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		return taskWorkflowEventRowsQuery(tx).
			Where("event.aggregate_type = 'task' AND event.aggregate_id = ?", taskIDValue).
			Order("julianday(event.created_at) DESC").
			Order("event.command_seq DESC").
			Order("event.id DESC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	events := make([]taskWorkflowEventOutput, len(rows))
	for index, row := range rows {
		event, err := taskWorkflowEventOutputFromRow(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		events[index] = event
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{
		"data": events,
		"meta": taskWorkflowEventMeta{Page: page, PageSize: pageSize, Total: total, TaskVersion: task.Version},
	})
}

func taskWorkflowEventRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("workflow_events AS event").
		Select(`
			event.id,
			event.action,
			event.actor_id,
			event.assignment_id,
			event.submission_id,
			event.artifact_id,
			event.request_id,
			event.command_seq,
			event.previous_json,
			event.current_json,
			event.created_at,
			actor.type AS actor_type,
			actor.display_name AS actor_display_name,
			actor.status AS actor_status,
			actor.is_builtin AS actor_is_builtin,
			actor.version AS actor_version
		`).
		Joins("LEFT JOIN actors AS actor ON actor.id = event.actor_id")
}

func loadTaskWorkflowEvent(db *gorm.DB, eventID string) (taskWorkflowEventOutput, error) {
	var row taskWorkflowEventRow
	if err := taskWorkflowEventRowsQuery(db).Where("event.id = ?", eventID).Take(&row).Error; err != nil {
		return taskWorkflowEventOutput{}, err
	}
	return taskWorkflowEventOutputFromRow(row)
}

func taskWorkflowEventOutputFromRow(row taskWorkflowEventRow) (taskWorkflowEventOutput, error) {
	previous, err := decodeWorkflowEventObject(row.PreviousJSON)
	if err != nil {
		return taskWorkflowEventOutput{}, err
	}
	current, err := decodeWorkflowEventObject(row.CurrentJSON)
	if err != nil {
		return taskWorkflowEventOutput{}, err
	}
	var actor *assignmentActorSummary
	if row.ActorID != nil {
		if row.ActorType == nil || row.ActorName == nil || row.ActorStatus == nil || row.ActorIsBuiltin == nil || row.ActorVersion == nil {
			return taskWorkflowEventOutput{}, errors.New("workflow event actor is missing")
		}
		actor = &assignmentActorSummary{
			ID: *row.ActorID, Type: *row.ActorType, DisplayName: *row.ActorName,
			Status: *row.ActorStatus, IsBuiltin: *row.ActorIsBuiltin, Version: *row.ActorVersion,
		}
	}
	reason := taskWorkflowEventReason(current)
	return taskWorkflowEventOutput{
		ID: row.ID, Action: row.Action, Actor: actor, AssignmentID: row.AssignmentID,
		SubmissionID: row.SubmissionID, ArtifactID: row.ArtifactID,
		RequestID: row.RequestID, CommandSeq: row.CommandSeq,
		Previous: previous, Current: current, Reason: reason,
		CreatedAt: normalizeTimestamp(row.CreatedAt),
	}, nil
}

func taskWorkflowEventReason(current map[string]any) *string {
	if text, valid := current["reason"].(string); valid && text != "" {
		return &text
	}
	// Reassignment snapshots wrap the ended Assignment so both the old and
	// new records remain available to the timeline. Expose its required reason
	// through the same top-level response field used by other event actions.
	endedAssignment, valid := current["ended_assignment"].(map[string]any)
	if !valid {
		return nil
	}
	if text, valid := endedAssignment["reason"].(string); valid && text != "" {
		return &text
	}
	return nil
}

func decodeWorkflowEventObject(value *string) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(*value), &object); err != nil {
		return nil, fmt.Errorf("decode workflow event JSON: %w", err)
	}
	return object, nil
}

func mapTaskWorkflowConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "TASK_TRANSITION_NOT_ALLOWED"):
		return newProjectRequestError(http.StatusConflict, "TASK_TRANSITION_NOT_ALLOWED", "The requested task lifecycle transition is not allowed")
	case strings.Contains(message, "TASK_HAS_ACTIVE_ASSIGNMENTS"):
		return newProjectRequestError(http.StatusConflict, "TASK_HAS_ACTIVE_ASSIGNMENTS", "Active assignments must be ended before entering a terminal status")
	case strings.Contains(message, "TASK_NOT_ASSIGNABLE"):
		return newProjectRequestError(http.StatusConflict, "TASK_NOT_ASSIGNABLE", "Terminal tasks cannot be assigned or reassigned")
	default:
		return err
	}
}
