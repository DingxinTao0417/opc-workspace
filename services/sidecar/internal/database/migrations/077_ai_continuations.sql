-- Explicit continuation leases are operational state, not portable business data.
-- A generated continuation trigger and its answer share one generation. Keep
-- each role independently unique without rewriting any existing message row.
DROP INDEX idx_ai_messages_generation;
CREATE UNIQUE INDEX idx_ai_messages_generation
ON ai_messages(generation_id) WHERE generation_id IS NOT NULL AND role='assistant';
CREATE UNIQUE INDEX idx_ai_messages_user_generation
ON ai_messages(generation_id) WHERE generation_id IS NOT NULL AND role='user';

CREATE TABLE ai_continuations (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    provider_version INTEGER NOT NULL CHECK (provider_version >= 1),
    provider_config_version INTEGER NOT NULL CHECK (provider_config_version >= 1),
    provider_name TEXT NOT NULL CHECK (length(trim(provider_name)) BETWEEN 1 AND 200),
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('local', 'remote')),
    provider_protocol TEXT NOT NULL CHECK (provider_protocol IN ('openai_chat', 'anthropic_messages')),
    provider_model TEXT NOT NULL CHECK (length(trim(provider_model)) BETWEEN 1 AND 200),
    workspace_json TEXT NOT NULL CHECK (json_valid(workspace_json) AND json_type(workspace_json) = 'object' AND length(CAST(workspace_json AS BLOB)) <= 65536),
    initial_plan_version INTEGER NOT NULL CHECK (initial_plan_version BETWEEN 1 AND 128),
    current_plan_version INTEGER NOT NULL CHECK (current_plan_version BETWEEN initial_plan_version AND 128),
    last_observation_hash TEXT NOT NULL DEFAULT '' CHECK (last_observation_hash = '' OR (length(last_observation_hash) = 64 AND last_observation_hash NOT GLOB '*[^0-9a-f]*')),
    max_turns INTEGER NOT NULL CHECK (max_turns BETWEEN 1 AND 8),
    turns_started INTEGER NOT NULL DEFAULT 0 CHECK (turns_started BETWEEN 0 AND max_turns),
    status TEXT NOT NULL CHECK (status IN ('waiting', 'running', 'completed', 'stopped', 'failed', 'exhausted', 'expired', 'interrupted')),
    reason TEXT NOT NULL CHECK (reason IN ('ready', 'pending_approval', 'run_active', 'output_pending', 'provider_busy', 'generating', 'plan_complete', 'user_stopped', 'no_progress', 'provider_changed', 'scope_changed', 'generation_failed', 'generation_cancelled', 'evidence_unavailable', 'plan_changed', 'turn_limit', 'time_limit', 'process_restart', 'shutdown', 'restore_pending')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    current_generation_id TEXT,
    last_generation_id TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    expires_at TEXT NOT NULL CHECK (julianday(expires_at) IS NOT NULL AND julianday(created_at) IS NOT NULL AND julianday(expires_at) > julianday(created_at) AND julianday(expires_at) <= julianday(created_at) + 1.0 / 12.0)
);

CREATE UNIQUE INDEX idx_ai_continuations_active_session
ON ai_continuations(session_id) WHERE status IN ('waiting', 'running');
CREATE INDEX idx_ai_continuations_status ON ai_continuations(status, expires_at, created_at, id);
CREATE INDEX idx_ai_continuations_provider ON ai_continuations(provider_id, status);

CREATE TRIGGER trg_ai_continuations_insert_identity
BEFORE INSERT ON ai_continuations
WHEN NOT EXISTS (SELECT 1 FROM ai_sessions WHERE id=NEW.session_id AND persist=1)
  OR NOT EXISTS (SELECT 1 FROM ai_providers WHERE id=NEW.provider_id)
  OR NOT EXISTS (SELECT 1 FROM ai_work_plan_revisions WHERE session_id=NEW.session_id AND version=NEW.initial_plan_version)
  OR NOT EXISTS (SELECT 1 FROM ai_work_plan_revisions WHERE session_id=NEW.session_id AND version=NEW.current_plan_version)
  OR (NEW.current_generation_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_generations WHERE id=NEW.current_generation_id AND session_id=NEW.session_id AND provider_id=NEW.provider_id))
  OR (NEW.last_generation_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_generations WHERE id=NEW.last_generation_id AND session_id=NEW.session_id AND provider_id=NEW.provider_id))
