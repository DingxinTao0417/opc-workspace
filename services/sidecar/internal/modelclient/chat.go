package modelclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// SystemPrompt is code-owned: it never enters the database, logs, or export
// surface, and it defines the only sanctioned structured outputs (the task
// suggestion block parsed by the WebView for the confirm-card flow, and the
// memory suggestion block for user-confirmed long-term preferences).
const SystemPrompt = `你是 opc-workspace 的本地 AI 助手，只提供问答、摘要与建议，输出只读：
- 不执行任务、不修改任何业务数据；无法安全完成时明确说明并拒绝。
- 仅当用户明确表达创建任务的意图，或清楚描述了一个待办工作时，先用自然语言确认你理解的任务，再在回复末尾输出一个任务建议块；结构化块不能作为整条回复的唯一内容，格式严格为：
[opc:task]{"title":"任务标题","description":"可选描述","due":"YYYY-MM-DD 或省略"}[/opc:task]
- title 必填；描述与截止日期不确定时省略；没有任务意图时绝不输出该块。
- 仅当用户明确要求你记住某件事，或清楚表达了持久偏好时，在回复末尾输出一个记忆建议块，格式严格为：
[opc:memory]{"content":"要记住的偏好或事实"}[/opc:memory]
- content 必填且不超过 200 字；没有明确的记忆意图时绝不输出该块。
- 每次回答结束前，自评该回答是否已完整、准确地满足用户的请求（含工具输出是否足以支撑结论），并在回复最末尾输出自评块，格式严格为二选一：
[opc:selfcheck]{"sufficient":true}
[opc:selfcheck]{"sufficient":false,"note":"未满足之处的简要说明"}
- 自评基于你自己的判断独立完成；确有不足才输出 false 并给出简要 note。
- 任务/记忆建议块必须使用带斜杠的闭合标记；普通自然语言回答可按用户需要使用 Markdown、JSON 或代码块，但不得伪造其他 opc 控制块。`

const (
	// FirstTokenTimeout bounds the wait for the first streamed delta.
	FirstTokenTimeout = 90 * time.Second
	// TotalTimeout bounds the whole generation.
	TotalTimeout = 10 * time.Minute
	// MaxResponseBytes caps the accumulated assistant text.
	MaxResponseBytes = 1 << 20
	// MaxPromptBytes caps the serialized prompt sent upstream.
	MaxPromptBytes = 64 << 10
)

// ErrTimeout reports the generation exceeded its time budget.
var ErrTimeout = errors.New("modelclient: generation timed out")

// ErrPromptTooLarge reports that the fully serialized provider request exceeds
// the configured prompt budget.
var ErrPromptTooLarge = errors.New("modelclient: prompt exceeded byte budget")

// ErrStream reports the upstream stream broke or exceeded its size cap.
var ErrStream = errors.New("modelclient: stream error")

// UpstreamStatusError reports a non-2xx chat completion response; it wraps
// ErrStream so generic stream handling keeps working while the status stays
// addressable for error mapping. Snippet carries a sanitized excerpt of the
// upstream error body (truncated, control characters removed) so provider
// rejections like "unknown model" are diagnosable instead of surfacing as a
// bare 404.
type UpstreamStatusError struct {
	StatusCode int
	Snippet    string
}

func (e *UpstreamStatusError) Error() string {
	if e.Snippet == "" {
		return fmt.Sprintf("upstream status %d", e.StatusCode)
	}
	return fmt.Sprintf("upstream status %d: %s", e.StatusCode, e.Snippet)
}

func (e *UpstreamStatusError) Unwrap() error { return ErrStream }

