package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiSSEProtocolVersion = "opc-ai-sse-v1"
const aiGenerationStopRequestedEvent = "ai_generation_stop_requested"

// This code-owned handoff remains visible after a saved generation is reloaded;
// a successful response boundary must not imply that the business task is done.
const aiBudgetHandoffNotice = "本轮已达到处理轮数上限，已停止继续调用工具。以下是当前交接结果，不表示整个任务已完成；操作是否已执行请以实际回执为准，待确认建议仍需你逐项确认。后续处理请发送新消息，并按需要重新授权。"

const aiContextWindowNotice = "本轮为继续处理工具结果，已从模型请求中移除部分较早对话；当前消息、最新工具证据和已授权上下文保留。较早细节可能不完整，重要旧约束请核对；本地聊天记录未删除，也未增加任何权限。"
const aiToolResultCompactionNotice = "本轮为继续处理，已从后续模型请求中收起部分较早的只读查询正文；部分结果只保留代码生成的身份/版本/状态/分页证据胶囊，其余仅有省略标记。胶囊不完整且可能过时，不能仅凭旧查询声称当前事实有效，需要完整或最新信息时应重新查询。最新工具结果与操作回执保留，本地记录未删除，也未增加任何权限。"

// Notices are code-owned and persisted with the successful reply. They share
// the response limit and never replace/truncate the model's actual evidence.
func aiCompletedRunText(result harness.Result) (string, error) {
	var notices []string
	if result.ContextTrimmedTurns > 0 {
		notices = append(notices, aiContextWindowNotice)
	}
	if result.CompactedToolResults > 0 {
		notices = append(notices, aiToolResultCompactionNotice)
	}
	if result.BudgetHandoff {
		notices = append(notices, aiBudgetHandoffNotice)
	}
	if len(notices) == 0 {
		return result.Text, nil
	}
	prefix := strings.Join(notices, "\n\n") + "\n\n"
	if len(prefix)+len(result.Text)+len(result.Reasoning) > modelclient.MaxResponseBytes {
		return "", modelclient.ErrResponseBudget
	}
	return prefix + result.Text, nil
}

type chatAIRequest struct {
	ProviderID                string                `json:"provider_id"`
	SessionID                 string                `json:"session_id"`
	Message                   string                `json:"message"`
	ActionReceiptGenerationID string                `json:"action_receipt_generation_id,omitempty"`
	ActionRecheckProposalID   string                `json:"action_recheck_proposal_id,omitempty"`
	Workspace                 *aiWorkspaceGrant     `json:"workspace,omitempty"`
	ProjectFiles              *aiProjectFileContext `json:"project_files,omitempty"`
	Context                   *struct {
		ProviderVersion int64                           `json:"provider_version"`
		Sources         []aiBusinessContextSourceInput  `json:"sources"`
		Knowledge       []aiKnowledgeContextSourceInput `json:"knowledge"`
	} `json:"context"`
}

// aiGenerationRegistry tracks active generations for this Sidecar process so
// an explicit cancel can stop the upstream request and so a provider or
// session never runs two generations at once.
type aiGenerationRegistry struct {
	beginMu                 sync.Mutex
	mu                      sync.Mutex
	active                  map[string]string
	cancels                 map[string]context.CancelFunc
	snapshots               map[string]aiGenerationResponse
	citations               map[string]aiTransientCitations
	dismissedAccessRequests map[string]bool
}

