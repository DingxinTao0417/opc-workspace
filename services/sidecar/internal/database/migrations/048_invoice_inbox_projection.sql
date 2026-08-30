CREATE INDEX idx_inbox_invoice_due_sources
ON inbox_items(source_entity_id, status, id)
WHERE source_entity_type = 'invoice_due';

CREATE TABLE invoice_payment_consistency_v48_guard (
    valid INTEGER NOT NULL
        CONSTRAINT invoice_payment_consistency_v48_guard CHECK (valid = 1)
);

INSERT INTO invoice_payment_consistency_v48_guard(valid)
SELECT CASE
    WHEN (
        SELECT COUNT(*)
        FROM invoices
        WHERE invoices.status = 'paid'
          AND (
              SELECT COUNT(*)
              FROM financial_entries
              WHERE financial_entries.invoice_id = invoices.id
                AND financial_entries.type = 'income'
                AND financial_entries.status = 'confirmed'
                AND financial_entries.amount_minor = invoices.amount_minor
                AND financial_entries.currency = invoices.currency
                AND financial_entries.occurred_on = invoices.paid_date
                AND financial_entries.client_id = invoices.client_id
                AND financial_entries.project_id IS invoices.project_id
          ) <> 1
    ) = 0
    AND (
        SELECT COUNT(*)
        FROM financial_entries
        WHERE financial_entries.invoice_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1
              FROM invoices
              WHERE invoices.id = financial_entries.invoice_id
                AND invoices.status = 'paid'
                AND financial_entries.type = 'income'
                AND financial_entries.status = 'confirmed'
                AND financial_entries.amount_minor = invoices.amount_minor
                AND financial_entries.currency = invoices.currency
                AND financial_entries.occurred_on = invoices.paid_date
                AND financial_entries.client_id = invoices.client_id
                AND financial_entries.project_id IS invoices.project_id
          )
    ) = 0
    THEN 1
    ELSE 0
END;

DROP TABLE invoice_payment_consistency_v48_guard;

