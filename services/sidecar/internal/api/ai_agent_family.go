package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiAgentFamilyMember struct {
	SessionID          string  `json:"session_id"`
	ProposalID         string  `json:"proposal_id"`
	TaskName           string  `json:"task_name"`
	Status             string  `json:"status"`
	ErrorCode          *string `json:"error_code"`
	Available          bool    `json:"available"`
	PendingApprovals   int     `json:"pending_approvals"`
	ActiveGenerationID *string `json:"active_generation_id"`
	CreatedAt          string  `json:"created_at"`
	DecidedAt          string  `json:"decided_at"`
}

type aiAgentFamilyView struct {
	SessionID string                `json:"session_id"`
	Parent    *aiAgentFamilyMember  `json:"parent"`
	Children  []aiAgentFamilyMember `json:"children"`
	AsOf      string                `json:"as_of"`
}

// This is a metadata-only navigation projection. A confirmed approval is not
// a child result; the actual child generation remains the status authority.
func (a *API) getAIAgentFamily(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(sessionID)
	if err != nil || parsed.String() != sessionID {
		writeError(c, http.StatusBadRequest, "INVALID_AI_SESSION_ID", "AI session id must be a canonical UUID")
		return
	}
	var view aiAgentFamilyView
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var readErr error
		view, readErr = readAIAgentFamily(tx, sessionID, a.options.Now(), a)
		return readErr
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// cancelAIAgentChildGeneration accepts a user-requested stop only for the
// exact live or pre-acceptance generation attempt authorized by this saved
// parent session. It re-reads the relationship immediately before signaling
// the in-process generation registry or delegation coordinator.
func (a *API) cancelAIAgentChildGeneration(c *gin.Context) {
	parentSessionID := strings.TrimSpace(c.Param("id"))
	childSessionID := strings.TrimSpace(c.Param("child_id"))
	generationID := strings.TrimSpace(c.Param("generation_id"))
	for _, identity := range []struct {
		value string
		code  string
		name  string
	}{
		{parentSessionID, "INVALID_AI_SESSION_ID", "AI session id"},
		{childSessionID, "INVALID_AI_CHILD_SESSION_ID", "AI child session id"},
		{generationID, "INVALID_AI_GENERATION_ID", "AI generation id"},
	} {
		parsed, err := uuid.Parse(identity.value)
		if err != nil || parsed.String() != identity.value {
			writeError(c, http.StatusBadRequest, identity.code, identity.name+" must be a canonical UUID")
			return
		}
	}

	var authorized bool
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		family, err := readAIAgentFamily(tx, parentSessionID, a.options.Now(), a)
		if err != nil {
			return err
		}
		if family.Parent != nil {
			return newProjectRequestError(http.StatusForbidden, "AI_AGENT_CHILD_CANCEL_FORBIDDEN", "Only a parent conversation can request a child stop")
		}
		for _, child := range family.Children {
			if child.SessionID == childSessionID && child.ActiveGenerationID != nil && *child.ActiveGenerationID == generationID {
				authorized = true
				return nil
			}
		}
		return newProjectRequestError(http.StatusNotFound, "AI_AGENT_CHILD_GENERATION_NOT_FOUND", "No active parent-authorized child generation was found")
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	if !authorized {
		writeError(c, http.StatusNotFound, "AI_AGENT_CHILD_GENERATION_NOT_FOUND", "No active parent-authorized child generation was found")
		return
	}

	writeAccepted := func() {
		c.JSON(http.StatusAccepted, gin.H{"data": gin.H{
			"parent_session_id": parentSessionID,
			"child_session_id":  childSessionID,
			"generation_id":     generationID,
			"cancel_requested":  true,
		}})
	}
	var generation models.AIGeneration
	err = a.db.WithContext(c.Request.Context()).Select("id,status").Take(&generation, "id=?", generationID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		writeDatabaseError(c)
		return
	}
	if err == nil {
		if generation.Status != "queued" && generation.Status != "streaming" {
			writeError(c, http.StatusConflict, "AI_GENERATION_ALREADY_TERMINAL", "This generation already reached a terminal state")
			return
		}
		if err := a.stopAIContinuationForGeneration(generationID); err != nil {
			writeDatabaseError(c)
			return
		}
	}
	stopped, err := a.requestAIAgentChildStopsAtomically(
		[]aiAgentChildCancelItem{{ChildSessionID: childSessionID, GenerationID: generationID}},
		requestIDFromContext(c),
	)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if stopped {
		writeAccepted()
		return
	}
	writeError(c, http.StatusConflict, "AI_GENERATION_NOT_ACTIVE", "This delegated generation is not cancellable in this Sidecar process")
}

func readAIAgentFamily(tx *gorm.DB, sessionID string, now time.Time, caller ...*API) (aiAgentFamilyView, error) {
	view := aiAgentFamilyView{SessionID: sessionID, Children: []aiAgentFamilyMember{}, AsOf: now.UTC().Format(time.RFC3339Nano)}
	var session models.AISession
	if err := tx.Select("id,persist").Take(&session, "id=?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return view, newProjectRequestError(http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
		}
		return view, err
	}
	if !session.Persist {
		return view, newProjectRequestError(http.StatusConflict, "AI_SESSION_NOT_SAVED", "Agent family requires a saved conversation")
	}
	var proposals []aiSessionDelegatedProposal
	if err := tx.Table("ai_action_proposals AS proposal").
		Select("proposal.*, generation.session_id AS session_id").
		Joins("JOIN ai_generations AS generation ON generation.id=proposal.generation_id").
		Where("proposal.status='confirmed' AND json_extract(proposal.action_json,'$.action')=?", aiAgentDelegateSpawnAction).
		Where("generation.session_id=? OR proposal.result_id=?", sessionID, sessionID).
		Order("proposal.created_at ASC").Order("proposal.id ASC").Find(&proposals).Error; err != nil {
		return view, err
	}
	seenChildren := map[string]bool{}
	for _, proposal := range proposals {
		member, err := projectAIAgentFamilyMember(tx, proposal.AIActionProposal, proposal.SessionID, now, caller...)
		if err != nil {
			return view, newProjectRequestError(http.StatusConflict, "AI_AGENT_FAMILY_INVALID", "Agent family history could not be verified")
		}
		if proposal.SessionID == sessionID {
			if seenChildren[member.SessionID] || len(view.Children) >= aiAgentDelegationMaxCount {
				return view, newProjectRequestError(http.StatusConflict, "AI_AGENT_FAMILY_INVALID", "Agent family history could not be verified")
			}
			seenChildren[member.SessionID] = true
			view.Children = append(view.Children, member)
		} else if proposal.ResultID != nil && *proposal.ResultID == sessionID {
			if view.Parent != nil {
				return view, newProjectRequestError(http.StatusConflict, "AI_AGENT_FAMILY_INVALID", "Agent family history could not be verified")
			}
			member.SessionID = proposal.SessionID
			member.Available = true     // joined parent generation belongs to an existing session
			member.PendingApprovals = 0 // the parent navigation row does not expose child work
			member.ActiveGenerationID = nil
			view.Parent = &member
		} else {
			return view, newProjectRequestError(http.StatusConflict, "AI_AGENT_FAMILY_INVALID", "Agent family history could not be verified")
		}
	}
	return view, nil
}

