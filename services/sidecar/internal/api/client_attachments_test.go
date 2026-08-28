package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func newClientAttachmentTestAPI(t *testing.T) (*Router, *database.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	artifactRoot := filepath.Join(root, "artifacts")
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactRoot,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router, store, artifactRoot
}

func performClientAttachmentUpload(
	t *testing.T,
	router http.Handler,
	path string,
	metadata string,
	filename string,
	content []byte,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("metadata", metadata); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
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

func decodeClientAttachmentResponse(t *testing.T, body []byte) clientAttachmentResponse {
	t.Helper()
	var envelope struct {
		Data clientAttachmentResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode client attachment response: %v: %s", err, body)
	}
	return envelope.Data
}

func TestClientAttachmentUploadListDownloadDeleteAndReplay(t *testing.T) {
	router, store, artifactRoot := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Attachment Client"}`, nil)
	content := []byte("client attachment body")
	headers := map[string]string{"If-Match": `"1"`, "Idempotency-Key": "client-attachment-upload-1"}
	createdRecorder := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"  合同.pdf  "}`, "source.pdf", content, headers,
	)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("upload attachment = %d headers=%v body=%s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeClientAttachmentResponse(t, createdRecorder.Body.Bytes())
	if created.ClientID != client.ID || created.Name != "合同.pdf" || created.SizeBytes != int64(len(content)) ||
		created.ClientVersion != 2 || created.DeletedAt != nil || created.RecordedBy.ID == "" || created.IntegrityStatus != "verified" {
		t.Fatalf("created attachment = %#v", created)
	}
	objectPath := filepath.Join(artifactRoot, "objects", created.ID)
	if actual, err := os.ReadFile(objectPath); err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("controlled attachment object=%q err=%v", actual, err)
	}

	replay := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"合同.pdf"}`, "again.pdf", content, headers,
	)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeClientAttachmentResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("attachment replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	var count int64
	if err := store.DB.Model(&clientAttachmentRow{}).Table("client_attachments").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("attachment count=%d err=%v", count, err)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/attachments", nil, nil)
	if listed.Code != http.StatusOK || listed.Header().Get("ETag") != `"2"` {
		t.Fatalf("list attachments = %d headers=%v body=%s", listed.Code, listed.Header(), listed.Body.String())
	}
	var listEnvelope struct {
		Data []clientAttachmentResponse `json:"data"`
		Meta clientAttachmentMeta       `json:"meta"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data) != 1 ||
		listEnvelope.Meta.Total != 1 || listEnvelope.Meta.ClientVersion != 2 {
		t.Fatalf("attachment list=%#v err=%v", listEnvelope, err)
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/client-attachments/"+created.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"2"` {
		t.Fatalf("attachment detail = %d headers=%v body=%s", detail.Code, detail.Header(), detail.Body.String())
	}
	download := performRequest(router, http.MethodGet, "/api/v1/client-attachments/"+created.ID+"/content", nil, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), content) ||
		download.Header().Get("Content-Disposition") == "" || download.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("attachment download = %d headers=%v body=%q", download.Code, download.Header(), download.Body.Bytes())
	}

	stale := performRequest(
		router, http.MethodDelete, "/api/v1/client-attachments/"+created.ID+"?confirm=true",
		[]byte(`{"reason":"obsolete"}`), map[string]string{"If-Match": `"1"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale attachment delete = %d: %s", stale.Code, stale.Body.String())
	}
	deletedRecorder := performRequest(
		router, http.MethodDelete, "/api/v1/client-attachments/"+created.ID+"?confirm=true",
		[]byte(`{"reason":"合同已替换"}`), map[string]string{"If-Match": `"2"`, "Idempotency-Key": "client-attachment-delete-1"},
	)
	if deletedRecorder.Code != http.StatusOK || deletedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("delete attachment = %d headers=%v body=%s", deletedRecorder.Code, deletedRecorder.Header(), deletedRecorder.Body.String())
	}
	deleted := decodeClientAttachmentResponse(t, deletedRecorder.Body.Bytes())
	if deleted.DeletedAt == nil || deleted.DeleteReason == nil || *deleted.DeleteReason != "合同已替换" || deleted.ClientVersion != 3 {
		t.Fatalf("deleted attachment = %#v", deleted)
	}
	if _, err := os.Stat(objectPath); !errorsIsNotExist(err) {
		t.Fatalf("deleted object still exists err=%v", err)
	}
	active := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/attachments", nil, nil)
	if active.Code != http.StatusOK || !jsonListDataIsEmpty(t, active.Body.Bytes()) {
		t.Fatalf("active attachment list = %d: %s", active.Code, active.Body.String())
	}
	history := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/attachments?include_deleted=true", nil, nil)
	if history.Code != http.StatusOK || jsonListDataIsEmpty(t, history.Body.Bytes()) {
		t.Fatalf("attachment history = %d: %s", history.Code, history.Body.String())
	}
	gone := performRequest(router, http.MethodGet, "/api/v1/client-attachments/"+created.ID+"/content", nil, nil)
	if gone.Code != http.StatusGone || responseErrorCode(t, gone.Body.Bytes()) != "CLIENT_ATTACHMENT_DELETED" {
		t.Fatalf("deleted attachment content = %d: %s", gone.Code, gone.Body.String())
	}
}

func TestClientAttachmentUploadValidatesMultipartAndActivityOwnership(t *testing.T) {
	router, _, artifactRoot := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"First"}`, nil)
	other := createClientForTest(t, router, `{"name":"Second"}`, nil)
	activity := createClientActivityForAttachmentTest(t, router, other.ID)

	mismatch := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"wrong.txt","activity_id":"`+activity.ID+`"}`, "wrong.txt", []byte("wrong"),
		map[string]string{"If-Match": `"1"`},
	)
	if mismatch.Code != http.StatusConflict || responseErrorCode(t, mismatch.Body.Bytes()) != "CLIENT_ACTIVITY_UNAVAILABLE" {
		t.Fatalf("cross-client activity upload = %d: %s", mismatch.Code, mismatch.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(artifactRoot, "objects"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected upload left objects=%v err=%v", entries, err)
	}

	withoutVersion := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments", `{}`, "fallback.txt", []byte("body"), nil,
	)
	if withoutVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, withoutVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("upload without version = %d: %s", withoutVersion.Code, withoutVersion.Body.String())
	}
}

func TestClientHardDeletePurgesActiveAttachmentAndKeepsTombstone(t *testing.T) {
	router, store, artifactRoot := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Delete With File"}`, nil)
	upload := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments", `{"name":"archive.bin"}`, "archive.bin", []byte("archive"),
		map[string]string{"If-Match": `"1"`},
	)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload before client delete = %d: %s", upload.Code, upload.Body.String())
	}
	attachment := decodeClientAttachmentResponse(t, upload.Body.Bytes())
	updated := performRequest(router, http.MethodPatch, "/api/v1/clients/"+client.ID, []byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"2"`})
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"3"` {
		t.Fatalf("deactivate client = %d headers=%v body=%s", updated.Code, updated.Header(), updated.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/clients/"+client.ID+"?confirm=true", nil, map[string]string{"If-Match": `"3"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete client with attachment = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(filepath.Join(artifactRoot, "objects", attachment.ID)); !errorsIsNotExist(err) {
		t.Fatalf("client delete left object err=%v", err)
	}
	var tombstones int64
	if err := store.DB.Table("client_attachment_deletion_tombstones").Where("attachment_id = ? AND deletion_scope = 'client'", attachment.ID).Count(&tombstones).Error; err != nil || tombstones != 1 {
		t.Fatalf("client attachment tombstone count=%d err=%v", tombstones, err)
	}
}

func TestArtifactStoreReconcilesClientAttachmentCrashWindows(t *testing.T) {
	router, store, artifactRoot := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Recovery Client"}`, nil)
	upload := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments", `{"name":"recover.bin"}`, "recover.bin", []byte("recover client file"),
		map[string]string{"If-Match": `"1"`},
	)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload recovery fixture = %d: %s", upload.Code, upload.Body.String())
	}
	attachment := decodeClientAttachmentResponse(t, upload.Body.Bytes())
	relative := "objects/" + attachment.ID
	moved, err := router.artifactStore.moveObjectToTrash(relative, attachment.ID)
	if err != nil {
		t.Fatalf("simulate pre-commit client attachment crash: %v", err)
	}
	if err := router.artifactStore.reconcile(store.DB); err != nil {
		t.Fatalf("reconcile active client attachment: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(artifactRoot, "objects", attachment.ID)); err != nil || string(content) != "recover client file" {
		t.Fatalf("active client attachment was not restored content=%q err=%v", content, err)
	}
	if _, err := os.Stat(moved.trashPath); !os.IsNotExist(err) {
		t.Fatalf("restored client attachment retained trash: %v", err)
	}

	moved, err = router.artifactStore.moveObjectToTrash(relative, attachment.ID)
	if err != nil {
		t.Fatalf("simulate committed client attachment deletion: %v", err)
	}
	now := "2026-08-28T12:00:00Z"
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var fact models.ClientAttachment
		if err := tx.First(&fact, "id = ?", attachment.ID).Error; err != nil {
			return err
		}
		if err := recordClientAttachmentDeletionTombstone(tx, fact, "attachment", now); err != nil {
			return err
		}
		return tx.Model(&models.ClientAttachment{}).Where("id = ?", attachment.ID).Updates(map[string]any{
			"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "crash fixture",
		}).Error
	}); err != nil {
		t.Fatalf("commit client attachment deletion fixture: %v", err)
	}
	if err := router.artifactStore.reconcile(store.DB); err != nil {
		t.Fatalf("reconcile committed client attachment deletion: %v", err)
	}
	if _, err := os.Stat(moved.trashPath); !os.IsNotExist(err) {
		t.Fatalf("committed client attachment trash was not purged: %v", err)
	}
}

func createClientActivityForAttachmentTest(t *testing.T, router http.Handler, clientID string) clientActivityResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/clients/"+clientID+"/activities", []byte(`{
		"kind":"note","title":"Attachment source","body":"source","occurred_at":"2026-08-28T00:00:00Z"
	}`), nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create source activity = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeClientActivityResponse(t, recorder.Body.Bytes())
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
