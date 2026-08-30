package api

import (
	"errors"
	"reflect"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var projectCompletionInboxEventStateKeys = []string{
	"kind", "title", "summary", "source_entity_type", "source_entity_id", "source_event_key", "source_deleted_at",
	"priority", "resolution_policy", "status", "due_at", "read_at", "triaged_at", "snoozed_until",
	"resolved_by_actor_id", "resolved_at", "resolution_reason", "resolution_mode",
	"dismissed_by_actor_id", "dismissed_at", "dismiss_reason", "payload_json", "version",
}

func validProjectCompletionImportSources(packageData businessExportPackage) bool {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	events, ok := automationImportWorkflowEvents(tables["workflow_events"])
	if !ok {
		return false
	}
	projects, ok := automationImportRowsByID(tables["projects"])
	if !ok {
		return false
	}
	for _, row := range automationImportRows(tables["inbox_items"]) {
		if row["source_entity_type"] != projectCompletionInboxSourceType {
			continue
		}
		if !validProjectCompletionImportSource(row, events, projects) {
			return false
		}
	}
	return true
}

func validProjectCompletionImportSource(
	row map[string]any,
	events automationImportEventIndex,
	projects map[string]map[string]any,
) bool {
	id, idOK := row["id"].(string)
	sourceID, sourceOK := row["source_entity_id"].(string)
	sourceKey, keyOK := row["source_event_key"].(string)
	deletedAt, deletedOK := automationImportOptionalString(row["source_deleted_at"])
	payloadJSON, payloadOK := row["payload_json"].(string)
	rowVersion, versionOK := automationImportInt64(row["version"])
	if !idOK || !validCanonicalAutomationUUID(id) || !sourceOK || !validCanonicalAutomationUUID(sourceID) ||
		!keyOK || !deletedOK || !payloadOK || !versionOK || rowVersion < 1 || row["kind"] != "event" ||
		row["priority"] != "P1" || row["due_at"] != nil {
		return false
	}
	if deletedAt != nil {
		if _, valid := automationImportTime(*deletedAt); !valid {
			return false
		}
	}
	payload, ok := automationImportJSONObject(payloadJSON)
	if !ok || len(payload) != 5 || payload["project_id"] != sourceID ||
		payload["project_name"] == nil || payload["completed_at"] == nil {
		return false
	}
	projectName, nameOK := payload["project_name"].(string)
	completedAt, completedOK := payload["completed_at"].(string)
	completionVersion, completionOK := automationImportJSONInt64(payload["completion_version"])
	incompleteTasks, incompleteOK := automationImportJSONInt64(payload["incomplete_task_count"])
	if !nameOK || strings.TrimSpace(projectName) == "" || !completedOK || completionVersion < 2 ||
		!completionOK || !incompleteOK || incompleteTasks < 0 || sourceKey != projectCompletionEventKey(sourceID, completionVersion) {
		return false
	}
	if _, valid := automationImportTime(completedAt); !valid {
		return false
	}
	if !validProjectCompletionImportEvent(events, sourceID, projectName, completionVersion, completedAt) ||
		!validProjectCompletionProjectedEvent(events, id, sourceID, sourceKey, projectName, payload, completedAt) {
		return false
	}
	if deletedAt == nil {
		_, projectExists := projects[sourceID]
		return projectExists && rowVersion >= 1
	}
	if row["status"] != "resolved" && row["status"] != "dismissed" {
		return false
	}
	deletedVersion, ok := validProjectCompletionSourceDeletedEvent(events, id, sourceID, sourceKey, payload, *deletedAt)
	_, projectExists := projects[sourceID]
	return ok && !projectExists && rowVersion >= deletedVersion &&
		validProjectCompletionProjectDeletedEvent(events, sourceID, completionVersion, *deletedAt)
}

func validProjectCompletionImportEvent(
	events automationImportEventIndex,
	projectID, projectName string,
	completionVersion int64,
	completedAt string,
) bool {
	count := 0
	action := automationProjectCompletionAction{ProjectID: projectID, ProjectName: projectName}
	for _, event := range automationImportAggregateActionEvents(events, "project", projectID, "project_completed") {
		if event.currentJSON == nil {
			continue
		}
		current, currentOK := automationImportJSONObject(*event.currentJSON)
		version, versionOK := automationImportJSONInt64(current["version"])
		if !currentOK || !versionOK || version != completionVersion {
			continue
		}
		count++
		if !validAutomationImportEventEnvelope(event) || event.createdAt != completedAt ||
			event.actorID == nil || *event.actorID != models.BuiltinOwnerActorID ||
			event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil ||
			event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
			return false
		}
		previous, previousOK := automationImportJSONObject(*event.previousJSON)
		if !previousOK || !validAutomationImportProjectCompletionTransition(previous, current, action) {
			return false
		}
	}
	return count == 1
}

