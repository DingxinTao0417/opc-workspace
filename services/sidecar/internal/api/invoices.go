package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	createInvoiceEndpoint  = "POST /api/v1/invoices"
	maxInvoiceAmountMinor  = int64(9_000_000_000_000_000)
	invoicePaymentCategory = "发票回款"
)

var validInvoiceStatuses = map[string]struct{}{
	"draft": {}, "sent": {}, "viewed": {}, "paid": {}, "overdue": {},
}

type createInvoiceRequest struct {
	ClientID    string  `json:"client_id"`
	ProjectID   *string `json:"project_id"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
	IssueDate   string  `json:"issue_date"`
	DueDate     string  `json:"due_date"`
	Notes       string  `json:"notes"`
}

type updateInvoiceRequest struct {
	ClientID    nullableStringPatch `json:"client_id"`
	ProjectID   nullableStringPatch `json:"project_id"`
	AmountMinor nullableInt64Patch  `json:"amount_minor"`
	Currency    nullableStringPatch `json:"currency"`
	IssueDate   nullableStringPatch `json:"issue_date"`
	DueDate     nullableStringPatch `json:"due_date"`
	Notes       nullableStringPatch `json:"notes"`
}

type transitionInvoiceRequest struct {
	Action   string  `json:"action"`
	PaidDate *string `json:"paid_date"`
}

type invoiceResponse struct {
	ID               string  `json:"id"`
	InvoiceNumber    string  `json:"invoice_number"`
	ClientID         string  `json:"client_id"`
	ClientName       string  `json:"client_name"`
	ProjectID        *string `json:"project_id"`
	ProjectName      *string `json:"project_name"`
	AmountMinor      int64   `json:"amount_minor"`
	Currency         string  `json:"currency"`
	Status           string  `json:"status"`
	IssueDate        string  `json:"issue_date"`
	DueDate          string  `json:"due_date"`
	PaidDate         *string `json:"paid_date"`
	Notes            string  `json:"notes"`
	FinancialEntryID *string `json:"financial_entry_id"`
	Version          int64   `json:"version"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type invoiceRow struct {
	models.Invoice   `gorm:"embedded"`
	ClientName       string  `gorm:"column:client_name"`
	ProjectName      *string `gorm:"column:project_name"`
	FinancialEntryID *string `gorm:"column:financial_entry_id"`
}

type invoiceFilters struct {
	Search    string
	Status    string
	Currency  string
	ClientID  string
	ProjectID string
	IssueFrom string
	IssueTo   string
	DueFrom   string
	DueTo     string
}

type invoiceRequestError struct {
	status  int
	code    string
	message string
}

func (err *invoiceRequestError) Error() string { return err.message }

func (a *API) listInvoices(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	filters, err := parseInvoiceFilters(c)
	if err != nil {
		writeInvoiceRequestError(c, err)
		return
	}

	var total int64
	var rows []invoiceRow
	invalidSort := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := applyInvoiceFilters(invoiceRowsQuery(tx.Table("invoices AS invoice")), filters)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyInvoiceSort(query, c.Query("sort"))
		if !valid {
			invalidSort = true
			return errors.New("invalid invoice sort")
		}
		return ordered.Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if invalidSort {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	items := make([]invoiceResponse, len(rows))
	for index := range rows {
		items[index] = invoiceResponseFromRow(rows[index])
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createInvoice(c *gin.Context) {
	var input createInvoiceRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	invoice, err := invoiceFromCreateRequest(input, a.options.Now())
	if err != nil {
		writeInvoiceRequestError(c, err)
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if key != "" {
		requestHash, err = invoiceCreateRequestHash(invoice)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	statusCode := http.StatusCreated
	replayed := false
	var response invoiceResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if key != "" {
			hit, status, err := replayInvoiceIdempotency(tx, key, createInvoiceEndpoint, requestHash, &response)
			if err != nil {
				return err
			}
			if hit {
				replayed = true
				statusCode = status
				return nil
			}
		}
		if err := validateInvoiceAssociations(tx, invoice.ClientID, invoice.ProjectID); err != nil {
			return err
		}
		number, err := nextInvoiceNumber(tx, invoice.IssueDate, invoice.CreatedAt)
		if err != nil {
			return err
		}
		invoice.InvoiceNumber = number
		if err := tx.Create(&invoice).Error; err != nil {
			return invoiceDatabaseError(err)
		}
		if err := recordInvoiceWorkflowEvent(tx, invoice.ID, "invoice_created", models.BuiltinOwnerActorID, nil, invoiceEventState(invoice), requestIDFromContext(c), invoice.CreatedAt); err != nil {
			return err
		}
		row, err := loadInvoiceRow(tx, invoice.ID)
		if err != nil {
			return err
		}
		response = invoiceResponseFromRow(row)
		return recordInvoiceIdempotency(tx, key, createInvoiceEndpoint, invoice.ID, requestHash, http.StatusCreated, response, invoice.CreatedAt)
	})
	if err != nil {
		if writeInvoiceRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getInvoice(c *gin.Context) {
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	row, err := loadInvoiceRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "INVOICE_NOT_FOUND", "Invoice not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := invoiceResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateInvoice(c *gin.Context) {
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateInvoiceRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	var response invoiceResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var invoice models.Invoice
		if err := tx.Where("id = ?", id).Take(&invoice).Error; err != nil {
			return err
		}
		if invoice.Version != expectedVersion {
			return invoiceVersionConflict()
		}
		if invoice.Status != "draft" {
			return newInvoiceRequestError(http.StatusConflict, "INVOICE_NOT_DRAFT", "Only draft invoices can be edited")
		}
		previous := invoiceEventState(invoice)
		updates, candidate, err := invoiceUpdates(invoice, input)
		if err != nil {
			return err
		}
		if err := validateInvoiceAssociations(tx, candidate.ClientID, candidate.ProjectID); err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		updates["updated_at"] = now
		result := tx.Model(&models.Invoice{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return invoiceDatabaseError(result.Error)
		}
		if result.RowsAffected != 1 {
			return invoiceVersionConflict()
		}
		if err := tx.Where("id = ?", id).Take(&invoice).Error; err != nil {
			return err
		}
		if err := recordInvoiceWorkflowEvent(tx, id, "invoice_updated", models.BuiltinOwnerActorID, previous, invoiceEventState(invoice), requestIDFromContext(c), now); err != nil {
			return err
		}
		row, err := loadInvoiceRow(tx, id)
		if err != nil {
			return err
		}
		response = invoiceResponseFromRow(row)
		return nil
	})
	if err != nil {
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
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteInvoice(c *gin.Context) {
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Deleting an invoice requires confirm=true")
		return
	}
	store := a.invoicePDFStore
	if store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
	}

	var movedPDF *trashedInvoicePDF
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var invoice models.Invoice
		if err := tx.Where("id = ?", id).Take(&invoice).Error; err != nil {
			return err
		}
		if invoice.Version != expectedVersion {
			return invoiceVersionConflict()
		}
		if invoice.Status != "draft" {
			return newInvoiceRequestError(http.StatusConflict, "INVOICE_NOT_DRAFT", "Only draft invoices can be deleted")
		}
		var entryCount int64
		if err := tx.Table("financial_entries").Where("invoice_id = ?", id).Count(&entryCount).Error; err != nil {
			return err
		}
		if entryCount > 0 {
			return newInvoiceRequestError(http.StatusConflict, "INVOICE_FINANCIAL_ENTRY_EXISTS", "An invoice linked to a financial entry cannot be deleted")
		}
		asset, exists, err := invoicePDFAssetExists(tx, id)
		if err != nil {
			return err
		}
		if exists {
			if store == nil {
				return newInvoiceRequestError(http.StatusServiceUnavailable, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
			}
			movedPDF, err = store.moveToTrash(asset.RelativePath, asset.ID)
			if err != nil {
				return newInvoiceRequestError(http.StatusInternalServerError, "INVOICE_PDF_STORAGE_ERROR", "The invoice PDF could not be prepared for deletion safely")
			}
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		if err := recordInvoiceWorkflowEvent(tx, id, "invoice_deleted", models.BuiltinOwnerActorID, invoiceEventState(invoice), nil, requestIDFromContext(c), now); err != nil {
			return err
		}
		result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.Invoice{})
		if result.Error != nil {
			return invoiceDatabaseError(result.Error)
		}
		if result.RowsAffected != 1 {
			return invoiceVersionConflict()
		}
		return nil
	})
	if err != nil {
		if store != nil {
			if restoreErr := store.restoreTrashed(movedPDF); restoreErr != nil && a.options.Logger != nil {
				a.options.Logger.Printf("invoice PDF delete compensation failed invoice_id=%s error=%v", id, restoreErr)
			}
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
	if store != nil {
		store.purgeTrashed(movedPDF)
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted_id": id}})
}

func (a *API) transitionInvoice(c *gin.Context) {
	id, ok := invoiceID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input transitionInvoiceRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	action := strings.TrimSpace(input.Action)
	paidDate, err := normalizeInvoiceTransitionInput(action, input.PaidDate)
	if err != nil {
		writeInvoiceRequestError(c, err)
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if key != "" {
		requestHash, err = invoiceTransitionRequestHash(expectedVersion, action, paidDate)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}
	endpoint := fmt.Sprintf("POST /api/v1/invoices/%s/transition", id)
	statusCode := http.StatusOK
	replayed := false
	var response invoiceResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if key != "" {
			hit, status, err := replayInvoiceIdempotency(tx, key, endpoint, requestHash, &response)
			if err != nil {
				return err
			}
			if hit {
				replayed = true
				statusCode = status
				return nil
			}
		}
		var invoice models.Invoice
		if err := tx.Where("id = ?", id).Take(&invoice).Error; err != nil {
			return err
		}
		if invoice.Version != expectedVersion {
			return invoiceVersionConflict()
		}
		target, eventAction, actorID, err := invoiceTransition(invoice, action, paidDate, a.options.Now())
		if err != nil {
			return err
		}
		previous := invoiceEventState(invoice)
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		var financialEntry *models.FinancialEntry
		if target == "paid" {
			entry, err := createInvoicePaymentEntry(tx, invoice, *paidDate, now, requestIDFromContext(c))
			if err != nil {
				return err
			}
			financialEntry = &entry
		}
		updates := map[string]any{
			"status": target, "version": gorm.Expr("version + 1"), "updated_at": now,
		}
		if target == "paid" {
			updates["paid_date"] = *paidDate
		}
		result := tx.Model(&models.Invoice{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return invoiceDatabaseError(result.Error)
		}
		if result.RowsAffected != 1 {
			return invoiceVersionConflict()
		}
		if err := tx.Where("id = ?", id).Take(&invoice).Error; err != nil {
			return err
		}
		if err := recordInvoiceWorkflowEvent(tx, id, eventAction, actorID, previous, invoiceEventState(invoice), requestIDFromContext(c), now); err != nil {
			return err
		}
		row, err := loadInvoiceRow(tx, id)
		if err != nil {
			return err
		}
		response = invoiceResponseFromRow(row)
		if financialEntry != nil && (response.FinancialEntryID == nil || *response.FinancialEntryID != financialEntry.ID) {
			return errors.New("paid invoice did not resolve its financial entry")
		}
		return recordInvoiceIdempotency(tx, key, endpoint, id, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
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
	setProjectETag(c, response.Version)
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(statusCode, gin.H{"data": response})
}

func parseInvoiceFilters(c *gin.Context) (invoiceFilters, error) {
	filters := invoiceFilters{Search: strings.TrimSpace(c.Query("q")), Status: strings.TrimSpace(c.Query("status"))}
	if utf8.RuneCountInString(filters.Search) > 200 {
		return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", "q cannot exceed 200 characters")
	}
	if filters.Status != "" {
		if _, ok := validInvoiceStatuses[filters.Status]; !ok {
			return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
		}
	}
	if raw := strings.TrimSpace(c.Query("currency")); raw != "" {
		currency, err := normalizeInvoiceCurrency(raw)
		if err != nil {
			return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", "currency filter must be a three-letter code")
		}
		filters.Currency = currency
	}
	for _, item := range []struct {
		name  string
		value *string
	}{{"client_id", &filters.ClientID}, {"project_id", &filters.ProjectID}} {
		raw := strings.TrimSpace(c.Query(item.name))
		if raw == "" {
			continue
		}
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", item.name+" filter must be a UUID")
		}
		*item.value = parsed.String()
	}
	filters.IssueFrom = strings.TrimSpace(c.Query("issue_from"))
	filters.IssueTo = strings.TrimSpace(c.Query("issue_to"))
	filters.DueFrom = strings.TrimSpace(c.Query("due_from"))
	filters.DueTo = strings.TrimSpace(c.Query("due_to"))
	for _, item := range []struct{ name, value string }{
		{"issue_from", filters.IssueFrom}, {"issue_to", filters.IssueTo}, {"due_from", filters.DueFrom}, {"due_to", filters.DueTo},
	} {
		if item.value != "" && !validDate(item.value) {
			return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", item.name+" must use YYYY-MM-DD")
		}
	}
	if filters.IssueFrom != "" && filters.IssueTo != "" && filters.IssueFrom > filters.IssueTo {
		return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", "issue_from cannot be after issue_to")
	}
	if filters.DueFrom != "" && filters.DueTo != "" && filters.DueFrom > filters.DueTo {
		return filters, newInvoiceRequestError(http.StatusBadRequest, "INVALID_FILTER", "due_from cannot be after due_to")
	}
	return filters, nil
}

func applyInvoiceFilters(query *gorm.DB, filters invoiceFilters) *gorm.DB {
	if filters.Search != "" {
		like := "%" + escapeLike(filters.Search) + "%"
		query = query.Where("(invoice.invoice_number LIKE ? ESCAPE '\\' OR clients.name LIKE ? ESCAPE '\\' OR projects.name LIKE ? ESCAPE '\\')", like, like, like)
	}
	if filters.Status != "" {
		query = query.Where("invoice.status = ?", filters.Status)
	}
	if filters.Currency != "" {
		query = query.Where("invoice.currency = ?", filters.Currency)
	}
	if filters.ClientID != "" {
		query = query.Where("invoice.client_id = ?", filters.ClientID)
	}
	if filters.ProjectID != "" {
		query = query.Where("invoice.project_id = ?", filters.ProjectID)
	}
	if filters.IssueFrom != "" {
		query = query.Where("invoice.issue_date >= ?", filters.IssueFrom)
	}
	if filters.IssueTo != "" {
		query = query.Where("invoice.issue_date <= ?", filters.IssueTo)
	}
	if filters.DueFrom != "" {
		query = query.Where("invoice.due_date >= ?", filters.DueFrom)
	}
	if filters.DueTo != "" {
		query = query.Where("invoice.due_date <= ?", filters.DueTo)
	}
	return query
}

func invoiceRowsQuery(query *gorm.DB) *gorm.DB {
	return query.Select(invoiceSelectColumns).
		Joins("JOIN clients ON clients.id = invoice.client_id").
		Joins("LEFT JOIN projects ON projects.id = invoice.project_id").
		Joins("LEFT JOIN financial_entries AS linked_entry ON linked_entry.invoice_id = invoice.id AND linked_entry.status <> 'voided'")
}

const invoiceSelectColumns = `
	invoice.id, invoice.invoice_number, invoice.client_id, invoice.project_id,
	invoice.amount_minor, invoice.currency, invoice.status, invoice.issue_date,
	invoice.due_date, invoice.paid_date, invoice.notes, invoice.version,
	invoice.created_at, invoice.updated_at, clients.name AS client_name,
	projects.name AS project_name, linked_entry.id AS financial_entry_id
`

func applyInvoiceSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.Order("invoice.issue_date DESC").Order("invoice.created_at DESC").Order("invoice.id ASC"), true
	}
	allowed := map[string]string{
		"invoice_number": "invoice.invoice_number", "client_name": "clients.name", "amount_minor": "invoice.amount_minor",
		"currency": "invoice.currency", "status": "invoice.status", "issue_date": "invoice.issue_date",
		"due_date": "invoice.due_date", "created_at": "invoice.created_at", "updated_at": "invoice.updated_at",
	}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		direction := "ASC"
		if strings.HasPrefix(field, "-") {
			direction = "DESC"
			field = strings.TrimPrefix(field, "-")
		}
		column, ok := allowed[field]
		if !ok {
			return query, false
		}
		query = query.Order(column + " " + direction)
	}
	return query.Order("invoice.id ASC"), true
}

func loadInvoiceRow(db *gorm.DB, id string) (invoiceRow, error) {
	var row invoiceRow
	err := invoiceRowsQuery(db.Table("invoices AS invoice")).Where("invoice.id = ?", id).Take(&row).Error
	return row, err
}

func invoiceResponseFromRow(row invoiceRow) invoiceResponse {
	return invoiceResponse{
		ID: row.ID, InvoiceNumber: row.InvoiceNumber, ClientID: row.ClientID, ClientName: row.ClientName,
		ProjectID: row.ProjectID, ProjectName: row.ProjectName, AmountMinor: row.AmountMinor, Currency: row.Currency,
		Status: row.Status, IssueDate: row.IssueDate, DueDate: row.DueDate, PaidDate: row.PaidDate, Notes: row.Notes,
		FinancialEntryID: row.FinancialEntryID, Version: row.Version,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

func invoiceFromCreateRequest(input createInvoiceRequest, now time.Time) (models.Invoice, error) {
	clientID, err := normalizeRequiredInvoiceUUID(input.ClientID, "client_id")
	if err != nil {
		return models.Invoice{}, err
	}
	projectID, err := normalizeOptionalInvoiceUUID(input.ProjectID, "project_id")
	if err != nil {
		return models.Invoice{}, err
	}
	if err := validateInvoiceAmount(input.AmountMinor); err != nil {
		return models.Invoice{}, err
	}
	currency, err := normalizeInvoiceCurrency(input.Currency)
	if err != nil {
		return models.Invoice{}, err
	}
	issueDate := strings.TrimSpace(input.IssueDate)
	dueDate := strings.TrimSpace(input.DueDate)
	if !validDate(issueDate) {
		return models.Invoice{}, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "issue_date must use YYYY-MM-DD")
	}
	if !validDate(dueDate) {
		return models.Invoice{}, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date must use YYYY-MM-DD")
	}
	if dueDate < issueDate {
		return models.Invoice{}, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date cannot be before issue_date")
	}
	if utf8.RuneCountInString(input.Notes) > 10_000 {
		return models.Invoice{}, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "notes cannot exceed 10000 characters")
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	return models.Invoice{
		ID: uuid.NewString(), ClientID: clientID, ProjectID: projectID, AmountMinor: input.AmountMinor,
		Currency: currency, Status: "draft", IssueDate: issueDate, DueDate: dueDate, Notes: input.Notes,
		Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}, nil
}

func invoiceUpdates(invoice models.Invoice, input updateInvoiceRequest) (map[string]any, models.Invoice, error) {
	updates := map[string]any{}
	candidate := invoice
	if input.ClientID.Set {
		if input.ClientID.Value == nil {
			return nil, candidate, invoiceFieldRequired("client_id")
		}
		value, err := normalizeRequiredInvoiceUUID(*input.ClientID.Value, "client_id")
		if err != nil {
			return nil, candidate, err
		}
		candidate.ClientID = value
		updates["client_id"] = value
	}
	if input.ProjectID.Set {
		value, err := normalizeOptionalInvoiceUUID(input.ProjectID.Value, "project_id")
		if err != nil {
			return nil, candidate, err
		}
		candidate.ProjectID = value
		updates["project_id"] = value
	}
	if input.AmountMinor.Set {
		if input.AmountMinor.Value == nil {
			return nil, candidate, invoiceFieldRequired("amount_minor")
		}
		if err := validateInvoiceAmount(*input.AmountMinor.Value); err != nil {
			return nil, candidate, err
		}
		candidate.AmountMinor = *input.AmountMinor.Value
		updates["amount_minor"] = candidate.AmountMinor
	}
	if input.Currency.Set {
		if input.Currency.Value == nil {
			return nil, candidate, invoiceFieldRequired("currency")
		}
		value, err := normalizeInvoiceCurrency(*input.Currency.Value)
		if err != nil {
			return nil, candidate, err
		}
		candidate.Currency = value
		updates["currency"] = value
	}
	if input.IssueDate.Set {
		if input.IssueDate.Value == nil {
			return nil, candidate, invoiceFieldRequired("issue_date")
		}
		value := strings.TrimSpace(*input.IssueDate.Value)
		if !validDate(value) {
			return nil, candidate, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "issue_date must use YYYY-MM-DD")
		}
		candidate.IssueDate = value
		updates["issue_date"] = value
	}
	if input.DueDate.Set {
		if input.DueDate.Value == nil {
			return nil, candidate, invoiceFieldRequired("due_date")
		}
		value := strings.TrimSpace(*input.DueDate.Value)
		if !validDate(value) {
			return nil, candidate, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date must use YYYY-MM-DD")
		}
		candidate.DueDate = value
		updates["due_date"] = value
	}
	if candidate.DueDate < candidate.IssueDate {
		return nil, candidate, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date cannot be before issue_date")
	}
	if input.Notes.Set {
		if input.Notes.Value == nil {
			return nil, candidate, invoiceFieldRequired("notes")
		}
		if utf8.RuneCountInString(*input.Notes.Value) > 10_000 {
			return nil, candidate, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "notes cannot exceed 10000 characters")
		}
		candidate.Notes = *input.Notes.Value
		updates["notes"] = candidate.Notes
	}
	if len(updates) == 0 {
		return nil, candidate, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable invoice field is required")
	}
	return updates, candidate, nil
}

func normalizeInvoiceTransitionInput(action string, rawPaidDate *string) (*string, error) {
	switch action {
	case "mark_sent", "mark_viewed", "mark_overdue":
		if rawPaidDate != nil {
			return nil, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "paid_date is only allowed for mark_paid")
		}
		return nil, nil
	case "mark_paid":
		if rawPaidDate == nil {
			return nil, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "paid_date is required for mark_paid")
		}
		value := strings.TrimSpace(*rawPaidDate)
		if !validDate(value) {
			return nil, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "paid_date must use YYYY-MM-DD")
		}
		return &value, nil
	default:
		return nil, newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "action must be mark_sent, mark_viewed, mark_paid, or mark_overdue")
	}
}

func invoiceTransition(invoice models.Invoice, action string, paidDate *string, now time.Time) (string, string, string, error) {
	invalid := func() (string, string, string, error) {
		return "", "", "", newInvoiceRequestError(http.StatusConflict, "INVALID_INVOICE_TRANSITION", fmt.Sprintf("action %q is not allowed while invoice status is %q", action, invoice.Status))
	}
	switch action {
	case "mark_sent":
		if invoice.Status != "draft" {
			return invalid()
		}
		return "sent", "invoice_sent", models.BuiltinOwnerActorID, nil
	case "mark_viewed":
		if invoice.Status != "sent" {
			return invalid()
		}
		return "viewed", "invoice_viewed", models.BuiltinOwnerActorID, nil
	case "mark_overdue":
		if invoice.Status != "sent" && invoice.Status != "viewed" {
			return invalid()
		}
		if invoice.DueDate >= now.Format("2006-01-02") {
			return "", "", "", newInvoiceRequestError(http.StatusConflict, "INVOICE_NOT_OVERDUE", "Invoice due date has not passed")
		}
		return "overdue", "invoice_overdue", models.BuiltinSystemActorID, nil
	case "mark_paid":
		if invoice.Status != "viewed" && invoice.Status != "overdue" {
			return invalid()
		}
		if paidDate == nil || *paidDate < invoice.IssueDate {
			return "", "", "", newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "paid_date cannot be before issue_date")
		}
		return "paid", "invoice_paid", models.BuiltinOwnerActorID, nil
	default:
		return invalid()
	}
}

