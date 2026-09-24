package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeClosedPlan(t *testing.T, code int, body string) aiWorkPlanView {
	t.Helper()
	var result struct {
		Data aiWorkPlanView `json:"data"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &result) != nil || result.Data.ClosedAt == nil {
		t.Fatalf("close plan=%d: %s", code, body)
	}
	return result.Data
}

func planInboxMeta(t *testing.T, f aiAgentRunFixture, state string) (int, aiWorkPlanInboxMeta, []aiWorkPlanInboxItem) {
	t.Helper()
	response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/work-plans?state="+state, nil, nil)
	var result struct {
		Data []aiWorkPlanInboxItem `json:"data"`
		Meta aiWorkPlanInboxMeta   `json:"meta"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("plan inbox=%d: %s", response.Code, response.Body.String())
	}
	return len(result.Data), result.Meta, result.Data
}

func TestAIWorkPlanCloseOnlyWithoutOpenObligationsAndOnce(t *testing.T) {
	f := newAIAgentRunFixture(t)
	bound := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE bound task"}}`)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(bound.ID), 0))
	path := "/api/v1/ai/sessions/" + f.Generation.SessionID + "/plan/close"
	closeBody := []byte(`{"expected_version":1,"confirm_close":true}`)

	// An active generation, then a pending approval, each keep the plan open.
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, closeBody, nil), http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED")
	finishAIGeneration(t, f.Store, f.Generation)
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, closeBody, nil), http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED")
	decideAIPlanProposal(t, f, bound, "reject")
	assertAIPlanContinuation(t, f, 1, "ready")

	for _, body := range []string{`{"expected_version":1}`, `{"expected_version":0,"confirm_close":true}`, `{"expected_version":129,"confirm_close":true}`} {
		assertAPIError(t, performRequest(f.Router, http.MethodPost, path, []byte(body), nil), http.StatusUnprocessableEntity, "AI_PLAN_CLOSE_INVALID")
	}
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, []byte(`{"expected_version":1,"confirm_close":true,"reason":"x"}`), nil), http.StatusBadRequest, "INVALID_JSON")
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, []byte(`{"expected_version":2,"confirm_close":true}`), nil), http.StatusConflict, "AI_PLAN_CLOSE_CHANGED")
	assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/not-a-uuid/plan/close", closeBody, nil), http.StatusUnprocessableEntity, "AI_SESSION_ID_INVALID")
	var tasksBefore int64
	if err := f.Store.DB.Model(&models.Task{}).Count(&tasksBefore).Error; err != nil {
		t.Fatal(err)
	}

	first := performRequest(f.Router, http.MethodPost, path, closeBody, nil)
	closed := decodeClosedPlan(t, first.Code, first.Body.String())
	replay := performRequest(f.Router, http.MethodPost, path, closeBody, nil)
	replayed := decodeClosedPlan(t, replay.Code, replay.Body.String())
	if *replayed.ClosedAt != *closed.ClosedAt {
		t.Fatalf("replayed close changed its time: %s vs %s", *replayed.ClosedAt, *closed.ClosedAt)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_work_plan_closed'", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", tasksBefore)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='rejected'", 1)

	// Closure removes the plan from attention and blocks every continuation.
	assertAIPlanContinuation(t, f, 1, "plan_closed")
	if count, meta, _ := planInboxMeta(t, f, "attention"); count != 0 || meta.AttentionTotal != 0 || meta.ClosedTotal != 1 {
		t.Fatalf("closed plan still needs attention: count=%d meta=%#v", count, meta)
	}
	if count, _, items := planInboxMeta(t, f, "closed"); count != 1 || items[0].State != "closed" {
		t.Fatalf("closed filter: %#v", items)
	}
	plan := performRequest(f.Router, http.MethodGet, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan", nil, nil)
	decodeClosedPlan(t, plan.Code, plan.Body.String())

	// A later model-authored revision is a new intent and starts open.
	f = nextAIPlanGeneration(t, f)
	read := decodePlanTool(t, planTool(t, f), []byte(`{"operation":"read"}`))
	if read == nil || read.ClosedAt == nil {
		t.Fatalf("plan tool did not disclose the closure: %#v", read)
	}
	revised := sampleWorkPlan(bound.ID)
	revised.Steps[2].Report = "in_progress"
	if next := decodePlanTool(t, planTool(t, f), updatePlanArgs(revised, 1)); next.Version != 2 || next.ClosedAt != nil {
		t.Fatalf("new revision inherited closure: %#v", next)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	assertAIPlanContinuation(t, f, 2, "ready")
	if count, meta, _ := planInboxMeta(t, f, "attention"); count != 1 || meta.ClosedTotal != 0 {
		t.Fatalf("new revision is not back in attention: count=%d meta=%#v", count, meta)
	}
}

func TestAIWorkPlanCloseRejectsActiveRunsAndRetainedRecovery(t *testing.T) {
	f := newAIAgentRunFixture(t)
	agentProposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(agentProposal.ID), 0))
	run := seedRunningAgentRun(t, f)
	if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id=?", agentProposal.ID).Updates(map[string]any{
		"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": f.Generation.CreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	path := "/api/v1/ai/sessions/" + f.Generation.SessionID + "/plan/close"
	body := []byte(`{"expected_version":1,"confirm_close":true}`)
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED")
	if err := transitionAgentRunOutput(f.Store.DB, run.ID, "PRIVATE retained", f.Generation.CreatedAt, agentRunOutputRetained, "TEST_RETAINED", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Retained output is recovery work the plan still owns.
	assertAIPlanContinuation(t, f, 1, "ready")
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_work_plan_closed'", 0)
}

func TestAIWorkPlanCloseDefersToActiveContinuationAndRefusesNewOnes(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	now := f.Generation.CreatedAt
	created, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		t.Fatal(err)
	}
	continuation := models.AIContinuation{
		ID: "018f0000-0000-7000-8000-00000000c105", SessionID: f.Generation.SessionID,
		ProviderID: f.Provider.ID, ProviderVersion: f.Provider.Version,
		ProviderConfigVersion: f.Provider.ConfigVersion, ProviderName: f.Provider.Name,
		ProviderKind: f.Provider.Kind, ProviderProtocol: f.Provider.Protocol,
		ProviderModel: f.Provider.Model, WorkspaceJSON: `{"provider_version":1,"scopes":["actions","work"]}`,
		InitialPlanVersion: 1, CurrentPlanVersion: 1, MaxTurns: 3,
		Status: "waiting", Reason: "ready", Version: 1,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: created.Add(time.Hour).Format(time.RFC3339Nano),
	}
	if err := f.Store.DB.Create(&continuation).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/sessions/" + f.Generation.SessionID + "/plan/close"
	body := []byte(`{"expected_version":1,"confirm_close":true}`)
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED")
	if _, err := a.stopAIContinuationID(t.Context(), continuation.ID); err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodPost, path, body, nil)
	decodeClosedPlan(t, response.Code, response.Body.String())
	input, _ := json.Marshal(continuationTestInput(f))
	assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", input, nil),
		http.StatusConflict, "AI_CONTINUATION_NOT_ELIGIBLE")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_continuations WHERE status IN ('waiting','running')", 0)
}

