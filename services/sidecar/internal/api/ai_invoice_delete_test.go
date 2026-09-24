package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiInvoiceDeleteFixture(t *testing.T) (*Router, *database.Store, *API, harness.Tool, models.AIGeneration) {
	t.Helper()
	_, store, service, tool, generation := aiActionTestFixture(t)
	options := service.options
	options.SessionToken = testToken
	options.AllowedOrigins = []string{"tauri://localhost"}
	options.Logger = log.New(io.Discard, "", 0)
	options.InvoicePDFDir = filepath.Join(t.TempDir(), "invoices")
	router, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	service.invoicePDFStore = router.invoicePDFStore
	// Startup recovery closes the earlier fixture run. This one starts afterwards.
	generation.ID = uuid.NewString()
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
	return router, store, service, tool, generation
}

func TestAIInvoiceDeleteFileLifecycle(t *testing.T) {
	for _, hasPDF := range []bool{false, true} {
		t.Run(fmt.Sprint(hasPDF), func(t *testing.T) {
			router, store, service, tool, generation := aiInvoiceDeleteFixture(t)
			pdfRoot := service.invoicePDFStore.root
			var err error
			client := createClientForTest(t, router.Engine, `{"name":"Private deletion customer"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30","notes":"private delete notes"}`, client.ID), nil)
			var asset models.InvoicePDFAsset
			var original []byte
			if hasPDF {
				generateInvoicePDFForTest(t, router.Engine, invoice, "deletion-pdf")
				if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&asset).Error; err != nil {
					t.Fatal(err)
				}
				original, err = os.ReadFile(filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath)))
				if err != nil {
					t.Fatal(err)
				}
			}
			args := fmt.Sprintf(`{"action":"invoice.delete","invoice_id":%q,"expected_version":%d,"changes":{}}`, invoice.ID, invoice.Version)
			row := proposeTestAction(t, store, tool, args)
			if !strings.Contains(row.PreviewJSON, "private delete notes") || strings.Contains(row.PreviewJSON, "relative_path") {
				t.Fatal("incomplete or unsafe preview", row.PreviewJSON)
			}
			var preview aiActionPreview
			if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.After["invoice_deleted"] != true || preview.After["pdf_removed"] != hasPDF {
				t.Fatal(preview)
			}
			if hasPDF && preview.Before["pdf_asset_id"] != asset.ID {
				t.Fatal("PDF identity not bound")
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true,"confirm_invoice_delete":true}`, row.Fingerprint))
			assertAPIError(t, performRequest(router.Engine, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			for _, flag := range []string{"", `,"confirm_invoice_delete":false`} {
				assertAPIError(t, performRequest(router.Engine, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true%s}`, row.Fingerprint, flag)), nil), 422, "INVOICE_DELETE_CONFIRMATION_REQUIRED")
			}
			assertAPIError(t, performRequest(router.Engine, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_invoice_delete":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
			// Failure AFTER file movement and invoice deletion must roll back both
			// domain/audit data and the file, leaving the same proposal retryable.
			if err := store.DB.Exec("CREATE TRIGGER fail_delete_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router.Engine, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id=?", 1, invoice.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='invoice_deleted'", 0)
			if hasPDF {
				data, err := os.ReadFile(filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath)))
				if err != nil || string(data) != string(original) {
					t.Fatal("PDF compensation failed", err)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE id=?", 1, asset.ID)
			}
			if err := store.DB.Exec("DROP TRIGGER fail_delete_approval").Error; err != nil {
				t.Fatal(err)
			}
			responses := make(chan *httptest.ResponseRecorder, 2)
			start := make(chan struct{})
			for i := 0; i < 2; i++ {
				go func() { <-start; responses <- performRequest(router.Engine, "POST", path, body, nil) }()
			}
			close(start)
			for i := 0; i < 2; i++ {
				r := <-responses
				if r.Code != 200 || !strings.Contains(r.Body.String(), `"route":"/invoices"`) {
					t.Fatal(r.Code, r.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id=?", 0, invoice.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoice_pdf_assets WHERE invoice_id=?", 0, invoice.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='invoice_deleted'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			if hasPDF {
				if _, err := os.Stat(filepath.Join(pdfRoot, filepath.FromSlash(asset.RelativePath))); !os.IsNotExist(err) {
					t.Fatal("deleted PDF still live", err)
				}
			}
			receipts, err := service.aiActionReceipts(context.Background(), generation.SessionID)
			if err != nil || !strings.Contains(receipts, `"action":"invoice.delete"`) || !strings.Contains(receipts, `"status":"confirmed"`) || strings.Contains(receipts, "private delete notes") || strings.Contains(receipts, client.Name) || strings.Contains(receipts, "pdf_sha256") {
				t.Fatal("unsafe or missing deletion receipt", receipts, err)
			}
		})
	}
}

func TestAIInvoiceDeleteConflictsAndMissingFile(t *testing.T) {
	for _, scenario := range []string{"pdf_added", "pdf_replaced", "client_renamed", "version", "deleted", "unsafe_file", "missing_file", "storage_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, service, tool, generation := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"Deletion conflict"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
			var asset models.InvoicePDFAsset
			if scenario != "pdf_added" {
				generateInvoicePDFForTest(t, router.Engine, invoice, "before")
				if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&asset).Error; err != nil {
					t.Fatal(err)
				}
			}
			args := fmt.Sprintf(`{"action":"invoice.delete","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID)
			row := proposeTestAction(t, store, tool, args)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			code, status := "AI_ACTION_PREVIEW_CHANGED", 409
			handler := router.Engine
			switch scenario {
			case "pdf_added", "pdf_replaced":
				generateInvoicePDFForTest(t, router.Engine, invoice, "changed")
			case "client_renamed":
				if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Update("name", "Changed").Error; err != nil {
					t.Fatal(err)
				}
			case "version":
				transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
				code = "VERSION_CONFLICT"
			case "deleted":
				r := performRequest(router.Engine, "DELETE", "/api/v1/invoices/"+invoice.ID+"?confirm=true", nil, map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				code, status = "INVOICE_NOT_FOUND", 404
			case "unsafe_file", "missing_file":
				path := filepath.Join(service.invoicePDFStore.root, filepath.FromSlash(asset.RelativePath))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "unsafe_file" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					code, status = "INVOICE_PDF_STORAGE_ERROR", 500
				} else {
					status = 200
				}
			case "storage_unavailable":
				options := service.options
				options.SessionToken, options.AllowedOrigins = testToken, []string{"tauri://localhost"}
				options.Logger = log.New(io.Discard, "", 0)
				unavailable, err := NewRouter(store.DB, options)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = unavailable.Close() })
				handler = unavailable.Engine
				code, status = "INVOICE_PDF_STORAGE_UNAVAILABLE", 503
			}
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true,"confirm_invoice_delete":true}`, row.Fingerprint))
			r := performRequest(handler, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
			if status == 200 {
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id=?", 0, invoice.ID)
			} else {
				assertAPIError(t, r, status, code)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
			}
		})
	}
}

