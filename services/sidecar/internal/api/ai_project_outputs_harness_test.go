package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// The fixture uses native submission to create its Artifact -> Inbox source.
// Provider decisions then use IDs/versions from actual tool results, not from
// fixture SQL. All writes still require completed generations and human clicks.
func TestAIProjectOutputsHarnessFollowupApprovalAndReview(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			project := createProjectForTest(t, router, `{"name":"Harness followup project"}`, nil)
			sourceTask := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Source delivery","project_id":%q,"review_policy":"manual"}`, project.ID))
			createAssignmentForTest(t, router, sourceTask.ID, "assignee", models.BuiltinOwnerActorID, sourceTask.Version, "")
			createAssignmentForTest(t, router, sourceTask.ID, "reviewer", models.BuiltinOwnerActorID, sourceTask.Version+1, "")
			const privateBody = "PRIVATE PROJECT ARTIFACT BODY"
			submitted := performRequest(router, http.MethodPost, "/api/v1/tasks/"+sourceTask.ID+"/submit-output", []byte(`{"summary":"PRIVATE SOURCE SUMMARY","artifacts":[{"client_ref":"follow","storage_kind":"text","name":"Followup delivery","content_text":"PRIVATE PROJECT ARTIFACT BODY","requires_followup":true},{"client_ref":"plain","storage_kind":"link","name":"Unrelated reference","reference_url":"https://private.invalid/secret","requires_followup":false}]}`), map[string]string{"If-Match": `"3"`})
			if submitted.Code != http.StatusCreated {
				t.Fatalf("native source submission=%d %s", submitted.Code, submitted.Body.String())
			}
			source := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
			var projectID, artifactID, inboxID, splitProposalID, completeProposalID, reviewProposalID string
			var plainTask, manualTask aiInboxTaskRow
			var inboxVersion int64
			readOutputs := func(payload map[string]any, resultID, wantStatus string, wantTotal, wantDone, wantReview int64) {
				t.Helper()
				var found struct {
					ProjectID string `json:"project_id"`
					Total     int    `json:"total_items"`
					Items     []struct {
						ArtifactID   string `json:"artifact_id"`
						SubmissionID string `json:"submission_id"`
						Task         struct {
							ID      string `json:"id"`
							Version int64  `json:"version"`
						} `json:"task"`
						Followup *projectArtifactFollowupOutput `json:"followup"`
					} `json:"items"`
				}
				content := aiPlanRetryHarnessResult(t, payload, protocol, resultID, "workspace_project_outputs")
				if json.Unmarshal([]byte(content), &found) != nil || found.ProjectID != projectID || found.Total != 1 || len(found.Items) != 1 || found.Items[0].ArtifactID != source.Artifacts[0].ID || found.Items[0].Followup == nil {
					t.Errorf("native Artifact followup was not discoverable: %s", content)
					return
				}
				item := found.Items[0]
				followup := item.Followup
				if item.Task.ID != sourceTask.ID || item.Task.Version != source.Task.Version || item.SubmissionID != source.Submission.ID || followup.Status != wantStatus || followup.Progress.RequiredTotal != wantTotal || followup.Progress.RequiredDone != wantDone || followup.Progress.RequiredRemaining != wantTotal-wantDone || followup.Progress.RequiredWaitingReview != wantReview || followup.Progress.AllRequiredDone != (wantTotal > 0 && wantTotal == wantDone) {
					t.Errorf("fresh Project progress mismatch (%s %d/%d waiting=%d): %s", wantStatus, wantDone, wantTotal, wantReview, content)
				}
				// Compare against the existing native aggregate, not a second mock.
				native := performRequest(router, http.MethodGet, "/api/v1/projects/"+projectID+"/artifacts", nil, nil)
				var nativePage struct {
					Data []projectArtifactOutput `json:"data"`
				}
				matched := false
				if native.Code == http.StatusOK && json.Unmarshal(native.Body.Bytes(), &nativePage) == nil {
					for _, candidate := range nativePage.Data {
						if candidate.Artifact.ID == item.ArtifactID {
							want, _ := json.Marshal(candidate.Followup)
							got, _ := json.Marshal(followup)
							matched = string(want) == string(got)
						}
					}
				}
				if !matched {
					t.Errorf("AI Project facts diverged from native aggregate: %s native=%s", content, native.Body.String())
				}
				if wantStatus == "tracking" && wantDone == 1 && inboxVersion != followup.InboxItemVersion {
					t.Error("fixture must exercise changed progress with unchanged Inbox version")
				}
				artifactID, inboxID, inboxVersion = item.ArtifactID, followup.InboxItemID, followup.InboxItemVersion
			}
			readRelations := func(payload map[string]any, resultID string, wantCount int) {
				t.Helper()
				var result struct {
					InboxID  string            `json:"inbox_item_id"`
					Version  int64             `json:"inbox_item_version"`
					Items    []aiInboxTaskRow  `json:"items"`
					Progress inboxTaskProgress `json:"progress"`
				}
				content := aiPlanRetryHarnessResult(t, payload, protocol, resultID, "workspace_inbox_tasks")
				if json.Unmarshal([]byte(content), &result) != nil || result.InboxID != inboxID || result.Version != inboxVersion || len(result.Items) != wantCount || result.Progress.RequiredTotal != int64(wantCount) {
					t.Errorf("actual Inbox relationship read=%s", content)
					return
				}
				for _, item := range result.Items {
					if item.TaskID == nil || item.TaskVersion == nil || item.TaskTitle == nil || item.TaskStatus == nil || *item.TaskStatus != "todo" || !item.IsRequired {
						t.Errorf("invalid newly split Task: %+v", item)
						continue
					}
					switch *item.TaskTitle {
					case "Publish followup":
						plainTask = item
					case "Verify followup":
						manualTask = item
					}
				}
			}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("followup-%d", call), name, args)
				}
				for _, secret := range []string{privateBody, "PRIVATE SOURCE SUMMARY", "private.invalid/secret", "PRIVATE FOLLOWUP PROOF"} {
					if strings.Contains(string(raw), secret) {
						t.Errorf("metadata journey exposed private content %q", secret)
					}
				}
				proposalResult := func(resultID string) string {
					var result struct {
						ID string `json:"proposal_id"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, resultID, "workspace_propose")
					if json.Unmarshal([]byte(content), &result) != nil || result.ID == "" || !strings.Contains(content, "NOT executed") {
						t.Errorf("missing real pending proposal: %s", content)
					}
					return result.ID
				}
				finish := func(message string) {
					writeAIBudgetTextTurn(w, protocol, message+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				switch call {
				case 1:
					emit("workspace_search", `{"type":"project","query":"Harness followup project"}`)
				case 2:
					var found struct {
						Items []aiWorkspaceListItem `json:"items"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-1", "workspace_search")
					if json.Unmarshal([]byte(content), &found) != nil || len(found.Items) != 1 || found.Items[0].ID != project.ID {
						t.Errorf("project discovery=%s", content)
					} else {
						projectID = found.Items[0].ID
					}
					emit("workspace_guide", `{"topic":"outputs"}`)
				case 3:
					if !aiBudgetHasTool(payload, "workspace_project_outputs") {
						t.Error("outputs guide did not expose project output discovery")
					}
					emit("workspace_project_outputs", fmt.Sprintf(`{"project_id":%q,"followup_status":"active"}`, projectID))
				case 4:
					readOutputs(payload, "followup-3", "open", 0, 0, 0)
					finish("发现待跟进产出，尚未修改任何任务。")
				case 5:
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 6, 14:
					if !aiBudgetHasTool(payload, "workspace_inbox_tasks") {
						t.Error("inbox guide did not expose relation reads")
					}
					emit("workspace_inbox_tasks", fmt.Sprintf(`{"inbox_item_id":%q}`, inboxID))
				case 7:
					readRelations(payload, "followup-6", 0)
					emit("workspace_task_options", `{"type":"actor"}`)
				case 8:
					var options struct {
						Items []aiTaskOption `json:"items"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-7", "workspace_task_options")
					var ownerID string
					if json.Unmarshal([]byte(content), &options) == nil {
						for _, item := range options.Items {
							if item.Type == "owner" {
								ownerID = item.ID
							}
						}
					}
					if ownerID == "" {
						t.Errorf("no actual owner candidate: %s", content)
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":%d,"changes":{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"publish","title":"Publish followup","project_id":%q,"review_policy":"none","is_required":true,"assignee_actor_id":%q},{"key":"verify","title":"Verify followup","project_id":%q,"review_policy":"manual","is_required":true,"assignee_actor_id":%q}]}}`, inboxID, inboxVersion, projectID, ownerID, projectID, ownerID))
				case 9:
					splitProposalID = proposalResult("followup-8")
					finish("拆分两个必需任务的建议待你确认，尚未创建。")
				case 10:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new message inherited %s without new grant", name)
						}
					}
					finish("本条消息没有工作台权限，未继续查询或执行。")
				case 11, 17, 22:
					if aiBudgetHasTool(payload, "workspace_project_outputs") {
						t.Error("new generation inherited previous catalog selection")
					}
					emit("workspace_guide", `{"topic":"outputs"}`)
				case 12, 18, 23:
					if !aiBudgetHasTool(payload, "workspace_project_outputs") {
						t.Error("fresh outputs guide did not select project tool")
					}
					emit("workspace_project_outputs", fmt.Sprintf(`{"project_id":%q,"artifact_id":%q}`, projectID, artifactID))
				case 13:
					readOutputs(payload, "followup-12", "tracking", 2, 0, 0)
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 15:
					readRelations(payload, "followup-14", 2)
					if plainTask.TaskID == nil || plainTask.TaskVersion == nil || manualTask.TaskID == nil {
						t.Error("fresh split Tasks missing")
						finish("无法核实任务。")
						return
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"task.complete","task_id":%q,"expected_version":%d,"changes":{}}`, *plainTask.TaskID, *plainTask.TaskVersion))
				case 16:
					completeProposalID = proposalResult("followup-15")
					finish("普通任务的完成建议待你确认，另一项仍需人工验收。")
				case 19:
					readOutputs(payload, "followup-18", "tracking", 2, 1, 1)
					emit("workspace_task_submissions", fmt.Sprintf(`{"task_id":%q,"view":"list","status":"pending_review"}`, *manualTask.TaskID))
				case 20:
					var result struct {
						TaskID              string                `json:"task_id"`
						TaskVersion         int64                 `json:"task_version"`
						TaskStatus          string                `json:"task_status"`
						CurrentSubmissionID string                `json:"current_submission_id"`
						Items               []aiTaskSubmissionRow `json:"items"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "followup-19", "workspace_task_submissions")
					if json.Unmarshal([]byte(content), &result) != nil || result.TaskID != *manualTask.TaskID || result.TaskStatus != "waiting_review" || len(result.Items) != 1 || !result.Items[0].IsCurrent || result.Items[0].ID != result.CurrentSubmissionID {
						t.Errorf("real pending review batch missing: %s", content)
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"task.review","task_id":%q,"expected_version":%d,"changes":{"submission_id":%q,"decision":"accept","reason":""}}`, result.TaskID, result.TaskVersion, result.CurrentSubmissionID))
				case 21:
					reviewProposalID = proposalResult("followup-20")
					finish("当前进度仍为一半。请亲自核验产出并单独确认验收，不能把已提交当成完成。")
				case 24:
					readOutputs(payload, "followup-23", "resolved", 2, 2, 0)
					if aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("final read-only message inherited action scope")
					}
					finish("已从实时记录核对：两项必需任务均完成，后续事项已自动解决。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			var sessionID string
			send := func(message string, scopes []string, wantCalls int) {
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
				if response.Code != http.StatusOK || upstream.calls.Load() != int32(wantCalls) || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") || t.Failed() {
					t.Fatalf("chat calls=%d want=%d response=%d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
				if sessionID == "" {
					var session models.AISession
					if err := store.DB.First(&session).Error; err != nil {
						t.Fatal(err)
					}
					sessionID = session.ID
				}
			}
			loadProposal := func(id string) models.AIActionProposal {
				t.Helper()
				var proposal models.AIActionProposal
				if id == "" {
					t.Fatal("missing observed proposal ID")
				}
				if err := store.DB.First(&proposal, "id=?", id).Error; err != nil {
					t.Fatal(err)
				}
				return proposal
			}
			confirm := func(id string, outputConsent bool) {
				t.Helper()
				proposal := loadProposal(id)
				decision := map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm"}
				if outputConsent {
					decision["confirm_task_output"] = true
				}
				body, _ := json.Marshal(decision)
				for range 2 {
					response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+id+"/decision", body, nil)
					if response.Code != http.StatusOK {
						t.Fatalf("human approval=%d %s", response.Code, response.Body.String())
					}
				}
				if current := loadProposal(id); current.Status != "confirmed" || current.ResultID == nil {
					t.Fatalf("missing durable approval result: %+v", current)
				}
			}
			send("查看 Harness followup project 的未完成产出后续事项。", []string{"work", "outputs"}, 4)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks", 0)
			send("将这个后续事项拆成发布和验收两项必需任务，先让我确认。", []string{"work", "actions"}, 9)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			confirm(splitProposalID, false)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 3)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=? AND is_required=1 AND unlinked_at IS NULL", 2, inboxID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND resolution_policy='all_required_tasks_done'", 1, inboxID)
			send("谢谢。", nil, 10)
			send("重新读取进度，发布工作已完成，请给我普通任务的完成建议。", []string{"work", "outputs", "actions"}, 16)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='done'", 0)
			confirm(completeProposalID, false)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='done'", 1, *plainTask.TaskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking' AND version=?", 1, inboxID, inboxVersion)
			// Submission is an actual human/native command. It does not mean review
			// passed and must leave the Project followup incomplete.
			followupSubmission := performRequest(router, http.MethodPost, "/api/v1/tasks/"+*manualTask.TaskID+"/submit-output", []byte(`{"summary":"Evidence submitted for owner review","artifacts":[{"client_ref":"proof","storage_kind":"text","name":"Followup proof","content_text":"PRIVATE FOLLOWUP PROOF"}]}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, *manualTask.TaskVersion)})
			if followupSubmission.Code != http.StatusCreated {
				t.Fatalf("native followup submission=%d %s", followupSubmission.Code, followupSubmission.Body.String())
			}
			proof := decodeSubmitOutputResponse(t, followupSubmission.Body.Bytes())
			send("重新核对项目产出及当前验收批次，给我验收建议，证据由我核实。", []string{"work", "outputs", "actions"}, 21)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, *manualTask.TaskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, proof.Submission.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='tracking'", 1, inboxID)
			review := loadProposal(reviewProposalID)
			if !strings.Contains(review.PreviewJSON, "PRIVATE FOLLOWUP PROOF") {
				t.Fatal("human review preview omitted actual evidence")
			}
			missingConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, review.Fingerprint))
			assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+review.ID+"/decision", missingConsent, nil), http.StatusUnprocessableEntity, "AI_TASK_OUTPUT_CONFIRMATION_REQUIRED")
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, *manualTask.TaskID)
			confirm(reviewProposalID, true)
			send("只读重新查看同一项目产出的后续事项，确认实时结果。", []string{"work", "outputs"}, 24)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id IN (?,?) AND status='done'", 2, *plainTask.TaskID, *manualTask.TaskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='resolved' AND resolution_mode='automatic'", 1, inboxID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='confirmed'", 3)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 3)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='accepted'", 1, proof.Submission.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, source.Submission.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts", 3)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_item_tasks WHERE inbox_item_id=?", 2, inboxID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%PRIVATE PROJECT ARTIFACT BODY%' OR content LIKE '%PRIVATE FOLLOWUP PROOF%'", 0)
		})
	}
}
