CREATE TABLE IF NOT EXISTS actors (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK (type IN ('owner', 'person', 'system', 'agent')),
    display_name TEXT NOT NULL
        CHECK (display_name = trim(display_name) AND length(display_name) BETWEEN 1 AND 100),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    is_builtin INTEGER NOT NULL DEFAULT 0 CHECK (is_builtin IN (0, 1)),
    notes TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    metadata_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(metadata_json) AND json_type(metadata_json) = 'object'),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(updated_at) > 0),
    CHECK (
        (type IN ('owner', 'system') AND is_builtin = 1 AND status = 'active')
        OR (type IN ('person', 'agent') AND is_builtin = 0)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_actors_single_owner
ON actors(type)
WHERE type = 'owner';

CREATE UNIQUE INDEX IF NOT EXISTS ux_actors_single_system
ON actors(type)
WHERE type = 'system';

CREATE INDEX IF NOT EXISTS idx_actors_status_type_name
ON actors(status, type, display_name COLLATE NOCASE, id);

CREATE TRIGGER IF NOT EXISTS trg_actors_protect_builtin_delete
BEFORE DELETE ON actors
WHEN OLD.is_builtin = 1
BEGIN
    SELECT RAISE(ABORT, 'BUILTIN_ACTOR_DELETE_FORBIDDEN');
END;

CREATE TRIGGER IF NOT EXISTS trg_actors_protect_builtin_identity
BEFORE UPDATE ON actors
WHEN OLD.is_builtin = 1 AND (
    NEW.id IS NOT OLD.id
    OR NEW.type IS NOT OLD.type
    OR NEW.is_builtin IS NOT OLD.is_builtin
    OR NEW.status IS NOT OLD.status
)
BEGIN
    SELECT RAISE(ABORT, 'BUILTIN_ACTOR_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER IF NOT EXISTS trg_actors_owner_profile_scope
BEFORE UPDATE ON actors
WHEN OLD.type = 'owner' AND (
    NEW.notes IS NOT OLD.notes
    OR NEW.metadata_json IS NOT OLD.metadata_json
)
BEGIN
    SELECT RAISE(ABORT, 'OWNER_ACTOR_ONLY_DISPLAY_NAME_EDITABLE');
END;

CREATE TRIGGER IF NOT EXISTS trg_actors_system_immutable
BEFORE UPDATE ON actors
WHEN OLD.type = 'system'
BEGIN
    SELECT RAISE(ABORT, 'SYSTEM_ACTOR_IMMUTABLE');
END;

CREATE TABLE IF NOT EXISTS task_assignments (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    role TEXT NOT NULL CHECK (role IN ('assignee', 'reviewer')),
    assigned_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    assigned_at TEXT NOT NULL CHECK (length(assigned_at) > 0),
    unassigned_at TEXT,
    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 1000),
    CHECK (unassigned_at IS NULL OR length(unassigned_at) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_task_assignments_active_role
ON task_assignments(task_id, role)
WHERE unassigned_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_task_assignments_task_history
ON task_assignments(task_id, role, assigned_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_task_assignments_actor_active
ON task_assignments(actor_id, role, task_id)
WHERE unassigned_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_task_assignments_assigned_by_actor
ON task_assignments(assigned_by_actor_id, assigned_at DESC, id);

CREATE TRIGGER IF NOT EXISTS trg_task_assignments_require_active_actor_insert
BEFORE INSERT ON task_assignments
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'ASSIGNMENT_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER IF NOT EXISTS trg_task_assignments_require_active_assigner_insert
BEFORE INSERT ON task_assignments
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.assigned_by_actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'ASSIGNMENT_ASSIGNER_NOT_ACTIVE');
END;

CREATE TRIGGER IF NOT EXISTS trg_task_assignments_reviewer_is_owner_insert
BEFORE INSERT ON task_assignments
WHEN NEW.role = 'reviewer' AND NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.actor_id AND type = 'owner' AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'ASSIGNMENT_REVIEWER_MUST_BE_OWNER');
END;

CREATE TRIGGER IF NOT EXISTS trg_task_assignments_protect_history_update
BEFORE UPDATE ON task_assignments
WHEN NEW.id IS NOT OLD.id
    OR NEW.task_id IS NOT OLD.task_id
    OR NEW.actor_id IS NOT OLD.actor_id
    OR NEW.role IS NOT OLD.role
    OR NEW.assigned_by_actor_id IS NOT OLD.assigned_by_actor_id
    OR NEW.assigned_at IS NOT OLD.assigned_at
    OR (
        OLD.unassigned_at IS NOT NULL
        AND (
            NEW.unassigned_at IS NOT OLD.unassigned_at
            OR NEW.reason IS NOT OLD.reason
        )
    )
BEGIN
    SELECT RAISE(ABORT, 'ASSIGNMENT_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER IF NOT EXISTS trg_task_assignments_require_active_actor_update
BEFORE UPDATE OF unassigned_at ON task_assignments
WHEN NEW.unassigned_at IS NULL AND NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'ASSIGNMENT_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER IF NOT EXISTS trg_actors_require_assignment_end_before_inactive
BEFORE UPDATE OF status ON actors
WHEN OLD.status = 'active'
    AND NEW.status = 'inactive'
    AND EXISTS (
        SELECT 1
        FROM task_assignments
        WHERE actor_id = OLD.id AND unassigned_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'ACTOR_HAS_ACTIVE_ASSIGNMENTS');
END;

CREATE TABLE IF NOT EXISTS workflow_events (
    id TEXT PRIMARY KEY,
    aggregate_type TEXT NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 50),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 200),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 100),
    actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    assignment_id TEXT REFERENCES task_assignments(id) ON DELETE SET NULL,
    agent_run_id TEXT,
    request_id TEXT CHECK (request_id IS NULL OR length(request_id) BETWEEN 1 AND 128),
    previous_json TEXT CHECK (previous_json IS NULL OR json_valid(previous_json)),
    current_json TEXT CHECK (current_json IS NULL OR json_valid(current_json)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_workflow_events_assignment_backfill
ON workflow_events(aggregate_type, aggregate_id, action)
WHERE action = 'migration_assignment_backfill';

CREATE INDEX IF NOT EXISTS idx_workflow_events_aggregate_timeline
ON workflow_events(aggregate_type, aggregate_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_workflow_events_action_timeline
ON workflow_events(action, created_at, id);

CREATE INDEX IF NOT EXISTS idx_workflow_events_actor
ON workflow_events(actor_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_workflow_events_assignment
ON workflow_events(assignment_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_workflow_events_agent_run
ON workflow_events(agent_run_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_workflow_events_request
ON workflow_events(request_id)
WHERE request_id IS NOT NULL;

INSERT INTO actors(
    id, type, display_name, status, is_builtin, notes, metadata_json, version
) VALUES (
    '00000000-0000-5000-8000-000000000001',
    'owner',
    '我',
    'active',
    1,
    '',
    '{}',
    1
)
ON CONFLICT(id) DO NOTHING;

INSERT INTO actors(
    id, type, display_name, status, is_builtin, notes, metadata_json, version
) VALUES (
    '00000000-0000-5000-8000-000000000002',
    'system',
    '系统',
    'active',
    1,
    '',
    '{}',
    1
)
ON CONFLICT(id) DO NOTHING;

-- Historical Task IDs are UUIDs created by the API. Replacing their UUID version
-- nibble yields stable, non-random backfill IDs while preserving all UUID payload
-- bits that vary within one database. The fallback remains deterministic for an
-- older non-canonical ID admitted before API UUID validation existed.
INSERT INTO task_assignments(
    id,
    task_id,
    actor_id,
    role,
    assigned_by_actor_id,
    assigned_at,
    unassigned_at,
    reason
)
SELECT
    CASE
        WHEN length(tasks.id) = 36
            AND substr(tasks.id, 9, 1) = '-'
            AND substr(tasks.id, 14, 1) = '-'
            AND substr(tasks.id, 19, 1) = '-'
            AND substr(tasks.id, 24, 1) = '-'
        THEN substr(tasks.id, 1, 14) || '5' || substr(tasks.id, 16)
        ELSE printf('00000000-0000-5001-8000-%012x', tasks.rowid)
    END,
    tasks.id,
    '00000000-0000-5000-8000-000000000001',
    'assignee',
    '00000000-0000-5000-8000-000000000001',
    COALESCE(NULLIF(tasks.created_at, ''), CURRENT_TIMESTAMP),
    CASE
        WHEN tasks.status = 'done'
        THEN COALESCE(
            NULLIF(tasks.completed_at, ''),
            NULLIF(tasks.updated_at, ''),
            NULLIF(tasks.created_at, ''),
            CURRENT_TIMESTAMP
        )
        ELSE NULL
    END,
    'schema_v7_migration_inferred_owner'
FROM tasks
WHERE NOT EXISTS (
    SELECT 1
    FROM task_assignments
    WHERE task_assignments.task_id = tasks.id
        AND task_assignments.role = 'assignee'
)
ON CONFLICT(id) DO NOTHING;

INSERT INTO workflow_events(
    id,
    aggregate_type,
    aggregate_id,
    action,
    actor_id,
    assignment_id,
    current_json
)
SELECT
    CASE
        WHEN tasks.id LIKE '________-____-____-____-____________'
        THEN substr(tasks.id, 1, 14) || '6' || substr(tasks.id, 16)
        ELSE printf('00000000-0000-6001-8000-%012x', tasks.rowid)
    END,
    'task',
    tasks.id,
    'migration_assignment_backfill',
    '00000000-0000-5000-8000-000000000001',
    task_assignments.id,
    '{"source":"schema_v7_migration","inferred":true,"role":"assignee"}'
FROM tasks
JOIN task_assignments
    ON task_assignments.task_id = tasks.id
    AND task_assignments.role = 'assignee'
    AND task_assignments.id = CASE
        WHEN length(tasks.id) = 36
            AND substr(tasks.id, 9, 1) = '-'
            AND substr(tasks.id, 14, 1) = '-'
            AND substr(tasks.id, 19, 1) = '-'
            AND substr(tasks.id, 24, 1) = '-'
        THEN substr(tasks.id, 1, 14) || '5' || substr(tasks.id, 16)
        ELSE printf('00000000-0000-5001-8000-%012x', tasks.rowid)
    END
WHERE NOT EXISTS (
    SELECT 1
    FROM workflow_events
    WHERE workflow_events.aggregate_type = 'task'
        AND workflow_events.aggregate_id = tasks.id
        AND workflow_events.action = 'migration_assignment_backfill'
)
ON CONFLICT(id) DO NOTHING;
