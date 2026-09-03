package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newMockAIUpstream(chatHandler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"object":"list"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			chatHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
}

func createReadyAIProvider(t *testing.T, router interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, name, baseURL, model string) aiProviderResponse {
	t.Helper()
	body := []byte(`{"name":"` + name + `","protocol":"openai_chat","base_url":"` + baseURL + `","model":"` + model + `"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider = %d: %s", created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	keySet := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/key", []byte(`{"api_key":"sk-test"}`), map[string]string{"If-Match": `"1"`})
	if keySet.Code != http.StatusOK {
		t.Fatalf("set key = %d: %s", keySet.Code, keySet.Body.String())
	}
	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", checked.Code, checked.Body.String())
	}
	return envelope.Data
}

func chatRequest(t *testing.T, router interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, providerID, sessionID, message string) *httptest.ResponseRecorder {
	t.Helper()
	payload := `{"provider_id":"` + providerID + `","session_id":"` + sessionID + `","message":"` + message + `"}`
	return performRequest(router, http.MethodPost, "/api/v1/ai/chat", []byte(payload), nil)
}

func TestAIChatStreamsOpenAIDeltasAndPersistsTurn(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode chat payload: %v", err)
		}
		if !payload.Stream || payload.Model != "gpt-test" {
			t.Errorf("unexpected chat payload model=%q stream=%v", payload.Model, payload.Stream)
		}
		if len(payload.Messages) < 2 || payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[0].Content, "[opc:task]") {
			t.Errorf("chat payload missing code-owned system prompt: %+v", payload.Messages)
		}
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role != "user" || last.Content != "帮我总结今天的工作" {
			t.Errorf("chat payload missing current user message: %+v", last)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		flusher.Flush()
		for _, delta := range []string{"今天的", "工作已完成"} {
			frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": delta}}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-openai", upstream.URL+"/v1", "gpt-test")

	response := chatRequest(t, router, provider.ID, "", "帮我总结今天的工作")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: meta") || !strings.Contains(body, `"sse_protocol":"opc-ai-sse-v1"`) {
		t.Fatalf("chat missing meta event: %s", body)
	}
	if !strings.Contains(body, "event: delta") || !strings.Contains(body, "今天的") || !strings.Contains(body, "工作已完成") {
		t.Fatalf("chat missing deltas: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("chat missing done event: %s", body)
	}
	var sessionCount, userMessages, assistantMessages, completed int64
	_ = store.DB.Raw("SELECT COUNT(*) FROM ai_sessions").Scan(&sessionCount).Error
	_ = store.DB.Raw("SELECT COUNT(*) FROM ai_messages WHERE role = 'user'").Scan(&userMessages).Error
	_ = store.DB.Raw("SELECT COUNT(*) FROM ai_messages WHERE role = 'assistant' AND status = 'completed'").Scan(&assistantMessages).Error
	_ = store.DB.Raw("SELECT COUNT(*) FROM ai_generations WHERE status = 'completed'").Scan(&completed).Error
	if sessionCount != 1 || userMessages != 1 || assistantMessages != 1 || completed != 1 {
		t.Fatalf("chat persistence sessions=%d user=%d assistant=%d completed=%d", sessionCount, userMessages, assistantMessages, completed)
	}
	var assistantContent string
	_ = store.DB.Raw("SELECT content FROM ai_messages WHERE role = 'assistant'").Scan(&assistantContent).Error
	if assistantContent != "今天的工作已完成" {
		t.Fatalf("assistant content = %q", assistantContent)
	}
}

func TestAIChatStreamsReasoningSeparatelyAndPersistsIt(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frames := []map[string]any{
			{"choices": []map[string]any{{"delta": map[string]any{"reasoning_content": "先分析任务意图"}}}},
			{"choices": []map[string]any{{"delta": map[string]any{"reasoning_content": "，再组织回答"}}}},
			{"choices": []map[string]any{{"delta": map[string]any{"content": "好的"}}}},
			{"choices": []map[string]any{{"delta": map[string]any{}, "finish_reason": "stop"}}},
		}
		for _, frame := range frames {
			encoded, _ := json.Marshal(frame)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-reasoning", upstream.URL+"/v1", "deepseek-r1")
	response := chatRequest(t, router, provider.ID, "", "推理一个问题")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: reasoning") || !strings.Contains(body, "先分析任务意图") {
		t.Fatalf("chat missing reasoning events: %s", body)
	}
	if !strings.Contains(body, "event: delta") || !strings.Contains(body, "好的") {
		t.Fatalf("chat missing delta events: %s", body)
	}
	var content, reasoning string
	if err := store.DB.Raw("SELECT content, COALESCE(reasoning,'') FROM ai_messages WHERE role = 'assistant'").Row().Scan(&content, &reasoning); err != nil {
		t.Fatalf("read assistant message: %v", err)
	}
	if content != "好的" || reasoning != "先分析任务意图，再组织回答" {
		t.Fatalf("assistant content=%q reasoning=%q", content, reasoning)
	}
	// reasoning must never leak into the reply content or the list payload before reasoning
	var leaked int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%先分析%'").Scan(&leaked).Error; err != nil {
		t.Fatalf("count leaked reasoning: %v", err)
	}
	if leaked != 0 {
		t.Fatal("reasoning text leaked into reply content")
	}
}

func TestAIChatKeepsTaskSuggestionBlockInRawContent(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": "好的，建议如下\n[opc:task]{\"title\":\"写周报\",\"due\":\"2026-09-02\"}[/opc:task]"}}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-card", upstream.URL+"/v1", "gpt-test")
	response := chatRequest(t, router, provider.ID, "", "建个任务提醒我写周报")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	var content string
	if err := store.DB.Raw("SELECT content FROM ai_messages WHERE role = 'assistant'").Scan(&content).Error; err != nil {
		t.Fatalf("read assistant content: %v", err)
	}
	if !strings.Contains(content, `[opc:task]{"title":"写周报","due":"2026-09-02"}[/opc:task]`) {
		t.Fatalf("task suggestion block missing from raw content: %q", content)
	}
}

func TestAIChatMapsAnthropicDeltas(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			if r.Header.Get("x-api-key") != "sk-test" {
				t.Errorf("anthropic chat key header = %q", r.Header.Get("x-api-key"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"收到\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			flusher.Flush()
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	body := []byte(`{"name":"chat-anthropic","protocol":"anthropic_messages","base_url":"` + upstream.URL + `","model":"claude-test"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create anthropic provider = %d: %s", created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/key", []byte(`{"api_key":"sk-test"}`), map[string]string{"If-Match": `"1"`})
	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("anthropic health = %d: %s", checked.Code, checked.Body.String())
	}
	response := chatRequest(t, router, envelope.Data.ID, "", "你好")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "收到") || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("anthropic chat = %d: %s", response.Code, response.Body.String())
	}
	var assistantContent string
	_ = store.DB.Raw("SELECT content FROM ai_messages WHERE role = 'assistant'").Scan(&assistantContent).Error
	if assistantContent != "收到" {
		t.Fatalf("anthropic assistant content = %q", assistantContent)
	}
}

