package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeAssignmentMutation(t *testing.T, body []byte) assignmentMutationResponse {
	t.Helper()
	var envelope struct {
		Data assignmentMutationResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode assignment mutation: %v", err)
	}
	return envelope.Data
}

func decodeReassignMutation(t *testing.T, body []byte) reassignMutationResponse {
	t.Helper()
	var envelope struct {
		Data reassignMutationResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode reassign mutation: %v", err)
	}
	return envelope.Data
}

func decodeAssignmentList(t *testing.T, body []byte) (assignmentListData, assignmentListMeta) {
	t.Helper()
	var envelope struct {
		Data assignmentListData `json:"data"`
		Meta assignmentListMeta `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode assignment list: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func assignmentError(t *testing.T, body []byte) errorResponse {
	t.Helper()
	var response errorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode assignment error: %v", err)
	}
	if response.RequestID == "" {
		t.Fatalf("assignment error has no request_id: %s", body)
	}
	return response
}

func createAssignmentForTest(
	t *testing.T,
	router http.Handler,
	taskIDValue string,
	role string,
	actorIDValue string,
	version int64,
	key string,
) assignmentMutationResponse {
	t.Helper()
	headers := map[string]string{"If-Match": fmt.Sprintf(`"%d"`, version)}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	recorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+taskIDValue+"/assignments",
		[]byte(fmt.Sprintf(`{"role":%q,"actor_id":%q}`, role, actorIDValue)),
		headers,
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create assignment = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeAssignmentMutation(t, recorder.Body.Bytes())
}

func TestAssignmentLifecycleIdempotencyListAndEvents(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Prepare assignment lifecycle"}`)
	alice := createActorForTest(t, router, `{"type":"person","display_name":"Alice"}`, nil)
	bob := createActorForTest(t, router, `{"type":"person","display_name":"Bob"}`, nil)

	uppercaseTaskPath := "/api/v1/tasks/" + strings.ToUpper(task.ID)
	uppercaseTask := performRequest(router, http.MethodGet, uppercaseTaskPath, nil, nil)
	if uppercaseTask.Code != http.StatusOK {
		t.Fatalf("uppercase task path = %d: %s", uppercaseTask.Code, uppercaseTask.Body.String())
	}
	initial := performRequest(router, http.MethodGet, uppercaseTaskPath+"/assignments", nil, nil)
	if initial.Code != http.StatusOK || initial.Header().Get("ETag") != `"1"` {
		t.Fatalf("initial assignments = %d headers=%v: %s", initial.Code, initial.Header(), initial.Body.String())
	}
	initialData, initialMeta := decodeAssignmentList(t, initial.Body.Bytes())
	if initialData.Active.Assignee != nil || initialData.Active.Reviewer != nil ||
		len(initialData.History) != 0 || initialMeta.Total != 0 || initialMeta.TaskVersion != 1 {
		t.Fatalf("initial assignment list = %#v meta=%#v", initialData, initialMeta)
	}

	createRequestID := uuid.NewString()
	createBody := []byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, strings.ToUpper(alice.ID)))
	createHeaders := map[string]string{
		"If-Match":        `"1"`,
		"Idempotency-Key": "assignment-create-lifecycle",
		"X-Request-ID":    createRequestID,
	}
	createdRecorder := performRequest(
		router,
		http.MethodPost,
		uppercaseTaskPath+"/assignments",
		createBody,
		createHeaders,
	)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("create assignment = %d headers=%v: %s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	createdBody := createdRecorder.Body.String()
	created := decodeAssignmentMutation(t, createdRecorder.Body.Bytes())
	if created.Task.Version != 2 || created.Assignment.TaskID != task.ID ||
		created.Assignment.ActorID != alice.ID || created.Assignment.Actor.DisplayName != "Alice" ||
		created.Assignment.Actor.Type != "person" || created.Assignment.AssignedByActorID != models.BuiltinOwnerActorID ||
		created.Assignment.Reason != nil || !created.Assignment.IsActive || created.Assignment.Inferred {
		t.Fatalf("created assignment = %#v task=%#v", created.Assignment, created.Task)
	}

	conflictingReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, bob.ID)),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "assignment-create-lifecycle"},
	)
	if conflictingReplay.Code != http.StatusConflict || assignmentError(t, conflictingReplay.Body.Bytes()).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflicting assignment replay = %d: %s", conflictingReplay.Code, conflictingReplay.Body.String())
	}

	reassignRequestID := uuid.NewString()
	reassignBody := []byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"  工作交接  "}`, bob.ID))
	reassignedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reassign",
		reassignBody,
		map[string]string{
			"If-Match": `"2"`, "Idempotency-Key": "assignment-reassign-lifecycle", "X-Request-ID": reassignRequestID,
		},
	)
	if reassignedRecorder.Code != http.StatusOK || reassignedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("reassign = %d headers=%v: %s", reassignedRecorder.Code, reassignedRecorder.Header(), reassignedRecorder.Body.String())
	}
	reassignedBody := reassignedRecorder.Body.String()
	reassigned := decodeReassignMutation(t, reassignedRecorder.Body.Bytes())
	if reassigned.Task.Version != 3 || reassigned.Assignment.ActorID != bob.ID || !reassigned.Assignment.IsActive ||
		reassigned.PreviousAssignment.ID != created.Assignment.ID || reassigned.PreviousAssignment.IsActive ||
		reassigned.PreviousAssignment.Reason == nil || *reassigned.PreviousAssignment.Reason != "工作交接" {
		t.Fatalf("reassigned response = %#v", reassigned)
	}

	lateCreateReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/assignments",
		createBody,
		createHeaders,
	)
	if lateCreateReplay.Code != http.StatusCreated || lateCreateReplay.Header().Get("ETag") != `"2"` ||
		lateCreateReplay.Header().Get("Idempotency-Replayed") != "true" || lateCreateReplay.Body.String() != createdBody {
		t.Fatalf("late create replay = %d headers=%v: %s", lateCreateReplay.Code, lateCreateReplay.Header(), lateCreateReplay.Body.String())
	}

	endRequestID := uuid.NewString()
	endPath := "/api/v1/assignments/" + strings.ToUpper(reassigned.Assignment.ID) + "/end"
	endBody := []byte(`{"reason":"  已完成线下交付  "}`)
	endedRecorder := performRequest(
		router,
		http.MethodPost,
		endPath,
		endBody,
		map[string]string{
			"If-Match": `"3"`, "Idempotency-Key": "assignment-end-lifecycle", "X-Request-ID": endRequestID,
		},
	)
	if endedRecorder.Code != http.StatusOK || endedRecorder.Header().Get("ETag") != `"4"` {
		t.Fatalf("end assignment = %d headers=%v: %s", endedRecorder.Code, endedRecorder.Header(), endedRecorder.Body.String())
	}
	endedBody := endedRecorder.Body.String()
	ended := decodeAssignmentMutation(t, endedRecorder.Body.Bytes())
	if ended.Task.Version != 4 || ended.Assignment.ID != reassigned.Assignment.ID || ended.Assignment.IsActive ||
		ended.Assignment.Reason == nil || *ended.Assignment.Reason != "已完成线下交付" {
		t.Fatalf("ended assignment = %#v", ended)
	}

	lateReassignReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reassign",
		reassignBody,
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "assignment-reassign-lifecycle"},
	)
	if lateReassignReplay.Code != http.StatusOK || lateReassignReplay.Header().Get("ETag") != `"3"` ||
		lateReassignReplay.Header().Get("Idempotency-Replayed") != "true" || lateReassignReplay.Body.String() != reassignedBody {
		t.Fatalf("late reassign replay = %d headers=%v: %s", lateReassignReplay.Code, lateReassignReplay.Header(), lateReassignReplay.Body.String())
	}
	lateEndReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/assignments/"+ended.Assignment.ID+"/end",
		endBody,
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "assignment-end-lifecycle"},
	)
	if lateEndReplay.Code != http.StatusOK || lateEndReplay.Header().Get("ETag") != `"4"` ||
		lateEndReplay.Header().Get("Idempotency-Replayed") != "true" || lateEndReplay.Body.String() != endedBody {
		t.Fatalf("late end replay = %d headers=%v: %s", lateEndReplay.Code, lateEndReplay.Header(), lateEndReplay.Body.String())
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments?page_size=1", nil, nil)
	if listed.Code != http.StatusOK || listed.Header().Get("ETag") != `"4"` {
		t.Fatalf("list ended assignments = %d: %s", listed.Code, listed.Body.String())
	}
	listedData, listedMeta := decodeAssignmentList(t, listed.Body.Bytes())
	if listedData.Active.Assignee != nil || listedData.Active.Reviewer != nil || len(listedData.History) != 1 ||
		listedMeta.Total != 2 || listedMeta.TaskVersion != 4 || listedMeta.PageSize != 1 {
		t.Fatalf("listed assignments = %#v meta=%#v", listedData, listedMeta)
	}
	secondPage := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments?page=2&page_size=1", nil, nil)
	secondData, secondMeta := decodeAssignmentList(t, secondPage.Body.Bytes())
	if secondPage.Code != http.StatusOK || len(secondData.History) != 1 || secondMeta.Total != 2 ||
		secondData.History[0].ID == listedData.History[0].ID {
		t.Fatalf("second assignment page = %d data=%#v meta=%#v", secondPage.Code, secondData, secondMeta)
	}
	filtered := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments?role=reviewer", nil, nil)
	filteredData, filteredMeta := decodeAssignmentList(t, filtered.Body.Bytes())
	if filtered.Code != http.StatusOK || filteredMeta.Total != 0 || len(filteredData.History) != 0 {
		t.Fatalf("reviewer-filtered history = %d: %s", filtered.Code, filtered.Body.String())
	}

	var events []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = ? AND aggregate_id = ?", "task", task.ID).
		Order("created_at ASC, id ASC").Find(&events).Error; err != nil {
		t.Fatalf("load assignment workflow events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("assignment event count = %d, want 3: %#v", len(events), events)
	}
	wantActions := map[string]string{
		"assignment_created":    createRequestID,
		"assignment_reassigned": reassignRequestID,
		"assignment_ended":      endRequestID,
	}
	for _, event := range events {
		wantRequestID, ok := wantActions[event.Action]
		if !ok || event.RequestID == nil || *event.RequestID != wantRequestID || event.CurrentJSON == nil ||
			!strings.Contains(*event.CurrentJSON, `"display_name"`) {
			t.Fatalf("assignment event = %#v", event)
		}
		if event.Action != "assignment_created" && event.PreviousJSON == nil {
			t.Fatalf("assignment event missing previous snapshot: %#v", event)
		}
	}
}

func TestMigratedAssignmentInferenceSurvivesReasonReplacement(t *testing.T) {
	router, store := newActorTestAPI(t)

	t.Run("reassign", func(t *testing.T) {
		task := createTaskForTaskFacts(t, router, `{"title":"Reassign inferred owner"}`)
		assignmentIDValue := seedInferredOwnerAssignment(t, store, task)
		person := createActorForTest(t, router, `{"type":"person","display_name":"Offline owner"}`, nil)
		recorder := performRequest(
			router,
			http.MethodPost,
			"/api/v1/tasks/"+task.ID+"/reassign",
			[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"显式改派"}`, person.ID)),
			map[string]string{"If-Match": `"1"`},
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("reassign inferred assignment = %d: %s", recorder.Code, recorder.Body.String())
		}
		response := decodeReassignMutation(t, recorder.Body.Bytes())
		if response.PreviousAssignment.ID != assignmentIDValue || !response.PreviousAssignment.Inferred ||
			response.PreviousAssignment.Reason == nil || *response.PreviousAssignment.Reason != "显式改派" ||
			response.Assignment.Inferred {
			t.Fatalf("reassigned inferred assignment = %#v", response)
		}
		list := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
		data, _ := decodeAssignmentList(t, list.Body.Bytes())
		if list.Code != http.StatusOK || len(data.History) != 1 || !data.History[0].Inferred ||
			data.History[0].Reason == nil || *data.History[0].Reason != "显式改派" {
			t.Fatalf("inferred history after reassign = %d: %s", list.Code, list.Body.String())
		}
	})

	t.Run("end", func(t *testing.T) {
		task := createTaskForTaskFacts(t, router, `{"title":"End inferred owner"}`)
		assignmentIDValue := seedInferredOwnerAssignment(t, store, task)
		before := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
		beforeData, _ := decodeAssignmentList(t, before.Body.Bytes())
		if before.Code != http.StatusOK || beforeData.Active.Assignee == nil || !beforeData.Active.Assignee.Inferred ||
			beforeData.Active.Assignee.Reason != nil {
			t.Fatalf("active inferred assignment leaked sentinel: %d: %s", before.Code, before.Body.String())
		}
		endedRecorder := performRequest(
			router,
			http.MethodPost,
			"/api/v1/assignments/"+assignmentIDValue+"/end",
			[]byte(`{"reason":"人工结束迁移记录"}`),
			map[string]string{"If-Match": `"1"`},
		)
		if endedRecorder.Code != http.StatusOK {
			t.Fatalf("end inferred assignment = %d: %s", endedRecorder.Code, endedRecorder.Body.String())
		}
		ended := decodeAssignmentMutation(t, endedRecorder.Body.Bytes()).Assignment
		if !ended.Inferred || ended.Reason == nil || *ended.Reason != "人工结束迁移记录" || ended.IsActive {
			t.Fatalf("ended inferred assignment = %#v", ended)
		}
	})
}

