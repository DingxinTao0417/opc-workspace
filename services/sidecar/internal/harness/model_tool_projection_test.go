package harness

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

type modelToolProjection interface {
	ModelDefinitions() []modelclient.ToolDefinition
	SelectModelTools([]string) error
}

func modelProjection(t *testing.T, registry *Registry) modelToolProjection {
	t.Helper()
	projection, ok := any(registry).(modelToolProjection)
	if !ok {
		t.Fatal("Registry has no independent model-facing tool projection")
	}
	return projection
}

func modelProjectionNames(definitions []modelclient.ToolDefinition) []string {
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	return names
}

func assertModelProjectionNames(t *testing.T, definitions []modelclient.ToolDefinition, want ...string) {
	t.Helper()
	got := modelProjectionNames(definitions)
	if len(got) != len(want) || (len(want) != 0 && !reflect.DeepEqual(got, want)) {
		t.Fatalf("model tool projection=%v want=%v", got, want)
	}
}

func TestModelToolProjectionDoesNotChangeAuthorization(t *testing.T) {
	registry, err := NewRegistry(&fakeTool{name: "guide"}, &fakeTool{name: "read"}, &fakeTool{name: "propose"})
	if err != nil {
		t.Fatal(err)
	}
	projection := modelProjection(t, registry)
	assertModelProjectionNames(t, projection.ModelDefinitions(), "guide", "read", "propose")
	if err := projection.SelectModelTools([]string{"propose", "guide"}); err != nil {
		t.Fatal(err)
	}
	assertModelProjectionNames(t, projection.ModelDefinitions(), "guide", "propose")
	assertModelProjectionNames(t, registry.Definitions(), "guide", "read", "propose")
	if !reflect.DeepEqual(registry.Names(), []string{"guide", "read", "propose"}) {
		t.Fatal("model projection removed authorization")
	}
	if _, allowed := registry.Get("read"); !allowed {
		t.Fatal("hidden tool was revoked instead of only hidden")
	}
	for _, invalid := range [][]string{{"unknown"}, {"guide", "unknown"}, {"guide", "guide"}, {""}} {
		if err := projection.SelectModelTools(invalid); err == nil {
			t.Fatalf("invalid selection accepted: %v", invalid)
		}
		assertModelProjectionNames(t, projection.ModelDefinitions(), "guide", "propose")
	}
	if err := projection.SelectModelTools([]string{}); err != nil {
		t.Fatal(err)
	}
	assertModelProjectionNames(t, projection.ModelDefinitions())
	if err := projection.SelectModelTools(nil); err != nil {
		t.Fatal(err)
	}
	assertModelProjectionNames(t, projection.ModelDefinitions(), "guide", "read", "propose")
}

type projectionAcceptingTool struct {
	name           string
	projection     modelToolProjection
	selection      []string
	accepted       atomic.Int32
	fail           bool
	started        chan struct{}
	release        chan struct{}
	finished       chan struct{}
	cancelOnReturn context.CancelFunc
}

func (tool *projectionAcceptingTool) Name() string { return tool.name }
func (tool *projectionAcceptingTool) Summary() string {
	return "Select already-authorized model definitions"
}
func (tool *projectionAcceptingTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if tool.started != nil {
		close(tool.started)
	}
	if tool.release != nil {
		<-tool.release
		defer close(tool.finished)
	}
	if tool.fail {
		return "", errors.New("guide unavailable")
	}
	if tool.cancelOnReturn != nil {
		tool.cancelOnReturn()
	}
	return `{"accepted_guide":"facts remain data"}`, nil
}
func (tool *projectionAcceptingTool) AcceptResult(ctx context.Context, _ string) error {
	// Deliberately no context guard here: Harness must never call this hook
	// after it has already observed cancellation/timeout from the Executor.
	tool.accepted.Add(1)
	return tool.projection.SelectModelTools(tool.selection)
}

