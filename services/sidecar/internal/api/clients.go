package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createClientEndpoint = "POST /api/v1/clients"

var validClientStatuses = map[string]struct{}{
	"active": {}, "lead": {}, "inactive": {},
}

type createClientRequest struct {
	Name        string              `json:"name"`
	ContactName *string             `json:"contact_name"`
	Email       *string             `json:"email"`
	Phone       *string             `json:"phone"`
	Notes       *string             `json:"notes"`
	Status      nullableStringPatch `json:"status"`
}

type updateClientRequest struct {
	Name        nullableStringPatch `json:"name"`
	ContactName nullableStringPatch `json:"contact_name"`
	Email       nullableStringPatch `json:"email"`
	Phone       nullableStringPatch `json:"phone"`
	Notes       nullableStringPatch `json:"notes"`
	Status      nullableStringPatch `json:"status"`
}

type clientResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	ContactName  *string `json:"contact_name"`
	Email        *string `json:"email"`
	Phone        *string `json:"phone"`
	Notes        *string `json:"notes"`
	Status       string  `json:"status"`
	ProjectCount int64   `json:"project_count"`
	Version      int64   `json:"version"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type clientRow struct {
	models.Client `gorm:"embedded"`
	ProjectCount  int64 `gorm:"column:project_count"`
}

type deletedClientResponse struct {
	DeletedID        string `json:"deleted_id"`
	DetachedProjects int64  `json:"detached_projects"`
}

func (a *API) listClients(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}

	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, valid := validClientStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
	}
	search := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(search) > 200 {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "q cannot exceed 200 characters")
		return
	}

	var total int64
	var rows []clientRow
	invalidSort := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Table("clients")
		if status != "" {
			query = query.Where("clients.status = ?", status)
		}
		if search != "" {
			like := "%" + escapeLike(search) + "%"
			query = query.Where(`
				clients.name LIKE ? ESCAPE '\'
				OR clients.contact_name LIKE ? ESCAPE '\'
				OR clients.email LIKE ? ESCAPE '\'
				OR clients.phone LIKE ? ESCAPE '\'
			`, like, like, like, like)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyClientSort(query.Select(clientSelectColumns), c.Query("sort"))
		if !valid {
			invalidSort = true
			return errors.New("invalid client sort")
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

	responses := make([]clientResponse, len(rows))
	for index := range rows {
		responses[index] = clientResponseFromRow(rows[index])
	}
	c.JSON(http.StatusOK, gin.H{
		"data": responses,
		"meta": pageMeta{Page: page, PageSize: pageSize, Total: total},
	})
}

func (a *API) createClient(c *gin.Context) {
	var input createClientRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	client, err := clientFromCreateRequest(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if idempotencyKey != "" {
		requestHash, err = clientCreateRequestHash(client)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	statusCode := http.StatusCreated
	replayed := false
	var response clientResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createClientEndpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_REPLAY_UNAVAILABLE",
						"This legacy Idempotency-Key cannot be replayed safely; use a new key",
					)
				}
				if *existing.RequestHash != requestHash {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_CONFLICT",
						"Idempotency-Key was already used with a different client request",
					)
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent client response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read client idempotency key: %w", err)
			}
		}

		if err := tx.Create(&client).Error; err != nil {
			return fmt.Errorf("create client: %w", err)
		}
		row, err := loadClientRow(tx, client.ID)
		if err != nil {
			return fmt.Errorf("load created client: %w", err)
		}
		response = clientResponseFromRow(row)
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent client response: %w", err)
			}
			responseText := string(encoded)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: createClientEndpoint, ResourceID: client.ID,
				RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &responseStatus,
				CreatedAt: client.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record client idempotency key: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
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

func (a *API) getClient(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	row, err := loadClientRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := clientResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateClient(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateClientRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	var response clientResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var client models.Client
		if err := tx.First(&client, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		if client.Version != expectedVersion {
			return clientVersionConflict()
		}
		updates, err := clientUpdates(input)
		if err != nil {
			return err
		}
		updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.Client{}).
			Where("id = ? AND version = ?", id, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return clientVersionConflict()
		}
		row, err := loadClientRow(tx, id)
		if err != nil {
			return err
		}
		response = clientResponseFromRow(row)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteClient(c *gin.Context) {
	id, ok := clientID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Hard deletion requires confirm=true")
		return
	}

	deleted := deletedClientResponse{DeletedID: id}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var client models.Client
		if err := tx.First(&client, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		if client.Version != expectedVersion {
			return clientVersionConflict()
		}
		if client.Status != "inactive" {
			return newProjectRequestError(
				http.StatusConflict,
				"CLIENT_NOT_INACTIVE",
				"Only inactive clients can be permanently deleted",
			)
		}

		var invoiceCount int64
		if err := tx.Table("invoices").Where("client_id = ?", id).Count(&invoiceCount).Error; err != nil {
			return err
		}
		if invoiceCount > 0 {
			return newProjectRequestError(
				http.StatusConflict,
				"CLIENT_HAS_INVOICES",
				fmt.Sprintf("Client is referenced by %d invoice(s) and cannot be deleted", invoiceCount),
			)
		}
		if err := tx.Table("projects").Where("client_id = ?", id).Count(&deleted.DetachedProjects).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.Client{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return clientVersionConflict()
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": deleted})
}

func clientFromCreateRequest(input createClientRequest) (models.Client, error) {
	name, err := cleanClientName(input.Name)
	if err != nil {
		return models.Client{}, err
	}
	contactName, err := cleanClientOptionalText(input.ContactName, "contact_name", 200, false)
	if err != nil {
		return models.Client{}, err
	}
	email, err := cleanClientEmail(input.Email)
	if err != nil {
		return models.Client{}, err
	}
	phone, err := cleanClientPhone(input.Phone)
	if err != nil {
		return models.Client{}, err
	}
	notes, err := cleanClientOptionalText(input.Notes, "notes", 10_000, true)
	if err != nil {
		return models.Client{}, err
	}
	status := "active"
	if input.Status.Set {
		if input.Status.Value == nil {
			return models.Client{}, errors.New("status cannot be null")
		}
		status, err = cleanClientStatus(*input.Status.Value)
		if err != nil {
			return models.Client{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.Client{
		ID: uuid.NewString(), Name: name, ContactName: contactName, Email: email,
		Phone: phone, Notes: notes, Status: status, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func clientCreateRequestHash(client models.Client) (string, error) {
	payload := struct {
		Name        string  `json:"name"`
		ContactName *string `json:"contact_name"`
		Email       *string `json:"email"`
		Phone       *string `json:"phone"`
		Notes       *string `json:"notes"`
		Status      string  `json:"status"`
	}{
		Name: client.Name, ContactName: client.ContactName, Email: client.Email,
		Phone: client.Phone, Notes: client.Notes, Status: client.Status,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode client request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func clientUpdates(input updateClientRequest) (map[string]any, error) {
	updates := make(map[string]any)
	if input.Name.Set {
		if input.Name.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name cannot be null")
		}
		name, err := cleanClientName(*input.Name.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		updates["name"] = name
	}
	optionalFields := []struct {
		name       string
		patch      nullableStringPatch
		maxRunes   int
		allowLines bool
	}{
		{name: "contact_name", patch: input.ContactName, maxRunes: 200},
		{name: "notes", patch: input.Notes, maxRunes: 10_000, allowLines: true},
	}
	for _, field := range optionalFields {
		if !field.patch.Set {
			continue
		}
		value, err := cleanClientOptionalText(field.patch.Value, field.name, field.maxRunes, field.allowLines)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		if value == nil {
			updates[field.name] = nil
		} else {
			updates[field.name] = *value
		}
	}
	if input.Email.Set {
		value, err := cleanClientEmail(input.Email.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		if value == nil {
			updates["email"] = nil
		} else {
			updates["email"] = *value
		}
	}
	if input.Phone.Set {
		value, err := cleanClientPhone(input.Phone.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		if value == nil {
			updates["phone"] = nil
		} else {
			updates["phone"] = *value
		}
	}
	if input.Status.Set {
		if input.Status.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status cannot be null")
		}
		status, err := cleanClientStatus(*input.Status.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		updates["status"] = status
	}
	if len(updates) == 0 {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable client field is required")
	}
	return updates, nil
}

func cleanClientName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if length := utf8.RuneCountInString(value); length < 1 || length > 200 {
		return "", errors.New("name must contain 1 to 200 characters")
	}
	if hasUnsupportedClientControl(value, false) {
		return "", errors.New("name cannot contain control characters")
	}
	return value, nil
}

func cleanClientOptionalText(raw *string, field string, maxRunes int, allowLines bool) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return nil, fmt.Errorf("%s cannot exceed %d characters", field, maxRunes)
	}
	if hasUnsupportedClientControl(value, allowLines) {
		return nil, fmt.Errorf("%s cannot contain unsupported control characters", field)
	}
	return &value, nil
}

func cleanClientEmail(raw *string) (*string, error) {
	value, err := cleanClientOptionalText(raw, "email", 320, false)
	if err != nil || value == nil {
		return value, err
	}
	address, parseErr := mail.ParseAddress(*value)
	if parseErr != nil || address.Name != "" || address.Address != *value {
		return nil, errors.New("email must be a valid email address")
	}
	local, domain, found := strings.Cut(address.Address, "@")
	if !found || local == "" || domain == "" {
		return nil, errors.New("email must be a valid email address")
	}
	return value, nil
}

func cleanClientPhone(raw *string) (*string, error) {
	return cleanClientOptionalText(raw, "phone", 50, false)
}

func cleanClientStatus(raw string) (string, error) {
	status := strings.TrimSpace(raw)
	if _, valid := validClientStatuses[status]; !valid {
		return "", errors.New("status must be active, lead, or inactive")
	}
	return status, nil
}

func hasUnsupportedClientControl(value string, allowLines bool) bool {
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowLines && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		return true
	}
	return false
}

func clientID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CLIENT_ID", "Client id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func clientVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Client has changed; reload it before retrying")
}

func applyClientSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.Order("clients.updated_at DESC").Order("clients.id ASC"), true
	}
	allowed := map[string]string{
		"name":          "LOWER(clients.name)",
		"contact_name":  "LOWER(clients.contact_name)",
		"status":        "clients.status",
		"project_count": "project_count",
		"created_at":    "clients.created_at",
		"updated_at":    "clients.updated_at",
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
	return query.Order("clients.id ASC"), true
}

const clientSelectColumns = `
	clients.id,
	clients.name,
	clients.contact_name,
	clients.email,
	clients.phone,
	clients.notes,
	clients.status,
	clients.version,
	clients.created_at,
	clients.updated_at,
	(SELECT COUNT(*) FROM projects WHERE projects.client_id = clients.id) AS project_count
`

func loadClientRow(db *gorm.DB, id string) (clientRow, error) {
	var row clientRow
	err := db.Table("clients").
		Select(clientSelectColumns).
		Where("clients.id = ?", id).
		Take(&row).Error
	return row, err
}

func clientResponseFromRow(row clientRow) clientResponse {
	return clientResponse{
		ID: row.ID, Name: row.Name, ContactName: row.ContactName, Email: row.Email,
		Phone: row.Phone, Notes: row.Notes, Status: row.Status, ProjectCount: row.ProjectCount,
		Version: row.Version, CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}
