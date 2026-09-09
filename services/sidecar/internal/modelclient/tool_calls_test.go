package modelclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var toolCallTestDefinitions = []ToolDefinition{
	{
		Name: "memory_search", Description: "search memory",
		Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	},
	{
		Name: "memory_write", Description: "write session memory",
		Parameters: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`),
	},
}

func TestStreamChatAggregatesOpenAIToolCallFragments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if tools, _ := payload["tools"].([]any); len(tools) != 2 || payload["tool_choice"] != "auto" {
			t.Errorf("OpenAI tools payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"memory_","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"search","arguments":"{\"query\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"roadmap\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call-2","function":{"name":"memory_write","arguments":"{\"content\":\"next\"}"}}]},"finish_reason":"tool_calls"}]}`,
		}
		for _, frame := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
	defer server.Close()

	var calls []ToolCall
	err := StreamChat(
		context.Background(), ProtocolOpenAIChat, server.URL, "sk-test", "gpt-test",
		[]ChatMessage{{Role: "user", Content: "查一下"}},
		PromptContext{Tools: toolCallTestDefinitions}, nil, nil,
		func(received []ToolCall) { calls = append(calls, received...) }, nil, server.Client(),
	)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(calls) != 2 || calls[0].ID != "call-1" || calls[0].Name != "memory_search" || string(calls[0].Arguments) != `{"query":"roadmap"}` ||
		calls[1].ID != "call-2" || calls[1].Name != "memory_write" || string(calls[1].Arguments) != `{"content":"next"}` {
		t.Fatalf("tool calls = %#v", calls)
	}
}

func TestStreamChatAggregatesAnthropicToolUseFragments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"input_schema"`) || !strings.Contains(string(body), `"memory_search"`) {
			t.Errorf("Anthropic tools payload = %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"memory_search","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"client\"}"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu-2","name":"memory_write","input":{"content":"follow up"}}}`,
			`{"type":"message_stop"}`,
		}
		for _, frame := range frames {
			_, _ = fmt.Fprintf(w, "event: frame\ndata: %s\n\n", frame)
		}
	}))
	defer server.Close()

	var calls []ToolCall
	err := StreamChat(
		context.Background(), ProtocolAnthropicMessages, server.URL, "sk-test", "claude-test",
		[]ChatMessage{{Role: "user", Content: "查一下"}},
		PromptContext{Tools: toolCallTestDefinitions}, nil, nil,
		func(received []ToolCall) { calls = append(calls, received...) }, nil, server.Client(),
	)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(calls) != 2 || calls[0].Name != "memory_search" || string(calls[0].Arguments) != `{"query":"client"}` ||
		calls[1].Name != "memory_write" || string(calls[1].Arguments) != `{"content":"follow up"}` {
		t.Fatalf("tool calls = %#v", calls)
	}
}

func TestStreamChatRejectsMalformedToolArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"memory_search\",\"arguments\":\"{bad\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
	}))
	defer server.Close()
	err := StreamChat(
		context.Background(), ProtocolOpenAIChat, server.URL, "sk-test", "gpt-test", nil,
		PromptContext{Tools: toolCallTestDefinitions}, nil, nil, nil, nil, server.Client(),
	)
	if !errors.Is(err, ErrStream) {
		t.Fatalf("malformed tool arguments error = %v", err)
	}
}

func TestStreamChatRedactsAPIKeyFromProviderStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"error\":{\"code\":\"bad_key\",\"message\":\"credential sk-secret rejected\"}}\n\n")
	}))
	defer server.Close()
	err := StreamChat(
		context.Background(), ProtocolOpenAIChat, server.URL, "sk-secret", "gpt-test", nil,
		PromptContext{}, nil, nil, nil, nil, server.Client(),
	)
	if !errors.Is(err, ErrStream) || strings.Contains(err.Error(), "sk-secret") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("stream secret redaction error = %v", err)
	}
}

func TestToolCallHistoryMapsToBothProviderProtocols(t *testing.T) {
	history := []ChatMessage{
		{Role: "user", Content: "查资料"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call-1", Name: "memory_search", Arguments: json.RawMessage(`{"query":"project"}`)}}},
		{Role: "tool", Content: "memory_search: result", ToolCallID: "call-1", ToolName: "memory_search"},
	}
	for _, test := range []struct {
		protocol Protocol
		marker   string
	}{
		{ProtocolOpenAIChat, `"tool_call_id":"call-1"`},
		{ProtocolAnthropicMessages, `"type":"tool_result"`},
	} {
		t.Run(string(test.protocol), func(t *testing.T) {
			body, _, _, err := buildChatRequest(test.protocol, "https://api.example.com/v1", "sk-test", "m", history, PromptContext{Tools: toolCallTestDefinitions})
			if err != nil {
				t.Fatalf("buildChatRequest: %v", err)
			}
			if !strings.Contains(body, test.marker) || !strings.Contains(body, `"memory_search"`) {
				t.Fatalf("mapped tool history = %s", body)
			}
		})
	}
}
