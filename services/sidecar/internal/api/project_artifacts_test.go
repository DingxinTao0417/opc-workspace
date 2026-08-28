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

	listed := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts?page=1&page_size=1", nil, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list project Artifacts = %d: %s", listed.Code, listed.Body.String())
	}
	items, meta := decodeProjectArtifactList(t, listed.Body.Bytes())
	if len(items) != 1 || meta.Total != 2 || meta.Page != 1 || meta.PageSize != 1 {
		t.Fatalf("project Artifact page = %#v meta=%#v", items, meta)
	}
	if items[0].Artifact.TaskID != task.ID || items[0].Task.Title != "Prepare delivery" ||
		items[0].Task.Status != "waiting_review" || items[0].SubmissionSequence != 1 ||
		items[0].Artifact.SubmissionStatus != "pending_review" {
		t.Fatalf("project Artifact context = %#v", items[0])
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
	deleted := performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+output.Artifacts[0].ID+"?confirm=true", []byte(`{"reason":"Superseded"}`), map[string]string{"If-Match": `"5"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete Artifact = %d: %s", deleted.Code, deleted.Body.String())
	}

	active := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts", nil, nil)
	activeItems, activeMeta := decodeProjectArtifactList(t, active.Body.Bytes())
	if active.Code != http.StatusOK || len(activeItems) != 1 || activeMeta.Total != 1 {
		t.Fatalf("active project Artifacts = %d %#v meta=%#v", active.Code, activeItems, activeMeta)
	}
	history := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID+"/artifacts?include_deleted=true", nil, nil)
	historyItems, historyMeta := decodeProjectArtifactList(t, history.Body.Bytes())
	deletedFound := false
	for _, item := range historyItems {
		deletedFound = deletedFound || item.Artifact.DeletedAt != nil
	}
	if history.Code != http.StatusOK || len(historyItems) != 2 || historyMeta.Total != 2 || !deletedFound {
		t.Fatalf("project Artifact history = %d %#v meta=%#v", history.Code, historyItems, historyMeta)
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
