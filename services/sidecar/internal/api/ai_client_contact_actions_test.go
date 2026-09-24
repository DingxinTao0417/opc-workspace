package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIClientContactActionsRequireScopeAndExistingActivePerson(t *testing.T) {
	router, store, service, defaultTool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Contact target"}`, nil)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Local person","notes":"PRIVATE_ACTOR_NOTE"}`, nil)
	valid := fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"expected_version":1,"changes":{"actor_id":%q}}`, client.ID, person.ID)
	if _, err := defaultTool.Execute(t.Context(), []byte(valid)); err == nil {
		t.Fatal("Client contact mutation accepted without clients scope")
	}
	tool := clientActionTool(t, service, generation)
	for _, args := range []string{
		fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"expected_version":1,"changes":{"actor_id":%q,"reason":"hidden"}}`, client.ID, person.ID),
		fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"client_actor_link_id":%q,"expected_version":1,"changes":{"actor_id":%q}}`, client.ID, uuid.NewString(), person.ID),
		fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"expected_version":1,"changes":{"actor_id":%q}}`, client.ID, models.BuiltinOwnerActorID),
		fmt.Sprintf(`{"action":"client_contact.unlink","client_id":%q,"client_actor_link_id":%q,"expected_version":1,"changes":{}}`, client.ID, uuid.NewString()),
		fmt.Sprintf(`{"action":"client_contact.unlink","client_id":%q,"client_actor_link_id":%q,"expected_version":1,"changes":{"reason":"x","actor_id":%q}}`, client.ID, uuid.NewString(), person.ID),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIClientContactLinkAndUnlinkRequireConfirmation(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Contact target","notes":"CLIENT_NOTE"}`, nil)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Local person","notes":"PRIVATE_ACTOR_NOTE"}`, nil)
	tool := clientActionTool(t, service, generation)
	link := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"expected_version":1,"changes":{"actor_id":%q}}`, client.ID, person.ID))
	if strings.Contains(link.PreviewJSON, "PRIVATE_ACTOR_NOTE") || !strings.Contains(link.PreviewJSON, `"link_state":"active"`) {
		t.Fatalf("unsafe or incomplete preview: %s", link.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_actor_links WHERE client_id=?", 0, client.ID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+link.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, link.Fingerprint)), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"route":"/clients/`+client.ID+`"`) {
		t.Fatalf("link confirm=%d %s", response.Code, response.Body.String())
	}
	var linked models.ClientActorLink
	if err := store.DB.Where("client_id=? AND actor_id=? AND unlinked_at IS NULL", client.ID, person.ID).Take(&linked).Error; err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND version=2", 1, client.ID)

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool = clientActionTool(t, service, generation)
	unlink := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_contact.unlink","client_id":%q,"client_actor_link_id":%q,"expected_version":2,"changes":{"reason":"联系人职责已变更"}}`, client.ID, linked.ID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_actor_links WHERE id=? AND unlinked_at IS NULL", 1, linked.ID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+unlink.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, unlink.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unlink confirm=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_actor_links WHERE id=? AND unlinked_at IS NOT NULL AND unlink_reason='联系人职责已变更'", 1, linked.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id=? AND status='active'", 1, person.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND version=3", 1, client.ID)

	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil || output.Data.ResultID == nil || *output.Data.ResultID != linked.ID || output.Data.ResultVersion == nil || *output.Data.ResultVersion != 3 {
		t.Fatalf("result=%s err=%v", response.Body.String(), err)
	}
}

func TestAIClientContextIncludesSafeActiveContactIdentity(t *testing.T) {
	router, store, _, _, _ := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Context target","notes":"CLIENT_NOTE"}`, nil)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Visible identity","notes":"PRIVATE_ACTOR_NOTE"}`, nil)
	if err := store.DB.Create(&models.ClientActorLink{
		ID: uuid.NewString(), ClientID: client.ID, ActorID: person.ID, Role: "contact",
		LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: "2026-09-19T00:00:00Z",
	}).Error; err != nil {
		t.Fatal(err)
	}
	source, err := loadAIClientContext(t.Context(), store.DB, client.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(source)
	text := string(encoded)
	if !strings.Contains(text, `"contact_actor_name":"Visible identity"`) || !strings.Contains(text, `"notes":"CLIENT_NOTE"`) || strings.Contains(text, "PRIVATE_ACTOR_NOTE") {
		t.Fatalf("unexpected Client context: %s", text)
	}
}

func TestAIClientContactConfirmationRejectsChangedPersonIdentity(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Contact target"}`, nil)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Original person"}`, nil)
	tool := clientActionTool(t, service, generation)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client_contact.link","client_id":%q,"expected_version":1,"changes":{"actor_id":%q}}`, client.ID, person.ID))
	if err := store.DB.Model(&models.Actor{}).Where("id=?", person.ID).Updates(map[string]any{"display_name": "Renamed person", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, response, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_actor_links WHERE client_id=?", 0, client.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending' AND result_id IS NULL", 1, proposal.ID)
}
