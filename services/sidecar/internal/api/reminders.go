package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createReminderEndpoint = "POST /api/v1/reminders"

var validReminderStatuses = map[string]struct{}{
	"scheduled": {}, "fired": {}, "cancelled": {},
}

type createReminderRequest struct {
	Title              string  `json:"title"`
	Summary            *string `json:"summary"`
	Priority           *string `json:"priority"`
	TriggerAt          string  `json:"trigger_at"`
	RecurrenceType     *string `json:"recurrence_type"`
	RecurrenceInterval *int    `json:"recurrence_interval"`
	RecurrenceTimezone *string `json:"recurrence_timezone"`
}

type updateReminderRequest struct {
	Title              nullableStringPatch `json:"title"`
	Summary            nullableStringPatch `json:"summary"`
	Priority           nullableStringPatch `json:"priority"`
	TriggerAt          nullableStringPatch `json:"trigger_at"`
	RecurrenceType     nullableStringPatch `json:"recurrence_type"`
	RecurrenceInterval nullableIntPatch    `json:"recurrence_interval"`
	RecurrenceTimezone nullableStringPatch `json:"recurrence_timezone"`
}

type cancelReminderRequest struct {
	Reason string `json:"reason"`
}

type normalizedReminderCreate struct {
	Title               string
	Summary             string
	Priority            string
	TriggerAt           string
	RecurrenceType      string
	RecurrenceInterval  int
	RecurrenceTimezone  string
	RecurrenceAnchorDay int
}

