package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
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
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=? AND status='resolved'", 4, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved' AND aggregate_id IN (SELECT id FROM inbox_items WHERE source_entity_type='invoice_due' AND source_entity_id=?)", 4, invoice.ID)
	for _, item := range loadInvoiceDueItemsForTest(t, store, invoice.ID) {
		if item.Status != "resolved" || item.ResolvedByActorID == nil || *item.ResolvedByActorID != models.BuiltinOwnerActorID ||
			item.ResolvedAt == nil || item.ResolutionReason == nil || *item.ResolutionReason != invoicePaidInboxResolutionReason ||
			item.ResolutionMode == nil || *item.ResolutionMode != "manual" || item.TriagedAt == nil || item.Version != 2 {
			t.Fatalf("resolved runtime Invoice due source = %#v", item)
		}
	}

	filtered := performRequest(router.Engine, http.MethodGet, "/api/v1/inbox-items?source_entity_type=invoice_due", nil, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter Invoice due Inbox = %d: %s", filtered.Code, filtered.Body.String())
	}
}

func TestInvoicePaymentResolvesOnlyActiveDueSourcesAndPreservesAutomationTask(t *testing.T) {
	now := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"付款协调客户"}`, nil)

	other := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":4300,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`, client.ID), nil)
	other = transitionInvoiceForTest(t, router.Engine, other, `{"action":"mark_sent"}`, "")
	other = transitionInvoiceForTest(t, router.Engine, other, `{"action":"mark_viewed"}`, "")
	other = transitionInvoiceForTest(t, router.Engine, other, `{"action":"mark_overdue"}`, "")

	configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	target := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":12800,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`, client.ID), nil)
	target = transitionInvoiceForTest(t, router.Engine, target, `{"action":"mark_sent"}`, "")
	target = transitionInvoiceForTest(t, router.Engine, target, `{"action":"mark_viewed"}`, "")
	target = transitionInvoiceForTest(t, router.Engine, target, `{"action":"mark_overdue"}`, "invoice-paid-source-overdue")

	var run models.AutomationRun
	if err := store.DB.Where(`source_event_id IN (
		SELECT id FROM workflow_events
		WHERE aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'
	)`, target.ID).Take(&run).Error; err != nil {
		t.Fatalf("load Invoice overdue Automation Run: %v", err)
	}
	if run.ResultID == nil || run.ResultType == nil || *run.ResultType != "task" {
		t.Fatalf("Invoice overdue Automation Run = %#v", run)
	}
	var taskBefore models.Task
	if err := store.DB.First(&taskBefore, "id = ?", *run.ResultID).Error; err != nil {
		t.Fatalf("load Invoice overdue followup Task: %v", err)
	}
	if taskBefore.Status != "todo" || taskBefore.Kind != "followup" {
		t.Fatalf("Invoice overdue followup Task = %#v", taskBefore)
	}

	for day := 2; day <= 5; day++ {
		now = time.Date(2026, 9, day, 9, 0, 0, 0, time.UTC)
		if err := service.projectDueInvoices(context.Background()); err != nil {
			t.Fatalf("project overdue Invoice day %d: %v", day, err)
		}
	}
	targetItems := loadInvoiceDueItemsForTest(t, store, target.ID)
	if len(targetItems) != 4 {
		t.Fatalf("target Invoice due source count = %d, want 4", len(targetItems))
	}
	trackingAt := "2026-09-02T10:00:00Z"
	result := store.DB.Model(&models.InboxItem{}).
		Where("id = ? AND version = ?", targetItems[0].ID, targetItems[0].Version).
		Updates(map[string]any{"status": "tracking", "triaged_at": trackingAt, "version": gorm.Expr("version + 1"), "updated_at": trackingAt})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("make Invoice due source tracking: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	resolved := performRequest(
		router.Engine, http.MethodPost, "/api/v1/inbox-items/"+targetItems[1].ID+"/resolve",
		[]byte(`{"reason":"already handled"}`), map[string]string{"If-Match": `"1"`},
	)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve terminal Invoice due source = %d: %s", resolved.Code, resolved.Body.String())
	}
	dismissed := performRequest(
		router.Engine, http.MethodPost, "/api/v1/inbox-items/"+targetItems[2].ID+"/dismiss",
		[]byte(`{"reason":"not actionable"}`), map[string]string{"If-Match": `"1"`},
	)
	if dismissed.Code != http.StatusOK {
		t.Fatalf("dismiss terminal Invoice due source = %d: %s", dismissed.Code, dismissed.Body.String())
	}

	beforePayment := loadInvoiceDueItemsForTest(t, store, target.ID)
	if beforePayment[0].Status != "tracking" || beforePayment[1].Status != "resolved" || beforePayment[2].Status != "dismissed" || beforePayment[3].Status != "open" {
		t.Fatalf("Invoice due source fixture states = %#v", beforePayment)
	}
	otherBefore := loadInvoiceDueItemsForTest(t, store, other.ID)
	if len(otherBefore) != 4 {
		t.Fatalf("other Invoice due source count = %d, want 4", len(otherBefore))
	}

	paidRequest := []byte(`{"action":"mark_paid","paid_date":"2026-09-05"}`)
	paidRecorder := performRequest(
		router.Engine, http.MethodPost, "/api/v1/invoices/"+target.ID+"/transition", paidRequest,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, target.Version), "Idempotency-Key": "invoice-paid-resolve-sources"},
	)
	if paidRecorder.Code != http.StatusOK {
		t.Fatalf("pay Invoice with due sources = %d: %s", paidRecorder.Code, paidRecorder.Body.String())
	}
	paid := decodeInvoiceResponse(t, paidRecorder.Body.Bytes())
	if paid.Status != "paid" || paid.FinancialEntryID == nil {
		t.Fatalf("paid Invoice = %#v", paid)
	}
	afterPayment := loadInvoiceDueItemsForTest(t, store, target.ID)
	for _, index := range []int{0, 3} {
		before, after := beforePayment[index], afterPayment[index]
		if after.Status != "resolved" || after.Version != before.Version+1 || after.ResolvedByActorID == nil || *after.ResolvedByActorID != models.BuiltinOwnerActorID ||
			after.ResolvedAt == nil || after.ResolutionReason == nil || *after.ResolutionReason != invoicePaidInboxResolutionReason ||
			after.ResolutionMode == nil || *after.ResolutionMode != "manual" || after.TriagedAt == nil || after.SnoozedUntil != nil {
			t.Fatalf("active Invoice due source %d after payment: before=%#v after=%#v", index, before, after)
		}
	}
	for _, index := range []int{1, 2} {
		if !reflect.DeepEqual(afterPayment[index], beforePayment[index]) {
			t.Fatalf("terminal Invoice due source %d changed: before=%#v after=%#v", index, beforePayment[index], afterPayment[index])
		}
	}
	if otherAfter := loadInvoiceDueItemsForTest(t, store, other.ID); !reflect.DeepEqual(otherAfter, otherBefore) {
		t.Fatalf("other Invoice due sources changed: before=%#v after=%#v", otherBefore, otherAfter)
	}
	assertDatabaseCount(t, store, `
		SELECT COUNT(*) FROM workflow_events
		WHERE aggregate_type='inbox_item' AND action='source_resolved'
		  AND actor_id=?
		  AND json_extract(current_json, '$.reason')=?
		  AND json_extract(current_json, '$.resolution_reason')=?
		  AND json_extract(current_json, '$.resolution_mode')='manual'
		  AND aggregate_id IN (?, ?)
	`, 2, models.BuiltinOwnerActorID, invoicePaidInboxResolutionReason, invoicePaidInboxResolutionReason, afterPayment[0].ID, afterPayment[3].ID)
	var taskAfter models.Task
	if err := store.DB.First(&taskAfter, "id = ?", taskBefore.ID).Error; err != nil {
		t.Fatalf("reload Invoice overdue followup Task: %v", err)
	}
	if !reflect.DeepEqual(taskAfter, taskBefore) {
		t.Fatalf("Invoice overdue followup Task changed on payment: before=%#v after=%#v", taskBefore, taskAfter)
	}

	replay := performRequest(
		router.Engine, http.MethodPost, "/api/v1/invoices/"+target.ID+"/transition", paidRequest,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, target.Version), "Idempotency-Key": "invoice-paid-resolve-sources"},
	)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("paid Invoice replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	if replayItems := loadInvoiceDueItemsForTest(t, store, target.ID); !reflect.DeepEqual(replayItems, afterPayment) {
		t.Fatalf("paid Invoice replay changed due sources: before=%#v after=%#v", afterPayment, replayItems)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE invoice_id=?", 1, target.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_paid'", 1, target.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved' AND aggregate_id IN (?, ?)", 2, afterPayment[0].ID, afterPayment[3].ID)
}

