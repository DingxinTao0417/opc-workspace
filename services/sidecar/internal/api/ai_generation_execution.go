package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Options are code-owned, never HTTP/model fields. The coordinator commits its
// claim, immutable generation link and consumed turn inside OnAccept.
type aiGenerationRunOptions struct {
	GenerationID         string
	RequestKey           string
	RequestID            string
	ContinuationID       string
	DelegationProposalID string
	DelegationFollowupID string
	Headless             bool
	OnAccept             func(tx *gorm.DB, generation models.AIGeneration) error
}

type aiGenerationEventSink func(event, payload string) bool

type aiGenerationRunResult struct {
	GenerationID string
	SessionID    string
	Status       string
	ErrorCode    string
	Accepted     bool
}

type aiGenerationRunError struct {
	Status  int
	Code    string
	Message string
}

func (e *aiGenerationRunError) Error() string { return e.Code }

// The winning terminal transition and all terminal side effects commit in one
// transaction. A cancelled/completed/failed generation can never be reopened or
// overwritten, and repeated callers cannot duplicate messages or run steps.
func claimAIGenerationTerminal(tx *gorm.DB, generation models.AIGeneration, updates map[string]any) (bool, error) {
	result := tx.Model(&models.AIGeneration{}).
		Where("id = ? AND session_id = ? AND provider_id = ? AND status IN ('queued','streaming')", generation.ID, generation.SessionID, generation.ProviderID).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	var current models.AIGeneration
	if err := tx.Select("id", "status").Where("id = ? AND session_id = ? AND provider_id = ?", generation.ID, generation.SessionID, generation.ProviderID).First(&current).Error; err != nil {
		return false, err
	}
	if current.Status != "completed" && current.Status != "failed" && current.Status != "cancelled" {
		return false, errors.New("AI generation terminal transition unavailable")
	}
	return false, nil
}

