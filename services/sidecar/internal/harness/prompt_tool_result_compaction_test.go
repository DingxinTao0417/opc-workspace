package harness

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

type requeryableFakeTool struct{ fakeTool }

func (*requeryableFakeTool) RequeryableResult() bool { return true }

type evidenceCompactorFakeTool struct {
	requeryableFakeTool
	capsule   string
	arguments string
	result    string
}

func (t *evidenceCompactorFakeTool) CompactResult(arguments json.RawMessage, result string) (string, bool) {
	t.arguments = string(arguments)
	t.result = result
	return t.capsule, true
}

func TestRunUsesReviewedEvidenceCapsuleForEarlierReadResult(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			readResult := strings.Repeat("READ-FACT", 3000)
			read := &evidenceCompactorFakeTool{
				requeryableFakeTool: requeryableFakeTool{fakeTool{name: "lookup", result: readResult}},
				capsule:             `{"kind":"compacted_read_evidence","id":"record-1","complete":false}`,
			}
			action := &fakeTool{name: "proposal", result: strings.Repeat("APPROVAL-RECEIPT", 1500)}
			registry, err := NewRegistry(read, action)
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Protocol: protocol, Model: "test", SystemPrompt: "CODE OWNED", Tools: registry.Definitions(), History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 0, 29000)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "read-1", Name: "lookup", Arguments: json.RawMessage(`{"id":"record-1"}`)}}},
				{ToolCalls: []ToolCall{{ID: "action-2", Name: "proposal"}}},
				{Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if err != nil || result.CompactedToolResults != 1 || len(client.requests) != 3 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[2]
			if last.History[2].Content != "lookup: "+read.capsule || read.arguments != `{"id":"record-1"}` || read.result != readResult {
				t.Fatalf("capsule=%q arguments=%q result-bytes=%d", last.History[2].Content, read.arguments, len(read.result))
			}
			if strings.Contains(last.History[2].Content, "READ-FACT") || !strings.Contains(last.SystemPrompt, ToolResultCompactionNotice) {
				t.Fatal("capsule retained removed facts or omitted the code-owned notice")
			}
		})
	}
}

func TestRunCompactsOnlyEarlierRequeryableResults(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			read := &requeryableFakeTool{fakeTool{name: "lookup", result: strings.Repeat("READ-FACT", 3000)}}
			action := &fakeTool{name: "proposal", result: strings.Repeat("APPROVAL-RECEIPT", 1500)}
			registry, err := NewRegistry(read, action)
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Protocol: protocol, Model: "test", SystemPrompt: "CODE OWNED", Tools: registry.Definitions(), History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 0, 29000)
			original := append([]modelclient.ChatMessage(nil), request.History...)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "read-1", Name: "lookup"}}},
				{ToolCalls: []ToolCall{{ID: "action-2", Name: "proposal"}}},
				{Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			var steps []RunStep
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{OnStep: func(step RunStep) { steps = append(steps, step) }})
			if err != nil || result.CompactedToolResults != 1 || len(client.requests) != 3 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[2]
			if promptWindowSize(t, last) > modelclient.MaxPromptBytes || !strings.Contains(last.SystemPrompt, ToolResultCompactionNotice) || strings.Contains(client.requests[1].SystemPrompt, ToolResultCompactionNotice) {
				t.Fatal("compaction notice was missing, early or outside exact budget")
			}
			if len(last.History) != 5 || last.History[2].ToolCallID != "read-1" || last.History[2].Content != compactedToolResult || last.History[4].ToolCallID != "action-2" || last.History[4].Content != "proposal: "+action.result {
				t.Fatal("compaction lost tool pairing or the latest/action receipt")
			}
			if client.requests[1].History[2].Content != "lookup: "+read.result {
				t.Fatal("later compaction mutated an already-sent provider request")
			}
			if strings.Contains(last.History[2].Content, "READ-FACT") || !reflect.DeepEqual(request.History, original) {
				t.Fatal("compaction retained old fact text or mutated caller history")
			}
			counts := []int{}
			for _, step := range steps {
				if step.Kind == "model_turn" {
					counts = append(counts, step.CompactedToolResults)
				}
			}
			if !reflect.DeepEqual(counts, []int{0, 0, 1}) {
				t.Fatalf("model step compaction counts = %v", counts)
			}
		})
	}
}

