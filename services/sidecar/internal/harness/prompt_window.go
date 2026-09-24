package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// ContextWindowNotice is code-owned and included in the exact request budget.
// Removing history is not summarization, a new permission, or a successful run.
const ContextWindowNotice = `本次运行因提示长度限制移除了部分较早的完整对话回合。当前用户请求、最新工具调用组及结果、本轮操作回执、自检修订及独立上下文仍保留；较早历史可能不完整，不能猜测已移除的约束、事实或授权。必要信息缺失时明确说明并询问用户；业务事实须按本条授权重新核验，不能把历史缺失当作新的权限或成功证据。`

// Older read results can opt in to a bounded, code-owned evidence capsule. A
// capsule retains selected identity/status/version/pagination fields and a
// digest, but is neither the complete result nor proof that the facts are still
// current. Tools without a reviewed compactor retain the opaque marker. The
// most recent tool-call group is never compacted.
const ToolResultCompactionNotice = `本次运行因提示长度限制，把较早的只读工具结果正文替换为代码生成的有界证据胶囊或省略标记。证据胶囊只保留所列身份、状态、版本、分页边界和原结果摘要，不是完整结果，也不证明事实仍然最新；未列字段一律视为未知，需要时按本条权限重新查询。省略标记不保留任何业务事实。当前用户请求、最新工具调用组、操作建议/计划/权限回执与独立上下文仍保留。`
const compactedToolResult = `Earlier read-only result omitted for prompt budget. Re-run the tool to verify current facts; this is not a summary or success receipt.`
const maxCompactedEvidenceBytes = 4 << 10

// RequeryableResult is an optional, code-owned opt-in. Mutating or mixed-mode
// tools are not compacted, even if a particular invocation only read data.
type RequeryableResult interface {
	RequeryableResult() bool
}

// EvidenceCompactor is a reviewed, code-owned opt-in for preserving a small
// subset of an older read result. It receives the exact accepted arguments and
// result payload (without the harness' tool-name prefix). Implementations must
// not call external systems, mutate state, or treat the capsule as fresh data.
type EvidenceCompactor interface {
	CompactResult(arguments json.RawMessage, result string) (string, bool)
}

// promptWindowClient has one lifetime per Run, including self-check. The
// original user boundaries never change: a revision's synthetic user message
// must not make the actual current user removable. Only older explicitly
// requeryable results may be omitted; the latest group and all other evidence
// stay intact. Requests retain the original history; both cursors move forward.
type promptWindowClient struct {
	inner      LLMClient
	tools      *Registry
	firstUser  int
	lastUser   int
	boundaries []int
	trimmed    int
	compacted  map[int]string
}

func newPromptWindowClient(inner LLMClient, history []modelclient.ChatMessage, tools *Registry) *promptWindowClient {
	window := &promptWindowClient{inner: inner, tools: tools, firstUser: -1, lastUser: -1, compacted: map[int]string{}}
	for i, message := range history {
		if message.Role != "user" {
			continue
		}
		window.lastUser = i
		if window.firstUser < 0 {
			window.firstUser = i
		} else {
			// Each next original user starts a new turn, closing the previous
			// one. The final boundary is the protected current user's start.
			window.boundaries = append(window.boundaries, i)
		}
	}
	return window
}

