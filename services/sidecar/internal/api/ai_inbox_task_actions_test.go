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

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInboxTaskHarnessReadProposeConfirmReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	item := createInboxItemForTest(t, router, `{"title":"对话编排"}`, "")
	task := createTaskForTaskFacts(t, router, `{"title":"已关联工作"}`)
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/tasks/"+task.ID, []byte(`{"is_required":true}`), map[string]string{"If-Match": `"1"`})
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
			name, args := "workspace_inbox_tasks", fmt.Sprintf(`{"inbox_item_id":%q}`, item.ID)
			if calls == 2 {
				if !strings.Contains(string(encoded), `\"task_version\":1`) || !strings.Contains(string(encoded), task.ID) {
					t.Error("missing real Task relation result")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"inbox.set_required","inbox_item_id":%q,"expected_version":2,"changes":{"task_id":%q,"expected_task_version":1,"is_required":false}}`, item.ID, task.ID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("relation-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("proposal did not run")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), "inbox.set_required") || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing confirmed relation receipt")
			}
			schema, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(schema) {
				t.Error("consent carried forward")
			}
		}
		streamMockAIDelta(w, `请查看下方操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "relation-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "将关联任务改为可选", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND is_required=1", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_inbox_tasks' AND status='succeeded'", 1)
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
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才执行了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND is_required=0", 1, item.ID)
}

func TestAIInboxTaskLinkApproval(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"关联工作"}`, "")
	task := createTaskForTaskFacts(t, router, `{"title":"已有任务"}`)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.link_task","inbox_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1,"is_required":true}}`, item.ID, task.ID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND task_id=? AND is_required=1 AND unlinked_at IS NULL", 1, item.ID, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND status='todo'", 1, task.ID)
}

func TestAIInboxTaskZeroRequiredAndNoopPreserveDomainSemantics(t *testing.T) {
	for _, required := range []bool{true, false} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			item := createInboxItemForTest(t, router, `{"title":"零必需不能结清"}`, "")
			r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"one","title":"唯一工作","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`), map[string]string{"If-Match": `"1"`})
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			split := decodeInboxSplitResponse(t, r.Body.Bytes())
			task := split.Created[0].Task
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.set_required","inbox_item_id":%q,"expected_version":%d,"changes":{"task_id":%q,"expected_task_version":%d,"is_required":%t}}`, item.ID, split.InboxItem.Version, task.ID, task.Version, required))
			var preview aiActionPreview
			if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.After["status"] != "tracking" {
				t.Fatal(row.PreviewJSON)
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			version, eventCount := split.InboxItem.Version, 0
			if !required {
				version++
				eventCount = 1
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=? AND status='tracking' AND resolution_mode IS NULL", 1, item.ID, version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='task_requirement_changed'", int64(eventCount), item.ID)
		})
	}
}

