package api

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	taskSubmissionOriginManual      = "manual"
	taskSubmissionOriginChildRollup = "child_rollup"

	childRollupSummary           = "所有非取消子任务已完成，等待父任务验收。"
	childRollupInvalidatedReason = "child_rollup_invalidated"
	childRollupGateLostReason    = "child_rollup_gate_lost"
)

type taskChildProgress struct {
	Total     int64 `gorm:"column:total"`
	Completed int64 `gorm:"column:completed"`
	Cancelled int64 `gorm:"column:cancelled"`
}

func (progress taskChildProgress) nonCancelled() int64 {
	return progress.Total - progress.Cancelled
}

func (progress taskChildProgress) readyForReview() bool {
	nonCancelled := progress.nonCancelled()
	return nonCancelled > 0 && progress.Completed == nonCancelled
}

func taskChildProgressSnapshot(progress taskChildProgress) map[string]any {
	return map[string]any{
		"subtask_total":         progress.Total,
		"subtask_completed":     progress.Completed,
		"subtask_cancelled":     progress.Cancelled,
		"subtask_non_cancelled": progress.nonCancelled(),
	}
}

// reconcileTaskParentChain recalculates direct-parent rollups from the final
// facts in the current transaction. Walking ancestors is necessary when an
// accepted child-rollup parent is reopened: that done -> todo transition can
// invalidate its own parent in the same command.
func reconcileTaskParentChain(
	tx *gorm.DB,
	parentTaskID *string,
	requestID string,
	now string,
) error {
	visited := make(map[string]struct{})
	currentID := parentTaskID
	for currentID != nil {
		id := *currentID
		if _, exists := visited[id]; exists {
			return errors.New("task parent reconciliation encountered a cycle")
		}
		visited[id] = struct{}{}

		parent, err := reconcileTaskParentProgress(tx, id, requestID, now)
		if err != nil {
			return err
		}
		currentID = parent.ParentTaskID
	}
	return nil
}

func reconcileTaskParentProgress(
	tx *gorm.DB,
	parentTaskID string,
	requestID string,
	now string,
) (models.Task, error) {
	var parent models.Task
	if err := tx.First(&parent, "id = ?", parentTaskID).Error; err != nil {
		return models.Task{}, err
	}
	progress, err := loadTaskChildProgress(tx, parentTaskID)
	if err != nil {
		return models.Task{}, err
	}

	currentSubmission, err := loadCurrentTaskSubmission(tx, parent)
	if err != nil {
		return models.Task{}, err
	}
	if currentSubmission != nil && currentSubmission.Origin == taskSubmissionOriginChildRollup {
		switch {
		case currentSubmission.Status == "pending_review" &&
			(parent.Status == "waiting_review" ||
				(parent.Status == "blocked" && parent.BlockedFromStatus != nil && *parent.BlockedFromStatus == "waiting_review")):
			gatesReady, gateErr := taskParentRollupGatesReady(tx, parent.ID)
			if gateErr != nil {
				return models.Task{}, gateErr
			}
			if !progress.readyForReview() || parent.ReviewPolicy != "manual" || !gatesReady {
				reason := childRollupInvalidatedReason
				if progress.readyForReview() && (parent.ReviewPolicy != "manual" || !gatesReady) {
					reason = childRollupGateLostReason
				}
				return withdrawTaskParentRollup(tx, parent, *currentSubmission, progress, reason, requestID, now)
			}
			return parent, nil
		case currentSubmission.Status == "accepted" && parent.Status == "done" && !progress.readyForReview():
			return reopenAcceptedTaskParentRollup(tx, parent, *currentSubmission, progress, requestID, now)
		}
	}

	if parent.Status != "todo" && parent.Status != "in_progress" {
		return parent, nil
	}
	if parent.ReviewPolicy != "manual" || !progress.readyForReview() {
		return parent, nil
	}
	gatesReady, err := taskParentRollupGatesReady(tx, parent.ID)
	if err != nil {
		return models.Task{}, err
	}
	if !gatesReady {
		return parent, nil
	}
	var manualSubmissionCount int64
	if err := tx.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND origin = ?", parent.ID, taskSubmissionOriginManual).
		Count(&manualSubmissionCount).Error; err != nil {
		return models.Task{}, err
	}
	if manualSubmissionCount != 0 {
		return parent, nil
	}
	var rejectedRollupCount int64
	if err := tx.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND origin = ? AND status = 'changes_requested'", parent.ID, taskSubmissionOriginChildRollup).
		Count(&rejectedRollupCount).Error; err != nil {
		return models.Task{}, err
	}
	if rejectedRollupCount != 0 {
		return parent, nil
	}
	var pendingSubmissionCount int64
	if err := tx.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND status = 'pending_review'", parent.ID).
		Count(&pendingSubmissionCount).Error; err != nil {
		return models.Task{}, err
	}
	if pendingSubmissionCount != 0 {
		return parent, nil
	}
	return requestTaskParentReview(tx, parent, progress, requestID, now)
}

