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

func TestRunPromptWindowPreservesMultipleToolPairsAndReportsExactTurn(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			// The raw tool result fits the spare bytes; JSON escaping does not.
			tool := &fakeTool{name: "lookup", result: strings.Repeat("<>&\"\n", 40)}
			registry, _ := NewRegistry(tool)
			request := Request{Protocol: protocol, Model: "test", Tools: registry.Definitions(), History: []modelclient.ChatMessage{
				{Role: "user", Content: "old:"},
				{Role: "assistant", ToolCalls: []ToolCall{{ID: "old-a", Name: "lookup", Arguments: json.RawMessage(`{"escaped":"<>&\""}`)}, {ID: "old-b", Name: "lookup"}}},
				{Role: "tool", ToolCallID: "old-a", Content: "old evidence A"},
				{Role: "tool", ToolCallID: "old-b", Content: "old evidence B"},
				{Role: "assistant", Content: "old final"},
				{Role: "user", Content: "CURRENT"},
			}}
			request = fillPromptWindow(t, request, 0, 900)
			encoded, _ := json.Marshal(request)
			calls := []ToolCall{{ID: "current-a", Name: "lookup", Arguments: json.RawMessage(`{"q":"<>&\""}`)}, {ID: "current-b", Name: "lookup"}}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{ToolCalls: calls}, {Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}}}}
			var steps, starts []RunStep
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{
				OnStep:      func(step RunStep) { steps = append(steps, step) },
				OnStepStart: func(step RunStep) { starts = append(starts, step) },
			})
			if err != nil || result.ContextTrimmedTurns != 1 || len(client.requests) != 2 {
				t.Fatalf("result=%+v calls=%d err=%v", result, len(client.requests), err)
			}
			last := client.requests[1]
			if len(last.History) != 4 || last.History[0].Content != "CURRENT" || !reflect.DeepEqual(last.History[1].ToolCalls, calls) || last.History[2].ToolCallID != "current-a" || last.History[3].ToolCallID != "current-b" || last.History[2].Content != "lookup: "+tool.result || last.History[3].Content != "lookup: "+tool.result {
				t.Fatal("old or current paired calls/results were cut incorrectly")
			}
			if strings.Contains(client.requests[0].SystemPrompt, ContextWindowNotice) || strings.Count(last.SystemPrompt, ContextWindowNotice) != 1 || promptWindowSize(t, last) > modelclient.MaxPromptBytes {
				t.Fatal("window notice is missing, unbudgeted, or present before trimming")
			}
			for _, step := range steps {
				want := 0
				if step.Kind == "model_turn" && step.TurnIndex == 2 {
					want = 1
				}
				if step.TrimmedHistoryTurns != want {
					t.Fatalf("wrong per-request count: %+v", step)
				}
			}
			for _, start := range starts {
				if start.TrimmedHistoryTurns != 0 {
					t.Fatalf("start claimed an unprepared trim: %+v", start)
				}
			}
			unchanged, _ := json.Marshal(request)
			if string(encoded) != string(unchanged) {
				t.Fatal("caller history or nested tool call arguments mutated")
			}
		})
	}
}

type promptWindowLargeSchemaTool struct{ fakeTool }

func (*promptWindowLargeSchemaTool) Summary() string { return strings.Repeat("tool-schema", 900) }

