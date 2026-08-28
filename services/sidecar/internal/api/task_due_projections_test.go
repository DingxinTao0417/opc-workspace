package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestTaskDueProjectionAdvancesPastTheFirstScanBatch(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "task-due-batch.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for index := 1; index <= 101; index++ {
		seedTaskForDueProjection(
			t, store, fmt.Sprintf("018f0000-0000-7000-8000-%012d", index),
			fmt.Sprintf("Due batch %03d", index), "todo", now.Add(time.Hour),
		)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueTasks(context.Background()); err != nil {
		t.Fatalf("first Task due batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due'", 100)
	if err := service.projectDueTasks(context.Background()); err != nil {
		t.Fatalf("second Task due batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due'", 101)
}

func TestTaskDueProjectionIsScheduledIdempotentAndDeletionCoordinated(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "task-due.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	dueSoon := seedTaskForDueProjection(t, store, "018f0000-0000-7000-8000-000000002511", "Due soon task", "in_progress", now.Add(23*time.Hour))
	overdue := seedTaskForDueProjection(t, store, "018f0000-0000-7000-8000-000000002512", "Overdue task", "todo", now.Add(-time.Minute))
	seedTaskForDueProjection(t, store, "018f0000-0000-7000-8000-000000002513", "Future task", "todo", now.Add(24*time.Hour+time.Second))
	seedTaskForDueProjection(t, store, "018f0000-0000-7000-8000-000000002514", "Completed task", "done", now.Add(time.Hour))

	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueTasks(context.Background()); err != nil {
		t.Fatalf("first Task due projection: %v", err)
	}
	if err := service.projectDueTasks(context.Background()); err != nil {
		t.Fatalf("repeat Task due projection: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due'", 2)

	var soonSource models.InboxItem
	if err := store.DB.First(&soonSource, "source_event_key = ?", taskDueEventKey(dueSoon.ID, *dueSoon.DueDate)).Error; err != nil {
		t.Fatalf("load due-soon source: %v", err)
	}
	var soonPayload map[string]any
	if err := json.Unmarshal([]byte(soonSource.PayloadJSON), &soonPayload); err != nil {
		t.Fatalf("decode due-soon payload: %v", err)
	}
	if soonSource.Kind != "event" || soonSource.DueAt == nil || *soonSource.DueAt != *dueSoon.DueDate ||
		soonPayload["due_state"] != "due_soon" || soonPayload["task_title"] != dueSoon.Title ||
		soonPayload["lead_minutes"] != float64(1440) {
		t.Fatalf("due-soon source=%#v payload=%#v", soonSource, soonPayload)
	}
	var overdueSource models.InboxItem
	if err := store.DB.First(&overdueSource, "source_event_key = ?", taskDueEventKey(overdue.ID, *overdue.DueDate)).Error; err != nil {
		t.Fatalf("load overdue source: %v", err)
	}
	if !strings.Contains(overdueSource.Title, "任务逾期") || !strings.Contains(overdueSource.PayloadJSON, `"due_state":"overdue"`) {
		t.Fatalf("overdue source=%#v", overdueSource)
	}

	rescheduledAt := formatInboxTimestamp(now.Add(22 * time.Hour))
	if err := store.DB.Model(&models.Task{}).Where("id = ?", dueSoon.ID).Updates(map[string]any{
		"due_date": rescheduledAt, "version": 2, "updated_at": formatInboxTimestamp(now),
	}).Error; err != nil {
		t.Fatalf("reschedule due Task: %v", err)
	}
	if err := service.projectDueTasks(context.Background()); err != nil {
		t.Fatalf("project rescheduled Task: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due' AND source_entity_id = ?", 2, dueSoon.ID)

	if err := store.DB.Exec(`CREATE TRIGGER fail_task_due_source_event BEFORE INSERT ON workflow_events
		WHEN NEW.action = 'source_projected' BEGIN SELECT RAISE(ABORT, 'TEST_TASK_DUE_EVENT_FAILURE'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	failing := seedTaskForDueProjection(t, store, "018f0000-0000-7000-8000-000000002515", "Rollback due task", "todo", now.Add(2*time.Hour))
	if err := service.projectTaskDue(context.Background(), failing.ID, now); err == nil {
		t.Fatal("Task due projection succeeded despite forced event failure")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due' AND source_entity_id = ?", 0, failing.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_task_due_source_event").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}

	options := Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	}
	router, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	blockedDelete := performRequest(
		router, http.MethodDelete, "/api/v1/tasks/"+dueSoon.ID+"?confirm=true", nil,
		map[string]string{"If-Match": `"2"`},
	)
	if blockedDelete.Code != http.StatusConflict || responseErrorCode(t, blockedDelete.Body.Bytes()) != "TASK_HAS_ACTIVE_INBOX_SOURCES" {
		t.Fatalf("delete active due source Task = %d: %s", blockedDelete.Code, blockedDelete.Body.String())
	}
	var dueSources []models.InboxItem
	if err := store.DB.Where("source_entity_type = 'task_due' AND source_entity_id = ?", dueSoon.ID).Order("id ASC").Find(&dueSources).Error; err != nil {
		t.Fatalf("load due sources for resolution: %v", err)
	}
	for _, source := range dueSources {
		resolved := performRequest(
			router, http.MethodPost, "/api/v1/inbox-items/"+source.ID+"/resolve",
			[]byte(`{"reason":"Deadline acknowledged"}`), map[string]string{"If-Match": `"1"`},
		)
		if resolved.Code != http.StatusOK {
			t.Fatalf("resolve due source = %d: %s", resolved.Code, resolved.Body.String())
		}
	}
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/tasks/"+dueSoon.ID+"?confirm=true", nil,
		map[string]string{"If-Match": `"2"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete coordinated due Task = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'task_due' AND source_entity_id = ? AND source_deleted_at IS NOT NULL", 2, dueSoon.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND action = 'source_deleted' AND aggregate_id IN (SELECT id FROM inbox_items WHERE source_entity_type = 'task_due' AND source_entity_id = ?)", 2, dueSoon.ID)
}

func seedTaskForDueProjection(t *testing.T, store *database.Store, id, title, status string, dueAt time.Time) models.Task {
	t.Helper()
	dueAtText := dueAt.UTC().Format(time.RFC3339Nano)
	createdAt := "2026-08-20T08:00:00Z"
	task := models.Task{
		ID: id, Title: title, Description: "", Kind: "work", Status: status,
		ReviewPolicy: "none", Priority: "P1", CompletionCriteria: "", DueDate: &dueAtText,
		ActualMinutes: 0, Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if status == "done" {
		completedAt := "2026-08-21T08:00:00Z"
		task.CompletedAt = &completedAt
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed due Task %s: %v", id, err)
	}
	return task
}
