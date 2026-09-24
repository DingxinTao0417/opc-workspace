package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRunExecutionIdentityMigration(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-run-identity.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 79 {
		t.Fatalf("SchemaVersion = %d, want 79", store.SchemaVersion)
	}

	var identityColumns int64
	if err := store.DB.Raw(`
		SELECT COUNT(*) FROM pragma_table_info('agent_runs')
		WHERE name IN (
			'task_version', 'assignment_assigned_at', 'actor_version',
			'adapter_version', 'provider_version', 'provider_config_version',
			'execution_contract_version'
		)
	`).Scan(&identityColumns).Error; err != nil {
		t.Fatalf("inspect agent run identity columns: %v", err)
	}
	if identityColumns != 7 {
		t.Fatalf("agent run identity columns = %d, want 7", identityColumns)
	}
	var activeIndexSQL string
	if err := store.DB.Table("sqlite_master").Select("sql").
		Where("type = 'index' AND name = 'ux_agent_runs_task_active'").
		Scan(&activeIndexSQL).Error; err != nil {
		t.Fatalf("inspect active run index: %v", err)
	}
	if !strings.Contains(activeIndexSQL, "WHERE status IN ('queued', 'running')") {
		t.Fatalf("active run index SQL = %q", activeIndexSQL)
	}

	const (
		taskID       = "018f0000-0000-7000-8000-000000007401"
		adapterID    = "018f0000-0000-7000-8000-000000007402"
		actorID      = "018f0000-0000-7000-8000-000000007403"
		assignmentID = "018f0000-0000-7000-8000-000000007404"
		providerID   = "018f0000-0000-7000-8000-000000007405"
		firstRunID   = "018f0000-0000-7000-8000-000000007406"
		secondRunID  = "018f0000-0000-7000-8000-000000007407"
		legacyRunID  = "018f0000-0000-7000-8000-000000007408"
		now          = "2026-09-18T12:00:00Z"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_adapters(
			id,adapter_key,kind,display_name,executable_ref,manifest_json,protocol_version,
			status,health_status,isolation_status,execution_ready,last_health_at,version,created_at,updated_at
		) VALUES (?, 'migration-agent', 'builtin', '迁移执行代理', 'builtin:migration-agent', '{}',
			'opc-agent-pipe-v1', 'enabled', 'healthy', 'verified', 1, ?, 1, ?, ?)`, []any{adapterID, now, now, now}},
		{`INSERT INTO actors(id,type,display_name,status,is_builtin,metadata_json,agent_adapter_id,version,created_at,updated_at)
			VALUES (?, 'agent', '迁移代理', 'active', 0, '{}', ?, 1, ?, ?)`, []any{actorID, adapterID, now, now}},
		{`INSERT INTO tasks(id,title,status,review_policy,created_at,updated_at)
			VALUES (?, '迁移执行任务', 'todo', 'manual', ?, ?)`, []any{taskID, now, now}},
		{`INSERT INTO task_assignments(id,task_id,actor_id,role,assigned_by_actor_id,assigned_at)
			VALUES (?, ?, ?, 'assignee', '00000000-0000-5000-8000-000000000001', ?)`, []any{assignmentID, taskID, actorID, now}},
	} {
		if err := store.DB.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed Agent Run identity dependency: %v", err)
		}
	}

	insertRun := `INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
		provider_id,model,input_snapshot_json,created_at,task_version,assignment_assigned_at,
		actor_version,adapter_version,provider_version,provider_config_version,execution_contract_version
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', ?, 'queued',
		?, 'model', '{}', ?, 1, ?, 1, 1, 1, 1, 1)`
	if err := store.DB.Exec(insertRun, firstRunID, taskID, assignmentID, actorID, adapterID, 1, providerID, now, now).Error; err != nil {
		t.Fatalf("insert first active run: %v", err)
	}
	if err := store.DB.Exec(insertRun, secondRunID, taskID, assignmentID, actorID, adapterID, 2, providerID, now, now).Error; err == nil ||
		!strings.Contains(err.Error(), "agent_runs.task_id") {
		t.Fatalf("second active run error = %v, want task partial-unique failure", err)
	}

	if err := store.DB.Exec(`UPDATE agent_runs SET status='failed', error_code='TEST', started_at=?, completed_at=? WHERE id=?`, now, now, firstRunID).Error; err != nil {
		t.Fatalf("finish first run: %v", err)
	}
	if err := store.DB.Exec(`INSERT INTO agent_runs(
		id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
		provider_id,model,input_snapshot_json,error_code,started_at,completed_at,created_at
	) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', 2, 'failed',
		?, 'model', '{}', 'LEGACY', ?, ?, ?)`, legacyRunID, taskID, assignmentID, actorID, adapterID, providerID, now, now, now).Error; err != nil {
		t.Fatalf("insert legacy-compatible run: %v", err)
	}
	var legacy struct {
		TaskVersion              int64
		AssignmentAssignedAt     string
		ActorVersion             int64
		AdapterVersion           int64
		ProviderVersion          int64
		ProviderConfigVersion    int64
		ExecutionContractVersion int64
	}
	if err := store.DB.Table("agent_runs").Where("id = ?", legacyRunID).Take(&legacy).Error; err != nil {
		t.Fatalf("load legacy-compatible run: %v", err)
	}
	if legacy.TaskVersion != 0 || legacy.AssignmentAssignedAt != "" || legacy.ActorVersion != 0 ||
		legacy.AdapterVersion != 0 || legacy.ProviderVersion != 0 || legacy.ProviderConfigVersion != 0 ||
		legacy.ExecutionContractVersion != 0 {
		t.Fatalf("legacy identity defaults = %#v, want fail-closed zero identity", legacy)
	}

	duplicateAttempt := strings.Replace(insertRun, "'queued'", "'failed'", 1)
	duplicateAttempt = strings.Replace(duplicateAttempt, "provider_id,model,input_snapshot_json,created_at", "provider_id,model,input_snapshot_json,error_code,started_at,completed_at,created_at", 1)
	duplicateAttempt = strings.Replace(duplicateAttempt, "?, 'model', '{}', ?, 1", "?, 'model', '{}', 'DUPLICATE', ?, ?, ?, 1", 1)
	if err := store.DB.Exec(duplicateAttempt, secondRunID, taskID, assignmentID, actorID, adapterID, 1, providerID, now, now, now, now).Error; err == nil ||
		!strings.Contains(err.Error(), "agent_runs.task_id, agent_runs.actor_id, agent_runs.attempt") {
		t.Fatalf("duplicate attempt error = %v, want attempt unique failure", err)
	}
}

func TestAgentRunExecutionIdentityUpgradeReconcilesLegacyConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-run-legacy-concurrency.db")
	old := openDatabaseAtVersion(t, path, 73)
	const (
		taskID       = "018f0000-0000-7000-8000-000000007411"
		adapterID    = "018f0000-0000-7000-8000-000000007412"
		actorID      = "018f0000-0000-7000-8000-000000007413"
		assignmentID = "018f0000-0000-7000-8000-000000007414"
		providerID   = "018f0000-0000-7000-8000-000000007415"
		now          = "2026-09-18T12:00:00Z"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_adapters(
			id,adapter_key,kind,display_name,executable_ref,manifest_json,protocol_version,
			status,health_status,isolation_status,execution_ready,last_health_at,version,created_at,updated_at
		) VALUES (?, 'legacy-concurrency', 'builtin', '旧并发代理', 'builtin:legacy-concurrency', '{}',
			'opc-agent-pipe-v1', 'enabled', 'healthy', 'verified', 1, ?, 1, ?, ?)`, []any{adapterID, now, now, now}},
		{`INSERT INTO actors(id,type,display_name,status,is_builtin,metadata_json,agent_adapter_id,version,created_at,updated_at)
			VALUES (?, 'agent', '旧执行代理', 'active', 0, '{}', ?, 1, ?, ?)`, []any{actorID, adapterID, now, now}},
		{`INSERT INTO tasks(id,title,status,review_policy,created_at,updated_at)
			VALUES (?, '旧并发执行任务', 'todo', 'manual', ?, ?)`, []any{taskID, now, now}},
		{`INSERT INTO task_assignments(id,task_id,actor_id,role,assigned_by_actor_id,assigned_at)
			VALUES (?, ?, ?, 'assignee', '00000000-0000-5000-8000-000000000001', ?)`, []any{assignmentID, taskID, actorID, now}},
	} {
		if _, err := old.Exec(statement.query, statement.args...); err != nil {
			_ = old.Close()
			t.Fatalf("seed legacy dependency: %v", err)
		}
	}
	insertLegacyRun := func(id string, attempt int, status, createdAt string) {
		t.Helper()
		startedAt, completedAt, errorCode := any(nil), any(nil), any(nil)
		if status == "running" {
			startedAt = createdAt
		}
		if status == "failed" {
			startedAt, completedAt, errorCode = createdAt, createdAt, "LEGACY_FAILED"
		}
		_, err := old.Exec(`INSERT INTO agent_runs(
			id,task_id,assignment_id,actor_id,adapter_id,created_by_actor_id,attempt,status,
			provider_id,model,input_snapshot_json,error_code,started_at,completed_at,created_at
		) VALUES (?, ?, ?, ?, ?, '00000000-0000-5000-8000-000000000001', ?, ?,
			?, 'legacy-model', '{}', ?, ?, ?, ?)`,
			id, taskID, assignmentID, actorID, adapterID, attempt, status,
			providerID, errorCode, startedAt, completedAt, createdAt)
		if err != nil {
			_ = old.Close()
			t.Fatalf("insert legacy run %s: %v", id, err)
		}
	}
	insertLegacyRun("018f0000-0000-7000-8000-000000007416", 1, "queued", "2026-09-18T12:00:00Z")
	insertLegacyRun("018f0000-0000-7000-8000-000000007417", 1, "running", "2026-09-18T12:01:00Z")
	insertLegacyRun("018f0000-0000-7000-8000-000000007418", 2, "failed", "2026-09-18T12:02:00Z")
	insertLegacyRun("018f0000-0000-7000-8000-000000007419", 2, "failed", "2026-09-18T12:03:00Z")
	if err := old.Close(); err != nil {
		t.Fatalf("close v73 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade legacy Agent Runs: %v", err)
	}
	defer store.Close()
	var active int64
	if err := store.DB.Table("agent_runs").Where("task_id = ? AND status IN ?", taskID, []string{"queued", "running"}).Count(&active).Error; err != nil {
		t.Fatalf("count reconciled active runs: %v", err)
	}
	if active != 1 {
		t.Fatalf("active runs after upgrade = %d, want 1", active)
	}
	var distinctAttempts, total int64
	if err := store.DB.Table("agent_runs").Where("task_id = ?", taskID).
		Select("COUNT(DISTINCT attempt)").Scan(&distinctAttempts).Error; err != nil {
		t.Fatalf("count distinct attempts: %v", err)
	}
	if err := store.DB.Table("agent_runs").Where("task_id = ?", taskID).Count(&total).Error; err != nil {
		t.Fatalf("count runs after upgrade: %v", err)
	}
	if total != 4 || distinctAttempts != total {
		t.Fatalf("attempts after upgrade: total=%d distinct=%d", total, distinctAttempts)
	}
	var interrupted struct {
		Status      string
		StartedAt   *string
		CompletedAt *string
	}
	if err := store.DB.Table("agent_runs").Where("id = ?", "018f0000-0000-7000-8000-000000007417").Take(&interrupted).Error; err != nil {
		t.Fatalf("load reconciled surplus active run: %v", err)
	}
	if interrupted.Status != "interrupted" || interrupted.StartedAt == nil || interrupted.CompletedAt == nil {
		t.Fatalf("reconciled surplus active run = %#v", interrupted)
	}
}
