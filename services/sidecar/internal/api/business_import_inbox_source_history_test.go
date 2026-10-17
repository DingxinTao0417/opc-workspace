package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// reminderHistoryWorkspace drives a workspace through its real commands so
// that its Inbox ends up holding reminders whose sources have moved on.
type reminderHistoryWorkspace struct {
	t      *testing.T
	router http.Handler
	store  *database.Store
	now    time.Time
	tasks  int
}

func newReminderHistoryWorkspace(t *testing.T) *reminderHistoryWorkspace {
	t.Helper()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	router, store, _, _ := newBackupTestAPI(t, now)
	return &reminderHistoryWorkspace{t: t, router: router, store: store, now: now}
}

func (w *reminderHistoryWorkspace) send(method, path, body string, version int64, want int) *httptest.ResponseRecorder {
	w.t.Helper()
	headers := map[string]string{}
	if version > 0 {
		headers["If-Match"] = fmt.Sprintf(`"%d"`, version)
	}
	var payload []byte
	if body != "" {
		payload = []byte(body)
	}
	response := performRequest(w.router, method, path, payload, headers)
	if response.Code != want {
		w.t.Fatalf("%s %s = %d, want %d: %s", method, path, response.Code, want, response.Body.String())
	}
	return response
}

func (w *reminderHistoryWorkspace) version(table, id string) int64 {
	w.t.Helper()
	var version int64
	if err := w.store.DB.Table(table).Where("id = ?", id).Pluck("version", &version).Error; err != nil || version < 1 {
		w.t.Fatalf("version of %s %s = %d: %v", table, id, version, err)
	}
	return version
}

func (w *reminderHistoryWorkspace) project() {
	w.t.Helper()
	projector := &API{db: w.store.DB, options: Options{Now: func() time.Time { return w.now }}}
	if err := projector.projectDueTasks(context.Background()); err != nil {
		w.t.Fatal(err)
	}
	if err := projector.projectDueContentItems(context.Background()); err != nil {
		w.t.Fatal(err)
	}
}

func (w *reminderHistoryWorkspace) reminders(sourceType, sourceID string) []string {
	w.t.Helper()
	var ids []string
	if err := w.store.DB.Table("inbox_items").Where("source_entity_type = ? AND source_entity_id = ?", sourceType, sourceID).
		Order("created_at ASC").Order("id ASC").Pluck("id", &ids).Error; err != nil {
		w.t.Fatal(err)
	}
	return ids
}

func (w *reminderHistoryWorkspace) resolveReminders(sourceType, sourceID string) []string {
	w.t.Helper()
	ids := w.reminders(sourceType, sourceID)
	if len(ids) == 0 {
		w.t.Fatalf("no %s reminder for %s", sourceType, sourceID)
	}
	for _, id := range ids {
		w.send(http.MethodPost, "/api/v1/inbox-items/"+id+"/resolve", `{"reason":"已处理"}`, w.version("inbox_items", id), http.StatusOK)
	}
	return ids
}

