package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func nextAITaskBatchProposalTool(t *testing.T, store *database.Store, previous models.AIGeneration) (harness.Tool, models.AIGeneration) {
	t.Helper()
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id = ?", previous.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{
		ID: uuid.NewString(), SessionID: previous.SessionID, ProviderID: previous.ProviderID,
		Status: "streaming", CreatedAt: previous.CreatedAt, UpdatedAt: previous.CreatedAt,
	}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry(previous.SessionID, true, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"work", "actions"},
	}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("proposal tool missing")
	}
	return tool, generation
}

func TestAITaskBatchPreviewAndExecuteReuseSingleTaskCommands(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	color := "#334455"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &color, Status: "planning",
		Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	first := models.Task{ID: uuid.NewString(), Title: "写下发布说明", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	second := models.Task{ID: uuid.NewString(), Title: "整理素材", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 3, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_project","items":[%q,%q],"expected_versions":[1,3],"project_id":%q}}`,
		first.ID, second.ID, project.ID)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"task_batch"`) ||
		!strings.Contains(row.PreviewJSON, `"after":"Launch"`) ||
		!strings.Contains(row.PreviewJSON, `"count":2`) ||
		!strings.Contains(row.PreviewJSON, "写下发布说明") {
		t.Fatalf("batch preview=%s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id IS NOT NULL", 0)

	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.ResultID == nil || *receipt.ResultID != row.ID || receipt.ResultVersion == nil || *receipt.ResultVersion != 1 || receipt.Route != "/tasks" {
		t.Fatalf("batch receipt=%#v", receipt)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=?", 2, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=2", 1, first.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=4", 1, second.ID)
}

func TestAITaskBatchPriorityAndDueDateUseExactApproval(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	first := createTaskForTaskFacts(t, router, `{"title":"排优先级一"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"排优先级二"}`)
	priority := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_priority","items":[%q,%q],"expected_versions":[1,1],"priority":"P0"}}`, first.ID, second.ID))
	if !strings.Contains(priority.PreviewJSON, `"field":"priority"`) || !strings.Contains(priority.PreviewJSON, `"after":"P0"`) {
		t.Fatalf("priority preview=%s", priority.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, priority, ""))
	for _, id := range []string{first.ID, second.ID} {
		task := getTaskForTaskFacts(t, router, id)
		if task.Priority != "P0" || task.Version != 2 {
			t.Fatalf("priority task=%#v", task)
		}
	}
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	for _, invalid := range []string{
		fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_priority","items":[%q],"expected_versions":[2],"priority":"P9"}}`, first.ID),
		fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_due_date","items":[%q],"expected_versions":[2],"due_date":"2026-09-25T16:30:00"}}`, first.ID),
		fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_due_date","items":[%q],"expected_versions":[2]}}`, first.ID),
	} {
		if output, err := tool.Execute(context.Background(), []byte(invalid)); err == nil {
			t.Fatalf("invalid proposal accepted: %s", output)
		}
	}
	due := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_due_date","items":[%q,%q],"expected_versions":[2,2],"due_date":"2026-09-25T16:30:00+08:00"}}`, first.ID, second.ID))
	if !strings.Contains(due.PreviewJSON, `"field":"due_date"`) || !strings.Contains(due.PreviewJSON, `"after":"2026-09-25T08:30:00Z"`) {
		t.Fatalf("due preview=%s", due.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, due, ""))
	for _, id := range []string{first.ID, second.ID} {
		task := getTaskForTaskFacts(t, router, id)
		if task.Version != 3 || task.DueDate == nil || *task.DueDate != "2026-09-25T08:30:00Z" {
			t.Fatalf("due task=%#v", task)
		}
	}
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	clear := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_due_date","items":[%q,%q],"expected_versions":[3,3],"due_date":null}}`, first.ID, second.ID))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, clear, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id IN (?,?) AND due_date IS NULL", 2, first.ID, second.ID)
}

