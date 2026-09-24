package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Read-only validation shared by preview and the native/AI transaction commands.
func prepareSimpleFocusCommand(tx *gorm.DB, id string, expectedVersion int64, command string) (focusSessionRow, error) {
	row, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return row, focusSessionLoadError(err)
	}
	if row.Version != expectedVersion {
		return row, focusVersionConflict()
	}
	switch command {
	case "pause":
		if row.Status != "active" {
			return row, invalidFocusState("Only an active Focus Session can be paused")
		}
	case "resume":
		if row.Status != "paused" {
			return row, invalidFocusState("Only a paused Focus Session can be resumed")
		}
	default:
		return row, invalidFocusState("Unsupported Focus Session command")
	}
	return row, nil
}

// Caller owns the transaction, including any AI approval decision and audit.
func executeSimpleFocusInTransaction(tx *gorm.DB, id string, expectedVersion int64, command, requestID string, now time.Time) (focusSessionSnapshot, error) {
	now = now.UTC().Truncate(time.Second)
	row, err := prepareSimpleFocusCommand(tx, id, expectedVersion, command)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	timestamp := now.Format(time.RFC3339Nano)
	updates := map[string]any{"version": gorm.Expr("version + 1"), "updated_at": timestamp}
	switch command {
	case "pause":
		accumulated, err := closeOpenFocusInterval(tx, row.FocusSession, now)
		if err != nil {
			return focusSessionSnapshot{}, err
		}
		updates["status"] = "paused"
		updates["accumulated_seconds"] = accumulated
		updates["last_resumed_at"] = nil
		updates["last_heartbeat_at"] = timestamp
	case "resume":
		updates["status"] = "active"
		updates["last_resumed_at"] = timestamp
		updates["last_heartbeat_at"] = timestamp
	default:
		return focusSessionSnapshot{}, invalidFocusState("Unsupported Focus Session command")
	}
	result := tx.Model(&models.FocusSession{}).
		Where("id = ? AND version = ? AND status = ?", id, expectedVersion, row.Status).
		Updates(updates)
	if result.Error != nil {
		return focusSessionSnapshot{}, mapFocusConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return focusSessionSnapshot{}, focusVersionConflict()
	}
	if command == "resume" {
		if err := createOpenFocusInterval(tx, id, timestamp); err != nil {
			return focusSessionSnapshot{}, mapFocusConstraintError(err)
		}
	}
	updated, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	action := "focus_paused"
	if command == "resume" {
		action = "focus_resumed"
	}
	if err := recordFocusWorkflowEvent(
		tx, "focus_session", id, action,
		focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
		requestID, timestamp, 1,
	); err != nil {
		return focusSessionSnapshot{}, err
	}
	return focusSessionSnapshotFromRow(updated, now)
}

func prepareFocusStart(tx *gorm.DB, input createFocusSessionRequest) error {
	if input.PlannedSeconds < 300 || input.PlannedSeconds > 7200 {
		return newProjectRequestError(422, "VALIDATION_ERROR", "planned_seconds must be between 300 and 7200")
	}
	taskIDValue := input.TaskID
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

	return nil
}

func executeStartFocusInTransaction(tx *gorm.DB, input createFocusSessionRequest, requestID string, now time.Time) (focusSessionSnapshot, error) {
	now = now.UTC().Truncate(time.Second)
	if err := prepareFocusStart(tx, input); err != nil {
		return focusSessionSnapshot{}, err
	}
	taskIDValue := input.TaskID
	var snapshot focusSessionSnapshot
	timestamp := now.Format(time.RFC3339Nano)
	session := models.FocusSession{
		ID: uuid.NewString(), TaskID: taskIDValue, StartedAt: timestamp,
		Status: "active", PlannedSeconds: input.PlannedSeconds,
		AccumulatedSeconds: 0, LastResumedAt: &timestamp, LastHeartbeatAt: &timestamp,
		Version: 1, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	if err := tx.Create(&session).Error; err != nil {
		return focusSessionSnapshot{}, mapFocusConstraintError(err)
	}
	if err := createOpenFocusInterval(tx, session.ID, timestamp); err != nil {
		return focusSessionSnapshot{}, mapFocusConstraintError(err)
	}
	row, err := loadFocusSessionRow(tx, session.ID)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	snapshot, err = focusSessionSnapshotFromRow(row, now)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	if err := recordFocusWorkflowEvent(
		tx, "focus_session", session.ID, "focus_started", nil,
		focusSessionEventState(row.FocusSession), requestID, timestamp, 1,
	); err != nil {
		return focusSessionSnapshot{}, err
	}
	return snapshot, nil
}

func prepareFocusRecovery(tx *gorm.DB, id string, expectedVersion int64, action string) (focusSessionRow, error) {
	row, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return row, focusSessionLoadError(err)
	}
	if _, ok := validFocusRecoveryActions[action]; !ok {
		return row, newProjectRequestError(422, "VALIDATION_ERROR", "Unsupported recovery action")
	}
	if row.Version != expectedVersion {
		return row, focusVersionConflict()
	}
	if row.Status != "recovery_pending" {
		return row, invalidFocusState("Only a recovery_pending Focus Session can be recovered")
	}
	return row, nil
}

