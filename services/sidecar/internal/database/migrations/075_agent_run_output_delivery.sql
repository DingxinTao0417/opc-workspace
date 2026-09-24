-- migration: destructive
-- 075: make delivery of successful Agent Run output an explicit, durable
-- state machine. A Run is only succeeded after its output is either linked to
-- the normal manual-review chain or intentionally retained on the Run with a
-- safe reason. Transient delivery failures keep the Run active and stage the
-- bounded result outside result_text so the v71 terminal-result invariant is
-- never violated.

ALTER TABLE agent_runs ADD COLUMN output_delivery_status TEXT NOT NULL DEFAULT 'not_ready'
    CHECK (output_delivery_status IN ('not_ready', 'pending', 'submitted', 'retained'));
ALTER TABLE agent_runs ADD COLUMN output_delivery_error_code TEXT
    CHECK (
        output_delivery_error_code IS NULL
        OR length(trim(output_delivery_error_code)) BETWEEN 1 AND 100
    );
ALTER TABLE agent_runs ADD COLUMN submission_id TEXT;
ALTER TABLE agent_runs ADD COLUMN artifact_id TEXT;
ALTER TABLE agent_runs ADD COLUMN output_delivery_pending_text TEXT;
ALTER TABLE agent_runs ADD COLUMN output_delivery_pending_bytes INTEGER
    CHECK (
        output_delivery_pending_bytes IS NULL
        OR output_delivery_pending_bytes BETWEEN 1 AND 65536
    );
ALTER TABLE agent_runs ADD COLUMN output_delivery_pending_completed_at TEXT
    CHECK (
        output_delivery_pending_completed_at IS NULL
        OR length(output_delivery_pending_completed_at) > 0
    );

-- v74 and earlier recorded the only Run -> output link in immutable event
-- JSON. Accept a historical link only when every referenced row agrees with
-- the Run's frozen Task/actor and the candidate is unique on both sides.
CREATE TEMP TABLE agent_run_output_delivery_events AS
SELECT
    aggregate_id AS run_id,
    CASE
        WHEN current_json IS NOT NULL AND json_valid(current_json) THEN current_json
        ELSE '{}'
    END AS safe_current_json
FROM workflow_events
WHERE aggregate_type = 'agent_run'
  AND action = 'agent_run_output_submitted';

CREATE TEMP TABLE agent_run_output_delivery_candidates AS
SELECT
    run.id AS run_id,
    json_extract(event.safe_current_json, '$.submission_id') AS submission_id,
    json_extract(event.safe_current_json, '$.artifact_id') AS artifact_id
FROM agent_runs AS run
JOIN agent_run_output_delivery_events AS event
  ON event.run_id = run.id
JOIN task_submissions AS submission
  ON submission.id = json_extract(event.safe_current_json, '$.submission_id')
 AND submission.task_id = run.task_id
 AND submission.submitted_by_actor_id = run.actor_id
JOIN task_artifacts AS artifact
  ON artifact.id = json_extract(event.safe_current_json, '$.artifact_id')
 AND artifact.task_id = run.task_id
 AND artifact.submission_id = submission.id
 AND artifact.produced_by_actor_id = run.actor_id
WHERE run.status = 'succeeded'
  AND json_type(event.safe_current_json, '$.submission_id') = 'text'
  AND json_type(event.safe_current_json, '$.artifact_id') = 'text';

CREATE TEMP TABLE agent_run_output_delivery_unique_candidates AS
SELECT candidate.run_id, candidate.submission_id, candidate.artifact_id
FROM agent_run_output_delivery_candidates AS candidate
WHERE (
        SELECT COUNT(*)
        FROM agent_run_output_delivery_candidates AS own
        WHERE own.run_id = candidate.run_id
    ) = 1
  AND (
        SELECT COUNT(*)
        FROM agent_run_output_delivery_candidates AS other
        WHERE other.submission_id = candidate.submission_id
    ) = 1
  AND (
        SELECT COUNT(*)
        FROM agent_run_output_delivery_candidates AS other
        WHERE other.artifact_id = candidate.artifact_id
    ) = 1;

-- v71 bounded result_text by SQLite character count while the executor and
-- API contract bound UTF-8 bytes. Repair valid historical byte counts before
-- installing the immutable terminal-state trigger. If an invalid old result
-- already has one proven Submission/Artifact chain, preserve that succeeded
-- history coherently and replace only the Run copy with a bounded explanation;
-- the linked Artifact remains the authoritative complete output. An invalid
-- unlinked result is not truncated or presented as deliverable success.
UPDATE agent_runs
SET result_bytes = length(CAST(result_text AS BLOB))
WHERE status = 'succeeded'
  AND length(CAST(result_text AS BLOB)) BETWEEN 1 AND 65536;

