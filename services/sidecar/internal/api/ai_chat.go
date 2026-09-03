package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiSSEProtocolVersion = "opc-ai-sse-v1"

type chatAIRequest struct {
	ProviderID string `json:"provider_id"`
	SessionID  string `json:"session_id"`
	Message    string `json:"message"`
}

// aiGenerationRegistry tracks active generations for this Sidecar process so
// an explicit cancel can stop the upstream request and so a provider or
// session never runs two generations at once.
type aiGenerationRegistry struct {
	mu      sync.Mutex
	active  map[string]string
	cancels map[string]context.CancelFunc
}

func newAIGenerationRegistry() *aiGenerationRegistry {
	return &aiGenerationRegistry{active: make(map[string]string), cancels: make(map[string]context.CancelFunc)}
}

func (r *aiGenerationRegistry) register(generationID, providerID, sessionID string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	providerKey, sessionKey := "provider:"+providerID, "session:"+sessionID
	if _, busy := r.active[providerKey]; busy {
		return false
	}
	if _, busy := r.active[sessionKey]; busy {
		return false
	}
	r.active[providerKey] = generationID
	r.active[sessionKey] = generationID
	r.cancels[generationID] = cancel
	return true
}

func (r *aiGenerationRegistry) release(generationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, generationID)
	for key, active := range r.active {
		if active == generationID {
			delete(r.active, key)
		}
	}
}

func (r *aiGenerationRegistry) cancel(generationID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[generationID]
	if !ok {
		return false
	}
	cancel()
	return true
}

