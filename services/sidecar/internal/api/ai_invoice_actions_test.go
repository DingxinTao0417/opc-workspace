package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInvoiceActionsScopeAndParser(t *testing.T) {
	if err := validateAIWorkspaceGrant(&models.AIProvider{Version: 1}, &aiWorkspaceGrant{ProviderVersion: 1, Scopes: []string{"finance", "invoice_actions"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAIWorkspaceAction([]byte(`{"action":"invoice.create","changes":{"client_id":"00000000-0000-4000-8000-000000000001","amount_minor":1250,"currency":"CNY","issue_date":"2026-09-18","due_date":"2026-09-30"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{{"invoice_actions"}, {"finance", "invoice_actions", "invoice_actions"}} {
		if err := validateAIWorkspaceGrant(&models.AIProvider{Version: 1}, &aiWorkspaceGrant{ProviderVersion: 1, Scopes: scopes}); err == nil {
			t.Fatal("invalid invoice grant accepted", scopes)
		}
	}
	_, _, _, tool, _ := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	if strings.Join(schema.Properties["action"].Enum, ",") != strings.Join(aiInvoiceActions, ",") {
		t.Fatal("unexpected invoice-only actions", schema)
	}
	var full map[string]any
	if err := json.Unmarshal(aiWorkspaceActionSchema(), &full); err != nil {
		t.Fatal(err)
	}
	fields := full["properties"].(map[string]any)["changes"].(map[string]any)["properties"].(map[string]any)
	due := fields["due_date"].(map[string]any)
	types, ok := due["type"].([]any)
	if !ok || len(types) != 2 || types[1] != "null" || !strings.Contains(due["description"].(string), "RFC3339") {
		t.Fatal("Invoice schema broke Task/Project dates", due)
	}
}

func TestAIInvoiceActionsLocalCalendarAtConfirmation(t *testing.T) {
	for _, command := range []string{"mark_paid", "mark_overdue"} {
		t.Run(command, func(t *testing.T) {
			now := time.Date(2026, 9, 18, 0, 30, 0, 0, time.FixedZone("UTC+8", 8*3600))
			router, store, _, tool, generation := aiActionTestFixture(t, now)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
			client := createClientForTest(t, router, `{"name":"Local calendar"}`, nil)
			invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-17"}`, client.ID), nil)
			invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
			invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
			changes := `{}`
			if command == "mark_paid" {
				changes = `{"paid_date":"2026-09-18"}`
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"invoice.%s","invoice_id":%q,"expected_version":%d,"changes":%s}`, command, invoice.ID, invoice.Version, changes))
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true}`, row.Fingerprint))
			if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil); r.Code != 200 {
				t.Fatal(r.Code, r.Body.String())
			}
		})
	}
}

func TestAIInvoiceActionsDomainConflicts(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
	client := createClientForTest(t, router, `{"name":"Invoice customer"}`, nil)
	other := createClientForTest(t, router, `{"name":"Other customer"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Other project","client_id":%q}`, other.ID), nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
	args := func(command, changes string, version int64) string {
		return fmt.Sprintf(`{"action":"invoice.%s","invoice_id":%q,"expected_version":%d,"changes":%s}`, command, invoice.ID, version, changes)
	}
	for _, bad := range []string{
		args("update", fmt.Sprintf(`{"project_id":%q}`, project.ID), invoice.Version),
		args("update", `{"due_date":"2026-08-31"}`, invoice.Version),
		args("update", `{"amount_minor":2500}`, invoice.Version+1),
		args("mark_paid", `{"paid_date":"2026-09-18"}`, invoice.Version),
		args("mark_viewed", `{}`, invoice.Version),
		args("update", fmt.Sprintf(`{"notes":%q}`, strings.Repeat("字", 10001)), invoice.Version),
	} {
		if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatal("accepted invalid invoice operation", bad)
		}
	}
	row := proposeTestAction(t, store, tool, args("update", `{"amount_minor":2500}`, invoice.Version))
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE amount_minor=1250 AND status='sent'", 1)
	// Re-enable the test generation solely to exercise proposal-time domain checks.
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{args("update", `{"amount_minor":2500}`, invoice.Version), args("mark_overdue", `{}`, invoice.Version)} {
		if _, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatal("accepted invalid sent invoice operation")
		}
	}
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
	for _, date := range []string{"2026-08-31", "2026-09-19"} {
		if _, err := tool.Execute(context.Background(), []byte(args("mark_paid", fmt.Sprintf(`{"paid_date":%q}`, date), invoice.Version))); err == nil {
			t.Fatal("accepted invalid payment date", date)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 1)
}

