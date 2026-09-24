package api

import (
	"bytes"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

func sameAgentRunRestartSource(left, right *agentRunRestartSource) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func aiActionRestartOfRunID(action aiWorkspaceAction) (string, error) {
	if action.Action != "agent_run.start" {
		return "", nil
	}
	var changes aiAgentRunStartChanges
	if err := json.Unmarshal(action.Changes, &changes); err != nil {
		return "", err
	}
	return changes.RestartOfRunID, nil
}

// Restart uses today's frozen facts, never the old execution input. Its
// independent immutable event proves the relationship, not the retry parent
// column or an assumption that attempts increase across different Actors.
func validateAIPlanRestartTarget(tx *gorm.DB, source aiPlanRetryRun, target aiWorkPlanActionFacts) error {
	preview := target.Receipt.Preview.AgentRunStart
	proof, err := loadAgentRunRestartSource(tx, source.TaskID, source.ID)
	if err != nil {
		return err
	}
	if preview == nil || preview.Restart == nil || !sameAgentRunRestartSource(proof, preview.Restart) ||
		preview.Task.ID != source.TaskID || target.Receipt.Preview.AgentRunRetry != nil {
		return errors.New("plan restart must freeze the exact source Run and current same-Task execution facts")
	}
	switch target.Receipt.Status {
	case "pending", "rejected", "expired", "unavailable":
		if target.Receipt.ResultID != nil || target.Receipt.ResultVersion != nil {
			return errors.New("unexecuted restart cannot claim a new Run")
		}
		return nil
	case "confirmed":
		if target.Receipt.ResultID == nil || target.Receipt.AgentRunResult == nil {
			return errors.New("confirmed restart has no execution evidence")
		}
		child, err := readAIPlanRetryRun(tx, *target.Receipt.ResultID)
		if err != nil {
			return err
		}
		actual, err := loadAgentRunRestartProof(tx, child.AgentRun)
		if err != nil {
			return err
		}
		if child.ID == source.ID || child.TaskID != source.TaskID || child.ParentRunID != nil ||
			!sameAgentRunRestartSource(actual, proof) || !aiPlanRunMatchesPreview(child.AgentRun, preview) ||
			aiPlanContinuationActionBlocker(target.Receipt) == "evidence_unavailable" {
			return errors.New("plan restart lineage or new execution evidence changed")
		}
		return nil
	default:
		return errors.New("plan restart proposal state is unavailable")
	}
}
