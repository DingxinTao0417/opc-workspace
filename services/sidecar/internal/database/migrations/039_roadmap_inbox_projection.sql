CREATE INDEX idx_inbox_roadmap_milestone_sources
ON inbox_items(source_entity_id, status, id)
WHERE source_entity_type = 'roadmap_milestone';

CREATE TRIGGER roadmap_milestone_inbox_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'roadmap_milestone'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR NEW.source_deleted_at IS NOT NULL
          OR json_type(NEW.payload_json, '$.roadmap_milestone_id') <> 'text'
          OR json_extract(NEW.payload_json, '$.roadmap_milestone_id') <> NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.event_type') <> 'text'
          OR json_extract(NEW.payload_json, '$.event_type') NOT IN ('due', 'achieved')
          OR json_type(NEW.payload_json, '$.milestone_version') <> 'integer'
          OR json_type(NEW.payload_json, '$.target_date') <> 'text'
          OR json_type(NEW.payload_json, '$.year') <> 'integer'
          OR json_type(NEW.payload_json, '$.quarter') <> 'integer'
          OR NEW.source_event_key <> 'roadmap:' || NEW.source_entity_id || ':' || json_extract(NEW.payload_json, '$.event_type') || ':' || json_extract(NEW.payload_json, '$.milestone_version')
        THEN RAISE(ABORT, 'INVALID_ROADMAP_MILESTONE_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NOT EXISTS (
            SELECT 1
            FROM roadmap_milestones
            WHERE id = NEW.source_entity_id
              AND version = json_extract(NEW.payload_json, '$.milestone_version')
              AND target_date = json_extract(NEW.payload_json, '$.target_date')
              AND year = json_extract(NEW.payload_json, '$.year')
              AND quarter = json_extract(NEW.payload_json, '$.quarter')
              AND (
                  (status IN ('planned', 'active') AND json_extract(NEW.payload_json, '$.event_type') = 'due')
                  OR (status = 'achieved' AND json_extract(NEW.payload_json, '$.event_type') = 'achieved')
              )
        )
        THEN RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_NOT_FOUND')
    END;
END;

CREATE TRIGGER roadmap_milestone_inbox_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'roadmap_milestone'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER roadmap_milestone_inbox_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'roadmap_milestone'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER roadmap_milestone_inbox_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'roadmap_milestone'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND OLD.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER roadmap_milestone_delete_requires_source_coordination
BEFORE DELETE ON roadmap_milestones
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'roadmap_milestone'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_NOT_COORDINATED');
END;
