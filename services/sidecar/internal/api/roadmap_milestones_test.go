package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func decodeRoadmapMilestoneResponse(t *testing.T, body []byte) roadmapMilestoneResponse {
	t.Helper()
	var envelope struct {
		Data roadmapMilestoneResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode roadmap milestone response: %v", err)
	}
	return envelope.Data
}

func decodeRoadmapMilestoneList(t *testing.T, body []byte) ([]roadmapMilestoneResponse, pageMeta) {
	t.Helper()
	var envelope struct {
		Data []roadmapMilestoneResponse `json:"data"`
		Meta pageMeta                   `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode roadmap milestone list: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func TestRoadmapMilestoneLifecycleFiltersAssociationsAndConcurrency(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	firstProject := createProjectForTest(t, router, `{"name":"Alpha roadmap project"}`, nil)
	secondProject := createProjectForTest(t, router, `{"name":"Beta roadmap project"}`, nil)

	invalid := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Wrong period","year":2026,"quarter":4,"target_date":"2026-09-30"
	}`), nil)
	if invalid.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalid.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid period = %d: %s", invalid.Code, invalid.Body.String())
	}

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(fmt.Sprintf(`{
		"title":"  Ship the local roadmap  ","description":"  first delivery  ",
		"year":2026,"quarter":4,"target_date":"2026-12-15","status":"active",
		"project_ids":[%q,%q]
	}`, firstProject.ID, secondProject.ID)), nil)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create roadmap milestone = %d etag=%q: %s", createdRecorder.Code, createdRecorder.Header().Get("ETag"), createdRecorder.Body.String())
	}
	created := decodeRoadmapMilestoneResponse(t, createdRecorder.Body.Bytes())
	if created.Title != "Ship the local roadmap" || created.Description == nil || *created.Description != "first delivery" || created.Version != 1 || len(created.Projects) != 2 || created.Projects[0].ID != firstProject.ID {
		t.Fatalf("created roadmap milestone = %#v", created)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?year=2026&quarter=4&project_id="+firstProject.ID, nil, nil)
	items, meta := decodeRoadmapMilestoneList(t, listed.Body.Bytes())
	if listed.Code != http.StatusOK || meta.Total != 1 || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("filtered roadmap milestones = %d %#v meta=%#v", listed.Code, items, meta)
	}

	updatedRecorder := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(fmt.Sprintf(`{
		"title":"Ship roadmap API","project_ids":[%q]
	}`, secondProject.ID)), map[string]string{"If-Match": `"1"`})
	if updatedRecorder.Code != http.StatusOK || updatedRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("update roadmap milestone = %d: %s", updatedRecorder.Code, updatedRecorder.Body.String())
	}
	updated := decodeRoadmapMilestoneResponse(t, updatedRecorder.Body.Bytes())
	if updated.Title != "Ship roadmap API" || updated.Version != 2 || len(updated.Projects) != 1 || updated.Projects[0].ID != secondProject.ID {
		t.Fatalf("updated roadmap milestone = %#v", updated)
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(`{"title":"old writer"}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale roadmap milestone update = %d: %s", stale.Code, stale.Body.String())
	}

	archivedRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+created.ID+"/archive", nil, map[string]string{"If-Match": `"2"`})
	archived := decodeRoadmapMilestoneResponse(t, archivedRecorder.Body.Bytes())
	if archivedRecorder.Code != http.StatusOK || archived.Status != "archived" || archived.ArchivedFromStatus == nil || *archived.ArchivedFromStatus != "active" || archived.Version != 3 {
		t.Fatalf("archive roadmap milestone = %d %#v", archivedRecorder.Code, archived)
	}
	defaultList := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?year=2026&quarter=4", nil, nil)
	defaultItems, _ := decodeRoadmapMilestoneList(t, defaultList.Body.Bytes())
	if defaultList.Code != http.StatusOK || len(defaultItems) != 0 {
		t.Fatalf("default roadmap list should exclude archived = %d %#v", defaultList.Code, defaultItems)
	}
	restoredRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+created.ID+"/restore", nil, map[string]string{"If-Match": `"3"`})
	restored := decodeRoadmapMilestoneResponse(t, restoredRecorder.Body.Bytes())
	if restoredRecorder.Code != http.StatusOK || restored.Status != "active" || restored.ArchivedFromStatus != nil || restored.Version != 4 {
		t.Fatalf("restore roadmap milestone = %d %#v", restoredRecorder.Code, restored)
	}

	secondRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Second milestone","year":2026,"quarter":4,"target_date":"2026-12-20"
	}`), nil)
	if secondRecorder.Code != http.StatusCreated {
		t.Fatalf("create second roadmap milestone = %d: %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	second := decodeRoadmapMilestoneResponse(t, secondRecorder.Body.Bytes())
	reorderedRecorder := performRequest(router, http.MethodPut, "/api/v1/roadmap/milestones/reorder", []byte(fmt.Sprintf(`{"items":[
		{"id":%q,"expected_version":1},{"id":%q,"expected_version":4}
	]}`, second.ID, restored.ID)), nil)
	if reorderedRecorder.Code != http.StatusOK {
		t.Fatalf("reorder roadmap milestones = %d: %s", reorderedRecorder.Code, reorderedRecorder.Body.String())
	}
	var reorderEnvelope struct {
		Data []roadmapMilestoneResponse `json:"data"`
	}
	if err := json.Unmarshal(reorderedRecorder.Body.Bytes(), &reorderEnvelope); err != nil {
		t.Fatalf("decode reordered milestones: %v", err)
	}
	reordered := reorderEnvelope.Data
	if len(reordered) != 2 || reordered[0].ID != second.ID || reordered[0].ManualOrder != roadmapMilestoneOrderStep || reordered[1].ID != restored.ID || reordered[1].ManualOrder != 2*roadmapMilestoneOrderStep {
		t.Fatalf("reordered roadmap milestones = %#v", reordered)
	}

	archiveForDelete := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+second.ID+"/archive", nil, map[string]string{"If-Match": `"2"`})
	if archiveForDelete.Code != http.StatusOK {
		t.Fatalf("archive second milestone = %d: %s", archiveForDelete.Code, archiveForDelete.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/roadmap/milestones/"+second.ID+"?confirm=true", nil, map[string]string{"If-Match": `"3"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete archived roadmap milestone = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestRoadmapMilestoneRejectsInvalidMutations(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	invalidFilter := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?quarter=5", nil, nil)
	if invalidFilter.Code != http.StatusBadRequest || responseErrorCode(t, invalidFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid roadmap filter = %d: %s", invalidFilter.Code, invalidFilter.Body.String())
	}
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Guarded milestone","year":2026,"quarter":1,"target_date":"2026-01-15"
	}`), nil)
	created := decodeRoadmapMilestoneResponse(t, createdRecorder.Body.Bytes())
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create guarded milestone = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	missingVersion := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(`{"status":"archived"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing roadmap version = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	directArchive := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": `"1"`})
	if directArchive.Code != http.StatusUnprocessableEntity || responseErrorCode(t, directArchive.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("direct roadmap archive = %d: %s", directArchive.Code, directArchive.Body.String())
	}
	missingConfirm := performRequest(router, http.MethodDelete, "/api/v1/roadmap/milestones/"+created.ID, nil, map[string]string{"If-Match": `"1"`})
	if missingConfirm.Code != http.StatusUnprocessableEntity || responseErrorCode(t, missingConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("missing roadmap delete confirm = %d: %s", missingConfirm.Code, missingConfirm.Body.String())
	}
}

func TestRoadmapMilestonePeriodMoveAppendsToDestinationOrder(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	destinationRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Existing destination","year":2026,"quarter":2,"target_date":"2026-05-10"
	}`), nil)
	destination := decodeRoadmapMilestoneResponse(t, destinationRecorder.Body.Bytes())
	if destinationRecorder.Code != http.StatusCreated || destination.ManualOrder != roadmapMilestoneOrderStep {
		t.Fatalf("create destination milestone = %d %#v", destinationRecorder.Code, destination)
	}
	movingRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Move between quarters","year":2026,"quarter":1,"target_date":"2026-03-20"
	}`), nil)
	moving := decodeRoadmapMilestoneResponse(t, movingRecorder.Body.Bytes())
	if movingRecorder.Code != http.StatusCreated {
		t.Fatalf("create moving milestone = %d: %s", movingRecorder.Code, movingRecorder.Body.String())
	}

	movedRecorder := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+moving.ID, []byte(`{
		"year":2026,"quarter":2,"target_date":"2026-06-30"
	}`), map[string]string{"If-Match": `"1"`})
	moved := decodeRoadmapMilestoneResponse(t, movedRecorder.Body.Bytes())
	if movedRecorder.Code != http.StatusOK || moved.Version != 2 || moved.Quarter != 2 || moved.ManualOrder != 2*roadmapMilestoneOrderStep {
		t.Fatalf("move milestone period = %d %#v", movedRecorder.Code, moved)
	}

	destinationList := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?year=2026&quarter=2", nil, nil)
	destinationItems, _ := decodeRoadmapMilestoneList(t, destinationList.Body.Bytes())
	if destinationList.Code != http.StatusOK || len(destinationItems) != 2 || destinationItems[0].ID != destination.ID || destinationItems[1].ID != moving.ID {
		t.Fatalf("destination order after move = %d %#v", destinationList.Code, destinationItems)
	}
	sourceList := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?year=2026&quarter=1", nil, nil)
	sourceItems, _ := decodeRoadmapMilestoneList(t, sourceList.Body.Bytes())
	if sourceList.Code != http.StatusOK || len(sourceItems) != 0 {
		t.Fatalf("source quarter after move = %d %#v", sourceList.Code, sourceItems)
	}
}

func TestProjectDeleteExplainsRoadmapMilestoneProtection(t *testing.T) {
	router, _ := newProjectTestAPI(t)
	project := createProjectForTest(t, router, `{"name":"Protected roadmap project"}`, nil)
	milestone := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(fmt.Sprintf(`{
		"title":"Protect project","year":2026,"quarter":2,"target_date":"2026-06-30","project_ids":[%q]
	}`, project.ID)), nil)
	if milestone.Code != http.StatusCreated {
		t.Fatalf("create protected milestone = %d: %s", milestone.Code, milestone.Body.String())
	}
	archivedProject := transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"archive"}`)
	deleted := performRequest(router, http.MethodDelete, "/api/v1/projects/"+project.ID+"?confirm=true", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, archivedProject.Version)})
	if deleted.Code != http.StatusConflict || responseErrorCode(t, deleted.Body.Bytes()) != "PROJECT_ROADMAP_MILESTONES_EXIST" {
		t.Fatalf("protected project deletion = %d: %s", deleted.Code, deleted.Body.String())
	}
}
