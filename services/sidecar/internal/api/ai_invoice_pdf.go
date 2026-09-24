package api

import (
	"errors"
	"strings"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// This namespace cannot be reached through an HTTP Idempotency-Key endpoint.
// Historical results must outlive later replacement/deletion of the live PDF.
const aiInvoicePDFReceiptEndpoint = "AI invoice.generate_pdf"

type aiInvoicePDFResult struct {
	AssetID string `json:"asset_id"`
	invoicePDFResponse
}

func (a *API) generateAIInvoicePDF(tx *gorm.DB, proposal models.AIActionProposal, action aiWorkspaceAction, clock time.Time) (*invoicePDFChange, error) {
	invoice, err := loadInvoiceRow(tx, action.InvoiceID)
	if err != nil {
		return nil, err
	}
	change, err := stageInvoicePDF(a.invoicePDFStore, invoice, clock.UTC())
	if err != nil {
		return change, err
	}
	if err := replaceInvoicePDFInTransaction(tx, a.invoicePDFStore, change); err != nil {
		return change, err
	}
	asset := change.asset
	// Same outer transaction as the business asset and immutable approval.
	err = recordInvoicePDFIdempotency(tx, proposal.ID, aiInvoicePDFReceiptEndpoint, asset.ID, proposal.Fingerprint, 201, invoicePDFResponseFromAsset(asset), asset.GeneratedAt)
	return change, err
}

func readAIInvoicePDFResult(tx *gorm.DB, out aiActionResponse) (*aiInvoicePDFResult, error) {
	var snapshot invoicePDFResponse
	hit, status, id, err := replayInvoicePDFIdempotency(tx, out.ID, aiInvoicePDFReceiptEndpoint, out.Fingerprint, &snapshot)
	if err != nil {
		return nil, err
	}
	_, timestampErr := time.Parse(time.RFC3339Nano, snapshot.GeneratedAt)
	if !hit || status != 201 || out.ResultID == nil || out.ResultVersion == nil || id != *out.ResultID ||
		!canonicalInvoicePDFUUID(id) || snapshot.InvoiceID != out.Action.InvoiceID || snapshot.GeneratedFromVersion != out.Action.ExpectedVersion || snapshot.GeneratedFromVersion != *out.ResultVersion ||
		snapshot.MimeType != invoicePDFMimeType || snapshot.SizeBytes <= 0 || snapshot.SizeBytes > maxInvoicePDFBytes ||
		!validInvoicePDFHash(snapshot.SHA256) || snapshot.IntegrityStatus != "verified" || timestampErr != nil || snapshot.IntegrityCheckedAt != snapshot.GeneratedAt ||
		snapshot.FileName != invoicePDFFilename(out.Preview.Label, out.Action.InvoiceID) || strings.ContainsAny(snapshot.FileName, "/\\\x00") {
		return nil, errors.New("invalid stored AI invoice PDF receipt")
	}
	// No live-asset lookup: this is generation-time metadata, not a statement
	// that the file is still current, present or uncorrupted. Download rechecks it.
	return &aiInvoicePDFResult{AssetID: id, invoicePDFResponse: snapshot}, nil
}

func validInvoicePDFHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
