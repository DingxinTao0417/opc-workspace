package api

import (
	"net/http"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Preview performs the same domain checks without granting consent or writing.
func prepareInboxForceResolve(tx *gorm.DB, id string, expectedVersion int64) (models.InboxItem, error) {
	current, err := loadInboxItem(tx, id)
	if err != nil {
		return current, inboxItemLoadError(err)
	}
	if current.Version != expectedVersion {
		return current, inboxVersionConflict()
	}
	if inboxItemTerminal(current.Status) {
		return current, inboxTerminalConflict("Only open or tracking Inbox Items can be force-resolved")
	}
	if current.ResolutionPolicy != "all_required_tasks_done" {
		return current, newProjectRequestError(http.StatusConflict, "INBOX_FORCE_RESOLVE_NOT_REQUIRED", "Manual-policy Inbox Items can be resolved with the regular resolve command")
	}
	return current, nil
}

// Human HTTP and AI approval both call this inside their own atomic transaction.
// The model cannot supply confirmed; it is provided by the human decision only.
func forceResolveInboxItemInTransaction(tx *gorm.DB, id string, expectedVersion int64, reason string, confirmed bool, requestID string, now time.Time) (inboxItemOutput, error) {
	var response inboxItemOutput
	if !confirmed {
		return response, newProjectRequestError(http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm must be true to force-resolve an Inbox Item")
	}
	reason, err := normalizeInboxReason(reason, true)
	if err != nil {
		return response, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
	}
	current, err := prepareInboxForceResolve(tx, id, expectedVersion)
	if err != nil {
		return response, err
	}
	nowText := formatInboxTimestamp(now)
	next := current
	ownerID := models.BuiltinOwnerActorID
	mode := "forced"
	next.Status = "resolved"
	next.SnoozedUntil = nil
	next.ResolvedByActorID = &ownerID
	next.ResolvedAt = &nowText
	next.ResolutionReason = &reason
	next.ResolutionMode = &mode
	if next.TriagedAt == nil {
		next.TriagedAt = &nowText
	}
	next.Version++
	next.UpdatedAt = nowText
	result := tx.Model(&models.InboxItem{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(inboxCommandUpdates(next))
	if result.Error != nil {
		return response, mapInboxTaskConstraintError(result.Error)
	}
	if result.RowsAffected != 1 {
		return response, inboxVersionConflict()
	}
	if err := recordInboxWorkflowEventAs(tx, id, "force_resolved", models.BuiltinOwnerActorID,
		inboxItemEventState(current, ""), inboxItemEventState(next, reason), requestID, nowText); err != nil {
		return response, err
	}
	return inboxItemOutputFromModel(next, now)
}