func createInvoicePaymentEntry(tx *gorm.DB, invoice models.Invoice, paidDate, now, requestID string) (models.FinancialEntry, error) {
	var activeCount int64
	if err := tx.Table("financial_entries").Where("invoice_id = ? AND status <> 'voided'", invoice.ID).Count(&activeCount).Error; err != nil {
		return models.FinancialEntry{}, err
	}
	if activeCount > 0 {
		return models.FinancialEntry{}, newInvoiceRequestError(http.StatusConflict, "INVOICE_FINANCIAL_ENTRY_EXISTS", "Invoice already has an active financial entry")
	}
	entry := models.FinancialEntry{
		ID: uuid.NewString(), Type: "income", AmountMinor: invoice.AmountMinor, Currency: invoice.Currency,
		OccurredOn: paidDate, Status: "confirmed", Category: invoicePaymentCategory,
		ClientID: &invoice.ClientID, ProjectID: invoice.ProjectID, InvoiceID: &invoice.ID,
		Notes: "发票 " + invoice.InvoiceNumber + " 付款确认", CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&entry).Error; err != nil {
		if strings.Contains(err.Error(), "ux_financial_entries_active_invoice") || strings.Contains(err.Error(), "financial_entries.invoice_id") {
			return models.FinancialEntry{}, newInvoiceRequestError(http.StatusConflict, "INVOICE_FINANCIAL_ENTRY_EXISTS", "Invoice already has an active financial entry")
		}
		return models.FinancialEntry{}, err
	}
	if err := recordFinancialEntryWorkflowEvent(tx, entry, "financial_entry_created", nil, requestID); err != nil {
		return models.FinancialEntry{}, err
	}
	return entry, nil
}

