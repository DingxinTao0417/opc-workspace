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
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiProjectOutputsTestFixture struct {
	router  *gin.Engine
	store   *database.Store
	tool    *aiWorkspaceTool
	project projectResponse
	task    models.Task
	output  submitOutputResponse
	inbox   models.InboxItem
}

type aiProjectOutputsPageForTest struct {
	ProjectID      string                `json:"project_id"`
	ProjectVersion int64                 `json:"project_version"`
	ProjectLabel   string                `json:"project_label"`
	ProjectStatus  string                `json:"project_status"`
	AsOf           string                `json:"as_of"`
	Items          []aiProjectOutputItem `json:"items"`
	Total          int64                 `json:"total_items"`
	Next           *int                  `json:"next_offset"`
	HasMore        bool                  `json:"has_more"`
	WindowLimited  bool                  `json:"window_limited"`
}

func readAIProjectOutputsForTest(t *testing.T, f aiProjectOutputsTestFixture, extra string) aiProjectOutputsPageForTest {
	t.Helper()
	args := fmt.Sprintf(`{"project_id":%q%s}`, f.project.ID, extra)
	encoded, err := f.tool.Execute(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("read %s: %v", args, err)
	}
	var page aiProjectOutputsPageForTest
	if err := json.Unmarshal([]byte(encoded), &page); err != nil {
		t.Fatal(err)
	}
	if page.AsOf == "" || page.Items == nil || page.ProjectID != f.project.ID || page.ProjectVersion < 1 {
		t.Fatalf("invalid page: %s", encoded)
	}
	return page
}

