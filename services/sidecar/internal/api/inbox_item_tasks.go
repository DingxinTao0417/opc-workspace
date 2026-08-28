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

const maxActiveInboxTasks = 100

type inboxTaskProgress struct {
	ActiveTotal           int64 `json:"active_total"`
	RequiredTotal         int64 `json:"required_total"`
	RequiredDone          int64 `json:"required_done"`
	RequiredRemaining     int64 `json:"required_remaining"`
	RequiredBlocked       int64 `json:"required_blocked"`
	RequiredWaitingReview int64 `json:"required_waiting_review"`
	RequiredCancelled     int64 `json:"required_cancelled"`
	Percent               *int  `json:"percent"`
	AllRequiredDone       bool  `json:"all_required_done"`
}

type inboxTaskSummary struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	Kind        string  `json:"kind"`
	ProjectID   *string `json:"project_id"`
	ProjectName *string `json:"project_name"`
	Version     int64   `json:"version"`
}

type inboxTaskRelationOutput struct {
	ID                string                  `json:"id"`
	InboxItemID       string                  `json:"inbox_item_id"`
	TaskRefID         string                  `json:"task_ref_id"`
	TaskID            *string                 `json:"task_id"`
	TaskTitleSnapshot string                  `json:"task_title_snapshot"`
	Task              *inboxTaskSummary       `json:"task"`
	RelationType      string                  `json:"relation_type"`
	IsRequired        bool                    `json:"is_required"`
	Position          int                     `json:"position"`
	LinkedByActorID   string                  `json:"linked_by_actor_id"`
	LinkedByActor     assignmentActorSummary  `json:"linked_by_actor"`
	LinkedAt          string                  `json:"linked_at"`
	UnlinkedByActorID *string                 `json:"unlinked_by_actor_id"`
	UnlinkedByActor   *assignmentActorSummary `json:"unlinked_by_actor"`
	UnlinkedAt        *string                 `json:"unlinked_at"`
	UnlinkReason      *string                 `json:"unlink_reason"`
	IsActive          bool                    `json:"is_active"`
	TaskDeleted       bool                    `json:"task_deleted"`
}

type inboxTaskRelationRow struct {
	ID                     string  `gorm:"column:id"`
	InboxItemID            string  `gorm:"column:inbox_item_id"`
	TaskRefID              string  `gorm:"column:task_ref_id"`
	TaskID                 *string `gorm:"column:task_id"`
	TaskTitleSnapshot      string  `gorm:"column:task_title_snapshot"`
	RelationType           string  `gorm:"column:relation_type"`
	IsRequired             bool    `gorm:"column:is_required"`
	Position               int     `gorm:"column:position"`
	LinkedByActorID        string  `gorm:"column:linked_by_actor_id"`
	LinkedAt               string  `gorm:"column:linked_at"`
	UnlinkedByActorID      *string `gorm:"column:unlinked_by_actor_id"`
	UnlinkedAt             *string `gorm:"column:unlinked_at"`
	UnlinkReason           *string `gorm:"column:unlink_reason"`
	TaskTitle              *string `gorm:"column:task_title"`
	TaskStatus             *string `gorm:"column:task_status"`
	TaskPriority           *string `gorm:"column:task_priority"`
	TaskKind               *string `gorm:"column:task_kind"`
	TaskProjectID          *string `gorm:"column:task_project_id"`
	TaskProjectName        *string `gorm:"column:task_project_name"`
	TaskVersion            *int64  `gorm:"column:task_version"`
	LinkedActorType        string  `gorm:"column:linked_actor_type"`
	LinkedActorName        string  `gorm:"column:linked_actor_name"`
	LinkedActorStatus      string  `gorm:"column:linked_actor_status"`
	LinkedActorIsBuiltin   bool    `gorm:"column:linked_actor_is_builtin"`
	LinkedActorVersion     int64   `gorm:"column:linked_actor_version"`
	UnlinkedActorType      *string `gorm:"column:unlinked_actor_type"`
	UnlinkedActorName      *string `gorm:"column:unlinked_actor_name"`
	UnlinkedActorStatus    *string `gorm:"column:unlinked_actor_status"`
	UnlinkedActorIsBuiltin *bool   `gorm:"column:unlinked_actor_is_builtin"`
	UnlinkedActorVersion   *int64  `gorm:"column:unlinked_actor_version"`
}

