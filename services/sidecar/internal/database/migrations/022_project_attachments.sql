CREATE TABLE project_attachments (
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
    name TEXT NOT NULL
        CHECK (name = trim(name) AND length(name) BETWEEN 1 AND 255),
    relative_path TEXT NOT NULL UNIQUE
        CHECK (relative_path = 'objects/' || id),
    mime_type TEXT NOT NULL
        CHECK (mime_type = trim(mime_type) AND length(mime_type) BETWEEN 1 AND 255),
    size_bytes INTEGER NOT NULL
        CHECK (size_bytes BETWEEN 1 AND 52428800),
    sha256 TEXT NOT NULL
        CHECK (
            length(sha256) = 64
            AND sha256 = lower(sha256)
            AND sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    recorded_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    integrity_status TEXT NOT NULL DEFAULT 'verified'
        CHECK (integrity_status IN ('verified', 'missing', 'mismatch')),
    integrity_checked_at TEXT NOT NULL
        CHECK (length(integrity_checked_at) > 0),
    deleted_at TEXT
        CHECK (deleted_at IS NULL OR length(deleted_at) > 0),
    deleted_by_actor_id TEXT
        REFERENCES actors(id) ON DELETE RESTRICT,
    delete_reason TEXT
        CHECK (delete_reason IS NULL OR length(trim(delete_reason)) BETWEEN 1 AND 1000),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(created_at) > 0),
    CHECK (
        (deleted_at IS NULL AND deleted_by_actor_id IS NULL AND delete_reason IS NULL)
        OR
        (deleted_at IS NOT NULL AND deleted_by_actor_id IS NOT NULL AND delete_reason IS NOT NULL)
    )
);

CREATE TABLE project_attachment_deletion_tombstones (
    attachment_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL CHECK (length(project_id) > 0),
    relative_path TEXT NOT NULL UNIQUE
        CHECK (relative_path = 'objects/' || attachment_id),
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    sha256 TEXT NOT NULL
        CHECK (
            length(sha256) = 64
            AND sha256 = lower(sha256)
            AND sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    deletion_scope TEXT NOT NULL
        CHECK (deletion_scope IN ('attachment', 'project')),
    deleted_at TEXT NOT NULL CHECK (length(deleted_at) > 0)
);

CREATE INDEX idx_project_attachments_timeline
ON project_attachments(project_id, deleted_at, created_at DESC, id ASC);

CREATE INDEX idx_project_attachments_integrity
ON project_attachments(integrity_status, created_at DESC, id ASC)
WHERE deleted_at IS NULL;

CREATE INDEX idx_project_attachment_tombstones_project
ON project_attachment_deletion_tombstones(project_id, deleted_at DESC, attachment_id);

CREATE TRIGGER project_attachments_unique_controlled_object_insert
BEFORE INSERT ON project_attachments
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM task_artifacts WHERE id = NEW.id)
  OR EXISTS (SELECT 1 FROM client_attachments WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_OBJECT_ID_CONFLICT');
END;

CREATE TRIGGER task_artifacts_unique_project_object_insert
BEFORE INSERT ON task_artifacts
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM project_attachments WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_OBJECT_ID_CONFLICT');
END;

CREATE TRIGGER client_attachments_unique_project_object_insert
BEFORE INSERT ON client_attachments
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM project_attachments WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_OBJECT_ID_CONFLICT');
END;

CREATE TRIGGER project_attachments_protect_facts_update
BEFORE UPDATE ON project_attachments
FOR EACH ROW
WHEN NEW.id IS NOT OLD.id
  OR NEW.project_id IS NOT OLD.project_id
  OR NEW.name IS NOT OLD.name
  OR NEW.relative_path IS NOT OLD.relative_path
  OR NEW.mime_type IS NOT OLD.mime_type
  OR NEW.size_bytes IS NOT OLD.size_bytes
  OR NEW.sha256 IS NOT OLD.sha256
  OR NEW.recorded_by_actor_id IS NOT OLD.recorded_by_actor_id
  OR NEW.created_at IS NOT OLD.created_at
  OR (
      OLD.deleted_at IS NOT NULL
      AND (
          NEW.deleted_at IS NOT OLD.deleted_at
          OR NEW.deleted_by_actor_id IS NOT OLD.deleted_by_actor_id
          OR NEW.delete_reason IS NOT OLD.delete_reason
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_ATTACHMENT_FACTS_IMMUTABLE');
END;

CREATE TRIGGER project_attachments_protect_member_delete
BEFORE DELETE ON project_attachments
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM projects WHERE id = OLD.project_id)
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_ATTACHMENT_HARD_DELETE_FORBIDDEN');
END;

CREATE TRIGGER project_attachment_tombstones_immutable_update
BEFORE UPDATE ON project_attachment_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_ATTACHMENT_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER project_attachment_tombstones_immutable_delete
BEFORE DELETE ON project_attachment_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_ATTACHMENT_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER project_attachments_bump_project_after_insert
AFTER INSERT ON project_attachments
FOR EACH ROW
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.project_id;
END;

CREATE TRIGGER project_attachments_bump_project_after_delete_state
AFTER UPDATE OF deleted_at ON project_attachments
FOR EACH ROW
WHEN NEW.deleted_at IS NOT OLD.deleted_at
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.project_id;
END;
