package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type projectClientActivityProjectionRow struct {
	models.ClientActivity `gorm:"embedded"`
	EventAction           string `gorm:"column:event_action"`
	EventCreatedAt        string `gorm:"column:event_created_at"`
}

func projectClientActivityProjections(
	t *testing.T,
	store *database.Store,
	projectID string,
) []projectClientActivityProjectionRow {
	t.Helper()
	var rows []projectClientActivityProjectionRow
	if err := store.DB.Table("client_activities AS activity").
		Select(`activity.*, event.action AS event_action, event.created_at AS event_created_at`).
		Joins("JOIN workflow_events AS event ON event.id = activity.source_id").
		Where("event.aggregate_type = 'project' AND event.aggregate_id = ?", projectID).
		Order("event.created_at ASC, event.id ASC").
		Scan(&rows).Error; err != nil {
		t.Fatalf("load Project Client activity projections: %v", err)
	}
	return rows
}

type projectProjectionAtomicState struct {
	ProjectStatus        string
	ProjectVersion       int64
	EventCount           int64
	ActivityCount        int64
	CompletionInboxCount int64
	ClientVersion        int64
}

func loadProjectProjectionAtomicState(
	t *testing.T,
	store *database.Store,
	projectID string,
	clientID string,
) projectProjectionAtomicState {
	t.Helper()
	var project models.Project
	if err := store.DB.First(&project, "id = ?", projectID).Error; err != nil {
		t.Fatalf("load Project atomic state: %v", err)
	}
	state := projectProjectionAtomicState{
		ProjectStatus:  project.Status,
		ProjectVersion: project.Version,
	}
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'project' AND aggregate_id = ?", projectID).
		Count(&state.EventCount).Error; err != nil {
		t.Fatalf("count Project events in atomic state: %v", err)
	}
	if err := store.DB.Model(&models.ClientActivity{}).
		Where("client_id = ? AND source_type = 'project_workflow_event'", clientID).
		Count(&state.ActivityCount).Error; err != nil {
		t.Fatalf("count Project Client activities in atomic state: %v", err)
	}
	if err := store.DB.Table("inbox_items").
		Where("source_entity_type = 'project_completion' AND source_entity_id = ?", projectID).
		Count(&state.CompletionInboxCount).Error; err != nil {
		t.Fatalf("count Project completion Inbox Items in atomic state: %v", err)
	}
	if err := store.DB.Raw("SELECT version FROM clients WHERE id = ?", clientID).
		Scan(&state.ClientVersion).Error; err != nil {
		t.Fatalf("load Client version in atomic state: %v", err)
	}
	return state
}

