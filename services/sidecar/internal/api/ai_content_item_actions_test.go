package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIContentItemActionBoundaries(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Boundary item","platform":"website"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	item := decodeContentItemResponse(t, created.Body.Bytes())
	for _, args := range []string{
		`{"action":"content_item.create","changes":{"title":"Missing platform"}}`,
		`{"action":"content_item.create","changes":{"title":"Published","platform":"website","status":"published"}}`,
		`{"action":"content_item.create","changes":{"title":"Bad schedule","platform":"website","scheduled_at":"2026-09-20T10:00:00Z","scheduled_timezone":"UTC"}}`,
		fmt.Sprintf(`{"action":"content_item.update","content_item_id":%q,"expected_version":1,"changes":{}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.update","content_item_id":%q,"expected_version":1,"changes":{"status":"published"}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.schedule","content_item_id":%q,"expected_version":1,"changes":{"scheduled_at":"2026-09-20T10:00:00Z"}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.archive","content_item_id":%q,"expected_version":1,"changes":{"reason":"model choice"}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.link_task","content_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"is_required":true}}`, item.ID, uuid.NewString()),
		fmt.Sprintf(`{"action":"content_item.set_task_required","content_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1}}`, item.ID, uuid.NewString()),
		fmt.Sprintf(`{"action":"content_item.unlink_task","content_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1,"is_required":false}}`, item.ID, uuid.NewString()),
		fmt.Sprintf(`{"action":"content_item.link_task","content_item_id":%q,"task_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1,"is_required":true}}`, item.ID, uuid.NewString(), uuid.NewString()),
		fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":1,"changes":{"status":"published"}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":1,"changes":{"published_at":null}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":1,"changes":{"published_at":"tomorrow"}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":1,"changes":{"external_link":""}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.delete","content_item_id":%q,"expected_version":1,"changes":{}}`, item.ID),
		fmt.Sprintf(`{"action":"content_item.update","content_item_id":%q,"roadmap_milestone_id":%q,"expected_version":1,"changes":{"title":"Mixed targets"}}`, item.ID, item.ID),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIContentPublishNeedsPersonalConsentAndUsesNativeTransaction(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"发布说明","platform":"website"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	item := decodeContentItemResponse(t, created.Body.Bytes())
	scheduled := performRequest(router, http.MethodPut, "/api/v1/content-items/"+item.ID+"/schedule", []byte(`{"scheduled_at":"2026-09-18T11:59:00Z","scheduled_timezone":"UTC"}`), map[string]string{"If-Match": `"1"`})
	if scheduled.Code != http.StatusOK {
		t.Fatalf("schedule=%d %s", scheduled.Code, scheduled.Body.String())
	}
	if err := service.projectDueContentItems(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item' AND source_entity_id=? AND status='open'", 1, item.ID)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":2,"changes":{"published_at":"2026-09-18T20:00:00+08:00","external_link":" https://example.test/post "}}`, item.ID))
	if !strings.Contains(proposal.PreviewJSON, `"published_at":"2026-09-18T12:00:00Z"`) || !strings.Contains(proposal.PreviewJSON, `"external_link":"https://example.test/post"`) || !strings.Contains(proposal.PreviewJSON, `"status":"published"`) {
		t.Fatalf("publish preview=%s", proposal.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='scheduled' AND published_at IS NULL", 1, item.ID)
	finishAIGeneration(t, store, generation)
	path := "/api/v1/ai/actions/" + proposal.ID + "/decision"
	missing := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, missing, http.StatusUnprocessableEntity, "CONTENT_PUBLISHED_CONFIRMATION_REQUIRED")
	falseConsent := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_published":false}`, proposal.Fingerprint)), nil)
	assertAPIError(t, falseConsent, http.StatusUnprocessableEntity, "CONTENT_PUBLISHED_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='scheduled'", 1, item.ID)
	if err := store.DB.Exec(`CREATE TRIGGER fail_content_publish_confirmation BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type = 'ai_action_proposal' AND NEW.action = 'ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_CONFIRMATION_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_published":true}`, proposal.Fingerprint)), nil)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced failure=%d %s", failed.Code, failed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='scheduled' AND version=2 AND published_at IS NULL", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item' AND source_entity_id=? AND status='open'", 1, item.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_content_publish_confirmation").Error; err != nil {
		t.Fatal(err)
	}
	confirmed := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_published":true}`, proposal.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), "/content-calendar?item="+item.ID) {
		t.Fatalf("publish confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='published' AND version=3 AND published_at='2026-09-18T12:00:00Z' AND external_link='https://example.test/post'", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item' AND source_entity_id=? AND status='resolved'", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIContentPublishRejectsStaleVersionAndDefaultsToConfirmationTime(t *testing.T) {
	clock := time.Date(2026, 9, 21, 16, 30, 0, 0, time.UTC)
	router, store, _, tool, generation := aiActionTestFixture(t, clock)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Release note","platform":"website"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	item := decodeContentItemResponse(t, created.Body.Bytes())
	stale := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":1,"changes":{}}`, item.ID))
	if !strings.Contains(stale.PreviewJSON, `"published_at":"confirmation_time"`) {
		t.Fatalf("publish preview=%s", stale.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	changed := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+item.ID, []byte(`{"title":"Release note updated"}`), map[string]string{"If-Match": `"1"`})
	if changed.Code != http.StatusOK {
		t.Fatalf("change=%d %s", changed.Code, changed.Body.String())
	}
	path := "/api/v1/ai/actions/" + stale.ID + "/decision"
	conflict := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_published":true}`, stale.Fingerprint)), nil)
	assertAPIError(t, conflict, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='draft' AND version=2 AND published_at IS NULL", 1, item.ID)

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	fresh := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.publish","content_item_id":%q,"expected_version":2,"changes":{}}`, item.ID))
	finishAIGeneration(t, store, generation)
	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+fresh.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_published":true}`, fresh.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='published' AND version=3 AND published_at=?", 1, item.ID, clock.Format(time.RFC3339Nano))
}

func TestAIContentItemTaskRelationsRequireExactPreviewAndConfirmation(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Campaign checklist","platform":"website"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create Content Item=%d %s", created.Code, created.Body.String())
	}
	item := decodeContentItemResponse(t, created.Body.Bytes())
	taskRecorder := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"Prepare campaign assets"}`), nil)
	if taskRecorder.Code != http.StatusCreated {
		t.Fatalf("create Task=%d %s", taskRecorder.Code, taskRecorder.Body.String())
	}
	var task struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}

	nextGeneration := func() {
		generation.ID = uuid.NewString()
		generation.Status = "streaming"
		if err := store.DB.Create(&generation).Error; err != nil {
			t.Fatal(err)
		}
		tool.(*aiWorkspaceTool).generationID = generation.ID
	}
	completeGeneration := func() {
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
	}
	confirm := func(proposal models.AIActionProposal) *httptest.ResponseRecorder {
		return performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	}

	link := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.link_task","content_item_id":%q,"expected_version":1,"changes":{"task_id":%q,"expected_task_version":1,"is_required":true}}`, item.ID, task.Data.ID))
	if !strings.Contains(link.PreviewJSON, `"relation_state":"unlinked"`) ||
		!strings.Contains(link.PreviewJSON, `"task_title":"Prepare campaign assets"`) ||
		!strings.Contains(link.PreviewJSON, `"content_item_version":2`) {
		t.Fatalf("link preview=%s", link.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=?", 0, item.ID)
	completeGeneration()
	linked := confirm(link)
	if linked.Code != http.StatusOK || !strings.Contains(linked.Body.String(), "/content-calendar?item=") {
		t.Fatalf("confirm link=%d %s", linked.Code, linked.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=? AND task_id=? AND is_required=1", 1, item.ID, task.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=2", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND status='todo'", 1, task.Data.ID)

	nextGeneration()
	if _, err := tool.Execute(t.Context(), []byte(fmt.Sprintf(`{"action":"content_item.link_task","content_item_id":%q,"expected_version":2,"changes":{"task_id":%q,"expected_task_version":1,"is_required":true}}`, item.ID, task.Data.ID))); err == nil {
		t.Fatal("accepted an already-linked Task as a new Content Item relation")
	}
	setOptional := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.set_task_required","content_item_id":%q,"expected_version":2,"changes":{"task_id":%q,"expected_task_version":1,"is_required":false}}`, item.ID, task.Data.ID))
	if !strings.Contains(setOptional.PreviewJSON, `"is_required":true`) || !strings.Contains(setOptional.PreviewJSON, `"content_item_version":3`) {
		t.Fatalf("requirement preview=%s", setOptional.PreviewJSON)
	}
	completeGeneration()
	if err := store.DB.Exec(`CREATE TRIGGER fail_content_item_relation_confirmation BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type = 'ai_action_proposal' AND NEW.action = 'ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_CONFIRMATION_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := confirm(setOptional)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced failure=%d %s", failed.Code, failed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=? AND task_id=? AND is_required=1", 1, item.ID, task.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=2", 1, item.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_content_item_relation_confirmation").Error; err != nil {
		t.Fatal(err)
	}
	changed := confirm(setOptional)
	if changed.Code != http.StatusOK {
		t.Fatalf("confirm requirement=%d %s", changed.Code, changed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=? AND task_id=? AND is_required=0", 1, item.ID, task.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=3", 1, item.ID)

	nextGeneration()
	unlink := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.unlink_task","content_item_id":%q,"expected_version":3,"changes":{"task_id":%q,"expected_task_version":1}}`, item.ID, task.Data.ID))
	if !strings.Contains(unlink.PreviewJSON, `"relation_state":"unlinked"`) || !strings.Contains(unlink.PreviewJSON, `"content_item_version":4`) {
		t.Fatalf("unlink preview=%s", unlink.PreviewJSON)
	}
	completeGeneration()
	unlinked := confirm(unlink)
	if unlinked.Code != http.StatusOK {
		t.Fatalf("confirm unlink=%d %s", unlinked.Code, unlinked.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=?", 0, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=4", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1 AND status='todo'", 1, task.Data.ID)
}

func TestAIContentItemCreateReadUpdateAndScheduleRequireConfirmation(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Editorial project"}`, nil)
	create := proposeTestAction(t, store, tool, fmt.Sprintf(`{
		"action":"content_item.create",
		"changes":{"title":"Launch article","platform":"website","status":"scheduled","scheduled_at":"2026-09-20T10:00:00-07:00","scheduled_timezone":"America/Tijuana","project_id":%q,"notes":"Review copy","external_link":"https://example.test/draft"}
	}`, project.ID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE title='Launch article'", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+create.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, create.Fingerprint)), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/content-calendar?item=") {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal, "id = ?", create.ID).Error; err != nil || proposal.ResultID == nil {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='scheduled' AND project_id=? AND version=1", 1, *proposal.ResultID, project.ID)

	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	get, ok := registry.Get("workspace_get")
	if !ok {
		t.Fatal("workspace_get missing")
	}
	read, err := get.Execute(t.Context(), []byte(fmt.Sprintf(`{"type":"content_item","id":%q}`, *proposal.ResultID)))
	if err != nil || !strings.Contains(read, `"platform":"website"`) || !strings.Contains(read, `"scheduled_timezone":"America/Tijuana"`) || !strings.Contains(read, `"external_link_policy":"untrusted_text_not_fetched"`) || !strings.Contains(read, `"required_task_total":0`) {
		t.Fatalf("workspace_get=%s err=%v", read, err)
	}

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	update := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.update","content_item_id":%q,"expected_version":1,"changes":{"title":"Launch article final","notes":null}}`, *proposal.ResultID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND title='Launch article' AND version=1", 1, *proposal.ResultID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+update.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, update.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND title='Launch article final' AND notes IS NULL AND version=2", 1, *proposal.ResultID)

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	unschedule := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.unschedule","content_item_id":%q,"expected_version":2,"changes":{}}`, *proposal.ResultID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+unschedule.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, unschedule.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='draft' AND scheduled_at IS NULL AND scheduled_timezone IS NULL AND version=3", 1, *proposal.ResultID)
}

func TestAIContentItemApprovalRejectsChangedPreviewAndSupportsLifecycle(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Lifecycle content","platform":"newsletter"}`), nil)
	item := decodeContentItemResponse(t, created.Body.Bytes())
	review := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.review","content_item_id":%q,"expected_version":1,"changes":{}}`, item.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.ContentItem{}).Where("id=?", item.ID).Updates(map[string]any{"platform": "changed-outside-preview", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+review.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, review.Fingerprint)), nil), http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status='draft' AND version=2", 1, item.ID)

	// Continue from the newer record with fresh proposals and normal optimistic
	// concurrency; the stale proposal above remains permanently non-executable.
	version := int64(2)
	for _, action := range []struct {
		name, want string
	}{
		{"content_item.review", "in_review"},
		{"content_item.cancel", "cancelled"},
		{"content_item.reopen", "draft"},
		{"content_item.archive", "archived"},
		{"content_item.restore", "draft"},
	} {
		generation.ID = uuid.NewString()
		generation.Status = "streaming"
		if err := store.DB.Create(&generation).Error; err != nil {
			t.Fatal(err)
		}
		tool.(*aiWorkspaceTool).generationID = generation.ID
		proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":%q,"content_item_id":%q,"expected_version":%d,"changes":{}}`, action.name, item.ID, version))
		if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("%s=%d %s", action.name, response.Code, response.Body.String())
		}
		version++
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND status=? AND version=?", 1, item.ID, action.want, version)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND archived_from_status IS NULL", 1, item.ID)
}

func TestAIContentItemDeleteRequiresExactPreviewAndSeparateConsent(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	taskRecorder := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"Prepare deletion example"}`), nil)
	if taskRecorder.Code != http.StatusCreated {
		t.Fatalf("create Task=%d %s", taskRecorder.Code, taskRecorder.Body.String())
	}
	var task struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Archived campaign","platform":"website"}`), nil)
	item := decodeContentItemResponse(t, created.Body.Bytes())
	linked := performRequest(router, http.MethodPost, "/api/v1/content-items/"+item.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":true}`, task.Data.ID)), map[string]string{"If-Match": `"1"`})
	if linked.Code != http.StatusCreated {
		t.Fatalf("link Task=%d %s", linked.Code, linked.Body.String())
	}
	scheduled := performRequest(router, http.MethodPut, "/api/v1/content-items/"+item.ID+"/schedule", []byte(`{"scheduled_at":"2026-09-18T11:59:00Z","scheduled_timezone":"UTC"}`), map[string]string{"If-Match": `"2"`})
	if scheduled.Code != http.StatusOK {
		t.Fatalf("schedule Content Item=%d %s", scheduled.Code, scheduled.Body.String())
	}
	if err := service.projectDueContentItems(t.Context()); err != nil {
		t.Fatal(err)
	}
	archived := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+item.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": `"3"`})
	if archived.Code != http.StatusOK {
		t.Fatalf("archive Content Item=%d %s", archived.Code, archived.Body.String())
	}

	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"content_item.delete","content_item_id":%q,"expected_version":4,"changes":{}}`, item.ID))
	if !strings.Contains(proposal.PreviewJSON, `"content_item_deleted":true`) ||
		!strings.Contains(proposal.PreviewJSON, `"content_item_task_links_deleted":1`) ||
		!strings.Contains(proposal.PreviewJSON, `"content_item_inbox_sources_marked":1`) {
		t.Fatalf("delete preview=%s", proposal.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	missingConsent := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, missingConsent, http.StatusUnprocessableEntity, "CONTENT_ITEM_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=?", 1, item.ID)

	if err := store.DB.Exec(`CREATE TRIGGER fail_content_item_ai_confirmation BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type = 'ai_action_proposal' AND NEW.action = 'ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_CONFIRMATION_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_item_delete":true}`, proposal.Fingerprint)), nil)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced failure=%d %s", failed.Code, failed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=?", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=?", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item' AND source_entity_id=? AND source_deleted_at IS NULL", 1, item.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_content_item_ai_confirmation").Error; err != nil {
		t.Fatal(err)
	}

	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_content_item_delete":true}`, proposal.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"route":"/content-calendar"`) {
		t.Fatalf("confirm deletion=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=?", 0, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=?", 0, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=?", 1, task.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item' AND source_entity_id=? AND source_deleted_at IS NOT NULL", 1, item.ID)
}

func TestAIContentItemChatUsesProposalToolAndNeverPublishesDirectly(t *testing.T) {
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
			if !strings.Contains(string(encoded), "workspace_guide") || strings.Contains(string(encoded), `"content_item.create"`) || strings.Contains(string(encoded), `"workspace_propose"`) {
				t.Error("content item proposal schema loaded before domain guidance")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "content-proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"content_item.create","changes":{"title":"Harness content","platform":"website"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if !strings.Contains(string(encoded), "NOT executed") {
			t.Error("proposal result did not preserve confirmation boundary")
		}
		streamMockAIDelta(w, `请核对下方内容日历操作卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "content-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create content", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 {
		t.Fatalf("chat=%d calls=%d %s", response.Code, calls, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE title='Harness content'", 0)
}
