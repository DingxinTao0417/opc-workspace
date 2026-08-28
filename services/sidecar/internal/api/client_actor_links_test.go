package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func decodeClientActorLinkResponse(t *testing.T, body []byte) clientActorLinkResponse {
	t.Helper()
	var envelope struct {
		Data clientActorLinkResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode client actor link response: %v: %s", err, body)
	}
	return envelope.Data
}

func createPersonActorForClientLinkTest(t *testing.T, router http.Handler, name string) actorResponse {
	t.Helper()
	recorder := performRequest(router, http.MethodPost, "/api/v1/actors", []byte(`{
		"type":"person","display_name":"`+name+`","notes":"","metadata":{}
	}`), nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create person actor = %d: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data actorResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestClientActorLinkLifecycleCreatePersonReplayAndHistory(t *testing.T) {
	router, store, _ := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Actor Link Client","contact_name":"Casey"}`, nil)
	person := createPersonActorForClientLinkTest(t, router, "Casey")

	linkBody := []byte(`{"actor_id":"` + person.ID + `","role":"contact"}`)
	headers := map[string]string{"If-Match": `"1"`, "Idempotency-Key": "client-link-create-1"}
	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links", linkBody, headers)
	if createdRecorder.Code != http.StatusCreated || createdRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("create client actor link = %d headers=%v: %s", createdRecorder.Code, createdRecorder.Header(), createdRecorder.Body.String())
	}
	created := decodeClientActorLinkResponse(t, createdRecorder.Body.Bytes())
	if created.ClientID != client.ID || created.Actor.ID != person.ID || created.Actor.Type != "person" ||
		created.Role != "contact" || created.UnlinkedAt != nil || created.ClientVersion != 2 {
		t.Fatalf("created client actor link = %#v", created)
	}

	replay := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links", linkBody, headers)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeClientActorLinkResponse(t, replay.Body.Bytes()).ID != created.ID {
		t.Fatalf("client actor link replay = %d headers=%v: %s", replay.Code, replay.Header(), replay.Body.String())
	}
	if got := readAPIInt64(t, store.DB, "SELECT COUNT(*) FROM client_actor_links"); got != 1 {
		t.Fatalf("client actor link count = %d, want 1", got)
	}

	listRecorder := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/actor-links", nil, nil)
	if listRecorder.Code != http.StatusOK || listRecorder.Header().Get("ETag") != `"2"` {
		t.Fatalf("list client actor links = %d headers=%v: %s", listRecorder.Code, listRecorder.Header(), listRecorder.Body.String())
	}
	var listEnvelope struct {
		Data []clientActorLinkResponse `json:"data"`
		Meta struct {
			Total         int64 `json:"total"`
			ClientVersion int64 `json:"client_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data) != 1 ||
		listEnvelope.Meta.Total != 1 || listEnvelope.Meta.ClientVersion != 2 {
		t.Fatalf("client actor link list = %#v err=%v", listEnvelope, err)
	}

	blockedDeactivate := performRequest(router, http.MethodPatch, "/api/v1/actors/"+person.ID,
		[]byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"1"`})
	if blockedDeactivate.Code != http.StatusConflict || responseErrorCode(t, blockedDeactivate.Body.Bytes()) != "ACTOR_HAS_ACTIVE_CLIENT_LINKS" {
		t.Fatalf("deactivate linked actor = %d: %s", blockedDeactivate.Code, blockedDeactivate.Body.String())
	}

	personCountBefore := readAPIInt64(t, store.DB, "SELECT COUNT(*) FROM actors WHERE type = 'person'")
	conflictingCreate := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links",
		[]byte(`{"create_person":{"display_name":"New Contact","notes":"should roll back"},"role":"contact"}`),
		map[string]string{"If-Match": `"2"`})
	if conflictingCreate.Code != http.StatusConflict || responseErrorCode(t, conflictingCreate.Body.Bytes()) != "CLIENT_CONTACT_ACTOR_ALREADY_LINKED" {
		t.Fatalf("second active client actor link = %d: %s", conflictingCreate.Code, conflictingCreate.Body.String())
	}
	if got := readAPIInt64(t, store.DB, "SELECT COUNT(*) FROM actors WHERE type = 'person'"); got != personCountBefore {
		t.Fatalf("failed atomic create left person count %d, want %d", got, personCountBefore)
	}

	stale := performRequest(router, http.MethodDelete, "/api/v1/client-actor-links/"+created.ID+"?confirm=true",
		[]byte(`{"reason":"contact changed"}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale unlink = %d: %s", stale.Code, stale.Body.String())
	}
	unlinkedRecorder := performRequest(router, http.MethodDelete, "/api/v1/client-actor-links/"+created.ID+"?confirm=true",
		[]byte(`{"reason":"contact changed"}`), map[string]string{"If-Match": `"2"`, "Idempotency-Key": "client-link-delete-1"})
	if unlinkedRecorder.Code != http.StatusOK || unlinkedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("unlink client actor = %d headers=%v: %s", unlinkedRecorder.Code, unlinkedRecorder.Header(), unlinkedRecorder.Body.String())
	}
	unlinked := decodeClientActorLinkResponse(t, unlinkedRecorder.Body.Bytes())
	if unlinked.UnlinkedAt == nil || unlinked.UnlinkedBy == nil || unlinked.UnlinkReason == nil ||
		*unlinked.UnlinkReason != "contact changed" || unlinked.ClientVersion != 3 {
		t.Fatalf("unlinked client actor = %#v", unlinked)
	}

	activeList := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/actor-links", nil, nil)
	if activeList.Code != http.StatusOK || !jsonListDataIsEmpty(t, activeList.Body.Bytes()) {
		t.Fatalf("active client actor links after unlink = %d: %s", activeList.Code, activeList.Body.String())
	}
	history := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/actor-links?include_unlinked=true", nil, nil)
	if history.Code != http.StatusOK || jsonListDataIsEmpty(t, history.Body.Bytes()) {
		t.Fatalf("client actor link history = %d: %s", history.Code, history.Body.String())
	}

	deactivated := performRequest(router, http.MethodPatch, "/api/v1/actors/"+person.ID,
		[]byte(`{"status":"inactive"}`), map[string]string{"If-Match": `"1"`})
	if deactivated.Code != http.StatusOK {
		t.Fatalf("deactivate unlinked actor = %d: %s", deactivated.Code, deactivated.Body.String())
	}

	createdPersonRecorder := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links",
		[]byte(`{"create_person":{"display_name":"Morgan","notes":"created from client"},"role":"contact"}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "client-link-create-person-1"})
	if createdPersonRecorder.Code != http.StatusCreated || createdPersonRecorder.Header().Get("ETag") != `"4"` {
		t.Fatalf("create person and link = %d headers=%v: %s", createdPersonRecorder.Code, createdPersonRecorder.Header(), createdPersonRecorder.Body.String())
	}
	createdPersonLink := decodeClientActorLinkResponse(t, createdPersonRecorder.Body.Bytes())
	if createdPersonLink.Actor.DisplayName != "Morgan" || createdPersonLink.Actor.Status != "active" || createdPersonLink.ClientVersion != 4 {
		t.Fatalf("created person link = %#v", createdPersonLink)
	}
	var createdPerson models.Actor
	if err := store.DB.First(&createdPerson, "id = ?", createdPersonLink.Actor.ID).Error; err != nil || createdPerson.Notes != "created from client" {
		t.Fatalf("created linked person = %#v err=%v", createdPerson, err)
	}
}

