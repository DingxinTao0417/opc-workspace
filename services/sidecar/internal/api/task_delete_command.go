package api

import (
	"errors"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// taskDeletionImpact is deliberately metadata-only. It is safe to persist in
// an approval preview and makes every destructive relationship visible without
// copying Artifact contents, local paths, or run output into AI history.
type taskDeletionImpact struct {
	ChildTasks     int64
	Submissions    int64
	Artifacts      int64
	FileArtifacts  int64
	Assignments    int64
	AgentRuns      int64
	FocusSessions  int64
	InboxRelations int64
	InboxSources   int64
	TagLinks       int64
}

func loadTaskDeletionImpact(tx *gorm.DB, id string, expectedVersion int64) (models.Task, taskDeletionImpact, error) {
	task, err := loadTask(tx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
	}
	if err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	if task.Version != expectedVersion {
		return models.Task{}, taskDeletionImpact{}, taskVersionConflict()
	}

	var activeAgentRun struct {
		OutputDeliveryStatus string `gorm:"column:output_delivery_status"`
	}
	activeRunErr := tx.Model(&models.AgentRun{}).
		Select("output_delivery_status").
		Where("task_id = ? AND status IN ?", id, []string{"queued", "running"}).
		Order("CASE WHEN output_delivery_status = 'pending' THEN 0 ELSE 1 END").
		Take(&activeAgentRun).Error
	if activeRunErr != nil && !errors.Is(activeRunErr, gorm.ErrRecordNotFound) {
		return models.Task{}, taskDeletionImpact{}, activeRunErr
	}
	if activeRunErr == nil {
		message := "Cancel the active Agent Run or wait for it to finish before deleting this Task"
		if activeAgentRun.OutputDeliveryStatus == agentRunOutputPending {
			message = "Retry the pending Agent Run output delivery before deleting this Task"
		}
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(http.StatusConflict, "TASK_HAS_ACTIVE_AGENT_RUN", message)
	}

	referenced, err := activeAgentRunReferencesProjectTaskSource(tx, id, "")
	if err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	if referenced {
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(http.StatusConflict, "TASK_HAS_ACTIVE_AGENT_RUN", "Wait for or cancel the successor Agent Run that uses this Task's accepted files before deleting the source Task")
	}
	var activeInboxRelations int64
	if err := tx.Model(&models.InboxItemTask{}).
		Where("task_id = ? AND unlinked_at IS NULL", id).
		Count(&activeInboxRelations).Error; err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	if activeInboxRelations > 0 {
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(
			http.StatusConflict,
			"TASK_HAS_ACTIVE_INBOX_RELATIONS",
			"Unlink the Task from active Inbox Items before deleting it",
		)
	}

	var openFocusSessions int64
	if err := tx.Model(&models.FocusSession{}).
		Where("task_id = ? AND status IN ?", id, []string{"active", "paused", "recovery_pending"}).
		Count(&openFocusSessions).Error; err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	if openFocusSessions > 0 {
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(
			http.StatusConflict,
			"TASK_HAS_OPEN_FOCUS_SESSION",
			"Stop, cancel, or recover the open Focus Session before deleting this task",
		)
	}

	var contentItemCount int64
	if err := tx.Table("content_item_tasks").Where("task_id = ?", id).Count(&contentItemCount).Error; err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	if contentItemCount > 0 {
		return models.Task{}, taskDeletionImpact{}, newProjectRequestError(
			http.StatusConflict,
			"TASK_CONTENT_ITEMS_EXIST",
			"Unlink the Task from Content Items before deleting it",
		)
	}

	impact := taskDeletionImpact{}
	counts := []struct {
		query *gorm.DB
		value *int64
	}{
		{tx.Model(&models.Task{}).Where("parent_task_id = ?", id), &impact.ChildTasks},
		{tx.Model(&models.TaskSubmission{}).Where("task_id = ?", id), &impact.Submissions},
		{tx.Model(&models.TaskArtifact{}).Where("task_id = ?", id), &impact.Artifacts},
		{tx.Model(&models.TaskArtifact{}).Where("task_id = ? AND storage_kind = 'file' AND deleted_at IS NULL", id), &impact.FileArtifacts},
		{tx.Model(&models.TaskAssignment{}).Where("task_id = ?", id), &impact.Assignments},
		{tx.Model(&models.AgentRun{}).Where("task_id = ?", id), &impact.AgentRuns},
		{tx.Model(&models.FocusSession{}).Where("task_id = ?", id), &impact.FocusSessions},
		{tx.Model(&models.InboxItemTask{}).Where("task_id = ?", id), &impact.InboxRelations},
		{tx.Table("task_tags").Where("task_id = ?", id), &impact.TagLinks},
	}
	for _, count := range counts {
		if err := count.query.Count(count.value).Error; err != nil {
			return models.Task{}, taskDeletionImpact{}, err
		}
	}
	artifactIDs := tx.Model(&models.TaskArtifact{}).Select("id").Where("task_id = ?", id)
	if err := tx.Table("inbox_items").
		Where("(source_entity_type IN ? AND source_entity_id = ?) OR (source_entity_type = ? AND source_entity_id IN (?))",
			[]string{taskBlockedInboxSourceType, taskDueInboxSourceType}, id, taskArtifactInboxSourceType, artifactIDs).
		Count(&impact.InboxSources).Error; err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	agentFailureSources, err := loadTaskAgentFailureInboxSources(tx, id)
	if err != nil {
		return models.Task{}, taskDeletionImpact{}, err
	}
	impact.InboxSources += int64(len(agentFailureSources))
	return task, impact, nil
}

func (a *API) deleteTaskInTransaction(
	tx *gorm.DB,
	id string,
	expectedVersion int64,
	requestID string,
	deletedAt string,
) (models.Task, taskDeletionImpact, []trashedArtifactFile, error) {
	task, impact, err := loadTaskDeletionImpact(tx, id, expectedVersion)
	if err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	if err := coordinateTaskBlockedInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	if err := coordinateTaskDueInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	if err := coordinateTaskAgentFailureInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	var sourceArtifactIDs []string
	if err := tx.Model(&models.TaskArtifact{}).
		Where("task_id = ?", id).
		Order("id ASC").
		Pluck("id", &sourceArtifactIDs).Error; err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	if err := coordinateTaskArtifactInboxSourceDeletion(
		tx,
		sourceArtifactIDs,
		"TASK_HAS_ACTIVE_INBOX_SOURCES",
		"Resolve or dismiss all Artifact follow-up Inbox Items before deleting this Task",
		requestID,
		deletedAt,
	); err != nil {
		return models.Task{}, taskDeletionImpact{}, nil, err
	}
	moved, err := a.trashTaskArtifactFiles(tx, id, deletedAt)
	if err != nil {
		return models.Task{}, taskDeletionImpact{}, moved, err
	}
	parentTaskID := task.ParentTaskID
	result := tx.Delete(&models.Task{}, "id = ?", id)
	if result.Error != nil {
		return models.Task{}, taskDeletionImpact{}, moved, result.Error
	}
	if result.RowsAffected == 0 {
		return models.Task{}, taskDeletionImpact{}, moved, taskVersionConflict()
	}
	if err := reconcileTaskParentChain(tx, parentTaskID, requestID, deletedAt); err != nil {
		return models.Task{}, taskDeletionImpact{}, moved, taskParentProgressError("reconcile deleted Task parent", err)
	}
	return task, impact, moved, nil
}

func (a *API) finishTaskDeletion(moved []trashedArtifactFile, err error) {
	if err != nil {
		if restoreErr := a.restoreTaskArtifactFiles(moved); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("Task delete Artifact compensation failed error=%v", restoreErr)
		}
		return
	}
	for _, file := range moved {
		a.artifactStore.purgeTrashedFile(file)
	}
}
