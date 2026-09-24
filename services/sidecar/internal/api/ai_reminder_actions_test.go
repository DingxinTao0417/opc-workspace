package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIReminderChatUsesRealToolLoopAndReturnsReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 1 {
			if !strings.Contains(string(encoded), `"workspace_guide"`) || strings.Contains(string(encoded), `"reminder.create"`) {
				t.Error("Reminder schema loaded before domain guidance")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "inbox-proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"reminder.create","changes":{"title":"Through harness","trigger_at":"2026-10-01T09:00:00+08:00"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 2 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("no real proposal tool response")
		}
		if calls == 3 {
			if !strings.Contains(string(encoded), "/inbox?reminder=") || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing Reminder receipt")
			}
			toolsJSON, _ := json.Marshal(body["tools"])
			if strings.Contains(string(toolsJSON), "workspace_propose") {
				t.Error("previous permission inherited")
			}
		}
		streamMockAIDelta(w, `请查看下方的操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "reminder-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create an Reminder item", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls != 2 {
		t.Fatalf("chat %d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders", 0)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "Did it succeed?"})
	response = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || calls != 3 {
		t.Fatalf("followup %d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders", 1)
}

func TestAIReminderCreateApproval(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	args := `{"action":"reminder.create","changes":{"title":"月末复盘","trigger_at":"2026-10-31T09:00:00+08:00","recurrence_type":"monthly","recurrence_timezone":"Asia/Shanghai"}}`
	row := proposeTestAction(t, store, tool, args)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "/inbox?reminder=") {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE status='scheduled' AND trigger_at='2026-10-31T01:00:00.000000000Z' AND recurrence_anchor_day=31 AND recurrence_timezone='Asia/Shanghai'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='reminder_created'", 1)
}

func TestAIReminderUpdatesAndCancellationShareNativeRules(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	r := performRequest(router, "POST", "/api/v1/reminders", []byte(`{"title":"Month end","trigger_at":"2026-10-31T09:00:00+08:00","recurrence_type":"monthly","recurrence_timezone":"Asia/Shanghai"}`), nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	var created reminderEnvelope
	if err := json.Unmarshal(r.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Data.ID
	for i, test := range []struct {
		action, changes string
		version         int64
	}{
		{"update", `{"recurrence_interval":2}`, 2},
		{"update", `{"trigger_at":"2026-11-15T10:00:00+08:00"}`, 3},
		{"update", `{"title":"Month end"}`, 3}, // No-op does not bump version or emit an event.
		{"cancel", `{"reason":" series no longer needed "}`, 4},
	} {
		if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
			t.Fatal(err)
		}
		var current models.Reminder
		if err := store.DB.First(&current, "id=?", id).Error; err != nil {
			t.Fatal(err)
		}
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"reminder.%s","reminder_id":%q,"expected_version":%d,"changes":%s}`, test.action, id, current.Version, test.changes))
		if i == 0 && !strings.Contains(row.PreviewJSON, `"recurrence_anchor_day":31`) {
			t.Fatal(row.PreviewJSON)
		}
		if i == 1 && !strings.Contains(row.PreviewJSON, `"recurrence_anchor_day":15`) {
			t.Fatal(row.PreviewJSON)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		for retry := 0; retry < 2; retry++ {
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE id=? AND version=?", 1, id, test.version)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE id=? AND status='cancelled' AND recurrence_interval=2 AND recurrence_anchor_day=15 AND cancel_reason='series no longer needed'", 1, id)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='reminder_updated'", 2, id)
	projector := &API{db: store.DB, options: Options{Now: func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }}}
	if err := projector.projectDueReminders(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
}