func (w *reminderHistoryWorkspace) createTask(title string) string {
	w.t.Helper()
	w.tasks++
	response := performRequest(w.router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"`+title+`","priority":"P2"}`),
		map[string]string{"Idempotency-Key": fmt.Sprintf("reminder-history-%d", w.tasks)})
	if response.Code != http.StatusCreated {
		w.t.Fatalf("create Task = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.ID == "" {
		w.t.Fatalf("decode Task: %v", err)
	}
	return envelope.Data.ID
}

func (w *reminderHistoryWorkspace) block(taskID string) {
	w.t.Helper()
	w.send(http.MethodPost, "/api/v1/tasks/"+taskID+"/block", `{"reason":"等待外部答复"}`, w.version("tasks", taskID), http.StatusOK)
}

func (w *reminderHistoryWorkspace) unblock(taskID string) {
	w.t.Helper()
	w.send(http.MethodPost, "/api/v1/tasks/"+taskID+"/unblock", `{}`, w.version("tasks", taskID), http.StatusOK)
}

func (w *reminderHistoryWorkspace) seedContentItem(id, title string) {
	w.t.Helper()
	scheduledAt, timezone := formatInboxTimestamp(w.now.Add(-time.Minute)), "UTC"
	createdAt := formatInboxTimestamp(w.now.Add(-time.Hour))
	if err := w.store.DB.Create(&models.ContentItem{
		ID: id, Title: title, Platform: "Newsletter", Status: "scheduled",
		ScheduledAt: &scheduledAt, ScheduledTimezone: &timezone, Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}).Error; err != nil {
		w.t.Fatal(err)
	}
}

func (w *reminderHistoryWorkspace) createMilestone(title string) string {
	w.t.Helper()
	response := w.send(http.MethodPost, "/api/v1/roadmap/milestones",
		`{"title":"`+title+`","year":2026,"quarter":3,"target_date":"2026-08-29","status":"active"}`, 0, http.StatusCreated)
	return decodeRoadmapMilestoneResponse(w.t, response.Body.Bytes()).ID
}

const (
	historyRescheduledTaskID = "018f0000-0000-7000-8000-00000000c001"
	historyCompletedTaskID   = "018f0000-0000-7000-8000-00000000c002"
	historyDeletedTaskID     = "018f0000-0000-7000-8000-00000000c003"
	historyRevisedContentID  = "018f0000-0000-7000-8000-00000000c011"
	historyDeletedContentID  = "018f0000-0000-7000-8000-00000000c012"
)

// reminderHistory names what the workspace built by remindersOfPastStates holds.
type reminderHistory struct {
	unblockedTaskID, deletedMilestoneID string
	reopenedTombstoneID                 string
}

// remindersOfPastStates leaves reminders in every state a workspace reaches in
// ordinary use: the source still as projected, moved on, or deleted.
func (w *reminderHistoryWorkspace) remindersOfPastStates() reminderHistory {
	w.t.Helper()
	var history reminderHistory

	// Blocked and still blocked: the one reminder the old guards accepted.
	w.block(w.createTask("仍然阻塞的任务"))
	// Still blocked but edited since: the Task is one version past its reminder.
	edited := w.createTask("阻塞后改名的任务")
	w.block(edited)
	w.send(http.MethodPatch, "/api/v1/tasks/"+edited, `{"title":"阻塞后改过名的任务"}`, w.version("tasks", edited), http.StatusOK)
	// Blocked, then unblocked. The reminder stays open; nothing resolves it.
	history.unblockedTaskID = w.createTask("阻塞过的任务")
	w.block(history.unblockedTaskID)
	w.unblock(history.unblockedTaskID)

	seedTaskForDueProjection(w.t, w.store, historyRescheduledTaskID, "到期后改期的任务", "todo", w.now.Add(time.Hour))
	seedTaskForDueProjection(w.t, w.store, historyCompletedTaskID, "逾期后完成的任务", "in_progress", w.now.Add(-time.Minute))
	seedTaskForDueProjection(w.t, w.store, historyDeletedTaskID, "提醒过后删除的任务", "todo", w.now.Add(2*time.Hour))
	w.seedContentItem(historyRevisedContentID, "排期后修改的内容")
	w.seedContentItem(historyDeletedContentID, "排期后删除的内容")
	w.project()

	w.send(http.MethodPatch, "/api/v1/tasks/"+historyRescheduledTaskID, `{"due_date":"2026-09-30T12:00:00Z"}`, 1, http.StatusOK)
	w.send(http.MethodPost, "/api/v1/tasks/"+historyCompletedTaskID+"/complete", `{}`, 1, http.StatusOK)

	// A Task with both kinds of reminder, deleted after they were handled.
	w.block(historyDeletedTaskID)
	blocked := w.resolveReminders(taskBlockedInboxSourceType, historyDeletedTaskID)
	w.resolveReminders(taskDueInboxSourceType, historyDeletedTaskID)
	w.unblock(historyDeletedTaskID)
	w.send(http.MethodDelete, "/api/v1/tasks/"+historyDeletedTaskID+"?confirm=true", "", w.version("tasks", historyDeletedTaskID), http.StatusNoContent)
	// A handled reminder can be reopened after its source is gone.
	history.reopenedTombstoneID = blocked[0]
	w.send(http.MethodPost, "/api/v1/inbox-items/"+history.reopenedTombstoneID+"/reopen", "",
		w.version("inbox_items", history.reopenedTombstoneID), http.StatusOK)

	// Editing a scheduled Content Item resolves its reminder and bumps the version.
	w.send(http.MethodPatch, "/api/v1/content-items/"+historyRevisedContentID, `{"title":"排期后修改过的内容"}`, 1, http.StatusOK)
	w.project()
	w.send(http.MethodPatch, "/api/v1/content-items/"+historyDeletedContentID, `{"status":"archived"}`, 1, http.StatusOK)
	w.send(http.MethodDelete, "/api/v1/content-items/"+historyDeletedContentID+"?confirm=true", "", 2, http.StatusOK)

	// A due Milestone that was achieved and then renamed: its "due" reminder is
	// resolved, its "achieved" reminder is open and one version behind.
	achieved := w.createMilestone("到期后达成的里程碑")
	w.send(http.MethodPatch, "/api/v1/roadmap/milestones/"+achieved, `{"status":"achieved"}`, 1, http.StatusOK)
	w.send(http.MethodPatch, "/api/v1/roadmap/milestones/"+achieved, `{"title":"到期后达成并改名的里程碑"}`, 2, http.StatusOK)
	history.deletedMilestoneID = w.createMilestone("到期后删除的里程碑")
	w.send(http.MethodPost, "/api/v1/roadmap/milestones/"+history.deletedMilestoneID+"/archive", "", 1, http.StatusOK)
	w.send(http.MethodDelete, "/api/v1/roadmap/milestones/"+history.deletedMilestoneID+"?confirm=true", "", 2, http.StatusOK)

	for sourceType, want := range map[string][2]int64{
		// total reminders, of which the source is deleted
		taskBlockedInboxSourceType:      {4, 1},
		taskDueInboxSourceType:          {3, 1},
		contentItemInboxSourceType:      {3, 1},
		roadmapMilestoneInboxSourceType: {3, 1},
	} {
		assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ?", want[0], sourceType)
		assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ? AND source_deleted_at IS NOT NULL", want[1], sourceType)
	}
	assertDatabaseCount(w.t, w.store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND status = 'open' AND source_deleted_at IS NOT NULL",
		1, history.reopenedTombstoneID)
	return history
}

var reminderHistoryTables = []string{
	"tasks", "content_items", "roadmap_milestones", "inbox_items", "inbox_item_tasks", "workflow_events",
}

func assertSameTablesForTest(t *testing.T, source, target *database.Store, when string) {
	t.Helper()
	for _, table := range reminderHistoryTables {
		exported, imported := tableRowsForTest(t, source, table), tableRowsForTest(t, target, table)
		if !reflect.DeepEqual(exported, imported) {
			t.Fatalf("%s %s:\n exported %v\n imported %v", table, when, exported, imported)
		}
	}
	for _, table := range []string{"business_import_project_completion_authorizations", inboxSourceImportAuthorizationsTable} {
		assertDatabaseCount(t, target, "SELECT COUNT(*) FROM "+table, 0)
	}
	assertDatabaseCount(t, target, "SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
}

// A workspace that has been used holds reminders of things that are no longer
// true. Before schema 80 the first of them made the whole export unimportable.
func TestBusinessImportRestoresRemindersWhoseSourcesMovedOn(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	source.remindersOfPastStates()
	body := exportBusinessDataForTest(t, source.router)

	router, store, _, _ := newBackupTestAPI(t, source.now)
	preview := previewBusinessImportForTest(t, router, body)
	if !preview.CanApply || preview.ApplyMode != importModeReplaceEmpty || preview.Blocker != "" || preview.RehearsalFailure != nil {
		t.Fatalf("preview = %+v failure = %+v", preview, preview.RehearsalFailure)
	}
	// The rehearsal wrote and consumed authorizations; none may be left behind.
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM "+inboxSourceImportAuthorizationsTable, 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)

	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	assertSameTablesForTest(t, source.store, store, "after the import")

	// The imported workspace exports the same package, and importing it again
	// finds every row already there.
	again := previewBusinessImportForTest(t, router, body)
	if !again.CanApply || again.ApplyMode != importModeAppend || again.KeyConflicts != 0 || again.SemanticConflicts != 0 {
		t.Fatalf("repeat preview = %+v failure = %+v", again, again.RehearsalFailure)
	}
	if skipped := resolutionFor(again, "inbox_items", importResolutionSkipIdentical); skipped != 13 {
		t.Fatalf("repeat import skips %d reminders, want 13: %+v", skipped, again.Resolutions)
	}
	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{
		"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeat apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	assertSameTablesForTest(t, source.store, store, "after the repeated import")
}

// The imported reminders behave like the ones that were projected locally:
// the next scan does not project them again, and a Task with an open reminder
// still cannot be deleted.
func TestBusinessImportedRemindersKeepTheirLifecycle(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	history := source.remindersOfPastStates()
	body := exportBusinessDataForTest(t, source.router)

	target := newReminderHistoryWorkspace(t)
	applied := performRequest(target.router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	before := tableRowsForTest(t, target.store, "inbox_items")
	target.project()
	if after := tableRowsForTest(t, target.store, "inbox_items"); !reflect.DeepEqual(before, after) {
		t.Fatalf("the scan after the import changed the Inbox:\n before %v\n after  %v", before, after)
	}

	path := "/api/v1/tasks/" + history.unblockedTaskID + "?confirm=true"
	refused := target.send(http.MethodDelete, path, "", target.version("tasks", history.unblockedTaskID), http.StatusConflict)
	if code := responseErrorCode(t, refused.Body.Bytes()); code != "TASK_HAS_ACTIVE_INBOX_SOURCES" {
		t.Fatalf("delete with an open imported reminder = %s", code)
	}
	target.resolveReminders(taskBlockedInboxSourceType, history.unblockedTaskID)
	target.send(http.MethodDelete, path, "", target.version("tasks", history.unblockedTaskID), http.StatusNoContent)
	assertDatabaseCount(t, target.store,
		"SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ? AND source_entity_id = ? AND source_deleted_at IS NOT NULL",
		1, taskBlockedInboxSourceType, history.unblockedTaskID)
}

// The reminder of a completed Project has to be handled before the Project can
// be deleted, and it can be reopened afterwards. Such an export used to be
// refused as invalid, because a reminder of a deleted source was expected to
// stay resolved.
func TestBusinessImportRestoresReopenedReminderOfADeletedProject(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	project := createProjectForTest(t, source.router, `{"name":"完成后删除的项目"}`, nil)
	project = transitionProjectForTest(t, source.router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, source.router, project.ID, project.Version, `{"action":"complete"}`)
	reminders := source.resolveReminders(projectCompletionInboxSourceType, project.ID)
	project = transitionProjectForTest(t, source.router, project.ID, project.Version, `{"action":"archive"}`)
	source.send(http.MethodDelete, "/api/v1/projects/"+project.ID+"?confirm=true", "", project.Version, http.StatusOK)
	source.send(http.MethodPost, "/api/v1/inbox-items/"+reminders[0]+"/reopen", "", source.version("inbox_items", reminders[0]), http.StatusOK)
	assertDatabaseCount(t, source.store,
		"SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ? AND status = 'open' AND source_deleted_at IS NOT NULL",
		1, projectCompletionInboxSourceType)
	body := exportBusinessDataForTest(t, source.router)

	router, store, _, _ := newBackupTestAPI(t, source.now)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
	}
	assertSameTablesForTest(t, source.store, store, "after the import")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects", 0)
}

func reminderHistoryPackage(t *testing.T) (businessExportPackage, reminderHistory) {
	t.Helper()
	source := newReminderHistoryWorkspace(t)
	history := source.remindersOfPastStates()
	return decodeBusinessImportJSON(t, exportBusinessDataForTest(t, source.router)), history
}

func removeImportRows(t *testing.T, packageData *businessExportPackage, table string, drop func(row map[string]any) bool) int {
	t.Helper()
	target := automationImportTable(t, packageData, table)
	kept, removed := make([][]any, 0, len(target.Rows)), 0
	for index, row := range automationImportRows(*target) {
		if drop(row) {
			removed++
			continue
		}
		kept = append(kept, target.Rows[index])
	}
	target.Rows = kept
	return removed
}

func setImportCell(t *testing.T, packageData *businessExportPackage, table, id, column string, value any) {
	t.Helper()
	target := automationImportTable(t, packageData, table)
	idIndex, columnIndexValue := columnIndex(target.Columns, "id"), columnIndex(target.Columns, column)
	for _, row := range target.Rows {
		if row[idIndex] == id {
			row[columnIndexValue] = value
			return
		}
	}
	t.Fatalf("%s has no row %s", table, id)
}

func reminderIDForTest(t *testing.T, packageData *businessExportPackage, sourceType, sourceID string) string {
	t.Helper()
	for _, row := range automationImportRows(*automationImportTable(t, packageData, "inbox_items")) {
		if row["source_entity_type"] == sourceType && row["source_entity_id"] == sourceID {
			return row["id"].(string)
		}
	}
	t.Fatalf("package has no %s reminder for %s", sourceType, sourceID)
	return ""
}

// The guards are opened for a reminder only when the package itself shows
// where it came from. Each case removes or bends one piece of that history.
func TestBusinessImportReminderHistoryMustBeComplete(t *testing.T) {
	intact, _ := reminderHistoryPackage(t)
	if !validInboxSourceImportHistory(intact) {
		t.Fatal("an untouched export has an invalid reminder history")
	}
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, packageData *businessExportPackage, history reminderHistory)
	}{
		{name: "the projection event is missing", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, taskBlockedInboxSourceType, history.unblockedTaskID)
			if removed := removeImportRows(t, packageData, "workflow_events", func(row map[string]any) bool {
				return row["aggregate_id"] == id && row["action"] == "source_projected"
			}); removed != 1 {
				t.Fatalf("removed %d projection events", removed)
			}
		}},
		{name: "the projection event was written by the owner", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, taskBlockedInboxSourceType, history.unblockedTaskID)
			events := automationImportTable(t, packageData, "workflow_events")
			actor, aggregate, action := columnIndex(events.Columns, "actor_id"), columnIndex(events.Columns, "aggregate_id"), columnIndex(events.Columns, "action")
			for _, row := range events.Rows {
				if row[aggregate] == id && row[action] == "source_projected" {
					row[actor] = models.BuiltinOwnerActorID
				}
			}
		}},
		{name: "the reminder describes another block than the one projected", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, taskBlockedInboxSourceType, history.unblockedTaskID)
			table := automationImportTable(t, packageData, "inbox_items")
			payload := columnIndex(table.Columns, "payload_json")
			for _, row := range table.Rows {
				if row[columnIndex(table.Columns, "id")] == id {
					row[payload] = strings.Replace(row[payload].(string), "等待外部答复", "另一个原因", 1)
				}
			}
		}},
		{name: "the source of a reminder is not in the package", mutate: func(t *testing.T, packageData *businessExportPackage, _ reminderHistory) {
			if removed := removeImportRows(t, packageData, "content_items", func(row map[string]any) bool {
				return row["id"] == historyRevisedContentID
			}); removed != 1 {
				t.Fatalf("removed %d Content Items", removed)
			}
		}},
		{name: "a source is recorded as deleted without a deletion event", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, roadmapMilestoneInboxSourceType, history.deletedMilestoneID)
			if removed := removeImportRows(t, packageData, "workflow_events", func(row map[string]any) bool {
				return row["aggregate_id"] == id && row["action"] == "source_deleted"
			}); removed != 1 {
				t.Fatalf("removed %d deletion events", removed)
			}
		}},
		{name: "a source is recorded as deleted at another time than its event", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, roadmapMilestoneInboxSourceType, history.deletedMilestoneID)
			setImportCell(t, packageData, "inbox_items", id, "source_deleted_at", "2026-08-30T00:00:00Z")
		}},
		{name: "a reminder of an existing source claims the source was deleted", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, taskBlockedInboxSourceType, history.unblockedTaskID)
			setImportCell(t, packageData, "inbox_items", id, "source_deleted_at", "2026-08-29T12:00:00Z")
		}},
		{name: "a reminder of a deleted source hides the deletion", mutate: func(t *testing.T, packageData *businessExportPackage, history reminderHistory) {
			id := reminderIDForTest(t, packageData, roadmapMilestoneInboxSourceType, history.deletedMilestoneID)
			setImportCell(t, packageData, "inbox_items", id, "source_deleted_at", nil)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packageData, history := reminderHistoryPackage(t)
			test.mutate(t, &packageData, history)
			if validInboxSourceImportHistory(packageData) {
				t.Fatal("a reminder without its history was accepted")
			}
		})
	}
}

// A package whose reminder history does not hold is refused as invalid data,
// before the rehearsal and before any backup.
func TestBusinessImportRefusesReminderWithoutHistoryBeforeTheBackup(t *testing.T) {
	packageData, history := reminderHistoryPackage(t)
	id := reminderIDForTest(t, &packageData, taskBlockedInboxSourceType, history.unblockedTaskID)
	removeImportRows(t, &packageData, "workflow_events", func(row map[string]any) bool {
		return row["aggregate_id"] == id && row["action"] == "source_projected"
	})
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
		if !strings.Contains(response.Body.String(), "Inbox source projection history is invalid") {
			t.Fatalf("%s was refused for another reason: %s", request.path, response.Body.String())
		}
	}
	if entries := backupEntriesForTest(t, backupDir); entries != 0 {
		t.Fatalf("a refused import left %d backup entries", entries)
	}
	for _, table := range []string{"tasks", "inbox_items", "workflow_events", inboxSourceImportAuthorizationsTable} {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM "+table, 0)
	}
}

// A target that was imported earlier already holds the Task. A later export of
// the same workspace adds a reminder for it; the Task itself is skipped as
// identical, and the reminder's history is still proven by the whole package.
func TestBusinessImportAddsReminderForSourceTheTargetAlreadyHolds(t *testing.T) {
	source := newReminderHistoryWorkspace(t)
	const taskID = "018f0000-0000-7000-8000-00000000c101"
	seedTaskForDueProjection(t, source.store, taskID, "先导入、后到期的任务", "todo", source.now.Add(time.Hour))
	first := exportBusinessDataForTest(t, source.router)

	router, store, _, _ := newBackupTestAPI(t, source.now)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", first,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("first apply = %d: %s", applied.Code, applied.Body.String())
	}

	source.project()
	second := exportBusinessDataForTest(t, source.router)
	preview := previewBusinessImportForTest(t, router, second)
	if !preview.CanApply || preview.ApplyMode != importModeAppend || resolutionFor(preview, "tasks", importResolutionSkipIdentical) != 1 {
		t.Fatalf("second preview = %+v failure = %+v", preview, preview.RehearsalFailure)
	}
	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", second, map[string]string{
		"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": preview.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("second apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	assertSameTablesForTest(t, source.store, store, "after the second import")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ? AND source_entity_id = ?", 1, taskDueInboxSourceType, taskID)
}

// Schema 80 left the portable tables alone. A package that schema 79 exported
// declares the exclusion list of its time and is imported without any
// conversion step, under the upgrade confirmation.
func TestBusinessImportAcceptsSchema79Export(t *testing.T) {
	historical, _ := historicalWorkspaceExport(t, businessImportSchema79)
	if !equalStrings(historical.ExcludedOperationalTables, businessExportExcludedTablesSchema79) {
		t.Fatalf("schema 79 export declares %v", historical.ExcludedOperationalTables)
	}
	body := encodeBusinessImportJSON(t, historical)

	router, store, _, backupDir := newBackupTestAPI(t)
	preview := previewBusinessImportForTest(t, router, body)
	if !preview.CanApply || preview.ApplyMode != importModeReplaceEmpty || preview.SchemaVersion != businessImportSchema79 ||
		preview.TargetSchemaVersion != store.SchemaVersion || len(preview.UpgradeSteps) != 0 {
		t.Fatalf("schema 79 preview = %+v", preview)
	}
	refused := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	assertAPIError(t, refused, http.StatusPreconditionRequired, "IMPORT_CONFIRMATION_REQUIRED")
	if entries := backupEntriesForTest(t, backupDir); entries != 0 {
		t.Fatalf("a refused confirmation left %d backup entries", entries)
	}
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importUpgradeReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("schema 79 apply = %d: %s", applied.Code, applied.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id = ?", 1, upgradeFixturePersonID)

	// A package claiming schema 79 cannot name the table that schema 80 added.
	historical.ExcludedOperationalTables = append([]string(nil), businessExportExcludedTables...)
	smuggled := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", encodeBusinessImportJSON(t, historical), nil)
	assertAPIError(t, smuggled, http.StatusUnprocessableEntity, "IMPORT_MANIFEST_INVALID")
}
