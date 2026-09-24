package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTaskSubmissionExactLocation(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	r := performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"旧批次完整摘要","artifacts":[{"client_ref":"text","storage_kind":"text","name":"旧产出","content_text":"PRIVATE_ARTIFACT_BODY","requires_followup":false}]}`), map[string]string{"If-Match": `"3"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	first := decodeSubmitOutputResponse(t, r.Body.Bytes())
	r = performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"人工要求返工"}`), map[string]string{"If-Match": `"4"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = performRequest(router, "POST", "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"新批次不得冒充旧批次","artifacts":[]}`), map[string]string{"If-Match": `"5"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	second := decodeSubmitOutputResponse(t, r.Body.Bytes())
	path := "/api/v1/tasks/" + task.ID + "/submissions/" + first.Submission.ID
	r = performRequest(router, "GET", path, nil, nil)
	if r.Code != 200 {
		t.Fatalf("exact batch GET: %d %s", r.Code, r.Body.String())
	}
	var out struct {
		Data struct {
			Task struct {
				ID                  string
				Version             int64
				CurrentSubmissionID *string `json:"current_submission_id"`
			}
			Submission taskSubmissionOutput
		}
	}
	if e := json.Unmarshal(r.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Task.ID != task.ID || out.Data.Task.Version != 6 || out.Data.Task.CurrentSubmissionID == nil || *out.Data.Task.CurrentSubmissionID != second.Submission.ID || out.Data.Submission.ID != first.Submission.ID || out.Data.Submission.Status != "changes_requested" || out.Data.Submission.Summary != "旧批次完整摘要" || out.Data.Submission.ReviewReason == nil || *out.Data.Submission.ReviewReason != "人工要求返工" || len(out.Data.Submission.Artifacts) != 1 || r.Header().Get("ETag") != `"6"` {
		t.Fatal(r.Body.String())
	}
	if strings.Contains(r.Body.String(), "PRIVATE_ARTIFACT_BODY") || strings.Contains(r.Body.String(), "relative_path") {
		t.Fatal("location must not eagerly expose artifact bodies/paths")
	}
	other, _ := setupManualReviewTask(t, router)
	for _, bad := range []string{"/api/v1/tasks/" + other.ID + "/submissions/" + first.Submission.ID, "/api/v1/tasks/" + task.ID + "/submissions/" + uuid.NewString()} {
		assertAPIError(t, performRequest(router, "GET", bad, nil, nil), 404, "TASK_SUBMISSION_NOT_FOUND")
	}
	assertAPIError(t, performRequest(router, "GET", "/api/v1/tasks/"+uuid.NewString()+"/submissions/"+first.Submission.ID, nil, nil), 404, "TASK_NOT_FOUND")
	assertAPIError(t, performRequest(router, "GET", "/api/v1/tasks/"+task.ID+"/submissions/not-a-uuid", nil, nil), 400, "INVALID_SUBMISSION_ID")
	assertAPIError(t, performRequest(router, "GET", path+"?submission_id="+second.Submission.ID, nil, nil), 400, "INVALID_QUERY")
	// Deleted payload remains deleted; the exact historical batch still exists.
	r = performRequest(router, "DELETE", "/api/v1/artifacts/"+first.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"删除旧产出"}`), map[string]string{"If-Match": `"6"`})
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = performRequest(router, "GET", path, nil, nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if e := json.Unmarshal(r.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Data.Submission.Artifacts[0].DeletedAt == nil {
		t.Fatal("deleted metadata lost")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 2, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
	if out.Data.Task.Version != 7 {
		t.Fatal(fmt.Sprintf("reads must not mutate Task: %d", out.Data.Task.Version))
	}
}
