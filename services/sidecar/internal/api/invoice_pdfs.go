package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type invoicePDFResponse struct {
	InvoiceID            string `json:"invoice_id"`
	FileName             string `json:"file_name"`
	MimeType             string `json:"mime_type"`
	SizeBytes            int64  `json:"size_bytes"`
	SHA256               string `json:"sha256"`
	GeneratedFromVersion int64  `json:"generated_from_version"`
	GeneratedAt          string `json:"generated_at"`
	IntegrityStatus      string `json:"integrity_status"`
	IntegrityCheckedAt   string `json:"integrity_checked_at"`
}

func (a *API) generateInvoicePDF(c *gin.Context) {
	if a.invoicePDFStore == nil {
		writeError(c, http.StatusServiceUnavailable, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		return
	}
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(c, http.StatusPreconditionRequired, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required")
		return
	}
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/invoices/%s/generate-pdf", id)
	requestHash := invoicePDFRequestHash(expectedVersion)

	store := a.invoicePDFStore
	store.mu.Lock()
	defer store.mu.Unlock()

	var response invoicePDFResponse
	hit, status, replayAssetID, err := replayInvoicePDFIdempotency(a.db.WithContext(c.Request.Context()), idempotencyKey, endpoint, requestHash, &response)
	if err != nil {
		if writeInvoiceRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if hit {
		checkedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
		response, err = validateInvoicePDFReplay(a.db.WithContext(c.Request.Context()), store, id, replayAssetID, response, checkedAt)
		if err != nil {
			if writeInvoiceRequestError(c, err) {
				return
			}
			writeDatabaseError(c)
			return
		}
		c.Header("Idempotency-Replayed", "true")
		setProjectETag(c, response.GeneratedFromVersion)
		c.JSON(status, gin.H{"data": response})
		return
	}

	invoice, err := loadInvoiceRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeInvoicePDFLoadError(c, err)
		return
	}
	if invoice.Version != expectedVersion {
		writeInvoiceRequestError(c, invoiceVersionConflict())
		return
	}
	generatedAt := a.options.Now().UTC()
	staged, err := store.stage(id, func(destination io.Writer) error {
		return renderInvoicePDF(destination, invoice, generatedAt)
	})
	if err != nil {
		writeInvoicePDFStorageError(c, a, "stage", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			store.discardStaged(staged)
		}
	}()
	if err := store.commit(staged); err != nil {
		writeInvoicePDFStorageError(c, a, "commit", err)
		return
	}
	committed = true

	now := generatedAt.Format(time.RFC3339Nano)
	asset := models.InvoicePDFAsset{
		ID: staged.assetID, InvoiceID: id, FileName: invoicePDFFilename(invoice.InvoiceNumber, id),
		RelativePath: staged.relative, MimeType: invoicePDFMimeType,
		SizeBytes: staged.sizeBytes, SHA256: staged.sha256,
		GeneratedFromVersion: expectedVersion, GeneratedAt: now,
		IntegrityStatus: "verified", IntegrityCheckedAt: now,
	}
	response = invoicePDFResponseFromAsset(asset)
	var movedOld *trashedInvoicePDF
	replayedAfterRender := false
	replayedAssetID := ""
	statusCode := http.StatusCreated
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		hit, status, assetID, err := replayInvoicePDFIdempotency(tx, idempotencyKey, endpoint, requestHash, &response)
		if err != nil {
			return err
		}
		if hit {
			replayedAfterRender = true
			replayedAssetID = assetID
			statusCode = status
			return nil
		}
		current, err := loadInvoiceRow(tx, id)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return invoiceVersionConflict()
		}
		old, exists, err := invoicePDFAssetExists(tx, id)
		if err != nil {
			return err
		}
		if exists {
			movedOld, err = store.moveToTrash(old.RelativePath, old.ID)
			if err != nil {
				return newInvoiceRequestError(http.StatusInternalServerError, "INVOICE_PDF_STORAGE_ERROR", "The previous invoice PDF could not be replaced safely")
			}
			result := tx.Model(&models.InvoicePDFAsset{}).Where("invoice_id = ? AND id = ?", id, old.ID).Updates(map[string]any{
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
		} else if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		return recordInvoicePDFIdempotency(tx, idempotencyKey, endpoint, asset.ID, requestHash, http.StatusCreated, response, now)
	})
	if err != nil {
		if removeErr := store.remove(staged.relative, staged.assetID); removeErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("invoice PDF compensation remove failed invoice_id=%s error=%v", id, removeErr)
		}
		if restoreErr := store.restoreTrashed(movedOld); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("invoice PDF compensation restore failed invoice_id=%s error=%v", id, restoreErr)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "INVOICE_NOT_FOUND", "Invoice not found")
			return
		}
		if writeInvoiceRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayedAfterRender {
		_ = store.remove(staged.relative, staged.assetID)
		checkedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
		response, err = validateInvoicePDFReplay(a.db.WithContext(c.Request.Context()), store, id, replayedAssetID, response, checkedAt)
		if err != nil {
			if writeInvoiceRequestError(c, err) {
				return
			}
			writeDatabaseError(c)
			return
		}
		c.Header("Idempotency-Replayed", "true")
	} else {
		store.purgeTrashed(movedOld)
	}
	setProjectETag(c, response.GeneratedFromVersion)
	c.JSON(statusCode, gin.H{"data": response})
}

