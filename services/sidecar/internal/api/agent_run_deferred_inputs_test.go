package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	deferredInputResearch = "PREDECESSOR_RESEARCH: 已由负责人验收的调研结论。"
	deferredInputNotes    = "PREDECESSOR_NOTES: 已由负责人验收的补充说明。"
)

// deferredInputFixture has a predecessor Run that is really executing (its
// model call is held open) and a second Task in the same Project that is
// meant to take the predecessor's files once a person has accepted them.
type deferredInputFixture struct {
	*anthropicAgentRunFixture
	Predecessor models.AgentRun
	SourceTask  models.Task
	Follow      models.Task
	Propose     harness.Tool
	// finishPredecessor lets the predecessor's model call return.
	finishPredecessor func()
}

func waitForDeferredInput(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newDeferredInputFixture(t *testing.T, next func(int, map[string]any, http.ResponseWriter)) *deferredInputFixture {
	t.Helper()
	held := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(held) }) }
	f := newAnthropicAgentRunFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		if call == 1 {
			<-held
			bodies, _ := json.Marshal([]string{deferredInputResearch, deferredInputNotes})
			writeAnthropicAgentRunResponse(w, string(bodies), "end_turn")
			return
		}
		next(call, request, w)
	})
	// Registered last, so a held model call is released before the server closes.
	t.Cleanup(finish)
	f.addProject(t)
	predecessor := f.create(t, `,"output_contract":{"type":"files","files":[{"name":"research.md","mime":"text/markdown"},{"name":"notes.md","mime":"text/markdown"}]}`)
	waitForDeferredInput(t, "the predecessor to reach the model", func() bool { return f.Calls.Load() == 1 })

	result := &deferredInputFixture{anthropicAgentRunFixture: f, Predecessor: predecessor, SourceTask: f.Task, finishPredecessor: finish}
	now := f.Service.options.Now().UTC().Format(time.RFC3339Nano)
	follow := models.Task{
		ID: uuid.NewString(), ProjectID: f.Task.ProjectID, Title: "Write the follow-on proposal",
		Description: "Build on the accepted research.", CompletionCriteria: "A reviewable follow-on proposal.",
		Kind: "work", Status: "todo", ReviewPolicy: "manual", Priority: "P1", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := f.Store.DB.Create(&follow).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: follow.ID, ActorID: f.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "follow-on assignment",
	}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	f.Task = follow
	addAgentRunOwnerReviewer(t, f.aiAgentRunFixture)
	if err := f.Store.DB.First(&result.Follow, "id = ?", follow.ID).Error; err != nil {
		t.Fatal(err)
	}
	result.Propose = result.proposeTool(t, "work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files")
	f.Native.GET("/api/v1/agent-runs/:id/start-gate/inputs", f.Service.getAgentRunStartGateInputs)
	f.Native.POST("/api/v1/agent-runs/:id/start-gate/inputs/confirm", f.Service.confirmAgentRunStartGateInputs)
	return result
}

func (f *deferredInputFixture) proposeTool(t *testing.T, scopes ...string) harness.Tool {
	t.Helper()
	grant := &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, grant, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("workspace_propose missing")
	}
	return tool
}

// startJSON proposes the follow-on start. extra is appended to changes.
func (f *deferredInputFixture) startJSON(require, extra string) string {
	return fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d,"start_after_run_id":%q,"start_after":%q,"start_within_hours":2,"start_after_input":"predecessor_files"%s}}`,
		f.Follow.ID, f.Follow.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion, f.Predecessor.ID, require, extra)
}

func (f *deferredInputFixture) decide(proposal models.AIActionProposal, consents string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, proposal.Fingerprint, consents)
	return performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(body), nil)
}

// confirmWaitingStart takes the proposal through the first human confirmation
// and returns the queued Run that now waits for the predecessor.
func (f *deferredInputFixture) confirmWaitingStart(t *testing.T) models.AgentRun {
	t.Helper()
	proposal := proposeTestAction(t, f.Store, f.Propose, f.startJSON(agentRunStartAfterAccepted, ""))
	finishAIGeneration(t, f.Store, f.Generation)
	response := f.decide(proposal, `,"confirm_agent_execution":true,"confirm_agent_predecessor_input":true`)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm waiting start=%d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ResultID string `json:"result_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id = ?", envelope.Data.ResultID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

