package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

func aiFocusReportSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view","date_from","date_to","timezone"],"properties":{"view":{"type":"string","enum":["summary","days","projects","tags","hours","heatmap"]},"date_from":{"type":"string","format":"date","description":"Explicit local YYYY-MM-DD, inclusive"},"date_to":{"type":"string","format":"date","description":"Explicit local YYYY-MM-DD, inclusive; at most 93 calendar days"},"timezone":{"type":"string","maxLength":100,"description":"User-confirmed IANA zone, e.g. Asia/Shanghai or UTC. Never infer from server_now; ask if unknown."},"project_id":{"type":"string","format":"uuid","description":"Optional current project filter; omit for all projects"},"limit":{"type":"integer","minimum":1,"maximum":20,"description":"Detail views only; defaults to 10"},"offset":{"type":"integer","minimum":0,"maximum":1000,"description":"Detail views only"}}}`)
}

func (t *aiWorkspaceTool) focusReport(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		View      string  `json:"view"`
		DateFrom  string  `json:"date_from"`
		DateTo    string  `json:"date_to"`
		Timezone  string  `json:"timezone"`
		ProjectID *string `json:"project_id"`
		Limit     *int    `json:"limit"`
		Offset    int     `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		if strings.TrimSpace(string(raw)) == "null" {
			return nil, errors.New("Focus report arguments cannot be null")
		}
		if input.View == "summary" && (key == "limit" || key == "offset") {
			return nil, errors.New("summary has no pagination; select a detail view")
		}
	}
	switch input.View {
	case "summary", "days", "projects", "tags", "hours", "heatmap":
	default:
		return nil, errors.New("view must be summary, days, projects, tags, hours or heatmap")
	}
	if input.DateFrom == "" || input.DateTo == "" {
		return nil, errors.New("explicit date_from and date_to are required; ask for the user's local date range")
	}
	if input.Timezone == "" || input.Timezone == "Local" || len(input.Timezone) > 100 {
		return nil, errors.New("an explicit user-confirmed IANA timezone is required")
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return nil, errors.New("timezone must be a valid IANA time zone")
	}
	if input.ProjectID != nil {
		if id, err := uuid.Parse(*input.ProjectID); err != nil || id.String() != *input.ProjectID {
			return nil, errors.New("project_id must be a canonical UUID; omit for all projects")
		}
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	observedAt := t.api.focusNow()
	report, err := readFocusPeriodStats(ctx, t.api.db, input.DateFrom, input.DateTo, location, input.ProjectID)
	if err != nil {
		var validation *projectRequestError
		if errors.As(err, &validation) {
			return nil, validation
		}
		return nil, safeAIWorkspaceError(ctx, err)
	}
	query := url.Values{"report": {input.View}, "date_from": {report.DateFrom}, "date_to": {report.DateTo}, "timezone": {report.Timezone}}
	if input.ProjectID != nil {
		query.Set("project_id", *input.ProjectID)
	}
	result := map[string]any{
		"view": input.View, "route": "/focus?" + query.Encode(), "server_now": observedAt.Format(time.RFC3339Nano),
		"date_from": report.DateFrom, "date_to": report.DateTo, "timezone": report.Timezone,
		"project_id": input.ProjectID, "totals": report.Totals,
		"current_streak_days": report.CurrentStreakDays, "longest_streak_days": report.LongestStreakDays,
		"semantics": map[string]any{
			"completed_only": true, "local_cycle_included": false,
			"project_and_tag_attribution": "current_task_relationships_not_historical",
			"tag_seconds_additive":        false, "bucket_session_counts_additive": false,
			"minutes_rounding": "floor_seconds_per_bucket", "streaks_within_requested_range": true,
			"pagination": "live_snapshot_per_request_not_frozen_across_pages",
		},
	}
	// Only the requested dimension leaves the process. No task body, actor details,
	// credentials or raw interval/SQL payload is exposed by this aggregate tool.
	switch input.View {
	case "days":
		aiFocusReportPage(result, report.Days, limit, input.Offset, "date_ascending")
	case "projects":
		aiFocusReportPage(result, report.Projects, limit, input.Offset, "seconds_desc_name_asc_id_asc")
	case "tags":
		aiFocusReportPage(result, report.Tags, limit, input.Offset, "seconds_desc_name_asc_id_asc")
	case "hours":
		aiFocusReportPage(result, report.Hours, limit, input.Offset, "hour_0_to_23")
	case "heatmap":
		aiFocusReportPage(result, report.Heatmap, limit, input.Offset, "weekday_monday_1_to_sunday_7_then_hour")
	}
	return result, nil
}

func aiFocusReportPage[T any](result map[string]any, items []T, limit, offset int, order string) {
	start := min(offset, len(items))
	end := min(start+limit, len(items))
	more := end < len(items)
	var next *int
	if more && end <= 1000 {
		next = &end
	}
	result["items"], result["total_items"], result["offset"], result["limit"] = items[start:end], len(items), offset, limit
	result["has_more"], result["next_offset"], result["window_limited"], result["order"] = more, next, more && next == nil, order
}