func TestAssignmentValidationPermissionsConflictsAndDoneRules(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Validate assignments"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Active person"}`, nil)
	inactive := createActorForTest(t, router, `{"type":"person","display_name":"Inactive person","status":"inactive"}`, nil)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	agent := models.Actor{
		ID: uuid.NewString(), Type: "agent", DisplayName: "Unavailable agent", Status: "active",
		MetadataJSON: "{}", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent actor: %v", err)
	}

	permissionCases := []struct {
		name    string
		role    string
		actorID string
		status  int
		code    string
	}{
		{name: "inactive assignee", role: "assignee", actorID: inactive.ID, status: http.StatusConflict, code: "ASSIGNMENT_ACTOR_NOT_ACTIVE"},
		{name: "system assignee", role: "assignee", actorID: models.BuiltinSystemActorID, status: http.StatusUnprocessableEntity, code: "ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED"},
		{name: "agent assignee", role: "assignee", actorID: agent.ID, status: http.StatusUnprocessableEntity, code: "ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED"},
		{name: "person reviewer", role: "reviewer", actorID: person.ID, status: http.StatusUnprocessableEntity, code: "ASSIGNMENT_REVIEWER_MUST_BE_OWNER"},
		{name: "missing actor", role: "assignee", actorID: uuid.NewString(), status: http.StatusUnprocessableEntity, code: "ACTOR_NOT_FOUND"},
	}
	for _, test := range permissionCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(
				router,
				http.MethodPost,
				"/api/v1/tasks/"+task.ID+"/assignments",
				[]byte(fmt.Sprintf(`{"role":%q,"actor_id":%q}`, test.role, test.actorID)),
				map[string]string{"If-Match": `"1"`},
			)
			if recorder.Code != test.status || assignmentError(t, recorder.Body.Bytes()).Code != test.code {
				t.Fatalf("permission result = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	var taskVersion int64
	if err := store.SQL.QueryRow("SELECT version FROM tasks WHERE id = ?", task.ID).Scan(&taskVersion); err != nil || taskVersion != 1 {
		t.Fatalf("failed permissions changed task version: version=%d err=%v", taskVersion, err)
	}

	reviewer := createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 1, "")
	assignee := createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 2, "")
	alreadyActive := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, models.BuiltinOwnerActorID)),
		map[string]string{"If-Match": `"3"`},
	)
	if alreadyActive.Code != http.StatusConflict || assignmentError(t, alreadyActive.Body.Bytes()).Code != "ASSIGNMENT_ALREADY_ACTIVE" {
		t.Fatalf("already active assignment = %d: %s", alreadyActive.Code, alreadyActive.Body.String())
	}
	unchanged := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reassign",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"same"}`, person.ID)),
		map[string]string{"If-Match": `"3"`},
	)
	if unchanged.Code != http.StatusConflict || assignmentError(t, unchanged.Body.Bytes()).Code != "ASSIGNMENT_UNCHANGED" {
		t.Fatalf("unchanged assignment = %d: %s", unchanged.Code, unchanged.Body.String())
	}
	staleEnd := performRequest(
		router,
		http.MethodPost,
		"/api/v1/assignments/"+assignee.Assignment.ID+"/end",
		[]byte(`{"reason":"stale"}`),
		map[string]string{"If-Match": `"2"`},
	)
	if staleEnd.Code != http.StatusConflict || assignmentError(t, staleEnd.Body.Bytes()).Code != "VERSION_CONFLICT" {
		t.Fatalf("stale assignment end = %d: %s", staleEnd.Code, staleEnd.Body.String())
	}

	validationCases := []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
		status  int
		code    string
	}{
		{name: "missing version", method: http.MethodPost, path: "/api/v1/tasks/" + task.ID + "/assignments", body: `{"role":"assignee","actor_id":"` + person.ID + `"}`, status: http.StatusPreconditionRequired, code: "VERSION_REQUIRED"},
		{name: "unknown field", method: http.MethodPost, path: "/api/v1/tasks/" + task.ID + "/assignments", body: `{"role":"assignee","actor_id":"` + person.ID + `","assigned_by_actor_id":"` + models.BuiltinOwnerActorID + `"}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusBadRequest, code: "INVALID_JSON"},
		{name: "trailing JSON", method: http.MethodPost, path: "/api/v1/tasks/" + task.ID + "/assignments", body: `{"role":"assignee","actor_id":"` + person.ID + `"}{}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusBadRequest, code: "INVALID_JSON"},
		{name: "invalid role", method: http.MethodPost, path: "/api/v1/tasks/" + task.ID + "/assignments", body: `{"role":"owner","actor_id":"` + person.ID + `"}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "invalid actor id", method: http.MethodPost, path: "/api/v1/tasks/" + task.ID + "/assignments", body: `{"role":"assignee","actor_id":"bad"}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "blank reason", method: http.MethodPost, path: "/api/v1/assignments/" + reviewer.Assignment.ID + "/end", body: `{"reason":"  "}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "long reason", method: http.MethodPost, path: "/api/v1/assignments/" + reviewer.Assignment.ID + "/end", body: `{"reason":"` + strings.Repeat("字", 1_001) + `"}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "invalid idempotency key", method: http.MethodPost, path: "/api/v1/assignments/" + reviewer.Assignment.ID + "/end", body: `{"reason":"end"}`, headers: map[string]string{"If-Match": `"3"`, "Idempotency-Key": "contains space"}, status: http.StatusBadRequest, code: "INVALID_IDEMPOTENCY_KEY"},
		{name: "invalid assignment id", method: http.MethodPost, path: "/api/v1/assignments/not-a-uuid/end", body: `{"reason":"end"}`, headers: map[string]string{"If-Match": `"3"`}, status: http.StatusBadRequest, code: "INVALID_ASSIGNMENT_ID"},
	}
	for _, test := range validationCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, test.method, test.path, []byte(test.body), test.headers)
			if recorder.Code != test.status || assignmentError(t, recorder.Body.Bytes()).Code != test.code {
				t.Fatalf("validation result = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	for _, path := range []string{
		"/api/v1/tasks/" + task.ID + "/assignments?role=owner",
		"/api/v1/tasks/" + task.ID + "/assignments?sort=-unassigned_at",
		"/api/v1/tasks/" + task.ID + "/assignments?page=0",
		"/api/v1/tasks/not-a-uuid/assignments",
	} {
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid assignment list query %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		assignmentError(t, recorder.Body.Bytes())
	}
	var currentTaskVersion int64
	var activeAssigneeCount int64
	if err := store.SQL.QueryRow("SELECT version FROM tasks WHERE id = ?", task.ID).Scan(&currentTaskVersion); err != nil {
		t.Fatalf("read task version after rejected commands: %v", err)
	}
	if err := store.DB.Model(&models.TaskAssignment{}).
		Where("id = ? AND unassigned_at IS NULL", assignee.Assignment.ID).
		Count(&activeAssigneeCount).Error; err != nil {
		t.Fatalf("read assignment after rejected commands: %v", err)
	}
	if currentTaskVersion != 3 || activeAssigneeCount != 1 {
		t.Fatalf("rejected commands were not atomic: task version=%d active assignment=%d", currentTaskVersion, activeAssigneeCount)
	}

	missingAssignment := performRequest(
		router,
		http.MethodPost,
		"/api/v1/assignments/"+uuid.NewString()+"/end",
		[]byte(`{"reason":"missing"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if missingAssignment.Code != http.StatusNotFound || assignmentError(t, missingAssignment.Body.Bytes()).Code != "ASSIGNMENT_NOT_FOUND" {
		t.Fatalf("missing assignment end = %d: %s", missingAssignment.Code, missingAssignment.Body.String())
	}
	emptyTask := createTaskForTaskFacts(t, router, `{"title":"No active assignment"}`)
	missingActive := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+emptyTask.ID+"/reassign",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"none"}`, person.ID)),
		map[string]string{"If-Match": `"1"`},
	)
	if missingActive.Code != http.StatusConflict || assignmentError(t, missingActive.Body.Bytes()).Code != "ASSIGNMENT_NOT_ACTIVE" {
		t.Fatalf("reassign without active assignment = %d: %s", missingActive.Code, missingActive.Body.String())
	}
	endedReviewer := performRequest(
		router,
		http.MethodPost,
		"/api/v1/assignments/"+reviewer.Assignment.ID+"/end",
		[]byte(`{"reason":"review no longer needed"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if endedReviewer.Code != http.StatusOK {
		t.Fatalf("end reviewer = %d: %s", endedReviewer.Code, endedReviewer.Body.String())
	}
	endAgain := performRequest(
		router,
		http.MethodPost,
		"/api/v1/assignments/"+reviewer.Assignment.ID+"/end",
		[]byte(`{"reason":"again"}`),
		map[string]string{"If-Match": `"4"`},
	)
	if endAgain.Code != http.StatusConflict || assignmentError(t, endAgain.Body.Bytes()).Code != "ASSIGNMENT_NOT_ACTIVE" {
		t.Fatalf("end assignment twice = %d: %s", endAgain.Code, endAgain.Body.String())
	}

	doneTask := createTaskForTaskFacts(t, router, `{"title":"Already completed task"}`)
	completed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+doneTask.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"1"`},
	)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete task before assignment rejection = %d: %s", completed.Code, completed.Body.String())
	}
	doneCreate := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+doneTask.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, person.ID)),
		map[string]string{"If-Match": `"2"`},
	)
	if doneCreate.Code != http.StatusConflict || assignmentError(t, doneCreate.Body.Bytes()).Code != "TASK_NOT_ASSIGNABLE" {
		t.Fatalf("assign completed task = %d: %s", doneCreate.Code, doneCreate.Body.String())
	}
	if err := store.DB.Create(&models.TaskAssignment{
		ID: uuid.NewString(), TaskID: doneTask.ID, ActorID: person.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now,
	}).Error; err == nil || !strings.Contains(err.Error(), "TASK_NOT_ASSIGNABLE") {
		t.Fatalf("database allowed assignment on done task: %v", err)
	}
}