func executeRecoverFocusInTransaction(tx *gorm.DB, id string, expectedVersion int64, recoveryAction, requestID string, now time.Time) (focusSessionSnapshot, error) {
	now = now.UTC().Truncate(time.Second)
	row, err := prepareFocusRecovery(tx, id, expectedVersion, recoveryAction)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	var snapshot focusSessionSnapshot
	timestamp := now.Format(time.RFC3339Nano)
	updates := map[string]any{
		"version":    gorm.Expr("version + 1"),
		"updated_at": timestamp,
	}
	var accumulated int64
	switch recoveryAction {
	case "include_gap_resume":
		accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, now)
		if err != nil {
			return focusSessionSnapshot{}, err
		}
		updates["status"] = "active"
		updates["accumulated_seconds"] = accumulated
		updates["last_resumed_at"] = timestamp
		updates["last_heartbeat_at"] = timestamp
	case "exclude_gap_resume", "interrupt":
		cutoff, cutoffErr := focusHeartbeatCutoff(row.FocusSession, now)
		if cutoffErr != nil {
			return focusSessionSnapshot{}, cutoffErr
		}
		accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, cutoff)
		if err != nil {
			return focusSessionSnapshot{}, err
		}
		updates["accumulated_seconds"] = accumulated
		if recoveryAction == "exclude_gap_resume" {
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
		return focusSessionSnapshot{}, mapFocusConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return focusSessionSnapshot{}, focusVersionConflict()
	}
	if recoveryAction == "include_gap_resume" || recoveryAction == "exclude_gap_resume" {
		if err := createOpenFocusInterval(tx, id, timestamp); err != nil {
			return focusSessionSnapshot{}, mapFocusConstraintError(err)
		}
	}
	updated, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	action := "focus_resumed"
	if recoveryAction == "interrupt" {
		action = "focus_interrupted"
	}
	if err := recordFocusWorkflowEvent(
		tx, "focus_session", id, action,
		focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
		requestID, timestamp, 1,
	); err != nil {
		return focusSessionSnapshot{}, err
	}
	snapshot, err = focusSessionSnapshotFromRow(updated, now)
	return snapshot, err
}

func executeTerminalFocusInTransaction(tx *gorm.DB, id string, expectedVersion int64, command, requestID string, now time.Time) (focusSessionSnapshot, error) {
	now = now.UTC().Truncate(time.Second)
	if command != "stop" && command != "cancel" {
		return focusSessionSnapshot{}, invalidFocusState("Unsupported end command")
	}
	var snapshot focusSessionSnapshot
	row, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return focusSessionSnapshot{}, focusSessionLoadError(err)
	}
	terminal := row.Status == "completed" || row.Status == "cancelled" || row.Status == "interrupted"
	matchingTerminal := (command == "stop" && row.Status == "completed") || (command == "cancel" && row.Status == "cancelled")
	if terminal {
		if !matchingTerminal {
			return focusSessionSnapshot{}, invalidFocusState("The terminal Focus Session cannot run a different end command")
		}
		snapshot, err = focusSessionSnapshotFromRow(row, now)
		if err != nil {
			return focusSessionSnapshot{}, err
		}
		return snapshot, nil
	}
	if err := validateFocusEnd(row, expectedVersion); err != nil {
		return focusSessionSnapshot{}, err
	}
	accumulated := row.AccumulatedSeconds
	if row.Status == "active" {
		accumulated, err = closeOpenFocusInterval(tx, row.FocusSession, now)
		if err != nil {
			return focusSessionSnapshot{}, err
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
		return focusSessionSnapshot{}, mapFocusConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return focusSessionSnapshot{}, focusVersionConflict()
	}
	creditedMinutes := int64(0)
	if command == "stop" && row.TaskID != nil {
		creditedMinutes, err = creditFocusSecondsToTask(tx, *row.TaskID, accumulated, timestamp)
		if err != nil {
			return focusSessionSnapshot{}, err
		}
		if err := tx.Model(&models.FocusSession{}).
			Where("id = ?", id).
			Update("credited_minutes", creditedMinutes).Error; err != nil {
			return focusSessionSnapshot{}, err
		}
	}
	updated, err := loadFocusSessionRow(tx, id)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	focusAction := "focus_completed"
	if command == "cancel" {
		focusAction = "focus_cancelled"
	}
	if err := recordFocusWorkflowEvent(
		tx, "focus_session", id, focusAction,
		focusSessionEventState(row.FocusSession), focusSessionEventState(updated.FocusSession),
		requestID, timestamp, 1,
	); err != nil {
		return focusSessionSnapshot{}, err
	}
	if command == "stop" && row.TaskID != nil {
		if err := recordFocusWorkflowEvent(
			tx, "task", *row.TaskID, "task_actual_time_added", nil,
			map[string]any{
				"focus_session_id":    id,
				"exact_seconds_added": accumulated,
				"minutes_added":       creditedMinutes,
			},
			requestID, timestamp, 2,
		); err != nil {
			return focusSessionSnapshot{}, err
		}
	}
	snapshot, err = focusSessionSnapshotFromRow(updated, now)
	if err != nil {
		return focusSessionSnapshot{}, err
	}
	return snapshot, nil
}

func validateFocusEnd(row focusSessionRow, expectedVersion int64) error {
	if row.Version != expectedVersion {
		return focusVersionConflict()
	}
	if row.Status != "active" && row.Status != "paused" {
		return invalidFocusState("Only an active or paused Focus Session can run this end command")
	}
	return nil
}
