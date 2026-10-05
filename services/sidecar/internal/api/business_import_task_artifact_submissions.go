package api

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

var taskOutputSubmittedStateKeys = []string{
	"status", "review_policy", "current_submission_id", "submitted_at", "reviewed_at", "version",
	"submission_id", "submission_sequence", "artifact_count", "artifacts",
}

type taskArtifactSubmittedImportProof struct {
	event       automationImportWorkflowEvent
	taskVersion int64
}

type taskArtifactSubmittedImportValidation struct {
	taskID    string
	createdAt string
	proof     taskArtifactSubmittedImportProof
	valid     bool
}

type taskArtifactSubmissionDispositionImportProof struct {
	status      string
	eventTime   time.Time
	taskVersion int64
}

func (index *taskArtifactImportValidationIndex) validSubmittedEvent(
	taskID, submissionID, createdAt string,
	task, submission, targetArtifact map[string]any,
) (taskArtifactSubmittedImportProof, bool) {
	validation, cached := index.submittedBySubmissionID[submissionID]
	if !cached {
		proof, valid := validTaskArtifactSubmittedImportEvent(
			index.events, taskID, submissionID, createdAt, task, submission,
			index.artifactsBySubmission[submissionID],
		)
		validation = taskArtifactSubmittedImportValidation{
			taskID: taskID, createdAt: createdAt, proof: proof, valid: valid,
		}
		index.submittedBySubmissionID[submissionID] = validation
	}
	if !validation.valid || validation.taskID != taskID || validation.createdAt != createdAt {
		return taskArtifactSubmittedImportProof{}, false
	}
	producedBy, producedByOK := targetArtifact["produced_by_actor_id"].(string)
	if !producedByOK || !validTaskArtifactSubmissionAssignmentActors(
		index.assignments, taskID, producedBy, createdAt,
	) {
		return taskArtifactSubmittedImportProof{}, false
	}
	return validation.proof, true
}

