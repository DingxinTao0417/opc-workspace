package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	invoiceDueInboxSourceType         = "invoice_due"
	invoiceDueLeadDays                = 3
	invoiceDueDateLayout              = "2006-01-02"
	invoiceDueReconciliationBatchSize = 100
	invoicePaidInboxResolutionReason  = "invoice_paid"
)

type invoiceDueProjectionPayload struct {
	InvoiceID      string  `json:"invoice_id"`
	InvoiceNumber  string  `json:"invoice_number"`
	ClientID       string  `json:"client_id"`
	ClientName     string  `json:"client_name"`
	ProjectID      *string `json:"project_id"`
	ProjectName    *string `json:"project_name"`
	AmountMinor    int64   `json:"amount_minor"`
	Currency       string  `json:"currency"`
	DueDate        string  `json:"due_date"`
	DueState       string  `json:"due_state"`
	OccurrenceDate string  `json:"occurrence_date"`
	InvoiceVersion int64   `json:"invoice_version"`
	ProjectedAt    string  `json:"projected_at"`
	LeadDays       int     `json:"lead_days"`
}

func invoiceDueEventKey(id, dueState, dueDate, occurrenceDate string) string {
	if dueState == "overdue" {
		return fmt.Sprintf("invoice:%s:overdue:%s", id, occurrenceDate)
	}
	return fmt.Sprintf("invoice:%s:%s:%s", id, dueState, dueDate)
}

func invoiceDueState(dueDate, occurrenceDate string) (string, bool) {
	due, err := time.Parse(invoiceDueDateLayout, dueDate)
	if err != nil {
		return "", false
	}
	occurrence, err := time.Parse(invoiceDueDateLayout, occurrenceDate)
	if err != nil {
		return "", false
	}
	days := int(due.Sub(occurrence).Hours() / 24)
	switch {
	case days > 0 && days <= invoiceDueLeadDays:
		return "due_soon", true
	case days == 0:
		return "due", true
	case days < 0:
		return "overdue", true
	default:
		return "", false
	}
}

// projectDueInvoices performs bounded startup/runtime compensation. It creates
// at most one current occurrence per invoice and never replays missed overdue
// calendar days.
func (a *API) projectDueInvoices(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := a.options.Now()
	if err := a.reconcilePaidInvoiceDueSources(ctx, now); err != nil {
		return err
	}
	today := now.Format(invoiceDueDateLayout)
	var ids []string
	err := a.db.WithContext(ctx).Table("invoices").
		Where("status IN ('sent', 'viewed', 'overdue') AND due_date <= date(?, '+3 days')", today).
		Where(`NOT EXISTS (
			SELECT 1 FROM inbox_items
			WHERE source_entity_type = 'invoice_due'
			  AND source_entity_id = invoices.id
			  AND kind = 'event'
			  AND source_event_key = CASE
				WHEN invoices.due_date > ? THEN 'invoice:' || invoices.id || ':due_soon:' || invoices.due_date
				WHEN invoices.due_date = ? THEN 'invoice:' || invoices.id || ':due:' || invoices.due_date
				ELSE 'invoice:' || invoices.id || ':overdue:' || ?
			END
		)`, today, today, today).
		Order("due_date ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error
	if err != nil {
		return fmt.Errorf("list due Invoices: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.projectInvoiceDue(ctx, id, today, now); err != nil {
			return err
		}
	}
	return nil
}

// reconcilePaidInvoiceDueSources drains a bounded set of stale actionable
// projections left by older runtimes or restored/imported business facts. Each
// Invoice is coordinated in its own transaction so one Invoice can never be
// partially resolved.
func (a *API) reconcilePaidInvoiceDueSources(ctx context.Context, now time.Time) error {
	var ids []string
	if err := a.db.WithContext(ctx).Table("invoices").
		Where("status = 'paid'").
		Where(`EXISTS (
			SELECT 1 FROM inbox_items
			WHERE source_entity_type = ?
			  AND source_entity_id = invoices.id
			  AND kind = 'event'
			  AND status IN ('open', 'tracking')
		)`, invoiceDueInboxSourceType).
		Order("id ASC").Limit(invoiceDueReconciliationBatchSize).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list paid Invoices with active due sources: %w", err)
	}
	nowText := formatInboxTimestamp(now.UTC())
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var invoice models.Invoice
			if err := tx.Select("id", "status").Where("id = ?", id).Take(&invoice).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if invoice.Status != "paid" {
				return nil
			}
			return resolveInvoiceDueInboxSources(tx, id, "", nowText)
		})
		if err != nil {
			return fmt.Errorf("reconcile paid Invoice due sources: %w", err)
		}
	}
	return nil
}

