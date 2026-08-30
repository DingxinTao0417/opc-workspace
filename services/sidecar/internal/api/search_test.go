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
		`INSERT INTO invoices (id, invoice_number, client_id, project_id, amount_minor, currency, status, issue_date, due_date, notes, version, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000501', 'INV-2026-001', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000201', 128045, 'CNY', 'overdue', '2026-08-01', '2026-08-20', 'invoice-private-token', 1, '2026-08-28T13:00:00Z', '2026-08-28T13:00:00Z'),
		 ('00000000-0000-4000-8000-000000000502', '字面 100%_done\qa', '00000000-0000-4000-8000-000000000102', NULL, 8800, 'CNY', 'draft', '2026-08-01', '2026-09-30', '', 1, '2026-08-28T14:00:00Z', '2026-08-28T14:00:00Z'),
		 ('00000000-0000-4000-8000-000000000503', 'Hidden terminal', '00000000-0000-4000-8000-000000000102', NULL, 9900, 'CNY', 'draft', '2026-08-01', '2026-09-30', '', 1, '2026-08-28T15:00:00Z', '2026-08-28T15:00:00Z')`,
		`INSERT INTO roadmap_milestones (id, title, description, year, quarter, target_date, status, manual_order, archived_from_status, version, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000601', '第三季度交付', '统一升级说明', 2026, 3, '2026-09-15', 'active', 1024, NULL, 1, '2026-08-28T13:00:00Z', '2026-08-28T13:00:00Z'),
		 ('00000000-0000-4000-8000-000000000602', '路线 100%_done\qa', '字面字符路线', 2026, 3, '2026-09-16', 'achieved', 2048, NULL, 1, '2026-08-28T14:00:00Z', '2026-08-28T14:00:00Z'),
		 ('00000000-0000-4000-8000-000000000603', 'Hidden terminal', '已归档路线', 2026, 3, '2026-09-17', 'archived', 3072, 'planned', 1, '2026-08-28T15:00:00Z', '2026-08-28T15:00:00Z')`,
		`INSERT INTO roadmap_milestone_projects (milestone_id, project_id, linked_at) VALUES
		 ('00000000-0000-4000-8000-000000000601', '00000000-0000-4000-8000-000000000201', '2026-08-28T13:00:00Z'),
		 ('00000000-0000-4000-8000-000000000601', '00000000-0000-4000-8000-000000000202', '2026-08-28T13:00:00Z')`,
		`INSERT INTO content_items (id, title, platform, status, notes, external_link, manual_order, archived_from_status, version, created_at, updated_at) VALUES
		 ('00000000-0000-4000-8000-000000000701', '发布预告', '公众号', 'in_review', 'Atlas 内容说明', 'https://example.com/content-private-token', 1024, NULL, 1, '2026-08-28T13:00:00Z', '2026-08-28T13:00:00Z'),
		 ('00000000-0000-4000-8000-000000000702', '内容 100%_done\qa', '视频号', 'cancelled', '字面字符内容', NULL, 2048, NULL, 1, '2026-08-28T14:00:00Z', '2026-08-28T14:00:00Z'),
		 ('00000000-0000-4000-8000-000000000703', 'Hidden terminal', '公众号', 'archived', '已归档内容', NULL, 3072, 'draft', 1, '2026-08-28T15:00:00Z', '2026-08-28T15:00:00Z'),
		 ('00000000-0000-4000-8000-000000000704', '内容 100XXdoneqa', '公众号', 'draft', '未包含字面特殊字符', NULL, 4096, NULL, 1, '2026-08-28T14:00:00Z', '2026-08-28T14:00:00Z')`,
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
	if result.Meta != (pageMeta{Page: 1, PageSize: 20, Total: 7}) {
		t.Fatalf("meta = %#v", result.Meta)
	}
	if len(result.Data) != 7 || result.Data[0].ResourceType != "task" || result.Data[0].Title != "Atlas" {
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
	if invoice := byType["invoice"]; invoice.Route != "/invoices/00000000-0000-4000-8000-000000000501" || invoice.Subtitle != "Atlas 客户" || invoice.Status != "overdue" || !reflect.DeepEqual(invoice.MatchedFields, []string{"client_name"}) {
		t.Fatalf("invoice result = %#v", invoice)
	}
	if milestone := byType["roadmap_milestone"]; milestone.Route != "/roadmap?milestone=00000000-0000-4000-8000-000000000601" || milestone.Subtitle != "Atlas Archived · Atlas Migration" || milestone.Status != "active" || !reflect.DeepEqual(milestone.MatchedFields, []string{"project_names"}) {
		t.Fatalf("roadmap milestone result = %#v", milestone)
	}
	if content := byType["content_item"]; content.Route != "/content-calendar?item=00000000-0000-4000-8000-000000000701" || content.Subtitle != "公众号" || content.Status != "in_review" || !reflect.DeepEqual(content.MatchedFields, []string{"notes"}) {
		t.Fatalf("content item result = %#v", content)
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

func TestUnifiedSearchSupportsNewTypeFiltersLiteralWildcardsAndStableOrdering(t *testing.T) {
	router, store := newSearchTestAPI(t)
	seedSearchFixtures(t, store)

	literalQuery := url.QueryEscape(`100%_done\qa`)
	path := "/api/v1/search?q=" + literalQuery + "&types=invoice,roadmap_milestone,content_item&page_size=2"
	firstResponse := performRequest(router, http.MethodGet, path, nil, nil)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("new type search status = %d: %s", firstResponse.Code, firstResponse.Body.String())
	}
	first := decodeSearchResponse(t, firstResponse.Body.Bytes())
	if first.Meta != (pageMeta{Page: 1, PageSize: 2, Total: 3}) || len(first.Data) != 2 {
		t.Fatalf("new type first page = %#v", first)
	}
	if first.Data[0].ResourceType != "content_item" || first.Data[1].ResourceType != "invoice" {
		t.Fatalf("stable first page order = %#v", first.Data)
	}
	if first.Data[0].Status != "cancelled" || first.Data[1].Status != "draft" {
		t.Fatalf("new type statuses = %#v", first.Data)
	}

	secondResponse := performRequest(router, http.MethodGet, path+"&page=2", nil, nil)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("new type second page status = %d: %s", secondResponse.Code, secondResponse.Body.String())
	}
	second := decodeSearchResponse(t, secondResponse.Body.Bytes())
	if second.Meta != (pageMeta{Page: 2, PageSize: 2, Total: 3}) || len(second.Data) != 1 || second.Data[0].ResourceType != "roadmap_milestone" || second.Data[0].Status != "achieved" {
		t.Fatalf("stable second page order = %#v", second)
	}

	repeated := decodeSearchResponse(t, performRequest(router, http.MethodGet, path, nil, nil).Body.Bytes())
	if !reflect.DeepEqual(first, repeated) {
		t.Fatalf("search order changed between identical requests:\nfirst=%#v\nrepeated=%#v", first, repeated)
	}
}

