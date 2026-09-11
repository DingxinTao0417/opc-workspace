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
	Messages            []modelclient.ChatMessage
	MessageCount        int
	InputBytes          int
	SourceMessageID     string
	SourceMessageOffset int
}

// aiCompactionRegistry gives each session at most one background compaction
// and owns cancellation during session deletion or Sidecar shutdown.
type aiCompactionRegistry struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	active map[string]context.CancelFunc
	states map[string]aiCompactionState
	wg     sync.WaitGroup
	closed bool
}

func newAICompactionRegistry() *aiCompactionRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &aiCompactionRegistry{ctx: ctx, cancel: cancel, active: make(map[string]context.CancelFunc), states: make(map[string]aiCompactionState)}
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
	if len(latest) == 0 {
		return aiCompactionCandidate{}, nil
	}
	promptMessages := aiPromptMessages(latest)
	// Reserve a next user turn. Otherwise a final oversized assistant reply
	// can itself make the history window invalid and prevent any compaction.
	last := latest[len(latest)-1]
	promptMessages = append(promptMessages, aiPromptMessage{ID: last.ID + "~", CreatedAt: last.CreatedAt, Message: modelclient.ChatMessage{Role: "user", Content: "继续"}})
	_, firstSelected, err := selectAIHistoryWindow(promptMessages, protocol, model, promptContext)
	if err != nil || firstSelected == nil {
		return aiCompactionCandidate{}, err
	}
	var watermark models.AIMessage
	if active != nil && active.SourceMessageID != nil {
		if err := a.db.Where("id = ? AND session_id = ?", *active.SourceMessageID, sessionID).First(&watermark).Error; err != nil {
			return aiCompactionCandidate{}, err
		}
		// A smaller replacement summary can pull a partially summarized
		// message back into the live window. Finish that source regardless of
		// the moved boundary so its durable offset never gets stranded.
		if active.SourceMessageOffset > 0 && (watermark.CreatedAt > firstSelected.CreatedAt || (watermark.CreatedAt == firstSelected.CreatedAt && watermark.ID >= firstSelected.ID)) {
			firstSelected = &aiPromptMessage{CreatedAt: watermark.CreatedAt, ID: watermark.ID + "~"}
		}
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
		comparison := ">"
		if active.SourceMessageOffset > 0 {
			comparison = ">="
		}
		rangeQuery = rangeQuery.Where("created_at > ? OR (created_at = ? AND id "+comparison+" ?)", watermark.CreatedAt, watermark.CreatedAt, watermark.ID)
		sumQuery += " AND (created_at > ? OR (created_at = ? AND id " + comparison + " ?))"
		sumArgs = append(sumArgs, watermark.CreatedAt, watermark.CreatedAt, watermark.ID)
	}
	var uncompressedBytes int64
	if err := a.db.Raw(sumQuery, sumArgs...).Row().Scan(&uncompressedBytes); err != nil {
		return aiCompactionCandidate{}, err
	}
	if active != nil {
		uncompressedBytes -= int64(active.SourceMessageOffset)
	}
	if uncompressedBytes <= aiCompactionTriggerBytes && (active == nil || active.SourceMessageOffset == 0) {
		return aiCompactionCandidate{}, nil
	}

	var rows []models.AIMessage
	if err := rangeQuery.Order("created_at ASC, id ASC").Limit(aiCompactionCandidateLimit).Find(&rows).Error; err != nil {
		return aiCompactionCandidate{}, err
	}
	if len(rows) > aiCompactionCandidateWindow {
		rows = rows[:aiCompactionCandidateWindow]
	}
	var candidate aiCompactionCandidate
	for _, item := range aiPromptMessages(rows) {
		offset := 0
		if active != nil && active.SourceMessageID != nil && *active.SourceMessageID == item.ID {
			offset = active.SourceMessageOffset
		}
		if offset < 0 || offset > len(item.Message.Content) || !utf8.ValidString(item.Message.Content[:offset]) {
			return aiCompactionCandidate{}, errors.New("AI context watermark offset is invalid")
		}
		remaining := item.Message.Content[offset:]
		if remaining == "" {
			continue
		}
		fits := func(n int) bool {
			message := modelclient.ChatMessage{Role: item.Message.Role, Content: remaining[:utf8PrefixLength(remaining, n)]}
			encoded, _ := json.Marshal(message)
			if candidate.InputBytes+len(encoded)+1 > aiCompactionBatchBytes {
				return false
			}
			probe := candidate
			probe.Messages = append(append([]modelclient.ChatMessage(nil), candidate.Messages...), message)
			size, sizeErr := modelclient.PromptSize(modelclient.Protocol(protocol), model, []modelclient.ChatMessage{{Role: "user", Content: buildAICompactionInput(active, probe)}}, modelclient.PromptContext{SystemPrompt: aiCompactionSystemPrompt})
			return sizeErr == nil && size <= modelclient.MaxPromptBytes
		}
		low, high := 0, len(remaining)
		for low < high {
			middle := (low + high + 1) / 2
			if fits(middle) {
				low = middle
			} else {
				high = middle - 1
			}
		}
		take := utf8PrefixLength(remaining, low)
		if take == 0 {
			break
		}
		message := modelclient.ChatMessage{Role: item.Message.Role, Content: remaining[:take]}
		encoded, _ := json.Marshal(message)
		candidate.Messages = append(candidate.Messages, message)
		candidate.InputBytes += len(encoded) + 1
		candidate.SourceMessageID = item.ID
		candidate.SourceMessageOffset = 0
		if take < len(remaining) {
			candidate.SourceMessageOffset = offset + take
			break
		}
		candidate.MessageCount++
	}
	return candidate, nil
}

