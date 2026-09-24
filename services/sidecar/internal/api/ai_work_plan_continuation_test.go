package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func assertAIPlanContinuation(t *testing.T, f aiAgentRunFixture, version int64, want string, blockingGeneration ...string) aiWorkPlanView {
	t.Helper()
	path := fmt.Sprintf("/api/v1/ai/sessions/%s/plan/continuation?expected_version=%d", f.Generation.SessionID, version)
	response := performRequest(f.Router, http.MethodGet, path, nil, nil)
	var result struct {
		Data struct {
			Plan                 aiWorkPlanView              `json:"plan"`
			Ready                bool                        `json:"ready"`
			Reason               string                      `json:"reason"`
			BlockingGenerationID string                      `json:"blocking_generation_id"`
			Blockers             []aiPlanContinuationBlocker `json:"blockers"`
		} `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.Reason != want || result.Data.Ready != (want == "ready") || result.Data.Plan.Version != version || result.Data.Plan.SessionID != f.Generation.SessionID {
		t.Fatalf("continuation want=%s got=%d: %s", want, response.Code, response.Body.String())
	}
	if len(blockingGeneration) == 1 && result.Data.BlockingGenerationID != blockingGeneration[0] {
		t.Fatalf("blocking generation=%q want=%q", result.Data.BlockingGenerationID, blockingGeneration[0])
	}
	if want == "ready" || want == "plan_complete" || want == "generation_active" {
		if strings.Contains(response.Body.String(), "blocking_generation_id") {
			t.Fatalf("%s carried a blocking generation", want)
		}
		if len(result.Data.Blockers) != 0 {
			t.Fatalf("%s carried blocker summary: %#v", want, result.Data.Blockers)
		}
	} else if id := result.Data.BlockingGenerationID; id != "" {
		var own int64
		if err := f.Store.DB.Model(&models.AIGeneration{}).Where("id=? AND session_id=?", id, f.Generation.SessionID).Count(&own).Error; err != nil || own != 1 {
			t.Fatalf("blocking generation is not verified in this session: %q err=%v", id, err)
		}
		if len(result.Data.Blockers) == 0 || result.Data.Blockers[0].Reason != want {
			t.Fatalf("primary reason missing from blocker summary: want=%s blockers=%#v", want, result.Data.Blockers)
		}
	}
	for _, blocker := range result.Data.Blockers {
		if blocker.Total < 1 {
			t.Fatalf("empty blocker total: %#v", blocker)
		}
		for _, id := range blocker.GenerationIDs {
			var own int64
			if err := f.Store.DB.Model(&models.AIGeneration{}).Where("id=? AND session_id=?", id, f.Generation.SessionID).Count(&own).Error; err != nil || own != 1 {
				t.Fatalf("blocker generation is not verified in this session: %q err=%v", id, err)
			}
		}
	}
	for _, secret := range []string{"PRIVATE", "action_json", "preview_json", "fingerprint", "changes", "input_snapshot", "result_text", "base_url"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("continuation leaked %q: %s", secret, response.Body.String())
		}
	}
	return result.Data.Plan
}

func decideAIPlanProposal(t *testing.T, f aiAgentRunFixture, proposal models.AIActionProposal, decision string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q}`, proposal.Fingerprint, decision))
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("%s plan proposal=%d: %s", decision, response.Code, response.Body.String())
	}
}

func nextAIPlanGeneration(t *testing.T, f aiAgentRunFixture) aiAgentRunFixture {
	t.Helper()
	f.Generation.ID, f.Generation.Status = uuid.NewString(), "streaming"
	if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.Tool, _ = registry.Get("workspace_propose")
	return f
}