// acceptPredecessor lets the predecessor finish and has the owner accept it.
func (f *deferredInputFixture) acceptPredecessor(t *testing.T) {
	t.Helper()
	f.finishPredecessor()
	done := f.wait(t, f.Predecessor.ID)
	if done.Status != "succeeded" || done.OutputDeliveryStatus != agentRunOutputSubmitted {
		t.Fatalf("predecessor did not submit its files: %#v", done)
	}
	var source models.Task
	if err := f.Store.DB.First(&source, "id = ?", f.SourceTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	review := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+source.ID+"/review", []byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, source.Version)})
	if review.Code != http.StatusOK {
		t.Fatalf("accept predecessor=%d: %s", review.Code, review.Body.String())
	}
	if err := f.Store.DB.First(&f.SourceTask, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
}

type deferredInputsView struct {
	Status      string                          `json:"status"`
	CloseReason *string                         `json:"close_reason"`
	Files       []frozenAgentRunFileReference   `json:"files"`
	TotalBytes  int                             `json:"total_bytes"`
	Fingerprint string                          `json:"fingerprint"`
	Provider    agentRunStartGateInputsProvider `json:"provider"`
}

func (f *deferredInputFixture) inputs(t *testing.T, runID string) deferredInputsView {
	t.Helper()
	response := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs/"+runID+"/start-gate/inputs", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("read inputs=%d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data deferredInputsView `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func (f *deferredInputFixture) confirmInputs(runID string, body map[string]any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	return performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+runID+"/start-gate/inputs/confirm", encoded, nil)
}

func (f *deferredInputFixture) inputConsents(fingerprint string) map[string]any {
	return map[string]any{
		"confirm_file_access": true, "confirm_project_task_files": true, "fingerprint": fingerprint,
		"file_access_provider_confirmation": map[string]any{
			"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind,
		},
	}
}

// agentRunsToolResult is what the model reads about the Runs of one Task.
func (f *deferredInputFixture) agentRunsToolResult(t *testing.T, taskID string) string {
	t.Helper()
	grant := &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs"}}
	registry, err := f.Service.aiChatToolRegistry("ephemeral", false, &f.Provider, grant)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_agent_runs")
	if !ok {
		t.Fatal("workspace_agent_runs missing")
	}
	raw, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q}`, taskID)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f *deferredInputFixture) slotStatus(t *testing.T, runID string) (string, string) {
	t.Helper()
	gate := gateOf(t, f.aiAgentRunFixture, runID)
	if gate == nil || gate.InputSlot == nil {
		t.Fatalf("Run %s has no input slot: %#v", runID, gate)
	}
	return gate.Status, gate.InputSlot.Status
}

