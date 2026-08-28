CREATE TABLE project_notes (
    id TEXT PRIMARY KEY
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND id NOT GLOB '*[^0-9a-f-]*'
        ),
    project_id TEXT NOT NULL
        REFERENCES projects(id) ON DELETE CASCADE,
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 200),
    body TEXT NOT NULL
        CHECK (length(trim(body)) BETWEEN 1 AND 10000),
    occurred_at TEXT NOT NULL
        CHECK (length(occurred_at) > 0),
    created_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    deleted_at TEXT,
    deleted_by_actor_id TEXT
        REFERENCES actors(id) ON DELETE RESTRICT,
    delete_reason TEXT
        CHECK (delete_reason IS NULL OR length(trim(delete_reason)) BETWEEN 1 AND 1000),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(updated_at) > 0),
    CHECK (
        (deleted_at IS NULL AND deleted_by_actor_id IS NULL AND delete_reason IS NULL)
        OR
        (deleted_at IS NOT NULL AND deleted_by_actor_id IS NOT NULL AND delete_reason IS NOT NULL)
    )
);

CREATE INDEX idx_project_notes_timeline
ON project_notes(project_id, deleted_at, occurred_at DESC, id ASC);

CREATE TRIGGER project_notes_immutable_identity
BEFORE UPDATE OF project_id, created_by_actor_id, created_at ON project_notes
FOR EACH ROW
WHEN NEW.project_id IS NOT OLD.project_id
  OR NEW.created_by_actor_id IS NOT OLD.created_by_actor_id
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'project note identity is immutable');
END;

CREATE TRIGGER project_notes_terminal_delete
BEFORE UPDATE ON project_notes
FOR EACH ROW
WHEN OLD.deleted_at IS NOT NULL AND (
    NEW.title IS NOT OLD.title
    OR NEW.body IS NOT OLD.body
    OR NEW.occurred_at IS NOT OLD.occurred_at
    OR NEW.version IS NOT OLD.version
    OR NEW.deleted_at IS NOT OLD.deleted_at
    OR NEW.deleted_by_actor_id IS NOT OLD.deleted_by_actor_id
    OR NEW.delete_reason IS NOT OLD.delete_reason
    OR NEW.updated_at IS NOT OLD.updated_at
)
BEGIN
    SELECT RAISE(ABORT, 'deleted project note is immutable');
END;

CREATE TRIGGER project_notes_bump_project_after_insert
AFTER INSERT ON project_notes
FOR EACH ROW
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.project_id;
END;

CREATE TRIGGER project_notes_bump_project_after_update
AFTER UPDATE ON project_notes
FOR EACH ROW
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.project_id;
END;
