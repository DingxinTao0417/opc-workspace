package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeProjectArtifactList(t *testing.T, body []byte) ([]projectArtifactOutput, projectArtifactMeta) {
	t.Helper()
	var envelope struct {
		Data []projectArtifactOutput `json:"data"`
		Meta projectArtifactMeta     `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project Artifact list: %v: %s", err, body)
	}
	return envelope.Data, envelope.Meta
}

func TestProjectArtifactAggregationUsesTaskArtifactFacts(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Delivery project"}`, nil)
	task := createTaskForTaskFacts(t, router, fmt.Sprintf(
		`{"title":"Prepare delivery","project_id":%q,"review_policy":"manual"}`,
		project.ID,
	))
	person := createActorForTest(t, router, `{"type":"person","display_name":"Project producer"}`, nil)
	createAssignmentForTest(t, router, task.ID, "assignee", person.ID, task.Version, "")
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, task.Version+1, "")

	submitted := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		[]byte(`{
			"summary":"Project delivery",
			"artifacts":[
				{"client_ref":"brief","storage_kind":"text","name":"Delivery brief","content_text":"Private body","requires_followup":true},
				{"client_ref":"link","storage_kind":"link","name":"Preview link","reference_url":"https://example.com/preview"}
			]
		}`),
		map[string]string{"If-Match": `"3"`},
	)
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit output = %d: %s", submitted.Code, submitted.Body.String())
	}
	output := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	var sourceInbox models.InboxItem
	if err := store.DB.First(&sourceInbox, "source_entity_id = ? AND source_entity_type = 'task_artifact'", output.Artifacts[0].ID).Error; err != nil {
		t.Fatalf("load Artifact source Inbox Item: %v", err)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts?page=1&page_size=2", nil, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list project Artifacts = %d: %s", listed.Code, listed.Body.String())
	}
	items, meta := decodeProjectArtifactList(t, listed.Body.Bytes())
	if len(items) != 2 || meta.Total != 2 || meta.Page != 1 || meta.PageSize != 2 {
		t.Fatalf("project Artifact page = %#v meta=%#v", items, meta)
	}
	var followupItem *projectArtifactOutput
	var nonFollowupItem *projectArtifactOutput
	for index := range items {
		if items[index].Artifact.ID == output.Artifacts[0].ID {
			followupItem = &items[index]
		}
		if items[index].Artifact.ID == output.Artifacts[1].ID {
			nonFollowupItem = &items[index]
		}
	}
	if followupItem == nil || followupItem.Artifact.TaskID != task.ID || followupItem.Task.Title != "Prepare delivery" ||
		followupItem.Task.Status != "waiting_review" || followupItem.SubmissionSequence != 1 ||
		followupItem.Artifact.SubmissionStatus != "pending_review" {
		t.Fatalf("project Artifact context = %#v", followupItem)
	}
	if followupItem.Followup == nil || followupItem.Followup.InboxItemID != sourceInbox.ID ||
		followupItem.Followup.InboxItemVersion != sourceInbox.Version ||
		followupItem.Followup.Status != "open" || followupItem.Followup.ResolutionPolicy != "manual" ||
		followupItem.Followup.SourceDeletedAt != nil || followupItem.Followup.Progress.ActiveTotal != 0 {
		t.Fatalf("project Artifact follow-up = %#v", followupItem.Followup)
	}
	if nonFollowupItem == nil || nonFollowupItem.Artifact.RequiresFollowup || nonFollowupItem.Followup != nil {
		t.Fatalf("non-follow-up project Artifact = %#v", nonFollowupItem)
	}
	if strings.Contains(listed.Body.String(), "Private body") {
		t.Fatalf("project Artifact summary leaked content: %s", listed.Body.String())
	}

	projectRecorder := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
	currentProject := decodeProjectResponse(t, projectRecorder.Body.Bytes())
	if meta.ProjectVersion != currentProject.Version || listed.Header().Get("ETag") != fmt.Sprintf(`"%d"`, currentProject.Version) {
		t.Fatalf("project aggregate version = %d etag=%q, want %d", meta.ProjectVersion, listed.Header().Get("ETag"), currentProject.Version)
	}

	reviewed := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": `"4"`})
	if reviewed.Code != http.StatusOK {
		t.Fatalf("accept output = %d: %s", reviewed.Code, reviewed.Body.String())
	}
	dismissed := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+sourceInbox.ID+"/dismiss", []byte(`{"reason":"delivery accepted"}`), map[string]string{"If-Match": `"1"`})
	if dismissed.Code != http.StatusOK {
		t.Fatalf("dismiss Artifact source Inbox Item = %d: %s", dismissed.Code, dismissed.Body.String())
	}
	dismissedItem := decodeInboxItemData(t, dismissed.Body.Bytes())
	deleted := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+output.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"Superseded"}`), map[string]string{"If-Match": `"5"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete Artifact = %d: %s", deleted.Code, deleted.Body.String())
	}
	var sourceAfterDelete models.InboxItem
	if err := store.DB.First(&sourceAfterDelete, "id = ?", sourceInbox.ID).Error; err != nil {
		t.Fatalf("reload deleted Artifact source Inbox Item: %v", err)
	}

	active := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts", nil, nil)
	activeItems, activeMeta := decodeProjectArtifactList(t, active.Body.Bytes())
	if active.Code != http.StatusOK || len(activeItems) != 1 || activeMeta.Total != 1 {
		t.Fatalf("active project Artifacts = %d %#v meta=%#v", active.Code, activeItems, activeMeta)
	}
	history := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts?include_deleted=true", nil, nil)
	historyItems, historyMeta := decodeProjectArtifactList(t, history.Body.Bytes())
	var deletedFollowup *projectArtifactOutput
	for index := range historyItems {
		if historyItems[index].Artifact.ID == output.Artifacts[0].ID {
			deletedFollowup = &historyItems[index]
			break
		}
	}
	if history.Code != http.StatusOK || len(historyItems) != 2 || historyMeta.Total != 2 || deletedFollowup == nil ||
		deletedFollowup.Artifact.DeletedAt == nil || deletedFollowup.Followup == nil ||
		deletedFollowup.Followup.Status != "dismissed" || deletedFollowup.Followup.SourceDeletedAt == nil ||
		deletedFollowup.Followup.InboxItemVersion != sourceAfterDelete.Version || sourceAfterDelete.Version <= dismissedItem.Version {
		t.Fatalf("project Artifact history = %d %#v meta=%#v", history.Code, historyItems, historyMeta)
	}
}

