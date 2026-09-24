package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInboxForceResolveRequiresHumanConsentAndPreservesTasks(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"例外结清","summary":"private summary","payload_json":{"secret":"never export"}}`, "")
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"tasks":[{"key":"one","title":"仍需验收的工作","review_policy":"manual","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	split := decodeInboxSplitResponse(t, r.Body.Bytes())
	args := fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":2,"changes":{"reason":"  客户取消本次交付  "}}`, item.ID)
	row := proposeTestAction(t, store, tool, args)
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.After["required_remaining_count"] != float64(1) || preview.After["resolution_mode"] != "forced" || preview.After["reason"] != "客户取消本次交付" || strings.Contains(row.PreviewJSON, "private") || strings.Contains(row.PreviewJSON, "secret") {
		t.Fatal(row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	plain := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, plain, nil), 400, "CONFIRMATION_REQUIRED")
	assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_force_resolve":false}`, row.Fingerprint)), nil), 400, "CONFIRMATION_REQUIRED")
	consent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_force_resolve":true}`, row.Fingerprint))
	// Approval audit failure must roll back the domain event and all business facts.
	if err := store.DB.Exec(`CREATE TRIGGER reject_forced_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if r = performRequest(router, "POST", path, consent, nil); r.Code != 500 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND resolution_mode IS NULL AND version=2", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='force_resolved'", 0, item.ID)
	if err := store.DB.Exec("DROP TRIGGER reject_forced_approval").Error; err != nil {
		t.Fatal(err)
	}
	if r = performRequest(router, "POST", path, consent, nil); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if r = performRequest(router, "POST", path, plain, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='resolved' AND resolution_mode='forced' AND resolution_reason='客户取消本次交付' AND resolved_by_actor_id=? AND version=3", 1, item.ID, models.BuiltinOwnerActorID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='force_resolved'", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND review_policy='manual' AND version=?", 1, split.Created[0].Task.ID, split.Created[0].Task.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND unlinked_at IS NULL AND is_required=1", 1, item.ID)
}

func TestAIInboxForceResolveRejectsForgedConsentAndManualPolicy(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"手工事项"}`, "")
	for _, changes := range []string{`{}`, `{"reason":null}`, `{"reason":" "}`, `{"reason":"例外","confirm":true}`, `{"reason":"例外","confirm_force_resolve":true}`} {
		args := fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":1,"changes":%s}`, item.ID, changes)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	args := fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":1,"changes":{"reason":"例外"}}`, item.ID)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "INBOX_FORCE_RESOLVE_NOT_REQUIRED") {
		t.Fatalf("manual policy: %v", err)
	}
	row := proposeTestAction(t, store, tool, strings.Replace(args, "inbox.force_resolve", "inbox.resolve", 1))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	for _, decision := range []string{"confirm", "reject"} {
		assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q,"confirm_force_resolve":true}`, row.Fingerprint, decision)), nil), 422, "AI_ACTION_DECISION_INVALID")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='open' AND version=1", 1, item.ID)
}

func TestAIInboxForceResolveBindsProgressAndRejectsWithoutMutation(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"进度变化保护"}`, "")
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"tasks":[{"key":"one","title":"第一项","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"},{"key":"two","title":"第二项","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	split := decodeInboxSplitResponse(t, r.Body.Bytes())
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":2,"changes":{"reason":"交付范围改变"}}`, item.ID))
	first := split.Created[0].Task
	r = performRequest(router, "POST", "/api/v1/tasks/"+first.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, first.Version)})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_force_resolve":true}`, row.Fingerprint)), nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	// A stale or risky proposal can always be declined without exceptional consent.
	r = performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='force_resolved'", 0)
}

func TestAIInboxForceResolveZeroRequiredStillNeedsConsentAndVersion(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"零必需例外"}`, "")
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"tasks":[{"key":"one","title":"可选工作","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	split := decodeInboxSplitResponse(t, r.Body.Bytes())
	r = performRequest(router, "PATCH", "/api/v1/inbox-items/"+item.ID+"/tasks/"+split.Created[0].Task.ID, []byte(`{"is_required":false}`), map[string]string{"If-Match": `"2"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":3,"changes":{"reason":"无需继续"}}`, item.ID))
	if !strings.Contains(row.PreviewJSON, `"required_task_count":0`) {
		t.Fatal(row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 400, "CONFIRMATION_REQUIRED")
	// A native decision must invalidate the old AI proposal, never overwrite it.
	r = performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/force-resolve", []byte(`{"confirm":true,"reason":"已人工结清"}`), map[string]string{"If-Match": `"3"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_force_resolve":true}`, row.Fingerprint)), nil), 409, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND resolution_reason='已人工结清' AND version=4", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}

func TestAIInboxForceResolveHarnessProposalAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	item := createInboxItemForTest(t, router, `{"title":"例外结清链路"}`, "")
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"tasks":[{"key":"one","title":"待完成工作","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 1 || calls == 2 {
			name, args := "workspace_get", fmt.Sprintf(`{"type":"inbox_item","id":%q}`, item.ID)
			if calls == 1 {
				schema, _ := json.Marshal(body["tools"])
				if !strings.Contains(string(schema), `"workspace_guide"`) || strings.Contains(string(schema), `"inbox.force_resolve"`) || strings.Contains(string(schema), "confirm_force_resolve") {
					t.Error("initial model catalog must defer proposals and never offer human consent")
				}
				if strings.Contains(string(encoded), "拆分关联暂未开放") || strings.Contains(string(encoded), "No force-resolve") {
					t.Error("stale capability prompt")
				}
			} else {
				if !strings.Contains(string(encoded), `\"required_remaining\":1`) {
					t.Error("missing real progress")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"inbox.force_resolve","inbox_item_id":%q,"expected_version":2,"changes":{"reason":"客户取消此事项"}}`, item.ID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("force-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("proposal not returned")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), "inbox.force_resolve") || !strings.Contains(string(encoded), `\"confirmed\"`) || !strings.Contains(string(encoded), "/inbox/"+item.ID) {
				t.Error("missing real confirmation receipt")
			}
			schema, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(schema) {
				t.Error("inherited permission")
			}
		}
		streamMockAIDelta(w, `请查看下方实际操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "force-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "客户取消交付，请例外结清事项", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_force_resolve":true}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "执行成功了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='force_resolved'", 1, item.ID)
}
