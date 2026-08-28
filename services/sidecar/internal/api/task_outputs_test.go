package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const testArtifactDatabaseID = "018f0000-0000-7000-8000-000000000999"

func newTaskOutputTestAPI(t *testing.T) (*gin.Engine, *database.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "output.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	artifactDir := filepath.Join(root, "artifacts")
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactDir,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		if err := router.Close(); err != nil {
			t.Errorf("Router.Close() error = %v", err)
		}
	})
	return router.Engine, store, artifactDir
}

func setupManualReviewTask(t *testing.T, router *gin.Engine) (models.Task, actorResponse) {
	t.Helper()
	task := createTaskForTaskFacts(t, router, `{"title":"Manual output Task","review_policy":"manual"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Offline producer"}`, nil)
	createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 1, "")
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 2, "")
	task.Version = 3
	return task, person
}

func decodeSubmitOutputResponse(t *testing.T, body []byte) submitOutputResponse {
	t.Helper()
	var envelope struct {
		Data submitOutputResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode submit response: %v: %s", err, body)
	}
	return envelope.Data
}

func decodeReviewOutputResponse(t *testing.T, body []byte) reviewTaskOutputResponse {
	t.Helper()
	var envelope struct {
		Data reviewTaskOutputResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode review response: %v: %s", err, body)
	}
	return envelope.Data
}

