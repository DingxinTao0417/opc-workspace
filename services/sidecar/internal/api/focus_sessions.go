package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createFocusSessionEndpoint = "POST /api/v1/focus-sessions"

var validFocusRecoveryActions = map[string]struct{}{
	"include_gap_resume": {},
	"exclude_gap_resume": {},
	"interrupt":          {},
}

func (a *API) focusNow() time.Time {
	return a.options.Now().UTC().Truncate(time.Second)
}

type createFocusSessionRequest struct {
	TaskID         *string `json:"task_id"`
	PlannedSeconds int64   `json:"planned_seconds"`
}

type recoverFocusSessionRequest struct {
	Action string `json:"action"`
}

type focusSessionRow struct {
	models.FocusSession `gorm:"embedded"`
	TaskTitle           *string `gorm:"column:task_title"`
}

type focusSessionOutput struct {
	models.FocusSession
	TaskTitle *string `json:"task_title"`
}

type focusSessionSnapshot struct {
	Session          *focusSessionOutput `json:"session"`
	ServerNow        string              `json:"server_now"`
	ElapsedSeconds   int64               `json:"elapsed_seconds"`
	RemainingSeconds int64               `json:"remaining_seconds"`
}

type createFocusSessionHash struct {
	TaskID         *string `json:"task_id"`
	PlannedSeconds int64   `json:"planned_seconds"`
}

type focusSessionCommandHash struct {
	ExpectedVersion int64  `json:"expected_version"`
	Action          string `json:"action,omitempty"`
}

func recoverFocusSessionsOnStartup(db *gorm.DB, now time.Time) error {
	timestamp := now.UTC().Format(time.RFC3339Nano)
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.FocusSession{}).
			Where("status = ?", "active").
			Updates(map[string]any{
				"status":     "recovery_pending",
				"version":    gorm.Expr("version + 1"),
				"updated_at": timestamp,
			}).Error
	})
}

func (a *API) refreshActiveFocusHeartbeat(ctx context.Context) error {
	return a.db.WithContext(ctx).Exec(
		"UPDATE focus_sessions SET last_heartbeat_at = ? WHERE status = 'active'",
		a.focusNow().Format(time.RFC3339Nano),
	).Error
}

func (a *API) getActiveFocusSession(c *gin.Context) {
	now := a.focusNow()
	var row focusSessionRow
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		current, err := loadOpenFocusSession(tx)
		if err != nil {
			return err
		}
		if current.Status == "active" {
			timestamp := now.Format(time.RFC3339Nano)
			result := tx.Exec(
				"UPDATE focus_sessions SET last_heartbeat_at = ? WHERE id = ? AND status = 'active' AND version = ?",
				timestamp, current.ID, current.Version,
			)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return focusVersionConflict()
			}
		}
		row, err = loadFocusSessionRow(tx, current.ID)
		return err
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"data": emptyFocusSessionSnapshot(now)})
			return
		}
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	snapshot, err := focusSessionSnapshotFromRow(row, now)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, row.Version)
	c.JSON(http.StatusOK, gin.H{"data": snapshot})
}

