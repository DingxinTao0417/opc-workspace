package database

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStorageSettingsMigrationGatesAndPreservesExistingSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v28-storage-settings.db")
	v28 := openDatabaseAtVersion(t, path, 28)
	if _, err := v28.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES ('appearance', '{"theme":"light"}', 1, 4,
			'00000000-0000-5000-8000-000000000001', '2026-08-28T08:00:00Z')
	`); err != nil {
		t.Fatalf("insert v28 appearance setting: %v", err)
	}
	if err := v28.Close(); err != nil {
		t.Fatalf("close v28 database: %v", err)
	}

	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatalf("open migration gate: %v", err)
	}
	if gated.SchemaVersion != 28 || gate == nil || gate.CurrentVersion != 28 || gate.TargetVersion != 50 || !reflect.DeepEqual(gate.PendingVersions, []int{29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50}) {
		t.Fatalf("storage settings migration gate: store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	if err := gated.Close(); err != nil {
		t.Fatalf("close gated database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("apply storage settings migration: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 50 {
		t.Fatalf("SchemaVersion = %d, want 50", store.SchemaVersion)
	}
	var value string
	var version int
	if err := store.SQL.QueryRow("SELECT value_json, version FROM app_settings WHERE key = 'appearance'").Scan(&value, &version); err != nil {
		t.Fatalf("read preserved setting: %v", err)
	}
	if value != `{"theme":"light"}` || version != 4 {
		t.Fatalf("preserved setting value=%s version=%d", value, version)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES ('storage', '{"low_space_threshold_gib":5}', 2, 1,
			'00000000-0000-5000-8000-000000000001', CURRENT_TIMESTAMP)
	`); err != nil {
		t.Fatalf("insert storage setting: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM app_settings WHERE key = 'storage'"); err == nil || !strings.Contains(err.Error(), "APP_SETTING_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("storage setting hard delete error = %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES ('unknown', '{}', 2, 1,
			'00000000-0000-5000-8000-000000000001', CURRENT_TIMESTAMP)
	`); err == nil {
		t.Fatal("unknown setting key was accepted")
	}
}