func performMultipartRequest(
	router http.Handler,
	path string,
	manifest string,
	files map[string][]byte,
	headers map[string]string,
) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("manifest", manifest)
	for field, content := range files {
		part, _ := writer.CreateFormFile(field, field+".bin")
		_, _ = part.Write(content)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

type multipartTestPart struct {
	field    string
	filename string
	content  []byte
}

func performMultipartPartsRequest(
	router http.Handler,
	path string,
	parts []multipartTestPart,
	headers map[string]string,
	chunked bool,
) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, testPart := range parts {
		if testPart.filename == "" {
			_ = writer.WriteField(testPart.field, string(testPart.content))
			continue
		}
		part, _ := writer.CreateFormFile(testPart.field, testPart.filename)
		_, _ = part.Write(testPart.content)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body.Bytes()))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if chunked {
		request.ContentLength = -1
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestTaskOutputJSONSubmitReviewAndIdempotency(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, producer := setupManualReviewTask(t, router)
	body := `{
		"summary":"First delivery",
		"artifacts":[
			{"client_ref":"note","storage_kind":"text","name":"Notes","content_text":"private text body"},
			{"client_ref":"link","storage_kind":"link","name":"Reference","reference_url":"https://example.com/result"},
			{"client_ref":"data","storage_kind":"structured","name":"Metrics","structured_json":{"score":9,"ready":true}}
		]
	}`
	headers := map[string]string{"If-Match": `"3"`, "Idempotency-Key": "submit-json-1", "X-Request-ID": "f68d4226-4d20-4c2a-99e6-b0397987bf89"}
	created := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(body), headers)
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"4"` {
		t.Fatalf("submit output = %d headers=%v: %s", created.Code, created.Header(), created.Body.String())
	}
	response := decodeSubmitOutputResponse(t, created.Body.Bytes())
	if response.Task.Status != "waiting_review" || response.Task.CurrentSubmissionID == nil ||
		*response.Task.CurrentSubmissionID != response.Submission.ID || response.Submission.Status != "pending_review" ||
		response.Submission.SubmittedByActorID != models.BuiltinOwnerActorID || len(response.Artifacts) != 3 {
		t.Fatalf("submit response = %#v", response)
	}
	for _, artifact := range response.Artifacts {
		if artifact.SubmissionStatus != "pending_review" || artifact.ProducedByActorID != producer.ID || artifact.RecordedByActorID != models.BuiltinOwnerActorID {
			t.Fatalf("Artifact actor attribution = %#v", artifact)
		}
	}
	if strings.Contains(created.Body.String(), "private text body") || strings.Contains(created.Body.String(), "https://example.com/result") {
		t.Fatalf("submit snapshot leaked Artifact payload: %s", created.Body.String())
	}

	replay := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(body), headers)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || replay.Body.String() != created.Body.String() {
		t.Fatalf("submit replay = %d headers=%v: %s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(strings.Replace(body, "First delivery", "Changed delivery", 1)), headers)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("submit idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
	var submissionCount, artifactCount, eventCount int64
	_ = store.DB.Model(&models.TaskSubmission{}).Where("task_id = ?", task.ID).Count(&submissionCount).Error
	_ = store.DB.Model(&models.TaskArtifact{}).Where("task_id = ?", task.ID).Count(&artifactCount).Error
	_ = store.DB.Model(&models.WorkflowEvent{}).Where("aggregate_id = ? AND action = 'task_output_submitted'", task.ID).Count(&eventCount).Error
	if submissionCount != 1 || artifactCount != 3 || eventCount != 1 {
		t.Fatalf("idempotent counts submissions=%d artifacts=%d events=%d", submissionCount, artifactCount, eventCount)
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/artifacts/"+response.Artifacts[2].ID, nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"submission_status":"pending_review"`) ||
		!strings.Contains(detail.Body.String(), `"structured_json":{"ready":true,"score":9}`) {
		t.Fatalf("structured Artifact detail = %d: %s", detail.Code, detail.Body.String())
	}
	list := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/submissions", nil, nil)
	if list.Code != http.StatusOK || list.Header().Get("ETag") != `"4"` || !strings.Contains(list.Body.String(), `"task_version":4`) {
		t.Fatalf("submission list = %d headers=%v: %s", list.Code, list.Header(), list.Body.String())
	}
	artifactList := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/artifacts", nil, nil)
	if artifactList.Code != http.StatusOK || !strings.Contains(artifactList.Body.String(), `"submission_status":"pending_review"`) {
		t.Fatalf("Artifact list = %d: %s", artifactList.Code, artifactList.Body.String())
	}
	events := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/events?page_size=100", nil, nil)
	if strings.Contains(events.Body.String(), "private text body") || strings.Contains(events.Body.String(), "https://example.com/result") {
		t.Fatalf("Workflow Event leaked Artifact payload: %s", events.Body.String())
	}

	changesBody := `{"decision":"request_changes","reason":"Please revise"}`
	changesHeaders := map[string]string{"If-Match": `"4"`, "Idempotency-Key": "review-changes-1"}
	changes := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(changesBody), changesHeaders)
	if changes.Code != http.StatusOK || changes.Header().Get("ETag") != `"5"` {
		t.Fatalf("request changes = %d: %s", changes.Code, changes.Body.String())
	}
	changed := decodeReviewOutputResponse(t, changes.Body.Bytes())
	if changed.Task.Status != "in_progress" || changed.Task.CurrentSubmissionID == nil || changed.Submission.Status != "changes_requested" ||
		changed.Submission.ReviewReason == nil || *changed.Submission.ReviewReason != "Please revise" {
		t.Fatalf("changes response = %#v", changed)
	}
	for _, artifact := range changed.Submission.Artifacts {
		if artifact.SubmissionStatus != "changes_requested" {
			t.Fatalf("reviewed Artifact submission status = %#v", artifact)
		}
	}
	changesReplay := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(changesBody), changesHeaders)
	if changesReplay.Header().Get("Idempotency-Replayed") != "true" || changesReplay.Body.String() != changes.Body.String() {
		t.Fatalf("review replay = %d headers=%v: %s", changesReplay.Code, changesReplay.Header(), changesReplay.Body.String())
	}

	second := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"Revised delivery","artifacts":[]}`), map[string]string{"If-Match": `"5"`})
	if second.Code != http.StatusCreated {
		t.Fatalf("second submission = %d: %s", second.Code, second.Body.String())
	}
	secondResponse := decodeSubmitOutputResponse(t, second.Body.Bytes())
	accepted := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": `"6"`})
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept = %d: %s", accepted.Code, accepted.Body.String())
	}
	acceptedResponse := decodeReviewOutputResponse(t, accepted.Body.Bytes())
	if acceptedResponse.Task.Status != "done" || acceptedResponse.Task.Version != 7 ||
		acceptedResponse.Task.CurrentSubmissionID == nil || *acceptedResponse.Task.CurrentSubmissionID != secondResponse.Submission.ID ||
		acceptedResponse.Submission.Status != "accepted" || acceptedResponse.Event.CommandSeq == nil || *acceptedResponse.Event.CommandSeq != 3 {
		t.Fatalf("accepted response = %#v", acceptedResponse)
	}
	reopened := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reopen", []byte(`{}`), map[string]string{"If-Match": `"7"`})
	if reopened.Code != http.StatusOK || decodeTaskLifecycleResponse(t, reopened.Body.Bytes()).Task.CurrentSubmissionID != nil {
		t.Fatalf("reopen clears current submission = %d: %s", reopened.Code, reopened.Body.String())
	}
}

func TestTaskOutputMultipartDownloadDeleteAndCompensation(t *testing.T) {
	router, store, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"File delivery","artifacts":[{"client_ref":"upload-1","storage_kind":"file","name":"report.txt","file_field":"report","requires_followup":true}]}`
	created := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"report": []byte("controlled file body")}, map[string]string{"If-Match": `"3"`, "Idempotency-Key": "multipart-submit-1"})
	if created.Code != http.StatusCreated {
		t.Fatalf("multipart submit = %d: %s", created.Code, created.Body.String())
	}
	response := decodeSubmitOutputResponse(t, created.Body.Bytes())
	artifact := response.Artifacts[0]
	if artifact.SubmissionStatus != "pending_review" || artifact.SizeBytes == nil || *artifact.SizeBytes != int64(len("controlled file body")) || artifact.SHA256 == nil ||
		artifact.MimeType == nil || artifact.IntegrityStatus != "verified" {
		t.Fatalf("file metadata = %#v", artifact)
	}
	var relativePath string
	if err := store.SQL.QueryRow("SELECT relative_path FROM task_artifacts WHERE id = ?", artifact.ID).Scan(&relativePath); err != nil {
		t.Fatalf("read relative path: %v", err)
	}
	if filepath.IsAbs(relativePath) || strings.Contains(relativePath, "..") {
		t.Fatalf("unsafe stored relative path = %q", relativePath)
	}
	livePath := filepath.Join(artifactDir, filepath.FromSlash(relativePath))
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("controlled file missing: %v", err)
	}
	// A COMMIT error is not proof that SQLite rolled back. Rechecking an active
	// row must preserve its object instead of applying destructive compensation.
	(&API{db: store.DB, artifactStore: &artifactStore{
		root: artifactDir, objectsDir: filepath.Join(artifactDir, "objects"),
	}}).compensateSubmittedArtifactFiles(task.ID, []committedArtifactFile{{
		artifactID: artifact.ID, relativePath: relativePath,
	}})
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("ambiguous commit compensation removed active file: %v", err)
	}
	download := performRequest(router, http.MethodGet, "/api/v1/artifacts/"+artifact.ID+"/content", nil, nil)
	if download.Code != http.StatusOK || download.Body.String() != "controlled file body" ||
		download.Header().Get("X-Content-Type-Options") != "nosniff" || download.Header().Get("Cache-Control") != "no-store" ||
		download.Header().Get("ETag") != `"`+*artifact.SHA256+`"` || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download = %d headers=%v body=%q", download.Code, download.Header(), download.Body.String())
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_artifact_integrity_update
		BEFORE UPDATE OF integrity_status, integrity_checked_at ON task_artifacts
		WHEN NEW.id = '` + artifact.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_INTEGRITY_UPDATE_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install integrity failure trigger: %v", err)
	}
	failedIntegrityPersist := performRequest(router, http.MethodGet, "/api/v1/artifacts/"+artifact.ID+"/content", nil, nil)
	if failedIntegrityPersist.Code != http.StatusInternalServerError || responseErrorCode(t, failedIntegrityPersist.Body.Bytes()) != "INTERNAL_ERROR" {
		t.Fatalf("download with failed integrity persistence = %d: %s", failedIntegrityPersist.Code, failedIntegrityPersist.Body.String())
	}
	if err := store.DB.Exec("DROP TRIGGER fail_artifact_integrity_update").Error; err != nil {
		t.Fatalf("drop integrity failure trigger: %v", err)
	}
	pendingDelete := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"premature"}`), map[string]string{"If-Match": `"4"`})
	if pendingDelete.Code != http.StatusConflict || responseErrorCode(t, pendingDelete.Body.Bytes()) != "ARTIFACT_PENDING_REVIEW" {
		t.Fatalf("pending delete = %d: %s", pendingDelete.Code, pendingDelete.Body.String())
	}
	changes := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"Replace file"}`), map[string]string{"If-Match": `"4"`})
	if changes.Code != http.StatusOK {
		t.Fatalf("request changes = %d: %s", changes.Code, changes.Body.String())
	}

	if err := store.DB.Exec(`CREATE TRIGGER fail_artifact_delete_event BEFORE INSERT ON workflow_events WHEN NEW.action = 'task_artifact_deleted' BEGIN SELECT RAISE(ABORT, 'TEST_EVENT_FAILURE'); END`).Error; err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	failedDelete := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"failed delete"}`), map[string]string{"If-Match": `"5"`})
	if failedDelete.Code != http.StatusInternalServerError {
		t.Fatalf("failed delete = %d: %s", failedDelete.Code, failedDelete.Body.String())
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("failed delete did not restore file: %v", err)
	}
	var deletedAt *string
	if err := store.SQL.QueryRow("SELECT deleted_at FROM task_artifacts WHERE id = ?", artifact.ID).Scan(&deletedAt); err != nil || deletedAt != nil {
		t.Fatalf("failed delete changed DB deleted_at=%v err=%v", deletedAt, err)
	}
	var tombstones int64
	if err := store.DB.Model(&models.ArtifactDeletionTombstone{}).Where("artifact_id = ?", artifact.ID).Count(&tombstones).Error; err != nil || tombstones != 0 {
		t.Fatalf("failed delete retained tombstone count=%d err=%v", tombstones, err)
	}
	if err := store.DB.Exec("DROP TRIGGER fail_artifact_delete_event").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}

	deleteHeaders := map[string]string{"If-Match": `"5"`, "Idempotency-Key": "delete-artifact-1"}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"Superseded"}`), deleteHeaders)
	if deleted.Code != http.StatusOK || deleted.Header().Get("ETag") != `"6"` {
		t.Fatalf("delete = %d headers=%v: %s", deleted.Code, deleted.Header(), deleted.Body.String())
	}
	if !strings.Contains(deleted.Body.String(), `"submission_status":"changes_requested"`) {
		t.Fatalf("delete response omitted authoritative submission status: %s", deleted.Body.String())
	}
	if _, err := os.Stat(livePath); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
	var tombstone models.ArtifactDeletionTombstone
	if err := store.DB.First(&tombstone, "artifact_id = ?", artifact.ID).Error; err != nil ||
		tombstone.TaskID != task.ID || tombstone.RelativePath != relativePath || tombstone.DeletionScope != "artifact" ||
		tombstone.SizeBytes != *artifact.SizeBytes || tombstone.SHA256 != *artifact.SHA256 {
		t.Fatalf("soft delete tombstone=%#v err=%v", tombstone, err)
	}
	deleteReplay := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"Superseded"}`), deleteHeaders)
	if deleteReplay.Code != http.StatusOK || deleteReplay.Header().Get("Idempotency-Replayed") != "true" || deleteReplay.Body.String() != deleted.Body.String() {
		t.Fatalf("delete replay = %d headers=%v: %s", deleteReplay.Code, deleteReplay.Header(), deleteReplay.Body.String())
	}
	deleteConflict := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"Different"}`), deleteHeaders)
	if deleteConflict.Code != http.StatusConflict || responseErrorCode(t, deleteConflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("delete idempotency conflict = %d: %s", deleteConflict.Code, deleteConflict.Body.String())
	}
	gone := performRequest(router, http.MethodGet, "/api/v1/artifacts/"+artifact.ID+"/content", nil, nil)
	if gone.Code != http.StatusGone || responseErrorCode(t, gone.Body.Bytes()) != "ARTIFACT_DELETED" {
		t.Fatalf("deleted content = %d: %s", gone.Code, gone.Body.String())
	}
}

