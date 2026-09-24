package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiProjectPageForTest struct {
	Items []aiProjectListItem `json:"items"`
	Total int64               `json:"total_items"`
	More  bool                `json:"has_more"`
	Next  *int                `json:"next_offset"`
	AsOf  string              `json:"as_of"`
}

func TestAIWorkspaceProjectsMatchesNativeFiltersAndProgressWithoutDisclosure(t *testing.T) {
	router, store, service, proposal, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id = ?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	registered, ok := registry.Get("workspace_projects")
	if !ok {
		t.Fatal("project discovery must be available for work without actions")
	}
	if _, err := registered.Execute(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	denied := *registered.(*aiWorkspaceTool)
	denied.policy = harness.NewCapabilities("clients")
	if _, err := denied.Execute(context.Background(), []byte(`{}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("runtime scope recheck=%v", err)
	}
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_projects"
	tool.policy = harness.NewCapabilities("work")
	stamp := "2026-09-18T12:00:00Z"
	secret := "PRIVATE-CLIENT-NOTE"
	client := models.Client{ID: uuid.NewString(), Name: "PRIVATE-CLIENT", Notes: &secret, Status: "active", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	amount := int64(987654321)
	projects := []models.Project{
		{ID: uuid.NewString(), Name: "Alpha", Description: "PRIVATE-DESCRIPTION needle", ClientID: &client.ID, AmountMinor: &amount, Status: "in_progress", Version: 1, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: uuid.NewString(), Name: "Beta", Status: "planning", Version: 1, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: uuid.NewString(), Name: "Archived", Status: "archived", Version: 1, CreatedAt: stamp, UpdatedAt: stamp},
	}
	for _, project := range projects {
		if err := store.DB.Create(&project).Error; err != nil {
			t.Fatal(err)
		}
	}
	completed := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"done","project_id":%q}`, projects[0].ID))
	createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"open","project_id":%q}`, projects[0].ID))
	if err := store.DB.Model(&completed).Updates(map[string]any{"status": "done", "completed_at": stamp, "actual_minutes": 30}).Error; err != nil {
		t.Fatal(err)
	}
	read := func(args string) (aiProjectPageForTest, string) {
		t.Helper()
		encoded, err := tool.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		var page aiProjectPageForTest
		if err := json.Unmarshal([]byte(encoded), &page); err != nil {
			t.Fatal(err)
		}
		return page, encoded
	}
	page, encoded := read(`{"filters":{"sort":"name"},"limit":1}`)
	if page.Total != 2 || !page.More || page.Next == nil || *page.Next != 1 || page.Items[0].ID != projects[0].ID || page.AsOf == "" {
		t.Fatalf("page=%s", encoded)
	}
	if page.Items[0].TaskSummary.Total != 2 || page.Items[0].TaskSummary.Completed != 1 || page.Items[0].TaskSummary.ProgressPercent != 50 || page.Items[0].TaskSummary.ActualMinutes != 30 {
		t.Fatalf("derived progress=%+v", page.Items[0].TaskSummary)
	}
	for _, marker := range []string{"PRIVATE-DESCRIPTION", "PRIVATE-CLIENT", "PRIVATE-CLIENT-NOTE", "987654321", "client_id", "amount_minor", "invoice_count"} {
		if strings.Contains(encoded, marker) {
			t.Fatalf("project result disclosed %q: %s", marker, encoded)
		}
	}
	second, _ := read(`{"filters":{"sort":"name"},"limit":1,"offset":1}`)
	if len(second.Items) != 1 || second.Items[0].ID != projects[1].ID {
		t.Fatalf("second page=%+v", second)
	}
	search, _ := read(`{"filters":{"q":"needle"}}`)
	if search.Total != 1 || search.Items[0].ID != projects[0].ID {
		t.Fatalf("description search=%+v", search)
	}
	archived, _ := read(`{"filters":{"status":"archived"}}`)
	if archived.Total != 1 || archived.Items[0].ID != projects[2].ID {
		t.Fatalf("archived=%+v", archived)
	}
	native := performRequest(router, http.MethodGet, "/api/v1/projects?sort=name&page_size=20", nil, nil)
	if native.Code != http.StatusOK {
		t.Fatal(native.Body.String())
	}
	var nativePage struct {
		Data []projectResponse `json:"data"`
		Meta pageMeta          `json:"meta"`
	}
	if err := json.Unmarshal(native.Body.Bytes(), &nativePage); err != nil {
		t.Fatal(err)
	}
	all, _ := read(`{"filters":{"sort":"name"}}`)
	gotIDs := []string{}
	for i, item := range all.Items {
		gotIDs = append(gotIDs, item.ID)
		if !reflect.DeepEqual(item.TaskSummary, nativePage.Data[i].TaskSummary) {
			t.Fatalf("progress differs native=%+v ai=%+v", nativePage.Data[i].TaskSummary, item.TaskSummary)
		}
	}
	wantIDs := []string{}
	for _, item := range nativePage.Data {
		wantIDs = append(wantIDs, item.ID)
	}
	if all.Total != nativePage.Meta.Total || !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("native IDs=%v total=%d, AI IDs=%v total=%d", wantIDs, nativePage.Meta.Total, gotIDs, all.Total)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"filters":{"client_id":%q}}`, client.ID))); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("work-only client filter=%v", err)
	}
	tool.policy = harness.NewCapabilities("work", "clients")
	filtered, _ := read(fmt.Sprintf(`{"filters":{"client_id":%q}}`, client.ID))
	if filtered.Total != 1 || filtered.Items[0].ID != projects[0].ID {
		t.Fatalf("client filter=%+v", filtered)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps", 0)
}

