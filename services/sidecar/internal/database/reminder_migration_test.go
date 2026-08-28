package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestReminderMigrationUpgradesV13WithoutChangingExistingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v13-reminder-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() current database error = %v", err)
	}
	const (
		taskID  = "018f0000-0000-7000-8000-000000001401"
		inboxID = "018f0000-0000-7000-8000-000000001402"
	)
	task := models.Task{
		ID: taskID, Title: "迁移前任务", Description: "保持原始事实", Kind: "work",
		Status: "todo", ReviewPolicy: "none", Priority: "P1", Version: 4,
		CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T09:00:00Z",
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed v13 Task: %v", err)
	}
	inbox := models.InboxItem{
		ID: inboxID, Kind: "manual", Title: "迁移前收件箱", Summary: "保持原始事实",
		SourceEntityType: "manual", Priority: "P2", Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: `{}`, Version: 3, CreatedAt: "2026-08-28T08:10:00Z", UpdatedAt: "2026-08-28T09:10:00Z",
	}
	if err := store.DB.Create(&inbox).Error; err != nil {
		t.Fatalf("seed v13 Inbox Item: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE reminders").Error; err != nil {
		t.Fatalf("remove v14 Reminder table: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 14").Error; err != nil {
		t.Fatalf("rewind fixture migration 14 history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close v13 fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v13 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 20 {
		t.Fatalf("SchemaVersion = %d, want 20", store.SchemaVersion)
	}
	var preservedTask models.Task
	if err := store.DB.First(&preservedTask, "id = ?", taskID).Error; err != nil {
		t.Fatalf("load preserved Task: %v", err)
	}
	if preservedTask.Title != task.Title || preservedTask.Description != task.Description ||
		preservedTask.Version != task.Version || preservedTask.CreatedAt != task.CreatedAt ||
		preservedTask.UpdatedAt != task.UpdatedAt {
		t.Fatalf("preserved Task = %#v", preservedTask)
	}
	var preservedInbox models.InboxItem
	if err := store.DB.First(&preservedInbox, "id = ?", inboxID).Error; err != nil {
		t.Fatalf("load preserved Inbox Item: %v", err)
	}
	if preservedInbox.Title != inbox.Title || preservedInbox.Summary != inbox.Summary ||
		preservedInbox.Version != inbox.Version || preservedInbox.CreatedAt != inbox.CreatedAt ||
		preservedInbox.UpdatedAt != inbox.UpdatedAt {
		t.Fatalf("preserved Inbox Item = %#v", preservedInbox)
	}
	var reminderCount int64
	if err := store.DB.Model(&models.Reminder{}).Count(&reminderCount).Error; err != nil || reminderCount != 0 {
		t.Fatalf("new Reminder table count=%d err=%v", reminderCount, err)
	}
}

func TestReminderMigrationConstrainsLifecycleAndProjection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "reminders.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	const reminderID = "018f0000-0000-7000-8000-000000001411"
	now := "2026-08-28T10:00:00.000000000Z"
	reminder := models.Reminder{
		ID: reminderID, SourceEntityType: "manual", Title: "提交本地提醒", Summary: "只在本机处理",
		Priority: "P1", TriggerAt: "2026-08-28T11:00:00.000000000Z", Status: "scheduled",
		SourceEventKey: "reminder:" + reminderID + ":due", CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&reminder).Error; err != nil {
		t.Fatalf("create valid Reminder: %v", err)
	}
	duplicate := reminder
	duplicate.ID = "018f0000-0000-7000-8000-000000001412"
	if err := store.DB.Create(&duplicate).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate source event error = %v", err)
	}
	if err := store.DB.Model(&models.Reminder{}).Where("id = ?", reminderID).
		Update("source_event_key", "changed").Error; err == nil || !strings.Contains(err.Error(), "REMINDER_IDENTITY_IMMUTABLE") {
		t.Fatalf("immutable event key error = %v", err)
	}
	if err := store.DB.Model(&models.Reminder{}).Where("id = ?", reminderID).Updates(map[string]any{
		"status": "fired", "fired_at": now, "inbox_item_id": "018f0000-0000-7000-8000-000000001499",
	}).Error; err == nil {
		t.Fatal("Reminder fired without matching Inbox Item")
	}
	ownerID := models.BuiltinOwnerActorID
	reason := "计划取消"
	if err := store.DB.Model(&models.Reminder{}).Where("id = ?", reminderID).Updates(map[string]any{
		"status": "cancelled", "cancelled_by_actor_id": ownerID,
		"cancelled_at": now, "cancel_reason": reason, "version": 2, "updated_at": now,
	}).Error; err != nil {
		t.Fatalf("cancel Reminder: %v", err)
	}
	if err := store.DB.Model(&models.Reminder{}).Where("id = ?", reminderID).
		Update("title", "篡改终态提醒").Error; err == nil || !strings.Contains(err.Error(), "REMINDER_TERMINAL_IMMUTABLE") {
		t.Fatalf("terminal mutation error = %v", err)
	}
	if err := store.DB.Delete(&models.Reminder{}, "id = ?", reminderID).Error; err == nil || !strings.Contains(err.Error(), "REMINDER_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("hard delete error = %v", err)
	}
	var violations int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations).Error; err != nil || violations != 0 {
		t.Fatalf("foreign_key_check count=%d err=%v", violations, err)
	}
}
