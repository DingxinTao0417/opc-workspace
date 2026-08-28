package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

const testToken = "test-session-token"

func newTestAPI(t *testing.T) *gin.Engine {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func performRequest(router http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testToken)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestHealthRequiresAuthenticationAndReturnsVersions(t *testing.T) {
	router := newTestAPI(t)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var apiError errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &apiError); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if apiError.Code != "UNAUTHORIZED" || apiError.RequestID == "" {
		t.Fatalf("error response = %#v", apiError)
	}

	recorder = performRequest(router, http.MethodGet, "/health", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authenticated health status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	for _, key := range []string{"status", "app", "api", "schema"} {
		if _, ok := response[key]; !ok {
			t.Fatalf("health response missing %q: %s", key, recorder.Body.String())
		}
	}
}

func TestTaskCreateListStatusAndTodayStats(t *testing.T) {
	router := newTestAPI(t)
	today := time.Now().UTC().Format("2006-01-02")
	body := []byte(`{"title":"Write integration tests","priority":"P1","planned_date":"` + today + `","estimated_minutes":45}`)
	recorder := performRequest(router, http.MethodPost, "/api/v1/tasks", body, map[string]string{"Idempotency-Key": "task-create-1"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.ID == "" {
		t.Fatal("created task id is empty")
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/tasks", body, map[string]string{"Idempotency-Key": "task-create-1"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("idempotency replay status=%d header=%q body=%s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}

	list := performRequest(router, http.MethodGet, "/api/v1/tasks?planned_date="+today, nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", list.Code, list.Body.String())
	}
	var listed struct {
		Data []json.RawMessage `json:"data"`
		Meta pageMeta          `json:"meta"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Data) != 1 || listed.Meta.Total != 1 {
		t.Fatalf("list response = %s", list.Body.String())
	}

	status := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID+"/status",
		[]byte(`{"status":"done"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if status.Code != http.StatusOK {
		t.Fatalf("status update = %d: %s", status.Code, status.Body.String())
	}

	stats := performRequest(router, http.MethodGet, "/api/v1/stats/today?date="+today, nil, nil)
	if stats.Code != http.StatusOK {
		t.Fatalf("stats status = %d: %s", stats.Code, stats.Body.String())
	}
	var todayResponse struct {
		Data struct {
			Tasks taskStats `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stats.Body.Bytes(), &todayResponse); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	if todayResponse.Data.Tasks.Total != 1 || todayResponse.Data.Tasks.Completed != 1 {
		t.Fatalf("today stats = %s", stats.Body.String())
	}
}

func TestTaskCreateIdempotencyReplaysOriginalResponseAfterMutationAndDeletion(t *testing.T) {
	router := newTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Idempotency project"}`, nil)
	body := []byte(fmt.Sprintf(
		`{"title":"Original task","priority":"P1","project_id":%q,"estimated_minutes":30}`,
		project.ID,
	))
	headers := map[string]string{"Idempotency-Key": "task-durable-replay-1"}

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/tasks", body, headers)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	var created struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(createdRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Data.ProjectName == nil || *created.Data.ProjectName != project.Name {
		t.Fatalf("created project name = %#v", created.Data.ProjectName)
	}

	updated := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID,
		[]byte(`{"title":"Mutated task"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("mutate task status = %d: %s", updated.Code, updated.Body.String())
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/tasks", body, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay status=%d header=%q body=%s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	var replayedTask struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayedTask); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayedTask.Data.ID != created.Data.ID || replayedTask.Data.Title != created.Data.Title ||
		replayedTask.Data.ProjectName == nil || *replayedTask.Data.ProjectName != project.Name {
		t.Fatalf("replayed task = %#v, original = %#v", replayedTask.Data, created.Data)
	}

	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+created.Data.ID,
		nil,
		map[string]string{"If-Match": `"2"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete task status = %d: %s", deleted.Code, deleted.Body.String())
	}
	replayedAfterDelete := performRequest(router, http.MethodPost, "/api/v1/tasks", body, headers)
	if replayedAfterDelete.Code != http.StatusCreated || replayedAfterDelete.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay after delete status=%d header=%q body=%s", replayedAfterDelete.Code, replayedAfterDelete.Header().Get("Idempotency-Replayed"), replayedAfterDelete.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/tasks/"+created.Data.ID, nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("replay recreated deleted task: %d: %s", missing.Code, missing.Body.String())
	}

	conflict := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(`{"title":"Different task"}`),
		headers,
	)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflicting replay status = %d: %s", conflict.Code, conflict.Body.String())
	}
}

func TestTaskUpdateAndDelete(t *testing.T) {
	router := newTestAPI(t)
	create := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(`{"title":"Prepare project brief","priority":"P2","planned_date":"2026-08-28","due_date":"2026-08-28T18:00:00Z","estimated_minutes":45}`),
		nil,
	)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", create.Code, create.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	updated := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID,
		[]byte(`{"title":"Prepare final project brief","description":"Confirm scope and delivery date","priority":"P1","planned_date":"2026-08-29","due_date":"2026-08-29T19:30:00+08:00","estimated_minutes":90}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", updated.Code, updated.Body.String())
	}
	var updateResponse struct {
		Data struct {
			Title            string  `json:"title"`
			Description      string  `json:"description"`
			Status           string  `json:"status"`
			Priority         string  `json:"priority"`
			PlannedDate      *string `json:"planned_date"`
			DueDate          *string `json:"due_date"`
			EstimatedMinutes *int    `json:"estimated_minutes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &updateResponse); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updateResponse.Data.Title != "Prepare final project brief" ||
		updateResponse.Data.Description != "Confirm scope and delivery date" ||
		updateResponse.Data.Status != "todo" ||
		updateResponse.Data.Priority != "P1" ||
		updateResponse.Data.PlannedDate == nil || *updateResponse.Data.PlannedDate != "2026-08-29" ||
		updateResponse.Data.DueDate == nil || *updateResponse.Data.DueDate != "2026-08-29T11:30:00Z" ||
		updateResponse.Data.EstimatedMinutes == nil || *updateResponse.Data.EstimatedMinutes != 90 {
		t.Fatalf("unexpected update response: %s", updated.Body.String())
	}

	cleared := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID,
		[]byte(`{"planned_date":null,"due_date":null,"estimated_minutes":null}`),
		map[string]string{"If-Match": `"2"`},
	)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear optional fields status = %d: %s", cleared.Code, cleared.Body.String())
	}
	var clearResponse struct {
		Data struct {
			PlannedDate      *string `json:"planned_date"`
			DueDate          *string `json:"due_date"`
			EstimatedMinutes *int    `json:"estimated_minutes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(cleared.Body.Bytes(), &clearResponse); err != nil {
		t.Fatalf("decode clear response: %v", err)
	}
	if clearResponse.Data.PlannedDate != nil || clearResponse.Data.DueDate != nil || clearResponse.Data.EstimatedMinutes != nil {
		t.Fatalf("optional fields were not cleared: %s", cleared.Body.String())
	}

	statusThroughEdit := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID,
		[]byte(`{"status":"done"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if statusThroughEdit.Code != http.StatusBadRequest {
		t.Fatalf("status through edit endpoint = %d, want %d: %s", statusThroughEdit.Code, http.StatusBadRequest, statusThroughEdit.Body.String())
	}

	invalid := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+created.Data.ID,
		[]byte(`{"title":"x"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status = %d, want %d: %s", invalid.Code, http.StatusUnprocessableEntity, invalid.Body.String())
	}

	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+created.Data.ID,
		nil,
		map[string]string{"If-Match": `"3"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/tasks/"+created.Data.ID, nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted task status = %d, want %d: %s", missing.Code, http.StatusNotFound, missing.Body.String())
	}
}

func TestOriginWhitelistRejectsBrowserRequests(t *testing.T) {
	router := newTestAPI(t)
	recorder := performRequest(router, http.MethodGet, "/health", nil, map[string]string{
		"Origin": "https://evil.example",
	})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %d: %s", recorder.Code, recorder.Body.String())
	}

	recorder = performRequest(router, http.MethodGet, "/health", nil, map[string]string{
		"Sec-Fetch-Mode": "cors",
		"Sec-Fetch-Site": "cross-site",
	})
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("missing browser origin status = %d: %s", recorder.Code, recorder.Body.String())
	}

	recorder = performRequest(router, http.MethodGet, "/health", nil, map[string]string{
		"Origin": "tauri://localhost",
	})
	if recorder.Code != http.StatusOK || recorder.Header().Get("Access-Control-Allow-Origin") != "tauri://localhost" {
		t.Fatalf("allowed origin status = %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}
