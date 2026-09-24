package api

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiClientFollowupFields = map[string]bool{
	"assigned_actor_id": true, "scheduled_at": true, "timezone": true,
	"channel": true, "purpose": true, "notes": true, "priority": true,
}

func addAIClientFollowupSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "client_followup.create", "client_followup.update", "client_followup.cancel", "client_followup.complete", "client_followup.skip", "client_followup.reschedule")
	properties["client_followup_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Existing followup target from workspace_client_records; exact expected_version required. Never used by other actions."}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Client followup actions require clients + work + actions. Create requires client_id, assigned_actor_id (active owner/person), scheduled_at, timezone, channel, purpose; notes optional/null, priority low/normal/high (default normal). Update only supplied plan fields, never client_id. Cancel/skip require reason (1-1000). Complete requires user-reported result (1-4000) and explicit completed_at, optional next_step and next_followup (a full plan WITHOUT client_id). Actual completion requires separate human consent; never invent communication/results. Reschedule requires reason and a FULL replacement plan WITHOUT client_id: atomically cancels old plan and creates linked next plan. All existing targets must be planned; inactive clients allow completion WITHOUT next plan, skip, cancel only. No contact is sent. Confirm timezone/time with user; past plans are allowed but overdue. No consent/Actor identity overrides."
	fields := changes["properties"].(map[string]any)
	fields["assigned_actor_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Client followup: active owner/person from workspace_task_options. Not agent."}
	fields["scheduled_at"] = map[string]any{"type": "string", "description": "Client followup RFC3339 instant with explicit offset; interpreted with explicit IANA timezone. Ask if ambiguous."}
	fields["timezone"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "description": "Explicit IANA timezone, never Local"}
	fields["channel"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 80}
	fields["purpose"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 500}
	fields["notes"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 4000}
	fields["client_id"] = map[string]any{"type": []string{"string", "null"}, "description": "Project customer (nullable), or required non-null Client UUID for followup create; requires clients scope."}
	fields["priority"].(map[string]any)["enum"] = []string{"P0", "P1", "P2", "P3", "low", "normal", "high"}
	fields["result"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 4000, "description": "Only the actual result reported by the user, not a planned outcome"}
	fields["completed_at"] = map[string]any{"type": "string", "description": "Actual completion instant (RFC3339 with offset), explicitly supplied; never invented"}
	fields["next_step"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 4000}
	nextFields := map[string]any{}
	for key := range aiClientFollowupFields {
		nextFields[key] = fields[key]
	}
	nextFields["priority"] = map[string]any{"type": "string", "enum": []string{"low", "normal", "high"}}
	fields["next_followup"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"assigned_actor_id", "scheduled_at", "timezone", "channel", "purpose"}, "properties": nextFields, "description": "Optional full next plan for the SAME customer, committed atomically with completion; omit when not requested"}
}

func parseAIClientFollowupAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{"task_id", "project_id", "inbox_item_id", "reminder_id", "focus_session_id"} {
		if _, present := raw[key]; present {
			return input, errors.New("followup actions only accept client_followup_id as target")
		}
	}
	if input.Action == "client_followup.create" {
		for _, key := range []string{"client_followup_id", "expected_version"} {
			if _, present := raw[key]; present {
				return input, errors.New("followup create does not accept target/version")
			}
		}
	} else if id, err := uuid.Parse(input.ClientFollowupID); err != nil || id.String() != input.ClientFollowupID || input.ExpectedVersion < 1 {
		return input, errors.New("use the canonical client_followup_id and version from workspace_client_records")
	}
	for key, value := range fields {
		allowed := aiClientFollowupFields[key] || (key == "client_id" && input.Action == "client_followup.create")
		if input.Action == "client_followup.cancel" || input.Action == "client_followup.skip" {
			allowed = key == "reason"
		}
		if input.Action == "client_followup.complete" {
			allowed = key == "result" || key == "completed_at" || key == "next_step" || key == "next_followup"
		}
		if input.Action == "client_followup.reschedule" {
			allowed = aiClientFollowupFields[key] || key == "reason"
		}
		if !allowed || (string(value) == "null" && key != "notes" && key != "next_step") {
			return input, errors.New("unsupported or null followup field: " + key)
		}
		if key == "client_id" || key == "assigned_actor_id" {
			var id string
			if json.Unmarshal(value, &id) != nil {
				return input, errors.New("invalid identity")
			}
			if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
				return input, errors.New("use canonical actual UUIDs, never guessed identities")
			}
		}
		if key == "timezone" {
			var zone string
			if json.Unmarshal(value, &zone) != nil || strings.TrimSpace(zone) == "" || strings.TrimSpace(zone) == "Local" {
				return input, errors.New("an explicit IANA timezone is required")
			}
		}
	}
	switch input.Action {
	case "client_followup.create":
		for _, key := range []string{"client_id", "assigned_actor_id", "scheduled_at", "timezone", "channel", "purpose"} {
			if _, present := fields[key]; !present {
				return input, errors.New("missing followup field: " + key)
			}
		}
		var create createClientFollowupRequest
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		plan, err := clientFollowupFromPlanRequest(clientFollowupPlanRequest{AssignedActorID: create.AssignedActorID, ScheduledAt: create.ScheduledAt, Timezone: create.Timezone, Channel: create.Channel, Purpose: create.Purpose, Notes: create.Notes, Priority: create.Priority})
		if err != nil {
			return input, err
		}
		plan.ClientID = create.ClientID
		canonical := aiClientFollowupPlan(plan)
		delete(canonical, "status")
		input.Changes, _ = json.Marshal(canonical)
	case "client_followup.update":
		var update updateClientFollowupRequest
		if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
			return input, err
		}
		patch, err := clientFollowupUpdates(update)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(patch)
	case "client_followup.cancel", "client_followup.skip":
		var cancel cancelClientFollowupRequest
		if err := decodeStrictToolArguments(input.Changes, &cancel); err != nil {
			return input, err
		}
		reason, err := cleanClientFollowupText(cancel.Reason, "reason", 1000, true, false)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(map[string]any{"reason": reason})
	case "client_followup.complete":
		var completion completeClientFollowupRequest
		if err := decodeStrictToolArguments(input.Changes, &completion); err != nil {
			return input, err
		}
		result, err := cleanClientFollowupText(completion.Result, "result", 4000, true, false)
		if err != nil {
			return input, err
		}
		if completion.CompletedAt == nil {
			return input, errors.New("actual completed_at is required; ask the user")
		}
		completedAt, err := cleanClientFollowupTimestamp(*completion.CompletedAt)
		if err != nil {
			return input, err
		}
		nextStep, err := cleanClientFollowupOptionalText(completion.NextStep, "next_step", 4000, true)
		if err != nil {
			return input, err
		}
		canonical := map[string]any{"result": result, "completed_at": completedAt, "next_step": nextStep}
		if next, exists := fields["next_followup"]; exists {
			plan, err := normalizeAIClientFollowupPlan(next)
			if err != nil {
				return input, err
			}
			canonical["next_followup"] = plan
		}
		input.Changes, _ = json.Marshal(canonical)
	case "client_followup.reschedule":
		var reason string
		if err := json.Unmarshal(fields["reason"], &reason); err != nil {
			return input, errors.New("reschedule reason required")
		}
		reason, err := cleanClientFollowupText(reason, "reason", 1000, true, false)
		if err != nil {
			return input, err
		}
		delete(fields, "reason")
		rawPlan, _ := json.Marshal(fields)
		canonical, err := normalizeAIClientFollowupPlan(rawPlan)
		if err != nil {
			return input, err
		}
		canonical["reason"] = reason
		input.Changes, _ = json.Marshal(canonical)
	default:
		return input, errors.New("unsupported client followup action")
	}
	return input, nil
}

