package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// inboxSourceAuthorizationCase describes one guarded reminder source. The seed
// leaves the source in a state the reminder's payload does not describe, the
// way it looks after the source moved on.
type inboxSourceAuthorizationCase struct {
	name          string
	sourceType    string
	seedSource    string
	payload       func(sourceID string) string
	sourceKey     func(sourceID string) string
	dueAt         any
	invalid       string
	notCurrent    string
	deletedSource string
}

var inboxSourceAuthorizationCases = []inboxSourceAuthorizationCase{
	{
		name: "blocked Task", sourceType: "task",
		seedSource: `INSERT INTO tasks(id, title, status, priority, version, created_at, updated_at)
			VALUES (?, 'Imported task', 'todo', 'P1', 6, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')`,
		payload: func(id string) string {
			return `{"task_id":"` + id + `","task_title":"Imported task","blocked_reason":"Waiting","blocked_at":"2026-08-28T10:00:00Z","blocked_from_status":"in_progress","block_version":4}`
		},
		sourceKey: func(id string) string { return "task:" + id + ":blocked:4" },
		invalid:   "INVALID_TASK_BLOCKED_INBOX_SOURCE", notCurrent: "TASK_BLOCKED_INBOX_SOURCE_NOT_CURRENT",
		deletedSource: "TASK_BLOCKED_DELETED_SOURCE_COLLISION",
	},
	{
		name: "due Task", sourceType: "task_due",
		seedSource: `INSERT INTO tasks(id, title, status, priority, due_date, version, created_at, updated_at)
			VALUES (?, 'Imported task', 'todo', 'P1', '2026-09-30T12:00:00Z', 6, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')`,
		payload: func(id string) string {
			return `{"task_id":"` + id + `","task_title":"Imported task","due_at":"2026-08-29T12:00:00Z","projected_at":"2026-08-28T12:00:00Z","due_state":"due_soon","lead_minutes":1440}`
		},
		sourceKey: func(id string) string { return "task:" + id + ":due:2026-08-29T12:00:00Z" },
		dueAt:     "2026-08-29T12:00:00Z",
		invalid:   "INVALID_TASK_DUE_INBOX_SOURCE", notCurrent: "TASK_DUE_INBOX_SOURCE_NOT_CURRENT",
		deletedSource: "TASK_DUE_DELETED_SOURCE_COLLISION",
	},
	{
		name: "Content Item", sourceType: "content_item",
		seedSource: `INSERT INTO content_items(id, title, platform, status, scheduled_at, scheduled_timezone, manual_order, version, created_at, updated_at)
			VALUES (?, 'Imported release', 'blog', 'scheduled', '2026-09-05T09:00:00Z', 'Asia/Shanghai', 10, 2, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')`,
		payload: func(id string) string {
			return `{"content_item_id":"` + id + `","event_type":"publish_due","content_version":1,"scheduled_at":"2026-09-01T09:00:00Z","scheduled_timezone":"Asia/Shanghai"}`
		},
		sourceKey: func(id string) string { return "content:" + id + ":publish_due:1" },
		dueAt:     "2026-09-01T09:00:00Z",
		invalid:   "INVALID_CONTENT_ITEM_INBOX_SOURCE", notCurrent: "CONTENT_ITEM_INBOX_SOURCE_NOT_FOUND",
		deletedSource: "CONTENT_ITEM_DELETED_SOURCE_COLLISION",
	},
	{
		name: "Roadmap Milestone", sourceType: "roadmap_milestone",
		seedSource: `INSERT INTO roadmap_milestones(id, title, year, quarter, target_date, status, manual_order, version, created_at, updated_at)
			VALUES (?, 'Imported launch', 2026, 3, '2026-08-29', 'achieved', 1024, 2, '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z')`,
		payload: func(id string) string {
			return `{"roadmap_milestone_id":"` + id + `","event_type":"due","milestone_version":1,"target_date":"2026-08-29","year":2026,"quarter":3}`
		},
		sourceKey: func(id string) string { return "roadmap:" + id + ":due:1" },
		dueAt:     "2026-08-30T06:59:59Z",
		invalid:   "INVALID_ROADMAP_MILESTONE_INBOX_SOURCE", notCurrent: "ROADMAP_MILESTONE_INBOX_SOURCE_NOT_FOUND",
		deletedSource: "ROADMAP_MILESTONE_DELETED_SOURCE_COLLISION",
	},
}

