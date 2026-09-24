package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Only the upstream models are fake HTTP endpoints. The conversational Harness,
// autonomous coordinator, actual ExecutorMain subprocess, approvals and Task
// submission/review transactions all run without a browser or manual auto-chat.
func TestAIContinuationIntegrationActualAgentOutputHumanReviewAndTwoAutomaticTurns(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			const report = "CONCRETE_EXECUTOR_REPORT: source evidence and conclusions for human review."
			releaseExecutor := make(chan struct{})
			var releaseOnce sync.Once
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				<-releaseExecutor
				writeAnthropicAgentRunResponse(w, report, "end_turn")
			})
			t.Cleanup(func() { releaseOnce.Do(func() { close(releaseExecutor) }) })
			f.Native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			finishAIGeneration(t, f.Store, f.Generation)

			// A one-hour fallback proves committed action/Run facts wake the real
			// coordinator instead of relying on its periodic safety scan.
			service := f.Router.aiContinuations.a
			f.Router.aiContinuations.close()
			service.options.ContinuationScanInterval = time.Hour
			coordinator := newAIContinuationCoordinator(service)
			service.aiContinuations, f.Router.aiContinuations = coordinator, coordinator
			f.Service.aiContinuations = coordinator
			var startID, reviewID, runID, submissionID, artifactID string
			var actualTaskVersion int64
			plan := aiWorkPlanIntent{Title: "Deliver, inspect, and independently approve a report", Steps: []aiWorkPlanStep{
				{ID: "execute", Title: "Produce a reviewable report", Kind: "action", DependsOn: []string{}},
				{ID: "review", Title: "Owner reviews the actual submission", Kind: "action", DependsOn: []string{"execute"}},
				{ID: "verify", Title: "Read back the final accepted facts", Kind: "analysis", Report: "pending", DependsOn: []string{"review"}},
			}}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("continuation-%d", call), name, args)
				}
				result := func(previous int, name string) string {
					return aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("continuation-%d", previous), name)
				}
				readProposal := func(previous int) string {
					var value struct {
						ID string `json:"proposal_id"`
					}
					body := result(previous, "workspace_propose")
					if json.Unmarshal([]byte(body), &value) != nil || value.ID == "" {
						t.Errorf("real proposal result missing: %s", body)
					}
					return value.ID
				}
				if call >= 6 && call <= 18 && aiBudgetHasTool(payload, "workspace_agent_execution") {
					t.Error("continuation inherited the original message's execution scope")
				}
				if call <= 10 && strings.Contains(string(raw), report) {
					t.Error("Agent body entered chat before an explicit authorized Artifact read")
				}
				switch call {
				case 1:
					emit("workspace_guide", `{"topic":"agents"}`)
				case 2:
					emit("workspace_agent_execution", fmt.Sprintf(`{"task_id":%q}`, f.Task.ID))
				case 3:
					body := result(2, "workspace_agent_execution")
					if !strings.Contains(body, `"eligible":true`) || !strings.Contains(body, f.Provider.ID) {
						t.Errorf("initial execution qualification missing: %s", body)
					}
					emit("workspace_propose", aiAgentRunActionJSON(f.aiAgentRunFixture))
				case 4:
					startID = readProposal(3)
					plan.Steps[0].ProposalID = startID
					emit("workspace_plan", string(updatePlanArgs(plan, 0)))
				case 5:
					if body := result(4, "workspace_plan"); !strings.Contains(body, `"version":1`) || !strings.Contains(body, `"state":"pending"`) {
						t.Errorf("initial plan omitted pending approval: %s", body)
					}
					writeAIBudgetTextTurn(w, protocol, `已准备执行计划，等待你确认启动。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 6, 14:
					if aiBudgetHasTool(payload, "workspace_task_submissions") {
						t.Error("new automatic generation inherited deferred tool projection")
					}
					emit("workspace_plan", `{"operation":"read"}`)
				case 7:
					body := result(6, "workspace_plan")
					if !strings.Contains(body, runID) || !strings.Contains(body, `"state":"submitted"`) {
						t.Errorf("automatic turn did not observe the actual Run delivery: %s", body)
					}
					emit("workspace_guide", `{"topic":"outputs"}`)
				case 8:
					emit("workspace_task_submissions", fmt.Sprintf(`{"task_id":%q,"view":"list"}`, f.Task.ID))
				case 9:
					var view struct {
						TaskVersion int64  `json:"task_version"`
						TaskStatus  string `json:"task_status"`
						Current     string `json:"current_submission_id"`
						Items       []struct {
							ID     string `json:"id"`
							Status string `json:"status"`
						} `json:"items"`
					}
					body := result(8, "workspace_task_submissions")
					if json.Unmarshal([]byte(body), &view) != nil || view.TaskStatus != "waiting_review" || view.Current == "" || len(view.Items) != 1 || view.Items[0].ID != view.Current || view.Items[0].Status != "pending_review" {
						t.Errorf("automatic inspection lacks actual current submission: %s", body)
					}
					actualTaskVersion, submissionID = view.TaskVersion, view.Current
					emit("workspace_task_submissions", fmt.Sprintf(`{"task_id":%q,"submission_id":%q,"view":"artifacts"}`, f.Task.ID, submissionID))
				case 10:
					var view struct {
						Items []struct {
							ID   string `json:"id"`
							Kind string `json:"storage_kind"`
						} `json:"items"`
					}
					body := result(9, "workspace_task_submissions")
					if json.Unmarshal([]byte(body), &view) != nil || len(view.Items) != 1 || view.Items[0].Kind != "text" {
						t.Errorf("actual output artifact missing: %s", body)
					} else {
						artifactID = view.Items[0].ID
					}
					emit("workspace_get", fmt.Sprintf(`{"type":"artifact","id":%q}`, artifactID))
				case 11:
					body := result(10, "workspace_get")
					if !strings.Contains(body, report) || !strings.Contains(body, artifactID) {
						t.Errorf("automatic reviewer did not read actual selected Artifact: %s", body)
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"task.review","task_id":%q,"expected_version":%d,"changes":{"submission_id":%q,"decision":"accept","reason":"已读取实际报告，请你独立核验并确认验收。"}}`, f.Task.ID, actualTaskVersion, submissionID))
				case 12:
					reviewID = readProposal(11)
					plan.Steps[1].ProposalID = reviewID
					emit("workspace_plan", string(updatePlanArgs(plan, 1)))
				case 13:
					if body := result(12, "workspace_plan"); !strings.Contains(body, `"version":2`) || !strings.Contains(body, `"state":"pending"`) {
						t.Errorf("automatic plan did not keep human review pending: %s", body)
					}
					writeAIBudgetTextTurn(w, protocol, `已读取实际报告并准备验收建议；任务仍等待你亲自确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 15:
					if body := result(14, "workspace_plan"); !strings.Contains(body, reviewID) || !strings.Contains(body, `"state":"recorded"`) || !strings.Contains(body, `"status":"confirmed"`) {
						t.Errorf("second automatic turn lacks the human review receipt: %s", body)
					}
					emit("workspace_guide", `{"topic":"outputs"}`)
				case 16:
					emit("workspace_task_submissions", fmt.Sprintf(`{"task_id":%q,"view":"list"}`, f.Task.ID))
				case 17:
					body := result(16, "workspace_task_submissions")
					if !strings.Contains(body, `"task_status":"done"`) || !strings.Contains(body, `"status":"accepted"`) || !strings.Contains(body, submissionID) {
						t.Errorf("completion is not grounded in actual accepted Task facts: %s", body)
					}
					plan.Steps[2].Report = "reported_done"
					emit("workspace_plan", string(updatePlanArgs(plan, 2)))
				case 18:
					if body := result(17, "workspace_plan"); !strings.Contains(body, `"version":3`) || strings.Contains(body, `"satisfied":false`) {
						t.Errorf("final plan has unsatisfied actual evidence: %s", body)
					}
					writeAIBudgetTextTurn(w, protocol, `已重新确认：你的验收已生效，任务完成，计划结束。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 19:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("manual ungranted followup inherited lease capability %s", name)
						}
					}
					writeAIBudgetTextTurn(w, protocol, `本条未授权工作台访问。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected repeated model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Stop.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			initialBody, _ := json.Marshal(map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": "执行报告任务，完成后读取实际产出并准备验收建议；所有执行和验收必须由我确认。", "workspace": aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}})
			first := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", initialBody, nil)
			if first.Code != 200 || upstream.calls.Load() != 5 || !strings.Contains(first.Body.String(), "event: done") || strings.Contains(first.Body.String(), "event: error") {
				t.Fatalf("initial chat=%d calls=%d %s", first.Code, upstream.calls.Load(), first.Body.String())
			}
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM agent_runs`, 0)
			if f.Calls.Load() != 0 {
				t.Fatal("proposal started executor without human approval")
			}
			var start models.AIActionProposal
			if err := f.Store.DB.Take(&start, "id=?", startID).Error; err != nil {
				t.Fatal(err)
			}
			approved := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+startID+"/decision", []byte(fmt.Sprintf(`{"decision":"confirm","fingerprint":%q,"confirm_agent_execution":true}`, start.Fingerprint)), nil)
			if approved.Code != 200 {
				t.Fatalf("human start=%d %s", approved.Code, approved.Body.String())
			}
			if err := f.Store.DB.Take(&start, "id=?", startID).Error; err != nil || start.ResultID == nil {
				t.Fatal("actual approved Run missing")
			}
			runID = *start.ResultID
			var session models.AISession
			if err := f.Store.DB.Take(&session, "id=?", f.Generation.SessionID).Error; err != nil {
				t.Fatal(err)
			}
			armBody, _ := json.Marshal(createAIContinuationRequest{ExpectedSessionVersion: session.Version, ExpectedPlanVersion: 1, ProviderID: chatProvider.ID, ExpectedProviderVersion: chatProvider.Version, ExpectedProviderConfigVersion: chatProvider.ConfigVersion, Workspace: &aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: []string{"work", "outputs", "actions"}}, MaxTurns: 2, TTLMinutes: 30, ConfirmAutomaticContinuation: true})
			armed := performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+session.ID+"/continuation", armBody, nil)
			var envelope struct {
				Data aiContinuationResponse `json:"data"`
			}
			if armed.Code != 201 || json.Unmarshal(armed.Body.Bytes(), &envelope) != nil || envelope.Data.ID == "" || envelope.Data.Reason != "run_active" {
				t.Fatalf("explicit bounded authorization=%d %s", armed.Code, armed.Body.String())
			}
			leaseID := envelope.Data.ID
			for range 4 {
				coordinator.scan()
			}
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 0)
			if upstream.calls.Load() != 5 {
				t.Fatal("active Agent caused premature model continuation")
			}
			releaseOnce.Do(func() { close(releaseExecutor) })
			run := f.wait(t, runID)
			if run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil {
				t.Fatalf("actual output not durably submitted: %+v", run)
			}
			waitForLease := func(status, reason string, turns int) models.AIContinuation {
				t.Helper()
				deadline := time.Now().Add(12 * time.Second)
				var row models.AIContinuation
				for time.Now().Before(deadline) {
					if err := f.Store.DB.Take(&row, "id=?", leaseID).Error; err != nil {
						t.Fatal(err)
					}
					if row.Status == status && row.Reason == reason && row.TurnsStarted == turns {
						return row
					}
					if !continuationActive(row.Status) && row.Status != status {
						t.Fatalf("unexpected lease terminal=%+v calls=%d", row, upstream.calls.Load())
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("lease did not reach %s/%s/%d: %+v calls=%d", status, reason, turns, row, upstream.calls.Load())
				return row
			}
			firstAuto := waitForLease("waiting", "pending_approval", 1)
			if upstream.calls.Load() != 13 || submissionID != *run.SubmissionID || artifactID != *run.ArtifactID || firstAuto.CurrentPlanVersion != 2 {
				t.Fatalf("first automatic generation did not use actual evidence: calls=%d lease=%+v", upstream.calls.Load(), firstAuto)
			}
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL`, 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'`, 1, submissionID)
			for range 4 {
				coordinator.scan()
			}
			if upstream.calls.Load() != 13 {
				t.Fatal("pending approval consumed another automatic turn")
			}
			var review models.AIActionProposal
			if err := f.Store.DB.Take(&review, "id=?", reviewID).Error; err != nil || review.Status != "pending" {
				t.Fatalf("real human review proposal missing: %+v %v", review, err)
			}
			decisionPath := "/api/v1/ai/actions/" + review.ID + "/decision"
			missingConsent := performRequest(f.Router, http.MethodPost, decisionPath, []byte(fmt.Sprintf(`{"decision":"confirm","fingerprint":%q}`, review.Fingerprint)), nil)
			if missingConsent.Code != 422 {
				t.Fatalf("automatic reviewer bypassed independent confirmation: %d %s", missingConsent.Code, missingConsent.Body.String())
			}
			decision := []byte(fmt.Sprintf(`{"decision":"confirm","fingerprint":%q,"confirm_task_output":true}`, review.Fingerprint))
			for range 2 {
				confirmed := performRequest(f.Router, http.MethodPost, decisionPath, decision, nil)
				if confirmed.Code != 200 {
					t.Fatalf("independent human review=%d %s", confirmed.Code, confirmed.Body.String())
				}
			}
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM tasks WHERE id=? AND status='done'`, 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='accepted'`, 1, submissionID)
			final := waitForLease("completed", "plan_complete", 2)
			if final.CurrentPlanVersion != 3 || upstream.calls.Load() != 18 || f.Calls.Load() != 1 {
				t.Fatalf("two automatic generations not bounded: %+v chat=%d executor=%d", final, upstream.calls.Load(), f.Calls.Load())
			}
			var turns []models.AIContinuationTurn
			if err := f.Store.DB.Where("continuation_id=?", leaseID).Order("turn_index").Find(&turns).Error; err != nil || len(turns) != 2 {
				t.Fatalf("automatic ledger invalid: %+v %v", turns, err)
			}
			for index, turn := range turns {
				if turn.TurnIndex != index+1 || turn.PlanVersion != int64(index+1) {
					t.Fatalf("turn not bound to exact acceptance plan: %+v", turn)
				}
				assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_generations WHERE id=? AND session_id=? AND status='completed'`, 1, turn.GenerationID, session.ID)
				assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_messages WHERE generation_id=?`, 2, turn.GenerationID)
				assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_run_steps WHERE generation_id=? AND kind='generation' AND status='succeeded'`, 1, turn.GenerationID)
				assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='ai_workspace_access_granted' AND json_extract(current_json,'$.scopes') NOT LIKE '%agent_execution%'`, 1, turn.GenerationID)
				response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/generations/"+turn.GenerationID, nil, nil)
				if response.Code != 200 || !strings.Contains(response.Body.String(), leaseID) || !strings.Contains(response.Body.String(), `"kind":"plan_continuation"`) {
					t.Fatalf("automatic generation origin is not explicit: %d %s", response.Code, response.Body.String())
				}
			}
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_messages WHERE context_snapshot LIKE ?`, 0, "%CONCRETE_EXECUTOR_REPORT%")
			assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM agent_runs`, 1)
			for range 4 {
				coordinator.scan()
			}
			if upstream.calls.Load() != 18 {
				t.Fatal("completed lease restarted after duplicate wakeup")
			}
			manualBody, _ := json.Marshal(map[string]any{"provider_id": chatProvider.ID, "session_id": session.ID, "message": "谢谢"})
			manual := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", manualBody, nil)
			if manual.Code != 200 || upstream.calls.Load() != 19 || !strings.Contains(manual.Body.String(), "event: done") {
				t.Fatalf("manual followup=%d calls=%d %s", manual.Code, upstream.calls.Load(), manual.Body.String())
			}
		})
	}
}