func TestAITaskBatchResponsibilitySetsAndClearsMixedCurrentAssignments(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	first := createTaskForTaskFacts(t, router, `{"title":"未分派任务"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"现有负责人任务"}`)
	current := createAssignmentForTest(t, router, second.ID, "assignee", models.BuiltinOwnerActorID, second.Version, "")
	person := createActorForTest(t, router, `{"type":"person","display_name":"新负责人","notes":"PRIVATE_ACTOR_NOTES"}`, nil)
	args := fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_assignee","items":[%q,%q],"expected_versions":[%d,%d],"actor_id":%q,"reason":"项目任务交接"}}`,
		first.ID, second.ID, first.Version, current.Task.Version, person.ID)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"field":"assignee"`) || !strings.Contains(row.PreviewJSON, `"before_actor"`) ||
		!strings.Contains(row.PreviewJSON, `"after_actor"`) || !strings.Contains(row.PreviewJSON, current.Assignment.ID) ||
		strings.Contains(row.PreviewJSON, "PRIVATE_ACTOR_NOTES") {
		t.Fatalf("incomplete or private preview: %s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE actor_id=?", 0, person.ID)
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.ResultID == nil || *receipt.ResultID != row.ID || receipt.Route != "/tasks" {
		t.Fatalf("batch receipt=%#v", receipt)
	}
	for _, id := range []string{first.ID, second.ID} {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND actor_id=? AND role='assignee' AND unassigned_at IS NULL", 1, id, person.ID)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE id=? AND unassigned_at IS NOT NULL AND reason=?", 1, current.Assignment.ID, "项目任务交接")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
	modelReceipt, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || strings.Contains(modelReceipt, "新负责人") || strings.Contains(modelReceipt, "项目任务交接") {
		t.Fatalf("private model receipt=%s err=%v", modelReceipt, err)
	}
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	firstNow, _ := loadTask(store.DB, first.ID)
	secondNow, _ := loadTask(store.DB, second.ID)
	clear := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"clear_assignee","items":[%q,%q],"expected_versions":[%d,%d],"reason":"移交完成"}}`, first.ID, second.ID, firstNow.Version, secondNow.Version))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, clear, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?) AND role='assignee' AND unassigned_at IS NULL", 0, first.ID, second.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?) AND actor_id=? AND reason=? AND unassigned_at IS NOT NULL", 2, first.ID, second.ID, person.ID, "移交完成")
}

