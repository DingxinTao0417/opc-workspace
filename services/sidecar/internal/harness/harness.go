// Package harness is the AI assistant's model runtime: a bounded run loop
// around a streaming LLM client with a narrow, explicitly registered tool
// allowlist. Production currently registers only ADR-007's three memory tools;
// any business or knowledge capability still requires separate authorization.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

const (
	// DefaultMaxTurns bounds the call/execute iterations of one run.
	DefaultMaxTurns = 8
	// DefaultToolTimeout bounds a single tool execution.
	DefaultToolTimeout = 30 * time.Second
	// DefaultMaxResultBytes caps one tool result (truncated) and, equally,
	// the accumulated tool output fed back into one run's prompt.
	DefaultMaxResultBytes = 64 << 10
	// DefaultMaxToolCorrections bounds how many tool failures may be fed
	// back into one run for the model to self-correct (ADR-006).
	DefaultMaxToolCorrections = 3
	// DefaultMaxToolCalls bounds execution work and content-free timeline
	// growth inside one generation.
	DefaultMaxToolCalls = 32
)

// Self-check (ADR-006): reflection is the agent's own behavior, not a user
// switch. The code-owned system prompt instructs the model to assess its
// draft before finishing and append a [opc:selfcheck] block with its verdict;
// the harness reacts to that verdict autonomously. The block itself is
// harness-internal: it is stripped before anything is persisted or shown.
const (
	selfCheckOpen  = "[opc:selfcheck]"
	selfCheckClose = "[/opc:selfcheck]"
)

// SelfCheckRevisionPrompt is code-owned. The model's own insufficiency note
// is injected as data into the user-role revision instruction.
const SelfCheckRevisionPrompt = `你刚才自评认为你的回答尚未满足用户请求。请针对该不足输出完整的修订后回答全文，不要输出解释或评论；若修订后已满足要求，照常在末尾输出自评块。`

// selfCheckVerdict is the parsed model self-assessment.
type selfCheckVerdict struct {
	sufficient bool
	note       string
}

// parseSelfCheck extracts the [opc:selfcheck] verdict block (the last one,
// normally trailing) and returns the text without it, so the internal block
// never leaks into emitted or persisted content. A missing open tag is an
// affirmative verdict over the untouched text; an unclosed or malformed block
// is stripped defensively and also treated as affirmative.
func parseSelfCheck(text string) (selfCheckVerdict, string) {
	idx := strings.LastIndex(text, selfCheckOpen)
	if idx < 0 {
		return selfCheckVerdict{sufficient: true}, text
	}
	remainder := text[idx:]
	end := strings.Index(remainder, selfCheckClose)
	verdict := selfCheckVerdict{sufficient: true}
	var stripped string
	if end < 0 {
		stripped = strings.TrimSpace(text[:idx])
		return verdict, stripped
	}
	payload := remainder[len(selfCheckOpen):end]
	stripped = strings.TrimSpace(text[:idx] + remainder[end+len(selfCheckClose):])
	var parsed struct {
		Sufficient *bool  `json:"sufficient"`
		Note       string `json:"note"`
	}
	if json.Unmarshal([]byte(payload), &parsed) == nil && parsed.Sufficient != nil {
		verdict = selfCheckVerdict{sufficient: *parsed.Sufficient, note: strings.TrimSpace(parsed.Note)}
	}
	return verdict, stripped
}

// selfCheckNoteMessage builds the user-role revision instruction carrying the
// model's own insufficiency note.
func selfCheckNoteMessage(note string) string {
	if note == "" {
		return SelfCheckRevisionPrompt
	}
	revisionNote := "\n你给出的不足说明：" + note
	return SelfCheckRevisionPrompt + revisionNote
}

// Sentinel errors surfaced by Run; callers map them to stable AI_* error codes.
var (
	ErrMaxTurns        = errors.New("harness: run exceeded its turn budget")
	ErrToolUnavailable = errors.New("harness: model requested a tool that is not registered")
	ErrToolBudget      = errors.New("harness: tool results exceeded their byte budget")
	ErrToolCorrections = errors.New("harness: tool failures exceeded the correction budget")
)