const inboxSourceAuthorizationDeletedAt = "2026-09-03T10:00:00Z"

type inboxSourceAuthorizationRow struct {
	inboxID, sourceID, sourceKey, payload string
}

func inboxSourceAuthorizationFixture(source inboxSourceAuthorizationCase, sourceIndex, row int) inboxSourceAuthorizationRow {
	sourceID := fmt.Sprintf("018f0000-0000-7000-8000-0000008%d%02d02", sourceIndex, row)
	return inboxSourceAuthorizationRow{
		inboxID:   fmt.Sprintf("018f0000-0000-7000-8000-0000008%d%02d01", sourceIndex, row),
		sourceID:  sourceID,
		sourceKey: source.sourceKey(sourceID),
		payload:   source.payload(sourceID),
	}
}

func authorizeInboxSourceImportForTest(db *sql.DB, source inboxSourceAuthorizationCase, row inboxSourceAuthorizationRow, payload string, deletedAt any) error {
	_, err := db.Exec(`
		INSERT INTO business_import_inbox_source_authorizations(
			inbox_item_id, source_entity_type, source_entity_id, source_event_key, payload_json, source_deleted_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, row.inboxID, source.sourceType, row.sourceID, row.sourceKey, payload, deletedAt)
	return err
}

// insertImportedReminderForTest writes the reminder the way an import does:
// in its final state. A reminder of a deleted source is resolved.
func insertImportedReminderForTest(db *sql.DB, source inboxSourceAuthorizationCase, row inboxSourceAuthorizationRow, deletedAt any) error {
	status, triagedAt, resolvedBy, resolvedAt, reason, mode := "open", any(nil), any(nil), any(nil), any(nil), any(nil)
	if deletedAt != nil {
		status, triagedAt, resolvedBy = "resolved", "2026-09-02T09:00:00Z", builtinOwnerActorID
		resolvedAt, reason, mode = "2026-09-02T09:00:00Z", "handled", "manual"
	}
	_, err := db.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
			source_deleted_at, priority, status, resolution_policy, due_at, triaged_at,
			resolved_by_actor_id, resolved_at, resolution_reason, resolution_mode,
			payload_json, version, created_at, updated_at
		) VALUES (?, 'event', 'Imported reminder', '', ?, ?, ?, ?, 'P1', ?, 'manual', ?, ?, ?, ?, ?, ?, ?, 3,
			'2026-08-28T12:00:00Z', '2026-09-03T10:00:00Z')
	`, row.inboxID, source.sourceType, row.sourceID, row.sourceKey, deletedAt, status, source.dueAt, triagedAt,
		resolvedBy, resolvedAt, reason, mode, row.payload)
	return err
}

