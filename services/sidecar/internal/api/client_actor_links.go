package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type createClientActorLinkPersonRequest struct {
	DisplayName string `json:"display_name"`
	Notes       string `json:"notes"`
}

type createClientActorLinkRequest struct {
	ActorID      string                              `json:"actor_id"`
	CreatePerson *createClientActorLinkPersonRequest `json:"create_person"`
	Role         string                              `json:"role"`
}

type deleteClientActorLinkRequest struct {
	Reason string `json:"reason"`
}

type clientActorLinkActorResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
}

type clientActorLinkResponse struct {
	ID            string                       `json:"id"`
	ClientID      string                       `json:"client_id"`
	Role          string                       `json:"role"`
	Actor         clientActorLinkActorResponse `json:"actor"`
	LinkedBy      clientActivityActorResponse  `json:"linked_by"`
	LinkedAt      string                       `json:"linked_at"`
	UnlinkedAt    *string                      `json:"unlinked_at"`
	UnlinkedBy    *clientActivityActorResponse `json:"unlinked_by"`
	UnlinkReason  *string                      `json:"unlink_reason"`
	ClientVersion int64                        `json:"client_version"`
}

type clientActorLinkRow struct {
	models.ClientActorLink `gorm:"embedded"`
	ActorType              string  `gorm:"column:actor_type"`
	ActorDisplayName       string  `gorm:"column:actor_display_name"`
	ActorStatus            string  `gorm:"column:actor_status"`
	ActorVersion           int64   `gorm:"column:actor_version"`
	LinkedByType           string  `gorm:"column:linked_by_type"`
	LinkedByDisplayName    string  `gorm:"column:linked_by_display_name"`
	UnlinkedByType         *string `gorm:"column:unlinked_by_type"`
	UnlinkedByDisplayName  *string `gorm:"column:unlinked_by_display_name"`
	ClientVersion          int64   `gorm:"column:client_version"`
}

type normalizedClientActorLinkInput struct {
	ActorID           *string `json:"actor_id"`
	CreateDisplayName *string `json:"create_display_name"`
	CreateNotes       *string `json:"create_notes"`
	Role              string  `json:"role"`
}

type clientActorLinkListState string

const (
	clientActorLinkStateActive   clientActorLinkListState = "active"
	clientActorLinkStateUnlinked clientActorLinkListState = "unlinked"
	clientActorLinkStateAll      clientActorLinkListState = "all"
)

func (a *API) listClientActorLinks(c *gin.Context) {
	clientIDValue, ok := clientID(c)
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
	state, errorMessage, valid := parseClientActorLinkListState(c)
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", errorMessage)
		return
	}

	var total int64
	var rows []clientActorLinkRow
	var clientVersion int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT version FROM clients WHERE id = ?", clientIDValue).Row().Scan(&clientVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		query := applyClientActorLinkListState(
			tx.Table("client_actor_links").Where("client_actor_links.client_id = ?", clientIDValue),
			state,
		)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select(clientActorLinkSelectColumns).
			Joins("JOIN actors linked_actor ON linked_actor.id = client_actor_links.actor_id").
			Joins("JOIN actors linked_by ON linked_by.id = client_actor_links.linked_by_actor_id").
			Joins("LEFT JOIN actors unlinked_by ON unlinked_by.id = client_actor_links.unlinked_by_actor_id").
			Joins("JOIN clients ON clients.id = client_actor_links.client_id").
			Order("client_actor_links.linked_at DESC").
			Order("client_actor_links.id ASC").
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
	items := make([]clientActorLinkResponse, len(rows))
	for index := range rows {
		items[index] = clientActorLinkResponseFromRow(rows[index])
	}
	setProjectETag(c, clientVersion)
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": gin.H{
		"page": page, "page_size": pageSize, "total": total, "client_version": clientVersion,
	}})
}

