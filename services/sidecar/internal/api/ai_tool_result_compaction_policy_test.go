package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAIWorkspaceResultCompactionPolicyIsExplicitAndReadOnly(t *testing.T) {
	for _, name := range []string{
		"workspace_search", "workspace_get", "workspace_tasks", "workspace_projects", "workspace_today",
		"workspace_roadmap_milestones", "workspace_content_items", "workspace_client_records",
		"workspace_automations", "workspace_finance", "workspace_focus", "workspace_focus_report",
		"workspace_project_notes", "workspace_project_outputs", "workspace_agent_runs",
		"workspace_outputs", "workspace_task_submissions", "workspace_task_assignments",
		"workspace_task_options", "workspace_task_views", "workspace_inbox_tasks", "workspace_inbox_source",
	} {
		if !(&aiWorkspaceTool{name: name}).RequeryableResult() {
			t.Fatalf("read-only %s was not opt-in", name)
		}
	}
	for _, name := range []string{
		"workspace_propose", "workspace_plan", "workspace_request_access", "workspace_guide",
		"workspace_open_panel", "workspace_request_browser_navigation", "workspace_request_browser_action", "workspace_request_record_navigation",
		"workspace_agent_execution", "workspace_agent_project_files", "workspace_read_artifact_file",
		"knowledge_search", "knowledge_read", "knowledge_library", "unknown_future_tool",
	} {
		if (&aiWorkspaceTool{name: name}).RequeryableResult() {
			t.Fatalf("protected or unknown %s was marked requeryable", name)
		}
	}
}

func TestAIWorkspaceResultCompactionCreatesBoundedIdentityCapsule(t *testing.T) {
	items := make([]map[string]any, 0, 12)
	for index := 0; index < 12; index++ {
		items = append(items, map[string]any{
			"id":          fmt.Sprintf("018f0000-0000-7000-8000-%012d", index),
			"task_id":     "018f0000-0000-7000-8000-000000000999",
			"version":     index + 1,
			"status":      "active",
			"title":       strings.Repeat("标题", 120),
			"description": strings.Repeat("PRIVATE-BODY-INSTRUCTION ", 200),
		})
	}
	raw, err := json.Marshal(map[string]any{
		"items": items, "total": 12, "offset": 0, "next_offset": 12, "has_more": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"filters":{"status":"active"},"limit":20}`)
	capsule, ok := (&aiWorkspaceTool{name: "workspace_tasks"}).CompactResult(arguments, string(raw))
	if !ok || len(capsule) > 4<<10 || !json.Valid([]byte(capsule)) {
		t.Fatalf("capsule accepted=%v bytes=%d body=%q", ok, len(capsule), capsule)
	}
	if strings.Contains(capsule, "PRIVATE-BODY-INSTRUCTION") || strings.Contains(capsule, "description") {
		t.Fatal("capsule retained free-form body evidence")
	}
	var decoded struct {
		Version         int            `json:"version"`
		Kind            string         `json:"kind"`
		Tool            string         `json:"tool"`
		Complete        bool           `json:"complete"`
		Stale           bool           `json:"stale"`
		OriginalBytes   int            `json:"original_bytes"`
		ArgumentsSHA256 string         `json:"arguments_sha256"`
		ResultSHA256    string         `json:"result_sha256"`
		Evidence        map[string]any `json:"evidence"`
	}
	if json.Unmarshal([]byte(capsule), &decoded) != nil || decoded.Version != 1 ||
		decoded.Kind != "compacted_read_evidence" || decoded.Tool != "workspace_tasks" ||
		decoded.Complete || !decoded.Stale || decoded.OriginalBytes != len(raw) ||
		len(decoded.ArgumentsSHA256) != 64 || len(decoded.ResultSHA256) != 64 {
		t.Fatalf("invalid capsule envelope: %+v", decoded)
	}
	facts, ok := decoded.Evidence["facts"].([]any)
	if !ok || len(facts) == 0 || len(facts) > 8 || decoded.Evidence["facts_omitted"].(float64) < 1 {
		t.Fatalf("invalid bounded facts: %#v", decoded.Evidence)
	}
	encodedFacts, _ := json.Marshal(facts)
	if !strings.Contains(string(encodedFacts), `"task_id"`) || !strings.Contains(string(encodedFacts), `"version"`) || !strings.Contains(string(encodedFacts), `"status"`) {
		t.Fatalf("identity/version/status evidence missing: %s", encodedFacts)
	}

	for _, test := range []struct {
		name   string
		result string
	}{
		{name: "workspace_propose", result: string(raw)},
		{name: "workspace_tasks", result: `{"description":"body only"}`},
		{name: "workspace_tasks", result: `{not-json}`},
	} {
		if compacted, accepted := (&aiWorkspaceTool{name: test.name}).CompactResult(arguments, test.result); accepted || compacted != "" {
			t.Fatalf("unsafe capsule accepted for %s: %q", test.name, compacted)
		}
	}
}
