package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAICompleteAccessRequestHarnessContinuesWithFreshGrant(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				switch call {
				case 1:
					writeAIBudgetToolTurn(w, protocol, "request", "workspace_request_access", `{"scopes":["work","outputs","actions","clients"]}`)
				case 2:
					if aiBudgetHasTool(payload, "workspace_client_records") || !strings.Contains(string(raw), "requires_new_user_message") {
						t.Error("permission recommendation changed current capabilities or lost the fresh-message boundary")
					}
					writeAIBudgetTextTurn(w, protocol, `请核对下一条完整权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 3:
					writeAIBudgetToolTurn(w, protocol, "guide", "workspace_guide", `{"topic":"tasks_projects","related_topic":"people_clients"}`)
				case 4:
					if !aiBudgetHasTool(payload, "workspace_client_records") || !aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("fresh grant lost client reads or existing task actions")
					}
					writeAIBudgetToolTurn(w, protocol, "client-read", "workspace_search", `{"type":"client","query":"待核对客户"}`)
				case 5:
					if strings.Contains(string(raw), "TOOL_PERMISSION_DENIED") {
						t.Error("client query was denied after fresh grant")
					}
					writeAIBudgetToolTurn(w, protocol, "proposal", "workspace_propose", `{"action":"task.create","changes":{"title":"补选后继续处理"}}`)
				case 6:
					writeAIBudgetTextTurn(w, protocol, `已准备待确认的任务建议。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 7:
					if aiBudgetHasTool(payload, "workspace_get") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("following ungranted turn inherited access")
					}
					writeAIBudgetTextTurn(w, protocol, `本条没有工作台授权。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected call %d", call)
					writeAIBudgetTextTurn(w, protocol, "unexpected")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "继续处理任务，需要补充客户权限", "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions"}}})
			first := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if first.Code != http.StatusOK || upstream.calls.Load() != 2 || !strings.Contains(first.Body.String(), `"scopes":["work","outputs","actions","clients"]`) {
				t.Fatalf("request: %d %s", first.Code, first.Body.String())
			}
			var generation models.AIGeneration
			if err := store.DB.First(&generation).Error; err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/api/v1/ai/generations/" + generation.ID, "/api/v1/ai/sessions/" + generation.SessionID + "/messages"} {
				read := performRequest(router, http.MethodGet, path, nil, nil)
				if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"access_request":{"scopes":["work","outputs","actions","clients"]}`) {
					t.Fatalf("reload lost recommendation: %d %s", read.Code, read.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": generation.SessionID, "message": "已核对权限，继续创建任务", "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "clients"}}})
			next := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if next.Code != http.StatusOK || upstream.calls.Load() != 6 {
				t.Fatalf("continuation: %d %s", next.Code, next.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='consumed'", 1, generation.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_search' AND completed_at IS NOT NULL AND error_code IS NULL", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			if proposal.Status != "pending" {
				t.Fatalf("proposal status=%s", proposal.Status)
			}
			confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
			if confirmed.Code != http.StatusOK {
				t.Fatalf("confirmation: %d %s", confirmed.Code, confirmed.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title=?", 1, "补选后继续处理")
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": generation.SessionID, "message": "接下来呢"})
			last := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if last.Code != http.StatusOK || upstream.calls.Load() != 7 {
				t.Fatalf("no-grant turn: %d %s", last.Code, last.Body.String())
			}
		})
	}
}

