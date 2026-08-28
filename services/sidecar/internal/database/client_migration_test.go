package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

const (
	v9ClientOneID  = "018f0000-0000-7000-8000-000000001001"
	v9ClientTwoID  = "018f0000-0000-7000-8000-000000001002"
	v9ProjectOneID = "018f0000-0000-7000-8000-000000001003"
)

func TestClientFactsMigrationUpgradesV9AndPropagatesAggregateVersions(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v9-to-v10.db")
	v9 := openDatabaseAtVersion(t, databasePath, 9)
	if _, err := v9.Exec(`
		INSERT INTO clients(
			id, name, contact_name, email, phone, notes, status, created_at, updated_at
		) VALUES
			(?, 'Existing Client', NULL, '', '   ', NULL, 'active', '2026-08-20T08:00:00Z', '2026-08-20T08:00:00Z'),
			(?, 'Second Client', 'Contact', 'contact@example.com', '+1 555 100 2000', 'Notes', 'lead', '2026-08-21T08:00:00Z', '2026-08-21T08:00:00Z')
	`, v9ClientOneID, v9ClientTwoID); err != nil {
		t.Fatalf("seed v9 clients: %v", err)
	}
	if _, err := v9.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, 'Existing Project', ?, 'planning', '2026-08-22T08:00:00Z', '2026-08-22T08:00:00Z')
	`, v9ProjectOneID, v9ClientOneID); err != nil {
		t.Fatalf("seed v9 project: %v", err)
	}
	if err := v9.Close(); err != nil {
		t.Fatalf("close v9 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v9 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 25 {
		t.Fatalf("SchemaVersion = %d, want 25", store.SchemaVersion)
	}

	for _, clientID := range []string{v9ClientOneID, v9ClientTwoID} {
		if got := readInt64(t, store.SQL, "SELECT version FROM clients WHERE id = ?", clientID); got != 1 {
			t.Fatalf("migrated client %s version = %d, want 1", clientID, got)
		}
	}
	var nullable struct {
		ContactName sql.NullString
		Email       sql.NullString
		Phone       sql.NullString
		Notes       sql.NullString
	}
	if err := store.SQL.QueryRow(`
		SELECT contact_name, email, phone, notes FROM clients WHERE id = ?
	`, v9ClientOneID).Scan(&nullable.ContactName, &nullable.Email, &nullable.Phone, &nullable.Notes); err != nil {
		t.Fatalf("read migrated nullable fields: %v", err)
	}
	if nullable.ContactName.Valid || nullable.Email.Valid || nullable.Phone.Valid || nullable.Notes.Valid {
		t.Fatalf("migrated nullable fields = %#v, want all NULL", nullable)
	}

	for _, name := range []string{"idx_clients_name", "idx_clients_status", "idx_clients_updated_at"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name); got != 1 {
			t.Fatalf("client query index %s count = %d, want 1", name, got)
		}
	}
	for _, name := range []string{
		"clients_version_after_project_insert",
		"clients_version_after_project_update",
		"clients_version_after_project_delete",
		"projects_version_after_client_name_update",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", name); got != 1 {
			t.Fatalf("aggregate trigger %s count = %d, want 1", name, got)
		}
	}

	const insertedProjectID = "018f0000-0000-7000-8000-000000001004"
	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, 'Inserted Project', ?, 'planning', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, insertedProjectID, v9ClientOneID); err != nil {
		t.Fatalf("insert linked project after migration: %v", err)
	}
	assertClientVersion(t, store.SQL, v9ClientOneID, 2)

	if _, err := store.SQL.Exec("UPDATE projects SET client_id = ? WHERE id = ?", v9ClientTwoID, v9ProjectOneID); err != nil {
		t.Fatalf("move project between clients: %v", err)
	}
	assertClientVersion(t, store.SQL, v9ClientOneID, 3)
	assertClientVersion(t, store.SQL, v9ClientTwoID, 2)

	if _, err := store.SQL.Exec("UPDATE projects SET client_id = client_id WHERE id = ?", v9ProjectOneID); err != nil {
		t.Fatalf("write unchanged project association: %v", err)
	}
	assertClientVersion(t, store.SQL, v9ClientTwoID, 2)

	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", insertedProjectID); err != nil {
		t.Fatalf("delete linked project: %v", err)
	}
	assertClientVersion(t, store.SQL, v9ClientOneID, 4)

	projectVersion := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v9ProjectOneID)
	if _, err := store.SQL.Exec("UPDATE clients SET name = 'Renamed Client', version = version + 1 WHERE id = ?", v9ClientTwoID); err != nil {
		t.Fatalf("rename client: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v9ProjectOneID); got != projectVersion+1 {
		t.Fatalf("project version after client rename = %d, want %d", got, projectVersion+1)
	}

	assertForeignKey(t, store.SQL, "projects", "client_id", "clients", "SET NULL")
	assertForeignKey(t, store.SQL, "invoices", "client_id", "clients", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}

func assertClientVersion(t *testing.T, db *sql.DB, clientID string, want int64) {
	t.Helper()
	if got := readInt64(t, db, "SELECT version FROM clients WHERE id = ?", clientID); got != want {
		t.Fatalf("client %s version = %d, want %d", clientID, got, want)
	}
}
