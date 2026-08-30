package database

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	builtinOwnerActorID  = "00000000-0000-5000-8000-000000000001"
	builtinSystemActorID = "00000000-0000-5000-8000-000000000002"
	v6ActorProjectID     = "018f0000-0000-7000-8000-000000000701"
	v6OpenTaskID         = "018f0000-0000-7000-8000-000000000702"
	v6DoneTaskID         = "018f0000-0000-7000-8000-000000000703"
	v6DoneFallbackTaskID = "018f0000-0000-7000-8000-000000000704"
	v6ActorTagID         = "018f0000-0000-7000-8000-000000000705"
)

func TestActorMigrationUpgradesRealV6DatabaseAndBackfillsHistory(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v6-to-v7.db")
	v6 := openDatabaseAtVersion(t, databasePath, 6)
	seedV6ActorFixture(t, v6)
	if err := v6.Close(); err != nil {
		t.Fatalf("close v6 database: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v6 database with Open(): %v", err)
	}
	if store.SchemaVersion != 44 {
		_ = store.Close()
		t.Fatalf("SchemaVersion = %d, want 44", store.SchemaVersion)
	}

	for taskID, wantVersion := range map[string]int64{
		v6OpenTaskID:         4,
		v6DoneTaskID:         2,
		v6DoneFallbackTaskID: 3,
	} {
		if got := readInt64(t, store.SQL, "SELECT version FROM tasks WHERE id = ?", taskID); got != wantVersion {
			_ = store.Close()
			t.Fatalf("task %s version after actor migration = %d, want %d", taskID, got, wantVersion)
		}
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_tags WHERE task_id = ? AND tag_id = ?", v6OpenTaskID, v6ActorTagID); got != 1 {
		_ = store.Close()
		t.Fatalf("preserved task tag count = %d, want 1", got)
	}

	assertBuiltinActor(t, store.SQL, builtinOwnerActorID, "owner", "我")
	assertBuiltinActor(t, store.SQL, builtinSystemActorID, "system", "系统")
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM actors WHERE type = 'owner'"); got != 1 {
		_ = store.Close()
		t.Fatalf("owner count = %d, want 1", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM actors"); got != 2 {
		_ = store.Close()
		t.Fatalf("builtin actor count = %d, want 2", got)
	}

	assertBackfilledAssignment(t, store.SQL, v6OpenTaskID, "2026-08-20T08:00:00Z", nil)
	doneAt := "2026-08-23T11:30:00Z"
	assertBackfilledAssignment(t, store.SQL, v6DoneTaskID, "2026-08-21T09:00:00Z", &doneAt)
	fallbackAt := "2026-08-24T12:00:00Z"
	assertBackfilledAssignment(t, store.SQL, v6DoneFallbackTaskID, "2026-08-22T10:00:00Z", &fallbackAt)

	for _, taskID := range []string{v6OpenTaskID, v6DoneTaskID, v6DoneFallbackTaskID} {
		assignmentID := stableMigrationUUID(taskID, "5")
		eventID := stableMigrationUUID(taskID, "6")
		if _, err := uuid.Parse(assignmentID); err != nil {
			_ = store.Close()
			t.Fatalf("assignment backfill id %q is not a UUID: %v", assignmentID, err)
		}
		if _, err := uuid.Parse(eventID); err != nil {
			_ = store.Close()
			t.Fatalf("event backfill id %q is not a UUID: %v", eventID, err)
		}

		var event struct {
			AggregateType string
			AggregateID   string
			Action        string
			ActorID       sql.NullString
			AssignmentID  sql.NullString
			CurrentJSON   sql.NullString
		}
		if err := store.SQL.QueryRow(`
			SELECT aggregate_type, aggregate_id, action, actor_id, assignment_id, current_json
			FROM workflow_events
			WHERE id = ?
		`, eventID).Scan(
			&event.AggregateType,
			&event.AggregateID,
			&event.Action,
			&event.ActorID,
			&event.AssignmentID,
			&event.CurrentJSON,
		); err != nil {
			_ = store.Close()
			t.Fatalf("read migration event for task %s: %v", taskID, err)
		}
		if event.AggregateType != "task" || event.AggregateID != taskID || event.Action != "migration_assignment_backfill" {
			_ = store.Close()
			t.Fatalf("migration event identity = %#v", event)
		}
		if !event.ActorID.Valid || event.ActorID.String != builtinOwnerActorID || !event.AssignmentID.Valid || event.AssignmentID.String != assignmentID {
			_ = store.Close()
			t.Fatalf("migration event references = %#v", event)
		}
		var payload map[string]any
		if !event.CurrentJSON.Valid || json.Unmarshal([]byte(event.CurrentJSON.String), &payload) != nil {
			_ = store.Close()
			t.Fatalf("migration event current_json = %#v, want valid JSON", event.CurrentJSON)
		}
		if payload["source"] != "schema_v7_migration" || payload["inferred"] != true || payload["role"] != "assignee" {
			_ = store.Close()
			t.Fatalf("migration event payload = %#v", payload)
		}
	}

	assertForeignKey(t, store.SQL, "task_assignments", "task_id", "tasks", "CASCADE")
	assertForeignKey(t, store.SQL, "task_assignments", "actor_id", "actors", "RESTRICT")
	assertForeignKey(t, store.SQL, "task_assignments", "assigned_by_actor_id", "actors", "RESTRICT")
	assertForeignKey(t, store.SQL, "workflow_events", "actor_id", "actors", "RESTRICT")
	assertForeignKey(t, store.SQL, "workflow_events", "assignment_id", "task_assignments", "SET NULL")
	assertNoForeignKeyViolations(t, store.SQL)

	var ownerCreatedAt string
	if err := store.SQL.QueryRow("SELECT created_at FROM actors WHERE id = ?", builtinOwnerActorID).Scan(&ownerCreatedAt); err != nil {
		_ = store.Close()
		t.Fatalf("read owner created_at: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM schema_migrations WHERE version = 7"); err != nil {
		_ = store.Close()
		t.Fatalf("rewind actor migration record: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close first upgraded store: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("reapply actor migration: %v", err)
	}
	defer store.Close()
	for table, want := range map[string]int64{
		"actors":           2,
		"task_assignments": 3,
		"workflow_events":  3,
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM "+table); got != want {
			t.Fatalf("%s count after migration replay = %d, want %d", table, got, want)
		}
	}
	var replayedOwnerCreatedAt string
	if err := store.SQL.QueryRow("SELECT created_at FROM actors WHERE id = ?", builtinOwnerActorID).Scan(&replayedOwnerCreatedAt); err != nil {
		t.Fatalf("read replayed owner created_at: %v", err)
	}
	if replayedOwnerCreatedAt != ownerCreatedAt {
		t.Fatalf("owner created_at changed on migration replay: got %q want %q", replayedOwnerCreatedAt, ownerCreatedAt)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestActorAndAssignmentConstraintsProtectResponsibilityHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "actor-constraints.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	const (
		taskID             = "018f0000-0000-7000-8000-000000000711"
		personID           = "018f0000-0000-7000-8000-000000000712"
		personAssignmentID = "018f0000-0000-7000-8000-000000000713"
		ownerEndedID       = "018f0000-0000-7000-8000-000000000714"
		ownerReviewerID    = "018f0000-0000-7000-8000-000000000715"
	)

	if _, err := store.SQL.Exec(`
		INSERT INTO tasks(id, title, status, priority, created_at, updated_at)
		VALUES (?, 'Actor 约束任务', 'todo', 'P2', '2026-08-20T08:00:00Z', '2026-08-20T08:00:00Z')
	`, taskID); err != nil {
		t.Fatalf("insert constraint task: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO actors(id, type, display_name, status, is_builtin, notes, metadata_json, version)
		VALUES (?, 'person', '线下协作者', 'active', 0, '仅本机记录', '{"source":"manual"}', 1)
	`, personID); err != nil {
		t.Fatalf("insert person actor: %v", err)
	}

	if _, err := store.SQL.Exec(`
		UPDATE actors
		SET display_name = '应用所有者', version = version + 1, updated_at = '2026-08-25T00:00:00Z'
		WHERE id = ?
	`, builtinOwnerActorID); err != nil {
		t.Fatalf("update permitted owner display name: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "insert second owner", "UNIQUE constraint failed", `
		INSERT INTO actors(id, type, display_name, status, is_builtin)
		VALUES ('018f0000-0000-7000-8000-000000000716', 'owner', '第二个 owner', 'active', 1)
	`)
	expectSQLErrorContains(t, store.SQL, "delete builtin owner", "BUILTIN_ACTOR_DELETE_FORBIDDEN", `
		DELETE FROM actors WHERE id = ?
	`, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "delete builtin system", "BUILTIN_ACTOR_DELETE_FORBIDDEN", `
		DELETE FROM actors WHERE id = ?
	`, builtinSystemActorID)
	expectSQLErrorContains(t, store.SQL, "change builtin owner status", "BUILTIN_ACTOR_IDENTITY_IMMUTABLE", `
		UPDATE actors SET status = 'inactive' WHERE id = ?
	`, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "change owner notes", "OWNER_ACTOR_ONLY_DISPLAY_NAME_EDITABLE", `
		UPDATE actors SET notes = 'not allowed' WHERE id = ?
	`, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "change system display name", "SYSTEM_ACTOR_IMMUTABLE", `
		UPDATE actors SET display_name = 'not allowed' WHERE id = ?
	`, builtinSystemActorID)
	expectSQLErrorContains(t, store.SQL, "invalid actor metadata", "CHECK constraint failed", `
		INSERT INTO actors(id, type, display_name, status, is_builtin, metadata_json)
		VALUES ('018f0000-0000-7000-8000-000000000717', 'person', '错误元数据', 'active', 0, '[]')
	`)
	expectSQLErrorContains(t, store.SQL, "non-builtin system", "CHECK constraint failed", `
		INSERT INTO actors(id, type, display_name, status, is_builtin)
		VALUES ('018f0000-0000-7000-8000-000000000718', 'system', '伪系统', 'active', 0)
	`)

	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, reason
		) VALUES (?, ?, ?, 'assignee', ?, '2026-08-20T08:00:00Z', '首次人工分派')
	`, personAssignmentID, taskID, personID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert active person assignment: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "duplicate active assignee", "UNIQUE constraint failed", `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES ('018f0000-0000-7000-8000-000000000719', ?, ?, 'assignee', ?, '2026-08-20T09:00:00Z')
	`, taskID, builtinOwnerActorID, builtinOwnerActorID)
	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, unassigned_at, reason
		) VALUES (?, ?, ?, 'assignee', ?, '2026-08-19T08:00:00Z', '2026-08-20T07:59:00Z', '历史已结束')
	`, ownerEndedID, taskID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert ended assignment beside active assignment: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "person reviewer", "ASSIGNMENT_REVIEWER_MUST_BE_OWNER", `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES ('018f0000-0000-7000-8000-000000000720', ?, ?, 'reviewer', ?, '2026-08-20T09:00:00Z')
	`, taskID, personID, builtinOwnerActorID)
	if _, err := store.SQL.Exec(`
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES (?, ?, ?, 'reviewer', ?, '2026-08-20T09:00:00Z')
	`, ownerReviewerID, taskID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert owner reviewer: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "deactivate actor with active assignment", "ACTOR_HAS_ACTIVE_ASSIGNMENTS", `
		UPDATE actors SET status = 'inactive', version = version + 1 WHERE id = ?
	`, personID)
	expectSQLErrorContains(t, store.SQL, "delete referenced person", "FOREIGN KEY constraint failed", `
		DELETE FROM actors WHERE id = ?
	`, personID)

	if _, err := store.SQL.Exec(`
		UPDATE task_assignments
		SET unassigned_at = '2026-08-25T10:00:00Z', reason = '人工结束'
		WHERE id = ?
	`, personAssignmentID); err != nil {
		t.Fatalf("end active assignment: %v", err)
	}
	if _, err := store.SQL.Exec(`
		UPDATE actors
		SET status = 'inactive', version = version + 1, updated_at = '2026-08-25T10:00:00Z'
		WHERE id = ?
	`, personID); err != nil {
		t.Fatalf("deactivate person after ending assignments: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "reopen ended assignment", "ASSIGNMENT_HISTORY_IMMUTABLE", `
		UPDATE task_assignments SET unassigned_at = NULL WHERE id = ?
	`, ownerEndedID)
	expectSQLErrorContains(t, store.SQL, "assign inactive person", "ASSIGNMENT_ACTOR_NOT_ACTIVE", `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, unassigned_at
		) VALUES (
			'018f0000-0000-7000-8000-000000000721', ?, ?, 'assignee', ?,
			'2026-08-26T08:00:00Z', '2026-08-26T09:00:00Z'
		)
	`, taskID, personID, builtinOwnerActorID)

	if _, err := store.SQL.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id, assignment_id, current_json
		) VALUES (
			'018f0000-0000-7000-8000-000000000722', 'task', ?, 'assignment_ended', ?, ?,
			'{"reason":"人工结束"}'
		)
	`, taskID, builtinOwnerActorID, personAssignmentID); err != nil {
		t.Fatalf("insert workflow event: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "invalid workflow JSON", "CHECK constraint failed", `
		INSERT INTO workflow_events(id, aggregate_type, aggregate_id, action, current_json)
		VALUES (
			'018f0000-0000-7000-8000-000000000723', 'task', ?, 'invalid_json', '{'
		)
	`, taskID)
	expectSQLErrorContains(t, store.SQL, "missing assignment actor", "ASSIGNMENT_ACTOR_NOT_ACTIVE", `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at
		) VALUES (
			'018f0000-0000-7000-8000-000000000724', ?,
			'018f0000-0000-7000-8000-999999999999', 'assignee', ?, '2026-08-27T08:00:00Z'
		)
	`, taskID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "missing assignment task", "FOREIGN KEY constraint failed", `
		INSERT INTO task_assignments(
			id, task_id, actor_id, role, assigned_by_actor_id, assigned_at, unassigned_at
		) VALUES (
			'018f0000-0000-7000-8000-000000000725',
			'018f0000-0000-7000-8000-999999999998', ?, 'assignee', ?,
			'2026-08-27T08:00:00Z', '2026-08-27T09:00:00Z'
		)
	`, builtinOwnerActorID, builtinOwnerActorID)

	assertNoForeignKeyViolations(t, store.SQL)
}

func seedV6ActorFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin v6 actor fixture: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `
				INSERT INTO projects(id, name, status, version, created_at, updated_at)
				VALUES (?, 'Actor 迁移项目', 'in_progress', 6, '2026-08-19T00:00:00Z', '2026-08-24T12:00:00Z')
			`,
			args: []any{v6ActorProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, description, kind, status, priority, project_id,
					completion_criteria, version, created_at, updated_at
				) VALUES (
					?, '未完成历史任务', '保留正文', 'work', 'in_progress', 'P1', ?,
					'交付可验证结果', 4, '2026-08-20T08:00:00Z', '2026-08-24T10:00:00Z'
				)
			`,
			args: []any{v6OpenTaskID, v6ActorProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, kind, status, priority, project_id, version,
					created_at, updated_at, completed_at
				) VALUES (
					?, '有完成时间历史任务', 'review', 'done', 'P2', ?, 2,
					'2026-08-21T09:00:00Z', '2026-08-23T11:30:00Z', '2026-08-23T11:30:00Z'
				)
			`,
			args: []any{v6DoneTaskID, v6ActorProjectID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, kind, status, priority, project_id, version,
					created_at, updated_at, completed_at
				) VALUES (
					?, '缺少完成时间历史任务', 'followup', 'done', 'P3', ?, 3,
					'2026-08-22T10:00:00Z', '2026-08-24T12:00:00Z', NULL
				)
			`,
			args: []any{v6DoneFallbackTaskID, v6ActorProjectID},
		},
		{
			query: `
				INSERT INTO tags(id, name, color, version, created_at)
				VALUES (?, 'Actor迁移标签', '#5E6AD2', 3, '2026-08-20T00:00:00Z')
			`,
			args: []any{v6ActorTagID},
		},
		{
			query: `INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)`,
			args:  []any{v6OpenTaskID, v6ActorTagID},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed v6 actor fixture: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v6 actor fixture: %v", err)
	}
}

