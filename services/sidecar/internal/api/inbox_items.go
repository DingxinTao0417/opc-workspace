package api

import (
	"crypto/sha256"
	"database/sql"
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

const (
	createInboxItemEndpoint = "POST /api/v1/inbox-items"
	readAllInboxEndpoint    = "POST /api/v1/inbox-items/read-all"
)

var validInboxViews = map[string]struct{}{
	"inbox": {}, "snoozed": {}, "archive": {},
}

type createInboxItemRequest struct {
	Kind             *string         `json:"kind"`
	Title            string          `json:"title"`
	Summary          *string         `json:"summary"`
	SourceEntityType *string         `json:"source_entity_type"`
	SourceEntityID   *string         `json:"source_entity_id"`
	SourceEventKey   *string         `json:"source_event_key"`
	Priority         *string         `json:"priority"`
	ResolutionPolicy *string         `json:"resolution_policy"`
	DueAt            *string         `json:"due_at"`
	PayloadJSON      json.RawMessage `json:"payload_json"`
}

type updateInboxItemRequest struct {
	Title    *string             `json:"title"`
	Summary  nullableStringPatch `json:"summary"`
	Priority *string             `json:"priority"`
	DueAt    nullableStringPatch `json:"due_at"`
}

type snoozeInboxItemRequest struct {
	SnoozedUntil string `json:"snoozed_until"`
}

type reasonInboxItemRequest struct {
	Reason string `json:"reason"`
}

type reopenInboxItemRequest struct {
	Reason *string `json:"reason"`
}

type readAllInboxItemsRequest struct {
	ThroughCreatedAt string `json:"through_created_at"`
}

type inboxItemOutput struct {
	ID                 string         `json:"id"`
	Kind               string         `json:"kind"`
	Title              string         `json:"title"`
	Summary            string         `json:"summary"`
	SourceEntityType   string         `json:"source_entity_type"`
	SourceEntityID     *string        `json:"source_entity_id"`
	SourceEventKey     *string        `json:"source_event_key"`
	SourceDeletedAt    *string        `json:"source_deleted_at"`
	Priority           string         `json:"priority"`
	Status             string         `json:"status"`
	ResolutionPolicy   string         `json:"resolution_policy"`
	DueAt              *string        `json:"due_at"`
	ReadAt             *string        `json:"read_at"`
	TriagedAt          *string        `json:"triaged_at"`
	SnoozedUntil       *string        `json:"snoozed_until"`
	ResolvedByActorID  *string        `json:"resolved_by_actor_id"`
	ResolvedAt         *string        `json:"resolved_at"`
	ResolutionReason   *string        `json:"resolution_reason"`
	ResolutionMode     *string        `json:"resolution_mode"`
	DismissedByActorID *string        `json:"dismissed_by_actor_id"`
	DismissedAt        *string        `json:"dismissed_at"`
	DismissReason      *string        `json:"dismiss_reason"`
	PayloadJSON        map[string]any `json:"payload_json"`
	Version            int64          `json:"version"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	AvailableActions   []string       `json:"available_actions"`
}

type inboxListMeta struct {
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Total       int64  `json:"total"`
	UnreadTotal int64  `json:"unread_total"`
	SnapshotAt  string `json:"snapshot_at"`
	ServerNow   string `json:"server_now"`
}

type readAllInboxItemsOutput struct {
	ThroughCreatedAt string `json:"through_created_at"`
	MarkedCount      int64  `json:"marked_count"`
}

type inboxWorkflowEventOutput struct {
	ID        string                  `json:"id"`
	Action    string                  `json:"action"`
	ActorID   *string                 `json:"actor_id"`
	Actor     *assignmentActorSummary `json:"actor"`
	RequestID *string                 `json:"request_id"`
	Previous  map[string]any          `json:"previous"`
	Current   map[string]any          `json:"current"`
	Reason    *string                 `json:"reason"`
	CreatedAt string                  `json:"created_at"`
}

type inboxWorkflowEventRow struct {
	ID             string  `gorm:"column:id"`
	Action         string  `gorm:"column:action"`
	ActorID        *string `gorm:"column:actor_id"`
	RequestID      *string `gorm:"column:request_id"`
	PreviousJSON   *string `gorm:"column:previous_json"`
	CurrentJSON    *string `gorm:"column:current_json"`
	CreatedAt      string  `gorm:"column:created_at"`
	ActorType      *string `gorm:"column:actor_type"`
	ActorName      *string `gorm:"column:actor_display_name"`
	ActorStatus    *string `gorm:"column:actor_status"`
	ActorIsBuiltin *bool   `gorm:"column:actor_is_builtin"`
	ActorVersion   *int64  `gorm:"column:actor_version"`
}

type inboxWorkflowEventMeta struct {
	Page             int   `json:"page"`
	PageSize         int   `json:"page_size"`
	Total            int64 `json:"total"`
	InboxItemVersion int64 `json:"inbox_item_version"`
}

type normalizedInboxCreate struct {
	Title       string
	Summary     string
	Priority    string
	DueAt       *string
	PayloadJSON string
}

type inboxCreateHash struct {
	Kind             string         `json:"kind"`
	Title            string         `json:"title"`
	Summary          string         `json:"summary"`
	SourceEntityType string         `json:"source_entity_type"`
	Priority         string         `json:"priority"`
	ResolutionPolicy string         `json:"resolution_policy"`
	DueAt            *string        `json:"due_at"`
	PayloadJSON      map[string]any `json:"payload_json"`
}

type inboxCommandHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	Command         string `json:"command"`
	SnoozedUntil    string `json:"snoozed_until,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

func (a *API) inboxNow() time.Time {
	return a.options.Now().UTC()
}

func (a *API) listInboxItems(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	view := strings.TrimSpace(c.DefaultQuery("view", "inbox"))
	if _, valid := validInboxViews[view]; !valid {
		writeError(c, http.StatusBadRequest, "INVALID_VIEW", "view must be inbox, snoozed, or archive")
		return
	}
	priority := strings.TrimSpace(c.Query("priority"))
	if priority != "" {
		if _, valid := validPriorities[priority]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "priority filter is invalid")
			return
		}
	}
	risk := strings.TrimSpace(c.Query("risk"))
	if risk != "" && risk != "tracking" && risk != "blocked" && risk != "waiting_review" {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "risk filter is invalid")
		return
	}
	search := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(search) > 200 {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "q cannot exceed 200 characters")
		return
	}

	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	var rows []models.InboxItem
	var total int64
	var unreadTotal int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		base := inboxListQuery(tx.Model(&models.InboxItem{}), view, nowText)
		if priority != "" {
			base = base.Where("priority = ?", priority)
		}
		switch risk {
		case "tracking":
			base = base.Where("status = 'tracking'")
		case "blocked", "waiting_review":
			base = base.Where(`EXISTS (
				SELECT 1 FROM inbox_item_tasks relation
				JOIN tasks task ON task.id = relation.task_id
				WHERE relation.inbox_item_id = inbox_items.id
				  AND relation.unlinked_at IS NULL
				  AND relation.is_required = 1
				  AND task.status = ?
			)`, risk)
		}
		if search != "" {
			like := "%" + escapeLike(search) + "%"
			base = base.Where("(title LIKE ? ESCAPE '\\' OR summary LIKE ? ESCAPE '\\')", like, like)
		}
		if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return err
		}
		unread := inboxListQuery(tx.Model(&models.InboxItem{}), "inbox", nowText).Where("read_at IS NULL")
		if err := unread.Count(&unreadTotal).Error; err != nil {
			return err
		}
		return base.
			Order("CASE priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END ASC").
			Order("CASE WHEN due_at IS NULL THEN 1 ELSE 0 END ASC").
			Order("due_at ASC").
			Order("created_at DESC").
			Order("id ASC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}

	data := make([]inboxItemOutput, len(rows))
	for index := range rows {
		output, err := inboxItemOutputFromModel(rows[index], now)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		data[index] = output
	}
	c.JSON(http.StatusOK, gin.H{
		"data": data,
		"meta": inboxListMeta{
			Page: page, PageSize: pageSize, Total: total, UnreadTotal: unreadTotal,
			SnapshotAt: nowText, ServerNow: nowText,
		},
	})
}

