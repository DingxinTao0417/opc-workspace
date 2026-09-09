-- 057: append-only AI session context snapshots (ADR-007 G2). Snapshot
-- content is operational/private AI state: it stays in SQLite backups but is
-- excluded from portable business exports.
CREATE TABLE ai_memory_entries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL
        CHECK (kind IN ('context_snapshot', 'session_fact', 'memory_proposal')),
    content TEXT NOT NULL
        CHECK (length(trim(content)) BETWEEN 1 AND 65536 AND content = trim(content))
        CHECK (kind <> 'context_snapshot' OR (json_valid(content) AND json_type(content) = 'object')),
    tags TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(tags) AND json_type(tags) = 'array' AND length(tags) <= 8192),
    origin TEXT NOT NULL
        CHECK (origin IN ('model_compaction', 'agent', 'model_proposal')),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'superseded')),
    source_message_id TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        kind <> 'context_snapshot'
        OR (origin = 'model_compaction' AND source_message_id IS NOT NULL)
    ),
    FOREIGN KEY (session_id, source_message_id)
        REFERENCES ai_messages(session_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_ai_messages_session_identity
    ON ai_messages(session_id, id);

CREATE INDEX idx_ai_memory_entries_session_timeline
    ON ai_memory_entries(session_id, kind, created_at DESC, id DESC);

CREATE UNIQUE INDEX idx_ai_memory_entries_active_snapshot
    ON ai_memory_entries(session_id)
    WHERE kind = 'context_snapshot' AND status = 'active';

-- Snapshot payloads are immutable. The sole permitted update retires an
-- active row after its replacement has been produced; the old payload and
-- watermark remain available for audit/replay.
CREATE TRIGGER trg_ai_memory_entries_append_only
BEFORE UPDATE ON ai_memory_entries
WHEN NOT (
    OLD.status = 'active'
    AND NEW.status = 'superseded'
    AND NEW.id = OLD.id
    AND NEW.session_id = OLD.session_id
    AND NEW.kind = OLD.kind
    AND NEW.content = OLD.content
    AND NEW.tags = OLD.tags
    AND NEW.origin = OLD.origin
    AND NEW.source_message_id IS OLD.source_message_id
    AND NEW.created_at = OLD.created_at
    AND NEW.updated_at <> OLD.updated_at
)
BEGIN
    SELECT RAISE(ABORT, 'AI_MEMORY_ENTRY_IMMUTABLE');
END;
