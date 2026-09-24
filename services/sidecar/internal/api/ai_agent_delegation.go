package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	aiAgentDelegateSpawnAction       = "agent_delegate.spawn"
	aiAgentDelegationMaxCount        = 4
	aiAgentDelegationMaxConcurrent   = 8
	aiAgentDelegationMaxQueued       = 32
	aiAgentDelegationMaxRun          = 10 * time.Minute
	aiDelegationQueueFullCode        = "AI_DELEGATION_QUEUE_FULL"
	aiDelegationQueueUnavailableCode = "AI_DELEGATION_QUEUE_UNAVAILABLE"
	aiDelegationQueuedEvent          = "ai_agent_delegation_queued"
	aiDelegationAcceptedEvent        = "ai_agent_delegation_accepted"
	aiDelegationStopRequestedEvent   = "ai_agent_delegation_stop_requested"
)

func recordAIAgentDelegationLifecycleEvent(tx *gorm.DB, proposalID, childSessionID, action, requestID, stamp string) error {
	payload, err := json.Marshal(struct {
		ProposalID     string `json:"proposal_id"`
		ChildSessionID string `json:"child_session_id"`
	}{ProposalID: proposalID, ChildSessionID: childSessionID})
	if err != nil {
		return err
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_agent_delegation", "aggregate_id": proposalID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": aiNullableString(requestID),
		"current_json": string(payload), "created_at": stamp,
	}).Error
}

func aiAgentDelegationLifecycleEventExists(tx *gorm.DB, proposalID, action string) (bool, error) {
	var count int64
	if err := tx.Table("workflow_events").Where(
		"aggregate_type=? AND aggregate_id=? AND action=?", "ai_agent_delegation", proposalID, action,
	).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

var aiAgentDelegationTaskNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

var aiAgentDelegationScopeOrder = []string{
	"work", "clients", "outputs", "output_files", "actions", "agent_execution",
	"agent_files", "agent_project_files", "knowledge", "knowledge_actions",
	"finance", "finance_actions", "invoice_actions", "finance_exports",
}

type aiAgentDelegationChanges struct {
	TaskName string   `json:"task_name"`
	Message  string   `json:"message"`
	Scopes   []string `json:"scopes"`
}

type aiAgentDelegationPreview struct {
	TaskName           string                   `json:"task_name"`
	Message            string                   `json:"message"`
	Scopes             []string                 `json:"scopes"`
	Knowledge          []aiKnowledgeSourceGrant `json:"knowledge_sources,omitempty"`
	ParentSessionID    string                   `json:"parent_session_id"`
	ParentGenerationID string                   `json:"parent_generation_id"`
	ChildSessionID     string                   `json:"child_session_id"`
	ProviderID         string                   `json:"provider_id"`
	ProviderVersion    int64                    `json:"provider_version"`
	ConfigVersion      int64                    `json:"provider_config_version"`
	ProviderName       string                   `json:"provider_name"`
	Model              string                   `json:"model"`
	LeavesDevice       bool                     `json:"leaves_device"`
	Depth              int                      `json:"depth"`
	MaxDepth           int                      `json:"max_depth"`
	SiblingIndex       int                      `json:"sibling_index"`
	MaxChildren        int                      `json:"max_children"`
}

type aiAgentDelegationResult struct {
	SessionID        string  `json:"session_id"`
	GenerationID     string  `json:"generation_id"`
	Status           string  `json:"status"`
	ErrorCode        *string `json:"error_code"`
	PendingApprovals int     `json:"pending_approvals"`
}

type aiAgentDelegationLaunch struct {
	ProposalID string
	SessionID  string
	ProviderID string
	Message    string
	Grant      aiWorkspaceGrant
	Followup   *aiAgentFollowupTarget
}

func aiAgentDelegationSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"task_name", "message", "scopes"},
		"properties": map[string]any{
			"task_name": map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9_]{0,39}$"},
			"message":   map[string]any{"type": "string", "minLength": 1, "maxLength": 4000},
			"scopes":    map[string]any{"type": "array", "minItems": 1, "maxItems": len(aiAgentDelegationScopeOrder), "uniqueItems": true, "items": map[string]any{"type": "string"}},
		},
	})
	return encoded
}