func inboxListQuery(query *gorm.DB, view, now string) *gorm.DB {
	query = query.Where("created_at <= ?", now)
	switch view {
	case "snoozed":
		return query.Where("status IN ('open', 'tracking') AND snoozed_until IS NOT NULL AND snoozed_until > ?", now)
	case "archive":
		return query.Where("status IN ('resolved', 'dismissed')")
	default:
		return query.Where("status IN ('open', 'tracking') AND (snoozed_until IS NULL OR snoozed_until <= ?)", now)
	}
}

func (a *API) createInboxItem(c *gin.Context) {
	var input createInboxItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	normalized, err := normalizeInboxCreate(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	payloadObject, err := decodeInboxPayload(normalized.PayloadJSON)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	requestHash, err := inboxRequestHash(inboxCreateHash{
		Kind: "manual", Title: normalized.Title, Summary: normalized.Summary,
		SourceEntityType: "manual", Priority: normalized.Priority,
		ResolutionPolicy: "manual", DueAt: normalized.DueAt, PayloadJSON: payloadObject,
	})
	if err != nil {
		writeDatabaseError(c)
		return
	}

	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	statusCode := http.StatusCreated
	replayed := false
	var response inboxItemOutput
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayInboxSnapshot(tx, idempotencyKey, createInboxItemEndpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "manual", Title: normalized.Title, Summary: normalized.Summary,
			SourceEntityType: "manual", Priority: normalized.Priority, Status: "open",
			ResolutionPolicy: "manual", DueAt: normalized.DueAt, PayloadJSON: normalized.PayloadJSON,
			Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("create Inbox Item: %w", err)
		}
		if err := recordInboxWorkflowEvent(tx, item.ID, "created", nil, inboxItemEventState(item, ""), requestIDFromContext(c), nowText); err != nil {
			return err
		}
		response, err = inboxItemOutputFromModel(item, now)
		if err != nil {
			return err
		}
		return recordInboxSnapshot(tx, idempotencyKey, createInboxItemEndpoint, item.ID, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
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

func (a *API) getInboxItem(c *gin.Context) {
	id, ok := inboxItemID(c)
	if !ok {
		return
	}
	var item models.InboxItem
	if err := a.db.WithContext(c.Request.Context()).First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "INBOX_ITEM_NOT_FOUND", "Inbox Item not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response, err := inboxItemOutputFromModel(item, a.inboxNow())
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateInboxItem(c *gin.Context) {
	id, ok := inboxItemID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateInboxItemRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	patch, err := normalizeInboxPatch(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	if !patch.hasChanges {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable field is required")
		return
	}

	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	var response inboxItemOutput
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		current, err := loadInboxItem(tx, id)
		if err != nil {
			return inboxItemLoadError(err)
		}
		if current.Version != expectedVersion {
			return inboxVersionConflict()
		}
		if inboxItemTerminal(current.Status) {
			return inboxTerminalConflict("Archived Inbox Items must be reopened before editing")
		}
		next := current
		patch.apply(&next)
		if current.SourceEntityType == systemMaintenanceInboxSourceType && !equalStringPointers(current.DueAt, next.DueAt) {
			return newProjectRequestError(
				http.StatusUnprocessableEntity,
				"VALIDATION_ERROR",
				"system maintenance Inbox Items cannot have a due date",
			)
		}
		if inboxItemEditableEqual(current, next) {
			response, err = inboxItemOutputFromModel(current, now)
			return err
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
			return result.Error
		}
		if result.RowsAffected == 0 {
			return inboxVersionConflict()
		}
		if err := recordInboxWorkflowEvent(tx, id, "updated", inboxItemEventState(current, ""), inboxItemEventState(next, ""), requestIDFromContext(c), nowText); err != nil {
			return err
		}
		response, err = inboxItemOutputFromModel(next, now)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, mapInboxItemConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) readInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "read")
}

func (a *API) snoozeInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "snooze")
}

func (a *API) unsnoozeInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "unsnooze")
}