BEGIN
    SELECT RAISE(ABORT, 'AI_CONTINUATION_IDENTITY_INVALID');
END;

CREATE TRIGGER trg_ai_continuations_immutable_authorization
BEFORE UPDATE ON ai_continuations
WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id
  OR NEW.provider_id IS NOT OLD.provider_id OR NEW.provider_version IS NOT OLD.provider_version
  OR NEW.provider_config_version IS NOT OLD.provider_config_version OR NEW.provider_name IS NOT OLD.provider_name
  OR NEW.provider_kind IS NOT OLD.provider_kind OR NEW.provider_protocol IS NOT OLD.provider_protocol
  OR NEW.provider_model IS NOT OLD.provider_model OR NEW.workspace_json IS NOT OLD.workspace_json
  OR NEW.initial_plan_version IS NOT OLD.initial_plan_version OR NEW.max_turns IS NOT OLD.max_turns
  OR NEW.created_at IS NOT OLD.created_at OR NEW.expires_at IS NOT OLD.expires_at
BEGIN
    SELECT RAISE(ABORT, 'AI_CONTINUATION_AUTHORIZATION_IMMUTABLE');
END;

CREATE TRIGGER trg_ai_continuations_update_state
BEFORE UPDATE ON ai_continuations
WHEN NEW.version != OLD.version + 1
  OR NEW.turns_started < OLD.turns_started OR NEW.turns_started > OLD.turns_started + 1
  OR NEW.current_plan_version < OLD.current_plan_version
  OR (NEW.current_plan_version != OLD.current_plan_version AND NOT EXISTS (SELECT 1 FROM ai_work_plan_revisions WHERE session_id=NEW.session_id AND version=NEW.current_plan_version))
  OR (OLD.status NOT IN ('waiting', 'running') AND (NEW.status IN ('waiting', 'running') OR NEW.turns_started != OLD.turns_started))
  OR (NEW.current_generation_id IS NOT OLD.current_generation_id AND NEW.current_generation_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_generations WHERE id=NEW.current_generation_id AND session_id=NEW.session_id AND provider_id=NEW.provider_id))
  OR (NEW.last_generation_id IS NOT OLD.last_generation_id AND NEW.last_generation_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM ai_generations WHERE id=NEW.last_generation_id AND session_id=NEW.session_id AND provider_id=NEW.provider_id))
BEGIN
    SELECT RAISE(ABORT, 'AI_CONTINUATION_STATE_INVALID');
END;

CREATE TABLE ai_continuation_turns (
    continuation_id TEXT NOT NULL REFERENCES ai_continuations(id) ON DELETE CASCADE,
    turn_index INTEGER NOT NULL CHECK (turn_index BETWEEN 1 AND 8),
    generation_id TEXT NOT NULL UNIQUE REFERENCES ai_generations(id) ON DELETE CASCADE,
    plan_version INTEGER NOT NULL CHECK (plan_version BETWEEN 1 AND 128),
    observation_hash TEXT NOT NULL CHECK (length(observation_hash) = 64 AND observation_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    PRIMARY KEY (continuation_id, turn_index)
);

CREATE TRIGGER trg_ai_continuation_turns_insert_identity
BEFORE INSERT ON ai_continuation_turns
WHEN NOT EXISTS (
    SELECT 1 FROM ai_continuations c
    JOIN ai_sessions s ON s.id=c.session_id AND s.persist=1
    JOIN ai_generations g ON g.id=NEW.generation_id AND g.session_id=c.session_id AND g.provider_id=c.provider_id
    JOIN ai_work_plan_revisions p ON p.session_id=c.session_id AND p.version=NEW.plan_version
    WHERE c.id=NEW.continuation_id AND c.status IN ('waiting', 'running') AND NEW.turn_index <= c.max_turns
)
  OR NEW.turn_index != COALESCE((SELECT MAX(turn_index) + 1 FROM ai_continuation_turns WHERE continuation_id=NEW.continuation_id), 1)
BEGIN
    SELECT RAISE(ABORT, 'AI_CONTINUATION_TURN_IDENTITY_INVALID');
END;

CREATE TRIGGER trg_ai_continuation_turns_immutable
BEFORE UPDATE ON ai_continuation_turns
BEGIN
    SELECT RAISE(ABORT, 'AI_CONTINUATION_TURN_IMMUTABLE');
END;
