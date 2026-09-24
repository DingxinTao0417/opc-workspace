package api

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Read only the bounded identity projection from the atomic approval event.
// Current Inbox relations may have changed and are not historical evidence.
func readAIInboxSplitCreatedTaskIDs(tx *gorm.DB, row models.AIActionProposal, out aiActionResponse) ([]string, error) {
	var events []struct{ CurrentJSON string }
	if err := tx.Table("workflow_events").Select("current_json").
		Where("aggregate_type = ? AND aggregate_id = ? AND action = ? AND created_at = ?", "ai_action_proposal", row.ID, "ai_workspace_action_confirmed", row.DecidedAt).
		Limit(2).Scan(&events).Error; err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	} // Legacy receipts have no mapping.
	invalid := errors.New("invalid inbox split receipt")
	if len(events) != 1 {
		return nil, invalid
	}
	var saved struct {
		GenerationID   string   `json:"generation_id"`
		ProposalID     string   `json:"proposal_id"`
		Fingerprint    string   `json:"fingerprint"`
		ResultID       string   `json:"result_id"`
		ResultVersion  int64    `json:"result_version"`
		CreatedTaskIDs []string `json:"created_task_ids"`
	}
	if err := json.Unmarshal([]byte(events[0].CurrentJSON), &saved); err != nil {
		return nil, invalid
	}
	if saved.CreatedTaskIDs == nil {
		return nil, nil
	}
	if saved.GenerationID != row.GenerationID || saved.ProposalID != row.ID || saved.Fingerprint != row.Fingerprint || row.ResultID == nil || saved.ResultID != *row.ResultID || row.ResultVersion == nil || saved.ResultVersion != *row.ResultVersion || len(saved.CreatedTaskIDs) != len(out.Preview.Tasks) || len(saved.CreatedTaskIDs) < 1 || len(saved.CreatedTaskIDs) > 20 {
		return nil, invalid
	}
	seen := make(map[string]bool)
	for _, id := range saved.CreatedTaskIDs {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id || seen[id] {
			return nil, invalid
		}
		seen[id] = true
	}
	return saved.CreatedTaskIDs, nil
}