func TestAIWorkPlanContinuationChecksWholeGenerationAndLatestVersion(t *testing.T) {
	f := newAIAgentRunFixture(t)
	bound := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE bound task"}}`)
	sibling := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE unbound sibling"}}`)
	plan := sampleWorkPlan(bound.ID)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	assertAIPlanContinuation(t, f, 1, "generation_active")
	finishAIGeneration(t, f.Store, f.Generation)
	decideAIPlanProposal(t, f, bound, "confirm")
	assertAIPlanContinuation(t, f, 1, "pending_approval", f.Generation.ID)
	decideAIPlanProposal(t, f, sibling, "reject")
	assertAIPlanContinuation(t, f, 1, "ready")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages", 0)

	f = nextAIPlanGeneration(t, f)
	assertAIPlanContinuation(t, f, 1, "generation_active")
	plan.Steps[2].Report = "in_progress"
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 1))
	finishAIGeneration(t, f.Store, f.Generation)
	response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan/continuation?expected_version=1", nil, nil)
	assertAPIError(t, response, http.StatusConflict, "AI_PLAN_CONTINUATION_CHANGED")
	assertAIPlanContinuation(t, f, 2, "ready")
}

func TestAIWorkPlanContinuationStrictQueryAndSavedSessionBoundary(t *testing.T) {
	f := newAIAgentRunFixture(t)
	base := "/api/v1/ai/sessions/" + f.Generation.SessionID + "/plan/continuation"
	for _, query := range []string{"", "?expected_version", "?expected_version=", "?expected_version=0", "?expected_version=129", "?expected_version=-1", "?expected_version=01", "?expected_version=%2B1", "?expected_version=1.0", "?expected_version=1e0", "?expected_version=%201", "?expected_version=1%20", "?expected_version=1&expected_version=1", "?version=1", "?expected_version=1&extra=1", "?expected_version=1;extra=1", "?expected_version=%ZZ", "?EXPECTED_VERSION=1"} {
		assertAPIError(t, performRequest(f.Router, http.MethodGet, base+query, nil, nil), http.StatusUnprocessableEntity, "AI_PLAN_VERSION_INVALID")
	}
	for _, id := range []string{"invalid", strings.ToUpper(f.Generation.SessionID), strings.ReplaceAll(f.Generation.SessionID, "-", "")} {
		assertAPIError(t, performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+id+"/plan/continuation?expected_version=1", nil, nil), http.StatusUnprocessableEntity, "AI_SESSION_ID_INVALID")
	}
	assertAPIError(t, performRequest(f.Router, http.MethodGet, base+"?expected_version=1", nil, nil), http.StatusNotFound, "AI_PLAN_NOT_FOUND")
	assertAPIError(t, performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+uuid.NewString()+"/plan/continuation?expected_version=1", nil, nil), http.StatusNotFound, "AI_PLAN_NOT_FOUND")
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(""), 0))
	if err := f.Store.DB.Model(&models.AISession{}).Where("id=?", f.Generation.SessionID).Updates(map[string]any{"persist": false, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(f.Router, http.MethodGet, base+"?expected_version=1", nil, nil), http.StatusNotFound, "AI_PLAN_NOT_FOUND")
}

func TestAIWorkPlanContinuationAnalysisPlansAndUnrelatedHistory(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE unrelated pending"}}`)
	finishAIGeneration(t, f.Store, f.Generation)
	f = nextAIPlanGeneration(t, f)
	plan := aiWorkPlanIntent{Title: "Only analysis", Steps: []aiWorkPlanStep{{ID: "inspect", Title: "Inspect current facts", Kind: "analysis", DependsOn: []string{}, Report: "pending"}}}
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 1, "ready")
	// The unrelated session's active generation cannot block this saved plan.
	other := models.AISession{ID: uuid.NewString(), Title: "Other", Persist: true, Version: 1, CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
	if err := f.Store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	generation := f.Generation
	generation.ID, generation.SessionID, generation.Status = uuid.NewString(), other.ID, "streaming"
	if err := f.Store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	assertAIPlanContinuation(t, f, 1, "ready")
	f = nextAIPlanGeneration(t, f)
	plan.Steps[0].Report = "reported_done"
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 1))
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 2, "plan_complete")
}

func TestAIWorkPlanContinuationLatestAnalysisGenerationIncludesUnboundSibling(t *testing.T) {
	f := newAIAgentRunFixture(t)
	plan := aiWorkPlanIntent{Title: "Analysis with separate operation", Steps: []aiWorkPlanStep{{ID: "inspect", Title: "Review", Kind: "analysis", DependsOn: []string{}, Report: "pending"}}}
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	sibling := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE analysis sibling"}}`)
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 1, "pending_approval", f.Generation.ID)
	decideAIPlanProposal(t, f, sibling, "reject")
	assertAIPlanContinuation(t, f, 1, "ready")
}

