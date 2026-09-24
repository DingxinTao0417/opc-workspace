package api

import (
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Live progress is a projection, not a second log or a tool-result channel.
// Only the final transaction writes detailed steps; active snapshots die with
// the generation. Sequence 1 is the durable root, so details start at 2.
type aiRunProgress struct {
	Sequence    int    `json:"sequence"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	TurnIndex   int    `json:"turn_index,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	DurationMS  int64  `json:"duration_ms"`
	// Automatic retries of a transient upstream failure are a live run fact:
	// the active snapshot reports them, while terminal steps stay unchanged so
	// observability never needs a schema migration.
	RetryCount  int    `json:"retry_count,omitempty"`
	RetryReason string `json:"retry_reason,omitempty"`
	// A live-only count; terminal reply text discloses any history adjustment.
	TrimmedHistoryTurns int `json:"trimmed_history_turns,omitempty"`
	// Live-only: older requeryable read results replaced by an explicit marker.
	CompactedToolResults int `json:"compacted_tool_results,omitempty"`
}

func aiProgressToolName(name string) string {
	switch name {
	case "memory_search", "memory_write", "memory_propose",
		"workspace_guide", "workspace_request_access", "workspace_plan", "workspace_search", "workspace_get",
		"workspace_outputs", "workspace_agent_runs", "workspace_task_submissions", "workspace_read_artifact_file",
		"workspace_inbox_tasks", "workspace_inbox_source", "workspace_task_options", "workspace_task_assignments",
		"workspace_task_views", "workspace_today", "workspace_tasks", "workspace_projects", "workspace_focus", "workspace_focus_report",
		"workspace_project_notes", "workspace_project_outputs", "workspace_content_items", "workspace_roadmap_milestones", "workspace_client_records", "workspace_automations",
		"workspace_finance", "workspace_agent_execution", "workspace_delegate_agent", "workspace_agent_followup", "workspace_agent_children", "workspace_propose",
		"knowledge_search", "knowledge_read", "knowledge_library":
		return name
	default:
		// This also protects recovery of older traces containing a model's
		// arbitrary, unregistered tool name. Never echo it into live progress.
		return "unknown_tool"
	}
}

func aiProgressFromStep(sequence int, step harness.RunStep) aiRunProgress {
	p := aiRunProgress{Sequence: sequence, Kind: step.Kind, Status: step.Status,
		StartedAt: step.StartedAt.UTC().Format(time.RFC3339Nano)}
	if step.Kind == "tool_call" {
		p.ToolName = aiProgressToolName(step.ToolName)
	}
	if step.Kind == "model_turn" || step.Kind == "self_check" {
		p.TurnIndex = max(1, step.TurnIndex)
		p.RetryCount = max(0, step.RetryCount)
		p.RetryReason = step.RetryReason
		p.TrimmedHistoryTurns = min(200, max(0, step.TrimmedHistoryTurns))
		p.CompactedToolResults = min(harness.DefaultMaxToolCalls, max(0, step.CompactedToolResults))
	}
	if step.Status != "running" {
		completed := step.CompletedAt
		if completed.Before(step.StartedAt) {
			completed = step.StartedAt
		}
		p.CompletedAt = completed.UTC().Format(time.RFC3339Nano)
		p.DurationMS = max(int64(0), step.DurationMS)
	}
	return p
}

func (r *aiGenerationRegistry) setProgress(id string, step aiRunProgress) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.snapshots[id]
	if !ok || step.Sequence < 2 || step.Sequence > aiMaxDetailedRunSteps+2 {
		return false
	}
	index := step.Sequence - 2
	if index > len(value.Progress) {
		return false
	}
	if index < len(value.Progress) {
		previous := value.Progress[index]
		if index != len(value.Progress)-1 || previous.Status != "running" || step.Status == "running" ||
			previous.Kind != step.Kind || previous.StartedAt != step.StartedAt || previous.ToolName != step.ToolName || previous.TurnIndex != step.TurnIndex {
			return false
		}
		value.Progress[index] = step
	} else {
		if index > 0 && value.Progress[index-1].Status == "running" {
			return false
		}
		value.Progress = append(value.Progress, step)
	}
	r.snapshots[id] = value
	return true
}

func (a *API) completedRunProgress(generationID string) ([]aiRunProgress, error) {
	var rows []models.AIRunStep
	if err := a.db.Where("generation_id = ? AND sequence > 1", generationID).
		Order("sequence ASC").Limit(aiMaxDetailedRunSteps + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	progress := make([]aiRunProgress, 0, len(rows))
	for _, row := range rows {
		p := aiRunProgress{Sequence: row.Sequence, Kind: row.Kind, Status: row.Status, StartedAt: row.StartedAt}
		if row.CompletedAt != nil {
			p.CompletedAt = *row.CompletedAt
		}
		if row.DurationMS != nil {
			p.DurationMS = *row.DurationMS
		}
		if row.TurnIndex != nil {
			p.TurnIndex = *row.TurnIndex
		}
		if row.ToolName != nil {
			p.ToolName = aiProgressToolName(*row.ToolName)
		}
		progress = append(progress, p)
	}
	return progress, nil
}