func TestAIWorkspaceAccessRequestDoesNotGrantCurrentGeneration(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id = ?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, nil, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_request_access")
	if !ok {
		t.Fatal("access request tool missing without a workspace grant")
	}
	if _, ok := registry.Get("workspace_get"); ok {
		t.Fatal("request tool must not register protected read tools")
	}
	if _, ok := registry.Get("workspace_propose"); ok {
		t.Fatal("request tool must not register write proposal tools")
	}
	if !strings.Contains(workspaceSystemPrompt(nil), "不授予权限") {
		t.Fatal("ungranted prompt must explain the request-only boundary")
	}
	for _, raw := range []string{
		`{"scopes":[]}`,
		`{"scopes":["work","work"]}`,
		`{"scopes":["work","clients","outputs","finance"]}`,
		`{"scopes":["unknown"]}`,
		`{"scopes":["work"],"confirm":true}`,
	} {
		if _, err := tool.Execute(t.Context(), []byte(raw)); err == nil {
			t.Fatalf("accepted invalid request %s", raw)
		}
	}
	if _, err := tool.Execute(t.Context(), []byte(`{"scopes":["work","actions"]}`)); err == nil {
		t.Fatal("request outside an accepted generation claimed a UI effect")
	}
	var emitted []string
	ctx := withAIWorkspaceAccessRequestEmitter(context.Background(), func(scopes []string) bool {
		emitted = append([]string(nil), scopes...)
		return true
	})
	result, err := tool.Execute(ctx, []byte(`{"scopes":["work","actions"]}`))
	if err != nil || !reflect.DeepEqual(emitted, []string{"work", "actions"}) || !strings.Contains(result, `"granted":false`) {
		t.Fatalf("request=%s emitted=%v err=%v", result, emitted, err)
	}
	if _, ok := registry.Get("workspace_get"); ok {
		t.Fatal("request mutated the frozen generation tool registry")
	}
	if _, err := tool.Execute(withAIWorkspaceAccessRequestEmitter(context.Background(), func([]string) bool { return false }), []byte(`{"scopes":["clients"]}`)); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("duplicate request should be rejected without cancelling generation, got %v", err)
	}

	granted, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := granted.Get("workspace_request_access")
	if _, err := request.Execute(ctx, []byte(`{"scopes":["work"]}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("requesting already granted scope should fail, got %v", err)
	}
	// Include still-needed scopes in the next-message recommendation instead of
	// losing them when the user prepares a fresh grant from the missing scopes.
	wanted := []string{"work", "clients", "outputs", "actions"}
	if _, err := request.Execute(ctx, []byte(`{"scopes":["work","clients","outputs","actions"]}`)); err != nil || !reflect.DeepEqual(emitted, wanted) {
		t.Fatalf("complete next-message recommendation=%v err=%v", emitted, err)
	}
	if _, err := request.Execute(ctx, []byte(`{"scopes":["work","clients","outputs","actions","finance"]}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("accepted more than three missing scopes: %v", err)
	}
	if _, ok := granted.Get("workspace_client_records"); ok {
		t.Fatal("recommendation added client access to current registry")
	}
	if err := store.DB.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := persistAIWorkspaceAccessRequest(store.DB, generation.ID, wanted, "open", nowStamp(service)); err != nil {
		t.Fatal(err)
	}
	restored, err := loadOpenAIWorkspaceAccessRequest(store.DB, generation.ID)
	if err != nil || restored == nil || !reflect.DeepEqual(restored.Scopes, wanted) {
		t.Fatalf("complete recommendation was not recoverable: %+v %v", restored, err)
	}
}

func TestAIWorkspaceAccessRequestHarnessNeedsFreshUserMessage(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if !aiBudgetHasTool(payload, "workspace_request_access") || aiBudgetHasTool(payload, "workspace_get") || aiBudgetHasTool(payload, "workspace_propose") {
					t.Error("no-grant generation advertised protected tools or omitted access request")
				}
				if call == 1 {
					writeAIBudgetToolTurn(w, protocol, "access-request", "workspace_request_access", `{"scopes":["work","actions"]}`)
					return
				}
				if call == 2 && (!strings.Contains(string(raw), `granted`) || !strings.Contains(string(raw), `requires_new_user_message`)) {
					t.Error("request result did not explicitly deny current access")
				}
				writeAIBudgetTextTurn(w, protocol, `请核对权限后再发送新消息。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "请创建一项任务"})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 2 || !strings.Contains(response.Body.String(), `event: workspace_access_request`) || !strings.Contains(response.Body.String(), `"scopes":["work","actions"]`) {
				t.Fatalf("request=%d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_access_granted'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title=?", 0, "请创建一项任务")
			var saved models.AIGeneration
			if err := store.DB.Order("created_at DESC, id DESC").First(&saved).Error; err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='open'", 1, saved.ID)
			exported := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
			if exported.Code != http.StatusOK {
				t.Fatalf("business export=%d", exported.Code)
			}
			packageData := decodeBusinessImportJSON(t, exported.Body.Bytes())
			foundExclusion := false
			for _, name := range packageData.ExcludedOperationalTables {
				foundExclusion = foundExclusion || name == "ai_workspace_access_requests"
			}
			for _, table := range packageData.Tables {
				if table.Name == "ai_workspace_access_requests" {
					t.Fatal("request ledger included in portable business tables")
				}
			}
			if !foundExclusion {
				t.Fatal("request ledger missing from portable exclusion manifest")
			}
			for _, path := range []string{"/api/v1/ai/generations/" + saved.ID, "/api/v1/ai/sessions/" + saved.SessionID + "/messages"} {
				read := performRequest(router, http.MethodGet, path, nil, nil)
				if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"access_request":{"scopes":["work","actions"]}`) {
					t.Fatalf("saved request not recoverable at %s: %d %s", path, read.Code, read.Body.String())
				}
			}
			if protocol == "openai_chat" {
				dismiss := performRequest(router, http.MethodPost, "/api/v1/ai/generations/"+saved.ID+"/access-request/dismiss", nil, nil)
				if dismiss.Code != http.StatusNoContent {
					t.Fatalf("dismiss=%d %s", dismiss.Code, dismiss.Body.String())
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='dismissed'", 1, saved.ID)
			} else {
				nextBody, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": saved.SessionID, "message": "继续，但仍未授予权限"})
				next := performRequest(router, http.MethodPost, "/api/v1/ai/chat", nextBody, nil)
				if next.Code != http.StatusOK {
					t.Fatalf("next=%d %s", next.Code, next.Body.String())
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='consumed'", 1, saved.ID)
			}
			read := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+saved.ID, nil, nil)
			if read.Code != http.StatusOK || strings.Contains(read.Body.String(), `"access_request"`) {
				t.Fatalf("closed request remains visible: %d %s", read.Code, read.Body.String())
			}
		})
	}
}

