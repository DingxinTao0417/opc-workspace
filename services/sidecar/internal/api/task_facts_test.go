package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func newTaskFactsAPI(t *testing.T) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "task-facts.db"))
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
	return router, store
}

func createTagForTaskFacts(t *testing.T, router http.Handler, name, color string) models.Tag {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"name":%q,"color":%q}`, name, color))
	recorder := performRequest(router, http.MethodPost, "/api/v1/tags", body, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create tag = %d: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data models.Tag `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode tag: %v", err)
	}
	return envelope.Data
}

func createTaskForTaskFacts(t *testing.T, router http.Handler, body string) models.Task {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(body), nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return envelope.Data
}

func getTaskForTaskFacts(t *testing.T, router http.Handler, id string) models.Task {
	t.Helper()
	recorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+id, nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get task = %d: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if recorder.Header().Get("ETag") != fmt.Sprintf(`"%d"`, envelope.Data.Version) {
		t.Fatalf("task ETag = %q, version = %d", recorder.Header().Get("ETag"), envelope.Data.Version)
	}
	return envelope.Data
}

func TestTaskFactsTagsHierarchyAndConcurrency(t *testing.T) {
	router := newTestAPI(t)
	tag := createTagForTaskFacts(t, router, "Design", "#6e7bf2")
	if tag.Color != "#6E7BF2" || tag.Version != 1 {
		t.Fatalf("created tag = %#v", tag)
	}

	duplicate := performRequest(router, http.MethodPost, "/api/v1/tags", []byte(`{"name":" design ","color":"#112233"}`), nil)
	if duplicate.Code != http.StatusConflict || responseErrorCode(t, duplicate.Body.Bytes()) != "TAG_NAME_CONFLICT" {
		t.Fatalf("case-insensitive duplicate tag = %d: %s", duplicate.Code, duplicate.Body.String())
	}
	invalidColor := performRequest(router, http.MethodPost, "/api/v1/tags", []byte(`{"name":"Invalid","color":"purple"}`), nil)
	if invalidColor.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid tag color = %d: %s", invalidColor.Code, invalidColor.Body.String())
	}

	parent := createTaskForTaskFacts(t, router, fmt.Sprintf(
		`{"title":"Review launch package","kind":"review","completion_criteria":"Owner accepts the package","planned_date":"2026-08-27","tag_ids":[%q]}`,
		tag.ID,
	))
	if parent.Kind != "review" || parent.CompletionCriteria == "" || parent.Version != 1 || len(parent.Tags) != 1 {
		t.Fatalf("parent facts = %#v", parent)
	}
	child := createTaskForTaskFacts(t, router, fmt.Sprintf(
		`{"title":"Check launch links","kind":"work","parent_task_id":%q,"planned_date":"2026-08-27"}`,
		parent.ID,
	))
	if child.ParentTaskID == nil || *child.ParentTaskID != parent.ID || child.ParentTaskTitle == nil || *child.ParentTaskTitle != parent.Title {
		t.Fatalf("child parent representation = %#v", child)
	}

	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Version != 2 || parent.SubtaskTotal != 1 || parent.SubtaskCompleted != 0 {
		t.Fatalf("parent aggregate after child insert = %#v", parent)
	}

	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+child.ID, []byte(`{"title":"Changed child"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing task If-Match = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	invalidVersion := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+child.ID, []byte(`{"title":"Changed child"}`), map[string]string{"If-Match": `"abc"`})
	if invalidVersion.Code != http.StatusBadRequest || responseErrorCode(t, invalidVersion.Body.Bytes()) != "INVALID_VERSION" {
		t.Fatalf("invalid task If-Match = %d: %s", invalidVersion.Code, invalidVersion.Body.String())
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+child.ID, []byte(`{"title":"Changed child"}`), map[string]string{"If-Match": `"9"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale task If-Match = %d: %s", stale.Code, stale.Body.String())
	}

	done := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+child.ID+"/status",
		[]byte(`{"status":"done"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if done.Code != http.StatusOK {
		t.Fatalf("complete child = %d: %s", done.Code, done.Body.String())
	}
	child = getTaskForTaskFacts(t, router, child.ID)
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if child.Version != 2 || parent.Version != 3 || parent.SubtaskCompleted != 1 {
		t.Fatalf("versions after child completion: parent=%#v child=%#v", parent, child)
	}

	cycle := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+parent.ID,
		[]byte(fmt.Sprintf(`{"parent_task_id":%q}`, child.ID)),
		map[string]string{"If-Match": `"3"`},
	)
	if cycle.Code != http.StatusUnprocessableEntity || responseErrorCode(t, cycle.Body.Bytes()) != "TASK_PARENT_CYCLE" {
		t.Fatalf("task cycle = %d: %s", cycle.Code, cycle.Body.String())
	}

	updatedTag := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tags/"+tag.ID,
		[]byte(`{"name":"UX","color":"#123456"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updatedTag.Code != http.StatusOK || updatedTag.Header().Get("ETag") != `"2"` {
		t.Fatalf("update tag = %d headers=%v: %s", updatedTag.Code, updatedTag.Header(), updatedTag.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Version != 4 || len(parent.Tags) != 1 || parent.Tags[0].Name != "UX" || parent.Tags[0].Color != "#123456" {
		t.Fatalf("task invalidation after tag update = %#v", parent)
	}

	unconfirmedDelete := performRequest(router, http.MethodDelete, "/api/v1/tags/"+tag.ID, nil, map[string]string{"If-Match": `"2"`})
	if unconfirmedDelete.Code != http.StatusUnprocessableEntity || responseErrorCode(t, unconfirmedDelete.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed tag deletion = %d: %s", unconfirmedDelete.Code, unconfirmedDelete.Body.String())
	}
	deletedTag := performRequest(router, http.MethodDelete, "/api/v1/tags/"+tag.ID+"?confirm=true", nil, map[string]string{"If-Match": `"2"`})
	if deletedTag.Code != http.StatusOK {
		t.Fatalf("delete tag = %d: %s", deletedTag.Code, deletedTag.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Version != 5 || len(parent.Tags) != 0 {
		t.Fatalf("task invalidation after tag delete = %#v", parent)
	}

	deletedParent := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+parent.ID, nil, map[string]string{"If-Match": `"5"`})
	if deletedParent.Code != http.StatusNoContent {
		t.Fatalf("delete parent = %d: %s", deletedParent.Code, deletedParent.Body.String())
	}
	child = getTaskForTaskFacts(t, router, child.ID)
	if child.ParentTaskID != nil || child.ParentTaskTitle != nil || child.Version != 3 {
		t.Fatalf("detached child = %#v", child)
	}
}

func TestTaskIdempotencyCanonicalTagsAndLegacySnapshot(t *testing.T) {
	router, store := newTaskFactsAPI(t)
	firstTag := createTagForTaskFacts(t, router, "First", "#112233")
	secondTag := createTagForTaskFacts(t, router, "Second", "#445566")
	firstBody := []byte(fmt.Sprintf(`{"title":"Canonical tag task","tag_ids":[%q,%q]}`, firstTag.ID, secondTag.ID))
	secondBody := []byte(fmt.Sprintf(`{"title":"Canonical tag task","tag_ids":[%q,%q]}`, secondTag.ID, firstTag.ID))
	headers := map[string]string{"Idempotency-Key": "task-tags-canonical-v2"}
	created := performRequest(router, http.MethodPost, "/api/v1/tasks", firstBody, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create tagged task = %d: %s", created.Code, created.Body.String())
	}
	replayed := performRequest(router, http.MethodPost, "/api/v1/tasks", secondBody, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("canonical tag replay = %d header=%q body=%s original=%s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String(), created.Body.String())
	}

	legacyTask, err := taskFromCreateRequest(createTaskRequest{Title: "Legacy task"})
	if err != nil {
		t.Fatalf("legacy task normalization: %v", err)
	}
	legacyHash, err := legacyTaskCreateRequestHash(legacyTask)
	if err != nil {
		t.Fatalf("legacy task hash: %v", err)
	}
	legacyBody := `{"id":"11111111-1111-4111-8111-111111111111","title":"Legacy task","description":"","status":"todo","priority":"P2","project_id":null,"due_date":null,"planned_date":null,"estimated_minutes":null,"actual_minutes":0,"manual_order":null,"created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:00:00Z","completed_at":null}`
	legacyStatus := http.StatusCreated
	legacyKey := models.IdempotencyKey{
		Key: "legacy-task-snapshot", Endpoint: createTaskEndpoint,
		ResourceID: "11111111-1111-4111-8111-111111111111", RequestHash: &legacyHash,
		ResponseBody: &legacyBody, ResponseStatus: &legacyStatus, CreatedAt: "2026-08-01T00:00:00Z",
	}
	if err := store.DB.Create(&legacyKey).Error; err != nil {
		t.Fatalf("insert legacy idempotency record: %v", err)
	}
	legacyReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(`{"title":"Legacy task"}`),
		map[string]string{"Idempotency-Key": legacyKey.Key},
	)
	if legacyReplay.Code != http.StatusCreated || legacyReplay.Header().Get("Idempotency-Replayed") != "true" || legacyReplay.Header().Get("ETag") != `"1"` {
		t.Fatalf("legacy replay = %d headers=%v body=%s", legacyReplay.Code, legacyReplay.Header(), legacyReplay.Body.String())
	}
	var legacyEnvelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(legacyReplay.Body.Bytes(), &legacyEnvelope); err != nil {
		t.Fatalf("decode legacy replay: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(legacyEnvelope.Data), []byte(legacyBody)) {
		t.Fatalf("legacy snapshot changed:\n got %s\nwant %s", legacyEnvelope.Data, legacyBody)
	}
	legacyConflict := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(`{"title":"Legacy task","kind":"review"}`),
		map[string]string{"Idempotency-Key": legacyKey.Key},
	)
	if legacyConflict.Code != http.StatusConflict || responseErrorCode(t, legacyConflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("legacy request with new facts = %d: %s", legacyConflict.Code, legacyConflict.Body.String())
	}
}

func TestTaskListStablePaginationFiltersAndEscaping(t *testing.T) {
	router, store := newTaskFactsAPI(t)
	firstTag := createTagForTaskFacts(t, router, "Alpha", "#112233")
	secondTag := createTagForTaskFacts(t, router, "Beta", "#445566")
	createdAt := "2026-08-01T00:00:00Z"
	ids := make([]string, 101)
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		for index := 0; index < 101; index++ {
			id := uuid.NewString()
			if index == 0 {
				// Keep the highest lexical UUID on the highest-priority task so
				// manual-order fallback cannot accidentally pass by UUID ordering.
				id = "ffffffff-ffff-4fff-bfff-ffffffffffff"
			}
			ids[index] = id
			title := fmt.Sprintf("Task %03d", index)
			if index == 1 {
				title = "Percent % marker"
			}
			if index == 2 {
				title = "Under_score marker"
			}
			if index == 3 {
				title = `Slash \ marker`
			}
			var plannedDate *string
			if index < 2 {
				value := fmt.Sprintf("2026-08-%02d", 20+index)
				plannedDate = &value
			}
			var parentID *string
			if index == 100 {
				parentID = &ids[0]
			}
			priority := "P2"
			if index == 0 {
				priority = "P0"
			}
			task := models.Task{
				ID: id, Title: title, Description: "", Kind: "work", Status: "todo", Priority: priority,
				ParentTaskID: parentID, PlannedDate: plannedDate, ActualMinutes: 0, Version: 1,
				CreatedAt: createdAt, UpdatedAt: createdAt,
			}
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
		}
		for _, relation := range [][2]string{{ids[0], firstTag.ID}, {ids[0], secondTag.ID}, {ids[1], firstTag.ID}} {
			if err := tx.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", relation[0], relation[1]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed pagination tasks: %v", err)
	}

	type listEnvelope struct {
		Data []models.Task `json:"data"`
		Meta pageMeta      `json:"meta"`
	}
	readList := func(path string) listEnvelope {
		t.Helper()
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		var result listEnvelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode list %s: %v", path, err)
		}
		return result
	}

	firstPage := readList("/api/v1/tasks?page=1&page_size=100&sort=created_at")
	secondPage := readList("/api/v1/tasks?page=2&page_size=100&sort=created_at")
	if firstPage.Meta.Total != 101 || len(firstPage.Data) != 100 || len(secondPage.Data) != 1 {
		t.Fatalf("pagination sizes: first=%d second=%d total=%d", len(firstPage.Data), len(secondPage.Data), firstPage.Meta.Total)
	}
	seen := make(map[string]struct{}, 101)
	for _, task := range append(firstPage.Data, secondPage.Data...) {
		if _, duplicate := seen[task.ID]; duplicate {
			t.Fatalf("task %s repeated across pages", task.ID)
		}
		seen[task.ID] = struct{}{}
	}
	if len(seen) != 101 {
		t.Fatalf("unique paged tasks = %d, want 101", len(seen))
	}
	repeated := readList("/api/v1/tasks?page=1&page_size=100&sort=created_at")
	for index := range firstPage.Data {
		if firstPage.Data[index].ID != repeated.Data[index].ID {
			t.Fatalf("stable tie-breaker changed at %d", index)
		}
	}

	for _, sortValue := range []string{"manual_order", "priority", "due_date", "planned_date", "created_at", "-updated_at", "title", "status", "kind"} {
		result := readList("/api/v1/tasks?page=1&page_size=5&sort=" + url.QueryEscape(sortValue))
		if result.Meta.Total != 101 || len(result.Data) != 5 {
			t.Fatalf("sort %s result = %#v", sortValue, result.Meta)
		}
	}
	defaultOrder := readList("/api/v1/tasks?page=1&page_size=5")
	manualOrder := readList("/api/v1/tasks?page=1&page_size=5&sort=manual_order")
	if defaultOrder.Data[0].ID != ids[0] || manualOrder.Data[0].ID != ids[0] {
		t.Fatalf("manual order did not retain the default priority fallback: default=%s manual=%s", defaultOrder.Data[0].ID, manualOrder.Data[0].ID)
	}
	planned := readList("/api/v1/tasks?page=1&page_size=5&sort=planned_date")
	if planned.Data[0].PlannedDate == nil || planned.Data[1].PlannedDate == nil {
		t.Fatalf("planned_date null values did not sort last: %#v", planned.Data)
	}
	roots := readList("/api/v1/tasks?root_only=true&page_size=100")
	if roots.Meta.Total != 100 {
		t.Fatalf("root task count = %d, want 100", roots.Meta.Total)
	}
	children := readList("/api/v1/tasks?parent_task_id=" + ids[0])
	if children.Meta.Total != 1 || children.Data[0].ID != ids[100] {
		t.Fatalf("child filter = %#v", children)
	}
	tagged := readList("/api/v1/tasks?tag_id=" + firstTag.ID + "&tag_id=" + secondTag.ID)
	if tagged.Meta.Total != 1 || tagged.Data[0].ID != ids[0] || len(tagged.Data[0].Tags) != 2 {
		t.Fatalf("AND tag filter = %#v", tagged)
	}
	dateRange := readList("/api/v1/tasks?planned_from=2026-08-20&planned_to=2026-08-21")
	if dateRange.Meta.Total != 2 {
		t.Fatalf("planned range total = %d, want 2", dateRange.Meta.Total)
	}
	for _, query := range []string{"%", "_", `\`} {
		result := readList("/api/v1/tasks?q=" + url.QueryEscape(query))
		if result.Meta.Total != 1 {
			t.Fatalf("escaped search %q total = %d, want 1", query, result.Meta.Total)
		}
	}
	invalid := performRequest(router, http.MethodGet, "/api/v1/tasks?sort=not_a_field", nil, nil)
	if invalid.Code != http.StatusBadRequest || responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_SORT" {
		t.Fatalf("invalid task sort = %d: %s", invalid.Code, invalid.Body.String())
	}
}

func TestTaskReorderAndBatchUpdatesAreAtomic(t *testing.T) {
	router := newTestAPI(t)
	first := createTaskForTaskFacts(t, router, `{"title":"First ordered task","planned_date":"2026-08-27"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"Second ordered task","planned_date":"2026-08-27"}`)
	tag := createTagForTaskFacts(t, router, "Batch", "#778899")

	reorderBody := []byte(fmt.Sprintf(
		`{"planned_date":"2026-08-27","mode":"manual","items":[{"id":%q,"expected_version":1},{"id":%q,"expected_version":1}]}`,
		second.ID,
		first.ID,
	))
	reordered := performRequest(router, http.MethodPut, "/api/v1/tasks/reorder", reorderBody, nil)
	if reordered.Code != http.StatusOK {
		t.Fatalf("manual reorder = %d: %s", reordered.Code, reordered.Body.String())
	}
	var reorderEnvelope struct {
		Data reorderedTasksResponse `json:"data"`
	}
	if err := json.Unmarshal(reordered.Body.Bytes(), &reorderEnvelope); err != nil {
		t.Fatalf("decode reorder: %v", err)
	}
	if reorderEnvelope.Data.Changed != 2 || len(reorderEnvelope.Data.Tasks) != 2 || reorderEnvelope.Data.Tasks[0].ID != second.ID || reorderEnvelope.Data.Tasks[1].ID != first.ID {
		t.Fatalf("manual reorder response = %#v", reorderEnvelope.Data)
	}

	staleReorder := performRequest(router, http.MethodPut, "/api/v1/tasks/reorder", []byte(fmt.Sprintf(
		`{"planned_date":"2026-08-27","mode":"manual","items":[{"id":%q,"expected_version":2},{"id":%q,"expected_version":1}]}`,
		first.ID,
		second.ID,
	)), nil)
	if staleReorder.Code != http.StatusConflict || responseErrorCode(t, staleReorder.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale reorder = %d: %s", staleReorder.Code, staleReorder.Body.String())
	}
	first = getTaskForTaskFacts(t, router, first.ID)
	second = getTaskForTaskFacts(t, router, second.ID)
	if first.Version != 2 || second.Version != 2 || first.ManualOrder == nil || second.ManualOrder == nil || *second.ManualOrder >= *first.ManualOrder {
		t.Fatalf("reorder rollback/current state: first=%#v second=%#v", first, second)
	}

	setChanged := performRequest(router, http.MethodPut, "/api/v1/tasks/reorder", []byte(fmt.Sprintf(
		`{"planned_date":"2026-08-27","mode":"manual","items":[{"id":%q,"expected_version":2}]}`,
		first.ID,
	)), nil)
	if setChanged.Code != http.StatusConflict || responseErrorCode(t, setChanged.Body.Bytes()) != "TASK_REORDER_SET_CHANGED" {
		t.Fatalf("partial reorder collection = %d: %s", setChanged.Code, setChanged.Body.String())
	}

	reset := performRequest(router, http.MethodPut, "/api/v1/tasks/reorder", []byte(fmt.Sprintf(
		`{"planned_date":"2026-08-27","mode":"default","items":[{"id":%q,"expected_version":2},{"id":%q,"expected_version":2}]}`,
		first.ID,
		second.ID,
	)), nil)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset reorder = %d: %s", reset.Code, reset.Body.String())
	}
	first = getTaskForTaskFacts(t, router, first.ID)
	second = getTaskForTaskFacts(t, router, second.ID)
	if first.Version != 3 || second.Version != 3 || first.ManualOrder != nil || second.ManualOrder != nil {
		t.Fatalf("reset reorder state: first=%#v second=%#v", first, second)
	}

	addTagsBody := []byte(fmt.Sprintf(
		`{"action":"add_tags","items":[{"id":%q,"expected_version":3},{"id":%q,"expected_version":3}],"tag_ids":[%q]}`,
		first.ID,
		second.ID,
		tag.ID,
	))
	added := performRequest(router, http.MethodPatch, "/api/v1/tasks/batch", addTagsBody, nil)
	if added.Code != http.StatusOK {
		t.Fatalf("batch add tags = %d: %s", added.Code, added.Body.String())
	}
	var batchEnvelope struct {
		Data batchUpdatedTasksResponse `json:"data"`
	}
	if err := json.Unmarshal(added.Body.Bytes(), &batchEnvelope); err != nil {
		t.Fatalf("decode batch add: %v", err)
	}
	if batchEnvelope.Data.Changed != 2 || len(batchEnvelope.Data.Tasks[0].Tags) != 1 || batchEnvelope.Data.Tasks[0].Version != 4 || batchEnvelope.Data.Tasks[1].Version != 4 {
		t.Fatalf("batch add response = %#v", batchEnvelope.Data)
	}

	noOp := performRequest(router, http.MethodPatch, "/api/v1/tasks/batch", []byte(fmt.Sprintf(
		`{"action":"add_tags","items":[{"id":%q,"expected_version":4},{"id":%q,"expected_version":4}],"tag_ids":[%q]}`,
		first.ID,
		second.ID,
		tag.ID,
	)), nil)
	if noOp.Code != http.StatusOK {
		t.Fatalf("no-op batch add = %d: %s", noOp.Code, noOp.Body.String())
	}
	if err := json.Unmarshal(noOp.Body.Bytes(), &batchEnvelope); err != nil || batchEnvelope.Data.Changed != 0 || batchEnvelope.Data.Tasks[0].Version != 4 || batchEnvelope.Data.Tasks[1].Version != 4 {
		t.Fatalf("no-op batch response = %s error=%v", noOp.Body.String(), err)
	}

	staleBatch := performRequest(router, http.MethodPatch, "/api/v1/tasks/batch", []byte(fmt.Sprintf(
		`{"action":"remove_tags","items":[{"id":%q,"expected_version":4},{"id":%q,"expected_version":3}],"tag_ids":[%q]}`,
		first.ID,
		second.ID,
		tag.ID,
	)), nil)
	if staleBatch.Code != http.StatusConflict || responseErrorCode(t, staleBatch.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale batch = %d: %s", staleBatch.Code, staleBatch.Body.String())
	}
	first = getTaskForTaskFacts(t, router, first.ID)
	second = getTaskForTaskFacts(t, router, second.ID)
	if first.Version != 4 || second.Version != 4 || len(first.Tags) != 1 || len(second.Tags) != 1 {
		t.Fatalf("stale batch was not atomic: first=%#v second=%#v", first, second)
	}

	moveDate := performRequest(router, http.MethodPatch, "/api/v1/tasks/batch", []byte(fmt.Sprintf(
		`{"action":"set_planned_date","items":[{"id":%q,"expected_version":4},{"id":%q,"expected_version":4}],"planned_date":"2026-08-28"}`,
		first.ID,
		second.ID,
	)), nil)
	if moveDate.Code != http.StatusOK {
		t.Fatalf("batch planned date = %d: %s", moveDate.Code, moveDate.Body.String())
	}
	if err := json.Unmarshal(moveDate.Body.Bytes(), &batchEnvelope); err != nil || batchEnvelope.Data.Changed != 2 {
		t.Fatalf("decode batch date response = %s error=%v", moveDate.Body.String(), err)
	}
	for _, task := range batchEnvelope.Data.Tasks {
		if task.Version != 5 || task.PlannedDate == nil || *task.PlannedDate != "2026-08-28" || task.ManualOrder != nil {
			t.Fatalf("batch date task = %#v", task)
		}
	}
}

func TestTaskBatchAddTagsPreservesTwentyTagLimitAtomically(t *testing.T) {
	router := newTestAPI(t)
	tagIDs := make([]string, 21)
	for index := range tagIDs {
		tag := createTagForTaskFacts(
			t,
			router,
			fmt.Sprintf("Limit %02d", index),
			fmt.Sprintf("#%06X", index+1),
		)
		tagIDs[index] = tag.ID
	}
	createBody, err := json.Marshal(map[string]any{
		"title":   "Task at tag limit",
		"tag_ids": tagIDs[:20],
	})
	if err != nil {
		t.Fatalf("encode task at tag limit: %v", err)
	}
	task := createTaskForTaskFacts(t, router, string(createBody))
	if len(task.Tags) != 20 || task.Version != 1 {
		t.Fatalf("task at tag limit = %#v", task)
	}
	batchBody, err := json.Marshal(map[string]any{
		"action": "add_tags",
		"items": []map[string]any{{
			"id":               task.ID,
			"expected_version": task.Version,
		}},
		"tag_ids": []string{tagIDs[20]},
	})
	if err != nil {
		t.Fatalf("encode over-limit batch: %v", err)
	}
	recorder := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/batch",
		batchBody,
		nil,
	)
	if recorder.Code != http.StatusUnprocessableEntity || responseErrorCode(t, recorder.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("over-limit batch = %d: %s", recorder.Code, recorder.Body.String())
	}
	task = getTaskForTaskFacts(t, router, task.ID)
	if len(task.Tags) != 20 || task.Version != 1 {
		t.Fatalf("over-limit batch was not atomic = %#v", task)
	}
}