func TestAIInboxTaskRequirementAndUnlinkAtomicHistory(t *testing.T) {
	for _, action := range []string{"set_required", "unlink_task"} {
		t.Run(action, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			item := createInboxItemForTest(t, router, `{"title":"调整关联"}`, "")
			task := createTaskForTaskFacts(t, router, `{"title":"保留任务"}`)
			r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/tasks/"+task.ID, []byte(`{"is_required":true}`), map[string]string{"If-Match": `"1"`})
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			extra := `"is_required":false`
			if action == "unlink_task" {
				extra = `"reason":"调整工作范围"`
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.%s","inbox_item_id":%q,"expected_version":2,"changes":{"task_id":%q,"expected_task_version":1,%s}}`, action, item.ID, task.ID, extra))
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			if err := store.DB.Exec(`CREATE TRIGGER fail_relation_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
				t.Fatal(err)
			}
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			r = performRequest(router, "POST", path, body, nil)
			if r.Code != 500 {
				t.Fatalf("%d %s", r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND is_required=1 AND unlinked_at IS NULL", 1, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=2", 1, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			if err := store.DB.Exec("DROP TRIGGER fail_relation_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				r = performRequest(router, "POST", path, body, nil)
				if r.Code != 200 {
					t.Fatalf("%d %s", r.Code, r.Body.String())
				}
			}
			status, event := "tracking", "task_requirement_changed"
			if action == "unlink_task" {
				status, event = "open", "task_unlinked"
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND unlinked_at IS NOT NULL AND unlink_reason=?", 1, item.ID, "调整工作范围")
			} else {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND is_required=0 AND unlinked_at IS NULL", 1, item.ID)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=3 AND status=?", 1, item.ID, status)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action=?", 1, item.ID, event)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND status='todo'", 1, task.ID)
		})
	}
}

func TestAIInboxTaskApprovalBindsRelatedProgressAndAutomaticResolution(t *testing.T) {
	for _, action := range []string{"set_required", "unlink_task"} {
		t.Run(action, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			item := createInboxItemForTest(t, router, `{"title":"自动策略"}`, "")
			body := `{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"one","title":"第一项","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"},{"key":"two","title":"第二项","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"}]}`
			r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(body), map[string]string{"If-Match": `"1"`})
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			split := decodeInboxSplitResponse(t, r.Body.Bytes())
			first, second := split.Created[0].Task, split.Created[1].Task
			extra := `"is_required":false`
			if action == "unlink_task" {
				extra = `"reason":"无需此任务"`
			}
			args := fmt.Sprintf(`{"action":"inbox.%s","inbox_item_id":%q,"expected_version":%d,"changes":{"task_id":%q,"expected_task_version":%d,%s}}`, action, item.ID, split.InboxItem.Version, second.ID, second.Version, extra)
			stale := proposeTestAction(t, store, tool, args)
			r = performRequest(router, "POST", "/api/v1/tasks/"+first.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, first.Version)})
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			// Completing only the OTHER required Task changes progress, not Inbox version.
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=? AND status='tracking'", 1, item.ID, split.InboxItem.Version)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+stale.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, stale.Fingerprint)), nil), 409, "AI_ACTION_PREVIEW_CHANGED")
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND unlinked_at IS NULL AND is_required=1", 2, item.ID)
			// A genuinely new generation can preview and approve the new effect.
			next := generation
			next.ID = first.ID
			next.Status = "streaming"
			if err := store.DB.Create(&next).Error; err != nil {
				t.Fatal(err)
			}
			newTool := *tool.(*aiWorkspaceTool)
			newTool.generationID = next.ID
			row := proposeTestAction(t, store, &newTool, args)
			if !strings.Contains(row.PreviewJSON, `"status":"resolved"`) || !strings.Contains(row.PreviewJSON, `"required_remaining_count":0`) {
				t.Fatal(row.PreviewJSON)
			}
			if err := store.DB.Model(&next).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='resolved' AND resolution_mode='automatic'", 1, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND version=?", 1, second.ID, second.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='automatically_resolved'", 1, item.ID)
		})
	}
}

func TestAIInboxTaskActionStrictInputAndStaleTask(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"严格边界"}`, "")
	task := createTaskForTaskFacts(t, router, `{"title":"真实任务"}`)
	for _, change := range []string{
		`{"task_id":"bad","expected_task_version":1,"is_required":true}`,
		fmt.Sprintf(`{"task_id":%q,"is_required":true}`, task.ID),
		fmt.Sprintf(`{"task_id":%q,"expected_task_version":1,"is_required":null}`, task.ID),
		fmt.Sprintf(`{"task_id":%q,"expected_task_version":1,"is_required":"true"}`, task.ID),
		fmt.Sprintf(`{"task_id":%q,"expected_task_version":1,"is_required":true,"actor_id":"forged"}`, task.ID),
		fmt.Sprintf(`{"task_id":%q,"expected_task_version":1,"is_required":true,"reason":"unexpected"}`, task.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"inbox.link_task","inbox_item_id":%q,"expected_version":1,"changes":%s}`, item.ID, change))); err == nil {
			t.Fatal("accepted " + change)
		}
	}
	args := fmt.Sprintf(`{"action":"inbox.link_task","inbox_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1,"is_required":false}}`, item.ID, task.ID)
	denied := *tool.(*aiWorkspaceTool)
	denied.policy = harness.NewCapabilities("work")
	if _, err := denied.Execute(context.Background(), []byte(args)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, args)
	r := performRequest(router, "PATCH", "/api/v1/tasks/"+task.ID, []byte(`{"title":"任务已改名"}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 409, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks", 0)
}

func TestAIInboxTaskReadToolPrivacyPagingHistoryAndConsent(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"只读关系","payload_json":{"secret":"source-private"}}`, "")
	first := createTaskForTaskFacts(t, router, `{"title":"首个任务","description":"task-private"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"第二任务"}`)
	version := 1
	for _, id := range []string{first.ID, second.ID} {
		r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/tasks/"+id, []byte(`{"is_required":true}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)})
		if r.Code != 201 {
			t.Fatal(r.Body.String())
		}
		version++
	}
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{{"clients"}, {"work"}} {
		registry, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		_, exists := registry.Get("workspace_inbox_tasks")
		if exists != (scopes[0] == "work") {
			t.Fatal(registry.Names())
		}
	}
	read := *tool.(*aiWorkspaceTool)
	read.name = "workspace_inbox_tasks"
	for offset := 0; offset < 2; offset++ {
		result, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q,"limit":1,"offset":%d}`, item.ID, offset)))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"source-private", "task-private", "linked_by_actor", "unlink_reason", "payload"} {
			if strings.Contains(result, secret) {
				t.Fatal(result)
			}
		}
		var page struct {
			Items []aiInboxTaskRow `json:"items"`
			More  bool             `json:"has_more"`
			Next  *int             `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatal(err)
		}
		expected := first.ID
		if offset == 1 {
			expected = second.ID
		}
		if len(page.Items) != 1 || page.Items[0].TaskID == nil || *page.Items[0].TaskID != expected || page.Items[0].Route != "/tasks/"+expected || page.More != (offset == 0) {
			t.Fatal(result)
		}
		if offset == 0 && (page.Next == nil || *page.Next != 1) {
			t.Fatal(result)
		}
	}
	for _, extra := range []string{`"limit":21`, `"offset":1001`, `"state":"all"`, `"sql":"SELECT *"`} {
		if _, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q,%s}`, item.ID, extra))); err == nil {
			t.Fatal(extra)
		}
	}
	r := performRequest(router, "DELETE", "/api/v1/inbox-items/"+item.ID+"/tasks/"+first.ID, []byte(`{"reason":"private-unlink-reason"}`), map[string]string{"If-Match": `"3"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = performRequest(router, "DELETE", "/api/v1/tasks/"+first.ID, nil, map[string]string{"If-Match": `"1"`})
	if r.Code != 204 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	result, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q,"state":"history"}`, item.ID)))
	if err != nil || !strings.Contains(result, `"task_id":null`) || !strings.Contains(result, `"route":""`) || !strings.Contains(result, "首个任务") || strings.Contains(result, "private-unlink-reason") {
		t.Fatalf("%s %v", result, err)
	}
	read.policy = harness.NewCapabilities("clients")
	if _, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q}`, item.ID))); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
