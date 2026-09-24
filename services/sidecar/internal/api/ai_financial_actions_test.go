package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

const financialCreateChanges = `{"type":"expense","amount_minor":1250,"currency":"CNY","occurred_on":"2026-09-18","status":"confirmed","category":"办公"}`

func TestAIFinancialActionsApprovalLifecycle(t *testing.T) {
	for _, action := range []string{"create", "update", "void"} {
		t.Run(action, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_actions")
			entry := createFinancialEntryForTest(t, router, strings.TrimSuffix(financialCreateChanges, "}")+`,"notes":"Private local note"}`, nil)
			args := `{"action":"financial_entry.create","changes":` + financialCreateChanges + `}`
			if action == "update" {
				args = fmt.Sprintf(`{"action":"financial_entry.update","financial_entry_id":%q,"expected_version":1,"changes":{"amount_minor":3750,"status":"pending"}}`, entry.ID)
			}
			if action == "void" {
				args = fmt.Sprintf(`{"action":"financial_entry.void","financial_entry_id":%q,"expected_version":1,"changes":{"reason":"重复录入"}}`, entry.ID)
			}
			row := proposeTestAction(t, store, tool, args)
			if action != "create" && !strings.Contains(row.PreviewJSON, "Private local note") {
				t.Fatal("incomplete HUMAN preview")
			}
			if duplicate := proposeTestAction(t, store, tool, args); duplicate.ID != row.ID {
				t.Fatal("duplicate proposal")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 1)
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_financial_effects":true}`, row.Fingerprint))
			assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			for _, consent := range []string{"", `,"confirm_financial_effects":false`} {
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, row.Fingerprint, consent)), nil), 422, "FINANCIAL_ACTION_CONFIRMATION_REQUIRED")
			}
			assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_financial_effects":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
			// A failure recording the approval rolls back the ledger and its audit.
			if err := store.DB.Exec("CREATE TRIGGER fail_finance_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries WHERE version=1 AND status='confirmed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			if err := store.DB.Exec("DROP TRIGGER fail_finance_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if r := performRequest(router, "POST", path, body, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "/income/") {
					t.Fatal(r.Code, r.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			var result models.AIActionProposal
			if err := store.DB.First(&result, "id=?", row.ID).Error; err != nil || result.ResultID == nil {
				t.Fatal(err)
			}
			actual := decodeFinancialEntryResponse(t, performRequest(router, "GET", "/api/v1/financial-entries/"+*result.ResultID, nil, nil).Body.Bytes())
			switch action {
			case "create":
				if actual.Version != 1 || actual.AmountMinor != 1250 {
					t.Fatal(actual)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 2)
			case "update":
				if actual.Version != 2 || actual.AmountMinor != 3750 || actual.Status != "pending" || actual.Notes != "Private local note" {
					t.Fatal(actual)
				}
			case "void":
				if actual.Version != 2 || actual.Status != "voided" || actual.VoidReason == nil || *actual.VoidReason != "重复录入" {
					t.Fatal(actual)
				}
			}
		})
	}
}

