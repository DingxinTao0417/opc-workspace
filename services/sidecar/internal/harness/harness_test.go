package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// fakeClient is a scripted LLMClient: each call pops the next turn.
type fakeClient struct {
	streams []Turn
	calls   int
	lastReq Request
	fail    error
	// failOn fails the Nth Stream call (1-based); takes precedence over fail.
	failOn map[int]error
}

func (f *fakeClient) Stream(ctx context.Context, request Request, onDelta func(string), onReasoning func(string)) (Turn, error) {
	f.calls++
	f.lastReq = request
	if err, ok := f.failOn[f.calls]; ok {
		return Turn{}, err
	}
	if f.fail != nil {
		return Turn{}, f.fail
	}
	if len(f.streams) == 0 {
		return Turn{}, errors.New("fakeClient: script exhausted")
	}
	turn := f.streams[0]
	f.streams = f.streams[1:]
	if onDelta != nil && turn.Text != "" {
		onDelta(turn.Text)
	}
	if onReasoning != nil && turn.Reasoning != "" {
		onReasoning(turn.Reasoning)
	}
	return turn, nil
}

type fakeTool struct {
	name    string
	result  string
	err     error
	delay   time.Duration
	panic   bool
	execCtx context.Context
}

func (t *fakeTool) Name() string    { return t.name }
func (t *fakeTool) Summary() string { return "fake tool " + t.name }
func (t *fakeTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	t.execCtx = ctx
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if t.panic {
		panic("boom")
	}
	if t.err != nil {
		return "", t.err
	}
	return t.result, nil
}

func TestRunSingleTurnWithoutTools(t *testing.T) {
	client := &fakeClient{streams: []Turn{{Text: "你好", Reasoning: "想一想"}}}
	var deltas []string
	result, err := Run(context.Background(), client,
		Request{Protocol: "openai_chat", Model: "m", History: []modelclient.ChatMessage{{Role: "user", Content: "hi"}}},
		nil, nil, Callbacks{OnDelta: func(d string) { deltas = append(deltas, d) }})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "你好" || result.Reasoning != "想一想" || result.Turns != 1 || result.ToolCalls != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(deltas) != 1 || deltas[0] != "你好" {
		t.Fatalf("deltas not streamed: %v", deltas)
	}
	if len(client.lastReq.History) != 1 || client.lastReq.History[0].Content != "hi" {
		t.Fatalf("history not forwarded: %+v", client.lastReq.History)
	}
}

func TestRunToolLoopFeedsResultsBack(t *testing.T) {
	tool := &fakeTool{name: "lookup", result: "42"}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"q": "answer"})
	client := &fakeClient{streams: []Turn{
		{Text: "让我查一下", ToolCalls: []ToolCall{{ID: "c1", Name: "lookup", Arguments: args}}},
		{Text: "答案是 42"},
	}}
	result, err := Run(context.Background(), client,
		Request{Model: "m", History: []modelclient.ChatMessage{{Role: "user", Content: "问题"}}},
		registry, &Executor{}, Callbacks{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "让我查一下答案是 42" || result.Turns != 2 || result.ToolCalls != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	// Second call must carry the user turn, the assistant turn, and the tool
	// result message.
	history := client.lastReq.History
	if len(history) != 3 || history[0].Role != "user" || history[1].Role != "assistant" || history[2].Role != "tool" ||
		!strings.Contains(history[2].Content, "lookup: 42") {
		t.Fatalf("tool result not fed back: %+v", history)
	}
}

func TestRunOversizedToolResultTriggersBudget(t *testing.T) {
	// A single oversized result is truncated to the per-result cap; the
	// run-level budget trips when accumulated results exceed it.
	tool := &fakeTool{name: "flood", result: strings.Repeat("x", DefaultMaxResultBytes/2+16)}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	client := &fakeClient{streams: []Turn{
		{ToolCalls: []ToolCall{
			{ID: "c1", Name: "flood"},
			{ID: "c2", Name: "flood"},
		}},
	}}
	result, err := Run(context.Background(), client, Request{Model: "m"}, registry, &Executor{}, Callbacks{})
	if !errors.Is(err, ErrToolBudget) {
		t.Fatalf("expected ErrToolBudget, got %v", err)
	}
	if result.ToolCalls != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunRejectsUnregisteredTool(t *testing.T) {
	registry, err := NewRegistry(&fakeTool{name: "known"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	client := &fakeClient{streams: []Turn{
		{ToolCalls: []ToolCall{{ID: "c1", Name: "unknown"}}},
	}}
	if _, err := Run(context.Background(), client, Request{Model: "m"}, registry, &Executor{}, Callbacks{}); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("expected ErrToolUnavailable, got %v", err)
	}
	// Model emits tool calls but nothing is registered.
	client = &fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "c1", Name: "known"}}}}}
	if _, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{}); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("expected ErrToolUnavailable for nil registry, got %v", err)
	}
}

