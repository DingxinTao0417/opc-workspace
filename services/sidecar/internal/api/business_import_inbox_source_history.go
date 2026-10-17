package api

import (
	"errors"
	"reflect"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The Inbox reminders that the system projects for a Task, a Content Item or a
// Roadmap Milestone are guarded on insert: the source has to be in the
// projected state right now, and it must not be deleted. An import writes rows
// in their final state, so a reminder whose source moved on afterwards is
// named in business_import_inbox_source_authorizations (schema 80) once its
// history in the package has been checked, and the guard accepts the named
// row. ADR-034.

// inboxSourceImportTables maps each authorized source type to the table that
// holds its source rows.
var inboxSourceImportTables = map[string]string{
	taskBlockedInboxSourceType:      "tasks",
	taskDueInboxSourceType:          "tasks",
	contentItemInboxSourceType:      "content_items",
	roadmapMilestoneInboxSourceType: "roadmap_milestones",
}

const inboxSourceImportAuthorizationsTable = "business_import_inbox_source_authorizations"

// validInboxSourceImportHistory checks every such reminder in the package:
// the system projected it exactly once with this identity, and its source is
// either still in the package or was deleted through the coordinated path.
// Whether the source is still in the projected state is not asked.
func validInboxSourceImportHistory(packageData businessExportPackage) bool {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	var events automationImportEventIndex
	sources := make(map[string]map[string]map[string]any, 3)
	indexed := false
	for _, row := range automationImportRows(tables["inbox_items"]) {
		sourceType, _ := row["source_entity_type"].(string)
		sourceTable, guarded := inboxSourceImportTables[sourceType]
		if !guarded {
			continue
		}
		if !indexed {
			var ok bool
			if events, ok = automationImportWorkflowEvents(tables["workflow_events"]); !ok {
				return false
			}
			indexed = true
		}
		if _, loaded := sources[sourceTable]; !loaded {
			rows, ok := automationImportRowsByID(tables[sourceTable])
			if !ok {
				return false
			}
			sources[sourceTable] = rows
		}
		if !validInboxSourceImportRow(row, sourceType, events, sources[sourceTable]) {
			return false
		}
	}
	return true
}

func validInboxSourceImportRow(
	row map[string]any,
	sourceType string,
	events automationImportEventIndex,
	sources map[string]map[string]any,
) bool {
	id, idOK := row["id"].(string)
	sourceID, sourceOK := row["source_entity_id"].(string)
	sourceKey, keyOK := row["source_event_key"].(string)
	payloadJSON, payloadOK := row["payload_json"].(string)
	deletedAt, deletedOK := automationImportOptionalString(row["source_deleted_at"])
	rowVersion, versionOK := automationImportInt64(row["version"])
	if !idOK || id == "" || !sourceOK || sourceID == "" || !keyOK || sourceKey == "" ||
		!payloadOK || !deletedOK || !versionOK || rowVersion < 1 || row["kind"] != "event" {
		return false
	}
	payload, ok := automationImportJSONObject(payloadJSON)
	if !ok || !validInboxSourceProjectedImportEvent(events, id, sourceType, sourceID, sourceKey, payload) {
		return false
	}
	_, sourceExists := sources[sourceID]
	if deletedAt == nil {
		// A source cannot be deleted while a reminder still points at it.
		return sourceExists
	}
	if _, valid := automationImportTime(*deletedAt); !valid || sourceExists {
		return false
	}
	deletedVersion, ok := validInboxSourceDeletedImportEvent(events, id, sourceType, sourceID, sourceKey, payload, *deletedAt)
	return ok && rowVersion >= deletedVersion
}

// validInboxSourceProjectedImportEvent finds the one event with which the
// system created the reminder. Titles, summaries and priorities are not
// compared: they are derived from source fields the payload does not carry.
func validInboxSourceProjectedImportEvent(
	events automationImportEventIndex,
	inboxID, sourceType, sourceID, sourceKey string,
	payload map[string]any,
) bool {
	projected := automationImportAggregateActionEvents(events, "inbox_item", inboxID, "source_projected")
	if len(projected) != 1 {
		return false
	}
	event := projected[0]
	if !validAutomationImportEventEnvelope(event) ||
		event.actorID == nil || *event.actorID != models.BuiltinSystemActorID ||
		event.commandSeq == nil || *event.commandSeq != 1 || event.previousJSON != nil || event.currentJSON == nil ||
		event.assignmentID != nil || event.submissionID != nil || event.artifactID != nil || event.agentRunID != nil {
		return false
	}
	current, ok := automationImportJSONObject(*event.currentJSON)
	if !ok || !automationImportObjectHasExactKeys(current, projectCompletionInboxEventStateKeys) ||
		!inboxSourceImportEventIdentity(current, sourceType, sourceID, sourceKey, payload) ||
		current["source_deleted_at"] != nil || current["status"] != "open" || current["resolution_policy"] != "manual" {
		return false
	}
	version, versionOK := automationImportJSONInt64(current["version"])
	return versionOK && version == 1
}

// validInboxSourceDeletedImportEvent finds the one event that marked the
// source as deleted and returns the reminder's version after it. Deleting a
// source requires its reminders to be resolved or dismissed first; they may be
// reopened afterwards, so the row's final status is not constrained here.
func validInboxSourceDeletedImportEvent(
	events automationImportEventIndex,
	inboxID, sourceType, sourceID, sourceKey string,
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
		!inboxSourceImportEventIdentity(current, sourceType, sourceID, sourceKey, payload) {
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

func inboxSourceImportEventIdentity(state map[string]any, sourceType, sourceID, sourceKey string, payload map[string]any) bool {
	return state["kind"] == "event" && state["source_entity_type"] == sourceType &&
		state["source_entity_id"] == sourceID && state["source_event_key"] == sourceKey &&
		reflect.DeepEqual(state["payload_json"], payload)
}

// authorizeInboxSourceImportRows names every guarded reminder that is about to
// be inserted. The rows reach this point only after their history was checked
// against the whole package; the insert guards then accept a named row whose
// source exists, or whose source is recorded as deleted and absent. Each insert
// consumes its authorization, and the apply fails when one is left over.
func authorizeInboxSourceImportRows(tx *gorm.DB, table businessExportTable) error {
	for _, row := range automationImportRows(table) {
		sourceType, _ := row["source_entity_type"].(string)
		if _, guarded := inboxSourceImportTables[sourceType]; !guarded {
			continue
		}
		id, idOK := row["id"].(string)
		sourceID, sourceOK := row["source_entity_id"].(string)
		sourceKey, keyOK := row["source_event_key"].(string)
		payloadJSON, payloadOK := row["payload_json"].(string)
		deletedAt, deletedOK := automationImportOptionalString(row["source_deleted_at"])
		if !idOK || !sourceOK || !keyOK || !payloadOK || !deletedOK {
			return errors.New("Inbox source import authorization is invalid")
		}
		if err := tx.Exec(`
			INSERT INTO `+inboxSourceImportAuthorizationsTable+`(
				inbox_item_id, source_entity_type, source_entity_id, source_event_key, payload_json, source_deleted_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`, id, sourceType, sourceID, sourceKey, payloadJSON, automationImportOptionalStringValue(deletedAt)).Error; err != nil {
			return err
		}
	}
	return nil
}

// pendingInboxSourceImportAuthorizations counts the authorizations that exist.
// There must be none before an import writes its own and none after its last
// Inbox row was inserted.
func pendingInboxSourceImportAuthorizations(tx *gorm.DB) (int64, error) {
	var count int64
	err := tx.Table(inboxSourceImportAuthorizationsTable).Count(&count).Error
	return count, err
}
