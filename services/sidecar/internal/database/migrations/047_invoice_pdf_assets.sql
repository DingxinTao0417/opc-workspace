CREATE TABLE invoice_pdf_assets (
    id TEXT PRIMARY KEY
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND length(replace(id, '-', '')) = 32
            AND replace(id, '-', '') NOT GLOB '*[^0-9a-f]*'
        ),
    invoice_id TEXT NOT NULL UNIQUE
        REFERENCES invoices(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL
        CHECK (
            file_name = trim(file_name)
            AND length(file_name) BETWEEN 5 AND 180
            AND lower(substr(file_name, -4)) = '.pdf'
            AND instr(file_name, '/') = 0
            AND instr(file_name, char(92)) = 0
            AND instr(file_name, char(0)) = 0
        ),
    relative_path TEXT NOT NULL UNIQUE
        CHECK (
            relative_path = invoice_id || '/' || id || '.pdf'
        ),
    mime_type TEXT NOT NULL DEFAULT 'application/pdf'
        CHECK (mime_type = 'application/pdf'),
    size_bytes INTEGER NOT NULL
        CHECK (
            typeof(size_bytes) = 'integer'
            AND size_bytes > 0
            AND size_bytes <= 52428800
        ),
    sha256 TEXT NOT NULL
        CHECK (
            length(sha256) = 64
            AND sha256 = lower(sha256)
            AND sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    generated_from_version INTEGER NOT NULL
        CHECK (
            typeof(generated_from_version) = 'integer'
            AND generated_from_version >= 1
        ),
    generated_at TEXT NOT NULL
        CHECK (length(generated_at) > 0),
    integrity_status TEXT NOT NULL DEFAULT 'verified'
        CHECK (integrity_status IN ('verified', 'missing', 'mismatch')),
    integrity_checked_at TEXT NOT NULL
        CHECK (length(integrity_checked_at) > 0)
);

CREATE INDEX idx_invoice_pdf_assets_generated_at
ON invoice_pdf_assets(generated_at DESC, invoice_id);

CREATE TRIGGER trg_invoice_pdf_assets_version_insert
BEFORE INSERT ON invoice_pdf_assets
WHEN NOT EXISTS (
    SELECT 1
    FROM invoices
    WHERE invoices.id = NEW.invoice_id
      AND invoices.version >= NEW.generated_from_version
)
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PDF_VERSION_INVALID');
END;

CREATE TRIGGER trg_invoice_pdf_assets_version_update
BEFORE UPDATE OF invoice_id, generated_from_version ON invoice_pdf_assets
WHEN NOT EXISTS (
    SELECT 1
    FROM invoices
    WHERE invoices.id = NEW.invoice_id
      AND invoices.version >= NEW.generated_from_version
)
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PDF_VERSION_INVALID');
END;

CREATE TRIGGER trg_invoices_pdf_version_guard
BEFORE UPDATE OF version ON invoices
WHEN EXISTS (
    SELECT 1
    FROM invoice_pdf_assets
    WHERE invoice_pdf_assets.invoice_id = OLD.id
      AND invoice_pdf_assets.generated_from_version > NEW.version
)
BEGIN
    SELECT RAISE(ABORT, 'INVOICE_PDF_VERSION_INVALID');
END;