func TestTaskOutputFileCommitFailureRemovesDatabaseAndObject(t *testing.T) {
	router, store, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	if err := store.DB.Exec(`CREATE TRIGGER fail_submit_event BEFORE INSERT ON workflow_events WHEN NEW.action = 'task_output_submitted' BEGIN SELECT RAISE(ABORT, 'TEST_SUBMIT_FAILURE'); END`).Error; err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	manifest := `{"summary":"Will fail","artifacts":[{"client_ref":"file","storage_kind":"file","name":"failure.bin","file_field":"payload"}]}`
	failed := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"payload": []byte("must be compensated")}, map[string]string{"If-Match": `"3"`})
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed submit = %d: %s", failed.Code, failed.Body.String())
	}
	var submissions, artifacts int64
	_ = store.DB.Model(&models.TaskSubmission{}).Where("task_id = ?", task.ID).Count(&submissions).Error
	_ = store.DB.Model(&models.TaskArtifact{}).Where("task_id = ?", task.ID).Count(&artifacts).Error
	if submissions != 0 || artifacts != 0 {
		t.Fatalf("failed submit left DB rows submissions=%d artifacts=%d", submissions, artifacts)
	}
	entries, err := os.ReadDir(filepath.Join(artifactDir, "objects"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed submit left object entries=%v err=%v", entries, err)
	}
	staging, err := os.ReadDir(filepath.Join(artifactDir, ".staging"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("failed submit left staging entries=%v err=%v", staging, err)
	}
}

func TestSubmitOutputMultipartRequiresManifestFirstAndStrictFileParts(t *testing.T) {
	router, store, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	path := "/api/v1/tasks/" + task.ID + "/submit-output"
	manifest := []byte(`{"summary":"Strict parts","artifacts":[{"client_ref":"file","storage_kind":"file","name":"payload.bin","file_field":"payload"}]}`)
	testCases := []struct {
		name  string
		parts []multipartTestPart
	}{
		{
			name: "file before manifest",
			parts: []multipartTestPart{
				{field: "payload", filename: "payload.bin", content: []byte("payload")},
				{field: "manifest", content: manifest},
			},
		},
		{
			name: "unknown file after staged file",
			parts: []multipartTestPart{
				{field: "manifest", content: manifest},
				{field: "payload", filename: "payload.bin", content: []byte("payload")},
				{field: "other", filename: "other.bin", content: []byte("other")},
			},
		},
		{
			name: "duplicate file field",
			parts: []multipartTestPart{
				{field: "manifest", content: manifest},
				{field: "payload", filename: "payload.bin", content: []byte("payload")},
				{field: "payload", filename: "duplicate.bin", content: []byte("duplicate")},
			},
		},
		{
			name:  "missing file field",
			parts: []multipartTestPart{{field: "manifest", content: manifest}},
		},
		{
			name: "duplicate manifest field",
			parts: []multipartTestPart{
				{field: "manifest", content: manifest},
				{field: "manifest", content: manifest},
			},
		},
		{
			name: "unknown text field",
			parts: []multipartTestPart{
				{field: "manifest", content: manifest},
				{field: "note", content: []byte("not allowed")},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := performMultipartPartsRequest(router, path, testCase.parts, map[string]string{"If-Match": `"3"`}, false)
			if response.Code != http.StatusBadRequest || responseErrorCode(t, response.Body.Bytes()) != "INVALID_MULTIPART" {
				t.Fatalf("strict multipart response = %d: %s", response.Code, response.Body.String())
			}
			entries, err := os.ReadDir(filepath.Join(artifactDir, ".staging"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejected multipart left staging entries=%v err=%v", entries, err)
			}
		})
	}
	var submissions, artifacts int64
	if err := store.DB.Model(&models.TaskSubmission{}).Where("task_id = ?", task.ID).Count(&submissions).Error; err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if err := store.DB.Model(&models.TaskArtifact{}).Where("task_id = ?", task.ID).Count(&artifacts).Error; err != nil {
		t.Fatalf("count Artifacts: %v", err)
	}
	if submissions != 0 || artifacts != 0 {
		t.Fatalf("rejected multipart persisted rows submissions=%d artifacts=%d", submissions, artifacts)
	}
}

func TestSubmitOutputMultipartStreamsChunkedFilesWithoutSystemTemp(t *testing.T) {
	router, _, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	blockedTemp := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedTemp, []byte("block system temp"), 0o600); err != nil {
		t.Fatalf("create blocked temp path: %v", err)
	}
	t.Setenv("TMPDIR", blockedTemp)
	t.Setenv("TMP", blockedTemp)
	t.Setenv("TEMP", blockedTemp)
	manifest := []byte(`{"summary":"Stream directly","artifacts":[{"client_ref":"file","storage_kind":"file","name":"payload.bin","file_field":"payload"}]}`)
	response := performMultipartPartsRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		[]multipartTestPart{
			{field: "manifest", content: manifest},
			{field: "payload", filename: "payload.bin", content: bytes.Repeat([]byte("x"), 2<<20)},
		},
		map[string]string{"If-Match": `"3"`},
		true,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("chunked streaming multipart = %d: %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(artifactDir, ".staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("successful streaming submit left staging entries=%v err=%v", entries, err)
	}
}

func TestSubmitOutputMultipartCleansStagingAfterChunkedTruncation(t *testing.T) {
	router, _, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"Truncated stream","artifacts":[{"client_ref":"first","storage_kind":"file","name":"first.bin","file_field":"first"},{"client_ref":"second","storage_kind":"file","name":"second.bin","file_field":"second"}]}`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("manifest", manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	first, err := writer.CreateFormFile("first", "first.bin")
	if err != nil {
		t.Fatalf("create first file part: %v", err)
	}
	if _, err := first.Write([]byte("first file")); err != nil {
		t.Fatalf("write first file: %v", err)
	}
	second, err := writer.CreateFormFile("second", "second.bin")
	if err != nil {
		t.Fatalf("create second file part: %v", err)
	}
	if _, err := second.Write([]byte("truncated second file")); err != nil {
		t.Fatalf("write second file: %v", err)
	}
	// Deliberately omit writer.Close so the second part terminates at transport EOF.
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", bytes.NewReader(body.Bytes()))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("If-Match", `"3"`)
	request.ContentLength = -1
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || responseErrorCode(t, response.Body.Bytes()) != "INVALID_MULTIPART" {
		t.Fatalf("truncated multipart response = %d: %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(artifactDir, ".staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("truncated multipart left staging entries=%v err=%v", entries, err)
	}
}

func TestArtifactStoreRejectsTraversalLinksAndOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := newArtifactStore(root, testArtifactDatabaseID, "")
	if err != nil {
		t.Fatalf("newArtifactStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.close() })
	for _, unsafe := range []string{"../outside", "objects/../outside", filepath.Join("objects", "not-a-uuid")} {
		if _, err := store.resolveObject(unsafe); err == nil {
			t.Fatalf("resolveObject(%q) accepted unsafe path", unsafe)
		}
	}
	artifactID := "a4eef45f-edc2-4ef2-8620-84c0af34cf72"
	staged, err := store.stageMultipartFile(io.NopCloser(strings.NewReader("new data")), artifactID)
	if err != nil {
		t.Fatalf("stage file: %v", err)
	}
	destination, _ := store.resolveObject(staged.relativePath)
	if err := os.WriteFile(destination, []byte("existing data"), 0o600); err != nil {
		t.Fatalf("seed occupied destination: %v", err)
	}
	if err := store.commitStagedFile(staged); err == nil {
		t.Fatal("commit overwrote an occupied destination")
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "existing data" {
		t.Fatalf("occupied destination changed to %q", content)
	}
	store.discardStagedFile(staged)

	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "artifact-link")
	if err := os.Symlink(target, link); err == nil {
		if _, err := newArtifactStore(link, testArtifactDatabaseID, ""); err == nil {
			t.Fatal("newArtifactStore accepted a symlink root")
		}
	}
}

func TestArtifactStoreEnforcesFileBoundsAndSafeStagingCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := newArtifactStore(root, testArtifactDatabaseID, "")
	if err != nil {
		t.Fatalf("newArtifactStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.close() })
	tooLargeID := "f0120f43-8400-4ca4-a959-36068d0924d0"
	if _, err := store.stageMultipartFileWithLimit(strings.NewReader("four"), tooLargeID, 3); !errors.Is(err, errArtifactFileTooLarge) {
		t.Fatalf("oversized stage error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.stagingDir, tooLargeID+".part")); !os.IsNotExist(err) {
		t.Fatalf("oversized stage retained partial file: %v", err)
	}
	emptyID := "fb842839-fe0e-4541-a1c4-069af969c331"
	if _, err := store.stageMultipartFileWithLimit(strings.NewReader(""), emptyID, 3); !errors.Is(err, errArtifactFileEmpty) {
		t.Fatalf("empty stage error = %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.part")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.discardStagedFile(stagedArtifactFile{stagingPath: outside})
	if content, err := os.ReadFile(outside); err != nil || string(content) != "keep" {
		t.Fatalf("unsafe staging cleanup touched outside path content=%q err=%v", content, err)
	}
}

func TestArtifactStoreRequiresOwnedNonRootDirectory(t *testing.T) {
	temp := t.TempDir()
	volumeRoot := filepath.VolumeName(temp) + string(filepath.Separator)
	if filepath.VolumeName(temp) == "" {
		volumeRoot = string(filepath.Separator)
	}
	if _, err := newArtifactStore(volumeRoot, testArtifactDatabaseID, ""); err == nil || !strings.Contains(err.Error(), "volume root") {
		t.Fatalf("filesystem root was accepted: %v", err)
	}

	shared := filepath.Join(temp, "shared")
	objectDir := filepath.Join(shared, "objects")
	if err := os.MkdirAll(objectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	valuable := filepath.Join(objectDir, "038f58ea-f8a7-481a-a4ca-959c4fcdf142")
	if err := os.WriteFile(valuable, []byte("not owned by OPC"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newArtifactStore(shared, testArtifactDatabaseID, ""); err == nil || !strings.Contains(err.Error(), "ownership marker") {
		t.Fatalf("unowned non-empty directory was accepted: %v", err)
	}
	if content, err := os.ReadFile(valuable); err != nil || string(content) != "not owned by OPC" {
		t.Fatalf("misconfigured root cleanup touched existing file content=%q err=%v", content, err)
	}

	owned := filepath.Join(temp, "owned")
	first, err := newArtifactStore(owned, testArtifactDatabaseID, "")
	if err != nil {
		t.Fatalf("claim empty root: %v", err)
	}
	t.Cleanup(func() { _ = first.close() })
	marker, err := os.ReadFile(filepath.Join(owned, artifactStoreMarkerName))
	expectedMarker, markerErr := artifactStoreMarkerBytes(testArtifactDatabaseID, first.storeID)
	if err != nil || markerErr != nil || string(marker) != string(expectedMarker) {
		t.Fatalf("ownership marker content=%q err=%v", marker, err)
	}
	if _, err := newArtifactStore(owned, testArtifactDatabaseID, first.storeID); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("concurrent Artifact root open was accepted: %v", err)
	}
	if err := first.close(); err != nil {
		t.Fatalf("release Artifact root: %v", err)
	}
	otherDatabaseID := "018f0000-0000-7000-8000-000000000998"
	if _, err := newArtifactStore(owned, otherDatabaseID, first.storeID); err == nil || !strings.Contains(err.Error(), "different workspace database") {
		t.Fatalf("mismatched database reused Artifact root: %v", err)
	}
	secondRoot := filepath.Join(temp, "second-owned")
	if _, err := newArtifactStore(secondRoot, testArtifactDatabaseID, first.storeID); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("bound database claimed a second empty root: %v", err)
	}
	second, err := newArtifactStore(owned, testArtifactDatabaseID, first.storeID)
	if err != nil || !sameFilesystemPath(first.root, second.root) {
		t.Fatalf("reopen released root store=%#v err=%v", second, err)
	}
	t.Cleanup(func() { _ = second.close() })
}

func TestSubmitOutputRejectsDeclaredOversizedMultipartRequest(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("manifest", `{"summary":"small body","artifacts":[]}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("If-Match", `"3"`)
	request.ContentLength = maxArtifactRequestBytes + 1
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || responseErrorCode(t, recorder.Body.Bytes()) != "REQUEST_TOO_LARGE" {
		t.Fatalf("oversized multipart = %d: %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", strings.NewReader(`{"summary":"small body","artifacts":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("If-Match", `"3"`)
	request.ContentLength = int64(maxJSONBodyBytes + 1)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || responseErrorCode(t, recorder.Body.Bytes()) != "REQUEST_TOO_LARGE" {
		t.Fatalf("oversized JSON = %d: %s", recorder.Code, recorder.Body.String())
	}

	oversizedJSON := `{"summary":"` + strings.Repeat("x", maxJSONBodyBytes) + `","artifacts":[]}`
	request = httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", strings.NewReader(oversizedJSON))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("If-Match", `"3"`)
	request.ContentLength = -1
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge || responseErrorCode(t, recorder.Body.Bytes()) != "REQUEST_TOO_LARGE" {
		t.Fatalf("chunked oversized JSON = %d: %s", recorder.Code, recorder.Body.String())
	}

	oversizedManifest := strings.Repeat("x", maxArtifactManifestBytes+1)
	recorder = performMultipartPartsRequest(
		router,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		[]multipartTestPart{{field: "manifest", content: []byte(oversizedManifest)}},
		map[string]string{"If-Match": `"3"`},
		true,
	)
	if recorder.Code != http.StatusRequestEntityTooLarge || responseErrorCode(t, recorder.Body.Bytes()) != "REQUEST_TOO_LARGE" {
		t.Fatalf("chunked oversized multipart manifest = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestReviewPolicySameValueDoesNotBlockFactsEdit(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	started := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/start", []byte(`{}`), map[string]string{"If-Match": `"3"`})
	if started.Code != http.StatusOK {
		t.Fatalf("start = %d: %s", started.Code, started.Body.String())
	}
	updated := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+task.ID, []byte(`{"title":"Updated while active","review_policy":"manual"}`), map[string]string{"If-Match": `"4"`})
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "Updated while active") {
		t.Fatalf("same-policy facts PATCH = %d: %s", updated.Code, updated.Body.String())
	}
	locked := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+task.ID, []byte(`{"review_policy":"none"}`), map[string]string{"If-Match": `"5"`})
	if locked.Code != http.StatusConflict || responseErrorCode(t, locked.Body.Bytes()) != "TASK_REVIEW_POLICY_LOCKED" {
		t.Fatalf("changed active policy = %d: %s", locked.Code, locked.Body.String())
	}
	var policyEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_id = ? AND action = 'task_review_policy_changed'", task.ID).
		Count(&policyEvents).Error; err != nil {
		t.Fatalf("count policy events: %v", err)
	}
	if policyEvents != 0 {
		t.Fatalf("same-value/rejected policy patches wrote %d events", policyEvents)
	}
}

func TestReviewPolicyChangeWritesSingleVersionedEvent(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Policy change"}`)
	changed := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+task.ID, []byte(`{"review_policy":"manual"}`), map[string]string{"If-Match": `"1"`})
	if changed.Code != http.StatusOK || changed.Header().Get("ETag") != `"2"` {
		t.Fatalf("policy change = %d headers=%v: %s", changed.Code, changed.Header(), changed.Body.String())
	}
	var event models.WorkflowEvent
	if err := store.DB.Where("aggregate_id = ? AND action = 'task_review_policy_changed'", task.ID).First(&event).Error; err != nil {
		t.Fatalf("load policy event: %v", err)
	}
	if event.CommandSeq == nil || *event.CommandSeq != 1 || event.RequestID == nil || *event.RequestID == "" ||
		event.CurrentJSON == nil || !strings.Contains(*event.CurrentJSON, `"version":2`) {
		t.Fatalf("policy event = %#v", event)
	}
}

func TestSubmitOutputValidatesReviewerClientRefsAndLinks(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Validation Task","review_policy":"manual"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Producer"}`, nil)
	createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 1, "")
	missingReviewer := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"ready","artifacts":[]}`), map[string]string{"If-Match": `"2"`})
	if missingReviewer.Code != http.StatusConflict || responseErrorCode(t, missingReviewer.Body.Bytes()) != "TASK_REVIEWER_REQUIRED" {
		t.Fatalf("missing reviewer = %d: %s", missingReviewer.Code, missingReviewer.Body.String())
	}
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 2, "")
	invalidCases := []struct {
		name string
		body string
	}{
		{"unknown producer", `{"summary":"x","produced_by_actor_id":"00000000-0000-5000-8000-000000000001","artifacts":[]}`},
		{"duplicate refs", `{"artifacts":[{"client_ref":"same","storage_kind":"text","name":"A","content_text":"a"},{"client_ref":"same","storage_kind":"text","name":"B","content_text":"b"}]}`},
		{"credential link", `{"artifacts":[{"client_ref":"link","storage_kind":"link","name":"Link","reference_url":"https://user:secret@example.com/output"}]}`},
	}
	for _, test := range invalidCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(test.body), map[string]string{"If-Match": `"3"`})
			if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("validation status = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	tooMany := make([]string, 21)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf(`{"client_ref":"%d","storage_kind":"text","name":"A","content_text":"a"}`, index)
	}
	recorder := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"artifacts":[`+strings.Join(tooMany, ",")+`]}`), map[string]string{"If-Match": `"3"`})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("too many Artifacts = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCancelPendingReviewWithdrawsSubmissionAndReopenClearsPointer(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	submitted := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"Cancel this delivery","artifacts":[]}`), map[string]string{"If-Match": `"3"`})
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit = %d: %s", submitted.Code, submitted.Body.String())
	}
	submission := decodeSubmitOutputResponse(t, submitted.Body.Bytes()).Submission
	cancelled := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/cancel", []byte(`{"reason":"No longer needed"}`), map[string]string{"If-Match": `"4"`})
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	cancelResponse := decodeTaskLifecycleResponse(t, cancelled.Body.Bytes())
	if cancelResponse.Task.Status != "cancelled" || cancelResponse.Task.CurrentSubmissionID == nil ||
		*cancelResponse.Task.CurrentSubmissionID != submission.ID || cancelResponse.Event.CommandSeq == nil || *cancelResponse.Event.CommandSeq != 4 {
		t.Fatalf("cancel response = %#v", cancelResponse)
	}
	var status string
	var withdrawnBy, withdrawnAt *string
	if err := store.SQL.QueryRow("SELECT status, withdrawn_by_actor_id, withdrawn_at FROM task_submissions WHERE id = ?", submission.ID).
		Scan(&status, &withdrawnBy, &withdrawnAt); err != nil {
		t.Fatalf("read withdrawn submission: %v", err)
	}
	if status != "withdrawn" || withdrawnBy == nil || *withdrawnBy != models.BuiltinOwnerActorID || withdrawnAt == nil {
		t.Fatalf("withdrawn submission status=%s actor=%v at=%v", status, withdrawnBy, withdrawnAt)
	}
	var withdrawalEvents int64
	_ = store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_id = ? AND action = 'task_submission_withdrawn' AND submission_id = ?", task.ID, submission.ID).
		Count(&withdrawalEvents).Error
	if withdrawalEvents != 1 {
		t.Fatalf("withdrawal event count = %d", withdrawalEvents)
	}
	reopened := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reopen", []byte(`{}`), map[string]string{"If-Match": `"5"`})
	if reopened.Code != http.StatusOK || decodeTaskLifecycleResponse(t, reopened.Body.Bytes()).Task.CurrentSubmissionID != nil {
		t.Fatalf("reopen = %d: %s", reopened.Code, reopened.Body.String())
	}
}

