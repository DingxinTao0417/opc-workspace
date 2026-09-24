package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiBudgetMockUpstream struct {
	server *httptest.Server
	calls  atomic.Int32
}

func newAIBudgetMockUpstream(t *testing.T, protocol string, chat func(int, map[string]any, []byte, http.ResponseWriter)) *aiBudgetMockUpstream {
	t.Helper()
	fixture := &aiBudgetMockUpstream{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			return
		}
		wantPath := "/v1/chat/completions"
		if protocol == "anthropic_messages" {
			wantPath = "/v1/messages"
		}
		if r.Method != http.MethodPost || r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read %s request: %v", protocol, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode %s request: %v", protocol, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		chat(int(fixture.calls.Add(1)), payload, raw, w)
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (u *aiBudgetMockUpstream) baseURL(protocol string) string {
	if protocol == "openai_chat" {
		return u.server.URL + "/v1"
	}
	return u.server.URL
}

func createAIBudgetProvider(t *testing.T, router http.Handler, store *database.Store, upstream *aiBudgetMockUpstream, protocol string) models.AIProvider {
	t.Helper()
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(fmt.Sprintf(
		`{"name":%q,"protocol":%q,"base_url":%q,"model":"budget-test"}`,
		"budget-"+protocol, protocol, upstream.baseURL(protocol),
	)), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create %s provider = %d: %s", protocol, created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s provider: %v", protocol, err)
	}
	key := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/key", []byte(`{"api_key":"sk-budget"}`), map[string]string{"If-Match": `"1"`})
	if key.Code != http.StatusOK {
		t.Fatalf("set %s key = %d: %s", protocol, key.Code, key.Body.String())
	}
	health := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
	if health.Code != http.StatusOK {
		t.Fatalf("check %s provider = %d: %s", protocol, health.Code, health.Body.String())
	}
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id = ?", envelope.Data.ID).Error; err != nil {
		t.Fatalf("reload %s provider: %v", protocol, err)
	}
	return provider
}

func aiBudgetToolNames(payload map[string]any) []string {
	rawTools, _ := payload["tools"].([]any)
	names := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, _ := rawTool.(map[string]any)
		name, _ := tool["name"].(string)
		if function, ok := tool["function"].(map[string]any); ok {
			name, _ = function["name"].(string)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func aiBudgetHasTool(payload map[string]any, wanted string) bool {
	for _, name := range aiBudgetToolNames(payload) {
		if name == wanted {
			return true
		}
	}
	return false
}

func aiBudgetSystemPrompt(payload map[string]any) string {
	if system, ok := payload["system"].(string); ok {
		return system
	}
	messages, _ := payload["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "system" {
			content, _ := message["content"].(string)
			return content
		}
	}
	return ""
}

func writeAIBudgetToolTurn(w http.ResponseWriter, protocol, id, name, arguments string) {
	w.Header().Set("Content-Type", "text/event-stream")
	if protocol == "openai_chat" {
		frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": name, "arguments": arguments},
			}}},
			"finish_reason": "tool_calls",
		}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
		return
	}
	var input any
	_ = json.Unmarshal([]byte(arguments), &input)
	start, _ := json.Marshal(map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": input},
	})
	_, _ = fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", start)
	_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
	_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

func writeAIBudgetTextTurn(w http.ResponseWriter, protocol, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	if protocol == "openai_chat" {
		frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"delta": map[string]any{"content": text}, "finish_reason": "stop",
		}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
		return
	}
	frame, _ := json.Marshal(map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", frame)
	_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
	_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

func aiBudgetSSEData(t *testing.T, body, wanted string) []byte {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 4096), modelclient.MaxResponseBytes+64*1024)
	event := ""
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if event == wanted && strings.HasPrefix(line, "data: ") {
			return []byte(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s event: %v", wanted, err)
	}
	t.Fatalf("missing %s event in %s", wanted, body)
	return nil
}

type aiBudgetStreamMetaForTest struct {
	GenerationID string `json:"generation_id"`
	SessionID    string `json:"session_id"`
}

type aiBudgetReplaceForTest struct {
	GenerationID string `json:"generation_id"`
	Text         string `json:"text"`
	Reasoning    string `json:"reasoning"`
}

