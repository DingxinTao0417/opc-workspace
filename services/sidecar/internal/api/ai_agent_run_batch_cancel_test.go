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

func addBatchCancelTask(t *testing.T, f aiAgentRunFixture, title string) aiAgentRunFixture {
	t.Helper()
	child := f
	child.Task = f.Task
	child.Task.ID = uuid.NewString()
	child.Task.Title = title
	if err := f.Store.DB.Create(&child.Task).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: child.Task.ID, ActorID: f.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: f.Generation.CreatedAt, Reason: "batch cancellation test",
	}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	return child
}

func aiCancelManyArgs(items ...aiAgentRunCancelManyItem) string {
	encoded, _ := json.Marshal(map[string]any{
		"action":  aiAgentRunCancelManyAction,
		"changes": map[string]any{"items": items},
	})
	return string(encoded)
}

func TestAIAgentRunCancelManyIsStrictAndBounded(t *testing.T) {
	f := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, f)
	valid := aiAgentRunCancelManyItem{AgentRunID: run.ID, TaskID: run.TaskID, ExpectedTaskVersion: f.Task.Version}
	for name, body := range map[string]string{
		"empty":            aiCancelManyArgs(),
		"duplicate":        aiCancelManyArgs(valid, valid),
		"bad version":      aiCancelManyArgs(aiAgentRunCancelManyItem{AgentRunID: run.ID, TaskID: run.TaskID}),
		"top level target": fmt.Sprintf(`{"action":%q,"task_id":%q,"changes":{"items":[{"agent_run_id":%q,"task_id":%q,"expected_task_version":1}]}}`, aiAgentRunCancelManyAction, run.TaskID, run.ID, run.TaskID),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAIWorkspaceAction([]byte(body)); err == nil {
				t.Fatalf("accepted %s", body)
			}
		})
	}
	items := make([]aiAgentRunCancelManyItem, maxAIAgentRunCancelItems+1)
	for i := range items {
		items[i] = aiAgentRunCancelManyItem{AgentRunID: uuid.NewString(), TaskID: uuid.NewString(), ExpectedTaskVersion: 1}
	}
	if _, err := parseAIWorkspaceAction([]byte(aiCancelManyArgs(items...))); err == nil {
		t.Fatal("accepted more than the bounded Run set")
	}
}

func TestAIAgentRunCancelManyApprovalIsAtomicAndSignalsEveryWorker(t *testing.T) {
	f := newAIAgentRunFixture(t)
	second := addBatchCancelTask(t, f, "Second parallel task")
	firstRun := seedRunningAgentRun(t, f)
	secondRun := seedRunningAgentRun(t, second)
	firstContext, firstCancel := context.WithCancel(context.Background())
	secondContext, secondCancel := context.WithCancel(context.Background())
	defer firstCancel()
	defer secondCancel()
	f.Service.agentRunCancels[firstRun.ID] = firstCancel
	f.Service.agentRunCancels[secondRun.ID] = secondCancel

	args := aiCancelManyArgs(
		aiAgentRunCancelManyItem{AgentRunID: firstRun.ID, TaskID: firstRun.TaskID, ExpectedTaskVersion: f.Task.Version},
		aiAgentRunCancelManyItem{AgentRunID: secondRun.ID, TaskID: secondRun.TaskID, ExpectedTaskVersion: second.Task.Version},
	)
	row := proposeTestAction(t, f.Store, f.Tool, args)
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.AgentRunCancelMany == nil || preview.AgentRunCancelMany.Count != 2 ||
		preview.AgentRunCancelMany.Items[0].TaskTitle != f.Task.Title ||
		preview.AgentRunCancelMany.Items[1].TaskTitle != second.Task.Title {
		t.Fatalf("preview=%s", row.PreviewJSON)
	}
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decisionRouter := gin.New()
	decisionRouter.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_cancel":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(decisionRouter, "POST", path,
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 422, "AGENT_CANCEL_CONFIRMATION_REQUIRED")

	if err := f.Store.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_second_batch_cancel BEFORE INSERT ON workflow_events WHEN NEW.action='agent_run_cancel_requested' AND NEW.aggregate_id='%s' BEGIN SELECT RAISE(ABORT,'injected rollback'); END`, secondRun.ID)).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(decisionRouter, "POST", path, body, nil)
	if response.Code != 500 {
		t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
	}
	if firstContext.Err() != nil || secondContext.Err() != nil {
		t.Fatal("rolled back batch signalled a worker")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 0)
	if err := f.Store.DB.Exec("DROP TRIGGER fail_second_batch_cancel").Error; err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		response = performRequest(decisionRouter, "POST", path, body, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data aiActionResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		result := envelope.Data.AgentRunCancelManyResult
		if result == nil || result.Count != 2 || len(result.Items) != 2 || envelope.Data.Route != "/ai?workspace=agents" ||
			envelope.Data.ResultID == nil || *envelope.Data.ResultID != row.ID {
			t.Fatalf("receipt=%s", response.Body.String())
		}
	}
	if firstContext.Err() != context.Canceled || secondContext.Err() != context.Canceled {
		t.Fatal("committed batch did not signal every live worker")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='confirmed' AND result_id=id", 1, row.ID)
}

func TestAIAgentRunCancelManyFailsClosedWhenOneRunChanges(t *testing.T) {
	f := newAIAgentRunFixture(t)
	second := addBatchCancelTask(t, f, "Changed parallel task")
	firstRun := seedRunningAgentRun(t, f)
	secondRun := seedRunningAgentRun(t, second)
	row := proposeTestAction(t, f.Store, f.Tool, aiCancelManyArgs(
		aiAgentRunCancelManyItem{AgentRunID: firstRun.ID, TaskID: firstRun.TaskID, ExpectedTaskVersion: f.Task.Version},
		aiAgentRunCancelManyItem{AgentRunID: secondRun.ID, TaskID: secondRun.TaskID, ExpectedTaskVersion: second.Task.Version},
	))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(secondRun, "", "AGENT_EXECUTION_FAILED", secondRun.CreatedAt); err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision",
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_cancel":true}`, row.Fingerprint)), nil)
	if response.Code != 409 {
		t.Fatalf("stale=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_cancel_requested'", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}

func TestAIAgentRunCancelManyPlanStateRequiresEveryTerminalRun(t *testing.T) {
	out := aiActionResponse{
		Status: "confirmed",
		Action: aiWorkspaceAction{Action: aiAgentRunCancelManyAction},
		AgentRunCancelManyResult: &aiAgentRunCancelManyResult{
			Count: 2,
			Items: []aiAgentRunCancelManyResultItem{
				{RunID: uuid.NewString(), TaskID: uuid.NewString(), Status: "cancelled"},
				{RunID: uuid.NewString(), TaskID: uuid.NewString(), Status: "running"},
			},
		},
	}
	state, satisfied := aiWorkPlanActionState(out)
	if state != "running" || satisfied {
		t.Fatalf("partial stop = %q %v", state, satisfied)
	}
	out.AgentRunCancelManyResult.Items[1].Status = "cancelled"
	state, satisfied = aiWorkPlanActionState(out)
	if state != "cancelled" || !satisfied {
		t.Fatalf("complete stop = %q %v", state, satisfied)
	}
}
