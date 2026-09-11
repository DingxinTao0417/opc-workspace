-- Stable request identity is metadata only, including for non-persistent chats.
ALTER TABLE ai_generations ADD COLUMN request_key TEXT
    CHECK (request_key IS NULL OR length(request_key) BETWEEN 1 AND 128);
ALTER TABLE ai_generations ADD COLUMN request_hash TEXT
    CHECK ((request_key IS NULL AND request_hash IS NULL) OR
           (request_key IS NOT NULL AND request_hash IS NOT NULL AND length(request_hash) = 64));
CREATE UNIQUE INDEX idx_ai_generations_request_key ON ai_generations(request_key)
    WHERE request_key IS NOT NULL;
CREATE TRIGGER trg_ai_generation_request_identity_immutable
BEFORE UPDATE ON ai_generations
WHEN NEW.request_key IS NOT OLD.request_key OR NEW.request_hash IS NOT OLD.request_hash
BEGIN SELECT RAISE(ABORT, 'AI_GENERATION_REQUEST_IMMUTABLE'); END;

-- The message itself owns one confirmed command; task creation and this static
-- binding commit together. Deleting a task must not make its suggestion reusable.
ALTER TABLE ai_messages ADD COLUMN task_confirmation_hash TEXT
    CHECK (task_confirmation_hash IS NULL OR (length(task_confirmation_hash) BETWEEN 64 AND 96 AND task_id IS NOT NULL));
CREATE TRIGGER trg_ai_message_task_binding_immutable
BEFORE UPDATE ON ai_messages
WHEN OLD.task_id IS NOT NULL AND
    (NEW.task_id IS NOT OLD.task_id OR NEW.task_title_snapshot IS NOT OLD.task_title_snapshot
     OR NEW.task_confirmation_hash IS NOT OLD.task_confirmation_hash)
BEGIN SELECT RAISE(ABORT, 'AI_MESSAGE_TASK_ALREADY_LINKED'); END;

-- A positive offset is the number of UTF-8 bytes already summarized in the
-- sanitized source message; zero means the entire source message is summarized.
ALTER TABLE ai_memory_entries ADD COLUMN source_message_offset INTEGER NOT NULL DEFAULT 0
    CHECK (typeof(source_message_offset) = 'integer' AND source_message_offset >= 0 AND (source_message_offset = 0 OR kind = 'context_snapshot'));
ALTER TABLE ai_memory_entries ADD COLUMN decision TEXT
    CHECK (decision IS NULL OR (kind = 'memory_proposal' AND status = 'superseded' AND decision IN ('confirmed', 'rejected')));
ALTER TABLE ai_memory_entries ADD COLUMN memory_id TEXT
    CHECK (memory_id IS NULL OR (decision IS NOT NULL AND decision = 'confirmed' AND length(memory_id) = 36));

DROP TRIGGER trg_ai_memory_entries_append_only;
-- Recover known historic resolutions without changing any workflow/audit row.
UPDATE ai_memory_entries SET
    decision = CASE WHEN EXISTS (
        SELECT 1 FROM workflow_events e WHERE e.aggregate_type = 'ai_session'
          AND e.aggregate_id = ai_memory_entries.session_id
          AND e.action = 'ai_memory_proposal_confirmed' AND json_valid(e.current_json)
          AND json_extract(e.current_json, '$.proposal_id') = ai_memory_entries.id
    ) THEN 'confirmed' WHEN EXISTS (
        SELECT 1 FROM workflow_events e WHERE e.aggregate_type = 'ai_session'
          AND e.aggregate_id = ai_memory_entries.session_id
          AND e.action = 'ai_memory_proposal_rejected' AND json_valid(e.current_json)
          AND json_extract(e.current_json, '$.proposal_id') = ai_memory_entries.id
    ) THEN 'rejected' ELSE NULL END,
    memory_id = (
        SELECT json_extract(e.current_json, '$.memory_id') FROM workflow_events e
        WHERE e.aggregate_type = 'ai_session' AND e.aggregate_id = ai_memory_entries.session_id
          AND e.action = 'ai_memory_proposal_confirmed' AND json_valid(e.current_json)
          AND json_extract(e.current_json, '$.proposal_id') = ai_memory_entries.id
        ORDER BY e.created_at DESC, e.id DESC LIMIT 1
    )
WHERE kind = 'memory_proposal' AND status = 'superseded';

CREATE TRIGGER trg_ai_memory_entries_append_only
BEFORE UPDATE ON ai_memory_entries
WHEN NOT (
    OLD.status = 'active' AND NEW.status = 'superseded'
    AND NEW.id = OLD.id AND NEW.session_id = OLD.session_id
    AND NEW.kind = OLD.kind AND NEW.content = OLD.content AND NEW.tags = OLD.tags
    AND NEW.origin = OLD.origin AND NEW.source_message_id IS OLD.source_message_id
    AND NEW.source_message_offset = OLD.source_message_offset
    AND NEW.created_at = OLD.created_at AND NEW.updated_at <> OLD.updated_at
    AND ((OLD.kind = 'memory_proposal' AND OLD.decision IS NULL AND OLD.memory_id IS NULL)
         OR (NEW.decision IS OLD.decision AND NEW.memory_id IS OLD.memory_id))
)
BEGIN SELECT RAISE(ABORT, 'AI_MEMORY_ENTRY_IMMUTABLE'); END;
