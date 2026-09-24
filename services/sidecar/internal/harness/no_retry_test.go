package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

type noRetryRecordingClient struct {
	requests []Request
	turns    []Turn
}

func (m *noRetryRecordingClient) Stream(_ context.Context, request Request, _, _ func(string)) (Turn, error) {
	m.requests = append(m.requests, request)
	turn := m.turns[0]
	m.turns = m.turns[1:]
	return turn, nil
}

func TestRunRetainsNoRetryPolicyThroughToolsAndSelfCheck(t *testing.T) {
	tools, err := NewRegistry(&fakeTool{name: "lookup", result: "evidence"})
	if err != nil {
		t.Fatal(err)
	}
	client := &noRetryRecordingClient{turns: []Turn{
		{ToolCalls: []ToolCall{{ID: "lookup1", Name: "lookup", Arguments: json.RawMessage(`{}`)}}},
		{Text: `draft[opc:selfcheck]{"sufficient":false,"note":"revise"}[/opc:selfcheck]`},
		{Text: `revised[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`},
	}}
	result, err := Run(context.Background(), client, Request{
		Protocol: "openai_chat", Model: "test", DisableTransientRetries: true,
		History: []modelclient.ChatMessage{{Role: "user", Content: "check"}},
	}, tools, nil, Callbacks{})
	if err != nil || result.Turns != 3 || len(client.requests) != 3 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for index, request := range client.requests {
		if !request.DisableTransientRetries {
			t.Errorf("request %d silently reenabled retries", index+1)
		}
	}
}