type reminderOutput struct {
	ID                  string   `json:"id"`
	SourceEntityType    string   `json:"source_entity_type"`
	SourceEntityID      *string  `json:"source_entity_id"`
	Title               string   `json:"title"`
	Summary             string   `json:"summary"`
	Priority            string   `json:"priority"`
	TriggerAt           string   `json:"trigger_at"`
	Status              string   `json:"status"`
	SourceEventKey      string   `json:"source_event_key"`
	CreatedByActorID    string   `json:"created_by_actor_id"`
	SeriesID            string   `json:"series_id"`
	RecurrenceType      string   `json:"recurrence_type"`
	RecurrenceInterval  int      `json:"recurrence_interval"`
	RecurrenceTimezone  string   `json:"recurrence_timezone"`
	OccurrenceNumber    int64    `json:"occurrence_number"`
	RecurrenceAnchorDay int      `json:"recurrence_anchor_day"`
	FiredAt             *string  `json:"fired_at"`
	InboxItemID         *string  `json:"inbox_item_id"`
	CancelledByActorID  *string  `json:"cancelled_by_actor_id"`
	CancelledAt         *string  `json:"cancelled_at"`
	CancelReason        *string  `json:"cancel_reason"`
	Version             int64    `json:"version"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	AvailableActions    []string `json:"available_actions"`
}

type reminderListMeta struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Total     int64  `json:"total"`
	ServerNow string `json:"server_now"`
}

type reminderCreateHash struct {
	Title               string `json:"title"`
	Summary             string `json:"summary"`
	Priority            string `json:"priority"`
	TriggerAt           string `json:"trigger_at"`
	RecurrenceType      string `json:"recurrence_type"`
	RecurrenceInterval  int    `json:"recurrence_interval"`
	RecurrenceTimezone  string `json:"recurrence_timezone"`
	RecurrenceAnchorDay int    `json:"recurrence_anchor_day"`
}

type reminderCancelHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (a *API) listReminders(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, valid := validReminderStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
	}
	search := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(search) > 200 {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "q cannot exceed 200 characters")
		return
	}

	query := a.db.WithContext(c.Request.Context()).Model(&models.Reminder{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if search != "" {
		like := "%" + escapeLike(search) + "%"
		query = query.Where("(title LIKE ? ESCAPE '\\' OR summary LIKE ? ESCAPE '\\')", like, like)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	sorted, valid := applyReminderSort(query, c.Query("sort"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	var reminders []models.Reminder
	if err := sorted.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&reminders).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	data := make([]reminderOutput, len(reminders))
	for index := range reminders {
		data[index] = reminderOutputFromModel(reminders[index])
	}
	nowText := formatInboxTimestamp(a.options.Now().UTC())
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": reminderListMeta{
		Page: page, PageSize: pageSize, Total: total, ServerNow: nowText,
	}})
}

func (a *API) createReminder(c *gin.Context) {
	var input createReminderRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	now := a.options.Now().UTC()
	normalized, err := normalizeReminderCreate(input, now)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := inboxRequestHash(reminderCreateHash(normalized))
	if err != nil {
		writeDatabaseError(c)
		return
	}

	statusCode := http.StatusCreated
	replayed := false
	var response reminderOutput
	nowText := formatInboxTimestamp(now)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayReminderSnapshot(tx, idempotencyKey, createReminderEndpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
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
			return fmt.Errorf("create Reminder: %w", err)
		}
		response = reminderOutputFromModel(reminder)
		if err := recordReminderWorkflowEvent(tx, reminder.ID, "reminder_created", nil, reminderEventState(reminder, ""), models.BuiltinOwnerActorID, requestIDFromContext(c), nowText); err != nil {
			return err
		}
		return recordReminderSnapshot(tx, idempotencyKey, createReminderEndpoint, reminder.ID, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, mapReminderConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getReminder(c *gin.Context) {
	id, ok := reminderID(c)
	if !ok {
		return
	}
	var reminder models.Reminder
	if err := a.db.WithContext(c.Request.Context()).First(&reminder, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "REMINDER_NOT_FOUND", "Reminder not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := reminderOutputFromModel(reminder)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateReminder(c *gin.Context) {
	id, ok := reminderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateReminderRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	now := a.options.Now().UTC()
	patch, err := normalizeReminderPatch(input, now)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	if len(patch) == 0 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable field is required")
		return
	}

	var response reminderOutput
	nowText := formatInboxTimestamp(now)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		current, err := loadReminder(tx, id)
		if err != nil {
			return reminderLoadError(err)
		}
		if current.Version != expectedVersion {
			return reminderVersionConflict()
		}
		if current.Status != "scheduled" {
			return reminderTerminalConflict()
		}
		next := current
		applyReminderPatch(&next, patch)
		if reminderPatchChangesAnchor(patch) {
			anchorDay, err := reminderRecurrenceAnchorDay(next.RecurrenceType, next.TriggerAt, next.RecurrenceTimezone)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
			next.RecurrenceAnchorDay = anchorDay
		}
		if err := validateReminderRecurrence(next.RecurrenceType, next.RecurrenceInterval, next.RecurrenceTimezone, next.RecurrenceAnchorDay); err != nil {
			return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		if reminderEditableEqual(current, next) {
			response = reminderOutputFromModel(current)
			return nil
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
			return result.Error
		}
		if result.RowsAffected != 1 {
			return reminderVersionConflict()
		}
		if err := recordReminderWorkflowEvent(tx, id, "reminder_updated", reminderEventState(current, ""), reminderEventState(next, ""), models.BuiltinOwnerActorID, requestIDFromContext(c), nowText); err != nil {
			return err
		}
		response = reminderOutputFromModel(next)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, mapReminderConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) cancelReminder(c *gin.Context) {
	id, ok := reminderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input cancelReminderRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	reason := strings.TrimSpace(input.Reason)
	if utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 1000 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason must contain 1 to 1000 characters")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	endpoint := "DELETE /api/v1/reminders/" + id
	requestHash, err := inboxRequestHash(reminderCancelHash{ExpectedVersion: expectedVersion, Reason: reason})
	if err != nil {
		writeDatabaseError(c)
		return
	}

	replayed := false
	var response reminderOutput
	nowText := formatInboxTimestamp(a.options.Now().UTC())
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, _, replayErr := replayReminderSnapshot(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			return nil
		}
		current, err := loadReminder(tx, id)
		if err != nil {
			return reminderLoadError(err)
		}
		if current.Version != expectedVersion {
			return reminderVersionConflict()
		}
		if current.Status != "scheduled" {
			return reminderTerminalConflict()
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
			return result.Error
		}
		if result.RowsAffected != 1 {
			return reminderVersionConflict()
		}
		if err := recordReminderWorkflowEvent(tx, id, "reminder_cancelled", reminderEventState(current, ""), reminderEventState(next, reason), ownerID, requestIDFromContext(c), nowText); err != nil {
			return err
		}
		response = reminderOutputFromModel(next)
		return recordReminderSnapshot(tx, idempotencyKey, endpoint, id, requestHash, http.StatusOK, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, mapReminderConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) projectDueReminders(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	nowText := formatInboxTimestamp(a.options.Now().UTC())
	var ids []string
	if err := a.db.WithContext(ctx).Model(&models.Reminder{}).
		Where("status = 'scheduled' AND trigger_at <= ?", nowText).
		Order("trigger_at ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list due Reminders: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.projectReminder(ctx, id, nowText); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectReminder(ctx context.Context, id, nowText string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reminder, err := loadReminder(tx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if reminder.Status != "scheduled" || reminder.TriggerAt > nowText {
			return nil
		}

		var inbox models.InboxItem
		createdInbox := false
		err = tx.First(&inbox, "source_event_key = ?", reminder.SourceEventKey).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			inboxID := uuid.NewString()
			sourceType := "reminder"
			payload, encodeErr := json.Marshal(map[string]any{
				"reminder_id": reminder.ID, "trigger_at": reminder.TriggerAt,
			})
			if encodeErr != nil {
				return encodeErr
			}
			inbox = models.InboxItem{
				ID: inboxID, Kind: "reminder", Title: reminder.Title, Summary: reminder.Summary,
				SourceEntityType: sourceType, SourceEntityID: &reminder.ID,
				SourceEventKey: &reminder.SourceEventKey, Priority: reminder.Priority,
				Status: "open", ResolutionPolicy: "manual", DueAt: &reminder.TriggerAt,
				PayloadJSON: string(payload), Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
			}
			if err := tx.Create(&inbox).Error; err != nil {
				return fmt.Errorf("create Reminder Inbox Item: %w", err)
			}
			createdInbox = true
		} else if err != nil {
			return err
		} else if inbox.Kind != "reminder" || inbox.SourceEntityType != "reminder" || inbox.SourceEntityID == nil || *inbox.SourceEntityID != reminder.ID {
			return errors.New("Reminder source_event_key belongs to an incompatible Inbox Item")
		}

		previous := reminder
		reminder.Status = "fired"
		reminder.FiredAt = &nowText
		reminder.InboxItemID = &inbox.ID
		reminder.Version++
		reminder.UpdatedAt = nowText
		result := tx.Model(&models.Reminder{}).
			Where("id = ? AND version = ? AND status = 'scheduled'", reminder.ID, previous.Version).
			Updates(map[string]any{
				"status": "fired", "fired_at": nowText, "inbox_item_id": inbox.ID,
				"version": reminder.Version, "updated_at": nowText,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if createdInbox {
			if err := recordReminderInboxEvent(tx, inbox, nowText); err != nil {
				return err
			}
		}
		if err := recordReminderWorkflowEvent(tx, reminder.ID, "reminder_fired", reminderEventState(previous, ""), reminderEventState(reminder, ""), models.BuiltinSystemActorID, "", nowText); err != nil {
			return err
		}
		return scheduleNextReminderOccurrence(tx, reminder, nowText)
	})
}

func scheduleNextReminderOccurrence(tx *gorm.DB, fired models.Reminder, nowText string) error {
	if fired.RecurrenceType == "none" {
		return nil
	}
	now, err := time.Parse(time.RFC3339Nano, nowText)
	if err != nil {
		return fmt.Errorf("parse Reminder projection time: %w", err)
	}
	nextAt, advances, err := nextReminderOccurrence(fired, now)
	if err != nil {
		return err
	}
	if advances < 1 || int64(advances) > (1<<63-1)-fired.OccurrenceNumber {
		return errors.New("Reminder occurrence number overflow")
	}
	id := uuid.NewString()
	next := models.Reminder{
		ID: id, SourceEntityType: fired.SourceEntityType, SourceEntityID: fired.SourceEntityID,
		Title: fired.Title, Summary: fired.Summary, Priority: fired.Priority,
		TriggerAt: formatInboxTimestamp(nextAt.UTC()), Status: "scheduled",
		SourceEventKey: "reminder:" + id + ":due", CreatedByActorID: fired.CreatedByActorID,
		SeriesID: fired.SeriesID, RecurrenceType: fired.RecurrenceType,
		RecurrenceInterval: fired.RecurrenceInterval, RecurrenceTimezone: fired.RecurrenceTimezone,
		OccurrenceNumber:    fired.OccurrenceNumber + int64(advances),
		RecurrenceAnchorDay: fired.RecurrenceAnchorDay,
		Version:             1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&next).Error; err != nil {
		return fmt.Errorf("create next recurring Reminder: %w", err)
	}
	return recordReminderWorkflowEvent(
		tx, next.ID, "reminder_recurrence_scheduled", nil, reminderEventState(next, ""),
		models.BuiltinSystemActorID, "", nowText,
	)
}

func nextReminderOccurrence(reminder models.Reminder, now time.Time) (time.Time, int, error) {
	if err := validateReminderRecurrence(reminder.RecurrenceType, reminder.RecurrenceInterval, reminder.RecurrenceTimezone, reminder.RecurrenceAnchorDay); err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid recurring Reminder: %w", err)
	}
	triggerAt, err := time.Parse(time.RFC3339Nano, reminder.TriggerAt)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse recurring Reminder trigger_at: %w", err)
	}
	location, err := time.LoadLocation(reminder.RecurrenceTimezone)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("load recurring Reminder timezone: %w", err)
	}
	base := triggerAt.In(location)
	localNow := now.In(location)
	if reminder.RecurrenceType == "monthly" {
		monthDifference := (localNow.Year()-base.Year())*12 + int(localNow.Month()-base.Month())
		advances := monthDifference / reminder.RecurrenceInterval
		if advances < 1 {
			advances = 1
		}
		candidate := monthlyReminderOccurrence(base, advances*reminder.RecurrenceInterval, reminder.RecurrenceAnchorDay)
		for !candidate.After(now) {
			advances++
			candidate = monthlyReminderOccurrence(base, advances*reminder.RecurrenceInterval, reminder.RecurrenceAnchorDay)
		}
		for advances > 1 {
			previous := monthlyReminderOccurrence(base, (advances-1)*reminder.RecurrenceInterval, reminder.RecurrenceAnchorDay)
			if !previous.After(now) {
				break
			}
			advances--
			candidate = previous
		}
		return candidate, advances, nil
	}
	stepDays := reminder.RecurrenceInterval
	if reminder.RecurrenceType == "weekly" {
		stepDays *= 7
	}
	baseDate := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.UTC)
	nowDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	differenceDays := int(nowDate.Sub(baseDate) / (24 * time.Hour))
	advances := 1
	if differenceDays > 0 {
		advances = differenceDays / stepDays
		if advances < 1 {
			advances = 1
		}
	}
	candidate := base.AddDate(0, 0, advances*stepDays)
	for !candidate.After(now) {
		advances++
		candidate = base.AddDate(0, 0, advances*stepDays)
	}
	for advances > 1 {
		previous := base.AddDate(0, 0, (advances-1)*stepDays)
		if !previous.After(now) {
			break
		}
		advances--
		candidate = previous
	}
	return candidate, advances, nil
}

func monthlyReminderOccurrence(base time.Time, monthOffset, anchorDay int) time.Time {
	first := time.Date(base.Year(), base.Month()+time.Month(monthOffset), 1, base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), base.Location())
	lastDay := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, base.Location()).Day()
	day := anchorDay
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, base.Hour(), base.Minute(), base.Second(), base.Nanosecond(), base.Location())
}

func validateReminderRecurrence(recurrenceType string, interval int, timezone string, anchorDay int) error {
	if recurrenceType != "none" && recurrenceType != "daily" && recurrenceType != "weekly" && recurrenceType != "monthly" {
		return errors.New("recurrence_type must be none, daily, weekly, or monthly")
	}
	if interval < 1 || interval > 365 {
		return errors.New("recurrence_interval must be between 1 and 365")
	}
	if recurrenceType == "none" {
		if interval != 1 || timezone != "UTC" || anchorDay != 1 {
			return errors.New("one-time Reminders require recurrence_interval 1, recurrence_timezone UTC, and recurrence_anchor_day 1")
		}
		return nil
	}
	if recurrenceType != "monthly" && anchorDay != 1 {
		return errors.New("daily and weekly Reminders require recurrence_anchor_day 1")
	}
	if recurrenceType == "monthly" && (anchorDay < 1 || anchorDay > 31) {
		return errors.New("monthly Reminders require recurrence_anchor_day between 1 and 31")
	}
	if timezone == "" || len(timezone) > 100 || timezone == "Local" {
		return errors.New("recurrence_timezone must be a stable IANA timezone")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return errors.New("recurrence_timezone must be a stable IANA timezone")
	}
	return nil
}

func reminderRecurrenceAnchorDay(recurrenceType, triggerAt, timezone string) (int, error) {
	if recurrenceType != "monthly" {
		return 1, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, triggerAt)
	if err != nil {
		return 0, errors.New("trigger_at must be a valid RFC 3339 timestamp")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil || timezone == "Local" {
		return 0, errors.New("recurrence_timezone must be a stable IANA timezone")
	}
	return parsed.In(location).Day(), nil
}

func normalizeReminderCreate(input createReminderRequest, now time.Time) (normalizedReminderCreate, error) {
	title := strings.TrimSpace(input.Title)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 200 {
		return normalizedReminderCreate{}, errors.New("title must contain 2 to 200 characters")
	}
	summary := ""
	if input.Summary != nil {
		summary = strings.TrimSpace(*input.Summary)
	}
	if utf8.RuneCountInString(summary) > 10000 {
		return normalizedReminderCreate{}, errors.New("summary cannot exceed 10000 characters")
	}
	priority := "P2"
	if input.Priority != nil {
		priority = strings.TrimSpace(*input.Priority)
	}
	if _, valid := validPriorities[priority]; !valid {
		return normalizedReminderCreate{}, errors.New("priority must be P0, P1, P2, or P3")
	}
	triggerAt, err := normalizeInboxTimestamp(input.TriggerAt, "trigger_at")
	if err != nil {
		return normalizedReminderCreate{}, err
	}
	parsed, _ := time.Parse(time.RFC3339Nano, triggerAt)
	if !parsed.After(now) {
		return normalizedReminderCreate{}, errors.New("trigger_at must be later than server_now")
	}
	recurrenceType := "none"
	if input.RecurrenceType != nil {
		recurrenceType = strings.TrimSpace(*input.RecurrenceType)
	}
	recurrenceInterval := 1
	if input.RecurrenceInterval != nil {
		recurrenceInterval = *input.RecurrenceInterval
	}
	recurrenceTimezone := "UTC"
	if input.RecurrenceTimezone != nil {
		recurrenceTimezone = strings.TrimSpace(*input.RecurrenceTimezone)
	}
	recurrenceAnchorDay, err := reminderRecurrenceAnchorDay(recurrenceType, triggerAt, recurrenceTimezone)
	if err != nil {
		return normalizedReminderCreate{}, err
	}
	if err := validateReminderRecurrence(recurrenceType, recurrenceInterval, recurrenceTimezone, recurrenceAnchorDay); err != nil {
		return normalizedReminderCreate{}, err
	}
	return normalizedReminderCreate{
		Title: title, Summary: summary, Priority: priority, TriggerAt: triggerAt,
		RecurrenceType: recurrenceType, RecurrenceInterval: recurrenceInterval,
		RecurrenceTimezone: recurrenceTimezone, RecurrenceAnchorDay: recurrenceAnchorDay,
	}, nil
}

func normalizeReminderPatch(input updateReminderRequest, now time.Time) (map[string]any, error) {
	patch := make(map[string]any)
	if input.Title.Set {
		if input.Title.Value == nil {
			return nil, errors.New("title cannot be null")
		}
		value := strings.TrimSpace(*input.Title.Value)
		if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > 200 {
			return nil, errors.New("title must contain 2 to 200 characters")
		}
		patch["title"] = value
	}
	if input.Summary.Set {
		if input.Summary.Value == nil {
			return nil, errors.New("summary cannot be null")
		}
		value := strings.TrimSpace(*input.Summary.Value)
		if utf8.RuneCountInString(value) > 10000 {
			return nil, errors.New("summary cannot exceed 10000 characters")
		}
		patch["summary"] = value
	}
	if input.Priority.Set {
		if input.Priority.Value == nil {
			return nil, errors.New("priority cannot be null")
		}
		value := strings.TrimSpace(*input.Priority.Value)
		if _, valid := validPriorities[value]; !valid {
			return nil, errors.New("priority must be P0, P1, P2, or P3")
		}
		patch["priority"] = value
	}
	if input.TriggerAt.Set {
		if input.TriggerAt.Value == nil {
			return nil, errors.New("trigger_at cannot be null")
		}
		value, err := normalizeInboxTimestamp(*input.TriggerAt.Value, "trigger_at")
		if err != nil {
			return nil, err
		}
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		if !parsed.After(now) {
			return nil, errors.New("trigger_at must be later than server_now")
		}
		patch["trigger_at"] = value
	}
	if input.RecurrenceType.Set {
		if input.RecurrenceType.Value == nil {
			return nil, errors.New("recurrence_type cannot be null")
		}
		value := strings.TrimSpace(*input.RecurrenceType.Value)
		if value != "none" && value != "daily" && value != "weekly" && value != "monthly" {
			return nil, errors.New("recurrence_type must be none, daily, weekly, or monthly")
		}
		patch["recurrence_type"] = value
	}
	if input.RecurrenceInterval.Set {
		if input.RecurrenceInterval.Value == nil {
			return nil, errors.New("recurrence_interval cannot be null")
		}
		value := *input.RecurrenceInterval.Value
		if value < 1 || value > 365 {
			return nil, errors.New("recurrence_interval must be between 1 and 365")
		}
		patch["recurrence_interval"] = value
	}
	if input.RecurrenceTimezone.Set {
		if input.RecurrenceTimezone.Value == nil {
			return nil, errors.New("recurrence_timezone cannot be null")
		}
		value := strings.TrimSpace(*input.RecurrenceTimezone.Value)
		if len(value) < 1 || len(value) > 100 {
			return nil, errors.New("recurrence_timezone must contain 1 to 100 characters")
		}
		patch["recurrence_timezone"] = value
	}
	return patch, nil
}

func applyReminderPatch(reminder *models.Reminder, patch map[string]any) {
	if value, ok := patch["title"]; ok {
		reminder.Title = value.(string)
	}
	if value, ok := patch["summary"]; ok {
		reminder.Summary = value.(string)
	}
	if value, ok := patch["priority"]; ok {
		reminder.Priority = value.(string)
	}
	if value, ok := patch["trigger_at"]; ok {
		reminder.TriggerAt = value.(string)
	}
	if value, ok := patch["recurrence_type"]; ok {
		reminder.RecurrenceType = value.(string)
	}
	if value, ok := patch["recurrence_interval"]; ok {
		reminder.RecurrenceInterval = value.(int)
	}
	if value, ok := patch["recurrence_timezone"]; ok {
		reminder.RecurrenceTimezone = value.(string)
	}
}

func reminderPatchChangesAnchor(patch map[string]any) bool {
	for _, field := range []string{"trigger_at", "recurrence_type", "recurrence_timezone"} {
		if _, ok := patch[field]; ok {
			return true
		}
	}
	return false
}

func reminderEditableEqual(first, second models.Reminder) bool {
	return first.Title == second.Title && first.Summary == second.Summary &&
		first.Priority == second.Priority && first.TriggerAt == second.TriggerAt &&
		first.RecurrenceType == second.RecurrenceType &&
		first.RecurrenceInterval == second.RecurrenceInterval &&
		first.RecurrenceTimezone == second.RecurrenceTimezone &&
		first.RecurrenceAnchorDay == second.RecurrenceAnchorDay
}

func applyReminderSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.Order("CASE status WHEN 'scheduled' THEN 0 WHEN 'fired' THEN 1 ELSE 2 END ASC").
			Order("CASE WHEN status = 'scheduled' THEN trigger_at END ASC").
			Order("updated_at DESC"), true
	}
	allowed := map[string]string{
		"title": "title", "status": "status", "priority": "priority",
		"trigger_at": "trigger_at", "created_at": "created_at", "updated_at": "updated_at",
	}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		direction := "ASC"
		if strings.HasPrefix(field, "-") {
			direction = "DESC"
			field = strings.TrimPrefix(field, "-")
		}
		column, ok := allowed[field]
		if !ok {
			return query, false
		}
		query = query.Order(column + " " + direction)
	}
	return query, true
}

func reminderOutputFromModel(reminder models.Reminder) reminderOutput {
	reminder.TriggerAt = normalizeTimestamp(reminder.TriggerAt)
	reminder.CreatedAt = normalizeTimestamp(reminder.CreatedAt)
	reminder.UpdatedAt = normalizeTimestamp(reminder.UpdatedAt)
	for _, field := range []**string{&reminder.FiredAt, &reminder.CancelledAt} {
		if *field != nil {
			value := normalizeTimestamp(**field)
			*field = &value
		}
	}
	actions := []string{}
	if reminder.Status == "scheduled" {
		actions = []string{"edit", "cancel"}
	}
	return reminderOutput{
		ID: reminder.ID, SourceEntityType: reminder.SourceEntityType,
		SourceEntityID: reminder.SourceEntityID, Title: reminder.Title, Summary: reminder.Summary,
		Priority: reminder.Priority, TriggerAt: reminder.TriggerAt, Status: reminder.Status,
		SourceEventKey: reminder.SourceEventKey, CreatedByActorID: reminder.CreatedByActorID,
		SeriesID: reminder.SeriesID, RecurrenceType: reminder.RecurrenceType,
		RecurrenceInterval: reminder.RecurrenceInterval, RecurrenceTimezone: reminder.RecurrenceTimezone,
		OccurrenceNumber: reminder.OccurrenceNumber, RecurrenceAnchorDay: reminder.RecurrenceAnchorDay,
		FiredAt: reminder.FiredAt, InboxItemID: reminder.InboxItemID,
		CancelledByActorID: reminder.CancelledByActorID, CancelledAt: reminder.CancelledAt,
		CancelReason: reminder.CancelReason, Version: reminder.Version,
		CreatedAt: reminder.CreatedAt, UpdatedAt: reminder.UpdatedAt, AvailableActions: actions,
	}
}

func reminderEventState(reminder models.Reminder, reason string) map[string]any {
	state := map[string]any{
		"source_entity_type": reminder.SourceEntityType, "source_entity_id": reminder.SourceEntityID,
		"title": reminder.Title, "summary": reminder.Summary, "priority": reminder.Priority,
		"trigger_at": reminder.TriggerAt, "status": reminder.Status,
		"source_event_key": reminder.SourceEventKey, "created_by_actor_id": reminder.CreatedByActorID,
		"series_id": reminder.SeriesID, "recurrence_type": reminder.RecurrenceType,
		"recurrence_interval": reminder.RecurrenceInterval, "recurrence_timezone": reminder.RecurrenceTimezone,
		"occurrence_number": reminder.OccurrenceNumber, "recurrence_anchor_day": reminder.RecurrenceAnchorDay,
		"fired_at": reminder.FiredAt, "inbox_item_id": reminder.InboxItemID,
		"cancelled_by_actor_id": reminder.CancelledByActorID, "cancelled_at": reminder.CancelledAt,
		"cancel_reason": reminder.CancelReason, "version": reminder.Version,
	}
	if reason != "" {
		state["reason"] = reason
	}
	return state
}

func recordReminderWorkflowEvent(tx *gorm.DB, reminderIDValue, action string, previous, current map[string]any, actorID, requestID, createdAt string) error {
	var previousText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return fmt.Errorf("encode previous Reminder state: %w", err)
		}
		value := string(encoded)
		previousText = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("encode current Reminder state: %w", err)
	}
	currentText := string(encoded)
	actor := actorID
	var request *string
	if requestID != "" {
		request = &requestID
	}
	sequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "reminder", AggregateID: reminderIDValue,
		Action: action, ActorID: &actor, RequestID: request, CommandSeq: &sequence,
		PreviousJSON: previousText, CurrentJSON: &currentText, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record Reminder workflow event: %w", err)
	}
	return nil
}

func recordReminderInboxEvent(tx *gorm.DB, inbox models.InboxItem, createdAt string) error {
	currentBytes, err := json.Marshal(inboxItemEventState(inbox, ""))
	if err != nil {
		return fmt.Errorf("encode Reminder Inbox Item event: %w", err)
	}
	current := string(currentBytes)
	actor := models.BuiltinSystemActorID
	sequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "inbox_item", AggregateID: inbox.ID,
		Action: "created", ActorID: &actor, CommandSeq: &sequence,
		CurrentJSON: &current, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record Reminder Inbox Item workflow event: %w", err)
	}
	return nil
}

func reminderID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REMINDER_ID", "Reminder id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func loadReminder(db *gorm.DB, id string) (models.Reminder, error) {
	var reminder models.Reminder
	err := db.First(&reminder, "id = ?", id).Error
	return reminder, err
}

func reminderLoadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "REMINDER_NOT_FOUND", "Reminder not found")
	}
	return err
}

func reminderVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Reminder has changed; reload it before retrying")
}

func reminderTerminalConflict() error {
	return newProjectRequestError(http.StatusConflict, "REMINDER_NOT_SCHEDULED", "Only scheduled Reminders can be changed or cancelled")
}

func replayReminderSnapshot(tx *gorm.DB, key, endpoint, requestHash string, destination any) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	var existing models.IdempotencyKey
	err := tx.Where("key = ? AND endpoint = ?", key, endpoint).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
	}
	if *existing.RequestHash != requestHash {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different Reminder request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), destination); err != nil {
		return false, 0, fmt.Errorf("decode idempotent Reminder response: %w", err)
	}
	return true, *existing.ResponseStatus, nil
}

func recordReminderSnapshot(tx *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response any, createdAt string) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotent Reminder response: %w", err)
	}
	responseText := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID,
		RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &status,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&record).Error; err != nil {
		return fmt.Errorf("record Reminder idempotency key: %w", err)
	}
	return nil
}

func mapReminderConstraintError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "REMINDER_ACTOR_NOT_ACTIVE"):
		return newProjectRequestError(http.StatusConflict, "ACTOR_NOT_ACTIVE", "The Reminder creator is inactive")
	case strings.Contains(message, "REMINDER_TERMINAL_IMMUTABLE"):
		return reminderTerminalConflict()
	case strings.Contains(message, "REMINDER_INBOX_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "REMINDER_INBOX_MISMATCH", "The Reminder Inbox projection is inconsistent")
	case strings.Contains(strings.ToLower(message), "unique") && strings.Contains(strings.ToLower(message), "source_event_key"):
		return newProjectRequestError(http.StatusConflict, "REMINDER_EVENT_CONFLICT", "The Reminder event key is already in use")
	default:
		return err
	}
}
