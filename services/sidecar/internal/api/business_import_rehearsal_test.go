package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func backupEntriesForTest(t *testing.T, backupDir string) int {
	t.Helper()
	entries, err := os.ReadDir(backupDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return len(entries)
}

func assertImportRehearsalFailure(t *testing.T, router http.Handler, body []byte, code string) {
	t.Helper()
	preview := previewBusinessImportForTest(t, router, body)
	if preview.CanApply || preview.Blocker != importBlockerApplyRehearsal || preview.ApplyMode != "" || preview.PlanDigest != "" ||
		preview.RehearsalFailure == nil || preview.RehearsalFailure.Code != code {
		t.Fatalf("preview = %+v failure = %+v", preview, preview.RehearsalFailure)
	}
}

// A package can record that a Task was deleted while the target still holds
// that Task: the target imported an earlier export. An import never deletes, so
// the reminder of the deleted Task cannot be written. Only the database knows,
// and the rehearsal asks it before the rollback backup is made.
func TestBusinessImportRehearsalRefusesADeletedSourceTheTargetStillHoldsBeforeTheBackup(t *testing.T) {
	const taskID = "018f0000-0000-7000-8000-00000000d001"
	source := newReminderHistoryWorkspace(t)
	seedTaskForDueProjection(t, source.store, taskID, "在源里删除的任务", "todo", source.now.Add(time.Hour))
	source.project()
	source.resolveReminders(taskDueInboxSourceType, taskID)
	source.send(http.MethodDelete, "/api/v1/tasks/"+taskID+"?confirm=true", "", 1, http.StatusNoContent)
	body := exportBusinessDataForTest(t, source.router)

	router, store, _, backupDir := newBackupTestAPI(t, source.now)
	seedTaskForDueProjection(t, store, taskID, "在源里删除的任务", "todo", source.now.Add(time.Hour))
	tables := []string{"tasks", "inbox_items", "workflow_events", inboxSourceImportAuthorizationsTable}
	before := tableSnapshotForTest(t, store, tables...)

	assertImportRehearsalFailure(t, router, body, "TASK_DUE_DELETED_SOURCE_COLLISION")
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation})
	assertAPIError(t, applied, http.StatusUnprocessableEntity, "IMPORT_REHEARSAL_FAILED")

	if entries := backupEntriesForTest(t, backupDir); entries != 0 {
		t.Fatalf("a refused import left %d backup entries", entries)
	}
	if after := tableSnapshotForTest(t, store, tables...); !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused import changed the target:\n before %v\n after  %v", before, after)
	}
}

func tableSnapshotForTest(t *testing.T, store *database.Store, tables ...string) map[string][][]any {
	t.Helper()
	snapshot := make(map[string][][]any, len(tables))
	for _, table := range tables {
		snapshot[table] = tableRowsForTest(t, store, table)
	}
	return snapshot
}

// The rehearsal writes every row and fires every trigger. None of that may
// remain, and the import that follows must still find the target as it was.
func TestBusinessImportRehearsalLeavesNoTraceInTheTarget(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-00000000e001', '演练来源客户', 'active', '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z');
		INSERT INTO projects(id, name, client_id, status, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-00000000e002', '演练来源项目', '018f0000-0000-7000-8000-00000000e001', 'in_progress', 1, '2026-08-28T08:01:00Z', '2026-08-28T08:01:00Z');
		INSERT INTO tasks(id, title, project_id, status, priority, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-00000000e003', '演练来源任务', '018f0000-0000-7000-8000-00000000e002', 'todo', 'P2', 1, '2026-08-28T08:02:00Z', '2026-08-28T08:02:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	body := exportBusinessDataForTest(t, sourceRouter)

	router, store, _, backupDir := newBackupTestAPI(t)
	existing := performRequest(router, http.MethodPost, "/api/v1/tasks",
		[]byte(`{"title":"目标里已有的任务","priority":"P3"}`), map[string]string{"Idempotency-Key": "rehearsal-existing"})
	if existing.Code != http.StatusCreated {
		t.Fatalf("create existing Task = %d: %s", existing.Code, existing.Body.String())
	}
	tables := []string{"clients", "projects", "tasks", "workflow_events", "inbox_items", "actors", "automation_rules", "task_focus_totals"}
	before := tableSnapshotForTest(t, store, tables...)
	backups := backupEntriesForTest(t, backupDir)

	for attempt := 1; attempt <= 2; attempt++ {
		preview := previewBusinessImportForTest(t, router, body)
		if !preview.CanApply || preview.ApplyMode != importModeAppend || preview.Blocker != "" || preview.RehearsalFailure != nil {
			t.Fatalf("preview %d = %+v", attempt, preview)
		}
		if after := tableSnapshotForTest(t, store, tables...); !reflect.DeepEqual(before, after) {
			t.Fatalf("preview %d changed the target:\n before %v\n after  %v", attempt, before, after)
		}
	}
	if after := backupEntriesForTest(t, backupDir); after != backups {
		t.Fatalf("a preview created backup entries: %d, had %d", after, backups)
	}

	plan := previewBusinessImportForTest(t, router, body)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{
		"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": plan.PlanDigest,
	})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply after rehearsals = %d: %s", applied.Code, applied.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE name = '演练来源客户'", 1)
}

func TestBusinessImportRehearsalReportsOnlyAStableRuleToken(t *testing.T) {
	for text, code := range map[string]string{
		"constraint failed: TASK_DUE_INBOX_SOURCE_NOT_CURRENT (1811)":        "TASK_DUE_INBOX_SOURCE_NOT_CURRENT",
		"SQLITE_CONSTRAINT_TRIGGER: INVALID_TASK_BLOCKED_INBOX_SOURCE":       "INVALID_TASK_BLOCKED_INBOX_SOURCE",
		"constraint failed: UNIQUE constraint failed: tags.name (2067)":      "UNIQUE_CONSTRAINT",
		"foreign key validation failed: count=2 err=<nil>":                   "FOREIGN_KEY_CONSTRAINT",
		"constraint failed: CHECK constraint failed: status (275)":           "CHECK_CONSTRAINT",
		"constraint failed: NOT NULL constraint failed: tasks.title (1299)":  "NOT_NULL_CONSTRAINT",
		"Task Artifact import replay plan is invalid for 张三 <a@example.com>": "INTEGRITY_VALIDATION",
	} {
		if got := businessImportRehearsalCode(errors.New(text)); got != code {
			t.Errorf("%q -> %q, want %q", text, got, code)
		}
	}
	for _, err := range []error{
		context.Canceled, context.DeadlineExceeded, errors.New("database is locked (5) (SQLITE_BUSY)"),
		errors.New("disk I/O error (10)"),
	} {
		if !businessImportRehearsalInterrupted(err) {
			t.Errorf("%v must not be reported as a rule that rejects the import", err)
		}
	}
	if businessImportRehearsalInterrupted(errors.New("constraint failed: TASK_DUE_INBOX_SOURCE_NOT_CURRENT (1811)")) {
		t.Error("a rule violation was taken for an interruption")
	}
}