func TestUnifiedSearchMatchesEverySupportedNewResourceField(t *testing.T) {
	router, store := newSearchTestAPI(t)
	seedSearchFixtures(t, store)

	tests := []struct {
		name          string
		query         string
		resourceType  string
		matchedFields []string
	}{
		{name: "invoice number", query: "INV-2026-001", resourceType: "invoice", matchedFields: []string{"invoice_number"}},
		{name: "invoice client", query: "Atlas 客户", resourceType: "invoice", matchedFields: []string{"client_name"}},
		{name: "milestone title", query: "第三季度交付", resourceType: "roadmap_milestone", matchedFields: []string{"title"}},
		{name: "milestone description", query: "统一升级说明", resourceType: "roadmap_milestone", matchedFields: []string{"description"}},
		{name: "milestone project summary", query: "Atlas Migration", resourceType: "roadmap_milestone", matchedFields: []string{"project_names"}},
		{name: "content title", query: "发布预告", resourceType: "content_item", matchedFields: []string{"title"}},
		{name: "content notes", query: "Atlas 内容说明", resourceType: "content_item", matchedFields: []string{"notes"}},
		{name: "content platform", query: "视频号", resourceType: "content_item", matchedFields: []string{"platform"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := "/api/v1/search?q=" + url.QueryEscape(test.query) + "&types=" + test.resourceType
			response := performRequest(router, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("search status = %d: %s", response.Code, response.Body.String())
			}
			result := decodeSearchResponse(t, response.Body.Bytes())
			if result.Meta.Total != 1 || len(result.Data) != 1 || result.Data[0].ResourceType != test.resourceType || !reflect.DeepEqual(result.Data[0].MatchedFields, test.matchedFields) {
				t.Fatalf("field search result = %#v", result)
			}
		})
	}

	excludedFields := []struct {
		name         string
		query        string
		resourceType string
	}{
		{name: "invoice project name", query: "Atlas Migration", resourceType: "invoice"},
		{name: "invoice notes", query: "invoice-private-token", resourceType: "invoice"},
		{name: "content external link", query: "content-private-token", resourceType: "content_item"},
	}
	for _, test := range excludedFields {
		t.Run("does not search "+test.name, func(t *testing.T) {
			path := "/api/v1/search?q=" + url.QueryEscape(test.query) + "&types=" + test.resourceType
			response := performRequest(router, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("search status = %d: %s", response.Code, response.Body.String())
			}
			result := decodeSearchResponse(t, response.Body.Bytes())
			if result.Meta.Total != 0 || len(result.Data) != 0 {
				t.Fatalf("private or unsupported field leaked into search: %#v", result)
			}
		})
	}
}