func TestAIProjectOutputsMatchesNativeFollowupAndIndependentVersions(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	split := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.inbox.ID+"/split", []byte(fmt.Sprintf(`{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"next","title":"Required followup","is_required":true,"assignee_actor_id":%q}]}`, models.BuiltinOwnerActorID)), map[string]string{"If-Match": `"1"`})
	if split.Code != http.StatusCreated {
		t.Fatalf("split=%d %s", split.Code, split.Body.String())
	}
	created := decodeInboxSplitResponse(t, split.Body.Bytes())
	before := readAIProjectOutputsForTest(t, f, `,"followup_status":"tracking"`)
	if before.Total != 1 || len(before.Items) != 1 || before.Items[0].Followup.Progress.RequiredRemaining != 1 {
		t.Fatalf("before=%+v", before)
	}
	completed := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+created.Created[0].Task.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, created.Created[0].Task.Version)})
	if completed.Code != http.StatusOK {
		t.Fatal(completed.Body.String())
	}
	after := readAIProjectOutputsForTest(t, f, `,"followup_status":"resolved"`)
	if after.Total != 1 || after.ProjectVersion != before.ProjectVersion || after.Items[0].Followup.InboxItemVersion <= before.Items[0].Followup.InboxItemVersion || !after.Items[0].Followup.Progress.AllRequiredDone {
		t.Fatalf("independent versions before=%+v after=%+v", before, after)
	}
	native := performRequest(f.router, http.MethodGet, "/api/v1/projects/"+f.project.ID+"/artifacts", nil, nil)
	if native.Code != http.StatusOK {
		t.Fatal(native.Body.String())
	}
	items, meta := decodeProjectArtifactList(t, native.Body.Bytes())
	if meta.ProjectVersion != after.ProjectVersion {
		t.Fatal("native project version differs")
	}
	for _, item := range items {
		if item.Artifact.ID == after.Items[0].ArtifactID && !reflect.DeepEqual(item.Followup, &after.Items[0].Followup.projectArtifactFollowupOutput) {
			t.Fatalf("native/AI followup differ: native=%+v ai=%+v", item.Followup, after.Items[0].Followup)
		}
	}
	if after.Items[0].Task.Status != "waiting_review" || after.Items[0].SubmissionStatus != "pending_review" {
		t.Fatal("resolved followup was conflated with delivery acceptance")
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAIProjectOutputsFiltersPagingExactAndCurrentProjectMembership(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	for _, test := range []struct {
		status string
		total  int64
	}{{"all", 2}, {"none", 1}, {"active", 1}, {"open", 1}, {"tracking", 0}, {"resolved", 0}, {"dismissed", 0}} {
		page := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"followup_status":%q`, test.status))
		if page.Total != test.total || len(page.Items) != int(test.total) {
			t.Fatalf("filter %s=%+v", test.status, page)
		}
		if test.status == "none" && (page.Items[0].Followup != nil || page.Items[0].ArtifactID != f.output.Artifacts[1].ID) {
			t.Fatal("none filter mismatch")
		}
	}
	first := readAIProjectOutputsForTest(t, f, `,"limit":1`)
	if first.Total != 2 || len(first.Items) != 1 || !first.HasMore || first.Next == nil || *first.Next != 1 || first.WindowLimited {
		t.Fatalf("first=%+v", first)
	}
	second := readAIProjectOutputsForTest(t, f, `,"limit":1,"offset":1`)
	if second.Total != 2 || len(second.Items) != 1 || second.HasMore || second.Next != nil || first.Items[0].ArtifactID == second.Items[0].ArtifactID {
		t.Fatalf("second=%+v", second)
	}
	exact := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, second.Items[0].ArtifactID))
	if exact.Total != 1 || len(exact.Items) != 1 || exact.Items[0].ArtifactID != second.Items[0].ArtifactID {
		t.Fatalf("exact=%+v", exact)
	}
	if empty := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, uuid.NewString())); empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatal("missing artifact not empty")
	}
	other := createProjectForTest(t, f.router, `{"name":"New delivery owner"}`, nil)
	moved := performRequest(f.router, http.MethodPatch, "/api/v1/tasks/"+f.task.ID, []byte(fmt.Sprintf(`{"project_id":%q}`, other.ID)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if moved.Code != http.StatusOK {
		t.Fatal(moved.Body.String())
	}
	if old := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, f.output.Artifacts[0].ID)); old.Total != 0 || len(old.Items) != 0 {
		t.Fatal("source Inbox snapshot overrode current project membership")
	}
	f.project = other
	current := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, f.output.Artifacts[0].ID))
	if current.Total != 1 || current.Items[0].Task.Version <= f.task.Version || current.Items[0].Followup == nil || current.Items[0].Followup.InboxItemID != f.inbox.ID {
		t.Fatalf("current=%+v", current)
	}
	if err := f.store.DB.Model(&models.Project{}).Where("id=?", f.project.ID).Updates(map[string]any{"status": "archived", "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if archived := readAIProjectOutputsForTest(t, f, ""); archived.ProjectStatus != "archived" || archived.Total != 2 {
		t.Fatal("archived project history lost")
	}
}

func TestAIProjectOutputsDeletedHistoryIsOnlyMetadata(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	accepted := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+f.task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if accepted.Code != http.StatusOK {
		t.Fatal(accepted.Body.String())
	}
	dismissed := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.inbox.ID+"/dismiss", []byte(`{"reason":"Followup not needed"}`), map[string]string{"If-Match": `"1"`})
	if dismissed.Code != http.StatusOK {
		t.Fatal(dismissed.Body.String())
	}
	task := getTaskForTaskFacts(t, f.router, f.task.ID)
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/artifacts/"+f.output.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"PRIVATE DELETE REASON"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)})
	if deleted.Code != http.StatusOK {
		t.Fatal(deleted.Body.String())
	}
	if active := readAIProjectOutputsForTest(t, f, ""); active.Total != 1 {
		t.Fatalf("deleted artifact visible: %+v", active)
	}
	if exact := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, f.output.Artifacts[0].ID)); exact.Total != 0 {
		t.Fatal("exact query bypassed deleted filter")
	}
	history := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"include_deleted":true,"artifact_id":%q,"followup_status":"dismissed"`, f.output.Artifacts[0].ID))
	if history.Total != 1 || len(history.Items) != 1 || history.Items[0].DeletedAt == nil || history.Items[0].Followup == nil || history.Items[0].Followup.SourceDeletedAt == nil {
		t.Fatalf("history=%+v", history)
	}
	encoded, _ := json.Marshal(history)
	for _, private := range []string{"PRIVATE DELETE REASON", "PRIVATE ARTIFACT BODY", "deleted_by_actor", "delete_reason", "relative_path"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("history leaked %s", private)
		}
	}
}

