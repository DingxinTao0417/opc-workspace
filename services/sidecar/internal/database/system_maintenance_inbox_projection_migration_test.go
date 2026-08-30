package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemMaintenanceInboxProjectionMigrationGuardsIncidentSnapshots(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v25-to-v26.db")
	v25 := openDatabaseAtVersion(t, databasePath, 25)
	const preservedID = "018f0000-0000-7000-8000-000000002500"
	if _, err := v25.Exec(`
		INSERT INTO inbox_items (
			id, kind, title, summary, source_entity_type, priority, status,
			resolution_policy, payload_json, version, created_at, updated_at
		) VALUES (?, 'manual', 'Keep existing item', '', 'manual', 'P2', 'open', 'manual', '{}', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, preservedID); err != nil {
		t.Fatalf("seed v25 Inbox Item: %v", err)
	}
	if err := v25.Close(); err != nil {
		t.Fatalf("close v25 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v25 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 50 {
		t.Fatalf("SchemaVersion = %d, want 50", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 1 {
		t.Fatalf("migration changed Inbox count = %d, want 1", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'"); got != 0 {
		t.Fatalf("migration invented %d system maintenance Inbox Items", got)
	}

	const sourceID = "backup:create"
	const occurredAt = "2026-08-28T12:00:00.000000000Z"
	payload := `{"component":"backup","operation":"create","failure_code":"backup_create_failed","occurred_at":"` + occurredAt + `","message":"Unable to create a verified local backup."}`
	insert := `INSERT INTO inbox_items (
		id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
		priority, status, resolution_policy, payload_json, version, created_at, updated_at
	) VALUES (?, 'event', 'Local backup needs attention', 'Unable to create a verified local backup.',
		'system_maintenance', ?, ?, 'P1', 'open', 'manual', ?, 1, ?, ?)`
	if _, err := store.SQL.Exec(insert, "018f0000-0000-7000-8000-000000002601", sourceID, "bad-key", payload, occurredAt, occurredAt); err == nil || !strings.Contains(err.Error(), "INVALID_SYSTEM_MAINTENANCE_INBOX_SOURCE") {
		t.Fatalf("invalid system maintenance key error = %v", err)
	}
	key := "system:backup:create:018f0000-0000-7000-8000-000000002602"
	if _, err := store.SQL.Exec(insert, "018f0000-0000-7000-8000-000000002601", sourceID, key, payload, occurredAt, occurredAt); err != nil {
		t.Fatalf("insert valid system maintenance source: %v", err)
	}
	if _, err := store.SQL.Exec(insert, "018f0000-0000-7000-8000-000000002603", sourceID, "system:backup:create:018f0000-0000-7000-8000-000000002604", payload, occurredAt, occurredAt); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("second active incident error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET payload_json = '{}' WHERE id = ?", "018f0000-0000-7000-8000-000000002601"); err == nil || !strings.Contains(err.Error(), "SYSTEM_MAINTENANCE_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("mutate system maintenance identity error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = ? WHERE id = ?", occurredAt, "018f0000-0000-7000-8000-000000002601"); err == nil || !strings.Contains(err.Error(), "SYSTEM_MAINTENANCE_INBOX_SOURCE_DELETE_FORBIDDEN") {
		t.Fatalf("delete system maintenance source error = %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE inbox_items SET
			status = 'resolved', triaged_at = ?, resolved_by_actor_id = ?,
			resolved_at = ?, resolution_reason = 'acknowledged', resolution_mode = 'manual'
		WHERE id = ?
	`, occurredAt, "00000000-0000-5000-8000-000000000001", occurredAt, "018f0000-0000-7000-8000-000000002601"); err != nil {
		t.Fatalf("resolve system maintenance incident: %v", err)
	}
	if _, err := store.SQL.Exec(insert, "018f0000-0000-7000-8000-000000002605", sourceID, "system:backup:create:018f0000-0000-7000-8000-000000002606", payload, occurredAt, occurredAt); err != nil {
		t.Fatalf("insert later incident after resolve: %v", err)
	}
}
