package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiContentItemsFixtureForTest(t *testing.T) (*gin.Engine, *database.Store, *aiWorkspaceTool) {
	t.Helper()
	router, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	return router, store, &aiWorkspaceTool{api: service, name: "workspace_content_items", policy: harness.NewCapabilities("work"), providerID: provider.ID, configVersion: provider.ConfigVersion}
}

type aiContentItemsPageForTest struct {
	View          string             `json:"view"`
	AsOf          string             `json:"as_of"`
	Items         []json.RawMessage  `json:"items"`
	Content       *aiContentListItem `json:"content_item"`
	Total         int64              `json:"total_items"`
	Next          *int               `json:"next_offset"`
	HasMore       bool               `json:"has_more"`
	WindowLimited bool               `json:"window_limited"`
}

func readAIContentItemsForTest(t *testing.T, tool *aiWorkspaceTool, args string) (aiContentItemsPageForTest, string) {
	t.Helper()
	encoded, err := tool.Execute(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("read %s: %v", args, err)
	}
	var page aiContentItemsPageForTest
	if err := json.Unmarshal([]byte(encoded), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || page.AsOf == "" || (page.View != "list" && page.View != "tasks") {
		t.Fatalf("invalid page=%s", encoded)
	}
	for _, private := range []string{"PRIVATE CONTENT NOTES", "PRIVATE TASK DESCRIPTION", "private.invalid", "external_link", "notes", "description", "actor_id", "result_text", "relative_path"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("metadata leaked %s", private)
		}
	}
	return page, encoded
}

