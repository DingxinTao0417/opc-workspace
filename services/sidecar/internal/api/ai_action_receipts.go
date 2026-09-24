package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var (
	errAIActionReceiptSourceInvalid     = errors.New("invalid action receipt source")
	errAIActionReceiptSourceUnavailable = errors.New("unavailable action receipt source")
)

// A selected receipt is an explicit historical source, never a permission grant.
// Check ownership, persistence and terminal state together, then rebuild its
// metadata projection in the same read-only snapshot. All unavailable sources
// deliberately share one error, including foreign conversations.
func (a *API) aiActionReceiptsForGeneration(ctx context.Context, sessionID, generationID string) (string, error) {
	for _, value := range []string{sessionID, generationID} {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value {
			return "", errAIActionReceiptSourceInvalid
		}
	}
	var receipts string
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateAIActionReceiptSource(tx, sessionID, generationID); err != nil {
			return err
		}
		var err error
		receipts, err = a.buildAIActionReceipts(tx, sessionID, generationID)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", errAIActionReceiptSourceUnavailable
	}
	return receipts, nil
}

func validateAIActionReceiptSource(db *gorm.DB, sessionID, generationID string) error {
	var count int64
	err := db.Table("ai_generations AS g").
		Joins("JOIN ai_sessions AS s ON s.id = g.session_id").
		Where("g.id = ? AND g.session_id = ? AND s.persist = ?", generationID, sessionID, true).
		Where("g.status IN ('completed', 'failed', 'cancelled')").
		Where("EXISTS (SELECT 1 FROM ai_action_proposals AS p WHERE p.generation_id = g.id)").
		Count(&count).Error
	if err != nil || count != 1 {
		return errAIActionReceiptSourceUnavailable
	}
	return nil
}

// Receipts are conversation-owned facts, not a grant or a fresh workspace read.
// Rebuild them from decisions on every send, independently of lossy compaction.
// Never send proposal changes/previews. Actions whose confirmation meaning
// depends on a follow-up execution read only their bounded receipt facts:
// automation retries read immutable Run outcome metadata, while Agent starts/cancellations
// read the Run lifecycle and delivery status. Missing Agent facts after a
// confirmed result identify a cascade-deleted Task/Run tombstone.
func (a *API) aiActionReceipts(ctx context.Context, sessionID string) (string, error) {
	return a.buildAIActionReceipts(a.db.WithContext(ctx), sessionID, "")
}

func (a *API) buildAIActionReceipts(db *gorm.DB, sessionID, sourceGenerationID string) (string, error) {
	const limit = 16
	var rows []struct {
		models.AIActionProposal
		GenerationStatus string `gorm:"column:generation_status"`
	}
	query := db.Table("ai_action_proposals AS p").
		Select("p.*, g.status AS generation_status").
		Joins("JOIN ai_generations AS g ON g.id = p.generation_id").
		Where("g.session_id = ?", sessionID)
	if sourceGenerationID != "" {
		query = query.Where("g.id = ?", sourceGenerationID)
	}
	err := query.
		Order("julianday(COALESCE(p.decided_at, p.created_at)) DESC, p.id DESC").
		Limit(limit + 1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return "", err
	}
	type receipt struct {
		ProposalID          string                   `json:"proposal_id"`
		GenerationID        string                   `json:"generation_id"`
		Action              string                   `json:"action"`
		Status              string                   `json:"status"`
		Route               string                   `json:"route,omitempty"`
		TargetID            string                   `json:"target_id,omitempty"`
		ResultID            *string                  `json:"result_id,omitempty"`
		ResultVersion       *int64                   `json:"result_version,omitempty"`
		DecidedAt           *string                  `json:"decided_at,omitempty"`
		AutomationRunResult *aiAutomationRetryResult `json:"automation_run_result,omitempty"`
		InboxReadAllResult  *readAllInboxItemsOutput `json:"inbox_read_all_result,omitempty"`
		AgentRunResult      *aiAgentRunResult        `json:"agent_run_result,omitempty"`
		CreatedTaskIDs      []string                 `json:"created_task_ids,omitempty"`
	}
	now := a.options.Now()
	envelope := struct {
		AsOf               string    `json:"as_of"`
		SourceGenerationID string    `json:"source_generation_id,omitempty"`
		Limited            bool      `json:"limited"`
		Items              []receipt `json:"items"`
	}{AsOf: now.UTC().Format(time.RFC3339Nano), SourceGenerationID: sourceGenerationID, Limited: len(rows) > limit, Items: []receipt{}}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	for _, row := range rows {
		out, err := aiActionOutputWithFacts(db, row.AIActionProposal, row.GenerationStatus, now)
		if err != nil {
			return "", err
		}
		_, targetID := aiActionTarget(out.Action)
		if out.Action.Action == "finance.export_csv" && out.Status == "confirmed" {
			out.Status = "approved_download_unverified"
		}
		item := receipt{
			ProposalID: out.ID, GenerationID: out.GenerationID, Action: out.Action.Action,
			Status: out.Status, TargetID: targetID, Route: out.Route, ResultID: out.ResultID,
			ResultVersion: out.ResultVersion, DecidedAt: out.DecidedAt,
			AutomationRunResult: out.AutomationRunResult,
			InboxReadAllResult:  out.InboxReadAllResult,
			AgentRunResult:      out.AgentRunResult,
			CreatedTaskIDs:      out.CreatedTaskIDs,
		}
		if out.TaskBatchCreateResult != nil {
			for _, created := range out.TaskBatchCreateResult.Items {
				item.CreatedTaskIDs = append(item.CreatedTaskIDs, created.ID)
			}
		}
		envelope.Items = append(envelope.Items, item)
	}
	for {
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return "", err
		}
		if len(encoded) <= 8<<10 {
			return string(encoded), nil
		}
		envelope.Limited = true
		envelope.Items = envelope.Items[:len(envelope.Items)-1]
	}
}