func (r *aiGenerationRegistry) cancelSession(sessionID string) {
	r.mu.Lock()
	generationID := r.active["session:"+sessionID]
	cancel := r.cancels[generationID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// recoverAIGenerationsOnStartup cancels queued/streaming generations left by
// a previous Sidecar process; they were never observed to completion.
func recoverAIGenerationsOnStartup(db *gorm.DB, now time.Time) error {
	return db.Model(&models.AIGeneration{}).
		Where("status IN ('queued','streaming')").
		Updates(map[string]any{
			"status": "cancelled", "error_code": "AI_GENERATION_INTERRUPTED", "updated_at": now.UTC().Format(time.RFC3339Nano),
		}).Error
}

func (a *API) chatAI(c *gin.Context) {
	var input chatAIRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	message := strings.TrimSpace(input.Message)
	if message == "" || len(message) > modelclient.MaxPromptBytes {
		writeError(c, http.StatusUnprocessableEntity, "AI_MESSAGE_INVALID", "The chat message must be between 1 and 65536 characters")
		return
	}
	provider, ok := a.loadChatProvider(c, input.ProviderID)
	if !ok {
		return
	}
	session, ok := a.resolveChatSession(c, &input.SessionID, message)
	if !ok {
		return
	}
	// Local providers run keyless on the loopback interface (ADR-005); remote
	// providers read their key from the OS credential store, use it for this
	// request only, and never persist it.
	apiKey := ""
	if provider.Kind != aiProviderKindLocal {
		var keyErr error
		apiKey, keyErr = a.keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID))
		if errors.Is(keyErr, keystore.ErrNotFound) {
			writeError(c, http.StatusConflict, "AI_KEY_UNAVAILABLE", "This provider has no stored API key")
			return
		}
		if keyErr != nil {
			writeError(c, http.StatusServiceUnavailable, "AI_KEY_STORE_UNAVAILABLE", "The operating system credential store is not available")
			return
		}
	}

	streamCtx := c.Request.Context()
	generationCtx, cancelGeneration := context.WithCancel(streamCtx)
	generationID := uuid.NewString()
	if !a.aiGenerations.register(generationID, provider.ID, session.ID, cancelGeneration) {
		cancelGeneration()
		writeError(c, http.StatusConflict, "AI_PROVIDER_BUSY", "This provider or session already has an active generation")
		return
	}
	defer a.aiGenerations.release(generationID)

	generation := models.AIGeneration{
		ID: generationID, SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a),
	}
	// The user turn is persisted immediately so it shows up right away and
	// survives an interrupted generation; only the assistant reply waits for
	// the stream outcome. A session still carrying its default title is
	// renamed from the first user message (ChatGPT/ZCode-style rail naming).
	if session.Persist {
		if err := a.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: session.ID, Role: "user", Status: "completed",
				Content: message, CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a),
			}).Error; err != nil {
				return err
			}
			updates := map[string]any{
				"version": gorm.Expr("version + 1"), "updated_at": nowStamp(a),
			}
			if session.Title == "" || session.Title == defaultAISessionTitle {
				updates["title"] = aiSessionTitleFromMessage(message)
			}
			return tx.Model(&models.AISession{}).Where("id = ?", session.ID).Updates(updates).Error
		}); err != nil {
			_ = a.db.Model(&models.AIGeneration{}).Where("id = ?", generationID).Updates(map[string]any{
				"status": "failed", "error_code": "AI_MESSAGE_PERSIST_FAILED", "updated_at": nowStamp(a),
			}).Error
			writeDatabaseError(c)
			return
		}
	}
	if err := a.db.Create(&generation).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	a.recordAIGenerationEvent("ai_generation_started", generation, requestIDFromContext(c))

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-store")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	writeSSE := func(event, payload string) bool {
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}
	meta := aiChatStreamMeta{
		Protocol: provider.Protocol, Generation: generationID, SessionID: session.ID,
		Model: provider.Model, ProviderID: provider.ID, SSEProtocol: aiSSEProtocolVersion,
	}
	metaJSON, _ := json.Marshal(meta)
	if !writeSSE("meta", string(metaJSON)) {
		a.finalizeCancelledGeneration(generation, session, "", "")
		return
	}

	// The run loop carries no registered tools (ADR-005): it degenerates to
	// the single streaming LLM call this endpoint has always performed.
	harnessClient := a.harnessClient
	if harnessClient == nil {
		harnessClient = harness.NewModelClient(nil)
	}
	runResult, streamErr := harness.Run(generationCtx, harnessClient,
		harness.Request{
			Protocol: provider.Protocol, BaseURL: provider.BaseURL, APIKey: apiKey, Model: provider.Model,
			History: a.chatHistory(session.ID), Memories: a.confirmedAIMemories(),
		},
		nil, nil,
		harness.Callbacks{
			OnDelta: func(delta string) {
				deltaJSON, _ := json.Marshal(struct {
					GenerationID string `json:"generation_id"`
					Text         string `json:"text"`
				}{generationID, delta})
				writeSSE("delta", string(deltaJSON))
			},
			OnReasoning: func(reasoning string) {
				reasoningJSON, _ := json.Marshal(struct {
					GenerationID string `json:"generation_id"`
					Text         string `json:"text"`
				}{generationID, reasoning})
				writeSSE("reasoning", string(reasoningJSON))
			},
		})

	switch {
	case streamCtx.Err() != nil || generationCtx.Err() != nil:
		partial := runResult.Text
		a.finalizeCancelledGeneration(generation, session, partial, runResult.Reasoning)
		cancelledJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			PartialText  string `json:"partial_text"`
		}{generationID, partial})
		_ = writeSSE("cancelled", string(cancelledJSON))
	case streamErr != nil:
		code := aiStreamErrorCode(streamErr)
		a.finalizeFailedGeneration(generation, code)
		detail := streamErr.Error()
		if len(detail) > 200 {
			detail = detail[:200]
		}
		errorJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
			Error        string `json:"error"`
			Detail       string `json:"detail"`
		}{generationID, code, detail})
		_ = writeSSE("error", string(errorJSON))
	default:
		completedAt := nowStamp(a)
		a.finalizeCompletedGeneration(generation, session, runResult.Text, runResult.Reasoning, provider, completedAt)
		doneJSON, _ := json.Marshal(struct {
			GenerationID string `json:"generation_id"`
		}{generationID})
		_ = writeSSE("done", string(doneJSON))
	}
}

