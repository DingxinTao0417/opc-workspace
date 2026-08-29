package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClientProjectActivityProjectionMigrationUpgradesV30WithoutBackfill(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v30-client-project-activity.db")
	v30 := openDatabaseAtVersion(t, databasePath, 30)
	v30Closed := false
	defer func() {
		if !v30Closed {
			_ = v30.Close()
		}
	}()
	const (
		firstClientID          = "018f0000-0000-7000-8000-000000003101"
		secondClientID         = "018f0000-0000-7000-8000-000000003102"
		projectID              = "018f0000-0000-7000-8000-000000003103"
		completedEventID       = "018f0000-0000-7000-8000-000000003104"
		reopenedEventID        = "018f0000-0000-7000-8000-000000003105"
		manualActivityID       = "018f0000-0000-7000-8000-000000003106"
		existingProjectionID   = "018f0000-0000-7000-8000-000000003107"
		firstLegacyActivityID  = "018f0000-0000-7000-8000-000000003108"
		secondLegacyActivityID = "018f0000-0000-7000-8000-000000003109"
	)
	if _, err := v30.Exec(`
		INSERT INTO clients(id, name, status, version, created_at, updated_at)
		VALUES
			(?, '迁移前客户 A', 'active', 7, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z'),
			(?, '迁移前客户 B', 'active', 9, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, firstClientID, secondClientID); err != nil {
		t.Fatalf("seed v30 Clients: %v", err)
	}
	if _, err := v30.Exec(`
		INSERT INTO projects(id, name, client_id, status, version, created_at, updated_at)
		VALUES (?, '迁移前项目', ?, 'completed', 4, '2026-08-29T08:00:00Z', '2026-08-29T09:00:00Z')
	`, projectID, firstClientID); err != nil {
		t.Fatalf("seed v30 Project: %v", err)
	}
	if _, err := v30.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id, command_seq,
			previous_json, current_json, created_at
		) VALUES
			(?, 'project', ?, 'project_completed', ?, 1, '{"status":"in_progress"}', '{"status":"completed"}', '2026-08-29T09:00:00Z'),
			(?, 'project', ?, 'project_reopened', ?, 1, '{"status":"completed"}', '{"status":"in_progress"}', '2026-08-29T10:00:00Z')
	`, completedEventID, projectID, builtinOwnerActorID, reopenedEventID, projectID, builtinOwnerActorID); err != nil {
		t.Fatalf("seed v30 Project workflow events: %v", err)
	}
	if _, err := v30.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			source_type, source_id, version, created_at, updated_at
		) VALUES
			(?, ?, 'note', '迁移前手工记录', '保留正文', '2026-08-29T08:30:00Z', ?, NULL, NULL, 3, '2026-08-29T08:30:00Z', '2026-08-29T08:31:00Z'),
			(?, ?, 'system_reference', '项目「迁移前项目」已完成', NULL, '2026-08-29T09:00:00Z', ?, 'project_workflow_event', ?, 1, '2026-08-29T09:00:00Z', '2026-08-29T09:00:00Z'),
			(?, ?, 'system_reference', '旧来源 A', NULL, '2026-08-29T09:10:00Z', ?, 'legacy_reference', 'shared-source', 1, '2026-08-29T09:10:00Z', '2026-08-29T09:10:00Z'),
			(?, ?, 'system_reference', '旧来源 B', NULL, '2026-08-29T09:20:00Z', ?, 'legacy_reference', 'shared-source', 1, '2026-08-29T09:20:00Z', '2026-08-29T09:20:00Z')
	`,
		manualActivityID, firstClientID, builtinOwnerActorID,
		existingProjectionID, firstClientID, builtinSystemActorID, completedEventID,
		firstLegacyActivityID, firstClientID, builtinSystemActorID,
		secondLegacyActivityID, secondClientID, builtinSystemActorID,
	); err != nil {
		t.Fatalf("seed v30 Client and Project activity facts: %v", err)
	}
	var firstVersionBefore, secondVersionBefore int64
	if err := v30.QueryRow("SELECT version FROM clients WHERE id = ?", firstClientID).Scan(&firstVersionBefore); err != nil {
		t.Fatalf("read first v30 Client version: %v", err)
	}
	if err := v30.QueryRow("SELECT version FROM clients WHERE id = ?", secondClientID).Scan(&secondVersionBefore); err != nil {
		t.Fatalf("read second v30 Client version: %v", err)
	}
	if err := v30.Close(); err != nil {
		t.Fatalf("close v30 fixture: %v", err)
	}
	v30Closed = true

	store, gate, err := OpenBeforeDestructiveMigrations(databasePath)
	if err != nil {
		t.Fatalf("upgrade v30 database: %v", err)
	}
	defer store.Close()
	if gate != nil || store.SchemaVersion != 39 {
		t.Fatalf("v30 to v31 migration store=%d gate=%#v", store.SchemaVersion, gate)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM client_activities"); got != 4 {
		t.Fatalf("v31 migration changed Client activity count to %d, want 4", got)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*)
		FROM client_activities
		WHERE kind = 'system_reference' AND source_type = 'project_workflow_event'
	`); got != 1 {
		t.Fatalf("v31 migration backfilled Project workflow activities: count=%d, want 1", got)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*)
		FROM client_activities
		WHERE source_type = 'project_workflow_event' AND source_id = ?
	`, reopenedEventID); got != 0 {
		t.Fatalf("v31 migration invented reopened Project activity: count=%d", got)
	}
	var title, body, createdAt, updatedAt string
	var activityVersion int64
	if err := store.SQL.QueryRow(`
		SELECT title, body, version, created_at, updated_at
		FROM client_activities
		WHERE id = ?
	`, manualActivityID).Scan(&title, &body, &activityVersion, &createdAt, &updatedAt); err != nil {
		t.Fatalf("read preserved manual Client activity: %v", err)
	}
	if title != "迁移前手工记录" || body != "保留正文" || activityVersion != 3 ||
		createdAt != "2026-08-29T08:30:00Z" || updatedAt != "2026-08-29T08:31:00Z" {
		t.Fatalf("preserved manual Client activity title=%q body=%q version=%d created=%q updated=%q", title, body, activityVersion, createdAt, updatedAt)
	}
	var firstVersionAfter, secondVersionAfter int64
	if err := store.SQL.QueryRow("SELECT version FROM clients WHERE id = ?", firstClientID).Scan(&firstVersionAfter); err != nil {
		t.Fatalf("read first v31 Client version: %v", err)
	}
	if err := store.SQL.QueryRow("SELECT version FROM clients WHERE id = ?", secondClientID).Scan(&secondVersionAfter); err != nil {
		t.Fatalf("read second v31 Client version: %v", err)
	}
	if firstVersionAfter != firstVersionBefore || secondVersionAfter != secondVersionBefore {
		t.Fatalf("v31 migration changed Client versions: before=(%d,%d) after=(%d,%d)", firstVersionBefore, secondVersionBefore, firstVersionAfter, secondVersionAfter)
	}

	var indexSQL string
	if err := store.SQL.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_client_activities_project_workflow_event_source'
	`).Scan(&indexSQL); err != nil {
		t.Fatalf("read v31 Project workflow activity index: %v", err)
	}
	if !strings.Contains(indexSQL, "UNIQUE INDEX") ||
		!strings.Contains(indexSQL, "source_type, source_id") ||
		!strings.Contains(indexSQL, "kind = 'system_reference'") ||
		!strings.Contains(indexSQL, "source_type = 'project_workflow_event'") {
		t.Fatalf("v31 Project workflow activity index = %s", indexSQL)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*) FROM pragma_index_list('client_activities')
		WHERE name = 'idx_client_activities_project_workflow_event_source'
		  AND "unique" = 1
		  AND partial = 1
	`); got != 1 {
		t.Fatalf("v31 Project workflow activity partial unique index metadata count = %d, want 1", got)
	}

	duplicateProjectionID := "018f0000-0000-7000-8000-000000003110"
	if _, err := store.SQL.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			source_type, source_id, version, created_at, updated_at
		) VALUES (?, ?, 'system_reference', '重复投影', NULL, '2026-08-29T11:00:00Z', ?,
			'project_workflow_event', ?, 1, '2026-08-29T11:00:00Z', '2026-08-29T11:00:00Z')
	`, duplicateProjectionID, secondClientID, builtinSystemActorID, completedEventID); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("duplicate Project workflow activity error = %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_activities(
			id, client_id, kind, title, body, occurred_at, created_by_actor_id,
			source_type, source_id, version, created_at, updated_at
		) VALUES
			('018f0000-0000-7000-8000-000000003111', ?, 'system_reference', '相同 ID 的其他来源', NULL, '2026-08-29T11:01:00Z', ?, 'other_reference', ?, 1, '2026-08-29T11:01:00Z', '2026-08-29T11:01:00Z'),
			('018f0000-0000-7000-8000-000000003112', ?, 'system_reference', '第三条旧来源', NULL, '2026-08-29T11:02:00Z', ?, 'legacy_reference', 'shared-source', 1, '2026-08-29T11:02:00Z', '2026-08-29T11:02:00Z')
	`, firstClientID, builtinSystemActorID, completedEventID, secondClientID, builtinSystemActorID); err != nil {
		t.Fatalf("partial unique index blocked unrelated Client activity sources: %v", err)
	}

	assertForeignKey(t, store.SQL, "client_activities", "client_id", "clients", "CASCADE")
	assertForeignKey(t, store.SQL, "client_activities", "created_by_actor_id", "actors", "RESTRICT")
	assertForeignKey(t, store.SQL, "client_activities", "deleted_by_actor_id", "actors", "RESTRICT")
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM pragma_foreign_key_list('client_activities')"); got != 3 {
		t.Fatalf("v31 Client activity foreign key count = %d, want 3", got)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*) FROM pragma_foreign_key_list('client_activities')
		WHERE "from" = 'source_id'
	`); got != 0 {
		t.Fatalf("v31 migration added %d source_id foreign keys, want 0", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
