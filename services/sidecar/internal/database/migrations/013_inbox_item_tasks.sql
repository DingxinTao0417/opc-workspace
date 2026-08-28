CREATE TABLE inbox_item_tasks (
    id TEXT PRIMARY KEY,
    inbox_item_id TEXT NOT NULL REFERENCES inbox_items(id) ON DELETE CASCADE,
    task_ref_id TEXT NOT NULL
        CHECK (length(trim(task_ref_id)) > 0 AND task_ref_id = trim(task_ref_id)),
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    task_title_snapshot TEXT NOT NULL
        CHECK (
            length(trim(task_title_snapshot)) BETWEEN 2 AND 200
            AND task_title_snapshot = trim(task_title_snapshot)
        ),
    relation_type TEXT NOT NULL DEFAULT 'linked'
        CHECK (relation_type IN ('linked', 'created')),
    is_required INTEGER NOT NULL DEFAULT 1 CHECK (is_required IN (0, 1)),
    position INTEGER NOT NULL CHECK (position > 0),
    linked_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    linked_at TEXT NOT NULL CHECK (length(linked_at) > 0),
    unlinked_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    unlinked_at TEXT CHECK (unlinked_at IS NULL OR length(unlinked_at) > 0),
    unlink_reason TEXT
        CHECK (unlink_reason IS NULL OR length(trim(unlink_reason)) BETWEEN 1 AND 1000),
    CHECK (task_id IS NULL OR task_id = task_ref_id),
    CHECK (
        (
            unlinked_at IS NULL
            AND task_id IS NOT NULL
            AND unlinked_by_actor_id IS NULL
            AND unlink_reason IS NULL
        )
        OR (
            unlinked_at IS NOT NULL
            AND unlinked_by_actor_id IS NOT NULL
            AND unlink_reason IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX ux_inbox_item_tasks_active_pair
ON inbox_item_tasks(inbox_item_id, task_ref_id)
WHERE unlinked_at IS NULL;

CREATE UNIQUE INDEX ux_inbox_item_tasks_active_position
ON inbox_item_tasks(inbox_item_id, position)
WHERE unlinked_at IS NULL;

CREATE INDEX idx_inbox_item_tasks_inbox_history
ON inbox_item_tasks(inbox_item_id, unlinked_at, linked_at DESC, id);

CREATE INDEX idx_inbox_item_tasks_task_active
ON inbox_item_tasks(task_id, inbox_item_id)
WHERE unlinked_at IS NULL;

CREATE INDEX idx_inbox_item_tasks_linked_actor
ON inbox_item_tasks(linked_by_actor_id, linked_at DESC, id);

CREATE INDEX idx_inbox_item_tasks_unlinked_actor
ON inbox_item_tasks(unlinked_by_actor_id, unlinked_at DESC, id)
WHERE unlinked_by_actor_id IS NOT NULL;

CREATE TRIGGER trg_inbox_item_tasks_require_active_inbox_insert
BEFORE INSERT ON inbox_item_tasks
WHEN NOT EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE id = NEW.inbox_item_id
      AND status IN ('open', 'tracking')
)
BEGIN
    SELECT RAISE(ABORT, 'INBOX_ITEM_TERMINAL');
END;

CREATE TRIGGER trg_inbox_item_tasks_require_live_task_insert
BEFORE INSERT ON inbox_item_tasks
WHEN NEW.task_id IS NULL
  OR NEW.task_id <> NEW.task_ref_id
  OR NOT EXISTS (
      SELECT 1
      FROM tasks
      WHERE id = NEW.task_id
  )
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_NOT_FOUND');
END;

CREATE TRIGGER trg_inbox_item_tasks_require_active_linker_insert
BEFORE INSERT ON inbox_item_tasks
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.linked_by_actor_id
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'INBOX_RELATION_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER trg_inbox_item_tasks_protect_identity_update
BEFORE UPDATE ON inbox_item_tasks
WHEN NEW.id IS NOT OLD.id
  OR NEW.inbox_item_id IS NOT OLD.inbox_item_id
  OR NEW.task_ref_id IS NOT OLD.task_ref_id
  OR NEW.task_title_snapshot IS NOT OLD.task_title_snapshot
  OR NEW.relation_type IS NOT OLD.relation_type
  OR NEW.position IS NOT OLD.position
  OR NEW.linked_by_actor_id IS NOT OLD.linked_by_actor_id
  OR NEW.linked_at IS NOT OLD.linked_at
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_RELATION_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_item_tasks_protect_task_reference_update
BEFORE UPDATE OF task_id ON inbox_item_tasks
WHEN NEW.task_id IS NOT OLD.task_id
  AND NOT (
      OLD.task_id IS NOT NULL
      AND NEW.task_id IS NULL
      AND OLD.unlinked_at IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM tasks
          WHERE id = OLD.task_id
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_RELATION_TASK_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_item_tasks_protect_required_update
BEFORE UPDATE OF is_required ON inbox_item_tasks
WHEN NEW.is_required IS NOT OLD.is_required
  AND OLD.unlinked_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_RELATION_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_item_tasks_protect_unlink_update
BEFORE UPDATE OF unlinked_by_actor_id, unlinked_at, unlink_reason ON inbox_item_tasks
WHEN NOT (
    (
        NEW.unlinked_by_actor_id IS OLD.unlinked_by_actor_id
        AND NEW.unlinked_at IS OLD.unlinked_at
        AND NEW.unlink_reason IS OLD.unlink_reason
    )
    OR (
        OLD.unlinked_by_actor_id IS NULL
        AND OLD.unlinked_at IS NULL
        AND OLD.unlink_reason IS NULL
        AND NEW.unlinked_by_actor_id IS NOT NULL
        AND NEW.unlinked_at IS NOT NULL
        AND NEW.unlink_reason IS NOT NULL
    )
)
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_RELATION_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER trg_inbox_item_tasks_protect_member_delete
BEFORE DELETE ON inbox_item_tasks
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE id = OLD.inbox_item_id
)
BEGIN
    SELECT RAISE(ABORT, 'INBOX_TASK_RELATION_HARD_DELETE_FORBIDDEN');
END;

CREATE TRIGGER trg_tasks_prevent_active_inbox_relation_delete
BEFORE DELETE ON tasks
WHEN EXISTS (
    SELECT 1
    FROM inbox_item_tasks
    WHERE task_id = OLD.id
      AND unlinked_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'TASK_HAS_ACTIVE_INBOX_RELATIONS');
END;
