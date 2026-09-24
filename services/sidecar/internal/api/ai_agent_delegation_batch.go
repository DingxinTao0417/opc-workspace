package api

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxAIAgentDelegationConfirmBatchSize = aiAgentDelegationMaxCount
	maxAIAgentDelegationConfirmBodyBytes = 4096
)

type aiAgentDelegationConfirmBatchItem struct {
	ProposalID  string `json:"proposal_id"`
	Fingerprint string `json:"fingerprint"`
}

// confirmAIAgentDelegationBatch consumes an exact, user-reviewed set of child
// proposals atomically. All previews are revalidated before any child session
// is created; model work is launched only after the transaction commits.
func (a *API) confirmAIAgentDelegationBatch(c *gin.Context) {
	generationID := c.Param("id")
	parsedGenerationID, err := uuid.Parse(generationID)
	if err != nil || parsedGenerationID.String() != generationID {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a canonical UUID")
		return
	}
	items, err := decodeAIAgentDelegationConfirmBatch(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "AI_DELEGATION_BATCH_INVALID", "The selected child delegations are invalid")
		return
	}

	outputs := make([]aiActionResponse, len(items))
	launches := make([]aiAgentDelegationLaunch, 0, len(items))
	var reservation *aiDelegationReservation
	defer func() {
		if reservation != nil && a.aiDelegations != nil {
			a.aiDelegations.releaseReservation(reservation)
		}
	}()
	decisionChanged := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var generation models.AIGeneration
		if err := tx.Take(&generation, "id=?", generationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "AI_GENERATION_NOT_FOUND", "Parent generation not found")
			}
			return err
		}
		if generation.Status != "completed" {
			return newProjectRequestError(http.StatusConflict, "AI_ACTION_NOT_CONFIRMABLE", "The parent generation is unfinished or unavailable")
		}

		now := a.options.Now().UTC()
		stamp := now.Format(time.RFC3339Nano)
		candidates := make([]aiAgentDelegationConfirmCandidate, len(items))
		confirmed := 0
		parentSessionID := ""
		for index, item := range items {
			var proposal models.AIActionProposal
			if err := tx.Take(&proposal, "id=?", item.ProposalID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return newProjectRequestError(http.StatusNotFound, "AI_ACTION_NOT_FOUND", "A selected child proposal no longer exists")
				}
				return err
			}
			if proposal.GenerationID != generationID || proposal.Fingerprint != item.Fingerprint {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_CHANGED", "A selected child proposal changed; reload the complete group")
			}
			out, err := aiActionOutputWithFacts(tx, proposal, generation.Status, now)
			if err != nil {
				return err
			}
			if out.Action.Action != aiAgentDelegateSpawnAction || out.Preview.AgentDelegation == nil || out.Preview.AgentDelegation.ParentGenerationID != generationID {
				return newProjectRequestError(http.StatusConflict, "AI_DELEGATION_BATCH_INVALID", "Every selected proposal must be a child spawn from this generation")
			}
			if parentSessionID == "" {
				parentSessionID = out.Preview.AgentDelegation.ParentSessionID
			} else if parentSessionID != out.Preview.AgentDelegation.ParentSessionID {
				return newProjectRequestError(http.StatusConflict, "AI_DELEGATION_BATCH_INVALID", "Selected child proposals do not share one parent conversation")
			}
			if proposal.Status == "confirmed" {
				confirmed++
				outputs[index] = out
				continue
			}
			if proposal.Status != "pending" {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_ALREADY_DECIDED", "A selected child proposal is no longer pending")
			}
			if !out.CanConfirm {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_NOT_CONFIRMABLE", "A selected child proposal is unfinished, expired or unavailable")
			}

			action, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON))
			if err != nil || action.Action != aiAgentDelegateSpawnAction {
				return newProjectRequestError(http.StatusConflict, "AI_DELEGATION_BATCH_INVALID", "A selected child proposal is invalid")
			}
			preview, err := previewAIAgentDelegationFromStored(tx, action, out.Preview.AgentDelegation)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(preview)
			if err != nil {
				return err
			}
			if string(encoded) != proposal.PreviewJSON {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED", "A child instruction, permission or Provider changed; reload the complete group")
			}
			candidates[index] = aiAgentDelegationConfirmCandidate{proposal: proposal, action: action, preview: preview.AgentDelegation}
		}

		// A replay is idempotent only for the exact complete set. Mixed pending
		// and confirmed state indicates a concurrent or differently scoped
		// decision and must not be silently completed as a partial batch.
		if confirmed != 0 {
			if confirmed != len(items) {
				return newProjectRequestError(http.StatusConflict, "AI_DELEGATION_BATCH_PARTIAL", "Some selected children were decided separately; refresh and review the remaining pending proposals")
			}
			return nil
		}

		// Revalidate every selected frozen preview before the first insert. This
		// prevents earlier child inserts from changing facts seen by later rows.
		for index, candidate := range candidates {
			result, launch, err := executeAIAgentDelegation(tx, candidate.proposal, candidate.action, candidate.preview, now)
			if err != nil {
				return err
			}
			if launch == nil {
				return errors.New("child delegation did not produce a launch record")
			}
			updated := tx.Model(&models.AIActionProposal{}).
				Where("id=? AND generation_id=? AND fingerprint=? AND status='pending'", candidate.proposal.ID, generationID, candidate.proposal.Fingerprint).
				Updates(map[string]any{"status": "confirmed", "decided_at": stamp, "result_id": result.ID, "result_version": result.Version})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_ALREADY_DECIDED", "A selected child proposal was decided concurrently")
			}
			candidate.proposal.Status = "confirmed"
			candidate.proposal.DecidedAt = &stamp
			candidate.proposal.ResultID = &result.ID
			candidate.proposal.ResultVersion = &result.Version
			if err := recordAIActionEvent(tx, candidate.proposal, "ai_workspace_action_confirmed", requestIDFromContext(c), stamp); err != nil {
				return err
			}
			if err := recordAIAgentDelegationLifecycleEvent(tx, candidate.proposal.ID, result.ID, aiDelegationQueuedEvent, requestIDFromContext(c), stamp); err != nil {
				return err
			}
			outputs[index], err = aiActionOutputWithFacts(tx, candidate.proposal, generation.Status, now)
			if err != nil {
				return err
			}
			launches = append(launches, *launch)
		}
		var reserveErr error
		reservation, reserveErr = a.reserveAIAgentDelegations(launches)
		if reserveErr != nil {
			return reserveErr
		}
		decisionChanged = true
		return nil
	})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	if decisionChanged {
		a.wakeAIContinuationsAfterFactCommit()
		if code := a.aiDelegations.launchReservedBatch(reservation, launches); code != "" {
			for _, launch := range launches {
				_ = recordAIDelegationLaunchFailure(a.db, launch, code, a.options.Now())
			}
		}
	}

	// Return a fresh ordered projection so a launch failure recorded just after
	// commit is not represented as a successfully queued child.
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var generation models.AIGeneration
		if err := tx.Select("id,status").Take(&generation, "id=?", generationID).Error; err != nil {
			return err
		}
		for index, item := range items {
			var proposal models.AIActionProposal
			if err := tx.Take(&proposal, "id=? AND generation_id=?", item.ProposalID, generationID).Error; err != nil {
				return err
			}
			if proposal.Fingerprint != item.Fingerprint || proposal.Status != "confirmed" {
				return newProjectRequestError(http.StatusConflict, "AI_ACTION_CHANGED", "The confirmed child set changed; reload its authoritative status")
			}
			out, err := aiActionOutputWithFacts(tx, proposal, generation.Status, a.options.Now())
			if err != nil {
				return err
			}
			outputs[index] = out
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": outputs})
}

