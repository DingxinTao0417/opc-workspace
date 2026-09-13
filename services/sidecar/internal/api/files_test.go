package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newFilesTestRouter(t *testing.T) (*database.Store, http.Handler) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "files.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), KeyStore: keystore.NewMemoryStore(),
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return store, router.Engine
}

func seedControlledFiles(t *testing.T, store *database.Store) {
	t.Helper()
	statements := []string{
		`INSERT INTO clients (id, name) VALUES ('018f0000-0000-4000-8000-00000000c101', '示例客户')`,
		`INSERT INTO projects (id, name) VALUES ('018f0000-0000-4000-8000-000000000101', '示例项目')`,
		`INSERT INTO client_attachments (
			id, client_id, name, relative_path, mime_type, size_bytes, sha256,
			recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES (
			'018f0000-0000-4000-8000-00000000ca01', '018f0000-0000-4000-8000-00000000c101',
			'合同.pdf', 'objects/018f0000-0000-4000-8000-00000000ca01', 'application/pdf', 128,
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'` + models.BuiltinOwnerActorID + `', 'verified', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z'
		)`,
		`INSERT INTO project_attachments (
			id, project_id, name, relative_path, mime_type, size_bytes, sha256,
			recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES (
			'018f0000-0000-4000-8000-00000000ba01', '018f0000-0000-4000-8000-000000000101',
			'设计稿.png', 'objects/018f0000-0000-4000-8000-00000000ba01', 'image/png', 64,
			'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			'` + models.BuiltinOwnerActorID + `', 'verified', '2026-09-02T00:00:00Z', '2026-09-02T00:00:00Z'
		)`,
	}
	for _, statement := range statements {
		if err := store.DB.Exec(statement).Error; err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
}

func TestListControlledFilesRequiresAuthAndValidatesScope(t *testing.T) {
	_, router := newFilesTestRouter(t)

	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, unauthenticated)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated files = %d", recorder.Code)
	}

	if response := performRequest(router, http.MethodGet, "/api/v1/files?scope=bogus", nil, nil); response.Code != http.StatusBadRequest ||
		responseErrorCode(t, response.Body.Bytes()) != "INVALID_FILE_SCOPE" {
		t.Fatalf("invalid scope = %d %s", response.Code, response.Body.String())
	}
	if response := performRequest(router, http.MethodGet, "/api/v1/files?page_size=0", nil, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid page_size = %d", response.Code)
	}
}

func TestListControlledFilesReturnsReadOnlyUnion(t *testing.T) {
	store, router := newFilesTestRouter(t)

	empty := performRequest(router, http.MethodGet, "/api/v1/files", nil, nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty files = %d %s", empty.Code, empty.Body.String())
	}
	var emptyBody struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyBody); err != nil || len(emptyBody.Data) != 0 || emptyBody.Meta.Total != 0 {
		t.Fatalf("empty files body = %s err=%v", empty.Body.String(), err)
	}

	seedControlledFiles(t, store)

	list := performRequest(router, http.MethodGet, "/api/v1/files", nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("files = %d %s", list.Code, list.Body.String())
	}
	raw := list.Body.String()
	var body struct {
		Data []controlledFileResponse `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if body.Meta.Total != 2 || len(body.Data) != 2 {
		t.Fatalf("files total = %d items = %d", body.Meta.Total, len(body.Data))
	}
	// Newest first by updated_at, and bodies/paths never leak.
	if body.Data[0].Scope != "project_attachment" || body.Data[1].Scope != "client_attachment" {
		t.Fatalf("files order = %#v", body.Data)
	}
	if body.Data[1].ContentRoute != "/api/v1/client-attachments/018f0000-0000-4000-8000-00000000ca01/content" {
		t.Fatalf("client content route = %q", body.Data[1].ContentRoute)
	}
	if body.Data[1].OwnerLabel != "示例客户" {
		t.Fatalf("owner label = %q", body.Data[1].OwnerLabel)
	}
	for _, forbidden := range []string{"content_text", "relative_path", "objects/"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("files response leaks %q", forbidden)
		}
	}

	scoped := performRequest(router, http.MethodGet, "/api/v1/files?scope=client_attachment", nil, nil)
	if scoped.Code != http.StatusOK {
		t.Fatalf("scoped files = %d", scoped.Code)
	}
	var scopedBody struct {
		Data []controlledFileResponse `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(scoped.Body.Bytes(), &scopedBody); err != nil || scopedBody.Meta.Total != 1 ||
		scopedBody.Data[0].Scope != "client_attachment" {
		t.Fatalf("scoped files = %s err=%v", scoped.Body.String(), err)
	}
}

func TestListAgentRunsValidatesAndPagesGlobalLedger(t *testing.T) {
	_, router := newFilesTestRouter(t)

	if response := performRequest(router, http.MethodGet, "/api/v1/agent-runs?status=bogus", nil, nil); response.Code != http.StatusBadRequest ||
		responseErrorCode(t, response.Body.Bytes()) != "INVALID_AGENT_RUN_STATUS" {
		t.Fatalf("invalid status = %d %s", response.Code, response.Body.String())
	}
	if response := performRequest(router, http.MethodGet, "/api/v1/agent-runs?page=0", nil, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid page = %d", response.Code)
	}

	empty := performRequest(router, http.MethodGet, "/api/v1/agent-runs", nil, nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("agent runs = %d %s", empty.Code, empty.Body.String())
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &body); err != nil || len(body.Data) != 0 || body.Meta.Total != 0 {
		t.Fatalf("empty agent runs = %s err=%v", empty.Body.String(), err)
	}
	// The global ledger must never expose executor input snapshots.
	if strings.Contains(empty.Body.String(), "input_snapshot_json") {
		t.Fatalf("agent runs leaked input snapshot")
	}
}