func TestProjectArtifactAggregationIncludesFollowupTaskProgress(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Follow-up project"}`, nil)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Delivery operator"}`, nil)
	task := createTaskForTaskFacts(t, router, fmt.Sprintf(
		`{"title":"Prepare handoff","project_id":%q,"review_policy":"manual"}`,
		project.ID,
	))
	createAssignmentForTest(t, router, task.ID, "assignee", models.BuiltinOwnerActorID, task.Version, "")
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, task.Version+1, "")

	submitted := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		[]byte(`{"summary":"Handoff","artifacts":[{"client_ref":"handoff","storage_kind":"text","name":"Handoff note","content_text":"Ready","requires_followup":true}]}`),
		map[string]string{"If-Match": `"3"`},
	)
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit output = %d: %s", submitted.Code, submitted.Body.String())
	}
	output := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	var sourceInbox models.InboxItem
	if err := store.DB.First(&sourceInbox, "source_entity_id = ? AND source_entity_type = 'task_artifact'", output.Artifacts[0].ID).Error; err != nil {
		t.Fatalf("load Artifact source Inbox Item: %v", err)
	}
	split := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+sourceInbox.ID+"/split",
		[]byte(fmt.Sprintf(`{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"publish","title":"Publish handoff","project_id":%q,"is_required":true,"assignee_actor_id":%q},{"key":"confirm","title":"Confirm delivery","project_id":%q,"review_policy":"manual","is_required":true,"assignee_actor_id":%q}]}`,
			project.ID, models.BuiltinOwnerActorID, project.ID, person.ID)),
		map[string]string{"If-Match": `"1"`},
	)
	if split.Code != http.StatusCreated {
		t.Fatalf("split follow-up = %d: %s", split.Code, split.Body.String())
	}
	created := decodeInboxSplitResponse(t, split.Body.Bytes())
	if len(created.Created) != 2 || len(created.Created[1].Assignments) != 2 ||
		created.Created[1].Assignments[0].Role != "assignee" || created.Created[1].Assignments[0].ActorID != person.ID ||
		created.Created[1].Assignments[1].Role != "reviewer" || created.Created[1].Assignments[1].ActorID != models.BuiltinOwnerActorID {
		t.Fatalf("manual follow-up assignments = %#v", created.Created)
	}
	completed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+created.Created[0].Task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, created.Created[0].Task.Version)},
	)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete follow-up Task = %d: %s", completed.Code, completed.Body.String())
	}
	submittedFollowup := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+created.Created[1].Task.ID+"/submit-output",
		[]byte(`{"summary":"Ready for owner review","artifacts":[{"client_ref":"proof","storage_kind":"text","name":"Delivery confirmation","content_text":"Ready"}]}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, created.Created[1].Task.Version)},
	)
	if submittedFollowup.Code != http.StatusCreated {
		t.Fatalf("submit follow-up Task = %d: %s", submittedFollowup.Code, submittedFollowup.Body.String())
	}
	followupSubmission := decodeSubmitOutputResponse(t, submittedFollowup.Body.Bytes())
	if len(followupSubmission.Artifacts) != 1 || followupSubmission.Artifacts[0].ProducedByActorID != person.ID {
		t.Fatalf("follow-up produced by = %#v, want %s", followupSubmission.Artifacts, person.ID)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts", nil, nil)
	items, _ := decodeProjectArtifactList(t, listed.Body.Bytes())
	var sourceArtifact *projectArtifactOutput
	for index := range items {
		if items[index].Artifact.ID == output.Artifacts[0].ID {
			sourceArtifact = &items[index]
			break
		}
	}
	if listed.Code != http.StatusOK || len(items) != 2 || sourceArtifact == nil || sourceArtifact.Followup == nil {
		t.Fatalf("project Artifact follow-up list = %d %#v", listed.Code, items)
	}
	followup := sourceArtifact.Followup
	if followup.InboxItemID != sourceInbox.ID || followup.Status != "tracking" ||
		followup.InboxItemVersion < 2 ||
		followup.ResolutionPolicy != "all_required_tasks_done" ||
		followup.Progress.ActiveTotal != 2 || followup.Progress.RequiredTotal != 2 ||
		followup.Progress.RequiredDone != 1 || followup.Progress.RequiredRemaining != 1 ||
		followup.Progress.RequiredBlocked != 0 || followup.Progress.RequiredWaitingReview != 1 ||
		followup.Progress.Percent == nil || *followup.Progress.Percent != 50 || followup.Progress.AllRequiredDone {
		t.Fatalf("project Artifact follow-up progress = %#v", followup)
	}
	reviewed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+created.Created[1].Task.ID+"/review",
		[]byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, followupSubmission.Task.Version)},
	)
	if reviewed.Code != http.StatusOK {
		t.Fatalf("accept follow-up Task = %d: %s", reviewed.Code, reviewed.Body.String())
	}
	resolvedList := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts", nil, nil)
	resolvedItems, _ := decodeProjectArtifactList(t, resolvedList.Body.Bytes())
	var resolvedSource *projectArtifactOutput
	for index := range resolvedItems {
		if resolvedItems[index].Artifact.ID == output.Artifacts[0].ID {
			resolvedSource = &resolvedItems[index]
			break
		}
	}
	if resolvedList.Code != http.StatusOK || len(resolvedItems) != 2 || resolvedSource == nil || resolvedSource.Followup == nil {
		t.Fatalf("resolved Project Artifact follow-up = %d %#v", resolvedList.Code, resolvedItems)
	}
	resolvedFollowup := resolvedSource.Followup
	if resolvedFollowup.Status != "resolved" || resolvedFollowup.Progress.RequiredDone != 2 ||
		resolvedFollowup.Progress.RequiredRemaining != 0 || !resolvedFollowup.Progress.AllRequiredDone ||
		resolvedFollowup.Progress.Percent == nil || *resolvedFollowup.Progress.Percent != 100 {
		t.Fatalf("resolved Project Artifact follow-up progress = %#v", resolvedFollowup)
	}
}

func TestProjectArtifactAggregationValidatesProjectAndFilters(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	invalid := performRequest(router, http.MethodGet, "/api/v1/projects/not-a-uuid/artifacts", nil, nil)
	if invalid.Code != http.StatusBadRequest || responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_PROJECT_ID" {
		t.Fatalf("invalid project id = %d: %s", invalid.Code, invalid.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/projects/018f0000-0000-7000-8000-000000000001/artifacts", nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing project = %d: %s", missing.Code, missing.Body.String())
	}
	badFilter := performRequest(router, http.MethodGet, "/api/v1/projects/018f0000-0000-7000-8000-000000000001/artifacts?include_deleted=maybe", nil, nil)
	if badFilter.Code != http.StatusBadRequest || responseErrorCode(t, badFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid filter = %d: %s", badFilter.Code, badFilter.Body.String())
	}
}
