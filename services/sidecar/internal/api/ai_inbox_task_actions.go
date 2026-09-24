package api

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func isAIInboxTaskAction(action string) bool {
	return action == "inbox.link_task" || action == "inbox.set_required" || action == "inbox.unlink_task"
}

func aiInboxTaskCommand(input aiWorkspaceAction) (inboxTaskCommandHash, int64, string, error) {
	var change struct {
		TaskID              string `json:"task_id"`
		ExpectedTaskVersion int64  `json:"expected_task_version"`
		IsRequired          *bool  `json:"is_required"`
		Reason              string `json:"reason"`
	}
	result := inboxTaskCommandHash{ExpectedVersion: input.ExpectedVersion}
	if err := decodeStrictToolArguments(input.Changes, &change); err != nil {
		return result, 0, "", err
	}
	if id, err := uuid.Parse(change.TaskID); err != nil || id.String() != change.TaskID || change.ExpectedTaskVersion < 1 {
		return result, 0, "", errors.New("use canonical task_id and expected_task_version from workspace_get or workspace_inbox_tasks")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input.Changes, &fields)
	command := map[string]string{"inbox.link_task": "link", "inbox.set_required": "requirement", "inbox.unlink_task": "unlink"}[input.Action]
	if command == "" {
		return result, 0, "", errors.New("unsupported Inbox Task action")
	}
	for field, value := range fields {
		allowed := field == "task_id" || field == "expected_task_version" ||
			(command != "unlink" && field == "is_required") || (command == "unlink" && field == "reason")
		if !allowed || string(value) == "null" {
			return result, 0, "", errors.New("unsupported or null Inbox Task field: " + field)
		}
	}
	if command != "unlink" && change.IsRequired == nil {
		return result, 0, "", errors.New("is_required is required")
	}
	if command == "unlink" {
		reason, err := validateAssignmentReason(change.Reason)
		if err != nil {
			return result, 0, "", err
		}
		change.Reason = reason
	}
	result.TaskID, result.IsRequired, result.Reason = change.TaskID, change.IsRequired, change.Reason
	return result, change.ExpectedTaskVersion, command, nil
}

func previewAIInboxTaskAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	change, taskVersion, command, err := aiInboxTaskCommand(input)
	if err != nil {
		return preview, err
	}
	current, err := prepareInboxTaskMutation(tx, input.InboxItemID, input.ExpectedVersion)
	if err != nil {
		return preview, err
	}
	task, err := loadInboxTaskSummary(tx, change.TaskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
	}
	if err != nil {
		return preview, err
	}
	if task.Version != taskVersion {
		return preview, taskVersionConflict()
	}
	progress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return preview, err
	}
	next := progress
	beforeRequired, afterRequired := false, false
	beforeRelation, afterRelation := "unlinked", "linked"
	status := current.Status
	changed := true
	if command == "link" {
		if progress.ActiveTotal >= maxActiveInboxTasks {
			return preview, newProjectRequestError(409, "INBOX_TASK_LIMIT_REACHED", "An Inbox Item cannot have more than 100 active Task relations")
		}
		var count int64
		if err := tx.Table("inbox_item_tasks").Where("inbox_item_id=? AND task_ref_id=? AND unlinked_at IS NULL", current.ID, change.TaskID).Count(&count).Error; err != nil {
			return preview, err
		}
		if count > 0 {
			return preview, inboxTaskAlreadyLinkedError()
		}
		next.ActiveTotal++
		afterRequired = *change.IsRequired
		status = "tracking"
	} else {
		relation, err := loadActiveInboxTaskRelation(tx, current.ID, change.TaskID)
		if err != nil {
			return preview, err
		}
		beforeRequired = relation.IsRequired
		beforeRelation = "linked"
		if command == "unlink" {
			afterRelation = "unlinked"
			next.ActiveTotal--
			status = "tracking"
			if next.ActiveTotal == 0 {
				status = "open"
			}
		} else {
			afterRequired = *change.IsRequired
			changed = beforeRequired != afterRequired
		}
	}
	delta := int64(0)
	if beforeRequired {
		delta--
	}
	if afterRequired {
		delta++
	}
	next.RequiredTotal += delta
	if task.Status == "done" {
		next.RequiredDone += delta
	}
	next.RequiredRemaining = next.RequiredTotal - next.RequiredDone
	// Same non-empty condition as domain reconciliation. A no-op requirement
	// command intentionally does not reconcile, matching the human API.
	if changed && current.ResolutionPolicy == "all_required_tasks_done" && next.RequiredTotal > 0 && next.RequiredRemaining == 0 {
		status = "resolved"
	}
	preview.Label = current.Title
	preview.Before = map[string]any{
		"relation_state": beforeRelation, "is_required": beforeRequired, "status": current.Status,
		"active_task_count": progress.ActiveTotal, "required_task_count": progress.RequiredTotal,
		"required_done_count": progress.RequiredDone, "required_remaining_count": progress.RequiredRemaining,
	}
	preview.After = map[string]any{
		"task_id": task.ID, "task_title": task.Title, "task_version": task.Version, "task_status": task.Status,
		"relation_state": afterRelation, "is_required": afterRequired, "status": status, "resolution_policy": current.ResolutionPolicy,
		"active_task_count": next.ActiveTotal, "required_task_count": next.RequiredTotal,
		"required_done_count": next.RequiredDone, "required_remaining_count": next.RequiredRemaining,
	}
	if status == "resolved" && current.SnoozedUntil != nil {
		preview.Before["snoozed_until"], preview.After["snoozed_until"] = current.SnoozedUntil, nil
	}
	if change.Reason != "" {
		preview.After["reason"] = change.Reason
	}
	return preview, nil
}
