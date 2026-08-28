package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

type searchTestResponse struct {
	Data []searchResultResponse `json:"data"`
	Meta pageMeta               `json:"meta"`
}

func newSearchTestAPI(t *testing.T) (*Router, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router, store
}

func decodeSearchResponse(t *testing.T, responseBody []byte) searchTestResponse {
	t.Helper()
	var response searchTestResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode search response: %v\n%s", err, responseBody)
	}
	return response
}

func seedSearchFixtures(t *testing.T, store *database.Store) {
	t.Helper()
	statements := []string{
		`INSERT INTO clients (id, name, contact_name, email, phone, status, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000101', 'Atlas 客户', 'Alice Atlas', 'atlas@example.com', '13800000000', 'active', '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z'),
		 ('00000000-0000-4000-8000-000000000102', '归档项目客户', NULL, NULL, NULL, 'inactive', '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')`,
		`INSERT INTO projects (id, name, description, client_id, status, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000201', 'Atlas Migration', '统一迁移计划', '00000000-0000-4000-8000-000000000101', 'in_progress', '2026-08-28T09:00:00Z', '2026-08-28T09:00:00Z'),
		 ('00000000-0000-4000-8000-000000000202', 'Atlas Archived', '不可执行项目', '00000000-0000-4000-8000-000000000102', 'archived', '2026-08-28T10:00:00Z', '2026-08-28T10:00:00Z')`,
		`INSERT INTO tasks (id, title, description, status, priority, project_id, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000301', 'Atlas', '精确任务', 'todo', 'P1', '00000000-0000-4000-8000-000000000201', '2026-08-28T11:00:00Z', '2026-08-28T11:00:00Z'),
		 ('00000000-0000-4000-8000-000000000302', '检查通配符 100%_done', 'literal marker', 'todo', 'P2', NULL, '2026-08-28T07:00:00Z', '2026-08-28T07:00:00Z')`,
		`INSERT INTO inbox_items (id, kind, title, summary, source_entity_type, priority, status, resolution_policy, payload_json, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000401', 'manual', '迁移提醒', '跟进 Atlas 迁移', 'manual', 'P2', 'open', 'manual', '{}', '2026-08-28T12:00:00Z', '2026-08-28T12:00:00Z'),
		 ('00000000-0000-4000-8000-000000000402', 'manual', '普通事项', '不包含目标词', 'manual', 'P2', 'open', 'manual', '{}', '2026-08-28T06:00:00Z', '2026-08-28T06:00:00Z')`,
		`INSERT INTO inbox_items (id, kind, title, summary, source_entity_type, priority, status, resolution_policy, triaged_at,
		 resolved_by_actor_id, resolved_at, resolution_reason, resolution_mode, payload_json, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000403', 'manual', '已完成 Atlas', '不再显示', 'manual', 'P2', 'resolved', 'manual', '2026-08-28T05:00:00Z',
		 '00000000-0000-5000-8000-000000000001', '2026-08-28T05:00:00Z', '已处理', 'manual', '{}', '2026-08-28T05:00:00Z', '2026-08-28T05:00:00Z')`,
	}
	for _, statement := range statements {
		if err := store.DB.Exec(statement).Error; err != nil {
			t.Fatalf("seed search fixture: %v", err)
		}
	}
}

