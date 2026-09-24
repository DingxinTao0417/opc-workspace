package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func confirmTestAgentRunProposal(t *testing.T, fixture aiAgentRunFixture, proposal models.AIActionProposal, runID string) models.AIActionProposal {
	t.Helper()
	decidedAt := fixture.Service.options.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	if err := fixture.Store.DB.Model(&models.AIActionProposal{}).Where("id = ?", proposal.ID).Updates(map[string]any{
		"status": "confirmed", "result_id": runID, "result_version": 1, "decided_at": decidedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.Store.DB.First(&proposal, "id = ?", proposal.ID).Error; err != nil {
		t.Fatal(err)
	}
	return proposal
}

func TestAISessionDelegatedRunsProjectsConfirmedRunAndLatestPlan(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, fixture.Store, fixture.Tool, aiAgentRunActionJSON(fixture))
	run := seedRunningAgentRun(t, fixture)
	proposal = confirmTestAgentRunProposal(t, fixture, proposal, run.ID)

	plan := sampleWorkPlan(proposal.ID)
	plan.Steps[1].Title = "Delegate the report"
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	revision := aiWorkPlanRevision{
		SessionID: fixture.Generation.SessionID, Version: 1, GenerationID: fixture.Generation.ID,
		PlanJSON: string(planJSON), CreatedAt: fixture.Generation.CreatedAt,
	}
	if err := fixture.Store.DB.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/"+fixture.Generation.SessionID+"/delegated-runs", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []aiSessionDelegatedRunResponse `json:"data"`
		Meta aiSessionDelegatedRunMeta       `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Run == nil || payload.Data[0].Run.ID != run.ID ||
		payload.Data[0].Delegation.ProposalID != proposal.ID || payload.Data[0].Delegation.Action != "agent_run.start" ||
		payload.Data[0].Delegation.Plan == nil || payload.Data[0].Delegation.Plan.StepID != "record" ||
		payload.Data[0].Delegation.Plan.StepTitle != "Delegate the report" || payload.Data[0].Delegation.Plan.Version != 1 {
		t.Fatalf("unexpected projection: %+v", payload)
	}
	if payload.Meta.Total != 1 || payload.Meta.ActiveTotal != 1 || payload.Meta.PendingDeliveryTotal != 0 || payload.Meta.UnavailableTotal != 0 {
		t.Fatalf("unexpected meta: %+v", payload.Meta)
	}
	for _, secret := range []string{"PRIVATE ACTOR NOTES", "MUST_NOT_LEAK", fixture.Task.Description, "action_json", "preview_json", "input_snapshot_json", "result_text"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("private field leaked: %q body=%s", secret, response.Body.String())
		}
	}

	filtered := performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/"+fixture.Generation.SessionID+"/delegated-runs?output_delivery_status=pending", nil, nil)
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), `"total":0`) {
		t.Fatalf("filtered status=%d body=%s", filtered.Code, filtered.Body.String())
	}
}

func TestAISessionDelegatedRunsPreservesUnavailableReceipt(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, fixture.Store, fixture.Tool, aiAgentRunActionJSON(fixture))
	missingRunID := uuid.NewString()
	proposal = confirmTestAgentRunProposal(t, fixture, proposal, missingRunID)

	response := performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/"+fixture.Generation.SessionID+"/delegated-runs", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []aiSessionDelegatedRunResponse `json:"data"`
		Meta aiSessionDelegatedRunMeta       `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Run != nil || payload.Data[0].RunID != missingRunID ||
		payload.Data[0].Delegation.ProposalID != proposal.ID || payload.Meta.UnavailableTotal != 1 {
		t.Fatalf("unavailable receipt lost: %+v", payload)
	}
}

func TestAISessionDelegatedRunsRejectsAmbiguousAndInvalidSession(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, fixture.Store, fixture.Tool, aiAgentRunActionJSON(fixture))
	run := seedRunningAgentRun(t, fixture)
	proposal = confirmTestAgentRunProposal(t, fixture, proposal, run.ID)

	secondGeneration := models.AIGeneration{
		ID: uuid.NewString(), SessionID: fixture.Generation.SessionID, ProviderID: fixture.Provider.ID, Status: "completed",
		CreatedAt: fixture.Generation.CreatedAt, UpdatedAt: fixture.Generation.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&secondGeneration).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := proposal
	duplicate.ID = uuid.NewString()
	duplicate.GenerationID = secondGeneration.ID
	duplicate.ResultID = &run.ID
	if err := fixture.Store.DB.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/"+fixture.Generation.SessionID+"/delegated-runs", nil, nil)
	assertAPIError(t, response, http.StatusConflict, "AI_AGENT_RUN_LINK_AMBIGUOUS")
	assertAPIError(t, performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/NOT-A-UUID/delegated-runs", nil, nil), http.StatusBadRequest, "INVALID_AI_SESSION_ID")
	assertAPIError(t, performRequest(fixture.Router, http.MethodGet,
		"/api/v1/ai/sessions/"+uuid.NewString()+"/delegated-runs", nil, nil), http.StatusNotFound, "AI_SESSION_NOT_FOUND")
}
