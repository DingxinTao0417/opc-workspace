package harness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

func TestRunEighthToolLoopRoundIsTextOnlyHandoff(t *testing.T) {
	tool := &fakeTool{name: "lookup", result: "actual bounded result"}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatal(err)
	}
	var turns []Turn
	for i := 1; i < DefaultMaxTurns; i++ {
		turns = append(turns, Turn{ToolCalls: []ToolCall{{ID: fmt.Sprintf("lookup-%d", i), Name: "lookup"}}})
	}
	turns = append(turns, Turn{Text: `已准备建议，仍需人工确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})
	client := &fakeClient{streams: turns}
	result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
	if err != nil || !result.BudgetHandoff || client.calls != DefaultMaxTurns || result.ToolCalls != DefaultMaxTurns-1 {
		t.Fatalf("handoff result=%+v calls=%d err=%v", result, client.calls, err)
	}
	if client.lastReq.Tools != nil {
		t.Fatalf("last model round still exposes tools: %+v", client.lastReq.Tools)
	}
}

func handoffToolTurns(count int) []Turn {
	turns := make([]Turn, count)
	for i := range turns {
		turns[i] = Turn{ToolCalls: []ToolCall{{ID: fmt.Sprintf("lookup-%d", i+1), Name: "lookup"}}}
	}
	return turns
}

type handoffRecordingClient struct {
	fakeClient
	requests []Request
	before   func(int)
}

func (c *handoffRecordingClient) Stream(ctx context.Context, request Request, onDelta, onReasoning func(string)) (Turn, error) {
	c.requests = append(c.requests, request)
	if c.before != nil {
		c.before(len(c.requests))
	}
	return c.fakeClient.Stream(ctx, request, onDelta, onReasoning)
}

func TestBudgetHandoffPreservesFinalToolEvidenceAndOriginalContext(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			tool := &flakyTool{name: "lookup", result: `{"proposal_id":"real-proposal","status":"pending","instruction":"NOT executed"}`}
			registry, _ := NewRegistry(tool)
			turns := append(handoffToolTurns(7), Turn{Text: `Still awaiting confirmation.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: turns}}
			request := Request{Protocol: protocol, Model: "test", SystemPrompt: "ORIGINAL CONSENT BOUNDARY", History: []modelclient.ChatMessage{{Role: "user", Content: "original request"}},
				Memories: []string{"memory"}, Summary: "summary", Facts: []string{"fact"}, BusinessContext: []string{"work context"}, KnowledgeContext: []string{"knowledge context"}, ActionReceipts: "receipt facts"}
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{})
			if err != nil || !result.BudgetHandoff || result.Reflections != 0 || tool.calls != 7 || len(client.requests) != 8 {
				t.Fatalf("result=%+v requests=%d tools=%d err=%v", result, len(client.requests), tool.calls, err)
			}
			for _, prior := range client.requests[:7] {
				if len(prior.Tools) != 1 || prior.SystemPrompt != request.SystemPrompt {
					t.Fatal("normal tool rounds changed")
				}
			}
			last := client.requests[7]
			if last.Tools != nil || !strings.HasPrefix(last.SystemPrompt, request.SystemPrompt) || !strings.HasSuffix(last.SystemPrompt, BudgetHandoffPrompt) {
				t.Fatalf("unsafe final request: %+v", last)
			}
			if last.Protocol != protocol || last.Model != request.Model || !reflect.DeepEqual(last.Memories, request.Memories) || last.Summary != request.Summary || !reflect.DeepEqual(last.Facts, request.Facts) || !reflect.DeepEqual(last.BusinessContext, request.BusinessContext) || !reflect.DeepEqual(last.KnowledgeContext, request.KnowledgeContext) || last.ActionReceipts != request.ActionReceipts {
				t.Fatal("handoff changed the authorized context")
			}
			if len(last.History) != 15 || !reflect.DeepEqual(last.History[0], request.History[0]) {
				t.Fatalf("lost user/history: %+v", last.History)
			}
			for i := 1; i < len(last.History); i += 2 {
				assistant, output := last.History[i], last.History[i+1]
				if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || output.Role != "tool" || output.ToolCallID != assistant.ToolCalls[0].ID || output.Content != "lookup: "+tool.result {
					t.Fatalf("lost complete tool/result pair: %+v %+v", assistant, output)
				}
			}
			if len(registry.Definitions()) != 1 || request.SystemPrompt != "ORIGINAL CONSENT BOUNDARY" {
				t.Fatal("handoff mutated the registry or caller request")
			}
		})
	}
	last := requestForBudgetHandoff(Request{})
	if !strings.HasPrefix(last.SystemPrompt, modelclient.SystemPrompt) {
		t.Fatal("empty custom prompt lost the default safety rules")
	}
}

