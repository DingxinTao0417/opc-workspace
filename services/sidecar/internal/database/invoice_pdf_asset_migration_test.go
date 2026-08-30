package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInvoicePDFAssetMigrationOpensEmptyWorkspaceAtV47(t *testing.T) {
	store, gate, err := OpenBeforeDestructiveMigrations(filepath.Join(t.TempDir(), "invoice-pdf-empty.db"))
	if err != nil {
		t.Fatalf("OpenBeforeDestructiveMigrations() error = %v", err)
	}
	defer store.Close()
	if gate != nil || store.SchemaVersion != 49 {
		t.Fatalf("empty invoice PDF workspace schema=%d gate=%#v, want schema 49 without a gate", store.SchemaVersion, gate)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM invoice_pdf_assets"); got != 0 {
		t.Fatalf("empty invoice PDF asset count = %d, want 0", got)
	}
}

func TestInvoicePDFAssetMigrationUpgradesV46AndEnforcesFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invoice-pdf-v46.db")
	v46 := openDatabaseAtVersion(t, path, 46)
	if err := v46.Close(); err != nil {
		t.Fatalf("close v46 invoice PDF fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("apply v47 invoice PDF migration: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 49 {
		t.Fatalf("invoice PDF schema version = %d, want 49", store.SchemaVersion)
	}

	const (
		clientID  = "018f0000-0000-7000-8000-000000004701"
		invoiceID = "018f0000-0000-7000-8000-000000004702"
		assetID   = "018f0000-0000-7000-8000-000000004703"
	)
	if _, err := store.SQL.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, 'PDF 迁移客户', 'active', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')
	`, clientID); err != nil {
		t.Fatalf("seed invoice PDF client: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, amount_minor, currency, status,
			issue_date, due_date, notes, version, created_at, updated_at
		) VALUES (?, 'INV-2026-471', ?, 128045, 'CNY', 'draft',
		          '2026-08-29', '2026-09-29', '', 3,
		          '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')
	`, invoiceID, clientID); err != nil {
		t.Fatalf("seed invoice PDF facts: %v", err)
	}
	validHash := strings.Repeat("a", 64)
	if _, err := store.SQL.Exec(`
		INSERT INTO invoice_pdf_assets(
			id, invoice_id, file_name, relative_path, mime_type,
			size_bytes, sha256, generated_from_version, generated_at,
			integrity_status, integrity_checked_at
		) VALUES (?, ?, 'invoice-INV-2026-471.pdf', ?, 'application/pdf',
		          512, ?, 3, '2026-08-29T01:00:00Z',
		          'verified', '2026-08-29T01:00:00Z')
	`, assetID, invoiceID, invoiceID+"/"+assetID+".pdf", validHash); err != nil {
		t.Fatalf("insert valid invoice PDF asset: %v", err)
	}

	invalidStatements := []string{
		"UPDATE invoice_pdf_assets SET relative_path = '../escape.pdf' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET file_name = '../escape.pdf' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET mime_type = 'text/plain' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET size_bytes = 0 WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET sha256 = 'ABC' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET id = upper(id), relative_path = invoice_id || '/' || upper(id) || '.pdf' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET id = '018f0000-0000-7000-8000-00000-004703', relative_path = invoice_id || '/018f0000-0000-7000-8000-00000-004703.pdf' WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET generated_from_version = 0 WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET generated_from_version = 4 WHERE id = '" + assetID + "'",
		"UPDATE invoice_pdf_assets SET integrity_status = 'unknown' WHERE id = '" + assetID + "'",
		"UPDATE invoices SET version = 2 WHERE id = '" + invoiceID + "'",
	}
	for _, statement := range invalidStatements {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid invoice PDF fact accepted: %s", statement)
		}
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_invoice_pdf_assets_generated_at'"); got != 1 {
		t.Fatalf("invoice PDF generated-at index count = %d, want 1", got)
	}
	for _, trigger := range []string{"trg_invoice_pdf_assets_version_insert", "trg_invoice_pdf_assets_version_update", "trg_invoices_pdf_version_guard"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name=?", trigger); got != 1 {
			t.Fatalf("invoice PDF trigger %s count = %d, want 1", trigger, got)
		}
	}
	if _, err := store.SQL.Exec("DELETE FROM invoices WHERE id = ?", invoiceID); err != nil {
		t.Fatalf("delete invoice with PDF asset: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM invoice_pdf_assets"); got != 0 {
		t.Fatalf("invoice delete left PDF asset rows = %d, want 0", got)
	}
	assertForeignKey(t, store.SQL, "invoice_pdf_assets", "invoice_id", "invoices", "CASCADE")
	assertNoForeignKeyViolations(t, store.SQL)
}
