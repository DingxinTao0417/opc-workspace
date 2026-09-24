package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestProjectTaskFileQueryParsersRejectAmbiguity(t *testing.T) {
	for _, raw := range []string{"offset=-1", "offset=01", "offset=+1", "offset=1.0", "offset=", "offset=1&offset=2", "query=a&query=b", "limit=1", "offset=999999999999999999999", "query=%ff", "query=%00", "query=" + strings.Repeat("x", 201)} {
		if _, _, err := parseAgentRunProjectTaskFilesQuery(raw); err == nil {
			t.Errorf("accepted query %q", raw)
		}
	}
	for _, raw := range []string{"", "offset=0", "offset=20&query=" + url.QueryEscape("来源任务"), "query=100%25_"} {
		if _, _, err := parseAgentRunProjectTaskFilesQuery(raw); err != nil {
			t.Errorf("rejected query %q: %v", raw, err)
		}
	}
	id := uuid.NewString()
	for _, raw := range []string{`{}`, `null`, `{"task_id":null}`, fmt.Sprintf(`{"task_id":%q,"task_id":%q}`, id, id), fmt.Sprintf(`{"Task_ID":%q}`, id), fmt.Sprintf(`{"task_id":%q,"offset":null}`, id), fmt.Sprintf(`{"task_id":%q,"offset":1.0}`, id), fmt.Sprintf(`{"task_id":%q,"offset":-1}`, id), fmt.Sprintf(`{"task_id":%q,"query":null}`, id), fmt.Sprintf(`{"task_id":%q,"limit":1}`, id)} {
		if _, err := parseAIAgentProjectFilesQuery(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted AI query %s", raw)
		}
	}
}

func TestProjectTaskFileToolsRequireEveryScopeAndGenerationBeforeDatabase(t *testing.T) {
	scopes := []string{"work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files"}
	for index, scope := range scopes {
		granted := append([]string{}, scopes[:index]...)
		granted = append(granted, scopes[index+1:]...)
		tool := aiWorkspaceTool{policy: harness.NewCapabilities(granted...)}
		if _, err := tool.agentProjectFiles(context.Background(), nil); !errors.Is(err, harness.ErrPermissionDenied) {
			t.Fatalf("missing %s: %v", scope, err)
		}
	}
	tool := aiWorkspaceTool{policy: harness.NewCapabilities(scopes...)}
	if _, err := tool.agentProjectFiles(context.Background(), nil); err == nil {
		t.Fatal("accepted no saved generation")
	}
	tool.generationID = uuid.NewString()
	tool.agentExecutionRun = newAIAgentExecutionRun(uuid.NewString())
	if _, err := tool.agentProjectFiles(context.Background(), nil); err == nil {
		t.Fatal("accepted different generation")
	}
	tool.agentExecutionRun = newAIAgentExecutionRun(tool.generationID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.agentProjectFiles(ctx, json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, uuid.NewString()))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled before DB: %v", err)
	}
}

