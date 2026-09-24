package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInboxSplitHarnessOptionsProposeConfirmReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	item := createInboxItemForTest(t, router, `{"title":"对话原子拆分"}`, "")
	calls := 0
	var continuedTask models.Task
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls <= 3 || (calls >= 6 && calls <= 8) {
			name, args := "workspace_get", fmt.Sprintf(`{"type":"inbox_item","id":%q}`, item.ID)
			if calls == 2 {
				name, args = "workspace_task_options", `{"type":"actor"}`
			}
			if calls == 3 {
				if !strings.Contains(string(encoded), models.BuiltinOwnerActorID) {
					t.Error("missing real actor lookup")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"one","title":"新的工作","is_required":true,"assignee_actor_id":%q}]}}`, item.ID, models.BuiltinOwnerActorID)
			}
			if calls == 6 {
				if !strings.Contains(string(encoded), continuedTask.ID) || !strings.Contains(string(encoded), "created_task_ids") {
					t.Error("continuation lost exact historical identity")
				}
				name, args = "workspace_guide", `{"topic":"tasks_projects"}`
			}
			if calls == 7 {
				name, args = "workspace_get", fmt.Sprintf(`{"type":"task","id":%q}`, continuedTask.ID)
			}
			if calls == 8 {
				if !strings.Contains(string(encoded), "Manually revised after split") {
					t.Error("continuation did not read the current task")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"priority":"P0"}}`, continuedTask.ID, continuedTask.Version)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("split-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if (calls == 4 || calls == 9) && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing pending result")
		}
		if calls == 5 || calls == 10 {
			if !strings.Contains(string(encoded), "created_task_ids") {
				t.Error("missing exact split receipt in next model request")
			}
			if !strings.Contains(string(encoded), "inbox.split") || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing confirmed receipt")
			}
			schema, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(schema) {
				t.Error("scope carried forward")
			}
		}
		streamMockAIDelta(w, `请查看操作卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "split-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "把事项拆分成任务并分给我", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_task_options' AND status='succeeded'", 1)
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
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "操作完成了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 5 {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE actor_id=? AND unassigned_at IS NULL", 1, models.BuiltinOwnerActorID)
	// Simulate a manual edit between approval and the explicitly reauthorized turn.
	if err := store.DB.First(&continuedTask).Error; err != nil {
		t.Fatal(err)
	}
	continuedTask.Version++
	if err := store.DB.Model(&models.Task{}).Where("id=?", continuedTask.ID).Updates(map[string]any{"title": "Manually revised after split", "priority": "P3", "version": continuedTask.Version}).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "把刚拆分的任务优先级设为 P0", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 9 {
		t.Fatalf("continuation: %d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	var next models.AIActionProposal
	if err := store.DB.Where("id <> ?", row.ID).First(&next).Error; err != nil {
		t.Fatal(err)
	}
	if next.Status != "pending" {
		t.Fatal("continuation bypassed approval")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND priority='P3'", 1, continuedTask.ID)
	updated := decodeTestActionResponse(t, decideTestAction(t, router, next, ""))
	if updated.ResultID == nil || *updated.ResultID != continuedTask.ID {
		t.Fatal("continuation updated the wrong task")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND priority='P0'", 1, continuedTask.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "核对结果"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 10 {
		t.Fatalf("post-continuation: %d %s calls=%d", r.Code, r.Body.String(), calls)
	}
}

func TestAIInboxSplitTwentyTasksAndCanonicalDefaults(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"整批二十个任务"}`, "")
	tasks := make([]map[string]any, 20)
	for i := range tasks {
		tasks[i] = map[string]any{"key": fmt.Sprintf("k%d", i), "title": fmt.Sprintf("第 %d 项任务", i), "is_required": true, "assignee_actor_id": models.BuiltinOwnerActorID}
	}
	input := map[string]any{"action": "inbox.split", "inbox_item_id": item.ID, "expected_version": 1, "changes": map[string]any{"tasks": tasks}}
	args, _ := json.Marshal(input)
	row := proposeTestAction(t, store, tool, string(args))
	canonical := proposeTestAction(t, store, tool, row.ActionJSON)
	if row.ID != canonical.ID {
		t.Fatal("canonical split did not deduplicate")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 20)
	input["changes"] = map[string]any{"tasks": append(tasks, tasks[0])}
	args, _ = json.Marshal(input)
	if _, err := parseAIWorkspaceAction(args); err == nil {
		t.Fatal("accepted 21 tasks")
	}
}

