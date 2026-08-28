CREATE INDEX idx_inbox_item_tasks_required_task_active
ON inbox_item_tasks(task_id, inbox_item_id)
WHERE unlinked_at IS NULL AND is_required = 1;

CREATE INDEX idx_inbox_item_tasks_required_inbox_active
ON inbox_item_tasks(inbox_item_id, task_id)
WHERE unlinked_at IS NULL AND is_required = 1;

CREATE TRIGGER trg_inbox_items_validate_automatic_resolution_insert
BEFORE INSERT ON inbox_items
WHEN NEW.resolution_mode = 'automatic'
  AND (
      NEW.status <> 'resolved'
      OR NEW.resolution_policy <> 'all_required_tasks_done'
      OR NEW.resolved_by_actor_id <> '00000000-0000-5000-8000-000000000002'
      OR NOT EXISTS (
          SELECT 1
          FROM inbox_item_tasks relation
          JOIN tasks task ON task.id = relation.task_id
          WHERE relation.inbox_item_id = NEW.id
            AND relation.unlinked_at IS NULL
            AND relation.is_required = 1
      )
      OR EXISTS (
          SELECT 1
          FROM inbox_item_tasks relation
          JOIN tasks task ON task.id = relation.task_id
          WHERE relation.inbox_item_id = NEW.id
            AND relation.unlinked_at IS NULL
            AND relation.is_required = 1
            AND task.status <> 'done'
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'INBOX_AUTOMATIC_RESOLUTION_INVALID');
END;

CREATE TRIGGER trg_inbox_items_validate_automatic_resolution_update
BEFORE UPDATE ON inbox_items
WHEN NEW.resolution_mode = 'automatic'
  AND (
      NEW.status <> 'resolved'
      OR NEW.resolution_policy <> 'all_required_tasks_done'
      OR NEW.resolved_by_actor_id <> '00000000-0000-5000-8000-000000000002'
      OR NOT EXISTS (
          SELECT 1
          FROM inbox_item_tasks relation
          JOIN tasks task ON task.id = relation.task_id
          WHERE relation.inbox_item_id = NEW.id
            AND relation.unlinked_at IS NULL
            AND relation.is_required = 1
      )
      OR EXISTS (
          SELECT 1
          FROM inbox_item_tasks relation
          JOIN tasks task ON task.id = relation.task_id
          WHERE relation.inbox_item_id = NEW.id
            AND relation.unlinked_at IS NULL
            AND relation.is_required = 1
            AND task.status <> 'done'
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'INBOX_AUTOMATIC_RESOLUTION_INVALID');
END;
