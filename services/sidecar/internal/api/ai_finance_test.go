package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIFinanceConsentAndReadBoundary(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	api := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	client := createClientForTest(t, router, `{"name":"private-client","email":"private@example.com"}`, nil)
	entry := createFinancialEntryForTest(t, router, fmt.Sprintf(`{"type":"income","amount_minor":12500,"currency":"CNY","occurred_on":"2026-09-18","status":"confirmed","category":"service","client_id":%q,"notes":"private-entry-notes"}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":9800,"currency":"USD","issue_date":"2026-09-01","due_date":"2026-09-30","notes":"private-invoice-notes"}`, client.ID), nil)
	for _, scopes := range [][]string{nil, {"work"}, {"clients"}, {"work", "actions"}, {"finance"}} {
		var grant *aiWorkspaceGrant
		if len(scopes) > 0 {
			grant = &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
		}
		registry, err := api.aiChatToolRegistry("ephemeral", false, &provider, grant)
		if err != nil {
			t.Fatal(err)
		}
		tool, ok := registry.Get("workspace_finance")
		if len(scopes) != 1 || scopes[0] != "finance" {
			if ok {
				t.Fatal("finance without consent")
			}
			continue
		}
		for _, name := range []string{"workspace_search", "workspace_get", "workspace_propose"} {
			if _, found := registry.Get(name); found {
				t.Fatal("inherited", name)
			}
		}
		if !ok {
			t.Fatal("finance tool missing")
		}
		for _, args := range []string{`{"view":"entries"}`, fmt.Sprintf(`{"view":"entry","id":%q}`, entry.ID), `{"view":"invoices"}`, fmt.Sprintf(`{"view":"invoice","id":%q}`, invoice.ID), `{"view":"summary","currency":"CNY","date_from":"2026-09-01","date_to":"2026-09-30"}`} {
			result, err := tool.Execute(context.Background(), []byte(args))
			if err != nil {
				t.Fatal(args, err)
			}
			for _, secret := range []string{"private-client", "private@example.com", "private-entry-notes", "private-invoice-notes", "created_by_actor", "void_reason", "pdf_path"} {
				if strings.Contains(result, secret) {
					t.Fatal("leak", secret, result)
				}
			}
			if !strings.Contains(result, "12500") && !strings.Contains(result, "9800") {
				t.Fatal("missing amounts", result)
			}
		}
		result, err := tool.Execute(context.Background(), []byte(`{"view":"summary","currency":"CNY","date_from":"2026-09-01","date_to":"2026-09-30"}`))
		if err != nil {
			t.Fatal(err)
		}
		native := performRequest(router, "GET", "/api/v1/stats/income?currency=CNY&date_from=2026-09-01&date_to=2026-09-30", nil, nil)
		var got struct {
			Stats incomeStatsResponse `json:"stats"`
		}
		var want struct {
			Data incomeStatsResponse `json:"data"`
		}
		if err := json.Unmarshal([]byte(result), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(native.Body.Bytes(), &want); err != nil || native.Code != 200 || got.Stats != want.Data {
			t.Fatal("aggregate mismatch", got, want, err)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM invoices", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIFinanceStrictArgumentsPaginationAndRevocation(t *testing.T) {
	router, store, api, proposer, _ := aiActionTestFixture(t)
	reader := *proposer.(*aiWorkspaceTool)
	reader.name = "workspace_finance"
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"entries"}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal("missing finance guard", err)
	}
	reader.policy = harness.NewCapabilities("finance")
	for _, args := range []string{`{}`, `{"view":"pay"}`, `{"view":"entries","limit":0}`, `{"view":"entries","offset":1001}`, `{"view":"entry"}`, `{"view":"entries","id":"bad"}`, `{"view":"entry","id":"bad"}`, `{"view":"summary","currency":"CNY"}`, `{"view":"summary","currency":"USD","date_from":"2026-01-01","date_to":"2027-01-03"}`, `{"view":"summary","currency":"USD","date_from":"2026-02-30","date_to":"2026-03-01"}`, `{"view":"summary","currency":"USD","date_from":"2026-09-20","date_to":"2026-09-01"}`, `{"view":"summary","currency":"USD","date_from":"2026-09-01","date_to":"2026-09-30","status":"pending"}`, `{"view":"entries","currency":"usd"}`, `{"view":"entries","currency":""}`, `{"view":"entries","status":"paid"}`, `{"view":"invoices","status":"confirmed"}`, `{"view":"entries","client_id":"not-id"}`, `{"view":"entries","entry_type":"other"}`, `{"view":"entries","date_from":""}`, `{"view":"entries","query":"secret"}`, `{"view":"entries","limit":null}`, `{"view":"entries","sql":"SELECT *"}`, `{"view":"invoices","category":"secret"}`, `{"view":"entries"} {}`} {
		if _, err := reader.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("accepted", args)
		}
	}
	first := createFinancialEntryForTest(t, router, `{"type":"income","amount_minor":100,"currency":"CNY","occurred_on":"2026-09-01","category":"A"}`, nil)
	createFinancialEntryForTest(t, router, `{"type":"expense","amount_minor":50,"currency":"CNY","occurred_on":"2026-09-02","category":"B","status":"pending"}`, nil)
	third := createFinancialEntryForTest(t, router, `{"type":"income","amount_minor":75,"currency":"USD","occurred_on":"2026-09-03","category":"A"}`, nil)
	voided := performRequest(router, "DELETE", "/api/v1/financial-entries/"+third.ID+"?confirm=true", []byte(`{"reason":"private-reason"}`), map[string]string{"If-Match": "\"1\""})
	if voided.Code != 200 {
		t.Fatal(voided.Body.String())
	}
	result, err := reader.Execute(context.Background(), []byte(`{"view":"entries","limit":1}`))
	if err != nil || !strings.Contains(result, `"has_more":true`) || strings.Contains(result, third.ID) {
		t.Fatal(result, err)
	}
	result, err = reader.Execute(context.Background(), []byte(`{"view":"entries","limit":1,"offset":1}`))
	if err != nil || !strings.Contains(result, first.ID) || !strings.Contains(result, `"next_offset":null`) {
		t.Fatal(result, err)
	}
	result, err = reader.Execute(context.Background(), []byte(`{"view":"entries","status":"voided"}`))
	if err != nil || !strings.Contains(result, third.ID) || strings.Contains(result, "private-reason") {
		t.Fatal(result, err)
	}
	result, err = reader.Execute(context.Background(), []byte(`{"view":"entries","currency":"CNY","date_from":"2026-09-01","date_to":"2026-09-01","entry_type":"income","category":"A"}`))
	if err != nil || !strings.Contains(result, first.ID) || strings.Contains(result, `"has_more":true`) {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Execute(ctx, []byte(`{"view":"entries"}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	api.restorePending.Store(true)
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"entries"}`)); err == nil {
		t.Fatal("read during restore")
	}
	api.restorePending.Store(false)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", reader.providerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&provider).Updates(map[string]any{"config_version": reader.configVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"entries"}`)); err == nil {
		t.Fatal("read after provider change")
	}
}