func assertBuiltinActor(t *testing.T, db *sql.DB, actorID, wantType, wantName string) {
	t.Helper()
	var actor struct {
		Type         string
		DisplayName  string
		Status       string
		IsBuiltin    int64
		Notes        string
		MetadataJSON string
		Version      int64
	}
	if err := db.QueryRow(`
		SELECT type, display_name, status, is_builtin, notes, metadata_json, version
		FROM actors
		WHERE id = ?
	`, actorID).Scan(
		&actor.Type,
		&actor.DisplayName,
		&actor.Status,
		&actor.IsBuiltin,
		&actor.Notes,
		&actor.MetadataJSON,
		&actor.Version,
	); err != nil {
		t.Fatalf("read builtin actor %s: %v", actorID, err)
	}
	if actor.Type != wantType || actor.DisplayName != wantName || actor.Status != "active" || actor.IsBuiltin != 1 || actor.Notes != "" || actor.MetadataJSON != "{}" || actor.Version != 1 {
		t.Fatalf("builtin actor %s = %#v", actorID, actor)
	}
}

func assertBackfilledAssignment(t *testing.T, db *sql.DB, taskID, assignedAt string, wantUnassignedAt *string) {
	t.Helper()
	assignmentID := stableMigrationUUID(taskID, "5")
	var assignment struct {
		TaskID            string
		ActorID           string
		Role              string
		AssignedByActorID string
		AssignedAt        string
		UnassignedAt      sql.NullString
		Reason            string
	}
	if err := db.QueryRow(`
		SELECT task_id, actor_id, role, assigned_by_actor_id, assigned_at, unassigned_at, reason
		FROM task_assignments
		WHERE id = ?
	`, assignmentID).Scan(
		&assignment.TaskID,
		&assignment.ActorID,
		&assignment.Role,
		&assignment.AssignedByActorID,
		&assignment.AssignedAt,
		&assignment.UnassignedAt,
		&assignment.Reason,
	); err != nil {
		t.Fatalf("read assignment for task %s: %v", taskID, err)
	}
	if assignment.TaskID != taskID || assignment.ActorID != builtinOwnerActorID || assignment.Role != "assignee" || assignment.AssignedByActorID != builtinOwnerActorID || assignment.AssignedAt != assignedAt || assignment.Reason != "schema_v7_migration_inferred_owner" {
		t.Fatalf("backfilled assignment for task %s = %#v", taskID, assignment)
	}
	if wantUnassignedAt == nil {
		if assignment.UnassignedAt.Valid {
			t.Fatalf("task %s unassigned_at = %q, want NULL", taskID, assignment.UnassignedAt.String)
		}
		return
	}
	if !assignment.UnassignedAt.Valid || assignment.UnassignedAt.String != *wantUnassignedAt {
		t.Fatalf("task %s unassigned_at = %#v, want %q", taskID, assignment.UnassignedAt, *wantUnassignedAt)
	}
}

func stableMigrationUUID(sourceID, versionNibble string) string {
	if len(sourceID) != 36 {
		return ""
	}
	return sourceID[:14] + versionNibble + sourceID[15:]
}

func expectSQLErrorContains(t *testing.T, db *sql.DB, name, want string, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err == nil {
		t.Fatalf("%s succeeded, want error containing %q", name, want)
	} else if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want substring %q", name, err, want)
	}
}