func TestInvoicePaymentDueSourceCoordinationFailureRollsBackAllFacts(t *testing.T) {
	now := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"付款回滚客户"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":6600,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`, client.ID), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_viewed"}`, "")
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_overdue"}`, "")
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project Invoice due source before payment rollback: %v", err)
	}
	var invoiceBefore models.Invoice
	if err := store.DB.First(&invoiceBefore, "id = ?", invoice.ID).Error; err != nil {
		t.Fatalf("load Invoice before payment rollback: %v", err)
	}
	sourceBefore := loadInvoiceDueItemsForTest(t, store, invoice.ID)
	if len(sourceBefore) != 1 || sourceBefore[0].Status != "open" {
		t.Fatalf("Invoice due source before payment rollback = %#v", sourceBefore)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_invoice_paid_due_source_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type='inbox_item' AND NEW.action='source_resolved'
		BEGIN
			SELECT RAISE(ABORT, 'FAIL_INVOICE_PAID_DUE_SOURCE_EVENT');
		END
	`).Error; err != nil {
		t.Fatalf("install Invoice paid due-source event failure: %v", err)
	}

	failed := performRequest(
		router.Engine, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition",
		[]byte(`{"action":"mark_paid","paid_date":"2026-09-02"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, invoice.Version), "Idempotency-Key": "invoice-paid-due-source-fail"},
	)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed Invoice paid due-source coordination = %d: %s", failed.Code, failed.Body.String())
	}
	var invoiceAfter models.Invoice
	if err := store.DB.First(&invoiceAfter, "id = ?", invoice.ID).Error; err != nil {
		t.Fatalf("load Invoice after payment rollback: %v", err)
	}
	if !reflect.DeepEqual(invoiceAfter, invoiceBefore) {
		t.Fatalf("Invoice changed after due-source coordination rollback: before=%#v after=%#v", invoiceBefore, invoiceAfter)
	}
	if sourceAfter := loadInvoiceDueItemsForTest(t, store, invoice.ID); !reflect.DeepEqual(sourceAfter, sourceBefore) {
		t.Fatalf("Invoice due source changed after coordination rollback: before=%#v after=%#v", sourceBefore, sourceAfter)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE invoice_id=?", 0, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='financial_entry' AND aggregate_id IN (SELECT id FROM financial_entries WHERE invoice_id=?)", 0, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_paid'", 0, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND aggregate_id=? AND action='source_resolved'", 0, sourceBefore[0].ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE key='invoice-paid-due-source-fail'", 0)
}

