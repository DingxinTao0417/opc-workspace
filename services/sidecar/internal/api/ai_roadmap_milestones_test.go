package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIRoadmapMilestonesMatchesNativeFiltersAndSort(t *testing.T) {
	router, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	tool := &aiWorkspaceTool{api: service, name: "workspace_roadmap_milestones", policy: harness.NewCapabilities("work"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	project := createProjectForTest(t, router, `{"name":"Roadmap project"}`, nil)
	createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Linked Task","project_id":%q,"description":"PRIVATE TASK DESCRIPTION"}`, project.ID))
	create := func(title, date, status, description string, linked bool) roadmapMilestoneResponse {
		t.Helper()
		projects := `[]`
		if linked {
			projects = fmt.Sprintf(`[%q]`, project.ID)
		}
		month, err := strconv.Atoi(date[5:7])
		if err != nil {
			t.Fatal(err)
		}
		request := fmt.Sprintf(`{"title":%q,"description":%q,"year":2026,"quarter":%d,"target_date":%q,"status":%q,"project_ids":%s}`,
			title, description, 1+(month-1)/3, date, status, projects)
		response := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(request), nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("create milestone = %d %s", response.Code, response.Body.String())
		}
		return decodeRoadmapMilestoneResponse(t, response.Body.Bytes())
	}
	first := create("Q3 first", "2026-09-12", "planned", "PRIVATE ROADMAP DESCRIPTION", true)
	second := create("Q3 second", "2026-09-28", "active", "PRIVATE ROADMAP DESCRIPTION", false)
	_ = create("Q4 goal", "2026-12-10", "planned", "PRIVATE ROADMAP DESCRIPTION", true)
	archived := create("Archived Q3", "2026-09-20", "planned", "PRIVATE ROADMAP DESCRIPTION", true)
	archive := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+archived.ID+"/archive", nil, map[string]string{"If-Match": `"1"`})
	if archive.Code != http.StatusOK {
		t.Fatal(archive.Body.String())
	}
	for _, tc := range []struct {
		args, native string
	}{
		{`{"filters":{"year":2026,"quarter":3}}`, "year=2026&quarter=3"},
		{fmt.Sprintf(`{"filters":{"year":2026,"project_id":%q}}`, project.ID), "year=2026&project_id=" + project.ID},
		{`{"filters":{"status":"archived"}}`, "status=archived"},
		{`{"filters":{"year":2026,"include_archived":true},"sort":"target_date","limit":2,"offset":1}`, "year=2026&include_archived=true&sort=target_date&page_size=2&page=1"},
	} {
		encoded, err := tool.Execute(context.Background(), []byte(tc.args))
		if err != nil {
			t.Fatalf("roadmap %s: %v", tc.args, err)
		}
		var page struct {
			Items []struct {
				ID           string             `json:"id"`
				Version      int64              `json:"version"`
				ProjectCount int                `json:"project_count"`
				Route        string             `json:"route"`
				TaskSummary  roadmapTaskSummary `json:"task_summary"`
			} `json:"items"`
			Total         int64 `json:"total_items"`
			HasMore       bool  `json:"has_more"`
			WindowLimited bool  `json:"window_limited"`
			Next          *int  `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(encoded), &page); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(encoded, "PRIVATE ROADMAP DESCRIPTION") || strings.Contains(encoded, "PRIVATE TASK DESCRIPTION") || strings.Contains(encoded, `"description"`) || strings.Contains(encoded, "Roadmap project") {
			t.Fatalf("roadmap list leaked description: %s", encoded)
		}
		native := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?"+tc.native, nil, nil)
		if native.Code != http.StatusOK {
			t.Fatal(native.Body.String())
		}
		items, meta := decodeRoadmapMilestoneList(t, native.Body.Bytes())
		if strings.Contains(tc.args, `"offset":1`) {
			// Native page-number pagination cannot express offset=1 at size=2.
			all := performRequest(router, http.MethodGet, "/api/v1/roadmap/milestones?year=2026&include_archived=true&sort=target_date&page_size=100", nil, nil)
			items, meta = decodeRoadmapMilestoneList(t, all.Body.Bytes())
			items = items[1:3]
		}
		if page.Total != meta.Total || len(page.Items) != len(items) {
			t.Fatalf("native/AI count mismatch args=%s ai=%s native=%+v", tc.args, encoded, meta)
		}
		for index, item := range page.Items {
			if item.ID != items[index].ID || item.Version != items[index].Version || item.ProjectCount != len(items[index].Projects) || item.TaskSummary != items[index].TaskSummary || item.Route != searchRoute("roadmap_milestone", item.ID) {
				t.Fatalf("native/AI item mismatch args=%s ai=%+v native=%+v", tc.args, item, items[index])
			}
		}
		if strings.Contains(tc.args, `"offset":1`) && (!page.HasMore || page.Next == nil || *page.Next != 3 || page.WindowLimited) {
			t.Fatalf("offset pagination did not expose next page: %s", encoded)
		}
	}
	if first.ID == second.ID {
		t.Fatal("fixture identities collided")
	}
}

func TestAIRoadmapMilestonesRejectsInvalidScopeAndArguments(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	tool := &aiWorkspaceTool{api: service, name: "workspace_roadmap_milestones", policy: harness.NewCapabilities("work"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	for _, args := range []string{
		`{"filters":{"year":2026,"year":2027}}`, `{"filters":null}`, `{"filters":{"year":1999}}`, `{"filters":{"year":0}}`, `{"filters":{"quarter":5}}`, `{"filters":{"quarter":0}}`,
		`{"filters":{"status":"unknown"}}`, `{"filters":{"status":""}}`, `{"filters":{"project_id":"wrong"}}`, `{"filters":{"project_id":""}}`, `{"filters":{"year":"2026"}}`, `{"filters":{"include_archived":"true"}}`, `{"sort":"title"}`, `{"sort":""}`, `{"limit":21}`,
		`{"offset":1001}`, `{"unknown":1}`, `[]`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted invalid roadmap query %s", args)
		}
	}
	tool.policy = harness.NewCapabilities()
	if _, err := tool.Execute(context.Background(), []byte(`{}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("ungranted roadmap read = %v", err)
	}
}
