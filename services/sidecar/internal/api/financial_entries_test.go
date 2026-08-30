package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func decodeFinancialEntryResponse(t *testing.T, body []byte) financialEntryResponse {
	t.Helper()
	var envelope struct {
		Data financialEntryResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode financial entry response: %v", err)
	}
	return envelope.Data
}

func createFinancialEntryForTest(t *testing.T, router http.Handler, body string, headers map[string]string) financialEntryResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/financial-entries", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create financial entry = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeFinancialEntryResponse(t, recorder.Body.Bytes())
}

func TestFinancialEntryCRUDIdempotencyAuditAndVoid(t *testing.T) {
	router, store := newProjectTestAPI(t)
	client := createClientForTest(t, router, `{"name":"财务客户"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"财务项目","client_id":%q}`, client.ID), nil)
	body := fmt.Sprintf(`{"type":"income","amount_minor":12800,"currency":"cny","occurred_on":"2026-08-20","status":"confirmed","category":"咨询服务","project_id":%q,"notes":"首付款"}`, project.ID)
	headers := map[string]string{"Idempotency-Key": "financial-entry-create-1"}
	created := createFinancialEntryForTest(t, router, body, headers)
	if created.ClientID == nil || *created.ClientID != client.ID || created.ClientName == nil || *created.ClientName != client.Name || created.Currency != "CNY" || created.Version != 1 {
		t.Fatalf("created financial entry = %#v", created)
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/financial-entries", []byte(body), headers)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" || decodeFinancialEntryResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("idempotent replay = %d headers=%v body=%s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/financial-entries", []byte(strings.Replace(body, "12800", "12801", 1)), headers)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/financial-entries/"+created.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"1"` {
		t.Fatalf("detail = %d ETag=%q body=%s", detail.Code, detail.Header().Get("ETag"), detail.Body.String())
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/financial-entries/"+created.ID, []byte(`{"amount_minor":13000}`), map[string]string{"If-Match": `"2"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale update = %d: %s", stale.Code, stale.Body.String())
	}
	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/financial-entries/"+created.ID, []byte(`{"amount_minor":13000,"status":"pending","category":"顾问费"}`), map[string]string{"If-Match": `"1"`})
	updated := decodeFinancialEntryResponse(t, updatedRecorder.Body.Bytes())
	if updatedRecorder.Code != http.StatusOK || updated.AmountMinor != 13000 || updated.Status != "pending" || updated.Version != 2 {
		t.Fatalf("updated = %d %#v", updatedRecorder.Code, updated)
	}
	missingConfirm := performRequest(router, http.MethodDelete, "/api/v1/financial-entries/"+created.ID, []byte(`{"reason":"重复记录"}`), map[string]string{"If-Match": `"2"`})
	if missingConfirm.Code != http.StatusUnprocessableEntity || responseErrorCode(t, missingConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("missing confirm = %d: %s", missingConfirm.Code, missingConfirm.Body.String())
	}
	voidRecorder := performRequest(router, http.MethodDelete, "/api/v1/financial-entries/"+created.ID+"?confirm=true", []byte(`{"reason":"重复记录"}`), map[string]string{"If-Match": `"2"`})
	voided := decodeFinancialEntryResponse(t, voidRecorder.Body.Bytes())
	if voidRecorder.Code != http.StatusOK || voided.Status != "voided" || voided.VoidReason == nil || voided.Version != 3 {
		t.Fatalf("voided = %d %#v", voidRecorder.Code, voided)
	}
	defaultList := performRequest(router, http.MethodGet, "/api/v1/financial-entries", nil, nil)
	var list struct {
		Data []financialEntryResponse `json:"data"`
		Meta pageMeta                 `json:"meta"`
	}
	_ = json.Unmarshal(defaultList.Body.Bytes(), &list)
	if list.Meta.Total != 0 || len(list.Data) != 0 {
		t.Fatalf("default list included voided entry: %s", defaultList.Body.String())
	}
	withVoided := performRequest(router, http.MethodGet, "/api/v1/financial-entries?include_voided=true", nil, nil)
	_ = json.Unmarshal(withVoided.Body.Bytes(), &list)
	if list.Meta.Total != 1 || len(list.Data) != 1 || list.Data[0].Status != "voided" {
		t.Fatalf("voided list = %s", withVoided.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='financial_entry' AND aggregate_id=?", 3, created.ID)
}

func TestFinancialEntryValidationFiltersStatsAndCSV(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	clientA := createClientForTest(t, router, `{"name":"客户 A"}`, nil)
	clientB := createClientForTest(t, router, `{"name":"客户 B"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"客户 A 项目","client_id":%q}`, clientA.ID), nil)

	for _, testCase := range []struct{ name, body, code string }{
		{"zero amount", `{"type":"income","amount_minor":0,"currency":"CNY","occurred_on":"2026-08-20","category":"x"}`, "VALIDATION_ERROR"},
		{"invalid status", `{"type":"income","amount_minor":1,"currency":"CNY","occurred_on":"2026-08-20","status":"voided","category":"x"}`, "VALIDATION_ERROR"},
		{"invalid currency", `{"type":"income","amount_minor":1,"currency":"人民币","occurred_on":"2026-08-20","category":"x"}`, "VALIDATION_ERROR"},
		{"invalid date", `{"type":"income","amount_minor":1,"currency":"CNY","occurred_on":"2026-02-30","category":"x"}`, "VALIDATION_ERROR"},
		{"project client mismatch", fmt.Sprintf(`{"type":"income","amount_minor":1,"currency":"CNY","occurred_on":"2026-08-20","category":"x","project_id":%q,"client_id":%q}`, project.ID, clientB.ID), "PROJECT_CLIENT_MISMATCH"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/financial-entries", []byte(testCase.body), nil)
			if recorder.Code != http.StatusUnprocessableEntity || responseErrorCode(t, recorder.Body.Bytes()) != testCase.code {
				t.Fatalf("validation = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	createFinancialEntryForTest(t, router, fmt.Sprintf(`{"type":"income","amount_minor":10000,"currency":"CNY","occurred_on":"2026-08-05","status":"confirmed","category":"项目款","client_id":%q}`, clientA.ID), nil)
	createFinancialEntryForTest(t, router, `{"type":"income","amount_minor":5000,"currency":"CNY","occurred_on":"2026-08-06","status":"pending","category":"待到账"}`, nil)
	createFinancialEntryForTest(t, router, `{"type":"expense","amount_minor":2500,"currency":"CNY","occurred_on":"2026-08-07","status":"confirmed","category":"软件"}`, nil)
	createFinancialEntryForTest(t, router, `{"type":"income","amount_minor":8000,"currency":"USD","occurred_on":"2026-08-08","status":"confirmed","category":"Overseas"}`, nil)

	filtered := performRequest(router, http.MethodGet, "/api/v1/financial-entries?type=income&status=confirmed&currency=CNY&date_from=2026-08-01&date_to=2026-08-31&sort=-amount_minor", nil, nil)
	var list struct {
		Data []financialEntryResponse `json:"data"`
		Meta pageMeta                 `json:"meta"`
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &list); err != nil || filtered.Code != http.StatusOK || list.Meta.Total != 1 || list.Data[0].AmountMinor != 10000 {
		t.Fatalf("filtered list = %d %s err=%v", filtered.Code, filtered.Body.String(), err)
	}
	invalidFilter := performRequest(router, http.MethodGet, "/api/v1/financial-entries?date_from=2026-09-01&date_to=2026-08-01", nil, nil)
	if invalidFilter.Code != http.StatusBadRequest || responseErrorCode(t, invalidFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid filter = %d: %s", invalidFilter.Code, invalidFilter.Body.String())
	}

	stats := performRequest(router, http.MethodGet, "/api/v1/stats/income?currency=CNY&date_from=2026-08-01&date_to=2026-08-31", nil, nil)
	var statsEnvelope struct {
		Data incomeStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(stats.Body.Bytes(), &statsEnvelope); err != nil || stats.Code != http.StatusOK {
		t.Fatalf("stats = %d: %s err=%v", stats.Code, stats.Body.String(), err)
	}
	if statsEnvelope.Data.ConfirmedIncomeMinor != 10000 || statsEnvelope.Data.ConfirmedExpenseMinor != 2500 || statsEnvelope.Data.PendingIncomeMinor != 5000 || statsEnvelope.Data.NetCashFlowMinor != 7500 || statsEnvelope.Data.AverageIncomeMinor != 10000 || statsEnvelope.Data.EntryCount != 3 {
		t.Fatalf("stats data = %#v", statsEnvelope.Data)
	}

	missingExportConfirm := performRequest(router, http.MethodGet, "/api/v1/financial-entries/export.csv", nil, nil)
	if missingExportConfirm.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing export confirm = %d", missingExportConfirm.Code)
	}
	exported := performRequest(router, http.MethodGet, "/api/v1/financial-entries/export.csv?confirm=true&currency=CNY&date_from=2026-08-01&date_to=2026-08-31", nil, nil)
	if exported.Code != http.StatusOK || !strings.Contains(exported.Header().Get("Content-Type"), "text/csv") || !strings.Contains(exported.Body.String(), "项目款") || strings.Contains(exported.Body.String(), "Overseas") {
		t.Fatalf("csv export = %d headers=%v body=%s", exported.Code, exported.Header(), exported.Body.String())
	}
	if !strings.Contains(exported.Header().Get("Content-Disposition"), "financial-entries-") {
		t.Fatalf("csv filename = %q", exported.Header().Get("Content-Disposition"))
	}
}