func TestUnifiedSearchExcludesArchivedAndHardDeletedNewResources(t *testing.T) {
	router, store := newSearchTestAPI(t)
	seedSearchFixtures(t, store)

	hiddenPath := "/api/v1/search?q=" + url.QueryEscape("Hidden terminal") + "&types=invoice,roadmap_milestone,content_item"
	beforeDeleteResponse := performRequest(router, http.MethodGet, hiddenPath, nil, nil)
	if beforeDeleteResponse.Code != http.StatusOK {
		t.Fatalf("search before delete status = %d: %s", beforeDeleteResponse.Code, beforeDeleteResponse.Body.String())
	}
	beforeDelete := decodeSearchResponse(t, beforeDeleteResponse.Body.Bytes())
	if beforeDelete.Meta.Total != 1 || len(beforeDelete.Data) != 1 || beforeDelete.Data[0].ResourceType != "invoice" {
		t.Fatalf("archived resources must remain hidden while live invoice is visible: %#v", beforeDelete)
	}
	if err := store.DB.Exec("DELETE FROM invoices WHERE id = ?", "00000000-0000-4000-8000-000000000503").Error; err != nil {
		t.Fatalf("hard delete invoice fixture: %v", err)
	}
	afterDeleteResponse := performRequest(router, http.MethodGet, hiddenPath, nil, nil)
	if afterDeleteResponse.Code != http.StatusOK {
		t.Fatalf("search after delete status = %d: %s", afterDeleteResponse.Code, afterDeleteResponse.Body.String())
	}
	afterDelete := decodeSearchResponse(t, afterDeleteResponse.Body.Bytes())
	if afterDelete.Meta.Total != 0 || len(afterDelete.Data) != 0 {
		t.Fatalf("archived or hard-deleted resources leaked into search: %#v", afterDelete)
	}
}

func TestUnifiedSearchValidatesQueryTypesAndPagination(t *testing.T) {
	router, _ := newSearchTestAPI(t)
	tests := []string{
		"/api/v1/search",
		"/api/v1/search?q=%20%20",
		"/api/v1/search?q=ok&types=financial_entry",
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
	for _, table := range []string{"tasks", "projects", "clients", "inbox_items", "invoices", "roadmap_milestones", "content_items"} {
		var count int64
		if err := store.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}
}
