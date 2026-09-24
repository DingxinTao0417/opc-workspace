package api

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestAIInboxSourceCatalogAndGuideBoundary(t *testing.T) {
	fixture := newAIToolCatalogFixture(t)
	for _, scopes := range [][]string{{"clients"}, {"finance"}, {"work"}, {"work", "actions"}, {"work", "outputs", "clients", "finance"}} {
		t.Run(strings.Join(scopes, "+"), func(t *testing.T) {
			registry := fixture.registry(t, scopes...)
			tool, found := registry.Get("workspace_inbox_source")
			if found != slices.Contains(scopes, "work") {
				t.Fatal("source discovery must require work independently of protected source scopes")
			}
			if !found {
				return
			}
			if slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
				t.Fatal("source tool must be deferred to the inbox guide")
			}
			var schema struct {
				Required             []string       `json:"required"`
				Properties           map[string]any `json:"properties"`
				AdditionalProperties bool           `json:"additionalProperties"`
			}
			if err := json.Unmarshal(tool.(*aiWorkspaceTool).InputSchema(), &schema); err != nil {
				t.Fatal(err)
			}
			assertPropertySet(t, tool.Name(), schema.Properties, map[string]bool{"inbox_item_id": true})
			if schema.AdditionalProperties || !slices.Equal(schema.Required, []string{"inbox_item_id"}) || aiProgressToolName(tool.Name()) != tool.Name() {
				t.Fatal("source input/progress contract changed")
			}
			guide := aiToolCatalogGuide(t, registry)
			encoded := aiToolCatalogExecuteGuide(t, guide, "inbox")
			var output aiWorkspaceGuideResult
			if err := json.Unmarshal([]byte(encoded), &output); err != nil {
				t.Fatal(err)
			}
			for _, boundary := range []string{"workspace_inbox_source", "permission_required", "outputs", "clients", "finance", "新消息重新授权", "snapshot_matches_current=false", "null", "人工确认", "自动化Run不是AgentRun", "不自动打开route", "agent_run_failed", "automation.retry只重试创建通知", "agent_execution", "pending只恢复登记"} {
				if !strings.Contains(output.Instructions, boundary) {
					t.Fatalf("inbox source guide lacks %s", boundary)
				}
			}
			if !slices.Contains(output.AvailableTools, tool.Name()) {
				t.Fatal("inbox guide did not discover source tool")
			}
			if err := guide.AcceptResult(context.Background(), encoded); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(aiToolCatalogNames(registry.ModelDefinitions()), tool.Name()) {
				t.Fatal("accepted guide did not expose source tool")
			}
		})
	}
}
