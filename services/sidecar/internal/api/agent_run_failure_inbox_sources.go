package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func invalidTaskAgentFailureSource() error {
	return newProjectRequestError(http.StatusConflict, "TASK_AGENT_FAILURE_SOURCE_INVALID", "Agent failure Inbox source history could not be verified")
}

// A portable import has no agent_runs. Its immutable event/run/result graph is
// sufficient to associate a notification with its Task, but never to recreate
// an Agent Run or claim that the notification repaired the failure.
func loadTaskAgentFailureInboxSources(tx *gorm.DB, taskID string) ([]models.InboxItem, error) {
	var items []models.InboxItem
	if err := tx.Where(`source_entity_type = ? AND (
		(CASE WHEN json_valid(payload_json) THEN json_extract(payload_json, '$.task_id') END) = ?
		OR source_entity_id IN (SELECT id FROM agent_runs WHERE task_id = ?))`,
		agentRunFailedInboxSourceType, taskID, taskID).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(items))
	for _, item := range items {
		payload, err := automationAgentRunFailurePayloadFromJSON(item.PayloadJSON)
		if err != nil || payload.TaskID != taskID || item.Kind != "event" || item.SourceEntityID == nil ||
			*item.SourceEntityID != payload.AgentRunID || item.SourceEventKey == nil ||
			*item.SourceEventKey != agentRunFailedEventKey(payload.AgentRunID) {
			return nil, invalidTaskAgentFailureSource()
		}
		var live models.AgentRun
		err = tx.Select("id", "task_id", "attempt", "status", "error_code", "completed_at", "output_delivery_status").
			First(&live, "id = ?", payload.AgentRunID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			evidence, evidenceErr := automationAgentRunFailureEvidenceFromRun(live)
			if evidenceErr != nil || evidence != payload.automationAgentRunFailureEvidence {
				return nil, invalidTaskAgentFailureSource()
			}
		}
		if valid, err := validStoredAgentFailureInboxHistory(tx, item, payload); err != nil {
			return nil, err
		} else if !valid {
			return nil, invalidTaskAgentFailureSource()
		}
		sourceIDs = append(sourceIDs, payload.AgentRunID)
	}
	if len(sourceIDs) != 0 {
		// The generic coordinator selects by source ID. Refuse a shadow row
		// claiming another Task instead of marking it as this Task's deletion.
		var count int64
		if err := tx.Model(&models.InboxItem{}).Where("source_entity_type = ? AND source_entity_id IN ?", agentRunFailedInboxSourceType, sourceIDs).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(items)) {
			return nil, invalidTaskAgentFailureSource()
		}
	}
	return items, nil
}

func coordinateTaskAgentFailureInboxSourceDeletion(tx *gorm.DB, taskID, requestID, deletedAt string) error {
	items, err := loadTaskAgentFailureInboxSources(tx, taskID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, *item.SourceEntityID)
	}
	return coordinateInboxSourceDeletion(tx, agentRunFailedInboxSourceType, ids, "TASK_HAS_ACTIVE_INBOX_SOURCES",
		"Resolve or dismiss all Agent failure Inbox Items before deleting this Task", requestID, deletedAt)
}

func validStoredAgentFailureInboxHistory(tx *gorm.DB, item models.InboxItem, payload automationAgentRunFailurePayload) (bool, error) {
	logicalKey := "event:" + payload.AutomationRuleID + ":" + payload.SourceEventID
	queries := []struct {
		name  string
		query *gorm.DB
	}{
		{"automation_rules", tx.Table("automation_rules")},
		{"automation_runs", tx.Table("automation_runs").Where("logical_key = ?", logicalKey)},
		{"inbox_items", tx.Table("inbox_items").Where("id = ?", item.ID)},
		{"tasks", tx.Table("tasks").Select("id").Where("id = ?", payload.TaskID)},
		{"workflow_events", tx.Table("workflow_events").Where(`id = ? OR (aggregate_type = 'inbox_item' AND aggregate_id = ?)
			OR (aggregate_type = 'automation_rule' AND aggregate_id = ?)
			OR (aggregate_type = 'automation_run' AND aggregate_id IN (SELECT id FROM automation_runs WHERE logical_key = ?))`,
			payload.SourceEventID, item.ID, payload.AutomationRuleID, logicalKey)},
	}
	pack := businessExportPackage{}
	for _, query := range queries {
		var rows []map[string]any
		if err := query.query.Find(&rows).Error; err != nil {
			return false, err
		}
		table := businessExportTable{Name: query.name, Rows: make([][]any, 0, len(rows))}
		if len(rows) > 0 {
			for key := range rows[0] {
				table.Columns = append(table.Columns, key)
			}
			sort.Strings(table.Columns)
		}
		for _, row := range rows {
			values := make([]any, len(table.Columns))
			for index, column := range table.Columns {
				values[index] = row[column]
			}
			table.Rows = append(table.Rows, values)
		}
		pack.Tables = append(pack.Tables, table)
	}
	return validAutomationImportGraph(pack), nil
}
