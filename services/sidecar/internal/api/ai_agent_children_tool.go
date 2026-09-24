package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiAgentChildrenInput struct {
	View            string   `json:"view"`
	ChildSessionID  string   `json:"child_session_id,omitempty"`
	ChildSessionIDs []string `json:"child_session_ids,omitempty"`
	GenerationID    *string  `json:"generation_id,omitempty"`
	ContentOffset   *int     `json:"content_offset,omitempty"`
	ContentLimit    *int     `json:"content_limit,omitempty"`
	ExpectedSHA256  *string  `json:"expected_content_sha256,omitempty"`
	WaitMS          *int     `json:"timeout_ms,omitempty"`
	WaitMode        *string  `json:"wait_mode,omitempty"`
}

type aiAgentChildrenReadError string

func (err aiAgentChildrenReadError) Error() string { return string(err) }

const aiAgentChildWaitFallbackInterval = time.Second

func aiAgentChildrenSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"view"},
		"properties": map[string]any{
			"view":                    map[string]any{"type": "string", "enum": []string{"list", "result", "results", "wait"}},
			"child_session_id":        map[string]any{"type": "string", "format": "uuid"},
			"child_session_ids":       map[string]any{"type": "array", "minItems": 1, "maxItems": aiAgentDelegationMaxCount, "uniqueItems": true, "items": map[string]any{"type": "string", "format": "uuid"}},
			"generation_id":           map[string]any{"type": "string", "format": "uuid", "description": "Optional exact parent-authorized child reply generation; required for continued pages."},
			"content_offset":          map[string]any{"type": "integer", "minimum": 0, "maximum": 1000000},
			"content_limit":           map[string]any{"type": "integer", "minimum": 1, "maximum": 4000},
			"expected_content_sha256": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
			"timeout_ms":              map[string]any{"type": "integer", "minimum": 1, "maximum": 15000},
			"wait_mode":               map[string]any{"type": "string", "enum": []string{"any", "all"}},
		},
	})
	return encoded
}