func TestRunPromptWindowDoesNotRestoreOldHistoryAtBudgetHandoff(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, revision := range []bool{false, true} {
			name := protocol + "/model"
			if revision {
				name = protocol + "/self-check"
			}
			t.Run(name, func(t *testing.T) {
				tool := &promptWindowLargeSchemaTool{fakeTool{name: "lookup", result: strings.Repeat("e", 250)}}
				registry, _ := NewRegistry(tool)
				request := Request{Protocol: protocol, Model: "test", SystemPrompt: "ORIGINAL", Tools: registry.Definitions(), History: []modelclient.ChatMessage{
					{Role: "user", Content: "old:"}, {Role: "assistant", Content: "old answer"}, {Role: "user", Content: strings.Repeat("current", 6000)},
				}}
				request = fillPromptWindow(t, request, 0, 100)
				turns := handoffToolTurns(7)
				if revision {
					turns = append(handoffToolTurns(6), Turn{Text: `DRAFT[opc:selfcheck]{"sufficient":false,"note":"finish"}[/opc:selfcheck]`})
				}
				turns = append(turns, Turn{Text: `Handoff[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})
				client := &handoffRecordingClient{fakeClient: fakeClient{streams: turns}}
				var steps []RunStep
				result, err := Run(context.Background(), client, request, registry, nil, Callbacks{OnStep: func(step RunStep) { steps = append(steps, step) }})
				if err != nil || !result.BudgetHandoff || result.ContextTrimmedTurns != 1 || len(client.requests) != 8 {
					t.Fatalf("result=%+v calls=%d err=%v", result, len(client.requests), err)
				}
				for i, sent := range client.requests[1:] {
					if sent.History[0].Content != request.History[2].Content || strings.Count(sent.SystemPrompt, ContextWindowNotice) != 1 || promptWindowSize(t, sent) > modelclient.MaxPromptBytes {
						t.Fatalf("request %d restored old history or lost its bounded notice", i+2)
					}
				}
				last := client.requests[7]
				if len(last.Tools) != 0 || !strings.Contains(last.SystemPrompt, BudgetHandoffPrompt) {
					t.Fatal("window changed budget handoff semantics")
				}
				// Removing tools frees enough space for the original history. It
				// must still stay removed, not silently return for the final turn.
				withOld := last
				withOld.History = append(append([]modelclient.ChatMessage(nil), request.History[:2]...), last.History...)
				if promptWindowSize(t, withOld) > modelclient.MaxPromptBytes {
					t.Fatal("fixture does not prove the monotonic cursor")
				}
				total := 0
				for _, step := range steps {
					total += step.TrimmedHistoryTurns
				}
				if total != result.ContextTrimmedTurns {
					t.Fatalf("step count=%d result=%d", total, result.ContextTrimmedTurns)
				}
			})
		}
	}
}

func TestRunPromptWindowFailureAndCancellationKeepTrimAccounting(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, outcome := range []string{"provider", "cancelled", "protected-overflow"} {
			t.Run(protocol+"/"+outcome, func(t *testing.T) {
				request := Request{Protocol: protocol, Model: "test", History: []modelclient.ChatMessage{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old reply"}, {Role: "user", Content: "CURRENT:"}}}
				request = fillPromptWindow(t, request, 2, 300)
				tool := &fakeTool{name: "lookup", result: strings.Repeat("e", 2000)}
				registry, _ := NewRegistry(tool)
				// Keep the accepted first request below the limit including its
				// tools, then force an irreducible evidence overflow on round two.
				if outcome == "protected-overflow" {
					request.Tools = registry.Definitions()
					request.History[2].Content = "CURRENT:"
					request = fillPromptWindow(t, request, 2, 512)
				} else {
					request.History[0].Content += strings.Repeat("old-padding", 400)
					// After removing the old turn, the notice also needs space.
					request.History[2].Content = request.History[2].Content[:len(request.History[2].Content)-2048]
					registry = nil
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "new-call", Name: "lookup"}}}}}}
				wantErr := modelclient.ErrPromptTooLarge
				if outcome == "provider" {
					wantErr = errors.New("provider failure")
					client.fail = wantErr
				}
				if outcome == "cancelled" {
					wantErr = context.Canceled
					client.before = func(int) { cancel() }
				}
				var steps []RunStep
				result, err := Run(ctx, client, request, registry, nil, Callbacks{OnStep: func(step RunStep) { steps = append(steps, step) }})
				if !errors.Is(err, wantErr) || result.ContextTrimmedTurns != 1 || len(client.requests) != 1 {
					t.Fatalf("result=%+v calls=%d err=%v want=%v", result, len(client.requests), err, wantErr)
				}
				last := steps[len(steps)-1]
				if last.TrimmedHistoryTurns != 1 || (outcome == "protected-overflow" && last.InputBytes <= modelclient.MaxPromptBytes) {
					t.Fatalf("failure lost actual trim metadata: %+v", last)
				}
			})
		}
	}
}

func TestPromptWindowNoticeParticipatesInExactBudget(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			request := Request{Protocol: protocol, Model: "test", History: []modelclient.ChatMessage{{Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 0, 100)
			request.History = append([]modelclient.ChatMessage{{Role: "user", Content: strings.Repeat("old", 200)}, {Role: "assistant", Content: "old reply"}}, request.History...)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{Text: "MUST NOT SEND"}}}}
			result, err := Run(context.Background(), client, request, nil, nil, Callbacks{})
			if !errors.Is(err, modelclient.ErrPromptTooLarge) || result.ContextTrimmedTurns != 1 || len(client.requests) != 0 {
				t.Fatalf("unbudgeted notice: result=%+v calls=%d err=%v", result, len(client.requests), err)
			}
		})
	}
}

func TestPromptWindowUnknownProtocolFailsBeforeClientAndCancellationWins(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		client := &fakeClient{streams: []Turn{{Text: "MUST NOT SEND"}}}
		result, err := Run(ctx, client, Request{Protocol: "unknown", Model: "test"}, nil, nil, Callbacks{})
		cancel()
		if err == nil || client.calls != 0 || result.ContextTrimmedTurns != 0 || (cancelled && !errors.Is(err, context.Canceled)) {
			t.Fatalf("result=%+v calls=%d err=%v cancelled=%t", result, client.calls, err, cancelled)
		}
	}
}

func TestRunPromptWindowAdvancesAcrossRequestsWithoutTrimmingPrefix(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			tool := &fakeTool{name: "lookup", result: strings.Repeat("e", 20000)}
			registry, _ := NewRegistry(tool)
			request := Request{Protocol: protocol, Model: "test", SystemPrompt: "original", History: []modelclient.ChatMessage{
				{Role: "assistant", Content: "protected non-turn prefix"},
				{Role: "user", Content: strings.Repeat("a", 15000)}, {Role: "assistant", Content: "old A"},
				{Role: "user", Content: strings.Repeat("b", 15000)}, {Role: "assistant", Content: "old B"},
				{Role: "user", Content: strings.Repeat("c", 15000)}, {Role: "assistant", Content: "recent C"},
				{Role: "user", Content: "CURRENT"},
			}}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: append(handoffToolTurns(2), Turn{Text: `Done[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})}}
			var counts []int
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{OnStep: func(step RunStep) {
				if step.Kind == "model_turn" {
					counts = append(counts, step.TrimmedHistoryTurns)
				}
			}})
			if err != nil || result.ContextTrimmedTurns != 2 || !reflect.DeepEqual(counts, []int{0, 1, 1}) || len(client.requests) != 3 {
				t.Fatalf("result=%+v counts=%v calls=%d err=%v", result, counts, len(client.requests), err)
			}
			for index, sent := range client.requests {
				if sent.History[0].Content != request.History[0].Content || sent.History[1].Content != request.History[1+2*index].Content || promptWindowSize(t, sent) > modelclient.MaxPromptBytes {
					t.Fatalf("request %d lost retained prefix or nearest old turn", index+1)
				}
			}
		})
	}
}