func TestProjectCompleteAndReopenProjectClientActivities(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clientID := uuid.NewString()
	insertTestClient(t, store, clientID, "星河工作室")

	project := createProjectForTest(
		t,
		router,
		fmt.Sprintf(`{"name":"品牌焕新","client_id":%q}`, clientID),
		nil,
	)
	started := transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	completed := transitionProjectForTest(t, router, project.ID, started.Version, `{"action":"complete"}`)

	stale := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+project.ID+"/transitions",
		[]byte(`{"action":"complete"}`),
		map[string]string{"If-Match": fmt.Sprintf("\"%d\"", started.Version)},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale completion = %d: %s", stale.Code, stale.Body.String())
	}

	reopened := transitionProjectForTest(t, router, project.ID, completed.Version, `{"action":"reopen"}`)
	if reopened.Status != "in_progress" {
		t.Fatalf("reopened Project = %#v", reopened)
	}
	staleReopen := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+project.ID+"/transitions",
		[]byte(`{"action":"reopen"}`),
		map[string]string{"If-Match": fmt.Sprintf("\"%d\"", completed.Version)},
	)
	if staleReopen.Code != http.StatusConflict || responseErrorCode(t, staleReopen.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale reopen = %d: %s", staleReopen.Code, staleReopen.Body.String())
	}

	rows := projectClientActivityProjections(t, store, project.ID)
	if len(rows) != 2 {
		t.Fatalf("Project Client activity projection count = %d, want 2: %#v", len(rows), rows)
	}
	wantTitles := map[string]string{
		"project_completed": "项目「品牌焕新」已完成",
		"project_reopened":  "项目「品牌焕新」已重新打开",
	}
	seenSources := make(map[string]struct{}, len(rows))
	latestOccurredAt := ""
	for _, row := range rows {
		wantTitle, ok := wantTitles[row.EventAction]
		if !ok {
			t.Fatalf("unexpected projected event action = %q", row.EventAction)
		}
		if row.ClientID != clientID || row.Kind != "system_reference" || row.Title != wantTitle || row.Body != nil ||
			row.CreatedByActorID != models.BuiltinSystemActorID || row.SourceType == nil || *row.SourceType != "project_workflow_event" ||
			row.SourceID == nil || row.Version != 1 {
			t.Fatalf("projected Client activity = %#v", row)
		}
		if *row.SourceID == "" {
			t.Fatal("projected Client activity source id is empty")
		}
		if row.OccurredAt != row.EventCreatedAt || row.CreatedAt != row.EventCreatedAt || row.UpdatedAt != row.EventCreatedAt {
			t.Fatalf("projection timestamps activity=%#v event_created_at=%q", row.ClientActivity, row.EventCreatedAt)
		}
		if _, duplicate := seenSources[*row.SourceID]; duplicate {
			t.Fatalf("duplicate projected source id = %q", *row.SourceID)
		}
		seenSources[*row.SourceID] = struct{}{}
		if row.OccurredAt > latestOccurredAt {
			latestOccurredAt = row.OccurredAt
		}
	}

	detailRecorder := performRequest(router, http.MethodGet, "/api/v1/clients/"+clientID, nil, nil)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("Client detail after Project lifecycle = %d: %s", detailRecorder.Code, detailRecorder.Body.String())
	}
	client := decodeClientResponse(t, detailRecorder.Body.Bytes())
	if client.Version != 4 || client.LatestActivityAt == nil || *client.LatestActivityAt != normalizeTimestamp(latestOccurredAt) {
		t.Fatalf("Client after Project lifecycle = %#v", client)
	}

	withoutClient := createProjectForTest(t, router, `{"name":"内部系统升级"}`, nil)
	withoutClient = transitionProjectForTest(t, router, withoutClient.ID, withoutClient.Version, `{"action":"start"}`)
	withoutClient = transitionProjectForTest(t, router, withoutClient.ID, withoutClient.Version, `{"action":"complete"}`)
	withoutClient = transitionProjectForTest(t, router, withoutClient.ID, withoutClient.Version, `{"action":"reopen"}`)
	if rows := projectClientActivityProjections(t, store, withoutClient.ID); len(rows) != 0 {
		t.Fatalf("Project without Client created projections: %#v", rows)
	}
	var lifecycleEventCount int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'project' AND aggregate_id = ? AND action IN ?", withoutClient.ID, []string{"project_completed", "project_reopened"}).
		Count(&lifecycleEventCount).Error; err != nil || lifecycleEventCount != 2 {
		t.Fatalf("Project without Client lifecycle events = %d, err=%v", lifecycleEventCount, err)
	}
}

