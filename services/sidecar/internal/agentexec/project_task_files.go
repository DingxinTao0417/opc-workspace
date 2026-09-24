package agentexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ValidateProjectTaskFileSource checks the typed, content-free proof. The
// Sidecar must also establish these facts from the live domain transaction.
func ValidateProjectTaskFileSource(source ProjectTaskFileSource) error {
	for _, value := range []string{source.ProjectID, source.TaskID, source.SubmissionID} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return errors.New("project task file source identity is invalid")
		}
	}
	length := utf8.RuneCountInString(source.TaskTitle)
	if source.TaskVersion < 1 || source.SubmissionSequence < 1 || length < 2 || length > 200 ||
		!utf8.ValidString(source.TaskTitle) || strings.TrimSpace(source.TaskTitle) != source.TaskTitle ||
		strings.ContainsRune(source.TaskTitle, '\x00') {
		return errors.New("project task file source metadata is invalid")
	}
	return nil
}

func (source *ProjectTaskFileSource) UnmarshalJSON(raw []byte) error {
	allowed := map[string]bool{"project_id": true, "task_id": true, "task_title": true,
		"task_version": true, "submission_id": true, "submission_sequence": true}
	fields, err := strictProjectTaskFileObject(raw, allowed)
	if err != nil || len(fields) != len(allowed) {
		return errors.New("project task file source must contain exact evidence fields")
	}
	type plainSource ProjectTaskFileSource
	var decoded plainSource
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return errors.New("project task file source has invalid field types")
	}
	if err := ValidateProjectTaskFileSource(ProjectTaskFileSource(decoded)); err != nil {
		return err
	}
	*source = ProjectTaskFileSource(decoded)
	return nil
}

// Legacy file frames retain their encoding and decoding. New cross-Task
// fields, however, cannot use duplicate or aliased JSON keys to erase proof.
func (file *FrozenFileInput) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("controlled file must be an object")
	}
	strict := false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("controlled file key is invalid")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if strings.EqualFold(key, "source_task") {
			strict = true
		}
		if strings.EqualFold(key, "source_kind") {
			var kind string
			if json.Unmarshal(value, &kind) == nil && kind == FileSourceProjectTaskArtifact {
				strict = true
			}
		}
	}
	if strict {
		allowed := map[string]bool{"id": true, "source_kind": true, "name": true, "mime": true,
			"size_bytes": true, "sha256": true, "content": true, "source_task": true}
		if _, err := strictProjectTaskFileObject(raw, allowed); err != nil {
			return err
		}
	}
	type plainFile FrozenFileInput
	var decoded plainFile
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("controlled file has trailing JSON")
	}
	*file = FrozenFileInput(decoded)
	return nil
}

func strictProjectTaskFileObject(raw []byte, allowed map[string]bool) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("project task file evidence is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("project task file evidence must be an object")
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || fields[key] != nil {
			return nil, errors.New("project task file evidence fields are invalid")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errors.New("project task file evidence cannot be null")
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("project task file evidence has trailing JSON")
	}
	return fields, nil
}