func (a *API) createClientActorLink(c *gin.Context) {
	clientIDValue, ok := clientID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input createClientActorLinkRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	normalized, err := normalizeClientActorLinkInput(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"client_id": clientIDValue, "expected_version": expectedVersion, "input": normalized,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/clients/%s/actor-links", clientIDValue)
	statusCode := http.StatusCreated
	replayed := false
	var response clientActorLinkResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, replayStatus, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		var client models.Client
		if err := tx.Select("id", "version").First(&client, "id = ?", clientIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		if client.Version != expectedVersion {
			return clientVersionConflict()
		}
		actorIDValue, err := a.resolveClientLinkActor(tx, normalized, requestIDFromContext(c))
		if err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		link := models.ClientActorLink{
			ID: uuid.NewString(), ClientID: clientIDValue, ActorID: actorIDValue, Role: normalized.Role,
			LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: now,
		}
		if err := tx.Create(&link).Error; err != nil {
			return mapClientActorLinkConstraintError(err)
		}
		row, err := loadClientActorLinkRow(tx, link.ID)
		if err != nil {
			return err
		}
		response = clientActorLinkResponseFromRow(row)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, link.ID, requestHash, http.StatusCreated, response, now)
	})
	if err != nil {
		if writeProjectRequestError(c, mapClientActorLinkConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.ClientVersion)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) deleteClientActorLink(c *gin.Context) {
	id, ok := clientActorLinkID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Client actor unlink requires confirm=true")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input deleteClientActorLinkRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := validateArtifactDeleteReason(input.Reason)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"link_id": id, "expected_version": expectedVersion, "confirm": true, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("DELETE /api/v1/client-actor-links/%s", id)
	replayed := false
	var response clientActorLinkResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			return nil
		}
		row, err := loadClientActorLinkRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_ACTOR_LINK_NOT_FOUND", "Client actor link not found")
			}
			return err
		}
		if row.UnlinkedAt != nil {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTOR_LINK_ALREADY_UNLINKED", "Client actor link is already unlinked")
		}
		if row.ClientVersion != expectedVersion {
			return clientVersionConflict()
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.ClientActorLink{}).
			Where("id = ? AND unlinked_at IS NULL", id).
			Updates(map[string]any{
				"unlinked_at": now, "unlinked_by_actor_id": models.BuiltinOwnerActorID, "unlink_reason": reason,
			})
		if result.Error != nil {
			return mapClientActorLinkConstraintError(result.Error)
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ACTOR_LINK_ALREADY_UNLINKED", "Client actor link is already unlinked")
		}
		row, err = loadClientActorLinkRow(tx, id)
		if err != nil {
			return err
		}
		response = clientActorLinkResponseFromRow(row)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, row.ClientID, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
		if writeProjectRequestError(c, mapClientActorLinkConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.ClientVersion)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func normalizeClientActorLinkInput(input createClientActorLinkRequest) (normalizedClientActorLinkInput, error) {
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "contact"
	}
	if role != "contact" {
		return normalizedClientActorLinkInput{}, errors.New("role must be contact")
	}
	actorIDValue := strings.TrimSpace(input.ActorID)
	if (actorIDValue == "") == (input.CreatePerson == nil) {
		return normalizedClientActorLinkInput{}, errors.New("provide exactly one of actor_id or create_person")
	}
	normalized := normalizedClientActorLinkInput{Role: role}
	if actorIDValue != "" {
		parsed, err := uuid.Parse(actorIDValue)
		if err != nil {
			return normalizedClientActorLinkInput{}, errors.New("actor_id must be a UUID")
		}
		canonical := parsed.String()
		normalized.ActorID = &canonical
		return normalized, nil
	}
	displayName, err := validateActorDisplayName(input.CreatePerson.DisplayName)
	if err != nil {
		return normalizedClientActorLinkInput{}, err
	}
	notes, err := validateActorNotes(input.CreatePerson.Notes)
	if err != nil {
		return normalizedClientActorLinkInput{}, err
	}
	normalized.CreateDisplayName = &displayName
	normalized.CreateNotes = &notes
	return normalized, nil
}

func (a *API) resolveClientLinkActor(tx *gorm.DB, input normalizedClientActorLinkInput, requestID string) (string, error) {
	if input.ActorID != nil {
		var actor models.Actor
		if err := tx.Select("id", "type", "status").First(&actor, "id = ?", *input.ActorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", newProjectRequestError(http.StatusNotFound, "ACTOR_NOT_FOUND", "Actor not found")
			}
			return "", err
		}
		if actor.Type != "person" || actor.Status != "active" {
			return "", newProjectRequestError(http.StatusConflict, "CLIENT_LINK_ACTOR_UNAVAILABLE", "The selected actor must be an active local person")
		}
		return actor.ID, nil
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	actor := models.Actor{
		ID: uuid.NewString(), Type: "person", DisplayName: *input.CreateDisplayName, Status: "active",
		IsBuiltin: false, Notes: *input.CreateNotes, MetadataJSON: "{}", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&actor).Error; err != nil {
		return "", fmt.Errorf("create linked person actor: %w", err)
	}
	actorResponseValue, err := actorResponseFromModel(actor)
	if err != nil {
		return "", err
	}
	if err := recordActorWorkflowEvent(tx, "actor_created", actor.ID, nil, actorResponseValue, requestID, now); err != nil {
		return "", err
	}
	return actor.ID, nil
}

func parseClientActorLinksIncludeUnlinked(raw string) (bool, bool) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}