func TestCompletingTaskEndsActiveAssignmentsOnce(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Complete assigned task"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Completer"}`, nil)
	assignee := createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 1, "")
	reviewer := createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 2, "")
	roleFiltered := performRequest(
		router,
		http.MethodGet,
		"/api/v1/tasks/"+task.ID+"/assignments?role=assignee",
		nil,
		nil,
	)
	roleData, roleMeta := decodeAssignmentList(t, roleFiltered.Body.Bytes())
	if roleFiltered.Code != http.StatusOK || roleData.Active.Assignee == nil || roleData.Active.Reviewer == nil ||
		len(roleData.History) != 0 || roleMeta.Total != 0 || roleMeta.TaskVersion != 3 {
		t.Fatalf("role filter must only scope ended history: %d data=%#v meta=%#v", roleFiltered.Code, roleData, roleMeta)
	}
	requestID := uuid.NewString()
	done := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "complete-assigned-task", "X-Request-ID": requestID},
	)
	if done.Code != http.StatusOK || done.Header().Get("ETag") != `"4"` {
		t.Fatalf("complete assigned task = %d headers=%v: %s", done.Code, done.Header(), done.Body.String())
	}
	var taskEnvelope struct {
		Data taskLifecycleResponse `json:"data"`
	}
	if err := json.Unmarshal(done.Body.Bytes(), &taskEnvelope); err != nil || taskEnvelope.Data.Task.Version != 4 || taskEnvelope.Data.Task.Status != "done" {
		t.Fatalf("completed task response = %#v err=%v", taskEnvelope.Data.Task, err)
	}

	list := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	data, meta := decodeAssignmentList(t, list.Body.Bytes())
	if list.Code != http.StatusOK || data.Active.Assignee != nil || data.Active.Reviewer != nil ||
		len(data.History) != 2 || meta.Total != 2 || meta.TaskVersion != 4 {
		t.Fatalf("assignments after task completion = %d data=%#v meta=%#v", list.Code, data, meta)
	}
	for _, assignment := range data.History {
		if assignment.IsActive || assignment.UnassignedAt == nil || assignment.Reason == nil || *assignment.Reason != taskCompletedReason {
			t.Fatalf("completion-ended assignment = %#v", assignment)
		}
	}

	var endedEvents []models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = ? AND aggregate_id = ? AND action = ?", "task", task.ID, "assignment_ended",
	).Find(&endedEvents).Error; err != nil {
		t.Fatalf("load completion assignment events: %v", err)
	}
	if len(endedEvents) != 2 {
		t.Fatalf("completion ended event count = %d, want 2", len(endedEvents))
	}
	wantIDs := map[string]bool{assignee.Assignment.ID: true, reviewer.Assignment.ID: true}
	for _, event := range endedEvents {
		if event.AssignmentID == nil || !wantIDs[*event.AssignmentID] || event.RequestID == nil || *event.RequestID != requestID {
			t.Fatalf("completion assignment event = %#v", event)
		}
	}
	doneAgain := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "complete-assigned-task"},
	)
	if doneAgain.Code != http.StatusOK || doneAgain.Header().Get("ETag") != `"4"` || doneAgain.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("repeat task completion = %d headers=%v: %s", doneAgain.Code, doneAgain.Header(), doneAgain.Body.String())
	}
	var repeatedEventCount int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = ? AND aggregate_id = ? AND action = ?", "task", task.ID, "assignment_ended").
		Count(&repeatedEventCount).Error; err != nil || repeatedEventCount != 2 {
		t.Fatalf("repeat completion duplicated assignment events: count=%d err=%v", repeatedEventCount, err)
	}

	assignDone := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, person.ID)),
		map[string]string{"If-Match": `"4"`},
	)
	if assignDone.Code != http.StatusConflict || assignmentError(t, assignDone.Body.Bytes()).Code != "TASK_NOT_ASSIGNABLE" {
		t.Fatalf("assign after completion = %d: %s", assignDone.Code, assignDone.Body.String())
	}
	reopened := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reopen",
		[]byte(`{}`),
		map[string]string{"If-Match": `"4"`},
	)
	if reopened.Code != http.StatusOK || reopened.Header().Get("ETag") != `"5"` {
		t.Fatalf("reopen completed task = %d: %s", reopened.Code, reopened.Body.String())
	}
	afterReopen := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	afterData, afterMeta := decodeAssignmentList(t, afterReopen.Body.Bytes())
	if afterData.Active.Assignee != nil || afterData.Active.Reviewer != nil || afterMeta.TaskVersion != 5 || len(afterData.History) != 2 {
		t.Fatalf("reopen resurrected assignments: %s", afterReopen.Body.String())
	}
}

func TestTaskDeleteKeepsCascadeAndDetachesAssignmentEvents(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Delete assigned task aggregate"}`)
	created := createAssignmentForTest(t, router, task.ID, "assignee", models.BuiltinOwnerActorID, 1, "delete-cascade-assignment")

	staleDelete := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+task.ID,
		nil,
		map[string]string{"If-Match": `"1"`},
	)
	if staleDelete.Code != http.StatusConflict || assignmentError(t, staleDelete.Body.Bytes()).Code != "VERSION_CONFLICT" {
		t.Fatalf("stale task delete = %d: %s", staleDelete.Code, staleDelete.Body.String())
	}
	var beforeDeleteCount int64
	if err := store.DB.Model(&models.TaskAssignment{}).Where("id = ?", created.Assignment.ID).Count(&beforeDeleteCount).Error; err != nil || beforeDeleteCount != 1 {
		t.Fatalf("stale delete changed assignment: count=%d err=%v", beforeDeleteCount, err)
	}

	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+strings.ToUpper(task.ID),
		nil,
		map[string]string{"If-Match": `"2"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete assigned task = %d: %s", deleted.Code, deleted.Body.String())
	}
	var assignmentCount int64
	if err := store.DB.Model(&models.TaskAssignment{}).Where("task_id = ?", task.ID).Count(&assignmentCount).Error; err != nil || assignmentCount != 0 {
		t.Fatalf("task delete did not cascade assignments: count=%d err=%v", assignmentCount, err)
	}
	var event struct {
		AggregateID  string
		AssignmentID sql.NullString
		CurrentJSON  sql.NullString
	}
	if err := store.SQL.QueryRow(`
		SELECT aggregate_id, assignment_id, current_json
		FROM workflow_events
		WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'assignment_created'
	`, task.ID).Scan(&event.AggregateID, &event.AssignmentID, &event.CurrentJSON); err != nil {
		t.Fatalf("load detached assignment event: %v", err)
	}
	if event.AggregateID != task.ID || event.AssignmentID.Valid || !event.CurrentJSON.Valid ||
		!strings.Contains(event.CurrentJSON.String, created.Assignment.ID) ||
		!strings.Contains(event.CurrentJSON.String, `"display_name"`) {
		t.Fatalf("detached assignment event = %#v", event)
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	if missing.Code != http.StatusNotFound || assignmentError(t, missing.Body.Bytes()).Code != "TASK_NOT_FOUND" {
		t.Fatalf("assignments after task delete = %d: %s", missing.Code, missing.Body.String())
	}
}

func seedInferredOwnerAssignment(t *testing.T, store *database.Store, task models.Task) string {
	t.Helper()
	assignmentIDValue := uuid.NewString()
	assignment := models.TaskAssignment{
		ID: assignmentIDValue, TaskID: task.ID, ActorID: models.BuiltinOwnerActorID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: task.CreatedAt, Reason: migrationAssignmentReason,
	}
	if err := store.DB.Create(&assignment).Error; err != nil {
		t.Fatalf("seed inferred assignment: %v", err)
	}
	actorIDValue := models.BuiltinOwnerActorID
	currentJSON := `{"source":"schema_v7_migration","inferred":true,"role":"assignee"}`
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: task.ID,
		Action: "migration_assignment_backfill", ActorID: &actorIDValue, AssignmentID: &assignmentIDValue,
		CurrentJSON: &currentJSON, CreatedAt: task.CreatedAt,
	}
	if err := store.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed migration assignment event: %v", err)
	}
	return assignmentIDValue
}