func validateInvoicePDFReplay(db *gorm.DB, store *invoicePDFStore, invoiceID, expectedAssetID string, snapshot invoicePDFResponse, checkedAt string) (invoicePDFResponse, error) {
	asset, err := loadInvoicePDFAsset(db, invoiceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invoicePDFResponse{}, newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "The invoice PDF previously created by this Idempotency-Key is no longer current; use a new key")
		}
		return invoicePDFResponse{}, err
	}
	current := invoicePDFResponseFromAsset(asset)
	if asset.ID != expectedAssetID || current.InvoiceID != snapshot.InvoiceID || current.FileName != snapshot.FileName || current.MimeType != snapshot.MimeType ||
		current.SizeBytes != snapshot.SizeBytes || current.SHA256 != snapshot.SHA256 ||
		current.GeneratedFromVersion != snapshot.GeneratedFromVersion || current.GeneratedAt != snapshot.GeneratedAt {
		return invoicePDFResponse{}, newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "The invoice PDF previously created by this Idempotency-Key is no longer current; use a new key")
	}
	status, verifyErr := store.verify(asset.RelativePath, asset.ID, asset.SizeBytes, asset.SHA256)
	if err := persistInvoicePDFIntegrity(db, invoiceID, asset.ID, status, checkedAt); err != nil {
		return invoicePDFResponse{}, err
	}
	current.IntegrityStatus = status
	current.IntegrityCheckedAt = checkedAt
	if verifyErr != nil || status != "verified" {
		return invoicePDFResponse{}, newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "The invoice PDF previously created by this Idempotency-Key is no longer available; use a new key")
	}
	return current, nil
}

func (a *API) getInvoicePDF(c *gin.Context) {
	if a.invoicePDFStore == nil {
		writeError(c, http.StatusServiceUnavailable, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		return
	}
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	store := a.invoicePDFStore
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := loadInvoiceRow(a.db.WithContext(c.Request.Context()), id); err != nil {
		writeInvoicePDFLoadError(c, err)
		return
	}
	asset, err := loadInvoicePDFAsset(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeInvoicePDFAssetLoadError(c, err)
		return
	}
	status, _ := store.verify(asset.RelativePath, asset.ID, asset.SizeBytes, asset.SHA256)
	checkedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	if err := persistInvoicePDFIntegrity(a.db.WithContext(c.Request.Context()), id, asset.ID, status, checkedAt); err != nil {
		writeDatabaseError(c)
		return
	}
	asset.IntegrityStatus = status
	asset.IntegrityCheckedAt = checkedAt
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": invoicePDFResponseFromAsset(asset)})
}

func (a *API) downloadInvoicePDF(c *gin.Context) {
	if a.invoicePDFStore == nil {
		writeError(c, http.StatusServiceUnavailable, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		return
	}
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	store := a.invoicePDFStore
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, err := loadInvoiceRow(a.db.WithContext(c.Request.Context()), id); err != nil {
		writeInvoicePDFLoadError(c, err)
		return
	}
	asset, err := loadInvoicePDFAsset(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeInvoicePDFAssetLoadError(c, err)
		return
	}
	file, verifyErr := store.openVerified(asset.RelativePath, asset.ID, asset.SizeBytes, asset.SHA256)
	var contents []byte
	if verifyErr == nil {
		defer file.Close()
		contents, verifyErr = readVerifiedInvoicePDFBytes(file, asset.SizeBytes, asset.SHA256)
	}
	status := "verified"
	if verifyErr != nil {
		status = "mismatch"
		if errors.Is(verifyErr, os.ErrNotExist) {
			status = "missing"
		}
	}
	checkedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	if err := persistInvoicePDFIntegrity(a.db.WithContext(c.Request.Context()), id, asset.ID, status, checkedAt); err != nil {
		writeDatabaseError(c)
		return
	}
	if verifyErr != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("invoice PDF integrity failure invoice_id=%s status=%s error=%v", id, status, verifyErr)
		}
		if status == "missing" {
			writeError(c, http.StatusConflict, "INVOICE_PDF_FILE_MISSING", "The generated invoice PDF file is missing; generate it again")
			return
		}
		writeError(c, http.StatusConflict, "INVOICE_PDF_INTEGRITY_MISMATCH", "The generated invoice PDF failed its integrity check; generate it again")
		return
	}
	c.Header("Content-Type", invoicePDFMimeType)
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": asset.FileName}))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Invoice-PDF-SHA256", asset.SHA256)
	c.Data(http.StatusOK, invoicePDFMimeType, contents)
}

