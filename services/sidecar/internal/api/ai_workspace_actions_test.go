package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiActionTestFixture(t *testing.T, clocks ...time.Time) (*gin.Engine, *database.Store, *API, harness.Tool, models.AIGeneration) {
	t.Helper()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if len(clocks) > 0 {
		now = clocks[0]
	}
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "Action tests", Persist: true, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("proposal tool missing")
	}
	return router, store, service, tool, generation
}

func proposeTestAction(t *testing.T, store *database.Store, tool harness.Tool, args string) models.AIActionProposal {
	t.Helper()
	result, err := tool.Execute(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("propose: %s %v", result, err)
	}
	var data struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		t.Fatal(err)
	}
	var row models.AIActionProposal
	if err := store.DB.First(&row, "id=?", data.ID).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestAIWorkspaceActionCreateRequiresExactHumanDecisionAndReplays(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	args := `{"action":"task.create","changes":{"title":"真实审批任务","planned_date":"2026-09-20"}}`
	row := proposeTestAction(t, store, tool, args)
	if strings.Contains(row.PreviewJSON, "tag_names") {
		t.Fatalf("unrequested tag preview=%s", row.PreviewJSON)
	}
	duplicate := proposeTestAction(t, store, tool, args)
	if duplicate.ID != row.ID {
		t.Fatal("duplicate proposal")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", path, []byte(`{"fingerprint":"`+strings.Repeat("0", 64)+`","decision":"confirm"}`), nil), 409, "AI_ACTION_CHANGED")
	for i := 0; i < 2; i++ {
		response := performRequest(router, "POST", path, body, nil)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"confirmed"`) {
			t.Fatalf("confirmation %d: %d %s", i, response.Code, response.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title=? AND planned_date=?", 1, "真实审批任务", "2026-09-20")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE current_json LIKE ?", 0, "%真实审批任务%")
	response := performRequest(router, "GET", "/api/v1/ai/generations/"+generation.ID+"/actions", nil, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"confirmed"`) || !strings.Contains(response.Body.String(), "/tasks/") {
		t.Fatalf("readback=%s", response.Body.String())
	}
	if err := store.DB.Model(&row).Update("action_json", `{}`).Error; err == nil {
		t.Fatal("proposal mutation accepted")
	}
	assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil), 409, "AI_ACTION_ALREADY_DECIDED")
}

