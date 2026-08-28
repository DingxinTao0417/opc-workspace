package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const (
	v7WorkflowProjectID      = "018f0000-0000-7000-8000-000000000801"
	v7WorkflowParentTaskID   = "018f0000-0000-7000-8000-000000000802"
	v7WorkflowOpenTaskID     = "018f0000-0000-7000-8000-000000000803"
	v7WorkflowDoneTaskID     = "018f0000-0000-7000-8000-000000000804"
	v7WorkflowTagID          = "018f0000-0000-7000-8000-000000000805"
	v7WorkflowFocusID        = "018f0000-0000-7000-8000-000000000806"
	v7WorkflowAssignmentID   = "018f0000-0000-7000-8000-000000000807"
	v7WorkflowDoneAssignID   = "018f0000-0000-7000-8000-000000000808"
	v7WorkflowEventID        = "018f0000-0000-7000-8000-000000000809"
	v7WorkflowIdempotencyKey = "v7-workflow-snapshot"
)

func TestTaskWorkflowMigrationUpgradesRealV7DatabaseWithoutLosingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v7-to-v8.db")
	v7 := openDatabaseAtVersion(t, databasePath, 7)
	seedV7TaskWorkflowFixture(t, v7)

	wantProjectVersion := readInt64(t, v7, "SELECT version FROM projects WHERE id = ?", v7WorkflowProjectID)
	wantParentVersion := readInt64(t, v7, "SELECT version FROM tasks WHERE id = ?", v7WorkflowParentTaskID)
	if err := v7.Close(); err != nil {
		t.Fatalf("close v7 workflow fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v7 workflow database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 10 {
		t.Fatalf("SchemaVersion = %d, want 10", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v7WorkflowProjectID); got != wantProjectVersion {
		t.Fatalf("project version changed during task rebuild: got %d want %d", got, wantProjectVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM tasks WHERE id = ?", v7WorkflowParentTaskID); got != wantParentVersion {
		t.Fatalf("parent task version changed during task rebuild: got %d want %d", got, wantParentVersion)
	}

	var openTask struct {
		Title              string
		Description        string
		Kind               string
		Status             string
		ReviewPolicy       string
		Priority           string
		ProjectID          sql.NullString
		ParentTaskID       sql.NullString
		CompletionCriteria string
		DueDate            sql.NullString
		PlannedDate        sql.NullString
		EstimatedMinutes   sql.NullInt64
		ActualMinutes      int64
		ManualOrder        sql.NullInt64
		Version            int64
		BlockedReason      sql.NullString
		BlockedAt          sql.NullString
		BlockedFromStatus  sql.NullString
		SubmittedAt        sql.NullString
		ReviewedAt         sql.NullString
	}
	if err := store.SQL.QueryRow(`
		SELECT title, description, kind, status, review_policy, priority,
		       project_id, parent_task_id, completion_criteria, due_date, planned_date,
		       estimated_minutes, actual_minutes, manual_order, version,
		       blocked_reason, blocked_at, blocked_from_status, submitted_at, reviewed_at
		FROM tasks
		WHERE id = ?
	`, v7WorkflowOpenTaskID).Scan(
		&openTask.Title,
		&openTask.Description,
		&openTask.Kind,
		&openTask.Status,
		&openTask.ReviewPolicy,
		&openTask.Priority,
		&openTask.ProjectID,
		&openTask.ParentTaskID,
		&openTask.CompletionCriteria,
		&openTask.DueDate,
		&openTask.PlannedDate,
		&openTask.EstimatedMinutes,
		&openTask.ActualMinutes,
		&openTask.ManualOrder,
		&openTask.Version,
		&openTask.BlockedReason,
		&openTask.BlockedAt,
		&openTask.BlockedFromStatus,
		&openTask.SubmittedAt,
		&openTask.ReviewedAt,
	); err != nil {
		t.Fatalf("read upgraded open task: %v", err)
	}
	if openTask.Title != "v7 开放任务" || openTask.Description != "所有字段必须原样保留" || openTask.Kind != "review" || openTask.Status != "in_progress" || openTask.ReviewPolicy != "none" || openTask.Priority != "P1" {
		t.Fatalf("upgraded open task identity changed: %#v", openTask)
	}
	if !openTask.ProjectID.Valid || openTask.ProjectID.String != v7WorkflowProjectID || !openTask.ParentTaskID.Valid || openTask.ParentTaskID.String != v7WorkflowParentTaskID {
		t.Fatalf("upgraded open task relationships changed: %#v", openTask)
	}
	if openTask.CompletionCriteria != "保留验收条件" || !openTask.DueDate.Valid || openTask.DueDate.String != "2026-08-30T12:00:00Z" || !openTask.PlannedDate.Valid || openTask.PlannedDate.String != "2026-08-29" {
		t.Fatalf("upgraded open task schedule changed: %#v", openTask)
	}
	if !openTask.EstimatedMinutes.Valid || openTask.EstimatedMinutes.Int64 != 75 || openTask.ActualMinutes != 25 || !openTask.ManualOrder.Valid || openTask.ManualOrder.Int64 != 3 || openTask.Version != 11 {
		t.Fatalf("upgraded open task planning/version changed: %#v", openTask)
	}
	if openTask.BlockedReason.Valid || openTask.BlockedAt.Valid || openTask.BlockedFromStatus.Valid || openTask.SubmittedAt.Valid || openTask.ReviewedAt.Valid {
		t.Fatalf("new workflow fields should be NULL for v7 task: %#v", openTask)
	}

	var doneCompletedAt, doneUpdatedAt string
	var doneVersion int64
	if err := store.SQL.QueryRow(`
		SELECT completed_at, updated_at, version
		FROM tasks
		WHERE id = ?
	`, v7WorkflowDoneTaskID).Scan(&doneCompletedAt, &doneUpdatedAt, &doneVersion); err != nil {
		t.Fatalf("read upgraded done task: %v", err)
	}
	if doneCompletedAt != doneUpdatedAt || doneCompletedAt != "2026-08-26T14:30:00Z" || doneVersion != 7 {
		t.Fatalf("done task fallback/version = (%q, %q, %d)", doneCompletedAt, doneUpdatedAt, doneVersion)
	}

	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM task_tags WHERE task_id = '" + v7WorkflowOpenTaskID + "' AND tag_id = '" + v7WorkflowTagID + "'":             1,
		"SELECT COUNT(*) FROM focus_sessions WHERE id = '" + v7WorkflowFocusID + "' AND task_id = '" + v7WorkflowOpenTaskID + "'":          1,
		"SELECT COUNT(*) FROM task_assignments WHERE id IN ('" + v7WorkflowAssignmentID + "', '" + v7WorkflowDoneAssignID + "')":           2,
		"SELECT COUNT(*) FROM workflow_events WHERE id = '" + v7WorkflowEventID + "' AND assignment_id = '" + v7WorkflowAssignmentID + "'": 1,
	} {
		if got := readInt64(t, store.SQL, query); got != want {
			t.Fatalf("preserved relationship count for %q = %d, want %d", query, got, want)
		}
	}
	var commandSequence sql.NullInt64
	if err := store.SQL.QueryRow("SELECT command_seq FROM workflow_events WHERE id = ?", v7WorkflowEventID).Scan(&commandSequence); err != nil {
		t.Fatalf("read migrated command_seq: %v", err)
	}
	if commandSequence.Valid {
		t.Fatalf("historical command_seq = %d, want NULL", commandSequence.Int64)
	}

	var requestHash, responseBody string
	var responseStatus int
	if err := store.SQL.QueryRow(`
		SELECT request_hash, response_body, response_status
		FROM idempotency_keys
		WHERE key = ? AND endpoint = 'POST /api/v1/tasks'
	`, v7WorkflowIdempotencyKey).Scan(&requestHash, &responseBody, &responseStatus); err != nil {
		t.Fatalf("read preserved v7 idempotency snapshot: %v", err)
	}
	if requestHash != "v7-request-hash" || responseBody != `{"status":"in_progress","version":11}` || responseStatus != 201 {
		t.Fatalf("v7 idempotency snapshot changed: (%q, %q, %d)", requestHash, responseBody, responseStatus)
	}

	taskIndexes := []string{
		"idx_tasks_status",
		"idx_tasks_priority",
		"idx_tasks_project_id",
		"idx_tasks_planned_date",
		"idx_tasks_due_date",
		"idx_tasks_manual_order",
		"idx_tasks_kind",
		"idx_tasks_parent_task_id",
		"idx_tasks_planned_manual_order",
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name IN ("+questionMarks(len(taskIndexes))+")", stringsToAny(taskIndexes)...); got != int64(len(taskIndexes)) {
		t.Fatalf("restored task index count = %d, want %d", got, len(taskIndexes))
	}
	taskTriggers := []string{
		"projects_version_after_task_insert",
		"projects_version_after_task_update",
		"projects_version_after_task_delete",
		"trg_tasks_parent_cycle_insert",
		"trg_tasks_parent_cycle_update",
		"trg_tasks_parent_after_insert",
		"trg_tasks_parent_after_delete",
		"trg_tasks_parent_after_status_update",
		"trg_tasks_parent_after_parent_update",
		"trg_tasks_parent_after_title_update",
		"trg_tasks_status_transition_update",
		"trg_tasks_terminal_requires_no_active_assignments",
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ("+questionMarks(len(taskTriggers))+")", stringsToAny(taskTriggers)...); got != int64(len(taskTriggers)) {
		t.Fatalf("restored/new task trigger count = %d, want %d", got, len(taskTriggers))
	}
	var timelineIndexSQL string
	if err := store.SQL.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_workflow_events_aggregate_timeline'").Scan(&timelineIndexSQL); err != nil {
		t.Fatalf("read workflow timeline index: %v", err)
	}
	if !strings.Contains(timelineIndexSQL, "command_seq") {
		t.Fatalf("workflow timeline index does not include command_seq: %s", timelineIndexSQL)
	}
	assertForeignKey(t, store.SQL, "tasks", "project_id", "projects", "SET NULL")
	assertForeignKey(t, store.SQL, "tasks", "parent_task_id", "tasks", "SET NULL")
	assertForeignKey(t, store.SQL, "task_assignments", "task_id", "tasks", "CASCADE")
	assertForeignKey(t, store.SQL, "task_tags", "task_id", "tasks", "CASCADE")
	assertForeignKey(t, store.SQL, "focus_sessions", "task_id", "tasks", "SET NULL")
	assertNoForeignKeyViolations(t, store.SQL)
	var integrity string
	if err := store.SQL.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("run integrity_check after v7 to v8 upgrade: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check after v7 to v8 upgrade = %q, want ok", integrity)
	}
}

