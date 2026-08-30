package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWeekdayReminderMigrationPreservesMonthlyAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v40-weekday-reminder.db")
	v40 := openDatabaseAtVersion(t, path, 40)
	const reminderID = "018f0000-0000-7000-8000-000000004101"
	if _, err := v40.Exec(`
		INSERT INTO reminders(
			id, source_entity_type, title, summary, priority, trigger_at, status,
			source_event_key, created_by_actor_id, series_id, recurrence_type,
			recurrence_interval, recurrence_timezone, occurrence_number,
			recurrence_anchor_day, version, created_at, updated_at
		) VALUES (?, 'manual', '既有月末提醒', '', 'P2', '2099-01-31T17:00:00Z',
			'scheduled', ?, ?, ?, 'monthly', 1, 'America/Los_Angeles', 1, 31, 1,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, reminderID, "reminder:"+reminderID+":due", builtinOwnerActorID, reminderID); err != nil {
		t.Fatalf("seed v40 Reminder: %v", err)
	}
	if err := v40.Close(); err != nil {
		t.Fatalf("close v40 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v40 Reminder database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 43 {
		t.Fatalf("SchemaVersion = %d, want 43", store.SchemaVersion)
	}
	var recurrenceType string
	var anchorDay int
	if err := store.DB.Raw(`SELECT recurrence_type, recurrence_anchor_day FROM reminders WHERE id = ?`, reminderID).Row().Scan(&recurrenceType, &anchorDay); err != nil {
		t.Fatalf("read migrated Reminder: %v", err)
	}
	if recurrenceType != "monthly" || anchorDay != 31 {
		t.Fatalf("migrated recurrence = %q anchor=%d", recurrenceType, anchorDay)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestWeekdayReminderMigrationAllowsWeekdaysAndRestoresGuards(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "weekday-reminder-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	const reminderID = "018f0000-0000-7000-8000-000000004111"
	insert := `INSERT INTO reminders(
		id, source_entity_type, title, summary, priority, trigger_at, status,
		source_event_key, created_by_actor_id, series_id, recurrence_type,
		recurrence_interval, recurrence_timezone, occurrence_number,
		recurrence_anchor_day, version, created_at, updated_at
	) VALUES (?, 'manual', '工作日提醒', '', 'P2', '2099-01-05T17:00:00Z',
		'scheduled', ?, ?, ?, 'weekdays', 1, 'America/Los_Angeles', 1, ?, 1,
		CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if err := store.DB.Exec(insert, reminderID, "reminder:"+reminderID+":due", builtinOwnerActorID, reminderID, 1).Error; err != nil {
		t.Fatalf("insert weekday Reminder: %v", err)
	}
	const invalidID = "018f0000-0000-7000-8000-000000004112"
	if err := store.DB.Exec(insert, invalidID, "reminder:"+invalidID+":due", builtinOwnerActorID, invalidID, 31).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_RECURRENCE_INVALID") {
		t.Fatalf("weekday anchor error = %v, want recurrence rejection", err)
	}
	if err := store.DB.Exec(`DELETE FROM reminders WHERE id = ?`, reminderID).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("hard delete error = %v, want guard rejection", err)
	}
}
