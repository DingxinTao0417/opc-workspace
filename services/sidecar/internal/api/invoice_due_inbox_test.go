package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newInvoiceDueTestAPI(t *testing.T, now *time.Time) (*Router, *API, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "invoice-due.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	options := Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return *now },
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
		DiskSpaceScanInterval: -1, ScheduledBackupScanInterval: -1,
	}
	router, err := NewRouter(store.DB, options)
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router, &API{db: store.DB, options: options}, store
}

func TestInvoiceDueProjectionLifecycleLocalDatesAndIdempotency(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, 8, 29, 10, 0, 0, 0, location)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"到期客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"到期项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"project_id":%q,"amount_minor":128045,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`, client.ID, project.ID), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	draft := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1000,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`, client.ID), nil)

	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project due-soon Invoice: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("repeat due-soon scan: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 0, draft.ID)
	var dueSoon models.InboxItem
	if err := store.DB.First(&dueSoon, "source_event_key = ?", invoiceDueEventKey(invoice.ID, "due_soon", invoice.DueDate, "2026-08-29")).Error; err != nil {
		t.Fatalf("load due-soon source: %v", err)
	}
	var payload invoiceDueProjectionPayload
	if err := json.Unmarshal([]byte(dueSoon.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode due-soon payload: %v", err)
	}
	if payload.ClientName != client.Name || payload.ProjectName == nil || *payload.ProjectName != project.Name ||
		payload.DueState != "due_soon" || payload.OccurrenceDate != "2026-08-29" || payload.LeadDays != 3 ||
		payload.InvoiceVersion != 2 || dueSoon.Summary != "客户：到期客户 · 到期日：2026-09-01 · 金额：CNY 1280.45" {
		t.Fatalf("due-soon source=%#v payload=%#v", dueSoon, payload)
	}
	dueAt, err := time.Parse(time.RFC3339Nano, *dueSoon.DueAt)
	if err != nil || dueAt.In(location).Format("2006-01-02 15:04:05") != "2026-09-01 23:59:59" {
		t.Fatalf("due_at=%#v err=%v", dueSoon.DueAt, err)
	}

	now = time.Date(2026, 9, 1, 8, 0, 0, 0, location)
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project due-today Invoice: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 2, invoice.ID)

	now = time.Date(2026, 9, 2, 0, 1, 0, 0, location)
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project first overdue Invoice: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("repeat first overdue scan: %v", err)
	}
	var overdueInvoice models.Invoice
	if err := store.DB.First(&overdueInvoice, "id = ?", invoice.ID).Error; err != nil {
		t.Fatalf("load overdue Invoice: %v", err)
	}
	if overdueInvoice.Status != "overdue" || overdueInvoice.Version != 3 {
		t.Fatalf("overdue Invoice=%#v", overdueInvoice)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_overdue'", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 3, invoice.ID)

	now = time.Date(2026, 9, 3, 23, 30, 0, 0, location)
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project next overdue day: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 4, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_overdue'", 1, invoice.ID)

	paid := transitionInvoiceForTest(t, router.Engine, invoiceResponseFromModelForTest(t, store, invoice.ID), `{"action":"mark_paid","paid_date":"2026-09-03"}`, "invoice-due-paid")
	if paid.Status != "paid" {
		t.Fatalf("paid Invoice=%#v", paid)
	}
	now = time.Date(2026, 9, 4, 8, 0, 0, 0, location)
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("scan after Invoice payment: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 4, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=? AND status IN ('resolved','dismissed')", 0, invoice.ID)

	filtered := performRequest(router.Engine, http.MethodGet, "/api/v1/inbox-items?source_entity_type=invoice_due", nil, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter Invoice due Inbox = %d: %s", filtered.Code, filtered.Body.String())
	}
}

func invoiceResponseFromModelForTest(t *testing.T, store *database.Store, id string) invoiceResponse {
	t.Helper()
	row, err := loadInvoiceRow(store.DB, id)
	if err != nil {
		t.Fatalf("load Invoice response row: %v", err)
	}
	return invoiceResponseFromRow(row)
}