func TestRunPromptWindowSelfCheckCannotDropCurrentDraftToFit(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			request := Request{Protocol: protocol, Model: "test", History: []modelclient.ChatMessage{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old answer"}, {Role: "user", Content: "CURRENT"}}}
			request = fillPromptWindow(t, request, 2, 512)
			draft := strings.Repeat("draft", 1000)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{{Text: draft + `[opc:selfcheck]{"sufficient":false,"note":"revise"}[/opc:selfcheck]`}, {Text: "MUST NOT SEND"}}}}
			var steps []RunStep
			result, err := Run(context.Background(), client, request, nil, nil, Callbacks{OnStep: func(step RunStep) { steps = append(steps, step) }})
			if !errors.Is(err, modelclient.ErrPromptTooLarge) || result.ContextTrimmedTurns != 1 || result.Text != draft || result.Reflections != 0 || len(client.requests) != 1 {
				t.Fatalf("protected revision wrongly reduced: result=%+v calls=%d err=%v", result, len(client.requests), err)
			}
			last := steps[len(steps)-1]
			if last.Kind != "self_check" || last.Status != "failed" || last.TrimmedHistoryTurns != 1 || last.InputBytes <= modelclient.MaxPromptBytes {
				t.Fatalf("revision failure lost trim accounting: %+v", last)
			}
		})
	}
}
