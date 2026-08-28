package database

import (
	"path/filepath"
	"testing"
)

const (
	v20LinkClientID  = "018f0000-0000-7000-8000-000000002001"
	v20LinkPersonID  = "018f0000-0000-7000-8000-000000002002"
	v20OtherPersonID = "018f0000-0000-7000-8000-000000002003"
	v20LinkID        = "018f0000-0000-7000-8000-000000002004"
	v20OtherLinkID   = "018f0000-0000-7000-8000-000000002005"
)

func TestClientActorLinksMigrationUpgradesV19WithoutInventingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v19-to-v20.db")
	v19 := openDatabaseAtVersion(t, databasePath, 19)
	if _, err := v19.Exec(`
		INSERT INTO clients(id, name, contact_name, status, version, created_at, updated_at)
		VALUES (?, 'Linked Client', 'Casey', 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v20LinkClientID); err != nil {
		t.Fatalf("seed v19 client: %v", err)
	}
	if _, err := v19.Exec(`
		INSERT INTO actors(id, type, display_name, status, is_builtin, notes, metadata_json, version, created_at, updated_at)
		VALUES
			(?, 'person', 'Casey', 'active', 0, '', '{}', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			(?, 'person', 'Taylor', 'active', 0, '', '{}', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v20LinkPersonID, v20OtherPersonID); err != nil {
		t.Fatalf("seed v19 people: %v", err)
	}
	if err := v19.Close(); err != nil {
		t.Fatalf("close v19 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v19 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 23 {
		t.Fatalf("SchemaVersion = %d, want 23", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_actor_links"); got != 0 {
		t.Fatalf("migration invented %d client actor links", got)
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO client_actor_links(id, client_id, actor_id, role, linked_by_actor_id, linked_at)
		VALUES (?, ?, ?, 'contact', ?, CURRENT_TIMESTAMP)
	`, v20LinkID, v20LinkClientID, v20LinkPersonID, attachmentOwnerID); err != nil {
		t.Fatalf("insert client actor link: %v", err)
	}
	assertClientVersion(t, store.SQL, v20LinkClientID, 2)

	if _, err := store.SQL.Exec(`
		INSERT INTO client_actor_links(id, client_id, actor_id, role, linked_by_actor_id, linked_at)
		VALUES (?, ?, ?, 'contact', ?, CURRENT_TIMESTAMP)
	`, v20OtherLinkID, v20LinkClientID, v20OtherPersonID, attachmentOwnerID); err == nil {
		t.Fatal("second active contact link unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("UPDATE actors SET status = 'inactive' WHERE id = ?", v20LinkPersonID); err == nil {
		t.Fatal("linked person deactivation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("UPDATE client_actor_links SET actor_id = ? WHERE id = ?", v20OtherPersonID, v20LinkID); err == nil {
		t.Fatal("client actor link identity mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("DELETE FROM client_actor_links WHERE id = ?", v20LinkID); err == nil {
		t.Fatal("client actor link hard delete while client exists unexpectedly succeeded")
	}

	if _, err := store.SQL.Exec(`
		UPDATE client_actor_links
		SET unlinked_at = CURRENT_TIMESTAMP,
			unlinked_by_actor_id = ?,
			unlink_reason = 'contact changed'
		WHERE id = ?
	`, attachmentOwnerID, v20LinkID); err != nil {
		t.Fatalf("unlink client actor: %v", err)
	}
	assertClientVersion(t, store.SQL, v20LinkClientID, 3)
	if _, err := store.SQL.Exec("UPDATE client_actor_links SET unlink_reason = 'rewritten' WHERE id = ?", v20LinkID); err == nil {
		t.Fatal("client actor link history mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("UPDATE actors SET status = 'inactive' WHERE id = ?", v20LinkPersonID); err != nil {
		t.Fatalf("deactivate person after unlink: %v", err)
	}

	if _, err := store.SQL.Exec("DELETE FROM clients WHERE id = ?", v20LinkClientID); err != nil {
		t.Fatalf("delete client aggregate with link history: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_actor_links WHERE client_id = ?", v20LinkClientID); got != 0 {
		t.Fatalf("client actor link count after client delete = %d, want 0", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM actors WHERE id = ?", v20LinkPersonID); got != 1 {
		t.Fatalf("person count after client delete = %d, want 1", got)
	}
	assertForeignKey(t, store.SQL, "client_actor_links", "client_id", "clients", "CASCADE")
	assertForeignKey(t, store.SQL, "client_actor_links", "actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}
