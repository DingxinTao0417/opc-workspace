package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func decodeContentItemResponse(t *testing.T, body []byte) contentItemResponse {
	t.Helper()
	var envelope struct {
		Data contentItemResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode content item response: %v", err)
	}
	return envelope.Data
}

func decodeContentItemListResponse(t *testing.T, body []byte) ([]contentItemResponse, pageMeta) {
	t.Helper()
	var envelope struct {
		Data []contentItemResponse `json:"data"`
		Meta pageMeta              `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode content item list response: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func contentItemIDs(items []contentItemResponse) map[string]struct{} {
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		ids[item.ID] = struct{}{}
	}
	return ids
}

func TestContentItemLifecycleSchedulePublishAndTaskLink(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Content calendar project"}`, nil)
	taskRecorder := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"Prepare the release"}`), nil)
	if taskRecorder.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", taskRecorder.Code, taskRecorder.Body.String())
	}
	var task struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(fmt.Sprintf(`{
		"title":"  Product launch  ","platform":"  Newsletter ","project_id":%q,
		"scheduled_at":"2026-09-01T09:30:00+08:00","scheduled_timezone":"Asia/Shanghai","notes":"  Prepare copy  "
	}`, project.ID)), nil)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create content item = %d etag=%q: %s", createdRecorder.Code, createdRecorder.Header().Get("ETag"), createdRecorder.Body.String())
	}
	created := decodeContentItemResponse(t, createdRecorder.Body.Bytes())
	if created.Title != "Product launch" || created.Platform != "Newsletter" || created.Status != "draft" || created.ScheduledTimezone == nil || *created.ScheduledTimezone != "Asia/Shanghai" || created.ProjectID == nil || *created.ProjectID != project.ID {
		t.Fatalf("created content item = %#v", created)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/content-items?scheduled_from=2026-09-01T00:00:00Z&scheduled_to=2026-09-02T00:00:00Z&project_id="+project.ID, nil, nil)
	var listEnvelope struct {
		Data []contentItemResponse `json:"data"`
		Meta pageMeta              `json:"meta"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode content list: %v", err)
	}
	if listed.Code != http.StatusOK || listEnvelope.Meta.Total != 1 || len(listEnvelope.Data) != 1 || listEnvelope.Data[0].ID != created.ID {
		t.Fatalf("list content item = %d %#v", listed.Code, listEnvelope)
	}

	linkedRecorder := performRequest(router, http.MethodPost, "/api/v1/content-items/"+created.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":true}`, task.Data.ID)), map[string]string{"If-Match": `"1"`})
	linked := decodeContentItemResponse(t, linkedRecorder.Body.Bytes())
	if linkedRecorder.Code != http.StatusCreated || linked.Version != 2 || linked.RequiredTaskTotal != 1 || len(linked.Tasks) != 1 || linked.Tasks[0].ID != task.Data.ID {
		t.Fatalf("link task = %d %#v", linkedRecorder.Code, linked)
	}

	scheduledRecorder := performRequest(router, http.MethodPut, "/api/v1/content-items/"+created.ID+"/schedule", []byte(`{"scheduled_at":"2026-09-03T08:00:00Z","scheduled_timezone":"UTC"}`), map[string]string{"If-Match": `"2"`})
	scheduled := decodeContentItemResponse(t, scheduledRecorder.Body.Bytes())
	if scheduledRecorder.Code != http.StatusOK || scheduled.Version != 3 || scheduled.Status != "scheduled" || scheduled.ScheduledTimezone == nil || *scheduled.ScheduledTimezone != "UTC" {
		t.Fatalf("schedule content item = %d %#v", scheduledRecorder.Code, scheduled)
	}

	publishedRecorder := performRequest(router, http.MethodPost, "/api/v1/content-items/"+created.ID+"/publish-confirmation", []byte(`{"external_link":" https://example.test/post "}`), map[string]string{"If-Match": `"3"`})
	published := decodeContentItemResponse(t, publishedRecorder.Body.Bytes())
	if publishedRecorder.Code != http.StatusOK || published.Version != 4 || published.Status != "published" || published.PublishedAt == nil || published.ExternalLink == nil || *published.ExternalLink != "https://example.test/post" {
		t.Fatalf("publish content item = %d %#v", publishedRecorder.Code, published)
	}

	stale := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+created.ID, []byte(`{"title":"stale writer"}`), map[string]string{"If-Match": `"3"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale update = %d: %s", stale.Code, stale.Body.String())
	}
	publishedArchive := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+created.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": `"4"`})
	if publishedArchive.Code != http.StatusConflict || responseErrorCode(t, publishedArchive.Body.Bytes()) != "CONTENT_ITEM_STATE_INVALID" {
		t.Fatalf("published archive protection = %d: %s", publishedArchive.Code, publishedArchive.Body.String())
	}
	toArchiveRecorder := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Archive me","platform":"Web"}`), nil)
	toArchive := decodeContentItemResponse(t, toArchiveRecorder.Body.Bytes())
	archivedRecorder := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+toArchive.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": `"1"`})
	archived := decodeContentItemResponse(t, archivedRecorder.Body.Bytes())
	if archivedRecorder.Code != http.StatusOK || archived.Status != "archived" || archived.ArchivedFromStatus == nil || *archived.ArchivedFromStatus != "draft" {
		t.Fatalf("archive content item = %d %#v", archivedRecorder.Code, archived)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/content-items/"+toArchive.ID+"?confirm=true", nil, map[string]string{"If-Match": `"2"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete archived content item = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestContentItemRejectsInvalidScheduleAndMissingVersion(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	invalid := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Bad timezone","platform":"Web","scheduled_at":"2026-09-01T09:00:00Z"}`), nil)
	if invalid.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalid.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid content schedule = %d: %s", invalid.Code, invalid.Body.String())
	}
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Guarded item","platform":"Web"}`), nil)
	created := decodeContentItemResponse(t, createdRecorder.Body.Bytes())
	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+created.ID, []byte(`{"title":"new"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing content version = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
}

func TestContentItemListFiltersByScheduleStateAndArchiveVisibility(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	create := func(body string) contentItemResponse {
		t.Helper()
		response := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(body), nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("create content item = %d: %s", response.Code, response.Body.String())
		}
		return decodeContentItemResponse(t, response.Body.Bytes())
	}
	archive := func(item contentItemResponse) contentItemResponse {
		t.Helper()
		response := performRequest(
			router,
			http.MethodPatch,
			"/api/v1/content-items/"+item.ID,
			[]byte(`{"status":"archived"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)},
		)
		if response.Code != http.StatusOK {
			t.Fatalf("archive content item = %d: %s", response.Code, response.Body.String())
		}
		return decodeContentItemResponse(t, response.Body.Bytes())
	}
	list := func(path string) ([]contentItemResponse, pageMeta) {
		t.Helper()
		response := performRequest(router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("list content items %s = %d: %s", path, response.Code, response.Body.String())
		}
		return decodeContentItemListResponse(t, response.Body.Bytes())
	}

	scheduledActive := create(`{"title":"Scheduled active","platform":"Web","scheduled_at":"2026-09-01T09:00:00Z","scheduled_timezone":"UTC"}`)
	unscheduledActive := create(`{"title":"Unscheduled active","platform":"Web"}`)
	scheduledArchived := archive(create(`{"title":"Scheduled archived","platform":"Web","scheduled_at":"2026-09-02T09:00:00Z","scheduled_timezone":"UTC"}`))
	unscheduledArchived := archive(create(`{"title":"Unscheduled archived","platform":"Web"}`))

	items, meta := list("/api/v1/content-items?schedule_state=scheduled")
	if meta.Total != 1 || len(items) != 1 || items[0].ID != scheduledActive.ID {
		t.Fatalf("default scheduled filter = meta=%#v items=%#v", meta, contentItemIDs(items))
	}
	items, meta = list("/api/v1/content-items?schedule_state=unscheduled")
	if meta.Total != 1 || len(items) != 1 || items[0].ID != unscheduledActive.ID {
		t.Fatalf("default unscheduled filter = meta=%#v items=%#v", meta, contentItemIDs(items))
	}
	items, meta = list("/api/v1/content-items?schedule_state=scheduled&scheduled_from=2026-09-01T00:00:00Z&scheduled_to=2026-09-02T00:00:00Z")
	if meta.Total != 1 || len(items) != 1 || items[0].ID != scheduledActive.ID {
		t.Fatalf("scheduled range intersection = meta=%#v items=%#v", meta, contentItemIDs(items))
	}

	items, meta = list("/api/v1/content-items?schedule_state=scheduled&include_archived=true")
	scheduledIDs := contentItemIDs(items)
	if meta.Total != 2 || len(items) != 2 {
		t.Fatalf("scheduled include archived = meta=%#v items=%#v", meta, scheduledIDs)
	}
	for _, id := range []string{scheduledActive.ID, scheduledArchived.ID} {
		if _, exists := scheduledIDs[id]; !exists {
			t.Fatalf("scheduled include archived omitted %s: %#v", id, scheduledIDs)
		}
	}
	items, meta = list("/api/v1/content-items?schedule_state=unscheduled&include_archived=true")
	unscheduledIDs := contentItemIDs(items)
	if meta.Total != 2 || len(items) != 2 {
		t.Fatalf("unscheduled include archived = meta=%#v items=%#v", meta, unscheduledIDs)
	}
	for _, id := range []string{unscheduledActive.ID, unscheduledArchived.ID} {
		if _, exists := unscheduledIDs[id]; !exists {
			t.Fatalf("unscheduled include archived omitted %s: %#v", id, unscheduledIDs)
		}
	}

	items, meta = list("/api/v1/content-items?status=archived&schedule_state=scheduled")
	if meta.Total != 1 || len(items) != 1 || items[0].ID != scheduledArchived.ID {
		t.Fatalf("archived scheduled status filter = meta=%#v items=%#v", meta, contentItemIDs(items))
	}
	items, meta = list("/api/v1/content-items?status=archived&schedule_state=unscheduled")
	if meta.Total != 1 || len(items) != 1 || items[0].ID != unscheduledArchived.ID {
		t.Fatalf("archived unscheduled status filter = meta=%#v items=%#v", meta, contentItemIDs(items))
	}

	items, meta = list("/api/v1/content-items")
	allActiveIDs := contentItemIDs(items)
	if meta.Total != 2 || len(items) != 2 {
		t.Fatalf("legacy unfiltered list = meta=%#v items=%#v", meta, allActiveIDs)
	}
	for _, id := range []string{scheduledActive.ID, unscheduledActive.ID} {
		if _, exists := allActiveIDs[id]; !exists {
			t.Fatalf("legacy unfiltered list omitted %s: %#v", id, allActiveIDs)
		}
	}
}

