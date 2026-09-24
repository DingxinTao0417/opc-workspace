package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiContentItemUpdateFields = map[string]bool{
	"title": true, "platform": true, "project_id": true,
	"notes": true, "external_link": true,
}

func addAIContentItemSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any),
		"content_item.create", "content_item.update",
		"content_item.schedule", "content_item.unschedule",
		"content_item.review", "content_item.cancel", "content_item.reopen",
		"content_item.archive", "content_item.restore", "content_item.delete", "content_item.publish",
		"content_item.link_task", "content_item.set_task_required", "content_item.unlink_task",
	)
	properties["content_item_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Required for an existing content item; read its exact ID and expected_version with workspace_get first",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Content item create requires title and platform; optional status is draft, in_review, or scheduled. A scheduled create and content_item.schedule require scheduled_at plus scheduled_timezone. Update accepts only title, platform, project_id, notes, and external_link. Unschedule/review/cancel/reopen/archive/restore/delete require empty changes. Publish is only a proposal after the user says external publication actually occurred: optional published_at (RFC3339, otherwise the human confirmation time) and external_link text or null; it requires separate human verification, never publishes externally or fetches links. link_task/set_task_required require task_id, expected_task_version and is_required; unlink_task requires task_id and expected_task_version. These relation actions never modify Task state. Permanent deletion is only for an archived item and has separate human-only consent. Manual reordering and external link fetching are not available to the model."
	fields := changes["properties"].(map[string]any)
	fields["platform"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 64}
	fields["notes"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 4000}
	fields["external_link"] = map[string]any{"type": []string{"string", "null"}, "maxLength": 2048, "description": "Stored as untrusted local text; never fetched by the workspace tool"}
	fields["published_at"] = map[string]any{"type": "string", "description": "Content publish confirmation only: actual RFC3339 time explicitly supplied by the user; omit to use the human confirmation time"}
	fields["scheduled_at"] = map[string]any{"type": "string", "description": "Content item RFC3339 timestamp with explicit zone"}
	fields["scheduled_timezone"] = map[string]any{"type": "string", "description": "IANA timezone for the content schedule"}
	// status is shared by several action domains. The action parser enforces the
	// narrower enum for each domain after schema validation.
	fields["status"] = map[string]any{"type": "string", "enum": []string{
		"planned", "active", "achieved", "pending", "confirmed",
		"draft", "in_review", "scheduled", "cancelled",
	}}
}

func parseAIContentItemAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for _, key := range []string{
		"agent_run_id", "project_note_id", "invoice_id", "financial_entry_id",
		"project_id", "task_id", "inbox_item_id", "reminder_id", "focus_session_id",
		"client_followup_id", "client_activity_id", "automation_rule_id", "automation_run_id",
		"roadmap_milestone_id",
	} {
		if _, present := raw[key]; present {
			return input, errors.New("content item actions only accept content_item_id as target")
		}
	}
	switch input.Action {
	case "content_item.create":
		if _, present := raw["content_item_id"]; present || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a content item ID/version")
		}
	case "content_item.update", "content_item.schedule", "content_item.unschedule",
		"content_item.review", "content_item.cancel", "content_item.reopen",
		"content_item.archive", "content_item.restore", "content_item.delete", "content_item.publish",
		"content_item.link_task", "content_item.set_task_required", "content_item.unlink_task":
		parsed, err := uuid.Parse(strings.TrimSpace(input.ContentItemID))
		if err != nil || parsed.String() != input.ContentItemID || input.ExpectedVersion < 1 {
			return input, errors.New("use a real content item ID and expected_version from workspace_get")
		}
	default:
		return input, errors.New("unsupported content item action")
	}

	switch input.Action {
	case "content_item.create":
		allowed := map[string]bool{"title": true, "platform": true, "status": true, "scheduled_at": true, "scheduled_timezone": true, "project_id": true, "notes": true, "external_link": true}
		for field := range fields {
			if !allowed[field] {
				return input, errors.New("unsupported content item field: " + field)
			}
		}
		var create createContentItemRequest
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		item, err := contentItemFromCreateRequest(create)
		if err != nil {
			return input, err
		}
		if item.Status != "draft" && item.Status != "in_review" && item.Status != "scheduled" {
			return input, errors.New("content item create status must be draft, in_review, or scheduled")
		}
		if item.ScheduledAt != nil && item.Status != "scheduled" {
			return input, errors.New("content item create with a schedule must use scheduled status")
		}
	case "content_item.update":
		if len(fields) == 0 {
			return input, errors.New("at least one content item field is required")
		}
		for field := range fields {
			if !aiContentItemUpdateFields[field] {
				return input, errors.New("unsupported content item field: " + field)
			}
		}
		var update updateContentItemRequest
		if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
			return input, err
		}
	case "content_item.schedule":
		if len(fields) != 2 || fields["scheduled_at"] == nil || fields["scheduled_timezone"] == nil {
			return input, errors.New("content item schedule requires only scheduled_at and scheduled_timezone")
		}
		var schedule struct {
			ScheduledAt       string `json:"scheduled_at"`
			ScheduledTimezone string `json:"scheduled_timezone"`
		}
		if err := decodeStrictToolArguments(input.Changes, &schedule); err != nil {
			return input, err
		}
		if _, _, err := validateContentSchedule(&schedule.ScheduledAt, &schedule.ScheduledTimezone); err != nil {
			return input, err
		}
	case "content_item.publish":
		if len(raw) != 4 || len(fields) > 2 {
			return input, errors.New("content_item.publish requires only the Content Item target/version and optional publication fields")
		}
		for _, key := range []string{"action", "content_item_id", "expected_version", "changes"} {
			if _, ok := raw[key]; !ok {
				return input, errors.New("content_item.publish requires a real Content Item ID and version")
			}
		}
		if _, err := aiContentPublishInput(input.Changes, fields); err != nil {
			return input, err
		}
	case "content_item.link_task", "content_item.set_task_required", "content_item.unlink_task":
		if _, _, err := aiContentItemTaskCommand(input); err != nil {
			return input, err
		}
	default:
		if len(fields) != 0 {
			return input, errors.New("content item lifecycle actions require empty changes")
		}
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func aiContentPublishInput(raw json.RawMessage, fields map[string]json.RawMessage) (publishContentItemRequest, error) {
	var input publishContentItemRequest
	for field, value := range fields {
		if (field != "published_at" && field != "external_link") || (field == "published_at" && string(value) == "null") {
			return input, errors.New("unsupported or null content publication field: " + field)
		}
	}
	if err := decodeStrictToolArguments(raw, &input); err != nil {
		return input, err
	}
	if input.PublishedAt != nil {
		if _, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.PublishedAt)); err != nil {
			return input, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "published_at must be RFC 3339 when provided")
		}
	}
	if _, err := normalizeContentOptional(input.ExternalLink.Value, 2048, "external_link"); err != nil {
		return input, err
	}
	return input, nil
}

func isAIContentItemTaskAction(action string) bool {
	return action == "content_item.link_task" || action == "content_item.set_task_required" || action == "content_item.unlink_task"
}

func aiContentItemTaskCommand(input aiWorkspaceAction) (contentItemTaskCommand, string, error) {
	var change struct {
		TaskID              string `json:"task_id"`
		ExpectedTaskVersion int64  `json:"expected_task_version"`
		IsRequired          *bool  `json:"is_required"`
	}
	var command contentItemTaskCommand
	if err := decodeStrictToolArguments(input.Changes, &change); err != nil {
		return command, "", err
	}
	parsed, err := uuid.Parse(change.TaskID)
	if err != nil || parsed.String() != change.TaskID || change.ExpectedTaskVersion < 1 {
		return command, "", errors.New("use canonical task_id and expected_task_version from workspace_get")
	}
	operation := map[string]string{
		"content_item.link_task":         "link",
		"content_item.set_task_required": "requirement",
		"content_item.unlink_task":       "unlink",
	}[input.Action]
	if operation == "" {
		return command, "", errors.New("unsupported Content Item Task action")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input.Changes, &fields)
	for field, value := range fields {
		allowed := field == "task_id" || field == "expected_task_version" || (operation != "unlink" && field == "is_required")
		if !allowed || string(value) == "null" {
			return command, "", errors.New("unsupported or null Content Item Task field: " + field)
		}
	}
	if operation != "unlink" && change.IsRequired == nil {
		return command, "", errors.New("is_required is required")
	}
	expectedTaskVersion := change.ExpectedTaskVersion
	command = contentItemTaskCommand{
		TaskID:              change.TaskID,
		ExpectedTaskVersion: &expectedTaskVersion,
		IsRequired:          change.IsRequired,
	}
	return command, operation, nil
}

func contentItemTaskPreviewFields(task models.Task, relationState string, required bool, contentVersion int64) map[string]any {
	return map[string]any{
		"task_id": task.ID, "task_title": task.Title, "task_version": task.Version, "task_status": task.Status,
		"relation_state": relationState, "is_required": required, "content_item_version": contentVersion,
	}
}

