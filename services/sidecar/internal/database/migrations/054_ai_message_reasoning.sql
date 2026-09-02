ALTER TABLE ai_messages ADD COLUMN reasoning TEXT;

CREATE INDEX idx_ai_messages_reasoning ON ai_messages(session_id, created_at)
WHERE reasoning IS NOT NULL;