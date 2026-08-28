ALTER TABLE tasks
ADD COLUMN kind TEXT NOT NULL DEFAULT 'work'
CHECK (kind IN ('work', 'review', 'followup', 'reminder'));

ALTER TABLE tasks
ADD COLUMN parent_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL
CHECK (parent_task_id IS NULL OR parent_task_id <> id);

ALTER TABLE tasks
ADD COLUMN completion_criteria TEXT NOT NULL DEFAULT ''
CHECK (length(completion_criteria) <= 10000);

ALTER TABLE tasks
ADD COLUMN version INTEGER NOT NULL DEFAULT 1
CHECK (version >= 1);

ALTER TABLE tags
ADD COLUMN version INTEGER NOT NULL DEFAULT 1
CHECK (version >= 1);

CREATE INDEX idx_tasks_kind ON tasks(kind);
CREATE INDEX idx_tasks_parent_task_id ON tasks(parent_task_id);
CREATE INDEX idx_task_tags_tag_id ON task_tags(tag_id);
CREATE INDEX idx_tasks_planned_manual_order ON tasks(planned_date, manual_order, id);

CREATE TRIGGER trg_tasks_parent_cycle_insert
BEFORE INSERT ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN NEW.parent_task_id = NEW.id THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
        WHEN EXISTS (
            WITH RECURSIVE ancestors(id, parent_task_id) AS (
                SELECT id, parent_task_id
                FROM tasks
                WHERE id = NEW.parent_task_id
                UNION
                SELECT tasks.id, tasks.parent_task_id
                FROM tasks
                JOIN ancestors ON tasks.id = ancestors.parent_task_id
            )
            SELECT 1 FROM ancestors WHERE id = NEW.id
        ) THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
    END;
END;

CREATE TRIGGER trg_tasks_parent_cycle_update
BEFORE UPDATE OF parent_task_id ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN NEW.parent_task_id = NEW.id THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
        WHEN EXISTS (
            WITH RECURSIVE ancestors(id, parent_task_id) AS (
                SELECT id, parent_task_id
                FROM tasks
                WHERE id = NEW.parent_task_id
                UNION
                SELECT tasks.id, tasks.parent_task_id
                FROM tasks
                JOIN ancestors ON tasks.id = ancestors.parent_task_id
            )
            SELECT 1 FROM ancestors WHERE id = NEW.id
        ) THEN RAISE(ABORT, 'TASK_PARENT_CYCLE')
    END;
END;

CREATE TRIGGER trg_tasks_parent_after_insert
AFTER INSERT ON tasks
WHEN NEW.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_delete
AFTER DELETE ON tasks
WHEN OLD.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = OLD.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_status_update
AFTER UPDATE OF status ON tasks
WHEN OLD.status IS NOT NEW.status AND NEW.parent_task_id IS NOT NULL
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_parent_update
AFTER UPDATE OF parent_task_id ON tasks
WHEN OLD.parent_task_id IS NOT NEW.parent_task_id
BEGIN
	-- API writes increment the edited task version in the same statement. Foreign-key
	-- ON DELETE SET NULL does not, so invalidate the detached child here exactly once.
	UPDATE tasks
	SET version = version + 1,
		updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
	WHERE id = NEW.id AND NEW.version = OLD.version;

    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE OLD.parent_task_id IS NOT NULL AND id = OLD.parent_task_id;

    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE NEW.parent_task_id IS NOT NULL AND id = NEW.parent_task_id;
END;

CREATE TRIGGER trg_tasks_parent_after_title_update
AFTER UPDATE OF title ON tasks
WHEN OLD.title IS NOT NEW.title
BEGIN
    UPDATE tasks
    SET version = version + 1,
        updated_at = STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'NOW')
    WHERE parent_task_id = NEW.id;
END;
