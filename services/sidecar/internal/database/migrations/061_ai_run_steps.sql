-- 061: local, content-free AI run telemetry. Existing generations receive one
-- summary step; new generations add bounded model/tool/self-check/citation
-- steps. No prompt, response, reasoning, tool arguments/results, or key is
-- stored in this table.
ALTER TABLE ai_messages ADD COLUMN generation_id TEXT
    REFERENCES ai_generations(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX idx_ai_messages_generation
    ON ai_messages(generation_id)
    WHERE generation_id IS NOT NULL;

CREATE TABLE ai_run_steps (
    id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES ai_generations(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    kind TEXT NOT NULL
        CHECK (kind IN ('generation', 'model_turn', 'tool_call', 'self_check', 'citation_validation', 'persistence')),
    status TEXT NOT NULL
        CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    turn_index INTEGER CHECK (turn_index IS NULL OR turn_index >= 1),
    tool_name TEXT
        CHECK (tool_name IS NULL OR (length(trim(tool_name)) BETWEEN 1 AND 100 AND tool_name = trim(tool_name))),
    started_at TEXT NOT NULL CHECK (length(started_at) > 0),
    completed_at TEXT,
    duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms >= 0),
    input_bytes INTEGER NOT NULL DEFAULT 0 CHECK (input_bytes >= 0),
    output_bytes INTEGER NOT NULL DEFAULT 0 CHECK (output_bytes >= 0),
    error_code TEXT
        CHECK (error_code IS NULL OR (length(trim(error_code)) BETWEEN 1 AND 100 AND error_code = trim(error_code))),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    UNIQUE(generation_id, sequence),
    CHECK (
        (status = 'running' AND completed_at IS NULL AND duration_ms IS NULL AND error_code IS NULL)
        OR (status IN ('succeeded', 'cancelled') AND completed_at IS NOT NULL AND duration_ms IS NOT NULL AND error_code IS NULL)
        OR (status = 'failed' AND completed_at IS NOT NULL AND duration_ms IS NOT NULL AND error_code IS NOT NULL)
    ),
    CHECK ((kind = 'tool_call') = (tool_name IS NOT NULL)),
    CHECK (
        (kind IN ('model_turn', 'self_check') AND turn_index IS NOT NULL)
        OR (kind NOT IN ('model_turn', 'self_check') AND turn_index IS NULL)
    )
);

CREATE INDEX idx_ai_run_steps_generation_sequence
    ON ai_run_steps(generation_id, sequence);

CREATE TRIGGER trg_ai_run_steps_update_guard
BEFORE UPDATE ON ai_run_steps
WHEN NOT (
    OLD.kind = 'generation'
    AND OLD.status = 'running'
    AND NEW.status IN ('succeeded', 'failed', 'cancelled')
    AND NEW.id = OLD.id
    AND NEW.generation_id = OLD.generation_id
    AND NEW.sequence = OLD.sequence
    AND NEW.kind = OLD.kind
    AND NEW.turn_index IS OLD.turn_index
    AND NEW.tool_name IS OLD.tool_name
    AND NEW.started_at = OLD.started_at
    AND NEW.completed_at IS NOT NULL
    AND NEW.duration_ms IS NOT NULL
    AND NEW.input_bytes >= 0
    AND NEW.output_bytes >= 0
    AND NEW.created_at = OLD.created_at
)
BEGIN
    SELECT RAISE(ABORT, 'AI_RUN_STEP_IMMUTABLE');
END;

INSERT INTO ai_run_steps(
    id, generation_id, sequence, kind, status, started_at, completed_at,
    duration_ms, input_bytes, output_bytes, error_code, created_at
)
SELECT
    lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
    substr(lower(hex(randomblob(2))), 2) || '-' ||
    substr('89ab', abs(random()) % 4 + 1, 1) ||
    substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))),
    id,
    1,
    'generation',
    CASE
        WHEN status = 'completed' THEN 'succeeded'
        WHEN status = 'failed' THEN 'failed'
        WHEN status = 'cancelled' THEN 'cancelled'
        ELSE 'running'
    END,
    created_at,
    CASE WHEN status IN ('completed', 'failed', 'cancelled') THEN updated_at ELSE NULL END,
    CASE WHEN status IN ('completed', 'failed', 'cancelled') THEN 0 ELSE NULL END,
    0,
    CASE WHEN content IS NULL THEN 0 ELSE length(CAST(content AS BLOB)) END,
    CASE WHEN status = 'failed' THEN COALESCE(error_code, 'AI_GENERATION_FAILED') ELSE NULL END,
    created_at
FROM ai_generations;