// ChatMessage is one prompt entry in provider-neutral form.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamChat opens a streaming chat completion and invokes onDelta for every
// text chunk and onReasoning for every reasoning (chain-of-thought) chunk the
// provider streams; either callback may be nil. Reasoning is never mixed into
// the reply text. systemNotes (e.g. user-confirmed long-term memories,
// ADR-006) are appended to the code-owned system prompt for both protocol
// families. client may be nil to use a default client honoring the process
// proxy environment (loopback endpoints are never proxied); tests pass an
// explicit client. Cancellation must flow through ctx.
func StreamChat(ctx context.Context, protocol Protocol, baseURL, apiKey, model string, history []ChatMessage, systemNotes []string, onDelta func(string), onReasoning func(string), client *http.Client) error {
	if client == nil {
		client = &http.Client{}
	}
	parentCtx := ctx
	totalCtx, cancelTotal := context.WithTimeout(parentCtx, TotalTimeout)
	defer cancelTotal()

	watchCtx, cancelWatch := context.WithCancel(totalCtx)
	defer cancelWatch()
	var firstTokenTimedOut atomic.Bool
	firstToken := time.AfterFunc(FirstTokenTimeout, func() {
		firstTokenTimedOut.Store(true)
		cancelWatch()
	})
	defer firstToken.Stop()

	payload, endpoint, headers, err := buildChatRequest(protocol, baseURL, apiKey, model, history, systemNotes)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(watchCtx, http.MethodPost, endpoint, strings.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		if firstTokenTimedOut.Load() || errors.Is(totalCtx.Err(), context.DeadlineExceeded) {
			return ErrTimeout
		}
		if parentCtx.Err() != nil {
			return parentCtx.Err()
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		snippet := sanitizeUpstreamErrorBody(response.Body)
		return &UpstreamStatusError{StatusCode: response.StatusCode, Snippet: snippet}
	}

	total := 0
	gotFirst := false
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		if firstTokenTimedOut.Load() || errors.Is(totalCtx.Err(), context.DeadlineExceeded) {
			return ErrTimeout
		}
		if parentCtx.Err() != nil {
			return parentCtx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return nil
		}
		delta, reasoning, stop, err := decodeStreamDelta(protocol, data)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
		if delta == "" && reasoning == "" {
			continue
		}
		if !gotFirst {
			firstToken.Stop()
			gotFirst = true
		}
		total += len(delta) + len(reasoning)
		if total > MaxResponseBytes {
			return fmt.Errorf("%w: response exceeded %d bytes", ErrStream, MaxResponseBytes)
		}
		if delta != "" && onDelta != nil {
			onDelta(delta)
		}
		if reasoning != "" && onReasoning != nil {
			onReasoning(reasoning)
		}
	}
	if err := scanner.Err(); err != nil {
		if firstTokenTimedOut.Load() || errors.Is(totalCtx.Err(), context.DeadlineExceeded) {
			return ErrTimeout
		}
		if parentCtx.Err() != nil {
			return parentCtx.Err()
		}
		return fmt.Errorf("%w: %v", ErrStream, err)
	}
	if !gotFirst {
		return fmt.Errorf("%w: upstream closed before first token", ErrStream)
	}
	return nil
}

// sanitizeUpstreamErrorBody reads up to 512 bytes of an upstream error
// response and reduces it to one line of printable text, so provider error
// messages survive into diagnostics without control characters or dumps.
func sanitizeUpstreamErrorBody(body io.Reader) string {
	excerpt, _ := io.ReadAll(io.LimitReader(body, 512))
	line := strings.Join(strings.Fields(string(excerpt)), " ")
	if len(line) > 300 {
		line = line[:300]
	}
	return line
}

// buildChatRequest returns the JSON body, endpoint, and protocol headers.
// User-confirmed memory notes ride inside the system role for both protocol
// families so context injection stays protocol-neutral (ADR-006).
func buildChatRequest(protocol Protocol, baseURL, apiKey, model string, history []ChatMessage, systemNotes []string) (body, endpoint string, headers map[string]string, err error) {
	base := strings.TrimRight(baseURL, "/")
	encoded, err := encodeChatPayload(protocol, model, history, systemNotes)
	if err != nil {
		return "", "", nil, err
	}
	if len(encoded) > MaxPromptBytes {
		return "", "", nil, fmt.Errorf("%w: request is %d bytes, limit is %d", ErrPromptTooLarge, len(encoded), MaxPromptBytes)
	}
	switch protocol {
	case ProtocolOpenAIChat:
		return string(encoded), base + "/chat/completions", map[string]string{"Authorization": "Bearer " + apiKey}, nil
	case ProtocolAnthropicMessages:
		return string(encoded), base + "/v1/messages", map[string]string{"x-api-key": apiKey, "anthropic-version": "2023-06-01"}, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported provider protocol %q", protocol)
	}
}

