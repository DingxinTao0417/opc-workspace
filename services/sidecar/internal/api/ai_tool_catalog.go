package api

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// This code-owned projection changes model presentation, never the registered
// allowlist. Only known domain helpers are deferred. Memory, proposals, plans,
// search/get, citation reads, UI tools and any future unclassified tools remain
// available without discovery. No capability is registered by this catalog.
var aiWorkspaceTopicTools = map[string][]string{
	"planning":        {"workspace_today", "workspace_tasks", "workspace_task_options"},
	"tasks_projects":  {"workspace_today", "workspace_tasks", "workspace_projects", "workspace_task_views", "workspace_task_options", "workspace_task_assignments", "workspace_project_outputs"},
	"inbox":           {"workspace_inbox_tasks", "workspace_inbox_source", "workspace_task_options", "workspace_project_outputs"},
	"people_clients":  {"workspace_client_records", "workspace_task_options", "workspace_task_assignments"},
	"outputs":         {"workspace_outputs", "workspace_task_submissions", "workspace_agent_runs", "workspace_project_outputs", "workspace_read_artifact_file"},
	"agents":          {"workspace_agent_execution", "workspace_delegate_agent", "workspace_agent_followup", "workspace_agent_children", "workspace_agent_project_files", "workspace_agent_runs", "workspace_outputs", "workspace_task_submissions", "workspace_task_assignments", "workspace_task_options", "workspace_read_artifact_file"},
	"focus":           {"workspace_focus", "workspace_focus_report"},
	"finance":         {"workspace_finance"},
	"automation":      {"workspace_automations"},
	"project_notes":   {"workspace_project_notes"},
	"roadmap_content": {"workspace_roadmap_milestones", "workspace_content_items"},
	"knowledge":       {"knowledge_library"},
}

type aiWorkspaceGuideResult struct {
	Topic          string   `json:"topic"`
	RelatedTopic   string   `json:"related_topic,omitempty"`
	Instructions   string   `json:"instructions"`
	AvailableTools []string `json:"available_tools,omitempty"`
}

type aiWorkspaceToolCatalog struct {
	registry     *harness.Registry
	core         []string
	protocol     modelclient.Protocol
	model        string
	systemPrompt string
}

// The guide result itself and the newly projected definitions must fit before
// acceptance. This is a conservative fixed-envelope check, not a promise that
// arbitrary user text or business context will fit the runtime prompt window.
// Keep 4 KiB beyond the maximum 8 KiB action-receipt envelope for the current
// user turn and other context; the runtime prompt guard remains authoritative.
func (c *aiWorkspaceToolCatalog) fitsHeavyGuide(result aiWorkspaceGuideResult) bool {
	if c == nil || c.systemPrompt == "" || c.protocol == "" {
		return false
	}
	selected := make(map[string]bool, len(result.AvailableTools))
	for _, name := range result.AvailableTools {
		selected[name] = true
	}
	definitions := []modelclient.ToolDefinition{}
	for _, definition := range c.registry.Definitions() {
		if selected[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	arguments, err := json.Marshal(map[string]string{"topic": result.Topic, "related_topic": result.RelatedTopic})
	if err != nil {
		return false
	}
	output, err := json.Marshal(result)
	if err != nil {
		return false
	}
	history := []modelclient.ChatMessage{
		{Role: "user", Content: "继续"},
		{Role: "assistant", ToolCalls: []modelclient.ToolCall{{ID: "guide-1", Name: "workspace_guide", Arguments: arguments}}},
		{Role: "tool", Content: "workspace_guide: " + string(output), ToolCallID: "guide-1", ToolName: "workspace_guide"},
	}
	size, err := modelclient.PromptSize(c.protocol, c.model, history, modelclient.PromptContext{
		SystemPrompt:   c.systemPrompt,
		ActionReceipts: strings.Repeat("x", 8<<10),
		Tools:          definitions,
	})
	return err == nil && size <= modelclient.MaxPromptBytes-(4<<10)
}

func configureAIWorkspaceToolCatalog(registry *harness.Registry) error {
	tool, ok := registry.Get("workspace_guide")
	if !ok {
		return nil // Small knowledge-only or UI/browser-only scopes stay complete.
	}
	guide, ok := tool.(*aiWorkspaceTool)
	if !ok {
		return errors.New("invalid workspace guide registration")
	}
	deferred := map[string]bool{}
	for _, topic := range guide.guideTopics() {
		for _, name := range aiWorkspaceTopicTools[topic] {
			deferred[name] = true
		}
	}
	// The full proposal schema is the largest model-facing definition. Write
	// rules already require a domain guide, so keep its execution authority in
	// the registry but advertise it only after that guide is accepted.
	deferred["workspace_propose"] = true
	catalog := &aiWorkspaceToolCatalog{registry: registry, core: []string{}}
	for _, name := range registry.Names() {
		if !deferred[name] {
			catalog.core = append(catalog.core, name)
		}
	}
	if err := registry.SelectModelTools(catalog.core); err != nil {
		return err
	}
	guide.guideCatalog = catalog // Newly constructed, owned by this one generation.
	return nil
}

func (c *aiWorkspaceToolCatalog) namesForTopics(topics ...string) []string {
	selected := make(map[string]bool, len(c.core))
	for _, name := range c.core {
		selected[name] = true
	}
	canPropose := false
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		for _, name := range aiWorkspaceTopicTools[topic] {
			selected[name] = true
		}
		canPropose = canPropose || topic != "workspace_ui"
	}
	if canPropose {
		selected["workspace_propose"] = true
	}
	names := []string{}
	for _, name := range c.registry.Names() {
		if selected[name] {
			names = append(names, name)
		}
	}
	return names
}

// Execute is asynchronous and may outlive its timeout; it must never select a
// catalog. Only Harness acceptance of its bounded successful result can do so.
func (t *aiWorkspaceTool) acceptGuideResult(ctx context.Context, output string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var result aiWorkspaceGuideResult
	if err := decodeStrictToolArguments(json.RawMessage(output), &result); err != nil {
		return err
	}
	// Recompute from code-owned rules and this generation's frozen allowlist;
	// never accept tool names or instructions supplied by a model or data record.
	input := map[string]string{"topic": result.Topic}
	if result.RelatedTopic != "" {
		input["related_topic"] = result.RelatedTopic
	}
	arguments, _ := json.Marshal(input)
	expectedValue, err := t.guide(arguments)
	if err != nil {
		return err
	}
	expected := expectedValue.(aiWorkspaceGuideResult)
	if result.Topic != expected.Topic || result.RelatedTopic != expected.RelatedTopic || result.Instructions != expected.Instructions || !slices.Equal(result.AvailableTools, expected.AvailableTools) {
		return errors.New("invalid workspace guide result")
	}
	return t.guideCatalog.registry.SelectModelTools(expected.AvailableTools)
}
