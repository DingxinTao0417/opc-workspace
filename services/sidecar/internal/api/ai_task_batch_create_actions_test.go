package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITaskBatchCreateHierarchyAndReceipt(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":{"drafts":[{"key":"plan","title":"规划发布","review_policy":"manual"},{"key":"write","parent_key":"plan","title":"撰写文案","priority":"P1","planned_date":"2026-09-25"}]}}`, project.ID)
	row := proposeTestAction(t, store, tool, args)
	assertModelReceipt := func(wantIDs []string) {
		t.Helper()
		body, err := service.aiActionReceipts(context.Background(), generation.SessionID)
		var envelope struct {
			Items []struct {
				CreatedTaskIDs []string `json:"created_task_ids"`
			} `json:"items"`
		}
		if err != nil || json.Unmarshal([]byte(body), &envelope) != nil || len(envelope.Items) != 1 {
			t.Fatalf("invalid model receipt: %s %v", body, err)
		}
		if len(envelope.Items[0].CreatedTaskIDs) != len(wantIDs) {
			t.Fatalf("unexpected created task IDs: %s", body)
		}
		for index, want := range wantIDs {
			if envelope.Items[0].CreatedTaskIDs[index] != want {
				t.Fatalf("unexpected created task IDs: %s", body)
			}
		}
		for _, private := range []string{"规划发布", "撰写文案", "parent_key", "drafts", "description", "changes", "preview"} {
			if strings.Contains(body, private) {
				t.Fatalf("model receipt leaked %q: %s", private, body)
			}
		}
	}
	assertModelReceipt(nil)
	if !strings.Contains(row.PreviewJSON, `"task_batch_create"`) || !strings.Contains(row.PreviewJSON, `"parent_key":"plan"`) {
		t.Fatalf("missing full batch preview: %s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.ResultID == nil || *receipt.ResultID != row.ID || receipt.Route != "/projects/"+project.ID || receipt.TaskBatchCreateResult == nil || receipt.TaskBatchCreateResult.Count != 2 {
		t.Fatalf("invalid batch receipt: %#v", receipt)
	}
	items := receipt.TaskBatchCreateResult.Items
	if len(items) != 2 || items[0].ID != aiTaskBatchCreatedID(row.ID, 0) || items[1].ID != aiTaskBatchCreatedID(row.ID, 1) || items[1].Route != "/tasks/"+items[1].ID {
		t.Fatalf("invalid exact task links: %#v", items)
	}
	assertModelReceipt([]string{items[0].ID, items[1].ID})
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 2, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND parent_task_id=?", 1, items[1].ID, items[0].ID)
}

func TestAITaskBatchCreateInitialAssignmentAndReviewer(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	person := createActorForTest(t, router, `{"type":"person","display_name":"初始负责人","notes":"PRIVATE_ACTOR_NOTES"}`, nil)
	args := fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":{"drafts":[{"key":"owned","title":"交付草案","review_policy":"manual","assignee_actor_id":%q},{"key":"open","title":"待分派"}]}}`, project.ID, person.ID)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"actor_name":"初始负责人"`) || !strings.Contains(row.PreviewJSON, `"role":"reviewer"`) || strings.Contains(row.PreviewJSON, "PRIVATE_ACTOR_NOTES") {
		t.Fatalf("invalid assignment preview: %s", row.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.TaskBatchCreateResult == nil || len(receipt.TaskBatchCreateResult.Items) != 2 {
		t.Fatalf("invalid result: %#v", receipt)
	}
	first := receipt.TaskBatchCreateResult.Items[0].ID
	second := receipt.TaskBatchCreateResult.Items[1].ID
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND role='assignee' AND actor_id=? AND unassigned_at IS NULL", 1, first, person.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND role='reviewer' AND actor_id=? AND unassigned_at IS NULL", 1, first, models.BuiltinOwnerActorID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, second)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAITaskBatchCreateRejectsDriftAndRollsBack(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":{"drafts":[{"key":"a","title":"任务一","assignee_actor_id":%q},{"key":"b","title":"任务二"}]}}`, project.ID, models.BuiltinOwnerActorID)
	row := proposeTestAction(t, store, tool, args)
	finishAIGeneration(t, store, generation)
	if err := store.DB.Model(&models.Project{}).Where("id=?", project.ID).Update("version", 2).Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code != 409 {
		t.Fatalf("stale Project version accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
	if err := store.DB.Model(&models.Project{}).Where("id=?", project.ID).Update("version", 1).Error; err != nil {
		t.Fatal(err)
	}
	// The second insertion collides after the first has run. The approval
	// transaction must roll back the first insertion as well.
	blocker := models.Task{ID: aiTaskBatchCreatedID(row.ID, 1), Title: "占用的任务", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&blocker).Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code < 400 {
		t.Fatalf("collision accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=?", 0, aiTaskBatchCreatedID(row.ID, 0))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=?", 0, aiTaskBatchCreatedID(row.ID, 0))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
}

func TestAITaskBatchCreateRejectsMalformedDrafts(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, changes string }{
		{"empty", `{"drafts":[]}`},
		{"duplicate key", `{"drafts":[{"key":"a","title":"任务一"},{"key":"a","title":"任务二"}]}`},
		{"forward parent", `{"drafts":[{"key":"a","parent_key":"b","title":"任务一"},{"key":"b","title":"任务二"}]}`},
		{"empty parent", `{"drafts":[{"key":"a","parent_key":"","title":"任务一"}]}`},
		{"unknown field", `{"drafts":[{"key":"a","title":"任务一","status":"done"}]}`},
		{"duplicate field", `{"drafts":[{"key":"a","title":"任务一","title":"任务二"}]}`},
		{"null field", `{"drafts":[{"key":"a","title":"任务一","description":null}]}`},
		{"invalid assignee", `{"drafts":[{"key":"a","title":"任务一","assignee_actor_id":"not-an-actor"}]}`},
		{"unknown assignee", fmt.Sprintf(`{"drafts":[{"key":"a","title":"任务一","assignee_actor_id":%q}]}`, uuid.NewString())},
	} {
		payload := fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":%s}`, project.ID, test.changes)
		if _, err := tool.Execute(context.Background(), []byte(payload)); err == nil {
			t.Fatalf("%s accepted", test.name)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
}

func TestAITaskBatchCreateRejectsChangedTagPreview(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	tag := models.Tag{ID: uuid.NewString(), Name: "设计", Color: "#112233", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":{"drafts":[{"key":"a","title":"设计主页","tag_ids":[%q]}]}}`, project.ID, tag.ID))
	finishAIGeneration(t, store, generation)
	if err := store.DB.Model(&models.Tag{}).Where("id=?", tag.ID).Update("name", "改名").Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code != 409 || !strings.Contains(response.Body.String(), "AI_ACTION_PREVIEW_CHANGED") {
		t.Fatalf("tag drift accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
}

func TestAITaskBatchCreateRejectsChangedAssigneePreview(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning", Version: 1,
		CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	person := createActorForTest(t, router, `{"type":"person","display_name":"原负责人"}`, nil)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_create","project_id":%q,"expected_version":1,"changes":{"drafts":[{"key":"a","title":"设计主页","review_policy":"manual","assignee_actor_id":%q}]}}`, project.ID, person.ID))
	finishAIGeneration(t, store, generation)
	if err := store.DB.Model(&models.Actor{}).Where("id=?", person.ID).Updates(map[string]any{"display_name": "已改名负责人", "version": person.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code != 409 || !strings.Contains(response.Body.String(), "AI_ACTION_PREVIEW_CHANGED") {
		t.Fatalf("actor drift accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 0, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments", 0)
}
