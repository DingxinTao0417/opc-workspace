package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Both chat encoders drive the real Harness, while the independently approved
// Agent always uses a real executor subprocess against the isolated Messages
// endpoint. These are not fake success replies standing in for an Agent Run.
func TestAIAgentRunAnthropicHarnessApprovalPlanAndActualSubmission(t *testing.T) {
	for _, chatProtocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(chatProtocol, func(t *testing.T) {
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				writeAnthropicAgentRunResponse(w, "The requested Agent deliverable is complete and ready for owner review.", "end_turn")
			})
			f.Native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			finishAIGeneration(t, f.Store, f.Generation)
			var proposalID, runID string
			upstream := newAIBudgetMockUpstream(t, chatProtocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, chatProtocol, fmt.Sprintf("anthropic-agent-%d", call), name, args)
				}
				if strings.Contains(string(raw), anthropicRunTestKey) {
					t.Error("execution credential leaked into the chat model")
				}
				switch call {
				case 1:
					emit("workspace_guide", `{"topic":"agents"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_agent_execution") {
						t.Error("guide did not expose the authorized execution eligibility tool")
					}
					emit("workspace_agent_execution", fmt.Sprintf(`{"task_id":%q}`, f.Task.ID))
				case 3:
					result := aiPlanRetryHarnessResult(t, payload, chatProtocol, "anthropic-agent-2", "workspace_agent_execution")
					if !strings.Contains(result, f.Provider.ID) || !strings.Contains(result, `"protocol":"anthropic_messages"`) || !strings.Contains(result, `"eligible":true`) {
						t.Errorf("actual eligibility did not offer the ready Anthropic provider: %s", result)
					}
					emit("workspace_propose", aiAgentRunActionJSON(f.aiAgentRunFixture))
				case 4:
					var result struct {
						ID string `json:"proposal_id"`
					}
					content := aiPlanRetryHarnessResult(t, payload, chatProtocol, "anthropic-agent-3", "workspace_propose")
					if json.Unmarshal([]byte(content), &result) != nil || result.ID == "" {
						t.Errorf("real execution proposal missing: %s", content)
					}
					proposalID = result.ID
					plan := aiWorkPlanIntent{Title: "Submit the Anthropic Agent deliverable", Steps: []aiWorkPlanStep{
						{ID: "execute", Title: "Create a real reviewed deliverable", Kind: "action", DependsOn: []string{}, ProposalID: proposalID},
					}}
					emit("workspace_plan", string(updatePlanArgs(plan, 0)))
				case 5:
					content := aiPlanRetryHarnessResult(t, payload, chatProtocol, "anthropic-agent-4", "workspace_plan")
					if !strings.Contains(content, `"state":"pending"`) || strings.Contains(content, `"satisfied":true`) {
						t.Errorf("unapproved plan claimed completed work: %s", content)
					}
					writeAIBudgetTextTurn(w, chatProtocol, `执行建议等待你单独确认，尚未运行。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 6:
					emit("workspace_plan", `{"operation":"read"}`)
				case 7:
					content := aiPlanRetryHarnessResult(t, payload, chatProtocol, "anthropic-agent-6", "workspace_plan")
					if !strings.Contains(content, runID) || !strings.Contains(content, `"state":"submitted"`) || !strings.Contains(content, `"satisfied":true`) {
						t.Errorf("fresh read is not backed by the actual submitted Run: %s", content)
					}
					writeAIBudgetTextTurn(w, chatProtocol, `产出已提交，任务仍待人工验收。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 8:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new ungranted message inherited tool %s", name)
						}
					}
					if strings.Contains(string(raw), "The requested Agent deliverable") {
						t.Error("ungranted message inherited execution output")
					}
					writeAIBudgetTextTurn(w, chatProtocol, `本轮没有工作台授权。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected chat call %d", call)
					writeAIBudgetTextTurn(w, chatProtocol, `Unexpected call.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, chatProtocol)
			send := func(message string, scopes []string, wantCalls int32) {
				t.Helper()
				request := map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": message}
				if len(scopes) > 0 {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") || upstream.calls.Load() != wantCalls {
					t.Fatalf("chat failed: calls=%d want=%d response=%d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
			}
			send("请准备任务执行建议，先让我确认。", []string{"work", "outputs", "actions", "agent_execution"}, 5)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			if f.Calls.Load() != 0 {
				t.Fatal("a model proposal executed Anthropic before human approval")
			}
			var proposal models.AIActionProposal
			if err := f.Store.DB.First(&proposal, "id=?", proposalID).Error; err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{`"execution_contract_version":5`, `"protocol":"anthropic_messages"`, `"max_output_tokens":8192`, `"leaves_device":true`} {
				if !strings.Contains(proposal.PreviewJSON, field) {
					t.Fatalf("approval does not disclose exact protocol/limits/device boundary %s: %s", field, proposal.PreviewJSON)
				}
			}
			missing, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm"})
			response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", missing, nil)
			assertAPIError(t, response, http.StatusUnprocessableEntity, "AGENT_EXECUTION_CONFIRMATION_REQUIRED")
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			approval, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true})
			response = performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", approval, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("human approval=%d: %s", response.Code, response.Body.String())
			}
			if err := f.Store.DB.First(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil || proposal.Status != "confirmed" {
				t.Fatalf("human approval did not create the real Run: %#v %v", proposal, err)
			}
			runID = *proposal.ResultID
			run := f.wait(t, runID)
			if run.ExecutionContractVersion != 5 || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil || f.Calls.Load() != 1 {
				t.Fatalf("confirmed Anthropic Agent never delivered real output: %#v", run)
			}
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 1, "plan_complete")
			send("重新读取实际执行结果。", []string{"work", "outputs", "actions"}, 7)
			send("谢谢", nil, 8)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			if f.Calls.Load() != 1 {
				t.Fatal("readback or missing scope started another execution")
			}
		})
	}
}

func anthropicActionTool(t *testing.T, f *anthropicAgentRunFixture, scopes ...string) harness.Tool {
	t.Helper()
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("authorized proposal tool missing")
	}
	return tool
}

func TestAIAgentRunAnthropicApprovalRechecksProtocolAndScopes(t *testing.T) {
	for _, drift := range []string{"protocol", "model"} {
		t.Run(drift, func(t *testing.T) {
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("changed approval must not invoke model HTTP")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			f.Native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			plain := anthropicActionTool(t, f, "work", "outputs", "actions")
			if _, err := plain.Execute(context.Background(), []byte(aiAgentRunActionJSON(f.aiAgentRunFixture))); err == nil {
				t.Fatal("Anthropic execution bypassed explicit agent_execution scope")
			}
			tool := anthropicActionTool(t, f, "work", "outputs", "actions", "agent_execution")
			if _, err := tool.Execute(context.Background(), []byte(aiAgentRunFileActionJSON(f.aiAgentRunFixture, nil, "file"))); err == nil {
				t.Fatal("Anthropic file output bypassed independent agent_files scope")
			}
			proposal := proposeTestAction(t, f.Store, tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
			finishAIGeneration(t, f.Store, f.Generation)
			changes := map[string]any{"version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1}
			if drift == "protocol" {
				changes["protocol"] = "openai_chat"
			} else {
				changes["model"] = "changed-execution-model"
			}
			if err := f.Store.DB.Model(&f.Provider).Updates(changes).Error; err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true})
			response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
			if response.Code != http.StatusConflict {
				t.Fatalf("changed execution approval=%d: %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
			if f.Calls.Load() != 0 {
				t.Fatalf("changed approval reached model %d times", f.Calls.Load())
			}
		})
	}
}
