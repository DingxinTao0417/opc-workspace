package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiAgentDelegateFollowupAction = "agent_delegate.followup"

type aiAgentFollowupChanges struct {
	ChildSessionID string   `json:"child_session_id"`
	Message        string   `json:"message"`
	Scopes         []string `json:"scopes"`
}

type aiAgentFollowupPreview struct {
	Target       aiAgentFollowupTarget    `json:"target"`
	TaskName     string                   `json:"task_name"`
	Message      string                   `json:"message"`
	Scopes       []string                 `json:"scopes"`
	Knowledge    []aiKnowledgeSourceGrant `json:"knowledge_sources,omitempty"`
	ProviderName string                   `json:"provider_name"`
	Model        string                   `json:"model"`
	LeavesDevice bool                     `json:"leaves_device"`
}

func aiAgentFollowupSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"child_session_id", "message", "scopes"},
		"properties": map[string]any{
			"child_session_id": map[string]any{"type": "string", "format": "uuid"},
			"message":          map[string]any{"type": "string", "minLength": 1, "maxLength": 4000},
			"scopes":           map[string]any{"type": "array", "minItems": 1, "maxItems": len(aiAgentDelegationScopeOrder), "uniqueItems": true, "items": map[string]any{"type": "string"}},
		},
	})
	return encoded
}

func normalizeAIAgentFollowupChanges(input aiAgentFollowupChanges) (aiAgentFollowupChanges, error) {
	parsed, err := uuid.Parse(input.ChildSessionID)
	if err != nil || parsed.String() != input.ChildSessionID {
		return input, errors.New("child_session_id must be a canonical UUID")
	}
	normalized, err := normalizeAIAgentDelegationChanges(aiAgentDelegationChanges{TaskName: "followup", Message: input.Message, Scopes: input.Scopes})
	if err != nil {
		return input, err
	}
	input.Message = normalized.Message
	input.Scopes = normalized.Scopes
	return input, nil
}

func parseAIAgentFollowupAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.Action != aiAgentDelegateFollowupAction || len(raw) != 2 || raw["action"] == nil || raw["changes"] == nil {
		return input, errors.New("Agent follow-up accepts only action and changes")
	}
	if len(fields) != 3 || fields["child_session_id"] == nil || fields["message"] == nil || fields["scopes"] == nil {
		return input, errors.New("Agent follow-up requires child_session_id, message and scopes")
	}
	var changes aiAgentFollowupChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return input, err
	}
	normalized, err := normalizeAIAgentFollowupChanges(changes)
	if err != nil {
		return input, err
	}
	input.Changes, _ = json.Marshal(normalized)
	return input, nil
}

// aiAgentFollowupTarget is a frozen acceptance baseline, not permission to
// dispatch another model request. A later proposal and its human decision must
// re-read it in their own transactions before a follow-up can be accepted.
type aiAgentFollowupTarget struct {
	ParentSessionID    string `json:"parent_session_id"`
	ChildSessionID     string `json:"child_session_id"`
	SpawnProposalID    string `json:"spawn_proposal_id"`
	PreviousGeneration string `json:"previous_generation_id"`
	ChildVersion       int64  `json:"child_version"`
	ProviderID         string `json:"provider_id"`
	ProviderVersion    int64  `json:"provider_version"`
	ConfigVersion      int64  `json:"provider_config_version"`
}

// This builds the full consent document from the same live facts that will be
// checked again at decision and generation acceptance. A fresh approval may
// grant fewer capabilities, but it cannot silently expand the child's original
// scope or introduce different knowledge sources or controlled files.
func previewAIAgentFollowup(tx *gorm.DB, input aiWorkspaceAction, parentGenerationID, providerID string, configVersion int64, parentGrant *aiWorkspaceGrant, stored *aiAgentFollowupPreview, now time.Time) (aiActionPreview, error) {
	return previewAIAgentFollowupIgnoring(tx, input, parentGenerationID, providerID, configVersion, parentGrant, stored, now, "")
}

