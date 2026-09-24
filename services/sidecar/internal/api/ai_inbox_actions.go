package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiInboxChangeFields = map[string]bool{"title": true, "summary": true, "priority": true, "due_at": true}

const maxAIInboxReadAllItems = 1000

type aiInboxReadAllPreview struct {
	ThroughCreatedAt     string `json:"through_created_at"`
	CandidateCount       int64  `json:"candidate_count"`
	SelectionFingerprint string `json:"selection_fingerprint"`
}

func loadAIInboxReadAllPreview(tx *gorm.DB, through string) (aiInboxReadAllPreview, error) {
	var candidates []struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := inboxReadAllEligibleQuery(tx, through).
		Select("id", "version").
		Order("created_at ASC").Order("id ASC").
		Limit(maxAIInboxReadAllItems + 1).
		Scan(&candidates).Error; err != nil {
		return aiInboxReadAllPreview{}, err
	}
	if len(candidates) > maxAIInboxReadAllItems {
		return aiInboxReadAllPreview{}, newProjectRequestError(422, "AI_INBOX_READ_ALL_TOO_LARGE", "Inbox snapshot exceeds the 1000-item AI approval limit; use the Inbox page")
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		return aiInboxReadAllPreview{}, err
	}
	return aiInboxReadAllPreview{
		ThroughCreatedAt: through, CandidateCount: int64(len(candidates)),
		SelectionFingerprint: sha256Hex(append([]byte("ai-inbox-read-all-v1\n"), encoded...)),
	}, nil
}

func parseAIInboxAction(input aiWorkspaceAction, fields map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" {
		return input, errors.New("inbox actions only accept inbox_item_id as the target")
	}
	if input.Action == "inbox.read_all" {
		if input.InboxItemID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("read_all uses a snapshot cutoff, not an Inbox ID/version")
		}
		if len(fields) != 1 {
			return input, errors.New("read_all requires only through_created_at from workspace_search")
		}
		raw, ok := fields["through_created_at"]
		if !ok {
			return input, errors.New("read_all requires through_created_at from workspace_search")
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return input, errors.New("through_created_at must be a timestamp from workspace_search")
		}
		normalized, err := normalizeInboxTimestamp(value, "through_created_at")
		if err != nil {
			return input, err
		}
		fields["through_created_at"], _ = json.Marshal(normalized)
		input.Changes, _ = json.Marshal(fields)
		return input, nil
	} else if input.Action == "inbox.create" {
		if input.InboxItemID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept an Inbox ID/version")
		}
	} else if id, err := uuid.Parse(input.InboxItemID); err != nil || id.String() != input.InboxItemID || input.ExpectedVersion < 1 {
		return input, errors.New("use the canonical inbox_item_id and expected_version from workspace_get")
	}
	if input.Action == "inbox.create" || input.Action == "inbox.update" {
		for field, value := range fields {
			if !aiInboxChangeFields[field] {
				return input, errors.New("unsupported Inbox field: " + field)
			}
			if (field == "title" || field == "priority") && string(value) == "null" {
				return input, errors.New(field + " cannot be null")
			}
		}
		if input.Action == "inbox.create" {
			var create createInboxItemRequest
			if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
				return input, err
			}
			if _, err := normalizeInboxCreate(create); err != nil {
				return input, err
			}
		} else {
			var update updateInboxItemRequest
			if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
				return input, err
			}
			patch, err := normalizeInboxPatch(update)
			if err != nil {
				return input, err
			}
			if !patch.hasChanges {
				return input, errors.New("at least one Inbox field is required")
			}
		}
	} else if input.Action == "inbox.split" {
		normalized, err := parseAIInboxSplit(input)
		if err != nil {
			return input, err
		}
		// Persist the same trimmed/defaulted, de-duplicated drafts the human sees.
		// Reuse the native hash projection but represent root parent keys as null
		// so the canonical request remains valid when reparsed at confirmation.
		canonical := splitHashInput(normalized, 0).(map[string]any)
		delete(canonical, "expected_version")
		for _, task := range canonical["tasks"].([]map[string]any) {
			if task["parent_key"] == "" {
				task["parent_key"] = nil
			}
		}
		encoded, _ := json.Marshal(canonical)
		_ = json.Unmarshal(encoded, &fields)
	} else if isAIInboxTaskAction(input.Action) {
		if _, _, _, err := aiInboxTaskCommand(input); err != nil {
			return input, err
		}
	} else if _, err := aiInboxCommand(input); err != nil {
		return input, err
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func aiInboxReadAllCutoff(input aiWorkspaceAction) (string, error) {
	var fields struct {
		ThroughCreatedAt string `json:"through_created_at"`
	}
	if err := decodeStrictToolArguments(input.Changes, &fields); err != nil {
		return "", err
	}
	return normalizeInboxTimestamp(fields.ThroughCreatedAt, "through_created_at")
}

func aiInboxCommand(input aiWorkspaceAction) (inboxCommandHash, error) {
	command := inboxCommandHash{Command: strings.TrimPrefix(input.Action, "inbox."), ExpectedVersion: input.ExpectedVersion}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input.Changes, &fields)
	var allowed string
	switch command.Command {
	case "read", "unsnooze":
	case "snooze":
		allowed = "snoozed_until"
	case "resolve", "dismiss", "reopen", "force_resolve":
		allowed = "reason"
	default:
		return command, errors.New("unsupported Inbox action")
	}
	for field := range fields {
		if allowed == "" || field != allowed {
			return command, errors.New("unsupported Inbox command field: " + field)
		}
	}
	if allowed == "snoozed_until" {
		var value string
		if json.Unmarshal(fields[allowed], &value) != nil {
			return command, errors.New("snoozed_until is required")
		}
		var err error
		command.SnoozedUntil, err = normalizeInboxTimestamp(value, allowed)
		return command, err
	}
	if allowed == "reason" {
		var value string
		if raw, exists := fields[allowed]; exists {
			if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
				return command, errors.New("reason must be a string")
			}
		}
		var err error
		command.Reason, err = normalizeInboxReason(value, command.Command != "reopen")
		return command, err
	}
	return command, nil
}

