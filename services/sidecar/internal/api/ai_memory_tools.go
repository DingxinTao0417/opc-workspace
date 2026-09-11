package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	aiSessionFactMaxRunes = 2000
	aiSessionFactMaxBytes = 8 << 10
	aiMemoryToolMaxTags   = 10
	aiMemoryToolTagRunes  = 50
	aiMemorySearchMaxRows = 100
)

type aiMemoryTool struct {
	api       *API
	sessionID string
	name      string
	ephemeral *aiEphemeralMemory
}

// Shared only by the three tools in one run; it is never serialized or logged.
type aiEphemeralMemory struct {
	mu      sync.Mutex
	entries []models.AIMemoryEntry
}

func (t *aiMemoryTool) Name() string { return t.name }

func (t *aiMemoryTool) Summary() string {
	switch t.name {
	case "memory_write":
		return "记录只在当前会话有效的工作事实、进度或约束；不写入跨会话长期记忆"
	case "memory_propose":
		return "提出一条需要用户确认后才能成为跨会话长期记忆的建议"
	case "memory_search":
		return "按关键词和可选标签检索当前会话的活动工作记忆与历史消息"
	default:
		return ""
	}
}

func (t *aiMemoryTool) InputSchema() json.RawMessage {
	switch t.name {
	case "memory_write":
		return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","minLength":1,"maxLength":2000},"tags":{"type":"array","items":{"type":"string","minLength":1,"maxLength":50},"maxItems":10}},"required":["content"],"additionalProperties":false}`)
	case "memory_propose":
		return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","minLength":1,"maxLength":500},"tags":{"type":"array","items":{"type":"string","minLength":1,"maxLength":50},"maxItems":10}},"required":["content"],"additionalProperties":false}`)
	case "memory_search":
		return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200},"tags":{"type":"array","items":{"type":"string","minLength":1,"maxLength":50},"maxItems":10},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`)
	default:
		return json.RawMessage(`{"type":"object","additionalProperties":false}`)
	}
}

func (t *aiMemoryTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.api.maintenance != nil {
		t.api.maintenance.RLock()
		defer t.api.maintenance.RUnlock()
	}
	if t.api.restorePending.Load() {
		return "", errors.New("workspace restore is pending")
	}
	switch t.name {
	case "memory_write":
		return t.write(ctx, arguments, "session_fact", "agent", aiSessionFactMaxRunes, aiSessionFactMaxBytes)
	case "memory_propose":
		return t.write(ctx, arguments, "memory_proposal", "model_proposal", 500, aiSessionFactMaxBytes)
	case "memory_search":
		return t.search(ctx, arguments)
	default:
		return "", errors.New("unknown memory tool")
	}
}

type aiMemoryWriteArguments struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

func (t *aiMemoryTool) write(ctx context.Context, arguments json.RawMessage, kind, origin string, maxRunes, maxBytes int) (string, error) {
	var input aiMemoryWriteArguments
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return "", err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" || !utf8.ValidString(content) || utf8.RuneCountInString(content) > maxRunes || len(content) > maxBytes {
		return "", errors.New("memory content is invalid")
	}
	tags, err := normalizeAIMemoryToolTags(input.Tags)
	if err != nil {
		return "", err
	}
	tagsJSON, _ := json.Marshal(tags)
	now := nowStamp(t.api)
	if t.ephemeral != nil {
		t.ephemeral.mu.Lock()
		defer t.ephemeral.mu.Unlock()
		var entry models.AIMemoryEntry
		for _, existing := range t.ephemeral.entries {
			if existing.Kind == kind && existing.Content == content && existing.Tags == string(tagsJSON) {
				entry = existing
				break
			}
		}
		if entry.ID == "" {
			if len(t.ephemeral.entries) >= aiMemorySearchMaxRows {
				return "", errors.New("temporary memory capacity reached")
			}
			entry = models.AIMemoryEntry{ID: uuid.NewString(), SessionID: t.sessionID, Kind: kind, Content: content, Tags: string(tagsJSON), Origin: origin, Status: "active", CreatedAt: now, UpdatedAt: now}
			t.ephemeral.entries = append(t.ephemeral.entries, entry)
		}
		result := map[string]any{"entry_id": entry.ID, "kind": kind, "status": "active", "persistence": "temporary"}
		if kind == "memory_proposal" {
			result["content"] = content
			result["confirmation_required"] = true
			result["instruction"] = "仅当前运行保留；永久记忆需要用户另行明确确认，输出不带 proposal_id 的记忆建议"
		}
		encoded, _ := json.Marshal(result)
		return string(encoded), nil
	}
	var entry models.AIMemoryEntry
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where(
			"session_id = ? AND kind = ? AND origin = ? AND status = 'active' AND content = ? AND tags = ?",
			t.sessionID, kind, origin, content, string(tagsJSON),
		).First(&entry)
		if query.Error == nil {
			return nil
		}
		if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		entry = models.AIMemoryEntry{
			ID: uuid.NewString(), SessionID: t.sessionID, Kind: kind, Content: content,
			Tags: string(tagsJSON), Origin: origin, Status: "active", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		action := "ai_session_memory_written"
		if kind == "memory_proposal" {
			action = "ai_memory_proposed"
		}
		payload, _ := json.Marshal(map[string]any{"entry_id": entry.ID, "kind": kind, "tag_count": len(tags)})
		return tx.Table("workflow_events").Create(map[string]any{
			"id": uuid.NewString(), "aggregate_type": "ai_session", "aggregate_id": t.sessionID,
			"action": action, "actor_id": models.BuiltinSystemActorID,
			"previous_json": nil, "current_json": string(payload), "created_at": now,
		}).Error
	})
	if err != nil {
		return "", safeAIMemoryToolStorageError(ctx)
	}
	result := map[string]any{"entry_id": entry.ID, "kind": kind, "status": entry.Status}
	if kind == "memory_proposal" {
		result["proposal_id"] = entry.ID
		result["content"] = entry.Content
		result["confirmation_required"] = true
	}
	encoded, _ := json.Marshal(result)
	return string(encoded), nil
}

type aiMemorySearchArguments struct {
	Query string   `json:"query"`
	Tags  []string `json:"tags"`
	Limit int      `json:"limit"`
}

type aiMemorySearchResult struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"-"`
}

