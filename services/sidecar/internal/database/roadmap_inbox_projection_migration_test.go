package database

import (
	"path/filepath"
	"testing"
)

func TestRoadmapInboxProjectionMigrationGuardsVersionedSources(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v38-to-v39.db")
	v38 := openDatabaseAtVersion(t, databasePath, 38)
	if err := v38.Close(); err != nil {
		t.Fatalf("close v38 fixture: %v", err)
	}
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v38 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 43 {
		t.Fatalf("SchemaVersion = %d, want 43", store.SchemaVersion)
	}
	const milestoneID = "018f0000-0000-7000-8000-000000003911"
	const inboxID = "018f0000-0000-7000-8000-000000003912"
	const sourceKey = "roadmap:" + milestoneID + ":due:1"
	if _, err := store.SQL.Exec(`
		INSERT INTO roadmap_milestones(id, title, year, quarter, target_date, status, manual_order, version, created_at, updated_at)
		VALUES (?, 'Local launch', 2026, 3, '2026-08-29', 'active', 1024, 1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')
	`, milestoneID); err != nil {
		t.Fatalf("seed roadmap milestone: %v", err)
	}
	payload := `{"roadmap_milestone_id":"` + milestoneID + `","event_type":"due","milestone_version":1,"target_date":"2026-08-29","year":2026,"quarter":3}`
	if _, err := store.SQL.Exec(`
		INSERT INTO inbox_items(id, kind, title, summary, source_entity_type, source_entity_id, source_event_key, priority, status, resolution_policy, due_at, payload_json, version, created_at, updated_at)
		VALUES (?, 'event', '里程碑到期：Local launch', '目标日期：2026-08-29', 'roadmap_milestone', ?, ?, 'P1', 'open', 'manual', '2026-08-30T06:59:59Z', ?, 1, '2026-08-29T07:00:00Z', '2026-08-29T07:00:00Z')
	`, inboxID, milestoneID, sourceKey, payload); err != nil {
		t.Fatalf("insert valid Roadmap Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO inbox_items(id, kind, title, source_entity_type, source_entity_id, source_event_key, payload_json, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000003913', 'event', '错误来源', 'roadmap_milestone', ?, 'roadmap:wrong:due:1', ?, '2026-08-29T07:00:00Z', '2026-08-29T07:00:00Z')
	`, milestoneID, payload); err == nil {
		t.Fatal("mismatched Roadmap Inbox source unexpectedly accepted")
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET source_deleted_at = '2026-08-30T00:00:00Z' WHERE id = ?`, inboxID); err == nil {
		t.Fatal("active Roadmap Inbox source unexpectedly marked deleted")
	}
	if _, err := store.SQL.Exec(`DELETE FROM roadmap_milestones WHERE id = ?`, milestoneID); err == nil {
		t.Fatal("roadmap milestone with live Inbox source unexpectedly deleted")
	}
	if _, err := store.SQL.Exec(`
		UPDATE inbox_items
		SET status = 'resolved', triaged_at = '2026-08-30T00:00:00Z', resolved_by_actor_id = ?, resolved_at = '2026-08-30T00:00:00Z', resolution_reason = 'source archived', resolution_mode = 'manual', version = 2, updated_at = '2026-08-30T00:00:00Z'
		WHERE id = ?
	`, builtinOwnerActorID, inboxID); err != nil {
		t.Fatalf("resolve Roadmap Inbox source: %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET source_deleted_at = '2026-08-30T00:00:00Z', version = 3, updated_at = '2026-08-30T00:00:00Z' WHERE id = ?`, inboxID); err != nil {
		t.Fatalf("mark terminal Roadmap Inbox source deleted: %v", err)
	}
	if _, err := store.SQL.Exec(`DELETE FROM roadmap_milestones WHERE id = ?`, milestoneID); err != nil {
		t.Fatalf("delete coordinated roadmap milestone: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