func TestBudgetHandoffReportsSelfCheckWithoutNinthRound(t *testing.T) {
	for _, tc := range []struct{ name, text, status, code string }{
		{"sufficient", `Pending approval.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`, "succeeded", ""},
		{"insufficient", `Unfinished.[opc:selfcheck]{"sufficient":false,"note":"Need another tool"}[/opc:selfcheck]`, "failed", "SELF_CHECK_INSUFFICIENT"},
		{"missing", "Unfinished.", "failed", "SELF_CHECK_UNAVAILABLE"},
		{"malformed", `Unfinished.[opc:selfcheck]invalid[/opc:selfcheck]`, "failed", "SELF_CHECK_UNAVAILABLE"},
		{"empty insufficient", `[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`, "failed", "SELF_CHECK_INSUFFICIENT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
			client := &fakeClient{streams: append(handoffToolTurns(7), Turn{Text: tc.text}, Turn{Text: "MUST NOT RUN"})}
			var checks []RunStep
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) {
				if step.Kind == "self_check" {
					checks = append(checks, step)
				}
			}})
			if err != nil || !result.BudgetHandoff || result.Turns != 8 || client.calls != 8 || result.Reflections != 0 || strings.Contains(result.Text, selfCheckOpen) {
				t.Fatalf("result=%+v calls=%d err=%v", result, client.calls, err)
			}
			if len(checks) != 1 || checks[0].TurnIndex != 8 || checks[0].Status != tc.status || checks[0].ErrorCode != tc.code {
				t.Fatalf("false self-check facts: %+v", checks)
			}
		})
	}
}

func TestBudgetHandoffRejectsAllFinalToolCallsBeforeExecution(t *testing.T) {
	for _, name := range []string{"lookup", "unknown_unauthorized_tool"} {
		t.Run(name, func(t *testing.T) {
			tool := &flakyTool{name: "lookup", result: "ok"}
			registry, _ := NewRegistry(tool)
			client := &fakeClient{streams: append(handoffToolTurns(7), Turn{ToolCalls: []ToolCall{{ID: "forbidden", Name: name}}})}
			var lastStep RunStep
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) { lastStep = step }})
			if !errors.Is(err, ErrMaxTurns) || result.BudgetHandoff || tool.calls != 7 || result.ToolCalls != 7 || client.calls != 8 {
				t.Fatalf("last-round call executed or succeeded: %+v tools=%d calls=%d err=%v", result, tool.calls, client.calls, err)
			}
			if lastStep.Kind != "model_turn" || lastStep.TurnIndex != 8 || lastStep.Status != "failed" || lastStep.ErrorCode != "TURN_BUDGET_EXHAUSTED" {
				t.Fatalf("final tool refusal did not emit the truthful failed model turn: %+v", lastStep)
			}
		})
	}
	tool := &flakyTool{name: "lookup", result: "ok"}
	registry, _ := NewRegistry(tool)
	client := &fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "ungranted", Name: "unknown"}}}}}
	result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
	if !errors.Is(err, ErrToolUnavailable) || result.BudgetHandoff || tool.calls != 0 {
		t.Fatalf("ordinary unauthorized call changed: %+v %v", result, err)
	}
}

