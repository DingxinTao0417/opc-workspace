package api

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiPersonCreateChanges struct {
	DisplayName string              `json:"display_name"`
	Notes       nullableStringPatch `json:"notes"`
}

var aiPersonChangeFields = map[string]bool{
	"display_name": true,
	"notes":        true,
	"status":       true,
}

func addAIPersonSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "person.create", "person.update")
	properties["actor_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "person.update only: exact active or inactive person ID from workspace_search/get type person",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Person create requires display_name and creates an active local identity; update accepts display_name, notes, status. Notes must come from explicit user input because person reads never expose them. No metadata, owner/system/agent edits, delete, message or external account."
	fields := changes["properties"].(map[string]any)
	fields["display_name"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 100}
}

func parseAIPersonAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{"task_id", "project_id", "client_id", "client_actor_link_id"} {
		if _, present := raw[key]; present {
			return input, errors.New("person actions only accept actor_id as a target")
		}
	}
	for field := range fields {
		if !aiPersonChangeFields[field] {
			return input, errors.New("unsupported person field: " + field)
		}
	}
	switch input.Action {
	case "person.create":
		if input.ActorID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("person.create does not accept an actor ID/version")
		}
		if _, present := fields["status"]; present {
			return input, errors.New("person.create always creates an active local person")
		}
		var changes aiPersonCreateChanges
		if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
			return input, err
		}
		request := createActorRequest{Type: "person", DisplayName: changes.DisplayName, Notes: changes.Notes}
		if _, err := actorFromCreateRequest(request); err != nil {
			return input, err
		}
	case "person.update":
		if _, err := uuid.Parse(input.ActorID); err != nil || input.ExpectedVersion < 1 {
			return input, errors.New("use a real person ID and expected_version from workspace_get")
		}
		if len(fields) == 0 {
			return input, errors.New("person.update requires at least one changed field")
		}
		var changes updateActorRequest
		if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
			return input, err
		}
	default:
		return input, errors.New("unsupported person action")
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func aiPersonFields(actor models.Actor) map[string]any {
	return map[string]any{
		"display_name": actor.DisplayName,
		"notes":        actor.Notes,
		"status":       actor.Status,
		"person_type":  actor.Type,
	}
}

func previewAIPersonAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "person.create" {
		var changes aiPersonCreateChanges
		_ = json.Unmarshal(input.Changes, &changes)
		request := createActorRequest{Type: "person", DisplayName: changes.DisplayName, Notes: changes.Notes}
		actor, err := actorFromCreateRequest(request)
		if err != nil {
			return preview, err
		}
		preview.Label, preview.After = actor.DisplayName, aiPersonFields(actor)
		return preview, nil
	}
	actor, err := loadActor(tx, input.ActorID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(404, "ACTOR_NOT_FOUND", "Person not found")
		}
		return preview, err
	}
	if actor.Type != "person" || actor.IsBuiltin || actor.AgentAdapterID != nil {
		return preview, newProjectRequestError(403, "ACTOR_NOT_EDITABLE", "Only local person identities can be edited")
	}
	if actor.Version != input.ExpectedVersion {
		return preview, taskVersionConflict()
	}
	var update updateActorRequest
	_ = json.Unmarshal(input.Changes, &update)
	updates, err := actorUpdates(tx, actor, update)
	if err != nil {
		return preview, err
	}
	before := aiPersonFields(actor)
	preview.Label = actor.DisplayName
	for field, value := range updates {
		preview.Before[field], preview.After[field] = before[field], value
	}
	return preview, nil
}

func executeAIPersonAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (aiActionResult, error) {
	if input.Action == "person.create" {
		var changes aiPersonCreateChanges
		_ = json.Unmarshal(input.Changes, &changes)
		request := createActorRequest{Type: "person", DisplayName: changes.DisplayName, Notes: changes.Notes}
		actor, err := actorFromCreateRequest(request)
		if err != nil {
			return aiActionResult{}, err
		}
		actor.CreatedAt, actor.UpdatedAt = now, now
		response, err := createActorInTransaction(tx, actor, requestID)
		return aiActionResult{ID: response.ID, Version: response.Version}, err
	}
	var update updateActorRequest
	_ = json.Unmarshal(input.Changes, &update)
	response, err := updateActorInTransaction(tx, input.ActorID, input.ExpectedVersion, update, requestID, now)
	return aiActionResult{ID: response.ID, Version: response.Version}, err
}

func loadAIPersonContext(db *gorm.DB, id string) (aiBusinessContextSource, error) {
	var actor models.Actor
	if err := db.Select("id", "type", "display_name", "status", "version").Where("id = ? AND type = 'person'", id).Take(&actor).Error; err != nil {
		return aiBusinessContextSource{}, err
	}
	return aiBusinessContextSource{
		Type: "person", ID: actor.ID, Version: actor.Version, Label: actor.DisplayName,
		Fields:          map[string]any{"display_name": actor.DisplayName, "status": actor.Status, "person_type": actor.Type},
		TruncatedFields: []string{},
	}, nil
}

func isAIPersonAction(action string) bool { return strings.HasPrefix(action, "person.") }