func normalizeAIAgentDelegationChanges(changes aiAgentDelegationChanges) (aiAgentDelegationChanges, error) {
	changes.TaskName = strings.TrimSpace(changes.TaskName)
	changes.Message = strings.TrimSpace(changes.Message)
	if !aiAgentDelegationTaskNamePattern.MatchString(changes.TaskName) {
		return changes, errors.New("task_name must be lowercase letters, digits or underscores and begin with a letter")
	}
	if changes.Message == "" || !utf8.ValidString(changes.Message) || utf8.RuneCountInString(changes.Message) > 4000 || len(changes.Message) > 16<<10 {
		return changes, errors.New("message must contain 1-4000 Unicode characters and fit the bounded delegation envelope")
	}
	allowed := make(map[string]int, len(aiAgentDelegationScopeOrder))
	for index, scope := range aiAgentDelegationScopeOrder {
		allowed[scope] = index
	}
	if len(changes.Scopes) == 0 || len(changes.Scopes) > len(aiAgentDelegationScopeOrder) {
		return changes, errors.New("select a bounded explicit scope subset")
	}
	seen := map[string]bool{}
	for _, scope := range changes.Scopes {
		if _, ok := allowed[scope]; !ok || seen[scope] {
			return changes, errors.New("delegated scopes must be unique allowed headless capabilities")
		}
		seen[scope] = true
	}
	sort.Slice(changes.Scopes, func(i, j int) bool { return allowed[changes.Scopes[i]] < allowed[changes.Scopes[j]] })
	return changes, nil
}

func parseAIAgentDelegationAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.Action != aiAgentDelegateSpawnAction || len(raw) != 2 || raw["action"] == nil || raw["changes"] == nil {
		return input, errors.New("Agent delegation accepts only action and changes")
	}
	if len(fields) != 3 || fields["task_name"] == nil || fields["message"] == nil || fields["scopes"] == nil {
		return input, errors.New("Agent delegation requires task_name, message and scopes")
	}
	var changes aiAgentDelegationChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return input, err
	}
	normalized, err := normalizeAIAgentDelegationChanges(changes)
	if err != nil {
		return input, err
	}
	input.Changes, _ = json.Marshal(normalized)
	return input, nil
}

func cloneAIWorkspaceGrant(grant *aiWorkspaceGrant) *aiWorkspaceGrant {
	if grant == nil {
		return nil
	}
	copy := &aiWorkspaceGrant{ProviderVersion: grant.ProviderVersion}
	copy.Scopes = append([]string(nil), grant.Scopes...)
	copy.KnowledgeSources = append([]aiKnowledgeSourceGrant(nil), grant.KnowledgeSources...)
	return copy
}

func delegatedAIWorkspaceGrant(parent *aiWorkspaceGrant, scopes []string) (aiWorkspaceGrant, error) {
	if parent == nil {
		return aiWorkspaceGrant{}, errors.New("Agent delegation requires an explicit parent grant")
	}
	parentScopes := map[string]bool{}
	for _, scope := range parent.Scopes {
		parentScopes[scope] = true
	}
	for _, scope := range scopes {
		if !parentScopes[scope] {
			return aiWorkspaceGrant{}, errors.New("delegated scopes must be a subset of the current message grant")
		}
	}
	child := aiWorkspaceGrant{ProviderVersion: parent.ProviderVersion, Scopes: append([]string(nil), scopes...)}
	for _, scope := range child.Scopes {
		if scope == "knowledge" {
			child.KnowledgeSources = append([]aiKnowledgeSourceGrant(nil), parent.KnowledgeSources...)
			break
		}
	}
	return child, nil
}

func isDelegatedAISession(tx *gorm.DB, sessionID string) (bool, error) {
	var count int64
	err := tx.Model(&models.AIActionProposal{}).
		Where("status='confirmed' AND result_id=? AND json_extract(action_json,'$.action')=?", sessionID, aiAgentDelegateSpawnAction).
		Count(&count).Error
	return count > 0, err
}

func countAIAgentDelegations(tx *gorm.DB, parentSessionID string) (int64, error) {
	var count int64
	err := tx.Table("ai_action_proposals AS p").
		Joins("JOIN ai_generations AS g ON g.id=p.generation_id").
		Where("g.session_id=? AND p.status IN ('pending','confirmed') AND json_extract(p.action_json,'$.action')=?", parentSessionID, aiAgentDelegateSpawnAction).
		Count(&count).Error
	return count, err
}