// PromptSize returns the exact serialized provider request size used for the
// prompt budget. Callers use it to retain complete recent turns without ever
// dropping the current user message silently.
func PromptSize(protocol Protocol, model string, history []ChatMessage, systemNotes []string) (int, error) {
	encoded, err := encodeChatPayload(protocol, model, history, systemNotes)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

func encodeChatPayload(protocol Protocol, model string, history []ChatMessage, systemNotes []string) ([]byte, error) {
	messages := make([]ChatMessage, 0, len(history)+1)
	messages = append(messages, ChatMessage{Role: "system", Content: SystemPrompt + systemNotesBlock(systemNotes)})
	messages = append(messages, history...)
	switch protocol {
	case ProtocolOpenAIChat:
		payload := map[string]any{"model": model, "stream": true, "messages": messages}
		return json.Marshal(payload)
	case ProtocolAnthropicMessages:
		// Anthropic keeps the system prompt out of the message list.
		chatMessages := make([]ChatMessage, 0, len(history))
		for _, message := range history {
			if message.Role == "assistant" || message.Role == "user" {
				chatMessages = append(chatMessages, message)
			}
		}
		payload := map[string]any{"model": model, "stream": true, "max_tokens": 8192, "system": SystemPrompt + systemNotesBlock(systemNotes), "messages": chatMessages}
		return json.Marshal(payload)
	default:
		return nil, fmt.Errorf("unsupported provider protocol %q", protocol)
	}
}

// systemNotesBlock renders the memory notes appended after the code-owned
// system prompt; empty notes render as the empty string.
func systemNotesBlock(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "\n\n[用户长期偏好（已经用户确认，仅供参考）]\n- " + strings.Join(notes, "\n- ")
}

// decodeStreamDelta extracts the next text or reasoning chunk; stop reports a
// terminal stream event. Reasoning is the provider's chain-of-thought stream
// (DeepSeek `reasoning_content`, Anthropic `thinking_delta`) and never part of
// the reply text.
func decodeStreamDelta(protocol Protocol, data string) (delta string, reasoning string, stop bool, err error) {
	var frame map[string]any
	if json.Unmarshal([]byte(data), &frame) != nil {
		return "", "", false, fmt.Errorf("%w: undecodable stream frame", ErrStream)
	}
	switch protocol {
	case ProtocolOpenAIChat:
		if code, message, ok := streamErrorPayload(frame); ok {
			return "", "", false, fmt.Errorf("%w: %s", ErrStream, messagePrefix(code, message))
		}
		choices, _ := frame["choices"].([]any)
		if len(choices) == 0 {
			return "", "", false, nil
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			return "", "", false, nil
		}
		if finish, _ := choice["finish_reason"].(string); finish == "stop" {
			return "", "", true, nil
		}
		deltaMap, _ := choice["delta"].(map[string]any)
		if deltaMap == nil {
			return "", "", false, nil
		}
		reasoningText, _ := deltaMap["reasoning_content"].(string)
		if reasoningText == "" {
			// OpenRouter-style providers use a `reasoning` field instead.
			reasoningText, _ = deltaMap["reasoning"].(string)
		}
		text, _ := deltaMap["content"].(string)
		return text, reasoningText, false, nil
	case ProtocolAnthropicMessages:
		switch frame["type"] {
		case "error":
			code, message := streamErrorFields(frame)
			return "", "", false, fmt.Errorf("%w: %s", ErrStream, messagePrefix(code, message))
		case "message_stop":
			return "", "", true, nil
		case "content_block_delta":
			deltaMap, _ := frame["delta"].(map[string]any)
			if deltaMap == nil {
				return "", "", false, nil
			}
			if thinking, _ := deltaMap["thinking"].(string); thinking != "" {
				return "", thinking, false, nil
			}
			text, _ := deltaMap["text"].(string)
			return text, "", false, nil
		}
		return "", "", false, nil
	default:
		return "", "", false, fmt.Errorf("unsupported provider protocol %q", protocol)
	}
}

func streamErrorPayload(frame map[string]any) (code, message string, ok bool) {
	if _, present := frame["error"]; !present {
		return "", "", false
	}
	code, message = streamErrorFields(frame)
	return code, message, true
}

func streamErrorFields(frame map[string]any) (code, message string) {
	errObj, _ := frame["error"].(map[string]any)
	if errObj == nil {
		return "unknown", "upstream stream error"
	}
	code, _ = errObj["code"].(string)
	if code == "" {
		code, _ = errObj["type"].(string)
	}
	message, _ = errObj["message"].(string)
	return code, message
}

func messagePrefix(code, message string) string {
	if message == "" {
		message = "upstream stream error"
	}
	if code == "" {
		return message
	}
	return code + ": " + message
}
