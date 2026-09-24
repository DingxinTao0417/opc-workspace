package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiReminderChangeFields = map[string]bool{
	"title": true, "summary": true, "priority": true, "trigger_at": true,
	"recurrence_type": true, "recurrence_interval": true, "recurrence_timezone": true,
}

func parseAIReminderAction(input aiWorkspaceAction, fields map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" || input.InboxItemID != "" {
		return input, errors.New("reminder actions only accept reminder_id as the target")
	}
	if input.Action == "reminder.create" {
		if input.ReminderID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a Reminder ID/version")
		}
	} else if id, err := uuid.Parse(input.ReminderID); err != nil || id.String() != input.ReminderID || input.ExpectedVersion < 1 {
		return input, errors.New("use the canonical reminder_id and expected_version from workspace_get")
	}
	for field, value := range fields {
		allowed := aiReminderChangeFields[field]
		if input.Action == "reminder.cancel" {
			allowed = field == "reason"
		}
		if !allowed || string(value) == "null" {
			return input, errors.New("unsupported or null Reminder field: " + field)
		}
	}
	// Canonical parsing is clock independent so old decisions remain replayable.
	// Preview AND confirmation repeat future-time validation against server_now.
	switch input.Action {
	case "reminder.create":
		var create createReminderRequest
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		normalized, err := normalizeReminderCreate(create, time.Time{})
		if err != nil {
			return input, err
		}
		canonical := aiReminderFields(models.Reminder{Title: normalized.Title, Summary: normalized.Summary,
			Priority: normalized.Priority, TriggerAt: normalized.TriggerAt, RecurrenceType: normalized.RecurrenceType,
			RecurrenceInterval: normalized.RecurrenceInterval, RecurrenceTimezone: normalized.RecurrenceTimezone})
		delete(canonical, "status")
		delete(canonical, "occurrence_number")
		delete(canonical, "recurrence_anchor_day")
		input.Changes, _ = json.Marshal(canonical)
	case "reminder.update":
		var update updateReminderRequest
		if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
			return input, err
		}
		patch, err := normalizeReminderPatch(update, time.Time{})
		if err != nil {
			return input, err
		}
		if len(patch) == 0 {
			return input, errors.New("at least one editable Reminder field is required")
		}
		input.Changes, _ = json.Marshal(patch)
	case "reminder.cancel":
		var cancel cancelReminderRequest
		if err := decodeStrictToolArguments(input.Changes, &cancel); err != nil {
			return input, err
		}
		reason, err := normalizeReminderCancelReason(cancel.Reason)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(map[string]any{"reason": reason})
	default:
		return input, errors.New("unsupported Reminder action")
	}
	return input, nil
}

func aiReminderFields(item models.Reminder) map[string]any {
	return map[string]any{"title": item.Title, "summary": item.Summary, "priority": item.Priority,
		"trigger_at": item.TriggerAt, "status": item.Status, "recurrence_type": item.RecurrenceType,
		"recurrence_interval": item.RecurrenceInterval, "recurrence_timezone": item.RecurrenceTimezone,
		"recurrence_anchor_day": item.RecurrenceAnchorDay, "occurrence_number": item.OccurrenceNumber}
}

func previewAIReminderAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "reminder.create" {
		var create createReminderRequest
		_ = json.Unmarshal(input.Changes, &create)
		n, err := normalizeReminderCreate(create, now)
		if err != nil {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
		}
		preview.Label = n.Title
		preview.After = aiReminderFields(models.Reminder{Title: n.Title, Summary: n.Summary, Priority: n.Priority,
			TriggerAt: n.TriggerAt, Status: "scheduled", RecurrenceType: n.RecurrenceType,
			RecurrenceInterval: n.RecurrenceInterval, RecurrenceTimezone: n.RecurrenceTimezone,
			RecurrenceAnchorDay: n.RecurrenceAnchorDay, OccurrenceNumber: 1})
		return preview, nil
	}
	patch := map[string]any{}
	if input.Action == "reminder.update" {
		var update updateReminderRequest
		_ = json.Unmarshal(input.Changes, &update)
		var err error
		patch, err = normalizeReminderPatch(update, now)
		if err != nil {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
		}
	}
	current, next, err := prepareReminderUpdate(tx, input.ReminderID, input.ExpectedVersion, patch)
	if err != nil {
		return preview, err
	}
	preview.Label = current.Title
	preview.Before, preview.After = aiReminderFields(current), aiReminderFields(next)
	// A scheduling/cancellation approval must not duplicate unrelated long notes
	// or become impossible solely because an existing summary exceeds its budget.
	if _, editsSummary := patch["summary"]; !editsSummary {
		delete(preview.Before, "summary")
		delete(preview.After, "summary")
	}
	if input.Action == "reminder.cancel" {
		var cancel cancelReminderRequest
		_ = json.Unmarshal(input.Changes, &cancel)
		preview.After["status"], preview.After["reason"] = "cancelled", cancel.Reason
	}
	return preview, nil
}

func executeAIReminderAction(tx *gorm.DB, input aiWorkspaceAction, requestID, nowText string) (aiActionResult, error) {
	now, err := time.Parse(time.RFC3339Nano, nowText)
	if err != nil {
		return aiActionResult{}, err
	}
	var result reminderOutput
	switch input.Action {
	case "reminder.create":
		var create createReminderRequest
		_ = json.Unmarshal(input.Changes, &create)
		var normalized normalizedReminderCreate
		normalized, err = normalizeReminderCreate(create, now)
		if err == nil {
			result, err = createReminderInTransaction(tx, normalized, requestID, nowText)
		}
	case "reminder.update":
		var update updateReminderRequest
		_ = json.Unmarshal(input.Changes, &update)
		var patch map[string]any
		patch, err = normalizeReminderPatch(update, now)
		if err == nil {
			result, err = updateReminderInTransaction(tx, input.ReminderID, input.ExpectedVersion, patch, requestID, nowText)
		}
	case "reminder.cancel":
		var cancel cancelReminderRequest
		_ = json.Unmarshal(input.Changes, &cancel)
		result, err = cancelReminderInTransaction(tx, input.ReminderID, input.ExpectedVersion, cancel.Reason, requestID, nowText)
	default:
		err = errors.New("unsupported Reminder action")
	}
	return aiActionResult{ID: result.ID, Version: result.Version}, err
}

func loadAIReminderContext(ctx context.Context, db *gorm.DB, id string, now time.Time) (aiBusinessContextSource, error) {
	var item models.Reminder
	if err := db.WithContext(ctx).Select("id", "title", "summary", "priority", "trigger_at", "status", "version",
		"series_id", "recurrence_type", "recurrence_interval", "recurrence_timezone", "recurrence_anchor_day",
		"occurrence_number", "fired_at", "inbox_item_id").First(&item, "id=?", id).Error; err != nil {
		return aiBusinessContextSource{}, err
	}
	fields := aiReminderFields(item)
	fields["series_id"], fields["fired_at"], fields["inbox_item_id"] = item.SeriesID, item.FiredAt, item.InboxItemID
	fields["server_now"] = formatInboxTimestamp(now)
	summary, truncated := boundedAIBusinessContextText(item.Summary)
	fields["summary"] = summary
	source := aiBusinessContextSource{Type: "reminder", ID: item.ID, Label: item.Title, Version: item.Version, Fields: fields, TruncatedFields: []string{}}
	if truncated {
		source.TruncatedFields = append(source.TruncatedFields, "summary")
	}
	return source, nil
}