type aiChatStreamMeta struct {
	Protocol    string `json:"protocol"`
	Generation  string `json:"generation_id"`
	SessionID   string `json:"session_id"`
	Model       string `json:"model"`
	ProviderID  string `json:"provider_id"`
	SSEProtocol string `json:"sse_protocol"`
}

func (a *API) loadChatProvider(c *gin.Context, providerID string) (models.AIProvider, bool) {
	id := strings.TrimSpace(providerID)
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "INVALID_AI_PROVIDER_ID", "AI provider id must be a UUID")
		return models.AIProvider{}, false
	}
	row, err := loadAIProvider(a.db.WithContext(c.Request.Context()), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", "AI provider not found")
		return models.AIProvider{}, false
	}
	if err != nil {
		writeDatabaseError(c)
		return models.AIProvider{}, false
	}
	if row.Status == "disabled" {
		writeError(c, http.StatusConflict, "AI_PROVIDER_DISABLED", "This AI provider is disabled")
		return models.AIProvider{}, false
	}
	if row.Status != "ready" {
		writeError(c, http.StatusConflict, "AI_PROVIDER_NOT_READY", "Run a successful health check before chatting with this provider")
		return models.AIProvider{}, false
	}
	return row, true
}

// resolveChatSession returns the referenced session or creates one; ok=false
// means the response has already been written.
func (a *API) resolveChatSession(c *gin.Context, sessionID *string, firstMessage string) (*models.AISession, bool) {
	if trimmed := strings.TrimSpace(*sessionID); trimmed != "" {
		if _, err := uuid.Parse(trimmed); err != nil {
			writeError(c, http.StatusUnprocessableEntity, "INVALID_AI_SESSION_ID", "AI session id must be a UUID")
			return nil, false
		}
		var row models.AISession
		if err := a.db.WithContext(c.Request.Context()).Where("id = ?", trimmed).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(c, http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
				return nil, false
			}
			writeDatabaseError(c)
			return nil, false
		}
		return &row, true
	}
	now := nowStamp(a)
	row := models.AISession{
		ID: uuid.NewString(), Title: aiSessionTitleFromMessage(firstMessage), Persist: true,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.db.Create(&row).Error; err != nil {
		writeDatabaseError(c)
		return nil, false
	}
	return &row, true
}

func aiSessionTitleFromMessage(message string) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) == 0 {
		return defaultAISessionTitle
	}
	if len(runes) > 30 {
		return string(runes[:30]) + "…"
	}
	return string(runes)
}

// chatHistory assembles the prompt turns from persisted messages
// (oldest-first, the new user turn included), trimmed from the front to the
// prompt budget.
func (a *API) chatHistory(sessionID string) []modelclient.ChatMessage {
	var rows []models.AIMessage
	if err := a.db.Where("session_id = ?", sessionID).Order("created_at ASC, id ASC").Limit(200).Find(&rows).Error; err != nil {
		rows = nil
	}
	history := make([]modelclient.ChatMessage, 0, len(rows))
	for _, row := range rows {
		if row.Content == "" || row.Status == "failed" {
			continue
		}
		history = append(history, modelclient.ChatMessage{Role: row.Role, Content: row.Content})
	}
	budget := modelclient.MaxPromptBytes - len(modelclient.SystemPrompt) - 1024
	total := 0
	cut := len(history)
	for i := len(history) - 1; i >= 0; i-- {
		total += len(history[i].Content) + 64
		if total > budget {
			cut = i + 1
			break
		}
		if i == 0 {
			cut = 0
		}
	}
	return history[cut:]
}

func (a *API) finalizeCompletedGeneration(generation models.AIGeneration, session *models.AISession, assistantText, reasoning string, provider models.AIProvider, completedAt string) {
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if session.Persist {
			snapshot, err := json.Marshal(struct {
				Model    string `json:"model"`
				Protocol string `json:"protocol"`
			}{provider.Model, provider.Protocol})
			if err != nil {
				return err
			}
			if err := tx.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed",
				Content: assistantText, Reasoning: aiNullableString(reasoning), ModelSnapshot: aiStringPtr(string(snapshot)),
				CreatedAt: completedAt, UpdatedAt: completedAt,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).Updates(map[string]any{
			"status": "completed", "content": assistantText, "updated_at": completedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&models.AISession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"version": gorm.Expr("version + 1"), "updated_at": completedAt,
		}).Error
	})
	if err != nil {
		a.options.Logger.Print("AI generation completion persistence failed for " + generation.ID)
		return
	}
	a.recordAIGenerationEvent("ai_generation_completed", generation, "")
}

