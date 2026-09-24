package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// recoverAgentRunOutputInTransaction reuses durable output, never the model.
// The caller owns the OUTER transaction and must call deliveryFiles.finish only
// after that transaction has committed or rolled back. This lets an approval
// and its output delivery share one commit, including file-store compensation.
// Use withAgentRunOutputTransaction so failed SQLite COMMITs release the pinned
// connection before compensation inspects durable Artifact references.
func (a *API) recoverAgentRunOutputInTransaction(
	tx *gorm.DB, runID string, now time.Time, deliveryFiles *agentRunOutputFileDelivery,
) (models.AgentRun, error) {
	var run models.AgentRun
	if deliveryFiles == nil {
		return run, errors.New("Agent Run output file delivery state is missing")
	}
	if err := tx.First(&run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return run, newProjectRequestError(http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Agent run not found")
		}
		return run, err
	}
	if run.Status == "succeeded" &&
		(run.OutputDeliveryStatus == agentRunOutputSubmitted || run.OutputDeliveryStatus == agentRunOutputRetained) {
		return run, nil
	}
	if run.Status != "running" || run.OutputDeliveryStatus != agentRunOutputPending {
		return run, newProjectRequestError(http.StatusConflict, agentRunOutputNotPending,
			"Agent output delivery is not pending")
	}
	if run.OutputDeliveryPendingText == nil || run.OutputDeliveryPendingBytes == nil ||
		run.OutputDeliveryPendingCompletedAt == nil {
		return run, errors.New("Agent Run pending output is incomplete")
	}
	if err := a.completeAgentRunOutputInTransaction(
		tx, run, *run.OutputDeliveryPendingText, *run.OutputDeliveryPendingCompletedAt,
		now.UTC().Format(time.RFC3339Nano), deliveryFiles,
	); err != nil {
		return run, err
	}
	var response models.AgentRun
	err := tx.First(&response, "id = ?", runID).Error
	return response, err
}