func TestTaskHardDeleteMovesControlledFilesBeforeCascade(t *testing.T) {
	router, store, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"Delete aggregate","artifacts":[{"client_ref":"file","storage_kind":"file","name":"aggregate.bin","file_field":"payload"}]}`
	submitted := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"payload": []byte("aggregate content")}, map[string]string{"If-Match": `"3"`})
	response := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	if submitted.Code != http.StatusCreated || len(response.Artifacts) != 1 {
		t.Fatalf("submit = %d: %s", submitted.Code, submitted.Body.String())
	}
	var relative string
	_ = store.SQL.QueryRow("SELECT relative_path FROM task_artifacts WHERE id = ?", response.Artifacts[0].ID).Scan(&relative)
	live := filepath.Join(artifactDir, filepath.FromSlash(relative))
	changes := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"Stop work"}`), map[string]string{"If-Match": `"4"`})
	if changes.Code != http.StatusOK {
		t.Fatalf("request changes = %d: %s", changes.Code, changes.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, map[string]string{"If-Match": `"5"`})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete Task = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("Task delete left controlled file: %v", err)
	}
	var taskCount, artifactCount int64
	_ = store.DB.Model(&models.Task{}).Where("id = ?", task.ID).Count(&taskCount).Error
	_ = store.DB.Model(&models.TaskArtifact{}).Where("task_id = ?", task.ID).Count(&artifactCount).Error
	if taskCount != 0 || artifactCount != 0 {
		t.Fatalf("Task delete left rows task=%d artifact=%d", taskCount, artifactCount)
	}
	var tombstone models.ArtifactDeletionTombstone
	if err := store.DB.First(&tombstone, "artifact_id = ?", response.Artifacts[0].ID).Error; err != nil ||
		tombstone.TaskID != task.ID || tombstone.RelativePath != relative || tombstone.DeletionScope != "task" {
		t.Fatalf("Task delete tombstone=%#v err=%v", tombstone, err)
	}
	trash, err := os.ReadDir(filepath.Join(artifactDir, ".trash"))
	if err != nil || len(trash) != 0 {
		t.Fatalf("Task delete left trash=%v err=%v", trash, err)
	}
}

