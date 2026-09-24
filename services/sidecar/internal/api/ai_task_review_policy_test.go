package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITaskReviewPolicyRequiresHumanConfirmationAndNativeCommand(t *testing.T) {
	for _, initial := range []string{"none", "manual"} {
		t.Run(initial, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Review policy approval","review_policy":%q}`, initial))
			target := "manual"
			if initial == "manual" {
				target = "none"
			}
			proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":%q}}`, task.ID, task.Version, target))
			preview := aiTaskPolicyPreviewForTest(t, proposal)
			if preview.Before["review_policy"] != initial || preview.After["review_policy"] != target || preview.Before["status"] != "todo" || preview.After["status"] != "todo" || preview.After["will_request_parent_review"] != false {
				t.Fatalf("missing policy before/after: %s", proposal.PreviewJSON)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy=? AND version=?", 1, task.ID, initial, task.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 0)
			finishAIGeneration(t, store, generation)
			body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint))
			for i := 0; i < 2; i++ {
				response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("policy approval=%d %s", response.Code, response.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy=? AND version=? AND status='todo'", 1, task.ID, target, task.Version+1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
		})
	}
}

func aiTaskPolicyPreviewForTest(t *testing.T, proposal models.AIActionProposal) aiActionPreview {
	t.Helper()
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(proposal.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	return preview
}

func aiTaskPolicyDecisionForTest(router *gin.Engine, proposal models.AIActionProposal) *httptest.ResponseRecorder {
	return performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
}

func aiTaskPolicyHistoryForTest(t *testing.T, store *database.Store, task models.Task) {
	t.Helper()
	owner, now := models.BuiltinOwnerActorID, task.CreatedAt
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: task.ID, Sequence: 1, Status: "withdrawn",
		Summary: "Private historical output", SubmittedByActorID: owner, SubmittedAt: now,
		WithdrawnByActorID: &owner, WithdrawnAt: &now,
	}
	if err := store.DB.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAITaskReviewPolicyRejectsMalformedAndDoesNotExpandBatch(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Strict policy schema"}`)
	for _, value := range []string{`null`, `"unknown"`, `""`, `true`, `1`, `[]`, `{}`} {
		for _, extra := range []string{"", `,"title":"Valid title"`} {
			args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":%s%s}}`, task.ID, task.Version, value, extra)
			if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
				t.Fatalf("accepted malformed policy: %s", args)
			}
		}
	}
	for _, field := range []string{"title", "description", "kind", "priority", "completion_criteria"} {
		args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual",%q:null}}`, task.ID, task.Version, field)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("silently ignored null field %s beside policy", field)
		}
	}
	for _, changes := range []string{
		fmt.Sprintf(`{"batch_action":"set_review_policy","items":[%q],"expected_versions":[1],"review_policy":"manual"}`, task.ID),
		fmt.Sprintf(`{"batch_action":"start","items":[%q],"expected_versions":[1],"review_policy":"manual"}`, task.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(`{"action":"task.batch_update","changes":`+changes+`}`)); err == nil {
			t.Fatal("expanded batch policy capability")
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND review_policy='none'", 1, task.ID)
	// Match the native trimming rule, but freeze the canonical enum in both
	// ActionJSON and PreviewJSON so frontends need not reinterpret the input.
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":" manual "}}`, task.ID))
	if !strings.Contains(row.ActionJSON, `"review_policy":"manual"`) || strings.Contains(row.ActionJSON, `" manual "`) || aiTaskPolicyPreviewForTest(t, row).After["review_policy"] != "manual" {
		t.Fatalf("policy not canonical: %s / %s", row.ActionJSON, row.PreviewJSON)
	}
}

func TestAITaskReviewPolicyLocksRealLifecycleStatesButAllowsSameValue(t *testing.T) {
	for _, status := range []string{"in_progress", "blocked", "waiting_review", "done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			task, _ := setupManualReviewTask(t, router)
			if status == "waiting_review" || status == "done" {
				response := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", []byte(`{"summary":"Prepared output","artifacts":[]}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)})
				if response.Code != http.StatusOK && response.Code != http.StatusCreated {
					t.Fatalf("submit=%d %s", response.Code, response.Body.String())
				}
				if status == "done" {
					task = getTaskForTaskFacts(t, router, task.ID)
					response = performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)})
					if response.Code != http.StatusOK {
						t.Fatalf("review=%d %s", response.Code, response.Body.String())
					}
				}
			} else {
				action, reason := "start", ""
				if status == "blocked" {
					action, reason = "block", "Blocked for review"
				}
				if status == "cancelled" {
					action, reason = "cancel", "Cancelled by owner"
				}
				runTaskLifecycleForParentTest(t, router, task, action, reason)
			}
			task = getTaskForTaskFacts(t, router, task.ID)
			if task.Status != status {
				t.Fatalf("fixture status=%s", task.Status)
			}
			args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"none"}}`, task.ID, task.Version)
			if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "todo") {
				t.Fatalf("unlocked %s: %v", status, err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual","title":"Same policy facts edit"}}`, task.ID, task.Version))
			preview := aiTaskPolicyPreviewForTest(t, row)
			if preview.After["status"] != status || preview.After["will_request_parent_review"] != false {
				t.Fatalf("same-value impact=%s", row.PreviewJSON)
			}
			finishAIGeneration(t, store, generation)
			if response := aiTaskPolicyDecisionForTest(router, row); response.Code != http.StatusOK {
				t.Fatalf("same value=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status=? AND review_policy='manual' AND version=? AND title='Same policy facts edit'", 1, task.ID, status, task.Version+1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 0)
		})
	}
}

