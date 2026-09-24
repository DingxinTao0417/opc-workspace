package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiOutputFileReadFixture struct {
	router     *Router
	store      *database.Store
	service    *API
	provider   models.AIProvider
	task       models.Task
	submission models.TaskSubmission
	artifact   models.TaskArtifact
}

func newAIOutputFileReadFixture(t *testing.T, name string, body []byte) aiOutputFileReadFixture {
	t.Helper()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	store, err := openAPITestDatabase(filepath.Join(root, "output-file-read.db"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(store.DB, Options{AppVersion: "test", Commit: "file-read", SchemaVersion: store.SchemaVersion, SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"}, Logger: log.New(io.Discard, "", 0), KeyStore: keystore.NewMemoryStore(), ArtifactDir: filepath.Join(root, "artifacts"), Now: func() time.Time { return now }, ReminderScanInterval: -1, FocusHeartbeatInterval: -1, AutomationDeliveryScanInterval: -1})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(); _ = store.Close() })
	task, _ := setupManualReviewTask(t, router.Engine)
	manifest, _ := json.Marshal(map[string]any{"summary": "File review fixture", "artifacts": []any{map[string]any{"client_ref": "file", "storage_kind": "file", "name": name, "file_field": "upload", "requires_followup": false}}})
	response := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", string(manifest), map[string][]byte{"upload": body}, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)})
	if response.Code != http.StatusCreated {
		t.Fatalf("submit file fixture = %d: %s", response.Code, response.Body.String())
	}
	submitted := decodeSubmitOutputResponse(t, response.Body.Bytes())
	var artifact models.TaskArtifact
	if err := store.DB.First(&artifact, "id=?", submitted.Artifacts[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	var submission models.TaskSubmission
	if err := store.DB.First(&submission, "id=?", submitted.Submission.ID).Error; err != nil {
		t.Fatal(err)
	}
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, artifactStore: router.artifactStore, options: Options{Now: func() time.Time { return now }}}
	return aiOutputFileReadFixture{router: router, store: store, service: service, provider: provider, task: submitted.Task, submission: submission, artifact: artifact}
}

func (f aiOutputFileReadFixture) args() map[string]any {
	return map[string]any{"task_id": f.task.ID, "task_version": f.task.Version, "submission_id": f.submission.ID, "artifact_id": f.artifact.ID, "expected_sha256": *f.artifact.SHA256}
}

func (f aiOutputFileReadFixture) reader(t *testing.T, scopes []string) (harness.Tool, bool) {
	t.Helper()
	registry, err := f.service.aiChatToolRegistry("ephemeral", false, &f.provider, &aiWorkspaceGrant{ProviderVersion: f.provider.Version, Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return registry.Get("workspace_read_artifact_file")
}

func TestAIOutputFileReadRequiresIndependentScopeAndAllowsTemporarySession(t *testing.T) {
	if aiProgressToolName("workspace_read_artifact_file") != "workspace_read_artifact_file" {
		t.Fatal("file reader omitted from safe execution progress")
	}
	f := newAIOutputFileReadFixture(t, "consent.txt", []byte("private file body"))
	for _, scopes := range [][]string{{"work"}, {"work", "outputs"}, {"work", "outputs", "actions"}} {
		if _, exists := f.reader(t, scopes); exists {
			t.Fatalf("file reader exposed to legacy scopes %v", scopes)
		}
	}
	grant := &aiWorkspaceGrant{ProviderVersion: f.provider.Version, Scopes: []string{"work", "outputs", "output_files"}}
	if err := validateAIWorkspaceGrant(&f.provider, grant); err != nil {
		t.Fatalf("read-only file scope should not require execution/actions: %v", err)
	}
	reader, exists := f.reader(t, grant.Scopes)
	if !exists {
		t.Fatal("explicit file reader missing in temporary session")
	}
	args, _ := json.Marshal(f.args())
	result, err := reader.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("temporary file read failed: %v, %s", err, result)
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs", 0)
	for _, scopes := range [][]string{{"output_files"}, {"work", "output_files"}, {"outputs", "output_files"}, {"work", "outputs", "output_files", "output_files"}} {
		if err := validateAIWorkspaceGrant(&f.provider, &aiWorkspaceGrant{ProviderVersion: f.provider.Version, Scopes: scopes}); err == nil {
			t.Fatalf("invalid dependent/duplicate scope accepted: %v", scopes)
		}
	}
}

func (f aiOutputFileReadFixture) requiredReader(t *testing.T) harness.Tool {
	t.Helper()
	reader, ok := f.reader(t, []string{"work", "outputs", "output_files"})
	if !ok {
		t.Fatal("explicit output_files reader missing")
	}
	return reader
}

type aiOutputFileTestPage struct {
	TaskID            string `json:"task_id"`
	TaskVersion       int64  `json:"task_version"`
	TaskStatus        string `json:"task_status"`
	SubmissionID      string `json:"submission_id"`
	SubmissionVersion *int64 `json:"submission_version"`
	SubmissionStatus  string `json:"submission_status"`
	IsCurrent         bool   `json:"is_current"`
	ArtifactID        string `json:"artifact_id"`
	Name              string `json:"name"`
	MIME              string `json:"mime_type"`
	Size              int64  `json:"size_bytes"`
	SHA256            string `json:"sha256"`
	Content           string `json:"content"`
	Length            int    `json:"content_length"`
	Offset            int    `json:"content_offset"`
	Limit             int    `json:"content_limit"`
	Next              *int   `json:"next_offset"`
	HasMore           bool   `json:"has_more"`
	IntegrityVerified bool   `json:"integrity_verified"`
	IntegrityScope    string `json:"integrity_scope"`
	ContentScope      string `json:"content_scope"`
	Route             string `json:"route"`
}

func TestAIOutputFileReadUnicodePagesAndReadOnlyEvidence(t *testing.T) {
	content := strings.Repeat("中文🙂abc", 1501)
	f := newAIOutputFileReadFixture(t, "unicode-review.md", []byte(content))
	reader := f.requiredReader(t)
	var beforeEvents int64
	if err := f.store.DB.Model(&models.WorkflowEvent{}).Count(&beforeEvents).Error; err != nil {
		t.Fatal(err)
	}
	// A derived stale observation is not authority: read actual bytes, but do
	// not silently write a newer integrity observation during an AI query.
	if err := f.store.DB.Model(&models.TaskArtifact{}).Where("id=?", f.artifact.ID).Updates(map[string]any{"integrity_status": "missing", "integrity_checked_at": "2026-09-20T12:00:00Z"}).Error; err != nil {
		t.Fatal(err)
	}
	var before models.TaskArtifact
	if err := f.store.DB.First(&before, "id=?", f.artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for offset := 0; ; {
		args := f.args()
		if offset > 0 {
			args["content_offset"] = offset
		}
		body, _ := json.Marshal(args)
		result, err := reader.Execute(context.Background(), body)
		if err != nil {
			t.Fatalf("page %d: %v", offset, err)
		}
		var page aiOutputFileTestPage
		if err := json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatal(err)
		}
		if page.TaskID != f.task.ID || page.TaskVersion != f.task.Version || page.TaskStatus != "waiting_review" || page.SubmissionID != f.submission.ID || page.SubmissionVersion != nil || page.SubmissionStatus != "pending_review" || !page.IsCurrent || page.ArtifactID != f.artifact.ID || page.Name != f.artifact.Name || page.Size != int64(len(content)) || page.SHA256 != *f.artifact.SHA256 || page.Length != utf8.RuneCountInString(content) || page.Offset != offset || page.Limit != 4000 || !page.IntegrityVerified || page.IntegrityScope != "entire_file" || page.ContentScope != "excerpt" || page.Route != taskSubmissionRoute(f.task.ID, f.submission.ID) {
			t.Fatalf("invalid file evidence: %s", result)
		}
		for _, private := range []string{*f.artifact.RelativePath, "relative_path", "input_snapshot", "provider_id", f.provider.ID, f.provider.BaseURL} {
			if private != "" && strings.Contains(result, private) {
				t.Fatalf("private field escaped read result: %s", private)
			}
		}
		all.WriteString(page.Content)
		if page.Next == nil {
			if page.HasMore {
				t.Fatal("missing next page while has_more")
			}
			break
		}
		if !page.HasMore || *page.Next != offset+utf8.RuneCountInString(page.Content) || *page.Next <= offset {
			t.Fatalf("nonadvancing/incorrect Unicode page: %s", result)
		}
		offset = *page.Next
	}
	if all.String() != content {
		t.Fatal("full Unicode file not recovered exactly")
	}
	var after models.TaskArtifact
	if err := f.store.DB.First(&after, "id=?", f.artifact.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read tool changed Artifact or integrity observation")
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM workflow_events", beforeEvents)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='waiting_review'", 1, f.task.ID, f.task.Version)
}

func TestAIOutputFileReadStrictArgumentsAndExactIdentity(t *testing.T) {
	f := newAIOutputFileReadFixture(t, "strict.txt", []byte("strict private bytes"))
	reader := f.requiredReader(t)
	base, _ := json.Marshal(f.args())
	cases := []struct{ name, raw string }{
		{"unknown", strings.TrimSuffix(string(base), "}") + `,"path":"C:/private.txt"}`},
		{"duplicate", strings.TrimSuffix(string(base), "}") + `,"task_id":"` + f.task.ID + `"}`},
		{"alias", strings.Replace(string(base), `"task_id"`, `"Task_ID"`, 1)},
		{"trailing", string(base) + ` {}`},
	}
	for _, field := range []string{"task_id", "task_version", "submission_id", "artifact_id", "expected_sha256"} {
		args := f.args()
		delete(args, field)
		raw, _ := json.Marshal(args)
		cases = append(cases, struct{ name, raw string }{"missing " + field, string(raw)})
		args = f.args()
		args[field] = nil
		raw, _ = json.Marshal(args)
		cases = append(cases, struct{ name, raw string }{"null " + field, string(raw)})
	}
	for key, values := range map[string][]any{"task_version": {0, -1, 1.5, "4", 9007199254740992.0}, "content_offset": {-1, 65537, 1.5, "0", nil}, "content_limit": {0, 4001, 1.5, "1", nil}, "expected_sha256": {"", strings.Repeat("A", 64), strings.Repeat("g", 64)}, "artifact_id": {"invalid", strings.ToUpper(f.artifact.ID)}} {
		for _, value := range values {
			args := f.args()
			args[key] = value
			raw, _ := json.Marshal(args)
			cases = append(cases, struct{ name, raw string }{fmt.Sprintf("%s=%v", key, value), string(raw)})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := reader.Execute(context.Background(), []byte(tc.raw))
			if err == nil || result != "" {
				t.Fatalf("invalid arguments accepted: %s result=%s err=%v", tc.raw, result, err)
			}
		})
	}
	for _, field := range []string{"task_id", "submission_id", "artifact_id", "expected_sha256", "task_version"} {
		t.Run("drift "+field, func(t *testing.T) {
			args := f.args()
			args[field] = uuid.NewString()
			if field == "expected_sha256" {
				args[field] = strings.Repeat("0", 64)
			}
			if field == "task_version" {
				args[field] = f.task.Version + 1
			}
			body, _ := json.Marshal(args)
			result, err := reader.Execute(context.Background(), body)
			if err == nil || result != "" {
				t.Fatalf("identity drift accepted %s", field)
			}
		})
	}
}

func TestAIOutputFileReadRejectsUnsafeOrCorruptFilesWithoutWrites(t *testing.T) {
	cases := []struct {
		name, filename string
		body           []byte
		mutate         func(*testing.T, aiOutputFileReadFixture)
	}{
		{"invalid UTF8", "bad.txt", []byte{0xff, 0xfe, 0xfd}, nil},
		{"NUL", "nul.txt", []byte{'a', 0, 'b'}, nil},
		{"binary extension", "data.png", []byte("text with binary extension"), nil},
		{"oversize", "large.txt", []byte(strings.Repeat("a", 65537)), nil},
		{"same size tamper", "changed.txt", []byte("original"), func(t *testing.T, f aiOutputFileReadFixture) {
			path, err := f.router.artifactStore.resolveObject(*f.artifact.RelativePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"size tamper", "size.txt", []byte("original"), func(t *testing.T, f aiOutputFileReadFixture) {
			path, err := f.router.artifactStore.resolveObject(*f.artifact.RelativePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("longer than original"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing", "missing.txt", []byte("missing file"), func(t *testing.T, f aiOutputFileReadFixture) {
			path, err := f.router.artifactStore.resolveObject(*f.artifact.RelativePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"storage unavailable", "store.txt", []byte("bounded bytes"), func(t *testing.T, f aiOutputFileReadFixture) { f.service.artifactStore = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAIOutputFileReadFixture(t, tc.filename, tc.body)
			reader := f.requiredReader(t)
			if tc.mutate != nil {
				tc.mutate(t, f)
			}
			var before models.TaskArtifact
			if err := f.store.DB.First(&before, "id=?", f.artifact.ID).Error; err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(f.args())
			result, err := reader.Execute(context.Background(), args)
			if err == nil || result != "" {
				t.Fatalf("invalid file read succeeded: %s %v", result, err)
			}
			if strings.Contains(err.Error(), *f.artifact.RelativePath) || strings.Contains(err.Error(), "original") {
				t.Fatalf("file/path leaked in error: %v", err)
			}
			var after models.TaskArtifact
			if err := f.store.DB.First(&after, "id=?", f.artifact.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed read changed Artifact integrity")
			}
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='waiting_review'", 1, f.task.ID, f.task.Version)
		})
	}
}

func TestAIOutputFileReadRevalidatesProviderTaskAndHistoricalDeletion(t *testing.T) {
	f := newAIOutputFileReadFixture(t, "review.txt", []byte("exact review content"))
	reader := f.requiredReader(t)
	args, _ := json.Marshal(f.args())
	result, err := reader.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var first aiOutputFileTestPage
	if json.Unmarshal([]byte(result), &first) != nil || first.ContentScope != "entire_file" || first.HasMore || first.Next != nil {
		t.Fatalf("short full-file contract: %s", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err = reader.Execute(ctx, args); err == nil || result != "" {
		t.Fatal("cancelled read leaked result")
	}
	f.service.restorePending.Store(true)
	if result, err = reader.Execute(context.Background(), args); err == nil || result != "" {
		t.Fatal("restore pending allowed read")
	}
	f.service.restorePending.Store(false)
	if err := f.store.DB.Model(&models.AIProvider{}).Where("id=?", f.provider.ID).Updates(map[string]any{"config_version": f.provider.ConfigVersion + 1, "version": f.provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if result, err = reader.Execute(context.Background(), args); err == nil || result != "" {
		t.Fatal("provider identity change retained file grant")
	}
	if err := f.store.DB.First(&f.provider, "id=?", f.provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	reader = f.requiredReader(t)
	review := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+f.task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if review.Code != http.StatusOK {
		t.Fatalf("native review = %d: %s", review.Code, review.Body.String())
	}
	if result, err = reader.Execute(context.Background(), args); err == nil || result != "" {
		t.Fatal("Task version drift accepted")
	}
	f.task = decodeReviewOutputResponse(t, review.Body.Bytes()).Task
	newArgs, _ := json.Marshal(f.args())
	result, err = reader.Execute(context.Background(), newArgs)
	if err != nil {
		t.Fatal(err)
	}
	var historical aiOutputFileTestPage
	if json.Unmarshal([]byte(result), &historical) != nil || historical.SubmissionStatus != "accepted" || historical.TaskStatus != "done" {
		t.Fatalf("accepted history not described honestly: %s", result)
	}
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/artifacts/"+f.artifact.ID+"?confirm=true", []byte(`{"reason":"native test deletion"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if deleted.Code != http.StatusOK && deleted.Code != http.StatusNoContent {
		t.Fatalf("native delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	if err := f.store.DB.First(&f.task, "id=?", f.task.ID).Error; err != nil {
		t.Fatal(err)
	}
	newArgs, _ = json.Marshal(f.args())
	if result, err = reader.Execute(context.Background(), newArgs); err == nil || result != "" {
		t.Fatal("soft-deleted file still readable")
	}
}

func TestAIOutputFileReadHistoricalBatchAndExistingIdentityMismatch(t *testing.T) {
	f := newAIOutputFileReadFixture(t, "first.md", []byte("Original file remains historical evidence."))
	reader := f.requiredReader(t)
	review := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+f.task.ID+"/review", []byte(`{"decision":"request_changes","reason":"Provide a revised delivery"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if review.Code != http.StatusOK {
		t.Fatalf("request changes = %d: %s", review.Code, review.Body.String())
	}
	f.task = decodeReviewOutputResponse(t, review.Body.Bytes()).Task
	revised := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+f.task.ID+"/submit-output", []byte(`{"summary":"Latest revision","artifacts":[{"client_ref":"text","storage_kind":"text","name":"Current description","content_text":"not a file"}]}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if revised.Code != http.StatusCreated {
		t.Fatalf("revised native output = %d: %s", revised.Code, revised.Body.String())
	}
	latest := decodeSubmitOutputResponse(t, revised.Body.Bytes())
	f.task = latest.Task
	args, _ := json.Marshal(f.args())
	result, err := reader.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var historical aiOutputFileTestPage
	if json.Unmarshal([]byte(result), &historical) != nil || historical.IsCurrent || historical.SubmissionStatus != "changes_requested" || historical.TaskStatus != "waiting_review" || historical.Content != "Original file remains historical evidence." || historical.Route != taskSubmissionRoute(f.task.ID, f.submission.ID) {
		t.Fatalf("historical file confused with current batch: %s", result)
	}
	otherTask, _ := setupManualReviewTask(t, f.router.Engine)
	otherResponse := performRequest(f.router, http.MethodPost, "/api/v1/tasks/"+otherTask.ID+"/submit-output", []byte(`{"summary":"Unrelated real batch","artifacts":[]}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, otherTask.Version)})
	if otherResponse.Code != http.StatusCreated {
		t.Fatal(otherResponse.Body.String())
	}
	other := decodeSubmitOutputResponse(t, otherResponse.Body.Bytes())
	for name, changes := range map[string]map[string]any{
		"existing-other-owner":       {"task_id": other.Task.ID, "task_version": other.Task.Version},
		"existing-other-submission":  {"submission_id": other.Submission.ID},
		"existing-later-batch":       {"submission_id": latest.Submission.ID},
		"existing-non-file-artifact": {"submission_id": latest.Submission.ID, "artifact_id": latest.Artifacts[0].ID},
	} {
		t.Run(name, func(t *testing.T) {
			input := f.args()
			for key, value := range changes {
				input[key] = value
			}
			args, _ := json.Marshal(input)
			if result, err := reader.Execute(context.Background(), args); err == nil || result != "" || !strings.HasPrefix(err.Error(), "ARTIFACT_FILE_UNAVAILABLE:") {
				t.Fatalf("existing identity mismatch exposed data: %s %v", result, err)
			}
		})
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested'", 1, f.submission.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, latest.Submission.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIOutputFileReadEscapedBudgetAnd64KiBBoundary(t *testing.T) {
	f := newAIOutputFileReadFixture(t, "escaped.html", []byte(strings.Repeat("<", 4099)+"中文🙂"))
	reader := f.requiredReader(t)
	args, _ := json.Marshal(f.args())
	if result, err := reader.Execute(context.Background(), args); err == nil || result != "" || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("encoded budget should reject rather than truncate: %s %v", result, err)
	}
	var all strings.Builder
	for offset := 0; ; {
		args := f.args()
		args["content_limit"] = 1000
		args["content_offset"] = offset
		body, _ := json.Marshal(args)
		result, err := reader.Execute(context.Background(), body)
		if err != nil {
			t.Fatal(err)
		}
		var page aiOutputFileTestPage
		if json.Unmarshal([]byte(result), &page) != nil {
			t.Fatal(result)
		}
		all.WriteString(page.Content)
		if page.Next == nil {
			break
		}
		if *page.Next <= offset {
			t.Fatal("page did not advance")
		}
		offset = *page.Next
	}
	if all.String() != strings.Repeat("<", 4099)+"中文🙂" {
		t.Fatal("escaped Unicode content incomplete")
	}
	boundary := newAIOutputFileReadFixture(t, "boundary.txt", []byte(strings.Repeat("x", 64<<10)))
	reader = boundary.requiredReader(t)
	args, _ = json.Marshal(boundary.args())
	result, err := reader.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var page aiOutputFileTestPage
	if json.Unmarshal([]byte(result), &page) != nil || page.Size != 64<<10 || page.Length != 64<<10 || !page.HasMore {
		t.Fatalf("exact 64KiB file rejected or truncated: %s", result)
	}
}

func TestAIOutputFileReadHarnessNativeEvidenceAndHumanReview(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			const fileBody = "FILE_EVIDENCE_ONLY: 已核对三个交付项，最后一项待人工判断。"
			f := newAIOutputFileReadFixture(t, "deliverable.md", []byte(fileBody))
			var taskVersion int64
			var submissionID, artifactID, expectedSHA string
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if strings.Contains(string(raw), *f.artifact.RelativePath) || strings.Contains(string(raw), "relative_path") {
					t.Error("controlled storage identity leaked")
				}
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_read_artifact_file") {
						t.Error("file reader should be deferred until outputs guide")
					}
					writeAIBudgetToolTurn(w, protocol, "file-guide", "workspace_guide", `{"topic":"outputs"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_read_artifact_file") {
						t.Error("outputs guide failed to discover explicitly granted reader")
					}
					args, _ := json.Marshal(map[string]any{"task_id": f.task.ID, "view": "list"})
					writeAIBudgetToolTurn(w, protocol, "file-batches", "workspace_task_submissions", string(args))
				case 3:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "file-batches", "workspace_task_submissions")
					var page struct {
						TaskVersion int64   `json:"task_version"`
						Current     *string `json:"current_submission_id"`
						Items       []struct {
							ID     string `json:"id"`
							Status string `json:"status"`
						} `json:"items"`
					}
					if json.Unmarshal([]byte(content), &page) != nil || page.Current == nil || len(page.Items) != 1 || page.Items[0].ID != *page.Current || page.Items[0].Status != "pending_review" {
						t.Errorf("actual pending batch not discovered: %s", content)
						writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
						return
					}
					taskVersion, submissionID = page.TaskVersion, *page.Current
					args, _ := json.Marshal(map[string]any{"task_id": f.task.ID, "view": "artifacts", "submission_id": submissionID})
					writeAIBudgetToolTurn(w, protocol, "file-metadata", "workspace_task_submissions", string(args))
				case 4:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "file-metadata", "workspace_task_submissions")
					var page struct {
						Items []struct {
							ID   string `json:"id"`
							SHA  string `json:"sha256"`
							Kind string `json:"storage_kind"`
						} `json:"items"`
					}
					if json.Unmarshal([]byte(content), &page) != nil || len(page.Items) != 1 || page.Items[0].Kind != "file" || strings.Contains(content, fileBody) {
						t.Errorf("actual file metadata not discovered: %s", content)
						writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
						return
					}
					artifactID, expectedSHA = page.Items[0].ID, page.Items[0].SHA
					args, _ := json.Marshal(map[string]any{"task_id": f.task.ID, "task_version": taskVersion, "submission_id": submissionID, "artifact_id": artifactID, "expected_sha256": expectedSHA})
					writeAIBudgetToolTurn(w, protocol, "file-read", "workspace_read_artifact_file", string(args))
				case 5:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "file-read", "workspace_read_artifact_file")
					var page aiOutputFileTestPage
					if json.Unmarshal([]byte(content), &page) != nil || page.Content != fileBody || page.SHA256 != expectedSHA || page.ArtifactID != artifactID || page.SubmissionID != submissionID || !page.IntegrityVerified || page.HasMore || page.Next != nil || page.ContentScope != "entire_file" {
						t.Errorf("actual verified file body missing: %s", content)
					}
					args := taskOutputActionArgs("task.review", models.Task{ID: f.task.ID, Version: taskVersion}, map[string]any{"submission_id": submissionID, "decision": "accept", "reason": "文件已读取，仍请你独立核验并确认验收"})
					writeAIBudgetToolTurn(w, protocol, "file-review-proposal", "workspace_propose", args)
				case 6:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "file-review-proposal", "workspace_propose")
					if !strings.Contains(content, "NOT executed") {
						t.Errorf("proposal falsely presented execution: %s", content)
					}
					writeAIBudgetTextTurn(w, protocol, `已读取文件并提出验收建议，等待你核验确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 7:
					if aiBudgetHasTool(payload, "workspace_read_artifact_file") || aiBudgetHasTool(payload, "workspace_propose") || strings.Contains(string(raw), fileBody) {
						t.Error("file permission or raw tool bytes carried into next ungranted message")
					}
					writeAIBudgetTextTurn(w, protocol, `已记录你的验收决定；本条没有文件读取权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected provider call %d", call)
					writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, f.router, f.store, upstream, protocol)
			request, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "读取该任务当前提交的文件，然后提出验收建议；由我独立确认。任务ID：" + f.task.ID, "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "output_files", "actions"}}})
			response := performRequest(f.router, http.MethodPost, "/api/v1/ai/chat", request, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 6 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("file review Harness = %d calls=%d: %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='waiting_review'", 1, f.task.ID, f.task.Version)
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, f.submission.ID)
			var proposal models.AIActionProposal
			if err := f.store.DB.Where("status='pending'").Take(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			if strings.Contains(proposal.PreviewJSON, fileBody) || strings.Contains(proposal.PreviewJSON, *f.artifact.RelativePath) {
				t.Fatal("review preview silently expanded to raw file evidence")
			}
			decisionPath := "/api/v1/ai/actions/" + proposal.ID + "/decision"
			withoutConsent := performRequest(f.router, http.MethodPost, decisionPath, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
			if withoutConsent.Code != http.StatusUnprocessableEntity {
				t.Fatalf("missing human file-review consent accepted: %d %s", withoutConsent.Code, withoutConsent.Body.String())
			}
			confirmed := performRequest(f.router, http.MethodPost, decisionPath, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_output":true}`, proposal.Fingerprint)), nil)
			if confirmed.Code != http.StatusOK {
				t.Fatalf("human review = %d: %s", confirmed.Code, confirmed.Body.String())
			}
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='done'", 1, f.task.ID)
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='accepted'", 1, f.submission.ID)
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM ai_messages WHERE context_snapshot LIKE ?", 0, "%FILE_EVIDENCE_ONLY%")
			var session models.AISession
			if err := f.store.DB.Where("persist=1").Take(&session).Error; err != nil {
				t.Fatal(err)
			}
			next, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "谢谢，请说明刚才的确认结果"})
			response = performRequest(f.router, http.MethodPost, "/api/v1/ai/chat", next, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 7 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("ungranted followup = %d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
		})
	}
}
