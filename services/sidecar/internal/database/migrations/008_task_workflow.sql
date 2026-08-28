-- migration: foreign_keys=off
-- Rebuilding tasks requires foreign_keys to be disabled before the transaction.
-- The migration runner owns that connection-scoped protocol and validates the
-- rebuilt graph with PRAGMA foreign_key_check before committing.

CREATE TABLE tasks_v8 (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 2 AND 200),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'blocked', 'waiting_review', 'done', 'cancelled')),
    priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0', 'P1', 'P2', 'P3')),
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    due_date TEXT,
    planned_date TEXT,
    estimated_minutes INTEGER CHECK (estimated_minutes IS NULL OR estimated_minutes >= 0),
    actual_minutes INTEGER NOT NULL DEFAULT 0 CHECK (actual_minutes >= 0),
    manual_order INTEGER,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT CHECK (completed_at IS NULL OR length(completed_at) > 0),
    kind TEXT NOT NULL DEFAULT 'work'
        CHECK (kind IN ('work', 'review', 'followup', 'reminder')),
    parent_task_id TEXT REFERENCES tasks_v8(id) ON DELETE SET NULL
        CHECK (parent_task_id IS NULL OR parent_task_id <> id),
    completion_criteria TEXT NOT NULL DEFAULT ''
        CHECK (length(completion_criteria) <= 10000),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    review_policy TEXT NOT NULL DEFAULT 'none'
        CHECK (review_policy IN ('none', 'manual')),
    blocked_reason TEXT
        CHECK (blocked_reason IS NULL OR length(trim(blocked_reason)) > 0),
    blocked_at TEXT CHECK (blocked_at IS NULL OR length(blocked_at) > 0),
    blocked_from_status TEXT
        CHECK (blocked_from_status IS NULL OR blocked_from_status IN ('todo', 'in_progress', 'waiting_review')),
    submitted_at TEXT CHECK (submitted_at IS NULL OR length(submitted_at) > 0),
    reviewed_at TEXT CHECK (reviewed_at IS NULL OR length(reviewed_at) > 0),
    CHECK (
        (
            status = 'blocked'
            AND blocked_reason IS NOT NULL
            AND blocked_at IS NOT NULL
            AND blocked_from_status IS NOT NULL
        )
        OR (
            status <> 'blocked'
            AND blocked_reason IS NULL
            AND blocked_at IS NULL
            AND blocked_from_status IS NULL
        )
    ),
    CHECK (
        (status = 'done' AND completed_at IS NOT NULL)
        OR (status <> 'done' AND completed_at IS NULL)
    ),
    CHECK (reviewed_at IS NULL OR submitted_at IS NOT NULL),
    CHECK (
        review_policy <> 'none'
        OR (submitted_at IS NULL AND reviewed_at IS NULL)
    ),
    CHECK (
        status <> 'waiting_review'
        OR (
            review_policy = 'manual'
            AND submitted_at IS NOT NULL
            AND reviewed_at IS NULL
        )
    ),
    CHECK (
        status <> 'done'
        OR review_policy <> 'manual'
        OR (submitted_at IS NOT NULL AND reviewed_at IS NOT NULL)
    )
);

INSERT INTO tasks_v8 (
    id,
    title,
    description,
    status,
    priority,
    project_id,
    due_date,
    planned_date,
    estimated_minutes,
    actual_minutes,
    manual_order,
    created_at,
    updated_at,
    completed_at,
    kind,
    parent_task_id,
    completion_criteria,
    version,
    review_policy,
    blocked_reason,
    blocked_at,
    blocked_from_status,
    submitted_at,
    reviewed_at
)
SELECT
    id,
    title,
    description,
    status,
    priority,
    project_id,
    due_date,
    planned_date,
    estimated_minutes,
    actual_minutes,
    manual_order,
    created_at,
    updated_at,
    CASE
        WHEN status = 'done' THEN COALESCE(
            NULLIF(completed_at, ''),
            NULLIF(updated_at, ''),
            NULLIF(created_at, ''),
            STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
        )
        ELSE NULL
    END,
    kind,
    parent_task_id,
    completion_criteria,
    version,
    'none',
    NULL,
    NULL,
    NULL,
    NULL,
    NULL
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_v8 RENAME TO tasks;

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_priority ON tasks(priority);
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_planned_date ON tasks(planned_date);
CREATE INDEX idx_tasks_due_date ON tasks(due_date);
CREATE INDEX idx_tasks_manual_order ON tasks(manual_order);
CREATE INDEX idx_tasks_kind ON tasks(kind);
CREATE INDEX idx_tasks_parent_task_id ON tasks(parent_task_id);
CREATE INDEX idx_tasks_planned_manual_order ON tasks(planned_date, manual_order, id);

CREATE TRIGGER projects_version_after_task_insert
AFTER INSERT ON tasks
WHEN NEW.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id;
END;

