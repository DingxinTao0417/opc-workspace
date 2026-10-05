package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The model can only propose. There is deliberately no confirm/execute tool.
// The authenticated human endpoint binds consent to an immutable fingerprint.
type aiWorkspaceAction struct {
	AgentRunID          string          `json:"agent_run_id,omitempty"`
	ProjectNoteID       string          `json:"project_note_id,omitempty"`
	InvoiceID           string          `json:"invoice_id,omitempty"`
	FinancialEntryID    string          `json:"financial_entry_id,omitempty"`
	Action              string          `json:"action"`
	ProjectID           string          `json:"project_id,omitempty"`
	TaskID              string          `json:"task_id,omitempty"`
	TagID               string          `json:"tag_id,omitempty"`
	InboxItemID         string          `json:"inbox_item_id,omitempty"`
	ReminderID          string          `json:"reminder_id,omitempty"`
	FocusSessionID      string          `json:"focus_session_id,omitempty"`
	ClientFollowupID    string          `json:"client_followup_id,omitempty"`
	ClientActivityID    string          `json:"client_activity_id,omitempty"`
	AutomationRuleID    string          `json:"automation_rule_id,omitempty"`
	AutomationRunID     string          `json:"automation_run_id,omitempty"`
	RoadmapMilestoneID  string          `json:"roadmap_milestone_id,omitempty"`
	ContentItemID       string          `json:"content_item_id,omitempty"`
	KnowledgeSourceID   string          `json:"knowledge_source_id,omitempty"`
	KnowledgeIndexJobID string          `json:"knowledge_index_job_id,omitempty"`
	TaskSavedViewID     string          `json:"task_saved_view_id,omitempty"`
	ClientID            string          `json:"client_id,omitempty"`
	ClientActorLinkID   string          `json:"client_actor_link_id,omitempty"`
	ActorID             string          `json:"actor_id,omitempty"`
	ExpectedVersion     int64           `json:"expected_version,omitempty"`
	Changes             json.RawMessage `json:"changes"`
}

type aiActionPreview struct {
	AgentDelegation    *aiAgentDelegationPreview    `json:"agent_delegation,omitempty"`
	AgentFollowup      *aiAgentFollowupPreview      `json:"agent_followup,omitempty"`
	AgentRunRecovery   *aiAgentRunRecoveryPreview   `json:"agent_run_recovery,omitempty"`
	AgentRunRetry      *aiAgentRunRetryPreview      `json:"agent_run_retry,omitempty"`
	AgentRunCancel     *aiAgentRunCancelPreview     `json:"agent_run_cancel,omitempty"`
	AgentRunCancelMany *aiAgentRunCancelManyPreview `json:"agent_run_cancel_many,omitempty"`
	TaskOutput         *aiTaskOutputPreview         `json:"task_output,omitempty"`
	InboxReadAll       *aiInboxReadAllPreview       `json:"inbox_read_all,omitempty"`
	AgentRunStart      *aiAgentRunStartPreview      `json:"agent_run_start,omitempty"`
	Label              string                       `json:"label"`
	Before             map[string]any               `json:"before"`
	After              map[string]any               `json:"after"`
	Tasks              []aiInboxSplitDraft          `json:"tasks,omitempty"`
	NextFollowup       map[string]any               `json:"next_followup,omitempty"`
	AutomationRetry    *aiAutomationRetryPreview    `json:"automation_retry,omitempty"`
	Knowledge          *aiKnowledgeSourcePreview    `json:"knowledge,omitempty"`
	TaskView           *aiTaskViewPreview           `json:"task_view,omitempty"`
	TaskBatch          *aiTaskBatchPreview          `json:"task_batch,omitempty"`
	TaskBatchCreate    *aiTaskBatchCreatePreview    `json:"task_batch_create,omitempty"`
	TaskOrder          *aiTaskOrderPreview          `json:"task_order,omitempty"`
	RoadmapOrder       *aiRoadmapOrderPreview       `json:"roadmap_order,omitempty"`
}

