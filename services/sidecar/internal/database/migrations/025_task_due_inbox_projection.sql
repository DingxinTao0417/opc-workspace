CREATE INDEX idx_inbox_items_task_due_at
ON inbox_items(
    source_entity_id,
    json_extract(payload_json, '$.due_at'),
    id
)
WHERE source_entity_type = 'task_due';

CREATE TRIGGER trg_inbox_task_due_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'task_due'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR json_type(NEW.payload_json, '$.task_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.task_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.task_title') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.task_title'))) = 0
          OR json_type(NEW.payload_json, '$.due_at') IS NOT 'text'
          OR julianday(json_extract(NEW.payload_json, '$.due_at')) IS NULL
          OR json_type(NEW.payload_json, '$.projected_at') IS NOT 'text'
          OR julianday(json_extract(NEW.payload_json, '$.projected_at')) IS NULL
          OR json_type(NEW.payload_json, '$.due_state') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.due_state') NOT IN ('due_soon', 'overdue')
          OR json_type(NEW.payload_json, '$.lead_minutes') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.lead_minutes') IS NOT 1440
          OR julianday(json_extract(NEW.payload_json, '$.due_at')) > julianday(json_extract(NEW.payload_json, '$.projected_at'), '+24 hours')
          OR (
              json_extract(NEW.payload_json, '$.due_state') = 'overdue'
              AND julianday(json_extract(NEW.payload_json, '$.due_at')) >= julianday(json_extract(NEW.payload_json, '$.projected_at'))
          )
          OR (
              json_extract(NEW.payload_json, '$.due_state') = 'due_soon'
              AND julianday(json_extract(NEW.payload_json, '$.due_at')) < julianday(json_extract(NEW.payload_json, '$.projected_at'))
          )
          OR NEW.source_event_key <> 'task:' || NEW.source_entity_id || ':due:' || json_extract(NEW.payload_json, '$.due_at')
          OR NEW.due_at IS NOT json_extract(NEW.payload_json, '$.due_at')
          OR NEW.source_deleted_at IS NOT NULL
        THEN RAISE(ABORT, 'INVALID_TASK_DUE_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM tasks
            WHERE id = NEW.source_entity_id
              AND status NOT IN ('done', 'cancelled')
              AND julianday(due_date) = julianday(json_extract(NEW.payload_json, '$.due_at'))
        )
        THEN RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_NOT_CURRENT')
    END;
END;

CREATE TRIGGER trg_inbox_task_due_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, due_at, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'task_due'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.due_at IS NOT OLD.due_at
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_due_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task_due'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_due_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task_due'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER trg_task_delete_requires_due_source_coordination
BEFORE DELETE ON tasks
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'task_due'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_NOT_COORDINATED');
END;
