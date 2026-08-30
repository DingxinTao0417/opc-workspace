package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAppSettingsMigrationPreservesV15FactsAndStartsEmpty(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v15-settings-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() current database error = %v", err)
	}
	const taskID = "018f0000-0000-7000-8000-000000001601"
	task := models.Task{
		ID: taskID, Title: "迁移保留任务", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", Version: 4, CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T09:00:00Z",
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed Task: %v", err)
	}
	for _, trigger := range []string{
		"trg_app_settings_require_active_actor_insert",
		"trg_app_settings_require_active_actor_update",
		"trg_app_settings_protect_identity_update",
		"trg_app_settings_protect_hard_delete",
	} {
		if err := store.DB.Exec("DROP TRIGGER " + trigger).Error; err != nil {
			t.Fatalf("drop v16 trigger %s: %v", trigger, err)
		}
	}
	if err := store.DB.Exec("DROP INDEX idx_app_settings_updated").Error; err != nil {
		t.Fatalf("drop v16 index: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE app_settings").Error; err != nil {
		t.Fatalf("drop v16 table: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 16").Error; err != nil {
		t.Fatalf("rewind v16 history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v15 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 42 {
		t.Fatalf("SchemaVersion = %d, want 42", store.SchemaVersion)
	}
	var preserved models.Task
	if err := store.DB.First(&preserved, "id = ?", taskID).Error; err != nil {
		t.Fatalf("load preserved Task: %v", err)
	}
	if preserved.Title != task.Title || preserved.Status != task.Status || preserved.Version != task.Version {
		t.Fatalf("preserved Task = %#v", preserved)
	}
	var settingCount int64
	if err := store.DB.Table("app_settings").Count(&settingCount).Error; err != nil {
		t.Fatalf("count app_settings: %v", err)
	}
	if settingCount != 0 {
		t.Fatalf("app_settings count = %d, want 0", settingCount)
	}
}

func TestAppSettingsMigrationEnforcesOwnershipAndImmutability(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "settings-guards.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	setting := models.AppSetting{
		Key: "appearance", ValueJSON: `{"theme":"dark"}`, SchemaVersion: 2, Version: 1,
		UpdatedByActorID: models.BuiltinOwnerActorID, UpdatedAt: "2026-08-28T10:00:00Z",
	}
	if err := store.DB.Create(&setting).Error; err != nil {
		t.Fatalf("create valid app setting: %v", err)
	}
	invalidKey := setting
	invalidKey.Key = "token"
	if err := store.DB.Create(&invalidKey).Error; err == nil {
		t.Fatal("unsupported app setting key was accepted")
	}
	inactiveActor := models.Actor{
		ID: "018f0000-0000-7000-8000-000000001602", Type: "person", DisplayName: "停用人员",
		Status: "inactive", MetadataJSON: `{}`, Version: 1, CreatedAt: setting.UpdatedAt, UpdatedAt: setting.UpdatedAt,
	}
	if err := store.DB.Create(&inactiveActor).Error; err != nil {
		t.Fatalf("create inactive Actor: %v", err)
	}
	inactiveSetting := setting
	inactiveSetting.Key = "general"
	inactiveSetting.UpdatedByActorID = inactiveActor.ID
	if err := store.DB.Create(&inactiveSetting).Error; err == nil || !strings.Contains(err.Error(), "APP_SETTING_ACTOR_NOT_ACTIVE") {
		t.Fatalf("inactive Actor insert error = %v", err)
	}
	if err := store.DB.Model(&models.AppSetting{}).Where("key = ?", setting.Key).Update("key", "focus").Error; err == nil || !strings.Contains(err.Error(), "APP_SETTING_KEY_IMMUTABLE") {
		t.Fatalf("identity update error = %v", err)
	}
	if err := store.DB.Delete(&models.AppSetting{}, "key = ?", setting.Key).Error; err == nil || !strings.Contains(err.Error(), "APP_SETTING_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("hard delete error = %v", err)
	}
}
