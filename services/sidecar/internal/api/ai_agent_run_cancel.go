package api

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiAgentRunCancelPreview struct {
	RunID       string `json:"run_id"`
	TaskID      string `json:"task_id"`
	TaskTitle   string `json:"task_title,omitempty"`
	TaskVersion int64  `json:"task_version"`
	Status      string `json:"status"`
	Attempt     int    `json:"attempt"`
	ProviderID  string `json:"provider_id"`
	Model       string `json:"model"`
}

func parseAIAgentRunControl(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for key := range raw {
		if key != "action" && key != "task_id" && key != "agent_run_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("unsupported Agent execution control field")
		}
	}
	for _, id := range []string{input.TaskID, input.AgentRunID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return input, errors.New("Agent execution control requires canonical Task and Run IDs")
		}
	}
	if input.ExpectedVersion < 1 || len(fields) != 0 {
		return input, errors.New("Agent execution control requires the current Task version and empty changes")
	}
	input.Changes = json.RawMessage(`{}`)
	return input, nil
}

func previewAIAgentRunCancel(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var task models.Task
	if err := tx.Select("id,title,version").First(&task, "id=?", input.TaskID).Error; err != nil {
		return preview, err
	}
	if task.Version != input.ExpectedVersion {
		return preview, newProjectRequestError(409, "VERSION_CONFLICT", "Task changed; query it again")
	}
	var run models.AgentRun
	if err := tx.Select("id,task_id,status,attempt,provider_id,model,output_delivery_status").First(&run, "id=? AND task_id=?", input.AgentRunID, input.TaskID).Error; err != nil {
		return preview, err
	}
	if run.Status != "queued" && run.Status != "running" {
		return preview, newProjectRequestError(409, "AGENT_RUN_NOT_ACTIVE", "Agent run is already terminal")
	}
	if run.OutputDeliveryStatus != agentRunOutputNotReady {
		return preview, newProjectRequestError(409, agentRunOutputPendingCode, "Recover completed output delivery instead of cancelling")
	}
	requested, err := agentRunCancellationRequested(tx, run.ID)
	if err != nil {
		return preview, err
	}
	if requested {
		return preview, newProjectRequestError(409, "AGENT_RUN_CANCEL_ALREADY_REQUESTED", "Cancellation is already accepted; wait for the executor to settle")
	}
	preview.Label = task.Title
	preview.AgentRunCancel = &aiAgentRunCancelPreview{RunID: run.ID, TaskID: task.ID, TaskVersion: task.Version, Status: run.Status, Attempt: run.Attempt, ProviderID: run.ProviderID, Model: run.Model}
	return preview, nil
}

func readAIAgentRunCancelResult(tx *gorm.DB, out aiActionResponse) (*aiAgentRunResult, error) {
	p := out.Preview.AgentRunCancel
	if p == nil || out.ResultID == nil || *out.ResultID != p.RunID || out.ResultVersion == nil || *out.ResultVersion != 1 {
		return nil, errors.New("invalid cancellation receipt")
	}
	var run models.AgentRun
	if err := tx.Select("id,task_id,status,attempt,provider_id,model,output_delivery_status").First(&run, "id=?", p.RunID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if run.TaskID != p.TaskID || run.Attempt != p.Attempt || run.ProviderID != p.ProviderID || run.Model != p.Model || (run.Status != "running" && run.Status != "cancelled") || run.OutputDeliveryStatus != agentRunOutputNotReady {
		return nil, errors.New("inconsistent cancellation receipt")
	}
	return &aiAgentRunResult{ID: run.ID, TaskID: run.TaskID, Status: run.Status, Attempt: run.Attempt, ProviderID: run.ProviderID, Model: run.Model, OutputDeliveryStatus: run.OutputDeliveryStatus}, nil
}
