package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiActionRecheckPayloadText(value any) string {
	var output strings.Builder
	var appendValue func(any)
	appendValue = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			for key, nested := range item {
				output.WriteString(key)
				output.WriteByte('\n')
				appendValue(nested)
			}
		case []any:
			for _, nested := range item {
				appendValue(nested)
			}
		case string:
			output.WriteString(item)
			output.WriteByte('\n')
		case float64, bool:
			_, _ = fmt.Fprintln(&output, item)
		}
	}
	appendValue(value)
	return output.String()
}

func rejectAIActionForRecheck(t *testing.T, router http.Handler, proposal models.AIActionProposal) {
	t.Helper()
	response := performRequest(
		router,
		http.MethodPost,
		"/api/v1/ai/actions/"+proposal.ID+"/decision",
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, proposal.Fingerprint)),
		nil,
	)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject proposal = %d: %s", response.Code, response.Body.String())
	}
}

func TestAIActionRecheckRebuildsStaleTaskProposalWithFreshReadAndHumanDecision(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			router, store, _, proposalTool, sourceGeneration := aiActionTestFixture(t, now)

			const (
				previewOnlySecret = "PREVIEW-ONLY-STALE-TITLE"
				oldUserMarker     = "OLD-USER-TURN-MUST-NOT-RETURN"
				historicalTitle   = "Historical requested title"
				historicalBody    = "ORIGINAL-ACTION-DESCRIPTION"
				currentTitle      = "Changed elsewhere"
				finalTitle        = "Rechecked final title"
			)
			task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":%q}`, previewOnlySecret))
			oldUserMessage := models.AIMessage{
				ID: uuid.NewString(), SessionID: sourceGeneration.SessionID, Role: "user", Status: "completed",
				Content: oldUserMarker, CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano),
			}
			if err := store.DB.Create(&oldUserMessage).Error; err != nil {
				t.Fatal(err)
			}
			oldProposal := proposeTestAction(t, store, proposalTool, fmt.Sprintf(
				`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"title":%q,"description":%q}}`,
				task.ID, historicalTitle, historicalBody,
			))
			if !strings.Contains(oldProposal.PreviewJSON, previewOnlySecret) || strings.Contains(oldProposal.ActionJSON, previewOnlySecret) {
				t.Fatalf("preview/action fixture does not isolate native preview: preview=%s action=%s", oldProposal.PreviewJSON, oldProposal.ActionJSON)
			}
			finishAIGeneration(t, store, sourceGeneration)

			updated := performRequest(
				router,
				http.MethodPatch,
				"/api/v1/tasks/"+task.ID,
				[]byte(fmt.Sprintf(`{"title":%q}`, currentTitle)),
				map[string]string{"If-Match": `"1"`},
			)
			if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` {
				t.Fatalf("native task update = %d etag=%q: %s", updated.Code, updated.Header().Get("ETag"), updated.Body.String())
			}
			decisionPath := "/api/v1/ai/actions/" + oldProposal.ID + "/decision"
			confirmOld := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, oldProposal.Fingerprint))
			assertAPIError(t, performRequest(router, http.MethodPost, decisionPath, confirmOld, nil), http.StatusConflict, "VERSION_CONFLICT")
			rejectAIActionForRecheck(t, router, oldProposal)

			// The selected historical command must not depend on the source user
			// turn remaining inside the rolling 200-message history window.
			fillers := make([]models.AIMessage, 0, 201)
			for index := 0; index < 201; index++ {
				stamp := now.Add(time.Duration(index+1) * time.Second).Format(time.RFC3339Nano)
				fillers = append(fillers, models.AIMessage{
					ID: uuid.NewString(), SessionID: sourceGeneration.SessionID, Role: "user", Status: "completed",
					Content: fmt.Sprintf("bounded filler %03d", index), CreatedAt: stamp, UpdatedAt: stamp,
				})
			}
			if err := store.DB.Create(&fillers).Error; err != nil {
				t.Fatal(err)
			}

			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				payloadText := aiActionRecheckPayloadText(payload)
				if strings.Contains(string(raw), previewOnlySecret) || strings.Contains(string(raw), oldUserMarker) {
					t.Errorf("%s call %d disclosed native preview or aged-out user content", protocol, call)
				}
				switch call {
				case 1:
					system := aiBudgetSystemPrompt(payload)
					for _, wanted := range []string{
						aiActionRecheckPrompt,
						`"source_proposal_id":"` + oldProposal.ID + `"`,
						`"source_generation_id":"` + sourceGeneration.ID + `"`,
						task.ID,
						`"expected_version":1`,
						historicalTitle,
						historicalBody,
					} {
						if !strings.Contains(system, wanted) {
							t.Errorf("%s recheck context missing %q", protocol, wanted)
						}
					}
					if !aiBudgetHasTool(payload, "workspace_get") || !aiBudgetHasTool(payload, "workspace_guide") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("%s fresh work/actions grant exposed the wrong initial catalog", protocol)
					}
					writeAIBudgetToolTurn(w, protocol, "recheck-read", "workspace_get", fmt.Sprintf(`{"type":"task","id":%q}`, task.ID))
				case 2:
					if !strings.Contains(payloadText, currentTitle) || !strings.Contains(payloadText, task.ID) || !strings.Contains(payloadText, `"version":2`) {
						t.Errorf("%s did not receive the current Task version: %s", protocol, payloadText)
					}
					writeAIBudgetToolTurn(w, protocol, "recheck-propose", "workspace_propose", fmt.Sprintf(
						`{"action":"task.update","task_id":%q,"expected_version":2,"changes":{"title":%q}}`, task.ID, finalTitle,
					))
				case 3:
					if !strings.Contains(payloadText, "NOT executed") {
						t.Errorf("%s did not preserve the new proposal as pending human work", protocol)
					}
					writeAIBudgetTextTurn(w, protocol, `已读取当前任务并生成新建议，仍需你人工确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 4:
					if aiBudgetHasTool(payload, "workspace_get") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("%s follow-up inherited workspace tools", protocol)
					}
					for _, forbidden := range []string{aiActionRecheckPrompt, historicalTitle, historicalBody, previewOnlySecret, oldUserMarker} {
						if strings.Contains(payloadText, forbidden) {
							t.Errorf("%s follow-up inherited recheck content %q", protocol, forbidden)
						}
					}
					writeAIBudgetTextTurn(w, protocol, `后续消息未继承复核附件或工作区权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected %s model call %d", protocol, call)
					writeAIBudgetTextTurn(w, protocol, `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			request := map[string]any{
				"provider_id":                  provider.ID,
				"session_id":                   sourceGeneration.SessionID,
				"message":                      "请基于当前事实复核这条被拒绝的旧建议",
				"action_receipt_generation_id": sourceGeneration.ID,
				"action_recheck_proposal_id":   oldProposal.ID,
				"workspace":                    aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}},
			}
			body, _ := json.Marshal(request)
			headers := map[string]string{"Idempotency-Key": "action-recheck-" + protocol}
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, headers)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") || upstream.calls.Load() != 3 {
				t.Fatalf("%s recheck chat = %d calls=%d: %s", protocol, response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title=? AND version=2", 1, task.ID, currentTitle)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='rejected'", 1, oldProposal.ID)

			var newProposal models.AIActionProposal
			if err := store.DB.Where("id <> ? AND status = 'pending' AND action_json LIKE ?", oldProposal.ID, "%"+finalTitle+"%").First(&newProposal).Error; err != nil {
				t.Fatalf("load replacement proposal: %v", err)
			}
			for _, wanted := range []string{task.ID, `"expected_version":2`, finalTitle} {
				if !strings.Contains(newProposal.ActionJSON, wanted) {
					t.Fatalf("replacement action missing %q: %s", wanted, newProposal.ActionJSON)
				}
			}
			confirmNew := performRequest(
				router,
				http.MethodPost,
				"/api/v1/ai/actions/"+newProposal.ID+"/decision",
				[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, newProposal.Fingerprint)),
				nil,
			)
			if confirmNew.Code != http.StatusOK || !strings.Contains(confirmNew.Body.String(), `"status":"confirmed"`) {
				t.Fatalf("confirm replacement = %d: %s", confirmNew.Code, confirmNew.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title=? AND version=3", 1, task.ID, finalTitle)

			// The attachment is part of request identity: exact replay cannot run,
			// and changing only its proposal ID cannot reuse the key.
			assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, headers), http.StatusConflict, "AI_CHAT_ALREADY_ACCEPTED")
			request["action_recheck_proposal_id"] = uuid.NewString()
			changedBody, _ := json.Marshal(request)
			assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/chat", changedBody, headers), http.StatusConflict, "IDEMPOTENCY_KEY_REUSED")
			if upstream.calls.Load() != 3 {
				t.Fatalf("%s idempotency retry called model %d times", protocol, upstream.calls.Load())
			}

			followUpBody, _ := json.Marshal(map[string]any{
				"provider_id": provider.ID,
				"session_id":  sourceGeneration.SessionID,
				"message":     "下一条普通消息",
			})
			followUp := performRequest(router, http.MethodPost, "/api/v1/ai/chat", followUpBody, nil)
			if followUp.Code != http.StatusOK || !strings.Contains(followUp.Body.String(), "event: done") || upstream.calls.Load() != 4 {
				t.Fatalf("%s ordinary follow-up = %d calls=%d: %s", protocol, followUp.Code, upstream.calls.Load(), followUp.Body.String())
			}

			var acceptedUser models.AIMessage
			if err := store.DB.Where("session_id=? AND role='user' AND content=?", sourceGeneration.SessionID, "请基于当前事实复核这条被拒绝的旧建议").First(&acceptedUser).Error; err != nil {
				t.Fatalf("load accepted recheck message: %v", err)
			}
			if acceptedUser.ContextSnapshot != nil {
				t.Fatalf("recheck attachment leaked into durable context snapshot: %s", *acceptedUser.ContextSnapshot)
			}
		})
	}
}

