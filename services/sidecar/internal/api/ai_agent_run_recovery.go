package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiAgentRunRecoverOutput = "agent_run.recover_output"

// Local approval metadata only: never return the staged result to the model.
type aiAgentRunRecoveryPreview struct {
	aiAgentRunCancelPreview
	OutputKind   string `json:"output_kind"`
	OutputBytes  int    `json:"output_bytes"`
	OutputSHA256 string `json:"output_sha256"`
}

func previewAIAgentRunRecovery(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var task models.Task
	if err := tx.Select("id,title,version").First(&task, "id=?", input.TaskID).Error; err != nil {
		return p, err
	}
	if task.Version != input.ExpectedVersion {
		return p, newProjectRequestError(409, "VERSION_CONFLICT", "Task changed; query it again")
	}
	var run models.AgentRun
	if err := tx.First(&run, "id=? AND task_id=?", input.AgentRunID, input.TaskID).Error; err != nil {
		return p, err
	}
	if run.Status != "running" || run.OutputDeliveryStatus != agentRunOutputPending {
		return p, newProjectRequestError(409, agentRunOutputNotPending, "Output delivery is no longer pending; inspect the exact Run")
	}
	if run.OutputDeliveryPendingText == nil || run.OutputDeliveryPendingBytes == nil || run.OutputDeliveryPendingCompletedAt == nil {
		return p, errors.New("incomplete pending output")
	}
	contract, err := agentRunOutputContract(run)
	if err != nil {
		return p, err
	}
	hash := sha256.Sum256([]byte(*run.OutputDeliveryPendingText))
	p.Label = task.Title
	p.AgentRunRecovery = &aiAgentRunRecoveryPreview{
		aiAgentRunCancelPreview: aiAgentRunCancelPreview{RunID: run.ID, TaskID: task.ID, TaskVersion: task.Version, Status: run.Status, Attempt: run.Attempt, ProviderID: run.ProviderID, Model: run.Model},
		OutputKind:              contract.Type, OutputBytes: len([]byte(*run.OutputDeliveryPendingText)), OutputSHA256: hex.EncodeToString(hash[:]),
	}
	return p, nil
}

func readAIAgentRunRecoveryResult(tx *gorm.DB, out aiActionResponse) (*aiAgentRunResult, error) {
	p := out.Preview.AgentRunRecovery
	if p == nil || out.ResultID == nil || *out.ResultID != p.RunID || out.ResultVersion == nil || *out.ResultVersion != 1 {
		return nil, errors.New("invalid output recovery receipt")
	}
	var run models.AgentRun
	if err := tx.Select("id,task_id,status,attempt,provider_id,model,output_delivery_status,output_delivery_error_code,submission_id,artifact_id").First(&run, "id=?", p.RunID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if run.TaskID != p.TaskID || run.Attempt != p.Attempt || run.ProviderID != p.ProviderID || run.Model != p.Model || run.Status != "succeeded" || (run.OutputDeliveryStatus != agentRunOutputSubmitted && run.OutputDeliveryStatus != agentRunOutputRetained) {
		return nil, errors.New("inconsistent output recovery receipt")
	}
	submissionStatus, err := readAgentRunSubmissionStatus(tx, run)
	if err != nil {
		return nil, err
	}
	return &aiAgentRunResult{ID: run.ID, TaskID: run.TaskID, Status: run.Status, Attempt: run.Attempt, ProviderID: run.ProviderID, Model: run.Model, OutputDeliveryStatus: run.OutputDeliveryStatus, OutputDeliveryErrorCode: run.OutputDeliveryErrorCode, SubmissionID: run.SubmissionID, SubmissionStatus: submissionStatus, ArtifactID: run.ArtifactID}, nil
}
