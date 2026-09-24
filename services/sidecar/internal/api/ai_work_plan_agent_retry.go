package api

import (
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiWorkPlanActionFacts struct {
	Proposal models.AIActionProposal
	Receipt  aiActionResponse
}

func strictAIWorkPlanAction(proposal models.AIActionProposal) (aiWorkspaceAction, error) {
	if sha256Hex([]byte(proposal.ActionJSON)) != proposal.Fingerprint {
		return aiWorkspaceAction{}, errors.New("plan replacement action fingerprint changed")
	}
	action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
	if err != nil {
		return aiWorkspaceAction{}, errors.New("plan replacement action is invalid")
	}
	return action, nil
}

// Frozen execution facts are compared only inside this projection. No source
// text, input snapshot, file identity or provider configuration is returned.
type aiPlanRetryRun struct {
	models.AgentRun
	HasOutput bool `gorm:"column:has_output"`
}

func readAIPlanRetryRun(tx *gorm.DB, id string) (aiPlanRetryRun, error) {
	var run aiPlanRetryRun
	err := tx.Model(&models.AgentRun{}).Select(
		"id,task_id,assignment_id,actor_id,adapter_id,parent_run_id,attempt,status,provider_id,model,"+
			"task_version,assignment_assigned_at,actor_version,adapter_version,provider_version,provider_config_version,"+
			"execution_contract_version,input_snapshot_json,output_delivery_status,"+
			"(result_text IS NOT NULL OR result_bytes IS NOT NULL OR submission_id IS NOT NULL OR artifact_id IS NOT NULL OR "+
			"output_delivery_pending_text IS NOT NULL OR output_delivery_pending_bytes IS NOT NULL OR output_delivery_pending_completed_at IS NOT NULL) AS has_output",
	).Take(&run, "id=?", id).Error
	return run, err
}

func aiPlanRetryableRun(run aiPlanRetryRun) bool {
	return (run.Status == "failed" || run.Status == "cancelled" || run.Status == "interrupted") &&
		run.OutputDeliveryStatus == agentRunOutputNotReady && !run.HasOutput
}

func aiPlanRetryIdentity(run models.AgentRun) agentRunIdentity {
	return agentRunIdentity{
		TaskVersion: run.TaskVersion, AssignmentID: run.AssignmentID, AssignmentAssignedAt: run.AssignmentAssignedAt,
		ActorID: run.ActorID, ActorVersion: run.ActorVersion, AdapterID: run.AdapterID, AdapterVersion: run.AdapterVersion,
		ProviderID: run.ProviderID, ProviderVersion: run.ProviderVersion, ProviderConfigVersion: run.ProviderConfigVersion,
	}
}

func aiPlanRunMatchesPreview(run models.AgentRun, preview *aiAgentRunStartPreview) bool {
	if isAgentRunModelContract(run.ExecutionContractVersion) {
		snapshot, err := parseAgentRunModelSnapshot(run.ExecutionContractVersion, run.InputSnapshotJSON)
		if err != nil || preview == nil || preview.Provider.Kind != snapshot.ProviderKind ||
			preview.Provider.Protocol != snapshot.ModelProtocol || preview.RuntimeLimits.MaxOutputTokens != snapshot.MaxOutputTokens {
			return false
		}
	}
	return preview != nil && run.ID != "" && run.TaskVersion > 0 && run.ActorVersion > 0 &&
		run.AdapterVersion > 0 && run.ProviderVersion > 0 && run.ProviderConfigVersion > 0 &&
		run.AssignmentID != "" && run.AssignmentAssignedAt != "" && run.InputSnapshotJSON != "" &&
		run.TaskID == preview.Task.ID && run.TaskVersion == preview.Task.Version &&
		run.AssignmentID == preview.Assignment.ID && run.AssignmentAssignedAt == preview.Assignment.AssignedAt &&
		run.ActorID == preview.Assignment.ActorID && run.ActorID == preview.Agent.ID && run.ActorVersion == preview.Agent.Version &&
		run.AdapterID == preview.Adapter.ID && run.AdapterVersion == preview.Adapter.Version &&
		run.ProviderID == preview.Provider.ID && run.ProviderVersion == preview.Provider.Version &&
		run.ProviderConfigVersion == preview.Provider.ConfigVersion && run.Model == preview.Provider.Model &&
		run.Attempt == preview.Attempt && run.ExecutionContractVersion == preview.ExecutionContractVersion &&
		(run.ExecutionContractVersion == agentRunExecutionContractVersion ||
			run.ExecutionContractVersion == agentRunExecutionContractVersionV2 ||
			run.ExecutionContractVersion == agentRunExecutionContractVersionV3 ||
			run.ExecutionContractVersion == agentRunExecutionContractVersionV4 ||
			isAgentRunModelContract(run.ExecutionContractVersion))
}

// One ordered pass over at most twelve steps propagates a required source Run
// through rejected/expired proposals. A rejected restart/retry cannot be replaced by an
// unrelated successful action to make an already-executed failure disappear.
// Once a real child attempt fails, the next retry must target that child.
func projectAIWorkPlanReplacements(tx *gorm.DB, plan *aiWorkPlanView, facts map[string]aiWorkPlanActionFacts) error {
	priorIndexes := map[string]int{}
	requiredRetryRun := map[string]aiPlanRetryRun{}
	for index := range plan.Steps {
		step := &plan.Steps[index]
		if step.Replaces == "" {
			priorIndexes[step.ID] = index
			continue
		}
		priorIndex, ok := priorIndexes[step.Replaces]
		if !ok {
			return errors.New("plan replacement source must precede its successor")
		}
		prior := &plan.Steps[priorIndex]
		source, ok := facts[prior.ID]
		if !ok || prior.Evidence == nil {
			return errors.New("plan replacement source evidence is unavailable")
		}
		sourceAction, err := strictAIWorkPlanAction(source.Proposal)
		if err != nil {
			return err
		}
		anchor, constrained := requiredRetryRun[prior.ID]
		switch source.Receipt.Status {
		case "rejected", "expired":
			// A plain unexecuted intent keeps the existing free-replacement rule.
			// A linked restart already identifies a real terminal source, even if
			// it was the first bound step rather than a replacement in this plan.
			restartID, err := aiActionRestartOfRunID(sourceAction)
			if err != nil {
				return err
			}
			if restartID != "" {
				if constrained && anchor.ID != restartID {
					return errors.New("rejected restart changed its inherited source Run")
				}
				anchor, err = readAIPlanRetryRun(tx, restartID)
				if err != nil {
					return err
				}
				if anchor.TaskID != sourceAction.TaskID {
					return errors.New("rejected restart no longer refers to its source Task")
				}
				if err := validateAIPlanRestartTarget(tx, anchor, source); err != nil {
					return err
				}
				constrained = true
			}
		case "confirmed":
			if (sourceAction.Action != "agent_run.start" && sourceAction.Action != "agent_run.retry") ||
				source.Receipt.ResultID == nil || source.Receipt.AgentRunResult == nil {
				return errors.New("only a failed Agent start or retry can be replaced after confirmation")
			}
			anchor, err = readAIPlanRetryRun(tx, *source.Receipt.ResultID)
			if err != nil {
				return err
			}
			if anchor.TaskID != sourceAction.TaskID ||
				!aiPlanRunMatchesPreview(anchor.AgentRun, source.Receipt.Preview.AgentRunStart) {
				return errors.New("plan replacement requires exact source execution evidence")
			}
			if _, err := loadAgentRunRestartSource(tx, anchor.TaskID, anchor.ID); err != nil {
				return err
			}
			constrained = true
		default:
			return errors.New("replacement source no longer proves a rejected, expired or retryable action")
		}
		target, targetFound := facts[step.ID]
		var targetAction aiWorkspaceAction
		if step.ProposalID != "" {
			if !targetFound || step.Evidence == nil {
				return errors.New("plan replacement target evidence is unavailable")
			}
			targetAction, err = strictAIWorkPlanAction(target.Proposal)
			if err != nil {
				return err
			}
		}
		if constrained {
			if !targetFound || targetAction.TaskID != anchor.TaskID {
				return errors.New("execution replacement must bind the exact same source Run and Task")
			}
			restartID, err := aiActionRestartOfRunID(targetAction)
			if err != nil {
				return err
			}
			switch {
			case targetAction.Action == "agent_run.retry" && targetAction.AgentRunID == anchor.ID &&
				step.Evidence.RetryOfRunID == anchor.ID && step.Evidence.RestartOfRunID == "" && aiPlanRetryableRun(anchor):
				if err := validateAIPlanRetryTarget(tx, anchor, target); err != nil {
					return err
				}
			case targetAction.Action == "agent_run.start" && restartID == anchor.ID &&
				step.Evidence.RestartOfRunID == anchor.ID && step.Evidence.RetryOfRunID == "":
				if err := validateAIPlanRestartTarget(tx, anchor, target); err != nil {
					return err
				}
			default:
				return errors.New("execution replacement must be an exact retry or an explicitly linked current-facts restart")
			}
			requiredRetryRun[step.ID] = anchor
		}
		prior.SupersededBy = step.ID
		priorIndexes[step.ID] = index
	}
	return nil
}

func validateAIPlanRetryTarget(tx *gorm.DB, parent aiPlanRetryRun, target aiWorkPlanActionFacts) error {
	preview := target.Receipt.Preview.AgentRunRetry
	if preview == nil || preview.RunID != parent.ID || preview.Attempt != parent.Attempt || preview.Status != parent.Status {
		return errors.New("plan retry preview no longer matches its failed source")
	}
	startPreview := target.Receipt.Preview.AgentRunStart
	if startPreview == nil || startPreview.Attempt <= parent.Attempt {
		return errors.New("plan retry preview has no new frozen attempt")
	}
	expected := parent.AgentRun
	expected.Attempt = startPreview.Attempt
	if !aiPlanRunMatchesPreview(expected, startPreview) {
		return errors.New("plan retry preview changed its source execution identity")
	}
	switch target.Receipt.Status {
	case "pending", "rejected", "expired", "unavailable":
		if target.Receipt.ResultID != nil || target.Receipt.ResultVersion != nil {
			return errors.New("unexecuted plan retry has an inconsistent result")
		}
		return nil // Remains unsatisfied; no approval, Run or grant is created.
	case "confirmed":
		if target.Receipt.ResultID == nil || target.Receipt.AgentRunResult == nil {
			return errors.New("confirmed plan retry has no execution evidence")
		}
		child, err := readAIPlanRetryRun(tx, *target.Receipt.ResultID)
		if err != nil {
			return err
		}
		// Attempts are allocated across Task+Actor, not simply parent+1.
		if child.ParentRunID == nil || *child.ParentRunID != parent.ID || child.Attempt <= parent.Attempt ||
			child.TaskID != parent.TaskID || child.Model != parent.Model ||
			aiPlanRetryIdentity(child.AgentRun) != aiPlanRetryIdentity(parent.AgentRun) ||
			child.ExecutionContractVersion != parent.ExecutionContractVersion || child.InputSnapshotJSON != parent.InputSnapshotJSON ||
			!aiPlanRunMatchesPreview(child.AgentRun, target.Receipt.Preview.AgentRunStart) ||
			aiPlanContinuationActionBlocker(target.Receipt) == "evidence_unavailable" {
			return errors.New("plan retry lineage or frozen execution contract changed")
		}
		return nil
	default:
		return errors.New("plan retry proposal state is unavailable")
	}
}