type aiActionRecheckFailureFixture struct {
	router     http.Handler
	store      *database.Store
	upstream   *aiBudgetMockUpstream
	provider   models.AIProvider
	generation models.AIGeneration
	proposal   models.AIActionProposal
}

func newAIActionRecheckFailureFixture(t *testing.T) *aiActionRecheckFailureFixture {
	t.Helper()
	router, store, _, proposalTool, generation := aiActionTestFixture(t)
	proposal := proposeTestAction(t, store, proposalTool, `{"action":"task.create","changes":{"title":"Rejected fixture task"}}`)
	finishAIGeneration(t, store, generation)
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, _ map[string]any, _ []byte, w http.ResponseWriter) {
		t.Errorf("validation failure reached model call %d", call)
		writeAIBudgetTextTurn(w, "openai_chat", `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
	return &aiActionRecheckFailureFixture{router: router, store: store, upstream: upstream, provider: provider, generation: generation, proposal: proposal}
}

func (fixture *aiActionRecheckFailureFixture) input() map[string]any {
	return map[string]any{
		"provider_id":                  fixture.provider.ID,
		"session_id":                   fixture.generation.SessionID,
		"message":                      "复核历史操作",
		"action_receipt_generation_id": fixture.generation.ID,
		"action_recheck_proposal_id":   fixture.proposal.ID,
		"workspace":                    aiWorkspaceGrant{ProviderVersion: fixture.provider.Version, Scopes: []string{"work", "actions"}},
	}
}

func countAIActionRecheckRows(t *testing.T, store *database.Store, table string) int64 {
	t.Helper()
	var count int64
	if err := store.DB.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func (fixture *aiActionRecheckFailureFixture) assertPreflightFailure(t *testing.T, input map[string]any, status int, code string) {
	t.Helper()
	tables := []string{"ai_messages", "ai_generations", "ai_action_proposals", "tasks"}
	before := make(map[string]int64, len(tables))
	for _, table := range tables {
		before[table] = countAIActionRecheckRows(t, fixture.store, table)
	}
	body, _ := json.Marshal(input)
	response := performRequest(fixture.router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	assertAPIError(t, response, status, code)
	for _, table := range tables {
		if after := countAIActionRecheckRows(t, fixture.store, table); after != before[table] {
			t.Fatalf("%s changed on %s: before=%d after=%d", table, code, before[table], after)
		}
	}
	if fixture.upstream.calls.Load() != 0 {
		t.Fatalf("%s reached model %d times", code, fixture.upstream.calls.Load())
	}
}

func TestAIActionRecheckRejectsUnavailableSourcesBeforeAcceptance(t *testing.T) {
	t.Run("missing exact generation", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		input := fixture.input()
		delete(input, "action_receipt_generation_id")
		fixture.assertPreflightFailure(t, input, http.StatusUnprocessableEntity, "AI_ACTION_RECHECK_SOURCE_INVALID")
	})

	t.Run("no grant", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		input := fixture.input()
		delete(input, "workspace")
		fixture.assertPreflightFailure(t, input, http.StatusUnprocessableEntity, "AI_ACTION_RECHECK_SCOPE_REQUIRED")
	})

	t.Run("insufficient action scope", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		input := fixture.input()
		input["workspace"] = aiWorkspaceGrant{ProviderVersion: fixture.provider.Version, Scopes: []string{"work"}}
		fixture.assertPreflightFailure(t, input, http.StatusUnprocessableEntity, "AI_ACTION_RECHECK_SCOPE_REQUIRED")
	})

	t.Run("foreign conversation", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		stamp := fixture.generation.CreatedAt
		foreign := models.AISession{ID: uuid.NewString(), Title: "Foreign", Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
		if err := fixture.store.DB.Create(&foreign).Error; err != nil {
			t.Fatal(err)
		}
		input := fixture.input()
		input["session_id"] = foreign.ID
		fixture.assertPreflightFailure(t, input, http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("pending proposal", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		fixture.assertPreflightFailure(t, fixture.input(), http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("confirmed proposal", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		response := performRequest(
			fixture.router,
			http.MethodPost,
			"/api/v1/ai/actions/"+fixture.proposal.ID+"/decision",
			[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, fixture.proposal.Fingerprint)),
			nil,
		)
		if response.Code != http.StatusOK {
			t.Fatalf("confirm source = %d: %s", response.Code, response.Body.String())
		}
		fixture.assertPreflightFailure(t, fixture.input(), http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("expired but not rejected proposal", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		if err := fixture.store.DB.Delete(&models.AIActionProposal{}, "id=?", fixture.proposal.ID).Error; err != nil {
			t.Fatal(err)
		}
		fixture.proposal.CreatedAt = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
		if err := fixture.store.DB.Create(&fixture.proposal).Error; err != nil {
			t.Fatal(err)
		}
		readback := performRequest(fixture.router, http.MethodGet, "/api/v1/ai/generations/"+fixture.generation.ID+"/actions", nil, nil)
		if readback.Code != http.StatusOK || !strings.Contains(readback.Body.String(), `"can_confirm":false`) || !strings.Contains(readback.Body.String(), `"status":"expired"`) {
			t.Fatalf("expired fixture = %d: %s", readback.Code, readback.Body.String())
		}
		fixture.assertPreflightFailure(t, fixture.input(), http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("nonpersistent conversation", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		if err := fixture.store.DB.Model(&models.AISession{}).Where("id=?", fixture.generation.SessionID).
			Updates(map[string]any{"persist": false, "version": 2}).Error; err != nil {
			t.Fatal(err)
		}
		fixture.assertPreflightFailure(t, fixture.input(), http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("mismatched source generation", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		otherGeneration := fixture.generation
		otherGeneration.ID = uuid.NewString()
		otherGeneration.Status = "completed"
		otherGeneration.RequestKey = nil
		otherGeneration.RequestHash = nil
		if err := fixture.store.DB.Create(&otherGeneration).Error; err != nil {
			t.Fatal(err)
		}
		input := fixture.input()
		input["action_receipt_generation_id"] = otherGeneration.ID
		fixture.assertPreflightFailure(t, input, http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("deleted proposal", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		if err := fixture.store.DB.Delete(&models.AIActionProposal{}, "id=?", fixture.proposal.ID).Error; err != nil {
			t.Fatal(err)
		}
		fixture.assertPreflightFailure(t, fixture.input(), http.StatusConflict, "AI_ACTION_RECHECK_SOURCE_UNAVAILABLE")
	})

	t.Run("prompt budget overflow", func(t *testing.T) {
		fixture := newAIActionRecheckFailureFixture(t)
		rejectAIActionForRecheck(t, fixture.router, fixture.proposal)
		input := fixture.input()
		input["message"] = strings.Repeat("z", modelclient.MaxPromptBytes-1)
		fixture.assertPreflightFailure(t, input, http.StatusUnprocessableEntity, "AI_PROMPT_TOO_LARGE")
	})
}
