package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIInvoicePDFProposal(t *testing.T) {
	router, store, _, tool, _ := aiInvoiceDeleteFixture(t)
	client := createClientForTest(t, router.Engine, `{"name":"PDF approval customer"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30","notes":"human-only notes"}`, client.ID), nil)
	proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"invoice.generate_pdf","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID))
	args := fmt.Sprintf(`{"action":"invoice.generate_pdf","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID)
	for _, bad := range []string{
		strings.Replace(args, `"changes":{}`, `"changes":{"confirm_invoice_pdf":true}`, 1),
		strings.Replace(args, `"changes":{}`, `"changes":{},"confirm_invoice_pdf":true`, 1),
		strings.Replace(args, `"changes":{}`, `"changes":{"notes":"new"}`, 1),
	} {
		if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatal("accepted model override", bad)
		}
	}
}

func TestInvoicePDFGenerationReconcileBoundary(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			router, store, service, _, _ := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"PDF recovery"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
			generateInvoicePDFForTest(t, router.Engine, invoice, "old")
			var old models.InvoicePDFAsset
			if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&old).Error; err != nil {
				t.Fatal(err)
			}
			files := service.invoicePDFStore
			files.mu.Lock()
			defer files.mu.Unlock()
			var change *invoicePDFChange
			rollback := errors.New("simulate interruption before DB commit")
			err := store.DB.Transaction(func(tx *gorm.DB) error {
				row, err := loadInvoiceRow(tx, invoice.ID)
				if err != nil {
					return err
				}
				change, err = stageInvoicePDF(files, row, service.options.Now())
				if err != nil {
					return err
				}
				if err := replaceInvoicePDFInTransaction(tx, files, change); err != nil {
					return err
				}
				if !committed {
					return rollback
				}
				return nil
			})
			if (committed && err != nil) || (!committed && !errors.Is(err, rollback)) {
				t.Fatal(err)
			}
			// Omit normal finish to exercise durable boundaries, not a real crash.
			if err := files.reconcile(store.DB); err != nil {
				t.Fatal(err)
			}
			live, removed := old, change.asset
			if committed {
				live, removed = change.asset, old
			}
			if status, err := files.verify(live.RelativePath, live.ID, live.SizeBytes, live.SHA256); err != nil || status != "verified" {
				t.Fatal(status, err)
			}
			if _, err := os.Stat(filepath.Join(files.root, filepath.FromSlash(removed.RelativePath))); !os.IsNotExist(err) {
				t.Fatal("orphan live file", err)
			}
			if _, err := os.Stat(change.movedOld.trashPath); !os.IsNotExist(err) {
				t.Fatal("trash retained", err)
			}
		})
	}
}

func TestInvoicePDFExactDownloadMissingAndCorrupt(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			router, store, service, _, _ := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"Integrity"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
			generateInvoicePDFForTest(t, router.Engine, invoice, "integrity")
			var asset models.InvoicePDFAsset
			if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&asset).Error; err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(service.invoicePDFStore.root, filepath.FromSlash(asset.RelativePath))
			code := "INVOICE_PDF_INTEGRITY_MISMATCH"
			if missing {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				code = "INVOICE_PDF_FILE_MISSING"
			} else {
				if err := os.WriteFile(path, []byte("%PDF-corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := performRequest(router.Engine, "GET", "/api/v1/invoices/"+invoice.ID+"/pdf/download?asset_id="+asset.ID+"&sha256="+asset.SHA256, nil, nil)
			assertAPIError(t, r, 409, code)
			if strings.Contains(r.Header().Get("Content-Type"), "application/pdf") {
				t.Fatal("bad file served")
			}
		})
	}
}

func TestAIInvoicePDFLifecycle(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			router, store, service, tool, generation := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"PDF private customer"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30","notes":"private PDF notes"}`, client.ID), nil)
			var old models.InvoicePDFAsset
			var oldBytes []byte
			if existing {
				generateInvoicePDFForTest(t, router.Engine, invoice, "old-pdf")
				if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&old).Error; err != nil {
					t.Fatal(err)
				}
				var err error
				oldBytes, err = os.ReadFile(filepath.Join(service.invoicePDFStore.root, filepath.FromSlash(old.RelativePath)))
				if err != nil {
					t.Fatal(err)
				}
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"invoice.generate_pdf","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID))
			if !strings.Contains(row.PreviewJSON, "private PDF notes") || !strings.Contains(row.PreviewJSON, fmt.Sprintf(`"pdf_replaced":%t`, existing)) || strings.Contains(row.PreviewJSON, "relative_path") {
				t.Fatal(row.PreviewJSON)
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true,"confirm_invoice_pdf":true}`, row.Fingerprint))
			assertAPIError(t, performRequest(router.Engine, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			for _, flag := range []string{"", `,"confirm_invoice_pdf":false`} {
				assertAPIError(t, performRequest(router.Engine, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true%s}`, row.Fingerprint, flag)), nil), 422, "INVOICE_PDF_CONFIRMATION_REQUIRED")
			}
			assertAPIError(t, performRequest(router.Engine, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_pdf":true}`, row.Fingerprint)), nil), 422, "INVOICE_ACTION_CONFIRMATION_REQUIRED")
			assertAPIError(t, performRequest(router.Engine, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_invoice_pdf":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
			// Deliberate failure after generation, asset replacement and receipt save.
			if err := store.DB.Exec("CREATE TRIGGER fail_pdf_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router.Engine, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE endpoint=?", 0, aiInvoicePDFReceiptEndpoint)
			files, err := filepath.Glob(filepath.Join(service.invoicePDFStore.root, invoice.ID, "*.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			if existing {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE id=?", 1, old.ID)
				data, err := os.ReadFile(filepath.Join(service.invoicePDFStore.root, filepath.FromSlash(old.RelativePath)))
				if err != nil || string(data) != string(oldBytes) || len(files) != 1 {
					t.Fatal("original PDF not restored", err, files)
				}
			} else if len(files) != 0 {
				t.Fatal("failed generation left a live file", files)
			}
			if err := store.DB.Exec("DROP TRIGGER fail_pdf_approval").Error; err != nil {
				t.Fatal(err)
			}
			responses := make(chan *httptest.ResponseRecorder, 2)
			for i := 0; i < 2; i++ {
				go func() { responses <- performRequest(router.Engine, "POST", path, body, nil) }()
			}
			var first string
			var result struct {
				Data aiActionResponse `json:"data"`
			}
			for i := 0; i < 2; i++ {
				r := <-responses
				if r.Code != 200 {
					t.Fatal(r.Code, r.Body.String())
				}
				if i == 0 {
					first = r.Body.String()
				} else if first != r.Body.String() {
					t.Fatal("replay changed historical receipt")
				}
				if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
			}
			p := result.Data.InvoicePDFResult
			if p == nil || p.AssetID == old.ID || p.AssetID != *result.Data.ResultID || p.InvoiceID != invoice.ID || p.GeneratedFromVersion != 1 || result.Data.Route != "/invoices/"+invoice.ID {
				t.Fatal(first)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE endpoint=?", 1, aiInvoicePDFReceiptEndpoint)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id=? AND version=1 AND status='draft'", 1, invoice.ID)
			download := "/api/v1/invoices/" + invoice.ID + "/pdf/download?asset_id=" + p.AssetID + "&sha256=" + p.SHA256
			if r := performRequest(router.Engine, "GET", download, nil, nil); r.Code != 200 || !strings.HasPrefix(r.Body.String(), "%PDF-") || int64(r.Body.Len()) != p.SizeBytes {
				t.Fatal(r.Code, r.Body.String())
			}
			receipts, err := service.aiActionReceipts(context.Background(), generation.SessionID)
			if err != nil || !strings.Contains(receipts, `"action":"invoice.generate_pdf"`) || !strings.Contains(receipts, p.AssetID) || strings.Contains(receipts, p.SHA256) || strings.Contains(receipts, p.FileName) || strings.Contains(receipts, "private PDF notes") || strings.Contains(receipts, client.Name) {
				t.Fatal(receipts, err)
			}
			generateInvoicePDFForTest(t, router.Engine, invoice, "replacement")
			assertAPIError(t, performRequest(router.Engine, "GET", download, nil, nil), 409, "INVOICE_PDF_CHANGED")
			if r := performRequest(router.Engine, "POST", path, body, nil); r.Code != 200 || r.Body.String() != first {
				t.Fatal("replacement changed historical result", r.Code, r.Body.String())
			}
			if r := performRequest(router.Engine, "DELETE", "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`}); r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			assertAPIError(t, performRequest(router.Engine, "GET", download, nil, nil), 404, "INVOICE_NOT_FOUND")
			if r := performRequest(router.Engine, "POST", path, body, nil); r.Code != 200 || r.Body.String() != first {
				t.Fatal("deletion rewrote historical result", r.Code, r.Body.String())
			}
		})
	}
}

func TestAIInvoicePDFGuardrails(t *testing.T) {
	for _, scenario := range []string{"pdf_changed", "client_changed", "version", "no_store", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, service, tool, generation := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"Guarded PDF"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
			args := fmt.Sprintf(`{"action":"invoice.generate_pdf","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID)
			for _, scopes := range [][]string{{"finance"}, {"work", "actions"}, {"finance", "finance_actions"}} {
				tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
				if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err == nil {
					t.Fatal("scope bypass", scopes)
				}
			}
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
			if scenario == "no_store" {
				service.invoicePDFStore = nil
				if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "INVOICE_PDF_STORAGE_UNAVAILABLE") {
					t.Fatal(err)
				}
				return
			}
			row := proposeTestAction(t, store, tool, args)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			code := "AI_ACTION_PREVIEW_CHANGED"
			switch scenario {
			case "pdf_changed":
				generateInvoicePDFForTest(t, router.Engine, invoice, "changed")
			case "client_changed":
				if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Update("name", "Another customer name").Error; err != nil {
					t.Fatal(err)
				}
			case "version":
				transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
				code = "VERSION_CONFLICT"
			}
			body := fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_pdf":true,"confirm_invoice_effects":true}`, row.Fingerprint)
			if scenario == "rejected" {
				body = fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)
			}
			r := performRequest(router.Engine, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(body), nil)
			if scenario == "rejected" {
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			} else {
				assertAPIError(t, r, 409, code)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE endpoint=?", 0, aiInvoicePDFReceiptEndpoint)
		})
	}
}

