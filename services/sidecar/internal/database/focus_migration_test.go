package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

const (
	focusMigrationTaskID        = "018f0000-0000-7000-8000-000000001101"
	focusMigrationCompletedID   = "018f0000-0000-7000-8000-000000001102"
	focusMigrationCancelledID   = "018f0000-0000-7000-8000-000000001103"
	focusMigrationInterruptedID = "018f0000-0000-7000-8000-000000001104"
	focusMigrationLongID        = "018f0000-0000-7000-8000-000000001105"
)

func TestFocusSessionMigrationUpgradesV10WithoutChangingTaskActualMinutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "focus-v10-to-v11.db")
	v10 := openDatabaseAtVersion(t, path, 10)
	if _, err := v10.Exec(`
		INSERT INTO tasks(id, title, status, priority, actual_minutes, created_at, updated_at)
		VALUES (?, 'Historical Focus Task', 'todo', 'P2', 77, '2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z')
	`, focusMigrationTaskID); err != nil {
		t.Fatalf("seed v10 task: %v", err)
	}
	if _, err := v10.Exec(`
		INSERT INTO focus_sessions(id, task_id, started_at, ended_at, duration_minutes, completed, created_at)
		VALUES
			(?, ?, '2026-03-01T08:00:00Z', '2026-03-01T08:15:00Z', 15, 1, '2026-03-01T08:00:00Z'),
			(?, ?, '2026-03-01T09:00:00Z', '2026-03-01T09:03:00Z', 3, 0, '2026-03-01T09:00:00Z'),
			(?, ?, '2026-03-01T10:00:00Z', NULL, 2, 0, '2026-03-01T10:00:00Z'),
			(?, ?, '2026-03-01T11:00:00Z', '2026-03-01T14:00:00Z', 180, 1, '2026-03-01T11:00:00Z')
	`,
		focusMigrationCompletedID, focusMigrationTaskID,
		focusMigrationCancelledID, focusMigrationTaskID,
		focusMigrationInterruptedID, focusMigrationTaskID,
		focusMigrationLongID, focusMigrationTaskID,
	); err != nil {
		t.Fatalf("seed v10 Focus Sessions: %v", err)
	}
	if err := v10.Close(); err != nil {
		t.Fatalf("close v10 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v10 Focus database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 22 {
		t.Fatalf("SchemaVersion = %d, want 22", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT actual_minutes FROM tasks WHERE id = ?", focusMigrationTaskID); got != 77 {
		t.Fatalf("historical task actual_minutes = %d, want unchanged 77", got)
	}

	tests := []struct {
		id              string
		status          string
		plannedSeconds  int64
		accumulated     int64
		endReason       string
		creditedMinutes int64
		legacyImported  int64
	}{
		{focusMigrationCompletedID, "completed", 900, 900, "completed", 15, 1},
		{focusMigrationCancelledID, "cancelled", 300, 180, "cancelled", 0, 1},
		{focusMigrationInterruptedID, "interrupted", 300, 120, "crash_recovery", 0, 1},
		{focusMigrationLongID, "completed", 10800, 10800, "completed", 180, 1},
	}
	for _, test := range tests {
		var status, endReason string
		var planned, accumulated, credited, version, legacyImported int64
		var endedAt sql.NullString
		if err := store.SQL.QueryRow(`
			SELECT status, planned_seconds, accumulated_seconds, ended_at, end_reason, credited_minutes, version, legacy_imported
			FROM focus_sessions WHERE id = ?
		`, test.id).Scan(&status, &planned, &accumulated, &endedAt, &endReason, &credited, &version, &legacyImported); err != nil {
			t.Fatalf("read migrated Focus Session %s: %v", test.id, err)
		}
		if status != test.status || planned != test.plannedSeconds || accumulated != test.accumulated ||
			!endedAt.Valid || endReason != test.endReason || credited != test.creditedMinutes || version != 1 || legacyImported != test.legacyImported {
			t.Fatalf("migrated Focus Session %s = status=%q planned=%d accumulated=%d ended=%#v reason=%q credited=%d version=%d legacy=%d", test.id, status, planned, accumulated, endedAt, endReason, credited, version, legacyImported)
		}
		if got := readInt64(t, store.SQL, "SELECT COALESCE(SUM(duration_seconds), 0) FROM focus_session_intervals WHERE session_id = ?", test.id); got != test.accumulated {
			t.Fatalf("migrated interval seconds for %s = %d, want %d", test.id, got, test.accumulated)
		}
	}
	if got := readInt64(t, store.SQL, "SELECT exact_seconds FROM task_focus_totals WHERE task_id = ?", focusMigrationTaskID); got != 11700 {
		t.Fatalf("historical task exact Focus seconds = %d, want 11700", got)
	}
	if got := readInt64(t, store.SQL, "SELECT applied_minutes FROM task_focus_totals WHERE task_id = ?", focusMigrationTaskID); got != 195 {
		t.Fatalf("historical task applied Focus minutes = %d, want 195", got)
	}

	columns := make(map[string]bool)
	rows, err := store.SQL.Query("PRAGMA table_info(focus_sessions)")
	if err != nil {
		t.Fatalf("read Focus Session columns: %v", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			t.Fatalf("scan Focus Session column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close Focus Session columns: %v", err)
	}
	if columns["duration_minutes"] || columns["completed"] || !columns["accumulated_seconds"] || !columns["version"] {
		t.Fatalf("Focus Session v11 columns = %#v", columns)
	}
	assertForeignKey(t, store.SQL, "focus_sessions", "task_id", "tasks", "SET NULL")
	assertForeignKey(t, store.SQL, "focus_session_intervals", "session_id", "focus_sessions", "CASCADE")
	assertForeignKey(t, store.SQL, "task_focus_totals", "task_id", "tasks", "CASCADE")
	assertNoForeignKeyViolations(t, store.SQL)

	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", focusMigrationTaskID); err != nil {
		t.Fatalf("delete task referenced only by terminal Focus Sessions: %v", err)
	}
	var detachedTaskID sql.NullString
	if err := store.SQL.QueryRow("SELECT task_id FROM focus_sessions WHERE id = ?", focusMigrationCompletedID).Scan(&detachedTaskID); err != nil {
		t.Fatalf("read detached historical Focus Session: %v", err)
	}
	if detachedTaskID.Valid {
		t.Fatalf("terminal Focus Session task_id = %#v, want NULL", detachedTaskID)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM task_focus_totals WHERE task_id = ?", focusMigrationTaskID); got != 0 {
		t.Fatalf("Task Focus total after task delete = %d, want 0", got)
	}
}

func TestFocusSessionV11ConstraintsAllowOnlyOneOpenSessionAndInterval(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "focus-constraints.db"))
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer store.Close()
	const firstID = "018f0000-0000-7000-8000-000000001111"
	const secondID = "018f0000-0000-7000-8000-000000001112"
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, started_at, status, planned_seconds, accumulated_seconds,
			version, created_at, updated_at
		) VALUES (?, '2026-03-01T07:00:00Z', 'planned', 7201, 0, 1,
			'2026-03-01T07:00:00Z', '2026-03-01T07:00:00Z')
	`, "018f0000-0000-7000-8000-000000001110"); err == nil {
		t.Fatal("schema accepted a Focus Session longer than 7200 seconds")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, started_at, status, planned_seconds, accumulated_seconds,
			last_resumed_at, last_heartbeat_at, version, created_at, updated_at
		) VALUES (?, '2026-03-01T08:00:00Z', 'active', 1500, 0,
			'2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z', 1,
			'2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z')
	`, firstID); err != nil {
		t.Fatalf("insert first active Focus Session: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, started_at, status, planned_seconds, accumulated_seconds,
			version, created_at, updated_at
		) VALUES (?, '2026-03-01T09:00:00Z', 'paused', 1500, 0, 1,
			'2026-03-01T09:00:00Z', '2026-03-01T09:00:00Z')
	`, secondID); err == nil {
		t.Fatal("schema accepted a second open Focus Session")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_session_intervals(session_id, started_at, created_at)
		VALUES (?, '2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z')
	`, firstID); err != nil {
		t.Fatalf("insert first open interval: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_session_intervals(session_id, started_at, created_at)
		VALUES (?, '2026-03-01T08:01:00Z', '2026-03-01T08:01:00Z')
	`, firstID); err == nil {
		t.Fatal("schema accepted a second open Focus interval")
	}
	if _, err := store.SQL.Exec(`
		UPDATE focus_sessions SET status = 'paused' WHERE id = ?
	`, firstID); err == nil {
		t.Fatal("schema accepted paused with last_resumed_at still present")
	}
	if _, err := store.SQL.Exec(`
		UPDATE focus_sessions SET status = 'recovery_pending', last_heartbeat_at = NULL WHERE id = ?
	`, firstID); err == nil {
		t.Fatal("schema accepted recovery_pending without a heartbeat")
	}
}
