CREATE TABLE reminders (
    id TEXT PRIMARY KEY,
    source_entity_type TEXT NOT NULL DEFAULT 'manual'
        CHECK (length(trim(source_entity_type)) BETWEEN 1 AND 50),
    source_entity_id TEXT
        CHECK (source_entity_id IS NULL OR length(trim(source_entity_id)) > 0),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 2 AND 200 AND title = trim(title)),
    summary TEXT NOT NULL DEFAULT ''
        CHECK (length(summary) <= 10000),
    priority TEXT NOT NULL DEFAULT 'P2'
        CHECK (priority IN ('P0', 'P1', 'P2', 'P3')),
    trigger_at TEXT NOT NULL CHECK (length(trigger_at) > 0),
    status TEXT NOT NULL DEFAULT 'scheduled'
        CHECK (status IN ('scheduled', 'fired', 'cancelled')),
    source_event_key TEXT NOT NULL UNIQUE
        CHECK (length(trim(source_event_key)) BETWEEN 1 AND 500),
    created_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    fired_at TEXT CHECK (fired_at IS NULL OR length(fired_at) > 0),
    inbox_item_id TEXT REFERENCES inbox_items(id) ON DELETE RESTRICT,
    cancelled_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    cancelled_at TEXT CHECK (cancelled_at IS NULL OR length(cancelled_at) > 0),
    cancel_reason TEXT
        CHECK (cancel_reason IS NULL OR length(trim(cancel_reason)) BETWEEN 1 AND 1000),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(updated_at) > 0),
    CHECK (
        source_entity_type <> 'manual'
        OR source_entity_id IS NULL
    ),
    CHECK (
        (
            status = 'scheduled'
            AND fired_at IS NULL
            AND inbox_item_id IS NULL
            AND cancelled_by_actor_id IS NULL
            AND cancelled_at IS NULL
            AND cancel_reason IS NULL
        )
        OR (
            status = 'fired'
            AND fired_at IS NOT NULL
            AND inbox_item_id IS NOT NULL
            AND cancelled_by_actor_id IS NULL
            AND cancelled_at IS NULL
            AND cancel_reason IS NULL
        )
        OR (
            status = 'cancelled'
            AND fired_at IS NULL
            AND inbox_item_id IS NULL
            AND cancelled_by_actor_id IS NOT NULL
            AND cancelled_at IS NOT NULL
            AND cancel_reason IS NOT NULL
        )
    )
);

CREATE INDEX idx_reminders_scheduled_trigger
ON reminders(status, trigger_at, id);

CREATE INDEX idx_reminders_status_updated
ON reminders(status, updated_at DESC, id);

CREATE INDEX idx_reminders_creator
ON reminders(created_by_actor_id, created_at DESC, id);

CREATE TRIGGER trg_reminders_require_active_creator_insert
BEFORE INSERT ON reminders
WHEN NOT EXISTS (
    SELECT 1 FROM actors
    WHERE id = NEW.created_by_actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER trg_reminders_protect_identity_update
BEFORE UPDATE ON reminders
WHEN NEW.id IS NOT OLD.id
  OR NEW.source_entity_type IS NOT OLD.source_entity_type
  OR NEW.source_entity_id IS NOT OLD.source_entity_id
  OR NEW.source_event_key IS NOT OLD.source_event_key
  OR NEW.created_by_actor_id IS NOT OLD.created_by_actor_id
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_reminders_protect_terminal_update
BEFORE UPDATE ON reminders
WHEN OLD.status IN ('fired', 'cancelled')
  AND (
      NEW.title IS NOT OLD.title
      OR NEW.summary IS NOT OLD.summary
      OR NEW.priority IS NOT OLD.priority
      OR NEW.trigger_at IS NOT OLD.trigger_at
      OR NEW.status IS NOT OLD.status
      OR NEW.fired_at IS NOT OLD.fired_at
      OR NEW.inbox_item_id IS NOT OLD.inbox_item_id
      OR NEW.cancelled_by_actor_id IS NOT OLD.cancelled_by_actor_id
      OR NEW.cancelled_at IS NOT OLD.cancelled_at
      OR NEW.cancel_reason IS NOT OLD.cancel_reason
      OR NEW.version IS NOT OLD.version
      OR NEW.updated_at IS NOT OLD.updated_at
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_TERMINAL_IMMUTABLE');
END;

CREATE TRIGGER trg_reminders_validate_fired_inbox_update
BEFORE UPDATE ON reminders
WHEN OLD.status = 'scheduled' AND NEW.status = 'fired'
  AND NOT EXISTS (
      SELECT 1 FROM inbox_items
      WHERE id = NEW.inbox_item_id
        AND kind = 'reminder'
        AND source_entity_type = 'reminder'
        AND source_entity_id = NEW.id
        AND source_event_key = NEW.source_event_key
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_INBOX_MISMATCH');
END;

CREATE TRIGGER trg_reminders_protect_hard_delete
BEFORE DELETE ON reminders
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_HARD_DELETE_FORBIDDEN');
END;
