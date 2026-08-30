-- migration: destructive
-- migration: foreign_keys=off

CREATE TABLE invoices_v46 (
    id TEXT PRIMARY KEY,
    invoice_number TEXT NOT NULL UNIQUE
        CHECK (
            invoice_number = trim(invoice_number)
            AND length(invoice_number) BETWEEN 1 AND 80
        ),
    client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE RESTRICT,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    amount_minor INTEGER NOT NULL
        CHECK (
            typeof(amount_minor) = 'integer'
            AND amount_minor BETWEEN 1 AND 9000000000000000
        ),
    currency TEXT NOT NULL DEFAULT 'CNY'
        CHECK (
            length(currency) = 3
            AND currency = upper(currency)
            AND currency NOT GLOB '*[^A-Z]*'
        ),
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'sent', 'viewed', 'paid', 'overdue')),
    issue_date TEXT NOT NULL
        CHECK (
            length(issue_date) = 10
            AND strftime('%Y-%m-%d', issue_date, '+0 days') = issue_date
        ),
    due_date TEXT NOT NULL
        CHECK (
            length(due_date) = 10
            AND strftime('%Y-%m-%d', due_date, '+0 days') = due_date
            AND due_date >= issue_date
        ),
    paid_date TEXT
        CHECK (
            paid_date IS NULL
            OR (
                length(paid_date) = 10
                AND strftime('%Y-%m-%d', paid_date, '+0 days') = paid_date
                AND paid_date >= issue_date
            )
        ),
    notes TEXT NOT NULL DEFAULT ''
        CHECK (length(notes) <= 10000),
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL
        CHECK (length(updated_at) > 0),
    CHECK (
        (status = 'paid' AND paid_date IS NOT NULL)
        OR (status <> 'paid' AND paid_date IS NULL)
    )
);

INSERT INTO invoices_v46 (
    id,
    invoice_number,
    client_id,
    project_id,
    amount_minor,
    currency,
    status,
    issue_date,
    due_date,
    paid_date,
    notes,
    version,
    created_at,
    updated_at
)
SELECT
    id,
    invoice_number,
    client_id,
    project_id,
    amount_minor,
    currency,
    status,
    issue_date,
    due_date,
    paid_date,
    notes,
    1,
    created_at,
    updated_at
FROM invoices;

DROP TABLE invoices;
ALTER TABLE invoices_v46 RENAME TO invoices;

CREATE INDEX idx_invoices_client_id ON invoices(client_id);
CREATE INDEX idx_invoices_project_id ON invoices(project_id);
CREATE INDEX idx_invoices_status_due_date ON invoices(status, due_date, id);
CREATE INDEX idx_invoices_issue_date ON invoices(issue_date DESC, id);

CREATE TABLE invoice_number_sequences (
    year INTEGER PRIMARY KEY
        CHECK (
            typeof(year) = 'integer'
            AND year BETWEEN 2000 AND 9999
        ),
    last_value INTEGER NOT NULL
        CHECK (
            typeof(last_value) = 'integer'
            AND last_value >= 0
        ),
    updated_at TEXT NOT NULL
        CHECK (length(updated_at) > 0)
);

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

CREATE TRIGGER trg_invoices_identity_immutable
BEFORE UPDATE ON invoices
WHEN NEW.id IS NOT OLD.id
    OR NEW.invoice_number IS NOT OLD.invoice_number
    OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_invoices_sent_fields_immutable
BEFORE UPDATE ON invoices
WHEN OLD.status <> 'draft'
    AND (
        NEW.client_id IS NOT OLD.client_id
        OR NEW.project_id IS NOT OLD.project_id
        OR NEW.amount_minor IS NOT OLD.amount_minor
        OR NEW.currency IS NOT OLD.currency
        OR NEW.issue_date IS NOT OLD.issue_date
        OR NEW.due_date IS NOT OLD.due_date
        OR NEW.notes IS NOT OLD.notes
    )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_SENT_FIELDS_IMMUTABLE');
END;

CREATE TRIGGER trg_invoices_status_transition
BEFORE UPDATE OF status ON invoices
WHEN NEW.status <> OLD.status
    AND NOT (
        (OLD.status = 'draft' AND NEW.status = 'sent')
        OR (OLD.status = 'sent' AND NEW.status IN ('viewed', 'overdue'))
        OR (OLD.status = 'viewed' AND NEW.status IN ('paid', 'overdue'))
        OR (OLD.status = 'overdue' AND NEW.status = 'paid')
    )
BEGIN
    SELECT RAISE(ABORT, 'INVALID_INVOICE_STATUS_TRANSITION');
END;

CREATE TRIGGER trg_invoices_project_client_insert
BEFORE INSERT ON invoices
WHEN NEW.project_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM projects
        WHERE projects.id = NEW.project_id
          AND projects.client_id = NEW.client_id
    )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PROJECT_CLIENT_MISMATCH');
END;

CREATE TRIGGER trg_invoices_project_client_update
BEFORE UPDATE OF client_id, project_id ON invoices
WHEN NEW.project_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM projects
        WHERE projects.id = NEW.project_id
          AND projects.client_id = NEW.client_id
    )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PROJECT_CLIENT_MISMATCH');
END;

CREATE TRIGGER trg_projects_client_change_invoice_guard
BEFORE UPDATE OF client_id ON projects
WHEN NEW.client_id IS NOT OLD.client_id
    AND EXISTS (
        SELECT 1
        FROM invoices
        WHERE invoices.project_id = OLD.id
          AND invoices.client_id IS NOT NEW.client_id
    )
BEGIN
    SELECT RAISE(ABORT, 'PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES');
END;
