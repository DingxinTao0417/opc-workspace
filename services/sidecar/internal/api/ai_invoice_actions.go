package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiInvoiceActions = []string{"invoice.create", "invoice.update", "invoice.mark_sent", "invoice.mark_viewed", "invoice.mark_paid", "invoice.mark_overdue", "invoice.delete", "invoice.generate_pdf"}

func addAIInvoiceActionSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	for _, name := range aiInvoiceActions {
		action["enum"] = append(action["enum"].([]any), name)
	}
	properties["invoice_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Actual invoice ID from workspace_finance; existing invoices also require expected_version"}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Invoice proposals require finance + invoice_actions, NOT finance_actions/work/actions. Create requires client_id, amount_minor, currency, issue_date, due_date; optional project_id and full user-supplied notes. Update those fields only while draft. No manual invoice number/status. mark_sent/viewed/overdue, delete and generate_pdf take empty changes; mark_paid requires actual paid_date only (viewed/overdue invoice, full amount received, never intent). All require separate HUMAN invoice consent. delete requires additional HUMAN deletion consent: draft only, no linked ledger, permanently removes invoice and its stored PDF, no undo, approval/audit history retained. mark_overdue requires past due_date and can trigger already-enabled local automation tasks. generate_pdf requires separate HUMAN PDF consent; renders the full invoice locally, replaces existing PDF with no undo, no invoice state change. Human card/download binds the actual asset; no bytes/paths to model. No external sending/payment or CSV export."
	f := changes["properties"].(map[string]any)
	f["issue_date"] = map[string]any{"type": "string", "description": "Explicit invoice issue date YYYY-MM-DD; create year 2000-9999"}
	// Shared fields must retain Task timestamps/clearing and Project dates.
	f["due_date"].(map[string]any)["description"] = "Task: RFC3339 timestamp with explicit time zone. Project: YYYY-MM-DD. Null clears Task/Project only. Invoice: required YYYY-MM-DD >= issue_date, never null."
	f["client_id"].(map[string]any)["description"] = "Project customer association and followup/activity require clients scope. Ledger optional customer uses finance+finance_actions; Invoice required non-null customer uses finance+invoice_actions. Use actual UUIDs; financial grants do not grant customer-profile reads."
	f["paid_date"] = map[string]any{"type": "string", "description": "Actual full payment date YYYY-MM-DD, between issue_date and today; ask user if unknown"}
}

func parseAIInvoiceAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	known := false
	for _, name := range aiInvoiceActions {
		if input.Action == name {
			known = true
		}
	}
	if !known {
		return input, errors.New("unsupported invoice action")
	}
	for key := range raw {
		if key != "action" && key != "invoice_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("invoice actions only accept their invoice target")
		}
	}
	creating, updating := input.Action == "invoice.create", input.Action == "invoice.update"
	if creating {
		if _, ok := raw["invoice_id"]; ok {
			return input, errors.New("create has no invoice target")
		}
		if _, ok := raw["expected_version"]; ok {
			return input, errors.New("create has no invoice version")
		}
	} else if id, err := uuid.Parse(input.InvoiceID); err != nil || id.String() != input.InvoiceID || input.ExpectedVersion < 1 {
		return input, errors.New("read actual invoice ID/version first")
	}
	canonical := map[string]any{}
	for key, value := range fields {
		if !creating && !updating && (input.Action != "invoice.mark_paid" || key != "paid_date") {
			return input, errors.New("invoice status commands only accept paid_date for mark_paid")
		}
		if (creating || updating) && key == "paid_date" {
			return input, errors.New("paid_date requires mark_paid")
		}
		if key == "amount_minor" {
			var n int64
			if json.Unmarshal(value, &n) != nil || validateInvoiceAmount(n) != nil {
				return input, errors.New("invalid invoice amount_minor")
			}
			canonical[key] = n
			continue
		}
		if key == "project_id" && string(value) == "null" {
			canonical[key] = nil
			continue
		}
		var s string
		if string(value) == "null" || json.Unmarshal(value, &s) != nil {
			return input, errors.New("invalid invoice field type")
		}
		switch key {
		case "client_id", "project_id":
			if id, err := uuid.Parse(s); err != nil || id.String() != s {
				return input, errors.New("canonical invoice association ID required")
			}
		case "currency":
			if n, err := normalizeInvoiceCurrency(s); err != nil || s != n {
				return input, errors.New("explicit uppercase invoice currency required")
			}
		case "issue_date", "due_date", "paid_date":
			if !validDate(s) {
				return input, errors.New("invoice dates must be explicit YYYY-MM-DD")
			}
		case "notes":
			if utf8.RuneCountInString(s) > 10000 {
				return input, errors.New("invoice notes exceed 10000 characters")
			}
		default:
			return input, errors.New("unsupported invoice field")
		}
		canonical[key] = s
	}
	if creating {
		for _, key := range []string{"client_id", "amount_minor", "currency", "issue_date", "due_date"} {
			if _, ok := canonical[key]; !ok {
				return input, errors.New("invoice create requires explicit " + key)
			}
		}
		if canonical["issue_date"].(string) < "2000-01-01" {
			return input, errors.New("invoice numbering requires an issue year between 2000 and 9999")
		}
	}
	if updating && len(canonical) == 0 {
		return input, errors.New("invoice update requires editable fields")
	}
	if input.Action == "invoice.mark_paid" && canonical["paid_date"] == nil {
		return input, errors.New("actual paid_date required")
	}
	input.Changes, _ = json.Marshal(canonical)
	return input, nil
}