func (a *API) resolveInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "resolve")
}

func (a *API) dismissInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "dismiss")
}

func (a *API) reopenInboxItem(c *gin.Context) {
	a.executeInboxCommand(c, "reopen")
}

func (a *API) executeInboxCommand(c *gin.Context, command string) {
	id, ok := inboxItemID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	commandInput, ok := decodeInboxCommand(c, command, expectedVersion)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := inboxRequestHash(commandInput)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/inbox-items/%s/%s", id, command)
	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	statusCode := http.StatusOK
	replayed := false
	var response inboxItemOutput
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayInboxSnapshot(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		if command == "snooze" {
			through, parseErr := time.Parse(time.RFC3339Nano, commandInput.SnoozedUntil)
			if parseErr != nil || !through.After(now) {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "snoozed_until must be later than server_now")
			}
		}
		current, loadErr := loadInboxItem(tx, id)
		if loadErr != nil {
			return inboxItemLoadError(loadErr)
		}
		if current.Version != expectedVersion {
			return inboxVersionConflict()
		}
		if command == "resolve" && current.ResolutionPolicy == "all_required_tasks_done" {
			progress, progressErr := loadInboxTaskProgress(tx, id)
			if progressErr != nil {
				return progressErr
			}
			if progress.RequiredTotal == 0 || !progress.AllRequiredDone {
				return newProjectRequestError(
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
				return err
			}
			reopenTracking = activeRelations > 0
		}
		next, changed, transitionErr := applyInboxCommand(current, commandInput, nowText, reopenTracking)
		if transitionErr != nil {
			return transitionErr
		}
		if changed {
			result := tx.Model(&models.InboxItem{}).
				Where("id = ? AND version = ?", id, expectedVersion).
				Updates(inboxCommandUpdates(next))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return inboxVersionConflict()
			}
			if err := recordInboxWorkflowEvent(
				tx, id, inboxCommandEventAction(command),
				inboxItemEventState(current, ""), inboxItemEventState(next, commandInput.Reason),
				requestIDFromContext(c), nowText,
			); err != nil {
				return err
			}
		}
		response, err = inboxItemOutputFromModel(next, now)
		if err != nil {
			return err
		}
		return recordInboxSnapshot(tx, idempotencyKey, endpoint, id, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
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

func (a *API) readAllInboxItems(c *gin.Context) {
	var input readAllInboxItemsRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	through, err := normalizeInboxTimestamp(input.ThroughCreatedAt, "through_created_at")
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := inboxRequestHash(readAllInboxItemsRequest{ThroughCreatedAt: through})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	now := a.inboxNow()
	nowText := formatInboxTimestamp(now)
	statusCode := http.StatusOK
	replayed := false
	response := readAllInboxItemsOutput{ThroughCreatedAt: through}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayInboxSnapshot(tx, idempotencyKey, readAllInboxEndpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		cutoff, parseErr := time.Parse(time.RFC3339Nano, through)
		if parseErr != nil || cutoff.After(now) {
			return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "through_created_at cannot be later than server_now")
		}
		var items []models.InboxItem
		if err := tx.Where(`
			read_at IS NULL
			AND status IN ('open', 'tracking')
			AND (snoozed_until IS NULL OR snoozed_until <= ?)
			AND created_at <= ?
			AND updated_at <= ?
		`, through, through, through).
			Order("created_at ASC").Order("id ASC").Find(&items).Error; err != nil {
			return err
		}
		for index := range items {
			current := items[index]
			next := current
			next.ReadAt = &nowText
			next.Version = current.Version + 1
			next.UpdatedAt = nowText
			result := tx.Model(&models.InboxItem{}).
				Where("id = ? AND version = ? AND read_at IS NULL", current.ID, current.Version).
				Updates(map[string]any{"read_at": nowText, "version": next.Version, "updated_at": nowText})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return inboxVersionConflict()
			}
			if err := recordInboxWorkflowEvent(tx, current.ID, "read", inboxItemEventState(current, ""), inboxItemEventState(next, ""), requestIDFromContext(c), nowText); err != nil {
				return err
			}
			response.MarkedCount++
		}
		return recordInboxSnapshot(tx, idempotencyKey, readAllInboxEndpoint, through, requestHash, statusCode, response, nowText)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) listInboxItemEvents(c *gin.Context) {
	id, ok := inboxItemID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	var item models.InboxItem
	var rows []inboxWorkflowEventRow
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		base := tx.Model(&models.WorkflowEvent{}).Where("aggregate_type = 'inbox_item' AND aggregate_id = ?", id)
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		return inboxWorkflowEventRowsQuery(tx).
			Where("event.aggregate_type = 'inbox_item' AND event.aggregate_id = ?", id).
			Order("event.created_at DESC").
			Order("COALESCE(event.command_seq, 0) DESC").
			Order("event.id DESC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "INBOX_ITEM_NOT_FOUND", "Inbox Item not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	events := make([]inboxWorkflowEventOutput, len(rows))
	for index := range rows {
		event, err := inboxWorkflowEventOutputFromRow(rows[index])
		if err != nil {
			writeDatabaseError(c)
			return
		}
		events[index] = event
	}
	setProjectETag(c, item.Version)
	c.JSON(http.StatusOK, gin.H{
		"data": events,
		"meta": inboxWorkflowEventMeta{Page: page, PageSize: pageSize, Total: total, InboxItemVersion: item.Version},
	})
}

type normalizedInboxPatch struct {
	title      *string
	summary    *string
	priority   *string
	dueAtSet   bool
	dueAt      *string
	hasChanges bool
}

func normalizeInboxPatch(input updateInboxItemRequest) (normalizedInboxPatch, error) {
	var result normalizedInboxPatch
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > 200 {
			return result, errors.New("title must contain between 2 and 200 characters")
		}
		result.title = &value
		result.hasChanges = true
	}
	if input.Summary.Set {
		value := ""
		if input.Summary.Value != nil {
			value = strings.TrimSpace(*input.Summary.Value)
		}
		if utf8.RuneCountInString(value) > 10000 {
			return result, errors.New("summary cannot exceed 10000 characters")
		}
		result.summary = &value
		result.hasChanges = true
	}
	if input.Priority != nil {
		value := strings.TrimSpace(*input.Priority)
		if _, valid := validPriorities[value]; !valid {
			return result, errors.New("priority must be P0, P1, P2, or P3")
		}
		result.priority = &value
		result.hasChanges = true
	}
	if input.DueAt.Set {
		result.dueAtSet = true
		result.hasChanges = true
		if input.DueAt.Value != nil {
			value, err := normalizeInboxTimestamp(*input.DueAt.Value, "due_at")
			if err != nil {
				return result, err
			}
			result.dueAt = &value
		}
	}
	return result, nil
}

func (patch normalizedInboxPatch) apply(item *models.InboxItem) {
	if patch.title != nil {
		item.Title = *patch.title
	}
	if patch.summary != nil {
		item.Summary = *patch.summary
	}
	if patch.priority != nil {
		item.Priority = *patch.priority
	}
	if patch.dueAtSet {
		item.DueAt = patch.dueAt
	}
}

func normalizeInboxCreate(input createInboxItemRequest) (normalizedInboxCreate, error) {
	if input.Kind != nil && strings.TrimSpace(*input.Kind) != "manual" {
		return normalizedInboxCreate{}, errors.New("kind must be manual in this version")
	}
	if input.SourceEntityType != nil && strings.TrimSpace(*input.SourceEntityType) != "manual" {
		return normalizedInboxCreate{}, errors.New("source_entity_type must be manual in this version")
	}
	if input.SourceEntityID != nil || input.SourceEventKey != nil {
		return normalizedInboxCreate{}, errors.New("manual Inbox Items cannot set source_entity_id or source_event_key")
	}
	if input.ResolutionPolicy != nil && strings.TrimSpace(*input.ResolutionPolicy) != "manual" {
		return normalizedInboxCreate{}, errors.New("resolution_policy must be manual in this version")
	}
	title := strings.TrimSpace(input.Title)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 200 {
		return normalizedInboxCreate{}, errors.New("title must contain between 2 and 200 characters")
	}
	summary := ""
	if input.Summary != nil {
		summary = strings.TrimSpace(*input.Summary)
	}
	if utf8.RuneCountInString(summary) > 10000 {
		return normalizedInboxCreate{}, errors.New("summary cannot exceed 10000 characters")
	}
	priority := "P2"
	if input.Priority != nil {
		priority = strings.TrimSpace(*input.Priority)
	}
	if _, valid := validPriorities[priority]; !valid {
		return normalizedInboxCreate{}, errors.New("priority must be P0, P1, P2, or P3")
	}
	var dueAt *string
	if input.DueAt != nil {
		normalized, err := normalizeInboxTimestamp(*input.DueAt, "due_at")
		if err != nil {
			return normalizedInboxCreate{}, err
		}
		dueAt = &normalized
	}
	payloadJSON := "{}"
	if len(input.PayloadJSON) > 0 {
		object, err := decodeInboxPayload(string(input.PayloadJSON))
		if err != nil {
			return normalizedInboxCreate{}, errors.New("payload_json must be a JSON object")
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return normalizedInboxCreate{}, errors.New("payload_json could not be encoded")
		}
		payloadJSON = string(encoded)
	}
	return normalizedInboxCreate{Title: title, Summary: summary, Priority: priority, DueAt: dueAt, PayloadJSON: payloadJSON}, nil
}

func decodeInboxCommand(c *gin.Context, command string, expectedVersion int64) (inboxCommandHash, bool) {
	result := inboxCommandHash{ExpectedVersion: expectedVersion, Command: command}
	switch command {
	case "read", "unsnooze":
		if !decodeEmptyFocusSessionCommand(c) {
			return inboxCommandHash{}, false
		}
	case "snooze":
		var input snoozeInboxItemRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return inboxCommandHash{}, false
		}
		normalized, err := normalizeInboxTimestamp(input.SnoozedUntil, "snoozed_until")
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return inboxCommandHash{}, false
		}
		result.SnoozedUntil = normalized
	case "resolve", "dismiss":
		var input reasonInboxItemRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return inboxCommandHash{}, false
		}
		reason, err := normalizeInboxReason(input.Reason, true)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return inboxCommandHash{}, false
		}
		result.Reason = reason
	case "reopen":
		if c.Request.Body == nil || c.Request.Body == http.NoBody || c.Request.ContentLength == 0 {
			return result, true
		}
		var input reopenInboxItemRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
			return inboxCommandHash{}, false
		}
		if input.Reason != nil {
			reason, err := normalizeInboxReason(*input.Reason, false)
			if err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
				return inboxCommandHash{}, false
			}
			result.Reason = reason
		}
	default:
		writeError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found")
		return inboxCommandHash{}, false
	}
	return result, true
}