func TestProjectClientActivityHistoryStaysWithClientAtEventTime(t *testing.T) {
	router, store := newProjectTestAPI(t)
	firstClientID := uuid.NewString()
	secondClientID := uuid.NewString()
	insertTestClient(t, store, firstClientID, "原客户")
	insertTestClient(t, store, secondClientID, "新客户")

	project := createProjectForTest(
		t,
		router,
		fmt.Sprintf(`{"name":"跨客户项目","client_id":%q}`, firstClientID),
		nil,
	)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"reopen"}`)

	reboundRecorder := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/projects/"+project.ID,
		[]byte(fmt.Sprintf(`{"client_id":%q}`, secondClientID)),
		map[string]string{"If-Match": fmt.Sprintf("\"%d\"", project.Version)},
	)
	if reboundRecorder.Code != http.StatusOK {
		t.Fatalf("rebind Project Client = %d: %s", reboundRecorder.Code, reboundRecorder.Body.String())
	}
	project = decodeProjectResponse(t, reboundRecorder.Body.Bytes())
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)

	rows := projectClientActivityProjections(t, store, project.ID)
	if len(rows) != 3 {
		t.Fatalf("rebound Project projection count = %d, want 3: %#v", len(rows), rows)
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.ClientID]++
	}
	if counts[firstClientID] != 2 || counts[secondClientID] != 1 {
		t.Fatalf("rebound Project Client activity ownership = %#v", counts)
	}
}

func TestProjectCompletionInboxFailureRollsBackClientActivityAndTransition(t *testing.T) {
	router, store := newProjectTestAPI(t)
	clientID := uuid.NewString()
	insertTestClient(t, store, clientID, "失败回滚客户")
	project := createProjectForTest(
		t,
		router,
		fmt.Sprintf(`{"name":"失败回滚项目","client_id":%q}`, clientID),
		nil,
	)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)

	if err := store.DB.Exec(`
		CREATE TRIGGER test_fail_project_completion_inbox
		BEFORE INSERT ON inbox_items
		WHEN NEW.source_entity_type = 'project_completion'
		BEGIN
			SELECT RAISE(ABORT, 'TEST_PROJECT_COMPLETION_INBOX_FAILURE');
		END;
	`).Error; err != nil {
		t.Fatalf("install Project completion failure trigger: %v", err)
	}

	failed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/projects/"+project.ID+"/transitions",
		[]byte(`{"action":"complete"}`),
		map[string]string{"If-Match": fmt.Sprintf("\"%d\"", project.Version)},
	)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed Project completion = %d: %s", failed.Code, failed.Body.String())
	}

	var persisted models.Project
	if err := store.DB.First(&persisted, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after rollback: %v", err)
	}
	if persisted.Status != "in_progress" || persisted.Version != project.Version {
		t.Fatalf("Project persisted after rollback = %#v", persisted)
	}
	var activityCount, eventCount, inboxCount int64
	if err := store.DB.Model(&models.ClientActivity{}).
		Where("client_id = ? AND source_type = 'project_workflow_event'", clientID).
		Count(&activityCount).Error; err != nil {
		t.Fatalf("count rolled-back Client activities: %v", err)
	}
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'", project.ID).
		Count(&eventCount).Error; err != nil {
		t.Fatalf("count rolled-back Project events: %v", err)
	}
	if err := store.DB.Table("inbox_items").
		Where("source_entity_type = 'project_completion' AND source_entity_id = ?", project.ID).
		Count(&inboxCount).Error; err != nil {
		t.Fatalf("count rolled-back Project Inbox Items: %v", err)
	}
	if activityCount != 0 || eventCount != 0 || inboxCount != 0 {
		t.Fatalf("failed transition left activity=%d event=%d inbox=%d", activityCount, eventCount, inboxCount)
	}
	var clientVersion int64
	if err := store.DB.Raw("SELECT version FROM clients WHERE id = ?", clientID).Scan(&clientVersion).Error; err != nil {
		t.Fatalf("load Client version after rollback: %v", err)
	}
	if clientVersion != 2 {
		t.Fatalf("Client version after rollback = %d, want 2", clientVersion)
	}
}

type projectProjectionFailureInstaller func(
	t *testing.T,
	store *database.Store,
	projectID string,
	clientID string,
	eventAction string,
)

func testProjectCompleteAndReopenProjectionRollback(
	t *testing.T,
	installFailure projectProjectionFailureInstaller,
) {
	t.Helper()
	testCases := []struct {
		name        string
		action      string
		eventAction string
		wantBefore  projectProjectionAtomicState
	}{
		{
			name:        "complete",
			action:      "complete",
			eventAction: "project_completed",
			wantBefore: projectProjectionAtomicState{
				ProjectStatus:  "in_progress",
				ProjectVersion: 2,
				EventCount:     2,
				ClientVersion:  2,
			},
		},
		{
			name:        "reopen",
			action:      "reopen",
			eventAction: "project_reopened",
			wantBefore: projectProjectionAtomicState{
				ProjectStatus:        "completed",
				ProjectVersion:       3,
				EventCount:           3,
				ActivityCount:        1,
				CompletionInboxCount: 1,
				ClientVersion:        3,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			router, store := newProjectTestAPI(t)
			clientID := uuid.NewString()
			insertTestClient(t, store, clientID, "原子投影故障客户")
			project := createProjectForTest(
				t,
				router,
				fmt.Sprintf(`{"name":"原子投影故障项目","client_id":%q}`, clientID),
				nil,
			)
			project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
			if testCase.action == "reopen" {
				project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
			}

			before := loadProjectProjectionAtomicState(t, store, project.ID, clientID)
			if before != testCase.wantBefore {
				t.Fatalf("%s precondition state = %#v, want %#v", testCase.action, before, testCase.wantBefore)
			}
			installFailure(t, store, project.ID, clientID, testCase.eventAction)

			failed := performRequest(
				router,
				http.MethodPost,
				"/api/v1/projects/"+project.ID+"/transitions",
				[]byte(fmt.Sprintf(`{"action":%q}`, testCase.action)),
				map[string]string{"If-Match": fmt.Sprintf("\"%d\"", project.Version)},
			)
			if failed.Code != http.StatusInternalServerError || responseErrorCode(t, failed.Body.Bytes()) != "INTERNAL_ERROR" {
				t.Fatalf("%s injected failure = %d: %s", testCase.action, failed.Code, failed.Body.String())
			}

			after := loadProjectProjectionAtomicState(t, store, project.ID, clientID)
			if after != before {
				t.Fatalf("%s injected failure left side effects: before=%#v after=%#v", testCase.action, before, after)
			}
		})
	}
}

func TestProjectWorkflowEventInsertFailureRollsBackCompleteAndReopenProjection(t *testing.T) {
	testProjectCompleteAndReopenProjectionRollback(t, func(
		t *testing.T,
		store *database.Store,
		projectID string,
		_ string,
		eventAction string,
	) {
		t.Helper()
		if err := store.DB.Exec(fmt.Sprintf(`
			CREATE TRIGGER test_fail_project_projection_workflow_event
			BEFORE INSERT ON workflow_events
			WHEN NEW.aggregate_type = 'project'
			  AND NEW.aggregate_id = '%s'
			  AND NEW.action = '%s'
			BEGIN
				SELECT RAISE(ABORT, 'TEST_PROJECT_PROJECTION_EVENT_FAILURE');
			END;
		`, projectID, eventAction)).Error; err != nil {
			t.Fatalf("install Project Workflow Event failure trigger: %v", err)
		}
	})
}

func TestProjectClientActivityInsertFailureRollsBackCompleteAndReopenProjection(t *testing.T) {
	testProjectCompleteAndReopenProjectionRollback(t, func(
		t *testing.T,
		store *database.Store,
		projectID string,
		clientID string,
		eventAction string,
	) {
		t.Helper()
		if err := store.DB.Exec(fmt.Sprintf(`
			CREATE TRIGGER test_fail_project_projection_client_activity
			BEFORE INSERT ON client_activities
			WHEN NEW.client_id = '%s'
			  AND NEW.kind = 'system_reference'
			  AND NEW.source_type = 'project_workflow_event'
			  AND EXISTS (
				SELECT 1
				FROM workflow_events
				WHERE id = NEW.source_id
				  AND aggregate_type = 'project'
				  AND aggregate_id = '%s'
				  AND action = '%s'
			  )
			BEGIN
				SELECT RAISE(ABORT, 'TEST_PROJECT_PROJECTION_ACTIVITY_FAILURE');
			END;
		`, clientID, projectID, eventAction)).Error; err != nil {
			t.Fatalf("install Project Client Activity failure trigger: %v", err)
		}
	})
}
