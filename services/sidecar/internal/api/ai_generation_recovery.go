package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Active text is process memory only. Once terminal, the persisted session
// owns any recoverable body; non-persistent runs expose metadata only.
type aiGenerationResponse struct {
	*aiGenerationCitations
	ID              string                            `json:"id"`
	SessionID       string                            `json:"session_id"`
	ProviderID      string                            `json:"provider_id"`
	Status          string                            `json:"status"`
	Content         string                            `json:"content"`
	Reasoning       string                            `json:"reasoning"`
	ErrorCode       *string                           `json:"error_code"`
	Persist         bool                              `json:"persist"`
	ClientRequestID *string                           `json:"client_request_id"`
	Progress        []aiRunProgress                   `json:"progress"`
	Origin          *aiGenerationOrigin               `json:"origin,omitempty"`
	AccessRequest   *aiWorkspaceAccessRequestResponse `json:"access_request,omitempty"`
}

// Absence means unavailable (active, expired, restarted, or legacy), NOT
// not_requested. No document content is retained in this bounded cache.
type aiGenerationCitations struct {
	CitationStatus string           `json:"citation_status"`
	Citations      []aiCitationItem `json:"citations"`
}

type aiTransientCitations struct {
	sessionID string
	expiresAt time.Time
	evidence  aiGenerationCitations
}

const aiCitationRecoveryTTL = 15 * time.Minute
const aiCitationRecoveryLimit = 32

func cloneGenerationCitations(value aiGenerationCitations) *aiGenerationCitations {
	value.Citations = append([]aiCitationItem{}, value.Citations...)
	return &value
}

func (r *aiGenerationRegistry) pruneCitations(now time.Time) {
	for id, entry := range r.citations {
		if !now.Before(entry.expiresAt) {
			delete(r.citations, id)
		}
	}
}

func (r *aiGenerationRegistry) rememberCitations(id, sessionID string, evidence aiGenerationCitations, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneCitations(now)
	// Same maximum as persisted citation snapshots; even temporary metadata
	// must have a byte bound, not merely an entry-count bound.
	encoded, err := encodeAICitationSnapshot(aiCitationSnapshot{Version: 1, Status: evidence.CitationStatus, Items: evidence.Citations})
	if err != nil || len(*encoded) > 16*1024 {
		return
	}
	if _, exists := r.citations[id]; !exists && len(r.citations) >= aiCitationRecoveryLimit {
		oldestID := ""
		var oldest time.Time
		for key, entry := range r.citations {
			if oldestID == "" || entry.expiresAt.Before(oldest) || (entry.expiresAt.Equal(oldest) && key < oldestID) {
				oldestID, oldest = key, entry.expiresAt
			}
		}
		delete(r.citations, oldestID)
	}
	r.citations[id] = aiTransientCitations{sessionID: sessionID, expiresAt: now.Add(aiCitationRecoveryTTL), evidence: *cloneGenerationCitations(evidence)}
}

func (r *aiGenerationRegistry) citationSnapshot(id, sessionID string, now time.Time) *aiGenerationCitations {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneCitations(now)
	if entry, exists := r.citations[id]; exists && entry.sessionID == sessionID {
		return cloneGenerationCitations(entry.evidence)
	}
	return nil
}

func (r *aiGenerationRegistry) forgetCitations(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.citations, id)
}

func (r *aiGenerationRegistry) forgetSessionCitations(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, entry := range r.citations {
		if entry.sessionID == sessionID {
			delete(r.citations, id)
		}
	}
}

func (r *aiGenerationRegistry) setSnapshot(value aiGenerationResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value.Progress = append([]aiRunProgress{}, value.Progress...)
	if value.AccessRequest != nil {
		value.AccessRequest = &aiWorkspaceAccessRequestResponse{Scopes: append([]string(nil), value.AccessRequest.Scopes...)}
	}
	r.snapshots[value.ID] = value
}
func (r *aiGenerationRegistry) appendSnapshot(id, text, reasoning string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value, ok := r.snapshots[id]; ok {
		value.Content += text
		value.Reasoning += reasoning
		r.snapshots[id] = value
	}
}
func (r *aiGenerationRegistry) setAccessRequest(id string, scopes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if value, ok := r.snapshots[id]; ok {
		if r.dismissedAccessRequests[id] {
			return
		}
		value.AccessRequest = &aiWorkspaceAccessRequestResponse{Scopes: append([]string(nil), scopes...)}
		r.snapshots[id] = value
	}
}
func (r *aiGenerationRegistry) dismissAccessRequest(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dismissedAccessRequests[id] {
		return true
	}
	value, ok := r.snapshots[id]
	if !ok || value.AccessRequest == nil || r.cancels[id] == nil {
		return false
	}
	r.dismissedAccessRequests[id] = true
	value.AccessRequest = nil
	r.snapshots[id] = value
	return true
}
func (r *aiGenerationRegistry) accessRequestDismissed(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dismissedAccessRequests[id]
}
func (r *aiGenerationRegistry) snapshot(id string) (aiGenerationResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.snapshots[id]
	value.Progress = append([]aiRunProgress{}, value.Progress...)
	if value.AccessRequest != nil {
		value.AccessRequest = &aiWorkspaceAccessRequestResponse{Scopes: append([]string(nil), value.AccessRequest.Scopes...)}
	}
	return value, ok
}
func (r *aiGenerationRegistry) cancelAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.citations)
	for _, cancel := range r.cancels {
		cancel()
	}
}