func projectAIAgentFamilyMember(tx *gorm.DB, proposal models.AIActionProposal, parentSessionID string, now time.Time, caller ...*API) (aiAgentFamilyMember, error) {
	action, err := strictAIWorkPlanAction(proposal)
	if err != nil || action.Action != aiAgentDelegateSpawnAction || proposal.ResultID == nil || proposal.ResultVersion == nil || *proposal.ResultVersion != 1 || proposal.DecidedAt == nil {
		return aiAgentFamilyMember{}, errors.New("invalid Agent family proposal")
	}
	var changes aiAgentDelegationChanges
	var preview aiActionPreview
	if json.Unmarshal(action.Changes, &changes) != nil || json.Unmarshal([]byte(proposal.PreviewJSON), &preview) != nil || preview.AgentDelegation == nil {
		return aiAgentFamilyMember{}, errors.New("invalid Agent family preview")
	}
	frozen := preview.AgentDelegation
	if frozen.ParentSessionID != parentSessionID || frozen.ParentGenerationID != proposal.GenerationID || frozen.ChildSessionID != *proposal.ResultID || frozen.TaskName != changes.TaskName || frozen.Message != changes.Message || !slices.Equal(frozen.Scopes, changes.Scopes) || frozen.Depth != 1 || frozen.MaxDepth != 1 || frozen.MaxChildren != aiAgentDelegationMaxCount || frozen.SiblingIndex < 1 || frozen.SiblingIndex > aiAgentDelegationMaxCount || !aiAgentDelegationTaskNamePattern.MatchString(changes.TaskName) {
		return aiAgentFamilyMember{}, errors.New("inconsistent Agent family preview")
	}
	for _, id := range []string{parentSessionID, frozen.ChildSessionID, frozen.ProviderID, proposal.ID} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return aiAgentFamilyMember{}, errors.New("invalid Agent family identity")
		}
	}
	created, createdErr := time.Parse(time.RFC3339Nano, proposal.CreatedAt)
	decided, decidedErr := time.Parse(time.RFC3339Nano, *proposal.DecidedAt)
	if createdErr != nil || decidedErr != nil {
		return aiAgentFamilyMember{}, errors.New("invalid Agent family timestamp")
	}
	member := aiAgentFamilyMember{SessionID: frozen.ChildSessionID, ProposalID: proposal.ID, TaskName: changes.TaskName,
		Status: "unavailable", CreatedAt: created.UTC().Format(time.RFC3339Nano), DecidedAt: decided.UTC().Format(time.RFC3339Nano)}
	response := aiActionResponse{ID: proposal.ID, ResultID: proposal.ResultID, ResultVersion: proposal.ResultVersion, Preview: preview}
	result, err := readAIAgentDelegationResult(tx, response, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return member, nil
	}
	if err != nil || result.SessionID != member.SessionID || result.GenerationID != proposal.ID {
		return aiAgentFamilyMember{}, errors.New("invalid Agent family generation")
	}
	member.Available = true
	member.Status = result.Status
	member.ErrorCode = result.ErrorCode
	member.PendingApprovals = result.PendingApprovals
	if len(caller) > 0 && caller[0] != nil {
		if err := projectAIAgentFamilyCurrentGeneration(tx, caller[0], parentSessionID, proposal, frozen, &member); err != nil {
			return aiAgentFamilyMember{}, err
		}
	}
	return member, nil
}

