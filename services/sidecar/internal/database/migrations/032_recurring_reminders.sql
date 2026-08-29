ALTER TABLE reminders ADD COLUMN series_id TEXT;
ALTER TABLE reminders ADD COLUMN recurrence_type TEXT NOT NULL DEFAULT 'none'
    CHECK (recurrence_type IN ('none', 'daily', 'weekly'));
ALTER TABLE reminders ADD COLUMN recurrence_interval INTEGER NOT NULL DEFAULT 1
    CHECK (recurrence_interval BETWEEN 1 AND 365);
ALTER TABLE reminders ADD COLUMN recurrence_timezone TEXT NOT NULL DEFAULT 'UTC'
    CHECK (length(trim(recurrence_timezone)) BETWEEN 1 AND 100);
ALTER TABLE reminders ADD COLUMN occurrence_number INTEGER NOT NULL DEFAULT 1
    CHECK (occurrence_number >= 1);

UPDATE reminders SET series_id = id WHERE series_id IS NULL;

CREATE UNIQUE INDEX idx_reminders_series_occurrence
ON reminders(series_id, occurrence_number);

CREATE UNIQUE INDEX idx_reminders_one_scheduled_per_series
ON reminders(series_id)
WHERE status = 'scheduled';

CREATE INDEX idx_reminders_series_history
ON reminders(series_id, occurrence_number DESC, id);

CREATE TRIGGER trg_reminders_validate_recurrence_insert
BEFORE INSERT ON reminders
WHEN NEW.series_id IS NULL
  OR length(trim(NEW.series_id)) = 0
  OR NEW.occurrence_number < 1
  OR NEW.recurrence_timezone <> trim(NEW.recurrence_timezone)
  OR length(NEW.recurrence_timezone) NOT BETWEEN 1 AND 100
  OR (
      NEW.recurrence_type = 'none'
      AND (
          NEW.recurrence_interval <> 1
          OR NEW.recurrence_timezone <> 'UTC'
      )
  )
  OR (
      NEW.recurrence_type IN ('daily', 'weekly')
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
  OR (
      NEW.recurrence_type = 'none'
      AND (
          NEW.recurrence_interval <> 1
          OR NEW.recurrence_timezone <> 'UTC'
      )
  )
  OR (
      NEW.recurrence_type IN ('daily', 'weekly')
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
  )
BEGIN
    SELECT RAISE(ABORT, 'REMINDER_TERMINAL_IMMUTABLE');
END;