func validateInvoiceAssociations(tx *gorm.DB, clientID string, projectID *string) error {
	var clientCount int64
	if err := tx.Table("clients").Where("id = ?", clientID).Count(&clientCount).Error; err != nil {
		return err
	}
	if clientCount == 0 {
		return newInvoiceRequestError(http.StatusUnprocessableEntity, "CLIENT_NOT_FOUND", "client_id does not reference an existing client")
	}
	if projectID == nil {
		return nil
	}
	var project struct {
		ClientID *string `gorm:"column:client_id"`
	}
	if err := tx.Table("projects").Select("client_id").Where("id = ?", *projectID).Take(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newInvoiceRequestError(http.StatusUnprocessableEntity, "PROJECT_NOT_FOUND", "project_id does not reference an existing project")
		}
		return err
	}
	if project.ClientID == nil || *project.ClientID != clientID {
		return newInvoiceRequestError(http.StatusUnprocessableEntity, "PROJECT_CLIENT_MISMATCH", "project_id belongs to a different client")
	}
	return nil
}

func nextInvoiceNumber(tx *gorm.DB, issueDate, now string) (string, error) {
	year, err := strconv.Atoi(issueDate[:4])
	if err != nil || year < 2000 || year > 9999 {
		return "", newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "issue_date year must be between 2000 and 9999")
	}
	prefix := fmt.Sprintf("INV-%04d-", year)
	var maxExisting int64
	if err := tx.Raw(`
		SELECT COALESCE(MAX(CAST(SUBSTR(invoice_number, ?) AS INTEGER)), 0)
		FROM invoices
		WHERE invoice_number GLOB ?
		  AND invoice_number NOT GLOB ?
	`, len(prefix)+1, prefix+"[0-9]*", prefix+"*[^0-9]*").Scan(&maxExisting).Error; err != nil {
		return "", err
	}
	if maxExisting >= math.MaxInt64-1 {
		return "", newInvoiceRequestError(http.StatusConflict, "INVOICE_NUMBER_EXHAUSTED", "Invoice number sequence is exhausted for this issue year")
	}
	if err := tx.Exec(`
		INSERT INTO invoice_number_sequences(year, last_value, updated_at)
		VALUES (?, 0, ?)
		ON CONFLICT(year) DO NOTHING
	`, year, now).Error; err != nil {
		return "", err
	}
	if err := tx.Exec(`
		UPDATE invoice_number_sequences
		SET last_value = CASE WHEN last_value < ? THEN ? ELSE last_value END,
		    updated_at = ?
		WHERE year = ?
	`, maxExisting, maxExisting, now, year).Error; err != nil {
		return "", err
	}
	if err := tx.Exec(`
		UPDATE invoice_number_sequences
		SET last_value = last_value + 1, updated_at = ?
		WHERE year = ?
	`, now, year).Error; err != nil {
		return "", err
	}
	var next int64
	if err := tx.Table("invoice_number_sequences").Select("last_value").Where("year = ?", year).Scan(&next).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", prefix, next), nil
}

