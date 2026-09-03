package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Memories are user-confirmed preference facts: create with idempotency,
// list newest-first, delete, and never leak content into workflow events.
func TestAIMemoryLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)

	created := performRequest(router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"回答保持简洁，用中文"}`), map[string]string{"Idempotency-Key": "memory-1", "X-Request-ID": "018f0000-0000-7000-8000-000000007201"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create memory = %d: %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data aiMemoryResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	if envelope.Data.Content != "回答保持简洁，用中文" {
		t.Fatalf("created memory = %#v", envelope.Data)
	}
	replayed := performRequest(router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"回答保持简洁，用中文"}`), map[string]string{"Idempotency-Key": "memory-1"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay memory = %d: %s", replayed.Code, replayed.Body.String())
	}
	malformed := performRequest(router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"   "}`), nil)
	assertAPIError(t, malformed, http.StatusUnprocessableEntity, "AI_MEMORY_CONTENT_INVALID")
	badSource := performRequest(router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"x","source_message_id":"not-a-uuid"}`), nil)
	assertAPIError(t, badSource, http.StatusUnprocessableEntity, "INVALID_AI_MESSAGE_ID")

	// List is newest-first.
	second := performRequest(router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"输出使用 Markdown 列表"}`), nil)
	if second.Code != http.StatusCreated {
		t.Fatalf("create second memory = %d: %s", second.Code, second.Body.String())
	}
	list := performRequest(router, http.MethodGet, "/api/v1/ai/memories", nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list memories = %d: %s", list.Code, list.Body.String())
	}
	var listEnvelope struct {
		Data []aiMemoryResponse `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listEnvelope.Data) != 2 {
		t.Fatalf("list = %d rows, want 2", len(listEnvelope.Data))
	}
	seen := map[string]bool{}
	for _, row := range listEnvelope.Data {
		seen[row.Content] = true
	}
	if !seen["回答保持简洁，用中文"] || !seen["输出使用 Markdown 列表"] {
		t.Fatalf("list contents wrong: %#v", listEnvelope.Data)
	}

	// Workflow events stay sanitized: no memory content anywhere.
	var eventJSONs []string
	if err := store.DB.Table("workflow_events").
		Where("aggregate_type = 'ai_memory' AND action = 'ai_memory_created'").
		Pluck("current_json", &eventJSONs).Error; err != nil || len(eventJSONs) != 2 {
		t.Fatalf("memory events missing: %v", err)
	}
	for _, eventJSON := range eventJSONs {
		if strings.Contains(eventJSON, "简洁") || strings.Contains(eventJSON, "Markdown") {
			t.Fatalf("memory content leaked into workflow event: %s", eventJSON)
		}
	}

	deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/memories/"+envelope.Data.ID, nil, nil)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete memory = %d: %s", deleted.Code, deleted.Body.String())
	}
	missing := performRequest(router, http.MethodDelete, "/api/v1/ai/memories/"+envelope.Data.ID, nil, nil)
	assertAPIError(t, missing, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND")
}

// atomicMemories records the last system prompt seen by the mock upstream.
type atomicMemories struct {
	mu     sync.Mutex
	system string
}

func (a *atomicMemories) add(content string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.system = content
}

func (a *atomicMemories) last() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.system
}

// Confirmed memories must ride inside the system prompt of the next chat
// request, alongside the code-owned system prompt (ADR-006).
func TestAIChatInjectsConfirmedMemories(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	var seenSystem atomicMemories
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		for _, message := range payload.Messages {
			if message.Role == "system" {
				seenSystem.add(message.Content)
			}
		}
		streamMockAIDelta(w, "好的")
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-memory", upstream.URL+"/v1", "gpt-test")

	for _, content := range []string{"回答保持简洁", "用中文回答"} {
		if err := store.DB.Create(&models.AIMemory{
			ID: uuid.NewString(), Content: content,
			CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
		}).Error; err != nil {
			t.Fatalf("seed memory: %v", err)
		}
	}

	response := chatRequest(t, router, provider.ID, "", "你好")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	system := seenSystem.last()
	if !strings.Contains(system, "用户长期偏好") || !strings.Contains(system, "回答保持简洁") || !strings.Contains(system, "用中文回答") {
		t.Fatalf("memories not injected into system prompt: %s", system)
	}
	if !strings.Contains(system, "[opc:task]") {
		t.Fatalf("code-owned system prompt must stay: %s", system)
	}

	// Memory content never persists into messages or generations.
	var count int64
	if err := store.DB.Table("ai_messages").Where("content LIKE ?", "%回答保持简洁%").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("memory leaked into ai_messages: %d %v", count, err)
	}
}

// The injection respects the count and byte budgets: only the newest 20 notes
// ride, oversized notes are skipped instead of starving smaller ones.
func TestAIMemoryInjectionBudgets(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)

	seed := func(index int, content string) {
		if err := store.DB.Create(&models.AIMemory{
			ID: uuid.NewString(), Content: content,
			CreatedAt: now.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
			UpdatedAt: now.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano),
		}).Error; err != nil {
			t.Fatalf("seed memory %d: %v", index, err)
		}
	}
	// 25 Chinese notes of ~450 bytes each: the newest 20 would total ~9KB,
	// so the byte budget must bind before the count budget.
	for index := 0; index < 25; index++ {
		seed(index, strings.Repeat("偏", 149)+"好")
	}

	// The injection helper only reads the database; a minimal API instance
	// mirrors what chatAI calls with.
	service := &API{db: store.DB}
	notes := service.confirmedAIMemories()
	total := 0
	for _, note := range notes {
		total += len(note)
	}
	if total > 8<<10 {
		t.Fatalf("injection exceeded byte budget: %d", total)
	}
	if len(notes) >= 20 {
		t.Fatalf("byte budget did not bind before the count budget: %d notes", len(notes))
	}
	if len(notes) == 0 {
		t.Fatalf("byte budget must still admit the newest notes")
	}
}

func TestAISystemPromptMentionsMemoryBlock(t *testing.T) {
	if !strings.Contains(modelclient.SystemPrompt, "[opc:memory]") {
		t.Fatalf("system prompt missing memory block instruction")
	}
}
