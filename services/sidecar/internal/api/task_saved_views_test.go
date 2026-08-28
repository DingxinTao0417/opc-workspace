package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestTaskSavedViewsCRUDValidationAndConcurrency(t *testing.T) {
	router, _ := newTaskFactsAPI(t)
	list := performRequest(router, http.MethodGet, "/api/v1/task-saved-views", nil, nil)
	if list.Code != http.StatusOK || string(list.Body.Bytes()) != `{"data":[]}` {
		t.Fatalf("initial list = %d: %s", list.Code, list.Body.String())
	}

	projectID := "018f0000-0000-7000-8000-000000001711"
	clientID := "018f0000-0000-7000-8000-000000001712"
	tagID := "018f0000-0000-7000-8000-000000001713"
	createBody := fmt.Sprintf(`{
		"name":"  本周高优任务  ",
		"definition":{
			"q":"  交付  ","status":"active","priority":"P1","kind":"work",
			"project_id":%q,"client_id":%q,"tag_ids":[%q,%q],
			"planned_date":"","planned_from":"2026-08-24","planned_to":"2026-08-30",
			"due_from":"2026-08-24","due_to":"2026-09-05","sort":" -updated_at,title "
		}
	}`, projectID, clientID, tagID, tagID)
	created := performRequest(router, http.MethodPost, "/api/v1/task-saved-views", []byte(createBody), nil)
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` {
		t.Fatalf("create = %d etag=%q: %s", created.Code, created.Header().Get("ETag"), created.Body.String())
	}
	var createdEnvelope struct {
		Data taskSavedViewResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	view := createdEnvelope.Data
	if view.Name != "本周高优任务" || view.Version != 1 || view.SchemaVersion != 1 || view.Definition.Query != "交付" || len(view.Definition.TagIDs) != 1 || view.Definition.Sort != "-updated_at,title" {
		t.Fatalf("normalized create = %#v", view)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/task-saved-views", nil, nil)
	if listed.Code != http.StatusOK || !containsResponseID(listed.Body.Bytes(), view.ID) {
		t.Fatalf("list after create = %d: %s", listed.Code, listed.Body.String())
	}
	duplicate := performRequest(router, http.MethodPost, "/api/v1/task-saved-views", []byte(`{"name":"本周高优任务","definition":{"tag_ids":[]}}`), nil)
	if duplicate.Code != http.StatusConflict || responseErrorCode(t, duplicate.Body.Bytes()) != "TASK_SAVED_VIEW_NAME_EXISTS" {
		t.Fatalf("duplicate name = %d: %s", duplicate.Code, duplicate.Body.String())
	}

	invalidDefinitions := []string{
		`{"name":"冲突日期","definition":{"tag_ids":[],"planned_date":"2026-08-28","planned_from":"2026-08-20"}}`,
		`{"name":"倒置日期","definition":{"tag_ids":[],"due_from":"2026-09-01","due_to":"2026-08-01"}}`,
		`{"name":"未知字段","definition":{"tag_ids":[],"invented":true}}`,
	}
	for _, body := range invalidDefinitions {
		invalid := performRequest(router, http.MethodPost, "/api/v1/task-saved-views", []byte(body), nil)
		if invalid.Code != http.StatusUnprocessableEntity && invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid definition = %d: %s", invalid.Code, invalid.Body.String())
		}
	}

	updated := performRequest(
		router, http.MethodPatch, "/api/v1/task-saved-views/"+view.ID,
		[]byte(`{"name":"本周交付","definition":{"q":"验收","status":"waiting_review","tag_ids":[],"sort":"priority"}}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` {
		t.Fatalf("update = %d etag=%q: %s", updated.Code, updated.Header().Get("ETag"), updated.Body.String())
	}
	stale := performRequest(
		router, http.MethodPatch, "/api/v1/task-saved-views/"+view.ID,
		[]byte(`{"name":"旧写入"}`), map[string]string{"If-Match": `"1"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale update = %d: %s", stale.Code, stale.Body.String())
	}
	withoutConfirm := performRequest(
		router, http.MethodDelete, "/api/v1/task-saved-views/"+view.ID,
		nil, map[string]string{"If-Match": `"2"`},
	)
	if withoutConfirm.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("delete without confirmation = %d: %s", withoutConfirm.Code, withoutConfirm.Body.String())
	}
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/task-saved-views/"+view.ID+"?confirm=true",
		nil, map[string]string{"If-Match": `"2"`},
	)
	if deleted.Code != http.StatusOK || !containsResponseID(deleted.Body.Bytes(), view.ID) {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func containsResponseID(body []byte, id string) bool {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return false
	}
	return findResponseString(value, id)
}

func findResponseString(value any, target string) bool {
	switch item := value.(type) {
	case string:
		return item == target
	case []any:
		for _, child := range item {
			if findResponseString(child, target) {
				return true
			}
		}
	case map[string]any:
		for _, child := range item {
			if findResponseString(child, target) {
				return true
			}
		}
	}
	return false
}
