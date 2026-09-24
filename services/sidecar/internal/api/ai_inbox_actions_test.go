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

func TestAIInboxChatUsesRealToolLoopAndReturnsInboxReceipt(t *testing.T) {
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
			if !strings.Contains(string(encoded), `"workspace_guide"`) || strings.Contains(string(encoded), `"inbox.create"`) {
				t.Error("Inbox schema loaded before domain guidance")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "inbox-proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"inbox.create","changes":{"title":"Through harness"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 2 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("no real proposal tool response")
		}
		if calls == 3 {
			if !strings.Contains(string(encoded), "/inbox/") || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing Inbox receipt")
			}
			toolsJSON, _ := json.Marshal(body["tools"])
			if strings.Contains(string(toolsJSON), "workspace_propose") {
				t.Error("previous permission inherited")
			}
		}
		streamMockAIDelta(w, `请查看下方的操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "inbox-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create an Inbox item", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls != 2 {
		t.Fatalf("chat %d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
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
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 1)
}

func TestAIInboxCreateApproval(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"inbox.create","changes":{"title":"跟进新需求","summary":"请确认范围","priority":"P1"}}`)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "/inbox/") {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE title=? AND kind='manual' AND resolution_policy='manual' AND status='open'", 1, "跟进新需求")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='created' AND aggregate_type='inbox_item'", 1)
}

func TestAIInboxActionBoundaries(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"Boundary item"}`, "")
	for _, args := range []string{
		`{"action":"inbox.create","changes":{"title":"Forged source","source_entity_type":"reminder"}}`,
		`{"action":"inbox.create","changes":{"title":"Forged data","payload_json":{"secret":"x"}}}`,
		`{"action":"inbox.create","changes":{"title":"Automatic policy","resolution_policy":"all_required_tasks_done"}}`,
		`{"action":"inbox.create","changes":{"title":"Direct state","status":"resolved"}}`,
		fmt.Sprintf(`{"action":"inbox.create","inbox_item_id":%q,"changes":{"title":"Unexpected ID"}}`, item.ID),
		fmt.Sprintf(`{"action":"inbox.update","task_id":%q,"inbox_item_id":%q,"expected_version":1,"changes":{"title":"Mixed target"}}`, item.ID, item.ID),
		fmt.Sprintf(`{"action":"task.update","inbox_item_id":%q,"task_id":%q,"expected_version":1,"changes":{"title":"Mixed target"}}`, item.ID, item.ID),
		fmt.Sprintf(`{"action":"project.update","inbox_item_id":%q,"project_id":%q,"expected_version":1,"changes":{"name":"Mixed target"}}`, item.ID, item.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	for _, c := range []struct{ action, changes string }{
		{"update", `{}`}, {"update", `{"title":null}`}, {"update", `{"priority":null}`},
		{"update", `{"priority":"admin"}`}, {"update", `{"due_at":"tomorrow"}`},
		{"read", `{"reason":"not allowed"}`}, {"read", `{"":"not allowed"}`}, {"snooze", `{"snoozed_until":"2026-09-18T11:59:59Z"}`},
		{"snooze", `{"snoozed_until":"2026-09-18T13:00:00"}`}, {"resolve", `{}`}, {"dismiss", `{"reason":"  "}`},
		{"reopen", `{"reason":null}`}, {"force-resolve", `{"reason":"override","confirm":true}`}, {"delete", `{}`},
	} {
		args := fmt.Sprintf(`{"action":"inbox.%s","inbox_item_id":%q,"expected_version":1,"changes":%s}`, c.action, item.ID, c.changes)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=1 AND status='open'", 1, item.ID)
}

func TestAIInboxCommandsPreserveDomainFacts(t *testing.T) {
	for _, c := range []struct{ action, initial, changes, status string }{
		{"read", "resolve", `{}`, "resolved"},
		{"snooze", "", `{"snoozed_until":"2026-09-19T10:00:00+08:00"}`, "open"},
		{"unsnooze", "snooze", `{}`, "open"},
		{"resolve", "", `{"reason":"处理完成"}`, "resolved"},
		{"dismiss", "", `{"reason":"不再需要"}`, "dismissed"},
		{"reopen", "dismiss", `{"reason":"需要继续跟进"}`, "open"},
	} {
		t.Run(c.action, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			item := createInboxItemForTest(t, router, `{"title":"Command item"}`, "")
			if c.initial != "" {
				body := `{"reason":"test reason"}`
				if c.initial == "snooze" {
					body = `{"snoozed_until":"2026-09-19T12:00:00Z"}`
				}
				r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/"+c.initial, []byte(body), map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				item = decodeInboxItemData(t, r.Body.Bytes())
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.%s","inbox_item_id":%q,"expected_version":%d,"changes":%s}`, c.action, item.ID, item.Version, c.changes))
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=? AND status=?", 1, item.ID, item.Version, item.Status)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
				if r.Code != 200 {
					t.Fatalf("%d %s", r.Code, r.Body.String())
				}
			}
			var current models.InboxItem
			if err := store.DB.First(&current, "id=?", item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != c.status || current.Version != item.Version+1 {
				t.Fatalf("%+v", current)
			}
			if (c.action == "read") != (current.ReadAt != nil) {
				t.Fatalf("read state changed: %+v", current)
			}
			if c.action == "snooze" {
				if current.SnoozedUntil == nil || normalizeTimestamp(*current.SnoozedUntil) != "2026-09-19T02:00:00Z" {
					t.Fatal(current.SnoozedUntil)
				}
			} else if current.SnoozedUntil != nil {
				t.Fatal("snooze was not cleared")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders", 0)
		})
	}
}