func TestAIFinanceHarnessUsesExplicitGrantWithoutPersistingText(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	createFinancialEntryForTest(t, router, `{"type":"income","amount_minor":12345,"currency":"USD","occurred_on":"2026-09-03","category":"service","notes":"not-for-model"}`, nil)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(payload)
		definitions, _ := json.Marshal(payload["tools"])
		if strings.Contains(string(encoded), "not-for-model") {
			t.Error("notes leaked")
		}
		if calls == 4 {
			if strings.Contains(string(definitions), `"name":"workspace_finance"`) || strings.Contains(string(definitions), `"name":"workspace_guide"`) {
				t.Error("finance permission inherited by next message")
			}
			streamMockAIDelta(w, `请再次授权财务查询。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			return
		}
		if calls == 1 {
			if !strings.Contains(string(definitions), `"name":"workspace_guide"`) || strings.Contains(string(definitions), `"name":"workspace_finance"`) {
				t.Error("initial finance catalog must expose discovery before the domain helper")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "finance-guide", "type": "function", "function": map[string]any{"name": "workspace_guide", "arguments": `{"topic":"finance"}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if !strings.Contains(string(definitions), `"name":"workspace_finance"`) {
			t.Error("accepted finance guide did not expose the consented tool schema")
		}
		if strings.Contains(string(definitions), `"name":"workspace_propose"`) {
			t.Error("read grant gained write proposals")
		}
		if calls == 2 {
			if !strings.Contains(string(encoded), `workspace_guide: `) {
				t.Error("accepted finance guide result was not returned to the model")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "finance-1", "type": "function", "function": map[string]any{"name": "workspace_finance", "arguments": `{"view":"summary","currency":"USD","date_from":"2026-09-01","date_to":"2026-09-30"}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if !strings.Contains(string(encoded), "12345") || !strings.Contains(string(encoded), "/income?") {
			t.Error("missing actual report", string(encoded))
		}
		streamMockAIDelta(w, `本地报告中的已确认收入为 123.45 USD。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "finance", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	created := performRequest(router, "POST", "/api/v1/ai/sessions", []byte(`{"title":"temporary finance","persist":false}`), nil)
	var session struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil || created.Code != 201 {
		t.Fatal(created.Body.String(), err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.Data.ID, "message": "查询本地美元账本", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"finance"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") || !strings.Contains(r.Body.String(), "workspace_finance") {
		t.Fatal(r.Body.String(), calls)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.Data.ID, "message": "再次查询"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 1)
}

func TestAIFinanceSummaryCurrencyStatusAndInvoiceFilters(t *testing.T) {
	router, store, _, proposer, _ := aiActionTestFixture(t)
	reader := *proposer.(*aiWorkspaceTool)
	reader.name, reader.policy = "workspace_finance", harness.NewCapabilities("finance")
	for _, body := range []string{
		`{"type":"income","amount_minor":101,"currency":"CNY","occurred_on":"2026-09-01","category":"A"}`,
		`{"type":"income","amount_minor":103,"currency":"CNY","occurred_on":"2026-09-02","category":"A"}`,
		`{"type":"expense","amount_minor":50,"currency":"CNY","occurred_on":"2026-09-02","category":"B"}`,
		`{"type":"income","amount_minor":70,"currency":"CNY","occurred_on":"2026-09-02","category":"A","status":"pending"}`,
		`{"type":"income","amount_minor":9999,"currency":"USD","occurred_on":"2026-09-02","category":"A"}`,
	} {
		createFinancialEntryForTest(t, router, body, nil)
	}
	client := createClientForTest(t, router, `{"name":"not-a-number"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-10-01"}`, client.ID), nil)
	for _, step := range []struct{ version, body string }{{`"1"`, `{"action":"mark_sent"}`}, {`"2"`, `{"action":"mark_viewed"}`}, {`"3"`, `{"action":"mark_paid","paid_date":"2026-09-03"}`}} {
		r := performRequest(router, "POST", "/api/v1/invoices/"+invoice.ID+"/transition", []byte(step.body), map[string]string{"If-Match": step.version})
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	result, err := reader.Execute(context.Background(), []byte(`{"view":"summary","currency":"CNY","date_from":"2026-09-01","date_to":"2026-09-30"}`))
	var summary struct {
		Stats incomeStatsResponse `json:"stats"`
	}
	if err != nil || json.Unmarshal([]byte(result), &summary) != nil {
		t.Fatal(result, err)
	}
	if summary.Stats.ConfirmedIncomeMinor != 304 || summary.Stats.ConfirmedExpenseMinor != 50 || summary.Stats.PendingIncomeMinor != 70 || summary.Stats.NetCashFlowMinor != 254 || summary.Stats.AverageIncomeMinor != 101 {
		t.Fatal(summary)
	}
	for _, args := range []string{
		`{"view":"invoices","date_from":"2026-09-01","date_to":"2026-09-30"}`,
		`{"view":"invoices","query":"not-a-number"}`,
	} {
		result, err := reader.Execute(context.Background(), []byte(args))
		if err != nil || !strings.Contains(result, `"items":[]`) {
			t.Fatal(result, err)
		}
	}
	result, err = reader.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"invoices","date_from":"2026-10-01","date_to":"2026-10-01","query":%q,"status":"paid"}`, invoice.InvoiceNumber)))
	if err != nil || !strings.Contains(result, invoice.ID) {
		t.Fatal(result, err)
	}
	if _, err := reader.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"entry","id":%q}`, uuid.NewString()))); err == nil {
		t.Fatal("missing record accepted")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 6)
}
