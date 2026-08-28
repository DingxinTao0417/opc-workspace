CREATE TRIGGER client_activities_protect_member_delete
BEFORE DELETE ON client_activities
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM clients
    WHERE id = OLD.client_id
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ACTIVITY_HARD_DELETE_FORBIDDEN');
END;

CREATE TABLE client_attachments (
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
    client_id TEXT NOT NULL
        REFERENCES clients(id) ON DELETE CASCADE,
    activity_id TEXT
        REFERENCES client_activities(id) ON DELETE CASCADE,
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

CREATE TABLE client_attachment_deletion_tombstones (
    attachment_id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL CHECK (length(client_id) > 0),
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
        CHECK (deletion_scope IN ('attachment', 'client')),
    deleted_at TEXT NOT NULL CHECK (length(deleted_at) > 0)
);

CREATE INDEX idx_client_attachments_timeline
ON client_attachments(client_id, deleted_at, created_at DESC, id ASC);

CREATE INDEX idx_client_attachments_activity
ON client_attachments(activity_id, created_at DESC, id ASC)
WHERE activity_id IS NOT NULL;

CREATE INDEX idx_client_attachments_integrity
ON client_attachments(integrity_status, created_at DESC, id ASC)
WHERE deleted_at IS NULL;

CREATE INDEX idx_client_attachment_tombstones_client
ON client_attachment_deletion_tombstones(client_id, deleted_at DESC, attachment_id);

CREATE TRIGGER client_attachments_activity_same_client_insert
BEFORE INSERT ON client_attachments
FOR EACH ROW
WHEN NEW.activity_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM client_activities
      WHERE id = NEW.activity_id
        AND client_id = NEW.client_id
        AND deleted_at IS NULL
  )
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ATTACHMENT_ACTIVITY_MISMATCH');
END;

CREATE TRIGGER client_attachments_unique_controlled_object_insert
BEFORE INSERT ON client_attachments
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM task_artifacts
    WHERE id = NEW.id
)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_OBJECT_ID_CONFLICT');
END;

CREATE TRIGGER task_artifacts_unique_controlled_object_insert
BEFORE INSERT ON task_artifacts
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM client_attachments
    WHERE id = NEW.id
)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_OBJECT_ID_CONFLICT');
END;

CREATE TRIGGER client_attachments_protect_facts_update
BEFORE UPDATE ON client_attachments
FOR EACH ROW
WHEN NEW.id IS NOT OLD.id
  OR NEW.client_id IS NOT OLD.client_id
  OR NEW.activity_id IS NOT OLD.activity_id
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
    SELECT RAISE(ABORT, 'CLIENT_ATTACHMENT_FACTS_IMMUTABLE');
END;

CREATE TRIGGER client_attachments_protect_member_delete
BEFORE DELETE ON client_attachments
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM clients
    WHERE id = OLD.client_id
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ATTACHMENT_HARD_DELETE_FORBIDDEN');
END;

CREATE TRIGGER client_attachment_tombstones_immutable_update
BEFORE UPDATE ON client_attachment_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ATTACHMENT_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER client_attachment_tombstones_immutable_delete
BEFORE DELETE ON client_attachment_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ATTACHMENT_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER client_attachments_bump_client_after_insert
AFTER INSERT ON client_attachments
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER client_attachments_bump_client_after_delete_state
AFTER UPDATE OF deleted_at ON client_attachments
FOR EACH ROW
WHEN NEW.deleted_at IS NOT OLD.deleted_at
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;