func invoiceCreateRequestHash(invoice models.Invoice) (string, error) {
	payload := struct {
		ClientID    string  `json:"client_id"`
		ProjectID   *string `json:"project_id"`
		AmountMinor int64   `json:"amount_minor"`
		Currency    string  `json:"currency"`
		IssueDate   string  `json:"issue_date"`
		DueDate     string  `json:"due_date"`
		Notes       string  `json:"notes"`
	}{invoice.ClientID, invoice.ProjectID, invoice.AmountMinor, invoice.Currency, invoice.IssueDate, invoice.DueDate, invoice.Notes}
	return invoiceRequestDigest(payload)
}

func invoiceTransitionRequestHash(expectedVersion int64, action string, paidDate *string) (string, error) {
	payload := struct {
		ExpectedVersion int64   `json:"expected_version"`
		Action          string  `json:"action"`
		PaidDate        *string `json:"paid_date"`
	}{expectedVersion, action, paidDate}
	return invoiceRequestDigest(payload)
}

func invoiceRequestDigest(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "v1:" + fmt.Sprintf("%x", digest), nil
}

func replayInvoiceIdempotency(tx *gorm.DB, key, endpoint, requestHash string, response *invoiceResponse) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	var existing models.IdempotencyKey
	err := tx.Where("key = ? AND endpoint = ?", key, endpoint).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
	}
	if *existing.RequestHash != requestHash {
		return false, 0, newInvoiceRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different invoice request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, err
	}
	return true, *existing.ResponseStatus, nil
}