func loadInvoiceDueItemsForTest(t *testing.T, store *database.Store, invoiceID string) []models.InboxItem {
	t.Helper()
	var items []models.InboxItem
	if err := store.DB.Where(
		"source_entity_type = ? AND source_entity_id = ?",
		invoiceDueInboxSourceType,
		invoiceID,
	).Order("created_at ASC").Order("id ASC").Find(&items).Error; err != nil {
		t.Fatalf("load Invoice due Inbox Items: %v", err)
	}
	return items
}

func seedPaidInvoiceForDueReconciliationTest(
	t *testing.T,
	db *gorm.DB,
	clientID,
	number,
	dueDate,
	paidDate,
	now string,
	amountMinor int64,
) models.Invoice {
	t.Helper()
	invoice := models.Invoice{
		ID: uuid.NewString(), InvoiceNumber: number, ClientID: clientID,
		AmountMinor: amountMinor, Currency: "CNY", Status: "paid",
		IssueDate: "2026-08-01", DueDate: dueDate, PaidDate: &paidDate,
		Notes: "", Version: 4, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&invoice).Error; err != nil {
		t.Fatalf("seed paid Invoice %s: %v", number, err)
	}
	invoiceID := invoice.ID
	entry := models.FinancialEntry{
		ID: uuid.NewString(), Type: "income", AmountMinor: amountMinor,
		Currency: "CNY", OccurredOn: paidDate, Status: "confirmed",
		Category: invoicePaymentCategory, ClientID: &clientID, InvoiceID: &invoiceID,
		Notes: "", CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("seed paid Invoice entry %s: %v", number, err)
	}
	return invoice
}

