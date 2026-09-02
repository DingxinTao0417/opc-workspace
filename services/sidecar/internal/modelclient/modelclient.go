package modelclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Protocol identifies the remote provider protocol adapter. The registry is
// code-owned and extensible: adding a provider family means a new constant
// and a mapper implementation.
type Protocol string

const (
	ProtocolOpenAIChat        Protocol = "openai_chat"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
)

const healthProbeTimeout = 5 * time.Second

// HealthCheck probes the provider endpoint and returns the upstream HTTP
// status code (0 when the request could not be completed). client may be
// nil to use a default client honoring the process proxy environment; tests
// pass an explicit client.
func HealthCheck(ctx context.Context, protocol Protocol, baseURL string, apiKey string, client *http.Client) (statusCode int, err error) {
	if client == nil {
		client = &http.Client{Timeout: healthProbeTimeout}
	}
	path := "/models"
	if protocol == ProtocolAnthropicMessages {
		path = "/v1/models"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	if protocol == ProtocolAnthropicMessages {
		request.Header.Set("x-api-key", apiKey)
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, nil
}
