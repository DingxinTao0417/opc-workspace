package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Caller owns the transaction, idempotency and committed-file compensation.
// AI uses non-file artifacts only; native multipart keeps its file lifecycle.
func submitTaskOutputInTransaction(tx *gorm.DB, taskIDValue string, expectedVersion int64, input submitOutputRequest, artifacts []preparedArtifact, commitFile func(preparedArtifact) error, requestID, now string) (response submitOutputResponse, err error) {
	return submitTaskOutputInTransactionAs(
		tx, taskIDValue, expectedVersion, input, artifacts, commitFile, requestID, now,
		taskOutputAttribution{
			SubmittedByActorID: models.BuiltinOwnerActorID,
			RecordedByActorID:  models.BuiltinOwnerActorID,
			EventActorID:       models.BuiltinOwnerActorID,
		},
	)
}

type taskOutputAttribution struct {
	SubmittedByActorID      string
	RecordedByActorID       string
	EventActorID            string
	ExpectedProducerActorID string
	AgentRunID              *string
}

func submitTaskOutputInTransactionAs(tx *gorm.DB, taskIDValue string, expectedVersion int64, input submitOutputRequest, artifacts []preparedArtifact, commitFile func(preparedArtifact) error, requestID, now string, attribution taskOutputAttribution) (response submitOutputResponse, err error) {
	var task models.Task
	if err := tx.First(&task, "id = ?", taskIDValue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return response, err
	}
	if task.Version != expectedVersion {
		return response, taskVersionConflict()
	}
	if task.ReviewPolicy != "manual" {
		return response, newProjectRequestError(http.StatusConflict, "TASK_MANUAL_REVIEW_REQUIRED", "Only manual-review tasks accept submitted output")
	}
	if task.Status != "todo" && task.Status != "in_progress" {
		return response, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_NOT_ALLOWED", "Output can only be submitted from todo or in-progress status")
	}
	assignee, err := requireTaskOutputActors(tx, taskIDValue)
	if err != nil {
		return response, err
	}
	if attribution.ExpectedProducerActorID != "" && assignee.ActorID != attribution.ExpectedProducerActorID {
		return response, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
			"The active Task assignee changed before Agent output delivery")
	}
	var sequence int
	if err := tx.Model(&models.TaskSubmission{}).Where("task_id = ?", taskIDValue).
		Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
		return response, err
	}
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: taskIDValue, Sequence: sequence, Status: "pending_review",
		Origin: taskSubmissionOriginManual, Summary: input.Summary,
		SubmittedByActorID: attribution.SubmittedByActorID, SubmittedAt: now,
	}
	if err := tx.Create(&submission).Error; err != nil {
		return response, mapTaskOutputConstraintError(err)
	}
	for _, artifact := range artifacts {
		if artifact.StagedFile != nil {
			if commitFile == nil {
				return response, newProjectRequestError(503, "ARTIFACT_STORAGE_UNAVAILABLE", "File storage is unavailable")
			}
			if err := commitFile(artifact); err != nil {
				return response, err
			}
		}
		record := models.TaskArtifact{
			ID: artifact.ID, TaskID: taskIDValue, SubmissionID: submission.ID, Position: artifact.Position,
			StorageKind: artifact.StorageKind, Name: artifact.Name, ContentText: artifact.ContentText,
			ReferenceURL: artifact.ReferenceURL, StructuredJSON: artifact.StructuredJSON,
			RelativePath: artifact.RelativePath, MimeType: artifact.MimeType, SizeBytes: artifact.SizeBytes,
			SHA256: artifact.SHA256, RequiresFollowup: artifact.RequiresFollowup,
			ProducedByActorID: assignee.ActorID, RecordedByActorID: attribution.RecordedByActorID,
			IntegrityStatus: "unverified", CreatedAt: now,
		}
		if artifact.StorageKind == "file" {
			record.IntegrityStatus = "verified"
			record.IntegrityCheckedAt = &now
		}
		if err := tx.Create(&record).Error; err != nil {
			return response, mapTaskOutputConstraintError(err)
		}
	}
	updates := map[string]any{
		"status": "waiting_review", "current_submission_id": submission.ID,
		"submitted_at": now, "reviewed_at": nil, "completed_at": nil,
		"updated_at": now, "version": gorm.Expr("version + 1"),
	}
	result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", taskIDValue, expectedVersion).Updates(updates)
	if result.Error != nil {
		return response, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return response, taskVersionConflict()
	}
	updated, err := loadTask(tx, taskIDValue)
	if err != nil {
		return response, err
	}
	submissionOutput, err := loadSubmissionOutput(tx, submission.ID)
	if err != nil {
		return response, err
	}
	artifactSnapshots := artifactEventSnapshots(submissionOutput.Artifacts)
	sequenceValue := 1
	event, err := recordTaskOutputEventAttributed(tx, "task_output_submitted", taskIDValue, &submission.ID, nil,
		taskLifecycleSnapshot(task, ""), map[string]any{
			"status": updated.Status, "review_policy": updated.ReviewPolicy,
			"current_submission_id": updated.CurrentSubmissionID, "submitted_at": updated.SubmittedAt,
			"reviewed_at": updated.ReviewedAt, "version": updated.Version,
			"submission_id": submission.ID, "submission_sequence": submission.Sequence,
			"artifact_count": len(submissionOutput.Artifacts), "artifacts": artifactSnapshots,
		}, attribution.EventActorID, attribution.AgentRunID, requestID, now, sequenceValue)
	if err != nil {
		return response, err
	}
	if err := projectTaskArtifactFollowups(
		tx, updated, submission, requestID, now,
	); err != nil {
		return response, err
	}
	if err := reconcileInboxItemsForTask(tx, taskIDValue, requestID, now); err != nil {
		return response, err
	}
	response = submitOutputResponse{
		Task: updated, Submission: submissionOutput, Artifacts: submissionOutput.Artifacts, Event: event,
	}
	return response, nil
}