func applyInboxCommand(current models.InboxItem, input inboxCommandHash, now string, reopenTracking bool) (models.InboxItem, bool, error) {
	next := current
	switch input.Command {
	case "read":
		if current.ReadAt != nil {
			return current, false, nil
		}
		next.ReadAt = &now
	case "snooze":
		if inboxItemTerminal(current.Status) {
			return current, false, inboxTerminalConflict("Archived Inbox Items cannot be snoozed")
		}
		if current.SnoozedUntil != nil && *current.SnoozedUntil == input.SnoozedUntil {
			return current, false, nil
		}
		next.SnoozedUntil = &input.SnoozedUntil
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
	case "unsnooze":
		if inboxItemTerminal(current.Status) {
			return current, false, inboxTerminalConflict("Archived Inbox Items cannot be unsnoozed")
		}
		if current.SnoozedUntil == nil {
			return current, false, nil
		}
		next.SnoozedUntil = nil
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
	case "resolve":
		if inboxItemTerminal(current.Status) {
			return current, false, inboxTerminalConflict("Only open or tracking Inbox Items can be resolved")
		}
		ownerID := models.BuiltinOwnerActorID
		mode := "manual"
		next.Status = "resolved"
		next.SnoozedUntil = nil
		next.ResolvedByActorID = &ownerID
		next.ResolvedAt = &now
		next.ResolutionReason = &input.Reason
		next.ResolutionMode = &mode
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
	case "dismiss":
		if inboxItemTerminal(current.Status) {
			return current, false, inboxTerminalConflict("Only open or tracking Inbox Items can be dismissed")
		}
		ownerID := models.BuiltinOwnerActorID
		next.Status = "dismissed"
		next.SnoozedUntil = nil
		next.DismissedByActorID = &ownerID
		next.DismissedAt = &now
		next.DismissReason = &input.Reason
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
	case "reopen":
		if !inboxItemTerminal(current.Status) {
			return current, false, newProjectRequestError(http.StatusConflict, "INBOX_ITEM_NOT_ARCHIVED", "Only resolved or dismissed Inbox Items can be reopened")
		}
		next.Status = "open"
		if reopenTracking {
			next.Status = "tracking"
		}
		next.SnoozedUntil = nil
		next.ResolvedByActorID = nil
		next.ResolvedAt = nil
		next.ResolutionReason = nil
		next.ResolutionMode = nil
		next.DismissedByActorID = nil
		next.DismissedAt = nil
		next.DismissReason = nil
	default:
		return current, false, errors.New("unsupported Inbox Item command")
	}
	next.Version = current.Version + 1
	next.UpdatedAt = now
	return next, true, nil
}

