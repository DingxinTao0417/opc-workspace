package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

const (
	projectNoteProjectID = "018f0000-0000-7000-8000-000000002101"
	projectNoteID        = "018f0000-0000-7000-8000-000000002102"
	projectNoteOwnerID   = "00000000-0000-5000-8000-000000000001"
)

func TestProjectNotesMigrationUpgradesV20WithoutInventingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v20-to-v21.db")
	v20 := openDatabaseAtVersion(t, databasePath, 20)
	if _, err := v20.Exec(`
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Existing Project', 'in_progress', 3, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')
	`, projectNoteProjectID); err != nil {
		t.Fatalf("seed v20 project: %v", err)
	}
	if err := v20.Close(); err != nil {
		t.Fatalf("close v20 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v20 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 32 {
		t.Fatalf("SchemaVersion = %d, want 32", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM project_notes"); got != 0 {
		t.Fatalf("migration invented %d project notes", got)
	}

	for _, name := range []string{
		"project_notes_immutable_identity",
		"project_notes_terminal_delete",
		"project_notes_bump_project_after_insert",
		"project_notes_bump_project_after_update",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", name); got != 1 {
			t.Fatalf("project note trigger %s count = %d, want 1", name, got)
		}
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO project_notes(
			id, project_id, title, body, occurred_at, created_by_actor_id,
			version, created_at, updated_at
		) VALUES (?, ?, 'Kickoff note', 'Agreed on scope', '2026-08-28T09:00:00Z', ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, projectNoteID, projectNoteProjectID, projectNoteOwnerID); err != nil {
		t.Fatalf("insert project note: %v", err)
	}
	assertProjectVersion(t, store.SQL, projectNoteProjectID, 4)

	if _, err := store.SQL.Exec("UPDATE project_notes SET body = 'Updated', version = version + 1 WHERE id = ?", projectNoteID); err != nil {
		t.Fatalf("update project note: %v", err)
	}
	assertProjectVersion(t, store.SQL, projectNoteProjectID, 5)
	if _, err := store.SQL.Exec("UPDATE project_notes SET project_id = ? WHERE id = ?", "018f0000-0000-7000-8000-000000002199", projectNoteID); err == nil {
		t.Fatal("project note identity update unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO project_notes(
			id, project_id, title, body, occurred_at, created_by_actor_id,
			deleted_at, version, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000002103', ?, 'Invalid', 'Body', CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, projectNoteProjectID, projectNoteOwnerID); err == nil {
		t.Fatal("partial deletion facts unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		UPDATE project_notes
		SET deleted_at = CURRENT_TIMESTAMP,
			deleted_by_actor_id = ?,
			delete_reason = 'duplicate',
			version = version + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, projectNoteOwnerID, projectNoteID); err != nil {
		t.Fatalf("soft delete project note: %v", err)
	}
	assertProjectVersion(t, store.SQL, projectNoteProjectID, 6)
	if _, err := store.SQL.Exec("UPDATE project_notes SET title = 'Mutated' WHERE id = ?", projectNoteID); err == nil {
		t.Fatal("deleted project note mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", projectNoteProjectID); err != nil {
		t.Fatalf("delete project with cascading notes: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM project_notes WHERE project_id = ?", projectNoteProjectID); got != 0 {
		t.Fatalf("project note count after project delete = %d, want 0", got)
	}
	assertForeignKey(t, store.SQL, "project_notes", "project_id", "projects", "CASCADE")
	assertForeignKey(t, store.SQL, "project_notes", "created_by_actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}

func assertProjectVersion(t *testing.T, db *sql.DB, projectID string, want int64) {
	t.Helper()
	var version int64
	if err := db.QueryRow("SELECT version FROM projects WHERE id = ?", projectID).Scan(&version); err != nil {
		t.Fatalf("read project version: %v", err)
	}
	if version != want {
		t.Fatalf("project version = %d, want %d", version, want)
	}
}