func parseClientActorLinkListState(c *gin.Context) (clientActorLinkListState, string, bool) {
	query := c.Request.URL.Query()
	_, statePresent := query["state"]
	_, includeUnlinkedPresent := query["include_unlinked"]
	if statePresent && includeUnlinkedPresent {
		return "", "state and include_unlinked cannot be combined", false
	}
	if statePresent {
		switch strings.TrimSpace(c.Query("state")) {
		case string(clientActorLinkStateActive):
			return clientActorLinkStateActive, "", true
		case string(clientActorLinkStateUnlinked):
			return clientActorLinkStateUnlinked, "", true
		case string(clientActorLinkStateAll):
			return clientActorLinkStateAll, "", true
		default:
			return "", "state must be active, unlinked, or all", false
		}
	}

	includeUnlinked, valid := parseClientActorLinksIncludeUnlinked(c.Query("include_unlinked"))
	if !valid {
		return "", "include_unlinked must be true or false", false
	}
	if includeUnlinked {
		return clientActorLinkStateAll, "", true
	}
	return clientActorLinkStateActive, "", true
}

func applyClientActorLinkListState(query *gorm.DB, state clientActorLinkListState) *gorm.DB {
	switch state {
	case clientActorLinkStateActive:
		return query.Where("client_actor_links.unlinked_at IS NULL")
	case clientActorLinkStateUnlinked:
		return query.Where("client_actor_links.unlinked_at IS NOT NULL")
	default:
		return query
	}
}

func clientActorLinkID(c *gin.Context) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_CLIENT_ACTOR_LINK_ID", "Client actor link id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

const clientActorLinkSelectColumns = `
	client_actor_links.id,
	client_actor_links.client_id,
	client_actor_links.actor_id,
	client_actor_links.role,
	client_actor_links.linked_by_actor_id,
	client_actor_links.linked_at,
	client_actor_links.unlinked_at,
	client_actor_links.unlinked_by_actor_id,
	client_actor_links.unlink_reason,
	linked_actor.type AS actor_type,
	linked_actor.display_name AS actor_display_name,
	linked_actor.status AS actor_status,
	linked_actor.version AS actor_version,
	linked_by.type AS linked_by_type,
	linked_by.display_name AS linked_by_display_name,
	unlinked_by.type AS unlinked_by_type,
	unlinked_by.display_name AS unlinked_by_display_name,
	clients.version AS client_version
`

func loadClientActorLinkRow(db *gorm.DB, id string) (clientActorLinkRow, error) {
	var row clientActorLinkRow
	err := db.Table("client_actor_links").Select(clientActorLinkSelectColumns).
		Joins("JOIN actors linked_actor ON linked_actor.id = client_actor_links.actor_id").
		Joins("JOIN actors linked_by ON linked_by.id = client_actor_links.linked_by_actor_id").
		Joins("LEFT JOIN actors unlinked_by ON unlinked_by.id = client_actor_links.unlinked_by_actor_id").
		Joins("JOIN clients ON clients.id = client_actor_links.client_id").
		Where("client_actor_links.id = ?", id).Take(&row).Error
	return row, err
}

func clientActorLinkResponseFromRow(row clientActorLinkRow) clientActorLinkResponse {
	response := clientActorLinkResponse{
		ID: row.ID, ClientID: row.ClientID, Role: row.Role,
		Actor: clientActorLinkActorResponse{
			ID: row.ActorID, Type: row.ActorType, DisplayName: row.ActorDisplayName,
			Status: row.ActorStatus, Version: row.ActorVersion,
		},
		LinkedBy: clientActivityActorResponse{
			ID: row.LinkedByActorID, Type: row.LinkedByType, DisplayName: row.LinkedByDisplayName,
		},
		LinkedAt: normalizeTimestamp(row.LinkedAt), UnlinkedAt: row.UnlinkedAt,
		UnlinkReason: row.UnlinkReason, ClientVersion: row.ClientVersion,
	}
	if response.UnlinkedAt != nil {
		normalized := normalizeTimestamp(*response.UnlinkedAt)
		response.UnlinkedAt = &normalized
	}
	if row.UnlinkedByActorID != nil && row.UnlinkedByType != nil && row.UnlinkedByDisplayName != nil {
		response.UnlinkedBy = &clientActivityActorResponse{
			ID: *row.UnlinkedByActorID, Type: *row.UnlinkedByType, DisplayName: *row.UnlinkedByDisplayName,
		}
	}
	return response
}

func mapClientActorLinkConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "ux_client_actor_links_active_role"),
		strings.Contains(message, "UNIQUE constraint failed: client_actor_links.client_id, client_actor_links.role"):
		return newProjectRequestError(http.StatusConflict, "CLIENT_CONTACT_ACTOR_ALREADY_LINKED", "This client already has an active contact actor")
	case strings.Contains(message, "CLIENT_LINK_ACTOR_NOT_ACTIVE_PERSON"):
		return newProjectRequestError(http.StatusConflict, "CLIENT_LINK_ACTOR_UNAVAILABLE", "The selected actor must be an active local person")
	case strings.Contains(message, "CLIENT_ACTOR_LINK_HISTORY_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "CLIENT_ACTOR_LINK_IMMUTABLE", "Client actor link history cannot be changed")
	default:
		return err
	}
}
