package api

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
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
	createFinancialEntryEndpoint = "POST /api/v1/financial-entries"
	maxFinancialAmountMinor      = int64(9_000_000_000_000_000)
)

var (
	validFinancialEntryTypes    = map[string]struct{}{"income": {}, "expense": {}}
	validFinancialEntryStatuses = map[string]struct{}{"pending": {}, "confirmed": {}, "voided": {}}
)

type createFinancialEntryRequest struct {
	Type        string  `json:"type"`
	AmountMinor int64   `json:"amount_minor"`
	Currency    string  `json:"currency"`
	OccurredOn  string  `json:"occurred_on"`
	Status      string  `json:"status"`
	Category    string  `json:"category"`
	ClientID    *string `json:"client_id"`
	ProjectID   *string `json:"project_id"`
	Notes       string  `json:"notes"`
}

type updateFinancialEntryRequest struct {
	Type        nullableStringPatch `json:"type"`
	AmountMinor nullableInt64Patch  `json:"amount_minor"`
	Currency    nullableStringPatch `json:"currency"`
	OccurredOn  nullableStringPatch `json:"occurred_on"`
	Status      nullableStringPatch `json:"status"`
	Category    nullableStringPatch `json:"category"`
	ClientID    nullableStringPatch `json:"client_id"`
	ProjectID   nullableStringPatch `json:"project_id"`
	Notes       nullableStringPatch `json:"notes"`
}

type voidFinancialEntryRequest struct {
	Reason string `json:"reason"`
}