func previewAIAgentFollowupIgnoring(tx *gorm.DB, input aiWorkspaceAction, parentGenerationID, providerID string, configVersion int64, parentGrant *aiWorkspaceGrant, stored *aiAgentFollowupPreview, now time.Time, acceptingGenerationID string) (aiActionPreview, error) {
	var changes aiAgentFollowupChanges
	if err := json.Unmarshal(input.Changes, &changes); err != nil {
		return aiActionPreview{}, err
	}
	var parentGeneration models.AIGeneration
	if err := tx.Select("id,session_id,provider_id,status").Take(&parentGeneration, "id=?", parentGenerationID).Error; err != nil {
		return aiActionPreview{}, err
	}
	wantStatus := "streaming"
	if stored != nil {
		wantStatus = "completed"
	}
	if parentGeneration.Status != wantStatus || parentGeneration.ProviderID != providerID {
		return aiActionPreview{}, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PARENT_CHANGED", "The parent generation changed; request a fresh follow-up")
	}
	target, err := inspectAIAgentFollowupTargetIgnoring(tx, parentGeneration.SessionID, changes.ChildSessionID, providerID, now, acceptingGenerationID)
	if err != nil {
		return aiActionPreview{}, err
	}
	if stored != nil && target != stored.Target {
		return aiActionPreview{}, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_FOLLOWUP_CHANGED", "The child conversation changed; review a fresh follow-up")
	}
	var spawn models.AIActionProposal
	if err := tx.Select("preview_json").Take(&spawn, "id=?", target.SpawnProposalID).Error; err != nil {
		return aiActionPreview{}, err
	}
	var spawnPreview aiActionPreview
	if err := json.Unmarshal([]byte(spawn.PreviewJSON), &spawnPreview); err != nil || spawnPreview.AgentDelegation == nil {
		return aiActionPreview{}, errors.New("invalid original child approval")
	}
	for _, scope := range changes.Scopes {
		if !slices.Contains(spawnPreview.AgentDelegation.Scopes, scope) || scope == "agent_files" || scope == "agent_project_files" {
			return aiActionPreview{}, newProjectRequestError(http.StatusUnprocessableEntity, "AI_DELEGATION_SCOPE_INVALID", "Follow-up scopes must be a non-file subset of the original child authorization")
		}
	}
	childGrant, err := delegatedAIWorkspaceGrant(parentGrant, changes.Scopes)
	if err != nil {
		return aiActionPreview{}, newProjectRequestError(http.StatusUnprocessableEntity, "AI_DELEGATION_SCOPE_INVALID", err.Error())
	}
	if slices.Contains(changes.Scopes, "knowledge") && !slices.Equal(childGrant.KnowledgeSources, spawnPreview.AgentDelegation.Knowledge) {
		return aiActionPreview{}, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_KNOWLEDGE_CHANGED", "The child's original knowledge sources changed; review a new delegation")
	}
	var provider models.AIProvider
	if err := tx.Take(&provider, "id=?", providerID).Error; err != nil {
		return aiActionPreview{}, err
	}
	if provider.ConfigVersion != configVersion || provider.Version != childGrant.ProviderVersion {
		return aiActionPreview{}, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PROVIDER_CHANGED", "The selected Provider changed; authorize a fresh follow-up")
	}
	if err := validateAIWorkspaceGrant(&provider, &childGrant); err != nil {
		return aiActionPreview{}, err
	}
	if stored != nil && (stored.Message != changes.Message || !slices.Equal(stored.Scopes, changes.Scopes) || !slices.Equal(stored.Knowledge, childGrant.KnowledgeSources) || stored.TaskName != spawnPreview.AgentDelegation.TaskName || stored.ProviderName != provider.Name || stored.Model != provider.Model || stored.LeavesDevice != (provider.Kind != aiProviderKindLocal)) {
		return aiActionPreview{}, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_FOLLOWUP_CHANGED", "Follow-up approval changed; review a fresh proposal")
	}
	return aiActionPreview{Label: spawnPreview.AgentDelegation.TaskName, Before: map[string]any{}, After: map[string]any{}, AgentFollowup: &aiAgentFollowupPreview{
		Target: target, TaskName: spawnPreview.AgentDelegation.TaskName,
		Message: changes.Message, Scopes: childGrant.Scopes, Knowledge: childGrant.KnowledgeSources,
		ProviderName: provider.Name, Model: provider.Model, LeavesDevice: provider.Kind != aiProviderKindLocal,
	}}, nil
}

