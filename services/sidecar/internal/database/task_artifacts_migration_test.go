package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const (
	v8ArtifactProjectID          = "018f0000-0000-7000-8000-000000000901"
	v8ArtifactParentTaskID       = "018f0000-0000-7000-8000-000000000902"
	v8ArtifactPendingTaskID      = "018f0000-0000-7000-8000-000000000903"
	v8ArtifactBlockedTaskID      = "018f0000-0000-7000-8000-000000000904"
	v8ArtifactAcceptedTaskID     = "018f0000-0000-7000-8000-000000000905"
	v8ArtifactChangesTaskID      = "018f0000-0000-7000-8000-000000000906"
	v8ArtifactWithdrawnTaskID    = "018f0000-0000-7000-8000-000000000907"
	v8ArtifactNoSubmissionTaskID = "018f0000-0000-7000-8000-000000000908"
	v8ArtifactTagID              = "018f0000-0000-7000-8000-000000000909"
	v8ArtifactFocusID            = "018f0000-0000-7000-8000-000000000910"
	v8ArtifactAssignmentID       = "018f0000-0000-7000-8000-000000000911"
	v8ArtifactExistingEventID    = "018f0000-0000-7000-8000-000000000912"
	v8ArtifactIdempotencyKey     = "v8-artifact-snapshot"
)