func TestClientActorLinkRejectsUnavailableActorsAndInvalidRequests(t *testing.T) {
	router, _, _ := newClientAttachmentTestAPI(t)
	client := createClientForTest(t, router, `{"name":"Validation Client"}`, nil)

	owner := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links",
		[]byte(`{"actor_id":"`+models.BuiltinOwnerActorID+`","role":"contact"}`), map[string]string{"If-Match": `"1"`})
	if owner.Code != http.StatusConflict || responseErrorCode(t, owner.Body.Bytes()) != "CLIENT_LINK_ACTOR_UNAVAILABLE" {
		t.Fatalf("owner client link = %d: %s", owner.Code, owner.Body.String())
	}

	both := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links",
		[]byte(`{"actor_id":"`+models.BuiltinOwnerActorID+`","create_person":{"display_name":"Duplicate","notes":""}}`),
		map[string]string{"If-Match": `"1"`})
	if both.Code != http.StatusUnprocessableEntity || responseErrorCode(t, both.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("both actor sources = %d: %s", both.Code, both.Body.String())
	}

	missingVersion := performRequest(router, http.MethodPost, "/api/v1/clients/"+client.ID+"/actor-links",
		[]byte(`{"create_person":{"display_name":"Casey","notes":""}}`), nil)
	if missingVersion.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingVersion.Body.Bytes()) != "VERSION_REQUIRED" {
		t.Fatalf("missing client version = %d: %s", missingVersion.Code, missingVersion.Body.String())
	}

	invalidHistory := performRequest(router, http.MethodGet, "/api/v1/clients/"+client.ID+"/actor-links?include_unlinked=yes", nil, nil)
	if invalidHistory.Code != http.StatusBadRequest || responseErrorCode(t, invalidHistory.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid include_unlinked = %d: %s", invalidHistory.Code, invalidHistory.Body.String())
	}
}

func readAPIInt64(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := db.Raw(query, args...).Scan(&value).Error; err != nil {
		t.Fatalf("query int64: %v", err)
	}
	return value
}