func newAIGenerationRegistry() *aiGenerationRegistry {
	return &aiGenerationRegistry{active: make(map[string]string), cancels: make(map[string]context.CancelFunc), snapshots: make(map[string]aiGenerationResponse), citations: make(map[string]aiTransientCitations), dismissedAccessRequests: make(map[string]bool)}
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
	delete(r.snapshots, generationID)
	delete(r.dismissedAccessRequests, generationID)
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

func (r *aiGenerationRegistry) cancelWithPersist(generationID string, persist func() (bool, error)) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.cancels[generationID]
	if !ok {
		return false, nil
	}
	persisted, err := persist()
	if err != nil || !persisted {
		return false, err
	}
	cancel()
	return true, nil
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
	completedAt := now.UTC().Format(time.RFC3339Nano)
	return db.Transaction(func(tx *gorm.DB) error {
		var generationIDs []string
		if err := tx.Model(&models.AIGeneration{}).
			Where("status IN ('queued','streaming')").Pluck("id", &generationIDs).Error; err != nil {
			return err
		}
		if len(generationIDs) == 0 {
			return nil
		}
		var stopRequestedIDs []string
		if err := tx.Table("workflow_events").Distinct("aggregate_id").
			Where("aggregate_type = 'ai_generation' AND action = ? AND aggregate_id IN ?", aiGenerationStopRequestedEvent, generationIDs).
			Pluck("aggregate_id", &stopRequestedIDs).Error; err != nil {
			return err
		}
		stopRequested := make(map[string]bool, len(stopRequestedIDs))
		for _, id := range stopRequestedIDs {
			stopRequested[id] = true
		}
		var interruptedIDs []string
		for _, id := range generationIDs {
			if !stopRequested[id] {
				interruptedIDs = append(interruptedIDs, id)
			}
		}
		if len(interruptedIDs) > 0 {
			if err := tx.Model(&models.AIGeneration{}).
				Where("id IN ?", interruptedIDs).
				Updates(map[string]any{
					"status": "cancelled", "error_code": "AI_GENERATION_INTERRUPTED", "updated_at": completedAt,
				}).Error; err != nil {
				return err
			}
		}
		if len(stopRequestedIDs) > 0 {
			if err := tx.Model(&models.AIGeneration{}).
				Where("id IN ?", stopRequestedIDs).
				Updates(map[string]any{
					"status": "cancelled", "error_code": nil, "updated_at": completedAt,
				}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.AIRunStep{}).
			Where("generation_id IN ? AND kind = 'generation' AND status = 'running'", generationIDs).
			Updates(map[string]any{
				"status": "cancelled", "completed_at": completedAt, "duration_ms": 0,
			}).Error
	})
}

func (a *API) chatAI(c *gin.Context) {
	var input chatAIRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	streamStarted := false
	sink := func(event, payload string) bool {
		if !streamStarted {
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.Header().Set("Cache-Control", "no-store")
			c.Writer.Header().Set("X-Accel-Buffering", "no")
			c.Writer.WriteHeader(http.StatusOK)
			streamStarted = true
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}
	result, err := a.runAIGeneration(c.Request.Context(), input, aiGenerationRunOptions{
		RequestKey: c.GetHeader("Idempotency-Key"), RequestID: requestIDFromContext(c),
	}, sink)
	if streamStarted {
		return
	}
	if err != nil {
		var runError *aiGenerationRunError
		if errors.As(err, &runError) {
			writeError(c, runError.Status, runError.Code, runError.Message)
		} else if !writeAIBusinessContextError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	if result.Accepted {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"code": "AI_CHAT_ALREADY_ACCEPTED", "message": "This chat request was already accepted; resume its existing generation", "request_id": requestIDFromContext(c), "generation_id": result.GenerationID, "session_id": result.SessionID})
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
	row, err := a.loadGenerationProvider(c.Request.Context(), providerID)
	if err == nil {
		return row, true
	}
	var runError *aiGenerationRunError
	if errors.As(err, &runError) {
		writeError(c, runError.Status, runError.Code, runError.Message)
	} else {
		writeDatabaseError(c)
	}
	return models.AIProvider{}, false
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

var (
	aiTaskBlockPattern      = regexp.MustCompile(`(?is)\[opc:task\].*?(?:\[/opc:task\]|\[opc:task\])`)
	aiMemoryBlockPattern    = regexp.MustCompile(`(?is)\[opc:memory\].*?(?:\[/opc:memory\]|\[opc:memory\])`)
	aiSelfCheckBlockPattern = regexp.MustCompile(`(?is)\[opc:selfcheck\].*?(?:\[/opc:selfcheck\]|$)`)
	aiOpenControlTail       = regexp.MustCompile(`(?is)\[opc:(?:task|memory)\].*$`)
)

func stripAIControlBlocks(content string) string {
	content = aiTaskBlockPattern.ReplaceAllString(content, "")
	content = aiMemoryBlockPattern.ReplaceAllString(content, "")
	content = aiSelfCheckBlockPattern.ReplaceAllString(content, "")
	content = stripAICitationBlocks(content)
	return strings.TrimSpace(aiOpenControlTail.ReplaceAllString(content, ""))
}

// chatHistory builds a complete-turn rolling window from the newest 200
// persisted messages plus the current user turn. It measures the exact
// serialized provider payload and never silently removes the current turn.
func (a *API) chatHistory(sessionID, currentMessage, protocol, model string, promptContext modelclient.PromptContext) ([]modelclient.ChatMessage, error) {
	var rows []models.AIMessage
	if err := a.db.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	messages := aiPromptMessages(rows)
	messages = append(messages, aiPromptMessage{Message: modelclient.ChatMessage{Role: "user", Content: currentMessage}})
	selected, _, err := selectAIHistoryWindow(messages, protocol, model, promptContext)
	return selected, err
}

func (a *API) finalizeCompletedGeneration(generation models.AIGeneration, session *models.AISession, assistantText, reasoning string, provider models.AIProvider, citationsSnapshot *string, steps []harness.RunStep, completedAt string, accessRequests ...[]string) error {
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	a.aiWorkspaceAccessRequestMu.Lock()
	defer a.aiWorkspaceAccessRequestMu.Unlock()
	if a.restorePending.Load() {
		return errors.New("AI generation interrupted by pending restore")
	}
	citationStatus, citations, err := decodeAICitationSnapshot(citationsSnapshot)
	if err != nil {
		return err
	}
	claimed := false
	err = a.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"status": "completed", "error_code": nil, "updated_at": completedAt}
		if session.Persist {
			updates["content"] = assistantText
		}
		var claimErr error
		claimed, claimErr = claimAIGenerationTerminal(tx, generation, updates)
		if claimErr != nil || !claimed {
			return claimErr
		}
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
				CitationsSnapshot: citationsSnapshot, GenerationID: &generation.ID,
				CreatedAt: completedAt, UpdatedAt: completedAt,
			}).Error; err != nil {
				return err
			}
			if len(accessRequests) > 0 && len(accessRequests[0]) > 0 {
				status := "open"
				if a.aiGenerations != nil && a.aiGenerations.accessRequestDismissed(generation.ID) {
					status = "dismissed"
				}
				if err := persistAIWorkspaceAccessRequest(tx, generation.ID, accessRequests[0], status, completedAt); err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&models.AISession{}).Where("id = ?", session.ID).Updates(map[string]any{
			"version": gorm.Expr("version + 1"), "updated_at": completedAt,
		}).Error; err != nil {
			return err
		}
		if err := persistAIRunSteps(tx, generation, steps, "succeeded", "", completedAt); err != nil {
			return err
		}
		if !session.Persist {
			// Publish before commit: a reader cannot observe the completed row
			// before its metadata exists. Rollback removes the unpublished result.
			a.aiGenerations.rememberCitations(generation.ID, session.ID, aiGenerationCitations{CitationStatus: citationStatus, Citations: citations}, time.Now())
		}
		return nil
	})
	if err != nil {
		a.aiGenerations.forgetCitations(generation.ID)
		a.options.Logger.Print("AI generation completion persistence failed for " + generation.ID)
		return err
	}
	if claimed {
		a.recordAIGenerationEvent("ai_generation_completed", generation, "")
	}
	return nil
}