func previewAIAgentDelegation(tx *gorm.DB, input aiWorkspaceAction, parentGenerationID, providerID string, configVersion int64, parentGrant *aiWorkspaceGrant, childSessionID string, existingProposal bool) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var changes aiAgentDelegationChanges
	if err := json.Unmarshal(input.Changes, &changes); err != nil {
		return preview, err
	}
	var generation models.AIGeneration
	if err := tx.Select("id,session_id,provider_id,status").Take(&generation, "id=?", parentGenerationID).Error; err != nil {
		return preview, err
	}
	expectedStatus := "streaming"
	if existingProposal {
		expectedStatus = "completed"
	}
	if generation.ProviderID != providerID || generation.Status != expectedStatus {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PARENT_CHANGED", "The parent generation is no longer active")
	}
	var parent models.AISession
	if err := tx.Take(&parent, "id=?", generation.SessionID).Error; err != nil {
		return preview, err
	}
	if !parent.Persist {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PARENT_INVALID", "Delegation requires a saved parent conversation")
	}
	delegated, err := isDelegatedAISession(tx, parent.ID)
	if err != nil {
		return preview, err
	}
	if delegated {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_DEPTH_LIMIT", "A delegated child cannot create another child")
	}
	count, err := countAIAgentDelegations(tx, parent.ID)
	if err != nil {
		return preview, err
	}
	if (!existingProposal && count >= aiAgentDelegationMaxCount) || (existingProposal && count > aiAgentDelegationMaxCount) {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_LIMIT", "This parent conversation already has four active or confirmed child delegations")
	}
	var provider models.AIProvider
	if err := tx.Take(&provider, "id=?", providerID).Error; err != nil {
		return preview, err
	}
	if provider.Status != "ready" || provider.ConfigVersion != configVersion || parentGrant == nil || provider.Version != parentGrant.ProviderVersion {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PROVIDER_CHANGED", "The selected Provider changed; authorize a fresh delegation")
	}
	childGrant, err := delegatedAIWorkspaceGrant(parentGrant, changes.Scopes)
	if err != nil {
		return preview, newProjectRequestError(http.StatusUnprocessableEntity, "AI_DELEGATION_SCOPE_INVALID", err.Error())
	}
	if err := validateAIWorkspaceGrant(&provider, &childGrant); err != nil {
		return preview, err
	}
	if childSessionID == "" {
		childSessionID = uuid.NewString()
	}
	if parsed, err := uuid.Parse(childSessionID); err != nil || parsed.String() != childSessionID {
		return preview, errors.New("invalid delegated child session identity")
	}
	var existing int64
	if err := tx.Model(&models.AISession{}).Where("id=?", childSessionID).Count(&existing).Error; err != nil {
		return preview, err
	}
	if existing != 0 {
		return preview, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_CHILD_EXISTS", "The delegated child identity is already in use")
	}
	preview.Label = changes.TaskName
	preview.AgentDelegation = &aiAgentDelegationPreview{
		TaskName: changes.TaskName, Message: changes.Message, Scopes: childGrant.Scopes,
		Knowledge: childGrant.KnowledgeSources, ParentSessionID: parent.ID, ParentGenerationID: generation.ID,
		ChildSessionID: childSessionID, ProviderID: provider.ID, ProviderVersion: provider.Version,
		ConfigVersion: provider.ConfigVersion, ProviderName: provider.Name, Model: provider.Model,
		LeavesDevice: provider.Kind != aiProviderKindLocal, Depth: 1, MaxDepth: 1,
		SiblingIndex: int(count) + 1, MaxChildren: aiAgentDelegationMaxCount,
	}
	return preview, nil
}

func previewAIAgentDelegationFromStored(tx *gorm.DB, input aiWorkspaceAction, stored *aiAgentDelegationPreview) (aiActionPreview, error) {
	if stored == nil {
		return aiActionPreview{}, errors.New("missing delegated Agent preview")
	}
	grant := &aiWorkspaceGrant{ProviderVersion: stored.ProviderVersion, Scopes: append([]string(nil), stored.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), stored.Knowledge...)}
	preview, err := previewAIAgentDelegation(tx, input, stored.ParentGenerationID, stored.ProviderID, stored.ConfigVersion, grant, stored.ChildSessionID, true)
	if err == nil && preview.AgentDelegation != nil {
		preview.AgentDelegation.SiblingIndex = stored.SiblingIndex
	}
	return preview, err
}

