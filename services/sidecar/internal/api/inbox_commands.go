package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared with human HTTP commands. The caller owns the transaction so business
// facts, domain events and an AI approval decision commit or roll back together.
func createInboxItemInTransaction(tx *gorm.DB, normalized normalizedInboxCreate, requestID string, now time.Time) (response inboxItemOutput, err error) {
	nowText := formatInboxTimestamp(now)
	item := models.InboxItem{
		ID: uuid.NewString(), Kind: "manual", Title: normalized.Title, Summary: normalized.Summary,
		SourceEntityType: "manual", Priority: normalized.Priority, Status: "open",
		ResolutionPolicy: "manual", DueAt: normalized.DueAt, PayloadJSON: normalized.PayloadJSON,
		Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&item).Error; err != nil {
		return response, fmt.Errorf("create Inbox Item: %w", err)
	}
	if err := recordInboxWorkflowEvent(tx, item.ID, "created", nil, inboxItemEventState(item, ""), requestID, nowText); err != nil {
		return response, err
	}
	response, err = inboxItemOutputFromModel(item, now)
	if err != nil {
		return response, err
	}

	return response, nil
}

func prepareInboxUpdate(tx *gorm.DB, id string, expectedVersion int64, patch normalizedInboxPatch) (current, next models.InboxItem, err error) {
	current, err = loadInboxItem(tx, id)
	if err != nil {
		return current, next, inboxItemLoadError(err)
	}
	if current.Version != expectedVersion {
		return current, next, inboxVersionConflict()
	}
	if inboxItemTerminal(current.Status) {
		return current, next, inboxTerminalConflict("Archived Inbox Items must be reopened before editing")
	}
	next = current
	patch.apply(&next)
	if (current.SourceEntityType == systemMaintenanceInboxSourceType ||
		current.SourceEntityType == projectCompletionInboxSourceType) &&
		!equalStringPointers(current.DueAt, next.DueAt) {
		message := "system maintenance Inbox Items cannot have a due date"
		if current.SourceEntityType == projectCompletionInboxSourceType {
			message = "Project completion Inbox Items cannot have a due date"
		}
		return current, next, newProjectRequestError(
			http.StatusUnprocessableEntity,
			"VALIDATION_ERROR",
			message,
		)
	}

	return current, next, nil
}

func updateInboxItemInTransaction(tx *gorm.DB, id string, expectedVersion int64, patch normalizedInboxPatch, requestID string, now time.Time) (response inboxItemOutput, err error) {
	nowText := formatInboxTimestamp(now)
	current, next, err := prepareInboxUpdate(tx, id, expectedVersion, patch)
	if err != nil {
		return response, err
	}
	if inboxItemEditableEqual(current, next) {
		response, err = inboxItemOutputFromModel(current, now)
		return response, err
	}
	if next.TriagedAt == nil {
		next.TriagedAt = &nowText
	}
	next.Version = current.Version + 1
	next.UpdatedAt = nowText
	result := tx.Model(&models.InboxItem{}).
		Where("id = ? AND version = ? AND status IN ('open', 'tracking')", id, expectedVersion).
		Updates(map[string]any{
			"title": next.Title, "summary": next.Summary, "priority": next.Priority,
			"due_at": next.DueAt, "triaged_at": next.TriagedAt,
			"version": next.Version, "updated_at": next.UpdatedAt,
		})
	if result.Error != nil {
		return response, result.Error
	}
	if result.RowsAffected == 0 {
		return response, inboxVersionConflict()
	}
	if err := recordInboxWorkflowEvent(tx, id, "updated", inboxItemEventState(current, ""), inboxItemEventState(next, ""), requestID, nowText); err != nil {
		return response, err
	}
	response, err = inboxItemOutputFromModel(next, now)
	return response, err
}

func prepareInboxCommand(tx *gorm.DB, id string, input inboxCommandHash, now time.Time) (current, next models.InboxItem, changed bool, err error) {
	command, commandInput, expectedVersion := input.Command, input, input.ExpectedVersion
	nowText := formatInboxTimestamp(now)
	if command == "snooze" {
		through, parseErr := time.Parse(time.RFC3339Nano, commandInput.SnoozedUntil)
		if parseErr != nil || !through.After(now) {
			return current, next, false, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "snoozed_until must be later than server_now")
		}
	}
	var loadErr error
	current, loadErr = loadInboxItem(tx, id)
	if loadErr != nil {
		return current, next, false, inboxItemLoadError(loadErr)
	}
	if current.Version != expectedVersion {
		return current, next, false, inboxVersionConflict()
	}
	if command == "resolve" && current.ResolutionPolicy == "all_required_tasks_done" {
		progress, progressErr := loadInboxTaskProgress(tx, id)
		if progressErr != nil {
			return current, next, false, progressErr
		}
		if progress.RequiredTotal == 0 || !progress.AllRequiredDone {
			return current, next, false, newProjectRequestError(
				http.StatusConflict,
				"INBOX_REQUIRED_TASKS_INCOMPLETE",
				"All active required Tasks must be done before resolving this Inbox Item; use force-resolve for an exception",
			)
		}
	}
	reopenTracking := false
	if command == "reopen" {
		var activeRelations int64
		if err := tx.Model(&models.InboxItemTask{}).
			Where("inbox_item_id = ? AND unlinked_at IS NULL", id).
			Count(&activeRelations).Error; err != nil {
			return current, next, false, err
		}
		reopenTracking = activeRelations > 0
	}
	var transitionErr error
	next, changed, transitionErr = applyInboxCommand(current, commandInput, nowText, reopenTracking)
	if transitionErr != nil {
		return current, next, false, transitionErr
	}

	return current, next, changed, nil
}

func commandInboxItemInTransaction(tx *gorm.DB, id string, commandInput inboxCommandHash, requestID string, now time.Time) (response inboxItemOutput, err error) {
	command, expectedVersion := commandInput.Command, commandInput.ExpectedVersion
	nowText := formatInboxTimestamp(now)
	current, next, changed, err := prepareInboxCommand(tx, id, commandInput, now)
	if err != nil {
		return response, err
	}
	if changed {
		result := tx.Model(&models.InboxItem{}).
			Where("id = ? AND version = ?", id, expectedVersion).
			Updates(inboxCommandUpdates(next))
		if result.Error != nil {
			return response, result.Error
		}
		if result.RowsAffected == 0 {
			return response, inboxVersionConflict()
		}
		if err := recordInboxWorkflowEvent(
			tx, id, inboxCommandEventAction(command),
			inboxItemEventState(current, ""), inboxItemEventState(next, commandInput.Reason),
			requestID, nowText,
		); err != nil {
			return response, err
		}
	}
	response, err = inboxItemOutputFromModel(next, now)
	if err != nil {
		return response, err
	}

	return response, nil
}