func TestAIInboxEditApprovalRollbackAndStaleVersion(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"Old title","summary":"private original summary","payload_json":{"private":"source secret"}}`, "")
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.update","inbox_item_id":%q,"expected_version":1,"changes":{"title":" New title ","due_at":"2026-09-19T16:00:00+08:00"}}`, item.ID))
	stale := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read","inbox_item_id":%q,"expected_version":1,"changes":{}}`, item.ID))
	if strings.Contains(row.PreviewJSON, "private") || strings.Contains(row.PreviewJSON, "secret") || !strings.Contains(row.PreviewJSON, "2026-09-19T08:00:00.000000000Z") {
		t.Fatal(row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`CREATE TRIGGER reject_inbox_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
	if r.Code != 500 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND title='Old title' AND version=1 AND due_at IS NULL", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='updated'", 0, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	if err := store.DB.Exec("DROP TRIGGER reject_inbox_approval").Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND title='New title' AND version=2 AND summary='private original summary'", 1, item.ID)
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+stale.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, stale.Fingerprint)), nil), 409, "VERSION_CONFLICT")
}

func TestAIInboxSnoozeRechecksClockAtConfirmation(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"Time boundary"}`, "")
	service.options.Now = func() time.Time { return time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC) }
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.snooze","inbox_item_id":%q,"expected_version":1,"changes":{"snoozed_until":"2026-09-18T11:30:00Z"}}`, item.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 422, "VALIDATION_ERROR")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=1 AND snoozed_until IS NULL", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}

func TestAIInboxRequiredTasksAndPrivateReadModel(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"Task coordination","summary":"Readable summary","payload_json":{"secret":"never send payload"}}`, "")
	body := `{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"one","title":"Required work","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"}]}`
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+item.ID+"/split", []byte(body), map[string]string{"If-Match": `"1"`})
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	split := decodeInboxSplitResponse(t, r.Body.Bytes())
	args := fmt.Sprintf(`{"action":"inbox.resolve","inbox_item_id":%q,"expected_version":%d,"changes":{"reason":"try resolving"}}`, item.ID, split.InboxItem.Version)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "INBOX_REQUIRED_TASKS_INCOMPLETE") {
		t.Fatalf("resolve guard: %v", err)
	}
	source, err := loadAIInboxContext(context.Background(), store.DB, item.ID, service.options.Now())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(source)
	if strings.Contains(string(encoded), "secret") || !strings.Contains(string(encoded), `"required_remaining":1`) || !strings.Contains(string(encoded), `"resolution_policy":"all_required_tasks_done"`) {
		t.Fatal(string(encoded))
	}
	get := *tool.(*aiWorkspaceTool)
	get.name = "workspace_get"
	read, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"inbox_item","id":%q}`, item.ID)))
	if err != nil || !strings.Contains(read, `"server_now"`) || !strings.Contains(read, "/inbox/"+item.ID) || strings.Contains(read, "never send payload") {
		t.Fatalf("authorized Inbox read = %s %v", read, err)
	}
	// Dismiss is a separate, explicit human decision and does not complete a Task.
	row := proposeTestAction(t, store, tool, strings.Replace(args, "inbox.resolve", "inbox.dismiss", 1))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo'", 1, split.Created[0].Task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='dismissed' AND resolution_policy='all_required_tasks_done'", 1, item.ID)
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	row = proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.reopen","inbox_item_id":%q,"expected_version":%d,"changes":{}}`, item.ID, split.InboxItem.Version+1))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking'", 1, item.ID)
}