func inboxCommandUpdates(item models.InboxItem) map[string]any {
	return map[string]any{
		"status": item.Status, "read_at": item.ReadAt, "triaged_at": item.TriagedAt,
		"snoozed_until":        item.SnoozedUntil,
		"resolved_by_actor_id": item.ResolvedByActorID, "resolved_at": item.ResolvedAt,
		"resolution_reason": item.ResolutionReason, "resolution_mode": item.ResolutionMode,
		"dismissed_by_actor_id": item.DismissedByActorID, "dismissed_at": item.DismissedAt,
		"dismiss_reason": item.DismissReason, "version": item.Version, "updated_at": item.UpdatedAt,
	}
}

func inboxCommandEventAction(command string) string {
	switch command {
	case "snooze":
		return "snoozed"
	case "unsnooze":
		return "unsnoozed"
	case "resolve":
		return "resolved"
	case "dismiss":
		return "dismissed"
	case "reopen":
		return "reopened"
	default:
		return "read"
	}
}

func inboxItemOutputFromModel(item models.InboxItem, now time.Time) (inboxItemOutput, error) {
	payload, err := decodeInboxPayload(item.PayloadJSON)
	if err != nil {
		return inboxItemOutput{}, err
	}
	normalizeInboxItem(&item)
	return inboxItemOutput{
		ID: item.ID, Kind: item.Kind, Title: item.Title, Summary: item.Summary,
		SourceEntityType: item.SourceEntityType, SourceEntityID: item.SourceEntityID,
		SourceEventKey: item.SourceEventKey, SourceDeletedAt: item.SourceDeletedAt,
		Priority: item.Priority, Status: item.Status, ResolutionPolicy: item.ResolutionPolicy,
		DueAt: item.DueAt, ReadAt: item.ReadAt, TriagedAt: item.TriagedAt, SnoozedUntil: item.SnoozedUntil,
		ResolvedByActorID: item.ResolvedByActorID, ResolvedAt: item.ResolvedAt,
		ResolutionReason: item.ResolutionReason, ResolutionMode: item.ResolutionMode,
		DismissedByActorID: item.DismissedByActorID, DismissedAt: item.DismissedAt,
		DismissReason: item.DismissReason, PayloadJSON: payload, Version: item.Version,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		AvailableActions: inboxAvailableActions(item, now),
	}, nil
}