func TestInvoiceDueProjectionUsesLocalCalendarDateAndRollsBack(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Date(2026, 8, 29, 23, 30, 0, 0, location)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"跨日客户"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":9000,"currency":"USD","issue_date":"2026-08-01","due_date":"2026-08-30"}`, client.ID), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project local-date source: %v", err)
	}
	var localSource models.InboxItem
	if err := store.DB.First(&localSource, "source_event_key = ?", invoiceDueEventKey(invoice.ID, "due_soon", invoice.DueDate, "2026-08-29")).Error; err != nil {
		t.Fatalf("load local-date source: %v", err)
	}

	rollbackInvoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":5000,"currency":"USD","issue_date":"2026-08-01","due_date":"2026-08-28"}`, client.ID), nil)
	rollbackInvoice = transitionInvoiceForTest(t, router.Engine, rollbackInvoice, `{"action":"mark_sent"}`, "")
	rollbackInvoice = transitionInvoiceForTest(t, router.Engine, rollbackInvoice, `{"action":"mark_viewed"}`, "")
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_invoice_due_source_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type='inbox_item' AND NEW.action='source_projected'
		BEGIN
			SELECT RAISE(ABORT, 'FAIL_INVOICE_DUE_SOURCE_EVENT');
		END
	`).Error; err != nil {
		t.Fatalf("install source event failure: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err == nil {
		t.Fatal("Invoice due projection unexpectedly succeeded")
	}
	var persisted models.Invoice
	if err := store.DB.First(&persisted, "id = ?", rollbackInvoice.ID).Error; err != nil {
		t.Fatalf("load rolled-back Invoice: %v", err)
	}
	if persisted.Status != "viewed" || persisted.Version != rollbackInvoice.Version {
		t.Fatalf("Invoice after projection rollback=%#v", persisted)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?", 0, rollbackInvoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_overdue'", 0, rollbackInvoice.ID)
}

func TestInvoiceDueProjectionProcessesBoundedBatches(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	_, service, store := newInvoiceDueTestAPI(t, &now)
	clientID := uuid.NewString()
	if err := store.DB.Create(&models.Client{ID: clientID, Name: "批量客户", Status: "active", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}).Error; err != nil {
		t.Fatalf("seed batch client: %v", err)
	}
	for index := 0; index < 101; index++ {
		invoice := models.Invoice{
			ID: uuid.NewString(), InvoiceNumber: fmt.Sprintf("INV-BATCH-%03d", index), ClientID: clientID,
			AmountMinor: int64(10000 + index), Currency: "CNY", Status: "sent",
			IssueDate: "2026-08-01", DueDate: "2026-09-01", Version: 2,
			CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
		}
		if err := store.DB.Create(&invoice).Error; err != nil {
			t.Fatalf("seed batch Invoice %d: %v", index, err)
		}
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project first Invoice batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due'", 100)
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project second Invoice batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due'", 101)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND action='invoice_overdue'", 101)
}

func TestInvoiceDueProjectionRunsDuringRouterStartup(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	store, err := database.Open(filepath.Join(t.TempDir(), "invoice-startup.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	defer store.Close()
	clientID := uuid.NewString()
	if err := store.DB.Create(&models.Client{
		ID: clientID, Name: "启动补偿客户", Status: "active", Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("seed startup client: %v", err)
	}
	invoice := models.Invoice{
		ID: uuid.NewString(), InvoiceNumber: "INV-STARTUP-001", ClientID: clientID,
		AmountMinor: 10000, Currency: "CNY", Status: "sent",
		IssueDate: "2026-08-01", DueDate: "2026-09-01", Version: 2,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if err := store.DB.Create(&invoice).Error; err != nil {
		t.Fatalf("seed startup Invoice: %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return now },
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
		DiskSpaceScanInterval: -1, ScheduledBackupScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter() startup projection error = %v", err)
	}
	defer router.Close()
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key=?", 1, invoiceDueEventKey(invoice.ID, "due", invoice.DueDate, "2026-09-01"))
}
