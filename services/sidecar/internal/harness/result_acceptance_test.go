package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type acceptingTool struct {
	*fakeTool
	accepted          int
	reject            bool
	release, finished chan struct{}
}

func (t *acceptingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if t.release != nil {
		<-t.release
		defer close(t.finished)
	}
	return t.fakeTool.Execute(ctx, args)
}
func (t *acceptingTool) AcceptResult(ctx context.Context, output string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if t.reject || !json.Valid([]byte(output)) {
		return errors.New("result not accepted")
	}
	t.accepted++
	return nil
}
func TestRunAcceptsOnlySuccessfulUntruncatedResults(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		reject, fail, truncate, timeout bool
		accepted                        int
	}{
		{name: "accepted", accepted: 1}, {name: "policy", reject: true}, {name: "failed", fail: true}, {name: "truncated", truncate: true}, {name: "late timeout", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := &acceptingTool{fakeTool: &fakeTool{name: "read", result: `{"content":"private"}`}, reject: tc.reject}
			if tc.fail {
				tool.err = errors.New("failed")
			}
			executor := &Executor{}
			if tc.truncate {
				executor.MaxResultBytes = 5
			}
			if tc.timeout {
				tool.release = make(chan struct{})
				tool.finished = make(chan struct{})
				executor.Timeout = 10 * time.Millisecond
			}
			registry, _ := NewRegistry(tool)
			client := &fakeClient{streams: []Turn{{ToolCalls: []ToolCall{{ID: "call", Name: "read", Arguments: json.RawMessage(`{}`)}}}, {Text: "answer"}}}
			result, err := Run(context.Background(), client, Request{Model: "test"}, registry, executor, Callbacks{})
			if tc.timeout {
				close(tool.release)
				<-tool.finished
			}
			if err != nil || tool.accepted != tc.accepted {
				t.Fatalf("accepted=%d result=%+v err=%v", tool.accepted, result, err)
			}
			if tc.accepted == 0 && (result.Corrections != 1 || strings.Contains(client.lastReq.History[len(client.lastReq.History)-1].Content, "private")) {
				t.Fatal("rejected result leaked or did not correct")
			}
		})
	}
}
