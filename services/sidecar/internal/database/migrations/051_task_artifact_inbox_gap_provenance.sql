-- migration: destructive

CREATE UNIQUE INDEX ux_workflow_events_task_artifact_inbox_gap
ON workflow_events(artifact_id, action)
WHERE action = 'migration_task_artifact_inbox_gap';

-- Migration 023 intentionally did not invent Inbox Items for existing
-- requires-followup Artifacts. Record the exact pre-projection Artifacts once
-- so later business exports can distinguish that legitimate historical gap
-- from a package whose Inbox deletion evidence was removed.
INSERT INTO workflow_events(
    id,
    aggregate_type,
    aggregate_id,
    action,
    actor_id,
    assignment_id,
    submission_id,
    artifact_id,
    agent_run_id,
    request_id,
    command_seq,
    previous_json,
    current_json,
    created_at
)
SELECT
    CASE
        WHEN length(task_artifacts.id) = 36
          AND substr(task_artifacts.id, 9, 1) = '-'
          AND substr(task_artifacts.id, 14, 1) = '-'
          AND substr(task_artifacts.id, 19, 1) = '-'
          AND substr(task_artifacts.id, 24, 1) = '-'
        THEN substr(task_artifacts.id, 1, 14) || '6' || substr(task_artifacts.id, 16)
        ELSE printf('00000000-0000-6002-8000-%012x', task_artifacts.rowid)
    END,
    'task',
    task_artifacts.task_id,
    'migration_task_artifact_inbox_gap',
    '00000000-0000-5000-8000-000000000001',
    NULL,
    task_artifacts.submission_id,
    task_artifacts.id,
    NULL,
    NULL,
    NULL,
    NULL,
    json_object(
        'source', 'schema_v51_migration',
        'artifact_id', task_artifacts.id,
        'task_id', task_artifacts.task_id,
        'submission_id', task_artifacts.submission_id,
        'artifact_created_at', task_artifacts.created_at,
        'requires_followup', 1
    ),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM task_artifacts
JOIN schema_migrations AS projection_migration
    ON projection_migration.version = 23
   AND projection_migration.name = '023_task_artifact_inbox_projection.sql'
WHERE task_artifacts.requires_followup = 1
  -- schema_migrations.applied_at was written by CURRENT_TIMESTAMP at whole-second
  -- precision. Treat every RFC3339Nano Artifact in that recorded UTC second as a
  -- pre-projection candidate; SQLite's date functions can round its fraction up.
  AND (
      julianday(task_artifacts.created_at) <= julianday(projection_migration.applied_at)
      OR substr(replace(task_artifacts.created_at, 'T', ' '), 1, 19)
         = substr(replace(projection_migration.applied_at, 'T', ' '), 1, 19)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM inbox_items
      WHERE inbox_items.source_entity_type = 'task_artifact'
        AND inbox_items.source_entity_id = task_artifacts.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM workflow_events
      WHERE workflow_events.action = 'migration_task_artifact_inbox_gap'
        AND workflow_events.artifact_id = task_artifacts.id
  );