func executeAIAgentDelegation(tx *gorm.DB, proposal models.AIActionProposal, action aiWorkspaceAction, preview *aiAgentDelegationPreview, now time.Time) (aiActionResult, *aiAgentDelegationLaunch, error) {
	if preview == nil || proposal.GenerationID != preview.ParentGenerationID {
		return aiActionResult{}, nil, errors.New("invalid delegated Agent preview")
	}
	var changes aiAgentDelegationChanges
	if err := json.Unmarshal(action.Changes, &changes); err != nil {
		return aiActionResult{}, nil, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	title := "Agent · " + changes.TaskName
	if utf8.RuneCountInString(title) > 200 {
		return aiActionResult{}, nil, errors.New("delegated Agent title is too long")
	}
	child := models.AISession{ID: preview.ChildSessionID, Title: title, Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := tx.Create(&child).Error; err != nil {
		return aiActionResult{}, nil, err
	}
	launch := &aiAgentDelegationLaunch{
		ProposalID: proposal.ID, SessionID: child.ID, ProviderID: preview.ProviderID, Message: changes.Message,
		Grant: aiWorkspaceGrant{ProviderVersion: preview.ProviderVersion, Scopes: append([]string(nil), preview.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), preview.Knowledge...)},
	}
	return aiActionResult{ID: child.ID, Version: 1}, launch, nil
}

func readAIAgentDelegationResult(tx *gorm.DB, out aiActionResponse, now time.Time) (*aiAgentDelegationResult, error) {
	if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.AgentDelegation == nil || *out.ResultID != out.Preview.AgentDelegation.ChildSessionID {
		return nil, errors.New("invalid delegated Agent receipt")
	}
	var session models.AISession
	if err := tx.Select("id,persist,version").Take(&session, "id=?", *out.ResultID).Error; err != nil {
		return nil, err
	}
	if !session.Persist || session.Version < 1 {
		return nil, errors.New("invalid delegated Agent session")
	}
	var generation models.AIGeneration
	err := tx.Select("id,session_id,provider_id,status,error_code").Take(&generation, "id=?", out.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &aiAgentDelegationResult{SessionID: session.ID, GenerationID: out.ID, Status: "queued"}, nil
	}
	if err != nil {
		return nil, err
	}
	if generation.SessionID != session.ID || generation.ProviderID != out.Preview.AgentDelegation.ProviderID {
		return nil, errors.New("inconsistent delegated Agent generation")
	}
	pending, err := countAIAgentChildPendingApprovals(tx, session.ID, now)
	if err != nil {
		return nil, err
	}
	return &aiAgentDelegationResult{SessionID: session.ID, GenerationID: generation.ID, Status: generation.Status, ErrorCode: generation.ErrorCode, PendingApprovals: pending}, nil
}

func (a *API) guardAIDelegationGeneration(tx *gorm.DB, sessionID string, options aiGenerationRunOptions) error {
	if options.DelegationFollowupID != "" {
		return a.guardAIAgentFollowupGeneration(tx, sessionID, options)
	}
	if options.DelegationProposalID == "" {
		if sessionID == "" {
			return nil
		}
		var proposal models.AIActionProposal
		err := tx.Select("id").Take(&proposal, "status='confirmed' AND result_id=? AND json_extract(action_json,'$.action')=?", sessionID, aiAgentDelegateSpawnAction).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.AIGeneration{}).Where("id=?", proposal.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_PENDING", Message: "The delegated child is waiting for its first generation"}
		}
		return nil
	}
	if options.DelegationProposalID != options.GenerationID || strings.TrimSpace(sessionID) == "" {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_INVALID", Message: "Delegated generation identity is invalid"}
	}
	var proposal models.AIActionProposal
	if err := tx.Take(&proposal, "id=?", options.DelegationProposalID).Error; err != nil {
		return err
	}
	if proposal.Status != "confirmed" || proposal.ResultID == nil || *proposal.ResultID != sessionID || proposal.ResultVersion == nil || *proposal.ResultVersion != 1 {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_INVALID", Message: "Delegation is not confirmed for this child conversation"}
	}
	action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
	if err != nil || action.Action != aiAgentDelegateSpawnAction {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_INVALID", Message: "Delegation approval is invalid"}
	}
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(proposal.PreviewJSON), &preview); err != nil || preview.AgentDelegation == nil || preview.AgentDelegation.ChildSessionID != sessionID || preview.AgentDelegation.ParentGenerationID != proposal.GenerationID || preview.AgentDelegation.ProviderID == "" {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_INVALID", Message: "Delegation preview is invalid"}
	}
	var provider models.AIProvider
	if err := tx.Take(&provider, "id=?", preview.AgentDelegation.ProviderID).Error; err != nil {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_PROVIDER_CHANGED", Message: "The delegated Provider is unavailable"}
	}
	frozen := preview.AgentDelegation
	if provider.Status != "ready" || provider.Version != frozen.ProviderVersion || provider.ConfigVersion != frozen.ConfigVersion || provider.Name != frozen.ProviderName || provider.Model != frozen.Model || (provider.Kind != aiProviderKindLocal) != frozen.LeavesDevice {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_PROVIDER_CHANGED", Message: "The delegated Provider changed after approval"}
	}
	return nil
}

type aiDelegationCoordinator struct {
	a             *API
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	closing       bool
	workers       map[string]*aiDelegationWorker
	reservations  map[string]*aiDelegationReservation
	slots         chan struct{}
	maxConcurrent int
	maxQueued     int
	runWorker     func(context.Context, aiAgentDelegationLaunch)
	wg            sync.WaitGroup
}

type aiDelegationReservation struct {
	coordinator *aiDelegationCoordinator
	proposalIDs []string
}

type aiDelegationWorker struct {
	cancel        context.CancelFunc
	stopRequested bool
	finalizing    bool
}

func newAIDelegationCoordinator(a *API) *aiDelegationCoordinator {
	return newAIDelegationCoordinatorWithLimits(a, aiAgentDelegationMaxConcurrent, aiAgentDelegationMaxQueued)
}

