-- Preserve all approval identities and decisions while allowing full 10,000-character
-- activity bodies (including JSON escaping) in a before/after preview. No business
-- tables change; the migration runner wraps copy/drop/rename in one transaction.
CREATE TABLE ai_action_proposals_v73 (
    id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES ai_generations(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL CHECK (length(fingerprint) = 64),
    action_json TEXT NOT NULL CHECK (json_valid(action_json) AND length(action_json) <= 65536),
    preview_json TEXT NOT NULL CHECK (json_valid(preview_json) AND length(preview_json) <= 131072),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','rejected')),
    result_id TEXT,
    result_version INTEGER,
    created_at TEXT NOT NULL,
    decided_at TEXT,
    UNIQUE(generation_id, fingerprint),
    CHECK ((status = 'pending' AND decided_at IS NULL AND result_id IS NULL AND result_version IS NULL)
        OR (status = 'rejected' AND decided_at IS NOT NULL AND result_id IS NULL AND result_version IS NULL)
        OR (status = 'confirmed' AND decided_at IS NOT NULL AND result_id IS NOT NULL AND result_version >= 1))
);
INSERT INTO ai_action_proposals_v73 SELECT * FROM ai_action_proposals;
DROP TABLE ai_action_proposals;
ALTER TABLE ai_action_proposals_v73 RENAME TO ai_action_proposals;
CREATE INDEX idx_ai_action_proposals_generation ON ai_action_proposals(generation_id, created_at, id);
CREATE TRIGGER trg_ai_action_proposals_immutable
BEFORE UPDATE ON ai_action_proposals
WHEN NOT (
    OLD.status = 'pending' AND NEW.status IN ('confirmed','rejected')
    AND NEW.id = OLD.id AND NEW.generation_id = OLD.generation_id
    AND NEW.fingerprint = OLD.fingerprint AND NEW.action_json = OLD.action_json
    AND NEW.preview_json = OLD.preview_json AND NEW.created_at = OLD.created_at
)
BEGIN
    SELECT RAISE(ABORT, 'AI_ACTION_PROPOSAL_IMMUTABLE');
END;
