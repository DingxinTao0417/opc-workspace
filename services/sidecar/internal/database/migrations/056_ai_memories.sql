-- 056: ai_memories — user-confirmed long-term preferences for the AI
-- assistant (ADR-006). Rows are created only through explicit user
-- confirmation of a suggestion card; they are operational/privacy data and
-- stay outside the business export surface.
CREATE TABLE ai_memories (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL
        CHECK (length(trim(content)) BETWEEN 1 AND 500 AND content = trim(content)),
    source_message_id TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

CREATE INDEX idx_ai_memories_updated ON ai_memories(updated_at DESC);