func seedInvoiceDueSourceForReconciliationTest(
	t *testing.T,
	db *gorm.DB,
	invoice models.Invoice,
	clientName,
	occurrenceDate,
	projectedAt string,
) models.InboxItem {
	t.Helper()
	payload := invoiceDueProjectionPayload{
		InvoiceID: invoice.ID, InvoiceNumber: invoice.InvoiceNumber,
		ClientID: invoice.ClientID, ClientName: clientName,
		AmountMinor: invoice.AmountMinor, Currency: invoice.Currency,
		DueDate: invoice.DueDate, DueState: "overdue", OccurrenceDate: occurrenceDate,
		InvoiceVersion: invoice.Version, ProjectedAt: projectedAt, LeadDays: invoiceDueLeadDays,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode reconciliation Invoice due source: %v", err)
	}
	dueAt, err := invoiceDueTimestamp(invoice.DueDate, time.UTC)
	if err != nil {
		t.Fatalf("build reconciliation Invoice due timestamp: %v", err)
	}
	key := invoiceDueEventKey(invoice.ID, "overdue", invoice.DueDate, occurrenceDate)
	invoiceID := invoice.ID
	item := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: invoiceDueTitle(invoice.InvoiceNumber, "overdue"),
		Summary: "历史发票到期投影", SourceEntityType: invoiceDueInboxSourceType,
		SourceEntityID: &invoiceID, SourceEventKey: &key, Priority: "P1",
		Status: "open", ResolutionPolicy: "manual", DueAt: &dueAt,
		PayloadJSON: string(payloadJSON), Version: 1, CreatedAt: projectedAt, UpdatedAt: projectedAt,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed reconciliation Invoice due source: %v", err)
	}
	if err := recordInboxWorkflowEventAs(
		db, item.ID, "source_projected", models.BuiltinSystemActorID,
		nil, inboxItemEventState(item, ""), "", projectedAt,
	); err != nil {
		t.Fatalf("record reconciliation Invoice due source event: %v", err)
	}
	return item
}

func invoiceResponseFromModelForTest(t *testing.T, store *database.Store, id string) invoiceResponse {
	t.Helper()
	row, err := loadInvoiceRow(store.DB, id)
	if err != nil {
		t.Fatalf("load Invoice response row: %v", err)
	}
	return invoiceResponseFromRow(row)
}

