package harness

import (
	"context"
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// One instance is shared by every model round, including the silent revision.
// The model adapter also applies this limit to raw tool-call deltas as they
// arrive. The wrapper enforces the same contract for any LLMClient adapter.
type budgetedClient struct {
	inner     LLMClient
	remaining int
}

func (b *budgetedClient) Stream(ctx context.Context, request Request, onDelta, onReasoning func(string)) (Turn, error) {
	if b.remaining <= 0 {
		return Turn{}, modelclient.ErrResponseBudget
	}
	request.ResponseByteLimit = b.remaining
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var text, reasoning string
	streamBytes := 0
	overflow := false
	accept := func(s string, destination *string, callback func(string)) {
		if overflow {
			return
		}
		if streamBytes+len(s) > b.remaining {
			overflow = true
			cancel()
			return
		}
		streamBytes += len(s)
		*destination += s
		if callback != nil {
			callback(s)
		}
	}
	turn, err := b.inner.Stream(turnCtx, request, func(s string) { accept(s, &text, onDelta) }, func(s string) { accept(s, &reasoning, onReasoning) })
	outputBytes := len(turn.Text) + len(turn.Reasoning)
	for _, call := range turn.ToolCalls {
		outputBytes += len(call.ID) + len(call.Name) + len(call.Arguments)
	}
	if turn.OutputBytes > outputBytes {
		outputBytes = turn.OutputBytes
	}
	if outputBytes > b.remaining || overflow {
		turn.Text, turn.Reasoning, turn.ToolCalls = text, reasoning, nil
		turn.OutputBytes = streamBytes
		b.remaining -= streamBytes
		return turn, modelclient.ErrResponseBudget
	}
	b.remaining -= outputBytes
	if ctx.Err() != nil {
		return turn, runContextError(ctx.Err())
	}
	return turn, err
}

func runContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return modelclient.ErrTimeout
	}
	return err
}