func (input aiAgentChildrenInput) validate() error {
	switch input.View {
	case "list":
		if input.ChildSessionID != "" || len(input.ChildSessionIDs) != 0 || input.GenerationID != nil || input.ContentOffset != nil || input.ContentLimit != nil || input.ExpectedSHA256 != nil || input.WaitMS != nil || input.WaitMode != nil {
			return errors.New("child result arguments are not valid for the list view")
		}
	case "result":
		if len(input.ChildSessionIDs) != 0 {
			return errors.New("child_session_ids are only valid for the results and wait views")
		}
		if input.WaitMode != nil {
			return errors.New("wait_mode is only valid for the wait view")
		}
		parsed, err := uuid.Parse(input.ChildSessionID)
		if err != nil || parsed.String() != input.ChildSessionID {
			return errors.New("child_session_id must be a canonical UUID")
		}
		if input.GenerationID != nil {
			generation, err := uuid.Parse(*input.GenerationID)
			if err != nil || generation.String() != *input.GenerationID {
				return errors.New("generation_id must be a canonical UUID")
			}
		}
		if input.ContentOffset != nil && (*input.ContentOffset < 0 || *input.ContentOffset > 1000000) {
			return errors.New("content_offset is out of range")
		}
		if input.ContentLimit != nil && (*input.ContentLimit < 1 || *input.ContentLimit > 4000) {
			return errors.New("content_limit is out of range")
		}
		if input.WaitMS != nil {
			return errors.New("timeout_ms is not valid for the result view")
		}
		if input.ExpectedSHA256 != nil && !validAIChildContentSHA256(*input.ExpectedSHA256) {
			return errors.New("expected_content_sha256 must be a lowercase SHA-256 digest")
		}
		if input.ContentOffset != nil && *input.ContentOffset > 0 && (input.ExpectedSHA256 == nil || input.GenerationID == nil) {
			return errors.New("continued child reply pages require generation_id and expected_content_sha256")
		}
	case "results":
		if input.ChildSessionID != "" || input.GenerationID != nil || input.ContentOffset != nil || input.ExpectedSHA256 != nil || input.WaitMS != nil || input.WaitMode != nil {
			return errors.New("results accepts only child_session_ids and content_limit")
		}
		if len(input.ChildSessionIDs) == 0 || len(input.ChildSessionIDs) > aiAgentDelegationMaxCount {
			return errors.New("results requires one to four confirmed child sessions")
		}
		if input.ContentLimit != nil && (*input.ContentLimit < 1 || *input.ContentLimit > 1000) {
			return errors.New("results content_limit must be between 1 and 1000")
		}
		seen := make(map[string]struct{}, len(input.ChildSessionIDs))
		for _, id := range input.ChildSessionIDs {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return errors.New("child session ids must be canonical UUIDs")
			}
			if _, exists := seen[id]; exists {
				return errors.New("child session ids must be unique")
			}
			seen[id] = struct{}{}
		}
	case "wait":
		if input.ChildSessionID != "" && len(input.ChildSessionIDs) != 0 {
			return errors.New("use child_session_id or child_session_ids, not both")
		}
		ids := input.waitChildIDs()
		if len(ids) == 0 || len(ids) > aiAgentDelegationMaxCount {
			return errors.New("wait requires one to four confirmed child sessions")
		}
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return errors.New("child session ids must be canonical UUIDs")
			}
			if _, exists := seen[id]; exists {
				return errors.New("child session ids must be unique")
			}
			seen[id] = struct{}{}
		}
		if input.GenerationID != nil || input.ContentOffset != nil || input.ContentLimit != nil || input.ExpectedSHA256 != nil {
			return errors.New("content pagination is not valid for the wait view")
		}
		if input.WaitMS != nil && (*input.WaitMS < 1 || *input.WaitMS > 15000) {
			return errors.New("timeout_ms is out of range")
		}
		if input.WaitMode != nil && *input.WaitMode != "any" && *input.WaitMode != "all" {
			return errors.New("wait_mode must be any or all")
		}
	default:
		return errors.New("view must be list, result, results or wait")
	}
	return nil
}

func (input aiAgentChildrenInput) waitChildIDs() []string {
	if input.ChildSessionID != "" {
		return []string{input.ChildSessionID}
	}
	return slices.Clone(input.ChildSessionIDs)
}

func (input aiAgentChildrenInput) waitMode() string {
	if input.WaitMode == nil {
		return "any"
	}
	return *input.WaitMode
}

func validAIChildContentSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return false
		}
	}
	return true
}

type aiAgentChildReply struct {
	Generation models.AIGeneration
	Kind       string
	ProposalID string
	Grant      *aiAgentDelegationPreview
}