type aiAgentDelegationConfirmCandidate struct {
	proposal models.AIActionProposal
	action   aiWorkspaceAction
	preview  *aiAgentDelegationPreview
}

func decodeAIAgentDelegationConfirmBatch(c *gin.Context) ([]aiAgentDelegationConfirmBatchItem, error) {
	if c.Request.Body == nil {
		return nil, errors.New("request body is required")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAIAgentDelegationConfirmBodyBytes))
	if err != nil {
		return nil, err
	}
	return parseAIAgentDelegationConfirmBatch(raw)
}

func parseAIAgentDelegationConfirmBatch(raw []byte) ([]aiAgentDelegationConfirmBatchItem, error) {
	if len(raw) > maxAIAgentDelegationConfirmBodyBytes {
		return nil, errors.New("request body exceeds the delegation batch limit")
	}
	fields, err := decodeUniqueAIAgentDelegationBatchObject(raw, map[string]bool{"items": true, "confirm_agent_delegations": true})
	if err != nil || len(fields) != 2 {
		return nil, errors.New("request must contain only items and explicit batch consent")
	}
	var consent bool
	if err := json.Unmarshal(fields["confirm_agent_delegations"], &consent); err != nil || !consent {
		return nil, errors.New("explicit consent to this selected batch is required")
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(fields["items"], &rawItems); err != nil || len(rawItems) < 2 || len(rawItems) > maxAIAgentDelegationConfirmBatchSize {
		return nil, errors.New("items must contain two to four child proposals")
	}
	items := make([]aiAgentDelegationConfirmBatchItem, 0, len(rawItems))
	seenIDs := make(map[string]bool, len(rawItems))
	seenFingerprints := make(map[string]bool, len(rawItems))
	for _, rawItem := range rawItems {
		itemFields, err := decodeUniqueAIAgentDelegationBatchObject(rawItem, map[string]bool{"proposal_id": true, "fingerprint": true})
		if err != nil || len(itemFields) != 2 {
			return nil, errors.New("each item must contain exactly one proposal id and fingerprint")
		}
		var item aiAgentDelegationConfirmBatchItem
		if err := json.Unmarshal(itemFields["proposal_id"], &item.ProposalID); err != nil {
			return nil, errors.New("proposal id must be a string")
		}
		if parsed, err := uuid.Parse(item.ProposalID); err != nil || parsed.String() != item.ProposalID {
			return nil, errors.New("proposal id must be a canonical UUID")
		}
		if err := json.Unmarshal(itemFields["fingerprint"], &item.Fingerprint); err != nil || len(item.Fingerprint) != 64 {
			return nil, errors.New("fingerprint must be a lowercase SHA-256 value")
		}
		fingerprint, err := hex.DecodeString(item.Fingerprint)
		if err != nil || hex.EncodeToString(fingerprint) != item.Fingerprint {
			return nil, errors.New("fingerprint must be a lowercase SHA-256 value")
		}
		if seenIDs[item.ProposalID] || seenFingerprints[item.Fingerprint] {
			return nil, errors.New("proposal ids and fingerprints must be unique")
		}
		seenIDs[item.ProposalID], seenFingerprints[item.Fingerprint] = true, true
		items = append(items, item)
	}
	return items, nil
}

func decodeUniqueAIAgentDelegationBatchObject(raw []byte, allowed map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("expected object")
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errors.New("object has unknown or duplicate keys")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errors.New("object fields must not be null")
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("invalid object")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("object has trailing data")
	}
	return fields, nil
}
