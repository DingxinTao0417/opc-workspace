package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiFollowupSourceDetailForTest struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	ClientID string `json:"client_id"`
	Version  int64  `json:"version"`
	Fields   struct {
		Status              string  `json:"status"`
		ClientID            string  `json:"client_id"`
		ClientStatus        string  `json:"client_status"`
		AssignedActorID     string  `json:"assigned_actor_id"`
		AssignedActorType   string  `json:"assigned_actor_type"`
		AssignedActorStatus string  `json:"assigned_actor_status"`
		ScheduledAt         string  `json:"scheduled_at"`
		Timezone            string  `json:"timezone"`
		Channel             string  `json:"channel"`
		Purpose             string  `json:"purpose"`
		CompletedAt         *string `json:"completed_at"`
	} `json:"fields"`
	Field   string  `json:"field"`
	Content *string `json:"content"`
	Route   string  `json:"route"`
}

// Native creation and the actual due scanner establish the Inbox source; no
// fixture inserts an Inbox row or fabricates a source identity for the model.
func TestAIClientFollowupSourceHarnessConsentCompletionAndNextPlan(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
			client := createClientForTest(t, router, `{"name":"Source consent customer","notes":"PRIVATE CLIENT NOTES","contact_name":"PRIVATE CONTACT NAME","email":"private-contact@example.invalid"}`, nil)
			actor := createActorForTest(t, router, `{"type":"person","display_name":"Local followup owner"}`, nil)
			created := performRequest(router, http.MethodPost, "/api/v1/client-followups", []byte(fmt.Sprintf(`{"client_id":%q,"assigned_actor_id":%q,"scheduled_at":"2026-09-21T12:00:00Z","timezone":"UTC","channel":"phone","purpose":"Discuss draft dimensions","notes":"AUTHORIZED FOLLOWUP NOTES","priority":"high"}`, client.ID, actor.ID)), nil)
			if created.Code != http.StatusCreated {
				t.Fatalf("native followup=%d %s", created.Code, created.Body.String())
			}
			original := decodeClientFollowupResponse(t, created.Body.Bytes())
			for range 2 {
				if err := service.projectDueClientFollowups(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			// Exact equality is due, although native creation stores the timestamp
			// without a fractional suffix and the scanner clock uses fixed nanos.
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='client_followup' AND source_entity_id=?", 1, original.ID)
			inbox := loadNativeInboxSourceForTest(t, store, "client_followup", original.ID)
			const reportedResult = "Customer verified draft dimensions."
			const completedAt = "2026-09-21T11:59:30Z"
			const nextStep = "Prepare a final estimate."
			const nextAt = "2026-09-22T12:00:00Z"
			var sourceFollowupID, sourceClientID, observedActorID, proposalID string
			var nextReadID, nextReceiptID string
			var sourceVersion int64
			var sourceClientVersion int64
			var proposalGenerationID string
			readSource := func(payload map[string]any, call int, expectedStatus string, expectedVersion int64, expectedInboxStatus string, matches bool) {
				t.Helper()
				content := aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("followup-source-%d", call), "workspace_inbox_source")
				var result aiInboxSourceResult
				if json.Unmarshal([]byte(content), &result) != nil || result.LookupStatus != "available" || result.SourceType != "client_followup" || result.InboxItemID != inbox.ID || result.InboxItemStatus != expectedInboxStatus || result.Subject == nil || result.Subject.ID != original.ID || result.Subject.Type != "client_followup" || result.Subject.Version == nil || *result.Subject.Version != expectedVersion || result.Subject.Status == nil || *result.Subject.Status != expectedStatus || result.Subject.Route != aiClientRecordRoute(client.ID, "followup", original.ID) || len(result.Related) != 1 || result.Related[0].Type != "client" || result.Related[0].ID != client.ID || result.Related[0].Version == nil || result.Related[0].Status == nil || *result.Related[0].Status != "active" || result.Related[0].Route != searchRoute("client", client.ID) || result.SourceVersion == nil || *result.SourceVersion != original.Version || result.SnapshotMatchesCurrent == nil || *result.SnapshotMatchesCurrent != matches {
					t.Errorf("current source identity/old occurrence mismatch: %s", content)
					return
				}
				if expectedStatus == "completed" && (*result.Related[0].Version != sourceClientVersion+2 || result.InboxItemVersion != inbox.Version+1) {
					t.Errorf("completion plus new plan did not refresh independent versions: %s", content)
				}
				sourceFollowupID, sourceVersion = result.Subject.ID, *result.Subject.Version
				sourceClientID, sourceClientVersion = result.Related[0].ID, *result.Related[0].Version
			}
			readDetail := func(payload map[string]any, call int) aiFollowupSourceDetailForTest {
				t.Helper()
				content := aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("followup-source-%d", call), "workspace_client_records")
				var result aiFollowupSourceDetailForTest
				if json.Unmarshal([]byte(content), &result) != nil || result.Type != "followup" || result.ClientID != sourceClientID || result.Fields.ClientID != sourceClientID || result.Fields.ClientStatus != "active" || result.Route != aiClientRecordRoute(sourceClientID, "followup", result.ID) {
					t.Errorf("current Followup detail mismatch: %s", content)
				}
				return result
			}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				// Even clients consent does not include private contact fields or
				// unrelated client notes. Source keys/payloads never cross the tool.
				for _, forbidden := range []string{"PRIVATE CLIENT NOTES", "PRIVATE CONTACT NAME", "private-contact@example.invalid", *inbox.SourceEventKey} {
					if strings.Contains(string(raw), forbidden) {
						t.Errorf("Provider payload leaked %q", forbidden)
					}
				}
				if call <= 3 {
					for _, forbidden := range []string{original.ID, client.ID, actor.ID, actor.DisplayName, client.Name, original.Purpose, "AUTHORIZED FOLLOWUP NOTES"} {
						if strings.Contains(string(raw), forbidden) {
							t.Errorf("work-only source exposed protected identity/body: %q", forbidden)
						}
					}
					if aiBudgetHasTool(payload, "workspace_client_records") || aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("work-only discovery imported client/write capability")
					}
				}
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("followup-source-%d", call), name, args)
				}
				finish := func(text string) {
					writeAIBudgetTextTurn(w, protocol, text+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				switch call {
				case 1:
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 2:
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inbox.ID))
				case 3:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-source-2", "workspace_inbox_source")
					var result aiInboxSourceResult
					if json.Unmarshal([]byte(content), &result) != nil || result.LookupStatus != "permission_required" || result.SourceType != "client_followup" || len(result.RequiredScopes) != 1 || result.RequiredScopes[0] != "clients" || result.Subject != nil || len(result.Related) != 0 || result.SourceVersion != nil || result.SnapshotMatchesCurrent != nil {
						t.Errorf("missing bounded clients-consent request: %s", content)
					}
					finish("此事项来源需要客户资料范围；请在下一条消息重新授权，当前没有读取或修改回访。")
				case 4:
					if aiBudgetHasTool(payload, "workspace_client_records") || aiBudgetHasTool(payload, "workspace_inbox_source") {
						t.Error("new grant unexpectedly inherited loaded catalog topic")
					}
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 5:
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inbox.ID))
				case 6:
					readSource(payload, 5, "planned", original.Version, "open", true)
					emit("workspace_guide", `{"topic":"people_clients"}`)
				case 7:
					if !aiBudgetHasTool(payload, "workspace_client_records") {
						t.Error("clients guide did not expose current-record lookup")
					}
					emit("workspace_client_records", fmt.Sprintf(`{"type":"followup","view":"detail","id":%q,"field":"notes"}`, sourceFollowupID))
				case 8:
					current := readDetail(payload, 7)
					if current.ID != sourceFollowupID || current.Version != sourceVersion || current.Fields.Status != "planned" || current.Fields.AssignedActorID != actor.ID || current.Fields.AssignedActorType != "person" || current.Fields.AssignedActorStatus != "active" || current.Fields.ScheduledAt != original.ScheduledAt || current.Fields.Timezone != "UTC" || current.Content == nil || *current.Content != "AUTHORIZED FOLLOWUP NOTES" {
						t.Errorf("proposal did not use fresh real detail: %+v", current)
					}
					observedActorID = current.Fields.AssignedActorID
					emit("workspace_propose", fmt.Sprintf(`{"action":"client_followup.complete","client_followup_id":%q,"expected_version":%d,"changes":{"result":%q,"completed_at":%q,"next_step":%q,"next_followup":{"assigned_actor_id":%q,"scheduled_at":%q,"timezone":"UTC","channel":"meeting","purpose":"Review final estimate","priority":"high"}}}`, current.ID, current.Version, reportedResult, completedAt, nextStep, observedActorID, nextAt))
				case 9:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-source-8", "workspace_propose")
					var result struct {
						ID     string `json:"proposal_id"`
						Status string `json:"status"`
					}
					if json.Unmarshal([]byte(content), &result) != nil || result.ID == "" || result.Status != "pending" {
						t.Errorf("completion executed instead of pending: %s", content)
					}
					proposalID = result.ID
					finish("已根据你提供的事实建议登记结果并续排，尚未执行；请核对并单独确认回访已实际完成。")
				case 10:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("following message inherited consent: %s", name)
						}
					}
					if strings.Contains(string(raw), "AUTHORIZED FOLLOWUP NOTES") {
						t.Error("previous tool-only notes carried into no-grant message")
					}
					finish("已收到确认回执；本条没有读取权限，不查询新的客户资料。")
				case 11:
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 12:
					if aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("read-only refresh imported action capability")
					}
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inbox.ID))
				case 13:
					readSource(payload, 12, "completed", original.Version+1, "resolved", false)
					emit("workspace_guide", `{"topic":"people_clients"}`)
				case 14:
					emit("workspace_client_records", fmt.Sprintf(`{"type":"followup","view":"list","client_id":%q}`, sourceClientID))
				case 15:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-source-14", "workspace_client_records")
					var result struct {
						Total int `json:"total_items"`
						Items []struct {
							ID       string `json:"id"`
							ClientID string `json:"client_id"`
							Status   string `json:"status"`
							Version  int64  `json:"version"`
							At       string `json:"at"`
							Route    string `json:"route"`
						} `json:"items"`
					}
					if json.Unmarshal([]byte(content), &result) != nil || result.Total != 2 || len(result.Items) != 2 {
						t.Errorf("compound plan facts missing: %s", content)
					}
					oldFound := false
					for _, entry := range result.Items {
						if entry.ClientID != sourceClientID || entry.Route != aiClientRecordRoute(sourceClientID, "followup", entry.ID) {
							t.Errorf("list escaped current client: %s", content)
						}
						if entry.ID == sourceFollowupID {
							oldFound = entry.Status == "completed" && entry.Version == sourceVersion
						} else if entry.Status == "planned" && entry.Version == 1 && entry.At == nextAt {
							nextReadID = entry.ID
						}
					}
					if !oldFound || nextReadID == "" || nextReadID != nextReceiptID {
						t.Errorf("list confused old target/new result: %s", content)
					}
					emit("workspace_client_records", fmt.Sprintf(`{"type":"followup","view":"detail","id":%q,"field":"result"}`, sourceFollowupID))
				case 16:
					current := readDetail(payload, 15)
					if current.ID != sourceFollowupID || current.Version != original.Version+1 || current.Fields.Status != "completed" || current.Fields.CompletedAt == nil || *current.Fields.CompletedAt != completedAt || current.Field != "result" || current.Content == nil || *current.Content != reportedResult {
						t.Errorf("completion not backed by user-reported persisted facts: %+v", current)
					}
					emit("workspace_client_records", fmt.Sprintf(`{"type":"followup","view":"detail","id":%q,"field":"notes"}`, nextReadID))
				case 17:
					current := readDetail(payload, 16)
					if current.ID != nextReadID || current.ID == sourceFollowupID || current.Version != 1 || current.Fields.Status != "planned" || current.Fields.CompletedAt != nil || current.Fields.AssignedActorID != observedActorID || current.Fields.ScheduledAt != nextAt || current.Fields.Channel != "meeting" || current.Fields.Purpose != "Review final estimate" || current.Content != nil {
						t.Errorf("new planned followup misreported as completed: %+v", current)
					}
					finish("已核实旧回访结果与下一条待回访计划，旧到期事项已解决；没有联系客户或自动执行下一次回访。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					finish("停止。")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			var sessionID string
			send := func(message string, scopes []string, wantCalls int32) string {
				t.Helper()
				request := map[string]any{"provider_id": provider.ID, "message": message}
				if sessionID != "" {
					request["session_id"] = sessionID
				}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || upstream.calls.Load() != wantCalls || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || t.Failed() {
					t.Fatalf("followup source journey calls=%d want=%d: %d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
				meta := decodeAIBudgetMeta(t, response.Body.String())
				if sessionID == "" {
					sessionID = meta.SessionID
				}
				return meta.GenerationID
			}
			assertUnchanged := func() {
				t.Helper()
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 1)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND version=? AND status='planned' AND completed_at IS NULL AND result IS NULL", 1, original.ID, original.Version)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='open' AND version=? AND payload_json=?", 1, inbox.ID, inbox.Version, inbox.PayloadJSON)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action IN ('client_followup_completed','client_followup_created_from_completion','ai_workspace_action_confirmed')", 0)
			}
			send(fmt.Sprintf("只读检查收件箱事项 %s 的来源。", inbox.ID), []string{"work"}, 3)
			assertUnchanged()
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			proposalGenerationID = send(fmt.Sprintf("现重新授权客户资料及操作建议。刚才对应的回访确于 %s 完成，结果原文：%s 下一步原文：%s 请建议登记并由原负责人续排 %s UTC 的 meeting，目的 Review final estimate，priority high。不要联系客户；保存前让我核对。", completedAt, reportedResult, nextStep, nextAt), []string{"work", "clients", "actions"}, 9)
			assertUnchanged()
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal, "id=?", proposalID).Error; err != nil {
				t.Fatal(err)
			}
			if proposal.GenerationID != proposalGenerationID || proposal.Status != "pending" {
				t.Fatalf("proposal not from current authorized generation: %+v", proposal)
			}
			for _, consent := range []string{"", `,"confirm_followup_completed":false`} {
				response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, proposal.Fingerprint, consent)), nil)
				assertAPIError(t, response, http.StatusUnprocessableEntity, "CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED")
				assertUnchanged()
			}
			var firstApproval string
			for range 2 {
				response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_followup_completed":true}`, proposal.Fingerprint)), nil)
				if response.Code != http.StatusOK {
					t.Fatalf("human completion=%d %s", response.Code, response.Body.String())
				}
				var receipt struct {
					Data aiActionResponse `json:"data"`
				}
				if json.Unmarshal(response.Body.Bytes(), &receipt) != nil || receipt.Data.Status != "confirmed" || receipt.Data.Action.ClientFollowupID != original.ID || receipt.Data.ResultID == nil || *receipt.Data.ResultID == original.ID || receipt.Data.ResultVersion == nil || *receipt.Data.ResultVersion != 1 {
					t.Fatalf("compound confirmation confused target and result: %s", response.Body.String())
				}
				nextReceiptID = *receipt.Data.ResultID
				if receipt.Data.Route != aiClientRecordRoute(client.ID, "followup", nextReceiptID) {
					t.Fatalf("receipt did not locate new plan: %s", response.Body.String())
				}
				if firstApproval != "" && firstApproval != response.Body.String() {
					t.Fatal("approval replay changed the compound result")
				}
				firstApproval = response.Body.String()
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups", 2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND version=? AND status='completed' AND result=? AND completed_at=? AND next_step=?", 1, original.ID, original.Version+1, reportedResult, completedAt, nextStep)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND client_id=? AND assigned_actor_id=? AND status='planned' AND scheduled_at=? AND version=1 AND result IS NULL AND completed_at IS NULL", 1, nextReceiptID, client.ID, actor.ID, nextAt)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='resolved' AND version=? AND source_event_key=? AND payload_json=?", 1, inbox.ID, inbox.Version+1, *inbox.SourceEventKey, inbox.PayloadJSON)
			for range 2 {
				if err := service.projectDueClientFollowups(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='client_followup'", 1)
			send("谢谢。", nil, 10)
			send("现在只读重新授权客户资料，核对刚才旧事项的来源、完成记录与下一条计划。", []string{"work", "clients"}, 17)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='client_followup_completed'", 1, original.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='client_followup_created_from_completion'", 1, nextReceiptID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='source_resolved'", 1, inbox.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE kind='tool_call' AND status='failed'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%AUTHORIZED FOLLOWUP NOTES%' OR content LIKE '%PRIVATE CLIENT NOTES%' OR content LIKE '%private-contact@example.invalid%'", 0)
		})
	}
}