func TestUnifiedSearchReturnsLiveResourcesWithStableRoutes(t *testing.T) {
	router, store := newSearchTestAPI(t)
	seedSearchFixtures(t, store)

	response := performRequest(router, http.MethodGet, "/api/v1/search?q=Atlas&page=1&page_size=20", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", response.Code, response.Body.String())
	}
	result := decodeSearchResponse(t, response.Body.Bytes())
	if result.Meta != (pageMeta{Page: 1, PageSize: 20, Total: 4}) {
		t.Fatalf("meta = %#v", result.Meta)
	}
	if len(result.Data) != 4 || result.Data[0].ResourceType != "task" || result.Data[0].Title != "Atlas" {
		t.Fatalf("unexpected relevance order: %#v", result.Data)
	}

	byType := make(map[string]searchResultResponse, len(result.Data))
	for _, item := range result.Data {
		byType[item.ResourceType] = item
		if !strings.HasSuffix(item.Route, item.ResourceID) {
			t.Fatalf("route does not locate resource: %#v", item)
		}
		if item.UpdatedAt == "" || len(item.MatchedFields) == 0 {
			t.Fatalf("incomplete result: %#v", item)
		}
	}
	if got := byType["project"].Route; got != "/projects/00000000-0000-4000-8000-000000000201" {
		t.Fatalf("project route = %q", got)
	}
	if !reflect.DeepEqual(byType["inbox_item"].MatchedFields, []string{"summary"}) {
		t.Fatalf("inbox matched fields = %#v", byType["inbox_item"].MatchedFields)
	}
	if _, exists := byType["project"+"-archived"]; exists {
		t.Fatal("archived project was returned")
	}
}

func TestUnifiedSearchSupportsTypeFiltersPaginationAndLiteralWildcards(t *testing.T) {
	router, store := newSearchTestAPI(t)
	seedSearchFixtures(t, store)

	filtered := performRequest(router, http.MethodGet, "/api/v1/search?q=Atlas&types=client,project&types=project&page_size=1", nil, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered status = %d: %s", filtered.Code, filtered.Body.String())
	}
	first := decodeSearchResponse(t, filtered.Body.Bytes())
	if first.Meta.Total != 2 || len(first.Data) != 1 {
		t.Fatalf("filtered first page = %#v", first)
	}
	secondResponse := performRequest(router, http.MethodGet, "/api/v1/search?q=Atlas&types=client,project&page=2&page_size=1", nil, nil)
	second := decodeSearchResponse(t, secondResponse.Body.Bytes())
	if len(second.Data) != 1 || second.Data[0].ResourceID == first.Data[0].ResourceID ||
		first.Data[0].ResourceType == second.Data[0].ResourceType {
		t.Fatalf("filtered second page = %#v", second)
	}

	literalQuery := url.QueryEscape("100%_done")
	literal := performRequest(router, http.MethodGet, "/api/v1/search?q="+literalQuery+"&types=task", nil, nil)
	literalResult := decodeSearchResponse(t, literal.Body.Bytes())
	if literal.Code != http.StatusOK || literalResult.Meta.Total != 1 || literalResult.Data[0].Title != "检查通配符 100%_done" {
		t.Fatalf("literal wildcard result = %d %#v", literal.Code, literalResult)
	}
}

func TestUnifiedSearchValidatesQueryTypesAndPagination(t *testing.T) {
	router, _ := newSearchTestAPI(t)
	tests := []string{
		"/api/v1/search",
		"/api/v1/search?q=%20%20",
		"/api/v1/search?q=ok&types=invoice",
		"/api/v1/search?q=ok&types=",
		"/api/v1/search?q=ok&page=0",
		"/api/v1/search?q=ok&page_size=101",
		"/api/v1/search?q=" + url.QueryEscape(strings.Repeat("界", 201)),
	}
	for _, path := range tests {
		response := performRequest(router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d: %s", path, response.Code, response.Body.String())
		}
	}
}

func TestUnifiedSearchReturnsEmptyPageWithoutCreatingData(t *testing.T) {
	router, store := newSearchTestAPI(t)
	response := performRequest(router, http.MethodGet, "/api/v1/search?q=不存在", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d: %s", response.Code, response.Body.String())
	}
	result := decodeSearchResponse(t, response.Body.Bytes())
	if result.Meta.Total != 0 || result.Data == nil || len(result.Data) != 0 {
		t.Fatalf("empty result = %#v", result)
	}
	for _, table := range []string{"tasks", "projects", "clients", "inbox_items"} {
		var count int64
		if err := store.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
}
