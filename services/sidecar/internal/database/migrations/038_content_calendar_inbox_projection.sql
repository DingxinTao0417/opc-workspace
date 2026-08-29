CREATE INDEX idx_inbox_content_item_sources
ON inbox_items(source_entity_id, status, id)
WHERE source_entity_type = 'content_item';

CREATE TRIGGER content_item_inbox_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'content_item'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR NEW.source_deleted_at IS NOT NULL
          OR json_type(NEW.payload_json, '$.content_item_id') <> 'text'
          OR json_extract(NEW.payload_json, '$.content_item_id') <> NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.event_type') <> 'text'
          OR json_extract(NEW.payload_json, '$.event_type') NOT IN ('review_due', 'publish_due')
          OR json_type(NEW.payload_json, '$.content_version') <> 'integer'
          OR json_type(NEW.payload_json, '$.scheduled_at') <> 'text'
          OR json_type(NEW.payload_json, '$.scheduled_timezone') <> 'text'
          OR NEW.source_event_key <> 'content:' || NEW.source_entity_id || ':' || json_extract(NEW.payload_json, '$.event_type') || ':' || json_extract(NEW.payload_json, '$.content_version')
        THEN RAISE(ABORT, 'INVALID_CONTENT_ITEM_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM content_items
            WHERE id = NEW.source_entity_id
              AND version = json_extract(NEW.payload_json, '$.content_version')
              AND scheduled_at = json_extract(NEW.payload_json, '$.scheduled_at')
              AND scheduled_timezone = json_extract(NEW.payload_json, '$.scheduled_timezone')
              AND (
                  (status = 'in_review' AND json_extract(NEW.payload_json, '$.event_type') = 'review_due')
                  OR (status = 'scheduled' AND json_extract(NEW.payload_json, '$.event_type') = 'publish_due')
              )
        )
        THEN RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_NOT_FOUND')
    END;
END;

CREATE TRIGGER content_item_inbox_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'content_item'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER content_item_inbox_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'content_item'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER content_item_inbox_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'content_item'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER content_item_delete_requires_source_coordination
BEFORE DELETE ON content_items
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'content_item'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_NOT_COORDINATED');
END;
