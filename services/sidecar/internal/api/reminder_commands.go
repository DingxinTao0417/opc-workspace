package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func normalizeReminderCancelReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 1 || n > 1000 {
		return "", errors.New("reason must contain 1 to 1000 characters")
	}
	return reason, nil
}

// Callers own the transaction and idempotency/approval snapshot. These commands
// are shared by the human Reminder API and explicitly approved AI suggestions.
func createReminderInTransaction(tx *gorm.DB, normalized normalizedReminderCreate, requestID, nowText string) (reminderOutput, error) {
	id := uuid.NewString()
	reminder := models.Reminder{
		ID: id, SourceEntityType: "manual", Title: normalized.Title,
		Summary: normalized.Summary, Priority: normalized.Priority,
		TriggerAt: normalized.TriggerAt, Status: "scheduled",
		SourceEventKey:   "reminder:" + id + ":due",
		CreatedByActorID: models.BuiltinOwnerActorID,
		SeriesID:         id, RecurrenceType: normalized.RecurrenceType,
		RecurrenceInterval: normalized.RecurrenceInterval,
		RecurrenceTimezone: normalized.RecurrenceTimezone, OccurrenceNumber: 1,
		RecurrenceAnchorDay: normalized.RecurrenceAnchorDay,
		Version:             1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&reminder).Error; err != nil {
		return reminderOutput{}, fmt.Errorf("create Reminder: %w", err)
	}
	if err := recordReminderWorkflowEvent(tx, reminder.ID, "reminder_created", nil, reminderEventState(reminder, ""), models.BuiltinOwnerActorID, requestID, nowText); err != nil {
		return reminderOutput{}, err
	}
	return reminderOutputFromModel(reminder), nil
}

func prepareReminderUpdate(tx *gorm.DB, id string, expectedVersion int64, patch map[string]any) (models.Reminder, models.Reminder, error) {
	current, err := loadReminder(tx, id)
	if err != nil {
		return current, current, reminderLoadError(err)
	}
	if current.Version != expectedVersion {
		return current, current, reminderVersionConflict()
	}
	if current.Status != "scheduled" {
		return current, current, reminderTerminalConflict()
	}
	next := current
	applyReminderPatch(&next, patch)
	if reminderPatchChangesAnchor(patch) {
		anchorDay, err := reminderRecurrenceAnchorDay(next.RecurrenceType, next.TriggerAt, next.RecurrenceTimezone)
		if err != nil {
			return current, current, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		next.RecurrenceAnchorDay = anchorDay
	}
	if err := validateReminderRecurrence(next.RecurrenceType, next.RecurrenceInterval, next.RecurrenceTimezone, next.RecurrenceAnchorDay); err != nil {
		return current, current, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
	}
	return current, next, nil
}

func updateReminderInTransaction(tx *gorm.DB, id string, expectedVersion int64, patch map[string]any, requestID, nowText string) (reminderOutput, error) {
	current, next, err := prepareReminderUpdate(tx, id, expectedVersion, patch)
	if err != nil {
		return reminderOutput{}, err
	}
	if reminderEditableEqual(current, next) {
		return reminderOutputFromModel(current), nil
	}
	next.Version++
	next.UpdatedAt = nowText
	updates := map[string]any{
		"title": next.Title, "summary": next.Summary, "priority": next.Priority,
		"trigger_at": next.TriggerAt, "version": next.Version, "updated_at": next.UpdatedAt,
		"recurrence_type": next.RecurrenceType, "recurrence_interval": next.RecurrenceInterval,
		"recurrence_timezone":   next.RecurrenceTimezone,
		"recurrence_anchor_day": next.RecurrenceAnchorDay,
	}
	result := tx.Model(&models.Reminder{}).
		Where("id = ? AND version = ? AND status = 'scheduled'", id, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return reminderOutput{}, result.Error
	}
	if result.RowsAffected != 1 {
		return reminderOutput{}, reminderVersionConflict()
	}
	if err := recordReminderWorkflowEvent(tx, id, "reminder_updated", reminderEventState(current, ""), reminderEventState(next, ""), models.BuiltinOwnerActorID, requestID, nowText); err != nil {
		return reminderOutput{}, err
	}
	return reminderOutputFromModel(next), nil
}

func cancelReminderInTransaction(tx *gorm.DB, id string, expectedVersion int64, reason, requestID, nowText string) (reminderOutput, error) {
	current, err := loadReminder(tx, id)
	if err != nil {
		return reminderOutput{}, reminderLoadError(err)
	}
	if current.Version != expectedVersion {
		return reminderOutput{}, reminderVersionConflict()
	}
	if current.Status != "scheduled" {
		return reminderOutput{}, reminderTerminalConflict()
	}
	ownerID := models.BuiltinOwnerActorID
	next := current
	next.Status = "cancelled"
	next.CancelledByActorID = &ownerID
	next.CancelledAt = &nowText
	next.CancelReason = &reason
	next.Version++
	next.UpdatedAt = nowText
	result := tx.Model(&models.Reminder{}).
		Where("id = ? AND version = ? AND status = 'scheduled'", id, expectedVersion).
		Updates(map[string]any{
			"status": next.Status, "cancelled_by_actor_id": ownerID,
			"cancelled_at": nowText, "cancel_reason": reason,
			"version": next.Version, "updated_at": nowText,
		})
	if result.Error != nil {
		return reminderOutput{}, result.Error
	}
	if result.RowsAffected != 1 {
		return reminderOutput{}, reminderVersionConflict()
	}
	if err := recordReminderWorkflowEvent(tx, id, "reminder_cancelled", reminderEventState(current, ""), reminderEventState(next, reason), ownerID, requestID, nowText); err != nil {
		return reminderOutput{}, err
	}
	return reminderOutputFromModel(next), nil
}
