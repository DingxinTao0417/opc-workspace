package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A local provider is keyless by contract (ADR-005): registration, health,
// and chat must work without ever touching the OS credential store, and the
// key endpoints must refuse it.
func TestAILocalProviderKeylessLifecycleAndChat(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, store, keyStore := newAIProviderTestRouter(t, now)
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, delta := range []string{"本地", "模型回答"} {
			frame, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{"content": delta}}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	defer upstream.Close()

	body := []byte(`{"name":"ollama","kind":"local","protocol":"openai_chat","base_url":"` + upstream.URL + `/v1","model":"qwen3"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create local provider = %d: %s", created.Code, created.Body.String())
	}
	var createdEnvelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}
	provider := createdEnvelope.Data
	if provider.Kind != "local" || provider.HasKey || provider.Status != "unconfigured" {
		t.Fatalf("created local provider = %#v", provider)
	}
	if _, err := keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID)); err == nil {
		t.Fatalf("local provider must not have a stored key")
	}

	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"1"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", checked.Code, checked.Body.String())
	}
	var healthEnvelope aiProviderEnvelope
	if err := json.Unmarshal(checked.Body.Bytes(), &healthEnvelope); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if healthEnvelope.Data.Status != "ready" || healthEnvelope.Data.HealthStatus != "healthy" {
		t.Fatalf("local provider not ready without a key: %#v", healthEnvelope.Data)
	}

	response := chatRequest(t, router, provider.ID, "", "你好")
	if response.Code != http.StatusOK {
		t.Fatalf("chat = %d: %s", response.Code, response.Body.String())
	}
	chatBody := response.Body.String()
	if !strings.Contains(chatBody, "event: meta") || !strings.Contains(chatBody, "本地") || !strings.Contains(chatBody, "event: done") {
		t.Fatalf("local chat stream incomplete: %s", chatBody)
	}
	assertDatabaseCount(t, store, `SELECT COUNT(*) FROM ai_messages WHERE role = 'assistant' AND content = ?`, 1, "本地模型回答")

	keyRefused := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-anything"}`), map[string]string{"If-Match": `"2"`})
	assertAPIError(t, keyRefused, http.StatusConflict, "AI_KEY_NOT_ALLOWED")
	if _, err := keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(provider.ID)); err == nil {
		t.Fatalf("key endpoint must not write a secret for a local provider")
	}
}

func TestAILocalProviderValidation(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	for index, endpoint := range []string{
		"http://localhost.evil.example/v1",
		"http://127.0.0.1.evil.example/v1",
		"http://localhost@evil.example/v1",
	} {
		spoofed := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(fmt.Sprintf(`{"name":"spoof-%d","kind":"local","protocol":"openai_chat","base_url":"%s","model":"m"}`, index, endpoint)), nil)
		assertAPIError(t, spoofed, http.StatusUnprocessableEntity, "AI_ENDPOINT_INVALID")
	}
	// Local providers are loopback-http only; https endpoints are rejected.
	httpsEndpoint := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"l1","kind":"local","protocol":"openai_chat","base_url":"https://127.0.0.1:11434/v1","model":"m"}`), nil)
	assertAPIError(t, httpsEndpoint, http.StatusUnprocessableEntity, "AI_ENDPOINT_INVALID")
	// Public remote endpoints are rejected for local kind.
	remoteEndpoint := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"l2","kind":"local","protocol":"openai_chat","base_url":"https://api.deepseek.com/v1","model":"m"}`), nil)
	assertAPIError(t, remoteEndpoint, http.StatusUnprocessableEntity, "AI_ENDPOINT_INVALID")
	// Local providers speak the OpenAI-compatible protocol only.
	anthropicLocal := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"l3","kind":"local","protocol":"anthropic_messages","base_url":"http://127.0.0.1:11434/v1","model":"m"}`), nil)
	assertAPIError(t, anthropicLocal, http.StatusUnprocessableEntity, "AI_PROTOCOL_INVALID")
	// Unknown kinds are rejected.
	unknownKind := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"l4","kind":"embedded","protocol":"openai_chat","base_url":"http://127.0.0.1:11434/v1","model":"m"}`), nil)
	assertAPIError(t, unknownKind, http.StatusUnprocessableEntity, "AI_PROVIDER_KIND_INVALID")

	// kind defaults to remote when omitted.
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"r1","protocol":"openai_chat","base_url":"https://api.deepseek.com/v1","model":"m"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create remote provider = %d: %s", created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	if envelope.Data.Kind != "remote" {
		t.Fatalf("default kind = %q, want remote", envelope.Data.Kind)
	}

	// A local provider can be patched in, and back out, via If-Match updates.
	local := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"l5","kind":"local","protocol":"openai_chat","base_url":"http://localhost:1234/v1","model":"lm-studio"}`), nil)
	if local.Code != http.StatusCreated {
		t.Fatalf("create localhost provider = %d: %s", local.Code, local.Body.String())
	}
	patched := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+envelope.Data.ID, []byte(`{"kind":"local","base_url":"http://127.0.0.1:11434/v1"}`), map[string]string{"If-Match": `"1"`})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch kind = %d: %s", patched.Code, patched.Body.String())
	}
	var patchedEnvelope aiProviderEnvelope
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedEnvelope); err != nil {
		t.Fatalf("decode patched provider: %v", err)
	}
	if patchedEnvelope.Data.Kind != "local" || patchedEnvelope.Data.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("patched provider = %#v", patchedEnvelope.Data)
	}
	backToRemote := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+envelope.Data.ID, []byte(`{"kind":"remote","base_url":"https://api.deepseek.com/v1"}`), map[string]string{"If-Match": `"2"`})
	if backToRemote.Code != http.StatusOK {
		t.Fatalf("patch kind back to remote = %d: %s", backToRemote.Code, backToRemote.Body.String())
	}
}

func TestAILocalProviderUnreachableHealth(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	// Port 1 on loopback refuses connections, exercising the probe failure
	// path without any key involvement.
	body := []byte(`{"name":"offline","kind":"local","protocol":"openai_chat","base_url":"http://127.0.0.1:1/v1","model":"m"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create provider = %d: %s", created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+envelope.Data.ID+"/health", nil, map[string]string{"If-Match": `"1"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", checked.Code, checked.Body.String())
	}
	var healthEnvelope aiProviderEnvelope
	if err := json.Unmarshal(checked.Body.Bytes(), &healthEnvelope); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if healthEnvelope.Data.Status != "unavailable" || healthEnvelope.Data.HealthErrorCode == nil || *healthEnvelope.Data.HealthErrorCode != "AI_ENDPOINT_UNREACHABLE" {
		t.Fatalf("unreachable local provider = %#v", healthEnvelope.Data)
	}
	// Chat stays gated on readiness, exactly like remote providers.
	response := chatRequest(t, router, envelope.Data.ID, "", "hi")
	assertAPIError(t, response, http.StatusConflict, "AI_PROVIDER_NOT_READY")
}