type aiActionResponse struct {
	CreatedTaskIDs           []string                    `json:"created_task_ids,omitempty"`
	ID                       string                      `json:"id"`
	GenerationID             string                      `json:"generation_id"`
	Fingerprint              string                      `json:"fingerprint"`
	Action                   aiWorkspaceAction           `json:"action"`
	Preview                  aiActionPreview             `json:"preview"`
	Status                   string                      `json:"status"`
	CanConfirm               bool                        `json:"can_confirm"`
	ResultID                 *string                     `json:"result_id"`
	ResultVersion            *int64                      `json:"result_version"`
	Route                    string                      `json:"route"`
	CreatedAt                string                      `json:"created_at"`
	DecidedAt                *string                     `json:"decided_at"`
	AutomationRunResult      *aiAutomationRetryResult    `json:"automation_run_result,omitempty"`
	InvoicePDFResult         *aiInvoicePDFResult         `json:"invoice_pdf_result,omitempty"`
	InboxReadAllResult       *readAllInboxItemsOutput    `json:"inbox_read_all_result,omitempty"`
	AgentRunResult           *aiAgentRunResult           `json:"agent_run_result,omitempty"`
	AgentRunCancelManyResult *aiAgentRunCancelManyResult `json:"agent_run_cancel_many_result,omitempty"`
	AgentDelegationResult    *aiAgentDelegationResult    `json:"agent_delegation_result,omitempty"`
	AgentFollowupResult      *aiAgentDelegationResult    `json:"agent_followup_result,omitempty"`
	TaskBatchCreateResult    *aiTaskBatchCreateResult    `json:"task_batch_create_result,omitempty"`
}

func executeAITaskAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (models.Task, error) {
	if isAIAssignmentAction(input.Action) {
		return executeAIAssignmentAction(tx, input, requestID, now)
	}
	if input.Action == "task.create" {
		var create createTaskRequest
		_ = json.Unmarshal(input.Changes, &create)
		task, err := taskFromCreateRequest(create)
		if err != nil {
			return models.Task{}, err
		}
		tagIDs, err := validateAITaskTagIDs(create.TagIDs)
		if err != nil {
			return models.Task{}, err
		}
		return createTaskInTransaction(tx, task, tagIDs, requestID)
	}
	if input.Action == "task.update" {
		var update updateTaskRequest
		_ = json.Unmarshal(input.Changes, &update)
		return updateTaskInTransaction(tx, input.TaskID, input.ExpectedVersion, update, requestID)
	}
	reason, err := aiActionReason(input)
	if err != nil {
		return models.Task{}, err
	}
	result, err := transitionTaskInTransaction(tx, input.TaskID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "task."), reason, requestID, now)
	return result.Task, err
}