func TestBudgetHandoffPreservesHardErrorsAndCumulativeLimits(t *testing.T) {
	for _, failure := range []error{modelclient.ErrStream, modelclient.ErrIncompleteStream, modelclient.ErrTimeout, modelclient.ErrPromptTooLarge, modelclient.ErrResponseBudget, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
			client := &fakeClient{streams: handoffToolTurns(7), failOn: map[int]error{8: failure}}
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
			if !errors.Is(err, failure) || result.BudgetHandoff || client.calls != 8 {
				t.Fatalf("hard failure softened: %+v calls=%d err=%v", result, client.calls, err)
			}
		})
	}
	registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
	turns := handoffToolTurns(7)
	turns[0].Text = strings.Repeat("a", 600000)
	turns = append(turns, Turn{Text: strings.Repeat("b", 600000)})
	client := &fakeClient{streams: turns}
	result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
	if !errors.Is(err, modelclient.ErrResponseBudget) || result.BudgetHandoff || len(result.Text) > modelclient.MaxResponseBytes {
		t.Fatalf("handoff reset cumulative response budget: bytes=%d flag=%v err=%v", len(result.Text), result.BudgetHandoff, err)
	}
	broken := &flakyTool{name: "lookup", failures: 100}
	registry, _ = NewRegistry(broken)
	result, err = Run(context.Background(), &fakeClient{streams: handoffToolTurns(7)}, Request{}, registry, nil, Callbacks{})
	if !errors.Is(err, ErrToolCorrections) || result.BudgetHandoff || broken.calls != DefaultMaxToolCorrections+1 {
		t.Fatalf("tool failures became handoff: %+v %v", result, err)
	}
}