func TestAIWorkPlanContinuationDoesNotTruncateRelevantGroupsToReceiptWindow(t *testing.T) {
	f := newAIAgentRunFixture(t)
	var bindings []string
	var pending models.AIActionProposal
	for group := 0; group < 3; group++ {
		proposals := make([]models.AIActionProposal, 0, 8)
		for index := 0; index < 8; index++ {
			proposal := proposeTestAction(t, f.Store, f.Tool, fmt.Sprintf(`{"action":"task.create","changes":{"title":"PRIVATE group %d item %d"}}`, group, index))
			proposals = append(proposals, proposal)
		}
		bindings = append(bindings, proposals[1].ID)
		if group == 2 {
			plan := aiWorkPlanIntent{Title: "Three relevant generations", Steps: []aiWorkPlanStep{}}
			for index, binding := range bindings {
				plan.Steps = append(plan.Steps, aiWorkPlanStep{ID: fmt.Sprintf("action_%d", index), Title: "Bound operation", Kind: "action", DependsOn: []string{}, ProposalID: binding})
			}
			decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
		}
		finishAIGeneration(t, f.Store, f.Generation)
		for index, proposal := range proposals {
			if group == 0 && index == 0 {
				pending = proposal
				continue
			}
			decideAIPlanProposal(t, f, proposal, "reject")
		}
		if group < 2 {
			f = nextAIPlanGeneration(t, f)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 24)
	assertAIPlanContinuation(t, f, 1, "pending_approval", pending.GenerationID)
	decideAIPlanProposal(t, f, pending, "reject")
	assertAIPlanContinuation(t, f, 1, "ready")
}

func TestAIWorkPlanContinuationIgnoresOnlySupersededHistoricalSource(t *testing.T) {
	f := newAIAgentRunFixture(t)
	bound := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE old bound"}}`)
	proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE old unbound pending"}}`)
	base := sampleWorkPlan(bound.ID)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(base, 0))
	decideAIPlanProposal(t, f, bound, "reject")
	finishAIGeneration(t, f.Store, f.Generation)
	f = nextAIPlanGeneration(t, f)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(replacementWorkPlan(base), 1))
	finishAIGeneration(t, f.Store, f.Generation)
	view := assertAIPlanContinuation(t, f, 2, "ready")
	if view.Steps[1].SupersededBy != "retry" {
		t.Fatal("supersession fixture did not preserve historical step")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
}

func TestAIWorkPlanContinuationUnavailablePendingIsNotAnExpiredDecision(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE interrupted pending"}}`)
			decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(proposal.ID), 0))
			if err := f.Store.DB.Model(&models.AIGeneration{}).Where("id=?", f.Generation.ID).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			assertAIPlanContinuation(t, f, 1, "generation_unavailable", f.Generation.ID)
			decideAIPlanProposal(t, f, proposal, "reject")
			assertAIPlanContinuation(t, f, 1, "ready")
		})
	}
}

func TestAIWorkPlanContinuationAgentSiblingTracksExecutionAndPendingDelivery(t *testing.T) {
	f := newAIAgentRunFixture(t)
	agentProposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(""), 0))
	run := seedRunningAgentRun(t, f)
	if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id=?", agentProposal.ID).Updates(map[string]any{
		"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": f.Generation.CreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 1, "run_active", f.Generation.ID)
	secret := "PRIVATE staged Agent output"
	if err := f.Service.persistPendingAgentRunOutput(run.ID, secret, f.Generation.CreatedAt); err != nil {
		t.Fatal(err)
	}
	assertAIPlanContinuation(t, f, 1, "output_pending", f.Generation.ID)
	if err := transitionAgentRunOutput(f.Store.DB, run.ID, secret, f.Generation.CreatedAt, agentRunOutputRetained, "TEST_RETAINED", nil, nil); err != nil {
		t.Fatal(err)
	}
	assertAIPlanContinuation(t, f, 1, "ready")
	if err := f.Store.DB.Delete(&models.AgentRun{}, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertAIPlanContinuation(t, f, 1, "evidence_unavailable", f.Generation.ID)
}