func TestAIInvoiceDeleteAuthorityAndDomain(t *testing.T) {
	router, store, service, tool, _ := aiInvoiceDeleteFixture(t)
	client := createClientForTest(t, router.Engine, `{"name":"Delete scope"}`, nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
	args := fmt.Sprintf(`{"action":"invoice.delete","invoice_id":%q,"expected_version":1,"changes":{}}`, invoice.ID)
	for _, scopes := range [][]string{{"work", "actions"}, {"finance"}, {"finance", "finance_actions"}} {
		tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("deletion accepted without invoice grant", scopes)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
	for _, bad := range []string{strings.Replace(args, `"changes":{}`, `"changes":{"confirm_invoice_delete":true}`, 1), strings.Replace(args, `"changes":{}`, `"changes":{},"confirm_invoice_delete":true`, 1), strings.Replace(args, `"changes":{}`, `"changes":{"notes":"replacement"}`, 1)} {
		if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatal("accepted model override", bad)
		}
	}
	generateInvoicePDFForTest(t, router.Engine, invoice, "scope-pdf")
	service.invoicePDFStore = nil
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "INVOICE_PDF_STORAGE_UNAVAILABLE") {
		t.Fatal("missing store not rejected", err)
	}
	service.invoicePDFStore = router.invoicePDFStore
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	args = strings.Replace(args, `"expected_version":1`, `"expected_version":2`, 1)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "INVOICE_NOT_DRAFT") {
		t.Fatal("sent invoice deletion accepted", err)
	}
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_viewed"}`, "")
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_paid","paid_date":"2026-09-18"}`, "")
	// Schema disallows draft-linked payments. Test the defensive ledger guard
	// with a stale in-memory draft against an actual native-created payment.
	if err := validateInvoiceDeletion(store.DB, models.Invoice{ID: invoice.ID, Status: "draft"}); err == nil || !strings.Contains(err.Error(), "financial entry") {
		t.Fatal("linked ledger not rejected", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestInvoiceDeleteReconcileTransactionBoundary(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			router, store, service, _, _ := aiInvoiceDeleteFixture(t)
			client := createClientForTest(t, router.Engine, `{"name":"Recovery fixture"}`, nil)
			invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
			generateInvoicePDFForTest(t, router.Engine, invoice, "recovery")
			var asset models.InvoicePDFAsset
			if err := store.DB.Where("invoice_id=?", invoice.ID).Take(&asset).Error; err != nil {
				t.Fatal(err)
			}
			files := service.invoicePDFStore
			files.mu.Lock()
			defer files.mu.Unlock()
			var moved *trashedInvoicePDF
			rollback := errors.New("simulate pre-commit interruption")
			err := store.DB.Transaction(func(tx *gorm.DB) error {
				var err error
				moved, err = deleteInvoiceInTransaction(tx, files, invoice.ID, invoice.Version, "", invoice.UpdatedAt)
				if err != nil {
					return err
				}
				if !committed {
					return rollback
				}
				return nil
			})
			if (committed && err != nil) || (!committed && !errors.Is(err, rollback)) || moved == nil {
				t.Fatal(err)
			}
			// Deliberately omit normal restore/purge to exercise the startup
			// reconciler on each durable DB/file boundary, without killing a process.
			if err := files.reconcile(store.DB); err != nil {
				t.Fatal(err)
			}
			live := filepath.Join(files.root, filepath.FromSlash(asset.RelativePath))
			if committed {
				if _, err := os.Stat(live); !os.IsNotExist(err) {
					t.Fatal("deleted PDF recovered", err)
				}
			} else if status, err := files.verify(asset.RelativePath, asset.ID, asset.SizeBytes, asset.SHA256); err != nil || status != "verified" {
				t.Fatal("rolled-back PDF not restored", status, err)
			}
			if _, err := os.Stat(moved.trashPath); !os.IsNotExist(err) {
				t.Fatal("trash not reconciled", err)
			}
		})
	}
}
