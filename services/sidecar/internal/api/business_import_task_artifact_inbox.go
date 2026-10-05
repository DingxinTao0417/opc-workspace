package api

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type taskArtifactInboxImportIdentity struct {
	artifactID string
	sourceKey  string
	payload    map[string]any
}

const taskArtifactInboxGapMigrationAction = "migration_task_artifact_inbox_gap"

func validTaskArtifactInboxEventReverseClosure(
	inboxes map[string]map[string]any,
	events automationImportEventIndex,
	replays taskArtifactImportReplays,
) bool {
	projectedCounts := make(map[string]int)
	deletedCounts := make(map[string]int)
	candidates := make(map[string]struct{})
	for _, event := range events.byID {
		if event.aggregateType != "inbox_item" || (event.action != "source_projected" && event.action != "source_deleted") {
			continue
		}
		finalRow, finalExists := inboxes[event.aggregateID]
		switch event.action {
		case "source_projected":
			if event.currentJSON == nil {
				return false
			}
			current, currentOK := automationImportJSONObject(*event.currentJSON)
			identity, candidate, stateOK := taskArtifactInboxImportIdentityFromState(current)
			if !candidate {
				continue
			}
			if !currentOK || !stateOK || !finalExists ||
				!validTaskArtifactProjectedReverseImportEvent(event, current, identity, finalRow) {
				return false
			}
			candidates[event.aggregateID] = struct{}{}
			projectedCounts[event.aggregateID]++
		case "source_deleted":
			if event.previousJSON == nil || event.currentJSON == nil {
				return false
			}
			previous, previousOK := automationImportJSONObject(*event.previousJSON)
			current, currentOK := automationImportJSONObject(*event.currentJSON)
			previousIdentity, previousCandidate, previousStateOK := taskArtifactInboxImportIdentityFromState(previous)
			currentIdentity, currentCandidate, currentStateOK := taskArtifactInboxImportIdentityFromState(current)
			if !previousCandidate && !currentCandidate {
				continue
			}
			if !previousOK || !currentOK || !previousStateOK || !currentStateOK || !finalExists ||
				previousIdentity.artifactID != currentIdentity.artifactID ||
				previousIdentity.sourceKey != currentIdentity.sourceKey ||
				!taskArtifactImportObjectEquals(previousIdentity.payload, currentIdentity.payload) {
				return false
			}
			if _, planned := replays.byInboxID[event.aggregateID]; !planned {
				return false
			}
			candidates[event.aggregateID] = struct{}{}
			deletedCounts[event.aggregateID]++
		}
	}
	for inboxID, row := range inboxes {
		_, candidate, valid := taskArtifactInboxImportIdentityFromRow(row)
		if !candidate {
			continue
		}
		if !valid {
			return false
		}
		candidates[inboxID] = struct{}{}
	}
	for inboxID := range candidates {
		row, exists := inboxes[inboxID]
		deletedAt, deletedAtOK := automationImportOptionalString(row["source_deleted_at"])
		if !exists || !deletedAtOK || projectedCounts[inboxID] != 1 {
			return false
		}
		_, replayed := replays.byInboxID[inboxID]
		if deletedAt == nil {
			if replayed || deletedCounts[inboxID] != 0 {
				return false
			}
		} else if !replayed || deletedCounts[inboxID] != 1 {
			return false
		}
	}
	return true
}

func taskArtifactInboxImportIdentityFromRow(
	row map[string]any,
) (taskArtifactInboxImportIdentity, bool, bool) {
	sourceType, typeOK := row["source_entity_type"].(string)
	artifactID, idOK := row["source_entity_id"].(string)
	sourceKey, keyOK := row["source_event_key"].(string)
	payloadRaw, payloadRawOK := row["payload_json"].(string)
	payload, payloadOK := automationImportJSONObject(payloadRaw)
	keyArtifactID, reservedKey := taskArtifactImportArtifactIDFromEventKey(sourceKey)
	payloadArtifactID, payloadArtifactOK := payload["artifact_id"].(string)
	payloadTaskID, payloadTaskOK := payload["task_id"].(string)
	payloadSubmissionID, payloadSubmissionOK := payload["submission_id"].(string)
	reservedPayload := payloadOK && payloadArtifactOK && validCanonicalAutomationUUID(payloadArtifactID) &&
		payloadTaskOK && validCanonicalAutomationUUID(payloadTaskID) && payloadSubmissionOK &&
		validCanonicalAutomationUUID(payloadSubmissionID) && idOK && payloadArtifactID == artifactID
	candidate := sourceType == taskArtifactInboxSourceType || reservedKey || reservedPayload
	if !candidate {
		return taskArtifactInboxImportIdentity{}, false, true
	}
	if !typeOK || sourceType != taskArtifactInboxSourceType || !idOK || !validCanonicalAutomationUUID(artifactID) ||
		!keyOK || !reservedKey || keyArtifactID != artifactID || !payloadRawOK || !payloadOK ||
		payloadArtifactID != artifactID {
		return taskArtifactInboxImportIdentity{}, true, false
	}
	return taskArtifactInboxImportIdentity{artifactID: artifactID, sourceKey: sourceKey, payload: payload}, true, true
}