type aiFailedPartial struct {
	session         *models.AISession
	text, reasoning string
}

func (a *API) finalizeFailedGeneration(generation models.AIGeneration, code string, steps []harness.RunStep, partial ...aiFailedPartial) error {
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	if a.restorePending.Load() {
		return errors.New("AI generation interrupted by pending restore")
	}
	completedAt := nowStamp(a)
	claimed := false
	err := a.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status": "failed", "error_code": code, "updated_at": completedAt,
		}
		hasPartial := len(partial) > 0 && partial[0].session != nil && partial[0].session.Persist && partial[0].text != ""
		if hasPartial {
			updates["content"] = partial[0].text
		}
		var claimErr error
		claimed, claimErr = claimAIGenerationTerminal(tx, generation, updates)
		if claimErr != nil || !claimed {
			return claimErr
		}
		if hasPartial {
			value := partial[0]
			updates["content"] = value.text
			if err := tx.Create(&models.AIMessage{ID: uuid.NewString(), SessionID: generation.SessionID, Role: "assistant", Status: "failed", Content: value.text, Reasoning: aiNullableString(value.reasoning), GenerationID: &generation.ID, CreatedAt: completedAt, UpdatedAt: completedAt}).Error; err != nil {
				return err
			}
		}
		return persistAIRunSteps(tx, generation, steps, "failed", code, completedAt)
	})
	if err != nil {
		a.options.Logger.Print("AI generation failure persistence failed for " + generation.ID)
		return err
	}
	if claimed {
		a.recordAIGenerationEvent("ai_generation_failed", generation, "")
	}
	return nil
}

