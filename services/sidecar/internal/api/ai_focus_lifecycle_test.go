package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIFocusStartStopApprovalLifecycle(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	router, store, _, tool, generation := aiFocusClockFixture(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 10)
	start := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.start","changes":{"task_id":%q,"expected_task_version":1,"planned_seconds":300}}`, taskID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + start.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, start.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 422, "FOCUS_START_CONFIRMATION_REQUIRED")
	body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_focus_start":true}`, start.Fingerprint))
	r := performRequest(router, "POST", path, body, nil)
	if r.Code != 200 {
		t.Fatalf("start=%d %s", r.Code, r.Body.String())
	}
	var result struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	id := *result.Data.ResultID
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active'", 1, id)
	clock.Add(20 * time.Second)
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	stop := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.stop","focus_session_id":%q,"expected_version":1,"changes":{}}`, id))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	clock.Add(55 * time.Second)
	stopPath := "/api/v1/ai/actions/" + stop.ID + "/decision"
	stopBody := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, stop.Fingerprint))
	for i := 0; i < 2; i++ {
		if r := performRequest(router, "POST", stopPath, stopBody, nil); r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='completed' AND accumulated_seconds=75 AND credited_minutes=1", 1, id)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND actual_minutes=11 AND status='todo' AND version=2", 1, taskID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_completed'", 1)
	// A confirmed creation receipt is history, not a command to restart it.
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions", 1)
}

