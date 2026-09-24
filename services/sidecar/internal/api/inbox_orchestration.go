package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const maxInboxSplitTasks = 20

type splitInboxTaskRequest struct {
	Key                string   `json:"key"`
	ParentKey          *string  `json:"parent_key"`
	Title              string   `json:"title"`
	Description        *string  `json:"description"`
	Kind               *string  `json:"kind"`
	Priority           *string  `json:"priority"`
	ProjectID          *string  `json:"project_id"`
	CompletionCriteria *string  `json:"completion_criteria"`
	TagIDs             []string `json:"tag_ids"`
	DueDate            *string  `json:"due_date"`
	PlannedDate        *string  `json:"planned_date"`
	EstimatedMinutes   *int     `json:"estimated_minutes"`
	ReviewPolicy       *string  `json:"review_policy"`
	IsRequired         *bool    `json:"is_required"`
	AssigneeActorID    string   `json:"assignee_actor_id"`
}

type splitInboxItemRequest struct {
	ResolutionPolicy *string                 `json:"resolution_policy"`
	Tasks            []splitInboxTaskRequest `json:"tasks"`
}

type normalizedInboxSplitTask struct {
	Key             string
	ParentKey       string
	Task            models.Task
	TagIDs          []string
	IsRequired      bool
	AssigneeActorID string
}

type normalizedInboxSplit struct {
	ResolutionPolicy string
	Tasks            []normalizedInboxSplitTask
}

type inboxSplitTaskOutput struct {
	Key         string                  `json:"key"`
	Task        models.Task             `json:"task"`
	Assignments []assignmentResponse    `json:"assignments"`
	Relation    inboxTaskRelationOutput `json:"relation"`
}

type splitInboxItemResponse struct {
	InboxItem inboxItemOutput        `json:"inbox_item"`
	Created   []inboxSplitTaskOutput `json:"created"`
	Progress  inboxTaskProgress      `json:"progress"`
}

type forceResolveInboxItemRequest struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason"`
}

func (a *API) splitInboxItem(c *gin.Context) {
	inboxID, ok := inboxItemID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input splitInboxItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	normalized, err := normalizeInboxSplit(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	hashInput := splitHashInput(normalized, expectedVersion)
	requestHash, err := inboxRequestHash(hashInput)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/inbox-items/%s/split", inboxID)
	statusCode := http.StatusCreated
	replayed := false
	var response splitInboxItemResponse
	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayInboxSnapshot(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		var splitErr error
		response, splitErr = splitInboxItemInTransaction(tx, inboxID, expectedVersion, normalized, requestIDFromContext(c), now)
		if splitErr != nil {
			return splitErr
		}
		return recordInboxSnapshot(tx, idempotencyKey, endpoint, inboxID, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, mapInboxTaskConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	for index := range response.Created {
		normalizeTask(&response.Created[index].Task)
	}
	setProjectETag(c, response.InboxItem.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func normalizeInboxSplit(input splitInboxItemRequest) (normalizedInboxSplit, error) {
	if len(input.Tasks) < 1 || len(input.Tasks) > maxInboxSplitTasks {
		return normalizedInboxSplit{}, fmt.Errorf("tasks must contain 1 to %d items", maxInboxSplitTasks)
	}
	policy := "all_required_tasks_done"
	if input.ResolutionPolicy != nil {
		policy = strings.TrimSpace(*input.ResolutionPolicy)
	}
	if policy != "manual" && policy != "all_required_tasks_done" {
		return normalizedInboxSplit{}, errors.New("resolution_policy must be manual or all_required_tasks_done")
	}
	result := normalizedInboxSplit{ResolutionPolicy: policy, Tasks: make([]normalizedInboxSplitTask, 0, len(input.Tasks))}
	seen := make(map[string]struct{}, len(input.Tasks))
	for index, raw := range input.Tasks {
		key := strings.TrimSpace(raw.Key)
		if utf8.RuneCountInString(key) < 1 || utf8.RuneCountInString(key) > 50 {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].key must contain 1 to 50 characters", index)
		}
		if _, exists := seen[key]; exists {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].key must be unique", index)
		}
		parentKey := ""
		if raw.ParentKey != nil {
			parentKey = strings.TrimSpace(*raw.ParentKey)
			if parentKey == key {
				return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].parent_key cannot reference itself", index)
			}
			if _, exists := seen[parentKey]; !exists {
				return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].parent_key must reference an earlier task key", index)
			}
		}
		if raw.IsRequired == nil {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].is_required is required", index)
		}
		actorID, err := validateAssignmentActorID(raw.AssigneeActorID)
		if err != nil {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d].assignee_actor_id %w", index, err)
		}
		task, err := taskFromCreateRequest(createTaskRequest{
			Title: raw.Title, Description: raw.Description, Kind: raw.Kind, Priority: raw.Priority,
			ProjectID: raw.ProjectID, CompletionCriteria: raw.CompletionCriteria, TagIDs: raw.TagIDs,
			DueDate: raw.DueDate, PlannedDate: raw.PlannedDate, EstimatedMinutes: raw.EstimatedMinutes,
			ReviewPolicy: raw.ReviewPolicy,
		})
		if err != nil {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d]: %w", index, err)
		}
		tagIDs, err := validateTaskTagIDs(raw.TagIDs)
		if err != nil {
			return normalizedInboxSplit{}, fmt.Errorf("tasks[%d]: %w", index, err)
		}
		seen[key] = struct{}{}
		result.Tasks = append(result.Tasks, normalizedInboxSplitTask{
			Key: key, ParentKey: parentKey, Task: task, TagIDs: tagIDs,
			IsRequired: *raw.IsRequired, AssigneeActorID: actorID,
		})
	}
	if policy == "all_required_tasks_done" {
		hasRequired := false
		for _, task := range result.Tasks {
			if task.IsRequired {
				hasRequired = true
				break
			}
		}
		if !hasRequired {
			return normalizedInboxSplit{}, errors.New("all_required_tasks_done requires at least one required task")
		}
	}
	return result, nil
}

