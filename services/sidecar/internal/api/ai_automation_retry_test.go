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

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func failAIProjectAutomation(t *testing.T, router *gin.Engine, store *database.Store) models.AutomationRun {
	t.Helper()
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	r := performRequest(router, "POST", "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Exec(`CREATE TRIGGER reject_ai_retry_inbox BEFORE INSERT ON inbox_items WHEN NEW.source_entity_type='automation' BEGIN SELECT RAISE(ABORT, 'TEST_RETRY_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	p := createProjectForTest(t, router, `{"name":"captured-private-name"}`, nil)
	p = transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"start"}`)
	transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"complete"}`)
	var run models.AutomationRun
	if err := store.DB.Where("rule_id=? AND status='failed'", rule.ID).First(&run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAIAutomationRetryDecisionBoundaries(t *testing.T) {
	for _, scenario := range []string{"false-consent", "wrong-consent", "reject-consent", "reject", "rule-changed", "native-race", "audit-rollback", "audit-failed-rollback", "failed-result", "max-attempts"} {
		t.Run(scenario, func(t *testing.T) {
			router, store, api, tool, generation := aiActionTestFixture(t)
			failed := failAIProjectAutomation(t, router, store)
			if scenario != "failed-result" && scenario != "max-attempts" && scenario != "audit-failed-rollback" {
				if err := store.DB.Exec("DROP TRIGGER reject_ai_retry_inbox").Error; err != nil {
					t.Fatal(err)
				}
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"automation.retry","automation_run_id":%q,"expected_version":%d,"changes":{}}`, failed.ID, failed.RuleVersion))
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			decision, flag := "confirm", `,"confirm_automation_retry":true`
			switch scenario {
			case "false-consent":
				flag = `,"confirm_automation_retry":false`
			case "wrong-consent":
				flag = `,"confirm_automation_effects":true`
			case "reject":
				decision, flag = "reject", ""
			case "reject-consent":
				decision = "reject"
			case "rule-changed":
				r := performRequest(router, "POST", "/api/v1/automations/rules/"+failed.RuleID+"/disable", nil, map[string]string{"If-Match": `"2"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "native-race":
				if err := api.retryAutomationRunByID(failed.ID, api.options.Now()); err != nil {
					t.Fatal(err)
				}
			case "audit-rollback", "audit-failed-rollback":
				if err := store.DB.Exec(`CREATE TRIGGER reject_retry_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_APPROVAL'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q%s}`, row.Fingerprint, decision, flag)), nil)
			switch scenario {
			case "false-consent":
				assertAPIError(t, r, 422, "AUTOMATION_RETRY_CONFIRMATION_REQUIRED")
			case "wrong-consent", "reject-consent":
				assertAPIError(t, r, 422, "AI_ACTION_DECISION_INVALID")
			case "rule-changed":
				assertAPIError(t, r, 409, "AI_ACTION_PREVIEW_CHANGED")
			case "native-race":
				assertAPIError(t, r, 409, "AUTOMATION_RETRY_ALREADY_EXISTS")
			case "audit-rollback", "audit-failed-rollback":
				if r.Code != 500 {
					t.Fatal(r.Code, r.Body.String())
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='automation'", 0)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='automation_run_succeeded'", 0)
			case "reject":
				if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"rejected"`) {
					t.Fatal(r.Body.String())
				}
			default:
				if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"confirmed"`) || !strings.Contains(r.Body.String(), `"status":"failed"`) {
					t.Fatal(r.Code, r.Body.String())
				}
				receipt, err := api.aiActionReceipts(context.Background(), generation.SessionID)
				if err != nil || !strings.Contains(receipt, `"status":"failed"`) || strings.Contains(receipt, "captured-private-name") {
					t.Fatal(receipt, err)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='automation'", 0)
				if scenario == "max-attempts" {
					var second models.AutomationRun
					if err := store.DB.Where("retry_of_run_id=?", failed.ID).First(&second).Error; err != nil {
						t.Fatal(err)
					}
					if err := api.retryAutomationRunByID(second.ID, api.options.Now()); err != nil {
						t.Fatal(err)
					}
					var third models.AutomationRun
					if err := store.DB.Where("retry_of_run_id=?", second.ID).First(&third).Error; err != nil {
						t.Fatal(err)
					}
					if third.Attempt != 3 || third.Retryable || third.RetryAt != nil {
						t.Fatal(third)
					}
					if _, err := previewAIAutomationRetry(store.DB, aiWorkspaceAction{AutomationRunID: third.ID, ExpectedVersion: third.RuleVersion}); err == nil {
						t.Fatal("fourth attempt preview allowed")
					}
					return
				}
			}
			count := int64(1)
			if scenario == "native-race" || scenario == "failed-result" {
				count = 2
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", count)
		})
	}
}

