package agentexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Only the newly introduced protocol-selection fields gain stricter raw
// parsing. Keep legacy field decoding unchanged, but never let duplicate or
// case-aliased new fields silently select a different model wire protocol.
func (input *InputFrame) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("executor input must be an object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("executor input key is invalid")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		canonical := strings.ToLower(key)
		if canonical != "model_protocol" && canonical != "max_output_tokens" {
			continue
		}
		if key != canonical || seen[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("executor model protocol fields are ambiguous")
		}
		seen[key] = true
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("executor input has trailing JSON")
	}
	type plainInput InputFrame
	var decoded plainInput
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*input = InputFrame(decoded)
	return nil
}