func TestModelToolProjectionRefreshesAfterAcceptedGuideAndForSelfCheck(t *testing.T) {
	for _, selfCheck := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "self_check"}[selfCheck], func(t *testing.T) {
			guide := &projectionAcceptingTool{name: "guide", selection: []string{"guide", "read"}}
			registry, _ := NewRegistry(guide, &fakeTool{name: "read"}, &fakeTool{name: "propose"})
			projection := modelProjection(t, registry)
			guide.projection = projection
			if err := projection.SelectModelTools([]string{"guide"}); err != nil {
				t.Fatal(err)
			}
			second := Turn{Text: `Ready.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}
			if selfCheck {
				second.Text = `Draft.[opc:selfcheck]{"sufficient":false,"note":"Revise wording"}[/opc:selfcheck]`
			}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "guide-1", Name: "guide"}}}, second,
				{Text: `Revised.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, Request{Protocol: "openai_chat", Model: "test"}, registry, nil, Callbacks{OnStepStart: func(step RunStep) {
				if selfCheck && step.Kind == "self_check" && step.TurnIndex == 3 {
					if err := projection.SelectModelTools([]string{"guide", "propose"}); err != nil {
						t.Error(err)
					}
				}
			}})
			if err != nil || guide.accepted.Load() != 1 || result.ToolCalls != 1 {
				t.Fatalf("projection run=%+v accepted=%d err=%v", result, guide.accepted.Load(), err)
			}
			wantCalls := 2
			if selfCheck {
				wantCalls++
			}
			if len(client.requests) != wantCalls {
				t.Fatalf("requests=%d want=%d", len(client.requests), wantCalls)
			}
			assertModelProjectionNames(t, client.requests[0].Tools, "guide")
			assertModelProjectionNames(t, client.requests[1].Tools, "guide", "read")
			if selfCheck {
				assertModelProjectionNames(t, client.requests[2].Tools, "guide", "propose")
				if result.Reflections != 1 {
					t.Fatal("revision did not run")
				}
			}
		})
	}
}

type projectionSchemaTool struct {
	*fakeTool
	schemaCalls atomic.Int32
	schema      json.RawMessage
}

func (tool *projectionSchemaTool) InputSchema() json.RawMessage {
	tool.schemaCalls.Add(1)
	return tool.schema
}

func TestModelToolProjectionSchemaIsEvaluatedOnceAndNeverAliased(t *testing.T) {
	tool := &projectionSchemaTool{fakeTool: &fakeTool{name: "read"}, schema: json.RawMessage(`{"type":"object","properties":{}}`)}
	registry, _ := NewRegistry(tool)
	projection := modelProjection(t, registry)
	definitions := registry.Definitions()
	if tool.schemaCalls.Load() != 1 {
		t.Fatalf("InputSchema called %d times for one definition", tool.schemaCalls.Load())
	}
	definitions[0].Parameters[0] = 'x'
	definitions = projection.ModelDefinitions()
	if !json.Valid(definitions[0].Parameters) || tool.schemaCalls.Load() != 2 {
		t.Fatal("definition schema alias leaked or was evaluated more than once")
	}
	if err := projection.SelectModelTools([]string{}); err != nil {
		t.Fatal(err)
	}
	assertModelProjectionNames(t, projection.ModelDefinitions())
	if tool.schemaCalls.Load() != 2 {
		t.Fatal("hidden tool schema was still evaluated")
	}
}

func TestModelToolProjectionConcurrentReadAndAtomicSelection(t *testing.T) {
	registry, _ := NewRegistry(&fakeTool{name: "guide"}, &fakeTool{name: "read"})
	projection := modelProjection(t, registry)
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			for count := 0; count < 50; count++ {
				if index%2 == 0 {
					if err := projection.SelectModelTools([]string{"read"}); err != nil {
						t.Error(err)
					}
					if err := projection.SelectModelTools(nil); err != nil {
						t.Error(err)
					}
				} else {
					names := modelProjectionNames(projection.ModelDefinitions())
					if !reflect.DeepEqual(names, []string{"read"}) && !reflect.DeepEqual(names, []string{"guide", "read"}) {
						t.Errorf("partial projection observed: %v", names)
					}
					if _, exists := registry.Get("guide"); !exists {
						t.Error("authorization raced with projection")
					}
				}
			}
		}(worker)
	}
	workers.Wait()
}

