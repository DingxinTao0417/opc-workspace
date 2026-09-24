package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiFocusClockFixture(t *testing.T, clock *focusTestClock) (*gin.Engine, *database.Store, *API, harness.Tool, models.AIGeneration) {
	t.Helper()
	router, store := newFocusTestAPI(t, clock)
	now := clock.Now()
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "Focus approval", Persist: true, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: clock.Now}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("missing proposal tool")
	}
	return router, store, service, tool, generation
}

func TestAIFocusConfirmationTimeRollbackAndCompetingNativeChange(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	router, store, _, tool, generation := aiFocusClockFixture(t, clock)
	taskID := uuid.NewString()
	seedFocusTask(t, store, taskID, "todo", 10)
	session := createFocusForTest(t, router, &taskID, 300, "focus-human-time").Session
	clock.Add(30 * time.Second)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.pause","focus_session_id":%q,"expected_version":1,"changes":{}}`, session.ID))
	if strings.Contains(proposal.PreviewJSON, "elapsed_seconds") {
		t.Fatal("consent froze moving time")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + proposal.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint))
	clock.Add(45 * time.Second)
	if err := store.DB.Exec(`CREATE TRIGGER fail_focus_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
		t.Fatalf("rollback=%d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active' AND version=1 AND accumulated_seconds=0", 1, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND ended_at IS NULL", 1, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_paused'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, proposal.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_focus_approval").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='paused' AND version=2 AND accumulated_seconds=75", 1, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND duration_seconds=75 AND ended_at IS NOT NULL", 1, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND actual_minutes=10 AND version=1", 1, taskID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_focus_totals", 0)
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	resume := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.resume","focus_session_id":%q,"expected_version":2,"changes":{}}`, session.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Hour)
	r := focusCommandForTest(t, router, session.ID, "resume", 2, "")
	if r.Code != 200 {
		t.Fatal(string(r.Body))
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+resume.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, resume.Fingerprint)), nil), 409, "VERSION_CONFLICT")
	// Re-reading a historical confirmed pause must not pause the now-resumed Session.
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active' AND version=3 AND accumulated_seconds=75", 1, session.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE session_id=? AND ended_at IS NULL", 1, session.ID)
}

