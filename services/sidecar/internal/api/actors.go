package api

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	createActorEndpoint   = "POST /api/v1/actors"
	maxActorMetadataBytes = 16 << 10
	maxActorMetadataDepth = 6
	maxActorMetadataKeys  = 100
)

var (
	validActorTypes = map[string]struct{}{
		"owner": {}, "person": {}, "system": {}, "agent": {},
	}
	validActorStatuses = map[string]struct{}{
		"active": {}, "inactive": {},
	}
)

type actorMetadataPatch struct {
	Set bool
	Raw json.RawMessage
}

func (field *actorMetadataPatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	field.Raw = append(field.Raw[:0], data...)
	return nil
}

type createActorRequest struct {
	Type        string              `json:"type"`
	DisplayName string              `json:"display_name"`
	Status      nullableStringPatch `json:"status"`
	Notes       nullableStringPatch `json:"notes"`
	Metadata    actorMetadataPatch  `json:"metadata"`
}

type updateActorRequest struct {
	DisplayName nullableStringPatch `json:"display_name"`
	Status      nullableStringPatch `json:"status"`
	Notes       nullableStringPatch `json:"notes"`
	Metadata    actorMetadataPatch  `json:"metadata"`
}

type actorResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	DisplayName string          `json:"display_name"`
	Status      string          `json:"status"`
	IsBuiltin   bool            `json:"is_builtin"`
	Notes       string          `json:"notes"`
	Metadata    json.RawMessage `json:"metadata"`
	Version     int64           `json:"version"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

func (a *API) listActors(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}

	actorType := strings.TrimSpace(c.Query("type"))
	if actorType != "" {
		if _, valid := validActorTypes[actorType]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "type filter is invalid")
			return
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, valid := validActorStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
	}

	var total int64
	var actors []models.Actor
	invalidSort := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.Actor{})
		if actorType != "" {
			query = query.Where("type = ?", actorType)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyActorSort(query, c.Query("sort"))
		if !valid {
			invalidSort = true
			return errors.New("invalid actor sort")
		}
		return ordered.Offset((page - 1) * pageSize).Limit(pageSize).Find(&actors).Error
	}, &sql.TxOptions{ReadOnly: true})
	if invalidSort {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	responses, err := actorResponsesFromModels(actors)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": responses,
		"meta": pageMeta{Page: page, PageSize: pageSize, Total: total},
	})
}

func (a *API) createActor(c *gin.Context) {
	var input createActorRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	actor, err := actorFromCreateRequest(input)
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
		requestHash, err = actorCreateRequestHash(actor)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	statusCode := http.StatusCreated
	replayed := false
	var response actorResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createActorEndpoint).First(&existing).Error
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
						"Idempotency-Key was already used with a different actor request",
					)
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent actor response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read actor idempotency key: %w", err)
			}
		}

		if err := tx.Create(&actor).Error; err != nil {
			return fmt.Errorf("create actor: %w", err)
		}
		response, err = actorResponseFromModel(actor)
		if err != nil {
			return err
		}
		if err := recordActorWorkflowEvent(
			tx,
			"actor_created",
			actor.ID,
			nil,
			response,
			requestIDFromContext(c),
			actor.CreatedAt,
		); err != nil {
			return err
		}
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent actor response: %w", err)
			}
			responseText := string(encoded)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: createActorEndpoint, ResourceID: actor.ID,
				RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &responseStatus,
				CreatedAt: actor.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record actor idempotency key: %w", err)
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

func (a *API) getActor(c *gin.Context) {
	id, ok := actorID(c)
	if !ok {
		return
	}
	actor, err := loadActor(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "ACTOR_NOT_FOUND", "Actor not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response, err := actorResponseFromModel(actor)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateActor(c *gin.Context) {
	id, ok := actorID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateActorRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	var response actorResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		actor, err := loadActor(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ACTOR_NOT_FOUND", "Actor not found")
			}
			return err
		}
		if actor.Version != expectedVersion {
			return taskVersionConflict()
		}
		previous, err := actorResponseFromModel(actor)
		if err != nil {
			return err
		}
		updates, err := actorUpdates(tx, actor, input)
		if err != nil {
			return err
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		updates["updated_at"] = updatedAt
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.Actor{}).
			Where("id = ? AND version = ?", id, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			if strings.Contains(result.Error.Error(), "ACTOR_HAS_ACTIVE_ASSIGNMENTS") {
				return actorHasActiveAssignmentsError()
			}
			if strings.Contains(result.Error.Error(), "ACTOR_HAS_ACTIVE_CLIENT_LINKS") {
				return actorHasActiveClientLinksError()
			}
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		updated, err := loadActor(tx, id)
		if err != nil {
			return err
		}
		response, err = actorResponseFromModel(updated)
		if err != nil {
			return err
		}
		action := "actor_updated"
		if previous.Status == "active" && response.Status == "inactive" {
			action = "actor_deactivated"
		}
		return recordActorWorkflowEvent(
			tx,
			action,
			actor.ID,
			&previous,
			response,
			requestIDFromContext(c),
			updatedAt,
		)
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

func actorFromCreateRequest(input createActorRequest) (models.Actor, error) {
	actorType := strings.TrimSpace(input.Type)
	if actorType != "person" {
		return models.Actor{}, errors.New("type must be person")
	}
	displayName, err := validateActorDisplayName(input.DisplayName)
	if err != nil {
		return models.Actor{}, err
	}
	status := "active"
	if input.Status.Set {
		if input.Status.Value == nil {
			return models.Actor{}, errors.New("status cannot be null")
		}
		status, err = validateActorStatus(*input.Status.Value)
		if err != nil {
			return models.Actor{}, err
		}
	}
	notes := ""
	if input.Notes.Set {
		if input.Notes.Value == nil {
			return models.Actor{}, errors.New("notes cannot be null")
		}
		notes, err = validateActorNotes(*input.Notes.Value)
		if err != nil {
			return models.Actor{}, err
		}
	}
	metadataJSON := "{}"
	if input.Metadata.Set {
		metadataJSON, err = validateActorMetadata(input.Metadata.Raw)
		if err != nil {
			return models.Actor{}, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.Actor{
		ID: uuid.NewString(), Type: actorType, DisplayName: displayName,
		Status: status, IsBuiltin: false, Notes: notes, MetadataJSON: metadataJSON,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func actorUpdates(tx *gorm.DB, actor models.Actor, input updateActorRequest) (map[string]any, error) {
	if actor.Type == "system" {
		return nil, newProjectRequestError(http.StatusForbidden, "ACTOR_NOT_EDITABLE", "System actors cannot be edited")
	}
	if actor.Type == "agent" {
		return nil, newProjectRequestError(http.StatusForbidden, "ACTOR_NOT_EDITABLE", "Agent actors are not editable in v0.1")
	}
	if actor.Type == "owner" && (input.Status.Set || input.Notes.Set || input.Metadata.Set) {
		return nil, newProjectRequestError(
			http.StatusForbidden,
			"ACTOR_FIELD_NOT_EDITABLE",
			"Only the owner display name can be edited",
		)
	}

	updates := make(map[string]any)
	if input.DisplayName.Set {
		if input.DisplayName.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "display_name cannot be null")
		}
		displayName, err := validateActorDisplayName(*input.DisplayName.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		updates["display_name"] = displayName
	}
	if input.Notes.Set {
		if input.Notes.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "notes cannot be null")
		}
		notes, err := validateActorNotes(*input.Notes.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		updates["notes"] = notes
	}
	if input.Metadata.Set {
		metadataJSON, err := validateActorMetadata(input.Metadata.Raw)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		updates["metadata_json"] = metadataJSON
	}
	if input.Status.Set {
		if input.Status.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "status cannot be null")
		}
		status, err := validateActorStatus(*input.Status.Value)
		if err != nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		if actor.Status == "active" && status == "inactive" {
			var activeAssignments int64
			if err := tx.Table("task_assignments").
				Where("actor_id = ? AND unassigned_at IS NULL", actor.ID).
				Count(&activeAssignments).Error; err != nil {
				return nil, err
			}
			if activeAssignments > 0 {
				return nil, actorHasActiveAssignmentsError()
			}
			var plannedFollowups int64
			if err := tx.Table("client_followups").
				Where("assigned_actor_id = ? AND status = 'planned'", actor.ID).
				Count(&plannedFollowups).Error; err != nil {
				return nil, err
			}
			if plannedFollowups > 0 {
				return nil, actorHasPlannedClientFollowupsError()
			}
		}
		updates["status"] = status
	}
	if len(updates) == 0 {
		return nil, newProjectRequestError(
			http.StatusUnprocessableEntity,
			"VALIDATION_ERROR",
			"at least one editable actor field is required",
		)
	}
	return updates, nil
}

func validateActorDisplayName(value string) (string, error) {
	displayName := strings.TrimSpace(value)
	if length := utf8.RuneCountInString(displayName); length < 1 || length > 100 {
		return "", errors.New("display_name must contain 1 to 100 characters")
	}
	for _, character := range displayName {
		if unicode.IsControl(character) {
			return "", errors.New("display_name cannot contain control characters")
		}
	}
	return displayName, nil
}

func validateActorStatus(value string) (string, error) {
	status := strings.TrimSpace(value)
	if _, valid := validActorStatuses[status]; !valid {
		return "", errors.New("status must be active or inactive")
	}
	return status, nil
}

func validateActorNotes(value string) (string, error) {
	if utf8.RuneCountInString(value) > 2_000 {
		return "", errors.New("notes cannot exceed 2000 characters")
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t' {
			return "", errors.New("notes cannot contain unsupported control characters")
		}
	}
	return value, nil
}

func validateActorMetadata(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", errors.New("metadata must be a JSON object")
	}
	if len(trimmed) > maxActorMetadataBytes {
		return "", fmt.Errorf("metadata cannot exceed %d bytes", maxActorMetadataBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("metadata must be valid JSON")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("metadata must contain one JSON value")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return "", errors.New("metadata must be a JSON object")
	}
	keyCount := 0
	if err := validateActorMetadataValue(object, 1, &keyCount); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return "", errors.New("metadata must be valid JSON")
	}
	if len(canonical) > maxActorMetadataBytes {
		return "", fmt.Errorf("metadata cannot exceed %d bytes", maxActorMetadataBytes)
	}
	return string(canonical), nil
}

func validateActorMetadataValue(value any, depth int, keyCount *int) error {
	if depth > maxActorMetadataDepth {
		return fmt.Errorf("metadata cannot exceed %d nesting levels", maxActorMetadataDepth)
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			(*keyCount)++
			if *keyCount > maxActorMetadataKeys {
				return fmt.Errorf("metadata cannot contain more than %d keys", maxActorMetadataKeys)
			}
			if length := utf8.RuneCountInString(key); length < 1 || length > 100 {
				return errors.New("metadata keys must contain 1 to 100 characters")
			}
			for _, character := range key {
				if unicode.IsControl(character) {
					return errors.New("metadata keys cannot contain control characters")
				}
			}
			if actorMetadataKeyIsSensitive(key) {
				return fmt.Errorf("metadata key %q may contain sensitive data", key)
			}
			if err := validateActorMetadataValue(child, depth+1, keyCount); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateActorMetadataValue(child, depth+1, keyCount); err != nil {
				return err
			}
		}
	}
	return nil
}

func actorMetadataKeyIsSensitive(key string) bool {
	words := actorMetadataKeyWords(key)
	for _, word := range words {
		switch word {
		case "password", "passwd", "secret", "token", "credential", "credentials", "authorization", "cookie":
			return true
		}
	}
	for index := 0; index+1 < len(words); index++ {
		pair := words[index] + "_" + words[index+1]
		if pair == "api_key" || pair == "private_key" || pair == "session_id" {
			return true
		}
	}
	return false
}

func actorMetadataKeyWords(key string) []string {
	words := make([]string, 0, 4)
	var current strings.Builder
	var previousWasLowerOrDigit bool
	flush := func() {
		if current.Len() > 0 {
			words = append(words, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for _, character := range key {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			previousWasLowerOrDigit = false
			continue
		}
		if unicode.IsUpper(character) && previousWasLowerOrDigit {
			flush()
		}
		current.WriteRune(character)
		previousWasLowerOrDigit = unicode.IsLower(character) || unicode.IsDigit(character)
	}
	flush()
	return words
}

func actorCreateRequestHash(actor models.Actor) (string, error) {
	payload := struct {
		Type        string          `json:"type"`
		DisplayName string          `json:"display_name"`
		Status      string          `json:"status"`
		Notes       string          `json:"notes"`
		Metadata    json.RawMessage `json:"metadata"`
	}{
		Type: actor.Type, DisplayName: actor.DisplayName, Status: actor.Status,
		Notes: actor.Notes, Metadata: json.RawMessage(actor.MetadataJSON),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode actor request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func actorID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(id)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ACTOR_ID", "Actor id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func applyActorSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.
			Order("CASE actors.type WHEN 'owner' THEN 0 WHEN 'person' THEN 1 WHEN 'system' THEN 2 ELSE 3 END ASC").
			Order("LOWER(actors.display_name) ASC").
			Order("actors.id ASC"), true
	}
	allowed := map[string]string{
		"type": "actors.type", "display_name": "LOWER(actors.display_name)", "status": "actors.status",
		"created_at": "actors.created_at", "updated_at": "actors.updated_at",
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
	return query.Order("actors.id ASC"), true
}

func loadActor(db *gorm.DB, id string) (models.Actor, error) {
	var actor models.Actor
	err := db.First(&actor, "id = ?", id).Error
	return actor, err
}

func actorResponsesFromModels(actors []models.Actor) ([]actorResponse, error) {
	responses := make([]actorResponse, len(actors))
	for index := range actors {
		response, err := actorResponseFromModel(actors[index])
		if err != nil {
			return nil, err
		}
		responses[index] = response
	}
	return responses, nil
}

func actorResponseFromModel(actor models.Actor) (actorResponse, error) {
	metadataJSON, err := validateActorMetadata(json.RawMessage(actor.MetadataJSON))
	if err != nil {
		return actorResponse{}, fmt.Errorf("decode actor metadata: %w", err)
	}
	return actorResponse{
		ID: actor.ID, Type: actor.Type, DisplayName: actor.DisplayName,
		Status: actor.Status, IsBuiltin: actor.IsBuiltin, Notes: actor.Notes,
		Metadata: json.RawMessage(metadataJSON), Version: actor.Version,
		CreatedAt: normalizeTimestamp(actor.CreatedAt), UpdatedAt: normalizeTimestamp(actor.UpdatedAt),
	}, nil
}

func actorHasActiveAssignmentsError() error {
	return newProjectRequestError(
		http.StatusConflict,
		"ACTOR_HAS_ACTIVE_ASSIGNMENTS",
		"Reassign or end this actor's active assignments before deactivating it",
	)
}

func actorHasActiveClientLinksError() error {
	return newProjectRequestError(
		http.StatusConflict,
		"ACTOR_HAS_ACTIVE_CLIENT_LINKS",
		"Unlink this actor from every active client contact before deactivating it",
	)
}

func actorHasPlannedClientFollowupsError() error {
	return newProjectRequestError(
		http.StatusConflict,
		"ACTOR_HAS_PLANNED_CLIENT_FOLLOWUPS",
		"Reassign, complete, skip, or cancel this actor's planned client followups before deactivating it",
	)
}

func recordActorWorkflowEvent(
	tx *gorm.DB,
	action string,
	actorID string,
	previous *actorResponse,
	current actorResponse,
	requestID string,
	createdAt string,
) error {
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("encode current actor workflow event: %w", err)
	}
	var previousJSON any
	if previous != nil {
		previousBytes, err := json.Marshal(previous)
		if err != nil {
			return fmt.Errorf("encode previous actor workflow event: %w", err)
		}
		previousJSON = string(previousBytes)
	}
	event := map[string]any{
		"id": uuid.NewString(), "aggregate_type": "actor", "aggregate_id": actorID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": requestID,
		"previous_json": previousJSON, "current_json": string(currentBytes), "created_at": createdAt,
	}
	if err := tx.Table("workflow_events").Create(event).Error; err != nil {
		return fmt.Errorf("record actor workflow event: %w", err)
	}
	return nil
}
