-- Conversation-owned intent, not business facts or content-free telemetry.
-- Each edit appends an immutable revision; session deletion removes all revisions.
CREATE TABLE ai_work_plan_revisions (
    session_id TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version BETWEEN 1 AND 128),
    generation_id TEXT NOT NULL REFERENCES ai_generations(id) ON DELETE CASCADE,
    plan_json TEXT NOT NULL CHECK (json_valid(plan_json) AND length(CAST(plan_json AS BLOB)) <= 12288),
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, version)
);
CREATE TRIGGER trg_ai_work_plan_revision_insert
BEFORE INSERT ON ai_work_plan_revisions
WHEN NOT EXISTS (
    SELECT 1 FROM ai_generations g JOIN ai_sessions s ON s.id=g.session_id
    WHERE g.id=NEW.generation_id AND s.id=NEW.session_id AND s.persist=1 AND g.status='streaming'
) OR NEW.version != COALESCE((SELECT MAX(version)+1 FROM ai_work_plan_revisions WHERE session_id=NEW.session_id),1)
BEGIN
    SELECT RAISE(ABORT, 'AI_PLAN_REVISION_INVALID');
END;
CREATE TRIGGER trg_ai_work_plan_revision_immutable
BEFORE UPDATE ON ai_work_plan_revisions
BEGIN
    SELECT RAISE(ABORT, 'AI_PLAN_REVISION_IMMUTABLE');
END;
