package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiRetryArgs(f aiAgentRunFixture, run models.AgentRun) string {
	return fmt.Sprintf(`{"action":"agent_run.retry","task_id":%q,"agent_run_id":%q,"expected_version":%d,"changes":{}}`, f.Task.ID, run.ID, f.Task.Version)
}

func seedFailedFrozenAIRun(t *testing.T, f aiAgentRunFixture) models.AgentRun {
	t.Helper()
	run := seedQueuedFrozenAgentRun(t, f)
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "failed", "started_at": run.CreatedAt, "completed_at": run.CreatedAt, "error_code": "AGENT_EXECUTION_FAILED"}).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAIAgentRunRetryApprovalAtomicAndIdempotent(t *testing.T) {
	f := newAIAgentRunFixture(t)
	previous := seedFailedFrozenAIRun(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = ctx // No process or real model in deterministic gates.
	router := gin.New()
	router.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
	row := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f, previous))
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	without := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, without, nil), 422, "AGENT_EXECUTION_CONFIRMATION_REQUIRED")
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_retry_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'injected rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if res := performRequest(router, "POST", path, body, nil); res.Code != 500 {
		t.Fatalf("rollback: %d %s", res.Code, res.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	if err := f.Store.DB.Exec("DROP TRIGGER fail_retry_approval").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if res := performRequest(router, "POST", path, body, nil); res.Code != 200 {
			t.Fatalf("confirm: %d %s", res.Code, res.Body.String())
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	var next models.AgentRun
	if err := f.Store.DB.First(&next, "parent_run_id=?", previous.ID).Error; err != nil {
		t.Fatal(err)
	}
	if next.Attempt != previous.Attempt+1 || next.InputSnapshotJSON != previous.InputSnapshotJSON || next.ProviderID != previous.ProviderID || next.Model != previous.Model || next.Status != "queued" {
		t.Fatalf("retry changed execution: %#v", next)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIAgentRunRetryRejectsDriftAndActiveRuns(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		run := seedQueuedFrozenAgentRun(t, f)
		if _, err := f.Tool.Execute(context.Background(), []byte(aiRetryArgs(f, run))); err == nil {
			t.Fatal("active run retry accepted")
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	})
	t.Run("frozen task", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		run := seedFailedFrozenAIRun(t, f)
		if err := f.Store.DB.Model(&f.Task).Update("version", f.Task.Version+1).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.Tool.Execute(context.Background(), []byte(aiRetryArgs(f, run))); err == nil {
			t.Fatal("changed task retry accepted")
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	})
	t.Run("approval drift", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		run := seedFailedFrozenAIRun(t, f)
		row := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f, run))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.Model(&f.Provider).Update("version", f.Provider.Version+1).Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AGENT_RUN_IDENTITY_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	})
}

func TestAIAgentRunRetryFilesRequireFreshScopeAndConsent(t *testing.T) {
	f := newAIAgentRunFixture(t)
	artifact := seedAIAgentRunTaskFile(t, f, "retry-input.md", "original controlled bytes")
	prepared, err := prepareAgentRun(f.Store.DB, prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Service.artifactStore, InputFiles: []agentRunInputFileRequest{{SourceKind: "task_artifact", ID: artifact.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	var original models.AgentRun
	if err = f.Store.DB.Transaction(func(tx *gorm.DB) error {
		var e error
		original, e = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "file-retry", f.Service.options.Now().Format(time.RFC3339Nano))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = f.Store.DB.Model(&original).Updates(map[string]any{"status": "failed", "started_at": original.CreatedAt, "completed_at": original.CreatedAt, "error_code": "AGENT_EXECUTION_FAILED"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.Tool.Execute(context.Background(), []byte(aiRetryArgs(f, original))); err == nil {
		t.Fatal("file retry inherited old file scope")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	_, tool := aiAgentFileTools(t, f)
	row := proposeTestAction(t, f.Store, tool, aiRetryArgs(f, original))
	if err = f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	without := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(f.Router, "POST", path, without, nil), 422, "AGENT_FILE_CONFIRMATION_REQUIRED")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = ctx
	router := gin.New()
	router.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_files":true}`, row.Fingerprint))
	if res := performRequest(router, "POST", path, body, nil); res.Code != 200 {
		t.Fatalf("file retry: %d %s", res.Code, res.Body.String())
	}
	var next models.AgentRun
	if err = f.Store.DB.First(&next, "parent_run_id=?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if next.InputSnapshotJSON != original.InputSnapshotJSON || next.ExecutionContractVersion != 2 {
		t.Fatal("file snapshot replaced")
	}
}
