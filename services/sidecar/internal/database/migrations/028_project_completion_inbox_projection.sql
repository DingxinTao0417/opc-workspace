CREATE INDEX idx_inbox_items_project_completion
ON inbox_items(
    source_entity_id,
    json_extract(payload_json, '$.completion_version'),
    id
)
WHERE source_entity_type = 'project_completion';

CREATE TRIGGER trg_inbox_project_completion_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'project_completion'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR NEW.priority <> 'P1'
          OR NEW.due_at IS NOT NULL
          OR NEW.source_deleted_at IS NOT NULL
          OR json_valid(NEW.payload_json) <> 1
          OR (SELECT COUNT(*) FROM json_each(NEW.payload_json)) <> 5
          OR json_type(NEW.payload_json, '$.project_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.project_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.project_name') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.project_name'))) = 0
          OR json_type(NEW.payload_json, '$.completed_at') IS NOT 'text'
          OR julianday(json_extract(NEW.payload_json, '$.completed_at')) IS NULL
          OR json_type(NEW.payload_json, '$.completion_version') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.completion_version') < 2
          OR json_type(NEW.payload_json, '$.incomplete_task_count') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.incomplete_task_count') < 0
          OR NEW.source_event_key <> 'project:' || NEW.source_entity_id || ':completed:' || json_extract(NEW.payload_json, '$.completion_version')
        THEN RAISE(ABORT, 'INVALID_PROJECT_COMPLETION_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM projects
            WHERE id = NEW.source_entity_id
              AND status = 'completed'
              AND version = json_extract(NEW.payload_json, '$.completion_version')
        )
        THEN RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_NOT_CURRENT')
    END;
END;

CREATE TRIGGER trg_inbox_project_completion_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, due_at, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'project_completion'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.due_at IS NOT OLD.due_at
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_project_completion_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'project_completion'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_project_completion_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'project_completion'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER trg_project_delete_requires_completion_source_coordination
BEFORE DELETE ON projects
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'project_completion'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_NOT_COORDINATED');
END;
