package database

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInvoiceFactsMigrationOpensEmptyWorkspaceAtV46(t *testing.T) {
	store, gate, err := OpenBeforeDestructiveMigrations(filepath.Join(t.TempDir(), "invoice-empty.db"))
	if err != nil {
		t.Fatalf("OpenBeforeDestructiveMigrations() error = %v", err)
	}
	defer store.Close()
	if gate != nil || store.SchemaVersion != 46 {
		t.Fatalf("empty invoice workspace schema=%d gate=%#v, want schema 46 without a gate", store.SchemaVersion, gate)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM invoices"); got != 0 {
		t.Fatalf("empty invoice count = %d, want 0", got)
	}
}

func TestInvoiceFactsMigrationGatesV45AndPreservesLegacyInvoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invoice-v45.db")
	v45 := openDatabaseAtVersion(t, path, 45)
	const (
		clientID  = "018f0000-0000-7000-8000-000000004601"
		projectID = "018f0000-0000-7000-8000-000000004602"
		draftID   = "018f0000-0000-7000-8000-000000004603"
		paidID    = "018f0000-0000-7000-8000-000000004604"
		entryID   = "018f0000-0000-7000-8000-000000004605"
	)
	if _, err := v45.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, '迁移发票客户', 'active', '2026-08-01T08:00:00Z', '2026-08-01T08:00:00Z')
	`, clientID); err != nil {
		_ = v45.Close()
		t.Fatalf("seed v45 invoice client: %v", err)
	}
	if _, err := v45.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, '迁移发票项目', ?, 'completed', '2026-08-01T08:00:00Z', '2026-08-02T08:00:00Z')
	`, projectID, clientID); err != nil {
		_ = v45.Close()
		t.Fatalf("seed v45 invoice project: %v", err)
	}
	if _, err := v45.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, project_id, amount_minor, currency, status,
			issue_date, due_date, paid_date, notes, created_at, updated_at
		) VALUES
			(?, 'INV-2026-041', ?, ?, 128000, 'CNY', 'draft', '2026-08-02', '2026-08-31', NULL, '迁移草稿', '2026-08-02T08:00:00Z', '2026-08-02T08:00:00Z'),
			(?, 'INV-2026-042', ?, NULL, 256000, 'USD', 'paid', '2026-07-01', '2026-07-31', '2026-07-20', '迁移已付款', '2026-07-01T08:00:00Z', '2026-07-20T08:00:00Z')
	`, draftID, clientID, projectID, paidID, clientID); err != nil {
		_ = v45.Close()
		t.Fatalf("seed v45 invoices: %v", err)
	}
	if _, err := v45.Exec(`
		INSERT INTO financial_entries(
			id, type, amount_minor, currency, occurred_on, status, category,
			client_id, invoice_id, notes, created_by_actor_id, version, created_at, updated_at
		) VALUES (?, 'income', 256000, 'USD', '2026-07-20', 'confirmed', '历史发票回款',
		          ?, ?, '', '00000000-0000-5000-8000-000000000001', 1,
		          '2026-07-20T08:00:00Z', '2026-07-20T08:00:00Z')
	`, entryID, clientID, paidID); err != nil {
		_ = v45.Close()
		t.Fatalf("seed v45 invoice-linked financial entry: %v", err)
	}
	if err := v45.Close(); err != nil {
		t.Fatalf("close v45 invoice fixture: %v", err)
	}

	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatalf("open v45 invoice migration gate: %v", err)
	}
	if gated.SchemaVersion != 45 || gate == nil || gate.CurrentVersion != 45 || gate.TargetVersion != 46 || !reflect.DeepEqual(gate.PendingVersions, []int{46}) {
		_ = gated.Close()
		t.Fatalf("v45 invoice migration gate: store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	if got := readInt64(t, gated.SQL, "SELECT COUNT(*) FROM pragma_table_info('invoices') WHERE name = 'version'"); got != 0 {
		_ = gated.Close()
		t.Fatalf("gated v45 invoice version column count = %d, want 0", got)
	}
	if got := readInt64(t, gated.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'invoice_number_sequences'"); got != 0 {
		_ = gated.Close()
		t.Fatalf("gated v45 invoice sequence table count = %d, want 0", got)
	}
	if got := readInt64(t, gated.SQL, "SELECT COUNT(*) FROM invoices"); got != 2 {
		_ = gated.Close()
		t.Fatalf("gated v45 invoice count = %d, want 2", got)
	}
	if err := gated.Close(); err != nil {
		t.Fatalf("close gated v45 invoice store: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("apply v46 invoice migration: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 46 {
		t.Fatalf("invoice schema version = %d, want 46", store.SchemaVersion)
	}

	var draft struct {
		Number, ClientID, ProjectID, Currency, Status, IssueDate, DueDate, Notes, CreatedAt, UpdatedAt string
		AmountMinor, Version                                                                           int64
		PaidDate                                                                                       sql.NullString
	}
	if err := store.SQL.QueryRow(`
		SELECT invoice_number, client_id, project_id, amount_minor, currency, status,
		       issue_date, due_date, paid_date, notes, version, created_at, updated_at
		FROM invoices WHERE id = ?
	`, draftID).Scan(
		&draft.Number, &draft.ClientID, &draft.ProjectID, &draft.AmountMinor, &draft.Currency, &draft.Status,
		&draft.IssueDate, &draft.DueDate, &draft.PaidDate, &draft.Notes, &draft.Version, &draft.CreatedAt, &draft.UpdatedAt,
	); err != nil {
		t.Fatalf("read migrated draft invoice: %v", err)
	}
	if draft.Number != "INV-2026-041" || draft.ClientID != clientID || draft.ProjectID != projectID ||
		draft.AmountMinor != 128000 || draft.Currency != "CNY" || draft.Status != "draft" ||
		draft.IssueDate != "2026-08-02" || draft.DueDate != "2026-08-31" || draft.PaidDate.Valid ||
		draft.Notes != "迁移草稿" || draft.Version != 1 || draft.CreatedAt != "2026-08-02T08:00:00Z" || draft.UpdatedAt != "2026-08-02T08:00:00Z" {
		t.Fatalf("migrated draft invoice = %#v", draft)
	}
	var paidStatus, paidDate, paidNotes string
	var paidVersion int64
	if err := store.SQL.QueryRow("SELECT status, paid_date, notes, version FROM invoices WHERE id = ?", paidID).Scan(&paidStatus, &paidDate, &paidNotes, &paidVersion); err != nil {
		t.Fatalf("read migrated paid invoice: %v", err)
	}
	if paidStatus != "paid" || paidDate != "2026-07-20" || paidNotes != "迁移已付款" || paidVersion != 1 {
		t.Fatalf("migrated paid invoice status=%q paid_date=%q notes=%q version=%d", paidStatus, paidDate, paidNotes, paidVersion)
	}
	var linkedInvoiceID string
	if err := store.SQL.QueryRow("SELECT invoice_id FROM financial_entries WHERE id = ?", entryID).Scan(&linkedInvoiceID); err != nil {
		t.Fatalf("read migrated invoice-linked financial entry: %v", err)
	}
	if linkedInvoiceID != paidID {
		t.Fatalf("migrated financial entry invoice_id=%q, want %q", linkedInvoiceID, paidID)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestInvoiceFactsMigrationEnforcesConstraintsForeignKeysAndTriggers(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "invoice-constraints.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	const (
		clientOne   = "018f0000-0000-7000-8000-000000004611"
		clientTwo   = "018f0000-0000-7000-8000-000000004612"
		projectOne  = "018f0000-0000-7000-8000-000000004613"
		projectTwo  = "018f0000-0000-7000-8000-000000004614"
		projectMove = "018f0000-0000-7000-8000-000000004615"
	)
	if _, err := store.SQL.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at) VALUES
			(?, '约束客户一', 'active', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, '约束客户二', 'active', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, clientOne, clientTwo); err != nil {
		t.Fatalf("seed invoice constraint clients: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at) VALUES
			(?, '约束项目一', ?, 'completed', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, '约束项目二', ?, 'completed', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'),
			(?, '移动目标项目', ?, 'completed', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, projectOne, clientOne, projectTwo, clientTwo, projectMove, clientOne); err != nil {
		t.Fatalf("seed invoice constraint projects: %v", err)
	}

	insertInvoice := func(id, number, clientID string, projectID any, amount any, currency, status, issueDate, dueDate string, paidDate any, notes string, version int64) error {
		_, err := store.SQL.Exec(`
			INSERT INTO invoices(
				id, invoice_number, client_id, project_id, amount_minor, currency, status,
				issue_date, due_date, paid_date, notes, version, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
		`, id, number, clientID, projectID, amount, currency, status, issueDate, dueDate, paidDate, notes, version)
		return err
	}

	const validID = "018f0000-0000-7000-8000-000000004620"
	if err := insertInvoice(validID, "INV-2026-100", clientOne, projectOne, int64(128000), "CNY", "draft", "2026-08-01", "2026-08-31", nil, "有效发票", 1); err != nil {
		t.Fatalf("insert valid invoice: %v", err)
	}
	overlongNotes := strings.Repeat("n", 10001)
	invalid := []struct {
		name, id, number, clientID string
		projectID                  any
		amount                     any
		currency, status           string
		issueDate, dueDate         string
		paidDate                   any
		notes                      string
		version                    int64
	}{
		{name: "blank number", id: "invalid-01", number: "", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "untrimmed number", id: "invalid-02", number: " INV-2026-101 ", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "duplicate number", id: "invalid-03", number: "INV-2026-100", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "zero amount", id: "invalid-04", number: "INV-2026-104", clientID: clientOne, amount: int64(0), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "fractional amount", id: "invalid-05", number: "INV-2026-105", clientID: clientOne, amount: 1.5, currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "amount above maximum", id: "invalid-06", number: "INV-2026-106", clientID: clientOne, amount: int64(9000000000000001), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "lowercase currency", id: "invalid-07", number: "INV-2026-107", clientID: clientOne, amount: int64(1), currency: "cny", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "non-letter currency", id: "invalid-08", number: "INV-2026-108", clientID: clientOne, amount: int64(1), currency: "US1", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "invalid status", id: "invalid-09", number: "INV-2026-109", clientID: clientOne, amount: int64(1), currency: "CNY", status: "archived", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "invalid issue date", id: "invalid-10", number: "INV-2026-110", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-02-30", dueDate: "2026-08-31", version: 1},
		{name: "invalid due date", id: "invalid-11", number: "INV-2026-111", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-02-30", version: 1},
		{name: "due before issue", id: "invalid-12", number: "INV-2026-112", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-31", dueDate: "2026-08-01", version: 1},
		{name: "paid without paid date", id: "invalid-13", number: "INV-2026-113", clientID: clientOne, amount: int64(1), currency: "CNY", status: "paid", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
		{name: "draft with paid date", id: "invalid-14", number: "INV-2026-114", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", paidDate: "2026-08-20", version: 1},
		{name: "invalid paid date", id: "invalid-15", number: "INV-2026-115", clientID: clientOne, amount: int64(1), currency: "CNY", status: "paid", issueDate: "2026-08-01", dueDate: "2026-08-31", paidDate: "2026-02-30", version: 1},
		{name: "zero version", id: "invalid-16", number: "INV-2026-116", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 0},
		{name: "overlong notes", id: "invalid-17", number: "INV-2026-117", clientID: clientOne, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", notes: overlongNotes, version: 1},
		{name: "project client mismatch", id: "invalid-18", number: "INV-2026-118", clientID: clientOne, projectID: projectTwo, amount: int64(1), currency: "CNY", status: "draft", issueDate: "2026-08-01", dueDate: "2026-08-31", version: 1},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			if err := insertInvoice(testCase.id, testCase.number, testCase.clientID, testCase.projectID, testCase.amount, testCase.currency, testCase.status, testCase.issueDate, testCase.dueDate, testCase.paidDate, testCase.notes, testCase.version); err == nil {
				t.Fatal("invalid invoice was accepted")
			}
		})
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM invoices"); got != 1 {
		t.Fatalf("invoice count after rejected rows = %d, want 1", got)
	}

	if _, err := store.SQL.Exec("UPDATE invoices SET invoice_number = 'INV-2026-CHANGED' WHERE id = ?", validID); err == nil || !strings.Contains(err.Error(), "INVOICE_IDENTITY_IMMUTABLE") {
		t.Fatalf("invoice identity mutation error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE invoices SET status = 'sent', version = 2, updated_at = '2026-08-02T00:00:00Z' WHERE id = ?", validID); err != nil {
		t.Fatalf("transition invoice draft to sent: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE invoices SET notes = 'changed' WHERE id = ?", validID); err == nil || !strings.Contains(err.Error(), "INVOICE_SENT_FIELDS_IMMUTABLE") {
		t.Fatalf("sent invoice field mutation error = %v", err)
	}
	for _, transition := range []struct {
		status   string
		paidDate any
	}{
		{status: "viewed"},
		{status: "overdue"},
		{status: "paid", paidDate: "2026-09-02"},
	} {
		if _, err := store.SQL.Exec("UPDATE invoices SET status = ?, paid_date = ?, version = version + 1, updated_at = '2026-09-02T00:00:00Z' WHERE id = ?", transition.status, transition.paidDate, validID); err != nil {
			t.Fatalf("transition invoice to %s: %v", transition.status, err)
		}
	}
	if _, err := store.SQL.Exec("UPDATE invoices SET status = 'sent', paid_date = NULL WHERE id = ?", validID); err == nil || !strings.Contains(err.Error(), "INVALID_INVOICE_STATUS_TRANSITION") {
		t.Fatalf("paid invoice reverse transition error = %v", err)
	}
	for _, clientValue := range []any{clientTwo, nil} {
		if _, err := store.SQL.Exec("UPDATE projects SET client_id = ? WHERE id = ?", clientValue, projectOne); err == nil || !strings.Contains(err.Error(), "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES") {
			t.Fatalf("invoice project client mutation to %#v error = %v", clientValue, err)
		}
	}
	var guardedProjectClient string
	if err := store.SQL.QueryRow("SELECT client_id FROM projects WHERE id = ?", projectOne).Scan(&guardedProjectClient); err != nil || guardedProjectClient != clientOne {
		t.Fatalf("guarded invoice project client = %q, want %q, err=%v", guardedProjectClient, clientOne, err)
	}
	if _, err := store.SQL.Exec("UPDATE projects SET client_id = ? WHERE id = ?", clientTwo, projectMove); err != nil {
		t.Fatalf("rebind project without invoices: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE projects SET client_id = ? WHERE id = ?", clientOne, projectMove); err != nil {
		t.Fatalf("restore project without invoices: %v", err)
	}

	const aggregateInvoiceID = "018f0000-0000-7000-8000-000000004621"
	projectOneBefore := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectOne)
	projectMoveBefore := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectMove)
	if err := insertInvoice(aggregateInvoiceID, "INV-2026-120", clientOne, projectOne, int64(5000), "CNY", "draft", "2026-08-05", "2026-08-20", nil, "", 1); err != nil {
		t.Fatalf("insert aggregate invoice: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectOne); got != projectOneBefore+1 {
		t.Fatalf("project version after invoice insert = %d, want %d", got, projectOneBefore+1)
	}
	if _, err := store.SQL.Exec("UPDATE invoices SET project_id = ?, version = version + 1 WHERE id = ?", projectMove, aggregateInvoiceID); err != nil {
		t.Fatalf("move draft invoice project: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectOne); got != projectOneBefore+2 {
		t.Fatalf("old project version after invoice move = %d, want %d", got, projectOneBefore+2)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectMove); got != projectMoveBefore+1 {
		t.Fatalf("new project version after invoice move = %d, want %d", got, projectMoveBefore+1)
	}
	if _, err := store.SQL.Exec("DELETE FROM invoices WHERE id = ?", aggregateInvoiceID); err != nil {
		t.Fatalf("delete draft aggregate invoice: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM projects WHERE id = ?", projectMove); got != projectMoveBefore+2 {
		t.Fatalf("project version after invoice delete = %d, want %d", got, projectMoveBefore+2)
	}

	const detachedInvoiceID = "018f0000-0000-7000-8000-000000004622"
	if err := insertInvoice(detachedInvoiceID, "INV-2026-121", clientTwo, projectTwo, int64(9000), "USD", "draft", "2026-08-05", "2026-08-20", nil, "", 1); err != nil {
		t.Fatalf("insert project detachment invoice: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM projects WHERE id = ?", projectTwo); err != nil {
		t.Fatalf("delete invoice project: %v", err)
	}
	var detachedProject sql.NullString
	if err := store.SQL.QueryRow("SELECT project_id FROM invoices WHERE id = ?", detachedInvoiceID).Scan(&detachedProject); err != nil || detachedProject.Valid {
		t.Fatalf("invoice project after project delete = %#v, err=%v", detachedProject, err)
	}
	if _, err := store.SQL.Exec("DELETE FROM clients WHERE id = ?", clientTwo); err == nil {
		t.Fatal("invoice client RESTRICT foreign key allowed client deletion")
	}

	if _, err := store.SQL.Exec("INSERT INTO invoice_number_sequences(year, last_value, updated_at) VALUES (2026, 0, '2026-08-01T00:00:00Z')"); err != nil {
		t.Fatalf("insert valid invoice number sequence: %v", err)
	}
	for _, statement := range []string{
		"INSERT INTO invoice_number_sequences(year, last_value, updated_at) VALUES (1999, 0, 'x')",
		"INSERT INTO invoice_number_sequences(year, last_value, updated_at) VALUES (2027, -1, 'x')",
		"INSERT INTO invoice_number_sequences(year, last_value, updated_at) VALUES (2027.5, 0, 'x')",
		"INSERT INTO invoice_number_sequences(year, last_value, updated_at) VALUES (2027, 1.5, 'x')",
	} {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid invoice number sequence was accepted: %s", statement)
		}
	}

	for _, index := range []string{"idx_invoices_client_id", "idx_invoices_project_id", "idx_invoices_status_due_date", "idx_invoices_issue_date"} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", index); got != 1 {
			t.Fatalf("invoice index %s count = %d, want 1", index, got)
		}
	}
	for _, trigger := range []string{
		"projects_version_after_invoice_insert", "projects_version_after_invoice_update", "projects_version_after_invoice_delete",
		"trg_invoices_identity_immutable", "trg_invoices_sent_fields_immutable", "trg_invoices_status_transition",
		"trg_invoices_project_client_insert", "trg_invoices_project_client_update", "trg_projects_client_change_invoice_guard",
	} {
		if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?", trigger); got != 1 {
			t.Fatalf("invoice trigger %s count = %d, want 1", trigger, got)
		}
	}
	assertForeignKey(t, store.SQL, "invoices", "client_id", "clients", "RESTRICT")
	assertForeignKey(t, store.SQL, "invoices", "project_id", "projects", "SET NULL")
	assertNoForeignKeyViolations(t, store.SQL)
}