func normalizeInboxItem(item *models.InboxItem) {
	item.CreatedAt = normalizeTimestamp(item.CreatedAt)
	item.UpdatedAt = normalizeTimestamp(item.UpdatedAt)
	for _, field := range []**string{
		&item.SourceDeletedAt, &item.DueAt, &item.ReadAt, &item.TriagedAt, &item.SnoozedUntil,
		&item.ResolvedAt, &item.DismissedAt,
	} {
		if *field != nil {
			value := normalizeTimestamp(**field)
			*field = &value
		}
	}
}

func inboxAvailableActions(item models.InboxItem, now time.Time) []string {
	if inboxItemTerminal(item.Status) {
		if item.ReadAt == nil {
			return []string{"read", "reopen"}
		}
		return []string{"reopen"}
	}
	actions := []string{"edit"}
	if item.ReadAt == nil {
		actions = append(actions, "read")
	}
	snoozed := false
	if item.SnoozedUntil != nil {
		if value, err := time.Parse(time.RFC3339Nano, *item.SnoozedUntil); err == nil && value.After(now) {
			snoozed = true
		}
	}
	if snoozed {
		actions = append(actions, "unsnooze")
	} else {
		actions = append(actions, "snooze")
	}
	actions = append(actions, "resolve", "dismiss")
	if item.ResolutionPolicy == "all_required_tasks_done" {
		actions = append(actions, "force-resolve")
	}
	return actions
}

