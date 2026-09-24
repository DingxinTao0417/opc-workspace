package api

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiFinanceExportFilters struct {
	Currency  string `json:"currency"`
	DateFrom  string `json:"date_from"`
	DateTo    string `json:"date_to"`
	EntryType string `json:"entry_type"`
	Status    string `json:"status"`
	Category  string `json:"category,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

func addAIFinanceExportSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "finance.export_csv")
	changes := properties["changes"].(map[string]any)["properties"].(map[string]any)
	var schema map[string]any
	_ = json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"required":["currency","date_from","date_to","entry_type","status"],"description":"Only finance.export_csv with finance+finance_exports. Changes contains ONLY export_filters, no target/version. Require explicit currency and 1-366 inclusive dates (occurred_on), entry_type all/income/expense and status active (excludes voided)/all/pending/confirmed/voided. Ask if unknown. Optional exact category/client/project filters. All matching rows, fixed date-desc/created-desc/ID-asc order, max 10000 rows/16 MiB. Human preview binds content hash; changed data requires new approval. Includes notes and association names in HUMAN CSV ONLY. Approval is NOT download; never claim saved or sent. No model CSV bytes, no arbitrary path.","properties":{"currency":{"type":"string","pattern":"^[A-Z]{3}$"},"date_from":{"type":"string","format":"date"},"date_to":{"type":"string","format":"date"},"entry_type":{"type":"string","enum":["all","income","expense"]},"status":{"type":"string","enum":["active","all","pending","confirmed","voided"]},"category":{"type":"string","minLength":1,"maxLength":80},"client_id":{"type":"string","format":"uuid"},"project_id":{"type":"string","format":"uuid"}}}`), &schema)
	changes["export_filters"] = schema
}

func parseAIFinanceExport(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if len(raw) != 2 || raw["action"] == nil || raw["changes"] == nil || len(fields) != 1 || fields["export_filters"] == nil {
		return input, errors.New("CSV export only accepts changes.export_filters; no target, version or consent")
	}
	var f aiFinanceExportFilters
	if err := decodeStrictToolArguments(fields["export_filters"], &f); err != nil {
		return input, err
	}
	var values map[string]json.RawMessage
	_ = json.Unmarshal(fields["export_filters"], &values)
	for _, v := range values {
		if string(v) == "null" {
			return input, errors.New("export filters cannot be null")
		}
	}
	from, e1 := time.Parse("2006-01-02", f.DateFrom)
	to, e2 := time.Parse("2006-01-02", f.DateTo)
	currency, err := normalizeFinancialCurrency(f.Currency)
	if err != nil || currency != f.Currency || e1 != nil || e2 != nil || to.Before(from) || to.Sub(from) > 365*24*time.Hour {
		return input, errors.New("CSV requires explicit currency and 1-366 inclusive dates")
	}
	if f.EntryType != "all" && f.EntryType != "income" && f.EntryType != "expense" {
		return input, errors.New("explicit entry_type required")
	}
	if f.Status != "active" && f.Status != "all" && f.Status != "pending" && f.Status != "confirmed" && f.Status != "voided" {
		return input, errors.New("explicit export status required")
	}
	if v, ok := values["category"]; ok && (len(v) == 0 || strings.TrimSpace(f.Category) == "" || utf8.RuneCountInString(f.Category) > 80) {
		return input, errors.New("invalid exact category")
	}
	for _, v := range []struct{ key, value string }{{"client_id", f.ClientID}, {"project_id", f.ProjectID}} {
		if _, ok := values[v.key]; ok {
			id, e := uuid.Parse(v.value)
			if e != nil || id.String() != v.value {
				return input, errors.New("canonical association ID required")
			}
		}
	}
	input.Changes, _ = json.Marshal(map[string]any{"export_filters": f})
	return input, nil
}

func financialExportFilters(input aiWorkspaceAction) aiFinanceExportFilters {
	var body struct {
		Filters aiFinanceExportFilters `json:"export_filters"`
	}
	_ = json.Unmarshal(input.Changes, &body)
	return body.Filters
}

func previewAIFinanceExport(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, financialCSV, error) {
	f := financialExportFilters(input)
	filters := financialEntryFilters{Currency: f.Currency, DateFrom: f.DateFrom, DateTo: f.DateTo, EntryType: f.EntryType, Status: f.Status, Category: f.Category, ClientID: f.ClientID, ProjectID: f.ProjectID, IncludeVoided: f.Status == "all"}
	if filters.EntryType == "all" {
		filters.EntryType = ""
	}
	if filters.Status == "active" || filters.Status == "all" {
		filters.Status = ""
	}
	file, err := readFinancialCSV(tx, filters, "")
	if err != nil {
		return aiActionPreview{}, file, err
	}
	p := aiActionPreview{Label: "财务 CSV · " + f.Currency + " · " + f.DateFrom + " 至 " + f.DateTo, Before: map[string]any{}, After: map[string]any{
		"currency": f.Currency, "date_from": f.DateFrom, "date_to": f.DateTo, "entry_type": f.EntryType, "export_status": f.Status, "category": f.Category, "client_id": aiNullableString(f.ClientID), "project_id": aiNullableString(f.ProjectID),
		"row_count": file.Rows, "size_bytes": len(file.Data), "sha256": file.SHA256, "csv_columns": financialCSVColumns, "export_sort": "occurred_on DESC, created_at DESC, id ASC",
	}}
	return p, file, nil
}

// Downloads are human-only authenticated requests, never a Harness tool. No CSV
// body is persisted; a changed dataset is rejected, not silently substituted.
func (a *API) downloadAIFinanceExport(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := uuid.Parse(c.Param("id"))
	query, e := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || id.String() != c.Param("id") || e != nil || len(query) != 1 || len(query["fingerprint"]) != 1 || (len(query.Get("fingerprint")) != 64 || strings.Trim(query.Get("fingerprint"), "0123456789abcdef") != "") {
		writeError(c, 422, "AI_EXPORT_LOCATION_INVALID", "Use the exact approved export link")
		return
	}
	var file financialCSV
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var row models.AIActionProposal
		if e := tx.First(&row, "id=?", id.String()).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return newFinancialEntryRequestError(404, "AI_ACTION_NOT_FOUND", "Export approval not found")
			}
			return e
		}
		if row.Fingerprint != query.Get("fingerprint") {
			return newFinancialEntryRequestError(409, "AI_ACTION_CHANGED", "Use the exact export approval")
		}
		input, e := parseAIWorkspaceAction([]byte(row.ActionJSON))
		if e != nil {
			return e
		}
		if input.Action != "finance.export_csv" || row.Status != "confirmed" {
			return newFinancialEntryRequestError(409, "AI_EXPORT_NOT_APPROVED", "Confirm this export before downloading")
		}
		preview, current, e := previewAIFinanceExport(tx, input)
		if e != nil {
			return e
		}
		encoded, _ := json.Marshal(preview)
		if string(encoded) != row.PreviewJSON {
			return newFinancialEntryRequestError(409, "AI_EXPORT_CHANGED", "Export data changed; request and review a new proposal")
		}
		file = current
		return nil
	})
	if err != nil {
		if !writeFinancialEntryRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Content-Disposition", `attachment; filename="financial-entries-`+id.String()+`.csv"`)
	c.Header("X-Financial-CSV-SHA256", file.SHA256)
	c.Data(200, "text/csv; charset=utf-8", file.Data)
}
