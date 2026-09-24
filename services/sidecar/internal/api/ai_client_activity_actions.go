package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func addAIClientActivitySchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "client_activity.create", "client_activity.update", "client_activity.delete")
	properties["client_activity_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Existing activity ID from workspace_client_records; exact expected_version required"}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Client activity create/update/delete require clients + work + actions. Only manual note/meeting based on user-provided content, never invent communication or system references. Create requires client_id, kind, title (1-200), body (1-10000), occurred_at (actual RFC3339 with explicit offset, at most server_now + 5min). Update only kind/title/body/occurred_at, supplied fields only; read the whole body before proposing replacement. Delete requires reason (1-1000), is soft deletion without undo and needs separate HUMAN consent; history/attachments remain. No client reassignment, source/actor overrides, nulls or consent flags. Activity input <=64 KiB; full before/after preview <=128 KiB, never truncate to make it fit."
	fields := changes["properties"].(map[string]any)
	fields["kind"].(map[string]any)["enum"] = []string{"work", "review", "followup", "reminder", "note", "meeting"}
	fields["body"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 10000, "description": "Full manual note/meeting text supplied or verified by user; no inferred events or truncated replacement"}
	fields["occurred_at"] = map[string]any{"type": "string", "description": "Actual RFC3339 instant with offset; ask user if unknown, not a future plan"}
	fields["client_id"].(map[string]any)["description"] = "Nullable Project customer or required non-null Client UUID for followup/activity create; requires clients scope."
}

func parseAIClientActivityAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{"task_id", "project_id", "inbox_item_id", "reminder_id", "focus_session_id", "client_followup_id"} {
		if _, present := raw[key]; present {
			return input, errors.New("activity actions only accept client_activity_id as target")
		}
	}
	creating := input.Action == "client_activity.create"
	if creating {
		for _, key := range []string{"client_activity_id", "expected_version"} {
			if _, present := raw[key]; present {
				return input, errors.New("activity create does not accept target/version")
			}
		}
	} else if id, err := uuid.Parse(input.ClientActivityID); err != nil || id.String() != input.ClientActivityID || input.ExpectedVersion < 1 {
		return input, errors.New("read actual activity ID/version first")
	}
	if !creating && input.Action != "client_activity.update" && input.Action != "client_activity.delete" {
		return input, errors.New("unsupported activity action")
	}
	if len(fields) == 0 {
		return input, errors.New("activity changes required")
	}
	canonical := map[string]any{}
	for key, rawValue := range fields {
		var value string
		if string(rawValue) == "null" || json.Unmarshal(rawValue, &value) != nil {
			return input, errors.New("activity fields must be non-null strings")
		}
		var cleaned string
		var err error
		if input.Action == "client_activity.delete" {
			if key != "reason" {
				return input, errors.New("activity delete only accepts reason")
			}
			cleaned, err = cleanClientActivityText(value, "reason", 1000, true)
		} else {
			switch key {
			case "client_id":
				if !creating {
					return input, errors.New("activity client cannot change")
				}
				id, parseErr := uuid.Parse(value)
				if parseErr != nil || id.String() != value {
					return input, errors.New("canonical actual client ID required")
				}
				cleaned = value
			case "kind":
				cleaned, err = cleanManualClientActivityKind(value)
			case "title":
				cleaned, err = cleanClientActivityText(value, key, 200, false)
			case "body":
				cleaned, err = cleanClientActivityText(value, key, 10000, true)
			case "occurred_at":
				var occurred time.Time
				occurred, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
				cleaned = occurred.UTC().Format(time.RFC3339Nano)
			default:
				return input, errors.New("unsupported activity field: " + key)
			}
		}
		if err != nil {
			return input, err
		}
		canonical[key] = cleaned
	}
	if creating {
		for _, key := range []string{"client_id", "kind", "title", "body", "occurred_at"} {
			if _, present := canonical[key]; !present {
				return input, errors.New("activity create missing " + key)
			}
		}
	}
	input.Changes, _ = json.Marshal(canonical)
	return input, nil
}

func previewAIClientActivityAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	patch := map[string]any{}
	_ = json.Unmarshal(input.Changes, &patch)
	if input.Action == "client_activity.create" {
		preview.After = patch
		preview.After["activity_state"] = "visible"
	} else {
		row, err := prepareClientActivityChange(tx, input.ClientActivityID, input.ExpectedVersion)
		if err != nil {
			return preview, err
		}
		for _, fields := range []map[string]any{preview.Before, preview.After} {
			fields["client_id"], fields["kind"], fields["title"], fields["occurred_at"], fields["activity_state"] = row.ClientID, row.Kind, row.Title, normalizeTimestamp(row.OccurredAt), "visible"
			if _, changesBody := patch["body"]; changesBody {
				fields["body"] = row.Body
			}
		}
		for key, value := range patch {
			preview.After[key] = value
		}
		if input.Action == "client_activity.delete" {
			preview.After["activity_state"] = "deleted"
		}
	}
	// Use the native future-skew rule only for an explicitly written timestamp;
	// deleting/editing another field must still work for imported historical data.
	if stamp, changesTime := patch["occurred_at"]; changesTime {
		if _, err := cleanClientActivityOccurredAt(stamp.(string), now); err != nil {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
		}
	}
	var client models.Client
	if err := tx.Select("id", "name", "status").First(&client, "id=?", preview.After["client_id"]).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(404, "CLIENT_NOT_FOUND", "Client not found")
		}
		return preview, err
	}
	preview.After["client_name"], preview.After["client_status"] = client.Name, client.Status
	if len(preview.Before) > 0 {
		preview.Before["client_name"], preview.Before["client_status"] = client.Name, client.Status
	}
	preview.Label = preview.After["title"].(string)
	return preview, nil
}

func executeAIClientActivityAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, error) {
	clock, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return aiActionResult{}, err
	}
	var out clientActivityResponse
	if input.Action == "client_activity.create" {
		var request struct {
			ClientID string `json:"client_id"`
			createClientActivityRequest
		}
		_ = json.Unmarshal(input.Changes, &request)
		activity, err := clientActivityFromCreateRequestAt(request.ClientID, request.createClientActivityRequest, clock)
		if err != nil {
			return aiActionResult{}, err
		}
		out, err = createClientActivityInTransaction(tx, activity)
		return aiActionResult{ID: out.ID, Version: out.Version}, err
	}
	var patch map[string]any
	if input.Action == "client_activity.delete" {
		var request deleteClientActivityRequest
		_ = json.Unmarshal(input.Changes, &request)
		patch = map[string]any{"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": request.Reason}
	} else {
		var request updateClientActivityRequest
		_ = json.Unmarshal(input.Changes, &request)
		patch, err = clientActivityUpdatesAt(request, clock)
		if err != nil {
			return aiActionResult{}, err
		}
	}
	out, err = changeClientActivityInTransaction(tx, input.ClientActivityID, input.ExpectedVersion, patch, now)
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