func TestProjectTaskFileNativeCandidatesAndPreviewAreReadOnlyExact(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(int, map[string]any, http.ResponseWriter) { t.Error("metadata path called executor") })
	router := gin.New()
	router.GET("/tasks/:id/files", f.Service.listAgentRunProjectTaskFiles)
	router.POST("/tasks/:id/files/preview", f.Service.previewAgentRunProjectTaskFiles)
	path := "/tasks/" + f.Task.ID + "/files"
	response := performRequest(router, http.MethodGet, path, nil, nil)
	if response.Code != 200 {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	var page struct {
		Data []agentRunFileCandidate `json:"data"`
		Meta struct {
			Offset int   `json:"offset"`
			Limit  int   `json:"limit"`
			Total  int64 `json:"total"`
			Next   *int  `json:"next_offset"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || page.Meta.Total != 2 || page.Meta.Limit != 20 || page.Meta.Next != nil || page.Data[0].SourceTask == nil {
		t.Fatalf("invalid page: %s", response.Body.String())
	}
	for _, candidate := range page.Data {
		if !candidate.Eligible || candidate.SourceKind != agentexec.FileSourceProjectTaskArtifact || candidate.SourceTask.TaskID != f.SourceTask.ID || candidate.SourceTask.SubmissionID != f.SourceSubmission.ID {
			t.Fatalf("invalid candidate %#v", candidate)
		}
	}
	for _, forbidden := range []string{projectTaskSelectedBody, projectTaskUnselectedBody, "objects/", "content_text", "reference_url", "structured_json"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("metadata leaked %q", forbidden)
		}
	}
	selected := make([]agentRunInputFileRequest, 2)
	for index, candidate := range page.Data {
		selected[1-index] = agentRunInputFileRequest{SourceKind: candidate.SourceKind, ID: candidate.ID, SHA256: candidate.SHA256, SourceTask: candidate.SourceTask}
	}
	body, _ := json.Marshal(agentRunProjectTaskFilesPreviewRequest{InputFiles: selected})
	preview := performRequest(router, http.MethodPost, path+"/preview", body, nil)
	if preview.Code != 200 {
		t.Fatalf("preview=%d %s", preview.Code, preview.Body.String())
	}
	var refs struct {
		Data []frozenAgentRunFileReference `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs.Data) != 2 || refs.Data[0].ID != selected[0].ID || refs.Data[1].ID != selected[1].ID || refs.Data[0].SourceTask == nil {
		t.Fatal("preview did not preserve exact selection order")
	}
	if strings.Contains(preview.Body.String(), projectTaskSelectedBody) || strings.Contains(preview.Body.String(), "objects/") {
		t.Fatal("preview leaked bytes/path")
	}
	if preview.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private metadata should not cache")
	}
	if f.Calls.Load() != 1 {
		t.Fatal("read-only endpoints executed a successor")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo'", 1, f.Task.ID)
	for _, raw := range []string{
		`null`, `{}`, `{"input_files":[]}`, `{"input_files":null}`, strings.Replace(string(body), `"input_files":`, `"Input_Files":`, 1),
		strings.Replace(string(body), `"input_files":`, `"input_files":[],"input_files":`, 1),
		strings.Replace(string(body), `"source_kind":"project_task_artifact"`, `"source_kind":"task_artifact"`, 1),
		strings.Replace(string(body), `"source_task":`, `"source_task":null,"source_task":`, 1),
		strings.Replace(string(body), `"task_version":`, `"Task_Version":`, 1),
	} {
		bad := performRequest(router, http.MethodPost, path+"/preview", []byte(raw), nil)
		if bad.Code < 400 {
			t.Errorf("accepted malformed preview %s", raw)
		}
	}
	filtered := performRequest(router, http.MethodGet, path+"?query=RESEARCH", nil, nil)
	if filtered.Code != 200 || json.Unmarshal(filtered.Body.Bytes(), &page) != nil || len(page.Data) != 1 || page.Data[0].Name != "research.md" {
		t.Fatalf("literal metadata filter: %s", filtered.Body.String())
	}
	filtered = performRequest(router, http.MethodGet, path+"?query=%25", nil, nil)
	if filtered.Code != 200 || json.Unmarshal(filtered.Body.Bytes(), &page) != nil || len(page.Data) != 0 || page.Meta.Total != 0 {
		t.Fatal("percent became wildcard")
	}
}

func TestProjectTaskFileCandidatesPageAllMatchingRowsBeforeFilteringEligibility(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(int, map[string]any, http.ResponseWriter) { t.Error("paging called executor") })
	for index := 0; index < 21; index++ {
		f.addTaskArtifactWithMutation(t, f.SourceTask.ID, fmt.Sprintf("extra-%02d.md", index), []byte("source-only fixture body"), func(artifact *models.TaskArtifact) {
			artifact.SubmissionID = f.SourceSubmission.ID
			artifact.Position = index + 3
		})
	}
	first, total, err := f.Service.loadAgentRunProjectTaskFileCandidates(f.Store.DB, f.Task, 0, "")
	if err != nil || total != 23 || len(first) != 20 {
		t.Fatalf("first=%d total=%d err=%v", len(first), total, err)
	}
	second, secondTotal, err := f.Service.loadAgentRunProjectTaskFileCandidates(f.Store.DB, f.Task, 20, "")
	if err != nil || secondTotal != 23 || len(second) != 3 {
		t.Fatalf("second=%d total=%d err=%v", len(second), secondTotal, err)
	}
	seen := map[string]bool{}
	for _, candidate := range append(first, second...) {
		if seen[candidate.ID] {
			t.Fatal("paging repeats candidates")
		}
		seen[candidate.ID] = true
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := f.Service.loadAgentRunProjectTaskFileCandidates(f.Store.DB.WithContext(ctx), f.Task, 0, ""); err == nil {
		t.Fatal("cancelled page returned evidence")
	}
	// A page cursor refers to matching metadata, not to the eligible subset.
	router := gin.New()
	router.GET("/tasks/:id/files", f.Service.listAgentRunProjectTaskFiles)
	response := performRequest(router, http.MethodGet, "/tasks/"+f.Task.ID+"/files", nil, nil)
	var page struct {
		Meta struct {
			Next *int `json:"next_offset"`
		} `json:"meta"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Meta.Next == nil || *page.Meta.Next != 20 {
		t.Fatalf("next page: %s", response.Body.String())
	}
}

func TestProjectTaskFileAIToolMinimalTargetProjectionPreservesEligibleProof(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(int, map[string]any, http.ResponseWriter) { t.Error("tool called executor") })
	generationID := uuid.NewString()
	tool := aiWorkspaceTool{api: f.Service, generationID: generationID, agentExecutionRun: newAIAgentExecutionRun(generationID),
		policy: harness.NewCapabilities("work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files")}
	value, err := tool.agentProjectFiles(context.Background(), json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, f.Task.ID)))
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.(aiAgentProjectFilesResult)
	if !ok || result.TaskVersion != f.Task.Version || len(result.InputFileCandidates) != 2 || result.Total != 2 {
		t.Fatalf("minimal target projection lost eligibility: %#v", value)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{f.SourceArtifact.ID, *f.SourceArtifact.SHA256, projectTaskSelectedBody, projectTaskUnselectedBody, "objects/"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("AI candidates leaked %q", forbidden)
		}
	}
	for _, candidate := range result.InputFileCandidates {
		bindings, err := tool.agentExecutionRun.resolveFileCandidates(f.Task.ID, []string{candidate.CandidateID})
		if err != nil || len(bindings) != 1 || bindings[0].File.SourceTask == nil || bindings[0].File.SourceTask.TaskID != f.SourceTask.ID {
			t.Fatalf("opaque candidate lost frozen source: %#v %v", bindings, err)
		}
	}
}
