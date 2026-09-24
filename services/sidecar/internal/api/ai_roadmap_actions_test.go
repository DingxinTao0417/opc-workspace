package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIRoadmapMilestoneActionBoundaries(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Boundary milestone","year":2026,"quarter":4,"target_date":"2026-10-10","status":"planned","project_ids":[]
	}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	milestone := decodeRoadmapMilestoneResponse(t, created.Body.Bytes())
	for _, args := range []string{
		`{"action":"roadmap_milestone.create","changes":{"title":"Missing period"}}`,
		`{"action":"roadmap_milestone.create","roadmap_milestone_id":"00000000-0000-4000-8000-000000000001","changes":{"title":"Bad target","year":2026,"quarter":4,"target_date":"2026-10-10"}}`,
		`{"action":"roadmap_milestone.create","changes":{"title":"Wrong quarter","year":2026,"quarter":3,"target_date":"2026-10-10"}}`,
		`{"action":"roadmap_milestone.create","changes":{"title":"Archived","year":2026,"quarter":4,"target_date":"2026-10-10","status":"archived"}}`,
		fmt.Sprintf(`{"action":"roadmap_milestone.update","roadmap_milestone_id":%q,"expected_version":1,"changes":{}}`, milestone.ID),
		fmt.Sprintf(`{"action":"roadmap_milestone.update","roadmap_milestone_id":%q,"expected_version":1,"changes":{"manual_order":1}}`, milestone.ID),
		fmt.Sprintf(`{"action":"roadmap_milestone.archive","roadmap_milestone_id":%q,"expected_version":1,"changes":{"reason":"model choice"}}`, milestone.ID),
		fmt.Sprintf(`{"action":"roadmap_milestone.update","project_id":%q,"roadmap_milestone_id":%q,"expected_version":1,"changes":{"title":"Mixed targets"}}`, milestone.ID, milestone.ID),
		fmt.Sprintf(`{"action":"roadmap_milestone.delete","roadmap_milestone_id":%q,"expected_version":1,"changes":{}}`, milestone.ID),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIRoadmapMilestoneDeleteRequiresExactPreviewAndSeparateConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Preserved roadmap project"}`, nil)
	created := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(fmt.Sprintf(`{
		"title":"Archived roadmap delivery","year":2026,"quarter":3,
		"target_date":"2026-09-18","status":"active","project_ids":[%q]
	}`, project.ID)), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create milestone=%d %s", created.Code, created.Body.String())
	}
	milestone := decodeRoadmapMilestoneResponse(t, created.Body.Bytes())
	archived := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+milestone.ID+"/archive", nil, map[string]string{"If-Match": `"1"`})
	if archived.Code != http.StatusOK {
		t.Fatalf("archive milestone=%d %s", archived.Code, archived.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='roadmap_milestone' AND source_entity_id=? AND status='resolved'", 1, milestone.ID)

	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"roadmap_milestone.delete","roadmap_milestone_id":%q,"expected_version":2,"changes":{}}`, milestone.ID))
	if !strings.Contains(proposal.PreviewJSON, `"roadmap_milestone_deleted":true`) ||
		!strings.Contains(proposal.PreviewJSON, `"roadmap_milestone_project_links_deleted":1`) ||
		!strings.Contains(proposal.PreviewJSON, `"roadmap_milestone_inbox_sources_marked":1`) {
		t.Fatalf("delete preview=%s", proposal.PreviewJSON)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	missingConsent := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, missingConsent, http.StatusUnprocessableEntity, "ROADMAP_MILESTONE_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=?", 1, milestone.ID)

	if err := store.DB.Exec(`CREATE TRIGGER fail_roadmap_ai_confirmation BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type = 'ai_action_proposal' AND NEW.action = 'ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_CONFIRMATION_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_roadmap_milestone_delete":true}`, proposal.Fingerprint)), nil)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("forced failure=%d %s", failed.Code, failed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=?", 1, milestone.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestone_projects WHERE milestone_id=?", 1, milestone.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='roadmap_milestone' AND source_entity_id=? AND source_deleted_at IS NULL", 1, milestone.ID)
	if err := store.DB.Exec("DROP TRIGGER fail_roadmap_ai_confirmation").Error; err != nil {
		t.Fatal(err)
	}

	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_roadmap_milestone_delete":true}`, proposal.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"route":"/roadmap"`) {
		t.Fatalf("confirm deletion=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=?", 0, milestone.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestone_projects WHERE milestone_id=?", 0, milestone.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id=?", 1, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='roadmap_milestone' AND source_entity_id=? AND source_deleted_at IS NOT NULL", 1, milestone.ID)
}

func TestAIRoadmapMilestoneChatUsesProposalToolAndHumanReceipt(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls == 1 {
			if !strings.Contains(string(encoded), `"workspace_guide"`) || strings.Contains(string(encoded), `"roadmap_milestone.create"`) {
				t.Error("roadmap proposal schema loaded before domain guidance")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "roadmap-proposal", "type": "function", "function": map[string]any{"name": "workspace_propose", "arguments": `{"action":"roadmap_milestone.create","changes":{"title":"Harness milestone","year":2026,"quarter":3,"target_date":"2026-09-30"}}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if !strings.Contains(string(encoded), "NOT executed") {
			t.Error("proposal result did not preserve confirmation boundary")
		}
		streamMockAIDelta(w, `请核对下方路线图操作卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "roadmap-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "Create a roadmap milestone", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat=%d calls=%d %s", response.Code, calls, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones", 0)
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/roadmap?milestone=") {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE title='Harness milestone' AND status='planned'", 1)
}

func TestAIRoadmapMilestoneCreateAndUpdateRequireConfirmation(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"Roadmap project"}`, nil)
	create := proposeTestAction(t, store, tool, fmt.Sprintf(`{
		"action":"roadmap_milestone.create",
		"changes":{"title":"AI delivery milestone","description":"Frozen review copy","year":2026,"quarter":3,"target_date":"2026-09-30","status":"active","project_ids":[%q]}
	}`, project.ID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, create.Fingerprint))
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+create.ID+"/decision", decision, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/roadmap?milestone=") {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal, "id = ?", create.ID).Error; err != nil || proposal.ResultID == nil || proposal.ResultVersion == nil {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND title=? AND status='active' AND version=1", 1, *proposal.ResultID, "AI delivery milestone")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestone_projects WHERE milestone_id=? AND project_id=?", 1, *proposal.ResultID, project.ID)
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
	read, err := get.Execute(t.Context(), []byte(fmt.Sprintf(`{"type":"roadmap_milestone","id":%q}`, *proposal.ResultID)))
	if err != nil || !strings.Contains(read, `"year":2026`) || !strings.Contains(read, `"quarter":3`) || !strings.Contains(read, `"target_date":"2026-09-30"`) || !strings.Contains(read, project.ID) || !strings.Contains(read, `"task_total":0`) {
		t.Fatalf("workspace_get=%s err=%v", read, err)
	}
	// The proposal receipt contains only IDs/routes, not private milestone text.
	receipts, err := service.aiActionReceipts(t.Context(), generation.SessionID)
	if err != nil || !strings.Contains(receipts, "/roadmap?milestone=") || strings.Contains(receipts, "Frozen review copy") {
		t.Fatalf("receipts=%s err=%v", receipts, err)
	}

	// A new generation owns the edit proposal. No write occurs until its card is approved.
	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	workspaceTool := tool.(*aiWorkspaceTool)
	workspaceTool.generationID = generation.ID
	update := proposeTestAction(t, store, tool, fmt.Sprintf(`{
		"action":"roadmap_milestone.update","roadmap_milestone_id":%q,"expected_version":1,
		"changes":{"title":"AI delivery complete","status":"achieved","project_ids":[]}
	}`, *proposal.ResultID))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND title=? AND status='active' AND version=1", 1, *proposal.ResultID, "AI delivery milestone")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+update.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, update.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("update=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND title=? AND status='achieved' AND version=2", 1, *proposal.ResultID, "AI delivery complete")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestone_projects WHERE milestone_id=?", 0, *proposal.ResultID)
}

func TestAIRoadmapMilestoneApprovalRejectsChangedPreviewAndSupportsArchiveRestore(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Lifecycle milestone","year":2026,"quarter":4,"target_date":"2026-10-10","status":"planned","project_ids":[]
	}`), nil)
	milestone := decodeRoadmapMilestoneResponse(t, created.Body.Bytes())
	archive := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"roadmap_milestone.archive","roadmap_milestone_id":%q,"expected_version":1,"changes":{}}`, milestone.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	// Associations have their own table and do not bump the milestone version;
	// the frozen preview must still reject a widened lifecycle snapshot.
	project := createProjectForTest(t, router, `{"name":"Added after preview"}`, nil)
	if err := store.DB.Create(&models.RoadmapMilestoneProject{MilestoneID: milestone.ID, ProjectID: project.ID, LinkedAt: milestone.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+archive.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, archive.Fingerprint)), nil), http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND status='planned' AND version=1", 1, milestone.ID)

	if err := store.DB.Where("milestone_id=?", milestone.ID).Delete(&models.RoadmapMilestoneProject{}).Error; err != nil {
		t.Fatal(err)
	}
	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	archive = proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"roadmap_milestone.archive","roadmap_milestone_id":%q,"expected_version":1,"changes":{}}`, milestone.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+archive.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, archive.Fingerprint)), nil); r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND status='archived' AND archived_from_status='planned' AND version=2", 1, milestone.ID)

	generation.ID = uuid.NewString()
	generation.Status = "streaming"
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	tool.(*aiWorkspaceTool).generationID = generation.ID
	restore := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"roadmap_milestone.restore","roadmap_milestone_id":%q,"expected_version":2,"changes":{}}`, milestone.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if r := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+restore.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, restore.Fingerprint)), nil); r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND status='planned' AND archived_from_status IS NULL AND version=3", 1, milestone.ID)
}