func (t *aiWorkspaceTool) propose(ctx context.Context, arguments json.RawMessage) (any, error) {
	agentFileFields := agentRunFileFieldsInArguments(arguments)
	if agentFileFields {
		if err := t.policy.Require("agent_files"); err != nil {
			return nil, err
		}
		if t.agentExecutionRun == nil {
			return nil, errors.New("read controlled file candidates in this saved generation before proposing Agent execution")
		}
	}
	input, err := parseAIWorkspaceAction(arguments)
	if err != nil {
		return nil, err
	}
	if input.Action == aiAgentDelegateFollowupAction && t.name != "workspace_agent_followup" {
		return nil, errors.New("Agent follow-up is not available through generic proposals")
	}
	if err := requireAIWorkspaceActionScopes(t.policy, input); err != nil {
		return nil, err
	}
	if agentRunDeferredInputInArguments(arguments) {
		if err := t.policy.Require("agent_project_files"); err != nil {
			return nil, err
		}
	}
	if agentFileFields {
		var changes aiAgentRunStartChanges
		if err := json.Unmarshal(input.Changes, &changes); err != nil {
			return nil, err
		}
		bindings, err := t.agentExecutionRun.resolveFileCandidates(input.TaskID, changes.InputFileCandidateIDs)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if binding.File.SourceKind == agentexec.FileSourceProjectTaskArtifact {
				if err := t.policy.Require("agent_project_files"); err != nil {
					return nil, err
				}
			}
		}
	}
	encoded, _ := json.Marshal(input)
	fingerprint := sha256Hex(encoded)
	var proposal models.AIActionProposal
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("ai_generations AS g").Joins("JOIN ai_sessions AS s ON s.id=g.session_id").
			Where("g.id=? AND g.status='streaming' AND s.persist=1", t.generationID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return newProjectRequestError(409, "AI_ACTION_CONVERSATION_UNAVAILABLE", "Proposals require an active saved conversation. Do not retry in this run; ask the user to continue in a saved conversation with a newly authorized message")
		}
		err := tx.Where("generation_id=? AND fingerprint=?", t.generationID, fingerprint).Take(&proposal).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Model(&models.AIActionProposal{}).Where("generation_id=?", t.generationID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 8 {
			return newProjectRequestError(409, "AI_ACTION_PROPOSAL_LIMIT", "This reply already contains eight proposals; existing proposals are retained, not executed. Stop proposing in this run, ask the user to review them, then continue in a new message with fresh authorization and current facts")
		}
		var preview aiActionPreview
		if input.Action == aiAgentDelegateSpawnAction {
			preview, err = previewAIAgentDelegation(tx, input, t.generationID, t.providerID, t.configVersion, t.delegationGrant, "", false)
		} else if input.Action == aiAgentDelegateFollowupAction {
			preview, err = previewAIAgentFollowup(tx, input, t.generationID, t.providerID, t.configVersion, t.delegationGrant, nil, t.api.options.Now())
		} else if input.Action == "agent_run.retry" {
			if err = requireAIAgentRetryFileScope(tx, t.policy, input); err != nil {
				return err
			}
			preview, err = previewAIAgentRunRetry(tx, input, t.api.artifactStore)
		} else if isAIAgentRunAction(input.Action) && agentFileFields {
			preview, err = previewAIAgentRunActionWithCandidates(tx, input, t.agentExecutionRun, t.api.artifactStore)
		} else {
			preview, err = previewAIWorkspaceAction(tx, input, t.api.options.Now())
		}
		if err != nil {
			return err
		}
		if t.api.invoicePDFStore == nil && (input.Action == "invoice.generate_pdf" || (input.Action == "invoice.delete" && preview.Before["pdf_asset_id"] != nil)) {
			return newInvoiceRequestError(503, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		}
		previewJSON, _ := json.Marshal(preview)
		maxPreviewBytes := 24 << 10
		if input.Action == aiAgentDelegateSpawnAction || input.Action == aiAgentDelegateFollowupAction || strings.HasPrefix(input.Action, "project_note.") || isAITaskOutputAction(input.Action) || isAIAgentRunAction(input.Action) || input.Action == aiTaskBatchCreateAction || strings.HasPrefix(input.Action, "client_activity.") || strings.HasPrefix(input.Action, "financial_entry.") || strings.HasPrefix(input.Action, "invoice.") {
			maxPreviewBytes = 128 << 10
		}
		if len(previewJSON) > maxPreviewBytes {
			if isAITaskOutputAction(input.Action) {
				return newProjectRequestError(422, "AI_OUTPUT_PREVIEW_TOO_LARGE", "Complete evidence exceeds preview capacity; use Task details rather than truncating")
			}
			return newProjectRequestError(422, "AI_ACTION_PREVIEW_TOO_LARGE", "The complete approval preview exceeds capacity. No proposal was saved. Reduce the scope or use the workbench; do not truncate required evidence or retry unchanged")
		}
		proposal = models.AIActionProposal{ID: uuid.NewString(), GenerationID: t.generationID, Fingerprint: fingerprint,
			ActionJSON: string(encoded), PreviewJSON: string(previewJSON), Status: "pending", CreatedAt: nowStamp(t.api)}
		if err := tx.Create(&proposal).Error; err != nil {
			return err
		}
		return recordAIActionEvent(tx, proposal, "ai_workspace_action_proposed", "", nowStamp(t.api))
	})
	if err != nil {
		var invoice *invoiceRequestError
		if errors.As(err, &invoice) {
			return nil, errors.New(invoice.code + ": " + invoice.message)
		}
		var financial *financialEntryRequestError
		if errors.As(err, &financial) {
			return nil, errors.New(financial.code + ": " + financial.message)
		}
		var domain *projectRequestError
		if errors.As(err, &domain) {
			return nil, errors.New(domain.code + ": " + domain.message)
		}
		// No raw SQL/schema errors leave the tool boundary.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("proposal unavailable: verify target/version, saved conversation and eight-proposal limit")
	}
	return map[string]any{"proposal_id": proposal.ID, "status": proposal.Status, "action": input.Action,
		"instruction": "Proposal saved, NOT executed. Tell the user to review the separate confirmation card. Do not emit opc:task blocks or claim success."}, nil
}