func TestAIInvoiceActionsApprovalLifecycle(t *testing.T) {
	// File deletion has a separate suite covering store rollback and recovery.
	for _, action := range []string{"invoice.create", "invoice.update", "invoice.mark_sent", "invoice.mark_viewed", "invoice.mark_paid", "invoice.mark_overdue"} {
		t.Run(action, func(t *testing.T) {
			router, store, service, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
			client := createClientForTest(t, router, `{"name":"Private invoice customer"}`, nil)
			due := "2026-09-20"
			if action == "invoice.mark_overdue" {
				due = "2026-09-10"
				configureAndEnableInvoiceAutomation(t, router, "P0")
			}
			notes := "Private invoice notes"
			if action == "invoice.update" {
				notes = strings.Repeat("<😀", 5000)
			}
			create := map[string]any{"client_id": client.ID, "amount_minor": 1250, "currency": "CNY", "issue_date": "2026-09-01", "due_date": due, "notes": notes}
			body, _ := json.Marshal(create)
			invoice := createInvoiceForTest(t, router, string(body), nil)
			if action == "invoice.mark_viewed" || action == "invoice.mark_paid" || action == "invoice.mark_overdue" {
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
			}
			if action == "invoice.mark_paid" {
				invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
				if err := service.projectDueInvoices(context.Background()); err != nil {
					t.Fatal(err)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status='open'", 1)
			}
			changes := map[string]any{}
			args := map[string]any{"action": action, "invoice_id": invoice.ID, "expected_version": invoice.Version, "changes": changes}
			switch action {
			case "invoice.create":
				args = map[string]any{"action": action, "changes": create}
			case "invoice.update":
				changes["amount_minor"], changes["notes"] = 3750, strings.Repeat(">🚀", 5000)
			case "invoice.mark_paid":
				changes["paid_date"] = "2026-09-18"
			}
			encoded, _ := json.Marshal(args)
			result, err := tool.Execute(context.Background(), encoded)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result, client.Name) || strings.Contains(result, "Private invoice notes") || strings.Contains(result, "😀") {
				t.Fatal("human preview leaked into tool result")
			}
			row := proposeTestAction(t, store, tool, string(encoded))
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 1)
			if action == "invoice.update" && len(row.PreviewJSON) <= 64<<10 {
				t.Fatal("long preview fixture too small")
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true}`, row.Fingerprint))
			assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			for _, consent := range []string{"", `,"confirm_invoice_effects":false`} {
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, row.Fingerprint, consent)), nil), 422, "INVOICE_ACTION_CONFIRMATION_REQUIRED")
			}
			assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_invoice_effects":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
			if err := store.DB.Exec("CREATE TRIGGER fail_invoice_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices WHERE id=? AND version=? AND status=?", 1, invoice.ID, invoice.Version, invoice.Status)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			if action == "invoice.mark_paid" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status='open'", 1)
			}
			if err := store.DB.Exec("DROP TRIGGER fail_invoice_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if r := performRequest(router, "POST", path, body, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "/invoices/") {
					t.Fatal(r.Code, r.Body.String())
				}
			}
			if err := store.DB.First(&row, "id=?", row.ID).Error; err != nil || row.ResultID == nil {
				t.Fatal(err)
			}
			var actual models.Invoice
			if err := store.DB.First(&actual, "id=?", *row.ResultID).Error; err != nil {
				t.Fatal(err)
			}
			switch action {
			case "invoice.create":
				if actual.Version != 1 || actual.Status != "draft" || actual.InvoiceNumber == invoice.InvoiceNumber || actual.Notes != notes {
					t.Fatal("invalid creation")
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 2)
			case "invoice.update":
				if actual.AmountMinor != 3750 || actual.Notes != changes["notes"] {
					t.Fatal("incomplete edit")
				}
			case "invoice.mark_sent":
				if actual.Status != "sent" {
					t.Fatal(actual.Status)
				}
			case "invoice.mark_viewed":
				if actual.Status != "viewed" {
					t.Fatal(actual.Status)
				}
			case "invoice.mark_paid":
				if actual.Status != "paid" || actual.PaidDate == nil || *actual.PaidDate != "2026-09-18" {
					t.Fatal("invalid payment")
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE invoice_id=? AND amount_minor=1250 AND currency='CNY' AND status='confirmed'", 1, invoice.ID)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='invoice_due' AND status='resolved'", 1)
			case "invoice.mark_overdue":
				if actual.Status != "overdue" {
					t.Fatal(actual.Status)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE status='succeeded'", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
			}
			if action != "invoice.create" && actual.Version != invoice.Version+1 {
				t.Fatal("duplicate command")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
		})
	}
}

func TestAIInvoiceActionsStrictBoundariesAndPreviewConflict(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Invoice customer"}`, nil)
	create := fmt.Sprintf(`{"action":"invoice.create","changes":{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-20"}}`, client.ID)
	for _, scopes := range [][]string{{"work", "actions", "finance"}, {"finance", "finance_actions"}, {"invoice_actions"}, {"finance"}} {
		tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(create)); err == nil {
			t.Fatal("scope bypass", scopes)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "invoice_actions")
	for _, other := range []string{`{"action":"task.create","changes":{"title":"No"}}`, `{"action":"financial_entry.create","changes":` + financialCreateChanges + `}`} {
		if _, err := tool.Execute(context.Background(), []byte(other)); err == nil {
			t.Fatal("grant expanded")
		}
	}
	for _, bad := range []string{
		strings.Replace(create, `"changes":`, `"invoice_id":null,"changes":`, 1),
		strings.Replace(create, `"changes":`, `"financial_entry_id":null,"changes":`, 1),
		strings.Replace(create, `"amount_minor":100`, `"amount_minor":1.25`, 1),
		strings.Replace(create, `"currency":"CNY"`, `"currency":"cny"`, 1),
		strings.Replace(create, `"issue_date":"2026-09-01"`, `"issue_date":"1999-09-01"`, 1),
		strings.Replace(create, `"due_date":"2026-09-20"`, `"status":"paid"`, 1),
		strings.Replace(create, `"due_date":"2026-09-20"`, `"due_date":"2026-02-30"`, 1),
		strings.Replace(create, `"due_date":"2026-09-20"`, `"due_date":"2026-09-20","confirm_invoice_effects":true`, 1),
	} {
		if _, err := parseAIWorkspaceAction([]byte(bad)); err == nil {
			t.Fatal("accepted invalid action", bad)
		}
	}
	row := proposeTestAction(t, store, tool, create)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Update("name", "Changed invoice customer").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 0)
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
}