func aiInboxActionFields(item models.InboxItem) map[string]any {
	return map[string]any{"title": item.Title, "summary": item.Summary, "priority": item.Priority, "due_at": item.DueAt, "status": item.Status}
}

func previewAIInboxAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	if input.Action == "inbox.read_all" {
		preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
		through, err := aiInboxReadAllCutoff(input)
		if err != nil {
			return preview, err
		}
		cutoff, err := time.Parse(time.RFC3339Nano, through)
		if err != nil || cutoff.After(now) {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", "through_created_at cannot be later than server_now")
		}
		snapshot, err := loadAIInboxReadAllPreview(tx, through)
		if err != nil {
			return preview, err
		}
		count := snapshot.CandidateCount
		if count == 0 {
			return preview, newProjectRequestError(409, "INBOX_READ_ALL_EMPTY", "The selected Inbox snapshot has no unread items")
		}
		preview.Label = fmt.Sprintf("将快照内 %d 条未读事项标为已读", count)
		preview.InboxReadAll = &snapshot
		preview.Before = map[string]any{"snapshot_at": through, "snapshot_unread_count": count}
		preview.After = map[string]any{"through_created_at": through, "snapshot_unread_count": int64(0), "marked_count": count}
		return preview, nil
	}
	if input.Action == "inbox.split" {
		return previewAIInboxSplit(tx, input)
	}
	if isAIInboxTaskAction(input.Action) {
		return previewAIInboxTaskAction(tx, input)
	}
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "inbox.create" {
		var create createInboxItemRequest
		_ = json.Unmarshal(input.Changes, &create)
		normalized, err := normalizeInboxCreate(create)
		if err != nil {
			return preview, err
		}
		preview.Label = normalized.Title
		preview.After = aiInboxActionFields(models.InboxItem{Title: normalized.Title, Summary: normalized.Summary, Priority: normalized.Priority, DueAt: normalized.DueAt, Status: "open"})
		return preview, nil
	}
	if input.Action == "inbox.update" {
		var update updateInboxItemRequest
		_ = json.Unmarshal(input.Changes, &update)
		patch, err := normalizeInboxPatch(update)
		if err != nil {
			return preview, err
		}
		current, next, err := prepareInboxUpdate(tx, input.InboxItemID, input.ExpectedVersion, patch)
		if err != nil {
			return preview, err
		}
		preview.Label = current.Title
		before, after := aiInboxActionFields(current), aiInboxActionFields(next)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &fields)
		for field := range fields {
			preview.Before[field], preview.After[field] = before[field], after[field]
		}
		return preview, nil
	}
	command, err := aiInboxCommand(input)
	if err != nil {
		return preview, err
	}
	if command.Command == "force_resolve" {
		current, err := prepareInboxForceResolve(tx, input.InboxItemID, input.ExpectedVersion)
		if err != nil {
			return preview, err
		}
		progress, err := loadInboxTaskProgress(tx, current.ID)
		if err != nil {
			return preview, err
		}
		preview.Label = current.Title
		preview.Before = map[string]any{
			"status": current.Status, "snoozed_until": current.SnoozedUntil,
			"resolution_policy": current.ResolutionPolicy,
			"active_task_count": progress.ActiveTotal, "required_task_count": progress.RequiredTotal,
			"required_done_count": progress.RequiredDone, "required_remaining_count": progress.RequiredRemaining,
			"required_blocked_count": progress.RequiredBlocked, "required_waiting_review_count": progress.RequiredWaitingReview,
			"required_cancelled_count": progress.RequiredCancelled,
		}
		for key, value := range preview.Before {
			preview.After[key] = value
		}
		preview.After["status"], preview.After["snoozed_until"] = "resolved", nil
		preview.After["resolution_mode"], preview.After["reason"] = "forced", command.Reason
		return preview, nil
	}
	current, next, _, err := prepareInboxCommand(tx, input.InboxItemID, command, now)
	if err != nil {
		return preview, err
	}
	preview.Label = current.Title
	switch command.Command {
	case "read":
		preview.Before["read_state"] = "unread"
		if current.ReadAt != nil {
			preview.Before["read_state"] = "read"
		}
		preview.After["read_state"] = "read"
	case "snooze", "unsnooze":
		preview.Before["snoozed_until"], preview.After["snoozed_until"] = current.SnoozedUntil, next.SnoozedUntil
	default:
		preview.Before["status"], preview.After["status"] = current.Status, next.Status
		preview.Before["snoozed_until"], preview.After["snoozed_until"] = current.SnoozedUntil, next.SnoozedUntil
		if command.Reason != "" {
			preview.After["reason"] = command.Reason
		}
	}
	return preview, nil
}

