package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessImportProjectCompletionAuthorizationIsExactAndSingleUse(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "business-import-authorization.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()

	if store.SchemaVersion != 56 {
		t.Fatalf("SchemaVersion = %d, want 56", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'business_import_project_completion_authorizations'
	`); got != 1 {
		t.Fatalf("business import authorization table count = %d, want 1", got)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'trigger'
		  AND name IN (
			'trg_inbox_project_completion_source_insert_guard',
			'trg_inbox_project_completion_import_authorization_consumed'
		  )
	`); got != 2 {
		t.Fatalf("business import authorization trigger count = %d, want 2", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM business_import_project_completion_authorizations"); got != 0 {
		t.Fatalf("new authorization table count = %d, want 0", got)
	}

	t.Run("historical tombstone without authorization is rejected", func(t *testing.T) {
		row := projectCompletionImportAuthorizationFixture("018f0000-0000-7000-8000-000000005001")
		if err := insertHistoricalProjectCompletionInbox(store.SQL, row); err == nil ||
			!strings.Contains(err.Error(), "INVALID_PROJECT_COMPLETION_INBOX_SOURCE") {
			t.Fatalf("unauthorized historical Project completion insert error = %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", row.inboxID); got != 0 {
			t.Fatalf("unauthorized historical Project completion rows = %d, want 0", got)
		}
	})

	t.Run("wrong authorization is neither accepted nor consumed", func(t *testing.T) {
		row := projectCompletionImportAuthorizationFixture("018f0000-0000-7000-8000-000000005011")
		if err := insertProjectCompletionImportAuthorization(store.SQL, row, `{"wrong":"payload"}`); err != nil {
			t.Fatalf("insert mismatched Project completion authorization: %v", err)
		}
		if err := insertHistoricalProjectCompletionInbox(store.SQL, row); err == nil ||
			!strings.Contains(err.Error(), "INVALID_PROJECT_COMPLETION_INBOX_SOURCE") {
			t.Fatalf("mismatched historical Project completion insert error = %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", row.inboxID); got != 0 {
			t.Fatalf("mismatched historical Project completion rows = %d, want 0", got)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM business_import_project_completion_authorizations WHERE inbox_item_id = ?", row.inboxID); got != 1 {
			t.Fatalf("mismatched authorization count = %d, want retained token", got)
		}
		if _, err := store.SQL.Exec("DELETE FROM business_import_project_completion_authorizations WHERE inbox_item_id = ?", row.inboxID); err != nil {
			t.Fatalf("clean mismatched Project completion authorization: %v", err)
		}
	})

	t.Run("exact authorization is consumed once", func(t *testing.T) {
		row := projectCompletionImportAuthorizationFixture("018f0000-0000-7000-8000-000000005021")
		if err := insertProjectCompletionImportAuthorization(store.SQL, row, row.payloadJSON); err != nil {
			t.Fatalf("insert exact Project completion authorization: %v", err)
		}
		if err := insertHistoricalProjectCompletionInbox(store.SQL, row); err != nil {
			t.Fatalf("insert exactly authorized historical Project completion row: %v", err)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at = ?", row.inboxID, row.deletedAt); got != 1 {
			t.Fatalf("authorized historical Project completion rows = %d, want 1", got)
		}
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM business_import_project_completion_authorizations"); got != 0 {
			t.Fatalf("authorization count after exact insert = %d, want consumed", got)
		}
	})
}

type projectCompletionImportAuthorizationRow struct {
	inboxID     string
	projectID   string
	sourceKey   string
	payloadJSON string
	completedAt string
	deletedAt   string
}

func projectCompletionImportAuthorizationFixture(inboxID string) projectCompletionImportAuthorizationRow {
	projectID := strings.TrimSuffix(inboxID, "1") + "2"
	completedAt := "2026-09-03T09:00:00.000000000Z"
	return projectCompletionImportAuthorizationRow{
		inboxID: inboxID, projectID: projectID,
		sourceKey:   "project:" + projectID + ":completed:3",
		payloadJSON: `{"project_id":"` + projectID + `","project_name":"Authorized Historical Project","completed_at":"` + completedAt + `","completion_version":3,"incomplete_task_count":0}`,
		completedAt: completedAt, deletedAt: "2026-09-03T10:00:00.000000000Z",
	}
}

func insertProjectCompletionImportAuthorization(db *sql.DB, row projectCompletionImportAuthorizationRow, payloadJSON string) error {
	_, err := db.Exec(`
		INSERT INTO business_import_project_completion_authorizations(
			inbox_item_id, source_entity_type, source_entity_id, source_event_key, payload_json, source_deleted_at
		) VALUES (?, 'project_completion', ?, ?, ?, ?)
	`, row.inboxID, row.projectID, row.sourceKey, payloadJSON, row.deletedAt)
	return err
}

func insertHistoricalProjectCompletionInbox(db *sql.DB, row projectCompletionImportAuthorizationRow) error {
	_, err := db.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
			source_deleted_at, priority, status, resolution_policy, triaged_at,
			resolved_by_actor_id, resolved_at, resolution_reason, resolution_mode,
			payload_json, version, created_at, updated_at
		) VALUES (
			?, 'event', '项目完成待跟进：Authorized Historical Project', '请确认后续工作',
			'project_completion', ?, ?, ?, 'P1', 'resolved', 'manual', ?,
			'00000000-0000-5000-8000-000000000001', ?, 'Historical source resolved', 'manual',
			?, 3, ?, ?
		)
	`, row.inboxID, row.projectID, row.sourceKey, row.deletedAt, row.completedAt,
		row.completedAt, row.payloadJSON, row.completedAt, row.deletedAt)
	return err
}
