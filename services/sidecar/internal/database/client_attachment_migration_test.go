package database

import (
	"path/filepath"
	"testing"
)

const (
	v18AttachmentClientID      = "018f0000-0000-7000-8000-000000001901"
	v18AttachmentOtherClientID = "018f0000-0000-7000-8000-000000001902"
	v18AttachmentActivityID    = "018f0000-0000-7000-8000-000000001903"
	v19AttachmentID            = "018f0000-0000-7000-8000-000000001904"
	attachmentOwnerID          = "00000000-0000-5000-8000-000000000001"
	attachmentHash             = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestClientAttachmentsMigrationUpgradesV18WithoutInventingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v18-to-v19.db")
	v18 := openDatabaseAtVersion(t, databasePath, 18)
	if _, err := v18.Exec(`
		INSERT INTO clients(id, name, status, version, created_at, updated_at)
		VALUES
			(?, 'Attachment Client', 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			(?, 'Other Client', 'active', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v18AttachmentClientID, v18AttachmentOtherClientID); err != nil {
		t.Fatalf("seed v18 clients: %v", err)
	}
	if _, err := v18.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			version, created_at, updated_at
		) VALUES (?, ?, 'note', 'Contract', 'Signed', CURRENT_TIMESTAMP, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v18AttachmentActivityID, v18AttachmentClientID, attachmentOwnerID); err != nil {
		t.Fatalf("seed v18 activity: %v", err)
	}
	if err := v18.Close(); err != nil {
		t.Fatalf("close v18 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v18 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 44 {
		t.Fatalf("SchemaVersion = %d, want 44", store.SchemaVersion)
	}
	for _, table := range []string{"client_attachments", "client_attachment_deletion_tombstones"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM "+table); got != 0 {
			t.Fatalf("migration invented %d rows in %s", got, table)
		}
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO client_attachments(
			id, client_id, activity_id, name, relative_path, mime_type, size_bytes,
			sha256, recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES (?, ?, ?, 'contract.pdf', 'objects/' || ?, 'application/pdf', 42, ?, ?, 'verified', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v19AttachmentID, v18AttachmentClientID, v18AttachmentActivityID, v19AttachmentID, attachmentHash, attachmentOwnerID); err != nil {
		t.Fatalf("insert client attachment: %v", err)
	}
	assertClientVersion(t, store.SQL, v18AttachmentClientID, 3)

	if _, err := store.SQL.Exec(`
		INSERT INTO client_attachments(
			id, client_id, activity_id, name, relative_path, mime_type, size_bytes,
			sha256, recorded_by_actor_id, integrity_status, integrity_checked_at, created_at
		) VALUES ('018f0000-0000-7000-8000-000000001905', ?, ?, 'wrong.txt',
			'objects/018f0000-0000-7000-8000-000000001905', 'text/plain', 1, ?, ?, 'verified', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v18AttachmentOtherClientID, v18AttachmentActivityID, attachmentHash, attachmentOwnerID); err == nil {
		t.Fatal("cross-client activity attachment unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("UPDATE client_attachments SET name = 'renamed.pdf' WHERE id = ?", v19AttachmentID); err == nil {
		t.Fatal("attachment fact mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec("DELETE FROM client_attachments WHERE id = ?", v19AttachmentID); err == nil {
		t.Fatal("attachment hard delete while client exists unexpectedly succeeded")
	}

	if _, err := store.SQL.Exec(`
		UPDATE client_attachments
		SET deleted_at = CURRENT_TIMESTAMP,
			deleted_by_actor_id = ?,
			delete_reason = 'duplicate'
		WHERE id = ?
	`, attachmentOwnerID, v19AttachmentID); err != nil {
		t.Fatalf("soft delete attachment: %v", err)
	}
	assertClientVersion(t, store.SQL, v18AttachmentClientID, 4)
	if _, err := store.SQL.Exec("UPDATE client_attachments SET delete_reason = 'rewritten' WHERE id = ?", v19AttachmentID); err == nil {
		t.Fatal("deleted attachment audit mutation unexpectedly succeeded")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_attachment_deletion_tombstones(
			attachment_id, client_id, relative_path, size_bytes, sha256, deletion_scope, deleted_at
		) VALUES (?, ?, 'objects/' || ?, 42, ?, 'attachment', CURRENT_TIMESTAMP)
	`, v19AttachmentID, v18AttachmentClientID, v19AttachmentID, attachmentHash); err != nil {
		t.Fatalf("insert client attachment tombstone: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE client_attachment_deletion_tombstones SET deletion_scope = 'client' WHERE attachment_id = ?", v19AttachmentID); err == nil {
		t.Fatal("attachment tombstone mutation unexpectedly succeeded")
	}

	if _, err := store.SQL.Exec("DELETE FROM clients WHERE id = ?", v18AttachmentClientID); err != nil {
		t.Fatalf("delete client aggregate with attachments: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_attachments WHERE client_id = ?", v18AttachmentClientID); got != 0 {
		t.Fatalf("attachment count after client delete = %d, want 0", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_attachment_deletion_tombstones WHERE client_id = ?", v18AttachmentClientID); got != 1 {
		t.Fatalf("attachment tombstone count after client delete = %d, want 1", got)
	}
	assertForeignKey(t, store.SQL, "client_attachments", "client_id", "clients", "CASCADE")
	assertForeignKey(t, store.SQL, "client_attachments", "activity_id", "client_activities", "CASCADE")
	assertForeignKey(t, store.SQL, "client_attachments", "recorded_by_actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)
}
