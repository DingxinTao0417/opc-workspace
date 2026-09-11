package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Active text is process memory only. Once terminal, the persisted session
// owns any recoverable body; non-persistent runs expose metadata only.
type aiGenerationResponse struct {
	ID              string  `json:"id"`
	SessionID       string  `json:"session_id"`
	ProviderID      string  `json:"provider_id"`
	Status          string  `json:"status"`
	Content         string  `json:"content"`
	Reasoning       string  `json:"reasoning"`
	ErrorCode       *string `json:"error_code"`
	Persist         bool    `json:"persist"`
	ClientRequestID *string `json:"client_request_id"`
}

func (r *aiGenerationRegistry) setSnapshot(value aiGenerationResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
func (r *aiGenerationRegistry) snapshot(id string) (aiGenerationResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.snapshots[id]
	return value, ok
}
func (r *aiGenerationRegistry) cancelAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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
	if row.Status == "streaming" || row.Status == "queued" {
		if snapshot, ok := a.aiGenerations.snapshot(row.ID); ok {
			result.Content = stripAIControlBlocks(snapshot.Content)
			result.Reasoning = snapshot.Reasoning
		}
		return result, nil
	}
	if session.Persist {
		if row.Content != nil {
			result.Content = *row.Content
		}
		var message models.AIMessage
		if err := a.db.Where("generation_id = ?", row.ID).First(&message).Error; err == nil {
			result.Content = message.Content
			if message.Reasoning != nil {
				result.Reasoning = *message.Reasoning
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
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
	a.aiGenerations.cancelAll()
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
}
