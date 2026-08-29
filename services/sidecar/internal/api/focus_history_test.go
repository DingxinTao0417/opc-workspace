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

func seedFocusProject(t *testing.T, store *database.Store, id, name, status string) {
	t.Helper()
	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, status, created_at, updated_at)
		VALUES (?, ?, ?, '2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z')
	`, id, name, status); err != nil {
		t.Fatalf("seed Focus project: %v", err)
	}
}

func assignFocusTaskProject(t *testing.T, store *database.Store, taskID, projectID string) {
	t.Helper()
	if _, err := store.SQL.Exec("UPDATE tasks SET project_id = ? WHERE id = ?", projectID, taskID); err != nil {
		t.Fatalf("assign Focus task project: %v", err)
	}
}

func decodeFocusHistory(t *testing.T, body []byte) focusHistoryListResponse {
	t.Helper()
	var response focusHistoryListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Focus history: %v body=%s", err, body)
	}
	return response
}

func decodeFocusPeriodStats(t *testing.T, body []byte) focusPeriodStatsResponse {
	t.Helper()
	var envelope struct {
		Data focusPeriodStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Focus period stats: %v body=%s", err, body)
	}
	return envelope.Data
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

func TestFocusProjectFiltersValidateAndReadArchivedEmptyProject(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	archivedProjectID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	seedFocusProject(t, store, archivedProjectID, "Archived project", "archived")

	for _, endpoint := range []string{
		"/api/v1/focus-sessions?project_id=",
		"/api/v1/stats/focus?date_from=2026-03-09&date_to=2026-03-10&timezone=UTC&project_id=",
	} {
		assertAPIError(t, performRequest(router, http.MethodGet, endpoint+"not-a-uuid", nil, nil), http.StatusBadRequest, "INVALID_PROJECT_ID")
		assertAPIError(t, performRequest(router, http.MethodGet, endpoint+"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", nil, nil), http.StatusBadRequest, "INVALID_PROJECT_ID")
		assertAPIError(t, performRequest(router, http.MethodGet, endpoint+"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", nil, nil), http.StatusNotFound, "PROJECT_NOT_FOUND")
	}

	historyRecorder := performRequest(router, http.MethodGet, "/api/v1/focus-sessions?project_id="+archivedProjectID, nil, nil)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("archived empty project history status=%d body=%s", historyRecorder.Code, historyRecorder.Body.String())
	}
	history := decodeFocusHistory(t, historyRecorder.Body.Bytes())
	if history.Meta.Total != 0 || len(history.Data) != 0 {
		t.Fatalf("archived empty project history=%#v", history)
	}

	statsRecorder := performRequest(router, http.MethodGet, "/api/v1/stats/focus?project_id="+archivedProjectID+"&date_from=2026-03-09&date_to=2026-03-10&timezone=UTC", nil, nil)
	if statsRecorder.Code != http.StatusOK {
		t.Fatalf("archived empty project stats status=%d body=%s", statsRecorder.Code, statsRecorder.Body.String())
	}
	stats := decodeFocusPeriodStats(t, statsRecorder.Body.Bytes())
	if stats.Totals.Sessions != 0 || stats.Totals.Seconds != 0 || stats.Totals.Minutes != 0 ||
		stats.CurrentStreakDays != 0 || stats.LongestStreakDays != 0 || len(stats.Projects) != 0 || len(stats.Tags) != 0 {
		t.Fatalf("archived empty project totals=%#v projects=%#v tags=%#v", stats.Totals, stats.Projects, stats.Tags)
	}
	if len(stats.Days) != 2 || len(stats.Hours) != 24 || len(stats.Heatmap) != 7*24 {
		t.Fatalf("archived empty project zero-series lengths: days=%d hours=%d heatmap=%d", len(stats.Days), len(stats.Hours), len(stats.Heatmap))
	}
	for _, day := range stats.Days {
		if day.Sessions != 0 || day.Seconds != 0 || day.Minutes != 0 {
			t.Fatalf("non-zero empty project day=%#v", day)
		}
	}
	for _, hour := range stats.Hours {
		if hour.Sessions != 0 || hour.Seconds != 0 || hour.Minutes != 0 {
			t.Fatalf("non-zero empty project hour=%#v", hour)
		}
	}
	for _, cell := range stats.Heatmap {
		if cell.Sessions != 0 || cell.Seconds != 0 || cell.Minutes != 0 {
			t.Fatalf("non-zero empty project heatmap cell=%#v", cell)
		}
	}
}

func TestFocusProjectFiltersUseCurrentTaskProjectAndPreserveHistoryPagination(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	projectAID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
	projectBID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb2"
	seedFocusProject(t, store, projectAID, "Project A", "in_progress")
	seedFocusProject(t, store, projectBID, "Project B", "in_progress")

	taskAID := uuid.NewString()
	taskBID := uuid.NewString()
	unassignedTaskID := uuid.NewString()
	movedTaskID := uuid.NewString()
	deletedTaskID := uuid.NewString()
	for _, taskID := range []string{taskAID, taskBID, unassignedTaskID, movedTaskID, deletedTaskID} {
		seedFocusTask(t, store, taskID, "todo", 0)
	}
	assignFocusTaskProject(t, store, taskAID, projectAID)
	assignFocusTaskProject(t, store, taskBID, projectBID)
	assignFocusTaskProject(t, store, movedTaskID, projectAID)
	assignFocusTaskProject(t, store, deletedTaskID, projectAID)

	completedAID := "00000000-0000-4000-8000-000000000001"
	cancelledAID := "00000000-0000-4000-8000-000000000002"
	interruptedAID := "00000000-0000-4000-8000-000000000003"
	outsideRangeAID := "00000000-0000-4000-8000-000000000004"
	completedBID := "00000000-0000-4000-8000-000000000005"
	unassignedID := "00000000-0000-4000-8000-000000000006"
	movedID := "00000000-0000-4000-8000-000000000007"
	deletedTaskSessionID := "00000000-0000-4000-8000-000000000008"
	noTaskSessionID := "00000000-0000-4000-8000-000000000009"
	seedTerminalFocusSession(t, store, completedAID, &taskAID, "completed", "2026-03-08T23:50:00Z", "2026-03-09T00:10:00Z", 1200)
	seedTerminalFocusSession(t, store, cancelledAID, &taskAID, "cancelled", "2026-03-08T12:00:00Z", "2026-03-08T12:05:00Z", 300)
	seedTerminalFocusSession(t, store, interruptedAID, &taskAID, "interrupted", "2026-03-08T12:04:00Z", "2026-03-08T12:05:00Z", 60)
	seedTerminalFocusSession(t, store, outsideRangeAID, &taskAID, "completed", "2026-03-07T09:00:00Z", "2026-03-07T09:10:00Z", 600)
	seedTerminalFocusSession(t, store, completedBID, &taskBID, "completed", "2026-03-08T09:00:00Z", "2026-03-08T09:30:00Z", 1800)
	seedTerminalFocusSession(t, store, unassignedID, &unassignedTaskID, "completed", "2026-03-08T10:00:00Z", "2026-03-08T10:10:00Z", 600)
	seedTerminalFocusSession(t, store, movedID, &movedTaskID, "completed", "2026-03-08T10:00:00Z", "2026-03-08T10:15:00Z", 900)
	seedTerminalFocusSession(t, store, deletedTaskSessionID, &deletedTaskID, "completed", "2026-03-08T10:00:00Z", "2026-03-08T10:20:00Z", 1200)
	seedTerminalFocusSession(t, store, noTaskSessionID, nil, "completed", "2026-03-08T10:00:00Z", "2026-03-08T10:25:00Z", 1500)

	// Historical Sessions follow the Task's current project rather than copying
	// the project that the Task had when the Session was recorded.
	assignFocusTaskProject(t, store, movedTaskID, projectBID)
	if _, err := store.SQL.Exec("DELETE FROM tasks WHERE id = ?", deletedTaskID); err != nil {
		t.Fatalf("delete Focus history task: %v", err)
	}

	pageOneRecorder := performRequest(router, http.MethodGet, "/api/v1/focus-sessions?project_id="+projectAID+"&page=1&page_size=2", nil, nil)
	if pageOneRecorder.Code != http.StatusOK {
		t.Fatalf("Project A history page one status=%d body=%s", pageOneRecorder.Code, pageOneRecorder.Body.String())
	}
	pageOne := decodeFocusHistory(t, pageOneRecorder.Body.Bytes())
	if pageOne.Meta.Total != 4 || len(pageOne.Data) != 2 || pageOne.Data[0].ID != completedAID || pageOne.Data[1].ID != cancelledAID {
		t.Fatalf("Project A history page one=%#v", pageOne)
	}
	pageTwo := decodeFocusHistory(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?project_id="+projectAID+"&page=2&page_size=2", nil, nil).Body.Bytes())
	if pageTwo.Meta.Total != 4 || len(pageTwo.Data) != 2 || pageTwo.Data[0].ID != interruptedAID || pageTwo.Data[1].ID != outsideRangeAID {
		t.Fatalf("Project A history page two=%#v", pageTwo)
	}
	repeatedPageOne := decodeFocusHistory(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?project_id="+projectAID+"&page=1&page_size=2", nil, nil).Body.Bytes())
	if len(repeatedPageOne.Data) != 2 || repeatedPageOne.Data[0].ID != completedAID || repeatedPageOne.Data[1].ID != cancelledAID {
		t.Fatalf("Project A repeated history page=%#v", repeatedPageOne)
	}

	projectBHistory := decodeFocusHistory(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?project_id="+projectBID, nil, nil).Body.Bytes())
	if projectBHistory.Meta.Total != 2 || len(projectBHistory.Data) != 2 || projectBHistory.Data[0].ID != movedID || projectBHistory.Data[1].ID != completedBID {
		t.Fatalf("Project B history=%#v", projectBHistory)
	}
	unfilteredHistory := decodeFocusHistory(t, performRequest(router, http.MethodGet, "/api/v1/focus-sessions?page_size=20", nil, nil).Body.Bytes())
	if unfilteredHistory.Meta.Total != 9 {
		t.Fatalf("unfiltered Focus history total=%d want=9", unfilteredHistory.Meta.Total)
	}

	projectAStats := decodeFocusPeriodStats(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?project_id="+projectAID+"&date_from=2026-03-08&date_to=2026-03-09&timezone=UTC", nil, nil).Body.Bytes())
	if projectAStats.Totals.Sessions != 1 || projectAStats.Totals.Seconds != 1200 || projectAStats.Totals.Minutes != 20 || len(projectAStats.Days) != 2 ||
		projectAStats.Days[0].Seconds != 600 || projectAStats.Days[1].Seconds != 600 {
		t.Fatalf("Project A Focus stats totals=%#v days=%#v", projectAStats.Totals, projectAStats.Days)
	}
	if len(projectAStats.Projects) != 1 || projectAStats.Projects[0].ProjectID == nil || *projectAStats.Projects[0].ProjectID != projectAID || projectAStats.Projects[0].Seconds != 1200 {
		t.Fatalf("Project A Focus project distribution=%#v", projectAStats.Projects)
	}

	projectBStats := decodeFocusPeriodStats(t, performRequest(router, http.MethodGet, "/api/v1/stats/focus?project_id="+projectBID+"&date_from=2026-03-08&date_to=2026-03-09&timezone=UTC", nil, nil).Body.Bytes())
	if projectBStats.Totals.Sessions != 2 || projectBStats.Totals.Seconds != 2700 || projectBStats.Totals.Minutes != 45 {
		t.Fatalf("Project B Focus stats totals=%#v", projectBStats.Totals)
	}
}

func TestFocusPeriodStatsUsesLocalDaysCompletedIntervalsAndStreaks(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	projectID := uuid.NewString()
	taskID := uuid.NewString()
	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, status, created_at, updated_at)
		VALUES (?, 'Client delivery', 'in_progress', '2026-03-01T08:00:00Z', '2026-03-01T08:00:00Z')
	`, projectID); err != nil {
		t.Fatalf("seed Focus report project: %v", err)
	}
	seedFocusTask(t, store, taskID, "todo", 0)
	if _, err := store.SQL.Exec("UPDATE tasks SET project_id = ? WHERE id = ?", projectID, taskID); err != nil {
		t.Fatalf("assign Focus report task project: %v", err)
	}
	alphaTagID, betaTagID := uuid.NewString(), uuid.NewString()
	if _, err := store.SQL.Exec(`
		INSERT INTO tags(id, name, color, created_at) VALUES
			(?, 'Alpha', '#6C5CE7', '2026-03-01T08:00:00Z'),
			(?, 'Beta', '#00B894', '2026-03-01T08:00:00Z')
	`, alphaTagID, betaTagID); err != nil {
		t.Fatalf("seed Focus report tags: %v", err)
	}
	if _, err := store.SQL.Exec(
		"INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?), (?, ?)",
		taskID, alphaTagID, taskID, betaTagID,
	); err != nil {
		t.Fatalf("assign Focus report tags: %v", err)
	}
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	cancelledID := uuid.NewString()
	// America/Los_Angeles: this interval crosses local midnight from Mar 7 to Mar 8.
	seedTerminalFocusSession(t, store, firstID, &taskID, "completed", "2026-03-08T07:50:00Z", "2026-03-08T08:10:00Z", 1200)
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
	if len(stats.Projects) != 2 || stats.Projects[0].ProjectID != nil || stats.Projects[0].Seconds != 1800 ||
		stats.Projects[1].ProjectID == nil || *stats.Projects[1].ProjectID != projectID || stats.Projects[1].ProjectName == nil ||
		*stats.Projects[1].ProjectName != "Client delivery" || stats.Projects[1].Seconds != 1200 {
		t.Fatalf("Focus project distribution=%#v", stats.Projects)
	}
	if len(stats.Hours) != 24 || stats.Hours[0].Seconds != 600 || stats.Hours[5].Seconds != 1800 ||
		stats.Hours[23].Seconds != 600 || stats.Hours[5].Sessions != 1 {
		t.Fatalf("Focus hour distribution=%#v", stats.Hours)
	}
	if len(stats.Heatmap) != 7*24 || stats.Heatmap[(1-1)*24+5].Seconds != 1800 ||
		stats.Heatmap[(6-1)*24+23].Seconds != 600 || stats.Heatmap[(7-1)*24].Seconds != 600 {
		t.Fatalf("Focus heatmap=%#v", stats.Heatmap)
	}
	if len(stats.Tags) != 3 || stats.Tags[0].TagID != nil || stats.Tags[0].Seconds != 1800 ||
		stats.Tags[1].TagID == nil || *stats.Tags[1].TagID != alphaTagID || stats.Tags[1].TagName == nil ||
		*stats.Tags[1].TagName != "Alpha" || stats.Tags[1].TagColor == nil || *stats.Tags[1].TagColor != "#6C5CE7" ||
		stats.Tags[1].Seconds != 1200 || stats.Tags[2].TagID == nil || *stats.Tags[2].TagID != betaTagID ||
		stats.Tags[2].Seconds != 1200 {
		t.Fatalf("Focus tag distribution=%#v", stats.Tags)
	}

	projectResponse := performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-03-07&date_to=2026-03-10&timezone=America%2FLos_Angeles&project_id="+projectID, nil, nil)
	if projectResponse.Code != http.StatusOK {
		t.Fatalf("project Focus DST stats status=%d body=%s", projectResponse.Code, projectResponse.Body.String())
	}
	projectStats := decodeFocusPeriodStats(t, projectResponse.Body.Bytes())
	if projectStats.Totals.Sessions != 1 || projectStats.Totals.Seconds != 1200 || len(projectStats.Days) != 4 ||
		projectStats.Days[0].Seconds != 600 || projectStats.Days[1].Seconds != 600 ||
		projectStats.Days[2].Seconds != 0 || projectStats.Days[3].Seconds != 0 {
		t.Fatalf("project Focus DST totals=%#v days=%#v", projectStats.Totals, projectStats.Days)
	}
	if len(projectStats.Projects) != 1 || projectStats.Projects[0].ProjectID == nil ||
		*projectStats.Projects[0].ProjectID != projectID || projectStats.Projects[0].Seconds != 1200 {
		t.Fatalf("project Focus DST distribution=%#v", projectStats.Projects)
	}
}