type financialEntryResponse struct {
	ID               string  `json:"id"`
	Type             string  `json:"type"`
	AmountMinor      int64   `json:"amount_minor"`
	Currency         string  `json:"currency"`
	OccurredOn       string  `json:"occurred_on"`
	Status           string  `json:"status"`
	Category         string  `json:"category"`
	ClientID         *string `json:"client_id"`
	ClientName       *string `json:"client_name"`
	ProjectID        *string `json:"project_id"`
	ProjectName      *string `json:"project_name"`
	InvoiceID        *string `json:"invoice_id"`
	InvoiceNumber    *string `json:"invoice_number"`
	Notes            string  `json:"notes"`
	CreatedByActorID string  `json:"created_by_actor_id"`
	VoidedAt         *string `json:"voided_at"`
	VoidReason       *string `json:"void_reason"`
	Version          int64   `json:"version"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type financialEntryRow struct {
	models.FinancialEntry `gorm:"embedded"`
	ClientName            *string `gorm:"column:client_name"`
	ProjectName           *string `gorm:"column:project_name"`
	InvoiceNumber         *string `gorm:"column:invoice_number"`
}

type financialEntryFilters struct {
	EntryType     string
	Status        string
	Currency      string
	Category      string
	ClientID      string
	ProjectID     string
	DateFrom      string
	DateTo        string
	IncludeVoided bool
}

type incomeStatsResponse struct {
	Currency              string `json:"currency"`
	DateFrom              string `json:"date_from"`
	DateTo                string `json:"date_to"`
	ConfirmedIncomeMinor  int64  `json:"confirmed_income_minor"`
	ConfirmedExpenseMinor int64  `json:"confirmed_expense_minor"`
	PendingIncomeMinor    int64  `json:"pending_income_minor"`
	PendingExpenseMinor   int64  `json:"pending_expense_minor"`
	NetCashFlowMinor      int64  `json:"net_cash_flow_minor"`
	ConfirmedIncomeCount  int64  `json:"confirmed_income_count"`
	AverageIncomeMinor    int64  `json:"average_income_minor"`
	EntryCount            int64  `json:"entry_count"`
}

type financialEntryRequestError struct {
	status  int
	code    string
	message string
}

func (err *financialEntryRequestError) Error() string { return err.message }

func (a *API) listFinancialEntries(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	filters, err := parseFinancialEntryFilters(c)
	if err != nil {
		writeFinancialEntryRequestError(c, err)
		return
	}
	var total int64
	var rows []financialEntryRow
	invalidSort := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := applyFinancialEntryFilters(tx.Table("financial_entries AS entry"), filters)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyFinancialEntrySort(financialEntryRowsQuery(query), c.Query("sort"))
		if !valid {
			invalidSort = true
			return errors.New("invalid financial entry sort")
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
	items := make([]financialEntryResponse, len(rows))
	for index := range rows {
		items[index] = financialEntryResponseFromRow(rows[index])
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createFinancialEntry(c *gin.Context) {
	var input createFinancialEntryRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	entry, err := financialEntryFromCreateRequest(input, a.options.Now())
	if err != nil {
		writeFinancialEntryRequestError(c, err)
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if idempotencyKey != "" {
		requestHash, err = financialEntryCreateRequestHash(entry)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}
	statusCode := http.StatusCreated
	replayed := false
	var response financialEntryResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createFinancialEntryEndpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newFinancialEntryRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
				}
				if *existing.RequestHash != requestHash {
					return newFinancialEntryRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different financial entry request")
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent financial entry response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if err := normalizeFinancialEntryAssociations(tx, &entry); err != nil {
			return err
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		if err := recordFinancialEntryWorkflowEvent(tx, entry, "financial_entry_created", nil, requestIDFromContext(c)); err != nil {
			return err
		}
		row, err := loadFinancialEntryRow(tx, entry.ID)
		if err != nil {
			return err
		}
		response = financialEntryResponseFromRow(row)
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return err
			}
			body := string(encoded)
			createdStatus := http.StatusCreated
			if err := tx.Create(&models.IdempotencyKey{Key: idempotencyKey, Endpoint: createFinancialEntryEndpoint, ResourceID: entry.ID, RequestHash: &requestHash, ResponseBody: &body, ResponseStatus: &createdStatus, CreatedAt: entry.CreatedAt}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if writeFinancialEntryRequestError(c, err) {
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

func (a *API) getFinancialEntry(c *gin.Context) {
	id, ok := financialEntryID(c)
	if !ok {
		return
	}
	row, err := loadFinancialEntryRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "FINANCIAL_ENTRY_NOT_FOUND", "Financial entry not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := financialEntryResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateFinancialEntry(c *gin.Context) {
	id, ok := financialEntryID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateFinancialEntryRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	var response financialEntryResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var entry models.FinancialEntry
		if err := tx.Where("id = ?", id).Take(&entry).Error; err != nil {
			return err
		}
		if entry.Version != expectedVersion {
			return financialEntryVersionConflict()
		}
		if entry.Status == "voided" {
			return newFinancialEntryRequestError(http.StatusConflict, "FINANCIAL_ENTRY_VOIDED", "A voided financial entry cannot be edited")
		}
		previous := financialEntryEventState(entry)
		updates, err := financialEntryUpdates(tx, entry, input)
		if err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		updates["updated_at"] = now
		result := tx.Model(&models.FinancialEntry{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return financialEntryVersionConflict()
		}
		if err := tx.Where("id = ?", id).Take(&entry).Error; err != nil {
			return err
		}
		if err := recordFinancialEntryWorkflowEvent(tx, entry, "financial_entry_updated", previous, requestIDFromContext(c)); err != nil {
			return err
		}
		row, err := loadFinancialEntryRow(tx, id)
		if err != nil {
			return err
		}
		response = financialEntryResponseFromRow(row)
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "FINANCIAL_ENTRY_NOT_FOUND", "Financial entry not found")
			return
		}
		if writeFinancialEntryRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) voidFinancialEntry(c *gin.Context) {
	id, ok := financialEntryID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Voiding a financial entry requires confirm=true")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input voidFinancialEntryRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason := strings.TrimSpace(input.Reason)
	if length := utf8.RuneCountInString(reason); length < 1 || length > 1000 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason must contain 1 to 1000 characters")
		return
	}
	var response financialEntryResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var entry models.FinancialEntry
		if err := tx.Where("id = ?", id).Take(&entry).Error; err != nil {
			return err
		}
		if entry.Version != expectedVersion {
			return financialEntryVersionConflict()
		}
		if entry.Status == "voided" {
			return newFinancialEntryRequestError(http.StatusConflict, "FINANCIAL_ENTRY_VOIDED", "Financial entry is already voided")
		}
		previous := financialEntryEventState(entry)
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.FinancialEntry{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"status": "voided", "voided_at": now, "voided_by_actor_id": models.BuiltinOwnerActorID,
			"void_reason": reason, "version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return financialEntryVersionConflict()
		}
		if err := tx.Where("id = ?", id).Take(&entry).Error; err != nil {
			return err
		}
		if err := recordFinancialEntryWorkflowEvent(tx, entry, "financial_entry_voided", previous, requestIDFromContext(c)); err != nil {
			return err
		}
		row, err := loadFinancialEntryRow(tx, id)
		if err != nil {
			return err
		}
		response = financialEntryResponseFromRow(row)
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "FINANCIAL_ENTRY_NOT_FOUND", "Financial entry not found")
			return
		}
		if writeFinancialEntryRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) getIncomeStats(c *gin.Context) {
	currency, err := normalizeFinancialCurrency(c.DefaultQuery("currency", "CNY"))
	if err != nil {
		writeFinancialEntryRequestError(c, err)
		return
	}
	now := a.options.Now()
	dateFrom := strings.TrimSpace(c.Query("date_from"))
	dateTo := strings.TrimSpace(c.Query("date_to"))
	if dateFrom == "" {
		dateFrom = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	}
	if dateTo == "" {
		dateTo = time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	}
	if err := validateFinancialDateRange(dateFrom, dateTo); err != nil {
		writeFinancialEntryRequestError(c, err)
		return
	}
	var row struct {
		ConfirmedIncome  int64 `gorm:"column:confirmed_income"`
		ConfirmedExpense int64 `gorm:"column:confirmed_expense"`
		PendingIncome    int64 `gorm:"column:pending_income"`
		PendingExpense   int64 `gorm:"column:pending_expense"`
		ConfirmedCount   int64 `gorm:"column:confirmed_count"`
		EntryCount       int64 `gorm:"column:entry_count"`
	}
	err = a.db.WithContext(c.Request.Context()).Table("financial_entries").Select(`
		COALESCE(SUM(CASE WHEN type = 'income' AND status = 'confirmed' THEN amount_minor ELSE 0 END), 0) AS confirmed_income,
		COALESCE(SUM(CASE WHEN type = 'expense' AND status = 'confirmed' THEN amount_minor ELSE 0 END), 0) AS confirmed_expense,
		COALESCE(SUM(CASE WHEN type = 'income' AND status = 'pending' THEN amount_minor ELSE 0 END), 0) AS pending_income,
		COALESCE(SUM(CASE WHEN type = 'expense' AND status = 'pending' THEN amount_minor ELSE 0 END), 0) AS pending_expense,
		COALESCE(SUM(CASE WHEN type = 'income' AND status = 'confirmed' THEN 1 ELSE 0 END), 0) AS confirmed_count,
		COUNT(*) AS entry_count
	`).Where("currency = ? AND occurred_on BETWEEN ? AND ? AND status <> 'voided'", currency, dateFrom, dateTo).Scan(&row).Error
	if err != nil {
		writeDatabaseError(c)
		return
	}
	average := int64(0)
	if row.ConfirmedCount > 0 {
		average = row.ConfirmedIncome / row.ConfirmedCount
	}
	response := incomeStatsResponse{
		Currency: currency, DateFrom: dateFrom, DateTo: dateTo,
		ConfirmedIncomeMinor: row.ConfirmedIncome, ConfirmedExpenseMinor: row.ConfirmedExpense,
		PendingIncomeMinor: row.PendingIncome, PendingExpenseMinor: row.PendingExpense,
		NetCashFlowMinor:     row.ConfirmedIncome - row.ConfirmedExpense,
		ConfirmedIncomeCount: row.ConfirmedCount, AverageIncomeMinor: average, EntryCount: row.EntryCount,
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) exportFinancialEntriesCSV(c *gin.Context) {
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "CSV export requires confirm=true")
		return
	}
	filters, err := parseFinancialEntryFilters(c)
	if err != nil {
		writeFinancialEntryRequestError(c, err)
		return
	}
	ordered, valid := applyFinancialEntrySort(financialEntryRowsQuery(applyFinancialEntryFilters(a.db.WithContext(c.Request.Context()).Table("financial_entries AS entry"), filters)), c.Query("sort"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	var rows []financialEntryRow
	if err := ordered.Limit(10_001).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	if len(rows) > 10_000 {
		writeError(c, http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "CSV export is limited to 10000 matching entries; narrow the filters")
		return
	}
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"id", "type", "status", "amount_minor", "currency", "occurred_on", "category", "client", "project", "invoice", "notes", "created_at", "updated_at"})
	for _, row := range rows {
		_ = writer.Write([]string{
			row.ID, row.Type, row.Status, strconv.FormatInt(row.AmountMinor, 10), row.Currency, row.OccurredOn, row.Category,
			stringValue(row.ClientName), stringValue(row.ProjectName), stringValue(row.InvoiceNumber), row.Notes,
			normalizeTimestamp(row.CreatedAt), normalizeTimestamp(row.UpdatedAt),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(c, http.StatusInternalServerError, "EXPORT_FAILED", "Financial entries could not be encoded")
		return
	}
	filename := "financial-entries-" + a.options.Now().Format("20060102") + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buffer.Bytes())
}

func parseFinancialEntryFilters(c *gin.Context) (financialEntryFilters, error) {
	filters := financialEntryFilters{
		EntryType: strings.TrimSpace(c.Query("type")), Status: strings.TrimSpace(c.Query("status")),
		Currency: strings.TrimSpace(c.Query("currency")), Category: strings.TrimSpace(c.Query("category")),
		ClientID: strings.TrimSpace(c.Query("client_id")), ProjectID: strings.TrimSpace(c.Query("project_id")),
		DateFrom: strings.TrimSpace(c.Query("date_from")), DateTo: strings.TrimSpace(c.Query("date_to")),
	}
	includeVoided, err := optionalBooleanQuery(c, "include_voided")
	if err != nil {
		return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", err.Error())
	}
	filters.IncludeVoided = includeVoided
	if filters.EntryType != "" {
		if _, ok := validFinancialEntryTypes[filters.EntryType]; !ok {
			return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", "type filter is invalid")
		}
	}
	if filters.Status != "" {
		if _, ok := validFinancialEntryStatuses[filters.Status]; !ok {
			return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
		}
	}
	if filters.Currency != "" {
		filters.Currency, err = normalizeFinancialCurrency(filters.Currency)
		if err != nil {
			return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", "currency filter must be a three-letter uppercase code")
		}
	}
	if utf8.RuneCountInString(filters.Category) > 80 {
		return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", "category filter cannot exceed 80 characters")
	}
	for _, item := range []struct {
		name  string
		value *string
	}{{"client_id", &filters.ClientID}, {"project_id", &filters.ProjectID}} {
		if *item.value != "" {
			parsed, err := uuid.Parse(*item.value)
			if err != nil {
				return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", item.name+" filter must be a UUID")
			}
			*item.value = parsed.String()
		}
	}
	if err := validateFinancialDateRange(filters.DateFrom, filters.DateTo); err != nil {
		return filters, newFinancialEntryRequestError(http.StatusBadRequest, "INVALID_FILTER", err.Error())
	}
	return filters, nil
}

func validateFinancialDateRange(dateFrom, dateTo string) error {
	if dateFrom != "" && !validDate(dateFrom) {
		return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "date_from must use YYYY-MM-DD")
	}
	if dateTo != "" && !validDate(dateTo) {
		return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "date_to must use YYYY-MM-DD")
	}
	if dateFrom != "" && dateTo != "" && dateFrom > dateTo {
		return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "date_from cannot be after date_to")
	}
	return nil
}

func applyFinancialEntryFilters(query *gorm.DB, filters financialEntryFilters) *gorm.DB {
	if filters.EntryType != "" {
		query = query.Where("entry.type = ?", filters.EntryType)
	}
	if filters.Status != "" {
		query = query.Where("entry.status = ?", filters.Status)
	} else if !filters.IncludeVoided {
		query = query.Where("entry.status <> 'voided'")
	}
	if filters.Currency != "" {
		query = query.Where("entry.currency = ?", filters.Currency)
	}
	if filters.Category != "" {
		query = query.Where("entry.category = ?", filters.Category)
	}
	if filters.ClientID != "" {
		query = query.Where("entry.client_id = ?", filters.ClientID)
	}
	if filters.ProjectID != "" {
		query = query.Where("entry.project_id = ?", filters.ProjectID)
	}
	if filters.DateFrom != "" {
		query = query.Where("entry.occurred_on >= ?", filters.DateFrom)
	}
	if filters.DateTo != "" {
		query = query.Where("entry.occurred_on <= ?", filters.DateTo)
	}
	return query
}

func financialEntryRowsQuery(query *gorm.DB) *gorm.DB {
	return query.Select(financialEntrySelectColumns).
		Joins("LEFT JOIN clients ON clients.id = entry.client_id").
		Joins("LEFT JOIN projects ON projects.id = entry.project_id").
		Joins("LEFT JOIN invoices ON invoices.id = entry.invoice_id")
}

const financialEntrySelectColumns = `
	entry.id, entry.type, entry.amount_minor, entry.currency, entry.occurred_on,
	entry.status, entry.category, entry.client_id, entry.project_id, entry.invoice_id,
	entry.notes, entry.created_by_actor_id, entry.voided_at, entry.voided_by_actor_id,
	entry.void_reason, entry.version, entry.created_at, entry.updated_at,
	clients.name AS client_name, projects.name AS project_name, invoices.invoice_number
`

func applyFinancialEntrySort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.Order("entry.occurred_on DESC").Order("entry.created_at DESC").Order("entry.id ASC"), true
	}
	allowed := map[string]string{"occurred_on": "entry.occurred_on", "amount_minor": "entry.amount_minor", "type": "entry.type", "status": "entry.status", "category": "entry.category", "created_at": "entry.created_at", "updated_at": "entry.updated_at"}
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
	return query.Order("entry.id ASC"), true
}

func loadFinancialEntryRow(db *gorm.DB, id string) (financialEntryRow, error) {
	var row financialEntryRow
	err := financialEntryRowsQuery(db.Table("financial_entries AS entry")).Where("entry.id = ?", id).Take(&row).Error
	return row, err
}

func financialEntryResponseFromRow(row financialEntryRow) financialEntryResponse {
	voidedAt := row.VoidedAt
	if voidedAt != nil {
		normalized := normalizeTimestamp(*voidedAt)
		voidedAt = &normalized
	}
	return financialEntryResponse{
		ID: row.ID, Type: row.Type, AmountMinor: row.AmountMinor, Currency: row.Currency,
		OccurredOn: row.OccurredOn, Status: row.Status, Category: row.Category,
		ClientID: row.ClientID, ClientName: row.ClientName, ProjectID: row.ProjectID, ProjectName: row.ProjectName,
		InvoiceID: row.InvoiceID, InvoiceNumber: row.InvoiceNumber, Notes: row.Notes,
		CreatedByActorID: row.CreatedByActorID, VoidedAt: voidedAt, VoidReason: row.VoidReason,
		Version: row.Version, CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

func financialEntryFromCreateRequest(input createFinancialEntryRequest, now time.Time) (models.FinancialEntry, error) {
	entryType, err := normalizeFinancialEntryType(input.Type)
	if err != nil {
		return models.FinancialEntry{}, err
	}
	if err := validateFinancialAmount(input.AmountMinor); err != nil {
		return models.FinancialEntry{}, err
	}
	currency, err := normalizeFinancialCurrency(input.Currency)
	if err != nil {
		return models.FinancialEntry{}, err
	}
	occurredOn := strings.TrimSpace(input.OccurredOn)
	if !validDate(occurredOn) {
		return models.FinancialEntry{}, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "occurred_on must use YYYY-MM-DD")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "confirmed"
	}
	if status != "pending" && status != "confirmed" {
		return models.FinancialEntry{}, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status must be pending or confirmed when creating an entry")
	}
	category, err := normalizeFinancialCategory(input.Category)
	if err != nil {
		return models.FinancialEntry{}, err
	}
	clientID, err := normalizeOptionalUUID(input.ClientID, "client_id")
	if err != nil {
		return models.FinancialEntry{}, err
	}
	projectID, err := normalizeOptionalUUID(input.ProjectID, "project_id")
	if err != nil {
		return models.FinancialEntry{}, err
	}
	if utf8.RuneCountInString(input.Notes) > 10_000 {
		return models.FinancialEntry{}, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "notes cannot exceed 10000 characters")
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	return models.FinancialEntry{ID: uuid.NewString(), Type: entryType, AmountMinor: input.AmountMinor, Currency: currency, OccurredOn: occurredOn, Status: status, Category: category, ClientID: clientID, ProjectID: projectID, Notes: input.Notes, CreatedByActorID: models.BuiltinOwnerActorID, Version: 1, CreatedAt: nowText, UpdatedAt: nowText}, nil
}

func financialEntryUpdates(tx *gorm.DB, entry models.FinancialEntry, input updateFinancialEntryRequest) (map[string]any, error) {
	updates := map[string]any{}
	if input.Type.Set {
		if input.Type.Value == nil {
			return nil, financialFieldRequired("type")
		}
		value, err := normalizeFinancialEntryType(*input.Type.Value)
		if err != nil {
			return nil, err
		}
		entry.Type = value
		updates["type"] = value
	}
	if input.AmountMinor.Set {
		if input.AmountMinor.Value == nil {
			return nil, financialFieldRequired("amount_minor")
		}
		if err := validateFinancialAmount(*input.AmountMinor.Value); err != nil {
			return nil, err
		}
		entry.AmountMinor = *input.AmountMinor.Value
		updates["amount_minor"] = entry.AmountMinor
	}
	if input.Currency.Set {
		if input.Currency.Value == nil {
			return nil, financialFieldRequired("currency")
		}
		value, err := normalizeFinancialCurrency(*input.Currency.Value)
		if err != nil {
			return nil, err
		}
		entry.Currency = value
		updates["currency"] = value
	}
	if input.OccurredOn.Set {
		if input.OccurredOn.Value == nil {
			return nil, financialFieldRequired("occurred_on")
		}
		value := strings.TrimSpace(*input.OccurredOn.Value)
		if !validDate(value) {
			return nil, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "occurred_on must use YYYY-MM-DD")
		}
		entry.OccurredOn = value
		updates["occurred_on"] = value
	}
	if input.Status.Set {
		if input.Status.Value == nil {
			return nil, financialFieldRequired("status")
		}
		value := strings.TrimSpace(*input.Status.Value)
		if value != "pending" && value != "confirmed" {
			return nil, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status must be pending or confirmed; use DELETE to void")
		}
		entry.Status = value
		updates["status"] = value
	}
	if input.Category.Set {
		if input.Category.Value == nil {
			return nil, financialFieldRequired("category")
		}
		value, err := normalizeFinancialCategory(*input.Category.Value)
		if err != nil {
			return nil, err
		}
		entry.Category = value
		updates["category"] = value
	}
	if input.Notes.Set {
		if input.Notes.Value == nil {
			return nil, financialFieldRequired("notes")
		}
		if utf8.RuneCountInString(*input.Notes.Value) > 10_000 {
			return nil, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "notes cannot exceed 10000 characters")
		}
		entry.Notes = *input.Notes.Value
		updates["notes"] = entry.Notes
	}
	if input.ClientID.Set {
		value, err := normalizeOptionalUUID(input.ClientID.Value, "client_id")
		if err != nil {
			return nil, err
		}
		entry.ClientID = value
		updates["client_id"] = value
	}
	if input.ProjectID.Set {
		value, err := normalizeOptionalUUID(input.ProjectID.Value, "project_id")
		if err != nil {
			return nil, err
		}
		entry.ProjectID = value
		updates["project_id"] = value
	}
	if len(updates) == 0 {
		return nil, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable financial entry field is required")
	}
	if err := normalizeFinancialEntryAssociations(tx, &entry); err != nil {
		return nil, err
	}
	updates["client_id"] = entry.ClientID
	updates["project_id"] = entry.ProjectID
	return updates, nil
}

func normalizeFinancialEntryAssociations(tx *gorm.DB, entry *models.FinancialEntry) error {
	if entry.ClientID != nil {
		var count int64
		if err := tx.Table("clients").Where("id = ?", *entry.ClientID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "CLIENT_NOT_FOUND", "client_id does not reference an existing client")
		}
	}
	if entry.ProjectID != nil {
		var project struct {
			ClientID *string `gorm:"column:client_id"`
		}
		if err := tx.Table("projects").Select("client_id").Where("id = ?", *entry.ProjectID).Take(&project).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "PROJECT_NOT_FOUND", "project_id does not reference an existing project")
			}
			return err
		}
		if project.ClientID != nil {
			if entry.ClientID == nil {
				entry.ClientID = project.ClientID
			} else if *entry.ClientID != *project.ClientID {
				return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "PROJECT_CLIENT_MISMATCH", "project_id belongs to a different client")
			}
		}
	}
	return nil
}

func financialEntryCreateRequestHash(entry models.FinancialEntry) (string, error) {
	payload := struct {
		Type        string  `json:"type"`
		AmountMinor int64   `json:"amount_minor"`
		Currency    string  `json:"currency"`
		OccurredOn  string  `json:"occurred_on"`
		Status      string  `json:"status"`
		Category    string  `json:"category"`
		ClientID    *string `json:"client_id"`
		ProjectID   *string `json:"project_id"`
		Notes       string  `json:"notes"`
	}{entry.Type, entry.AmountMinor, entry.Currency, entry.OccurredOn, entry.Status, entry.Category, entry.ClientID, entry.ProjectID, entry.Notes}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func recordFinancialEntryWorkflowEvent(tx *gorm.DB, entry models.FinancialEntry, action string, previous map[string]any, requestID string) error {
	var previousJSON any
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		previousJSON = string(encoded)
	}
	current, err := json.Marshal(financialEntryEventState(entry))
	if err != nil {
		return err
	}
	var storedRequestID any
	if requestID != "" {
		storedRequestID = requestID
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "financial_entry", "aggregate_id": entry.ID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": storedRequestID,
		"previous_json": previousJSON, "current_json": string(current), "created_at": entry.UpdatedAt,
	}).Error
}

func financialEntryEventState(entry models.FinancialEntry) map[string]any {
	return map[string]any{"type": entry.Type, "amount_minor": entry.AmountMinor, "currency": entry.Currency, "occurred_on": entry.OccurredOn, "status": entry.Status, "category": entry.Category, "client_id": entry.ClientID, "project_id": entry.ProjectID, "invoice_id": entry.InvoiceID, "notes": entry.Notes, "voided_at": entry.VoidedAt, "void_reason": entry.VoidReason, "version": entry.Version}
}

func normalizeFinancialEntryType(value string) (string, error) {
	value = strings.TrimSpace(value)
	if _, ok := validFinancialEntryTypes[value]; !ok {
		return "", newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "type must be income or expense")
	}
	return value, nil
}
func validateFinancialAmount(value int64) error {
	if value < 1 || value > maxFinancialAmountMinor {
		return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "amount_minor must be between 1 and 9000000000000000")
	}
	return nil
}
func normalizeFinancialCurrency(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 3 {
		return "", newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "currency must be a three-letter code")
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return "", newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "currency must be a three-letter code")
		}
	}
	return value, nil
}
func normalizeFinancialCategory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if length := utf8.RuneCountInString(value); length < 1 || length > 80 {
		return "", newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "category must contain 1 to 80 characters")
	}
	return value, nil
}
func normalizeOptionalUUID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", field+" must be a UUID or null")
	}
	normalized := parsed.String()
	return &normalized, nil
}
func financialFieldRequired(field string) error {
	return newFinancialEntryRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", field+" cannot be null")
}
func financialEntryID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed.String() != strings.ToLower(raw) {
		writeError(c, http.StatusBadRequest, "INVALID_FINANCIAL_ENTRY_ID", "Financial entry id must be a canonical UUID")
		return "", false
	}
	return parsed.String(), true
}
func newFinancialEntryRequestError(status int, code, message string) error {
	return &financialEntryRequestError{status: status, code: code, message: message}
}
func financialEntryVersionConflict() error {
	return newFinancialEntryRequestError(http.StatusConflict, "VERSION_CONFLICT", "Financial entry has changed; reload it before retrying")
}
func writeFinancialEntryRequestError(c *gin.Context, err error) bool {
	var requestError *financialEntryRequestError
	if !errors.As(err, &requestError) {
		return false
	}
	writeError(c, requestError.status, requestError.code, requestError.message)
	return true
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
