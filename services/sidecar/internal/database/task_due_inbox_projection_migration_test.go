package database

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	v25TaskID  = "018f0000-0000-7000-8000-000000002501"
	v25InboxID = "018f0000-0000-7000-8000-000000002502"
)

func TestTaskDueInboxProjectionMigrationGuardsSourceLifecycle(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v24-to-v25.db")
	v24 := openDatabaseAtVersion(t, databasePath, 24)
	if _, err := v24.Exec(`
		INSERT INTO tasks(
			id, title, description, kind, status, review_policy, priority,
			completion_criteria, due_date, actual_minutes, version, created_at, updated_at
		) VALUES (?, 'Due delivery', '', 'work', 'in_progress', 'none', 'P1', '',
			'2026-08-29T12:00:00Z', 0, 3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v25TaskID); err != nil {
		t.Fatalf("seed v24 due Task: %v", err)
	}
	if err := v24.Close(); err != nil {
		t.Fatalf("close v24 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v24 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 41 {
		t.Fatalf("SchemaVersion = %d, want 41", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}

	insert := `INSERT INTO inbox_items(
		id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
		priority, status, resolution_policy, due_at, payload_json, version, created_at, updated_at
	) VALUES (?, 'event', 'Due task', '', 'task_due', ?, ?, 'P1', 'open', 'manual', ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	dueAt := "2026-08-29T12:00:00Z"
	payload := `{"task_id":"` + v25TaskID + `","task_title":"Due delivery","due_at":"` + dueAt + `","projected_at":"2026-08-28T12:00:00Z","due_state":"due_soon","lead_minutes":1440}`
	if _, err := store.SQL.Exec(insert, v25InboxID, v25TaskID, "bad-key", dueAt, payload); err == nil || !strings.Contains(err.Error(), "INVALID_TASK_DUE_INBOX_SOURCE") {
		t.Fatalf("invalid due source identity error = %v", err)
	}
	key := "task:" + v25TaskID + ":due:" + dueAt
	missingStatePayload := `{"task_id":"` + v25TaskID + `","task_title":"Due delivery","due_at":"` + dueAt + `","projected_at":"2026-08-28T12:00:00Z","lead_minutes":1440}`
	if _, err := store.SQL.Exec(insert, v25InboxID, v25TaskID, key, dueAt, missingStatePayload); err == nil || !strings.Contains(err.Error(), "INVALID_TASK_DUE_INBOX_SOURCE") {
		t.Fatalf("incomplete due source payload error = %v", err)
	}
	if _, err := store.SQL.Exec(insert, v25InboxID, v25TaskID, key, dueAt, payload); err != nil {
		t.Fatalf("insert valid Task due Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET due_at = '2026-08-30T12:00:00Z' WHERE id = ?", v25InboxID); err == nil || !strings.Contains(err.Error(), "TASK_DUE_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("due source identity mutation error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP WHERE id = ?", v25InboxID); err == nil || !strings.Contains(err.Error(), "TASK_DUE_INBOX_SOURCE_ACTIVE") {
		t.Fatalf("active due source deletion coordination error = %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", v25TaskID); err == nil || !strings.Contains(err.Error(), "TASK_DUE_INBOX_SOURCE_NOT_COORDINATED") {
		t.Fatalf("uncoordinated Task deletion error = %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET
		status = 'dismissed', triaged_at = CURRENT_TIMESTAMP,
		dismissed_by_actor_id = ?, dismissed_at = CURRENT_TIMESTAMP,
		dismiss_reason = 'handled', version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, attachmentOwnerID, v25InboxID); err != nil {
		t.Fatalf("dismiss Task due Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = ?", v25InboxID); err != nil {
		t.Fatalf("mark terminal Task due source deleted: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", v25TaskID); err != nil {
		t.Fatalf("delete coordinated source Task: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