type aiBudgetGenerationForTest struct {
	Status    string  `json:"status"`
	Content   string  `json:"content"`
	ErrorCode *string `json:"error_code"`
	Persist   bool    `json:"persist"`
}

func decodeAIBudgetMeta(t *testing.T, body string) aiBudgetStreamMetaForTest {
	t.Helper()
	var value aiBudgetStreamMetaForTest
	if err := json.Unmarshal(aiBudgetSSEData(t, body, "meta"), &value); err != nil {
		t.Fatalf("decode budget meta: %v", err)
	}
	return value
}

func decodeAIBudgetReplace(t *testing.T, body string) aiBudgetReplaceForTest {
	t.Helper()
	var value aiBudgetReplaceForTest
	if err := json.Unmarshal(aiBudgetSSEData(t, body, "replace"), &value); err != nil {
		t.Fatalf("decode budget replace: %v", err)
	}
	return value
}

func TestAIBudgetHandoffAcrossProviderProtocolsKeepsApprovalManual(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			summary := "已完成查询并登记一条待确认建议；任务尚未创建。"
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				switch {
				case call == 1:
					if !aiBudgetHasTool(payload, "workspace_guide") || aiBudgetHasTool(payload, "workspace_tasks") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("initial discovery tools = %v", aiBudgetToolNames(payload))
					}
					writeAIBudgetToolTurn(w, protocol, "guide-1", "workspace_guide", `{"topic":"tasks_projects"}`)
				case call <= 6:
					if !aiBudgetHasTool(payload, "workspace_tasks") || !aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("round %d missing granted tools: %v", call, aiBudgetToolNames(payload))
					}
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("query-%d", call), "workspace_tasks", `{"limit":1}`)
				case call == 7:
					if !aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("proposal round tools = %v", aiBudgetToolNames(payload))
					}
					writeAIBudgetToolTurn(w, protocol, "proposal-7", "workspace_propose", `{"action":"task.create","changes":{"title":"Budget handoff task"}}`)
				case call == 8:
					if names := aiBudgetToolNames(payload); len(names) != 0 {
						t.Errorf("handoff tools = %v, want none", names)
					}
					if !strings.Contains(aiBudgetSystemPrompt(payload), harness.BudgetHandoffPrompt) {
						t.Error("handoff instruction missing from final system prompt")
					}
					if !strings.Contains(string(raw), "proposal_id") || !strings.Contains(string(raw), "workspace_propose") {
						t.Error("final handoff request lost the real proposal result")
					}
					writeAIBudgetTextTurn(w, protocol, summary+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					if names := aiBudgetToolNames(payload); len(names) != 4 || !aiBudgetHasTool(payload, "workspace_request_access") || aiBudgetHasTool(payload, "workspace_tasks") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("ungranted follow-up inherited tools: %v", names)
					}
					if strings.Contains(aiBudgetSystemPrompt(payload), harness.BudgetHandoffPrompt) {
						t.Error("ordinary follow-up inherited the budget handoff instruction")
					}
					writeAIBudgetTextTurn(w, protocol, `后续消息未获得工作区授权。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			requestBody, _ := json.Marshal(map[string]any{
				"provider_id": provider.ID,
				"message":     "查询后提出一个任务，但不要替我确认",
				"workspace": aiWorkspaceGrant{
					ProviderVersion: provider.Version,
					Scopes:          []string{"work", "actions"},
				},
			})
			requestKey := "budget-handoff-" + protocol
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", requestBody, map[string]string{"Idempotency-Key": requestKey})
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") {
				t.Fatalf("handoff chat = %d: %s", response.Code, response.Body.String())
			}
			meta := decodeAIBudgetMeta(t, response.Body.String())
			replacement := decodeAIBudgetReplace(t, response.Body.String())
			wantText := aiBudgetHandoffNotice + "\n\n" + summary
			if replacement.GenerationID != meta.GenerationID || replacement.Text != wantText || replacement.Reasoning != "" {
				t.Fatalf("replacement = %#v, want text %q", replacement, wantText)
			}
			if !strings.HasPrefix(replacement.Text, aiBudgetHandoffNotice) {
				t.Fatal("fixed handoff notice missing")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE generation_id=? AND status='pending'", 1, meta.GenerationID)

			generationRead := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+meta.GenerationID, nil, nil)
			var generationEnvelope struct {
				Data aiBudgetGenerationForTest `json:"data"`
			}
			if generationRead.Code != http.StatusOK || json.Unmarshal(generationRead.Body.Bytes(), &generationEnvelope) != nil {
				t.Fatalf("generation read = %d: %s", generationRead.Code, generationRead.Body.String())
			}
			if generationEnvelope.Data.Status != "completed" || generationEnvelope.Data.Content != replacement.Text {
				t.Fatalf("generation = %#v", generationEnvelope.Data)
			}
			messagesRead := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+meta.SessionID+"/messages", nil, nil)
			var messagesEnvelope struct {
				Data []aiMessageResponse `json:"data"`
			}
			if messagesRead.Code != http.StatusOK || json.Unmarshal(messagesRead.Body.Bytes(), &messagesEnvelope) != nil {
				t.Fatalf("messages read = %d: %s", messagesRead.Code, messagesRead.Body.String())
			}
			foundAssistant := false
			for _, message := range messagesEnvelope.Data {
				if message.GenerationID != nil && *message.GenerationID == meta.GenerationID && message.Role == "assistant" {
					foundAssistant = message.Status == "completed" && message.Content == replacement.Text
				}
			}
			if !foundAssistant {
				t.Fatalf("assistant message did not match replacement: %#v", messagesEnvelope.Data)
			}

			replay := performRequest(router, http.MethodPost, "/api/v1/ai/chat", requestBody, map[string]string{"Idempotency-Key": requestKey})
			if replay.Code != http.StatusConflict || responseErrorCode(t, replay.Body.Bytes()) != "AI_CHAT_ALREADY_ACCEPTED" || upstream.calls.Load() != 8 {
				t.Fatalf("idempotent replay = %d calls=%d: %s", replay.Code, upstream.calls.Load(), replay.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 1)

			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal, "generation_id = ?", meta.GenerationID).Error; err != nil {
				t.Fatal(err)
			}
			decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(
				`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint,
			)), nil)
			if decision.Code != http.StatusOK {
				t.Fatalf("confirm pending proposal = %d: %s", decision.Code, decision.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title='Budget handoff task'", 1)

			followUp := chatRequest(t, router, provider.ID, meta.SessionID, "继续，但这条消息没有工作区授权")
			if followUp.Code != http.StatusOK || !strings.Contains(followUp.Body.String(), "event: done") || upstream.calls.Load() != 9 {
				t.Fatalf("ungranted follow-up = %d calls=%d: %s", followUp.Code, upstream.calls.Load(), followUp.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_access_granted'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
		})
	}
}