func (a *API) replayAIChatRequest(c *gin.Context, key, hash string) bool {
	var row models.AIGeneration
	err := a.db.WithContext(c.Request.Context()).Where("request_key = ?", key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		writeDatabaseError(c)
		return true
	}
	if row.RequestHash == nil || *row.RequestHash != hash {
		writeError(c, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "This request key was already used with different chat input")
		return true
	}
	c.AbortWithStatusJSON(http.StatusConflict, gin.H{"code": "AI_CHAT_ALREADY_ACCEPTED", "message": "This chat request was already accepted; resume its existing generation", "request_id": requestIDFromContext(c), "generation_id": row.ID, "session_id": row.SessionID})
	return true
}

func (a *API) generationResponse(row models.AIGeneration) (aiGenerationResponse, error) {
	var session models.AISession
	if err := a.db.Where("id = ?", row.SessionID).First(&session).Error; err != nil {
		return aiGenerationResponse{}, err
	}
	result := aiGenerationResponse{ID: row.ID, SessionID: row.SessionID, ProviderID: row.ProviderID, Status: row.Status, ErrorCode: row.ErrorCode, Persist: session.Persist, ClientRequestID: row.RequestKey}
	origin, originErr := aiGenerationOriginFor(a.db, row.ID)
	if originErr != nil {
		return result, originErr
	}
	result.Origin = origin
	if row.Status == "streaming" || row.Status == "queued" {
		if snapshot, ok := a.aiGenerations.snapshot(row.ID); ok {
			result.Content = stripAIControlBlocks(snapshot.Content)
			result.Reasoning = snapshot.Reasoning
			result.Progress = snapshot.Progress
			result.AccessRequest = snapshot.AccessRequest
		}
		if result.Progress == nil {
			result.Progress = []aiRunProgress{}
		}
		return result, nil
	}
	var err error
	result.Progress, err = a.completedRunProgress(row.ID)
	if err != nil {
		return result, err
	}
	if session.Persist {
		request, err := loadOpenAIWorkspaceAccessRequest(a.db, row.ID)
		if err != nil {
			return result, err
		}
		result.AccessRequest = request
		if row.Content != nil {
			result.Content = *row.Content
		}
		var message models.AIMessage
		if err := a.db.Where("generation_id = ? AND role='assistant'", row.ID).First(&message).Error; err == nil {
			result.Content = message.Content
			if message.Reasoning != nil {
				result.Reasoning = *message.Reasoning
			}
			if row.Status == "completed" && message.Status == "completed" {
				status, items, err := decodeAICitationSnapshot(message.CitationsSnapshot)
				if err != nil {
					return result, err
				}
				result.aiGenerationCitations = &aiGenerationCitations{CitationStatus: status, Citations: items}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
	} else if row.Status == "completed" {
		result.aiGenerationCitations = a.aiGenerations.citationSnapshot(row.ID, row.SessionID, time.Now())
	}
	return result, nil
}

func (a *API) getAIGeneration(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a UUID")
		return
	}
	a.getAIGenerationWhere(c, "id = ?", id)
}
func (a *API) getAIGenerationByRequest(c *gin.Context) {
	key := strings.TrimSpace(c.Param("key"))
	if key == "" || len(key) > 128 {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Invalid chat request key")
		return
	}
	a.getAIGenerationWhere(c, "request_key = ?", key)
}
func (a *API) getAIGenerationWhere(c *gin.Context, condition, value string) {
	c.Header("Cache-Control", "no-store")
	var row models.AIGeneration
	err := a.db.WithContext(c.Request.Context()).Where(condition, value).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_GENERATION_NOT_FOUND", "AI generation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	response, err := a.generationResponse(row)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}
func (a *API) listActiveAIGenerations(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var rows []models.AIGeneration
	if err := a.db.WithContext(c.Request.Context()).Where("status IN ('queued','streaming')").Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	result := make([]aiGenerationResponse, 0, len(rows))
	for _, row := range rows {
		response, err := a.generationResponse(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		result = append(result, response)
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// Called with the maintenance writer held. Request cancellation without
// waiting: finalizers must be allowed to reacquire their short storage lease.
func (a *API) cancelAIRunsForRestore() {
	if a.aiContinuations != nil {
		a.aiContinuations.cancelAll()
		if err := interruptAIContinuations(a.db, "restore_pending", a.options.Now()); err != nil {
			a.options.Logger.Print("AI continuation restore state could not be recorded")
		}
	}
	if a.aiGenerations != nil {
		a.aiGenerations.cancelAll()
	}
	if a.aiCompactions != nil {
		a.aiCompactions.cancel()
	}
	if a.aiEvaluationRunner != nil {
		r := a.aiEvaluationRunner
		r.mu.Lock()
		for _, cancel := range r.active {
			cancel()
		}
		r.mu.Unlock()
	}
	// Agent execution/finalization goroutines observe their cancelled context,
	// then fail closed on restorePending when they next request a short storage
	// lease. Do not wait here: restore scheduling holds the maintenance writer.
	a.requestCancelAgentRuns()
}