func (a *API) createFocusSession(c *gin.Context) {
	var input createFocusSessionRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	taskIDValue, err := cleanFocusTaskID(input.TaskID)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	if input.PlannedSeconds < 300 || input.PlannedSeconds > 7200 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "planned_seconds must be between 300 and 7200")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := focusRequestHash(createFocusSessionHash{TaskID: taskIDValue, PlannedSeconds: input.PlannedSeconds})
	if err != nil {
		writeDatabaseError(c)
		return
	}

	now := a.focusNow()
	statusCode := http.StatusCreated
	replayed := false
	var snapshot focusSessionSnapshot
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayFocusSessionSnapshot(tx, idempotencyKey, createFocusSessionEndpoint, requestHash, &snapshot)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		if err := validateFocusTask(tx, taskIDValue); err != nil {
			return err
		}
		var openCount int64
		if err := tx.Model(&models.FocusSession{}).
			Where("status IN ?", []string{"active", "paused", "recovery_pending"}).
			Count(&openCount).Error; err != nil {
			return err
		}
		if openCount > 0 {
			return activeFocusSessionConflict()
		}
		timestamp := now.Format(time.RFC3339Nano)
		session := models.FocusSession{
			ID: uuid.NewString(), TaskID: taskIDValue, StartedAt: timestamp,
			Status: "active", PlannedSeconds: input.PlannedSeconds,
			AccumulatedSeconds: 0, LastResumedAt: &timestamp, LastHeartbeatAt: &timestamp,
			Version: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
		}
		if err := tx.Create(&session).Error; err != nil {
			return mapFocusConstraintError(err)
		}
		if err := createOpenFocusInterval(tx, session.ID, timestamp); err != nil {
			return mapFocusConstraintError(err)
		}
		row, err := loadFocusSessionRow(tx, session.ID)
		if err != nil {
			return err
		}
		snapshot, err = focusSessionSnapshotFromRow(row, now)
		if err != nil {
			return err
		}
		if err := recordFocusWorkflowEvent(
			tx, "focus_session", session.ID, "focus_started", nil,
			focusSessionEventState(row.FocusSession), requestIDFromContext(c), timestamp, 1,
		); err != nil {
			return err
		}
		return recordFocusSessionSnapshot(tx, idempotencyKey, createFocusSessionEndpoint, session.ID, requestHash, statusCode, snapshot, timestamp)
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
	if snapshot.Session != nil {
		setProjectETag(c, snapshot.Session.Version)
	}
	c.JSON(statusCode, gin.H{"data": snapshot})
}

func (a *API) pauseFocusSession(c *gin.Context) {
	a.executeSimpleFocusSessionCommand(c, "pause")
}

func (a *API) resumeFocusSession(c *gin.Context) {
	a.executeSimpleFocusSessionCommand(c, "resume")
}

func (a *API) executeSimpleFocusSessionCommand(c *gin.Context, command string) {
	id, ok := focusSessionID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if !decodeEmptyFocusSessionCommand(c) {
		return
	}
	now := a.focusNow()
	var snapshot focusSessionSnapshot
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return focusSessionLoadError(err)
		}
		if row.Version != expectedVersion {
			return focusVersionConflict()
		}
		timestamp := now.Format(time.RFC3339Nano)
		updates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": timestamp}
		switch command {
		case "pause":
			if row.Status != "active" {
				return invalidFocusState("Only an active Focus Session can be paused")
			}
			accumulated, err := closeOpenFocusInterval(tx, row.FocusSession, now)
			if err != nil {
				return err
			}
			updates["status"] = "paused"
			updates["accumulated_seconds"] = accumulated
			updates["last_resumed_at"] = nil
			updates["last_heartbeat_at"] = timestamp
		case "resume":
			if row.Status != "paused" {
				return invalidFocusState("Only a paused Focus Session can be resumed")
			}
			updates["status"] = "active"
			updates["last_resumed_at"] = timestamp
			updates["last_heartbeat_at"] = timestamp
		default:
			return errors.New("unsupported Focus Session command")
		}
		result := tx.Model(&models.FocusSession{}).
			Where("id = ? AND version = ? AND status = ?", id, expectedVersion, row.Status).
			Updates(updates)
		if result.Error != nil {
			return mapFocusConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return focusVersionConflict()
		}
		if command == "resume" {
			if err := createOpenFocusInterval(tx, id, timestamp); err != nil {
				return mapFocusConstraintError(err)
			}
		}
		updated, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return err
		}
		action := "focus_paused"
		if command == "resume" {
			action = "focus_resumed"
		}
		if err := recordFocusWorkflowEvent(
			tx, "focus_session", id, action,
			focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
			requestIDFromContext(c), timestamp, 1,
		); err != nil {
			return err
		}
		snapshot, err = focusSessionSnapshotFromRow(updated, now)
		return err
	})
	writeFocusSessionCommandResponse(c, snapshot, err)
}

