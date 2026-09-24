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

func TestAIClientFollowupLifecycleAtomicity(t *testing.T) {
	for _, scenario := range []string{"complete", "complete-next", "skip", "reschedule", "reschedule-audit", "reschedule-approval-audit", "next-audit", "approval-audit", "next-actor-changed", "inactive-next", "inactive-complete", "reject"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, service, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "actions", "clients")
			client := createClientForTest(t, router, `{"name":"Customer"}`, nil)
			actor := createActorForTest(t, router, `{"type":"person","display_name":"Current owner"}`, nil)
			nextActor := createActorForTest(t, router, `{"type":"person","display_name":"Next owner"}`, nil)
			r := performRequest(router, "POST", "/api/v1/client-followups", []byte(fmt.Sprintf(`{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-17T12:00:00Z","timezone":"UTC","channel":"phone","purpose":"Original"}`, client.ID, actor.ID)), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			original := decodeClientFollowupResponse(t, r.Body.Bytes())
			if err := service.projectDueClientFollowups(context.Background()); err != nil {
				t.Fatal(err)
			}
			nextPlan := fmt.Sprintf(`{"assigned_actor_id":%q,"scheduled_at":"2026-10-02T09:00:00+08:00","timezone":"Asia/Shanghai","channel":"meeting","purpose":"Next plan","notes":"Bring draft"}`, nextActor.ID)
			action, changes := "complete", `{"result":"Customer confirmed the draft","completed_at":"2026-09-18T08:00:00+08:00","next_step":"Prepare proposal"}`
			if scenario != "complete" && scenario != "inactive-complete" {
				changes = strings.TrimSuffix(changes, "}") + `,"next_followup":` + nextPlan + `}`
			}
			if scenario == "skip" {
				action, changes = "skip", `{"reason":"Customer requested no call today"}`
			}
			if strings.HasPrefix(scenario, "reschedule") {
				action, changes = "reschedule", strings.TrimSuffix(nextPlan, "}")+`,"reason":"Customer changed availability"}`
			}
			if scenario == "inactive-complete" {
				if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"status": "inactive", "version": original.ClientVersion + 1}).Error; err != nil {
					t.Fatal(err)
				}
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_followup.%s","client_followup_id":%q,"expected_version":1,"changes":%s}`, action, original.ID, changes))
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='planned'", 1)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			if action == "complete" {
				for _, consent := range []string{"", `,"confirm_followup_completed":false`} {
					r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, row.Fingerprint, consent)), nil)
					assertAPIError(t, r, 422, "CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED")
				}
			} else {
				r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_followup_completed":true}`, row.Fingerprint)), nil)
				assertAPIError(t, r, 422, "AI_ACTION_DECISION_INVALID")
			}
			var err error
			switch scenario {
			case "next-audit", "approval-audit", "reschedule-audit", "reschedule-approval-audit":
				event := "client_followup_created_from_completion"
				if scenario == "reschedule-audit" {
					event = "client_followup_reschedule_created"
				}
				if strings.Contains(scenario, "approval-audit") {
					event = "ai_workspace_action_confirmed"
				}
				err = store.DB.Exec("CREATE TRIGGER reject_lifecycle BEFORE INSERT ON workflow_events WHEN NEW.action='" + event + "' BEGIN SELECT RAISE(ABORT,'test'); END").Error
			case "next-actor-changed":
				err = store.DB.Model(&models.Actor{}).Where("id=?", nextActor.ID).Updates(map[string]any{"display_name": "Different name", "version": 2}).Error
			case "inactive-next":
				err = store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"status": "inactive", "version": original.ClientVersion + 1}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			decision, consent := "confirm", ""
			if action == "complete" {
				consent = `,"confirm_followup_completed":true`
			}
			if scenario == "reject" {
				decision, consent = "reject", ""
			}
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q%s}`, row.Fingerprint, decision, consent))
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
			success := true
			switch scenario {
			case "next-audit", "approval-audit", "reschedule-audit", "reschedule-approval-audit":
				success = false
				if r.Code != 500 {
					t.Fatal(r.Body.String())
				}
			case "next-actor-changed":
				success = false
				assertAPIError(t, r, 409, "AI_ACTION_PREVIEW_CHANGED")
			case "inactive-next":
				success = false
				assertAPIError(t, r, 409, "CLIENT_FOLLOWUP_CLIENT_INACTIVE")
			default:
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				replay := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
				if replay.Code != 200 || replay.Body.String() != r.Body.String() {
					t.Fatal("non-idempotent lifecycle replay", replay.Body.String())
				}
			}
			var current models.ClientFollowup
			if err := store.DB.First(&current, "id=?", original.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !success || scenario == "reject" {
				if current.Status != "planned" || current.Version != 1 {
					t.Fatal(current)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE status='open'", 1)
			} else {
				want := map[string]string{"complete": "completed", "skip": "skipped", "reschedule": "cancelled"}[action]
				assertAIClientRecordResponseRoute(t, r.Body.Bytes(), client.ID, "followup")
				if current.Status != want || current.Version != 2 {
					t.Fatal(current)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE status='open'", 0)
				if scenario == "complete-next" || scenario == "reschedule" {
					var next models.ClientFollowup
					if err := store.DB.First(&next, "id != ?", original.ID).Error; err != nil {
						t.Fatal(err)
					}
					if next.Status != "planned" || next.AssignedActorID != nextActor.ID || next.ScheduledAt != "2026-10-02T01:00:00Z" || !strings.Contains(r.Body.String(), `"result_id":"`+next.ID+`"`) {
						t.Fatal(next, r.Body.String())
					}
					if scenario == "reschedule" && (next.RescheduledFromID == nil || *next.RescheduledFromID != original.ID) {
						t.Fatal(next)
					}
					assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 2)
				} else {
					assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 1)
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
		})
	}
}

func TestAIClientFollowupLifecycleStrictFields(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	plan := fmt.Sprintf(`{"assigned_actor_id":%q,"scheduled_at":"2026-10-02T01:00:00Z","timezone":"UTC","channel":"phone","purpose":"Next","notes":null,"priority":"normal"}`, id)
	completion := `{"result":"Actual result","completed_at":"2026-09-18T08:00:00+08:00"}`
	for _, action := range []string{"complete", "skip", "reschedule"} {
		changes := completion
		if action == "skip" {
			changes = `{"reason":"No need today"}`
		}
		if action == "reschedule" {
			changes = strings.TrimSuffix(plan, "}") + `,"reason":"Another day"}`
		}
		input := fmt.Sprintf(`{"action":"client_followup.%s","client_followup_id":%q,"expected_version":1,"changes":%s}`, action, id, changes)
		parsed, err := parseAIWorkspaceAction([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(parsed)
		second, err := parseAIWorkspaceAction(encoded)
		if err != nil || string(parsed.Changes) != string(second.Changes) {
			t.Fatal("unstable canonical action", err)
		}
	}
	for _, extra := range []string{`"confirm_followup_completed":true`, `"next_followup":null`, `"next_followup":{}`, `"client_id":"` + id + `"`, `"status":"completed"`, `"scheduled_at":"2026-10-01T00:00:00Z"`, `"result":null`, `"completed_at":null`} {
		changes := strings.TrimSuffix(completion, "}") + "," + extra + "}"
		if _, err := parseAIWorkspaceAction([]byte(fmt.Sprintf(`{"action":"client_followup.complete","client_followup_id":%q,"expected_version":1,"changes":%s}`, id, changes))); err == nil {
			t.Fatal("invalid completion accepted", extra)
		}
	}
	for _, next := range []string{
		strings.Replace(plan, `"UTC"`, `"Local"`, 1), strings.Replace(plan, `"UTC"`, `""`, 1),
		strings.Replace(plan, id, strings.ToUpper(id), 1), strings.Replace(plan, `"normal"`, `"P1"`, 1),
		strings.TrimSuffix(plan, "}") + `,"client_id":"` + id + `"}`, strings.TrimSuffix(plan, "}") + `,"result":"Invented"}`,
		strings.Replace(plan, `"scheduled_at":"2026-10-02T01:00:00Z",`, "", 1),
	} {
		input := fmt.Sprintf(`{"action":"client_followup.complete","client_followup_id":%q,"expected_version":1,"changes":{"result":"Actual","completed_at":"2026-09-18T00:00:00Z","next_followup":%s}}`, id, next)
		if _, err := parseAIWorkspaceAction([]byte(input)); err == nil {
			t.Fatal("invalid nested plan accepted", next)
		}
	}
}

func TestAIClientFollowupCompletionHarnessAndNextReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	client := createClientForTest(t, router, `{"name":"Harness customer"}`, nil)
	r := performRequest(router, "POST", "/api/v1/client-followups", []byte(fmt.Sprintf(`{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-17T12:00:00Z","timezone":"UTC","channel":"phone","purpose":"Review draft"}`, client.ID, models.BuiltinOwnerActorID)), nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	original := decodeClientFollowupResponse(t, r.Body.Bytes())
	calls, nextID := 0, ""
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls <= 2 {
			name, args := "workspace_client_records", fmt.Sprintf(`{"type":"followup","view":"detail","id":%q}`, original.ID)
			if calls == 2 {
				if !strings.Contains(string(encoded), original.ID) || !strings.Contains(string(encoded), `\"version\":1`) {
					t.Error("missing real followup detail")
				}
				name, args = "workspace_propose", fmt.Sprintf(`{"action":"client_followup.complete","client_followup_id":%q,"expected_version":1,"changes":{"result":"Customer approved draft","completed_at":"2026-09-18T11:00:00Z","next_followup":{"assigned_actor_id":%q,"scheduled_at":"2026-10-01T00:00:00Z","timezone":"UTC","channel":"meeting","purpose":"Review final"}}}`, original.ID, models.BuiltinOwnerActorID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("complete-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing pending receipt")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), `\"result_id\":\"`+nextID+`\"`) || !strings.Contains(string(encoded), original.ID) || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing compound result receipt")
			}
			tools, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(tools) {
				t.Error("inherited grant")
			}
		}
		streamMockAIDelta(w, `请查看回访操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "followup-completion", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "客户已在11点确认草稿，请记录并续排", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "clients", "actions"}}})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='planned'", 1)
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_followup_completed":true}`, proposal.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var next models.ClientFollowup
	if err := store.DB.First(&next, "id != ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	nextID = next.ID
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才执行了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='completed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE status='planned'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
}
