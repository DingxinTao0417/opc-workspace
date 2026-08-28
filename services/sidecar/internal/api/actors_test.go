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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newActorTestAPI(t *testing.T) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "actors-api.db"))
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

func createActorForTest(t *testing.T, router http.Handler, body string, headers map[string]string) actorResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/actors", []byte(body), headers)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create actor status = %d: %s", recorder.Code, recorder.Body.String())
	}
	return decodeActorResponse(t, recorder.Body.Bytes())
}

func decodeActorResponse(t *testing.T, body []byte) actorResponse {
	t.Helper()
	var envelope struct {
		Data actorResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode actor response: %v", err)
	}
	return envelope.Data
}

func decodeActorList(t *testing.T, body []byte) ([]actorResponse, pageMeta) {
	t.Helper()
	var envelope struct {
		Data []actorResponse `json:"data"`
		Meta pageMeta        `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode actor list: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func decodeActorError(t *testing.T, body []byte) errorResponse {
	t.Helper()
	var response errorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode actor error: %v", err)
	}
	if response.RequestID == "" {
		t.Fatalf("actor error has no request_id: %s", body)
	}
	return response
}

func TestActorCreateListDetailIdempotencyAndStableSort(t *testing.T) {
	router, store := newActorTestAPI(t)

	initial := performRequest(router, http.MethodGet, "/api/v1/actors", nil, nil)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial actor list = %d: %s", initial.Code, initial.Body.String())
	}
	initialActors, initialMeta := decodeActorList(t, initial.Body.Bytes())
	if initialMeta.Total != 2 || len(initialActors) != 2 ||
		initialActors[0].ID != models.BuiltinOwnerActorID || initialActors[1].ID != models.BuiltinSystemActorID {
		t.Fatalf("initial actors = %s", initial.Body.String())
	}

	requestID := uuid.NewString()
	body := `{
		"type":"person",
		"display_name":"  Alice  ",
		"notes":"线下联系人",
		"metadata":{"timezone":"UTC","channels":["email"],"level":2}
	}`
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/actors", []byte(body), map[string]string{
		"Idempotency-Key": "actor-alice",
		"X-Request-ID":    requestID,
	})
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("create actor = %d headers=%v: %s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeActorResponse(t, createdRecorder.Body.Bytes())
	if created.Type != "person" || created.DisplayName != "Alice" || created.Status != "active" ||
		created.IsBuiltin || created.Notes != "线下联系人" || created.Version != 1 || created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("created actor = %#v", created)
	}
	if string(created.Metadata) != `{"channels":["email"],"level":2,"timezone":"UTC"}` {
		t.Fatalf("created metadata = %s", created.Metadata)
	}

	replayBody := `{
		"metadata":{"level":2,"channels":["email"],"timezone":"UTC"},
		"notes":"线下联系人",
		"status":"active",
		"display_name":"Alice",
		"type":"person"
	}`
	replayed := performRequest(router, http.MethodPost, "/api/v1/actors", []byte(replayBody), map[string]string{
		"Idempotency-Key": "actor-alice",
	})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" ||
		replayed.Header().Get("ETag") != `"1"` {
		t.Fatalf("replayed actor = %d headers=%v: %s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	replayedActor := decodeActorResponse(t, replayed.Body.Bytes())
	if replayedActor.ID != created.ID || replayedActor.CreatedAt != created.CreatedAt {
		t.Fatalf("replayed actor changed snapshot: %#v vs %#v", replayedActor, created)
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/actors", []byte(`{
		"type":"person","display_name":"Different"
	}`), map[string]string{"Idempotency-Key": "actor-alice"})
	if conflict.Code != http.StatusConflict || decodeActorError(t, conflict.Body.Bytes()).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	bobOne := createActorForTest(t, router, `{"type":"person","display_name":"bob"}`, nil)
	bobTwo := createActorForTest(t, router, `{"type":"person","display_name":"bob"}`, nil)
	inactive := createActorForTest(t, router, `{
		"type":"person","display_name":"Zed","status":"inactive"
	}`, nil)
	if inactive.Status != "inactive" {
		t.Fatalf("inactive actor = %#v", inactive)
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/actors/"+created.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"1"` || decodeActorResponse(t, detail.Body.Bytes()).ID != created.ID {
		t.Fatalf("actor detail = %d headers=%v: %s", detail.Code, detail.Header(), detail.Body.String())
	}
	uppercaseDetail := performRequest(router, http.MethodGet, "/api/v1/actors/"+strings.ToUpper(created.ID), nil, nil)
	if uppercaseDetail.Code != http.StatusOK || decodeActorResponse(t, uppercaseDetail.Body.Bytes()).ID != created.ID {
		t.Fatalf("uppercase actor detail = %d: %s", uppercaseDetail.Code, uppercaseDetail.Body.String())
	}

	filtered := performRequest(
		router,
		http.MethodGet,
		"/api/v1/actors?type=person&status=active&sort=display_name&page=1&page_size=100",
		nil,
		nil,
	)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered actors = %d: %s", filtered.Code, filtered.Body.String())
	}
	actors, meta := decodeActorList(t, filtered.Body.Bytes())
	if meta.Total != 3 || len(actors) != 3 || actors[0].ID != created.ID ||
		actors[1].DisplayName != "bob" || actors[2].DisplayName != "bob" {
		t.Fatalf("filtered actors = %s", filtered.Body.String())
	}
	wantFirstBob, wantSecondBob := bobOne.ID, bobTwo.ID
	if wantFirstBob > wantSecondBob {
		wantFirstBob, wantSecondBob = wantSecondBob, wantFirstBob
	}
	if actors[1].ID != wantFirstBob || actors[2].ID != wantSecondBob {
		t.Fatalf("equal-name actor order is not stable: %s", filtered.Body.String())
	}

	all := performRequest(router, http.MethodGet, "/api/v1/actors", nil, nil)
	allActors, _ := decodeActorList(t, all.Body.Bytes())
	if len(allActors) != 6 || allActors[0].Type != "owner" || allActors[1].DisplayName != "Alice" ||
		allActors[4].DisplayName != "Zed" || allActors[5].Type != "system" {
		t.Fatalf("default actor order = %s", all.Body.String())
	}

	for _, path := range []string{
		"/api/v1/actors?type=team",
		"/api/v1/actors?status=paused",
		"/api/v1/actors?sort=name",
		"/api/v1/actors?page=0",
		"/api/v1/actors?page_size=101",
	} {
		recorder := performRequest(router, http.MethodGet, path, nil, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid actor query %s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		decodeActorError(t, recorder.Body.Bytes())
	}

	deleteRecorder := performRequest(router, http.MethodDelete, "/api/v1/actors/"+created.ID, nil, nil)
	if deleteRecorder.Code != http.StatusMethodNotAllowed || decodeActorError(t, deleteRecorder.Body.Bytes()).Code != "METHOD_NOT_ALLOWED" {
		t.Fatalf("actor delete route = %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	var events []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = ? AND aggregate_id = ?", "actor", created.ID).Find(&events).Error; err != nil {
		t.Fatalf("load actor workflow events: %v", err)
	}
	if len(events) != 1 || events[0].Action != "actor_created" || events[0].RequestID == nil || *events[0].RequestID != requestID {
		t.Fatalf("idempotent actor events = %#v", events)
	}
}

func TestActorCreateAndLookupValidation(t *testing.T) {
	router, _ := newActorTestAPI(t)

	tooLongNotes, _ := json.Marshal(map[string]any{
		"type": "person", "display_name": "Notes", "notes": strings.Repeat("字", 2_001),
	})
	tooLargeMetadata, _ := json.Marshal(map[string]any{
		"type": "person", "display_name": "Metadata", "metadata": map[string]any{"bio": strings.Repeat("x", maxActorMetadataBytes)},
	})
	cases := []struct {
		name   string
		body   []byte
		status int
	}{
		{name: "missing fields", body: []byte(`{}`), status: http.StatusUnprocessableEntity},
		{name: "owner type", body: []byte(`{"type":"owner","display_name":"Owner"}`), status: http.StatusUnprocessableEntity},
		{name: "system type", body: []byte(`{"type":"system","display_name":"System"}`), status: http.StatusUnprocessableEntity},
		{name: "agent type", body: []byte(`{"type":"agent","display_name":"Agent"}`), status: http.StatusUnprocessableEntity},
		{name: "blank name", body: []byte(`{"type":"person","display_name":"  "}`), status: http.StatusUnprocessableEntity},
		{name: "control in name", body: []byte(`{"type":"person","display_name":"A\u0000B"}`), status: http.StatusUnprocessableEntity},
		{name: "null status", body: []byte(`{"type":"person","display_name":"Null","status":null}`), status: http.StatusUnprocessableEntity},
		{name: "invalid status", body: []byte(`{"type":"person","display_name":"Paused","status":"paused"}`), status: http.StatusUnprocessableEntity},
		{name: "null notes", body: []byte(`{"type":"person","display_name":"Null","notes":null}`), status: http.StatusUnprocessableEntity},
		{name: "long notes", body: tooLongNotes, status: http.StatusUnprocessableEntity},
		{name: "null metadata", body: []byte(`{"type":"person","display_name":"Null","metadata":null}`), status: http.StatusUnprocessableEntity},
		{name: "array metadata", body: []byte(`{"type":"person","display_name":"Array","metadata":[]}`), status: http.StatusUnprocessableEntity},
		{name: "sensitive metadata", body: []byte(`{"type":"person","display_name":"Secret","metadata":{"github_token":"x"}}`), status: http.StatusUnprocessableEntity},
		{name: "deep metadata", body: []byte(`{"type":"person","display_name":"Deep","metadata":{"a":{"b":{"c":{"d":{"e":{"f":{}}}}}}}}`), status: http.StatusUnprocessableEntity},
		{name: "large metadata", body: tooLargeMetadata, status: http.StatusUnprocessableEntity},
		{name: "server field", body: []byte(`{"type":"person","display_name":"Builtin","is_builtin":true}`), status: http.StatusBadRequest},
		{name: "trailing JSON", body: []byte(`{"type":"person","display_name":"Two"}{}`), status: http.StatusBadRequest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/actors", test.body, nil)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
			response := decodeActorError(t, recorder.Body.Bytes())
			if test.status == http.StatusUnprocessableEntity && response.Code != "VALIDATION_ERROR" {
				t.Fatalf("error = %#v", response)
			}
			if test.status == http.StatusBadRequest && response.Code != "INVALID_JSON" {
				t.Fatalf("error = %#v", response)
			}
		})
	}

	invalidID := performRequest(router, http.MethodGet, "/api/v1/actors/not-a-uuid", nil, nil)
	if invalidID.Code != http.StatusBadRequest || decodeActorError(t, invalidID.Body.Bytes()).Code != "INVALID_ACTOR_ID" {
		t.Fatalf("invalid actor id = %d: %s", invalidID.Code, invalidID.Body.String())
	}
	notFound := performRequest(router, http.MethodGet, "/api/v1/actors/"+uuid.NewString(), nil, nil)
	if notFound.Code != http.StatusNotFound || decodeActorError(t, notFound.Body.Bytes()).Code != "ACTOR_NOT_FOUND" {
		t.Fatalf("missing actor = %d: %s", notFound.Code, notFound.Body.String())
	}
}

func TestActorPatchPermissionsConcurrencyAuditAndActiveAssignmentGuard(t *testing.T) {
	router, store := newActorTestAPI(t)

	ownerPath := "/api/v1/actors/" + models.BuiltinOwnerActorID
	ownerUpdated := performRequest(
		router,
		http.MethodPatch,
		ownerPath,
		[]byte(`{"display_name":"  Workspace Owner  "}`),
		map[string]string{"If-Match": `"1"`},
	)
	if ownerUpdated.Code != http.StatusOK || ownerUpdated.Header().Get("ETag") != `"2"` {
		t.Fatalf("owner display update = %d headers=%v: %s", ownerUpdated.Code, ownerUpdated.Header(), ownerUpdated.Body.String())
	}
	owner := decodeActorResponse(t, ownerUpdated.Body.Bytes())
	if owner.DisplayName != "Workspace Owner" || owner.Version != 2 || !owner.IsBuiltin || owner.Status != "active" {
		t.Fatalf("updated owner = %#v", owner)
	}
	ownerForbidden := performRequest(
		router,
		http.MethodPatch,
		ownerPath,
		[]byte(`{"display_name":"Must Roll Back","notes":"not allowed"}`),
		map[string]string{"If-Match": `"2"`},
	)
	if ownerForbidden.Code != http.StatusForbidden || decodeActorError(t, ownerForbidden.Body.Bytes()).Code != "ACTOR_FIELD_NOT_EDITABLE" {
		t.Fatalf("owner forbidden fields = %d: %s", ownerForbidden.Code, ownerForbidden.Body.String())
	}
	ownerDetail := performRequest(router, http.MethodGet, ownerPath, nil, nil)
	if persistedOwner := decodeActorResponse(t, ownerDetail.Body.Bytes()); persistedOwner.DisplayName != "Workspace Owner" || persistedOwner.Version != 2 {
		t.Fatalf("forbidden owner update was not atomic: %#v", persistedOwner)
	}

	systemPath := "/api/v1/actors/" + models.BuiltinSystemActorID
	systemForbidden := performRequest(
		router,
		http.MethodPatch,
		systemPath,
		[]byte(`{"display_name":"New System"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if systemForbidden.Code != http.StatusForbidden || decodeActorError(t, systemForbidden.Body.Bytes()).Code != "ACTOR_NOT_EDITABLE" {
		t.Fatalf("system update = %d: %s", systemForbidden.Code, systemForbidden.Body.String())
	}

	person := createActorForTest(t, router, `{"type":"person","display_name":"Casey"}`, nil)
	personPath := "/api/v1/actors/" + person.ID
	missingVersion := performRequest(router, http.MethodPatch, personPath, []byte(`{"notes":"x"}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || decodeActorError(t, missingVersion.Body.Bytes()).Code != "VERSION_REQUIRED" {
		t.Fatalf("missing actor If-Match = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}
	invalidVersion := performRequest(router, http.MethodPatch, personPath, []byte(`{"notes":"x"}`), map[string]string{"If-Match": `"abc"`})
	if invalidVersion.Code != http.StatusBadRequest || decodeActorError(t, invalidVersion.Body.Bytes()).Code != "INVALID_VERSION" {
		t.Fatalf("invalid actor If-Match = %d: %s", invalidVersion.Code, invalidVersion.Body.String())
	}
	stale := performRequest(router, http.MethodPatch, personPath, []byte(`{"notes":"x"}`), map[string]string{"If-Match": `"9"`})
	if stale.Code != http.StatusConflict || decodeActorError(t, stale.Body.Bytes()).Code != "VERSION_CONFLICT" {
		t.Fatalf("stale actor patch = %d: %s", stale.Code, stale.Body.String())
	}
	serverField := performRequest(router, http.MethodPatch, personPath, []byte(`{"type":"owner"}`), map[string]string{"If-Match": `"1"`})
	if serverField.Code != http.StatusBadRequest || decodeActorError(t, serverField.Body.Bytes()).Code != "INVALID_JSON" {
		t.Fatalf("actor type patch = %d: %s", serverField.Code, serverField.Body.String())
	}
	empty := performRequest(router, http.MethodPatch, personPath, []byte(`{}`), map[string]string{"If-Match": `"1"`})
	if empty.Code != http.StatusUnprocessableEntity || decodeActorError(t, empty.Body.Bytes()).Code != "VALIDATION_ERROR" {
		t.Fatalf("empty actor patch = %d: %s", empty.Code, empty.Body.String())
	}

	updateRequestID := uuid.NewString()
	updatedRecorder := performRequest(
		router,
		http.MethodPatch,
		personPath,
		[]byte(`{
			"display_name":"  Casey Jones  ",
			"notes":"Works offline\nOwner records progress.",
			"metadata":{"role":"Designer","contact":{"method":"offline"}},
			"status":"active"
		}`),
		map[string]string{"If-Match": `"1"`, "X-Request-ID": updateRequestID},
	)
	if updatedRecorder.Code != http.StatusOK || updatedRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("person update = %d headers=%v: %s", updatedRecorder.Code, updatedRecorder.Header(), updatedRecorder.Body.String())
	}
	updated := decodeActorResponse(t, updatedRecorder.Body.Bytes())
	if updated.DisplayName != "Casey Jones" || updated.Version != 2 || updated.Status != "active" ||
		updated.Notes != "Works offline\nOwner records progress." ||
		string(updated.Metadata) != `{"contact":{"method":"offline"},"role":"Designer"}` {
		t.Fatalf("updated person = %#v metadata=%s", updated, updated.Metadata)
	}

	task := createTaskForTaskFacts(t, router, `{"title":"Assigned person task"}`)
	assignedAt := time.Now().UTC().Format(time.RFC3339Nano)
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: task.ID, ActorID: person.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: assignedAt,
		Reason: "actor API guard test",
	}
	if err := store.DB.Create(&assignment).Error; err != nil {
		t.Fatalf("create active assignment: %v", err)
	}
	blockedRequestID := uuid.NewString()
	blocked := performRequest(
		router,
		http.MethodPatch,
		personPath,
		[]byte(`{"status":"inactive"}`),
		map[string]string{"If-Match": `"2"`, "X-Request-ID": blockedRequestID},
	)
	if blocked.Code != http.StatusConflict || decodeActorError(t, blocked.Body.Bytes()).Code != "ACTOR_HAS_ACTIVE_ASSIGNMENTS" {
		t.Fatalf("active assignment actor deactivation = %d: %s", blocked.Code, blocked.Body.String())
	}
	afterBlocked := performRequest(router, http.MethodGet, personPath, nil, nil)
	blockedActor := decodeActorResponse(t, afterBlocked.Body.Bytes())
	if blockedActor.Status != "active" || blockedActor.Version != 2 {
		t.Fatalf("blocked deactivation changed actor: %#v", blockedActor)
	}

	unassignedAt := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	if err := store.DB.Model(&models.TaskAssignment{}).
		Where("id = ?", assignment.ID).
		Updates(map[string]any{"unassigned_at": unassignedAt, "reason": "ended before deactivation"}).Error; err != nil {
		t.Fatalf("end active assignment: %v", err)
	}
	deactivateRequestID := uuid.NewString()
	deactivatedRecorder := performRequest(
		router,
		http.MethodPatch,
		personPath,
		[]byte(`{"status":"inactive"}`),
		map[string]string{"If-Match": `"2"`, "X-Request-ID": deactivateRequestID},
	)
	if deactivatedRecorder.Code != http.StatusOK || deactivatedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("deactivate actor = %d headers=%v: %s", deactivatedRecorder.Code, deactivatedRecorder.Header(), deactivatedRecorder.Body.String())
	}
	deactivated := decodeActorResponse(t, deactivatedRecorder.Body.Bytes())
	if deactivated.Status != "inactive" || deactivated.Version != 3 {
		t.Fatalf("deactivated actor = %#v", deactivated)
	}
	reactivatedRecorder := performRequest(
		router,
		http.MethodPatch,
		personPath,
		[]byte(`{"status":"active"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if reactivatedRecorder.Code != http.StatusOK || decodeActorResponse(t, reactivatedRecorder.Body.Bytes()).Version != 4 {
		t.Fatalf("reactivate actor = %d: %s", reactivatedRecorder.Code, reactivatedRecorder.Body.String())
	}

	var events []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = ? AND aggregate_id = ?", "actor", person.ID).
		Order("created_at ASC, id ASC").Find(&events).Error; err != nil {
		t.Fatalf("load person workflow events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("person workflow event count = %d, want 4: %#v", len(events), events)
	}
	var foundUpdate, foundDeactivation bool
	for _, event := range events {
		if event.Action == "actor_updated" && event.RequestID != nil && *event.RequestID == updateRequestID {
			foundUpdate = event.PreviousJSON != nil && event.CurrentJSON != nil
		}
		if event.Action == "actor_deactivated" && event.RequestID != nil && *event.RequestID == deactivateRequestID {
			foundDeactivation = event.PreviousJSON != nil && event.CurrentJSON != nil
		}
		if event.RequestID != nil && *event.RequestID == blockedRequestID {
			t.Fatalf("failed deactivation wrote workflow event: %#v", event)
		}
	}
	if !foundUpdate || !foundDeactivation {
		t.Fatalf("actor workflow events missing update/deactivation: %#v", events)
	}

	var currentMetadata map[string]any
	if err := json.Unmarshal(updated.Metadata, &currentMetadata); err != nil {
		t.Fatalf("updated metadata is not an object: %v", err)
	}
	if fmt.Sprint(currentMetadata["role"]) != "Designer" {
		t.Fatalf("updated metadata = %#v", currentMetadata)
	}
}