func splitHashInput(input normalizedInboxSplit, expectedVersion int64) any {
	tasks := make([]map[string]any, len(input.Tasks))
	for index, entry := range input.Tasks {
		task := entry.Task
		tasks[index] = map[string]any{
			"key": entry.Key, "parent_key": entry.ParentKey, "title": task.Title,
			"description": task.Description, "kind": task.Kind, "priority": task.Priority,
			"project_id": task.ProjectID, "completion_criteria": task.CompletionCriteria,
			"tag_ids": entry.TagIDs, "due_date": task.DueDate, "planned_date": task.PlannedDate,
			"estimated_minutes": task.EstimatedMinutes, "review_policy": task.ReviewPolicy,
			"is_required": entry.IsRequired, "assignee_actor_id": entry.AssigneeActorID,
		}
	}
	return map[string]any{"expected_version": expectedVersion, "resolution_policy": input.ResolutionPolicy, "tasks": tasks}
}

func createSplitAssignment(tx *gorm.DB, taskIDValue, actorIDValue, role, requestID, now string, sequence int) (assignmentResponse, error) {
	if err := requireAssignmentActor(tx, actorIDValue, role); err != nil {
		return assignmentResponse{}, err
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: taskIDValue, ActorID: actorIDValue, Role: role,
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now,
	}
	if err := tx.Create(&assignment).Error; err != nil {
		return assignmentResponse{}, mapAssignmentConstraintError(err)
	}
	if err := tx.Model(&models.Task{}).Where("id = ?", taskIDValue).
		Updates(map[string]any{"version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
		return assignmentResponse{}, err
	}
	created, err := loadAssignmentResponse(tx, assignment.ID)
	if err != nil {
		return assignmentResponse{}, err
	}
	if err := recordAssignmentWorkflowEventWithSequence(
		tx, "assignment_created", taskIDValue, assignment.ID, nil, created, requestID, now, &sequence,
	); err != nil {
		return assignmentResponse{}, err
	}
	return created, nil
}

func recordSplitTaskCreatedEvent(tx *gorm.DB, task models.Task, inboxID, key, requestID, now string) error {
	current, err := json.Marshal(map[string]any{
		"source": "inbox_split", "inbox_item_id": inboxID, "split_key": key,
		"status": task.Status, "version": task.Version,
	})
	if err != nil {
		return err
	}
	currentText := string(current)
	ownerID := models.BuiltinOwnerActorID
	sequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: task.ID, Action: "task_created_from_inbox",
		ActorID: &ownerID, RequestID: &requestID, CommandSeq: &sequence, CurrentJSON: &currentText, CreatedAt: now,
	}
	return tx.Create(&event).Error
}

func splitInboxEventState(item models.InboxItem, created []inboxSplitTaskOutput, progress inboxTaskProgress) map[string]any {
	state := inboxItemEventState(item, "")
	tasks := make([]map[string]any, len(created))
	for index, entry := range created {
		tasks[index] = map[string]any{
			"key": entry.Key, "task_id": entry.Task.ID, "parent_task_id": entry.Task.ParentTaskID,
			"relation_id": entry.Relation.ID, "is_required": entry.Relation.IsRequired,
			"assignee_actor_id": entry.Assignments[0].ActorID,
		}
	}
	state["created_tasks"] = tasks
	state["progress"] = progress
	return state
}

