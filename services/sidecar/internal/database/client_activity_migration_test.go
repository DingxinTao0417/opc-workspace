package database

import (
	"path/filepath"
	"testing"
)

const (
	v17ActivityClientID = "018f0000-0000-7000-8000-000000001801"
	v18ActivityID       = "018f0000-0000-7000-8000-000000001802"
)

func TestClientActivitiesMigrationUpgradesV17WithoutInventingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v17-to-v18.db")
	v17 := openDatabaseAtVersion(t, databasePath, 17)
	if _, err := v17.Exec(`
		INSERT INTO clients(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Existing Client', 'active', 1, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')
	`, v17ActivityClientID); err != nil {
		t.Fatalf("seed v17 client: %v", err)
	}
	if err := v17.Close(); err != nil {
		t.Fatalf("close v17 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v17 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 43 {
		t.Fatalf("SchemaVersion = %d, want 43", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_activities"); got != 0 {
		t.Fatalf("migration invented %d client activities", got)
	}

	for _, name := range []string{"idx_client_activities_timeline", "idx_client_activities_kind"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name); got != 1 {
			t.Fatalf("client activity index %s count = %d, want 1", name, got)
		}
	}
	for _, name := range []string{
		"client_activities_immutable_identity",
		"client_activities_terminal_delete",
		"client_activities_bump_client_after_insert",
		"client_activities_bump_client_after_update",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", name); got != 1 {
			t.Fatalf("client activity trigger %s count = %d, want 1", name, got)
		}
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			version, created_at, updated_at
		) VALUES (?, ?, 'note', 'Kickoff note', 'Agreed on scope', '2026-08-28T09:00:00Z', ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v18ActivityID, v17ActivityClientID, "00000000-0000-5000-8000-000000000001"); err != nil {
		t.Fatalf("insert client activity: %v", err)
	}
	assertClientVersion(t, store.SQL, v17ActivityClientID, 2)

	if _, err := store.SQL.Exec("UPDATE client_activities SET body = 'Updated', version = version + 1 WHERE id = ?", v18ActivityID); err != nil {
		t.Fatalf("update client activity: %v", err)
	}
	assertClientVersion(t, store.SQL, v17ActivityClientID, 3)

	if _, err := store.SQL.Exec("UPDATE client_activities SET client_id = ? WHERE id = ?", "018f0000-0000-7000-8000-000000001899", v18ActivityID); err == nil {
		t.Fatal("client activity identity update unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			deleted_at, version, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000001803', ?, 'note', 'Invalid', 'Body', CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v17ActivityClientID, "00000000-0000-5000-8000-000000000001"); err == nil {
		t.Fatal("partial deletion facts unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		UPDATE client_activities
		SET deleted_at = CURRENT_TIMESTAMP,
			deleted_by_actor_id = ?,
			delete_reason = 'duplicate',
			version = version + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, "00000000-0000-5000-8000-000000000001", v18ActivityID); err != nil {
		t.Fatalf("soft delete client activity: %v", err)
	}
	assertClientVersion(t, store.SQL, v17ActivityClientID, 4)
	if _, err := store.SQL.Exec("UPDATE client_activities SET title = 'Mutated' WHERE id = ?", v18ActivityID); err == nil {
		t.Fatal("deleted client activity mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("DELETE FROM clients WHERE id = ?", v17ActivityClientID); err != nil {
		t.Fatalf("delete client with cascading activities: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_activities WHERE client_id = ?", v17ActivityClientID); got != 0 {
		t.Fatalf("client activity count after client delete = %d, want 0", got)
	}
	assertForeignKey(t, store.SQL, "client_activities", "client_id", "clients", "CASCADE")
	assertForeignKey(t, store.SQL, "client_activities", "created_by_actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}
