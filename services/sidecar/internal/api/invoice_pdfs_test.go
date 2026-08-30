package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

var invoicePDFTestNow = time.Date(2026, 8, 29, 19, 30, 0, 123456789, time.UTC)

func newInvoicePDFTestAPI(t *testing.T) (*gin.Engine, *database.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "invoice-pdf-api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pdfRoot := filepath.Join(root, "invoices")
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), InvoicePDFDir: pdfRoot,
		Now: func() time.Time { return invoicePDFTestNow },
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store, pdfRoot
}

func decodeInvoicePDFResponse(t *testing.T, body []byte) invoicePDFResponse {
	t.Helper()
	var envelope struct {
		Data invoicePDFResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode invoice PDF response: %v", err)
	}
	return envelope.Data
}

func generateInvoicePDFForTest(t *testing.T, router http.Handler, invoice invoiceResponse, key string) invoicePDFResponse {
	t.Helper()
	response := performRequest(
		router,
		http.MethodPost,
		"/api/v1/invoices/"+invoice.ID+"/generate-pdf",
		nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, invoice.Version), "Idempotency-Key": key},
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("generate invoice PDF = %d: %s", response.Code, response.Body.String())
	}
	return decodeInvoicePDFResponse(t, response.Body.Bytes())
}

func TestInvoicePDFGenerateMetadataDownloadReplaceAndReplay(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"星河设计事务所"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"品牌视觉升级","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":128045,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29","notes":"第一期设计服务费。\n请在到期日前完成付款。"}`,
		client.ID, project.ID,
	), nil)

	generated := generateInvoicePDFForTest(t, router, invoice, "invoice-pdf-generate-1")
	if generated.InvoiceID != invoice.ID || generated.FileName != "invoice-INV-2026-001.pdf" ||
		generated.MimeType != invoicePDFMimeType || generated.SizeBytes <= 0 || len(generated.SHA256) != 64 ||
		generated.GeneratedFromVersion != invoice.Version || generated.IntegrityStatus != "verified" ||
		generated.GeneratedAt != "2026-08-29T19:30:00.123456789Z" || generated.IntegrityCheckedAt != generated.GeneratedAt {
		t.Fatalf("generated invoice PDF = %#v", generated)
	}
	var stored models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&stored).Error; err != nil {
		t.Fatalf("load generated invoice PDF asset: %v", err)
	}
	firstPath := filepath.Join(pdfRoot, filepath.FromSlash(stored.RelativePath))
	firstBytes, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read generated invoice PDF: %v", err)
	}
	if !bytes.HasPrefix(firstBytes, []byte("%PDF-")) {
		t.Fatalf("generated invoice PDF header = %q", firstBytes[:min(len(firstBytes), 8)])
	}
	firstHash := sha256.Sum256(firstBytes)
	if hex.EncodeToString(firstHash[:]) != generated.SHA256 || int64(len(firstBytes)) != generated.SizeBytes {
		t.Fatalf("generated invoice PDF integrity does not match metadata")
	}

	replay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "invoice-pdf-generate-1"},
	)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || decodeInvoicePDFResponse(t, replay.Body.Bytes()) != generated {
		t.Fatalf("invoice PDF replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 1, invoice.ID)

	metadata := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", nil, nil)
	if metadata.Code != http.StatusOK || metadata.Header().Get("Cache-Control") != "no-store" || decodeInvoicePDFResponse(t, metadata.Body.Bytes()).IntegrityStatus != "verified" {
		t.Fatalf("invoice PDF metadata = %d headers=%v body=%s", metadata.Code, metadata.Header(), metadata.Body.String())
	}
	download := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusOK || download.Header().Get("Content-Type") != invoicePDFMimeType ||
		!strings.Contains(download.Header().Get("Content-Disposition"), "invoice-INV-2026-001.pdf") ||
		download.Header().Get("Cache-Control") != "no-store" || download.Header().Get("X-Content-Type-Options") != "nosniff" ||
		download.Header().Get("X-Invoice-PDF-SHA256") != generated.SHA256 || !bytes.Equal(download.Body.Bytes(), firstBytes) {
		t.Fatalf("invoice PDF download = %d headers=%v bytes=%d", download.Code, download.Header(), download.Body.Len())
	}

	current := decodeInvoiceResponse(t, performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID, nil, nil).Body.Bytes())
	if current.Status != "draft" || current.Version != 1 {
		t.Fatalf("PDF generation changed invoice facts: %#v", current)
	}
	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/invoices/"+invoice.ID, []byte(`{"notes":"更新后的本地账单备注"}`), map[string]string{"If-Match": `"1"`})
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update invoice before PDF replacement = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeInvoiceResponse(t, updatedRecorder.Body.Bytes())
	conflictingReplay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "invoice-pdf-generate-1"},
	)
	if conflictingReplay.Code != http.StatusConflict || responseErrorCode(t, conflictingReplay.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("invoice PDF idempotency conflict = %d: %s", conflictingReplay.Code, conflictingReplay.Body.String())
	}
	replaced := generateInvoicePDFForTest(t, router, updated, "invoice-pdf-generate-2")
	if replaced.GeneratedFromVersion != 2 || replaced.InvoiceID != invoice.ID {
		t.Fatalf("replacement invoice PDF = %#v", replaced)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("replaced invoice PDF left old file, err=%v", err)
	}
	stored = models.InvoicePDFAsset{}
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&stored).Error; err != nil {
		t.Fatalf("load replacement invoice PDF asset: %v", err)
	}
	if stored.GeneratedFromVersion != 2 || stored.ID == "" || filepath.Join(pdfRoot, filepath.FromSlash(stored.RelativePath)) == firstPath {
		t.Fatalf("stored replacement invoice PDF = %#v", stored)
	}
	current = decodeInvoiceResponse(t, performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID, nil, nil).Body.Bytes())
	if current.Status != "draft" || current.Version != 2 {
		t.Fatalf("PDF replacement changed invoice facts: %#v", current)
	}
	supersededReplay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "invoice-pdf-generate-1"},
	)
	if supersededReplay.Code != http.StatusConflict || responseErrorCode(t, supersededReplay.Body.Bytes()) != "IDEMPOTENCY_REPLAY_UNAVAILABLE" {
		t.Fatalf("superseded invoice PDF replay = %d: %s", supersededReplay.Code, supersededReplay.Body.String())
	}
}

