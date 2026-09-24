package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiSessionDelegatedRunPlan struct {
	Version      int64  `json:"version"`
	StepID       string `json:"step_id"`
	StepTitle    string `json:"step_title"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

type aiSessionDelegatedRunFacts struct {
	SessionID    string                     `json:"session_id"`
	GenerationID string                     `json:"generation_id"`
	ProposalID   string                     `json:"proposal_id"`
	Action       string                     `json:"action"`
	CreatedAt    string                     `json:"created_at"`
	DecidedAt    string                     `json:"decided_at"`
	Plan         *aiSessionDelegatedRunPlan `json:"plan"`
}

type aiSessionDelegatedRunResponse struct {
	RunID      string                     `json:"run_id"`
	Run        *agentRunSummaryResponse   `json:"run"`
	Delegation aiSessionDelegatedRunFacts `json:"delegation"`
}

type aiSessionDelegatedRunMeta struct {
	Page                 int    `json:"page"`
	PageSize             int    `json:"page_size"`
	Total                int    `json:"total"`
	ActiveTotal          int    `json:"active_total"`
	PendingDeliveryTotal int    `json:"pending_delivery_total"`
	UnavailableTotal     int    `json:"unavailable_total"`
	AsOf                 string `json:"as_of"`
}

type aiSessionDelegatedProposal struct {
	models.AIActionProposal
	SessionID string `gorm:"column:session_id"`
}

// listAISessionDelegatedRuns projects only durable, human-confirmed Agent Run
// receipts owned by one saved AI conversation. It does not expose approval
// bodies, Run inputs, result text, file references or provider configuration.
// parent_run_id remains the exact retry lineage and is never repurposed as a
// conversation/child-agent relationship.
func (a *API) listAISessionDelegatedRuns(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	parsedSessionID, err := uuid.Parse(sessionID)
	if err != nil || parsedSessionID.String() != sessionID {
		writeError(c, http.StatusBadRequest, "INVALID_AI_SESSION_ID", "AI session id must be a canonical UUID")
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	outputDeliveryStatus := strings.TrimSpace(c.Query("output_delivery_status"))
	if outputDeliveryStatus != "" {
		if _, allowed := agentRunOutputDeliveryStatusSet[outputDeliveryStatus]; !allowed {
			writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_OUTPUT_DELIVERY_STATUS",
				"output_delivery_status must be one of not_ready, pending, submitted, retained")
			return
		}

	}

	var items []aiSessionDelegatedRunResponse
	meta := aiSessionDelegatedRunMeta{Page: page, PageSize: pageSize}
	now := a.options.Now()
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Select("id").Take(&session, "id = ?", sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
			}
			return err
		}

		var proposals []aiSessionDelegatedProposal
		if err := tx.Table("ai_action_proposals AS proposal").
			Select("proposal.*, generation.session_id AS session_id").
			Joins("JOIN ai_generations AS generation ON generation.id = proposal.generation_id").
			Where("generation.session_id = ? AND proposal.status = 'confirmed' AND proposal.result_id IS NOT NULL", sessionID).
			Where("json_extract(proposal.action_json, '$.action') IN ?", []string{"agent_run.start", "agent_run.retry"}).
			Order("proposal.created_at DESC").Order("proposal.id DESC").
			Find(&proposals).Error; err != nil {
			return err
		}

		runIDs := make([]string, 0, len(proposals))
		actions := make(map[string]aiWorkspaceAction, len(proposals))
		proposalByRunID := make(map[string]string, len(proposals))
		for _, proposal := range proposals {
			action, err := strictAIWorkPlanAction(proposal.AIActionProposal)
			if err != nil || (action.Action != "agent_run.start" && action.Action != "agent_run.retry") || proposal.ResultID == nil || proposal.DecidedAt == nil {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_INVALID", "AI conversation Agent Run history could not be verified")
			}
			runID, err := uuid.Parse(*proposal.ResultID)
			if err != nil || runID.String() != *proposal.ResultID {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_INVALID", "AI conversation Agent Run history could not be verified")
			}
			if previous := proposalByRunID[*proposal.ResultID]; previous != "" && previous != proposal.ID {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_AMBIGUOUS", "AI conversation has multiple receipts for the same Agent Run")
			}
			proposalByRunID[*proposal.ResultID] = proposal.ID
			actions[proposal.ID] = action
			runIDs = append(runIDs, *proposal.ResultID)
		}

		runsByID := make(map[string]agentRunSummaryResponse, len(runIDs))
		if len(runIDs) > 0 {
			var runs []agentRunSummaryResponse
			if err := tx.Table("agent_runs AS run").
				Select(`run.id, run.task_id, run.assignment_id, run.actor_id, run.adapter_id,
					run.created_by_actor_id, run.parent_run_id, run.attempt, run.status,
					run.provider_id, run.model, run.result_bytes, run.error_code,
					run.output_delivery_status, run.output_delivery_error_code,
					run.submission_id, run.artifact_id, run.started_at, run.completed_at,
					run.created_at, task.title AS task_title`).
				Joins("JOIN tasks AS task ON task.id = run.task_id").
				Where("run.id IN ?", runIDs).Scan(&runs).Error; err != nil {
				return err
			}
			for index := range runs {
				runs[index].Progress = a.agentRunProgressAt(runs[index].ID, runs[index].Status, now)
				if _, duplicate := runsByID[runs[index].ID]; duplicate {
					return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_AMBIGUOUS", "AI conversation Agent Run history could not be verified")
				}
				source, err := loadAgentRunRestartProof(tx, models.AgentRun{ID: runs[index].ID})
				if err != nil {
					return err
				}
				if source != nil {
					id := source.RunID
					runs[index].RestartOfRunID = &id
				}
				runsByID[runs[index].ID] = runs[index]
			}
		}

		planLinks := map[string]*aiSessionDelegatedRunPlan{}
		var latest aiWorkPlanRevision
		planErr := tx.Where("session_id = ?", sessionID).Order("version DESC").Take(&latest).Error
		if planErr == nil {
			plan, err := projectAIWorkPlan(tx, latest, now)
			if err != nil {
				return newProjectRequestError(http.StatusConflict, "AI_WORK_PLAN_INVALID", "Latest AI work plan could not be verified")
			}
			for _, step := range plan.Steps {
				if step.ProposalID == "" {
					continue
				}
				planLinks[step.ProposalID] = &aiSessionDelegatedRunPlan{
					Version: plan.Version, StepID: step.ID, StepTitle: step.Title, SupersededBy: step.SupersededBy,
				}
			}
		} else if !errors.Is(planErr, gorm.ErrRecordNotFound) {
			return planErr
		}

		all := make([]aiSessionDelegatedRunResponse, 0, len(proposals))
		for _, proposal := range proposals {
			runID := *proposal.ResultID
			createdAt, createdErr := time.Parse(time.RFC3339Nano, proposal.CreatedAt)
			decidedAt, decidedErr := time.Parse(time.RFC3339Nano, *proposal.DecidedAt)
			if createdErr != nil || decidedErr != nil {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_INVALID", "AI conversation Agent Run history could not be verified")
			}
			run, available := runsByID[runID]
			if !available {
				meta.UnavailableTotal++
			} else {
				if action := actions[proposal.ID]; action.TaskID == "" || action.TaskID != run.TaskID {
					return newProjectRequestError(http.StatusConflict, "AI_AGENT_RUN_LINK_INVALID", "AI conversation Agent Run history could not be verified")
				}
				if run.Status == "queued" || run.Status == "running" {
					meta.ActiveTotal++
				}
				if run.OutputDeliveryStatus == agentRunOutputPending {
					meta.PendingDeliveryTotal++
				}
			}
			if outputDeliveryStatus != "" && (!available || run.OutputDeliveryStatus != outputDeliveryStatus) {
				continue
			}
			var runResponse *agentRunSummaryResponse
			if available {
				copy := run
				runResponse = &copy
			}
			all = append(all, aiSessionDelegatedRunResponse{
				RunID: runID,
				Run:   runResponse,
				Delegation: aiSessionDelegatedRunFacts{
					SessionID: sessionID, GenerationID: proposal.GenerationID, ProposalID: proposal.ID,
					Action: actions[proposal.ID].Action, CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
					DecidedAt: decidedAt.UTC().Format(time.RFC3339Nano), Plan: planLinks[proposal.ID],
				},
			})
		}
		meta.Total = len(all)
		start := (page - 1) * pageSize
		if start > len(all) {
			start = len(all)
		}
		end := start + pageSize
		if end > len(all) {
			end = len(all)
		}
		items = all[start:end]
		meta.AsOf = now.UTC().Format(time.RFC3339Nano)
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}