func TestAIInboxSplitApprovalAtomicGraphAndReplay(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"待拆分事项"}`, "")
	args := fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"parent","title":"准备工作","is_required":false,"assignee_actor_id":%q},{"key":"child","parent_key":"parent","title":"交付工作","completion_criteria":"提供验收证据","review_policy":"manual","is_required":true,"assignee_actor_id":%q}]}}`, item.ID, models.BuiltinOwnerActorID, models.BuiltinOwnerActorID)
	row := proposeTestAction(t, store, tool, args)
	again := proposeTestAction(t, store, tool, args)
	if row.ID != again.ID {
		t.Fatal("duplicate proposal")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Tasks) != 2 || preview.Tasks[1].ParentKey != "parent" || preview.Tasks[1].Reviewer == nil || preview.Tasks[1].Reviewer.ID != models.BuiltinOwnerActorID {
		t.Fatal(row.PreviewJSON)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", path, body, nil)
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE parent_task_id IS NOT NULL AND review_policy='manual' AND completion_criteria='提供验收证据'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE unassigned_at IS NULL", 3)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE relation_type='created'", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=2", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='tasks_split'", 1, item.ID)
}

func TestAIInboxSplitConflictsAndRollback(t *testing.T) {
	for _, scenario := range []string{"actor renamed", "actor inactive", "tag renamed", "project changed", "audit failure", "second task failure", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			item := createInboxItemForTest(t, router, `{"title":"拆分事务边界"}`, "")
			actor := createActorForTest(t, router, `{"type":"person","display_name":"本地负责人","notes":"PRIVATE","metadata":{"bio":"PRIVATE"}}`, nil)
			tag := models.Tag{ID: uuid.NewString(), Name: "验收标签", Color: "#123456", Version: 1, CreatedAt: generation.CreatedAt}
			if err := store.DB.Create(&tag).Error; err != nil {
				t.Fatal(err)
			}
			project := models.Project{ID: uuid.NewString(), Name: "拆分项目", Status: "planning", Version: 1, CreatedAt: generation.CreatedAt, UpdatedAt: generation.CreatedAt}
			if err := store.DB.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			draft := fmt.Sprintf(`{"key":"one","title":"第一项","is_required":true,"assignee_actor_id":%q,"project_id":%q,"tag_ids":[%q]}`, actor.ID, project.ID, tag.ID)
			args := fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[%s,{"key":"two","title":"第二项","is_required":false,"assignee_actor_id":%q}]}}`, item.ID, draft, actor.ID)
			row := proposeTestAction(t, store, tool, args)
			if strings.Contains(row.PreviewJSON, "PRIVATE") {
				t.Fatal("private Actor fields leaked")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			var sql string
			expected := 409
			decision := "confirm"
			switch scenario {
			case "actor renamed":
				sql = fmt.Sprintf("UPDATE actors SET display_name='修改后的名字',version=version+1 WHERE id='%s'", actor.ID)
			case "actor inactive":
				sql = fmt.Sprintf("UPDATE actors SET status='inactive',version=version+1 WHERE id='%s'", actor.ID)
			case "tag renamed":
				sql = fmt.Sprintf("UPDATE tags SET name='修改后的标签',version=version+1 WHERE id='%s'", tag.ID)
			case "project changed":
				sql = fmt.Sprintf("UPDATE projects SET name='修改后的项目',version=version+1 WHERE id='%s'", project.ID)
			case "audit failure":
				sql = "CREATE TRIGGER fail_split_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END"
				expected = 500
			case "second task failure":
				sql = "CREATE TRIGGER fail_split_second BEFORE INSERT ON tasks WHEN NEW.title='第二项' BEGIN SELECT RAISE(ABORT,'test'); END"
				expected = 500
			case "rejected":
				decision = "reject"
				expected = 200
			}
			if sql != "" {
				if err := store.DB.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
			r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q}`, row.Fingerprint, decision)), nil)
			if r.Code != expected {
				t.Fatalf("%d %s", r.Code, r.Body.String())
			}
			if strings.Contains(scenario, "renamed") || scenario == "project changed" {
				if responseErrorCode(t, r.Body.Bytes()) != "AI_ACTION_PREVIEW_CHANGED" {
					t.Fatal(r.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action IN ('tasks_split','task_created_from_inbox','assignment_created','ai_workspace_action_confirmed')", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=1 AND status='open'", 1, item.ID)
			status := "pending"
			if decision == "reject" {
				status = "rejected"
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status=?", 1, row.ID, status)
		})
	}
}