func TestAIFocusStartStrictPayload(t *testing.T) {
	for _, args := range []string{
		`{"action":"focus.start","changes":{"planned_seconds":300}}`,
		`{"action":"focus.start","focus_session_id":null,"changes":{"task_id":null,"planned_seconds":300}}`,
		`{"action":"focus.start","expected_version":0,"changes":{"task_id":null,"planned_seconds":300}}`,
		`{"action":"focus.start","changes":{"task_id":null,"expected_task_version":1,"planned_seconds":300}}`,
		`{"action":"focus.start","changes":{"task_id":null,"planned_seconds":299}}`,
		`{"action":"focus.start","changes":{"task_id":null,"planned_seconds":300,"confirm_focus_start":true}}`,
		`{"action":"focus.recover","focus_session_id":"00000000-0000-4000-8000-000000000001","expected_version":1,"changes":{"recovery_action":"guess"}}`,
	} {
		if _, err := parseAIWorkspaceAction([]byte(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	if _, err := parseAIWorkspaceAction([]byte(`{"action":"focus.start","changes":{"task_id":null,"planned_seconds":300}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestAIFocusLifecycleHarnessHumanDecisionAndReceipt(t *testing.T) {
	for _, mode := range []string{"start", "stop", "cancel", "include_gap_resume", "exclude_gap_resume", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			action, expectedStatus, sessionID, version := "focus."+mode, "active", "", 1
			changes := `{}`
			consent := ""
			if mode == "start" {
				changes = `{"task_id":null,"planned_seconds":300}`
				consent = `,"confirm_focus_start":true`
			} else {
				sessionID = createFocusForTest(t, router, nil, 300, "loop-"+mode).Session.ID
				switch mode {
				case "stop":
					expectedStatus = "completed"
				case "cancel":
					expectedStatus = "cancelled"
				default:
					action = "focus.recover"
					version = 2
					changes = fmt.Sprintf(`{"recovery_action":%q}`, mode)
					if err := recoverFocusSessionsOnStartup(store.DB, now); err != nil {
						t.Fatal(err)
					}
					if mode == "include_gap_resume" {
						consent = `,"confirm_focus_gap":true`
					}
					if mode == "interrupt" {
						expectedStatus = "interrupted"
					}
				}
			}
			args := fmt.Sprintf(`{"action":%q,"changes":%s}`, action, changes)
			if mode != "start" {
				args = fmt.Sprintf(`{"action":%q,"focus_session_id":%q,"expected_version":%d,"changes":%s}`, action, sessionID, version, changes)
			}
			calls := 0
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				encoded, _ := json.Marshal(body)
				if calls <= 2 {
					name, toolArgs := "workspace_focus", `{"view":"active"}`
					if calls == 2 {
						if mode == "start" {
							if !strings.Contains(string(encoded), `\"session\":null`) {
								t.Error("missing empty Focus fact")
							}
						} else if !strings.Contains(string(encoded), sessionID) {
							t.Error("missing Session fact")
						}
						name, toolArgs = "workspace_propose", args
					}
					w.Header().Set("Content-Type", "text/event-stream")
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("focus-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": toolArgs}}}}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\n", frame)
					return
				}
				if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
					t.Error("missing pending proposal result")
				}
				if calls == 4 {
					if !strings.Contains(string(encoded), action) || !strings.Contains(string(encoded), `\"confirmed\"`) || !strings.Contains(string(encoded), "/focus") {
						t.Error("missing Focus receipt")
					}
					definitions, _ := json.Marshal(body["tools"])
					if aiHasProtectedWorkspaceTool(definitions) {
						t.Error("permission leaked")
					}
				}
				streamMockAIDelta(w, `请核对下方操作卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "focus-life-"+mode, upstream.URL+"/v1", "test-model")
			var current models.AIProvider
			if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "请帮我处理专注", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
			r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
				t.Fatalf("chat %d %s calls=%d", r.Code, r.Body.String(), calls)
			}
			if mode == "start" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions", 0)
			} else {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND version=?", 1, sessionID, version)
			}
			var p models.AIActionProposal
			if err := store.DB.First(&p).Error; err != nil {
				t.Fatal(err)
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+p.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, p.Fingerprint, consent)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE status=?", 1, expectedStatus)
			var session models.AISession
			if err := store.DB.First(&session).Error; err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "结果呢"})
			r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 4 {
				t.Fatalf("receipt %d %s calls=%d", r.Code, r.Body.String(), calls)
			}
		})
	}
}

func TestAIFocusRecoveryConsentTimeAndRollback(t *testing.T) {
	for _, mode := range []string{"include_gap_resume", "exclude_gap_resume", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
			router, store, _, tool, generation := aiFocusClockFixture(t, clock)
			taskID := uuid.NewString()
			seedFocusTask(t, store, taskID, "todo", 10)
			s := createFocusForTest(t, router, &taskID, 300, "recovery-"+mode).Session
			clock.Add(30 * time.Second)
			if r := performRequest(router, "GET", "/api/v1/focus-sessions/active", nil, nil); r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			clock.Add(40 * time.Second)
			if err := recoverFocusSessionsOnStartup(store.DB, clock.Now()); err != nil {
				t.Fatal(err)
			}
			p := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.recover","focus_session_id":%q,"expected_version":2,"changes":{"recovery_action":%q}}`, s.ID, mode))
			if strings.Contains(p.PreviewJSON, "elapsed_seconds") {
				t.Fatal("recovery consent froze moving elapsed time")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/ai/actions/" + p.ID + "/decision"
			decision := map[string]any{"fingerprint": p.Fingerprint, "decision": "confirm"}
			body, _ := json.Marshal(decision)
			if mode == "include_gap_resume" {
				assertAPIError(t, performRequest(router, "POST", path, body, nil), 422, "FOCUS_GAP_CONFIRMATION_REQUIRED")
				decision["confirm_focus_gap"] = false
				body, _ = json.Marshal(decision)
				assertAPIError(t, performRequest(router, "POST", path, body, nil), 422, "FOCUS_GAP_CONFIRMATION_REQUIRED")
				decision["confirm_focus_gap"] = true
			} else {
				decision["confirm_focus_gap"] = true
				body, _ = json.Marshal(decision)
				assertAPIError(t, performRequest(router, "POST", path, body, nil), 422, "AI_ACTION_DECISION_INVALID")
				delete(decision, "confirm_focus_gap")
			}
			body, _ = json.Marshal(decision)
			clock.Add(25 * time.Second)
			if err := store.DB.Exec(`CREATE TRIGGER fail_recovery_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
				t.Fatalf("rollback %d %s", r.Code, r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='recovery_pending' AND version=2 AND accumulated_seconds=0", 1, s.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND ended_at IS NULL", 1, s.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, p.ID)
			if err := store.DB.Exec("DROP TRIGGER fail_recovery_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			}
			status, elapsed, openIntervals, event := "active", 30, 1, "focus_resumed"
			if mode == "include_gap_resume" {
				elapsed = 95
			}
			if mode == "interrupt" {
				status, openIntervals, event = "interrupted", 0, "focus_interrupted"
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status=? AND version=3 AND accumulated_seconds=? AND credited_minutes=0", 1, s.ID, status, elapsed)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND ended_at IS NULL", int64(openIntervals), s.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action=?", 1, event)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND actual_minutes=10 AND version=1 AND status='todo'", 1, taskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_focus_totals", 0)
		})
	}
}

func TestAIFocusStartConflictsAndApprovalRollback(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	router, store, _, tool, generation := aiFocusClockFixture(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 0)
	bound := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.start","changes":{"task_id":%q,"expected_task_version":1,"planned_seconds":300}}`, taskID))
	unbound := proposeTestAction(t, store, tool, `{"action":"focus.start","changes":{"task_id":null,"planned_seconds":300}}`)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Task{}).Where("id=?", taskID).Update("version", 2).Error; err != nil {
		t.Fatal(err)
	}
	confirm := func(p models.AIActionProposal) (string, []byte) {
		return "/api/v1/ai/actions/" + p.ID + "/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_focus_start":true}`, p.Fingerprint))
	}
	path, body := confirm(bound)
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions", 0)
	path, body = confirm(unbound)
	if err := store.DB.Exec(`CREATE TRIGGER fail_start_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
		t.Fatalf("rollback %d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_started'", 0)
	if err := store.DB.Exec("DROP TRIGGER fail_start_approval").Error; err != nil {
		t.Fatal(err)
	}
	native := createFocusForTest(t, router, nil, 300, "competing-start").Session
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "ACTIVE_FOCUS_SESSION_EXISTS")
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"action":"focus.start","changes":{"task_id":null,"planned_seconds":600}}`)); err == nil {
		t.Fatal("proposed start over existing active session")
	}
	if r := focusCommandForTest(t, router, native.ID, "cancel", 1, "cancel-competing"); r.Code != 200 {
		t.Fatal(string(r.Body))
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE status='active' AND task_id IS NULL", 1)
}

func TestAIFocusTerminalRollbackCreditAndStaleProposal(t *testing.T) {
	for _, command := range []string{"stop", "cancel"} {
		t.Run(command, func(t *testing.T) {
			clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
			router, store, _, tool, generation := aiFocusClockFixture(t, clock)
			taskID := uuid.NewString()
			seedFocusTask(t, store, taskID, "todo", 10)
			s := createFocusForTest(t, router, &taskID, 300, "terminal-"+command).Session
			clock.Add(65 * time.Second)
			p := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.%s","focus_session_id":%q,"expected_version":1,"changes":{}}`, command, s.ID))
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			path, body := "/api/v1/ai/actions/"+p.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, p.Fingerprint))
			if err := store.DB.Exec(`CREATE TRIGGER fail_end_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND actual_minutes=10 AND version=1", 1, taskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_focus_totals", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active' AND version=1", 1, s.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND ended_at IS NULL", 1, s.ID)
			if err := store.DB.Exec("DROP TRIGGER fail_end_approval").Error; err != nil {
				t.Fatal(err)
			}
			// A matching native terminal command is not permission to confirm a stale AI preview.
			if r := focusCommandForTest(t, router, s.ID, command, 1, "native-"+command); r.Code != 200 {
				t.Fatal(string(r.Body))
			}
			assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "VERSION_CONFLICT")
			minutes := 10
			if command == "stop" {
				minutes = 11
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND actual_minutes=? AND status='todo'", 1, taskID, minutes)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, p.ID)
		})
	}
}
