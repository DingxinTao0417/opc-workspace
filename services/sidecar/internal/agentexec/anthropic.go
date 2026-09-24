package agentexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// The bounded executor does not enable streaming, tools, thinking or stop
// sequences. It makes exactly one request and requires a normal end_turn.
type anthropicRequest struct {
	Model     string        `json:"model"`
	System    string        `json:"system"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens"`
}

func completeAnthropicResponse(payload []byte) (string, error) {
	invalid := &modelResponseFailure{code: ErrorCodeModelResponseInvalid}
	if !utf8.Valid(payload) {
		return "", invalid
	}
	response, err := modelResponseObject(payload)
	if err != nil {
		return "", invalid
	}
	if raw := response["error"]; len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", errors.New(ErrorCodeModelFailed)
	}
	if !modelStringEquals(response["type"], "message") || !modelStringEquals(response["role"], "assistant") {
		return "", invalid
	}
	var reason string
	if json.Unmarshal(response["stop_reason"], &reason) != nil {
		return "", invalid
	}
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return "", &modelResponseFailure{code: ErrorCodeModelTruncated}
	case "refusal":
		return "", &modelResponseFailure{code: ErrorCodeModelFiltered}
	case "end_turn":
	default:
		return "", invalid
	}
	// No stop sequence was requested. A sequence-bearing response cannot be
	// silently treated as a normal complete deliverable.
	if raw := response["stop_sequence"]; len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", invalid
	}
	var blocks []json.RawMessage
	if json.Unmarshal(response["content"], &blocks) != nil || len(blocks) == 0 {
		return "", invalid
	}
	var builder strings.Builder
	for _, raw := range blocks {
		block, err := modelResponseObject(raw)
		if err != nil || !modelStringEquals(block["type"], "text") {
			return "", invalid
		}
		content := block["text"]
		if len(content) == 0 || bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
			return "", invalid
		}
		var text string
		if json.Unmarshal(content, &text) != nil {
			return "", invalid
		}
		// Blocks are ordered fragments, not independent deliverables. Do not
		// invent separators or discard any non-text block to salvage output.
		builder.WriteString(text)
	}
	return builder.String(), nil
}

func modelStringEquals(raw json.RawMessage, want string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == want
}
