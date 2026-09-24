package api

import (
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func validateInvoiceDeletion(tx *gorm.DB, invoice models.Invoice) error {
	if invoice.Status != "draft" {
		return newInvoiceRequestError(409, "INVOICE_NOT_DRAFT", "Only draft invoices can be deleted")
	}
	var count int64
	if err := tx.Table("financial_entries").Where("invoice_id = ?", invoice.ID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return newInvoiceRequestError(409, "INVOICE_FINANCIAL_ENTRY_EXISTS", "An invoice linked to a financial entry cannot be deleted")
	}
	return nil
}

// Caller locks the PDF store BEFORE opening the transaction, and keeps that
// lock until commit/purge or rollback/restore. Return moved even on failure:
// the caller owns file compensation together with its approval/idempotency audit.
func deleteInvoiceInTransaction(tx *gorm.DB, store *invoicePDFStore, id string, version int64, requestID, now string) (moved *trashedInvoicePDF, err error) {
	invoice, err := prepareInvoiceChange(tx, id, version)
	if err != nil {
		return nil, err
	}
	if err := validateInvoiceDeletion(tx, invoice); err != nil {
		return nil, err
	}
	asset, exists, err := invoicePDFAssetExists(tx, id)
	if err != nil {
		return nil, err
	}
	if exists {
		if store == nil {
			return nil, newInvoiceRequestError(503, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		}
		moved, err = store.moveToTrash(asset.RelativePath, asset.ID)
		if err != nil {
			return moved, newInvoiceRequestError(500, "INVOICE_PDF_STORAGE_ERROR", "The invoice PDF could not be prepared for deletion safely")
		}
	}
	if err = recordInvoiceWorkflowEvent(tx, id, "invoice_deleted", models.BuiltinOwnerActorID, invoiceEventState(invoice), nil, requestID, now); err != nil {
		return moved, err
	}
	result := tx.Where("id = ? AND version = ?", id, version).Delete(&models.Invoice{})
	if result.Error != nil {
		return moved, invoiceDatabaseError(result.Error)
	}
	if result.RowsAffected != 1 {
		return moved, invoiceVersionConflict()
	}
	return moved, nil
}

func (a *API) finishInvoiceDeletion(moved *trashedInvoicePDF, transactionErr error) {
	if a.invoicePDFStore == nil || moved == nil {
		return
	}
	if transactionErr == nil {
		a.invoicePDFStore.purgeTrashed(moved)
	} else if err := a.invoicePDFStore.restoreTrashed(moved); err != nil && a.options.Logger != nil {
		// Keep the trash entry for the existing startup reconciler to recover.
		a.options.Logger.Printf("invoice PDF delete compensation failed: %v", err)
	}
}
