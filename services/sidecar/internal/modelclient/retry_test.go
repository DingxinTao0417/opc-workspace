package modelclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func successfulOpenAIStream() string {
	return "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
}

func TestStreamRetriesTransientUpstreamFailureBeforeAnyOutput(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
			return
		}
		_, _ = w.Write([]byte(successfulOpenAIStream()))
	}))
	defer server.Close()
	var content string
	retries := []string{}
	attemptsSeen := []int{}
	err := StreamChat(context.Background(), ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{
		OnRetry: func(attempt int, reason string) {
			attemptsSeen = append(attemptsSeen, attempt)
			retries = append(retries, reason)
		},
	}, func(s string) { content += s }, nil, nil, nil, server.Client())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if content != "ok" || attempts.Load() != 3 {
		t.Fatalf("content=%q attempts=%d", content, attempts.Load())
	}
	if len(retries) != 2 || retries[0] != "upstream_503" || attemptsSeen[0] != 1 || attemptsSeen[1] != 2 {
		t.Fatalf("retries=%v attempts=%v", retries, attemptsSeen)
	}
}

func TestStreamDoesNotRetryDeterministicRejection(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()
	calls := 0
	err := StreamChat(context.Background(), ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{
		OnRetry: func(int, string) { calls++ },
	}, nil, nil, nil, nil, server.Client())
	var status *UpstreamStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err=%v", err)
	}
	if attempts.Load() != 1 || calls != 0 {
		t.Fatalf("attempts=%d retries=%d", attempts.Load(), calls)
	}
}

func TestStreamDoesNotRetryAfterAnyProviderOutput(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Content followed by a closed stream: the partial answer already
		// reached the caller, so repeating the request would duplicate it.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	}))
	defer server.Close()
	var content string
	calls := 0
	err := StreamChat(context.Background(), ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{
		OnRetry: func(int, string) { calls++ },
	}, func(s string) { content += s }, nil, nil, nil, server.Client())
	if !errors.Is(err, ErrStream) {
		t.Fatalf("err=%v", err)
	}
	if content != "partial" || attempts.Load() != 1 || calls != 0 {
		t.Fatalf("content=%q attempts=%d retries=%d", content, attempts.Load(), calls)
	}
}

func TestStreamRetryBudgetIsBounded(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	calls := 0
	err := StreamChat(context.Background(), ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{
		OnRetry: func(int, string) { calls++ },
	}, nil, nil, nil, nil, server.Client())
	var status *UpstreamStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusInternalServerError {
		t.Fatalf("err=%v", err)
	}
	if attempts.Load() != int32(MaxTransientRetries+1) || calls != MaxTransientRetries {
		t.Fatalf("attempts=%d retries=%d", attempts.Load(), calls)
	}
}

func TestStreamRetryCapsRetryAfterAndHonoursCancellation(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := StreamChat(ctx, ProtocolOpenAIChat, server.URL, "", "test", nil, PromptContext{}, nil, nil, nil, nil, server.Client())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation was not observed during backoff: %s", elapsed)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
}

func TestParseRetryAfterIgnoresInvalidAndCapsSeconds(t *testing.T) {
	if parseRetryAfter("") != 0 || parseRetryAfter("soon") != 0 || parseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT") != 0 {
		t.Fatal("invalid Retry-After values must be ignored")
	}
	if parseRetryAfter("1") != time.Second {
		t.Fatal("seconds form must be honoured")
	}
	if parseRetryAfter("600") != transientRetryMaxDelay {
		t.Fatal("Retry-After must be capped")
	}
}