func TestAIChatCancelStopsUpstreamAndKeepsPartial(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	chatStarted := make(chan struct{})
	var startOnce sync.Once
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(chatStarted) })
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": "部分"}}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		flusher.Flush()
		<-r.Context().Done()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-cancel", upstream.URL+"/v1", "gpt-test")

	type chatResult struct {
		body string
		code int
	}
	resultCh := make(chan chatResult, 1)
	go func() {
		response := chatRequest(t, router, provider.ID, "", "慢慢回答这个问题")
		resultCh <- chatResult{response.Body.String(), response.Code}
	}()
	<-chatStarted
	var generationID string
	deadline := time.Now().Add(5 * time.Second)
	for generationID == "" && time.Now().Before(deadline) {
		_ = store.DB.Raw("SELECT id FROM ai_generations WHERE status = 'streaming' LIMIT 1").Scan(&generationID).Error
		if generationID == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if generationID == "" {
		t.Fatal("streaming generation row never appeared")
	}
	// the user turn is persisted when the generation starts, not at the end
	var userTurns int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM ai_messages WHERE role = 'user'").Scan(&userTurns).Error; err != nil {
		t.Fatalf("count user turns: %v", err)
	}
	if userTurns != 1 {
		t.Fatalf("user turn persisted during streaming = %d, want 1", userTurns)
	}
	// give the flushed upstream delta a deterministic window to reach the stream loop
	time.Sleep(500 * time.Millisecond)
	cancelled := performRequest(router, http.MethodPost, "/api/v1/ai/generations/"+generationID+"/cancel", nil, nil)
	if cancelled.Code != http.StatusAccepted {
		t.Fatalf("cancel = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	select {
	case result := <-resultCh:
		if result.code != http.StatusOK || !strings.Contains(result.body, "event: cancelled") || !strings.Contains(result.body, "部分") {
			t.Fatalf("cancelled chat = %d: %s", result.code, result.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled chat request never completed")
	}
	var status, partial string
	if err := store.DB.Raw("SELECT status, COALESCE(content,'') FROM ai_generations WHERE id = ?", generationID).Row().Scan(&status, &partial); err != nil {
		t.Fatalf("read generation: %v", err)
	}
	if status != "cancelled" || partial != "部分" {
		t.Fatalf("cancelled generation status=%q partial=%q", status, partial)
	}
	var assistantStatus, assistantContent string
	if err := store.DB.Raw("SELECT status, content FROM ai_messages WHERE role = 'assistant'").Row().Scan(&assistantStatus, &assistantContent); err != nil {
		t.Fatalf("read assistant message: %v", err)
	}
	if assistantStatus != "cancelled" || assistantContent != "部分" {
		t.Fatalf("cancelled assistant message status=%q content=%q", assistantStatus, assistantContent)
	}
}

func TestAIStaleSessionDeleteDoesNotCancelActiveGeneration(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	chatStarted := make(chan struct{})
	var startOnce sync.Once
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(chatStarted) })
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-stale-session-delete", upstream.URL+"/v1", "gpt-test")
	createdSession := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{}`), nil)
	var sessionEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdSession.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = chatRequest(t, router, provider.ID, sessionEnvelope.Data.ID, "保持生成")
	}()
	<-chatStarted
	staleDelete := performRequest(router, http.MethodDelete, "/api/v1/ai/sessions/"+sessionEnvelope.Data.ID, nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, staleDelete, http.StatusConflict, "VERSION_CONFLICT")
	select {
	case <-done:
		t.Fatal("stale session delete cancelled the active generation")
	case <-time.After(200 * time.Millisecond):
	}

	var generationID string
	if err := store.DB.Raw("SELECT id FROM ai_generations WHERE session_id = ? AND status = 'streaming'", sessionEnvelope.Data.ID).Scan(&generationID).Error; err != nil || generationID == "" {
		t.Fatalf("active generation missing after stale delete: id=%q err=%v", generationID, err)
	}
	if cancelled := performRequest(router, http.MethodPost, "/api/v1/ai/generations/"+generationID+"/cancel", nil, nil); cancelled.Code != http.StatusAccepted {
		t.Fatalf("cleanup cancel = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("active chat did not stop after explicit cancel")
	}
}

func TestAIChatBusyWhileGenerationActive(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	chatStarted := make(chan struct{})
	var startOnce sync.Once
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		startOnce.Do(func() { close(chatStarted) })
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-busy", upstream.URL+"/v1", "gpt-test")

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = chatRequest(t, router, provider.ID, "", "第一个问题")
	}()
	<-chatStarted
	busy := chatRequest(t, router, provider.ID, "", "第二个问题")
	if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), "AI_PROVIDER_BUSY") {
		t.Fatalf("second chat = %d: %s", busy.Code, busy.Body.String())
	}
	var generationID string
	deadline := time.Now().Add(5 * time.Second)
	for generationID == "" && time.Now().Before(deadline) {
		_ = store.DB.Raw("SELECT id FROM ai_generations WHERE status = 'streaming' LIMIT 1").Scan(&generationID).Error
		if generationID == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if generationID == "" {
		t.Fatal("streaming generation row never appeared")
	}
	if cancelled := performRequest(router, http.MethodPost, "/api/v1/ai/generations/"+generationID+"/cancel", nil, nil); cancelled.Code != http.StatusAccepted {
		t.Fatalf("cancel first generation = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("first chat never completed")
	}
}

func TestAIChatGatesAndValidation(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, _, keyStore := newAIProviderTestRouter(t, now)
	if response := chatRequest(t, router, "not-a-uuid", "", "你好"); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "INVALID_AI_PROVIDER_ID") {
		t.Fatalf("chat invalid provider = %d: %s", response.Code, response.Body.String())
	}
	missingProvider := chatRequest(t, router, "018f0000-0000-5000-8000-000000009991", "", "你好")
	if missingProvider.Code != http.StatusNotFound || !strings.Contains(missingProvider.Body.String(), "AI_PROVIDER_NOT_FOUND") {
		t.Fatalf("chat missing provider = %d: %s", missingProvider.Code, missingProvider.Body.String())
	}
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-gates", upstream.URL+"/v1", "gpt-test")
	if response := chatRequest(t, router, provider.ID, "", "   "); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "AI_MESSAGE_INVALID") {
		t.Fatalf("chat empty message = %d: %s", response.Code, response.Body.String())
	}
	if response := chatRequest(t, router, provider.ID, "not-a-uuid", "你好"); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "INVALID_AI_SESSION_ID") {
		t.Fatalf("chat invalid session = %d: %s", response.Code, response.Body.String())
	}
	if response := chatRequest(t, router, provider.ID, "018f0000-0000-5000-8000-000000009992", "你好"); response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "AI_SESSION_NOT_FOUND") {
		t.Fatalf("chat missing session = %d: %s", response.Code, response.Body.String())
	}
	// remove the stored key behind a ready provider: key unavailability surfaces as a stable conflict
	if err := keyStore.Delete(aiProviderKeyService, aiProviderKeyAccount(provider.ID)); err != nil {
		t.Fatalf("delete stored key: %v", err)
	}
	if response := chatRequest(t, router, provider.ID, "", "你好"); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "AI_KEY_UNAVAILABLE") {
		t.Fatalf("chat without key = %d: %s", response.Code, response.Body.String())
	}
}

func TestAIChatRejectsSerializedPromptOverflowWithoutCreatingConversation(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("oversized prompt must not reach the model endpoint")
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-prompt-cap", upstream.URL+"/v1", "gpt-test")
	payload, err := json.Marshal(chatAIRequest{
		ProviderID: provider.ID,
		Message:    strings.Repeat("x", modelclient.MaxPromptBytes-100),
	})
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", payload, nil)
	assertAPIError(t, response, http.StatusUnprocessableEntity, "AI_PROMPT_TOO_LARGE")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
}

func TestAIChatHistoryUsesNewestCompleteWindowAndStripsControlBlocks(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []modelclient.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode rolling history: %v", err)
		}
		chatMessages := payload.Messages
		if len(chatMessages) > 0 && chatMessages[0].Role == "system" {
			chatMessages = chatMessages[1:]
		}
		encoded, _ := json.Marshal(chatMessages)
		history := string(encoded)
		if strings.Contains(history, "oldest-000") || strings.Contains(history, "oldest-001") {
			t.Errorf("oldest rows leaked into newest-200 window: %s", history)
		}
		if !strings.Contains(history, "latest-natural-answer") || !strings.Contains(history, "current-question") {
			t.Errorf("recent or current turn missing from rolling history: %s", history)
		}
		if strings.Contains(history, "opc:task") || strings.Contains(history, "hidden-task-title") {
			t.Errorf("task control block leaked into model history: %s", history)
		}
		streamMockAIDelta(w, "回答")
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-history-window", upstream.URL+"/v1", "gpt-test")

	createdSession := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{}`), nil)
	var sessionEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdSession.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	base := now.Add(-time.Hour)
	for index := 0; index < 202; index++ {
		role := "user"
		content := fmt.Sprintf("history-%03d", index)
		if index < 2 {
			content = fmt.Sprintf("oldest-%03d", index)
		}
		if index%2 == 1 {
			role = "assistant"
		}
		if index == 201 {
			content = `latest-natural-answer[opc:task]{"title":"hidden-task-title"}[opc:task]`
		}
		stamp := base.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)
		message := models.AIMessage{
			ID: fmt.Sprintf("018f0000-0000-7000-8000-%012d", index), SessionID: sessionEnvelope.Data.ID,
			Role: role, Status: "completed", Content: content, CreatedAt: stamp, UpdatedAt: stamp,
		}
		if err := store.DB.Create(&message).Error; err != nil {
			t.Fatalf("seed history message %d: %v", index, err)
		}
	}
	response := chatRequest(t, router, provider.ID, sessionEnvelope.Data.ID, "current-question")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat with rolling history = %d: %s", response.Code, response.Body.String())
	}
}