func TestAIReminderClockConflictRollbackAndRejection(t *testing.T) {
	for _, scenario := range []string{"create-expired", "update-expired", "version", "fired", "audit", "reject"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, service, tool, generation := aiActionTestFixture(t)
			r := performRequest(router, "POST", "/api/v1/reminders", []byte(`{"title":"Original reminder","trigger_at":"2026-09-18T12:30:00Z","recurrence_type":"daily","recurrence_timezone":"UTC"}`), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			var created reminderEnvelope
			if err := json.Unmarshal(r.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			service.options.Now = func() time.Time { return time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC) }
			args := fmt.Sprintf(`{"action":"reminder.update","reminder_id":%q,"expected_version":1,"changes":{"title":"Updated reminder"}}`, created.Data.ID)
			if scenario == "create-expired" {
				args = `{"action":"reminder.create","changes":{"title":"Too late","trigger_at":"2026-09-18T11:30:00Z"}}`
			}
			if scenario == "update-expired" {
				args = fmt.Sprintf(`{"action":"reminder.update","reminder_id":%q,"expected_version":1,"changes":{"trigger_at":"2026-09-18T11:30:00Z"}}`, created.Data.ID)
			}
			row := proposeTestAction(t, store, tool, args)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "version":
				r = performRequest(router, "PATCH", "/api/v1/reminders/"+created.Data.ID, []byte(`{"summary":"Changed by owner"}`), map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "fired":
				projector := &API{db: store.DB, options: Options{Now: func() time.Time { return time.Date(2026, 9, 18, 13, 0, 0, 0, time.UTC) }}}
				if err := projector.projectDueReminders(context.Background()); err != nil {
					t.Fatal(err)
				}
			case "audit":
				if err := store.DB.Exec(`CREATE TRIGGER reject_reminder_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			decision := "confirm"
			if scenario == "reject" {
				decision = "reject"
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q}`, row.Fingerprint, decision)), nil)
			switch scenario {
			case "create-expired", "update-expired":
				assertAPIError(t, r, 422, "VALIDATION_ERROR")
			case "version", "fired":
				assertAPIError(t, r, 409, "VERSION_CONFLICT")
			case "audit":
				if r.Code != 500 {
					t.Fatal(r.Body.String())
				}
			case "reject":
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE title='Updated reminder' OR title='Too late'", 0)
			if scenario != "reject" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			}
			if scenario == "audit" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='reminder_updated'", 0)
			}
			if scenario == "fired" {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE status='scheduled' AND occurrence_number=2", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 1)
				if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
					t.Fatal(err)
				}
				_, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"reminder.cancel","reminder_id":%q,"expected_version":2,"changes":{"reason":"cannot alter history"}}`, created.Data.ID)))
				if err == nil || !strings.Contains(err.Error(), "REMINDER_NOT_SCHEDULED") {
					t.Fatalf("terminal guard: %v", err)
				}
			}
		})
	}
}

func TestAIReminderReadPrivacyPagingAndBoundaries(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	var ids []string
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/reminders", []byte(fmt.Sprintf(`{"title":"Reminder %d","summary":"Visible notes","trigger_at":"2026-10-01T01:00:00Z"}`, i)), nil)
		var row reminderEnvelope
		if err := json.Unmarshal(r.Body.Bytes(), &row); err != nil || r.Code != 201 {
			t.Fatal(r.Body.String())
		}
		ids = append(ids, row.Data.ID)
	}
	read := *tool.(*aiWorkspaceTool)
	read.name = "workspace_search"
	page, err := read.Execute(context.Background(), []byte(`{"type":"reminder","status":"scheduled","limit":1}`))
	if err != nil || !strings.Contains(page, `"has_more":true`) || !strings.Contains(page, `"next_offset":1`) || !strings.Contains(page, `"server_now"`) {
		t.Fatalf("page=%s %v", page, err)
	}
	read.name = "workspace_get"
	args := []byte(fmt.Sprintf(`{"type":"reminder","id":%q}`, ids[0]))
	detail, err := read.Execute(context.Background(), args)
	if err != nil || !strings.Contains(detail, `"recurrence_timezone":"UTC"`) || !strings.Contains(detail, "/inbox?reminder="+ids[0]) {
		t.Fatalf("read=%s %v", detail, err)
	}
	for _, private := range []string{"created_by_actor_id", "source_entity_id", "source_event_key", "cancel_reason"} {
		if strings.Contains(detail, private) {
			t.Fatal("private field: " + detail)
		}
	}
	read.policy = harness.NewCapabilities("clients")
	if _, err := read.Execute(context.Background(), args); err == nil {
		t.Fatal("read without work scope")
	}
	for _, changes := range []string{
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00"}`, `{"title":"Invalid","trigger_at":"2026-09-17T01:00:00Z"}`,
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00Z","recurrence_type":"daily","recurrence_timezone":"not-a-zone"}`,
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00Z","recurrence_type":"none","recurrence_interval":2}`,
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00Z","recurrence_anchor_day":5}`,
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00Z","created_by_actor_id":"owner"}`,
		`{"title":"Invalid","trigger_at":"2026-10-01T01:00:00Z","summary":null}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(`{"action":"reminder.create","changes":`+changes+`}`)); err == nil {
			t.Fatal("accepted " + changes)
		}
	}
	for _, action := range []string{"reminder.delete", "reminder.fire", "reminder.cancel", "reminder.update"} {
		if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":%q,"reminder_id":%q,"expected_version":1,"changes":{}}`, action, ids[0]))); err == nil {
			t.Fatal("accepted " + action)
		}
	}
	for _, action := range []string{"task.create", "project.create", "inbox.create"} {
		if _, err := parseAIWorkspaceAction([]byte(fmt.Sprintf(`{"action":%q,"reminder_id":%q,"changes":{"title":"Mixed target","name":"Mixed target"}}`, action, ids[0]))); err == nil {
			t.Fatal("mixed target accepted")
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIReminderCancellationDoesNotCopyUnchangedLongSummary(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	body, _ := json.Marshal(map[string]any{"title": "Long reminder", "summary": strings.Repeat("长", 10000), "trigger_at": "2026-10-01T01:00:00Z"})
	r := performRequest(router, "POST", "/api/v1/reminders", body, nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	var created reminderEnvelope
	if err := json.Unmarshal(r.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"reminder.cancel","reminder_id":%q,"expected_version":1,"changes":{"reason":"No longer needed"}}`, created.Data.ID))
	if strings.Contains(row.PreviewJSON, "长") {
		t.Fatal("unchanged summary copied into cancellation preview")
	}
}
