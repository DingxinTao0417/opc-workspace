package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type reminderEnvelope struct {
	Data reminderOutput `json:"data"`
}

type reminderListEnvelope struct {
	Data []reminderOutput `json:"data"`
	Meta reminderListMeta `json:"meta"`
}

func TestReminderAPIManagesScheduledLifecycleAndIdempotency(t *testing.T) {
	router := newTestAPI(t)
	body := []byte(`{"title":"复查本地备份","summary":"确认恢复点可用","priority":"P1","trigger_at":"2099-08-30T09:00:00+08:00"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/reminders", body, map[string]string{"Idempotency-Key": "reminder-create-1"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var first reminderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if first.Data.Status != "scheduled" || first.Data.Version != 1 || first.Data.Priority != "P1" ||
		first.Data.TriggerAt != "2099-08-30T01:00:00Z" || first.Data.SourceEntityType != "manual" ||
		first.Data.SourceEntityID != nil || first.Data.SourceEventKey != "reminder:"+first.Data.ID+":due" ||
		len(first.Data.AvailableActions) != 2 {
		t.Fatalf("created Reminder=%#v", first.Data)
	}
	if got := created.Header().Get("ETag"); got != `"1"` {
		t.Fatalf("create ETag=%q want quoted 1", got)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/reminders", body, map[string]string{"Idempotency-Key": "reminder-create-1"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("create replay status=%d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{"title":"另一个提醒","trigger_at":"2099-08-31T01:00:00Z"}`), map[string]string{"Idempotency-Key": "reminder-create-1"})
	assertAPIError(t, conflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")

	list := performRequest(router, http.MethodGet, "/api/v1/reminders?status=scheduled&q="+"%E5%A4%87%E4%BB%BD&sort=trigger_at", nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var page reminderListEnvelope
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if page.Meta.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != first.Data.ID || page.Meta.ServerNow == "" {
		t.Fatalf("list page=%#v", page)
	}

	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{"title":"更新提醒"}`), nil)
	assertAPIError(t, missingVersion, http.StatusPreconditionRequired, "VERSION_REQUIRED")
	updated := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{"title":"复查本地恢复点","trigger_at":"2099-09-01T10:30:00+08:00"}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	var changed reminderEnvelope
	if err := json.Unmarshal(updated.Body.Bytes(), &changed); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if changed.Data.Version != 2 || changed.Data.Title != "复查本地恢复点" || changed.Data.TriggerAt != "2099-09-01T02:30:00Z" {
		t.Fatalf("updated Reminder=%#v", changed.Data)
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{"title":"旧窗口修改"}`), map[string]string{"If-Match": `"1"`})
	assertAPIError(t, stale, http.StatusConflict, "VERSION_CONFLICT")

	cancelBody := []byte(`{"reason":"计划已经取消"}`)
	cancelled := performRequest(router, http.MethodDelete, "/api/v1/reminders/"+first.Data.ID, cancelBody, map[string]string{"If-Match": `"2"`, "Idempotency-Key": "reminder-cancel-1"})
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}
	var terminal reminderEnvelope
	if err := json.Unmarshal(cancelled.Body.Bytes(), &terminal); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if terminal.Data.Status != "cancelled" || terminal.Data.Version != 3 || terminal.Data.CancelReason == nil ||
		*terminal.Data.CancelReason != "计划已经取消" || len(terminal.Data.AvailableActions) != 0 {
		t.Fatalf("cancelled Reminder=%#v", terminal.Data)
	}
	cancelReplay := performRequest(router, http.MethodDelete, "/api/v1/reminders/"+first.Data.ID, cancelBody, map[string]string{"If-Match": `"2"`, "Idempotency-Key": "reminder-cancel-1"})
	if cancelReplay.Code != http.StatusOK || cancelReplay.Header().Get("Idempotency-Replayed") != "true" || cancelReplay.Body.String() != cancelled.Body.String() {
		t.Fatalf("cancel replay status=%d headers=%v body=%s", cancelReplay.Code, cancelReplay.Header(), cancelReplay.Body.String())
	}
	terminalEdit := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{"title":"不能修改终态"}`), map[string]string{"If-Match": `"3"`})
	assertAPIError(t, terminalEdit, http.StatusConflict, "REMINDER_NOT_SCHEDULED")
}

func TestReminderAPIValidatesInputAndRoutes(t *testing.T) {
	router := newTestAPI(t)
	invalid := []struct {
		name string
		body string
		code string
	}{
		{"short title", `{"title":"a","trigger_at":"2099-01-01T00:00:00Z"}`, "VALIDATION_ERROR"},
		{"past trigger", `{"title":"过去提醒","trigger_at":"2020-01-01T00:00:00Z"}`, "VALIDATION_ERROR"},
		{"invalid priority", `{"title":"优先级提醒","priority":"urgent","trigger_at":"2099-01-01T00:00:00Z"}`, "VALIDATION_ERROR"},
		{"unknown field", `{"title":"未知字段提醒","trigger_at":"2099-01-01T00:00:00Z","repeat":true}`, "INVALID_JSON"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(test.body), nil)
			assertAPIError(t, recorder, map[string]int{"INVALID_JSON": http.StatusBadRequest, "VALIDATION_ERROR": http.StatusUnprocessableEntity}[test.code], test.code)
		})
	}
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/reminders?status=unknown", nil, nil), http.StatusBadRequest, "INVALID_FILTER")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/reminders?sort=unknown", nil, nil), http.StatusBadRequest, "INVALID_SORT")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/reminders/not-a-uuid", nil, nil), http.StatusBadRequest, "INVALID_REMINDER_ID")
}

func TestReminderProjectionIsAtomicAndIdempotentAcrossScans(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "projection.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	due := seedReminderForProjection(t, store, "018f0000-0000-7000-8000-000000001421", "到期本地提醒", "2026-08-28T11:59:00.000000000Z", "scheduled")
	seedReminderForProjection(t, store, "018f0000-0000-7000-8000-000000001422", "未来本地提醒", "2026-08-28T12:01:00.000000000Z", "scheduled")
	cancelled := seedReminderForProjection(t, store, "018f0000-0000-7000-8000-000000001423", "已取消本地提醒", "2026-08-28T11:58:00.000000000Z", "scheduled")
	nowText := formatInboxTimestamp(now)
	ownerID := models.BuiltinOwnerActorID
	reason := "不再需要"
	if err := store.DB.Model(&models.Reminder{}).Where("id = ?", cancelled.ID).Updates(map[string]any{
		"status": "cancelled", "cancelled_by_actor_id": ownerID, "cancelled_at": nowText,
		"cancel_reason": reason, "version": 2, "updated_at": nowText,
	}).Error; err != nil {
		t.Fatalf("cancel fixture Reminder: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueReminders(context.Background()); err != nil {
		t.Fatalf("first projection: %v", err)
	}
	if err := service.projectDueReminders(context.Background()); err != nil {
		t.Fatalf("repeat projection: %v", err)
	}
	var fired models.Reminder
	if err := store.DB.First(&fired, "id = ?", due.ID).Error; err != nil {
		t.Fatalf("load fired Reminder: %v", err)
	}
	if fired.Status != "fired" || fired.InboxItemID == nil || fired.FiredAt == nil || fired.Version != 2 {
		t.Fatalf("fired Reminder=%#v", fired)
	}
	var inbox models.InboxItem
	if err := store.DB.First(&inbox, "id = ?", *fired.InboxItemID).Error; err != nil {
		t.Fatalf("load projected Inbox Item: %v", err)
	}
	if inbox.Kind != "reminder" || inbox.SourceEntityType != "reminder" || inbox.SourceEntityID == nil ||
		*inbox.SourceEntityID != due.ID || inbox.SourceEventKey == nil || *inbox.SourceEventKey != due.SourceEventKey ||
		inbox.Title != due.Title || inbox.DueAt == nil || *inbox.DueAt != due.TriggerAt {
		t.Fatalf("projected Inbox Item=%#v", inbox)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ?", 1, due.SourceEventKey)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'reminder' AND aggregate_id = ? AND action = 'reminder_fired'", 1, due.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'created' AND actor_id = ?", 1, inbox.ID, models.BuiltinSystemActorID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE status = 'scheduled'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE status = 'cancelled'", 1)

	if err := store.DB.Exec(`CREATE TRIGGER fail_reminder_fire_event BEFORE INSERT ON workflow_events WHEN NEW.action = 'reminder_fired' BEGIN SELECT RAISE(ABORT, 'TEST_REMINDER_EVENT_FAILURE'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	failing := seedReminderForProjection(t, store, "018f0000-0000-7000-8000-000000001424", "事务回滚提醒", "2026-08-28T11:57:00.000000000Z", "scheduled")
	if err := service.projectDueReminders(context.Background()); err == nil {
		t.Fatal("projection succeeded despite forced event failure")
	}
	var stillScheduled models.Reminder
	if err := store.DB.First(&stillScheduled, "id = ?", failing.ID).Error; err != nil {
		t.Fatalf("load rolled back Reminder: %v", err)
	}
	if stillScheduled.Status != "scheduled" || stillScheduled.InboxItemID != nil || stillScheduled.Version != 1 {
		t.Fatalf("rolled back Reminder=%#v", stillScheduled)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ?", 0, failing.SourceEventKey)
}

func TestRouterProjectsOverdueReminderOnStartupWithoutDuplication(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "startup.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	due := seedReminderForProjection(t, store, "018f0000-0000-7000-8000-000000001431", "启动补偿提醒", "2026-08-28T11:00:00.000000000Z", "scheduled")
	options := Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	}
	first, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatalf("first NewRouter: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first Router: %v", err)
	}
	second, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatalf("second NewRouter: %v", err)
	}
	defer second.Close()
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ?", 1, due.SourceEventKey)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'reminder' AND aggregate_id = ? AND action = 'reminder_fired'", 1, due.ID)
}

func seedReminderForProjection(t *testing.T, store *database.Store, id, title, triggerAt, status string) models.Reminder {
	t.Helper()
	reminder := models.Reminder{
		ID: id, SourceEntityType: "manual", Title: title, Summary: "本地提醒说明",
		Priority: "P2", TriggerAt: triggerAt, Status: status,
		SourceEventKey: "reminder:" + id + ":due", CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: "2026-08-28T10:00:00.000000000Z", UpdatedAt: "2026-08-28T10:00:00.000000000Z",
	}
	if err := store.DB.Create(&reminder).Error; err != nil {
		t.Fatalf("seed Reminder %s: %v", id, err)
	}
	return reminder
}

func assertDatabaseCount(t *testing.T, store *database.Store, query string, want int64, values ...any) {
	t.Helper()
	var got int64
	if err := store.DB.Raw(query, values...).Scan(&got).Error; err != nil {
		t.Fatalf("count query error: %v", err)
	}
	if got != want {
		t.Fatalf("count=%d want=%d query=%s", got, want, query)
	}
}
