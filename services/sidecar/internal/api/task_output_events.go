package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type activeTaskAssignee struct {
	ActorID string `gorm:"column:actor_id"`
}

func requireTaskOutputActors(tx *gorm.DB, taskIDValue string) (activeTaskAssignee, error) {
	var assignee activeTaskAssignee
	err := tx.Table("task_assignments AS assignment").Select("assignment.actor_id").
		Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
		Where("assignment.task_id = ? AND assignment.role = 'assignee' AND assignment.unassigned_at IS NULL", taskIDValue).
		// ADR-027: an active agent assignee submits through its run pipeline;
		// the produced artifact records the agent as producer.
		Where("actor.status = 'active' AND actor.type IN ('owner', 'person', 'agent')").Take(&assignee).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activeTaskAssignee{}, newProjectRequestError(http.StatusConflict, "TASK_ASSIGNEE_REQUIRED", "An active assignee is required before submitting output")
	}
	if err != nil {
		return activeTaskAssignee{}, err
	}
	if _, err := requireActiveOwnerReviewer(tx, taskIDValue); err != nil {
		return activeTaskAssignee{}, err
	}
	return assignee, nil
}

func requireActiveOwnerReviewer(tx *gorm.DB, taskIDValue string) (string, error) {
	var actorID string
	err := tx.Table("task_assignments AS assignment").Select("assignment.actor_id").
		Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
		Where("assignment.task_id = ? AND assignment.role = 'reviewer' AND assignment.unassigned_at IS NULL", taskIDValue).
		Where("actor.id = ? AND actor.type = 'owner' AND actor.status = 'active'", models.BuiltinOwnerActorID).
		Scan(&actorID).Error
	if err != nil {
		return "", err
	}
	if actorID == "" {
		return "", newProjectRequestError(http.StatusConflict, "TASK_REVIEWER_REQUIRED", "An active owner reviewer is required for manual review")
	}
	return actorID, nil
}

func recordTaskOutputEvent(
	tx *gorm.DB,
	action, taskIDValue string,
	submissionID, artifactIDValue *string,
	previous, current map[string]any,
	requestID, createdAt string,
	commandSequence int,
) (taskWorkflowEventOutput, error) {
	return recordTaskOutputEventAs(
		tx, action, taskIDValue, submissionID, artifactIDValue,
		previous, current, models.BuiltinOwnerActorID, requestID, createdAt, commandSequence,
	)
}

func normalizeTaskSubmissionOutputOrigin(submission *taskSubmissionOutput) {
	if submission.Origin == "" {
		submission.Origin = taskSubmissionOriginManual
	}
}

func recordTaskOutputEventAs(
	tx *gorm.DB,
	action, taskIDValue string,
	submissionID, artifactIDValue *string,
	previous, current map[string]any,
	actorIDValue, requestID, createdAt string,
	commandSequence int,
) (taskWorkflowEventOutput, error) {
	return recordTaskOutputEventAttributed(
		tx, action, taskIDValue, submissionID, artifactIDValue,
		previous, current, actorIDValue, nil, requestID, createdAt, commandSequence,
	)
}

func recordTaskOutputEventAttributed(
	tx *gorm.DB,
	action, taskIDValue string,
	submissionID, artifactIDValue *string,
	previous, current map[string]any,
	actorIDValue string,
	agentRunID *string,
	requestID, createdAt string,
	commandSequence int,
) (taskWorkflowEventOutput, error) {
	var previousJSON *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return taskWorkflowEventOutput{}, err
		}
		value := string(encoded)
		previousJSON = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return taskWorkflowEventOutput{}, err
	}
	currentJSON := string(encoded)
	var requestIDValue *string
	if requestID != "" {
		requestIDValue = &requestID
	}
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: taskIDValue, Action: action,
		ActorID: &actorIDValue, SubmissionID: submissionID, ArtifactID: artifactIDValue,
		AgentRunID: agentRunID, RequestID: requestIDValue, CommandSeq: &commandSequence, PreviousJSON: previousJSON,
		CurrentJSON: &currentJSON, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return taskWorkflowEventOutput{}, err
	}
	return loadTaskWorkflowEvent(tx, event.ID)
}

func artifactEventSnapshots(artifacts []taskArtifactSummary) []map[string]any {
	values := make([]map[string]any, len(artifacts))
	for index, artifact := range artifacts {
		values[index] = map[string]any{
			"id": artifact.ID, "position": artifact.Position, "storage_kind": artifact.StorageKind,
			"name": artifact.Name, "mime_type": artifact.MimeType, "size_bytes": artifact.SizeBytes,
			"sha256": artifact.SHA256, "requires_followup": artifact.RequiresFollowup,
			"produced_by_actor_id": artifact.ProducedByActorID, "recorded_by_actor_id": artifact.RecordedByActorID,
		}
	}
	return values
}

func artifactEventSnapshotFromRow(row taskArtifactRow) map[string]any {
	return map[string]any{
		"id": row.ID, "submission_id": row.SubmissionID, "position": row.Position,
		"storage_kind": row.StorageKind, "name": row.Name, "mime_type": row.MimeType,
		"size_bytes": row.SizeBytes, "sha256": row.SHA256, "requires_followup": row.RequiresFollowup,
		"produced_by_actor_id": row.ProducedByActorID, "recorded_by_actor_id": row.RecordedByActorID,
	}
}

func mapTaskOutputConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "UNIQUE constraint failed: task_submissions.task_id"):
		return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_ALREADY_PENDING", "The task already has a submission pending review")
	case strings.Contains(message, "TASK_ARTIFACT_SUBMISSION_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "TASK_ARTIFACT_SUBMISSION_MISMATCH", "Artifact and submission must belong to the same Task")
	case strings.Contains(message, "TASK_CURRENT_SUBMISSION_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is invalid")
	case strings.Contains(message, "TASK_SUBMISSION_HISTORY_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The submission is no longer pending review")
	case strings.Contains(message, "TASK_ARTIFACT_FACTS_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "ARTIFACT_IMMUTABLE", "Artifact facts cannot be changed")
	default:
		return mapTaskWorkflowConstraintError(err)
	}
}

func writeTaskOutputError(c *gin.Context, err error) {
	err = mapTaskOutputConstraintError(err)
	if writeProjectRequestError(c, err) {
		return
	}
	writeDatabaseError(c)
}
