-- 063: explicitly triggered, local-only AI quality evaluation runs. The
-- database stores outcome codes and content-free metrics, never case prompts,
-- knowledge text, model answers, reasoning, endpoint URLs, or credentials.
CREATE TABLE ai_evaluation_runs (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES ai_providers(id) ON DELETE RESTRICT,
    provider_name_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_name_snapshot)) BETWEEN 1 AND 100 AND provider_name_snapshot = trim(provider_name_snapshot)),
    provider_model_snapshot TEXT NOT NULL
        CHECK (length(trim(provider_model_snapshot)) BETWEEN 1 AND 200 AND provider_model_snapshot = trim(provider_model_snapshot)),
    provider_protocol_snapshot TEXT NOT NULL CHECK (provider_protocol_snapshot = 'openai_chat'),
    provider_version INTEGER NOT NULL CHECK (provider_version >= 1),
    dataset_version INTEGER NOT NULL CHECK (dataset_version = 1),
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
    CHECK (passed_cases + failed_cases + error_cases = completed_cases),
    CHECK (
        (status = 'queued' AND started_at IS NULL AND completed_at IS NULL AND current_case_id IS NULL AND error_code IS NULL)
        OR (status = 'running' AND started_at IS NOT NULL AND completed_at IS NULL AND error_code IS NULL)
        OR (status = 'succeeded' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NULL AND completed_cases = total_cases AND error_cases = 0)
        OR (status = 'failed' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NOT NULL)
        OR (status = 'cancelled' AND completed_at IS NOT NULL AND current_case_id IS NULL AND error_code IS NULL)
    )
);

CREATE UNIQUE INDEX idx_ai_evaluation_runs_active_provider
    ON ai_evaluation_runs(provider_id)
    WHERE status IN ('queued', 'running');

CREATE INDEX idx_ai_evaluation_runs_created
    ON ai_evaluation_runs(created_at DESC, id DESC);

CREATE TABLE ai_evaluation_results (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES ai_evaluation_runs(id) ON DELETE CASCADE,
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

CREATE INDEX idx_ai_evaluation_results_run_sequence
    ON ai_evaluation_results(run_id, sequence);

CREATE TRIGGER trg_ai_evaluation_results_immutable_update
BEFORE UPDATE ON ai_evaluation_results
BEGIN
    SELECT RAISE(ABORT, 'AI_EVALUATION_RESULT_IMMUTABLE');
END;
