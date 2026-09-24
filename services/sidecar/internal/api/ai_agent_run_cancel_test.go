package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiCancelArgs(f aiAgentRunFixture, run models.AgentRun) string {
	return fmt.Sprintf(`{"action":"agent_run.cancel","task_id":%q,"agent_run_id":%q,"expected_version":%d,"changes":{}}`, run.TaskID, run.ID, f.Task.Version)
}

func TestAIAgentRunCancelRejectsForgedFieldsAndWrongTask(t *testing.T) {
	f := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, f)
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["confirm_agent_cancel"] = true },
		func(m map[string]any) { m["changes"] = map[string]any{"provider_id": f.Provider.ID} },
		func(m map[string]any) { m["expected_version"] = 0 },
		func(m map[string]any) { delete(m, "agent_run_id") },
		func(m map[string]any) { m["action"] = "task.start" },
	} {
		var raw map[string]any
		if err := json.Unmarshal([]byte(aiCancelArgs(f, run)), &raw); err != nil {
			t.Fatal(err)
		}
		mutate(raw)
		encoded, _ := json.Marshal(raw)
		if _, err := parseAIWorkspaceAction(encoded); err == nil {
			t.Fatalf("accepted forged action %s", encoded)
		}
	}
	wrongTask := f.Task
	wrongTask.ID = uuid.NewString()
	if err := f.Store.DB.Create(&wrongTask).Error; err != nil {
		t.Fatal(err)
	}
	run.TaskID = wrongTask.ID
	if _, err := f.Tool.Execute(context.Background(), []byte(aiCancelArgs(f, run))); err == nil {
		t.Fatal("cancel accepted Run owned by another Task")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIAgentRunCancelApproval(t *testing.T) {
	for _, queued := range []bool{true, false} {
		t.Run(fmt.Sprintf("queued=%v", queued), func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			workerContext, workerCancel := context.WithCancel(context.Background())
			defer workerCancel()
			var run models.AgentRun
			if queued {
				run = seedQueuedFrozenAgentRun(t, f)
			} else {
				run = seedRunningAgentRun(t, f)
			}
			f.Service.agentRunCancels[run.ID] = workerCancel
			// Exercise the decision handler with the same runtime cancellation
			// registry as the worker; do not launch an executor or real model.
			decisionRouter := gin.New()
			decisionRouter.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
			row := proposeTestAction(t, f.Store, f.Tool, aiCancelArgs(f, run))
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 0)
			if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			assertAPIError(t, performRequest(f.Router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 422, "AGENT_CANCEL_CONFIRMATION_REQUIRED")
			assertAPIError(t, performRequest(f.Router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_agent_cancel":true}`, row.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_cancel":true}`, row.Fingerprint))
			if err := f.Store.DB.Exec(`CREATE TRIGGER fail_cancel_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'injected rollback'); END`).Error; err != nil {
				t.Fatal(err)
			}
			response := performRequest(decisionRouter, "POST", path, body, nil)
			if response.Code != 500 {
				t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
			}
			if workerContext.Err() != nil {
				t.Fatal("rolled back confirmation cancelled worker")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			if err := f.Store.DB.Exec("DROP TRIGGER fail_cancel_approval").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				response = performRequest(decisionRouter, "POST", path, body, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
				}
				if workerContext.Err() != context.Canceled {
					t.Fatal("committed confirmation did not signal worker")
				}
				var envelope struct {
					Data aiActionResponse `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				want := "running"
				if queued {
					want = "cancelled"
				}
				if envelope.Data.AgentRunResult == nil || envelope.Data.AgentRunResult.Status != want || envelope.Data.Route != agentRunRoute(run.TaskID, run.ID) {
					t.Fatalf("receipt=%s", response.Body.String())
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND version=?", 1, run.TaskID, f.Task.Version)
			if !queued {
				if err := f.Service.finalizeAgentRun(run, "late success", "", run.CreatedAt); err != nil {
					t.Fatal(err)
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='cancelled'", 1, run.ID)
		})
	}
}

func TestAIAgentRunCancelScopesAndStaleFacts(t *testing.T) {
	for _, change := range []string{"pending", "task", "state", "already_requested"} {
		t.Run(change, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			run := seedRunningAgentRun(t, f)
			weak, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions"}}, f.Generation.ID)
			if err != nil {
				t.Fatal(err)
			}
			tool, _ := weak.Get("workspace_propose")
			if schemaActionEnum(t, tool)["agent_run.cancel"] {
				t.Fatal("cancel exposed without execution consent")
			}
			if _, err := tool.Execute(context.Background(), []byte(aiCancelArgs(f, run))); err == nil {
				t.Fatal("cancel bypassed execution consent")
			}
			row := proposeTestAction(t, f.Store, f.Tool, aiCancelArgs(f, run))
			if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			switch change {
			case "pending":
				err = f.Service.persistPendingAgentRunOutput(run.ID, "keep output", run.CreatedAt)
			case "task":
				err = f.Store.DB.Model(&f.Task).Updates(map[string]any{"title": "changed task", "version": f.Task.Version + 1}).Error
			case "state":
				err = f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", run.CreatedAt)
			case "already_requested":
				response := performRequest(f.Router, "POST", "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil)
				if response.Code != 200 {
					t.Fatal(response.Body.String())
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_cancel":true}`, row.Fingerprint)), nil)
			if response.Code != 409 {
				t.Fatalf("stale %s=%d %s", change, response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
		})
	}
}