func executeAIInboxReadAllAction(tx *gorm.DB, input aiWorkspaceAction, requestID string, now time.Time) (readAllInboxItemsOutput, error) {
	through, err := aiInboxReadAllCutoff(input)
	if err != nil {
		return readAllInboxItemsOutput{}, err
	}
	return readAllInboxItemsInTransaction(tx, through, requestID, now)
}

func executeAIInboxAction(tx *gorm.DB, input aiWorkspaceAction, requestID, nowText string, confirmForceResolve bool) (aiActionResult, error) {
	now, err := time.Parse(time.RFC3339Nano, nowText)
	if err != nil {
		return aiActionResult{}, err
	}
	if input.Action == "inbox.split" {
		normalized, err := parseAIInboxSplit(input)
		if err != nil {
			return aiActionResult{}, err
		}
		result, err := splitInboxItemInTransaction(tx, input.InboxItemID, input.ExpectedVersion, normalized, requestID, now)
		out := aiActionResult{ID: result.InboxItem.ID, Version: result.InboxItem.Version}
		if err == nil {
			for _, created := range result.Created {
				out.CreatedTaskIDs = append(out.CreatedTaskIDs, created.Task.ID)
			}
		}
		return out, err
	}
	if isAIInboxTaskAction(input.Action) {
		change, _, command, err := aiInboxTaskCommand(input)
		if err != nil {
			return aiActionResult{}, err
		}
		result, err := mutateInboxTaskInTransaction(tx, input.InboxItemID, command, change, requestID, now)
		return aiActionResult{ID: result.InboxItem.ID, Version: result.InboxItem.Version}, err
	}
	var result inboxItemOutput
	switch input.Action {
	case "inbox.create":
		var create createInboxItemRequest
		_ = json.Unmarshal(input.Changes, &create)
		var normalized normalizedInboxCreate
		normalized, err = normalizeInboxCreate(create)
		if err == nil {
			result, err = createInboxItemInTransaction(tx, normalized, requestID, now)
		}
	case "inbox.update":
		var update updateInboxItemRequest
		_ = json.Unmarshal(input.Changes, &update)
		var patch normalizedInboxPatch
		patch, err = normalizeInboxPatch(update)
		if err == nil {
			result, err = updateInboxItemInTransaction(tx, input.InboxItemID, input.ExpectedVersion, patch, requestID, now)
		}
	default:
		var command inboxCommandHash
		command, err = aiInboxCommand(input)
		if err == nil {
			if command.Command == "force_resolve" {
				result, err = forceResolveInboxItemInTransaction(tx, input.InboxItemID, input.ExpectedVersion, command.Reason, confirmForceResolve, requestID, now)
			} else {
				result, err = commandInboxItemInTransaction(tx, input.InboxItemID, command, requestID, now)
			}
		}
	}
	return aiActionResult{ID: result.ID, Version: result.Version}, err
}
