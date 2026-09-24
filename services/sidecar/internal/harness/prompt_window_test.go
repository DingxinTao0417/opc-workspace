package harness

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

func promptWindowSize(t *testing.T, request Request) int {
	t.Helper()
	size, err := modelclient.PromptSize(modelclient.Protocol(request.Protocol), request.Model, request.History, modelclient.PromptContext{
		SystemPrompt: request.SystemPrompt, Memories: request.Memories, Summary: request.Summary, Facts: request.Facts,
		BusinessContext: request.BusinessContext, KnowledgeContext: request.KnowledgeContext, ActionReceipts: request.ActionReceipts, Tools: request.Tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	return size
}

func fillPromptWindow(t *testing.T, request Request, messageIndex, spare int) Request {
	t.Helper()
	padding := modelclient.MaxPromptBytes - spare - promptWindowSize(t, request)
	if padding < 0 {
		t.Fatal("fixture already exceeds target size")
	}
	request.History[messageIndex].Content += strings.Repeat("x", padding)
	return request
}

func TestRunPromptWindowDropsOnlyCompleteOldTurns(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			tool := &fakeTool{name: "lookup", result: strings.Repeat("e", 4096)}
			registry, _ := NewRegistry(tool)
			request := Request{Protocol: protocol, Model: "test", SystemPrompt: "ORIGINAL SAFETY", Memories: []string{"memory"}, Summary: "summary", Facts: []string{"fact"}, BusinessContext: []string{"explicit business"}, KnowledgeContext: []string{"explicit knowledge"}, ActionReceipts: "real receipts", Tools: registry.Definitions(), History: []modelclient.ChatMessage{
				{Role: "user", Content: "old-user:"}, {Role: "assistant", Content: "old-assistant"},
				{Role: "user", Content: "recent-user"}, {Role: "assistant", Content: "recent-assistant"},
				{Role: "user", Content: "CURRENT USER INTENT"},
			}}
			request = fillPromptWindow(t, request, 0, 512)
			originalHistory := append([]modelclient.ChatMessage(nil), request.History...)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "call-1", Name: "lookup"}}},
				{Text: `Done.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if err != nil || len(client.requests) != 2 || result.Turns != 2 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[1]
			if size := promptWindowSize(t, last); size > modelclient.MaxPromptBytes {
				t.Fatalf("second tool round exceeds prompt budget: %d", size)
			}
			if len(last.History) != 5 || !reflect.DeepEqual(last.History[:3], originalHistory[2:]) || last.History[3].ToolCalls[0].ID != "call-1" || last.History[4].ToolCallID != "call-1" || last.History[4].Content != "lookup: "+tool.result {
				t.Fatal("window lost current intent, recent complete turn, or current tool pair")
			}
			if !reflect.DeepEqual(request.History, originalHistory) || !reflect.DeepEqual(last.Memories, request.Memories) || last.Summary != request.Summary || !reflect.DeepEqual(last.Facts, request.Facts) || !reflect.DeepEqual(last.BusinessContext, request.BusinessContext) || !reflect.DeepEqual(last.KnowledgeContext, request.KnowledgeContext) || last.ActionReceipts != request.ActionReceipts || !reflect.DeepEqual(last.Tools, request.Tools) {
				t.Fatal("window mutated the caller, authorization, or independent context layers")
			}
		})
	}
}

func TestRunPromptWindowPinsCurrentTurnDuringSelfCheck(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			request := Request{Protocol: protocol, Model: "test", History: []modelclient.ChatMessage{{Role: "user", Content: "old:"}, {Role: "assistant", Content: "old reply"}, {Role: "user", Content: "CURRENT INTENT"}}}
			request = fillPromptWindow(t, request, 0, 512)
			draft := strings.Repeat("draft", 500)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{Text: draft + `[opc:selfcheck]{"sufficient":false,"note":"Need revision"}[/opc:selfcheck]`}, {Text: `Revised.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}}}}
			result, err := Run(context.Background(), client, request, nil, nil, Callbacks{})
			if err != nil || result.Reflections != 1 || len(client.requests) != 2 {
				t.Fatalf("result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[1]
			if promptWindowSize(t, last) > modelclient.MaxPromptBytes || len(last.History) != 3 || last.History[0].Content != "CURRENT INTENT" || last.History[1].Content != draft || last.History[2].Content != selfCheckNoteMessage("Need revision") {
				t.Fatal("self-check did not retain the original user/draft/instruction as one protected suffix")
			}
		})
	}
}

func TestRunPromptWindowRefusesProtectedEvidenceOverflow(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			tool := &fakeTool{name: "lookup", result: strings.Repeat("e", 4096)}
			registry, _ := NewRegistry(tool)
			request := Request{Protocol: protocol, Model: "test", Tools: registry.Definitions(), History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT:"}}}
			request = fillPromptWindow(t, request, 0, 512)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "call-1", Name: "lookup"}}}, {Text: "MUST NOT SEND"}}}}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if !errors.Is(err, modelclient.ErrPromptTooLarge) || len(client.requests) != 1 || result.ToolCalls != 1 {
				t.Fatalf("protected evidence was sent oversized or silently discarded: result=%+v requests=%d err=%v", result, len(client.requests), err)
			}
		})
	}
}
