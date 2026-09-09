package modelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestProviderClientRefusesCrossOriginRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	err := StreamChat(
		context.Background(), ProtocolOpenAIChat, source.URL, "secret", "model",
		[]ChatMessage{{Role: "user", Content: "private prompt"}}, PromptContext{},
		nil, nil, nil, nil, source.Client(),
	)
	var statusError *UpstreamStatusError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect chat error = %v", err)
	}
	status, err := HealthCheck(context.Background(), ProtocolOpenAIChat, source.URL, "secret", source.Client())
	if err != nil || status != http.StatusTemporaryRedirect {
		t.Fatalf("redirect health status=%d err=%v", status, err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("cross-origin redirect reached target %d times", targetHits.Load())
	}
}

func TestProviderClientAllowsSameOriginRedirect(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			http.Redirect(w, r, server.URL+"/stream", http.StatusTemporaryRedirect)
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var content string
	err := StreamChat(
		context.Background(), ProtocolOpenAIChat, server.URL, "secret", "model", nil, PromptContext{},
		func(delta string) { content += delta }, nil, nil, nil, server.Client(),
	)
	if err != nil || content != "ok" {
		t.Fatalf("same-origin redirect content=%q err=%v", content, err)
	}
}