func TestModelToolProjectionRejectedGuideNeverSwitches(t *testing.T) {
	for _, scenario := range []string{"failure", "timeout", "cancel", "cancel_on_return"} {
		t.Run(scenario, func(t *testing.T) {
			guide := &projectionAcceptingTool{name: "guide", selection: []string{"read"}}
			registry, _ := NewRegistry(guide, &fakeTool{name: "read"})
			projection := modelProjection(t, registry)
			guide.projection = projection
			if err := projection.SelectModelTools([]string{"guide"}); err != nil {
				t.Fatal(err)
			}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "guide-1", Name: "guide"}}},
				{Text: `Guide failed.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			executor := &Executor{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "failure" {
				guide.fail = true
			} else if scenario == "cancel_on_return" {
				guide.cancelOnReturn = cancel
			} else {
				guide.started, guide.release, guide.finished = make(chan struct{}), make(chan struct{}), make(chan struct{})
				if scenario == "timeout" {
					executor.Timeout = 10 * time.Millisecond
				}
				if scenario == "cancel" {
					go func() { <-guide.started; cancel() }()
				}
			}
			result, err := Run(ctx, client, Request{}, registry, executor, Callbacks{})
			if guide.release != nil {
				close(guide.release)
				<-guide.finished
			}
			cancelled := scenario == "cancel" || scenario == "cancel_on_return"
			if (cancelled && !errors.Is(err, context.Canceled)) || (!cancelled && err != nil) || guide.accepted.Load() != 0 {
				t.Fatalf("scenario=%s result=%+v accepted=%d err=%v", scenario, result, guide.accepted.Load(), err)
			}
			assertModelProjectionNames(t, projection.ModelDefinitions(), "guide")
			for _, request := range client.requests {
				assertModelProjectionNames(t, request.Tools, "guide")
			}
		})
	}
}

func TestModelToolProjectionNeverRevivesToolsInFinalHandoff(t *testing.T) {
	for _, scenario := range []string{"finish", "revision", "illegal_tool"} {
		t.Run(scenario, func(t *testing.T) {
			guide := &projectionAcceptingTool{name: "guide", selection: []string{"read"}}
			read := &fakeTool{name: "read", result: "must never execute in final round"}
			registry, _ := NewRegistry(guide, read)
			projection := modelProjection(t, registry)
			guide.projection = projection
			if err := projection.SelectModelTools([]string{"guide"}); err != nil {
				t.Fatal(err)
			}
			toolRounds := DefaultMaxTurns - 1
			if scenario == "revision" {
				toolRounds--
			}
			turns := make([]Turn, toolRounds)
			for index := range turns {
				turns[index] = Turn{ToolCalls: []ToolCall{{ID: "guide-call", Name: "guide"}}}
			}
			if scenario == "revision" {
				turns = append(turns, Turn{Text: `Need clearer answer.[opc:selfcheck]{"sufficient":false}[/opc:selfcheck]`})
			}
			if scenario == "illegal_tool" {
				turns = append(turns, Turn{ToolCalls: []ToolCall{{ID: "forbidden-final", Name: "read"}}})
			} else {
				turns = append(turns, Turn{Text: `Stopped at budget.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`})
			}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: turns}}
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{OnStepStart: func(step RunStep) {
				if step.TurnIndex == DefaultMaxTurns {
					// A catalog switch immediately before the last request must
					// not undo the stronger no-tools handoff restriction.
					if err := projection.SelectModelTools(nil); err != nil {
						t.Error(err)
					}
				}
			}})
			if (scenario == "illegal_tool" && !errors.Is(err, ErrMaxTurns)) || (scenario != "illegal_tool" && err != nil) {
				t.Fatalf("final handoff result=%+v err=%v", result, err)
			}
			if len(client.requests) != DefaultMaxTurns || read.execCtx != nil || int(guide.accepted.Load()) != toolRounds {
				t.Fatalf("final request executed tools or consumed another round: calls=%d result=%+v", len(client.requests), result)
			}
			assertModelProjectionNames(t, client.requests[0].Tools, "guide")
			assertModelProjectionNames(t, client.requests[1].Tools, "read")
			if client.requests[DefaultMaxTurns-1].Tools != nil {
				t.Fatal("last request revived projected tools")
			}
			assertModelProjectionNames(t, registry.Definitions(), "guide", "read")
		})
	}
}

func TestModelToolProjectionHiddenToolRemainsAuthorizedAndUnknownToolDoesNot(t *testing.T) {
	for _, name := range []string{"read", "unregistered"} {
		t.Run(name, func(t *testing.T) {
			read := &fakeTool{name: "read", result: "permitted result"}
			registry, _ := NewRegistry(&fakeTool{name: "guide"}, read)
			projection := modelProjection(t, registry)
			if err := projection.SelectModelTools([]string{"guide"}); err != nil {
				t.Fatal(err)
			}
			client := &handoffRecordingClient{fakeClient: fakeClient{streams: []Turn{
				{ToolCalls: []ToolCall{{ID: "read-call", Name: name}}},
				{Text: `Done.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
			}}}
			result, err := Run(context.Background(), client, Request{}, registry, nil, Callbacks{})
			if name == "read" {
				if err != nil || read.execCtx == nil || result.ToolCalls != 1 {
					t.Fatalf("projection incorrectly revoked explicit authorization: %+v err=%v", result, err)
				}
			} else if !errors.Is(err, ErrToolUnavailable) || read.execCtx != nil {
				t.Fatalf("projection expanded authorization: %+v err=%v", result, err)
			}
			for _, request := range client.requests {
				assertModelProjectionNames(t, request.Tools, "guide")
			}
		})
	}
}