func TestAITaskBatchResponsibilityApprovalRollsBackAndRejectsActorDrift(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	first := createTaskForTaskFacts(t, router, `{"title":"设计任务"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"实现任务"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"原负责人"}`, nil)
	args := fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_assignee","items":[%q,%q],"expected_versions":[%d,%d],"actor_id":%q,"reason":"项目排期"}}`, first.ID, second.ID, first.Version, second.Version, person.ID)
	row := proposeTestAction(t, store, tool, args)
	finishAIGeneration(t, store, generation)
	if err := store.DB.Model(&models.Actor{}).Where("id=?", person.ID).Updates(map[string]any{"display_name": "已改名负责人", "version": person.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code != 409 || !strings.Contains(response.Body.String(), "AI_ACTION_PREVIEW_CHANGED") {
		t.Fatalf("actor drift accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?)", 0, first.ID, second.ID)
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	row = proposeTestAction(t, store, tool, args)
	finishAIGeneration(t, store, generation)
	if err := store.DB.Exec(fmt.Sprintf(`CREATE TRIGGER fail_second_assignment BEFORE INSERT ON task_assignments WHEN NEW.task_id='%s' BEGIN SELECT RAISE(ABORT,'test'); END`, second.ID)).Error; err != nil {
		t.Fatal(err)
	}
	if response := decideTestAction(t, router, row, ""); response.Code < 400 {
		t.Fatalf("partial assignment accepted: %d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?)", 0, first.ID, second.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, first.ID, first.Version)
	if err := store.DB.Exec("DROP TRIGGER fail_second_assignment").Error; err != nil {
		t.Fatal(err)
	}
	decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?) AND actor_id=? AND unassigned_at IS NULL", 2, first.ID, second.ID, person.ID)
}

func TestAITaskBatchReviewerAndInvalidResponsibilityTargets(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	first := createTaskForTaskFacts(t, router, `{"title":"人工验收 A","review_policy":"manual"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"人工验收 B","review_policy":"manual"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"不可做审核人"}`, nil)
	set := fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_reviewer","items":[%q,%q],"expected_versions":[%d,%d],"actor_id":%q,"reason":"补齐人工审核责任"}}`, first.ID, second.ID, first.Version, second.Version, models.BuiltinOwnerActorID)
	shortBlockReason := fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"block","items":[%q],"expected_versions":[%d],"reason":"x"}}`, first.ID, first.Version)
	if output, err := tool.Execute(context.Background(), []byte(shortBlockReason)); err == nil {
		t.Fatalf("one-character block reason accepted: %s", output)
	}
	for _, invalid := range []string{
		strings.Replace(set, models.BuiltinOwnerActorID, person.ID, 1),
		strings.Replace(set, `,"reason":"补齐人工审核责任"`, "", 1),
		strings.Replace(set, `,"actor_id":"`+models.BuiltinOwnerActorID+`"`, "", 1),
		strings.Replace(set, `"set_reviewer"`, `"clear_reviewer"`, 1),
	} {
		if output, err := tool.Execute(context.Background(), []byte(invalid)); err == nil {
			t.Fatalf("invalid responsibility batch accepted: %s => %s", invalid, output)
		}
	}
	row := proposeTestAction(t, store, tool, set)
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?) AND role='reviewer' AND actor_id=? AND unassigned_at IS NULL", 2, first.ID, second.ID, models.BuiltinOwnerActorID)
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	if output, err := tool.Execute(context.Background(), []byte(set)); err == nil {
		t.Fatalf("stale/no-op reviewer batch accepted: %s", output)
	}
	firstNow, _ := loadTask(store.DB, first.ID)
	secondNow, _ := loadTask(store.DB, second.ID)
	clear := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"clear_reviewer","items":[%q,%q],"expected_versions":[%d,%d],"reason":"调整验收责任"}}`, first.ID, second.ID, firstNow.Version, secondNow.Version))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, clear, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id IN (?,?) AND role='reviewer' AND unassigned_at IS NULL", 0, first.ID, second.ID)
}