func TestContentItemListRejectsInvalidAndConflictingScheduleState(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	for name, path := range map[string]string{
		"invalid":         "/api/v1/content-items?schedule_state=all",
		"empty":           "/api/v1/content-items?schedule_state=",
		"not trimmed":     "/api/v1/content-items?schedule_state=%20scheduled%20",
		"from conflict":   "/api/v1/content-items?schedule_state=unscheduled&scheduled_from=2026-09-01T00:00:00Z",
		"to conflict":     "/api/v1/content-items?schedule_state=unscheduled&scheduled_to=2026-09-02T00:00:00Z",
		"bounds conflict": "/api/v1/content-items?schedule_state=unscheduled&scheduled_from=2026-09-01T00:00:00Z&scheduled_to=2026-09-02T00:00:00Z",
		"empty bound":     "/api/v1/content-items?schedule_state=unscheduled&scheduled_from=",
		"duplicate same":  "/api/v1/content-items?schedule_state=scheduled&schedule_state=scheduled",
		"duplicate mixed": "/api/v1/content-items?schedule_state=scheduled&schedule_state=unscheduled",
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodGet, path, nil, nil)
			if response.Code != http.StatusBadRequest || responseErrorCode(t, response.Body.Bytes()) != "INVALID_FILTER" {
				t.Fatalf("response = %d: %s", response.Code, response.Body.String())
			}
		})
	}

	allowed := performRequest(
		router,
		http.MethodGet,
		"/api/v1/content-items?schedule_state=scheduled&scheduled_from=2026-09-01T00:00:00Z&scheduled_to=2026-09-02T00:00:00Z",
		nil,
		nil,
	)
	if allowed.Code != http.StatusOK {
		t.Fatalf("scheduled range combination = %d: %s", allowed.Code, allowed.Body.String())
	}
}

