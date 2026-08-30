CREATE TABLE automation_event_deliveries (
    id TEXT NOT NULL PRIMARY KEY
        CHECK (length(trim(id)) BETWEEN 1 AND 100 AND id = trim(id)),
    rule_id TEXT NOT NULL REFERENCES automation_rules(id) ON DELETE RESTRICT,
    preset_key TEXT NOT NULL
        CHECK (length(trim(preset_key)) BETWEEN 1 AND 100 AND preset_key = trim(preset_key)),
    rule_version INTEGER NOT NULL CHECK (rule_version >= 1),
    source_event_id TEXT NOT NULL REFERENCES workflow_events(id) ON DELETE RESTRICT,
    logical_key TEXT NOT NULL UNIQUE
        CHECK (length(logical_key) BETWEEN 1 AND 300),
    config_snapshot_json TEXT NOT NULL
        CHECK (
            json_valid(config_snapshot_json)
            AND json_type(config_snapshot_json) = 'object'
            AND length(config_snapshot_json) BETWEEN 2 AND 4096
        ),
    action_snapshot_json TEXT NOT NULL
        CHECK (
            json_valid(action_snapshot_json)
            AND json_type(action_snapshot_json) = 'object'
            AND length(action_snapshot_json) BETWEEN 2 AND 16384
        ),
    delivery_attempts INTEGER NOT NULL DEFAULT 0 CHECK (delivery_attempts >= 0),
    available_at TEXT NOT NULL CHECK (length(available_at) > 0),
    last_error_code TEXT
        CHECK (
            last_error_code IS NULL
            OR (
                length(trim(last_error_code)) BETWEEN 1 AND 100
                AND last_error_code = trim(last_error_code)
            )
        ),
    last_error_at TEXT CHECK (last_error_at IS NULL OR length(last_error_at) > 0),
    captured_at TEXT NOT NULL CHECK (length(captured_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    UNIQUE (rule_id, source_event_id),
    CHECK (logical_key = 'event:' || rule_id || ':' || source_event_id),
    CHECK (
        (last_error_code IS NULL AND last_error_at IS NULL)
        OR (delivery_attempts > 0 AND last_error_code IS NOT NULL AND last_error_at IS NOT NULL)
    )
);

CREATE INDEX idx_automation_event_deliveries_due
ON automation_event_deliveries(available_at, captured_at, id);

CREATE INDEX idx_automation_event_deliveries_source
ON automation_event_deliveries(source_event_id, id);

CREATE TRIGGER trg_automation_event_deliveries_rule_snapshot_insert
BEFORE INSERT ON automation_event_deliveries
WHEN NOT EXISTS (
    SELECT 1
    FROM automation_rules
    WHERE id = NEW.rule_id
      AND preset_key = NEW.preset_key
      AND version = NEW.rule_version
      AND enabled = 1
)
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_EVENT_DELIVERY_RULE_SNAPSHOT_INVALID');
END;

CREATE TRIGGER trg_automation_event_deliveries_snapshot_immutable
BEFORE UPDATE ON automation_event_deliveries
WHEN NEW.id IS NOT OLD.id
  OR NEW.rule_id IS NOT OLD.rule_id
  OR NEW.preset_key IS NOT OLD.preset_key
  OR NEW.rule_version IS NOT OLD.rule_version
  OR NEW.source_event_id IS NOT OLD.source_event_id
  OR NEW.logical_key IS NOT OLD.logical_key
  OR NEW.config_snapshot_json IS NOT OLD.config_snapshot_json
  OR NEW.action_snapshot_json IS NOT OLD.action_snapshot_json
  OR NEW.captured_at IS NOT OLD.captured_at
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_EVENT_DELIVERY_SNAPSHOT_IMMUTABLE');
END;

CREATE TRIGGER trg_automation_event_deliveries_backoff_update
BEFORE UPDATE ON automation_event_deliveries
WHEN NEW.id IS OLD.id
  AND NEW.rule_id IS OLD.rule_id
  AND NEW.preset_key IS OLD.preset_key
  AND NEW.rule_version IS OLD.rule_version
  AND NEW.source_event_id IS OLD.source_event_id
  AND NEW.logical_key IS OLD.logical_key
  AND NEW.config_snapshot_json IS OLD.config_snapshot_json
  AND NEW.action_snapshot_json IS OLD.action_snapshot_json
  AND NEW.captured_at IS OLD.captured_at
  AND NOT (
      (
          NEW.delivery_attempts = OLD.delivery_attempts + 1
          AND NEW.available_at > OLD.available_at
          AND NEW.last_error_code IS OLD.last_error_code
          AND NEW.last_error_at IS OLD.last_error_at
          AND NEW.updated_at IS NOT OLD.updated_at
      )
      OR (
          NEW.delivery_attempts = OLD.delivery_attempts
          AND NEW.available_at IS OLD.available_at
          AND NEW.last_error_code IS NOT NULL
          AND NEW.last_error_at IS NOT NULL
          AND (
              NEW.last_error_code IS NOT OLD.last_error_code
              OR NEW.last_error_at IS NOT OLD.last_error_at
          )
          AND NEW.updated_at IS NOT OLD.updated_at
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'AUTOMATION_EVENT_DELIVERY_BACKOFF_INVALID');
END;