func TestAITaskReviewPolicyHistoryLocksBothProposalAndConfirmation(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprint(late), func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			task := createTaskForTaskFacts(t, router, `{"title":"Historical policy gate","review_policy":"manual"}`)
			args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"none"}}`, task.ID, task.Version)
			var row models.AIActionProposal
			if late {
				row = proposeTestAction(t, store, tool, args)
			}
			aiTaskPolicyHistoryForTest(t, store, task)
			if late {
				finishAIGeneration(t, store, generation)
				assertAPIError(t, aiTaskPolicyDecisionForTest(router, row), http.StatusConflict, "TASK_REVIEW_POLICY_LOCKED")
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			} else {
				if _, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "submission") {
					t.Fatalf("history not locked: %v", err)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
				row = proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":"manual"}}`, task.ID))
				finishAIGeneration(t, store, generation)
				if response := aiTaskPolicyDecisionForTest(router, row); response.Code != http.StatusOK {
					t.Fatalf("same policy after history=%d %s", response.Code, response.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND review_policy='manual'", 1, task.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='withdrawn'", 1, task.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 0)
		})
	}
}

func TestAITaskReviewPolicyConfirmationRejectsVersionAndStatusDrift(t *testing.T) {
	for _, drift := range []string{"version", "status"} {
		t.Run(drift, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			task := createTaskForTaskFacts(t, router, `{"title":"Policy drift"}`)
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":"manual"}}`, task.ID))
			wantCode := "TASK_REVIEW_POLICY_LOCKED"
			if drift == "version" {
				response := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+task.ID, []byte(`{"title":"Edited by human"}`), map[string]string{"If-Match": `"1"`})
				if response.Code != http.StatusOK {
					t.Fatal(response.Body.String())
				}
				wantCode = "VERSION_CONFLICT"
			} else {
				// Simulate an independently refreshed/imported fact without relying
				// solely on the optimistic version to reject the stale policy.
				if err := store.DB.Model(&models.Task{}).Where("id=?", task.ID).Update("status", "in_progress").Error; err != nil {
					t.Fatal(err)
				}
			}
			finishAIGeneration(t, store, generation)
			assertAPIError(t, aiTaskPolicyDecisionForTest(router, row), http.StatusConflict, wantCode)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='none'", 1, task.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
		})
	}
}