func inspectAIAgentFollowupTarget(tx *gorm.DB, parentSessionID, childSessionID, providerID string, now time.Time) (aiAgentFollowupTarget, error) {
	return inspectAIAgentFollowupTargetIgnoring(tx, parentSessionID, childSessionID, providerID, now, "")
}

func inspectAIAgentFollowupTargetIgnoring(tx *gorm.DB, parentSessionID, childSessionID, providerID string, now time.Time, acceptingGenerationID string) (aiAgentFollowupTarget, error) {
	var target aiAgentFollowupTarget
	family, err := readAIAgentFamily(tx, parentSessionID, now)
	if err != nil {
		return target, err
	}
	if family.Parent != nil {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_DEPTH_LIMIT", "A delegated child cannot follow up another child")
	}
	var child *aiAgentFamilyMember
	for i := range family.Children {
		if family.Children[i].SessionID == childSessionID {
			child = &family.Children[i]
			break
		}
	}
	if child == nil || !child.Available {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_CHILD_UNAVAILABLE", "Select an available, confirmed child of this conversation")
	}
	if child.Status != "completed" || child.PendingApprovals > 0 {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_CHILD_BLOCKED", "Finish the child generation and decide its pending actions before following up")
	}
	var proposal models.AIActionProposal
	if err := tx.Take(&proposal, "id=? AND status='confirmed' AND result_id=?", child.ProposalID, childSessionID).Error; err != nil {
		return target, err
	}
	var preview aiActionPreview
	if err := decodeStrictToolArguments([]byte(proposal.PreviewJSON), &preview); err != nil || preview.AgentDelegation == nil {
		return target, errors.New("invalid child approval preview")
	}
	frozen := preview.AgentDelegation
	if frozen.ParentSessionID != parentSessionID || frozen.ChildSessionID != childSessionID || frozen.ProviderID != providerID {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PROVIDER_CHANGED", "The child uses a different Provider; review a new delegation")
	}
	var provider models.AIProvider
	if err := tx.Take(&provider, "id=?", providerID).Error; err != nil {
		return target, err
	}
	if provider.Status != "ready" || provider.Version != frozen.ProviderVersion || provider.ConfigVersion != frozen.ConfigVersion || provider.Name != frozen.ProviderName || provider.Model != frozen.Model || (provider.Kind != aiProviderKindLocal) != frozen.LeavesDevice {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_PROVIDER_CHANGED", "The child's Provider changed; review a new delegation")
	}
	var session models.AISession
	if err := tx.Select("id,persist,version").Take(&session, "id=?", childSessionID).Error; err != nil {
		return target, err
	}
	if !session.Persist || session.Version < 1 {
		return target, errors.New("invalid child conversation")
	}
	query := tx.Where("session_id=?", childSessionID)
	if acceptingGenerationID != "" {
		var accepting models.AIGeneration
		err := tx.Select("id,session_id,provider_id,status").Take(&accepting, "id=?", acceptingGenerationID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return target, err
		}
		if err == nil {
			if accepting.SessionID != childSessionID || accepting.ProviderID != providerID || accepting.Status != "streaming" {
				return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_FOLLOWUP_CHANGED", "The accepting child generation changed")
			}
			query = query.Where("id<>?", acceptingGenerationID)
		}
	}
	var latest models.AIGeneration
	if err := query.Order("rowid DESC").Take(&latest).Error; err != nil {
		return target, err
	}
	if latest.ProviderID != providerID || latest.Status != "completed" {
		return target, newProjectRequestError(http.StatusConflict, "AI_DELEGATION_CHILD_BLOCKED", "The latest child generation is not completed on the approved Provider")
	}
	var active int64
	if err := tx.Table("ai_continuations").Where("session_id=? AND status IN ('waiting','running')", childSessionID).Count(&active).Error; err != nil {
		return target, err
	}
	if active != 0 {
		return target, newProjectRequestError(http.StatusConflict, "AI_CONTINUATION_ACTIVE", "Stop the child's automatic continuation before following up")
	}
	return aiAgentFollowupTarget{
		ParentSessionID: parentSessionID, ChildSessionID: childSessionID,
		SpawnProposalID: child.ProposalID, PreviousGeneration: latest.ID,
		ChildVersion: session.Version, ProviderID: providerID,
		ProviderVersion: provider.Version, ConfigVersion: provider.ConfigVersion,
	}, nil
}