func TestBusinessImportInboxSourceAuthorizationIsExactAndSingleUse(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "inbox-source-authorization.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 80 {
		t.Fatalf("SchemaVersion = %d, want 80", store.SchemaVersion)
	}
	pending := func() int64 {
		return readInt64(t, store.SQL, "SELECT COUNT(*) FROM business_import_inbox_source_authorizations")
	}
	inboxRows := func(id string) int64 {
		return readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", id)
	}
	wantRejected := func(t *testing.T, err error, token, inboxID string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), token) {
			t.Fatalf("insert error = %v, want %s", err, token)
		}
		if inboxRows(inboxID) != 0 {
			t.Fatal("a rejected reminder was written")
		}
	}
	if pending() != 0 {
		t.Fatal("a new workspace holds import authorizations")
	}

	for sourceIndex, source := range inboxSourceAuthorizationCases {
		seed := func(t *testing.T, row inboxSourceAuthorizationRow) {
			t.Helper()
			if _, err := store.SQL.Exec(source.seedSource, row.sourceID); err != nil {
				t.Fatalf("seed source: %v", err)
			}
		}
		t.Run(source.name, func(t *testing.T) {
			t.Run("a reminder of a past state is rejected without an authorization", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 1)
				seed(t, row)
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, nil), source.notCurrent, row.inboxID)
			})

			t.Run("an authorization for other content is neither accepted nor consumed", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 2)
				seed(t, row)
				other := inboxSourceAuthorizationFixture(source, sourceIndex, 3)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, other.payload, nil); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, nil), source.notCurrent, row.inboxID)
				if pending() != 1 {
					t.Fatal("the mismatched authorization was consumed")
				}
				if _, err := store.SQL.Exec("DELETE FROM business_import_inbox_source_authorizations"); err != nil {
					t.Fatal(err)
				}
			})

			t.Run("an exact authorization admits one reminder and is consumed", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 4)
				seed(t, row)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, row.payload, nil); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				if err := insertImportedReminderForTest(store.SQL, source, row, nil); err != nil {
					t.Fatalf("insert authorized reminder: %v", err)
				}
				if inboxRows(row.inboxID) != 1 || pending() != 0 {
					t.Fatalf("rows = %d, authorizations left = %d", inboxRows(row.inboxID), pending())
				}
				// The same identity cannot be written a second time on that authorization.
				if _, err := store.SQL.Exec("DELETE FROM inbox_items WHERE id = ?", row.inboxID); err != nil {
					t.Fatalf("remove the reminder again: %v", err)
				}
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, nil), source.notCurrent, row.inboxID)
			})

			t.Run("an authorized reminder still needs its source", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 5)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, row.payload, nil); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, nil), source.notCurrent, row.inboxID)
				if _, err := store.SQL.Exec("DELETE FROM business_import_inbox_source_authorizations"); err != nil {
					t.Fatal(err)
				}
			})

			t.Run("a reminder of a deleted source is rejected without an authorization", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 6)
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, inboxSourceAuthorizationDeletedAt), source.invalid, row.inboxID)
			})

			t.Run("an authorization without the deletion does not cover a deleted source", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 7)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, row.payload, nil); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, inboxSourceAuthorizationDeletedAt), source.invalid, row.inboxID)
				if _, err := store.SQL.Exec("DELETE FROM business_import_inbox_source_authorizations"); err != nil {
					t.Fatal(err)
				}
			})

			t.Run("an authorized reminder of a deleted source is admitted", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 8)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, row.payload, inboxSourceAuthorizationDeletedAt); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				if err := insertImportedReminderForTest(store.SQL, source, row, inboxSourceAuthorizationDeletedAt); err != nil {
					t.Fatalf("insert authorized reminder of a deleted source: %v", err)
				}
				if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at = ?",
					row.inboxID, inboxSourceAuthorizationDeletedAt); got != 1 || pending() != 0 {
					t.Fatalf("rows = %d, authorizations left = %d", got, pending())
				}
			})

			t.Run("a source recorded as deleted must not exist", func(t *testing.T) {
				row := inboxSourceAuthorizationFixture(source, sourceIndex, 9)
				seed(t, row)
				if err := authorizeInboxSourceImportForTest(store.SQL, source, row, row.payload, inboxSourceAuthorizationDeletedAt); err != nil {
					t.Fatalf("authorize: %v", err)
				}
				wantRejected(t, insertImportedReminderForTest(store.SQL, source, row, inboxSourceAuthorizationDeletedAt), source.deletedSource, row.inboxID)
				if _, err := store.SQL.Exec("DELETE FROM business_import_inbox_source_authorizations"); err != nil {
					t.Fatal(err)
				}
			})
		})
	}

	t.Run("only the four guarded sources can be authorized", func(t *testing.T) {
		for _, sourceType := range []string{"project_completion", "task_artifact", "invoice_due", "manual"} {
			_, err := store.SQL.Exec(`
				INSERT INTO business_import_inbox_source_authorizations(
					inbox_item_id, source_entity_type, source_entity_id, source_event_key, payload_json
				) VALUES ('018f0000-0000-7000-8000-000000089001', ?, '018f0000-0000-7000-8000-000000089002', 'other:key', '{}')
			`, sourceType)
			if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Fatalf("authorization for %s: %v", sourceType, err)
			}
		}
	})
	assertNoForeignKeyViolations(t, store.SQL)
}

