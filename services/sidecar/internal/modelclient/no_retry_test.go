package modelclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestStreamExplicitNoRetryNeverRepeatsUncertainRequests(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolOpenAIChat, ProtocolAnthropicMessages} {
		for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusOK} {
			t.Run(string(protocol)+http.StatusText(status), func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.WriteHeader(status)
					// A clean transport EOF without a protocol terminator is still
					// ambiguous: the Provider may have accepted/charged the work.
				}))
				defer server.Close()
				retries := 0
				err := StreamChat(context.Background(), protocol, server.URL, "", "test", nil, PromptContext{
					DisableTransientRetries: true,
					OnRetry:                 func(int, string) { retries++ },
				}, nil, nil, nil, nil, server.Client())
				if err == nil || !errors.Is(err, ErrStream) || requests.Load() != 1 || retries != 0 {
					t.Fatalf("err=%v requests=%d retries=%d", err, requests.Load(), retries)
				}
			})
		}
	}
}
