package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func seedRunningAgentRun(t *testing.T, fixture aiAgentRunFixture) models.AgentRun {
	t.Helper()
	var assignment models.TaskAssignment
	if err := fixture.Store.DB.First(&assignment,
		"task_id = ? AND actor_id = ? AND role = 'assignee' AND unassigned_at IS NULL",
		fixture.Task.ID, fixture.Actor.ID).Error; err != nil {
		t.Fatal(err)
	}
	var adapter models.AgentAdapter
	if err := fixture.Store.DB.First(&adapter, "id = ?", fixture.Actor.AgentAdapterID).Error; err != nil {
		t.Fatal(err)
	}
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	run := models.AgentRun{
		ID: uuid.NewString(), TaskID: fixture.Task.ID, AssignmentID: assignment.ID,
		ActorID: fixture.Actor.ID, AdapterID: adapter.ID, CreatedByActorID: models.BuiltinOwnerActorID,
		Attempt: 1, Status: "running", ProviderID: fixture.Provider.ID, Model: fixture.Provider.Model,
		TaskVersion: fixture.Task.Version, AssignmentAssignedAt: assignment.AssignedAt,
		ActorVersion: fixture.Actor.Version, AdapterVersion: adapter.Version,
		ProviderVersion: fixture.Provider.Version, ProviderConfigVersion: fixture.Provider.ConfigVersion,
		ExecutionContractVersion: agentRunExecutionContractVersion, InputSnapshotJSON: `{}`,
		OutputDeliveryStatus: agentRunOutputNotReady, StartedAt: &now, CreatedAt: now,
	}
	if err := fixture.Store.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func addAgentRunOwnerReviewer(t *testing.T, fixture aiAgentRunFixture) {
	t.Helper()
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	reviewer := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: fixture.Task.ID, ActorID: models.BuiltinOwnerActorID,
		Role: "reviewer", AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now,
		Reason: "Agent output delivery test",
	}
	if err := fixture.Store.DB.Create(&reviewer).Error; err != nil {
		t.Fatal(err)
	}
}

func seedQueuedFrozenAgentRun(t *testing.T, fixture aiAgentRunFixture) models.AgentRun {
	t.Helper()
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
	})
	if err != nil {
		t.Fatalf("prepare queued Agent Run: %v", err)
	}
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	var run models.AgentRun
	if err := fixture.Store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		run, createErr = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "queued-storage-gate", now,
		)
		return createErr
	}); err != nil {
		t.Fatalf("create queued Agent Run: %v", err)
	}
	return run
}