// finalizeCancelledGeneration keeps the generated partial content. The user
// turn was already persisted when the generation started.
func (a *API) finalizeCancelledGeneration(generation models.AIGeneration, session *models.AISession, partial, reasoning string, steps []harness.RunStep) error {
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	if a.restorePending.Load() {
		return errors.New("AI generation interrupted by pending restore")
	}
	completedAt := nowStamp(a)
	claimed := false
	err := a.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"status": "cancelled", "error_code": nil, "updated_at": completedAt}
		if session.Persist && partial != "" {
			updates["content"] = partial
		}
		var claimErr error
		claimed, claimErr = claimAIGenerationTerminal(tx, generation, updates)
		if claimErr != nil || !claimed {
			return claimErr
		}
		if session.Persist {
			if err := tx.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "cancelled",
				Content: partial, Reasoning: aiNullableString(reasoning), GenerationID: &generation.ID,
				CreatedAt: completedAt, UpdatedAt: completedAt,
			}).Error; err != nil {
				return err
			}
		}
		return persistAIRunSteps(tx, generation, steps, "cancelled", "", completedAt)
	})
	if err != nil {
		a.options.Logger.Print("AI generation cancel persistence failed for " + generation.ID)
		return err
	}
	if claimed {
		a.recordAIGenerationEvent("ai_generation_cancelled", generation, "")
	}
	return nil
}

func (a *API) recordAIGenerationEvent(action string, generation models.AIGeneration, requestID string) {
	if err := recordAIGenerationEventTx(a.db, action, generation, requestID, nowStamp(a)); err != nil {
		a.options.Logger.Print("AI generation event persistence failed for " + generation.ID)
	}
}

func recordAIGenerationEventTx(tx *gorm.DB, action string, generation models.AIGeneration, requestID, createdAt string) error {
	payload, err := json.Marshal(struct {
		GenerationID string `json:"generation_id"`
		SessionID    string `json:"session_id"`
	}{generation.ID, generation.SessionID})
	if err != nil {
		return err
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_generation", "aggregate_id": generation.ID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": aiNullableString(requestID),
		"current_json": string(payload), "created_at": createdAt,
	}).Error
}