func TestAIInboxSourceDateRestrictionAndArchivedEdit(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	p := createProjectForTest(t, router, `{"name":"Source project"}`, nil)
	p = transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"start"}`)
	_ = transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"complete"}`)
	var source models.InboxItem
	if err := store.DB.Where("source_entity_type='project_completion'").First(&source).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"inbox.update","inbox_item_id":%q,"expected_version":%d,"changes":{"due_at":"2026-10-01T12:00:00Z"}}`, source.ID, source.Version)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "VALIDATION_ERROR") {
		t.Fatalf("source guard %v", err)
	}
	r := performRequest(router, "POST", "/api/v1/inbox-items/"+source.ID+"/resolve", []byte(`{"reason":"owner checked"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, source.Version)})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	current := decodeInboxItemData(t, r.Body.Bytes())
	args = fmt.Sprintf(`{"action":"inbox.update","inbox_item_id":%q,"expected_version":%d,"changes":{"title":"Should fail"}}`, source.ID, current.Version)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "INBOX_ITEM_TERMINAL") {
		t.Fatalf("terminal guard %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func aiInboxReadAllTestItem(title string, at time.Time) models.InboxItem {
	timestamp := formatInboxTimestamp(at)
	return models.InboxItem{
		ID: uuid.NewString(), Kind: "manual", Title: title, Summary: "",
		SourceEntityType: "manual", Priority: "P2", Status: "open",
		ResolutionPolicy: "manual", PayloadJSON: "{}", Version: 1,
		CreatedAt: timestamp, UpdatedAt: timestamp,
	}
}

func TestAIInboxReadAllProposalStrictSnapshotPrivacyAndCapacity(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 123, time.UTC)

	t.Run("strict snapshot preview", func(t *testing.T) {
		_, store, _, tool, _ := aiActionTestFixture(t, now)
		for _, item := range []models.InboxItem{
			func() models.InboxItem {
				item := aiInboxReadAllTestItem("Private candidate alpha", now)
				item.Summary = "summary-secret-alpha"
				item.PayloadJSON = `{"source_secret":"payload-secret-alpha"}`
				return item
			}(),
			func() models.InboxItem {
				item := aiInboxReadAllTestItem("Private candidate beta", now)
				item.Summary = "summary-secret-beta"
				return item
			}(),
		} {
			if err := store.DB.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
		}

		cutoff := formatInboxTimestamp(now)
		invalid := []string{
			fmt.Sprintf(`{"action":"inbox.read_all","inbox_item_id":%q,"changes":{"through_created_at":%q}}`, uuid.NewString(), cutoff),
			fmt.Sprintf(`{"action":"inbox.read_all","expected_version":1,"changes":{"through_created_at":%q}}`, cutoff),
			fmt.Sprintf(`{"action":"inbox.read_all","task_id":%q,"changes":{"through_created_at":%q}}`, uuid.NewString(), cutoff),
			fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q,"reason":"not allowed"}}`, cutoff),
			fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q},"unexpected":true}`, cutoff),
			`{"action":"inbox.read_all","changes":{"through_created_at":"not-a-time"}}`,
			fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, formatInboxTimestamp(now.Add(time.Nanosecond))),
		}
		for _, args := range invalid {
			if result, err := tool.Execute(context.Background(), []byte(args)); err == nil {
				t.Fatalf("accepted invalid read_all %s: %s", args, result)
			}
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)

		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff))
		var preview aiActionPreview
		if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
			t.Fatal(err)
		}
		if preview.InboxReadAll == nil || preview.InboxReadAll.ThroughCreatedAt != cutoff ||
			preview.InboxReadAll.CandidateCount != 2 || len(preview.InboxReadAll.SelectionFingerprint) != 64 {
			t.Fatalf("read_all preview = %s", row.PreviewJSON)
		}
		for _, private := range []string{"Private candidate", "summary-secret", "payload-secret", "source_secret"} {
			if strings.Contains(row.PreviewJSON, private) {
				t.Fatalf("read_all preview leaked %q: %s", private, row.PreviewJSON)
			}
		}
	})

	t.Run("one thousand items accepted", func(t *testing.T) {
		_, store, _, tool, _ := aiActionTestFixture(t, now)
		items := make([]models.InboxItem, maxAIInboxReadAllItems)
		for index := range items {
			items[index] = aiInboxReadAllTestItem(fmt.Sprintf("Capacity candidate %04d", index), now)
		}
		if err := store.DB.CreateInBatches(&items, 100).Error; err != nil {
			t.Fatal(err)
		}
		cutoff := formatInboxTimestamp(now)
		args := fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff)
		row := proposeTestAction(t, store, tool, args)
		var preview aiActionPreview
		if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil || preview.InboxReadAll == nil || preview.InboxReadAll.CandidateCount != maxAIInboxReadAllItems {
			t.Fatalf("maximum-size preview=%s err=%v", row.PreviewJSON, err)
		}
	})

	t.Run("more than one thousand rejected", func(t *testing.T) {
		_, store, _, tool, _ := aiActionTestFixture(t, now)
		items := make([]models.InboxItem, maxAIInboxReadAllItems+1)
		for index := range items {
			items[index] = aiInboxReadAllTestItem(fmt.Sprintf("Overflow candidate %04d", index), now)
		}
		if err := store.DB.CreateInBatches(&items, 100).Error; err != nil {
			t.Fatal(err)
		}
		cutoff := formatInboxTimestamp(now)
		args := fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff)
		if result, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "1000-item") {
			t.Fatalf("over-capacity proposal result=%s err=%v", result, err)
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	})
}