func utf8PrefixLength(value string, n int) int {
	if n >= len(value) {
		return len(value)
	}
	for n > 0 && !utf8.RuneStart(value[n]) {
		n--
	}
	return n
}

func buildAICompactionInput(previous *models.AIMemoryEntry, candidate aiCompactionCandidate) string {
	previousContent := `{"summary":"","facts":[]}`
	if previous != nil {
		previousContent = previous.Content
	}
	var builder strings.Builder
	builder.WriteString("旧快照：\n")
	builder.WriteString(previousContent)
	builder.WriteString("\n\n待压缩对话（JSONL，按时间正序；超长消息可分成连续片段，每片只包含尚未压缩的内容，需与旧快照合并）：\n")
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
		ctx, cancel := context.WithTimeout(ctx, modelclient.TotalTimeout)
		defer cancel()
		for batch := 0; batch < 8; batch++ {
			progressed, err := a.compactAISessionBatch(ctx, sessionID, provider)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.options.Logger.Printf("AI context compaction failed for session %s", sessionID)
				}
				return
			}
			if !progressed {
				return
			}
		}
		a.setAICompactionState(sessionID, "pending", "")
	})
}

func (a *API) compactAISession(ctx context.Context, sessionID string, provider models.AIProvider) error {
	_, err := a.compactAISessionBatch(ctx, sessionID, provider)
	return err
}

func (a *API) compactAISessionBatch(ctx context.Context, sessionID string, provider models.AIProvider) (progressed bool, err error) {
	a.setAICompactionState(sessionID, "running", "")
	defer func() {
		state, code := "idle", ""
		if progressed {
			state = "succeeded"
		}
		if err != nil {
			state, code = "failed", "AI_COMPACTION_FAILED"
			if errors.Is(err, errAICompactionProviderChanged) {
				code = "AI_COMPACTION_PROVIDER_CHANGED"
			}
			if errors.Is(err, context.Canceled) {
				state, code = "cancelled", ""
			}
		}
		a.setAICompactionState(sessionID, state, code)
	}()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	a.maintenance.RLock()
	if a.restorePending.Load() {
		a.maintenance.RUnlock()
		return false, context.Canceled
	}
	var session models.AISession
	if err := a.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		a.maintenance.RUnlock()
		return false, err
	}
	if !session.Persist {
		a.maintenance.RUnlock()
		return false, nil
	}
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
		if err == nil && len(candidate.Messages) == 0 {
			a.maintenance.RUnlock()
			return false, nil
		}
		if err == nil {
			input := buildAICompactionInput(active, candidate)
			a.maintenance.RUnlock()
			err = a.runAICompaction(ctx, sessionID, provider, active, candidate, input)
			return err == nil, err
		}
	}
	a.maintenance.RUnlock()
	return false, err
}

var errAICompactionProviderChanged = errors.New("AI compaction provider configuration changed")

func (a *API) runAICompaction(ctx context.Context, sessionID string, provider models.AIProvider, active *models.AIMemoryEntry, candidate aiCompactionCandidate, input string) error {
	a.maintenance.RLock()
	a.aiProviderMu.RLock()
	apiKey, err := func() (string, error) {
		if a.restorePending.Load() {
			return "", context.Canceled
		}
		if err := a.checkAICompactionProvider(ctx, provider); err != nil {
			return "", err
		}
		if provider.Kind == aiProviderKindLocal {
			return "", nil
		}
		return a.keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID))
	}()
	a.aiProviderMu.RUnlock()
	a.maintenance.RUnlock()
	if err != nil {
		return err
	}
	client := a.harnessClient
	if client == nil {
		client = harness.NewModelClient(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, modelclient.TotalTimeout)
	defer cancel()
	turn, err := client.Stream(ctx, harness.Request{
		ResponseByteLimit: modelclient.MaxResponseBytes,
		Protocol:          provider.Protocol, BaseURL: provider.BaseURL, APIKey: apiKey, Model: provider.Model,
		SystemPrompt: aiCompactionSystemPrompt,
		History:      []modelclient.ChatMessage{{Role: "user", Content: input}},
	}, nil, nil)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(turn.Text)+len(turn.Reasoning) > modelclient.MaxResponseBytes {
		return modelclient.ErrResponseBudget
	}
	next, err := decodeAIContextSnapshot(turn.Text)
	if err != nil {
		return err
	}
	return a.persistAIContextSnapshot(ctx, sessionID, active, next, candidate, provider)
}

func (a *API) checkAICompactionProvider(ctx context.Context, provider models.AIProvider) error {
	current, err := loadAIProvider(a.db.WithContext(ctx), provider.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errAICompactionProviderChanged
	}
	if err != nil {
		return err
	}
	if current.ConfigVersion != provider.ConfigVersion || current.Kind != provider.Kind || current.Protocol != provider.Protocol || current.BaseURL != provider.BaseURL || current.Model != provider.Model || current.Status != "ready" {
		return errAICompactionProviderChanged
	}
	return nil
}

func (a *API) persistAIContextSnapshot(ctx context.Context, sessionID string, previous *models.AIMemoryEntry, snapshot aiContextSnapshot, candidate aiCompactionCandidate, providers ...models.AIProvider) error {
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
		SourceMessageOffset: candidate.SourceMessageOffset,
	}
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	if a.restorePending.Load() {
		return context.Canceled
	}
	a.aiProviderMu.RLock()
	defer a.aiProviderMu.RUnlock()
	if len(providers) > 0 {
		if err := a.checkAICompactionProvider(ctx, providers[0]); err != nil {
			return err
		}
	}
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Where("id = ?", sessionID).First(&session).Error; err != nil {
			return err
		}
		if !session.Persist {
			return errors.New("nonpersistent sessions cannot retain context snapshots")
		}
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
			"source_message_offset": candidate.SourceMessageOffset,
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