CREATE TRIGGER invoice_due_inbox_source_insert_guard
BEFORE INSERT ON inbox_items
WHEN NEW.source_entity_type = 'invoice_due'
BEGIN
    SELECT CASE
        WHEN NEW.kind <> 'event'
          OR NEW.source_entity_id IS NULL
          OR NEW.source_event_key IS NULL
          OR (
              NEW.source_deleted_at IS NOT NULL
              AND (
                  NEW.status NOT IN ('resolved', 'dismissed')
                  OR julianday(NEW.source_deleted_at) IS NULL
                  OR strftime('%Y-%m-%dT%H:%M:%S', NEW.source_deleted_at, '+0 seconds') <> substr(NEW.source_deleted_at, 1, 19)
                  OR CAST(substr(NEW.source_deleted_at, 12, 2) AS INTEGER) NOT BETWEEN 0 AND 23
                  OR length(NEW.source_deleted_at) NOT BETWEEN 20 AND 30
                  OR substr(NEW.source_deleted_at, 5, 1) <> '-'
                  OR substr(NEW.source_deleted_at, 8, 1) <> '-'
                  OR substr(NEW.source_deleted_at, 11, 1) <> 'T'
                  OR substr(NEW.source_deleted_at, 14, 1) <> ':'
                  OR substr(NEW.source_deleted_at, 17, 1) <> ':'
                  OR substr(NEW.source_deleted_at, -1, 1) <> 'Z'
                  OR NOT (
                      length(NEW.source_deleted_at) = 20
                      OR (
                          length(NEW.source_deleted_at) BETWEEN 22 AND 30
                          AND substr(NEW.source_deleted_at, 20, 1) = '.'
                          AND substr(
                              NEW.source_deleted_at, 21,
                              length(NEW.source_deleted_at) - 21
                          ) NOT GLOB '*[^0-9]*'
                      )
                  )
              )
          )
          OR (SELECT COUNT(*) FROM json_each(NEW.payload_json)) <> 14
          OR EXISTS (
              SELECT 1
              FROM json_each(NEW.payload_json)
              WHERE key NOT IN (
                  'invoice_id', 'invoice_number', 'client_id', 'client_name',
                  'project_id', 'project_name', 'amount_minor', 'currency',
                  'due_date', 'due_state', 'occurrence_date', 'invoice_version',
                  'projected_at', 'lead_days'
              )
          )
          OR json_type(NEW.payload_json, '$.invoice_id') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.invoice_id') <> NEW.source_entity_id
          OR json_type(NEW.payload_json, '$.invoice_number') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.invoice_number'))) = 0
          OR json_extract(NEW.payload_json, '$.invoice_number') <> trim(json_extract(NEW.payload_json, '$.invoice_number'))
          OR json_type(NEW.payload_json, '$.client_id') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.client_id'))) = 0
          OR json_type(NEW.payload_json, '$.client_name') IS NOT 'text'
          OR length(trim(json_extract(NEW.payload_json, '$.client_name'))) = 0
          OR json_extract(NEW.payload_json, '$.client_name') <> trim(json_extract(NEW.payload_json, '$.client_name'))
          OR json_type(NEW.payload_json, '$.project_id') IS NULL
          OR json_type(NEW.payload_json, '$.project_id') NOT IN ('null', 'text')
          OR json_type(NEW.payload_json, '$.project_name') IS NULL
          OR json_type(NEW.payload_json, '$.project_name') NOT IN ('null', 'text')
          OR (json_type(NEW.payload_json, '$.project_id') = 'null') <> (json_type(NEW.payload_json, '$.project_name') = 'null')
          OR (
              json_type(NEW.payload_json, '$.project_name') = 'text'
              AND (
                  length(trim(json_extract(NEW.payload_json, '$.project_name'))) = 0
                  OR json_extract(NEW.payload_json, '$.project_name') <> trim(json_extract(NEW.payload_json, '$.project_name'))
              )
          )
          OR json_type(NEW.payload_json, '$.amount_minor') IS NOT 'integer'
          OR json_type(NEW.payload_json, '$.currency') IS NOT 'text'
          OR length(json_extract(NEW.payload_json, '$.currency')) <> 3
          OR json_extract(NEW.payload_json, '$.currency') <> upper(json_extract(NEW.payload_json, '$.currency'))
          OR json_extract(NEW.payload_json, '$.currency') GLOB '*[^A-Z]*'
          OR json_type(NEW.payload_json, '$.due_date') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.due_state') IS NOT 'text'
          OR json_extract(NEW.payload_json, '$.due_state') NOT IN ('due_soon', 'due', 'overdue')
          OR json_type(NEW.payload_json, '$.occurrence_date') IS NOT 'text'
          OR json_type(NEW.payload_json, '$.invoice_version') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.invoice_version') < 1
          OR json_type(NEW.payload_json, '$.projected_at') IS NOT 'text'
          OR length(json_extract(NEW.payload_json, '$.projected_at')) = 0
          OR julianday(json_extract(NEW.payload_json, '$.projected_at')) IS NULL
          OR strftime(
              '%Y-%m-%dT%H:%M:%S',
              json_extract(NEW.payload_json, '$.projected_at'),
              '+0 seconds'
          ) <> substr(json_extract(NEW.payload_json, '$.projected_at'), 1, 19)
          OR CAST(substr(json_extract(NEW.payload_json, '$.projected_at'), 12, 2) AS INTEGER) NOT BETWEEN 0 AND 23
          OR length(json_extract(NEW.payload_json, '$.projected_at')) NOT BETWEEN 20 AND 30
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), 5, 1) <> '-'
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), 8, 1) <> '-'
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), 11, 1) <> 'T'
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), 14, 1) <> ':'
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), 17, 1) <> ':'
          OR substr(json_extract(NEW.payload_json, '$.projected_at'), -1, 1) <> 'Z'
          OR NOT (
              length(json_extract(NEW.payload_json, '$.projected_at')) = 20
              OR (
                  length(json_extract(NEW.payload_json, '$.projected_at')) BETWEEN 22 AND 30
                  AND substr(json_extract(NEW.payload_json, '$.projected_at'), 20, 1) = '.'
                  AND substr(
                      json_extract(NEW.payload_json, '$.projected_at'), 21,
                      length(json_extract(NEW.payload_json, '$.projected_at')) - 21
                  ) NOT GLOB '*[^0-9]*'
              )
          )
          OR NEW.due_at IS NULL
          OR julianday(NEW.due_at) IS NULL
          OR strftime('%Y-%m-%dT%H:%M:%S', NEW.due_at, '+0 seconds') <> substr(NEW.due_at, 1, 19)
          OR CAST(substr(NEW.due_at, 12, 2) AS INTEGER) NOT BETWEEN 0 AND 23
          OR length(NEW.due_at) NOT BETWEEN 20 AND 30
          OR substr(NEW.due_at, 5, 1) <> '-'
          OR substr(NEW.due_at, 8, 1) <> '-'
          OR substr(NEW.due_at, 11, 1) <> 'T'
          OR substr(NEW.due_at, 14, 1) <> ':'
          OR substr(NEW.due_at, 17, 1) <> ':'
          OR substr(NEW.due_at, -1, 1) <> 'Z'
          OR NOT (
              length(NEW.due_at) = 20
              OR (
                  length(NEW.due_at) BETWEEN 22 AND 30
                  AND substr(NEW.due_at, 20, 1) = '.'
                  AND substr(NEW.due_at, 21, length(NEW.due_at) - 21) NOT GLOB '*[^0-9]*'
              )
          )
          OR json_type(NEW.payload_json, '$.lead_days') IS NOT 'integer'
          OR json_extract(NEW.payload_json, '$.lead_days') <> 3
          OR length(json_extract(NEW.payload_json, '$.due_date')) <> 10
          OR strftime('%Y-%m-%d', json_extract(NEW.payload_json, '$.due_date'), '+0 days') <> json_extract(NEW.payload_json, '$.due_date')
          OR length(json_extract(NEW.payload_json, '$.occurrence_date')) <> 10
          OR strftime('%Y-%m-%d', json_extract(NEW.payload_json, '$.occurrence_date'), '+0 days') <> json_extract(NEW.payload_json, '$.occurrence_date')
          OR NEW.source_event_key <> CASE json_extract(NEW.payload_json, '$.due_state')
              WHEN 'due_soon' THEN 'invoice:' || NEW.source_entity_id || ':due_soon:' || json_extract(NEW.payload_json, '$.due_date')
              WHEN 'due' THEN 'invoice:' || NEW.source_entity_id || ':due:' || json_extract(NEW.payload_json, '$.due_date')
              ELSE 'invoice:' || NEW.source_entity_id || ':overdue:' || json_extract(NEW.payload_json, '$.occurrence_date')
          END
          OR (
              json_extract(NEW.payload_json, '$.due_state') = 'due_soon'
              AND NOT (
                  json_extract(NEW.payload_json, '$.occurrence_date') >= date(json_extract(NEW.payload_json, '$.due_date'), '-3 days')
                  AND json_extract(NEW.payload_json, '$.occurrence_date') < json_extract(NEW.payload_json, '$.due_date')
              )
          )
          OR (
              json_extract(NEW.payload_json, '$.due_state') = 'due'
              AND json_extract(NEW.payload_json, '$.occurrence_date') <> json_extract(NEW.payload_json, '$.due_date')
          )
          OR (
              json_extract(NEW.payload_json, '$.due_state') = 'overdue'
              AND json_extract(NEW.payload_json, '$.occurrence_date') <= json_extract(NEW.payload_json, '$.due_date')
          )
        THEN RAISE(ABORT, 'INVALID_INVOICE_DUE_INBOX_SOURCE')
    END;

    SELECT CASE
        WHEN NEW.source_deleted_at IS NULL
          AND NOT EXISTS (
            SELECT 1
            FROM invoices
            WHERE invoices.id = NEW.source_entity_id
              AND invoices.status <> 'draft'
              AND invoices.invoice_number = json_extract(NEW.payload_json, '$.invoice_number')
              AND invoices.client_id = json_extract(NEW.payload_json, '$.client_id')
              AND invoices.project_id IS json_extract(NEW.payload_json, '$.project_id')
              AND invoices.amount_minor = json_extract(NEW.payload_json, '$.amount_minor')
              AND invoices.currency = json_extract(NEW.payload_json, '$.currency')
              AND invoices.due_date = json_extract(NEW.payload_json, '$.due_date')
              AND invoices.version >= json_extract(NEW.payload_json, '$.invoice_version')
        )
        THEN RAISE(ABORT, 'INVOICE_DUE_INBOX_SOURCE_NOT_FOUND')
    END;

    SELECT CASE
        WHEN NEW.source_deleted_at IS NOT NULL
          AND EXISTS (
              SELECT 1
              FROM invoices
              WHERE invoices.id = NEW.source_entity_id
          )
        THEN RAISE(ABORT, 'INVOICE_DUE_DELETED_SOURCE_COLLISION')
    END;
