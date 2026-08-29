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

func TestReminderAPICreatesAndEditsRecurringSchedule(t *testing.T) {
	router := newTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"每周复盘","trigger_at":"2099-08-30T09:00:00+08:00",
		"recurrence_type":"daily","recurrence_interval":2,"recurrence_timezone":"Asia/Shanghai"
	}`), map[string]string{"Idempotency-Key": "recurring-reminder-create"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create recurring Reminder = %d: %s", created.Code, created.Body.String())
	}
	var first reminderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode recurring create: %v", err)
	}
	if first.Data.SeriesID != first.Data.ID || first.Data.RecurrenceType != "daily" ||
		first.Data.RecurrenceInterval != 2 || first.Data.RecurrenceTimezone != "Asia/Shanghai" ||
		first.Data.OccurrenceNumber != 1 || first.Data.RecurrenceAnchorDay != 1 {
		t.Fatalf("created recurrence facts = %#v", first.Data)
	}

	updated := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{
		"recurrence_type":"weekly","recurrence_interval":3,"recurrence_timezone":"America/Los_Angeles"
	}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("update recurring Reminder = %d: %s", updated.Code, updated.Body.String())
	}
	var changed reminderEnvelope
	if err := json.Unmarshal(updated.Body.Bytes(), &changed); err != nil {
		t.Fatalf("decode recurring update: %v", err)
	}
	if changed.Data.RecurrenceType != "weekly" || changed.Data.RecurrenceInterval != 3 ||
		changed.Data.RecurrenceTimezone != "America/Los_Angeles" || changed.Data.Version != 2 {
		t.Fatalf("updated recurrence facts = %#v", changed.Data)
	}

	weekday := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"工作日提醒","trigger_at":"2099-09-01T09:00:00+08:00",
		"recurrence_type":"weekdays","recurrence_interval":1,"recurrence_timezone":"Asia/Shanghai"
	}`), nil)
	if weekday.Code != http.StatusCreated {
		t.Fatalf("create weekday Reminder = %d: %s", weekday.Code, weekday.Body.String())
	}
	var weekdayReminder reminderEnvelope
	if err := json.Unmarshal(weekday.Body.Bytes(), &weekdayReminder); err != nil {
		t.Fatalf("decode weekday create: %v", err)
	}
	if weekdayReminder.Data.RecurrenceType != "weekdays" || weekdayReminder.Data.RecurrenceAnchorDay != 1 {
		t.Fatalf("weekday recurrence facts = %#v", weekdayReminder.Data)
	}

	invalid := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"无效重复提醒","trigger_at":"2099-08-30T09:00:00Z",
		"recurrence_type":"daily","recurrence_interval":0,"recurrence_timezone":"private/not-a-zone"
	}`), nil)
	assertAPIError(t, invalid, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

func TestReminderAPIMonthlyScheduleDerivesLocalAnchorDay(t *testing.T) {
	router := newTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"月末结账提醒","trigger_at":"2099-01-31T09:00:00-08:00",
		"recurrence_type":"monthly","recurrence_interval":1,"recurrence_timezone":"America/Los_Angeles"
	}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create monthly Reminder = %d: %s", created.Code, created.Body.String())
	}
	var first reminderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode monthly create: %v", err)
	}
	if first.Data.RecurrenceType != "monthly" || first.Data.RecurrenceInterval != 1 ||
		first.Data.RecurrenceTimezone != "America/Los_Angeles" || first.Data.RecurrenceAnchorDay != 31 {
		t.Fatalf("monthly recurrence facts = %#v", first.Data)
	}

	updated := performRequest(router, http.MethodPatch, "/api/v1/reminders/"+first.Data.ID, []byte(`{
		"trigger_at":"2099-04-30T09:00:00-07:00"
	}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("update monthly anchor = %d: %s", updated.Code, updated.Body.String())
	}
	var changed reminderEnvelope
	if err := json.Unmarshal(updated.Body.Bytes(), &changed); err != nil {
		t.Fatalf("decode monthly update: %v", err)
	}
	if changed.Data.RecurrenceAnchorDay != 30 || changed.Data.Version != 2 {
		t.Fatalf("updated monthly anchor = %#v", changed.Data)
	}

	invalid := performRequest(router, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"无效每月提醒","trigger_at":"2099-01-31T09:00:00Z",
		"recurrence_type":"monthly","recurrence_interval":1,"recurrence_timezone":"Local"
	}`), nil)
	assertAPIError(t, invalid, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

func TestMonthlyReminderOccurrenceClampsShortMonthAndReturnsToAnchor(t *testing.T) {
	reminder := models.Reminder{
		TriggerAt: "2026-01-31T17:00:00Z", RecurrenceType: "monthly",
		RecurrenceInterval: 1, RecurrenceTimezone: "America/Los_Angeles",
		RecurrenceAnchorDay: 31,
	}
	february, advances, err := nextReminderOccurrence(reminder, time.Date(2026, 1, 31, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next February occurrence: %v", err)
	}
	if got := formatInboxTimestamp(february.UTC()); got != "2026-02-28T17:00:00.000000000Z" || advances != 1 {
		t.Fatalf("February occurrence = %s advances=%d", got, advances)
	}
	march, advances, err := nextReminderOccurrence(reminder, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next March occurrence: %v", err)
	}
	if got := formatInboxTimestamp(march.UTC()); got != "2026-03-31T16:00:00.000000000Z" || advances != 2 {
		t.Fatalf("March occurrence = %s advances=%d", got, advances)
	}
}

func TestWeekdayReminderOccurrenceSkipsWeekendAndOfflineBacklog(t *testing.T) {
	reminder := models.Reminder{
		TriggerAt: "2026-01-02T17:00:00Z", RecurrenceType: "weekdays",
		RecurrenceInterval: 1, RecurrenceTimezone: "America/Los_Angeles",
		RecurrenceAnchorDay: 1,
	}
	monday, advances, err := nextReminderOccurrence(reminder, time.Date(2026, 1, 3, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next weekday after weekend: %v", err)
	}
	if got := formatInboxTimestamp(monday.UTC()); got != "2026-01-05T17:00:00.000000000Z" || advances != 1 {
		t.Fatalf("Monday occurrence = %s advances=%d", got, advances)
	}
	thursday, advances, err := nextReminderOccurrence(reminder, time.Date(2026, 1, 7, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("next weekday after backlog: %v", err)
	}
	if got := formatInboxTimestamp(thursday.UTC()); got != "2026-01-08T17:00:00.000000000Z" || advances != 4 {
		t.Fatalf("Thursday occurrence = %s advances=%d", got, advances)
	}
}

func TestRecurringReminderProjectionPreservesWallClockAndSkipsOfflineBacklog(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "recurring-projection.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	const reminderID = "018f0000-0000-7000-8000-000000003221"
	current := models.Reminder{
		ID: reminderID, SourceEntityType: "manual", Title: "每天当地九点半", Summary: "DST 后仍保持当地时间",
		Priority: "P1", TriggerAt: "2026-03-07T17:30:00.000000000Z", Status: "scheduled",
		SourceEventKey: "reminder:" + reminderID + ":due", CreatedByActorID: models.BuiltinOwnerActorID,
		SeriesID: reminderID, RecurrenceType: "daily", RecurrenceInterval: 1,
		RecurrenceTimezone: "America/Los_Angeles", OccurrenceNumber: 1,
		Version: 1, CreatedAt: "2026-03-07T16:00:00.000000000Z", UpdatedAt: "2026-03-07T16:00:00.000000000Z",
	}
	if err := store.DB.Create(&current).Error; err != nil {
		t.Fatalf("seed recurring Reminder: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueReminders(context.Background()); err != nil {
		t.Fatalf("project recurring Reminder: %v", err)
	}
	if err := service.projectDueReminders(context.Background()); err != nil {
		t.Fatalf("repeat recurring projection: %v", err)
	}

	var fired models.Reminder
	if err := store.DB.First(&fired, "id = ?", reminderID).Error; err != nil || fired.Status != "fired" {
		t.Fatalf("fired occurrence = %#v err=%v", fired, err)
	}
	var next models.Reminder
	if err := store.DB.Where("series_id = ? AND status = 'scheduled'", reminderID).First(&next).Error; err != nil {
		t.Fatalf("load next occurrence: %v", err)
	}
	if next.ID == reminderID || next.TriggerAt != "2026-03-09T16:30:00.000000000Z" ||
		next.OccurrenceNumber != 3 || next.RecurrenceType != "daily" || next.RecurrenceTimezone != "America/Los_Angeles" {
		t.Fatalf("next occurrence = %#v", next)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'reminder' AND source_entity_id = ?", 1, reminderID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE series_id = ?", 2, reminderID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'reminder' AND aggregate_id = ? AND action = 'reminder_recurrence_scheduled'", 1, next.ID)
}

func TestBusinessImportRejectsInvalidReminderTimezoneDuringPreflight(t *testing.T) {
	source := newTestAPI(t)
	created := performRequest(source, http.MethodPost, "/api/v1/reminders", []byte(`{
		"title":"导出重复提醒","trigger_at":"2099-08-30T09:00:00Z",
		"recurrence_type":"daily","recurrence_interval":1,"recurrence_timezone":"UTC"
	}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create export Reminder = %d: %s", created.Code, created.Body.String())
	}
	packageData := emptyBusinessExportFixture(t, source)
	found := false
	for tableIndex := range packageData.Tables {
		table := &packageData.Tables[tableIndex]
		if table.Name != "reminders" {
			continue
		}
		for columnIndex, column := range table.Columns {
			if column == "recurrence_timezone" {
				table.Rows[0][columnIndex] = "private/not-a-zone"
				found = true
			}
		}
	}
	if !found {
		t.Fatal("exported Reminder recurrence timezone was not found")
	}
	body, err := json.Marshal(packageData)
	if err != nil {
		t.Fatalf("encode invalid import fixture: %v", err)
	}
	target := newTestAPI(t)
	preview := performRequest(target, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	assertAPIError(t, preview, http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID")
}
