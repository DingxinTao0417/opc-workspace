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

func TestAIAutomationEnableRequiresHumanFutureEffectsConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	id := "00000000-0000-5000-8000-000000000102"
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"automation.enable","automation_rule_id":%q,"expected_version":1,"changes":{}}`, id))
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "id=?", id).Error; err != nil || rule.Enabled {
		t.Fatal(rule, err)
	}
	if !strings.Contains(row.PreviewJSON, "permission_summary") {
		t.Fatal("missing persistent effects")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	assertAPIError(t, r, 422, "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED")
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_effects":true}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.First(&rule, "id=?", id).Error; err != nil || !rule.Enabled || rule.Version != 2 || rule.NextRunAt == nil {
		t.Fatal(rule, err)
	}
	// Proposal execution saves the rule, never runs it immediately.
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
}

func TestAIAutomationReadOnlyToolsExist(t *testing.T) {
	_, _, _, tool, _ := aiActionTestFixture(t)
	reader := *tool.(*aiWorkspaceTool)
	reader.name = "workspace_automations"
	result, err := reader.Execute(context.Background(), []byte(`{"view":"rules"}`))
	if err != nil || !strings.Contains(result, "daily-today-reminder") {
		t.Fatal(result, err)
	}
}

func TestAIAutomationStrictInputsAndGrants(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	id := "00000000-0000-5000-8000-000000000102"
	base := fmt.Sprintf(`{"action":"automation.update","automation_rule_id":%q,"expected_version":1,"changes":{"local_time":"10:30","timezone":"Asia/Shanghai"}}`, id)
	for _, raw := range []string{
		strings.Replace(base, `"timezone":"Asia/Shanghai"`, `"timezone":"Local"`, 1),
		strings.Replace(base, `"timezone":"Asia/Shanghai"`, `"timezone":""`, 1),
		strings.Replace(base, `,"timezone":"Asia/Shanghai"`, "", 1),
		strings.Replace(base, `"10:30"`, `"25:00"`, 1),
		strings.Replace(base, `"expected_version":1`, `"expected_version":0`, 1),
		strings.Replace(base, `"changes":`, `"confirm_automation_effects":true,"changes":`, 1),
		strings.Replace(base, `"changes":`, `"task_id":null,"changes":`, 1),
		strings.Replace(base, `"local_time":"10:30"`, `"permissions":[]`, 1),
		strings.Replace(base, "automation.update", "automation.enable", 1),
		strings.Replace(base, "automation.update", "automation.retry", 1),
		strings.Replace(base, id, uuid.NewString(), 1),
	} {
		if _, err := tool.Execute(context.Background(), []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	reader := *tool.(*aiWorkspaceTool)
	reader.name = "workspace_automations"
	for _, raw := range []string{`{}`, `{"view":"rules","limit":1}`, `{"view":"rule"}`, `{"view":"run","id":"bad"}`, `{"view":"runs","id":null}`, `{"view":"runs","rule_id":""}`, `{"view":"runs","status":"running"}`, `{"view":"runs","status":""}`, `{"view":"runs","limit":21}`, `{"view":"runs","offset":1001}`, `{"view":"runs","limit":null}`, `{"view":"rules","raw":true}`} {
		if _, err := reader.Execute(context.Background(), []byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	reader.policy = harness.NewCapabilities("clients")
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"rules"}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work")
	if _, err := tool.Execute(context.Background(), []byte(base)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIAgentFailureAutomationEnableRequiresHumanConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	id := "00000000-0000-5000-8000-000000000105"
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"automation.enable","automation_rule_id":%q,"expected_version":1,"changes":{}}`, id))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_rules WHERE id=? AND enabled=0 AND version=1", 1, id)
	finishAIGeneration(t, store, generation)
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	assertAPIError(t, response, 422, "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED")
	for range 2 {
		response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_effects":true}`, row.Fingerprint)), nil)
		if response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_rules WHERE id=? AND enabled=1 AND version=2 AND next_run_at IS NULL", 1, id)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='automation_rule_enabled'", 1, id)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
}

