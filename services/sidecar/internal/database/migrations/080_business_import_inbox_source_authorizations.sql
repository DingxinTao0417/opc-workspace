-- A reminder that the system projected for a Task, a Content Item or a Roadmap
-- Milestone is only accepted while its source is still in the projected state,
-- and never with a deleted source. That is right for the live projections and
-- wrong for an import, which writes rows in their final state: a workspace
-- whose Task was unblocked, completed, rescheduled or deleted after the
-- reminder appeared could not be imported at all.
--
-- Like schema 50 did for Project completion, the import now names every such
-- row in an operational table after it has checked the row's history, and the
-- insert guards accept exactly the named rows. An authorization is consumed by
-- the insert it allows. Rows written without one are checked as before.
CREATE TABLE business_import_inbox_source_authorizations (
    inbox_item_id TEXT PRIMARY KEY
        CHECK (length(trim(inbox_item_id)) BETWEEN 1 AND 100 AND inbox_item_id = trim(inbox_item_id)),
    source_entity_type TEXT NOT NULL
        CHECK (source_entity_type IN ('task', 'task_due', 'content_item', 'roadmap_milestone')),
    source_entity_id TEXT NOT NULL
        CHECK (length(trim(source_entity_id)) BETWEEN 1 AND 100 AND source_entity_id = trim(source_entity_id)),
    source_event_key TEXT NOT NULL UNIQUE
        CHECK (length(trim(source_event_key)) BETWEEN 1 AND 500 AND source_event_key = trim(source_event_key)),
    payload_json TEXT NOT NULL
        CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    source_deleted_at TEXT,
    CHECK (source_deleted_at IS NULL OR length(source_deleted_at) > 0)
);

-- The comparison with the live source used to cover fields that the shape
-- check let through when they were missing (NULL <> 'text' is not true). An
-- authorized row skips that comparison, so the shape checks below are NULL-safe.

DROP TRIGGER trg_inbox_task_blocked_source_insert_guard;

CREATE TRIGGER trg_inbox_task_blocked_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'task'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR json_type(NEW.payload_json, '$.task_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.task_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.task_title') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.task_title'))) = 0
          OR json_type(NEW.payload_json, '$.blocked_reason') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.blocked_reason'))) = 0
          OR json_type(NEW.payload_json, '$.blocked_at') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.blocked_from_status') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.blocked_from_status') NOT IN ('todo', 'in_progress', 'waiting_review')
          OR json_type(NEW.payload_json, '$.block_version') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.block_version') < 2
          OR NEW.source_event_key IS NOT 'task:' || NEW.source_entity_id || ':blocked:' || json_extract(NEW.payload_json, '$.block_version')
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND (
                  julianday(NEW.source_deleted_at) IS NULL
                  OR NOT EXISTS (
                      SELECT 1
                      FROM business_import_inbox_source_authorizations AS authorization
                      WHERE authorization.inbox_item_id = NEW.id
                        AND authorization.source_entity_type = NEW.source_entity_type
                        AND authorization.source_entity_id = NEW.source_entity_id
                        AND authorization.source_event_key = NEW.source_event_key
                        AND authorization.payload_json = NEW.payload_json
                        AND authorization.source_deleted_at IS NEW.source_deleted_at
                  )
              )
          )
        THEN RAISE(ABORT, 'INVALID_TASK_BLOCKED_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM tasks
              WHERE id = NEW.source_entity_id
                AND status = 'blocked'
                AND version = json_extract(NEW.payload_json, '$.block_version')
                AND blocked_reason = json_extract(NEW.payload_json, '$.blocked_reason')
                AND blocked_at = json_extract(NEW.payload_json, '$.blocked_at')
                AND blocked_from_status = json_extract(NEW.payload_json, '$.blocked_from_status')
          )
          AND NOT (
              EXISTS (SELECT 1 FROM tasks WHERE id = NEW.source_entity_id)
              AND EXISTS (
                  SELECT 1
                  FROM business_import_inbox_source_authorizations AS authorization
                  WHERE authorization.inbox_item_id = NEW.id
                    AND authorization.source_entity_type = NEW.source_entity_type
                    AND authorization.source_entity_id = NEW.source_entity_id
                    AND authorization.source_event_key = NEW.source_event_key
                    AND authorization.payload_json = NEW.payload_json
                    AND authorization.source_deleted_at IS NULL
              )
          )
        THEN RAISE(ABORT, 'TASK_BLOCKED_INBOX_SOURCE_NOT_CURRENT')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NOT NULL
          AND EXISTS (SELECT 1 FROM tasks WHERE id = NEW.source_entity_id)
        THEN RAISE(ABORT, 'TASK_BLOCKED_DELETED_SOURCE_COLLISION')
    END;
