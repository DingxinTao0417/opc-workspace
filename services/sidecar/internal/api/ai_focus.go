package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func aiFocusSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view"],"properties":{"view":{"type":"string","enum":["active","session","history"]},"id":{"type":"string","format":"uuid","description":"Required only for session view"},"task_id":{"type":"string","format":"uuid","description":"Only history filter"},"status":{"type":"string","enum":["terminal","completed","cancelled","interrupted"],"description":"Only history; defaults to all terminal sessions"},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

// This does not call getActiveFocusSession: that HTTP read also writes a heartbeat.
// Focus facts are backend-owned; local break/round presentation is not included.
func (t *aiWorkspaceTool) focus(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		View   string `json:"view"`
		ID     string `json:"id"`
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Limit  *int   `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		if string(raw) == "null" {
			return nil, errors.New("Focus arguments cannot be null")
		}
		if key == "view" {
			continue
		}
		allowed := (input.View == "session" && key == "id") || (input.View == "history" && key != "id")
		if !allowed {
			return nil, errors.New("unexpected argument for Focus view")
		}
	}
	for _, id := range []string{input.ID, input.TaskID} {
		if id != "" {
			if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
				return nil, errors.New("Focus IDs must be canonical UUIDs")
			}
		}
	}
	if _, ok := fields["task_id"]; ok && input.TaskID == "" {
		return nil, errors.New("task_id cannot be empty")
	}
	now := t.api.focusNow()
	db := t.api.db.WithContext(ctx)
	result := map[string]any{"view": input.View, "route": "/focus", "server_now": now.Format(time.RFC3339Nano), "local_cycle_included": false}
	switch input.View {
	case "active", "session":
		var row focusSessionRow
		var err error
		if input.View == "active" {
			row, err = loadOpenFocusSession(db)
		} else {
			if input.ID == "" {
				return nil, errors.New("session view requires id")
			}
			row, err = loadFocusSessionRow(db, input.ID)
		}
		if input.View == "active" && errors.Is(err, gorm.ErrRecordNotFound) {
			result["session"] = nil
			return result, nil
		}
		if err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		result["session"], err = aiFocusRecord(row, now)
		if err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
	case "history":
		status := input.Status
		if status == "" {
			status = "terminal"
		}
		if status != "terminal" && status != "completed" && status != "cancelled" && status != "interrupted" {
			return nil, errors.New("history accepts terminal statuses only")
		}
		limit, err := workspacePaging(input.Limit, input.Offset)
		if err != nil {
			return nil, err
		}
		query := focusSessionRows(db)
		if status == "terminal" {
			query = query.Where("focus_sessions.status IN ?", []string{"completed", "cancelled", "interrupted"})
		} else {
			query = query.Where("focus_sessions.status = ?", status)
		}
		if input.TaskID != "" {
			query = query.Where("focus_sessions.task_id = ?", input.TaskID)
		}
		rows := []focusSessionRow{}
		if err := query.Order("focus_sessions.ended_at DESC, focus_sessions.id ASC").Limit(limit + 1).Offset(input.Offset).Scan(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			item, err := aiFocusRecord(row, now)
			if err != nil {
				return nil, safeAIWorkspaceError(ctx, err)
			}
			items = append(items, item)
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		result["items"], result["has_more"], result["next_offset"], result["window_limited"] = items, more, next, more && next == nil
	default:
		return nil, errors.New("view must be active, session or history")
	}
	return result, nil
}

func aiFocusRecord(row focusSessionRow, now time.Time) (map[string]any, error) {
	snapshot, err := focusSessionSnapshotFromRow(row, now)
	if err != nil {
		return nil, err
	}
	s := snapshot.Session
	return map[string]any{
		"id": s.ID, "version": s.Version, "status": s.Status, "task_id": s.TaskID, "task_title": s.TaskTitle,
		"planned_seconds": s.PlannedSeconds, "accumulated_seconds": s.AccumulatedSeconds,
		"elapsed_seconds": snapshot.ElapsedSeconds, "remaining_seconds": snapshot.RemainingSeconds,
		"started_at": s.StartedAt, "ended_at": s.EndedAt, "last_heartbeat_at": s.LastHeartbeatAt,
		"end_reason": s.EndReason, "credited_minutes": s.CreditedMinutes, "route": "/focus",
		"recovery_pending": s.Status == "recovery_pending",
	}, nil
}

func parseAIFocusAction(input aiWorkspaceAction, fields map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" || input.InboxItemID != "" || input.ReminderID != "" {
		return input, errors.New("Focus actions only accept focus_session_id as target")
	}
	if input.Action == "focus.start" {
		for key := range fields {
			if key != "task_id" && key != "expected_task_version" && key != "planned_seconds" {
				return input, errors.New("unexpected Focus start field: " + key)
			}
		}
		var start aiFocusStartChanges
		if err := decodeStrictToolArguments(input.Changes, &start); err != nil {
			return input, err
		}
		if _, present := fields["task_id"]; !present {
			return input, errors.New("task_id must be explicitly selected or null")
		}
		if start.PlannedSeconds < 300 || start.PlannedSeconds > 7200 {
			return input, errors.New("planned_seconds must be between 300 and 7200")
		}
		if start.TaskID != nil {
			if id, err := uuid.Parse(*start.TaskID); err != nil || id.String() != *start.TaskID || start.ExpectedTaskVersion < 1 {
				return input, errors.New("read the current task ID and version before starting Focus")
			}
		} else if _, present := fields["expected_task_version"]; present {
			return input, errors.New("unbound Focus does not accept a task version")
		}
		input.Changes, _ = json.Marshal(start)
		return input, nil
	}
	if input.Action != "focus.pause" && input.Action != "focus.resume" && input.Action != "focus.stop" && input.Action != "focus.cancel" && input.Action != "focus.recover" {
		return input, errors.New("unsupported Focus action")
	}
	if id, err := uuid.Parse(input.FocusSessionID); err != nil || id.String() != input.FocusSessionID || input.ExpectedVersion < 1 {
		return input, errors.New("read workspace_focus for the canonical focus_session_id and expected_version")
	}
	if input.Action == "focus.recover" {
		if len(fields) != 1 {
			return input, errors.New("Focus recover requires only recovery_action")
		}
		var action string
		if json.Unmarshal(fields["recovery_action"], &action) != nil {
			return input, errors.New("recovery_action is required")
		}
		if _, valid := validFocusRecoveryActions[action]; !valid {
			return input, errors.New("invalid recovery_action")
		}
		input.Changes, _ = json.Marshal(map[string]string{"recovery_action": action})
		return input, nil
	}
	if len(fields) != 0 {
		return input, errors.New("Focus pause/resume/stop/cancel requires empty changes")
	}
	input.Changes = json.RawMessage(`{}`)
	return input, nil
}

type aiFocusStartChanges struct {
	TaskID              *string `json:"task_id"`
	ExpectedTaskVersion int64   `json:"expected_task_version,omitempty"`
	PlannedSeconds      int64   `json:"planned_seconds"`
}

func aiFocusRecoveryAction(input aiWorkspaceAction) string {
	var changes struct {
		Action string `json:"recovery_action"`
	}
	_ = json.Unmarshal(input.Changes, &changes)
	return changes.Action
}

func previewAIFocusAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "focus.start" {
		var start aiFocusStartChanges
		_ = json.Unmarshal(input.Changes, &start)
		if err := prepareFocusStart(tx, createFocusSessionRequest{TaskID: start.TaskID, PlannedSeconds: start.PlannedSeconds}); err != nil {
			return preview, err
		}
		preview.Label = "未绑定任务的专注"
		var title *string
		var version *int64
		if start.TaskID != nil {
			task, err := loadTask(tx, *start.TaskID)
			if err != nil {
				return preview, err
			}
			if task.Version != start.ExpectedTaskVersion {
				return preview, taskVersionConflict()
			}
			title, version = &task.Title, &task.Version
			preview.Label = task.Title
		}
		preview.Before["status"] = "idle"
		preview.After = map[string]any{"status": "active", "task_id": start.TaskID, "task_title": title, "task_version": version, "planned_seconds": start.PlannedSeconds}
		return preview, nil
	}
	var row focusSessionRow
	var err error
	switch input.Action {
	case "focus.pause", "focus.resume":
		row, err = prepareSimpleFocusCommand(tx, input.FocusSessionID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "focus."))
	case "focus.recover":
		row, err = prepareFocusRecovery(tx, input.FocusSessionID, input.ExpectedVersion, aiFocusRecoveryAction(input))
	default:
		row, err = loadFocusSessionRow(tx, input.FocusSessionID)
		if err != nil {
			err = focusSessionLoadError(err)
		} else {
			err = validateFocusEnd(row, input.ExpectedVersion)
		}
	}
	if err != nil {
		return preview, err
	}
	preview.Label = "未绑定任务的专注"
	if row.TaskTitle != nil {
		preview.Label = *row.TaskTitle
	}
	target := map[string]string{"focus.pause": "paused", "focus.resume": "active", "focus.stop": "completed", "focus.cancel": "cancelled", "focus.recover": "active"}[input.Action]
	if input.Action == "focus.recover" && aiFocusRecoveryAction(input) == "interrupt" {
		target = "interrupted"
	}
	preview.Before["status"] = row.Status
	preview.After = map[string]any{"status": target, "task_id": row.TaskID, "task_title": row.TaskTitle, "planned_seconds": row.PlannedSeconds}
	if input.Action == "focus.recover" {
		preview.After["recovery_action"] = aiFocusRecoveryAction(input)
		preview.After["last_heartbeat_at"] = row.LastHeartbeatAt
		preview.After["accumulated_seconds"] = row.AccumulatedSeconds
	}
	// Never freeze elapsed time in consent: pause settles at human confirmation.
	return preview, nil
}

func executeAIFocusAction(tx *gorm.DB, input aiWorkspaceAction, requestID, timestamp string) (aiActionResult, error) {
	now, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return aiActionResult{}, err
	}
	var snapshot focusSessionSnapshot
	switch input.Action {
	case "focus.start":
		var start aiFocusStartChanges
		_ = json.Unmarshal(input.Changes, &start)
		snapshot, err = executeStartFocusInTransaction(tx, createFocusSessionRequest{TaskID: start.TaskID, PlannedSeconds: start.PlannedSeconds}, requestID, now)
	case "focus.stop", "focus.cancel":
		snapshot, err = executeTerminalFocusInTransaction(tx, input.FocusSessionID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "focus."), requestID, now)
	case "focus.recover":
		snapshot, err = executeRecoverFocusInTransaction(tx, input.FocusSessionID, input.ExpectedVersion, aiFocusRecoveryAction(input), requestID, now)
	default:
		snapshot, err = executeSimpleFocusInTransaction(tx, input.FocusSessionID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "focus."), requestID, now)
	}
	if err != nil {
		return aiActionResult{}, err
	}
	return aiActionResult{ID: snapshot.Session.ID, Version: snapshot.Session.Version}, nil
}