func TestAIFocusStrictArgumentsHistoryAndRecovery(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	read := &aiWorkspaceTool{api: service, name: "workspace_focus", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	empty, err := read.Execute(context.Background(), []byte(`{"view":"active"}`))
	if err != nil || !strings.Contains(empty, `"session":null`) {
		t.Fatalf("empty=%s %v", empty, err)
	}
	for _, args := range []string{`{}`, `{"view":"active","id":""}`, `{"view":"history","id":null}`, `{"view":"session"}`, `{"view":"history","limit":21}`, `{"view":"history","offset":-1}`, `{"view":"history","status":"active"}`, `{"view":"history","task_id":""}`, `{"view":"history","task_id":"bad"}`, `{"view":"active","limit":1}`, `{"view":"active","confirm":true}`} {
		if _, err := read.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	for _, command := range []string{"stop", "cancel"} {
		s := createFocusForTest(t, router, nil, 300, "history-"+command).Session
		r := focusCommandForTest(t, router, s.ID, command, 1, "terminal-"+command)
		if r.Code != 200 {
			t.Fatal(string(r.Body))
		}
	}
	result, err := read.Execute(context.Background(), []byte(`{"view":"history","limit":1}`))
	if err != nil || !strings.Contains(result, `"has_more":true`) || !strings.Contains(result, `"next_offset":1`) {
		t.Fatalf("history=%s %v", result, err)
	}
	result, err = read.Execute(context.Background(), []byte(`{"view":"history","limit":1,"offset":1}`))
	if err != nil || !strings.Contains(result, `"has_more":false`) {
		t.Fatalf("last=%s %v", result, err)
	}
	result, err = read.Execute(context.Background(), []byte(`{"view":"history","status":"cancelled"}`))
	if err != nil || strings.Contains(result, `"status":"completed"`) || !strings.Contains(result, `"status":"cancelled"`) {
		t.Fatalf("filter=%s %v", result, err)
	}
	s := createFocusForTest(t, router, nil, 300, "recovery").Session
	if err := recoverFocusSessionsOnStartup(store.DB, service.options.Now()); err != nil {
		t.Fatal(err)
	}
	result, err = read.Execute(context.Background(), []byte(`{"view":"active"}`))
	if err != nil || !strings.Contains(result, `"recovery_pending":true`) {
		t.Fatalf("recovery=%s %v", result, err)
	}
	for _, command := range []string{"pause", "resume", "stop", "start", "recover"} {
		args := fmt.Sprintf(`{"action":"focus.%s","focus_session_id":%q,"expected_version":2,"changes":{}}`, command, s.ID)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("accepted recovery/unsupported %s", command)
		}
	}
	for _, key := range []string{"task_id", "project_id", "inbox_item_id", "reminder_id"} {
		args := fmt.Sprintf(`{"action":"focus.pause","focus_session_id":%q,%q:null,"expected_version":1,"changes":{}}`, s.ID, key)
		if _, err := parseAIWorkspaceAction([]byte(args)); err == nil {
			t.Errorf("mixed %s accepted", key)
		}
	}
	for _, args := range []string{`{"action":"task.create","focus_session_id":null,"changes":{"title":"not focus"}}`, fmt.Sprintf(`{"action":"focus.pause","focus_session_id":%q,"expected_version":1,"changes":{"confirm":true}}`, s.ID)} {
		if _, err := parseAIWorkspaceAction([]byte(args)); err == nil {
			t.Errorf("unexpected fields accepted: %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIFocusTaskRenameInvalidatesPreview(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	id := uuid.NewString()
	seedFocusTask(t, store, id, "todo", 0)
	if err := store.DB.Model(&models.Task{}).Where("id=?", id).Update("description", "private-task-body").Error; err != nil {
		t.Fatal(err)
	}
	s := createFocusForTest(t, router, &id, 300, "rename-focus").Session
	read := &aiWorkspaceTool{api: service, name: "workspace_focus", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	result, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"session","id":%q}`, s.ID)))
	if err != nil || !strings.Contains(result, id) || strings.Contains(result, "private-task-body") || strings.Contains(result, "description") {
		t.Fatalf("projection=%s %v", result, err)
	}
	if _, err := read.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"session","id":%q}`, uuid.NewString()))); err == nil {
		t.Fatal("nonexistent session read succeeded")
	}
	p := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"focus.pause","focus_session_id":%q,"expected_version":1,"changes":{}}`, s.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Task{}).Where("id=?", id).Update("title", "Different work").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+p.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, p.Fingerprint)), nil), 409, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active' AND version=1", 1, s.ID)
}

func TestAIFocusHarnessReadProposeDecisionAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	s := createFocusForTest(t, router, nil, 300, "focus-loop").Session
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 1 || calls == 2 {
			name, args := "workspace_focus", `{"view":"active"}`
			if calls == 2 {
				if !strings.Contains(string(encoded), s.ID) || !strings.Contains(string(encoded), `\"status\":\"active\"`) {
					t.Error("missing true Focus state")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"focus.pause","focus_session_id":%q,"expected_version":1,"changes":{}}`, s.ID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("focus-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("proposal not returned")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), "focus.pause") || !strings.Contains(string(encoded), `\"confirmed\"`) || !strings.Contains(string(encoded), "/focus") {
				t.Error("missing approval receipt")
			}
			definitions, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(definitions) {
				t.Error("grant leaked to next message")
			}
		}
		streamMockAIDelta(w, `请查看下方实际操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "focus-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "暂停当前专注", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND status='active' AND version=1", 1, s.ID)
	var p models.AIActionProposal
	if err := store.DB.First(&p).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+p.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, p.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "执行成功了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatalf("%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_paused'", 1)
}

func TestAIFocusReadAndHumanPauseResume(t *testing.T) {
	router, store, service, propose, generation := aiActionTestFixture(t)
	created := createFocusForTest(t, router, nil, 300, "ai-focus-create")
	id := created.Session.ID
	read := &aiWorkspaceTool{api: service, name: "workspace_focus", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	result, err := read.Execute(context.Background(), []byte(`{"view":"active"}`))
	if err != nil || !strings.Contains(result, id) || !strings.Contains(result, `"local_cycle_included":false`) {
		t.Fatalf("read=%s err=%v", result, err)
	}
	for index, command := range []string{"pause", "resume"} {
		row := proposeTestAction(t, store, propose, fmt.Sprintf(`{"action":"focus.%s","focus_session_id":%q,"expected_version":%d,"changes":{}}`, command, id, index+1))
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND version=?", 1, id, index+1)
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
		for replay := 0; replay < 2; replay++ {
			response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"route":"/focus"`) {
				t.Fatalf("decision=%d %s", response.Code, response.Body.String())
			}
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_sessions WHERE id=? AND version=?", 1, id, index+2)
		if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
			t.Fatal(err)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_paused'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='focus_resumed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM focus_session_intervals WHERE ended_at IS NULL", 1)
}

func TestAIFocusReadNeverUpdatesHeartbeatAndRequiresScope(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	created := createFocusForTest(t, router, nil, 300, "ai-focus-read")
	var before models.FocusSession
	if err := store.DB.First(&before, "id=?", created.Session.ID).Error; err != nil {
		t.Fatal(err)
	}
	service.options.Now = func() time.Time { return time.Date(2026, 9, 18, 12, 1, 5, 0, time.UTC) }
	read := &aiWorkspaceTool{api: service, name: "workspace_focus", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	result, err := read.Execute(context.Background(), []byte(`{"view":"active"}`))
	if err != nil || !strings.Contains(result, `"elapsed_seconds":65`) {
		t.Fatalf("read=%s err=%v", result, err)
	}
	var after models.FocusSession
	if err := store.DB.First(&after, "id=?", before.ID).Error; err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(before)
	last, _ := json.Marshal(after)
	if string(first) != string(last) {
		t.Fatal("read modified session")
	}
	read.policy = harness.NewCapabilities("clients")
	if _, err := read.Execute(context.Background(), []byte(`{"view":"active"}`)); err == nil {
		t.Fatal("unauthorized Focus read")
	}
}