func TestFocusPeriodHoursCombineRepeatedDSTHour(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	seedTerminalFocusSession(
		t, store, uuid.NewString(), nil, "completed",
		"2026-11-01T08:30:00Z", "2026-11-01T09:30:00Z", 3600,
	)

	response := performRequest(router, http.MethodGet, "/api/v1/stats/focus?date_from=2026-11-01&date_to=2026-11-01&timezone=America%2FLos_Angeles", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("Focus repeated-hour stats status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data focusPeriodStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode repeated-hour stats: %v", err)
	}
	if len(envelope.Data.Hours) != 24 || envelope.Data.Hours[1].Seconds != 3600 || envelope.Data.Hours[1].Sessions != 1 {
		t.Fatalf("repeated local hour distribution=%#v", envelope.Data.Hours)
	}
	repeatedCell := envelope.Data.Heatmap[(7-1)*24+1]
	if len(envelope.Data.Heatmap) != 7*24 || repeatedCell.Weekday != 7 || repeatedCell.Hour != 1 ||
		repeatedCell.Seconds != 3600 || repeatedCell.Sessions != 1 {
		t.Fatalf("repeated local hour heatmap=%#v", envelope.Data.Heatmap)
	}
	if len(envelope.Data.Tags) != 1 || envelope.Data.Tags[0].TagID != nil || envelope.Data.Tags[0].Seconds != 3600 {
		t.Fatalf("untagged repeated-hour distribution=%#v", envelope.Data.Tags)
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
