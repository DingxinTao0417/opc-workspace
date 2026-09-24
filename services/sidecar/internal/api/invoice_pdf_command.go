package api

import (
	"errors"
	"io"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The caller holds store.mu BEFORE opening its transaction through finish.
// Files and SQLite are not one atomic resource: keep both file identities until
// the complete outer transaction (including approval/idempotency) has finished.
type invoicePDFChange struct {
	asset    models.InvoicePDFAsset
	movedOld *trashedInvoicePDF
}

func stageInvoicePDF(store *invoicePDFStore, invoice invoiceRow, generatedAt time.Time) (*invoicePDFChange, error) {
	if store == nil {
		return nil, newInvoiceRequestError(503, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
	}
	staged, err := store.stage(invoice.ID, func(destination io.Writer) error {
		return renderInvoicePDF(destination, invoice, generatedAt)
	})
	if err != nil {
		return nil, newInvoiceRequestError(500, "INVOICE_PDF_STORAGE_ERROR", "The invoice PDF could not be staged safely")
	}
	defer store.discardStaged(staged)
	if err := store.commit(staged); err != nil {
		return nil, newInvoiceRequestError(500, "INVOICE_PDF_STORAGE_ERROR", "The invoice PDF could not be stored safely")
	}
	now := generatedAt.UTC().Format(time.RFC3339Nano)
	return &invoicePDFChange{asset: models.InvoicePDFAsset{
		ID: staged.assetID, InvoiceID: invoice.ID, FileName: invoicePDFFilename(invoice.InvoiceNumber, invoice.ID),
		RelativePath: staged.relative, MimeType: invoicePDFMimeType,
		SizeBytes: staged.sizeBytes, SHA256: staged.sha256,
		GeneratedFromVersion: invoice.Version, GeneratedAt: now,
		IntegrityStatus: "verified", IntegrityCheckedAt: now,
	}}, nil
}

func replaceInvoicePDFInTransaction(tx *gorm.DB, store *invoicePDFStore, change *invoicePDFChange) error {
	asset := change.asset
	current, err := loadInvoiceRow(tx, asset.InvoiceID)
	if err != nil {
		return err
	}
	if current.Version != asset.GeneratedFromVersion {
		return invoiceVersionConflict()
	}
	old, exists, err := invoicePDFAssetExists(tx, asset.InvoiceID)
	if err != nil {
		return err
	}
	if !exists {
		return tx.Create(&asset).Error
	}
	change.movedOld, err = store.moveToTrash(old.RelativePath, old.ID)
	if err != nil {
		return newInvoiceRequestError(500, "INVOICE_PDF_STORAGE_ERROR", "The previous invoice PDF could not be replaced safely")
	}
	result := tx.Model(&models.InvoicePDFAsset{}).Where("invoice_id = ? AND id = ?", asset.InvoiceID, old.ID).Updates(map[string]any{
		"id": asset.ID, "file_name": asset.FileName, "relative_path": asset.RelativePath,
		"mime_type": asset.MimeType, "size_bytes": asset.SizeBytes, "sha256": asset.SHA256,
		"generated_from_version": asset.GeneratedFromVersion, "generated_at": asset.GeneratedAt,
		"integrity_status": asset.IntegrityStatus, "integrity_checked_at": asset.IntegrityCheckedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("invoice PDF asset changed during replacement")
	}
	return nil
}

func (a *API) finishInvoicePDFChange(change *invoicePDFChange, transactionErr error) {
	if change == nil {
		return
	}
	store := a.invoicePDFStore
	if transactionErr == nil {
		store.purgeTrashed(change.movedOld)
		return
	}
	if err := store.remove(change.asset.RelativePath, change.asset.ID); err != nil && a.options.Logger != nil {
		a.options.Logger.Printf("invoice PDF compensation remove failed invoice_id=%s error=%v", change.asset.InvoiceID, err)
	}
	if err := store.restoreTrashed(change.movedOld); err != nil && a.options.Logger != nil {
		a.options.Logger.Printf("invoice PDF compensation restore failed invoice_id=%s error=%v", change.asset.InvoiceID, err)
	}
}
