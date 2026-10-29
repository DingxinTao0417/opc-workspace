package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func (a *API) listTaskAgentRuns(c *gin.Context) {
	taskID, ok := taskID(c)
	if !ok {
		return
	}
	var rows []models.AgentRun
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Order("created_at DESC").Order("id DESC").Limit(50).Find(&rows, "task_id=?", taskID).Error; err != nil {
			return err
		}
		for index := range rows {
			if err := projectAgentRunRestart(tx, &rows[index]); err != nil {
				return err
			}
		}
		return projectAgentRunStartGates(tx, rows)
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	results := make([]agentRunResponse, len(rows))
	for index := range rows {
		results[index] = agentRunResponseFromModel(rows[index])
		results[index].Progress = a.agentRunProgressFor(rows[index].ID, rows[index].Status)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": results})
}

type agentRunSummaryResponse struct {
	ID                      string                    `json:"id"`
	TaskID                  string                    `json:"task_id"`
	AssignmentID            string                    `json:"assignment_id"`
	ActorID                 string                    `json:"actor_id"`
	AdapterID               string                    `json:"adapter_id"`
	CreatedByActorID        string                    `json:"created_by_actor_id"`
	ParentRunID             *string                   `json:"parent_run_id"`
	RestartOfRunID          *string                   `json:"restart_of_run_id,omitempty"`
	Attempt                 int                       `json:"attempt"`
	Status                  string                    `json:"status"`
	ProviderID              string                    `json:"provider_id"`
	Model                   string                    `json:"model"`
	ResultBytes             *int                      `json:"result_bytes"`
	ErrorCode               *string                   `json:"error_code"`
	OutputDeliveryStatus    string                    `json:"output_delivery_status"`
	OutputDeliveryErrorCode *string                   `json:"output_delivery_error_code"`
	SubmissionID            *string                   `json:"submission_id"`
	ArtifactID              *string                   `json:"artifact_id"`
	StartedAt               *string                   `json:"started_at"`
	CompletedAt             *string                   `json:"completed_at"`
	CreatedAt               string                    `json:"created_at"`
	TaskTitle               string                    `json:"task_title"`
	Progress                *agentRunProgress         `json:"progress,omitempty" gorm:"-"`
	StartGate               *models.AgentRunStartGate `json:"start_gate,omitempty" gorm:"-"`
}

var agentRunStatusSet = map[string]struct{}{
	"queued": {}, "running": {}, "succeeded": {},
	"failed": {}, "cancelled": {}, "interrupted": {},
}

var agentRunOutputDeliveryStatusSet = map[string]struct{}{
	agentRunOutputNotReady:  {},
	agentRunOutputPending:   {},
	agentRunOutputSubmitted: {},
	agentRunOutputRetained:  {},
}

type agentRunListMeta struct {
	Page                 int   `json:"page"`
	PageSize             int   `json:"page_size"`
	Total                int64 `json:"total"`
	ActiveTotal          int64 `json:"active_total"`
	PendingDeliveryTotal int64 `json:"pending_delivery_total"`
	SucceededTotal       int64 `json:"succeeded_total"`
}

// listAgentRuns exposes a read-only, paginated global view of the agent_runs
// ledger so the workspace overview can surface sub-agents without iterating
// every task. It joins the owning task title and never returns input snapshots.
func (a *API) listAgentRuns(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, allowed := agentRunStatusSet[status]; !allowed {
			writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_STATUS",
				"status must be one of queued, running, succeeded, failed, cancelled, interrupted")
			return
		}
	}
	outputDeliveryStatus := strings.TrimSpace(c.Query("output_delivery_status"))
	if outputDeliveryStatus != "" {
		if _, allowed := agentRunOutputDeliveryStatusSet[outputDeliveryStatus]; !allowed {
			writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_OUTPUT_DELIVERY_STATUS",
				"output_delivery_status must be one of not_ready, pending, submitted, retained")
			return
		}
	}
	attentionOnly := false
	if raw := strings.TrimSpace(c.Query("attention")); raw != "" {
		if raw != "1" {
			writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_ATTENTION_FILTER",
				"attention must be 1 when provided")
			return
		}
		attentionOnly = true
	}
	planLink := strings.TrimSpace(c.Query("plan_link"))
	if planLink != "" && planLink != "unplanned" {
		writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_PLAN_LINK_FILTER",
			"plan_link must be unplanned when provided")
		return
	}
	var verifiedRestartSourcesJSON string
	build := func(tx *gorm.DB) *gorm.DB {
		query := tx.Table("agent_runs AS run").
			Joins("JOIN tasks AS task ON task.id = run.task_id")
		if status != "" {
			query = query.Where("run.status = ?", status)
		}
		if outputDeliveryStatus != "" {
			query = query.Where("run.output_delivery_status = ?", outputDeliveryStatus)
		}
		if attentionOnly {
			query = query.Where(`(
				run.status IN ('queued','running','failed','interrupted') OR
				run.output_delivery_status IN ('pending','retained')
			)`)
			// A direct exact retry is the actionable attempt. Keep the failed
			// parent in the unfiltered ledger, but do not count it again in the
			// attention queue. A separate restart has no parent_run_id and
			// is handled below only after validating its durable proof.
			query = query.Where(`NOT (
				run.status IN ('failed','interrupted') AND EXISTS (
					SELECT 1 FROM agent_runs AS retry
					WHERE retry.parent_run_id = run.id
					  AND retry.task_id = run.task_id
					  AND retry.attempt = run.attempt + 1
				)
			)`)
			if verifiedRestartSourcesJSON != "" {
				query = query.Where("run.id NOT IN (SELECT value FROM json_each(?))", verifiedRestartSourcesJSON)
			}
		}
		if planLink == "unplanned" {
			// A Run belongs to the plan queue only when a proposal that produced
			// it is still referenced by the latest, not user-closed revision for
			// that proposal's session. Older or closed revisions do not hide an
			// independently actionable Run after the plan has moved on.
			query = query.Where(`NOT EXISTS (
				SELECT 1
				FROM ai_action_proposals AS proposal
				JOIN ai_generations AS generation ON generation.id = proposal.generation_id
				JOIN ai_work_plan_revisions AS revision ON revision.session_id = generation.session_id
				JOIN (
					SELECT session_id, MAX(version) AS version
					FROM ai_work_plan_revisions
					GROUP BY session_id
				) AS latest ON latest.session_id = revision.session_id AND latest.version = revision.version
				JOIN json_each(revision.plan_json, '$.steps') AS step
				WHERE proposal.result_id = run.id
				  AND json_extract(step.value, '$.proposal_id') = proposal.id
				  AND NOT ` + aiWorkPlanRevisionClosedFilter + `
			)`)
		}
		return query
	}
	// Scan leaves the slice nil when nothing matches; the clients need [].
	items := make([]agentRunSummaryResponse, 0)
	var total int64
	var counts struct {
		ActiveTotal          int64
		PendingDeliveryTotal int64
		SucceededTotal       int64
	}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if attentionOnly {
			sources, err := verifiedAgentRunRestartSupersededIDs(tx)
			if err != nil {
				return err
			}
			if len(sources) > 0 {
				encoded, err := json.Marshal(sources)
				if err != nil {
					return err
				}
				verifiedRestartSourcesJSON = string(encoded)
			}
		}
		// These counters are intentionally global and ignore the list filters.
		// `total` below is the count matching all requested list filters.
		// Reading both counters and the page in this transaction gives clients
		// one authoritative snapshot for polling and recovery discovery.
		if err := tx.Table("agent_runs").Select(`
			COALESCE(SUM(CASE WHEN status IN ('queued','running') THEN 1 ELSE 0 END), 0) AS active_total,
			COALESCE(SUM(CASE WHEN output_delivery_status = 'pending' THEN 1 ELSE 0 END), 0) AS pending_delivery_total,
			COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END), 0) AS succeeded_total`).
			Scan(&counts).Error; err != nil {
			return err
		}
		if err := build(tx).Count(&total).Error; err != nil {
			return err
		}
		if err := build(tx).
			Select(`run.id, run.task_id, run.assignment_id, run.actor_id, run.adapter_id,
				run.created_by_actor_id, run.parent_run_id, run.attempt, run.status,
				run.provider_id, run.model, run.result_bytes, run.error_code,
				run.output_delivery_status, run.output_delivery_error_code,
				run.submission_id, run.artifact_id, run.started_at, run.completed_at,
				run.created_at, task.title AS task_title`).
			Order("run.created_at DESC").Order("run.id DESC").
			Offset((page - 1) * pageSize).Limit(pageSize).
			Scan(&items).Error; err != nil {
			return err
		}
		ids := make([]string, len(items))
		for index := range items {
			ids[index] = items[index].ID
			source, err := loadAgentRunRestartProof(tx, models.AgentRun{ID: items[index].ID})
			if err != nil {
				return err
			}
			if source != nil {
				id := source.RunID
				items[index].RestartOfRunID = &id
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
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	for index := range items {
		items[index].Progress = a.agentRunProgressFor(items[index].ID, items[index].Status)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": agentRunListMeta{
			Page: page, PageSize: pageSize, Total: total,
			ActiveTotal: counts.ActiveTotal, PendingDeliveryTotal: counts.PendingDeliveryTotal,
			SucceededTotal: counts.SucceededTotal,
		},
	})
}