func taskArtifactInboxImportIdentityFromState(
	state map[string]any,
) (taskArtifactInboxImportIdentity, bool, bool) {
	sourceType, typeOK := state["source_entity_type"].(string)
	artifactID, idOK := state["source_entity_id"].(string)
	sourceKey, keyOK := state["source_event_key"].(string)
	payload, payloadOK := state["payload_json"].(map[string]any)
	_, payloadHasArtifact := payload["artifact_id"]
	_, payloadHasTask := payload["task_id"]
	_, payloadHasSubmission := payload["submission_id"]
	keyArtifactID, reservedKey := taskArtifactImportArtifactIDFromEventKey(sourceKey)
	candidate := sourceType == taskArtifactInboxSourceType || reservedKey ||
		(payloadOK && payloadHasArtifact && payloadHasTask && payloadHasSubmission)
	if !candidate {
		return taskArtifactInboxImportIdentity{}, false, true
	}
	if !automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) || !typeOK ||
		sourceType != taskArtifactInboxSourceType || !idOK || !validCanonicalAutomationUUID(artifactID) ||
		!keyOK || !reservedKey || keyArtifactID != artifactID || !payloadOK || payload["artifact_id"] != artifactID {
		return taskArtifactInboxImportIdentity{}, true, false
	}
	return taskArtifactInboxImportIdentity{artifactID: artifactID, sourceKey: sourceKey, payload: payload}, true, true
}

func taskArtifactImportArtifactIDFromEventKey(value string) (string, bool) {
	const prefix = "task-artifact:"
	const suffix = ":followup"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	artifactID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return artifactID, validCanonicalAutomationUUID(artifactID) && value == taskArtifactFollowupEventKey(artifactID)
}

func validTaskArtifactProjectedReverseImportEvent(
	event automationImportWorkflowEvent,
	state map[string]any,
	identity taskArtifactInboxImportIdentity,
	finalRow map[string]any,
) bool {
	createdAt, createdAtOK := finalRow["created_at"].(string)
	finalPayloadRaw, payloadOK := finalRow["payload_json"].(string)
	finalPayload, finalPayloadOK := automationImportJSONObject(finalPayloadRaw)
	if !createdAtOK || !payloadOK || !finalPayloadOK ||
		!validTaskArtifactImportEventEnvelope(event, models.BuiltinSystemActorID, true) || event.createdAt != createdAt ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil ||
		state["kind"] != "event" || state["source_deleted_at"] != nil || state["resolution_policy"] != "manual" ||
		state["status"] != "open" || state["version"] == nil || finalRow["kind"] != state["kind"] ||
		finalRow["source_entity_type"] != taskArtifactInboxSourceType || finalRow["source_entity_id"] != identity.artifactID ||
		finalRow["source_event_key"] != identity.sourceKey || !taskArtifactImportObjectEquals(finalPayload, identity.payload) {
		return false
	}
	version, versionOK := automationImportJSONInt64(state["version"])
	return versionOK && version == 1
}

