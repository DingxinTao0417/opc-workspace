package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	aiContextSummaryBudget      = 4 << 10
	aiContextFactsBudget        = 4 << 10
	aiCompactionTriggerBytes    = 16 << 10
	aiCompactionBatchBytes      = 32 << 10
	aiCompactionMaxFacts        = 32
	aiCompactionCandidateLimit  = 2001
	aiCompactionCandidateWindow = 2000
)

const aiCompactionSystemPrompt = `你是 opc-workspace 的会话压缩器。你的唯一任务是把旧快照与给定对话合并为新的上下文快照。
硬性规则：
- 只压缩用户和助手已经明确表达的内容，不推断、不补充、不执行任务。
- 旧快照中仍相关的事实必须延续；新对话明确取代旧事实时保留新事实。
- 只输出一个 JSON 对象，不输出 Markdown、代码围栏、自评块或解释。
- JSON 格式严格为 {"summary":"...","facts":[{"kind":"decision|constraint|entity|open_question|progress","content":"..."}]}。
- summary 使用简洁连续文本；facts 最多 32 条，每条只表达一个事实。`

type aiContextFact struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type aiContextSnapshot struct {
	Summary string          `json:"summary"`
	Facts   []aiContextFact `json:"facts"`
}

type aiPromptMessage struct {
	ID        string
	CreatedAt string
	Message   modelclient.ChatMessage
}

type aiPromptTurn struct {
	Messages []aiPromptMessage
}

type aiCompactionCandidate struct {
	Messages        []modelclient.ChatMessage
	MessageCount    int
	InputBytes      int
	SourceMessageID string
}

// aiCompactionRegistry gives each session at most one background compaction
// and owns cancellation during session deletion or Sidecar shutdown.
type aiCompactionRegistry struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	active map[string]context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

func newAICompactionRegistry() *aiCompactionRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &aiCompactionRegistry{ctx: ctx, cancel: cancel, active: make(map[string]context.CancelFunc)}
}

func (r *aiCompactionRegistry) start(sessionID string, run func(context.Context)) bool {
	r.mu.Lock()
	if r.closed || r.ctx.Err() != nil {
		r.mu.Unlock()
		return false
	}
	if _, exists := r.active[sessionID]; exists {
		r.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.active[sessionID] = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.active, sessionID)
			r.mu.Unlock()
			cancel()
		}()
		run(ctx)
	}()
	return true
}

func (r *aiCompactionRegistry) cancelSession(sessionID string) {
	r.mu.Lock()
	cancel := r.active[sessionID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *aiCompactionRegistry) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.cancel()
	r.mu.Unlock()
	r.wg.Wait()
}

