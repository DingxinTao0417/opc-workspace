package modelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamRequiresProtocolCompletionAndRetainsPartial(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protocol  Protocol
		frames    string
		wantError bool
	}{
		{"openai_eof", ProtocolOpenAIChat, `{"choices":[{"delta":{"content":"partial"}}]}`, true},
		{"openai_truncated", ProtocolOpenAIChat, `{"choices":[{"delta":{"content":"partial"},"finish_reason":"length"}]}` + "\n[DONE]", true},
		{"openai_filtered", ProtocolOpenAIChat, `{"choices":[{"delta":{"content":"partial"},"finish_reason":"content_filter"}]}` + "\n[DONE]", true},
		{"openai_unknown_finish", ProtocolOpenAIChat, `{"choices":[{"delta":{"content":"partial"},"finish_reason":"mystery"}]}` + "\n[DONE]", true},
		{"openai_done", ProtocolOpenAIChat, `{"choices":[{"delta":{"content":"partial"},"finish_reason":"stop"}]}` + "\n[DONE]", false},
		{"anthropic_eof", ProtocolAnthropicMessages, `{"type":"content_block_delta","delta":{"text":"partial"}}`, true},
		{"anthropic_truncated", ProtocolAnthropicMessages, `{"type":"content_block_delta","delta":{"text":"partial"}}` + "\n" + `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}` + "\n" + `{"type":"message_stop"}`, true},
		{"anthropic_done", ProtocolAnthropicMessages, `{"type":"content_block_delta","delta":{"text":"partial"}}` + "\n" + `{"type":"message_stop"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, frame := range strings.Split(tc.frames, "\n") {
					fmt.Fprintf(w, "data: %s\n\n", frame)
				}
			}))
			defer server.Close()
			var partial string
			err := StreamChat(context.Background(), tc.protocol, server.URL, "", "test", nil, PromptContext{}, func(s string) { partial += s }, nil, nil, nil, server.Client())
			if (err != nil) != tc.wantError || (tc.wantError && !errors.Is(err, ErrStream)) {
				t.Fatalf("err=%v wantError=%v", err, tc.wantError)
			}
			if partial != "partial" {
				t.Fatalf("partial=%q", partial)
			}
		})
	}
}

func TestStreamReadsUsageAfterFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":41,\"completion_tokens\":7}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	var usage Usage
	calls := 0
	err := StreamChat(context.Background(), ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{}, nil, nil, nil, func(u Usage) { usage = u; calls++ }, server.Client())
	if err != nil || calls != 1 || usage.InputTokens != 41 || usage.OutputTokens != 7 {
		t.Fatalf("usage=%+v calls=%d err=%v", usage, calls, err)
	}
}

func TestPartialUsageNeverPromotesInitialOrStaleCounts(t *testing.T) {
	var anthropic providerUsageAccumulator
	anthropic.apply(ProtocolAnthropicMessages, `{"type":"message_start","message":{"usage":{"input_tokens":9,"output_tokens":0}}}`)
	if _, ok := anthropic.complete(); ok {
		t.Fatal("initial Anthropic output count is not final usage")
	}
	var openAI providerUsageAccumulator
	openAI.apply(ProtocolOpenAIChat, `{"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
	openAI.apply(ProtocolOpenAIChat, `{"usage":{"prompt_tokens":6,"completion_tokens":-1}}`)
	if _, ok := openAI.complete(); ok {
		t.Fatal("invalid latest usage retained a stale field")
	}
}
