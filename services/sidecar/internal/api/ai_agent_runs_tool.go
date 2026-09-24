package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiAgentRunsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{
		"status":{"type":"string","enum":["queued","running","succeeded","failed","cancelled","interrupted"]},
		"output_delivery_status":{"type":"string","enum":["not_ready","pending","submitted","retained"]},
		"task_id":{"type":"string","format":"uuid","description":"Optional exact Task identity to narrow the execution ledger"},
		"parent_run_id":{"type":"string","format":"uuid","description":"Optional exact parent Run identity to list direct retry children"},
		"limit":{"type":"integer","minimum":1,"maximum":20},
		"offset":{"type":"integer","minimum":0,"maximum":1000}
	}}`)
}

func (t *aiWorkspaceTool) agentRuns(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.authorize("agent_run"); err != nil {
		return nil, err
	}
	var input struct {
		Status      string `json:"status"`
		Delivery    string `json:"output_delivery_status"`
		TaskID      string `json:"task_id"`
		ParentRunID string `json:"parent_run_id"`
		Limit       *int   `json:"limit"`
		Offset      int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	if _, ok := agentRunStatusSet[input.Status]; input.Status != "" && !ok {
		return nil, errors.New("invalid Agent run status")
	}
	if _, ok := agentRunOutputDeliveryStatusSet[input.Delivery]; input.Delivery != "" && !ok {
		return nil, errors.New("invalid Agent output delivery status")
	}
	for name, value := range map[string]string{"task_id": input.TaskID, "parent_run_id": input.ParentRunID} {
		if value == "" {
			continue
		}
		parsed, parseErr := uuid.Parse(value)
		if parseErr != nil || parsed.String() != value {
			return nil, errors.New(name + " must be a canonical UUID")
		}
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	type item struct {
		ID                      string                    `json:"id"`
		TaskID                  string                    `json:"task_id"`
		TaskTitle               string                    `json:"task_title"`
		ParentRunID             *string                   `json:"parent_run_id"`
		RestartOfRunID          *string                   `json:"restart_of_run_id,omitempty" gorm:"-"`
		Attempt                 int                       `json:"attempt"`
		Status                  string                    `json:"status"`
		OutputDeliveryStatus    string                    `json:"output_delivery_status"`
		SubmissionID            *string                   `json:"submission_id,omitempty"`
		SubmissionStatus        *string                   `json:"submission_status,omitempty"`
		ErrorCode               *string                   `json:"error_code"`
		OutputDeliveryErrorCode *string                   `json:"output_delivery_error_code"`
		CreatedAt               string                    `json:"created_at"`
		StartedAt               *string                   `json:"started_at,omitempty"`
		CompletedAt             *string                   `json:"completed_at,omitempty"`
		Route                   string                    `json:"route" gorm:"-"`
		StartGate               *models.AgentRunStartGate `json:"start_gate,omitempty" gorm:"-"`
	}
	items := []item{}
	var counts struct {
		ActiveTotal          int64 `json:"active_total"`
		PendingDeliveryTotal int64 `json:"pending_delivery_total"`
	}
	var total int64
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("agent_runs").Select(`
			COALESCE(SUM(CASE WHEN status IN ('queued','running') THEN 1 ELSE 0 END),0) AS active_total,
			COALESCE(SUM(CASE WHEN output_delivery_status = 'pending' THEN 1 ELSE 0 END),0) AS pending_delivery_total`).Scan(&counts).Error; err != nil {
			return err
		}
		build := func() *gorm.DB {
			q := tx.Table("agent_runs AS run").
				Joins("JOIN tasks AS task ON task.id = run.task_id").
				Joins("LEFT JOIN task_submissions AS submission ON submission.id = run.submission_id AND submission.task_id = run.task_id")
			if input.Status != "" {
				q = q.Where("run.status = ?", input.Status)
			}
			if input.Delivery != "" {
				q = q.Where("run.output_delivery_status = ?", input.Delivery)
			}
			if input.TaskID != "" {
				q = q.Where("run.task_id = ?", input.TaskID)
			}
			if input.ParentRunID != "" {
				q = q.Where("run.parent_run_id = ?", input.ParentRunID)
			}
			return q
		}
		if err := build().Count(&total).Error; err != nil {
			return err
		}
		if err := build().Select(`run.id, run.task_id, substr(task.title,1,200) AS task_title,
			run.parent_run_id, run.attempt, run.status, run.output_delivery_status, run.submission_id,
			CASE WHEN run.output_delivery_status = 'submitted' THEN submission.status END AS submission_status,
			run.error_code, run.output_delivery_error_code, run.created_at,
			run.started_at, run.completed_at`).
			Order("run.created_at DESC, run.id DESC").Limit(limit).Offset(input.Offset).Scan(&items).Error; err != nil {
			return err
		}
		ids := make([]string, len(items))
		for index := range items {
			ids[index] = items[index].ID
			proof, err := loadAgentRunRestartProof(tx, models.AgentRun{ID: items[index].ID, TaskID: items[index].TaskID})
			if err != nil {
				return err
			}
			if proof != nil {
				items[index].RestartOfRunID = &proof.RunID
			}
		}
		gates, err := readAgentRunStartGates(tx, ids)
		if err != nil {
			return err
		}
		for index := range items {
			items[index].StartGate = gates[items[index].ID]
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	for i := range items {
		items[i].Route = agentRunRoute(items[i].TaskID, items[i].ID)
	}
	var next *int
	more := int64(input.Offset+len(items)) < total
	if n := input.Offset + len(items); more && len(items) > 0 && n <= 1000 {
		next = &n
	}
	return map[string]any{"items": items, "total": total, "global_counts": counts, "next_offset": next, "has_more": more, "window_limited": more && next == nil}, nil
}