// The immutable preview must be compared with fresh database facts both when a
// human confirms it and immediately before a headless generation is accepted.
// A child message, a newer generation, or a Provider change invalidates the
// old review; callers must never silently retarget it.
func recheckAIAgentFollowupTarget(tx *gorm.DB, baseline aiAgentFollowupTarget, now time.Time) error {
	return recheckAIAgentFollowupTargetIgnoring(tx, baseline, now, "")
}

func recheckAIAgentFollowupTargetIgnoring(tx *gorm.DB, baseline aiAgentFollowupTarget, now time.Time, acceptingGenerationID string) error {
	current, err := inspectAIAgentFollowupTargetIgnoring(tx, baseline.ParentSessionID, baseline.ChildSessionID, baseline.ProviderID, now, acceptingGenerationID)
	if err != nil {
		return err
	}
	if current != baseline {
		return newProjectRequestError(http.StatusConflict, "AI_DELEGATION_FOLLOWUP_CHANGED", "The child conversation changed; review a fresh follow-up")
	}
	return nil
}

// A follow-up generation has exactly one confirmed, immutable proposal owner.
// The model cannot supply this owner ID through chat input: only the in-process
// coordinator sets it, and both preflight and acceptance transactions recheck
// the complete consent document against current child facts.
func (a *API) guardAIAgentFollowupGeneration(tx *gorm.DB, sessionID string, options aiGenerationRunOptions) error {
	if options.DelegationProposalID != "" || options.DelegationFollowupID == "" || options.GenerationID != options.DelegationFollowupID || sessionID == "" {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_FOLLOWUP_INVALID", Message: "Follow-up generation identity is invalid"}
	}
	var proposal models.AIActionProposal
	if err := tx.Take(&proposal, "id=?", options.DelegationFollowupID).Error; err != nil {
		return err
	}
	if proposal.Status != "confirmed" || proposal.ResultID == nil || *proposal.ResultID != sessionID || proposal.ResultVersion == nil || *proposal.ResultVersion != 1 {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_FOLLOWUP_INVALID", Message: "Follow-up approval is not confirmed for this child"}
	}
	action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
	if err != nil || action.Action != aiAgentDelegateFollowupAction {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_FOLLOWUP_INVALID", Message: "Follow-up action is invalid"}
	}
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(proposal.PreviewJSON), &preview); err != nil || preview.AgentFollowup == nil || preview.AgentFollowup.Target.ChildSessionID != sessionID {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_FOLLOWUP_INVALID", Message: "Follow-up consent preview is invalid"}
	}
	frozen := preview.AgentFollowup
	grant := &aiWorkspaceGrant{ProviderVersion: frozen.Target.ProviderVersion, Scopes: append([]string(nil), frozen.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), frozen.Knowledge...)}
	current, err := previewAIAgentFollowupIgnoring(tx, action, proposal.GenerationID, frozen.Target.ProviderID, frozen.Target.ConfigVersion, grant, frozen, a.options.Now(), options.GenerationID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(current)
	if err != nil || string(encoded) != proposal.PreviewJSON {
		return &aiGenerationRunError{Status: http.StatusConflict, Code: "AI_DELEGATION_FOLLOWUP_CHANGED", Message: "Follow-up consent changed; review a new proposal"}
	}
	return nil
}

