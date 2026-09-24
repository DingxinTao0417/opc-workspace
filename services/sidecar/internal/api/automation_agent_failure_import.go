package api

import (
	"reflect"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Agent Runs intentionally do not belong to a portable business package. These
// checks prove only the immutable failure -> notification history, never a live
// execution or permission to retry the Agent.
func automationImportAgentFailureSourceEvidence(run automationImportRun, events automationImportEventIndex) (automationAgentRunFailureEvidence, bool) {
	if run.sourceEventID == nil || run.triggerType != "event" {
		return automationAgentRunFailureEvidence{}, false
	}
	event, ok := events.byID[*run.sourceEventID]
	if !ok || !validAutomationImportEventEnvelope(event) || event.currentJSON == nil || event.previousJSON != nil ||
		event.aggregateType != "agent_run" || event.action != "agent_run_failed" || event.actorID == nil ||
		*event.actorID != models.BuiltinSystemActorID || event.agentRunID == nil || *event.agentRunID != event.aggregateID ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.commandSeq != nil || event.requestID != nil {
		return automationAgentRunFailureEvidence{}, false
	}
	evidence, err := automationAgentRunFailureEventFromJSON(*event.currentJSON)
	created, createdOK := automationImportTime(event.createdAt)
	failed, failedOK := automationImportTime(evidence.FailedAt)
	started, startedOK := automationImportTime(run.startedAt)
	return evidence, err == nil && evidence.AgentRunID == event.aggregateID && createdOK && failedOK && startedOK &&
		created.Equal(failed) && !failed.After(started)
}

func validAutomationImportAgentFailureSource(run automationImportRun, events automationImportEventIndex) bool {
	action, err := automationAgentRunFailureActionFromSnapshot(run.actionSnapshot)
	if run.status == "failed" && run.errorCode != nil {
		switch *run.errorCode {
		case "ACTION_SNAPSHOT_INVALID":
			return !run.actionValid
		case "ATTEMPT_CONTRACT_INVALID":
			return run.actionValid && !run.attemptContractOK
		case "SOURCE_EVENT_INVALID":
			evidence, valid := automationImportAgentFailureSourceEvidence(run, events)
			return err == nil && run.attemptContractOK && (!valid || evidence != action.automationAgentRunFailureEvidence)
		}
	}
	evidence, valid := automationImportAgentFailureSourceEvidence(run, events)
	return err == nil && valid && evidence == action.automationAgentRunFailureEvidence
}

func validAutomationImportAgentFailureResult(run automationImportRun, row map[string]any, events automationImportEventIndex, tasks map[string]map[string]any) bool {
	if run.resultID == nil || run.sourceEventID == nil || run.resultSummary != automationAgentRunFailureResultSummary || row["id"] != *run.resultID || row["kind"] != "event" ||
		row["source_entity_type"] != agentRunFailedInboxSourceType {
		return false
	}
	payloadJSON, ok := row["payload_json"].(string)
	payload, err := automationAgentRunFailurePayloadFromJSON(payloadJSON)
	action, actionErr := automationAgentRunFailureActionFromSnapshot(run.actionSnapshot)
	if !ok || err != nil || actionErr != nil || payload.automationAgentRunFailureEvidence != action.automationAgentRunFailureEvidence ||
		payload.AutomationRunID != run.id || payload.AutomationRuleID != run.rule.id || payload.SourceEventID != *run.sourceEventID ||
		row["source_entity_id"] != payload.AgentRunID || row["source_event_key"] != agentRunFailedEventKey(payload.AgentRunID) ||
		row["created_at"] != run.startedAt {
		return false
	}
	payloadObject, ok := automationImportJSONObject(payloadJSON)
	if !ok || !validAutomationImportAgentFailureProjectedEvent(run, action, payloadObject, events) {
		return false
	}
	deletedAt, deletedOK := automationImportOptionalString(row["source_deleted_at"])
	version, versionOK := automationImportInt64(row["version"])
	if !deletedOK || !versionOK || version < 1 {
		return false
	}
	if deletedAt == nil {
		// Missing portable Agent Run is not proof of deletion. The Task must
		// remain unless a genuine source_deleted transition proves otherwise.
		_, exists := tasks[payload.TaskID]
		return exists && len(automationImportAggregateActionEvents(events, "inbox_item", *run.resultID, "source_deleted")) == 0
	}
	if _, exists := tasks[payload.TaskID]; exists {
		return false
	}
	deletedVersion, valid := validAutomationImportAgentFailureDeletedEvent(row, payloadObject, *deletedAt, events)
	return valid && version >= deletedVersion
}

func validAutomationImportAgentFailureProjectedEvent(run automationImportRun, action automationAgentRunFailureAction, payload map[string]any, events automationImportEventIndex) bool {
	if run.resultID == nil {
		return false
	}
	projected := automationImportAggregateActionEvents(events, "inbox_item", *run.resultID, "source_projected")
	if len(projected) != 1 {
		return false
	}
	event := projected[0]
	if !validAutomationImportEventEnvelope(event) || event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.requestID != nil || event.previousJSON != nil ||
		event.currentJSON == nil || event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil ||
		event.agentRunID != nil || event.createdAt != run.startedAt {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	if !ok || !automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) ||
		current["kind"] != "event" || current["title"] != automationAgentRunFailureItemTitle ||
		current["summary"] != automationAgentRunFailureItemSummary || current["source_entity_type"] != agentRunFailedInboxSourceType ||
		current["source_entity_id"] != action.AgentRunID || current["source_event_key"] != agentRunFailedEventKey(action.AgentRunID) ||
		current["source_deleted_at"] != nil || current["priority"] != action.Priority || current["resolution_policy"] != "manual" ||
		current["status"] != "open" || current["due_at"] != nil || current["read_at"] != nil || current["triaged_at"] != nil ||
		current["snoozed_until"] != nil || current["resolved_by_actor_id"] != nil || current["resolved_at"] != nil ||
		current["resolution_reason"] != nil || current["resolution_mode"] != nil || current["dismissed_by_actor_id"] != nil ||
		current["dismissed_at"] != nil || current["dismiss_reason"] != nil || !reflect.DeepEqual(current["payload_json"], payload) {
		return false
	}
	version, ok := automationImportJSONInt64(current["version"])
	return ok && version == 1
}

func validAutomationImportAgentFailureDeletedEvent(row map[string]any, payload map[string]any, deletedAt string, events automationImportEventIndex) (int64, bool) {
	id, _ := row["id"].(string)
	deleted := automationImportAggregateActionEvents(events, "inbox_item", id, "source_deleted")
	if len(deleted) != 1 {
		return 0, false
	}
	event := deleted[0]
	if !validAutomationImportEventEnvelope(event) || event.createdAt != deletedAt ||
		event.actorID == nil || *event.actorID != models.BuiltinOwnerActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON == nil ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return 0, false
	}
	previous, previousOK := automationImportJSONObject(*event.previousJSON)
	current, currentOK := automationImportJSONObject(*event.currentJSON)
	if !previousOK || !currentOK || !automationImportObjectHasExactKeys(previous, projectCompletionInboxEventStateKeys) ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) || previous["source_deleted_at"] != nil ||
		current["source_deleted_at"] != deletedAt || (current["status"] != "resolved" && current["status"] != "dismissed") ||
		current["kind"] != "event" || current["source_entity_type"] != agentRunFailedInboxSourceType ||
		current["source_entity_id"] != row["source_entity_id"] || current["source_event_key"] != row["source_event_key"] ||
		!reflect.DeepEqual(current["payload_json"], payload) {
		return 0, false
	}
	for key, value := range previous {
		if key != "source_deleted_at" && key != "version" && !reflect.DeepEqual(value, current[key]) {
			return 0, false
		}
	}
	previousVersion, previousOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentOK := automationImportJSONInt64(current["version"])
	return currentVersion, previousOK && previousVersion >= 1 && currentOK && currentVersion == previousVersion+1
}
