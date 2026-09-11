-- migration: destructive

-- Row version remains the HTTP concurrency token. Configuration identity only
-- advances when request semantics or credentials change. Legacy run/review
-- snapshots cannot be reconstructed safely and intentionally remain NULL.
ALTER TABLE ai_providers ADD COLUMN config_version INTEGER NOT NULL DEFAULT 1
    CHECK (config_version >= 1);
ALTER TABLE ai_evaluation_runs ADD COLUMN provider_config_version INTEGER
    CHECK (provider_config_version IS NULL OR provider_config_version >= 1);
-- Rebuild only the review ledger to add configuration identity and allow the
-- fourth critical signal (FACT_CONTRADICTED). Copy all old audit fields verbatim;
-- the new identity remains NULL. Restore the original immutability constraints.
CREATE TABLE ai_evaluation_reviews_v68 (
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
        CHECK (json_valid(critical_failure_codes) AND json_type(critical_failure_codes) = 'array' AND json_array_length(critical_failure_codes) <= 4 AND length(critical_failure_codes) <= 512),
    decision TEXT NOT NULL
        CHECK (decision IN ('accepted_for_local_use', 'needs_more_evidence', 'rejected')),
    reason TEXT NOT NULL
        CHECK (reason = trim(reason) AND length(reason) BETWEEN 1 AND 1000),
    reviewed_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT
        CHECK (reviewed_by_actor_id = '00000000-0000-5000-8000-000000000001'),
    reviewed_by_actor_name_snapshot TEXT NOT NULL
        CHECK (length(trim(reviewed_by_actor_name_snapshot)) BETWEEN 1 AND 100 AND reviewed_by_actor_name_snapshot = trim(reviewed_by_actor_name_snapshot)),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    provider_config_version INTEGER CHECK (provider_config_version IS NULL OR provider_config_version >= 1),
    CHECK (passed_cases + failed_cases = total_cases)
);

INSERT INTO ai_evaluation_reviews_v68 (
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

DROP TRIGGER trg_ai_evaluation_reviews_immutable_delete;
DROP TABLE ai_evaluation_reviews;
ALTER TABLE ai_evaluation_reviews_v68 RENAME TO ai_evaluation_reviews;

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

CREATE TRIGGER trg_ai_provider_config_version_step
BEFORE UPDATE ON ai_providers
WHEN NEW.config_version < OLD.config_version OR NEW.config_version > OLD.config_version + 1
BEGIN
    SELECT RAISE(ABORT, 'AI_PROVIDER_CONFIG_VERSION_INVALID');
END;

CREATE TRIGGER trg_ai_evaluation_config_snapshot_immutable
BEFORE UPDATE ON ai_evaluation_runs
WHEN NEW.provider_config_version IS NOT OLD.provider_config_version
BEGIN
    SELECT RAISE(ABORT, 'AI_EVALUATION_CONFIG_IMMUTABLE');
END;

CREATE INDEX idx_ai_evaluation_runs_config_group ON ai_evaluation_runs
    (provider_id, provider_config_version, dataset_version, suite_key, status, completed_at);