func persistInvoicePDFIntegrity(db *gorm.DB, invoiceID, assetID, status, checkedAt string) error {
	result := db.Model(&models.InvoicePDFAsset{}).
		Where("invoice_id = ? AND id = ?", invoiceID, assetID).
		Updates(map[string]any{"integrity_status": status, "integrity_checked_at": checkedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("invoice PDF asset changed while its integrity was checked")
	}
	return nil
}

func readVerifiedInvoicePDFBytes(file *os.File, expectedSize int64, expectedHash string) ([]byte, error) {
	if expectedSize <= 0 || expectedSize > maxInvoicePDFBytes {
		return nil, errors.New("invoice PDF file size is invalid")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	contents, err := io.ReadAll(io.LimitReader(file, expectedSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) != expectedSize || len(contents) < 5 || string(contents[:5]) != "%PDF-" {
		return nil, errors.New("invoice PDF bytes do not match stored metadata")
	}
	digest := sha256.Sum256(contents)
	if fmt.Sprintf("%x", digest) != expectedHash {
		return nil, errors.New("invoice PDF checksum changed during download")
	}
	return contents, nil
}

func invoicePDFResponseFromAsset(asset models.InvoicePDFAsset) invoicePDFResponse {
	return invoicePDFResponse{
		InvoiceID: asset.InvoiceID, FileName: asset.FileName, MimeType: asset.MimeType,
		SizeBytes: asset.SizeBytes, SHA256: asset.SHA256,
		GeneratedFromVersion: asset.GeneratedFromVersion, GeneratedAt: normalizeTimestamp(asset.GeneratedAt),
		IntegrityStatus: asset.IntegrityStatus, IntegrityCheckedAt: normalizeTimestamp(asset.IntegrityCheckedAt),
	}
}

func invoicePDFRequestHash(expectedVersion int64) string {
	digest := sha256.Sum256([]byte("v1:invoice-pdf:" + strconv.FormatInt(expectedVersion, 10)))
	return "v1:" + fmt.Sprintf("%x", digest)
}

func replayInvoicePDFIdempotency(db *gorm.DB, key, endpoint, requestHash string, response *invoicePDFResponse) (bool, int, string, error) {
	var existing models.IdempotencyKey
	err := db.Where("key = ? AND endpoint = ?", key, endpoint).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, "", nil
	}
	if err != nil {
		return false, 0, "", err
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, "", newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
	}
	if *existing.RequestHash != requestHash {
		return false, 0, "", newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different invoice PDF request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, "", err
	}
	return true, *existing.ResponseStatus, existing.ResourceID, nil
}

func recordInvoicePDFIdempotency(db *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response invoicePDFResponse, createdAt string) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	body := string(encoded)
	return db.Create(&models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID, RequestHash: &requestHash,
		ResponseBody: &body, ResponseStatus: &status, CreatedAt: createdAt,
	}).Error
}

func invoicePDFFilename(invoiceNumber, invoiceID string) string {
	var result strings.Builder
	previousSeparator := false
	for _, character := range strings.TrimSpace(invoiceNumber) {
		allowed := unicode.Is(unicode.Latin, character) || unicode.IsDigit(character) || character == '-' || character == '_'
		if allowed {
			result.WriteRune(character)
			previousSeparator = false
			continue
		}
		if !previousSeparator {
			result.WriteByte('-')
			previousSeparator = true
		}
	}
	base := strings.Trim(result.String(), "-_")
	if base == "" {
		base = invoiceID
	}
	if len(base) > 150 {
		base = base[:150]
	}
	return "invoice-" + base + ".pdf"
}

func writeInvoicePDFLoadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "INVOICE_NOT_FOUND", "Invoice not found")
		return
	}
	writeDatabaseError(c)
}

func writeInvoicePDFAssetLoadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "INVOICE_PDF_NOT_FOUND", "Invoice PDF has not been generated")
		return
	}
	writeDatabaseError(c)
}

func writeInvoicePDFStorageError(c *gin.Context, api *API, operation string, err error) {
	if api.options.Logger != nil {
		api.options.Logger.Printf("invoice PDF storage failure operation=%s request_id=%s error=%v", operation, requestIDFromContext(c), err)
	}
	writeError(c, http.StatusInternalServerError, "INVOICE_PDF_STORAGE_ERROR", "The invoice PDF could not be stored safely")
}
