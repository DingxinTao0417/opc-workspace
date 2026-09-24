package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiClientContactLinkChanges struct {
	ActorID string `json:"actor_id"`
}

type aiClientContactUnlinkChanges struct {
	Reason string `json:"reason"`
}

func addAIClientContactSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "client_contact.link", "client_contact.unlink")
	properties["client_actor_link_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "client_contact.unlink: active contact_link_id from workspace_get client",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Client contact link/unlink require work+clients+actions and confirmation. Link: only actor_id of an existing active person from workspace_task_options. Unlink: only reason; keeps person/history. Read Client/contact first. No person creation or messaging."
	fields := changes["properties"].(map[string]any)
	fields["actor_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Client contact link: existing active person Actor ID"}
}

func parseAIClientContactAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for key := range raw {
		if key != "action" && key != "client_id" && key != "client_actor_link_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("Client contact actions only accept their Client/contact-link targets")
		}
	}
	clientID, err := uuid.Parse(strings.TrimSpace(input.ClientID))
	if err != nil || clientID.String() != input.ClientID || input.ExpectedVersion < 1 {
		return input, errors.New("use a real Client ID and expected_version from workspace_get")
	}
	switch input.Action {
	case "client_contact.link":
		if _, present := raw["client_actor_link_id"]; present {
			return input, errors.New("link does not accept client_actor_link_id")
		}
		if len(fields) != 1 || !hasRawField(fields, "actor_id") {
			return input, errors.New("link changes require only actor_id")
		}
		var changes aiClientContactLinkChanges
		if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
			return input, err
		}
		actorID, err := uuid.Parse(strings.TrimSpace(changes.ActorID))
		if err != nil || actorID.String() != changes.ActorID {
			return input, errors.New("actor_id must be a canonical UUID from workspace_task_options")
		}
		input.Changes, _ = json.Marshal(changes)
	case "client_contact.unlink":
		linkID, err := uuid.Parse(strings.TrimSpace(input.ClientActorLinkID))
		if err != nil || linkID.String() != input.ClientActorLinkID {
			return input, errors.New("use the active client_actor_link_id from workspace_get")
		}
		if len(fields) != 1 || !hasRawField(fields, "reason") {
			return input, errors.New("unlink changes require only reason")
		}
		var changes aiClientContactUnlinkChanges
		if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
			return input, err
		}
		reason, err := validateArtifactDeleteReason(changes.Reason)
		if err != nil {
			return input, err
		}
		changes.Reason = reason
		input.Changes, _ = json.Marshal(changes)
	default:
		return input, errors.New("unsupported Client contact action")
	}
	return input, nil
}

func previewAIClientContactAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	var client models.Client
	if err := tx.Select("id", "name", "status", "version").First(&client, "id = ?", input.ClientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
		}
		return preview, err
	}
	if client.Version != input.ExpectedVersion {
		return preview, clientVersionConflict()
	}
	preview.Label = client.Name
	if input.Action == "client_contact.link" {
		var changes aiClientContactLinkChanges
		_ = json.Unmarshal(input.Changes, &changes)
		var actor models.Actor
		if err := tx.Select("id", "type", "display_name", "status", "version").First(&actor, "id = ?", changes.ActorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return preview, newProjectRequestError(http.StatusNotFound, "ACTOR_NOT_FOUND", "Actor not found")
			}
			return preview, err
		}
		if actor.Type != "person" || actor.Status != "active" {
			return preview, newProjectRequestError(http.StatusConflict, "CLIENT_LINK_ACTOR_UNAVAILABLE", "The selected actor must be an active local person")
		}
		var count int64
		if err := tx.Model(&models.ClientActorLink{}).Where("client_id = ? AND role = 'contact' AND unlinked_at IS NULL", client.ID).Count(&count).Error; err != nil {
			return preview, err
		}
		if count != 0 {
			return preview, newProjectRequestError(http.StatusConflict, "CLIENT_CONTACT_ACTOR_ALREADY_LINKED", "This client already has an active contact actor")
		}
		preview.Before["link_state"] = "none"
		preview.After = aiClientContactPreviewFields(client, actor, "active")
		return preview, nil
	}

	row, err := loadClientActorLinkRow(tx, input.ClientActorLinkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(http.StatusNotFound, "CLIENT_ACTOR_LINK_NOT_FOUND", "Client actor link not found")
		}
		return preview, err
	}
	if row.ClientID != client.ID {
		return preview, newProjectRequestError(http.StatusConflict, "CLIENT_ACTOR_LINK_CLIENT_MISMATCH", "Contact link does not belong to the selected client")
	}
	if row.UnlinkedAt != nil {
		return preview, newProjectRequestError(http.StatusConflict, "CLIENT_ACTOR_LINK_ALREADY_UNLINKED", "Client actor link is already unlinked")
	}
	actor := models.Actor{ID: row.ActorID, Type: row.ActorType, DisplayName: row.ActorDisplayName, Status: row.ActorStatus, Version: row.ActorVersion}
	preview.Before = aiClientContactPreviewFields(client, actor, "active")
	preview.After = aiClientContactPreviewFields(client, actor, "unlinked")
	var changes aiClientContactUnlinkChanges
	_ = json.Unmarshal(input.Changes, &changes)
	preview.After["reason"] = changes.Reason
	return preview, nil
}

func aiClientContactPreviewFields(client models.Client, actor models.Actor, state string) map[string]any {
	return map[string]any{
		"client_id": client.ID, "client_name": client.Name, "client_status": client.Status,
		"actor_id": actor.ID, "actor_name": actor.DisplayName, "actor_type": actor.Type,
		"actor_status": actor.Status, "actor_version": actor.Version,
		"role": "contact", "link_state": state,
	}
}

func (a *API) executeAIClientContactAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (aiActionResult, error) {
	if input.Action == "client_contact.link" {
		var changes aiClientContactLinkChanges
		_ = json.Unmarshal(input.Changes, &changes)
		normalized := normalizedClientActorLinkInput{ActorID: &changes.ActorID, Role: "contact"}
		response, err := a.createClientActorLinkInTransaction(tx, input.ClientID, input.ExpectedVersion, normalized, requestID, now)
		return aiActionResult{ID: response.ID, Version: response.ClientVersion}, err
	}
	var changes aiClientContactUnlinkChanges
	_ = json.Unmarshal(input.Changes, &changes)
	response, err := unlinkClientActorLinkInTransaction(tx, input.ClientActorLinkID, input.ExpectedVersion, changes.Reason, now)
	return aiActionResult{ID: response.ID, Version: response.ClientVersion}, err
}
