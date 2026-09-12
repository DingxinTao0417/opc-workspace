package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAILocalEvaluationMigrationCreatesContentFreeRunLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-local-evaluation.db")
	v62 := openDatabaseAtVersion(t, path, 62)
	const providerID = "018f0000-0000-7000-8000-000000006701"
	if _, err := v62.Exec(`
		INSERT INTO ai_providers(
			id, name, kind, protocol, base_url, model, status, health_status,
			has_key, last_health_at, version, created_at, updated_at
		) VALUES (?, 'Local evaluator', 'local', 'openai_chat', 'http://127.0.0.1:11434/v1',
			'model', 'ready', 'healthy', 0, '2026-09-09T08:00:00Z', 1,
			'2026-09-09T08:00:00Z', '2026-09-09T08:00:00Z')
	`, providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if err := v62.Close(); err != nil {
		t.Fatalf("close v62 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion=%d, want 71", store.SchemaVersion)
	}

	const runID = "018f0000-0000-7000-8000-000000006702"
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, status,
			total_cases, created_at, updated_at
		) VALUES (?, ?, 'Local evaluator', 'model', 'openai_chat', 1, 1, 'queued', 4,
			'2026-09-09T08:00:00Z', '2026-09-09T08:00:00Z')
	`, runID, providerID); err != nil {
		t.Fatalf("insert queued evaluation: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, status,
			total_cases, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006703', ?, 'Local evaluator', 'model',
			'openai_chat', 1, 1, 'queued', 4, '2026-09-09T08:00:00Z', '2026-09-09T08:00:00Z')
	`, providerID); err == nil {
		t.Fatal("second active evaluation for one provider was accepted")
	}
	if _, err := store.SQL.Exec(`
		UPDATE ai_evaluation_runs SET status='running', started_at='2026-09-09T08:00:01Z',
			current_case_id='zh_invoice_grounded', updated_at='2026-09-09T08:00:01Z'
		WHERE id=?
	`, runID); err != nil {
		t.Fatalf("start evaluation: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_results(
			id, run_id, sequence, case_id, language, category, status, failure_codes,
			citation_status, citation_count, duration_ms, input_bytes, output_bytes,
			input_tokens, output_tokens, token_source, created_at
		) VALUES ('018f0000-0000-7000-8000-000000006704', ?, 1, 'zh_invoice_grounded',
			'zh-CN', 'grounded', 'failed', '["REQUIRED_PHRASE_MISSING"]', 'validated', 1,
			20, 100, 40, 12, 5, 'provider', '2026-09-09T08:00:02Z')
	`, runID); err != nil {
		t.Fatalf("insert content-free result: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_results(
			id, run_id, sequence, case_id, language, category, status, failure_codes,
			citation_status, citation_count, duration_ms, input_bytes, output_bytes,
			input_tokens, output_tokens, token_source, created_at
		) VALUES ('018f0000-0000-7000-8000-000000006705', ?, 2, 'bad-partial',
			'en', 'grounded', 'passed', '[]', 'validated', 1, 20, 100, 40, 12, NULL,
			'provider', '2026-09-09T08:00:02Z')
	`, runID); err == nil {
		t.Fatal("partial Provider token usage was accepted")
	}
	if _, err := store.SQL.Exec(`
		UPDATE ai_evaluation_results SET failure_codes='[]'
		WHERE id='018f0000-0000-7000-8000-000000006704'
	`); err == nil || !strings.Contains(err.Error(), "AI_EVALUATION_RESULT_IMMUTABLE") {
		t.Fatalf("immutable result update error=%v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM ai_providers WHERE id = ?", providerID); err == nil {
		t.Fatal("provider with evaluation history was deleted")
	}
	var forbiddenColumns int
	if err := store.SQL.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('ai_evaluation_results')
		WHERE name IN ('prompt', 'content', 'answer', 'reasoning', 'base_url', 'api_key')
	`).Scan(&forbiddenColumns); err != nil || forbiddenColumns != 0 {
		t.Fatalf("forbidden result columns=%d err=%v", forbiddenColumns, err)
	}
}

func TestAIEvaluationDatasetVersionMigrationPreservesV1AndAllowsV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-evaluation-dataset-v2.db")
	v63 := openDatabaseAtVersion(t, path, 63)
	const (
		providerID = "018f0000-0000-7000-8000-000000006711"
		runID      = "018f0000-0000-7000-8000-000000006712"
		resultID   = "018f0000-0000-7000-8000-000000006713"
	)
	if _, err := v63.Exec(`
		INSERT INTO ai_providers(
			id, name, kind, protocol, base_url, model, status, health_status,
			has_key, last_health_at, version, created_at, updated_at
		) VALUES (?, 'Dataset migration model', 'local', 'openai_chat',
			'http://127.0.0.1:11434/v1', 'model-v1', 'ready', 'healthy', 0,
			'2026-09-09T09:00:00Z', 1, '2026-09-09T09:00:00Z', '2026-09-09T09:00:00Z')
	`, providerID); err != nil {
		t.Fatalf("seed v63 provider: %v", err)
	}
	if _, err := v63.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, status,
			total_cases, completed_cases, passed_cases, failed_cases, error_cases,
			started_at, completed_at, created_at, updated_at
		) VALUES (?, ?, 'Dataset migration model', 'model-v1', 'openai_chat', 1, 1,
			'succeeded', 4, 4, 3, 1, 0, '2026-09-09T09:00:00Z',
			'2026-09-09T09:00:04Z', '2026-09-09T09:00:00Z', '2026-09-09T09:00:04Z')
	`, runID, providerID); err != nil {
		t.Fatalf("seed v1 evaluation run: %v", err)
	}
	if _, err := v63.Exec(`
		INSERT INTO ai_evaluation_results(
			id, run_id, sequence, case_id, language, category, status, failure_codes,
			citation_status, citation_count, duration_ms, input_bytes, output_bytes,
			created_at
		) VALUES (?, ?, 4, 'zh_conflicting_payment_terms', 'zh-CN',
			'conflicting_sources', 'failed', '["CITATION_SET_MISMATCH"]',
			'validated', 1, 20, 100, 40, '2026-09-09T09:00:04Z')
	`, resultID, runID); err != nil {
		t.Fatalf("seed v1 evaluation result: %v", err)
	}
	if err := v63.Close(); err != nil {
		t.Fatalf("close v63 database: %v", err)
	}
	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatalf("open schema 64 migration gate: %v", err)
	}
	if gated.SchemaVersion != 63 || gate == nil || gate.CurrentVersion != 63 || gate.TargetVersion != 71 ||
		len(gate.PendingVersions) != 8 || gate.PendingVersions[0] != 64 || gate.PendingVersions[1] != 65 || gate.PendingVersions[2] != 66 || gate.PendingVersions[3] != 67 || gate.PendingVersions[4] != 68 || gate.PendingVersions[5] != 69 || gate.PendingVersions[6] != 70 || gate.PendingVersions[7] != 71 {
		_ = gated.Close()
		t.Fatalf("schema 64 migration gate store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	var preMigrationSQL string
	if err := gated.SQL.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='ai_evaluation_runs'").Scan(&preMigrationSQL); err != nil || !strings.Contains(preMigrationSQL, "dataset_version = 1") {
		_ = gated.Close()
		t.Fatalf("pre-migration dataset constraint=%q err=%v", preMigrationSQL, err)
	}
	if err := gated.Close(); err != nil {
		t.Fatalf("close gated v63 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("apply schema 64: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion=%d, want 71", store.SchemaVersion)
	}
	var postMigrationSQL string
	if err := store.SQL.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='ai_evaluation_runs'").Scan(&postMigrationSQL); err != nil || !strings.Contains(postMigrationSQL, "dataset_version >= 1") {
		t.Fatalf("post-migration dataset constraint=%q err=%v", postMigrationSQL, err)
	}
	var datasetVersion, totalCases, failedCases int
	if err := store.SQL.QueryRow(`
		SELECT dataset_version, total_cases, failed_cases
		FROM ai_evaluation_runs WHERE id = ?
	`, runID).Scan(&datasetVersion, &totalCases, &failedCases); err != nil {
		t.Fatalf("read preserved v1 run: %v", err)
	}
	if datasetVersion != 1 || totalCases != 4 || failedCases != 1 {
		t.Fatalf("preserved v1 run dataset=%d total=%d failed=%d", datasetVersion, totalCases, failedCases)
	}
	var suiteKey string
	if err := store.SQL.QueryRow("SELECT suite_key FROM ai_evaluation_runs WHERE id = ?", runID).Scan(&suiteKey); err != nil || suiteKey != "full" {
		t.Fatalf("preserved v1 run suite=%q err=%v", suiteKey, err)
	}
	var failureCodes string
	if err := store.SQL.QueryRow("SELECT failure_codes FROM ai_evaluation_results WHERE id = ?", resultID).Scan(&failureCodes); err != nil || failureCodes != `["CITATION_SET_MISMATCH"]` {
		t.Fatalf("preserved v1 result failure_codes=%q err=%v", failureCodes, err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, status,
			total_cases, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006714', ?, 'Dataset migration model',
			'model-v1', 'openai_chat', 1, 2, 'queued', 12,
			'2026-09-09T09:01:00Z', '2026-09-09T09:01:00Z')
	`, providerID); err != nil {
		t.Fatalf("insert dataset v2 run: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, status,
			total_cases, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006715', ?, 'Dataset migration model',
			'model-v1', 'openai_chat', 1, 0, 'queued', 12,
			'2026-09-09T09:01:00Z', '2026-09-09T09:01:00Z')
	`, providerID); err == nil {
		t.Fatal("dataset version zero was accepted")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, suite_key, status,
			total_cases, completed_cases, passed_cases, failed_cases, error_cases,
			started_at, completed_at, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006716', ?, 'Dataset migration model',
			'model-v1', 'openai_chat', 1, 3, 'smoke', 'succeeded', 8, 8, 8, 0, 0,
			'2026-09-09T09:02:00Z', '2026-09-09T09:02:08Z',
			'2026-09-09T09:02:00Z', '2026-09-09T09:02:08Z')
	`, providerID); err != nil {
		t.Fatalf("insert smoke suite run: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, suite_key, status,
			total_cases, completed_cases, passed_cases, failed_cases, error_cases,
			started_at, completed_at, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006717', ?, 'Dataset migration model',
			'model-v1', 'openai_chat', 1, 3, 'quick', 'succeeded', 8, 8, 8, 0, 0,
			'2026-09-09T09:03:00Z', '2026-09-09T09:03:08Z',
			'2026-09-09T09:03:00Z', '2026-09-09T09:03:08Z')
	`, providerID); err == nil {
		t.Fatal("unknown evaluation suite was accepted")
	}
	if _, err := store.SQL.Exec("UPDATE ai_evaluation_results SET failure_codes='[]' WHERE id = ?", resultID); err == nil || !strings.Contains(err.Error(), "AI_EVALUATION_RESULT_IMMUTABLE") {
		t.Fatalf("rebuilt immutable result update error=%v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