UPDATE agent_runs
SET result_text = 'Historical Agent Run result omitted during schema 075 normalization; use the linked Task Artifact.',
    result_bytes = length(CAST('Historical Agent Run result omitted during schema 075 normalization; use the linked Task Artifact.' AS BLOB))
WHERE status = 'succeeded'
  AND COALESCE(length(CAST(result_text AS BLOB)), 0) NOT BETWEEN 1 AND 65536
  AND id IN (SELECT run_id FROM agent_run_output_delivery_unique_candidates);

UPDATE agent_runs
SET status = 'failed',
    result_text = NULL,
    result_bytes = NULL,
    error_code = CASE
        WHEN COALESCE(length(CAST(result_text AS BLOB)), 0) > 65536
            THEN 'AGENT_RESULT_TOO_LARGE'
        ELSE 'AGENT_RESULT_INVALID'
    END
WHERE status = 'succeeded'
  AND COALESCE(length(CAST(result_text AS BLOB)), 0) NOT BETWEEN 1 AND 65536
  AND id NOT IN (SELECT run_id FROM agent_run_output_delivery_unique_candidates);

UPDATE agent_runs
SET output_delivery_status = 'submitted',
    submission_id = (
        SELECT candidate.submission_id
        FROM agent_run_output_delivery_unique_candidates AS candidate
        WHERE candidate.run_id = agent_runs.id
    ),
    artifact_id = (
        SELECT candidate.artifact_id
        FROM agent_run_output_delivery_unique_candidates AS candidate
        WHERE candidate.run_id = agent_runs.id
    )
WHERE status = 'succeeded'
  AND id IN (SELECT run_id FROM agent_run_output_delivery_unique_candidates);

UPDATE agent_runs
SET output_delivery_status = 'retained',
    output_delivery_error_code = 'AGENT_OUTPUT_DELIVERY_LEGACY'
WHERE status = 'succeeded'
  AND output_delivery_status = 'not_ready';

DROP TABLE agent_run_output_delivery_candidates;
DROP TABLE agent_run_output_delivery_unique_candidates;
DROP TABLE agent_run_output_delivery_events;

CREATE UNIQUE INDEX ux_agent_runs_output_submission
ON agent_runs(submission_id)
WHERE submission_id IS NOT NULL;

CREATE UNIQUE INDEX ux_agent_runs_output_artifact
ON agent_runs(artifact_id)
WHERE artifact_id IS NOT NULL;

CREATE INDEX idx_agent_runs_output_delivery_recovery
ON agent_runs(output_delivery_status, created_at, id)
WHERE output_delivery_status = 'pending';

CREATE TRIGGER trg_agent_runs_output_delivery_state_insert
BEFORE INSERT ON agent_runs
WHEN (
    (
        NEW.output_delivery_status = 'not_ready'
        AND NEW.status <> 'succeeded'
        AND NEW.output_delivery_error_code IS NULL
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
    )
    OR (
        NEW.output_delivery_status = 'pending'
        AND NEW.status = 'running'
        AND NEW.output_delivery_error_code = 'AGENT_OUTPUT_DELIVERY_PENDING'
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NOT NULL
        AND length(NEW.output_delivery_pending_text) BETWEEN 1 AND 65536
        AND NEW.output_delivery_pending_bytes = length(CAST(NEW.output_delivery_pending_text AS BLOB))
        AND NEW.output_delivery_pending_completed_at IS NOT NULL
    )
    OR (
        NEW.output_delivery_status = 'submitted'
        AND NEW.status = 'succeeded'
        AND NEW.output_delivery_error_code IS NULL
        AND NEW.submission_id IS NOT NULL
        AND NEW.artifact_id IS NOT NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
        AND NEW.result_bytes BETWEEN 1 AND 65536
        AND NEW.result_bytes = length(CAST(NEW.result_text AS BLOB))
        AND EXISTS (
            SELECT 1
            FROM task_submissions AS submission
            JOIN task_artifacts AS artifact
              ON artifact.id = NEW.artifact_id
             AND artifact.submission_id = submission.id
            WHERE submission.id = NEW.submission_id
              AND submission.task_id = NEW.task_id
              AND submission.submitted_by_actor_id = NEW.actor_id
              AND artifact.task_id = NEW.task_id
              AND artifact.produced_by_actor_id = NEW.actor_id
        )
    )
    OR (
        NEW.output_delivery_status = 'retained'
        AND NEW.status = 'succeeded'
        AND NEW.output_delivery_error_code IS NOT NULL
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
        AND NEW.result_bytes BETWEEN 1 AND 65536
        AND NEW.result_bytes = length(CAST(NEW.result_text AS BLOB))
    )
) IS NOT TRUE
BEGIN
    SELECT RAISE(ABORT, 'AGENT_RUN_OUTPUT_DELIVERY_INVALID');