func recordAIActionEvent(tx *gorm.DB, row models.AIActionProposal, action, requestID, now string, createdTaskIDs ...string) error {
	fields := map[string]any{"generation_id": row.GenerationID, "proposal_id": row.ID, "fingerprint": row.Fingerprint, "result_id": row.ResultID, "result_version": row.ResultVersion}
	if len(createdTaskIDs) > 0 {
		fields["created_task_ids"] = createdTaskIDs
	}
	payload, _ := json.Marshal(fields)
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_action_proposal", "aggregate_id": row.ID, "action": action,
		"actor_id": models.BuiltinOwnerActorID, "request_id": aiNullableString(requestID), "current_json": string(payload), "created_at": now,
	}).Error
}

func aiActionOutput(row models.AIActionProposal, generationStatus string, now time.Time) (aiActionResponse, error) {
	var out aiActionResponse
	if json.Unmarshal([]byte(row.ActionJSON), &out.Action) != nil || json.Unmarshal([]byte(row.PreviewJSON), &out.Preview) != nil {
		return out, errors.New("invalid stored proposal")
	}
	out.ID, out.GenerationID, out.Fingerprint, out.Status = row.ID, row.GenerationID, row.Fingerprint, row.Status
	out.ResultID, out.ResultVersion, out.CreatedAt, out.DecidedAt = row.ResultID, row.ResultVersion, row.CreatedAt, row.DecidedAt
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return out, err
	}
	if row.Status == "pending" {
		if generationStatus == "failed" || generationStatus == "cancelled" {
			out.Status = "unavailable"
		} else if now.Sub(created) > 24*time.Hour {
			out.Status = "expired"
		}
	}
	out.CanConfirm = out.Status == "pending" && generationStatus == "completed"
	if out.Action.Action == aiAgentDelegateSpawnAction {
		if out.Preview.AgentDelegation == nil || out.Preview.AgentDelegation.ParentGenerationID != out.GenerationID {
			return out, errors.New("invalid delegated Agent proposal")
		}
		if out.Status == "confirmed" {
			if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || *out.ResultID != out.Preview.AgentDelegation.ChildSessionID {
				return out, errors.New("invalid delegated Agent receipt")
			}
			out.Route = "/ai?session=" + *out.ResultID
		}
		return out, nil
	}
	if out.Action.Action == aiAgentDelegateFollowupAction {
		if out.Preview.AgentFollowup == nil || out.Preview.AgentFollowup.Target.ParentSessionID == "" || out.Preview.AgentFollowup.Target.ChildSessionID == "" {
			return out, errors.New("invalid Agent follow-up proposal")
		}
		if out.Status == "confirmed" {
			if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || *out.ResultID != out.Preview.AgentFollowup.Target.ChildSessionID {
				return out, errors.New("invalid Agent follow-up receipt")
			}
			out.Route = "/ai?session=" + *out.ResultID
		}
		return out, nil
	}
	if out.Action.Action == "inbox.read_all" && out.Status == "confirmed" && out.Preview.InboxReadAll != nil {
		out.InboxReadAllResult = &readAllInboxItemsOutput{
			ThroughCreatedAt: out.Preview.InboxReadAll.ThroughCreatedAt,
			MarkedCount:      out.Preview.InboxReadAll.CandidateCount,
		}
	}
	if out.Action.Action == aiTaskBatchCreateAction && out.Status == "confirmed" {
		if out.ResultID == nil || *out.ResultID != out.ID || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.TaskBatchCreate == nil {
			return out, errors.New("invalid task batch create receipt")
		}
		out.TaskBatchCreateResult = aiTaskBatchCreateReceipt(out.ID, out.Preview.TaskBatchCreate)
	}
	if out.Action.Action == aiAgentRunCancelManyAction && out.Status == "confirmed" {
		if out.ResultID == nil || *out.ResultID != out.ID || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.AgentRunCancelMany == nil {
			return out, errors.New("invalid batch Agent cancellation receipt")
		}
	}
	kind, targetID := aiActionTarget(out.Action)
	if out.Action.Action == aiAgentRunCancelManyAction {
		out.Route = "/ai?workspace=agents"
	} else if out.Action.Action == "agent_run.cancel" || out.Action.Action == aiAgentRunRecoverOutput {
		out.Route = agentRunRoute(out.Action.TaskID, out.Action.AgentRunID)
	} else if isAIAgentRunAction(out.Action.Action) {
		out.Route = agentRunRoute(out.Action.TaskID, out.Action.AgentRunID)
		if sourceID, err := aiActionRestartOfRunID(out.Action); err == nil && sourceID != "" {
			out.Route = agentRunRoute(out.Action.TaskID, sourceID)
		}
		if out.Status == "confirmed" && out.ResultID != nil {
			out.Route = agentRunRoute(out.Action.TaskID, *out.ResultID)
		}
	} else if isAITaskOutputAction(out.Action.Action) {
		out.Route = searchRoute("task", out.Action.TaskID)
		submissionID := ""
		if out.ResultID != nil {
			submissionID = *out.ResultID
		} else if out.Action.Action == "task.review" {
			var changes aiReviewOutputChanges
			if err := json.Unmarshal(out.Action.Changes, &changes); err != nil {
				return out, err
			}
			submissionID = changes.SubmissionID
		}
		if id, err := uuid.Parse(submissionID); err == nil && id.String() == submissionID {
			out.Route = taskSubmissionRoute(out.Action.TaskID, submissionID)
		}
	} else if out.Action.Action == "finance.export_csv" {
		out.Route = ""
	} else if out.Action.Action == "inbox.read_all" {
		out.Route = "/inbox"
	} else if kind == "project_note" {
		projectID, _ := out.Preview.After["project_id"].(string)
		out.Route = searchRoute("project", projectID)
		noteID := targetID
		if out.ResultID != nil {
			noteID = *out.ResultID
		}
		if project, err := uuid.Parse(projectID); err == nil && project.String() == projectID {
			if note, err := uuid.Parse(noteID); err == nil && note.String() == noteID {
				out.Route = aiProjectNoteRoute(projectID, noteID)
			}
		}
	} else if kind == "client_contact" {
		clientID, _ := out.Preview.After["client_id"].(string)
		if id, err := uuid.Parse(clientID); err == nil && id.String() == clientID {
			out.Route = searchRoute("client", clientID)
		}
	} else if kind == "person" {
		// Person records live in the Settings modal, not at a routable detail URL.
		out.Route = ""
	} else if kind == "tag" {
		// Tags are managed from the Task workspace rather than a standalone route.
		out.Route = "/tasks"
	} else if kind == "client_followup" || kind == "client_activity" {
		clientID, _ := out.Preview.After["client_id"].(string)
		if id, err := uuid.Parse(clientID); err == nil && id.String() == clientID {
			out.Route = searchRoute("client", clientID)
			recordID := targetID
			if out.ResultID != nil {
				recordID = *out.ResultID
			}
			if id, err := uuid.Parse(recordID); err == nil && id.String() == recordID {
				out.Route = aiClientRecordRoute(clientID, strings.TrimPrefix(kind, "client_"), recordID)
			}
		}
	} else if kind == "automation_rule" || kind == "automation_run" {
		id := targetID
		if out.ResultID != nil {
			id = *out.ResultID
		}
		out.Route = aiAutomationRoute(strings.TrimPrefix(kind, "automation_"), id)
	} else if isAIKnowledgeAction(out.Action.Action) {
		// The native page only highlights a source whose latest job is the
		// exact failed job in the link. Management receipts describe queued,
		// running, cancelled or deleted states, so they open the library itself
		// instead of claiming a precise location that does not exist.
		out.Route = "/knowledge"
	} else if isAITaskViewAction(out.Action.Action) {
		// Saved views are applied inside the Tasks page filter bar; there is no
		// per-view route, so the receipt only opens the task list.
		out.Route = aiTaskViewRoute()
	} else if out.Action.Action == aiTaskBatchUpdateAction {
		// A batch has no single target route: the card and receipt link to the
		// task list where every affected task is visible.
		out.Route = "/tasks"
	} else if out.Action.Action == aiTaskBatchCreateAction {
		out.Route = searchRoute("project", out.Action.ProjectID)
	} else if kind == "financial_entry" {
		id := targetID
		if out.ResultID != nil {
			id = *out.ResultID
		}
		if id != "" {
			out.Route = "/income/" + id
		}
	} else if out.Action.Action == "project.delete" && out.Status == "confirmed" {
		out.Route = "/projects"
	} else if out.Action.Action == "client.delete" && out.Status == "confirmed" {
		out.Route = "/clients"
	} else if out.Action.Action == "content_item.delete" && out.Status == "confirmed" {
		out.Route = "/content-calendar"
	} else if out.Action.Action == "roadmap_milestone.delete" && out.Status == "confirmed" {
		out.Route = "/roadmap"
	} else if out.Action.Action == "task.delete" && out.Status == "confirmed" {
		out.Route = "/tasks"
	} else if out.Action.Action == "invoice.delete" && out.Status == "confirmed" {
		out.Route = "/invoices"
	} else if out.Action.Action == "invoice.generate_pdf" {
		out.Route = searchRoute("invoice", out.Action.InvoiceID)
	} else if out.ResultID != nil {
		out.Route = searchRoute(kind, *out.ResultID)
	} else if targetID != "" {
		out.Route = searchRoute(kind, targetID)
	}
	return out, nil
}

func (a *API) listAIWorkspaceActions(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "INVALID_AI_GENERATION_ID", "Invalid generation ID")
		return
	}
	var generation models.AIGeneration
	if err := a.db.WithContext(c.Request.Context()).First(&generation, "id=?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, 404, "AI_GENERATION_NOT_FOUND", "Generation not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	rows := []models.AIActionProposal{}
	if err := a.db.WithContext(c.Request.Context()).Where("generation_id=?", id).Order("created_at,id").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	items := []aiActionResponse{}
	for _, row := range rows {
		out, err := aiActionOutputWithFacts(a.db.WithContext(c.Request.Context()), row, generation.Status, a.options.Now())
		if err != nil {
			writeDatabaseError(c)
			return
		}
		items = append(items, out)
	}
	c.JSON(200, gin.H{"data": items})
}
