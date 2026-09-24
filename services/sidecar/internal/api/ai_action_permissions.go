package api

import (
	"encoding/json"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The original proposal and an explicitly selected historical command share
// the same domain boundary. Neither a receipt nor a past grant is permission.
func requireAIWorkspaceActionScopes(policy harness.Capabilities, input aiWorkspaceAction) error {
	required := []string{"work", "actions"}
	if isAITaskOutputAction(input.Action) {
		required = append(required, "outputs")
	}
	if isAIAgentRunAction(input.Action) || isAIAgentRunCancelAction(input.Action) || input.Action == aiAgentRunRecoverOutput {
		required = []string{"work", "outputs", "actions", "agent_execution"}
	}
	if input.Action == aiAgentDelegateSpawnAction || input.Action == aiAgentDelegateFollowupAction {
		required = []string{"work", "outputs", "actions", "agent_execution"}
	}
	if input.Action == "finance.export_csv" {
		required = []string{"finance", "finance_exports"}
	}
	if isAIKnowledgeAction(input.Action) {
		required = []string{"knowledge_actions"}
	}
	if strings.HasPrefix(input.Action, "invoice.") {
		required = []string{"finance", "invoice_actions"}
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		required = []string{"finance", "finance_actions"}
	}
	if strings.HasPrefix(input.Action, "client_followup.") || strings.HasPrefix(input.Action, "client_activity.") ||
		strings.HasPrefix(input.Action, "client.") || strings.HasPrefix(input.Action, "client_contact.") {
		required = append(required, "clients")
	}
	if strings.HasPrefix(input.Action, "project.") {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &fields)
		if _, present := fields["client_id"]; present {
			required = append(required, "clients")
		}
	}
	for _, scope := range required {
		if err := policy.Require(scope); err != nil {
			return err
		}
	}
	return nil
}

// Retry commands carry no file fields; their captured execution contract
// decides whether the original proposal requires the extra file permission.
func requireAIAgentRetryFileScope(db *gorm.DB, policy harness.Capabilities, input aiWorkspaceAction) error {
	if input.Action != "agent_run.retry" {
		return nil
	}
	var source models.AgentRun
	if err := db.Select("execution_contract_version", "input_snapshot_json").First(&source, "id=? AND task_id=?", input.AgentRunID, input.TaskID).Error; err != nil {
		return err
	}
	if source.ExecutionContractVersion == agentRunExecutionContractVersionV2 || source.ExecutionContractVersion == agentRunExecutionContractVersionV3 {
		return policy.Require("agent_files")
	}
	if source.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
		snapshot, err := parseAgentRunV4Snapshot(source.InputSnapshotJSON)
		if err != nil {
			return agentRunIdentityChangedError()
		}
		if len(snapshot.Files) > 0 || snapshot.OutputContract.Type == agentexec.ResultTypeFile || snapshot.OutputContract.Type == agentexec.ResultTypeFiles {
			return policy.Require("agent_files")
		}
	}
	if isAgentRunModelContract(source.ExecutionContractVersion) {
		snapshot, err := parseAgentRunModelSnapshot(source.ExecutionContractVersion, source.InputSnapshotJSON)
		if err != nil {
			return agentRunIdentityChangedError()
		}
		if len(snapshot.Files) > 0 || snapshot.OutputContract.Type != agentexec.ResultTypeText {
			if agentRunHasProjectTaskFiles(snapshot.Files) {
				if err := policy.Require("agent_project_files"); err != nil {
					return err
				}
			}
			return policy.Require("agent_files")
		}
	}
	return nil
}
