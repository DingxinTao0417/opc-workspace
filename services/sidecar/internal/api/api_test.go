package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
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
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotency-Replayed") != "true" {
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

	status := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+created.Data.ID+"/status", []byte(`{"status":"done"}`), nil)
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