type inboxTaskListData struct {
	Active  []inboxTaskRelationOutput `json:"active"`
	History []inboxTaskRelationOutput `json:"history"`
}

type inboxTaskListMeta struct {
	Page             int               `json:"page"`
	PageSize         int               `json:"page_size"`
	Total            int64             `json:"total"`
	InboxItemVersion int64             `json:"inbox_item_version"`
	Progress         inboxTaskProgress `json:"progress"`
}

type setInboxTaskRequiredRequest struct {
	IsRequired *bool `json:"is_required"`
}

type unlinkInboxTaskRequest struct {
	Reason string `json:"reason"`
}

type inboxTaskCommandHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	TaskID          string `json:"task_id"`
	IsRequired      *bool  `json:"is_required,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type inboxTaskMutationResponse struct {
	InboxItem inboxItemOutput         `json:"inbox_item"`
	Relation  inboxTaskRelationOutput `json:"relation"`
	Progress  inboxTaskProgress       `json:"progress"`
}

func (a *API) listInboxItemTasks(c *gin.Context) {
	inboxID, ok := inboxItemID(c)
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

	var item models.InboxItem
	var activeRows []inboxTaskRelationRow
	var historyRows []inboxTaskRelationRow
	var historyTotal int64
	var progress inboxTaskProgress
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&item, "id = ?", inboxID).Error; err != nil {
			return err
		}
		if err := inboxTaskRelationRowsQuery(tx).
			Where("relation.inbox_item_id = ? AND relation.unlinked_at IS NULL", inboxID).
			Order("relation.position ASC").Order("relation.linked_at ASC").Order("relation.id ASC").
			Limit(maxActiveInboxTasks + 1).
			Scan(&activeRows).Error; err != nil {
			return err
		}
		if len(activeRows) > maxActiveInboxTasks {
			return newProjectRequestError(http.StatusConflict, "INBOX_TASK_LIMIT_EXCEEDED", "Inbox Item has more active Task relations than this version supports")
		}
		historyQuery := tx.Model(&models.InboxItemTask{}).
			Where("inbox_item_id = ? AND unlinked_at IS NOT NULL", inboxID)
		if err := historyQuery.Count(&historyTotal).Error; err != nil {
			return err
		}
		if err := inboxTaskRelationRowsQuery(tx).
			Where("relation.inbox_item_id = ? AND relation.unlinked_at IS NOT NULL", inboxID).
			Order("relation.unlinked_at DESC").Order("relation.linked_at DESC").Order("relation.id DESC").
			Offset((page - 1) * pageSize).Limit(pageSize).
			Scan(&historyRows).Error; err != nil {
			return err
		}
		var err error
		progress, err = loadInboxTaskProgress(tx, inboxID)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "INBOX_ITEM_NOT_FOUND", "Inbox Item not found")
			return
		}
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	active, err := inboxTaskRelationOutputs(activeRows)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	history, err := inboxTaskRelationOutputs(historyRows)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, item.Version)
	c.JSON(http.StatusOK, gin.H{
		"data": inboxTaskListData{Active: active, History: history},
		"meta": inboxTaskListMeta{
			Page: page, PageSize: pageSize, Total: historyTotal,
			InboxItemVersion: item.Version, Progress: progress,
		},
	})
}

func (a *API) linkInboxItemTask(c *gin.Context) {
	a.executeInboxTaskMutation(c, "link")
}

func (a *API) updateInboxItemTask(c *gin.Context) {
	a.executeInboxTaskMutation(c, "requirement")
}

func (a *API) unlinkInboxItemTask(c *gin.Context) {
	a.executeInboxTaskMutation(c, "unlink")
}

func (a *API) executeInboxTaskMutation(c *gin.Context, command string) {
	inboxID, ok := inboxItemID(c)
	if !ok {
		return
	}
	taskIDValue, ok := inboxTaskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	payload, ok := decodeInboxTaskMutation(c, command, expectedVersion, taskIDValue)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := inboxTaskRequestHash(payload)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("%s /api/v1/inbox-items/%s/tasks/%s", strings.ToUpper(c.Request.Method), inboxID, taskIDValue)
	statusCode := http.StatusOK
	if command == "link" {
		statusCode = http.StatusCreated
	}
	replayed := false
	var response inboxTaskMutationResponse
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
		current, err := loadInboxItem(tx, inboxID)
		if err != nil {
			return inboxItemLoadError(err)
		}
		if current.Version != expectedVersion {
			return inboxVersionConflict()
		}
		if inboxItemTerminal(current.Status) {
			return inboxTerminalConflict("Archived Inbox Items must be reopened before changing Task relations")
		}
		switch command {
		case "link":
			return a.createInboxTaskRelation(tx, c, current, taskIDValue, *payload.IsRequired, idempotencyKey, endpoint, requestHash, now, nowText, statusCode, &response)
		case "requirement":
			return a.changeInboxTaskRequirement(tx, c, current, taskIDValue, *payload.IsRequired, idempotencyKey, endpoint, requestHash, now, nowText, statusCode, &response)
		case "unlink":
			return a.softUnlinkInboxTask(tx, c, current, taskIDValue, payload.Reason, idempotencyKey, endpoint, requestHash, now, nowText, statusCode, &response)
		default:
			return errors.New("unsupported Inbox Task command")
		}
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
	setProjectETag(c, response.InboxItem.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) createInboxTaskRelation(
	tx *gorm.DB,
	c *gin.Context,
	current models.InboxItem,
	taskIDValue string,
	isRequired bool,
	idempotencyKey string,
	endpoint string,
	requestHash string,
	now time.Time,
	nowText string,
	statusCode int,
	response *inboxTaskMutationResponse,
) error {
	var activeCount int64
	if err := tx.Model(&models.InboxItemTask{}).
		Where("inbox_item_id = ? AND unlinked_at IS NULL", current.ID).
		Count(&activeCount).Error; err != nil {
		return err
	}
	if activeCount >= maxActiveInboxTasks {
		return newProjectRequestError(http.StatusConflict, "INBOX_TASK_LIMIT_REACHED", "An Inbox Item cannot have more than 100 active Task relations")
	}
	var duplicateCount int64
	if err := tx.Model(&models.InboxItemTask{}).
		Where("inbox_item_id = ? AND task_ref_id = ? AND unlinked_at IS NULL", current.ID, taskIDValue).
		Count(&duplicateCount).Error; err != nil {
		return err
	}
	if duplicateCount > 0 {
		return inboxTaskAlreadyLinkedError()
	}
	task, err := loadInboxTaskSummary(tx, taskIDValue)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return err
	}
	var maxPosition int
	if err := tx.Raw(`
		SELECT COALESCE(MAX(position), 0)
		FROM inbox_item_tasks
		WHERE inbox_item_id = ? AND unlinked_at IS NULL
	`, current.ID).Scan(&maxPosition).Error; err != nil {
		return err
	}
	ownerID := models.BuiltinOwnerActorID
	relation := models.InboxItemTask{
		ID: uuid.NewString(), InboxItemID: current.ID,
		TaskRefID: taskIDValue, TaskID: &taskIDValue, TaskTitleSnapshot: task.Title,
		RelationType: "linked", IsRequired: isRequired, Position: maxPosition + 1,
		LinkedByActorID: ownerID, LinkedAt: nowText,
	}
	if err := tx.Create(&relation).Error; err != nil {
		return mapInboxTaskConstraintError(err)
	}
	updated, err := bumpInboxForTaskRelation(tx, current, "tracking", nowText)
	if err != nil {
		return err
	}
	relationOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
	if err != nil {
		return err
	}
	progress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return err
	}
	inboxOutput, err := inboxItemOutputFromModel(updated, now)
	if err != nil {
		return err
	}
	response.InboxItem = inboxOutput
	response.Relation = relationOutput
	response.Progress = progress
	if err := recordInboxTaskWorkflowEvent(
		tx, current.ID, "task_linked", nil,
		inboxTaskEventState(relationOutput, progress, "", updated.Status, updated.Version),
		requestIDFromContext(c), nowText,
	); err != nil {
		return err
	}
	return recordInboxSnapshot(tx, idempotencyKey, endpoint, relation.ID, requestHash, statusCode, response, nowText)
}

func (a *API) changeInboxTaskRequirement(
	tx *gorm.DB,
	c *gin.Context,
	current models.InboxItem,
	taskIDValue string,
	isRequired bool,
	idempotencyKey string,
	endpoint string,
	requestHash string,
	now time.Time,
	nowText string,
	statusCode int,
	response *inboxTaskMutationResponse,
) error {
	relation, err := loadActiveInboxTaskRelation(tx, current.ID, taskIDValue)
	if err != nil {
		return err
	}
	previousOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
	if err != nil {
		return err
	}
	previousProgress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return err
	}
	if relation.IsRequired == isRequired {
		inboxOutput, err := inboxItemOutputFromModel(current, now)
		if err != nil {
			return err
		}
		response.InboxItem = inboxOutput
		response.Relation = previousOutput
		response.Progress = previousProgress
		return recordInboxSnapshot(tx, idempotencyKey, endpoint, relation.ID, requestHash, statusCode, response, nowText)
	}
	result := tx.Model(&models.InboxItemTask{}).
		Where("id = ? AND unlinked_at IS NULL", relation.ID).
		Update("is_required", isRequired)
	if result.Error != nil {
		return mapInboxTaskConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return inboxTaskRelationNotActiveError()
	}
	updated, err := bumpInboxForTaskRelation(tx, current, current.Status, nowText)
	if err != nil {
		return err
	}
	currentOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
	if err != nil {
		return err
	}
	progress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return err
	}
	inboxOutput, err := inboxItemOutputFromModel(updated, now)
	if err != nil {
		return err
	}
	response.InboxItem = inboxOutput
	response.Relation = currentOutput
	response.Progress = progress
	if err := recordInboxTaskWorkflowEvent(
		tx, current.ID, "task_requirement_changed",
		inboxTaskEventState(previousOutput, previousProgress, "", current.Status, current.Version),
		inboxTaskEventState(currentOutput, progress, "", updated.Status, updated.Version),
		requestIDFromContext(c), nowText,
	); err != nil {
		return err
	}
	return recordInboxSnapshot(tx, idempotencyKey, endpoint, relation.ID, requestHash, statusCode, response, nowText)
}

func (a *API) softUnlinkInboxTask(
	tx *gorm.DB,
	c *gin.Context,
	current models.InboxItem,
	taskIDValue string,
	reason string,
	idempotencyKey string,
	endpoint string,
	requestHash string,
	now time.Time,
	nowText string,
	statusCode int,
	response *inboxTaskMutationResponse,
) error {
	relation, err := loadActiveInboxTaskRelation(tx, current.ID, taskIDValue)
	if err != nil {
		return err
	}
	previousOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
	if err != nil {
		return err
	}
	previousProgress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return err
	}
	ownerID := models.BuiltinOwnerActorID
	result := tx.Model(&models.InboxItemTask{}).
		Where("id = ? AND unlinked_at IS NULL", relation.ID).
		Updates(map[string]any{
			"unlinked_by_actor_id": ownerID,
			"unlinked_at":          nowText,
			"unlink_reason":        reason,
		})
	if result.Error != nil {
		return mapInboxTaskConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return inboxTaskRelationNotActiveError()
	}
	var remaining int64
	if err := tx.Model(&models.InboxItemTask{}).
		Where("inbox_item_id = ? AND unlinked_at IS NULL", current.ID).
		Count(&remaining).Error; err != nil {
		return err
	}
	nextStatus := "tracking"
	if remaining == 0 {
		nextStatus = "open"
	}
	updated, err := bumpInboxForTaskRelation(tx, current, nextStatus, nowText)
	if err != nil {
		return err
	}
	currentOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
	if err != nil {
		return err
	}
	progress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return err
	}
	inboxOutput, err := inboxItemOutputFromModel(updated, now)
	if err != nil {
		return err
	}
	response.InboxItem = inboxOutput
	response.Relation = currentOutput
	response.Progress = progress
	if err := recordInboxTaskWorkflowEvent(
		tx, current.ID, "task_unlinked",
		inboxTaskEventState(previousOutput, previousProgress, "", current.Status, current.Version),
		inboxTaskEventState(currentOutput, progress, reason, updated.Status, updated.Version),
		requestIDFromContext(c), nowText,
	); err != nil {
		return err
	}
	return recordInboxSnapshot(tx, idempotencyKey, endpoint, relation.ID, requestHash, statusCode, response, nowText)
}

func decodeInboxTaskMutation(c *gin.Context, command string, expectedVersion int64, taskIDValue string) (inboxTaskCommandHash, bool) {
	result := inboxTaskCommandHash{ExpectedVersion: expectedVersion, TaskID: taskIDValue}
	switch command {
	case "link", "requirement":
		var input setInboxTaskRequiredRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return inboxTaskCommandHash{}, false
		}
		if input.IsRequired == nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "is_required is required")
			return inboxTaskCommandHash{}, false
		}
		result.IsRequired = input.IsRequired
	case "unlink":
		var input unlinkInboxTaskRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return inboxTaskCommandHash{}, false
		}
		reason, err := validateAssignmentReason(input.Reason)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return inboxTaskCommandHash{}, false
		}
		result.Reason = reason
	default:
		writeError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found")
		return inboxTaskCommandHash{}, false
	}
	return result, true
}

func inboxTaskRequestHash(payload inboxTaskCommandHash) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("v1:%x", digest), nil
}

func inboxTaskID(c *gin.Context) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(c.Param("task_id")))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_TASK_ID", "Task id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func loadActiveInboxTaskRelation(tx *gorm.DB, inboxID, taskIDValue string) (models.InboxItemTask, error) {
	var relation models.InboxItemTask
	err := tx.Where("inbox_item_id = ? AND task_ref_id = ? AND unlinked_at IS NULL", inboxID, taskIDValue).
		Take(&relation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.InboxItemTask{}, newProjectRequestError(http.StatusNotFound, "INBOX_TASK_RELATION_NOT_FOUND", "Active Inbox Task relation not found")
	}
	return relation, err
}

func bumpInboxForTaskRelation(tx *gorm.DB, current models.InboxItem, status, now string) (models.InboxItem, error) {
	triagedAt := current.TriagedAt
	if triagedAt == nil {
		triagedAt = &now
	}
	result := tx.Model(&models.InboxItem{}).
		Where("id = ? AND version = ? AND status IN ('open', 'tracking')", current.ID, current.Version).
		Updates(map[string]any{
			"status": status, "triaged_at": triagedAt,
			"version": current.Version + 1, "updated_at": now,
		})
	if result.Error != nil {
		return models.InboxItem{}, result.Error
	}
	if result.RowsAffected == 0 {
		return models.InboxItem{}, inboxVersionConflict()
	}
	return loadInboxItem(tx, current.ID)
}

func inboxTaskRelationRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("inbox_item_tasks AS relation").Select(`
		relation.id,
		relation.inbox_item_id,
		relation.task_ref_id,
		relation.task_id,
		relation.task_title_snapshot,
		relation.relation_type,
		relation.is_required,
		relation.position,
		relation.linked_by_actor_id,
		relation.linked_at,
		relation.unlinked_by_actor_id,
		relation.unlinked_at,
		relation.unlink_reason,
		task.title AS task_title,
		task.status AS task_status,
		task.priority AS task_priority,
		task.kind AS task_kind,
		task.project_id AS task_project_id,
		project.name AS task_project_name,
		task.version AS task_version,
		linked_actor.type AS linked_actor_type,
		linked_actor.display_name AS linked_actor_name,
		linked_actor.status AS linked_actor_status,
		linked_actor.is_builtin AS linked_actor_is_builtin,
		linked_actor.version AS linked_actor_version,
		unlinked_actor.type AS unlinked_actor_type,
		unlinked_actor.display_name AS unlinked_actor_name,
		unlinked_actor.status AS unlinked_actor_status,
		unlinked_actor.is_builtin AS unlinked_actor_is_builtin,
		unlinked_actor.version AS unlinked_actor_version
	`).Joins("LEFT JOIN tasks AS task ON task.id = relation.task_id").
		Joins("LEFT JOIN projects AS project ON project.id = task.project_id").
		Joins("JOIN actors AS linked_actor ON linked_actor.id = relation.linked_by_actor_id").
		Joins("LEFT JOIN actors AS unlinked_actor ON unlinked_actor.id = relation.unlinked_by_actor_id")
}

func loadInboxTaskRelationOutput(db *gorm.DB, relationID string) (inboxTaskRelationOutput, error) {
	var row inboxTaskRelationRow
	if err := inboxTaskRelationRowsQuery(db).Where("relation.id = ?", relationID).Take(&row).Error; err != nil {
		return inboxTaskRelationOutput{}, err
	}
	return inboxTaskRelationOutputFromRow(row)
}

func inboxTaskRelationOutputs(rows []inboxTaskRelationRow) ([]inboxTaskRelationOutput, error) {
	outputs := make([]inboxTaskRelationOutput, len(rows))
	for index := range rows {
		output, err := inboxTaskRelationOutputFromRow(rows[index])
		if err != nil {
			return nil, err
		}
		outputs[index] = output
	}
	return outputs, nil
}

func inboxTaskRelationOutputFromRow(row inboxTaskRelationRow) (inboxTaskRelationOutput, error) {
	linkedActor := assignmentActorSummary{
		ID: row.LinkedByActorID, Type: row.LinkedActorType, DisplayName: row.LinkedActorName,
		Status: row.LinkedActorStatus, IsBuiltin: row.LinkedActorIsBuiltin, Version: row.LinkedActorVersion,
	}
	var unlinkedActor *assignmentActorSummary
	if row.UnlinkedByActorID != nil {
		if row.UnlinkedActorType == nil || row.UnlinkedActorName == nil || row.UnlinkedActorStatus == nil || row.UnlinkedActorIsBuiltin == nil || row.UnlinkedActorVersion == nil {
			return inboxTaskRelationOutput{}, errors.New("Inbox Task relation unlink Actor is missing")
		}
		unlinkedActor = &assignmentActorSummary{
			ID: *row.UnlinkedByActorID, Type: *row.UnlinkedActorType, DisplayName: *row.UnlinkedActorName,
			Status: *row.UnlinkedActorStatus, IsBuiltin: *row.UnlinkedActorIsBuiltin, Version: *row.UnlinkedActorVersion,
		}
	}
	var task *inboxTaskSummary
	if row.TaskID != nil {
		if row.TaskTitle == nil || row.TaskStatus == nil || row.TaskPriority == nil || row.TaskKind == nil || row.TaskVersion == nil {
			return inboxTaskRelationOutput{}, errors.New("Inbox Task relation live Task is missing")
		}
		task = &inboxTaskSummary{
			ID: *row.TaskID, Title: *row.TaskTitle, Status: *row.TaskStatus,
			Priority: *row.TaskPriority, Kind: *row.TaskKind,
			ProjectID: row.TaskProjectID, ProjectName: row.TaskProjectName, Version: *row.TaskVersion,
		}
	}
	linkedAt := normalizeTimestamp(row.LinkedAt)
	var unlinkedAt *string
	if row.UnlinkedAt != nil {
		normalized := normalizeTimestamp(*row.UnlinkedAt)
		unlinkedAt = &normalized
	}
	return inboxTaskRelationOutput{
		ID: row.ID, InboxItemID: row.InboxItemID, TaskRefID: row.TaskRefID, TaskID: row.TaskID,
		TaskTitleSnapshot: row.TaskTitleSnapshot, Task: task,
		RelationType: row.RelationType, IsRequired: row.IsRequired, Position: row.Position,
		LinkedByActorID: row.LinkedByActorID, LinkedByActor: linkedActor, LinkedAt: linkedAt,
		UnlinkedByActorID: row.UnlinkedByActorID, UnlinkedByActor: unlinkedActor,
		UnlinkedAt: unlinkedAt, UnlinkReason: row.UnlinkReason,
		IsActive: row.UnlinkedAt == nil, TaskDeleted: row.TaskID == nil,
	}, nil
}

func loadInboxTaskSummary(db *gorm.DB, taskIDValue string) (inboxTaskSummary, error) {
	var task inboxTaskSummary
	err := db.Table("tasks AS task").Select(`
		task.id,
		task.title,
		task.status,
		task.priority,
		task.kind,
		task.project_id,
		project.name AS project_name,
		task.version
	`).Joins("LEFT JOIN projects AS project ON project.id = task.project_id").
		Where("task.id = ?", taskIDValue).Take(&task).Error
	return task, err
}

func loadInboxTaskProgress(db *gorm.DB, inboxID string) (inboxTaskProgress, error) {
	totals, err := loadInboxTaskProgressByInboxIDs(db, []string{inboxID})
	if err != nil {
		return inboxTaskProgress{}, err
	}
	return totals[inboxID], nil
}

func loadInboxTaskProgressByInboxIDs(db *gorm.DB, inboxIDs []string) (map[string]inboxTaskProgress, error) {
	result := make(map[string]inboxTaskProgress, len(inboxIDs))
	if len(inboxIDs) == 0 {
		return result, nil
	}
	for _, id := range inboxIDs {
		result[id] = inboxTaskProgress{}
	}
	type progressRow struct {
		InboxItemID           string `gorm:"column:inbox_item_id"`
		ActiveTotal           int64  `gorm:"column:active_total"`
		RequiredTotal         int64  `gorm:"column:required_total"`
		RequiredDone          int64  `gorm:"column:required_done"`
		RequiredBlocked       int64  `gorm:"column:required_blocked"`
		RequiredWaitingReview int64  `gorm:"column:required_waiting_review"`
		RequiredCancelled     int64  `gorm:"column:required_cancelled"`
	}
	var rows []progressRow
	if err := db.Table("inbox_item_tasks AS relation").Select(`
		relation.inbox_item_id,
		COUNT(*) AS active_total,
		COALESCE(SUM(CASE WHEN relation.is_required = 1 THEN 1 ELSE 0 END), 0) AS required_total,
		COALESCE(SUM(CASE WHEN relation.is_required = 1 AND task.status = 'done' THEN 1 ELSE 0 END), 0) AS required_done,
		COALESCE(SUM(CASE WHEN relation.is_required = 1 AND task.status = 'blocked' THEN 1 ELSE 0 END), 0) AS required_blocked,
		COALESCE(SUM(CASE WHEN relation.is_required = 1 AND task.status = 'waiting_review' THEN 1 ELSE 0 END), 0) AS required_waiting_review,
		COALESCE(SUM(CASE WHEN relation.is_required = 1 AND task.status = 'cancelled' THEN 1 ELSE 0 END), 0) AS required_cancelled
	`).Joins("JOIN tasks AS task ON task.id = relation.task_id").
		Where("relation.inbox_item_id IN ? AND relation.unlinked_at IS NULL", inboxIDs).
		Group("relation.inbox_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		progress := inboxTaskProgress{
			ActiveTotal: row.ActiveTotal, RequiredTotal: row.RequiredTotal,
			RequiredDone: row.RequiredDone, RequiredRemaining: row.RequiredTotal - row.RequiredDone,
			RequiredBlocked: row.RequiredBlocked, RequiredWaitingReview: row.RequiredWaitingReview,
			RequiredCancelled: row.RequiredCancelled,
		}
		if progress.RequiredTotal > 0 {
			percent := int(progress.RequiredDone * 100 / progress.RequiredTotal)
			progress.Percent = &percent
			progress.AllRequiredDone = progress.RequiredDone == progress.RequiredTotal
		}
		result[row.InboxItemID] = progress
	}
	return result, nil
}

func recordInboxTaskWorkflowEvent(tx *gorm.DB, inboxID, action string, previous, current any, requestID, createdAt string) error {
	var previousText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return fmt.Errorf("encode previous Inbox Task relation state: %w", err)
		}
		value := string(encoded)
		previousText = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("encode current Inbox Task relation state: %w", err)
	}
	currentText := string(encoded)
	ownerID := models.BuiltinOwnerActorID
	commandSequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "inbox_item", AggregateID: inboxID,
		Action: action, ActorID: &ownerID, RequestID: &requestID, CommandSeq: &commandSequence,
		PreviousJSON: previousText, CurrentJSON: &currentText, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record Inbox Task relation workflow event: %w", err)
	}
	return nil
}

func inboxTaskEventState(
	relation inboxTaskRelationOutput,
	progress inboxTaskProgress,
	reason string,
	inboxStatus string,
	inboxVersion int64,
) map[string]any {
	state := map[string]any{
		"relation": relation, "progress": progress,
		"inbox_status": inboxStatus, "inbox_version": inboxVersion,
	}
	if reason != "" {
		state["reason"] = reason
	}
	return state
}

func inboxTaskAlreadyLinkedError() error {
	return newProjectRequestError(http.StatusConflict, "INBOX_TASK_ALREADY_LINKED", "Task is already actively linked to this Inbox Item")
}

func inboxTaskRelationNotActiveError() error {
	return newProjectRequestError(http.StatusConflict, "INBOX_TASK_RELATION_NOT_ACTIVE", "Inbox Task relation is no longer active")
}

func mapInboxTaskConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "TASK_HAS_ACTIVE_INBOX_RELATIONS"):
		return newProjectRequestError(http.StatusConflict, "TASK_HAS_ACTIVE_INBOX_RELATIONS", "Unlink the Task from active Inbox Items before deleting it")
	case strings.Contains(message, "INBOX_ITEM_TERMINAL"):
		return inboxTerminalConflict("Archived Inbox Items must be reopened before changing Task relations")
	case strings.Contains(message, "INBOX_TASK_NOT_FOUND"):
		return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
	case strings.Contains(message, "INBOX_RELATION_ACTOR_NOT_ACTIVE"):
		return newProjectRequestError(http.StatusConflict, "ACTOR_NOT_ACTIVE", "The relation Actor is inactive")
	case strings.Contains(message, "UNIQUE constraint failed: inbox_item_tasks.inbox_item_id, inbox_item_tasks.task_ref_id"):
		return inboxTaskAlreadyLinkedError()
	case strings.Contains(message, "UNIQUE constraint failed: inbox_item_tasks.inbox_item_id, inbox_item_tasks.position"):
		return newProjectRequestError(http.StatusConflict, "INBOX_TASK_POSITION_CONFLICT", "Inbox Task relation order changed; reload before retrying")
	case strings.Contains(message, "INBOX_TASK_RELATION_HISTORY_IMMUTABLE"):
		return inboxTaskRelationNotActiveError()
	default:
		return err
	}
}