func aiInvoiceFields(tx *gorm.DB, invoice models.Invoice) (map[string]any, error) {
	var client struct{ Name string }
	if err := tx.Table("clients").Select("name").Where("id=?", invoice.ClientID).Take(&client).Error; err != nil {
		return nil, err
	}
	var projectName *string
	if invoice.ProjectID != nil {
		var project struct{ Name string }
		if err := tx.Table("projects").Select("name").Where("id=?", *invoice.ProjectID).Take(&project).Error; err != nil {
			return nil, err
		}
		projectName = &project.Name
	}
	var number *string
	if invoice.InvoiceNumber != "" {
		number = &invoice.InvoiceNumber
	}
	return map[string]any{"invoice_number": number, "client_id": invoice.ClientID, "client_name": client.Name, "project_id": invoice.ProjectID, "project_name": projectName,
		"amount_minor": invoice.AmountMinor, "currency": invoice.Currency, "issue_date": invoice.IssueDate, "due_date": invoice.DueDate, "paid_date": invoice.PaidDate, "status": invoice.Status, "notes": invoice.Notes}, nil
}

func previewAIInvoiceAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var invoice models.Invoice
	var err error
	if input.Action == "invoice.create" {
		var request createInvoiceRequest
		_ = json.Unmarshal(input.Changes, &request)
		invoice, err = invoiceFromCreateRequest(request, now)
		if err == nil {
			err = validateInvoiceAssociations(tx, invoice.ClientID, invoice.ProjectID)
		}
		if err != nil {
			return p, err
		}
		p.Label = "新发票草稿（确认时生成编号）"
	} else {
		invoice, err = prepareInvoiceChange(tx, input.InvoiceID, input.ExpectedVersion)
		if err != nil {
			return p, err
		}
		p.Label = invoice.InvoiceNumber
		p.Before, err = aiInvoiceFields(tx, invoice)
		if err != nil {
			return p, err
		}
		if input.Action == "invoice.delete" || input.Action == "invoice.generate_pdf" {
			if input.Action == "invoice.delete" {
				if err := validateInvoiceDeletion(tx, invoice); err != nil {
					return p, err
				}
			}
			asset, exists, err := invoicePDFAssetExists(tx, invoice.ID)
			if err != nil {
				return p, err
			}
			// Bind stable file identity, not transient integrity-check timestamps.
			// These values stay on the human card; never expose paths or bytes.
			for _, key := range []string{"pdf_asset_id", "pdf_file_name", "pdf_size_bytes", "pdf_sha256", "pdf_generated_from_version", "pdf_generated_at"} {
				p.Before[key] = nil
			}
			if exists {
				p.Before["pdf_asset_id"], p.Before["pdf_file_name"] = asset.ID, asset.FileName
				p.Before["pdf_size_bytes"], p.Before["pdf_sha256"] = asset.SizeBytes, asset.SHA256
				p.Before["pdf_generated_from_version"], p.Before["pdf_generated_at"] = asset.GeneratedFromVersion, asset.GeneratedAt
			}
			p.After = map[string]any{"invoice_deleted": true, "pdf_removed": exists}
			if input.Action == "invoice.generate_pdf" {
				p.After = map[string]any{"pdf_generated": true, "pdf_replaced": exists}
			}
			return p, nil
		}
		if input.Action == "invoice.update" {
			if invoice.Status != "draft" {
				return p, newInvoiceRequestError(409, "INVOICE_NOT_DRAFT", "Only draft invoices can be edited")
			}
			var request updateInvoiceRequest
			_ = json.Unmarshal(input.Changes, &request)
			_, invoice, err = invoiceUpdates(invoice, request)
			if err == nil {
				err = validateInvoiceAssociations(tx, invoice.ClientID, invoice.ProjectID)
			}
		} else {
			var request transitionInvoiceRequest
			_ = json.Unmarshal(input.Changes, &request)
			invoice.Status, _, _, err = invoiceTransition(invoice, strings.TrimPrefix(input.Action, "invoice."), request.PaidDate, now)
			if input.Action == "invoice.mark_paid" {
				invoice.PaidDate = request.PaidDate
			}
		}
		if err != nil {
			return p, err
		}
	}
	p.After, err = aiInvoiceFields(tx, invoice)
	return p, err
}

func executeAIInvoiceAction(tx *gorm.DB, input aiWorkspaceAction, requestID string, clock time.Time) (aiActionResult, error) {
	// Dates follow the same local calendar as native Invoice commands. Only
	// persisted timestamps are normalized to UTC, never the validation clock.
	now := clock.UTC().Format(time.RFC3339Nano)
	var err error
	var out invoiceResponse
	switch input.Action {
	case "invoice.create":
		var request createInvoiceRequest
		_ = json.Unmarshal(input.Changes, &request)
		invoice, e := invoiceFromCreateRequest(request, clock)
		if e != nil {
			return aiActionResult{}, e
		}
		out, err = createInvoiceInTransaction(tx, invoice, requestID)
	case "invoice.update":
		var request updateInvoiceRequest
		_ = json.Unmarshal(input.Changes, &request)
		out, err = updateInvoiceInTransaction(tx, input.InvoiceID, input.ExpectedVersion, request, requestID, now)
	default:
		var request transitionInvoiceRequest
		_ = json.Unmarshal(input.Changes, &request)
		out, err = transitionInvoiceInTransaction(tx, input.InvoiceID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "invoice."), request.PaidDate, clock, requestID)
	}
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