func (a *API) recoverFocusSession(c *gin.Context) {
	id, ok := focusSessionID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input recoverFocusSessionRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	input.Action = strings.TrimSpace(input.Action)
	if _, valid := validFocusRecoveryActions[input.Action]; !valid {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "action must be include_gap_resume, exclude_gap_resume, or interrupt")
		return
	}
	now := a.focusNow()
	var snapshot focusSessionSnapshot
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return focusSessionLoadError(err)
		}
		if row.Version != expectedVersion {
			return focusVersionConflict()
		}
		if row.Status != "recovery_pending" {
			return invalidFocusState("Only a recovery_pending Focus Session can be recovered")
		}
		timestamp := now.Format(time.RFC3339Nano)
		updates := map[string]any{
			"version":    gorm.Expr("version + 1"),
			"updated_at": timestamp,
		}
		var accumulated int64
		switch input.Action {
		case "include_gap_resume":
			accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, now)
			if err != nil {
				return err
			}
			updates["status"] = "active"
			updates["accumulated_seconds"] = accumulated
			updates["last_resumed_at"] = timestamp
			updates["last_heartbeat_at"] = timestamp
		case "exclude_gap_resume", "interrupt":
			cutoff, cutoffErr := focusHeartbeatCutoff(row.FocusSession, now)
			if cutoffErr != nil {
				return cutoffErr
			}
			accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, cutoff)
			if err != nil {
				return err
			}
			updates["accumulated_seconds"] = accumulated
			if input.Action == "exclude_gap_resume" {
				updates["status"] = "active"
				updates["last_resumed_at"] = timestamp
				updates["last_heartbeat_at"] = timestamp
			} else {
				updates["status"] = "interrupted"
				updates["ended_at"] = timestamp
				updates["end_reason"] = "crash_recovery"
				updates["last_resumed_at"] = nil
			}
		}
		result := tx.Model(&models.FocusSession{}).
			Where("id = ? AND version = ? AND status = 'recovery_pending'", id, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			return mapFocusConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return focusVersionConflict()
		}
		if input.Action == "include_gap_resume" || input.Action == "exclude_gap_resume" {
			if err := createOpenFocusInterval(tx, id, timestamp); err != nil {
				return mapFocusConstraintError(err)
			}
		}
		updated, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return err
		}
		action := "focus_resumed"
		if input.Action == "interrupt" {
			action = "focus_interrupted"
		}
		if err := recordFocusWorkflowEvent(
			tx, "focus_session", id, action,
			focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
			requestIDFromContext(c), timestamp, 1,
		); err != nil {
			return err
		}
		snapshot, err = focusSessionSnapshotFromRow(updated, now)
		return err
	})
	writeFocusSessionCommandResponse(c, snapshot, err)
}

func (a *API) stopFocusSession(c *gin.Context) {
	a.executeTerminalFocusSessionCommand(c, "stop")
}

func (a *API) cancelFocusSession(c *gin.Context) {
	a.executeTerminalFocusSessionCommand(c, "cancel")
}

