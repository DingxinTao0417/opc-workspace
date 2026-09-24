package api

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func seedAgentRunQueueRow(t *testing.T, f aiAgentRunFixture, index int, status, delivery string, createdAt time.Time) models.AgentRun {
	t.Helper()
	createdAtText := createdAt.UTC().Format(time.RFC3339Nano)
	task := f.Task
	task.ID = uuid.NewString()
	task.Title = fmt.Sprintf("Queue fixture %d", index)
	task.CreatedAt = createdAtText
	task.UpdatedAt = createdAtText
	if err := f.Store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: task.ID, ActorID: f.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: createdAtText, Reason: "queue fixture",
	}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}

	run := models.AgentRun{
		ID: uuid.NewString(), TaskID: task.ID, AssignmentID: assignment.ID,
		ActorID: f.Actor.ID, AdapterID: *f.Actor.AgentAdapterID,
		CreatedByActorID: models.BuiltinOwnerActorID, Attempt: 1,
		Status: status, ProviderID: f.Provider.ID, Model: f.Provider.Model,
		TaskVersion: task.Version, AssignmentAssignedAt: createdAtText,
		ActorVersion: f.Actor.Version, AdapterVersion: 1,
		ProviderVersion: f.Provider.Version, ProviderConfigVersion: f.Provider.ConfigVersion,
		ExecutionContractVersion: agentRunExecutionContractVersion,
		InputSnapshotJSON:        `{}`, OutputDeliveryStatus: delivery, CreatedAt: createdAtText,
	}
	if status == "running" {
		startedAt := createdAt.Add(-time.Second).UTC().Format(time.RFC3339Nano)
		run.StartedAt = &startedAt
	}
	if delivery == agentRunOutputPending {
		pendingText := "safe staged result"
		pendingBytes := len(pendingText)
		completedAt := createdAtText
		code := agentRunOutputPendingCode
		run.OutputDeliveryErrorCode = &code
		run.OutputDeliveryPendingText = &pendingText
		run.OutputDeliveryPendingBytes = &pendingBytes
		run.OutputDeliveryPendingCompletedAt = &completedAt
	}
	if err := f.Store.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAgentRunWorkerReservationIsGloballyBounded(t *testing.T) {
	f := newAIAgentRunFixture(t)
	const contenders = 96
	start := make(chan struct{})
	release := make(chan struct{})
	results := make(chan bool, contenders)
	var workers sync.WaitGroup
	for index := 0; index < contenders; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			runID := uuid.NewString()
			_, reserved := f.Service.reserveAgentRunWorker(runID)
			results <- reserved
			if reserved {
				<-release
				f.Service.finishAgentRunWorker(runID)
			}
		}()
	}
	close(start)
	reservedCount := 0
	for index := 0; index < contenders; index++ {
		if <-results {
			reservedCount++
		}
	}
	if reservedCount != agentRunMaxConcurrentWorkers {
		t.Fatalf("reserved workers=%d want=%d", reservedCount, agentRunMaxConcurrentWorkers)
	}
	f.Service.agentRunCancelsMu.Lock()
	activeCount := len(f.Service.agentRunCancels)
	f.Service.agentRunCancelsMu.Unlock()
	if activeCount != agentRunMaxConcurrentWorkers {
		t.Fatalf("active worker slots=%d want=%d", activeCount, agentRunMaxConcurrentWorkers)
	}
	close(release)
	workers.Wait()
	f.Service.agentRunWorkers.Wait()
}

func TestAgentRunQueueSelectsOldestQueuedRuns(t *testing.T) {
	f := newAIAgentRunFixture(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	later := seedAgentRunQueueRow(t, f, 1, "queued", agentRunOutputNotReady, base.Add(2*time.Minute))
	oldest := seedAgentRunQueueRow(t, f, 2, "queued", agentRunOutputNotReady, base)
	middle := seedAgentRunQueueRow(t, f, 3, "queued", agentRunOutputNotReady, base.Add(time.Minute))
	ids, err := f.Service.oldestQueuedAgentRunIDs(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != oldest.ID || ids[1] != middle.ID {
		t.Fatalf("oldest queued ids=%v want=[%s %s] (later=%s)", ids, oldest.ID, middle.ID, later.ID)
	}
}

func TestAgentRunQueueAdmissionRejectsProposalWithoutSideEffects(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for index := 0; index < agentRunMaxAccepted; index++ {
		seedAgentRunQueueRow(t, f, index, "queued", agentRunOutputNotReady, base.Add(time.Duration(index)*time.Second))
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, proposal.Fingerprint))
	path := "/api/v1/ai/actions/" + proposal.ID + "/decision"
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, body, nil), http.StatusTooManyRequests, agentRunQueueFullCode)
	assertDatabaseCount(t, f.Store,
		"SELECT COUNT(*) FROM agent_runs WHERE status IN ('queued','running') AND output_delivery_status='not_ready'",
		agentRunMaxAccepted)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, proposal.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
}

func TestAgentRunQueueAdmissionRollsBackNativeAutoAssignment(t *testing.T) {
	f := newAIAgentRunFixture(t)
	unassignedTask := f.Task
	unassignedTask.ID = uuid.NewString()
	unassignedTask.Title = "Queue-full auto-assignment fixture"
	if err := f.Store.DB.Create(&unassignedTask).Error; err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for index := 0; index < agentRunMaxAccepted; index++ {
		seedAgentRunQueueRow(t, f, index, "queued", agentRunOutputNotReady, base.Add(time.Duration(index)*time.Second))
	}
	body := []byte(fmt.Sprintf(
		`{"provider_id":%q,"auto_assign":{"actor_id":%q,"expected_task_version":%d}}`,
		f.Provider.ID, f.Actor.ID, unassignedTask.Version,
	))
	path := "/api/v1/tasks/" + unassignedTask.ID + "/agent-runs"
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, body, nil), http.StatusTooManyRequests, agentRunQueueFullCode)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, unassignedTask.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id=?", 0, unassignedTask.ID)
}

func TestAgentRunQueueAdmissionDoesNotCountPendingOutputDelivery(t *testing.T) {
	f := newAIAgentRunFixture(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for index := 0; index < agentRunMaxAccepted-1; index++ {
		seedAgentRunQueueRow(t, f, index, "queued", agentRunOutputNotReady, base.Add(time.Duration(index)*time.Second))
	}
	seedAgentRunQueueRow(t, f, agentRunMaxAccepted, "running", agentRunOutputPending, base.Add(time.Hour))

	err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareAgentRun(tx, prepareAgentRunInput{
			TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Service.artifactStore,
		})
		if err != nil {
			return err
		}
		_, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "queue-test", base.Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		t.Fatalf("pending output delivery incorrectly consumed execution capacity: %v", err)
	}
	assertDatabaseCount(t, f.Store,
		"SELECT COUNT(*) FROM agent_runs WHERE status IN ('queued','running') AND output_delivery_status='not_ready'",
		agentRunMaxAccepted)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE output_delivery_status='pending'", 1)
}
