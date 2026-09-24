package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAgentRunStartGateDecisionTable(t *testing.T) {
	status := func(value string) *string { return &value }
	run := func(runStatus, delivery string) *models.AgentRun {
		return &models.AgentRun{Status: runStatus, OutputDeliveryStatus: delivery}
	}
	for _, tt := range []struct {
		name       string
		run        *models.AgentRun
		submission *string
		require    string
		want       string
	}{
		{"queued waits", run("queued", agentRunOutputNotReady), nil, agentRunStartAfterSubmitted, "wait"},
		{"running waits", run("running", agentRunOutputNotReady), nil, agentRunStartAfterAccepted, "wait"},
		{"pending delivery waits", run("running", agentRunOutputPending), nil, agentRunStartAfterSubmitted, "wait"},
		{"submitted releases submitted", run("succeeded", agentRunOutputSubmitted), status("pending_review"), agentRunStartAfterSubmitted, "release"},
		{"review pending waits for accepted", run("succeeded", agentRunOutputSubmitted), status("pending_review"), agentRunStartAfterAccepted, "wait"},
		{"accepted releases accepted", run("succeeded", agentRunOutputSubmitted), status("accepted"), agentRunStartAfterAccepted, "release"},
		{"rework releases submitted", run("succeeded", agentRunOutputSubmitted), status("changes_requested"), agentRunStartAfterSubmitted, "release"},
		{"rework closes accepted", run("succeeded", agentRunOutputSubmitted), status("changes_requested"), agentRunStartAfterAccepted, "predecessor_not_accepted"},
		{"withdrawn closes accepted", run("succeeded", agentRunOutputSubmitted), status("withdrawn"), agentRunStartAfterAccepted, "predecessor_not_accepted"},
		{"missing submission is unavailable", run("succeeded", agentRunOutputSubmitted), nil, agentRunStartAfterSubmitted, "predecessor_unavailable"},
		{"retained closes", run("succeeded", agentRunOutputRetained), nil, agentRunStartAfterSubmitted, "predecessor_retained"},
		{"failed closes", run("failed", agentRunOutputNotReady), nil, agentRunStartAfterSubmitted, "predecessor_failed"},
		{"interrupted never releases", run("interrupted", agentRunOutputNotReady), nil, agentRunStartAfterSubmitted, "predecessor_failed"},
		{"cancelled closes", run("cancelled", agentRunOutputNotReady), nil, agentRunStartAfterAccepted, "predecessor_cancelled"},
		{"illegal pair is unavailable", run("queued", agentRunOutputPending), nil, agentRunStartAfterSubmitted, "predecessor_unavailable"},
		{"missing Run is unavailable", nil, nil, agentRunStartAfterSubmitted, "predecessor_unavailable"},
	} {
		if got := decideAgentRunStartGate(tt.run, tt.submission, tt.require); got != tt.want {
			t.Errorf("%s: decision=%q want %q", tt.name, got, tt.want)
		}
	}
}

func seedAgentGateTask(t *testing.T, f aiAgentRunFixture, title string) models.Task {
	t.Helper()
	now := f.Service.options.Now().UTC().Format(time.RFC3339Nano)
	task := models.Task{
		ID: uuid.NewString(), Title: title, Description: "Follow-up work.", CompletionCriteria: "Return a reviewable report.",
		Kind: "work", Status: "todo", ReviewPolicy: "manual", Priority: "P2", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := f.Store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: task.ID, ActorID: f.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "gate test",
	}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	return task
}

func gatedStartJSON(f aiAgentRunFixture, task models.Task, predecessorID, require string, hours int) string {
	return fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d,"start_after_run_id":%q,"start_after":%q,"start_within_hours":%d}}`,
		task.ID, task.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion, predecessorID, require, hours)
}

func confirmGatedStart(t *testing.T, f aiAgentRunFixture, proposal models.AIActionProposal) models.AgentRun {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, proposal.Fingerprint))
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm gated start=%d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ResultID       string           `json:"result_id"`
			AgentRunResult aiAgentRunResult `json:"agent_run_result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	gate := envelope.Data.AgentRunResult.StartGate
	if gate == nil || gate.Status != agentRunStartGateWaiting || envelope.Data.AgentRunResult.Status != "queued" {
		t.Fatalf("receipt did not disclose a waiting gate: %s", response.Body.String())
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id = ?", envelope.Data.ResultID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func gateOf(t *testing.T, f aiAgentRunFixture, runID string) *models.AgentRunStartGate {
	t.Helper()
	gates, err := readAgentRunStartGates(f.Store.DB, []string{runID})
	if err != nil {
		t.Fatal(err)
	}
	return gates[runID]
}

