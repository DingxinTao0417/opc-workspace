package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func newProjectTestAPI(t *testing.T) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "projects-api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store
}

func createProjectForTest(t *testing.T, router http.Handler, body string, headers map[string]string) projectResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create project status = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeProjectResponse(t, recorder.Body.Bytes())
}

func transitionProjectForTest(t *testing.T, router http.Handler, id string, version int64, body string) projectResponse {
	t.Helper()
	recorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+id+"/transitions",
		[]byte(body),
		map[string]string{"If-Match": fmt.Sprintf("\"%d\"", version)},
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("transition project status = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeProjectResponse(t, recorder.Body.Bytes())
}

func decodeProjectResponse(t *testing.T, body []byte) projectResponse {
	t.Helper()
	var envelope struct {
		Data projectResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project response: %v", err)
	}
	return envelope.Data
}

func responseErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var response errorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return response.Code
}

func decodeProjectEvents(t *testing.T, body []byte) ([]projectWorkflowEventOutput, projectWorkflowEventMeta) {
	t.Helper()
	var envelope struct {
		Data []projectWorkflowEventOutput `json:"data"`
		Meta projectWorkflowEventMeta     `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project events: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func insertTestClient(t *testing.T, store *database.Store, id, name string) {
	t.Helper()
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, id, name).Error; err != nil {
		t.Fatalf("insert client: %v", err)
	}
}

func TestProjectCreateListDetailAndTaskProjectName(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clientID := uuid.NewString()
	insertTestClient(t, store, clientID, "星河工作室")
	body := fmt.Sprintf(`{
		"name":"  官网改版  ",
		"description":"重新设计转化路径",
		"client_id":%q,
		"start_date":"2026-08-28",
		"due_date":"2026-09-30",
		"amount_minor":2400000,
		"color":"#5e6ad2"
	}`, clientID)
	headers := map[string]string{"Idempotency-Key": "project-create-1"}
	recorder := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", recorder.Code, recorder.Body.String())
	}
	created := decodeProjectResponse(t, recorder.Body.Bytes())
	if created.Name != "官网改版" || created.Status != "planning" || created.Version != 1 {
		t.Fatalf("created project = %#v", created)
	}
	if created.ClientName == nil || *created.ClientName != "星河工作室" {
		t.Fatalf("client name = %#v", created.ClientName)
	}
	if created.Color == nil || *created.Color != "#5E6AD2" {
		t.Fatalf("normalized color = %#v", created.Color)
	}
	if recorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create ETag = %q", recorder.Header().Get("ETag"))
	}
	if got := strings.Join(created.AvailableActions, ","); got != "start,archive" {
		t.Fatalf("available actions = %q", got)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(body), headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay status=%d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	if replayedProject := decodeProjectResponse(t, replayed.Body.Bytes()); replayedProject.ID != created.ID {
		t.Fatalf("replayed project id = %q, want %q", replayedProject.ID, created.ID)
	}
	conflictingReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects",
		[]byte(`{"name":"另一项目"}`),
		headers,
	)
	if conflictingReplay.Code != http.StatusConflict || responseErrorCode(t, conflictingReplay.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflicting replay = %d: %s", conflictingReplay.Code, conflictingReplay.Body.String())
	}

	taskCreate := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(fmt.Sprintf(`{"title":"完成首页评审","project_id":%q}`, created.ID)),
		nil,
	)
	if taskCreate.Code != http.StatusCreated {
		t.Fatalf("create project task = %d: %s", taskCreate.Code, taskCreate.Body.String())
	}
	var taskEnvelope struct {
		Data struct {
			ID          string  `json:"id"`
			ProjectName *string `json:"project_name"`
			Version     int64   `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(taskCreate.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("decode task create: %v", err)
	}
	if err := store.DB.Table("tasks").Where("id = ?", taskEnvelope.Data.ID).
		Update("actual_minutes", 135).Error; err != nil {
		t.Fatalf("set project task actual minutes: %v", err)
	}
	if taskEnvelope.Data.ProjectName == nil || *taskEnvelope.Data.ProjectName != created.Name {
		t.Fatalf("created task project name = %#v", taskEnvelope.Data.ProjectName)
	}
	// List/detail resolve the current project name through the same database join.
	taskList := performRequest(router, http.MethodGet, "/api/v1/tasks?project_id="+created.ID, nil, nil)
	if taskList.Code != http.StatusOK {
		t.Fatalf("list project tasks = %d: %s", taskList.Code, taskList.Body.String())
	}
	var listed struct {
		Data []struct {
			ID          string  `json:"id"`
			ProjectName *string `json:"project_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(taskList.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ProjectName == nil || *listed.Data[0].ProjectName != created.Name {
		t.Fatalf("task list project name = %s", taskList.Body.String())
	}
	taskDetail := performRequest(router, http.MethodGet, "/api/v1/tasks/"+taskEnvelope.Data.ID, nil, nil)
	if taskDetail.Code != http.StatusOK || !strings.Contains(taskDetail.Body.String(), `"project_name":"官网改版"`) {
		t.Fatalf("task detail project name = %d: %s", taskDetail.Code, taskDetail.Body.String())
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/projects/"+created.ID, nil, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("project detail = %d: %s", detail.Code, detail.Body.String())
	}
	withTask := decodeProjectResponse(t, detail.Body.Bytes())
	if withTask.TaskSummary.Total != 1 || withTask.TaskSummary.Remaining != 1 ||
		withTask.TaskSummary.ProgressPercent != 0 || withTask.TaskSummary.ActualMinutes != 135 {
		t.Fatalf("task summary before completion = %#v", withTask.TaskSummary)
	}

	taskDone := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, taskEnvelope.Data.Version)},
	)
	if taskDone.Code != http.StatusOK {
		t.Fatalf("complete task = %d: %s", taskDone.Code, taskDone.Body.String())
	}
	detail = performRequest(router, http.MethodGet, "/api/v1/projects/"+created.ID, nil, nil)
	completed := decodeProjectResponse(t, detail.Body.Bytes())
	if completed.TaskSummary.Completed != 1 || completed.TaskSummary.Remaining != 0 || completed.TaskSummary.ProgressPercent != 100 {
		t.Fatalf("task summary after completion = %#v", completed.TaskSummary)
	}

	list := performRequest(router, http.MethodGet, "/api/v1/projects?q=%E5%AE%98%E7%BD%91&status=planning&sort=name", nil, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), created.ID) {
		t.Fatalf("project list = %d: %s", list.Code, list.Body.String())
	}

	archivedProject := transitionProjectForTest(
		t, router, created.ID, completed.Version, `{"action":"archive"}`,
	)
	if archivedProject.Status != "archived" {
		t.Fatalf("archive client project = %#v", archivedProject)
	}
	normalizedClientID := strings.ToUpper(clientID)
	defaultClientList := performRequest(
		router, http.MethodGet, "/api/v1/projects?client_id="+normalizedClientID, nil, nil,
	)
	if defaultClientList.Code != http.StatusOK || strings.Contains(defaultClientList.Body.String(), created.ID) {
		t.Fatalf("default client list includes archived project: %d %s", defaultClientList.Code, defaultClientList.Body.String())
	}
	allClientProjects := performRequest(
		router, http.MethodGet,
		"/api/v1/projects?client_id="+normalizedClientID+"&include_archived=true", nil, nil,
	)
	if allClientProjects.Code != http.StatusOK || !strings.Contains(allClientProjects.Body.String(), created.ID) {
		t.Fatalf("complete client project list misses archived project: %d %s", allClientProjects.Code, allClientProjects.Body.String())
	}
}

func TestProjectWorkflowEventsRecordMutationsAndPaginate(t *testing.T) {
	router, store := newProjectTestAPI(t)
	headers := map[string]string{"Idempotency-Key": "project-event-create"}
	created := createProjectForTest(t, router, `{"name":"活动时间线项目","description":"第一版"}`, headers)

	// A safe create replay returns its original snapshot and must not append a second event.
	replayed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects",
		[]byte(`{"name":"活动时间线项目","description":"第一版"}`),
		headers,
	)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("project create replay = %d: %s", replayed.Code, replayed.Body.String())
	}

	updatedRecorder := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+created.ID,
		[]byte(`{"name":"活动时间线项目二版","description":"第二版"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update project for events = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeProjectResponse(t, updatedRecorder.Body.Bytes())
	started := transitionProjectForTest(t, router, created.ID, updated.Version, `{"action":"start"}`)
	paused := transitionProjectForTest(t, router, created.ID, started.Version, `{"action":"pause"}`)

	firstPageRecorder := performRequest(
		router,
		http.MethodGet,
		"/api/v1/projects/"+created.ID+"/events?page=1&page_size=2",
		nil,
		nil,
	)
	if firstPageRecorder.Code != http.StatusOK {
		t.Fatalf("list project events page 1 = %d: %s", firstPageRecorder.Code, firstPageRecorder.Body.String())
	}
	firstPage, firstMeta := decodeProjectEvents(t, firstPageRecorder.Body.Bytes())
	if len(firstPage) != 2 || firstMeta.Total != 4 || firstMeta.ProjectVersion != paused.Version ||
		firstPageRecorder.Header().Get("ETag") != fmt.Sprintf(`"%d"`, paused.Version) {
		t.Fatalf("project events page 1 = data %#v meta %#v ETag %q", firstPage, firstMeta, firstPageRecorder.Header().Get("ETag"))
	}

	secondPageRecorder := performRequest(
		router,
		http.MethodGet,
		"/api/v1/projects/"+created.ID+"/events?page=2&page_size=2",
		nil,
		nil,
	)
	secondPage, secondMeta := decodeProjectEvents(t, secondPageRecorder.Body.Bytes())
	if len(secondPage) != 2 || secondMeta.Total != 4 || firstPage[0].ID == secondPage[0].ID {
		t.Fatalf("project events page 2 = data %#v meta %#v", secondPage, secondMeta)
	}

	actions := map[string]projectWorkflowEventOutput{}
	for _, event := range append(firstPage, secondPage...) {
		actions[event.Action] = event
		if event.Actor == nil || event.Actor.ID != "00000000-0000-5000-8000-000000000001" || event.Actor.DisplayName == "" {
			t.Fatalf("project event actor = %#v", event.Actor)
		}
		if event.RequestID == nil || *event.RequestID == "" || event.CreatedAt == "" {
			t.Fatalf("project event request/timestamp = %#v", event)
		}
	}
	for _, action := range []string{"project_created", "project_updated", "project_started", "project_paused"} {
		if _, found := actions[action]; !found {
			t.Fatalf("project event action %q missing from %#v", action, actions)
		}
	}
	createdEvent := actions["project_created"]
	if createdEvent.Previous != nil || createdEvent.Current["name"] != "活动时间线项目" || createdEvent.Current["version"] != float64(1) {
		t.Fatalf("project created event snapshot = %#v", createdEvent)
	}
	updatedEvent := actions["project_updated"]
	if updatedEvent.Previous["name"] != "活动时间线项目" || updatedEvent.Current["name"] != "活动时间线项目二版" ||
		updatedEvent.Previous["version"] != float64(1) || updatedEvent.Current["version"] != float64(2) {
		t.Fatalf("project updated event snapshot = %#v", updatedEvent)
	}
	if got := actions["project_paused"].Current["status"]; got != "paused" {
		t.Fatalf("project paused current status = %#v", got)
	}

	var eventCount int64
	if err := store.DB.Table("workflow_events").
		Where("aggregate_type = 'project' AND aggregate_id = ?", created.ID).
		Count(&eventCount).Error; err != nil || eventCount != 4 {
		t.Fatalf("project workflow event count = %d, err = %v", eventCount, err)
	}
}

func TestProjectWorkflowEventFailuresRollBackCommands(t *testing.T) {
	router, store := newProjectTestAPI(t)
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_project_created_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'project' AND NEW.action = 'project_created'
		BEGIN SELECT RAISE(ABORT, 'TEST_PROJECT_CREATED_EVENT_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("create project event failure trigger: %v", err)
	}
	failedCreate := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects",
		[]byte(`{"name":"不应创建的项目"}`),
		nil,
	)
	if failedCreate.Code != http.StatusInternalServerError {
		t.Fatalf("failed project create = %d: %s", failedCreate.Code, failedCreate.Body.String())
	}
	var projectCount int64
	if err := store.DB.Table("projects").Where("name = ?", "不应创建的项目").Count(&projectCount).Error; err != nil || projectCount != 0 {
		t.Fatalf("rolled back project count = %d, err = %v", projectCount, err)
	}
	if err := store.DB.Exec("DROP TRIGGER fail_project_created_event").Error; err != nil {
		t.Fatalf("drop project create failure trigger: %v", err)
	}

	created := createProjectForTest(t, router, `{"name":"事务项目"}`, nil)
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_project_updated_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'project' AND NEW.action = 'project_updated'
		BEGIN SELECT RAISE(ABORT, 'TEST_PROJECT_UPDATED_EVENT_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("create project update failure trigger: %v", err)
	}
	failedUpdate := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+created.ID,
		[]byte(`{"name":"不应保留的名称"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if failedUpdate.Code != http.StatusInternalServerError {
		t.Fatalf("failed project update = %d: %s", failedUpdate.Code, failedUpdate.Body.String())
	}
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+created.ID, nil, nil)
	current := decodeProjectResponse(t, currentRecorder.Body.Bytes())
	if current.Name != created.Name || current.Version != created.Version {
		t.Fatalf("project update was not rolled back: %#v", current)
	}
}

func TestProjectWorkflowEventsValidateProjectAndPagination(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	invalidID := performRequest(router, http.MethodGet, "/api/v1/projects/not-a-uuid/events", nil, nil)
	if invalidID.Code != http.StatusBadRequest || responseErrorCode(t, invalidID.Body.Bytes()) != "INVALID_PROJECT_ID" {
		t.Fatalf("invalid project event id = %d: %s", invalidID.Code, invalidID.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/projects/"+uuid.NewString()+"/events", nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing project events = %d: %s", missing.Code, missing.Body.String())
	}
	project := createProjectForTest(t, router, `{"name":"分页校验项目"}`, nil)
	invalidPage := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/events?page_size=101", nil, nil)
	if invalidPage.Code != http.StatusBadRequest {
		t.Fatalf("invalid project event page size = %d: %s", invalidPage.Code, invalidPage.Body.String())
	}
}

func TestProjectValidationFilteringAndArchivedVisibility(t *testing.T) {
	router, _ := newProjectTestAPI(t)

	invalidBodies := []string{
		`{"name":"x"}`,
		`{"name":"日期错误","start_date":"2026-09-02","due_date":"2026-09-01"}`,
		`{"name":"金额错误","amount_minor":-1}`,
		`{"name":"颜色错误","color":"purple"}`,
		`{"name":"客户错误","client_id":"not-a-uuid"}`,
	}
	for _, body := range invalidBodies {
		recorder := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(body), nil)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid create %s status = %d: %s", body, recorder.Code, recorder.Body.String())
		}
	}
	missingClient := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects",
		[]byte(fmt.Sprintf(`{"name":"不存在客户","client_id":%q}`, uuid.NewString())),
		nil,
	)
	if missingClient.Code != http.StatusUnprocessableEntity || responseErrorCode(t, missingClient.Body.Bytes()) != "CLIENT_NOT_FOUND" {
		t.Fatalf("missing client = %d: %s", missingClient.Code, missingClient.Body.String())
	}
	unknownField := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(`{"name":"非法状态","status":"completed"}`), nil)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("create status field = %d: %s", unknownField.Code, unknownField.Body.String())
	}

	literalPercent := createProjectForTest(t, router, `{"name":"100%计划"}`, nil)
	createProjectForTest(t, router, `{"name":"100个计划"}`, nil)
	search := performRequest(router, http.MethodGet, "/api/v1/projects?q=%25", nil, nil)
	if search.Code != http.StatusOK {
		t.Fatalf("literal percent search = %d: %s", search.Code, search.Body.String())
	}
	var searched struct {
		Data []projectResponse `json:"data"`
		Meta pageMeta          `json:"meta"`
	}
	if err := json.Unmarshal(search.Body.Bytes(), &searched); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if searched.Meta.Total != 1 || len(searched.Data) != 1 || searched.Data[0].ID != literalPercent.ID {
		t.Fatalf("literal percent search = %s", search.Body.String())
	}

	archived := transitionProjectForTest(t, router, literalPercent.ID, 1, `{"action":"archive"}`)
	if archived.Status != "archived" {
		t.Fatalf("archived project = %#v", archived)
	}
	activeList := performRequest(router, http.MethodGet, "/api/v1/projects", nil, nil)
	if strings.Contains(activeList.Body.String(), literalPercent.ID) {
		t.Fatalf("default list includes archived project: %s", activeList.Body.String())
	}
	archivedList := performRequest(router, http.MethodGet, "/api/v1/projects?status=archived", nil, nil)
	if !strings.Contains(archivedList.Body.String(), literalPercent.ID) {
		t.Fatalf("archived list misses project: %s", archivedList.Body.String())
	}
	allList := performRequest(router, http.MethodGet, "/api/v1/projects?include_archived=true", nil, nil)
	if allList.Code != http.StatusOK || !strings.Contains(allList.Body.String(), literalPercent.ID) {
		t.Fatalf("include archived list misses project: %d %s", allList.Code, allList.Body.String())
	}

	invalidQueries := []string{
		"/api/v1/projects?status=unknown",
		"/api/v1/projects?client_id=not-a-uuid",
		"/api/v1/projects?include_archived=sometimes",
		"/api/v1/projects?sort=task_total",
		"/api/v1/projects?page_size=101",
	}
	for _, path := range invalidQueries {
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %s status = %d: %s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestProjectIdempotencyReplaySurvivesMutationAndDeletion(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	body := `{"name":"幂等快照项目","description":"首次请求"}`
	headers := map[string]string{"Idempotency-Key": "project-durable-replay"}
	created := createProjectForTest(t, router, body, headers)

	updatedRecorder := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+created.ID,
		[]byte(`{"name":"已修改项目"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updatedRecorder.Code != http.StatusOK {
		t.Fatalf("update before replay = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeProjectResponse(t, updatedRecorder.Body.Bytes())
	archived := transitionProjectForTest(t, router, created.ID, updated.Version, `{"action":"archive"}`)
	if got := strings.Join(archived.AvailableActions, ","); got != "restore" {
		t.Fatalf("archived available transitions = %q, want restore", got)
	}
	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/projects/"+created.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archived.Version)},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete before replay = %d: %s", deleted.Code, deleted.Body.String())
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/projects", []byte(body), headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("durable replay = %d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	snapshot := decodeProjectResponse(t, replayed.Body.Bytes())
	if snapshot.ID != created.ID || snapshot.Name != created.Name || snapshot.Status != "planning" || snapshot.Version != 1 {
		t.Fatalf("replayed creation snapshot = %#v, want original %#v", snapshot, created)
	}

	conflict := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects",
		[]byte(`{"name":"不同请求"}`),
		headers,
	)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("changed request replay = %d: %s", conflict.Code, conflict.Body.String())
	}
}

func TestProjectPatchValidationNullsAndVersionConflict(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	created := createProjectForTest(t, router, `{
		"name":"产品交付",
		"description":"初始描述",
		"start_date":"2026-08-28",
		"due_date":"2026-09-30",
		"amount_minor":9900,
		"color":"#4CB782"
	}`, nil)
	path := "/api/v1/projects/" + created.ID
	preflight := performRequest(router, http.MethodOptions, path, nil, map[string]string{
		"Origin":                         "tauri://localhost",
		"Access-Control-Request-Method":  http.MethodPatch,
		"Access-Control-Request-Headers": "authorization,content-type,if-match",
	})
	if preflight.Code != http.StatusNoContent ||
		!strings.Contains(preflight.Header().Get("Access-Control-Allow-Headers"), "If-Match") ||
		!strings.Contains(preflight.Header().Get("Access-Control-Expose-Headers"), "ETag") {
		t.Fatalf("If-Match preflight = %d headers=%v", preflight.Code, preflight.Header())
	}

	missingVersion := performRequest(router, http.MethodPatch, path, []byte(`{"name":"新名称"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing If-Match = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	invalidVersion := performRequest(router, http.MethodPatch, path, []byte(`{"name":"新名称"}`), map[string]string{"If-Match": `"abc"`})
	if invalidVersion.Code != http.StatusBadRequest {
		t.Fatalf("invalid If-Match = %d: %s", invalidVersion.Code, invalidVersion.Body.String())
	}
	stale := performRequest(router, http.MethodPatch, path, []byte(`{"name":"新名称"}`), map[string]string{"If-Match": `"9"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale patch = %d: %s", stale.Code, stale.Body.String())
	}
	nullName := performRequest(router, http.MethodPatch, path, []byte(`{"name":null}`), map[string]string{"If-Match": `"1"`})
	if nullName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("null name = %d: %s", nullName.Code, nullName.Body.String())
	}
	statusThroughPatch := performRequest(router, http.MethodPatch, path, []byte(`{"status":"completed"}`), map[string]string{"If-Match": `"1"`})
	if statusThroughPatch.Code != http.StatusBadRequest {
		t.Fatalf("status through patch = %d: %s", statusThroughPatch.Code, statusThroughPatch.Body.String())
	}
	invalidPartialDates := performRequest(router, http.MethodPatch, path, []byte(`{"start_date":"2026-10-01"}`), map[string]string{"If-Match": `"1"`})
	if invalidPartialDates.Code != http.StatusUnprocessableEntity {
		t.Fatalf("partial date ordering = %d: %s", invalidPartialDates.Code, invalidPartialDates.Body.String())
	}
	missingClient := performRequest(
		router,
		http.MethodPatch,
		path,
		[]byte(fmt.Sprintf(`{"client_id":%q}`, uuid.NewString())),
		map[string]string{"If-Match": `"1"`},
	)
	if missingClient.Code != http.StatusUnprocessableEntity || responseErrorCode(t, missingClient.Body.Bytes()) != "CLIENT_NOT_FOUND" {
		t.Fatalf("patch missing client = %d: %s", missingClient.Code, missingClient.Body.String())
	}

	updated := performRequest(
		router,
		http.MethodPatch,
		path,
		[]byte(`{
			"name":"  产品最终交付  ",
			"description":"",
			"client_id":null,
			"start_date":null,
			"due_date":null,
			"amount_minor":null,
			"color":null
		}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("valid patch = %d: %s", updated.Code, updated.Body.String())
	}
	project := decodeProjectResponse(t, updated.Body.Bytes())
	if project.Name != "产品最终交付" || project.Description != "" || project.Version != 2 ||
		project.ClientID != nil || project.StartDate != nil || project.DueDate != nil || project.AmountMinor != nil || project.Color != nil {
		t.Fatalf("updated project = %#v", project)
	}
	if updated.Header().Get("ETag") != `"2"` {
		t.Fatalf("update ETag = %q", updated.Header().Get("ETag"))
	}
	empty := performRequest(router, http.MethodPatch, path, []byte(`{}`), map[string]string{"If-Match": `"2"`})
	if empty.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty patch = %d: %s", empty.Code, empty.Body.String())
	}
}

func TestProjectNameChangeInvalidatesLinkedTaskVersions(t *testing.T) {
	router, store := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"关联任务版本项目"}`, nil)

	createTask := func(title string, projectID *string) (string, int64) {
		t.Helper()
		body := fmt.Sprintf(`{"title":%q}`, title)
		if projectID != nil {
			body = fmt.Sprintf(`{"title":%q,"project_id":%q}`, title, *projectID)
		}
		recorder := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(body), nil)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create task %q = %d: %s", title, recorder.Code, recorder.Body.String())
		}
		var envelope struct {
			Data struct {
				ID      string `json:"id"`
				Version int64  `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode task %q: %v", title, err)
		}
		return envelope.Data.ID, envelope.Data.Version
	}

	linkedTaskID, linkedVersion := createTask("关联任务", &project.ID)
	unrelatedTaskID, unrelatedVersion := createTask("无关任务", nil)
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	current := decodeProjectResponse(t, currentRecorder.Body.Bytes())

	sameName := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+project.ID,
		[]byte(`{"name":"  关联任务版本项目  "}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if sameName.Code != http.StatusOK {
		t.Fatalf("same-name project update = %d: %s", sameName.Code, sameName.Body.String())
	}
	sameNameProject := decodeProjectResponse(t, sameName.Body.Bytes())

	var version int64
	if err := store.DB.Table("tasks").Select("version").Where("id = ?", linkedTaskID).Scan(&version).Error; err != nil {
		t.Fatalf("read linked task version after same-name update: %v", err)
	}
	if version != linkedVersion {
		t.Fatalf("linked task version after same-name update = %d, want %d", version, linkedVersion)
	}

	renamed := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+project.ID,
		[]byte(`{"name":"关联任务版本项目（新）"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, sameNameProject.Version)},
	)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename project = %d: %s", renamed.Code, renamed.Body.String())
	}

	linked := performRequest(router, http.MethodGet, "/api/v1/tasks/"+linkedTaskID, nil, nil)
	if linked.Code != http.StatusOK {
		t.Fatalf("read linked task after project rename = %d: %s", linked.Code, linked.Body.String())
	}
	var linkedEnvelope struct {
		Data struct {
			ProjectName *string `json:"project_name"`
			Version     int64   `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(linked.Body.Bytes(), &linkedEnvelope); err != nil {
		t.Fatalf("decode linked task after project rename: %v", err)
	}
	if linkedEnvelope.Data.ProjectName == nil || *linkedEnvelope.Data.ProjectName != "关联任务版本项目（新）" ||
		linkedEnvelope.Data.Version != linkedVersion+1 || linked.Header().Get("ETag") != fmt.Sprintf(`"%d"`, linkedVersion+1) {
		t.Fatalf("linked task after project rename = version %d, project_name %#v, ETag %q", linkedEnvelope.Data.Version, linkedEnvelope.Data.ProjectName, linked.Header().Get("ETag"))
	}
	if err := store.DB.Table("tasks").Select("version").Where("id = ?", unrelatedTaskID).Scan(&version).Error; err != nil {
		t.Fatalf("read unrelated task version: %v", err)
	}
	if version != unrelatedVersion {
		t.Fatalf("unrelated task version after project rename = %d, want %d", version, unrelatedVersion)
	}
}

func TestProjectTransitionsRequireValidStateVersionAndIncompleteConfirmation(t *testing.T) {
	router, store := newProjectTestAPI(t)
	created := createProjectForTest(t, router, `{"name":"状态流转项目"}`, nil)
	task := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(fmt.Sprintf(`{"title":"未完成任务","project_id":%q}`, created.ID)),
		nil,
	)
	if task.Code != http.StatusCreated {
		t.Fatalf("create incomplete task = %d: %s", task.Code, task.Body.String())
	}
	staleAfterTaskChange := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+created.ID+"/transitions",
		[]byte(`{"action":"start"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if staleAfterTaskChange.Code != http.StatusConflict || responseErrorCode(t, staleAfterTaskChange.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale transition after task change = %d: %s", staleAfterTaskChange.Code, staleAfterTaskChange.Body.String())
	}
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+created.ID, nil, nil)
	current := decodeProjectResponse(t, currentRecorder.Body.Bytes())

	invalid := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+created.ID+"/transitions",
		[]byte(`{"action":"complete"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if invalid.Code != http.StatusConflict || responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_PROJECT_TRANSITION" {
		t.Fatalf("invalid planning completion = %d: %s", invalid.Code, invalid.Body.String())
	}

	started := transitionProjectForTest(t, router, created.ID, current.Version, `{"action":"start"}`)
	paused := transitionProjectForTest(t, router, created.ID, started.Version, `{"action":"pause"}`)
	resumed := transitionProjectForTest(t, router, created.ID, paused.Version, `{"action":"resume"}`)
	withoutConfirmation := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+created.ID+"/transitions",
		[]byte(`{"action":"complete"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, resumed.Version)},
	)
	if withoutConfirmation.Code != http.StatusConflict || responseErrorCode(t, withoutConfirmation.Body.Bytes()) != "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED" {
		t.Fatalf("unconfirmed completion = %d: %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}
	completed := transitionProjectForTest(
		t,
		router,
		created.ID,
		resumed.Version,
		`{"action":"complete","confirm_incomplete_tasks":true}`,
	)
	if completed.Status != "completed" {
		t.Fatalf("completed project = %#v", completed)
	}
	var taskStatus string
	if err := store.DB.Table("tasks").Select("status").Where("project_id = ?", created.ID).Scan(&taskStatus).Error; err != nil {
		t.Fatalf("read project task status: %v", err)
	}
	if taskStatus != "todo" {
		t.Fatalf("project completion changed task status to %q", taskStatus)
	}

	reopened := transitionProjectForTest(t, router, created.ID, completed.Version, `{"action":"reopen"}`)
	archived := transitionProjectForTest(t, router, created.ID, reopened.Version, `{"action":"archive"}`)
	if archived.Status != "archived" || archived.ArchivedFromStatus == nil || *archived.ArchivedFromStatus != "in_progress" {
		t.Fatalf("archived = %#v", archived)
	}
	archivedEdit := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+created.ID,
		[]byte(`{"name":"不应直接修改归档项目"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archived.Version)},
	)
	if archivedEdit.Code != http.StatusConflict || responseErrorCode(t, archivedEdit.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("edit archived project = %d: %s", archivedEdit.Code, archivedEdit.Body.String())
	}
	restored := transitionProjectForTest(t, router, created.ID, archived.Version, `{"action":"restore"}`)
	if restored.Status != "in_progress" {
		t.Fatalf("restored status = %q, want in_progress", restored.Status)
	}

	legacyFallback := createProjectForTest(t, router, `{"name":"旧归档项目"}`, nil)
	if err := store.DB.Model(struct{ ID string }{}).Table("projects").Where("id = ?", legacyFallback.ID).
		Updates(map[string]any{"status": "archived", "archived_from_status": nil}).Error; err != nil {
		t.Fatalf("prepare legacy archived project: %v", err)
	}
	restoredFallback := transitionProjectForTest(t, router, legacyFallback.ID, 1, `{"action":"restore"}`)
	if restoredFallback.Status != "planning" {
		t.Fatalf("legacy restore status = %q, want planning", restoredFallback.Status)
	}
}

func TestArchivedProjectsRejectNewTaskLinksButKeepExistingTaskEditable(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"冻结关联项目"}`, nil)
	linkedTask := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(fmt.Sprintf(`{"title":"既有项目任务","project_id":%q}`, project.ID)),
		nil,
	)
	if linkedTask.Code != http.StatusCreated {
		t.Fatalf("create linked task = %d: %s", linkedTask.Code, linkedTask.Body.String())
	}
	var linkedEnvelope struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(linkedTask.Body.Bytes(), &linkedEnvelope); err != nil {
		t.Fatalf("decode linked task: %v", err)
	}
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	current := decodeProjectResponse(t, currentRecorder.Body.Bytes())
	archived := transitionProjectForTest(t, router, project.ID, current.Version, `{"action":"archive"}`)

	newLink := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(fmt.Sprintf(`{"title":"归档后新增任务","project_id":%q}`, project.ID)),
		nil,
	)
	if newLink.Code != http.StatusConflict || responseErrorCode(t, newLink.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("new task link to archived project = %d: %s", newLink.Code, newLink.Body.String())
	}

	unrelatedTask := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(`{"title":"未归项目任务"}`),
		nil,
	)
	if unrelatedTask.Code != http.StatusCreated {
		t.Fatalf("create unrelated task = %d: %s", unrelatedTask.Code, unrelatedTask.Body.String())
	}
	var unrelatedEnvelope struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(unrelatedTask.Body.Bytes(), &unrelatedEnvelope); err != nil {
		t.Fatalf("decode unrelated task: %v", err)
	}
	changedLink := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+unrelatedEnvelope.Data.ID,
		[]byte(fmt.Sprintf(`{"project_id":%q}`, project.ID)),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, unrelatedEnvelope.Data.Version)},
	)
	if changedLink.Code != http.StatusConflict || responseErrorCode(t, changedLink.Body.Bytes()) != "PROJECT_ARCHIVED" {
		t.Fatalf("move task to archived project = %d: %s", changedLink.Code, changedLink.Body.String())
	}

	keepLink := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+linkedEnvelope.Data.ID,
		[]byte(fmt.Sprintf(`{"title":"归档项目中的既有任务","project_id":%q}`, project.ID)),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, linkedEnvelope.Data.Version)},
	)
	if keepLink.Code != http.StatusOK {
		t.Fatalf("edit task with unchanged archived project = %d: %s", keepLink.Code, keepLink.Body.String())
	}

	detailRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	detail := decodeProjectResponse(t, detailRecorder.Body.Bytes())
	if detail.Status != archived.Status || detail.Version != archived.Version {
		t.Fatalf("unchanged archived link bumped aggregate version: before=%#v after=%#v", archived, detail)
	}
}

func TestProjectHardDeleteRequiresArchiveConfirmationAndDetachesReferences(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clientID := uuid.NewString()
	insertTestClient(t, store, clientID, "删除验证客户")
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"待删除项目","client_id":%q}`, clientID), nil)

	task := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks",
		[]byte(fmt.Sprintf(`{"title":"保留关联任务","project_id":%q}`, project.ID)),
		nil,
	)
	if task.Code != http.StatusCreated {
		t.Fatalf("create referenced task = %d: %s", task.Code, task.Body.String())
	}
	var taskEnvelope struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(task.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("decode referenced task: %v", err)
	}
	invoiceID := uuid.NewString()
	if err := store.DB.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, project_id, amount_minor, currency,
			status, issue_date, due_date, notes, created_at, updated_at
		) VALUES (?, ?, ?, ?, 10000, 'CNY', 'draft', '2026-08-28', '2026-09-28', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, invoiceID, "INV-DELETE-001", clientID, project.ID).Error; err != nil {
		t.Fatalf("insert referenced invoice: %v", err)
	}
	currentRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	current := decodeProjectResponse(t, currentRecorder.Body.Bytes())

	notArchived := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/projects/"+project.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)},
	)
	if notArchived.Code != http.StatusConflict || responseErrorCode(t, notArchived.Body.Bytes()) != "PROJECT_NOT_ARCHIVED" {
		t.Fatalf("delete active project = %d: %s", notArchived.Code, notArchived.Body.String())
	}
	archived := transitionProjectForTest(t, router, project.ID, current.Version, `{"action":"archive"}`)

	withoutConfirmation := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/projects/"+project.ID,
		nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archived.Version)},
	)
	if withoutConfirmation.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirmation.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("delete without confirmation = %d: %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}
	stale := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/projects/"+project.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": `"1"`},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale delete = %d: %s", stale.Code, stale.Body.String())
	}

	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/projects/"+project.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archived.Version)},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete archived project = %d: %s", deleted.Code, deleted.Body.String())
	}
	var deletion struct {
		Data deletedProjectResponse `json:"data"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletion); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deletion.Data.DeletedID != project.ID || deletion.Data.DetachedTasks != 1 || deletion.Data.DetachedInvoices != 1 {
		t.Fatalf("delete response = %#v", deletion.Data)
	}
	var detachedTask struct {
		ProjectID *string `gorm:"column:project_id"`
		Version   int64   `gorm:"column:version"`
	}
	if err := store.DB.Table("tasks").Select("project_id, version").Where("id = ?", taskEnvelope.Data.ID).Scan(&detachedTask).Error; err != nil {
		t.Fatalf("read detached task: %v", err)
	}
	if detachedTask.ProjectID != nil {
		t.Fatalf("task project_id = %q, want null", *detachedTask.ProjectID)
	}
	if detachedTask.Version != taskEnvelope.Data.Version+1 {
		t.Fatalf("detached task version = %d, want %d", detachedTask.Version, taskEnvelope.Data.Version+1)
	}
	var detachedInvoiceProjectID *string
	if err := store.DB.Table("invoices").Select("project_id").Where("id = ?", invoiceID).Scan(&detachedInvoiceProjectID).Error; err != nil {
		t.Fatalf("read detached invoice: %v", err)
	}
	if detachedInvoiceProjectID != nil {
		t.Fatalf("invoice project_id = %q, want null", *detachedInvoiceProjectID)
	}
	var deletedEvent struct {
		PreviousJSON *string `gorm:"column:previous_json"`
		CurrentJSON  *string `gorm:"column:current_json"`
	}
	if err := store.DB.Table("workflow_events").
		Select("previous_json, current_json").
		Where("aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_deleted'", project.ID).
		Take(&deletedEvent).Error; err != nil {
		t.Fatalf("read retained project deletion event: %v", err)
	}
	if deletedEvent.PreviousJSON == nil || !strings.Contains(*deletedEvent.PreviousJSON, `"status":"archived"`) || deletedEvent.CurrentJSON != nil {
		t.Fatalf("project deletion event = %#v", deletedEvent)
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted project detail = %d: %s", missing.Code, missing.Body.String())
	}
	missingEvents := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/events", nil, nil)
	if missingEvents.Code != http.StatusNotFound {
		t.Fatalf("deleted project events = %d: %s", missingEvents.Code, missingEvents.Body.String())
	}
}