func loadTaskChildProgress(tx *gorm.DB, parentTaskID string) (taskChildProgress, error) {
	var progress taskChildProgress
	err := tx.Raw(`
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'done' THEN 1 ELSE 0 END), 0) AS completed,
			COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0) AS cancelled
		FROM tasks
		WHERE parent_task_id = ?
	`, parentTaskID).Scan(&progress).Error
	return progress, err
}

func loadCurrentTaskSubmission(tx *gorm.DB, task models.Task) (*models.TaskSubmission, error) {
	if task.CurrentSubmissionID == nil {
		return nil, nil
	}
	var submission models.TaskSubmission
	if err := tx.First(&submission, "id = ? AND task_id = ?", *task.CurrentSubmissionID, task.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newProjectRequestError(409, "TASK_SUBMISSION_INVALID", "The current Task submission is unavailable")
		}
		return nil, err
	}
	return &submission, nil
}

func taskParentRollupGatesReady(tx *gorm.DB, taskIDValue string) (bool, error) {
	var gates struct {
		Assignee int64 `gorm:"column:assignee"`
		Reviewer int64 `gorm:"column:reviewer"`
	}
	err := tx.Raw(`
		SELECT
			COALESCE(SUM(CASE
				WHEN assignment.role = 'assignee'
				 AND actor.status = 'active'
				 AND actor.type IN ('owner', 'person')
				THEN 1 ELSE 0 END), 0) AS assignee,
			COALESCE(SUM(CASE
				WHEN assignment.role = 'reviewer'
				 AND actor.id = ?
				 AND actor.type = 'owner'
				 AND actor.status = 'active'
				THEN 1 ELSE 0 END), 0) AS reviewer
		FROM task_assignments AS assignment
		JOIN actors AS actor ON actor.id = assignment.actor_id
		WHERE assignment.task_id = ?
		  AND assignment.unassigned_at IS NULL
	`, models.BuiltinOwnerActorID, taskIDValue).Scan(&gates).Error
	return gates.Assignee > 0 && gates.Reviewer > 0, err
}

func requestTaskParentReview(
	tx *gorm.DB,
	parent models.Task,
	progress taskChildProgress,
	requestID string,
	now string,
) (models.Task, error) {
	var sequence int
	if err := tx.Model(&models.TaskSubmission{}).Where("task_id = ?", parent.ID).
		Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
		return models.Task{}, err
	}
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: parent.ID, Sequence: sequence, Status: "pending_review",
		Summary: childRollupSummary, SubmittedByActorID: models.BuiltinSystemActorID,
		SubmittedAt: now, IsInferred: false, Origin: taskSubmissionOriginChildRollup,
	}
	if err := tx.Create(&submission).Error; err != nil {
		return models.Task{}, mapTaskOutputConstraintError(err)
	}
	updates := map[string]any{
		"status": "waiting_review", "current_submission_id": submission.ID,
		"submitted_at": now, "reviewed_at": nil, "completed_at": nil,
		"updated_at": now, "version": gorm.Expr("version + 1"),
	}
	result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", parent.ID, parent.Version).Updates(updates)
	if result.Error != nil {
		return models.Task{}, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Task{}, taskVersionConflict()
	}
	updated, err := loadTask(tx, parent.ID)
	if err != nil {
		return models.Task{}, err
	}
	previous := taskLifecycleSnapshot(parent, "")
	current := taskLifecycleSnapshot(updated, "")
	for key, value := range taskChildProgressSnapshot(progress) {
		previous[key] = value
		current[key] = value
	}
	current["submission_id"] = submission.ID
	current["submission_origin"] = submission.Origin
	current["submission_status"] = submission.Status
	eventSequence, err := nextTaskWorkflowCommandSequence(tx, parent.ID, requestID)
	if err != nil {
		return models.Task{}, err
	}
	if _, err := recordTaskOutputEventAs(
		tx, "task_parent_review_requested", parent.ID, &submission.ID, nil,
		previous, current, models.BuiltinSystemActorID, requestID, now, eventSequence,
	); err != nil {
		return models.Task{}, err
	}
	if err := reconcileInboxItemsForTask(tx, parent.ID, requestID, now); err != nil {
		return models.Task{}, err
	}
	return updated, nil
}

