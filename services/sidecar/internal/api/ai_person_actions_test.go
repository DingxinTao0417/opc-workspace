package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIPersonReadsHidePrivateFieldsAndActionsRejectPrivilegeExpansion(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Visible person","notes":"PRIVATE_PERSON_NOTE","metadata":{"department":"hidden"}}`, nil)
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
	result, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"person","id":%q}`, person.ID)))
	if err != nil || !strings.Contains(result, `"display_name":"Visible person"`) || strings.Contains(result, "PRIVATE_PERSON_NOTE") || strings.Contains(result, "department") {
		t.Fatalf("unsafe person read=%s err=%v", result, err)
	}
	search, ok := registry.Get("workspace_search")
	if !ok {
		t.Fatal("workspace_search missing")
	}
	result, err = search.Execute(context.Background(), []byte(`{"type":"person","query":"Visible","limit":10}`))
	if err != nil || !strings.Contains(result, person.ID) || !strings.Contains(result, `"label":"Visible person"`) || strings.Contains(result, "PRIVATE_PERSON_NOTE") || strings.Contains(result, "department") {
		t.Fatalf("unsafe person search=%s err=%v", result, err)
	}

	for _, args := range []string{
		`{"action":"person.create","changes":{"display_name":"Unsafe","status":"inactive"}}`,
		`{"action":"person.create","changes":{"display_name":"Unsafe","metadata":{"token":"secret"}}}`,
		fmt.Sprintf(`{"action":"person.update","actor_id":%q,"expected_version":1,"changes":{}}`, person.ID),
		fmt.Sprintf(`{"action":"person.update","actor_id":%q,"expected_version":1,"changes":{"display_name":"Owner"}}`, models.BuiltinOwnerActorID),
		fmt.Sprintf(`{"action":"person.update","actor_id":%q,"client_id":%q,"expected_version":1,"changes":{"display_name":"Mixed"}}`, person.ID, uuid.NewString()),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("accepted unsafe person action %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIPersonCreateRequiresConfirmationAndReturnsRealIdentity(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	proposal := proposeTestAction(t, store, tool, `{"action":"person.create","changes":{"display_name":"Ada Lovelace","notes":"用户明确提供"}}`)
	if !strings.Contains(proposal.PreviewJSON, `"person_type":"person"`) || !strings.Contains(proposal.PreviewJSON, `"status":"active"`) {
		t.Fatalf("incomplete preview: %s", proposal.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE display_name=?", 0, "Ada Lovelace")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil || output.Data.ResultID == nil || output.Data.ResultVersion == nil || *output.Data.ResultVersion != 1 || output.Data.Route != "" {
		t.Fatalf("result=%s err=%v", response.Body.String(), err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id=? AND type='person' AND display_name=? AND notes=? AND status='active' AND version=1", 1, *output.Data.ResultID, "Ada Lovelace", "用户明确提供")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='actor' AND aggregate_id=? AND action='actor_created'", 1, *output.Data.ResultID)
}

func TestAIPersonDeactivationRechecksClientLinksAtConfirmation(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Contact person"}`, nil)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"person.update","actor_id":%q,"expected_version":1,"changes":{"status":"inactive"}}`, person.ID))
	client := createClientForTest(t, router, `{"name":"Late linked client"}`, nil)
	if err := store.DB.Create(&models.ClientActorLink{
		ID: uuid.NewString(), ClientID: client.ID, ActorID: person.ID, Role: "contact",
		LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: "2026-09-18T12:00:00Z",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, response, http.StatusConflict, "ACTOR_HAS_ACTIVE_CLIENT_LINKS")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id=? AND status='active' AND version=1", 1, person.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending' AND result_id IS NULL", 1, proposal.ID)
}