func (a *API) getAgentRun(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	var row models.AgentRun
	// The files of a v7 Run are the ones a person confirmed, if anyone has.
	var confirmedFiles []frozenAgentRunFileReference
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&row, "id=?", runID).Error; err != nil {
			return err
		}
		if err := projectAgentRunRestart(tx, &row); err != nil {
			return err
		}
		if err := projectAgentRunStartGate(tx, &row); err != nil {
			return err
		}
		if row.ExecutionContractVersion != agentRunExecutionContractVersionV7 {
			return nil
		}
		snapshot, err := parseAgentRunV7Snapshot(row.InputSnapshotJSON)
		if err != nil {
			return err
		}
		confirmedFiles, err = agentRunBoundInputFiles(tx, row.ID, snapshot)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Agent run not found")
		} else if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	response := agentRunResponseFromModel(row)
	response.Progress = a.agentRunProgressFor(row.ID, row.Status)
	if isAgentRunModelContract(row.ExecutionContractVersion) {
		snapshot, err := parseAgentRunModelSnapshot(row.ExecutionContractVersion, row.InputSnapshotJSON)
		if err != nil {
			writeProjectRequestError(c, agentRunIdentityChangedError())
			return
		}
		response.ModelProtocol = snapshot.ModelProtocol
		response.MaxOutputTokens = &snapshot.MaxOutputTokens
		response.InputFiles = snapshot.Files
		if snapshot.DeferredInputs != nil {
			response.InputFiles = confirmedFiles
		}
		response.ReworkContext = snapshot.Rework
		response.ExecutionProviderConfirmation = &agentRunFileAccessProviderConfirmation{Version: row.ProviderVersion, ConfigVersion: row.ProviderConfigVersion, Kind: snapshot.ProviderKind}
	}
	// Full frozen rework is available only in this explicit local detail read,
	// never in lists, creation receipts, or AI metadata tools.
	if row.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
		if snapshot, err := parseAgentRunV4Snapshot(row.InputSnapshotJSON); err == nil {
			response.ReworkContext = &snapshot.Rework
			response.InputFiles = snapshot.Files
			response.ReworkProviderConfirmation = &agentRunFileAccessProviderConfirmation{Version: row.ProviderVersion, ConfigVersion: row.ProviderConfigVersion, Kind: snapshot.ProviderKind}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}