func taskArtifactLegacyInboxGapImportProofs(
	artifacts, inboxes map[string]map[string]any,
	events automationImportEventIndex,
) (map[string]struct{}, bool) {
	result := make(map[string]struct{})
	inboxEvidence := taskArtifactImportInboxEvidenceIDs(inboxes, events)
	for _, event := range events.byID {
		if event.action != taskArtifactInboxGapMigrationAction {
			continue
		}
		if event.aggregateType != "task" || event.artifactID == nil || event.submissionID == nil ||
			event.assignmentID != nil || event.agentRunID != nil || event.requestID != nil || event.commandSeq != nil ||
			event.previousJSON != nil || event.currentJSON == nil ||
			!validTaskArtifactImportEventEnvelope(event, models.BuiltinOwnerActorID, false) {
			return nil, false
		}
		artifactID := *event.artifactID
		artifact, exists := artifacts[artifactID]
		expectedEventID, eventIDOK := taskArtifactInboxGapMigrationEventID(artifactID)
		taskID, taskIDOK := artifact["task_id"].(string)
		submissionID, submissionIDOK := artifact["submission_id"].(string)
		createdAt, createdAtOK := artifact["created_at"].(string)
		requiresFollowup, followupOK := automationImportInt64(artifact["requires_followup"])
		markerTime, markerTimeOK := taskArtifactImportTime(event.createdAt)
		artifactTime, artifactTimeOK := taskArtifactImportTime(createdAt)
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		currentRequiresFollowup, currentFollowupOK := automationImportJSONInt64(current["requires_followup"])
		_, hasInboxEvidence := inboxEvidence[artifactID]
		if !exists || !eventIDOK || event.id != expectedEventID || !taskIDOK || event.aggregateID != taskID ||
			!submissionIDOK || *event.submissionID != submissionID || !createdAtOK || !followupOK || requiresFollowup != 1 ||
			!markerTimeOK || !artifactTimeOK || markerTime.Before(artifactTime) || !currentOK || !currentFollowupOK ||
			currentRequiresFollowup != 1 || !automationImportObjectHasExactKeys(current, []string{
			"source", "artifact_id", "task_id", "submission_id", "artifact_created_at", "requires_followup",
		}) || current["source"] != "schema_v51_migration" || current["artifact_id"] != artifactID ||
			current["task_id"] != taskID || current["submission_id"] != submissionID || current["artifact_created_at"] != createdAt ||
			hasInboxEvidence {
			return nil, false
		}
		if _, duplicate := result[artifactID]; duplicate {
			return nil, false
		}
		result[artifactID] = struct{}{}
	}
	return result, true
}

func taskArtifactInboxGapMigrationEventID(artifactID string) (string, bool) {
	if !validCanonicalAutomationUUID(artifactID) {
		return "", false
	}
	derived := artifactID[:14] + "6" + artifactID[15:]
	return derived, validCanonicalAutomationUUID(derived)
}

func taskArtifactImportInboxEvidenceIDs(
	inboxes map[string]map[string]any,
	events automationImportEventIndex,
) map[string]struct{} {
	result := make(map[string]struct{})
	for _, inbox := range inboxes {
		if artifactID, ok := inbox["source_entity_id"].(string); ok {
			result[artifactID] = struct{}{}
		}
	}
	for _, event := range events.byID {
		if event.aggregateType != "inbox_item" {
			continue
		}
		for _, raw := range []*string{event.previousJSON, event.currentJSON} {
			if raw == nil {
				continue
			}
			state, ok := automationImportJSONObject(*raw)
			if !ok {
				continue
			}
			if artifactID, ok := state["source_entity_id"].(string); ok {
				result[artifactID] = struct{}{}
			}
			if sourceKey, ok := state["source_event_key"].(string); ok {
				if artifactID, reserved := taskArtifactImportArtifactIDFromEventKey(sourceKey); reserved {
					result[artifactID] = struct{}{}
				}
			}
			if payload, ok := state["payload_json"].(map[string]any); ok {
				if artifactID, ok := payload["artifact_id"].(string); ok {
					result[artifactID] = struct{}{}
				}
			}
		}
	}
	return result
}

func validTaskArtifactProjectedImportEvent(
	events automationImportEventIndex,
	inboxID, artifactID, sourceKey, createdAt string,
	submission, artifact, payload map[string]any,
	submittedEvent automationImportWorkflowEvent,
) bool {
	projected := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_projected")
	if len(projected) != 1 {
		return false
	}
	event := projected[0]
	if !validTaskArtifactImportEventEnvelope(event, models.BuiltinSystemActorID, true) || event.createdAt != createdAt ||
		event.requestID == nil || submittedEvent.requestID == nil || *event.requestID != *submittedEvent.requestID ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	artifactName, nameOK := artifact["name"].(string)
	taskTitle, titleOK := payload["task_title"].(string)
	sequence, sequenceOK := automationImportInt64(submission["sequence"])
	priority, priorityOK := current["priority"].(string)
	_, priorityValid := validPriorities[priority]
	if !ok || !nameOK || !titleOK || !priorityOK || !priorityValid || !sequenceOK ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) {
		return false
	}
	expected := map[string]any{
		"kind": "event", "title": taskArtifactFollowupTitle(artifactName),
		"summary":            fmt.Sprintf("任务「%s」第 %d 批产出已明确标记为需要后续处理。", taskTitle, sequence),
		"source_entity_type": taskArtifactInboxSourceType, "source_entity_id": artifactID,
		"source_event_key": sourceKey, "source_deleted_at": nil, "priority": priority,
		"resolution_policy": "manual", "status": "open", "due_at": nil, "read_at": nil,
		"triaged_at": nil, "snoozed_until": nil, "resolved_by_actor_id": nil, "resolved_at": nil,
		"resolution_reason": nil, "resolution_mode": nil, "dismissed_by_actor_id": nil,
		"dismissed_at": nil, "dismiss_reason": nil, "payload_json": payload, "version": int64(1),
	}
	return taskArtifactImportObjectEquals(current, expected)
}