func (a *API) executeTerminalFocusSessionCommand(c *gin.Context, command string) {
	id, ok := focusSessionID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if !decodeEmptyFocusSessionCommand(c) {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := focusRequestHash(focusSessionCommandHash{ExpectedVersion: expectedVersion, Action: command})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/focus-sessions/%s/%s", id, command)
	now := a.focusNow()
	statusCode := http.StatusOK
	replayed := false
	var snapshot focusSessionSnapshot
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		found, replayStatus, replayErr := replayFocusSessionSnapshot(tx, idempotencyKey, endpoint, requestHash, &snapshot)
		if replayErr != nil {
			return replayErr
		}
		if found {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		row, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return focusSessionLoadError(err)
		}
		terminal := row.Status == "completed" || row.Status == "cancelled" || row.Status == "interrupted"
		matchingTerminal := (command == "stop" && row.Status == "completed") || (command == "cancel" && row.Status == "cancelled")
		if terminal {
			if !matchingTerminal {
				return invalidFocusState("The terminal Focus Session cannot run a different end command")
			}
			snapshot, err = focusSessionSnapshotFromRow(row, now)
			if err != nil {
				return err
			}
			return recordFocusSessionSnapshot(tx, idempotencyKey, endpoint, id, requestHash, statusCode, snapshot, now.Format(time.RFC3339Nano))
		}
		if row.Version != expectedVersion {
			return focusVersionConflict()
		}
		if row.Status != "active" && row.Status != "paused" {
			return invalidFocusState("Only an active or paused Focus Session can run this end command")
		}
		accumulated := row.AccumulatedSeconds
		if row.Status == "active" {
			accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, now)
			if err != nil {
				return err
			}
		}
		timestamp := now.Format(time.RFC3339Nano)
		updates := map[string]any{
			"accumulated_seconds": accumulated,
			"ended_at":            timestamp,
			"last_resumed_at":     nil,
			"version":             gorm.Expr("version + 1"),
			"updated_at":          timestamp,
		}
		if row.Status == "active" {
			updates["last_heartbeat_at"] = timestamp
		}
		if command == "stop" {
			updates["status"] = "completed"
			if accumulated >= row.PlannedSeconds {
				updates["end_reason"] = "completed"
			} else {
				updates["end_reason"] = "user_stop"
			}
		} else {
			updates["status"] = "cancelled"
			updates["end_reason"] = "cancelled"
		}
		result := tx.Model(&models.FocusSession{}).
			Where("id = ? AND version = ? AND status = ?", id, expectedVersion, row.Status).
			Updates(updates)
		if result.Error != nil {
			return mapFocusConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return focusVersionConflict()
		}
		creditedMinutes := int64(0)
		if command == "stop" && row.TaskID != nil {
			creditedMinutes, err = creditFocusSecondsToTask(tx, *row.TaskID, accumulated, timestamp)
			if err != nil {
				return err
			}
			if err := tx.Model(&models.FocusSession{}).
				Where("id = ?", id).
				Update("credited_minutes", creditedMinutes).Error; err != nil {
				return err
			}
		}
		updated, err := loadFocusSessionRow(tx, id)
		if err != nil {
			return err
		}
		focusAction := "focus_completed"
		if command == "cancel" {
			focusAction = "focus_cancelled"
		}
		if err := recordFocusWorkflowEvent(
			tx, "focus_session", id, focusAction,
			focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
			requestIDFromContext(c), timestamp, 1,
		); err != nil {
			return err
		}
		if command == "stop" && row.TaskID != nil {
			if err := recordFocusWorkflowEvent(
				tx, "task", *row.TaskID, "task_actual_time_added", nil,
				map[string]any{
					"focus_session_id":    id,
					"exact_seconds_added": accumulated,
					"minutes_added":       creditedMinutes,
				},
				requestIDFromContext(c), timestamp, 2,
			); err != nil {
				return err
			}
		}
		snapshot, err = focusSessionSnapshotFromRow(updated, now)
		if err != nil {
			return err
		}
		return recordFocusSessionSnapshot(tx, idempotencyKey, endpoint, id, requestHash, statusCode, snapshot, timestamp)
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
	if snapshot.Session != nil {
		setProjectETag(c, snapshot.Session.Version)
	}
	c.JSON(statusCode, gin.H{"data": snapshot})
}

func writeFocusSessionCommandResponse(c *gin.Context, snapshot focusSessionSnapshot, err error) {
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if snapshot.Session != nil {
		setProjectETag(c, snapshot.Session.Version)
	}
	c.JSON(http.StatusOK, gin.H{"data": snapshot})
}

func decodeEmptyFocusSessionCommand(c *gin.Context) bool {
	if c.Request.Body == nil || c.Request.Body == http.NoBody || c.Request.ContentLength == 0 {
		return true
	}
	var input emptyTaskLifecycleRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return false
	}
	return true
}

