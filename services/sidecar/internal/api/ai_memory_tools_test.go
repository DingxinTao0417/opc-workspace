package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIMemoryToolsWriteSearchAndProposeWithConfirmation(t *testing.T) {
	now := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	const sessionID = "018f0000-0000-7000-8000-000000005751"
	if err := store.DB.Create(&models.AISession{
		ID: sessionID, Title: "memory tools", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiMemoryToolRegistry(sessionID)
	if err != nil {
		t.Fatalf("aiMemoryToolRegistry: %v", err)
	}
	if got := registry.Names(); len(got) != 3 || strings.Join(got, ",") != "memory_search,memory_write,memory_propose" {
		t.Fatalf("memory tool allowlist = %v", got)
	}
	for _, definition := range registry.Definitions() {
		if !json.Valid(definition.Parameters) || !strings.Contains(string(definition.Parameters), `"additionalProperties":false`) {
			t.Fatalf("tool definition = %#v", definition)
		}
	}

	writeTool, _ := registry.Get("memory_write")
	written, err := writeTool.Execute(context.Background(), json.RawMessage(`{"content":"Project Alpha deadline is Friday","tags":["project","deadline"]}`))
	if err != nil || !strings.Contains(written, `"kind":"session_fact"`) {
		t.Fatalf("memory_write result=%s err=%v", written, err)
	}
	duplicate, err := writeTool.Execute(context.Background(), json.RawMessage(`{"content":"Project Alpha deadline is Friday","tags":["deadline","project"]}`))
	if err != nil || duplicate != written {
		t.Fatalf("memory_write duplicate=%s first=%s err=%v", duplicate, written, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE kind = 'session_fact' AND status = 'active'", 1)

	messageID := uuid.NewString()
	if err := store.DB.Create(&models.AIMessage{
		ID: messageID, SessionID: sessionID, Role: "assistant", Status: "completed",
		Content:   `Client requested a concise report [opc:task]{"title":"hidden"}[/opc:task]`,
		CreatedAt: now.Add(time.Second).Format(time.RFC3339Nano), UpdatedAt: now.Add(time.Second).Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create searchable message: %v", err)
	}
	searchTool, _ := registry.Get("memory_search")
	searchResult, err := searchTool.Execute(context.Background(), json.RawMessage(`{"query":"project","tags":["deadline"],"limit":3}`))
	if err != nil || !strings.Contains(searchResult, "Project Alpha") {
		t.Fatalf("memory_search fact=%s err=%v", searchResult, err)
	}
	messageResult, err := searchTool.Execute(context.Background(), json.RawMessage(`{"query":"Client","limit":3}`))
	if err != nil || !strings.Contains(messageResult, "concise report") || strings.Contains(messageResult, "opc:task") {
		t.Fatalf("memory_search message=%s err=%v", messageResult, err)
	}

	proposeTool, _ := registry.Get("memory_propose")
	proposalResult, err := proposeTool.Execute(context.Background(), json.RawMessage(`{"content":"回答保持简洁","tags":["preference"]}`))
	if err != nil {
		t.Fatalf("memory_propose: %v", err)
	}
	var proposal struct {
		ProposalID           string `json:"proposal_id"`
		Content              string `json:"content"`
		ConfirmationRequired bool   `json:"confirmation_required"`
	}
	if err := json.Unmarshal([]byte(proposalResult), &proposal); err != nil || proposal.ProposalID == "" || !proposal.ConfirmationRequired {
		t.Fatalf("proposal result=%s err=%v", proposalResult, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memories", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE kind = 'memory_proposal' AND status = 'active'", 1)

	assistantMessageID := uuid.NewString()
	if err := store.DB.Create(&models.AIMessage{
		ID: assistantMessageID, SessionID: sessionID, Role: "assistant", Status: "completed",
		Content: "请确认记忆", CreatedAt: now.Add(2 * time.Second).Format(time.RFC3339Nano), UpdatedAt: now.Add(2 * time.Second).Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create proposal message: %v", err)
	}
	body := []byte(`{"content":"回答保持简洁","source_message_id":"` + assistantMessageID + `","proposal_id":"` + proposal.ProposalID + `"}`)
	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, map[string]string{"Idempotency-Key": "confirm-memory-proposal"})
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirm proposal = %d: %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memories WHERE content = ?", 1, proposal.Content)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE id = ? AND status = 'superseded'", 1, proposal.ProposalID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'ai_memory_proposal_confirmed' AND current_json NOT LIKE ?", 1, "%回答保持简洁%")

	replayed := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, map[string]string{"Idempotency-Key": "confirm-memory-proposal"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("confirm replay = %d: %s", replayed.Code, replayed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memories WHERE content = ?", 1, proposal.Content)
}

func TestAIMemoryToolsRejectInvalidArgumentsAndKeepProposalsPending(t *testing.T) {
	now := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	const sessionID = "018f0000-0000-7000-8000-000000005761"
	if err := store.DB.Create(&models.AISession{
		ID: sessionID, Title: "invalid tools", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	registry, _ := service.aiMemoryToolRegistry(sessionID)
	writeTool, _ := registry.Get("memory_write")
	for _, arguments := range []string{
		`{"content":"","unknown":true}`,
		`{"content":"x","tags":["a\nb"]}`,
		`{"content":"x"} trailing`,
	} {
		if _, err := writeTool.Execute(context.Background(), json.RawMessage(arguments)); err == nil {
			t.Fatalf("invalid memory_write accepted: %s", arguments)
		}
	}
	proposeTool, _ := registry.Get("memory_propose")
	proposalJSON, err := proposeTool.Execute(context.Background(), json.RawMessage(`{"content":"pending preference"}`))
	if err != nil {
		t.Fatalf("memory_propose: %v", err)
	}
	var proposal struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(proposalJSON), &proposal); err != nil || proposal.ProposalID == "" {
		t.Fatalf("decode proposal=%s err=%v", proposalJSON, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memories", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE kind = 'memory_proposal' AND status = 'active'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE current_json LIKE ?", 0, "%pending preference%")
	listed := performRequest(router, http.MethodGet, "/api/v1/ai/memory-proposals", nil, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), proposal.ProposalID) || !strings.Contains(listed.Body.String(), "invalid tools") {
		t.Fatalf("list proposals = %d: %s", listed.Code, listed.Body.String())
	}
	rejected := performRequest(router, http.MethodDelete, "/api/v1/ai/memory-proposals/"+proposal.ProposalID, nil, nil)
	if rejected.Code != http.StatusOK {
		t.Fatalf("reject proposal = %d: %s", rejected.Code, rejected.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE id = ? AND status = 'superseded'", 1, proposal.ProposalID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'ai_memory_proposal_rejected' AND current_json NOT LIKE ?", 1, "%pending preference%")

	settingsProposalJSON, err := proposeTool.Execute(context.Background(), json.RawMessage(`{"content":"confirm from settings"}`))
	if err != nil {
		t.Fatalf("second memory_propose: %v", err)
	}
	if err := json.Unmarshal([]byte(settingsProposalJSON), &proposal); err != nil {
		t.Fatalf("decode second proposal: %v", err)
	}
	confirmed := performRequest(
		router, http.MethodPost, "/api/v1/ai/memories",
		[]byte(`{"content":"confirm from settings","proposal_id":"`+proposal.ProposalID+`"}`),
		map[string]string{"Idempotency-Key": "settings-proposal-confirm"},
	)
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("settings confirmation = %d: %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memories WHERE content = 'confirm from settings'", 1)
}

func TestAIChatExecutesAllowlistedMemoryToolAndFeedsResultBack(t *testing.T) {
	now := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode chat request: %v", err)
		}
		if len(payload.Tools) != 3 {
			t.Errorf("production memory tools=%d, want 3", len(payload.Tools))
		}
		if call == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{
					"delta": map[string]any{"tool_calls": []map[string]any{{
						"index": 0, "id": "call-memory", "type": "function",
						"function": map[string]any{
							"name": "memory_write", "arguments": `{"content":"Milestone agreed","tags":["progress"]}`,
						},
					}}},
					"finish_reason": "tool_calls",
				}},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		foundResult := false
		for _, message := range payload.Messages {
			if message["role"] == "tool" && message["tool_call_id"] == "call-memory" && strings.Contains(fmt.Sprint(message["content"]), "session_fact") {
				foundResult = true
			}
		}
		if !foundResult {
			t.Errorf("tool result missing from second request: %#v", payload.Messages)
		}
		streamMockAIDelta(w, `已记录当前进度[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "memory-tool-chat", upstream.URL+"/v1", "gpt-test")

	response := chatRequest(t, router, provider.ID, "", "记住当前进度")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls=%d, want 2", calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE kind = 'session_fact' AND content = ?", 1, "Milestone agreed")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'ai_session_memory_written' AND current_json NOT LIKE ?", 1, "%Milestone agreed%")
}