func TestAIContentItemsSharedNativeFiltersAndTaskProgress(t *testing.T) {
	router, store, tool := aiContentItemsFixtureForTest(t)
	whole := createContentQueryFixtureItem(t, router, "Whole second", "2026-09-21T00:00:00Z")
	half := createContentQueryFixtureItem(t, router, "Half second", "2026-09-21T00:00:00.5Z")
	empty := createContentQueryFixtureItem(t, router, "Empty preparation", "2026-09-21T00:00:00.9Z")
	archived := createContentQueryFixtureItem(t, router, "Archived schedule", "2026-09-21T00:00:00Z")
	response := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+archived.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": "\"1\""})
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	other := createContentQueryFixtureItem(t, router, "Other platform", "2026-09-21T00:00:00Z")
	response = performRequest(router, http.MethodPatch, "/api/v1/content-items/"+other.ID, []byte(`{"platform":"Other"}`), map[string]string{"If-Match": "\"1\""})
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	link := func(item contentItemResponse, required bool, status string) contentItemResponse {
		t.Helper()
		task := createTaskForTaskFacts(t, router, `{"title":"Preparation task","description":"PRIVATE TASK DESCRIPTION"}`)
		if status == "cancelled" {
			runTaskLifecycleForParentTest(t, router, task, taskLifecycleCancel, "Cancelled by owner")
		}
		if status == "done" {
			runTaskLifecycleForParentTest(t, router, task, taskLifecycleComplete, "")
		}
		linked := performRequest(router, http.MethodPost, "/api/v1/content-items/"+item.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":%t}`, task.ID, required)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)})
		if linked.Code != http.StatusCreated {
			t.Fatal(linked.Body.String())
		}
		return decodeContentItemResponse(t, linked.Body.Bytes())
	}
	whole = link(whole, true, "cancelled")
	half = link(half, true, "done")
	half = link(half, false, "todo")
	for _, test := range []struct {
		filter, query string
		expected      int64
	}{
		{`{"platform":"Web","scheduled_from":"2026-09-21T08:00:00+08:00","scheduled_to":"2026-09-21T00:00:00.9Z"}`, `platform=Web&scheduled_from=2026-09-21T08%3A00%3A00%2B08%3A00&scheduled_to=2026-09-21T00%3A00%3A00.9Z`, 2},
		{`{"platform":"Web","task_state":"any_incomplete"}`, `platform=Web&task_state=any_incomplete`, 2},
		{`{"platform":"Web","task_state":"required_incomplete"}`, `platform=Web&task_state=required_incomplete`, 1},
		{`{"status":"archived","schedule_state":"scheduled"}`, `status=archived&schedule_state=scheduled`, 1},
		{`{"platform":"Web","include_archived":true}`, `platform=Web&include_archived=true`, 4},
	} {
		page, _ := readAIContentItemsForTest(t, tool, `{"view":"list","filters":`+test.filter+`}`)
		native := performRequest(router, http.MethodGet, "/api/v1/content-items?"+test.query, nil, nil)
		if native.Code != http.StatusOK {
			t.Fatal(native.Body.String())
		}
		items, meta := decodeContentItemListResponse(t, native.Body.Bytes())
		if page.Total != test.expected || meta.Total != page.Total || len(page.Items) != len(items) {
			t.Fatalf("native/AI count %s ai=%+v native=%+v", test.filter, page, meta)
		}
		for i, raw := range page.Items {
			var item aiContentListItem
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatal(err)
			}
			if item.ID != items[i].ID || item.Version != items[i].Version || item.RequiredTaskTotal != items[i].RequiredTaskTotal || item.RequiredTaskDone != items[i].RequiredTaskDone || item.TaskTotal != int64(len(items[i].Tasks)) || item.Route != searchRoute("content_item", item.ID) {
				t.Fatalf("native/AI item mismatch ai=%+v native=%+v", item, items[i])
			}
			if item.ID == empty.ID && (item.TaskTotal != 0 || item.TaskDone != 0) {
				t.Fatal("empty preparation counted as incomplete")
			}
		}
	}
	page, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"incomplete","required_only":true}`, whole.ID))
	var cancelled aiContentTaskItem
	if len(page.Items) != 1 {
		t.Fatalf("cancelled preparation missing=%+v", page)
	}
	if err := json.Unmarshal(page.Items[0], &cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" || cancelled.Version < 2 || !cancelled.IsRequired || cancelled.Route != searchRoute("task", cancelled.ID) || page.Content == nil || page.Content.RequiredTaskDone != 0 {
		t.Fatal("cancelled was treated as completed or metadata incomplete")
	}
	optional, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"incomplete"}`, half.ID))
	if optional.Total != 1 {
		t.Fatal("optional incomplete task omitted")
	}
	onlyRequired, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"incomplete","required_only":true}`, half.ID))
	if onlyRequired.Total != 0 || onlyRequired.Content.TaskTotal != 2 || onlyRequired.Content.RequiredTaskDone != 1 {
		t.Fatal("filtered task page corrupted full content progress")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIContentItemsRejectsAmbiguousAndCrossViewArguments(t *testing.T) {
	_, _, tool := aiContentItemsFixtureForTest(t)
	id := uuid.NewString()
	invalid := []string{`null`, `[]`, `{}`, `{"view":"list"} {}`, `{"view":"list","view":"tasks"}`, `{"View":"list"}`, `{"view":"list","vi\u0065w":"list"}`, `{"view":"unknown"}`, `{"view":"list","content_item_id":"` + id + `"}`, `{"view":"list","required_only":false}`, `{"view":"list","task_state":"all"}`, `{"view":"tasks","content_item_id":"` + id + `","filters":{}}`, `{"view":"tasks"}`, `{"view":"tasks","content_item_id":"bad"}`, `{"view":"tasks","content_item_id":"` + id + `","task_state":"required_incomplete"}`, `{"view":"list","limit":0}`, `{"view":"list","limit":21}`, `{"view":"list","offset":1001}`, `{"view":"list","offset":-1}`, `{"view":"list","offset":0.5}`, `{"view":"list","limit":"1"}`, `{"view":"list","filters":[]}`, `{"view":"list","filters":{"platform":"Web","platform":"Other"}}`, `{"view":"list","filters":{"Platform":"Web"}}`, `{"view":"list","filters":{"platform":"Web","plat\u0066orm":"Other"}}`, `{"view":"list","filters":{"platform":{}}}`, `{"view":"list","filters":{"unknown":true}}`, `{"view":"list","filters":{"include_archived":"true"}}`, `{"view":"list","filters":{"include_archived":1}}`, `{"view":"list","filters":{"status":"active"}}`, `{"view":"list","filters":{"task_state":"incomplete"}}`, `{"view":"list","filters":{"schedule_state":"unscheduled","scheduled_from":"2026-09-21T00:00:00Z"}}`}
	for _, field := range []string{"view", "filters", "content_item_id", "task_state", "required_only", "limit", "offset"} {
		prefix := `"view":"list",`
		if field == "view" {
			prefix = ""
		}
		invalid = append(invalid, `{`+prefix+fmt.Sprintf(`%q:null}`, field))
	}
	for _, field := range []string{"platform", "status", "project_id", "scheduled_from", "scheduled_to", "schedule_state", "task_state"} {
		for _, value := range []string{`null`, `""`, `" "`, `1`} {
			invalid = append(invalid, `{"view":"list","filters":{`+fmt.Sprintf(`%q:%s`, field, value)+`}}`)
		}
	}
	invalid = append(invalid, `{"view":"list","filters":{"include_archived":null}}`, `{"view":"tasks","content_item_id":"`+id+`","required_only":"true"}`, `{"view":"tasks","content_item_id":"`+id+`","task_state":""}`)
	for _, args := range invalid {
		if result, err := tool.Execute(context.Background(), []byte(args)); err == nil || result != "" {
			t.Fatalf("accepted %s: %s %v", args, result, err)
		}
	}
}

func TestAIContentItemsScopeProviderRestoreCancellationAndNoWrites(t *testing.T) {
	router, store, tool := aiContentItemsFixtureForTest(t)
	item := createContentQueryFixtureItem(t, router, "Protected metadata", "")
	response := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+item.ID, []byte(`{"notes":"PRIVATE CONTENT NOTES","external_link":"https://private.invalid/token"}`), map[string]string{"If-Match": "\"1\""})
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var before int64
	if err := store.DB.Table("workflow_events").Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{nil, {"outputs"}, {"actions"}, {"clients"}} {
		tool.policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(`{"view":"list"}`)); !errors.Is(err, harness.ErrPermissionDenied) {
			t.Fatalf("scope %v err=%v", scopes, err)
		}
	}
	tool.policy = harness.NewCapabilities("work")
	readAIContentItemsForTest(t, tool, `{"view":"list"}`)
	readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q}`, item.ID))
	if result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"tasks","content_item_id":%q}`, uuid.NewString()))); err == nil || result != "" || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("missing content=%s %v", result, err)
	}
	tool.api.restorePending.Store(true)
	if _, err := tool.Execute(context.Background(), []byte(`{"view":"list"}`)); err == nil {
		t.Fatal("read during restore")
	}
	tool.api.restorePending.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, []byte(`{"view":"list"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if err := store.DB.Model(&models.AIProvider{}).Where("id=?", tool.providerID).Updates(map[string]any{"config_version": gorm.Expr("config_version + 1"), "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"view":"list"}`)); err == nil {
		t.Fatal("expired provider grant read")
	}
	tool.configVersion++
	readAIContentItemsForTest(t, tool, `{"view":"list"}`)
	if err := store.DB.Model(&models.AIProvider{}).Where("id=?", tool.providerID).Updates(map[string]any{"status": "disabled", "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"view":"list"}`)); err == nil {
		t.Fatal("disabled provider read")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events", before)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func seedAIContentTasksForTest(t *testing.T, store *database.Store, itemID string, count int, title string) []models.Task {
	t.Helper()
	tasks := make([]models.Task, count)
	relations := make([]models.ContentItemTask, count)
	for i := range tasks {
		task, err := taskFromCreateRequest(createTaskRequest{Title: title})
		if err != nil {
			t.Fatal(err)
		}
		task.Description = "PRIVATE TASK DESCRIPTION"
		task.Version = int64(i + 1)
		tasks[i] = task
		relations[i] = models.ContentItemTask{ContentItemID: itemID, TaskID: task.ID, IsRequired: i%2 == 0, LinkedAt: fmt.Sprintf("2026-09-21T00:%02d:%02dZ", i/60, i%60)}
	}
	if err := store.DB.CreateInBatches(&tasks, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.CreateInBatches(&relations, 100).Error; err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestAIContentItemsTasksBeyondFiftyAndBoundedWindows(t *testing.T) {
	router, store, tool := aiContentItemsFixtureForTest(t)
	item := createContentQueryFixtureItem(t, router, "Large preparation list", "")
	tasks := seedAIContentTasksForTest(t, store, item.ID, 1022, strings.Repeat("界", 200))
	seen := map[string]bool{}
	for _, offset := range []int{0, 20, 40} {
		page, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"limit":20,"offset":%d}`, item.ID, offset))
		if len(page.Items) != 20 || page.Total != 1022 || page.Content == nil || page.Content.TaskTotal != 1022 || page.Content.RequiredTaskTotal != 511 || page.Next == nil || *page.Next != offset+20 {
			t.Fatalf("task page=%+v", page)
		}
		for index, raw := range page.Items {
			var task aiContentTaskItem
			if err := json.Unmarshal(raw, &task); err != nil {
				t.Fatal(err)
			}
			if task.ID != tasks[offset+index].ID || task.Version != int64(offset+index+1) || utf8.RuneCountInString(task.Title) != 200 || seen[task.ID] {
				t.Fatalf("wrong exact task beyond former limit=%+v", task)
			}
			seen[task.ID] = true
		}
	}
	last, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"limit":20,"offset":1000}`, item.ID))
	if last.Total != 1022 || len(last.Items) != 20 || !last.HasMore || last.Next != nil || !last.WindowLimited {
		t.Fatalf("task window=%+v", last)
	}
	required, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"required_only":true,"limit":1}`, item.ID))
	if required.Total != 511 {
		t.Fatal("task filtering happened after pagination")
	}
	contents := make([]models.ContentItem, 1021)
	for i := range contents {
		contents[i] = models.ContentItem{ID: uuid.NewString(), Title: "Another scheduled entry", Platform: "Web", Status: "draft", Version: 1, CreatedAt: "2026-09-21T00:00:00Z", UpdatedAt: "2026-09-21T00:00:00Z"}
	}
	if err := store.DB.CreateInBatches(&contents, 100).Error; err != nil {
		t.Fatal(err)
	}
	listed, _ := readAIContentItemsForTest(t, tool, `{"view":"list","limit":20,"offset":1000}`)
	if listed.Total != 1022 || len(listed.Items) != 20 || !listed.HasMore || listed.Next != nil || !listed.WindowLimited {
		t.Fatalf("list window=%+v", listed)
	}
	filtered, _ := readAIContentItemsForTest(t, tool, `{"view":"list","filters":{"task_state":"required_incomplete"}}`)
	if filtered.Total != 1 || len(filtered.Items) != 1 {
		t.Fatal("list progress filter applied after window")
	}
}

func TestAIContentItemsEncodedBudgetAndSQLMetadataOnly(t *testing.T) {
	router, store, tool := aiContentItemsFixtureForTest(t)
	private, link := "PRIVATE CONTENT NOTES", "https://private.invalid/token"
	platform := strings.Repeat("&", 64)
	contents := make([]models.ContentItem, 20)
	for i := range contents {
		contents[i] = models.ContentItem{ID: uuid.NewString(), Title: strings.Repeat("&", 200), Platform: platform, Status: "draft", Notes: &private, ExternalLink: &link, Version: 1, CreatedAt: "2026-09-21T00:00:00Z", UpdatedAt: "2026-09-21T00:00:00Z"}
	}
	if err := store.DB.CreateInBatches(&contents, 20).Error; err != nil {
		t.Fatal(err)
	}
	seedAIContentTasksForTest(t, store, contents[0].ID, 20, strings.Repeat("&", 200))
	for _, args := range []string{`{"view":"list","limit":20}`, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"limit":20}`, contents[0].ID)} {
		result, err := tool.Execute(context.Background(), []byte(args))
		if err == nil || result != "" || !strings.Contains(err.Error(), "result too large") || strings.Contains(err.Error(), private) {
			t.Fatalf("oversized result exposed/truncated: %s %v", result, err)
		}
	}
	var selects []string
	const callback = "test:content_metadata_projection"
	observe := func(tx *gorm.DB) { selects = append(selects, strings.ToLower(tx.Statement.SQL.String())) }
	if err := store.DB.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	page, _ := readAIContentItemsForTest(t, tool, `{"view":"list","limit":1}`)
	taskPage, _ := readAIContentItemsForTest(t, tool, fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"limit":1}`, contents[0].ID))
	if page.Total != 20 || len(page.Items) != 1 || !page.HasMore || taskPage.Total != 20 || !taskPage.HasMore {
		t.Fatal("narrowing lost pagination")
	}
	_ = store.DB.Callback().Query().Remove(callback)
	_ = store.DB.Callback().Row().Remove(callback)
	for _, query := range selects {
		if strings.Contains(query, "notes") || strings.Contains(query, "external_link") || strings.Contains(query, "description") || strings.Contains(query, "select *") {
			t.Fatalf("metadata SQL loaded unbounded/private columns: %s", query)
		}
	}
	if len(selects) < 5 {
		t.Fatal("metadata projection query spy incomplete")
	}
	native := performRequest(router, http.MethodGet, "/api/v1/content-items?platform="+url.QueryEscape(platform)+"&page_size=100", nil, nil)
	items, meta := decodeContentItemListResponse(t, native.Body.Bytes())
	if native.Code != http.StatusOK || len(items) != 20 || meta.Total != 20 {
		t.Fatalf("AI cap changed native list: %d count=%d", native.Code, len(items))
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
