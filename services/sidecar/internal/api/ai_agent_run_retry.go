package api

import (
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiAgentRunRetryPreview struct {
	RunID   string `json:"run_id"`
	Status  string `json:"status"`
	Attempt int    `json:"attempt"`
}

func prepareAIAgentRunRetry(tx *gorm.DB, input aiWorkspaceAction, store *artifactStore) (preparedAgentRun, models.AgentRun, error) {
	// Reject cross-Task references and successful Runs before touching files.
	var previous models.AgentRun
	if err := tx.Select("id,task_id,status").First(&previous, "id=? AND task_id=?", input.AgentRunID, input.TaskID).Error; err != nil {
		return preparedAgentRun{}, previous, err
	}
	if previous.Status != "failed" && previous.Status != "cancelled" && previous.Status != "interrupted" {
		return preparedAgentRun{}, previous, newProjectRequestError(409, "AGENT_RUN_NOT_RETRYABLE", "Only failed, cancelled or interrupted runs can be proposed for retry; pending output must be recovered")
	}
	prepared, previous, err := prepareAgentRunRetry(tx, input.AgentRunID, store)
	if err == nil && prepared.Task.Version != input.ExpectedVersion {
		err = agentRunIdentityChangedError()
	}
	return prepared, previous, err
}

func previewAIAgentRunRetry(tx *gorm.DB, input aiWorkspaceAction, store *artifactStore) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	prepared, previous, err := prepareAIAgentRunRetry(tx, input, store)
	if err != nil {
		return p, err
	}
	typed := aiAgentRunPreviewFromPrepared(prepared)
	p.Label = typed.Task.Title
	p.AgentRunStart = &typed
	p.AgentRunRetry = &aiAgentRunRetryPreview{RunID: previous.ID, Status: previous.Status, Attempt: previous.Attempt}
	return p, nil
}
