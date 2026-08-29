package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCompletionInboxProjectionMigrationGuardsSnapshotsAndDeletion(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v27-to-v28.db")
	v27 := openDatabaseAtVersion(t, databasePath, 27)
	const projectID = "018f0000-0000-7000-8000-000000002800"
	if _, err := v27.Exec(`
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES (?, '迁移前完成项目', 'completed', 4, '2026-08-28T08:00:00Z', '2026-08-28T09:00:00Z')
	`, projectID); err != nil {
		t.Fatalf("seed v27 Project: %v", err)
	}
	if err := v27.Close(); err != nil {
		t.Fatalf("close v27 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v27 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 29 {
		t.Fatalf("SchemaVersion = %d, want 29", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'project_completion'"); got != 0 {
		t.Fatalf("migration invented %d Project completion Inbox Items", got)
	}

	const inboxID = "018f0000-0000-7000-8000-000000002801"
	const completedAt = "2026-08-28T09:00:00Z"
	payload := `{"project_id":"` + projectID + `","project_name":"迁移前完成项目","completed_at":"` + completedAt + `","completion_version":4,"incomplete_task_count":0}`
	insert := `INSERT INTO inbox_items (
		id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
		priority, status, resolution_policy, payload_json, version, created_at, updated_at
	) VALUES (?, 'event', '项目完成待跟进：迁移前完成项目', '请确认后续工作',
		'project_completion', ?, ?, 'P1', 'open', 'manual', ?, 1, ?, ?)`
	if _, err := store.SQL.Exec(insert, inboxID, projectID, "project:"+projectID+":completed:3", payload, completedAt, completedAt); err == nil || !strings.Contains(err.Error(), "INVALID_PROJECT_COMPLETION_INBOX_SOURCE") {
		t.Fatalf("invalid completion key error = %v", err)
	}
	key := "project:" + projectID + ":completed:4"
	if _, err := store.SQL.Exec(insert, inboxID, projectID, key, payload, completedAt, completedAt); err != nil {
		t.Fatalf("insert valid Project completion source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET payload_json = '{}' WHERE id = ?", inboxID); err == nil || !strings.Contains(err.Error(), "PROJECT_COMPLETION_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("mutate completion snapshot error = %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", projectID); err == nil || !strings.Contains(err.Error(), "PROJECT_COMPLETION_INBOX_SOURCE_NOT_COORDINATED") {
		t.Fatalf("delete Project with uncoordinated source error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = ? WHERE id = ?", completedAt, inboxID); err == nil || !strings.Contains(err.Error(), "PROJECT_COMPLETION_INBOX_SOURCE_ACTIVE") {
		t.Fatalf("delete active completion source error = %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE inbox_items SET
			status = 'resolved', triaged_at = ?, resolved_by_actor_id = ?,
			resolved_at = ?, resolution_reason = '已完成收尾', resolution_mode = 'manual'
		WHERE id = ?
	`, completedAt, "00000000-0000-5000-8000-000000000001", completedAt, inboxID); err != nil {
		t.Fatalf("resolve completion source: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = ? WHERE id = ?", completedAt, inboxID); err != nil {
		t.Fatalf("coordinate completion source deletion: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = NULL WHERE id = ?", inboxID); err == nil || !strings.Contains(err.Error(), "PROJECT_COMPLETION_INBOX_SOURCE_DELETION_IMMUTABLE") {
		t.Fatalf("rewrite completion source deletion error = %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", projectID); err != nil {
		t.Fatalf("delete Project after source coordination: %v", err)
	}
}
