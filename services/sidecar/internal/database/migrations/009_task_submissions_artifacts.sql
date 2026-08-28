-- migration: foreign_keys=off
-- Submission and Artifact introduce a circular aggregate reference:
-- task_submissions belong to tasks, while tasks point at their current
-- submission. The migration runner owns the connection-scoped foreign-key
-- suspension and validates the completed graph before committing.

CREATE TABLE workspace_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    database_id TEXT NOT NULL UNIQUE
        CHECK (
            length(database_id) = 36
            AND database_id = lower(database_id)
            AND substr(database_id, 9, 1) = '-'
            AND substr(database_id, 14, 1) = '-'
            AND substr(database_id, 19, 1) = '-'
            AND substr(database_id, 24, 1) = '-'
            AND database_id NOT GLOB '*[^0-9a-f-]*'
        ),
    artifact_store_id TEXT UNIQUE
        CHECK (
            artifact_store_id IS NULL
            OR (
                length(artifact_store_id) = 36
                AND artifact_store_id = lower(artifact_store_id)
                AND substr(artifact_store_id, 9, 1) = '-'
                AND substr(artifact_store_id, 14, 1) = '-'
                AND substr(artifact_store_id, 19, 1) = '-'
                AND substr(artifact_store_id, 24, 1) = '-'
                AND artifact_store_id NOT GLOB '*[^0-9a-f-]*'
            )
        ),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0)
);

INSERT INTO workspace_identity(singleton, database_id)
VALUES (
    1,
    lower(hex(randomblob(4))) || '-'
        || lower(hex(randomblob(2))) || '-4'
        || substr(lower(hex(randomblob(2))), 2) || '-8'
        || substr(lower(hex(randomblob(2))), 2) || '-'
        || lower(hex(randomblob(6)))
);

