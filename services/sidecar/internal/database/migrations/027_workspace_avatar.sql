CREATE TABLE workspace_avatars (
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
    relative_path TEXT NOT NULL UNIQUE,
    extension TEXT NOT NULL
        CHECK (extension IN ('png', 'jpg', 'webp')),
    mime_type TEXT NOT NULL
        CHECK (mime_type IN ('image/png', 'image/jpeg', 'image/webp')),
    size_bytes INTEGER NOT NULL
        CHECK (size_bytes BETWEEN 1 AND 2097152),
    sha256 TEXT NOT NULL
        CHECK (length(sha256) = 64 AND sha256 = lower(sha256) AND sha256 NOT GLOB '*[^0-9a-f]*'),
    integrity_status TEXT NOT NULL DEFAULT 'verified'
        CHECK (integrity_status IN ('verified', 'missing', 'mismatch')),
    integrity_checked_at TEXT NOT NULL
        CHECK (length(integrity_checked_at) > 0),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(created_at) > 0),
    deleted_at TEXT,
    deletion_reason TEXT,
    CHECK (relative_path = 'avatars/' || id || '.' || extension),
    CHECK (
        (deleted_at IS NULL AND deletion_reason IS NULL)
        OR
        (deleted_at IS NOT NULL AND length(deleted_at) > 0 AND deletion_reason IS NOT NULL AND length(trim(deletion_reason)) BETWEEN 1 AND 500)
    )
);

CREATE UNIQUE INDEX idx_workspace_avatars_single_active
ON workspace_avatars((1))
WHERE deleted_at IS NULL;

CREATE INDEX idx_workspace_avatars_history
ON workspace_avatars(created_at DESC, id ASC);

CREATE TRIGGER trg_workspace_avatars_cross_domain_id_insert
BEFORE INSERT ON workspace_avatars
WHEN EXISTS (SELECT 1 FROM task_artifacts WHERE id = NEW.id)
    OR EXISTS (SELECT 1 FROM client_attachments WHERE id = NEW.id)
    OR EXISTS (SELECT 1 FROM project_attachments WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_FILE_ID_CONFLICT');
END;

CREATE TRIGGER trg_task_artifacts_workspace_avatar_id_insert
BEFORE INSERT ON task_artifacts
WHEN EXISTS (SELECT 1 FROM workspace_avatars WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_FILE_ID_CONFLICT');
END;

CREATE TRIGGER trg_client_attachments_workspace_avatar_id_insert
BEFORE INSERT ON client_attachments
WHEN EXISTS (SELECT 1 FROM workspace_avatars WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_FILE_ID_CONFLICT');
END;

CREATE TRIGGER trg_project_attachments_workspace_avatar_id_insert
BEFORE INSERT ON project_attachments
WHEN EXISTS (SELECT 1 FROM workspace_avatars WHERE id = NEW.id)
BEGIN
    SELECT RAISE(ABORT, 'CONTROLLED_FILE_ID_CONFLICT');
END;

CREATE TABLE workspace_avatar_deletion_tombstones (
    avatar_id TEXT PRIMARY KEY,
    relative_path TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 2097152),
    sha256 TEXT NOT NULL
        CHECK (length(sha256) = 64 AND sha256 = lower(sha256) AND sha256 NOT GLOB '*[^0-9a-f]*'),
    reason TEXT NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 500),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0)
);

CREATE TRIGGER trg_workspace_avatars_identity_immutable
BEFORE UPDATE ON workspace_avatars
WHEN NEW.id IS NOT OLD.id
    OR NEW.relative_path IS NOT OLD.relative_path
    OR NEW.extension IS NOT OLD.extension
    OR NEW.mime_type IS NOT OLD.mime_type
    OR NEW.size_bytes IS NOT OLD.size_bytes
    OR NEW.sha256 IS NOT OLD.sha256
    OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_workspace_avatars_terminal_immutable
BEFORE UPDATE ON workspace_avatars
WHEN OLD.deleted_at IS NOT NULL
    AND (NEW.deleted_at IS NOT OLD.deleted_at OR NEW.deletion_reason IS NOT OLD.deletion_reason)
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_DELETION_IMMUTABLE');
END;

CREATE TRIGGER trg_workspace_avatars_require_tombstone
BEFORE UPDATE OF deleted_at ON workspace_avatars
WHEN OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL
    AND NOT EXISTS (
        SELECT 1 FROM workspace_avatar_deletion_tombstones tombstone
        WHERE tombstone.avatar_id = OLD.id
          AND tombstone.relative_path = OLD.relative_path
          AND tombstone.size_bytes = OLD.size_bytes
          AND tombstone.sha256 = OLD.sha256
    )
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_TOMBSTONE_REQUIRED');
END;

CREATE TRIGGER trg_workspace_avatars_protect_hard_delete
BEFORE DELETE ON workspace_avatars
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_HARD_DELETE_FORBIDDEN');
END;

CREATE TRIGGER trg_workspace_avatar_tombstones_match_insert
BEFORE INSERT ON workspace_avatar_deletion_tombstones
WHEN NOT EXISTS (
    SELECT 1 FROM workspace_avatars avatar
    WHERE avatar.id = NEW.avatar_id
      AND avatar.relative_path = NEW.relative_path
      AND avatar.size_bytes = NEW.size_bytes
      AND avatar.sha256 = NEW.sha256
      AND avatar.deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_TOMBSTONE_MISMATCH');
END;

CREATE TRIGGER trg_workspace_avatar_tombstones_immutable_update
BEFORE UPDATE ON workspace_avatar_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER trg_workspace_avatar_tombstones_immutable_delete
BEFORE DELETE ON workspace_avatar_deletion_tombstones
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_TOMBSTONE_IMMUTABLE');
END;

CREATE TRIGGER trg_app_settings_workspace_avatar_exists_insert
BEFORE INSERT ON app_settings
WHEN NEW.key = 'workspace'
    AND json_extract(NEW.value_json, '$.avatar_ref') IS NOT NULL
    AND NOT EXISTS (
        SELECT 1 FROM workspace_avatars avatar
        WHERE avatar.relative_path = json_extract(NEW.value_json, '$.avatar_ref')
          AND avatar.deleted_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_NOT_ACTIVE');
END;

CREATE TRIGGER trg_app_settings_workspace_avatar_exists_update
BEFORE UPDATE OF value_json ON app_settings
WHEN NEW.key = 'workspace'
    AND json_extract(NEW.value_json, '$.avatar_ref') IS NOT NULL
    AND NOT EXISTS (
        SELECT 1 FROM workspace_avatars avatar
        WHERE avatar.relative_path = json_extract(NEW.value_json, '$.avatar_ref')
          AND avatar.deleted_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'WORKSPACE_AVATAR_NOT_ACTIVE');
END;
