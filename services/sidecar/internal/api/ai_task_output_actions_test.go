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

func taskOutputActionArgs(action string, task models.Task, changes any) string {
	b, _ := json.Marshal(map[string]any{"action": action, "task_id": task.ID, "expected_version": task.Version, "changes": changes})
	return string(b)
}

func TestAITaskOutputHarnessApprovalAndPermissionExpiry(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	task, _ := setupManualReviewTask(t, router)
	calls := 0
	var submittedID string
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]any
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
		}
		if calls == 1 {
			args := taskOutputActionArgs("task.submit_output", task, map[string]any{"summary": "模型起草后由人工核实", "artifacts": []any{}})
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "submit-output", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		encoded, _ := json.Marshal(request)
		if calls == 2 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing proposal-only receipt")
		}
		if calls == 3 {
			definitions, _ := json.Marshal(request["tools"])
			if aiHasProtectedWorkspaceTool(definitions) || !strings.Contains(string(encoded), `\"status\":\"confirmed\"`) || submittedID == "" || !strings.Contains(string(encoded), taskSubmissionRoute(task.ID, submittedID)) {
				t.Error("no-grant receipt boundary")
			}
		}
		streamMockAIDelta(w, `请核对完整产出建议。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "output harness", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if e := store.DB.First(&current, "id=?", provider.ID).Error; e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "提出提交建议，等我确认", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "outputs", "actions"}}})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 2 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions", 0)
	var proposal models.AIActionProposal
	if e := store.DB.First(&proposal).Error; e != nil {
		t.Fatal(e)
	}
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_output":true}`, proposal.Fingerprint)), nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	submitted := getTaskForTaskFacts(t, router, task.ID)
	if submitted.CurrentSubmissionID == nil {
		t.Fatal("confirmed submission identity missing")
	}
	submittedID = *submitted.CurrentSubmissionID
	var session models.AISession
	if e := store.DB.Where("persist=1").First(&session).Error; e != nil {
		t.Fatal(e)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "提交成功了吗"})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 3 {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE status='pending_review'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAITaskOutputReviewFileEvidenceAndCapacity(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := []byte(`{"summary":"人工文件证据","artifacts":[{"client_ref":"f","storage_kind":"file","name":"证据.txt","file_field":"upload","requires_followup":false}]}`)
	r := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", string(manifest), map[string][]byte{"upload": []byte("PRIVATE_FILE_BYTES")}, map[string]string{"If-Match": `"3"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	submitted := decodeSubmitOutputResponse(t, r.Body.Bytes())
	args := taskOutputActionArgs("task.review", submitted.Task, map[string]any{"submission_id": submitted.Submission.ID, "decision": "accept", "reason": "我已人工核对文件"})
	in, e := parseAIWorkspaceAction([]byte(args))
	if e != nil {
		t.Fatal(e)
	}
	p, e := previewAITaskOutputAction(store.DB, in)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.TaskOutput.Artifacts) != 1 || p.TaskOutput.Artifacts[0].Content != nil || p.TaskOutput.Artifacts[0].SHA256 == nil {
		t.Fatal("missing safe file metadata")
	}
	encoded, _ := json.Marshal(p)
	if strings.Contains(string(encoded), "PRIVATE_FILE_BYTES") || strings.Contains(string(encoded), "relative_path") {
		t.Fatal("file leaked")
	}
	in.Changes = []byte(fmt.Sprintf(`{"submission_id":%q,"decision":"accept","reason":""}`, uuid.NewString()))
	if _, e := previewAITaskOutputAction(store.DB, in); e == nil {
		t.Fatal("wrong batch accepted")
	}
	large, _ := setupManualReviewTask(t, router)
	body, _ := json.Marshal(map[string]any{"summary": "大文本", "artifacts": []any{map[string]any{"client_ref": "t", "storage_kind": "text", "name": "全部内容", "content_text": strings.Repeat("x", 100000), "requires_followup": false}}})
	r = performRequest(router, "POST", "/api/v1/tasks/"+large.ID+"/submit-output", body, map[string]string{"If-Match": `"3"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	submitted = decodeSubmitOutputResponse(t, r.Body.Bytes())
	in, e = parseAIWorkspaceAction([]byte(taskOutputActionArgs("task.review", submitted.Task, map[string]any{"submission_id": submitted.Submission.ID, "decision": "accept", "reason": ""})))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = previewAITaskOutputAction(store.DB, in); e == nil || !strings.Contains(e.Error(), "large submission") {
		t.Fatal("large review should not be truncated", e)
	}
}
func TestAITaskOutputSubmitReviewReworkAndAtomicity(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "outputs", "actions")
	task, person := setupManualReviewTask(t, router)
	content := "人工核实的完整交付正文，不应进入回执"
	changes := map[string]any{"summary": "提交摘要", "artifacts": []any{
		map[string]any{"client_ref": "text", "storage_kind": "text", "name": "结果", "content_text": content, "requires_followup": true},
		map[string]any{"client_ref": "link", "storage_kind": "link", "name": "参考", "reference_url": "https://example.com/result", "requires_followup": false},
		map[string]any{"client_ref": "json", "storage_kind": "structured", "name": "数据", "structured_json": map[string]any{"answer": 42}, "requires_followup": false},
	}}
	historicalRoutes := map[string]string{}
	for _, step := range []string{"submit", "changes", "resubmit", "accept"} {
		task = getTaskForTaskFacts(t, router, task.ID)
		if e := store.DB.Model(&generation).Update("status", "streaming").Error; e != nil {
			t.Fatal(e)
		}
		action := "task.submit_output"
		var body any = changes
		if step == "resubmit" {
			body = map[string]any{"summary": "修订后的摘要（无附件）", "artifacts": []any{}}
		}
		if step == "changes" || step == "accept" {
			action = "task.review"
			decision := "request_changes"
			if step == "accept" {
				decision = "accept"
			}
			body = map[string]any{"submission_id": *task.CurrentSubmissionID, "decision": decision, "reason": "人工决定：" + step}
		}
		args := taskOutputActionArgs(action, task, body)
		row := proposeTestAction(t, store, tool, args)
		if duplicate := proposeTestAction(t, store, tool, args); duplicate.ID != row.ID {
			t.Fatal("duplicate proposal")
		}
		if e := store.DB.Model(&generation).Update("status", "completed").Error; e != nil {
			t.Fatal(e)
		}
		path := "/api/v1/ai/actions/" + row.ID + "/decision"
		for _, consent := range []string{"", `,"confirm_task_output":false`} {
			assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, row.Fingerprint, consent)), nil), 422, "AI_TASK_OUTPUT_CONFIRMATION_REQUIRED")
		}
		confirm := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_output":true}`, row.Fingerprint))
		if e := store.DB.Exec(`CREATE TRIGGER fail_output_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test rollback'); END`).Error; e != nil {
			t.Fatal(e)
		}
		if r := performRequest(router, "POST", path, confirm, nil); r.Code != 500 {
			t.Fatalf("rollback %s: %s", step, r.Body.String())
		}
		current := getTaskForTaskFacts(t, router, task.ID)
		if current.Version != task.Version || current.Status != task.Status {
			t.Fatalf("partial transaction %s: %#v", step, current)
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
		if e := store.DB.Exec("DROP TRIGGER fail_output_approval").Error; e != nil {
			t.Fatal(e)
		}
		var output aiActionResponse
		for i := 0; i < 2; i++ {
			r := performRequest(router, "POST", path, confirm, nil)
			if r.Code != 200 {
				t.Fatalf("%s: %d %s", step, r.Code, r.Body.String())
			}
			var envelope struct{ Data aiActionResponse }
			if e := json.Unmarshal(r.Body.Bytes(), &envelope); e != nil {
				t.Fatal(e)
			}
			output = envelope.Data
		}
		current = getTaskForTaskFacts(t, router, task.ID)
		if output.ResultID == nil || current.CurrentSubmissionID == nil || *output.ResultID != *current.CurrentSubmissionID || output.ResultVersion == nil || *output.ResultVersion != task.Version+1 || output.Route != taskSubmissionRoute(task.ID, *current.CurrentSubmissionID) {
			t.Fatalf("bad receipt: %#v", output)
		}
		historicalRoutes[output.ID] = output.Route
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='ai_workspace_action_confirmed'", 1, row.ID)
		if step == "submit" {
			if current.Status != "waiting_review" {
				t.Fatal(current.Status)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=? AND produced_by_actor_id=? AND recorded_by_actor_id=?", 3, task.ID, person.ID, models.BuiltinOwnerActorID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='task_artifact'", 1)
		} else if step == "changes" {
			if current.Status != "in_progress" {
				t.Fatal(current.Status)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND unassigned_at IS NULL", 2, task.ID)
		} else if step == "accept" {
			if current.Status != "done" {
				t.Fatal(current.Status)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND unassigned_at IS NULL", 0, task.ID)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 2, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
	receipts, e := service.aiActionReceipts(context.Background(), generation.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{content, "提交摘要", "人工决定", person.DisplayName} {
		if strings.Contains(receipts, secret) {
			t.Fatal("body/name leaked to receipts")
		}
	}
	if !strings.Contains(receipts, "task.review") {
		t.Fatal("review receipt missing")
	}
	var envelope struct {
		Items []struct {
			ProposalID string `json:"proposal_id"`
			Route      string `json:"route"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(receipts), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Items) != len(historicalRoutes) {
		t.Fatal("historical receipts missing", receipts)
	}
	for _, receipt := range envelope.Items {
		if receipt.Route != historicalRoutes[receipt.ProposalID] {
			t.Fatal("new submission replaced a historical receipt", receipts)
		}
	}
}

func TestAITaskOutputScopeAndStrictParameters(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task, _ := setupManualReviewTask(t, router)
	args := taskOutputActionArgs("task.submit_output", task, map[string]any{"summary": "完整摘要", "artifacts": []any{}})
	for _, scopes := range [][]string{{"work", "actions"}, {"work", "outputs"}, {"actions", "outputs"}, {"finance", "invoice_actions"}} {
		tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
		if _, e := tool.Execute(context.Background(), []byte(args)); e != harness.ErrPermissionDenied {
			t.Fatalf("%v: %v", scopes, e)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "actions", "outputs")
	invalid := []string{
		strings.Replace(args, `"summary":"完整摘要"`, `"summary":null`, 1),
		strings.Replace(args, `"artifacts":[]`, `"artifacts":null`, 1),
		strings.Replace(args, `"artifacts":[]`, `"artifacts":[{"client_ref":"f","storage_kind":"file","name":"x","file_field":"x","requires_followup":false}]`, 1),
		strings.Replace(args, `"artifacts":[]`, `"artifacts":[{"client_ref":"x","storage_kind":"text","name":"x","content_text":"x","requires_followup":null}]`, 1),
		strings.Replace(args, `"changes":`, `"confirm_task_output":true,"changes":`, 1),
		strings.Replace(args, `"changes":`, `"project_id":null,"changes":`, 1),
		taskOutputActionArgs("task.review", task, map[string]any{"submission_id": uuid.NewString(), "decision": "request_changes", "reason": " "}),
	}
	for _, input := range invalid {
		if _, e := parseAIWorkspaceAction([]byte(input)); e == nil {
			t.Fatal("accepted invalid: " + input)
		}
	}
	if e := store.DB.Exec("UPDATE ai_sessions SET persist=0, version=version+1 WHERE id=?", generation.SessionID).Error; e != nil {
		t.Fatal(e)
	}
	if _, e := tool.Execute(context.Background(), []byte(args)); e == nil {
		t.Fatal("temporary conversation proposed write")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAITaskOutputSnapshotConflictAndReject(t *testing.T) {
	for _, change := range []string{"actor", "task", "reject"} {
		t.Run(change, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "outputs", "actions")
			task, person := setupManualReviewTask(t, router)
			row := proposeTestAction(t, store, tool, taskOutputActionArgs("task.submit_output", task, map[string]any{"summary": strings.Repeat("完整🙂", 2000), "artifacts": []any{}}))
			if !strings.Contains(row.PreviewJSON, strings.Repeat("完整🙂", 2000)) {
				t.Fatal("preview truncated")
			}
			if e := store.DB.Model(&generation).Update("status", "completed").Error; e != nil {
				t.Fatal(e)
			}
			code := "AI_ACTION_PREVIEW_CHANGED"
			if change == "actor" {
				if e := store.DB.Model(&models.Actor{}).Where("id=?", person.ID).Update("display_name", "改名").Error; e != nil {
					t.Fatal(e)
				}
			}
			if change == "task" {
				if e := store.DB.Model(&models.Task{}).Where("id=?", task.ID).Update("version", 4).Error; e != nil {
					t.Fatal(e)
				}
				code = "VERSION_CONFLICT"
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			if change == "reject" {
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_task_output":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
				if r := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil); r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			} else {
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_output":true}`, row.Fingerprint)), nil), 409, code)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, task.ID)
		})
	}
}

func TestAITaskOutputChildRollupReview(t *testing.T) {
	for _, decision := range []string{"accept", "request_changes"} {
		t.Run(decision, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "outputs", "actions")
			parent, _ := setupManualReviewTask(t, router)
			child := createChildTaskForTest(t, router, parent.ID, "子任务")
			runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
			parent = getTaskForTaskFacts(t, router, parent.ID)
			if parent.CurrentSubmissionID == nil {
				t.Fatal("rollup missing")
			}
			row := proposeTestAction(t, store, tool, taskOutputActionArgs("task.review", parent, map[string]any{"submission_id": *parent.CurrentSubmissionID, "decision": decision, "reason": "人工复核"}))
			if !strings.Contains(row.PreviewJSON, "child_rollup") {
				t.Fatal("origin missing")
			}
			if e := store.DB.Model(&generation).Update("status", "completed").Error; e != nil {
				t.Fatal(e)
			}
			r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_output":true}`, row.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			parent = getTaskForTaskFacts(t, router, parent.ID)
			want := "done"
			if decision == "request_changes" {
				want = "in_progress"
			}
			if parent.Status != want {
				t.Fatal(parent.Status)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, parent.ID)
		})
	}
}
