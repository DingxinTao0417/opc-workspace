package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func createContentQueryFixtureItem(t *testing.T, router *gin.Engine, title, scheduledAt string) contentItemResponse {
	t.Helper()
	input := fmt.Sprintf(`{"title":%q,"platform":"Web"}`, title)
	if scheduledAt != "" {
		input = fmt.Sprintf(`{"title":%q,"platform":"Web","scheduled_at":%q,"scheduled_timezone":"UTC"}`, title, scheduledAt)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(input), nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create content=%d %s", response.Code, response.Body.String())
	}
	return decodeContentItemResponse(t, response.Body.Bytes())
}

func TestContentItemQueryTaskStateUsesCurrentSharedProgress(t *testing.T) {
	router, store := newProjectTestAPI(t)
	items := map[string]contentItemResponse{}
	for _, name := range []string{"empty", "done", "optional", "required", "cancelled", "mixed"} {
		items[name] = createContentQueryFixtureItem(t, router, name, "2026-09-21T00:00:00Z")
	}
	link := func(name, status string, required bool) {
		t.Helper()
		task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":%q}`, name+" preparation"))
		if status == "done" {
			runTaskLifecycleForParentTest(t, router, task, taskLifecycleComplete, "")
		}
		if status == "cancelled" {
			runTaskLifecycleForParentTest(t, router, task, taskLifecycleCancel, "No longer proceeding")
		}
		item := items[name]
		response := performRequest(router, http.MethodPost, "/api/v1/content-items/"+item.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":%t}`, task.ID, required)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)})
		if response.Code != http.StatusCreated {
			t.Fatalf("link=%d %s", response.Code, response.Body.String())
		}
		items[name] = decodeContentItemResponse(t, response.Body.Bytes())
	}
	link("done", "done", true)
	link("optional", "todo", false)
	link("required", "todo", true)
	link("cancelled", "cancelled", true)
	link("mixed", "done", true)
	link("mixed", "todo", false)
	for _, test := range []struct {
		state string
		names []string
	}{
		{"all", []string{"empty", "done", "optional", "required", "cancelled", "mixed"}},
		{"any_incomplete", []string{"optional", "required", "cancelled", "mixed"}},
		{"required_incomplete", []string{"required", "cancelled"}},
	} {
		t.Run(test.state, func(t *testing.T) {
			response := performRequest(router, http.MethodGet, "/api/v1/content-items?platform=Web&schedule_state=scheduled&task_state="+test.state, nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("list=%d %s", response.Code, response.Body.String())
			}
			listed, meta := decodeContentItemListResponse(t, response.Body.Bytes())
			if len(listed) != len(test.names) || meta.Total != int64(len(test.names)) {
				t.Fatalf("list size=%d meta=%+v", len(listed), meta)
			}
			seen := contentItemIDs(listed)
			for _, name := range test.names {
				if _, ok := seen[items[name].ID]; !ok {
					t.Fatalf("missing %s", name)
				}
			}
		})
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	progress, err := loadContentItemTaskProgressByIDs(store.DB, ids)
	if err != nil {
		t.Fatal(err)
	}
	for name, item := range items {
		row := progress[item.ID]
		if row.RequiredTaskTotal != item.RequiredTaskTotal || row.RequiredTaskDone != item.RequiredTaskDone || row.TaskTotal != int64(len(item.Tasks)) {
			t.Fatalf("shared/native %s progress=%+v native=%+v", name, row, item)
		}
	}
	if progress[items["empty"].ID] != (contentItemTaskProgress{}) || progress[items["mixed"].ID].TaskTotal != 2 || progress[items["mixed"].ID].TaskDone != 1 || progress[items["cancelled"].ID].RequiredTaskDone != 0 {
		t.Fatalf("progress=%+v", progress)
	}
	// Completing a linked Task does not change its Content Item version. The
	// predicate and counters must nevertheless read the new Task fact.
	item := items["required"]
	task := getTaskForTaskFacts(t, router, item.Tasks[0].ID)
	runTaskLifecycleForParentTest(t, router, task, taskLifecycleComplete, "")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=?", 1, item.ID, item.Version)
	response := performRequest(router, http.MethodGet, "/api/v1/content-items?task_state=required_incomplete", nil, nil)
	listed, meta := decodeContentItemListResponse(t, response.Body.Bytes())
	if response.Code != http.StatusOK || len(listed) != 1 || listed[0].ID != items["cancelled"].ID || meta.Total != 1 {
		t.Fatalf("stale task facts=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestContentItemQueryParserPreservesNativeCompatibilityAndRejectsConflicts(t *testing.T) {
	from := "2026-09-21T08:00:00+08:00"
	to := "2026-09-21T00:00:00.000000001Z"
	projectID := "ABCDEFAB-ABCD-4ABC-8ABC-ABCDEFABCDEF"
	filters, err := parseContentItemQueryFilters(url.Values{"scheduled_from": {from}, "scheduled_to": {to}, "platform": {" Web "}, "status": {" draft "}, "project_id": {projectID}, "include_archived": {"1"}})
	if err != nil || filters.Platform != "Web" || filters.Status != "draft" || filters.ProjectID != strings.ToLower(projectID) || !filters.IncludeArchived || filters.TaskState != "all" || filters.ScheduledFrom == nil || filters.ScheduledTo == nil || filters.ScheduledTo.Sub(*filters.ScheduledFrom) != time.Nanosecond {
		t.Fatalf("filters=%+v err=%v", filters, err)
	}
	for _, values := range []url.Values{
		{"scheduled_from": {"not-a-date"}},
		{"scheduled_from": {"2026-09-21T00:00:00Z"}, "scheduled_to": {"2026-09-21T08:00:00+08:00"}},
		{"scheduled_from": {"2026-09-21T00:00:00.5Z"}, "scheduled_to": {"2026-09-21T00:00:00Z"}},
		{"schedule_state": {"unscheduled"}, "scheduled_from": {""}},
		{"schedule_state": {"scheduled", "scheduled"}},
		{"schedule_state": {" scheduled "}},
		{"platform": {strings.Repeat("界", 65)}},
		{"status": {"unknown"}},
		{"project_id": {"bad"}},
		{"include_archived": {"unknown"}},
		{"task_state": {""}}, {"task_state": {"pending"}}, {"task_state": {" all "}}, {"task_state": {"all", "all"}},
	} {
		if _, err := parseContentItemQueryFilters(values); err == nil {
			t.Fatalf("accepted invalid filters %v", values)
		}
	}
	router, _ := newProjectTestAPI(t)
	for _, query := range []string{"task_state=unknown", "task_state=all&task_state=all", "task_state="} {
		response := performRequest(router, http.MethodGet, "/api/v1/content-items?"+query, nil, nil)
		assertAPIError(t, response, http.StatusBadRequest, "INVALID_FILTER")
	}
}

func TestContentItemQueryListHydrationSharesReadTransaction(t *testing.T) {
	router, store := newProjectTestAPI(t)
	item := createContentQueryFixtureItem(t, router, "Snapshot content", "2026-09-21T00:00:00Z")
	task := createTaskForTaskFacts(t, router, `{"title":"Snapshot preparation"}`)
	linked := performRequest(router, http.MethodPost, "/api/v1/content-items/"+item.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":true}`, task.ID)), map[string]string{"If-Match": "\"1\""})
	if linked.Code != http.StatusCreated {
		t.Fatal(linked.Body.String())
	}
	var pools []gorm.ConnPool
	observe := func(tx *gorm.DB) {
		sql := strings.ToLower(tx.Statement.SQL.String())
		if strings.Contains(sql, "content_items") || strings.Contains(sql, "content_item_tasks") {
			pools = append(pools, tx.Statement.ConnPool)
		}
	}
	const callback = "test:content_snapshot"
	if err := store.DB.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.DB.Callback().Query().Remove(callback)
		_ = store.DB.Callback().Row().Remove(callback)
	})
	response := performRequest(router, http.MethodGet, "/api/v1/content-items?task_state=required_incomplete", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if len(pools) < 4 {
		t.Fatalf("missing observed list/hydration queries: %d", len(pools))
	}
	for _, pool := range pools {
		if _, inTx := pool.(gorm.TxCommitter); !inTx || pool != pools[0] {
			t.Fatal("count, page, relation or progress read escaped the single transaction")
		}
	}
	var stored models.ContentItem
	if err := store.DB.First(&stored, "id=?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ScheduledAt == nil || *stored.ScheduledAt != "2026-09-21T00:00:00Z" {
		t.Fatal("query rewrote the stored timestamp")
	}
}

func TestContentItemQueryFractionalBoundaryAndChronologicalOrder(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	whole := createContentQueryFixtureItem(t, router, "Whole second", "2026-09-21T00:00:00Z")
	nano := createContentQueryFixtureItem(t, router, "One nanosecond", "2026-09-21T00:00:00.000000001Z")
	half := createContentQueryFixtureItem(t, router, "Half second", "2026-09-21T00:00:00.5Z")
	last := createContentQueryFixtureItem(t, router, "Last nanosecond", "2026-09-21T00:00:00.999999999Z")
	next := createContentQueryFixtureItem(t, router, "Next second", "2026-09-21T00:00:01Z")
	unscheduled := createContentQueryFixtureItem(t, router, "No date", "")
	for _, test := range []struct {
		name, from, to string
		ids            []string
	}{
		{"mixed precision order", "", "", []string{whole.ID, nano.ID, half.ID, last.ID, next.ID, unscheduled.ID}},
		{"inclusive whole-second lower bound", "2026-09-21T00:00:00Z", "2026-09-21T00:00:01Z", []string{whole.ID, nano.ID, half.ID, last.ID}},
		{"valid same-second interval", "2026-09-21T00:00:00Z", "2026-09-21T00:00:00.5Z", []string{whole.ID, nano.ID}},
		{"exclusive fractional upper bound", "2026-09-21T00:00:00.000000001Z", "2026-09-21T00:00:00.999999999Z", []string{nano.ID, half.ID}},
		{"offset boundaries normalize to UTC", "2026-09-21T08:00:00+08:00", "2026-09-21T08:00:00.5+08:00", []string{whole.ID, nano.ID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := url.Values{}
			if test.from != "" {
				query.Set("scheduled_from", test.from)
			}
			if test.to != "" {
				query.Set("scheduled_to", test.to)
			}
			response := performRequest(router, http.MethodGet, "/api/v1/content-items?"+query.Encode(), nil, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("list=%d %s", response.Code, response.Body.String())
			}
			items, meta := decodeContentItemListResponse(t, response.Body.Bytes())
			if len(items) != len(test.ids) || meta.Total != int64(len(test.ids)) {
				t.Fatalf("count=%d meta=%+v want=%d", len(items), meta, len(test.ids))
			}
			for i, item := range items {
				if item.ID != test.ids[i] {
					t.Fatalf("order[%d]=%s want=%s", i, item.Title, test.ids[i])
				}
			}
		})
	}
}
