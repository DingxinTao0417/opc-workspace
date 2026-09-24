package api

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// Records actual provider JSON bytes, not token estimates. This probe deliberately
// inspects the complete authorized registry independently of any runtime catalog.
func TestAIToolCatalogDefinitionByteBreakdown(t *testing.T) {
	_, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	for _, all := range []bool{false, true} {
		name := "work_actions"
		scopes := []string{"work", "actions"}
		if all {
			name = "all_scopes"
			scopes = aiToolCatalogAllScopes()
		}
		t.Run(name, func(t *testing.T) {
			grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
			if all {
				for range 20 {
					grant.KnowledgeSources = append(grant.KnowledgeSources, aiKnowledgeSourceGrant{uuid.NewString(), 1})
				}
			}
			registry, err := service.aiChatToolRegistry(uuid.NewString(), true, &provider, grant, uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			definitions := registry.Definitions()
			sort.Slice(definitions, func(i, j int) bool { return len(definitions[i].Parameters) > len(definitions[j].Parameters) })
			variants := map[string][]modelclient.ToolDefinition{
				"full":          definitions,
				"model_initial": registry.ModelDefinitions(),
			}
			for _, definition := range definitions {
				openAI, err := json.Marshal(map[string]any{"type": "function", "function": map[string]any{"name": definition.Name, "description": definition.Description, "parameters": definition.Parameters}})
				if err != nil {
					t.Fatal(err)
				}
				anthropic, err := json.Marshal(map[string]any{"name": definition.Name, "description": definition.Description, "input_schema": definition.Parameters})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("tool=%s openai=%d anthropic=%d schema=%d description=%d", definition.Name, len(openAI), len(anthropic), len(definition.Parameters), len(definition.Description))
				if definition.Name != "workspace_propose" {
					variants["without_propose"] = append(variants["without_propose"], definition)
				}
				if definition.Name == "memory_search" || definition.Name == "memory_write" || definition.Name == "memory_propose" || definition.Name == "workspace_guide" {
					variants["memory_and_guide"] = append(variants["memory_and_guide"], definition)
				}
				switch definition.Name {
				case "memory_search", "memory_write", "memory_propose", "workspace_guide", "workspace_search", "workspace_get", "workspace_tasks", "workspace_propose", "workspace_plan":
					variants["task_flow_subset"] = append(variants["task_flow_subset"], definition)
				}
				var schema any
				if err := json.Unmarshal(definition.Parameters, &schema); err != nil {
					t.Fatal(err)
				}
				stripped, err := json.Marshal(omitModelSchemaDescriptions(schema))
				if err != nil {
					t.Fatal(err)
				}
				copy := definition
				copy.Parameters = stripped
				variants["without_schema_descriptions"] = append(variants["without_schema_descriptions"], copy)
				copy.Description = ""
				variants["without_any_descriptions"] = append(variants["without_any_descriptions"], copy)
			}
			for _, protocol := range []modelclient.Protocol{modelclient.ProtocolOpenAIChat, modelclient.ProtocolAnthropicMessages} {
				prompt := modelclient.PromptContext{SystemPrompt: workspaceSystemPrompt(grant), ActionReceipts: maximumStructuredReceiptFixture(t)}
				history := []modelclient.ChatMessage{{Role: "user", Content: "继续"}}
				base, err := modelclient.PromptSize(protocol, "test", history, prompt)
				if err != nil {
					t.Fatal(err)
				}
				for _, variant := range []string{"full", "model_initial", "without_propose", "without_schema_descriptions", "without_any_descriptions", "memory_and_guide", "task_flow_subset"} {
					prompt.Tools = variants[variant]
					size, err := modelclient.PromptSize(protocol, "test", history, prompt)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("protocol=%s variant=%s tools=%d total=%d tool_contribution=%d non_tool=%d", protocol, variant, len(prompt.Tools), size, size-base, base)
				}
			}
		})
	}
}

func TestAITwoDomainGuideNextTurnFitsBound(t *testing.T) {
	_, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: aiToolCatalogAllScopes()}
	for range 20 {
		grant.KnowledgeSources = append(grant.KnowledgeSources, aiKnowledgeSourceGrant{uuid.NewString(), 1})
	}
	registry, err := service.aiChatToolRegistry(uuid.NewString(), true, &provider, grant, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	guide := aiToolCatalogGuide(t, registry)
	topics := guide.guideTopics()
	for _, protocol := range []modelclient.Protocol{modelclient.ProtocolOpenAIChat, modelclient.ProtocolAnthropicMessages} {
		maxSize := 0
		maxPair := ""
		oversized := []string{}
		for i, first := range topics {
			for _, second := range topics[i+1:] {
				args, _ := json.Marshal(map[string]string{"topic": first, "related_topic": second})
				output, err := guide.Execute(context.Background(), args)
				largePair := (first == "tasks_projects" && (second == "outputs" || second == "agents")) ||
					(second == "tasks_projects" && (first == "outputs" || first == "agents"))
				if largePair {
					if err == nil {
						t.Fatalf("large pair %s+%s was not rejected before model presentation", first, second)
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := guide.AcceptResult(context.Background(), output); err != nil {
					t.Fatal(err)
				}
				prompt := modelclient.PromptContext{SystemPrompt: workspaceSystemPrompt(grant), ActionReceipts: maximumStructuredReceiptFixture(t), Tools: registry.ModelDefinitions()}
				history := []modelclient.ChatMessage{
					{Role: "user", Content: "继续"},
					{Role: "assistant", ToolCalls: []modelclient.ToolCall{{ID: "guide-1", Name: "workspace_guide", Arguments: args}}},
					{Role: "tool", Content: "workspace_guide: " + output, ToolCallID: "guide-1", ToolName: "workspace_guide"},
				}
				size, err := modelclient.PromptSize(protocol, "test", history, prompt)
				if err != nil {
					t.Fatal(err)
				}
				if size > maxSize {
					maxSize, maxPair = size, first+"+"+second
				}
				if size > modelclient.MaxPromptBytes {
					oversized = append(oversized, first+"+"+second)
				}
			}
		}
		t.Logf("protocol=%s max_two_domain_prompt=%d pair=%s oversized=%v", protocol, maxSize, maxPair, oversized)
		if len(oversized) > 0 {
			t.Fatalf("supported two-domain catalog exceeds the prompt cap: %v", oversized)
		}
	}
}

func TestAIHeavyDomainPairGuideUsesActualGrantBudget(t *testing.T) {
	_, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	for _, test := range []struct {
		name   string
		scopes []string
		accept bool
	}{
		{"basic", []string{"work", "outputs", "actions", "agent_execution"}, true},
		{"with_files", []string{"work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files", "output_files"}, true},
		{"with_browser", []string{"work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files", "output_files", "workspace_ui", "workspace_browser"}, false},
		{"all_scopes", aiToolCatalogAllScopes(), false},
	} {
		grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: test.scopes}
		if test.name == "all_scopes" {
			for range 20 {
				grant.KnowledgeSources = append(grant.KnowledgeSources, aiKnowledgeSourceGrant{uuid.NewString(), 1})
			}
		}
		registry, err := service.aiChatToolRegistry(uuid.NewString(), true, &provider, grant, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		guide := aiToolCatalogGuide(t, registry)
		for _, pair := range [][2]string{{"tasks_projects", "outputs"}, {"tasks_projects", "agents"}} {
			arguments, _ := json.Marshal(map[string]string{"topic": pair[0], "related_topic": pair[1]})
			before := registry.ModelDefinitions()
			output, err := guide.Execute(context.Background(), arguments)
			if !test.accept {
				if err == nil {
					t.Fatalf("%s %s+%s should reject the oversized catalog", test.name, pair[0], pair[1])
				}
				if !reflect.DeepEqual(registry.ModelDefinitions(), before) {
					t.Fatal("rejected guide changed model tool visibility")
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s %s+%s rejected: %v", test.name, pair[0], pair[1], err)
			}
			if !reflect.DeepEqual(registry.ModelDefinitions(), before) {
				t.Fatal("unaccepted guide changed model tool visibility")
			}
			if err := guide.AcceptResult(context.Background(), output); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"workspace_tasks", "workspace_outputs", "workspace_propose"} {
				if _, present := registry.Get(name); !present {
					t.Fatalf("accepted guide lost registered %s", name)
				}
				found := false
				for _, definition := range registry.ModelDefinitions() {
					found = found || definition.Name == name
				}
				if !found {
					t.Fatalf("accepted guide omitted model-visible %s", name)
				}
			}
			if pair[1] == "agents" {
				found := false
				for _, definition := range registry.ModelDefinitions() {
					found = found || definition.Name == "workspace_agent_execution"
				}
				if !found {
					t.Fatal("agent guide omitted execution eligibility tool")
				}
			}
			for _, protocol := range []modelclient.Protocol{modelclient.ProtocolOpenAIChat, modelclient.ProtocolAnthropicMessages} {
				prompt := modelclient.PromptContext{SystemPrompt: workspaceSystemPrompt(grant), ActionReceipts: maximumStructuredReceiptFixture(t), Tools: registry.ModelDefinitions()}
				history := []modelclient.ChatMessage{{Role: "user", Content: "继续"}, {Role: "assistant", ToolCalls: []modelclient.ToolCall{{ID: "guide-1", Name: "workspace_guide", Arguments: arguments}}}, {Role: "tool", Content: "workspace_guide: " + output, ToolCallID: "guide-1", ToolName: "workspace_guide"}}
				size, err := modelclient.PromptSize(protocol, "test", history, prompt)
				if err != nil {
					t.Fatal(err)
				}
				if size > modelclient.MaxPromptBytes {
					t.Fatalf("accepted %s %s+%s exceeds %s prompt cap: %d", test.name, pair[0], pair[1], protocol, size)
				}
			}
		}
	}
}
