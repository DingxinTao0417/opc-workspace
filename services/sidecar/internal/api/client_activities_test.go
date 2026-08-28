package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeClientActivityResponse(t *testing.T, body []byte) clientActivityResponse {
	t.Helper()
	var envelope struct {
		Data clientActivityResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode client activity response: %v", err)
	}
	return envelope.Data
}

func TestClientActivityCreateListUpdateAndSoftDelete(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Activity Client"}`, nil)
	occurredAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339)
	body := fmt.Sprintf(`{"kind":"note","title":"  项目沟通  ","body":"  第一行\n第二行  ","occurred_at":%q}`, occurredAt)
	headers := map[string]string{"Idempotency-Key": "client-activity-create-1"}

	createdRecorder := performRequest(
		router, http.MethodPost, "/api/v1/clients/"+client.ID+"/activities",
		[]byte(body), headers,
	)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create activity = %d headers=%v body=%s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeClientActivityResponse(t, createdRecorder.Body.Bytes())
	if created.ClientID != client.ID || created.Kind != "note" || created.Title != "项目沟通" ||
		created.Body == nil || *created.Body != "第一行\n第二行" || created.OccurredAt != occurredAt ||
		created.CreatedBy.ID != models.BuiltinOwnerActorID || created.CreatedBy.Type != "owner" ||
		created.Version != 1 || created.ClientVersion != 2 || created.DeletedAt != nil {
		t.Fatalf("created activity = %#v", created)
	}

	replay := performRequest(
		router, http.MethodPost, "/api/v1/clients/"+client.ID+"/activities",
		[]byte(body), headers,
	)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("activity replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	if replayed := decodeClientActivityResponse(t, replay.Body.Bytes()); replayed.ID != created.ID {
		t.Fatalf("activity replay id = %q, want %q", replayed.ID, created.ID)
	}
	var count int64
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM client_activities").Scan(&count); err != nil {
		t.Fatalf("count client activities: %v", err)
	}
	if count != 1 {
		t.Fatalf("activity count after replay = %d, want 1", count)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/activities?page=1&page_size=10", nil, nil)
	if listed.Code != http.StatusOK || listed.Header().Get("ETag") != `"2"` {
		t.Fatalf("list activities = %d headers=%v body=%s", listed.Code, listed.Header(), listed.Body.String())
	}
	var listEnvelope struct {
		Data []clientActivityResponse `json:"data"`
		Meta struct {
			Page          int   `json:"page"`
			PageSize      int   `json:"page_size"`
			Total         int64 `json:"total"`
			ClientVersion int64 `json:"client_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data) != 1 || listEnvelope.Meta.Total != 1 || listEnvelope.Meta.ClientVersion != 2 {
		t.Fatalf("activity list = %#v", listEnvelope)
	}

	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/client-activities/"+created.ID, []byte(`{"title":"新标题"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing activity version = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	updatedRecorder := performRequest(
		router, http.MethodPatch, "/api/v1/client-activities/"+created.ID,
		[]byte(`{"kind":"meeting","title":"复盘会议","body":"确认下一步"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updatedRecorder.Code != http.StatusOK || updatedRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("update activity = %d headers=%v body=%s", updatedRecorder.Code, updatedRecorder.Header(), updatedRecorder.Body.String())
	}
	updated := decodeClientActivityResponse(t, updatedRecorder.Body.Bytes())
	if updated.Kind != "meeting" || updated.Title != "复盘会议" || updated.Body == nil || *updated.Body != "确认下一步" ||
		updated.Version != 2 || updated.ClientVersion != 3 {
		t.Fatalf("updated activity = %#v", updated)
	}
	stale := performRequest(
		router, http.MethodPatch, "/api/v1/client-activities/"+created.ID,
		[]byte(`{"title":"旧窗口覆盖"}`), map[string]string{"If-Match": `"1"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale activity update = %d: %s", stale.Code, stale.Body.String())
	}

	withoutConfirmation := performRequest(
		router, http.MethodDelete, "/api/v1/client-activities/"+created.ID,
		[]byte(`{"reason":"录入重复"}`), map[string]string{"If-Match": `"2"`},
	)
	if withoutConfirmation.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirmation.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed activity delete = %d: %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}
	deletedRecorder := performRequest(
		router, http.MethodDelete, "/api/v1/client-activities/"+created.ID+"?confirm=true",
		[]byte(`{"reason":"  录入重复  "}`), map[string]string{"If-Match": `"2"`},
	)
	if deletedRecorder.Code != http.StatusOK || deletedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("delete activity = %d headers=%v body=%s", deletedRecorder.Code, deletedRecorder.Header(), deletedRecorder.Body.String())
	}
	deleted := decodeClientActivityResponse(t, deletedRecorder.Body.Bytes())
	if deleted.Body != nil || deleted.DeletedAt == nil || deleted.DeleteReason == nil || *deleted.DeleteReason != "录入重复" ||
		deleted.Version != 3 || deleted.ClientVersion != 4 {
		t.Fatalf("deleted activity = %#v", deleted)
	}

	activeList := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/activities", nil, nil)
	if activeList.Code != http.StatusOK || !jsonListDataIsEmpty(t, activeList.Body.Bytes()) {
		t.Fatalf("active activity list after delete = %d: %s", activeList.Code, activeList.Body.String())
	}
	deletedList := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/activities?include_deleted=true", nil, nil)
	if deletedList.Code != http.StatusOK || jsonListDataIsEmpty(t, deletedList.Body.Bytes()) {
		t.Fatalf("deleted activity history = %d: %s", deletedList.Code, deletedList.Body.String())
	}
	detail := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID, nil, nil)
	clientAfterDelete := decodeClientResponse(t, detail.Body.Bytes())
	if clientAfterDelete.Version != 4 || clientAfterDelete.LatestActivityAt != nil {
		t.Fatalf("client after activity deletion = %#v", clientAfterDelete)
	}

	deletedUpdate := performRequest(
		router, http.MethodPatch, "/api/v1/client-activities/"+created.ID,
		[]byte(`{"title":"不能修改"}`), map[string]string{"If-Match": `"3"`},
	)
	if deletedUpdate.Code != http.StatusConflict || responseErrorCode(t, deletedUpdate.Body.Bytes()) != "CLIENT_ACTIVITY_DELETED" {
		t.Fatalf("deleted activity update = %d: %s", deletedUpdate.Code, deletedUpdate.Body.String())
	}
}

func TestClientActivityValidationAndReadOnlySystemReference(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Validation Client"}`, nil)
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)

	for name, body := range map[string]string{
		"system kind": `{"kind":"system_reference","title":"System","body":"Body","occurred_at":"2026-08-28T08:00:00Z"}`,
		"empty body":  `{"kind":"note","title":"Title","body":"  ","occurred_at":"2026-08-28T08:00:00Z"}`,
		"future":      fmt.Sprintf(`{"kind":"meeting","title":"Future","body":"Body","occurred_at":%q}`, future),
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/activities", []byte(body), nil)
			if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "VALIDATION_ERROR" {
				t.Fatalf("validation = %d: %s", response.Code, response.Body.String())
			}
		})
	}

	systemID := "018f0000-0000-7000-8000-000000001899"
	if err := store.DB.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			source_type, source_id, version, created_at, updated_at
		) VALUES (?, ?, 'system_reference', 'Project completed', NULL, ?, ?, 'workflow_event', 'event-1', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, systemID, client.ID, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), models.BuiltinSystemActorID).Error; err != nil {
		t.Fatalf("seed system reference: %v", err)
	}
	readOnly := performRequest(
		router, http.MethodPatch, "/api/v1/client-activities/"+systemID,
		[]byte(`{"title":"Mutate system fact"}`), map[string]string{"If-Match": `"1"`},
	)
	if readOnly.Code != http.StatusConflict || responseErrorCode(t, readOnly.Body.Bytes()) != "CLIENT_ACTIVITY_READ_ONLY" {
		t.Fatalf("system activity update = %d: %s", readOnly.Code, readOnly.Body.String())
	}
	badFilter := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/activities?include_deleted=1", nil, nil)
	if badFilter.Code != http.StatusBadRequest || responseErrorCode(t, badFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("activity bad filter = %d: %s", badFilter.Code, badFilter.Body.String())
	}
}

func jsonListDataIsEmpty(t *testing.T, body []byte) bool {
	t.Helper()
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	return len(envelope.Data) == 0
}