func TestAIAutomationRetryStrictInputsAndScheduleGuard(t *testing.T) {
	router, store, api, tool, _ := aiActionTestFixture(t)
	failed := failAIProjectAutomation(t, router, store)
	base := fmt.Sprintf(`{"action":"automation.retry","automation_run_id":%q,"expected_version":%d,"changes":{}}`, failed.ID, failed.RuleVersion)
	for _, raw := range []string{
		strings.Replace(base, `"changes":{}`, `"changes":{"priority":"P0"}`, 1),
		strings.Replace(base, `"changes":{}`, `"changes":null`, 1),
		strings.Replace(base, `"changes":`, `"automation_rule_id":null,"changes":`, 1),
		strings.Replace(base, `"changes":`, `"task_id":null,"changes":`, 1),
		strings.Replace(base, `"changes":`, `"confirm_automation_retry":true,"changes":`, 1),
		strings.Replace(base, failed.ID, "bad", 1),
		strings.Replace(base, `"expected_version":2`, `"expected_version":3`, 1),
		strings.Replace(base, "automation.retry", "automation.enable", 1),
		strings.Replace(base, "automation.retry", "task.start", 1),
	} {
		if _, err := tool.Execute(context.Background(), []byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	tool.(*aiWorkspaceTool).policy = harness.NewCapabilities("work")
	if _, err := tool.Execute(context.Background(), []byte(base)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	rule := automationRuleByPreset(t, router, automationPresetDailyToday)
	r := performRequest(router, "POST", "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Exec(`CREATE TRIGGER reject_ai_retry_reminder BEFORE INSERT ON reminders BEGIN SELECT RAISE(ABORT, 'TEST_RETRY'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.AutomationRule{}).Where("id=?", rule.ID).Update("next_run_at", formatInboxTimestamp(api.options.Now())).Error; err != nil {
		t.Fatal(err)
	}
	if err := api.projectDueAutomationRule(rule.ID, api.options.Now()); err != nil {
		t.Fatal(err)
	}
	var scheduled models.AutomationRun
	if err := store.DB.Where("rule_id=?", rule.ID).First(&scheduled).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/automations/rules/"+rule.ID+"/disable", nil, map[string]string{"If-Match": `"2"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if _, err := previewAIAutomationRetry(store.DB, aiWorkspaceAction{AutomationRunID: scheduled.ID, ExpectedVersion: scheduled.RuleVersion}); err == nil || !strings.Contains(err.Error(), "Enable") {
		t.Fatal("disabled schedule", err)
	}
}

func TestAIAutomationRetryHarnessOutcomeAndPrivacy(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprint(succeeds), func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
			failed := failAIProjectAutomation(t, router, store)
			if succeeds {
				if err := store.DB.Exec("DROP TRIGGER reject_ai_retry_inbox").Error; err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			confirmedRoute := ""
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				encoded, _ := json.Marshal(payload)
				if strings.Contains(string(encoded), "captured-private-name") || strings.Contains(string(encoded), "config_snapshot") || strings.Contains(string(encoded), "TEST_RETRY_FAILURE") {
					t.Error("private capture/storage text leaked")
				}
				if calls <= 2 {
					name, args := "workspace_automations", fmt.Sprintf(`{"view":"run","id":%q}`, failed.ID)
					if calls == 2 {
						if !strings.Contains(string(encoded), "ACTION_WRITE_FAILED") {
							t.Error("missing actual failed run")
						}
						name, args = "workspace_propose", fmt.Sprintf(`{"action":"automation.retry","automation_run_id":%q,"expected_version":%d,"changes":{}}`, failed.ID, failed.RuleVersion)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("retry-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
					fmt.Fprintf(w, "data: %s\n\n", frame)
					return
				}
				if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
					t.Error("missing pending receipt")
				}
				if calls == 4 {
					if confirmedRoute == "" || !strings.Contains(string(encoded), confirmedRoute) {
						t.Error("missing exact confirmed attempt route in follow-up receipt")
					}
					if !strings.Contains(string(encoded), "confirmed 只表示新尝试已记录") {
						t.Error("missing failure-aware receipt instructions without a grant")
					}
					status := "failed"
					if succeeds {
						status = "succeeded"
					}
					if !strings.Contains(string(encoded), `\"status\":\"`+status+`\"`) || !strings.Contains(string(encoded), "automation_run_result") {
						t.Error("missing actual immutable outcome")
					}
					registered, _ := json.Marshal(payload["tools"])
					if aiHasProtectedWorkspaceTool(registered) {
						t.Error("inherited grant")
					}
				}
				streamMockAIDelta(w, `请核对重试卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "retry", upstream.URL+"/v1", "model")
			var current models.AIProvider
			if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "检查失败运行并建议重试", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
			r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
				t.Fatal(r.Body.String(), calls)
			}
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 1)
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_retry":true}`, proposal.Fingerprint)), nil)
			if r.Code != 200 {
				t.Fatal(r.Body.String())
			}
			var next models.AutomationRun
			if err := store.DB.Where("retry_of_run_id=?", failed.ID).First(&next).Error; err != nil {
				t.Fatal(err)
			}
			confirmedRoute = "/settings/automation?run=" + next.ID
			var response struct {
				Data struct {
					Route string `json:"route"`
				} `json:"data"`
			}
			if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil || response.Data.Route != confirmedRoute {
				t.Fatal("decision did not locate actual attempt", r.Body.String(), err)
			}
			var session models.AISession
			if err := store.DB.First(&session).Error; err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "重试结果怎样"})
			r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
			if r.Code != 200 || calls != 4 {
				t.Fatal(r.Body.String(), calls)
			}
		})
	}
}

