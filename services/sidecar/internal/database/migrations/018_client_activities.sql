CREATE TABLE client_activities (
    id TEXT PRIMARY KEY
        CHECK (length(id) = 36),
    client_id TEXT NOT NULL
        REFERENCES clients(id) ON DELETE CASCADE,
    kind TEXT NOT NULL
        CHECK (kind IN ('note', 'meeting', 'system_reference')),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 200),
    body TEXT
        CHECK (body IS NULL OR length(trim(body)) BETWEEN 1 AND 10000),
    occurred_at TEXT NOT NULL
        CHECK (length(occurred_at) > 0),
    created_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    source_type TEXT,
    source_id TEXT,
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    deleted_at TEXT,
    deleted_by_actor_id TEXT
        REFERENCES actors(id) ON DELETE RESTRICT,
    delete_reason TEXT
        CHECK (delete_reason IS NULL OR length(trim(delete_reason)) BETWEEN 1 AND 1000),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(updated_at) > 0),
    CHECK (
        (kind IN ('note', 'meeting') AND body IS NOT NULL AND source_type IS NULL AND source_id IS NULL)
        OR
        (kind = 'system_reference' AND body IS NULL AND source_type IS NOT NULL AND length(trim(source_type)) BETWEEN 1 AND 100 AND source_id IS NOT NULL AND length(trim(source_id)) BETWEEN 1 AND 200)
    ),
    CHECK (
        (deleted_at IS NULL AND deleted_by_actor_id IS NULL AND delete_reason IS NULL)
        OR
        (deleted_at IS NOT NULL AND deleted_by_actor_id IS NOT NULL AND delete_reason IS NOT NULL)
    )
);

CREATE INDEX idx_client_activities_timeline
ON client_activities(client_id, deleted_at, occurred_at DESC, id ASC);

CREATE INDEX idx_client_activities_kind
ON client_activities(client_id, kind, occurred_at DESC, id ASC);

CREATE TRIGGER client_activities_immutable_identity
BEFORE UPDATE OF client_id, created_by_actor_id, source_type, source_id, created_at ON client_activities
FOR EACH ROW
WHEN NEW.client_id IS NOT OLD.client_id
  OR NEW.created_by_actor_id IS NOT OLD.created_by_actor_id
  OR NEW.source_type IS NOT OLD.source_type
  OR NEW.source_id IS NOT OLD.source_id
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'client activity identity is immutable');
END;

CREATE TRIGGER client_activities_terminal_delete
BEFORE UPDATE ON client_activities
FOR EACH ROW
WHEN OLD.deleted_at IS NOT NULL AND (
    NEW.kind IS NOT OLD.kind
    OR NEW.title IS NOT OLD.title
    OR NEW.body IS NOT OLD.body
    OR NEW.occurred_at IS NOT OLD.occurred_at
    OR NEW.version IS NOT OLD.version
    OR NEW.deleted_at IS NOT OLD.deleted_at
    OR NEW.deleted_by_actor_id IS NOT OLD.deleted_by_actor_id
    OR NEW.delete_reason IS NOT OLD.delete_reason
    OR NEW.updated_at IS NOT OLD.updated_at
)
BEGIN
    SELECT RAISE(ABORT, 'deleted client activity is immutable');
END;

CREATE TRIGGER client_activities_bump_client_after_insert
AFTER INSERT ON client_activities
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER client_activities_bump_client_after_update
AFTER UPDATE ON client_activities
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;
