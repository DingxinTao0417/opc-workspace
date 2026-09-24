package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunCancelledToolReportsTerminalStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry, _ := NewRegistry(&fakeTool{name: "read"})
	client := &fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "one", Name: "read"}}}}}
	var steps []RunStep
	_, err := Run(ctx, client, Request{}, registry, nil, Callbacks{OnStep: func(step RunStep) {
		steps = append(steps, step)
		if step.Kind == "model_turn" {
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) || len(steps) != 2 || steps[1].Kind != "tool_call" || steps[1].Status != "cancelled" {
		t.Fatalf("cancelled tool lost its terminal step: %+v %v", steps, err)
	}
}

type progressBlockedTool struct {
	started  chan struct{}
	release  chan struct{}
	returned chan struct{}
}

func (t *progressBlockedTool) Name() string    { return "read" }
func (t *progressBlockedTool) Summary() string { return "read" }
func (t *progressBlockedTool) Execute(context.Context, json.RawMessage) (string, error) {
	close(t.started)
	<-t.release // Deliberately ignores cancellation to exercise late completion.
	close(t.returned)
	return "private result", nil
}

func TestRunToolProgressStartsBeforeExecutionAndSettlesTimeoutOnce(t *testing.T) {
	tool := &progressBlockedTool{started: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}
	defer func() { close(tool.release); <-tool.returned }()
	registry, _ := NewRegistry(tool)
	client := &fakeClient{streams: []Turn{
		{ToolCalls: []ToolCall{{ID: "private-id", Name: "read"}}},
		{Text: `could not read[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	var events []RunStep
	_, err := Run(context.Background(), client, Request{}, registry, &Executor{Timeout: 30 * time.Millisecond}, Callbacks{
		OnStepStart: func(step RunStep) {
			if step.Kind == "model_turn" && client.calls != step.TurnIndex-1 {
				t.Fatal("model already started")
			}
			if step.Kind == "tool_call" {
				select {
				case <-tool.started:
					t.Fatal("tool already started")
				default:
				}
			}
			events = append(events, step)
		},
		OnStep: func(step RunStep) { events = append(events, step) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 || events[2].Status != "running" || events[3].Status != "failed" || events[3].ErrorCode != "TOOL_TIMEOUT" || events[3].StartedAt != events[2].StartedAt {
		t.Fatalf("bad timeout progress: %+v", events)
	}
	// The late worker only returns to Executor's buffered channel. It has no
	// progress callback and cannot turn the timed-out step into a success.
}

func TestRunRevisionProgressHasMatchingStartAndTerminalIdentity(t *testing.T) {
	client := &fakeClient{streams: []Turn{
		{Text: `draft[opc:selfcheck]{"sufficient":false,"note":"private note"}[/opc:selfcheck]`},
		{Text: `revised[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	var starts, ends []RunStep
	result, err := Run(context.Background(), client, Request{}, nil, nil, Callbacks{
		OnStepStart: func(step RunStep) { starts = append(starts, step) },
		OnStep:      func(step RunStep) { ends = append(ends, step) },
	})
	if err != nil || result.Reflections != 1 || len(starts) != 2 || len(ends) != 2 {
		t.Fatal(result, err, starts, ends)
	}
	if starts[1].Kind != "self_check" || starts[1].TurnIndex != 2 || starts[1].Status != "running" || starts[1].StartedAt != ends[1].StartedAt || ends[1].Status != "succeeded" {
		t.Fatal(starts, ends)
	}
}

func TestRunUnavailableToolDoesNotExposeModelTextInSteps(t *testing.T) {
	registry, _ := NewRegistry(&fakeTool{name: "read"})
	client := &fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "one", Name: "private-model-text"}}}}}
	var step RunStep
	_, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnStep: func(value RunStep) { step = value }})
	if !errors.Is(err, ErrToolUnavailable) || step.Kind != "tool_call" || strings.Contains(step.ToolName, "private") {
		t.Fatalf("model text entered trace: %+v %v", step, err)
	}
}
