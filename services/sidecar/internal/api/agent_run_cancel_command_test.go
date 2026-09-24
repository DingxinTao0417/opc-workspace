package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAgentRunCancellationSignalsOnlyAfterCommit(t *testing.T) {
	f := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Service.agentRunCancels[run.ID] = cancel
	router := gin.New()
	router.POST("/agent-runs/:id/cancel", f.Service.cancelAgentRun)
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_cancel_intent
		BEFORE INSERT ON workflow_events WHEN NEW.action='agent_run_cancel_requested'
		BEGIN SELECT RAISE(ABORT,'injected cancel event failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/agent-runs/"+run.ID+"/cancel", nil, nil)
	if response.Code != http.StatusInternalServerError || ctx.Err() != nil {
		t.Fatalf("rollback signalled worker: %d %v", response.Code, ctx.Err())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested' AND aggregate_id=?", 0, run.ID)
	if err := f.Store.DB.Exec("DROP TRIGGER fail_cancel_intent").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/agent-runs/"+run.ID+"/cancel", nil, nil)
	if response.Code != http.StatusOK || ctx.Err() != context.Canceled {
		t.Fatalf("committed cancellation not signalled: %d %v", response.Code, ctx.Err())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested' AND aggregate_id=?", 1, run.ID)
}

func TestAgentRunDurableCancellationWinsLateOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "pending_fallback", "interrupted", "restart"} {
		t.Run(outcome, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			run := seedRunningAgentRun(t, f)
			now := f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)
			for i := 0; i < 2; i++ {
				response := performRequest(f.Router, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("cancel=%d %s", response.Code, response.Body.String())
				}
			}
			var current models.AgentRun
			if err := f.Store.DB.First(&current, "id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != "running" {
				t.Fatal("cancel request released active Run before executor settled")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested' AND aggregate_id=?", 1, run.ID)
			var err error
			switch outcome {
			case "success":
				err = f.Service.finalizeAgentRun(run, "late result must not be submitted", "", now)
			case "failure":
				err = f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", now)
			case "pending_fallback":
				err = f.Service.persistPendingAgentRunOutput(run.ID, "late staging must not be kept", now)
			case "interrupted":
				err = f.Service.interruptClaimedAgentRun(run.ID, now)
			case "restart":
				var queued []string
				queued, err = f.Service.recoverAgentRunsOnStartup(f.Service.options.Now().Add(time.Minute))
				if len(queued) != 0 {
					t.Fatal("cancelled run relaunched")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			// Late and repeated finalization is idempotent even with the stale
			// worker snapshot captured before the cancellation request.
			if err := f.Service.finalizeAgentRun(run, "another late result", "", now); err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.First(&current, "id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != "cancelled" || current.ErrorCode != nil || current.ResultText != nil || current.OutputDeliveryPendingText != nil || current.CompletedAt == nil {
				t.Fatalf("cancelled=%#v", current)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancelled' AND aggregate_id=?", 1, run.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, run.TaskID)
		})
	}
}

func TestAgentRunCancellationTransactionRollbackAndPendingGuard(t *testing.T) {
	f := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, f)
	now := run.CreatedAt
	injected := errors.New("rollback approval")
	err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		if err := requestAgentRunCancellation(tx, run.ID, "", now); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested' AND aggregate_id=?", 0, run.ID)
	if err := f.Service.persistPendingAgentRunOutput(run.ID, "already durable output", now); err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil)
	if response.Code != http.StatusConflict || responseErrorCode(t, response.Body.Bytes()) != agentRunOutputPendingCode {
		t.Fatalf("pending cancel=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested' AND aggregate_id=?", 0, run.ID)
	var current models.AgentRun
	if err := f.Store.DB.First(&current, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.OutputDeliveryPendingText == nil || *current.OutputDeliveryPendingText != "already durable output" {
		t.Fatal("pending result lost")
	}
}
