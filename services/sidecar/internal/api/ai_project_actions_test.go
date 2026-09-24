package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIProjectActionBoundaries(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	p := createProjectForTest(t, router, `{"name":"Boundary project","start_date":"2026-09-20"}`, nil)
	for _, args := range []string{
		`{"action":"project.create","changes":{"name":"Money test","amount_minor":500}}`,
		`{"action":"project.create","changes":{"name":"Status test","status":"completed"}}`,
		`{"action":"project.create","changes":{"name":"No client grant","client_id":null}}`,
		`{"action":"project.create","changes":{"name":"Bad dates","start_date":"2026-09-30","due_date":"2026-09-20"}}`,
		fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":1,"changes":{"name":null}}`, p.ID),
		fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":1,"changes":{"due_date":"2026-09-19"}}`, p.ID),
		fmt.Sprintf(`{"action":"project.complete","project_id":%q,"expected_version":1,"changes":{"confirm_incomplete_tasks":true}}`, p.ID),
		fmt.Sprintf(`{"action":"project.start","task_id":%q,"expected_version":1,"changes":{}}`, p.ID),
		fmt.Sprintf(`{"action":"task.complete","project_id":%q,"task_id":%q,"expected_version":1,"changes":{}}`, p.ID, p.ID),
		fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":99,"changes":{"name":"Stale test"}}`, p.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIProjectDeleteRequiresConsentAndRejectsChangedImpact(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Delete project"}`, nil)
	archived := transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"archive"}`)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.delete","project_id":%q,"expected_version":%d,"changes":{}}`, archived.ID, archived.Version))
	if !strings.Contains(row.PreviewJSON, `"project_deleted":true`) || !strings.Contains(row.PreviewJSON, `"project_tasks_detached":0`) {
		t.Fatalf("delete preview=%s", row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	missingConsent := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	assertAPIError(t, missingConsent, http.StatusUnprocessableEntity, "PROJECT_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 1, project.ID)

	task, err := taskFromCreateRequest(createTaskRequest{Title: "Late project task", ProjectID: &project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	changed := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_project_delete":true}`, row.Fingerprint)), nil)
	assertAPIError(t, changed, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 1, project.ID)
}

func TestAIProjectDeleteDetachesTasksAfterExplicitConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Delete project"}`, nil)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Retained project task", ProjectID: &project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	var current models.Project
	if err := store.DB.First(&current, "id=?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	archived := transitionProjectForTest(t, router, project.ID, current.Version, `{"action":"archive"}`)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.delete","project_id":%q,"expected_version":%d,"changes":{}}`, archived.ID, archived.Version))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_project_delete":true}`, row.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(confirmed.Body.Bytes(), &output); err != nil || output.Data.ResultID == nil || *output.Data.ResultID != project.ID || output.Data.Route != "/projects" {
		t.Fatalf("result=%s err=%v", confirmed.Body.String(), err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 0, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND project_id IS NULL AND version=2", 1, task.ID)
}

func TestAIProjectDeleteRestoresAttachmentWhenApprovalRollsBack(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Delete attachment project"}`, nil)
	upload := performMultipartPartsRequest(router, "/api/v1/projects/"+project.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"evidence.txt"}`)},
		{field: "file", filename: "evidence.txt", content: []byte("retained after rollback")},
	}, map[string]string{"If-Match": `"1"`}, false)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload=%d %s", upload.Code, upload.Body.String())
	}
	attachment := decodeProjectAttachmentResponse(t, upload.Body.Bytes())
	archived := transitionProjectForTest(t, router, project.ID, attachment.ProjectVersion, `{"action":"archive"}`)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.delete","project_id":%q,"expected_version":%d,"changes":{}}`, archived.ID, archived.Version))
	if !strings.Contains(row.PreviewJSON, `"project_attachments_deleted":1`) || !strings.Contains(row.PreviewJSON, `"project_attachment_files_deleted":1`) {
		t.Fatalf("delete preview=%s", row.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`CREATE TRIGGER reject_project_delete_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_project_delete":true}`, row.Fingerprint))
	if response := performRequest(router, http.MethodPost, path, body, nil); response.Code != http.StatusInternalServerError {
		t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 1, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM project_attachments WHERE id=?", 1, attachment.ID)
	download := performRequest(router, http.MethodGet, "/api/v1/project-attachments/"+attachment.ID+"/content", nil, nil)
	if download.Code != http.StatusOK || download.Body.String() != "retained after rollback" {
		t.Fatalf("restored attachment=%d %q", download.Code, download.Body.String())
	}
	if err := store.DB.Exec("DROP TRIGGER reject_project_delete_approval").Error; err != nil {
		t.Fatal(err)
	}
	if response := performRequest(router, http.MethodPost, path, body, nil); response.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 0, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM project_attachments WHERE id=?", 0, attachment.ID)
}

func TestAIProjectLifecycleApprovals(t *testing.T) {
	for _, c := range []struct{ action, from, to string }{
		{"start", "planning", "in_progress"}, {"pause", "in_progress", "paused"}, {"resume", "paused", "in_progress"},
		{"complete", "in_progress", "completed"}, {"reopen", "completed", "in_progress"}, {"archive", "paused", "archived"}, {"restore", "archived", "paused"},
	} {
		t.Run(c.action, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			p := createProjectForTest(t, router, `{"name":"Lifecycle project"}`, nil)
			changes := map[string]any{"status": c.from}
			if c.from == "archived" {
				changes["archived_from_status"] = "paused"
			}
			if err := store.DB.Model(&models.Project{}).Where("id=?", p.ID).Updates(changes).Error; err != nil {
				t.Fatal(err)
			}
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.%s","project_id":%q,"expected_version":1,"changes":{}}`, c.action, p.ID))
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND status=?", 1, p.ID, c.from)
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			if response.Code != 200 {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND status=? AND version=2", 1, p.ID, c.to)
		})
	}
}

func TestAIProjectCompleteRequiresHumanConsentAndKeepsTasks(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	rule := enableProjectCompletionAutomationForDeliveryTest(t, router)
	client := createClientForTest(t, router, `{"name":"Project client"}`, nil)
	p := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Completion project","client_id":%q}`, client.ID), nil)
	p = transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"start"}`)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Still unfinished", ProjectID: &p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	var current models.Project
	if err = store.DB.First(&current, "id=?", p.ID).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.complete","project_id":%q,"expected_version":%d,"changes":{}}`, p.ID, current.Version))
	var preview aiActionPreview
	if err = json.Unmarshal([]byte(row.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.After["incomplete_task_count"] != float64(1) {
		t.Fatalf("preview=%s", row.PreviewJSON)
	}
	if err = store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	assertAPIError(t, performRequest(router, "POST", path, body, nil), 409, "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED")
	// A failed downstream projection must roll back project, event and approval.
	if err = store.DB.Exec(`CREATE TRIGGER reject_project_completion BEFORE INSERT ON inbox_items WHEN NEW.source_entity_type='project_completion' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	consent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_incomplete_tasks":true}`, row.Fingerprint))
	if r := performRequest(router, "POST", path, consent, nil); r.Code != 500 {
		t.Fatalf("rollback=%d %s", r.Code, r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND status='in_progress' AND version=?", 1, p.ID, current.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities", 0)
	if err = store.DB.Exec("DROP TRIGGER reject_project_completion").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, consent, nil); r.Code != 200 {
		t.Fatalf("complete=%d %s", r.Code, r.Body.String())
	}
	// Replay does not need a new checkbox and cannot create another completion.
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo'", 1, task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='project_completion'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_activities WHERE client_id=? AND kind='system_reference'", 1, client.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='project_completed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND status='succeeded'", 1, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='automation'", 1)
}

func TestAIProjectChatUsesRealToolLoopAndReturnsProjectReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 1 {
			if !strings.Contains(string(encoded), `"workspace_guide"`) || strings.Contains(string(encoded), `"project.create"`) {
				t.Error("project schema loaded before domain guidance")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "project-proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"project.create","changes":{"name":"Through harness"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 2 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("no real proposal tool response")
		}
		if calls == 3 {
			if !strings.Contains(string(encoded), "/projects/") || !strings.Contains(string(encoded), `\"confirmed\"`) {
				t.Error("missing project receipt")
			}
			toolsJSON, _ := json.Marshal(body["tools"])
			if strings.Contains(string(toolsJSON), "workspace_propose") {
				t.Error("previous permission inherited")
			}
		}
		streamMockAIDelta(w, `请查看下方的操作状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "project-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create a project", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls != 2 {
		t.Fatalf("chat %d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects", 0)
	var row models.AIActionProposal
	if err := store.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "Did it succeed?"})
	response = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || calls != 3 {
		t.Fatalf("followup %d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects", 1)
}

func TestAIProjectEditRefreshesTasksAndDetectsStalePreview(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	p := createProjectForTest(t, router, `{"name":"Old project"}`, nil)
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Linked task", ProjectID: &p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	var current models.Project
	if err = store.DB.First(&current, "id=?", p.ID).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":%d,"changes":{"name":"New project","due_date":"2026-10-01"}}`, p.ID, current.Version)
	row := proposeTestAction(t, store, tool, args)
	stale := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.start","project_id":%q,"expected_version":%d,"changes":{}}`, p.ID, current.Version))
	if err = store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	if err = store.DB.Exec(`CREATE TRIGGER reject_project_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test'); END`).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	if r := performRequest(router, "POST", path, body, nil); r.Code != 500 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND name='Old project' AND due_date IS NULL", 1, p.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, task.ID, task.Version)
	if err = store.DB.Exec("DROP TRIGGER reject_project_approval").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", path, body, nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND name='New project' AND due_date='2026-10-01'", 1, p.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version>?", 1, task.ID, task.Version)
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+stale.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, stale.Fingerprint)), nil), 409, "VERSION_CONFLICT")
}

