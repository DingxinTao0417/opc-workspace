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

var aiClientChangeFields = map[string]bool{
	"name": true, "contact_name": true, "email": true,
	"phone": true, "notes": true, "status": true,
}

func addAIClientSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "client.create", "client.update", "client.delete")
	properties["client_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Required for client.update/delete; read the exact Client ID and expected_version with workspace_get first",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Client create/update/delete require work + clients + actions and always remain human-confirmed. Create requires name; update accepts only supplied profile fields. delete requires an inactive Client, empty changes and a separate human deletion confirmation; it detaches Projects and permanently removes local Client activities, contact-link history and attachments after blocker checks. contact_name/email/phone/notes may be null to clear. status is active/lead/inactive. Contact details must come from explicit user input and never be inferred. Notes may come from explicit user input or an authorized, untruncated workspace_get client result; never replace a truncated note. The model cannot inspect attachments, contact anyone or send messages."
	fields := changes["properties"].(map[string]any)
	fields["name"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 200}
	fields["contact_name"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 200}
	fields["email"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 320}
	fields["phone"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 50}
	fields["notes"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 10000}
	fields["status"] = map[string]any{"type": "string", "enum": []string{
		"active", "lead", "inactive",
		"planned", "achieved", "pending", "confirmed",
		"draft", "in_review", "scheduled", "cancelled",
	}}
}

func parseAIClientAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for key := range raw {
		if key != "action" && key != "client_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("client actions only accept their Client target")
		}
	}
	creating := input.Action == "client.create"
	deleting := input.Action == "client.delete"
	if !creating && input.Action != "client.update" && !deleting {
		return input, errors.New("unsupported client action")
	}
	if creating {
		if _, present := raw["client_id"]; present || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a Client ID/version")
		}
	} else {
		parsed, err := uuid.Parse(strings.TrimSpace(input.ClientID))
		if err != nil || parsed.String() != input.ClientID || input.ExpectedVersion < 1 {
			return input, errors.New("use a real Client ID and expected_version from workspace_get")
		}
	}
	if deleting {
		if len(fields) != 0 {
			return input, errors.New("client deletion requires empty changes; consent belongs to the human decision")
		}
		input.Changes = json.RawMessage(`{}`)
		return input, nil
	}
	if len(fields) == 0 {
		return input, errors.New("at least one client field is required")
	}
	for field := range fields {
		if !aiClientChangeFields[field] {
			return input, errors.New("unsupported client field: " + field)
		}
	}
	if creating {
		var create createClientRequest
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		client, err := clientFromCreateRequest(create)
		if err != nil {
			return input, err
		}
		canonical := aiClientActionFields(client)
		for field := range canonical {
			if !aiClientChangeFields[field] || !hasRawField(fields, field) {
				delete(canonical, field)
			}
		}
		input.Changes, _ = json.Marshal(canonical)
	} else {
		var update updateClientRequest
		if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
			return input, err
		}
		updates, err := clientUpdates(update)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(updates)
	}
	return input, nil
}

func hasRawField(fields map[string]json.RawMessage, field string) bool {
	_, present := fields[field]
	return present
}

func aiClientActionFields(client models.Client) map[string]any {
	return map[string]any{
		"name": client.Name, "contact_name": client.ContactName,
		"email": client.Email, "phone": client.Phone,
		"notes": client.Notes, "status": client.Status,
	}
}

func previewAIClientAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "client.create" {
		var create createClientRequest
		_ = json.Unmarshal(input.Changes, &create)
		client, err := clientFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		preview.Label = client.Name
		preview.After = aiClientActionFields(client)
		return preview, nil
	}
	var client models.Client
	if err := tx.First(&client, "id = ?", input.ClientID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
		}
		return preview, err
	}
	if client.Version != input.ExpectedVersion {
		return preview, clientVersionConflict()
	}
	if input.Action == "client.delete" {
		_, impact, err := loadClientDeletionImpact(tx, client.ID, client.Version)
		if err != nil {
			return preview, err
		}
		preview.Label = client.Name
		preview.Before = map[string]any{"status": client.Status, "client_version": client.Version}
		preview.After = map[string]any{
			"client_deleted":                  true,
			"client_projects_detached":        impact.Projects,
			"client_activities_deleted":       impact.Activities,
			"client_contact_history_deleted":  impact.ActorLinks,
			"client_attachments_deleted":      impact.Attachments,
			"client_attachment_files_deleted": impact.AttachmentFiles,
		}
		return preview, nil
	}
	var update updateClientRequest
	_ = json.Unmarshal(input.Changes, &update)
	updates, err := clientUpdates(update)
	if err != nil {
		return preview, err
	}
	preview.Label = client.Name
	before := aiClientActionFields(client)
	for field, value := range updates {
		preview.Before[field], preview.After[field] = before[field], value
	}
	return preview, nil
}

func executeAIClientAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, error) {
	if input.Action == "client.create" {
		var create createClientRequest
		_ = json.Unmarshal(input.Changes, &create)
		client, err := clientFromCreateRequest(create)
		if err != nil {
			return aiActionResult{}, err
		}
		response, err := createClientInTransaction(tx, client, now)
		return aiActionResult{ID: response.ID, Version: response.Version}, err
	}
	if input.Action == "client.delete" {
		return aiActionResult{}, errors.New("client deletion requires the compensated approval command")
	}
	var update updateClientRequest
	_ = json.Unmarshal(input.Changes, &update)
	response, err := updateClientInTransaction(tx, input.ClientID, input.ExpectedVersion, update, now)
	return aiActionResult{ID: response.ID, Version: response.Version}, err
}