func TestTaskArtifactsMigrationUpgradesRealV8DatabaseWithoutLosingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v8-to-v9.db")
	v8 := openDatabaseAtVersion(t, databasePath, 8)
	seedV8TaskArtifactsFixture(t, v8)

	wantProjectVersion := readInt64(t, v8, "SELECT version FROM projects WHERE id = ?", v8ArtifactProjectID)
	wantParentVersion := readInt64(t, v8, "SELECT version FROM tasks WHERE id = ?", v8ArtifactParentTaskID)
	wantPendingVersion := readInt64(t, v8, "SELECT version FROM tasks WHERE id = ?", v8ArtifactPendingTaskID)
	var wantPendingUpdatedAt string
	if err := v8.QueryRow("SELECT updated_at FROM tasks WHERE id = ?", v8ArtifactPendingTaskID).Scan(&wantPendingUpdatedAt); err != nil {
		t.Fatalf("read v8 pending task timestamp: %v", err)
	}
	if err := v8.Close(); err != nil {
		t.Fatalf("close v8 artifact fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v8 artifact database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 56 {
		t.Fatalf("SchemaVersion = %d, want 56", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v8ArtifactProjectID); got != wantProjectVersion {
		t.Fatalf("project version changed during v9 migration: got %d want %d", got, wantProjectVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM tasks WHERE id = ?", v8ArtifactParentTaskID); got != wantParentVersion {
		t.Fatalf("parent task version changed during v9 migration: got %d want %d", got, wantParentVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM tasks WHERE id = ?", v8ArtifactPendingTaskID); got != wantPendingVersion {
		t.Fatalf("pending task version changed during v9 migration: got %d want %d", got, wantPendingVersion)
	}
	var gotPendingUpdatedAt string
	if err := store.SQL.QueryRow("SELECT updated_at FROM tasks WHERE id = ?", v8ArtifactPendingTaskID).Scan(&gotPendingUpdatedAt); err != nil {
		t.Fatalf("read migrated pending task timestamp: %v", err)
	}
	if gotPendingUpdatedAt != wantPendingUpdatedAt {
		t.Fatalf("pending task updated_at changed: got %q want %q", gotPendingUpdatedAt, wantPendingUpdatedAt)
	}

	wantStatuses := map[string]string{
		v8ArtifactPendingTaskID:   "pending_review",
		v8ArtifactBlockedTaskID:   "pending_review",
		v8ArtifactAcceptedTaskID:  "accepted",
		v8ArtifactChangesTaskID:   "changes_requested",
		v8ArtifactWithdrawnTaskID: "withdrawn",
	}
	for taskID, wantStatus := range wantStatuses {
		var submission struct {
			ID                 string
			TaskID             string
			Sequence           int64
			Status             string
			Summary            string
			SubmittedByActorID string
			SubmittedAt        string
			ReviewedByActorID  sql.NullString
			ReviewedAt         sql.NullString
			ReviewReason       sql.NullString
			WithdrawnByActorID sql.NullString
			WithdrawnAt        sql.NullString
			IsInferred         int64
			CurrentSubmission  sql.NullString
		}
		if err := store.SQL.QueryRow(`
			SELECT s.id, s.task_id, s.sequence, s.status, s.summary,
			       s.submitted_by_actor_id, s.submitted_at,
			       s.reviewed_by_actor_id, s.reviewed_at, s.review_reason,
			       s.withdrawn_by_actor_id, s.withdrawn_at, s.is_inferred,
			       t.current_submission_id
			FROM task_submissions s
			JOIN tasks t ON t.id = s.task_id
			WHERE s.task_id = ?
		`, taskID).Scan(
			&submission.ID,
			&submission.TaskID,
			&submission.Sequence,
			&submission.Status,
			&submission.Summary,
			&submission.SubmittedByActorID,
			&submission.SubmittedAt,
			&submission.ReviewedByActorID,
			&submission.ReviewedAt,
			&submission.ReviewReason,
			&submission.WithdrawnByActorID,
			&submission.WithdrawnAt,
			&submission.IsInferred,
			&submission.CurrentSubmission,
		); err != nil {
			t.Fatalf("read inferred submission for task %s: %v", taskID, err)
		}
		if submission.TaskID != taskID || submission.Sequence != 1 || submission.Status != wantStatus || submission.Summary != "" || submission.SubmittedByActorID != builtinOwnerActorID || submission.IsInferred != 1 {
			t.Fatalf("inferred submission for task %s = %#v", taskID, submission)
		}
		if !submission.CurrentSubmission.Valid || submission.CurrentSubmission.String != submission.ID {
			t.Fatalf("task %s current submission = %#v, want %s", taskID, submission.CurrentSubmission, submission.ID)
		}
		switch wantStatus {
		case "pending_review":
			if submission.ReviewedByActorID.Valid || submission.ReviewedAt.Valid || submission.ReviewReason.Valid || submission.WithdrawnByActorID.Valid || submission.WithdrawnAt.Valid {
				t.Fatalf("pending inferred submission has terminal facts: %#v", submission)
			}
		case "accepted":
			if !submission.ReviewedByActorID.Valid || submission.ReviewedByActorID.String != builtinOwnerActorID || !submission.ReviewedAt.Valid || submission.ReviewReason.Valid || submission.WithdrawnByActorID.Valid {
				t.Fatalf("accepted inferred submission facts = %#v", submission)
			}
		case "changes_requested":
			if !submission.ReviewedByActorID.Valid || submission.ReviewedByActorID.String != builtinOwnerActorID || !submission.ReviewedAt.Valid || !submission.ReviewReason.Valid || submission.ReviewReason.String != "schema_v9_migration_inferred_changes_requested" || submission.WithdrawnByActorID.Valid {
				t.Fatalf("changes-requested inferred submission facts = %#v", submission)
			}
		case "withdrawn":
			if submission.ReviewedByActorID.Valid || submission.ReviewedAt.Valid || submission.ReviewReason.Valid || !submission.WithdrawnByActorID.Valid || submission.WithdrawnByActorID.String != builtinOwnerActorID || !submission.WithdrawnAt.Valid {
				t.Fatalf("withdrawn inferred submission facts = %#v", submission)
			}
		}
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_submissions"); got != int64(len(wantStatuses)) {
		t.Fatalf("inferred submission count = %d, want %d", got, len(wantStatuses))
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_submissions WHERE task_id = ?", v8ArtifactNoSubmissionTaskID); got != 0 {
		t.Fatalf("manual task without submitted_at received %d inferred submissions", got)
	}
	var noCurrentSubmission sql.NullString
	if err := store.SQL.QueryRow("SELECT current_submission_id FROM tasks WHERE id = ?", v8ArtifactNoSubmissionTaskID).Scan(&noCurrentSubmission); err != nil {
		t.Fatalf("read task without inferred submission: %v", err)
	}
	if noCurrentSubmission.Valid {
		t.Fatalf("task without submitted_at current submission = %q, want NULL", noCurrentSubmission.String)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_artifacts"); got != 0 {
		t.Fatalf("migration invented %d artifacts, want none", got)
	}
	var databaseID, identityCreatedAt string
	if err := store.SQL.QueryRow("SELECT database_id, created_at FROM workspace_identity WHERE singleton = 1").Scan(&databaseID, &identityCreatedAt); err != nil {
		t.Fatalf("read workspace identity: %v", err)
	}
	if len(databaseID) != 36 || strings.ToLower(databaseID) != databaseID || identityCreatedAt == "" {
		t.Fatalf("workspace identity = (%q, %q)", databaseID, identityCreatedAt)
	}
	expectSQLErrorContains(t, store.SQL, "mutate workspace identity", "WORKSPACE_IDENTITY_IMMUTABLE", `
		UPDATE workspace_identity SET database_id = '018f0000-0000-7000-8000-000000000999'
		WHERE singleton = 1
	`)
	expectSQLErrorContains(t, store.SQL, "delete workspace identity", "WORKSPACE_IDENTITY_IMMUTABLE", `
		DELETE FROM workspace_identity WHERE singleton = 1
	`)
	const artifactStoreID = "018f0000-0000-7000-8000-000000000998"
	if _, err := store.SQL.Exec("UPDATE workspace_identity SET artifact_store_id = ? WHERE singleton = 1", artifactStoreID); err != nil {
		t.Fatalf("bind Artifact store identity once: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "rebind Artifact store identity", "WORKSPACE_IDENTITY_IMMUTABLE", `
		UPDATE workspace_identity SET artifact_store_id = '018f0000-0000-7000-8000-000000000997'
		WHERE singleton = 1
	`)

	for taskID := range wantStatuses {
		var action, actorID, status string
		var commandSequence sql.NullInt64
		var inferred bool
		if err := store.SQL.QueryRow(`
			SELECT action, actor_id, command_seq,
			       json_extract(current_json, '$.status'),
			       json_extract(current_json, '$.inferred')
			FROM workflow_events
			WHERE aggregate_type = 'task'
			  AND aggregate_id = ?
			  AND action = 'migration_submission_backfill'
		`, taskID).Scan(&action, &actorID, &commandSequence, &status, &inferred); err != nil {
			t.Fatalf("read migration submission event for task %s: %v", taskID, err)
		}
		if action != "migration_submission_backfill" || actorID != builtinSystemActorID || commandSequence.Valid || status != wantStatuses[taskID] || !inferred {
			t.Fatalf("migration event for task %s = (%q, %q, %#v, %q, %v)", taskID, action, actorID, commandSequence, status, inferred)
		}
	}

	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM task_tags WHERE task_id = '" + v8ArtifactPendingTaskID + "' AND tag_id = '" + v8ArtifactTagID + "'":                  1,
		"SELECT COUNT(*) FROM focus_sessions WHERE id = '" + v8ArtifactFocusID + "' AND task_id = '" + v8ArtifactPendingTaskID + "'":               1,
		"SELECT COUNT(*) FROM task_assignments WHERE id = '" + v8ArtifactAssignmentID + "' AND task_id = '" + v8ArtifactPendingTaskID + "'":        1,
		"SELECT COUNT(*) FROM workflow_events WHERE id = '" + v8ArtifactExistingEventID + "' AND assignment_id = '" + v8ArtifactAssignmentID + "'": 1,
	} {
		if got := readInt64(t, store.SQL, query); got != want {
			t.Fatalf("preserved relationship count for %q = %d, want %d", query, got, want)
		}
	}
	var requestHash, responseBody string
	var responseStatus int
	if err := store.SQL.QueryRow(`
		SELECT request_hash, response_body, response_status
		FROM idempotency_keys
		WHERE key = ? AND endpoint = 'POST /api/v1/tasks'
	`, v8ArtifactIdempotencyKey).Scan(&requestHash, &responseBody, &responseStatus); err != nil {
		t.Fatalf("read preserved v8 idempotency snapshot: %v", err)
	}
	if requestHash != "v8-request-hash" || responseBody != `{"status":"waiting_review","version":17}` || responseStatus != 201 {
		t.Fatalf("v8 idempotency snapshot changed: (%q, %q, %d)", requestHash, responseBody, responseStatus)
	}

	for _, item := range []struct {
		table    string
		from     string
		parent   string
		onDelete string
	}{
		{"task_submissions", "task_id", "tasks", "CASCADE"},
		{"task_submissions", "submitted_by_actor_id", "actors", "RESTRICT"},
		{"tasks", "current_submission_id", "task_submissions", "SET NULL"},
		{"task_artifacts", "task_id", "tasks", "CASCADE"},
		{"task_artifacts", "submission_id", "task_submissions", "CASCADE"},
		{"workflow_events", "submission_id", "task_submissions", "SET NULL"},
		{"workflow_events", "artifact_id", "task_artifacts", "SET NULL"},
	} {
		assertForeignKey(t, store.SQL, item.table, item.from, item.parent, item.onDelete)
	}
	for _, name := range []string{
		"ux_task_submissions_task_sequence",
		"ux_task_submissions_single_pending",
		"ux_task_artifacts_submission_position",
		"idx_artifact_deletion_tombstones_task",
		"idx_tasks_current_submission_id",
		"idx_workflow_events_submission",
		"idx_workflow_events_artifact",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name); got != 1 {
			t.Fatalf("schema v9 index %s count = %d, want 1", name, got)
		}
	}
	for _, name := range []string{
		"projects_version_after_task_update",
		"trg_tasks_parent_cycle_update",
		"trg_tasks_status_transition_update",
		"trg_tasks_current_submission_same_task_update",
		"trg_task_artifacts_submission_same_task_insert",
		"trg_artifact_deletion_tombstones_immutable_update",
		"trg_artifact_deletion_tombstones_immutable_delete",
		"trg_workspace_identity_immutable_update",
		"trg_workspace_identity_immutable_delete",
		"trg_workflow_events_immutable_update",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", name); got != 1 {
			t.Fatalf("preserved/schema v9 trigger %s count = %d, want 1", name, got)
		}
	}
	assertNoForeignKeyViolations(t, store.SQL)
	var integrity string
	if err := store.SQL.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("run integrity_check after v8 to v9 upgrade: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check after v8 to v9 upgrade = %q, want ok", integrity)
	}
}

func TestWorkspaceIdentityPersistsAcrossDatabaseReopen(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "workspace-identity.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open workspace database: %v", err)
	}
	var first string
	if err := store.SQL.QueryRow("SELECT database_id FROM workspace_identity WHERE singleton = 1").Scan(&first); err != nil {
		t.Fatalf("read first workspace identity: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close workspace database: %v", err)
	}
	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatalf("reopen workspace database: %v", err)
	}
	defer reopened.Close()
	var second string
	if err := reopened.SQL.QueryRow("SELECT database_id FROM workspace_identity WHERE singleton = 1").Scan(&second); err != nil {
		t.Fatalf("read reopened workspace identity: %v", err)
	}
	if first == "" || second != first {
		t.Fatalf("workspace identity changed across reopen: first=%q second=%q", first, second)
	}
}

func TestTaskSubmissionAndArtifactConstraints(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "artifact-constraints.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	const (
		taskOneID       = "018f0000-0000-7000-8000-000000000921"
		taskTwoID       = "018f0000-0000-7000-8000-000000000922"
		submissionOneID = "018f0000-0000-7000-8000-000000000923"
		submissionTwoID = "018f0000-0000-7000-8000-000000000924"
		textArtifactID  = "018f0000-0000-7000-8000-000000000925"
		linkArtifactID  = "018f0000-0000-7000-8000-000000000926"
		jsonArtifactID  = "018f0000-0000-7000-8000-000000000927"
		fileArtifactID  = "018f0000-0000-7000-8000-000000000928"
		eventID         = "018f0000-0000-7000-8000-000000000929"
		eventArtifactID = "018f0000-0000-7000-8000-000000000935"
		deleteTaskID    = "018f0000-0000-7000-8000-000000000936"
		deleteSubmitID  = "018f0000-0000-7000-8000-000000000937"
		deleteArtifact  = "018f0000-0000-7000-8000-000000000938"
		deleteAssignID  = "018f0000-0000-7000-8000-000000000939"
		deleteEventID   = "018f0000-0000-7000-8000-000000000940"
	)
	insertWorkflowTask(t, store.SQL, taskOneID, "第一条人工验收任务", "manual")
	insertWorkflowTask(t, store.SQL, taskTwoID, "第二条人工验收任务", "manual")
	insertSubmission(t, store.SQL, submissionOneID, taskOneID, 1)

	expectSQLErrorContains(t, store.SQL, "duplicate pending submission", "UNIQUE constraint failed", `
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at
		) VALUES (?, ?, 2, 'pending_review', ?, '2026-08-27T11:01:00Z')
	`, submissionTwoID, taskOneID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "duplicate task submission sequence", "UNIQUE constraint failed", `
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at,
			reviewed_by_actor_id, reviewed_at
		) VALUES (?, ?, 1, 'accepted', ?, '2026-08-27T11:01:00Z', ?, '2026-08-27T11:02:00Z')
	`, submissionTwoID, taskOneID, builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "changes requested without reason", "CHECK constraint failed", `
		UPDATE task_submissions
		SET status = 'changes_requested', reviewed_by_actor_id = ?, reviewed_at = '2026-08-27T11:02:00Z'
		WHERE id = ?
	`, builtinOwnerActorID, submissionOneID)
	expectSQLErrorContains(t, store.SQL, "mutate submitted summary", "TASK_SUBMISSION_HISTORY_IMMUTABLE", `
		UPDATE task_submissions SET summary = 'rewritten evidence' WHERE id = ?
	`, submissionOneID)

	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 1, 'text', '说明', '有效文本产出', ?, ?)
	`, textArtifactID, taskOneID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert text artifact: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, reference_url,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 2, 'link', '参考链接', 'https://example.test/result', ?, ?)
	`, linkArtifactID, taskOneID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert link artifact: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, structured_json,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 3, 'structured', '结构化摘要', '{"result":"ok"}', ?, ?)
	`, jsonArtifactID, taskOneID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert structured artifact: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (
			?, ?, ?, 4, 'file', '交付文件.pdf', ?,
			'application/pdf', 128, ?, ?, ?, 'verified', '2026-08-27T11:00:00Z'
		)
	`, fileArtifactID, taskOneID, submissionOneID, "objects/"+fileArtifactID, strings.Repeat("a", 64), builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert file artifact: %v", err)
	}

	invalidArtifactID := func(suffix string) string {
		return "018f0000-0000-7000-8000-0000000009" + suffix
	}
	expectSQLErrorContains(t, store.SQL, "artifact points across tasks", "TASK_ARTIFACT_SUBMISSION_MISMATCH", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 5, 'text', '跨任务', '非法', ?, ?)
	`, invalidArtifactID("31"), taskTwoID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "text artifact with a second payload", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text, reference_url,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 5, 'text', '双重载荷', '正文', 'https://example.test', ?, ?)
	`, invalidArtifactID("32"), taskOneID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "structured artifact with array payload", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, structured_json,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 5, 'structured', '数组不是对象', '[]', ?, ?)
	`, invalidArtifactID("33"), taskOneID, submissionOneID, builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "file artifact path traversal", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, ?, 5, 'file', '越界文件', '../outside.bin', 'application/octet-stream', 1, ?, ?, ?, 'verified', '2026-08-27T11:00:00Z')
	`, invalidArtifactID("34"), taskOneID, submissionOneID, strings.Repeat("b", 64), builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "file artifact Windows path traversal", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, ?, 5, 'file', 'Windows越界文件', '..\outside.bin', 'application/octet-stream', 1, ?, ?, ?, 'verified', '2026-08-27T11:00:00Z')
	`, invalidArtifactID("36"), taskOneID, submissionOneID, strings.Repeat("c", 64), builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "file artifact aliases another object", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, ?, 5, 'file', '路径别名', ?, 'application/octet-stream', 1, ?, ?, ?, 'verified', '2026-08-27T11:00:00Z')
	`, invalidArtifactID("37"), taskOneID, submissionOneID, "objects/"+fileArtifactID, strings.Repeat("d", 64), builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "file artifact invalid hash", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, ?, 5, 'file', '坏哈希', ?, 'application/octet-stream', 1, 'XYZ', ?, ?, 'verified', '2026-08-27T11:00:00Z')
	`, invalidArtifactID("35"), taskOneID, submissionOneID, "objects/"+invalidArtifactID("35"), builtinOwnerActorID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "file artifact empty size", "CHECK constraint failed", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, relative_path,
			mime_type, size_bytes, sha256, produced_by_actor_id, recorded_by_actor_id,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, ?, 5, 'file', '空文件', ?, 'application/octet-stream', 0, ?, ?, ?, 'verified', '2026-08-27T11:00:00Z')
	`, invalidArtifactID("38"), taskOneID, submissionOneID, "objects/"+invalidArtifactID("38"), strings.Repeat("e", 64), builtinOwnerActorID, builtinOwnerActorID)
	if _, err := store.SQL.Exec(`
		INSERT INTO artifact_deletion_tombstones(
			artifact_id, task_id, relative_path, size_bytes, sha256, deletion_scope, deleted_at
		) VALUES (?, ?, ?, 128, ?, 'artifact', '2026-08-27T12:00:00Z')
	`, fileArtifactID, taskOneID, "objects/"+fileArtifactID, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("insert Artifact deletion tombstone: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "mutate Artifact deletion tombstone", "ARTIFACT_DELETION_TOMBSTONE_IMMUTABLE", `
		UPDATE artifact_deletion_tombstones SET deletion_scope = 'task' WHERE artifact_id = ?
	`, fileArtifactID)
	expectSQLErrorContains(t, store.SQL, "delete Artifact deletion tombstone", "ARTIFACT_DELETION_TOMBSTONE_IMMUTABLE", `
		DELETE FROM artifact_deletion_tombstones WHERE artifact_id = ?
	`, fileArtifactID)
	expectSQLErrorContains(t, store.SQL, "verified artifact without check time", "CHECK constraint failed", `
		UPDATE task_artifacts SET integrity_status = 'verified' WHERE id = ?
	`, textArtifactID)
	expectSQLErrorContains(t, store.SQL, "partial soft delete", "CHECK constraint failed", `
		UPDATE task_artifacts SET deleted_at = '2026-08-27T12:00:00Z' WHERE id = ?
	`, textArtifactID)
	expectSQLErrorContains(t, store.SQL, "mutate artifact evidence", "TASK_ARTIFACT_FACTS_IMMUTABLE", `
		UPDATE task_artifacts SET content_text = 'rewritten' WHERE id = ?
	`, textArtifactID)
	if _, err := store.SQL.Exec(`
		UPDATE task_artifacts
		SET deleted_at = '2026-08-27T12:00:00Z', deleted_by_actor_id = ?, delete_reason = '用户确认删除'
		WHERE id = ?
	`, builtinOwnerActorID, textArtifactID); err != nil {
		t.Fatalf("soft delete artifact: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "rewrite artifact deletion", "TASK_ARTIFACT_FACTS_IMMUTABLE", `
		UPDATE task_artifacts SET delete_reason = 'rewritten' WHERE id = ?
	`, textArtifactID)

	expectSQLErrorContains(t, store.SQL, "current submission points across tasks", "TASK_CURRENT_SUBMISSION_MISMATCH", `
		UPDATE tasks SET current_submission_id = ? WHERE id = ?
	`, submissionOneID, taskTwoID)
	if _, err := store.SQL.Exec("UPDATE tasks SET current_submission_id = ? WHERE id = ?", submissionOneID, taskOneID); err != nil {
		t.Fatalf("set same-task current submission: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE task_submissions
		SET status = 'accepted', reviewed_by_actor_id = ?, reviewed_at = '2026-08-27T12:30:00Z'
		WHERE id = ?
	`, builtinOwnerActorID, submissionOneID); err != nil {
		t.Fatalf("accept pending submission: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "rewrite accepted submission", "TASK_SUBMISSION_HISTORY_IMMUTABLE", `
		UPDATE task_submissions SET review_reason = 'late rewrite' WHERE id = ?
	`, submissionOneID)
	insertSubmission(t, store.SQL, submissionTwoID, taskOneID, 2)
	if _, err := store.SQL.Exec("UPDATE tasks SET current_submission_id = ? WHERE id = ?", submissionTwoID, taskOneID); err != nil {
		t.Fatalf("advance current submission pointer: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "current submission points to an older batch", "TASK_CURRENT_SUBMISSION_MISMATCH", `
		UPDATE tasks SET current_submission_id = ? WHERE id = ?
	`, submissionOneID, taskOneID)
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 1, 'text', '第二批产出', '与第二批提交一致', ?, ?)
	`, eventArtifactID, taskOneID, submissionTwoID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert second-submission artifact: %v", err)
	}

	expectSQLErrorContains(t, store.SQL, "workflow submission points across tasks", "WORKFLOW_EVENT_SUBMISSION_MISMATCH", `
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, submission_id
		) VALUES (?, 'task', ?, 'invalid_cross_task_submission', ?)
	`, invalidArtifactID("41"), taskTwoID, submissionTwoID)
	expectSQLErrorContains(t, store.SQL, "workflow artifact points across tasks", "WORKFLOW_EVENT_ARTIFACT_MISMATCH", `
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, artifact_id
		) VALUES (?, 'task', ?, 'invalid_cross_task_artifact', ?)
	`, invalidArtifactID("42"), taskTwoID, eventArtifactID)
	expectSQLErrorContains(t, store.SQL, "workflow artifact belongs to another submission", "WORKFLOW_EVENT_ARTIFACT_SUBMISSION_MISMATCH", `
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, submission_id, artifact_id
		) VALUES (?, 'task', ?, 'invalid_cross_submission_artifact', ?, ?)
	`, invalidArtifactID("43"), taskOneID, submissionTwoID, linkArtifactID)

	if _, err := store.SQL.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id,
			submission_id, artifact_id, current_json, command_seq, created_at
		) VALUES (
			?, 'task', ?, 'task_output_submitted', ?, ?, ?,
			'{"status":"pending_review"}', 1, '2026-08-27T13:00:00Z'
		)
	`, eventID, taskOneID, builtinOwnerActorID, submissionTwoID, eventArtifactID); err != nil {
		t.Fatalf("insert artifact workflow event: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "rewrite workflow event", "WORKFLOW_EVENT_IMMUTABLE", `
		UPDATE workflow_events SET action = 'rewritten' WHERE id = ?
	`, eventID)
	expectSQLErrorContains(t, store.SQL, "retarget workflow event submission", "WORKFLOW_EVENT_IMMUTABLE", `
		UPDATE workflow_events SET submission_id = ? WHERE id = ?
	`, submissionOneID, eventID)
	expectSQLErrorContains(t, store.SQL, "delete workflow event", "WORKFLOW_EVENT_IMMUTABLE", `
		DELETE FROM workflow_events WHERE id = ?
	`, eventID)
	expectSQLErrorContains(t, store.SQL, "manually null workflow assignment reference", "WORKFLOW_EVENT_IMMUTABLE", `
		UPDATE workflow_events SET artifact_id = NULL WHERE id = ?
	`, eventID)
	expectSQLErrorContains(t, store.SQL, "hard delete Artifact while Task exists", "TASK_ARTIFACT_HARD_DELETE_FORBIDDEN", `
		DELETE FROM task_artifacts WHERE id = ?
	`, eventArtifactID)
	expectSQLErrorContains(t, store.SQL, "hard delete Submission while Task exists", "TASK_SUBMISSION_HARD_DELETE_FORBIDDEN", `
		DELETE FROM task_submissions WHERE id = ?
	`, submissionTwoID)

	insertWorkflowTask(t, store.SQL, deleteTaskID, "验证任务聚合级联删除", "manual")
	insertSubmission(t, store.SQL, deleteSubmitID, deleteTaskID, 1)
	if _, err := store.SQL.Exec("UPDATE tasks SET current_submission_id = ? WHERE id = ?", deleteSubmitID, deleteTaskID); err != nil {
		t.Fatalf("set delete fixture current submission: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 1, 'text', '待级联产出', '保留事件快照', ?, ?)
	`, deleteArtifact, deleteTaskID, deleteSubmitID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert delete fixture artifact: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES (?, ?, ?, 'assignee', ?, '2026-08-27T14:00:00Z')
	`, deleteAssignID, deleteTaskID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert delete fixture assignment: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id, assignment_id,
			submission_id, artifact_id, current_json, created_at
		) VALUES (
			?, 'task', ?, 'task_output_submitted', ?, ?, ?, ?,
			'{"assignment":"preserved","submission":"preserved","artifact":"preserved"}',
			'2026-08-27T14:01:00Z'
		)
	`, deleteEventID, deleteTaskID, builtinOwnerActorID, deleteAssignID, deleteSubmitID, deleteArtifact); err != nil {
		t.Fatalf("insert delete fixture event: %v", err)
	}
	for column, label := range map[string]string{
		"assignment_id": "Assignment",
		"submission_id": "Submission",
		"artifact_id":   "Artifact",
	} {
		expectSQLErrorContains(t, store.SQL, "manually null live "+label+" event reference", "WORKFLOW_EVENT_IMMUTABLE", `
			UPDATE workflow_events SET `+column+` = NULL WHERE id = ?
		`, deleteEventID)
	}
	expectSQLErrorContains(t, store.SQL, "hard delete aggregate Submission while Task exists", "TASK_SUBMISSION_HARD_DELETE_FORBIDDEN", `
		DELETE FROM task_submissions WHERE id = ?
	`, deleteSubmitID)
	expectSQLErrorContains(t, store.SQL, "hard delete aggregate Artifact while Task exists", "TASK_ARTIFACT_HARD_DELETE_FORBIDDEN", `
		DELETE FROM task_artifacts WHERE id = ?
	`, deleteArtifact)
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", deleteTaskID); err != nil {
		t.Fatalf("delete complete Task aggregate: %v", err)
	}
	for table, id := range map[string]string{
		"tasks":            deleteTaskID,
		"task_assignments": deleteAssignID,
		"task_submissions": deleteSubmitID,
		"task_artifacts":   deleteArtifact,
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM "+table+" WHERE id = ?", id); got != 0 {
			t.Fatalf("Task aggregate delete left %s row %s", table, id)
		}
	}
	var deletedAssignmentRef, deletedSubmissionRef, deletedArtifactRef sql.NullString
	var preservedSnapshot string
	if err := store.SQL.QueryRow(`
		SELECT assignment_id, submission_id, artifact_id, current_json
		FROM workflow_events
		WHERE id = ?
	`, deleteEventID).Scan(&deletedAssignmentRef, &deletedSubmissionRef, &deletedArtifactRef, &preservedSnapshot); err != nil {
		t.Fatalf("read event after complete Task aggregate delete: %v", err)
	}
	if deletedAssignmentRef.Valid || deletedSubmissionRef.Valid || deletedArtifactRef.Valid || preservedSnapshot != `{"assignment":"preserved","submission":"preserved","artifact":"preserved"}` {
		t.Fatalf("event after Task aggregate delete = (%#v, %#v, %#v, %q)", deletedAssignmentRef, deletedSubmissionRef, deletedArtifactRef, preservedSnapshot)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestTaskArtifactsMigrationUsesInjectiveIDsAndAvoidsOccupiedLegacyEventID(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v9-id-safety.db")
	v8 := openDatabaseAtVersion(t, databasePath, 8)
	const (
		versionSevenTaskID = "018f0000-0000-7000-8000-000000000941"
		versionEightTaskID = "018f0000-0000-8000-8000-000000000941"
		legacyDerivedEvent = "018f0000-0000-4000-8000-000000000941"
	)
	for taskID, title := range map[string]string{
		versionSevenTaskID: "UUID v7 待验收任务",
		versionEightTaskID: "UUID v8 待验收任务",
	} {
		if _, err := v8.Exec(`
			INSERT INTO tasks(
				id, title, status, priority, review_policy, submitted_at,
				created_at, updated_at
			) VALUES (
				?, ?, 'waiting_review', 'P2', 'manual',
				'2026-08-27T10:00:00Z', '2026-08-27T09:00:00Z', '2026-08-27T10:00:00Z'
			)
		`, taskID, title); err != nil {
			t.Fatalf("insert UUID-version migration task %s: %v", taskID, err)
		}
	}
	if _, err := v8.Exec(`
		INSERT INTO workflow_events(id, aggregate_type, aggregate_id, action, current_json)
		VALUES (?, 'task', ?, 'preexisting_legacy_derived_id', '{"source":"fixture"}')
	`, legacyDerivedEvent, versionSevenTaskID); err != nil {
		t.Fatalf("occupy legacy derived workflow event ID: %v", err)
	}
	if err := v8.Close(); err != nil {
		t.Fatalf("close v8 UUID-version fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v8 database with UUID-version siblings and occupied event ID: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 56 {
		t.Fatalf("SchemaVersion = %d, want 56", store.SchemaVersion)
	}
	for _, taskID := range []string{versionSevenTaskID, versionEightTaskID} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_submissions WHERE task_id = ?", taskID); got != 1 {
			t.Fatalf("task %s inferred submission count = %d, want 1", taskID, got)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'migration_submission_backfill'", taskID); got != 1 {
			t.Fatalf("task %s inferred event count = %d, want 1", taskID, got)
		}
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(DISTINCT id) FROM task_submissions WHERE task_id IN (?, ?)", versionSevenTaskID, versionEightTaskID); got != 2 {
		t.Fatalf("distinct inferred submission IDs = %d, want 2", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(DISTINCT id) FROM workflow_events WHERE action = 'migration_submission_backfill' AND aggregate_id IN (?, ?)", versionSevenTaskID, versionEightTaskID); got != 2 {
		t.Fatalf("distinct inferred event IDs = %d, want 2", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM workflow_events WHERE id = ? AND action = 'preexisting_legacy_derived_id'", legacyDerivedEvent); got != 1 {
		t.Fatalf("preoccupied legacy-derived event count = %d, want 1", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM workflow_events WHERE id = ? AND action = 'migration_submission_backfill'", legacyDerivedEvent); got != 0 {
		t.Fatalf("migration reused occupied legacy-derived event ID %s", legacyDerivedEvent)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestTaskArtifactsMigrationFailureRollsBackAndRestoresForeignKeys(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v9-rollback.db")
	v8 := openDatabaseAtVersion(t, databasePath, 8)
	defer v8.Close()
	const taskID = "018f0000-0000-7000-8000-000000000951"
	if _, err := v8.Exec(`
		INSERT INTO tasks(id, title, status, priority, review_policy, created_at, updated_at)
		VALUES (?, '迁移回滚任务', 'todo', 'P2', 'none', '2026-08-27T09:00:00Z', '2026-08-27T10:00:00Z')
	`, taskID); err != nil {
		t.Fatalf("insert v8 rollback task: %v", err)
	}

	v9 := loadTaskArtifactsMigration(t)
	brokenV9 := v9
	brokenV9.version = 999
	brokenV9.name = "999_broken_task_submissions_artifacts.sql"
	brokenV9.sql += "\nINSERT INTO forced_v9_rollback_failure(id) VALUES ('fail');\n"

	ctx := context.Background()
	conn, err := v8.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve v9 rollback connection: %v", err)
	}
	defer conn.Close()
	if err := applyMigration(ctx, conn, brokenV9); err == nil {
		t.Fatal("artificially broken v9 migration succeeded")
	} else if !strings.Contains(err.Error(), "forced_v9_rollback_failure") {
		t.Fatalf("broken v9 error = %v, want forced rollback failure", err)
	}

	var foreignKeys int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read restored foreign_keys after v9 rollback: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys after failed v9 migration = %d, want 1", foreignKeys)
	}
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 999":                            0,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'task_submissions'": 0,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'task_artifacts'":   0,
		"SELECT COUNT(*) FROM tasks WHERE id = '" + taskID + "'":                                1,
	} {
		var got int64
		if err := conn.QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatalf("read v9 rollback fact with %q: %v", query, err)
		}
		if got != want {
			t.Fatalf("v9 rollback fact for %q = %d, want %d", query, got, want)
		}
	}
	for table, column := range map[string]string{
		"tasks":           "current_submission_id",
		"workflow_events": "submission_id",
	} {
		if got := readInt64FromConn(t, conn, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, column); got != 0 {
			t.Fatalf("rolled-back column %s.%s count = %d, want 0", table, column, got)
		}
	}
	if got := readInt64FromConn(t, conn, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'trg_workflow_events_immutable_update'"); got != 1 {
		t.Fatalf("v8 immutable event trigger count after rollback = %d, want 1", got)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO task_assignments(id, task_id, actor_id, role, assigned_by_actor_id, assigned_at) VALUES ('orphan', 'missing', ?, 'assignee', ?, CURRENT_TIMESTAMP)", builtinOwnerActorID, builtinOwnerActorID); err == nil {
		t.Fatal("restored connection accepted a foreign-key violation")
	}
}

func seedV8TaskArtifactsFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin v8 artifact fixture: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO projects(id, name, status, version, created_at, updated_at) VALUES (?, 'v8 产出项目', 'in_progress', 6, '2026-08-20T00:00:00Z', '2026-08-27T00:00:00Z')`,
			args:  []any{v8ArtifactProjectID},
		},
		{
			query: `
				INSERT INTO tasks(id, title, status, priority, project_id, review_policy, version, created_at, updated_at)
				VALUES (?, 'v8 产出父任务', 'in_progress', 'P2', ?, 'none', 13, '2026-08-20T01:00:00Z', '2026-08-27T01:00:00Z')
			`,
			args: []any{v8ArtifactParentTaskID, v8ArtifactProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, description, kind, status, priority, project_id, parent_task_id,
					completion_criteria, review_policy, submitted_at, due_date, planned_date,
					estimated_minutes, actual_minutes, manual_order, version, created_at, updated_at
				) VALUES (
					?, 'v8 待验收任务', '保留全部事实', 'review', 'waiting_review', 'P1', ?, ?,
					'保留完成标准', 'manual', '2026-08-27T10:00:00Z', '2026-08-30T10:00:00Z', '2026-08-28',
					90, 35, 7, 17, '2026-08-21T09:00:00Z', '2026-08-27T10:00:00Z'
				)
			`,
			args: []any{v8ArtifactPendingTaskID, v8ArtifactProjectID, v8ArtifactParentTaskID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, status, priority, review_policy, blocked_reason, blocked_at,
					blocked_from_status, submitted_at, version, created_at, updated_at
				) VALUES (
					?, 'v8 验收中阻塞任务', 'blocked', 'P2', 'manual', '等待补充材料',
					'2026-08-27T10:30:00Z', 'waiting_review', '2026-08-27T10:00:00Z',
					8, '2026-08-22T09:00:00Z', '2026-08-27T10:30:00Z'
				)
			`,
			args: []any{v8ArtifactBlockedTaskID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, status, priority, review_policy, submitted_at, reviewed_at,
					completed_at, version, created_at, updated_at
				) VALUES (
					?, 'v8 已验收任务', 'done', 'P2', 'manual', '2026-08-26T10:00:00Z',
					'2026-08-26T11:00:00Z', '2026-08-26T11:00:00Z', 6,
					'2026-08-24T09:00:00Z', '2026-08-26T11:00:00Z'
				)
			`,
			args: []any{v8ArtifactAcceptedTaskID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, status, priority, review_policy, submitted_at, reviewed_at,
					version, created_at, updated_at
				) VALUES (
					?, 'v8 已返工任务', 'in_progress', 'P2', 'manual', '2026-08-26T12:00:00Z',
					'2026-08-26T13:00:00Z', 9, '2026-08-24T09:00:00Z', '2026-08-26T13:00:00Z'
				)
			`,
			args: []any{v8ArtifactChangesTaskID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, status, priority, review_policy, submitted_at,
					version, created_at, updated_at
				) VALUES (
					?, 'v8 已取消提交任务', 'cancelled', 'P3', 'manual', '2026-08-26T14:00:00Z',
					4, '2026-08-24T09:00:00Z', '2026-08-26T15:00:00Z'
				)
			`,
			args: []any{v8ArtifactWithdrawnTaskID},
		},
		{
			query: `
				INSERT INTO tasks(id, title, status, priority, review_policy, version, created_at, updated_at)
				VALUES (?, 'v8 未提交人工任务', 'todo', 'P2', 'manual', 3, '2026-08-24T09:00:00Z', '2026-08-26T15:00:00Z')
			`,
			args: []any{v8ArtifactNoSubmissionTaskID},
		},
		{
			query: `INSERT INTO tags(id, name, color, version, created_at) VALUES (?, 'v8产出标签', '#5E6AD2', 3, '2026-08-21T00:00:00Z')`,
			args:  []any{v8ArtifactTagID},
		},
		{
			query: `INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)`,
			args:  []any{v8ArtifactPendingTaskID, v8ArtifactTagID},
		},
		{
			query: `
				INSERT INTO focus_sessions(id, task_id, started_at, ended_at, duration_minutes, completed, created_at)
				VALUES (?, ?, '2026-08-25T10:00:00Z', '2026-08-25T10:35:00Z', 35, 1, '2026-08-25T10:00:00Z')
			`,
			args: []any{v8ArtifactFocusID, v8ArtifactPendingTaskID},
		},
		{
			query: `
				INSERT INTO task_assignments(
					id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, reason
				) VALUES (?, ?, ?, 'assignee', ?, '2026-08-21T09:00:00Z', 'v8 active responsibility')
			`,
			args: []any{v8ArtifactAssignmentID, v8ArtifactPendingTaskID, builtinOwnerActorID, builtinOwnerActorID},
		},
		{
			query: `
				INSERT INTO workflow_events(
					id, aggregate_type, aggregate_id, action, actor_id, assignment_id,
					request_id, current_json, command_seq, created_at
				) VALUES (
					?, 'task', ?, 'assignment_created', ?, ?, 'v8-artifact-request',
					'{"source":"v8_fixture"}', 1, '2026-08-21T09:00:00Z'
				)
			`,
			args: []any{v8ArtifactExistingEventID, v8ArtifactPendingTaskID, builtinOwnerActorID, v8ArtifactAssignmentID},
		},
		{
			query: `
				INSERT INTO idempotency_keys(
					key, endpoint, resource_id, request_hash, response_body, response_status, created_at
				) VALUES (?, 'POST /api/v1/tasks', ?, 'v8-request-hash',
				          '{"status":"waiting_review","version":17}', 201, '2026-08-21T09:00:00Z')
			`,
			args: []any{v8ArtifactIdempotencyKey, v8ArtifactPendingTaskID},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed v8 artifact fixture: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v8 artifact fixture: %v", err)
	}
}

func insertSubmission(t *testing.T, db *sql.DB, submissionID, taskID string, sequence int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO task_submissions(
			id, task_id, sequence, status, summary, submitted_by_actor_id, submitted_at
		) VALUES (?, ?, ?, 'pending_review', '', ?, '2026-08-27T11:00:00Z')
	`, submissionID, taskID, sequence, builtinOwnerActorID); err != nil {
		t.Fatalf("insert submission %s: %v", submissionID, err)
	}
}

func loadTaskArtifactsMigration(t *testing.T) migration {
	t.Helper()
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for _, item := range items {
		if item.version == 9 {
			if !item.foreignKeysOff {
				t.Fatal("schema v9 migration marker was not detected")
			}
			return item
		}
	}
	t.Fatal("schema v9 migration not found")
	return migration{}
}

func readInt64FromConn(t *testing.T, conn *sql.Conn, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&value); err != nil {
		t.Fatalf("read integer with %q: %v", query, err)
	}
	return value
}