func newAIDelegationCoordinatorWithLimits(a *API, maxConcurrent, maxQueued int) *aiDelegationCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	r := &aiDelegationCoordinator{
		a: a, ctx: ctx, cancel: cancel,
		workers: map[string]*aiDelegationWorker{}, reservations: map[string]*aiDelegationReservation{},
		slots: make(chan struct{}, maxConcurrent), maxConcurrent: maxConcurrent, maxQueued: maxQueued,
	}
	r.runWorker = r.run
	return r
}

// reserve atomically admits all proposals in a decision before the database
// transaction commits. A full queue therefore leaves the displayed proposals
// pending instead of confirming children that cannot be scheduled.
func (r *aiDelegationCoordinator) reserve(specs []aiAgentDelegationLaunch) (*aiDelegationReservation, string) {
	if r == nil {
		return nil, aiDelegationQueueUnavailableCode
	}
	if len(specs) == 0 {
		return nil, aiDelegationQueueUnavailableCode
	}
	ids := make([]string, len(specs))
	seen := make(map[string]bool, len(specs))
	for i, spec := range specs {
		parsed, err := uuid.Parse(spec.ProposalID)
		if err != nil || parsed.String() != spec.ProposalID || seen[spec.ProposalID] {
			return nil, aiDelegationQueueUnavailableCode
		}
		seen[spec.ProposalID] = true
		ids[i] = spec.ProposalID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return nil, aiDelegationQueueUnavailableCode
	}
	for _, id := range ids {
		if r.workers[id] != nil || r.reservations[id] != nil {
			return nil, aiDelegationQueueUnavailableCode
		}
	}
	if len(r.workers)+len(r.reservations)+len(ids) > r.maxConcurrent+r.maxQueued {
		return nil, aiDelegationQueueFullCode
	}
	reservation := &aiDelegationReservation{coordinator: r, proposalIDs: ids}
	for _, id := range ids {
		r.reservations[id] = reservation
	}
	return reservation, ""
}