func (a *API) runAIGeneration(ctx context.Context, input chatAIRequest, options aiGenerationRunOptions, sink aiGenerationEventSink) (out aiGenerationRunResult, runErr error) {
	// There is no detached HTTP request or resumable prepared handle: acceptance
	// and execution have exactly one owner and always retain the caller's context.
	if err := ctx.Err(); err != nil {
		return out, err
	}
	headlessOwners := 0
	if options.ContinuationID != "" {
		headlessOwners++
	}
	if options.DelegationProposalID != "" {
		headlessOwners++
	}
	if options.DelegationFollowupID != "" {
		headlessOwners++
	}
	if options.Headless && (headlessOwners != 1 || options.GenerationID == "" || options.OnAccept == nil || strings.TrimSpace(input.SessionID) == "") {
		return out, &aiGenerationRunError{http.StatusConflict, "AI_HEADLESS_OWNER_INVALID", "Headless generation requires exactly one saved, authorized owner"}
	}
	if headlessOwners != 0 && !options.Headless {
		return out, &aiGenerationRunError{http.StatusConflict, "AI_HEADLESS_OWNER_INVALID", "A background owner requires headless execution"}
	}
	if options.Headless && input.Workspace != nil {
		for _, scope := range input.Workspace.Scopes {
			if scope == "workspace_ui" || scope == "workspace_browser" {
				return out, &aiGenerationRunError{http.StatusUnprocessableEntity, "AI_CONTINUATION_SCOPE_INVALID", "Headless generation cannot request local UI navigation"}
			}
		}
	}
	defer func() {
		if !out.Accepted {
			return
		}
		var row models.AIGeneration
		if err := a.db.Where("id = ?", out.GenerationID).First(&row).Error; err != nil {
			if runErr == nil {
				runErr = err
			}
			return
		}
		out.Status, out.SessionID = row.Status, row.SessionID
		if row.ErrorCode != nil {
			out.ErrorCode = *row.ErrorCode
		}
	}()
	message := strings.TrimSpace(input.Message)
	if message == "" || len(message) > modelclient.MaxPromptBytes {
		runErr = &aiGenerationRunError{http.StatusUnprocessableEntity, "AI_MESSAGE_INVALID", "The chat message must be between 1 and 65536 characters"}
		return
	}
	requestKey := strings.TrimSpace(options.RequestKey)
	if requestKey == "" && options.GenerationID != "" {
		// A coordinator-supplied ID must remain replay-safe even if its caller
		// does not also choose a human-visible request key.
		requestKey = "generation:" + options.GenerationID
	}
	if len(requestKey) > 128 {
		runErr = &aiGenerationRunError{http.StatusUnprocessableEntity, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain at most 128 bytes"}
		return
	}
	input.Message = message
	requestBody, _ := json.Marshal(input)
	requestHash := sha256Hex(requestBody)
	// Serialize only acceptance, so concurrent retries observe one committed
	// request identity even when the first response has not sent its meta yet.
	a.aiGenerations.beginMu.Lock()
	a.maintenance.RLock()
	preparing := true
	defer func() {
		if preparing {
			a.aiProviderMu.RUnlock()
			a.maintenance.RUnlock()
			a.aiGenerations.beginMu.Unlock()
		}
	}()
	a.aiProviderMu.RLock()
	if a.restorePending.Load() {
		runErr = &aiGenerationRunError{http.StatusServiceUnavailable, "RESTORE_RESTART_REQUIRED", "A verified restore is pending; restart the application to apply it"}
		return
	}
	if accepted, found, err := a.findAcceptedGeneration(ctx, input, options.GenerationID, requestKey, requestHash); err != nil {
		return out, err
	} else if found {
		return accepted, nil
	}
	if err := a.guardAIContinuationGeneration(a.db.WithContext(ctx), input.SessionID, options); err != nil {
		return out, err
	}
	if err := a.guardAIDelegationGeneration(a.db.WithContext(ctx), input.SessionID, options); err != nil {
		return out, err
	}
	if input.ActionRecheckProposalID != "" {
		if err := validateAIActionRecheckSelection(input.SessionID, input.ActionReceiptGenerationID, input.ActionRecheckProposalID, input.Workspace); err != nil {
			runErr = err
			return
		}
	}
	var selectedActionReceipts string
	if input.ActionReceiptGenerationID != "" && input.ActionRecheckProposalID == "" {
		var receiptErr error
		selectedActionReceipts, receiptErr = a.aiActionReceiptsForGeneration(ctx, input.SessionID, input.ActionReceiptGenerationID)
		if errors.Is(receiptErr, errAIActionReceiptSourceInvalid) {
			runErr = &aiGenerationRunError{http.StatusUnprocessableEntity, "AI_ACTION_RECEIPT_SOURCE_INVALID", "The action receipt source requires a saved session and canonical generation ID"}
			return
		}
		if receiptErr != nil {
			runErr = &aiGenerationRunError{http.StatusConflict, "AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE", "The action receipt source is unavailable in this saved conversation"}
			return
		}
	}
	provider, providerErr := a.loadGenerationProvider(ctx, input.ProviderID)
	if providerErr != nil {
		return out, providerErr
	}
	if err := validateAIWorkspaceGrant(&provider, input.Workspace); err != nil {
		runErr = err
		return
	}
	projectFiles, projectFilesErr := prepareAIProjectFileContext(input, provider, a.options.Now(), options.Headless)
	if projectFilesErr != nil {
		return out, projectFilesErr
	}
	var actionRecheckContext string
	if input.ActionRecheckProposalID != "" {
		var recheckErr error
		actionRecheckContext, selectedActionReceipts, recheckErr = a.aiActionRecheckContext(ctx, input.SessionID, input.ActionReceiptGenerationID, input.ActionRecheckProposalID, input.Workspace)
		if recheckErr != nil {
			runErr = recheckErr
			return
		}
	}
	if input.Workspace != nil {
		if err := validateAIKnowledgeSources(ctx, a.db, input.Workspace.KnowledgeSources); err != nil {
			runErr = err
			return
		}
	}
	var businessContext aiBusinessContextEnvelope
	var businessContextJSON *string
	if input.Context != nil {
		if input.Context.ProviderVersion < 1 || input.Context.ProviderVersion != provider.Version {
			runErr = &aiGenerationRunError{http.StatusConflict, "AI_CONTEXT_PROVIDER_CHANGED", "The selected AI provider changed; preview workspace context again"}
			return
		}
		var contextErr error
		businessContext, _, contextErr = buildAIMessageContext(
			ctx, a.db, input.Context.Sources, input.Context.Knowledge, true,
		)
		if contextErr != nil {
			runErr = contextErr
			return
		}
		businessContext.Provider = &aiBusinessContextProviderSnapshot{
			ID: provider.ID, Name: provider.Name, Kind: provider.Kind, Version: provider.Version,
		}
		businessContextJSON, contextErr = encodeAIBusinessContextSnapshot(businessContext)
		if contextErr != nil {
			runErr = errors.New("AI generation database operation failed")
			return
		}
	}
	// Local providers run keyless on the loopback interface (ADR-005); remote
	// providers read their key from the OS credential store, use it for this
	// request only, and never persist it.
	apiKey := ""
	if provider.Kind != aiProviderKindLocal {
		var keyErr error
		apiKey, keyErr = a.keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID))
		if errors.Is(keyErr, keystore.ErrNotFound) {
			runErr = &aiGenerationRunError{http.StatusConflict, "AI_KEY_UNAVAILABLE", "This provider has no stored API key"}
			return
		}
		if keyErr != nil {
			runErr = &aiGenerationRunError{http.StatusServiceUnavailable, "AI_KEY_STORE_UNAVAILABLE", "The operating system credential store is not available"}
			return
		}
	}
	session, newSession, sessionErr := a.resolveGenerationSession(ctx, input.SessionID, message)
	if sessionErr != nil {
		return out, sessionErr
	}
	if options.Headless && (!session.Persist || newSession) {
		return out, &aiGenerationRunError{http.StatusConflict, "AI_CONTINUATION_INVALID", "A continuation requires an existing saved session"}
	}
	memories := a.confirmedAIMemories()
	_, snapshot, snapshotErr := a.activeAIContextSnapshot(session.ID)
	if snapshotErr != nil {
		runErr = errors.New("AI generation database operation failed")
		return
	}
	promptContext := promptContextFromSnapshot(memories, snapshot)
	// These raw bytes are never persisted in AIMessage/ContextSnapshot or
	// reconstructed from history. Replies may quote them under normal history rules.
	promptContext.ProjectFiles = projectFiles
	promptContext = promptContextWithBusinessContext(promptContext, businessContext)
	if actionRecheckContext != "" {
		promptContext.BusinessContext = append(promptContext.BusinessContext, actionRecheckContext)
	}
	promptContext.SystemPrompt = workspaceSystemPrompt(input.Workspace)
	if input.ActionReceiptGenerationID != "" {
		promptContext.ActionReceipts = selectedActionReceipts
	} else if session.Persist {
		receipts, err := a.aiActionReceipts(ctx, session.ID)
		if err != nil {
			runErr = errors.New("AI generation database operation failed")
			return
		}
		promptContext.ActionReceipts = receipts
	}
	generationID := options.GenerationID
	if generationID == "" {
		generationID = uuid.NewString()
	}
	if parsed, err := uuid.Parse(generationID); err != nil || parsed.String() != generationID {
		return out, &aiGenerationRunError{http.StatusUnprocessableEntity, "INVALID_AI_GENERATION_ID", "AI generation id must be a canonical UUID"}
	}
	if input.Workspace != nil && !session.Persist {
		for _, scope := range input.Workspace.Scopes {
			if scope == "actions" || scope == "agent_execution" || scope == "agent_files" || scope == "agent_project_files" || scope == "finance_actions" || scope == "invoice_actions" || scope == "finance_exports" || scope == "knowledge_actions" {
				runErr = &aiGenerationRunError{422, "AI_ACTION_PERSIST_REQUIRED", "Action proposals require a saved conversation"}
				return
			}
		}
	}
	chatTools, err := a.aiChatToolRegistryWithFiles(session.ID, session.Persist, &provider, input.Workspace, input.ProjectFiles, generationID)
	if err != nil {
		runErr = &aiGenerationRunError{http.StatusInternalServerError, "AI_TOOL_CONFIGURATION_INVALID", "The AI tools could not be configured"}
		return
	}
	promptContext.Tools = chatTools.ModelDefinitions()
	history, historyErr := a.chatHistory(session.ID, message, provider.Protocol, provider.Model, promptContext)
	if errors.Is(historyErr, modelclient.ErrPromptTooLarge) {
		runErr = &aiGenerationRunError{http.StatusUnprocessableEntity, "AI_PROMPT_TOO_LARGE", "The current message and system context exceed the prompt budget"}
		return
	}
	if historyErr != nil {
		runErr = errors.New("AI generation database operation failed")
		return
	}

	streamCtx := ctx
	generationCtx, cancelGeneration := context.WithCancel(streamCtx)
	defer cancelGeneration()
	if !a.aiGenerations.register(generationID, provider.ID, session.ID, cancelGeneration) {
		cancelGeneration()
		runErr = &aiGenerationRunError{http.StatusConflict, "AI_PROVIDER_BUSY", "This provider or session already has an active generation"}
		return
	}
	defer a.aiGenerations.release(generationID)

	generation := models.AIGeneration{
		ID: generationID, SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a),
	}
	if requestKey != "" {
		generation.RequestKey = &requestKey
		generation.RequestHash = &requestHash
	}
	runSteps := &aiRunStepCollector{}
	// Session creation, the durable user turn and generation start form one
	// transaction. The assistant reply is committed separately after the
	// upstream stream reaches a terminal outcome.
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.guardAIContinuationGeneration(tx, session.ID, options); err != nil {
			return err
		}
		if err := a.guardAIDelegationGeneration(tx, session.ID, options); err != nil {
			return err
		}
		if _, err := prepareAIProjectFileContext(input, provider, a.options.Now(), options.Headless); err != nil {
			return err
		}
		// Recheck source availability before any acceptance writes. A session or
		// source generation can be deleted after its bounded receipt was read.
		if input.ActionRecheckProposalID != "" {
			verified, err := buildAIActionRecheckContext(tx, session.ID, input.ActionReceiptGenerationID, input.ActionRecheckProposalID, input.Workspace)
			if err != nil {
				return err
			}
			if verified != actionRecheckContext {
				return aiActionRecheckUnavailable()
			}
		}
		if input.ActionReceiptGenerationID != "" {
			if err := validateAIActionReceiptSource(tx, session.ID, input.ActionReceiptGenerationID); err != nil {
				return err
			}
		}
		if input.Workspace != nil {
			if err := validateAIKnowledgeSources(ctx, tx, input.Workspace.KnowledgeSources); err != nil {
				return err
			}
		}
		if input.Context != nil {
			verified, _, err := buildAIMessageContext(
				ctx, tx, input.Context.Sources, input.Context.Knowledge, true,
			)
			if err != nil {
				return err
			}
			verified.Provider = &aiBusinessContextProviderSnapshot{
				ID: provider.ID, Name: provider.Name, Kind: provider.Kind, Version: provider.Version,
			}
			verifiedJSON, err := encodeAIBusinessContextSnapshot(verified)
			if err != nil {
				return err
			}
			if verifiedJSON == nil || businessContextJSON == nil || *verifiedJSON != *businessContextJSON {
				return &aiBusinessContextRequestError{status: http.StatusConflict, code: "AI_CONTEXT_CHANGED", message: "Selected workspace context changed; preview it again before sending"}
			}
		}
		if newSession {
			if err := tx.Create(session).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&generation).Error; err != nil {
			return err
		}
		if options.OnAccept != nil {
			if err := options.OnAccept(tx, generation); err != nil {
				return err
			}
		}
		var userGenerationID *string
		if options.Headless {
			userGenerationID = &generation.ID
		}
		if session.Persist {
			// A new accepted user message supersedes any pending permission
			// recommendation from this saved conversation. This is not a grant.
			if err := consumeOpenAIWorkspaceAccessRequests(tx, session.ID, nowStamp(a)); err != nil {
				return err
			}
			if err := tx.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: session.ID, Role: "user", Status: "completed",
				Content: message, ContextSnapshot: businessContextJSON, GenerationID: userGenerationID,
				CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a),
			}).Error; err != nil {
				return err
			}
			updates := map[string]any{
				"version": gorm.Expr("version + 1"), "updated_at": nowStamp(a),
			}
			if session.Title == "" || session.Title == defaultAISessionTitle {
				updates["title"] = aiSessionTitleFromMessage(message)
			}
			if err := tx.Model(&models.AISession{}).Where("id = ?", session.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if input.Workspace != nil {
			payload, _ := json.Marshal(map[string]any{"generation_id": generation.ID, "provider_id": provider.ID, "provider_config_version": provider.ConfigVersion, "scopes": input.Workspace.Scopes, "knowledge_sources": input.Workspace.KnowledgeSources})
			if err := tx.Table("workflow_events").Create(map[string]any{
				"id": uuid.NewString(), "aggregate_type": "ai_generation", "aggregate_id": generation.ID,
				"action": "ai_workspace_access_granted", "actor_id": models.BuiltinOwnerActorID,
				"request_id": aiNullableString(options.RequestID), "current_json": string(payload), "created_at": nowStamp(a),
			}).Error; err != nil {
				return err
			}
		}
		return createAIRunRootStep(tx, generation)
	}); err != nil {
		if errors.Is(err, errAIActionReceiptSourceUnavailable) {
			runErr = &aiGenerationRunError{http.StatusConflict, "AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE", "The action receipt source is unavailable in this saved conversation"}
			return
		}
		runErr = err
		return
	}
	out = aiGenerationRunResult{GenerationID: generation.ID, SessionID: session.ID, Accepted: true}
	a.recordAIGenerationEvent("ai_generation_started", generation, options.RequestID)
	a.aiGenerations.setSnapshot(aiGenerationResponse{ID: generationID, SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", Persist: session.Persist, ClientRequestID: generation.RequestKey})
	a.aiProviderMu.RUnlock()
	a.maintenance.RUnlock()
	a.aiGenerations.beginMu.Unlock()
	preparing = false

	writeSSE := func(event, payload string) bool {
		return sink == nil || sink(event, payload)
	}
	terminalMatches := func(status string) bool {
		var current models.AIGeneration
		if err := a.db.Select("id", "status").Where("id = ?", generation.ID).First(&current).Error; err != nil {
			runErr = errors.Join(runErr, err)
			return false
		}
		// A different terminal writer won the CAS. Do not publish stale content
		// or a conflicting SSE terminal; the caller recovers the persisted row.
		return current.Status == status
	}
	meta := aiChatStreamMeta{
		Protocol: provider.Protocol, Generation: generationID, SessionID: session.ID,
		Model: provider.Model, ProviderID: provider.ID, SSEProtocol: aiSSEProtocolVersion,
	}
	metaJSON, _ := json.Marshal(meta)
	if !writeSSE("meta", string(metaJSON)) {
		runErr = a.finalizeCancelledGeneration(generation, session, "", "", runSteps.snapshot())
		return
	}
	publishProgress := func(step harness.RunStep) {
		progress := aiProgressFromStep(runSteps.nextSequence(), step)
		if !a.aiGenerations.setProgress(generationID, progress) {
			return
		}
		payload, _ := json.Marshal(struct {
			GenerationID string        `json:"generation_id"`
			Step         aiRunProgress `json:"step"`
		}{generationID, progress})
		if !writeSSE("progress", string(payload)) {
			cancelGeneration()
		}
	}
	finishStep := func(step harness.RunStep) {
		if runSteps.nextSequence() <= aiMaxDetailedRunSteps+1 {
			publishProgress(step)
		}
		runSteps.add(step)
	}
	// Missing-scope requests never grant access. An interactive turn streams a
	// card; a bounded background turn has no SSE sink and records the request
	// with its completed assistant message for later human review instead.
	accessRequested := false
	var requestedScopes []string
	workspaceAccessCtx := withAIWorkspaceAccessRequestEmitter(generationCtx, func(scopes []string) bool {
		if accessRequested {
			return false
		}
		requestedScopes = append([]string(nil), scopes...)
		a.aiGenerations.setAccessRequest(generationID, requestedScopes)
		if !options.Headless {
			payload, _ := json.Marshal(struct {
				GenerationID string   `json:"generation_id"`
				Scopes       []string `json:"scopes"`
			}{generationID, scopes})
			if !writeSSE("workspace_access_request", string(payload)) {
				cancelGeneration()
				return false
			}
		}
		accessRequested = true
		return true
	})
	// Panel navigation has a separate opt-in workspace_ui grant. Its closed
	// panel enum carries neither local content nor an arbitrary route.
	workspacePanelCtx := withAIWorkspacePanelEmitter(workspaceAccessCtx, func(request aiWorkspacePanelRequest) bool {
		if len(request.Panels) == 1 && request.SplitRatio == nil {
			payload, _ := json.Marshal(struct {
				GenerationID string           `json:"generation_id"`
				Panel        aiWorkspacePanel `json:"panel"`
			}{generationID, request.Panels[0]})
			if !writeSSE("workspace_panel", string(payload)) {
				cancelGeneration()
				return false
			}
			return true
		}
		if len(request.Panels) != 2 {
			return false
		}
		payload, _ := json.Marshal(struct {
			GenerationID string             `json:"generation_id"`
			Panels       []aiWorkspacePanel `json:"panels"`
			SplitRatio   *float64           `json:"split_ratio,omitempty"`
		}{generationID, request.Panels, request.SplitRatio})
		if !writeSSE("workspace_panels", string(payload)) {
			cancelGeneration()
			return false
		}
		return true
	})
	// Browser navigation is intentionally separate from panel navigation. Its
	// input is one validated HTTP(S) address, but the local UI remains the
	// execution point: it will show the address and wait for a user click
	// before opening a new tab. One outstanding request per generation avoids
	// turning a tool loop into a local navigation queue.
	browserNavigationRequested := false
	workspaceBrowserCtx := withAIWorkspaceBrowserNavigationEmitter(workspacePanelCtx, func(address string) bool {
		if browserNavigationRequested {
			return false
		}
		payload, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			URL          string `json:"url"`
		}{generationID, address})
		if !writeSSE("workspace_browser_navigation", string(payload)) {
			cancelGeneration()
			return false
		}
		browserNavigationRequested = true
		return true
	})
	workspaceBrowserActionCtx := withAIWorkspaceBrowserActionEmitter(workspaceBrowserCtx, func(action string) bool {
		if browserNavigationRequested {
			return false
		}
		payload, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			Action       string `json:"action"`
		}{generationID, action})
		if !writeSSE("workspace_browser_action", string(payload)) {
			cancelGeneration()
			return false
		}
		browserNavigationRequested = true
		return true
	})
	// Record navigation accepts neither a model-provided route nor record
	// content. The client derives a fixed local route from the authorized
	// identity and presents its own confirmation before it leaves the
	// conversation. An Agent Run's owner Task ID is server-derived only.
	recordNavigationRequested := false
	workspaceRecordCtx := withAIWorkspaceRecordNavigationEmitter(workspaceBrowserActionCtx, func(request aiWorkspaceRecordNavigationRequest) bool {
		if recordNavigationRequested {
			return false
		}
		payload, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			RecordType   string `json:"record_type"`
			RecordID     string `json:"record_id"`
			TaskID       string `json:"task_id,omitempty"`
			SubmissionID string `json:"submission_id,omitempty"`
			ParentID     string `json:"parent_id,omitempty"`
		}{generationID, request.RecordType, request.RecordID, request.TaskID, request.SubmissionID, request.ParentID})
		if !writeSSE("workspace_record_navigation", string(payload)) {
			cancelGeneration()
			return false
		}
		recordNavigationRequested = true
		return true
	})
	// Plan updates are not a local command. After their transaction commits,
	// stream only a tiny invalidation receipt so the current conversation can
	// refresh its already-authorized plan without waiting for the poll interval.
	// Titles, steps, proposals and workspace bodies remain outside SSE.
	workspacePlanCtx := withAIWorkPlanUpdateEmitter(workspaceRecordCtx, func(update aiWorkPlanUpdate) bool {
		payload, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			Version      int64  `json:"version"`
			StepCount    int    `json:"step_count"`
		}{generationID, update.Version, update.StepCount})
		if !writeSSE("workspace_plan_updated", string(payload)) {
			cancelGeneration()
			return false
		}
		return true
	})

	// ADR-029 adds per-request, explicitly consented business reads. No grant
	// retains the three memory tools; manual desktop tool permissions never flow here.
	harnessClient := a.harnessClient
	if harnessClient == nil {
		harnessClient = harness.NewModelClient(nil)
	}
	fileProposalCtx := context.WithValue(workspacePlanCtx, aiProjectFileProposalEmitterKey{}, aiProjectFileProposalEmitter(func(proposal aiProjectFileProposal) bool {
		if options.Headless || sink == nil {
			return false
		}
		payload, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			aiProjectFileProposal
		}{generationID, proposal})
		if !writeSSE("project_file_proposal", string(payload)) {
			cancelGeneration()
			return false
		}
		return true
	}))
	runResult, streamErr := harness.Run(fileProposalCtx, harnessClient,
		harness.Request{
			Protocol: provider.Protocol, BaseURL: provider.BaseURL, APIKey: apiKey, Model: provider.Model,
			DisableTransientRetries: options.Headless,
			History:                 history, Memories: promptContext.Memories,
			SystemPrompt: promptContext.SystemPrompt,
			Summary:      promptContext.Summary, Facts: promptContext.Facts,
			BusinessContext:  promptContext.BusinessContext,
			KnowledgeContext: promptContext.KnowledgeContext,
			ProjectFiles:     promptContext.ProjectFiles,
			ActionReceipts:   promptContext.ActionReceipts,
		},
		chatTools, nil,
		harness.Callbacks{
			OnStepStart: func(step harness.RunStep) {
				if runSteps.nextSequence() <= aiMaxDetailedRunSteps+1 {
					publishProgress(step)
				}
			},
			OnStep: finishStep,
			OnDelta: func(delta string) {
				a.aiGenerations.appendSnapshot(generationID, delta, "")
				deltaJSON, _ := json.Marshal(struct {
					GenerationID string `json:"generation_id"`
					Text         string `json:"text"`
				}{generationID, delta})
				if !writeSSE("delta", string(deltaJSON)) {
					cancelGeneration()
				}
			},
			OnReasoning: func(reasoning string) {
				a.aiGenerations.appendSnapshot(generationID, "", reasoning)
				reasoningJSON, _ := json.Marshal(struct {
					GenerationID string `json:"generation_id"`
					Text         string `json:"text"`
				}{generationID, reasoning})
				if !writeSSE("reasoning", string(reasoningJSON)) {
					cancelGeneration()
				}
			},
		})

	var completedText string
	if streamErr == nil {
		completedText, streamErr = aiCompletedRunText(runResult)
	}

	switch {
	case streamCtx.Err() != nil || generationCtx.Err() != nil:
		partial := stripAIControlBlocks(runResult.Text)
		runErr = a.finalizeCancelledGeneration(generation, session, partial, runResult.Reasoning, runSteps.snapshot())
		cancelledJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			PartialText  string `json:"partial_text"`
		}{generationID, partial})
		if terminalMatches("cancelled") {
			_ = writeSSE("cancelled", string(cancelledJSON))
		}
	case streamErr != nil:
		code := aiStreamErrorCode(streamErr)
		partial := stripAIControlBlocks(runResult.Text)
		runErr = a.finalizeFailedGeneration(generation, code, runSteps.snapshot(), aiFailedPartial{session: session, text: partial, reasoning: runResult.Reasoning})
		detail := streamErr.Error()
		if len(detail) > 200 {
			detail = detail[:200]
		}
		errorJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			Error        string `json:"error"`
			Detail       string `json:"detail"`
			PartialText  string `json:"partial_text"`
		}{generationID, code, detail, partial})
		if terminalMatches("failed") {
			_ = writeSSE("error", string(errorJSON))
		}
	default:
		runResult.Text = completedText
		citationStartedAt := time.Now().UTC()
		publishProgress(harness.RunStep{Kind: "citation_validation", Status: "running", StartedAt: citationStartedAt})
		knowledgeEvidence, knowledgeRequested := aiRunKnowledgeEvidence(chatTools, businessContext.Knowledge)
		cleanedText, citationsJSON, citation, citationErr := validateAIResponseCitations(runResult.Text, knowledgeEvidence, knowledgeRequested)
		citationCompletedAt := time.Now().UTC()
		citationStatus, citationErrorCode := "succeeded", ""
		if citationErr != nil {
			citationStatus, citationErrorCode = "failed", "AI_CITATION_PERSIST_FAILED"
		}
		citationOutputBytes := 0
		if citationsJSON != nil {
			citationOutputBytes = len(*citationsJSON)
		}
		finishStep(harness.RunStep{
			Kind: "citation_validation", Status: citationStatus,
			StartedAt: citationStartedAt, CompletedAt: citationCompletedAt,
			DurationMS:  citationCompletedAt.Sub(citationStartedAt).Milliseconds(),
			OutputBytes: citationOutputBytes, ErrorCode: citationErrorCode,
		})
		if citationErr != nil {
			runErr = a.finalizeFailedGeneration(generation, "AI_CITATION_PERSIST_FAILED", runSteps.snapshot())
			errorJSON, _ := json.Marshal(struct {
				GenerationID string `json:"generation_id"`
				Error        string `json:"error"`
			}{generationID, "AI_CITATION_PERSIST_FAILED"})
			if terminalMatches("failed") {
				_ = writeSSE("error", string(errorJSON))
			}
			return
		}
		if generationCtx.Err() != nil {
			partial := stripAIControlBlocks(runResult.Text)
			runErr = a.finalizeCancelledGeneration(generation, session, partial, runResult.Reasoning, runSteps.snapshot())
			payload, _ := json.Marshal(map[string]string{"generation_id": generationID, "partial_text": partial})
			if terminalMatches("cancelled") {
				_ = writeSSE("cancelled", string(payload))
			}
			return
		}
		completedAt := nowStamp(a)
		runResult.Text = cleanedText
		if err := a.finalizeCompletedGeneration(generation, session, runResult.Text, runResult.Reasoning, provider, citationsJSON, runSteps.snapshot(), completedAt, requestedScopes); err != nil {
			runErr = a.finalizeFailedGeneration(generation, "AI_MESSAGE_PERSIST_FAILED", runSteps.snapshot())
			errorJSON, _ := json.Marshal(struct {
				GenerationID string `json:"generation_id"`
				Error        string `json:"error"`
			}{generationID, "AI_MESSAGE_PERSIST_FAILED"})
			if terminalMatches("failed") {
				_ = writeSSE("error", string(errorJSON))
			}
			return
		}
		if !terminalMatches("completed") {
			return
		}
		// The durable persistence step records the commit marker, not a
		// measured transaction duration. Publish only after a successful commit.
		committedAt, _ := time.Parse(time.RFC3339Nano, completedAt)
		publishProgress(harness.RunStep{Kind: "persistence", Status: "succeeded", StartedAt: committedAt, CompletedAt: committedAt})
		if !options.Headless {
			a.scheduleAICompaction(session.ID, provider)
		}
		if runResult.Reflections > 0 || runResult.BudgetHandoff || runResult.ContextTrimmedTurns > 0 {
			replacementJSON, _ := json.Marshal(struct {
				GenerationID string `json:"generation_id"`
				Text         string `json:"text"`
				Reasoning    string `json:"reasoning"`
			}{generationID, runResult.Text, runResult.Reasoning})
			if !writeSSE("replace", string(replacementJSON)) {
				return
			}
		}
		doneJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			*aiGenerationCitations
		}{generationID, &aiGenerationCitations{CitationStatus: citation.Status, Citations: citation.Items}})
		_ = writeSSE("done", string(doneJSON))
	}
	return
}