// LLMClient abstracts the streaming model adapter so the run loop is testable
// without a network. Implementations must honor ctx cancellation and stream
// incremental output through the callbacks.
type LLMClient interface {
	Stream(ctx context.Context, request Request, onDelta func(string), onReasoning func(string)) (Turn, error)
}

// Request is one provider-scoped generation request.
type Request struct {
	Protocol     string
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	History      []modelclient.ChatMessage
	// Memories are user-confirmed long-term preference notes injected into
	// the system prompt with a byte budget (ADR-006).
	Memories []string
	// Summary and Facts are the latest active, model-produced context snapshot
	// layers. They are operational context only and never become business facts.
	Summary          string
	Facts            []string
	BusinessContext  []string
	KnowledgeContext []string
	Tools            []modelclient.ToolDefinition
}

// Turn is one completed LLM round, including any fully aggregated provider
// tool calls.
type Turn struct {
	Text           string
	Reasoning      string
	ToolCalls      []ToolCall
	InputBytes     int
	OutputBytes    int
	InputTokens    int
	OutputTokens   int
	UsageAvailable bool
}

// ToolCall is one model-initiated tool invocation.
type ToolCall = modelclient.ToolCall

// Tool is a named capability the model may invoke during a run. Production
// registration is explicit and allowlisted; capabilities outside the accepted
// memory tools must be individually authorized before registration.
type Tool interface {
	Name() string
	Summary() string
	Execute(ctx context.Context, arguments json.RawMessage) (string, error)
}

type toolSchemaProvider interface {
	InputSchema() json.RawMessage
}

// Registry is the ordered, duplicate-free allowlist of tools for a run.
type Registry struct {
	tools []Tool
	names map[string]struct{}
}

// NewRegistry builds a registry, rejecting duplicate tool names.
func NewRegistry(tools ...Tool) (*Registry, error) {
	registry := &Registry{names: make(map[string]struct{}, len(tools))}
	for _, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("harness: nil tool in registry")
		}
		name := tool.Name()
		if name == "" {
			return nil, fmt.Errorf("harness: tool with empty name")
		}
		if _, duplicate := registry.names[name]; duplicate {
			return nil, fmt.Errorf("harness: duplicate tool name %q", name)
		}
		registry.names[name] = struct{}{}
		registry.tools = append(registry.tools, tool)
	}
	return registry, nil
}

// Get returns the named tool.
func (r *Registry) Get(name string) (Tool, bool) {
	for _, tool := range r.tools {
		if tool.Name() == name {
			return tool, true
		}
	}
	return nil, false
}

// Names lists the registered tool names in registration order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for _, tool := range r.tools {
		names = append(names, tool.Name())
	}
	return names
}

// Definitions exposes only the registered allowlist to the model. Tools may
// provide a strict input schema; legacy/internal tools receive an empty object
// schema and remain responsible for validating arguments in Execute.
func (r *Registry) Definitions() []modelclient.ToolDefinition {
	definitions := make([]modelclient.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		parameters := json.RawMessage(`{"type":"object","additionalProperties":true}`)
		if provider, ok := tool.(toolSchemaProvider); ok && len(provider.InputSchema()) > 0 {
			parameters = provider.InputSchema()
		}
		definitions = append(definitions, modelclient.ToolDefinition{
			Name: tool.Name(), Description: tool.Summary(), Parameters: parameters,
		})
	}
	return definitions
}

// Executor bounds single tool executions.
type Executor struct {
	Timeout        time.Duration
	MaxResultBytes int
}

func (e *Executor) withDefaults() Executor {
	fixed := *e
	if fixed.Timeout <= 0 {
		fixed.Timeout = DefaultToolTimeout
	}
	if fixed.MaxResultBytes <= 0 {
		fixed.MaxResultBytes = DefaultMaxResultBytes
	}
	return fixed
}