func validProjectCompletionProjectedEvent(
	events automationImportEventIndex,
	inboxID, projectID, sourceKey, projectName string,
	payload map[string]any,
	completedAt string,
) bool {
	projected := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_projected")
	if len(projected) != 1 {
		return false
	}
	event := projected[0]
	if !validAutomationImportEventEnvelope(event) || event.createdAt != completedAt ||
		event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	if !ok || len(current) != 23 || current["kind"] != "event" ||
		current["title"] != projectCompletionTitle(projectName) ||
		current["summary"] != "项目已标记完成，请确认交付收尾、归档或其他后续工作。" ||
		current["source_entity_type"] != projectCompletionInboxSourceType || current["source_entity_id"] != projectID ||
		current["source_event_key"] != sourceKey || current["source_deleted_at"] != nil || current["priority"] != "P1" ||
		current["resolution_policy"] != "manual" || current["status"] != "open" || current["due_at"] != nil ||
		current["read_at"] != nil || current["triaged_at"] != nil || current["snoozed_until"] != nil ||
		current["resolved_by_actor_id"] != nil || current["resolved_at"] != nil || current["resolution_reason"] != nil ||
		current["resolution_mode"] != nil || current["dismissed_by_actor_id"] != nil || current["dismissed_at"] != nil ||
		current["dismiss_reason"] != nil || !reflect.DeepEqual(current["payload_json"], payload) {
		return false
	}
	version, versionOK := automationImportJSONInt64(current["version"])
	return versionOK && version == 1
}

func validProjectCompletionSourceDeletedEvent(
	events automationImportEventIndex,
	inboxID, projectID, sourceKey string,
	payload map[string]any,
	deletedAt string,
) (int64, bool) {
	deleted := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_deleted")
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
	if !previousOK || !currentOK ||
		!automationImportObjectHasExactKeys(previous, projectCompletionInboxEventStateKeys) ||
		!automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) ||
		previous["source_deleted_at"] != nil || current["source_deleted_at"] != deletedAt ||
		(current["status"] != "resolved" && current["status"] != "dismissed") ||
		!validProjectCompletionDeletedEventIdentity(current, projectID, sourceKey, payload) {
		return 0, false
	}
	for key, value := range previous {
		if key != "source_deleted_at" && key != "version" && !reflect.DeepEqual(value, current[key]) {
			return 0, false
		}
	}
	previousVersion, previousVersionOK := automationImportJSONInt64(previous["version"])
	currentVersion, currentVersionOK := automationImportJSONInt64(current["version"])
	return currentVersion, previousVersionOK && previousVersion >= 1 && currentVersionOK && currentVersion == previousVersion+1
}

func validProjectCompletionDeletedEventIdentity(current map[string]any, projectID, sourceKey string, payload map[string]any) bool {
	return current["kind"] == "event" && current["source_entity_type"] == projectCompletionInboxSourceType &&
		current["source_entity_id"] == projectID && current["source_event_key"] == sourceKey &&
		current["due_at"] == nil && reflect.DeepEqual(current["payload_json"], payload)
}

func automationImportObjectHasExactKeys(object map[string]any, keys []string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := object[key]; !exists {
			return false
		}
	}
	return true
}

func projectCompletionImportTombstoneSourceIDs(table businessExportTable) []string {
	result := make([]string, 0)
	for _, row := range automationImportRows(table) {
		if row["source_entity_type"] != projectCompletionInboxSourceType || row["source_deleted_at"] == nil {
			continue
		}
		if sourceID, ok := row["source_entity_id"].(string); ok {
			result = append(result, sourceID)
		}
	}
	return result
}