func (a *API) forceResolveInboxItem(c *gin.Context) {
	id, ok := inboxItemID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input forceResolveInboxItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if !input.Confirm {
		writeError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm must be true to force-resolve an Inbox Item")
		return
	}
	reason, err := normalizeInboxReason(input.Reason, true)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	hash := map[string]any{"expected_version": expectedVersion, "confirm": true, "reason": reason}
	requestHash, err := inboxRequestHash(hash)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/inbox-items/%s/force-resolve", id)
	statusCode := http.StatusOK
	replayed := false
	var response inboxItemOutput
	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayInboxSnapshot(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		response, err = forceResolveInboxItemInTransaction(tx, id, expectedVersion, reason, input.Confirm, requestIDFromContext(c), now)
		if err != nil {
			return err
		}
		return recordInboxSnapshot(tx, idempotencyKey, endpoint, id, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, mapInboxTaskConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func reconcileInboxItemsForTask(tx *gorm.DB, taskIDValue, requestID, now string) error {
	var inboxIDs []string
	if err := tx.Model(&models.InboxItemTask{}).Distinct("inbox_item_id").
		Where("task_id = ? AND unlinked_at IS NULL AND is_required = 1", taskIDValue).
		Pluck("inbox_item_id", &inboxIDs).Error; err != nil {
		return err
	}
	for _, inboxID := range inboxIDs {
		if _, _, err := reconcileInboxItem(tx, inboxID, requestID, now); err != nil {
			return err
		}
	}
	return nil
}

func reconcileInboxItem(tx *gorm.DB, inboxID, requestID, now string) (models.InboxItem, inboxTaskProgress, error) {
	current, err := loadInboxItem(tx, inboxID)
	if err != nil {
		return models.InboxItem{}, inboxTaskProgress{}, err
	}
	progress, err := loadInboxTaskProgress(tx, inboxID)
	if err != nil {
		return models.InboxItem{}, inboxTaskProgress{}, err
	}
	if current.ResolutionPolicy != "all_required_tasks_done" {
		return current, progress, nil
	}
	shouldResolve := !inboxItemTerminal(current.Status) && progress.RequiredTotal > 0 && progress.AllRequiredDone
	shouldReopen := current.Status == "resolved" && current.ResolutionMode != nil && *current.ResolutionMode == "automatic" &&
		(progress.RequiredTotal == 0 || !progress.AllRequiredDone)
	if !shouldResolve && !shouldReopen {
		return current, progress, nil
	}
	next := current
	action := "automatically_resolved"
	if shouldResolve {
		systemID := models.BuiltinSystemActorID
		mode := "automatic"
		reason := "所有必需任务已完成"
		next.Status = "resolved"
		next.SnoozedUntil = nil
		next.ResolvedByActorID = &systemID
		next.ResolvedAt = &now
		next.ResolutionReason = &reason
		next.ResolutionMode = &mode
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
	} else {
		action = "automatically_reopened"
		next.Status = "open"
		if progress.ActiveTotal > 0 {
			next.Status = "tracking"
		}
		next.SnoozedUntil = nil
		next.ResolvedByActorID = nil
		next.ResolvedAt = nil
		next.ResolutionReason = nil
		next.ResolutionMode = nil
	}
	next.Version++
	next.UpdatedAt = now
	result := tx.Model(&models.InboxItem{}).Where("id = ? AND version = ?", inboxID, current.Version).Updates(inboxCommandUpdates(next))
	if result.Error != nil {
		return models.InboxItem{}, inboxTaskProgress{}, mapInboxTaskConstraintError(result.Error)
	}
	if result.RowsAffected != 1 {
		return models.InboxItem{}, inboxTaskProgress{}, inboxVersionConflict()
	}
	if err := recordInboxWorkflowEventAs(
		tx, inboxID, action, models.BuiltinSystemActorID,
		inboxItemEventState(current, ""), inboxItemEventState(next, ""), requestID, now,
	); err != nil {
		return models.InboxItem{}, inboxTaskProgress{}, err
	}
	return next, progress, nil
}

func recordInboxWorkflowEventAs(
	tx *gorm.DB,
	inboxID string,
	action string,
	actorID string,
	previous map[string]any,
	current map[string]any,
	requestID string,
	createdAt string,
) error {
	var previousText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		value := string(encoded)
		previousText = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return err
	}
	currentText := string(encoded)
	sequence := 1
	var requestIDValue *string
	if requestID != "" {
		requestIDValue = &requestID
	}
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "inbox_item", AggregateID: inboxID,
		Action: action, ActorID: &actorID, RequestID: requestIDValue, CommandSeq: &sequence,
		PreviousJSON: previousText, CurrentJSON: &currentText, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record Inbox orchestration workflow event: %w", err)
	}
	return nil
}