func TestAIInboxReadAllConfirmationUsesCutoffAndReplays(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 321, time.UTC)
	router, store, _, tool, generation := aiActionTestFixture(t, now)
	first := createInboxItemForTest(t, router, `{"title":"Snapshot first"}`, "")
	second := createInboxItemForTest(t, router, `{"title":"Snapshot second"}`, "")
	cutoff := formatInboxTimestamp(now)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff))

	afterSnapshot := aiInboxReadAllTestItem("Created after snapshot", now.Add(time.Nanosecond))
	if err := store.DB.Create(&afterSnapshot).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	for attempt := 0; attempt < 2; attempt++ {
		response := performRequest(router, http.MethodPost, path, body, nil)
		var envelope struct {
			Data aiActionResponse `json:"data"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("confirmation %d = %d: %s", attempt, response.Code, response.Body.String())
		}
		result := envelope.Data.InboxReadAllResult
		if envelope.Data.Status != "confirmed" || envelope.Data.Route != "/inbox" || result == nil ||
			result.ThroughCreatedAt != cutoff || result.MarkedCount != 2 || envelope.Data.ResultID == nil ||
			*envelope.Data.ResultID != row.ID || envelope.Data.ResultVersion == nil || *envelope.Data.ResultVersion != 1 {
			t.Fatalf("confirmation %d output = %s", attempt, response.Body.String())
		}
	}
	if readInboxItemReadAt(t, store, first.ID) == nil || readInboxItemReadAt(t, store, second.ID) == nil || readInboxItemReadAt(t, store, afterSnapshot.ID) != nil {
		t.Fatalf("snapshot scope first=%v second=%v after=%v", readInboxItemReadAt(t, store, first.ID), readInboxItemReadAt(t, store, second.ID), readInboxItemReadAt(t, store, afterSnapshot.ID))
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id IN (?, ?) AND version=2", 2, first.ID, second.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=1", 1, afterSnapshot.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='read'", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIInboxReadAllRejectsChangedCandidateSetOrVersion(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 456, time.UTC)

	t.Run("same count replacement", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t, now)
		removed := createInboxItemForTest(t, router, `{"title":"Candidate removed"}`, "")
		kept := createInboxItemForTest(t, router, `{"title":"Candidate kept"}`, "")
		cutoff := formatInboxTimestamp(now)
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff))
		var original aiActionPreview
		if err := json.Unmarshal([]byte(row.PreviewJSON), &original); err != nil || original.InboxReadAll == nil {
			t.Fatal(err)
		}
		read := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+removed.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"1"`})
		if read.Code != http.StatusOK {
			t.Fatal(read.Body.String())
		}
		replacement := aiInboxReadAllTestItem("Replacement candidate", now)
		if err := store.DB.Create(&replacement).Error; err != nil {
			t.Fatal(err)
		}
		current, err := loadAIInboxReadAllPreview(store.DB, cutoff)
		if err != nil || current.CandidateCount != original.InboxReadAll.CandidateCount || current.SelectionFingerprint == original.InboxReadAll.SelectionFingerprint {
			t.Fatalf("replacement snapshot=%+v original=%+v err=%v", current, original.InboxReadAll, err)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		assertAPIError(t, response, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
		if readInboxItemReadAt(t, store, kept.ID) != nil || readInboxItemReadAt(t, store, replacement.ID) != nil {
			t.Fatal("changed candidate set was partially confirmed")
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	})

	t.Run("candidate version", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t, now)
		changed := createInboxItemForTest(t, router, `{"title":"Candidate old title"}`, "")
		other := createInboxItemForTest(t, router, `{"title":"Candidate unchanged"}`, "")
		cutoff := formatInboxTimestamp(now)
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff))
		var original aiActionPreview
		if err := json.Unmarshal([]byte(row.PreviewJSON), &original); err != nil || original.InboxReadAll == nil {
			t.Fatal(err)
		}
		updated := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+changed.ID, []byte(`{"title":"Candidate new title"}`), map[string]string{"If-Match": `"1"`})
		if updated.Code != http.StatusOK {
			t.Fatal(updated.Body.String())
		}
		current, err := loadAIInboxReadAllPreview(store.DB, cutoff)
		if err != nil || current.CandidateCount != original.InboxReadAll.CandidateCount || current.SelectionFingerprint == original.InboxReadAll.SelectionFingerprint {
			t.Fatalf("version snapshot=%+v original=%+v err=%v", current, original.InboxReadAll, err)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		assertAPIError(t, response, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
		if readInboxItemReadAt(t, store, changed.ID) != nil || readInboxItemReadAt(t, store, other.ID) != nil {
			t.Fatal("changed candidate version was partially confirmed")
		}
	})
}

func TestAIInboxReadAllConfirmationRollsBackWholeBatch(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 789, time.UTC)
	router, store, _, tool, generation := aiActionTestFixture(t, now)
	first := createInboxItemForTest(t, router, `{"title":"Rollback first"}`, "")
	second := createInboxItemForTest(t, router, `{"title":"Rollback second"}`, "")
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, formatInboxTimestamp(now)))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_ai_inbox_read_all_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type='inbox_item' AND NEW.action='read' AND NEW.aggregate_id='%s'
		BEGIN SELECT RAISE(ABORT,'forced AI read-all failure'); END
	`, second.ID)).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("rollback response=%d %s", response.Code, response.Body.String())
	}
	if readInboxItemReadAt(t, store, first.ID) != nil || readInboxItemReadAt(t, store, second.ID) != nil {
		t.Fatal("failed AI read_all retained a partial batch")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id IN (?, ?) AND version=1", 2, first.ID, second.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='read'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_ai_inbox_read_all_event").Error; err != nil {
		t.Fatal(err)
	}
}
