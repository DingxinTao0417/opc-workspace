package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Size the fixture using the actual provider encoder and the same allowlisted
// definitions as chatAI. ASCII padding changes the encoded payload byte-for-byte.
func fillAIContextWindow(t *testing.T, store *database.Store, provider models.AIProvider, sessionID string, persist bool, grant *aiWorkspaceGrant, history []modelclient.ChatMessage, index, spare int) []modelclient.ChatMessage {
	t.Helper()
	api := &API{db: store.DB, options: Options{Now: time.Now}}
	registry, err := api.aiChatToolRegistry(sessionID, persist, &provider, grant, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	prompt := modelclient.PromptContext{SystemPrompt: workspaceSystemPrompt(grant), Tools: registry.ModelDefinitions()}
	size, err := modelclient.PromptSize(modelclient.Protocol(provider.Protocol), provider.Model, history, prompt)
	if err != nil {
		t.Fatal(err)
	}
	padding := modelclient.MaxPromptBytes - spare - size
	if padding < 0 {
		t.Fatalf("context fixture already exceeds its target: size=%d spare=%d", size, spare)
	}
	history[index].Content += strings.Repeat("x", padding)
	return history
}

func createAIContextWindowSession(t *testing.T, store *database.Store, persist bool, now time.Time) models.AISession {
	t.Helper()
	stamp := now.Add(-time.Hour).Format(time.RFC3339Nano)
	session := models.AISession{ID: uuid.NewString(), Title: "context window", Persist: persist, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	return session
}

func seedAIContextWindowHistory(t *testing.T, store *database.Store, sessionID string, history []modelclient.ChatMessage, now time.Time) []models.AIMessage {
	t.Helper()
	rows := make([]models.AIMessage, 0, len(history))
	for index, message := range history {
		stamp := now.Add(-time.Minute).Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)
		rows = append(rows, models.AIMessage{ID: uuid.NewString(), SessionID: sessionID, Role: message.Role, Status: "completed", Content: message.Content, CreatedAt: stamp, UpdatedAt: stamp})
	}
	if len(rows) > 0 {
		if err := store.DB.Create(&rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	return rows
}

func assertAIContextWindowToolPair(t *testing.T, payload map[string]any, protocol, id, name, evidence string) {
	t.Helper()
	callIndex, resultIndex, callCount, resultCount := -1, -1, 0, 0
	messages, _ := payload["messages"].([]any)
	for index, raw := range messages {
		message, _ := raw.(map[string]any)
		if protocol == "openai_chat" {
			calls, _ := message["tool_calls"].([]any)
			for _, rawCall := range calls {
				call, _ := rawCall.(map[string]any)
				function, _ := call["function"].(map[string]any)
				if call["id"] == id && function["name"] == name && message["role"] == "assistant" {
					callIndex, callCount = index, callCount+1
				}
			}
			if message["role"] == "tool" && message["tool_call_id"] == id {
				content, _ := message["content"].(string)
				if !strings.Contains(content, evidence) {
					t.Errorf("tool result %s lost complete evidence", id)
				}
				resultIndex, resultCount = index, resultCount+1
			}
			continue
		}
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "tool_use" && block["id"] == id && block["name"] == name && message["role"] == "assistant" {
				callIndex, callCount = index, callCount+1
			}
			if block["type"] == "tool_result" && block["tool_use_id"] == id && message["role"] == "user" {
				content, _ := block["content"].(string)
				if !strings.Contains(content, evidence) {
					t.Errorf("tool result %s lost complete evidence", id)
				}
				resultIndex, resultCount = index, resultCount+1
			}
		}
	}
	if callCount != 1 || resultCount != 1 || resultIndex != callIndex+1 {
		t.Errorf("%s tool pair %s has calls=%d results=%d indexes=%d,%d", protocol, id, callCount, resultCount, callIndex, resultIndex)
	}
}

func TestAIContextWindowAcrossProviderProtocolsPreservesEvidenceAndManualApproval(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 19, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			const currentIntent = "CURRENT-USER-INTENT: read the task and propose its title, do not confirm"
			const oldUser = "OLD-USER-PAIR:"
			const oldAssistant = "OLD-ASSISTANT-PAIR"
			const recentUser = "RECENT-USER-PAIR"
			const recentAssistant = "RECENT-ASSISTANT-PAIR"
			const finalTitle = "Window reviewed title"
			const summary = "已核验任务并提出改名建议，尚未执行，需人工确认。"
			evidence := "EVIDENCE-BEGIN:" + strings.Repeat("e", 1400) + ":EVIDENCE-END"
			task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Window original title","description":%q}`, evidence))
			session := createAIContextWindowSession(t, store, true, now)
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if len(raw) > modelclient.MaxPromptBytes {
					t.Errorf("round %d sent %d bytes, maximum %d", call, len(raw), modelclient.MaxPromptBytes)
				}
				text := aiActionRecheckPayloadText(payload)
				for _, required := range []string{currentIntent, recentUser, recentAssistant} {
					if !strings.Contains(text, required) {
						t.Errorf("round %d lost retained user turn %q", call, required)
					}
				}
				// This context-window fixture calls the authorized proposal tool by
				// name without first asking for a guide. The catalog is presentation,
				// not execution authority; provider discovery is tested separately.
				if !aiBudgetHasTool(payload, "workspace_get") || !aiBudgetHasTool(payload, "workspace_guide") || aiBudgetHasTool(payload, "workspace_propose") {
					t.Errorf("round %d changed the initial model-visible catalog", call)
				}
				switch call {
				case 1:
					if len(raw) != modelclient.MaxPromptBytes-256 || !strings.Contains(text, oldUser) || !strings.Contains(text, oldAssistant) {
						t.Errorf("initial request was not the full near-limit history: bytes=%d", len(raw))
					}
					if strings.Contains(aiBudgetSystemPrompt(payload), harness.ContextWindowNotice) {
						t.Error("uncropped initial request claimed history was removed")
					}
					writeAIBudgetToolTurn(w, protocol, "window-read", "workspace_get", fmt.Sprintf(`{"type":"task","id":%q}`, task.ID))
				case 2, 3:
					if strings.Count(aiBudgetSystemPrompt(payload), harness.ContextWindowNotice) != 1 {
						t.Errorf("round %d did not preserve exactly one code-owned context window notice", call)
					}
					assertAIContextWindowToolPair(t, payload, protocol, "window-read", "workspace_get", evidence)
					if strings.Contains(text, oldUser) || strings.Contains(text, oldAssistant) {
						t.Errorf("round %d retained part of the oldest complete turn", call)
					}
					for _, required := range []string{evidence, "window-read", "workspace_get", task.ID, `"version":1`} {
						if !strings.Contains(text, required) {
							t.Errorf("round %d lost original tool evidence %q", call, required)
						}
					}
					if call == 2 {
						writeAIBudgetToolTurn(w, protocol, "window-propose", "workspace_propose", fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"title":%q}}`, task.ID, finalTitle))
					} else {
						assertAIContextWindowToolPair(t, payload, protocol, "window-propose", "workspace_propose", "NOT executed")
						if !strings.Contains(text, "window-propose") || !strings.Contains(text, "NOT executed") || !strings.Contains(text, "proposal_id") {
							t.Error("finishing request lost the pending proposal result")
						}
						writeAIBudgetTextTurn(w, protocol, summary+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
					}
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Unexpected.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}}
			history := fillAIContextWindow(t, store, provider, session.ID, true, grant, []modelclient.ChatMessage{
				{Role: "user", Content: oldUser}, {Role: "assistant", Content: oldAssistant},
				{Role: "user", Content: recentUser}, {Role: "assistant", Content: recentAssistant},
				{Role: "user", Content: currentIntent},
			}, 0, 256)
			oldRows := seedAIContextWindowHistory(t, store, session.ID, history[:4], now)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": currentIntent, "workspace": grant})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": "context-window-" + protocol})
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") || upstream.calls.Load() != 3 {
				t.Fatalf("window chat=%d calls=%d: %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			meta := decodeAIBudgetMeta(t, response.Body.String())
			replacement := decodeAIBudgetReplace(t, response.Body.String())
			wantText := aiContextWindowNotice + "\n\n" + summary
			if replacement.Text != wantText || replacement.GenerationID != meta.GenerationID || replacement.Reasoning != "" {
				t.Fatalf("window replacement=%#v", replacement)
			}
			if !strings.Contains(response.Body.String(), `"trimmed_history_turns":1`) {
				t.Fatal("runtime progress did not disclose the whole-turn removal")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE id=? AND status='completed' AND content=?", 1, meta.GenerationID, wantText)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE generation_id=? AND role='assistant' AND content=?", 1, meta.GenerationID, wantText)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id=?", 6, session.ID)
			for _, old := range oldRows {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE id=? AND content=?", 1, old.ID, old.Content)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id=? AND context_snapshot IS NOT NULL", 0, session.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id=? AND instr(content,?)>0", 0, session.ID, evidence)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE session_id=?", 0, session.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title='Window original title' AND version=1", 1, task.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE generation_id=? AND kind='model_turn'", 3, meta.GenerationID)
			var proposal models.AIActionProposal
			if err := store.DB.Where("generation_id=? AND status='pending'", meta.GenerationID).First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
			if confirmed.Code != http.StatusOK {
				t.Fatalf("manual confirmation=%d: %s", confirmed.Code, confirmed.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title=? AND version=2", 1, task.ID, finalTitle)
		})
	}
}

func TestAIContextWindowProtectedEvidenceOverflowKeepsProposalsUnconfirmable(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 20, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Protected task","description":%q}`, strings.Repeat("e", 3000)))
			session := createAIContextWindowSession(t, store, true, now)
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, _ map[string]any, raw []byte, w http.ResponseWriter) {
				if len(raw) > modelclient.MaxPromptBytes {
					t.Errorf("oversized protected request reached provider: %d", len(raw))
				}
				switch call {
				case 1:
					writeAIBudgetToolTurn(w, protocol, "protected-proposal", "workspace_propose", fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"title":"Must not execute"}}`, task.ID))
				case 2:
					writeAIBudgetToolTurn(w, protocol, "protected-read", "workspace_get", fmt.Sprintf(`{"type":"task","id":%q}`, task.ID))
				default:
					t.Errorf("protected evidence overflow wrongly caused call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Must not finish.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}}
			history := fillAIContextWindow(t, store, provider, session.ID, true, grant, []modelclient.ChatMessage{{Role: "user", Content: "PROTECTED-CURRENT-INTENT:"}}, 0, 1600)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": history[0].Content, "workspace": grant})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 2 || !strings.Contains(response.Body.String(), "event: error") || strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: replace") {
				t.Fatalf("protected overflow=%d calls=%d: %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			meta := decodeAIBudgetMeta(t, response.Body.String())
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE id=? AND status='failed' AND error_code='AI_PROMPT_TOO_LARGE'", 1, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE generation_id=? AND status='pending'", 1, meta.GenerationID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title='Protected task' AND version=1", 1, task.ID)
			var proposal models.AIActionProposal
			if err := store.DB.Where("generation_id=?", meta.GenerationID).First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
			assertAPIError(t, decision, http.StatusConflict, "AI_ACTION_NOT_CONFIRMABLE")
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title='Protected task' AND version=1", 1, task.ID)
		})
	}
}

func TestAIContextWindowTemporaryEvidenceOverflowDoesNotPersistBodies(t *testing.T) {
	now := time.Date(2026, 9, 21, 21, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	session := createAIContextWindowSession(t, store, false, now)
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
		if call != 1 || len(raw) > modelclient.MaxPromptBytes || !slices.Equal(aiBudgetToolNames(payload), []string{"memory_search", "memory_write", "memory_propose", "workspace_request_access"}) {
			t.Errorf("temporary request call=%d bytes=%d tools=%v", call, len(raw), aiBudgetToolNames(payload))
		}
		writeAIBudgetToolTurn(w, "openai_chat", "temporary-read", "memory_search", `{"query":"private-window-query"}`)
	})
	provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
	history := fillAIContextWindow(t, store, provider, session.ID, false, nil, []modelclient.ChatMessage{{Role: "user", Content: "PRIVATE-WINDOW-CURRENT:"}}, 0, 32)
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": history[0].Content})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: error") || upstream.calls.Load() != 1 || strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("temporary overflow=%d calls=%d: %s", response.Code, upstream.calls.Load(), response.Body.String())
	}
	meta := decodeAIBudgetMeta(t, response.Body.String())
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE id=? AND status='failed' AND error_code='AI_PROMPT_TOO_LARGE' AND content IS NULL", 1, meta.GenerationID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id=?", 0, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE session_id=?", 0, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_access_granted'", 0)
}

func TestAIContextWindowInitialOverflowRejectsBeforeAcceptance(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 22, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, _ map[string]any, _ []byte, w http.ResponseWriter) {
				t.Errorf("initial overflowing prompt called provider %d", call)
				writeAIBudgetTextTurn(w, protocol, `Must not run.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": strings.Repeat("x", modelclient.MaxPromptBytes-1)})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": "initial-window-overflow"})
			assertAPIError(t, response, http.StatusUnprocessableEntity, "AI_PROMPT_TOO_LARGE")
			if upstream.calls.Load() != 0 {
				t.Fatalf("overflow model calls=%d", upstream.calls.Load())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps", 0)
		})
	}
}

func TestAIContextWindowCompletionNoticesShareTheResponseLimit(t *testing.T) {
	for _, budgetHandoff := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_budget_handoff_%v", budgetHandoff), func(t *testing.T) {
			prefix := aiContextWindowNotice + "\n\n"
			if budgetHandoff {
				prefix += aiBudgetHandoffNotice + "\n\n"
			}
			result := harness.Result{ContextTrimmedTurns: 2, BudgetHandoff: budgetHandoff, Reasoning: "保留推理"}
			result.Text = strings.Repeat("x", modelclient.MaxResponseBytes-len(prefix)-len(result.Reasoning)-len("尾部")) + "尾部"
			text, err := aiCompletedRunText(result)
			if err != nil || text != prefix+result.Text || len(text)+len(result.Reasoning) != modelclient.MaxResponseBytes {
				t.Fatalf("exact-limit completion: bytes=%d err=%v", len(text)+len(result.Reasoning), err)
			}
			result.Reasoning += "x"
			text, err = aiCompletedRunText(result)
			if text != "" || !errors.Is(err, modelclient.ErrResponseBudget) {
				t.Fatalf("notice overflow truncated or admitted: bytes=%d err=%v", len(text), err)
			}
		})
	}
}