END;

DROP TRIGGER trg_inbox_task_due_source_insert_guard;

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
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND (
                  julianday(NEW.source_deleted_at) IS NULL
                  OR NOT EXISTS (
                      SELECT 1
                      FROM business_import_inbox_source_authorizations AS authorization
                      WHERE authorization.inbox_item_id = NEW.id
                        AND authorization.source_entity_type = NEW.source_entity_type
                        AND authorization.source_entity_id = NEW.source_entity_id
                        AND authorization.source_event_key = NEW.source_event_key
                        AND authorization.payload_json = NEW.payload_json
                        AND authorization.source_deleted_at IS NEW.source_deleted_at
                  )
              )
          )
        THEN RAISE(ABORT, 'INVALID_TASK_DUE_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM tasks
              WHERE id = NEW.source_entity_id
                AND status NOT IN ('done', 'cancelled')
                AND julianday(due_date) = julianday(json_extract(NEW.payload_json, '$.due_at'))
          )
          AND NOT (
              EXISTS (SELECT 1 FROM tasks WHERE id = NEW.source_entity_id)
              AND EXISTS (
                  SELECT 1
                  FROM business_import_inbox_source_authorizations AS authorization
                  WHERE authorization.inbox_item_id = NEW.id
                    AND authorization.source_entity_type = NEW.source_entity_type
                    AND authorization.source_entity_id = NEW.source_entity_id
                    AND authorization.source_event_key = NEW.source_event_key
                    AND authorization.payload_json = NEW.payload_json
                    AND authorization.source_deleted_at IS NULL
              )
          )
        THEN RAISE(ABORT, 'TASK_DUE_INBOX_SOURCE_NOT_CURRENT')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NOT NULL
          AND EXISTS (SELECT 1 FROM tasks WHERE id = NEW.source_entity_id)
        THEN RAISE(ABORT, 'TASK_DUE_DELETED_SOURCE_COLLISION')
    END;
END;

DROP TRIGGER content_item_inbox_source_insert_guard;