func TestAIProjectClientAssociationRequiresScopeAndRetainsInvoiceGuard(t *testing.T) {
	router, store, _, baseTool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Bound client"}`, nil)
	tool := *baseTool.(*aiWorkspaceTool)
	tool.policy = harness.NewCapabilities("work", "actions", "clients")
	row := proposeTestAction(t, store, &tool, fmt.Sprintf(`{"action":"project.create","changes":{"name":"Client proposal","client_id":%q}}`, client.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var p models.Project
	if err := store.DB.First(&p, "client_id=?", client.ID).Error; err != nil {
		t.Fatal(err)
	}
	createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"project_id":%q,"amount_minor":100,"currency":"CNY","issue_date":"2026-09-18","due_date":"2026-10-18"}`, client.ID, p.ID), nil)
	if err := store.DB.First(&p, "id=?", p.ID).Error; err != nil {
		t.Fatal(err)
	}
	// A new generation owns the next proposal.
	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.generationID = generation.ID
	row = proposeTestAction(t, store, &tool, fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":%d,"changes":{"client_id":null}}`, p.ID, p.Version))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil), 409, "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND client_id=?", 1, p.ID, client.ID)
}

func TestAIProjectApprovalSnapshotCannotBroadenConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Private association"}`, nil)
	p := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Snapshot project","client_id":%q,"amount_minor":987654321,"description":"Unchanged private body"}`, client.ID), nil)
	p = transitionProjectForTest(t, router, p.ID, p.Version, `{"action":"start"}`)
	edit := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.update","project_id":%q,"expected_version":%d,"changes":{"name":"Renamed project"}}`, p.ID, p.Version))
	for _, private := range []string{client.ID, "client_id", "amount_minor", "987654321", "Unchanged private body"} {
		if strings.Contains(edit.PreviewJSON, private) {
			t.Fatalf("unrelated field leaked: %s", edit.PreviewJSON)
		}
	}
	completion := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project.complete","project_id":%q,"expected_version":%d,"changes":{}}`, p.ID, p.Version))
	// Work added after preview changes the aggregate version and invalidates the
	// entire consent snapshot, even when the human supplies the extra checkbox.
	task, err := taskFromCreateRequest(createTaskRequest{Title: "Added after preview", ProjectID: &p.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err = store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+completion.ID+"/decision",
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_incomplete_tasks":true}`, completion.Fingerprint)), nil), 409, "VERSION_CONFLICT")
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/actions/"+edit.ID+"/decision",
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_incomplete_tasks":true}`, edit.Fingerprint)), nil), 422, "AI_ACTION_DECISION_INVALID")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND status='in_progress' AND name='Snapshot project'", 1, p.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE generation_id=? AND status='pending'", 2, generation.ID)
}

func TestAIProjectCreateApproval(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"project.create","changes":{"name":"审批项目","start_date":"2026-09-20","due_date":"2026-09-25","color":"#abcdef"}}`)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
	for i := 0; i < 2; i++ {
		response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "/projects/") {
			t.Fatalf("confirm: %d %s", response.Code, response.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE name=? AND status='planning' AND color='#ABCDEF'", 1, "审批项目")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='project_created'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
	receipts, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || !strings.Contains(receipts, "/projects/") || strings.Contains(receipts, "审批项目") {
		t.Fatalf("receipts: %s %v", receipts, err)
	}
	var result models.AIActionProposal
	if err := store.DB.First(&result, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if result.ResultID == nil {
		t.Fatal("missing result")
	}
}
