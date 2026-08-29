-- migration: destructive
-- migration: foreign_keys=off

CREATE TABLE reminders_v40 (
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
    series_id TEXT NOT NULL CHECK (length(trim(series_id)) > 0),
    recurrence_type TEXT NOT NULL DEFAULT 'none'
        CHECK (recurrence_type IN ('none', 'daily', 'weekly', 'monthly')),
    recurrence_interval INTEGER NOT NULL DEFAULT 1
        CHECK (recurrence_interval BETWEEN 1 AND 365),
    recurrence_timezone TEXT NOT NULL DEFAULT 'UTC'
        CHECK (length(trim(recurrence_timezone)) BETWEEN 1 AND 100),
    occurrence_number INTEGER NOT NULL DEFAULT 1
        CHECK (occurrence_number >= 1),
    recurrence_anchor_day INTEGER NOT NULL DEFAULT 1
        CHECK (recurrence_anchor_day BETWEEN 1 AND 31),
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

INSERT INTO reminders_v40(
    id, source_entity_type, source_entity_id, title, summary, priority,
    trigger_at, status, source_event_key, created_by_actor_id, fired_at,
    inbox_item_id, cancelled_by_actor_id, cancelled_at, cancel_reason,
    version, created_at, updated_at, series_id, recurrence_type,
    recurrence_interval, recurrence_timezone, occurrence_number,
    recurrence_anchor_day
)
SELECT
    id, source_entity_type, source_entity_id, title, summary, priority,
    trigger_at, status, source_event_key, created_by_actor_id, fired_at,
    inbox_item_id, cancelled_by_actor_id, cancelled_at, cancel_reason,
    version, created_at, updated_at, series_id, recurrence_type,
    recurrence_interval, recurrence_timezone, occurrence_number, 1
FROM reminders;

DROP TABLE reminders;
ALTER TABLE reminders_v40 RENAME TO reminders;

CREATE INDEX idx_reminders_scheduled_trigger
ON reminders(status, trigger_at, id);

CREATE INDEX idx_reminders_status_updated
ON reminders(status, updated_at DESC, id);

CREATE INDEX idx_reminders_creator
ON reminders(created_by_actor_id, created_at DESC, id);

CREATE UNIQUE INDEX idx_reminders_series_occurrence
ON reminders(series_id, occurrence_number);

CREATE UNIQUE INDEX idx_reminders_one_scheduled_per_series
ON reminders(series_id)
WHERE status = 'scheduled';

CREATE INDEX idx_reminders_series_history
ON reminders(series_id, occurrence_number DESC, id);

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

CREATE TRIGGER trg_reminders_validate_recurrence_insert
BEFORE INSERT ON reminders
WHEN NEW.series_id IS NULL
  OR length(trim(NEW.series_id)) = 0
  OR NEW.occurrence_number < 1
  OR NEW.recurrence_timezone <> trim(NEW.recurrence_timezone)
  OR length(NEW.recurrence_timezone) NOT BETWEEN 1 AND 100
  OR NEW.recurrence_anchor_day NOT BETWEEN 1 AND 31
  OR (
      NEW.recurrence_type = 'none'
      AND (
          NEW.recurrence_interval <> 1
          OR NEW.recurrence_timezone <> 'UTC'
          OR NEW.recurrence_anchor_day <> 1
      )
  )
  OR (
      NEW.recurrence_type IN ('daily', 'weekly')
      AND (
          NEW.recurrence_interval NOT BETWEEN 1 AND 365
          OR NEW.recurrence_anchor_day <> 1
      )
  )
  OR (
      NEW.recurrence_type = 'monthly'
      AND NEW.recurrence_interval NOT BETWEEN 1 AND 365
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_RECURRENCE_INVALID');
END;

CREATE TRIGGER trg_reminders_validate_recurrence_update
BEFORE UPDATE ON reminders
WHEN NEW.series_id IS NULL
  OR length(trim(NEW.series_id)) = 0
  OR NEW.occurrence_number < 1
  OR NEW.recurrence_timezone <> trim(NEW.recurrence_timezone)
  OR length(NEW.recurrence_timezone) NOT BETWEEN 1 AND 100
  OR NEW.recurrence_anchor_day NOT BETWEEN 1 AND 31
  OR (
      NEW.recurrence_type = 'none'
      AND (
          NEW.recurrence_interval <> 1
          OR NEW.recurrence_timezone <> 'UTC'
          OR NEW.recurrence_anchor_day <> 1
      )
  )
  OR (
      NEW.recurrence_type IN ('daily', 'weekly')
      AND (
          NEW.recurrence_interval NOT BETWEEN 1 AND 365
          OR NEW.recurrence_anchor_day <> 1
      )
  )
  OR (
      NEW.recurrence_type = 'monthly'
      AND NEW.recurrence_interval NOT BETWEEN 1 AND 365
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_RECURRENCE_INVALID');
END;

CREATE TRIGGER trg_reminders_protect_series_identity_update
BEFORE UPDATE ON reminders
WHEN NEW.series_id IS NOT OLD.series_id
  OR NEW.occurrence_number IS NOT OLD.occurrence_number
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_SERIES_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_reminders_protect_terminal_recurrence_update
BEFORE UPDATE ON reminders
WHEN OLD.status IN ('fired', 'cancelled')
  AND (
      NEW.recurrence_type IS NOT OLD.recurrence_type
      OR NEW.recurrence_interval IS NOT OLD.recurrence_interval
      OR NEW.recurrence_timezone IS NOT OLD.recurrence_timezone
      OR NEW.recurrence_anchor_day IS NOT OLD.recurrence_anchor_day
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_TERMINAL_IMMUTABLE');
END;
