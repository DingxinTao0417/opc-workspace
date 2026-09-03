package modelclient

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptSizeMatchesEncodedRequestAndEnforcesLimit(t *testing.T) {
	history := []ChatMessage{{Role: "user", Content: "你好"}}
	size, err := PromptSize(ProtocolOpenAIChat, "gpt-test", history, []string{"偏好简洁回答"})
	if err != nil {
		t.Fatalf("PromptSize: %v", err)
	}
	body, _, _, err := buildChatRequest(ProtocolOpenAIChat, "https://api.example.com/v1", "sk-test", "gpt-test", history, []string{"偏好简洁回答"})
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	if size != len(body) {
		t.Fatalf("prompt size = %d, encoded body = %d", size, len(body))
	}

	overflow := []ChatMessage{{Role: "user", Content: strings.Repeat("x", MaxPromptBytes)}}
	_, _, _, err = buildChatRequest(ProtocolOpenAIChat, "https://api.example.com/v1", "sk-test", "gpt-test", overflow, nil)
	if !errors.Is(err, ErrPromptTooLarge) {
		t.Fatalf("oversized prompt error = %v, want ErrPromptTooLarge", err)
	}
}
