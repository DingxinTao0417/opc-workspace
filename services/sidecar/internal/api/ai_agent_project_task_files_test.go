package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

var projectTaskAgentScopes = []string{"work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files"}

// Both conversational encoders exercise discovery, proposal and plan receipt.
// The separately approved Agent executes through the real pipe subprocess;
// only its upstream model and the chat model are isolated HTTP fixtures.
func TestAIAgentProjectTaskFilesHarnessApprovalAndActualSuccessor(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f := newProjectTaskFilesFixture(t, func(_ int, request map[string]any, w http.ResponseWriter) {
				assertProjectTaskSelectedRequest(t, request)
				writeAnthropicAgentRunResponse(w, "Successor based on the selected accepted research.", "end_turn")
			})
			if protocol == "openai_chat" {
				f.useOpenAISuccessor(t, "local")
			}
			f.Native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			finishAIGeneration(t, f.Store, f.Generation)
			var candidateID, proposalID, runID string
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("project-input-%d", call), name, args)
				}
				for _, private := range []string{projectTaskSelectedBody, projectTaskUnselectedBody, anthropicRunTestKey, "objects/"} {
					if strings.Contains(string(raw), private) {
						t.Errorf("chat received file body/credential/path without output_files: %q", private)
					}
				}
				switch call {
				case 1:
					emit("workspace_guide", `{"topic":"agents"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_agent_project_files") {
						t.Error("agents guide did not discover separately authorized project file tool")
					}
					emit("workspace_agent_execution", fmt.Sprintf(`{"task_id":%q}`, f.Task.ID))
				case 3:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-2", "workspace_agent_execution")
					if !strings.Contains(result, f.Provider.ID) || !strings.Contains(result, `"eligible":true`) {
						t.Errorf("target execution eligibility missing: %s", result)
					}
					emit("workspace_agent_project_files", fmt.Sprintf(`{"task_id":%q,"query":"research.md"}`, f.Task.ID))
				case 4:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-3", "workspace_agent_project_files")
					var view struct {
						TaskID     string `json:"task_id"`
						Candidates []struct {
							ID          string         `json:"id"`
							CandidateID string         `json:"candidate_id"`
							SourceKind  string         `json:"source_kind"`
							SourceTask  map[string]any `json:"source_task"`
						} `json:"input_file_candidates"`
					}
					if json.Unmarshal([]byte(result), &view) != nil || view.TaskID != f.Task.ID || len(view.Candidates) != 1 {
						t.Errorf("real accepted-source lookup invalid: %s", result)
					} else {
						item := view.Candidates[0]
						candidateID = item.CandidateID
						if item.ID != "" || candidateID == "" || candidateID == f.SourceArtifact.ID || item.SourceKind != "project_task_artifact" || item.SourceTask["task_id"] != f.SourceTask.ID || item.SourceTask["submission_id"] != f.SourceSubmission.ID || item.SourceTask["task_version"] != float64(f.SourceTask.Version) {
							t.Errorf("candidate lacks opaque authorization or exact source: %s", result)
						}
					}
					emit("workspace_propose", aiAgentRunFileActionJSON(f.aiAgentRunFixture, []string{candidateID}, "text"))
				case 5:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-4", "workspace_propose")
					var proposal struct {
						ID string `json:"proposal_id"`
					}
					if json.Unmarshal([]byte(result), &proposal) != nil || proposal.ID == "" {
						t.Errorf("successor proposal missing: %s", result)
					}
					proposalID = proposal.ID
					plan := aiWorkPlanIntent{Title: "Use the accepted predecessor to produce a reviewable successor", Steps: []aiWorkPlanStep{{ID: "successor", Title: "Deliver the successor using only the explicitly selected source", Kind: "action", DependsOn: []string{}, ProposalID: proposalID}}}
					emit("workspace_plan", string(updatePlanArgs(plan, 0)))
				case 6:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-5", "workspace_plan")
					if strings.Contains(result, `"satisfied":true`) || !strings.Contains(result, `"state":"pending"`) {
						t.Errorf("pending suggestion claimed execution: %s", result)
					}
					writeAIBudgetTextTurn(w, protocol, `已准备后继执行建议；来源是已通过验收的指定调研文件，等待你的独立确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 7:
					emit("workspace_plan", `{"operation":"read"}`)
				case 8:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-7", "workspace_plan")
					if !strings.Contains(result, runID) || !strings.Contains(result, `"state":"submitted"`) || !strings.Contains(result, `"satisfied":true`) {
						t.Errorf("plan did not use actual successor receipt: %s", result)
					}
					emit("workspace_guide", `{"topic":"outputs"}`)
				case 9:
					emit("workspace_task_submissions", fmt.Sprintf(`{"task_id":%q,"view":"list"}`, f.Task.ID))
				case 10:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "project-input-9", "workspace_task_submissions")
					if !strings.Contains(result, `"status":"pending_review"`) || strings.Contains(result, `"status":"accepted"`) {
						t.Errorf("model was told successor had passed review: %s", result)
					}
					writeAIBudgetTextTurn(w, protocol, `后继产出已实际提交，仍需你人工验收。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 11:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("ungranted message inherited %s", name)
						}
					}
					writeAIBudgetTextTurn(w, protocol, `本轮没有工作台授权。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected chat call %d", call)
					writeAIBudgetTextTurn(w, protocol, `Unexpected.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			send := func(message string, scopes []string, want int32) {
				t.Helper()
				request := map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": message}
				if len(scopes) > 0 {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != 200 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || upstream.calls.Load() != want {
					t.Fatalf("chat calls=%d want=%d response=%d %s", upstream.calls.Load(), want, response.Code, response.Body.String())
				}
			}
			send("使用同项目已验收的 research.md，为后继任务准备执行建议，先让我确认。", projectTaskAgentScopes, 6)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			if f.Calls.Load() != 1 {
				t.Fatal("suggestion executed before independent human approval")
			}
			var proposal models.AIActionProposal
			if err := f.Store.DB.First(&proposal, "id=?", proposalID).Error; err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"execution_contract_version":6`, `"source_kind":"project_task_artifact"`, f.SourceTask.ID, f.SourceSubmission.ID, *f.SourceArtifact.SHA256} {
				if !strings.Contains(proposal.PreviewJSON, want) {
					t.Fatalf("approval omitted source disclosure %s: %s", want, proposal.PreviewJSON)
				}
			}
			for _, missing := range []string{"confirm_agent_execution", "confirm_agent_files", "confirm_agent_project_files"} {
				consent := map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true, "confirm_agent_files": true, "confirm_agent_project_files": true}
				delete(consent, missing)
				body, _ := json.Marshal(consent)
				response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
				if response.Code != 422 {
					t.Fatalf("approval without %s=%d %s", missing, response.Code, response.Body.String())
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			}
			consent, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true, "confirm_agent_files": true, "confirm_agent_project_files": true})
			response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", consent, nil)
			if response.Code != 200 {
				t.Fatalf("approved successor=%d %s", response.Code, response.Body.String())
			}
			if err := f.Store.DB.First(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil {
				t.Fatal("approval did not record real Run")
			}
			runID = *proposal.ResultID
			run := f.wait(t, runID)
			if run.ExecutionContractVersion != 6 || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || f.Calls.Load() != 2 {
				t.Fatalf("real successor never submitted: %#v", run)
			}
			response = performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", consent, nil)
			if response.Code != 200 {
				t.Fatalf("approval replay=%d %s", response.Code, response.Body.String())
			}
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 1, "plan_complete")
			send("重新读取计划和后继任务实际提交结果。", []string{"work", "outputs", "actions"}, 10)
			send("谢谢", nil, 11)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			if f.Calls.Load() != 2 {
				t.Fatal("replay/readback inherited execution")
			}
			f.assertSourceUnchanged(t)
		})
	}
}

