package api

import (
	"errors"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The caller owns the transaction, including any approval and idempotency audit.
func createInvoiceInTransaction(tx *gorm.DB, invoice models.Invoice, requestID string) (invoiceResponse, error) {
	if err := validateInvoiceAssociations(tx, invoice.ClientID, invoice.ProjectID); err != nil {
		return invoiceResponse{}, err
	}
	number, err := nextInvoiceNumber(tx, invoice.IssueDate, invoice.CreatedAt)
	if err != nil {
		return invoiceResponse{}, err
	}
	invoice.InvoiceNumber = number
	if err := tx.Create(&invoice).Error; err != nil {
		return invoiceResponse{}, invoiceDatabaseError(err)
	}
	if err := recordInvoiceWorkflowEvent(tx, invoice.ID, "invoice_created", models.BuiltinOwnerActorID, nil, invoiceEventState(invoice), requestID, invoice.CreatedAt); err != nil {
		return invoiceResponse{}, err
	}
	row, err := loadInvoiceRow(tx, invoice.ID)
	return invoiceResponseFromRow(row), err
}

func prepareInvoiceChange(tx *gorm.DB, id string, version int64) (models.Invoice, error) {
	var invoice models.Invoice
	if err := tx.Where("id=?", id).Take(&invoice).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invoice, newInvoiceRequestError(404, "INVOICE_NOT_FOUND", "Invoice not found")
		}
		return invoice, err
	}
	if invoice.Version != version {
		return invoice, invoiceVersionConflict()
	}
	return invoice, nil
}

func updateInvoiceInTransaction(tx *gorm.DB, id string, version int64, input updateInvoiceRequest, requestID, now string) (invoiceResponse, error) {
	invoice, err := prepareInvoiceChange(tx, id, version)
	if err != nil {
		return invoiceResponse{}, err
	}
	if invoice.Status != "draft" {
		return invoiceResponse{}, newInvoiceRequestError(409, "INVOICE_NOT_DRAFT", "Only draft invoices can be edited")
	}
	updates, candidate, err := invoiceUpdates(invoice, input)
	if err != nil {
		return invoiceResponse{}, err
	}
	if err := validateInvoiceAssociations(tx, candidate.ClientID, candidate.ProjectID); err != nil {
		return invoiceResponse{}, err
	}
	previous := invoiceEventState(invoice)
	invoice, err = writeInvoiceChange(tx, invoice, updates, now)
	if err != nil {
		return invoiceResponse{}, err
	}
	if err := recordInvoiceWorkflowEvent(tx, id, "invoice_updated", models.BuiltinOwnerActorID, previous, invoiceEventState(invoice), requestID, now); err != nil {
		return invoiceResponse{}, err
	}
	row, err := loadInvoiceRow(tx, id)
	return invoiceResponseFromRow(row), err
}

func writeInvoiceChange(tx *gorm.DB, invoice models.Invoice, updates map[string]any, now string) (models.Invoice, error) {
	updates["version"], updates["updated_at"] = gorm.Expr("version + 1"), now
	result := tx.Model(&models.Invoice{}).Where("id=? AND version=?", invoice.ID, invoice.Version).Updates(updates)
	if result.Error != nil {
		return invoice, invoiceDatabaseError(result.Error)
	}
	if result.RowsAffected != 1 {
		return invoice, invoiceVersionConflict()
	}
	err := tx.Where("id=?", invoice.ID).Take(&invoice).Error
	return invoice, err
}

func transitionInvoiceInTransaction(tx *gorm.DB, id string, version int64, action string, paidDate *string, clock time.Time, requestID string) (invoiceResponse, error) {
	invoice, err := prepareInvoiceChange(tx, id, version)
	if err != nil {
		return invoiceResponse{}, err
	}
	target, eventAction, actorID, err := invoiceTransition(invoice, action, paidDate, clock)
	if err != nil {
		return invoiceResponse{}, err
	}
	previous, now := invoiceEventState(invoice), clock.UTC().Format(time.RFC3339Nano)
	var payment *models.FinancialEntry
	if target == "paid" {
		entry, err := createInvoicePaymentEntry(tx, invoice, *paidDate, now, requestID)
		if err != nil {
			return invoiceResponse{}, err
		}
		payment = &entry
	}
	updates := map[string]any{"status": target}
	if target == "paid" {
		updates["paid_date"] = *paidDate
	}
	invoice, err = writeInvoiceChange(tx, invoice, updates, now)
	if err != nil {
		return invoiceResponse{}, err
	}
	event, err := recordInvoiceWorkflowEventWithID(tx, id, eventAction, actorID, previous, invoiceEventState(invoice), requestID, now)
	if err != nil {
		return invoiceResponse{}, err
	}
	if target == "paid" {
		if err := resolveInvoiceDueInboxSources(tx, id, requestID, now); err != nil {
			return invoiceResponse{}, err
		}
	}
	if eventAction == "invoice_overdue" {
		if err := enqueueInvoiceOverdueAutomationDelivery(tx, event.ID, invoice, now); err != nil {
			return invoiceResponse{}, err
		}
	}
	row, err := loadInvoiceRow(tx, id)
	if err != nil {
		return invoiceResponse{}, err
	}
	response := invoiceResponseFromRow(row)
	if payment != nil && (response.FinancialEntryID == nil || *response.FinancialEntryID != payment.ID) {
		return invoiceResponse{}, errors.New("paid invoice did not resolve its financial entry")
	}
	return response, nil
}
