package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	v5ClientID        = "018f0000-0000-7000-8000-000000000401"
	v5ProjectOneID    = "018f0000-0000-7000-8000-000000000402"
	v5ProjectTwoID    = "018f0000-0000-7000-8000-000000000403"
	v5ParentTaskID    = "018f0000-0000-7000-8000-000000000404"
	v5ChildTaskID     = "018f0000-0000-7000-8000-000000000405"
	v5TagID           = "018f0000-0000-7000-8000-000000000406"
	v5FocusSessionID  = "018f0000-0000-7000-8000-000000000407"
	v5IdempotencyKey  = "v5-task-create-snapshot"
	v5IdempotencyHash = "v5-request-hash"
	v5SnapshotBody    = `{"id":"018f0000-0000-7000-8000-000000000405","title":"历史子任务","status":"in_progress"}`
)

func TestTaskFactsMigrationUpgradesRealV5DatabaseWithoutLosingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v5-to-v6.db")
	v5 := openDatabaseAtVersion(t, databasePath, 5)
	seedV5TaskFactsFixture(t, v5)

	if got := readInt64(t, v5, "SELECT version FROM projects WHERE id = ?", v5ProjectOneID); got != 3 {
		t.Fatalf("v5 project one version = %d, want 3 after two task inserts", got)
	}
	if got := readInt64(t, v5, "SELECT version FROM projects WHERE id = ?", v5ProjectTwoID); got != 1 {
		t.Fatalf("v5 project two version = %d, want 1", got)
	}
	if err := v5.Close(); err != nil {
		t.Fatalf("close v5 database: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v5 database with Open(): %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 15 {
		t.Fatalf("SchemaVersion = %d, want 15", store.SchemaVersion)
	}

	var task struct {
		Title              string
		Description        string
		Kind               string
		Status             string
		Priority           string
		ProjectID          sql.NullString
		ParentTaskID       sql.NullString
		CompletionCriteria string
		EstimatedMinutes   sql.NullInt64
		ActualMinutes      int64
		ManualOrder        sql.NullInt64
		Version            int64
	}
	if err := store.SQL.QueryRow(`
		SELECT title, description, kind, status, priority, project_id, parent_task_id,
		       completion_criteria, estimated_minutes, actual_minutes, manual_order, version
		FROM tasks
		WHERE id = ?
	`, v5ChildTaskID).Scan(
		&task.Title,
		&task.Description,
		&task.Kind,
		&task.Status,
		&task.Priority,
		&task.ProjectID,
		&task.ParentTaskID,
		&task.CompletionCriteria,
		&task.EstimatedMinutes,
		&task.ActualMinutes,
		&task.ManualOrder,
		&task.Version,
	); err != nil {
		t.Fatalf("read upgraded historical task: %v", err)
	}
	if task.Title != "历史子任务" || task.Description != "升级时必须保留" || task.Status != "in_progress" || task.Priority != "P1" {
		t.Fatalf("historical task fields changed during upgrade: %#v", task)
	}
	if !task.ProjectID.Valid || task.ProjectID.String != v5ProjectOneID {
		t.Fatalf("historical task project_id = %#v, want %s", task.ProjectID, v5ProjectOneID)
	}
	if task.Kind != "work" || task.ParentTaskID.Valid || task.CompletionCriteria != "" || task.Version != 1 {
		t.Fatalf("v6 task defaults = %#v, want work/null/empty/version 1", task)
	}
	if !task.EstimatedMinutes.Valid || task.EstimatedMinutes.Int64 != 45 || task.ActualMinutes != 15 || !task.ManualOrder.Valid || task.ManualOrder.Int64 != 2 {
		t.Fatalf("historical task planning facts changed during upgrade: %#v", task)
	}

	var tagName, tagColor string
	var tagVersion int64
	if err := store.SQL.QueryRow("SELECT name, color, version FROM tags WHERE id = ?", v5TagID).
		Scan(&tagName, &tagColor, &tagVersion); err != nil {
		t.Fatalf("read upgraded tag: %v", err)
	}
	if tagName != "历史标签" || tagColor != "#5E6AD2" || tagVersion != 1 {
		t.Fatalf("upgraded tag = (%q, %q, %d)", tagName, tagColor, tagVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_tags WHERE task_id = ? AND tag_id = ?", v5ChildTaskID, v5TagID); got != 1 {
		t.Fatalf("historical task_tags link count = %d, want 1", got)
	}

	var focusTaskID sql.NullString
	var focusStatus string
	var plannedSeconds, accumulatedSeconds, creditedMinutes, focusVersion int64
	if err := store.SQL.QueryRow(`
		SELECT task_id, status, planned_seconds, accumulated_seconds, credited_minutes, version
		FROM focus_sessions
		WHERE id = ?
	`, v5FocusSessionID).Scan(
		&focusTaskID, &focusStatus, &plannedSeconds, &accumulatedSeconds, &creditedMinutes, &focusVersion,
	); err != nil {
		t.Fatalf("read upgraded focus session: %v", err)
	}
	if !focusTaskID.Valid || focusTaskID.String != v5ChildTaskID || focusStatus != "completed" ||
		plannedSeconds != 900 || accumulatedSeconds != 900 || creditedMinutes != 15 || focusVersion != 1 {
		t.Fatalf(
			"upgraded focus session = (%#v, %q, %d, %d, %d, %d)",
			focusTaskID, focusStatus, plannedSeconds, accumulatedSeconds, creditedMinutes, focusVersion,
		)
	}
	if got := readInt64(t, store.SQL, "SELECT duration_seconds FROM focus_session_intervals WHERE session_id = ?", v5FocusSessionID); got != 900 {
		t.Fatalf("upgraded focus interval seconds = %d, want 900", got)
	}
	if got := readInt64(t, store.SQL, "SELECT exact_seconds FROM task_focus_totals WHERE task_id = ?", v5ChildTaskID); got != 900 {
		t.Fatalf("upgraded Task Focus total seconds = %d, want 900", got)
	}

	var requestHash, responseBody string
	var responseStatus int
	if err := store.SQL.QueryRow(`
		SELECT request_hash, response_body, response_status
		FROM idempotency_keys
		WHERE key = ? AND endpoint = 'POST /api/v1/tasks'
	`, v5IdempotencyKey).Scan(&requestHash, &responseBody, &responseStatus); err != nil {
		t.Fatalf("read upgraded idempotency snapshot: %v", err)
	}
	if requestHash != v5IdempotencyHash || responseBody != v5SnapshotBody || responseStatus != 201 {
		t.Fatalf("idempotency snapshot changed during upgrade: hash=%q body=%q status=%d", requestHash, responseBody, responseStatus)
	}

	assertForeignKey(t, store.SQL, "tasks", "project_id", "projects", "SET NULL")
	assertForeignKey(t, store.SQL, "tasks", "parent_task_id", "tasks", "SET NULL")
	assertNoForeignKeyViolations(t, store.SQL)
	if _, err := store.SQL.Exec("UPDATE tasks SET project_id = ? WHERE id = ?", "018f0000-0000-7000-8000-999999999999", v5ChildTaskID); err == nil {
		t.Fatal("upgraded tasks.project_id accepted a missing project")
	}

	t.Run("schema v5 project aggregate triggers still fire", func(t *testing.T) {
		if _, err := store.SQL.Exec(`
			UPDATE task_assignments
			SET unassigned_at = '2026-08-27T12:00:00Z', reason = 'migration constraint test'
			WHERE task_id = ? AND unassigned_at IS NULL
		`, v5ChildTaskID); err != nil {
			t.Fatalf("end upgraded task assignment: %v", err)
		}
		if _, err := store.SQL.Exec(`
			UPDATE tasks
			SET status = 'done', completed_at = '2026-08-27T12:00:00Z', version = version + 1
			WHERE id = ?
		`, v5ChildTaskID); err != nil {
			t.Fatalf("update upgraded task status: %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v5ProjectOneID); got != 4 {
			t.Fatalf("project one version after task status = %d, want 4", got)
		}

		if _, err := store.SQL.Exec(`
			UPDATE tasks
			SET actual_minutes = 30, version = version + 1
			WHERE id = ?
		`, v5ChildTaskID); err != nil {
			t.Fatalf("update upgraded task actual_minutes: %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v5ProjectOneID); got != 5 {
			t.Fatalf("project one version after task time = %d, want 5", got)
		}

		if _, err := store.SQL.Exec(`
			UPDATE tasks
			SET project_id = ?, version = version + 1
			WHERE id = ?
		`, v5ProjectTwoID, v5ChildTaskID); err != nil {
			t.Fatalf("move upgraded task between projects: %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v5ProjectOneID); got != 6 {
			t.Fatalf("old project version after task move = %d, want 6", got)
		}
		if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v5ProjectTwoID); got != 2 {
			t.Fatalf("new project version after task move = %d, want 2", got)
		}

		if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", v5ChildTaskID); err != nil {
			t.Fatalf("delete upgraded task: %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", v5ProjectTwoID); got != 3 {
			t.Fatalf("project two version after task delete = %d, want 3", got)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_tags WHERE task_id = ?", v5ChildTaskID); got != 0 {
			t.Fatalf("task_tags rows after task delete = %d, want 0", got)
		}
		if err := store.SQL.QueryRow("SELECT task_id FROM focus_sessions WHERE id = ?", v5FocusSessionID).Scan(&focusTaskID); err != nil {
			t.Fatalf("read focus session after task delete: %v", err)
		}
		if focusTaskID.Valid {
			t.Fatalf("focus task_id after task delete = %#v, want NULL", focusTaskID)
		}
		assertNoForeignKeyViolations(t, store.SQL)
	})
}

func TestTaskParentConstraintsAndVersionTriggers(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "task-parent-triggers.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	const (
		projectID    = "018f0000-0000-7000-8000-000000000501"
		rootID       = "018f0000-0000-7000-8000-000000000502"
		childID      = "018f0000-0000-7000-8000-000000000503"
		grandchildID = "018f0000-0000-7000-8000-000000000504"
		selfID       = "018f0000-0000-7000-8000-000000000505"
	)

	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, status, created_at, updated_at)
		VALUES (?, '父子触发器项目', 'planning', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, projectID); err != nil {
		t.Fatalf("insert hierarchy project: %v", err)
	}
	insertTask := func(id, title string, parentID any) {
		t.Helper()
		if _, err := store.SQL.Exec(`
			INSERT INTO tasks(id, title, status, priority, project_id, parent_task_id, created_at, updated_at)
			VALUES (?, ?, 'todo', 'P2', ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		`, id, title, projectID, parentID); err != nil {
			t.Fatalf("insert task %s: %v", title, err)
		}
	}

	insertTask(rootID, "根任务", nil)
	insertTask(childID, "子任务", rootID)
	insertTask(grandchildID, "孙任务", childID)
	assertTaskVersion(t, store.SQL, rootID, 2)
	assertTaskVersion(t, store.SQL, childID, 2)
	assertTaskVersion(t, store.SQL, grandchildID, 1)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 4 {
		t.Fatalf("project version after hierarchy inserts = %d, want 4", got)
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO tasks(id, title, status, priority, project_id, parent_task_id, created_at, updated_at)
		VALUES (?, '自引用任务', 'todo', 'P2', ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, selfID, projectID, selfID); err == nil {
		t.Fatal("self-referencing task insert succeeded")
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM tasks WHERE id = ?", selfID); got != 0 {
		t.Fatalf("self-referencing task count = %d, want 0", got)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 4 {
		t.Fatalf("project version changed after rejected insert: %d", got)
	}

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET parent_task_id = ?, version = version + 1
		WHERE id = ?
	`, rootID, rootID); err == nil {
		t.Fatal("self-referencing task update succeeded")
	}
	assertTaskVersion(t, store.SQL, rootID, 2)

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET parent_task_id = ?, version = version + 1
		WHERE id = ?
	`, grandchildID, rootID); err == nil {
		t.Fatal("multi-level task cycle succeeded")
	} else if !strings.Contains(err.Error(), "TASK_PARENT_CYCLE") {
		t.Fatalf("multi-level cycle error = %v, want TASK_PARENT_CYCLE", err)
	}
	assertTaskVersion(t, store.SQL, rootID, 2)
	assertTaskParent(t, store.SQL, rootID, nil)
	assertNoForeignKeyViolations(t, store.SQL)

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'in_progress', version = version + 1
		WHERE id = ?
	`, rootID); err != nil {
		t.Fatalf("update root status: %v", err)
	}
	assertTaskVersion(t, store.SQL, rootID, 3)
	assertTaskVersion(t, store.SQL, childID, 2)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 5 {
		t.Fatalf("project version after root status = %d, want 5", got)
	}

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'in_progress', version = version + 1
		WHERE id = ?
	`, childID); err != nil {
		t.Fatalf("update child status: %v", err)
	}
	assertTaskVersion(t, store.SQL, childID, 3)
	assertTaskVersion(t, store.SQL, rootID, 4)
	assertTaskVersion(t, store.SQL, grandchildID, 1)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 6 {
		t.Fatalf("project version after child status = %d, want 6", got)
	}

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET status = 'done', completed_at = '2026-08-27T12:00:00Z', version = version + 1
		WHERE id = ?
	`, grandchildID); err != nil {
		t.Fatalf("update grandchild status: %v", err)
	}
	assertTaskVersion(t, store.SQL, grandchildID, 2)
	assertTaskVersion(t, store.SQL, childID, 4)
	assertTaskVersion(t, store.SQL, rootID, 4)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 7 {
		t.Fatalf("project version after grandchild status = %d, want 7", got)
	}

	if _, err := store.SQL.Exec(`
		UPDATE tasks
		SET parent_task_id = ?, version = version + 1
		WHERE id = ?
	`, rootID, grandchildID); err != nil {
		t.Fatalf("reattach grandchild: %v", err)
	}
	assertTaskParent(t, store.SQL, grandchildID, stringPointerForMigrationTest(rootID))
	assertTaskVersion(t, store.SQL, grandchildID, 3)
	assertTaskVersion(t, store.SQL, childID, 5)
	assertTaskVersion(t, store.SQL, rootID, 5)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 7 {
		t.Fatalf("project version after hierarchy-only reattach = %d, want 7", got)
	}

	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", rootID); err != nil {
		t.Fatalf("delete parent task: %v", err)
	}
	assertTaskParent(t, store.SQL, childID, nil)
	assertTaskParent(t, store.SQL, grandchildID, nil)
	assertTaskVersion(t, store.SQL, childID, 6)
	assertTaskVersion(t, store.SQL, grandchildID, 4)
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectID); got != 8 {
		t.Fatalf("project version after parent delete = %d, want 8", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func openDatabaseAtVersion(t *testing.T, path string, targetVersion int) *sql.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open migration fixture database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get migration fixture connection: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMilliseconds),
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			_ = sqlDB.Close()
			t.Fatalf("configure migration fixture (%s): %v", pragma, err)
		}
	}
	if _, err := sqlDB.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("create migration fixture history: %v", err)
	}

	items, err := loadMigrations()
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("load embedded migrations: %v", err)
	}
	latest := 0
	for _, item := range items {
		if item.version > targetVersion {
			break
		}
		tx, err := sqlDB.Begin()
		if err != nil {
			_ = sqlDB.Close()
			t.Fatalf("begin fixture migration %d: %v", item.version, err)
		}
		if _, err := tx.Exec(item.sql); err != nil {
			_ = tx.Rollback()
			_ = sqlDB.Close()
			t.Fatalf("apply fixture migration %d: %v", item.version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations(version, name) VALUES (?, ?)", item.version, item.name); err != nil {
			_ = tx.Rollback()
			_ = sqlDB.Close()
			t.Fatalf("record fixture migration %d: %v", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = sqlDB.Close()
			t.Fatalf("commit fixture migration %d: %v", item.version, err)
		}
		latest = item.version
	}
	if latest != targetVersion {
		_ = sqlDB.Close()
		t.Fatalf("fixture schema version = %d, want %d", latest, targetVersion)
	}
	return sqlDB
}

func seedV5TaskFactsFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin v5 fixture seed: %v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO clients(id, name, status, created_at, updated_at) VALUES (?, '历史客户', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			args:  []any{v5ClientID},
		},
		{
			query: `INSERT INTO projects(id, name, client_id, status, created_at, updated_at) VALUES (?, '历史项目一', ?, 'in_progress', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			args:  []any{v5ProjectOneID, v5ClientID},
		},
		{
			query: `INSERT INTO projects(id, name, client_id, status, created_at, updated_at) VALUES (?, '历史项目二', ?, 'planning', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			args:  []any{v5ProjectTwoID, v5ClientID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, description, status, priority, project_id, planned_date,
					estimated_minutes, actual_minutes, manual_order, created_at, updated_at
				) VALUES (?, '历史父任务', '父任务历史正文', 'todo', 'P2', ?, '2026-08-27', 90, 20, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			`,
			args: []any{v5ParentTaskID, v5ProjectOneID},
		},
		{
			query: `
				INSERT INTO tasks(
					id, title, description, status, priority, project_id, due_date, planned_date,
					estimated_minutes, actual_minutes, manual_order, created_at, updated_at
				) VALUES (?, '历史子任务', '升级时必须保留', 'in_progress', 'P1', ?, '2026-08-28T01:02:03Z', '2026-08-27', 45, 15, 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			`,
			args: []any{v5ChildTaskID, v5ProjectOneID},
		},
		{
			query: `INSERT INTO tags(id, name, color, created_at) VALUES (?, '历史标签', '#5E6AD2', CURRENT_TIMESTAMP)`,
			args:  []any{v5TagID},
		},
		{
			query: `INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)`,
			args:  []any{v5ChildTaskID, v5TagID},
		},
		{
			query: `
				INSERT INTO focus_sessions(id, task_id, started_at, ended_at, duration_minutes, completed, created_at)
				VALUES (?, ?, '2026-08-27T01:00:00Z', '2026-08-27T01:15:00Z', 15, 1, CURRENT_TIMESTAMP)
			`,
			args: []any{v5FocusSessionID, v5ChildTaskID},
		},
		{
			query: `
				INSERT INTO idempotency_keys(key, endpoint, resource_id, request_hash, response_body, response_status, created_at)
				VALUES (?, 'POST /api/v1/tasks', ?, ?, ?, 201, CURRENT_TIMESTAMP)
			`,
			args: []any{v5IdempotencyKey, v5ChildTaskID, v5IdempotencyHash, v5SnapshotBody},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed v5 fixture: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v5 fixture: %v", err)
	}
}

func assertForeignKey(t *testing.T, db *sql.DB, table, from, parent, onDelete string) {
	t.Helper()
	rows, err := db.Query("PRAGMA foreign_key_list(" + table + ")")
	if err != nil {
		t.Fatalf("read %s foreign keys: %v", table, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, sequence int
		var referencedTable, sourceColumn, targetColumn, onUpdate, deleteAction, match string
		if err := rows.Scan(&id, &sequence, &referencedTable, &sourceColumn, &targetColumn, &onUpdate, &deleteAction, &match); err != nil {
			t.Fatalf("scan %s foreign key: %v", table, err)
		}
		if sourceColumn == from && referencedTable == parent && strings.EqualFold(deleteAction, onDelete) {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s foreign keys: %v", table, err)
	}
	if !found {
		t.Fatalf("missing foreign key %s.%s -> %s ON DELETE %s", table, from, parent, onDelete)
	}
}

func assertNoForeignKeyViolations(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("run foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var constraintID int
		if err := rows.Scan(&table, &rowID, &parent, &constraintID); err != nil {
			t.Fatalf("scan foreign_key_check violation: %v", err)
		}
		t.Fatalf("foreign key violation: table=%s rowid=%v parent=%s constraint=%d", table, rowID, parent, constraintID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign_key_check: %v", err)
	}
}

func readInt64(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("read integer with %q: %v", query, err)
	}
	return value
}

func assertTaskVersion(t *testing.T, db *sql.DB, taskID string, want int64) {
	t.Helper()
	if got := readInt64(t, db, "SELECT version FROM tasks WHERE id = ?", taskID); got != want {
		t.Fatalf("task %s version = %d, want %d", taskID, got, want)
	}
}

func assertTaskParent(t *testing.T, db *sql.DB, taskID string, want *string) {
	t.Helper()
	var got sql.NullString
	if err := db.QueryRow("SELECT parent_task_id FROM tasks WHERE id = ?", taskID).Scan(&got); err != nil {
		t.Fatalf("read task %s parent: %v", taskID, err)
	}
	if want == nil {
		if got.Valid {
			t.Fatalf("task %s parent = %q, want NULL", taskID, got.String)
		}
		return
	}
	if !got.Valid || got.String != *want {
		t.Fatalf("task %s parent = %#v, want %q", taskID, got, *want)
	}
}

func stringPointerForMigrationTest(value string) *string { return &value }