func TestAITaskBatchTagsAndLifecycleShareTheSameTransaction(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tag := models.Tag{ID: uuid.NewString(), Name: "Focus", Color: "#112233", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	first := models.Task{ID: uuid.NewString(), Title: "任务 A", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	second := models.Task{ID: uuid.NewString(), Title: "任务 B", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	// Starting a task requires an active assignee, so the fixture creates one
	// for each task before the lifecycle batch.
	for _, taskID := range []string{first.ID, second.ID} {
		if err := store.DB.Create(&models.TaskAssignment{
			ID: uuid.NewString(), TaskID: taskID, ActorID: models.BuiltinOwnerActorID, Role: "assignee",
			AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: "2026-09-18T12:00:00Z",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	addTags := proposeTestAction(t, store, tool, fmt.Sprintf(
		`{"action":"task.batch_update","changes":{"batch_action":"add_tags","items":[%q,%q],"expected_versions":[1,1],"tag_ids":[%q]}}`,
		first.ID, second.ID, tag.ID))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, addTags, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE tag_id=?", 2, tag.ID)

	// Removing the tag from both tasks keeps the native batch semantics.
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	current, err := loadTask(store.DB, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	removeTags := proposeTestAction(t, store, tool, fmt.Sprintf(
		`{"action":"task.batch_update","changes":{"batch_action":"remove_tags","items":[%q,%q],"expected_versions":[%d,%d],"tag_ids":[%q]}}`,
		first.ID, second.ID, current.Version, current.Version, tag.ID))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, removeTags, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE tag_id=?", 0, tag.ID)

	// Lifecycle batches use the same transition/reconciliation rules as the
	// workbench batch endpoint.
	tool, generation = nextAITaskBatchProposalTool(t, store, generation)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id=? AND unassigned_at IS NULL", 1, first.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id=? AND status='active'", 1, models.BuiltinOwnerActorID)
	started, err := loadTask(store.DB, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	startBatch := proposeTestAction(t, store, tool, fmt.Sprintf(
		`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%q,%q],"expected_versions":[%d,%d]}}`,
		first.ID, second.ID, started.Version, started.Version))
	finishAIGeneration(t, store, generation)
	decodeTestActionResponse(t, decideTestAction(t, router, startBatch, ""))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='in_progress'", 2)
}

func TestAITaskBatchRejectsDriftInvalidPayloadsAndTagOverflow(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	full := models.Task{ID: uuid.NewString(), Title: "Full", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	empty := models.Task{ID: uuid.NewString(), Title: "Empty", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&full).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&empty).Error; err != nil {
		t.Fatal(err)
	}
	tag := models.Tag{ID: uuid.NewString(), Name: "Twenty", Color: "#445566", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 20; index++ {
		created := models.Tag{ID: uuid.NewString(), Name: fmt.Sprintf("tag-%02d", index), Color: "#556677",
			Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
		if err := store.DB.Create(&created).Error; err != nil {
			t.Fatal(err)
		}
		if err := store.DB.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", full.ID, created.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	// The 21st tag is caught before a proposal is stored, so the user never
	// sees a confirmable card that can only fail at approval time.
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(
		`{"action":"task.batch_update","changes":{"batch_action":"add_tags","items":[%q,%q],"expected_versions":[1,1],"tag_ids":[%q]}}`,
		full.ID, empty.ID, tag.ID))); err == nil {
		t.Fatal("overflowing batch must be rejected")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE task_id=?", 0, empty.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1", 1, empty.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1", 1, full.ID)

	for _, test := range []struct {
		label   string
		payload string
	}{
		{"stale version", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%q],"expected_versions":[9]}}`, empty.ID)},
		{"empty items", `{"action":"task.batch_update","changes":{"batch_action":"start","items":[],"expected_versions":[]}}`},
		{"duplicate items", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%q,%q],"expected_versions":[1,1]}}`, empty.ID, empty.ID)},
		{"version length mismatch", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%q],"expected_versions":[1,1]}}`, empty.ID)},
		{"unknown action", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"archive","items":[%q],"expected_versions":[1]}}`, empty.ID)},
		{"missing reason", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"block","items":[%q],"expected_versions":[1]}}`, empty.ID)},
		{"missing project_id", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_project","items":[%q],"expected_versions":[1]}}`, empty.ID)},
		{"missing planned_date", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"set_planned_date","items":[%q],"expected_versions":[1]}}`, empty.ID)},
		{"unexpected field", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%q],"expected_versions":[1],"project_id":null}}`, empty.ID)},
		{"top level target", fmt.Sprintf(`{"action":"task.batch_update","task_id":%q,"changes":{"batch_action":"start","items":[%q],"expected_versions":[1]}}`, empty.ID, empty.ID)},
		{"too many items", fmt.Sprintf(`{"action":"task.batch_update","changes":{"batch_action":"start","items":[%s],"expected_versions":[%s]}}`,
			strings.Join(repeatUUIDs(21), ","), strings.TrimSuffix(strings.Repeat("1,", 21), ","))},
	} {
		if _, err := tool.Execute(context.Background(), []byte(test.payload)); err == nil {
			t.Fatalf("%s must be rejected", test.label)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='todo'", 2)
}

func repeatUUIDs(count int) []string {
	values := make([]string, 0, count)
	for index := 0; index < count; index++ {
		values = append(values, fmt.Sprintf("%q", uuid.NewString()))
	}
	return values
}

func TestAITaskBatchRejectsPreviewChangedSelection(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task := models.Task{ID: uuid.NewString(), Title: "Moving", Kind: "work", Status: "todo", ReviewPolicy: "none",
		Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(
		`{"action":"task.batch_update","changes":{"batch_action":"set_planned_date","items":[%q],"expected_versions":[1],"planned_date":"2026-09-25"}}`, task.ID))
	if err := store.DB.Model(&models.Task{}).Where("id = ?", task.ID).
		Updates(map[string]any{"version": 2, "updated_at": "2026-09-19T09:00:00Z"}).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, store, generation)
	changed := decideTestAction(t, router, row, "")
	// A direct version drift is reported as the domain conflict; nothing is
	// applied and the card must be regenerated.
	assertAPIError(t, changed, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND planned_date IS NULL", 1, task.ID)
}