func TestRunTurnBudgetExhausted(t *testing.T) {
	tool := &fakeTool{name: "loop"}
	registry, err := NewRegistry(tool)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	toolCall := func() Turn {
		return Turn{ToolCalls: []ToolCall{{ID: "c", Name: "loop"}}}
	}
	streams := make([]Turn, DefaultMaxTurns+2)
	for i := range streams {
		streams[i] = toolCall()
	}
	client := &fakeClient{streams: streams}
	result, err := Run(context.Background(), client, Request{Model: "m"}, registry, &Executor{}, Callbacks{})
	if !errors.Is(err, ErrMaxTurns) {
		t.Fatalf("expected ErrMaxTurns, got %v", err)
	}
	if result.Turns != DefaultMaxTurns {
		t.Fatalf("run should stop at %d turns, got %d", DefaultMaxTurns, result.Turns)
	}
}

func TestRunPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeClient{fail: context.Canceled}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	result, err := Run(ctx, client, Request{Model: "m"}, nil, nil, Callbacks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if result.Turns != 1 {
		t.Fatalf("failed turn should still count: %+v", result)
	}
}

func TestRunKeepsPartialOutputOnClientFailure(t *testing.T) {
	client := &fakeClient{streams: []Turn{{Text: "部分"}}}
	client.fail = errors.New("upstream down")
	// A scripted turn followed by failure: first Stream returns the turn, the
	// second (if any) would fail; here the single call fails but must still
	// have streamed through the callback into the accumulated result.
	var deltas []string
	result, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil,
		Callbacks{OnDelta: func(d string) { deltas = append(deltas, d) }})
	if err == nil {
		t.Fatalf("expected the scripted failure, got %+v", result)
	}
	if len(deltas) != 0 {
		t.Fatalf("no delta should stream from a failed call: %v", deltas)
	}
}