func TestAIInboxSplitRejectsMalformedBatch(t *testing.T) {
	validDraft := fmt.Sprintf(`{"key":"one","title":"有效工作","is_required":true,"assignee_actor_id":%q}`, models.BuiltinOwnerActorID)
	for _, changes := range []string{
		`{"tasks":[]}`, `{"tasks":[null]}`,
		`{"tasks":[` + validDraft + `,` + validDraft + `]}`,
		strings.Replace(`{"tasks":[`+validDraft+`]}`, `"is_required":true`, `"is_required":null`, 1),
		strings.Replace(`{"tasks":[`+validDraft+`]}`, `"key":"one"`, `"key":"one","parent_key":"later"`, 1),
		strings.Replace(`{"tasks":[`+validDraft+`]}`, `"key":"one"`, `"key":"one","status":"done"`, 1),
		`{"tasks":[` + validDraft + `],"force":true}`,
		`{"resolution_policy":"all_required_tasks_done","tasks":[` + strings.Replace(validDraft, `"is_required":true`, `"is_required":false`, 1) + `]}`,
	} {
		args := fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":%s}`, uuid.NewString(), changes)
		if _, err := parseAIWorkspaceAction([]byte(args)); err == nil {
			t.Fatalf("accepted %s", changes)
		}
	}
}

func TestAITaskOptionsBoundedAndAuthorized(t *testing.T) {
	router, store, _, proposalTool, _ := aiActionTestFixture(t)
	actor := createActorForTest(t, router, `{"type":"person","display_name":"Alice","notes":"PRIVATE","metadata":{"bio":"PRIVATE"}}`, nil)
	createActorForTest(t, router, `{"type":"person","display_name":"Alice"}`, nil)
	createActorForTest(t, router, `{"type":"person","display_name":"Inactive","status":"inactive"}`, nil)
	tool := *proposalTool.(*aiWorkspaceTool)
	tool.name = "workspace_task_options"
	first, err := tool.Execute(context.Background(), []byte(`{"type":"actor","query":"Alice","limit":1}`))
	if err != nil || !strings.Contains(first, `"has_more":true`) || strings.Contains(first, "PRIVATE") {
		t.Fatalf("%s %v", first, err)
	}
	second, err := tool.Execute(context.Background(), []byte(`{"type":"actor","query":"Alice","limit":1,"offset":1}`))
	if err != nil || !strings.Contains(second, `"has_more":false`) || !strings.Contains(first+second, actor.ID) {
		t.Fatalf("%s %s %v", first, second, err)
	}
	all, err := tool.Execute(context.Background(), []byte(`{"type":"actor"}`))
	if err != nil || strings.Contains(all, "Inactive") || strings.Contains(all, models.BuiltinSystemActorID) {
		t.Fatalf("%s %v", all, err)
	}
	for _, args := range []string{`{"type":"invoice"}`, `{"type":"actor","limit":0}`, `{"type":"actor","offset":1001}`, `{"type":"actor","sql":"anything"}`} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("accepted", args)
		}
	}
	tag := models.Tag{ID: uuid.NewString(), Name: "Alpha", Color: "#123456", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), []byte(`{"type":"tag"}`))
	if err != nil || !strings.Contains(result, tag.ID) {
		t.Fatalf("%s %v", result, err)
	}
	tool.policy = harness.NewCapabilities("clients")
	if _, err := tool.Execute(context.Background(), []byte(`{"type":"actor"}`)); err != harness.ErrPermissionDenied {
		t.Fatalf("missing scope: %v", err)
	}
}

