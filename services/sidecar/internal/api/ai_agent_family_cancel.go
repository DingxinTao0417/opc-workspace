package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxAIAgentChildCancelBatchSize = 4
	maxAIAgentChildCancelBodyBytes = 4096
)

type aiAgentChildCancelItem struct {
	ChildSessionID string `json:"child_session_id"`
	GenerationID   string `json:"generation_id"`
}

type aiAgentChildCancelBatchItem struct {
	aiAgentChildCancelItem
	CancelRequested bool `json:"cancel_requested"`
}

// cancelAIAgentChildGenerations accepts one user-confirmed stop request for
// exact child generations owned by this saved parent conversation. Every row
// is revalidated before any in-process worker is signaled; the final signal
// phase checks all workers under their registries' locks and is all-or-none.
func (a *API) cancelAIAgentChildGenerations(c *gin.Context) {
	parentSessionID := c.Param("id")
	parsed, err := uuid.Parse(parentSessionID)
	if err != nil || parsed.String() != parentSessionID {
		writeError(c, http.StatusBadRequest, "INVALID_AI_SESSION_ID", "AI session id must be a canonical UUID")
		return
	}
	items, err := decodeAIAgentChildCancelBatch(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "AI_AGENT_CHILD_CANCEL_BATCH_INVALID", "The selected child generations are invalid")
		return
	}

	var authorizedTargets []aiAgentChildCancelItem
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		family, err := readAIAgentFamily(tx, parentSessionID, a.options.Now(), a)
		if err != nil {
			return err
		}
		if family.Parent != nil {
			return newProjectRequestError(http.StatusForbidden, "AI_AGENT_CHILD_CANCEL_FORBIDDEN", "Only a parent conversation can request child stops")
		}
		children := make(map[string]aiAgentFamilyMember, len(family.Children))
		for _, child := range family.Children {
			children[child.SessionID] = child
		}
		for _, item := range items {
			child, ok := children[item.ChildSessionID]
			if !ok {
				return newProjectRequestError(http.StatusNotFound, "AI_AGENT_CHILD_GENERATION_NOT_FOUND", "A selected parent-authorized child generation was not found")
			}
			if child.ActiveGenerationID == nil || *child.ActiveGenerationID != item.GenerationID || (child.Status != "queued" && child.Status != "streaming") {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_CHILD_GENERATION_CHANGED", "A selected child generation is no longer the current active generation")
			}

			var generation models.AIGeneration
			err := tx.Select("id,session_id,status").Take(&generation, "id=?", item.GenerationID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// The family projection only exposes a missing row while the exact
				// confirmed spawn/follow-up proposal is queued in its coordinator.
				if child.Status != "queued" || a.aiDelegations == nil || !a.aiDelegations.hasPendingWorker(item.GenerationID) {
					return newProjectRequestError(http.StatusConflict, "AI_AGENT_CHILD_GENERATION_NOT_ACTIVE", "A selected child generation is not cancellable")
				}
			} else if err != nil {
				return err
			} else if generation.SessionID != item.ChildSessionID || (generation.Status != "queued" && generation.Status != "streaming") {
				return newProjectRequestError(http.StatusConflict, "AI_AGENT_CHILD_GENERATION_CHANGED", "A selected child generation is no longer active")
			}
			authorizedTargets = append(authorizedTargets, item)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	stopped, err := a.requestAIAgentChildStopsAtomically(authorizedTargets, requestIDFromContext(c))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if !stopped {
		writeError(c, http.StatusConflict, "AI_AGENT_CHILD_GENERATION_NOT_ACTIVE", "At least one selected child generation is no longer active; no stop request was sent")
		return
	}

	result := make([]aiAgentChildCancelBatchItem, len(authorizedTargets))
	for index, target := range authorizedTargets {
		result[index] = aiAgentChildCancelBatchItem{aiAgentChildCancelItem: target, CancelRequested: true}
	}
	c.JSON(http.StatusAccepted, gin.H{"data": gin.H{
		"parent_session_id": parentSessionID,
		"items":             result,
	}})
}

func decodeAIAgentChildCancelBatch(c *gin.Context) ([]aiAgentChildCancelItem, error) {
	if c.Request.Body == nil {
		return nil, errors.New("request body is required")
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAIAgentChildCancelBodyBytes))
	if err != nil {
		return nil, err
	}
	return parseAIAgentChildCancelBatch(raw)
}