func TestBudgetHandoffCancellationNeverSetsSuccessFlag(t *testing.T) {
	for _, cancelAt := range []string{"model", "self_check_callback"} {
		t.Run(cancelAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: append(handoffToolTurns(7), Turn{Text: `handoff[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})}}
			client.before = func(round int) {
				if cancelAt == "model" && round == 8 {
					cancel()
				}
			}
			result, err := Run(ctx, client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) {
				if cancelAt == "self_check_callback" && step.Kind == "self_check" {
					cancel()
				}
			}})
			if !errors.Is(err, context.Canceled) || result.BudgetHandoff || client.calls != 8 {
				t.Fatalf("cancelled handoff succeeded: %+v calls=%d err=%v", result, client.calls, err)
			}
		})
	}
}

func TestBudgetHandoffAlsoAppliesToLastRoundSelfCheckRevision(t *testing.T) {
	const draft = `Incomplete draft.[opc:selfcheck]{"sufficient":false,"note":"Need the remaining work"}[/opc:selfcheck]`
	for _, tc := range []struct{ name, text, wantText, status, code string }{
		{"sufficient", `Pending approval.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`, "Pending approval.", "succeeded", ""},
		{"insufficient", `Remaining work.[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`, "Remaining work.", "failed", "SELF_CHECK_INSUFFICIENT"},
		{"missing", "Still awaiting confirmation.", "Still awaiting confirmation.", "failed", "SELF_CHECK_UNAVAILABLE"},
		{"identical", `Incomplete draft.[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`, "Incomplete draft.", "failed", "SELF_CHECK_INSUFFICIENT"},
		{"empty", `[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`, "Incomplete draft.", "failed", "SELF_CHECK_INSUFFICIENT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := &flakyTool{name: "lookup", result: `{"state":"pending","proposal_id":"frozen-proposal"}`}
			registry, _ := NewRegistry(tool)
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: append(handoffToolTurns(6), Turn{Text: draft}, Turn{Text: tc.text}, Turn{Text: "MUST NOT RUN"})}}
			request := Request{SystemPrompt: "ORIGINAL RULES", History: []modelclient.ChatMessage{{Role: "user", Content: "original request"}}}
			var checks []RunStep
			result, err := Run(context.Background(), client, request, registry, nil, Callbacks{OnStep: func(step RunStep) {
				if step.Kind == "self_check" {
					checks = append(checks, step)
				}
			}})
			if err != nil || !result.BudgetHandoff || client.calls != 8 || result.Turns != 8 || tool.calls != 6 || result.Text != tc.wantText {
				t.Fatalf("revision handoff result=%+v model=%d tools=%d err=%v", result, client.calls, tool.calls, err)
			}
			last := client.requests[7]
			if last.Tools != nil || !strings.HasPrefix(last.SystemPrompt, request.SystemPrompt) || !strings.HasSuffix(last.SystemPrompt, BudgetHandoffPrompt) || len(last.History) != 15 {
				t.Fatalf("unsafe last-round revision request: %+v", last)
			}
			if last.History[12].Role != "tool" || last.History[12].Content != "lookup: "+tool.result || last.History[12].ToolCallID != "lookup-6" || last.History[13].Role != "assistant" || last.History[13].Content != "Incomplete draft." || last.History[14].Role != "user" || last.History[14].Content != selfCheckNoteMessage("Need the remaining work") {
				t.Fatalf("revision lost actual tool evidence or changed its existing draft/note: %+v", last.History)
			}
			if len(checks) != 1 || checks[0].TurnIndex != 8 || checks[0].Status != tc.status || checks[0].ErrorCode != tc.code {
				t.Fatalf("revision self-check facts changed: %+v", checks)
			}
		})
	}
}

func TestBudgetHandoffRevisionCannotExecuteToolsOrSoftenFailures(t *testing.T) {
	const draft = `Draft.[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`
	for _, name := range []string{"lookup", "unknown_unauthorized_tool"} {
		t.Run(name, func(t *testing.T) {
			tool := &flakyTool{name: "lookup", result: "ok"}
			registry, _ := NewRegistry(tool)
			client := &fakeClient{streams: append(handoffToolTurns(6), Turn{Text: draft}, Turn{ToolCalls: []ToolCall{{ID: "forbidden", Name: name}}})}
			var lastStep RunStep
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) { lastStep = step }})
			if !errors.Is(err, ErrMaxTurns) || result.BudgetHandoff || tool.calls != 6 || result.ToolCalls != 6 || client.calls != 8 || result.Text != "Draft." {
				t.Fatalf("revision tool call was executed or softened: %+v tools=%d model=%d err=%v", result, tool.calls, client.calls, err)
			}
			if lastStep.Kind != "self_check" || lastStep.Status != "failed" || lastStep.TurnIndex != 8 || lastStep.ErrorCode != "TURN_BUDGET_EXHAUSTED" {
				t.Fatalf("wrong revision failure telemetry: %+v", lastStep)
			}
		})
	}
	for _, failure := range []error{modelclient.ErrStream, modelclient.ErrIncompleteStream, modelclient.ErrTimeout, modelclient.ErrPromptTooLarge, modelclient.ErrResponseBudget, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
			client := &fakeClient{streams: append(handoffToolTurns(6), Turn{Text: draft}), failOn: map[int]error{8: failure}}
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
			if !errors.Is(err, failure) || result.BudgetHandoff || client.calls != 8 || result.Text != "Draft." {
				t.Fatalf("revision failure was softened: %+v calls=%d err=%v", result, client.calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry, _ := NewRegistry(&fakeTool{name: "lookup", result: "ok"})
	client := &fakeClient{streams: append(handoffToolTurns(6), Turn{Text: draft}, Turn{Text: "handoff"})}
	result, err := Run(ctx, client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) {
		if step.Kind == "self_check" {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) || result.BudgetHandoff || client.calls != 8 {
		t.Fatalf("cancelled revision claimed a successful handoff: %+v calls=%d err=%v", result, client.calls, err)
	}
}

func TestBudgetHandoffKeepsThirtyTwoToolCallLimitAndOrdinaryReplies(t *testing.T) {
	tool := &flakyTool{name: "lookup", result: "ok"}
	registry, _ := NewRegistry(tool)
	var turns []Turn
	for round, count := range []int{5, 5, 5, 5, 4, 4, 4} {
		turn := Turn{}
		for i := 0; i < count; i++ {
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: fmt.Sprintf("tool-%d-%d", round, i), Name: "lookup"})
		}
		turns = append(turns, turn)
	}
	client := &fakeClient{streams: append(turns, Turn{Text: "Pending approval."})}
	result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
	if err != nil || !result.BudgetHandoff || result.ToolCalls != DefaultMaxToolCalls || tool.calls != DefaultMaxToolCalls || client.calls != 8 {
		t.Fatalf("32-tool boundary changed: %+v tools=%d model=%d err=%v", result, tool.calls, client.calls, err)
	}
	for _, replies := range [][]Turn{
		{{Text: `Done.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}},
		{{Text: `Draft.[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`}, {Text: `Revised.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}},
	} {
		client = &fakeClient{streams: replies}
		result, err = Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
		if err != nil || result.BudgetHandoff || result.Turns != len(replies) || strings.Contains(client.lastReq.SystemPrompt, BudgetHandoffPrompt) || len(client.lastReq.Tools) != 1 {
			t.Fatalf("ordinary reply/reflection incorrectly treated as handoff: %+v err=%v", result, err)
		}
	}
}
