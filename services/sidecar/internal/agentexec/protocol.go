// Package agentexec implements the opc-agent-pipe-v1 protocol shared by the
// Sidecar runner and the builtin executor subprocess. Frames are 4-byte
// big-endian length prefixed UTF-8 JSON; unknown fields are rejected so both
// sides fail closed on protocol drift.
package agentexec

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolVersion = "opc-agent-pipe-v1"
	// MaxFrameBytes bounds a single protocol frame. The result frame carries
	// the full text payload, so it must exceed the 64 KiB result budget.
	MaxFrameBytes = 1 << 20
	// MaxResultBytes bounds the inline text result of a v0.2-B run.
	MaxResultBytes = 64 << 10
)

var (
	// ErrFrameTooLarge is a stable protocol failure mapped to a run error code.
	ErrFrameTooLarge = errors.New("agent pipe frame exceeds the size limit")
	// ErrShortFrame reports a truncated or malformed frame stream.
	ErrShortFrame = errors.New("agent pipe frame stream ended early")
)

// InputFrame is the single request frame written to the executor's stdin.
type InputFrame struct {
	ProtocolVersion string       `json:"protocol_version"`
	RunID           string       `json:"run_id"`
	Nonce           string       `json:"nonce"`
	Capabilities    []string     `json:"capabilities"`
	Input           TaskSnapshot `json:"input"`
	ModelEndpoint   string       `json:"model_endpoint"`
	Model           string       `json:"model"`
	// ModelAPIKey carries a run-scoped provider credential for online models.
	// It exists only in the pipe and both processes' memory; it is never
	// persisted, logged, or included in snapshots (ADR-027).
	ModelAPIKey    string `json:"model_api_key,omitempty"`
	DeadlineMS     int64  `json:"deadline_ms"`
	MaxResultBytes int    `json:"max_result_bytes"`
	Instruction    string `json:"instruction"`
}

type TaskSnapshot struct {
	TaskID             string `json:"task_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	CompletionCriteria string `json:"completion_criteria,omitempty"`
	Status             string `json:"status"`
	Kind               string `json:"kind"`
}

// ManifestFrame is the single response frame read from the executor's stdout.
type ManifestFrame struct {
	ProtocolVersion string `json:"protocol_version"`
	RunID           string `json:"run_id"`
	Nonce           string `json:"nonce"`
	Result          Result `json:"result"`
}

type Result struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewNonce returns a 128-bit random hex nonce for one pipe session.
func NewNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// WriteFrame encodes one length-prefixed JSON frame.
func WriteFrame(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(encoded)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err = writer.Write(encoded)
	return err
}

// ReadFrame decodes exactly one length-prefixed JSON frame into a strict
// struct; trailing data inside the frame and unknown fields are rejected.
func ReadFrame(reader *bufio.Reader, target any) error {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return fmt.Errorf("%w: %v", ErrShortFrame, err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return fmt.Errorf("%w: %v", ErrShortFrame, err)
	}
	decoder := json.NewDecoder(newExactReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("agent pipe frame has trailing JSON content")
	}
	return nil
}

type exactReader struct {
	data   []byte
	cursor int
}

func newExactReader(data []byte) *exactReader { return &exactReader{data: data} }

func (r *exactReader) Read(target []byte) (int, error) {
	if r.cursor >= len(r.data) {
		return 0, io.EOF
	}
	count := copy(target, r.data[r.cursor:])
	r.cursor += count
	return count, nil
}