CREATE TRIGGER trg_workspace_identity_immutable_update
BEFORE UPDATE ON workspace_identity
WHEN NOT (
    OLD.singleton = NEW.singleton
    AND OLD.database_id = NEW.database_id
    AND OLD.created_at = NEW.created_at
    AND OLD.artifact_store_id IS NULL
    AND NEW.artifact_store_id IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_workspace_identity_immutable_delete
BEFORE DELETE ON workspace_identity
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_IDENTITY_IMMUTABLE');
END;

CREATE TABLE task_submissions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    status TEXT NOT NULL
        CHECK (status IN ('pending_review', 'accepted', 'changes_requested', 'withdrawn')),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 10000),
    submitted_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    submitted_at TEXT NOT NULL CHECK (length(submitted_at) > 0),
    reviewed_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    reviewed_at TEXT CHECK (reviewed_at IS NULL OR length(reviewed_at) > 0),
    review_reason TEXT
        CHECK (review_reason IS NULL OR length(trim(review_reason)) BETWEEN 1 AND 10000),
    withdrawn_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    withdrawn_at TEXT CHECK (withdrawn_at IS NULL OR length(withdrawn_at) > 0),
    is_inferred INTEGER NOT NULL DEFAULT 0 CHECK (is_inferred IN (0, 1)),
    CHECK (
        (
            status = 'pending_review'
            AND reviewed_by_actor_id IS NULL
            AND reviewed_at IS NULL
            AND review_reason IS NULL
            AND withdrawn_by_actor_id IS NULL
            AND withdrawn_at IS NULL
        )
        OR (
            status = 'accepted'
            AND reviewed_by_actor_id IS NOT NULL
            AND reviewed_at IS NOT NULL
            AND withdrawn_by_actor_id IS NULL
            AND withdrawn_at IS NULL
        )
        OR (
            status = 'changes_requested'
            AND reviewed_by_actor_id IS NOT NULL
            AND reviewed_at IS NOT NULL
            AND review_reason IS NOT NULL
            AND withdrawn_by_actor_id IS NULL
            AND withdrawn_at IS NULL
        )
        OR (
            status = 'withdrawn'
            AND reviewed_by_actor_id IS NULL
            AND reviewed_at IS NULL
            AND review_reason IS NULL
            AND withdrawn_by_actor_id IS NOT NULL
            AND withdrawn_at IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX ux_task_submissions_task_sequence
ON task_submissions(task_id, sequence);

CREATE UNIQUE INDEX ux_task_submissions_single_pending
ON task_submissions(task_id)
WHERE status = 'pending_review';

CREATE INDEX idx_task_submissions_task_timeline
ON task_submissions(task_id, submitted_at DESC, sequence DESC, id);

CREATE INDEX idx_task_submissions_submitted_by_actor
ON task_submissions(submitted_by_actor_id, submitted_at DESC, id);

CREATE INDEX idx_task_submissions_reviewed_by_actor
ON task_submissions(reviewed_by_actor_id, reviewed_at DESC, id)
WHERE reviewed_by_actor_id IS NOT NULL;

CREATE INDEX idx_task_submissions_withdrawn_by_actor
ON task_submissions(withdrawn_by_actor_id, withdrawn_at DESC, id)
WHERE withdrawn_by_actor_id IS NOT NULL;

CREATE TRIGGER trg_task_submissions_protect_history_update
BEFORE UPDATE ON task_submissions
WHEN NEW.id IS NOT OLD.id
    OR NEW.task_id IS NOT OLD.task_id
    OR NEW.sequence IS NOT OLD.sequence
    OR NEW.summary IS NOT OLD.summary
    OR NEW.submitted_by_actor_id IS NOT OLD.submitted_by_actor_id
    OR NEW.submitted_at IS NOT OLD.submitted_at
    OR NEW.is_inferred IS NOT OLD.is_inferred
    OR OLD.status <> 'pending_review'
    OR NEW.status NOT IN ('accepted', 'changes_requested', 'withdrawn')
BEGIN
    SELECT RAISE(ABORT, 'TASK_SUBMISSION_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER trg_task_submissions_protect_member_delete
BEFORE DELETE ON task_submissions
WHEN EXISTS (
    SELECT 1
    FROM tasks
    WHERE id = OLD.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_SUBMISSION_HARD_DELETE_FORBIDDEN');
END;

ALTER TABLE tasks
ADD COLUMN current_submission_id TEXT
REFERENCES task_submissions(id) ON DELETE SET NULL;

CREATE INDEX idx_tasks_current_submission_id
ON tasks(current_submission_id)
WHERE current_submission_id IS NOT NULL;

CREATE TRIGGER trg_tasks_current_submission_same_task_insert
BEFORE INSERT ON tasks
WHEN NEW.current_submission_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM task_submissions
      WHERE id = NEW.current_submission_id
        AND task_id = NEW.id
        AND sequence = (
            SELECT MAX(latest.sequence)
            FROM task_submissions AS latest
            WHERE latest.task_id = NEW.id
        )
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_CURRENT_SUBMISSION_MISMATCH');
END;

CREATE TRIGGER trg_tasks_current_submission_same_task_update
BEFORE UPDATE OF current_submission_id ON tasks
WHEN NEW.current_submission_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM task_submissions
      WHERE id = NEW.current_submission_id
        AND task_id = NEW.id
        AND sequence = (
            SELECT MAX(latest.sequence)
            FROM task_submissions AS latest
            WHERE latest.task_id = NEW.id
        )
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_CURRENT_SUBMISSION_MISMATCH');
END;

CREATE TABLE task_artifacts (
    id TEXT PRIMARY KEY
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND id NOT GLOB '*[^0-9a-f-]*'
        ),
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    submission_id TEXT NOT NULL REFERENCES task_submissions(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position > 0),
    storage_kind TEXT NOT NULL CHECK (storage_kind IN ('text', 'file', 'link', 'structured')),
    name TEXT NOT NULL
        CHECK (name = trim(name) AND length(name) BETWEEN 1 AND 255),
    content_text TEXT,
    reference_url TEXT,
    structured_json TEXT
        CHECK (
            structured_json IS NULL
            OR (json_valid(structured_json) AND json_type(structured_json) = 'object')
        ),
    relative_path TEXT,
    mime_type TEXT,
    size_bytes INTEGER CHECK (size_bytes IS NULL OR size_bytes > 0),
    sha256 TEXT
        CHECK (
            sha256 IS NULL
            OR (
                length(sha256) = 64
                AND sha256 = lower(sha256)
                AND sha256 NOT GLOB '*[^0-9a-f]*'
            )
        ),
    requires_followup INTEGER NOT NULL DEFAULT 0 CHECK (requires_followup IN (0, 1)),
    produced_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    recorded_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    integrity_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (integrity_status IN ('unverified', 'verified', 'missing', 'mismatch')),
    integrity_checked_at TEXT
        CHECK (integrity_checked_at IS NULL OR length(integrity_checked_at) > 0),
    deleted_at TEXT CHECK (deleted_at IS NULL OR length(deleted_at) > 0),
    deleted_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    delete_reason TEXT
        CHECK (delete_reason IS NULL OR length(trim(delete_reason)) BETWEEN 1 AND 1000),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0),
    CHECK (
        (
            storage_kind = 'text'
            AND content_text IS NOT NULL
            AND length(trim(content_text)) > 0
            AND reference_url IS NULL
            AND structured_json IS NULL
            AND relative_path IS NULL
            AND mime_type IS NULL
            AND size_bytes IS NULL
            AND sha256 IS NULL
        )
        OR (
            storage_kind = 'link'
            AND content_text IS NULL
            AND reference_url IS NOT NULL
            AND length(trim(reference_url)) > 0
            AND structured_json IS NULL
            AND relative_path IS NULL
            AND mime_type IS NULL
            AND size_bytes IS NULL
            AND sha256 IS NULL
        )
        OR (
            storage_kind = 'structured'
            AND content_text IS NULL
            AND reference_url IS NULL
            AND structured_json IS NOT NULL
            AND relative_path IS NULL
            AND mime_type IS NULL
            AND size_bytes IS NULL
            AND sha256 IS NULL
        )
        OR (
            storage_kind = 'file'
            AND content_text IS NULL
            AND reference_url IS NULL
            AND structured_json IS NULL
            AND relative_path IS NOT NULL
            AND length(trim(relative_path)) > 0
            AND relative_path = trim(relative_path)
            AND relative_path NOT LIKE '/%'
            AND instr(relative_path, char(92)) = 0
            AND relative_path NOT GLOB '[A-Za-z]:*'
            AND relative_path <> '..'
            AND relative_path NOT LIKE '../%'
            AND relative_path NOT LIKE '%/../%'
            AND relative_path NOT LIKE '%/..'
            AND relative_path = 'objects/' || id
            AND mime_type IS NOT NULL
            AND length(trim(mime_type)) > 0
            AND size_bytes IS NOT NULL
            AND sha256 IS NOT NULL
        )
    ),
    CHECK (
        (integrity_status = 'unverified' AND integrity_checked_at IS NULL)
        OR (integrity_status <> 'unverified' AND integrity_checked_at IS NOT NULL)
    ),
    CHECK (
        (
            deleted_at IS NULL
            AND deleted_by_actor_id IS NULL
            AND delete_reason IS NULL
        )
        OR (
            deleted_at IS NOT NULL
            AND deleted_by_actor_id IS NOT NULL
            AND delete_reason IS NOT NULL
        )
    )
);

-- Tombstones deliberately do not reference tasks or task_artifacts: they must
-- survive aggregate deletion so startup recovery can distinguish an authorized
-- file deletion from an unknown object belonging to another database snapshot.
CREATE TABLE artifact_deletion_tombstones (
    artifact_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL CHECK (length(task_id) > 0),
    relative_path TEXT NOT NULL UNIQUE
        CHECK (relative_path = 'objects/' || artifact_id),
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    sha256 TEXT NOT NULL
        CHECK (
            length(sha256) = 64
            AND sha256 = lower(sha256)
            AND sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    deletion_scope TEXT NOT NULL CHECK (deletion_scope IN ('artifact', 'task')),
    deleted_at TEXT NOT NULL CHECK (length(deleted_at) > 0)
);

CREATE INDEX idx_artifact_deletion_tombstones_task
ON artifact_deletion_tombstones(task_id, deleted_at DESC, artifact_id);

CREATE TRIGGER trg_artifact_deletion_tombstones_immutable_update
BEFORE UPDATE ON artifact_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'ARTIFACT_DELETION_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER trg_artifact_deletion_tombstones_immutable_delete
BEFORE DELETE ON artifact_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'ARTIFACT_DELETION_TOMBSTONE_IMMUTABLE');
END;

CREATE UNIQUE INDEX ux_task_artifacts_submission_position
ON task_artifacts(submission_id, position);

CREATE INDEX idx_task_artifacts_task_timeline
ON task_artifacts(task_id, created_at DESC, position, id);

CREATE INDEX idx_task_artifacts_submission
ON task_artifacts(submission_id, position, id);

CREATE INDEX idx_task_artifacts_produced_by_actor
ON task_artifacts(produced_by_actor_id, created_at DESC, id);

CREATE INDEX idx_task_artifacts_recorded_by_actor
ON task_artifacts(recorded_by_actor_id, created_at DESC, id);

CREATE INDEX idx_task_artifacts_followup
ON task_artifacts(task_id, requires_followup, created_at DESC, id)
WHERE requires_followup = 1 AND deleted_at IS NULL;

CREATE INDEX idx_task_artifacts_integrity
ON task_artifacts(integrity_status, created_at DESC, id)
WHERE deleted_at IS NULL;

CREATE TRIGGER trg_task_artifacts_submission_same_task_insert
BEFORE INSERT ON task_artifacts
WHEN NOT EXISTS (
    SELECT 1
    FROM task_submissions
    WHERE id = NEW.submission_id
      AND task_id = NEW.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_SUBMISSION_MISMATCH');
END;

CREATE TRIGGER trg_task_artifacts_submission_same_task_update
BEFORE UPDATE OF task_id, submission_id ON task_artifacts
WHEN NOT EXISTS (
    SELECT 1
    FROM task_submissions
    WHERE id = NEW.submission_id
      AND task_id = NEW.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_SUBMISSION_MISMATCH');
END;

CREATE TRIGGER trg_task_artifacts_protect_facts_update
BEFORE UPDATE ON task_artifacts
WHEN NEW.id IS NOT OLD.id
    OR NEW.task_id IS NOT OLD.task_id
    OR NEW.submission_id IS NOT OLD.submission_id
    OR NEW.position IS NOT OLD.position
    OR NEW.storage_kind IS NOT OLD.storage_kind
    OR NEW.name IS NOT OLD.name
    OR NEW.content_text IS NOT OLD.content_text
    OR NEW.reference_url IS NOT OLD.reference_url
    OR NEW.structured_json IS NOT OLD.structured_json
    OR NEW.relative_path IS NOT OLD.relative_path
    OR NEW.mime_type IS NOT OLD.mime_type
    OR NEW.size_bytes IS NOT OLD.size_bytes
    OR NEW.sha256 IS NOT OLD.sha256
    OR NEW.requires_followup IS NOT OLD.requires_followup
    OR NEW.produced_by_actor_id IS NOT OLD.produced_by_actor_id
    OR NEW.recorded_by_actor_id IS NOT OLD.recorded_by_actor_id
    OR NEW.created_at IS NOT OLD.created_at
    OR (
        OLD.deleted_at IS NOT NULL
        AND (
            NEW.deleted_at IS NOT OLD.deleted_at
            OR NEW.deleted_by_actor_id IS NOT OLD.deleted_by_actor_id
            OR NEW.delete_reason IS NOT OLD.delete_reason
        )
    )
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_FACTS_IMMUTABLE');
END;

CREATE TRIGGER trg_task_artifacts_protect_member_delete
BEFORE DELETE ON task_artifacts
WHEN EXISTS (
    SELECT 1
    FROM tasks
    WHERE id = OLD.task_id
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_HARD_DELETE_FORBIDDEN');
END;

DROP TRIGGER trg_workflow_events_immutable_update;
DROP TRIGGER trg_workflow_events_immutable_delete;

ALTER TABLE workflow_events
ADD COLUMN submission_id TEXT
REFERENCES task_submissions(id) ON DELETE SET NULL;

ALTER TABLE workflow_events
ADD COLUMN artifact_id TEXT
REFERENCES task_artifacts(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX ux_workflow_events_submission_backfill
ON workflow_events(aggregate_type, aggregate_id, action)
WHERE action = 'migration_submission_backfill';

CREATE INDEX idx_workflow_events_submission
ON workflow_events(submission_id, created_at, id);

CREATE INDEX idx_workflow_events_artifact
ON workflow_events(artifact_id, created_at, id);

CREATE TRIGGER trg_workflow_events_submission_matches_task_insert
BEFORE INSERT ON workflow_events
WHEN NEW.submission_id IS NOT NULL
  AND (
      NEW.aggregate_type <> 'task'
      OR NOT EXISTS (
          SELECT 1
          FROM task_submissions
          WHERE id = NEW.submission_id
            AND task_id = NEW.aggregate_id
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'WORKFLOW_EVENT_SUBMISSION_MISMATCH');
END;

CREATE TRIGGER trg_workflow_events_artifact_matches_task_insert
BEFORE INSERT ON workflow_events
WHEN NEW.artifact_id IS NOT NULL
  AND (
      NEW.aggregate_type <> 'task'
      OR NOT EXISTS (
          SELECT 1
          FROM task_artifacts
          WHERE id = NEW.artifact_id
            AND task_id = NEW.aggregate_id
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'WORKFLOW_EVENT_ARTIFACT_MISMATCH');
END;

CREATE TRIGGER trg_workflow_events_artifact_matches_submission_insert
BEFORE INSERT ON workflow_events
WHEN NEW.submission_id IS NOT NULL
  AND NEW.artifact_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM task_artifacts
      WHERE id = NEW.artifact_id
        AND submission_id = NEW.submission_id
  )
BEGIN
    SELECT RAISE(ABORT, 'WORKFLOW_EVENT_ARTIFACT_SUBMISSION_MISMATCH');
END;

CREATE TRIGGER trg_workflow_events_immutable_update
BEFORE UPDATE ON workflow_events
WHEN NOT (
    (
        (
            OLD.assignment_id IS NOT NULL
            AND NEW.assignment_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_assignments
                WHERE id = OLD.assignment_id
            )
        )
        OR (
            OLD.submission_id IS NOT NULL
            AND NEW.submission_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_submissions
                WHERE id = OLD.submission_id
            )
        )
        OR (
            OLD.artifact_id IS NOT NULL
            AND NEW.artifact_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_artifacts
                WHERE id = OLD.artifact_id
            )
        )
    )
    AND (
        NEW.assignment_id IS OLD.assignment_id
        OR (
            OLD.assignment_id IS NOT NULL
            AND NEW.assignment_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_assignments
                WHERE id = OLD.assignment_id
            )
        )
    )
    AND (
        NEW.submission_id IS OLD.submission_id
        OR (
            OLD.submission_id IS NOT NULL
            AND NEW.submission_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_submissions
                WHERE id = OLD.submission_id
            )
        )
    )
    AND (
        NEW.artifact_id IS OLD.artifact_id
        OR (
            OLD.artifact_id IS NOT NULL
            AND NEW.artifact_id IS NULL
            AND NOT EXISTS (
                SELECT 1
                FROM task_artifacts
                WHERE id = OLD.artifact_id
            )
        )
    )
    AND NEW.id IS OLD.id
    AND NEW.aggregate_type IS OLD.aggregate_type
    AND NEW.aggregate_id IS OLD.aggregate_id
    AND NEW.action IS OLD.action
    AND NEW.actor_id IS OLD.actor_id
    AND NEW.agent_run_id IS OLD.agent_run_id
    AND NEW.request_id IS OLD.request_id
    AND NEW.previous_json IS OLD.previous_json
    AND NEW.current_json IS OLD.current_json
    AND NEW.created_at IS OLD.created_at
    AND NEW.command_seq IS OLD.command_seq
)
BEGIN
    SELECT RAISE(ABORT, 'WORKFLOW_EVENT_IMMUTABLE');
END;

CREATE TRIGGER trg_workflow_events_immutable_delete
BEFORE DELETE ON workflow_events
BEGIN
    SELECT RAISE(ABORT, 'WORKFLOW_EVENT_IMMUTABLE');
END;

-- Schema v8 already allowed manual-review facts at rest but did not expose the
-- commands that would create them. Convert only unambiguous, legal historical
-- combinations into an inferred first Submission. Do not invent Artifacts.
INSERT INTO task_submissions(
    id,
    task_id,
    sequence,
    status,
    summary,
    submitted_by_actor_id,
    submitted_at,
    reviewed_by_actor_id,
    reviewed_at,
    review_reason,
    withdrawn_by_actor_id,
    withdrawn_at,
    is_inferred
)
SELECT
    lower(hex(randomblob(4))) || '-'
        || lower(hex(randomblob(2))) || '-4'
        || substr(lower(hex(randomblob(2))), 2) || '-8'
        || substr(lower(hex(randomblob(2))), 2) || '-'
        || lower(hex(randomblob(6))),
    tasks.id,
    1,
    CASE
        WHEN tasks.status = 'waiting_review'
            OR (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
        THEN 'pending_review'
        WHEN tasks.status = 'done' THEN 'accepted'
        WHEN tasks.status = 'cancelled' THEN 'withdrawn'
        ELSE 'changes_requested'
    END,
    '',
    '00000000-0000-5000-8000-000000000001',
    tasks.submitted_at,
    CASE
        WHEN tasks.status = 'done'
            OR (
                tasks.reviewed_at IS NOT NULL
                AND tasks.status NOT IN ('waiting_review', 'cancelled')
                AND NOT (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
            )
        THEN '00000000-0000-5000-8000-000000000001'
        ELSE NULL
    END,
    CASE
        WHEN tasks.status = 'done'
            OR (
                tasks.reviewed_at IS NOT NULL
                AND tasks.status NOT IN ('waiting_review', 'cancelled')
                AND NOT (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
            )
        THEN tasks.reviewed_at
        ELSE NULL
    END,
    CASE
        WHEN tasks.reviewed_at IS NOT NULL
            AND tasks.status NOT IN ('waiting_review', 'done', 'cancelled')
            AND NOT (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
        THEN 'schema_v9_migration_inferred_changes_requested'
        ELSE NULL
    END,
    CASE
        WHEN tasks.status = 'cancelled'
        THEN '00000000-0000-5000-8000-000000000001'
        ELSE NULL
    END,
    CASE
        WHEN tasks.status = 'cancelled'
        THEN COALESCE(
            NULLIF(tasks.updated_at, ''),
            NULLIF(tasks.submitted_at, ''),
            STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
        )
        ELSE NULL
    END,
    1
FROM tasks
WHERE tasks.review_policy = 'manual'
  AND tasks.submitted_at IS NOT NULL
  AND (
      tasks.status IN ('waiting_review', 'done', 'cancelled')
      OR (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
      OR (
          tasks.reviewed_at IS NOT NULL
          AND tasks.status IN ('todo', 'in_progress', 'blocked')
          AND NOT (tasks.status = 'blocked' AND tasks.blocked_from_status = 'waiting_review')
      )
  );

UPDATE tasks
SET current_submission_id = (
    SELECT task_submissions.id
    FROM task_submissions
    WHERE task_submissions.task_id = tasks.id
      AND task_submissions.sequence = 1
)
WHERE EXISTS (
    SELECT 1
    FROM task_submissions
    WHERE task_submissions.task_id = tasks.id
      AND task_submissions.sequence = 1
      AND task_submissions.is_inferred = 1
);

INSERT INTO workflow_events(
    id,
    aggregate_type,
    aggregate_id,
    action,
    actor_id,
    submission_id,
    current_json
)
SELECT
    lower(hex(randomblob(4))) || '-'
        || lower(hex(randomblob(2))) || '-4'
        || substr(lower(hex(randomblob(2))), 2) || '-8'
        || substr(lower(hex(randomblob(2))), 2) || '-'
        || lower(hex(randomblob(6))),
    'task',
    tasks.id,
    'migration_submission_backfill',
    '00000000-0000-5000-8000-000000000002',
    task_submissions.id,
    json_object(
        'source', 'schema_v9_migration',
        'inferred', json('true'),
        'submission_id', task_submissions.id,
        'sequence', task_submissions.sequence,
        'status', task_submissions.status
    )
FROM tasks
JOIN task_submissions
  ON task_submissions.task_id = tasks.id
 AND task_submissions.sequence = 1
 AND task_submissions.is_inferred = 1;