func TestAIWorkPlanCloseReleasesFailedRunsToTheUnplannedLedger(t *testing.T) {
	f := newAIAgentRunFixture(t)
	agentProposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	decodePlanTool(t, planTool(t, f), updatePlanArgs(sampleWorkPlan(agentProposal.ID), 0))
	run := seedRunningAgentRun(t, f)
	if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id=?", agentProposal.ID).Updates(map[string]any{
		"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": f.Generation.CreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id=?", run.ID).Updates(map[string]any{
		"status": "failed", "completed_at": f.Generation.CreatedAt, "error_code": "TEST_FAILED",
	}).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	unplanned := func() (int64, string) {
		response := performRequest(f.Router, http.MethodGet, "/api/v1/agent-runs?attention=1&plan_link=unplanned&page=1&page_size=10", nil, nil)
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Meta struct {
				Total int64 `json:"total"`
			} `json:"meta"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatalf("unplanned runs=%d: %s", response.Code, response.Body.String())
		}
		if len(result.Data) == 0 {
			return result.Meta.Total, ""
		}
		return result.Meta.Total, result.Data[0].ID
	}
	if total, _ := unplanned(); total != 0 {
		t.Fatalf("plan-owned failed Run leaked into the unplanned ledger: %d", total)
	}
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan/close",
		[]byte(`{"expected_version":1,"confirm_close":true}`), nil)
	decodeClosedPlan(t, response.Code, response.Body.String())
	if total, id := unplanned(); total != 1 || id != run.ID {
		t.Fatalf("closed plan still hides its failed Run: total=%d id=%s", total, id)
	}
	var stored models.AgentRun
	if err := f.Store.DB.Take(&stored, "id=?", run.ID).Error; err != nil || stored.Status != "failed" {
		t.Fatalf("closing a plan changed its Run: %#v err=%v", stored, err)
	}
}
