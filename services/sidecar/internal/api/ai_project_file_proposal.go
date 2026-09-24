package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A transient review artifact, not a filesystem command or a business proposal.
// Native handles and absolute roots never enter this bridge or the model.
type aiProjectFileProposal struct {
	ID         string `json:"proposal_id"`
	Path       string `json:"path"`
	BaseSHA256 string `json:"base_sha256"`
	Content    string `json:"content"`
}

type aiProjectFileProposalEmitter func(aiProjectFileProposal) bool
type aiProjectFileProposalEmitterKey struct{}

type aiProjectFileProposalTool struct {
	mu       sync.Mutex
	files    []aiProjectFileInput
	expires  time.Time
	now      func() time.Time
	pending  *aiProjectFileProposal
	receipt  string
	accepted bool
}

func newAIProjectFileProposalTool(files *aiProjectFileContext, now func() time.Time) *aiProjectFileProposalTool {
	expires, _ := time.Parse(time.RFC3339Nano, files.ExpiresAt)
	return &aiProjectFileProposalTool{files: append([]aiProjectFileInput(nil), files.Files...), expires: expires, now: now}
}

func (*aiProjectFileProposalTool) Name() string { return "workspace_propose_file_edit" }
func (*aiProjectFileProposalTool) Summary() string {
	return "Only when asked to edit an explicitly attached file: propose its complete replacement against its exact SHA256. One proposal per generation, 32 KiB UTF-8, no truncation. Review only; no writes, execution or claim of applied changes."
}
func (*aiProjectFileProposalTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["path","base_sha256","content"],"properties":{"path":{"type":"string","maxLength":4096},"base_sha256":{"type":"string","pattern":"^[0-9a-f]{64}$"},"content":{"type":"string","maxLength":32768}}}`)
}

func (t *aiProjectFileProposalTool) Execute(ctx context.Context, arguments json.RawMessage) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !t.expires.After(t.now()) {
		return "", errors.New("file baseline expired; ask the user to select and disclose it again")
	}
	var input struct {
		Path       *string `json:"path"`
		BaseSHA256 *string `json:"base_sha256"`
		Content    *string `json:"content"`
	}
	if !utf8.Valid(arguments) || len(arguments) > 256<<10 || decodeStrictToolArguments(arguments, &input) != nil || input.Path == nil || input.BaseSHA256 == nil || input.Content == nil {
		return "", errors.New("file proposal requires only path, base_sha256 and complete UTF-8 content")
	}
	if len(*input.Content) > aiProjectFileBytes || strings.ContainsRune(*input.Content, 0) || !utf8.ValidString(*input.Content) {
		return "", errors.New("replacement must be a complete UTF-8 file at most 32 KiB without NUL; do not truncate")
	}
	matched := false
	for _, file := range t.files {
		if file.Path == *input.Path && file.SHA256 == *input.BaseSHA256 {
			if file.Content == *input.Content {
				return "", errors.New("file proposal has no byte changes")
			}
			matched = true
		}
	}
	if !matched {
		return "", errors.New("file proposal does not match an explicitly disclosed path and baseline digest")
	}
	if t.pending != nil {
		if t.pending.Path == *input.Path && t.pending.BaseSHA256 == *input.BaseSHA256 && t.pending.Content == *input.Content {
			return t.receipt, nil
		}
		return "", errors.New("one file proposal is already prepared; do not replace it or claim it was written")
	}
	t.pending = &aiProjectFileProposal{ID: uuid.NewString(), Path: *input.Path, BaseSHA256: *input.BaseSHA256, Content: *input.Content}
	// Never echo candidate bytes into the tool result, traces or persisted steps.
	encoded, _ := json.Marshal(map[string]any{"proposal_id": t.pending.ID, "review_only": true, "written": false})
	t.receipt = string(encoded)
	return t.receipt, nil
}

func (t *aiProjectFileProposalTool) AcceptResult(ctx context.Context, output string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.pending == nil || output != t.receipt || !t.expires.After(t.now()) {
		return errors.New("file proposal is no longer available")
	}
	if t.accepted {
		return nil
	}
	emit, _ := ctx.Value(aiProjectFileProposalEmitterKey{}).(aiProjectFileProposalEmitter)
	if emit == nil || !emit(*t.pending) {
		return errors.New("file review bridge unavailable; no file was written")
	}
	t.accepted = true
	return nil
}
