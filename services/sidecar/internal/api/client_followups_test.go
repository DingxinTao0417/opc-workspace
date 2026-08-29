package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
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

func TestClientFollowupUpdateArchivesStaleDueInboxSource(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Followup Source Client"}`, nil)
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(`{"client_id":"`+client.ID+`","assigned_actor_id":"00000000-0000-5000-8000-000000000001","scheduled_at":"2026-09-01T09:00:00Z","timezone":"UTC","channel":"phone","purpose":"confirm milestone"}`), nil)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create followup = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeClientFollowupResponse(t, createdRecorder.Body.Bytes())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	key := "followup:" + created.ID + ":due:1"
	source := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: created.Purpose,
		Summary: "客户回访到期", SourceEntityType: clientFollowupInboxSourceType,
		SourceEntityID: &created.ID, SourceEventKey: &key,
		Priority: "P2", Status: "open", ResolutionPolicy: "manual",
		DueAt: &created.ScheduledAt, PayloadJSON: `{}`,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/client-followups/"+created.ID, []byte(`{"purpose":"confirm revised milestone"}`), map[string]string{"If-Match": `"1"`})
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update followup = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	var actual models.InboxItem
	if err := store.DB.First(&actual, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Status != "resolved" || actual.ResolutionReason == nil || *actual.ResolutionReason != "客户回访计划已更新" || actual.Version != 2 {
		t.Fatalf("stale due source = %#v", actual)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_resolved'", 1, source.ID)
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

func TestClientFollowupTerminalTransitionsAndReschedule(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Followup Transition Client"}`, nil)
	create := func(purpose string) clientFollowupResponse {
		recorder := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(`{"client_id":"`+client.ID+`","assigned_actor_id":"00000000-0000-5000-8000-000000000001","scheduled_at":"2026-09-01T09:00:00Z","timezone":"UTC","channel":"phone","purpose":"`+purpose+`"}`), nil)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create transition followup = %d: %s", recorder.Code, recorder.Body.String())
		}
		return decodeClientFollowupResponse(t, recorder.Body.Bytes())
	}
	createOpenDueSource := func(followup clientFollowupResponse) models.InboxItem {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		key := "followup:" + followup.ID + ":due:" + "1"
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: followup.Purpose,
			Summary: "客户回访到期", SourceEntityType: clientFollowupInboxSourceType,
			SourceEntityID: &followup.ID, SourceEventKey: &key,
			Priority: "P2", Status: "open", ResolutionPolicy: "manual",
			DueAt: &followup.ScheduledAt, PayloadJSON: `{}`,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.DB.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		return item
	}
	assertSourceResolved := func(item models.InboxItem, reason string) {
		var actual models.InboxItem
		if err := store.DB.First(&actual, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		if actual.Status != "resolved" || actual.ResolvedByActorID == nil || *actual.ResolvedByActorID != models.BuiltinOwnerActorID || actual.ResolutionReason == nil || *actual.ResolutionReason != reason || actual.ResolutionMode == nil || *actual.ResolutionMode != "manual" || actual.Version != 2 {
			t.Fatalf("resolved client followup source = %#v", actual)
		}
	}

	completed := create("completed")
	completedSource := createOpenDueSource(completed)
	completeRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups/"+completed.ID+"/complete", []byte(`{"result":"confirmed","next_step":"send summary"}`), map[string]string{"If-Match": `"1"`})
	if completeRecorder.Code != http.StatusOK || completeRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("complete followup = %d: %s", completeRecorder.Code, completeRecorder.Body.String())
	}
	completedResult := decodeClientFollowupResponse(t, completeRecorder.Body.Bytes())
	if completedResult.Status != "completed" || completedResult.Result == nil || *completedResult.Result != "confirmed" || completedResult.CompletedAt == nil || completedResult.NextStep == nil || *completedResult.NextStep != "send summary" {
		t.Fatalf("completed followup = %#v", completedResult)
	}
	assertSourceResolved(completedSource, "客户回访已完成")
	terminalEdit := performRequest(router, http.MethodPatch, "/api/v1/client-followups/"+completed.ID, []byte(`{"channel":"email"}`), map[string]string{"If-Match": `"2"`})
	if terminalEdit.Code != http.StatusConflict || responseErrorCode(t, terminalEdit.Body.Bytes()) != "CLIENT_FOLLOWUP_FINAL" {
		t.Fatalf("terminal followup edit = %d: %s", terminalEdit.Code, terminalEdit.Body.String())
	}

	skipped := create("skipped")
	skippedSource := createOpenDueSource(skipped)
	skipRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups/"+skipped.ID+"/skip", []byte(`{"reason":"client asked to wait"}`), map[string]string{"If-Match": `"1"`})
	if skipRecorder.Code != http.StatusOK || decodeClientFollowupResponse(t, skipRecorder.Body.Bytes()).Status != "skipped" {
		t.Fatalf("skip followup = %d: %s", skipRecorder.Code, skipRecorder.Body.String())
	}
	assertSourceResolved(skippedSource, "客户回访已跳过")

	rescheduled := create("reschedule")
	rescheduledSource := createOpenDueSource(rescheduled)
	rescheduleRecorder := performRequest(router, http.MethodPost, "/api/v1/client-followups/"+rescheduled.ID+"/reschedule", []byte(`{"scheduled_at":"2026-09-03T10:00:00+08:00","timezone":"Asia/Shanghai","assigned_actor_id":"00000000-0000-5000-8000-000000000001","channel":"meeting","purpose":"next meeting","priority":"low","reason":"time changed"}`), map[string]string{"If-Match": `"1"`})
	var rescheduleEnvelope struct {
		Data struct {
			Previous clientFollowupResponse `json:"previous"`
			Next     clientFollowupResponse `json:"next"`
		} `json:"data"`
	}
	if rescheduleRecorder.Code != http.StatusOK || json.Unmarshal(rescheduleRecorder.Body.Bytes(), &rescheduleEnvelope) != nil || rescheduleEnvelope.Data.Previous.Status != "cancelled" || rescheduleEnvelope.Data.Next.Status != "planned" || rescheduleEnvelope.Data.Next.RescheduledFromID == nil || *rescheduleEnvelope.Data.Next.RescheduledFromID != rescheduled.ID || rescheduleEnvelope.Data.Next.ScheduledAt != "2026-09-03T02:00:00Z" {
		t.Fatalf("reschedule followup = %d: %s", rescheduleRecorder.Code, rescheduleRecorder.Body.String())
	}
	assertSourceResolved(rescheduledSource, "客户回访已重新安排")

	cancelled := create("cancelled")
	cancelledSource := createOpenDueSource(cancelled)
	withoutConfirm := performRequest(router, http.MethodDelete, "/api/v1/client-followups/"+cancelled.ID, []byte(`{"reason":"no longer needed"}`), map[string]string{"If-Match": `"1"`})
	if withoutConfirm.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed cancellation = %d: %s", withoutConfirm.Code, withoutConfirm.Body.String())
	}
	cancelRecorder := performRequest(router, http.MethodDelete, "/api/v1/client-followups/"+cancelled.ID+"?confirm=true", []byte(`{"reason":"no longer needed"}`), map[string]string{"If-Match": `"1"`})
	if cancelRecorder.Code != http.StatusOK || decodeClientFollowupResponse(t, cancelRecorder.Body.Bytes()).Status != "cancelled" {
		t.Fatalf("cancel followup = %d: %s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	assertSourceResolved(cancelledSource, "客户回访已取消")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND action IN ('client_followup_completed', 'client_followup_skipped', 'client_followup_rescheduled', 'client_followup_reschedule_created', 'client_followup_cancelled')", 5)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND action = 'source_resolved'", 4)
}