func readAIAgentChildReply(tx *gorm.DB, parentSessionID string, child aiAgentFamilyMember, spawn models.AIActionProposal, original *aiAgentDelegationPreview, requestedGenerationID *string) (aiAgentChildReply, error) {
	readSpawn := func() (aiAgentChildReply, error) {
		var generation models.AIGeneration
		if err := tx.Select("id,session_id,provider_id,status,content").Take(&generation, "id=?", spawn.ID).Error; err != nil {
			return aiAgentChildReply{}, aiAgentChildrenReadError("child generation result is unavailable")
		}
		return aiAgentChildReply{Generation: generation, Kind: "spawn", ProposalID: spawn.ID, Grant: original}, nil
	}
	if requestedGenerationID != nil && *requestedGenerationID == spawn.ID {
		return readSpawn()
	}

	query := tx.Select("id,generation_id,action_json,preview_json,result_id,result_version,status").Where(
		"status='confirmed' AND result_id=? AND result_version=1 AND json_extract(action_json,'$.action')=? AND generation_id IN (SELECT id FROM ai_generations WHERE session_id=?)",
		child.SessionID, aiAgentDelegateFollowupAction, parentSessionID,
	)
	if requestedGenerationID != nil {
		query = query.Where("id=?", *requestedGenerationID)
	} else {
		query = query.Order("rowid DESC").Limit(1)
	}
	var proposal models.AIActionProposal
	err := query.Take(&proposal).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && requestedGenerationID == nil {
		return readSpawn()
	}
	if err != nil {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child reply generation is not authorized by this conversation")
	}
	action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
	if err != nil || action.Action != aiAgentDelegateFollowupAction {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up approval could not be verified")
	}
	var changes aiAgentFollowupChanges
	if err := decodeStrictToolArguments(action.Changes, &changes); err != nil {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up approval could not be verified")
	}
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(proposal.PreviewJSON), &preview); err != nil || preview.AgentFollowup == nil {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up approval could not be verified")
	}
	followup := preview.AgentFollowup
	target := followup.Target
	if target.ParentSessionID != parentSessionID || target.ChildSessionID != child.SessionID || target.SpawnProposalID != spawn.ID || target.ProviderID != original.ProviderID || target.ProviderVersion != original.ProviderVersion || target.ConfigVersion != original.ConfigVersion || target.PreviousGeneration == "" || target.ChildVersion < 1 ||
		changes.ChildSessionID != child.SessionID || changes.Message != followup.Message || !slices.Equal(changes.Scopes, followup.Scopes) || followup.TaskName != original.TaskName || followup.ProviderName != original.ProviderName || followup.Model != original.Model || followup.LeavesDevice != original.LeavesDevice {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up approval does not match its original delegation")
	}
	for _, scope := range followup.Scopes {
		if !slices.Contains(original.Scopes, scope) || scope == "agent_files" || scope == "agent_project_files" {
			return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up scope exceeds its original delegation")
		}
	}
	if slices.Contains(followup.Scopes, "knowledge") && !slices.Equal(followup.Knowledge, original.Knowledge) {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up knowledge sources do not match its original delegation")
	}
	var previous models.AIGeneration
	if err := tx.Select("id,session_id,provider_id,status").Take(&previous, "id=?", target.PreviousGeneration).Error; err != nil || previous.SessionID != child.SessionID || previous.ProviderID != target.ProviderID || previous.Status != "completed" {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up baseline could not be verified")
	}
	var generation models.AIGeneration
	if err := tx.Select("id,session_id,provider_id,status,content").Take(&generation, "id=?", proposal.ID).Error; err != nil {
		return aiAgentChildReply{}, aiAgentChildrenReadError("child follow-up generation result is unavailable")
	}
	grant := &aiAgentDelegationPreview{ProviderID: target.ProviderID, ProviderVersion: target.ProviderVersion, ConfigVersion: target.ConfigVersion, Scopes: followup.Scopes, Knowledge: followup.Knowledge}
	return aiAgentChildReply{Generation: generation, Kind: "followup", ProposalID: proposal.ID, Grant: grant}, nil
}

func allowsAIAgentChildResult(current *aiWorkspaceGrant, providerID string, configVersion int64, frozen *aiAgentDelegationPreview) bool {
	if current == nil || frozen == nil || providerID != frozen.ProviderID || configVersion != frozen.ConfigVersion || slices.Contains(frozen.Scopes, "agent_files") || slices.Contains(frozen.Scopes, "agent_project_files") {
		return false
	}
	child, err := delegatedAIWorkspaceGrant(current, frozen.Scopes)
	return err == nil && child.ProviderVersion == frozen.ProviderVersion && slices.Equal(child.Scopes, frozen.Scopes) && slices.Equal(child.KnowledgeSources, frozen.Knowledge)
}

