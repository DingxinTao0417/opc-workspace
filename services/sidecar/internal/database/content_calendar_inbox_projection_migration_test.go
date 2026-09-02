package database

import (
	"path/filepath"
	"testing"
)

func TestContentCalendarInboxProjectionMigrationGuardsVersionedSources(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v37-to-v38.db")
	v37 := openDatabaseAtVersion(t, databasePath, 37)
	if err := v37.Close(); err != nil {
		t.Fatalf("close v37 fixture: %v", err)
	}
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v37 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 54 {
		t.Fatalf("SchemaVersion = %d, want 54", store.SchemaVersion)
	}
	const contentID = "018f0000-0000-7000-8000-000000003801"
	const inboxID = "018f0000-0000-7000-8000-000000003802"
	const scheduledAt = "2026-09-01T09:00:00Z"
	const sourceKey = "content:" + contentID + ":publish_due:1"
	if _, err := store.SQL.Exec(`
		INSERT INTO content_items(id, title, platform, status, scheduled_at, scheduled_timezone, manual_order, version, created_at, updated_at)
		VALUES (?, 'Local release', 'blog', 'scheduled', ?, 'Asia/Shanghai', 10, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, contentID, scheduledAt); err != nil {
		t.Fatalf("seed content item: %v", err)
	}
	payload := `{"content_item_id":"` + contentID + `","event_type":"publish_due","content_version":1,"scheduled_at":"` + scheduledAt + `","scheduled_timezone":"Asia/Shanghai"}`
	if _, err := store.SQL.Exec(`
		INSERT INTO inbox_items(id, kind, title, summary, source_entity_type, source_entity_id, source_event_key, priority, status, resolution_policy, due_at, payload_json, version, created_at, updated_at)
		VALUES (?, 'event', '待发布：Local release', '平台：blog', 'content_item', ?, ?, 'P1', 'open', 'manual', ?, ?, 1, '2026-09-01T09:00:00Z', '2026-09-01T09:00:00Z')
	`, inboxID, contentID, sourceKey, scheduledAt, payload); err != nil {
		t.Fatalf("insert valid content Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO inbox_items(id, kind, title, source_entity_type, source_entity_id, source_event_key, payload_json, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000003803', 'event', '错误来源', 'content_item', ?, 'content:wrong:publish_due:1', ?, '2026-09-01T09:00:00Z', '2026-09-01T09:00:00Z')
	`, contentID, payload); err == nil {
		t.Fatal("mismatched content Inbox source unexpectedly accepted")
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET source_deleted_at = '2026-09-02T00:00:00Z' WHERE id = ?`, inboxID); err == nil {
		t.Fatal("active content Inbox source unexpectedly marked deleted")
	}
	if _, err := store.SQL.Exec(`DELETE FROM content_items WHERE id = ?`, contentID); err == nil {
		t.Fatal("content item with live Inbox source unexpectedly deleted")
	}
	if _, err := store.SQL.Exec(`
		UPDATE inbox_items
		SET status = 'resolved', triaged_at = '2026-09-02T00:00:00Z', resolved_by_actor_id = ?, resolved_at = '2026-09-02T00:00:00Z', resolution_reason = 'source archived', resolution_mode = 'manual', version = 2, updated_at = '2026-09-02T00:00:00Z'
		WHERE id = ?
	`, builtinOwnerActorID, inboxID); err != nil {
		t.Fatalf("resolve content Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET source_deleted_at = '2026-09-02T00:00:00Z', version = 3, updated_at = '2026-09-02T00:00:00Z' WHERE id = ?`, inboxID); err != nil {
		t.Fatalf("mark terminal content Inbox source deleted: %v", err)
	}
	if _, err := store.SQL.Exec(`DELETE FROM content_items WHERE id = ?`, contentID); err != nil {
		t.Fatalf("delete coordinated content item: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
