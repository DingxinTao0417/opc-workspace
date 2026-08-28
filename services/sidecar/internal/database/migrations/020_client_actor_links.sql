CREATE TABLE client_actor_links (
    id TEXT PRIMARY KEY
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND id NOT GLOB '*[^0-9a-f-]*'
        ),
    client_id TEXT NOT NULL
        REFERENCES clients(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'contact'
        CHECK (role = 'contact'),
    linked_by_actor_id TEXT NOT NULL
        REFERENCES actors(id) ON DELETE RESTRICT,
    linked_at TEXT NOT NULL
        CHECK (length(linked_at) > 0),
    unlinked_at TEXT
        CHECK (unlinked_at IS NULL OR length(unlinked_at) > 0),
    unlinked_by_actor_id TEXT
        REFERENCES actors(id) ON DELETE RESTRICT,
    unlink_reason TEXT
        CHECK (unlink_reason IS NULL OR length(trim(unlink_reason)) BETWEEN 1 AND 1000),
    CHECK (
        (unlinked_at IS NULL AND unlinked_by_actor_id IS NULL AND unlink_reason IS NULL)
        OR
        (unlinked_at IS NOT NULL AND unlinked_by_actor_id IS NOT NULL AND unlink_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX ux_client_actor_links_active_role
ON client_actor_links(client_id, role)
WHERE unlinked_at IS NULL;

CREATE INDEX idx_client_actor_links_client_history
ON client_actor_links(client_id, role, linked_at DESC, id ASC);

CREATE INDEX idx_client_actor_links_actor_active
ON client_actor_links(actor_id, client_id)
WHERE unlinked_at IS NULL;

CREATE TRIGGER client_actor_links_require_active_person_insert
BEFORE INSERT ON client_actor_links
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.actor_id
      AND type = 'person'
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_LINK_ACTOR_NOT_ACTIVE_PERSON');
END;

CREATE TRIGGER client_actor_links_require_owner_linker_insert
BEFORE INSERT ON client_actor_links
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1
    FROM actors
    WHERE id = NEW.linked_by_actor_id
      AND type = 'owner'
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_LINK_LINKER_NOT_OWNER');
END;

CREATE TRIGGER client_actor_links_require_owner_unlinker_update
BEFORE UPDATE OF unlinked_at ON client_actor_links
FOR EACH ROW
WHEN NEW.unlinked_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM actors
      WHERE id = NEW.unlinked_by_actor_id
        AND type = 'owner'
        AND status = 'active'
  )
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_LINK_UNLINKER_NOT_OWNER');
END;

CREATE TRIGGER client_actor_links_protect_history_update
BEFORE UPDATE ON client_actor_links
FOR EACH ROW
WHEN NEW.id IS NOT OLD.id
  OR NEW.client_id IS NOT OLD.client_id
  OR NEW.actor_id IS NOT OLD.actor_id
  OR NEW.role IS NOT OLD.role
  OR NEW.linked_by_actor_id IS NOT OLD.linked_by_actor_id
  OR NEW.linked_at IS NOT OLD.linked_at
  OR (
      OLD.unlinked_at IS NOT NULL
      AND (
          NEW.unlinked_at IS NOT OLD.unlinked_at
          OR NEW.unlinked_by_actor_id IS NOT OLD.unlinked_by_actor_id
          OR NEW.unlink_reason IS NOT OLD.unlink_reason
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ACTOR_LINK_HISTORY_IMMUTABLE');
END;

CREATE TRIGGER client_actor_links_protect_member_delete
BEFORE DELETE ON client_actor_links
FOR EACH ROW
WHEN EXISTS (
    SELECT 1
    FROM clients
    WHERE id = OLD.client_id
)
BEGIN
    SELECT RAISE(ABORT, 'CLIENT_ACTOR_LINK_HARD_DELETE_FORBIDDEN');
END;

CREATE TRIGGER actors_require_client_links_end_before_inactive
BEFORE UPDATE OF status ON actors
FOR EACH ROW
WHEN OLD.status = 'active'
  AND NEW.status = 'inactive'
  AND EXISTS (
      SELECT 1
      FROM client_actor_links
      WHERE actor_id = OLD.id
        AND unlinked_at IS NULL
  )
BEGIN
    SELECT RAISE(ABORT, 'ACTOR_HAS_ACTIVE_CLIENT_LINKS');
END;

CREATE TRIGGER client_actor_links_bump_client_after_insert
AFTER INSERT ON client_actor_links
FOR EACH ROW
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER client_actor_links_bump_client_after_unlink
AFTER UPDATE OF unlinked_at ON client_actor_links
FOR EACH ROW
WHEN NEW.unlinked_at IS NOT OLD.unlinked_at
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.client_id;
END;
