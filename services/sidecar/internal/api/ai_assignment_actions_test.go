package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAssignmentReassignEndAndAtomicity(t *testing.T) {
	for _, action := range []string{"task.reassign", "task.unassign"} {
		t.Run(action, func(t *testing.T) {
			router, store, service, tool, generation := aiActionTestFixture(t)
			task := createTaskForTaskFacts(t, router, `{"title":"原责任保留"}`)
			first := createAssignmentForTest(t, router, task.ID, "assignee", models.BuiltinOwnerActorID, task.Version, "")
			person := createActorForTest(t, router, `{"type":"person","display_name":"新负责人","notes":"PRIVATE_ACTOR_NOTES"}`, nil)
			changes := map[string]any{"role": "assignee", "assignment_id": first.Assignment.ID, "reason": "明确交接原因"}
			if action == "task.reassign" {
				changes["actor_id"] = person.ID
			}
			args, _ := json.Marshal(map[string]any{"action": action, "task_id": task.ID, "expected_version": first.Task.Version, "changes": changes})
			row := proposeTestAction(t, store, tool, string(args))
			if strings.Contains(row.PreviewJSON, "PRIVATE_ACTOR_NOTES") {
				t.Fatal("notes in preview")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			if err := store.DB.Exec(`CREATE TRIGGER fail_assignment_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE id=? AND unassigned_at IS NULL", 1, first.Assignment.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, task.ID, first.Task.Version)
			if err := store.DB.Exec("DROP TRIGGER fail_assignment_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE id=? AND unassigned_at IS NOT NULL AND reason=?", 1, first.Assignment.ID, "明确交接原因")
			wanted := int64(0)
			if action == "task.reassign" {
				wanted = 1
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND unassigned_at IS NULL", wanted, task.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			receipt, err := service.aiActionReceipts(context.Background(), generation.SessionID)
			if err != nil || !strings.Contains(receipt, "/tasks/"+task.ID) || strings.Contains(receipt, "明确交接原因") || strings.Contains(receipt, "新负责人") {
				t.Fatal(receipt, err)
			}
			query := *tool.(*aiWorkspaceTool)
			query.name = "workspace_task_assignments"
			out, err := query.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"state":"history","limit":1}`, task.ID)))
			if err != nil || !strings.Contains(out, first.Assignment.ID) || strings.Contains(out, "明确交接原因") || strings.Contains(out, "PRIVATE_ACTOR_NOTES") {
				t.Fatal(out, err)
			}
		})
	}
}

func TestAITaskAssignmentsBatchReadsExactCurrentResponsibility(t *testing.T) {
	router, _, _, tool, _ := aiActionTestFixture(t)
	first := createTaskForTaskFacts(t, router, `{"title":"已有负责人"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"待分派任务"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"候选人","notes":"PRIVATE_ACTOR_NOTES"}`, nil)
	assigned := createAssignmentForTest(t, router, first.ID, "assignee", person.ID, first.Version, "")
	query := *tool.(*aiWorkspaceTool)
	query.name = "workspace_task_assignments"
	args := fmt.Sprintf(`{"task_ids":[%q,%q],"role":"assignee"}`, second.ID, first.ID)
	body, err := query.Execute(context.Background(), []byte(args))
	if err != nil || !strings.Contains(body, `"count":2`) || !strings.Contains(body, assigned.Assignment.ID) ||
		!strings.Contains(body, `"task_version":`+fmt.Sprint(assigned.Task.Version)) || strings.Contains(body, "PRIVATE_ACTOR_NOTES") ||
		strings.Index(body, second.ID) > strings.Index(body, first.ID) {
		t.Fatalf("batch assignments=%s err=%v", body, err)
	}
	for _, invalid := range []string{
		fmt.Sprintf(`{"task_ids":[%q,%q]}`, first.ID, first.ID),
		fmt.Sprintf(`{"task_ids":[%q,%q]}`, first.ID, uuid.NewString()),
		fmt.Sprintf(`{"task_ids":[%q],"state":"history"}`, first.ID),
		fmt.Sprintf(`{"task_id":%q,"task_ids":[%q]}`, first.ID, second.ID),
	} {
		if output, err := query.Execute(context.Background(), []byte(invalid)); err == nil {
			t.Fatalf("invalid batch query accepted: %s => %s", invalid, output)
		}
	}
	query.policy = harness.NewCapabilities("actions")
	if output, err := query.Execute(context.Background(), []byte(args)); err == nil {
		t.Fatalf("missing work scope read batch: %s", output)
	}
}

