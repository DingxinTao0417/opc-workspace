package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Read the actual paired tool result, not a fabricated model reply or a DB ID
// that the model never received. Both supported provider encoders are exercised.
func aiPlanRetryHarnessResult(t *testing.T, payload map[string]any, protocol, id, name string) string {
	t.Helper()
	messages, _ := payload["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if protocol == "openai_chat" && message["role"] == "tool" && message["tool_call_id"] == id {
			content, _ := message["content"].(string)
			return strings.TrimPrefix(content, name+": ")
		}
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if protocol == "anthropic_messages" && message["role"] == "user" && block["type"] == "tool_result" && block["tool_use_id"] == id {
				content, _ := block["content"].(string)
				return strings.TrimPrefix(content, name+": ")
			}
		}
	}
	t.Errorf("missing actual %s result for %s", name, id)
	return ""
}

// The Harness and approval/finalization/recovery code are real. Executor process
// launch is deliberately disabled, as in the Agent retry approval fixture; a
// controlled executor completion invokes the same durable finalization command.
// This is deterministic integration coverage, not a native Runner acceptance.
func TestAIWorkPlanHarnessFailedAgentRetryUsesActualChildDelivery(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			addAgentRunOwnerReviewer(t, f)
			finishAIGeneration(t, f.Store, f.Generation)
			stopped, cancel := context.WithCancel(context.Background())
			cancel()
			f.Service.agentRunLifecycleContext = stopped
			approvals := gin.New()
			approvals.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)

			var originalID, retryID, originalRunID string
			var plan aiWorkPlanIntent
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name string, args []byte) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("agent-plan-%d", call), name, string(args))
				}
				proposalID := func(id string) string {
					var result struct {
						ID string `json:"proposal_id"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, id, "workspace_propose")
					if json.Unmarshal([]byte(content), &result) != nil || result.ID == "" {
						t.Errorf("proposal not delivered to model: %s", content)
					}
					return result.ID
				}
				switch call {
				case 1, 7:
					emit("workspace_guide", []byte(`{"topic":"agents"}`))
				case 2:
					if !aiBudgetHasTool(payload, "workspace_agent_execution") {
						t.Error("Agent guide did not expose authorized preparation tools")
					}
					emit("workspace_propose", []byte(aiAgentRunActionJSON(f)))
				case 3:
					originalID = proposalID("agent-plan-2")
					plan = aiWorkPlanIntent{Title: "Exact Agent retry plan", Steps: []aiWorkPlanStep{{
						ID: "original", Title: "Submit the Agent report", Kind: "action", DependsOn: []string{}, ProposalID: originalID,
					}}}
					emit("workspace_plan", updatePlanArgs(plan, 0))
				case 4:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "agent-plan-3", "workspace_plan")
					if !strings.Contains(content, `"version":1`) || !strings.Contains(content, `"state":"pending"`) {
						t.Errorf("initial plan did not preserve pending approval: %s", content)
					}
					writeAIBudgetTextTurn(w, protocol, `启动建议等待人工确认，尚未执行。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 5, 11:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("ungranted message inherited %s", name)
						}
					}
					if strings.Contains(string(raw), plan.Title) || strings.Contains(string(raw), "PRIVATE EXECUTOR OUTPUT") {
						t.Error("ungranted message inherited plan intent or executor output")
					}
					writeAIBudgetTextTurn(w, protocol, `本轮没有工作台授权。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 6:
					emit("workspace_plan", []byte(`{"operation":"read"}`))
				case 8:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "agent-plan-6", "workspace_plan")
					if !strings.Contains(content, `"state":"failed"`) || !strings.Contains(content, originalRunID) {
						t.Errorf("retry did not read the actual failed Run: %s", content)
					}
					emit("workspace_propose", []byte(fmt.Sprintf(`{"action":"agent_run.retry","task_id":%q,"agent_run_id":%q,"expected_version":%d,"changes":{}}`, f.Task.ID, originalRunID, f.Task.Version)))
				case 9:
					retryID = proposalID("agent-plan-8")
					plan.Steps = append(plan.Steps, aiWorkPlanStep{ID: "retry", Title: "Retry the same failed report", Kind: "action", DependsOn: []string{}, ProposalID: retryID, Replaces: "original"})
					emit("workspace_plan", updatePlanArgs(plan, 1))
				case 10:
					content := aiPlanRetryHarnessResult(t, payload, protocol, "agent-plan-9", "workspace_plan")
					if !strings.Contains(content, `"version":2`) || !strings.Contains(content, `"superseded_by":"retry"`) || !strings.Contains(content, `"retry_of_run_id":"`+originalRunID+`"`) {
						t.Errorf("exact retry did not replace the failed step with retained evidence: %s", content)
					}
					writeAIBudgetTextTurn(w, protocol, `重试建议等待人工确认，原失败记录保留。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Unexpected call.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			grant := aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
			send := func(message string, authorized bool, wantCalls int) {
				t.Helper()
				request := map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": message}
				if authorized {
					request["workspace"] = grant
				}
				body, _ := json.Marshal(request)
				response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || upstream.calls.Load() != int32(wantCalls) || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") {
					t.Fatalf("chat calls=%d want=%d response=%d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
			}
			loadProposal := func(id string) models.AIActionProposal {
				t.Helper()
				var proposal models.AIActionProposal
				if err := f.Store.DB.First(&proposal, "id=?", id).Error; err != nil {
					t.Fatal(err)
				}
				return proposal
			}
			loadRun := func(id string) models.AgentRun {
				t.Helper()
				var run models.AgentRun
				if err := f.Store.DB.First(&run, "id=?", id).Error; err != nil {
					t.Fatal(err)
				}
				return run
			}
			confirm := func(id string) models.AgentRun {
				t.Helper()
				proposal := loadProposal(id)
				body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, proposal.Fingerprint))
				response := performRequest(approvals, http.MethodPost, "/api/v1/ai/actions/"+id+"/decision", body, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("confirm %d: %s", response.Code, response.Body.String())
				}
				proposal = loadProposal(id)
				if proposal.ResultID == nil {
					t.Fatal("confirmed action has no Run result")
				}
				return loadRun(*proposal.ResultID)
			}
			inboxState := func(want string, superseded int) {
				t.Helper()
				response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/work-plans?state=all", nil, nil)
				var envelope struct {
					Data []aiWorkPlanInboxItem `json:"data"`
				}
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 1 || envelope.Data[0].State != want || envelope.Data[0].SupersededStepTotal != superseded {
					t.Fatalf("inbox want=%s superseded=%d: %d %s", want, superseded, response.Code, response.Body.String())
				}
			}
			claim := func(run models.AgentRun) models.AgentRun {
				t.Helper()
				if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
					t.Fatal(err)
				}
				return loadRun(run.ID)
			}

			send("准备提交 Agent 报告，先请求我确认启动。", true, 4)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertAIPlanContinuation(t, f, 1, "pending_approval", loadProposal(originalID).GenerationID)
			original := confirm(originalID)
			originalRunID = original.ID
			assertAIPlanContinuation(t, f, 1, "run_active")
			original = claim(original)
			if err := f.Service.finalizeAgentRun(original, "", "AGENT_EXECUTION_FAILED", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			original = loadRun(original.ID)
			originalProposal := loadProposal(originalID)
			if original.Status != "failed" || original.OutputDeliveryStatus != agentRunOutputNotReady || original.ErrorCode == nil {
				t.Fatalf("controlled failed completion not persisted: %+v", original)
			}
			var firstRevision aiWorkPlanRevision
			if err := f.Store.DB.First(&firstRevision, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil {
				t.Fatal(err)
			}
			assertAIPlanContinuation(t, f, 1, "ready")
			inboxState("awaiting_continuation", 0)
			send("谢谢", false, 5)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			send("检查失败记录，仅为同一次执行准备重试建议并更新计划；不要替我确认。", true, 10)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
			pending := assertAIPlanContinuation(t, f, 2, "pending_approval", loadProposal(retryID).GenerationID)
			if len(pending.Steps) != 2 || pending.Steps[0].State != "failed" || pending.Steps[0].SupersededBy != "retry" || pending.Steps[0].Satisfied || pending.Steps[1].Satisfied {
				t.Fatalf("pending retry rewrote failure or claimed success: %+v", pending)
			}
			inboxState("needs_approval", 1)

			child := confirm(retryID)
			if child.Status != "queued" || child.ParentRunID == nil || *child.ParentRunID != original.ID || child.Attempt != original.Attempt+1 || child.InputSnapshotJSON != original.InputSnapshotJSON || child.ProviderID != original.ProviderID || child.ActorID != original.ActorID || child.AssignmentID != original.AssignmentID {
				t.Fatalf("retry is not an exact frozen child: %+v", child)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertAIPlanContinuation(t, f, 2, "run_active")
			inboxState("running", 1)
			child = claim(child)
			assertAIPlanContinuation(t, f, 2, "run_active")
			if err := f.Store.DB.Exec(`CREATE TRIGGER fail_plan_retry_delivery BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted' BEGIN SELECT RAISE(ABORT,'controlled delivery failure'); END`).Error; err != nil {
				t.Fatal(err)
			}
			const output = "PRIVATE EXECUTOR OUTPUT: complete reviewable report"
			err := f.Service.finalizeAgentRun(child, output, "", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano))
			if !errors.Is(err, errAgentRunOutputDeliveryPending) {
				t.Fatalf("delivery must remain recoverable: %v", err)
			}
			pending = assertAIPlanContinuation(t, f, 2, "output_pending")
			if pending.Steps[1].State != "output_pending" || pending.Steps[1].Satisfied {
				t.Fatal("pending delivery falsely completed the retry plan")
			}
			inboxState("needs_recovery", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			if err := f.Store.DB.Exec("DROP TRIGGER fail_plan_retry_delivery").Error; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				response := performRequest(f.Router, http.MethodPost, "/api/v1/agent-runs/"+child.ID+"/output-delivery/retry", nil, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("recover actual output: %d %s", response.Code, response.Body.String())
				}
			}
			child = loadRun(child.ID)
			if child.Status != "succeeded" || child.OutputDeliveryStatus != agentRunOutputSubmitted || child.ResultText == nil || *child.ResultText != output || child.SubmissionID == nil || child.ArtifactID == nil {
				t.Fatalf("actual child output not submitted: %+v", child)
			}
			complete := assertAIPlanContinuation(t, f, 2, "plan_complete")
			if complete.Steps[0].State != "failed" || complete.Steps[0].Satisfied || complete.Steps[1].State != "submitted" || !complete.Steps[1].Satisfied {
				t.Fatalf("completion is not sourced from child delivery: %+v", complete)
			}
			inboxState("completed", 1)
			var task models.Task
			if err := f.Store.DB.First(&task, "id=?", f.Task.ID).Error; err != nil || task.Status != "waiting_review" || task.CompletedAt != nil {
				t.Fatalf("plan submission incorrectly completed the Task: %+v err=%v", task, err)
			}
			var revisionAgain aiWorkPlanRevision
			if err := f.Store.DB.First(&revisionAgain, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil || !reflect.DeepEqual(firstRevision, revisionAgain) {
				t.Fatalf("immutable source revision changed: %+v err=%v", revisionAgain, err)
			}
			if !reflect.DeepEqual(original, loadRun(original.ID)) || !reflect.DeepEqual(originalProposal, loadProposal(originalID)) {
				t.Fatal("retry mutated the failed Run or its confirmed action receipt")
			}
			history := performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan?version=1", nil, nil)
			if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"state":"failed"`) || strings.Contains(history.Body.String(), "superseded_by") || strings.Contains(history.Body.String(), child.ID) {
				t.Fatalf("historical plan silently rewritten: %s", history.Body.String())
			}
			send("谢谢", false, 11)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='confirmed'", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%PRIVATE EXECUTOR OUTPUT%'", 0)
		})
	}
}