func TestInvoicePDFExactDownloadParameters(t *testing.T) {
	router, store, _, _, _ := aiInvoiceDeleteFixture(t)
	client := createClientForTest(t, router.Engine, `{"name":"PDF download"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
	generateInvoicePDFForTest(t, router.Engine, invoice, "download")
	var asset models.InvoicePDFAsset
	if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&asset).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/invoices/" + invoice.ID + "/pdf/download"
	valid := "asset_id=" + asset.ID + "&sha256=" + asset.SHA256
	for _, q := range []string{"asset_id=" + asset.ID, "sha256=" + asset.SHA256, valid + "&extra=1", valid + "&asset_id=" + asset.ID, valid + "&sha256=" + asset.SHA256, "asset_id=x&sha256=" + asset.SHA256, "asset_id=" + asset.ID + "&sha256=x", valid + "&bad=%ZZ"} {
		assertAPIError(t, performRequest(router.Engine, "GET", path+"?"+q, nil, nil), 422, "INVALID_INVOICE_PDF_LOCATION")
	}
	assertAPIError(t, performRequest(router.Engine, "GET", path+"?asset_id="+asset.ID+"&sha256="+strings.Repeat("a", 64), nil, nil), 409, "INVOICE_PDF_CHANGED")
	if r := performRequest(router.Engine, "GET", path, nil, nil); r.Code != 200 {
		t.Fatal("native download regressed", r.Code, r.Body.String())
	}
}