func TestAIAssignmentBoundaryAndStaleCandidate(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task := createTaskForTaskFacts(t, router, `{"title":"分派门禁"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"候选人","notes":"PRIVATE"}`, nil)
	valid := fmt.Sprintf(`{"action":"task.assign","task_id":%q,"expected_version":%d,"changes":{"role":"assignee","actor_id":%q}}`, task.ID, task.Version, person.ID)
	for _, bad := range []string{
		strings.Replace(valid, `"role":"assignee"`, `"role":null`, 1),
		strings.Replace(valid, `"role":"assignee"`, `"role":"reviewer"`, 1),
		strings.Replace(valid, person.ID, uuid.NewString(), 1),
		strings.Replace(valid, person.ID, models.BuiltinSystemActorID, 1),
		strings.Replace(valid, `"changes":{`, `"confirm":true,"changes":{`, 1),
		strings.Replace(valid, `"changes":{`, `"changes":{"assigned_by_actor_id":"fake",`, 1),
		strings.Replace(valid, `"task.assign"`, `"task.unassign"`, 1),
		strings.Replace(valid, `"task.assign"`, `"task.reassign"`, 1),
	} {
		if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	denied := *tool.(*aiWorkspaceTool)
	for _, scopes := range [][]string{{"work"}, {"finance", "finance_actions"}, {"actions"}} {
		denied.policy = harness.NewCapabilities(scopes...)
		if _, err := denied.Execute(context.Background(), []byte(valid)); err == nil {
			t.Fatal("permission escalation", scopes)
		}
	}
	row := proposeTestAction(t, store, tool, valid)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	rename := performRequest(router, "PATCH", "/api/v1/actors/"+person.ID, []byte(`{"display_name":"已改名候选人"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, person.Version)})
	if rename.Code != 200 {
		t.Fatal(rename.Body.String())
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, task.ID)
	query := *tool.(*aiWorkspaceTool)
	query.name = "workspace_task_options"
	out, err := query.Execute(context.Background(), []byte(`{"type":"actor","role":"reviewer"}`))
	if err != nil || !strings.Contains(out, models.BuiltinOwnerActorID) || strings.Contains(out, person.ID) {
		t.Fatal(out, err)
	}
	query.name = "workspace_task_assignments"
	for _, args := range []string{`{"task_id":"bad"}`, fmt.Sprintf(`{"task_id":%q,"state":"any"}`, task.ID), fmt.Sprintf(`{"task_id":%q,"limit":21}`, task.ID), fmt.Sprintf(`{"task_id":%q,"reason":true}`, task.ID)} {
		if _, e := query.Execute(context.Background(), []byte(args)); e == nil {
			t.Fatal(args)
		}
	}
	query.policy = harness.NewCapabilities("clients")
	if _, e := query.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q}`, task.ID))); e != harness.ErrPermissionDenied {
		t.Fatal(e)
	}
}

func TestAIAssignmentParentReviewUsesSharedRules(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	parent := createTaskForTaskFacts(t, router, `{"title":"待补验收人的父任务","review_policy":"manual"}`)
	createAssignmentForTest(t, router, parent.ID, "assignee", models.BuiltinOwnerActorID, parent.Version, "")
	child := createChildTaskForTest(t, router, parent.ID, "子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	args := fmt.Sprintf(`{"action":"task.assign","task_id":%q,"expected_version":%d,"changes":{"role":"reviewer","actor_id":%q}}`, parent.ID, parent.Version, models.BuiltinOwnerActorID)
	row := proposeTestAction(t, store, tool, args)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	b := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", b, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil {
		t.Fatalf("parent rules bypassed: %#v", parent)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='pending_review' AND origin='child_rollup'", 1, parent.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, parent.ID)
	// A separate completed generation may propose ending reviewer responsibility.
	next := generation
	next.ID = uuid.NewString()
	next.Status = "streaming"
	if err := store.DB.Create(&next).Error; err != nil {
		t.Fatal(err)
	}
	nextTool := *tool.(*aiWorkspaceTool)
	nextTool.generationID = next.ID
	var assignment models.TaskAssignment
	if err := store.DB.Where("task_id=? AND role='reviewer' AND unassigned_at IS NULL", parent.ID).First(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	row = proposeTestAction(t, store, &nextTool, fmt.Sprintf(`{"action":"task.unassign","task_id":%q,"expected_version":%d,"changes":{"role":"reviewer","assignment_id":%q,"reason":"暂缓验收责任"}}`, parent.ID, parent.Version, assignment.ID))
	if err := store.DB.Model(&next).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	b = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", b, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "in_progress" {
		t.Fatalf("review not withdrawn: %s", parent.Status)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='withdrawn'", 1, parent.ID)
}

func TestAIAssignmentHarnessQueryProposeConfirmAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	task := createTaskForTaskFacts(t, router, `{"title":"待分派任务"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"本地责任人","notes":"PRIVATE_PERSON_DATA"}`, nil)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), "PRIVATE_PERSON_DATA") {
			t.Error("Actor notes leaked")
		}
		if calls <= 2 {
			name := "workspace_task_assignments"
			args := fmt.Sprintf(`{"task_id":%q}`, task.ID)
			if calls == 2 {
				found := false
				messages, _ := body["messages"].([]any)
				for _, message := range messages {
					m, _ := message.(map[string]any)
					content, _ := m["content"].(string)
					if m["role"] == "tool" && strings.Contains(content, fmt.Sprintf(`"task_version":%d`, task.Version)) && strings.Contains(content, task.ID) {
						found = true
					}
				}
				if !found {
					t.Error("missing actual assignment read result")
				}
				name = "workspace_propose"
				args = fmt.Sprintf(`{"action":"task.assign","task_id":%q,"expected_version":%d,"changes":{"role":"assignee","actor_id":%q}}`, task.ID, task.Version, person.ID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("assignment-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 4 {
			definitions, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(definitions) || !strings.Contains(string(encoded), "confirmed") || !strings.Contains(string(encoded), "/tasks/"+task.ID) {
				t.Error("receipt or scope boundary")
			}
		}
		streamMockAIDelta(w, `请核对分派建议。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "assignment-harness", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查询责任并提出分派建议", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, task.ID)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var session models.AISession
	if err := store.DB.Where("persist=1").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才分派了吗"})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND actor_id=? AND unassigned_at IS NULL", 1, task.ID, person.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAIAssignmentAgentReadinessAndNoRun(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprint(disable), func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
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
			task := createTaskForTaskFacts(t, router, `{"title":"只分派不执行"}`)
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.assign","task_id":%q,"expected_version":%d,"changes":{"role":"assignee","actor_id":%q}}`, task.ID, task.Version, actor.ID))
			if disable {
				if err := store.DB.Model(&adapter).Updates(map[string]any{"status": "disabled", "version": 2}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if disable {
				assertAPIError(t, r, 409, "ASSIGNMENT_ACTOR_NOT_EXECUTABLE")
			} else if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
		})
	}
}

