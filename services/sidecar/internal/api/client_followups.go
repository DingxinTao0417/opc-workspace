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

var validClientFollowupStatuses = map[string]struct{}{"planned": {}, "completed": {}, "skipped": {}, "cancelled": {}}
var validClientFollowupPriorities = map[string]struct{}{"low": {}, "normal": {}, "high": {}}

type createClientFollowupRequest struct {
	ClientID        string  `json:"client_id"`
	AssignedActorID string  `json:"assigned_actor_id"`
	ScheduledAt     string  `json:"scheduled_at"`
	Timezone        string  `json:"timezone"`
	Channel         string  `json:"channel"`
	Purpose         string  `json:"purpose"`
	Notes           *string `json:"notes"`
	Priority        string  `json:"priority"`
}

type updateClientFollowupRequest struct {
	AssignedActorID nullableStringPatch `json:"assigned_actor_id"`
	ScheduledAt     nullableStringPatch `json:"scheduled_at"`
	Timezone        nullableStringPatch `json:"timezone"`
	Channel         nullableStringPatch `json:"channel"`
	Purpose         nullableStringPatch `json:"purpose"`
	Notes           nullableStringPatch `json:"notes"`
	Priority        nullableStringPatch `json:"priority"`
}

type clientFollowupResponse struct {
	ID                string  `json:"id"`
	ClientID          string  `json:"client_id"`
	ClientName        string  `json:"client_name"`
	AssignedActorID   string  `json:"assigned_actor_id"`
	AssignedActorName string  `json:"assigned_actor_name"`
	AssignedActorType string  `json:"assigned_actor_type"`
	ScheduledAt       string  `json:"scheduled_at"`
	Timezone          string  `json:"timezone"`
	Channel           string  `json:"channel"`
	Purpose           string  `json:"purpose"`
	Notes             *string `json:"notes"`
	Status            string  `json:"status"`
	Priority          string  `json:"priority"`
	CompletedAt       *string `json:"completed_at"`
	Result            *string `json:"result"`
	NextStep          *string `json:"next_step"`
	SkippedAt         *string `json:"skipped_at"`
	SkipReason        *string `json:"skip_reason"`
	CancelledAt       *string `json:"cancelled_at"`
	CancelReason      *string `json:"cancel_reason"`
	RescheduledFromID *string `json:"rescheduled_from_id"`
	Version           int64   `json:"version"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	ClientVersion     int64   `json:"client_version"`
}

type clientFollowupRow struct {
	models.ClientFollowup `gorm:"embedded"`
	ClientName            string `gorm:"column:client_name"`
	AssignedActorName     string `gorm:"column:assigned_actor_name"`
	AssignedActorType     string `gorm:"column:assigned_actor_type"`
	ClientVersion         int64  `gorm:"column:client_version"`
}

func (a *API) listClientFollowups(c *gin.Context) { a.listClientFollowupsForClient(c, "") }

func (a *API) listClientFollowupsForClient(c *gin.Context, scopedClientID string) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	clientID := scopedClientID
	if clientID == "" && strings.TrimSpace(c.Query("client_id")) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(c.Query("client_id")))
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "client_id filter must be a UUID")
			return
		}
		clientID = parsed.String()
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, valid := validClientFollowupStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
	}
	actorID := strings.TrimSpace(c.Query("assigned_actor_id"))
	if actorID != "" {
		parsed, err := uuid.Parse(actorID)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "assigned_actor_id filter must be a UUID")
			return
		}
		actorID = parsed.String()
	}

	var total int64
	var rows []clientFollowupRow
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if scopedClientID != "" {
			var exists int64
			if err := tx.Model(&models.Client{}).Where("id = ?", scopedClientID).Count(&exists).Error; err != nil {
				return err
			}
			if exists == 0 {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
		}
		query := tx.Table("client_followups")
		if clientID != "" {
			query = query.Where("client_followups.client_id = ?", clientID)
		}
		if status != "" {
			query = query.Where("client_followups.status = ?", status)
		}
		if actorID != "" {
			query = query.Where("client_followups.assigned_actor_id = ?", actorID)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select(clientFollowupSelectColumns).
			Joins("JOIN clients ON clients.id = client_followups.client_id").
			Joins("JOIN actors assigned_actor ON assigned_actor.id = client_followups.assigned_actor_id").
			Order("client_followups.scheduled_at ASC").Order("client_followups.id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	items := make([]clientFollowupResponse, len(rows))
	for i := range rows {
		items[i] = clientFollowupResponseFromRow(rows[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) listClientFollowupsForClientRoute(c *gin.Context) {
	clientID, ok := clientID(c)
	if !ok {
		return
	}
	a.listClientFollowupsForClient(c, clientID)
}

func (a *API) createClientFollowup(c *gin.Context) {
	var input createClientFollowupRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	followup, err := a.clientFollowupFromCreateRequest(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	hash := ""
	if key != "" {
		hash, err = clientFollowupCreateRequestHash(followup)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}
	endpoint := "POST /api/v1/client-followups"
	statusCode, replayed := http.StatusCreated, false
	var response clientFollowupResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if key != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", key, endpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
				}
				if *existing.RequestHash != hash {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different client followup request")
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent client followup response: %w", err)
				}
				statusCode, replayed = *existing.ResponseStatus, true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read client followup idempotency key: %w", err)
			}
		}
		if err := ensureClientFollowupReferences(tx, followup.ClientID, followup.AssignedActorID); err != nil {
			return err
		}
		if err := tx.Create(&followup).Error; err != nil {
			return fmt.Errorf("create client followup: %w", err)
		}
		row, err := loadClientFollowupRow(tx, followup.ID)
		if err != nil {
			return err
		}
		response = clientFollowupResponseFromRow(row)
		if err := recordClientFollowupWorkflowEvent(tx, followup.ID, "client_followup_created", nil, clientFollowupEventState(response), requestIDFromContext(c), followup.CreatedAt); err != nil {
			return err
		}
		if key != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return err
			}
			body, code := string(encoded), http.StatusCreated
			if err := tx.Create(&models.IdempotencyKey{Key: key, Endpoint: endpoint, ResourceID: followup.ID, RequestHash: &hash, ResponseBody: &body, ResponseStatus: &code, CreatedAt: followup.CreatedAt}).Error; err != nil {
				return fmt.Errorf("record client followup idempotency key: %w", err)
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

func (a *API) getClientFollowup(c *gin.Context) {
	id, ok := clientFollowupID(c)
	if !ok {
		return
	}
	row, err := loadClientFollowupRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_FOLLOWUP_NOT_FOUND", "Client followup not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	response := clientFollowupResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateClientFollowup(c *gin.Context) {
	id, ok := clientFollowupID(c)
	if !ok {
		return
	}
	expected, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateClientFollowupRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	updates, err := clientFollowupUpdates(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	var response clientFollowupResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current models.ClientFollowup
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_FOLLOWUP_NOT_FOUND", "Client followup not found")
			}
			return err
		}
		if current.Version != expected {
			return clientFollowupVersionConflict()
		}
		if current.Status != "planned" {
			return newProjectRequestError(http.StatusConflict, "CLIENT_FOLLOWUP_FINAL", "Terminal client followups cannot be edited")
		}
		if actor, exists := updates["assigned_actor_id"]; exists {
			if err := ensureClientFollowupReferences(tx, current.ClientID, actor.(string)); err != nil {
				return err
			}
		}
		previousRow, err := loadClientFollowupRow(tx, id)
		if err != nil {
			return err
		}
		updates["updated_at"], updates["version"] = a.options.Now().UTC().Format(time.RFC3339Nano), gorm.Expr("version + 1")
		result := tx.Model(&models.ClientFollowup{}).Where("id = ? AND version = ? AND status = 'planned'", id, expected).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return clientFollowupVersionConflict()
		}
		row, err := loadClientFollowupRow(tx, id)
		if err != nil {
			return err
		}
		response = clientFollowupResponseFromRow(row)
		return recordClientFollowupWorkflowEvent(tx, id, "client_followup_updated", clientFollowupEventState(clientFollowupResponseFromRow(previousRow)), clientFollowupEventState(response), requestIDFromContext(c), response.UpdatedAt)
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

func (a *API) clientFollowupFromCreateRequest(input createClientFollowupRequest) (models.ClientFollowup, error) {
	clientID, err := canonicalUUID(input.ClientID, "client_id")
	if err != nil {
		return models.ClientFollowup{}, err
	}
	actorID, err := canonicalUUID(input.AssignedActorID, "assigned_actor_id")
	if err != nil {
		return models.ClientFollowup{}, err
	}
	scheduledAt, err := cleanClientFollowupTimestamp(input.ScheduledAt)
	if err != nil {
		return models.ClientFollowup{}, err
	}
	timezone, err := cleanClientFollowupTimezone(input.Timezone)
	if err != nil {
		return models.ClientFollowup{}, err
	}
	channel, err := cleanClientFollowupText(input.Channel, "channel", 80, false, false)
	if err != nil {
		return models.ClientFollowup{}, err
	}
	purpose, err := cleanClientFollowupText(input.Purpose, "purpose", 500, false, false)
	if err != nil {
		return models.ClientFollowup{}, err
	}
	notes, err := cleanClientFollowupOptionalText(input.Notes, "notes", 4000, true)
	if err != nil {
		return models.ClientFollowup{}, err
	}
	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = "normal"
	}
	if _, valid := validClientFollowupPriorities[priority]; !valid {
		return models.ClientFollowup{}, errors.New("priority must be low, normal, or high")
	}
	now := a.options.Now().UTC()
	return models.ClientFollowup{ID: uuid.NewString(), ClientID: clientID, AssignedActorID: actorID, ScheduledAt: scheduledAt, Timezone: timezone, Channel: channel, Purpose: purpose, Notes: notes, Status: "planned", Priority: priority, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}, nil
}

func clientFollowupUpdates(input updateClientFollowupRequest) (map[string]any, error) {
	updates := map[string]any{}
	if input.AssignedActorID.Set {
		if input.AssignedActorID.Value == nil {
			return nil, errors.New("assigned_actor_id cannot be null")
		}
		value, err := canonicalUUID(*input.AssignedActorID.Value, "assigned_actor_id")
		if err != nil {
			return nil, err
		}
		updates["assigned_actor_id"] = value
	}
	if input.ScheduledAt.Set {
		if input.ScheduledAt.Value == nil {
			return nil, errors.New("scheduled_at cannot be null")
		}
		value, err := cleanClientFollowupTimestamp(*input.ScheduledAt.Value)
		if err != nil {
			return nil, err
		}
		updates["scheduled_at"] = value
	}
	if input.Timezone.Set {
		if input.Timezone.Value == nil {
			return nil, errors.New("timezone cannot be null")
		}
		value, err := cleanClientFollowupTimezone(*input.Timezone.Value)
		if err != nil {
			return nil, err
		}
		updates["timezone"] = value
	}
	for _, field := range []struct {
		name     string
		patch    nullableStringPatch
		max      int
		lines    bool
		nullable bool
	}{{"channel", input.Channel, 80, false, false}, {"purpose", input.Purpose, 500, false, false}, {"notes", input.Notes, 4000, true, true}} {
		if !field.patch.Set {
			continue
		}
		if field.patch.Value == nil {
			if field.nullable {
				updates[field.name] = nil
				continue
			}
			return nil, fmt.Errorf("%s cannot be null", field.name)
		}
		value, err := cleanClientFollowupText(*field.patch.Value, field.name, field.max, field.lines, false)
		if err != nil {
			return nil, err
		}
		updates[field.name] = value
	}
	if input.Priority.Set {
		if input.Priority.Value == nil {
			return nil, errors.New("priority cannot be null")
		}
		value := strings.TrimSpace(*input.Priority.Value)
		if _, valid := validClientFollowupPriorities[value]; !valid {
			return nil, errors.New("priority must be low, normal, or high")
		}
		updates["priority"] = value
	}
	if len(updates) == 0 {
		return nil, errors.New("at least one editable client followup field is required")
	}
	return updates, nil
}

func canonicalUUID(raw, field string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%s must be a UUID", field)
	}
	return parsed.String(), nil
}
func cleanClientFollowupTimestamp(raw string) (string, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("scheduled_at must be an RFC 3339 timestamp")
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}
func cleanClientFollowupTimezone(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if utf8.RuneCountInString(value) > 100 {
		return "", errors.New("timezone cannot exceed 100 characters")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", errors.New("timezone must be an IANA time zone")
	}
	return value, nil
}
func cleanClientFollowupOptionalText(raw *string, field string, max int, lines bool) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value, err := cleanClientFollowupText(*raw, field, max, lines, false)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func cleanClientFollowupText(raw, field string, max int, lines, allowEmpty bool) (string, error) {
	value := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(value)
	if length > max || (!allowEmpty && length < 1) {
		return "", fmt.Errorf("%s must contain 1 to %d characters", field, max)
	}
	for _, character := range value {
		if unicode.IsControl(character) && !(lines && (character == '\n' || character == '\r' || character == '\t')) {
			return "", fmt.Errorf("%s cannot contain unsupported control characters", field)
		}
	}
	return value, nil
}
func clientFollowupID(c *gin.Context) (string, bool) {
	id, err := canonicalUUID(c.Param("id"), "client followup id")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CLIENT_FOLLOWUP_ID", "Client followup id must be a UUID")
		return "", false
	}
	return id, true
}
func clientFollowupVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Client followup has changed; reload it before retrying")
}

func ensureClientFollowupReferences(tx *gorm.DB, clientID, actorID string) error {
	var clientCount, actorCount int64
	if err := tx.Model(&models.Client{}).Where("id = ?", clientID).Count(&clientCount).Error; err != nil {
		return err
	}
	if clientCount == 0 {
		return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
	}
	if err := tx.Model(&models.Actor{}).Where("id = ? AND status = 'active' AND type IN ('owner', 'person')", actorID).Count(&actorCount).Error; err != nil {
		return err
	}
	if actorCount == 0 {
		return newProjectRequestError(http.StatusUnprocessableEntity, "CLIENT_FOLLOWUP_ASSIGNEE_UNAVAILABLE", "assigned_actor_id must reference an active owner or person")
	}
	return nil
}

func clientFollowupCreateRequestHash(followup models.ClientFollowup) (string, error) {
	encoded, err := json.Marshal(struct {
		ClientID        string  `json:"client_id"`
		AssignedActorID string  `json:"assigned_actor_id"`
		ScheduledAt     string  `json:"scheduled_at"`
		Timezone        string  `json:"timezone"`
		Channel         string  `json:"channel"`
		Purpose         string  `json:"purpose"`
		Notes           *string `json:"notes"`
		Priority        string  `json:"priority"`
	}{followup.ClientID, followup.AssignedActorID, followup.ScheduledAt, followup.Timezone, followup.Channel, followup.Purpose, followup.Notes, followup.Priority})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

const clientFollowupSelectColumns = `client_followups.id, client_followups.client_id, client_followups.assigned_actor_id, client_followups.scheduled_at, client_followups.timezone, client_followups.channel, client_followups.purpose, client_followups.notes, client_followups.status, client_followups.priority, client_followups.completed_at, client_followups.result, client_followups.next_step, client_followups.skipped_at, client_followups.skip_reason, client_followups.cancelled_at, client_followups.cancel_reason, client_followups.rescheduled_from_id, client_followups.version, client_followups.created_at, client_followups.updated_at, clients.name AS client_name, assigned_actor.display_name AS assigned_actor_name, assigned_actor.type AS assigned_actor_type, clients.version AS client_version`

func loadClientFollowupRow(db *gorm.DB, id string) (clientFollowupRow, error) {
	var row clientFollowupRow
	err := db.Table("client_followups").Select(clientFollowupSelectColumns).Joins("JOIN clients ON clients.id = client_followups.client_id").Joins("JOIN actors assigned_actor ON assigned_actor.id = client_followups.assigned_actor_id").Where("client_followups.id = ?", id).Take(&row).Error
	return row, err
}
func clientFollowupResponseFromRow(row clientFollowupRow) clientFollowupResponse {
	return clientFollowupResponse{ID: row.ID, ClientID: row.ClientID, ClientName: row.ClientName, AssignedActorID: row.AssignedActorID, AssignedActorName: row.AssignedActorName, AssignedActorType: row.AssignedActorType, ScheduledAt: row.ScheduledAt, Timezone: row.Timezone, Channel: row.Channel, Purpose: row.Purpose, Notes: row.Notes, Status: row.Status, Priority: row.Priority, CompletedAt: row.CompletedAt, Result: row.Result, NextStep: row.NextStep, SkippedAt: row.SkippedAt, SkipReason: row.SkipReason, CancelledAt: row.CancelledAt, CancelReason: row.CancelReason, RescheduledFromID: row.RescheduledFromID, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ClientVersion: row.ClientVersion}
}
func clientFollowupEventState(row clientFollowupResponse) map[string]any {
	return map[string]any{"client_id": row.ClientID, "assigned_actor_id": row.AssignedActorID, "scheduled_at": row.ScheduledAt, "timezone": row.Timezone, "channel": row.Channel, "purpose": row.Purpose, "status": row.Status, "priority": row.Priority, "version": row.Version}
}
func recordClientFollowupWorkflowEvent(tx *gorm.DB, id, action string, previous, current map[string]any, requestID, createdAt string) error {
	var previousText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		text := string(encoded)
		previousText = &text
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return err
	}
	currentText := string(encoded)
	actor := models.BuiltinOwnerActorID
	request := (*string)(nil)
	if requestID != "" {
		request = &requestID
	}
	sequence := 1
	return tx.Create(&models.WorkflowEvent{ID: uuid.NewString(), AggregateType: "client_followup", AggregateID: id, Action: action, ActorID: &actor, RequestID: request, CommandSeq: &sequence, PreviousJSON: previousText, CurrentJSON: &currentText, CreatedAt: createdAt}).Error
}
