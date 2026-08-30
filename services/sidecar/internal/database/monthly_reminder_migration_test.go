package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMonthlyReminderMigrationPreservesExistingSeriesAndAddsAnchor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v39-monthly-reminder.db")
	v39 := openDatabaseAtVersion(t, path, 39)
	const reminderID = "018f0000-0000-7000-8000-000000004001"
	if _, err := v39.Exec(`
		INSERT INTO reminders(
			id, source_entity_type, title, summary, priority, trigger_at, status,
			source_event_key, created_by_actor_id, series_id, recurrence_type,
			recurrence_interval, recurrence_timezone, occurrence_number,
			version, created_at, updated_at
		) VALUES (?, 'manual', '既有每周提醒', '迁移前说明', 'P1',
			'2099-01-31T17:30:00Z', 'scheduled', ?, ?, ?, 'weekly', 2,
			'America/Tijuana', 7, 3, '2026-08-29T00:00:00Z', '2026-08-29T01:00:00Z')
	`, reminderID, "reminder:"+reminderID+":due", builtinOwnerActorID, reminderID); err != nil {
		t.Fatalf("seed v39 Reminder: %v", err)
	}
	if err := v39.Close(); err != nil {
		t.Fatalf("close v39 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v39 Reminder database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 47 {
		t.Fatalf("SchemaVersion = %d, want 47", store.SchemaVersion)
	}
	var title, summary, priority, triggerAt, recurrenceType, timezone string
	var recurrenceInterval, occurrenceNumber, anchorDay, version int
	if err := store.DB.Raw(`
		SELECT title, summary, priority, trigger_at, recurrence_type,
		       recurrence_interval, recurrence_timezone, occurrence_number,
		       recurrence_anchor_day, version
		FROM reminders WHERE id = ?
	`, reminderID).Row().Scan(
		&title, &summary, &priority, &triggerAt, &recurrenceType,
		&recurrenceInterval, &timezone, &occurrenceNumber, &anchorDay, &version,
	); err != nil {
		t.Fatalf("read migrated Reminder: %v", err)
	}
	if title != "既有每周提醒" || summary != "迁移前说明" || priority != "P1" ||
		triggerAt != "2099-01-31T17:30:00Z" || recurrenceType != "weekly" ||
		recurrenceInterval != 2 || timezone != "America/Tijuana" ||
		occurrenceNumber != 7 || anchorDay != 1 || version != 3 {
		t.Fatalf("migrated Reminder changed: title=%q summary=%q priority=%q trigger=%q recurrence=%q/%d/%q occurrence=%d anchor=%d version=%d",
			title, summary, priority, triggerAt, recurrenceType, recurrenceInterval,
			timezone, occurrenceNumber, anchorDay, version)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestMonthlyReminderMigrationConstrainsAnchorAndRestoresGuards(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "monthly-reminder-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	const reminderID = "018f0000-0000-7000-8000-000000004011"
	insert := `
		INSERT INTO reminders(
			id, source_entity_type, title, summary, priority, trigger_at, status,
			source_event_key, created_by_actor_id, series_id, recurrence_type,
			recurrence_interval, recurrence_timezone, occurrence_number,
			recurrence_anchor_day, version, created_at, updated_at
		) VALUES (?, 'manual', '每月月末提醒', '', 'P2', '2099-01-31T17:30:00Z',
			'scheduled', ?, ?, ?, ?, 1, 'America/Tijuana', 1, ?, 1,
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	if err := store.DB.Exec(insert, reminderID, "reminder:"+reminderID+":due", builtinOwnerActorID, reminderID, "monthly", 31).Error; err != nil {
		t.Fatalf("insert valid monthly Reminder: %v", err)
	}
	const invalidID = "018f0000-0000-7000-8000-000000004012"
	if err := store.DB.Exec(insert, invalidID, "reminder:"+invalidID+":due", builtinOwnerActorID, invalidID, "weekly", 31).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_RECURRENCE_INVALID") {
		t.Fatalf("weekly anchor error = %v, want recurrence rejection", err)
	}
	if err := store.DB.Exec(`DELETE FROM reminders WHERE id = ?`, reminderID).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("hard delete error = %v, want guard rejection", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