func TestAIProjectOutputsStrictQueryAndZeroWrites(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	base := fmt.Sprintf(`"project_id":%q`, f.project.ID)
	invalid := []string{`null`, `[]`, `{}`, `{` + base + `} {}`, `{` + base + `,"Project_ID":"x"}`, `{` + base + `,` + base + `}`, `{` + base + `,"project\u005fid":"x"}`, `{"project_id":"bad"}`, fmt.Sprintf(`{"project_id":%q}`, strings.ToUpper(f.project.ID))}
	for _, field := range []string{"project_id", "artifact_id", "followup_status", "include_deleted", "limit", "offset"} {
		prefix := base + ","
		if field == "project_id" {
			prefix = ""
		}
		invalid = append(invalid, `{`+prefix+fmt.Sprintf(`%q:null}`, field))
	}
	for _, extra := range []string{`"artifact_id":""`, `"artifact_id":"bad"`, `"followup_status":""`, `"followup_status":"pending"`, `"include_deleted":"true"`, `"include_deleted":0`, `"limit":0`, `"limit":21`, `"offset":-1`, `"offset":1001`, `"offset":1.5`, `"task_id":"bad"`, `"limit":1,"limit":2`} {
		invalid = append(invalid, `{`+base+`,`+extra+`}`)
	}
	var before int64
	if err := f.store.DB.Table("workflow_events").Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, args := range invalid {
		if result, err := f.tool.Execute(context.Background(), []byte(args)); err == nil || result != "" {
			t.Fatalf("accepted invalid query %s: %s %v", args, result, err)
		}
	}
	for _, extra := range []string{"", `,"offset":1000`, `,"followup_status":"none"`} {
		readAIProjectOutputsForTest(t, f, extra)
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM workflow_events", before)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions", 1)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_artifacts", 2)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items", 1)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAIProjectOutputsScopeProviderRestoreCancellation(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	args := []byte(fmt.Sprintf(`{"project_id":%q}`, f.project.ID))
	for _, scopes := range [][]string{nil, {"work"}, {"outputs"}, {"work", "actions"}, {"clients", "outputs"}} {
		f.tool.policy = harness.NewCapabilities(scopes...)
		if _, err := f.tool.Execute(context.Background(), args); !errors.Is(err, harness.ErrPermissionDenied) {
			t.Fatalf("scope %v: %v", scopes, err)
		}
	}
	f.tool.policy = harness.NewCapabilities("work", "outputs")
	f.tool.api.restorePending.Store(true)
	if _, err := f.tool.Execute(context.Background(), args); err == nil {
		t.Fatal("read during restore")
	}
	f.tool.api.restorePending.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.tool.Execute(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if err := f.store.DB.Model(&models.AIProvider{}).Where("id=?", f.tool.providerID).Updates(map[string]any{"config_version": gorm.Expr("config_version + 1"), "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.tool.Execute(context.Background(), args); err == nil {
		t.Fatal("stale provider config read")
	}
	f.tool.configVersion++
	readAIProjectOutputsForTest(t, f, "")
	if err := f.store.DB.Model(&models.AIProvider{}).Where("id=?", f.tool.providerID).Updates(map[string]any{"status": "disabled", "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.tool.Execute(context.Background(), args); err == nil {
		t.Fatal("disabled provider read")
	}
}

func TestAIProjectOutputsPagingWindowAndSQLBoundedMetadata(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	rows := make([]models.TaskArtifact, 1020)
	content := "NEVER LOAD PRIVATE OUTPUT"
	for i := range rows {
		rows[i] = models.TaskArtifact{ID: uuid.NewString(), TaskID: f.task.ID, SubmissionID: f.output.Submission.ID, Position: i + 3, StorageKind: "text", Name: strings.Repeat("界", 255), ContentText: &content, ProducedByActorID: models.BuiltinOwnerActorID, RecordedByActorID: models.BuiltinOwnerActorID, IntegrityStatus: "unverified", CreatedAt: "2026-09-19T00:00:00Z"}
	}
	if err := f.store.DB.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	page := readAIProjectOutputsForTest(t, f, `,"offset":1000,"limit":20`)
	if page.Total != 1022 || len(page.Items) != 20 || !page.HasMore || page.Next != nil || !page.WindowLimited {
		t.Fatalf("window=%+v", page)
	}
	exact := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"artifact_id":%q`, f.output.Artifacts[0].ID))
	if exact.Total != 1 || len(exact.Items) != 1 || exact.Items[0].ArtifactID != f.output.Artifacts[0].ID || exact.WindowLimited {
		t.Fatal("exact lookup was trapped behind the global paging window")
	}
	first := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"limit":1,"artifact_id":%q`, rows[0].ID))
	if utf8.RuneCountInString(first.Items[0].Label) != 200 {
		t.Fatal("SQL artifact name bound missing")
	}
}

func TestAIProjectOutputsEncodedResultBudgetFailsClosedAndCanNarrow(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	content := "PRIVATE OVERSIZED DATA"
	rows := make([]models.TaskArtifact, 20)
	for i := range rows {
		rows[i] = models.TaskArtifact{ID: uuid.NewString(), TaskID: f.task.ID, SubmissionID: f.output.Submission.ID, Position: i + 3, StorageKind: "text", Name: strings.Repeat("&", 255), ContentText: &content, ProducedByActorID: models.BuiltinOwnerActorID, RecordedByActorID: models.BuiltinOwnerActorID, IntegrityStatus: "unverified", CreatedAt: "2026-09-19T00:00:00Z"}
	}
	if err := f.store.DB.CreateInBatches(&rows, 20).Error; err != nil {
		t.Fatal(err)
	}
	args := []byte(fmt.Sprintf(`{"project_id":%q,"limit":20}`, f.project.ID))
	result, err := f.tool.Execute(context.Background(), args)
	if err == nil || result != "" || !strings.Contains(err.Error(), "result too large") || strings.Contains(err.Error(), content) {
		t.Fatalf("oversize result leaked/truncated: len=%d err=%v", len(result), err)
	}
	narrow := readAIProjectOutputsForTest(t, f, fmt.Sprintf(`,"limit":1,"artifact_id":%q`, rows[0].ID))
	if narrow.Total != 1 || len(narrow.Items) != 1 || narrow.Items[0].Label != strings.Repeat("&", 200) || narrow.HasMore {
		t.Fatalf("narrow=%+v", narrow)
	}
	native := performRequest(f.router, http.MethodGet, "/api/v1/projects/"+f.project.ID+"/artifacts?page_size=100", nil, nil)
	if native.Code != http.StatusOK || strings.Contains(native.Body.String(), content) {
		t.Fatalf("AI result cap changed native metadata read: %d", native.Code)
	}
	items, meta := decodeProjectArtifactList(t, native.Body.Bytes())
	if len(items) != 22 || meta.Total != 22 {
		t.Fatal("native artifacts were capped/truncated by AI limits")
	}
	for _, item := range items {
		if item.Artifact.ID == rows[0].ID && item.Artifact.Name != strings.Repeat("&", 255) {
			t.Fatal("native Artifact name was truncated by AI limits")
		}
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIProjectOutputsSourceIdentityInconsistencyFailsClosed(t *testing.T) {
	for _, mode := range []string{"wrong_key", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newAIProjectOutputsTestFixture(t)
			// Fault injection is confined to the query result of this temporary
			// database; do not disable durable source/history constraints.
			callback := "test:project_followup_corruption"
			if err := f.store.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				rows, ok := tx.Statement.Dest.(*[]models.InboxItem)
				if !ok || len(*rows) != 1 {
					return
				}
				if mode == "wrong_key" {
					invalid := "invalid-source-key"
					(*rows)[0].SourceEventKey = &invalid
				} else {
					duplicate := (*rows)[0]
					duplicate.ID = uuid.NewString()
					*rows = append(*rows, duplicate)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.store.DB.Callback().Query().Remove(callback) })
			args := []byte(fmt.Sprintf(`{"project_id":%q,"artifact_id":%q}`, f.project.ID, f.output.Artifacts[0].ID))
			if result, err := f.tool.Execute(context.Background(), args); err == nil || result != "" || strings.Contains(err.Error(), "invalid-source-key") {
				t.Fatalf("bad source accepted/leaked: %s %v", result, err)
			}
			native := performRequest(f.router, http.MethodGet, "/api/v1/projects/"+f.project.ID+"/artifacts", nil, nil)
			if native.Code != http.StatusInternalServerError {
				t.Fatalf("native source invariant changed: %d %s", native.Code, native.Body.String())
			}
		})
	}
}