func TestPaidInvoiceDueSourceReconciliationIsAtomicScopedAndIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"历史付款协调客户"}`, nil)
	nowText := formatInboxTimestamp(now.UTC())
	paid := seedPaidInvoiceForDueReconciliationTest(
		t, store.DB, client.ID, "INV-PAID-RECONCILE", "2026-09-01", "2026-09-05", nowText, 8800,
	)
	openSource := seedInvoiceDueSourceForReconciliationTest(t, store.DB, paid, client.Name, "2026-09-02", "2026-09-02T08:00:00Z")
	trackingSource := seedInvoiceDueSourceForReconciliationTest(t, store.DB, paid, client.Name, "2026-09-03", "2026-09-03T08:00:00Z")
	resolvedSource := seedInvoiceDueSourceForReconciliationTest(t, store.DB, paid, client.Name, "2026-09-04", "2026-09-04T08:00:00Z")
	dismissedSource := seedInvoiceDueSourceForReconciliationTest(t, store.DB, paid, client.Name, "2026-09-05", "2026-09-05T08:00:00Z")

	trackingAt := "2026-09-03T10:00:00Z"
	result := store.DB.Model(&models.InboxItem{}).
		Where("id = ? AND version = ?", trackingSource.ID, trackingSource.Version).
		Updates(map[string]any{"status": "tracking", "triaged_at": trackingAt, "version": gorm.Expr("version + 1"), "updated_at": trackingAt})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("make stale paid Invoice source tracking: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	resolved := performRequest(
		router.Engine, http.MethodPost, "/api/v1/inbox-items/"+resolvedSource.ID+"/resolve",
		[]byte(`{"reason":"historically resolved"}`), map[string]string{"If-Match": `"1"`},
	)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve stale paid Invoice terminal source = %d: %s", resolved.Code, resolved.Body.String())
	}
	dismissed := performRequest(
		router.Engine, http.MethodPost, "/api/v1/inbox-items/"+dismissedSource.ID+"/dismiss",
		[]byte(`{"reason":"historically dismissed"}`), map[string]string{"If-Match": `"1"`},
	)
	if dismissed.Code != http.StatusOK {
		t.Fatalf("dismiss stale paid Invoice terminal source = %d: %s", dismissed.Code, dismissed.Body.String())
	}

	unrelated := models.Invoice{
		ID: uuid.NewString(), InvoiceNumber: "INV-UNRELATED-OVERDUE", ClientID: client.ID,
		AmountMinor: 9900, Currency: "CNY", Status: "overdue",
		IssueDate: "2026-08-01", DueDate: "2026-09-01", Version: 4,
		CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&unrelated).Error; err != nil {
		t.Fatalf("seed unrelated overdue Invoice: %v", err)
	}
	seedInvoiceDueSourceForReconciliationTest(t, store.DB, unrelated, client.Name, "2026-09-06", nowText)

	beforeFailure := loadInvoiceDueItemsForTest(t, store, paid.ID)
	unrelatedBefore := loadInvoiceDueItemsForTest(t, store, unrelated.ID)
	activeIDs := []string{openSource.ID, trackingSource.ID}
	sort.Strings(activeIDs)
	if err := store.DB.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_paid_invoice_reconciliation_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type='inbox_item'
		 AND NEW.action='source_resolved'
		 AND NEW.aggregate_id='%s'
		BEGIN
			SELECT RAISE(ABORT, 'FAIL_PAID_INVOICE_RECONCILIATION_EVENT');
		END
	`, activeIDs[1])).Error; err != nil {
		t.Fatalf("install paid Invoice reconciliation failure: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err == nil {
		t.Fatal("paid Invoice due-source reconciliation unexpectedly succeeded")
	}
	if afterFailure := loadInvoiceDueItemsForTest(t, store, paid.ID); !reflect.DeepEqual(afterFailure, beforeFailure) {
		t.Fatalf("paid Invoice sources partially changed on reconciliation failure: before=%#v after=%#v", beforeFailure, afterFailure)
	}
	if unrelatedAfterFailure := loadInvoiceDueItemsForTest(t, store, unrelated.ID); !reflect.DeepEqual(unrelatedAfterFailure, unrelatedBefore) {
		t.Fatalf("unrelated Invoice changed on reconciliation failure: before=%#v after=%#v", unrelatedBefore, unrelatedAfterFailure)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved' AND aggregate_id IN (?, ?)", 0, activeIDs[0], activeIDs[1])
	if err := store.DB.Exec("DROP TRIGGER fail_paid_invoice_reconciliation_event").Error; err != nil {
		t.Fatalf("remove paid Invoice reconciliation failure: %v", err)
	}

	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("reconcile stale paid Invoice due sources: %v", err)
	}
	afterSuccess := loadInvoiceDueItemsForTest(t, store, paid.ID)
	byID := make(map[string]models.InboxItem, len(afterSuccess))
	for _, item := range afterSuccess {
		byID[item.ID] = item
	}
	for _, current := range []models.InboxItem{byID[openSource.ID], byID[trackingSource.ID]} {
		if current.Status != "resolved" || current.ResolvedByActorID == nil || *current.ResolvedByActorID != models.BuiltinOwnerActorID ||
			current.ResolutionReason == nil || *current.ResolutionReason != invoicePaidInboxResolutionReason ||
			current.ResolutionMode == nil || *current.ResolutionMode != "manual" || current.ResolvedAt == nil || current.TriagedAt == nil {
			t.Fatalf("reconciled paid Invoice due source = %#v", current)
		}
	}
	beforeByID := make(map[string]models.InboxItem, len(beforeFailure))
	for _, item := range beforeFailure {
		beforeByID[item.ID] = item
	}
	if byID[openSource.ID].Version != beforeByID[openSource.ID].Version+1 || byID[trackingSource.ID].Version != beforeByID[trackingSource.ID].Version+1 {
		t.Fatalf("reconciled source versions: before=%#v after=%#v", beforeByID, byID)
	}
	for _, terminalID := range []string{resolvedSource.ID, dismissedSource.ID} {
		if !reflect.DeepEqual(byID[terminalID], beforeByID[terminalID]) {
			t.Fatalf("terminal paid Invoice due source %s changed: before=%#v after=%#v", terminalID, beforeByID[terminalID], byID[terminalID])
		}
	}
	if unrelatedAfter := loadInvoiceDueItemsForTest(t, store, unrelated.ID); !reflect.DeepEqual(unrelatedAfter, unrelatedBefore) {
		t.Fatalf("unrelated overdue Invoice due source changed: before=%#v after=%#v", unrelatedBefore, unrelatedAfter)
	}
	assertDatabaseCount(t, store, `
		SELECT COUNT(*) FROM workflow_events
		WHERE aggregate_type='inbox_item' AND action='source_resolved'
		  AND actor_id=?
		  AND json_extract(current_json, '$.reason')=?
		  AND aggregate_id IN (?, ?)
	`, 2, models.BuiltinOwnerActorID, invoicePaidInboxResolutionReason, activeIDs[0], activeIDs[1])
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)

	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("repeat paid Invoice due-source reconciliation: %v", err)
	}
	if repeated := loadInvoiceDueItemsForTest(t, store, paid.ID); !reflect.DeepEqual(repeated, afterSuccess) {
		t.Fatalf("repeat paid Invoice reconciliation changed sources: before=%#v after=%#v", afterSuccess, repeated)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved' AND aggregate_id IN (?, ?)", 2, activeIDs[0], activeIDs[1])
}

