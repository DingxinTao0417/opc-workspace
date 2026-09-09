-- migration: destructive
-- migration: foreign_keys=off

-- 067: widen the code-owned evaluation-suite identity with four balanced
-- category-specific diagnostic suites. Rebuild the Run/Result pair together
-- to preserve its exact foreign key, and rebuild the immutable review ledger
-- so a topic-suite evidence snapshot can be audited without weakening checks.
CREATE TABLE ai_evaluation_runs_v67 (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES ai_providers(id) ON DELETE RESTRICT,
    provider_name_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_name_snapshot)) BETWEEN 1 AND 100 AND provider_name_snapshot = trim(provider_name_snapshot)),
    provider_model_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_model_snapshot)) BETWEEN 1 AND 200 AND provider_model_snapshot = trim(provider_model_snapshot)),
    provider_protocol_snapshot TEXT NOT NULL CHECK (provider_protocol_snapshot = 'openai_chat'),
    provider_version INTEGER NOT NULL CHECK (provider_version >= 1),
    dataset_version INTEGER NOT NULL CHECK (dataset_version >= 1),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    total_cases INTEGER NOT NULL CHECK (total_cases BETWEEN 1 AND 32),
    completed_cases INTEGER NOT NULL DEFAULT 0 CHECK (completed_cases BETWEEN 0 AND total_cases),
    passed_cases INTEGER NOT NULL DEFAULT 0 CHECK (passed_cases BETWEEN 0 AND completed_cases),
    failed_cases INTEGER NOT NULL DEFAULT 0 CHECK (failed_cases BETWEEN 0 AND completed_cases),
    error_cases INTEGER NOT NULL DEFAULT 0 CHECK (error_cases BETWEEN 0 AND completed_cases),
    current_case_id TEXT
        CHECK (current_case_id IS NULL OR (length(trim(current_case_id)) BETWEEN 1 AND 100 AND current_case_id = trim(current_case_id))),
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
    error_code TEXT
        CHECK (error_code IS NULL OR (length(trim(error_code)) BETWEEN 1 AND 100 AND error_code = trim(error_code))),
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    suite_key TEXT NOT NULL DEFAULT 'full'
        CHECK (suite_key IN ('smoke', 'full', 'grounded', 'no_evidence', 'prompt_injection', 'conflicting_sources')),
    CHECK (passed_cases + failed_cases + error_cases = completed_cases),
    CHECK (
        (status = 'queued' AND started_at IS NULL AND completed_at IS NULL AND current_case_id IS NULL AND error_code IS NULL)
        OR (status = 'running' AND started_at IS NOT NULL AND completed_at IS NULL AND error_code IS NULL)
        OR (status = 'succeeded' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NULL AND completed_cases = total_cases AND error_cases = 0)
        OR (status = 'failed' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NOT NULL)
        OR (status = 'cancelled' AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NULL)
    )
);

CREATE TABLE ai_evaluation_results_v67 (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES ai_evaluation_runs_v67(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 32),
    case_id TEXT NOT NULL
        CHECK (length(trim(case_id)) BETWEEN 1 AND 100 AND case_id = trim(case_id)),
    language TEXT NOT NULL CHECK (language IN ('zh-CN', 'en')),
    category TEXT NOT NULL CHECK (category IN ('grounded', 'no_evidence', 'prompt_injection', 'conflicting_sources')),
    status TEXT NOT NULL CHECK (status IN ('passed', 'failed', 'error')),
    failure_codes TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(failure_codes) AND json_type(failure_codes) = 'array' AND json_array_length(failure_codes) <= 32 AND length(failure_codes) <= 4096),
    citation_status TEXT CHECK (citation_status IS NULL OR citation_status IN ('validated', 'no_evidence', 'missing', 'invalid')),
    citation_count INTEGER NOT NULL DEFAULT 0 CHECK (citation_count BETWEEN 0 AND 3),
    duration_ms INTEGER NOT NULL CHECK (duration_ms >= 0),
    input_bytes INTEGER NOT NULL CHECK (input_bytes >= 0),
    output_bytes INTEGER NOT NULL CHECK (output_bytes >= 0),
    input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
    token_source TEXT,
    error_code TEXT
        CHECK (error_code IS NULL OR (length(trim(error_code)) BETWEEN 1 AND 100 AND error_code = trim(error_code))),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    UNIQUE(run_id, sequence),
    UNIQUE(run_id, case_id),
    CHECK (
        (token_source IS NULL AND input_tokens IS NULL AND output_tokens IS NULL)
        OR (token_source = 'provider' AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL)
    ),
    CHECK (
        (status = 'passed' AND json_array_length(failure_codes) = 0 AND error_code IS NULL AND citation_status IS NOT NULL)
        OR (status = 'failed' AND json_array_length(failure_codes) >= 1 AND error_code IS NULL AND citation_status IS NOT NULL)
        OR (status = 'error' AND json_array_length(failure_codes) = 0 AND error_code IS NOT NULL AND citation_status IS NULL AND citation_count = 0)
    ),
    CHECK (
        status = 'error'
        OR (citation_status = 'validated' AND citation_count BETWEEN 1 AND 3)
        OR (citation_status IN ('no_evidence', 'missing', 'invalid') AND citation_count = 0)
    )
);

