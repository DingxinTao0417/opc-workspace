CREATE TABLE app_settings (
    key TEXT PRIMARY KEY
        CHECK (key IN ('workspace', 'general', 'appearance', 'focus')),
    value_json TEXT NOT NULL
        CHECK (length(value_json) BETWEEN 2 AND 65536),
    schema_version INTEGER NOT NULL DEFAULT 1
        CHECK (schema_version = 1),
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    updated_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(updated_at) > 0)
);

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
