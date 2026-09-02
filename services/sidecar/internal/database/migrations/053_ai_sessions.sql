CREATE TABLE ai_sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 200 AND title = trim(title)),
    persist INTEGER NOT NULL DEFAULT 1 CHECK (persist IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

CREATE TRIGGER trg_ai_sessions_version_step
BEFORE UPDATE ON ai_sessions
WHEN NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'AI_SESSION_VERSION_INVALID');
END;

CREATE TABLE ai_generations (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id),
    provider_id TEXT NOT NULL REFERENCES ai_providers(id),
    status TEXT NOT NULL CHECK (status IN ('queued', 'streaming', 'completed', 'failed', 'cancelled')),
    error_code TEXT
        CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 100),
    content TEXT
        CHECK (content IS NULL OR length(content) <= 1048576),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

CREATE INDEX idx_ai_generations_session ON ai_generations(session_id, status, created_at);
CREATE INDEX idx_ai_generations_provider ON ai_generations(provider_id, status);

CREATE TABLE ai_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id),
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('completed', 'cancelled', 'failed')),
    content TEXT NOT NULL
        CHECK ((length(content) >= 1) OR status = 'cancelled'),
    model_snapshot TEXT
        CHECK (model_snapshot IS NULL OR json_valid(model_snapshot)),
    task_id TEXT,
    task_title_snapshot TEXT
        CHECK (task_title_snapshot IS NULL OR length(task_title_snapshot) BETWEEN 1 AND 500),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        (task_id IS NULL AND task_title_snapshot IS NULL)
        OR (task_id IS NOT NULL AND task_title_snapshot IS NOT NULL)
    )
);

CREATE INDEX idx_ai_messages_session ON ai_messages(session_id, created_at, id);