func (a *API) findAcceptedGeneration(ctx context.Context, input chatAIRequest, generationID, requestKey, requestHash string) (aiGenerationRunResult, bool, error) {
	var row models.AIGeneration
	if generationID == "" && requestKey == "" {
		return aiGenerationRunResult{}, false, nil
	}
	query := a.db.WithContext(ctx)
	if generationID != "" && requestKey != "" {
		query = query.Where("id = ? OR request_key = ?", generationID, requestKey)
	} else if generationID != "" {
		query = query.Where("id = ?", generationID)
	} else {
		query = query.Where("request_key = ?", requestKey)
	}
	var rows []models.AIGeneration
	if err := query.Limit(2).Find(&rows).Error; err != nil {
		return aiGenerationRunResult{}, false, err
	}
	if len(rows) == 0 {
		return aiGenerationRunResult{}, false, nil
	}
	row = rows[0]
	if len(rows) != 1 || (generationID != "" && row.ID != generationID) ||
		(requestKey != "" && (row.RequestKey == nil || *row.RequestKey != requestKey)) ||
		row.RequestHash == nil || *row.RequestHash != requestHash ||
		row.ProviderID != strings.TrimSpace(input.ProviderID) ||
		(strings.TrimSpace(input.SessionID) != "" && row.SessionID != strings.TrimSpace(input.SessionID)) {
		return aiGenerationRunResult{}, false, &aiGenerationRunError{http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "This request identity was already used with different chat input"}
	}
	return aiGenerationRunResult{GenerationID: row.ID, SessionID: row.SessionID, Status: row.Status, Accepted: true}, true, nil
}