func TestAIAutomationRetryRequiresConsentAndPreservesCapture(t *testing.T) {
	router, store, api, tool, generation := aiActionTestFixture(t)
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	r := performRequest(router, "POST", "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if err := store.DB.Exec(`CREATE TRIGGER reject_ai_retry_inbox BEFORE INSERT ON inbox_items WHEN NEW.source_entity_type='automation' BEGIN SELECT RAISE(ABORT, 'TEST_RETRY_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	project := createProjectForTest(t, router, `{"name":"captured-private-name"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id=? AND status='failed'", rule.ID).First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec("DROP TRIGGER reject_ai_retry_inbox").Error; err != nil {
		t.Fatal(err)
	}
	// A captured event can be retried after its rule has been changed/disabled.
	r = performRequest(router, "PATCH", "/api/v1/automations/rules/"+rule.ID, []byte(`{"config":{"priority":"P3"}}`), map[string]string{"If-Match": `"2"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	r = performRequest(router, "POST", "/api/v1/automations/rules/"+rule.ID+"/disable", nil, map[string]string{"If-Match": `"3"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"automation.retry","automation_run_id":%q,"expected_version":%d,"changes":{}}`, failed.ID, failed.RuleVersion))
	if !strings.Contains(row.PreviewJSON, "captured-private-name") {
		t.Fatal("human preview lost captured action")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 1)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	r = performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	assertAPIError(t, r, 422, "AUTOMATION_RETRY_CONFIRMATION_REQUIRED")
	r = performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_retry":true}`, row.Fingerprint)), nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"succeeded"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	var next models.AutomationRun
	if err := store.DB.Where("retry_of_run_id=?", failed.ID).First(&next).Error; err != nil {
		t.Fatal(err)
	}
	if next.RuleVersion != failed.RuleVersion || next.ConfigSnapshotJSON != failed.ConfigSnapshotJSON || next.ActionSnapshotJSON != failed.ActionSnapshotJSON || next.Attempt != 2 {
		t.Fatal("retry did not preserve immutable capture", next)
	}
	r = performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 2)
	receipt, err := api.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || !strings.Contains(receipt, "succeeded") || strings.Contains(receipt, "captured-private-name") {
		t.Fatal(receipt, err)
	}
}
