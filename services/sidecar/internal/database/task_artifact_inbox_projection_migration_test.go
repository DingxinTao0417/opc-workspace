package database

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	v23TaskID       = "018f0000-0000-7000-8000-000000002301"
	v23SubmissionID = "018f0000-0000-7000-8000-000000002302"
	v23ArtifactID   = "018f0000-0000-7000-8000-000000002303"
	v23InboxID      = "018f0000-0000-7000-8000-000000002304"
)

func TestTaskArtifactInboxProjectionMigrationGuardsSourceLifecycle(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v22-to-v23.db")
	v22 := openDatabaseAtVersion(t, databasePath, 22)
	if _, err := v22.Exec(`
		INSERT INTO tasks(
			id, title, description, kind, status, review_policy, priority,
			completion_criteria, actual_minutes, version, created_at, updated_at
		) VALUES (?, 'Delivery task', '', 'work', 'in_progress', 'manual', 'P1', '', 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, v23TaskID); err != nil {
		t.Fatalf("seed v22 Task: %v", err)
	}
	if _, err := v22.Exec(`
		INSERT INTO task_submissions(
			id, task_id, sequence, status, summary, submitted_by_actor_id, submitted_at,
			reviewed_by_actor_id, reviewed_at, review_reason, is_inferred
		) VALUES (?, ?, 1, 'changes_requested', 'Delivery', ?, CURRENT_TIMESTAMP, ?, CURRENT_TIMESTAMP, 'revise', 0)
	`, v23SubmissionID, v23TaskID, attachmentOwnerID, attachmentOwnerID); err != nil {
		t.Fatalf("seed v22 Submission: %v", err)
	}
	if _, err := v22.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			requires_followup, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, created_at
		) VALUES (?, ?, ?, 1, 'text', 'Delivery brief', 'body', 1, ?, ?, 'unverified', CURRENT_TIMESTAMP)
	`, v23ArtifactID, v23TaskID, v23SubmissionID, attachmentOwnerID, attachmentOwnerID); err != nil {
		t.Fatalf("seed v22 Artifact: %v", err)
	}
	if err := v22.Close(); err != nil {
		t.Fatalf("close v22 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v22 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 45 {
		t.Fatalf("SchemaVersion = %d, want 45", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}

	insert := `INSERT INTO inbox_items(
		id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
		priority, status, resolution_policy, payload_json, version, created_at, updated_at
	) VALUES (?, 'event', 'Follow delivery', '', 'task_artifact', ?, ?, 'P1', 'open', 'manual', ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	payload := `{"artifact_id":"` + v23ArtifactID + `","artifact_name":"Delivery brief","task_id":"` + v23TaskID + `","submission_id":"` + v23SubmissionID + `"}`
	if _, err := store.SQL.Exec(insert, v23InboxID, v23ArtifactID, "bad-key", payload); err == nil || !strings.Contains(err.Error(), "INVALID_TASK_ARTIFACT_INBOX_SOURCE") {
		t.Fatalf("invalid source identity error = %v", err)
	}
	key := "task-artifact:" + v23ArtifactID + ":followup"
	if _, err := store.SQL.Exec(insert, v23InboxID, v23ArtifactID, key, payload); err != nil {
		t.Fatalf("insert valid projected Inbox Item: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_event_key = 'rewritten' WHERE id = ?", v23InboxID); err == nil || !strings.Contains(err.Error(), "TASK_ARTIFACT_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("source identity mutation error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP WHERE id = ?", v23InboxID); err == nil || !strings.Contains(err.Error(), "TASK_ARTIFACT_INBOX_SOURCE_ACTIVE") {
		t.Fatalf("active source deletion coordination error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE task_artifacts SET deleted_at = CURRENT_TIMESTAMP, deleted_by_actor_id = ?, delete_reason = 'remove' WHERE id = ?", attachmentOwnerID, v23ArtifactID); err == nil || !strings.Contains(err.Error(), "TASK_ARTIFACT_INBOX_SOURCE_NOT_COORDINATED") {
		t.Fatalf("uncoordinated Artifact deletion error = %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE inbox_items SET
		status = 'dismissed', triaged_at = CURRENT_TIMESTAMP,
		dismissed_by_actor_id = ?, dismissed_at = CURRENT_TIMESTAMP,
		dismiss_reason = 'not needed', version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, attachmentOwnerID, v23InboxID); err != nil {
		t.Fatalf("archive projected Inbox Item: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE inbox_items SET source_deleted_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = ?", v23InboxID); err != nil {
		t.Fatalf("mark terminal source deleted: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE task_artifacts SET deleted_at = CURRENT_TIMESTAMP, deleted_by_actor_id = ?, delete_reason = 'remove' WHERE id = ?", attachmentOwnerID, v23ArtifactID); err != nil {
		t.Fatalf("delete coordinated Artifact: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
