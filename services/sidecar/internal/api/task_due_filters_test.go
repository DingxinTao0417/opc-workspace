package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newTaskDueFilterAPI(t *testing.T, now time.Time) (*gin.Engine, *database.Store) {
	return newTaskDueFilterAPIWithClock(t, func() time.Time { return now })
}

func newTaskDueFilterAPIWithClock(t *testing.T, now func() time.Time) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "task-due-filters.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: now,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store
}

func TestTaskDueStateFiltersReclassifyWhenTheServiceClockAdvances(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 0, 0, 250000000, time.UTC)
	router, store := newTaskDueFilterAPIWithClock(t, func() time.Time { return now })
	dueDate := now.Add(30 * time.Minute).Format(time.RFC3339Nano)
	createdAt := "2026-08-01T00:00:00Z"
	task := models.Task{
		ID: "00000000-0000-4000-8000-000000000009", Title: "Moves across the deadline", Description: "", Kind: "work",
		Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &dueDate,
		Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatalf("seed moving due-state task: %v", err)
	}

	readTotal := func(dueState string) int64 {
		t.Helper()
		recorder := performRequest(router, http.MethodGet, "/api/v1/tasks?due_state="+dueState, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list %s = %d: %s", dueState, recorder.Code, recorder.Body.String())
		}
		var result struct {
			Meta pageMeta `json:"meta"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode %s list: %v", dueState, err)
		}
		return result.Meta.Total
	}
	readStats := func() taskStats {
		t.Helper()
		recorder := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-08-29&timezone=UTC", nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("today stats = %d: %s", recorder.Code, recorder.Body.String())
		}
		var result struct {
			Data struct {
				Tasks taskStats `json:"tasks"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode today stats: %v", err)
		}
		return result.Data.Tasks
	}

	if got := readTotal("due_soon"); got != 1 {
		t.Fatalf("initial due-soon total = %d, want 1", got)
	}
	if got := readTotal("overdue"); got != 0 {
		t.Fatalf("initial overdue total = %d, want 0", got)
	}
	if got := readStats(); got.DueSoon != 1 || got.Overdue != 0 {
		t.Fatalf("initial stats = %#v, want dueSoon=1 overdue=0", got)
	}

	now = now.Add(31 * time.Minute)
	if got := readTotal("due_soon"); got != 0 {
		t.Fatalf("advanced due-soon total = %d, want 0", got)
	}
	if got := readTotal("overdue"); got != 1 {
		t.Fatalf("advanced overdue total = %d, want 1", got)
	}
	if got := readStats(); got.DueSoon != 0 || got.Overdue != 1 {
		t.Fatalf("advanced stats = %#v, want dueSoon=0 overdue=1", got)
	}
}

