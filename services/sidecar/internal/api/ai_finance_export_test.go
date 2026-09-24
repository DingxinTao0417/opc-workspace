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

const financeExportArgs = `{"action":"finance.export_csv","changes":{"export_filters":{"currency":"CNY","date_from":"2026-09-01","date_to":"2026-09-30","entry_type":"all","status":"active"}}}`

func TestAIFinanceExportLifecycle(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_exports")
	entry := createFinancialEntryForTest(t, router, strings.TrimSuffix(financialCreateChanges, "}")+`,"notes":"=PRIVATE(1)"}`, nil)
	row := proposeTestAction(t, store, tool, financeExportArgs)
	if strings.Contains(row.PreviewJSON, "PRIVATE") {
		t.Fatal("export body retained in preview")
	}
	var preview aiActionPreview
	_ = json.Unmarshal([]byte(row.PreviewJSON), &preview)
	if preview.After["row_count"] != float64(1) {
		t.Fatal(preview)
	}
	path := "/api/v1/ai/actions/" + row.ID
	download := path + "/export.csv?fingerprint=" + row.Fingerprint
	assertAPIError(t, performRequest(router, "GET", download, nil, nil), 409, "AI_EXPORT_NOT_APPROVED")
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_finance_export":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path+"/decision", body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", path+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 422, "FINANCE_EXPORT_CONFIRMATION_REQUIRED")
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", path+"/decision", body, nil)
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := performRequest(router, "GET", download, nil, nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "'=PRIVATE(1)") || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(r.Code, r.Body.String())
	}
	if r.Header().Get("X-Financial-CSV-SHA256") != preview.After["sha256"] {
		t.Fatal("identity missing")
	}
	native := performRequest(router, "GET", "/api/v1/financial-entries/export.csv?confirm=true&currency=CNY&date_from=2026-09-01&date_to=2026-09-30", nil, nil)
	if native.Code != 200 || native.Body.String() != r.Body.String() {
		t.Fatal("native export diverged")
	}
	receipts, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || strings.Contains(receipts, "PRIVATE") || strings.Contains(receipts, "sha256") || !strings.Contains(receipts, "approved_download_unverified") {
		t.Fatal(receipts, err)
	}
	if err := store.DB.Exec("UPDATE financial_entries SET notes='changed',version=version+1 WHERE id=?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "GET", download, nil, nil), 409, "AI_EXPORT_CHANGED")
	if r := performRequest(router, "POST", path+"/decision", body, nil); r.Code != 200 {
		t.Fatal("historical approval replay failed", r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIFinanceExportGuardrails(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	for _, scopes := range [][]string{{"finance"}, {"finance_exports"}, {"finance", "finance_actions", "invoice_actions", "work", "actions"}} {
		tool.(*aiWorkspaceTool).policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(financeExportArgs)); err == nil {
			t.Fatal("unauthorized", scopes)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_exports")
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema)
	if strings.Join(schema.Properties["action"].Enum, ",") != "finance.export_csv" {
		t.Fatal(schema)
	}
	for _, bad := range []string{
		strings.Replace(financeExportArgs, `"CNY"`, `"cny"`, 1), strings.Replace(financeExportArgs, `"date_from":"2026-09-01",`, "", 1),
		strings.Replace(financeExportArgs, `2026-09-30`, `2028-01-01`, 1), strings.Replace(financeExportArgs, `2026-09-01`, `2026-09-31`, 1),
		strings.Replace(financeExportArgs, `"status":"active"`, `"status":null`, 1), strings.Replace(financeExportArgs, `"status":"active"`, `"status":"active","page":1`, 1),
		strings.Replace(financeExportArgs, `"status":"active"`, `"status":"active","category":""`, 1),
		strings.Replace(financeExportArgs, `"changes":`, `"expected_version":1,"changes":`, 1),
		strings.Replace(financeExportArgs, `"changes":`, `"confirm_finance_export":true,"changes":`, 1),
	} {
		if _, err := parseAIWorkspaceAction([]byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	row := proposeTestAction(t, store, tool, financeExportArgs)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID
	for _, q := range []string{"", "?fingerprint=" + row.Fingerprint + "&currency=USD", "?fingerprint=" + row.Fingerprint + "&fingerprint=" + row.Fingerprint, "?fingerprint=bad"} {
		assertAPIError(t, performRequest(router, "GET", path+"/export.csv"+q, nil, nil), 422, "AI_EXPORT_LOCATION_INVALID")
	}
	assertAPIError(t, performRequest(router, "POST", path+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_finance_export":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
	// New matching rows change the immutable preview, even without a row version target.
	createFinancialEntryForTest(t, router, financialCreateChanges, nil)
	assertAPIError(t, performRequest(router, "POST", path+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_finance_export":true}`, row.Fingerprint)), nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	if r := performRequest(router, "POST", path+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertAPIError(t, performRequest(router, "GET", path+"/export.csv?fingerprint="+row.Fingerprint, nil, nil), 409, "AI_EXPORT_NOT_APPROVED")
}

func TestAIFinanceExportFullSetAndBounds(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_exports")
	entry := createFinancialEntryForTest(t, router, financialCreateChanges, nil)
	var base models.FinancialEntry
	if err := store.DB.First(&base, "id=?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]models.FinancialEntry, 10000)
	for i := range rows {
		rows[i] = base
		rows[i].ID = uuid.NewString()
	}
	if err := store.DB.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(financeExportArgs)); err == nil || !strings.Contains(err.Error(), "EXPORT_TOO_LARGE") {
		t.Fatal(err)
	}
	if r := performRequest(router, "GET", "/api/v1/financial-entries/export.csv?confirm=true", nil, nil); r.Code != 413 {
		t.Fatal(r.Code)
	}
	if err := store.DB.Model(&rows[0]).Updates(map[string]any{"currency": "USD", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	proposal := proposeTestAction(t, store, tool, financeExportArgs)
	var p aiActionPreview
	_ = json.Unmarshal([]byte(proposal.PreviewJSON), &p)
	if p.After["row_count"] != float64(10000) {
		t.Fatal("truncated export", p.After["row_count"])
	}
	// Same encoder enforces a byte budget independent of the number of rows.
	var b financialCSVBuffer
	if _, err := b.Write(make([]byte, maxFinancialCSVBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte{1}); err == nil || b.Len() != maxFinancialCSVBytes {
		t.Fatal("unbounded CSV buffer", err)
	}
	for _, v := range []string{"=1", "+cmd", "-1", "@sum", " \t=1", "\rtext", "\ntext", "＝1"} {
		if financialCSVText(v) != "'"+v {
			t.Fatal("formula", v)
		}
	}
	for _, v := range []string{"项目款", "12.50", "a,b\n c", "2026-09-18"} {
		if financialCSVText(v) != v {
			t.Fatal("changed safe text", v)
		}
	}
}

func TestAIFinanceExportFiltersAndRelatedChanges(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("finance", "finance_exports")
	client := createClientForTest(t, router, `{"name":"Export customer"}`, nil)
	for _, kind := range []string{"income", "expense"} {
		for _, status := range []string{"pending", "confirmed", "voided"} {
			initial := status
			if initial == "voided" {
				initial = "confirmed"
			}
			e := createFinancialEntryForTest(t, router, fmt.Sprintf(`{"type":%q,"status":%q,"amount_minor":100,"currency":"CNY","occurred_on":"2026-09-18","category":"办公","client_id":%q}`, kind, initial, client.ID), nil)
			if status == "voided" {
				if r := performRequest(router, "DELETE", "/api/v1/financial-entries/"+e.ID+"?confirm=true", []byte(`{"reason":"duplicate"}`), map[string]string{"If-Match": `"1"`}); r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			}
		}
	}
	for _, v := range []struct {
		status, kind string
		count        int
	}{{"active", "all", 4}, {"all", "all", 6}, {"voided", "all", 2}, {"pending", "expense", 1}, {"confirmed", "income", 1}} {
		args := strings.Replace(financeExportArgs, `"status":"active"`, `"status":"`+v.status+`"`, 1)
		args = strings.Replace(args, `"entry_type":"all"`, `"entry_type":"`+v.kind+`"`, 1)
		input, err := parseAIWorkspaceAction([]byte(args))
		if err != nil {
			t.Fatal(err)
		}
		_, file, err := previewAIFinanceExport(store.DB, input)
		if err != nil || file.Rows != v.count {
			t.Fatal(v, file.Rows, err)
		}
	}
	row := proposeTestAction(t, store, tool, financeExportArgs)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_finance_export":true}`, row.Fingerprint))
	path := "/api/v1/ai/actions/" + row.ID
	if err := store.DB.Exec("CREATE TRIGGER fail_export_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path+"/decision", body, nil); r.Code != 500 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
	if err := store.DB.Exec("DROP TRIGGER fail_export_approval").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path+"/decision", body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	// Related human-only names are part of the actual file identity as well.
	if err := store.DB.Table("clients").Where("id=?", client.ID).Updates(map[string]any{"name": "Renamed export customer", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "GET", path+"/export.csv?fingerprint="+row.Fingerprint, nil, nil), 409, "AI_EXPORT_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM financial_entries", 6)
}

func TestAIFinanceExportHarnessAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	createFinancialEntryForTest(t, router, strings.TrimSuffix(financialCreateChanges, "}")+`,"notes":"PRIVATE_EXPORT_BODY"}`, nil)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), "PRIVATE_EXPORT_BODY") {
			t.Error("CSV leaked to model")
		}
		if calls == 1 {
			tools, _ := json.Marshal(body["tools"])
			if !strings.Contains(string(tools), `"workspace_guide"`) || strings.Contains(string(tools), `"finance.export_csv"`) || strings.Contains(string(tools), `"financial_entry.create"`) {
				t.Error("wrong initial finance export catalog")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "export-1", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": financeExportArgs}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 {
			tools, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(tools) || !strings.Contains(string(encoded), "approved_download_unverified") {
				t.Error("approval receipt or scope boundary")
			}
		}
		streamMockAIDelta(w, `请核对导出卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "export-harness", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	grant := aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"finance", "finance_exports"}}
	for _, scopes := range [][]string{{"finance_exports"}, {"finance", "finance_exports", "finance_exports"}} {
		bad := grant
		bad.Scopes = scopes
		b, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "导出", "workspace": bad})
		assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", b, nil), 422, "AI_WORKSPACE_GRANT_INVALID")
	}
	temporary := models.AISession{ID: uuid.NewString(), Title: "Temporary", Persist: false, Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&temporary).Error; err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": temporary.ID, "message": "导出", "workspace": grant})
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", b, nil), 422, "AI_ACTION_PERSIST_REQUIRED")
	b, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "message": "导出九月 CNY 全部非作废收支", "workspace": grant})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", b, nil); r.Code != 200 || calls != 2 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	b = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_finance_export":true}`, row.Fingerprint))
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", b, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var session models.AISession
	if err := store.DB.Where("persist=1").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才下载了吗"})
	if r := performRequest(router, "POST", "/api/v1/ai/chat", b, nil); r.Code != 200 || calls != 3 {
		t.Fatal(r.Body.String(), calls)
	}
}