func validTaskArtifactSubmissionDispositionImportEvent(
	events automationImportEventIndex,
	taskID, submissionID string,
	task, submission map[string]any,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	status, statusOK := submission["status"].(string)
	origin, originOK := submission["origin"].(string)
	isInferred, inferredOK := automationImportInt64(submission["is_inferred"])
	if !statusOK || !originOK || origin != taskSubmissionOriginManual || !inferredOK || isInferred != 0 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	for _, action := range []string{"task_review_accepted", "task_changes_requested", "task_submission_withdrawn"} {
		matched := taskArtifactSubmissionDispositionEvents(events, taskID, submissionID, action)
		if action == taskArtifactSubmissionDispositionAction(status) {
			if len(matched) != 1 {
				return taskArtifactSubmissionDispositionImportProof{}, false
			}
			continue
		}
		if len(matched) != 0 {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	}
	eventAction := taskArtifactSubmissionDispositionAction(status)
	if eventAction == "" {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	event := taskArtifactSubmissionDispositionEvents(events, taskID, submissionID, eventAction)[0]
	if status == "withdrawn" {
		return validTaskArtifactWithdrawnSubmissionImportEvent(events, event, task, submission, submissionID)
	}
	return validTaskArtifactReviewedSubmissionImportEvent(event, task, submission, submissionID, status)
}

func taskArtifactSubmissionDispositionAction(status string) string {
	switch status {
	case "accepted":
		return "task_review_accepted"
	case "changes_requested":
		return "task_changes_requested"
	case "withdrawn":
		return "task_submission_withdrawn"
	default:
		return ""
	}
}

func taskArtifactSubmissionDispositionEvents(
	events automationImportEventIndex,
	taskID, submissionID, action string,
) []automationImportWorkflowEvent {
	matched := make([]automationImportWorkflowEvent, 0, 1)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, action) {
		if event.submissionID != nil && *event.submissionID == submissionID {
			matched = append(matched, event)
		}
	}
	return matched
}

func validTaskArtifactReviewedSubmissionImportEvent(
	event automationImportWorkflowEvent,
	task, submission map[string]any,
	submissionID, status string,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	reviewedBy, reviewedByOK := submission["reviewed_by_actor_id"].(string)
	reviewedAt, reviewedAtOK := submission["reviewed_at"].(string)
	reviewReason, reasonOK := automationImportOptionalString(submission["review_reason"])
	withdrawnBy, withdrawnByOK := automationImportOptionalString(submission["withdrawn_by_actor_id"])
	withdrawnAt, withdrawnAtOK := automationImportOptionalString(submission["withdrawn_at"])
	submittedAt, submittedAtOK := submission["submitted_at"].(string)
	if !reviewedByOK || reviewedBy != models.BuiltinOwnerActorID || !reviewedAtOK || !reasonOK ||
		!withdrawnByOK || withdrawnBy != nil || !withdrawnAtOK || withdrawnAt != nil || !submittedAtOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != reviewedAt ||
		event.assignmentID != nil || event.submissionID == nil || *event.submissionID != submissionID ||
		event.artifactID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq < 1 ||
		event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if status == "changes_requested" && (*event.commandSeq != 1 || reviewReason == nil) {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !taskArtifactReviewStateHasExactKeys(previous, false) ||
		!taskArtifactReviewStateHasExactKeys(current, reviewReason != nil) ||
		previous["status"] != "waiting_review" || previous["review_policy"] != "manual" ||
		previous["blocked_reason"] != nil || previous["blocked_at"] != nil || previous["blocked_from_status"] != nil ||
		previous["completed_at"] != nil || previous["submitted_at"] != submittedAt || previous["reviewed_at"] != nil ||
		previous["current_submission_id"] != submissionID || previous["submission_id"] != submissionID ||
		previous["submission_status"] != "pending_review" || current["status"] != map[bool]string{true: "done", false: "in_progress"}[status == "accepted"] ||
		current["review_policy"] != "manual" || current["blocked_reason"] != nil || current["blocked_at"] != nil ||
		current["blocked_from_status"] != nil || current["submitted_at"] != submittedAt || current["reviewed_at"] != reviewedAt ||
		current["current_submission_id"] != submissionID || current["submission_id"] != submissionID ||
		current["submission_status"] != status {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if status == "accepted" {
		if current["completed_at"] != reviewedAt {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	} else if current["completed_at"] != nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	if reviewReason == nil {
		if _, exists := current["reason"]; exists {
			return taskArtifactSubmissionDispositionImportProof{}, false
		}
	} else if current["reason"] != *reviewReason || strings.TrimSpace(*reviewReason) != *reviewReason || *reviewReason == "" ||
		utf8.RuneCountInString(*reviewReason) > 1_000 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalTaskVersion, finalVersionOK := automationImportInt64(task["version"])
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	if !previousVersionOK || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalTaskVersion < currentVersion || !eventTimeOK {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	return taskArtifactSubmissionDispositionImportProof{
		status: status, eventTime: eventTime, taskVersion: currentVersion,
	}, true
}

func taskArtifactReviewStateHasExactKeys(state map[string]any, includeReason bool) bool {
	keys := make([]string, 0, len(taskImportLifecycleStateKeys)+3)
	keys = append(keys, taskImportLifecycleStateKeys...)
	keys = append(keys, "submission_id", "submission_status")
	if includeReason {
		keys = append(keys, "reason")
	}
	return automationImportObjectHasExactKeys(state, keys)
}

func validTaskArtifactWithdrawnSubmissionImportEvent(
	events automationImportEventIndex,
	event automationImportWorkflowEvent,
	task, submission map[string]any,
	submissionID string,
) (taskArtifactSubmissionDispositionImportProof, bool) {
	reviewedBy, reviewedByOK := automationImportOptionalString(submission["reviewed_by_actor_id"])
	reviewedAt, reviewedAtOK := automationImportOptionalString(submission["reviewed_at"])
	reviewReason, reviewReasonOK := automationImportOptionalString(submission["review_reason"])
	withdrawnBy, withdrawnByOK := submission["withdrawn_by_actor_id"].(string)
	withdrawnAt, withdrawnAtOK := submission["withdrawn_at"].(string)
	if !reviewedByOK || reviewedBy != nil || !reviewedAtOK || reviewedAt != nil ||
		!reviewReasonOK || reviewReason != nil || !withdrawnByOK || withdrawnBy != models.BuiltinOwnerActorID || !withdrawnAtOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != withdrawnAt ||
		event.assignmentID != nil || event.submissionID == nil || *event.submissionID != submissionID ||
		event.artifactID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq != 1 ||
		event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	reason, reasonOK := current["reason"].(string)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, []string{"submission_id", "submission_status"}) ||
		!automationImportObjectHasExactKeys(current, []string{"submission_id", "submission_status", "reason"}) ||
		previous["submission_id"] != submissionID || previous["submission_status"] != "pending_review" ||
		current["submission_id"] != submissionID || current["submission_status"] != "withdrawn" ||
		!reasonOK || strings.TrimSpace(reason) != reason || reason == "" || utf8.RuneCountInString(reason) > 1_000 {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	matchedVersion := int64(0)
	matched := 0
	for _, cancelled := range automationImportAggregateActionEvents(events, "task", event.aggregateID, "task_cancelled") {
		currentVersion, valid := validTaskArtifactCancellationForWithdrawal(
			cancelled, event, task, submission, submissionID, withdrawnAt, reason,
		)
		if valid {
			matched++
			matchedVersion = currentVersion
		}
	}
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	if matched != 1 || !eventTimeOK {
		return taskArtifactSubmissionDispositionImportProof{}, false
	}
	return taskArtifactSubmissionDispositionImportProof{
		status: "withdrawn", eventTime: eventTime, taskVersion: matchedVersion,
	}, true
}

func validTaskArtifactSubmittedImportEvent(
	events automationImportEventIndex,
	taskID, submissionID, createdAt string,
	task, submission map[string]any,
	artifacts []map[string]any,
) (taskArtifactSubmittedImportProof, bool) {
	matched := make([]automationImportWorkflowEvent, 0, 1)
	for _, event := range automationImportAggregateActionEvents(events, "task", taskID, "task_output_submitted") {
		if event.submissionID != nil && *event.submissionID == submissionID {
			matched = append(matched, event)
		}
	}
	if len(matched) != 1 {
		return taskArtifactSubmittedImportProof{}, false
	}
	event := matched[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) ||
		event.createdAt != createdAt || event.assignmentID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSubmittedImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, taskImportLifecycleStateKeys) ||
		!automationImportObjectHasExactKeys(current, taskOutputSubmittedStateKeys) ||
		previous["review_policy"] != "manual" || current["review_policy"] != "manual" ||
		(previous["status"] != "todo" && previous["status"] != "in_progress") || current["status"] != "waiting_review" ||
		current["current_submission_id"] != submissionID || current["submission_id"] != submissionID ||
		current["submitted_at"] != createdAt || current["reviewed_at"] != nil {
		return taskArtifactSubmittedImportProof{}, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalTaskVersion, finalVersionOK := automationImportInt64(task["version"])
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	currentSequence, currentSequenceOK := automationImportJSONInt64(current["submission_sequence"])
	artifactCount, countOK := automationImportJSONInt64(current["artifact_count"])
	submittedAt, submittedAtOK := submission["submitted_at"].(string)
	if !previousVersionOK || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalTaskVersion < currentVersion || !sequenceOK || !currentSequenceOK || currentSequence != sequence ||
		!countOK || artifactCount != int64(len(artifacts)) || !submittedAtOK || submittedAt != createdAt ||
		submission["submitted_by_actor_id"] != models.BuiltinOwnerActorID || submission["origin"] != "manual" {
		return taskArtifactSubmittedImportProof{}, false
	}
	artifactValues, valuesOK := current["artifacts"].([]any)
	if !valuesOK || len(artifactValues) != len(artifacts) {
		return taskArtifactSubmittedImportProof{}, false
	}
	for index, value := range artifactValues {
		snapshot, snapshotOK := value.(map[string]any)
		if !snapshotOK || !validTaskOutputArtifactImportSnapshot(snapshot, artifacts[index], false) {
			return taskArtifactSubmittedImportProof{}, false
		}
	}
	return taskArtifactSubmittedImportProof{event: event, taskVersion: currentVersion}, true
}
