package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIEvaluationReviewMigrationCreatesImmutableLocalAuditLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-evaluation-review.db")
	v65 := openDatabaseAtVersion(t, path, 65)
	if err := v65.Close(); err != nil {
		t.Fatalf("close v65 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 70 {
		t.Fatalf("SchemaVersion=%d, want 70", store.SchemaVersion)
	}

	const reviewID = "018f0000-0000-7000-8000-000000006801"
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_evaluation_reviews(
			id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
			dataset_version, suite_key, provider_version_min, provider_version_max,
			group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
			overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
			readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
			reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
		) VALUES (?, '018f0000-0000-7000-8000-000000006800', 'Local evaluator', 'model-v1',
			3, 'full', 1, 1, '2026-09-09T10:00:00Z', 3, 72, 72, 0,
			9494, 'conflicting_sources', 8628, 'review_candidate', '[]', '[]',
			'accepted_for_local_use', '三次完整评测稳定通过，仅批准本机试用。',
			'00000000-0000-5000-8000-000000000001', 'Owner', '2026-09-09T10:05:00Z')
	`, reviewID); err != nil {
		t.Fatalf("insert evaluation review: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE ai_evaluation_reviews SET reason='changed' WHERE id=?", reviewID); err == nil ||
		!strings.Contains(err.Error(), "AI_EVALUATION_REVIEW_IMMUTABLE") {
		t.Fatalf("immutable review update error=%v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM ai_evaluation_reviews WHERE id=?", reviewID); err == nil ||
		!strings.Contains(err.Error(), "AI_EVALUATION_REVIEW_IMMUTABLE") {
		t.Fatalf("immutable review delete error=%v", err)
	}

	invalidStatements := []string{
		`INSERT INTO ai_evaluation_reviews SELECT '018f0000-0000-7000-8000-000000006802', provider_id_snapshot, provider_name_snapshot, provider_model_snapshot, dataset_version, suite_key, provider_version_min, provider_version_max, group_last_completed_at, run_count, total_cases, passed_cases, failed_cases, overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps, readiness_status, readiness_reasons, critical_failure_codes, 'approved', reason, reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at FROM ai_evaluation_reviews WHERE id='018f0000-0000-7000-8000-000000006801'`,
		`INSERT INTO ai_evaluation_reviews SELECT '018f0000-0000-7000-8000-000000006803', provider_id_snapshot, provider_name_snapshot, provider_model_snapshot, dataset_version, suite_key, provider_version_min, provider_version_max, group_last_completed_at, run_count, total_cases, passed_cases, failed_cases, overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps, readiness_status, readiness_reasons, critical_failure_codes, decision, '', reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at FROM ai_evaluation_reviews WHERE id='018f0000-0000-7000-8000-000000006801'`,
		`INSERT INTO ai_evaluation_reviews SELECT '018f0000-0000-7000-8000-000000006804', provider_id_snapshot, provider_name_snapshot, provider_model_snapshot, dataset_version, suite_key, provider_version_min, provider_version_max, group_last_completed_at, run_count, total_cases, passed_cases, failed_cases, overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps, readiness_status, 'not-json', critical_failure_codes, decision, reason, reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at FROM ai_evaluation_reviews WHERE id='018f0000-0000-7000-8000-000000006801'`,
		`INSERT INTO ai_evaluation_reviews SELECT '018f0000-0000-7000-8000-000000006805', provider_id_snapshot, provider_name_snapshot, provider_model_snapshot, dataset_version, suite_key, provider_version_min, provider_version_max, group_last_completed_at, run_count, total_cases, passed_cases, failed_cases, overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps, readiness_status, readiness_reasons, critical_failure_codes, decision, reason, '018f0000-0000-7000-8000-000000006899', reviewed_by_actor_name_snapshot, created_at FROM ai_evaluation_reviews WHERE id='018f0000-0000-7000-8000-000000006801'`,
	}
	for index, statement := range invalidStatements {
		// Keep testing the intended constraints after nullable identity columns
		// are added; an implicit INSERT would fail only on column-count mismatch.
		statement = strings.Replace(statement, "INSERT INTO ai_evaluation_reviews SELECT", `INSERT INTO ai_evaluation_reviews(
			id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
			dataset_version, suite_key, provider_version_min, provider_version_max,
			group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
			overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
			readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
			reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at) SELECT`, 1)
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid review statement %d was accepted", index)
		}
	}

	var forbiddenColumns int
	if err := store.SQL.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('ai_evaluation_reviews')
		WHERE name IN ('prompt', 'question', 'answer', 'content', 'base_url', 'api_key')
	`).Scan(&forbiddenColumns); err != nil || forbiddenColumns != 0 {
		t.Fatalf("forbidden review columns=%d err=%v", forbiddenColumns, err)
	}
}
