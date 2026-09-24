package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// A real terminal transaction and the existing durable consumer establish the
// source. No fabricated Inbox or model process is needed for these read tests.
func newAIInboxAgentFailureForTest(t *testing.T) (aiAgentRunFixture, models.AgentRun, models.InboxItem) {
	t.Helper()
	f := newAIAgentRunFixture(t)
	rule := automationRuleByPreset(t, f.Router, automationPresetAgentRunFailed)
	response := performRequest(f.Router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)})
	if response.Code != http.StatusOK {
		t.Fatalf("enable failure diagnostic: %s", response.Body.String())
	}
	run := seedRunningAgentRun(t, f)
	if err := f.Service.finalizeAgentRunOutcome(run, "", "", "AGENT_RUN_FAILED", formatInboxTimestamp(f.Service.options.Now())); err != nil {
		t.Fatal(err)
	}
	f.Service.consumeAutomationEventDeliveriesBestEffort("test")
	inbox := loadNativeInboxSourceForTest(t, f.Store, "agent_run_failed", run.ID)
	return f, run, inbox
}

func TestAIInboxAgentFailureSourcePermissionAndExactOwner(t *testing.T) {
	f, run, inbox := newAIInboxAgentFailureForTest(t)
	tool := aiInboxSourceToolForTest(t, f.Service, f.Store, "work", "outputs")
	assertAIInboxProtectedScopeForTest(t, tool, inbox.ID, "outputs", []string{"tasks", "agent_runs", "automation_runs", "automation_rules", "workflow_events"}, run.ID, run.TaskID)
	got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
	assertAIInboxSourceTargetForTest(t, got, "agent_run", run.ID, "failed", 0)
	if got.Subject.Route != agentRunRoute(run.TaskID, run.ID) || got.SourceType != "agent_run_failed" || got.SourceVersion != nil || got.SnapshotMatchesCurrent != nil || len(got.Related) != 2 || got.Related[0].Type != "task" || got.Related[0].ID != run.TaskID || got.Related[0].Version == nil || *got.Related[0].Version != f.Task.Version || got.Related[1].Type != "automation_run" || got.Related[1].Status == nil || *got.Related[1].Status != "succeeded" || got.Related[1].Version != nil {
		t.Fatalf("failure identity or receipt conflated: %s", raw)
	}
	for _, forbidden := range []string{"PRIVATE", "MUST_NOT_LEAK", "input_snapshot", "result_text", "action_snapshot", "config_snapshot", "error_code", "failed_at", "attempt", f.Provider.ID} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("source result contains non-identity data %s: %s", forbidden, raw)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIInboxAgentFailureSourceRejectsTamperingAndMissingRun(t *testing.T) {
	f, run, inbox := newAIInboxAgentFailureForTest(t)
	tool := aiInboxSourceToolForTest(t, f.Service, f.Store, "work", "outputs")
	for _, test := range []struct {
		name, statement string
		args            []any
	}{
		{"wrong task", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.task_id',?) WHERE id=?", []any{uuid.NewString(), inbox.ID}},
		{"wrong Agent", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.agent_run_id',?) WHERE id=?", []any{uuid.NewString(), inbox.ID}},
		{"wrong event", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.source_event_id',?) WHERE id=?", []any{uuid.NewString(), inbox.ID}},
		{"wrong automation", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.automation_run_id',?) WHERE id=?", []any{uuid.NewString(), inbox.ID}},
		{"wrong rule", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.automation_rule_id',?) WHERE id=?", []any{uuid.NewString(), inbox.ID}},
		{"attempt drift", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.attempt',2) WHERE id=?", []any{inbox.ID}},
		{"string attempt", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.attempt','1') WHERE id=?", []any{inbox.ID}},
		{"boolean attempt", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.attempt',json('true')) WHERE id=?", []any{inbox.ID}},
		{"error drift", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.error_code','AGENT_RUN_IDENTITY_CHANGED') WHERE id=?", []any{inbox.ID}},
		{"extra payload", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.private','MUST_NOT_LEAK') WHERE id=?", []any{inbox.ID}},
		{"invalid key", "UPDATE inbox_items SET source_event_key='agent-run:other:failed' WHERE id=?", []any{inbox.ID}},
		{"missing live Run", "DELETE FROM agent_runs WHERE id=?", []any{run.ID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			withCorruptAIInboxSourceForTest(t, tool, inbox.ID, test.statement, test.args...)
		})
	}
	if err := f.Store.DB.Exec("UPDATE inbox_items SET source_deleted_at=? WHERE id=?", formatInboxTimestamp(f.Service.options.Now()), inbox.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertAIInboxProtectedScopeForTest(t, tool, inbox.ID, "outputs", []string{"agent_runs", "tasks", "automation_runs"}, run.ID, run.TaskID)
	got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
	if got.LookupStatus != "deleted" || got.Subject != nil || len(got.Related) != 0 {
		t.Fatalf("deleted source has live route: %s", raw)
	}
}

func TestAIInboxAgentFailureHarnessRequiresFreshOutputsConsent(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f, run, inbox := newAIInboxAgentFailureForTest(t)
			var sourceTaskID string
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				for _, secret := range []string{"MUST_NOT_LEAK", "PRIVATE ACTOR", "input_snapshot_json", "action_snapshot_json", "config_snapshot_json", "result_text", *inbox.SourceEventKey} {
					if strings.Contains(string(raw), secret) {
						t.Errorf("source journey leaked %q", secret)
					}
				}
				if aiBudgetHasTool(payload, "workspace_propose") || aiBudgetHasTool(payload, "workspace_agent_execution") {
					t.Error("read consent exposed execution/preparation")
				}
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("failure-source-%d", call), name, args)
				}
				finish := func(answer string) {
					writeAIBudgetTextTurn(w, protocol, answer+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				switch call {
				case 1, 4:
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 2, 5:
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inbox.ID))
				case 3, 6:
					var source aiInboxSourceResult
					text := aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("failure-source-%d", call-1), "workspace_inbox_source")
					if err := json.Unmarshal([]byte(text), &source); err != nil {
						t.Error(err)
					}
					if call == 3 {
						if source.LookupStatus != "permission_required" || source.Subject != nil || len(source.RequiredScopes) != 1 || source.RequiredScopes[0] != "outputs" || strings.Contains(text, run.ID) || strings.Contains(text, run.TaskID) {
							t.Errorf("work-only source leaked identity: %s", text)
						}
						finish("需要在新消息授权产出读取，尚未读取或重试原执行。")
					} else {
						if source.LookupStatus != "available" || source.Subject == nil || source.Subject.ID != run.ID || len(source.Related) != 2 {
							t.Errorf("actual failed Run not resolved: %s", text)
							finish("没有可用来源。")
							return
						}
						sourceTaskID = source.Related[0].ID
						emit("workspace_guide", `{"topic":"agents"}`)
					}
				case 7:
					emit("workspace_agent_runs", fmt.Sprintf(`{"task_id":%q,"status":"failed"}`, sourceTaskID))
				case 8:
					text := aiPlanRetryHarnessResult(t, payload, protocol, "failure-source-7", "workspace_agent_runs")
					if !strings.Contains(text, run.ID) || !strings.Contains(text, `"status":"failed"`) {
						t.Errorf("wrong execution ledger: %s", text)
					}
					finish("原执行仍失败，诊断事项已经创建。未重跑模型，也未完成任务；重试需要新授权和人工确认。")
				case 9:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new message inherited scope: %s", name)
						}
					}
					finish("本条未授权，不读取或重试。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					finish("停止。")
				}
			})
			provider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			var sessionID string
			for i, scopes := range [][]string{{"work"}, {"work", "outputs"}, nil} {
				request := map[string]any{"provider_id": provider.ID, "message": "只查询失败诊断事项 " + inbox.ID + "，不要执行任何动作。"}
				if sessionID != "" {
					request["session_id"] = sessionID
				}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != 200 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || upstream.calls.Load() != []int32{3, 8, 9}[i] || t.Failed() {
					t.Fatalf("source journey failed: %d %s", response.Code, response.Body.String())
				}
				if sessionID == "" {
					sessionID = decodeAIBudgetMeta(t, response.Body.String()).SessionID
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='failed'", 1, run.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='open' AND version=1", 1, inbox.ID)
		})
	}
}