CREATE TRIGGER content_item_inbox_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'content_item'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR json_type(NEW.payload_json, '$.content_item_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.content_item_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.event_type') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.event_type') NOT IN ('review_due', 'publish_due')
          OR json_type(NEW.payload_json, '$.content_version') IS NOT 'integer'
          OR json_type(NEW.payload_json, '$.scheduled_at') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.scheduled_timezone') IS NOT 'text'
          OR NEW.source_event_key IS NOT 'content:' || NEW.source_entity_id || ':' || json_extract(NEW.payload_json, '$.event_type') || ':' || json_extract(NEW.payload_json, '$.content_version')
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND (
                  julianday(NEW.source_deleted_at) IS NULL
                  OR NOT EXISTS (
                      SELECT 1
                      FROM business_import_inbox_source_authorizations AS authorization
                      WHERE authorization.inbox_item_id = NEW.id
                        AND authorization.source_entity_type = NEW.source_entity_type
                        AND authorization.source_entity_id = NEW.source_entity_id
                        AND authorization.source_event_key = NEW.source_event_key
                        AND authorization.payload_json = NEW.payload_json
                        AND authorization.source_deleted_at IS NEW.source_deleted_at
                  )
              )
          )
        THEN RAISE(ABORT, 'INVALID_CONTENT_ITEM_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NULL
          AND NOT EXISTS (
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
          AND NOT (
              EXISTS (SELECT 1 FROM content_items WHERE id = NEW.source_entity_id)
              AND EXISTS (
                  SELECT 1
                  FROM business_import_inbox_source_authorizations AS authorization
                  WHERE authorization.inbox_item_id = NEW.id
                    AND authorization.source_entity_type = NEW.source_entity_type
                    AND authorization.source_entity_id = NEW.source_entity_id
                    AND authorization.source_event_key = NEW.source_event_key
                    AND authorization.payload_json = NEW.payload_json
                    AND authorization.source_deleted_at IS NULL
              )
          )
        THEN RAISE(ABORT, 'CONTENT_ITEM_INBOX_SOURCE_NOT_FOUND')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NOT NULL
          AND EXISTS (SELECT 1 FROM content_items WHERE id = NEW.source_entity_id)
        THEN RAISE(ABORT, 'CONTENT_ITEM_DELETED_SOURCE_COLLISION')
    END;
END;

DROP TRIGGER roadmap_milestone_inbox_source_insert_guard;

CREATE TRIGGER roadmap_milestone_inbox_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'roadmap_milestone'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR json_type(NEW.payload_json, '$.roadmap_milestone_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.roadmap_milestone_id') IS NOT NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.event_type') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.event_type') NOT IN ('due', 'achieved')
          OR json_type(NEW.payload_json, '$.milestone_version') IS NOT 'integer'
          OR json_type(NEW.payload_json, '$.target_date') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.year') IS NOT 'integer'
          OR json_type(NEW.payload_json, '$.quarter') IS NOT 'integer'
          OR NEW.source_event_key IS NOT 'roadmap:' || NEW.source_entity_id || ':' || json_extract(NEW.payload_json, '$.event_type') || ':' || json_extract(NEW.payload_json, '$.milestone_version')
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND (
                  julianday(NEW.source_deleted_at) IS NULL
                  OR NOT EXISTS (
                      SELECT 1
                      FROM business_import_inbox_source_authorizations AS authorization
                      WHERE authorization.inbox_item_id = NEW.id
                        AND authorization.source_entity_type = NEW.source_entity_type
                        AND authorization.source_entity_id = NEW.source_entity_id
                        AND authorization.source_event_key = NEW.source_event_key
                        AND authorization.payload_json = NEW.payload_json
                        AND authorization.source_deleted_at IS NEW.source_deleted_at
                  )
              )
          )
        THEN RAISE(ABORT, 'INVALID_ROADMAP_MILESTONE_INBOX_SOURCE')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NULL
          AND NOT EXISTS (
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
          AND NOT (
              EXISTS (SELECT 1 FROM roadmap_milestones WHERE id = NEW.source_entity_id)
              AND EXISTS (
                  SELECT 1
                  FROM business_import_inbox_source_authorizations AS authorization
                  WHERE authorization.inbox_item_id = NEW.id
                    AND authorization.source_entity_type = NEW.source_entity_type
                    AND authorization.source_entity_id = NEW.source_entity_id
                    AND authorization.source_event_key = NEW.source_event_key
                    AND authorization.payload_json = NEW.payload_json
                    AND authorization.source_deleted_at IS NULL
              )
          )
        THEN RAISE(ABORT, 'ROADMAP_MILESTONE_INBOX_SOURCE_NOT_FOUND')
    END;
    SELECT CASE
        WHEN NEW.source_deleted_at IS NOT NULL
          AND EXISTS (SELECT 1 FROM roadmap_milestones WHERE id = NEW.source_entity_id)
        THEN RAISE(ABORT, 'ROADMAP_MILESTONE_DELETED_SOURCE_COLLISION')
    END;
END;

CREATE TRIGGER trg_inbox_source_import_authorization_consumed
AFTER INSERT ON inbox_items
WHEN NEW.source_entity_type IN ('task', 'task_due', 'content_item', 'roadmap_milestone')
BEGIN
    DELETE FROM business_import_inbox_source_authorizations
    WHERE inbox_item_id = NEW.id
      AND source_entity_type = NEW.source_entity_type
      AND source_entity_id = NEW.source_entity_id
      AND source_event_key = NEW.source_event_key
      AND payload_json = NEW.payload_json
      AND source_deleted_at IS NEW.source_deleted_at;
END;