func TestAIAssignmentExactTargetPagingAndVersionConflict(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task := createTaskForTaskFacts(t, router, `{"title":"精确任务"}`)
	other := createTaskForTaskFacts(t, router, `{"title":"不能被误改的任务"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"另一位负责人"}`, nil)
	own := createAssignmentForTest(t, router, task.ID, "assignee", models.BuiltinOwnerActorID, task.Version, "")
	foreign := createAssignmentForTest(t, router, other.ID, "assignee", models.BuiltinOwnerActorID, other.Version, "")
	bad := fmt.Sprintf(`{"action":"task.unassign","task_id":%q,"expected_version":%d,"changes":{"role":"assignee","assignment_id":%q,"reason":"不能结束别的任务"}}`, task.ID, own.Task.Version, foreign.Assignment.ID)
	if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
		t.Fatal("cross-task assignment accepted")
	}
	args := fmt.Sprintf(`{"action":"task.reassign","task_id":%q,"expected_version":%d,"changes":{"role":"assignee","assignment_id":%q,"actor_id":%q,"reason":"计划交接"}}`, task.ID, own.Task.Version, own.Assignment.ID, person.ID)
	proposal := proposeTestAction(t, store, tool, args)
	// Native UI wins the version race; AI cannot silently reassign the new record.
	body := []byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"PRIVATE_NATIVE_REASON"}`, person.ID))
	r := performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/reassign", body, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, own.Task.Version)})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	changed := decodeReassignMutation(t, r.Body.Bytes())
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"decision":"confirm","fingerprint":%q}`, proposal.Fingerprint)), nil), 409, "VERSION_CONFLICT")
	// One further human reassignment supplies two real historical pages.
	r = performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/reassign", []byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"PRIVATE_NATIVE_REASON"}`, models.BuiltinOwnerActorID)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, changed.Task.Version)})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	query := *tool.(*aiWorkspaceTool)
	query.name = "workspace_task_assignments"
	seen := map[string]bool{}
	for offset := 0; offset < 2; offset++ {
		out, err := query.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"state":"history","role":"assignee","limit":1,"offset":%d}`, task.ID, offset)))
		if err != nil || strings.Contains(out, "PRIVATE_NATIVE_REASON") || strings.Contains(out, foreign.Assignment.ID) {
			t.Fatal(out, err)
		}
		var page struct {
			Items      []aiAssignmentRow `json:"items"`
			HasMore    bool              `json:"has_more"`
			NextOffset *int              `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || seen[page.Items[0].ID] || page.HasMore != (offset == 0) || page.Items[0].UnassignedAt == nil {
			t.Fatal(out)
		}
		if offset == 0 && (page.NextOffset == nil || *page.NextOffset != 1) {
			t.Fatal(out)
		}
		seen[page.Items[0].ID] = true
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE id=? AND unassigned_at IS NULL", 1, foreign.Assignment.ID)
}

func TestAIAssignmentCreateRequiresConfirmation(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Assignment approval"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.assign","task_id":%q,"expected_version":1,"changes":{"role":"assignee","actor_id":%q}}`, task.ID, models.BuiltinOwnerActorID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, task.ID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"decision":"confirm","fingerprint":%q}`, row.Fingerprint)), nil)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND actor_id=? AND unassigned_at IS NULL", 1, task.ID, models.BuiltinOwnerActorID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=2", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='assignment_created' AND aggregate_id=?", 1, task.ID)
}
