package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func addAIAutomationSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "automation.enable", "automation.disable", "automation.update", "automation.retry")
	properties["automation_run_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Retry only: actual failed Run ID from workspace_automations; expected_version is its captured rule_version, NOT the current rule version. Empty changes; no replacement snapshot. Human separately confirms the complete capture. A confirmed retry may fail again: check automation_run_result.status, never claim success from approval alone."}
	properties["automation_rule_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Existing code-owned rule from workspace_automations; exact expected_version required."}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Automation enable/disable/retry require empty changes. Update event rule requires only priority; schedule rule requires both local_time (HH:mm) and timezone (explicit IANA), no defaults. Only code-owned presets, no new rules/scripts/actions/permissions. Retry preserves the original immutable config/action, at most three attempts, no duplicate successor; it requires separate HUMAN retry consent and cannot bypass a disabled schedule rule. Enable and updates to an enabled rule require separate HUMAN consent to continuing local effects; never supply consent flags. Saving does not run immediately. Scheduling uses confirmation time; disabling stops new triggers but retains captured event deliveries/retries, history and existing results."
	fields := changes["properties"].(map[string]any)
	fields["local_time"] = map[string]any{"type": "string", "pattern": "^([01][0-9]|2[0-3]):[0-5][0-9]$", "description": "Schedule automation update only; explicit actual 24-hour local clock with timezone"}
}

func parseAIAutomationAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{"task_id", "project_id", "inbox_item_id", "reminder_id", "focus_session_id", "client_followup_id", "client_activity_id"} {
		if _, ok := raw[key]; ok {
			return input, errors.New("automation accepts only automation_rule_id")
		}
	}
	if input.Action == "automation.retry" {
		_, mixed := raw["automation_rule_id"]
		id, err := uuid.Parse(input.AutomationRunID)
		if mixed || err != nil || id.String() != input.AutomationRunID || input.ExpectedVersion < 1 || len(fields) != 0 {
			return input, errors.New("retry requires only actual automation_run_id, captured rule_version as expected_version and empty changes")
		}
		input.Changes = json.RawMessage(`{}`)
		return input, nil
	}
	if _, mixed := raw["automation_run_id"]; mixed {
		return input, errors.New("only retry accepts automation_run_id")
	}
	id, err := uuid.Parse(input.AutomationRuleID)
	if err != nil || id.String() != input.AutomationRuleID || input.ExpectedVersion < 1 {
		return input, errors.New("read actual automation rule ID/version first")
	}
	preset, ok := automationPresetByID(input.AutomationRuleID)
	if !ok {
		return input, errors.New("only existing code-owned automation presets are supported")
	}
	switch input.Action {
	case "automation.enable", "automation.disable":
		if len(fields) != 0 {
			return input, errors.New("enable/disable changes must be empty")
		}
		input.Changes = json.RawMessage(`{}`)
	case "automation.update":
		config := automationConfig{}
		required := []string{"priority"}
		if preset.TriggerType == "schedule" {
			required = []string{"local_time", "timezone"}
		}
		if len(fields) != len(required) {
			return input, errors.New("supply complete limited automation config")
		}
		values := map[string]string{}
		for _, key := range required {
			var value string
			if json.Unmarshal(fields[key], &value) != nil || strings.TrimSpace(value) == "" {
				return input, errors.New("automation config requires explicit " + key)
			}
			values[key] = strings.TrimSpace(value)
		}
		config.Priority, config.LocalTime, config.Timezone = values["priority"], values["local_time"], values["timezone"]
		config, err = normalizeAutomationConfig(preset.PresetKey, config)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(config)
	default:
		return input, errors.New("unsupported automation action")
	}
	return input, nil
}

func previewAIAutomationAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	if input.Action == "automation.retry" {
		return previewAIAutomationRetry(tx, input)
	}
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var current models.AutomationRule
	if err := tx.First(&current, "id=?", input.AutomationRuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(404, "AUTOMATION_RULE_NOT_FOUND", "Automation rule not found")
		}
		return preview, err
	}
	if current.Version != input.ExpectedVersion {
		return preview, newProjectRequestError(409, "VERSION_CONFLICT", "Automation rule changed")
	}
	out, err := automationRuleOutputFromModel(current)
	if err != nil {
		return preview, err
	}
	if input.Action == "automation.enable" && !out.Available {
		return preview, newProjectRequestError(409, "AUTOMATION_DEPENDENCY_UNAVAILABLE", out.UnavailableReason)
	}
	for _, f := range []map[string]any{preview.Before, preview.After} {
		f["preset_key"], f["name"], f["available"], f["unavailable_reason"] = out.PresetKey, out.Name, out.Available, out.UnavailableReason
		f["trigger_type"], f["trigger_label"], f["action_type"], f["action_label"] = out.TriggerType, out.TriggerLabel, out.ActionType, out.ActionLabel
		f["permission_summary"], f["rule_enabled"] = strings.Join(out.Permissions, "\n"), current.Enabled
		if out.TriggerType == "schedule" {
			f["local_time"], f["timezone"] = out.Config.LocalTime, out.Config.Timezone
		} else {
			f["priority"] = out.Config.Priority
		}
	}
	switch input.Action {
	case "automation.enable":
		preview.After["rule_enabled"] = true
	case "automation.disable":
		preview.After["rule_enabled"] = false
	case "automation.update":
		changes := map[string]any{}
		_ = json.Unmarshal(input.Changes, &changes)
		for key, value := range changes {
			preview.After[key] = value
		}
	}
	// Scheduler-owned next_run_at can move without a rule version change. Approval
	// authorizes the displayed schedule, not a frozen future execution instant.
	preview.Label = out.Name
	return preview, nil
}

func aiAutomationNeedsConsent(input aiWorkspaceAction, preview aiActionPreview) bool {
	return input.Action == "automation.enable" || (input.Action == "automation.update" && preview.After["rule_enabled"] == true)
}

func executeAIAutomationAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, error) {
	clock, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return aiActionResult{}, err
	}
	if input.Action == "automation.retry" {
		run, err := retryAutomationRunInTransaction(tx, input.AutomationRunID, clock)
		// Run records are immutable; this is the captured RULE version, not a Run version.
		return aiActionResult{ID: run.ID, Version: run.RuleVersion}, err
	}
	var config *automationConfig
	var enabled *bool
	if input.Action == "automation.update" {
		config = &automationConfig{}
		if err := json.Unmarshal(input.Changes, config); err != nil {
			return aiActionResult{}, err
		}
	} else {
		value := input.Action == "automation.enable"
		enabled = &value
	}
	out, err := changeAutomationRuleInTransaction(tx, input.AutomationRuleID, input.ExpectedVersion, config, enabled, clock)
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