func projectAIAgentFamilyCurrentGeneration(tx *gorm.DB, a *API, parentSessionID string, spawn models.AIActionProposal, original *aiAgentDelegationPreview, member *aiAgentFamilyMember) error {
	if a == nil || original == nil || member == nil {
		return errors.New("invalid Agent family generation projection")
	}
	var followup models.AIActionProposal
	err := tx.Table("ai_action_proposals AS p").
		Select("p.*").
		Joins("JOIN ai_generations AS parent_generation ON parent_generation.id=p.generation_id").
		Where("p.status='confirmed' AND p.result_id=? AND p.result_version=1 AND json_extract(p.action_json,'$.action')=? AND parent_generation.session_id=?", member.SessionID, aiAgentDelegateFollowupAction, parentSessionID).
		Order("p.rowid DESC").Take(&followup).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		member.ActiveGenerationID = nil
		var generation models.AIGeneration
		err = tx.Select("id,session_id,provider_id,status,error_code").Take(&generation, "id=?", spawn.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if member.Status == "queued" {
				if err := a.guardAIDelegationGeneration(tx, member.SessionID, aiGenerationRunOptions{GenerationID: spawn.ID, DelegationProposalID: spawn.ID, Headless: true}); err != nil {
					return errors.New("queued child generation could not be verified")
				}
				if a.aiDelegations.hasPendingWorker(spawn.ID) {
					id := spawn.ID
					member.ActiveGenerationID = &id
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
		reply, err := readAIAgentChildReply(tx, parentSessionID, *member, spawn, original, &spawn.ID)
		if err != nil || reply.Generation.ID != generation.ID || reply.Generation.SessionID != member.SessionID || reply.Generation.ProviderID != original.ProviderID {
			return errors.New("child generation could not be verified")
		}
		member.Status = generation.Status
		member.ErrorCode = generation.ErrorCode
		if generation.Status == "queued" || generation.Status == "streaming" {
			id := generation.ID
			member.ActiveGenerationID = &id
		}
		return nil
	}

	id := followup.ID
	var generation models.AIGeneration
	err = tx.Select("id,session_id,provider_id,status,error_code").Take(&generation, "id=?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := a.guardAIDelegationGeneration(tx, member.SessionID, aiGenerationRunOptions{GenerationID: id, DelegationFollowupID: id, Headless: true}); err != nil {
			return errors.New("queued child follow-up could not be verified")
		}
		member.Status = "queued"
		member.ErrorCode = nil
		member.ActiveGenerationID = nil
		if a.aiDelegations.hasPendingWorker(id) {
			member.ActiveGenerationID = &id
		}
		return nil
	}
	if err != nil {
		return err
	}
	var reply aiAgentChildReply
	reply, err = readAIAgentChildReply(tx, parentSessionID, *member, spawn, original, &id)
	if err != nil || reply.Generation.ID != generation.ID || reply.Generation.SessionID != member.SessionID || reply.Generation.ProviderID != original.ProviderID {
		return errors.New("child follow-up generation could not be verified")
	}
	member.Status = generation.Status
	member.ErrorCode = generation.ErrorCode
	member.ActiveGenerationID = nil
	if generation.Status == "queued" || generation.Status == "streaming" {
		member.ActiveGenerationID = &id
	}
	return nil
}

func countAIAgentChildPendingApprovals(tx *gorm.DB, childSessionID string, now time.Time) (int, error) {
	var pending []struct{ CreatedAt string }
	if err := tx.Table("ai_action_proposals AS p").
		Select("p.created_at").
		Joins("JOIN ai_generations AS g ON g.id=p.generation_id").
		Where("g.session_id=? AND g.status='completed' AND p.status='pending'", childSessionID).
		Find(&pending).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, row := range pending {
		created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
		if err != nil {
			return 0, errors.New("invalid child approval timestamp")
		}
		if now.Sub(created) <= 24*time.Hour {
			count++
		}
	}
	return count, nil
}
