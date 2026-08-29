CREATE TABLE client_followups (
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
    client_id TEXT NOT NULL
        REFERENCES clients(id) ON DELETE RESTRICT,
    assigned_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    scheduled_at TEXT NOT NULL
        CHECK (length(scheduled_at) BETWEEN 20 AND 40),
    timezone TEXT NOT NULL
        CHECK (length(trim(timezone)) BETWEEN 1 AND 100 AND timezone = trim(timezone)),
    channel TEXT NOT NULL
        CHECK (length(trim(channel)) BETWEEN 1 AND 80 AND channel = trim(channel)),
    purpose TEXT NOT NULL
        CHECK (length(trim(purpose)) BETWEEN 1 AND 500 AND purpose = trim(purpose)),
    notes TEXT
        CHECK (notes IS NULL OR length(trim(notes)) BETWEEN 1 AND 4000),
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'completed', 'skipped', 'cancelled')),
    priority TEXT NOT NULL DEFAULT 'normal'
        CHECK (priority IN ('low', 'normal', 'high')),
    completed_at TEXT,
    result TEXT
        CHECK (result IS NULL OR length(trim(result)) BETWEEN 1 AND 4000),
    next_step TEXT
        CHECK (next_step IS NULL OR length(trim(next_step)) BETWEEN 1 AND 4000),
    skipped_at TEXT,
    skip_reason TEXT
        CHECK (skip_reason IS NULL OR length(trim(skip_reason)) BETWEEN 1 AND 1000),
    cancelled_at TEXT,
    cancel_reason TEXT
        CHECK (cancel_reason IS NULL OR length(trim(cancel_reason)) BETWEEN 1 AND 1000),
    rescheduled_from_id TEXT
        REFERENCES client_followups(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (rescheduled_from_id IS NULL OR rescheduled_from_id <> id),
    CHECK (
        (status = 'planned'
            AND completed_at IS NULL AND result IS NULL AND next_step IS NULL
            AND skipped_at IS NULL AND skip_reason IS NULL
            AND cancelled_at IS NULL AND cancel_reason IS NULL)
        OR
        (status = 'completed'
            AND completed_at IS NOT NULL AND result IS NOT NULL
            AND skipped_at IS NULL AND skip_reason IS NULL
            AND cancelled_at IS NULL AND cancel_reason IS NULL)
        OR
        (status = 'skipped'
            AND completed_at IS NULL AND result IS NULL AND next_step IS NULL
            AND skipped_at IS NOT NULL AND skip_reason IS NOT NULL
            AND cancelled_at IS NULL AND cancel_reason IS NULL)
        OR
        (status = 'cancelled'
            AND completed_at IS NULL AND result IS NULL AND next_step IS NULL
            AND skipped_at IS NULL AND skip_reason IS NULL
            AND cancelled_at IS NOT NULL AND cancel_reason IS NOT NULL)
    )
);

CREATE INDEX idx_client_followups_client_history
ON client_followups(client_id, scheduled_at DESC, id ASC);

CREATE INDEX idx_client_followups_planned_due
ON client_followups(status, scheduled_at ASC, priority DESC, id ASC)
WHERE status = 'planned';

CREATE INDEX idx_client_followups_actor_planned_due
ON client_followups(assigned_actor_id, scheduled_at ASC, id ASC)
WHERE status = 'planned';

CREATE INDEX idx_client_followups_rescheduled_from
ON client_followups(rescheduled_from_id)
WHERE rescheduled_from_id IS NOT NULL;

CREATE TRIGGER client_followups_require_active_assignee_insert
BEFORE INSERT ON client_followups
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.assigned_actor_id
      AND type IN ('owner', 'person')
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_FOLLOWUP_ASSIGNEE_NOT_ACTIVE');
END;

CREATE TRIGGER client_followups_require_active_assignee_update
BEFORE UPDATE OF assigned_actor_id ON client_followups
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.assigned_actor_id
      AND type IN ('owner', 'person')
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_FOLLOWUP_ASSIGNEE_NOT_ACTIVE');
END;

CREATE TRIGGER client_followups_status_transition
BEFORE UPDATE OF status ON client_followups
FOR EACH ROW
WHEN OLD.status <> 'planned'
  OR NEW.status = 'planned'
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_FOLLOWUP_STATUS_IMMUTABLE');
END;

CREATE TRIGGER client_followups_version_step
BEFORE UPDATE ON client_followups
FOR EACH ROW
WHEN NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_FOLLOWUP_VERSION_INVALID');
END;

CREATE TRIGGER client_followups_bump_client_after_insert
AFTER INSERT ON client_followups
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER client_followups_bump_client_after_update
AFTER UPDATE ON client_followups
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER client_followups_bump_client_after_delete
AFTER DELETE ON client_followups
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.client_id;
END;