func TestInvoicePDFReplacementRestoresPreviousFileWhenDatabaseWriteFails(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 替换补偿客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":12000,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	first := generateInvoicePDFForTest(t, router, invoice, "pdf-replace-compensation-1")
	var original models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&original).Error; err != nil {
		t.Fatalf("load original invoice PDF: %v", err)
	}
	originalPath := filepath.Join(pdfRoot, filepath.FromSlash(original.RelativePath))
	originalBytes, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("read original invoice PDF: %v", err)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER force_invoice_pdf_replace_failure
		BEFORE UPDATE ON invoice_pdf_assets
		BEGIN
			SELECT RAISE(ABORT, 'FORCED_INVOICE_PDF_REPLACE_FAILURE');
		END
	`).Error; err != nil {
		t.Fatalf("install invoice PDF replacement failure trigger: %v", err)
	}
	failed := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-replace-compensation-2"},
	)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced invoice PDF replacement failure = %d: %s", failed.Code, failed.Body.String())
	}
	var preserved models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&preserved).Error; err != nil {
		t.Fatalf("load preserved invoice PDF after replacement failure: %v", err)
	}
	if preserved.ID != original.ID || invoicePDFResponseFromAsset(preserved).SHA256 != first.SHA256 {
		t.Fatalf("replacement failure changed invoice PDF metadata: before=%#v after=%#v", original, preserved)
	}
	preservedBytes, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(preservedBytes, originalBytes) {
		t.Fatalf("replacement failure did not restore original invoice PDF bytes=%d err=%v", len(preservedBytes), err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE key = ?", 0, "pdf-replace-compensation-2")
}

func TestInvoicePDFFreshKeyRegeneratesSameVersionAndSupersedesOldReplay(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 同版本重生成客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":4500,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	first := generateInvoicePDFForTest(t, router, invoice, "pdf-same-version-1")
	var firstAsset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&firstAsset).Error; err != nil {
		t.Fatalf("load first same-version invoice PDF: %v", err)
	}
	firstPath := filepath.Join(pdfRoot, filepath.FromSlash(firstAsset.RelativePath))
	if err := store.DB.Model(&models.InvoicePDFAsset{}).
		Where("invoice_id = ? AND id = ?", invoice.ID, firstAsset.ID).
		Update("integrity_checked_at", "2000-01-01T00:00:00Z").Error; err != nil {
		t.Fatalf("age invoice PDF integrity timestamp: %v", err)
	}
	replay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-same-version-1"},
	)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("same-version initial replay = %d: %s", replay.Code, replay.Body.String())
	}
	replayed := decodeInvoicePDFResponse(t, replay.Body.Bytes())
	if replayed.IntegrityCheckedAt != invoicePDFTestNow.Format(time.RFC3339Nano) {
		t.Fatalf("same-version replay integrity timestamp = %q", replayed.IntegrityCheckedAt)
	}
	var replayedAsset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&replayedAsset).Error; err != nil || replayedAsset.IntegrityCheckedAt != replayed.IntegrityCheckedAt {
		t.Fatalf("persisted replay integrity = %#v err=%v", replayedAsset, err)
	}

	second := generateInvoicePDFForTest(t, router, invoice, "pdf-same-version-2")
	var secondAsset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&secondAsset).Error; err != nil {
		t.Fatalf("load regenerated same-version invoice PDF: %v", err)
	}
	if secondAsset.ID == firstAsset.ID || secondAsset.RelativePath == firstAsset.RelativePath {
		t.Fatalf("fresh Idempotency-Key reused old invoice PDF asset: first=%#v second=%#v", firstAsset, secondAsset)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("same-version regeneration left superseded PDF, err=%v", err)
	}
	if first.SHA256 != second.SHA256 {
		t.Fatalf("deterministic same-version PDF hashes differ: %s != %s", first.SHA256, second.SHA256)
	}
	oldReplay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-same-version-1"},
	)
	if oldReplay.Code != http.StatusConflict || responseErrorCode(t, oldReplay.Body.Bytes()) != "IDEMPOTENCY_REPLAY_UNAVAILABLE" {
		t.Fatalf("superseded same-version replay = %d: %s", oldReplay.Code, oldReplay.Body.String())
	}
	newReplay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-same-version-2"},
	)
	if newReplay.Code != http.StatusCreated || newReplay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("current same-version replay = %d: %s", newReplay.Code, newReplay.Body.String())
	}
}

func TestInvoicePDFPreconditionsAndStorageAvailability(t *testing.T) {
	withoutStorage, _ := newProjectTestAPI(t)
	client := createClientForTest(t, withoutStorage, `{"name":"无 PDF 存储客户"}`, nil)
	invoice := createInvoiceForTest(t, withoutStorage, fmt.Sprintf(`{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	unavailable := performRequest(withoutStorage, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-unavailable"})
	if unavailable.Code != http.StatusServiceUnavailable || responseErrorCode(t, unavailable.Body.Bytes()) != "INVOICE_PDF_STORAGE_UNAVAILABLE" {
		t.Fatalf("invoice PDF without storage = %d: %s", unavailable.Code, unavailable.Body.String())
	}

	router, _, _ := newInvoicePDFTestAPI(t)
	client = createClientForTest(t, router, `{"name":"PDF 前置条件客户"}`, nil)
	invoice = createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	tests := []struct {
		name    string
		headers map[string]string
		status  int
		code    string
	}{
		{name: "missing version", headers: map[string]string{"Idempotency-Key": "pdf-missing-version"}, status: http.StatusPreconditionRequired, code: "VERSION_REQUIRED"},
		{name: "missing idempotency", headers: map[string]string{"If-Match": `"1"`}, status: http.StatusPreconditionRequired, code: "IDEMPOTENCY_KEY_REQUIRED"},
		{name: "invalid idempotency", headers: map[string]string{"If-Match": `"1"`, "Idempotency-Key": "contains space"}, status: http.StatusBadRequest, code: "INVALID_IDEMPOTENCY_KEY"},
		{name: "stale version", headers: map[string]string{"If-Match": `"2"`, "Idempotency-Key": "pdf-stale-version"}, status: http.StatusConflict, code: "VERSION_CONFLICT"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil, testCase.headers)
			if response.Code != testCase.status || responseErrorCode(t, response.Body.Bytes()) != testCase.code {
				t.Fatalf("invoice PDF precondition = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "INVOICE_PDF_NOT_FOUND" {
		t.Fatalf("missing invoice PDF metadata = %d: %s", missing.Code, missing.Body.String())
	}
}

func TestInvoicePDFMetadataRefreshesIntegrityAndDraftDeleteCleansFile(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 完整性客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":9900,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-integrity-1")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF for integrity test: %v", err)
	}
	path := filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	if err := os.WriteFile(path, []byte("%PDF-tampered"), 0o600); err != nil {
		t.Fatalf("tamper invoice PDF: %v", err)
	}
	metadata := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", nil, nil)
	if metadata.Code != http.StatusOK || decodeInvoicePDFResponse(t, metadata.Body.Bytes()).IntegrityStatus != "mismatch" {
		t.Fatalf("tampered invoice PDF metadata = %d: %s", metadata.Code, metadata.Body.String())
	}
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil || asset.IntegrityStatus != "mismatch" {
		t.Fatalf("persisted tampered invoice PDF status = %#v err=%v", asset, err)
	}
	download := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusConflict || responseErrorCode(t, download.Body.Bytes()) != "INVOICE_PDF_INTEGRITY_MISMATCH" {
		t.Fatalf("tampered invoice PDF download = %d: %s", download.Code, download.Body.String())
	}

	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-integrity-2")
	staleReplay := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-integrity-1"},
	)
	if staleReplay.Code != http.StatusConflict || responseErrorCode(t, staleReplay.Body.Bytes()) != "IDEMPOTENCY_REPLAY_UNAVAILABLE" {
		t.Fatalf("replaced tampered invoice PDF replay = %d: %s", staleReplay.Code, staleReplay.Body.String())
	}
	asset = models.InvoicePDFAsset{}
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load regenerated invoice PDF: %v", err)
	}
	path = filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove regenerated invoice PDF: %v", err)
	}
	metadata = performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", nil, nil)
	if metadata.Code != http.StatusOK || decodeInvoicePDFResponse(t, metadata.Body.Bytes()).IntegrityStatus != "missing" {
		t.Fatalf("missing invoice PDF metadata = %d: %s", metadata.Code, metadata.Body.String())
	}
	download = performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusConflict || responseErrorCode(t, download.Body.Bytes()) != "INVOICE_PDF_FILE_MISSING" {
		t.Fatalf("missing invoice PDF download = %d: %s", download.Code, download.Body.String())
	}

	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-integrity-3")
	asset = models.InvoicePDFAsset{}
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF before delete: %v", err)
	}
	path = filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	deleted := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete draft invoice with PDF = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("draft invoice delete left PDF file, err=%v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 0, invoice.ID)
}