func verifyAIAgentFollowupLaunch(tx *gorm.DB, spec aiAgentDelegationLaunch, now time.Time) error {
	if spec.Followup == nil || spec.ProposalID == "" || spec.SessionID != spec.Followup.ChildSessionID || spec.ProviderID != spec.Followup.ProviderID {
		return errors.New("invalid follow-up launch identity")
	}
	var proposal models.AIActionProposal
	if err := tx.Take(&proposal, "id=?", spec.ProposalID).Error; err != nil {
		return err
	}
	if proposal.Status != "confirmed" || proposal.ResultID == nil || *proposal.ResultID != spec.SessionID || proposal.ResultVersion == nil || *proposal.ResultVersion != 1 {
		return errors.New("follow-up is not confirmed for this child")
	}
	action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
	if err != nil || action.Action != aiAgentDelegateFollowupAction {
		return errors.New("invalid follow-up action")
	}
	var changes aiAgentFollowupChanges
	var preview aiActionPreview
	if json.Unmarshal(action.Changes, &changes) != nil || json.Unmarshal([]byte(proposal.PreviewJSON), &preview) != nil || preview.AgentFollowup == nil {
		return errors.New("invalid follow-up approval")
	}
	frozen := preview.AgentFollowup
	if frozen.Target != *spec.Followup || changes.ChildSessionID != spec.SessionID || changes.Message != spec.Message || frozen.Message != spec.Message || !slices.Equal(changes.Scopes, spec.Grant.Scopes) || !slices.Equal(frozen.Scopes, spec.Grant.Scopes) || !slices.Equal(frozen.Knowledge, spec.Grant.KnowledgeSources) || frozen.Target.ProviderVersion != spec.Grant.ProviderVersion {
		return errors.New("follow-up launch differs from approved instruction or grant")
	}
	return recheckAIAgentFollowupTargetIgnoring(tx, *spec.Followup, now, spec.ProposalID)
}

func executeAIAgentFollowup(tx *gorm.DB, proposal models.AIActionProposal, action aiWorkspaceAction, preview *aiAgentFollowupPreview, now time.Time) (aiActionResult, *aiAgentDelegationLaunch, error) {
	if preview == nil || preview.Target.ChildSessionID == "" || preview.Target.ParentSessionID == "" || proposal.GenerationID == "" {
		return aiActionResult{}, nil, errors.New("invalid follow-up consent preview")
	}
	var parentGeneration models.AIGeneration
	if err := tx.Select("id,session_id").Take(&parentGeneration, "id=?", proposal.GenerationID).Error; err != nil {
		return aiActionResult{}, nil, err
	}
	if parentGeneration.SessionID != preview.Target.ParentSessionID {
		return aiActionResult{}, nil, errors.New("follow-up parent generation changed")
	}
	if err := recheckAIAgentFollowupTarget(tx, preview.Target, now); err != nil {
		return aiActionResult{}, nil, err
	}
	var changes aiAgentFollowupChanges
	if err := json.Unmarshal(action.Changes, &changes); err != nil || changes.ChildSessionID != preview.Target.ChildSessionID || changes.Message != preview.Message || !slices.Equal(changes.Scopes, preview.Scopes) {
		return aiActionResult{}, nil, errors.New("follow-up instruction changed")
	}
	target := preview.Target
	launch := &aiAgentDelegationLaunch{ProposalID: proposal.ID, SessionID: target.ChildSessionID, ProviderID: target.ProviderID, Message: changes.Message,
		Grant: aiWorkspaceGrant{ProviderVersion: target.ProviderVersion, Scopes: append([]string(nil), preview.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), preview.Knowledge...)}, Followup: &target}
	return aiActionResult{ID: target.ChildSessionID, Version: 1}, launch, nil
}