func TestAIAutomationConfigurationDecisionAndRollback(t *testing.T) {
	for _, scenario := range []string{"disabled-update", "enabled-update", "disable", "noop", "reject", "version", "audit", "catalog", "schedule-moved", "event-update"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, api, tool, generation := aiActionTestFixture(t)
			id := "00000000-0000-5000-8000-000000000102"
			version := int64(1)
			enabled := scenario != "disabled-update" && scenario != "event-update"
			if scenario == "event-update" {
				id = "00000000-0000-5000-8000-000000000101"
			}
			if enabled {
				r := performRequest(router, "POST", "/api/v1/automations/rules/"+id+"/enable", nil, map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				version = 2
			}
			var original models.AutomationRule
			if err := store.DB.First(&original, "id=?", id).Error; err != nil {
				t.Fatal(err)
			}
			action, changes := "update", `{"local_time":"10:30","timezone":"Asia/Shanghai"}`
			if scenario == "event-update" {
				changes = `{"priority":"P2"}`
			}
			if scenario == "disable" {
				action, changes = "disable", `{}`
			}
			if scenario == "noop" {
				changes = original.ConfigJSON
			}
			args := fmt.Sprintf(`{"action":"automation.%s","automation_rule_id":%q,"expected_version":%d,"changes":%s}`, action, id, version, changes)
			row := proposeTestAction(t, store, tool, args)
			if duplicate := proposeTestAction(t, store, tool, args); duplicate.ID != row.ID {
				t.Fatal("duplicate proposal")
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			consent := ""
			if enabled && action == "update" {
				r := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_effects":false}`, row.Fingerprint)), nil)
				assertAPIError(t, r, 422, "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED")
				consent = `,"confirm_automation_effects":true`
			} else {
				r := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_effects":true}`, row.Fingerprint)), nil)
				assertAPIError(t, r, 422, "AI_ACTION_DECISION_INVALID")
			}
			decision := "confirm"
			switch scenario {
			case "reject":
				decision, consent = "reject", ""
			case "version":
				r := performRequest(router, "PATCH", "/api/v1/automations/rules/"+id, []byte(`{"config":{"local_time":"11:00","timezone":"UTC"}}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "audit":
				if err := store.DB.Exec("CREATE TRIGGER fail_automation_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END").Error; err != nil {
					t.Fatal(err)
				}
			case "catalog":
				previous := automationPresets[1].ActionLabel
				automationPresets[1].ActionLabel = "Changed action"
				defer func() { automationPresets[1].ActionLabel = previous }()
			case "schedule-moved":
				// Scheduler may advance the next window without changing editable version.
				if err := store.DB.Model(&original).Update("next_run_at", "2026-09-25T09:00:00.000000000Z").Error; err != nil {
					t.Fatal(err)
				}
			}
			r := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q%s}`, row.Fingerprint, decision, consent)), nil)
			if scenario == "version" {
				assertAPIError(t, r, 409, "VERSION_CONFLICT")
				return
			}
			if scenario == "catalog" {
				assertAPIError(t, r, 409, "AI_ACTION_PREVIEW_CHANGED")
				return
			}
			var current models.AutomationRule
			if err := store.DB.First(&current, "id=?", id).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "audit" || scenario == "reject" {
				if scenario == "audit" && r.Code != 500 || scenario == "reject" && r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				if current.ConfigJSON != original.ConfigJSON || current.Enabled != original.Enabled || current.Version != original.Version {
					t.Fatal("partial rule mutation", current)
				}
				var proposal models.AIActionProposal
				if err := store.DB.First(&proposal, "id=?", row.ID).Error; err != nil {
					t.Fatal(err)
				}
				if scenario == "audit" && proposal.Status != "pending" {
					t.Fatal(proposal.Status)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='automation_rule_updated'", 0)
				return
			}
			if r.Code != 200 || strings.Contains(r.Body.String(), `"route":"`+id) {
				t.Fatal(r.Body.String())
			}
			expected := version + 1
			if scenario == "noop" {
				expected = version
				if *current.NextRunAt != *original.NextRunAt {
					t.Fatal("no-op rescheduled")
				}
			}
			if current.Version != expected || (action == "disable" && (current.Enabled || current.NextRunAt != nil)) {
				t.Fatal(current)
			}
			if action == "update" && scenario != "noop" {
				config, err := decodeAutomationConfig(current.PresetKey, current.ConfigJSON)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "event-update" {
					if config.Priority != "P2" {
						t.Fatal(config)
					}
				} else {
					if config.LocalTime != "10:30" || config.Timezone != "Asia/Shanghai" {
						t.Fatal(config)
					}
					if enabled {
						next, err := nextAutomationSchedule(current.PresetKey, config, api.options.Now())
						if err != nil || current.NextRunAt == nil || *current.NextRunAt != formatInboxTimestamp(next) {
							t.Fatal(current, err)
						}
					}
				}
			}
			// A confirmed retry needs no new consent and cannot execute twice.
			r = performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
		})
	}
}

func TestAIAutomationRunReadPrivacyPagingAndFailures(t *testing.T) {
	_, store, api, tool, _ := aiActionTestFixture(t)
	reader := *tool.(*aiWorkspaceTool)
	reader.name = "workspace_automations"
	id := "00000000-0000-5000-8000-000000000102"
	var rows []models.AutomationRun
	for i := 0; i < 3; i++ {
		stamp := formatInboxTimestamp(api.options.Now().Add(time.Duration(i) * time.Second))
		row := models.AutomationRun{ID: uuid.NewString(), RuleID: id, RuleVersion: 1, TriggerType: "schedule", ScheduledFor: &stamp, LogicalKey: fmt.Sprint("private-source-", i), DedupeKey: fmt.Sprint("private-dedupe-", i), Status: "failed", Attempt: 1, Retryable: true, ErrorCode: stringPointer("sensitive raw error"), ConfigSnapshotJSON: `{"local_time":"09:00","timezone":"UTC"}`, ActionSnapshotJSON: `{"customer":"private-business-body","amount":9000}`, ResultSummary: "private-result-summary", StartedAt: stamp, EndedAt: stamp}
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	read := func(raw string) map[string]any {
		t.Helper()
		encoded, err := reader.Execute(context.Background(), []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"private-", "sensitive raw error", "action_snapshot", "config_snapshot", "logical_key", "dedupe_key", "source_event_id", "result_summary"} {
			if strings.Contains(encoded, secret) {
				t.Fatalf("leaked %s: %s", secret, encoded)
			}
		}
		var out map[string]any
		if err = json.Unmarshal([]byte(encoded), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for offset := 0; offset < 3; offset++ {
		out := read(fmt.Sprintf(`{"view":"runs","rule_id":%q,"status":"failed","limit":1,"offset":%d}`, id, offset))
		items := out["items"].([]any)
		if items[0].(map[string]any)["route"] != aiAutomationRoute("run", rows[2-offset].ID) {
			t.Fatal("missing exact run location", out)
		}
		if len(items) != 1 || items[0].(map[string]any)["id"] != rows[2-offset].ID || items[0].(map[string]any)["error_code"] != "UNKNOWN_ERROR" || out["has_more"] != (offset < 2) {
			t.Fatal(out)
		}
	}
	run := read(fmt.Sprintf(`{"view":"run","id":%q}`, rows[0].ID))["run"].(map[string]any)
	if run["route"] != aiAutomationRoute("run", rows[0].ID) {
		t.Fatal(run)
	}
	out := read(fmt.Sprintf(`{"view":"rule","id":%q}`, id))
	if out["rule"].(map[string]any)["route"] != aiAutomationRoute("rule", id) {
		t.Fatal(out)
	}
	for _, raw := range read(`{"view":"rules"}`)["items"].([]any) {
		item := raw.(map[string]any)
		if item["route"] != aiAutomationRoute("rule", item["id"].(string)) {
			t.Fatal(item)
		}
	}
	if out["ui_entry"] != "automation_settings" {
		t.Fatal(out)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 3)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_rules WHERE enabled=1", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	api.restorePending.Store(true)
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"rules"}`)); err == nil {
		t.Fatal("read during restore")
	}
	api.restorePending.Store(false)
	if err := store.DB.Exec("DROP TABLE automation_runs").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Execute(context.Background(), []byte(`{"view":"runs"}`)); err == nil || strings.Contains(err.Error(), "no such table") {
		t.Fatal("unsafe storage error", err)
	}
}

func TestAIAutomationHarnessAndNextReceipt(t *testing.T) {
	for _, action := range []string{"enable", "disable", "update"} {
		t.Run(action, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
			id := "00000000-0000-5000-8000-000000000102"
			version := 1
			if action != "enable" {
				r := performRequest(router, "POST", "/api/v1/automations/rules/"+id+"/enable", nil, map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
				version = 2
			}
			calls := 0
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				encoded, _ := json.Marshal(payload)
				if calls <= 2 {
					name, args := "workspace_automations", fmt.Sprintf(`{"view":"rule","id":%q}`, id)
					if calls == 2 {
						if !strings.Contains(string(encoded), "daily-today-reminder") {
							t.Error("rule not read")
						}
						changes := `{}`
						if action == "update" {
							changes = `{"local_time":"11:22","timezone":"Asia/Shanghai"}`
						}
						name, args = "workspace_propose", fmt.Sprintf(`{"action":"automation.%s","automation_rule_id":%q,"expected_version":%d,"changes":%s}`, action, id, version, changes)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("automation-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\n", frame)
					return
				}
				if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
					t.Error("missing pending receipt")
				}
				if calls == 4 {
					if !strings.Contains(string(encoded), `\"result_id\":\"`+id+`\"`) || !strings.Contains(string(encoded), `\"confirmed\"`) {
						t.Error("missing durable receipt")
					}
					registered, _ := json.Marshal(payload["tools"])
					if aiHasProtectedWorkspaceTool(registered) {
						t.Error("grant inherited")
					}
					if strings.Contains(string(encoded), "11:22") || strings.Contains(string(encoded), "Asia/Shanghai") {
						t.Error("config leaked into next receipt")
					}
				}
				streamMockAIDelta(w, `请核对自动化确认卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "automation-"+action, upstream.URL+"/v1", "model")
			var current models.AIProvider
			if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "按我提供的参数处理自动化规则", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
			r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
				t.Fatal(r.Body.String(), calls)
			}
			var rule models.AutomationRule
			if err := store.DB.First(&rule, "id=?", id).Error; err != nil || rule.Version != int64(version) {
				t.Fatal(rule, err)
			}
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			consent := `,"confirm_automation_effects":true`
			if action == "disable" {
				consent = ""
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, proposal.Fingerprint, consent)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			if err := store.DB.First(&rule, "id=?", id).Error; err != nil || rule.Version != int64(version+1) {
				t.Fatal(rule, err)
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
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
		})
	}
}