func TestTaskDueStateFiltersPreserveNanosecondPrecisionAndChronologicalSort(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 34, 56, 789123000, time.UTC)
	router, store := newTaskDueFilterAPI(t, now)
	createdAt := "2026-08-01T00:00:00Z"
	withinSameMillisecond := now.Add(-123 * time.Microsecond).Format(time.RFC3339Nano)
	wholeSecond := time.Date(2026, time.August, 29, 12, 34, 57, 0, time.UTC).Format(time.RFC3339Nano)
	hundredthSecond := time.Date(2026, time.August, 29, 12, 34, 57, 10_000_000, time.UTC).Format(time.RFC3339Nano)
	tenthSecond := time.Date(2026, time.August, 29, 12, 34, 57, 100_000_000, time.UTC).Format(time.RFC3339Nano)

	tasks := []models.Task{
		{
			ID: "00000000-0000-4000-8000-000000000010", Title: "Expired within the same millisecond", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &withinSameMillisecond,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000013", Title: "Whole-second deadline", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &wholeSecond,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000011", Title: "Hundredth-second deadline", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &hundredthSecond,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000012", Title: "Tenth-second deadline", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &tenthSecond,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
	}
	if err := store.DB.Create(&tasks).Error; err != nil {
		t.Fatalf("seed precision due-state tasks: %v", err)
	}

	type listEnvelope struct {
		Data []models.Task `json:"data"`
		Meta pageMeta      `json:"meta"`
	}
	readList := func(path string) listEnvelope {
		t.Helper()
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		var result listEnvelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode list %s: %v", path, err)
		}
		return result
	}

	overdue := readList("/api/v1/tasks?due_state=overdue&sort=due_date")
	if overdue.Meta.Total != 1 || len(overdue.Data) != 1 || overdue.Data[0].ID != tasks[0].ID {
		t.Fatalf("same-millisecond overdue result = %#v", overdue)
	}
	dueSoon := readList("/api/v1/tasks?due_state=due_soon&sort=due_date")
	wantDueSoonIDs := []string{tasks[1].ID, tasks[2].ID, tasks[3].ID}
	if dueSoon.Meta.Total != int64(len(wantDueSoonIDs)) || len(dueSoon.Data) != len(wantDueSoonIDs) {
		t.Fatalf("mixed-precision due-soon result = %#v", dueSoon)
	}
	for index, wantID := range wantDueSoonIDs {
		if dueSoon.Data[index].ID != wantID {
			t.Fatalf("chronological due-soon ID at %d = %s, want %s", index, dueSoon.Data[index].ID, wantID)
		}
	}

	statsRecorder := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-08-29&timezone=UTC", nil, nil)
	if statsRecorder.Code != http.StatusOK {
		t.Fatalf("today stats = %d: %s", statsRecorder.Code, statsRecorder.Body.String())
	}
	var stats struct {
		Data struct {
			Tasks taskStats `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode today stats: %v", err)
	}
	if stats.Data.Tasks.Overdue != 1 || stats.Data.Tasks.DueSoon != 3 {
		t.Fatalf("precision stats = %#v, want overdue=1 dueSoon=3", stats.Data.Tasks)
	}
}

func TestTaskDueStateFiltersMatchTodayStatsAndBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 34, 56, 789123000, time.UTC)
	router, store := newTaskDueFilterAPI(t, now)
	createdAt := "2026-08-01T00:00:00Z"
	plannedDate := "2026-08-29"
	justExpired := now.Truncate(time.Second).Format(time.RFC3339Nano)
	atNow := now.Format(time.RFC3339Nano)
	insideWindow := now.Add(time.Hour).Format(time.RFC3339Nano)
	atHorizon := now.Add(taskDueLeadTime).Format(time.RFC3339Nano)
	afterHorizon := now.Add(taskDueLeadTime + time.Second).Format(time.RFC3339Nano)
	completedAt := createdAt

	tasks := []models.Task{
		{
			ID: "00000000-0000-4000-8000-000000000001", Title: "Alpha just expired", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P1", DueDate: &justExpired, PlannedDate: &plannedDate,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000002", Title: "Alpha now boundary", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P1", DueDate: &atNow, PlannedDate: &plannedDate,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000003", Title: "Beta inside window", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P2", DueDate: &insideWindow,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000004", Title: "Alpha horizon boundary", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P1", DueDate: &atHorizon, PlannedDate: &plannedDate,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000005", Title: "Alpha after horizon", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P1", DueDate: &afterHorizon, PlannedDate: &plannedDate,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000006", Title: "Alpha without due date", Description: "", Kind: "work",
			Status: "todo", ReviewPolicy: "none", Priority: "P1", PlannedDate: &plannedDate,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000007", Title: "Done overdue task", Description: "", Kind: "work",
			Status: "done", ReviewPolicy: "none", Priority: "P1", DueDate: &justExpired, CompletedAt: &completedAt,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
		{
			ID: "00000000-0000-4000-8000-000000000008", Title: "Cancelled due soon task", Description: "", Kind: "work",
			Status: "cancelled", ReviewPolicy: "none", Priority: "P1", DueDate: &atNow,
			Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
		},
	}
	if err := store.DB.Create(&tasks).Error; err != nil {
		t.Fatalf("seed due-state tasks: %v", err)
	}

	type listEnvelope struct {
		Data []models.Task `json:"data"`
		Meta pageMeta      `json:"meta"`
	}
	readList := func(path string) listEnvelope {
		t.Helper()
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("list %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		var result listEnvelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode list %s: %v", path, err)
		}
		return result
	}

	overdue := readList("/api/v1/tasks?due_state=overdue&sort=due_date")
	if overdue.Meta.Total != 1 || len(overdue.Data) != 1 || overdue.Data[0].ID != tasks[0].ID {
		t.Fatalf("overdue result = %#v", overdue)
	}
	dueSoon := readList("/api/v1/tasks?due_state=due_soon&sort=due_date")
	wantDueSoonIDs := []string{tasks[1].ID, tasks[2].ID, tasks[3].ID}
	if dueSoon.Meta.Total != int64(len(wantDueSoonIDs)) || len(dueSoon.Data) != len(wantDueSoonIDs) {
		t.Fatalf("due-soon result = %#v", dueSoon)
	}
	for index, wantID := range wantDueSoonIDs {
		if dueSoon.Data[index].ID != wantID {
			t.Fatalf("due-soon ID at %d = %s, want %s", index, dueSoon.Data[index].ID, wantID)
		}
	}

	firstPage := readList("/api/v1/tasks?due_state=due_soon&status=active&page=1&page_size=2&sort=due_date")
	secondPage := readList("/api/v1/tasks?due_state=due_soon&status=active&page=2&page_size=2&sort=due_date")
	repeatedPage := readList("/api/v1/tasks?due_state=due_soon&status=active&page=1&page_size=2&sort=due_date")
	if firstPage.Meta.Total != 3 || len(firstPage.Data) != 2 || len(secondPage.Data) != 1 ||
		firstPage.Data[0].ID != repeatedPage.Data[0].ID || firstPage.Data[1].ID != repeatedPage.Data[1].ID ||
		secondPage.Data[0].ID != tasks[3].ID {
		t.Fatalf("unstable due-soon pagination: first=%#v second=%#v repeated=%#v", firstPage, secondPage, repeatedPage)
	}
	combined := readList("/api/v1/tasks?due_state=due_soon&status=active&priority=P1&planned_date=2026-08-29&q=Alpha&sort=due_date")
	if combined.Meta.Total != 2 || len(combined.Data) != 2 || combined.Data[0].ID != tasks[1].ID || combined.Data[1].ID != tasks[3].ID {
		t.Fatalf("combined due-state filters = %#v", combined)
	}

	statsRecorder := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-08-29&timezone=UTC", nil, nil)
	if statsRecorder.Code != http.StatusOK {
		t.Fatalf("today stats = %d: %s", statsRecorder.Code, statsRecorder.Body.String())
	}
	var stats struct {
		Data struct {
			Tasks taskStats `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode today stats: %v", err)
	}
	if stats.Data.Tasks.Overdue != int(overdue.Meta.Total) || stats.Data.Tasks.DueSoon != int(dueSoon.Meta.Total) {
		t.Fatalf("stats/list due facts diverged: stats=%#v overdue=%#v dueSoon=%#v", stats.Data.Tasks, overdue.Meta, dueSoon.Meta)
	}
}

func TestTaskDueStateFilterValidation(t *testing.T) {
	now := time.Date(2026, time.August, 29, 12, 34, 56, 789123000, time.UTC)
	router, _ := newTaskDueFilterAPI(t, now)
	for _, path := range []string{
		"/api/v1/tasks?due_state=",
		"/api/v1/tasks?due_state=later",
		"/api/v1/tasks?due_state=overdue&due_from=2026-08-29",
		"/api/v1/tasks?due_state=due_soon&due_to=2026-08-30",
		"/api/v1/tasks?due_state=overdue&due_from=",
		"/api/v1/tasks?due_state=due_soon&due_to=",
		"/api/v1/tasks?due_state=overdue&status=todo",
		"/api/v1/tasks?due_state=overdue&status=",
	} {
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusBadRequest || responseErrorCode(t, recorder.Body.Bytes()) != "INVALID_FILTER" {
			t.Fatalf("invalid due-state filter %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
	}
}
