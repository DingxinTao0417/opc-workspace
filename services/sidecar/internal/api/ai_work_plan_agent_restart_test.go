package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiPlanRestartFixture struct {
	aiAgentRunFixture
	Approvals http.Handler
	Plan      harness.Tool
}

func newAIPlanRestartFixture(t *testing.T) *aiPlanRestartFixture {
	t.Helper()
	f := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, f)
	// Only process scheduling is disabled. Proposals, explicit human decisions,
	// native Run creation, finalization, and plan projections are real.
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = stopped
	approvals := gin.New()
	approvals.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
	return &aiPlanRestartFixture{aiAgentRunFixture: f, Approvals: approvals, Plan: planTool(t, f)}
}

func (f *aiPlanRestartFixture) nextGeneration(t *testing.T) {
	t.Helper()
	finishAIGeneration(t, f.Store, f.Generation)
	f.Generation.ID, f.Generation.Status = uuid.NewString(), "streaming"
	if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	f.Tool, ok = registry.Get("workspace_propose")
	if !ok {
		t.Fatal("fresh explicit execution grant has no proposal tool")
	}
	f.Plan = planTool(t, f.aiAgentRunFixture)
}

func (f *aiPlanRestartFixture) loadRun(t *testing.T, id string) models.AgentRun {
	t.Helper()
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *aiPlanRestartFixture) confirm(t *testing.T, proposal models.AIActionProposal, extra string) models.AgentRun {
	t.Helper()
	finishAIGeneration(t, f.Store, f.Generation)
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true%s}`, proposal.Fingerprint, extra))
	response := performRequest(f.Approvals, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("human confirmation=%d: %s", response.Code, response.Body.String())
	}
	if err := f.Store.DB.First(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil || proposal.Status != "confirmed" {
		t.Fatalf("actual confirmed Run missing: %#v err=%v", proposal, err)
	}
	return f.loadRun(t, *proposal.ResultID)
}

func (f *aiPlanRestartFixture) claim(t *testing.T, run models.AgentRun) models.AgentRun {
	t.Helper()
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	return f.loadRun(t, run.ID)
}

func (f *aiPlanRestartFixture) changeTask(t *testing.T) {
	t.Helper()
	response := performRequest(f.Router, http.MethodPatch, "/api/v1/tasks/"+f.Task.ID,
		[]byte(`{"description":"Updated requirements after the original execution was frozen"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if response.Code != http.StatusOK {
		t.Fatalf("native task update=%d: %s", response.Code, response.Body.String())
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func aiPlanRestartArgs(f aiAgentRunFixture, originalID string) string {
	var action map[string]any
	_ = json.Unmarshal([]byte(aiAgentRunActionJSON(f)), &action)
	action["changes"].(map[string]any)["restart_of_run_id"] = originalID
	encoded, _ := json.Marshal(action)
	return string(encoded)
}

func newAIPlanRestartFailedSource(t *testing.T) (*aiPlanRestartFixture, models.AgentRun, aiWorkPlanIntent) {
	t.Helper()
	f := newAIPlanRestartFixture(t)
	source := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
	base := aiWorkPlanIntent{Title: "Keep the failed obligation linked", Steps: []aiWorkPlanStep{
		{ID: "original", Title: "Submit the requested work", Kind: "action", DependsOn: []string{}, ProposalID: source.ID},
	}}
	decodePlanTool(t, f.Plan, updatePlanArgs(base, 0))
	run := f.claim(t, f.confirm(t, source, ""))
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	run = f.loadRun(t, run.ID)
	f.nextGeneration(t)
	return f, run, base
}

func appendAIPlanRestart(base aiWorkPlanIntent, id, replaces, proposalID string) aiWorkPlanIntent {
	return aiWorkPlanIntent{Title: base.Title, Steps: append(append([]aiWorkPlanStep{}, base.Steps...), aiWorkPlanStep{
		ID: id, Title: "Restart this exact obligation with current facts", Kind: "action", DependsOn: []string{}, Replaces: replaces, ProposalID: proposalID,
	})}
}

func TestAIWorkPlanAgentRestartRecoversCurrentFactsAfterUnretryableExecution(t *testing.T) {
	for _, kind := range []string{"retained_task_drift", "failed_task_drift", "failed_provider_drift", "failed_actor_drift"} {
		t.Run(kind, func(t *testing.T) {
			f := newAIPlanRestartFixture(t)
			source := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
			base := aiWorkPlanIntent{Title: "Deliver reviewed work despite changed requirements", Steps: []aiWorkPlanStep{
				{ID: "original", Title: "Submit the original report", Kind: "action", DependsOn: []string{}, ProposalID: source.ID},
			}}
			decodePlanTool(t, f.Plan, updatePlanArgs(base, 0))
			original := f.claim(t, f.confirm(t, source, ""))
			if kind == "retained_task_drift" {
				f.changeTask(t)
				if err := f.Service.finalizeAgentRun(original, "PRIVATE_RETAINED_OUTPUT original completed answer", "", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.Service.finalizeAgentRun(original, "", "AGENT_MODEL_FAILED", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
				if kind == "failed_task_drift" {
					f.changeTask(t)
				} else if kind == "failed_provider_drift" {
					if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{
						"model": "current-replacement-model", "version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1,
					}).Error; err != nil {
						t.Fatal(err)
					}
					if err := f.Store.DB.First(&f.Provider, "id=?", f.Provider.ID).Error; err != nil {
						t.Fatal(err)
					}
				} else {
					if err := f.Store.DB.Model(&f.Actor).Updates(map[string]any{"display_name": "Updated agent identity", "version": f.Actor.Version + 1}).Error; err != nil {
						t.Fatal(err)
					}
					if err := f.Store.DB.First(&f.Actor, "id=?", f.Actor.ID).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			original = f.loadRun(t, original.ID)
			if kind == "retained_task_drift" {
				if original.Status != "succeeded" || original.OutputDeliveryStatus != agentRunOutputRetained || original.ResultText == nil || original.SubmissionID != nil {
					t.Fatalf("fixture did not produce an actual retained result: %#v", original)
				}
			} else if original.Status != "failed" || original.OutputDeliveryStatus != agentRunOutputNotReady {
				t.Fatalf("fixture did not produce actual failed execution: %#v", original)
			}
			var firstRevision aiWorkPlanRevision
			if err := f.Store.DB.First(&firstRevision, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil {
				t.Fatal(err)
			}
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 1, "ready")
			f.nextGeneration(t)
			if _, err := f.Tool.Execute(context.Background(), []byte(aiRetryArgs(f.aiAgentRunFixture, original))); err == nil {
				t.Fatal("exact retry unexpectedly accepted stale identity or retained output")
			}
			// Fresh work is valid under the current task/provider, but a generic
			// start cannot launder the old failed/retained plan evidence.
			unbound := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
			next := aiWorkPlanIntent{Title: base.Title, Steps: append(append([]aiWorkPlanStep{}, base.Steps...), aiWorkPlanStep{
				ID: "restart", Title: "Submit using the current facts", Kind: "action", DependsOn: []string{}, Replaces: "original", ProposalID: unbound.ID,
			})}
			if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(next, 1)); err == nil {
				t.Fatal("unbound new start silently erased the old execution obligation")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
			finishAIGeneration(t, f.Store, f.Generation)
			decideAIPlanProposal(t, f.aiAgentRunFixture, unbound, "reject")
			f.nextGeneration(t)
			var action map[string]any
			if err := json.Unmarshal([]byte(aiAgentRunActionJSON(f.aiAgentRunFixture)), &action); err != nil {
				t.Fatal(err)
			}
			action["changes"].(map[string]any)["restart_of_run_id"] = original.ID
			encoded, _ := json.Marshal(action)
			// First-red seam: without an explicit restart source there is no safe
			// proposal connecting current execution facts to this old obligation.
			restart := proposeTestAction(t, f.Store, f.Tool, string(encoded))
			next.Steps[1].ProposalID = restart.ID
			view := decodePlanTool(t, f.Plan, updatePlanArgs(next, 1))
			if view.Steps[0].Satisfied || view.Steps[0].SupersededBy != "restart" || view.Steps[1].Satisfied || view.Steps[1].State != "pending" {
				t.Fatalf("unapproved restart altered the original result: %#v", view)
			}
			viewJSON, _ := json.Marshal(view)
			if !strings.Contains(string(viewJSON), `"restart_of_run_id":"`+original.ID+`"`) || strings.Contains(string(viewJSON), `"retry_of_run_id"`) || strings.Contains(string(viewJSON), "PRIVATE_RETAINED_OUTPUT") {
				t.Fatalf("plan restart metadata missing, conflated with retry, or leaking old output: %s", viewJSON)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
			finishAIGeneration(t, f.Store, f.Generation)
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "pending_approval", f.Generation.ID)
			missingConfirmation := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, restart.Fingerprint))
			response := performRequest(f.Approvals, http.MethodPost, "/api/v1/ai/actions/"+restart.ID+"/decision", missingConfirmation, nil)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("restart without independent confirmation=%d: %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			current := f.confirm(t, restart, `,"confirm_agent_restart":true`)
			if current.ParentRunID != nil || current.Attempt <= original.Attempt || current.TaskVersion != f.Task.Version || current.ProviderVersion != f.Provider.Version || current.ActorVersion != f.Actor.Version {
				t.Fatalf("restart did not use fresh current identities, or pretended to be a frozen retry: %#v", current)
			}
			if strings.Contains(current.InputSnapshotJSON, "PRIVATE_RETAINED_OUTPUT") {
				t.Fatal("restart implicitly copied retained result into the new execution input")
			}
			if again := f.confirm(t, restart, `,"confirm_agent_restart":true`); !reflect.DeepEqual(current, again) {
				t.Fatalf("replayed human confirmation created a second restart: %#v", again)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 1)
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "run_active")
			if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, current, f.Service.artifactStore); err != nil {
				t.Fatalf("actual restart lost its startup identity: %v", err)
			}
			current = f.claim(t, current)
			if err := f.Service.finalizeAgentRun(current, "The updated report is ready for human review.", "", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			current = f.loadRun(t, current.ID)
			if current.Status != "succeeded" || current.OutputDeliveryStatus != agentRunOutputSubmitted || current.SubmissionID == nil || current.ArtifactID == nil {
				t.Fatalf("new execution did not produce an actual submission: %#v", current)
			}
			complete := assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "plan_complete")
			if complete.Steps[0].Satisfied || complete.Steps[0].SupersededBy != "restart" || !complete.Steps[1].Satisfied || complete.Steps[1].State != "submitted" {
				t.Fatalf("completion was not backed by the new submitted Run: %#v", complete)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, f.Task.ID)
			var unchanged aiWorkPlanRevision
			if err := f.Store.DB.First(&unchanged, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil || !reflect.DeepEqual(firstRevision, unchanged) || !reflect.DeepEqual(original, f.loadRun(t, original.ID)) {
				t.Fatalf("restart changed immutable source history: %v", err)
			}
		})
	}
}

func TestAIWorkPlanAgentRestartRejectedOrExpiredProposalCannotLaunderSource(t *testing.T) {
	for _, state := range []string{"rejected", "expired"} {
		t.Run(state, func(t *testing.T) {
			f, original, base := newAIPlanRestartFailedSource(t)
			restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			next := appendAIPlanRestart(base, "restart", "original", restart.ID)
			decodePlanTool(t, f.Plan, updatePlanArgs(next, 1))
			finishAIGeneration(t, f.Store, f.Generation)
			if state == "rejected" {
				decideAIPlanProposal(t, f.aiAgentRunFixture, restart, "reject")
			} else {
				later := f.Service.options.Now().Add(25 * time.Hour)
				f.Service.options.Now = func() time.Time { return later }
			}
			f.nextGeneration(t)
			for _, action := range []string{
				aiAgentRunActionJSON(f.aiAgentRunFixture),
				`{"action":"task.create","changes":{"title":"Unrelated work must not retire a failed execution"}}`,
			} {
				unbound := proposeTestAction(t, f.Store, f.Tool, action)
				candidate := appendAIPlanRestart(next, "replacement", "restart", unbound.ID)
				if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(candidate, 2)); err == nil {
					t.Fatalf("%s restart let unrelated action erase the original Run anchor", state)
				}
			}
			precise := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			candidate := appendAIPlanRestart(next, "replacement", "restart", precise.ID)
			view := decodePlanTool(t, f.Plan, updatePlanArgs(candidate, 2))
			if view.Steps[0].State != "failed" || view.Steps[0].Satisfied || view.Steps[1].State != state || view.Steps[1].Satisfied ||
				view.Steps[2].Satisfied || view.Steps[2].State != "pending" {
				t.Fatalf("replacement lost historical failure and unexecuted evidence: %#v", view)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
		})
	}
}

func TestAIWorkPlanAgentRestartFirstBoundStepKeepsSourceAfterRejectionOrExpiry(t *testing.T) {
	for _, state := range []string{"rejected", "expired"} {
		t.Run(state, func(t *testing.T) {
			f := newAIPlanRestartFixture(t)
			source := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
			original := f.claim(t, f.confirm(t, source, ""))
			if err := f.Service.finalizeAgentRun(original, "", "AGENT_MODEL_FAILED", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			f.nextGeneration(t)
			restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			// This plan starts with the linked restart itself. The original
			// confirmed execution is not separately listed as a plan step.
			base := aiWorkPlanIntent{Title: "A plan starting after an earlier failed execution", Steps: []aiWorkPlanStep{
				{ID: "first", Title: "Restart the existing obligation", Kind: "action", DependsOn: []string{}, ProposalID: restart.ID},
			}}
			decodePlanTool(t, f.Plan, updatePlanArgs(base, 0))
			finishAIGeneration(t, f.Store, f.Generation)
			if state == "rejected" {
				decideAIPlanProposal(t, f.aiAgentRunFixture, restart, "reject")
			} else {
				later := f.Service.options.Now().Add(25 * time.Hour)
				f.Service.options.Now = func() time.Time { return later }
			}
			f.nextGeneration(t)
			for _, args := range []string{aiAgentRunActionJSON(f.aiAgentRunFixture), `{"action":"task.create","changes":{"title":"Unrelated replacement"}}`} {
				unbound := proposeTestAction(t, f.Store, f.Tool, args)
				if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(appendAIPlanRestart(base, "next", "first", unbound.ID), 1)); err == nil {
					t.Fatal("a first-step restart lost its original Run constraint after rejection/expiry")
				}
			}
			precise := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			view := decodePlanTool(t, f.Plan, updatePlanArgs(appendAIPlanRestart(base, "next", "first", precise.ID), 1))
			if view.Steps[0].State != state || view.Steps[0].Satisfied || view.Steps[1].Satisfied || view.Steps[1].Evidence == nil || view.Steps[1].Evidence.RestartOfRunID != original.ID {
				t.Fatalf("the exact anchored replacement is no longer readable: %#v", view)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
		})
	}
}

func TestAIWorkPlanAgentRestartFirstBoundStepCanRetryOnlyItsFailedSource(t *testing.T) {
	for _, sourceState := range []string{"failed", "retained"} {
		for _, proposalState := range []string{"rejected", "expired"} {
			t.Run(sourceState+"_"+proposalState, func(t *testing.T) {
				f := newAIPlanRestartFixture(t)
				// The original fixture Router owns a separate fixed clock. This
				// handler must share the proposal service clock when time advances.
				continuations := gin.New()
				continuations.GET("/api/v1/ai/sessions/:id/plan/continuation", f.Service.getAIWorkPlanContinuation)
				assertContinuation := func(version int64, want string) aiWorkPlanView {
					t.Helper()
					path := fmt.Sprintf("/api/v1/ai/sessions/%s/plan/continuation?expected_version=%d", f.Generation.SessionID, version)
					response := performRequest(continuations, http.MethodGet, path, nil, nil)
					var result struct {
						Data struct {
							Plan                 aiWorkPlanView `json:"plan"`
							Ready                bool           `json:"ready"`
							Reason               string         `json:"reason"`
							BlockingGenerationID string         `json:"blocking_generation_id"`
						} `json:"data"`
					}
					if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
						result.Data.Reason != want || result.Data.Ready != (want == "ready") ||
						result.Data.Plan.Version != version || result.Data.Plan.SessionID != f.Generation.SessionID {
						t.Fatalf("continuation want=%s version=%d got=%d: %s", want, version, response.Code, response.Body.String())
					}
					if want == "pending_approval" && result.Data.BlockingGenerationID != f.Generation.ID {
						t.Fatalf("pending approval did not identify the current retry generation: %s", response.Body.String())
					}
					return result.Data.Plan
				}
				source := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
				original := f.claim(t, f.confirm(t, source, ""))
				result, errorCode := "", "AGENT_MODEL_FAILED"
				if sourceState == "retained" {
					f.changeTask(t)
					result, errorCode = "Retained work still requires a new current-facts execution.", ""
				}
				if err := f.Service.finalizeAgentRun(original, result, errorCode, f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
				original = f.loadRun(t, original.ID)
				if sourceState == "failed" && (original.Status != "failed" || original.OutputDeliveryStatus != agentRunOutputNotReady) ||
					sourceState == "retained" && (original.Status != "succeeded" || original.OutputDeliveryStatus != agentRunOutputRetained || original.ResultText == nil) {
					t.Fatalf("shared finalizer did not establish the expected source facts: %#v", original)
				}
				f.nextGeneration(t)
				restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
				base := aiWorkPlanIntent{Title: "Choose how to continue the same execution obligation", Steps: []aiWorkPlanStep{
					{ID: "first", Title: "Start again from current facts", Kind: "action", DependsOn: []string{}, ProposalID: restart.ID},
				}}
				decodePlanTool(t, f.Plan, updatePlanArgs(base, 0))
				var firstRevision aiWorkPlanRevision
				if err := f.Store.DB.First(&firstRevision, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil {
					t.Fatal(err)
				}
				finishAIGeneration(t, f.Store, f.Generation)
				if proposalState == "rejected" {
					decideAIPlanProposal(t, f.aiAgentRunFixture, restart, "reject")
				} else {
					later := f.Service.options.Now().Add(25 * time.Hour)
					f.Service.options.Now = func() time.Time { return later }
				}
				f.nextGeneration(t)
				if sourceState == "retained" {
					_, err := f.Tool.Execute(context.Background(), []byte(aiRetryArgs(f.aiAgentRunFixture, original)))
					if err == nil || !strings.HasPrefix(err.Error(), "AGENT_RUN_NOT_RETRYABLE:") {
						t.Fatalf("retained source must fail the retry state gate, not merely stale identity: %v", err)
					}
					finishAIGeneration(t, f.Store, f.Generation)
					view := assertContinuation(1, "ready")
					if view.Steps[0].State != proposalState || view.Steps[0].Satisfied || view.Steps[0].Evidence == nil || view.Steps[0].Evidence.RestartOfRunID != original.ID {
						t.Fatalf("refused retained retry changed the required restart source: %#v", view)
					}
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 2)
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
				} else {
					retry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, original))
					view := decodePlanTool(t, f.Plan, updatePlanArgs(appendAIPlanRestart(base, "retry", "first", retry.ID), 1))
					if view.Steps[0].State != proposalState || view.Steps[0].Satisfied || view.Steps[0].SupersededBy != "retry" ||
						view.Steps[1].State != "pending" || view.Steps[1].Satisfied || view.Steps[1].Evidence == nil ||
						view.Steps[1].Evidence.RetryOfRunID != original.ID || view.Steps[1].Evidence.RestartOfRunID != "" {
						t.Fatalf("first-bound restart did not allow an exact frozen retry of its failed source: %#v", view)
					}
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
					finishAIGeneration(t, f.Store, f.Generation)
					assertContinuation(2, "pending_approval")
					// This is now an exact retry, not a restart. Execution consent is
					// required, but an irrelevant restart confirmation is not.
					child := f.confirm(t, retry, "")
					if child.ParentRunID == nil || *child.ParentRunID != original.ID || child.Attempt <= original.Attempt ||
						child.InputSnapshotJSON != original.InputSnapshotJSON || child.TaskVersion != original.TaskVersion {
						t.Fatalf("confirmed retry did not retain its exact frozen parent: %#v", child)
					}
					if again := f.confirm(t, retry, ""); !reflect.DeepEqual(child, again) {
						t.Fatal("replayed retry confirmation created another execution")
					}
					assertContinuation(2, "run_active")
					child = f.claim(t, child)
					if err := f.Service.finalizeAgentRun(child, "The frozen retry has produced the report for human review.", "", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
						t.Fatal(err)
					}
					child = f.loadRun(t, child.ID)
					if child.Status != "succeeded" || child.OutputDeliveryStatus != agentRunOutputSubmitted || child.SubmissionID == nil || child.ArtifactID == nil {
						t.Fatalf("retry completion is not backed by a real submission: %#v", child)
					}
					complete := assertContinuation(2, "plan_complete")
					if complete.Steps[0].State != proposalState || complete.Steps[0].Satisfied || !complete.Steps[1].Satisfied || complete.Steps[1].State != "submitted" {
						t.Fatalf("completion lost the rejected/expired first step: %#v", complete)
					}
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
					assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, f.Task.ID)
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
				var unchanged aiWorkPlanRevision
				if err := f.Store.DB.First(&unchanged, "session_id=? AND version=1", f.Generation.SessionID).Error; err != nil ||
					!reflect.DeepEqual(firstRevision, unchanged) || !reflect.DeepEqual(original, f.loadRun(t, original.ID)) {
					t.Fatalf("continuation changed the original Run or first revision: %v", err)
				}
			})
		}
	}
}

func TestAIWorkPlanAgentRestartFailedSuccessorMustAnchorItsOwnRun(t *testing.T) {
	f, original, base := newAIPlanRestartFailedSource(t)
	restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
	next := appendAIPlanRestart(base, "restart", "original", restart.ID)
	decodePlanTool(t, f.Plan, updatePlanArgs(next, 1))
	current := f.claim(t, f.confirm(t, restart, `,"confirm_agent_restart":true`))
	if err := f.Service.finalizeAgentRun(current, "", "AGENT_MODEL_TRUNCATED", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	current = f.loadRun(t, current.ID)
	f.nextGeneration(t)
	wrongAnchor := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
	if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(appendAIPlanRestart(next, "next", "restart", wrongAnchor.ID), 2)); err == nil {
		t.Fatal("newly executed failure was hidden by pointing back to its ancestor")
	}
	correct := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, current.ID))
	view := decodePlanTool(t, f.Plan, updatePlanArgs(appendAIPlanRestart(next, "next", "restart", correct.ID), 2))
	if view.Steps[0].State != "failed" || view.Steps[1].State != "failed" || view.Steps[0].Satisfied || view.Steps[1].Satisfied || view.Steps[2].Satisfied {
		t.Fatalf("restart chain falsely completed a failed Run: %#v", view)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
}

func TestAIWorkPlanAgentRestartStrictSourceSelectionAndScope(t *testing.T) {
	f, original, _ := newAIPlanRestartFailedSource(t)
	valid := aiPlanRestartArgs(f.aiAgentRunFixture, original.ID)
	for _, test := range []struct{ name, replacement string }{
		{"null", "null"}, {"empty", `""`}, {"space", fmt.Sprintf("%q", " "+original.ID)},
		{"nonexistent", fmt.Sprintf("%q", uuid.NewString())}, {"object", `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var action map[string]any
			_ = json.Unmarshal([]byte(valid), &action)
			var bad any
			_ = json.Unmarshal([]byte(test.replacement), &bad)
			action["changes"].(map[string]any)["restart_of_run_id"] = bad
			encoded, _ := json.Marshal(action)
			if _, err := f.Tool.Execute(context.Background(), encoded); err == nil {
				t.Fatal("ambiguous or absent restart source accepted")
			}
		})
	}
	otherTask := models.Task{ID: uuid.NewString(), Title: "Different obligation", Kind: "work", Status: "todo", ReviewPolicy: "manual", Priority: "P1", Version: 1, CreatedAt: f.Task.CreatedAt, UpdatedAt: f.Task.UpdatedAt}
	if err := f.Store.DB.Create(&otherTask).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{ID: uuid.NewString(), TaskID: otherTask.ID, ActorID: f.Actor.ID, Role: "assignee", AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: f.Task.CreatedAt, Reason: "Valid separate task fixture"}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	other := f.aiAgentRunFixture
	other.Task = otherTask
	proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(other)) // independent start is otherwise valid
	wrongTask := strings.Replace(valid, fmt.Sprintf(`"task_id":%q`, f.Task.ID), fmt.Sprintf(`"task_id":%q`, otherTask.ID), 1)
	if _, err := f.Tool.Execute(context.Background(), []byte(wrongTask)); err == nil {
		t.Fatal("cross-Task source was accepted")
	}
	for _, scopes := range [][]string{{"work", "outputs", "actions"}, {"work", "actions", "agent_execution"}, {"work", "outputs", "agent_execution"}} {
		registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
			&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID)
		if err != nil {
			continue // Scope dependency rejection is also fail-closed.
		}
		tool, ok := registry.Get("workspace_propose")
		if ok {
			if _, err := tool.Execute(context.Background(), []byte(valid)); err == nil {
				t.Fatalf("restart acquired missing authority from scopes %v", scopes)
			}
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
}

func TestAIWorkPlanAgentRestartApprovalRechecksCurrentFacts(t *testing.T) {
	for _, drift := range []string{"task", "provider"} {
		t.Run(drift, func(t *testing.T) {
			f, original, _ := newAIPlanRestartFailedSource(t)
			restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			if drift == "task" {
				f.changeTask(t)
			} else if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"model": "changed-after-preview", "version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1}).Error; err != nil {
				t.Fatal(err)
			}
			finishAIGeneration(t, f.Store, f.Generation)
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_restart":true}`, restart.Fingerprint))
			response := performRequest(f.Approvals, http.MethodPost, "/api/v1/ai/actions/"+restart.ID+"/decision", body, nil)
			if response.Code != http.StatusConflict {
				t.Fatalf("stale restart preview confirmed: %d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
			if !reflect.DeepEqual(original, f.loadRun(t, original.ID)) {
				t.Fatal("failed confirmation changed the old Run")
			}
		})
	}
}

func TestAIWorkPlanAgentRestartDoesNotAcceptAnotherSessionProposal(t *testing.T) {
	f, original, base := newAIPlanRestartFailedSource(t)
	session := models.AISession{ID: uuid.NewString(), Persist: true, Title: "Other conversation", Version: 1, CreatedAt: f.Task.CreatedAt, UpdatedAt: f.Task.UpdatedAt}
	if err := f.Store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := f.Generation
	generation.ID, generation.SessionID = uuid.NewString(), session.ID
	if err := f.Store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := f.Service.aiChatToolRegistry(session.ID, true, &f.Provider, &aiWorkspaceGrant{
		ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"},
	}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("separate explicitly authorized session cannot propose")
	}
	foreign := proposeTestAction(t, f.Store, tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
	if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(appendAIPlanRestart(base, "restart", "original", foreign.ID), 1)); err == nil {
		t.Fatal("foreign session proposal became current plan evidence")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
}

func TestAIWorkPlanAgentRestartCorruptedLineageFailsClosed(t *testing.T) {
	for _, corruption := range []string{"parent_mix", "snapshot_hash", "second_event"} {
		t.Run(corruption, func(t *testing.T) {
			f, original, base := newAIPlanRestartFailedSource(t)
			restart := proposeTestAction(t, f.Store, f.Tool, aiPlanRestartArgs(f.aiAgentRunFixture, original.ID))
			next := appendAIPlanRestart(base, "restart", "original", restart.ID)
			decodePlanTool(t, f.Plan, updatePlanArgs(next, 1))
			current := f.confirm(t, restart, `,"confirm_agent_restart":true`)
			var row aiWorkPlanRevision
			if err := f.Store.DB.First(&row, "session_id=? AND version=2", f.Generation.SessionID).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := projectAIWorkPlan(f.Store.DB, row, f.Service.options.Now()); err != nil {
				t.Fatalf("uncorrupted actual restart evidence rejected: %v", err)
			}
			// Simulate storage corruption inside a rollback-only transaction.
			// No immutable event is edited/deleted and no DB protection is disabled.
			rollback := fmt.Errorf("rollback isolated corruption fixture")
			err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
				switch corruption {
				case "parent_mix":
					if err := tx.Model(&models.AgentRun{}).Where("id=?", current.ID).Update("parent_run_id", original.ID).Error; err != nil {
						t.Fatal(err)
					}
				case "snapshot_hash":
					if err := tx.Model(&models.AgentRun{}).Where("id=?", current.ID).Update("input_snapshot_json", current.InputSnapshotJSON+" ").Error; err != nil {
						t.Fatal(err)
					}
				case "second_event":
					var event models.WorkflowEvent
					if err := tx.Where("action='agent_run_restarted' AND aggregate_id=?", current.ID).Take(&event).Error; err != nil {
						t.Fatal(err)
					}
					event.ID, event.RequestID = uuid.NewString(), nil
					if err := tx.Create(&event).Error; err != nil {
						t.Fatal(err)
					}
				}
				if _, err := loadAgentRunRestartProof(tx, models.AgentRun{ID: current.ID}); err == nil {
					t.Error("corrupt restart proof accepted")
				}
				if _, err := projectAIWorkPlan(tx, row, f.Service.options.Now()); err == nil {
					t.Error("plan accepted missing/ambiguous execution provenance")
				}
				return rollback
			})
			if err != rollback {
				t.Fatalf("corruption fixture did not roll back: %v", err)
			}
			if _, err := projectAIWorkPlan(f.Store.DB, row, f.Service.options.Now()); err != nil || !reflect.DeepEqual(current, f.loadRun(t, current.ID)) {
				t.Fatalf("rollback failed to preserve real provenance: %v", err)
			}
		})
	}
}