// Execute runs one tool under the executor's timeout, recovering panics as
// errors and truncating oversized results. A parent ctx cancellation wins over
// the per-tool timeout.
func (e *Executor) Execute(ctx context.Context, tool Tool, arguments json.RawMessage) (string, error) {
	fixed := e.withDefaults()
	runCtx, cancel := context.WithTimeout(ctx, fixed.Timeout)
	defer cancel()
	type outcome struct {
		result string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- outcome{err: fmt.Errorf("harness: tool %q panicked", tool.Name())}
			}
		}()
		result, err := tool.Execute(runCtx, arguments)
		done <- outcome{result: result, err: err}
	}()
	var completed outcome
	select {
	case completed = <-done:
	case <-runCtx.Done():
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("harness: tool %q timed out after %s", tool.Name(), fixed.Timeout)
	}
	if completed.err != nil {
		return "", completed.err
	}
	if len(completed.result) > fixed.MaxResultBytes {
		return completed.result[:fixed.MaxResultBytes], nil
	}
	return completed.result, nil
}

// Callbacks receive streamed output and content-free execution steps; every
// field is optional.
type Callbacks struct {
	OnDelta     func(string)
	OnReasoning func(string)
	OnStep      func(RunStep)
}

// RunStep never carries prompts, responses, reasoning, tool arguments or
// results, URLs, or credentials.
type RunStep struct {
	Kind           string
	Status         string
	TurnIndex      int
	ToolName       string
	StartedAt      time.Time
	CompletedAt    time.Time
	DurationMS     int64
	InputBytes     int
	OutputBytes    int
	InputTokens    int
	OutputTokens   int
	UsageAvailable bool
	ErrorCode      string
}

// Result summarizes a finished (or failed, cancelled) run with everything
// accumulated so far. Reflections counts the autonomous revision turns the
// agent triggered on its own insufficient-verdict (ADR-006).
type Result struct {
	Text        string
	Reasoning   string
	Turns       int
	ToolCalls   int
	Corrections int
	Reflections int
}

