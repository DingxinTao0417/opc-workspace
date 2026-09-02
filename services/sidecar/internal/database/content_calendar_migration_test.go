package database

import (
	"path/filepath"
	"testing"
)

func TestContentCalendarMigrationCreatesLocalScheduleAndTaskLinks(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v36-to-v37.db")
	v36 := openDatabaseAtVersion(t, databasePath, 36)
	if err := v36.Close(); err != nil {
		t.Fatalf("close v36 fixture: %v", err)
	}
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v36 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 54 {
		t.Fatalf("SchemaVersion = %d, want 54", store.SchemaVersion)
	}
	const projectID = "018f0000-0000-7000-8000-000000003701"
	const taskID = "018f0000-0000-7000-8000-000000003702"
	const contentID = "018f0000-0000-7000-8000-000000003703"
	if _, err := store.SQL.Exec(`INSERT INTO projects(id, name, status, version, created_at, updated_at) VALUES (?, 'Content Project', 'planning', 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')`, projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := store.SQL.Exec(`INSERT INTO tasks(id, title, status, priority, kind, review_policy, actual_minutes, version, created_at, updated_at) VALUES (?, 'Prepare content', 'todo', 'P2', 'work', 'none', 0, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')`, taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO content_items(id, title, platform, status, scheduled_at, scheduled_timezone, project_id, manual_order, version, created_at, updated_at)
		VALUES (?, 'Local launch post', 'blog', 'scheduled', '2026-09-01T09:00:00Z', 'Asia/Shanghai', ?, 10, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, contentID, projectID); err != nil {
		t.Fatalf("insert content item: %v", err)
	}
	if _, err := store.SQL.Exec(`INSERT INTO content_item_tasks(content_item_id, task_id, is_required, linked_at) VALUES (?, ?, 1, '2026-08-29T08:00:00Z')`, contentID, taskID); err != nil {
		t.Fatalf("link content task: %v", err)
	}
	if _, err := store.SQL.Exec(`INSERT INTO content_items(id, title, platform, status, scheduled_at, scheduled_timezone, version, created_at, updated_at) VALUES ('018f0000-0000-7000-8000-000000003704', 'Invalid schedule', 'blog', 'scheduled', '2026-09-01T09:00:00Z', NULL, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')`); err == nil {
		t.Fatal("schedule without timezone unexpectedly accepted")
	}
	if _, err := store.SQL.Exec(`DELETE FROM projects WHERE id = ?`, projectID); err == nil {
		t.Fatal("content-linked project unexpectedly deleted")
	}
	if _, err := store.SQL.Exec(`DELETE FROM content_items WHERE id = ?`, contentID); err != nil {
		t.Fatalf("delete content item: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id = ?", contentID); got != 0 {
		t.Fatalf("cascaded content task links = %d, want 0", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