func TestAIWorkPlanAgentRestartHarnessCurrentFactsAndActualSubmission(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f, original, _ := newAIPlanRestartFailedSource(t)
			f.changeTask(t)
			finishAIGeneration(t, f.Store, f.Generation)
			var restartID, currentID, observedRunID string
			var observedPlan aiWorkPlanIntent
			var observedPlanVersion int64
			const output = "PRIVATE_RESTART_FINAL_OUTPUT submitted for human review, not accepted"
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("restart-%d", call), name, args)
				}
				result := func(previous int, name string) string {
					return aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("restart-%d", previous), name)
				}
				finish := func(text string) {
					writeAIBudgetTextTurn(w, protocol, text+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				assertLinked := func(content string, list bool) {
					var object map[string]any
					if json.Unmarshal([]byte(content), &object) != nil {
						t.Errorf("malformed actual Run metadata: %s", content)
						return
					}
					if list {
						var found map[string]any
						items, _ := object["items"].([]any)
						for _, value := range items {
							item, _ := value.(map[string]any)
							if item["id"] == currentID {
								found = item
								break
							}
						}
						object = found
					}
					if object == nil || object["id"] != currentID || object["restart_of_run_id"] != original.ID || object["parent_run_id"] != nil {
						t.Errorf("metadata lost proven restart or mislabeled it as retry: %s", content)
					}
				}
				switch call {
				case 1, 9:
					emit("workspace_plan", `{"operation":"read"}`)
				case 2:
					var envelope struct {
						Plan *aiWorkPlanView `json:"plan"`
					}
					content := result(1, "workspace_plan")
					if json.Unmarshal([]byte(content), &envelope) != nil || envelope.Plan == nil || len(envelope.Plan.Steps) != 1 || envelope.Plan.Steps[0].State != "failed" {
						t.Errorf("model never read the actual old obligation: %s", content)
						finish("读取失败，停止。")
						return
					}
					observedPlanVersion = envelope.Plan.Version
					observedPlan = aiWorkPlanIntent{Title: envelope.Plan.Title, Steps: []aiWorkPlanStep{envelope.Plan.Steps[0].aiWorkPlanStep}}
					emit("workspace_guide", `{"topic":"agents"}`)
				case 3:
					if !aiBudgetHasTool(payload, "workspace_agent_execution") {
						t.Error("Agent guide did not discover current eligibility")
					}
					emit("workspace_get", fmt.Sprintf(`{"type":"agent_run","id":%q}`, original.ID))
				case 4:
					var source struct{ ID, TaskID, Status string }
					var object map[string]any
					content := result(3, "workspace_get")
					if json.Unmarshal([]byte(content), &object) != nil || object["id"] != original.ID || object["task_id"] != f.Task.ID || object["status"] != "failed" {
						t.Errorf("model did not observe the authoritative failed Run: %s", content)
					}
					source.ID, _ = object["id"].(string)
					source.TaskID, _ = object["task_id"].(string)
					observedRunID = source.ID
					emit("workspace_agent_execution", fmt.Sprintf(`{"task_id":%q}`, source.TaskID))
				case 5:
					var eligibility struct {
						Eligible  bool                        `json:"eligible"`
						Task      aiAgentRunEligibilityTask   `json:"task"`
						Providers []aiAgentRunProviderPreview `json:"providers"`
					}
					content := result(4, "workspace_agent_execution")
					if json.Unmarshal([]byte(content), &eligibility) != nil || !eligibility.Eligible || eligibility.Task.ID != f.Task.ID || eligibility.Task.Version != f.Task.Version || eligibility.Task.Version == original.TaskVersion {
						t.Errorf("restart did not read current Task eligibility: %s", content)
					}
					var provider aiAgentRunProviderPreview
					for _, candidate := range eligibility.Providers {
						if candidate.ID == f.Provider.ID {
							provider = candidate
						}
					}
					if provider.ID == "" {
						t.Error("current execution Provider absent from actual eligibility")
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d,"restart_of_run_id":%q}}`, eligibility.Task.ID, eligibility.Task.Version, provider.ID, provider.Version, provider.ConfigVersion, observedRunID))
				case 6:
					var proposal struct {
						ID string `json:"proposal_id"`
					}
					content := result(5, "workspace_propose")
					if json.Unmarshal([]byte(content), &proposal) != nil || proposal.ID == "" {
						t.Errorf("restart proposal never reached model: %s", content)
					}
					restartID = proposal.ID
					plan := appendAIPlanRestart(observedPlan, "restart", "original", restartID)
					emit("workspace_plan", string(updatePlanArgs(plan, observedPlanVersion)))
				case 7:
					content := result(6, "workspace_plan")
					if !strings.Contains(content, `"state":"pending"`) || !strings.Contains(content, `"restart_of_run_id":"`+observedRunID+`"`) || !strings.Contains(content, `"superseded_by":"restart"`) {
						t.Errorf("pending restart did not extend the actual plan: %s", content)
					}
					finish("已准备按当前事实重新执行的建议，请分别确认执行和重新执行来源；尚未启动。")
				case 8, 16:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new message inherited authority: %s", name)
						}
					}
					if strings.Contains(string(raw), output) || strings.Contains(string(raw), "input_snapshot_json") {
						t.Error("ungranted message inherited execution output/snapshot")
					}
					finish("本轮没有工作台授权，不读取或执行。")
				case 10:
					content := result(9, "workspace_plan")
					if !strings.Contains(content, `"state":"submitted"`) || !strings.Contains(content, `"state":"failed"`) || !strings.Contains(content, `"restart_of_run_id":"`+original.ID+`"`) {
						t.Errorf("fresh grant did not read actual submitted restart: %s", content)
					}
					if aiBudgetHasTool(payload, "workspace_agent_execution") {
						t.Error("plan-reading grant inherited execution authority")
					}
					emit("workspace_guide", `{"topic":"agents"}`)
				case 11:
					emit("workspace_get", fmt.Sprintf(`{"type":"agent_run","id":%q}`, currentID))
				case 12:
					assertLinked(result(11, "workspace_get"), false)
					emit("workspace_outputs", fmt.Sprintf(`{"type":"agent_run","task_id":%q}`, f.Task.ID))
				case 13:
					assertLinked(result(12, "workspace_outputs"), true)
					emit("workspace_agent_runs", fmt.Sprintf(`{"task_id":%q}`, f.Task.ID))
				case 14:
					assertLinked(result(13, "workspace_agent_runs"), true)
					emit("workspace_outputs", fmt.Sprintf(`{"type":"artifact","task_id":%q}`, f.Task.ID))
				case 15:
					content := result(14, "workspace_outputs")
					if strings.Contains(content, "restart_of_run_id") || strings.Contains(content, "parent_run_id") || strings.Contains(content, output) {
						t.Errorf("Artifact list leaked Run-only fields or body: %s", content)
					}
					finish("新的执行已提交，旧失败记录保留；任务等待人工验收，没有自动完成。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					finish("停止。")
				}
			})
			chatProvider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			send := func(message string, scopes []string, expectedCalls int) {
				t.Helper()
				request := map[string]any{"provider_id": chatProvider.ID, "session_id": f.Generation.SessionID, "message": message}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: chatProvider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || upstream.calls.Load() != int32(expectedCalls) || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || t.Failed() {
					t.Fatalf("restart Harness calls=%d want=%d response=%d: %s", upstream.calls.Load(), expectedCalls, response.Code, response.Body.String())
				}
			}
			send("检查计划中的失败执行，核对当前要求后准备重新执行建议，等我人工确认。", []string{"work", "outputs", "actions", "agent_execution"}, 7)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			var proposal models.AIActionProposal
			if err := f.Store.DB.First(&proposal, "id=?", restartID).Error; err != nil {
				t.Fatal(err)
			}
			var actualGeneration models.AIGeneration
			if err := f.Store.DB.First(&actualGeneration, "id=?", proposal.GenerationID).Error; err != nil {
				t.Fatal(err)
			}
			f.Generation = actualGeneration
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "pending_approval", proposal.GenerationID)
			send("谢谢", nil, 8)
			current := f.confirm(t, proposal, `,"confirm_agent_restart":true`)
			currentID = current.ID
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "run_active")
			current = f.claim(t, current)
			if err := f.Service.finalizeAgentRun(current, output, "", f.Service.options.Now().Add(3*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			current = f.loadRun(t, current.ID)
			if current.Status != "succeeded" || current.OutputDeliveryStatus != agentRunOutputSubmitted || current.SubmissionID == nil || current.ParentRunID != nil {
				t.Fatalf("real restart did not submit: %#v", current)
			}
			assertAIPlanContinuation(t, f.aiAgentRunFixture, 2, "plan_complete")
			// workspace_plan currently includes both read/update operations and
			// requires actions. This fresh grant does not include agent_execution;
			// the scripted model only reads and must create no additional proposal.
			send("重新授权计划读取所需范围，仅核对计划和这次执行的真实提交状态，不创建建议。", []string{"work", "outputs", "actions"}, 15)
			send("谢谢", nil, 16)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 2)
			if !reflect.DeepEqual(original, f.loadRun(t, original.ID)) {
				t.Fatal("restart Harness changed old failed execution")
			}
		})
	}
}
