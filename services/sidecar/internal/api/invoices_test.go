package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeInvoiceResponse(t *testing.T, body []byte) invoiceResponse {
	t.Helper()
	var envelope struct {
		Data invoiceResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode invoice response: %v", err)
	}
	return envelope.Data
}

func createInvoiceForTest(t *testing.T, router http.Handler, body string, headers map[string]string) invoiceResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/invoices", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create invoice = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeInvoiceResponse(t, recorder.Body.Bytes())
}

func transitionInvoiceForTest(t *testing.T, router http.Handler, invoice invoiceResponse, body string, key string) invoiceResponse {
	t.Helper()
	headers := map[string]string{"If-Match": fmt.Sprintf("\"%d\"", invoice.Version)}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	recorder := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition", []byte(body), headers)
	if recorder.Code != http.StatusOK {
		t.Fatalf("transition invoice = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeInvoiceResponse(t, recorder.Body.Bytes())
}

func TestInvoiceCRUDNumberingFilteringAndDraftDelete(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clientA := createClientForTest(t, router, `{"name":"发票客户 A"}`, nil)
	clientB := createClientForTest(t, router, `{"name":"发票客户 B"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"发票项目","client_id":%q}`, clientA.ID), nil)
	body := fmt.Sprintf(`{"client_id":%q,"project_id":%q,"amount_minor":12800,"currency":"cny","issue_date":"2026-08-01","due_date":"2026-08-31","notes":"首期"}`, clientA.ID, project.ID)
	headers := map[string]string{"Idempotency-Key": "invoice-create-1"}
	created := createInvoiceForTest(t, router, body, headers)
	if created.InvoiceNumber != "INV-2026-001" || created.ClientID != clientA.ID || created.ClientName != clientA.Name || created.ProjectID == nil || *created.ProjectID != project.ID || created.ProjectName == nil || *created.ProjectName != project.Name || created.Currency != "CNY" || created.Status != "draft" || created.Version != 1 || created.FinancialEntryID != nil {
		t.Fatalf("created invoice = %#v", created)
	}
	currentProjectRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	currentProject := decodeProjectResponse(t, currentProjectRecorder.Body.Bytes())
	for _, body := range []string{fmt.Sprintf(`{"client_id":%q}`, clientB.ID), `{"client_id":null}`} {
		blocked := performRequest(
			router,
			http.MethodPatch,
			"/api/v1/projects/"+project.ID,
			[]byte(body),
			map[string]string{"If-Match": fmt.Sprintf("\"%d\"", currentProject.Version)},
		)
		if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES" {
			t.Fatalf("invoice project client mutation body=%s = %d: %s", body, blocked.Code, blocked.Body.String())
		}
	}
	guardedProject := decodeProjectResponse(t, performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil).Body.Bytes())
	if guardedProject.ClientID == nil || *guardedProject.ClientID != clientA.ID || guardedProject.Version != currentProject.Version {
		t.Fatalf("guarded invoice project = %#v, before=%#v", guardedProject, currentProject)
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/invoices", []byte(body), headers)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || decodeInvoiceResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("create replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/invoices", []byte(strings.Replace(body, "12800", "12801", 1)), headers)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("create idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	second := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":5000,"currency":"USD","issue_date":"2026-08-02","due_date":"2026-09-02"}`, clientB.ID), nil)
	if second.InvoiceNumber != "INV-2026-002" {
		t.Fatalf("second invoice number = %q", second.InvoiceNumber)
	}
	importedID := uuid.NewString()
	if err := store.DB.Exec(`
		INSERT INTO invoices(id, invoice_number, client_id, amount_minor, currency, status, issue_date, due_date, version, created_at, updated_at)
		VALUES(?, 'INV-2027-042', ?, 1, 'CNY', 'draft', '2027-01-01', '2027-01-31', 1, '2027-01-01T00:00:00Z', '2027-01-01T00:00:00Z')
	`, importedID, clientA.ID).Error; err != nil {
		t.Fatalf("insert imported invoice number: %v", err)
	}
	afterImport := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":6000,"currency":"CNY","issue_date":"2027-02-01","due_date":"2027-02-28"}`, clientA.ID), nil)
	if afterImport.InvoiceNumber != "INV-2027-043" {
		t.Fatalf("post-import invoice number = %q", afterImport.InvoiceNumber)
	}

	filtered := performRequest(router, http.MethodGet, "/api/v1/invoices?q=%E5%8F%91%E7%A5%A8%E5%AE%A2%E6%88%B7+A&status=draft&currency=CNY&issue_from=2026-08-01&issue_to=2026-08-31&sort=-amount_minor", nil, nil)
	var list struct {
		Data []invoiceResponse `json:"data"`
		Meta pageMeta          `json:"meta"`
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &list); err != nil || filtered.Code != http.StatusOK || list.Meta.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != created.ID {
		t.Fatalf("filtered invoice list = %d %s err=%v", filtered.Code, filtered.Body.String(), err)
	}
	invalidRange := performRequest(router, http.MethodGet, "/api/v1/invoices?due_from=2026-09-01&due_to=2026-08-01", nil, nil)
	if invalidRange.Code != http.StatusBadRequest || responseErrorCode(t, invalidRange.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid invoice range = %d: %s", invalidRange.Code, invalidRange.Body.String())
	}

	stale := performRequest(router, http.MethodPatch, "/api/v1/invoices/"+created.ID, []byte(`{"notes":"stale"}`), map[string]string{"If-Match": `"2"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale invoice update = %d: %s", stale.Code, stale.Body.String())
	}
	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/invoices/"+created.ID, []byte(`{"amount_minor":13000,"notes":"updated"}`), map[string]string{"If-Match": `"1"`})
	updated := decodeInvoiceResponse(t, updatedRecorder.Body.Bytes())
	if updatedRecorder.Code != http.StatusOK || updated.AmountMinor != 13000 || updated.Notes != "updated" || updated.Version != 2 {
		t.Fatalf("updated invoice = %d %#v", updatedRecorder.Code, updated)
	}

	missingConfirm := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+second.ID, nil, map[string]string{"If-Match": `"1"`})
	if missingConfirm.Code != http.StatusUnprocessableEntity || responseErrorCode(t, missingConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("invoice delete without confirm = %d: %s", missingConfirm.Code, missingConfirm.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+second.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
	var deletedEnvelope struct {
		Data struct {
			DeletedID string `json:"deleted_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletedEnvelope); err != nil || deleted.Code != http.StatusOK || deletedEnvelope.Data.DeletedID != second.ID {
		t.Fatalf("deleted invoice = %d %s err=%v", deleted.Code, deleted.Body.String(), err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=? AND action='invoice_deleted'", 1, second.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_number_sequences", 2)
}

func TestInvoiceConcurrentCreateGeneratesUniqueNumbers(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"并发编号客户"}`, nil)
	body := []byte(fmt.Sprintf(`{"client_id":%q,"amount_minor":8800,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID))

	type result struct {
		code int
		body []byte
	}
	const workers = 6
	start := make(chan struct{})
	results := make(chan result, workers)
	for index := range workers {
		index := index
		go func() {
			<-start
			response := performRequest(
				router,
				http.MethodPost,
				"/api/v1/invoices",
				body,
				map[string]string{"Idempotency-Key": fmt.Sprintf("invoice-concurrent-%d", index)},
			)
			results <- result{code: response.Code, body: response.Body.Bytes()}
		}()
	}
	close(start)

	numbers := make(map[string]struct{}, workers)
	ids := make(map[string]struct{}, workers)
	for range workers {
		created := <-results
		if created.code != http.StatusCreated {
			t.Fatalf("concurrent invoice create = %d: %s", created.code, created.body)
		}
		invoice := decodeInvoiceResponse(t, created.body)
		if !strings.HasPrefix(invoice.InvoiceNumber, "INV-2026-") {
			t.Fatalf("concurrent invoice number = %q", invoice.InvoiceNumber)
		}
		if _, exists := numbers[invoice.InvoiceNumber]; exists {
			t.Fatalf("duplicate concurrent invoice number = %q", invoice.InvoiceNumber)
		}
		if _, exists := ids[invoice.ID]; exists {
			t.Fatalf("duplicate concurrent invoice id = %q", invoice.ID)
		}
		numbers[invoice.InvoiceNumber] = struct{}{}
		ids[invoice.ID] = struct{}{}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE issue_date='2026-08-29'", workers)
	assertDatabaseCount(t, store, "SELECT last_value FROM invoice_number_sequences WHERE year=2026", workers)
}

func TestInvoiceTransitionsPaidEntryAuditIdempotencyAndLedgerProtection(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"付款客户"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"付款项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"project_id":%q,"amount_minor":32000,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-08-15"}`, client.ID, project.ID), nil)

	directPaid := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition", []byte(`{"action":"mark_paid","paid_date":"2026-08-20"}`), map[string]string{"If-Match": `"1"`})
	if directPaid.Code != http.StatusConflict || responseErrorCode(t, directPaid.Body.Bytes()) != "INVALID_INVOICE_TRANSITION" {
		t.Fatalf("direct paid transition = %d: %s", directPaid.Code, directPaid.Body.String())
	}
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "invoice-sent-1")
	if invoice.Status != "sent" || invoice.Version != 2 {
		t.Fatalf("sent invoice = %#v", invoice)
	}
	replaySent := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition", []byte(`{"action":"mark_sent"}`), map[string]string{"If-Match": `"1"`, "Idempotency-Key": "invoice-sent-1"})
	if replaySent.Code != http.StatusOK || replaySent.Header().Get("Idempotency-Replayed") != "true" || decodeInvoiceResponse(t, replaySent.Body.Bytes()).Version != 2 {
		t.Fatalf("sent replay = %d headers=%v body=%s", replaySent.Code, replaySent.Header(), replaySent.Body.String())
	}
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
	if invoice.Status != "viewed" || invoice.Version != 3 {
		t.Fatalf("viewed invoice = %#v", invoice)
	}
	paidRequest := `{"action":"mark_paid","paid_date":"2026-08-20"}`
	invoice = transitionInvoiceForTest(t, router, invoice, paidRequest, "invoice-paid-1")
	if invoice.Status != "paid" || invoice.PaidDate == nil || *invoice.PaidDate != "2026-08-20" || invoice.FinancialEntryID == nil || invoice.Version != 4 {
		t.Fatalf("paid invoice = %#v", invoice)
	}
	paidEntryID := *invoice.FinancialEntryID
	var entry models.FinancialEntry
	if err := store.DB.Where("id = ?", paidEntryID).Take(&entry).Error; err != nil {
		t.Fatalf("load invoice payment entry: %v", err)
	}
	if entry.Type != "income" || entry.Status != "confirmed" || entry.AmountMinor != 32000 || entry.Currency != "CNY" || entry.OccurredOn != "2026-08-20" || entry.Category != invoicePaymentCategory || entry.InvoiceID == nil || *entry.InvoiceID != invoice.ID || entry.ClientID == nil || *entry.ClientID != client.ID || entry.ProjectID == nil || *entry.ProjectID != project.ID {
		t.Fatalf("invoice payment entry = %#v", entry)
	}
	replayPaid := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition", []byte(paidRequest), map[string]string{"If-Match": `"3"`, "Idempotency-Key": "invoice-paid-1"})
	replayedInvoice := decodeInvoiceResponse(t, replayPaid.Body.Bytes())
	if replayPaid.Code != http.StatusOK || replayPaid.Header().Get("Idempotency-Replayed") != "true" || replayedInvoice.FinancialEntryID == nil || *replayedInvoice.FinancialEntryID != paidEntryID {
		t.Fatalf("paid replay = %d headers=%v body=%s", replayPaid.Code, replayPaid.Header(), replayPaid.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE invoice_id=?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='invoice' AND aggregate_id=?", 4, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='financial_entry' AND aggregate_id=? AND action='financial_entry_created'", 1, paidEntryID)

	editEntry := performRequest(router, http.MethodPatch, "/api/v1/financial-entries/"+paidEntryID, []byte(`{"amount_minor":1}`), map[string]string{"If-Match": `"1"`})
	if editEntry.Code != http.StatusConflict || responseErrorCode(t, editEntry.Body.Bytes()) != "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE" {
		t.Fatalf("edit invoice-linked entry = %d: %s", editEntry.Code, editEntry.Body.String())
	}
	voidEntry := performRequest(router, http.MethodDelete, "/api/v1/financial-entries/"+paidEntryID+"?confirm=true", []byte(`{"reason":"not allowed"}`), map[string]string{"If-Match": `"1"`})
	if voidEntry.Code != http.StatusConflict || responseErrorCode(t, voidEntry.Body.Bytes()) != "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE" {
		t.Fatalf("void invoice-linked entry = %d: %s", voidEntry.Code, voidEntry.Body.String())
	}
	deletePaid := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"4"`})
	if deletePaid.Code != http.StatusConflict || responseErrorCode(t, deletePaid.Body.Bytes()) != "INVOICE_NOT_DRAFT" {
		t.Fatalf("delete paid invoice = %d: %s", deletePaid.Code, deletePaid.Body.String())
	}
}

