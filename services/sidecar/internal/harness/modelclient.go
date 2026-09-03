package harness

import (
	"context"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
)

// ModelClient adapts the protocol-mapper streaming client to LLMClient. A nil
// inner client uses the default HTTP client honoring the process proxy
// environment (loopback endpoints are never proxied).
type ModelClient struct {
	inner *http.Client
}

// NewModelClient returns the production LLMClient implementation.
func NewModelClient(inner *http.Client) *ModelClient {
	return &ModelClient{inner: inner}
}

// Stream runs one streaming chat completion, forwarding deltas and reasoning
// chunks as they arrive and returning the accumulated round. Request.Memories
// ride inside the system prompt (ADR-006).
func (m *ModelClient) Stream(ctx context.Context, request Request, onDelta func(string), onReasoning func(string)) (Turn, error) {
	var turn Turn
	err := modelclient.StreamChat(ctx, modelclient.Protocol(request.Protocol), request.BaseURL, request.APIKey, request.Model,
		request.History, request.Memories,
		func(delta string) {
			turn.Text += delta
			if onDelta != nil {
				onDelta(delta)
			}
		},
		func(reasoning string) {
			turn.Reasoning += reasoning
			if onReasoning != nil {
				onReasoning(reasoning)
			}
		},
		m.inner)
	return turn, err
}
