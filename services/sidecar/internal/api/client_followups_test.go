package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func decodeClientFollowupResponse(t *testing.T, body []byte) clientFollowupResponse {
	t.Helper()
	var envelope struct {
		Data clientFollowupResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode client followup response: %v", err)
	}
	return envelope.Data
}

func TestClientFollowupCreateListAndUpdate(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Followup Client"}`, nil)
	actor := createActorForTest(t, router, `{"type":"person","display_name":"Followup Owner"}`, nil)
	body := `{"client_id":"` + client.ID + `","assigned_actor_id":"` + actor.ID + `","scheduled_at":"2026-09-01T09:30:00+08:00","timezone":"Asia/Shanghai","channel":"  phone  ","purpose":"  confirm next milestone  ","notes":"  call before noon  ","priority":"high"}`
	headers := map[string]string{"Idempotency-Key": "client-followup-create-1"}

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(body), headers)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create followup = %d headers=%v body=%s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeClientFollowupResponse(t, createdRecorder.Body.Bytes())
	if created.ClientID != client.ID || created.ClientName != "Followup Client" || created.AssignedActorID != actor.ID ||
		created.AssignedActorName != "Followup Owner" || created.ScheduledAt != "2026-09-01T01:30:00Z" ||
		created.Channel != "phone" || created.Purpose != "confirm next milestone" || created.Notes == nil || *created.Notes != "call before noon" ||
		created.Status != "planned" || created.Priority != "high" || created.Version != 1 || created.ClientVersion != 2 {
		t.Fatalf("created followup = %#v", created)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(body), headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || decodeClientFollowupResponse(t, replayed.Body.Bytes()).ID != created.ID {
		t.Fatalf("create replay = %d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND aggregate_id = ? AND action = 'client_followup_created'", 1, created.ID)

	listed := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/followups?status=planned", nil, nil)
	var listEnvelope struct {
		Data []clientFollowupResponse `json:"data"`
		Meta pageMeta                 `json:"meta"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &listEnvelope) != nil || len(listEnvelope.Data) != 1 || listEnvelope.Meta.Total != 1 {
		t.Fatalf("client followup list = %d: %s", listed.Code, listed.Body.String())
	}

	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/client-followups/"+created.ID,
		[]byte(`{"scheduled_at":"2026-09-02T10:00:00Z","notes":null,"priority":"normal"}`), map[string]string{"If-Match": `"1"`})
	if updatedRecorder.Code != http.StatusOK || updatedRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("update followup = %d headers=%v body=%s", updatedRecorder.Code, updatedRecorder.Header(), updatedRecorder.Body.String())
	}
	updated := decodeClientFollowupResponse(t, updatedRecorder.Body.Bytes())
	if updated.ScheduledAt != "2026-09-02T10:00:00Z" || updated.Notes != nil || updated.Priority != "normal" || updated.Version != 2 || updated.ClientVersion != 3 {
		t.Fatalf("updated followup = %#v", updated)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND aggregate_id = ? AND action = 'client_followup_updated'", 1, created.ID)

	stale := performRequest(router, http.MethodPatch, "/api/v1/client-followups/"+created.ID, []byte(`{"channel":"email"}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale update = %d: %s", stale.Code, stale.Body.String())
	}
}

func TestClientFollowupRejectsUnavailableAssigneeAndInvalidTimezone(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Guard Client"}`, nil)
	invalidTimezone := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(`{"client_id":"`+client.ID+`","assigned_actor_id":"00000000-0000-5000-8000-000000000001","scheduled_at":"`+time.Now().UTC().Format(time.RFC3339)+`","timezone":"Not/AZone","channel":"phone","purpose":"check"}`), nil)
	if invalidTimezone.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalidTimezone.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid timezone = %d: %s", invalidTimezone.Code, invalidTimezone.Body.String())
	}
	actor := createActorForTest(t, router, `{"type":"person","display_name":"Inactive Followup Person"}`, nil)
	if recorder := performRequest(router, http.MethodPatch, "/api/v1/actors/"+actor.ID, []byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"1"`}); recorder.Code != http.StatusOK {
		t.Fatalf("inactivate actor = %d: %s", recorder.Code, recorder.Body.String())
	}
	unavailable := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(`{"client_id":"`+client.ID+`","assigned_actor_id":"`+actor.ID+`","scheduled_at":"2026-09-01T09:00:00Z","timezone":"UTC","channel":"phone","purpose":"check"}`), nil)
	if unavailable.Code != http.StatusUnprocessableEntity || responseErrorCode(t, unavailable.Body.Bytes()) != "CLIENT_FOLLOWUP_ASSIGNEE_UNAVAILABLE" {
		t.Fatalf("unavailable assignee = %d: %s", unavailable.Code, unavailable.Body.String())
	}
}