func validTaskArtifactInboxPostDeleteImportHistory(
	events automationImportEventIndex,
	inboxID, deletedAt string,
	deletedState, finalRow map[string]any,
) bool {
	finalVersion, finalVersionOK := automationImportInt64(finalRow["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(deletedState["version"])
	finalUpdatedAt, updatedAtOK := finalRow["updated_at"].(string)
	lastTime, deletedTimeOK := automationImportTime(deletedAt)
	finalUpdatedTime, finalUpdatedTimeOK := automationImportTime(finalUpdatedAt)
	if !finalVersionOK || !currentVersionOK || finalVersion < currentVersion || !updatedAtOK ||
		!deletedTimeOK || !finalUpdatedTimeOK || finalUpdatedTime.Before(lastTime) {
		return false
	}
	state := deletedState
	consumed := make(map[string]struct{})
	for currentVersion < finalVersion {
		matched := make([]struct {
			event   automationImportWorkflowEvent
			current map[string]any
		}, 0, 1)
		for _, candidate := range automationImportAggregateEvents(events, "inbox_item", inboxID) {
			if candidate.action == "source_projected" || candidate.action == "source_deleted" ||
				candidate.previousJSON == nil || candidate.currentJSON == nil {
				continue
			}
			previous, previousOK := automationImportJSONObject(*candidate.previousJSON)
			next, nextOK := automationImportJSONObject(*candidate.currentJSON)
			previousState, previousStateOK := taskArtifactInboxImportEventState(previous, false)
			nextState, nextStateOK := taskArtifactInboxImportEventState(next, true)
			previousEventVersion, versionOK := automationImportJSONInt64(previousState["version"])
			if !previousOK || !nextOK || !previousStateOK || !nextStateOK || !versionOK || previousEventVersion != currentVersion ||
				!taskArtifactImportObjectEquals(previousState, state) {
				continue
			}
			if !validTaskArtifactInboxPostDeleteImportEvent(candidate, previous, next, previousState, nextState, lastTime) {
				return false
			}
			matched = append(matched, struct {
				event   automationImportWorkflowEvent
				current map[string]any
			}{event: candidate, current: nextState})
		}
		if len(matched) != 1 {
			return false
		}
		step := matched[0]
		if _, duplicate := consumed[step.event.id]; duplicate {
			return false
		}
		consumed[step.event.id] = struct{}{}
		state = step.current
		currentVersion, _ = automationImportJSONInt64(state["version"])
		lastTime, _ = automationImportTime(step.event.createdAt)
	}
	return lastTime.Equal(finalUpdatedTime) && taskArtifactInboxStateMatchesRow(state, finalRow)
}

func taskArtifactInboxImportEventState(state map[string]any, allowReason bool) (map[string]any, bool) {
	if automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) {
		return state, true
	}
	if !allowReason || len(state) != len(projectCompletionInboxEventStateKeys)+1 {
		return nil, false
	}
	reason, reasonOK := state["reason"].(string)
	if !reasonOK || strings.TrimSpace(reason) != reason || reason == "" || utf8.RuneCountInString(reason) > 1_000 {
		return nil, false
	}
	copyState := make(map[string]any, len(projectCompletionInboxEventStateKeys))
	for _, key := range projectCompletionInboxEventStateKeys {
		value, exists := state[key]
		if !exists {
			return nil, false
		}
		copyState[key] = value
	}
	return copyState, true
}

func validTaskArtifactInboxPostDeleteImportEvent(
	event automationImportWorkflowEvent,
	rawPrevious, rawCurrent, previous, current map[string]any,
	lastTime time.Time,
) bool {
	expectedActor := models.BuiltinOwnerActorID
	switch event.action {
	case "read", "updated", "snoozed", "unsnoozed", "resolved", "dismissed", "reopened", "force_resolved":
	case "automatically_resolved", "automatically_reopened":
		expectedActor = models.BuiltinSystemActorID
	default:
		return false
	}
	eventTime, eventTimeOK := automationImportTime(event.createdAt)
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	if !eventTimeOK || eventTime.Before(lastTime) || !previousVersionOK || !currentVersionOK ||
		currentVersion != previousVersion+1 || !validTaskArtifactImportEventEnvelope(event, expectedActor, true) ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil ||
		event.commandSeq == nil || *event.commandSeq != 1 || previous["source_deleted_at"] == nil ||
		!taskArtifactInboxStateEqualExcept(previous, current, taskArtifactInboxPostDeleteChangedKeys(event.action)) {
		return false
	}
	reason, hasReason := rawCurrent["reason"].(string)
	if _, previousHasReason := rawPrevious["reason"]; previousHasReason {
		return false
	}
	switch event.action {
	case "read":
		return !hasReason && previous["read_at"] == nil && current["read_at"] == event.createdAt
	case "updated":
		return !hasReason && taskArtifactInboxNonterminalStatus(previous["status"]) &&
			taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt) &&
			(!taskArtifactImportValueEquals(previous["title"], current["title"]) ||
				!taskArtifactImportValueEquals(previous["summary"], current["summary"]) ||
				!taskArtifactImportValueEquals(previous["priority"], current["priority"]) ||
				!taskArtifactImportValueEquals(previous["due_at"], current["due_at"]))
	case "snoozed":
		snoozedUntil, ok := current["snoozed_until"].(string)
		until, timeOK := automationImportTime(snoozedUntil)
		return !hasReason && ok && timeOK && until.After(eventTime) && taskArtifactInboxNonterminalStatus(previous["status"]) &&
			taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "unsnoozed":
		return !hasReason && taskArtifactInboxNonterminalStatus(previous["status"]) && previous["snoozed_until"] != nil &&
			current["snoozed_until"] == nil && taskArtifactImportValueEquals(current["status"], previous["status"]) &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "resolved", "force_resolved", "automatically_resolved":
		mode := "manual"
		actorID := models.BuiltinOwnerActorID
		resolutionReason := reason
		if event.action == "force_resolved" {
			mode = "forced"
		}
		if event.action == "automatically_resolved" {
			mode = "automatic"
			actorID = models.BuiltinSystemActorID
			resolutionReason = "所有必需任务已完成"
			if hasReason {
				return false
			}
		} else if !hasReason || reason == "" {
			return false
		}
		return taskArtifactInboxNonterminalStatus(previous["status"]) && current["status"] == "resolved" &&
			current["snoozed_until"] == nil && current["resolved_by_actor_id"] == actorID && current["resolved_at"] == event.createdAt &&
			current["resolution_reason"] == resolutionReason && current["resolution_mode"] == mode &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "dismissed":
		return hasReason && reason != "" && taskArtifactInboxNonterminalStatus(previous["status"]) && current["status"] == "dismissed" &&
			current["snoozed_until"] == nil && current["dismissed_by_actor_id"] == models.BuiltinOwnerActorID &&
			current["dismissed_at"] == event.createdAt && current["dismiss_reason"] == reason &&
			taskArtifactInboxTriagedAtTransition(previous, current, event.createdAt)
	case "reopened", "automatically_reopened":
		if hasReason && event.action != "reopened" {
			return false
		}
		return taskArtifactInboxTerminalStatus(previous["status"]) && taskArtifactInboxNonterminalStatus(current["status"]) &&
			current["snoozed_until"] == nil && current["resolved_by_actor_id"] == nil && current["resolved_at"] == nil &&
			current["resolution_reason"] == nil && current["resolution_mode"] == nil && current["dismissed_by_actor_id"] == nil &&
			current["dismissed_at"] == nil && current["dismiss_reason"] == nil
	default:
		return false
	}
}