func TestAIWorkspaceActionTaskTagsUseNativeCreateAndUpdateTransactions(t *testing.T) {
	t.Run("create keeps the selected tags and shows their names", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t)
		alpha := models.Tag{ID: uuid.NewString(), Name: "Alpha", Color: "#123456", Version: 1, CreatedAt: generation.CreatedAt}
		beta := models.Tag{ID: uuid.NewString(), Name: "Beta", Color: "#654321", Version: 1, CreatedAt: generation.CreatedAt}
		if err := store.DB.Create(&[]models.Tag{beta, alpha}).Error; err != nil {
			t.Fatal(err)
		}
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.create","changes":{"title":"Tagged task","tag_ids":[%q,%q]}}`, beta.ID, alpha.ID))
		if !strings.Contains(row.PreviewJSON, `"tag_names":["Alpha","Beta"]`) {
			t.Fatalf("preview=%s", row.PreviewJSON)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE tag_id IN (?, ?)", 2, alpha.ID, beta.ID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title='Tagged task' AND version=1", 1)
	})

	t.Run("update replaces the full tag set and revalidates late changes", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t)
		alpha := models.Tag{ID: uuid.NewString(), Name: "Alpha", Color: "#123456", Version: 1, CreatedAt: generation.CreatedAt}
		beta := models.Tag{ID: uuid.NewString(), Name: "Beta", Color: "#654321", Version: 1, CreatedAt: generation.CreatedAt}
		if err := store.DB.Create(&[]models.Tag{alpha, beta}).Error; err != nil {
			t.Fatal(err)
		}
		task, err := taskFromCreateRequest(createTaskRequest{Title: "Retag task"})
		if err != nil {
			t.Fatal(err)
		}
		task, err = createTaskInTransaction(store.DB, task, []string{alpha.ID}, "test-create")
		if err != nil {
			t.Fatal(err)
		}
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"tag_ids":[%q]}}`, task.ID, beta.ID))
		if !strings.Contains(row.PreviewJSON, `"before":{"tag_names":["Alpha"]}`) || !strings.Contains(row.PreviewJSON, `"after":{"tag_names":["Beta"]}`) {
			t.Fatalf("preview=%s", row.PreviewJSON)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/ai/actions/" + row.ID + "/decision"
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
		if err := store.DB.Delete(&models.Tag{}, "id = ?", beta.ID).Error; err != nil {
			t.Fatal(err)
		}
		assertAPIError(t, performRequest(router, http.MethodPost, path, body, nil), http.StatusUnprocessableEntity, "TAG_NOT_FOUND")
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE task_id=? AND tag_id=?", 1, task.ID, alpha.ID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1", 1, task.ID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	})
}

func TestAIWorkspaceActionTaskHierarchyUsesNativeParentRules(t *testing.T) {
	t.Run("create shows the parent title and persists the relationship", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t)
		parent, err := taskFromCreateRequest(createTaskRequest{Title: "Parent task"})
		if err != nil {
			t.Fatal(err)
		}
		parent, err = createTaskInTransaction(store.DB, parent, nil, "create-parent")
		if err != nil {
			t.Fatal(err)
		}
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.create","changes":{"title":"Child task","parent_task_id":%q}}`, parent.ID))
		if !strings.Contains(row.PreviewJSON, `"parent_task_title":"Parent task"`) || strings.Contains(row.PreviewJSON, parent.ID) {
			t.Fatalf("preview=%s", row.PreviewJSON)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title='Child task' AND parent_task_id=? AND version=1", 1, parent.ID)
	})

	t.Run("update displays both parents and rejects a changed preview", func(t *testing.T) {
		router, store, _, tool, generation := aiActionTestFixture(t)
		parentA, _ := taskFromCreateRequest(createTaskRequest{Title: "Parent A"})
		parentB, _ := taskFromCreateRequest(createTaskRequest{Title: "Parent B"})
		var err error
		parentA, err = createTaskInTransaction(store.DB, parentA, nil, "create-parent-a")
		if err != nil {
			t.Fatal(err)
		}
		parentB, err = createTaskInTransaction(store.DB, parentB, nil, "create-parent-b")
		if err != nil {
			t.Fatal(err)
		}
		child, _ := taskFromCreateRequest(createTaskRequest{Title: "Move child", ParentTaskID: &parentA.ID})
		child, err = createTaskInTransaction(store.DB, child, nil, "create-child")
		if err != nil {
			t.Fatal(err)
		}
		row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"parent_task_id":%q}}`, child.ID, parentB.ID))
		if !strings.Contains(row.PreviewJSON, `"before":{"parent_task_title":"Parent A"}`) || !strings.Contains(row.PreviewJSON, `"after":{"parent_task_title":"Parent B"}`) || strings.Contains(row.PreviewJSON, parentB.ID) {
			t.Fatalf("preview=%s", row.PreviewJSON)
		}
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		if err := store.DB.Model(&models.Task{}).Where("id = ?", parentB.ID).Updates(map[string]any{"title": "Parent B renamed", "version": 2}).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/ai/actions/" + row.ID + "/decision"
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
		assertAPIError(t, performRequest(router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND parent_task_id=? AND version=1", 1, child.ID, parentA.ID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	})

	t.Run("update rejects a descendant as parent", func(t *testing.T) {
		_, store, _, tool, _ := aiActionTestFixture(t)
		root, _ := taskFromCreateRequest(createTaskRequest{Title: "Root"})
		root, err := createTaskInTransaction(store.DB, root, nil, "create-root")
		if err != nil {
			t.Fatal(err)
		}
		child, _ := taskFromCreateRequest(createTaskRequest{Title: "Descendant", ParentTaskID: &root.ID})
		child, err = createTaskInTransaction(store.DB, child, nil, "create-descendant")
		if err != nil {
			t.Fatal(err)
		}
		root, err = loadTask(store.DB, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"parent_task_id":%q}}`, root.ID, root.Version, child.ID)
		if result, err := tool.Execute(context.Background(), []byte(args)); err == nil || !strings.Contains(err.Error(), "TASK_PARENT_CYCLE") {
			t.Fatalf("cycle result=%s err=%v", result, err)
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	})
}

