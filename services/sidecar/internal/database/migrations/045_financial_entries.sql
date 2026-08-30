CREATE TABLE financial_entries (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL
        CHECK (type IN ('income', 'expense')),
    amount_minor INTEGER NOT NULL
        CHECK (amount_minor BETWEEN 1 AND 9000000000000000),
    currency TEXT NOT NULL DEFAULT 'CNY'
        CHECK (length(currency) = 3 AND currency = upper(currency)),
    occurred_on TEXT NOT NULL
        CHECK (
            length(occurred_on) = 10
            AND strftime('%Y-%m-%d', occurred_on, '+0 days') = occurred_on
        ),
    status TEXT NOT NULL DEFAULT 'confirmed'
        CHECK (status IN ('pending', 'confirmed', 'voided')),
    category TEXT NOT NULL
        CHECK (category = trim(category) AND length(category) BETWEEN 1 AND 80),
    client_id TEXT REFERENCES clients(id) ON DELETE RESTRICT,
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    invoice_id TEXT REFERENCES invoices(id) ON DELETE RESTRICT,
    notes TEXT NOT NULL DEFAULT ''
        CHECK (length(notes) <= 10000),
    created_by_actor_id TEXT NOT NULL REFERENCES actors(id) ON DELETE RESTRICT,
    voided_at TEXT,
    voided_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    void_reason TEXT,
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL
        CHECK (length(updated_at) > 0),
    CHECK (invoice_id IS NULL OR type = 'income'),
    CHECK (
        (status = 'voided'
            AND voided_at IS NOT NULL
            AND voided_by_actor_id IS NOT NULL
            AND void_reason IS NOT NULL
            AND length(trim(void_reason)) BETWEEN 1 AND 1000)
        OR
        (status <> 'voided'
            AND voided_at IS NULL
            AND voided_by_actor_id IS NULL
            AND void_reason IS NULL)
    )
);

CREATE INDEX idx_financial_entries_occurred
ON financial_entries(occurred_on DESC, created_at DESC, id);

CREATE INDEX idx_financial_entries_type_status
ON financial_entries(type, status, occurred_on DESC, id);

CREATE INDEX idx_financial_entries_client
ON financial_entries(client_id, occurred_on DESC, id)
WHERE client_id IS NOT NULL;

CREATE INDEX idx_financial_entries_project
ON financial_entries(project_id, occurred_on DESC, id)
WHERE project_id IS NOT NULL;

CREATE UNIQUE INDEX ux_financial_entries_active_invoice
ON financial_entries(invoice_id)
WHERE invoice_id IS NOT NULL AND status <> 'voided';

CREATE TRIGGER trg_financial_entries_immutable_identity
BEFORE UPDATE ON financial_entries
WHEN NEW.id IS NOT OLD.id
    OR NEW.created_by_actor_id IS NOT OLD.created_by_actor_id
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.invoice_id IS NOT OLD.invoice_id
BEGIN
    SELECT RAISE(ABORT, 'FINANCIAL_ENTRY_IDENTITY_IMMUTABLE');
END;

CREATE TRIGGER trg_financial_entries_voided_final
BEFORE UPDATE ON financial_entries
WHEN OLD.status = 'voided'
BEGIN
    SELECT RAISE(ABORT, 'FINANCIAL_ENTRY_VOIDED_FINAL');
END;

CREATE TRIGGER trg_financial_entries_no_hard_delete
BEFORE DELETE ON financial_entries
BEGIN
    SELECT RAISE(ABORT, 'FINANCIAL_ENTRY_HARD_DELETE_FORBIDDEN');
END;