func inboxItemEventState(item models.InboxItem, reason string) map[string]any {
	state := map[string]any{
		"kind": item.Kind, "title": item.Title, "summary": item.Summary,
		"source_entity_type": item.SourceEntityType, "source_entity_id": item.SourceEntityID,
		"source_event_key": item.SourceEventKey, "source_deleted_at": item.SourceDeletedAt,
		"priority": item.Priority, "resolution_policy": item.ResolutionPolicy,
		"status": item.Status, "due_at": item.DueAt, "read_at": item.ReadAt,
		"triaged_at": item.TriagedAt, "snoozed_until": item.SnoozedUntil,
		"resolved_by_actor_id": item.ResolvedByActorID, "resolved_at": item.ResolvedAt,
		"resolution_reason": item.ResolutionReason, "resolution_mode": item.ResolutionMode,
		"dismissed_by_actor_id": item.DismissedByActorID, "dismissed_at": item.DismissedAt,
		"dismiss_reason": item.DismissReason, "payload_json": json.RawMessage(item.PayloadJSON),
		"version": item.Version,
	}
	if reason != "" {
		state["reason"] = reason
	}
	return state
}

func recordInboxWorkflowEvent(tx *gorm.DB, inboxItemIDValue, action string, previous, current map[string]any, requestID, createdAt string) error {
	return recordInboxWorkflowEventAs(
		tx, inboxItemIDValue, action, models.BuiltinOwnerActorID,
		previous, current, requestID, createdAt,
	)
}

func inboxWorkflowEventRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("workflow_events AS event").
		Select(`
			event.id,
			event.action,
			event.actor_id,
			event.request_id,
			event.previous_json,
			event.current_json,
			event.created_at,
			actor.type AS actor_type,
			actor.display_name AS actor_display_name,
			actor.status AS actor_status,
			actor.is_builtin AS actor_is_builtin,
			actor.version AS actor_version
		`).Joins("LEFT JOIN actors AS actor ON actor.id = event.actor_id")
}