func TestAIWorkspaceActionTaskDeleteUsesNativeConstraintsAndExplicitConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	parent, _ := taskFromCreateRequest(createTaskRequest{Title: "Delete parent"})
	parent, err := createTaskInTransaction(store.DB, parent, nil, "create-delete-parent")
	if err != nil {
		t.Fatal(err)
	}
	child, _ := taskFromCreateRequest(createTaskRequest{Title: "Keep child", ParentTaskID: &parent.ID})
	child, err = createTaskInTransaction(store.DB, child, nil, "create-delete-child")
	if err != nil {
		t.Fatal(err)
	}
	parent, err = loadTask(store.DB, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.delete","task_id":%q,"expected_version":%d,"changes":{}}`, parent.ID, parent.Version))
	if !strings.Contains(row.PreviewJSON, `"task_deleted":true`) ||
		!strings.Contains(row.PreviewJSON, `"child_tasks_moved_to_top_level":1`) ||
		!strings.Contains(row.PreviewJSON, `"agent_runs_deleted":0`) {
		t.Fatalf("preview=%s", row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	withoutConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, http.MethodPost, path, withoutConsent, nil), http.StatusUnprocessableEntity, "TASK_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=?", 1, parent.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)

	withConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_delete":true}`, row.Fingerprint))
	response := performRequest(router, http.MethodPost, path, withConsent, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"confirmed"`) || !strings.Contains(response.Body.String(), `"route":"/tasks"`) {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=?", 0, parent.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND parent_task_id IS NULL", 1, child.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='confirmed' AND result_id=? AND result_version=?", 1, row.ID, parent.ID, parent.Version)

	replay := performRequest(router, http.MethodPost, path, withConsent, nil)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"route":"/tasks"`) {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
}

func TestAIWorkspaceActionTaskDeleteRejectsChangedImpactPreview(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Delete only after an exact impact review"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = createTaskInTransaction(store.DB, task, nil, "create-delete-preview-task")
	if err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.delete","task_id":%q,"expected_version":%d,"changes":{}}`, task.ID, task.Version))
	if !strings.Contains(row.PreviewJSON, `"tag_links_deleted":0`) {
		t.Fatalf("preview=%s", row.PreviewJSON)
	}
	tag := models.Tag{ID: uuid.NewString(), Name: "Late tag", Color: "#123456", Version: 1, CreatedAt: generation.CreatedAt}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate a relationship changing after the proposal without changing the Task row version.
	if err := store.DB.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", task.ID, tag.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_task_delete":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, http.MethodPost, path, body, nil), http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, task.ID, task.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE task_id=? AND tag_id=?", 1, task.ID, tag.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}

func TestAIWorkspaceActionUpdateConflictRejectionAndAtomicRollback(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Before change"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"title":"After change","planned_date":"2026-09-21"}}`, task.ID)
	row := proposeTestAction(t, store, tool, args)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	// Failure recording the decision must roll back the task edit too.
	if err := store.DB.Exec(`CREATE TRIGGER reject_action_event BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if response := performRequest(router, "POST", path, body, nil); response.Code != 500 {
		t.Fatalf("wanted rollback failure: %d", response.Code)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title='Before change' AND version=1", 1, task.ID)
	if err := store.DB.Exec("DROP TRIGGER reject_action_event").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Task{}).Where("id=?", task.ID).Updates(map[string]any{"title": "Changed elsewhere", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "VERSION_CONFLICT")
	for i := 0; i < 2; i++ {
		response := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title='Changed elsewhere' AND planned_date IS NULL", 1, task.ID)
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "AI_ACTION_ALREADY_DECIDED")
}

func TestAIWorkspaceRejectedProposalCannotFallBackToLegacyTaskConfirmation(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Declined task"}}`)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	message := models.AIMessage{ID: uuid.NewString(), SessionID: generation.SessionID, GenerationID: &generation.ID, Role: "assistant", Status: "completed", Content: `[opc:task]{"title":"Declined task"}[/opc:task]`, CreatedAt: generation.CreatedAt, UpdatedAt: generation.CreatedAt}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/messages/"+message.ID+"/task-confirmation", []byte(`{"title":"Declined task"}`), nil), 409, "AI_ACTION_USE_PROPOSAL")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIWorkspaceActionLifecycleAndCapabilityBoundaries(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Lifecycle approval"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		`{"action":"task.delete","changes":{}}`,
		fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"review_policy":null}}`, task.ID),
		fmt.Sprintf(`{"action":"task.start","task_id":%q,"expected_version":1,"changes":{}}`, task.ID), // no assignee
		fmt.Sprintf(`{"action":"task.cancel","task_id":%q,"expected_version":1,"changes":{}}`, task.ID),
		fmt.Sprintf(`{"action":"task.complete","task_id":%q,"expected_version":1,"changes":{"reason":"fake"}}`, task.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.complete","task_id":%q,"expected_version":1,"changes":{}}`, task.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatalf("complete=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='done'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='task_completed'", 1, task.ID)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	for _, persist := range []bool{false, true} {
		grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}
		if !persist {
			grant.Scopes = append(grant.Scopes, "actions")
		}
		registry, err := service.aiChatToolRegistry(generation.SessionID, persist, &provider, grant, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := registry.Get("workspace_propose"); ok {
			t.Fatal("unconsented/ephemeral proposal tool exposed")
		}
		if _, ok := registry.Get("workspace_execute"); ok {
			t.Fatal("execution tool must never be exposed")
		}
	}
}

