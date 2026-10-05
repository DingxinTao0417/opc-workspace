package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func (a *API) decideAIWorkspaceAction(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "AI_ACTION_ID_INVALID", "Invalid proposal ID")
		return
	}
	var input struct {
		ConfirmTaskOutput        *bool  `json:"confirm_task_output"`
		ConfirmTaskDelete        *bool  `json:"confirm_task_delete"`
		ConfirmProjectDelete     *bool  `json:"confirm_project_delete"`
		ConfirmClientDelete      *bool  `json:"confirm_client_delete"`
		ConfirmContentItemDelete *bool  `json:"confirm_content_item_delete"`
		ConfirmContentPublished  *bool  `json:"confirm_content_published"`
		ConfirmRoadmapDelete     *bool  `json:"confirm_roadmap_milestone_delete"`
		ConfirmTagDelete         *bool  `json:"confirm_tag_delete"`
		ConfirmInvoicePDF        *bool  `json:"confirm_invoice_pdf"`
		ConfirmFinanceExport     *bool  `json:"confirm_finance_export"`
		ConfirmInvoiceDelete     *bool  `json:"confirm_invoice_delete"`
		ConfirmInvoiceEffects    *bool  `json:"confirm_invoice_effects"`
		ConfirmFinancialEffects  *bool  `json:"confirm_financial_effects"`
		Fingerprint              string `json:"fingerprint"`
		Decision                 string `json:"decision"`
		ConfirmIncompleteTasks   *bool  `json:"confirm_incomplete_tasks"`
		ConfirmForceResolve      *bool  `json:"confirm_force_resolve"`
		ConfirmFocusStart        *bool  `json:"confirm_focus_start"`
		ConfirmFocusGap          *bool  `json:"confirm_focus_gap"`
		ConfirmFollowupCompleted *bool  `json:"confirm_followup_completed"`
		ConfirmProjectNoteDelete *bool  `json:"confirm_project_note_delete"`
		ConfirmActivityDelete    *bool  `json:"confirm_activity_delete"`
		ConfirmAutomationEffects *bool  `json:"confirm_automation_effects"`
		ConfirmAutomationRetry   *bool  `json:"confirm_automation_retry"`
		ConfirmAgentExecution    *bool  `json:"confirm_agent_execution"`
		ConfirmAgentDelegation   *bool  `json:"confirm_agent_delegation"`
		ConfirmAgentCancel       *bool  `json:"confirm_agent_cancel"`
		ConfirmAgentFiles        *bool  `json:"confirm_agent_files"`
		ConfirmAgentProjectFiles *bool  `json:"confirm_agent_project_files"`
		ConfirmAgentRework       *bool  `json:"confirm_agent_rework"`
		ConfirmAgentRestart      *bool  `json:"confirm_agent_restart"`
		ConfirmAgentPredecessor  *bool  `json:"confirm_agent_predecessor_input"`
		ConfirmAgentRecovery     *bool  `json:"confirm_agent_output_recovery"`
		ConfirmKnowledgeDelete   *bool  `json:"confirm_knowledge_source_delete"`
		ConfirmTaskViewDelete    *bool  `json:"confirm_task_view_delete"`
	}
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, 400, "INVALID_JSON", "Invalid decision request")
		return
	}
	if len(input.Fingerprint) != 64 || (input.Decision != "confirm" && input.Decision != "reject") {
		writeError(c, 422, "AI_ACTION_DECISION_INVALID", "Confirm or reject the exact displayed proposal")
		return
	}
	var out aiActionResponse
	completedProject := false
	overdueInvoice := false
	var movedInvoicePDF *trashedInvoicePDF
	var movedTaskArtifactFiles []filestore.TrashedArtifactFile
	var movedProjectAttachmentFiles []filestore.TrashedArtifactFile
	var movedClientAttachmentFiles []filestore.TrashedArtifactFile
	deletedProjectID := ""
	deletedClientID := ""
	var generatedInvoicePDF *invoicePDFChange
	launchAgentRunID := ""
	var launchAgentDelegation *aiAgentDelegationLaunch
	var delegationReservation *aiDelegationReservation
	cancelAgentRunIDs := []string{}
	queuedKnowledgeJobID := ""
	decisionChanged := false
	recoveryFiles := agentRunOutputFileDelivery{api: a, store: a.artifactStore}
	defer func() {
		if delegationReservation != nil && a.aiDelegations != nil {
			a.aiDelegations.releaseReservation(delegationReservation)
		}
	}()
	decisionTransaction := func(operation func(*gorm.DB) error) error {
		return a.db.WithContext(c.Request.Context()).Transaction(operation)
	}
	// Action JSON is immutable. Read it before the transaction to preserve the
	// same store -> DB lock order used by native PDF writes/deletes and backups.
	// Output recovery also needs the pinned-connection commit boundary before
	// its file compensation can safely inspect durable Artifact references.
	if input.Decision == "confirm" {
		var candidate models.AIActionProposal
		if err := a.db.WithContext(c.Request.Context()).Select("action_json").Where("id=?", id).Take(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(c, 404, "AI_ACTION_NOT_FOUND", "Proposal not found")
			} else {
				writeDatabaseError(c)
			}
			return
		}
		var action aiWorkspaceAction
		if json.Unmarshal([]byte(candidate.ActionJSON), &action) == nil && a.invoicePDFStore != nil && (action.Action == "invoice.delete" || action.Action == "invoice.generate_pdf") {
			a.invoicePDFStore.mu.Lock()
			defer a.invoicePDFStore.mu.Unlock()
		}
		if action.Action == aiAgentRunRecoverOutput {
			decisionTransaction = func(operation func(*gorm.DB) error) error {
				return withAgentRunOutputTransaction(a.db.WithContext(c.Request.Context()), operation)
			}
		}
	}
	err := decisionTransaction(func(tx *gorm.DB) error {
		var row models.AIActionProposal
		if err := tx.First(&row, "id=?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "AI_ACTION_NOT_FOUND", "Proposal not found")
			}
			return err
		}
		if row.Fingerprint != input.Fingerprint {
			return newProjectRequestError(409, "AI_ACTION_CHANGED", "Reload the proposal before confirming")
		}
		var generation models.AIGeneration
		if err := tx.First(&generation, "id=?", row.GenerationID).Error; err != nil {
			return err
		}
		var err error
		clock := a.options.Now()
		out, err = aiActionOutputWithFacts(tx, row, generation.Status, clock)
		if err != nil {
			return err
		}
		if input.ConfirmIncompleteTasks != nil && (input.Decision != "confirm" || out.Action.Action != "project.complete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Incomplete-task consent is only valid for confirming project completion")
		}
		if input.ConfirmForceResolve != nil && (input.Decision != "confirm" || out.Action.Action != "inbox.force_resolve") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Force-resolve consent is only valid for confirming an Inbox force-resolve proposal")
		}
		if input.ConfirmFocusStart != nil && (input.Decision != "confirm" || out.Action.Action != "focus.start") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Start consent is only valid for confirming Focus start")
		}
		includesGap := out.Action.Action == "focus.recover" && aiFocusRecoveryAction(out.Action) == "include_gap_resume"
		if input.ConfirmFocusGap != nil && (input.Decision != "confirm" || !includesGap) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Gap consent is only valid for including the recovery gap")
		}
		if input.ConfirmTaskOutput != nil && (input.Decision != "confirm" || !isAITaskOutputAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task output consent only applies to confirming submit/review")
		}
		if input.ConfirmTaskDelete != nil && (input.Decision != "confirm" || out.Action.Action != "task.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task deletion consent only applies to confirming task deletion")
		}
		if input.ConfirmProjectDelete != nil && (input.Decision != "confirm" || out.Action.Action != "project.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Project deletion consent only applies to confirming project deletion")
		}
		if input.ConfirmClientDelete != nil && (input.Decision != "confirm" || out.Action.Action != "client.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Client deletion consent only applies to confirming client deletion")
		}
		if input.ConfirmContentItemDelete != nil && (input.Decision != "confirm" || out.Action.Action != "content_item.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Content Item deletion consent only applies to confirming Content Item deletion")
		}
		if input.ConfirmContentPublished != nil && (input.Decision != "confirm" || out.Action.Action != "content_item.publish") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Publication consent only applies to confirming a content publication fact")
		}
		if input.ConfirmRoadmapDelete != nil && (input.Decision != "confirm" || out.Action.Action != "roadmap_milestone.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Roadmap milestone deletion consent only applies to confirming roadmap milestone deletion")
		}
		if input.ConfirmTagDelete != nil && (input.Decision != "confirm" || out.Action.Action != "tag.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Tag deletion consent only applies to confirming tag deletion")
		}
		if input.ConfirmAgentExecution != nil && (input.Decision != "confirm" || !isAIAgentRunAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent execution consent only applies to confirming an Agent run start or retry")
		}
		if input.ConfirmAgentDelegation != nil && (input.Decision != "confirm" || (out.Action.Action != aiAgentDelegateSpawnAction && out.Action.Action != aiAgentDelegateFollowupAction)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent delegation consent only applies to confirming a child instruction")
		}
		if input.ConfirmAgentCancel != nil && (input.Decision != "confirm" || !isAIAgentRunCancelAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent cancellation consent only applies to confirming cancellation")
		}
		if input.ConfirmAgentRecovery != nil && (input.Decision != "confirm" || out.Action.Action != aiAgentRunRecoverOutput) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Output recovery consent only applies to confirming output recovery")
		}
		if input.ConfirmKnowledgeDelete != nil && (input.Decision != "confirm" || out.Action.Action != aiKnowledgeDeleteAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Knowledge deletion consent only applies to confirming knowledge source deletion")
		}
		if input.ConfirmTaskViewDelete != nil && (input.Decision != "confirm" || out.Action.Action != aiTaskViewDeleteAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task view deletion consent only applies to confirming task view deletion")
		}
		agentHasInputFiles := isAIAgentRunAction(out.Action.Action) && out.Preview.AgentRunStart != nil && len(out.Preview.AgentRunStart.InputFiles) > 0
		agentHasProjectFiles := agentHasInputFiles && agentRunHasProjectTaskFiles(out.Preview.AgentRunStart.InputFiles)
		agentHasRework := isAIAgentRunAction(out.Action.Action) && out.Preview.AgentRunStart != nil && out.Preview.AgentRunStart.ReworkContext != nil
		agentHasRestart := out.Action.Action == "agent_run.start" && out.Preview.AgentRunStart != nil && out.Preview.AgentRunStart.Restart != nil
		agentTakesPredecessorInput := out.Action.Action == "agent_run.start" && out.Preview.AgentRunStart != nil &&
			out.Preview.AgentRunStart.StartGate != nil && out.Preview.AgentRunStart.StartGate.InputSlot != nil
		if input.ConfirmAgentPredecessor != nil && (input.Decision != "confirm" || !agentTakesPredecessorInput) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Predecessor input consent only applies to confirming a start that takes the predecessor's accepted files")
		}
		if input.ConfirmAgentRestart != nil && (input.Decision != "confirm" || !agentHasRestart) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Restart consent only applies to confirming a current-facts start with an explicit source Run")
		}
		if input.ConfirmAgentRework != nil && (input.Decision != "confirm" || !agentHasRework) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Rework consent only applies to confirming an Agent run with frozen rework context")
		}
		if input.ConfirmAgentFiles != nil && (input.Decision != "confirm" || !agentHasInputFiles) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent file consent only applies to confirming an Agent run with frozen controlled inputs")
		}
		if input.ConfirmAgentProjectFiles != nil && (input.Decision != "confirm" || !agentHasProjectFiles) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Project Task file consent only applies to confirming selected accepted predecessor files")
		}
		wanted := "confirmed"
		if input.ConfirmFinanceExport != nil && (input.Decision != "confirm" || out.Action.Action != "finance.export_csv") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Export consent only applies to CSV export")
		}
		financialAction := strings.HasPrefix(out.Action.Action, "financial_entry.")
		invoiceAction := strings.HasPrefix(out.Action.Action, "invoice.")
		if input.ConfirmInvoicePDF != nil && (input.Decision != "confirm" || out.Action.Action != "invoice.generate_pdf") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "PDF consent only applies to confirming invoice PDF generation")
		}
		if input.ConfirmInvoiceDelete != nil && (input.Decision != "confirm" || out.Action.Action != "invoice.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Invoice deletion consent only applies to confirming invoice deletion")
		}
		if input.ConfirmInvoiceEffects != nil && (input.Decision != "confirm" || !invoiceAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Invoice consent only applies to confirming an invoice command")
		}
		if input.ConfirmFinancialEffects != nil && (input.Decision != "confirm" || !financialAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Financial consent only applies to confirming a ledger change")
		}
		if input.ConfirmAutomationRetry != nil && (input.Decision != "confirm" || out.Action.Action != "automation.retry") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Retry consent only applies to retrying a captured automation run")
		}
		automationConsent := aiAutomationNeedsConsent(out.Action, out.Preview)
		if input.ConfirmAutomationEffects != nil && (input.Decision != "confirm" || !automationConsent) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Automation consent only applies to enabling or updating an enabled rule")
		}
		if input.ConfirmProjectNoteDelete != nil && (input.Decision != "confirm" || out.Action.Action != "project_note.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Note deletion consent only applies to confirming note deletion")
		}
		if input.ConfirmActivityDelete != nil && (input.Decision != "confirm" || out.Action.Action != "client_activity.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Activity deletion consent only applies to deleting an activity")
		}
		if input.ConfirmFollowupCompleted != nil && (input.Decision != "confirm" || out.Action.Action != "client_followup.complete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Completion consent only applies to completing a client followup")
		}
		if input.Decision == "reject" {
			wanted = "rejected"
		}
		if row.Status == wanted {
			if wanted == "confirmed" && out.Action.Action == "agent_run.cancel" && out.AgentRunResult != nil && out.AgentRunResult.Status == "running" {
				cancelAgentRunIDs = append(cancelAgentRunIDs, out.Action.AgentRunID)
			}
			if wanted == "confirmed" && out.Action.Action == aiAgentRunCancelManyAction && out.AgentRunCancelManyResult != nil {
				for _, item := range out.AgentRunCancelManyResult.Items {
					if item.Status == "running" {
						cancelAgentRunIDs = append(cancelAgentRunIDs, item.RunID)
					}
				}
			}
			return nil
		} // Durable identity, independent of HTTP retry keys/TTL.
		if row.Status != "pending" {
			return newProjectRequestError(409, "AI_ACTION_ALREADY_DECIDED", "Proposal already decided")
		}
		if input.Decision == "confirm" && !out.CanConfirm {
			return newProjectRequestError(409, "AI_ACTION_NOT_CONFIRMABLE", "Proposal is unfinished, expired or unavailable")
		}
		now := clock.UTC().Format(time.RFC3339Nano)
		updates := map[string]any{"status": wanted, "decided_at": now}
		if input.Decision == "confirm" {
			if (out.Action.Action == aiAgentDelegateSpawnAction || out.Action.Action == aiAgentDelegateFollowupAction) && (input.ConfirmAgentDelegation == nil || !*input.ConfirmAgentDelegation) {
				return newProjectRequestError(422, "AI_DELEGATION_CONFIRMATION_REQUIRED", "Review the complete child instruction, Provider and delegated scope subset before confirming")
			}
			if isAIAgentRunCancelAction(out.Action.Action) && (input.ConfirmAgentCancel == nil || !*input.ConfirmAgentCancel) {
				return newProjectRequestError(422, "AGENT_CANCEL_CONFIRMATION_REQUIRED", "Explicitly confirm cancelling the displayed Agent run set")
			}
			if out.Action.Action == aiAgentRunRecoverOutput && (input.ConfirmAgentRecovery == nil || !*input.ConfirmAgentRecovery) {
				return newProjectRequestError(422, "AGENT_OUTPUT_RECOVERY_CONFIRMATION_REQUIRED", "Confirm recovery of the stored output without running the model")
			}
			if isAIAgentRunAction(out.Action.Action) && (input.ConfirmAgentExecution == nil || !*input.ConfirmAgentExecution) {
				return newProjectRequestError(422, "AGENT_EXECUTION_CONFIRMATION_REQUIRED", "Explicitly confirm starting this exact frozen Agent run")
			}
			if agentHasInputFiles && (input.ConfirmAgentFiles == nil || !*input.ConfirmAgentFiles) {
				return newProjectRequestError(422, "AGENT_FILE_CONFIRMATION_REQUIRED", "Explicitly confirm sending the frozen controlled files to this exact Provider")
			}
			if agentHasProjectFiles && (input.ConfirmAgentProjectFiles == nil || !*input.ConfirmAgentProjectFiles) {
				return newProjectRequestError(422, "AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED", "Independently confirm sending the selected accepted predecessor Task files to this exact Provider")
			}
			if agentHasRework && (input.ConfirmAgentRework == nil || !*input.ConfirmAgentRework) {
				return newProjectRequestError(422, "AGENT_REWORK_CONFIRMATION_REQUIRED", "Review the full feedback and selected outputs, and explicitly confirm sending them to this exact Provider")
			}
			if agentTakesPredecessorInput && (input.ConfirmAgentPredecessor == nil || !*input.ConfirmAgentPredecessor) {
				return newProjectRequestError(422, "AGENT_PREDECESSOR_INPUT_CONFIRMATION_REQUIRED", "Confirm that this Run will wait for the predecessor's accepted files, which still need a separate confirmation before they are sent")
			}
			if agentHasRestart && (input.ConfirmAgentRestart == nil || !*input.ConfirmAgentRestart) {
				return newProjectRequestError(422, "AGENT_RESTART_CONFIRMATION_REQUIRED", "Review the source Run and complete current execution facts, then explicitly confirm a new execution")
			}
			if isAITaskOutputAction(out.Action.Action) && (input.ConfirmTaskOutput == nil || !*input.ConfirmTaskOutput) {
				return newProjectRequestError(422, "AI_TASK_OUTPUT_CONFIRMATION_REQUIRED", "Review the full evidence, attribution and effects before confirming")
			}
			if out.Action.Action == "task.delete" && (input.ConfirmTaskDelete == nil || !*input.ConfirmTaskDelete) {
				return newProjectRequestError(422, "TASK_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this Task and the displayed local history and files")
			}
			if out.Action.Action == "project.delete" && (input.ConfirmProjectDelete == nil || !*input.ConfirmProjectDelete) {
				return newProjectRequestError(422, "PROJECT_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived Project and the displayed local records and files")
			}
			if out.Action.Action == "client.delete" && (input.ConfirmClientDelete == nil || !*input.ConfirmClientDelete) {
				return newProjectRequestError(422, "CLIENT_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this inactive Client and the displayed local history and files")
			}
			if out.Action.Action == "content_item.delete" && (input.ConfirmContentItemDelete == nil || !*input.ConfirmContentItemDelete) {
				return newProjectRequestError(422, "CONTENT_ITEM_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived Content Item and the displayed local relations")
			}
			if out.Action.Action == "content_item.publish" && (input.ConfirmContentPublished == nil || !*input.ConfirmContentPublished) {
				return newProjectRequestError(422, "CONTENT_PUBLISHED_CONFIRMATION_REQUIRED", "Personally verify external publication before recording it as a local fact")
			}
			if out.Action.Action == "roadmap_milestone.delete" && (input.ConfirmRoadmapDelete == nil || !*input.ConfirmRoadmapDelete) {
				return newProjectRequestError(422, "ROADMAP_MILESTONE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived roadmap milestone and the displayed local relations")
			}
			if out.Action.Action == "tag.delete" && (input.ConfirmTagDelete == nil || !*input.ConfirmTagDelete) {
				return newProjectRequestError(422, "TAG_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this Tag and detaching it from the displayed Tasks")
			}
			if out.Action.Action == "finance.export_csv" && (input.ConfirmFinanceExport == nil || !*input.ConfirmFinanceExport) {
				return newProjectRequestError(422, "FINANCE_EXPORT_CONFIRMATION_REQUIRED", "Verify the export scope and included private fields before approving")
			}
			if out.Action.Action == "invoice.generate_pdf" && (input.ConfirmInvoicePDF == nil || !*input.ConfirmInvoicePDF) {
				return newProjectRequestError(422, "INVOICE_PDF_CONFIRMATION_REQUIRED", "Explicitly confirm generating or replacing the controlled local PDF; no external sending or download")
			}
			if invoiceAction && (input.ConfirmInvoiceEffects == nil || !*input.ConfirmInvoiceEffects) {
				return newProjectRequestError(422, "INVOICE_ACTION_CONFIRMATION_REQUIRED", "Explicitly verify invoice facts and any actual payment and local automation effects")
			}
			if out.Action.Action == "invoice.delete" && (input.ConfirmInvoiceDelete == nil || !*input.ConfirmInvoiceDelete) {
				return newProjectRequestError(422, "INVOICE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this draft and its stored PDF; approval and audit history remain")
			}
			if financialAction && (input.ConfirmFinancialEffects == nil || !*input.ConfirmFinancialEffects) {
				return newProjectRequestError(422, "FINANCIAL_ACTION_CONFIRMATION_REQUIRED", "Explicitly verify the financial facts and their effect on local reports")
			}
			if out.Action.Action == "automation.retry" && (input.ConfirmAutomationRetry == nil || !*input.ConfirmAutomationRetry) {
				return newProjectRequestError(422, "AUTOMATION_RETRY_CONFIRMATION_REQUIRED", "Explicitly confirm retrying the original captured local action")
			}
			if automationConsent && (input.ConfirmAutomationEffects == nil || !*input.ConfirmAutomationEffects) {
				return newProjectRequestError(422, "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED", "Explicitly confirm continuing local automation effects")
			}
			if out.Action.Action == "project_note.delete" && (input.ConfirmProjectNoteDelete == nil || !*input.ConfirmProjectNoteDelete) {
				return newProjectRequestError(422, "PROJECT_NOTE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm soft deletion and retained history")
			}
			if out.Action.Action == "client_activity.delete" && (input.ConfirmActivityDelete == nil || !*input.ConfirmActivityDelete) {
				return newProjectRequestError(422, "CLIENT_ACTIVITY_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm soft deletion and retained history")
			}
			if out.Action.Action == "client_followup.complete" && (input.ConfirmFollowupCompleted == nil || !*input.ConfirmFollowupCompleted) {
				return newProjectRequestError(422, "CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED", "Confirm the actual completion time and reported result")
			}
			if out.Action.Action == aiKnowledgeDeleteAction && (input.ConfirmKnowledgeDelete == nil || !*input.ConfirmKnowledgeDelete) {
				return newProjectRequestError(422, "KNOWLEDGE_SOURCE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm deleting this knowledge source, its index and its managed copy")
			}
			if out.Action.Action == aiTaskViewDeleteAction && (input.ConfirmTaskViewDelete == nil || !*input.ConfirmTaskViewDelete) {
				return newProjectRequestError(422, "TASK_VIEW_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm deleting this saved task view")
			}
			if out.Action.Action == "focus.start" && (input.ConfirmFocusStart == nil || !*input.ConfirmFocusStart) {
				return newProjectRequestError(422, "FOCUS_START_CONFIRMATION_REQUIRED", "Explicitly confirm starting a new local cycle")
			}
			if includesGap && (input.ConfirmFocusGap == nil || !*input.ConfirmFocusGap) {
				return newProjectRequestError(422, "FOCUS_GAP_CONFIRMATION_REQUIRED", "Explicitly confirm including the unknown gap")
			}
			action, err := parseAIWorkspaceAction([]byte(row.ActionJSON))
			if err != nil {
				return err
			}
			var preview aiActionPreview
			if action.Action == aiAgentDelegateSpawnAction {
				preview, err = previewAIAgentDelegationFromStored(tx, action, out.Preview.AgentDelegation)
			} else if action.Action == aiAgentDelegateFollowupAction {
				if out.Preview.AgentFollowup == nil {
					return errors.New("missing follow-up approval preview")
				}
				frozen := out.Preview.AgentFollowup
				grant := &aiWorkspaceGrant{ProviderVersion: frozen.Target.ProviderVersion, Scopes: append([]string(nil), frozen.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), frozen.Knowledge...)}
				preview, err = previewAIAgentFollowup(tx, action, row.GenerationID, frozen.Target.ProviderID, frozen.Target.ConfigVersion, grant, frozen, clock)
			} else if isAIAgentRunAction(action.Action) {
				preview, err = previewAIAgentRunActionFromStored(tx, action, out.Preview.AgentRunStart, a.artifactStore)
			} else {
				preview, err = previewAIWorkspaceAction(tx, action, clock)
			}
			if err != nil {
				return err
			}
			// Related Tasks can change progress without bumping the Inbox version.
			if isAIAgentRunCancelAction(action.Action) || action.Action == aiAgentRunRecoverOutput {
				encoded, marshalErr := json.Marshal(preview)
				if marshalErr != nil {
					return marshalErr
				}
				if string(encoded) != row.PreviewJSON {
					return newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Agent execution changed; request a fresh control proposal")
				}
			}
			// Never approve a newly computed effect instead of the displayed snapshot.
			if action.Action == aiAgentDelegateSpawnAction || action.Action == aiAgentDelegateFollowupAction || action.Action == "task.create" || action.Action == "task.update" || action.Action == "task.delete" || action.Action == aiTaskBatchUpdateAction || action.Action == aiTaskBatchCreateAction || action.Action == aiTaskMoveAction || action.Action == "project.delete" || strings.HasPrefix(action.Action, "tag.") || strings.HasPrefix(action.Action, "project_note.") || strings.HasPrefix(action.Action, "roadmap_milestone.") || strings.HasPrefix(action.Action, "content_item.") || strings.HasPrefix(action.Action, "client.") || strings.HasPrefix(action.Action, "client_contact.") || isAIPersonAction(action.Action) || isAITaskOutputAction(action.Action) || isAIAgentRunAction(action.Action) || isAIAssignmentAction(action.Action) || action.Action == "finance.export_csv" || invoiceAction || financialAction || strings.HasPrefix(action.Action, "automation.") || isAIKnowledgeAction(action.Action) || isAITaskViewAction(action.Action) || strings.HasPrefix(action.Action, "client_activity.") || strings.HasPrefix(action.Action, "client_followup.") || strings.HasPrefix(action.Action, "focus.") || isAIInboxTaskAction(action.Action) || action.Action == "inbox.split" || action.Action == "inbox.force_resolve" || action.Action == "inbox.read_all" {
				matches := false
				if isAIAgentRunAction(action.Action) {
					matches = aiAgentRunPreviewMatchesStored(preview, row.PreviewJSON)
				} else {
					encoded, marshalErr := json.Marshal(preview)
					if marshalErr != nil {
						return marshalErr
					}
					matches = string(encoded) == row.PreviewJSON
				}
				if !matches {
					return newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Related records or Task progress changed; request a fresh proposal")
				}
			}
			var result aiActionResult
			if action.Action == aiAgentDelegateSpawnAction {
				result, launchAgentDelegation, err = executeAIAgentDelegation(tx, row, action, preview.AgentDelegation, clock)
			} else if action.Action == aiAgentDelegateFollowupAction {
				result, launchAgentDelegation, err = executeAIAgentFollowup(tx, row, action, preview.AgentFollowup, clock)
			} else if action.Action == aiAgentRunRecoverOutput {
				_, err = a.recoverAgentRunOutputInTransaction(tx, action.AgentRunID, clock, &recoveryFiles)
				result = aiActionResult{ID: action.AgentRunID, Version: 1}
			} else if action.Action == aiAgentRunCancelManyAction {
				var ids []string
				ids, err = executeAIAgentRunCancelMany(tx, action, requestIDFromContext(c), now)
				if err == nil {
					result = aiActionResult{ID: row.ID, Version: 1}
					cancelAgentRunIDs = append(cancelAgentRunIDs, ids...)
				}
			} else if action.Action == "agent_run.cancel" {
				err = requestAgentRunCancellation(tx, action.AgentRunID, requestIDFromContext(c), now)
				if err == nil {
					result = aiActionResult{ID: action.AgentRunID, Version: 1}
					cancelAgentRunIDs = append(cancelAgentRunIDs, action.AgentRunID)
				}
			} else if isAIAgentRunAction(action.Action) {
				run, executeErr := executeAIAgentRunAction(tx, action, preview.AgentRunStart, a.artifactStore, requestIDFromContext(c), now)
				err = executeErr
				if err == nil {
					result = aiActionResult{ID: run.ID, Version: 1}
					launchAgentRunID = run.ID
				}
			} else if action.Action == "finance.export_csv" {
				// Approval identity/version, not a ledger record or saved file.
				result = aiActionResult{ID: row.ID, Version: 1}
			} else if isAIKnowledgeAction(action.Action) {
				// Indexing is enqueued only after this transaction commits; the
				// durable job row must already exist for the indexer to claim it.
				var knowledgeErr error
				result, queuedKnowledgeJobID, knowledgeErr = executeAIKnowledgeSourceAction(tx, action, now)
				err = knowledgeErr
			} else if isAITaskViewAction(action.Action) {
				result, err = executeAITaskViewAction(tx, action, now)
			} else if action.Action == aiTaskBatchUpdateAction {
				if batchErr := executeAITaskBatchAction(tx, action, requestIDFromContext(c), now); batchErr != nil {
					err = batchErr
				} else {
					// A batch has no single target; the proposal is the durable
					// receipt identity, exactly like the snapshot read-all card.
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == aiTaskBatchCreateAction {
				if batchErr := executeAITaskBatchCreateAction(tx, action, row.ID, requestIDFromContext(c), now); batchErr != nil {
					err = batchErr
				} else {
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == aiTaskMoveAction {
				result, err = executeAITaskMoveAction(tx, action, clock)
			} else if action.Action == aiRoadmapMilestoneMoveAction {
				result, err = executeAIRoadmapMilestoneMove(tx, action, clock)
			} else if action.Action == "inbox.read_all" {
				bulk, executeErr := executeAIInboxReadAllAction(tx, action, requestIDFromContext(c), clock)
				if executeErr != nil {
					err = executeErr
				} else if expected, ok := preview.After["marked_count"].(int64); !ok || expected != bulk.MarkedCount {
					err = newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Inbox snapshot changed; request a fresh proposal")
				} else {
					// Batch actions have no single Inbox entity. The proposal is the
					// durable receipt identity; version 1 is the receipt version.
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == "invoice.generate_pdf" {
				generatedInvoicePDF, err = a.generateAIInvoicePDF(tx, row, action, clock)
				if err == nil {
					result = aiActionResult{ID: generatedInvoicePDF.asset.ID, Version: action.ExpectedVersion}
				}
			} else if action.Action == "invoice.delete" {
				movedInvoicePDF, err = deleteInvoiceInTransaction(tx, a.invoicePDFStore, action.InvoiceID, action.ExpectedVersion, requestIDFromContext(c), now)
				// Hard deletion has no new version; retain the last deleted version
				// as a historical receipt, never an extant entity/version claim.
				result = aiActionResult{ID: action.InvoiceID, Version: action.ExpectedVersion}
			} else if action.Action == "task.delete" {
				_, _, movedTaskArtifactFiles, err = a.deleteTaskInTransaction(tx, action.TaskID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The receipt identifies the deleted Task and its last version. It
				// must never be interpreted as an extant Task/version pair.
				result = aiActionResult{ID: action.TaskID, Version: action.ExpectedVersion}
			} else if action.Action == "project.delete" {
				deletedProjectID = action.ProjectID
				_, _, _, movedProjectAttachmentFiles, err = a.deleteProjectInTransaction(tx, action.ProjectID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ProjectID, Version: action.ExpectedVersion}
			} else if action.Action == "client.delete" {
				deletedClientID = action.ClientID
				_, _, _, movedClientAttachmentFiles, err = a.deleteClientInTransaction(tx, action.ClientID, action.ExpectedVersion, now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ClientID, Version: action.ExpectedVersion}
			} else if action.Action == "content_item.delete" {
				_, _, err = deleteContentItemInTransaction(tx, action.ContentItemID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ContentItemID, Version: action.ExpectedVersion}
			} else if action.Action == "roadmap_milestone.delete" {
				_, _, err = deleteRoadmapMilestoneInTransaction(tx, action.RoadmapMilestoneID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.RoadmapMilestoneID, Version: action.ExpectedVersion}
			} else if action.Action == "tag.delete" {
				_, err = deleteTagInTransaction(tx, action.TagID, action.ExpectedVersion, now)
				result = aiActionResult{ID: action.TagID, Version: action.ExpectedVersion}
			} else {
				result, err = a.executeAIWorkspaceAction(tx, action, requestIDFromContext(c), clock, input.ConfirmIncompleteTasks != nil && *input.ConfirmIncompleteTasks, input.ConfirmForceResolve != nil && *input.ConfirmForceResolve)
			}
			if err != nil {
				return err
			}
			completedProject = action.Action == "project.complete"
			overdueInvoice = action.Action == "invoice.mark_overdue"
			row.ResultID, row.ResultVersion = &result.ID, &result.Version
			out.CreatedTaskIDs = result.CreatedTaskIDs
			updates["result_id"], updates["result_version"] = result.ID, result.Version
		}
		updated := tx.Model(&models.AIActionProposal{}).Where("id=? AND status='pending'", id).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return newProjectRequestError(409, "AI_ACTION_ALREADY_DECIDED", "Proposal already decided")
		}
		row.Status, row.DecidedAt = wanted, &now
		if err := recordAIActionEvent(tx, row, "ai_workspace_action_"+wanted, requestIDFromContext(c), now, out.CreatedTaskIDs...); err != nil {
			return err
		}
		if launchAgentDelegation != nil && launchAgentDelegation.Followup == nil {
			if err := recordAIAgentDelegationLifecycleEvent(tx, launchAgentDelegation.ProposalID, launchAgentDelegation.SessionID, aiDelegationQueuedEvent, requestIDFromContext(c), now); err != nil {
				return err
			}
		}
		out, err = aiActionOutputWithFacts(tx, row, generation.Status, a.options.Now())
		if err == nil {
			if launchAgentDelegation != nil {
				delegationReservation, err = a.reserveAIAgentDelegations([]aiAgentDelegationLaunch{*launchAgentDelegation})
			}
			if err == nil {
				decisionChanged = true
			}
		}
		return err
	})
	a.finishInvoiceDeletion(movedInvoicePDF, err)
	a.finishTaskDeletion(movedTaskArtifactFiles, err)
	a.finishProjectDeletion(deletedProjectID, movedProjectAttachmentFiles, err)
	a.finishClientDeletion(deletedClientID, movedClientAttachmentFiles, err)
	a.finishInvoicePDFChange(generatedInvoicePDF, err)
	err = recoveryFiles.finish(err)
	if err != nil {
		if writeFinancialEntryRequestError(c, err) || writeInvoiceRequestError(c, err) {
			return
		}
		if writeProjectRequestError(c, mapReminderConstraintError(mapInboxTaskConstraintError(mapInboxItemConstraintError(mapTaskWorkflowConstraintError(err))))) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if decisionChanged {
		a.wakeAIContinuationsAfterFactCommit()
	}
	if launchAgentRunID != "" {
		a.launchAgentRun(launchAgentRunID)
	}
	if launchAgentDelegation != nil {
		if code := a.aiDelegations.launchReservedBatch(delegationReservation, []aiAgentDelegationLaunch{*launchAgentDelegation}); code != "" {
			_ = recordAIDelegationLaunchFailure(a.db, *launchAgentDelegation, code, a.options.Now())
		}
	}
	if queuedKnowledgeJobID != "" && !a.knowledgeIndexer.enqueue(context.Background(), queuedKnowledgeJobID) {
		a.failKnowledgeIndexJob(queuedKnowledgeJobID, "KNOWLEDGE_INDEX_QUEUE_UNAVAILABLE")
	}
	for _, runID := range cancelAgentRunIDs {
		if cancel, ok := a.agentRunCancel(runID); ok {
			cancel()
		}
	}
	if completedProject {
		a.consumeAutomationEventDeliveriesBestEffort("project-completed")
	}
	if overdueInvoice {
		a.consumeAutomationEventDeliveriesBestEffort("invoice-overdue")
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}