func validProjectCompletionProjectDeletedEvent(
	events automationImportEventIndex,
	projectID string,
	completionVersion int64,
	deletedAt string,
) bool {
	deleted := automationImportAggregateActionEvents(events, "project", projectID, "project_deleted")
	if len(deleted) != 1 {
		return false
	}
	event := deleted[0]
	if !validAutomationImportEventEnvelope(event) || event.createdAt != deletedAt ||
		event.actorID == nil || *event.actorID != models.BuiltinOwnerActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON == nil || event.currentJSON != nil ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return false
	}
	previous, ok := automationImportJSONObject(*event.previousJSON)
	name, nameOK := previous["name"].(string)
	_, descriptionOK := previous["description"].(string)
	clientID, clientOK := automationImportOptionalString(previous["client_id"])
	startDate, startOK := automationImportOptionalString(previous["start_date"])
	dueDate, dueOK := automationImportOptionalString(previous["due_date"])
	amount, amountOK := automationImportOptionalInt64(previous["amount_minor"])
	color, colorOK := automationImportOptionalString(previous["color"])
	archivedFrom, archivedOK := automationImportOptionalString(previous["archived_from_status"])
	if !ok || !automationImportObjectHasExactKeys(previous, automationImportProjectEventStateKeys) ||
		previous["id"] != projectID || previous["status"] != "archived" ||
		!nameOK || strings.TrimSpace(name) == "" || !descriptionOK || !clientOK || !startOK || !dueOK ||
		!amountOK || !colorOK || !archivedOK || archivedFrom == nil {
		return false
	}
	if clientID != nil && !validCanonicalAutomationUUID(*clientID) {
		return false
	}
	if (startDate != nil && strings.TrimSpace(*startDate) == "") || (dueDate != nil && strings.TrimSpace(*dueDate) == "") ||
		(amount != nil && *amount < 0) || (color != nil && strings.TrimSpace(*color) == "") {
		return false
	}
	switch *archivedFrom {
	case "planned", "in_progress", "paused", "completed":
	default:
		return false
	}
	version, versionOK := automationImportJSONInt64(previous["version"])
	return versionOK && version >= completionVersion
}

func authorizeHistoricalProjectCompletionImportSources(tx *gorm.DB, table businessExportTable) error {
	for _, row := range automationImportRows(table) {
		if row["source_entity_type"] != projectCompletionInboxSourceType {
			continue
		}
		id, idOK := row["id"].(string)
		sourceID, sourceOK := row["source_entity_id"].(string)
		sourceKey, keyOK := row["source_event_key"].(string)
		payloadJSON, payloadOK := row["payload_json"].(string)
		deletedAt, deletedOK := automationImportOptionalString(row["source_deleted_at"])
		if !idOK || !sourceOK || !keyOK || !payloadOK || !deletedOK {
			return errors.New("Project completion import source authorization is invalid")
		}
		payload, payloadValid := automationImportJSONObject(payloadJSON)
		completionVersion, versionOK := automationImportJSONInt64(payload["completion_version"])
		if !payloadValid || !versionOK {
			return errors.New("Project completion import source payload is invalid")
		}
		if deletedAt == nil {
			var current int64
			if err := tx.Table("projects").Where(
				"id = ? AND status = 'completed' AND version = ?", sourceID, completionVersion,
			).Count(&current).Error; err != nil {
				return err
			}
			if current == 1 {
				continue
			}
		} else {
			var targetProject int64
			if err := tx.Table("projects").Where("id = ?", sourceID).Count(&targetProject).Error; err != nil {
				return err
			}
			if targetProject != 0 {
				return errors.New("Project completion tombstone conflicts with an existing target Project")
			}
		}
		var deletedAtValue any
		if deletedAt != nil {
			deletedAtValue = *deletedAt
		}
		if err := tx.Exec(`
			INSERT INTO business_import_project_completion_authorizations(
				inbox_item_id, source_entity_type, source_entity_id, source_event_key, payload_json, source_deleted_at
			) VALUES (?, 'project_completion', ?, ?, ?, ?)
		`, id, sourceID, sourceKey, payloadJSON, deletedAtValue).Error; err != nil {
			return err
		}
	}
	return nil
}