END;

CREATE TRIGGER invoice_due_inbox_source_update_into_guard
BEFORE UPDATE OF source_entity_type ON inbox_items
WHEN OLD.source_entity_type <> 'invoice_due'
  AND NEW.source_entity_type = 'invoice_due'
BEGIN
    SELECT RAISE(ABORT, 'INVALID_INVOICE_DUE_INBOX_SOURCE');
END;

CREATE TRIGGER invoice_due_inbox_source_identity_immutable
BEFORE UPDATE OF kind, source_entity_type, source_entity_id, source_event_key, due_at, payload_json
ON inbox_items
WHEN OLD.source_entity_type = 'invoice_due'
  AND (
      NEW.kind IS NOT OLD.kind
      OR NEW.source_entity_type IS NOT OLD.source_entity_type
      OR NEW.source_entity_id IS NOT OLD.source_entity_id
      OR NEW.source_event_key IS NOT OLD.source_event_key
      OR NEW.due_at IS NOT OLD.due_at
      OR NEW.payload_json IS NOT OLD.payload_json
  )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_DUE_INBOX_SOURCE_IMMUTABLE');
END;

CREATE TRIGGER invoice_due_inbox_source_deleted_once
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'invoice_due'
  AND OLD.source_deleted_at IS NOT NULL
  AND NEW.source_deleted_at IS NOT OLD.source_deleted_at
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_DUE_INBOX_SOURCE_DELETION_IMMUTABLE');
END;