func cleanFocusTaskID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, errors.New("task_id must be a UUID or null")
	}
	canonical := parsed.String()
	return &canonical, nil
}

func validateFocusTask(db *gorm.DB, taskIDValue *string) error {
	if taskIDValue == nil {
		return nil
	}
	var task struct {
		Status string `gorm:"column:status"`
	}
	if err := db.Table("tasks").Select("status").Where("id = ?", *taskIDValue).Take(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newProjectRequestError(http.StatusUnprocessableEntity, "TASK_NOT_FOUND", "task_id does not reference an existing task")
		}
		return err
	}
	if task.Status == "cancelled" {
		return newProjectRequestError(http.StatusConflict, "TASK_CANCELLED", "A cancelled task cannot start a Focus Session")
	}
	return nil
}

func focusSessionID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FOCUS_SESSION_ID", "Focus Session id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

const focusSessionSelectColumns = `
	focus_sessions.id,
	focus_sessions.task_id,
	focus_sessions.started_at,
	focus_sessions.ended_at,
	focus_sessions.status,
	focus_sessions.planned_seconds,
	focus_sessions.accumulated_seconds,
	focus_sessions.last_resumed_at,
	focus_sessions.last_heartbeat_at,
	focus_sessions.end_reason,
	focus_sessions.credited_minutes,
	focus_sessions.version,
	focus_sessions.created_at,
	focus_sessions.updated_at,
	tasks.title AS task_title
`

func focusSessionRows(db *gorm.DB) *gorm.DB {
	return db.Table("focus_sessions").
		Select(focusSessionSelectColumns).
		Joins("LEFT JOIN tasks ON tasks.id = focus_sessions.task_id")
}

func loadFocusSessionRow(db *gorm.DB, id string) (focusSessionRow, error) {
	var row focusSessionRow
	err := focusSessionRows(db).Where("focus_sessions.id = ?", id).Take(&row).Error
	return row, err
}

func loadOpenFocusSession(db *gorm.DB) (focusSessionRow, error) {
	var row focusSessionRow
	err := focusSessionRows(db).
		Where("focus_sessions.status IN ?", []string{"active", "paused", "recovery_pending"}).
		Order("julianday(focus_sessions.started_at) DESC").
		Order("focus_sessions.id DESC").
		Take(&row).Error
	return row, err
}

func focusSessionSnapshotFromRow(row focusSessionRow, now time.Time) (focusSessionSnapshot, error) {
	elapsed, err := displayedFocusSeconds(row.FocusSession, now)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	remaining := row.PlannedSeconds - elapsed
	if remaining < 0 {
		remaining = 0
	}
	output := focusSessionOutput{FocusSession: row.FocusSession, TaskTitle: row.TaskTitle}
	normalizeFocusSession(&output.FocusSession)
	return focusSessionSnapshot{
		Session: &output, ServerNow: now.UTC().Format(time.RFC3339Nano),
		ElapsedSeconds: elapsed, RemainingSeconds: remaining,
	}, nil
}

func emptyFocusSessionSnapshot(now time.Time) focusSessionSnapshot {
	return focusSessionSnapshot{Session: nil, ServerNow: now.UTC().Format(time.RFC3339Nano)}
}

func normalizeFocusSession(session *models.FocusSession) {
	session.StartedAt = normalizeTimestamp(session.StartedAt)
	session.CreatedAt = normalizeTimestamp(session.CreatedAt)
	session.UpdatedAt = normalizeTimestamp(session.UpdatedAt)
	for _, field := range []**string{&session.EndedAt, &session.LastResumedAt, &session.LastHeartbeatAt} {
		if *field != nil {
			normalized := normalizeTimestamp(**field)
			*field = &normalized
		}
	}
}

func displayedFocusSeconds(session models.FocusSession, now time.Time) (int64, error) {
	if session.Status != "active" {
		return boundedFocusSeconds(session.AccumulatedSeconds, session.PlannedSeconds), nil
	}
	return settleFocusSession(session, now)
}