END;

CREATE TRIGGER trg_agent_runs_output_delivery_state_update
BEFORE UPDATE ON agent_runs
WHEN (
    (
        NEW.output_delivery_status = 'not_ready'
        AND NEW.status <> 'succeeded'
        AND NEW.output_delivery_error_code IS NULL
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
    )
    OR (
        NEW.output_delivery_status = 'pending'
        AND NEW.status = 'running'
        AND NEW.output_delivery_error_code = 'AGENT_OUTPUT_DELIVERY_PENDING'
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NOT NULL
        AND length(NEW.output_delivery_pending_text) BETWEEN 1 AND 65536
        AND NEW.output_delivery_pending_bytes = length(CAST(NEW.output_delivery_pending_text AS BLOB))
        AND NEW.output_delivery_pending_completed_at IS NOT NULL
    )
    OR (
        NEW.output_delivery_status = 'submitted'
        AND NEW.status = 'succeeded'
        AND NEW.output_delivery_error_code IS NULL
        AND NEW.submission_id IS NOT NULL
        AND NEW.artifact_id IS NOT NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
        AND NEW.result_bytes BETWEEN 1 AND 65536
        AND NEW.result_bytes = length(CAST(NEW.result_text AS BLOB))
        AND EXISTS (
            SELECT 1
            FROM task_submissions AS submission
            JOIN task_artifacts AS artifact
              ON artifact.id = NEW.artifact_id
             AND artifact.submission_id = submission.id
            WHERE submission.id = NEW.submission_id
              AND submission.task_id = NEW.task_id
              AND submission.submitted_by_actor_id = NEW.actor_id
              AND artifact.task_id = NEW.task_id
              AND artifact.produced_by_actor_id = NEW.actor_id
        )
    )
    OR (
        NEW.output_delivery_status = 'retained'
        AND NEW.status = 'succeeded'
        AND NEW.output_delivery_error_code IS NOT NULL
        AND NEW.submission_id IS NULL
        AND NEW.artifact_id IS NULL
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
        AND NEW.result_bytes BETWEEN 1 AND 65536
        AND NEW.result_bytes = length(CAST(NEW.result_text AS BLOB))
    )
) IS NOT TRUE
BEGIN
    SELECT RAISE(ABORT, 'AGENT_RUN_OUTPUT_DELIVERY_INVALID');
END;

-- Once a bounded result is staged, it is a durable recovery fact. No caller
-- may discard or rewrite it in place. A pending row may only be observed with
-- exactly the same values, or atomically consume that exact staged result into
-- one of the two legal successful dispositions. The general state trigger
-- above independently validates the submitted/retained target combination.
CREATE TRIGGER trg_agent_runs_output_delivery_pending_immutable
BEFORE UPDATE ON agent_runs
WHEN OLD.output_delivery_status = 'pending'
 AND (
    (
        NEW.id IS OLD.id
        AND NEW.task_id IS OLD.task_id
        AND NEW.assignment_id IS OLD.assignment_id
        AND NEW.actor_id IS OLD.actor_id
        AND NEW.adapter_id IS OLD.adapter_id
        AND NEW.created_by_actor_id IS OLD.created_by_actor_id
        AND NEW.parent_run_id IS OLD.parent_run_id
        AND NEW.attempt IS OLD.attempt
        AND NEW.status IS OLD.status
        AND NEW.provider_id IS OLD.provider_id
        AND NEW.model IS OLD.model
        AND NEW.input_snapshot_json IS OLD.input_snapshot_json
        AND NEW.result_text IS OLD.result_text
        AND NEW.result_bytes IS OLD.result_bytes
        AND NEW.error_code IS OLD.error_code
        AND NEW.started_at IS OLD.started_at
        AND NEW.completed_at IS OLD.completed_at
        AND NEW.created_at IS OLD.created_at
        AND NEW.task_version IS OLD.task_version
        AND NEW.assignment_assigned_at IS OLD.assignment_assigned_at
        AND NEW.actor_version IS OLD.actor_version
        AND NEW.adapter_version IS OLD.adapter_version
        AND NEW.provider_version IS OLD.provider_version
        AND NEW.provider_config_version IS OLD.provider_config_version
        AND NEW.execution_contract_version IS OLD.execution_contract_version
        AND NEW.output_delivery_status IS OLD.output_delivery_status
        AND NEW.output_delivery_error_code IS OLD.output_delivery_error_code
        AND NEW.submission_id IS OLD.submission_id
        AND NEW.artifact_id IS OLD.artifact_id
        AND NEW.output_delivery_pending_text IS OLD.output_delivery_pending_text
        AND NEW.output_delivery_pending_bytes IS OLD.output_delivery_pending_bytes
        AND NEW.output_delivery_pending_completed_at IS OLD.output_delivery_pending_completed_at
    )
    OR (
        NEW.id IS OLD.id
        AND NEW.task_id IS OLD.task_id
        AND NEW.assignment_id IS OLD.assignment_id
        AND NEW.actor_id IS OLD.actor_id
        AND NEW.adapter_id IS OLD.adapter_id
        AND NEW.created_by_actor_id IS OLD.created_by_actor_id
        AND NEW.parent_run_id IS OLD.parent_run_id
        AND NEW.attempt IS OLD.attempt
        AND NEW.status = 'succeeded'
        AND NEW.provider_id IS OLD.provider_id
        AND NEW.model IS OLD.model
        AND NEW.input_snapshot_json IS OLD.input_snapshot_json
        AND NEW.result_text IS OLD.output_delivery_pending_text
        AND NEW.result_bytes IS OLD.output_delivery_pending_bytes
        AND NEW.error_code IS NULL
        AND NEW.started_at IS OLD.started_at
        AND NEW.completed_at IS OLD.output_delivery_pending_completed_at
        AND NEW.created_at IS OLD.created_at
        AND NEW.task_version IS OLD.task_version
        AND NEW.assignment_assigned_at IS OLD.assignment_assigned_at
        AND NEW.actor_version IS OLD.actor_version
        AND NEW.adapter_version IS OLD.adapter_version
        AND NEW.provider_version IS OLD.provider_version
        AND NEW.provider_config_version IS OLD.provider_config_version
        AND NEW.execution_contract_version IS OLD.execution_contract_version
        AND NEW.output_delivery_status IN ('submitted', 'retained')
        AND NEW.output_delivery_pending_text IS NULL
        AND NEW.output_delivery_pending_bytes IS NULL
        AND NEW.output_delivery_pending_completed_at IS NULL
    )
) IS NOT TRUE
BEGIN
    SELECT RAISE(ABORT, 'AGENT_RUN_OUTPUT_DELIVERY_PENDING_IMMUTABLE');