func TestAIFinancialActionsStrictBoundariesAndConflicts(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	args := `{"action":"financial_entry.create","changes":` + financialCreateChanges + `}`
	for _, scopes := range [][]string{{"work", "actions", "finance"}, {"finance"}, {"finance_actions"}} {
		tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("unauthorized", scopes)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_actions")
	if _, err := tool.Execute(context.Background(), []byte(`{"action":"task.create","changes":{"title":"Not allowed"}}`)); err == nil {
		t.Fatal("financial permission expanded to work")
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties["action"].Enum) != 3 {
		t.Fatal(schema)
	}
	for _, bad := range []string{
		strings.Replace(args, `"amount_minor":1250`, `"amount_minor":1.25`, 1),
		strings.Replace(args, `"amount_minor":1250`, `"amount_minor":9000000000000001`, 1),
		strings.Replace(args, `"amount_minor":1250`, `"amount_minor":null`, 1),
		strings.Replace(args, `"currency":"CNY"`, `"currency":"cny"`, 1),
		strings.Replace(args, `"status":"confirmed"`, `"status":"voided"`, 1),
		strings.Replace(args, `"status":"confirmed",`, "", 1),
		strings.Replace(args, `"category":"办公"`, `"category":null`, 1),
		strings.Replace(args, `"2026-09-18"`, `"2026-02-30"`, 1),
		strings.Replace(args, `"changes":`, `"task_id":null,"changes":`, 1),
		strings.Replace(args, `"changes":`, `"financial_entry_id":null,"changes":`, 1),
		strings.Replace(args, `"category":"办公"`, `"category":"办公","invoice_id":null`, 1),
		strings.Replace(args, `"category":"办公"`, `"category":"办公","confirm_financial_effects":true`, 1),
	} {
		if _, err := parseAIWorkspaceAction([]byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	client := createClientForTest(t, router, `{"name":"Local private name"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Financial project","client_id":%q}`, client.ID), nil)
	create := strings.Replace(args, `"category":"办公"`, fmt.Sprintf(`"category":"办公","project_id":%q`, project.ID), 1)
	row := proposeTestAction(t, store, tool, create)
	if !strings.Contains(row.PreviewJSON, client.ID) || !strings.Contains(row.PreviewJSON, client.Name) {
		t.Fatal("inferred relation missing")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Update("name", "Changed name").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_financial_effects":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 0)
	// Rejection remains available after related records change.
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	entry := createFinancialEntryForTest(t, router, financialCreateChanges, nil)
	update := fmt.Sprintf(`{"action":"financial_entry.update","financial_entry_id":%q,"expected_version":1,"changes":{"notes":"Explicit replacement"}}`, entry.ID)
	row = proposeTestAction(t, store, tool, update)
	if r := performRequest(router, "PATCH", "/api/v1/financial-entries/"+entry.ID, []byte(`{"amount_minor":1300}`), map[string]string{"If-Match": `"1"`}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_financial_effects":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "VERSION_CONFLICT")
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "DELETE", "/api/v1/financial-entries/"+entry.ID+"?confirm=true", []byte(`{"reason":"test"}`), map[string]string{"If-Match": `"2"`}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	update = strings.Replace(update, `"expected_version":1`, `"expected_version":3`, 1)
	if _, err := tool.Execute(context.Background(), []byte(update)); err == nil || !strings.Contains(err.Error(), "FINANCIAL_ENTRY_VOIDED") {
		t.Fatal(err)
	}
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-30"}`, client.ID), nil)
	for i, action := range []string{"mark_sent", "mark_viewed", "mark_paid"} {
		paidDate := ""
		if action == "mark_paid" {
			paidDate = `,"paid_date":"2026-09-18"`
		}
		r := performRequest(router, "POST", "/api/v1/invoices/"+invoice.ID+"/transition", []byte(fmt.Sprintf(`{"action":%q%s}`, action, paidDate)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, i+1)})
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	var paid models.FinancialEntry
	if err := store.DB.Where("invoice_id=?", invoice.ID).First(&paid).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"update", "void"} {
		changes := `{"amount_minor":200}`
		if action == "void" {
			changes = `{"reason":"not allowed"}`
		}
		_, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"financial_entry.%s","financial_entry_id":%q,"expected_version":1,"changes":%s}`, action, paid.ID, changes)))
		if err == nil || !strings.Contains(err.Error(), "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE") {
			t.Fatal(err)
		}
	}
}

func TestAIFinancialActionsHarnessAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	calls := 0
	resultID := ""
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if calls == 1 {
			encoded, _ := json.Marshal(body["tools"])
			if !strings.Contains(string(encoded), `"workspace_guide"`) || strings.Contains(string(encoded), `"financial_entry.create"`) || strings.Contains(string(encoded), `"task.create"`) {
				t.Error("wrong initial financial action catalog")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			args := `{"action":"financial_entry.create","changes":` + strings.TrimSuffix(financialCreateChanges, "}") + `,"notes":"New user supplied note"}}`
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "financial-1", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 2 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing pending instruction")
		}
		if calls == 3 {
			tools, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(tools) || !strings.Contains(string(encoded), resultID) || !strings.Contains(string(encoded), "/income/") {
				t.Error("receipt/grant boundary")
			}
			if strings.Contains(string(encoded), "New user supplied note") {
				t.Error("local preview leaked into receipt")
			}
		}
		streamMockAIDelta(w, `请核对确认卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "financial-actions", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{{"finance_actions"}, {"finance", "finance_actions", "finance_actions"}} {
		b, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "录入支出", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: scopes}})
		assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", b, nil), 422, "AI_WORKSPACE_GRANT_INVALID")
	}
	grant := aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"finance", "finance_actions"}}
	temporary := models.AISession{ID: uuid.NewString(), Title: "Temporary", Persist: false, Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&temporary).Error; err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": temporary.ID, "message": "记录真实支出", "workspace": grant})
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", b, nil), 422, "AI_ACTION_PERSIST_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
	b, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "message": "记录真实支出", "workspace": grant})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", b, nil); r.Code != 200 || calls != 2 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 0)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	b = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_financial_effects":true}`, row.Fingerprint))
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", b, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.First(&row, "id=?", row.ID).Error; err != nil || row.ResultID == nil {
		t.Fatal(err)
	}
	resultID = *row.ResultID
	var session models.AISession
	if err := store.DB.Where("persist=1").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才执行了吗"})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", b, nil); r.Code != 200 || calls != 3 {
		t.Fatal(r.Body.String(), calls)
	}
	if _, err := uuid.Parse(resultID); err != nil {
		t.Fatal(err)
	}
}

func TestAIFinancialActionScopeAndParser(t *testing.T) {
	provider := &models.AIProvider{Version: 1}
	if err := validateAIWorkspaceGrant(provider, &aiWorkspaceGrant{ProviderVersion: 1, Scopes: []string{"finance", "finance_actions"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAIWorkspaceAction([]byte(`{"action":"financial_entry.create","changes":{"type":"expense","amount_minor":1250,"currency":"CNY","occurred_on":"2026-09-18","status":"confirmed","category":"办公"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestAIFinancialActionsFullNotesAndPrivatePreview(t *testing.T) {
	for _, operation := range []string{"create", "update", "void"} {
		t.Run(operation, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_actions")
			oldNotes, newNotes := strings.Repeat("<😀", 5000), strings.Repeat(">🚀", 5000)
			var fields map[string]any
			if err := json.Unmarshal([]byte(financialCreateChanges), &fields); err != nil {
				t.Fatal(err)
			}
			fields["notes"] = oldNotes
			body, _ := json.Marshal(fields)
			entry := createFinancialEntryForTest(t, router, string(body), nil)
			fields["notes"] = newNotes
			action := map[string]any{"action": "financial_entry." + operation, "changes": fields}
			if operation != "create" {
				action["financial_entry_id"], action["expected_version"] = entry.ID, 1
				action["changes"] = map[string]any{"notes": newNotes}
			}
			if operation == "void" {
				action["changes"] = map[string]any{"reason": "Explicit duplicate record"}
			}
			args, _ := json.Marshal(action)
			result, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result, "😀") || strings.Contains(result, "🚀") || strings.Contains(result, "Explicit duplicate record") {
				t.Fatal("human-only preview leaked into tool result")
			}
			var row models.AIActionProposal
			if err := store.DB.First(&row).Error; err != nil {
				t.Fatal(err)
			}
			var preview aiActionPreview
			if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
				t.Fatal(err)
			}
			if operation != "create" && preview.Before["notes"] != oldNotes {
				t.Fatal("old notes missing or truncated")
			}
			want := newNotes
			if operation == "void" {
				want = oldNotes
			}
			if preview.After["notes"] != want {
				t.Fatal("new notes missing or truncated")
			}
			if operation == "update" && len(row.PreviewJSON) <= 64<<10 {
				t.Fatal("fixture did not exercise the full preview capacity")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_financial_effects":true}`, row.Fingerprint))
			if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil); r.Code != 200 {
				t.Fatal(r.Code, r.Body.String())
			}
			if err := store.DB.First(&row, "id=?", row.ID).Error; err != nil || row.ResultID == nil {
				t.Fatal(err)
			}
			var actual models.FinancialEntry
			if err := store.DB.First(&actual, "id=?", *row.ResultID).Error; err != nil || actual.Notes != want {
				t.Fatal("full notes not preserved in ledger", err)
			}
			if operation != "void" {
				action["changes"].(map[string]any)["notes"] = newNotes + "x"
				args, _ = json.Marshal(action)
				if _, err := parseAIWorkspaceAction(args); err == nil {
					t.Fatal("accepted notes beyond 10000 Unicode characters")
				}
			}
		})
	}
}
