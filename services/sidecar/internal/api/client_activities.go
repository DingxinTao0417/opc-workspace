package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const maxClientActivityFutureSkew = 5 * time.Minute

var validClientActivityKinds = map[string]struct{}{
	"note": {}, "meeting": {}, "system_reference": {},
}

type createClientActivityRequest struct {
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	OccurredAt string `json:"occurred_at"`
}

type updateClientActivityRequest struct {
	Kind       nullableStringPatch `json:"kind"`
	Title      nullableStringPatch `json:"title"`
	Body       nullableStringPatch `json:"body"`
	OccurredAt nullableStringPatch `json:"occurred_at"`
}

type deleteClientActivityRequest struct {
	Reason string `json:"reason"`
}

type clientActivityActorResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
}

type clientActivityResponse struct {
	ID               string                      `json:"id"`
	ClientID         string                      `json:"client_id"`
	Kind             string                      `json:"kind"`
	Title            string                      `json:"title"`
	Body             *string                     `json:"body"`
	OccurredAt       string                      `json:"occurred_at"`
	CreatedBy        clientActivityActorResponse `json:"created_by"`
	SourceType       *string                     `json:"source_type"`
	SourceID         *string                     `json:"source_id"`
	Version          int64                       `json:"version"`
	DeletedAt        *string                     `json:"deleted_at"`
	DeletedByActorID *string                     `json:"deleted_by_actor_id"`
	DeleteReason     *string                     `json:"delete_reason"`
	CreatedAt        string                      `json:"created_at"`
	UpdatedAt        string                      `json:"updated_at"`
	ClientVersion    int64                       `json:"client_version"`
}

type clientActivityRow struct {
	models.ClientActivity `gorm:"embedded"`
	CreatedByType         string `gorm:"column:created_by_type"`
	CreatedByDisplayName  string `gorm:"column:created_by_display_name"`
	ClientVersion         int64  `gorm:"column:client_version"`
}

func (a *API) listClientActivities(c *gin.Context) {
	clientID, ok := clientID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	kind := strings.TrimSpace(c.Query("kind"))
	if kind != "" {
		if _, valid := validClientActivityKinds[kind]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "kind filter is invalid")
			return
		}
	}
	includeDeleted, valid := parseClientActivityIncludeDeleted(c.Query("include_deleted"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "include_deleted must be true or false")
		return
	}

	var total int64
	var rows []clientActivityRow
	var clientVersion int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT version FROM clients WHERE id = ?", clientID).Row().Scan(&clientVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		query := tx.Table("client_activities").Where("client_activities.client_id = ?", clientID)
		if !includeDeleted {
			query = query.Where("client_activities.deleted_at IS NULL")
		}
		if kind != "" {
			query = query.Where("client_activities.kind = ?", kind)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.
			Select(clientActivitySelectColumns).
			Joins("JOIN actors created_by ON created_by.id = client_activities.created_by_actor_id").
			Joins("JOIN clients ON clients.id = client_activities.client_id").
			Order("client_activities.occurred_at DESC").
			Order("client_activities.id ASC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}

	items := make([]clientActivityResponse, len(rows))
	for index := range rows {
		items[index] = clientActivityResponseFromRow(rows[index])
	}
	setProjectETag(c, clientVersion)
	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": gin.H{
			"page": page, "page_size": pageSize, "total": total,
			"client_version": clientVersion,
		},
	})
}

func (a *API) createClientActivity(c *gin.Context) {
	clientID, ok := clientID(c)
	if !ok {
		return
	}
	var input createClientActivityRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	activity, err := a.clientActivityFromCreateRequest(clientID, input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	endpoint := "POST /api/v1/clients/" + clientID + "/activities"
	requestHash := ""
	if idempotencyKey != "" {
		requestHash, err = clientActivityCreateRequestHash(activity)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	statusCode := http.StatusCreated
	replayed := false
	var response clientActivityResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, endpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
				}
				if *existing.RequestHash != requestHash {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different client activity request")
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent client activity response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read client activity idempotency key: %w", err)
			}
		}
		var clientCount int64
		if err := tx.Model(&models.Client{}).Where("id = ?", clientID).Count(&clientCount).Error; err != nil {
			return err
		}
		if clientCount == 0 {
			return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
		}
		if err := tx.Create(&activity).Error; err != nil {
			return fmt.Errorf("create client activity: %w", err)
		}
		row, err := loadClientActivityRow(tx, activity.ID)
		if err != nil {
			return err
		}
		response = clientActivityResponseFromRow(row)
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent client activity response: %w", err)
			}
			body := string(encoded)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: endpoint, ResourceID: activity.ID,
				RequestHash: &requestHash, ResponseBody: &body, ResponseStatus: &responseStatus,
				CreatedAt: activity.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record client activity idempotency key: %w", err)
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