func TestAIWorkPlanContinuationSummarizesEveryParallelAgentBlocker(t *testing.T) {
	f := newAIAgentRunFixture(t)
	now := f.Generation.CreatedAt
	secondTask := f.Task
	secondTask.ID = uuid.NewString()
	secondTask.Title = "Prepare the second audited report"
	secondTask.CreatedAt, secondTask.UpdatedAt = now, now
	if err := f.Store.DB.Create(&secondTask).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Create(&models.TaskAssignment{
		ID: uuid.NewString(), TaskID: secondTask.ID, ActorID: f.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "parallel plan fixture",
	}).Error; err != nil {
		t.Fatal(err)
	}
	second := f
	second.Task = secondTask
	firstProposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	secondProposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(second))
	proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE parallel sibling"}}`)
	plan := aiWorkPlanIntent{Title: "Coordinate parallel reports", Steps: []aiWorkPlanStep{
		{ID: "prepare", Title: "Check both report requirements", Kind: "analysis", DependsOn: []string{}, Report: "reported_done"},
		{ID: "first", Title: "Run the first report", Kind: "action", DependsOn: []string{"prepare"}, ProposalID: firstProposal.ID},
		{ID: "second", Title: "Run the second report", Kind: "action", DependsOn: []string{"prepare"}, ProposalID: secondProposal.ID},
		{ID: "review", Title: "Review both actual outputs", Kind: "analysis", DependsOn: []string{"first", "second"}, Report: "pending"},
	}}
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	firstRun := seedRunningAgentRun(t, f)
	secondRun := seedRunningAgentRun(t, second)
	if err := f.Service.persistPendingAgentRunOutput(secondRun.ID, "PRIVATE pending second output", now); err != nil {
		t.Fatal(err)
	}
	for proposalID, runID := range map[string]string{firstProposal.ID: firstRun.ID, secondProposal.ID: secondRun.ID} {
		if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id=?", proposalID).Updates(map[string]any{
			"status": "confirmed", "result_id": runID, "result_version": 1, "decided_at": now,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	finishAIGeneration(t, f.Store, f.Generation)

	response := performRequest(f.Router, http.MethodGet, fmt.Sprintf("/api/v1/ai/sessions/%s/plan/continuation?expected_version=1", f.Generation.SessionID), nil, nil)
	var envelope struct {
		Data aiPlanContinuationResponse `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("parallel continuation: %d %s", response.Code, response.Body.String())
	}
	if envelope.Data.Reason != "pending_approval" || envelope.Data.BlockingGenerationID != f.Generation.ID {
		t.Fatalf("parallel primary blocker = %#v", envelope.Data)
	}
	want := []aiPlanContinuationBlocker{
		{Reason: "pending_approval", Total: 1, GenerationIDs: []string{f.Generation.ID}},
		{Reason: "output_pending", Total: 1, GenerationIDs: []string{f.Generation.ID}},
		{Reason: "run_active", Total: 1, GenerationIDs: []string{f.Generation.ID}},
	}
	if !reflect.DeepEqual(envelope.Data.Blockers, want) {
		t.Fatalf("parallel blockers=%#v want=%#v", envelope.Data.Blockers, want)
	}
	for _, secret := range []string{"PRIVATE parallel sibling", "PRIVATE pending second output", "action_json", "preview_json"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("parallel blocker summary leaked %q: %s", secret, response.Body.String())
		}
	}
}

func TestAIWorkPlanContinuationMissingOrForeignBoundEvidence(t *testing.T) {
	for _, scenario := range []string{"deleted_proposal", "foreign_generation", "foreign_plan_generation"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE evidence"}}`)
			finishAIGeneration(t, f.Store, f.Generation)
			oldGeneration := f.Generation
			f = nextAIPlanGeneration(t, f)
			decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(proposal.ID), 0))
			finishAIGeneration(t, f.Store, f.Generation)
			if scenario == "deleted_proposal" {
				if err := f.Store.DB.Delete(&models.AIActionProposal{}, "id=?", proposal.ID).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				other := createAIContextWindowSession(t, f.Store, true, f.Service.options.Now())
				id := oldGeneration.ID
				if scenario == "foreign_plan_generation" {
					id = f.Generation.ID
				}
				if err := f.Store.DB.Model(&models.AIGeneration{}).Where("id=?", id).Update("session_id", other.ID).Error; err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "foreign_plan_generation" {
				response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan/continuation?expected_version=1", nil, nil)
				assertAPIError(t, response, http.StatusConflict, "AI_PLAN_CONTINUATION_UNAVAILABLE")
				return
			}
			assertAIPlanContinuation(t, f, 1, "evidence_unavailable", "")
		})
	}
}