func TestAITaskOptionsExactTagSurvivesRenameAndRejectsAmbiguity(t *testing.T) {
	_, store, _, proposalTool, _ := aiActionTestFixture(t)
	tool := *proposalTool.(*aiWorkspaceTool)
	tool.name = "workspace_task_options"
	tag := models.Tag{ID: uuid.NewString(), Name: "当前新名称", Color: "#123456", Version: 2, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"type":"tag","id":%q}`, tag.ID)
	result, err := tool.Execute(t.Context(), []byte(args))
	if err != nil || !strings.Contains(result, tag.Name) || !strings.Contains(result, `"version":2`) || !strings.Contains(result, `"has_more":false`) {
		t.Fatalf("exact tag=%s err=%v", result, err)
	}
	for _, raw := range []string{
		`{"type":"tag","id":null}`, `{"type":"tag","id":"bad"}`,
		fmt.Sprintf(`{"type":"tag","ID":%q}`, tag.ID),
		fmt.Sprintf(`{"type":"tag","id":%q,"query":""}`, tag.ID),
		fmt.Sprintf(`{"type":"tag","id":%q,"offset":0}`, tag.ID),
		fmt.Sprintf(`{"type":"tag","id":%q,"limit":1}`, tag.ID),
		fmt.Sprintf(`{"type":"actor","id":%q}`, tag.ID),
		fmt.Sprintf(`{"type":"tag","id":%q}`, uuid.NewString()),
	} {
		if _, err := tool.Execute(t.Context(), []byte(raw)); err == nil {
			t.Fatalf("accepted ambiguous/missing tag: %s", raw)
		}
	}
	tool.policy = harness.NewCapabilities("clients")
	if _, err := tool.Execute(t.Context(), []byte(args)); err != harness.ErrPermissionDenied {
		t.Fatalf("scope bypass: %v", err)
	}
}

func TestAIInboxSplitAgentAssignmentNeverStartsExecution(t *testing.T) {
	for _, disableBeforeConfirmation := range []bool{false, true} {
		t.Run(fmt.Sprint(disableBeforeConfirmation), func(t *testing.T) {
			router, store, _, proposalTool, generation := aiActionTestFixture(t)
			stamp := generation.CreatedAt
			adapter := models.AgentAdapter{ID: uuid.NewString(), AdapterKey: "builtin-local-text-v1", Kind: "builtin", DisplayName: "本地诊断", ExecutableRef: "builtin:local-text-v1", ManifestJSON: "{}", ProtocolVersion: "opc-agent-pipe-v1", Status: "enabled", HealthStatus: "healthy", IsolationStatus: "verified", ExecutionReady: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
			adapter.LastHealthAt = &stamp
			if err := store.DB.Create(&adapter).Error; err != nil {
				t.Fatal(err)
			}
			actor := models.Actor{ID: uuid.NewString(), Type: "agent", DisplayName: "就绪执行器", Status: "active", AgentAdapterID: &adapter.ID, MetadataJSON: "{}", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
			if err := store.DB.Create(&actor).Error; err != nil {
				t.Fatal(err)
			}
			options := *proposalTool.(*aiWorkspaceTool)
			options.name = "workspace_task_options"
			result, err := options.Execute(context.Background(), []byte(`{"type":"actor"}`))
			if err != nil || !strings.Contains(result, actor.ID) || strings.Contains(result, adapter.ID) || strings.Contains(result, adapter.ExecutableRef) {
				t.Fatalf("%s %v", result, err)
			}
			item := createInboxItemForTest(t, router, `{"title":"只分派不执行"}`, "")
			args := fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"one","title":"执行器待办","is_required":true,"assignee_actor_id":%q}]}}`, item.ID, actor.ID)
			row := proposeTestAction(t, store, proposalTool, args)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			if disableBeforeConfirmation {
				if err := store.DB.Model(&adapter).Updates(map[string]any{"status": "disabled", "version": adapter.Version + 1, "updated_at": stamp}).Error; err != nil {
					t.Fatal(err)
				}
				result, err = options.Execute(context.Background(), []byte(`{"type":"actor"}`))
				if err != nil || strings.Contains(result, actor.ID) {
					t.Fatalf("disabled actor exposed: %s %v", result, err)
				}
			}
			r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if disableBeforeConfirmation {
				assertAPIError(t, r, 409, "ASSIGNMENT_ACTOR_NOT_EXECUTABLE")
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			} else {
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='todo'", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE actor_id=? AND unassigned_at IS NULL", 1, actor.ID)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
		})
	}
}
