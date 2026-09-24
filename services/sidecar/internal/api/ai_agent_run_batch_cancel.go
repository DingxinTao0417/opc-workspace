package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	aiAgentRunCancelManyAction = "agent_run.cancel_many"
	maxAIAgentRunCancelItems   = 8
)

type aiAgentRunCancelManyItem struct {
	AgentRunID          string `json:"agent_run_id"`
	TaskID              string `json:"task_id"`
	ExpectedTaskVersion int64  `json:"expected_task_version"`
}

type aiAgentRunCancelManyChanges struct {
	Items []aiAgentRunCancelManyItem `json:"items"`
}

type aiAgentRunCancelManyPreview struct {
	Count int                       `json:"count"`
	Items []aiAgentRunCancelPreview `json:"items"`
}

type aiAgentRunCancelManyResultItem struct {
	RunID  string `json:"run_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type aiAgentRunCancelManyResult struct {
	Count int                              `json:"count"`
	Items []aiAgentRunCancelManyResultItem `json:"items"`
}

func isAIAgentRunCancelAction(action string) bool {
	return action == "agent_run.cancel" || action == aiAgentRunCancelManyAction
}

func addAIAgentRunCancelManySchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), aiAgentRunCancelManyAction)
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " agent_run.cancel_many stops 1-8 exact queued/running Runs in one all-or-nothing human confirmation. changes.items must copy each run_id, task_id and current task version from workspace_agent_runs/current reads. It never cancels Tasks, deletes output or guarantees remote billing stops."
	fields := changes["properties"].(map[string]any)
	fields["items"] = map[string]any{
		"type": "array", "minItems": 1, "maxItems": maxAIAgentRunCancelItems,
		"items": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"agent_run_id", "task_id", "expected_task_version"},
			"properties": map[string]any{
				"agent_run_id":          map[string]any{"type": "string", "format": "uuid"},
				"task_id":               map[string]any{"type": "string", "format": "uuid"},
				"expected_task_version": map[string]any{"type": "integer", "minimum": 1},
			},
		},
	}
}

func parseAIAgentRunCancelMany(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for key := range raw {
		if key != "action" && key != "changes" {
			return input, errors.New("batch Agent cancellation accepts only action and changes")
		}
	}
	if len(fields) != 1 || fields["items"] == nil {
		return input, errors.New("batch Agent cancellation requires only changes.items")
	}
	var changes aiAgentRunCancelManyChanges
	if err := decodeStrictJSONBytes(input.Changes, &changes); err != nil {
		return input, err
	}
	if len(changes.Items) < 1 || len(changes.Items) > maxAIAgentRunCancelItems {
		return input, fmt.Errorf("items must contain 1-%d Agent runs", maxAIAgentRunCancelItems)
	}
	seenRuns, seenTasks := map[string]bool{}, map[string]bool{}
	for _, item := range changes.Items {
		for _, id := range []string{item.AgentRunID, item.TaskID} {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return input, errors.New("batch Agent cancellation requires canonical Run and Task IDs")
			}
		}
		if item.ExpectedTaskVersion < 1 {
			return input, errors.New("batch Agent cancellation requires current positive Task versions")
		}
		if seenRuns[item.AgentRunID] || seenTasks[item.TaskID] {
			return input, errors.New("batch Agent cancellation cannot repeat a Run or Task")
		}
		seenRuns[item.AgentRunID], seenTasks[item.TaskID] = true, true
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return input, err
	}
	input.Changes = encoded
	return input, nil
}

func decodeAIAgentRunCancelManyChanges(input aiWorkspaceAction) (aiAgentRunCancelManyChanges, error) {
	var changes aiAgentRunCancelManyChanges
	if err := decodeStrictJSONBytes(input.Changes, &changes); err != nil {
		return changes, err
	}
	if len(changes.Items) < 1 || len(changes.Items) > maxAIAgentRunCancelItems {
		return changes, errors.New("invalid batch Agent cancellation")
	}
	return changes, nil
}

func previewAIAgentRunCancelMany(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	changes, err := decodeAIAgentRunCancelManyChanges(input)
	if err != nil {
		return preview, err
	}
	batch := aiAgentRunCancelManyPreview{Count: len(changes.Items), Items: make([]aiAgentRunCancelPreview, 0, len(changes.Items))}
	for _, item := range changes.Items {
		single := aiWorkspaceAction{Action: "agent_run.cancel", TaskID: item.TaskID, AgentRunID: item.AgentRunID, ExpectedVersion: item.ExpectedTaskVersion, Changes: json.RawMessage(`{}`)}
		candidate, err := previewAIAgentRunCancel(tx, single)
		if err != nil {
			return preview, err
		}
		if candidate.AgentRunCancel == nil {
			return preview, errors.New("invalid Agent cancellation preview")
		}
		itemPreview := *candidate.AgentRunCancel
		itemPreview.TaskTitle = candidate.Label
		batch.Items = append(batch.Items, itemPreview)
	}
	preview.Label = fmt.Sprintf("停止 %d 个 Agent 执行", batch.Count)
	preview.Before["active_runs"] = batch.Count
	preview.After["cancel_requested_runs"] = batch.Count
	preview.AgentRunCancelMany = &batch
	return preview, nil
}

func executeAIAgentRunCancelMany(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) ([]string, error) {
	changes, err := decodeAIAgentRunCancelManyChanges(input)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(changes.Items))
	for _, item := range changes.Items {
		if err := requestAgentRunCancellation(tx, item.AgentRunID, requestID, now); err != nil {
			return nil, err
		}
		ids = append(ids, item.AgentRunID)
	}
	return ids, nil
}

func readAIAgentRunCancelManyResult(tx *gorm.DB, out aiActionResponse) (*aiAgentRunCancelManyResult, error) {
	p := out.Preview.AgentRunCancelMany
	if p == nil || p.Count < 1 || p.Count != len(p.Items) || out.ResultID == nil || *out.ResultID != out.ID || out.ResultVersion == nil || *out.ResultVersion != 1 {
		return nil, errors.New("invalid batch cancellation receipt")
	}
	result := &aiAgentRunCancelManyResult{Count: p.Count, Items: make([]aiAgentRunCancelManyResultItem, 0, p.Count)}
	for _, item := range p.Items {
		var run models.AgentRun
		if err := tx.Select("id,task_id,status,attempt,provider_id,model,output_delivery_status").First(&run, "id=?", item.RunID).Error; err != nil {
			return nil, err
		}
		if run.TaskID != item.TaskID || run.Attempt != item.Attempt || run.ProviderID != item.ProviderID || run.Model != item.Model || (run.Status != "running" && run.Status != "cancelled") || run.OutputDeliveryStatus != agentRunOutputNotReady {
			return nil, errors.New("inconsistent batch cancellation receipt")
		}
		result.Items = append(result.Items, aiAgentRunCancelManyResultItem{RunID: run.ID, TaskID: run.TaskID, Status: run.Status})
	}
	return result, nil
}