func settleFocusSession(session models.FocusSession, through time.Time) (int64, error) {
	if session.LastResumedAt == nil {
		return 0, errors.New("active Focus Session is missing last_resumed_at")
	}
	resumedAt, err := parseFocusTimestamp(*session.LastResumedAt)
	if err != nil {
		return 0, fmt.Errorf("parse Focus Session last_resumed_at: %w", err)
	}
	return addFocusInterval(session.AccumulatedSeconds, session.PlannedSeconds, resumedAt, through), nil
}

func focusHeartbeatCutoff(session models.FocusSession, now time.Time) (time.Time, error) {
	if session.LastHeartbeatAt == nil {
		return time.Time{}, errors.New("recovery-pending Focus Session is missing last_heartbeat_at")
	}
	heartbeat, err := parseFocusTimestamp(*session.LastHeartbeatAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Focus Session last_heartbeat_at: %w", err)
	}
	if heartbeat.After(now) {
		return now, nil
	}
	return heartbeat, nil
}

func createOpenFocusInterval(tx *gorm.DB, sessionID, startedAt string) error {
	interval := models.FocusSessionInterval{
		SessionID: sessionID, StartedAt: startedAt, DurationSeconds: 0, CreatedAt: startedAt,
	}
	return tx.Create(&interval).Error
}

func closeOpenFocusInterval(tx *gorm.DB, session models.FocusSession, cutoff time.Time) (int64, error) {
	var interval models.FocusSessionInterval
	if err := tx.Where("session_id = ? AND ended_at IS NULL", session.ID).Take(&interval).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errors.New("open Focus Session interval is missing")
		}
		return 0, err
	}
	startedAt, err := parseFocusTimestamp(interval.StartedAt)
	if err != nil {
		return 0, fmt.Errorf("parse Focus Session interval start: %w", err)
	}
	accumulated := boundedFocusSeconds(session.AccumulatedSeconds, session.PlannedSeconds)
	remaining := session.PlannedSeconds - accumulated
	durationSeconds := int64(0)
	if cutoff.After(startedAt) && remaining > 0 {
		durationSeconds = int64(cutoff.Sub(startedAt) / time.Second)
		if durationSeconds > remaining {
			durationSeconds = remaining
		}
	}
	endedAt := startedAt.Add(time.Duration(durationSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	result := tx.Model(&models.FocusSessionInterval{}).
		Where("id = ? AND ended_at IS NULL", interval.ID).
		Updates(map[string]any{"ended_at": endedAt, "duration_seconds": durationSeconds})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, focusVersionConflict()
	}
	return accumulated + durationSeconds, nil
}

