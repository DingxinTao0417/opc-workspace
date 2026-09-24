package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITaskReviewPolicyExistingTaskProposal(t *testing.T) {
	f := newAIAgentRunFixture(t)
	changed := performRequest(f.Router, http.MethodPatch, "/api/v1/tasks/"+f.Task.ID,
		[]byte(`{"review_policy":"none"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if changed.Code != http.StatusOK {
		t.Fatalf("initialize legal none-policy task: %d %s", changed.Code, changed.Body.String())
	}
	var task models.Task
	if err := f.Store.DB.First(&task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	args := []byte(fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual"}}`, task.ID, task.Version))
	if output, err := f.Tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("existing task review policy proposal unavailable: %s %v", output, err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='none' AND version=?", 1, task.ID, task.Version)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
}

// Exercise actual reads, proposals, human approvals and output submission. The
// local model only emits tool calls. A controlled completion uses the real Run
// finalizer; no executor subprocess or real model service is started here.
func TestAITaskReviewPolicyHarnessApprovalThenAgentSubmission(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			addAgentRunOwnerReviewer(t, f)
			initialized := performRequest(f.Router, http.MethodPatch, "/api/v1/tasks/"+f.Task.ID,
				[]byte(`{"review_policy":"none"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
			if initialized.Code != http.StatusOK {
				t.Fatalf("initialize existing none Task: %d %s", initialized.Code, initialized.Body.String())
			}
			if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if f.Task.ReviewPolicy != "none" || f.Task.Status != "todo" {
				t.Fatal("initial task is not a legal none-policy todo")
			}
			baseVersion := f.Task.Version
			finishAIGeneration(t, f.Store, f.Generation)
			stopped, cancel := context.WithCancel(context.Background())
			cancel()
			f.Service.agentRunLifecycleContext = stopped
			approvals := gin.New()
			approvals.Use(requestIDMiddleware())
			approvals.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)

			var observedVersion int64
			var policyProposalID, startProposalID string
			const privateOutput = "PRIVATE REVIEWABLE OUTPUT: report body kept out of chat"
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name string, arguments []byte) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("policy-%d", call), name, string(arguments))
				}
				readTaskResult := func(id, wantPolicy, wantStatus string, wantAllowed bool) {
					var output struct {
						Record aiBusinessContextSource `json:"record"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, id, "workspace_get")
					if json.Unmarshal([]byte(content), &output) != nil || output.Record.ID != f.Task.ID || output.Record.Version < 1 || output.Record.Fields["review_policy"] != wantPolicy || output.Record.Fields["status"] != wantStatus || output.Record.Fields["review_policy_change_allowed"] != wantAllowed {
						t.Errorf("task policy read must be authoritative (%s/%s/%v): %s", wantPolicy, wantStatus, wantAllowed, content)
					}
					observedVersion = output.Record.Version
				}
				readProposalID := func(id string) string {
					var output struct {
						ID string `json:"proposal_id"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, id, "workspace_propose")
					if json.Unmarshal([]byte(content), &output) != nil || output.ID == "" {
						t.Errorf("missing real pending proposal: %s", content)
					}
					return output.ID
				}
				switch call {
				case 1, 6, 12:
					emit("workspace_get", []byte(fmt.Sprintf(`{"type":"task","id":%q}`, f.Task.ID)))
				case 2:
					readTaskResult("policy-1", "none", "todo", true)
					if observedVersion != baseVersion {
						t.Errorf("initial Task version=%d want=%d", observedVersion, baseVersion)
					}
					emit("workspace_guide", []byte(`{"topic":"tasks_projects"}`))
				case 3:
					emit("workspace_propose", []byte(fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual"}}`, f.Task.ID, observedVersion)))
				case 4:
					policyProposalID = readProposalID("policy-3")
					writeAIBudgetTextTurn(w, protocol, `验收策略修改建议等待你的确认，任务尚未修改。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 5, 11:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new message inherited %s without fresh consent", name)
						}
					}
					if strings.Contains(string(raw), privateOutput) {
						t.Error("executor output entered an ungranted chat")
					}
					writeAIBudgetTextTurn(w, protocol, `本轮未授权读取或修改工作台。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 7:
					readTaskResult("policy-6", "manual", "todo", true)
					if observedVersion != baseVersion+1 {
						t.Errorf("fresh Task version=%d want=%d", observedVersion, baseVersion+1)
					}
					emit("workspace_guide", []byte(`{"topic":"agents"}`))
				case 8:
					if !aiBudgetHasTool(payload, "workspace_agent_execution") {
						t.Error("Agent discovery did not expose the separately granted tool")
					}
					emit("workspace_agent_execution", []byte(fmt.Sprintf(`{"task_id":%q}`, f.Task.ID)))
				case 9:
					var eligibility struct {
						Eligible  bool                        `json:"eligible"`
						Task      aiAgentRunEligibilityTask   `json:"task"`
						Providers []aiAgentRunProviderPreview `json:"providers"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "policy-8", "workspace_agent_execution")
					if json.Unmarshal([]byte(content), &eligibility) != nil || !eligibility.Eligible || eligibility.Task.ID != f.Task.ID || eligibility.Task.Version != observedVersion || eligibility.Task.ReviewPolicy != "manual" {
						t.Errorf("Agent eligibility ignored approved policy: %s", content)
					}
					var executionProvider aiAgentRunProviderPreview
					for _, candidate := range eligibility.Providers {
						if candidate.ID == f.Provider.ID {
							executionProvider = candidate
						}
					}
					if executionProvider.ID == "" {
						t.Error("real execution Provider was not returned by eligibility")
					}
					emit("workspace_propose", []byte(fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d}}`, eligibility.Task.ID, eligibility.Task.Version, executionProvider.ID, executionProvider.Version, executionProvider.ConfigVersion)))
				case 10:
					startProposalID = readProposalID("policy-9")
					writeAIBudgetTextTurn(w, protocol, `任务已要求人工验收；启动 Agent 仍需你单独确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 13:
					readTaskResult("policy-12", "manual", "waiting_review", false)
					if aiBudgetHasTool(payload, "workspace_propose") || aiBudgetHasTool(payload, "workspace_agent_execution") || strings.Contains(string(raw), privateOutput) {
						t.Error("read-only follow-up inherited execution, writes or output content")
					}
					writeAIBudgetTextTurn(w, protocol, `产出已提交并等待人工验收，任务尚未完成。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Unexpected call.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			send := func(message string, scopes []string, wantCalls int) {
				t.Helper()
				request := map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": message}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: scopes}
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
			confirm := func(id string, agentConsent bool) models.AIActionProposal {
				t.Helper()
				proposal := loadProposal(id)
				decision := map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm"}
				if agentConsent {
					decision["confirm_agent_execution"] = true
				}
				body, _ := json.Marshal(decision)
				for range 2 { // Replayed human click remains idempotent.
					response := performRequest(approvals, http.MethodPost, "/api/v1/ai/actions/"+id+"/decision", body, nil)
					if response.Code != http.StatusOK {
						t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
					}
				}
				return loadProposal(id)
			}

			send("读取这个现有任务，把它改为需要人工验收；先给我修改建议。", []string{"work", "actions"}, 4)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='none' AND version=?", 1, f.Task.ID, baseVersion)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 1) // Native fixture setup only.
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			proposal := loadProposal(policyProposalID)
			var preview aiActionPreview
			if json.Unmarshal([]byte(proposal.PreviewJSON), &preview) != nil || preview.Before["review_policy"] != "none" || preview.After["review_policy"] != "manual" {
				t.Fatalf("human preview lost before/after policy: %s", proposal.PreviewJSON)
			}
			proposal = confirm(policyProposalID, false)
			if proposal.ResultID == nil || *proposal.ResultID != f.Task.ID || proposal.ResultVersion == nil || *proposal.ResultVersion != baseVersion+1 {
				t.Fatalf("approval did not bind actual updated Task/version: %+v", proposal)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='manual' AND status='todo' AND version=?", 1, f.Task.ID, baseVersion+1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			send("谢谢", nil, 5)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			send("重新核对任务和 Agent 执行条件，给我单独的启动确认建议。", []string{"work", "outputs", "actions", "agent_execution"}, 10)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			start := loadProposal(startProposalID)
			withoutConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, start.Fingerprint))
			assertAPIError(t, performRequest(approvals, http.MethodPost, "/api/v1/ai/actions/"+start.ID+"/decision", withoutConsent, nil), http.StatusUnprocessableEntity, "AGENT_EXECUTION_CONFIRMATION_REQUIRED")
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			start = confirm(start.ID, true)
			if start.ResultID == nil {
				t.Fatal("Agent confirmation did not create a Run")
			}
			var run models.AgentRun
			if err := f.Store.DB.First(&run, "id=?", *start.ResultID).Error; err != nil || run.Status != "queued" || run.TaskVersion != baseVersion+1 {
				t.Fatalf("Run did not freeze approved Task version: %+v err=%v", run, err)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Service.finalizeAgentRun(run, privateOutput, "", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil || run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil {
				t.Fatalf("actual output was not submitted: %+v err=%v", run, err)
			}
			var task models.Task
			if err := f.Store.DB.First(&task, "id=?", f.Task.ID).Error; err != nil || task.ReviewPolicy != "manual" || task.Status != "waiting_review" || task.CompletedAt != nil || task.CurrentSubmissionID == nil || *task.CurrentSubmissionID != *run.SubmissionID {
				t.Fatalf("Agent submission falsely completed Task: %+v err=%v", task, err)
			}
			var submission models.TaskSubmission
			if err := f.Store.DB.First(&submission, "id=?", *run.SubmissionID).Error; err != nil || submission.Status != "pending_review" || submission.SubmittedByActorID != f.Actor.ID {
				t.Fatalf("submission bypassed human review: %+v err=%v", submission, err)
			}
			var artifact models.TaskArtifact
			if err := f.Store.DB.First(&artifact, "id=?", *run.ArtifactID).Error; err != nil || artifact.ContentText == nil || *artifact.ContentText != privateOutput || artifact.SubmissionID != submission.ID {
				t.Fatalf("actual reviewable output not preserved: %+v err=%v", artifact, err)
			}
			send("谢谢", nil, 11)
			send("只读查看任务现在是否仍等待人工验收。", []string{"work"}, 13)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_output_submitted' AND agent_run_id=?", 1, run.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%PRIVATE REVIEWABLE OUTPUT%'", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 1)
		})
	}
}
