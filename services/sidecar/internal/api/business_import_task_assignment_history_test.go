package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// assignmentHistory names what assignmentsOfPastStates leaves behind.
type assignmentHistory struct {
	completedTaskID, cancelledTaskID, reassignedTaskID, reopenedTaskID, openTaskID string
	retiredPersonID                                                                string
}

func (w *reminderHistoryWorkspace) createPerson(name string) string {
	w.t.Helper()
	response := w.send(http.MethodPost, "/api/v1/actors", `{"type":"person","display_name":"`+name+`"}`, 0, http.StatusCreated)
	return decodeActorResponse(w.t, response.Body.Bytes()).ID
}

func (w *reminderHistoryWorkspace) assign(taskID, actorID string) string {
	w.t.Helper()
	response := w.send(http.MethodPost, "/api/v1/tasks/"+taskID+"/assignments",
		fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, actorID), w.version("tasks", taskID), http.StatusCreated)
	var envelope struct {
		Data struct {
			Assignment struct {
				ID string `json:"id"`
			} `json:"assignment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.Assignment.ID == "" {
		w.t.Fatalf("decode assignment: %v: %s", err, response.Body.String())
	}
	return envelope.Data.Assignment.ID
}

// assignmentsOfPastStates leaves ended assignments in every place ordinary use
// puts them: on Tasks that were completed, cancelled or reopened, and with a
// person who was deactivated once their work was over.
func (w *reminderHistoryWorkspace) assignmentsOfPastStates() assignmentHistory {
	w.t.Helper()
	var history assignmentHistory
	active := w.createPerson("仍在岗的成员")
	history.retiredPersonID = w.createPerson("已离开的成员")

	// Completing or cancelling a Task ends its assignments.
	history.completedTaskID = w.createTask("指派后完成的任务")
	w.assign(history.completedTaskID, active)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.completedTaskID+"/complete", `{}`, w.version("tasks", history.completedTaskID), http.StatusOK)
	history.cancelledTaskID = w.createTask("指派后取消的任务")
	w.assign(history.cancelledTaskID, active)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.cancelledTaskID+"/cancel", `{"reason":"不再需要"}`, w.version("tasks", history.cancelledTaskID), http.StatusOK)

	// Handed over to someone else, then completed.
	history.reassignedTaskID = w.createTask("改派后完成的任务")
	w.assign(history.reassignedTaskID, history.retiredPersonID)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.reassignedTaskID+"/reassign",
		fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"交接"}`, active), w.version("tasks", history.reassignedTaskID), http.StatusOK)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.reassignedTaskID+"/complete", `{}`, w.version("tasks", history.reassignedTaskID), http.StatusOK)

	// Completed and reopened: the ended assignment stays on an open Task.
	history.reopenedTaskID = w.createTask("完成后重新打开的任务")
	w.assign(history.reopenedTaskID, active)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.reopenedTaskID+"/complete", `{}`, w.version("tasks", history.reopenedTaskID), http.StatusOK)
	w.send(http.MethodPost, "/api/v1/tasks/"+history.reopenedTaskID+"/reopen", `{}`, w.version("tasks", history.reopenedTaskID), http.StatusOK)

	// An open Task whose assignee stepped away: the assignment is ended so the
	// person can be deactivated, and the Task keeps it as history.
	history.openTaskID = w.createTask("成员离开后仍在进行的任务")
	ended := w.assign(history.openTaskID, history.retiredPersonID)
	w.send(http.MethodPost, "/api/v1/assignments/"+ended+"/end", `{"reason":"成员离开"}`, w.version("tasks", history.openTaskID), http.StatusOK)
	w.send(http.MethodPatch, "/api/v1/actors/"+history.retiredPersonID, `{"status":"inactive"}`, w.version("actors", history.retiredPersonID), http.StatusOK)

	assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM task_assignments WHERE unassigned_at IS NOT NULL", 6)
	assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM task_assignments WHERE unassigned_at IS NULL", 0)
	assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM tasks WHERE status IN ('done', 'cancelled')", 3)
	assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM actors WHERE id = ? AND status = 'inactive'", 1, history.retiredPersonID)
	return history
}

var assignmentHistoryTables = []string{"actors", "tasks", "task_assignments", "workflow_events"}

// assignmentHistoryRowsForTest leaves out the built-in Actors: every workspace
// creates its own, and an import keeps the target's.
func assignmentHistoryRowsForTest(t *testing.T, store *database.Store, table string) [][]any {
	t.Helper()
	rows := tableRowsForTest(t, store, table)
	if table != "actors" {
		return rows
	}
	kept := make([][]any, 0, len(rows))
	for _, row := range rows {
		if row[0] != models.BuiltinOwnerActorID && row[0] != models.BuiltinSystemActorID {
			kept = append(kept, row)
		}
	}
	return kept
}