func TestAIBudgetHandoffNonPersistentMemorySearchDoesNotRetainBodiesOrWriteWorkspace(t *testing.T) {
	now := time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	marker := "PRIVATE-BUDGET-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	summary := "临时会话交接摘要-" + marker
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, payload map[string]any, _ []byte, w http.ResponseWriter) {
		if call <= 7 {
			if names := aiBudgetToolNames(payload); len(names) != 4 || !aiBudgetHasTool(payload, "memory_search") || !aiBudgetHasTool(payload, "workspace_request_access") || aiBudgetHasTool(payload, "workspace_tasks") {
				t.Errorf("temporary round %d tools = %v", call, names)
			}
			writeAIBudgetToolTurn(w, "openai_chat", fmt.Sprintf("memory-%d", call), "memory_search", `{"query":"nothing"}`)
			return
		}
		if call != 8 || len(aiBudgetToolNames(payload)) != 0 || !strings.Contains(aiBudgetSystemPrompt(payload), harness.BudgetHandoffPrompt) {
			t.Errorf("temporary handoff request call=%d tools=%v", call, aiBudgetToolNames(payload))
		}
		writeAIBudgetTextTurn(w, "openai_chat", summary+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
	created := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{"title":"temporary budget","persist":false}`), nil)
	var sessionEnvelope struct {
		Data aiSessionResponse `json:"data"`
	}
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &sessionEnvelope) != nil {
		t.Fatalf("create temporary session = %d: %s", created.Code, created.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": sessionEnvelope.Data.ID, "message": marker})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || upstream.calls.Load() != 8 {
		t.Fatalf("temporary handoff = %d calls=%d: %s", response.Code, upstream.calls.Load(), response.Body.String())
	}
	meta := decodeAIBudgetMeta(t, response.Body.String())
	replacement := decodeAIBudgetReplace(t, response.Body.String())
	if replacement.Text != aiBudgetHandoffNotice+"\n\n"+summary {
		t.Fatalf("temporary replacement = %q", replacement.Text)
	}
	var generation models.AIGeneration
	if err := store.DB.First(&generation, "id = ?", meta.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Status != "completed" || generation.Content != nil {
		t.Fatalf("temporary generation = %#v", generation)
	}
	read := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+meta.GenerationID, nil, nil)
	var readEnvelope struct {
		Data aiBudgetGenerationForTest `json:"data"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &readEnvelope) != nil || readEnvelope.Data.Content != "" {
		t.Fatalf("temporary generation read = %d: %s", read.Code, read.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id=?", 0, meta.SessionID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE session_id=?", 0, meta.SessionID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_access_granted'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE id=? AND content IS NULL", 1, meta.GenerationID)
}

func TestAIBudgetHandoffFinalTurnFailuresDoNotExecuteBusinessTools(t *testing.T) {
	for _, terminal := range []string{"illegal_tool", "provider_failure"} {
		t.Run(terminal, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, payload map[string]any, _ []byte, w http.ResponseWriter) {
				if call <= 6 {
					writeAIBudgetToolTurn(w, "openai_chat", fmt.Sprintf("query-%d", call), "workspace_tasks", `{"limit":1}`)
					return
				}
				if call == 7 {
					writeAIBudgetToolTurn(w, "openai_chat", "proposal-before-failure", "workspace_propose", `{"action":"task.create","changes":{"title":"Before failure"}}`)
					return
				}
				if call != 8 || len(aiBudgetToolNames(payload)) != 0 || !strings.Contains(aiBudgetSystemPrompt(payload), harness.BudgetHandoffPrompt) {
					t.Errorf("terminal request call=%d tools=%v", call, aiBudgetToolNames(payload))
				}
				if terminal == "illegal_tool" {
					writeAIBudgetToolTurn(w, "openai_chat", "must-not-run", "workspace_propose", `{"action":"task.create","changes":{"title":"Must not exist"}}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"hard failure"}}`)
			})
			provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
			body, _ := json.Marshal(map[string]any{
				"provider_id": provider.ID, "message": "query repeatedly",
				"workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}},
			})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: error") || strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: replace") {
				t.Fatalf("%s response = %d: %s", terminal, response.Code, response.Body.String())
			}
			meta := decodeAIBudgetMeta(t, response.Body.String())
			var generation models.AIGeneration
			if err := store.DB.First(&generation, "id = ?", meta.GenerationID).Error; err != nil {
				t.Fatal(err)
			}
			wantError := "AI_PROVIDER_ERROR"
			if terminal == "illegal_tool" {
				wantError = "AI_TURN_BUDGET_EXHAUSTED"
			}
			if generation.Status != "failed" || generation.ErrorCode == nil || *generation.ErrorCode != wantError || upstream.calls.Load() != 8 {
				t.Fatalf("%s generation=%#v calls=%d", terminal, generation, upstream.calls.Load())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE generation_id=? AND status='pending'", 1, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE generation_id=? AND kind='model_turn'", 8, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE generation_id=? AND kind='tool_call' AND status='succeeded'", 7, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE generation_id=? AND tool_name='workspace_propose' AND status='succeeded'", 1, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_access_granted'", 1)

			actions := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+meta.GenerationID+"/actions", nil, nil)
			var actionsEnvelope struct {
				Data []aiActionResponse `json:"data"`
			}
			if actions.Code != http.StatusOK || json.Unmarshal(actions.Body.Bytes(), &actionsEnvelope) != nil || len(actionsEnvelope.Data) != 1 {
				t.Fatalf("failed generation actions = %d: %s", actions.Code, actions.Body.String())
			}
			proposal := actionsEnvelope.Data[0]
			if proposal.Status != "unavailable" || proposal.CanConfirm || proposal.Action.Action != "task.create" {
				t.Fatalf("failed generation proposal = %#v", proposal)
			}
			decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(
				`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint,
			)), nil)
			if decision.Code != http.StatusConflict || responseErrorCode(t, decision.Body.Bytes()) != "AI_ACTION_NOT_CONFIRMABLE" {
				t.Fatalf("failed generation proposal confirmed = %d: %s", decision.Code, decision.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
		})
	}
}

type aiBudgetLimitClient struct {
	overflow bool
	calls    atomic.Int32
	limit    atomic.Int64
}

func (c *aiBudgetLimitClient) Stream(_ context.Context, request harness.Request, _ func(string), _ func(string)) (harness.Turn, error) {
	call := c.calls.Add(1)
	if call <= 7 {
		return harness.Turn{ToolCalls: []harness.ToolCall{{ID: "x", Name: "memory_search", Arguments: json.RawMessage(`{"query":"x"}`)}}}, nil
	}
	c.limit.Store(int64(request.ResponseByteLimit))
	size := request.ResponseByteLimit
	if c.overflow {
		size++
	}
	tail := "BUDGET-TAIL"
	if size < len(tail) {
		return harness.Turn{Text: strings.Repeat("x", size)}, nil
	}
	return harness.Turn{Text: strings.Repeat("x", size-len(tail)) + tail}, nil
}

func TestAIBudgetHandoffNearResponseLimitIsWholeAndHarnessOverflowFailsClosed(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("overflow_%v", overflow), func(t *testing.T) {
			router, api := newAIRuntimeLockTest(t)
			client := &aiBudgetLimitClient{overflow: overflow}
			api.harnessClient = client
			stamp := time.Now().UTC().Format(time.RFC3339Nano)
			provider := models.AIProvider{
				ID: uuid.NewString(), Name: "budget-limit", Kind: "local", Protocol: "openai_chat",
				BaseURL: "http://127.0.0.1:1", Model: "test", Status: "ready", HealthStatus: "healthy",
				LastHealthAt: &stamp, Version: 1, ConfigVersion: 1, CreatedAt: stamp, UpdatedAt: stamp,
			}
			session := models.AISession{ID: uuid.NewString(), Title: "temporary", Persist: false, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
			if err := api.db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			if err := api.db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "bounded handoff"})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || client.calls.Load() != 8 || client.limit.Load() <= 0 {
				t.Fatalf("bounded handoff = %d calls=%d limit=%d", response.Code, client.calls.Load(), client.limit.Load())
			}
			meta := decodeAIBudgetMeta(t, response.Body.String())
			var generation models.AIGeneration
			if err := api.db.First(&generation, "id = ?", meta.GenerationID).Error; err != nil {
				t.Fatal(err)
			}
			if overflow {
				if generation.Status != "failed" || generation.ErrorCode == nil || *generation.ErrorCode != "AI_RESPONSE_BUDGET_EXHAUSTED" || !strings.Contains(response.Body.String(), "event: error") || strings.Contains(response.Body.String(), "event: replace") {
					bodyText := response.Body.String()
					start := len(bodyText) - 500
					if start < 0 {
						start = 0
					}
					t.Fatalf("overflow generation=%#v body suffix=%q", generation, bodyText[start:])
				}
				return
			}
			replacement := decodeAIBudgetReplace(t, response.Body.String())
			if generation.Status != "completed" || !strings.HasPrefix(replacement.Text, aiBudgetHandoffNotice+"\n\n") || !strings.HasSuffix(replacement.Text, "BUDGET-TAIL") {
				t.Fatalf("near-limit generation=%#v replacement length=%d", generation, len(replacement.Text))
			}
			if len(replacement.Text) > modelclient.MaxResponseBytes || modelclient.MaxResponseBytes-len(replacement.Text) > 16 {
				t.Fatalf("near-limit response length=%d max=%d", len(replacement.Text), modelclient.MaxResponseBytes)
			}
			assertDatabaseCount(t, &database.Store{DB: api.db}, "SELECT COUNT(*) FROM ai_messages WHERE session_id=?", 0, session.ID)
		})
	}
}