func TestAIWorkspaceProjectsRejectsAmbiguousAndUnsafeQueries(t *testing.T) {
	_, _, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_projects"
	tool.policy = harness.NewCapabilities("work", "clients")
	for _, args := range []string{
		`{"filters":{"status":""}}`, `{"filters":{"sort":"amount_minor"}}`,
		`{"filters":{"q":"x","q":"y"}}`, `{"filters":{"client_id":"bad"}}`,
		`{"filters":null}`, `{"filters":{"status":null}}`, `{"extra":1}`,
		`{"limit":0}`, `{"offset":1001}`, `{"filters":{"sort":"name,-amount_minor"}}`,
		`{"filters":{"include_archived":1}}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted unsafe query %s", args)
		}
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"filters":{"client_id":"`+strings.ToUpper(uuid.NewString())+`"}}`)); err == nil {
		t.Fatal("accepted noncanonical client ID")
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"filters":{"q":"%"}}`)); err != nil {
		t.Fatalf("literal wildcard should be valid: %v", err)
	}
}

func TestAIWorkspaceProjectsHarnessGuideAndReadOnlyResult(t *testing.T) {
	router, store, _, _, _ := aiActionTestFixture(t)
	stamp := "2026-09-18T12:00:00Z"
	project := models.Project{ID: uuid.NewString(), Name: "Portfolio target", Description: "PRIVATE-BODY", Status: "planning", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		foundProjectTool := false
		for _, definition := range payload.Tools {
			function, _ := definition["function"].(map[string]any)
			if function["name"] == "workspace_projects" {
				foundProjectTool = true
			}
		}
		if foundProjectTool != (call == 2 || call == 3) {
			t.Errorf("project tool projection at request %d = %v", call, foundProjectTool)
		}
		if call == 1 || call == 2 {
			name, id, arguments := "workspace_guide", "guide-projects", `{"topic":"tasks_projects"}`
			if call == 2 {
				name, id, arguments = "workspace_projects", "read-projects", `{"filters":{"q":"Portfolio","sort":"name"}}`
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		found := false
		for _, message := range payload.Messages {
			content, _ := message["content"].(string)
			if !strings.HasPrefix(content, "workspace_projects: ") {
				continue
			}
			var page aiProjectPageForTest
			if err := json.Unmarshal([]byte(strings.TrimPrefix(content, "workspace_projects: ")), &page); err != nil {
				t.Error(err)
				continue
			}
			found = page.Total == 1 && len(page.Items) == 1 && page.Items[0].ID == project.ID
			if strings.Contains(content, "PRIVATE-BODY") {
				t.Error("project body disclosed to model")
			}
		}
		if !found {
			t.Error("model did not receive filtered project result")
		}
		streamMockAIDelta(w, `找到 1 个项目；尚未修改。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "structured-project-query", upstream.URL+"/v1", "gpt-test")
	var current models.AIProvider
	if err := store.DB.First(&current, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查找 Portfolio 项目", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") || !strings.Contains(response.Body.String(), `"tool_name":"workspace_projects"`) || calls.Load() != 3 {
		t.Fatalf("project chat=%d calls=%d %s", response.Code, calls.Load(), response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