func newAIProjectOutputsTestFixture(t *testing.T) aiProjectOutputsTestFixture {
	t.Helper()
	router, store, service, _, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Output bridge project"}`, nil)
	task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Delivery task","project_id":%q,"review_policy":"manual"}`, project.ID))
	createAssignmentForTest(t, router, task.ID, "assignee", models.BuiltinOwnerActorID, task.Version, "")
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, task.Version+1, "")
	response := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"PRIVATE SUMMARY","artifacts":[{"client_ref":"follow","storage_kind":"text","name":"Delivery followup","content_text":"PRIVATE ARTIFACT BODY","requires_followup":true},{"client_ref":"plain","storage_kind":"link","name":"Reference only","reference_url":"https://private.invalid/token","requires_followup":false}]}`), map[string]string{"If-Match": `"3"`})
	if response.Code != http.StatusCreated {
		t.Fatalf("fixture output=%d %s", response.Code, response.Body.String())
	}
	output := decodeSubmitOutputResponse(t, response.Body.Bytes())
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	var inbox models.InboxItem
	if err := store.DB.Where("source_entity_type='task_artifact' AND source_entity_id=?", output.Artifacts[0].ID).First(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	return aiProjectOutputsTestFixture{router: router, store: store, project: project, task: output.Task, output: output, inbox: inbox,
		tool: &aiWorkspaceTool{api: service, name: "workspace_project_outputs", policy: harness.NewCapabilities("work", "outputs"), providerID: provider.ID, configVersion: provider.ConfigVersion}}
}

func TestAIProjectOutputsProjectArtifactFollowupBridge(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	encoded, err := f.tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"project_id":%q}`, f.project.ID)))
	if err != nil {
		t.Fatalf("project outputs inaccessible: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(encoded), &body); err != nil {
		t.Fatal(err)
	}
	if body["project_id"] != f.project.ID || body["total_items"] != float64(2) {
		t.Fatalf("project bridge=%s", encoded)
	}
	var decoded struct {
		Items []aiProjectOutputItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 2 {
		t.Fatalf("items=%s", encoded)
	}
	for _, item := range decoded.Items {
		if item.ArtifactID == "" || item.SubmissionID != f.output.Submission.ID || item.Label == "" || item.StorageKind == "" || item.SubmissionSequence != 1 || item.SubmissionStatus != "pending_review" || item.CreatedAt == "" || item.Task.ID != f.task.ID || item.Task.Version != f.task.Version || item.Task.Status != "waiting_review" || item.Route != taskSubmissionRoute(f.task.ID, f.output.Submission.ID) {
			t.Fatalf("incomplete metadata=%s", encoded)
		}
		if item.ArtifactID == f.output.Artifacts[0].ID && (item.Followup == nil || item.Followup.InboxItemID != f.inbox.ID || !item.RequiresFollowup) {
			t.Fatalf("missing followup=%s", encoded)
		}
	}
	for _, forbidden := range []string{"PRIVATE SUMMARY", "PRIVATE ARTIFACT BODY", "private.invalid", "payload_json", "produced_by_actor", "recorded_by_actor", "relative_path", "sha256", "mime_type", "size_bytes", "delete_reason", "client_id"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("project metadata exposed %s: %s", forbidden, encoded)
		}
	}
}