// The whole chain with a real executor: the proposal declares the inputs, the
// predecessor finishes and is accepted, a person confirms the exact files, and
// only then does the follow-on Run start and receive those files.
func TestAgentRunDeferredInputsReachTheSuccessorOnlyAfterTheyAreConfirmed(t *testing.T) {
	f := newDeferredInputFixture(t, func(_ int, request map[string]any, w http.ResponseWriter) {
		encoded, _ := json.Marshal(request)
		for _, want := range []string{deferredInputResearch, deferredInputNotes, "research.md", "notes.md"} {
			if !strings.Contains(string(encoded), want) {
				t.Errorf("the follow-on Run did not receive %q", want)
			}
		}
		for _, forbidden := range []string{"objects/", "relative_path"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Errorf("the follow-on Run received storage detail %q", forbidden)
			}
		}
		writeAnthropicAgentRunResponse(w, "The follow-on proposal is ready for review.", "end_turn")
	})

	proposal := proposeTestAction(t, f.Store, f.Propose, f.startJSON(agentRunStartAfterAccepted, ""))
	for _, want := range []string{
		`"execution_contract_version":7`,
		`"input_slot":{"source":"predecessor_files","expected_files":[{"name":"research.md","mime":"text/markdown"},{"name":"notes.md","mime":"text/markdown"}],"max_files":4,"max_file_bytes":65536,"max_total_bytes":131072}`,
		`"input_files_leave_device":true`,
	} {
		if !strings.Contains(proposal.PreviewJSON, want) {
			t.Fatalf("preview lacks %s: %s", want, proposal.PreviewJSON)
		}
	}
	if strings.Contains(proposal.PreviewJSON, `"input_files":`) {
		t.Fatalf("preview claims files that do not exist yet: %s", proposal.PreviewJSON)
	}
	finishAIGeneration(t, f.Store, f.Generation)

	// The declaration needs its own consent, next to starting the Run.
	missing := f.decide(proposal, `,"confirm_agent_execution":true`)
	assertAPIError(t, missing, http.StatusUnprocessableEntity, "AGENT_PREDECESSOR_INPUT_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id = ?", 0, f.Follow.ID)
	confirmed := f.decide(proposal, `,"confirm_agent_execution":true,"confirm_agent_predecessor_input":true`)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"input_slot":{"source":"predecessor_files","status":"awaiting_predecessor"`) {
		t.Fatalf("confirm=%d: %s", confirmed.Code, confirmed.Body.String())
	}
	var waiting models.AgentRun
	if err := f.Store.DB.First(&waiting, "task_id = ?", f.Follow.ID).Error; err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "queued" || waiting.ExecutionContractVersion != agentRunExecutionContractVersionV7 ||
		!strings.Contains(waiting.InputSnapshotJSON, `"files":[]`) ||
		!strings.Contains(waiting.InputSnapshotJSON, `"deferred_inputs":{"source":"predecessor_files","predecessor_run_id":"`+f.Predecessor.ID+`"`) {
		t.Fatalf("the waiting Run did not freeze only the declaration: %#v", waiting)
	}
	frozenSnapshot := waiting.InputSnapshotJSON

	// Nothing to confirm while the predecessor is still running.
	if view := f.inputs(t, waiting.ID); view.Status != agentRunInputSlotAwaitingPredecessor || len(view.Files) != 0 || view.Fingerprint != "" {
		t.Fatalf("inputs before the predecessor finished = %#v", view)
	}
	early := f.confirmInputs(waiting.ID, f.inputConsents(strings.Repeat("0", 64)))
	assertAPIError(t, early, http.StatusConflict, agentRunInputNotReadyCode)

	f.acceptPredecessor(t)
	// Acceptance alone never starts the Run: the reconciler keeps the gate,
	// the dispatcher skips it and a direct claim is refused.
	outcome, err := f.Service.reconcileAgentRunStartGates(f.Service.options.Now())
	if err != nil || len(outcome.Released) != 0 || len(outcome.Closed) != 0 {
		t.Fatalf("reconcile after acceptance = %#v err=%v", outcome, err)
	}
	if gateStatus, slot := f.slotStatus(t, waiting.ID); gateStatus != agentRunStartGateWaiting || slot != agentRunInputSlotAwaitingConfirmation {
		t.Fatalf("gate=%s slot=%s after acceptance", gateStatus, slot)
	}
	// The model can see that a person is needed, and nothing about the files
	// beyond the names the declaration announced.
	seen := f.agentRunsToolResult(t, f.Follow.ID)
	if !strings.Contains(seen, `"input_slot":{"source":"predecessor_files","status":"awaiting_confirmation"`) ||
		strings.Contains(seen, "sha256") || strings.Contains(seen, "source_task") {
		t.Fatalf("workspace_agent_runs = %s", seen)
	}
	queued, err := f.Service.oldestQueuedAgentRunIDs(8)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range queued {
		if id == waiting.ID {
			t.Fatal("the dispatcher selected a Run whose files nobody confirmed")
		}
	}
	if f.Service.executeAgentRun(context.Background(), waiting.ID) {
		t.Fatal("the claim guard asked to retry an unconfirmed Run")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id = ? AND status = 'queued'", 1, waiting.ID)
	if f.Calls.Load() != 1 {
		t.Fatalf("the model was called %d times before the files were confirmed", f.Calls.Load())
	}

	view := f.inputs(t, waiting.ID)
	if view.Status != agentRunInputSlotAwaitingConfirmation || len(view.Files) != 2 || len(view.Fingerprint) != 64 ||
		!view.Provider.LeavesDevice || view.Files[0].Name != "research.md" || view.Files[1].Name != "notes.md" ||
		view.TotalBytes != len(deferredInputResearch)+len(deferredInputNotes) {
		t.Fatalf("inputs to confirm = %#v", view)
	}
	for _, file := range view.Files {
		if file.SourceKind != agentexec.FileSourceProjectTaskArtifact || file.SourceTask == nil ||
			file.SourceTask.TaskID != f.SourceTask.ID || len(file.SHA256) != 64 {
			t.Fatalf("file to confirm lacks its accepted source: %#v", file)
		}
	}

	// Each part of the confirmation is required and exact.
	for _, drop := range []string{"confirm_file_access", "confirm_project_task_files", "file_access_provider_confirmation"} {
		body := f.inputConsents(view.Fingerprint)
		delete(body, drop)
		response := f.confirmInputs(waiting.ID, body)
		assertAPIError(t, response, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED")
	}
	stale := f.confirmInputs(waiting.ID, f.inputConsents(strings.Repeat("a", 64)))
	assertAPIError(t, stale, http.StatusConflict, agentRunInputChangedCode)
	otherProvider := f.inputConsents(view.Fingerprint)
	otherProvider["file_access_provider_confirmation"] = map[string]any{
		"version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind,
	}
	assertAPIError(t, f.confirmInputs(waiting.ID, otherProvider), http.StatusConflict, agentRunIdentityChanged)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'agent_run_start_gate_released'", 0, waiting.ID)
	if f.Calls.Load() != 1 {
		t.Fatal("a refused confirmation reached the model")
	}

	bound := f.confirmInputs(waiting.ID, f.inputConsents(view.Fingerprint))
	if bound.Code != http.StatusOK || !strings.Contains(bound.Body.String(), `"status":"bound"`) {
		t.Fatalf("confirm inputs=%d: %s", bound.Code, bound.Body.String())
	}
	var released struct {
		CurrentJSON string `gorm:"column:current_json"`
	}
	if err := f.Store.DB.Table("workflow_events").Select("current_json").
		Where("aggregate_id = ? AND action = 'agent_run_start_gate_released'", waiting.ID).Take(&released).Error; err != nil {
		t.Fatal(err)
	}
	var binding agentRunStartGateBinding
	if err := json.Unmarshal([]byte(released.CurrentJSON), &binding); err != nil || len(binding.InputFiles) != 2 ||
		binding.ConfirmedBy != models.BuiltinOwnerActorID || binding.PredecessorRunID != f.Predecessor.ID ||
		binding.Provider.Version != f.Provider.Version || binding.Provider.Kind != "remote" {
		t.Fatalf("release event = %s err=%v", released.CurrentJSON, err)
	}
	for _, forbidden := range []string{deferredInputResearch, deferredInputNotes, "objects/"} {
		if strings.Contains(released.CurrentJSON, forbidden) {
			t.Fatalf("the release event recorded file content or a path: %s", released.CurrentJSON)
		}
	}
	// Confirming the same files again changes nothing.
	again := f.confirmInputs(waiting.ID, f.inputConsents(view.Fingerprint))
	if again.Code != http.StatusOK || again.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("repeated confirmation=%d: %s", again.Code, again.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'agent_run_start_gate_released'", 1, waiting.ID)

	finished := f.wait(t, waiting.ID)
	if finished.Status != "succeeded" || finished.OutputDeliveryStatus != agentRunOutputSubmitted || f.Calls.Load() != 2 ||
		finished.InputSnapshotJSON != frozenSnapshot {
		t.Fatalf("the follow-on Run did not run once on its frozen row: calls=%d run=%#v", f.Calls.Load(), finished)
	}

	detail := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs/"+waiting.ID, nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"execution_contract_version":7`) ||
		!strings.Contains(detail.Body.String(), `"input_slot":{"source":"predecessor_files","status":"bound"`) ||
		!strings.Contains(detail.Body.String(), view.Files[0].SHA256) || !strings.Contains(detail.Body.String(), view.Files[1].SHA256) {
		t.Fatalf("detail=%d: %s", detail.Code, detail.Body.String())
	}
	list := performRequest(f.Router, http.MethodGet, "/api/v1/tasks/"+f.Follow.ID+"/agent-runs", nil, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"status":"released"`) ||
		strings.Contains(list.Body.String(), `"input_files"`) || strings.Contains(list.Body.String(), `"source_task"`) {
		t.Fatalf("list=%d: %s", list.Code, list.Body.String())
	}
}

// disableAdapter turns the fixture's Adapter off through its own command.
func (f *deferredInputFixture) disableAdapter(t *testing.T) {
	t.Helper()
	var adapter models.AgentAdapter
	if err := f.Store.DB.First(&adapter, "id = ?", f.Actor.AgentAdapterID).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, adapter.Version)})
	if response.Code != http.StatusOK {
		t.Fatalf("disable Adapter=%d: %s", response.Code, response.Body.String())
	}
}

// Accepted files can stop being valid inputs before anyone confirms them.
// The gate then closes, the waiting Run is cancelled and nothing is sent.
func TestAgentRunDeferredInputsCloseWhenTheAcceptedFilesAreNoLongerAvailable(t *testing.T) {
	for _, change := range []string{"reopen", "moved_project", "file_deleted"} {
		t.Run(change, func(t *testing.T) {
			f := newDeferredInputFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("unavailable inputs must not start the follow-on Run")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			waiting := f.confirmWaitingStart(t)
			f.acceptPredecessor(t)
			view := f.inputs(t, waiting.ID)
			if view.Status != agentRunInputSlotAwaitingConfirmation {
				t.Fatalf("inputs = %#v", view)
			}
			switch change {
			case "reopen":
				response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.SourceTask.ID+"/reopen", []byte(`{}`),
					map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version)})
				if response.Code != http.StatusOK {
					t.Fatalf("reopen=%d: %s", response.Code, response.Body.String())
				}
			case "moved_project":
				if err := f.Store.DB.Model(&models.Task{}).Where("id = ?", f.SourceTask.ID).
					Updates(map[string]any{"project_id": nil, "version": f.SourceTask.Version + 1}).Error; err != nil {
					t.Fatal(err)
				}
			case "file_deleted":
				// Nothing protects the files before they are bound.
				response := performRequest(f.Router, http.MethodDelete, "/api/v1/artifacts/"+view.Files[0].ID+"?confirm=true",
					[]byte(`{"reason":"removed before anyone confirmed it"}`),
					map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version), "Idempotency-Key": "deferred-input-" + uuid.NewString()})
				if response.Code != http.StatusOK && response.Code != http.StatusNoContent {
					t.Fatalf("delete accepted file=%d: %s", response.Code, response.Body.String())
				}
			}
			// The files shown a moment ago can no longer be confirmed.
			refused := f.confirmInputs(waiting.ID, f.inputConsents(view.Fingerprint))
			if refused.Code != http.StatusConflict {
				t.Fatalf("confirmation of unavailable inputs=%d: %s", refused.Code, refused.Body.String())
			}
			if _, err := f.Service.reconcileAgentRunStartGates(f.Service.options.Now()); err != nil {
				t.Fatal(err)
			}
			gate := gateOf(t, f.aiAgentRunFixture, waiting.ID)
			if gate == nil || gate.Status != agentRunStartGateClosed || gate.CloseReason == nil ||
				*gate.CloseReason != agentRunStartGateOutputUnavailable || gate.InputSlot.Status != agentRunInputSlotClosed {
				t.Fatalf("gate = %#v", gate)
			}
			var cancelled models.AgentRun
			if err := f.Store.DB.First(&cancelled, "id = ?", waiting.ID).Error; err != nil || cancelled.Status != "cancelled" {
				t.Fatalf("waiting Run = %#v err=%v", cancelled, err)
			}
			closed := f.inputs(t, waiting.ID)
			if closed.Status != agentRunInputSlotClosed || closed.CloseReason == nil || *closed.CloseReason != agentRunStartGateOutputUnavailable ||
				closed.Fingerprint != "" || len(closed.Files) != 0 {
				t.Fatalf("closed inputs = %#v", closed)
			}
			assertAPIError(t, f.confirmInputs(waiting.ID, f.inputConsents(view.Fingerprint)), http.StatusConflict, agentRunInputNotReadyCode)
			if f.Calls.Load() != 1 {
				t.Fatal("the follow-on Run reached the model")
			}
		})
	}
}

// bindWithoutLaunching records the confirmation exactly as the endpoint does,
// but leaves the Run queued so the claim-time checks can be observed.
func (f *deferredInputFixture) bindWithoutLaunching(t *testing.T, runID string) agentRunStartGateBinding {
	t.Helper()
	var binding agentRunStartGateBinding
	err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		state, err := f.Service.readAgentRunStartGateInputs(tx, runID, f.Service.options.Now())
		if err != nil {
			return err
		}
		if state.response.Status != agentRunInputSlotAwaitingConfirmation {
			return fmt.Errorf("inputs are %s", state.response.Status)
		}
		binding = agentRunStartGateBinding{
			PredecessorRunID: state.slot.PredecessorRunID, InputFiles: state.response.Files,
			Provider: state.provider, ConfirmedBy: models.BuiltinOwnerActorID,
		}
		return recordAgentRunWorkflowEvent(tx, agentRunStartGateReleasedEvent, runID, map[string]any{
			"predecessor_run_id": binding.PredecessorRunID, "input_files": binding.InputFiles,
			"provider": binding.Provider, "confirmed_by": binding.ConfirmedBy,
		}, "", f.Service.options.Now().UTC().Format(time.RFC3339Nano))
	})
	if err != nil {
		t.Fatalf("bind inputs: %v", err)
	}
	return binding
}

// Between the confirmation and the launch, the files are protected against
// deletion and re-read byte for byte; a change fails the Run without a call.
func TestAgentRunDeferredInputsAreLockedAndRecheckedAfterConfirmation(t *testing.T) {
	f := newDeferredInputFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("changed inputs must not reach the model")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	waiting := f.confirmWaitingStart(t)
	f.acceptPredecessor(t)
	referenced := func(sourceKind, id string) bool {
		value, err := activeAgentRunReferencesControlledFile(f.Store.DB, sourceKind, id)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	view := f.inputs(t, waiting.ID)
	if referenced(agentexec.FileSourceTaskArtifact, view.Files[0].ID) {
		t.Fatal("an unconfirmed Run already locks the accepted files")
	}
	binding := f.bindWithoutLaunching(t, waiting.ID)
	if !referenced(agentexec.FileSourceTaskArtifact, binding.InputFiles[0].ID) || !referenced(agentexec.FileSourceTaskArtifact, binding.InputFiles[1].ID) {
		t.Fatal("confirmed files are not protected while the Run is queued")
	}
	sourceLocked, err := activeAgentRunReferencesProjectTaskSource(f.Store.DB, f.SourceTask.ID, "")
	if err != nil || !sourceLocked {
		t.Fatalf("the accepted Task is not protected: %v err=%v", sourceLocked, err)
	}
	blocked := performRequest(f.Router, http.MethodDelete, "/api/v1/artifacts/"+binding.InputFiles[0].ID+"?confirm=true",
		[]byte(`{"reason":"attempted removal"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version), "Idempotency-Key": "deferred-locked-" + uuid.NewString()})
	assertAPIError(t, blocked, http.StatusConflict, agentRunFileReferencedByActiveRunCode)

	var artifact models.TaskArtifact
	if err := f.Store.DB.First(&artifact, "id = ?", binding.InputFiles[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	objectPath, err := f.Artifacts.ResolveObject(*artifact.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte(strings.Repeat("x", binding.InputFiles[1].SizeBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Service.executeAgentRun(context.Background(), waiting.ID)
	failed := f.wait(t, waiting.ID)
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != agentRunIdentityChanged || f.Calls.Load() != 1 {
		t.Fatalf("changed bytes were not refused before the model: calls=%d run=%#v", f.Calls.Load(), failed)
	}

	// The files were confirmed for that Run alone: it is never retried as it
	// was, and a failed Run no longer holds them.
	retry := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+waiting.ID+"/retry", []byte(`{}`), nil)
	assertAPIError(t, retry, http.StatusConflict, "AGENT_RUN_NOT_RETRYABLE")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id = ?", 1, f.Follow.ID)
	if referenced(agentexec.FileSourceTaskArtifact, binding.InputFiles[0].ID) {
		t.Fatal("a failed Run still locks the files it was bound to")
	}
}

func TestAgentRunDeferredInputProposalBoundaries(t *testing.T) {
	f := newDeferredInputFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("a refused proposal must not run anything")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	refused := func(name string, tool harness.Tool, args string) {
		t.Helper()
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("%s: the proposal was accepted", name)
		}
	}
	refused("submitted is not accepted", f.Propose, f.startJSON(agentRunStartAfterSubmitted, ""))
	refused("with selected files", f.Propose, f.startJSON(agentRunStartAfterAccepted, `,"input_file_candidate_ids":[]`))
	refused("with rework", f.Propose, f.startJSON(agentRunStartAfterAccepted,
		fmt.Sprintf(`,"rework_submission_id":%q,"rework_artifact_ids":[]`, uuid.NewString())))
	refused("unknown source", f.Propose, strings.Replace(f.startJSON(agentRunStartAfterAccepted, ""), `"predecessor_files"`, `"predecessor_text"`, 1))
	withoutGate := fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d,"start_after_input":"predecessor_files"}}`,
		f.Follow.ID, f.Follow.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion)
	refused("without a start gate", f.Propose, withoutGate)
	// Passing accepted files of another Task on needs its own permission.
	refused("without agent_project_files", f.proposeTool(t, "work", "outputs", "actions", "agent_execution", "agent_files"),
		f.startJSON(agentRunStartAfterAccepted, ""))
	refused("without agent_files", f.proposeTool(t, "work", "outputs", "actions", "agent_execution"),
		f.startJSON(agentRunStartAfterAccepted, ""))

	// The follow-on Task has to be in the predecessor's Project.
	if err := f.Store.DB.Model(&models.Task{}).Where("id = ?", f.Follow.ID).UpdateColumn("project_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	refused("outside the Project", f.Propose, f.startJSON(agentRunStartAfterAccepted, ""))
	if err := f.Store.DB.Model(&models.Task{}).Where("id = ?", f.Follow.ID).UpdateColumn("project_id", f.SourceTask.ProjectID).Error; err != nil {
		t.Fatal(err)
	}

	// Only file outputs can be passed on.
	var original models.AgentRun
	if err := f.Store.DB.First(&original, "id = ?", f.Predecessor.ID).Error; err != nil {
		t.Fatal(err)
	}
	textSnapshot := strings.Replace(original.InputSnapshotJSON,
		`"output_contract":{"type":"files","files":[{"name":"research.md","mime":"text/markdown"},{"name":"notes.md","mime":"text/markdown"}]}`,
		`"output_contract":{"type":"text"}`, 1)
	if textSnapshot == original.InputSnapshotJSON {
		t.Fatalf("fixture snapshot has an unexpected shape: %s", original.InputSnapshotJSON)
	}
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", f.Predecessor.ID).UpdateColumn("input_snapshot_json", textSnapshot).Error; err != nil {
		t.Fatal(err)
	}
	refused("text predecessor", f.Propose, f.startJSON(agentRunStartAfterAccepted, ""))
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", f.Predecessor.ID).UpdateColumn("input_snapshot_json", original.InputSnapshotJSON).Error; err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)

	// A proposal that was fine is refused later if the Adapter can no longer
	// execute, and no new one is accepted for it.
	proposal := proposeTestAction(t, f.Store, f.Propose, f.startJSON(agentRunStartAfterAccepted, ""))
	f.disableAdapter(t)
	if _, err := f.Propose.Execute(context.Background(), []byte(f.startJSON(agentRunStartAfterAccepted, `,"output_kind":"file"`))); err == nil ||
		!strings.Contains(err.Error(), "not healthy, enabled, isolated, and execution ready") {
		t.Fatalf("a proposal for an Adapter that cannot execute: %v", err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	blocked := f.decide(proposal, `,"confirm_agent_execution":true,"confirm_agent_predecessor_input":true`)
	if blocked.Code == http.StatusOK {
		t.Fatalf("the start was confirmed for an Adapter that cannot execute: %s", blocked.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id = ?", 0, f.Follow.ID)
}

// The second confirmation is refused as well when the Adapter stopped being
// executable while the Run waited, and one step is the limit of passing on.
func TestAgentRunDeferredInputsNeedAnExecutableAdapterAndOneStep(t *testing.T) {
	f := newDeferredInputFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("nothing may run in this test")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	waiting := f.confirmWaitingStart(t)

	// A third Task cannot wait for the files of a Run that still waits itself.
	now := f.Service.options.Now().UTC().Format(time.RFC3339Nano)
	third := models.Task{
		ID: uuid.NewString(), ProjectID: f.Follow.ProjectID, Title: "Third step", Description: "Later.",
		CompletionCriteria: "A reviewable result.", Kind: "work", Status: "todo", ReviewPolicy: "manual", Priority: "P2",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := f.Store.DB.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := agentRunDeferredInputsFor(f.Store.DB, third, waiting.ID, agentRunStartAfterAccepted); err == nil {
		t.Fatal("a text-producing waiting Run was accepted as a source of files")
	}
	fileSnapshot := strings.Replace(waiting.InputSnapshotJSON, `"output_contract":{"type":"text"}`,
		`"output_contract":{"type":"file","name":"proposal.md","mime":"text/markdown"}`, 1)
	if fileSnapshot == waiting.InputSnapshotJSON {
		t.Fatalf("fixture snapshot has an unexpected shape: %s", waiting.InputSnapshotJSON)
	}
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", waiting.ID).UpdateColumn("input_snapshot_json", fileSnapshot).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := agentRunDeferredInputsFor(f.Store.DB, third, waiting.ID, agentRunStartAfterAccepted); err == nil ||
		!strings.Contains(err.Error(), "one step at a time") {
		t.Fatalf("a Run waiting for its own inputs was accepted as a source: %v", err)
	}
	if err := f.Store.DB.Model(&models.AgentRun{}).Where("id = ?", waiting.ID).UpdateColumn("input_snapshot_json", waiting.InputSnapshotJSON).Error; err != nil {
		t.Fatal(err)
	}

	f.acceptPredecessor(t)
	view := f.inputs(t, waiting.ID)

	// The external Runner stays closed. The schema refuses any Adapter that is
	// not builtin, and the execution gate refuses one even if a row existed.
	var adapter models.AgentAdapter
	if err := f.Store.DB.First(&adapter, "id = ?", f.Actor.AgentAdapterID).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateAgentRunAdapter(adapter); err != nil {
		t.Fatalf("the fixture Adapter is not executable: %v", err)
	}
	external := adapter
	external.Kind = "external"
	if err := validateAgentRunAdapter(external); err == nil || !strings.Contains(err.Error(), "execution ready") {
		t.Fatalf("an external Adapter passed the execution gate: %v", err)
	}
	if err := f.Store.DB.Model(&models.AgentAdapter{}).Where("id = ?", adapter.ID).UpdateColumn("kind", "external").Error; err == nil {
		t.Fatal("the schema accepted an external Adapter")
	}

	f.disableAdapter(t)
	assertAPIError(t, f.confirmInputs(waiting.ID, f.inputConsents(view.Fingerprint)), http.StatusConflict, agentRunNotExecutable)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'agent_run_start_gate_released'", 0, waiting.ID)
	if f.Calls.Load() != 1 {
		t.Fatal("an Adapter that cannot execute reached the model")
	}
}

// Adding the v7 field must not change one byte of the older contracts, and
// each parser accepts only its own shape.
func TestAgentRunSnapshotContractsKeepTheirBytes(t *testing.T) {
	project := "018f0000-0000-7000-8000-0000000000aa"
	task := agentexec.TaskSnapshot{TaskID: "018f0000-0000-7000-8000-0000000000ab", Title: "T", Status: "todo", Kind: "work",
		ReviewPolicy: "manual", Priority: "P2", ProjectID: &project, Version: 1, CreatedAt: "2026-09-08T12:00:00Z", UpdatedAt: "2026-09-08T12:00:00Z"}
	v5 := agentRunV5InputSnapshot{
		Task: task, ProviderKind: "remote", ModelProtocol: agentexec.ModelProtocolAnthropicMessages,
		MaxOutputTokens: agentexec.AnthropicMaxOutputTokens, Files: []frozenAgentRunFileReference{},
		OutputContract: agentexec.OutputContract{Type: agentexec.ResultTypeText},
	}
	encoded, err := json.Marshal(v5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "deferred_inputs") || !strings.HasSuffix(string(encoded), `"files":[],"output_contract":{"type":"text"}}`) {
		t.Fatalf("v5 bytes changed: %s", encoded)
	}
	if _, err := parseAgentRunV5Snapshot(string(encoded)); err != nil {
		t.Fatalf("v5 no longer parses: %v", err)
	}
	slot := agentRunDeferredInputs{
		Source: agentRunDeferredInputPredecessorFiles, PredecessorRunID: "018f0000-0000-7000-8000-0000000000ac",
		ExpectedFiles: []agentexec.OutputFileContract{{Name: "research.md", MIME: "text/markdown"}},
	}
	v7 := v5
	v7.DeferredInputs = &slot
	withSlot, err := json.Marshal(v7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAgentRunV7Snapshot(string(withSlot)); err != nil {
		t.Fatalf("v7 does not parse its own bytes: %v", err)
	}
	// The declaration is not valid in v5 or v6, and v7 is nothing without it.
	if _, err := parseAgentRunV5Snapshot(string(withSlot)); err == nil {
		t.Fatal("v5 accepted a deferred input declaration")
	}
	if _, err := parseAgentRunV6Snapshot(string(withSlot)); err == nil {
		t.Fatal("v6 accepted a deferred input declaration")
	}
	if _, err := parseAgentRunV7Snapshot(string(encoded)); err == nil {
		t.Fatal("v7 accepted a snapshot without a declaration")
	}
	for name, mutate := range map[string]func(*agentRunV5InputSnapshot){
		"static files": func(s *agentRunV5InputSnapshot) {
			s.Files = []frozenAgentRunFileReference{{ID: "018f0000-0000-7000-8000-0000000000ad", SourceKind: agentexec.FileSourceTaskArtifact,
				Name: "a.md", MIME: "text/markdown", SizeBytes: 1, SHA256: strings.Repeat("a", 64)}}
		},
		"no project": func(s *agentRunV5InputSnapshot) { s.Task.ProjectID = nil },
		"no expected files": func(s *agentRunV5InputSnapshot) {
			empty := slot
			empty.ExpectedFiles = []agentexec.OutputFileContract{}
			s.DeferredInputs = &empty
		},
		"unknown source": func(s *agentRunV5InputSnapshot) {
			other := slot
			other.Source = "predecessor_text"
			s.DeferredInputs = &other
		},
	} {
		invalid := v7
		mutate(&invalid)
		raw, _ := json.Marshal(invalid)
		if _, err := parseAgentRunV7Snapshot(string(raw)); err == nil {
			t.Errorf("v7 accepted a snapshot with %s", name)
		}
	}
}