func reviewTaskOutputInTransaction(tx *gorm.DB, taskIDValue string, expectedVersion int64, input reviewTaskOutputRequest, requestID, now string) (response reviewTaskOutputResponse, err error) {
	reason := ""
	if input.Reason != nil {
		reason = strings.TrimSpace(*input.Reason)
	}
	var task models.Task
	if err := tx.First(&task, "id = ?", taskIDValue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return response, err
	}
	if task.Version != expectedVersion {
		return response, taskVersionConflict()
	}
	if task.ReviewPolicy != "manual" || task.Status != "waiting_review" || task.CurrentSubmissionID == nil {
		return response, newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The task does not have output awaiting manual review")
	}
	if _, err := requireActiveOwnerReviewer(tx, taskIDValue); err != nil {
		return response, err
	}
	var submission models.TaskSubmission
	if err := tx.First(&submission, "id = ? AND task_id = ?", *task.CurrentSubmissionID, taskIDValue).Error; err != nil {
		return response, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is unavailable")
	}
	if submission.Status != "pending_review" {
		return response, newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The current submission is no longer pending review")
	}
	commandSequence := 1
	action := "task_changes_requested"
	targetStatus := "in_progress"
	updates := map[string]any{
		"status": targetStatus, "reviewed_at": now, "completed_at": nil,
		"updated_at": now, "version": gorm.Expr("version + 1"),
	}
	if input.Decision == "accept" {
		action = "task_review_accepted"
		targetStatus = "done"
		updates["status"] = targetStatus
		updates["completed_at"] = now
		var err error
		commandSequence, err = closeActiveAssignmentsForTerminalTask(
			tx, taskIDValue, requestID, now, "Task review accepted", commandSequence,
		)
		if err != nil {
			return response, err
		}
	}
	submissionUpdates := map[string]any{
		"status":               map[bool]string{true: "accepted", false: "changes_requested"}[input.Decision == "accept"],
		"reviewed_by_actor_id": models.BuiltinOwnerActorID, "reviewed_at": now,
	}
	if reason != "" {
		submissionUpdates["review_reason"] = reason
	} else {
		submissionUpdates["review_reason"] = nil
	}
	result := tx.Model(&models.TaskSubmission{}).
		Where("id = ? AND task_id = ? AND status = 'pending_review'", submission.ID, taskIDValue).
		Updates(submissionUpdates)
	if result.Error != nil {
		return response, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return response, newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The current submission is no longer pending review")
	}
	result = tx.Model(&models.Task{}).Where("id = ? AND version = ?", taskIDValue, expectedVersion).Updates(updates)
	if result.Error != nil {
		return response, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return response, taskVersionConflict()
	}
	updated, err := loadTask(tx, taskIDValue)
	if err != nil {
		return response, err
	}
	submissionOutput, err := loadSubmissionOutput(tx, submission.ID)
	if err != nil {
		return response, err
	}
	previousSnapshot := taskLifecycleSnapshot(task, "")
	previousSnapshot["submission_id"] = submission.ID
	previousSnapshot["submission_status"] = "pending_review"
	currentSnapshot := taskLifecycleSnapshot(updated, reason)
	currentSnapshot["submission_id"] = submission.ID
	currentSnapshot["submission_status"] = submissionOutput.Status
	event, err := recordTaskOutputEvent(
		tx, action, taskIDValue, &submission.ID, nil, previousSnapshot, currentSnapshot,
		requestID, now, commandSequence,
	)
	if err != nil {
		return response, err
	}
	if err := reconcileInboxItemsForTask(tx, taskIDValue, requestID, now); err != nil {
		return response, err
	}
	if input.Decision == "accept" {
		if err := reconcileTaskParentChain(tx, updated.ParentTaskID, requestID, now); err != nil {
			return response, taskParentProgressError("reconcile reviewed Task parent", err)
		}
	}
	response = reviewTaskOutputResponse{Task: updated, Submission: submissionOutput, Event: event}
	return response, nil
}

func prepareTaskOutputInput(input submitOutputRequest, isMultipart bool) (submitOutputRequest, []preparedArtifact, error) {
	input.Summary = strings.TrimSpace(input.Summary)
	if utf8.RuneCountInString(input.Summary) > 10_000 {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "summary cannot exceed 10000 characters")
	}
	if len(input.Artifacts) > maxArtifactsPerSubmission {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "a submission cannot contain more than 20 Artifacts")
	}
	if input.Summary == "" && len(input.Artifacts) == 0 {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "summary or at least one Artifact is required")
	}
	prepared := make([]preparedArtifact, 0, len(input.Artifacts))
	usedFileFields := make(map[string]struct{})
	usedClientRefs := make(map[string]struct{})
	for index, artifact := range input.Artifacts {
		artifact.ClientRef = strings.TrimSpace(artifact.ClientRef)
		if utf8.RuneCountInString(artifact.ClientRef) < 1 || utf8.RuneCountInString(artifact.ClientRef) > 100 || hasUnsafeControlCharacters(artifact.ClientRef) {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "client_ref must contain 1 to 100 safe characters")
		}
		if _, exists := usedClientRefs[artifact.ClientRef]; exists {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "client_ref values must be unique within a submission")
		}
		usedClientRefs[artifact.ClientRef] = struct{}{}
		value, err := prepareArtifactInput(artifact, index+1, usedFileFields, isMultipart)
		if err != nil {
			return submitOutputRequest{}, nil, err
		}
		prepared = append(prepared, value)
	}
	return input, prepared, nil
}
