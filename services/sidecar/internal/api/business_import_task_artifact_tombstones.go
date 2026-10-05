package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var taskOutputArtifactSnapshotKeys = []string{
	"id", "position", "storage_kind", "name", "mime_type", "size_bytes", "sha256",
	"requires_followup", "produced_by_actor_id", "recorded_by_actor_id",
}

var taskArtifactDeletedSnapshotKeys = []string{
	"id", "submission_id", "position", "storage_kind", "name", "mime_type", "size_bytes", "sha256",
	"requires_followup", "produced_by_actor_id", "recorded_by_actor_id",
}

type taskArtifactImportReplay struct {
	artifactID           string
	inboxID              string
	taskID               string
	submissionID         string
	submissionStatus     string
	deletedAt            string
	deletedByActorID     string
	deleteReason         string
	previousInboxVersion int64
	deletedInboxVersion  int64
	deletedInboxPrevious map[string]any
	deletedInboxCurrent  map[string]any
}

type taskArtifactImportReplays struct {
	ordered                 []taskArtifactImportReplay
	byArtifactID            map[string]taskArtifactImportReplay
	byInboxID               map[string]taskArtifactImportReplay
	taskIDs                 map[string]struct{}
	changesRequestedTaskIDs map[string]struct{}
}

type taskArtifactImportValidationIndex struct {
	events                   automationImportEventIndex
	assignments              []map[string]any
	artifactsBySubmission    map[string][]map[string]any
	submittedBySubmissionID  map[string]taskArtifactSubmittedImportValidation
	deletedEventByArtifactID map[string]automationImportWorkflowEvent
}

type taskArtifactSourceDeletedImportProof struct {
	event           automationImportWorkflowEvent
	previous        map[string]any
	current         map[string]any
	previousVersion int64
	currentVersion  int64
}