func withdrawTaskParentRollup(
	tx *gorm.DB,
	parent models.Task,
	submission models.TaskSubmission,
	progress taskChildProgress,
	reason string,
	requestID string,
	now string,
) (models.Task, error) {
	result := tx.Model(&models.TaskSubmission{}).
		Where("id = ? AND task_id = ? AND status = 'pending_review' AND origin = ?", submission.ID, parent.ID, taskSubmissionOriginChildRollup).
		Updates(map[string]any{
			"status": "withdrawn", "withdrawn_by_actor_id": models.BuiltinSystemActorID, "withdrawn_at": now,
		})
	if result.Error != nil {
		return models.Task{}, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Task{}, newProjectRequestError(409, "TASK_REVIEW_NOT_ALLOWED", "The child-rollup submission is no longer pending review")
	}
	updates := map[string]any{
		"current_submission_id": nil, "submitted_at": nil, "reviewed_at": nil,
		"completed_at": nil, "updated_at": now, "version": gorm.Expr("version + 1"),
	}
	if parent.Status == "blocked" {
		updates["blocked_from_status"] = "in_progress"
	} else {
		updates["status"] = "in_progress"
	}
	result = tx.Model(&models.Task{}).Where("id = ? AND version = ?", parent.ID, parent.Version).Updates(updates)
	if result.Error != nil {
		return models.Task{}, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Task{}, taskVersionConflict()
	}
	updated, err := loadTask(tx, parent.ID)
	if err != nil {
		return models.Task{}, err
	}
	previous := taskLifecycleSnapshot(parent, "")
	previous["submission_id"] = submission.ID
	previous["submission_origin"] = submission.Origin
	previous["submission_status"] = "pending_review"
	current := taskLifecycleSnapshot(updated, reason)
	current["submission_id"] = submission.ID
	current["submission_origin"] = submission.Origin
	current["submission_status"] = "withdrawn"
	for key, value := range taskChildProgressSnapshot(progress) {
		current[key] = value
	}
	eventSequence, err := nextTaskWorkflowCommandSequence(tx, parent.ID, requestID)
	if err != nil {
		return models.Task{}, err
	}
	if _, err := recordTaskOutputEventAs(
		tx, "task_parent_review_withdrawn", parent.ID, &submission.ID, nil,
		previous, current, models.BuiltinSystemActorID, requestID, now, eventSequence,
	); err != nil {
		return models.Task{}, err
	}
	if err := reconcileInboxItemsForTask(tx, parent.ID, requestID, now); err != nil {
		return models.Task{}, err
	}
	return updated, nil
}

func reopenAcceptedTaskParentRollup(
	tx *gorm.DB,
	parent models.Task,
	submission models.TaskSubmission,
	progress taskChildProgress,
	requestID string,
	now string,
) (models.Task, error) {
	updates := map[string]any{
		"status": "todo", "completed_at": nil, "current_submission_id": nil,
		"submitted_at": nil, "reviewed_at": nil, "blocked_reason": nil,
		"blocked_at": nil, "blocked_from_status": nil,
		"updated_at": now, "version": gorm.Expr("version + 1"),
	}
	result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", parent.ID, parent.Version).Updates(updates)
	if result.Error != nil {
		return models.Task{}, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Task{}, taskVersionConflict()
	}
	updated, err := loadTask(tx, parent.ID)
	if err != nil {
		return models.Task{}, err
	}
	previous := taskLifecycleSnapshot(parent, "")
	previous["submission_id"] = submission.ID
	previous["submission_origin"] = submission.Origin
	previous["submission_status"] = submission.Status
	current := taskLifecycleSnapshot(updated, childRollupInvalidatedReason)
	current["submission_id"] = submission.ID
	current["submission_origin"] = submission.Origin
	current["submission_status"] = submission.Status
	for key, value := range taskChildProgressSnapshot(progress) {
		current[key] = value
	}
	eventSequence, err := nextTaskWorkflowCommandSequence(tx, parent.ID, requestID)
	if err != nil {
		return models.Task{}, err
	}
	if _, err := recordTaskOutputEventAs(
		tx, "task_parent_reopened", parent.ID, &submission.ID, nil,
		previous, current, models.BuiltinSystemActorID, requestID, now, eventSequence,
	); err != nil {
		return models.Task{}, err
	}
	if err := reconcileInboxItemsForTask(tx, parent.ID, requestID, now); err != nil {
		return models.Task{}, err
	}
	return updated, nil
}

func nextTaskWorkflowCommandSequence(tx *gorm.DB, taskIDValue, requestID string) (int, error) {
	var sequence int
	err := tx.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'task' AND aggregate_id = ? AND request_id = ?", taskIDValue, requestID).
		Select("COALESCE(MAX(command_seq), 0) + 1").Scan(&sequence).Error
	return sequence, err
}

func taskParentProgressError(context string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}