func (t *aiMemoryTool) search(ctx context.Context, arguments json.RawMessage) (string, error) {
	var input aiMemorySearchArguments
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return "", err
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || utf8.RuneCountInString(query) > 200 {
		return "", errors.New("memory search query is invalid")
	}
	tags, err := normalizeAIMemoryToolTags(input.Tags)
	if err != nil {
		return "", err
	}
	limit := input.Limit
	if limit == 0 {
		limit = 5
	}
	if limit < 1 || limit > 10 {
		return "", errors.New("memory search limit is invalid")
	}

	var entries []models.AIMemoryEntry
	if t.ephemeral != nil {
		t.ephemeral.mu.Lock()
		entries = append(entries, t.ephemeral.entries...)
		t.ephemeral.mu.Unlock()
	} else if err := t.api.db.WithContext(ctx).
		Where("session_id = ? AND status = 'active' AND kind IN ?", t.sessionID, []string{"context_snapshot", "session_fact"}).
		Order("created_at DESC, id DESC").Limit(aiMemorySearchMaxRows).Find(&entries).Error; err != nil {
		return "", safeAIMemoryToolStorageError(ctx)
	}
	lowerQuery := strings.ToLower(query)
	results := make([]aiMemorySearchResult, 0, limit)
	for _, entry := range entries {
		var entryTags []string
		if json.Unmarshal([]byte(entry.Tags), &entryTags) != nil || !matchesAIMemoryTags(entryTags, tags) {
			continue
		}
		if !strings.Contains(strings.ToLower(entry.Content), lowerQuery) && !containsAIMemoryTag(entryTags, lowerQuery) {
			continue
		}
		results = append(results, aiMemorySearchResult{
			Kind: entry.Kind, ID: entry.ID, Content: boundedAIMemorySearchContent(entry.Content),
			Tags: entryTags, CreatedAt: entry.CreatedAt,
		})
	}
	if len(tags) == 0 && t.ephemeral == nil {
		var messages []models.AIMessage
		like := "%" + escapeLike(query) + "%"
		if err := t.api.db.WithContext(ctx).
			Where("session_id = ? AND status <> 'failed' AND content LIKE ? ESCAPE '\\'", t.sessionID, like).
			Order("created_at DESC, id DESC").Limit(limit).Find(&messages).Error; err != nil {
			return "", safeAIMemoryToolStorageError(ctx)
		}
		for _, message := range messages {
			content := message.Content
			if message.Role == "assistant" {
				content = stripAIControlBlocks(content)
			}
			if content == "" {
				continue
			}
			results = append(results, aiMemorySearchResult{
				Kind: "message_" + message.Role, ID: message.ID,
				Content: boundedAIMemorySearchContent(content), CreatedAt: message.CreatedAt,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].CreatedAt == results[j].CreatedAt {
			return results[i].ID > results[j].ID
		}
		return results[i].CreatedAt > results[j].CreatedAt
	})
	if len(results) > limit {
		results = results[:limit]
	}
	encoded, _ := json.Marshal(map[string]any{"results": results})
	return string(encoded), nil
}

func safeAIMemoryToolStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("memory storage is unavailable")
}

func decodeStrictToolArguments(arguments json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("tool arguments are invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("tool arguments contain trailing data")
	}
	return nil
}

func normalizeAIMemoryToolTags(input []string) ([]string, error) {
	if len(input) > aiMemoryToolMaxTags {
		return nil, errors.New("too many memory tags")
	}
	seen := make(map[string]struct{}, len(input))
	tags := make([]string, 0, len(input))
	for _, value := range input {
		tag := strings.ToLower(strings.TrimSpace(value))
		if tag == "" || utf8.RuneCountInString(tag) > aiMemoryToolTagRunes || strings.ContainsAny(tag, "\r\n\t") {
			return nil, errors.New("memory tag is invalid")
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

func matchesAIMemoryTags(entryTags, requested []string) bool {
	for _, tag := range requested {
		if !containsAIMemoryTag(entryTags, tag) {
			return false
		}
	}
	return true
}

func containsAIMemoryTag(tags []string, query string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func boundedAIMemorySearchContent(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > 500 {
		return string(runes[:500]) + "…"
	}
	return string(runes)
}

func (a *API) aiMemoryToolRegistry(sessionID string, persist ...bool) (*harness.Registry, error) {
	durable := true
	if len(persist) > 0 {
		durable = persist[0]
	} else {
		var session models.AISession
		if err := a.db.Select("persist").Where("id = ?", sessionID).First(&session).Error; err != nil {
			return nil, err
		}
		durable = session.Persist
	}
	var ephemeral *aiEphemeralMemory
	if !durable {
		ephemeral = &aiEphemeralMemory{}
	}
	return harness.NewRegistry(
		&aiMemoryTool{api: a, sessionID: sessionID, name: "memory_search", ephemeral: ephemeral},
		&aiMemoryTool{api: a, sessionID: sessionID, name: "memory_write", ephemeral: ephemeral},
		&aiMemoryTool{api: a, sessionID: sessionID, name: "memory_propose", ephemeral: ephemeral},
	)
}
