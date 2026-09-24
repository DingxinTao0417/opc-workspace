package api

import (
	"errors"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const agentRunCancelRequestedEvent = "agent_run_cancel_requested"

// A running executor stays active until it settles. Persist intent before
// signalling its context, so a late model result or process restart cannot
// silently turn an accepted cancellation into a successful submission.
// This command must run in the caller's transaction; signal only after commit.
func requestAgentRunCancellation(tx *gorm.DB, runID, requestID, now string) error {
	var run models.AgentRun
	if err := tx.First(&run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newProjectRequestError(http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Agent run not found")
		}
		return err
	}
	if run.Status == "running" && run.OutputDeliveryStatus == agentRunOutputPending {
		return newProjectRequestError(http.StatusConflict, agentRunOutputPendingCode,
			"The model result is complete and its output delivery must be retried instead of cancelled")
	}
	if run.Status != "queued" && run.Status != "running" {
		return nil
	}
	requested, err := agentRunCancellationRequested(tx, runID)
	if err != nil {
		return err
	}
	if !requested {
		if err := recordAgentRunWorkflowEvent(tx, agentRunCancelRequestedEvent, runID, nil, requestID, now); err != nil {
			return err
		}
	}
	if run.Status == "running" {
		return nil
	}
	result := tx.Model(&models.AgentRun{}).Where("id = ? AND status = 'queued'", runID).
		Updates(map[string]any{"status": "cancelled", "started_at": now, "completed_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return recordAgentRunWorkflowEvent(tx, "agent_run_cancelled", runID, nil, requestID, now)
	}
	return nil
}

func agentRunCancellationRequested(tx *gorm.DB, runID string) (bool, error) {
	var count int64
	err := tx.Table("workflow_events").Where("aggregate_type = 'agent_run' AND aggregate_id = ? AND action = ?", runID, agentRunCancelRequestedEvent).Count(&count).Error
	return count > 0, err
}

// Must be checked inside the same transaction that would accept output or
// another terminal disposition. Pending output cannot coexist with a request
// through the public command: whichever transaction commits first wins.
func settleRequestedAgentRunCancellation(tx *gorm.DB, runID, now string) (bool, error) {
	requested, err := agentRunCancellationRequested(tx, runID)
	if err != nil || !requested {
		return false, err
	}
	result := tx.Model(&models.AgentRun{}).
		Where("id = ? AND status = 'running' AND output_delivery_status = ?", runID, agentRunOutputNotReady).
		Updates(map[string]any{"status": "cancelled", "error_code": nil, "completed_at": now})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		if err := recordAgentRunWorkflowEvent(tx, "agent_run_cancelled", runID, nil, "", now); err != nil {
			return false, err
		}
	}
	return true, nil
}