func readAIAgentFollowupResult(tx *gorm.DB, out aiActionResponse, now time.Time) (*aiAgentDelegationResult, error) {
	if out.Preview.AgentFollowup == nil || out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || *out.ResultID != out.Preview.AgentFollowup.Target.ChildSessionID {
		return nil, errors.New("invalid follow-up receipt")
	}
	var session models.AISession
	if err := tx.Select("id,persist").Take(&session, "id=?", *out.ResultID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		code := "AI_DELEGATION_CHILD_UNAVAILABLE"
		return &aiAgentDelegationResult{SessionID: *out.ResultID, GenerationID: out.ID, Status: "unavailable", ErrorCode: &code}, nil
	} else if err != nil {
		return nil, err
	}
	if !session.Persist {
		return nil, errors.New("follow-up child conversation is unavailable")
	}
	var generation models.AIGeneration
	err := tx.Select("id,session_id,provider_id,status,error_code").Take(&generation, "id=?", out.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &aiAgentDelegationResult{SessionID: session.ID, GenerationID: out.ID, Status: "queued"}, nil
	}
	if err != nil {
		return nil, err
	}
	if generation.SessionID != session.ID || generation.ProviderID != out.Preview.AgentFollowup.Target.ProviderID {
		return nil, errors.New("inconsistent follow-up generation")
	}
	pending, err := countAIAgentChildPendingApprovals(tx, session.ID, now)
	if err != nil {
		return nil, err
	}
	return &aiAgentDelegationResult{SessionID: session.ID, GenerationID: generation.ID, Status: generation.Status, ErrorCode: generation.ErrorCode, PendingApprovals: pending}, nil
}

func recordAIDelegationLaunchFailure(db *gorm.DB, spec aiAgentDelegationLaunch, code string, now time.Time) error {
	if spec.Followup != nil {
		return recordAIAgentFollowupPreAcceptFailure(db, spec, code, now)
	}
	return recordAIDelegationPreAcceptFailure(db, spec, code, now)
}

// A confirmed follow-up with no accepted generation is never retried after an
// uncertain launch. Record a terminal child fact instead. This does not call
// the Provider and cannot turn a failed acceptance into another model request.
func recordAIAgentFollowupPreAcceptFailure(db *gorm.DB, spec aiAgentDelegationLaunch, code string, now time.Time) error {
	return recordAIAgentFollowupPreAcceptOutcome(db, spec, "failed", code, now)
}

func recordAIAgentFollowupPreAcceptCancellation(db *gorm.DB, spec aiAgentDelegationLaunch, now time.Time) error {
	return recordAIAgentFollowupPreAcceptOutcome(db, spec, "cancelled", "", now)
}