func TestAIWorkPlanContinuationReadDoesNotMutateAnyOwnedFacts(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE unchanged task","description":"PRIVATE body"}}`)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(proposal.ID), 0))
	finishAIGeneration(t, f.Store, f.Generation)
	decideAIPlanProposal(t, f, proposal, "reject")
	snapshot := func() string {
		t.Helper()
		all := map[string]any{}
		for _, table := range []string{"ai_sessions", "ai_generations", "ai_messages", "ai_memory_entries", "ai_action_proposals", "ai_work_plan_revisions", "agent_runs", "tasks", "workflow_events"} {
			var rows []map[string]any
			order := "id"
			if table == "ai_work_plan_revisions" {
				order = "session_id,version"
			}
			if err := f.Store.DB.Table(table).Order(order).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			all[table] = rows
		}
		encoded, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	before := snapshot()
	for index := 0; index < 3; index++ {
		assertAIPlanContinuation(t, f, 1, "ready")
	}
	if after := snapshot(); before != after {
		t.Fatal("read-only continuation check changed stored facts")
	}
}

func TestAIWorkPlanContinuationExpiredProposalCanBeReconsideredWithoutExecuting(t *testing.T) {
	f := newAIAgentRunFixture(t)
	now := f.Service.options.Now()
	f.Service.options.Now = func() time.Time { return now.Add(-25 * time.Hour) }
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE expired action"}}`)
	f.Service.options.Now = func() time.Time { return now }
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(proposal.ID), 0))
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 1, "ready")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, proposal.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
}

func TestAIWorkPlanContinuationOldBoundMissingRunTakesPriorityOverPendingSibling(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE sibling of missing run"}}`)
	run := seedRunningAgentRun(t, f)
	if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id=?", proposal.ID).Updates(map[string]any{
		"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": f.Generation.CreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	oldID := f.Generation.ID
	f = nextAIPlanGeneration(t, f)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(proposal.ID), 0))
	finishAIGeneration(t, f.Store, f.Generation)
	if err := f.Store.DB.Delete(&models.AgentRun{}, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertAIPlanContinuation(t, f, 1, "evidence_unavailable", oldID)
}

func TestAIWorkPlanContinuationMissingAutomationResultDoesNotRelaxProjection(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprintf("bound_%v", bound), func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			failed := failAIProjectAutomation(t, f.Router.Engine, f.Store)
			proposal := proposeTestAction(t, f.Store, f.Tool, fmt.Sprintf(`{"action":"automation.retry","automation_run_id":%q,"expected_version":%d,"changes":{}}`, failed.ID, failed.RuleVersion))
			binding := ""
			if bound {
				binding = proposal.ID
			}
			decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(binding), 0))
			finishAIGeneration(t, f.Store, f.Generation)
			response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_automation_retry":true}`, proposal.Fingerprint)), nil)
			if response.Code != http.StatusOK {
				t.Fatal(response.Body.String())
			}
			if err := f.Store.DB.First(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil {
				t.Fatalf("load confirmed retry=%+v err=%v", proposal, err)
			}
			assertAIPlanContinuation(t, f, 1, "ready")
			if err := f.Store.DB.Delete(&models.AutomationRun{}, "id=?", *proposal.ResultID).Error; err != nil {
				t.Fatal(err)
			}
			if bound {
				response = performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan/continuation?expected_version=1", nil, nil)
				assertAPIError(t, response, http.StatusConflict, "AI_PLAN_CONTINUATION_UNAVAILABLE")
				if strings.Contains(response.Body.String(), "PRIVATE") || strings.Contains(response.Body.String(), "automation_runs") {
					t.Fatal("projection error leaked business or storage internals")
				}
			} else {
				assertAIPlanContinuation(t, f, 1, "evidence_unavailable", f.Generation.ID)
			}
		})
	}
}

