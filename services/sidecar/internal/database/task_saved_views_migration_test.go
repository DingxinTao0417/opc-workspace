package database

import (
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestTaskSavedViewsMigrationPreservesV16FactsAndStartsEmpty(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v16-task-views-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open current database: %v", err)
	}
	task := models.Task{
		ID: "018f0000-0000-7000-8000-000000001701", Title: "迁移保留任务", Kind: "work",
		Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 3,
		CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T09:00:00Z",
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed Task: %v", err)
	}
	if err := store.DB.Exec("DROP INDEX idx_task_saved_views_updated").Error; err != nil {
		t.Fatalf("drop v17 updated index: %v", err)
	}
	if err := store.DB.Exec("DROP INDEX idx_task_saved_views_name").Error; err != nil {
		t.Fatalf("drop v17 name index: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE task_saved_views").Error; err != nil {
		t.Fatalf("drop v17 table: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 17").Error; err != nil {
		t.Fatalf("rewind v17 history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v16 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 23 {
		t.Fatalf("SchemaVersion = %d, want 23", store.SchemaVersion)
	}
	var preserved models.Task
	if err := store.DB.First(&preserved, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("load preserved Task: %v", err)
	}
	if preserved.Title != task.Title || preserved.Version != task.Version {
		t.Fatalf("preserved Task = %#v", preserved)
	}
	var count int64
	if err := store.DB.Model(&models.TaskSavedView{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("initial saved view count = %d, err=%v", count, err)
	}
}

func TestTaskSavedViewsMigrationEnforcesDefinitionAndNameConstraints(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "task-view-constraints.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	base := models.TaskSavedView{
		ID: "018f0000-0000-7000-8000-000000001702", Name: "Weekly tasks",
		DefinitionJSON: `{}`, SchemaVersion: 1, Version: 1,
		CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T08:00:00Z",
	}
	if err := store.DB.Create(&base).Error; err != nil {
		t.Fatalf("create valid saved view: %v", err)
	}
	duplicate := base
	duplicate.ID = "018f0000-0000-7000-8000-000000001703"
	duplicate.Name = "weekly TASKS"
	if err := store.DB.Create(&duplicate).Error; err == nil {
		t.Fatal("case-insensitive duplicate name was accepted")
	}
	invalidJSON := base
	invalidJSON.ID = "018f0000-0000-7000-8000-000000001704"
	invalidJSON.Name = "无效 JSON"
	invalidJSON.DefinitionJSON = `{`
	if err := store.DB.Create(&invalidJSON).Error; err == nil {
		t.Fatal("invalid definition JSON was accepted")
	}
	invalidSchema := base
	invalidSchema.ID = "018f0000-0000-7000-8000-000000001705"
	invalidSchema.Name = "未知结构"
	invalidSchema.SchemaVersion = 2
	if err := store.DB.Create(&invalidSchema).Error; err == nil {
		t.Fatal("unknown saved view schema was accepted")
	}
}
