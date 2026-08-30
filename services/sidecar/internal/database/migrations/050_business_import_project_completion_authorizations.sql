CREATE TABLE business_import_project_completion_authorizations (
    inbox_item_id TEXT PRIMARY KEY
        CHECK (length(trim(inbox_item_id)) BETWEEN 1 AND 100 AND inbox_item_id = trim(inbox_item_id)),
    source_entity_type TEXT NOT NULL CHECK (source_entity_type = 'project_completion'),
    source_entity_id TEXT NOT NULL
        CHECK (length(trim(source_entity_id)) BETWEEN 1 AND 100 AND source_entity_id = trim(source_entity_id)),
    source_event_key TEXT NOT NULL UNIQUE
        CHECK (length(trim(source_event_key)) BETWEEN 1 AND 500 AND source_event_key = trim(source_event_key)),
    payload_json TEXT NOT NULL
        CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    source_deleted_at TEXT,
    CHECK (source_deleted_at IS NULL OR length(source_deleted_at) > 0)
);

DROP TRIGGER trg_inbox_project_completion_source_insert_guard;

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
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND NOT EXISTS (
                  SELECT 1
                  FROM business_import_project_completion_authorizations AS authorization
                  WHERE authorization.inbox_item_id = NEW.id
                    AND authorization.source_entity_type = NEW.source_entity_type
                    AND authorization.source_entity_id = NEW.source_entity_id
                    AND authorization.source_event_key = NEW.source_event_key
                    AND authorization.payload_json = NEW.payload_json
                    AND authorization.source_deleted_at IS NEW.source_deleted_at
              )
          )
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
        AND NOT EXISTS (
            SELECT 1
            FROM business_import_project_completion_authorizations AS authorization
            WHERE authorization.inbox_item_id = NEW.id
              AND authorization.source_entity_type = NEW.source_entity_type
              AND authorization.source_entity_id = NEW.source_entity_id
              AND authorization.source_event_key = NEW.source_event_key
              AND authorization.payload_json = NEW.payload_json
              AND authorization.source_deleted_at IS NEW.source_deleted_at
        )
        THEN RAISE(ABORT, 'PROJECT_COMPLETION_INBOX_SOURCE_NOT_CURRENT')
    END;
END;

CREATE TRIGGER trg_inbox_project_completion_import_authorization_consumed
AFTER INSERT ON inbox_items
WHEN NEW.source_entity_type = 'project_completion'
BEGIN
    DELETE FROM business_import_project_completion_authorizations
    WHERE inbox_item_id = NEW.id
      AND source_entity_type = NEW.source_entity_type
      AND source_entity_id = NEW.source_entity_id
      AND source_event_key = NEW.source_event_key
      AND payload_json = NEW.payload_json
      AND source_deleted_at IS NEW.source_deleted_at;
END;