func TestAIChatUpstreamFailurePersistsFailedGeneration(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-failure", upstream.URL+"/v1", "gpt-test")
	response := chatRequest(t, router, provider.ID, "", "触发失败")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "AI_PROVIDER_ERROR") {
		t.Fatalf("failed chat = %d: %s", response.Code, response.Body.String())
	}
	var status, errorCode string
	if err := store.DB.Raw("SELECT status, COALESCE(error_code,'') FROM ai_generations LIMIT 1").Row().Scan(&status, &errorCode); err != nil {
		t.Fatalf("read generation: %v", err)
	}
	if status != "failed" || errorCode != "AI_PROVIDER_ERROR" {
		t.Fatalf("failed generation status=%q code=%q", status, errorCode)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'ai_generation' AND action = 'ai_generation_failed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE role = 'assistant'", 0)
}

func TestAISessionLifecycleMessagesPaginationAndDeletion(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)

	created := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create session = %d: %s", created.Code, created.Body.String())
	}
	var sessionEnvelope struct {
		Data struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sessionEnvelope.Data.Title != "新会话" || sessionEnvelope.Data.Version != 1 {
		t.Fatalf("created session = %#v", sessionEnvelope.Data)
	}
	sessionID := sessionEnvelope.Data.ID
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		message := models.AIMessage{
			ID: fmt.Sprintf("018f0000-0000-7000-8000-00000000500%d", i), SessionID: sessionID,
			Role: "user", Status: "completed", Content: fmt.Sprintf("消息 %d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano),
			UpdatedAt: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano),
		}
		if err := store.DB.Create(&message).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
	page1 := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+sessionID+"/messages?limit=2", nil, nil)
	if page1.Code != http.StatusOK {
		t.Fatalf("messages page1 = %d: %s", page1.Code, page1.Body.String())
	}
	var page1Envelope struct {
		Data []struct {
			Content string `json:"content"`
		} `json:"data"`
		Meta struct {
			HasMore         bool    `json:"has_more"`
			OldestCreatedAt *string `json:"oldest_created_at"`
			OldestID        *string `json:"oldest_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(page1.Body.Bytes(), &page1Envelope); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1Envelope.Data) != 2 || page1Envelope.Data[0].Content != "消息 3" || page1Envelope.Data[1].Content != "消息 4" {
		t.Fatalf("page1 = %#v", page1Envelope.Data)
	}
	if !page1Envelope.Meta.HasMore || page1Envelope.Meta.OldestID == nil {
		t.Fatalf("page1 meta = %#v", page1Envelope.Meta)
	}
	page2 := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+sessionID+"/messages?limit=10&before_created_at="+*page1Envelope.Meta.OldestCreatedAt+"&before_id="+*page1Envelope.Meta.OldestID, nil, nil)
	var page2Envelope struct {
		Data []struct {
			Content string `json:"content"`
		} `json:"data"`
		Meta struct {
			HasMore bool `json:"has_more"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(page2.Body.Bytes(), &page2Envelope); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2Envelope.Data) != 3 || page2Envelope.Data[0].Content != "消息 0" || page2Envelope.Meta.HasMore {
		t.Fatalf("page2 = %#v meta=%#v", page2Envelope.Data, page2Envelope.Meta)
	}

	updated := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+sessionID, nil, nil)
	var updatedEnvelope struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &updatedEnvelope); err != nil {
		t.Fatalf("decode session detail: %v", err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/sessions/"+sessionID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, updatedEnvelope.Data.Version)})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete session = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions WHERE id = ?", 0, sessionID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id = ?", 0, sessionID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE session_id = ?", 0, sessionID)
}