func TestAIWorkPlanContinuationDatabaseFailureIsNotMissingEvidence(t *testing.T) {
	f := newAIAgentRunFixture(t)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(""), 0))
	finishAIGeneration(t, f.Store, f.Generation)
	if err := f.Store.DB.Exec("DROP TABLE ai_work_plan_revisions").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan/continuation?expected_version=1", nil, nil)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "ai_work_plan_revisions") || strings.Contains(response.Body.String(), "SELECT") {
		t.Fatalf("database failure was hidden or leaked SQL: %d %s", response.Code, response.Body.String())
	}
}

func TestAIWorkPlanContinuationRejectsMalformedUnboundEvidence(t *testing.T) {
	for _, scenario := range []string{"unknown_action", "fingerprint_drift"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(""), 0))
			action := `{"action":"task.create","changes":{"title":"PRIVATE malformed action"}}`
			if scenario == "unknown_action" {
				action = `{"action":"future.unknown","changes":{}}`
			}
			fingerprint := sha256Hex([]byte(action))
			if scenario == "fingerprint_drift" {
				fingerprint = strings.Repeat("0", 64)
			}
			proposal := models.AIActionProposal{ID: uuid.NewString(), GenerationID: f.Generation.ID, Status: "pending", ActionJSON: action, PreviewJSON: `{}`, Fingerprint: fingerprint, CreatedAt: f.Generation.CreatedAt}
			if err := f.Store.DB.Create(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			finishAIGeneration(t, f.Store, f.Generation)
			assertAIPlanContinuation(t, f, 1, "evidence_unavailable", f.Generation.ID)
		})
	}
}

func TestAIWorkPlanContinuationExecutionStatesAreClosed(t *testing.T) {
	for _, scenario := range []struct {
		status, delivery, submissionStatus, want string
	}{
		{"queued", "not_ready", "", "run_active"}, {"running", "not_ready", "", "run_active"},
		{"running", "pending", "", "output_pending"},
		{"succeeded", "submitted", "pending_review", ""}, {"succeeded", "submitted", "accepted", ""},
		{"succeeded", "submitted", "changes_requested", ""}, {"succeeded", "submitted", "withdrawn", ""},
		{"succeeded", "submitted", "", "evidence_unavailable"}, {"succeeded", "submitted", "unknown", "evidence_unavailable"},
		{"succeeded", "retained", "", ""},
		{"failed", "not_ready", "", ""}, {"cancelled", "not_ready", "", ""}, {"interrupted", "not_ready", "", ""},
		{"unknown", "not_ready", "", "evidence_unavailable"}, {"succeeded", "not_ready", "", "evidence_unavailable"},
		{"queued", "pending", "", "evidence_unavailable"}, {"succeeded", "pending", "", "evidence_unavailable"},
		{"running", "submitted", "pending_review", "evidence_unavailable"}, {"failed", "retained", "", "evidence_unavailable"},
		{"succeeded", "unknown", "", "evidence_unavailable"},
	} {
		id := uuid.NewString()
		var submissionStatus *string
		if scenario.submissionStatus != "" {
			submissionStatus = &scenario.submissionStatus
		}
		out := aiActionResponse{Action: aiWorkspaceAction{Action: "agent_run.start"}, Status: "confirmed", ResultID: &id,
			AgentRunResult: &aiAgentRunResult{ID: id, Status: scenario.status, OutputDeliveryStatus: scenario.delivery, SubmissionStatus: submissionStatus}}
		if got := aiPlanContinuationActionBlocker(out); got != scenario.want {
			t.Fatalf("state=%+v blocker=%s", scenario, got)
		}
	}
	for _, run := range []*aiAutomationRetryResult{nil, {Status: "unknown"}} {
		if got := aiPlanContinuationActionBlocker(aiActionResponse{Action: aiWorkspaceAction{Action: "automation.retry"}, Status: "confirmed", AutomationRunResult: run}); got != "evidence_unavailable" {
			t.Fatalf("unverifiable automation accepted: %+v", run)
		}
	}
}
