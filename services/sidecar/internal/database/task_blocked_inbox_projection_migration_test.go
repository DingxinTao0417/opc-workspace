package database

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	v24TaskID  = "018f0000-0000-7000-8000-000000002401"
	v24InboxID = "018f0000-0000-7000-8000-000000002402"
)

func TestTaskBlockedInboxProjectionMigrationGuardsSourceLifecycle(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v23-to-v24.db")
	v23 := openDatabaseAtVersion(t, databasePath, 23)
	if _, err := v23.Exec(`
		INSERT INTO tasks(
			id, title, description, kind, status, review_policy, priority,
			completion_criteria, actual_minutes, version,
			blocked_reason, blocked_at, blocked_from_status, created_at, updated_at
		) VALUES (?, 'Blocked delivery', '', 'work', 'blocked', 'none', 'P0', '', 0, 4,
			'Waiting for approval', '2026-08-28T10:00:00Z', 'in_progress', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v24TaskID); err != nil {
		t.Fatalf("seed v23 blocked Task: %v", err)
	}
	if err := v23.Close(); err != nil {
		t.Fatalf("close v23 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v23 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 37 {
		t.Fatalf("SchemaVersion = %d, want 37", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}

	insert := `INSERT INTO inbox_items(
		id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
		priority, status, resolution_policy, payload_json, version, created_at, updated_at
	) VALUES (?, 'event', 'Blocked task', '', 'task', ?, ?, 'P0', 'open', 'manual', ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	payload := `{"task_id":"` + v24TaskID + `","task_title":"Blocked delivery","blocked_reason":"Waiting for approval","blocked_at":"2026-08-28T10:00:00Z","blocked_from_status":"in_progress","block_version":4}`
	if _, err := store.SQL.Exec(insert, v24InboxID, v24TaskID, "bad-key", payload); err == nil || !strings.Contains(err.Error(), "INVALID_TASK_BLOCKED_INBOX_SOURCE") {
		t.Fatalf("invalid blocked source identity error = %v", err)
	}
	key := "task:" + v24TaskID + ":blocked:4"
	if _, err := store.SQL.Exec(insert, v24InboxID, v24TaskID, key, payload); err != nil {
		t.Fatalf("insert valid Task blocked Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET payload_json = '{}' WHERE id = ?", v24InboxID); err == nil || !strings.Contains(err.Error(), "TASK_BLOCKED_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("blocked source identity mutation error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP WHERE id = ?", v24InboxID); err == nil || !strings.Contains(err.Error(), "TASK_BLOCKED_INBOX_SOURCE_ACTIVE") {
		t.Fatalf("active blocked source deletion coordination error = %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", v24TaskID); err == nil || !strings.Contains(err.Error(), "TASK_BLOCKED_INBOX_SOURCE_NOT_COORDINATED") {
		t.Fatalf("uncoordinated Task deletion error = %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET
		status = 'resolved', triaged_at = CURRENT_TIMESTAMP,
		resolved_by_actor_id = ?, resolved_at = CURRENT_TIMESTAMP,
		resolution_reason = 'handled', resolution_mode = 'manual',
		version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, attachmentOwnerID, v24InboxID); err != nil {
		t.Fatalf("resolve Task blocked Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = ?", v24InboxID); err != nil {
		t.Fatalf("mark terminal Task source deleted: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", v24TaskID); err != nil {
		t.Fatalf("delete coordinated source Task: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