func TestAIWorkspaceAccessRequestDismissDuringStreamingSurvivesCompletion(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	waiting := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, _ map[string]any, _ []byte, w http.ResponseWriter) {
		if call == 1 {
			writeAIBudgetToolTurn(w, "openai_chat", "access-request", "workspace_request_access", `{"scopes":["work","actions"]}`)
			return
		}
		close(waiting)
		<-release
		writeAIBudgetTextTurn(w, "openai_chat", `请核对权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "请创建任务"})
	type chatResult struct {
		code int
		body string
	}
	done := make(chan chatResult, 1)
	go func() {
		response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
		done <- chatResult{response.Code, response.Body.String()}
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not reach the post-request wait")
	}
	var generation models.AIGeneration
	if err := store.DB.Order("created_at DESC, id DESC").First(&generation).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Status != "streaming" {
		t.Fatalf("generation status before dismissal = %s", generation.Status)
	}
	unknown := performRequest(router, http.MethodPost, "/api/v1/ai/generations/00000000-0000-4000-8000-000000000001/access-request/dismiss", nil, nil)
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), "AI_ACCESS_REQUEST_NOT_FOUND") {
		t.Fatalf("unknown live dismissal = %d %s", unknown.Code, unknown.Body.String())
	}
	dismissPath := "/api/v1/ai/generations/" + generation.ID + "/access-request/dismiss"
	for attempt := 0; attempt < 2; attempt++ {
		dismiss := performRequest(router, http.MethodPost, dismissPath, nil, nil)
		if dismiss.Code != http.StatusNoContent {
			t.Fatalf("live dismiss attempt %d = %d %s", attempt, dismiss.Code, dismiss.Body.String())
		}
	}
	active := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+generation.ID, nil, nil)
	if active.Code != http.StatusOK || strings.Contains(active.Body.String(), `"access_request"`) {
		t.Fatalf("dismissed live request remains visible: %d %s", active.Code, active.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests", 0)
	close(release)
	released = true
	select {
	case result := <-done:
		if result.code != http.StatusOK || !strings.Contains(result.body, `event: workspace_access_request`) || upstream.calls.Load() != 2 {
			t.Fatalf("completed chat = %d calls=%d %s", result.code, upstream.calls.Load(), result.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("chat did not complete after release")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='dismissed'", 1, generation.ID)
	for _, path := range []string{"/api/v1/ai/generations/" + generation.ID, "/api/v1/ai/sessions/" + generation.SessionID + "/messages", "/api/v1/ai/inbox?kind=access_request"} {
		read := performRequest(router, http.MethodGet, path, nil, nil)
		if read.Code != http.StatusOK || strings.Contains(read.Body.String(), `"access_request":{"scopes"`) ||
			(path == "/api/v1/ai/inbox?kind=access_request" && !strings.Contains(read.Body.String(), `"access_request_total":0`)) {
			t.Fatalf("dismissed request visible at %s: %d %s", path, read.Code, read.Body.String())
		}
	}
}

func TestAIWorkspaceAccessRequestEphemeralConversationDoesNotPersist(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, _ map[string]any, _ []byte, w http.ResponseWriter) {
		if call == 1 {
			writeAIBudgetToolTurn(w, "openai_chat", "access-request", "workspace_request_access", `{"scopes":["work"]}`)
			return
		}
		writeAIBudgetTextTurn(w, "openai_chat", `请核对权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	provider := createAIBudgetProvider(t, router, store, upstream, "openai_chat")
	created := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{"title":"ephemeral access","persist":false}`), nil)
	var sessionEnvelope struct {
		Data aiSessionResponse `json:"data"`
	}
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &sessionEnvelope) != nil {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": sessionEnvelope.Data.ID, "message": "请查询工作台"})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: workspace_access_request") {
		t.Fatalf("chat=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_workspace_access_requests", 0)
}

func TestAIWorkspaceAccessRequestOpenStateSurvivesTimestampTie(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	stamp := nowStamp(a)
	older := models.AIGeneration{
		ID: "ffffffff-ffff-4fff-8fff-ffffffffffff", SessionID: f.Generation.SessionID,
		ProviderID: f.Generation.ProviderID, Status: "failed", ErrorCode: aiStringPtr("AI_TEST_FAILURE"),
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	newer := models.AIGeneration{
		ID: "00000000-0000-4000-8000-000000000001", SessionID: f.Generation.SessionID,
		ProviderID: f.Generation.ProviderID, Status: "completed",
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := f.Store.DB.Create(&older).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}
	if err := persistAIWorkspaceAccessRequest(f.Store.DB, newer.ID, []string{"clients"}, "open", stamp); err != nil {
		t.Fatal(err)
	}
	open, err := hasOpenAIWorkspaceAccessRequest(f.Store.DB, f.Generation.SessionID)
	if err != nil || !open {
		t.Fatalf("open request was hidden behind a random UUID timestamp tie: open=%v err=%v", open, err)
	}
	inbox := performRequest(f.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), newer.ID) || !strings.Contains(inbox.Body.String(), `"access_request_total":1`) {
		t.Fatalf("newer permission request missing from inbox: %d %s", inbox.Code, inbox.Body.String())
	}
	if err := consumeOpenAIWorkspaceAccessRequests(f.Store.DB, f.Generation.SessionID, stamp); err != nil {
		t.Fatal(err)
	}
	open, err = hasOpenAIWorkspaceAccessRequest(f.Store.DB, f.Generation.SessionID)
	if err != nil || open {
		t.Fatalf("consumed request remained open: open=%v err=%v", open, err)
	}
	inbox = performRequest(f.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), `"access_request_total":0`) {
		t.Fatalf("consumed request remained in inbox: %d %s", inbox.Code, inbox.Body.String())
	}
}

// Existing no-grant regressions must continue to reject protected workspace
// tools while allowing the new request-only bridge to be advertised.
func aiHasProtectedWorkspaceTool(raw []byte) bool {
	var definitions []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &definitions); err != nil {
		return true
	}
	for _, definition := range definitions {
		var name string
		if function := definition["function"]; len(function) != 0 {
			var value struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(function, &value) != nil {
				return true
			}
			name = value.Name
		} else if json.Unmarshal(definition["name"], &name) != nil {
			return true
		}
		if name == "" || (strings.HasPrefix(name, "workspace_") && name != "workspace_request_access") {
			return true
		}
	}
	return false
}

func TestAIHasProtectedWorkspaceToolIgnoresOnlyRequestBridge(t *testing.T) {
	for _, raw := range []string{
		`[{"function":{"name":"memory_search"}},{"function":{"name":"workspace_request_access"}}]`,
		`[{"name":"memory_search"},{"name":"workspace_request_access"}]`,
	} {
		if aiHasProtectedWorkspaceTool([]byte(raw)) {
			t.Fatalf("request-only bridge counted as inherited authority: %s", raw)
		}
	}
	for _, raw := range []string{
		`[{"function":{"name":"workspace_get"}}]`,
		`[{"name":"workspace_propose"}]`,
		`[{"function":{}}]`,
		`not json`,
	} {
		if !aiHasProtectedWorkspaceTool([]byte(raw)) {
			t.Fatalf("protected or malformed tool was accepted: %s", raw)
		}
	}
}
