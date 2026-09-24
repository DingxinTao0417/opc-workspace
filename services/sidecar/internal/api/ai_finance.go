package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiFinanceSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view"],"properties":{"view":{"type":"string","enum":["entries","entry","invoices","invoice","summary"]},"id":{"type":"string","format":"uuid","description":"Detail only"},"currency":{"type":"string","pattern":"^[A-Z]{3}$","description":"Required for summary; optional list filter. No currency conversion."},"date_from":{"type":"string","format":"date"},"date_to":{"type":"string","format":"date","description":"Summary requires both explicit dates, 1-366 days. Entries use occurred_on; invoice lists use due_date; inclusive stored calendar dates, no timezone conversion."},"status":{"type":"string","description":"Lists only: pending/confirmed/voided for entries, draft/sent/viewed/paid/overdue for invoices"},"entry_type":{"type":"string","enum":["income","expense"],"description":"Entries only"},"category":{"type":"string","maxLength":80,"description":"Exact category filter for entries only"},"client_id":{"type":"string","format":"uuid","description":"Lists only; finance permits association IDs, not customer profiles"},"project_id":{"type":"string","format":"uuid","description":"Lists only"},"query":{"type":"string","maxLength":200,"description":"Invoices only: literal invoice number substring, not customer name or notes"},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