func taskArtifactImportReplayPlan(packageData businessExportPackage) (taskArtifactImportReplays, bool) {
	result := taskArtifactImportReplays{
		ordered:                 make([]taskArtifactImportReplay, 0),
		byArtifactID:            make(map[string]taskArtifactImportReplay),
		byInboxID:               make(map[string]taskArtifactImportReplay),
		taskIDs:                 make(map[string]struct{}),
		changesRequestedTaskIDs: make(map[string]struct{}),
	}
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	events, ok := automationImportWorkflowEvents(tables["workflow_events"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	artifacts, ok := automationImportRowsByID(tables["task_artifacts"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	tasks, ok := automationImportRowsByID(tables["tasks"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	submissions, ok := automationImportRowsByID(tables["task_submissions"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	inboxes, ok := automationImportRowsByID(tables["inbox_items"])
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	legacyGaps, ok := taskArtifactLegacyInboxGapImportProofs(artifacts, inboxes, events)
	if !ok {
		return taskArtifactImportReplays{}, false
	}
	artifactsBySubmission := make(map[string][]map[string]any)
	assignments := automationImportRows(tables["task_assignments"])
	for _, artifact := range artifacts {
		submissionID, submissionOK := artifact["submission_id"].(string)
		if !submissionOK {
			return taskArtifactImportReplays{}, false
		}
		artifactsBySubmission[submissionID] = append(artifactsBySubmission[submissionID], artifact)
	}
	for submissionID := range artifactsBySubmission {
		sort.Slice(artifactsBySubmission[submissionID], func(left, right int) bool {
			leftPosition, _ := automationImportInt64(artifactsBySubmission[submissionID][left]["position"])
			rightPosition, _ := automationImportInt64(artifactsBySubmission[submissionID][right]["position"])
			if leftPosition != rightPosition {
				return leftPosition < rightPosition
			}
			leftID, _ := artifactsBySubmission[submissionID][left]["id"].(string)
			rightID, _ := artifactsBySubmission[submissionID][right]["id"].(string)
			return leftID < rightID
		})
	}
	validation, ok := newTaskArtifactImportValidationIndex(events, assignments, artifactsBySubmission)
	if !ok {
		return taskArtifactImportReplays{}, false
	}

	for _, inbox := range automationImportRows(tables["inbox_items"]) {
		if inbox["source_entity_type"] != taskArtifactInboxSourceType || inbox["source_deleted_at"] == nil {
			continue
		}
		replay, valid := validTaskArtifactImportReplay(inbox, artifacts, tasks, submissions, validation)
		if !valid {
			return taskArtifactImportReplays{}, false
		}
		if _, duplicate := result.byArtifactID[replay.artifactID]; duplicate {
			return taskArtifactImportReplays{}, false
		}
		if _, duplicate := result.byInboxID[replay.inboxID]; duplicate {
			return taskArtifactImportReplays{}, false
		}
		result.ordered = append(result.ordered, replay)
		result.byArtifactID[replay.artifactID] = replay
		result.byInboxID[replay.inboxID] = replay
		result.taskIDs[replay.taskID] = struct{}{}
		if replay.submissionStatus == "changes_requested" {
			result.changesRequestedTaskIDs[replay.taskID] = struct{}{}
		}
	}
	if !addLegacyTaskArtifactImportProtection(
		&result, artifacts, tasks, submissions, legacyGaps, validation,
	) {
		return taskArtifactImportReplays{}, false
	}
	sort.Slice(result.ordered, func(left, right int) bool {
		return result.ordered[left].inboxID < result.ordered[right].inboxID
	})
	if !validTaskArtifactInboxEventReverseClosure(inboxes, events, result) {
		return taskArtifactImportReplays{}, false
	}
	return result, true
}

func newTaskArtifactImportValidationIndex(
	events automationImportEventIndex,
	assignments []map[string]any,
	artifactsBySubmission map[string][]map[string]any,
) (*taskArtifactImportValidationIndex, bool) {
	result := &taskArtifactImportValidationIndex{
		events:                   events,
		assignments:              assignments,
		artifactsBySubmission:    artifactsBySubmission,
		submittedBySubmissionID:  make(map[string]taskArtifactSubmittedImportValidation),
		deletedEventByArtifactID: make(map[string]automationImportWorkflowEvent),
	}
	for _, event := range events.byID {
		if event.aggregateType != "task" || event.action != "task_artifact_deleted" || event.artifactID == nil {
			continue
		}
		artifactID := *event.artifactID
		if _, duplicate := result.deletedEventByArtifactID[artifactID]; duplicate {
			return nil, false
		}
		result.deletedEventByArtifactID[artifactID] = event
	}
	return result, true
}

// Schema versions before the Task Artifact Inbox projection did not backfill
// Inbox rows for existing follow-up Artifacts. A later export can therefore
// contain a valid deleted follow-up Artifact with complete Task history but no
// Inbox aggregate. Protect those Tasks so ended assignments are restored via
// the history path, while requiring the same submission, disposition, and
// deletion proofs as a projected Artifact.
func addLegacyTaskArtifactImportProtection(
	result *taskArtifactImportReplays,
	artifacts, tasks, submissions map[string]map[string]any,
	legacyGaps map[string]struct{},
	validation *taskArtifactImportValidationIndex,
) bool {
	events := validation.events
	for artifactID, artifact := range artifacts {
		if _, projected := result.byArtifactID[artifactID]; projected {
			continue
		}
		requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
		deletedAt, deletedAtOK := automationImportOptionalString(artifact["deleted_at"])
		if !followupOK || !deletedAtOK {
			return false
		}
		if requiresFollowup != 1 || deletedAt == nil {
			continue
		}
		if _, provenLegacyGap := legacyGaps[artifactID]; !provenLegacyGap {
			return false
		}
		taskID, taskIDOK := artifact["task_id"].(string)
		submissionID, submissionIDOK := artifact["submission_id"].(string)
		deletedBy, deletedByOK := artifact["deleted_by_actor_id"].(string)
		deleteReason, reasonOK := artifact["delete_reason"].(string)
		createdAt, createdAtOK := artifact["created_at"].(string)
		createdTime, createdTimeOK := automationImportTime(createdAt)
		deletedTime, deletedTimeOK := automationImportTime(*deletedAt)
		if !validCanonicalAutomationUUID(artifactID) || !taskIDOK || !validCanonicalAutomationUUID(taskID) ||
			!submissionIDOK || !validCanonicalAutomationUUID(submissionID) || !deletedByOK ||
			deletedBy != models.BuiltinOwnerActorID || !reasonOK || strings.TrimSpace(deleteReason) == "" ||
			strings.TrimSpace(deleteReason) != deleteReason || utf8.RuneCountInString(deleteReason) > 1_000 ||
			!createdAtOK || !createdTimeOK || !deletedTimeOK || deletedTime.Before(createdTime) {
			return false
		}
		task, taskExists := tasks[taskID]
		submission, submissionExists := submissions[submissionID]
		if !taskExists || !submissionExists || submission["task_id"] != taskID {
			return false
		}
		disposition, dispositionOK := validTaskArtifactSubmissionDispositionImportEvent(
			events, taskID, submissionID, task, submission,
		)
		submitted, submittedOK := validation.validSubmittedEvent(
			taskID, submissionID, createdAt, task, submission, artifact,
		)
		deleteTaskVersion, deletionOK := validTaskArtifactDeletedImportEvent(
			validation, taskID, submissionID, artifactID, *deletedAt, deleteReason,
			artifact, task, nil, submitted,
		)
		if !dispositionOK || !submittedOK || !deletionOK || createdTime.After(disposition.eventTime) ||
			disposition.eventTime.After(deletedTime) || submitted.taskVersion >= disposition.taskVersion ||
			disposition.taskVersion > deleteTaskVersion {
			return false
		}
		result.taskIDs[taskID] = struct{}{}
		if disposition.status == "changes_requested" {
			result.changesRequestedTaskIDs[taskID] = struct{}{}
		}
	}
	return true
}

func validTaskArtifactImportReplay(
	inbox map[string]any,
	artifacts map[string]map[string]any,
	tasks map[string]map[string]any,
	submissions map[string]map[string]any,
	validation *taskArtifactImportValidationIndex,
) (taskArtifactImportReplay, bool) {
	events := validation.events
	inboxID, inboxIDOK := inbox["id"].(string)
	artifactID, artifactIDOK := inbox["source_entity_id"].(string)
	sourceKey, sourceKeyOK := inbox["source_event_key"].(string)
	deletedAt, deletedAtOK := automationImportOptionalString(inbox["source_deleted_at"])
	payloadJSON, payloadJSONOK := inbox["payload_json"].(string)
	createdAt, createdAtOK := inbox["created_at"].(string)
	updatedAt, updatedAtOK := inbox["updated_at"].(string)
	resolutionPolicy, resolutionPolicyOK := inbox["resolution_policy"].(string)
	status, statusOK := inbox["status"].(string)
	if !inboxIDOK || !validCanonicalAutomationUUID(inboxID) || !artifactIDOK || !validCanonicalAutomationUUID(artifactID) ||
		!sourceKeyOK || sourceKey != taskArtifactFollowupEventKey(artifactID) || !deletedAtOK || deletedAt == nil ||
		!payloadJSONOK || !createdAtOK || !updatedAtOK || inbox["kind"] != "event" ||
		!resolutionPolicyOK || (resolutionPolicy != "manual" && resolutionPolicy != "all_required_tasks_done") ||
		!statusOK || (status != "open" && status != "tracking" && status != "resolved" && status != "dismissed") {
		return taskArtifactImportReplay{}, false
	}
	deletedTime, deletedTimeOK := automationImportTime(*deletedAt)
	createdTime, createdTimeOK := automationImportTime(createdAt)
	updatedTime, updatedTimeOK := automationImportTime(updatedAt)
	if !deletedTimeOK || !createdTimeOK || !updatedTimeOK || deletedTime.Before(createdTime) || updatedTime.Before(deletedTime) {
		return taskArtifactImportReplay{}, false
	}
	artifact, artifactExists := artifacts[artifactID]
	if !artifactExists {
		return taskArtifactImportReplay{}, false
	}
	taskID, taskIDOK := artifact["task_id"].(string)
	submissionID, submissionIDOK := artifact["submission_id"].(string)
	requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
	artifactDeletedAt, artifactDeletedOK := automationImportOptionalString(artifact["deleted_at"])
	deletedBy, deletedByOK := automationImportOptionalString(artifact["deleted_by_actor_id"])
	deleteReason, reasonOK := automationImportOptionalString(artifact["delete_reason"])
	artifactCreatedAt, artifactCreatedOK := artifact["created_at"].(string)
	if !taskIDOK || !validCanonicalAutomationUUID(taskID) || !submissionIDOK || !validCanonicalAutomationUUID(submissionID) ||
		!followupOK || requiresFollowup != 1 || !artifactDeletedOK || artifactDeletedAt == nil || *artifactDeletedAt != *deletedAt ||
		!deletedByOK || deletedBy == nil || *deletedBy != models.BuiltinOwnerActorID || !reasonOK || deleteReason == nil ||
		strings.TrimSpace(*deleteReason) == "" || strings.TrimSpace(*deleteReason) != *deleteReason ||
		!artifactCreatedOK || artifactCreatedAt != createdAt {
		return taskArtifactImportReplay{}, false
	}
	task, taskExists := tasks[taskID]
	submission, submissionExists := submissions[submissionID]
	if !taskExists || !submissionExists || submission["task_id"] != taskID {
		return taskArtifactImportReplay{}, false
	}
	disposition, dispositionOK := validTaskArtifactSubmissionDispositionImportEvent(
		events, taskID, submissionID, task, submission,
	)
	if !dispositionOK {
		return taskArtifactImportReplay{}, false
	}
	payload, payloadOK := validTaskArtifactImportPayload(payloadJSON, artifact, submission)
	if !payloadOK {
		return taskArtifactImportReplay{}, false
	}
	submittedEvent, submittedOK := validation.validSubmittedEvent(
		taskID, submissionID, artifactCreatedAt, task, submission, artifact,
	)
	if !submittedOK || !validTaskArtifactProjectedImportEvent(
		events, inboxID, artifactID, sourceKey, artifactCreatedAt, submission, artifact, payload, submittedEvent.event,
	) {
		return taskArtifactImportReplay{}, false
	}
	sourceDeleted, deletedOK := validTaskArtifactSourceDeletedImportEvent(
		events, inboxID, artifactID, sourceKey, *deletedAt, payload, inbox,
	)
	deleteTaskVersion, artifactDeletedOK := validTaskArtifactDeletedImportEvent(
		validation, taskID, submissionID, artifactID, *deletedAt, *deleteReason,
		artifact, task, &sourceDeleted.event, submittedEvent,
	)
	submittedTime, submittedTimeOK := automationImportTime(artifactCreatedAt)
	if !deletedOK || !artifactDeletedOK || !submittedTimeOK || submittedTime.After(disposition.eventTime) ||
		disposition.eventTime.After(deletedTime) || submittedEvent.taskVersion >= disposition.taskVersion ||
		disposition.taskVersion > deleteTaskVersion {
		return taskArtifactImportReplay{}, false
	}
	return taskArtifactImportReplay{
		artifactID: artifactID, inboxID: inboxID, taskID: taskID, submissionID: submissionID,
		submissionStatus: disposition.status, deletedAt: *deletedAt,
		deletedByActorID: *deletedBy, deleteReason: *deleteReason,
		previousInboxVersion: sourceDeleted.previousVersion, deletedInboxVersion: sourceDeleted.currentVersion,
		deletedInboxPrevious: sourceDeleted.previous, deletedInboxCurrent: sourceDeleted.current,
	}, true
}

func validTaskArtifactImportPayload(
	raw string,
	artifact, submission map[string]any,
) (map[string]any, bool) {
	artifactID, artifactIDOK := artifact["id"].(string)
	artifactName, nameOK := artifact["name"].(string)
	storageKind, storageOK := artifact["storage_kind"].(string)
	taskID, taskIDOK := artifact["task_id"].(string)
	submissionID, submissionIDOK := submission["id"].(string)
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	decoded, decodedOK := automationImportJSONObject(raw)
	taskTitle, titleOK := decoded["task_title"].(string)
	if !artifactIDOK || !nameOK || !storageOK || !taskIDOK || !submissionIDOK ||
		!sequenceOK || sequence < 1 || !decodedOK || !titleOK || strings.TrimSpace(taskTitle) != taskTitle ||
		utf8.RuneCountInString(taskTitle) < 2 || utf8.RuneCountInString(taskTitle) > 200 {
		return nil, false
	}
	expected := map[string]any{
		"artifact_id": artifactID, "artifact_name": artifactName, "storage_kind": storageKind,
		"task_id": taskID, "task_title": taskTitle, "submission_id": submissionID,
		"submission_sequence": sequence,
	}
	if len(decoded) == 9 {
		projectID, projectIDOK := decoded["project_id"].(string)
		projectName, projectNameOK := decoded["project_name"].(string)
		if !projectIDOK || !validCanonicalAutomationUUID(projectID) || !projectNameOK ||
			strings.TrimSpace(projectName) != projectName || utf8.RuneCountInString(projectName) < 2 ||
			utf8.RuneCountInString(projectName) > 100 {
			return nil, false
		}
		expected["project_id"] = projectID
		expected["project_name"] = projectName
	} else if len(decoded) != 7 {
		return nil, false
	}
	if !taskArtifactImportObjectEquals(decoded, expected) {
		return nil, false
	}
	canonical, err := json.Marshal(expected)
	return expected, err == nil && raw == string(canonical)
}

func validTaskArtifactSourceDeletedImportEvent(
	events automationImportEventIndex,
	inboxID, artifactID, sourceKey, deletedAt string,
	payload, finalRow map[string]any,
) (taskArtifactSourceDeletedImportProof, bool) {
	deleted := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_deleted")
	if len(deleted) != 1 {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	event := deleted[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != deletedAt ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK ||
		!automationImportObjectHasExactKeys(previous, projectCompletionInboxEventStateKeys) ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) ||
		previous["source_deleted_at"] != nil || current["source_deleted_at"] != deletedAt ||
		(previous["status"] != "resolved" && previous["status"] != "dismissed") ||
		current["source_entity_type"] != taskArtifactInboxSourceType || current["source_entity_id"] != artifactID ||
		current["source_event_key"] != sourceKey || !taskArtifactImportValueEquals(current["payload_json"], payload) {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	for key, value := range previous {
		if key != "source_deleted_at" && key != "version" && !taskArtifactImportValueEquals(value, current[key]) {
			return taskArtifactSourceDeletedImportProof{}, false
		}
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	if !previousVersionOK || previousVersion < 2 || !currentVersionOK || currentVersion != previousVersion+1 ||
		!validTaskArtifactInboxPostDeleteImportHistory(events, inboxID, deletedAt, current, finalRow) {
		return taskArtifactSourceDeletedImportProof{}, false
	}
	return taskArtifactSourceDeletedImportProof{
		event: event, previous: previous, current: current,
		previousVersion: previousVersion, currentVersion: currentVersion,
	}, true
}

func validTaskArtifactDeletedImportEvent(
	validation *taskArtifactImportValidationIndex,
	taskID, submissionID, artifactID, deletedAt, deleteReason string,
	artifact, task map[string]any,
	sourceDeletedEvent *automationImportWorkflowEvent,
	submitted taskArtifactSubmittedImportProof,
) (int64, bool) {
	event, exists := validation.deletedEventByArtifactID[artifactID]
	if !exists || event.aggregateType != "task" || event.aggregateID != taskID ||
		event.action != "task_artifact_deleted" || event.artifactID == nil || *event.artifactID != artifactID {
		return 0, false
	}
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, true) || event.createdAt != deletedAt ||
		event.submissionID == nil || *event.submissionID != submissionID ||
		event.assignmentID != nil || event.agentRunID != nil || event.commandSeq == nil || *event.commandSeq != 1 ||
		event.previousJSON == nil || event.currentJSON == nil || event.requestID == nil {
		return 0, false
	}
	if sourceDeletedEvent != nil && (sourceDeletedEvent.requestID == nil || *event.requestID != *sourceDeletedEvent.requestID) {
		return 0, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, []string{"artifact", "version"}) ||
		!automationImportObjectHasExactKeys(current, []string{"artifact_id", "deleted_at", "reason", "version"}) ||
		current["artifact_id"] != artifactID || current["deleted_at"] != deletedAt || current["reason"] != deleteReason {
		return 0, false
	}
	snapshot, snapshotOK := previous["artifact"].(map[string]any)
	if !snapshotOK || !validTaskOutputArtifactImportSnapshot(snapshot, artifact, true) {
		return 0, false
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	finalVersion, finalVersionOK := automationImportInt64(task["version"])
	if !previousVersionOK || previousVersion < submitted.taskVersion || !currentVersionOK || currentVersion != previousVersion+1 ||
		!finalVersionOK || finalVersion < currentVersion {
		return 0, false
	}
	return previousVersion, true
}

func validTaskOutputArtifactImportSnapshot(snapshot, artifact map[string]any, includeSubmission bool) bool {
	keys := taskOutputArtifactSnapshotKeys
	if includeSubmission {
		keys = taskArtifactDeletedSnapshotKeys
	}
	if !automationImportObjectHasExactKeys(snapshot, keys) {
		return false
	}
	requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
	position, positionOK := automationImportInt64(artifact["position"])
	if !followupOK || (requiresFollowup != 0 && requiresFollowup != 1) || !positionOK {
		return false
	}
	expected := map[string]any{
		"id": artifact["id"], "position": position, "storage_kind": artifact["storage_kind"],
		"name": artifact["name"], "mime_type": artifact["mime_type"], "size_bytes": artifact["size_bytes"],
		"sha256": artifact["sha256"], "requires_followup": requiresFollowup == 1,
		"produced_by_actor_id": artifact["produced_by_actor_id"],
		"recorded_by_actor_id": artifact["recorded_by_actor_id"],
	}
	if includeSubmission {
		expected["submission_id"] = artifact["submission_id"]
	}
	return taskArtifactImportObjectEquals(snapshot, expected)
}

func validTaskArtifactImportEventEnvelope(event automationImportWorkflowEvent, actorID string, requireRequest bool) bool {
	if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != actorID {
		return false
	}
	if !requireRequest {
		return event.requestID == nil
	}
	return event.requestID != nil && validCanonicalAutomationUUID(*event.requestID)
}

func taskArtifactImportObjectEquals(actual, expected map[string]any) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		if !exists || !taskArtifactImportValueEquals(actualValue, expectedValue) {
			return false
		}
	}
	return true
}

func taskArtifactImportValueEquals(actual, expected any) bool {
	if actualInteger, actualOK := importInteger(actual); actualOK {
		expectedInteger, expectedOK := importInteger(expected)
		return expectedOK && actualInteger == expectedInteger
	}
	if actualObject, actualOK := actual.(map[string]any); actualOK {
		expectedObject, expectedOK := expected.(map[string]any)
		return expectedOK && taskArtifactImportObjectEquals(actualObject, expectedObject)
	}
	if actualValues, actualOK := actual.([]any); actualOK {
		expectedValues, expectedOK := expected.([]any)
		if !expectedOK || len(actualValues) != len(expectedValues) {
			return false
		}
		for index := range actualValues {
			if !taskArtifactImportValueEquals(actualValues[index], expectedValues[index]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}

func insertTaskArtifactImportRows(
	tx *gorm.DB,
	table businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return insertBusinessImportRows(tx, table)
	}
	idIndex := columnIndex(table.Columns, "id")
	deletedAtIndex := columnIndex(table.Columns, "deleted_at")
	deletedByIndex := columnIndex(table.Columns, "deleted_by_actor_id")
	reasonIndex := columnIndex(table.Columns, "delete_reason")
	if idIndex < 0 || deletedAtIndex < 0 || deletedByIndex < 0 || reasonIndex < 0 {
		return errors.New("Task Artifact import columns are incomplete")
	}
	staged := businessExportTable{Name: table.Name, Columns: table.Columns, Rows: make([][]any, 0, len(table.Rows))}
	for _, row := range table.Rows {
		artifactID, _ := row[idIndex].(string)
		if _, replay := replays.byArtifactID[artifactID]; !replay {
			staged.Rows = append(staged.Rows, row)
			continue
		}
		copyRow := append([]any(nil), row...)
		copyRow[deletedAtIndex] = nil
		copyRow[deletedByIndex] = nil
		copyRow[reasonIndex] = nil
		staged.Rows = append(staged.Rows, copyRow)
	}
	return insertBusinessImportRows(tx, staged)
}

func finalizeTaskArtifactImportReplays(
	tx *gorm.DB,
	artifactTable businessExportTable,
	inboxTable businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return nil
	}
	artifactRows, ok := importRowsByStringColumn(artifactTable, "id")
	if !ok {
		return errors.New("Task Artifact import rows are invalid")
	}
	inboxRows, ok := importRowsByStringColumn(inboxTable, "id")
	if !ok {
		return errors.New("Task Artifact Inbox import rows are invalid")
	}
	for _, replay := range replays.ordered {
		finalInbox, inboxExists := inboxRows[replay.inboxID]
		finalArtifact, artifactExists := artifactRows[replay.artifactID]
		if !inboxExists || !artifactExists {
			return errors.New("Task Artifact import replay row is missing")
		}
		finalInboxVersion, versionOK := importInteger(finalInbox[columnIndex(inboxTable.Columns, "version")])
		finalUpdatedAt, updatedOK := finalInbox[columnIndex(inboxTable.Columns, "updated_at")].(string)
		if !versionOK || !updatedOK {
			return errors.New("Task Artifact Inbox import replay state is invalid")
		}
		inboxResult := tx.Exec(`
			UPDATE inbox_items
			SET source_deleted_at = ?, version = ?, updated_at = ?
			WHERE id = ? AND source_deleted_at IS NULL AND version = ?
		`, replay.deletedAt, replay.deletedInboxVersion, replay.deletedAt, replay.inboxID, replay.previousInboxVersion)
		if inboxResult.Error != nil {
			return fmt.Errorf("restore Task Artifact Inbox tombstone: %w", inboxResult.Error)
		}
		if inboxResult.RowsAffected != 1 {
			return errors.New("Task Artifact Inbox replay did not match its staged row")
		}
		artifactResult := tx.Exec(`
			UPDATE task_artifacts
			SET deleted_at = ?, deleted_by_actor_id = ?, delete_reason = ?
			WHERE id = ? AND deleted_at IS NULL
		`, replay.deletedAt, replay.deletedByActorID, replay.deleteReason, replay.artifactID)
		if artifactResult.Error != nil {
			return fmt.Errorf("restore Task Artifact soft deletion: %w", artifactResult.Error)
		}
		if artifactResult.RowsAffected != 1 {
			return errors.New("Task Artifact replay did not match its staged row")
		}
		if finalInboxVersion != replay.deletedInboxVersion {
			updates, valid := taskArtifactInboxFinalImportUpdates(inboxTable, finalInbox)
			if !valid {
				return errors.New("Task Artifact Inbox final replay state is invalid")
			}
			result := tx.Table("inbox_items").Where(
				"id = ? AND source_deleted_at = ? AND version = ?",
				replay.inboxID, replay.deletedAt, replay.deletedInboxVersion,
			).Updates(updates)
			if result.Error != nil {
				return fmt.Errorf("restore Task Artifact Inbox post-deletion history: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return errors.New("Task Artifact Inbox post-deletion replay did not match its tombstone")
			}
		} else if finalUpdatedAt != replay.deletedAt {
			return errors.New("Task Artifact Inbox deletion timestamp is inconsistent")
		}
		if err := verifyExactBusinessImportRow(tx, artifactTable, finalArtifact); err != nil {
			return err
		}
		if err := verifyExactBusinessImportRow(tx, inboxTable, finalInbox); err != nil {
			return err
		}
	}
	return nil
}

func importRowsByStringColumn(table businessExportTable, column string) (map[string][]any, bool) {
	index := columnIndex(table.Columns, column)
	if index < 0 {
		return nil, false
	}
	result := make(map[string][]any, len(table.Rows))
	for _, row := range table.Rows {
		value, ok := row[index].(string)
		if !ok || value == "" {
			return nil, false
		}
		if _, duplicate := result[value]; duplicate {
			return nil, false
		}
		result[value] = row
	}
	return result, true
}

func verifyExactBusinessImportRow(tx *gorm.DB, table businessExportTable, expected []any) error {
	idIndex := columnIndex(table.Columns, "id")
	if idIndex < 0 {
		return errors.New("business import replay table has no id")
	}
	id, ok := expected[idIndex].(string)
	if !ok || id == "" {
		return errors.New("business import replay row id is invalid")
	}
	quoted := make([]string, len(table.Columns))
	for index, column := range table.Columns {
		quoted[index] = quoteIdentifier(column)
	}
	actual := make([]any, len(table.Columns))
	destinations := make([]any, len(actual))
	for index := range actual {
		destinations[index] = &actual[index]
	}
	row := tx.Raw(
		"SELECT "+strings.Join(quoted, ",")+" FROM "+quoteIdentifier(table.Name)+" WHERE id = ?", id,
	).Row()
	if err := row.Scan(destinations...); err != nil {
		return fmt.Errorf("read restored import row %s: %w", table.Name, err)
	}
	want, err := canonicalBusinessImportKey(append([]any(nil), expected...))
	if err != nil {
		return err
	}
	got, err := canonicalBusinessImportKey(actual)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("restored import row %s does not match its package", table.Name)
	}
	return nil
}
