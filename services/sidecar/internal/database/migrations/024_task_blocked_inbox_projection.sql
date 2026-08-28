CREATE INDEX idx_inbox_items_task_block_version
ON inbox_items(
    source_entity_id,
    CAST(json_extract(payload_json, '$.block_version') AS INTEGER),
    id
)
WHERE source_entity_type = 'task';

CREATE TRIGGER trg_inbox_task_blocked_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'task'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR json_type(NEW.payload_json, '$.task_id') <> 'text'
          OR json_extract(NEW.payload_json, '$.task_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.task_title') <> 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.task_title'))) = 0
          OR json_type(NEW.payload_json, '$.blocked_reason') <> 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.blocked_reason'))) = 0
          OR json_type(NEW.payload_json, '$.blocked_at') <> 'text'
          OR json_type(NEW.payload_json, '$.blocked_from_status') <> 'text'
          OR json_extract(NEW.payload_json, '$.blocked_from_status') NOT IN ('todo', 'in_progress', 'waiting_review')
          OR json_type(NEW.payload_json, '$.block_version') <> 'integer'
          OR json_extract(NEW.payload_json, '$.block_version') < 2
          OR NEW.source_event_key <> 'task:' || NEW.source_entity_id || ':blocked:' || json_extract(NEW.payload_json, '$.block_version')
          OR NEW.source_deleted_at IS NOT NULL
        THEN RAISE(ABORT, 'INVALID_TASK_BLOCKED_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM tasks
            WHERE id = NEW.source_entity_id
              AND status = 'blocked'
              AND version = json_extract(NEW.payload_json, '$.block_version')
              AND blocked_reason = json_extract(NEW.payload_json, '$.blocked_reason')
              AND blocked_at = json_extract(NEW.payload_json, '$.blocked_at')
              AND blocked_from_status = json_extract(NEW.payload_json, '$.blocked_from_status')
        )
        THEN RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_NOT_CURRENT')
    END;
END;

CREATE TRIGGER trg_inbox_task_blocked_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'task'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_blocked_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_blocked_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER trg_task_delete_requires_blocked_source_coordination
BEFORE DELETE ON tasks
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'task'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_NOT_COORDINATED');
END;