END;

CREATE TRIGGER trg_agent_runs_output_delivery_terminal_immutable
BEFORE UPDATE ON agent_runs
WHEN OLD.status IN ('succeeded', 'failed', 'cancelled', 'interrupted')
 AND NOT (
     NEW.id IS OLD.id
     AND NEW.task_id IS OLD.task_id
     AND NEW.assignment_id IS OLD.assignment_id
     AND NEW.actor_id IS OLD.actor_id
     AND NEW.adapter_id IS OLD.adapter_id
     AND NEW.created_by_actor_id IS OLD.created_by_actor_id
     AND (
         NEW.parent_run_id IS OLD.parent_run_id
         OR (
             OLD.parent_run_id IS NOT NULL
             AND NEW.parent_run_id IS NULL
             AND NOT EXISTS (
                 SELECT 1 FROM agent_runs AS parent
                 WHERE parent.id = OLD.parent_run_id
             )
         )
     )
     AND NEW.attempt IS OLD.attempt
     AND NEW.status IS OLD.status
     AND NEW.provider_id IS OLD.provider_id
     AND NEW.model IS OLD.model
     AND NEW.input_snapshot_json IS OLD.input_snapshot_json
     AND NEW.result_text IS OLD.result_text
     AND NEW.result_bytes IS OLD.result_bytes
     AND NEW.error_code IS OLD.error_code
     AND NEW.started_at IS OLD.started_at
     AND NEW.completed_at IS OLD.completed_at
     AND NEW.created_at IS OLD.created_at
     AND NEW.task_version IS OLD.task_version
     AND NEW.assignment_assigned_at IS OLD.assignment_assigned_at
     AND NEW.actor_version IS OLD.actor_version
     AND NEW.adapter_version IS OLD.adapter_version
     AND NEW.provider_version IS OLD.provider_version
     AND NEW.provider_config_version IS OLD.provider_config_version
     AND NEW.execution_contract_version IS OLD.execution_contract_version
     AND NEW.output_delivery_status IS OLD.output_delivery_status
     AND NEW.output_delivery_error_code IS OLD.output_delivery_error_code
     AND NEW.submission_id IS OLD.submission_id
     AND NEW.artifact_id IS OLD.artifact_id
     AND NEW.output_delivery_pending_text IS OLD.output_delivery_pending_text
     AND NEW.output_delivery_pending_bytes IS OLD.output_delivery_pending_bytes
     AND NEW.output_delivery_pending_completed_at IS OLD.output_delivery_pending_completed_at
 )
BEGIN
    SELECT RAISE(ABORT, 'AGENT_RUN_OUTPUT_DELIVERY_IMMUTABLE');
END;
