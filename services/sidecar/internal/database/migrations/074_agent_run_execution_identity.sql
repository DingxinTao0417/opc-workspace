-- 074: freeze every mutable identity used by an Agent Run and let SQLite
-- arbitrate both the one-active-run invariant and monotonic attempt numbers.
-- Legacy rows keep zero/empty identity snapshots and remain readable, but the
-- runtime will fail them closed instead of executing them after an upgrade.

ALTER TABLE agent_runs ADD COLUMN task_version INTEGER NOT NULL DEFAULT 0
    CHECK (task_version >= 0);
ALTER TABLE agent_runs ADD COLUMN assignment_assigned_at TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN actor_version INTEGER NOT NULL DEFAULT 0
    CHECK (actor_version >= 0);
ALTER TABLE agent_runs ADD COLUMN adapter_version INTEGER NOT NULL DEFAULT 0
    CHECK (adapter_version >= 0);
ALTER TABLE agent_runs ADD COLUMN provider_version INTEGER NOT NULL DEFAULT 0
    CHECK (provider_version >= 0);
ALTER TABLE agent_runs ADD COLUMN provider_config_version INTEGER NOT NULL DEFAULT 0
    CHECK (provider_config_version >= 0);
ALTER TABLE agent_runs ADD COLUMN execution_contract_version INTEGER NOT NULL DEFAULT 0
    CHECK (execution_contract_version >= 0);

-- v71 did not yet arbitrate concurrency in SQLite. Preserve every historical
-- row while deterministically interrupting only surplus active rows and moving
-- duplicate attempt numbers above the former per-task/actor maximum. The one
-- remaining active row is recovered by normal startup recovery after migration.
WITH ranked_active AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at, id) AS position
    FROM agent_runs
    WHERE status IN ('queued', 'running')
)
UPDATE agent_runs
SET status = 'interrupted',
    started_at = COALESCE(started_at, created_at),
    completed_at = COALESCE(completed_at, created_at)
WHERE id IN (SELECT id FROM ranked_active WHERE position > 1);

WITH ranked_attempts AS (
    SELECT id, task_id, actor_id, created_at,
           ROW_NUMBER() OVER (
               PARTITION BY task_id, actor_id, attempt
               ORDER BY created_at, id
           ) AS duplicate_position
    FROM agent_runs
),
duplicate_attempts AS (
    SELECT id, task_id, actor_id, created_at
    FROM ranked_attempts
    WHERE duplicate_position > 1
),
attempt_maxima AS (
    SELECT task_id, actor_id, MAX(attempt) AS max_attempt
    FROM agent_runs
    GROUP BY task_id, actor_id
),
renumbered AS (
    SELECT duplicate_attempts.id,
           attempt_maxima.max_attempt + ROW_NUMBER() OVER (
               PARTITION BY duplicate_attempts.task_id, duplicate_attempts.actor_id
               ORDER BY duplicate_attempts.created_at, duplicate_attempts.id
           ) AS new_attempt
    FROM duplicate_attempts
    JOIN attempt_maxima
      ON attempt_maxima.task_id = duplicate_attempts.task_id
     AND attempt_maxima.actor_id = duplicate_attempts.actor_id
)
UPDATE agent_runs
SET attempt = (SELECT new_attempt FROM renumbered WHERE renumbered.id = agent_runs.id)
WHERE id IN (SELECT id FROM renumbered);

CREATE UNIQUE INDEX ux_agent_runs_task_active
ON agent_runs(task_id)
WHERE status IN ('queued', 'running');

CREATE UNIQUE INDEX ux_agent_runs_task_actor_attempt
ON agent_runs(task_id, actor_id, attempt);