func TestInvoicePDFDraftDeleteAllowsMissingOwnedDirectory(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 缺失目录删除客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":7200,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-missing-owner-delete")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF before missing-directory delete: %v", err)
	}
	assetPath := filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	if err := os.Remove(assetPath); err != nil {
		t.Fatalf("remove invoice PDF before missing-directory delete: %v", err)
	}
	if err := os.Remove(filepath.Dir(assetPath)); err != nil {
		t.Fatalf("remove invoice PDF owner before delete: %v", err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete draft invoice with missing PDF directory = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id = ?", 0, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 0, invoice.ID)
}

func TestInvoicePDFDeleteRestoresFileWhenDatabaseDeleteFails(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 删除补偿客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":8800,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-delete-compensation")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF before compensated delete: %v", err)
	}
	path := filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	if err := store.DB.Exec(`
		CREATE TRIGGER force_invoice_delete_failure
		BEFORE DELETE ON invoices
		BEGIN
			SELECT RAISE(ABORT, 'FORCED_INVOICE_DELETE_FAILURE');
		END
	`).Error; err != nil {
		t.Fatalf("install invoice delete failure trigger: %v", err)
	}
	failed := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced invoice delete failure = %d: %s", failed.Code, failed.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("failed invoice delete did not restore PDF file: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id = ?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 1, invoice.ID)
}