func taskArtifactInboxPostDeleteChangedKeys(action string) map[string]struct{} {
	keys := map[string]struct{}{"version": {}}
	switch action {
	case "read":
		keys["read_at"] = struct{}{}
	case "updated":
		for _, key := range []string{"title", "summary", "priority", "due_at", "triaged_at"} {
			keys[key] = struct{}{}
		}
	case "snoozed", "unsnoozed":
		keys["snoozed_until"] = struct{}{}
		keys["triaged_at"] = struct{}{}
	case "resolved", "force_resolved", "automatically_resolved":
		for _, key := range []string{"status", "snoozed_until", "triaged_at", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode"} {
			keys[key] = struct{}{}
		}
	case "dismissed":
		for _, key := range []string{"status", "snoozed_until", "triaged_at", "dismissed_by_actor_id", "dismissed_at", "dismiss_reason"} {
			keys[key] = struct{}{}
		}
	case "reopened", "automatically_reopened":
		for _, key := range []string{"status", "snoozed_until", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode", "dismissed_by_actor_id", "dismissed_at", "dismiss_reason"} {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func taskArtifactInboxStateEqualExcept(previous, current map[string]any, changed map[string]struct{}) bool {
	for _, key := range projectCompletionInboxEventStateKeys {
		if _, allowed := changed[key]; allowed {
			continue
		}
		if !taskArtifactImportValueEquals(previous[key], current[key]) {
			return false
		}
	}
	return true
}

func taskArtifactInboxTriagedAtTransition(previous, current map[string]any, createdAt string) bool {
	if previous["triaged_at"] == nil {
		return current["triaged_at"] == createdAt
	}
	return taskArtifactImportValueEquals(current["triaged_at"], previous["triaged_at"])
}

func taskArtifactInboxTerminalStatus(value any) bool {
	return value == "resolved" || value == "dismissed"
}

func taskArtifactInboxNonterminalStatus(value any) bool {
	return value == "open" || value == "tracking"
}

func taskArtifactInboxStateMatchesRow(state, row map[string]any) bool {
	if !automationImportObjectHasExactKeys(state, projectCompletionInboxEventStateKeys) {
		return false
	}
	payloadRaw, ok := row["payload_json"].(string)
	if !ok {
		return false
	}
	payload, ok := automationImportJSONObject(payloadRaw)
	if !ok {
		return false
	}
	for _, key := range projectCompletionInboxEventStateKeys {
		expected := row[key]
		if key == "payload_json" {
			expected = payload
		}
		if !taskArtifactImportValueEquals(state[key], expected) {
			return false
		}
	}
	return true
}

func insertTaskArtifactInboxImportRows(
	tx *gorm.DB,
	table businessExportTable,
	replays taskArtifactImportReplays,
) error {
	if len(replays.ordered) == 0 {
		return insertBusinessImportRows(tx, table)
	}
	idIndex := columnIndex(table.Columns, "id")
	deletedAtIndex := columnIndex(table.Columns, "source_deleted_at")
	versionIndex := columnIndex(table.Columns, "version")
	updatedAtIndex := columnIndex(table.Columns, "updated_at")
	if idIndex < 0 || deletedAtIndex < 0 || versionIndex < 0 || updatedAtIndex < 0 {
		return errors.New("Task Artifact Inbox import columns are incomplete")
	}
	staged := businessExportTable{Name: table.Name, Columns: table.Columns, Rows: make([][]any, 0, len(table.Rows))}
	for _, row := range table.Rows {
		inboxID, _ := row[idIndex].(string)
		replay, planned := replays.byInboxID[inboxID]
		if !planned {
			staged.Rows = append(staged.Rows, row)
			continue
		}
		copyRow := append([]any(nil), row...)
		for index, column := range table.Columns {
			if column == "payload_json" {
				continue
			}
			if value, exists := replay.deletedInboxPrevious[column]; exists {
				copyRow[index] = value
			}
		}
		copyRow[deletedAtIndex] = nil
		copyRow[versionIndex] = replay.previousInboxVersion
		copyRow[updatedAtIndex] = replay.deletedAt
		staged.Rows = append(staged.Rows, copyRow)
	}
	return insertBusinessImportRows(tx, staged)
}

func taskArtifactInboxFinalImportUpdates(
	table businessExportTable,
	row []any,
) (map[string]any, bool) {
	columns := []string{
		"title", "summary", "priority", "resolution_policy", "status", "due_at", "read_at", "triaged_at",
		"snoozed_until", "resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode",
		"dismissed_by_actor_id", "dismissed_at", "dismiss_reason", "version", "updated_at",
	}
	updates := make(map[string]any, len(columns))
	for _, column := range columns {
		index := columnIndex(table.Columns, column)
		if index < 0 || index >= len(row) {
			return nil, false
		}
		updates[column] = row[index]
	}
	return updates, true
}
