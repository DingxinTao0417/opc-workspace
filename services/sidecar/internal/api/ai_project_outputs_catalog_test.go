package api

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestAIProjectOutputsCatalogRequiresBothReadScopes(t *testing.T) {
	fixture := newAIToolCatalogFixture(t)
	for _, scopes := range [][]string{{"work"}, {"work", "actions"}, {"work", "outputs"}, {"work", "outputs", "actions"}} {
		t.Run(strings.Join(scopes, "+"), func(t *testing.T) {
			registry := fixture.registry(t, scopes...)
			tool, found := registry.Get("workspace_project_outputs")
			want := slices.Contains(scopes, "work") && slices.Contains(scopes, "outputs")
			if found != want {
				t.Fatalf("project outputs registered=%v want=%v", found, want)
			}
			if !want {
				return
			}
			if slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
				t.Fatal("project outputs should be discovered through the domain guide")
			}
			var schema struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema); err != nil {
				t.Fatal(err)
			}
			assertPropertySet(t, tool.Name(), schema.Properties, map[string]bool{
				"project_id": true, "artifact_id": true, "followup_status": true,
				"include_deleted": true, "limit": true, "offset": true,
			})
			if !slices.Equal(schema.Required, []string{"project_id"}) {
				t.Fatalf("required=%v", schema.Required)
			}
			if aiProgressToolName(tool.Name()) != tool.Name() {
				t.Fatal("project output query is hidden from live progress")
			}
			guide := aiToolCatalogGuide(t, registry)
			for _, topic := range []string{"outputs", "tasks_projects", "inbox"} {
				output := aiToolCatalogExecuteGuide(t, guide, topic)
				if !strings.Contains(output, "workspace_project_outputs") || !strings.Contains(output, "project_version") {
					t.Fatalf("%s lacks project output discovery and version boundary: %s", topic, output)
				}
				if err := guide.AcceptResult(context.Background(), output); err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
					t.Fatalf("%s did not expose project output query", topic)
				}
			}
		})
	}
}
