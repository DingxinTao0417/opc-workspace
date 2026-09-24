-- migration: destructive
-- Preserve request history while widening complete next-message recommendations.
DROP TRIGGER trg_ai_workspace_access_request_insert;
DROP TRIGGER trg_ai_workspace_access_request_update;
ALTER TABLE ai_workspace_access_requests RENAME TO ai_workspace_access_requests_old;
-- Saved-conversation permission requests are UI recommendations, never grants.
-- They are committed only alongside a completed assistant generation.
CREATE TABLE ai_workspace_access_requests (
    generation_id TEXT PRIMARY KEY REFERENCES ai_generations(id) ON DELETE CASCADE,
    scopes_json TEXT NOT NULL CHECK (
        json_valid(scopes_json) AND json_type(scopes_json)='array'
        AND json_array_length(scopes_json) BETWEEN 1 AND 16
        AND length(CAST(scopes_json AS BLOB)) <= 512
    ),
    status TEXT NOT NULL CHECK (status IN ('open', 'dismissed', 'consumed')),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

INSERT INTO ai_workspace_access_requests SELECT * FROM ai_workspace_access_requests_old;
DROP TABLE ai_workspace_access_requests_old;

CREATE TRIGGER trg_ai_workspace_access_request_insert
BEFORE INSERT ON ai_workspace_access_requests
WHEN NEW.status != 'open'
  OR NOT EXISTS (
    SELECT 1 FROM ai_generations g
    JOIN ai_sessions s ON s.id=g.session_id AND s.persist=1
    WHERE g.id=NEW.generation_id AND g.status='completed'
  )
  OR EXISTS (
    SELECT 1 FROM json_each(NEW.scopes_json)
    WHERE type != 'text' OR value NOT IN (
      'work','clients','outputs','output_files','actions','agent_execution',
      'agent_files','agent_project_files','workspace_ui','workspace_browser',
      'knowledge','knowledge_actions','finance','finance_actions',
      'invoice_actions','finance_exports'
    )
  )
  OR (SELECT COUNT(*) FROM json_each(NEW.scopes_json)) !=
     (SELECT COUNT(DISTINCT value) FROM json_each(NEW.scopes_json))
BEGIN
    SELECT RAISE(ABORT, 'AI_ACCESS_REQUEST_INVALID');
END;

CREATE TRIGGER trg_ai_workspace_access_request_update
BEFORE UPDATE ON ai_workspace_access_requests
WHEN NEW.generation_id IS NOT OLD.generation_id
  OR NEW.scopes_json IS NOT OLD.scopes_json
  OR NEW.created_at IS NOT OLD.created_at
  OR OLD.status != 'open'
  OR NEW.status NOT IN ('dismissed', 'consumed')
BEGIN
    SELECT RAISE(ABORT, 'AI_ACCESS_REQUEST_STATE_INVALID');
END;
