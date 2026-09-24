package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIProjectFileProposalValidation(t *testing.T) {
	now := time.Now()
	input := projectFileInput(models.AIProvider{ID: uuid.NewString(), Version: 1}, uuid.NewString(), now)
	file := input.ProjectFiles.Files[0]
	valid := map[string]any{"path": file.Path, "base_sha256": file.SHA256, "content": "\ufeffreplacement\r\n"}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"other-file", func(v map[string]any) { v["path"] = "other.txt" }},
		{"traversal", func(v map[string]any) { v["path"] = "../outside.txt" }},
		{"wrong-case", func(v map[string]any) { v["path"] = strings.ToUpper(file.Path) }},
		{"wrong-base", func(v map[string]any) { v["base_sha256"] = strings.Repeat("0", 64) }},
		{"no-change", func(v map[string]any) { v["content"] = file.Content }},
		{"missing-content", func(v map[string]any) { delete(v, "content") }},
		{"null-content", func(v map[string]any) { v["content"] = nil }},
		{"unknown-field", func(v map[string]any) { v["write"] = true }},
		{"too-large", func(v map[string]any) { v["content"] = strings.Repeat("汉", aiProjectFileBytes/3+1) }},
		{"nul", func(v map[string]any) { v["content"] = "\x00" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := newAIProjectFileProposalTool(input.ProjectFiles, func() time.Time { return now })
			args := map[string]any{}
			for key, value := range valid {
				args[key] = value
			}
			test.change(args)
			raw, _ := json.Marshal(args)
			if _, err := tool.Execute(context.Background(), raw); err == nil || tool.pending != nil {
				t.Fatal("invalid proposal accepted")
			}
		})
	}
	tool := newAIProjectFileProposalTool(input.ProjectFiles, func() time.Time { return now })
	raw, _ := json.Marshal(valid)
	out, err := tool.Execute(context.Background(), raw)
	if err != nil || strings.Contains(out, "replacement") {
		t.Fatalf("execute: %s %v", out, err)
	}
	var emitted []aiProjectFileProposal
	ctx := context.WithValue(context.Background(), aiProjectFileProposalEmitterKey{}, aiProjectFileProposalEmitter(func(p aiProjectFileProposal) bool { emitted = append(emitted, p); return true }))
	if len(emitted) != 0 {
		t.Fatal("Execute published before acceptance")
	}
	if err := tool.AcceptResult(ctx, out+" "); err == nil {
		t.Fatal("forged acceptance")
	}
	if err := tool.AcceptResult(context.Background(), out); err == nil {
		t.Fatal("missing UI accepted")
	}
	if err := tool.AcceptResult(ctx, out); err != nil {
		t.Fatal(err)
	}
	if err := tool.AcceptResult(ctx, out); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 1 || emitted[0].Content != valid["content"] {
		t.Fatalf("incorrect publication: %+v", emitted)
	}
	if again, err := tool.Execute(ctx, raw); err != nil || again != out {
		t.Fatal("same proposal not idempotent")
	}
	valid["content"] = "another"
	raw, _ = json.Marshal(valid)
	if _, err := tool.Execute(ctx, raw); err == nil {
		t.Fatal("replaced pending proposal")
	}
	now = now.Add(6 * time.Minute)
	if err := tool.AcceptResult(ctx, out); err == nil {
		t.Fatal("expired acceptance")
	}
	if _, err := tool.Execute(ctx, raw); err == nil {
		t.Fatal("expired execution")
	}
}

func TestAIProjectFileProposalEmptyAndCancelled(t *testing.T) {
	now := time.Now()
	input := projectFileInput(models.AIProvider{ID: uuid.NewString(), Version: 1}, uuid.NewString(), now)
	file := input.ProjectFiles.Files[0]
	raw, _ := json.Marshal(map[string]string{"path": file.Path, "base_sha256": file.SHA256, "content": ""})
	tool := newAIProjectFileProposalTool(input.ProjectFiles, func() time.Time { return now })
	out, err := tool.Execute(context.Background(), raw)
	if err != nil || tool.pending.Content != "" {
		t.Fatal("empty replacement is a valid review, not deletion")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tool.AcceptResult(ctx, out); err == nil {
		t.Fatal("cancelled acceptance")
	}
	if _, err := tool.Execute(ctx, raw); err == nil {
		t.Fatal("cancelled execution")
	}
}

func TestAIProjectFileProposalGenerationPrivacy(t *testing.T) {
	a, provider, session, _ := newGenerationExecutionFixture(t)
	input := projectFileInput(provider, session.ID, a.options.Now())
	file := input.ProjectFiles.Files[0]
	const candidate = "PRIVATE CANDIDATE\r\n"
	raw, _ := json.Marshal(map[string]string{"path": file.Path, "base_sha256": file.SHA256, "content": candidate})
	client := &generationExecutionClient{}
	client.stream = func(context.Context) (harness.Turn, error) {
		if len(client.requests) == 1 {
			return harness.Turn{ToolCalls: []harness.ToolCall{{ID: "edit-one", Name: "workspace_propose_file_edit", Arguments: raw}}}, nil
		}
		return harness.Turn{Text: `已提供审查建议，未写入。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}, nil
	}
	a.harnessClient = client
	var events []string
	result, err := a.runAIGeneration(context.Background(), input, aiGenerationRunOptions{RequestKey: "file-proposal"}, func(event, payload string) bool {
		if event == "project_file_proposal" {
			events = append(events, payload)
		}
		return true
	})
	if err != nil || result.Status != "completed" || len(events) != 1 {
		t.Fatalf("generation %+v %v events=%v", result, err, events)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(events[0]), &event); err != nil {
		t.Fatal(err)
	}
	if len(event) != 5 || event["generation_id"] != result.GenerationID || event["content"] != candidate || event["base_sha256"] != file.SHA256 {
		t.Fatalf("event: %v", event)
	}
	var messages []models.AIMessage
	var steps []models.AIRunStep
	a.db.Where("session_id = ?", session.ID).Find(&messages)
	a.db.Where("generation_id = ?", result.GenerationID).Find(&steps)
	stored, _ := json.Marshal([]any{messages, steps})
	if strings.Contains(string(stored), candidate) || strings.Contains(string(stored), "PRIVATE CANDIDATE") || strings.Contains(string(stored), "PRIVATE FILE SOURCE") {
		t.Fatal("raw file or candidate persisted")
	}
	// A new generation and even a fresh full workbench grant cannot inherit the file tool.
	registry, err := a.aiChatToolRegistry(session.ID, true, &provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("workspace_propose_file_edit"); ok {
		t.Fatal("file capability inherited")
	}
	before := len(events)
	if _, err := a.runAIGeneration(context.Background(), input, aiGenerationRunOptions{RequestKey: "file-proposal"}, func(event, payload string) bool { events = append(events, payload); return true }); err != nil {
		t.Fatal(err)
	}
	if len(events) != before {
		t.Fatal("replayed ephemeral proposal")
	}
}
