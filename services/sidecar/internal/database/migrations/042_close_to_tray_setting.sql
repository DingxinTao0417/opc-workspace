-- migration: foreign_keys=off
-- migration: destructive

ALTER TABLE app_settings RENAME TO app_settings_v41;

CREATE TABLE app_settings (
    key TEXT PRIMARY KEY
        CHECK (key IN ('workspace', 'general', 'appearance', 'focus', 'storage')),
    value_json TEXT NOT NULL
        CHECK (length(value_json) BETWEEN 2 AND 65536),
    schema_version INTEGER NOT NULL DEFAULT 2
        CHECK (schema_version = 2),
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    updated_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(updated_at) > 0)
);

INSERT INTO app_settings (
    key,
    value_json,
    schema_version,
    version,
    updated_by_actor_id,
    updated_at
)
SELECT
    key,
    CASE
        WHEN key = 'general'
             AND json_type(value_json, '$.close_to_tray') IS NULL
        THEN json_set(value_json, '$.close_to_tray', json('true'))
        ELSE value_json
    END,
    2,
    version,
    updated_by_actor_id,
    updated_at
FROM app_settings_v41;

DROP TABLE app_settings_v41;

CREATE INDEX idx_app_settings_updated
ON app_settings(updated_at DESC, key);

CREATE TRIGGER trg_app_settings_require_active_actor_insert
BEFORE INSERT ON app_settings
WHEN NOT EXISTS (
    SELECT 1 FROM actors
    WHERE id = NEW.updated_by_actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'APP_SETTING_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER trg_app_settings_require_active_actor_update
BEFORE UPDATE ON app_settings
WHEN NOT EXISTS (
    SELECT 1 FROM actors
    WHERE id = NEW.updated_by_actor_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'APP_SETTING_ACTOR_NOT_ACTIVE');
END;

CREATE TRIGGER trg_app_settings_protect_identity_update
BEFORE UPDATE ON app_settings
WHEN NEW.key IS NOT OLD.key
BEGIN
    SELECT RAISE(ABORT, 'APP_SETTING_KEY_IMMUTABLE');
END;

CREATE TRIGGER trg_app_settings_protect_hard_delete
BEFORE DELETE ON app_settings
BEGIN
    SELECT RAISE(ABORT, 'APP_SETTING_HARD_DELETE_FORBIDDEN');
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
