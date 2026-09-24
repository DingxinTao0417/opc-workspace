// Package agentexec implements the opc-agent-pipe-v1 protocol shared by the
// Sidecar runner and the builtin executor subprocess. Frames are 4-byte
// big-endian length prefixed UTF-8 JSON; unknown fields are rejected so both
// sides fail closed on protocol drift.
package agentexec

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	ProtocolVersion = "opc-agent-pipe-v1"
	// MaxFrameBytes bounds a single protocol frame. The result frame carries
	// the full text payload, so it must exceed the 64 KiB result budget.
	MaxFrameBytes = 1 << 20
	// MaxResultBytes bounds the inline text result of a v0.2-B run.
	MaxResultBytes = 64 << 10
	// MaxFileInputs bounds the number of controlled UTF-8 references in one
	// execution. File bytes cross the anonymous pipe, never filesystem paths.
	MaxFileInputs = 4
	// MaxFileInputBytes bounds each controlled input independently.
	MaxFileInputBytes = 64 << 10
	// MaxFileInputsTotalBytes bounds all controlled inputs in one frame.
	MaxFileInputsTotalBytes = 128 << 10
	// MaxFileNameBytes matches the controlled-file stores' public name limit.
	MaxFileNameBytes = 255
	// MaxFileOutputs keeps a multiple-file deliverable bounded to the same
	// human-reviewable scale as controlled inputs. A multi-file contract must
	// contain at least two files; one file continues to use ResultTypeFile.
	MaxFileOutputs = 4

	ResultTypeText  = "text"
	ResultTypeFile  = "file"
	ResultTypeFiles = "files"

	ModelProtocolOpenAIChat        = "openai_chat"
	ModelProtocolAnthropicMessages = "anthropic_messages"
	AnthropicMaxOutputTokens       = 8192

	CapabilityReadTaskSnapshot     = "read_task_snapshot"
	CapabilityReadControlledFiles  = "read_controlled_files"
	CapabilityReadProjectTaskFiles = "read_project_task_files"
	CapabilityWriteTextResult      = "write_text_result"
	CapabilityWriteFileResult      = "write_file_result"
	CapabilityWriteFilesResult     = "write_files_result"

	FileSourceTaskArtifact        = "task_artifact"
	FileSourceProjectAttachment   = "project_attachment"
	FileSourceProjectTaskArtifact = "project_task_artifact"
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
	// These fields are omitted by legacy OpenAI execution contracts. A new
	// protocol must be explicitly frozen by the Sidecar, never inferred from
	// the endpoint URL or provider response.
	ModelProtocol   string `json:"model_protocol,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	// ModelAPIKey carries a run-scoped provider credential for online models.
	// It exists only in the pipe and both processes' memory; it is never
	// persisted, logged, or included in snapshots (ADR-027).
	ModelAPIKey    string `json:"model_api_key,omitempty"`
	DeadlineMS     int64  `json:"deadline_ms"`
	MaxResultBytes int    `json:"max_result_bytes"`
	Instruction    string `json:"instruction"`
	// Files contains a frozen, bounded copy of explicitly selected controlled
	// UTF-8 files. The executor never receives a local path or storage route.
	Files []FrozenFileInput `json:"files,omitempty"`
	// OutputContract is nil for legacy v1 text-only frames. When present it is
	// frozen by the Sidecar; the executor may only return its matching type.
	OutputContract *OutputContract `json:"output_contract,omitempty"`
	Rework         *ReworkContext  `json:"rework,omitempty"`
}

// FrozenFileInput is one server-authorized controlled text file. SizeBytes and
// SHA256 cover the UTF-8 bytes in Content, not a decoded or normalized form.
type FrozenFileInput struct {
	ID         string                 `json:"id"`
	SourceKind string                 `json:"source_kind"`
	Name       string                 `json:"name"`
	MIME       string                 `json:"mime"`
	SizeBytes  int                    `json:"size_bytes"`
	SHA256     string                 `json:"sha256"`
	Content    string                 `json:"content"`
	SourceTask *ProjectTaskFileSource `json:"source_task,omitempty"`
}

// ProjectTaskFileSource binds a selected file to its human-accepted current
// submission in a different Task of the same Project. It grants no authority.
type ProjectTaskFileSource struct {
	ProjectID          string `json:"project_id"`
	TaskID             string `json:"task_id"`
	TaskTitle          string `json:"task_title"`
	TaskVersion        int64  `json:"task_version"`
	SubmissionID       string `json:"submission_id"`
	SubmissionSequence int    `json:"submission_sequence"`
}

// OutputContract declares the sole deliverable kind. For file results, names
// and MIME values are authoritative server facts and deliberately absent from
// Result. The executor receives ordered file contracts but never receives a
// write path; Result.Files maps to the frozen list by position only.
type OutputContract struct {
	Type  string               `json:"type"`
	Name  string               `json:"name,omitempty"`
	MIME  string               `json:"mime,omitempty"`
	Files []OutputFileContract `json:"files,omitempty"`
}

type OutputFileContract struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
}

type TaskSnapshot struct {
	TaskID             string  `json:"task_id"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	CompletionCriteria string  `json:"completion_criteria"`
	Status             string  `json:"status"`
	Kind               string  `json:"kind"`
	ReviewPolicy       string  `json:"review_policy"`
	Priority           string  `json:"priority"`
	ProjectID          *string `json:"project_id"`
	ParentTaskID       *string `json:"parent_task_id"`
	DueDate            *string `json:"due_date"`
	PlannedDate        *string `json:"planned_date"`
	EstimatedMinutes   *int    `json:"estimated_minutes"`
	ActualMinutes      int     `json:"actual_minutes"`
	ManualOrder        *int    `json:"manual_order"`
	Version            int64   `json:"version"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

// ManifestFrame is the single response frame read from the executor's stdout.
type ManifestFrame struct {
	ProtocolVersion string `json:"protocol_version"`
	RunID           string `json:"run_id"`
	Nonce           string `json:"nonce"`
	Result          Result `json:"result"`
}

type Result struct {
	Type  string   `json:"type"`
	Text  string   `json:"text"`
	Files []string `json:"files,omitempty"`
}

// EffectiveOutputContract preserves pipe-v1 compatibility: an omitted output
// contract means one inline text result.
func EffectiveOutputContract(input InputFrame) OutputContract {
	if input.OutputContract == nil {
		return OutputContract{Type: ResultTypeText}
	}
	return *input.OutputContract
}

// EffectiveMaxResultBytes applies the executor's legacy default consistently
// in both the executor and runner.
func EffectiveMaxResultBytes(input InputFrame) int {
	if input.MaxResultBytes <= 0 || input.MaxResultBytes > MaxResultBytes {
		return MaxResultBytes
	}
	return input.MaxResultBytes
}

// ValidateInputFrame validates the parts of the pipe contract that are shared
// by the Sidecar runner and builtin executor. It never includes file content in
// returned errors.
func ValidateInputFrame(input InputFrame) error {
	if err := validateModelProtocol(input.ModelProtocol, input.MaxOutputTokens); err != nil {
		return err
	}
	if input.ProtocolVersion != ProtocolVersion || input.RunID == "" || input.Nonce == "" {
		return errors.New("executor input frame identity missing or invalid")
	}
	if len(input.Capabilities) == 0 || input.Instruction == "" || input.Input.TaskID == "" {
		return errors.New("executor input frame missing capabilities or instruction")
	}
	if len(input.Files) > MaxFileInputs {
		return fmt.Errorf("executor input has more than %d controlled files", MaxFileInputs)
	}
	totalBytes := 0
	hasProjectTaskFiles := false
	seenIDs := make(map[string]struct{}, len(input.Files))
	for index, file := range input.Files {
		if err := validateFrozenFileInput(file); err != nil {
			return fmt.Errorf("controlled file %d is invalid: %w", index+1, err)
		}
		if file.SourceKind == FileSourceProjectTaskArtifact {
			hasProjectTaskFiles = true
			if input.Input.ProjectID == nil || file.SourceTask.ProjectID != *input.Input.ProjectID ||
				file.SourceTask.TaskID == input.Input.TaskID {
				return errors.New("project task file does not belong to a different Task in the target Project")
			}
		}
		if _, exists := seenIDs[file.ID]; exists {
			return fmt.Errorf("controlled file %d repeats an id", index+1)
		}
		seenIDs[file.ID] = struct{}{}
		totalBytes += file.SizeBytes
		if totalBytes > MaxFileInputsTotalBytes {
			return fmt.Errorf("controlled files exceed the %d-byte total limit", MaxFileInputsTotalBytes)
		}
	}
	if len(input.Files) > 0 && !hasCapability(input.Capabilities, CapabilityReadControlledFiles) {
		return errors.New("executor input frame cannot read controlled files")
	}
	if hasProjectTaskFiles != hasCapability(input.Capabilities, CapabilityReadProjectTaskFiles) {
		return errors.New("project task files require their exact independent capability")
	}
	if input.Rework != nil {
		if !hasCapability(input.Capabilities, CapabilityReadReworkContext) ||
			input.Input.Status != "in_progress" || input.Input.ReviewPolicy != "manual" {
			return errors.New("executor input frame cannot read rework context")
		}
		if err := ValidateReworkContext(*input.Rework); err != nil {
			return err
		}
	} else if hasCapability(input.Capabilities, CapabilityReadReworkContext) {
		return errors.New("executor rework capability requires frozen context")
	}
	output := EffectiveOutputContract(input)
	if err := ValidateOutputContract(output); err != nil {
		return fmt.Errorf("output contract is invalid: %w", err)
	}
	requiredOutputCapability := CapabilityWriteFileResult
	switch output.Type {
	case ResultTypeText:
		requiredOutputCapability = CapabilityWriteTextResult
	case ResultTypeFiles:
		requiredOutputCapability = CapabilityWriteFilesResult
	}
	// Legacy pipe-v1 text frames predate canonical capability names; keep them
	// compatible. Every explicit H5-C output contract is capability-bound.
	if (input.OutputContract != nil || output.Type == ResultTypeFile) && !hasCapability(input.Capabilities, requiredOutputCapability) {
		return errors.New("executor input frame cannot write the frozen output type")
	}
	return nil
}

func validateModelProtocol(protocol string, maxOutputTokens int) error {
	switch protocol {
	case "", ModelProtocolOpenAIChat:
		if maxOutputTokens == 0 {
			return nil
		}
	case ModelProtocolAnthropicMessages:
		if maxOutputTokens == AnthropicMaxOutputTokens {
			return nil
		}
	}
	return errors.New("executor model protocol or token budget is invalid")
}

func hasCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

func validateFrozenFileInput(file FrozenFileInput) error {
	parsedID, err := uuid.Parse(file.ID)
	if err != nil || parsedID.String() != file.ID {
		return errors.New("id must be a canonical UUID")
	}
	if file.SourceKind != FileSourceTaskArtifact && file.SourceKind != FileSourceProjectAttachment && file.SourceKind != FileSourceProjectTaskArtifact {
		return errors.New("source kind is not allowed")
	}
	if file.SourceKind == FileSourceProjectTaskArtifact {
		if file.SourceTask == nil {
			return errors.New("project task file requires frozen source evidence")
		}
		if err := ValidateProjectTaskFileSource(*file.SourceTask); err != nil {
			return err
		}
	} else if file.SourceTask != nil {
		return errors.New("legacy file sources cannot declare project task evidence")
	}
	if err := ValidateTextFileNameAndMIME(file.Name, file.MIME); err != nil {
		return err
	}
	if file.SizeBytes <= 0 || file.SizeBytes > MaxFileInputBytes || file.SizeBytes != len(file.Content) {
		return errors.New("size does not match the bounded content")
	}
	if !utf8.ValidString(file.Content) || strings.ContainsRune(file.Content, '\x00') {
		return errors.New("content must be UTF-8 without NUL bytes")
	}
	if !validSHA256(file.SHA256) {
		return errors.New("sha256 must be lowercase hexadecimal")
	}
	digest := sha256.Sum256([]byte(file.Content))
	if hex.EncodeToString(digest[:]) != file.SHA256 {
		return errors.New("sha256 does not match content")
	}
	return nil
}

// ValidateOutputContract checks a server-frozen output declaration. Callers
// must still use these values, rather than model-returned metadata, when they
// persist a file result.
func ValidateOutputContract(contract OutputContract) error {
	switch contract.Type {
	case ResultTypeText:
		if contract.Name != "" || contract.MIME != "" || len(contract.Files) != 0 {
			return errors.New("text output cannot declare file metadata")
		}
		return nil
	case ResultTypeFile:
		if len(contract.Files) != 0 {
			return errors.New("single file output cannot declare multiple files")
		}
		return ValidateTextFileNameAndMIME(contract.Name, contract.MIME)
	case ResultTypeFiles:
		if contract.Name != "" || contract.MIME != "" {
			return errors.New("multiple file output cannot declare single-file metadata")
		}
		if len(contract.Files) < 2 || len(contract.Files) > MaxFileOutputs {
			return fmt.Errorf("multiple file output must declare 2 to %d files", MaxFileOutputs)
		}
		seenNames := make(map[string]struct{}, len(contract.Files))
		for index, file := range contract.Files {
			if err := ValidateTextFileNameAndMIME(file.Name, file.MIME); err != nil {
				return fmt.Errorf("output file %d is invalid: %w", index+1, err)
			}
			key := strings.ToLower(file.Name)
			if _, duplicate := seenNames[key]; duplicate {
				return fmt.Errorf("output file %d repeats a name", index+1)
			}
			seenNames[key] = struct{}{}
		}
		return nil
	default:
		return errors.New("output type is not allowed")
	}
}

var (
	ErrResultInvalid  = errors.New("agent result is invalid")
	ErrResultTooLarge = errors.New("agent result exceeds the byte budget")
)

// ResultPayload validates a manifest result against its frozen contract and
// returns the exact durable payload. Multi-file payloads are canonical JSON
// arrays of UTF-8 bodies. The array carries no model-chosen names, MIME types
// or paths: position is bound to OutputContract.Files by the Sidecar.
func ResultPayload(result Result, contract OutputContract, maxBytes int) (string, error) {
	if err := ValidateOutputContract(contract); err != nil {
		return "", fmt.Errorf("%w: output contract", ErrResultInvalid)
	}
	if result.Type != contract.Type {
		return "", fmt.Errorf("%w: result type", ErrResultInvalid)
	}
	if maxBytes <= 0 || maxBytes > MaxResultBytes {
		maxBytes = MaxResultBytes
	}
	validateText := func(value string) error {
		if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return ErrResultInvalid
		}
		return nil
	}
	switch contract.Type {
	case ResultTypeText, ResultTypeFile:
		if len(result.Files) != 0 {
			return "", fmt.Errorf("%w: unexpected file bodies", ErrResultInvalid)
		}
		if err := validateText(result.Text); err != nil {
			return "", err
		}
		if len(result.Text) > maxBytes {
			return "", ErrResultTooLarge
		}
		return result.Text, nil
	case ResultTypeFiles:
		if result.Text != "" || len(result.Files) != len(contract.Files) {
			return "", fmt.Errorf("%w: file body count", ErrResultInvalid)
		}
		for _, value := range result.Files {
			if err := validateText(value); err != nil {
				return "", err
			}
		}
		encoded, err := json.Marshal(result.Files)
		if err != nil {
			return "", fmt.Errorf("%w: encode file bodies", ErrResultInvalid)
		}
		if len(encoded) > maxBytes {
			return "", ErrResultTooLarge
		}
		return string(encoded), nil
	default:
		return "", ErrResultInvalid
	}
}

// DecodeFilesPayload restores the canonical durable multi-file payload. It is
// used only after the stored OutputContract has established the ordered names
// and MIME values; malformed or noncanonical payloads fail closed.
func DecodeFilesPayload(payload string, contract OutputContract, maxBytes int) ([]string, error) {
	if contract.Type != ResultTypeFiles {
		return nil, fmt.Errorf("%w: output type", ErrResultInvalid)
	}
	if len(payload) == 0 {
		return nil, ErrResultInvalid
	}
	var files []string
	decoder := json.NewDecoder(strings.NewReader(payload))
	if err := decoder.Decode(&files); err != nil {
		return nil, fmt.Errorf("%w: decode file bodies", ErrResultInvalid)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing file bodies", ErrResultInvalid)
	}
	canonical, err := ResultPayload(Result{Type: ResultTypeFiles, Files: files}, contract, maxBytes)
	if err != nil || canonical != payload {
		if errors.Is(err, ErrResultTooLarge) {
			return nil, err
		}
		return nil, ErrResultInvalid
	}
	return files, nil
}

// ValidateTextFileNameAndMIME applies the shared safe-basename and exact
// UTF-8 text MIME/extension allowlist without reading file content.
func ValidateTextFileNameAndMIME(name, mimeType string) error {
	if name == "" || len(name) > MaxFileNameBytes || !utf8.ValidString(name) || strings.TrimSpace(name) != name ||
		name == "." || name == ".." || strings.ContainsAny(name, `/\\<>:"|?*`) || strings.ContainsRune(name, '\x00') {
		return errors.New("name must be a safe UTF-8 basename")
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return errors.New("name must not contain control characters")
		}
	}
	extension := strings.ToLower(filepath.Ext(name))
	allowed, exists := allowedTextFileMIMEs[mimeType]
	if !exists {
		return errors.New("mime is not an allowed UTF-8 text type")
	}
	if _, exists := allowed[extension]; !exists {
		return errors.New("name extension does not match mime")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

var allowedTextFileMIMEs = map[string]map[string]struct{}{
	"text/plain":             {".txt": {}, ".log": {}},
	"text/markdown":          {".md": {}, ".markdown": {}},
	"text/csv":               {".csv": {}},
	"text/html":              {".html": {}, ".htm": {}},
	"text/css":               {".css": {}},
	"text/javascript":        {".js": {}, ".mjs": {}, ".cjs": {}, ".jsx": {}},
	"application/javascript": {".js": {}, ".mjs": {}, ".cjs": {}, ".jsx": {}},
	"text/typescript":        {".ts": {}, ".tsx": {}},
	"application/json":       {".json": {}},
	"application/xml":        {".xml": {}},
	"text/xml":               {".xml": {}},
	"application/yaml":       {".yaml": {}, ".yml": {}},
	"text/yaml":              {".yaml": {}, ".yml": {}},
	"text/x-go":              {".go": {}},
	"text/x-python":          {".py": {}},
	"text/x-shellscript":     {".sh": {}},
	"text/x-sql":             {".sql": {}},
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
	if written, err := writer.Write(header); err != nil {
		return err
	} else if written != len(header) {
		return io.ErrShortWrite
	}
	written, err := writer.Write(encoded)
	if err == nil && written != len(encoded) {
		return io.ErrShortWrite
	}
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
