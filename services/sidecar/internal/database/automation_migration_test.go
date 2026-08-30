package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationMigrationUpgradesV32AndCreatesNoBusinessRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v32-automation.db")
	v32 := openDatabaseAtVersion(t, path, 32)
	if err := v32.Close(); err != nil {
		t.Fatalf("close v32 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v32 automation database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 49 {
		t.Fatalf("SchemaVersion = %d, want 49", store.SchemaVersion)
	}
	for _, table := range []string{"automation_rules", "automation_runs"} {
		var count int64
		if err := store.DB.Raw("SELECT COUNT(*) FROM " + table).Row().Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want no migration seed", table, count)
		}
	}
}

func TestAutomationMigrationConstrainsPresetIdentityAndRunDedupe(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "automation-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	const ruleID = "018f0000-0000-7000-8000-000000003301"
	insertRule := `
		INSERT INTO automation_rules(
			id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at
		) VALUES (?, ?, 0, ?, NULL, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	if err := store.DB.Exec(insertRule, ruleID, "project-completed-inbox", `{"priority":"P1"}`).Error; err != nil {
		t.Fatalf("insert valid automation rule: %v", err)
	}
	if err := store.DB.Exec(insertRule, "018f0000-0000-7000-8000-000000003302", "project-completed-inbox", `{}`).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate preset error = %v", err)
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("automation_rules").Where("id = ?", ruleID).Update("preset_key", "other").Error; err == nil || !strings.Contains(err.Error(), "AUTOMATION_RULE_IDENTITY_IMMUTABLE") {
		t.Fatalf("preset identity update error = %v", err)
	}

	insertRun := `
		INSERT INTO automation_runs(
			id, rule_id, rule_version, trigger_type, source_event_id, scheduled_for,
			logical_key, dedupe_key, status, attempt, retryable, causal_depth,
			config_snapshot_json, action_snapshot_json, result_type, result_id,
			started_at, ended_at
		) VALUES (?, ?, 1, 'event', ?, NULL, ?, ?, 'succeeded', 1, 0, 0, '{}', '{}', 'inbox_item', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	const eventID = "018f0000-0000-7000-8000-000000003303"
	if err := store.DB.Exec(insertRun, "018f0000-0000-7000-8000-000000003304", ruleID, eventID, "event:"+eventID, "event:"+eventID+":attempt:1", "018f0000-0000-7000-8000-000000003307").Error; err != nil {
		t.Fatalf("insert valid automation run: %v", err)
	}
	if err := store.DB.Exec(insertRun, "018f0000-0000-7000-8000-000000003305", ruleID, eventID, "event:"+eventID, "event:"+eventID+":attempt:2", "018f0000-0000-7000-8000-000000003308").Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate event run error = %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO automation_runs(
			id, rule_id, rule_version, trigger_type, logical_key, dedupe_key,
			status, attempt, retryable, causal_depth, config_snapshot_json,
			action_snapshot_json, started_at, ended_at
		) VALUES (?, ?, 1, 'schedule', 'invalid', 'invalid:attempt:1', 'succeeded', 1, 0, 0, '{}', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, "018f0000-0000-7000-8000-000000003306", ruleID).Error; err == nil {
		t.Fatal("schedule run without scheduled_for was accepted")
	}
}
