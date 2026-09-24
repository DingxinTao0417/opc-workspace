package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func clientActionTool(t *testing.T, service *API, generation models.AIGeneration) harness.Tool {
	t.Helper()
	var provider models.AIProvider
	if err := service.db.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := service.aiChatToolRegistry(
		generation.SessionID,
		true,
		&provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "clients", "actions"}},
		generation.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("proposal tool missing")
	}
	return tool
}

func TestAIClientActionBoundariesAndScope(t *testing.T) {
	router, store, service, defaultTool, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Existing customer","status":"lead"}`, nil)
	validCreate := `{"action":"client.create","changes":{"name":"New customer","status":"active"}}`
	if _, err := defaultTool.Execute(t.Context(), []byte(validCreate)); err == nil {
		t.Fatal("client mutation accepted without clients scope")
	}
	tool := clientActionTool(t, service, generation)
	for _, args := range []string{
		`{"action":"client.create","changes":{"status":"lead"}}`,
		`{"action":"client.create","changes":{"name":"Bad email","email":"not-an-email"}}`,
		fmt.Sprintf(`{"action":"client.create","client_id":%q,"changes":{"name":"Targeted create"}}`, client.ID),
		fmt.Sprintf(`{"action":"client.update","client_id":%q,"expected_version":1,"changes":{}}`, client.ID),
		fmt.Sprintf(`{"action":"client.update","client_id":%q,"expected_version":1,"changes":{"unknown":"value"}}`, client.ID),
		fmt.Sprintf(`{"action":"client.delete","client_id":%q,"expected_version":1,"changes":{}}`, client.ID),
		fmt.Sprintf(`{"action":"client.update","client_id":%q,"task_id":%q,"expected_version":1,"changes":{"name":"Mixed targets"}}`, client.ID, client.ID),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIClientCreateAndUpdateRequireConfirmation(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	tool := clientActionTool(t, service, generation)
	create := proposeTestAction(t, store, tool, `{
		"action":"client.create",
		"changes":{"name":"  New customer  ","contact_name":" Ada ","email":"ada@example.com","phone":" +1 555 0100 ","notes":"User supplied note","status":"lead"}
	}`)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE name='New customer'", 0)
	if !strings.Contains(create.PreviewJSON, `"email":"ada@example.com"`) || strings.Contains(create.ActionJSON, "created_at") {
		t.Fatalf("unexpected stored proposal: action=%s preview=%s", create.ActionJSON, create.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+create.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, create.Fingerprint)), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"route":"/clients/`) {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var createdProposal models.AIActionProposal
	if err := store.DB.First(&createdProposal, "id=?", create.ID).Error; err != nil || createdProposal.ResultID == nil {
		t.Fatalf("proposal=%+v err=%v", createdProposal, err)
	}
	clientID := *createdProposal.ResultID
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND name='New customer' AND contact_name='Ada' AND email='ada@example.com' AND status='lead' AND version=1", 1, clientID)

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool = clientActionTool(t, service, generation)
	update := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client.update","client_id":%q,"expected_version":1,"changes":{"name":"Renamed customer","email":null,"status":"inactive"}}`, clientID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND name='New customer' AND email='ada@example.com' AND version=1", 1, clientID)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+update.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, update.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND name='Renamed customer' AND email IS NULL AND status='inactive' AND version=2", 1, clientID)

	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil || output.Data.ResultID == nil || *output.Data.ResultID != clientID || output.Data.ResultVersion == nil || *output.Data.ResultVersion != 2 {
		t.Fatalf("result=%s err=%v", response.Body.String(), err)
	}
}

func TestAIClientDeleteRequiresConsentAndRestoresAttachmentOnRollback(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Delete customer"}`, nil)
	project := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Retained client project","client_id":%q}`, client.ID), nil)
	var current models.Client
	if err := store.DB.First(&current, "id=?", client.ID).Error; err != nil {
		t.Fatal(err)
	}
	upload := performMultipartPartsRequest(router, "/api/v1/clients/"+client.ID+"/attachments", []multipartTestPart{
		{field: "metadata", content: []byte(`{"name":"client-evidence.txt"}`)},
		{field: "file", filename: "client-evidence.txt", content: []byte("client attachment rollback")},
	}, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)}, false)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload=%d %s", upload.Code, upload.Body.String())
	}
	attachment := decodeClientAttachmentResponse(t, upload.Body.Bytes())
	if err := store.DB.First(&current, "id=?", client.ID).Error; err != nil {
		t.Fatal(err)
	}
	inactive := performRequest(router, http.MethodPatch, "/api/v1/clients/"+client.ID, []byte(`{"status":"inactive"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.Version)})
	if inactive.Code != http.StatusOK {
		t.Fatalf("inactive=%d %s", inactive.Code, inactive.Body.String())
	}
	if err := store.DB.First(&current, "id=?", client.ID).Error; err != nil {
		t.Fatal(err)
	}
	tool := clientActionTool(t, service, generation)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client.delete","client_id":%q,"expected_version":%d,"changes":{}}`, client.ID, current.Version))
	for _, expected := range []string{`"client_deleted":true`, `"client_projects_detached":1`, `"client_attachments_deleted":1`, `"client_attachment_files_deleted":1`} {
		if !strings.Contains(proposal.PreviewJSON, expected) {
			t.Fatalf("missing %s in preview=%s", expected, proposal.PreviewJSON)
		}
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + proposal.ID + "/decision"
	withoutConsent := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, withoutConsent, http.StatusUnprocessableEntity, "CLIENT_DELETE_CONFIRMATION_REQUIRED")
	if err := store.DB.Exec(`CREATE TRIGGER reject_client_delete_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_client_delete":true}`, proposal.Fingerprint))
	if response := performRequest(router, http.MethodPost, path, body, nil); response.Code != http.StatusInternalServerError {
		t.Fatalf("rollback=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=?", 1, client.ID)
	download := performRequest(router, http.MethodGet, "/api/v1/client-attachments/"+attachment.ID+"/content", nil, nil)
	if download.Code != http.StatusOK || download.Body.String() != "client attachment rollback" {
		t.Fatalf("restored attachment=%d %q", download.Code, download.Body.String())
	}
	if err := store.DB.Exec("DROP TRIGGER reject_client_delete_approval").Error; err != nil {
		t.Fatal(err)
	}
	confirmed := performRequest(router, http.MethodPost, path, body, nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"route":"/clients"`) {
		t.Fatalf("confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=?", 0, client.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=? AND client_id IS NULL", 1, project.ID)
}

func TestAIClientUpdateRejectsAStalePreview(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Current customer","email":"current@example.com"}`, nil)
	tool := clientActionTool(t, service, generation)
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"client.update","client_id":%q,"expected_version":1,"changes":{"name":"Proposed name"}}`, client.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.Client{}).Where("id=?", client.ID).Updates(map[string]any{"name": "Changed elsewhere", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, response, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id=? AND name='Changed elsewhere' AND email='current@example.com' AND version=2", 1, client.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending' AND result_id IS NULL", 1, proposal.ID)
}
