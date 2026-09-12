package database

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAIEvaluationTopicSuiteMigrationPreservesHistoryAndWidensChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-evaluation-topic-suites.db")
	v66 := openDatabaseAtVersion(t, path, 66)
	const (
		providerID = "018f0000-0000-7000-8000-000000006901"
		runID      = "018f0000-0000-7000-8000-000000006902"
		resultID   = "018f0000-0000-7000-8000-000000006903"
		reviewID   = "018f0000-0000-7000-8000-000000006904"
	)
	if _, err := v66.Exec(`
		INSERT INTO ai_providers(
			id, name, kind, protocol, base_url, model, status, health_status,
			has_key, last_health_at, version, created_at, updated_at
		) VALUES (?, 'Topic migration model', 'local', 'openai_chat',
			'http://127.0.0.1:11434/v1', 'model-v3', 'ready', 'healthy', 0,
			'2026-09-09T12:00:00Z', 3, '2026-09-09T12:00:00Z', '2026-09-09T12:00:00Z')
	`, providerID); err != nil {
		t.Fatalf("seed v66 provider: %v", err)
	}
	if _, err := v66.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, suite_key,
			status, total_cases, completed_cases, passed_cases, failed_cases, error_cases,
			started_at, completed_at, created_at, updated_at
		) VALUES (?, ?, 'Topic migration model', 'model-v3', 'openai_chat', 3, 3,
			'full', 'succeeded', 1, 1, 1, 0, 0, '2026-09-09T12:00:00Z',
			'2026-09-09T12:00:01Z', '2026-09-09T12:00:00Z', '2026-09-09T12:00:01Z')
	`, runID, providerID); err != nil {
		t.Fatalf("seed v66 run: %v", err)
	}
	if _, err := v66.Exec(`
		INSERT INTO ai_evaluation_results(
			id, run_id, sequence, case_id, language, category, status, failure_codes,
			citation_status, citation_count, duration_ms, input_bytes, output_bytes,
			input_tokens, output_tokens, token_source, created_at
		) VALUES (?, ?, 1, 'zh_invoice_grounded', 'zh-CN', 'grounded', 'passed', '[]',
			'validated', 1, 10, 20, 30, 4, 5, 'provider', '2026-09-09T12:00:01Z')
	`, resultID, runID); err != nil {
		t.Fatalf("seed v66 result: %v", err)
	}
	if _, err := v66.Exec(`
		INSERT INTO ai_evaluation_reviews(
			id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
			dataset_version, suite_key, provider_version_min, provider_version_max,
			group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
			overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
			readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
			reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
		) VALUES (?, ?, 'Topic migration model', 'model-v3', 3, 'full', 3, 3,
			'2026-09-09T12:00:01Z', 1, 1, 1, 0, 2065, 'grounded', 2065,
			'insufficient_evidence', '["RUN_COUNT_LOW"]', '[]', 'needs_more_evidence',
			'保留完整套件历史。', '00000000-0000-5000-8000-000000000001', '我',
			'2026-09-09T12:00:02Z')
	`, reviewID, providerID); err != nil {
		t.Fatalf("seed v66 review: %v", err)
	}
	if err := v66.Close(); err != nil {
		t.Fatalf("close v66 database: %v", err)
	}

	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatalf("open schema 67 migration gate: %v", err)
	}
	if gated.SchemaVersion != 66 || gate == nil || gate.CurrentVersion != 66 || gate.TargetVersion != 71 ||
		!reflect.DeepEqual(gate.PendingVersions, []int{67, 68, 69, 70, 71}) {
		_ = gated.Close()
		t.Fatalf("schema 67 migration gate store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	if err := gated.Close(); err != nil {
		t.Fatalf("close gated v66 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("apply schema 67: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion=%d, want 71", store.SchemaVersion)
	}
	for table, id := range map[string]string{
		"ai_evaluation_runs": runID, "ai_evaluation_results": resultID, "ai_evaluation_reviews": reviewID,
	} {
		var count int
		if err := store.SQL.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id = ?", id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("preserved %s count=%d err=%v", table, count, err)
		}
	}

	const topicRunID = "018f0000-0000-7000-8000-000000006905"
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, suite_key,
			status, total_cases, created_at, updated_at
		) VALUES (?, ?, 'Topic migration model', 'model-v3', 'openai_chat', 3, 3,
			'grounded', 'queued', 6, '2026-09-09T12:01:00Z', '2026-09-09T12:01:00Z')
	`, topicRunID, providerID); err != nil {
		t.Fatalf("insert topic-suite run: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_reviews(
			id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
			dataset_version, suite_key, provider_version_min, provider_version_max,
			group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
			overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
			readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
			reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
		) VALUES ('018f0000-0000-7000-8000-000000006906', ?, 'Topic migration model',
			'model-v3', 3, 'grounded', 3, 3, '2026-09-09T12:02:00Z', 1, 6, 6, 0,
			6097, 'grounded', 6097, 'insufficient_evidence', '["SUITE_NOT_ELIGIBLE"]',
			'[]', 'needs_more_evidence', '专题套件只用于诊断。',
			'00000000-0000-5000-8000-000000000001', '我', '2026-09-09T12:02:01Z')
	`, providerID); err != nil {
		t.Fatalf("insert topic-suite review: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_runs(
			id, provider_id, provider_name_snapshot, provider_model_snapshot,
			provider_protocol_snapshot, provider_version, dataset_version, suite_key,
			status, total_cases, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000006907', ?, 'Topic migration model',
			'model-v3', 'openai_chat', 3, 3, 'quick', 'queued', 6,
			'2026-09-09T12:03:00Z', '2026-09-09T12:03:00Z')
	`, providerID); err == nil {
		t.Fatal("unknown topic suite was accepted")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_reviews
		SELECT '018f0000-0000-7000-8000-000000006908', provider_id_snapshot,
			provider_name_snapshot, provider_model_snapshot, dataset_version, 'quick',
			provider_version_min, provider_version_max, group_last_completed_at, run_count,
			total_cases, passed_cases, failed_cases, overall_wilson_lower_bps,
			minimum_category, minimum_category_wilson_lower_bps, readiness_status,
			readiness_reasons, critical_failure_codes, decision, reason,
			reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
		FROM ai_evaluation_reviews WHERE id = ?
	`, reviewID); err == nil {
		t.Fatal("unknown review topic suite was accepted")
	}
	if _, err := store.SQL.Exec("UPDATE ai_evaluation_results SET duration_ms=11 WHERE id=?", resultID); err == nil ||
		!strings.Contains(err.Error(), "AI_EVALUATION_RESULT_IMMUTABLE") {
		t.Fatalf("preserved result update error=%v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM ai_evaluation_reviews WHERE id=?", reviewID); err == nil ||
		!strings.Contains(err.Error(), "AI_EVALUATION_REVIEW_IMMUTABLE") {
		t.Fatalf("preserved review delete error=%v", err)
	}
}