func (r *aiDelegationCoordinator) releaseReservation(reservation *aiDelegationReservation) {
	if r == nil || reservation == nil || reservation.coordinator != r {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range reservation.proposalIDs {
		if r.reservations[id] == reservation {
			delete(r.reservations, id)
		}
	}
}

func (a *API) reserveAIAgentDelegations(specs []aiAgentDelegationLaunch) (*aiDelegationReservation, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if a == nil || a.aiDelegations == nil {
		return nil, newProjectRequestError(http.StatusServiceUnavailable, aiDelegationQueueUnavailableCode, "The Agent scheduler is unavailable; no child delegation was confirmed")
	}
	reservation, code := a.aiDelegations.reserve(specs)
	switch code {
	case "":
		return reservation, nil
	case aiDelegationQueueFullCode:
		return nil, newProjectRequestError(http.StatusTooManyRequests, code, "The Agent queue is full; no child delegation was confirmed. Retry after active work finishes")
	default:
		return nil, newProjectRequestError(http.StatusServiceUnavailable, aiDelegationQueueUnavailableCode, "The Agent scheduler is unavailable; no child delegation was confirmed")
	}
}

// launchReservedBatch consumes a reservation only after the confirming
// transaction commits. Batch confirmation and scheduler admission remain
// all-or-none; each admitted child then runs independently.
func (r *aiDelegationCoordinator) launchReservedBatch(reservation *aiDelegationReservation, specs []aiAgentDelegationLaunch) string {
	if r == nil || reservation == nil || reservation.coordinator != r || len(specs) == 0 || len(specs) != len(reservation.proposalIDs) {
		return aiDelegationQueueUnavailableCode
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return aiDelegationQueueUnavailableCode
	}
	for i, spec := range specs {
		if spec.ProposalID != reservation.proposalIDs[i] || r.reservations[spec.ProposalID] != reservation || r.workers[spec.ProposalID] != nil {
			return aiDelegationQueueUnavailableCode
		}
	}
	for _, id := range reservation.proposalIDs {
		delete(r.reservations, id)
	}
	for _, spec := range specs {
		ctx, cancel := context.WithTimeout(r.ctx, aiAgentDelegationMaxRun)
		r.workers[spec.ProposalID] = &aiDelegationWorker{cancel: cancel}
		r.wg.Add(1)
		go func(spec aiAgentDelegationLaunch, ctx context.Context, cancel context.CancelFunc) {
			defer r.wg.Done()
			defer func() {
				cancel()
				r.mu.Lock()
				delete(r.workers, spec.ProposalID)
				r.mu.Unlock()
			}()
			r.runWorker(ctx, spec)
		}(spec, ctx, cancel)
	}
	return ""
}

// withConcurrencySlot bounds only active Provider attempts. Provider/session
// busy backoff runs outside the slot, so one busy Provider cannot occupy the
// full global budget while unrelated queued children are ready to run.
func (r *aiDelegationCoordinator) withConcurrencySlot(ctx context.Context, run func()) bool {
	if r == nil || r.slots == nil {
		return false
	}
	select {
	case r.slots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	if ctx.Err() != nil {
		<-r.slots
		return false
	}
	defer func() { <-r.slots }()
	run()
	return true
}

func (r *aiDelegationCoordinator) hasPendingWorker(proposalID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	worker := r.workers[proposalID]
	return !r.closing && worker != nil && !worker.finalizing
}

func (r *aiDelegationCoordinator) beginPreAcceptFinalization(proposalID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	worker := r.workers[proposalID]
	if worker == nil {
		return false
	}
	worker.finalizing = true
	return worker.stopRequested
}

func (r *aiDelegationCoordinator) finishPreAccept(spec aiAgentDelegationLaunch, failureCode string) {
	if r.beginPreAcceptFinalization(spec.ProposalID) {
		var err error
		if spec.Followup != nil {
			err = recordAIAgentFollowupPreAcceptCancellation(r.a.db, spec, r.a.options.Now())
		} else {
			err = recordAIDelegationPreAcceptCancellation(r.a.db, spec, r.a.options.Now())
		}
		if err == nil {
			r.a.recordAIGenerationEvent("ai_generation_cancelled", models.AIGeneration{ID: spec.ProposalID, SessionID: spec.SessionID}, "")
		}
		if err != nil {
			r.a.options.Logger.Print("AI delegated generation cancellation persistence failed for " + spec.ProposalID)
			return
		}
	} else if err := recordAIDelegationLaunchFailure(r.a.db, spec, failureCode, r.a.options.Now()); err != nil {
		r.a.options.Logger.Print("AI delegated generation failure persistence failed for " + spec.ProposalID)
		return
	}
	r.a.wakeAIContinuationsAfterFactCommit()
}

func (r *aiDelegationCoordinator) run(ctx context.Context, spec aiAgentDelegationLaunch) {
	options := aiGenerationRunOptions{
		GenerationID: spec.ProposalID, RequestKey: "delegation:" + spec.ProposalID,
		RequestID: uuid.NewString(), DelegationProposalID: spec.ProposalID, Headless: true,
	}
	if spec.Followup != nil {
		options.RequestKey = "delegation-followup:" + spec.ProposalID
		options.DelegationProposalID = ""
		options.DelegationFollowupID = spec.ProposalID
	}
	options.OnAccept = func(tx *gorm.DB, generation models.AIGeneration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if spec.Followup == nil {
			queued, err := aiAgentDelegationLifecycleEventExists(tx, spec.ProposalID, aiDelegationQueuedEvent)
			if err != nil {
				return err
			}
			if !queued {
				return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_QUEUE_TICKET_MISSING", Message: "The confirmed child has no durable launch ticket"}
			}
			stopped, err := aiAgentDelegationLifecycleEventExists(tx, spec.ProposalID, aiDelegationStopRequestedEvent)
			if err != nil {
				return err
			}
			if stopped {
				return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_STOP_REQUESTED", Message: "The parent has already requested that this child stop"}
			}
		}
		if err := r.a.guardAIDelegationGeneration(tx, spec.SessionID, options); err != nil {
			return err
		}
		if spec.Followup != nil {
			return verifyAIAgentFollowupLaunch(tx, spec, r.a.options.Now())
		}
		var count int64
		if err := tx.Model(&models.AIMessage{}).Where("session_id=?", spec.SessionID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_CHANGED", Message: "The delegated child conversation is no longer empty"}
		}
		return recordAIAgentDelegationLifecycleEvent(tx, spec.ProposalID, spec.SessionID, aiDelegationAcceptedEvent, options.RequestID, nowStamp(r.a))
	}
	input := chatAIRequest{ProviderID: spec.ProviderID, SessionID: spec.SessionID, Message: spec.Message, Workspace: &spec.Grant}
	for {
		if ctx.Err() != nil {
			r.finishPreAccept(spec, "AI_DELEGATION_INTERRUPTED")
			return
		}
		var result aiGenerationRunResult
		var err error
		if !r.withConcurrencySlot(ctx, func() {
			result, err = r.a.runAIGeneration(ctx, input, options, nil)
		}) {
			r.finishPreAccept(spec, "AI_DELEGATION_INTERRUPTED")
			return
		}
		if result.Accepted {
			if result.Status == "completed" || result.Status == "failed" || result.Status == "cancelled" {
				r.a.wakeAIContinuationsAfterFactCommit()
			}
			return
		}
		var runErr *aiGenerationRunError
		busy := errors.As(err, &runErr) && (runErr.Code == "AI_PROVIDER_BUSY" || runErr.Code == "AI_SESSION_BUSY" || runErr.Code == "AI_GENERATION_IN_PROGRESS")
		if !busy {
			code := "AI_DELEGATION_START_FAILED"
			if spec.Followup != nil {
				code = "AI_DELEGATION_FOLLOWUP_START_FAILED"
			}
			if ctx.Err() != nil {
				code = "AI_DELEGATION_INTERRUPTED"
			} else if runErr != nil && runErr.Code != "" {
				code = runErr.Code
			}
			r.finishPreAccept(spec, code)
			return
		}
		timer := time.NewTimer(750 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			r.finishPreAccept(spec, "AI_DELEGATION_INTERRUPTED")
			return
		case <-timer.C:
		}
	}
}

func (r *aiDelegationCoordinator) close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if !r.closing {
		r.closing = true
		r.cancel()
		for _, worker := range r.workers {
			worker.cancel()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func launchRecoveredAIDelegations(a *API, specs []aiAgentDelegationLaunch) string {
	if len(specs) == 0 {
		return ""
	}
	if a == nil || a.aiDelegations == nil {
		return aiDelegationQueueUnavailableCode
	}
	if a.restorePending.Load() {
		return ""
	}
	reservation, code := a.aiDelegations.reserve(specs)
	if code != "" {
		return code
	}
	if code = a.aiDelegations.launchReservedBatch(reservation, specs); code != "" {
		a.aiDelegations.releaseReservation(reservation)
	}
	return code
}

func recordAIDelegationPreAcceptFailure(db *gorm.DB, spec aiAgentDelegationLaunch, code string, now time.Time) error {
	return recordAIDelegationPreAcceptOutcome(db, spec, "failed", code, now)
}

func recordAIDelegationPreAcceptCancellation(db *gorm.DB, spec aiAgentDelegationLaunch, now time.Time) error {
	return recordAIDelegationPreAcceptOutcome(db, spec, "cancelled", "", now)
}

func recordAIDelegationPreAcceptOutcome(db *gorm.DB, spec aiAgentDelegationLaunch, status, code string, now time.Time) error {
	if status != "failed" && status != "cancelled" {
		return errors.New("invalid pre-acceptance delegation outcome")
	}
	if status == "cancelled" {
		code = ""
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if status == "failed" && (strings.TrimSpace(code) == "" || len(code) > 100) {
		code = "AI_DELEGATION_START_FAILED"
	}
	var errorCode *string
	if code != "" {
		errorCode = &code
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var proposal models.AIActionProposal
		if err := tx.Take(&proposal, "id=?", spec.ProposalID).Error; err != nil {
			return err
		}
		if proposal.Status != "confirmed" || proposal.ResultID == nil || *proposal.ResultID != spec.SessionID {
			return errors.New("delegation proposal is not confirmed for this session")
		}
		action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
		if err != nil || action.Action != aiAgentDelegateSpawnAction {
			return errors.New("invalid delegation action")
		}
		var changes aiAgentDelegationChanges
		if err := json.Unmarshal(action.Changes, &changes); err != nil || changes.Message != spec.Message {
			return errors.New("delegation message changed")
		}
		var preview aiActionPreview
		if err := json.Unmarshal([]byte(proposal.PreviewJSON), &preview); err != nil || preview.AgentDelegation == nil {
			return errors.New("invalid delegation preview")
		}
		frozen := preview.AgentDelegation
		if frozen.ParentGenerationID != proposal.GenerationID || frozen.ChildSessionID != spec.SessionID || frozen.ProviderID != spec.ProviderID || frozen.Message != spec.Message || frozen.ProviderVersion != spec.Grant.ProviderVersion {
			return errors.New("delegation identity changed")
		}
		frozenGrant, _ := json.Marshal(aiWorkspaceGrant{ProviderVersion: frozen.ProviderVersion, Scopes: frozen.Scopes, KnowledgeSources: frozen.Knowledge})
		actualGrant, _ := json.Marshal(spec.Grant)
		if string(frozenGrant) != string(actualGrant) {
			return errors.New("delegation grant changed")
		}
		var existing int64
		if err := tx.Model(&models.AIGeneration{}).Where("id=?", spec.ProposalID).Count(&existing).Error; err != nil || existing != 0 {
			return err
		}
		var session models.AISession
		if err := tx.Select("id,persist").Take(&session, "id=?", spec.SessionID).Error; err != nil || !session.Persist {
			return errors.New("delegated child session is unavailable")
		}
		var messages int64
		if err := tx.Model(&models.AIMessage{}).Where("session_id=?", spec.SessionID).Count(&messages).Error; err != nil {
			return err
		}
		if messages != 0 {
			return errors.New("delegated child session is no longer empty")
		}
		requestKey := "delegation:" + spec.ProposalID
		input := chatAIRequest{ProviderID: spec.ProviderID, SessionID: spec.SessionID, Message: spec.Message, Workspace: &spec.Grant}
		requestJSON, _ := json.Marshal(input)
		requestHash := sha256Hex(requestJSON)
		generation := models.AIGeneration{ID: spec.ProposalID, SessionID: spec.SessionID, ProviderID: spec.ProviderID, Status: status, ErrorCode: errorCode, RequestKey: &requestKey, RequestHash: &requestHash, CreatedAt: stamp, UpdatedAt: stamp}
		if err := tx.Create(&generation).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.AIMessage{ID: uuid.NewString(), SessionID: spec.SessionID, Role: "user", Status: "completed", Content: spec.Message, GenerationID: &generation.ID, CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
			return err
		}
		message := "子智能体未能开始执行。请查看错误码并在父会话中重新确认新的委派；系统不会自动重放这次请求。错误码：" + code
		if status == "failed" && code == "AI_DELEGATION_INTERRUPTED" {
			message = "子智能体在上次启动期间被中断，受理结果不可确认。系统不会自动重放；请在父会话核对后决定是否创建新的委派。"
		}
		if status == "cancelled" {
			message = "父会话已请求停止；子任务在生成受理前取消，本次模型请求未发送。"
		}
		if err := tx.Create(&models.AIMessage{ID: uuid.NewString(), SessionID: spec.SessionID, Role: "assistant", Status: status, Content: message, GenerationID: &generation.ID, CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AISession{}).Where("id=?", spec.SessionID).Updates(map[string]any{"version": gorm.Expr("version+1"), "updated_at": stamp}).Error; err != nil {
			return err
		}
		zero := int64(0)
		return tx.Create(&models.AIRunStep{ID: uuid.NewString(), GenerationID: generation.ID, Sequence: 1, Kind: "generation", Status: status, StartedAt: stamp, CompletedAt: &stamp, DurationMS: &zero, ErrorCode: errorCode, CreatedAt: stamp}).Error
	})
}

func recoverAIDelegationsOnStartup(db *gorm.DB, now time.Time) ([]aiAgentDelegationLaunch, error) {
	var rows []models.AIActionProposal
	if err := db.Where("status='confirmed' AND json_extract(action_json,'$.action')=?", aiAgentDelegateSpawnAction).Find(&rows).Error; err != nil {
		return nil, err
	}
	var pending []aiAgentDelegationLaunch
	for _, row := range rows {
		var count int64
		if err := db.Model(&models.AIGeneration{}).Where("id=?", row.ID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 0 {
			continue
		}
		accepted, err := aiAgentDelegationLifecycleEventExists(db, row.ID, aiDelegationAcceptedEvent)
		if err != nil {
			return nil, err
		}
		stopRequested, err := aiAgentDelegationLifecycleEventExists(db, row.ID, aiDelegationStopRequestedEvent)
		if err != nil {
			return nil, err
		}
		if row.ResultID == nil {
			return nil, errors.New("confirmed delegation has no child session")
		}
		action, err := parseAIWorkspaceAction([]byte(row.ActionJSON))
		if err != nil {
			return nil, err
		}
		var changes aiAgentDelegationChanges
		if err := json.Unmarshal(action.Changes, &changes); err != nil {
			return nil, err
		}
		var preview aiActionPreview
		if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil || preview.AgentDelegation == nil {
			return nil, errors.New("invalid confirmed delegation preview")
		}
		spec := aiAgentDelegationLaunch{ProposalID: row.ID, SessionID: *row.ResultID, ProviderID: preview.AgentDelegation.ProviderID, Message: changes.Message, Grant: aiWorkspaceGrant{ProviderVersion: preview.AgentDelegation.ProviderVersion, Scopes: append([]string(nil), preview.AgentDelegation.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), preview.AgentDelegation.Knowledge...)}}
		queued, err := aiAgentDelegationLifecycleEventExists(db, row.ID, aiDelegationQueuedEvent)
		if err != nil {
			return nil, err
		}
		if queued && !accepted && !stopRequested {
			pending = append(pending, spec)
			continue
		}
		if stopRequested && !accepted {
			err = recordAIDelegationPreAcceptCancellation(db, spec, now)
		} else {
			// Legacy proposals have no queue ticket; an accepted marker with a
			// missing Generation is also uncertain. Neither case may be replayed.
			err = recordAIDelegationPreAcceptFailure(db, spec, "AI_DELEGATION_INTERRUPTED", now)
		}
		if err != nil {
			return nil, fmt.Errorf("recover delegation %s: %w", row.ID, err)
		}
	}
	if err := recoverAIAgentFollowupsOnStartup(db, now); err != nil {
		return nil, err
	}
	return pending, nil
}