func TestTaskWorkflowConstraintsProtectLifecycleAndEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "workflow-constraints.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	const (
		primaryTaskID      = "018f0000-0000-7000-8000-000000000821"
		manualTaskID       = "018f0000-0000-7000-8000-000000000822"
		assignedTaskID     = "018f0000-0000-7000-8000-000000000823"
		assignmentID       = "018f0000-0000-7000-8000-000000000824"
		eventTaskID        = "018f0000-0000-7000-8000-000000000825"
		eventAssignmentID  = "018f0000-0000-7000-8000-000000000826"
		eventID            = "018f0000-0000-7000-8000-000000000827"
		terminalTaskID     = "018f0000-0000-7000-8000-000000000828"
		terminalAssignID   = "018f0000-0000-7000-8000-000000000829"
		invalidSequenceID  = "018f0000-0000-7000-8000-000000000830"
		invalidTransition  = "TASK_TRANSITION_NOT_ALLOWED"
		activeAssignments  = "TASK_HAS_ACTIVE_ASSIGNMENTS"
		notAssignable      = "TASK_NOT_ASSIGNABLE"
		immutableEventText = "WORKFLOW_EVENT_IMMUTABLE"
	)

	insertWorkflowTask(t, store.SQL, primaryTaskID, "普通生命周期任务", "none")
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'in_progress', version = version + 1
		WHERE id = ?
	`, primaryTaskID); err != nil {
		t.Fatalf("start task: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "return in-progress task to todo", invalidTransition, `
		UPDATE tasks SET status = 'todo', version = version + 1 WHERE id = ?
	`, primaryTaskID)
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'blocked', blocked_reason = '等待客户确认',
		    blocked_at = '2026-08-27T12:00:00Z', blocked_from_status = 'in_progress',
		    version = version + 1
		WHERE id = ?
	`, primaryTaskID); err != nil {
		t.Fatalf("block task: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "unblock to a different source state", invalidTransition, `
		UPDATE tasks
		SET status = 'todo', blocked_reason = NULL, blocked_at = NULL, blocked_from_status = NULL,
		    version = version + 1
		WHERE id = ?
	`, primaryTaskID)
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'in_progress', blocked_reason = NULL, blocked_at = NULL, blocked_from_status = NULL,
		    version = version + 1
		WHERE id = ?
	`, primaryTaskID); err != nil {
		t.Fatalf("unblock to stored source status: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'done', completed_at = '2026-08-27T13:00:00Z', version = version + 1
		WHERE id = ?
	`, primaryTaskID); err != nil {
		t.Fatalf("complete no-review task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'todo', completed_at = NULL, version = version + 1
		WHERE id = ?
	`, primaryTaskID); err != nil {
		t.Fatalf("reopen done task: %v", err)
	}

	insertWorkflowTask(t, store.SQL, manualTaskID, "人工验收任务", "manual")
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'waiting_review', submitted_at = '2026-08-27T14:00:00Z', version = version + 1
		WHERE id = ?
	`, manualTaskID); err != nil {
		t.Fatalf("submit manual-review task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'in_progress', reviewed_at = '2026-08-27T14:30:00Z', version = version + 1
		WHERE id = ?
	`, manualTaskID); err != nil {
		t.Fatalf("request changes on manual-review task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'waiting_review', submitted_at = '2026-08-27T15:00:00Z', reviewed_at = NULL,
		    version = version + 1
		WHERE id = ?
	`, manualTaskID); err != nil {
		t.Fatalf("resubmit manual-review task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'done', completed_at = '2026-08-27T15:30:00Z',
		    reviewed_at = '2026-08-27T15:30:00Z', version = version + 1
		WHERE id = ?
	`, manualTaskID); err != nil {
		t.Fatalf("accept manual-review task: %v", err)
	}

	insertWorkflowTask(t, store.SQL, assignedTaskID, "存在活动责任人的任务", "none")
	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES (?, ?, ?, 'assignee', ?, '2026-08-27T16:00:00Z')
	`, assignmentID, assignedTaskID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert active assignment: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "cancel task with active assignment", activeAssignments, `
		UPDATE tasks SET status = 'cancelled', version = version + 1 WHERE id = ?
	`, assignedTaskID)
	if _, err := store.SQL.Exec(`
		UPDATE task_assignments
		SET unassigned_at = '2026-08-27T16:30:00Z', reason = 'cancelled by lifecycle test'
		WHERE id = ?
	`, assignmentID); err != nil {
		t.Fatalf("close assignment before cancellation: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE tasks SET status = 'cancelled', version = version + 1 WHERE id = ?
	`, assignedTaskID); err != nil {
		t.Fatalf("cancel task after closing assignment: %v", err)
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO tasks(
			id, title, status, priority, review_policy, completed_at, created_at, updated_at
		) VALUES (?, '已完成终态任务', 'done', 'P2', 'none', '2026-08-27T17:00:00Z',
		          '2026-08-27T17:00:00Z', '2026-08-27T17:00:00Z')
	`, terminalTaskID); err != nil {
		t.Fatalf("insert terminal task: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "assign terminal task", notAssignable, `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES (?, ?, ?, 'assignee', ?, '2026-08-27T17:01:00Z')
	`, terminalAssignID, terminalTaskID, builtinOwnerActorID, builtinOwnerActorID)

	insertWorkflowTask(t, store.SQL, eventTaskID, "事件不可变任务", "none")
	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at,
			unassigned_at, reason
		) VALUES (
			?, ?, ?, 'assignee', ?, '2026-08-27T18:00:00Z',
			'2026-08-27T18:30:00Z', 'historical event test'
		)
	`, eventAssignmentID, eventTaskID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert historical event assignment: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id, assignment_id,
			request_id, command_seq, current_json, created_at
		) VALUES (
			?, 'task', ?, 'assignment_ended', ?, ?, 'workflow-test-request', 1,
			'{"reason":"historical event test"}', '2026-08-27T18:30:00Z'
		)
	`, eventID, eventTaskID, builtinOwnerActorID, eventAssignmentID); err != nil {
		t.Fatalf("insert sequenced workflow event: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "zero workflow command sequence", "CHECK constraint failed", `
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, command_seq
		) VALUES (?, 'task', ?, 'invalid_sequence', 0)
	`, invalidSequenceID, eventTaskID)
	expectSQLErrorContains(t, store.SQL, "mutate workflow event", immutableEventText, `
		UPDATE workflow_events SET action = 'rewritten' WHERE id = ?
	`, eventID)
	expectSQLErrorContains(t, store.SQL, "delete workflow event", immutableEventText, `
		DELETE FROM workflow_events WHERE id = ?
	`, eventID)
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", eventTaskID); err != nil {
		t.Fatalf("delete task aggregate and null preserved event assignment reference: %v", err)
	}
	var eventAssignment sql.NullString
	var eventAction string
	var eventSequence int64
	if err := store.SQL.QueryRow(`
		SELECT assignment_id, action, command_seq
		FROM workflow_events
		WHERE id = ?
	`, eventID).Scan(&eventAssignment, &eventAction, &eventSequence); err != nil {
		t.Fatalf("read event after assignment delete: %v", err)
	}
	if eventAssignment.Valid || eventAction != "assignment_ended" || eventSequence != 1 {
		t.Fatalf("event after FK SET NULL = (%#v, %q, %d)", eventAssignment, eventAction, eventSequence)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_assignments WHERE id = ?", eventAssignmentID); got != 0 {
		t.Fatalf("assignment count after task aggregate delete = %d, want 0", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestForeignKeysOffMigrationRollsBackAndRestoresForeignKeys(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "migration-rollback.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if _, err := store.SQL.Exec(`
		CREATE TABLE fk_migration_parents(id TEXT PRIMARY KEY);
		CREATE TABLE fk_migration_children(
			id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL REFERENCES fk_migration_parents(id)
		);
		INSERT INTO fk_migration_parents(id) VALUES ('parent');
		INSERT INTO fk_migration_children(id, parent_id) VALUES ('child', 'parent');
	`); err != nil {
		t.Fatalf("seed migration rollback fixture: %v", err)
	}

	ctx := context.Background()
	conn, err := store.SQL.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve rollback test connection: %v", err)
	}
	defer conn.Close()

	brokenMigration := migration{
		version:        999,
		name:           "999_broken_fk_rebuild.sql",
		foreignKeysOff: true,
		sql: `
			CREATE TABLE fk_migration_parents_v2(id TEXT PRIMARY KEY);
			INSERT INTO fk_migration_parents_v2(id) SELECT id FROM fk_migration_parents;
			DROP TABLE fk_migration_parents;
			ALTER TABLE fk_migration_parents_v2 RENAME TO fk_migration_parents;
			DELETE FROM fk_migration_parents WHERE id = 'parent';
		`,
	}
	if err := applyMigration(ctx, conn, brokenMigration); err == nil {
		t.Fatal("broken foreign-key migration succeeded")
	} else if !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatalf("broken migration error = %v, want foreign key violation", err)
	}

	var foreignKeys int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read restored foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys after failed migration = %d, want 1", foreignKeys)
	}
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM fk_migration_parents WHERE id = 'parent'":                                     1,
		"SELECT COUNT(*) FROM fk_migration_children WHERE id = 'child' AND parent_id = 'parent'":            1,
		"SELECT COUNT(*) FROM schema_migrations WHERE version = 999 AND name = '999_broken_fk_rebuild.sql'": 0,
	} {
		var got int64
		if err := conn.QueryRowContext(ctx, query).Scan(&got); err != nil {
			t.Fatalf("read rollback fact with %q: %v", query, err)
		}
		if got != want {
			t.Fatalf("rollback fact for %q = %d, want %d", query, got, want)
		}
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO fk_migration_children(id, parent_id) VALUES ('orphan', 'missing')"); err == nil {
		t.Fatal("restored connection accepted a foreign-key violation")
	}

	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for _, item := range items {
		if item.version == 8 {
			if !item.foreignKeysOff {
				t.Fatal("schema v8 migration marker was not detected")
			}
			return
		}
	}
	t.Fatal("schema v8 migration not found")
}

func seedV7TaskWorkflowFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin v7 workflow fixture: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `
				INSERT INTO projects(id, name, status, version, created_at, updated_at)
				VALUES (?, 'v7 工作流项目', 'in_progress', 4, '2026-08-20T00:00:00Z', '2026-08-26T14:30:00Z')
			`,
			args: []any{v7WorkflowProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, kind, status, priority, project_id, completion_criteria,
					version, created_at, updated_at
				) VALUES (
					?, 'v7 父任务', 'work', 'todo', 'P2', ?, '父任务完成条件',
					5, '2026-08-20T08:00:00Z', '2026-08-25T10:00:00Z'
				)
			`,
			args: []any{v7WorkflowParentTaskID, v7WorkflowProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, description, kind, status, priority, project_id, parent_task_id,
					completion_criteria, due_date, planned_date, estimated_minutes, actual_minutes,
					manual_order, version, created_at, updated_at
				) VALUES (
					?, 'v7 开放任务', '所有字段必须原样保留', 'review', 'in_progress', 'P1', ?, ?,
					'保留验收条件', '2026-08-30T12:00:00Z', '2026-08-29', 75, 25,
					3, 11, '2026-08-21T09:00:00Z', '2026-08-26T13:00:00Z'
				)
			`,
			args: []any{v7WorkflowOpenTaskID, v7WorkflowProjectID, v7WorkflowParentTaskID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, kind, status, priority, project_id, completed_at,
					version, created_at, updated_at
				) VALUES (
					?, 'v7 缺完成时间任务', 'followup', 'done', 'P3', ?, NULL,
					7, '2026-08-22T10:00:00Z', '2026-08-26T14:30:00Z'
				)
			`,
			args: []any{v7WorkflowDoneTaskID, v7WorkflowProjectID},
		},
		{
			query: `INSERT INTO tags(id, name, color, version, created_at) VALUES (?, 'v7工作流标签', '#5E6AD2', 2, '2026-08-21T00:00:00Z')`,
			args:  []any{v7WorkflowTagID},
		},
		{
			query: `INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)`,
			args:  []any{v7WorkflowOpenTaskID, v7WorkflowTagID},
		},
		{
			query: `
				INSERT INTO focus_sessions(
					id, task_id, started_at, ended_at, duration_minutes, completed, created_at
				) VALUES (
					?, ?, '2026-08-25T10:00:00Z', '2026-08-25T10:25:00Z', 25, 1, '2026-08-25T10:00:00Z'
				)
			`,
			args: []any{v7WorkflowFocusID, v7WorkflowOpenTaskID},
		},
		{
			query: `
				INSERT INTO task_assignments(
					id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, reason
				) VALUES (?, ?, ?, 'assignee', ?, '2026-08-21T09:00:00Z', 'v7 active responsibility')
			`,
			args: []any{v7WorkflowAssignmentID, v7WorkflowOpenTaskID, builtinOwnerActorID, builtinOwnerActorID},
		},
		{
			query: `
				INSERT INTO task_assignments(
					id, task_id, actor_id, role, assigned_by_actor_id, assigned_at,
					unassigned_at, reason
				) VALUES (
					?, ?, ?, 'assignee', ?, '2026-08-22T10:00:00Z',
					'2026-08-26T14:30:00Z', 'v7 completed responsibility'
				)
			`,
			args: []any{v7WorkflowDoneAssignID, v7WorkflowDoneTaskID, builtinOwnerActorID, builtinOwnerActorID},
		},
		{
			query: `
				INSERT INTO workflow_events(
					id, aggregate_type, aggregate_id, action, actor_id, assignment_id,
					request_id, current_json, created_at
				) VALUES (
					?, 'task', ?, 'assignment_created', ?, ?, 'v7-workflow-request',
					'{"source":"v7_fixture"}', '2026-08-21T09:00:00Z'
				)
			`,
			args: []any{v7WorkflowEventID, v7WorkflowOpenTaskID, builtinOwnerActorID, v7WorkflowAssignmentID},
		},
		{
			query: `
				INSERT INTO idempotency_keys(
					key, endpoint, resource_id, request_hash, response_body, response_status, created_at
				) VALUES (?, 'POST /api/v1/tasks', ?, 'v7-request-hash',
				          '{"status":"in_progress","version":11}', 201, '2026-08-21T09:00:00Z')
			`,
			args: []any{v7WorkflowIdempotencyKey, v7WorkflowOpenTaskID},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed v7 workflow fixture: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v7 workflow fixture: %v", err)
	}
}

func insertWorkflowTask(t *testing.T, db *sql.DB, taskID, title, reviewPolicy string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO tasks(
			id, title, status, priority, review_policy, created_at, updated_at
		) VALUES (?, ?, 'todo', 'P2', ?, '2026-08-27T10:00:00Z', '2026-08-27T10:00:00Z')
	`, taskID, title, reviewPolicy); err != nil {
		t.Fatalf("insert workflow task %s: %v", title, err)
	}
}

func questionMarks(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func stringsToAny(values []string) []any {
	items := make([]any, len(values))
	for index, value := range values {
		items[index] = value
	}
	return items
}
