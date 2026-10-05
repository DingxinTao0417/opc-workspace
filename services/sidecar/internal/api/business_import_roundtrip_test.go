package api

import (
	"net/http"
	"reflect"
	"testing"
)

const (
	roundTripClientID  = "018f0000-0000-7000-8000-00000000f001"
	roundTripProjectID = "018f0000-0000-7000-8000-00000000f002"
	roundTripParentID  = "018f0000-0000-7000-8000-00000000f003"
	roundTripChildID   = "018f0000-0000-7000-8000-00000000f004"
)

// Inserting a child bumps its Client, Project or parent Task. An import that
// left those bumps in place would hand back rows that differ from the exported
// ones, and the same package could not be imported a second time.
func TestBusinessImportReturnsTheExportedClientAndParentTaskRows(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES ('` + roundTripClientID + `', '往返客户', 'active', '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z');
		INSERT INTO projects(id, name, client_id, status, version, created_at, updated_at)
		VALUES ('` + roundTripProjectID + `', '往返项目', '` + roundTripClientID + `', 'in_progress', 1, '2026-08-28T08:01:00Z', '2026-08-28T08:01:00Z');
		INSERT INTO tasks(id, title, project_id, status, priority, version, created_at, updated_at)
		VALUES ('` + roundTripParentID + `', '父任务', '` + roundTripProjectID + `', 'todo', 'P2', 1, '2026-08-28T08:02:00Z', '2026-08-28T08:02:00Z');
		INSERT INTO tasks(id, title, project_id, parent_task_id, status, priority, version, created_at, updated_at)
		VALUES ('` + roundTripChildID + `', '子任务', '` + roundTripProjectID + `', '` + roundTripParentID + `', 'todo', 'P2', 1, '2026-08-28T08:03:00Z', '2026-08-28T08:03:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	activity := performRequest(sourceRouter, http.MethodPost, "/api/v1/clients/"+roundTripClientID+"/activities",
		[]byte(`{"kind":"note","title":"往返记录","body":"客户确认了范围","occurred_at":"2026-08-28T09:00:00Z"}`), nil)
	if activity.Code != http.StatusCreated {
		t.Fatalf("create client activity = %d: %s", activity.Code, activity.Body.String())
	}
	note := performRequest(sourceRouter, http.MethodPost, "/api/v1/projects/"+roundTripProjectID+"/notes",
		[]byte(`{"title":"往返笔记","body":"项目笔记正文","occurred_at":"2026-08-28T09:30:00Z"}`), nil)
	if note.Code != http.StatusCreated {
		t.Fatalf("create project note = %d: %s", note.Code, note.Body.String())
	}
	// Every one of these rows was bumped by a child after it was created.
	assertDatabaseCount(t, sourceStore, "SELECT COUNT(*) FROM clients WHERE id = ? AND version > 2", 1, roundTripClientID)
	assertDatabaseCount(t, sourceStore, "SELECT COUNT(*) FROM projects WHERE id = ? AND version > 2", 1, roundTripProjectID)
	assertDatabaseCount(t, sourceStore, "SELECT COUNT(*) FROM tasks WHERE id = ? AND version = 2", 1, roundTripParentID)
	body := exportBusinessDataForTest(t, sourceRouter)

	router, store, _, _ := newBackupTestAPI(t)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	tables := []string{"clients", "projects", "tasks", "client_activities", "project_notes"}
	for _, table := range tables {
		exported, imported := tableRowsForTest(t, sourceStore, table), tableRowsForTest(t, store, table)
		if len(exported) == 0 || !reflect.DeepEqual(exported, imported) {
			t.Fatalf("%s after import:\n exported %v\n imported %v", table, exported, imported)
		}
	}

	// Nothing differs, so the same package is recognised as already imported.
	again := previewBusinessImportForTest(t, router, body)
	if !again.CanApply || again.ApplyMode != importModeAppend || again.KeyConflicts != 0 || again.SemanticConflicts != 0 {
		t.Fatalf("repeat preview = %+v", again)
	}
	for table, rows := range map[string]int{"clients": 1, "projects": 1, "tasks": 2, "client_activities": 1, "project_notes": 1} {
		if skipped := resolutionFor(again, table, importResolutionSkipIdentical); skipped != rows {
			t.Fatalf("repeat import skips %d rows of %s, want %d: %+v", skipped, table, rows, again.Resolutions)
		}
	}
	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{
		"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeat apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	for _, table := range tables {
		if exported, imported := tableRowsForTest(t, sourceStore, table), tableRowsForTest(t, store, table); !reflect.DeepEqual(exported, imported) {
			t.Fatalf("%s after the repeated import:\n exported %v\n imported %v", table, exported, imported)
		}
	}
}
