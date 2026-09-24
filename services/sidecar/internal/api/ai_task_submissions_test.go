package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITaskSubmissionsHistoryTextAndExactArtifacts(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.ConfigVersion, Scopes: []string{"work", "outputs"}})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_task_submissions")
	if !ok {
		t.Fatal("submission reader not registered")
	}
	summary := strings.Repeat("🙂中文", 1700) + "完整摘要"
	manifest, _ := json.Marshal(map[string]any{"summary": summary, "artifacts": []map[string]any{
		{"client_ref": "note", "storage_kind": "text", "name": "审查材料", "content_text": "PRIVATE_ARTIFACT_TEXT"},
		{"client_ref": "file", "storage_kind": "file", "name": "本地文件", "file_field": "attachment"},
		{"client_ref": "link", "storage_kind": "link", "name": "参考地址", "reference_url": "https://example.com/private-result"},
	}})
	created := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", string(manifest), map[string][]byte{"attachment": []byte("PRIVATE_FILE_BYTES")}, map[string]string{"If-Match": `"3"`})
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	first := decodeSubmitOutputResponse(t, created.Body.Bytes())
	read := func(input map[string]any) (map[string]any, string) {
		t.Helper()
		input["task_id"] = task.ID
		args, _ := json.Marshal(input)
		out, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"PRIVATE_FILE_BYTES", "PRIVATE_ARTIFACT_TEXT", "private-result", "relative_path", "delete_reason", "produced_by_actor"} {
			if strings.Contains(out, secret) {
				t.Fatalf("unexpected private field %s: %s", secret, out)
			}
		}
		return result, out
	}
	page, out := read(map[string]any{"view": "list"})
	rows := page["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != first.Submission.ID || rows[0].(map[string]any)["is_current"] != true || rows[0].(map[string]any)["artifact_count"] != float64(3) || page["task_status"] != "waiting_review" || strings.Contains(out, "完整摘要") {
		t.Fatal(out)
	}
	if rows[0].(map[string]any)["route"] != taskSubmissionRoute(task.ID, first.Submission.ID) || page["route"] != "/tasks/"+task.ID {
		t.Fatal("list must preserve the Task entry and exact batch links", out)
	}
	fullText := ""
	for offset := 0; ; {
		text, _ := read(map[string]any{"view": "text", "submission_id": first.Submission.ID, "field": "summary", "content_offset": offset})
		if text["route"] != taskSubmissionRoute(task.ID, first.Submission.ID) {
			t.Fatal("text must link the requested batch", text)
		}
		fullText += text["content"].(string)
		if text["next_content_offset"] == nil {
			break
		}
		offset = int(text["next_content_offset"].(float64))
		if offset != 4000 {
			t.Fatal("pagination must count Unicode characters", text)
		}
	}
	if fullText != summary {
		t.Fatal("summary truncated or Unicode split")
	}
	for offset := 0; offset < 3; offset++ {
		page, out := read(map[string]any{"view": "artifacts", "submission_id": first.Submission.ID, "limit": 1, "offset": offset})
		if page["route"] != taskSubmissionRoute(task.ID, first.Submission.ID) {
			t.Fatal("artifacts must link the requested batch", out)
		}
		items := page["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != first.Artifacts[offset].ID || page["has_more"] != (offset < 2) {
			t.Fatal(out)
		}
		if offset == 1 && (items[0].(map[string]any)["sha256"] == nil || items[0].(map[string]any)["size_bytes"] != float64(len("PRIVATE_FILE_BYTES"))) {
			t.Fatal("file metadata missing", out)
		}
	}
	// Exercise actual human review and soft deletion: reading must distinguish
	// historical changes-requested output from the latest summary-only delivery.
	review := performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"请核实最后一项"}`), map[string]string{"If-Match": `"4"`})
	if review.Code != 200 {
		t.Fatal(review.Body.String())
	}
	deleted := performRequest(router, "DELETE", "/api/v1/artifacts/"+first.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"PRIVATE_DELETE_REASON"}`), map[string]string{"If-Match": `"5"`})
	if deleted.Code != 200 {
		t.Fatal(deleted.Body.String())
	}
	created = performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"修订后的说明，无附件","artifacts":[]}`), map[string]string{"If-Match": `"6"`})
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	second := decodeSubmitOutputResponse(t, created.Body.Bytes())
	review = performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": `"7"`})
	if review.Code != 200 {
		t.Fatal(review.Body.String())
	}
	page, out = read(map[string]any{"view": "list", "limit": 1})
	latest := page["items"].([]any)[0].(map[string]any)
	if latest["id"] != second.Submission.ID || latest["status"] != "accepted" || latest["is_current"] != true || latest["artifact_count"] != float64(0) || page["task_status"] != "done" || page["task_version"] != float64(8) || page["next_offset"] != float64(1) {
		t.Fatal(out)
	}
	page, out = read(map[string]any{"view": "list", "limit": 1, "offset": 1})
	history := page["items"].([]any)[0].(map[string]any)
	if history["route"] != taskSubmissionRoute(task.ID, first.Submission.ID) || latest["route"] != taskSubmissionRoute(task.ID, second.Submission.ID) {
		t.Fatal("later submissions must not replace historical batch links", out)
	}
	if history["id"] != first.Submission.ID || history["status"] != "changes_requested" || history["is_current"] != false || history["deleted_artifact_count"] != float64(1) || page["has_more"] != false {
		t.Fatal(out)
	}
	page, out = read(map[string]any{"view": "text", "submission_id": first.Submission.ID, "field": "review_reason"})
	if page["content"] != "请核实最后一项" || page["next_content_offset"] != nil {
		t.Fatal(out)
	}
	page, out = read(map[string]any{"view": "list", "status": "pending_review"})
	if len(page["items"].([]any)) != 0 || page["current_submission_id"] != second.Submission.ID {
		t.Fatal(out)
	}
	page, out = read(map[string]any{"view": "artifacts", "submission_id": first.Submission.ID})
	if page["items"].([]any)[0].(map[string]any)["deleted_at"] == nil || strings.Contains(out, "PRIVATE_DELETE_REASON") {
		t.Fatal(out)
	}
	get, _ := registry.Get("workspace_get")
	if _, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q}`, first.Artifacts[0].ID))); err == nil {
		t.Fatal("deleted payload should remain unavailable")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 2, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=8 AND status='done'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAITaskSubmissionsScopeTargetsAndRevocation(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	other := createTaskForTaskFacts(t, router, `{"title":"另外一个任务"}`)
	created := performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"真实提交","artifacts":[]}`), map[string]string{"If-Match": `"3"`})
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	submission := decodeSubmitOutputResponse(t, created.Body.Bytes()).Submission
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	tool := &aiWorkspaceTool{api: service, name: "workspace_task_submissions", policy: harness.NewCapabilities("work", "outputs"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	for _, scopes := range [][]string{nil, {"work"}, {"work", "actions"}, {"clients"}, {"finance"}} {
		var grant *aiWorkspaceGrant
		if len(scopes) > 0 {
			grant = &aiWorkspaceGrant{ProviderVersion: provider.ConfigVersion, Scopes: scopes}
		}
		registry, err := service.aiChatToolRegistry("ephemeral", true, &provider, grant)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := registry.Get(tool.name); exists {
			t.Fatal("scope exposes output text", scopes)
		}
		denied := *tool
		denied.policy = harness.NewCapabilities(scopes...)
		if _, err := denied.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"view":"list"}`, task.ID))); !errors.Is(err, harness.ErrPermissionDenied) {
			t.Fatal("scope not checked at execution", scopes, err)
		}
	}
	for _, extra := range []string{
		`"view":"unknown"`, `"view":"list","limit":0`, `"view":"list","limit":21`, `"view":"list","offset":1001`,
		`"view":"list","status":"done"`, `"view":"list","status":null`, `"view":"list","field":"summary"`,
		`"view":"list","content_offset":0`, `"view":"list","submission_id":"` + submission.ID + `"`,
		`"view":"text","submission_id":"` + submission.ID + `","field":"summary","limit":1`,
		`"view":"text","submission_id":"` + submission.ID + `","field":"sql"`,
		`"view":"text","submission_id":"` + submission.ID + `","field":"summary","content_offset":-1`,
		`"view":"text","submission_id":"` + submission.ID + `","field":"summary","content_offset":1000001`,
		`"view":"artifacts","submission_id":"current"`, `"view":"artifacts","submission_id":null`,
		`"view":"artifacts","submission_id":"` + submission.ID + `","status":"accepted"`,
		`"view":"list","path":"C:/private"`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,%s}`, task.ID, extra))); err == nil {
			t.Fatal("invalid parameters accepted", extra)
		}
	}
	for _, view := range []string{"text", "artifacts"} {
		input := map[string]any{"task_id": other.ID, "view": view, "submission_id": submission.ID}
		if view == "text" {
			input["field"] = "summary"
		}
		args, _ := json.Marshal(input)
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Fatal("cross-task submission accepted", view)
		}
	}
	args := []byte(fmt.Sprintf(`{"task_id":%q,"view":"list"}`, other.ID))
	out, err := tool.Execute(context.Background(), args)
	if err != nil || !strings.Contains(out, `"items":[]`) || !strings.Contains(out, `"current_submission_id":null`) {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	service.restorePending.Store(true)
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("restore allowed query")
	}
	service.restorePending.Store(false)
	if err := store.DB.Model(&provider).Updates(map[string]any{"config_version": provider.ConfigVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("provider change kept access")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=4 AND status='waiting_review'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAITaskSubmissionsHarnessReadsRollupWithoutAcceptingOrKeepingGrant(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	parent, _ := createRollupParentForTest(t, router, "父任务验收依据")
	child := createChildTaskForTest(t, router, parent.ID, "真实子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil {
		t.Fatal(parent)
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if calls == 2 || calls == 3 {
			found := false
			for _, message := range body.Messages {
				content, _ := message["content"].(string)
				if message["role"] == "tool" && strings.Contains(content, *parent.CurrentSubmissionID) {
					if calls == 2 {
						found = found || strings.Contains(content, `"origin":"child_rollup"`)
					}
					if calls == 3 {
						found = found || strings.Contains(content, childRollupSummary)
					}
				}
			}
			if !found {
				t.Error("missing actual submission evidence", calls)
			}
		}
		if calls <= 2 {
			args := map[string]any{"task_id": parent.ID, "view": "list"}
			if calls == 2 {
				args["view"], args["submission_id"], args["field"] = "text", *parent.CurrentSubmissionID, "summary"
			}
			arguments, _ := json.Marshal(args)
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("submission-%d", calls), "type": "function", "function": map[string]any{"name": "workspace_task_submissions", "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}})
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 4 {
			encoded, _ := json.Marshal(body.Tools)
			if aiHasProtectedWorkspaceTool(encoded) {
				t.Error("grant carried to next message")
			}
		}
		streamMockAIDelta(w, `直属子任务已完成，父任务仍待你验收。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "submission-evidence", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查看父任务为何待验收", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "outputs"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") || !strings.Contains(r.Body.String(), "workspace_task_submissions") {
		t.Fatal(r.Body.String(), calls)
	}
	var session models.AISession
	if err := store.DB.Where("persist=1").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "谢谢"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='waiting_review'", 1, parent.ID, parent.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, *parent.CurrentSubmissionID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAITaskSubmissionsEscapingBudgetCanUseSmallerPages(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	summary := strings.Repeat("<", 5000)
	body, _ := json.Marshal(map[string]any{"summary": summary, "artifacts": []any{}})
	r := performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/submit-output", body, map[string]string{"If-Match": `"3"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	submission := decodeSubmitOutputResponse(t, r.Body.Bytes()).Submission
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	tool := &aiWorkspaceTool{api: &API{db: store.DB, maintenance: &sync.RWMutex{}}, name: "workspace_task_submissions", policy: harness.NewCapabilities("work", "outputs"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	content := ""
	for offset := 0; offset < 5000; {
		args := fmt.Sprintf(`{"task_id":%q,"view":"text","submission_id":%q,"field":"summary","content_offset":%d,"content_limit":2000}`, task.ID, submission.ID, offset)
		out, err := tool.Execute(context.Background(), []byte(args))
		if err != nil || len(out) > 24<<10 {
			t.Fatal(err, len(out))
		}
		var page struct {
			Content string `json:"content"`
			Next    *int   `json:"next_content_offset"`
		}
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		content += page.Content
		if page.Next == nil {
			break
		}
		offset = *page.Next
	}
	if content != summary {
		t.Fatal("escaped pages lost content")
	}
	for _, limit := range []int{0, 4001} {
		args := fmt.Sprintf(`{"task_id":%q,"view":"text","submission_id":%q,"field":"summary","content_limit":%d}`, task.ID, submission.ID, limit)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("invalid content_limit accepted")
		}
	}
}
