CREATE TABLE automation_rules (
    id TEXT PRIMARY KEY,
    preset_key TEXT NOT NULL UNIQUE
        CHECK (length(trim(preset_key)) BETWEEN 1 AND 100 AND preset_key = trim(preset_key)),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    config_json TEXT NOT NULL DEFAULT '{}'
        CHECK (
            json_valid(config_json)
            AND json_type(config_json) = 'object'
            AND length(config_json) BETWEEN 2 AND 4096
        ),
    next_run_at TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

CREATE INDEX idx_automation_rules_enabled_schedule
ON automation_rules(next_run_at, id)
WHERE enabled = 1 AND next_run_at IS NOT NULL;

CREATE TRIGGER trg_automation_rules_identity_immutable
BEFORE UPDATE ON automation_rules
WHEN NEW.id IS NOT OLD.id
  OR NEW.preset_key IS NOT OLD.preset_key
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_RULE_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_automation_rules_user_version_step
BEFORE UPDATE ON automation_rules
WHEN (
        NEW.enabled IS NOT OLD.enabled
        OR NEW.config_json IS NOT OLD.config_json
     )
  AND NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_RULE_VERSION_INVALID');
END;

CREATE TRIGGER trg_automation_rules_runtime_version_stable
BEFORE UPDATE ON automation_rules
WHEN NEW.enabled IS OLD.enabled
  AND NEW.config_json IS OLD.config_json
  AND NEW.version <> OLD.version
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_RULE_RUNTIME_VERSION_INVALID');
END;

CREATE TABLE automation_runs (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL REFERENCES automation_rules(id) ON DELETE RESTRICT,
    rule_version INTEGER NOT NULL CHECK (rule_version >= 1),
    trigger_type TEXT NOT NULL CHECK (trigger_type IN ('event', 'schedule')),
    source_event_id TEXT,
    scheduled_for TEXT,
    logical_key TEXT NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 300),
    dedupe_key TEXT NOT NULL UNIQUE CHECK (length(dedupe_key) BETWEEN 1 AND 340),
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed', 'skipped', 'cancelled')),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 3),
    retry_of_run_id TEXT REFERENCES automation_runs(id) ON DELETE RESTRICT,
    retryable INTEGER NOT NULL DEFAULT 0 CHECK (retryable IN (0, 1)),
    retry_at TEXT,
    caused_by_run_id TEXT REFERENCES automation_runs(id) ON DELETE RESTRICT,
    causal_depth INTEGER NOT NULL DEFAULT 0 CHECK (causal_depth BETWEEN 0 AND 4),
    config_snapshot_json TEXT NOT NULL
        CHECK (json_valid(config_snapshot_json) AND json_type(config_snapshot_json) = 'object'),
    action_snapshot_json TEXT NOT NULL
        CHECK (json_valid(action_snapshot_json) AND json_type(action_snapshot_json) = 'object'),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 100),
    result_type TEXT CHECK (result_type IS NULL OR result_type IN ('inbox_item', 'task', 'reminder')),
    result_id TEXT,
    result_summary TEXT NOT NULL DEFAULT '' CHECK (length(result_summary) <= 500),
    started_at TEXT NOT NULL CHECK (length(started_at) > 0),
    ended_at TEXT NOT NULL CHECK (length(ended_at) > 0),
    CHECK (
        (trigger_type = 'event' AND source_event_id IS NOT NULL AND scheduled_for IS NULL)
        OR (trigger_type = 'schedule' AND source_event_id IS NULL AND scheduled_for IS NOT NULL)
    ),
    CHECK (
        (attempt = 1 AND retry_of_run_id IS NULL)
        OR (attempt > 1 AND retry_of_run_id IS NOT NULL)
    ),
    CHECK (
        (status = 'succeeded' AND error_code IS NULL AND result_type IS NOT NULL AND result_id IS NOT NULL AND retryable = 0 AND retry_at IS NULL)
        OR (status = 'failed' AND error_code IS NOT NULL AND result_type IS NULL AND result_id IS NULL)
        OR (status IN ('skipped', 'cancelled') AND result_type IS NULL AND result_id IS NULL AND retryable = 0 AND retry_at IS NULL)
    ),
    CHECK (retry_at IS NULL OR (status = 'failed' AND retryable = 1 AND attempt < 3))
);

CREATE UNIQUE INDEX idx_automation_runs_logical_attempt
ON automation_runs(logical_key, attempt);

CREATE UNIQUE INDEX idx_automation_runs_rule_event
ON automation_runs(rule_id, source_event_id)
WHERE source_event_id IS NOT NULL AND attempt = 1;

CREATE UNIQUE INDEX idx_automation_runs_rule_schedule
ON automation_runs(rule_id, scheduled_for)
WHERE scheduled_for IS NOT NULL AND attempt = 1;

CREATE INDEX idx_automation_runs_history
ON automation_runs(started_at DESC, id);

CREATE INDEX idx_automation_runs_rule_history
ON automation_runs(rule_id, started_at DESC, id);

CREATE INDEX idx_automation_runs_retry_due
ON automation_runs(retry_at, id)
WHERE status = 'failed' AND retryable = 1 AND retry_at IS NOT NULL;

CREATE TRIGGER trg_automation_runs_immutable
BEFORE UPDATE ON automation_runs
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_RUN_IMMUTABLE');
END;
