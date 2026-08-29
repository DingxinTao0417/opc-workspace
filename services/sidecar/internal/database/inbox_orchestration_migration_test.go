package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInboxOrchestrationMigrationPreservesV14Facts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v14-orchestration-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() current database error = %v", err)
	}
	item := models.InboxItem{
		ID: "018f0000-0000-7000-8000-000000001501", Kind: "manual", Title: "迁移保留事项",
		SourceEntityType: "manual", Priority: "P1", Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: `{}`, Version: 3, CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T09:00:00Z",
	}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatalf("seed Inbox Item: %v", err)
	}
	for _, trigger := range []string{
		"trg_inbox_items_validate_automatic_resolution_insert",
		"trg_inbox_items_validate_automatic_resolution_update",
	} {
		if err := store.DB.Exec("DROP TRIGGER " + trigger).Error; err != nil {
			t.Fatalf("drop v15 trigger %s: %v", trigger, err)
		}
	}
	for _, index := range []string{
		"idx_inbox_item_tasks_required_task_active",
		"idx_inbox_item_tasks_required_inbox_active",
	} {
		if err := store.DB.Exec("DROP INDEX " + index).Error; err != nil {
			t.Fatalf("drop v15 index %s: %v", index, err)
		}
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 15").Error; err != nil {
		t.Fatalf("rewind v15 history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v14 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 33 {
		t.Fatalf("SchemaVersion = %d, want 33", store.SchemaVersion)
	}
	var preserved models.InboxItem
	if err := store.DB.First(&preserved, "id = ?", item.ID).Error; err != nil {
		t.Fatalf("load preserved Inbox Item: %v", err)
	}
	if preserved.Title != item.Title || preserved.Status != item.Status || preserved.Version != item.Version || preserved.UpdatedAt != item.UpdatedAt {
		t.Fatalf("preserved Inbox Item = %#v", preserved)
	}
}

func TestInboxOrchestrationMigrationGuardsAutomaticResolution(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "orchestration-guards.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	now := "2026-08-28T10:00:00Z"
	item := models.InboxItem{
		ID: "018f0000-0000-7000-8000-000000001511", Kind: "manual", Title: "自动解决约束",
		SourceEntityType: "manual", Priority: "P2", Status: "tracking", ResolutionPolicy: "all_required_tasks_done",
		PayloadJSON: `{}`, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatalf("create Inbox Item: %v", err)
	}
	invalid := map[string]any{
		"status": "resolved", "triaged_at": now, "resolved_by_actor_id": models.BuiltinSystemActorID,
		"resolved_at": now, "resolution_reason": "必需任务已完成", "resolution_mode": "automatic",
	}
	if err := store.DB.Model(&models.InboxItem{}).Where("id = ?", item.ID).Updates(invalid).Error; err == nil || !strings.Contains(err.Error(), "INBOX_AUTOMATIC_RESOLUTION_INVALID") {
		t.Fatalf("zero-required automatic resolution error = %v", err)
	}

	task := models.Task{
		ID: "018f0000-0000-7000-8000-000000001512", Title: "必需任务", Kind: "work", Status: "todo",
		ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("create Task: %v", err)
	}
	relation := models.InboxItemTask{
		ID: "018f0000-0000-7000-8000-000000001513", InboxItemID: item.ID, TaskRefID: task.ID, TaskID: stringPointer(task.ID),
		TaskTitleSnapshot: task.Title, RelationType: "created", IsRequired: true, Position: 1,
		LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: now,
	}
	if err := store.DB.Create(&relation).Error; err != nil {
		t.Fatalf("create relation: %v", err)
	}
	if err := store.DB.Model(&models.InboxItem{}).Where("id = ?", item.ID).Updates(invalid).Error; err == nil || !strings.Contains(err.Error(), "INBOX_AUTOMATIC_RESOLUTION_INVALID") {
		t.Fatalf("incomplete-required automatic resolution error = %v", err)
	}
	if err := store.DB.Model(&models.Task{}).Where("id = ?", task.ID).
		Updates(map[string]any{"status": "done", "completed_at": now}).Error; err != nil {
		t.Fatalf("complete Task: %v", err)
	}
	if err := store.DB.Model(&models.InboxItem{}).Where("id = ?", item.ID).Updates(invalid).Error; err != nil {
		t.Fatalf("valid automatic resolution: %v", err)
	}
	if err := store.DB.Model(&models.InboxItem{}).Where("id = ?", item.ID).Update("resolved_by_actor_id", models.BuiltinOwnerActorID).Error; err == nil || !strings.Contains(err.Error(), "INBOX_AUTOMATIC_RESOLUTION_INVALID") {
		t.Fatalf("owner automatic resolution error = %v", err)
	}
}