func TestAgentRunStartGateHoldsDispatchUntilPredecessorSubmits(t *testing.T) {
	f := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, f)
	api := f.Router.agentRunGates.a
	predecessor := seedRunningAgentRun(t, f)
	follow := seedAgentGateTask(t, f, "Publish the reviewed report")
	proposal := proposeTestAction(t, f.Store, f.Tool, gatedStartJSON(f, follow, predecessor.ID, agentRunStartAfterSubmitted, 2))
	if !strings.Contains(proposal.PreviewJSON, `"start_gate":{"predecessor_run_id":"`+predecessor.ID+`"`) ||
		strings.Contains(proposal.PreviewJSON, "predecessor_status") {
		t.Fatalf("preview did not freeze only the stable gate identity: %s", proposal.PreviewJSON)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	gated := confirmGatedStart(t, f, proposal)
	if gated.TaskID != follow.ID || gated.Status != "queued" {
		t.Fatalf("gated Run = %#v", gated)
	}

	// Neither the FIFO scan nor a direct worker claim may start a waiting Run.
	queued, err := api.oldestQueuedAgentRunIDs(8)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range queued {
		if id == gated.ID {
			t.Fatal("dispatcher selected a Run whose gate is still waiting")
		}
	}
	if api.executeAgentRun(context.Background(), gated.ID) {
		t.Fatal("claim guard asked to retry a waiting Run")
	}
	var stillQueued models.AgentRun
	if err := f.Store.DB.First(&stillQueued, "id = ?", gated.ID).Error; err != nil || stillQueued.Status != "queued" {
		t.Fatalf("waiting Run was claimed: %#v err=%v", stillQueued, err)
	}
	if outcome, err := api.reconcileAgentRunStartGates(api.options.Now()); err != nil || len(outcome.Released)+len(outcome.Closed) != 0 {
		t.Fatalf("reconcile changed a gate while the predecessor runs: %#v err=%v", outcome, err)
	}

	completedAt := api.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := f.Service.finalizeAgentRun(predecessor, "review me", "", completedAt); err != nil {
		t.Fatalf("finalize predecessor: %v", err)
	}
	if _, err := api.reconcileAgentRunStartGates(api.options.Now().Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if gate := gateOf(t, f, gated.ID); gate == nil || gate.Status != agentRunStartGateReleased {
		t.Fatalf("gate was not released after submission: %#v", gate)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='agent_run_start_gate_released'", 1, gated.ID)
	// Settled gates are never settled twice.
	if _, err := api.reconcileAgentRunStartGates(api.options.Now().Add(3 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action IN ('agent_run_start_gate_released','agent_run_start_gate_closed')", 1, gated.ID)
}

func TestAgentRunStartGateCancelsWhenThePredecessorFailsOrItExpires(t *testing.T) {
	for _, mode := range []string{"failed", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			api := f.Router.agentRunGates.a
			predecessor := seedRunningAgentRun(t, f)
			follow := seedAgentGateTask(t, f, "Follow-up "+mode)
			proposal := proposeTestAction(t, f.Store, f.Tool, gatedStartJSON(f, follow, predecessor.ID, agentRunStartAfterAccepted, 2))
			finishAIGeneration(t, f.Store, f.Generation)
			gated := confirmGatedStart(t, f, proposal)
			now := api.options.Now()
			want := "expired"
			if mode == "failed" {
				want = "predecessor_failed"
				if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", predecessor.ID).Updates(map[string]any{
					"status": "failed", "completed_at": now.Format(time.RFC3339Nano), "error_code": "TEST_FAILED",
				}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				now = now.Add(2*time.Hour + time.Second)
			}
			outcome, err := api.reconcileAgentRunStartGates(now)
			if err != nil || outcome.Closed[gated.ID] != want {
				t.Fatalf("outcome=%#v err=%v", outcome, err)
			}
			var cancelled models.AgentRun
			if err := f.Store.DB.First(&cancelled, "id = ?", gated.ID).Error; err != nil || cancelled.Status != "cancelled" || cancelled.ErrorCode != nil {
				t.Fatalf("waiting Run was not cancelled without executing: %#v err=%v", cancelled, err)
			}
			gate := gateOf(t, f, gated.ID)
			if gate == nil || gate.Status != agentRunStartGateClosed || gate.CloseReason == nil || *gate.CloseReason != want {
				t.Fatalf("gate = %#v", gate)
			}
			var predecessorNow models.AgentRun
			if err := f.Store.DB.First(&predecessorNow, "id = ?", predecessor.ID).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "expired" && predecessorNow.Status != "running" {
				t.Fatalf("expiry touched the predecessor: %#v", predecessorNow)
			}
		})
	}
}

func TestAgentRunStartGateProposalBoundaries(t *testing.T) {
	f := newAIAgentRunFixture(t)
	predecessor := seedRunningAgentRun(t, f)
	follow := seedAgentGateTask(t, f, "Follow-up boundaries")
	execute := func(args string) error {
		_, err := f.Tool.Execute(context.Background(), []byte(args))
		return err
	}
	base := fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d`,
		follow.ID, follow.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion)
	for name, tail := range map[string]string{
		"missing hours":    fmt.Sprintf(`,"start_after_run_id":%q,"start_after":"submitted"}}`, predecessor.ID),
		"bad requirement":  fmt.Sprintf(`,"start_after_run_id":%q,"start_after":"done","start_within_hours":2}}`, predecessor.ID),
		"too long":         fmt.Sprintf(`,"start_after_run_id":%q,"start_after":"submitted","start_within_hours":25}}`, predecessor.ID),
		"non canonical id": fmt.Sprintf(`,"start_after_run_id":%q,"start_after":"submitted","start_within_hours":2}}`, strings.ToUpper(predecessor.ID)),
		"with restart":     fmt.Sprintf(`,"restart_of_run_id":%q,"start_after_run_id":%q,"start_after":"submitted","start_within_hours":2}}`, predecessor.ID, predecessor.ID),
	} {
		if err := execute(base + tail); err == nil {
			t.Errorf("%s: gate proposal was accepted", name)
		}
	}
	if err := execute(gatedStartJSON(f, f.Task, predecessor.ID, agentRunStartAfterSubmitted, 2)); err == nil {
		t.Error("a Run was allowed to wait for another Run of the same Task")
	}
	if err := execute(gatedStartJSON(f, follow, uuid.NewString(), agentRunStartAfterSubmitted, 2)); err == nil {
		t.Error("an unknown predecessor was accepted")
	}
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", predecessor.ID).Updates(map[string]any{
		"status": "failed", "completed_at": f.Service.options.Now().UTC().Format(time.RFC3339Nano), "error_code": "TEST_FAILED",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := execute(gatedStartJSON(f, follow, predecessor.ID, agentRunStartAfterSubmitted, 2)); err == nil {
		t.Error("an unsatisfiable predecessor was accepted")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAgentRunStartGateBudgetIsBounded(t *testing.T) {
	f := newAIAgentRunFixture(t)
	predecessor := seedRunningAgentRun(t, f)
	for index := 0; index < agentRunStartGateMaxOpen; index++ {
		task := seedAgentGateTask(t, f, fmt.Sprintf("Gated %d", index))
		proposeTestAction(t, f.Store, f.Tool, gatedStartJSON(f, task, predecessor.ID, agentRunStartAfterSubmitted, 1))
	}
	finishAIGeneration(t, f.Store, f.Generation)
	var proposals []models.AIActionProposal
	if err := f.Store.DB.Order("created_at, id").Find(&proposals).Error; err != nil {
		t.Fatal(err)
	}
	for _, proposal := range proposals {
		confirmGatedStart(t, f, proposal)
	}
	f = nextAIPlanGeneration(t, f)
	extra := seedAgentGateTask(t, f, "Over budget")
	if _, err := f.Tool.Execute(context.Background(), []byte(gatedStartJSON(f, extra, predecessor.ID, agentRunStartAfterSubmitted, 1))); err == nil ||
		!strings.Contains(err.Error(), "four gated") {
		t.Fatalf("fifth waiting Run was accepted: %v", err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_start_gated'", agentRunStartGateMaxOpen)
}