func recordInvoiceIdempotency(tx *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response invoiceResponse, createdAt string) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	body := string(encoded)
	return tx.Create(&models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID, RequestHash: &requestHash,
		ResponseBody: &body, ResponseStatus: &status, CreatedAt: createdAt,
	}).Error
}

func invoiceEventState(invoice models.Invoice) map[string]any {
	return map[string]any{
		"invoice_number": invoice.InvoiceNumber, "client_id": invoice.ClientID, "project_id": invoice.ProjectID,
		"amount_minor": invoice.AmountMinor, "currency": invoice.Currency, "status": invoice.Status,
		"issue_date": invoice.IssueDate, "due_date": invoice.DueDate, "paid_date": invoice.PaidDate,
		"notes": invoice.Notes, "version": invoice.Version,
	}
}

func recordInvoiceWorkflowEvent(tx *gorm.DB, invoiceID, action, actorID string, previous, current map[string]any, requestID, createdAt string) error {
	var previousText, currentText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		value := string(encoded)
		previousText = &value
	}
	if current != nil {
		encoded, err := json.Marshal(current)
		if err != nil {
			return err
		}
		value := string(encoded)
		currentText = &value
	}
	var requestIDValue *string
	if requestID != "" {
		requestIDValue = &requestID
	}
	sequence := 1
	return tx.Create(&models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "invoice", AggregateID: invoiceID, Action: action,
		ActorID: &actorID, RequestID: requestIDValue, CommandSeq: &sequence,
		PreviousJSON: previousText, CurrentJSON: currentText, CreatedAt: createdAt,
	}).Error
}