func normalizeAIClientFollowupPlan(raw json.RawMessage) (map[string]any, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for key, value := range fields {
		if !aiClientFollowupFields[key] || (string(value) == "null" && key != "notes") {
			return nil, errors.New("unsupported next plan field: " + key)
		}
	}
	for _, key := range []string{"assigned_actor_id", "scheduled_at", "timezone", "channel", "purpose"} {
		if _, exists := fields[key]; !exists {
			return nil, errors.New("next plan missing " + key)
		}
	}
	var input clientFollowupPlanRequest
	if err := decodeStrictToolArguments(raw, &input); err != nil {
		return nil, err
	}
	if id, err := uuid.Parse(input.AssignedActorID); err != nil || id.String() != input.AssignedActorID {
		return nil, errors.New("canonical next assignee ID required")
	}
	if strings.TrimSpace(input.Timezone) == "" || strings.TrimSpace(input.Timezone) == "Local" {
		return nil, errors.New("explicit next plan IANA timezone required")
	}
	plan, err := clientFollowupFromPlanRequest(input)
	if err != nil {
		return nil, err
	}
	canonical := aiClientFollowupPlan(plan)
	delete(canonical, "client_id")
	delete(canonical, "status")
	return canonical, nil
}

func aiClientFollowupPlan(item models.ClientFollowup) map[string]any {
	return map[string]any{"client_id": item.ClientID, "assigned_actor_id": item.AssignedActorID,
		"scheduled_at": item.ScheduledAt, "timezone": item.Timezone, "channel": item.Channel,
		"purpose": item.Purpose, "notes": item.Notes, "priority": item.Priority, "status": item.Status}
}

// Labels/status are part of consent. Changing a Client/Actor does not always bump
// the Followup version, so confirmation compares this stable preview as well.
func addAIClientFollowupIdentities(tx *gorm.DB, fields map[string]any) error {
	var client models.Client
	var actor models.Actor
	if err := tx.Select("id", "name", "status").First(&client, "id=?", fields["client_id"]).Error; err != nil {
		return err
	}
	if err := tx.Select("id", "display_name", "type", "status").First(&actor, "id=?", fields["assigned_actor_id"]).Error; err != nil {
		return err
	}
	fields["client_name"], fields["client_status"] = client.Name, client.Status
	fields["assigned_actor_name"], fields["assigned_actor_type"], fields["assigned_actor_status"] = actor.DisplayName, actor.Type, actor.Status
	return nil
}

func previewAIClientFollowupAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "client_followup.create" {
		_ = json.Unmarshal(input.Changes, &preview.After)
		preview.After["status"] = "planned"
		if err := ensureClientFollowupReferences(tx, preview.After["client_id"].(string), preview.After["assigned_actor_id"].(string)); err != nil {
			return preview, err
		}
	} else {
		patch := map[string]any{}
		_ = json.Unmarshal(input.Changes, &patch)
		row, err := prepareClientFollowupChange(tx, input.ClientFollowupID, input.ExpectedVersion, patch, input.Action == "client_followup.update")
		if err != nil {
			return preview, err
		}
		preview.Before, preview.After = aiClientFollowupPlan(row.ClientFollowup), aiClientFollowupPlan(row.ClientFollowup)
		if input.Action == "client_followup.reschedule" {
			preview.After["status"], preview.After["reason"] = "cancelled", patch["reason"]
			preview.NextFollowup = patch
			delete(preview.NextFollowup, "reason")
		} else {
			for key, value := range patch {
				if key != "next_followup" {
					preview.After[key] = value
				}
			}
			if next, ok := patch["next_followup"].(map[string]any); ok {
				preview.NextFollowup = next
			}
			switch input.Action {
			case "client_followup.cancel":
				preview.After["status"] = "cancelled"
			case "client_followup.skip":
				preview.After["status"] = "skipped"
			case "client_followup.complete":
				preview.After["status"] = "completed"
			}
		}
		_, changesNotes := patch["notes"]
		if !changesNotes || input.Action == "client_followup.reschedule" {
			delete(preview.Before, "notes")
			delete(preview.After, "notes")
		}
		if preview.NextFollowup != nil {
			preview.NextFollowup["client_id"], preview.NextFollowup["status"] = row.ClientID, "planned"
			if err := ensureClientFollowupReferences(tx, row.ClientID, preview.NextFollowup["assigned_actor_id"].(string)); err != nil {
				return preview, err
			}
			if err := addAIClientFollowupIdentities(tx, preview.NextFollowup); err != nil {
				return preview, err
			}
		}
		if err := addAIClientFollowupIdentities(tx, preview.Before); err != nil {
			return preview, err
		}
	}
	if err := addAIClientFollowupIdentities(tx, preview.After); err != nil {
		return preview, err
	}
	preview.Label = preview.After["purpose"].(string)
	return preview, nil
}

func executeAIClientFollowupAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (aiActionResult, error) {
	var out clientFollowupResponse
	var err error
	if input.Action == "client_followup.complete" {
		var complete completeClientFollowupRequest
		_ = json.Unmarshal(input.Changes, &complete)
		var next *models.ClientFollowup
		if complete.NextFollowup != nil {
			plan, planErr := clientFollowupFromPlanRequest(*complete.NextFollowup)
			if planErr != nil {
				return aiActionResult{}, planErr
			}
			next = &plan
		}
		out, err = completeClientFollowupInTransaction(tx, input.ClientFollowupID, input.ExpectedVersion, complete.Result, complete.NextStep, *complete.CompletedAt, next, requestID, now)
		// A compound command's result is the newly created plan; target_id still
		// identifies the completed original in durable conversation receipts.
		if err == nil && out.NextFollowup != nil {
			out = *out.NextFollowup
		}
	} else if input.Action == "client_followup.reschedule" {
		var request rescheduleClientFollowupRequest
		_ = json.Unmarshal(input.Changes, &request)
		var plan models.ClientFollowup
		plan, err = clientFollowupFromPlanRequest(clientFollowupPlanRequest{AssignedActorID: request.AssignedActorID, ScheduledAt: request.ScheduledAt, Timezone: request.Timezone, Channel: request.Channel, Purpose: request.Purpose, Notes: request.Notes, Priority: request.Priority})
		if err == nil {
			_, out, err = rescheduleClientFollowupInTransaction(tx, input.ClientFollowupID, input.ExpectedVersion, plan, request.Reason, requestID, now)
		}
	} else if input.Action == "client_followup.create" {
		var create createClientFollowupRequest
		_ = json.Unmarshal(input.Changes, &create)
		var plan models.ClientFollowup
		plan, err = clientFollowupFromPlanRequest(clientFollowupPlanRequest{AssignedActorID: create.AssignedActorID, ScheduledAt: create.ScheduledAt, Timezone: create.Timezone, Channel: create.Channel, Purpose: create.Purpose, Notes: create.Notes, Priority: create.Priority})
		if err == nil {
			plan.ClientID, plan.CreatedAt, plan.UpdatedAt = create.ClientID, now, now
			out, err = createClientFollowupInTransaction(tx, plan, requestID)
		}
	} else {
		patch := map[string]any{}
		action := "client_followup_updated"
		if input.Action == "client_followup.cancel" {
			var cancel cancelClientFollowupRequest
			_ = json.Unmarshal(input.Changes, &cancel)
			patch = map[string]any{"status": "cancelled", "cancelled_at": now, "cancel_reason": cancel.Reason}
			action = "client_followup_cancelled"
		} else if input.Action == "client_followup.skip" {
			var skip skipClientFollowupRequest
			_ = json.Unmarshal(input.Changes, &skip)
			patch = map[string]any{"status": "skipped", "skipped_at": now, "skip_reason": skip.Reason}
			action = "client_followup_skipped"
		} else if input.Action == "client_followup.update" {
			var update updateClientFollowupRequest
			_ = json.Unmarshal(input.Changes, &update)
			patch, err = clientFollowupUpdates(update)
		} else {
			return aiActionResult{}, errors.New("unsupported followup action")
		}
		if err == nil {
			out, err = changeClientFollowupInTransaction(tx, input.ClientFollowupID, input.ExpectedVersion, patch, action, requestID, now)
		}
	}
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
