package database

import (
	"path/filepath"
	"testing"
)

const (
	v22ProjectAttachmentProjectID = "018f0000-0000-7000-8000-000000002201"
	v22ProjectAttachmentClientID  = "018f0000-0000-7000-8000-000000002202"
	v22ProjectAttachmentID        = "018f0000-0000-7000-8000-000000002203"
	v22ProjectAttachmentHash      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestProjectAttachmentsMigrationUpgradesV21WithoutInventingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v21-to-v22.db")
	v21 := openDatabaseAtVersion(t, databasePath, 21)
	if _, err := v21.Exec(`INSERT INTO clients(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Attachment Client', 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, v22ProjectAttachmentClientID); err != nil {
		t.Fatalf("seed v21 Client: %v", err)
	}
	if _, err := v21.Exec(`INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Attachment Project', 'planning', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, v22ProjectAttachmentProjectID); err != nil {
		t.Fatalf("seed v21 Project: %v", err)
	}
	if err := v21.Close(); err != nil {
		t.Fatalf("close v21 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v21 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 48 {
		t.Fatalf("SchemaVersion = %d, want 48", store.SchemaVersion)
	}
	for _, table := range []string{"project_attachments", "project_attachment_deletion_tombstones"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM "+table); got != 0 {
			t.Fatalf("migration invented %d rows in %s", got, table)
		}
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO project_attachments(
			id, project_id, name, relative_path, mime_type, size_bytes, sha256,
			recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES (?, ?, 'delivery.pdf', 'objects/' || ?, 'application/pdf', 42, ?, ?, 'verified', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v22ProjectAttachmentID, v22ProjectAttachmentProjectID, v22ProjectAttachmentID, v22ProjectAttachmentHash, attachmentOwnerID); err != nil {
		t.Fatalf("insert project attachment: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v22ProjectAttachmentProjectID); got != 2 {
		t.Fatalf("Project version after attachment insert = %d, want 2", got)
	}
	if _, err := store.SQL.Exec("UPDATE project_attachments SET name = 'renamed.pdf' WHERE id = ?", v22ProjectAttachmentID); err == nil {
		t.Fatal("project attachment fact mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("DELETE FROM project_attachments WHERE id = ?", v22ProjectAttachmentID); err == nil {
		t.Fatal("project attachment hard delete unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_attachments(
			id, client_id, name, relative_path, mime_type, size_bytes, sha256,
			recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES (?, ?, 'conflict.pdf', 'objects/' || ?, 'application/pdf', 42, ?, ?, 'verified', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v22ProjectAttachmentID, v22ProjectAttachmentClientID, v22ProjectAttachmentID, v22ProjectAttachmentHash, attachmentOwnerID); err == nil {
		t.Fatal("cross-table controlled object id conflict unexpectedly succeeded")
	}

	if _, err := store.SQL.Exec(`
		UPDATE project_attachments
		SET deleted_at = CURRENT_TIMESTAMP, deleted_by_actor_id = ?, delete_reason = 'duplicate'
		WHERE id = ?
	`, attachmentOwnerID, v22ProjectAttachmentID); err != nil {
		t.Fatalf("soft delete project attachment: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v22ProjectAttachmentProjectID); got != 3 {
		t.Fatalf("Project version after attachment delete = %d, want 3", got)
	}
	if _, err := store.SQL.Exec("UPDATE project_attachments SET delete_reason = 'rewritten' WHERE id = ?", v22ProjectAttachmentID); err == nil {
		t.Fatal("deleted project attachment audit mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO project_attachment_deletion_tombstones(
			attachment_id, project_id, relative_path, size_bytes, sha256, deletion_scope, deleted_at
		) VALUES (?, ?, 'objects/' || ?, 42, ?, 'attachment', CURRENT_TIMESTAMP)
	`, v22ProjectAttachmentID, v22ProjectAttachmentProjectID, v22ProjectAttachmentID, v22ProjectAttachmentHash); err != nil {
		t.Fatalf("insert project attachment tombstone: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE project_attachment_deletion_tombstones SET deletion_scope = 'project' WHERE attachment_id = ?", v22ProjectAttachmentID); err == nil {
		t.Fatal("project attachment tombstone mutation unexpectedly succeeded")
	}

	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", v22ProjectAttachmentProjectID); err != nil {
		t.Fatalf("delete Project aggregate with attachments: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM project_attachments WHERE project_id = ?", v22ProjectAttachmentProjectID); got != 0 {
		t.Fatalf("attachment count after Project delete = %d, want 0", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM project_attachment_deletion_tombstones WHERE project_id = ?", v22ProjectAttachmentProjectID); got != 1 {
		t.Fatalf("tombstone count after Project delete = %d, want 1", got)
	}
	assertForeignKey(t, store.SQL, "project_attachments", "project_id", "projects", "CASCADE")
	assertForeignKey(t, store.SQL, "project_attachments", "recorded_by_actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}