CREATE TRIGGER projects_version_after_task_update
AFTER UPDATE OF project_id, status, actual_minutes ON tasks
WHEN OLD.project_id IS NOT NEW.project_id
  OR OLD.status <> NEW.status
  OR OLD.actual_minutes <> NEW.actual_minutes
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;

    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id
      AND (OLD.project_id IS NULL OR NEW.project_id <> OLD.project_id);
END;

CREATE TRIGGER projects_version_after_task_delete
AFTER DELETE ON tasks
WHEN OLD.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;
END;

CREATE TRIGGER trg_tasks_parent_cycle_insert
BEFORE INSERT ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN NEW.parent_task_id = NEW.id THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
        WHEN EXISTS (
            WITH RECURSIVE ancestors(id, parent_task_id) AS (
                SELECT id, parent_task_id
                FROM tasks
                WHERE id = NEW.parent_task_id
                UNION
                SELECT tasks.id, tasks.parent_task_id
                FROM tasks
                JOIN ancestors ON tasks.id = ancestors.parent_task_id
            )
            SELECT 1 FROM ancestors WHERE id = NEW.id
        ) THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
    END;
END;

CREATE TRIGGER trg_tasks_parent_cycle_update
BEFORE UPDATE OF parent_task_id ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN NEW.parent_task_id = NEW.id THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
        WHEN EXISTS (
            WITH RECURSIVE ancestors(id, parent_task_id) AS (
                SELECT id, parent_task_id
                FROM tasks
                WHERE id = NEW.parent_task_id
                UNION
                SELECT tasks.id, tasks.parent_task_id
                FROM tasks
                JOIN ancestors ON tasks.id = ancestors.parent_task_id
            )
            SELECT 1 FROM ancestors WHERE id = NEW.id
        ) THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
    END;
END;

CREATE TRIGGER trg_tasks_parent_after_insert
AFTER INSERT ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_delete
AFTER DELETE ON tasks
WHEN OLD.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = OLD.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_status_update
AFTER UPDATE OF status ON tasks
WHEN OLD.status IS NOT NEW.status AND NEW.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_parent_update
AFTER UPDATE OF parent_task_id ON tasks
WHEN OLD.parent_task_id IS NOT NEW.parent_task_id
BEGIN
    -- API writes increment the edited task version in the same statement. Foreign-key
    -- ON DELETE SET NULL does not, so invalidate the detached child here exactly once.
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = NEW.id AND NEW.version = OLD.version;

    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE OLD.parent_task_id IS NOT NULL AND id = OLD.parent_task_id;

    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE NEW.parent_task_id IS NOT NULL AND id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_title_update
AFTER UPDATE OF title ON tasks
WHEN OLD.title IS NOT NEW.title
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE parent_task_id = NEW.id;
END;

CREATE TRIGGER trg_tasks_status_transition_update
BEFORE UPDATE OF status ON tasks
WHEN OLD.status IS NOT NEW.status
  AND NOT (
      (OLD.status = 'todo' AND NEW.status IN ('in_progress', 'blocked', 'waiting_review', 'done', 'cancelled'))
      OR (OLD.status = 'in_progress' AND NEW.status IN ('blocked', 'waiting_review', 'done', 'cancelled'))
      OR (OLD.status = 'waiting_review' AND NEW.status IN ('in_progress', 'blocked', 'done', 'cancelled'))
      OR (OLD.status = 'blocked' AND NEW.status = OLD.blocked_from_status)
      OR (OLD.status = 'blocked' AND NEW.status = 'cancelled')
      OR (OLD.status IN ('done', 'cancelled') AND NEW.status = 'todo')
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_TRANSITION_NOT_ALLOWED');
END;

CREATE TRIGGER trg_tasks_terminal_requires_no_active_assignments
BEFORE UPDATE OF status ON tasks
WHEN OLD.status IS NOT NEW.status
  AND NEW.status IN ('done', 'cancelled')
  AND EXISTS (
      SELECT 1
      FROM task_assignments
      WHERE task_id = OLD.id AND unassigned_at IS NULL
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_HAS_ACTIVE_ASSIGNMENTS');
END;

CREATE TRIGGER trg_task_assignments_reject_terminal_insert
BEFORE INSERT ON task_assignments
WHEN EXISTS (
    SELECT 1
    FROM tasks
    WHERE id = NEW.task_id AND status IN ('done', 'cancelled')
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_NOT_ASSIGNABLE');
END;

ALTER TABLE workflow_events
ADD COLUMN command_seq INTEGER
CHECK (command_seq IS NULL OR command_seq > 0);

DROP INDEX idx_workflow_events_aggregate_timeline;
CREATE INDEX idx_workflow_events_aggregate_timeline
ON workflow_events(aggregate_type, aggregate_id, created_at, command_seq, id);

CREATE TRIGGER trg_workflow_events_immutable_update
BEFORE UPDATE ON workflow_events
WHEN NOT (
    OLD.assignment_id IS NOT NULL
    AND NEW.assignment_id IS NULL
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