func (t *aiWorkspaceTool) readAgentChildStatuses(ctx context.Context, childSessionIDs []string) ([]aiAgentFamilyMember, string, error) {
	children := make([]aiAgentFamilyMember, 0, len(childSessionIDs))
	var asOf string
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		family, err := readAIAgentFamily(tx, t.sessionID, t.api.options.Now(), t.api)
		if err != nil {
			return err
		}
		if family.Parent != nil {
			return harness.ErrPermissionDenied
		}
		byID := make(map[string]aiAgentFamilyMember, len(family.Children))
		for _, member := range family.Children {
			byID[member.SessionID] = member
		}
		for _, childSessionID := range childSessionIDs {
			member, ok := byID[childSessionID]
			if !ok {
				return aiAgentChildrenReadError("not a confirmed child of this conversation")
			}
			children = append(children, member)
		}
		asOf = family.AsOf
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	return children, asOf, err
}

// Wait subscribes before its first read so a just-committed child fact cannot
// be lost between the initial snapshot and sleeping. Wakeups are only hints:
// every observation revalidates the confirmed parent/child relationship in a
// fresh read-only transaction. A low-frequency poll remains a recovery path
// for facts that do not have an in-process notifier, such as external deletion.
func (t *aiWorkspaceTool) waitAgentChild(ctx context.Context, input aiAgentChildrenInput) (any, error) {
	childSessionIDs := input.waitChildIDs()
	plural := input.ChildSessionID == ""
	waitMode := input.waitMode()
	timeout := 10 * time.Second
	if input.WaitMS != nil {
		timeout = time.Duration(*input.WaitMS) * time.Millisecond
	}
	var factWake <-chan struct{}
	unsubscribe := func() {}
	if t.api != nil && t.api.aiContinuations != nil {
		factWake, unsubscribe = t.api.aiContinuations.subscribeFactWake()
	}
	defer unsubscribe()
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(aiAgentChildWaitFallbackInterval)
	defer timer.Stop()
	defer ticker.Stop()
	expired := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		children, asOf, err := t.readAgentChildStatuses(ctx, childSessionIDs)
		if err != nil {
			var readErr aiAgentChildrenReadError
			if errors.As(err, &readErr) || errors.Is(err, harness.ErrPermissionDenied) {
				return nil, err
			}
			return nil, safeAIWorkspaceError(ctx, err)
		}
		terminalIDs := make([]string, 0, len(children))
		for _, child := range children {
			if aiAgentChildStatusTerminal(child.Status) {
				terminalIDs = append(terminalIDs, child.SessionID)
			}
		}
		satisfied := len(terminalIDs) > 0
		if waitMode == "all" {
			satisfied = len(terminalIDs) == len(children)
		}
		if satisfied || expired {
			if plural {
				projected := make([]map[string]any, 0, len(children))
				for _, child := range children {
					projected = append(projected, aiAgentChildWaitProjection(child))
				}
				return map[string]any{
					"view": "wait", "session_id": t.sessionID, "wait_mode": waitMode,
					"children": projected, "terminal_child_session_ids": terminalIDs,
					"as_of": asOf, "timed_out": !satisfied,
				}, nil
			}
			result := aiAgentChildWaitProjection(children[0])
			result["view"] = "wait"
			result["session_id"] = t.sessionID
			result["as_of"] = asOf
			result["timed_out"] = !satisfied
			return result, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			expired = true
		case <-factWake:
		case <-ticker.C:
		}
	}
}

func aiAgentChildStatusTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled" || status == "unavailable"
}

func aiAgentChildWaitProjection(child aiAgentFamilyMember) map[string]any {
	result := map[string]any{
		"child_session_id": child.SessionID, "proposal_id": child.ProposalID,
		"task_name": child.TaskName, "status": child.Status,
		"error_code": child.ErrorCode, "available": child.Available,
		"pending_approvals": child.PendingApprovals,
	}
	if child.Available {
		result["route"] = "/ai?session=" + child.SessionID
	}
	return result
}

const aiAgentChildrenResultsMaxContentBytes = 8 << 10

func addAIAgentChildResultBytes(current int, content string) (int, error) {
	if current < 0 || len(content) > aiAgentChildrenResultsMaxContentBytes-current {
		return 0, aiAgentChildrenReadError("combined child reply pages exceed the bounded result limit; reduce content_limit or read fewer children")
	}
	return current + len(content), nil
}

