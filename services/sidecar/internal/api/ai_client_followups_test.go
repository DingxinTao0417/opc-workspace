package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIClientFollowupCreateRequiresConsentAndReplays(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Followup customer"}`, nil)
	actor := createActorForTest(t, router, `{"type":"person","display_name":"Local owner"}`, nil)
	args := fmt.Sprintf(`{"action":"client_followup.create","changes":{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-10-01T09:30:00+08:00","timezone":"Asia/Shanghai","channel":"phone","purpose":"Confirm delivery","notes":"Ask for feedback"}}`, client.ID, actor.ID)
	if _, err := tool.Execute(context.Background(), []byte(args)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal("followup proposal bypassed clients consent")
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "actions", "clients")
	row := proposeTestAction(t, store, tool, args)
	duplicate := proposeTestAction(t, store, tool, args)
	if duplicate.ID != row.ID {
		t.Fatal("normalized followup generated duplicate approvals")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 0)
	if !strings.Contains(row.PreviewJSON, "Followup customer") || !strings.Contains(row.PreviewJSON, "Local owner") {
		t.Fatal(row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "/clients/"+client.ID) {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
		assertAIClientRecordResponseRoute(t, r.Body.Bytes(), client.ID, "followup")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='planned' AND scheduled_at='2026-10-01T01:30:00Z' AND priority='normal'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='client_followup_created'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
}

func TestAIClientFollowupUpdateCancelAndInboxAtomicity(t *testing.T) {
	for _, scenario := range []string{"update", "cancel", "cancel-inactive", "version", "rename-client", "rename-actor", "inactive", "actor-unavailable", "domain-audit", "approval-audit", "reject"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, service, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "actions", "clients")
			client := createClientForTest(t, router, `{"name":"Client A"}`, nil)
			actor := createActorForTest(t, router, `{"type":"person","display_name":"Person A"}`, nil)
			r := performRequest(router, "POST", "/api/v1/client-followups", []byte(fmt.Sprintf(`{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-17T12:00:00Z","timezone":"UTC","channel":"phone","purpose":"Original plan","notes":"Original notes"}`, client.ID, actor.ID)), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			item := decodeClientFollowupResponse(t, r.Body.Bytes())
			if err := service.projectDueClientFollowups(context.Background()); err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE status='open'", 1)
			action, changes := "update", `{"purpose":"Changed plan","scheduled_at":"2026-09-20T12:00:00Z","notes":null}`
			if strings.HasPrefix(scenario, "cancel") {
				action, changes = "cancel", `{"reason":"No longer needed"}`
			}
			if scenario == "cancel-inactive" {
				if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"status": "inactive", "version": item.ClientVersion + 1}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "actor-unavailable" {
				replacement := createActorForTest(t, router, `{"type":"person","display_name":"Replacement"}`, nil)
				changes = fmt.Sprintf(`{"assigned_actor_id":%q}`, replacement.ID)
				actor.ID = replacement.ID
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_followup.%s","client_followup_id":%q,"expected_version":1,"changes":%s}`, action, item.ID, changes))
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			var err error
			switch scenario {
			case "version":
				r = performRequest(router, "PATCH", "/api/v1/client-followups/"+item.ID, []byte(`{"notes":"Human changed"}`), map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "rename-client":
				err = store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"name": "Client B", "version": item.ClientVersion + 1}).Error
			case "inactive":
				err = store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"status": "inactive", "version": item.ClientVersion + 1}).Error
			case "rename-actor":
				err = store.DB.Model(&models.Actor{}).Where("id=?", actor.ID).Updates(map[string]any{"display_name": "Person B", "version": 2}).Error
			case "actor-unavailable":
				err = store.DB.Model(&models.Actor{}).Where("id=?", actor.ID).Updates(map[string]any{"status": "inactive", "version": 2}).Error
			case "domain-audit", "approval-audit":
				event := "client_followup_updated"
				if scenario == "approval-audit" {
					event = "ai_workspace_action_confirmed"
				}
				err = store.DB.Exec("CREATE TRIGGER reject_followup_approval BEFORE INSERT ON workflow_events WHEN NEW.action='" + event + "' BEGIN SELECT RAISE(ABORT,'test'); END").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			decision := "confirm"
			if scenario == "reject" {
				decision = "reject"
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q}`, row.Fingerprint, decision)), nil)
			switch scenario {
			case "update", "cancel", "cancel-inactive", "reject":
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "version":
				assertAPIError(t, r, 409, "VERSION_CONFLICT")
			case "rename-client", "rename-actor":
				assertAPIError(t, r, 409, "AI_ACTION_PREVIEW_CHANGED")
			case "inactive":
				assertAPIError(t, r, 409, "CLIENT_FOLLOWUP_CLIENT_INACTIVE")
			case "actor-unavailable":
				assertAPIError(t, r, 422, "CLIENT_FOLLOWUP_ASSIGNEE_UNAVAILABLE")
			default:
				if r.Code != 500 {
					t.Fatal(r.Body.String())
				}
			}
			if scenario == "update" || strings.HasPrefix(scenario, "cancel") {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND version=2", 1, item.ID)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE status='open'", 0)
				if scenario == "update" {
					assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE notes IS NULL AND purpose='Changed plan'", 1)
				}
				if strings.HasPrefix(scenario, "cancel") {
					assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='cancelled' AND cancel_reason='No longer needed'", 1)
				}
			} else if scenario != "version" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND version=1 AND status='planned'", 1, item.ID)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE status='open'", 1)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
		})
	}
}

func TestAIClientFollowupStrictParsing(t *testing.T) {
	id := uuid.NewString()
	base := map[string]any{"action": "client_followup.create", "changes": map[string]any{"client_id": id, "assigned_actor_id": id, "scheduled_at": "2026-10-01T09:00:00+08:00", "timezone": "Asia/Shanghai", "channel": "phone", "purpose": "plan"}}
	for _, scenario := range []string{"target-null", "version-null", "wrong-target", "unknown", "missing-zone", "local-zone", "null-purpose", "fake-client", "upper-actor", "result", "consent", "bad-priority", "huge-notes"} {
		t.Run(scenario, func(t *testing.T) {
			encoded, _ := json.Marshal(base)
			var input map[string]any
			_ = json.Unmarshal(encoded, &input)
			changes := input["changes"].(map[string]any)
			switch scenario {
			case "target-null":
				input["client_followup_id"] = nil
			case "version-null":
				input["expected_version"] = nil
			case "wrong-target":
				input["focus_session_id"] = nil
			case "unknown":
				input["unknown"] = true
			case "missing-zone":
				delete(changes, "timezone")
			case "local-zone":
				changes["timezone"] = "Local"
			case "null-purpose":
				changes["purpose"] = nil
			case "fake-client":
				changes["client_id"] = "missing"
			case "upper-actor":
				changes["assigned_actor_id"] = "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"
			case "result":
				changes["result"] = "Already called"
			case "consent":
				changes["confirm"] = true
			case "bad-priority":
				changes["priority"] = "P1"
			case "huge-notes":
				changes["notes"] = strings.Repeat("x", 4001)
			}
			encoded, _ = json.Marshal(input)
			if _, err := parseAIWorkspaceAction(encoded); err == nil {
				t.Fatal("accepted invalid action")
			}
		})
	}
	for _, action := range []string{"complete", "skip", "reschedule", "delete", "update", "cancel"} {
		if _, err := parseAIWorkspaceAction([]byte(fmt.Sprintf(`{"action":"client_followup.%s","client_followup_id":%q,"expected_version":1,"changes":{}}`, action, id))); err == nil {
			t.Fatalf("accepted %s", action)
		}
	}
	if _, err := parseAIWorkspaceAction([]byte(`{"action":"task.create","client_followup_id":null,"changes":{"title":"Task"}}`)); err == nil {
		t.Fatal("mixed target")
	}
}

func TestAIClientRecordsPrivacyPaginationAndGuards(t *testing.T) {
	router, store, service, proposal, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Client A","contact_name":"secret contact","email":"secret@example.com"}`, nil)
	read := *proposal.(*aiWorkspaceTool)
	read.name, read.policy = "workspace_client_records", harness.NewCapabilities("clients")
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{nil, {"work"}, {"clients"}} {
		var grant *aiWorkspaceGrant
		if scopes != nil {
			grant = &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
		}
		registry, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, grant)
		if err != nil {
			t.Fatal(err)
		}
		_, exists := registry.Get(read.name)
		if exists != (len(scopes) > 0 && scopes[0] == "clients") {
			t.Fatal(scopes, exists)
		}
	}
	call := func(args string) map[string]any {
		t.Helper()
		text, err := read.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		for _, secret := range []string{"secret@example.com", "secret contact", "created_by_actor_id", "delete_reason", "source_id", "contact_name"} {
			if strings.Contains(text, secret) {
				t.Fatal("leaked", secret)
			}
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(text), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	body := strings.Repeat("界😀", 4500)
	r := performRequest(router, "POST", "/api/v1/clients/"+client.ID+"/activities", []byte(fmt.Sprintf(`{"kind":"note","title":"Activity record","body":%q,"occurred_at":"2026-09-17T12:00:00Z"}`, body)), nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	activity := decodeClientActivityResponse(t, r.Body.Bytes())
	var collected string
	for offset := 0; offset < 9000; offset += 4000 {
		data := call(fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_offset":%d}`, activity.ID, offset))
		if data["route"] != "/clients/"+client.ID+"?activity="+activity.ID {
			t.Fatal("activity detail lost its record location", data["route"])
		}
		collected += data["content"].(string)
	}
	if collected != body {
		t.Fatal("unicode pagination lost content")
	}
	segment := call(fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_offset":1,"content_limit":3}`, activity.ID))
	if segment["content"] != "😀界😀" || segment["next_content_offset"] != float64(4) {
		t.Fatal(segment)
	}
	items := call(`{"type":"activity","view":"list","limit":1}`)
	if items["items"].([]any)[0].(map[string]any)["route"] != "/clients/"+client.ID+"?activity="+activity.ID {
		t.Fatal("activity list lost its record location")
	}
	if len(items["items"].([]any)) != 1 || strings.Contains(fmt.Sprint(items), body[:100]) {
		t.Fatal(items)
	}
	// Followup nanosecond boundary shares the native overdue predicate.
	for _, scheduled := range []string{"2026-09-18T11:59:59.999999999Z", "2026-09-18T12:00:00Z", "2026-09-18T12:00:00.000000001Z"} {
		r = performRequest(router, "POST", "/api/v1/client-followups", []byte(fmt.Sprintf(`{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":%q,"timezone":"UTC","channel":"phone","purpose":"Plan","notes":"Private to selected scope"}`, client.ID, models.BuiltinOwnerActorID, scheduled)), nil)
		if r.Code != 201 {
			t.Fatal(r.Body.String())
		}
		item := decodeClientFollowupResponse(t, r.Body.Bytes())
		detail := call(fmt.Sprintf(`{"type":"followup","view":"detail","id":%q}`, item.ID))
		if detail["content"] != "Private to selected scope" || detail["route"] != "/clients/"+client.ID+"?followup="+item.ID {
			t.Fatal(detail)
		}
	}
	items = call(`{"type":"followup","view":"list","due_state":"overdue"}`)
	for _, raw := range items["items"].([]any) {
		item := raw.(map[string]any)
		if item["route"] != "/clients/"+client.ID+"?followup="+item["id"].(string) {
			t.Fatal("followup list lost its record location")
		}
	}
	if items["total_items"] != float64(1) {
		t.Fatal(items)
	}
	items = call(`{"type":"followup","view":"list","limit":1}`)
	if items["total_items"] != float64(3) || items["next_offset"] != float64(1) {
		t.Fatal(items)
	}
	if len(call(`{"type":"followup","view":"list","offset":1000}`)["items"].([]any)) != 0 {
		t.Fatal("offset")
	}
	for _, args := range []string{`{}`, `{"type":"followup","view":"list","limit":null}`, `{"type":"followup","view":"list","offset":1001}`, `{"type":"activity","view":"list","status":"planned"}`, `{"type":"followup","view":"list","due_state":"overdue","status":"completed"}`, `{"type":"followup","view":"list","field":"notes"}`, fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"field":"result"}`, activity.ID), fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"limit":1}`, activity.ID), fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_offset":-1}`, activity.ID)} {
		if _, err := read.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("accepted", args)
		}
	}
	r = performRequest(router, "DELETE", "/api/v1/client-activities/"+activity.ID+"?confirm=true", []byte(`{"reason":"Removed"}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if _, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"activity","view":"detail","id":%q}`, activity.ID))); err == nil {
		t.Fatal("deleted activity readable")
	}
	if call(`{"type":"activity","view":"list"}`)["total_items"] != float64(0) {
		t.Fatal("deleted activity listed")
	}
	read.policy = harness.NewCapabilities("work")
	if _, err := read.Execute(context.Background(), []byte(`{"type":"followup","view":"list"}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	read.policy = harness.NewCapabilities("clients")
	service.restorePending.Store(true)
	if _, err := read.Execute(context.Background(), []byte(`{"type":"followup","view":"list"}`)); err == nil {
		t.Fatal("restore allowed")
	}
	service.restorePending.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := read.Execute(ctx, []byte(`{"type":"followup","view":"list"}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.DB.Model(&provider).Updates(map[string]any{"version": provider.Version + 1, "config_version": provider.ConfigVersion + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := read.Execute(context.Background(), []byte(`{"type":"followup","view":"list"}`)); err == nil {
		t.Fatal("stale grant")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIClientRecordsEscapedTextCanReducePageBudget(t *testing.T) {
	router, _, _, proposal, _ := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Escaped content"}`, nil)
	body := strings.Repeat("<", 8000)
	r := performRequest(router, "POST", "/api/v1/clients/"+client.ID+"/activities", []byte(fmt.Sprintf(`{"kind":"note","title":"Long escaped text","body":%q,"occurred_at":"2026-09-17T12:00:00Z"}`, body)), nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	activity := decodeClientActivityResponse(t, r.Body.Bytes())
	read := *proposal.(*aiWorkspaceTool)
	read.name, read.policy = "workspace_client_records", harness.NewCapabilities("clients")
	var full string
	for offset := 0; offset < len(body); offset += 1000 {
		text, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_offset":%d,"content_limit":1000}`, activity.ID, offset)))
		if err != nil || len(text) > 24<<10 {
			t.Fatal(err, len(text))
		}
		var data struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(text), &data); err != nil {
			t.Fatal(err)
		}
		full += data.Content
	}
	if full != body {
		t.Fatal("escaped text was lost")
	}
	for _, args := range []string{
		fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_limit":0}`, activity.ID),
		fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"content_limit":4001}`, activity.ID),
		fmt.Sprintf(`{"type":"activity","view":"detail","id":%q,"field":""}`, activity.ID),
		`{"type":"activity","view":"list","content_limit":1}`,
		`{"type":"activity","view":"list","query":"secret"}`,
	} {
		if _, err := read.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
}

func TestAIClientFollowupHarnessReadsProposesAndReportsReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	client := createClientForTest(t, router, `{"name":"Harness Client"}`, nil)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		name, args := "workspace_client_records", `{"type":"followup","view":"list"}`
		if calls == 2 {
			if !strings.Contains(string(encoded), `\"total_items\":0`) {
				t.Error("missing real record result")
			}
			name, args = "workspace_propose", fmt.Sprintf(`{"action":"client_followup.create","changes":{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-10-01T09:00:00Z","timezone":"UTC","channel":"phone","purpose":"Harness plan"}}`, client.ID, models.BuiltinOwnerActorID)
		}
		if calls <= 2 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("client-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing proposal receipt")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), "/clients/"+client.ID) || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing confirmed receipt")
			}
			tools, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(tools) {
				t.Error("permission inherited")
			}
		}
		streamMockAIDelta(w, `请查看回访计划的操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "client-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "安排客户回访", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "clients", "actions"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 0)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "完成了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='planned'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
}
