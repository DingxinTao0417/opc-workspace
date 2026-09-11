package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

func TestSelfCheckUsesActualPromptExamplesAndNeverPassesUnknown(t *testing.T) {
	for _, line := range strings.Split(modelclient.SystemPrompt, "\n") {
		if strings.HasPrefix(line, selfCheckOpen) {
			verdict, stripped := parseSelfCheck("draft" + line)
			if !strings.Contains(line, selfCheckClose) || stripped != "draft" {
				t.Fatalf("prompt example cannot be parsed: %q", line)
			}
			if strings.Contains(line, `"sufficient":false`) && verdict.sufficient {
				t.Fatalf("insufficient example treated as sufficient: %q", line)
			}
		}
	}
	for _, text := range []string{"reply", "reply[opc:selfcheck]null[/opc:selfcheck]", "reply[opc:selfcheck]{}[/opc:selfcheck]", "reply[opc:selfcheck]{\"sufficient\":true}", `reply[opc:selfcheck]{"sufficient":true}[/opc:selfcheck][opc:selfcheck]null`, "reply" + strings.Repeat(`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`, 20000)} {
		client := &fakeClient{streams: []Turn{{Text: text}}}
		var steps []RunStep
		result, err := Run(context.Background(), client, Request{}, nil, nil, Callbacks{OnStep: func(s RunStep) { steps = append(steps, s) }})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.Text, selfCheckOpen) {
			t.Fatal("selfcheck control leaked from repeated declarations")
		}
		for _, s := range steps {
			if s.Kind == "self_check" && (s.Status == "succeeded" || s.ErrorCode != "SELF_CHECK_UNAVAILABLE") {
				t.Fatalf("unknown check claimed success: %+v", s)
			}
		}
	}
}

func TestRunResponseBudgetIncludesEarlierTurnsAndRevision(t *testing.T) {
	tool := &fakeTool{name: "lookup", result: "ok"}
	registry, _ := NewRegistry(tool)
	client := &fakeClient{streams: []Turn{
		{Text: strings.Repeat("a", 600000), ToolCalls: []ToolCall{{ID: "c", Name: "lookup", Arguments: json.RawMessage(`{}`)}}},
		{Text: strings.Repeat("b", 600000)},
	}}
	var emitted int
	result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnDelta: func(s string) { emitted += len(s) }})
	if err == nil || len(result.Text) > modelclient.MaxResponseBytes || emitted > modelclient.MaxResponseBytes {
		t.Fatalf("budget err=%v result=%d emitted=%d", err, len(result.Text), emitted)
	}
	client = &fakeClient{streams: []Turn{
		{Text: strings.Repeat("a", 600000) + `[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`},
		{Text: strings.Repeat("b", 600000)},
	}}
	_, err = Run(context.Background(), client, Request{}, nil, nil, Callbacks{})
	if err == nil {
		t.Fatal("revision reset response budget")
	}
}

type deadlineProbeClient struct {
	deadlines []time.Time
	calls     int
}

func (c *deadlineProbeClient) Stream(ctx context.Context, _ Request, _ func(string), _ func(string)) (Turn, error) {
	d, ok := ctx.Deadline()
	if !ok {
		return Turn{}, errors.New("no run deadline")
	}
	c.deadlines = append(c.deadlines, d)
	c.calls++
	if c.calls == 1 {
		return Turn{Text: `draft[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`}, nil
	}
	return Turn{Text: "revision"}, nil
}
func TestRunAndRevisionShareDeadline(t *testing.T) {
	c := &deadlineProbeClient{}
	_, err := Run(context.Background(), c, Request{}, nil, nil, Callbacks{})
	if err != nil || len(c.deadlines) != 2 || !c.deadlines[0].Equal(c.deadlines[1]) {
		t.Fatalf("deadlines=%v err=%v", c.deadlines, err)
	}
}

type deadlineWaitingClient struct{}

func (deadlineWaitingClient) Stream(ctx context.Context, _ Request, _ func(string), _ func(string)) (Turn, error) {
	<-ctx.Done()
	return Turn{Text: "partial"}, ctx.Err()
}
func TestRunDeadlineIsTimeoutNotCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := Run(ctx, deadlineWaitingClient{}, Request{}, nil, nil, Callbacks{})
	if !errors.Is(err, modelclient.ErrTimeout) || result.Text != "partial" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestSelfCheckInsufficientWithoutDraftNeverPassesOrRevises(t *testing.T) {
	for _, fixture := range []struct{ name, prefix string }{{"empty", ""}, {"whitespace", " \n\t"}} {
		t.Run(fixture.name, func(t *testing.T) {
			client := &fakeClient{streams: []Turn{{Text: fixture.prefix + `[opc:selfcheck]{"sufficient":false,"note":"No answer was produced"}[/opc:selfcheck]`}}}
			var checks []RunStep
			result, err := Run(context.Background(), client, Request{}, nil, nil, Callbacks{OnStep: func(step RunStep) {
				if step.Kind == "self_check" {
					checks = append(checks, step)
				}
			}})
			if err != nil || result.Text != "" || client.calls != 1 || result.Reflections != 0 {
				t.Fatalf("empty draft was revised or leaked: result=%+v calls=%d err=%v", result, client.calls, err)
			}
			if len(checks) != 1 || checks[0].Status != "failed" || checks[0].ErrorCode != "SELF_CHECK_INSUFFICIENT" {
				t.Fatalf("insufficient empty draft must remain a failed self-check: %+v", checks)
			}
		})
	}
}