func TestInvoicePDFConcurrentDuplicateKeyRendersOnce(t *testing.T) {
	router, store, _ := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 并发客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":5000,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)

	responses := make(chan struct {
		code     int
		replayed string
		body     []byte
	}, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			response := performRequest(router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil, map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-concurrent-key"})
			responses <- struct {
				code     int
				replayed string
				body     []byte
			}{response.Code, response.Header().Get("Idempotency-Replayed"), response.Body.Bytes()}
		}()
	}
	close(start)
	workers.Wait()
	close(responses)
	replayedCount := 0
	var first invoicePDFResponse
	for response := range responses {
		if response.code != http.StatusCreated {
			t.Fatalf("concurrent invoice PDF generation = %d: %s", response.code, response.body)
		}
		decoded := decodeInvoicePDFResponse(t, response.body)
		if first.InvoiceID == "" {
			first = decoded
		} else if decoded != first {
			t.Fatalf("concurrent invoice PDF responses differ: %#v != %#v", decoded, first)
		}
		if response.replayed == "true" {
			replayedCount++
		}
	}
	if replayedCount != 1 {
		t.Fatalf("concurrent invoice PDF replay count = %d, want 1", replayedCount)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE endpoint = ?", 1, "POST /api/v1/invoices/"+invoice.ID+"/generate-pdf")
}

func TestInvoicePDFLimitWriterEnforcesExactBoundary(t *testing.T) {
	var destination bytes.Buffer
	writer := &invoicePDFLimitWriter{writer: &destination, remaining: 4}
	if written, err := writer.Write([]byte("1234")); err != nil || written != 4 || writer.remaining != 0 || writer.written != 4 {
		t.Fatalf("boundary invoice PDF write written=%d remaining=%d total=%d err=%v", written, writer.remaining, writer.written, err)
	}
	if written, err := writer.Write([]byte("5")); err == nil || written != 0 || writer.remaining != 0 || writer.written != 4 {
		t.Fatalf("overflow invoice PDF write written=%d remaining=%d total=%d err=%v", written, writer.remaining, writer.written, err)
	}
}

func TestInvoicePDFStoreRequiresExplicitEmptyOrClaimedRootAndExclusiveLease(t *testing.T) {
	databaseRoot := t.TempDir()
	workspace, err := database.Open(filepath.Join(databaseRoot, "workspace.db"))
	if err != nil {
		t.Fatalf("open invoice PDF store workspace: %v", err)
	}
	defer workspace.Close()
	if _, err := openInvoicePDFStore(workspace.DB, "   "); err == nil || !strings.Contains(err.Error(), "root is required") {
		t.Fatalf("blank invoice PDF root error = %v", err)
	}

	unclaimed := filepath.Join(databaseRoot, "unclaimed")
	ownerDir := filepath.Join(unclaimed, "018f0000-0000-7000-8000-000000004799")
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		t.Fatalf("create unclaimed invoice PDF fixture: %v", err)
	}
	privatePath := filepath.Join(ownerDir, "private.pdf")
	if err := os.WriteFile(privatePath, []byte("private user file"), 0o600); err != nil {
		t.Fatalf("write unclaimed invoice PDF fixture: %v", err)
	}
	if _, err := openInvoicePDFStore(workspace.DB, unclaimed); err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("non-empty unclaimed invoice PDF root error = %v", err)
	}
	if contents, err := os.ReadFile(privatePath); err != nil || string(contents) != "private user file" {
		t.Fatalf("unclaimed invoice PDF root content changed: %q err=%v", contents, err)
	}
	for _, unexpected := range []string{".opc-workspace-invoice-pdf-store", ".staging", ".trash", artifactStoreLockName} {
		if _, err := os.Lstat(filepath.Join(unclaimed, unexpected)); !os.IsNotExist(err) {
			t.Fatalf("unclaimed invoice PDF root gained %s, err=%v", unexpected, err)
		}
	}

	claimed := filepath.Join(databaseRoot, "claimed")
	first, err := openInvoicePDFStore(workspace.DB, claimed)
	if err != nil {
		t.Fatalf("claim empty invoice PDF root: %v", err)
	}
	if _, err := openInvoicePDFStore(workspace.DB, claimed); err == nil || !strings.Contains(err.Error(), "already in use") {
		_ = first.close()
		t.Fatalf("second invoice PDF root lease error = %v", err)
	}
	if err := first.close(); err != nil {
		t.Fatalf("close first invoice PDF store: %v", err)
	}
	reopened, err := openInvoicePDFStore(workspace.DB, claimed)
	if err != nil {
		t.Fatalf("reopen released invoice PDF root: %v", err)
	}
	if err := reopened.close(); err != nil {
		t.Fatalf("close reopened invoice PDF store: %v", err)
	}
}

