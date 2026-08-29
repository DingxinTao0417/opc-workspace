CREATE TABLE agent_adapters (
    id TEXT PRIMARY KEY,
    adapter_key TEXT NOT NULL UNIQUE
        CHECK (length(trim(adapter_key)) BETWEEN 1 AND 100 AND adapter_key = trim(adapter_key)),
    kind TEXT NOT NULL CHECK (kind = 'builtin'),
    display_name TEXT NOT NULL
        CHECK (length(trim(display_name)) BETWEEN 2 AND 100 AND display_name = trim(display_name)),
    executable_ref TEXT NOT NULL UNIQUE
        CHECK (
            length(executable_ref) BETWEEN 9 AND 150
            AND executable_ref = trim(executable_ref)
            AND executable_ref LIKE 'builtin:%'
        ),
    manifest_json TEXT NOT NULL
        CHECK (
            json_valid(manifest_json)
            AND json_type(manifest_json) = 'object'
            AND length(manifest_json) BETWEEN 2 AND 16384
        ),
    protocol_version TEXT NOT NULL CHECK (protocol_version = 'opc-agent-pipe-v1'),
    status TEXT NOT NULL DEFAULT 'disabled' CHECK (status IN ('enabled', 'disabled')),
    health_status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (health_status IN ('unknown', 'blocked', 'healthy', 'unhealthy')),
    health_error_code TEXT
        CHECK (health_error_code IS NULL OR length(health_error_code) BETWEEN 1 AND 100),
    isolation_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (isolation_status IN ('unverified', 'verified', 'unsupported')),
    execution_ready INTEGER NOT NULL DEFAULT 0 CHECK (execution_ready IN (0, 1)),
    last_health_at TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        (health_status = 'unknown' AND health_error_code IS NULL AND last_health_at IS NULL)
        OR (health_status = 'healthy' AND health_error_code IS NULL AND last_health_at IS NOT NULL)
        OR (health_status IN ('blocked', 'unhealthy') AND health_error_code IS NOT NULL AND last_health_at IS NOT NULL)
    ),
    CHECK (status = 'disabled' OR execution_ready = 1),
    CHECK (
        execution_ready = 0
        OR (health_status = 'healthy' AND isolation_status = 'verified')
    )
);

CREATE INDEX idx_agent_adapters_status
ON agent_adapters(status, execution_ready, display_name, id);

CREATE TRIGGER trg_agent_adapters_identity_immutable
BEFORE UPDATE ON agent_adapters
WHEN NEW.id IS NOT OLD.id
  OR NEW.adapter_key IS NOT OLD.adapter_key
  OR NEW.kind IS NOT OLD.kind
  OR NEW.display_name IS NOT OLD.display_name
  OR NEW.executable_ref IS NOT OLD.executable_ref
  OR NEW.manifest_json IS NOT OLD.manifest_json
  OR NEW.protocol_version IS NOT OLD.protocol_version
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'AGENT_ADAPTER_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_agent_adapters_user_version_step
BEFORE UPDATE ON agent_adapters
WHEN NEW.status IS NOT OLD.status
  AND NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'AGENT_ADAPTER_VERSION_INVALID');
END;

CREATE TRIGGER trg_agent_adapters_runtime_version_stable
BEFORE UPDATE ON agent_adapters
WHEN NEW.status IS OLD.status
  AND NEW.version <> OLD.version
BEGIN
    SELECT RAISE(ABORT, 'AGENT_ADAPTER_RUNTIME_VERSION_INVALID');
END;

CREATE TRIGGER trg_agent_adapters_hard_delete_forbidden
BEFORE DELETE ON agent_adapters
BEGIN
    SELECT RAISE(ABORT, 'AGENT_ADAPTER_HARD_DELETE_FORBIDDEN');
END;