func (t *aiWorkspaceTool) finance(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("finance"); err != nil {
		return nil, err
	}
	var in struct {
		View      string `json:"view"`
		ID        string `json:"id"`
		Currency  string `json:"currency"`
		DateFrom  string `json:"date_from"`
		DateTo    string `json:"date_to"`
		Status    string `json:"status"`
		EntryType string `json:"entry_type"`
		Category  string `json:"category"`
		ClientID  string `json:"client_id"`
		ProjectID string `json:"project_id"`
		Query     string `json:"query"`
		Limit     *int   `json:"limit"`
		Offset    int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &in); err != nil {
		return nil, err
	}
	detail := in.View == "entry" || in.View == "invoice"
	list := in.View == "entries" || in.View == "invoices"
	if !detail && !list && in.View != "summary" {
		return nil, errors.New("unknown finance view")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		allowed := key == "view" || (detail && key == "id") || (!detail && (key == "currency" || key == "date_from" || key == "date_to")) || (list && (key == "status" || key == "client_id" || key == "project_id" || key == "limit" || key == "offset")) || (in.View == "entries" && (key == "entry_type" || key == "category")) || (in.View == "invoices" && key == "query")
		if !allowed || strings.TrimSpace(string(raw)) == "null" {
			return nil, errors.New("unexpected or null finance argument")
		}
		if key == "id" || key == "client_id" || key == "project_id" {
			var id string
			_ = json.Unmarshal(raw, &id)
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return nil, errors.New("finance identities must be canonical UUIDs")
			}
		}
		if key == "currency" {
			value, err := normalizeFinancialCurrency(in.Currency)
			if err != nil || value != in.Currency {
				return nil, errors.New("currency must be an explicit uppercase three-letter code")
			}
		}
		if (key == "date_from" && in.DateFrom == "") || (key == "date_to" && in.DateTo == "") {
			return nil, errors.New("dates cannot be empty")
		}
	}
	if detail && in.ID == "" {
		return nil, errors.New("detail requires actual id")
	}
	if err := validateFinancialDateRange(in.DateFrom, in.DateTo); err != nil {
		return nil, err
	}
	if _, ok := fields["status"]; ok {
		valid := validFinancialEntryStatuses
		if in.View == "invoices" {
			valid = validInvoiceStatuses
		}
		if _, ok := valid[in.Status]; !ok {
			return nil, errors.New("invalid finance status")
		}
	}
	if _, ok := fields["entry_type"]; ok {
		if _, valid := validFinancialEntryTypes[in.EntryType]; !valid {
			return nil, errors.New("invalid entry_type")
		}
	}
	if utf8.RuneCountInString(in.Category) > 80 || utf8.RuneCountInString(in.Query) > 200 {
		return nil, errors.New("finance filter too long")
	}
	result := map[string]any{"view": in.View, "server_now": t.api.options.Now().UTC().Format(time.RFC3339Nano), "semantics": map[string]any{"amounts": "integer_minor_units", "currency_conversion": false, "notes_and_contact_fields_included": false, "states": "owner_recorded_local_facts_not_external_payment_evidence", "pagination": "live_per_request_not_frozen"}}
	if in.View == "summary" {
		from, e1 := time.Parse("2006-01-02", in.DateFrom)
		to, e2 := time.Parse("2006-01-02", in.DateTo)
		if in.Currency == "" || e1 != nil || e2 != nil || to.Sub(from) > 365*24*time.Hour {
			return nil, errors.New("summary requires currency and explicit 1-366 inclusive stored calendar dates; ask if unknown")
		}
		stats, err := readIncomeStats(ctx, t.api.db, in.Currency, in.DateFrom, in.DateTo)
		if err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		result["stats"] = stats
		result["route"] = "/income?" + url.Values{"currency": {in.Currency}, "date_from": {in.DateFrom}, "date_to": {in.DateTo}}.Encode()
		result["summary_semantics"] = "voided excluded; net=confirmed income-expense; pending separate; average is integer-floor per confirmed income entry, not per customer; invoice amounts are not added again"
		return result, nil
	}
	limit, err := workspacePaging(in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	db := t.api.db.WithContext(ctx)
	items := make([]map[string]any, 0)
	if in.View == "entry" || in.View == "entries" {
		query := db.Model(&models.FinancialEntry{}).Select("id,type,amount_minor,currency,occurred_on,status,category,client_id,project_id,invoice_id,version,created_at,updated_at")
		if detail {
			query = query.Where("id=?", in.ID)
		} else {
			// Share filtering semantics, but deliberately never join names or select notes/audit data.
			query = applyFinancialEntryFilters(query.Table("financial_entries AS entry"), financialEntryFilters{EntryType: in.EntryType, Status: in.Status, Currency: in.Currency, Category: in.Category, ClientID: in.ClientID, ProjectID: in.ProjectID, DateFrom: in.DateFrom, DateTo: in.DateTo})
		}
		rows := []models.FinancialEntry{}
		if err := query.Order("occurred_on DESC,created_at DESC,id ASC").Offset(in.Offset).Limit(limit + 1).Find(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		for _, r := range rows {
			items = append(items, map[string]any{"id": r.ID, "route": "/income/" + r.ID, "type": r.Type, "amount_minor": r.AmountMinor, "currency": r.Currency, "occurred_on": r.OccurredOn, "status": r.Status, "category": r.Category, "client_id": r.ClientID, "project_id": r.ProjectID, "invoice_id": r.InvoiceID, "version": r.Version, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt})
		}
		result["date_basis"] = "occurred_on"
	} else {
		query := db.Model(&models.Invoice{}).Select("id,invoice_number,client_id,project_id,amount_minor,currency,status,issue_date,due_date,paid_date,version,created_at,updated_at")
		if detail {
			query = query.Where("id=?", in.ID)
		} else {
			for _, f := range []struct{ column, value string }{{"currency", in.Currency}, {"status", in.Status}, {"client_id", in.ClientID}, {"project_id", in.ProjectID}} {
				if f.value != "" {
					query = query.Where(f.column+"=?", f.value)
				}
			}
			if in.DateFrom != "" {
				query = query.Where("due_date>=?", in.DateFrom)
			}
			if in.DateTo != "" {
				query = query.Where("due_date<=?", in.DateTo)
			}
			if in.Query != "" {
				query = query.Where("instr(invoice_number,?)>0", in.Query)
			}
		}
		rows := []models.Invoice{}
		if err := query.Order("due_date ASC,id ASC").Offset(in.Offset).Limit(limit + 1).Find(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		for _, r := range rows {
			items = append(items, map[string]any{"id": r.ID, "route": "/invoices/" + r.ID, "invoice_number": r.InvoiceNumber, "client_id": r.ClientID, "project_id": r.ProjectID, "amount_minor": r.AmountMinor, "currency": r.Currency, "status": r.Status, "issue_date": r.IssueDate, "due_date": r.DueDate, "paid_date": r.PaidDate, "version": r.Version, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt})
		}
		result["date_basis"] = "due_date"
	}
	if detail {
		if len(items) != 1 {
			return nil, errors.New("financial record not found")
		}
		result["record"] = items[0]
		return result, nil
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var next *int
	if more && in.Offset+limit <= 1000 {
		value := in.Offset + limit
		next = &value
	}
	result["items"], result["has_more"], result["next_offset"], result["window_limited"], result["limit"], result["offset"] = items, more, next, more && next == nil, limit, in.Offset
	return result, nil
}