func (a *API) finalizeFailedGeneration(generation models.AIGeneration, code string) {
	err := a.db.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).Updates(map[string]any{
		"status": "failed", "error_code": code, "updated_at": nowStamp(a),
	}).Error
	if err != nil {
		a.options.Logger.Print("AI generation failure persistence failed for " + generation.ID)
		return
	}
	a.recordAIGenerationEvent("ai_generation_failed", generation, "")
}

// finalizeCancelledGeneration keeps the generated partial content. The user
// turn was already persisted when the generation started.
func (a *API) finalizeCancelledGeneration(generation models.AIGeneration, session *models.AISession, partial, reasoning string) {
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if session.Persist {
			if err := tx.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "cancelled",
				Content: partial, Reasoning: aiNullableString(reasoning), CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a),
			}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"status": "cancelled", "updated_at": nowStamp(a)}
		if partial != "" {
			updates["content"] = partial
		}
		return tx.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).Updates(updates).Error
	})
	if err != nil {
		a.options.Logger.Print("AI generation cancel persistence failed for " + generation.ID)
		return
	}
	a.recordAIGenerationEvent("ai_generation_cancelled", generation, "")
}

func (a *API) recordAIGenerationEvent(action string, generation models.AIGeneration, requestID string) {
	payload, err := json.Marshal(struct {
		GenerationID string `json:"generation_id"`
		SessionID    string `json:"session_id"`
	}{generation.ID, generation.SessionID})
	if err != nil {
		return
	}
	err = a.db.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_generation", "aggregate_id": generation.ID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": aiNullableString(requestID),
		"current_json": string(payload), "created_at": nowStamp(a),
	}).Error
	if err != nil {
		a.options.Logger.Print("AI generation event persistence failed for " + generation.ID)
	}
}

// cancelAIGeneration stops one active generation on behalf of the user.
func (a *API) cancelAIGeneration(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a UUID")
		return
	}
	if a.aiGenerations.cancel(id) {
		c.JSON(http.StatusAccepted, gin.H{"data": gin.H{"id": id, "cancel_requested": true}})
		return
	}
	var row models.AIGeneration
	err := a.db.WithContext(c.Request.Context()).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_GENERATION_NOT_FOUND", "AI generation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if row.Status == "queued" || row.Status == "streaming" {
		writeError(c, http.StatusConflict, "AI_GENERATION_NOT_ACTIVE", "This generation is not active in this Sidecar process")
		return
	}
	writeError(c, http.StatusConflict, "AI_GENERATION_ALREADY_TERMINAL", "This generation already reached a terminal state")
}

func aiStreamErrorCode(err error) string {
	var statusErr *modelclient.UpstreamStatusError
	switch {
	case errors.Is(err, modelclient.ErrTimeout):
		return "AI_GENERATION_TIMEOUT"
	case errors.As(err, &statusErr):
		if statusErr.StatusCode == http.StatusUnauthorized || statusErr.StatusCode == http.StatusForbidden {
			return "AI_KEY_INVALID"
		}
		if statusErr.StatusCode == http.StatusNotFound || statusErr.StatusCode == http.StatusMethodNotAllowed {
			return "AI_ENDPOINT_INVALID"
		}
		return "AI_PROVIDER_ERROR"
	case errors.Is(err, modelclient.ErrStream):
		return "AI_STREAM_ERROR"
	default:
		return "AI_ENDPOINT_UNREACHABLE"
	}
}

func nowStamp(a *API) string {
	return a.options.Now().UTC().Format(time.RFC3339Nano)
}

func aiStringPtr(value string) *string { return &value }