func TestRunNeverCompactsActionOrLatestReadResult(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			action := &fakeTool{name: "proposal", result: strings.Repeat("APPROVAL-RECEIPT", 1500)}
			read := &requeryableFakeTool{fakeTool{name: "lookup", result: strings.Repeat("READ-FACT", 3000)}}
			registry, _ := NewRegistry(action, read)
			request := Request{Protocol: protocol, Model: "test", Tools: registry.Definitions(), History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 0, 29000)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "action-1", Name: "proposal"}}},
				{ToolCalls: []ToolCall{{ID: "read-2", Name: "lookup"}}},
				{Text: "MUST NOT SEND"},
			}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if !errors.Is(err, modelclient.ErrPromptTooLarge) || len(client.requests) != 2 || result.CompactedToolResults != 0 {
				t.Fatalf("action/latest evidence was compacted: result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
		})
	}
}

func TestRunReadResultCompactionMovesForwardAcrossTurns(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			read := &requeryableFakeTool{fakeTool{name: "lookup", result: strings.Repeat("r", 18000)}}
			registry, _ := NewRegistry(read)
			request := Request{Protocol: protocol, Model: "test", Tools: registry.Definitions(), History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 0, 22000)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "read-1", Name: "lookup"}}},
				{ToolCalls: []ToolCall{{ID: "read-2", Name: "lookup"}}},
				{ToolCalls: []ToolCall{{ID: "read-3", Name: "lookup"}}},
				{Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if err != nil || len(client.requests) != 4 || result.CompactedToolResults != 2 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			third, fourth := client.requests[2], client.requests[3]
			if third.History[2].Content != compactedToolResult || third.History[4].Content != "lookup: "+read.result ||
				fourth.History[2].Content != compactedToolResult || fourth.History[4].Content != compactedToolResult || fourth.History[6].Content != "lookup: "+read.result {
				t.Fatal("prior compaction reversed or latest read result was removed")
			}
			if promptWindowSize(t, third) > modelclient.MaxPromptBytes || promptWindowSize(t, fourth) > modelclient.MaxPromptBytes {
				t.Fatal("compacted request still exceeded protocol bytes")
			}
		})
	}
}

func TestRunTrimsOldConversationBeforeCompactingCurrentRead(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			read := &requeryableFakeTool{fakeTool{name: "lookup", result: strings.Repeat("r", 27000)}}
			action := &fakeTool{name: "proposal", result: strings.Repeat("p", 24000)}
			registry, _ := NewRegistry(read, action)
			request := Request{Protocol: protocol, Model: "test", Tools: registry.Definitions(), History: []modelclient.ChatMessage{
				{Role: "user", Content: strings.Repeat("old", 3300)}, {Role: "assistant", Content: "old answer"},
				{Role: "user", Content: "CURRENT"},
			}}
			request = fillPromptWindow(t, request, 2, 29000)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "read-1", Name: "lookup"}}},
				{ToolCalls: []ToolCall{{ID: "action-2", Name: "proposal"}}},
				{Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if err != nil || result.ContextTrimmedTurns != 1 || result.CompactedToolResults != 1 || len(client.requests) != 3 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[2]
			if len(last.History) != 5 || last.History[0].Content != request.History[2].Content || last.History[2].Content != compactedToolResult || last.History[4].Content != "proposal: "+action.result ||
				!strings.Contains(last.SystemPrompt, ContextWindowNotice) || !strings.Contains(last.SystemPrompt, ToolResultCompactionNotice) || promptWindowSize(t, last) > modelclient.MaxPromptBytes {
				t.Fatal("old history and old read were not independently adjusted")
			}
		})
	}
}