func parseAIAgentChildCancelBatch(raw []byte) ([]aiAgentChildCancelItem, error) {
	if len(raw) > maxAIAgentChildCancelBodyBytes {
		return nil, errors.New("request body exceeds the batch limit")
	}
	fields, err := decodeUniqueAIChildCancelObject(raw, map[string]bool{"items": true})
	if err != nil || len(fields) != 1 {
		return nil, errors.New("request must contain only items")
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(fields["items"], &rawItems); err != nil || len(rawItems) == 0 || len(rawItems) > maxAIAgentChildCancelBatchSize {
		return nil, errors.New("items must contain one to four child generations")
	}
	items := make([]aiAgentChildCancelItem, 0, len(rawItems))
	seenChildren := make(map[string]bool, len(rawItems))
	seenGenerations := make(map[string]bool, len(rawItems))
	for _, rawItem := range rawItems {
		itemFields, err := decodeUniqueAIChildCancelObject(rawItem, map[string]bool{"child_session_id": true, "generation_id": true})
		if err != nil || len(itemFields) != 2 {
			return nil, errors.New("each item must contain exactly two unique identities")
		}
		var item aiAgentChildCancelItem
		if err := json.Unmarshal(itemFields["child_session_id"], &item.ChildSessionID); err != nil {
			return nil, errors.New("child session id must be a string")
		}
		if err := json.Unmarshal(itemFields["generation_id"], &item.GenerationID); err != nil {
			return nil, errors.New("generation id must be a string")
		}
		for _, identity := range []struct {
			value string
			name  string
		}{{item.ChildSessionID, "child session"}, {item.GenerationID, "generation"}} {
			parsed, err := uuid.Parse(identity.value)
			if err != nil || parsed.String() != identity.value {
				return nil, errors.New(identity.name + " id must be a canonical UUID")
			}
		}
		if seenChildren[item.ChildSessionID] || seenGenerations[item.GenerationID] {
			return nil, errors.New("child sessions and generations must be unique")
		}
		seenChildren[item.ChildSessionID] = true
		seenGenerations[item.GenerationID] = true
		items = append(items, item)
	}
	return items, nil
}

func decodeUniqueAIChildCancelObject(raw []byte, allowed map[string]bool) (map[string]json.RawMessage, error) {
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

// requestAIAgentChildStopsAtomically takes the generation registry before the
// delegation coordinator (the only lock order used here), verifies every
// exact owner before sending any signal, then cancels all eligible workers.
// Persisted generations are tied to the child session; pre-accept generations
// are tied to their still-live confirmed delegation worker.
func (a *API) requestAIAgentChildStopsAtomically(targets []aiAgentChildCancelItem, requestID string) (bool, error) {
	if a == nil || a.db == nil || a.aiGenerations == nil || len(targets) == 0 || len(targets) > maxAIAgentChildCancelBatchSize {
		return false, nil
	}
	registry := a.aiGenerations
	coordinator := a.aiDelegations
	registry.mu.Lock()
	if coordinator != nil {
		coordinator.mu.Lock()
	}
	defer func() {
		if coordinator != nil {
			coordinator.mu.Unlock()
		}
		registry.mu.Unlock()
	}()

	type activeTarget struct {
		generationCancel context.CancelFunc
		worker           *aiDelegationWorker
	}
	active := make([]activeTarget, len(targets))
	for index, target := range targets {
		generationCancel := registry.cancels[target.GenerationID]
		if generationCancel != nil && registry.active["session:"+target.ChildSessionID] != target.GenerationID {
			return false, nil
		}
		var worker *aiDelegationWorker
		if coordinator != nil && !coordinator.closing {
			worker = coordinator.workers[target.GenerationID]
			if worker != nil && worker.finalizing {
				worker = nil
			}
		}
		if generationCancel == nil && worker == nil {
			return false, nil
		}
		active[index] = activeTarget{generationCancel: generationCancel, worker: worker}
	}
	stamp := nowStamp(a)
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		for _, target := range targets {
			if err := recordAIAgentDelegationLifecycleEvent(tx, target.GenerationID, target.ChildSessionID, aiDelegationStopRequestedEvent, requestID, stamp); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	for _, target := range active {
		if target.generationCancel != nil {
			target.generationCancel()
		}
		if target.worker != nil {
			target.worker.stopRequested = true
			target.worker.cancel()
		}
	}
	return true, nil
}