func TestAgentRunOutputDeliveryUsesSharedReviewChainAndIsIdempotent(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	resultText := "<!doctype html><html><body>review me</body></html>"

	if err := fixture.Service.finalizeAgentRun(run, resultText, "", completedAt); err != nil {
		t.Fatalf("finalize Agent Run: %v", err)
	}
	var delivered models.AgentRun
	if err := fixture.Store.DB.First(&delivered, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if delivered.Status != "succeeded" || delivered.OutputDeliveryStatus != agentRunOutputSubmitted ||
		delivered.OutputDeliveryErrorCode != nil || delivered.SubmissionID == nil || delivered.ArtifactID == nil {
		t.Fatalf("delivered Run = %#v", delivered)
	}
	var submission models.TaskSubmission
	if err := fixture.Store.DB.First(&submission, "id = ?", *delivered.SubmissionID).Error; err != nil {
		t.Fatal(err)
	}
	var artifact models.TaskArtifact
	if err := fixture.Store.DB.First(&artifact, "id = ?", *delivered.ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if submission.SubmittedByActorID != run.ActorID || submission.Status != "pending_review" ||
		artifact.SubmissionID != submission.ID || artifact.ProducedByActorID != run.ActorID ||
		artifact.RecordedByActorID != models.BuiltinSystemActorID || artifact.Name != "agent-run-attempt-1.html" ||
		artifact.ContentText == nil || *artifact.ContentText != resultText {
		t.Fatalf("shared output chain submission=%#v artifact=%#v", submission, artifact)
	}
	var task models.Task
	if err := fixture.Store.DB.First(&task, "id = ?", run.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != "waiting_review" || task.CurrentSubmissionID == nil ||
		*task.CurrentSubmissionID != submission.ID || task.CompletedAt != nil {
		t.Fatalf("Task after Agent output = %#v", task)
	}
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='task_output_submitted' AND submission_id=? AND agent_run_id=?",
		1, submission.ID, run.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_output_submitted' AND aggregate_id=? AND agent_run_id=?",
		1, run.ID, run.ID)

	// Both a repeated finalize and the explicit recovery endpoint are safe
	// terminal reads; neither creates a second review batch.
	if err := fixture.Service.finalizeAgentRun(run, resultText, "", completedAt); err != nil {
		t.Fatalf("repeat finalize: %v", err)
	}
	recovered, err := fixture.Service.recoverAgentRunOutput(run.ID, fixture.Service.options.Now().Add(2*time.Minute))
	if err != nil || recovered.SubmissionID == nil || *recovered.SubmissionID != submission.ID {
		t.Fatalf("terminal recovery = %#v err=%v", recovered, err)
	}
	httpRecovery := performRequest(fixture.Router, http.MethodPost,
		"/api/v1/agent-runs/"+run.ID+"/output-delivery/retry", nil, nil)
	if httpRecovery.Code != http.StatusOK {
		t.Fatalf("terminal HTTP recovery=%d %s", httpRecovery.Code, httpRecovery.Body.String())
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, run.TaskID)
}

func TestAgentRunOutputDeliveryRetainsFrozenIdentityDrift(t *testing.T) {
	for _, test := range []struct {
		name  string
		drift func(*testing.T, aiAgentRunFixture, models.AgentRun)
	}{
		{
			name: "task version",
			drift: func(t *testing.T, fixture aiAgentRunFixture, _ models.AgentRun) {
				if err := fixture.Store.DB.Model(&models.Task{}).Where("id = ?", fixture.Task.ID).
					Update("version", gorm.Expr("version + 1")).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "assignment",
			drift: func(t *testing.T, fixture aiAgentRunFixture, run models.AgentRun) {
				now := fixture.Service.options.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
				if err := fixture.Store.DB.Model(&models.TaskAssignment{}).Where("id = ?", run.AssignmentID).
					Update("unassigned_at", now).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAIAgentRunFixture(t)
			addAgentRunOwnerReviewer(t, fixture)
			run := seedRunningAgentRun(t, fixture)
			test.drift(t, fixture, run)
			if err := fixture.Service.finalizeAgentRun(run, "bounded retained result", "",
				fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
				t.Fatalf("finalize drifted Run: %v", err)
			}
			var retained models.AgentRun
			if err := fixture.Store.DB.First(&retained, "id = ?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if retained.Status != "succeeded" || retained.OutputDeliveryStatus != agentRunOutputRetained ||
				retained.OutputDeliveryErrorCode == nil || *retained.OutputDeliveryErrorCode != agentRunIdentityChanged ||
				retained.SubmissionID != nil || retained.ArtifactID != nil || retained.ResultText == nil {
				t.Fatalf("retained Run = %#v", retained)
			}
			assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
		})
	}
}

func TestAgentRunOutputDeliveryFailureStaysPendingAndRecoversOnce(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	if err := fixture.Store.DB.Exec(`CREATE TRIGGER fail_agent_output_task_event
		BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted'
		BEGIN SELECT RAISE(ABORT,'injected output event failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	var logs bytes.Buffer
	fixture.Service.options.Logger = log.New(&logs, "", 0)
	fixture.Service.finalizeAgentRunObserved(run, "recoverable output", "", completedAt)
	if observed := logs.String(); strings.Contains(observed, "could not be persisted safely") ||
		strings.Contains(observed, "recoverable output") || strings.Contains(observed, "injected output event failure") {
		t.Fatalf("recoverable pending delivery produced unsafe critical log: %q", observed)
	}
	var pending models.AgentRun
	if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending ||
		pending.OutputDeliveryErrorCode == nil || *pending.OutputDeliveryErrorCode != agentRunOutputPendingCode ||
		pending.OutputDeliveryPendingText == nil || *pending.OutputDeliveryPendingText != "recoverable output" ||
		pending.ResultText != nil || pending.CompletedAt != nil {
		t.Fatalf("pending Run = %#v", pending)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, run.TaskID)

	response := performRequest(fixture.Router, http.MethodPost,
		"/api/v1/agent-runs/"+run.ID+"/output-delivery/retry", nil, nil)
	if response.Code != http.StatusServiceUnavailable || responseErrorCode(t, response.Body.Bytes()) != agentRunOutputPendingCode {
		t.Fatalf("pending recovery response=%d %s", response.Code, response.Body.String())
	}
	if cancel := performRequest(fixture.Router, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil); cancel.Code != http.StatusConflict || responseErrorCode(t, cancel.Body.Bytes()) != agentRunOutputPendingCode {
		t.Fatalf("pending cancel=%d %s", cancel.Code, cancel.Body.String())
	}
	if err := fixture.Store.DB.Exec("DROP TRIGGER fail_agent_output_task_event").Error; err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fixture.Service.recoverAgentRunOutput(run.ID, fixture.Service.options.Now().Add(2*time.Minute))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent recovery: %v", err)
		}
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='task_output_submitted' AND agent_run_id=?", 1, run.ID)
	if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "succeeded" || pending.OutputDeliveryStatus != agentRunOutputSubmitted ||
		pending.OutputDeliveryPendingText != nil || pending.SubmissionID == nil || pending.ArtifactID == nil {
		t.Fatalf("recovered Run = %#v", pending)
	}
}

func TestListAgentRunsReturnsMetadataWithoutResultOrRecoveryPayload(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, fixture)
	const stagedBody = "PRIVATE STAGED AGENT BODY"
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := fixture.Service.persistPendingAgentRunOutput(run.ID, stagedBody, completedAt); err != nil {
		t.Fatal(err)
	}
	response := performRequest(fixture.Router, http.MethodGet, "/api/v1/agent-runs", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("global Agent Run list=%d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0]["id"] != run.ID ||
		envelope.Data[0]["output_delivery_status"] != agentRunOutputPending {
		t.Fatalf("global Agent Run summary=%s", response.Body.String())
	}
	for _, forbidden := range []string{
		"result_text", "input_snapshot_json", "output_delivery_pending_text",
		"output_delivery_pending_bytes", "output_delivery_pending_completed_at",
	} {
		if _, exists := envelope.Data[0][forbidden]; exists {
			t.Fatalf("global Agent Run summary exposes %s: %s", forbidden, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), stagedBody) {
		t.Fatalf("global Agent Run summary leaked staged body: %s", response.Body.String())
	}
}

func TestListAgentRunsCountsAndDeliveryFilterDiscoverOldPendingBeyondFirstPage(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	pendingRun := seedRunningAgentRun(t, fixture)
	const stagedBody = "PRIVATE OLD PENDING BODY"
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := fixture.Service.persistPendingAgentRunOutput(pendingRun.ID, stagedBody, completedAt); err != nil {
		t.Fatal(err)
	}

	resultText := "terminal result"
	resultBytes := len(resultText)
	retainedCode := agentRunIdentityChanged
	for index := 0; index < 51; index++ {
		timestamp := fixture.Service.options.Now().Add(time.Duration(index+2) * time.Minute).
			UTC().Format(time.RFC3339Nano)
		run := models.AgentRun{
			ID: uuid.NewString(), TaskID: pendingRun.TaskID, AssignmentID: pendingRun.AssignmentID,
			ActorID: pendingRun.ActorID, AdapterID: pendingRun.AdapterID,
			CreatedByActorID: models.BuiltinOwnerActorID, Attempt: index + 2, Status: "succeeded",
			ProviderID: pendingRun.ProviderID, Model: pendingRun.Model,
			TaskVersion: pendingRun.TaskVersion, AssignmentAssignedAt: pendingRun.AssignmentAssignedAt,
			ActorVersion: pendingRun.ActorVersion, AdapterVersion: pendingRun.AdapterVersion,
			ProviderVersion: pendingRun.ProviderVersion, ProviderConfigVersion: pendingRun.ProviderConfigVersion,
			ExecutionContractVersion: agentRunExecutionContractVersion, InputSnapshotJSON: `{}`,
			ResultText: &resultText, ResultBytes: &resultBytes,
			OutputDeliveryStatus: agentRunOutputRetained, OutputDeliveryErrorCode: &retainedCode,
			StartedAt: &timestamp, CompletedAt: &timestamp, CreatedAt: timestamp,
		}
		if err := fixture.Store.DB.Create(&run).Error; err != nil {
			t.Fatalf("create newer terminal Run %d: %v", index, err)
		}
	}

	type listEnvelope struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Page                 int   `json:"page"`
			PageSize             int   `json:"page_size"`
			Total                int64 `json:"total"`
			ActiveTotal          int64 `json:"active_total"`
			PendingDeliveryTotal int64 `json:"pending_delivery_total"`
			SucceededTotal       int64 `json:"succeeded_total"`
		} `json:"meta"`
	}
	decode := func(path string) listEnvelope {
		t.Helper()
		response := performRequest(fixture.Router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("global Agent Run list %s=%d %s", path, response.Code, response.Body.String())
		}
		var envelope listEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope
	}
	assertCounts := func(meta listEnvelope) {
		t.Helper()
		if meta.Meta.ActiveTotal != 1 || meta.Meta.PendingDeliveryTotal != 1 || meta.Meta.SucceededTotal != 51 {
			t.Fatalf("global Agent Run counts=%#v", meta.Meta)
		}
	}

	firstPage := decode("/api/v1/agent-runs?page=1&page_size=50")
	assertCounts(firstPage)
	if firstPage.Meta.Total != 52 || len(firstPage.Data) != 50 {
		t.Fatalf("unfiltered Agent Run page meta=%#v items=%d", firstPage.Meta, len(firstPage.Data))
	}
	for _, item := range firstPage.Data {
		if item["id"] == pendingRun.ID || item["output_delivery_status"] == agentRunOutputPending {
			t.Fatalf("old pending Run unexpectedly remained on first page: %#v", item)
		}
		for _, forbidden := range []string{
			"result_text", "input_snapshot_json", "output_delivery_pending_text",
			"output_delivery_pending_bytes", "output_delivery_pending_completed_at",
		} {
			if _, exists := item[forbidden]; exists {
				t.Fatalf("filtered-safe summary exposes %s: %#v", forbidden, item)
			}
		}
	}

	pending := decode("/api/v1/agent-runs?output_delivery_status=pending&page=1&page_size=1")
	assertCounts(pending)
	if pending.Meta.Total != 1 || len(pending.Data) != 1 || pending.Data[0]["id"] != pendingRun.ID {
		t.Fatalf("pending delivery filter did not discover old Run: %#v", pending)
	}
	encodedPending, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedPending), stagedBody) {
		t.Fatalf("pending delivery filter leaked staged body: %#v", pending)
	}

	succeeded := decode("/api/v1/agent-runs?status=succeeded&page=1&page_size=1")
	assertCounts(succeeded)
	if succeeded.Meta.Total != 51 || len(succeeded.Data) != 1 || succeeded.Data[0]["status"] != "succeeded" {
		t.Fatalf("status-filtered total/global counters mismatch: %#v", succeeded)
	}

	invalid := performRequest(fixture.Router, http.MethodGet,
		"/api/v1/agent-runs?output_delivery_status=unknown", nil, nil)
	if invalid.Code != http.StatusBadRequest ||
		responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_AGENT_RUN_OUTPUT_DELIVERY_STATUS" {
		t.Fatalf("invalid delivery status=%d %s", invalid.Code, invalid.Body.String())
	}
}

func TestListAgentRunsAttentionInboxExcludesRunsLinkedToLatestPlan(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, fixture.Store, fixture.Tool, aiAgentRunActionJSON(fixture))
	plan := sampleWorkPlan(proposal.ID)
	if got := decodePlanTool(t, planTool(t, fixture), updatePlanArgs(plan, 0)); got == nil || got.Version != 1 {
		t.Fatalf("save latest plan = %#v", got)
	}
	plannedRun := seedRunningAgentRun(t, fixture)

	failedRun := plannedRun
	failedRun.ID = uuid.NewString()
	failedRun.Attempt = 2
	failedRun.Status = "failed"
	failedRun.OutputDeliveryStatus = agentRunOutputNotReady
	failedRun.OutputDeliveryErrorCode = nil
	failureCode := "AGENT_PROCESS_EXIT"
	failedRun.ErrorCode = &failureCode
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	failedRun.CompletedAt = &completedAt
	failedRun.CreatedAt = fixture.Service.options.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
	if err := fixture.Store.DB.Create(&failedRun).Error; err != nil {
		t.Fatal(err)
	}
	retriedRun := failedRun
	retriedRun.ID = uuid.NewString()
	retriedRun.ParentRunID = &failedRun.ID
	retriedRun.Attempt = failedRun.Attempt + 1
	retriedRun.CreatedAt = fixture.Service.options.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)
	if err := fixture.Store.DB.Create(&retriedRun).Error; err != nil {
		t.Fatal(err)
	}

	if err := fixture.Store.DB.Model(&proposal).Updates(map[string]any{
		"status":         "confirmed",
		"result_id":      plannedRun.ID,
		"result_version": 1,
		"decided_at":     fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatal(err)
	}

	decode := func(path string) struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	} {
		t.Helper()
		response := performRequest(fixture.Router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("Agent Run attention list %s=%d %s", path, response.Code, response.Body.String())
		}
		var envelope struct {
			Data []map[string]any `json:"data"`
			Meta struct {
				Total int64 `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"result_text", "input_snapshot_json", "plan_json", "proposal_id"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("attention list leaked %s: %s", forbidden, response.Body.String())
			}
		}
		return envelope
	}

	allAttention := decode("/api/v1/agent-runs?attention=1&page=1&page_size=10")
	if allAttention.Meta.Total != 2 || len(allAttention.Data) != 2 || allAttention.Data[0]["id"] != retriedRun.ID {
		t.Fatalf("attention filter lost actionable Runs: %#v", allAttention)
	}
	unplanned := decode("/api/v1/agent-runs?attention=1&plan_link=unplanned&page=1&page_size=10")
	if unplanned.Meta.Total != 1 || len(unplanned.Data) != 1 || unplanned.Data[0]["id"] != retriedRun.ID {
		t.Fatalf("unplanned inbox did not exclude latest-plan Run: %#v", unplanned)
	}
	failedOnly := decode("/api/v1/agent-runs?attention=1&status=failed&page=1&page_size=1")
	if failedOnly.Meta.Total != 1 || len(failedOnly.Data) != 1 || failedOnly.Data[0]["id"] != retriedRun.ID {
		t.Fatalf("attention retry de-duplication must precede status count and pagination: %#v", failedOnly)
	}
	ledger := decode("/api/v1/agent-runs?page=1&page_size=10")
	if ledger.Meta.Total != 3 {
		t.Fatalf("unfiltered Run ledger lost the historical failed parent: %#v", ledger)
	}

	assertAPIError(t, performRequest(fixture.Router, http.MethodGet,
		"/api/v1/agent-runs?attention=true", nil, nil), 400, "INVALID_AGENT_RUN_ATTENTION_FILTER")
	assertAPIError(t, performRequest(fixture.Router, http.MethodGet,
		"/api/v1/agent-runs?plan_link=planned", nil, nil), 400, "INVALID_AGENT_RUN_PLAN_LINK_FILTER")
}

func TestAgentRunOutputTransientDoublePersistenceFailureRetriesWithoutLosingBody(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	if err := fixture.Store.DB.Exec(`CREATE TRIGGER fail_agent_output_delivery_event
		 BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted'
		 BEGIN SELECT RAISE(ABORT,'injected primary delivery failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	callbackName := "test:fail_agent_output_pending_once"
	var pendingAttempts atomic.Int32
	if err := fixture.Store.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "agent_runs" && pendingAttempts.Add(1) == 1 {
			db.AddError(errors.New("PRIVATE injected pending persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Store.DB.Callback().Update().Remove(callbackName) })
	var logs bytes.Buffer
	fixture.Service.options.Logger = log.New(&logs, "", 0)
	const privateResult = "PRIVATE AGENT RESULT MUST NOT ENTER LOGS"
	fixture.Service.finalizeAgentRunObserved(run, privateResult, "",
		fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))

	observed := logs.String()
	if !strings.Contains(observed, "Agent Run finalization could not be persisted safely") ||
		!strings.Contains(observed, "run_id="+run.ID) {
		t.Fatalf("safe finalization observation = %q", observed)
	}
	for _, forbidden := range []string{privateResult, "injected primary delivery failure", "injected pending persistence failure"} {
		if strings.Contains(observed, forbidden) {
			t.Fatalf("safe finalization observation leaked %q: %q", forbidden, observed)
		}
	}
	if pendingAttempts.Load() < 2 {
		t.Fatalf("pending persistence attempts=%d, want retry", pendingAttempts.Load())
	}
	var pending models.AgentRun
	if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending ||
		pending.OutputDeliveryPendingText == nil || *pending.OutputDeliveryPendingText != privateResult ||
		pending.OutputDeliveryPendingBytes == nil || *pending.OutputDeliveryPendingBytes != len(privateResult) ||
		pending.ResultText != nil {
		t.Fatalf("transient double persistence recovery Run = %#v", pending)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, run.TaskID)
}

func TestAgentRunFinalizationWaitsForMaintenanceWriterThenCommitsAtomically(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	fixture.Service.maintenance.Lock()
	locked := true
	defer func() {
		if locked {
			fixture.Service.maintenance.Unlock()
		}
	}()
	done := make(chan struct{})
	go func() {
		fixture.Service.finalizeAgentRunObserved(run, "maintenance-safe output", "", completedAt)
		close(done)
	}()
	select {
	case <-done:
		fixture.Service.maintenance.Unlock()
		locked = false
		t.Fatal("Agent Run finalization bypassed the maintenance writer")
	case <-time.After(100 * time.Millisecond):
	}
	var before models.AgentRun
	if err := fixture.Store.DB.First(&before, "id = ?", run.ID).Error; err != nil {
		fixture.Service.maintenance.Unlock()
		locked = false
		t.Fatal(err)
	}
	if before.Status != "running" || before.OutputDeliveryStatus != agentRunOutputNotReady {
		fixture.Service.maintenance.Unlock()
		locked = false
		t.Fatalf("Run changed while maintenance writer held: %#v", before)
	}
	fixture.Service.maintenance.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent Run finalization did not resume after maintenance writer released")
	}
	var finalized models.AgentRun
	if err := fixture.Store.DB.First(&finalized, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if finalized.Status != "succeeded" || finalized.OutputDeliveryStatus != agentRunOutputSubmitted ||
		finalized.SubmissionID == nil || finalized.ArtifactID == nil {
		t.Fatalf("finalized Run = %#v", finalized)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, run.TaskID)
}

func TestAgentRunRestoreBarrierCancelsWithoutWaitingAndPreventsFinalizationWrite(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	runContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.Service.agentRunCancelsMu.Lock()
	fixture.Service.agentRunCancels[run.ID] = cancel
	fixture.Service.agentRunCancelsMu.Unlock()
	defer func() {
		fixture.Service.agentRunCancelsMu.Lock()
		delete(fixture.Service.agentRunCancels, run.ID)
		fixture.Service.agentRunCancelsMu.Unlock()
	}()

	fixture.Service.maintenance.Lock()
	locked := true
	defer func() {
		if locked {
			fixture.Service.maintenance.Unlock()
		}
		fixture.Service.restorePending.Store(false)
	}()
	finalizeDone := make(chan struct{})
	go func() {
		fixture.Service.finalizeAgentRunObserved(run, "must not cross restore barrier", "", completedAt)
		close(finalizeDone)
	}()
	time.Sleep(50 * time.Millisecond)
	fixture.Service.restorePending.Store(true)
	cancelDone := make(chan struct{})
	go func() {
		fixture.Service.cancelAIRunsForRestore()
		close(cancelDone)
	}()
	select {
	case <-cancelDone:
	case <-time.After(250 * time.Millisecond):
		fixture.Service.maintenance.Unlock()
		locked = false
		t.Fatal("restore cancellation waited for Agent Run finalization while writer held")
	}
	if runContext.Err() == nil {
		fixture.Service.maintenance.Unlock()
		locked = false
		t.Fatal("restore did not request cancellation of the active Agent Run")
	}
	fixture.Service.maintenance.Unlock()
	locked = false
	select {
	case <-finalizeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent Run finalizer did not fail closed after restore barrier")
	}
	var unchanged models.AgentRun
	if err := fixture.Store.DB.First(&unchanged, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != "running" || unchanged.OutputDeliveryStatus != agentRunOutputNotReady ||
		unchanged.ResultText != nil || unchanged.OutputDeliveryPendingText != nil {
		t.Fatalf("restore barrier allowed Agent Run write: %#v", unchanged)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, run.TaskID)
}

func TestAgentRunRestoreBarrierLeavesQueuedRunUnclaimed(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	run := seedQueuedFrozenAgentRun(t, fixture)
	fixture.Service.restorePending.Store(true)
	t.Cleanup(func() { fixture.Service.restorePending.Store(false) })

	if retry := fixture.Service.executeAgentRun(context.Background(), run.ID); retry {
		t.Fatal("restore-blocked Agent Run claim requested an in-process retry")
	}

	var unchanged models.AgentRun
	if err := fixture.Store.DB.First(&unchanged, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != "queued" || unchanged.StartedAt != nil || unchanged.ErrorCode != nil {
		t.Fatalf("restore barrier allowed Agent Run claim: %#v", unchanged)
	}
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_started' AND aggregate_id=?", 0, run.ID)
}

func TestAgentRunIdentityStorageFailuresDoNotMasqueradeAsDrift(t *testing.T) {
	t.Run("queued claim remains restartable", func(t *testing.T) {
		fixture := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, fixture)
		queued, err := fixture.Service.recoverAgentRunsOnStartup(fixture.Service.options.Now())
		if err != nil || len(queued) != 1 || queued[0] != run.ID {
			t.Fatalf("startup queued Runs=%v err=%v", queued, err)
		}
		var logs bytes.Buffer
		fixture.Service.options.Logger = log.New(&logs, "", 0)
		callbackName := "test:fail_queued_agent_identity_query"
		if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
			if db.Statement.Table == "tasks" {
				db.AddError(errors.New("PRIVATE injected queued identity query failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		fixture.Service.executeAgentRun(context.Background(), run.ID)
		if err := fixture.Store.DB.Callback().Query().Remove(callbackName); err != nil {
			t.Fatal(err)
		}
		var unchanged models.AgentRun
		if err := fixture.Store.DB.First(&unchanged, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if unchanged.Status != "queued" || unchanged.ErrorCode != nil {
			t.Fatalf("transient claim failure changed Run = %#v", unchanged)
		}
		observed := logs.String()
		if !strings.Contains(observed, "Agent Run claim deferred after storage failure run_id="+run.ID) ||
			strings.Contains(observed, "PRIVATE") || strings.Contains(observed, agentRunIdentityChanged) {
			t.Fatalf("unsafe/missing queued claim observation: %q", observed)
		}
		queued, err = fixture.Service.recoverAgentRunsOnStartup(fixture.Service.options.Now().Add(time.Minute))
		if err != nil || len(queued) != 1 || queued[0] != run.ID {
			t.Fatalf("queued Run not restartable after storage recovery: %v err=%v", queued, err)
		}
	})

	t.Run("pre-launch storage failure interrupts without identity error", func(t *testing.T) {
		fixture := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, fixture)
		var logs bytes.Buffer
		fixture.Service.options.Logger = log.New(&logs, "", 0)
		callbackName := "test:fail_prelaunch_agent_identity_query"
		taskQueries := 0
		if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
			if db.Statement.Table == "tasks" {
				taskQueries++
				if taskQueries == 2 {
					db.AddError(errors.New("PRIVATE injected pre-launch identity query failure"))
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		fixture.Service.executeAgentRun(context.Background(), run.ID)
		if err := fixture.Store.DB.Callback().Query().Remove(callbackName); err != nil {
			t.Fatal(err)
		}
		var interrupted models.AgentRun
		if err := fixture.Store.DB.First(&interrupted, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if taskQueries != 2 || interrupted.Status != "interrupted" || interrupted.ErrorCode != nil ||
			interrupted.OutputDeliveryStatus != agentRunOutputNotReady {
			t.Fatalf("pre-launch storage failure Run=%#v task_queries=%d", interrupted, taskQueries)
		}
		observed := logs.String()
		if !strings.Contains(observed, "Agent Run pre-launch check stopped after storage failure run_id="+run.ID) ||
			strings.Contains(observed, "PRIVATE") || strings.Contains(observed, agentRunIdentityChanged) {
			t.Fatalf("unsafe/missing pre-launch observation: %q", observed)
		}
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_interrupted' AND aggregate_id=?", 1, run.ID)
	})

	t.Run("pre-launch read and first interruption persistence failure recover in process", func(t *testing.T) {
		fixture := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, fixture)
		var logs bytes.Buffer
		fixture.Service.options.Logger = log.New(&logs, "", 0)

		queryCallback := "test:fail_prelaunch_agent_identity_query_before_interrupt_retry"
		taskQueries := 0
		if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(queryCallback, func(db *gorm.DB) {
			if db.Statement.Table == "tasks" {
				taskQueries++
				if taskQueries == 2 {
					db.AddError(errors.New("PRIVATE injected pre-launch identity query failure"))
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Store.DB.Callback().Query().Remove(queryCallback) })

		updateCallback := "test:fail_first_prelaunch_interruption_update"
		agentRunUpdates := 0
		if err := fixture.Store.DB.Callback().Update().Before("gorm:update").Register(updateCallback, func(db *gorm.DB) {
			if db.Statement.Table == "agent_runs" {
				agentRunUpdates++
				// The first update claims the Run; fail the first interruption
				// persistence attempt and allow the in-process retry to succeed.
				if agentRunUpdates == 2 {
					db.AddError(errors.New("PRIVATE injected interruption persistence failure"))
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Store.DB.Callback().Update().Remove(updateCallback) })

		fixture.Service.executeAgentRun(context.Background(), run.ID)

		var interrupted models.AgentRun
		if err := fixture.Store.DB.First(&interrupted, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if taskQueries != 2 || agentRunUpdates < 3 || interrupted.Status != "interrupted" ||
			interrupted.ErrorCode != nil || interrupted.OutputDeliveryStatus != agentRunOutputNotReady ||
			interrupted.ResultText != nil {
			t.Fatalf("recovered pre-launch interruption Run=%#v task_queries=%d updates=%d",
				interrupted, taskQueries, agentRunUpdates)
		}
		observed := logs.String()
		if !strings.Contains(observed, "Agent Run pre-launch check stopped after storage failure run_id="+run.ID) ||
			!strings.Contains(observed, "Agent Run pre-launch interruption could not be persisted safely run_id="+run.ID) ||
			strings.Contains(observed, "PRIVATE") || strings.Contains(observed, agentRunIdentityChanged) {
			t.Fatalf("unsafe/missing recovered interruption observation: %q", observed)
		}
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_started' AND aggregate_id=?", 1, run.ID)
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_interrupted' AND aggregate_id=?", 1, run.ID)
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action IN ('agent_run_failed','agent_run_succeeded') AND aggregate_id=?", 0, run.ID)
	})

	t.Run("run cancellation during interruption backoff still reaches one terminal disposition", func(t *testing.T) {
		fixture := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, fixture)
		queryCallback := "test:fail_prelaunch_query_before_cancel_race"
		var taskQueries atomic.Int32
		if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(queryCallback, func(db *gorm.DB) {
			if db.Statement.Table == "tasks" && taskQueries.Add(1) == 2 {
				db.AddError(errors.New("PRIVATE injected pre-launch query failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Store.DB.Callback().Query().Remove(queryCallback) })

		firstInterruptionFailed := make(chan struct{}, 1)
		updateCallback := "test:fail_interruption_before_cancel_race"
		var agentRunUpdates atomic.Int32
		if err := fixture.Store.DB.Callback().Update().Before("gorm:update").Register(updateCallback, func(db *gorm.DB) {
			if db.Statement.Table != "agent_runs" {
				return
			}
			if agentRunUpdates.Add(1) == 2 {
				select {
				case firstInterruptionFailed <- struct{}{}:
				default:
				}
				db.AddError(errors.New("PRIVATE injected first interruption persistence failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fixture.Store.DB.Callback().Update().Remove(updateCallback) })

		fixture.Service.launchAgentRun(run.ID)
		select {
		case <-firstInterruptionFailed:
		case <-time.After(5 * time.Second):
			t.Fatal("Agent Run did not enter interruption persistence backoff")
		}
		cancel, active := fixture.Service.agentRunCancel(run.ID)
		if !active {
			t.Fatal("Agent Run cancellation handle disappeared during interruption backoff")
		}
		cancel() // Mirrors the running-Run side effect of POST /agent-runs/:id/cancel.

		deadline := time.Now().Add(5 * time.Second)
		for {
			if err := fixture.Store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			_, active = fixture.Service.agentRunCancel(run.ID)
			if run.Status != "queued" && run.Status != "running" && !active {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("cancelled interruption retry did not settle: %#v active=%v", run, active)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if run.Status != "interrupted" || run.ErrorCode != nil ||
			run.OutputDeliveryStatus != agentRunOutputNotReady {
			t.Fatalf("cancel/backoff race Run=%#v", run)
		}
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_interrupted' AND aggregate_id=?", 1, run.ID)
		assertDatabaseCount(t, fixture.Store,
			"SELECT COUNT(*) FROM workflow_events WHERE action IN ('agent_run_cancelled','agent_run_failed','agent_run_succeeded') AND aggregate_id=?", 0, run.ID)
	})
}

func TestAgentRunClaimTransientFailureRetriesInCurrentProcess(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	run := seedQueuedFrozenAgentRun(t, fixture)
	var logs bytes.Buffer
	fixture.Service.options.Logger = log.New(&logs, "", 0)
	callbackName := "test:fail_agent_claim_once"
	var taskQueries atomic.Int32
	if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "tasks" && taskQueries.Add(1) == 1 {
			db.AddError(errors.New("PRIVATE transient claim query failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Store.DB.Callback().Query().Remove(callbackName) })
	fixture.Service.launchAgentRun(run.ID)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := fixture.Store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != "queued" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transient claim failure was not retried in the current process")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := fixture.Store.DB.Callback().Query().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	if taskQueries.Load() < 2 || run.ErrorCode != nil {
		t.Fatalf("automatic claim retry Run=%#v task_queries=%d", run, taskQueries.Load())
	}
	if cancel, ok := fixture.Service.agentRunCancel(run.ID); ok {
		cancel()
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if err := fixture.Store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		_, active := fixture.Service.agentRunCancel(run.ID)
		if run.Status != "queued" && run.Status != "running" && !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retried Agent Run did not settle: %#v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	observed := logs.String()
	if !strings.Contains(observed, "Agent Run claim deferred after storage failure run_id="+run.ID) ||
		strings.Contains(observed, "PRIVATE") || strings.Contains(observed, agentRunIdentityChanged) {
		t.Fatalf("unsafe/missing automatic claim retry observation: %q", observed)
	}
}

func TestTaskDeleteBlocksActiveAgentRunsButAllowsTerminalHistoryCascade(t *testing.T) {
	for _, test := range []struct {
		name        string
		prepare     func(*testing.T, aiAgentRunFixture, models.AgentRun)
		messagePart string
	}{
		{name: "queued", messagePart: "Cancel the active Agent Run"},
		{
			name: "running", messagePart: "Cancel the active Agent Run",
			prepare: func(t *testing.T, fixture aiAgentRunFixture, run models.AgentRun) {
				startedAt := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
				if err := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).
					Updates(map[string]any{"status": "running", "started_at": startedAt}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "pending output delivery", messagePart: "Retry the pending Agent Run output delivery",
			prepare: func(t *testing.T, fixture aiAgentRunFixture, run models.AgentRun) {
				startedAt := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
				if err := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).
					Updates(map[string]any{"status": "running", "started_at": startedAt}).Error; err != nil {
					t.Fatal(err)
				}
				if err := fixture.Service.persistPendingAgentRunOutput(run.ID, "staged result", startedAt); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAIAgentRunFixture(t)
			run := seedQueuedFrozenAgentRun(t, fixture)
			if test.prepare != nil {
				test.prepare(t, fixture, run)
			}
			response := performRequest(fixture.Router, http.MethodDelete, "/api/v1/tasks/"+fixture.Task.ID,
				nil, map[string]string{"If-Match": `"1"`})
			if response.Code != http.StatusConflict || responseErrorCode(t, response.Body.Bytes()) != "TASK_HAS_ACTIVE_AGENT_RUN" ||
				!strings.Contains(response.Body.String(), test.messagePart) {
				t.Fatalf("active Agent Run delete=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM tasks WHERE id=?", 1, fixture.Task.ID)
			assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=?", 1, run.ID)
		})
	}

	t.Run("terminal history may cascade", func(t *testing.T) {
		fixture := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, fixture)
		completedAt := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
		if err := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).
			Updates(map[string]any{"status": "running", "started_at": completedAt}).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.Service.finalizeAgentRun(run, "", "TEST_AGENT_RUN_FAILED", completedAt); err != nil {
			t.Fatal(err)
		}
		response := performRequest(fixture.Router, http.MethodDelete, "/api/v1/tasks/"+fixture.Task.ID,
			nil, map[string]string{"If-Match": `"1"`})
		if response.Code != http.StatusNoContent {
			t.Fatalf("terminal Agent Run delete=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=?", 0, run.ID)
	})
}

func TestAgentRunOutputIdentityStorageFailureIsPendingNotRetained(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	run := seedRunningAgentRun(t, fixture)
	callbackName := "test:fail_agent_output_task_query"
	if err := fixture.Store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table == "tasks" {
			db.AddError(errors.New("injected task query failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Store.DB.Callback().Query().Remove(callbackName) })
	if err := fixture.Service.finalizeAgentRun(run, "query failure output", "",
		fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)); err == nil {
		t.Fatal("finalize succeeded despite injected query failure")
	}
	if err := fixture.Store.DB.Callback().Query().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	var pending models.AgentRun
	if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending ||
		pending.OutputDeliveryErrorCode == nil || *pending.OutputDeliveryErrorCode != agentRunOutputPendingCode {
		t.Fatalf("query failure disposition = %#v", pending)
	}
}

func TestAgentRunStartupRecoverySeparatesQueuedRunningAndPending(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, fixture)
	pendingRun := seedRunningAgentRun(t, fixture)
	if err := fixture.Service.persistPendingAgentRunOutput(pendingRun.ID, "startup output",
		fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// A second task avoids the one-active-run-per-task invariant.
	second := fixture
	second.Task.ID = uuid.NewString()
	second.Task.Title = "Interrupted startup task"
	if err := fixture.Store.DB.Create(&second.Task).Error; err != nil {
		t.Fatal(err)
	}
	var assignment models.TaskAssignment
	if err := fixture.Store.DB.First(&assignment, "id = ?", pendingRun.AssignmentID).Error; err != nil {
		t.Fatal(err)
	}
	assignment.ID, assignment.TaskID = uuid.NewString(), second.Task.ID
	if err := fixture.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	running := seedRunningAgentRun(t, second)

	third := fixture
	third.Task.ID = uuid.NewString()
	third.Task.Title = "Queued startup task"
	if err := fixture.Store.DB.Create(&third.Task).Error; err != nil {
		t.Fatal(err)
	}
	assignment.ID, assignment.TaskID = uuid.NewString(), third.Task.ID
	if err := fixture.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	queued := seedRunningAgentRun(t, third)
	if err := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", queued.ID).
		Updates(map[string]any{"status": "queued", "started_at": nil}).Error; err != nil {
		t.Fatal(err)
	}

	queuedIDs, err := fixture.Service.recoverAgentRunsOnStartup(fixture.Service.options.Now().Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(queuedIDs) != 1 || queuedIDs[0] != queued.ID {
		t.Fatalf("queued recovery IDs = %v", queuedIDs)
	}
	var rows []models.AgentRun
	if err := fixture.Store.DB.Find(&rows, "id IN ?", []string{pendingRun.ID, running.ID, queued.ID}).Error; err != nil {
		t.Fatal(err)
	}
	statuses := map[string]models.AgentRun{}
	for _, row := range rows {
		statuses[row.ID] = row
	}
	if statuses[pendingRun.ID].Status != "succeeded" || statuses[pendingRun.ID].OutputDeliveryStatus != agentRunOutputSubmitted ||
		statuses[running.ID].Status != "interrupted" || statuses[queued.ID].Status != "queued" {
		t.Fatalf("startup recovery states = %#v", statuses)
	}
	queuedIDs, err = fixture.Service.recoverAgentRunsOnStartup(fixture.Service.options.Now().Add(3 * time.Minute))
	if err != nil || len(queuedIDs) != 1 || queuedIDs[0] != queued.ID {
		t.Fatalf("repeated startup recovery IDs=%v err=%v", queuedIDs, err)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, pendingRun.TaskID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_interrupted' AND aggregate_id=?", 1, running.ID)
	for _, runID := range queuedIDs {
		fixture.Service.launchAgentRun(runID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := fixture.Store.DB.First(&queued, "id = ?", queued.ID).Error; err != nil {
			t.Fatal(err)
		}
		if queued.Status != "queued" && queued.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("committed queued Run was not relaunched")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