func (t *aiWorkspaceTool) readAgentChildResultPage(tx *gorm.DB, family aiAgentFamilyView, child aiAgentFamilyMember, requestedGenerationID, expectedSHA256 *string, requestedOffset, requestedLimit int) (map[string]any, error) {
	if !child.Available || child.Status != "completed" {
		return nil, aiAgentChildrenReadError("child reply is not available until its generation completes")
	}
	var proposal models.AIActionProposal
	if err := tx.Take(&proposal, "id=? AND status='confirmed' AND result_id=?", child.ProposalID, child.SessionID).Error; err != nil {
		return nil, aiAgentChildrenReadError("child approval is unavailable")
	}
	verified, err := projectAIAgentFamilyMember(tx, proposal, t.sessionID, t.api.options.Now(), t.api)
	if err != nil || verified.SessionID != child.SessionID || verified.ProposalID != child.ProposalID || verified.Status != "completed" {
		return nil, aiAgentChildrenReadError("child approval history could not be verified")
	}
	var preview aiActionPreview
	if json.Unmarshal([]byte(proposal.PreviewJSON), &preview) != nil || preview.AgentDelegation == nil {
		return nil, aiAgentChildrenReadError("child approval history could not be verified")
	}
	reply, err := readAIAgentChildReply(tx, t.sessionID, child, proposal, preview.AgentDelegation, requestedGenerationID)
	if err != nil {
		return nil, err
	}
	if !allowsAIAgentChildResult(t.delegationGrant, t.providerID, t.configVersion, reply.Grant) {
		return nil, harness.ErrPermissionDenied
	}
	generation := reply.Generation
	if generation.SessionID != child.SessionID || generation.ProviderID != reply.Grant.ProviderID || generation.Status != "completed" || generation.Content == nil {
		return nil, aiAgentChildrenReadError("child generation result is inconsistent")
	}
	var messages []models.AIMessage
	if err := tx.Select("id,session_id,role,status,content,generation_id").Where("generation_id=? AND role='assistant'", generation.ID).Find(&messages).Error; err != nil {
		return nil, err
	}
	if len(messages) != 1 || messages[0].SessionID != child.SessionID || messages[0].Status != "completed" || messages[0].Content != *generation.Content {
		return nil, aiAgentChildrenReadError("child generation message is inconsistent")
	}
	contentSHA256 := sha256Hex([]byte(*generation.Content))
	if expectedSHA256 != nil && *expectedSHA256 != contentSHA256 {
		return nil, aiAgentChildrenReadError("child reply changed; restart from the first page")
	}
	runes := []rune(*generation.Content)
	if requestedOffset < 0 || requestedOffset > len(runes) {
		return nil, aiAgentChildrenReadError("content_offset exceeds the child reply")
	}
	if requestedLimit < 1 {
		requestedLimit = 4000
	}
	end := min(requestedOffset+requestedLimit, len(runes))
	var next *int
	if end < len(runes) {
		next = &end
	}
	return map[string]any{
		"view": "result", "session_id": t.sessionID, "child_session_id": child.SessionID,
		"proposal_id": child.ProposalID, "task_name": child.TaskName, "status": "completed",
		"pending_approvals": child.PendingApprovals,
		"generation_id":     generation.ID, "reply_kind": reply.Kind, "reply_proposal_id": reply.ProposalID, "content_sha256": contentSHA256,
		"content": string(runes[requestedOffset:end]), "content_offset": requestedOffset, "next_content_offset": next,
		"route": "/ai?session=" + child.SessionID, "as_of": family.AsOf,
		"content_trust": "untrusted_child_reply",
	}, nil
}