func aiTaskPolicyRollupFixture(t *testing.T, router *gin.Engine) (models.Task, models.Task, actorResponse) {
	t.Helper()
	parent := createTaskForTaskFacts(t, router, `{"title":"Policy rollup parent","review_policy":"none"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Policy rollup producer"}`, nil)
	createAssignmentForTest(t, router, parent.ID, "assignee", person.ID, parent.Version, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	createAssignmentForTest(t, router, parent.ID, "reviewer", models.BuiltinOwnerActorID, parent.Version, "")
	child := createChildTaskForTest(t, router, parent.ID, "Completed policy child")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	return getTaskForTaskFacts(t, router, parent.ID), getTaskForTaskFacts(t, router, child.ID), person
}

func TestAITaskReviewPolicyRollupPreviewAndAtomicRollback(t *testing.T) {
	for _, failureAction := range []string{"task_review_policy_changed", "task_parent_review_requested", "ai_workspace_action_confirmed"} {
		t.Run(failureAction, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			parent, _, _ := aiTaskPolicyRollupFixture(t, router)
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual"}}`, parent.ID, parent.Version))
			preview := aiTaskPolicyPreviewForTest(t, row)
			if preview.Before["status"] != "todo" || preview.After["status"] != "waiting_review" || preview.After["will_request_parent_review"] != true || preview.After["parent_rollup_gates_ready"] != true || preview.After["subtask_total"] != float64(1) || preview.After["subtask_completed"] != float64(1) || preview.After["subtask_cancelled"] != float64(0) {
				t.Fatalf("rollup preview=%s", row.PreviewJSON)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions", 0)
			finishAIGeneration(t, store, generation)
			if err := store.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_policy_event BEFORE INSERT ON workflow_events WHEN NEW.action='%s' BEGIN SELECT RAISE(ABORT,'policy test'); END`, failureAction)).Error; err != nil {
				t.Fatal(err)
			}
			if response := aiTaskPolicyDecisionForTest(router, row); response.Code != http.StatusInternalServerError {
				t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='none' AND status='todo' AND version=? AND current_submission_id IS NULL", 1, parent.ID, parent.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action IN ('task_review_policy_changed','task_parent_review_requested','ai_workspace_action_confirmed')", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
			if err := store.DB.Exec("DROP TRIGGER fail_policy_event").Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if response := aiTaskPolicyDecisionForTest(router, row); response.Code != http.StatusOK {
					t.Fatalf("rollup confirmation=%d %s", response.Code, response.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND review_policy='manual' AND status='waiting_review' AND version=? AND current_submission_id IS NOT NULL", 1, parent.ID, parent.Version+2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND origin='child_rollup' AND status='pending_review' AND submitted_by_actor_id=?", 1, parent.ID, models.BuiltinSystemActorID)
			for _, action := range []string{"task_review_policy_changed", "task_parent_review_requested", "ai_workspace_action_confirmed"} {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action=?", 1, action)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND unassigned_at IS NULL", 2, parent.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
		})
	}
}

func TestAITaskReviewPolicyRechecksRollupFacts(t *testing.T) {
	for _, drift := range []string{"new_child", "child_reopened", "assignment_ended_without_task_version"} {
		t.Run(drift, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			parent, child, _ := aiTaskPolicyRollupFixture(t, router)
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"review_policy":"manual"}}`, parent.ID, parent.Version))
			wantCode := "VERSION_CONFLICT"
			switch drift {
			case "new_child":
				createChildTaskForTest(t, router, parent.ID, "Late policy child")
			case "child_reopened":
				runTaskLifecycleForParentTest(t, router, child, taskLifecycleReopen, "")
			case "assignment_ended_without_task_version":
				// Valid independent assignment history does not itself have a
				// database trigger bumping the task version. Recheck the native
				// rollup gate, not just the task CAS, on this recovered fact set.
				if err := store.DB.Model(&models.TaskAssignment{}).Where("task_id=? AND role='assignee' AND unassigned_at IS NULL", parent.ID).Updates(map[string]any{"unassigned_at": generation.CreatedAt, "reason": "Independent assignment history"}).Error; err != nil {
					t.Fatal(err)
				}
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, parent.ID, parent.Version)
				wantCode = "AI_ACTION_PREVIEW_CHANGED"
			}
			finishAIGeneration(t, store, generation)
			assertAPIError(t, aiTaskPolicyDecisionForTest(router, row), http.StatusConflict, wantCode)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND review_policy='none'", 1, parent.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_submissions", 0)
		})
	}
}

func TestAITaskReviewPolicyConcurrentApprovalKeepsOneVersionAndEvent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Concurrent policy approvals"}`)
	rows := []models.AIActionProposal{
		proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":"manual","title":"First choice"}}`, task.ID)),
		proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":"manual","title":"Second choice"}}`, task.ID)),
	}
	finishAIGeneration(t, store, generation)
	responses := make([]*httptest.ResponseRecorder, len(rows))
	var workers sync.WaitGroup
	start := make(chan struct{})
	for index := range rows {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			responses[index] = aiTaskPolicyDecisionForTest(router, rows[index])
		}(index)
	}
	close(start)
	workers.Wait()
	success, conflict := 0, 0
	for _, response := range responses {
		switch response.Code {
		case http.StatusOK:
			success++
		case http.StatusConflict:
			assertAPIError(t, response, http.StatusConflict, "VERSION_CONFLICT")
			conflict++
		default:
			t.Fatalf("concurrent result=%d %s", response.Code, response.Body.String())
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=2 AND review_policy='manual'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_review_policy_changed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='confirmed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
}