func recordAIAgentFollowupPreAcceptOutcome(db *gorm.DB, spec aiAgentDelegationLaunch, status, code string, now time.Time) error {
	if spec.Followup == nil {
		return errors.New("missing follow-up baseline")
	}
	if status != "failed" && status != "cancelled" {
		return errors.New("invalid pre-acceptance follow-up outcome")
	}
	if status == "cancelled" {
		code = ""
	}
	if status == "failed" && (code == "" || len(code) > 100) {
		code = "AI_DELEGATION_FOLLOWUP_START_FAILED"
	}
	var errorCode *string
	if code != "" {
		errorCode = &code
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	return db.Transaction(func(tx *gorm.DB) error {
		var proposal models.AIActionProposal
		if err := tx.Take(&proposal, "id=?", spec.ProposalID).Error; err != nil {
			return err
		}
		if proposal.Status != "confirmed" || proposal.ResultID == nil || *proposal.ResultID != spec.SessionID || proposal.ResultVersion == nil || *proposal.ResultVersion != 1 {
			return errors.New("follow-up proposal is not confirmed for this child")
		}
		action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
		if err != nil || action.Action != aiAgentDelegateFollowupAction {
			return errors.New("invalid follow-up action")
		}
		var changes aiAgentFollowupChanges
		var preview aiActionPreview
		if json.Unmarshal(action.Changes, &changes) != nil || json.Unmarshal([]byte(proposal.PreviewJSON), &preview) != nil || preview.AgentFollowup == nil {
			return errors.New("invalid follow-up approval")
		}
		frozen := preview.AgentFollowup
		if frozen.Target != *spec.Followup || changes.ChildSessionID != spec.SessionID || changes.Message != spec.Message || frozen.Message != spec.Message || !slices.Equal(changes.Scopes, spec.Grant.Scopes) || !slices.Equal(frozen.Scopes, spec.Grant.Scopes) || !slices.Equal(frozen.Knowledge, spec.Grant.KnowledgeSources) || frozen.Target.ProviderVersion != spec.Grant.ProviderVersion {
			return errors.New("follow-up failure fact differs from approved instruction or grant")
		}
		var existing int64
		if err := tx.Model(&models.AIGeneration{}).Where("id=?", spec.ProposalID).Count(&existing).Error; err != nil {
			return err
		}
		if existing != 0 {
			return errors.New("follow-up generation already exists")
		}
		var child models.AISession
		if err := tx.Select("id,persist").Take(&child, "id=?", spec.SessionID).Error; err != nil || !child.Persist {
			return errors.New("follow-up child session is unavailable")
		}
		requestKey := "delegation-followup:" + spec.ProposalID
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
		failure := "子智能体续交办未能开始。请核对错误码与当前子会话，系统不会自动重放该请求。错误码：" + code
		if status == "cancelled" {
			failure = "父会话已请求停止；子任务在生成受理前取消，本次模型请求未发送。"
		}
		if err := tx.Create(&models.AIMessage{ID: uuid.NewString(), SessionID: spec.SessionID, Role: "assistant", Status: status, Content: failure, GenerationID: &generation.ID, CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.AISession{}).Where("id=?", spec.SessionID).Updates(map[string]any{"version": gorm.Expr("version+1"), "updated_at": stamp}).Error; err != nil {
			return err
		}
		zero := int64(0)
		return tx.Create(&models.AIRunStep{ID: uuid.NewString(), GenerationID: generation.ID, Sequence: 1, Kind: "generation", Status: status, StartedAt: stamp, CompletedAt: &stamp, DurationMS: &zero, ErrorCode: errorCode, CreatedAt: stamp}).Error
	})
}

func recoverAIAgentFollowupsOnStartup(db *gorm.DB, now time.Time) error {
	var rows []models.AIActionProposal
	if err := db.Where("status='confirmed' AND json_extract(action_json,'$.action')=?", aiAgentDelegateFollowupAction).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var count int64
		if err := db.Model(&models.AIGeneration{}).Where("id=?", row.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			continue
		}
		if row.ResultID == nil || row.ResultVersion == nil || *row.ResultVersion != 1 {
			return errors.New("confirmed follow-up has no child receipt")
		}
		var childCount int64
		if err := db.Model(&models.AISession{}).Where("id=? AND persist=1", *row.ResultID).Count(&childCount).Error; err != nil {
			return err
		}
		if childCount == 0 {
			// The user deleted this child; no FK-backed Generation can be
			// synthesized. The receipt projects unavailable and nothing is sent.
			continue
		}
		action, err := parseAIWorkspaceAction([]byte(row.ActionJSON))
		if err != nil || action.Action != aiAgentDelegateFollowupAction {
			return errors.New("invalid confirmed follow-up action")
		}
		var changes aiAgentFollowupChanges
		var preview aiActionPreview
		if json.Unmarshal(action.Changes, &changes) != nil || json.Unmarshal([]byte(row.PreviewJSON), &preview) != nil || preview.AgentFollowup == nil {
			return errors.New("invalid confirmed follow-up preview")
		}
		frozen := preview.AgentFollowup
		target := frozen.Target
		spec := aiAgentDelegationLaunch{ProposalID: row.ID, SessionID: *row.ResultID, ProviderID: target.ProviderID, Message: changes.Message,
			Grant: aiWorkspaceGrant{ProviderVersion: target.ProviderVersion, Scopes: append([]string(nil), frozen.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), frozen.Knowledge...)}, Followup: &target}
		if err := recordAIAgentFollowupPreAcceptFailure(db, spec, "AI_DELEGATION_INTERRUPTED", now); err != nil {
			return err
		}
	}
	return nil
}