func TestAIChatWorkspaceProposesWithoutExecutingAndEphemeralRejects(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"task.create","changes":{"title":"工具提议任务"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `请确认下方的任务建议，尚未执行。[opc:task]{"title":"工具提议任务"}[/opc:task][opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "action-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create a task", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls != 2 {
		t.Fatalf("chat=%d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	var assistant models.AIMessage
	if err := store.DB.Where("role = ?", "assistant").First(&assistant).Error; err != nil {
		t.Fatal(err)
	}
	legacyPath := "/api/v1/ai/messages/" + assistant.ID + "/task-confirmation"
	legacyBody := []byte(`{"title":"工具提议任务"}`)
	assertAPIError(t, performRequest(router, "POST", legacyPath, legacyBody, nil), 409, "AI_ACTION_USE_PROPOSAL")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	decision := performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if decision.Code != 200 {
		t.Fatal(decision.Body.String())
	}
	assertAPIError(t, performRequest(router, "POST", legacyPath, legacyBody, nil), 409, "AI_ACTION_USE_PROPOSAL")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&session).Updates(map[string]any{"persist": false, "version": session.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "no saving", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", body, nil), 422, "AI_ACTION_PERSIST_REQUIRED")
	if calls != 2 {
		t.Fatal("ephemeral rejection called upstream")
	}
}

func TestAIWorkspaceActionPlanUpdateResetsOrderAndRetainsReviewPolicy(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	planned, order := "2026-09-18", 3
	reviewPolicy := "manual"
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Replan safely", PlannedDate: &planned, ManualOrder: &order, ReviewPolicy: &reviewPolicy})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":1,"changes":{"planned_date":"2026-09-21"}}`, task.ID))
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"task.complete","task_id":%q,"expected_version":1,"changes":{}}`, task.ID))); err == nil {
		t.Fatal("manual review bypass accepted")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatalf("replan=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND planned_date='2026-09-21' AND manual_order IS NULL AND version=2 AND review_policy='manual' AND status='todo'", 1, task.ID)
}

func TestAIWorkspaceActionUnavailableExpiryLimitAndDecisionPayload(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "completed"} {
		t.Run(status, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Must not execute"}}`)
			if err := store.DB.Model(&generation).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			if status != "completed" {
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 409, "AI_ACTION_NOT_CONFIRMABLE")
			} else {
				created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
				out, err := aiActionOutput(row, status, created.Add(24*time.Hour+time.Second))
				if err != nil || out.Status != "expired" || out.CanConfirm {
					t.Fatalf("expiry=%#v %v", out, err)
				}
				assertAPIError(t, performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","changes":{"title":"Injected"}}`, row.Fingerprint)), nil), 400, "INVALID_JSON")
			}
			response := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
			if response.Code != 200 {
				t.Fatalf("reject=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
		})
	}
	_, store, _, tool, _ := aiActionTestFixture(t)
	for i := 0; i < 8; i++ {
		proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.create","changes":{"title":"Suggestion %d"}}`, i))
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"action":"task.create","changes":{"title":"Ninth suggestion"}}`)); err == nil {
		t.Fatal("proposal limit ignored")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 8)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIProjectNoteActionOutputUsesTheExactAvailableNoteIdentity(t *testing.T) {
	projectID := uuid.NewString()
	targetID := uuid.NewString()
	resultID := uuid.NewString()
	created := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		action    string
		status    string
		targetID  string
		resultID  *string
		wantRoute string
	}{
		{name: "pending create stays at the project", action: "create", status: "pending", wantRoute: searchRoute("project", projectID)},
		{name: "pending update uses its target", action: "update", status: "pending", targetID: targetID, wantRoute: aiProjectNoteRoute(projectID, targetID)},
		{name: "pending delete uses its target", action: "delete", status: "pending", targetID: targetID, wantRoute: aiProjectNoteRoute(projectID, targetID)},
		{name: "confirmed create uses the result", action: "create", status: "confirmed", resultID: &resultID, wantRoute: aiProjectNoteRoute(projectID, resultID)},
		{name: "confirmed update prefers the result", action: "update", status: "confirmed", targetID: targetID, resultID: &resultID, wantRoute: aiProjectNoteRoute(projectID, resultID)},
		{name: "confirmed delete prefers the result", action: "delete", status: "confirmed", targetID: targetID, resultID: &resultID, wantRoute: aiProjectNoteRoute(projectID, resultID)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			action, err := json.Marshal(aiWorkspaceAction{
				Action:        "project_note." + test.action,
				ProjectNoteID: test.targetID,
				Changes:       json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := json.Marshal(aiActionPreview{
				Label:  "Project note",
				Before: map[string]any{},
				After:  map[string]any{"project_id": projectID},
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := aiActionOutput(models.AIActionProposal{
				ID:           uuid.NewString(),
				GenerationID: uuid.NewString(),
				Fingerprint:  strings.Repeat("a", 64),
				ActionJSON:   string(action),
				PreviewJSON:  string(preview),
				Status:       test.status,
				ResultID:     test.resultID,
				CreatedAt:    created.Format(time.RFC3339Nano),
			}, "completed", created)
			if err != nil {
				t.Fatal(err)
			}
			if out.Route != test.wantRoute {
				t.Fatalf("route=%q want %q", out.Route, test.wantRoute)
			}
		})
	}
}