func TestInvoiceOverdueAndPaidTransactionRollback(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"逾期客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":9000,"currency":"CNY","issue_date":"2020-01-01","due_date":"2020-01-31"}`, client.ID), nil)
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_overdue"}`, "")
	if invoice.Status != "overdue" || invoice.Version != 4 {
		t.Fatalf("overdue invoice = %#v", invoice)
	}

	if err := store.DB.Exec(`
		CREATE TRIGGER fail_invoice_paid_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type='invoice' AND NEW.action='invoice_paid'
		BEGIN
			SELECT RAISE(ABORT, 'FAIL_INVOICE_PAID_EVENT');
		END
	`).Error; err != nil {
		t.Fatalf("install paid event failure trigger: %v", err)
	}
	failed := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/transition", []byte(`{"action":"mark_paid","paid_date":"2020-02-01"}`), map[string]string{"If-Match": `"4"`, "Idempotency-Key": "invoice-paid-fail"})
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed paid transaction = %d: %s", failed.Code, failed.Body.String())
	}
	var persisted models.Invoice
	if err := store.DB.Where("id = ?", invoice.ID).Take(&persisted).Error; err != nil {
		t.Fatalf("load invoice after rollback: %v", err)
	}
	if persisted.Status != "overdue" || persisted.Version != 4 || persisted.PaidDate != nil {
		t.Fatalf("invoice after rollback = %#v", persisted)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE invoice_id=?", 0, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE key='invoice-paid-fail'", 0)
}
