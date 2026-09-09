-- 065: code-owned evaluation suite identity. Existing v1-v3 Runs were all
-- complete-dataset executions, so the non-null default preserves their exact
-- interpretation without rewriting Result rows.
ALTER TABLE ai_evaluation_runs ADD COLUMN suite_key TEXT NOT NULL DEFAULT 'full'
    CHECK (suite_key IN ('smoke', 'full'));

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