func creditFocusSecondsToTask(tx *gorm.DB, taskIDValue string, seconds int64, updatedAt string) (int64, error) {
	if err := tx.Exec(`
		INSERT INTO task_focus_totals(task_id, exact_seconds, applied_minutes, updated_at)
		VALUES (?, ?, 0, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			exact_seconds = task_focus_totals.exact_seconds + excluded.exact_seconds,
			updated_at = excluded.updated_at
	`, taskIDValue, seconds, updatedAt).Error; err != nil {
		return 0, err
	}
	var total models.TaskFocusTotal
	if err := tx.First(&total, "task_id = ?", taskIDValue).Error; err != nil {
		return 0, err
	}
	creditedMinutes := total.ExactSeconds/60 - total.AppliedMinutes
	if creditedMinutes < 0 {
		return 0, errors.New("Task Focus total applied minutes exceed exact seconds")
	}
	result := tx.Model(&models.Task{}).
		Where("id = ?", taskIDValue).
		Updates(map[string]any{
			"actual_minutes": gorm.Expr("actual_minutes + ?", creditedMinutes),
			"version":        gorm.Expr("version + 1"),
			"updated_at":     updatedAt,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, errors.New("Focus Session task disappeared during settlement")
	}
	if err := tx.Model(&models.TaskFocusTotal{}).
		Where("task_id = ?", taskIDValue).
		Updates(map[string]any{
			"applied_minutes": total.AppliedMinutes + creditedMinutes,
			"updated_at":      updatedAt,
		}).Error; err != nil {
		return 0, err
	}
	return creditedMinutes, nil
}

func focusSessionEventState(session models.FocusSession) map[string]any {
	return map[string]any{
		"task_id":             session.TaskID,
		"status":              session.Status,
		"planned_seconds":     session.PlannedSeconds,
		"accumulated_seconds": session.AccumulatedSeconds,
		"last_resumed_at":     session.LastResumedAt,
		"last_heartbeat_at":   session.LastHeartbeatAt,
		"ended_at":            session.EndedAt,
		"end_reason":          session.EndReason,
		"credited_minutes":    session.CreditedMinutes,
		"version":             session.Version,
	}
}

func recordFocusWorkflowEvent(
	tx *gorm.DB,
	aggregateType string,
	aggregateID string,
	action string,
	previous map[string]any,
	current map[string]any,
	requestID string,
	createdAt string,
	commandSequence int,
) error {
	var previousText *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		value := string(encoded)
		previousText = &value
	}
	var currentText *string
	if current != nil {
		encoded, err := json.Marshal(current)
		if err != nil {
			return err
		}
		value := string(encoded)
		currentText = &value
	}
	actorID := models.BuiltinOwnerActorID
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: aggregateType, AggregateID: aggregateID,
		Action: action, ActorID: &actorID, RequestID: &requestID, CommandSeq: &commandSequence,
		PreviousJSON: previousText, CurrentJSON: currentText, CreatedAt: createdAt,
	}
	return tx.Create(&event).Error
}

func addFocusInterval(accumulated, planned int64, startedAt, through time.Time) int64 {
	accumulated = boundedFocusSeconds(accumulated, planned)
	if accumulated >= planned || !through.After(startedAt) {
		return accumulated
	}
	remaining := planned - accumulated
	delta := through.Sub(startedAt)
	if delta >= time.Duration(remaining)*time.Second {
		return planned
	}
	return accumulated + int64(delta/time.Second)
}

func boundedFocusSeconds(value, planned int64) int64 {
	if value < 0 {
		return 0
	}
	if value > planned {
		return planned
	}
	return value
}

func parseFocusTimestamp(value string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", value)
}

func focusRequestHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("v1:%x", digest), nil
}

func replayFocusSessionSnapshot(
	tx *gorm.DB,
	key string,
	endpoint string,
	requestHash string,
	snapshot *focusSessionSnapshot,
) (bool, int, error) {
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
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different Focus Session request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), snapshot); err != nil {
		return false, 0, fmt.Errorf("decode idempotent Focus Session response: %w", err)
	}
	return true, *existing.ResponseStatus, nil
}

func recordFocusSessionSnapshot(
	tx *gorm.DB,
	key string,
	endpoint string,
	resourceID string,
	requestHash string,
	status int,
	snapshot focusSessionSnapshot,
	createdAt string,
) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	responseBody := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID,
		RequestHash: &requestHash, ResponseBody: &responseBody, ResponseStatus: &status,
		CreatedAt: createdAt,
	}
	return tx.Create(&record).Error
}

func focusSessionLoadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "FOCUS_SESSION_NOT_FOUND", "Focus Session not found")
	}
	return err
}

func focusVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Focus Session has changed; reload it before retrying")
}

func activeFocusSessionConflict() error {
	return newProjectRequestError(http.StatusConflict, "ACTIVE_FOCUS_SESSION_EXISTS", "Another active, paused, or recovery-pending Focus Session already exists")
}

func invalidFocusState(message string) error {
	return newProjectRequestError(http.StatusConflict, "INVALID_FOCUS_SESSION_STATE", message)
}

func mapFocusConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "idx_focus_sessions_single_open") ||
		strings.Contains(message, "UNIQUE constraint failed: index 'idx_focus_sessions_single_open'") {
		return activeFocusSessionConflict()
	}
	return err
}