CREATE TABLE ai_evaluation_reviews_v67 (
    id TEXT PRIMARY KEY,
    provider_id_snapshot TEXT NOT NULL
        CHECK (length(provider_id_snapshot) = 36),
    provider_name_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_name_snapshot)) BETWEEN 1 AND 100 AND provider_name_snapshot = trim(provider_name_snapshot)),
    provider_model_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_model_snapshot)) BETWEEN 1 AND 200 AND provider_model_snapshot = trim(provider_model_snapshot)),
    dataset_version INTEGER NOT NULL CHECK (dataset_version >= 1),
    suite_key TEXT NOT NULL
        CHECK (suite_key IN ('smoke', 'full', 'grounded', 'no_evidence', 'prompt_injection', 'conflicting_sources')),
    provider_version_min INTEGER NOT NULL CHECK (provider_version_min >= 1),
    provider_version_max INTEGER NOT NULL CHECK (provider_version_max >= provider_version_min),
    group_last_completed_at TEXT NOT NULL CHECK (length(group_last_completed_at) > 0),
    run_count INTEGER NOT NULL CHECK (run_count >= 1),
    total_cases INTEGER NOT NULL CHECK (total_cases >= 1),
    passed_cases INTEGER NOT NULL CHECK (passed_cases BETWEEN 0 AND total_cases),
    failed_cases INTEGER NOT NULL CHECK (failed_cases BETWEEN 0 AND total_cases),
    overall_wilson_lower_bps INTEGER NOT NULL CHECK (overall_wilson_lower_bps BETWEEN 0 AND 10000),
    minimum_category TEXT NOT NULL
        CHECK (minimum_category IN ('grounded', 'no_evidence', 'prompt_injection', 'conflicting_sources')),
    minimum_category_wilson_lower_bps INTEGER NOT NULL
        CHECK (minimum_category_wilson_lower_bps BETWEEN 0 AND 10000),
    readiness_status TEXT NOT NULL
        CHECK (readiness_status IN ('insufficient_evidence', 'needs_attention', 'review_candidate')),
    readiness_reasons TEXT NOT NULL
        CHECK (json_valid(readiness_reasons) AND json_type(readiness_reasons) = 'array' AND json_array_length(readiness_reasons) <= 7 AND length(readiness_reasons) <= 1024),
    critical_failure_codes TEXT NOT NULL
        CHECK (json_valid(critical_failure_codes) AND json_type(critical_failure_codes) = 'array' AND json_array_length(critical_failure_codes) <= 3 AND length(critical_failure_codes) <= 512),
    decision TEXT NOT NULL
        CHECK (decision IN ('accepted_for_local_use', 'needs_more_evidence', 'rejected')),
    reason TEXT NOT NULL
        CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 1000),
    reviewed_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT
        CHECK (reviewed_by_actor_id = '00000000-0000-5000-8000-000000000001'),
    reviewed_by_actor_name_snapshot TEXT NOT NULL
        CHECK (length(trim(reviewed_by_actor_name_snapshot)) BETWEEN 1 AND 100 AND reviewed_by_actor_name_snapshot = trim(reviewed_by_actor_name_snapshot)),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    CHECK (passed_cases + failed_cases = total_cases)
);