CREATE TRIGGER invoice_due_inbox_source_delete_requires_terminal
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'invoice_due'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND NEW.status NOT IN ('resolved', 'dismissed')
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_DUE_INBOX_SOURCE_ACTIVE');
END;

CREATE TRIGGER invoice_due_inbox_source_delete_timestamp_guard
BEFORE UPDATE OF source_deleted_at ON inbox_items
WHEN OLD.source_entity_type = 'invoice_due'
  AND OLD.source_deleted_at IS NULL
  AND NEW.source_deleted_at IS NOT NULL
  AND (
      julianday(NEW.source_deleted_at) IS NULL
      OR strftime('%Y-%m-%dT%H:%M:%S', NEW.source_deleted_at, '+0 seconds') <> substr(NEW.source_deleted_at, 1, 19)
      OR CAST(substr(NEW.source_deleted_at, 12, 2) AS INTEGER) NOT BETWEEN 0 AND 23
      OR length(NEW.source_deleted_at) NOT BETWEEN 20 AND 30
      OR substr(NEW.source_deleted_at, 5, 1) <> '-'
      OR substr(NEW.source_deleted_at, 8, 1) <> '-'
      OR substr(NEW.source_deleted_at, 11, 1) <> 'T'
      OR substr(NEW.source_deleted_at, 14, 1) <> ':'
      OR substr(NEW.source_deleted_at, 17, 1) <> ':'
      OR substr(NEW.source_deleted_at, -1, 1) <> 'Z'
      OR NOT (
          length(NEW.source_deleted_at) = 20
          OR (
              length(NEW.source_deleted_at) BETWEEN 22 AND 30
              AND substr(NEW.source_deleted_at, 20, 1) = '.'
              AND substr(
                  NEW.source_deleted_at, 21,
                  length(NEW.source_deleted_at) - 21
              ) NOT GLOB '*[^0-9]*'
          )
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'INVALID_INVOICE_DUE_SOURCE_DELETED_AT');
END;

CREATE TRIGGER invoice_delete_requires_due_source_coordination
BEFORE DELETE ON invoices
WHEN EXISTS (
    SELECT 1
    FROM inbox_items
    WHERE source_entity_type = 'invoice_due'
      AND source_entity_id = OLD.id
      AND source_deleted_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_DUE_INBOX_SOURCE_NOT_COORDINATED');
END;

CREATE TRIGGER invoice_paid_requires_matching_entry
BEFORE UPDATE OF status, paid_date ON invoices
WHEN NEW.status = 'paid'
  AND (OLD.status <> 'paid' OR NEW.paid_date IS NOT OLD.paid_date)
  AND NOT EXISTS (
      SELECT 1
      FROM financial_entries
      WHERE financial_entries.invoice_id = NEW.id
        AND financial_entries.type = 'income'
        AND financial_entries.status = 'confirmed'
        AND financial_entries.amount_minor = NEW.amount_minor
        AND financial_entries.currency = NEW.currency
        AND financial_entries.occurred_on = NEW.paid_date
        AND financial_entries.client_id = NEW.client_id
        AND financial_entries.project_id IS NEW.project_id
  )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PAID_ENTRY_MISMATCH');
END;

CREATE TRIGGER invoice_linked_financial_entry_insert_guard
BEFORE INSERT ON financial_entries
WHEN NEW.invoice_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM invoices
      WHERE invoices.id = NEW.invoice_id
        AND invoices.status IN ('viewed', 'overdue', 'paid')
        AND NEW.type = 'income'
        AND NEW.status = 'confirmed'
        AND NEW.amount_minor = invoices.amount_minor
        AND NEW.currency = invoices.currency
        AND NEW.client_id = invoices.client_id
        AND NEW.project_id IS invoices.project_id
        AND (
            invoices.status <> 'paid'
            OR NEW.occurred_on = invoices.paid_date
        )
  )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_LINKED_FINANCIAL_ENTRY_MISMATCH');
END;

CREATE TRIGGER invoice_linked_financial_entry_immutable
BEFORE UPDATE ON financial_entries
WHEN OLD.invoice_id IS NOT NULL
  AND (
      NEW.id IS NOT OLD.id
      OR NEW.type IS NOT OLD.type
      OR NEW.amount_minor IS NOT OLD.amount_minor
      OR NEW.currency IS NOT OLD.currency
      OR NEW.occurred_on IS NOT OLD.occurred_on
      OR NEW.status IS NOT OLD.status
      OR NEW.category IS NOT OLD.category
      OR NEW.client_id IS NOT OLD.client_id
      OR NEW.project_id IS NOT OLD.project_id
      OR NEW.invoice_id IS NOT OLD.invoice_id
      OR NEW.notes IS NOT OLD.notes
      OR NEW.created_by_actor_id IS NOT OLD.created_by_actor_id
      OR NEW.voided_at IS NOT OLD.voided_at
      OR NEW.voided_by_actor_id IS NOT OLD.voided_by_actor_id
      OR NEW.void_reason IS NOT OLD.void_reason
      OR NEW.version IS NOT OLD.version
      OR NEW.created_at IS NOT OLD.created_at
      OR NEW.updated_at IS NOT OLD.updated_at
  )
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE');
END;