func contentItemTaskBeforeFields(relationState string, required bool, contentVersion int64) map[string]any {
	return map[string]any{
		"relation_state": relationState, "is_required": required, "content_item_version": contentVersion,
	}
}

func aiContentItemFields(response contentItemResponse) map[string]any {
	return map[string]any{
		"title": response.Title, "platform": response.Platform, "status": response.Status,
		"scheduled_at": response.ScheduledAt, "scheduled_timezone": response.ScheduledTimezone,
		"project_id": response.ProjectID, "notes": response.Notes, "external_link": response.ExternalLink,
	}
}

func aiContentItemModelFields(item models.ContentItem) map[string]any {
	return map[string]any{
		"title": item.Title, "platform": item.Platform, "status": item.Status,
		"scheduled_at": item.ScheduledAt, "scheduled_timezone": item.ScheduledTimezone,
		"project_id": item.ProjectID, "notes": item.Notes, "external_link": item.ExternalLink,
	}
}

func contentItemLifecycleTarget(item models.ContentItem, action string) (string, *string, error) {
	switch action {
	case "content_item.review":
		if item.Status != "draft" && item.Status != "scheduled" {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Only draft or scheduled content can enter review")
		}
		return "in_review", nil, nil
	case "content_item.cancel":
		if item.Status != "draft" && item.Status != "in_review" && item.Status != "scheduled" {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Only active unpublished content can be cancelled")
		}
		return "cancelled", nil, nil
	case "content_item.reopen":
		if item.Status != "cancelled" {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Only cancelled content can be reopened")
		}
		return "draft", nil, nil
	case "content_item.archive":
		if item.Status == "published" {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Published content cannot be archived until published-history archival is implemented")
		}
		if item.Status == "archived" {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Content item is already archived")
		}
		from := item.Status
		return "archived", &from, nil
	case "content_item.restore":
		if item.Status != "archived" || item.ArchivedFromStatus == nil {
			return "", nil, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Content item is not archived")
		}
		return *item.ArchivedFromStatus, nil, nil
	default:
		return "", nil, errors.New("unsupported content item lifecycle action")
	}
}

func previewAIContentItemAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "content_item.create" {
		var create createContentItemRequest
		_ = json.Unmarshal(input.Changes, &create)
		item, err := contentItemFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if err := requireContentItemProject(tx, item.ProjectID); err != nil {
			return preview, err
		}
		preview.Label, preview.After = item.Title, aiContentItemModelFields(item)
		return preview, nil
	}
	response, err := loadContentItemResponse(tx, input.ContentItemID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_NOT_FOUND", "Content item not found")
	}
	if err != nil {
		return preview, err
	}
	if response.Version != input.ExpectedVersion {
		return preview, contentItemVersionConflict()
	}
	preview.Label = response.Title
	var current models.ContentItem
	if err := tx.First(&current, "id = ?", response.ID).Error; err != nil {
		return preview, err
	}
	before := aiContentItemFields(response)
	switch input.Action {
	case "content_item.publish":
		if response.Status != "draft" && response.Status != "in_review" && response.Status != "scheduled" {
			return preview, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Only active unpublished content can be confirmed as published")
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &fields)
		publish, err := aiContentPublishInput(input.Changes, fields)
		if err != nil {
			return preview, err
		}
		publishedAt := "confirmation_time"
		if publish.PublishedAt != nil {
			parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(*publish.PublishedAt))
			publishedAt = parsed.UTC().Format(time.RFC3339Nano)
		}
		link := response.ExternalLink
		if publish.ExternalLink.Set {
			link, err = normalizeContentOptional(publish.ExternalLink.Value, 2048, "external_link")
			if err != nil {
				return preview, err
			}
		}
		preview.Before = map[string]any{"status": response.Status, "published_at": response.PublishedAt, "external_link": response.ExternalLink}
		preview.After = map[string]any{"status": "published", "published_at": publishedAt, "external_link": link}
	case "content_item.link_task", "content_item.set_task_required", "content_item.unlink_task":
		command, operation, err := aiContentItemTaskCommand(input)
		if err != nil {
			return preview, err
		}
		task, err := loadContentItemTaskTarget(tx, command.TaskID, command.ExpectedTaskVersion)
		if err != nil {
			return preview, err
		}
		if operation == "link" {
			var relation models.ContentItemTask
			if err := tx.First(&relation, "content_item_id = ? AND task_id = ?", current.ID, command.TaskID).Error; err == nil {
				return preview, contentItemTaskAlreadyLinkedError()
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return preview, err
			}
			preview.Before = contentItemTaskBeforeFields("unlinked", false, response.Version)
			preview.After = contentItemTaskPreviewFields(task, "linked", *command.IsRequired, response.Version+1)
			break
		}
		relation, err := loadContentItemTaskRelation(tx, current.ID, command.TaskID)
		if err != nil {
			return preview, err
		}
		if operation == "requirement" && relation.IsRequired == *command.IsRequired {
			return preview, contentItemTaskRequirementUnchangedError()
		}
		preview.Before = contentItemTaskBeforeFields("linked", relation.IsRequired, response.Version)
		if operation == "unlink" {
			preview.After = contentItemTaskPreviewFields(task, "unlinked", relation.IsRequired, response.Version+1)
		} else {
			preview.After = contentItemTaskPreviewFields(task, "linked", *command.IsRequired, response.Version+1)
		}
	case "content_item.delete":
		_, impact, err := loadContentItemDeletionImpact(tx, current.ID, current.Version)
		if err != nil {
			return preview, err
		}
		preview.Before = map[string]any{
			"status":               current.Status,
			"content_item_version": current.Version,
		}
		preview.After = map[string]any{
			"content_item_deleted":              true,
			"content_item_task_links_deleted":   impact.TaskLinks,
			"content_item_inbox_sources_marked": impact.InboxSourcesMarked,
		}
	case "content_item.update":
		if response.Status == "archived" || response.Status == "published" {
			return preview, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Archived or published content cannot be edited by the assistant")
		}
		var update updateContentItemRequest
		_ = json.Unmarshal(input.Changes, &update)
		updates, err := contentItemUpdates(current, update)
		if err != nil {
			return preview, err
		}
		if projectID, exists := updates["project_id"].(*string); exists {
			if err := requireContentItemProject(tx, projectID); err != nil {
				return preview, err
			}
		}
		for field := range aiContentItemUpdateFields {
			if _, changed := rawObjectField(input.Changes, field); changed {
				preview.Before[field], preview.After[field] = before[field], updates[field]
			}
		}
	case "content_item.schedule":
		if response.Status == "archived" || response.Status == "published" || response.Status == "cancelled" {
			return preview, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Restore or reopen the content item before scheduling it")
		}
		var schedule struct {
			ScheduledAt       string `json:"scheduled_at"`
			ScheduledTimezone string `json:"scheduled_timezone"`
		}
		_ = json.Unmarshal(input.Changes, &schedule)
		at, zone, err := validateContentSchedule(&schedule.ScheduledAt, &schedule.ScheduledTimezone)
		if err != nil {
			return preview, err
		}
		preview.Before["scheduled_at"], preview.After["scheduled_at"] = response.ScheduledAt, at
		preview.Before["scheduled_timezone"], preview.After["scheduled_timezone"] = response.ScheduledTimezone, zone
		preview.Before["status"], preview.After["status"] = response.Status, "scheduled"
	case "content_item.unschedule":
		if response.Status == "archived" || response.Status == "published" || response.Status == "cancelled" {
			return preview, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Restore or reopen the content item before clearing its schedule")
		}
		if response.ScheduledAt == nil && response.ScheduledTimezone == nil {
			return preview, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Content item is not scheduled")
		}
		preview.Before["scheduled_at"], preview.After["scheduled_at"] = response.ScheduledAt, nil
		preview.Before["scheduled_timezone"], preview.After["scheduled_timezone"] = response.ScheduledTimezone, nil
		preview.Before["status"] = response.Status
		if response.Status == "scheduled" {
			preview.After["status"] = "draft"
		} else {
			preview.After["status"] = response.Status
		}
	default:
		target, _, err := contentItemLifecycleTarget(current, input.Action)
		if err != nil {
			return preview, err
		}
		preview.Before["status"], preview.After["status"] = response.Status, target
	}
	return preview, nil
}

func rawObjectField(raw json.RawMessage, key string) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	value, ok := fields[key]
	return value, ok
}