func normalizeRequiredInvoiceUUID(raw, field string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := uuid.Parse(value)
	if err != nil || value == "" {
		return "", newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", field+" must be a UUID")
	}
	return parsed.String(), nil
}

func normalizeOptionalInvoiceUUID(raw *string, field string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value, err := normalizeRequiredInvoiceUUID(*raw, field)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func validateInvoiceAmount(value int64) error {
	if value < 1 || value > maxInvoiceAmountMinor {
		return newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "amount_minor must be between 1 and 9000000000000000")
	}
	return nil
}

func normalizeInvoiceCurrency(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if len(value) != 3 {
		return "", newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "currency must be a three-letter code")
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return "", newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "currency must be a three-letter code")
		}
	}
	return value, nil
}

func invoiceID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_INVOICE_ID", "Invoice id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func invoiceFieldRequired(field string) error {
	return newInvoiceRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", field+" cannot be null")
}

func newInvoiceRequestError(status int, code, message string) error {
	return &invoiceRequestError{status: status, code: code, message: message}
}

func invoiceVersionConflict() error {
	return newInvoiceRequestError(http.StatusConflict, "VERSION_CONFLICT", "Invoice has changed; reload it before retrying")
}

func invoiceDatabaseError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "UNIQUE constraint failed: invoices.invoice_number"):
		return newInvoiceRequestError(http.StatusConflict, "INVOICE_NUMBER_CONFLICT", "Invoice number already exists")
	case strings.Contains(message, "INVOICE_PROJECT_CLIENT_MISMATCH"):
		return newInvoiceRequestError(http.StatusUnprocessableEntity, "PROJECT_CLIENT_MISMATCH", "project_id belongs to a different client")
	case strings.Contains(message, "INVALID_INVOICE_STATUS_TRANSITION"):
		return newInvoiceRequestError(http.StatusConflict, "INVALID_INVOICE_TRANSITION", "Invoice status transition is not allowed")
	default:
		return err
	}
}

func writeInvoiceRequestError(c *gin.Context, err error) bool {
	var requestError *invoiceRequestError
	if !errors.As(err, &requestError) {
		return false
	}
	writeError(c, requestError.status, requestError.code, requestError.message)
	return true
}
