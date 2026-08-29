package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceAvatarMigrationPreservesSettingsAndStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-avatar.db")
	v26 := openDatabaseAtVersion(t, path, 26)
	if _, err := v26.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES ('workspace', '{"display_name":"Existing Workspace","avatar_ref":null}', 1, 4,
			'00000000-0000-5000-8000-000000000001', '2026-08-28T08:00:00Z')
	`); err != nil {
		t.Fatalf("insert v26 workspace setting: %v", err)
	}
	if err := v26.Close(); err != nil {
		t.Fatalf("close v26 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 39 {
		t.Fatalf("SchemaVersion = %d, want 39", store.SchemaVersion)
	}
	var value string
	var version int
	if err := store.SQL.QueryRow("SELECT value_json, version FROM app_settings WHERE key = 'workspace'").Scan(&value, &version); err != nil {
		t.Fatalf("read workspace setting: %v", err)
	}
	if value != `{"display_name":"Existing Workspace","avatar_ref":null}` || version != 4 {
		t.Fatalf("workspace setting value=%s version=%d", value, version)
	}
	for _, table := range []string{"workspace_avatars", "workspace_avatar_deletion_tombstones"} {
		var count int
		if err := store.SQL.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
}

func TestWorkspaceAvatarMigrationGuardsControlledLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "workspace-avatar-guards.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	const (
		avatarID = "018f0000-0000-7000-8000-000000002701"
		path     = "avatars/018f0000-0000-7000-8000-000000002701.png"
		hash     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	if _, err := store.SQL.Exec(`
		INSERT INTO workspace_avatars(
			id, relative_path, extension, mime_type, size_bytes, sha256,
			integrity_status, integrity_checked_at, created_at
		) VALUES (?, ?, 'png', 'image/png', 4, ?, 'verified', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, avatarID, path, hash); err != nil {
		t.Fatalf("insert workspace avatar: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO app_settings(key, value_json, schema_version, version, updated_by_actor_id, updated_at)
		VALUES ('workspace', ?, 1, 1, '00000000-0000-5000-8000-000000000001', CURRENT_TIMESTAMP)
	`, `{"display_name":"Workspace","avatar_ref":"`+path+`"}`); err != nil {
		t.Fatalf("reference active avatar: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE workspace_avatars SET deleted_at = CURRENT_TIMESTAMP, deletion_reason = 'replace' WHERE id = ?", avatarID); err == nil || !strings.Contains(err.Error(), "WORKSPACE_AVATAR_TOMBSTONE_REQUIRED") {
		t.Fatalf("delete without tombstone error = %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO workspace_avatar_deletion_tombstones(avatar_id, relative_path, size_bytes, sha256, reason)
		VALUES (?, ?, 4, ?, 'replace')
	`, avatarID, path, hash); err != nil {
		t.Fatalf("insert avatar tombstone: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE app_settings SET value_json = ? WHERE key = 'workspace'", `{"display_name":"Workspace","avatar_ref":null}`); err != nil {
		t.Fatalf("clear avatar reference: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE workspace_avatars SET deleted_at = CURRENT_TIMESTAMP, deletion_reason = 'replace' WHERE id = ?", avatarID); err != nil {
		t.Fatalf("retire avatar with tombstone: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM workspace_avatar_deletion_tombstones WHERE avatar_id = ?", avatarID); err == nil || !strings.Contains(err.Error(), "WORKSPACE_AVATAR_TOMBSTONE_IMMUTABLE") {
		t.Fatalf("delete tombstone error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE app_settings SET value_json = ? WHERE key = 'workspace'", `{"display_name":"Workspace","avatar_ref":"avatars/018f0000-0000-7000-8000-000000002702.png"}`); err == nil || !strings.Contains(err.Error(), "WORKSPACE_AVATAR_NOT_ACTIVE") {
		t.Fatalf("reference unknown avatar error = %v", err)
	}
}
