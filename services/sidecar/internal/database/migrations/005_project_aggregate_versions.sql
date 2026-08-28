CREATE TRIGGER projects_version_after_task_insert
AFTER INSERT ON tasks
WHEN NEW.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id;
END;

CREATE TRIGGER projects_version_after_task_update
AFTER UPDATE OF project_id, status, actual_minutes ON tasks
WHEN OLD.project_id IS NOT NEW.project_id
  OR OLD.status <> NEW.status
  OR OLD.actual_minutes <> NEW.actual_minutes
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;

    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id
      AND (OLD.project_id IS NULL OR NEW.project_id <> OLD.project_id);
END;

CREATE TRIGGER projects_version_after_task_delete
AFTER DELETE ON tasks
WHEN OLD.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;
END;

CREATE TRIGGER projects_version_after_invoice_insert
AFTER INSERT ON invoices
WHEN NEW.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id;
END;

CREATE TRIGGER projects_version_after_invoice_update
AFTER UPDATE OF project_id ON invoices
WHEN OLD.project_id IS NOT NEW.project_id
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;

    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = NEW.project_id
      AND (OLD.project_id IS NULL OR NEW.project_id <> OLD.project_id);
END;

CREATE TRIGGER projects_version_after_invoice_delete
AFTER DELETE ON invoices
WHEN OLD.project_id IS NOT NULL
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE id = OLD.project_id;
END;

CREATE TRIGGER projects_version_after_client_name_update
AFTER UPDATE OF name ON clients
WHEN NEW.name <> OLD.name
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE client_id = NEW.id;
END;

CREATE TRIGGER projects_version_before_client_delete
BEFORE DELETE ON clients
BEGIN
    UPDATE projects
    SET version = version + 1,
        updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    WHERE client_id = OLD.id;
END;