func (a *API) guardAIContinuationGeneration(tx *gorm.DB, sessionID string, options aiGenerationRunOptions) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	var active struct{ ID string }
	if err := tx.Table("ai_continuations").Select("id").Where("session_id = ? AND status IN ('waiting','running')", sessionID).Scan(&active).Error; err != nil {
		return err
	}
	if options.ContinuationID == "" {
		if active.ID != "" {
			return &aiGenerationRunError{http.StatusConflict, "AI_CONTINUATION_ACTIVE", "Stop automatic continuation before sending another message in this session"}
		}
		return nil
	}
	if active.ID != options.ContinuationID {
		return &aiGenerationRunError{http.StatusConflict, "AI_CONTINUATION_INVALID", "This continuation is not active in the selected conversation"}
	}
	return nil
}

func (a *API) loadGenerationProvider(ctx context.Context, providerID string) (models.AIProvider, error) {
	id := strings.TrimSpace(providerID)
	if _, err := uuid.Parse(id); err != nil {
		return models.AIProvider{}, &aiGenerationRunError{http.StatusUnprocessableEntity, "INVALID_AI_PROVIDER_ID", "AI provider id must be a UUID"}
	}
	row, err := loadAIProvider(a.db.WithContext(ctx), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, &aiGenerationRunError{http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", "AI provider not found"}
	}
	if err != nil {
		return row, err
	}
	if row.Status == "disabled" {
		return row, &aiGenerationRunError{http.StatusConflict, "AI_PROVIDER_DISABLED", "This AI provider is disabled"}
	}
	if row.Status != "ready" {
		return row, &aiGenerationRunError{http.StatusConflict, "AI_PROVIDER_NOT_READY", "Run a successful health check before chatting with this provider"}
	}
	return row, nil
}

func (a *API) resolveGenerationSession(ctx context.Context, sessionID, firstMessage string) (*models.AISession, bool, error) {
	if id := strings.TrimSpace(sessionID); id != "" {
		if _, err := uuid.Parse(id); err != nil {
			return nil, false, &aiGenerationRunError{http.StatusUnprocessableEntity, "INVALID_AI_SESSION_ID", "AI session id must be a UUID"}
		}
		var row models.AISession
		if err := a.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, &aiGenerationRunError{http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found"}
			}
			return nil, false, err
		}
		return &row, false, nil
	}
	now := nowStamp(a)
	return &models.AISession{ID: uuid.NewString(), Title: aiSessionTitleFromMessage(firstMessage), Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}, true, nil
}