// Run executes the call/execute loop until the model produces a final reply
// or a budget is hit. The returned Result always carries the text and
// reasoning accumulated before the outcome, so callers can persist partial
// output for cancelled or failed runs.
func Run(ctx context.Context, client LLMClient, request Request, tools *Registry, executor *Executor, callbacks Callbacks) (Result, error) {
	if client == nil {
		return Result{}, errors.New("harness: nil LLM client")
	}
	maxTurns := DefaultMaxTurns

	var result Result
	history := make([]modelclient.ChatMessage, len(request.History))
	copy(history, request.History)
	totalToolResults := 0
	toolDefinitions := request.Tools
	if tools != nil {
		toolDefinitions = tools.Definitions()
	}
	request.Tools = toolDefinitions

	for result.Turns < maxTurns {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		turnIndex := result.Turns + 1
		startedAt := time.Now().UTC()
		turn, err := client.Stream(ctx, Request{
			Protocol: request.Protocol, BaseURL: request.BaseURL,
			APIKey: request.APIKey, Model: request.Model, SystemPrompt: request.SystemPrompt,
			History: history, Memories: request.Memories, Summary: request.Summary, Facts: request.Facts,
			BusinessContext:  request.BusinessContext,
			KnowledgeContext: request.KnowledgeContext,
			Tools:            toolDefinitions,
		}, callbacks.OnDelta, callbacks.OnReasoning)
		completedAt := time.Now().UTC()
		stepStatus, stepErrorCode := "succeeded", ""
		if err != nil {
			stepStatus, stepErrorCode = "failed", "MODEL_TURN_FAILED"
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				stepStatus, stepErrorCode = "cancelled", ""
			}
		}
		outputBytes := turn.OutputBytes
		if outputBytes == 0 {
			outputBytes = len(turn.Text) + len(turn.Reasoning)
			for _, call := range turn.ToolCalls {
				outputBytes += len(call.Arguments)
			}
		}
		emitRunStep(callbacks, RunStep{
			Kind: "model_turn", Status: stepStatus, TurnIndex: turnIndex,
			StartedAt: startedAt, CompletedAt: completedAt,
			DurationMS: completedAt.Sub(startedAt).Milliseconds(),
			InputBytes: turn.InputBytes, OutputBytes: outputBytes,
			InputTokens: turn.InputTokens, OutputTokens: turn.OutputTokens, UsageAvailable: turn.UsageAvailable,
			ErrorCode: stepErrorCode,
		})
		result.Turns++
		result.Text += turn.Text
		result.Reasoning += turn.Reasoning
		if err != nil {
			return result, err
		}
		if len(turn.ToolCalls) == 0 {
			return runSelfCheck(ctx, client, request, history, result, callbacks), nil
		}
		if tools == nil || len(tools.Names()) == 0 {
			return result, ErrToolUnavailable
		}
		historyCalls := make([]modelclient.ToolCall, len(turn.ToolCalls))
		copy(historyCalls, turn.ToolCalls)
		history = append(history, modelclient.ChatMessage{Role: "assistant", Content: turn.Text, ToolCalls: historyCalls})
		for _, call := range turn.ToolCalls {
			result.ToolCalls++
			if result.ToolCalls > DefaultMaxToolCalls {
				return result, ErrToolBudget
			}
			tool, ok := tools.Get(call.Name)
			if !ok {
				now := time.Now().UTC()
				emitRunStep(callbacks, RunStep{
					Kind: "tool_call", Status: "failed", ToolName: call.Name,
					StartedAt: now, CompletedAt: now, InputBytes: len(call.Arguments),
					ErrorCode: "TOOL_UNAVAILABLE",
				})
				return result, fmt.Errorf("%w: %q", ErrToolUnavailable, call.Name)
			}
			var exec Executor
			if executor != nil {
				exec = *executor
			}
			toolStartedAt := time.Now().UTC()
			output, err := exec.Execute(ctx, tool, call.Arguments)
			toolCompletedAt := time.Now().UTC()
			toolStatus, toolErrorCode := "succeeded", ""
			if err != nil {
				toolStatus, toolErrorCode = "failed", "TOOL_EXECUTION_FAILED"
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					toolStatus, toolErrorCode = "cancelled", ""
				}
			}
			emitRunStep(callbacks, RunStep{
				Kind: "tool_call", Status: toolStatus, ToolName: tool.Name(),
				StartedAt: toolStartedAt, CompletedAt: toolCompletedAt,
				DurationMS: toolCompletedAt.Sub(toolStartedAt).Milliseconds(),
				InputBytes: len(call.Arguments), OutputBytes: len(output), ErrorCode: toolErrorCode,
			})
			if err != nil {
				// Self-correction: feed the failure back so the model can
				// adjust and retry, bounded by the correction budget.
				result.Corrections++
				if result.Corrections > DefaultMaxToolCorrections {
					return result, ErrToolCorrections
				}
				history = append(history, modelclient.ChatMessage{
					Role: "tool", Content: fmt.Sprintf("%s: error: %v", tool.Name(), err),
					ToolCallID: call.ID, ToolName: tool.Name(),
				})
				continue
			}
			totalToolResults += len(output)
			if totalToolResults > DefaultMaxResultBytes {
				return result, ErrToolBudget
			}
			history = append(history, modelclient.ChatMessage{
				Role: "tool", Content: tool.Name() + ": " + output,
				ToolCallID: call.ID, ToolName: tool.Name(),
			})
		}
	}
	return result, ErrMaxTurns
}

