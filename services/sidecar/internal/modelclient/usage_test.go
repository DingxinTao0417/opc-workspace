package modelclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderUsageAccumulatorRequiresCompleteNonNegativeIntegers(t *testing.T) {
	var openAI providerUsageAccumulator
	openAI.apply(ProtocolOpenAIChat, `{"usage":{"prompt_tokens":120,"completion_tokens":34,"total_tokens":154}}`)
	if usage, ok := openAI.complete(); !ok || usage.InputTokens != 120 || usage.OutputTokens != 34 {
		t.Fatalf("OpenAI usage=%#v ok=%v", usage, ok)
	}
	var anthropic providerUsageAccumulator
	anthropic.apply(ProtocolAnthropicMessages, `{"type":"message_start","message":{"usage":{"input_tokens":88,"output_tokens":0}}}`)
	anthropic.apply(ProtocolAnthropicMessages, `{"type":"message_delta","usage":{"output_tokens":21}}`)
	if usage, ok := anthropic.complete(); !ok || usage.InputTokens != 88 || usage.OutputTokens != 21 {
		t.Fatalf("Anthropic usage=%#v ok=%v", usage, ok)
	}
	for name, frame := range map[string]string{
		"partial":  `{"usage":{"prompt_tokens":1}}`,
		"negative": `{"usage":{"prompt_tokens":-1,"completion_tokens":2}}`,
		"fraction": `{"usage":{"prompt_tokens":1.5,"completion_tokens":2}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var accumulator providerUsageAccumulator
			accumulator.apply(ProtocolOpenAIChat, frame)
			if _, ok := accumulator.complete(); ok {
				t.Fatalf("invalid usage completed for %s", frame)
			}
		})
	}
}

func TestStreamChatEmitsProviderUsageOnlyAtSuccessfulFinish(t *testing.T) {
	for _, test := range []struct {
		name     string
		protocol Protocol
		frames   []string
		want     Usage
	}{
		{
			name: "openai", protocol: ProtocolOpenAIChat, want: Usage{InputTokens: 44, OutputTokens: 9},
			frames: []string{
				`{"choices":[],"usage":{"prompt_tokens":44,"completion_tokens":9,"total_tokens":53}}`,
				`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
			},
		},
		{
			name: "anthropic", protocol: ProtocolAnthropicMessages, want: Usage{InputTokens: 55, OutputTokens: 11},
			frames: []string{
				`{"type":"message_start","message":{"usage":{"input_tokens":55,"output_tokens":0}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
				`{"type":"message_delta","usage":{"output_tokens":11}}`,
				`{"type":"message_stop"}`,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.protocol == ProtocolOpenAIChat {
					var request map[string]any
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("decode request: %v", err)
					}
					if _, present := request["stream_options"]; present {
						t.Error("usage collection must not force unsupported stream_options")
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range test.frames {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
				}
			}))
			defer server.Close()
			var got Usage
			usageCalls := 0
			err := StreamChat(
				context.Background(), test.protocol, server.URL, "key", "model", nil, PromptContext{},
				nil, nil, nil, func(usage Usage) { got, usageCalls = usage, usageCalls+1 }, server.Client(),
			)
			if err != nil || usageCalls != 1 || got != test.want {
				t.Fatalf("StreamChat usage=%#v calls=%d err=%v", got, usageCalls, err)
			}
		})
	}
}
