package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if store.SchemaVersion != 54 {
		t.Fatalf("SchemaVersion = %d, want 54", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}
	assertTaskArtifactInboxGapMigrationMarker(t, store.SQL, v23TaskID, v23SubmissionID, v23ArtifactID)

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

func TestTaskArtifactInboxGapMigrationDoesNotMarkPostProjectionArtifactWithoutInbox(t *testing.T) {
	const (
		taskID       = "018f0000-0000-7000-8000-000000002311"
		submissionID = "018f0000-0000-7000-8000-000000002312"
		artifactID   = "018f0000-0000-7000-8000-000000002313"
	)
	databasePath := filepath.Join(t.TempDir(), "v23-to-v51.db")
	v23 := openDatabaseAtVersion(t, databasePath, 23)
	var createdAt string
	if err := v23.QueryRow(`
		SELECT strftime('%Y-%m-%dT%H:%M:%SZ', datetime(applied_at, '+1 day'))
		FROM schema_migrations
		WHERE version = 23 AND name = '023_task_artifact_inbox_projection.sql'
	`).Scan(&createdAt); err != nil {
		t.Fatalf("read projection migration time: %v", err)
	}
	seedTaskArtifactInboxGapMigrationFixture(t, v23, taskID, submissionID, artifactID, createdAt)
	if err := v23.Close(); err != nil {
		t.Fatalf("close v23 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v23 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 54 {
		t.Fatalf("SchemaVersion = %d, want 54", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*)
		FROM workflow_events
		WHERE action = 'migration_task_artifact_inbox_gap' AND artifact_id = ?
	`, artifactID); got != 0 {
		t.Fatalf("post-projection Artifact migration markers = %d, want 0", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestTaskArtifactInboxGapMigrationMarksRFC3339NanoArtifactWithinProjectionSecond(t *testing.T) {
	const (
		taskID       = "018f0000-0000-7000-8000-000000002321"
		submissionID = "018f0000-0000-7000-8000-000000002322"
		artifactID   = "018f0000-0000-7000-8000-000000002323"
		appliedAt    = "2026-08-30 12:34:56"
		createdAt    = "2026-08-30T12:34:56.999999999Z"
	)
	databasePath := filepath.Join(t.TempDir(), "v23-same-second-to-v51.db")
	v23 := openDatabaseAtVersion(t, databasePath, 23)
	if _, err := v23.Exec(`
		UPDATE schema_migrations
		SET applied_at = ?
		WHERE version = 23 AND name = '023_task_artifact_inbox_projection.sql'
	`, appliedAt); err != nil {
		t.Fatalf("set deterministic projection migration time: %v", err)
	}
	seedTaskArtifactInboxGapMigrationFixture(t, v23, taskID, submissionID, artifactID, createdAt)
	if err := v23.Close(); err != nil {
		t.Fatalf("close v23 same-second fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade same-second v23 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 54 {
		t.Fatalf("SchemaVersion = %d, want 54", store.SchemaVersion)
	}
	assertTaskArtifactInboxGapMigrationMarker(t, store.SQL, taskID, submissionID, artifactID)
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items"); got != 0 {
		t.Fatalf("migration invented %d Inbox Items", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func assertTaskArtifactInboxGapMigrationMarker(
	t *testing.T,
	db *sql.DB,
	taskID, submissionID, artifactID string,
) {
	t.Helper()
	var artifactCreatedAt string
	if err := db.QueryRow("SELECT created_at FROM task_artifacts WHERE id = ?", artifactID).Scan(&artifactCreatedAt); err != nil {
		t.Fatalf("read pre-projection Artifact created_at: %v", err)
	}
	var (
		id, aggregateType, aggregateID, action, actorID string
		assignmentID, agentRunID, requestID             sql.NullString
		gotSubmissionID, gotArtifactID                  sql.NullString
		commandSeq                                      sql.NullInt64
		previousJSON, currentJSON, createdAt            sql.NullString
	)
	err := db.QueryRow(`
		SELECT id, aggregate_type, aggregate_id, action, actor_id,
		       assignment_id, submission_id, artifact_id, agent_run_id,
		       request_id, command_seq, previous_json, current_json, created_at
		FROM workflow_events
		WHERE action = 'migration_task_artifact_inbox_gap' AND artifact_id = ?
	`, artifactID).Scan(
		&id, &aggregateType, &aggregateID, &action, &actorID,
		&assignmentID, &gotSubmissionID, &gotArtifactID, &agentRunID,
		&requestID, &commandSeq, &previousJSON, &currentJSON, &createdAt,
	)
	if err != nil {
		t.Fatalf("read Task Artifact Inbox gap marker: %v", err)
	}
	expectedID := artifactID[:14] + "6" + artifactID[15:]
	expectedCurrent := `{"source":"schema_v51_migration","artifact_id":"` + artifactID +
		`","task_id":"` + taskID + `","submission_id":"` + submissionID +
		`","artifact_created_at":"` + artifactCreatedAt + `","requires_followup":1}`
	_, markerTimeErr := time.Parse(time.RFC3339Nano, createdAt.String)
	if id != expectedID || aggregateType != "task" || aggregateID != taskID ||
		action != "migration_task_artifact_inbox_gap" || actorID != builtinOwnerActorID ||
		assignmentID.Valid || !gotSubmissionID.Valid || gotSubmissionID.String != submissionID ||
		!gotArtifactID.Valid || gotArtifactID.String != artifactID || agentRunID.Valid || requestID.Valid ||
		commandSeq.Valid || previousJSON.Valid || !currentJSON.Valid || currentJSON.String != expectedCurrent ||
		!createdAt.Valid || strings.TrimSpace(createdAt.String) == "" || markerTimeErr != nil {
		t.Fatalf("Task Artifact Inbox gap marker is not exact: id=%q aggregate=%s/%s action=%q actor=%q assignment=%#v submission=%#v artifact=%#v agent=%#v request=%#v command=%#v previous=%#v current=%#v created_at=%#v",
			id, aggregateType, aggregateID, action, actorID, assignmentID, gotSubmissionID, gotArtifactID,
			agentRunID, requestID, commandSeq, previousJSON, currentJSON, createdAt)
	}
	if got := readInt64(t, db, `
		SELECT COUNT(*)
		FROM workflow_events
		WHERE action = 'migration_task_artifact_inbox_gap' AND artifact_id = ?
	`, artifactID); got != 1 {
		t.Fatalf("Task Artifact Inbox gap markers = %d, want 1", got)
	}
}

func seedTaskArtifactInboxGapMigrationFixture(
	t *testing.T,
	db *sql.DB,
	taskID, submissionID, artifactID, createdAt string,
) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO tasks(
			id, title, description, kind, status, review_policy, priority,
			completion_criteria, actual_minutes, version, created_at, updated_at
		) VALUES (?, 'Post-projection delivery', '', 'work', 'in_progress', 'manual', 'P1', '', 0, 1, ?, ?)
	`, taskID, createdAt, createdAt); err != nil {
		t.Fatalf("seed post-projection Task: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO task_submissions(
			id, task_id, sequence, status, summary, submitted_by_actor_id, submitted_at,
			reviewed_by_actor_id, reviewed_at, review_reason, is_inferred
		) VALUES (?, ?, 1, 'changes_requested', 'Post-projection delivery', ?, ?, ?, ?, 'revise', 0)
	`, submissionID, taskID, attachmentOwnerID, createdAt, attachmentOwnerID, createdAt); err != nil {
		t.Fatalf("seed post-projection Submission: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			requires_followup, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, created_at
		) VALUES (?, ?, ?, 1, 'text', 'Post-projection brief', 'body', 1, ?, ?, 'unverified', ?)
	`, artifactID, taskID, submissionID, attachmentOwnerID, attachmentOwnerID, createdAt); err != nil {
		t.Fatalf("seed post-projection Artifact: %v", err)
	}
}
