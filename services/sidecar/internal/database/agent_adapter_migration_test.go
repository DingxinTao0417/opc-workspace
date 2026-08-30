package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentAdapterMigrationUpgradesV33WithoutCreatingAdapters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v33-agent-adapters.db")
	v33 := openDatabaseAtVersion(t, path, 33)
	const ruleID = "018f0000-0000-7000-8000-000000003401"
	if _, err := v33.Exec(`
		INSERT INTO automation_rules(
			id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at
		) VALUES (?, 'project-completed-inbox', 0, '{"priority":"P1"}', NULL, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, ruleID); err != nil {
		t.Fatalf("seed v33 automation rule: %v", err)
	}
	if err := v33.Close(); err != nil {
		t.Fatalf("close v33 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v33 agent adapter database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 46 {
		t.Fatalf("SchemaVersion = %d, want 46", store.SchemaVersion)
	}
	var adapterCount, ruleCount int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM agent_adapters").Row().Scan(&adapterCount); err != nil {
		t.Fatalf("count agent adapters: %v", err)
	}
	if err := store.DB.Raw("SELECT COUNT(*) FROM automation_rules WHERE id = ?", ruleID).Row().Scan(&ruleCount); err != nil {
		t.Fatalf("count preserved automation rule: %v", err)
	}
	if adapterCount != 0 || ruleCount != 1 {
		t.Fatalf("adapter_count=%d rule_count=%d, want 0/1", adapterCount, ruleCount)
	}
}

func TestAgentAdapterMigrationConstrainsIdentityAndExecutionReadiness(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-adapter-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()

	const adapterID = "018f0000-0000-7000-8000-000000003411"
	insert := `
		INSERT INTO agent_adapters(
			id, adapter_key, kind, display_name, executable_ref, manifest_json,
			protocol_version, status, health_status, isolation_status,
			execution_ready, version, created_at, updated_at
		) VALUES (?, ?, 'builtin', '本地文本诊断执行器', ?, ?, 'opc-agent-pipe-v1',
			'disabled', 'unknown', 'unverified', 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	manifest := `{"schema_version":1,"protocol_version":"opc-agent-pipe-v1","capabilities":["read_task_snapshot"]}`
	if err := store.DB.Exec(insert, adapterID, "builtin-local-text-v1", "builtin:local-text-v1", manifest).Error; err != nil {
		t.Fatalf("insert valid adapter: %v", err)
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("agent_adapters").Where("id = ?", adapterID).Update("adapter_key", "changed").Error; err == nil || !strings.Contains(err.Error(), "AGENT_ADAPTER_IDENTITY_IMMUTABLE") {
		t.Fatalf("identity update error = %v", err)
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("agent_adapters").Where("id = ?", adapterID).Updates(map[string]any{
		"status": "enabled", "version": 2,
	}).Error; err == nil {
		t.Fatal("enabled adapter without verified readiness was accepted")
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("agent_adapters").Where("id = ?", adapterID).Updates(map[string]any{
		"health_status": "blocked", "health_error_code": "PLATFORM_ISOLATION_UNVERIFIED", "last_health_at": "2026-08-29T00:00:00Z",
	}).Error; err != nil {
		t.Fatalf("record blocked diagnostic health: %v", err)
	}
	if err := store.DB.Model(new(struct{ ID string })).Table("agent_adapters").Where("id = ?", adapterID).Update("version", 2).Error; err == nil || !strings.Contains(err.Error(), "AGENT_ADAPTER_RUNTIME_VERSION_INVALID") {
		t.Fatalf("runtime version update error = %v", err)
	}
	if err := store.DB.Exec("DELETE FROM agent_adapters WHERE id = ?", adapterID).Error; err == nil || !strings.Contains(err.Error(), "AGENT_ADAPTER_HARD_DELETE_FORBIDDEN") {
		t.Fatalf("hard delete error = %v", err)
	}
}