func inboxWorkflowEventOutputFromRow(row inboxWorkflowEventRow) (inboxWorkflowEventOutput, error) {
	previous, err := decodeWorkflowEventObject(row.PreviousJSON)
	if err != nil {
		return inboxWorkflowEventOutput{}, err
	}
	current, err := decodeWorkflowEventObject(row.CurrentJSON)
	if err != nil {
		return inboxWorkflowEventOutput{}, err
	}
	var actor *assignmentActorSummary
	if row.ActorID != nil {
		if row.ActorType == nil || row.ActorName == nil || row.ActorStatus == nil || row.ActorIsBuiltin == nil || row.ActorVersion == nil {
			return inboxWorkflowEventOutput{}, errors.New("Inbox Item workflow event actor is missing")
		}
		actor = &assignmentActorSummary{
			ID: *row.ActorID, Type: *row.ActorType, DisplayName: *row.ActorName,
			Status: *row.ActorStatus, IsBuiltin: *row.ActorIsBuiltin, Version: *row.ActorVersion,
		}
	}
	return inboxWorkflowEventOutput{
		ID: row.ID, Action: row.Action, ActorID: row.ActorID, Actor: actor, RequestID: row.RequestID,
		Previous: previous, Current: current, Reason: taskWorkflowEventReason(current),
		CreatedAt: normalizeTimestamp(row.CreatedAt),
	}, nil
}

func replayInboxSnapshot(tx *gorm.DB, key, endpoint, requestHash string, destination any) (bool, int, error) {
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
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different Inbox request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), destination); err != nil {
		return false, 0, fmt.Errorf("decode idempotent Inbox response: %w", err)
	}
	return true, *existing.ResponseStatus, nil
}

func recordInboxSnapshot(tx *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response any, createdAt string) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode idempotent Inbox response: %w", err)
	}
	responseText := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID,
		RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &status,
		CreatedAt: createdAt,
	}
	if err := tx.Create(&record).Error; err != nil {
		return fmt.Errorf("record Inbox idempotency key: %w", err)
	}
	return nil
}

func inboxRequestHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("v1:%x", digest), nil
}

func decodeInboxPayload(value string) (map[string]any, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return nil, fmt.Errorf("decode Inbox Item payload: %w", err)
	}
	if object == nil {
		return nil, errors.New("Inbox Item payload must be an object")
	}
	return object, nil
}

func normalizeInboxTimestamp(value, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return "", fmt.Errorf("%s must be an RFC 3339 timestamp", field)
	}
	return formatInboxTimestamp(parsed), nil
}

func formatInboxTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func normalizeInboxReason(value string, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", errors.New("reason is required")
	}
	if utf8.RuneCountInString(trimmed) > 2000 {
		return "", errors.New("reason cannot exceed 2000 characters")
	}
	return trimmed, nil
}

func inboxItemID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_INBOX_ITEM_ID", "Inbox Item id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func loadInboxItem(db *gorm.DB, id string) (models.InboxItem, error) {
	var item models.InboxItem
	err := db.First(&item, "id = ?", id).Error
	return item, err
}

func inboxItemLoadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "INBOX_ITEM_NOT_FOUND", "Inbox Item not found")
	}
	return err
}

func inboxVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Inbox Item has changed; reload it before retrying")
}

func inboxTerminalConflict(message string) error {
	return newProjectRequestError(http.StatusConflict, "INBOX_ITEM_TERMINAL", message)
}

func inboxItemTerminal(status string) bool {
	return status == "resolved" || status == "dismissed"
}

func inboxItemEditableEqual(left, right models.InboxItem) bool {
	return left.Title == right.Title && left.Summary == right.Summary && left.Priority == right.Priority && equalStringPointers(left.DueAt, right.DueAt)
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mapInboxItemConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "SYSTEM_MAINTENANCE_INBOX_SOURCE_IMMUTABLE"):
		return newProjectRequestError(
			http.StatusConflict,
			"SYSTEM_MAINTENANCE_INBOX_SOURCE_IMMUTABLE",
			"System maintenance source facts cannot be changed",
		)
	case strings.Contains(message, "SYSTEM_MAINTENANCE_INBOX_SOURCE_DELETE_FORBIDDEN"):
		return newProjectRequestError(
			http.StatusConflict,
			"SYSTEM_MAINTENANCE_INBOX_SOURCE_DELETE_FORBIDDEN",
			"System maintenance Inbox sources cannot be marked deleted",
		)
	case strings.Contains(message, "INVALID_SYSTEM_MAINTENANCE_INBOX_SOURCE"):
		return newProjectRequestError(
			http.StatusUnprocessableEntity,
			"INVALID_SYSTEM_MAINTENANCE_INBOX_SOURCE",
			"System maintenance Inbox source is invalid",
		)
	default:
		return err
	}
}