// runSelfCheck closes the loop on the agent's own reflection (ADR-006): the
// draft carries the model's trailing [opc:selfcheck] verdict. An affirmative
// verdict emits the stripped draft as-is; an insufficiency verdict triggers
// one internal revision turn fed back with the model's own note, bounded to a
// single pass and the global turn budget. Any failure keeps the draft.
func runSelfCheck(ctx context.Context, client LLMClient, request Request, history []modelclient.ChatMessage, result Result, callbacks Callbacks) Result {
	checkStartedAt := time.Now().UTC()
	if ctx.Err() != nil {
		emitRunStep(callbacks, RunStep{
			Kind: "self_check", Status: "cancelled", TurnIndex: maxInt(result.Turns, 1),
			StartedAt: checkStartedAt, CompletedAt: checkStartedAt,
		})
		return result
	}
	verdict, stripped := parseSelfCheck(result.Text)
	if !verdict.sufficient {
		// The self-check block must never leak into the emitted text, even
		// when the revision path is not taken.
		result.Text = stripped
	}
	if verdict.sufficient || strings.TrimSpace(stripped) == "" {
		result.Text = stripped
		completedAt := time.Now().UTC()
		emitRunStep(callbacks, RunStep{
			Kind: "self_check", Status: "succeeded", TurnIndex: maxInt(result.Turns, 1),
			StartedAt: checkStartedAt, CompletedAt: completedAt,
			DurationMS: completedAt.Sub(checkStartedAt).Milliseconds(),
		})
		return result
	}
	if result.Turns >= DefaultMaxTurns {
		completedAt := time.Now().UTC()
		emitRunStep(callbacks, RunStep{
			Kind: "self_check", Status: "failed", TurnIndex: maxInt(result.Turns, 1),
			StartedAt: checkStartedAt, CompletedAt: completedAt,
			DurationMS: completedAt.Sub(checkStartedAt).Milliseconds(), ErrorCode: "TURN_BUDGET_EXHAUSTED",
		})
		return result
	}
	verifyHistory := make([]modelclient.ChatMessage, 0, len(history)+2)
	verifyHistory = append(verifyHistory, history...)
	verifyHistory = append(verifyHistory,
		modelclient.ChatMessage{Role: "assistant", Content: stripped},
		modelclient.ChatMessage{Role: "user", Content: selfCheckNoteMessage(verdict.note)},
	)
	revisionStartedAt := time.Now().UTC()
	revised, err := client.Stream(ctx, Request{
		Protocol: request.Protocol, BaseURL: request.BaseURL,
		APIKey: request.APIKey, Model: request.Model, SystemPrompt: request.SystemPrompt,
		History: verifyHistory, Memories: request.Memories, Summary: request.Summary, Facts: request.Facts,
		BusinessContext:  request.BusinessContext,
		KnowledgeContext: request.KnowledgeContext,
		Tools:            request.Tools,
	}, nil, nil)
	revisionCompletedAt := time.Now().UTC()
	revisionStatus, revisionErrorCode := "succeeded", ""
	if err != nil {
		revisionStatus, revisionErrorCode = "failed", "SELF_CHECK_REVISION_FAILED"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			revisionStatus, revisionErrorCode = "cancelled", ""
		}
	}
	revisionOutputBytes := revised.OutputBytes
	if revisionOutputBytes == 0 {
		revisionOutputBytes = len(revised.Text) + len(revised.Reasoning)
	}
	emitRunStep(callbacks, RunStep{
		Kind: "self_check", Status: revisionStatus, TurnIndex: result.Turns + 1,
		StartedAt: revisionStartedAt, CompletedAt: revisionCompletedAt,
		DurationMS: revisionCompletedAt.Sub(revisionStartedAt).Milliseconds(),
		InputBytes: revised.InputBytes, OutputBytes: revisionOutputBytes,
		InputTokens: revised.InputTokens, OutputTokens: revised.OutputTokens, UsageAvailable: revised.UsageAvailable,
		ErrorCode: revisionErrorCode,
	})
	result.Turns++
	if err != nil {
		return result
	}
	_, revisedStripped := parseSelfCheck(revised.Text)
	if revisedStripped == "" || revisedStripped == stripped {
		return result
	}
	result.Text = revisedStripped
	result.Reasoning = revised.Reasoning
	result.Reflections = 1
	return result
}

func emitRunStep(callbacks Callbacks, step RunStep) {
	if callbacks.OnStep != nil {
		callbacks.OnStep(step)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
