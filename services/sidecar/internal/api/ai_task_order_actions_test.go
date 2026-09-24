package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiTaskOrderFixture(t *testing.T) []models.Task {
	t.Helper()
	date := "2026-09-18"
	statuses := []string{"todo", "done", "in_progress", "cancelled", "todo"}
	titles := []string{"规划 A", "完成 B", "执行 C", "取消 D", "规划 E"}
	tasks := make([]models.Task, len(statuses))
	for index := range tasks {
		order := (index + 1) * 1000
		var completedAt *string
		if statuses[index] == "done" {
			value := "2026-09-18T12:00:00Z"
			completedAt = &value
		}
		tasks[index] = models.Task{
			ID: uuid.NewString(), Title: titles[index], Kind: "work", Status: statuses[index],
			ReviewPolicy: "none", Priority: "P2", PlannedDate: &date, ManualOrder: &order,
			Version: 1, CompletedAt: completedAt, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z",
		}
	}
	return tasks
}

func aiTaskMoveArguments(source, anchor models.Task, placement string) string {
	return fmt.Sprintf(`{"action":"task.move","task_id":%q,"expected_version":%d,"changes":{"anchor_task_id":%q,"expected_anchor_version":%d,"placement":%q}}`,
		source.ID, source.Version, anchor.ID, anchor.Version, placement)
}

func TestAITaskMovePreservesTerminalSlotsAndConfirmsAtomicOrder(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tasks := aiTaskOrderFixture(t)
	if err := store.DB.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	otherDate := "2026-09-19"
	other := tasks[0]
	other.ID = uuid.NewString()
	other.Title = "其他日期"
	other.PlannedDate = &otherDate
	if err := store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, aiTaskMoveArguments(tasks[4], tasks[0], "before"))
	if !strings.Contains(row.PreviewJSON, `"task_order"`) ||
		!strings.Contains(row.PreviewJSON, `"before_position":3`) ||
		!strings.Contains(row.PreviewJSON, `"after_position":1`) ||
		!strings.Contains(row.PreviewJSON, `"group_count":5`) {
		t.Fatalf("preview=%s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND manual_order=5000", 1, tasks[4].ID)
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.ResultID == nil || *receipt.ResultID != tasks[4].ID || receipt.ResultVersion == nil || *receipt.ResultVersion != 2 || receipt.Route != "/tasks/"+tasks[4].ID {
		t.Fatalf("receipt=%#v", receipt)
	}
	expected := []string{tasks[4].ID, tasks[1].ID, tasks[0].ID, tasks[3].ID, tasks[2].ID}
	rows, err := loadAITaskOrderRows(store.DB, tasks[0].PlannedDate)
	if err != nil {
		t.Fatal(err)
	}
	for index, task := range rows {
		if task.ID != expected[index] || task.ManualOrder == nil || *task.ManualOrder != (index+1)*1000 {
			t.Fatalf("position %d: %#v", index, task)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND manual_order=1000", 1, other.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAITaskMoveRejectsGroupDriftWithoutPartialWrites(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tasks := aiTaskOrderFixture(t)
	if err := store.DB.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, aiTaskMoveArguments(tasks[4], tasks[0], "before"))
	finishAIGeneration(t, store, generation)
	newTask := tasks[0]
	newTask.ID = uuid.NewString()
	newTask.Title = "新加入任务"
	newTask.ManualOrder = nil
	if err := store.DB.Create(&newTask).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND manual_order=5000 AND version=1", 1, tasks[4].ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
}

func TestAITaskMoveRejectsInvalidOrNoopTargets(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	tasks := aiTaskOrderFixture(t)
	if err := store.DB.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	otherDate := "2026-09-20"
	other := tasks[0]
	other.ID = uuid.NewString()
	other.PlannedDate = &otherDate
	if err := store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		aiTaskMoveArguments(tasks[0], tasks[0], "after"),
		aiTaskMoveArguments(tasks[0], tasks[1], "after"), // completed anchor
		aiTaskMoveArguments(tasks[0], other, "after"),    // another date
		aiTaskMoveArguments(tasks[2], tasks[0], "after"), // already adjacent in active order
		strings.Replace(aiTaskMoveArguments(tasks[4], tasks[0], "before"), `"placement":"before"`, `"placement":"first"`, 1),
		strings.Replace(aiTaskMoveArguments(tasks[4], tasks[0], "before"), `"expected_anchor_version":1`, `"expected_anchor_version":0`, 1),
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("invalid move accepted: %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
