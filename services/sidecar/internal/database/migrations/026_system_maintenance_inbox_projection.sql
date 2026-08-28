CREATE UNIQUE INDEX ux_inbox_items_active_system_maintenance_source
ON inbox_items(source_entity_type, source_entity_id)
WHERE source_entity_type = 'system_maintenance'
  AND source_entity_id IS NOT NULL
  AND status IN ('open', 'tracking')
  AND source_deleted_at IS NULL;

CREATE TRIGGER trg_inbox_system_maintenance_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'system_maintenance'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR NEW.priority NOT IN ('P0', 'P1')
          OR NEW.due_at IS NOT NULL
          OR NEW.source_deleted_at IS NOT NULL
          OR json_type(NEW.payload_json, '$.component') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.operation') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.failure_code') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.occurred_at') IS NOT 'text'
          OR julianday(json_extract(NEW.payload_json, '$.occurred_at')) IS NULL
          OR json_type(NEW.payload_json, '$.message') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.message'))) = 0
          OR NEW.source_entity_id <> json_extract(NEW.payload_json, '$.component') || ':' || json_extract(NEW.payload_json, '$.operation')
          OR NEW.source_event_key NOT GLOB 'system:' || NEW.source_entity_id || ':*'
        THEN RAISE(ABORT, 'INVALID_SYSTEM_MAINTENANCE_INBOX_SOURCE')
    END;
END;

CREATE TRIGGER trg_inbox_system_maintenance_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, due_at, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'system_maintenance'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.due_at IS NOT OLD.due_at
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'SYSTEM_MAINTENANCE_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_system_maintenance_source_deleted_forbidden
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'system_maintenance'
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'SYSTEM_MAINTENANCE_INBOX_SOURCE_DELETE_FORBIDDEN');
END;
