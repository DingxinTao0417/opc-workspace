package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func addAITagSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "tag.create", "tag.update", "tag.delete")
	properties["tag_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Tag update/delete target from workspace_task_options type tag; expected_version required",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Tag create requires name (1-50 chars) and #RRGGBB color; update accepts at least one of name/color; delete uses empty changes and separate human consent. Read real tag ID, color and version with workspace_task_options type tag."
}

func parseAITagAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	switch input.Action {
	case "tag.create":
		if !exactJSONKeys(raw, "action", "changes") {
			return input, errors.New("Tag create accepts only action and changes")
		}
		if input.TagID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("Tag create does not accept a tag ID/version")
		}
	case "tag.update", "tag.delete":
		if !exactJSONKeys(raw, "action", "tag_id", "expected_version", "changes") {
			return input, errors.New("Tag update/delete require only tag_id, expected_version and changes")
		}
		parsed, err := uuid.Parse(input.TagID)
		if err != nil || parsed.String() != input.TagID || input.ExpectedVersion < 1 {
			return input, errors.New("use a real tag ID and expected_version from workspace_task_options")
		}
	default:
		return input, errors.New("unsupported Tag action")
	}
	if input.Action == "tag.delete" {
		if len(fields) != 0 {
			return input, errors.New("Tag deletion requires empty changes; permanent-deletion consent belongs to the human decision")
		}
		input.Changes, _ = json.Marshal(fields)
		return input, nil
	}
	for key := range fields {
		if key != "name" && key != "color" {
			return input, errors.New("unsupported Tag field: " + key)
		}
	}
	var name, color *string
	if rawName, present := fields["name"]; present {
		var value string
		if json.Unmarshal(rawName, &value) != nil {
			return input, errors.New("Tag name must be a string")
		}
		canonical, err := validateTagName(value)
		if err != nil {
			return input, err
		}
		name = &canonical
		fields["name"], _ = json.Marshal(canonical)
	}
	if rawColor, present := fields["color"]; present {
		var value string
		if json.Unmarshal(rawColor, &value) != nil {
			return input, errors.New("Tag color must be a string")
		}
		canonical, err := validateTagColor(value)
		if err != nil {
			return input, err
		}
		color = &canonical
		fields["color"], _ = json.Marshal(canonical)
	}
	if input.Action == "tag.create" && (name == nil || color == nil) {
		return input, errors.New("Tag create requires name and color")
	}
	if input.Action == "tag.update" && name == nil && color == nil {
		return input, errors.New("Tag update requires at least one editable field")
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func exactJSONKeys(values map[string]json.RawMessage, keys ...string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func aiTagFields(tag models.Tag) map[string]any {
	return map[string]any{"name": tag.Name, "color": tag.Color}
}

func previewAITagAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "tag.create" {
		var create createTagRequest
		_ = json.Unmarshal(input.Changes, &create)
		tag, err := tagFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if err := requireUniqueTagName(tx, tag.Name, ""); err != nil {
			return preview, err
		}
		preview.Label, preview.After = tag.Name, aiTagFields(tag)
		return preview, nil
	}
	var tag models.Tag
	if err := tx.First(&tag, "id = ?", input.TagID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(http.StatusNotFound, "TAG_NOT_FOUND", "Tag not found")
		}
		return preview, err
	}
	if tag.Version != input.ExpectedVersion {
		return preview, taskVersionConflict()
	}
	preview.Label = tag.Name
	if input.Action == "tag.delete" {
		var count int64
		if err := tx.Table("task_tags").Where("tag_id = ?", tag.ID).Count(&count).Error; err != nil {
			return preview, err
		}
		preview.Before = map[string]any{"name": tag.Name, "color": tag.Color, "tag_version": tag.Version}
		preview.After = map[string]any{"tag_deleted": true, "detached_tasks": count}
		return preview, nil
	}
	var update updateTagRequest
	_ = json.Unmarshal(input.Changes, &update)
	if update.Name != nil {
		name, err := validateTagName(*update.Name)
		if err != nil {
			return preview, err
		}
		if err := requireUniqueTagName(tx, name, tag.ID); err != nil {
			return preview, err
		}
		preview.Before["name"], preview.After["name"] = tag.Name, name
	}
	if update.Color != nil {
		color, err := validateTagColor(*update.Color)
		if err != nil {
			return preview, err
		}
		preview.Before["color"], preview.After["color"] = tag.Color, color
	}
	return preview, nil
}

func executeAITagAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, error) {
	switch input.Action {
	case "tag.create":
		var create createTagRequest
		_ = json.Unmarshal(input.Changes, &create)
		tag, err := tagFromCreateRequest(create)
		if err != nil {
			return aiActionResult{}, err
		}
		tag.CreatedAt = now
		if err := createTagInTransaction(tx, &tag); err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: tag.ID, Version: tag.Version}, nil
	case "tag.update":
		var update updateTagRequest
		_ = json.Unmarshal(input.Changes, &update)
		tag, err := updateTagInTransaction(tx, input.TagID, input.ExpectedVersion, update, now)
		return aiActionResult{ID: tag.ID, Version: tag.Version}, err
	case "tag.delete":
		_, err := deleteTagInTransaction(tx, input.TagID, input.ExpectedVersion, now)
		return aiActionResult{ID: input.TagID, Version: input.ExpectedVersion}, err
	default:
		return aiActionResult{}, errors.New("unsupported Tag action")
	}
}