func decodeAIContextSnapshot(text string) (aiContextSnapshot, error) {
	text = strings.TrimSpace(text)
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	var wire struct {
		Summary *string          `json:"summary"`
		Facts   *[]aiContextFact `json:"facts"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return aiContextSnapshot{}, fmt.Errorf("decode AI context snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return aiContextSnapshot{}, errors.New("decode AI context snapshot: trailing data")
	}
	if wire.Summary == nil || wire.Facts == nil {
		return aiContextSnapshot{}, errors.New("AI context snapshot is missing required fields")
	}
	snapshot := aiContextSnapshot{Summary: strings.TrimSpace(*wire.Summary), Facts: *wire.Facts}
	if !utf8.ValidString(snapshot.Summary) || len(snapshot.Summary) > aiContextSummaryBudget {
		return aiContextSnapshot{}, errors.New("AI context summary exceeds its byte budget")
	}
	if len(snapshot.Facts) > aiCompactionMaxFacts {
		return aiContextSnapshot{}, errors.New("AI context snapshot has too many facts")
	}
	factBytes := 2
	for index := range snapshot.Facts {
		fact := &snapshot.Facts[index]
		fact.Kind = strings.TrimSpace(fact.Kind)
		fact.Content = strings.TrimSpace(fact.Content)
		if !validAIContextFactKind(fact.Kind) || fact.Content == "" || !utf8.ValidString(fact.Content) {
			return aiContextSnapshot{}, errors.New("AI context snapshot contains an invalid fact")
		}
		if index > 0 {
			factBytes += 3
		}
		factBytes += len(renderAIContextFact(*fact))
	}
	if factBytes > aiContextFactsBudget {
		return aiContextSnapshot{}, errors.New("AI context facts exceed their byte budget")
	}
	if snapshot.Summary == "" && len(snapshot.Facts) == 0 {
		return aiContextSnapshot{}, errors.New("AI context snapshot is empty")
	}
	return snapshot, nil
}

func validAIContextFactKind(kind string) bool {
	switch kind {
	case "decision", "constraint", "entity", "open_question", "progress":
		return true
	default:
		return false
	}
}

func renderAIContextFact(fact aiContextFact) string {
	return "[" + fact.Kind + "] " + fact.Content
}

func nextAIEntryTimestamp(candidate, previous string) string {
	candidateTime, candidateErr := time.Parse(time.RFC3339Nano, candidate)
	previousTime, previousErr := time.Parse(time.RFC3339Nano, previous)
	if candidateErr == nil && previousErr == nil && !candidateTime.After(previousTime) {
		return previousTime.Add(time.Nanosecond).Format(time.RFC3339Nano)
	}
	return candidate
}

func promptContextFromSnapshot(memories []string, snapshot aiContextSnapshot) modelclient.PromptContext {
	facts := make([]string, 0, len(snapshot.Facts))
	for _, fact := range snapshot.Facts {
		facts = append(facts, renderAIContextFact(fact))
	}
	return modelclient.PromptContext{Memories: memories, Summary: snapshot.Summary, Facts: facts}
}

func (a *API) activeAIContextSnapshot(sessionID string) (*models.AIMemoryEntry, aiContextSnapshot, error) {
	var row models.AIMemoryEntry
	err := a.db.Where("session_id = ? AND kind = 'context_snapshot' AND status = 'active'", sessionID).
		Order("created_at DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, aiContextSnapshot{}, nil
	}
	if err != nil {
		return nil, aiContextSnapshot{}, err
	}
	snapshot, err := decodeAIContextSnapshot(row.Content)
	if err != nil {
		return nil, aiContextSnapshot{}, err
	}
	return &row, snapshot, nil
}

func aiPromptMessages(rows []models.AIMessage) []aiPromptMessage {
	messages := make([]aiPromptMessage, 0, len(rows))
	for _, row := range rows {
		if row.Content == "" || row.Status == "failed" {
			continue
		}
		content := row.Content
		if row.Role == "assistant" {
			content = stripAIControlBlocks(content)
		}
		if content == "" {
			continue
		}
		messages = append(messages, aiPromptMessage{
			ID: row.ID, CreatedAt: row.CreatedAt,
			Message: modelclient.ChatMessage{Role: row.Role, Content: content},
		})
	}
	return messages
}

func groupAIPromptTurns(messages []aiPromptMessage) []aiPromptTurn {
	turns := make([]aiPromptTurn, 0, len(messages)/2+1)
	var turn []aiPromptMessage
	for _, message := range messages {
		if message.Message.Role == "user" {
			if len(turn) > 0 {
				turns = append(turns, aiPromptTurn{Messages: turn})
			}
			turn = []aiPromptMessage{message}
			continue
		}
		if len(turn) > 0 {
			turn = append(turn, message)
		}
	}
	if len(turn) > 0 {
		turns = append(turns, aiPromptTurn{Messages: turn})
	}
	return turns
}

func selectAIHistoryWindow(messages []aiPromptMessage, protocol, model string, promptContext modelclient.PromptContext) ([]modelclient.ChatMessage, *aiPromptMessage, error) {
	turns := groupAIPromptTurns(messages)
	selected := make([]modelclient.ChatMessage, 0, len(messages))
	var first *aiPromptMessage
	for index := len(turns) - 1; index >= 0; index-- {
		turnMessages := make([]modelclient.ChatMessage, 0, len(turns[index].Messages))
		for _, item := range turns[index].Messages {
			turnMessages = append(turnMessages, item.Message)
		}
		candidate := make([]modelclient.ChatMessage, 0, len(turnMessages)+len(selected))
		candidate = append(candidate, turnMessages...)
		candidate = append(candidate, selected...)
		size, err := modelclient.PromptSize(modelclient.Protocol(protocol), model, candidate, promptContext)
		if err != nil {
			return nil, nil, err
		}
		if size > modelclient.MaxPromptBytes {
			if len(selected) == 0 {
				return nil, nil, modelclient.ErrPromptTooLarge
			}
			break
		}
		selected = candidate
		firstItem := turns[index].Messages[0]
		first = &firstItem
	}
	return selected, first, nil
}

func (a *API) buildAICompactionCandidate(sessionID, protocol, model string, promptContext modelclient.PromptContext, active *models.AIMemoryEntry) (aiCompactionCandidate, error) {
	var latest []models.AIMessage
	if err := a.db.Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").Limit(200).Find(&latest).Error; err != nil {
		return aiCompactionCandidate{}, err
	}
	for left, right := 0, len(latest)-1; left < right; left, right = left+1, right-1 {
		latest[left], latest[right] = latest[right], latest[left]
	}
	_, firstSelected, err := selectAIHistoryWindow(aiPromptMessages(latest), protocol, model, promptContext)
	if err != nil || firstSelected == nil {
		return aiCompactionCandidate{}, err
	}

	rangeQuery := a.db.Model(&models.AIMessage{}).
		Where("session_id = ? AND status <> 'failed' AND content <> ''", sessionID).
		Where("created_at < ? OR (created_at = ? AND id < ?)", firstSelected.CreatedAt, firstSelected.CreatedAt, firstSelected.ID)
	sumQuery := `
		SELECT COALESCE(SUM(length(CAST(content AS BLOB))), 0)
		FROM ai_messages
		WHERE session_id = ?
		  AND status <> 'failed'
		  AND content <> ''
		  AND (created_at < ? OR (created_at = ? AND id < ?))
	`
	sumArgs := []any{sessionID, firstSelected.CreatedAt, firstSelected.CreatedAt, firstSelected.ID}
	if active != nil && active.SourceMessageID != nil {
		var watermark models.AIMessage
		if err := a.db.Where("id = ? AND session_id = ?", *active.SourceMessageID, sessionID).First(&watermark).Error; err != nil {
			return aiCompactionCandidate{}, err
		}
		rangeQuery = rangeQuery.Where("created_at > ? OR (created_at = ? AND id > ?)", watermark.CreatedAt, watermark.CreatedAt, watermark.ID)
		sumQuery += " AND (created_at > ? OR (created_at = ? AND id > ?))"
		sumArgs = append(sumArgs, watermark.CreatedAt, watermark.CreatedAt, watermark.ID)
	}
	var uncompressedBytes int64
	if err := a.db.Raw(sumQuery, sumArgs...).Row().Scan(&uncompressedBytes); err != nil {
		return aiCompactionCandidate{}, err
	}
	if uncompressedBytes <= aiCompactionTriggerBytes {
		return aiCompactionCandidate{}, nil
	}

	var rows []models.AIMessage
	if err := rangeQuery.Order("created_at ASC, id ASC").Limit(aiCompactionCandidateLimit).Find(&rows).Error; err != nil {
		return aiCompactionCandidate{}, err
	}
	truncated := len(rows) > aiCompactionCandidateWindow
	if truncated {
		rows = rows[:aiCompactionCandidateWindow]
	}
	turns := groupAIPromptTurns(aiPromptMessages(rows))
	if truncated && len(turns) > 0 {
		turns = turns[:len(turns)-1]
	}

	var candidate aiCompactionCandidate
	for _, turn := range turns {
		turnMessages := make([]modelclient.ChatMessage, 0, len(turn.Messages))
		turnBytes := 0
		for _, item := range turn.Messages {
			encoded, _ := json.Marshal(item.Message)
			turnBytes += len(encoded) + 1
			turnMessages = append(turnMessages, item.Message)
		}
		if candidate.InputBytes+turnBytes > aiCompactionBatchBytes {
			break
		}
		candidate.Messages = append(candidate.Messages, turnMessages...)
		candidate.MessageCount += len(turn.Messages)
		candidate.InputBytes += turnBytes
		candidate.SourceMessageID = turn.Messages[len(turn.Messages)-1].ID
	}
	return candidate, nil
}

func buildAICompactionInput(previous *models.AIMemoryEntry, candidate aiCompactionCandidate) string {
	previousContent := `{"summary":"","facts":[]}`
	if previous != nil {
		previousContent = previous.Content
	}
	var builder strings.Builder
	builder.WriteString("旧快照：\n")
	builder.WriteString(previousContent)
	builder.WriteString("\n\n待压缩对话（JSONL，按时间正序）：\n")
	for _, message := range candidate.Messages {
		encoded, _ := json.Marshal(message)
		builder.Write(encoded)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func (a *API) scheduleAICompaction(sessionID string, provider models.AIProvider) {
	if a.aiCompactions == nil {
		return
	}
	a.aiCompactions.start(sessionID, func(ctx context.Context) {
		if err := a.compactAISession(ctx, sessionID, provider); err != nil && !errors.Is(err, context.Canceled) {
			a.options.Logger.Printf("AI context compaction failed for session %s", sessionID)
		}
	})
}

func (a *API) compactAISession(ctx context.Context, sessionID string, provider models.AIProvider) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a.maintenance.RLock()
	active, snapshot, err := a.activeAIContextSnapshot(sessionID)
	if err == nil {
		memories := a.confirmedAIMemories()
		promptContext := promptContextFromSnapshot(memories, snapshot)
		memoryTools, registryErr := a.aiMemoryToolRegistry(sessionID)
		if registryErr != nil {
			err = registryErr
		} else {
			promptContext.Tools = memoryTools.Definitions()
		}
		var candidate aiCompactionCandidate
		if err == nil {
			candidate, err = a.buildAICompactionCandidate(sessionID, provider.Protocol, provider.Model, promptContext, active)
		}
		if err == nil && candidate.MessageCount == 0 {
			a.maintenance.RUnlock()
			return nil
		}
		if err == nil {
			input := buildAICompactionInput(active, candidate)
			a.maintenance.RUnlock()
			return a.runAICompaction(ctx, sessionID, provider, active, candidate, input)
		}
	}
	a.maintenance.RUnlock()
	return err
}

func (a *API) runAICompaction(ctx context.Context, sessionID string, provider models.AIProvider, active *models.AIMemoryEntry, candidate aiCompactionCandidate, input string) error {
	a.aiProviderMu.RLock()
	defer a.aiProviderMu.RUnlock()
	currentProvider, err := loadAIProvider(a.db.WithContext(ctx), provider.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if currentProvider.Version != provider.Version || currentProvider.Kind != provider.Kind ||
		currentProvider.Protocol != provider.Protocol || currentProvider.BaseURL != provider.BaseURL || currentProvider.Model != provider.Model {
		return nil
	}
	apiKey := ""
	if provider.Kind != aiProviderKindLocal {
		var err error
		apiKey, err = a.keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID))
		if err != nil {
			if errors.Is(err, keystore.ErrNotFound) {
				return nil
			}
			return err
		}
	}
	client := a.harnessClient
	if client == nil {
		client = harness.NewModelClient(nil)
	}
	turn, err := client.Stream(ctx, harness.Request{
		Protocol: provider.Protocol, BaseURL: provider.BaseURL, APIKey: apiKey, Model: provider.Model,
		SystemPrompt: aiCompactionSystemPrompt,
		History:      []modelclient.ChatMessage{{Role: "user", Content: input}},
	}, nil, nil)
	if err != nil {
		return err
	}
	next, err := decodeAIContextSnapshot(turn.Text)
	if err != nil {
		return err
	}
	return a.persistAIContextSnapshot(ctx, sessionID, active, next, candidate)
}

func (a *API) persistAIContextSnapshot(ctx context.Context, sessionID string, previous *models.AIMemoryEntry, snapshot aiContextSnapshot, candidate aiCompactionCandidate) error {
	content, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tagSet := make(map[string]struct{}, len(snapshot.Facts))
	for _, fact := range snapshot.Facts {
		tagSet[fact.Kind] = struct{}{}
	}
	tags := make([]string, 0, len(tagSet))
	for _, kind := range []string{"decision", "constraint", "entity", "open_question", "progress"} {
		if _, ok := tagSet[kind]; ok {
			tags = append(tags, kind)
		}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	now := nowStamp(a)
	if previous != nil {
		now = nextAIEntryTimestamp(now, previous.UpdatedAt)
	}
	entry := models.AIMemoryEntry{
		ID: uuid.NewString(), SessionID: sessionID, Kind: "context_snapshot",
		Content: string(content), Tags: string(tagsJSON), Origin: "model_compaction", Status: "active",
		SourceMessageID: &candidate.SourceMessageID, CreatedAt: now, UpdatedAt: now,
	}
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if previous != nil {
			result := tx.Model(&models.AIMemoryEntry{}).
				Where("id = ? AND session_id = ? AND status = 'active'", previous.ID, sessionID).
				Updates(map[string]any{"status": "superseded", "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("AI context snapshot changed during compaction")
			}
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"entry_id": entry.ID, "message_count": candidate.MessageCount,
			"input_bytes": candidate.InputBytes, "source_message_id": candidate.SourceMessageID,
		})
		if err != nil {
			return err
		}
		return tx.Table("workflow_events").Create(map[string]any{
			"id": uuid.NewString(), "aggregate_type": "ai_session", "aggregate_id": sessionID,
			"action": "ai_session_compacted", "actor_id": models.BuiltinSystemActorID,
			"previous_json": nil, "current_json": string(payload), "created_at": now,
		}).Error
	})
}