func (a *API) getClientActivity(c *gin.Context) {
	id, ok := clientActivityID(c)
	if !ok {
		return
	}
	row, err := loadClientActivityRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_ACTIVITY_NOT_FOUND", "Client activity not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := clientActivityResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateClientActivity(c *gin.Context) {
	id, ok := clientActivityID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateClientActivityRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	updates, err := a.clientActivityUpdates(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	var response clientActivityResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var activity models.ClientActivity
		if err := tx.First(&activity, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_ACTIVITY_NOT_FOUND", "Client activity not found")
			}
			return err
		}
		if activity.Version != expectedVersion {
			return clientActivityVersionConflict()
		}
		if activity.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_DELETED", "Deleted client activities cannot be changed")
		}
		if activity.Kind == "system_reference" {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_READ_ONLY", "System reference activities are read-only")
		}
		updates["updated_at"] = a.options.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.ClientActivity{}).Where("id = ? AND version = ? AND deleted_at IS NULL", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return clientActivityVersionConflict()
		}
		row, err := loadClientActivityRow(tx, id)
		if err != nil {
			return err
		}
		response = clientActivityResponseFromRow(row)
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

func (a *API) deleteClientActivity(c *gin.Context) {
	id, ok := clientActivityID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Client activity deletion requires confirm=true")
		return
	}
	var input deleteClientActivityRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := cleanClientActivityText(input.Reason, "reason", 1_000, true)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	var response clientActivityResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var activity models.ClientActivity
		if err := tx.First(&activity, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_ACTIVITY_NOT_FOUND", "Client activity not found")
			}
			return err
		}
		if activity.Version != expectedVersion {
			return clientActivityVersionConflict()
		}
		if activity.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_DELETED", "Client activity is already deleted")
		}
		if activity.Kind == "system_reference" {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_READ_ONLY", "System reference activities are read-only")
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.ClientActivity{}).
			Where("id = ? AND version = ? AND deleted_at IS NULL", id, expectedVersion).
			Updates(map[string]any{
				"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID,
				"delete_reason": reason, "updated_at": now, "version": gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return clientActivityVersionConflict()
		}
		row, err := loadClientActivityRow(tx, id)
		if err != nil {
			return err
		}
		response = clientActivityResponseFromRow(row)
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

func (a *API) clientActivityFromCreateRequest(clientID string, input createClientActivityRequest) (models.ClientActivity, error) {
	kind, err := cleanManualClientActivityKind(input.Kind)
	if err != nil {
		return models.ClientActivity{}, err
	}
	title, err := cleanClientActivityText(input.Title, "title", 200, false)
	if err != nil {
		return models.ClientActivity{}, err
	}
	body, err := cleanClientActivityText(input.Body, "body", 10_000, true)
	if err != nil {
		return models.ClientActivity{}, err
	}
	occurredAt, err := cleanClientActivityOccurredAt(input.OccurredAt, a.options.Now())
	if err != nil {
		return models.ClientActivity{}, err
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	return models.ClientActivity{
		ID: uuid.NewString(), ClientID: clientID, Kind: kind, Title: title, Body: &body,
		OccurredAt: occurredAt, CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (a *API) clientActivityUpdates(input updateClientActivityRequest) (map[string]any, error) {
	updates := make(map[string]any)
	if input.Kind.Set {
		if input.Kind.Value == nil {
			return nil, errors.New("kind cannot be null")
		}
		kind, err := cleanManualClientActivityKind(*input.Kind.Value)
		if err != nil {
			return nil, err
		}
		updates["kind"] = kind
	}
	for _, field := range []struct {
		name       string
		patch      nullableStringPatch
		maxRunes   int
		allowLines bool
	}{
		{name: "title", patch: input.Title, maxRunes: 200},
		{name: "body", patch: input.Body, maxRunes: 10_000, allowLines: true},
	} {
		if !field.patch.Set {
			continue
		}
		if field.patch.Value == nil {
			return nil, fmt.Errorf("%s cannot be null", field.name)
		}
		value, err := cleanClientActivityText(*field.patch.Value, field.name, field.maxRunes, field.allowLines)
		if err != nil {
			return nil, err
		}
		updates[field.name] = value
	}
	if input.OccurredAt.Set {
		if input.OccurredAt.Value == nil {
			return nil, errors.New("occurred_at cannot be null")
		}
		occurredAt, err := cleanClientActivityOccurredAt(*input.OccurredAt.Value, a.options.Now())
		if err != nil {
			return nil, err
		}
		updates["occurred_at"] = occurredAt
	}
	if len(updates) == 0 {
		return nil, errors.New("at least one editable client activity field is required")
	}
	return updates, nil
}

func cleanManualClientActivityKind(raw string) (string, error) {
	kind := strings.TrimSpace(raw)
	if kind != "note" && kind != "meeting" {
		return "", errors.New("kind must be note or meeting")
	}
	return kind, nil
}

func cleanClientActivityText(raw, field string, maxRunes int, allowLines bool) (string, error) {
	value := strings.TrimSpace(raw)
	if length := utf8.RuneCountInString(value); length < 1 || length > maxRunes {
		return "", fmt.Errorf("%s must contain 1 to %d characters", field, maxRunes)
	}
	for _, character := range value {
		if !unicode.IsControl(character) {
			continue
		}
		if allowLines && (character == '\n' || character == '\r' || character == '\t') {
			continue
		}
		return "", fmt.Errorf("%s cannot contain unsupported control characters", field)
	}
	return value, nil
}

func cleanClientActivityOccurredAt(raw string, now time.Time) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", errors.New("occurred_at must be an RFC 3339 timestamp")
	}
	if parsed.After(now.UTC().Add(maxClientActivityFutureSkew)) {
		return "", errors.New("occurred_at cannot be in the future")
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func clientActivityCreateRequestHash(activity models.ClientActivity) (string, error) {
	payload := struct {
		ClientID   string `json:"client_id"`
		Kind       string `json:"kind"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		OccurredAt string `json:"occurred_at"`
	}{
		ClientID: activity.ClientID, Kind: activity.Kind, Title: activity.Title,
		Body: *activity.Body, OccurredAt: activity.OccurredAt,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func parseClientActivityIncludeDeleted(raw string) (bool, bool) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}

func clientActivityID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CLIENT_ACTIVITY_ID", "Client activity id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func clientActivityVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Client activity has changed; reload it before retrying")
}

const clientActivitySelectColumns = `
	client_activities.id,
	client_activities.client_id,
	client_activities.kind,
	client_activities.title,
	client_activities.body,
	client_activities.occurred_at,
	client_activities.created_by_actor_id,
	client_activities.source_type,
	client_activities.source_id,
	client_activities.version,
	client_activities.deleted_at,
	client_activities.deleted_by_actor_id,
	client_activities.delete_reason,
	client_activities.created_at,
	client_activities.updated_at,
	created_by.type AS created_by_type,
	created_by.display_name AS created_by_display_name,
	clients.version AS client_version
`

func loadClientActivityRow(db *gorm.DB, id string) (clientActivityRow, error) {
	var row clientActivityRow
	err := db.Table("client_activities").
		Select(clientActivitySelectColumns).
		Joins("JOIN actors created_by ON created_by.id = client_activities.created_by_actor_id").
		Joins("JOIN clients ON clients.id = client_activities.client_id").
		Where("client_activities.id = ?", id).
		Take(&row).Error
	return row, err
}

func clientActivityResponseFromRow(row clientActivityRow) clientActivityResponse {
	body := row.Body
	if row.DeletedAt != nil {
		body = nil
	}
	response := clientActivityResponse{
		ID: row.ID, ClientID: row.ClientID, Kind: row.Kind, Title: row.Title, Body: body,
		OccurredAt: normalizeTimestamp(row.OccurredAt),
		CreatedBy: clientActivityActorResponse{
			ID: row.CreatedByActorID, Type: row.CreatedByType, DisplayName: row.CreatedByDisplayName,
		},
		SourceType: row.SourceType, SourceID: row.SourceID, Version: row.Version,
		DeletedAt: row.DeletedAt, DeletedByActorID: row.DeletedByActorID, DeleteReason: row.DeleteReason,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
		ClientVersion: row.ClientVersion,
	}
	for _, field := range []**string{&response.DeletedAt} {
		if *field != nil {
			normalized := normalizeTimestamp(**field)
			*field = &normalized
		}
	}
	return response
}