func recordAIGenerationStopIntentTx(tx *gorm.DB, generationID, requestID, createdAt string) (bool, error) {
	var generation models.AIGeneration
	if err := tx.Select("id", "session_id", "status").Where("id = ?", generationID).Take(&generation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if generation.Status != "queued" && generation.Status != "streaming" {
		return false, nil
	}
	return recordAIGenerationStopIntentEventTx(tx, generation, requestID, createdAt)
}

// recordAIGenerationStopIntentEventTx is the idempotent event writer shared by
// the normal active-generation path and the continuation stop race. The latter
// may arrive after its own cancellation finalizer has already marked the exact
// generation cancelled, but never treats a naturally completed/failed row as
// a user stop.
func recordAIGenerationStopIntentEventTx(tx *gorm.DB, generation models.AIGeneration, requestID, createdAt string) (bool, error) {
	var count int64
	if err := tx.Table("workflow_events").Where(
		"aggregate_type = 'ai_generation' AND aggregate_id = ? AND action = ?", generation.ID, aiGenerationStopRequestedEvent,
	).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		if err := recordAIGenerationEventTx(tx, aiGenerationStopRequestedEvent, generation, requestID, createdAt); err != nil {
			return false, err
		}
	}
	return true, nil
}

func persistAIGenerationStopIntent(db *gorm.DB, generationID, requestID string, now time.Time) (bool, error) {
	persisted := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		persisted, err = recordAIGenerationStopIntentTx(tx, generationID, requestID, now.UTC().Format(time.RFC3339Nano))
		return err
	})
	return persisted, err
}

// cancelAIGeneration stops one active generation on behalf of the user.
func (a *API) cancelAIGeneration(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a UUID")
		return
	}
	if err := a.stopAIContinuationForGeneration(id); err != nil {
		writeDatabaseError(c)
		return
	}
	cancelled, cancelErr := a.aiGenerations.cancelWithPersist(id, func() (bool, error) {
		return persistAIGenerationStopIntent(a.db, id, requestIDFromContext(c), a.options.Now())
	})
	if cancelErr != nil {
		a.options.Logger.Print("AI generation stop intent persistence failed for " + id)
		writeDatabaseError(c)
		return
	}
	if !cancelled {
		// A coordinator worker may have an in-memory generation before its
		// database row commits (and restore cancellation deliberately bypasses
		// the maintenance writer). A missing row is not a persistence failure;
		// revoke that process-local worker directly and let its own finalizer
		// converge the durable state if/when it becomes visible.
		var status string
		err := a.db.Model(&models.AIGeneration{}).Select("status").Where("id = ?", id).Limit(1).Scan(&status).Error
		if err != nil {
			writeDatabaseError(c)
			return
		}
		if status == "" {
			cancelled = a.aiGenerations.cancel(id)
		}
	}
	if cancelled {
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
	case errors.Is(err, context.DeadlineExceeded):
		return "AI_GENERATION_TIMEOUT"
	case errors.Is(err, modelclient.ErrResponseBudget):
		return "AI_RESPONSE_BUDGET_EXHAUSTED"
	case errors.Is(err, modelclient.ErrIncompleteStream):
		return "AI_STREAM_INCOMPLETE"
	case errors.Is(err, modelclient.ErrTruncated):
		return "AI_RESPONSE_TRUNCATED"
	case errors.Is(err, modelclient.ErrFiltered):
		return "AI_RESPONSE_FILTERED"
	case errors.Is(err, harness.ErrMaxTurns):
		return "AI_TURN_BUDGET_EXHAUSTED"
	case errors.Is(err, harness.ErrToolBudget):
		return "AI_TOOL_BUDGET_EXHAUSTED"
	case errors.Is(err, harness.ErrToolCorrections):
		return "AI_TOOL_CORRECTIONS_EXHAUSTED"
	case errors.Is(err, harness.ErrToolUnavailable):
		return "AI_TOOL_UNAVAILABLE"
	case errors.Is(err, modelclient.ErrPromptTooLarge):
		return "AI_PROMPT_TOO_LARGE"
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