func TestMissingArtifactFilesDoNotBlockConfirmedOrAggregateDeletion(t *testing.T) {
	router, store, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"Missing before deletion","artifacts":[{"client_ref":"file","storage_kind":"file","name":"missing.bin","file_field":"payload"}]}`
	submitted := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"payload": []byte("gone")}, map[string]string{"If-Match": `"3"`})
	response := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	if submitted.Code != http.StatusCreated || len(response.Artifacts) != 1 {
		t.Fatalf("submit = %d: %s", submitted.Code, submitted.Body.String())
	}
	changes := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"Remove evidence"}`), map[string]string{"If-Match": `"4"`})
	if changes.Code != http.StatusOK {
		t.Fatalf("request changes = %d: %s", changes.Code, changes.Body.String())
	}
	var relative string
	if err := store.SQL.QueryRow("SELECT relative_path FROM task_artifacts WHERE id = ?", response.Artifacts[0].ID).Scan(&relative); err != nil {
		t.Fatalf("read relative path: %v", err)
	}
	if err := os.Remove(filepath.Join(artifactDir, filepath.FromSlash(relative))); err != nil {
		t.Fatalf("remove object before soft delete: %v", err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+response.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"Already missing"}`), map[string]string{"If-Match": `"5"`})
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"integrity_status":"missing"`) {
		t.Fatalf("soft delete missing file = %d: %s", deleted.Code, deleted.Body.String())
	}
	var firstTombstone models.ArtifactDeletionTombstone
	if err := store.DB.First(&firstTombstone, "artifact_id = ?", response.Artifacts[0].ID).Error; err != nil || firstTombstone.DeletionScope != "artifact" {
		t.Fatalf("missing soft delete tombstone=%#v err=%v", firstTombstone, err)
	}

	task, _ = setupManualReviewTask(t, router)
	submitted = performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"payload": []byte("gone too")}, map[string]string{"If-Match": `"3"`})
	response = decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	if submitted.Code != http.StatusCreated || len(response.Artifacts) != 1 {
		t.Fatalf("second submit = %d: %s", submitted.Code, submitted.Body.String())
	}
	changes = performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"request_changes","reason":"Delete task"}`), map[string]string{"If-Match": `"4"`})
	if changes.Code != http.StatusOK {
		t.Fatalf("second request changes = %d: %s", changes.Code, changes.Body.String())
	}
	if err := store.SQL.QueryRow("SELECT relative_path FROM task_artifacts WHERE id = ?", response.Artifacts[0].ID).Scan(&relative); err != nil {
		t.Fatalf("read second relative path: %v", err)
	}
	if err := os.Remove(filepath.Join(artifactDir, filepath.FromSlash(relative))); err != nil {
		t.Fatalf("remove object before Task delete: %v", err)
	}
	deletedTask := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, map[string]string{"If-Match": `"5"`})
	if deletedTask.Code != http.StatusNoContent {
		t.Fatalf("Task delete with missing object = %d: %s", deletedTask.Code, deletedTask.Body.String())
	}
	var secondTombstone models.ArtifactDeletionTombstone
	if err := store.DB.First(&secondTombstone, "artifact_id = ?", response.Artifacts[0].ID).Error; err != nil || secondTombstone.DeletionScope != "task" {
		t.Fatalf("missing Task delete tombstone=%#v err=%v", secondTombstone, err)
	}
}

func TestArtifactStoreStartupReconcilesCrashWindows(t *testing.T) {
	router, db, artifactDir := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"Recovery","artifacts":[{"client_ref":"file","storage_kind":"file","name":"recovery.bin","file_field":"payload"}]}`
	submitted := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"payload": []byte("recover me")}, map[string]string{"If-Match": `"3"`})
	response := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	artifactIDValue := response.Artifacts[0].ID
	var relative string
	_ = db.SQL.QueryRow("SELECT relative_path FROM task_artifacts WHERE id = ?", artifactIDValue).Scan(&relative)
	store := &artifactStore{
		root:          artifactDir,
		objectsDir:    filepath.Join(artifactDir, "objects"),
		stagingDir:    filepath.Join(artifactDir, ".staging"),
		trashDir:      filepath.Join(artifactDir, ".trash"),
		quarantineDir: filepath.Join(artifactDir, ".quarantine"),
	}
	moved, err := store.moveObjectToTrash(relative, artifactIDValue)
	if err != nil {
		t.Fatalf("simulate crash before DB commit: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile active DB Artifact: %v", err)
	}
	if _, err := os.Stat(moved.livePath); err != nil {
		t.Fatalf("reconcile did not restore active file: %v", err)
	}
	if _, err := os.Stat(moved.trashPath); !os.IsNotExist(err) {
		t.Fatalf("reconcile retained restored trash: %v", err)
	}

	orphanID := "038f58ea-f8a7-481a-a4ca-959c4fcdf142"
	orphan := filepath.Join(artifactDir, "objects", orphanID)
	if err := os.WriteFile(orphan, []byte("orphan after DB rollback"), 0o600); err != nil {
		t.Fatalf("seed orphan object: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile orphan object: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("reconcile retained orphan in live objects: %v", err)
	}
	quarantined, err := os.ReadDir(filepath.Join(artifactDir, ".quarantine"))
	if err != nil || len(quarantined) != 1 || !strings.HasPrefix(quarantined[0].Name(), orphanID+"-") {
		t.Fatalf("reconcile quarantine entries=%v err=%v", quarantined, err)
	}

	if err := store.quarantineFile(moved.livePath, artifactIDValue); err != nil {
		t.Fatalf("simulate same-identity snapshot quarantine: %v", err)
	}
	if err := db.DB.Model(&models.TaskArtifact{}).Where("id = ?", artifactIDValue).Updates(map[string]any{
		"integrity_status": "missing", "integrity_checked_at": "2026-08-27T21:00:00Z",
	}).Error; err != nil {
		t.Fatalf("mark snapshot Artifact missing: %v", err)
	}
	if _, err := os.Stat(moved.livePath); !os.IsNotExist(err) {
		t.Fatalf("snapshot simulation retained live object: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile current database after snapshot: %v", err)
	}
	if content, err := os.ReadFile(moved.livePath); err != nil || string(content) != "recover me" {
		t.Fatalf("reconcile did not restore matching quarantined object content=%q err=%v", content, err)
	}
	var recoveredStatus string
	if err := db.SQL.QueryRow("SELECT integrity_status FROM task_artifacts WHERE id = ?", artifactIDValue).Scan(&recoveredStatus); err != nil || recoveredStatus != "verified" {
		t.Fatalf("recovered Artifact integrity_status=%q err=%v", recoveredStatus, err)
	}
	quarantined, err = os.ReadDir(filepath.Join(artifactDir, ".quarantine"))
	if err != nil || len(quarantined) != 1 || !strings.HasPrefix(quarantined[0].Name(), orphanID+"-") {
		t.Fatalf("reconcile changed unrelated quarantine entries=%v err=%v", quarantined, err)
	}

	corruptTrash := filepath.Join(artifactDir, ".trash", artifactIDValue+"-048f58ea-f8a7-481a-a4ca-959c4fcdf143.trash")
	if err := os.WriteFile(corruptTrash, bytes.Repeat([]byte("x"), len("recover me")), 0o600); err != nil {
		t.Fatalf("seed mismatched active trash: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile mismatched active trash: %v", err)
	}
	if content, err := os.ReadFile(moved.livePath); err != nil || string(content) != "recover me" {
		t.Fatalf("mismatched trash displaced valid live object content=%q err=%v", content, err)
	}
	if _, err := os.Stat(corruptTrash); !os.IsNotExist(err) {
		t.Fatalf("mismatched trash remained active: %v", err)
	}

	verifiedTrash := filepath.Join(artifactDir, ".trash", artifactIDValue+"-048f58ea-f8a7-481a-a4ca-959c4fcdf144.trash")
	if err := os.WriteFile(verifiedTrash, []byte("recover me"), 0o600); err != nil {
		t.Fatalf("seed verified recovery trash: %v", err)
	}
	if err := os.WriteFile(moved.livePath, bytes.Repeat([]byte("y"), len("recover me")), 0o600); err != nil {
		t.Fatalf("corrupt live object: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile verified trash over corrupted live object: %v", err)
	}
	if content, err := os.ReadFile(moved.livePath); err != nil || string(content) != "recover me" {
		t.Fatalf("verified trash did not restore correct live content=%q err=%v", content, err)
	}
	quarantineBeforeCommittedDelete, err := os.ReadDir(filepath.Join(artifactDir, ".quarantine"))
	if err != nil {
		t.Fatalf("read quarantine before committed delete: %v", err)
	}

	moved, err = store.moveObjectToTrash(relative, artifactIDValue)
	if err != nil {
		t.Fatalf("simulate crash after DB commit: %v", err)
	}
	now := "2026-08-27T21:15:00Z"
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var artifact models.TaskArtifact
		if err := tx.First(&artifact, "id = ?", artifactIDValue).Error; err != nil {
			return err
		}
		if err := recordArtifactDeletionTombstone(tx, artifact, "artifact", now); err != nil {
			return err
		}
		return tx.Model(&models.TaskArtifact{}).Where("id = ?", artifactIDValue).Updates(map[string]any{
			"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "committed before crash",
		}).Error
	}); err != nil {
		t.Fatalf("commit Artifact deletion fact: %v", err)
	}
	if err := store.reconcile(db.DB); err != nil {
		t.Fatalf("reconcile committed delete: %v", err)
	}
	if _, err := os.Stat(moved.trashPath); !os.IsNotExist(err) {
		t.Fatalf("reconcile retained committed trash: %v", err)
	}
	quarantined, err = os.ReadDir(filepath.Join(artifactDir, ".quarantine"))
	if err != nil {
		t.Fatalf("read quarantine after committed delete: %v", err)
	}
	if len(quarantined) != len(quarantineBeforeCommittedDelete) {
		t.Fatalf("authorized committed delete changed quarantine entries before=%v after=%v", quarantineBeforeCommittedDelete, quarantined)
	}
	for index := range quarantined {
		if quarantined[index].Name() != quarantineBeforeCommittedDelete[index].Name() {
			t.Fatalf("authorized committed delete changed quarantine entries before=%v after=%v", quarantineBeforeCommittedDelete, quarantined)
		}
	}
}
