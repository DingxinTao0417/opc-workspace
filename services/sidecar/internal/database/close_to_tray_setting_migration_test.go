package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseToTraySettingMigrationUpgradesStoredSettingsWithoutCreatingDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v41-close-to-tray.db")
	v41 := openDatabaseAtVersion(t, path, 41)
	if _, err := v41.Exec(`
		INSERT INTO app_settings(
			key, value_json, schema_version, version, updated_by_actor_id, updated_at
		) VALUES
			('general', '{"default_route":"projects","show_right_overview":false,"reduce_motion":true}', 1, 7, ?, '2026-08-29T10:00:00Z'),
			('appearance', '{"theme":"light"}', 1, 3, ?, '2026-08-29T11:00:00Z')
	`, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("seed v41 settings: %v", err)
	}
	if err := v41.Close(); err != nil {
		t.Fatalf("close v41 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v41 settings database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 51 {
		t.Fatalf("SchemaVersion = %d, want 51", store.SchemaVersion)
	}

	var valueJSON string
	var schemaVersion, version int
	if err := store.DB.Raw(`
		SELECT value_json, schema_version, version
		FROM app_settings WHERE key = 'general'
	`).Row().Scan(&valueJSON, &schemaVersion, &version); err != nil {
		t.Fatalf("read migrated general setting: %v", err)
	}
	if valueJSON != `{"default_route":"projects","show_right_overview":false,"reduce_motion":true,"close_to_tray":true}` || schemaVersion != 2 || version != 7 {
		t.Fatalf("migrated general = value %s schema %d version %d", valueJSON, schemaVersion, version)
	}

	var count int
	if err := store.DB.Raw(`SELECT COUNT(*) FROM app_settings`).Row().Scan(&count); err != nil {
		t.Fatalf("count migrated settings: %v", err)
	}
	if count != 2 {
		t.Fatalf("app_settings count = %d, want 2", count)
	}
	if err := store.DB.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES('focus', '{}', 1, 1, ?, CURRENT_TIMESTAMP)
	`, builtinOwnerActorID).Error; err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("schema v1 insert error = %v, want constraint rejection", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestCloseToTraySettingMigrationKeepsEmptyWorkspaceEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v41-empty-settings.db")
	v41 := openDatabaseAtVersion(t, path, 41)
	if err := v41.Close(); err != nil {
		t.Fatalf("close empty v41 fixture: %v", err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade empty v41 settings database: %v", err)
	}
	defer store.Close()
	var count int64
	if err := store.DB.Table("app_settings").Count(&count).Error; err != nil {
		t.Fatalf("count empty settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("app_settings count = %d, want 0", count)
	}
}