func TestInvoicePDFStoreRejectsRootBoundToAnotherWorkspace(t *testing.T) {
	root := t.TempDir()
	firstWorkspace, err := database.Open(filepath.Join(root, "first.db"))
	if err != nil {
		t.Fatalf("open first invoice PDF workspace: %v", err)
	}
	defer firstWorkspace.Close()
	secondWorkspace, err := database.Open(filepath.Join(root, "second.db"))
	if err != nil {
		t.Fatalf("open second invoice PDF workspace: %v", err)
	}
	defer secondWorkspace.Close()
	pdfRoot := filepath.Join(root, "invoices")
	first, err := openInvoicePDFStore(firstWorkspace.DB, pdfRoot)
	if err != nil {
		t.Fatalf("bind invoice PDF root to first workspace: %v", err)
	}
	if err := first.close(); err != nil {
		t.Fatalf("close first bound invoice PDF store: %v", err)
	}
	if _, err := openInvoicePDFStore(secondWorkspace.DB, pdfRoot); err == nil || !strings.Contains(err.Error(), "another workspace") {
		t.Fatalf("second workspace invoice PDF binding error = %v", err)
	}
}

func TestInvoicePDFRejectsRuntimeOwnerDirectorySymlink(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 路径安全客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":6600,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-owner-symlink-1")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF for owner symlink test: %v", err)
	}
	ownerDir := filepath.Join(pdfRoot, invoice.ID)
	assetPath := filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	if err := os.Remove(assetPath); err != nil {
		t.Fatalf("remove invoice PDF before owner symlink swap: %v", err)
	}
	if err := os.Remove(ownerDir); err != nil {
		t.Fatalf("remove invoice PDF owner directory before symlink swap: %v", err)
	}
	external := t.TempDir()
	sentinel := filepath.Join(external, "do-not-touch.txt")
	if err := os.WriteFile(sentinel, []byte("outside invoice PDF root"), 0o600); err != nil {
		t.Fatalf("write invoice PDF symlink sentinel: %v", err)
	}
	if err := os.Symlink(external, ownerDir); err != nil {
		t.Skipf("directory symlink is unavailable on this host: %v", err)
	}

	metadata := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf", nil, nil)
	if metadata.Code != http.StatusOK || decodeInvoicePDFResponse(t, metadata.Body.Bytes()).IntegrityStatus != "mismatch" {
		t.Fatalf("owner symlink invoice PDF metadata = %d: %s", metadata.Code, metadata.Body.String())
	}
	download := performRequest(router, http.MethodGet, "/api/v1/invoices/"+invoice.ID+"/pdf/download", nil, nil)
	if download.Code != http.StatusConflict || responseErrorCode(t, download.Body.Bytes()) != "INVOICE_PDF_INTEGRITY_MISMATCH" {
		t.Fatalf("owner symlink invoice PDF download = %d: %s", download.Code, download.Body.String())
	}
	regenerate := performRequest(
		router, http.MethodPost, "/api/v1/invoices/"+invoice.ID+"/generate-pdf", nil,
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "pdf-owner-symlink-2"},
	)
	if regenerate.Code != http.StatusInternalServerError || responseErrorCode(t, regenerate.Body.Bytes()) != "INVOICE_PDF_STORAGE_ERROR" {
		t.Fatalf("owner symlink invoice PDF regeneration = %d: %s", regenerate.Code, regenerate.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusInternalServerError || responseErrorCode(t, deleted.Body.Bytes()) != "INVOICE_PDF_STORAGE_ERROR" {
		t.Fatalf("owner symlink invoice delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside invoice PDF root" {
		t.Fatalf("invoice PDF symlink operation changed outside file: %q err=%v", contents, err)
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 1 || entries[0].Name() != "do-not-touch.txt" {
		t.Fatalf("invoice PDF symlink operation changed outside directory: entries=%v err=%v", entries, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id = ?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id = ?", 1, invoice.ID)
}

func TestInvoicePDFReconcileRejectsNestedTrashSymlink(t *testing.T) {
	router, store, pdfRoot := newInvoicePDFTestAPI(t)
	client := createClientForTest(t, router, `{"name":"PDF 回收站路径安全客户"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":6700,"currency":"CNY","issue_date":"2026-08-29","due_date":"2026-09-29"}`, client.ID), nil)
	_ = generateInvoicePDFForTest(t, router, invoice, "pdf-trash-symlink-1")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id = ?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatalf("load invoice PDF for trash symlink test: %v", err)
	}
	livePath := filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))
	contents, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("read invoice PDF before trash symlink test: %v", err)
	}
	if err := os.Remove(livePath); err != nil {
		t.Fatalf("remove live invoice PDF before trash symlink test: %v", err)
	}
	if err := os.Remove(filepath.Dir(livePath)); err != nil {
		t.Fatalf("remove live invoice PDF owner before trash symlink test: %v", err)
	}
	external := t.TempDir()
	externalPDF := filepath.Join(external, asset.ID+".pdf")
	if err := os.WriteFile(externalPDF, contents, 0o600); err != nil {
		t.Fatalf("write external trash symlink PDF: %v", err)
	}
	trashOwner := filepath.Join(pdfRoot, ".trash", invoice.ID)
	if err := os.Symlink(external, trashOwner); err != nil {
		t.Skipf("directory symlink is unavailable on this host: %v", err)
	}
	controlled := &invoicePDFStore{
		root: pdfRoot, stagingDir: filepath.Join(pdfRoot, ".staging"), trashDir: filepath.Join(pdfRoot, ".trash"),
	}
	if err := controlled.reconcile(store.DB); err == nil {
		t.Fatal("invoice PDF reconcile accepted a nested trash symlink")
	}
	after, err := os.ReadFile(externalPDF)
	if err != nil || !bytes.Equal(after, contents) {
		t.Fatalf("invoice PDF reconcile changed external file bytes=%d err=%v", len(after), err)
	}
	if _, err := os.Lstat(livePath); !os.IsNotExist(err) {
		t.Fatalf("invoice PDF reconcile restored through unsafe trash path, err=%v", err)
	}
}

func TestEnsureInvoicePDFOwnerDirectorySyncsParentAfterCreation(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "018f0000-0000-7000-8000-000000004751")
	syncCalls := 0
	syncDirectory := func(path string) error {
		syncCalls++
		if !sameFilesystemPath(path, root) {
			return fmt.Errorf("synced path %q, want %q", path, root)
		}
		if info, err := os.Stat(owner); err != nil || !info.IsDir() {
			return fmt.Errorf("owner was not created before parent sync: info=%v err=%v", info, err)
		}
		return nil
	}
	if err := ensureInvoicePDFOwnerDirectory(root, owner, syncDirectory); err != nil {
		t.Fatalf("ensure invoice PDF owner: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("parent sync calls=%d, want 1", syncCalls)
	}
	if err := ensureInvoicePDFOwnerDirectory(root, owner, syncDirectory); err != nil {
		t.Fatalf("recheck existing invoice PDF owner: %v", err)
	}
	if syncCalls != 1 {
		t.Fatalf("existing owner unexpectedly resynced parent: calls=%d", syncCalls)
	}
}

func TestEnsureInvoicePDFOwnerDirectoryCompensatesParentSyncFailure(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "018f0000-0000-7000-8000-000000004752")
	syncCalls := 0
	forced := fmt.Errorf("forced invoice PDF parent sync failure")
	syncDirectory := func(path string) error {
		if !sameFilesystemPath(path, root) {
			return fmt.Errorf("synced path %q, want %q", path, root)
		}
		syncCalls++
		if syncCalls == 1 {
			return forced
		}
		return nil
	}
	if err := ensureInvoicePDFOwnerDirectory(root, owner, syncDirectory); err == nil || !strings.Contains(err.Error(), forced.Error()) {
		t.Fatalf("ensure owner sync failure error=%v", err)
	}
	if _, err := os.Lstat(owner); !os.IsNotExist(err) {
		t.Fatalf("failed owner creation was not compensated: %v", err)
	}
	if syncCalls != 2 {
		t.Fatalf("parent sync calls=%d, want failure plus compensation sync", syncCalls)
	}
}