func (a *API) executeAIContentItemAction(tx *gorm.DB, input aiWorkspaceAction, requestID string, clock time.Time) (aiActionResult, error) {
	now := formatInboxTimestamp(clock.UTC())
	if input.Action == "content_item.delete" {
		return aiActionResult{}, errors.New("content item deletion must use the guarded approval transaction")
	}
	if isAIContentItemTaskAction(input.Action) {
		command, operation, err := aiContentItemTaskCommand(input)
		if err != nil {
			return aiActionResult{}, err
		}
		response, _, err := mutateContentItemTaskInTransaction(
			tx, input.ContentItemID, input.ExpectedVersion, command, operation, requestID, clock,
		)
		return aiActionResult{ID: response.ID, Version: response.Version}, err
	}
	if input.Action == "content_item.publish" {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &fields)
		publish, err := aiContentPublishInput(input.Changes, fields)
		if err != nil {
			return aiActionResult{}, err
		}
		response, err := publishContentItemInTransaction(tx, input.ContentItemID, input.ExpectedVersion, publish, requestID, clock, clock)
		return aiActionResult{ID: response.ID, Version: response.Version}, err
	}
	if input.Action == "content_item.create" {
		var create createContentItemRequest
		_ = json.Unmarshal(input.Changes, &create)
		item, err := contentItemFromCreateRequest(create)
		if err != nil {
			return aiActionResult{}, err
		}
		item.CreatedAt, item.UpdatedAt = now, now
		if err := requireContentItemProject(tx, item.ProjectID); err != nil {
			return aiActionResult{}, err
		}
		order, err := nextContentItemOrder(tx, item.ScheduledAt)
		if err != nil {
			return aiActionResult{}, err
		}
		item.ManualOrder = order
		if err := tx.Create(&item).Error; err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: item.ID, Version: item.Version}, nil
	}
	var item models.ContentItem
	if err := tx.First(&item, "id = ?", input.ContentItemID).Error; err != nil {
		return aiActionResult{}, contentItemNotFoundError(err)
	}
	if item.Version != input.ExpectedVersion {
		return aiActionResult{}, contentItemVersionConflict()
	}
	updates := map[string]any{}
	reason := "content_item_updated"
	switch input.Action {
	case "content_item.update":
		if item.Status == "archived" || item.Status == "published" {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Archived or published content cannot be edited by the assistant")
		}
		var update updateContentItemRequest
		_ = json.Unmarshal(input.Changes, &update)
		var err error
		updates, err = contentItemUpdates(item, update)
		if err != nil {
			return aiActionResult{}, err
		}
		if projectID, exists := updates["project_id"].(*string); exists {
			if err := requireContentItemProject(tx, projectID); err != nil {
				return aiActionResult{}, err
			}
		}
	case "content_item.schedule":
		if item.Status == "archived" || item.Status == "published" || item.Status == "cancelled" {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Restore or reopen the content item before scheduling it")
		}
		var schedule struct {
			ScheduledAt       string `json:"scheduled_at"`
			ScheduledTimezone string `json:"scheduled_timezone"`
		}
		_ = json.Unmarshal(input.Changes, &schedule)
		at, zone, err := validateContentSchedule(&schedule.ScheduledAt, &schedule.ScheduledTimezone)
		if err != nil {
			return aiActionResult{}, err
		}
		updates = map[string]any{"scheduled_at": at, "scheduled_timezone": zone, "status": "scheduled"}
		reason = "content_item_rescheduled"
	case "content_item.unschedule":
		if item.Status == "archived" || item.Status == "published" || item.Status == "cancelled" || (item.ScheduledAt == nil && item.ScheduledTimezone == nil) {
			return aiActionResult{}, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_STATE_INVALID", "Content item schedule cannot be cleared in its current state")
		}
		status := item.Status
		if status == "scheduled" {
			status = "draft"
		}
		updates = map[string]any{"scheduled_at": nil, "scheduled_timezone": nil, "status": status}
		reason = "content_item_rescheduled"
	default:
		target, archivedFrom, err := contentItemLifecycleTarget(item, input.Action)
		if err != nil {
			return aiActionResult{}, err
		}
		updates = map[string]any{"status": target}
		if input.Action == "content_item.archive" {
			updates["archived_from_status"] = archivedFrom
			reason = "content_item_archived"
		} else if input.Action == "content_item.restore" {
			updates["archived_from_status"] = nil
			reason = "content_item_restored"
		} else {
			reason = "content_item_status_changed"
		}
	}
	if err := resolveContentItemInboxSources(tx, item.ID, reason, requestID, now); err != nil {
		return aiActionResult{}, err
	}
	updates["updated_at"], updates["version"] = now, gorm.Expr("version + 1")
	result := tx.Model(&models.ContentItem{}).Where("id = ? AND version = ?", item.ID, input.ExpectedVersion).Updates(updates)
	if result.Error != nil {
		return aiActionResult{}, result.Error
	}
	if result.RowsAffected == 0 {
		return aiActionResult{}, contentItemVersionConflict()
	}
	var updated models.ContentItem
	if err := tx.First(&updated, "id = ?", item.ID).Error; err != nil {
		return aiActionResult{}, err
	}
	return aiActionResult{ID: updated.ID, Version: updated.Version}, nil
}
