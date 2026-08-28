ALTER TABLE clients
ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1);

UPDATE clients
SET contact_name = CASE WHEN trim(contact_name) = '' THEN NULL ELSE contact_name END,
    email = CASE WHEN trim(email) = '' THEN NULL ELSE email END,
    phone = CASE WHEN trim(phone) = '' THEN NULL ELSE phone END,
    notes = CASE WHEN trim(notes) = '' THEN NULL ELSE notes END
WHERE (contact_name IS NOT NULL AND trim(contact_name) = '')
   OR (email IS NOT NULL AND trim(email) = '')
   OR (phone IS NOT NULL AND trim(phone) = '')
   OR (notes IS NOT NULL AND trim(notes) = '');

CREATE INDEX idx_clients_name ON clients(name);
CREATE INDEX idx_clients_status ON clients(status);
CREATE INDEX idx_clients_updated_at ON clients(updated_at);

CREATE TRIGGER clients_version_after_project_insert
AFTER INSERT ON projects
WHEN NEW.client_id IS NOT NULL
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.client_id;
END;

CREATE TRIGGER clients_version_after_project_update
AFTER UPDATE OF client_id ON projects
WHEN OLD.client_id IS NOT NEW.client_id
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.client_id;

    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.client_id
      AND (OLD.client_id IS NULL OR NEW.client_id <> OLD.client_id);
END;

CREATE TRIGGER clients_version_after_project_delete
AFTER DELETE ON projects
WHEN OLD.client_id IS NOT NULL
BEGIN
    UPDATE clients
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.client_id;
END;
