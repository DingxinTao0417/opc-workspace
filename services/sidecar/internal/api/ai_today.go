package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const aiTodayGuide = "- 今日概览用 workspace_today，必须提供用户确认的 IANA 时区；date 为用户指定的当地日期，省略才表示该时区的今天。任务 total/completed/remaining/estimated_minutes/actual_minutes 属于该 planned_date；overdue/due_soon 是全部活跃任务的全局风险，不是当天子集。Focus 只计当天重叠的 completed 区间，不含本地休息/轮次和正在运行的周期。概览不含任务明细；具体是哪项任务须用 workspace_tasks 查真实记录，不从计数猜测。只读统计不是执行授权；变更仍须独立提议和人工确认。"

func aiTodaySchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["timezone"],"properties":{"timezone":{"type":"string","maxLength":100,"description":"User-confirmed IANA time zone, e.g. Asia/Shanghai or UTC. Never infer from server UTC; ask if unknown."},"date":{"type":"string","format":"date","description":"Optional local YYYY-MM-DD. Omit only to use today in the confirmed time zone."}}}`)
}

// Today is a read model, not a bulk Task query or authority to act. It shares
// the native page calculation while keeping private Task/Focus rows local.
func (t *aiWorkspaceTool) today(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		Timezone string `json:"timezone"`
		Date     string `json:"date"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, err
	}
	for key, value := range fields {
		if string(value) == "null" || (key == "date" && input.Date == "") {
			return nil, errors.New("Today arguments cannot be null or empty")
		}
	}
	if input.Timezone == "" || input.Timezone != strings.TrimSpace(input.Timezone) || input.Timezone == "Local" || len(input.Timezone) > 100 {
		return nil, errors.New("an explicit user-confirmed IANA timezone is required")
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return nil, errors.New("timezone must be a valid IANA time zone")
	}
	observedAt := t.api.options.Now().UTC()
	date := input.Date
	if date == "" {
		date = observedAt.In(location).Format("2006-01-02")
	}
	if !validDate(date) {
		return nil, errors.New("date must use YYYY-MM-DD")
	}
	snapshot, err := readTodayStats(ctx, t.api.db, date, location, observedAt)
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return map[string]any{
		"server_now": observedAt.Format(time.RFC3339Nano),
		"timezone":   input.Timezone,
		"date":       snapshot.Date,
		"tasks":      snapshot.Tasks,
		"focus":      snapshot.Focus,
		"semantics": map[string]any{
			"task_counts_on_selected_planned_date":         true,
			"overdue_and_due_soon_across_all_active_tasks": true,
			"focus_completed_intervals_only":               true,
			"local_cycle_included":                         false,
			"task_details_included":                        false,
		},
	}, nil
}