// The migration only adds an empty table and replaces four insert guards.
// Reminders that existed before it stay as they were.
func TestBusinessImportInboxSourceAuthorizationMigrationKeepsExistingReminders(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v79-to-v80.db")
	v79 := openDatabaseAtVersion(t, databasePath, 79)
	const taskID = "018f0000-0000-7000-8000-000000088002"
	const inboxID = "018f0000-0000-7000-8000-000000088001"
	payload := `{"task_id":"` + taskID + `","task_title":"Blocked before the upgrade","blocked_reason":"Waiting","blocked_at":"2026-08-28T10:00:00Z","blocked_from_status":"todo","block_version":2}`
	if _, err := v79.Exec(`
		INSERT INTO tasks(id, title, status, priority, version, blocked_reason, blocked_at, blocked_from_status, created_at, updated_at)
		VALUES (?, 'Blocked before the upgrade', 'blocked', 'P1', 2, 'Waiting', '2026-08-28T10:00:00Z', 'todo', '2026-08-28T08:00:00Z', '2026-08-28T10:00:00Z');
	`, taskID); err != nil {
		t.Fatalf("seed blocked Task: %v", err)
	}
	if _, err := v79.Exec(`
		INSERT INTO inbox_items(id, kind, title, summary, source_entity_type, source_entity_id, source_event_key, priority, status, resolution_policy, payload_json, version, created_at, updated_at)
		VALUES (?, 'event', '任务阻塞：Blocked before the upgrade', '', 'task', ?, ?, 'P1', 'open', 'manual', ?, 1, '2026-08-28T10:00:00Z', '2026-08-28T10:00:00Z')
	`, inboxID, taskID, "task:"+taskID+":blocked:2", payload); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}
	before := readInboxRowForTest(t, v79, inboxID)
	if err := v79.Close(); err != nil {
		t.Fatalf("close v79 fixture: %v", err)
	}

	// Nothing here rebuilds a table with rows, so an existing workspace is not
	// stopped for a rollback package first.
	store, gate, err := OpenBeforeDestructiveMigrations(databasePath)
	if err != nil {
		t.Fatalf("upgrade v79 database: %v", err)
	}
	defer store.Close()
	if gate != nil || store.SchemaVersion != 80 {
		t.Fatalf("gate = %+v schema = %d, want an ungated upgrade to 80", gate, store.SchemaVersion)
	}
	if after := readInboxRowForTest(t, store.SQL, inboxID); after != before {
		t.Fatalf("reminder changed in the upgrade:\n before %s\n after  %s", before, after)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'trigger' AND name IN (
			'trg_inbox_task_blocked_source_insert_guard', 'trg_inbox_task_due_source_insert_guard',
			'content_item_inbox_source_insert_guard', 'roadmap_milestone_inbox_source_insert_guard',
			'trg_inbox_source_import_authorization_consumed'
		) AND sql LIKE '%business_import_inbox_source_authorizations%'
	`); got != 5 {
		t.Fatalf("guards that know the authorization table = %d, want 5", got)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM business_import_inbox_source_authorizations"); got != 0 {
		t.Fatalf("the upgrade created %d authorizations", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func readInboxRowForTest(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var row string
	if err := db.QueryRow(`
		SELECT json_array(id, kind, title, summary, source_entity_type, source_entity_id, source_event_key,
			source_deleted_at, priority, status, resolution_policy, due_at, payload_json, version, created_at, updated_at)
		FROM inbox_items WHERE id = ?
	`, id).Scan(&row); err != nil {
		t.Fatalf("read reminder: %v", err)
	}
	return row
}
