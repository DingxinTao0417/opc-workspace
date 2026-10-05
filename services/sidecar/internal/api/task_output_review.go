package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func (a *API) reviewTaskOutput(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input reviewTaskOutputRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.Decision = strings.TrimSpace(input.Decision)
	if input.Decision != "accept" && input.Decision != "request_changes" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "decision must be accept or request_changes")
		return
	}
	reason := ""
	if input.Reason != nil {
		reason = strings.TrimSpace(*input.Reason)
	}
	if input.Decision == "request_changes" && reason == "" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason is required when requesting changes")
		return
	}
	if utf8.RuneCountInString(reason) > 1_000 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason cannot exceed 1000 characters")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"expected_version": expectedVersion, "decision": input.Decision, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/review", taskIDValue)
	statusCode := http.StatusOK
	replayed := false
	var response reviewTaskOutputResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, replayStatus, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		var commandErr error
		response, commandErr = reviewTaskOutputInTransaction(tx, taskIDValue, expectedVersion, input, requestIDFromContext(c), now)
		if commandErr != nil {
			return commandErr
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, taskIDValue, requestHash, statusCode, response, now)
	})
	if err != nil {
		writeTaskOutputError(c, err)
		return
	}
	if !replayed {
		// The Task Submission is the authoritative review fact. Wake plan
		// continuations only after its review transaction commits; replayed
		// idempotent responses do not represent a new fact transition.
		a.wakeAIContinuationsAfterFactCommit()
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTaskSubmissionOutputOrigin(&response.Submission)
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) deleteTaskArtifact(c *gin.Context) {
	id, ok := artifactID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm=true is required to delete an Artifact")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input deleteTaskArtifactRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := validateArtifactDeleteReason(input.Reason)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"artifact_id": id, "expected_version": expectedVersion, "confirm": true, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("DELETE /api/v1/artifacts/%s", id)
	replayed := false
	var moved *filestore.TrashedArtifactFile
	var response deleteTaskArtifactResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			return nil
		}
		row, err := loadArtifactRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ARTIFACT_NOT_FOUND", "Artifact not found")
			}
			return err
		}
		if row.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_ALREADY_DELETED", "The Artifact is already deleted")
		}
		referenced, err := activeAgentRunReferencesControlledFile(
			tx, agentexec.FileSourceTaskArtifact, row.ID,
		)
		if err != nil {
			return err
		}
		if referenced {
			return newProjectRequestError(http.StatusConflict, agentRunFileReferencedByActiveRunCode,
				"Wait for or cancel the active Agent Run before deleting this input Artifact")
		}
		var task models.Task
		if err := tx.First(&task, "id = ?", row.TaskID).Error; err != nil {
			return err
		}
		if task.Version != expectedVersion {
			return taskVersionConflict()
		}
		var submission models.TaskSubmission
		if err := tx.Select("status").First(&submission, "id = ?", row.SubmissionID).Error; err != nil {
			return err
		}
		if submission.Status == "pending_review" {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_PENDING_REVIEW", "Artifacts cannot be deleted while their submission is pending review")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := coordinateTaskArtifactInboxSourceDeletion(
			tx,
			[]string{row.ID},
			"ARTIFACT_HAS_ACTIVE_INBOX_SOURCE",
			"Resolve or dismiss the Artifact follow-up Inbox Item before deleting this Artifact",
			requestIDFromContext(c),
			now,
		); err != nil {
			return err
		}
		fileMissing := false
		if row.StorageKind == "file" {
			if a.artifactStore == nil || row.RelativePath == nil {
				return newProjectRequestError(http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
			}
			if err := recordArtifactDeletionTombstone(tx, row.TaskArtifact, "artifact", now); err != nil {
				return err
			}
			trashed, err := a.artifactStore.MoveObjectToTrash(*row.RelativePath, row.ID)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fileMissing = true
				} else {
					return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be moved to controlled trash")
				}
			} else {
				moved = &trashed
			}
		}
		artifactUpdates := map[string]any{
			"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": reason,
		}
		if fileMissing {
			artifactUpdates["integrity_status"] = "missing"
			artifactUpdates["integrity_checked_at"] = now
		}
		result := tx.Model(&models.TaskArtifact{}).Where("id = ? AND deleted_at IS NULL", id).Updates(artifactUpdates)
		if result.Error != nil {
			return mapTaskOutputConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_ALREADY_DELETED", "The Artifact is already deleted")
		}
		result = tx.Model(&models.Task{}).Where("id = ? AND version = ?", task.ID, expectedVersion).Updates(map[string]any{
			"updated_at": now, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		updated, err := loadTask(tx, task.ID)
		if err != nil {
			return err
		}
		deletedRow, err := loadArtifactRow(tx, id)
		if err != nil {
			return err
		}
		deletedArtifact, err := artifactSummaryFromRow(deletedRow)
		if err != nil {
			return err
		}
		sequenceValue := 1
		event, err := recordTaskOutputEvent(tx, "task_artifact_deleted", task.ID, &row.SubmissionID, &row.ID,
			map[string]any{"artifact": artifactEventSnapshotFromRow(row), "version": task.Version},
			map[string]any{"artifact_id": row.ID, "deleted_at": now, "reason": reason, "version": updated.Version},
			requestIDFromContext(c), now, sequenceValue)
		if err != nil {
			return err
		}
		response = deleteTaskArtifactResponse{Task: updated, Artifact: deletedArtifact, Event: event}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, task.ID, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
		if moved != nil {
			if restoreErr := a.artifactStore.RestoreTrashedFile(*moved); restoreErr != nil && a.options.Logger != nil {
				a.options.Logger.Printf("Artifact delete compensation failed artifact_id=%s", id)
			}
		}
		writeTaskOutputError(c, err)
		return
	}
	if moved != nil {
		a.artifactStore.PurgeTrashedFile(*moved)
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func validateArtifactDeleteReason(value string) (string, error) {
	reason := strings.TrimSpace(value)
	length := utf8.RuneCountInString(reason)
	if length < 1 || length > 1000 {
		return "", errors.New("reason must contain 1 to 1000 characters")
	}
	return reason, nil
}

func taskHasPendingReview(task models.Task) bool {
	return task.Status == "waiting_review" ||
		(task.Status == "blocked" && task.BlockedFromStatus != nil && *task.BlockedFromStatus == "waiting_review")
}

func withdrawCurrentSubmissionForCancellation(
	tx *gorm.DB,
	task models.Task,
	requestID, now, reason string,
	commandSequence int,
) (int, error) {
	if task.CurrentSubmissionID == nil {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The pending Task submission is unavailable")
	}
	var submission models.TaskSubmission
	if err := tx.First(&submission, "id = ? AND task_id = ?", *task.CurrentSubmissionID, task.ID).Error; err != nil {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The pending Task submission is unavailable")
	}
	if submission.Status != "pending_review" {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is not pending review")
	}
	result := tx.Model(&models.TaskSubmission{}).
		Where("id = ? AND task_id = ? AND status = 'pending_review'", submission.ID, task.ID).
		Updates(map[string]any{
			"status": "withdrawn", "withdrawn_by_actor_id": models.BuiltinOwnerActorID, "withdrawn_at": now,
		})
	if result.Error != nil {
		return commandSequence, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is no longer pending review")
	}
	eventSequence := commandSequence
	_, err := recordTaskOutputEvent(
		tx, "task_submission_withdrawn", task.ID, &submission.ID, nil,
		map[string]any{"submission_id": submission.ID, "submission_status": "pending_review"},
		map[string]any{"submission_id": submission.ID, "submission_status": "withdrawn", "reason": reason},
		requestID, now, eventSequence,
	)
	if err != nil {
		return commandSequence, err
	}
	return commandSequence + 1, nil
}

func recordArtifactDeletionTombstone(tx *gorm.DB, artifact models.TaskArtifact, scope, deletedAt string) error {
	if artifact.StorageKind != "file" || artifact.RelativePath == nil || artifact.SizeBytes == nil || artifact.SHA256 == nil {
		return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "A file Artifact has incomplete deletion metadata")
	}
	return tx.Create(&models.ArtifactDeletionTombstone{
		ArtifactID: artifact.ID, TaskID: artifact.TaskID, RelativePath: *artifact.RelativePath,
		SizeBytes: *artifact.SizeBytes, SHA256: *artifact.SHA256, DeletionScope: scope, DeletedAt: deletedAt,
	}).Error
}

func (a *API) trashTaskArtifactFiles(tx *gorm.DB, taskIDValue, deletedAt string) ([]filestore.TrashedArtifactFile, error) {
	var artifacts []models.TaskArtifact
	if err := tx.Select("id", "task_id", "storage_kind", "relative_path", "size_bytes", "sha256").
		Where("task_id = ? AND storage_kind = 'file' AND deleted_at IS NULL", taskIDValue).
		Order("id ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	if a.artifactStore == nil {
		return nil, newProjectRequestError(http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
	}
	moved := make([]filestore.TrashedArtifactFile, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.RelativePath == nil {
			return moved, newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "A Task Artifact has an invalid controlled path")
		}
		if err := recordArtifactDeletionTombstone(tx, artifact, "task", deletedAt); err != nil {
			return moved, err
		}
		entry, err := a.artifactStore.MoveObjectToTrash(*artifact.RelativePath, artifact.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return moved, newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "Task Artifact files could not be prepared for deletion")
		}
		moved = append(moved, entry)
	}
	return moved, nil
}

func (a *API) restoreTaskArtifactFiles(moved []filestore.TrashedArtifactFile) error {
	if a.artifactStore == nil {
		return nil
	}
	var restoreErr error
	for index := len(moved) - 1; index >= 0; index-- {
		if err := a.artifactStore.RestoreTrashedFile(moved[index]); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	return restoreErr
}