func (t *aiWorkspaceTool) readAgentChildResults(ctx context.Context, input aiAgentChildrenInput) (any, error) {
	limit := 1000
	if input.ContentLimit != nil {
		limit = *input.ContentLimit
	}
	var result any
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		family, err := readAIAgentFamily(tx, t.sessionID, t.api.options.Now(), t.api)
		if err != nil {
			return err
		}
		if family.Parent != nil {
			return harness.ErrPermissionDenied
		}
		childrenByID := make(map[string]aiAgentFamilyMember, len(family.Children))
		for _, child := range family.Children {
			childrenByID[child.SessionID] = child
		}
		children := make([]map[string]any, 0, len(input.ChildSessionIDs))
		totalContentBytes := 0
		for _, childID := range input.ChildSessionIDs {
			child, ok := childrenByID[childID]
			if !ok {
				return aiAgentChildrenReadError("not a confirmed child of this conversation")
			}
			page, err := t.readAgentChildResultPage(tx, family, child, nil, nil, 0, limit)
			if err != nil {
				return err
			}
			pageContent, _ := page["content"].(string)
			totalContentBytes, err = addAIAgentChildResultBytes(totalContentBytes, pageContent)
			if err != nil {
				return err
			}
			children = append(children, page)
		}
		result = map[string]any{"view": "results", "session_id": t.sessionID, "children": children, "as_of": family.AsOf}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		var readErr aiAgentChildrenReadError
		if errors.As(err, &readErr) || errors.Is(err, harness.ErrPermissionDenied) {
			return nil, err
		}
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

// The parent identity and per-message grant are captured by the registry, not
// supplied by the model. List never discloses reply text; result requires a
// fresh grant covering the selected parent-authorized reply's exact scopes and sources.
func (t *aiWorkspaceTool) agentChildren(ctx context.Context, arguments json.RawMessage) (any, error) {
	if t.sessionID == "" || t.delegationGrant == nil || !t.policy.Allows("work") || !t.policy.Allows("outputs") || !t.policy.Allows("actions") || !t.policy.Allows("agent_execution") {
		return nil, harness.ErrPermissionDenied
	}
	if _, err := decodeAIReadQueryObject(arguments, map[string]bool{"view": true, "child_session_id": true, "child_session_ids": true, "generation_id": true, "content_offset": true, "content_limit": true, "expected_content_sha256": true, "timeout_ms": true, "wait_mode": true}); err != nil {
		return nil, err
	}
	var input aiAgentChildrenInput
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	if err := input.validate(); err != nil {
		return nil, err
	}
	if input.View == "wait" {
		return t.waitAgentChild(ctx, input)
	}
	if input.View == "results" {
		return t.readAgentChildResults(ctx, input)
	}
	var result any
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		family, err := readAIAgentFamily(tx, t.sessionID, t.api.options.Now(), t.api)
		if err != nil {
			return err
		}
		if family.Parent != nil {
			return harness.ErrPermissionDenied
		}
		if input.View == "list" {
			children := append([]aiAgentFamilyMember(nil), family.Children...)
			for index := range children {
				children[index].ActiveGenerationID = nil
			}
			result = map[string]any{"view": "list", "session_id": t.sessionID, "children": children, "as_of": family.AsOf}
			return nil
		}
		var child *aiAgentFamilyMember
		for index := range family.Children {
			if family.Children[index].SessionID == input.ChildSessionID {
				child = &family.Children[index]
				break
			}
		}
		if child == nil {
			return aiAgentChildrenReadError("not a confirmed child of this conversation")
		}
		offset := 0
		if input.ContentOffset != nil {
			offset = *input.ContentOffset
		}
		limit := 4000
		if input.ContentLimit != nil {
			limit = *input.ContentLimit
		}
		page, err := t.readAgentChildResultPage(tx, family, *child, input.GenerationID, input.ExpectedSHA256, offset, limit)
		if err != nil {
			return err
		}
		result = page
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		var readErr aiAgentChildrenReadError
		if errors.As(err, &readErr) || errors.Is(err, harness.ErrPermissionDenied) {
			return nil, err
		}
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
