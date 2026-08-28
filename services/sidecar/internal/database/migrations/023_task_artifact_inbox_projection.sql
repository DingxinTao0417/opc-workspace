CREATE INDEX idx_inbox_items_source_entity
ON inbox_items(source_entity_type, source_entity_id, status, id)
WHERE source_entity_id IS NOT NULL;

CREATE TRIGGER trg_inbox_task_artifact_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'task_artifact'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR NEW.source_event_key <> 'task-artifact:' || NEW.source_entity_id || ':followup'
          OR NEW.source_deleted_at IS NOT NULL
          OR json_extract(NEW.payload_json, '$.artifact_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.task_id') <> 'text'
          OR json_type(NEW.payload_json, '$.submission_id') <> 'text'
          OR json_type(NEW.payload_json, '$.artifact_name') <> 'text'
        THEN RAISE(ABORT, 'INVALID_TASK_ARTIFACT_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM task_artifacts
            WHERE id = NEW.source_entity_id
              AND requires_followup = 1
              AND deleted_at IS NULL
        )
        THEN RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_NOT_FOUND')
    END;
END;

CREATE TRIGGER trg_inbox_task_artifact_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'task_artifact'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_artifact_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task_artifact'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_task_artifact_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'task_artifact'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER trg_task_artifact_soft_delete_requires_source_coordination
BEFORE UPDATE OF deleted_at ON task_artifacts
WHEN OLD.deleted_at IS NULL
  AND NEW.deleted_at IS NOT NULL
  AND EXISTS (
      SELECT 1
      FROM inbox_items
      WHERE source_entity_type = 'task_artifact'
        AND source_entity_id = OLD.id
        AND source_deleted_at IS NULL
  )
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_NOT_COORDINATED');
END;

CREATE TRIGGER trg_task_artifact_hard_delete_requires_source_coordination
BEFORE DELETE ON task_artifacts
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'task_artifact'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_ARTIFACT_INBOX_SOURCE_NOT_COORDINATED');
END;
