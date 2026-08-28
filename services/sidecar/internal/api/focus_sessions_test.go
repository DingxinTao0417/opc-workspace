package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

type focusTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (clock *focusTestClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func (clock *focusTestClock) Set(value time.Time) {
	clock.mu.Lock()
	clock.now = value
	clock.mu.Unlock()
}

func (clock *focusTestClock) Add(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func newFocusTestAPI(t *testing.T, clock *focusTestClock) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "focus-api.db"))
	if err != nil {
		t.Fatalf("database.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: clock.Now,
		FocusHeartbeatInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store
}

func seedFocusTask(t *testing.T, store *database.Store, id, status string, actualMinutes int) {
	t.Helper()
	now := "2026-03-01T08:00:00Z"
	if _, err := store.SQL.Exec(`
		INSERT INTO tasks(id, title, status, priority, actual_minutes, created_at, updated_at)
		VALUES (?, ?, ?, 'P2', ?, ?, ?)
	`, id, "Focus Task "+id[len(id)-4:], status, actualMinutes, now, now); err != nil {
		t.Fatalf("seed Focus task: %v", err)
	}
}

func decodeFocusSnapshot(t *testing.T, body []byte) focusSessionSnapshot {
	t.Helper()
	var envelope struct {
		Data focusSessionSnapshot `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Focus response: %v body=%s", err, body)
	}
	return envelope.Data
}

func createFocusForTest(t *testing.T, router http.Handler, taskID *string, planned int64, key string) focusSessionSnapshot {
	t.Helper()
	body, err := json.Marshal(createFocusSessionRequest{TaskID: taskID, PlannedSeconds: planned})
	if err != nil {
		t.Fatalf("encode create Focus request: %v", err)
	}
	recorder := performRequest(router, http.MethodPost, "/api/v1/focus-sessions", body, map[string]string{"Idempotency-Key": key})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create Focus status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	return decodeFocusSnapshot(t, recorder.Body.Bytes())
}

func focusCommandForTest(t *testing.T, router http.Handler, id, command string, version int64, key string) *httptestResponse {
	t.Helper()
	headers := map[string]string{"If-Match": fmt.Sprintf("\"%d\"", version)}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	recorder := performRequest(router, http.MethodPost, "/api/v1/focus-sessions/"+id+"/"+command, []byte(`{}`), headers)
	return &httptestResponse{Code: recorder.Code, Body: recorder.Body.Bytes(), Header: recorder.Header()}
}

type httptestResponse struct {
	Code   int
	Body   []byte
	Header http.Header
}

func TestFocusSessionLifecycleIntervalsExactLedgerAndIdempotency(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 10)

	created := createFocusForTest(t, router, &taskID, 300, "focus-create-ledger")
	if created.Session == nil || created.Session.Status != "active" || created.Session.Version != 1 || created.Session.TaskTitle == nil || created.ElapsedSeconds != 0 || created.RemainingSeconds != 300 {
		t.Fatalf("created Focus snapshot = %#v", created)
	}
	firstBody, _ := json.Marshal(created)
	clock.Add(time.Minute)
	replayBody, _ := json.Marshal(createFocusSessionRequest{TaskID: &taskID, PlannedSeconds: 300})
	replayed := performRequest(router, http.MethodPost, "/api/v1/focus-sessions", replayBody, map[string]string{"Idempotency-Key": "focus-create-ledger"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("create replay status=%d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	replayedSnapshot := decodeFocusSnapshot(t, replayed.Body.Bytes())
	replayedBody, _ := json.Marshal(replayedSnapshot)
	if string(replayedBody) != string(firstBody) {
		t.Fatalf("create replay changed snapshot: first=%s replay=%s", firstBody, replayedBody)
	}
	conflictBody, _ := json.Marshal(createFocusSessionRequest{TaskID: &taskID, PlannedSeconds: 600})
	conflict := performRequest(router, http.MethodPost, "/api/v1/focus-sessions", conflictBody, map[string]string{"Idempotency-Key": "focus-create-ledger"})
	assertAPIError(t, conflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	secondOpen := performRequest(router, http.MethodPost, "/api/v1/focus-sessions", replayBody, map[string]string{"Idempotency-Key": "another-focus"})
	assertAPIError(t, secondOpen, http.StatusConflict, "ACTIVE_FOCUS_SESSION_EXISTS")

	clock.Set(time.Date(2026, 3, 1, 8, 0, 40, 0, time.UTC))
	pausedResponse := focusCommandForTest(t, router, created.Session.ID, "pause", 1, "")
	if pausedResponse.Code != http.StatusOK {
		t.Fatalf("pause status=%d body=%s", pausedResponse.Code, pausedResponse.Body)
	}
	paused := decodeFocusSnapshot(t, pausedResponse.Body)
	if paused.Session == nil || paused.Session.Status != "paused" || paused.Session.AccumulatedSeconds != 40 || paused.Session.Version != 2 {
		t.Fatalf("paused Focus snapshot = %#v", paused)
	}
	if got := focusInt64(t, store, "SELECT SUM(duration_seconds) FROM focus_session_intervals WHERE session_id = ?", created.Session.ID); got != 40 {
		t.Fatalf("paused interval seconds=%d want=40", got)
	}

	clock.Set(time.Date(2026, 3, 1, 8, 0, 50, 0, time.UTC))
	resumedResponse := focusCommandForTest(t, router, created.Session.ID, "resume", 2, "")
	if resumedResponse.Code != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resumedResponse.Code, resumedResponse.Body)
	}
	resumed := decodeFocusSnapshot(t, resumedResponse.Body)
	if resumed.Session == nil || resumed.Session.Status != "active" || resumed.Session.Version != 3 {
		t.Fatalf("resumed Focus snapshot = %#v", resumed)
	}

	clock.Set(time.Date(2026, 3, 1, 8, 1, 25, 0, time.UTC))
	stoppedResponse := focusCommandForTest(t, router, created.Session.ID, "stop", 3, "focus-stop-ledger")
	if stoppedResponse.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", stoppedResponse.Code, stoppedResponse.Body)
	}
	stopped := decodeFocusSnapshot(t, stoppedResponse.Body)
	if stopped.Session == nil || stopped.Session.Status != "completed" || stopped.Session.AccumulatedSeconds != 75 ||
		stopped.Session.CreditedMinutes != 1 || stopped.Session.Version != 4 || stopped.Session.EndReason == nil || *stopped.Session.EndReason != "user_stop" {
		t.Fatalf("stopped Focus snapshot = %#v", stopped)
	}
	assertFocusTaskAccounting(t, store, taskID, 11, 2, 75, 1)
	if got := focusInt64(t, store, "SELECT SUM(duration_seconds) FROM focus_session_intervals WHERE session_id = ?", created.Session.ID); got != 75 {
		t.Fatalf("stopped interval seconds=%d want=75", got)
	}

	replayedStop := focusCommandForTest(t, router, created.Session.ID, "stop", 3, "focus-stop-ledger")
	if replayedStop.Code != http.StatusOK || replayedStop.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("stop replay status=%d headers=%v body=%s", replayedStop.Code, replayedStop.Header, replayedStop.Body)
	}
	conflictingStopReplay := focusCommandForTest(t, router, created.Session.ID, "stop", 4, "focus-stop-ledger")
	if conflictingStopReplay.Code != http.StatusConflict {
		t.Fatalf("stop idempotency conflict status=%d body=%s", conflictingStopReplay.Code, conflictingStopReplay.Body)
	}
	oldVersionStop := focusCommandForTest(t, router, created.Session.ID, "stop", 1, "focus-stop-after-terminal")
	if oldVersionStop.Code != http.StatusOK {
		t.Fatalf("completed stop with old If-Match status=%d body=%s", oldVersionStop.Code, oldVersionStop.Body)
	}
	assertFocusTaskAccounting(t, store, taskID, 11, 2, 75, 1)
	mismatchedTerminal := focusCommandForTest(t, router, created.Session.ID, "cancel", 1, "focus-cancel-after-complete")
	if mismatchedTerminal.Code != http.StatusConflict {
		t.Fatalf("cancel completed session status=%d body=%s", mismatchedTerminal.Code, mismatchedTerminal.Body)
	}

	clock.Set(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	second := createFocusForTest(t, router, &taskID, 300, "focus-create-remainder")
	clock.Add(45 * time.Second)
	secondStop := focusCommandForTest(t, router, second.Session.ID, "stop", 1, "focus-stop-remainder")
	if secondStop.Code != http.StatusOK {
		t.Fatalf("second stop status=%d body=%s", secondStop.Code, secondStop.Body)
	}
	secondStopped := decodeFocusSnapshot(t, secondStop.Body)
	if secondStopped.Session == nil || secondStopped.Session.CreditedMinutes != 1 {
		t.Fatalf("second stopped snapshot = %#v", secondStopped)
	}
	assertFocusTaskAccounting(t, store, taskID, 12, 3, 120, 2)

	clock.Set(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC))
	cancelled := createFocusForTest(t, router, &taskID, 300, "focus-create-cancel")
	clock.Add(30 * time.Second)
	cancelResponse := focusCommandForTest(t, router, cancelled.Session.ID, "cancel", 1, "focus-cancel")
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelResponse.Code, cancelResponse.Body)
	}
	cancelSnapshot := decodeFocusSnapshot(t, cancelResponse.Body)
	if cancelSnapshot.Session == nil || cancelSnapshot.Session.Status != "cancelled" || cancelSnapshot.Session.AccumulatedSeconds != 30 || cancelSnapshot.Session.CreditedMinutes != 0 {
		t.Fatalf("cancelled snapshot = %#v", cancelSnapshot)
	}
	cancelReplay := focusCommandForTest(t, router, cancelled.Session.ID, "cancel", 1, "focus-cancel")
	if cancelReplay.Code != http.StatusOK || cancelReplay.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("cancel replay status=%d headers=%v body=%s", cancelReplay.Code, cancelReplay.Header, cancelReplay.Body)
	}
	cancelConflict := focusCommandForTest(t, router, cancelled.Session.ID, "cancel", 2, "focus-cancel")
	if cancelConflict.Code != http.StatusConflict {
		t.Fatalf("cancel idempotency conflict status=%d body=%s", cancelConflict.Code, cancelConflict.Body)
	}
	repeatCancel := focusCommandForTest(t, router, cancelled.Session.ID, "cancel", 1, "different-cancel-key")
	if repeatCancel.Code != http.StatusOK {
		t.Fatalf("repeat cancel with old If-Match status=%d body=%s", repeatCancel.Code, repeatCancel.Body)
	}
	assertFocusTaskAccounting(t, store, taskID, 12, 3, 120, 2)

	clock.Set(time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC))
	capped := createFocusForTest(t, router, nil, 300, "focus-create-capped")
	clock.Add(400 * time.Second)
	cappedResponse := focusCommandForTest(t, router, capped.Session.ID, "stop", 1, "focus-stop-capped")
	if cappedResponse.Code != http.StatusOK {
		t.Fatalf("capped stop status=%d body=%s", cappedResponse.Code, cappedResponse.Body)
	}
	cappedSnapshot := decodeFocusSnapshot(t, cappedResponse.Body)
	if cappedSnapshot.Session == nil || cappedSnapshot.Session.AccumulatedSeconds != 300 || cappedSnapshot.Session.EndReason == nil || *cappedSnapshot.Session.EndReason != "completed" {
		t.Fatalf("capped Focus snapshot=%#v", cappedSnapshot)
	}
	if got := focusInt64(t, store, "SELECT SUM(duration_seconds) FROM focus_session_intervals WHERE session_id = ?", capped.Session.ID); got != 300 {
		t.Fatalf("capped interval seconds=%d want=300", got)
	}
	emptyActive := performRequest(router, http.MethodGet, "/api/v1/focus-sessions/active", nil, nil)
	if emptyActive.Code != http.StatusOK {
		t.Fatalf("empty active status=%d body=%s", emptyActive.Code, emptyActive.Body.String())
	}
	emptySnapshot := decodeFocusSnapshot(t, emptyActive.Body.Bytes())
	if emptySnapshot.Session != nil || emptySnapshot.ServerNow == "" || emptySnapshot.ElapsedSeconds != 0 || emptySnapshot.RemainingSeconds != 0 {
		t.Fatalf("empty active snapshot=%#v", emptySnapshot)
	}

	actions := []string{"focus_started", "focus_paused", "focus_resumed", "focus_completed", "focus_cancelled", "task_actual_time_added"}
	for _, action := range actions {
		if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = ?", action); got == 0 {
			t.Fatalf("workflow event %s was not recorded", action)
		}
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'focus_completed'", created.Session.ID); got != 1 {
		t.Fatalf("idempotent stop completion events=%d want=1", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'focus_cancelled'", cancelled.Session.ID); got != 1 {
		t.Fatalf("idempotent cancel events=%d want=1", got)
	}
}

func TestFocusSessionValidationStateAndConcurrentPause(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	cancelledTaskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	seedFocusTask(t, store, cancelledTaskID, "cancelled", 0)

	for _, test := range []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"too short", `{"task_id":null,"planned_seconds":299}`, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"too long", `{"task_id":null,"planned_seconds":7201}`, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"bad task id", `{"task_id":"not-a-uuid","planned_seconds":300}`, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		{"missing task", `{"task_id":"018f0000-0000-7000-8000-000000009999","planned_seconds":300}`, http.StatusUnprocessableEntity, "TASK_NOT_FOUND"},
		{"cancelled task", fmt.Sprintf(`{"task_id":%q,"planned_seconds":300}`, cancelledTaskID), http.StatusConflict, "TASK_CANCELLED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/focus-sessions", []byte(test.body), nil)
			assertAPIError(t, response, test.status, test.code)
		})
	}

	created := createFocusForTest(t, router, &taskID, 300, "focus-concurrent")
	missingVersion := performRequest(router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/pause", []byte(`{}`), nil)
	assertAPIError(t, missingVersion, http.StatusPreconditionRequired, "VERSION_REQUIRED")
	badVersion := performRequest(router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/pause", []byte(`{}`), map[string]string{"If-Match": "bad"})
	assertAPIError(t, badVersion, http.StatusBadRequest, "INVALID_VERSION")

	clock.Add(10 * time.Second)
	results := make(chan int, 2)
	for range 2 {
		go func() {
			response := performRequest(
				router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/pause",
				[]byte(`{}`), map[string]string{"If-Match": `"1"`},
			)
			results <- response.Code
		}()
	}
	statuses := []int{<-results, <-results}
	successes, conflicts := 0, 0
	for _, status := range statuses {
		if status == http.StatusOK {
			successes++
		}
		if status == http.StatusConflict {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent pause statuses=%v want one 200 and one 409", statuses)
	}
	currentVersion := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", created.Session.ID)
	invalidResume := focusCommandForTest(t, router, created.Session.ID, "pause", currentVersion, "")
	if invalidResume.Code != http.StatusConflict {
		t.Fatalf("pause from paused status=%d body=%s", invalidResume.Code, invalidResume.Body)
	}
	invalidRecover := performRequest(
		router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/recover",
		[]byte(`{"action":"interrupt"}`), map[string]string{"If-Match": fmt.Sprintf("\"%d\"", currentVersion)},
	)
	if invalidRecover.Code != http.StatusConflict {
		t.Fatalf("recover from paused status=%d body=%s", invalidRecover.Code, invalidRecover.Body.String())
	}
	invalidRecoverAction := performRequest(
		router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/recover",
		[]byte(`{"action":"guess"}`), map[string]string{"If-Match": fmt.Sprintf("\"%d\"", currentVersion)},
	)
	assertAPIError(t, invalidRecoverAction, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

func TestFocusSessionConcurrentCreateHasSingleWinner(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	body, err := json.Marshal(createFocusSessionRequest{TaskID: &taskID, PlannedSeconds: 300})
	if err != nil {
		t.Fatalf("encode concurrent create: %v", err)
	}
	results := make(chan int, 2)
	for index := range 2 {
		go func(key string) {
			response := performRequest(
				router, http.MethodPost, "/api/v1/focus-sessions", body,
				map[string]string{"Idempotency-Key": key},
			)
			results <- response.Code
		}(fmt.Sprintf("focus-concurrent-create-%d", index))
	}
	statuses := []int{<-results, <-results}
	winners, conflicts := 0, 0
	for _, status := range statuses {
		if status == http.StatusCreated {
			winners++
		}
		if status == http.StatusConflict {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent create statuses=%v want one 201 and one 409", statuses)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE status IN ('active', 'paused', 'recovery_pending')"); got != 1 {
		t.Fatalf("open Focus Session count=%d want=1", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE ended_at IS NULL"); got != 1 {
		t.Fatalf("open Focus interval count=%d want=1", got)
	}
}

func TestFocusSessionConcurrentStopReturnsTerminalOnceAndCreditsOnce(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	created := createFocusForTest(t, router, &taskID, 300, "focus-concurrent-stop-create")
	clock.Add(90 * time.Second)
	results := make(chan int, 2)
	for index := range 2 {
		go func(key string) {
			response := performRequest(
				router, http.MethodPost, "/api/v1/focus-sessions/"+created.Session.ID+"/stop",
				[]byte(`{}`), map[string]string{"If-Match": `"1"`, "Idempotency-Key": key},
			)
			results <- response.Code
		}(fmt.Sprintf("focus-concurrent-stop-%d", index))
	}
	statuses := []int{<-results, <-results}
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusOK {
		t.Fatalf("concurrent stop statuses=%v want both 200 terminal results", statuses)
	}
	assertFocusTaskAccounting(t, store, taskID, 1, 2, 90, 1)
	if got := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", created.Session.ID); got != 2 {
		t.Fatalf("concurrent stop Focus version=%d want=2", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'focus_completed'", created.Session.ID); got != 1 {
		t.Fatalf("concurrent stop completion event count=%d want=1", got)
	}
}

func TestFocusSessionStartupRecoveryIntervalsAndHeartbeatRefresh(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 3, 8, 1, 0, 0, time.UTC)}
	store, err := database.Open(filepath.Join(t.TempDir(), "focus-recovery.db"))
	if err != nil {
		t.Fatalf("database.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	sessionID := uuid.NewString()
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, task_id, started_at, status, planned_seconds, accumulated_seconds,
			last_resumed_at, last_heartbeat_at, version, created_at, updated_at
		) VALUES (?, ?, '2026-03-03T08:00:00Z', 'active', 300, 10,
			'2026-03-03T08:00:10Z', '2026-03-03T08:00:20Z', 5,
			'2026-03-03T08:00:00Z', '2026-03-03T08:00:10Z')
	`, sessionID, taskID); err != nil {
		t.Fatalf("seed active Focus Session: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_session_intervals(session_id, started_at, ended_at, duration_seconds, created_at)
		VALUES (?, '2026-03-03T08:00:00Z', '2026-03-03T08:00:10Z', 10, '2026-03-03T08:00:00Z'),
		       (?, '2026-03-03T08:00:10Z', NULL, 0, '2026-03-03T08:00:10Z')
	`, sessionID, sessionID); err != nil {
		t.Fatalf("seed active Focus intervals: %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: clock.Now, FocusHeartbeatInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	if status := focusString(t, store, "SELECT status FROM focus_sessions WHERE id = ?", sessionID); status != "recovery_pending" {
		t.Fatalf("startup recovery status=%q want recovery_pending", status)
	}
	if version := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", sessionID); version != 6 {
		t.Fatalf("startup recovery version=%d want=6", version)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id = ? AND ended_at IS NULL", sessionID); got != 1 {
		t.Fatalf("startup recovery open interval count=%d want=1", got)
	}

	active := performRequest(router.Engine, http.MethodGet, "/api/v1/focus-sessions/active", nil, nil)
	if active.Code != http.StatusOK || active.Header().Get("ETag") != `"6"` {
		t.Fatalf("active recovery status=%d ETag=%q body=%s", active.Code, active.Header().Get("ETag"), active.Body.String())
	}
	activeSnapshot := decodeFocusSnapshot(t, active.Body.Bytes())
	if activeSnapshot.Session == nil || activeSnapshot.Session.Status != "recovery_pending" || activeSnapshot.ElapsedSeconds != 10 {
		t.Fatalf("active recovery snapshot=%#v", activeSnapshot)
	}

	recovered := performRequest(
		router.Engine, http.MethodPost, "/api/v1/focus-sessions/"+sessionID+"/recover",
		[]byte(`{"action":"exclude_gap_resume"}`), map[string]string{"If-Match": `"6"`},
	)
	if recovered.Code != http.StatusOK {
		t.Fatalf("exclude-gap recovery status=%d body=%s", recovered.Code, recovered.Body.String())
	}
	recoveredSnapshot := decodeFocusSnapshot(t, recovered.Body.Bytes())
	if recoveredSnapshot.Session == nil || recoveredSnapshot.Session.Status != "active" || recoveredSnapshot.Session.AccumulatedSeconds != 20 || recoveredSnapshot.Session.Version != 7 {
		t.Fatalf("exclude-gap recovery snapshot=%#v", recoveredSnapshot)
	}
	if got := focusInt64(t, store, "SELECT COALESCE(SUM(duration_seconds), 0) FROM focus_session_intervals WHERE session_id = ? AND ended_at IS NOT NULL", sessionID); got != 20 {
		t.Fatalf("closed recovered interval seconds=%d want=20", got)
	}

	clock.Add(5 * time.Second)
	heartbeat := performRequest(router.Engine, http.MethodGet, "/api/v1/focus-sessions/active", nil, nil)
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("active heartbeat status=%d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	heartbeatSnapshot := decodeFocusSnapshot(t, heartbeat.Body.Bytes())
	if heartbeatSnapshot.Session == nil || heartbeatSnapshot.Session.Version != 7 || heartbeatSnapshot.Session.LastHeartbeatAt == nil || *heartbeatSnapshot.Session.LastHeartbeatAt != "2026-03-03T08:01:05Z" {
		t.Fatalf("heartbeat snapshot=%#v", heartbeatSnapshot)
	}
	if got := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", sessionID); got != 7 {
		t.Fatalf("heartbeat bumped business version to %d", got)
	}
}

func TestFocusSessionRecoveryActionsCloseUnknownInterval(t *testing.T) {
	for _, test := range []struct {
		action      string
		wantStatus  string
		wantSeconds int64
		wantOpen    int64
		wantEvent   string
	}{
		{"include_gap_resume", "active", 60, 1, "focus_resumed"},
		{"exclude_gap_resume", "active", 20, 1, "focus_resumed"},
		{"interrupt", "interrupted", 20, 0, "focus_interrupted"},
	} {
		t.Run(test.action, func(t *testing.T) {
			clock := &focusTestClock{now: time.Date(2026, 3, 4, 8, 1, 0, 0, time.UTC)}
			router, store := newFocusTestAPI(t, clock)
			sessionID := uuid.NewString()
			if _, err := store.SQL.Exec(`
				INSERT INTO focus_sessions(
					id, started_at, status, planned_seconds, accumulated_seconds,
					last_resumed_at, last_heartbeat_at, version, created_at, updated_at
				) VALUES (?, '2026-03-04T08:00:00Z', 'recovery_pending', 300, 10,
					'2026-03-04T08:00:10Z', '2026-03-04T08:00:20Z', 2,
					'2026-03-04T08:00:00Z', '2026-03-04T08:00:20Z')
			`, sessionID); err != nil {
				t.Fatalf("seed recovery-pending Focus Session: %v", err)
			}
			if _, err := store.SQL.Exec(`
				INSERT INTO focus_session_intervals(session_id, started_at, ended_at, duration_seconds, created_at)
				VALUES (?, '2026-03-04T08:00:00Z', '2026-03-04T08:00:10Z', 10, '2026-03-04T08:00:00Z'),
				       (?, '2026-03-04T08:00:10Z', NULL, 0, '2026-03-04T08:00:10Z')
			`, sessionID, sessionID); err != nil {
				t.Fatalf("seed recovery intervals: %v", err)
			}
			response := performRequest(
				router, http.MethodPost, "/api/v1/focus-sessions/"+sessionID+"/recover",
				[]byte(fmt.Sprintf(`{"action":%q}`, test.action)), map[string]string{"If-Match": `"2"`},
			)
			if response.Code != http.StatusOK {
				t.Fatalf("recover status=%d body=%s", response.Code, response.Body.String())
			}
			snapshot := decodeFocusSnapshot(t, response.Body.Bytes())
			if snapshot.Session == nil || snapshot.Session.Status != test.wantStatus || snapshot.Session.AccumulatedSeconds != test.wantSeconds || snapshot.Session.Version != 3 {
				t.Fatalf("recover snapshot=%#v", snapshot)
			}
			if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id = ? AND ended_at IS NULL", sessionID); got != test.wantOpen {
				t.Fatalf("open intervals=%d want=%d", got, test.wantOpen)
			}
			if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = ?", sessionID, test.wantEvent); got != 1 {
				t.Fatalf("recovery event count=%d want=1", got)
			}
		})
	}
}

func TestFocusHeartbeatTickerIsSidecarOwnedAndDoesNotBumpVersion(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC)}
	store, err := database.Open(filepath.Join(t.TempDir(), "focus-heartbeat.db"))
	if err != nil {
		t.Fatalf("database.Open(): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: clock.Now,
		FocusHeartbeatInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	created := createFocusForTest(t, router.Engine, nil, 300, "focus-heartbeat-create")
	clock.Add(30 * time.Second)
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		value := focusString(t, store, "SELECT last_heartbeat_at FROM focus_sessions WHERE id = ?", created.Session.ID)
		if normalizeTimestamp(value) == "2026-03-05T08:00:30Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Sidecar heartbeat did not refresh before deadline; last=%q", value)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", created.Session.ID); got != 1 {
		t.Fatalf("ticker heartbeat bumped business version=%d", got)
	}
	if err := router.Close(); err != nil {
		t.Fatalf("close Router heartbeat: %v", err)
	}
	clock.Add(30 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if value := normalizeTimestamp(focusString(t, store, "SELECT last_heartbeat_at FROM focus_sessions WHERE id = ?", created.Session.ID)); value != "2026-03-05T08:00:30Z" {
		t.Fatalf("closed Router continued heartbeat updates: %q", value)
	}
}

func TestTaskDeleteBlocksOpenFocusButTerminalSessionDetaches(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	created := createFocusForTest(t, router, &taskID, 300, "focus-delete-task")

	blocked := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+taskID, nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, blocked, http.StatusConflict, "TASK_HAS_OPEN_FOCUS_SESSION")
	if got := focusInt64(t, store, "SELECT version FROM tasks WHERE id = ?", taskID); got != 1 {
		t.Fatalf("blocked task delete changed task version=%d", got)
	}

	clock.Add(15 * time.Second)
	cancelled := focusCommandForTest(t, router, created.Session.ID, "cancel", 1, "focus-cancel-before-delete")
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel before task delete status=%d body=%s", cancelled.Code, cancelled.Body)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+taskID, nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete task after terminal Focus status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	var taskIDAfter *string
	if err := store.DB.Raw("SELECT task_id FROM focus_sessions WHERE id = ?", created.Session.ID).Scan(&taskIDAfter).Error; err != nil {
		t.Fatalf("read detached Focus Session: %v", err)
	}
	if taskIDAfter != nil {
		t.Fatalf("terminal Focus Session task_id=%v want nil", taskIDAfter)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id = ?", created.Session.ID); got != 1 {
		t.Fatalf("Focus intervals after task delete=%d want=1", got)
	}
}

func TestFocusStopRollsBackSessionIntervalLedgerTaskAndEventsTogether(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 7, 8, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 4)
	created := createFocusForTest(t, router, &taskID, 300, "focus-rollback-create")
	if _, err := store.SQL.Exec(fmt.Sprintf(`
		CREATE TRIGGER reject_focus_task_credit
		BEFORE UPDATE OF actual_minutes ON tasks
		WHEN NEW.id = %q
		BEGIN
			SELECT RAISE(ABORT, 'TEST_REJECT_FOCUS_CREDIT');
		END
	`, taskID)); err != nil {
		t.Fatalf("create rollback trigger: %v", err)
	}
	clock.Add(75 * time.Second)
	stopped := focusCommandForTest(t, router, created.Session.ID, "stop", 1, "focus-rollback-stop")
	if stopped.Code != http.StatusInternalServerError {
		t.Fatalf("rollback stop status=%d body=%s", stopped.Code, stopped.Body)
	}
	if status := focusString(t, store, "SELECT status FROM focus_sessions WHERE id = ?", created.Session.ID); status != "active" {
		t.Fatalf("rolled-back Focus status=%q want active", status)
	}
	if got := focusInt64(t, store, "SELECT version FROM focus_sessions WHERE id = ?", created.Session.ID); got != 1 {
		t.Fatalf("rolled-back Focus version=%d want=1", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id = ? AND ended_at IS NULL", created.Session.ID); got != 1 {
		t.Fatalf("rolled-back open interval count=%d want=1", got)
	}
	if got := focusInt64(t, store, "SELECT actual_minutes FROM tasks WHERE id = ?", taskID); got != 4 {
		t.Fatalf("rolled-back task actual_minutes=%d want=4", got)
	}
	if got := focusInt64(t, store, "SELECT version FROM tasks WHERE id = ?", taskID); got != 1 {
		t.Fatalf("rolled-back task version=%d want=1", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM task_focus_totals WHERE task_id = ?", taskID); got != 0 {
		t.Fatalf("rolled-back Task Focus total count=%d want=0", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'focus_completed'", created.Session.ID); got != 0 {
		t.Fatalf("rolled-back completion event count=%d want=0", got)
	}
	if got := focusInt64(t, store, "SELECT COUNT(*) FROM idempotency_keys WHERE key = 'focus-rollback-stop'"); got != 0 {
		t.Fatalf("rolled-back idempotency snapshot count=%d want=0", got)
	}
}

func TestTodayFocusStatsUseIANAOverlapDSTAndDistinctSessions(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-08T07:59:30Z", 60}}, "completed")
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-09T06:59:00Z", 120}}, "completed")
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{
		{"2026-03-08T12:00:00Z", 60},
		{"2026-03-08T12:05:00Z", 90},
	}, "completed")
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-08T13:00:00Z", 300}}, "cancelled")

	spring := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-08&timezone=America%2FLos_Angeles", nil, nil)
	if spring.Code != http.StatusOK {
		t.Fatalf("spring DST stats status=%d body=%s", spring.Code, spring.Body.String())
	}
	stats := decodeFocusStats(t, spring.Body.Bytes())
	if stats.Sessions != 3 || stats.Seconds != 240 || stats.Minutes != 4 {
		t.Fatalf("spring DST Focus stats=%#v want sessions=3 seconds=240 minutes=4", stats)
	}
	previousDay := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-07&timezone=America%2FLos_Angeles", nil, nil)
	previousStats := decodeFocusStats(t, previousDay.Body.Bytes())
	if previousStats.Sessions != 1 || previousStats.Seconds != 30 || previousStats.Minutes != 0 {
		t.Fatalf("cross-midnight previous-day stats=%#v", previousStats)
	}
	nextDay := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-09&timezone=America%2FLos_Angeles", nil, nil)
	nextStats := decodeFocusStats(t, nextDay.Body.Bytes())
	if nextStats.Sessions != 1 || nextStats.Seconds != 60 || nextStats.Minutes != 1 {
		t.Fatalf("cross-midnight next-day stats=%#v", nextStats)
	}

	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-11-02T07:30:00Z", 60}}, "completed")
	fall := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-11-01&timezone=America%2FLos_Angeles", nil, nil)
	fallStats := decodeFocusStats(t, fall.Body.Bytes())
	if fallStats.Sessions != 1 || fallStats.Seconds != 60 || fallStats.Minutes != 1 {
		t.Fatalf("fall DST 25-hour stats=%#v", fallStats)
	}

	invalidTimezone := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-08&timezone=Moon%2FBase", nil, nil)
	assertAPIError(t, invalidTimezone, http.StatusBadRequest, "INVALID_TIMEZONE")
	invalidOffset := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-08&timezone_offset_minutes=841", nil, nil)
	assertAPIError(t, invalidOffset, http.StatusBadRequest, "INVALID_TIMEZONE_OFFSET")
	compatibleOffset := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-08&timezone_offset_minutes=480", nil, nil)
	if compatibleOffset.Code != http.StatusOK {
		t.Fatalf("offset-compatible stats status=%d body=%s", compatibleOffset.Code, compatibleOffset.Body.String())
	}
}

func TestTodayFocusStatsSplitPausedSegmentsAcrossLocalMidnightAndUTC14(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)}
	router, store := newFocusTestAPI(t, clock)
	sessionID := uuid.NewString()
	seedCompletedFocusIntervals(t, store, sessionID, []focusIntervalFixture{
		// America/Los_Angeles is UTC-7 on these dates: 23:50 on March 10,
		// then 01:50 after a two-hour paused gap on March 11.
		{"2026-03-11T06:50:00Z", 600},
		{"2026-03-11T08:50:00Z", 600},
	}, "completed")
	firstDay := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-10&timezone=America%2FLos_Angeles", nil, nil)
	firstStats := decodeFocusStats(t, firstDay.Body.Bytes())
	if firstStats.Sessions != 1 || firstStats.Seconds != 600 || firstStats.Minutes != 10 {
		t.Fatalf("pre-midnight paused-segment stats=%#v", firstStats)
	}
	secondDay := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-11&timezone=America%2FLos_Angeles", nil, nil)
	secondStats := decodeFocusStats(t, secondDay.Body.Bytes())
	if secondStats.Sessions != 1 || secondStats.Seconds != 600 || secondStats.Minutes != 10 {
		t.Fatalf("post-midnight paused-segment stats=%#v", secondStats)
	}

	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-08T10:30:00Z", 60}}, "completed")
	utcPlus14 := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-09&timezone=Pacific%2FKiritimati", nil, nil)
	plus14Stats := decodeFocusStats(t, utcPlus14.Body.Bytes())
	if plus14Stats.Sessions != 1 || plus14Stats.Seconds != 60 {
		t.Fatalf("UTC+14 Focus stats=%#v", plus14Stats)
	}
	utcMinus12 := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-07&timezone=Etc%2FGMT%2B12", nil, nil)
	minus12Stats := decodeFocusStats(t, utcMinus12.Body.Bytes())
	if minus12Stats.Sessions != 1 || minus12Stats.Seconds != 60 {
		t.Fatalf("UTC-12 Focus stats=%#v", minus12Stats)
	}
}

type focusIntervalFixture struct {
	StartedAt string
	Seconds   int64
}

func seedCompletedFocusIntervals(t *testing.T, store *database.Store, sessionID string, intervals []focusIntervalFixture, status string) {
	t.Helper()
	if len(intervals) == 0 {
		t.Fatal("Focus interval fixture cannot be empty")
	}
	accumulated := int64(0)
	for _, interval := range intervals {
		accumulated += interval.Seconds
	}
	planned := accumulated
	if planned < 300 {
		planned = 300
	}
	firstStart, err := parseFocusTimestamp(intervals[0].StartedAt)
	if err != nil {
		t.Fatalf("parse Focus fixture start: %v", err)
	}
	last := intervals[len(intervals)-1]
	lastStart, err := parseFocusTimestamp(last.StartedAt)
	if err != nil {
		t.Fatalf("parse Focus fixture end: %v", err)
	}
	endedAt := lastStart.Add(time.Duration(last.Seconds) * time.Second).Format(time.RFC3339Nano)
	endReason := "user_stop"
	if status == "cancelled" {
		endReason = "cancelled"
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO focus_sessions(
			id, started_at, ended_at, status, planned_seconds, accumulated_seconds,
			end_reason, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, sessionID, firstStart.Format(time.RFC3339Nano), endedAt, status, planned, accumulated, endReason, firstStart.Format(time.RFC3339Nano), endedAt); err != nil {
		t.Fatalf("seed completed Focus Session: %v", err)
	}
	for _, interval := range intervals {
		startedAt, err := parseFocusTimestamp(interval.StartedAt)
		if err != nil {
			t.Fatalf("parse interval fixture: %v", err)
		}
		endedAt := startedAt.Add(time.Duration(interval.Seconds) * time.Second).Format(time.RFC3339Nano)
		if _, err := store.SQL.Exec(`
			INSERT INTO focus_session_intervals(session_id, started_at, ended_at, duration_seconds, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, sessionID, startedAt.Format(time.RFC3339Nano), endedAt, interval.Seconds, startedAt.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("seed completed Focus interval: %v", err)
		}
	}
}

func decodeFocusStats(t *testing.T, body []byte) focusStats {
	t.Helper()
	var envelope struct {
		Data struct {
			Focus focusStats `json:"focus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Today Focus stats: %v body=%s", err, body)
	}
	return envelope.Data.Focus
}

func focusString(t *testing.T, store *database.Store, query string, arguments ...any) string {
	t.Helper()
	var value string
	if err := store.SQL.QueryRow(query, arguments...).Scan(&value); err != nil {
		t.Fatalf("query Focus string: %v", err)
	}
	return value
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("API error status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var payload errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode API error: %v body=%s", err, response.Body.String())
	}
	if payload.Code != code {
		t.Fatalf("API error code=%q want=%q body=%s", payload.Code, code, response.Body.String())
	}
}

func assertFocusTaskAccounting(t *testing.T, store *database.Store, taskID string, actualMinutes, taskVersion, exactSeconds, appliedMinutes int64) {
	t.Helper()
	if got := focusInt64(t, store, "SELECT actual_minutes FROM tasks WHERE id = ?", taskID); got != actualMinutes {
		t.Fatalf("task actual_minutes=%d want=%d", got, actualMinutes)
	}
	if got := focusInt64(t, store, "SELECT version FROM tasks WHERE id = ?", taskID); got != taskVersion {
		t.Fatalf("task version=%d want=%d", got, taskVersion)
	}
	if got := focusInt64(t, store, "SELECT exact_seconds FROM task_focus_totals WHERE task_id = ?", taskID); got != exactSeconds {
		t.Fatalf("Task Focus exact_seconds=%d want=%d", got, exactSeconds)
	}
	if got := focusInt64(t, store, "SELECT applied_minutes FROM task_focus_totals WHERE task_id = ?", taskID); got != appliedMinutes {
		t.Fatalf("Task Focus applied_minutes=%d want=%d", got, appliedMinutes)
	}
}

func focusInt64(t *testing.T, store *database.Store, query string, arguments ...any) int64 {
	t.Helper()
	var value int64
	if err := store.SQL.QueryRow(query, arguments...).Scan(&value); err != nil {
		t.Fatalf("query Focus integer: %v", err)
	}
	return value
}
