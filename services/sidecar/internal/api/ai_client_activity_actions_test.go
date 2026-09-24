package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIClientActivityHarnessAndNextReceipt(t *testing.T) {
	for _, action := range []string{"create", "update", "delete"} {
		t.Run(action, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
			client := createClientForTest(t, router, `{"name":"Actual customer"}`, nil)
			r := performRequest(router, "POST", "/api/v1/clients/"+client.ID+"/activities", []byte(`{"kind":"note","title":"Actual meeting","body":"User reported original meeting","occurred_at":"2026-09-17T00:00:00Z"}`), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			original := decodeClientActivityResponse(t, r.Body.Bytes())
			calls, resultID := 0, ""
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				encoded, _ := json.Marshal(body)
				if calls <= 2 {
					name, args := "workspace_client_records", fmt.Sprintf(`{"type":"activity","view":"detail","id":%q}`, original.ID)
					if calls == 2 {
						if !strings.Contains(string(encoded), "User reported original meeting") {
							t.Error("real activity text not read")
						}
						name = "workspace_propose"
						switch action {
						case "create":
							args = fmt.Sprintf(`{"action":"client_activity.create","changes":{"client_id":%q,"kind":"meeting","title":"New user-reported meeting","body":"User supplied actual meeting record","occurred_at":"2026-09-18T08:00:00Z"}}`, client.ID)
						case "update":
							args = fmt.Sprintf(`{"action":"client_activity.update","client_activity_id":%q,"expected_version":1,"changes":{"body":"User supplied corrected full body"}}`, original.ID)
						case "delete":
							args = fmt.Sprintf(`{"action":"client_activity.delete","client_activity_id":%q,"expected_version":1,"changes":{"reason":"Duplicate meeting"}}`, original.ID)
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("activity-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\n", frame)
					return
				}
				if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
					t.Error("missing pending receipt")
				}
				if calls == 4 {
					if !strings.Contains(string(encoded), `\"result_id\":\"`+resultID+`\"`) || !strings.Contains(string(encoded), `\"confirmed\"`) {
						t.Error("missing execution receipt")
					}
					registered, _ := json.Marshal(body["tools"])
					if aiHasProtectedWorkspaceTool(registered) {
						t.Error("grant inherited")
					}
					if strings.Contains(string(encoded), "User supplied corrected full body") || strings.Contains(string(encoded), "User supplied actual meeting record") {
						t.Error("approval body leaked into next-turn receipt")
					}
				}
				streamMockAIDelta(w, `请核对活动确认卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "activity-"+action, upstream.URL+"/v1", "model")
			var current models.AIProvider
			if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "按我提供的真实信息处理活动", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "clients", "actions"}}})
			r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
				t.Fatal(r.Body.String(), calls)
			}
			var unchanged models.ClientActivity
			if err := store.DB.First(&unchanged, "id=?", original.ID).Error; err != nil || unchanged.Version != 1 {
				t.Fatal(unchanged, err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 1)
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			consent := ""
			if action == "delete" {
				consent = `,"confirm_activity_delete":true`
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, proposal.Fingerprint, consent)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			if err := store.DB.First(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil {
				t.Fatal(err)
			}
			resultID = *proposal.ResultID
			assertAIClientRecordResponseRoute(t, r.Body.Bytes(), client.ID, "activity")
			var result models.ClientActivity
			if err := store.DB.First(&result, "id=?", resultID).Error; err != nil {
				t.Fatal(err)
			}
			switch action {
			case "create":
				if result.Title != "New user-reported meeting" || result.Version != 1 {
					t.Fatal(result)
				}
			case "update":
				if result.Body == nil || *result.Body != "User supplied corrected full body" || result.Version != 2 {
					t.Fatal(result)
				}
			case "delete":
				if result.DeletedAt == nil || result.Version != 2 {
					t.Fatal(result)
				}
			}
			var session models.AISession
			if err := store.DB.First(&session).Error; err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才执行了吗"})
			r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 4 {
				t.Fatal(r.Body.String(), calls)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
		})
	}
}

func TestAIClientActivityStrictInputsAndReadOnlySources(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "clients", "actions")
	client := createClientForTest(t, router, `{"name":"Read only sources"}`, nil)
	source, sourceID := "project", uuid.NewString()
	item := models.ClientActivity{ID: uuid.NewString(), ClientID: client.ID, Kind: "system_reference", Title: "System event",
		OccurredAt: "2026-09-17T00:00:00Z", CreatedByActorID: models.BuiltinSystemActorID, SourceType: &source, SourceID: &sourceID, Version: 1,
		CreatedAt: "2026-09-17T00:00:00Z", UpdatedAt: "2026-09-17T00:00:00Z"}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"update", "delete"} {
		changes := `{"title":"Fake edit"}`
		if action == "delete" {
			changes = `{"reason":"Attempted deletion"}`
		}
		_, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"client_activity.%s","client_activity_id":%q,"expected_version":1,"changes":%s}`, action, item.ID, changes)))
		if err == nil || !strings.Contains(err.Error(), "CLIENT_ACTIVITY_READ_ONLY") {
			t.Fatal(action, err)
		}
	}
	base := map[string]any{"action": "client_activity.create", "changes": map[string]any{"client_id": client.ID, "kind": "note", "title": "Actual", "body": "Actual body", "occurred_at": "2026-09-17T00:00:00Z"}}
	for _, field := range []string{"confirm_activity_delete", "created_by_actor_id", "source_type", "source_id", "client_activity_id", "task_id", "expected_version"} {
		for _, root := range []bool{true, false} {
			args := map[string]any{}
			raw, _ := json.Marshal(base)
			_ = json.Unmarshal(raw, &args)
			if root {
				args[field] = item.ID
			} else {
				args["changes"].(map[string]any)[field] = item.ID
			}
			raw, _ = json.Marshal(args)
			if _, err := parseAIWorkspaceAction(raw); err == nil {
				t.Fatal("accepted", field, root)
			}
		}
	}
	for key, value := range map[string]any{"body": nil, "kind": "system_reference", "occurred_at": "2026-09-18", "client_id": "fake", "title": ""} {
		args := map[string]any{}
		raw, _ := json.Marshal(base)
		_ = json.Unmarshal(raw, &args)
		args["changes"].(map[string]any)[key] = value
		raw, _ = json.Marshal(args)
		if _, err := parseAIWorkspaceAction(raw); err == nil {
			t.Fatal("accepted", key)
		}
	}
	base["changes"].(map[string]any)["occurred_at"] = "2099-01-01T00:00:00Z"
	raw, _ := json.Marshal(base)
	if _, err := tool.Execute(context.Background(), raw); err == nil {
		t.Fatal("accepted future event")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIClientActivityCreateAndLongBody(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Activity customer"}`, nil)
	body := strings.Repeat("<", 10000)
	args, _ := json.Marshal(map[string]any{"action": "client_activity.create", "changes": map[string]any{"client_id": client.ID, "kind": "note", "title": " Actual note ", "body": body, "occurred_at": "2026-09-18T08:00:00+08:00"}})
	if _, err := tool.Execute(context.Background(), args); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal("missing clients permission", err)
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "clients", "actions")
	row := proposeTestAction(t, store, tool, string(args))
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil || preview.After["body"] != body {
		t.Fatal("creation preview lost full body", err)
	}
	if duplicate := proposeTestAction(t, store, tool, string(args)); duplicate.ID != row.ID {
		t.Fatal("duplicate proposal")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if r.Code != 200 || !strings.Contains(r.Body.String(), "/clients/"+client.ID) {
			t.Fatal(r.Body.String())
		}
	}
	var item models.ClientActivity
	if err := store.DB.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Body == nil || *item.Body != body || item.Title != "Actual note" || item.OccurredAt != "2026-09-18T00:00:00Z" || item.CreatedByActorID != models.BuiltinOwnerActorID || item.SourceType != nil {
		t.Fatal(item)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIClientActivityCreateApprovalFailureRollsBackClientFacts(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "clients", "actions")
	client := createClientForTest(t, router, `{"name":"Rollback customer"}`, nil)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_activity.create","changes":{"client_id":%q,"kind":"note","title":"Actual","body":"User supplied","occurred_at":"2026-09-17T00:00:00Z"}}`, client.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec("CREATE TRIGGER fail_activity_create_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
		t.Fatal(err)
	}
	r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 500 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
	var current models.Client
	if err := store.DB.First(&current, "id=?", client.ID).Error; err != nil || current.Version != client.Version {
		t.Fatal(current.Version, client.Version, err)
	}
	var pending models.AIActionProposal
	if err := store.DB.First(&pending, "id=?", row.ID).Error; err != nil || pending.Status != "pending" {
		t.Fatal(pending.Status, err)
	}
}

func TestAIClientActivityUpdateDeleteAndRollback(t *testing.T) {
	for _, scenario := range []string{"update", "delete", "delete-reject", "update-audit", "delete-audit", "renamed", "version", "deleted", "long-body"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work", "clients", "actions")
			client := createClientForTest(t, router, `{"name":"Original customer","email":"private@example.com"}`, nil)
			createBody := fmt.Sprintf(`{"kind":"note","title":"Original","body":%q,"occurred_at":"2026-09-17T00:00:00Z"}`, strings.Repeat("<", 10000))
			r := performRequest(router, "POST", "/api/v1/clients/"+client.ID+"/activities", []byte(createBody), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			item := decodeClientActivityResponse(t, r.Body.Bytes())
			action, changes := "update", `{"title":"Corrected","kind":"meeting"}`
			deleting := scenario == "delete" || scenario == "delete-reject" || scenario == "delete-audit"
			if deleting {
				action, changes = "delete", `{"reason":"Duplicate record"}`
			}
			if scenario == "long-body" {
				encoded, _ := json.Marshal(map[string]any{"body": strings.Repeat(">", 10000)})
				changes = string(encoded)
			}
			if scenario == "deleted" {
				r = performRequest(router, "DELETE", "/api/v1/client-activities/"+item.ID+"?confirm=true", []byte(`{"reason":"Already removed"}`), map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				_, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"client_activity.update","client_activity_id":%q,"expected_version":2,"changes":%s}`, item.ID, changes)))
				if err == nil || !strings.Contains(err.Error(), "CLIENT_ACTIVITY_DELETED") {
					t.Fatal(err)
				}
				return
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_activity.%s","client_activity_id":%q,"expected_version":1,"changes":%s}`, action, item.ID, changes))
			if scenario == "long-body" {
				var preview aiActionPreview
				if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil || preview.Before["body"] != *item.Body || preview.After["body"] != strings.Repeat(">", 10000) {
					t.Fatal("replacement preview truncated", err)
				}
			}
			if strings.Contains(row.PreviewJSON, "private@example.com") || (scenario != "long-body" && strings.Contains(row.PreviewJSON, `"body"`)) {
				t.Fatal("unrelated private text in preview")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			if deleting {
				r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
				assertAPIError(t, r, 422, "CLIENT_ACTIVITY_DELETE_CONFIRMATION_REQUIRED")
				for _, invalid := range []string{
					`"decision":"confirm","confirm_activity_delete":false`,
					`"decision":"reject","confirm_activity_delete":true`,
				} {
					r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,%s}`, row.Fingerprint, invalid)), nil)
					if r.Code != 422 {
						t.Fatal("invalid deletion consent accepted", r.Body.String())
					}
				}
			} else {
				r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_activity_delete":true}`, row.Fingerprint)), nil)
				assertAPIError(t, r, 422, "AI_ACTION_DECISION_INVALID")
			}
			var err error
			switch scenario {
			case "update-audit", "delete-audit":
				err = store.DB.Exec("CREATE TRIGGER fail_activity_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error
			case "renamed":
				err = store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"name": "Renamed", "version": item.ClientVersion + 1}).Error
			case "version":
				err = store.DB.Model(&models.ClientActivity{}).Where("id=?", item.ID).Updates(map[string]any{"title": "Human edit", "version": 2}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			decision, consent := "confirm", ""
			if deleting {
				consent = `,"confirm_activity_delete":true`
			}
			if scenario == "delete-reject" {
				decision, consent = "reject", ""
			}
			request := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q%s}`, row.Fingerprint, decision, consent))
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", request, nil)
			switch scenario {
			case "update-audit", "delete-audit":
				if r.Code != 500 {
					t.Fatal(r.Body.String())
				}
			case "renamed":
				assertAPIError(t, r, 409, "AI_ACTION_PREVIEW_CHANGED")
			case "version":
				assertAPIError(t, r, 409, "VERSION_CONFLICT")
			default:
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				replay := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", request, nil)
				if replay.Code != 200 || replay.Body.String() != r.Body.String() {
					t.Fatal("replay changed")
				}
			}
			var got models.ClientActivity
			if err := store.DB.First(&got, "id=?", item.ID).Error; err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "update":
				if got.Title != "Corrected" || got.Kind != "meeting" || got.Version != 2 || *got.Body != *item.Body {
					t.Fatal(got)
				}
			case "delete":
				if got.DeletedAt == nil || got.DeletedByActorID == nil || *got.DeletedByActorID != models.BuiltinOwnerActorID || *got.DeleteReason != "Duplicate record" || got.Version != 2 || got.Body == nil || *got.Body != *item.Body {
					t.Fatal(got)
				}
			case "long-body":
				if got.Body == nil || *got.Body != strings.Repeat(">", 10000) {
					t.Fatal("truncated replacement")
				}
			case "version":
				if got.Title != "Human edit" || got.Version != 2 {
					t.Fatal(got)
				}
			default:
				if got.Version != 1 || got.DeletedAt != nil || got.Title != "Original" {
					t.Fatal(got)
				}
			}
			if strings.HasSuffix(scenario, "-audit") {
				var currentClient models.Client
				if err := store.DB.First(&currentClient, "id=?", client.ID).Error; err != nil || currentClient.Version != item.ClientVersion {
					t.Fatal("client aggregate escaped rollback", currentClient.Version, item.ClientVersion, err)
				}
				var pending models.AIActionProposal
				if err := store.DB.First(&pending, "id=?", row.ID).Error; err != nil || pending.Status != "pending" {
					t.Fatal(pending.Status, err)
				}
			}
		})
	}
}
