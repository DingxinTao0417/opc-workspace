package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInboxTaskMigrationUpgradesV12WithoutChangingExistingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v12-inbox-task-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() current database error = %v", err)
	}
	const (
		inboxID = "018f0000-0000-7000-8000-000000001301"
		taskID  = "018f0000-0000-7000-8000-000000001302"
	)
	inbox := models.InboxItem{
		ID: inboxID, Kind: "manual", Title: "迁移前收件箱项", Summary: "保持原始事实",
		SourceEntityType: "manual", Priority: "P1", Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: `{}`, Version: 4,
		CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T09:00:00Z",
	}
	if err := store.DB.Create(&inbox).Error; err != nil {
		t.Fatalf("seed v12 Inbox fact: %v", err)
	}
	task := models.Task{
		ID: taskID, Title: "迁移前任务", Description: "保持任务事实", Kind: "work",
		Status: "todo", ReviewPolicy: "none", Priority: "P2", ActualMinutes: 17, Version: 3,
		CreatedAt: "2026-08-28T08:10:00Z", UpdatedAt: "2026-08-28T09:10:00Z",
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed v12 Task fact: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE reminders").Error; err != nil {
		t.Fatalf("remove v14 Reminder table: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 14").Error; err != nil {
		t.Fatalf("rewind fixture migration 14 history: %v", err)
	}
	if err := store.DB.Exec("DROP TRIGGER trg_tasks_prevent_active_inbox_relation_delete").Error; err != nil {
		t.Fatalf("remove v13 Task delete trigger: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE inbox_item_tasks").Error; err != nil {
		t.Fatalf("remove v13 relation table: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 13").Error; err != nil {
		t.Fatalf("rewind fixture migration history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close v12 fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v12 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 18 {
		t.Fatalf("SchemaVersion = %d, want 18", store.SchemaVersion)
	}
	var preservedInbox models.InboxItem
	if err := store.DB.First(&preservedInbox, "id = ?", inboxID).Error; err != nil {
		t.Fatalf("load preserved Inbox Item: %v", err)
	}
	if preservedInbox.Title != inbox.Title || preservedInbox.Summary != inbox.Summary ||
		preservedInbox.Priority != inbox.Priority || preservedInbox.Status != inbox.Status ||
		preservedInbox.Version != inbox.Version || preservedInbox.CreatedAt != inbox.CreatedAt ||
		preservedInbox.UpdatedAt != inbox.UpdatedAt {
		t.Fatalf("preserved Inbox Item = %#v", preservedInbox)
	}
	var preservedTask models.Task
	if err := store.DB.First(&preservedTask, "id = ?", taskID).Error; err != nil {
		t.Fatalf("load preserved Task: %v", err)
	}
	if preservedTask.Title != task.Title || preservedTask.Description != task.Description ||
		preservedTask.Status != task.Status || preservedTask.ActualMinutes != task.ActualMinutes ||
		preservedTask.Version != task.Version || preservedTask.CreatedAt != task.CreatedAt ||
		preservedTask.UpdatedAt != task.UpdatedAt {
		t.Fatalf("preserved Task = %#v", preservedTask)
	}
	var relationCount int64
	if err := store.DB.Model(&models.InboxItemTask{}).Count(&relationCount).Error; err != nil || relationCount != 0 {
		t.Fatalf("new relation table count=%d err=%v", relationCount, err)
	}
}

func TestInboxTaskMigrationConstrainsHistoryAndTaskDeletion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "inbox-task-relations.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	const (
		inboxID    = "018f0000-0000-7000-8000-000000001311"
		taskID     = "018f0000-0000-7000-8000-000000001312"
		otherID    = "018f0000-0000-7000-8000-000000001313"
		relationID = "018f0000-0000-7000-8000-000000001314"
	)
	now := "2026-08-28T10:00:00Z"
	inbox := models.InboxItem{
		ID: inboxID, Kind: "manual", Title: "关系约束测试", Summary: "",
		SourceEntityType: "manual", Priority: "P2", Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: `{}`, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&inbox).Error; err != nil {
		t.Fatalf("create Inbox Item: %v", err)
	}
	for _, task := range []models.Task{
		{ID: taskID, Title: "主关联任务", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: otherID, Title: "另一个任务", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.DB.Create(&task).Error; err != nil {
			t.Fatalf("create Task %s: %v", task.ID, err)
		}
	}
	relation := models.InboxItemTask{
		ID: relationID, InboxItemID: inboxID, TaskRefID: taskID, TaskID: stringPointer(taskID),
		TaskTitleSnapshot: "主关联任务", RelationType: "linked", IsRequired: true, Position: 1,
		LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: now,
	}
	if err := store.DB.Create(&relation).Error; err != nil {
		t.Fatalf("create valid relation: %v", err)
	}

	duplicate := relation
	duplicate.ID = "018f0000-0000-7000-8000-000000001315"
	duplicate.Position = 2
	if err := store.DB.Create(&duplicate).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate active pair error = %v", err)
	}
	positionConflict := relation
	positionConflict.ID = "018f0000-0000-7000-8000-000000001316"
	positionConflict.TaskRefID = otherID
	positionConflict.TaskID = stringPointer(otherID)
	positionConflict.TaskTitleSnapshot = "另一个任务"
	if err := store.DB.Create(&positionConflict).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate active position error = %v", err)
	}
	if err := store.DB.Model(&models.InboxItemTask{}).Where("id = ?", relationID).
		Update("task_title_snapshot", "篡改标题").Error; err == nil || !strings.Contains(err.Error(), "INBOX_TASK_RELATION_IDENTITY_IMMUTABLE") {
		t.Fatalf("immutable snapshot error = %v", err)
	}
	if err := store.DB.Model(&models.InboxItemTask{}).Where("id = ?", relationID).
		Update("unlinked_at", "2026-08-28T11:00:00Z").Error; err == nil {
		t.Fatal("partial unlink facts were accepted")
	}
	if err := store.DB.Delete(&models.Task{}, "id = ?", taskID).Error; err == nil || !strings.Contains(err.Error(), "TASK_HAS_ACTIVE_INBOX_RELATIONS") {
		t.Fatalf("active relation Task delete error = %v", err)
	}

	unlinkedAt := "2026-08-28T11:00:00Z"
	reason := "不再需要此任务"
	if err := store.DB.Model(&models.InboxItemTask{}).Where("id = ?", relationID).Updates(map[string]any{
		"unlinked_by_actor_id": models.BuiltinOwnerActorID,
		"unlinked_at":          unlinkedAt,
		"unlink_reason":        reason,
	}).Error; err != nil {
		t.Fatalf("soft unlink relation: %v", err)
	}
	if err := store.DB.Model(&models.InboxItemTask{}).Where("id = ?", relationID).
		Update("is_required", false).Error; err == nil || !strings.Contains(err.Error(), "INBOX_TASK_RELATION_HISTORY_IMMUTABLE") {
		t.Fatalf("historical requirement mutation error = %v", err)
	}
	if err := store.DB.Delete(&models.Task{}, "id = ?", taskID).Error; err != nil {
		t.Fatalf("delete Task after unlink: %v", err)
	}
	var historical models.InboxItemTask
	if err := store.DB.First(&historical, "id = ?", relationID).Error; err != nil {
		t.Fatalf("load historical relation: %v", err)
	}
	if historical.TaskID != nil || historical.TaskRefID != taskID || historical.TaskTitleSnapshot != "主关联任务" ||
		historical.UnlinkedAt == nil || *historical.UnlinkedAt != unlinkedAt ||
		historical.UnlinkReason == nil || *historical.UnlinkReason != reason {
		t.Fatalf("historical relation after Task delete = %#v", historical)
	}
	if err := store.DB.Delete(&historical).Error; err == nil || !strings.Contains(err.Error(), "INBOX_TASK_RELATION_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("relation hard delete error = %v", err)
	}
	if err := store.DB.Delete(&models.InboxItem{}, "id = ?", inboxID).Error; err != nil {
		t.Fatalf("delete parent Inbox Item with historical relation: %v", err)
	}
	var remainingRelations int64
	if err := store.DB.Model(&models.InboxItemTask{}).Where("inbox_item_id = ?", inboxID).
		Count(&remainingRelations).Error; err != nil || remainingRelations != 0 {
		t.Fatalf("Inbox delete cascade relation count=%d err=%v", remainingRelations, err)
	}
	var violations int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations).Error; err != nil || violations != 0 {
		t.Fatalf("foreign_key_check count=%d err=%v", violations, err)
	}
}