func TestPaidInvoiceDueSourceReconciliationProcessesBoundedBatches(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	_, service, store := newInvoiceDueTestAPI(t, &now)
	clientID := uuid.NewString()
	nowText := formatInboxTimestamp(now.UTC())
	if err := store.DB.Create(&models.Client{
		ID: clientID, Name: "历史付款批量客户", Status: "active", Version: 1,
		CreatedAt: nowText, UpdatedAt: nowText,
	}).Error; err != nil {
		t.Fatalf("seed paid reconciliation batch client: %v", err)
	}
	for index := 0; index < invoiceDueReconciliationBatchSize+1; index++ {
		invoice := seedPaidInvoiceForDueReconciliationTest(
			t, store.DB, clientID, fmt.Sprintf("INV-PAID-BATCH-%03d", index),
			"2026-09-01", "2026-09-02", nowText, int64(10000+index),
		)
		seedInvoiceDueSourceForReconciliationTest(t, store.DB, invoice, "历史付款批量客户", "2026-09-03", nowText)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status IN ('open','tracking')", int64(invoiceDueReconciliationBatchSize+1))

	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("reconcile first paid Invoice batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status='resolved'", int64(invoiceDueReconciliationBatchSize))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status IN ('open','tracking')", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved'", int64(invoiceDueReconciliationBatchSize))

	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("reconcile second paid Invoice batch: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status='resolved'", int64(invoiceDueReconciliationBatchSize+1))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status IN ('open','tracking')", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved'", int64(invoiceDueReconciliationBatchSize+1))
	var versionSumBefore int64
	if err := store.DB.Table("inbox_items").Where("source_entity_type = ?", invoiceDueInboxSourceType).Select("SUM(version)").Scan(&versionSumBefore).Error; err != nil {
		t.Fatalf("read reconciled source version sum: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("repeat drained paid Invoice reconciliation: %v", err)
	}
	var versionSumAfter int64
	if err := store.DB.Table("inbox_items").Where("source_entity_type = ?", invoiceDueInboxSourceType).Select("SUM(version)").Scan(&versionSumAfter).Error; err != nil {
		t.Fatalf("read repeated source version sum: %v", err)
	}
	if versionSumAfter != versionSumBefore {
		t.Fatalf("drained paid Invoice reconciliation changed versions: before=%d after=%d", versionSumBefore, versionSumAfter)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_resolved'", int64(invoiceDueReconciliationBatchSize+1))
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
	paid := seedPaidInvoiceForDueReconciliationTest(
		t, store.DB, clientID, "INV-STARTUP-PAID", "2026-08-31", "2026-09-01", formatInboxTimestamp(now.UTC()), 12000,
	)
	stalePaidSource := seedInvoiceDueSourceForReconciliationTest(
		t, store.DB, paid, "启动补偿客户", "2026-09-01", "2026-09-01T07:00:00Z",
	)
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
	var reconciled models.InboxItem
	if err := store.DB.Where("id = ?", stalePaidSource.ID).Take(&reconciled).Error; err != nil {
		t.Fatalf("load startup-reconciled paid Invoice source: %v", err)
	}
	if reconciled.Status != "resolved" || reconciled.ResolutionReason == nil || *reconciled.ResolutionReason != invoicePaidInboxResolutionReason {
		t.Fatalf("startup paid Invoice reconciliation = %#v, want resolved with invoice_paid reason", reconciled)
	}
	assertDatabaseCount(t, store, `
		SELECT COUNT(*) FROM workflow_events
		WHERE aggregate_type = 'inbox_item'
		  AND aggregate_id = ?
		  AND action = 'source_resolved'
		  AND actor_id = ?
	`, 1, stalePaidSource.ID, models.BuiltinOwnerActorID)
}
