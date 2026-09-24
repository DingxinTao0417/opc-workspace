package api

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestAIContentItemsCatalogAndGuideBoundary(t *testing.T) {
	fixture := newAIToolCatalogFixture(t)
	for _, scopes := range [][]string{{"clients"}, {"finance"}, {"work"}, {"work", "actions"}} {
		t.Run(strings.Join(scopes, "+"), func(t *testing.T) {
			registry := fixture.registry(t, scopes...)
			tool, found := registry.Get("workspace_content_items")
			if found != slices.Contains(scopes, "work") {
				t.Fatal("content discovery was not isolated to work")
			}
			if !found {
				return
			}
			if slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
				t.Fatal("content tool must remain deferred")
			}
			var schema struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema); err != nil {
				t.Fatal(err)
			}
			assertPropertySet(t, tool.Name(), schema.Properties, map[string]bool{
				"view": true, "filters": true, "content_item_id": true, "task_state": true, "required_only": true, "limit": true, "offset": true,
			})
			if !slices.Equal(schema.Required, []string{"view"}) || aiProgressToolName(tool.Name()) != tool.Name() {
				t.Fatal("required view/progress contract missing")
			}
			guide := aiToolCatalogGuide(t, registry)
			encoded := aiToolCatalogExecuteGuide(t, guide, "roadmap_content")
			var output aiWorkspaceGuideResult
			if err := json.Unmarshal([]byte(encoded), &output); err != nil {
				t.Fatal(err)
			}
			for _, boundary := range []string{"workspace_content_items", "required_incomplete", "window_limited", "cancelled/waiting_review", "published", "人工确认", "IANA"} {
				if !strings.Contains(output.Instructions, boundary) {
					t.Fatalf("guide lacks %s", boundary)
				}
			}
			if !slices.Contains(output.AvailableTools, tool.Name()) {
				t.Fatal("guide does not discover content tool")
			}
			if err := guide.AcceptResult(context.Background(), encoded); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
				t.Fatal("accepted guide did not expose content query")
			}
		})
	}
}