func assertSameAssignmentTablesForTest(t *testing.T, source, target *reminderHistoryWorkspace, when string) {
	t.Helper()
	for _, table := range assignmentHistoryTables {
		exported, imported := assignmentHistoryRowsForTest(t, source.store, table), assignmentHistoryRowsForTest(t, target.store, table)
		if !reflect.DeepEqual(exported, imported) {
			t.Fatalf("%s %s:\n exported %v\n imported %v", table, when, exported, imported)
		}
	}
	assertDatabaseCount(t, target.store, "SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
}

// Every workspace that has assigned work holds ended assignments on finished
// Tasks, and often on people who have since been deactivated. Writing those
// rows after their Task used to trip the online guards: TASK_NOT_ASSIGNABLE
// for a finished Task, ASSIGNMENT_ACTOR_NOT_ACTIVE for a deactivated person.
func TestBusinessImportRestoresEndedAssignments(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	source.assignmentsOfPastStates()
	body := exportBusinessDataForTest(t, source.router)

	target := newReminderHistoryWorkspace(t)
	preview := previewBusinessImportForTest(t, target.router, body)
	if !preview.CanApply || preview.ApplyMode != importModeReplaceEmpty || preview.Blocker != "" || preview.RehearsalFailure != nil {
		t.Fatalf("preview = %+v failure = %+v", preview, preview.RehearsalFailure)
	}
	applied := performRequest(target.router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	assertSameAssignmentTablesForTest(t, source, target, "after the import")

	again := previewBusinessImportForTest(t, target.router, body)
	if !again.CanApply || again.ApplyMode != importModeAppend || again.KeyConflicts != 0 || again.SemanticConflicts != 0 {
		t.Fatalf("repeat preview = %+v failure = %+v", again, again.RehearsalFailure)
	}
	repeated := performRequest(target.router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{
		"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeat apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	assertSameAssignmentTablesForTest(t, source, target, "after the repeated import")
}

// The imported history does not make finished work assignable again, and a
// deactivated person stays deactivated.
func TestBusinessImportedAssignmentHistoryKeepsTheOnlineRules(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	history := source.assignmentsOfPastStates()
	body := exportBusinessDataForTest(t, source.router)

	target := newReminderHistoryWorkspace(t)
	applied := performRequest(target.router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	owner := fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, models.BuiltinOwnerActorID)
	for _, taskID := range []string{history.completedTaskID, history.cancelledTaskID} {
		refused := target.send(http.MethodPost, "/api/v1/tasks/"+taskID+"/assignments", owner, target.version("tasks", taskID), http.StatusConflict)
		if code := assignmentError(t, refused.Body.Bytes()).Code; code != "TASK_NOT_ASSIGNABLE" {
			t.Fatalf("assign a finished imported Task = %s", code)
		}
	}
	retired := fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, history.retiredPersonID)
	refused := target.send(http.MethodPost, "/api/v1/tasks/"+history.openTaskID+"/assignments", retired,
		target.version("tasks", history.openTaskID), http.StatusConflict)
	if code := assignmentError(t, refused.Body.Bytes()).Code; code != "ASSIGNMENT_ACTOR_NOT_ACTIVE" {
		t.Fatalf("assign a deactivated imported person = %s", code)
	}
	target.assign(history.reopenedTaskID, models.BuiltinOwnerActorID)
}

// A finished Task cannot hold an active assignment. A package that says it does
// is refused as invalid data, before the rehearsal and before any backup.
func TestBusinessImportRefusesActiveAssignmentOfAFinishedTask(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	history := source.assignmentsOfPastStates()
	var assignmentID string
	if err := source.store.DB.Table("task_assignments").Where("task_id = ?", history.completedTaskID).Pluck("id", &assignmentID).Error; err != nil {
		t.Fatal(err)
	}
	packageData := decodeBusinessImportJSON(t, exportBusinessDataForTest(t, source.router))
	setImportCell(t, &packageData, "task_assignments", assignmentID, "unassigned_at", nil)
	setImportCell(t, &packageData, "task_assignments", assignmentID, "reason", nil)
	body := encodeBusinessImportJSON(t, packageData)

	router, store, _, backupDir := newBackupTestAPI(t)
	for _, request := range []struct {
		path    string
		headers map[string]string
	}{
		{path: "/api/v1/imports/business-data/preview"},
		{path: "/api/v1/imports/business-data", headers: map[string]string{"X-Import-Confirmation": importReplaceConfirmation}},
	} {
		response := performRequest(router, http.MethodPost, request.path, body, request.headers)
		assertAPIError(t, response, http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID")
		if !strings.Contains(response.Body.String(), "Task assignment history is invalid") {
			t.Fatalf("%s was refused for another reason: %s", request.path, response.Body.String())
		}
	}
	if entries := backupEntriesForTest(t, backupDir); entries != 0 {
		t.Fatalf("a refused import left %d backup entries", entries)
	}
	for _, table := range []string{"tasks", "task_assignments"} {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM "+table, 0)
	}
}

// An ended assignment of a person the package does not hold is still refused by
// the rule that names it, before any backup.
func TestBusinessImportRefusesEndedAssignmentOfAMissingPerson(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	history := source.assignmentsOfPastStates()
	var assignmentID string
	if err := source.store.DB.Table("task_assignments").Where("task_id = ?", history.openTaskID).Pluck("id", &assignmentID).Error; err != nil {
		t.Fatal(err)
	}
	packageData := decodeBusinessImportJSON(t, exportBusinessDataForTest(t, source.router))
	setImportCell(t, &packageData, "task_assignments", assignmentID, "actor_id", "018f0000-0000-7000-8000-00000000d099")
	body := encodeBusinessImportJSON(t, packageData)

	router, _, _, backupDir := newBackupTestAPI(t)
	assertImportRehearsalFailure(t, router, body, "ASSIGNMENT_ACTOR_NOT_ACTIVE")
	if entries := backupEntriesForTest(t, backupDir); entries != 0 {
		t.Fatalf("a refused import left %d backup entries", entries)
	}
}
