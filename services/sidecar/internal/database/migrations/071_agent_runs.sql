-- 071: agent runs for the builtin executor (ADR-027, T-19 v0.2-B). Runs are
-- immutable execution facts; retries create new rows linked by parent_run_id.
-- Input snapshots are sanitized task facts plus a loopback-only model
-- endpoint; no tokens, nonces, or executor paths are stored.

ALTER TABLE actors ADD COLUMN agent_adapter_id TEXT
    REFERENCES agent_adapters(id) ON DELETE SET NULL
    CHECK (
        (type = 'agent' AND agent_adapter_id IS NOT NULL)
        OR (type <> 'agent' AND agent_adapter_id IS NULL)
    );

CREATE INDEX idx_actors_agent_adapter ON actors(agent_adapter_id) WHERE type = 'agent';

CREATE TABLE agent_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    assignment_id TEXT NOT NULL REFERENCES task_assignments(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL REFERENCES actors(id),
    adapter_id TEXT NOT NULL REFERENCES agent_adapters(id),
    created_by_actor_id TEXT NOT NULL REFERENCES actors(id),
    parent_run_id TEXT REFERENCES agent_runs(id) ON DELETE SET NULL,
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    status TEXT NOT NULL
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted')),
    provider_id TEXT NOT NULL,
    model TEXT NOT NULL CHECK (length(trim(model)) BETWEEN 1 AND 255),
    input_snapshot_json TEXT NOT NULL CHECK (length(input_snapshot_json) BETWEEN 1 AND 262144),
    result_text TEXT CHECK (
        (status = 'succeeded' AND result_text IS NOT NULL AND length(result_text) BETWEEN 1 AND 65536)
        OR (status <> 'succeeded' AND result_text IS NULL)
    ),
    result_bytes INTEGER,
    error_code TEXT,
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    CHECK (
        (status IN ('queued', 'running') AND completed_at IS NULL AND started_at IS NOT NULL)
        OR (status = 'queued' AND started_at IS NULL)
        OR (
            status IN ('succeeded', 'failed', 'cancelled', 'interrupted')
            AND completed_at IS NOT NULL AND started_at IS NOT NULL
        )
    ),
    CHECK (
        (status = 'succeeded' AND error_code IS NULL AND result_bytes IS NOT NULL)
        OR (status = 'failed' AND error_code IS NOT NULL)
        OR (status IN ('queued', 'running', 'cancelled', 'interrupted') AND error_code IS NULL AND result_bytes IS NULL)
    )
);

CREATE INDEX idx_agent_runs_task_timeline ON agent_runs(task_id, created_at DESC, id DESC);
CREATE INDEX idx_agent_runs_parent ON agent_runs(parent_run_id) WHERE parent_run_id IS NOT NULL;
