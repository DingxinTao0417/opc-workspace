package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Reflection is the agent's own behavior (ADR-006): the model appends its
// [opc:selfcheck] verdict to the draft and the harness autonomously runs one
// revision turn on an insufficiency verdict. No user-facing switch exists.
func TestAIChatSelfCheckDrivesAutonomousRevision(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			streamMockAIDelta(w, "占位")
			return
		}
		last := payload.Messages[len(payload.Messages)-1]
		if last.Role == "user" && strings.Contains(last.Content, "修订后回答全文") {
			// Autonomous revision turn: it must carry the stripped draft and
			// the model's own insufficiency note.
			if payload.Messages[len(payload.Messages)-2].Role != "assistant" ||
				payload.Messages[len(payload.Messages)-2].Content != "初稿回答" {
				t.Errorf("draft not in revision history: %+v", payload.Messages)
			}
			if !strings.Contains(last.Content, "缺了步骤二") {
				t.Errorf("self-check note not fed back: %q", last.Content)
			}
			streamMockAIDelta(w, `修订后的回答[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			return
		}
		streamMockAIDelta(w, `初稿回答[opc:selfcheck]{"sufficient":false,"note":"缺了步骤二"}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-selfcheck", upstream.URL+"/v1", "gpt-test")

	response := chatRequest(t, router, provider.ID, "", "帮我写个方案")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("insufficient self-check must trigger exactly one revision: calls=%d", calls.Load())
	}
	if !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat incomplete: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event: replace") ||
		!strings.Contains(response.Body.String(), "修订后的回答") {
		t.Fatalf("revised answer was not sent as a replacement event: %s", response.Body.String())
	}
	// The persisted assistant answer is the revised text and never contains
	// the internal self-check block.
	var assistantContent string
	if err := store.DB.Table("ai_messages").
		Where("role = 'assistant'").Order("created_at DESC").Limit(1).
		Pluck("content", &assistantContent).Error; err != nil || assistantContent == "" {
		t.Fatalf("assistant message missing: %v", err)
	}
	if assistantContent != "修订后的回答" {
		t.Fatalf("persisted answer = %q, want revised text without the block", assistantContent)
	}
}

// An affirmative self-check verdict emits the stripped draft as-is with a
// single upstream call.
func TestAIChatSelfCheckAffirmativeStaysSingleCall(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		streamMockAIDelta(w, `完整回答[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "chat-selfcheck-ok", upstream.URL+"/v1", "gpt-test")

	response := chatRequest(t, router, provider.ID, "", "你好")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("affirmative verdict must not revise: calls=%d", calls.Load())
	}
	var assistantContent string
	if err := store.DB.Table("ai_messages").
		Where("role = 'assistant'").Order("created_at DESC").Limit(1).
		Pluck("content", &assistantContent).Error; err != nil || assistantContent == "" {
		t.Fatalf("assistant message missing: %v", err)
	}
	if assistantContent != "完整回答" {
		t.Fatalf("persisted answer = %q, want the stripped draft", assistantContent)
	}
	if strings.Contains(assistantContent, "opc:selfcheck") {
		t.Fatalf("self-check block leaked into persisted content: %q", assistantContent)
	}
}

// streamMockAIDelta writes one streamed OpenAI-style content frame plus DONE.
func streamMockAIDelta(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher := w.(http.Flusher)
	frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": text}}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}
