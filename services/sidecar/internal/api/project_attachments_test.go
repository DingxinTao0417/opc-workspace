package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeProjectAttachmentResponse(t *testing.T, body []byte) projectAttachmentResponse {
	t.Helper()
	var envelope struct {
		Data projectAttachmentResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project attachment response: %v: %s", err, body)
	}
	return envelope.Data
}

func TestProjectAttachmentUploadListDownloadDeleteAndReplay(t *testing.T) {
	router, store, artifactRoot := newClientAttachmentTestAPI(t)
	project := createProjectForTest(t, router.Engine, `{"name":"Attachment project"}`, nil)
	content := []byte("project attachment payload")
	upload := performMultipartPartsRequest(router.Engine, "/api/v1/projects/"+project.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"Delivery notes.txt"}`)},
		{field: "file", filename: "delivery.txt", content: content},
	}, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "project-attachment-1"}, false)
	if upload.Code != http.StatusCreated || upload.Header().Get("ETag") != `"2"` {
		t.Fatalf("upload = %d headers=%v body=%s", upload.Code, upload.Header(), upload.Body.String())
	}
	created := decodeProjectAttachmentResponse(t, upload.Body.Bytes())
	if created.ProjectID != project.ID || created.Name != "Delivery notes.txt" || created.SizeBytes != int64(len(content)) || created.ProjectVersion != 2 {
		t.Fatalf("created attachment = %#v", created)
	}
	objectPath := filepath.Join(artifactRoot, filepath.FromSlash("objects/"+created.ID))
	if stored, err := os.ReadFile(objectPath); err != nil || string(stored) != string(content) {
		t.Fatalf("stored object = %q err=%v", stored, err)
	}

	replay := performMultipartPartsRequest(router.Engine, "/api/v1/projects/"+project.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"Delivery notes.txt"}`)},
		{field: "file", filename: "delivery.txt", content: content},
	}, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "project-attachment-1"}, false)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || decodeProjectAttachmentResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	var count int64
	if err := store.DB.Model(&models.ProjectAttachment{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("attachment count = %d err=%v", count, err)
	}

	listed := performRequest(router.Engine, http.MethodGet, "/api/v1/projects/"+project.ID+"/attachments", nil, nil)
	if listed.Code != http.StatusOK || listed.Header().Get("ETag") != `"2"` || jsonListDataIsEmpty(t, listed.Body.Bytes()) {
		t.Fatalf("list = %d headers=%v body=%s", listed.Code, listed.Header(), listed.Body.String())
	}
	download := performRequest(router.Engine, http.MethodGet, "/api/v1/project-attachments/"+created.ID+"/content", nil, nil)
	if download.Code != http.StatusOK || download.Body.String() != string(content) || download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download = %d headers=%v body=%q", download.Code, download.Header(), download.Body.String())
	}

	deletedRecorder := performRequest(router.Engine, http.MethodDelete, "/api/v1/project-attachments/"+created.ID+"?confirm=true", []byte(`{"reason":"Replaced"}`), map[string]string{"If-Match": `"2"`, "Idempotency-Key": "project-attachment-delete-1"})
	if deletedRecorder.Code != http.StatusOK || deletedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("delete = %d headers=%v body=%s", deletedRecorder.Code, deletedRecorder.Header(), deletedRecorder.Body.String())
	}
	deleted := decodeProjectAttachmentResponse(t, deletedRecorder.Body.Bytes())
	if deleted.DeletedAt == nil || deleted.DeleteReason == nil || *deleted.DeleteReason != "Replaced" || deleted.ProjectVersion != 3 {
		t.Fatalf("deleted attachment = %#v", deleted)
	}
	if _, err := os.Stat(objectPath); !os.IsNotExist(err) {
		t.Fatalf("deleted object still exists err=%v", err)
	}
	active := performRequest(router.Engine, http.MethodGet, "/api/v1/projects/"+project.ID+"/attachments", nil, nil)
	if active.Code != http.StatusOK || !jsonListDataIsEmpty(t, active.Body.Bytes()) {
		t.Fatalf("active list = %d body=%s", active.Code, active.Body.String())
	}
	history := performRequest(router.Engine, http.MethodGet, "/api/v1/projects/"+project.ID+"/attachments?include_deleted=true", nil, nil)
	if history.Code != http.StatusOK || jsonListDataIsEmpty(t, history.Body.Bytes()) {
		t.Fatalf("history = %d body=%s", history.Code, history.Body.String())
	}
}

func TestProjectAttachmentArchivedReadOnlyAndProjectDeleteCleanup(t *testing.T) {
	router, store, artifactRoot := newClientAttachmentTestAPI(t)
	project := createProjectForTest(t, router.Engine, `{"name":"Archived attachments"}`, nil)
	upload := performMultipartPartsRequest(router.Engine, "/api/v1/projects/"+project.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"archive.txt"}`)},
		{field: "file", filename: "archive.txt", content: []byte("archive")},
	}, map[string]string{"If-Match": `"1"`}, false)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload = %d: %s", upload.Code, upload.Body.String())
	}
	attachment := decodeProjectAttachmentResponse(t, upload.Body.Bytes())
	archived := transitionProjectForTest(t, router.Engine, project.ID, 2, `{"action":"archive"}`)
	if archived.Version != 3 {
		t.Fatalf("archived project = %#v", archived)
	}

	rejectedDelete := performRequest(router.Engine, http.MethodDelete, "/api/v1/project-attachments/"+attachment.ID+"?confirm=true", []byte(`{"reason":"No writes"}`), map[string]string{"If-Match": `"3"`})
	if rejectedDelete.Code != http.StatusConflict || responseErrorCode(t, rejectedDelete.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("archived delete = %d: %s", rejectedDelete.Code, rejectedDelete.Body.String())
	}
	rejectedUpload := performMultipartPartsRequest(router.Engine, "/api/v1/projects/"+project.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"late.txt"}`)},
		{field: "file", filename: "late.txt", content: []byte("late")},
	}, map[string]string{"If-Match": `"3"`}, false)
	if rejectedUpload.Code != http.StatusConflict || responseErrorCode(t, rejectedUpload.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("archived upload = %d: %s", rejectedUpload.Code, rejectedUpload.Body.String())
	}

	deletedProject := performRequest(router.Engine, http.MethodDelete, "/api/v1/projects/"+project.ID+"?confirm=true", nil, map[string]string{"If-Match": `"3"`})
	if deletedProject.Code != http.StatusOK {
		t.Fatalf("delete project = %d: %s", deletedProject.Code, deletedProject.Body.String())
	}
	var count int64
	if err := store.DB.Model(&models.ProjectAttachment{}).Where("id = ?", attachment.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("project attachment after project delete = %d err=%v", count, err)
	}
	var tombstones int64
	if err := store.DB.Model(&models.ProjectAttachmentDeletionTombstone{}).Where("attachment_id = ? AND deletion_scope = 'project'", attachment.ID).Count(&tombstones).Error; err != nil || tombstones != 1 {
		t.Fatalf("project tombstone count = %d err=%v", tombstones, err)
	}
	if _, err := os.Stat(filepath.Join(artifactRoot, "objects", attachment.ID)); !os.IsNotExist(err) {
		t.Fatalf("project delete left object err=%v", err)
	}
}
