CREATE TABLE ai_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
        CHECK (length(trim(name)) BETWEEN 1 AND 100 AND name = trim(name)),
    protocol TEXT NOT NULL
        CHECK (protocol IN ('openai_chat', 'anthropic_messages')),
    base_url TEXT NOT NULL
        CHECK (
            length(trim(base_url)) BETWEEN 8 AND 500
            AND base_url = trim(base_url)
            AND (
                base_url LIKE 'https://%'
                OR base_url LIKE 'http://127.0.0.1%'
                OR base_url LIKE 'http://localhost%'
            )
        ),
    model TEXT NOT NULL
        CHECK (length(trim(model)) BETWEEN 1 AND 200 AND model = trim(model)),
    status TEXT NOT NULL DEFAULT 'unconfigured'
        CHECK (status IN ('unconfigured', 'checking', 'ready', 'unavailable', 'disabled')),
    health_status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (health_status IN ('unknown', 'healthy', 'unhealthy')),
    health_error_code TEXT
        CHECK (health_error_code IS NULL OR length(health_error_code) BETWEEN 1 AND 100),
    has_key INTEGER NOT NULL DEFAULT 0 CHECK (has_key IN (0, 1)),
    last_health_at TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        (health_status = 'unknown' AND health_error_code IS NULL AND last_health_at IS NULL)
        OR (health_status = 'healthy' AND health_error_code IS NULL AND last_health_at IS NOT NULL)
        OR (health_status = 'unhealthy' AND health_error_code IS NOT NULL AND last_health_at IS NOT NULL)
    ),
    CHECK (status <> 'ready' OR health_status = 'healthy')
);

CREATE INDEX idx_ai_providers_status ON ai_providers(status, name);

CREATE TRIGGER trg_ai_providers_version_step
BEFORE UPDATE ON ai_providers
WHEN NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'AI_PROVIDER_VERSION_INVALID');
END;