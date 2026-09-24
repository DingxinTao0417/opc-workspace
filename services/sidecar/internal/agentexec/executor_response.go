package agentexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// modelResponseFailure contains code-owned text only. No provider message,
// partial output, endpoint or credential is retained in the error.
type modelResponseFailure struct{ code string }

func (failure *modelResponseFailure) Error() string { return failure.code }

// completeModelResponse accepts exactly one explicitly completed text choice.
// Missing completion metadata cannot prove that an otherwise usable body is
// complete. Tool/refusal payloads never become deliverables or tool requests.
func completeModelResponse(payload []byte) (string, error) {
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
	var choices []json.RawMessage
	if json.Unmarshal(response["choices"], &choices) != nil || len(choices) != 1 {
		return "", invalid
	}
	choice, err := modelResponseObject(choices[0])
	if err != nil {
		return "", invalid
	}
	var reason string
	if json.Unmarshal(choice["finish_reason"], &reason) != nil {
		return "", invalid
	}
	switch reason {
	case "length":
		return "", &modelResponseFailure{code: ErrorCodeModelTruncated}
	case "content_filter":
		return "", &modelResponseFailure{code: ErrorCodeModelFiltered}
	case "stop":
	default:
		return "", invalid
	}
	message, err := modelResponseObject(choice["message"])
	if err != nil {
		return "", invalid
	}
	if refusal := message["refusal"]; len(refusal) != 0 && !bytes.Equal(bytes.TrimSpace(refusal), []byte("null")) {
		var text string
		if json.Unmarshal(refusal, &text) != nil {
			return "", invalid
		}
		if text != "" {
			return "", &modelResponseFailure{code: ErrorCodeModelFiltered}
		}
	}
	if call := message["function_call"]; len(call) != 0 && !bytes.Equal(bytes.TrimSpace(call), []byte("null")) {
		return "", invalid
	}
	if calls := message["tool_calls"]; len(calls) != 0 {
		var values []json.RawMessage
		if json.Unmarshal(calls, &values) != nil || len(values) != 0 {
			return "", invalid
		}
	}
	content := message["content"]
	if len(content) == 0 || bytes.Equal(bytes.TrimSpace(content), []byte("null")) {
		return "", invalid
	}
	var text string
	if json.Unmarshal(content, &text) != nil {
		return "", invalid
	}
	return text, nil
}

// Preserve unrelated provider metadata, but do not let duplicate identity or
// termination fields be silently resolved by Go's last-key-wins decoder.
func modelResponseObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("invalid model response object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid model response key")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate model response key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing model response data")
	}
	return fields, nil
}