func TestAIInvoiceActionsHarnessAndReceipt(t *testing.T) {
	for _, command := range []string{"mark_paid", "delete", "generate_pdf"} {
		t.Run(command, func(t *testing.T) { testAIInvoiceHarnessCommand(t, command) })
	}
}

func testAIInvoiceHarnessCommand(t *testing.T, command string) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, keyStore := newAIProviderTestRouter(t, now)
	if command == "generate_pdf" {
		pdfRouter, err := NewRouter(store.DB, Options{
			AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
			SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
			Logger: log.New(io.Discard, "", 0), KeyStore: keyStore, Now: func() time.Time { return now },
			InvoicePDFDir: filepath.Join(t.TempDir(), "pdf"),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pdfRouter.Close() })
		router = pdfRouter.Engine
	}
	client := createClientForTest(t, router, `{"name":"Private invoice customer"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":1250,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30","notes":"Private invoice notes"}`, client.ID), nil)
	if command == "mark_paid" {
		invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
		invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_viewed"}`, "")
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if calls <= 2 {
			name, args := "workspace_finance", fmt.Sprintf(`{"view":"invoice","id":%q}`, invoice.ID)
			if calls == 2 {
				name = "workspace_propose"
				args = fmt.Sprintf(`{"action":"invoice.mark_paid","invoice_id":%q,"expected_version":%d,"changes":{"paid_date":"2026-09-18"}}`, invoice.ID, invoice.Version)
				if command == "delete" || command == "generate_pdf" {
					args = fmt.Sprintf(`{"action":"invoice.%s","invoice_id":%q,"expected_version":%d,"changes":{}}`, command, invoice.ID, invoice.Version)
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("invoice-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), client.Name) || strings.Contains(string(encoded), "Private invoice notes") {
			t.Error("manual-only invoice data leaked")
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("pending result missing")
		}
		if calls == 4 {
			definitions, _ := json.Marshal(body["tools"])
			route := "/invoices/" + invoice.ID
			if command == "delete" {
				route = `/invoices\"`
			}
			if aiHasProtectedWorkspaceTool(definitions) || !strings.Contains(string(encoded), route) || !strings.Contains(string(encoded), `confirmed`) || !strings.Contains(string(encoded), "invoice."+command) {
				t.Error("receipt or grant boundary")
			}
		}
		streamMockAIDelta(w, `请核对发票确认卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "invoice-actions", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	grant := aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"finance", "invoice_actions"}}
	temporary := models.AISession{ID: uuid.NewString(), Title: "Temporary", Persist: false, Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&temporary).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": temporary.ID, "message": "按我的请求处理发票", "workspace": grant})
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", body, nil), 422, "AI_ACTION_PERSIST_REQUIRED")
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "message": "按我的请求处理发票", "workspace": grant})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 3 {
		t.Fatal(r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 1)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true}`, row.Fingerprint))
	if command == "delete" {
		body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true,"confirm_invoice_delete":true}`, row.Fingerprint))
	}
	if command == "generate_pdf" {
		body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_invoice_effects":true,"confirm_invoice_pdf":true}`, row.Fingerprint))
	}
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if command == "delete" {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 0)
	}
	var session models.AISession
	if err := store.DB.Where("persist=1").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才执行了吗"})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil); r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
}
