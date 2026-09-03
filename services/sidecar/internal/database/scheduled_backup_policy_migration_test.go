package database

import (
	"path/filepath"
	"testing"
)

func TestScheduledBackupPolicyMigrationCreatesDisabledConstrainedSingleton(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "scheduled-backup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SchemaVersion != 56 {
		t.Fatalf("schema version=%d, want 56", store.SchemaVersion)
	}
	var policy struct {
		Enabled        bool
		LocalTime      string
		Timezone       string
		RetentionCount int
		LastStatus     string
		Version        int64
	}
	if err := store.DB.Table("scheduled_backup_policy").Where("singleton = 1").Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.LocalTime != "02:00" || policy.Timezone != "UTC" || policy.RetentionCount != 30 || policy.LastStatus != "idle" || policy.Version != 1 {
		t.Fatalf("default scheduled backup policy=%#v", policy)
	}
	invalid := []string{
		"UPDATE scheduled_backup_policy SET local_time = '25:00' WHERE singleton = 1",
		"UPDATE scheduled_backup_policy SET retention_count = 0 WHERE singleton = 1",
		"UPDATE scheduled_backup_policy SET last_status = 'running' WHERE singleton = 1",
		"INSERT INTO scheduled_backup_policy(singleton, enabled, local_time, timezone, retention_count, last_status, version, updated_at) VALUES (2, 0, '02:00', 'UTC', 30, 'idle', 1, 'x')",
	}
	for _, statement := range invalid {
		if err := store.DB.Exec(statement).Error; err == nil {
			t.Fatalf("invalid policy unexpectedly accepted: %s", statement)
		}
	}
}
