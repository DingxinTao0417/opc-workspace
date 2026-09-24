package api

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Human-only preview. Business snapshots must never enter model tool results or
// conversation receipts, including when the capture came from an invoice.
type aiAutomationRetryPreview struct {
	RunID              string          `json:"run_id"`
	RuleID             string          `json:"rule_id"`
	RuleVersion        int64           `json:"rule_version"`
	CurrentRuleVersion int64           `json:"current_rule_version"`
	RuleEnabled        bool            `json:"rule_enabled"`
	TriggerType        string          `json:"trigger_type"`
	ActionType         string          `json:"action_type"`
	Permissions        []string        `json:"permissions"`
	Attempt            int             `json:"attempt"`
	NextAttempt        int             `json:"next_attempt"`
	ScheduledFor       *string         `json:"scheduled_for"`
	ErrorCode          *string         `json:"error_code"`
	Config             json.RawMessage `json:"config"`
	Action             json.RawMessage `json:"action"`
}

// Immutable outcome metadata only. Approval success and action success are
// distinct: the retry engine can record a new failed attempt without an error.
type aiAutomationRetryResult struct {
	ID          string  `json:"id"`
	RuleID      string  `json:"rule_id"`
	RuleVersion int64   `json:"rule_version"`
	Status      string  `json:"status"`
	Attempt     int     `json:"attempt"`
	Retryable   bool    `json:"retryable"`
	RetryAt     *string `json:"retry_at"`
	ErrorCode   *string `json:"error_code"`
	ResultType  *string `json:"result_type"`
	ResultID    *string `json:"result_id"`
}

func previewAIAutomationRetry(tx *gorm.DB, action aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	previous, current, _, err := prepareAutomationRetry(tx, action.AutomationRunID, time.Time{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(404, "AUTOMATION_RUN_NOT_FOUND", "Automation run or rule not found")
	}
	if err != nil {
		return preview, err
	}
	if previous.RuleVersion != action.ExpectedVersion {
		return preview, newProjectRequestError(409, "VERSION_CONFLICT", "Use the captured rule version of the failed run")
	}
	rule, err := automationRuleOutputFromModel(current)
	if err != nil {
		return preview, err
	}
	preview.Label = rule.Name
	preview.AutomationRetry = &aiAutomationRetryPreview{
		RunID: previous.ID, RuleID: previous.RuleID, RuleVersion: previous.RuleVersion,
		CurrentRuleVersion: current.Version, RuleEnabled: current.Enabled,
		TriggerType: previous.TriggerType, ActionType: rule.ActionType, Permissions: rule.Permissions,
		Attempt: previous.Attempt, NextAttempt: previous.Attempt + 1,
		ScheduledFor: previous.ScheduledFor, ErrorCode: safeAIAutomationErrorCode(previous.ErrorCode),
		Config: json.RawMessage(previous.ConfigSnapshotJSON), Action: json.RawMessage(previous.ActionSnapshotJSON),
	}
	return preview, nil
}

func aiActionOutputWithFacts(tx *gorm.DB, row models.AIActionProposal, generationStatus string, now time.Time) (aiActionResponse, error) {
	out, err := aiActionOutput(row, generationStatus, now)
	if err == nil && out.Action.Action == aiAgentDelegateSpawnAction && out.Status == "confirmed" {
		out.AgentDelegationResult, err = readAIAgentDelegationResult(tx, out, now)
		return out, err
	}
	if err == nil && out.Action.Action == aiAgentDelegateFollowupAction && out.Status == "confirmed" {
		out.AgentFollowupResult, err = readAIAgentFollowupResult(tx, out, now)
		if err == nil && out.AgentFollowupResult != nil && out.AgentFollowupResult.Status == "unavailable" {
			out.Route = ""
		}
		return out, err
	}
	if err == nil && out.Action.Action == "inbox.split" && out.Status == "confirmed" {
		out.CreatedTaskIDs, err = readAIInboxSplitCreatedTaskIDs(tx, row, out)
		return out, err
	}
	if err == nil && out.Action.Action == aiAgentRunRecoverOutput && out.Status == "confirmed" {
		out.AgentRunResult, err = readAIAgentRunRecoveryResult(tx, out)
		if err == nil && out.AgentRunResult == nil {
			out.Route = ""
		} else if err == nil && out.AgentRunResult.SubmissionID != nil {
			out.Route = taskSubmissionRoute(out.AgentRunResult.TaskID, *out.AgentRunResult.SubmissionID)
		}
		return out, err
	}
	if err == nil && out.Action.Action == "agent_run.cancel" && out.Status == "confirmed" {
		out.AgentRunResult, err = readAIAgentRunCancelResult(tx, out)
		if err == nil && out.AgentRunResult == nil {
			out.Route = ""
		}
		return out, err
	}
	if err == nil && out.Action.Action == aiAgentRunCancelManyAction && out.Status == "confirmed" {
		out.AgentRunCancelManyResult, err = readAIAgentRunCancelManyResult(tx, out)
		return out, err
	}
	if err == nil && isAIAgentRunAction(out.Action.Action) && out.Status == "confirmed" {
		out.AgentRunResult, err = readAIAgentRunResult(tx, out)
		if err == nil && out.AgentRunResult == nil {
			out.Route = ""
		} else if err == nil && out.AgentRunResult.OutputDeliveryStatus == agentRunOutputSubmitted &&
			out.AgentRunResult.SubmissionID != nil {
			out.Route = taskSubmissionRoute(out.AgentRunResult.TaskID, *out.AgentRunResult.SubmissionID)
		}
		return out, err
	}
	if err == nil && out.Action.Action == "invoice.generate_pdf" && out.Status == "confirmed" {
		out.InvoicePDFResult, err = readAIInvoicePDFResult(tx, out)
		return out, err
	}
	if err != nil || out.Action.Action != "automation.retry" || out.Status != "confirmed" {
		return out, err
	}
	if out.ResultID == nil || out.ResultVersion == nil || out.Preview.AutomationRetry == nil {
		return out, errors.New("invalid automation retry receipt")
	}
	var run models.AutomationRun
	// Never load action/config snapshots, source events or result text here.
	err = tx.Select("id,rule_id,rule_version,status,attempt,retryable,retry_at,error_code,result_type,result_id,retry_of_run_id").First(&run, "id=?", *out.ResultID).Error
	if err != nil {
		return out, err
	}
	p := out.Preview.AutomationRetry
	if run.RetryOfRunID == nil || *run.RetryOfRunID != out.Action.AutomationRunID || run.RuleVersion != *out.ResultVersion || run.RuleVersion != p.RuleVersion || run.RuleID != p.RuleID || run.Attempt != p.NextAttempt || (run.Status != "succeeded" && run.Status != "failed") {
		return out, errors.New("inconsistent automation retry outcome")
	}
	out.AutomationRunResult = &aiAutomationRetryResult{
		ID: run.ID, RuleID: run.RuleID, RuleVersion: run.RuleVersion, Status: run.Status,
		Attempt: run.Attempt, Retryable: run.Retryable, RetryAt: run.RetryAt,
		ErrorCode: safeAIAutomationErrorCode(run.ErrorCode),
	}
	safe := aiAutomationRunRecord(run)
	if kind, ok := safe["result_type"].(string); ok && run.Status == "succeeded" {
		id := safe["result_id"].(string)
		out.AutomationRunResult.ResultType, out.AutomationRunResult.ResultID = &kind, &id
	}
	return out, nil
}