func (w *promptWindowClient) Stream(ctx context.Context, request Request, onDelta, onReasoning func(string)) (Turn, error) {
	// Legacy pure fake clients omit protocol. Production always identifies its
	// protocol; any nonempty unknown value fails in the real payload encoder.
	if request.Protocol == "" {
		return w.inner.Stream(ctx, request, onDelta, onReasoning)
	}
	trimmedBefore := w.trimmed
	compactedBefore := len(w.compacted)
	for {
		metadata := Turn{TrimmedHistoryTurns: w.trimmed - trimmedBefore, CompactedToolResults: len(w.compacted) - compactedBefore}
		if ctx.Err() != nil {
			return metadata, runContextError(ctx.Err())
		}
		candidate := request
		if w.trimmed > 0 {
			cut := w.boundaries[w.trimmed-1]
			// The run owns the history suffix and always retains its original
			// prefix. Do not mutate or append into the caller's backing array.
			candidate.History = make([]modelclient.ChatMessage, 0, w.firstUser+len(request.History)-cut)
			candidate.History = append(candidate.History, request.History[:w.firstUser]...)
			candidate.History = append(candidate.History, request.History[cut:]...)
			base := strings.TrimSpace(request.SystemPrompt)
			if base == "" {
				base = modelclient.SystemPrompt
			}
			candidate.SystemPrompt = base + "\n\n" + ContextWindowNotice
		}
		if len(w.compacted) > 0 {
			if w.trimmed == 0 {
				candidate.History = append([]modelclient.ChatMessage(nil), request.History...)
			}
			for originalIndex, compacted := range w.compacted {
				index := originalIndex
				if w.trimmed > 0 {
					cut := w.boundaries[w.trimmed-1]
					if originalIndex >= w.firstUser && originalIndex < cut {
						continue
					}
					if originalIndex >= cut {
						index = w.firstUser + originalIndex - cut
					}
				}
				if index >= 0 && index < len(candidate.History) {
					candidate.History[index].Content = compacted
				}
			}
			base := strings.TrimSpace(candidate.SystemPrompt)
			if base == "" {
				base = modelclient.SystemPrompt
			}
			candidate.SystemPrompt = base + "\n\n" + ToolResultCompactionNotice
		}
		size, err := modelclient.PromptSize(modelclient.Protocol(candidate.Protocol), candidate.Model, candidate.History, modelclient.PromptContext{
			SystemPrompt: candidate.SystemPrompt, Memories: candidate.Memories,
			Summary: candidate.Summary, Facts: candidate.Facts,
			BusinessContext: candidate.BusinessContext, KnowledgeContext: candidate.KnowledgeContext,
			ProjectFiles:   candidate.ProjectFiles,
			ActionReceipts: candidate.ActionReceipts, Tools: candidate.Tools,
		})
		metadata.InputBytes = size
		if err != nil {
			return metadata, err
		}
		if size <= modelclient.MaxPromptBytes {
			// Encoding can be nontrivial for a large prompt; cancellation still
			// wins before any provider work after that local preparation.
			if ctx.Err() != nil {
				return metadata, runContextError(ctx.Err())
			}
			turn, streamErr := w.inner.Stream(ctx, candidate, onDelta, onReasoning)
			turn.TrimmedHistoryTurns = metadata.TrimmedHistoryTurns
			turn.CompactedToolResults = metadata.CompactedToolResults
			return turn, streamErr
		}
		if w.trimmed < len(w.boundaries) {
			w.trimmed++
			continue
		}
		if index, compacted := w.oldestCompactableResult(request.History); index >= 0 {
			w.compacted[index] = compacted
			continue
		}
		return metadata, fmt.Errorf("%w: protected request is %d bytes, limit is %d", modelclient.ErrPromptTooLarge, size, modelclient.MaxPromptBytes)
	}
}

func (w *promptWindowClient) oldestCompactableResult(history []modelclient.ChatMessage) (int, string) {
	if w.tools == nil || w.lastUser < 0 {
		return -1, ""
	}
	latestGroup := -1
	for i := w.lastUser + 1; i < len(history); i++ {
		if history[i].Role == "assistant" && len(history[i].ToolCalls) > 0 {
			latestGroup = i
		}
	}
	// Only a prior, complete current-run tool group can be compacted. An
	// injected or orphan tool message cannot become eligible by name alone.
	groupCalls := map[string]modelclient.ToolCall{}
	for i := w.lastUser + 1; i < latestGroup; i++ {
		message := history[i]
		if message.Role == "assistant" {
			groupCalls = map[string]modelclient.ToolCall{}
			for _, call := range message.ToolCalls {
				groupCalls[call.ID] = call
			}
			continue
		}
		call, paired := groupCalls[message.ToolCallID]
		if message.Role != "tool" || w.compacted[i] != "" || len(message.Content) <= len(compactedToolResult)+128 || !paired || call.Name != message.ToolName {
			continue
		}
		tool, ok := w.tools.Get(message.ToolName)
		if !ok {
			continue
		}
		policy, ok := tool.(RequeryableResult)
		if ok && policy.RequeryableResult() {
			compacted := compactedToolResult
			prefix := tool.Name() + ": "
			if compactor, ok := tool.(EvidenceCompactor); ok && strings.HasPrefix(message.Content, prefix) {
				if payload, accepted := compactor.CompactResult(call.Arguments, strings.TrimPrefix(message.Content, prefix)); accepted {
					candidate := prefix + payload
					if len(candidate) <= maxCompactedEvidenceBytes && len(candidate)+128 < len(message.Content) {
						compacted = candidate
					}
				}
			}
			return i, compacted
		}
	}
	return -1, ""
}
