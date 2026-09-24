package api

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiRecoveryArgs(f aiAgentRunFixture, run models.AgentRun) string {
	return fmt.Sprintf(`{"action":"agent_run.recover_output","task_id":%q,"agent_run_id":%q,"expected_version":%d,"changes":{}}`, f.Task.ID, run.ID, f.Task.Version)
}

func seedAIPendingOutput(t *testing.T, kind string) (agentRunFileTestFixture, models.AgentRun) {
	t.Helper()
	f := newAgentRunFileTestFixture(t)
	addAgentRunOwnerReviewer(t, f.aiAgentRunFixture)
	input := prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Artifacts}
	if kind == "file" {
		input.OutputContract = &agentRunOutputContractRequest{Type: kind, Name: "report.md", MIME: "text/markdown"}
	}
	p, err := prepareAgentRun(f.Store.DB, input)
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, f, p)
	if err = f.Service.persistPendingAgentRunOutput(run.ID, "PRIVATE RECOVERY BODY", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return f, run
}

func TestAIAgentRunRecoveryApprovalAtomicAndIdempotent(t *testing.T) {
	for _, kind := range []string{agentexec.ResultTypeText, agentexec.ResultTypeFile} {
		t.Run(kind, func(t *testing.T) {
			f, run := seedAIPendingOutput(t, kind)
			row := proposeTestAction(t, f.Store, f.Tool, aiRecoveryArgs(f.aiAgentRunFixture, run))
			if strings.Contains(row.PreviewJSON, "PRIVATE RECOVERY BODY") || !strings.Contains(row.PreviewJSON, `"output_kind":"`+kind+`"`) {
				t.Fatalf("unsafe recovery preview: %s", row.PreviewJSON)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			without := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
			assertAPIError(t, performRequest(router, "POST", path, without, nil), 422, "AGENT_OUTPUT_RECOVERY_CONFIRMATION_REQUIRED")
			wrong := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
			assertAPIError(t, performRequest(router, "POST", path, wrong, nil), 422, "AI_ACTION_DECISION_INVALID")
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_output_recovery":true}`, row.Fingerprint))
			objects, err := os.ReadDir(f.Artifacts.objectsDir)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.Store.DB.Exec(`CREATE TRIGGER fail_recovery_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'injected approval rollback'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if res := performRequest(router, "POST", path, body, nil); res.Code != 500 {
				t.Fatalf("rollback=%d %s", res.Code, res.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND output_delivery_status='pending' AND output_delivery_pending_text=?", 1, run.ID, "PRIVATE RECOVERY BODY")
			after, err := os.ReadDir(f.Artifacts.objectsDir)
			if err != nil || len(after) != len(objects) {
				t.Fatalf("orphan recovery file: %v", err)
			}
			if err = f.Store.DB.Exec("DROP TRIGGER fail_recovery_approval").Error; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				res := performRequest(router, "POST", path, body, nil)
				if res.Code != 200 || !strings.Contains(res.Body.String(), `"output_delivery_status":"submitted"`) || strings.Contains(res.Body.String(), "PRIVATE RECOVERY BODY") {
					t.Fatalf("recovery=%d %s", res.Code, res.Body.String())
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			if len(f.Service.agentRunCancels) != 0 {
				t.Fatal("recovery launched an executor")
			}
			receipts, err := f.Service.aiActionReceipts(context.Background(), f.Generation.SessionID)
			if err != nil || strings.Contains(receipts, "PRIVATE RECOVERY BODY") || strings.Contains(receipts, "output_sha256") || !strings.Contains(receipts, `"output_delivery_status":"submitted"`) {
				t.Fatalf("receipts=%s err=%v", receipts, err)
			}
		})
	}
}

func TestAIAgentRunRecoveryRejectsStaleCard(t *testing.T) {
	f, run := seedAIPendingOutput(t, "text")
	row := proposeTestAction(t, f.Store, f.Tool, aiRecoveryArgs(f.aiAgentRunFixture, run))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.recoverAgentRunOutput(run.ID, f.Service.options.Now()); err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_output_recovery":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "VERSION_CONFLICT")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}

func TestAIAgentRunRecoveryRequiresScopeAndCanRetainChangedIdentity(t *testing.T) {
	f, run := seedAIPendingOutput(t, "text")
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions"}}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("proposal tool missing")
	}
	if _, err = tool.Execute(context.Background(), []byte(aiRecoveryArgs(f.aiAgentRunFixture, run))); err == nil {
		t.Fatal("recovery lacked execution scope")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	if err = f.Store.DB.Model(&models.Task{}).Where("id=?", f.Task.ID).Update("version", f.Task.Version+1).Error; err != nil {
		t.Fatal(err)
	}
	f.Task.Version++
	row := proposeTestAction(t, f.Store, f.Tool, aiRecoveryArgs(f.aiAgentRunFixture, run))
	if err = f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_output_recovery":true}`, row.Fingerprint))
	res := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"output_delivery_status":"retained"`) {
		t.Fatalf("retained=%d %s", res.Code, res.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo'", 1, f.Task.ID)
}

func TestAIAgentRunRecoveryApprovalCommitFailureKeepsPendingFile(t *testing.T) {
	f, run := seedAIPendingOutput(t, "file")
	row := proposeTestAction(t, f.Store, f.Tool, aiRecoveryArgs(f.aiAgentRunFixture, run))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Exec(`CREATE TABLE recovery_commit_guard (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id) DEFERRABLE INITIALLY DEFERRED)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_recovery_commit AFTER INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN INSERT INTO recovery_commit_guard(id,task_id) VALUES(NEW.id,'missing-task'); END`).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_output_recovery":true}`, row.Fingerprint))
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	if res := performRequest(f.Router, "POST", path, body, nil); res.Code != 500 {
		t.Fatalf("commit failure=%d %s", res.Code, res.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND output_delivery_status='pending'", 1, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 0)
	objects, err := os.ReadDir(f.Artifacts.objectsDir)
	if err != nil || len(objects) != 0 {
		t.Fatalf("orphan output files=%d err=%v", len(objects), err)
	}
	if err = f.Store.DB.Exec("DROP TRIGGER fail_recovery_commit").Error; err != nil {
		t.Fatal(err)
	}
	if res := performRequest(f.Router, "POST", path, body, nil); res.Code != 200 {
		t.Fatalf("connection/recovery after failure=%d %s", res.Code, res.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts", 1)
}