func TestContentItemUnscheduledPaginationIsStable(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	createdIDs := make(map[string]struct{}, 2)
	for _, title := range []string{"Unscheduled first", "Unscheduled second"} {
		response := performRequest(
			router,
			http.MethodPost,
			"/api/v1/content-items",
			[]byte(fmt.Sprintf(`{"title":%q,"platform":"Web"}`, title)),
			nil,
		)
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", title, response.Code, response.Body.String())
		}
		createdIDs[decodeContentItemResponse(t, response.Body.Bytes()).ID] = struct{}{}
	}

	seen := make(map[string]struct{}, 2)
	for page := 1; page <= 2; page++ {
		response := performRequest(
			router,
			http.MethodGet,
			fmt.Sprintf("/api/v1/content-items?schedule_state=unscheduled&page_size=1&page=%d", page),
			nil,
			nil,
		)
		if response.Code != http.StatusOK {
			t.Fatalf("list page %d = %d: %s", page, response.Code, response.Body.String())
		}
		items, meta := decodeContentItemListResponse(t, response.Body.Bytes())
		if meta.Page != page || meta.PageSize != 1 || meta.Total != 2 || len(items) != 1 {
			t.Fatalf("page %d meta=%#v items=%#v", page, meta, items)
		}
		if _, duplicate := seen[items[0].ID]; duplicate {
			t.Fatalf("duplicate content item across pages: %s", items[0].ID)
		}
		seen[items[0].ID] = struct{}{}
	}
	if len(seen) != len(createdIDs) {
		t.Fatalf("pagination omitted content items: created=%#v seen=%#v", createdIDs, seen)
	}
	for id := range createdIDs {
		if _, exists := seen[id]; !exists {
			t.Fatalf("pagination omitted content item %s", id)
		}
	}
}
