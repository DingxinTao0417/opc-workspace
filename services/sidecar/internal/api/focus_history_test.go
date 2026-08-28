package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func seedTerminalFocusSession(
	t *testing.T,
	store *database.Store,
	id string,
	taskID *string,
	status string,
	startedAt string,
	endedAt string,
	seconds int64,
) {
	t.Helper()
	endReason := "completed"
	if status == "cancelled" {
		endReason = "cancelled"
	}
	if status == "interrupted" {
		endReason = "crash_recovery"
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, task_id, started_at, ended_at, status, planned_seconds,
			accumulated_seconds, end_reason, credited_minutes, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 7200, ?, ?, 0, 1, ?, ?)
	`, id, taskID, startedAt, endedAt, status, seconds, endReason, startedAt, endedAt); err != nil {
		t.Fatalf("seed terminal Focus Session: %v", err)
	}
	if seconds > 0 {
		if _, err := store.SQL.Exec(`
			INSERT INTO focus_session_intervals(session_id, started_at, ended_at, duration_seconds, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, id, startedAt, endedAt, seconds, startedAt); err != nil {
			t.Fatalf("seed completed Focus interval: %v", err)
		}
	}
}

func TestListFocusSessionsFiltersAndPaginatesTerminalHistory(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	completedID := uuid.NewString()
	cancelledID := uuid.NewString()
	interruptedID := uuid.NewString()
	seedTerminalFocusSession(t, store, completedID, &taskID, "completed", "2026-03-08T08:00:00Z", "2026-03-08T08:25:00Z", 1500)
	seedTerminalFocusSession(t, store, cancelledID, nil, "cancelled", "2026-03-09T08:00:00Z", "2026-03-09T08:05:00Z", 300)
	seedTerminalFocusSession(t, store, interruptedID, nil, "interrupted", "2026-03-10T08:00:00Z", "2026-03-10T08:01:00Z", 60)

	page := performRequest(router, http.MethodGet, "/api/v1/focus-sessions?page=1&page_size=2", nil, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("list Focus history status=%d body=%s", page.Code, page.Body.String())
	}
	var response focusHistoryListResponse
	if err := json.Unmarshal(page.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode Focus history: %v", err)
	}
	if response.Meta.Total != 3 || response.Meta.Page != 1 || response.Meta.PageSize != 2 || len(response.Data) != 2 {
		t.Fatalf("Focus history response=%#v", response)
	}
	if response.Data[0].ID != interruptedID || response.Data[1].ID != cancelledID {
		t.Fatalf("Focus history order=%v want=%s,%s", []string{response.Data[0].ID, response.Data[1].ID}, interruptedID, cancelledID)
	}

	completed := performRequest(router, http.MethodGet, "/api/v1/focus-sessions?status=completed&task_id="+taskID, nil, nil)
	if completed.Code != http.StatusOK {
		t.Fatalf("filtered Focus history status=%d body=%s", completed.Code, completed.Body.String())
	}
	if err := json.Unmarshal(completed.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode filtered Focus history: %v", err)
	}
	if response.Meta.Total != 1 || len(response.Data) != 1 || response.Data[0].TaskTitle == nil {
		t.Fatalf("filtered Focus history=%#v", response)
	}

	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?status=active", nil, nil), http.StatusBadRequest, "INVALID_FOCUS_STATUS")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?task_id=nope", nil, nil), http.StatusBadRequest, "INVALID_TASK_ID")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?page_size=101", nil, nil), http.StatusBadRequest, "INVALID_PAGINATION")
}

func TestFocusPeriodStatsUsesLocalDaysCompletedIntervalsAndStreaks(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	cancelledID := uuid.NewString()
	// America/Los_Angeles: this interval crosses local midnight from Mar 7 to Mar 8.
	seedTerminalFocusSession(t, store, firstID, nil, "completed", "2026-03-08T07:50:00Z", "2026-03-08T08:10:00Z", 1200)
	seedTerminalFocusSession(t, store, secondID, nil, "completed", "2026-03-09T12:00:00Z", "2026-03-09T12:30:00Z", 1800)
	seedTerminalFocusSession(t, store, cancelledID, nil, "cancelled", "2026-03-10T12:00:00Z", "2026-03-10T12:20:00Z", 1200)

	response := performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-03-07&date_to=2026-03-10&timezone=America%2FLos_Angeles", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("Focus period stats status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data focusPeriodStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode Focus period stats: %v", err)
	}
	stats := envelope.Data
	if stats.Timezone != "America/Los_Angeles" || stats.DateFrom != "2026-03-07" || stats.DateTo != "2026-03-10" {
		t.Fatalf("Focus period identity=%#v", stats)
	}
	if len(stats.Days) != 4 || stats.Totals.Sessions != 2 || stats.Totals.Seconds != 3000 || stats.Totals.Minutes != 50 {
		t.Fatalf("Focus period totals=%#v days=%#v", stats.Totals, stats.Days)
	}
	wantSeconds := []int64{600, 600, 1800, 0}
	for index, day := range stats.Days {
		if day.Seconds != wantSeconds[index] {
			t.Fatalf("day %s seconds=%d want=%d", day.Date, day.Seconds, wantSeconds[index])
		}
	}
	if stats.CurrentStreakDays != 0 || stats.LongestStreakDays != 3 {
		t.Fatalf("Focus streaks current=%d longest=%d", stats.CurrentStreakDays, stats.LongestStreakDays)
	}
}

func TestFocusPeriodStatsDefaultsAndValidatesRange(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, _ := newFocusTestAPI(t, clock)

	response := performRequest(router, http.MethodGet, "/api/v1/stats/focus?timezone=UTC", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("default Focus period stats status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data focusPeriodStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode default Focus period stats: %v", err)
	}
	if envelope.Data.DateFrom != "2026-03-04" || envelope.Data.DateTo != "2026-03-10" || len(envelope.Data.Days) != 7 {
		t.Fatalf("default Focus range=%#v", envelope.Data)
	}

	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-03-01", nil, nil), http.StatusBadRequest, "INVALID_DATE_RANGE")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-03-10&date_to=2026-03-01", nil, nil), http.StatusBadRequest, "INVALID_DATE_RANGE")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-01-01&date_to=2026-04-04", nil, nil), http.StatusBadRequest, "DATE_RANGE_TOO_LARGE")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?timezone=Nope%2FNowhere", nil, nil), http.StatusBadRequest, "INVALID_TIMEZONE")
}