INSERT INTO ai_evaluation_runs_v67 (
    id, provider_id, provider_name_snapshot, provider_model_snapshot,
    provider_protocol_snapshot, provider_version, dataset_version, status,
    total_cases, completed_cases, passed_cases, failed_cases, error_cases,
    current_case_id, cancel_requested, error_code, started_at, completed_at,
    created_at, updated_at, suite_key
)
SELECT
    id, provider_id, provider_name_snapshot, provider_model_snapshot,
    provider_protocol_snapshot, provider_version, dataset_version, status,
    total_cases, completed_cases, passed_cases, failed_cases, error_cases,
    current_case_id, cancel_requested, error_code, started_at, completed_at,
    created_at, updated_at, suite_key
FROM ai_evaluation_runs;

INSERT INTO ai_evaluation_results_v67 (
    id, run_id, sequence, case_id, language, category, status, failure_codes,
    citation_status, citation_count, duration_ms, input_bytes, output_bytes,
    input_tokens, output_tokens, token_source, error_code, created_at
)
SELECT
    id, run_id, sequence, case_id, language, category, status, failure_codes,
    citation_status, citation_count, duration_ms, input_bytes, output_bytes,
    input_tokens, output_tokens, token_source, error_code, created_at
FROM ai_evaluation_results;

INSERT INTO ai_evaluation_reviews_v67 (
    id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
    dataset_version, suite_key, provider_version_min, provider_version_max,
    group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
    overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
    readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
    reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
)
SELECT
    id, provider_id_snapshot, provider_name_snapshot, provider_model_snapshot,
    dataset_version, suite_key, provider_version_min, provider_version_max,
    group_last_completed_at, run_count, total_cases, passed_cases, failed_cases,
    overall_wilson_lower_bps, minimum_category, minimum_category_wilson_lower_bps,
    readiness_status, readiness_reasons, critical_failure_codes, decision, reason,
    reviewed_by_actor_id, reviewed_by_actor_name_snapshot, created_at
FROM ai_evaluation_reviews;

DROP TABLE ai_evaluation_results;
DROP TABLE ai_evaluation_runs;
DROP TABLE ai_evaluation_reviews;

ALTER TABLE ai_evaluation_runs_v67 RENAME TO ai_evaluation_runs;
ALTER TABLE ai_evaluation_results_v67 RENAME TO ai_evaluation_results;
ALTER TABLE ai_evaluation_reviews_v67 RENAME TO ai_evaluation_reviews;

CREATE UNIQUE INDEX idx_ai_evaluation_runs_active_provider
    ON ai_evaluation_runs(provider_id)
    WHERE status IN ('queued', 'running');

CREATE INDEX idx_ai_evaluation_runs_created
    ON ai_evaluation_runs(created_at DESC, id DESC);

CREATE INDEX idx_ai_evaluation_runs_quality_group
    ON ai_evaluation_runs(
        provider_id,
        provider_name_snapshot,
        provider_model_snapshot,
        dataset_version,
        suite_key,
        status,
        completed_at
    );

CREATE INDEX idx_ai_evaluation_results_run_sequence
    ON ai_evaluation_results(run_id, sequence);

CREATE INDEX idx_ai_evaluation_reviews_group_created
    ON ai_evaluation_reviews(
        provider_id_snapshot,
        provider_name_snapshot,
        provider_model_snapshot,
        dataset_version,
        suite_key,
        created_at DESC,
        id DESC
    );

CREATE INDEX idx_ai_evaluation_reviews_created
    ON ai_evaluation_reviews(created_at DESC, id DESC);

CREATE TRIGGER trg_ai_evaluation_results_immutable_update
BEFORE UPDATE ON ai_evaluation_results
BEGIN
    SELECT RAISE(ABORT, 'AI_EVALUATION_RESULT_IMMUTABLE');
END;

CREATE TRIGGER trg_ai_evaluation_reviews_immutable_update
BEFORE UPDATE ON ai_evaluation_reviews
BEGIN
    SELECT RAISE(ABORT, 'AI_EVALUATION_REVIEW_IMMUTABLE');
END;

CREATE TRIGGER trg_ai_evaluation_reviews_immutable_delete
BEFORE DELETE ON ai_evaluation_reviews
BEGIN
    SELECT RAISE(ABORT, 'AI_EVALUATION_REVIEW_IMMUTABLE');
END;