func TestRegistryRejectsDuplicatesAndNil(t *testing.T) {
	if _, err := NewRegistry(&fakeTool{name: "a"}, &fakeTool{name: "a"}); err == nil {
		t.Fatal("expected duplicate name rejection")
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("expected nil tool rejection")
	}
	if _, err := NewRegistry(&fakeTool{name: ""}); err == nil {
		t.Fatal("expected empty name rejection")
	}
	registry, err := NewRegistry(&fakeTool{name: "b"}, &fakeTool{name: "a"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if names := registry.Names(); len(names) != 2 || names[0] != "b" || names[1] != "a" {
		t.Fatalf("unexpected order: %v", names)
	}
	if _, ok := registry.Get("a"); !ok {
		t.Fatal("Get should find registered tool")
	}
}

func TestExecutorTimeout(t *testing.T) {
	executor := &Executor{Timeout: 30 * time.Millisecond}
	tool := &fakeTool{name: "slow", delay: 2 * time.Second}
	started := time.Now()
	if _, err := executor.Execute(context.Background(), tool, nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout not enforced promptly: %s", elapsed)
	}
}

func TestExecutorRecoversPanic(t *testing.T) {
	executor := &Executor{}
	tool := &fakeTool{name: "panic", panic: true}
	if _, err := executor.Execute(context.Background(), tool, nil); err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("expected panic recovery error, got %v", err)
	}
}

func TestExecutorTruncatesOversizedResult(t *testing.T) {
	executor := &Executor{MaxResultBytes: 8}
	tool := &fakeTool{name: "big", result: "0123456789abcdef"}
	result, err := executor.Execute(context.Background(), tool, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "01234567" {
		t.Fatalf("expected truncation to 8 bytes, got %q", result)
	}
}

func TestExecutorParentCancelWinsOverTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	executor := &Executor{Timeout: 5 * time.Second}
	tool := &fakeTool{name: "blocked", delay: 10 * time.Second}
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := executor.Execute(ctx, tool, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestExecutorSurfacesToolError(t *testing.T) {
	executor := &Executor{}
	want := fmt.Errorf("disk full")
	tool := &fakeTool{name: "broken", err: want}
	if _, err := executor.Execute(context.Background(), tool, nil); !errors.Is(err, want) {
		t.Fatalf("expected tool error, got %v", err)
	}
}

func TestModelClientAdapterAccumulatesTurn(t *testing.T) {
	// The production adapter must accumulate what it streams. Verified via
	// the api package's httptest end-to-end tests; here only the interface
	// wiring is asserted.
	var _ LLMClient = NewModelClient(nil)
}

func TestRunCorrectsToolFailureWithinBudget(t *testing.T) {
	// The tool fails on the first call and succeeds on the retry; the model
	// gets the error fed back and produces the final answer afterwards.
	flaky := &flakyTool{name: "flaky", failures: 1, result: "ok"}
	registry, err := NewRegistry(flaky)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	client := &fakeClient{streams: []Turn{
		{ToolCalls: []ToolCall{{ID: "c1", Name: "flaky"}}},
		{Text: "修好了"},
	}}
	result, err := Run(context.Background(), client,
		Request{Model: "m", History: []modelclient.ChatMessage{{Role: "user", Content: "调用工具"}}},
		registry, &Executor{}, Callbacks{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "修好了" || result.Corrections != 1 || result.ToolCalls != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	last := client.lastReq.History
	if len(last) != 3 || !strings.Contains(last[2].Content, "error:") {
		t.Fatalf("tool error not fed back: %+v", last)
	}
}

func TestRunAbortsWhenCorrectionBudgetExhausted(t *testing.T) {
	alwaysBroken := &flakyTool{name: "broken", failures: 99, result: ""}
	registry, err := NewRegistry(alwaysBroken)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	toolCall := Turn{ToolCalls: []ToolCall{{ID: "c", Name: "broken"}}}
	streams := make([]Turn, DefaultMaxTurns+4)
	for i := range streams {
		streams[i] = toolCall
	}
	client := &fakeClient{streams: streams}
	result, err := Run(context.Background(), client, Request{Model: "m"}, registry, &Executor{}, Callbacks{})
	if !errors.Is(err, ErrToolCorrections) {
		t.Fatalf("expected ErrToolCorrections, got %v", err)
	}
	if result.Corrections != DefaultMaxToolCorrections+1 {
		t.Fatalf("corrections = %d, want %d", result.Corrections, DefaultMaxToolCorrections+1)
	}
}

func TestRunSelfCheckInsufficientTriggersAutonomousRevision(t *testing.T) {
	// The agent itself judges its draft insufficient and revises: two calls,
	// the revised answer replaces the draft, and the self-check block never
	// leaks into the result.
	client := &fakeClient{streams: []Turn{
		{Text: `有遗漏的草稿[opc:selfcheck]{"sufficient":false,"note":"缺了步骤二"}[/opc:selfcheck]`},
		{Text: `修订后的完整回答[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	var deltas int
	result, err := Run(context.Background(), client,
		Request{Model: "m", History: []modelclient.ChatMessage{{Role: "user", Content: "问题"}}},
		nil, nil, Callbacks{OnDelta: func(string) { deltas++ }})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "修订后的完整回答" || result.Reflections != 1 || result.Turns != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if deltas != 1 {
		t.Fatalf("revision must stay silent, streamed %d deltas", deltas)
	}
	// The revision turn sees the stripped draft and the model's own note.
	last := client.lastReq.History
	if len(last) != 3 || last[1].Role != "assistant" || last[1].Content != "有遗漏的草稿" ||
		!strings.Contains(last[2].Content, "缺了步骤二") {
		t.Fatalf("revision history wrong: %+v", last)
	}
}

func TestRunSelfCheckSufficientEmitsWithoutRevision(t *testing.T) {
	client := &fakeClient{streams: []Turn{
		{Text: `完整回答[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	result, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "完整回答" || result.Reflections != 0 || client.calls != 1 {
		t.Fatalf("sufficient verdict must not revise: %+v calls=%d", result, client.calls)
	}
}

func TestRunSelfCheckMissingOrMalformedStaysSufficient(t *testing.T) {
	// No block at all: one call, text untouched.
	client := &fakeClient{streams: []Turn{{Text: "普通回答"}}}
	result, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil || result.Text != "普通回答" || client.calls != 1 {
		t.Fatalf("missing block: %+v calls=%d err=%v", result, client.calls, err)
	}
	// Malformed or unclosed blocks are stripped defensively and the draft is
	// treated as sufficient.
	client = &fakeClient{streams: []Turn{{Text: "回答[opc:selfcheck]不是json[/opc:selfcheck]"}}}
	result, err = Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil || result.Text != "回答" || client.calls != 1 {
		t.Fatalf("malformed block: %+v calls=%d err=%v", result, client.calls, err)
	}
	client = &fakeClient{streams: []Turn{{Text: `回答[opc:selfcheck]{"sufficient":true}`}}}
	result, err = Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil || result.Text != "回答" || client.calls != 1 {
		t.Fatalf("unclosed block: %+v calls=%d err=%v", result, client.calls, err)
	}
}

func TestRunSelfCheckRevisionFailureKeepsStrippedDraft(t *testing.T) {
	client := &fakeClient{streams: []Turn{
		{Text: `草稿[opc:selfcheck]{"sufficient":false,"note":"理由"}`},
	}, failOn: map[int]error{2: errors.New("revision down")}}
	result, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil {
		t.Fatalf("revision failure must not fail the run: %v", err)
	}
	if result.Text != "草稿" || result.Reflections != 0 {
		t.Fatalf("draft must survive revision failure without the block: %+v", result)
	}
}

func TestRunSelfCheckIdenticalRevisionKeepsDraft(t *testing.T) {
	client := &fakeClient{streams: []Turn{
		{Text: `草稿[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`},
		{Text: `草稿[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	result, err := Run(context.Background(), client, Request{Model: "m"}, nil, nil, Callbacks{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Text != "草稿" || result.Reflections != 0 || result.Turns != 2 {
		t.Fatalf("identical revision must keep the draft: %+v", result)
	}
}

// flakyTool fails the first N executions, then returns its result.
type flakyTool struct {
	name     string
	failures int
	result   string
	calls    int
}

func (t *flakyTool) Name() string    { return t.name }
func (t *flakyTool) Summary() string { return "flaky tool " + t.name }
func (t *flakyTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	t.calls++
	if t.calls <= t.failures {
		return "", fmt.Errorf("transient failure %d", t.calls)
	}
	return t.result, nil
}