func (a *API) projectInvoiceDue(ctx context.Context, id, today string, now time.Time) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadInvoiceRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if row.Status == "draft" || row.Status == "paid" {
			return nil
		}
		dueState, due := invoiceDueState(row.DueDate, today)
		if !due {
			return nil
		}
		createdAt := formatInboxTimestamp(now.UTC())
		if dueState == "overdue" && (row.Status == "sent" || row.Status == "viewed") {
			previous := invoiceEventState(row.Invoice)
			result := tx.Model(&models.Invoice{}).
				Where("id = ? AND version = ? AND status IN ('sent', 'viewed')", row.ID, row.Version).
				Updates(map[string]any{
					"status": "overdue", "version": gorm.Expr("version + 1"), "updated_at": createdAt,
				})
			if result.Error != nil {
				return invoiceDatabaseError(result.Error)
			}
			if result.RowsAffected != 1 {
				return invoiceVersionConflict()
			}
			if err := tx.Where("id = ?", row.ID).Take(&row.Invoice).Error; err != nil {
				return err
			}
			event, err := recordInvoiceWorkflowEventWithID(
				tx, row.ID, "invoice_overdue", models.BuiltinSystemActorID,
				previous, invoiceEventState(row.Invoice), "", createdAt,
			)
			if err != nil {
				return err
			}
			a.executeInvoiceOverdueAutomationsSafely(tx, event.ID, row.Invoice, createdAt)
		}
		if dueState == "overdue" && row.Status != "overdue" {
			return nil
		}

		key := invoiceDueEventKey(row.ID, dueState, row.DueDate, today)
		var existing models.InboxItem
		err = tx.First(&existing, "source_event_key = ?", key).Error
		if err == nil {
			return validateInvoiceDueReplay(existing, row.ID)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		payload := invoiceDueProjectionPayload{
			InvoiceID: row.ID, InvoiceNumber: row.InvoiceNumber,
			ClientID: row.ClientID, ClientName: row.ClientName,
			ProjectID: row.ProjectID, ProjectName: row.ProjectName,
			AmountMinor: row.AmountMinor, Currency: row.Currency,
			DueDate: row.DueDate, DueState: dueState, OccurrenceDate: today,
			InvoiceVersion: row.Version, ProjectedAt: createdAt, LeadDays: invoiceDueLeadDays,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		sourceID := row.ID
		dueAt, err := invoiceDueTimestamp(row.DueDate, now.Location())
		if err != nil {
			return err
		}
		inbox := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: invoiceDueTitle(row.InvoiceNumber, dueState),
			Summary: invoiceDueSummary(row, dueState), SourceEntityType: invoiceDueInboxSourceType,
			SourceEntityID: &sourceID, SourceEventKey: &key, Priority: invoiceDuePriority(dueState),
			Status: "open", ResolutionPolicy: "manual", DueAt: &dueAt,
			PayloadJSON: string(payloadJSON), Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&inbox)
		if result.Error != nil {
			return fmt.Errorf("create Invoice due Inbox Item: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			if err := tx.First(&existing, "source_event_key = ?", key).Error; err != nil {
				return err
			}
			return validateInvoiceDueReplay(existing, row.ID)
		}
		return recordInboxWorkflowEventAs(
			tx, inbox.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(inbox, ""), "", createdAt,
		)
	})
}

func validateInvoiceDueReplay(existing models.InboxItem, invoiceID string) error {
	if existing.Kind != "event" || existing.SourceEntityType != invoiceDueInboxSourceType ||
		existing.SourceEntityID == nil || *existing.SourceEntityID != invoiceID {
		return errors.New("Invoice due source_event_key belongs to an incompatible Inbox Item")
	}
	return nil
}

// resolveInvoiceDueInboxSources archives every still-actionable due projection
// when the underlying Invoice is paid. The projections remain auditable, while
// payment and source resolution commit as one business transaction.
func resolveInvoiceDueInboxSources(tx *gorm.DB, invoiceID, requestID, now string) error {
	var items []models.InboxItem
	if err := tx.Where(
		"source_entity_type = ? AND source_entity_id = ? AND kind = 'event' AND status IN ('open', 'tracking')",
		invoiceDueInboxSourceType,
		invoiceID,
	).Order("id ASC").Find(&items).Error; err != nil {
		return err
	}
	for _, current := range items {
		next := current
		ownerID := models.BuiltinOwnerActorID
		mode := "manual"
		reason := invoicePaidInboxResolutionReason
		next.Status = "resolved"
		next.SnoozedUntil = nil
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
		next.ResolvedByActorID = &ownerID
		next.ResolvedAt = &now
		next.ResolutionReason = &reason
		next.ResolutionMode = &mode
		next.Version++
		next.UpdatedAt = now
		result := tx.Model(&models.InboxItem{}).
			Where("id = ? AND version = ? AND status IN ('open', 'tracking')", current.ID, current.Version).
			Updates(inboxCommandUpdates(next))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return inboxVersionConflict()
		}
		if err := recordInboxWorkflowEvent(
			tx,
			current.ID,
			"source_resolved",
			inboxItemEventState(current, invoicePaidInboxResolutionReason),
			inboxItemEventState(next, invoicePaidInboxResolutionReason),
			requestID,
			now,
		); err != nil {
			return err
		}
	}
	return nil
}

func invoiceDueTitle(number, state string) string {
	prefix := "发票即将到期："
	if state == "due" {
		prefix = "发票今日到期："
	} else if state == "overdue" {
		prefix = "发票已逾期："
	}
	value := prefix + number
	if utf8.RuneCountInString(value) <= 200 {
		return value
	}
	runes := []rune(value)
	return string(runes[:200])
}

func invoiceDueSummary(row invoiceRow, state string) string {
	label := "到期日"
	if state == "overdue" {
		label = "原到期日"
	}
	return fmt.Sprintf("客户：%s · %s：%s · 金额：%s", row.ClientName, label, row.DueDate, formatInvoiceDueAmount(row.AmountMinor, row.Currency))
}

func formatInvoiceDueAmount(amountMinor int64, currency string) string {
	return fmt.Sprintf("%s %d.%02d", currency, amountMinor/100, amountMinor%100)
}

func invoiceDuePriority(state string) string {
	if state == "due_soon" {
		return "P2"
	}
	return "P1"
}

func invoiceDueTimestamp(dueDate string, location *time.Location) (string, error) {
	date, err := time.ParseInLocation(invoiceDueDateLayout, dueDate, location)
	if err != nil {
		return "", err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, location).
		UTC().Format(time.RFC3339Nano), nil
}
