package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRecurringReminderMigrationUpgradesV31WithoutChangingOneTimeFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v31-recurring-reminder.db")
	v31 := openDatabaseAtVersion(t, path, 31)
	const reminderID = "018f0000-0000-7000-8000-000000003201"
	if _, err := v31.Exec(`
		INSERT INTO reminders(
			id, source_entity_type, title, summary, priority, trigger_at, status,
			source_event_key, created_by_actor_id, version, created_at, updated_at
		) VALUES (?, 'manual', '既有一次性提醒', '', 'P2', '2099-01-01T00:00:00Z', 'scheduled', ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, reminderID, "reminder:"+reminderID+":due", builtinOwnerActorID); err != nil {
		t.Fatalf("seed v31 Reminder: %v", err)
	}
	if err := v31.Close(); err != nil {
		t.Fatalf("close v31 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v31 Reminder database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 45 {
		t.Fatalf("SchemaVersion = %d, want 45", store.SchemaVersion)
	}
	var seriesID, recurrenceType, recurrenceTimezone string
	var recurrenceInterval, occurrenceNumber int
	if err := store.DB.Raw(`
		SELECT series_id, recurrence_type, recurrence_interval, recurrence_timezone, occurrence_number
		FROM reminders WHERE id = ?
	`, reminderID).Row().Scan(&seriesID, &recurrenceType, &recurrenceInterval, &recurrenceTimezone, &occurrenceNumber); err != nil {
		t.Fatalf("read migrated Reminder recurrence facts: %v", err)
	}
	if seriesID != reminderID || recurrenceType != "none" || recurrenceInterval != 1 || recurrenceTimezone != "UTC" || occurrenceNumber != 1 {
		t.Fatalf("migrated recurrence=(%q,%q,%d,%q,%d)", seriesID, recurrenceType, recurrenceInterval, recurrenceTimezone, occurrenceNumber)
	}
}

func TestRecurringReminderMigrationConstrainsSeriesAndScheduledOccurrence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "recurring-reminder-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	const firstID = "018f0000-0000-7000-8000-000000003211"
	const secondID = "018f0000-0000-7000-8000-000000003212"
	insert := `
		INSERT INTO reminders(
			id, source_entity_type, title, summary, priority, trigger_at, status,
			source_event_key, created_by_actor_id, series_id, recurrence_type,
			recurrence_interval, recurrence_timezone, occurrence_number,
			version, created_at, updated_at
		) VALUES (?, 'manual', '重复提醒约束', '', 'P2', '2099-01-01T00:00:00Z', 'scheduled', ?, ?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	if err := store.DB.Exec(insert, firstID, "reminder:"+firstID+":due", builtinOwnerActorID, firstID, "daily", 1, "Asia/Shanghai", 1).Error; err != nil {
		t.Fatalf("insert valid recurring Reminder: %v", err)
	}
	if err := store.DB.Exec(insert, secondID, "reminder:"+secondID+":due", builtinOwnerActorID, firstID, "daily", 1, "Asia/Shanghai", 2).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("second active occurrence error = %v, want unique series rejection", err)
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("reminders").Where("id = ?", firstID).Update("series_id", secondID).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_SERIES_IDENTITY_IMMUTABLE") {
		t.Fatalf("series identity update error = %v", err)
	}
	if err := store.DB.Exec(insert, secondID, "reminder:"+secondID+":due", builtinOwnerActorID, secondID, "daily", 0, "Asia/Shanghai", 1).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_RECURRENCE_INVALID") {
		t.Fatalf("invalid recurrence interval error = %v", err)
	}
}
