package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type continuationEdgeClient struct {
	stream func(context.Context, harness.Request) (harness.Turn, error)
}

func TestAIContinuationEdgeUnboundAutomaticProposalStillBlocksNextGeneration(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	var calls atomic.Int32
	a.harnessClient = continuationEdgeClient{stream: func(_ context.Context, _ harness.Request) (harness.Turn, error) {
		switch calls.Add(1) {
		case 1:
			return harness.Turn{ToolCalls: []harness.ToolCall{{ID: "guide", Name: "workspace_guide", Arguments: json.RawMessage(`{"topic":"tasks_projects"}`)}}}, nil
		case 2:
			return harness.Turn{ToolCalls: []harness.ToolCall{{ID: "proposal", Name: "workspace_propose", Arguments: json.RawMessage(`{"action":"task.create","changes":{"title":"Unbound but still requires human approval"}}`)}}}, nil
		default:
			return harness.Turn{Text: `请确认这个建议。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}, nil
		}
	}}
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	a.aiContinuations.scan()
	waitContinuationWorkers(t, a)
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_work_plan_revisions WHERE session_id=?`, 1, f.Generation.SessionID)
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'`, 1)
	for range 6 {
		a.aiContinuations.scan()
		waitContinuationWorkers(t, a)
	}
	var row models.AIContinuation
	if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil || row.Status != "waiting" || row.Reason != "pending_approval" || row.TurnsStarted != 1 || calls.Load() != 3 {
		t.Fatalf("unbound sibling lost at generation boundary: %+v calls=%d err=%v", row, calls.Load(), err)
	}
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 1)
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM tasks WHERE title='Unbound but still requires human approval'`, 0)
}

func TestAIContinuationEdgeProviderBusyWaitsWithoutConsumingOrChurning(t *testing.T) {
	f, a, model := newContinuationTestFixture(t)
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	if !a.aiGenerations.register("controlled-foreground", f.Provider.ID, "different-session", func() {}) {
		t.Fatal("could not establish separate foreground occupancy")
	}
	defer a.aiGenerations.release("controlled-foreground")
	for range 6 {
		a.aiContinuations.scan()
		waitContinuationWorkers(t, a)
	}
	var row models.AIContinuation
	if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil || row.Status != "waiting" || row.Reason != "provider_busy" || row.TurnsStarted != 0 || row.Version != 2 || model.calls.Load() != 0 {
		t.Fatalf("busy scan charged a turn or repeatedly wrote state: %+v calls=%d err=%v", row, model.calls.Load(), err)
	}
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 0)
	a.aiGenerations.release("controlled-foreground")
	a.aiContinuations.scan()
	waitContinuationWorkers(t, a)
	if model.calls.Load() != 1 {
		t.Fatalf("released provider was not usable: calls=%d", model.calls.Load())
	}
}

func TestAIContinuationEdgeEightAuthorizationsOnlyTwoModelWorkers(t *testing.T) {
	f, a, model := newContinuationTestFixture(t)
	model.entered = make(chan struct{}, 8)
	fixtures := []aiAgentRunFixture{f}
	for index := 1; index < 9; index++ {
		copy := f
		copy.Provider = f.Provider
		copy.Provider.ID, copy.Provider.Name = uuid.NewString(), fmt.Sprintf("Continuation provider %d", index)
		if err := a.db.Create(&copy.Provider).Error; err != nil {
			t.Fatal(err)
		}
		session := models.AISession{ID: uuid.NewString(), Title: fmt.Sprintf("Continuation session %d", index), Persist: true, Version: 1, CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
		if err := a.db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
		copy.Generation = models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: copy.Provider.ID, Status: "streaming", CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
		if err := a.db.Create(&copy.Generation).Error; err != nil {
			t.Fatal(err)
		}
		plan := aiWorkPlanIntent{Title: "Inspect pending facts", Steps: []aiWorkPlanStep{{ID: "inspect", Title: "Read current facts", Kind: "analysis", DependsOn: []string{}, Report: "pending"}}}
		body, _ := json.Marshal(plan)
		if err := a.db.Create(&aiWorkPlanRevision{SessionID: session.ID, Version: 1, GenerationID: copy.Generation.ID, PlanJSON: string(body), CreatedAt: copy.Generation.CreatedAt}).Error; err != nil {
			t.Fatal(err)
		}
		finishAIGeneration(t, f.Store, copy.Generation)
		fixtures = append(fixtures, copy)
	}
	leases := make([]aiContinuationResponse, 0, 8)
	for index := 0; index < 8; index++ {
		leases = append(leases, createContinuationTestLease(t, fixtures[index], continuationTestInput(fixtures[index])))
	}
	body, _ := json.Marshal(continuationTestInput(fixtures[8]))
	tooMany := performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+fixtures[8].Generation.SessionID+"/continuation", body, nil)
	assertAPIError(t, tooMany, 409, "AI_CONTINUATION_CAPACITY")
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuations`, 8)
	a.aiContinuations.scan()
	for range 2 {
		select {
		case <-model.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("two independently authorized workers did not start")
		}
	}
	for range 4 {
		a.aiContinuations.scan()
	}
	a.aiContinuations.mu.Lock()
	activeWorkers := len(a.aiContinuations.workers)
	a.aiContinuations.mu.Unlock()
	if activeWorkers != 2 || model.calls.Load() != 2 {
		t.Fatalf("process worker cap exceeded: workers=%d calls=%d", activeWorkers, model.calls.Load())
	}
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 2)
	for _, lease := range leases {
		response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/continuations/"+lease.ID+"/stop", nil, nil)
		if response.Code != 200 {
			t.Fatalf("stop=%d %s", response.Code, response.Body.String())
		}
	}
	waitContinuationWorkers(t, a)
	a.aiContinuations.scan()
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuations WHERE status='stopped'`, 8)
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 2)
	if model.calls.Load() != 2 {
		t.Fatal("stopped waiting leases were started after worker slots freed")
	}
}

func (m continuationEdgeClient) Stream(ctx context.Context, request harness.Request, _ func(string), _ func(string)) (harness.Turn, error) {
	return m.stream(ctx, request)
}

func TestAIContinuationEdgeStopCancelsModelBeforeMaintenanceWriterFinishes(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	a.harnessClient = continuationEdgeClient{stream: func(ctx context.Context, _ harness.Request) (harness.Turn, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return harness.Turn{}, ctx.Err()
	}}
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	a.aiContinuations.scan()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic model did not start")
	}
	a.maintenance.Lock()
	response := make(chan int, 1)
	go func() {
		result := performRequest(f.Router, http.MethodPost, "/api/v1/ai/continuations/"+lease.ID+"/stop", nil, nil)
		response <- result.Code
	}()
	select {
	case <-cancelled:
	case <-time.After(500 * time.Millisecond):
		t.Error("stop waits for backup writer before cancelling the active model")
	}
	// Storage may wait for the writer, but the billable request must not.
	a.maintenance.Unlock()
	select {
	case code := <-response:
		if code != 200 {
			t.Fatalf("durable stop=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not finish after maintenance released")
	}
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='ai_generation' AND action=? AND aggregate_id=(SELECT generation_id FROM ai_continuation_turns WHERE continuation_id=? LIMIT 1)`, 1, aiGenerationStopRequestedEvent, lease.ID)
	waitContinuationWorkers(t, a)
	var row models.AIContinuation
	if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil || row.Status != "stopped" || row.CurrentGenerationID != nil || row.TurnsStarted != 1 {
		t.Fatalf("stopped worker retained active marker or authority: %+v %v", row, err)
	}
	a.aiContinuations.scan()
	assertDatabaseCount(t, f.Store, `SELECT COUNT(*) FROM ai_continuation_turns`, 1)
}