func projectTaskFilesRegistry(t *testing.T, f *projectTaskFilesFixture, scopes []string) *harness.Registry {
	t.Helper()
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestAIAgentProjectTaskFilesRequireNewScopeAndGenerationCandidate(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("invalid scope/candidate must never execute")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	old := projectTaskFilesRegistry(t, f, []string{"work", "outputs", "actions", "agent_execution", "agent_files"})
	if _, ok := old.Get("workspace_agent_project_files"); ok {
		t.Fatal("old agent_files implicitly acquired cross-Task access")
	}
	registry := projectTaskFilesRegistry(t, f, projectTaskAgentScopes)
	tool, ok := registry.Get("workspace_agent_project_files")
	if !ok {
		t.Fatal("new explicitly granted tool missing")
	}
	result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"query":"research.md"}`, f.Task.ID)))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Candidates []struct {
			ID string `json:"candidate_id"`
		} `json:"input_file_candidates"`
	}
	if json.Unmarshal([]byte(result), &view) != nil || len(view.Candidates) != 1 || view.Candidates[0].ID == "" {
		t.Fatalf("no exact candidate: %s", result)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	nextGeneration := f.Generation
	nextGeneration.ID = uuid.NewString()
	nextGeneration.Status = "streaming"
	if err := f.Store.DB.Create(&nextGeneration).Error; err != nil {
		t.Fatal(err)
	}
	f.Generation = nextGeneration
	for _, candidate := range []string{f.SourceArtifact.ID, view.Candidates[0].ID} {
		fresh := projectTaskFilesRegistry(t, f, projectTaskAgentScopes)
		propose, _ := fresh.Get("workspace_propose")
		if _, err := propose.Execute(context.Background(), []byte(aiAgentRunFileActionJSON(f.aiAgentRunFixture, []string{candidate}, "text"))); err == nil {
			t.Fatal("fresh generation state accepted a guessed/previously issued candidate")
		}
	}
	for _, remove := range []string{"work", "outputs", "actions", "agent_execution", "agent_files"} {
		var scopes []string
		for _, scope := range projectTaskAgentScopes {
			if scope != remove {
				scopes = append(scopes, scope)
			}
		}
		if _, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID); err == nil {
			t.Errorf("agent_project_files grant accepted without %s", remove)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	if f.Calls.Load() != 1 {
		t.Fatal("candidate lookup or invalid proposal invoked executor")
	}
	f.assertSourceUnchanged(t)
}

func TestAIAgentProjectTaskFilesApprovalRechecksActualSource(t *testing.T) {
	for _, change := range []string{"source_version", "source_deleted"} {
		t.Run(change, func(t *testing.T) {
			f := newProjectTaskFilesFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("stale source approval must not call a model")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			f.Native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			registry := projectTaskFilesRegistry(t, f, projectTaskAgentScopes)
			query, _ := registry.Get("workspace_agent_project_files")
			result, err := query.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"query":"research.md"}`, f.Task.ID)))
			if err != nil {
				t.Fatal(err)
			}
			var candidates struct {
				Items []struct {
					ID string `json:"candidate_id"`
				} `json:"input_file_candidates"`
			}
			if json.Unmarshal([]byte(result), &candidates) != nil || len(candidates.Items) != 1 {
				t.Fatalf("candidate lookup=%s", result)
			}
			propose, _ := registry.Get("workspace_propose")
			result, err = propose.Execute(context.Background(), []byte(aiAgentRunFileActionJSON(f.aiAgentRunFixture, []string{candidates.Items[0].ID}, "text")))
			if err != nil {
				t.Fatal(err)
			}
			var created struct {
				ID string `json:"proposal_id"`
			}
			if json.Unmarshal([]byte(result), &created) != nil || created.ID == "" {
				t.Fatalf("proposal=%s", result)
			}
			finishAIGeneration(t, f.Store, f.Generation)
			var proposal models.AIActionProposal
			if err := f.Store.DB.First(&proposal, "id=?", created.ID).Error; err != nil {
				t.Fatal(err)
			}
			if change == "source_version" {
				response := performRequest(f.Router, http.MethodPatch, "/api/v1/tasks/"+f.SourceTask.ID, []byte(`{"title":"Owner revised the accepted source title"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version)})
				if response.Code != 200 {
					t.Fatalf("source update=%d %s", response.Code, response.Body.String())
				}
			} else {
				response := performRequest(f.Router, http.MethodDelete, "/api/v1/artifacts/"+f.SourceArtifact.ID+"?confirm=true", []byte(`{"reason":"owner removed source before approval"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version), "Idempotency-Key": "before-source-approval"})
				if response.Code != 200 && response.Code != 204 {
					t.Fatalf("source delete=%d %s", response.Code, response.Body.String())
				}
			}
			body, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true, "confirm_agent_files": true, "confirm_agent_project_files": true})
			response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
			if response.Code != 409 && response.Code != 422 {
				t.Fatalf("stale source approval accepted=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='todo'", 1, f.Task.ID, f.Task.Version)
			if f.Calls.Load() != 1 {
				t.Fatal("stale human confirmation executed a changed source")
			}
		})
	}
}
