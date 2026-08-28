package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeProjectNoteResponse(t *testing.T, body []byte) projectNoteResponse {
	t.Helper()
	var envelope struct {
		Data projectNoteResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project note response: %v", err)
	}
	return envelope.Data
}

func TestProjectNoteCreateListUpdateAndSoftDelete(t *testing.T) {
	router, store := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Project Notes"}`, nil)
	occurredAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339)
	body := fmt.Sprintf(`{"title":"  启动记录  ","body":"  第一行\n第二行  ","occurred_at":%q}`, occurredAt)
	headers := map[string]string{"Idempotency-Key": "project-note-create-1"}

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(body), headers)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create project note = %d headers=%v body=%s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeProjectNoteResponse(t, createdRecorder.Body.Bytes())
	if created.ProjectID != project.ID || created.Title != "启动记录" || created.Body == nil || *created.Body != "第一行\n第二行" ||
		created.OccurredAt != occurredAt || created.CreatedBy.ID != models.BuiltinOwnerActorID || created.CreatedBy.Type != "owner" ||
		created.Version != 1 || created.ProjectVersion != 2 || created.DeletedAt != nil {
		t.Fatalf("created project note = %#v", created)
	}

	replay := performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(body), headers)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || decodeProjectNoteResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("project note replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(fmt.Sprintf(`{"title":"其他","body":"正文","occurred_at":%q}`, occurredAt)), headers)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("project note idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
	if got := readAPIInt64(t, store.DB, "SELECT COUNT(*) FROM project_notes"); got != 1 {
		t.Fatalf("project note count after replay = %d, want 1", got)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/notes?page=1&page_size=10", nil, nil)
	if listed.Code != http.StatusOK || listed.Header().Get("ETag") != `"2"` {
		t.Fatalf("list project notes = %d headers=%v body=%s", listed.Code, listed.Header(), listed.Body.String())
	}
	var listEnvelope struct {
		Data []projectNoteResponse `json:"data"`
		Meta struct {
			Page           int   `json:"page"`
			PageSize       int   `json:"page_size"`
			Total          int64 `json:"total"`
			ProjectVersion int64 `json:"project_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data) != 1 || listEnvelope.Meta.Total != 1 || listEnvelope.Meta.ProjectVersion != 2 {
		t.Fatalf("project note list = %#v", listEnvelope)
	}

	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/project-notes/"+created.ID, []byte(`{"title":"新标题"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing project note version = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/project-notes/"+created.ID, []byte(`{"title":"复盘记录","body":"确认下一步"}`), map[string]string{"If-Match": `"1"`})
	if updatedRecorder.Code != http.StatusOK || updatedRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("update project note = %d headers=%v body=%s", updatedRecorder.Code, updatedRecorder.Header(), updatedRecorder.Body.String())
	}
	updated := decodeProjectNoteResponse(t, updatedRecorder.Body.Bytes())
	if updated.Title != "复盘记录" || updated.Body == nil || *updated.Body != "确认下一步" || updated.Version != 2 || updated.ProjectVersion != 3 {
		t.Fatalf("updated project note = %#v", updated)
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/project-notes/"+created.ID, []byte(`{"title":"旧窗口覆盖"}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale project note update = %d: %s", stale.Code, stale.Body.String())
	}

	unconfirmed := performRequest(router, http.MethodDelete, "/api/v1/project-notes/"+created.ID, []byte(`{"reason":"录入重复"}`), map[string]string{"If-Match": `"2"`})
	if unconfirmed.Code != http.StatusUnprocessableEntity || responseErrorCode(t, unconfirmed.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed project note delete = %d: %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	deletedRecorder := performRequest(router, http.MethodDelete, "/api/v1/project-notes/"+created.ID+"?confirm=true", []byte(`{"reason":"  录入重复  "}`), map[string]string{"If-Match": `"2"`})
	if deletedRecorder.Code != http.StatusOK || deletedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("delete project note = %d headers=%v body=%s", deletedRecorder.Code, deletedRecorder.Header(), deletedRecorder.Body.String())
	}
	deleted := decodeProjectNoteResponse(t, deletedRecorder.Body.Bytes())
	if deleted.Body != nil || deleted.DeletedAt == nil || deleted.DeleteReason == nil || *deleted.DeleteReason != "录入重复" || deleted.Version != 3 || deleted.ProjectVersion != 4 {
		t.Fatalf("deleted project note = %#v", deleted)
	}
	activeList := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/notes", nil, nil)
	if activeList.Code != http.StatusOK || !jsonListDataIsEmpty(t, activeList.Body.Bytes()) {
		t.Fatalf("active project note list after delete = %d: %s", activeList.Code, activeList.Body.String())
	}
	deletedList := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/notes?include_deleted=true", nil, nil)
	if deletedList.Code != http.StatusOK || jsonListDataIsEmpty(t, deletedList.Body.Bytes()) {
		t.Fatalf("deleted project note history = %d: %s", deletedList.Code, deletedList.Body.String())
	}
	deletedUpdate := performRequest(router, http.MethodPatch, "/api/v1/project-notes/"+created.ID, []byte(`{"title":"不能修改"}`), map[string]string{"If-Match": `"3"`})
	if deletedUpdate.Code != http.StatusConflict || responseErrorCode(t, deletedUpdate.Body.Bytes()) != "PROJECT_NOTE_DELETED" {
		t.Fatalf("deleted project note update = %d: %s", deletedUpdate.Code, deletedUpdate.Body.String())
	}
}

func TestProjectNoteValidationPaginationAndArchivedReadOnly(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Project Note Rules"}`, nil)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for name, body := range map[string]string{
		"empty title": `{"title":" ","body":"Body","occurred_at":"2026-08-28T08:00:00Z"}`,
		"empty body":  `{"title":"Title","body":" ","occurred_at":"2026-08-28T08:00:00Z"}`,
		"future":      fmt.Sprintf(`{"title":"Future","body":"Body","occurred_at":%q}`, future),
		"control":     `{"title":"Bad\u0001","body":"Body","occurred_at":"2026-08-28T08:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(body), nil)
			if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "VALIDATION_ERROR" {
				t.Fatalf("validation = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	badFilter := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/notes?include_deleted=1", nil, nil)
	if badFilter.Code != http.StatusBadRequest || responseErrorCode(t, badFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("bad include_deleted = %d: %s", badFilter.Code, badFilter.Body.String())
	}

	occurredAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Format(time.RFC3339)
	created := decodeProjectNoteResponse(t, performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(fmt.Sprintf(`{"title":"Before archive","body":"Body","occurred_at":%q}`, occurredAt)), nil).Body.Bytes())
	project = transitionProjectForTest(t, router, project.ID, created.ProjectVersion, `{"action":"archive"}`)
	blockedCreate := performRequest(router, http.MethodPost, "/api/v1/projects/"+project.ID+"/notes", []byte(fmt.Sprintf(`{"title":"Blocked","body":"Body","occurred_at":%q}`, occurredAt)), nil)
	if blockedCreate.Code != http.StatusConflict || responseErrorCode(t, blockedCreate.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("archived project note create = %d: %s", blockedCreate.Code, blockedCreate.Body.String())
	}
	blockedUpdate := performRequest(router, http.MethodPatch, "/api/v1/project-notes/"+created.ID, []byte(`{"title":"Blocked"}`), map[string]string{"If-Match": `"1"`})
	if blockedUpdate.Code != http.StatusConflict || responseErrorCode(t, blockedUpdate.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("archived project note update = %d: %s", blockedUpdate.Code, blockedUpdate.Body.String())
	}
	readable := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/notes", nil, nil)
	if readable.Code != http.StatusOK || jsonListDataIsEmpty(t, readable.Body.Bytes()) {
		t.Fatalf("archived project note list = %d: %s", readable.Code, readable.Body.String())
	}
}
