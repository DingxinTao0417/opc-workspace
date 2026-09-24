package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiAutomationsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view"],"properties":{"view":{"type":"string","enum":["rules","rule","runs","run"]},"id":{"type":"string","format":"uuid","description":"Required only for rule/run detail"},"rule_id":{"type":"string","format":"uuid","description":"Only runs filter"},"status":{"type":"string","enum":["succeeded","failed","skipped","cancelled"],"description":"Only runs filter"},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

func aiAutomationRoute(kind, id string) string {
	return "/settings/automation?" + kind + "=" + id
}

type aiAutomationRuleRecord struct {
	automationRuleOutput
	Route string `json:"route"`
}

func (t *aiWorkspaceTool) automations(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		View   string `json:"view"`
		ID     string `json:"id"`
		RuleID string `json:"rule_id"`
		Status string `json:"status"`
		Limit  *int   `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		allowed := key == "view" || ((input.View == "rule" || input.View == "run") && key == "id") || (input.View == "runs" && key != "id")
		if !allowed || string(raw) == "null" {
			return nil, errors.New("unexpected or null automation argument")
		}
	}
	for _, key := range []string{"id", "rule_id"} {
		if raw, ok := fields[key]; ok {
			var value string
			_ = json.Unmarshal(raw, &value)
			id, err := uuid.Parse(value)
			if err != nil || id.String() != value {
				return nil, errors.New("automation IDs must be canonical UUIDs")
			}
		}
	}
	if (input.View == "rule" || input.View == "run") && input.ID == "" {
		return nil, errors.New("detail requires actual id")
	}
	if raw, ok := fields["status"]; ok && string(raw) != `"succeeded"` && string(raw) != `"failed"` && string(raw) != `"skipped"` && string(raw) != `"cancelled"` {
		return nil, errors.New("invalid automation run status")
	}
	db := t.api.db.WithContext(ctx)
	result := map[string]any{"view": input.View, "ui_entry": "automation_settings", "server_now": t.api.options.Now().UTC().Format(time.RFC3339Nano), "business_snapshots_included": false}
	switch input.View {
	case "rules", "rule":
		query := db.Model(&models.AutomationRule{})
		if input.View == "rule" {
			query = query.Where("id=?", input.ID)
		}
		rows := []models.AutomationRule{}
		if err := query.Order("id ASC").Limit(len(automationPresets) + 1).Find(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		if input.View == "rule" && len(rows) == 0 {
			return nil, errors.New("automation rule not found")
		}
		if len(rows) > len(automationPresets) {
			return nil, errors.New("automation catalog inconsistent")
		}
		items := make([]aiAutomationRuleRecord, 0, len(rows))
		for _, row := range rows {
			out, err := automationRuleOutputFromModel(row)
			if err != nil {
				return nil, safeAIWorkspaceError(ctx, err)
			}
			items = append(items, aiAutomationRuleRecord{automationRuleOutput: out, Route: aiAutomationRoute("rule", row.ID)})
		}
		if input.View == "rule" {
			result["rule"] = items[0]
		} else {
			result["items"] = items
		}
	case "run", "runs":
		// Select only the public metadata boundary; do not load sensitive snapshots.
		query := db.Model(&models.AutomationRun{}).Select("id,rule_id,rule_version,trigger_type,scheduled_for,status,attempt,retryable,retry_at,error_code,result_type,result_id,started_at,ended_at")
		if input.View == "run" {
			var row models.AutomationRun
			if err := query.Where("id=?", input.ID).Take(&row).Error; err != nil {
				return nil, safeAIWorkspaceError(ctx, err)
			}
			result["run"] = aiAutomationRunRecord(row)
			break
		}
		limit, err := workspacePaging(input.Limit, input.Offset)
		if err != nil {
			return nil, err
		}
		if input.RuleID != "" {
			query = query.Where("rule_id=?", input.RuleID)
		}
		if input.Status != "" {
			query = query.Where("status=?", input.Status)
		}
		rows := []models.AutomationRun{}
		// Native automation timestamps use fixed-width nanosecond UTC strings.
		if err := query.Order("started_at DESC, id ASC").Limit(limit + 1).Offset(input.Offset).Find(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			items = append(items, aiAutomationRunRecord(row))
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		result["items"], result["has_more"], result["next_offset"], result["window_limited"] = items, more, next, more && next == nil
	default:
		return nil, errors.New("view must be rules, rule, runs or run")
	}
	return result, nil
}

func safeAIAutomationErrorCode(value *string) *string {
	var errorCode *string
	if value != nil {
		code := *value
		switch code {
		case "SCHEDULE_WINDOW_EXPIRED", "ACTION_WRITE_FAILED", "SOURCE_EVENT_CONFLICT", "ACTION_SNAPSHOT_INVALID", "ATTEMPT_CONTRACT_INVALID", "SOURCE_EVENT_INVALID", "SOURCE_UNAVAILABLE":
		default:
			code = "UNKNOWN_ERROR"
		}
		errorCode = &code
	}
	return errorCode
}

func aiAutomationRunRecord(row models.AutomationRun) map[string]any {
	errorCode := safeAIAutomationErrorCode(row.ErrorCode)
	result := map[string]any{"id": row.ID, "rule_id": row.RuleID, "rule_version": row.RuleVersion, "trigger_type": row.TriggerType, "scheduled_for": row.ScheduledFor, "status": row.Status, "attempt": row.Attempt, "retryable": row.Retryable, "retry_at": row.RetryAt, "error_code": errorCode, "started_at": row.StartedAt, "ended_at": row.EndedAt}
	result["route"] = aiAutomationRoute("run", row.ID)
	if row.ResultType != nil && row.ResultID != nil {
		id, err := uuid.Parse(*row.ResultID)
		kind := *row.ResultType
		if err == nil && id.String() == *row.ResultID && (kind == "inbox_item" || kind == "task" || kind == "reminder") {
			result["result_type"], result["result_id"] = kind, id.String()
		}
	}
	return result
}