func TestAIChatRecoversStaleGenerationsOnStartup(t *testing.T) {
	storePath := t.TempDir()
	store, err := database.Open(storePath + "/recovery.db")
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	session := models.AISession{ID: "018f0000-0000-7000-8000-000000006001", Title: "恢复", Persist: true, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	provider := models.AIProvider{ID: "018f0000-0000-7000-8000-000000006002", Name: "stale", Protocol: "openai_chat", BaseURL: "https://api.example.com/v1", Model: "m", Status: "ready", HealthStatus: "healthy", HasKey: true, LastHealthAt: aiNullableString(now.Format(time.RFC3339Nano)), Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	for index, status := range []string{"queued", "streaming"} {
		generation := models.AIGeneration{
			ID: fmt.Sprintf("018f0000-0000-7000-8000-00000000610%d", index), SessionID: session.ID,
			ProviderID: provider.ID, Status: status,
			CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
		}
		if err := store.DB.Create(&generation).Error; err != nil {
			t.Fatalf("seed generation: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopened, err := database.Open(storePath + "/recovery.db")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	keyStore := keystore.NewMemoryStore()
	router, err := NewRouter(reopened.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: reopened.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		KeyStore: keyStore, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	assertDatabaseCount(t, reopened, "SELECT COUNT(*) FROM ai_generations WHERE status = 'cancelled' AND error_code = 'AI_GENERATION_INTERRUPTED'", 2)
}

func TestAIChatRenamesDefaultTitleFromFirstUserMessage(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": "好"}, "finish_reason": "stop"}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-title", upstream.URL+"/v1", "gpt-test")

	// explicit empty-title session (the 新会话 button path)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{}`), nil)
	var createdEnvelope struct {
		Data struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if createdEnvelope.Data.Title != defaultAISessionTitle {
		t.Fatalf("default title = %q", createdEnvelope.Data.Title)
	}
	if resp := chatRequest(t, router, provider.ID, createdEnvelope.Data.ID, "帮我梳理特斯拉落地页任务"); resp.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", resp.Code, resp.Body.String())
	}
	var renamed string
	if err := store.DB.Raw("SELECT title FROM ai_sessions WHERE id = ?", createdEnvelope.Data.ID).Scan(&renamed).Error; err != nil {
		t.Fatalf("read renamed session: %v", err)
	}
	if !strings.HasPrefix(renamed, "帮我梳理特斯拉落地页任务") {
		t.Fatalf("session title not renamed from first message: %q", renamed)
	}

	// a user-named session keeps its title
	named := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{"title":"我的计划"}`), nil)
	var namedEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(named.Body.Bytes(), &namedEnvelope); err != nil {
		t.Fatalf("decode named session: %v", err)
	}
	if resp := chatRequest(t, router, provider.ID, namedEnvelope.Data.ID, "新消息不应覆盖标题"); resp.Code != http.StatusOK {
		t.Fatalf("named chat = %d: %s", resp.Code, resp.Body.String())
	}
	var kept string
	if err := store.DB.Raw("SELECT title FROM ai_sessions WHERE id = ?", namedEnvelope.Data.ID).Scan(&kept).Error; err != nil {
		t.Fatalf("read named session: %v", err)
	}
	if kept != "我的计划" {
		t.Fatalf("user title overwritten: %q", kept)
	}
}

func TestAIMessageTaskReferenceLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	createdSession := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{}`), nil)
	var sessionEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdSession.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	message := models.AIMessage{
		ID: "018f0000-0000-7000-8000-000000006501", SessionID: sessionEnvelope.Data.ID,
		Role: "assistant", Status: "completed", Content: "建议创建任务",
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	createdTask := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"写周报"}`), map[string]string{"Idempotency-Key": "ai-task-ref-task"})
	if createdTask.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", createdTask.Code, createdTask.Body.String())
	}
	var taskEnvelope struct {
		Data struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdTask.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("decode task: %v", err)
	}

	missingMessage := performRequest(router, http.MethodPost, "/api/v1/ai/messages/018f0000-0000-7000-8000-000000006599/task", []byte(`{"task_id":"`+taskEnvelope.Data.ID+`"}`), nil)
	assertAPIError(t, missingMessage, http.StatusNotFound, "AI_MESSAGE_NOT_FOUND")
	missingTask := performRequest(router, http.MethodPost, "/api/v1/ai/messages/"+message.ID+"/task", []byte(`{"task_id":"018f0000-0000-5000-8000-000000006598"}`), nil)
	assertAPIError(t, missingTask, http.StatusNotFound, "AI_TASK_NOT_FOUND")

	attached := performRequest(router, http.MethodPost, "/api/v1/ai/messages/"+message.ID+"/task", []byte(`{"task_id":"`+taskEnvelope.Data.ID+`"}`), nil)
	if attached.Code != http.StatusOK {
		t.Fatalf("attach task = %d: %s", attached.Code, attached.Body.String())
	}
	var attachedEnvelope struct {
		Data struct {
			TaskID            string  `json:"task_id"`
			TaskTitleSnapshot *string `json:"task_title_snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(attached.Body.Bytes(), &attachedEnvelope); err != nil {
		t.Fatalf("decode attach: %v", err)
	}
	if attachedEnvelope.Data.TaskID != taskEnvelope.Data.ID || attachedEnvelope.Data.TaskTitleSnapshot == nil || *attachedEnvelope.Data.TaskTitleSnapshot != "写周报" {
		t.Fatalf("attached message = %#v", attachedEnvelope.Data)
	}
	replayed := performRequest(router, http.MethodPost, "/api/v1/ai/messages/"+message.ID+"/task", []byte(`{"task_id":"`+taskEnvelope.Data.ID+`"}`), nil)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("same task attach replay = %d replay=%q: %s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	relabeled := performRequest(router, http.MethodPost, "/api/v1/ai/messages/"+message.ID+"/task", []byte(`{"task_id":"018f0000-0000-5000-8000-000000006597"}`), nil)
	assertAPIError(t, relabeled, http.StatusConflict, "AI_MESSAGE_TASK_ALREADY_LINKED")
}
