package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
)

type aiProviderEnvelope struct {
	Data aiProviderResponse `json:"data"`
}

func newAIProviderTestRouter(t *testing.T, now time.Time) (*gin.Engine, *database.Store, *keystore.MemoryStore) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-provider-api.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	keyStore := keystore.NewMemoryStore()
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), KeyStore: keyStore, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store, keyStore
}

func createTestAIProvider(t *testing.T, router *gin.Engine, name, protocol, baseURL, model string, headers map[string]string) aiProviderResponse {
	t.Helper()
	body := []byte(`{"name":"` + name + `","protocol":"` + protocol + `","base_url":"` + baseURL + `","model":"` + model + `"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create AI provider = %d: %s", created.Code, created.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode AI provider: %v", err)
	}
	return envelope.Data
}

func TestAIProviderLifecycleRegistrationAndRemoval(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 123, time.UTC)
	router, store, keyStore := newAIProviderTestRouter(t, now)

	empty := performRequest(router, http.MethodGet, "/api/v1/ai/providers", nil, nil)
	if empty.Code != http.StatusOK || empty.Body.String() != `{"data":[]}` {
		t.Fatalf("empty AI provider list = %d: %s", empty.Code, empty.Body.String())
	}
	body := []byte(`{"name":"deepseek","protocol":"openai_chat","base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}`)
	created := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, map[string]string{
		"Idempotency-Key": "register-deepseek", "X-Request-ID": "018f0000-0000-7000-8000-000000004201",
	})
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` {
		t.Fatalf("create AI provider = %d etag=%q: %s", created.Code, created.Header().Get("ETag"), created.Body.String())
	}
	var createdEnvelope aiProviderEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created AI provider: %v", err)
	}
	provider := createdEnvelope.Data
	if provider.Name != "deepseek" || provider.Protocol != "openai_chat" || provider.BaseURL != "https://api.deepseek.com/v1" ||
		provider.Model != "deepseek-chat" || provider.Status != "unconfigured" || provider.HealthStatus != "unknown" {
		t.Fatalf("created AI provider = %#v", provider)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/ai/providers", body, map[string]string{"Idempotency-Key": "register-deepseek"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay AI provider = %d replay=%q: %s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	idempotencyConflict := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":" deepseek ","protocol":"openai_chat","base_url":"https://api.deepseek.com/v1","model":"deepseek-chat"}`), map[string]string{"Idempotency-Key": "register-deepseek"})
	assertAPIError(t, idempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	nameTaken := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"deepseek","protocol":"openai_chat","base_url":"https://api.deepseek.com/v2","model":"deepseek-chat"}`), nil)
	assertAPIError(t, nameTaken, http.StatusConflict, "AI_PROVIDER_NAME_TAKEN")

	detail := performRequest(router, http.MethodGet, "/api/v1/ai/providers/"+provider.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"1"` {
		t.Fatalf("get AI provider = %d: %s", detail.Code, detail.Body.String())
	}
	missingIfMatch := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+provider.ID, []byte(`{"name":"deepseek-prod"}`), nil)
	assertAPIError(t, missingIfMatch, http.StatusPreconditionRequired, "VERSION_REQUIRED")

	patched := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+provider.ID, []byte(`{"name":"deepseek-prod","model":"deepseek-chat-latest"}`), map[string]string{"If-Match": `"1"`})
	if patched.Code != http.StatusOK || patched.Header().Get("ETag") != `"2"` {
		t.Fatalf("patch AI provider = %d: %s", patched.Code, patched.Body.String())
	}
	var patchedEnvelope aiProviderEnvelope
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedEnvelope); err != nil {
		t.Fatalf("decode patched AI provider: %v", err)
	}
	if patchedEnvelope.Data.Name != "deepseek-prod" || patchedEnvelope.Data.Model != "deepseek-chat-latest" || patchedEnvelope.Data.Version != 2 {
		t.Fatalf("patched AI provider = %#v", patchedEnvelope.Data)
	}
	stalePatch := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+provider.ID, []byte(`{"name":"stale"}`), map[string]string{"If-Match": `"1"`})
	assertAPIError(t, stalePatch, http.StatusConflict, "VERSION_CONFLICT")

	account := aiProviderKeyAccount(provider.ID)
	if _, err := keyStore.Get(aiProviderKeyService, account); err == nil {
		t.Fatalf("key existed before it was stored")
	}
	keySet := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-test-secret"}`), map[string]string{"If-Match": `"2"`})
	if keySet.Code != http.StatusOK || keySet.Header().Get("ETag") != `"3"` {
		t.Fatalf("set AI provider key = %d: %s", keySet.Code, keySet.Body.String())
	}
	var keyEnvelope aiProviderEnvelope
	if err := json.Unmarshal(keySet.Body.Bytes(), &keyEnvelope); err != nil {
		t.Fatalf("decode key response: %v", err)
	}
	if !keyEnvelope.Data.HasKey {
		t.Fatalf("provider has_key not set after storing the key")
	}
	if strings.Contains(keySet.Body.String(), "sk-test-secret") {
		t.Fatalf("API key leaked in response: %s", keySet.Body.String())
	}
	storedKey, err := keyStore.Get(aiProviderKeyService, account)
	if err != nil || storedKey != "sk-test-secret" {
		t.Fatalf("stored key = %q err=%v", storedKey, err)
	}
	keyMalformed := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"   "}`), map[string]string{"If-Match": `"3"`})
	assertAPIError(t, keyMalformed, http.StatusUnprocessableEntity, "AI_KEY_MALFORMED")
	staleKey := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-overwrite"}`), map[string]string{"If-Match": `"2"`})
	assertAPIError(t, staleKey, http.StatusConflict, "VERSION_CONFLICT")
	if stored, err := keyStore.Get(aiProviderKeyService, account); err != nil || stored != "sk-test-secret" {
		t.Fatalf("stale key write must not overwrite the stored secret: got %q err=%v", stored, err)
	}

	deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/providers/"+provider.ID, nil, map[string]string{"If-Match": `"3"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete AI provider = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := keyStore.Get(aiProviderKeyService, account); err == nil {
		t.Fatalf("key survived provider deletion")
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/ai/providers/"+provider.ID, nil, nil)
	assertAPIError(t, missing, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND")

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'ai_provider' AND action = 'ai_adapter_registered' AND request_id = ?", 1, "018f0000-0000-7000-8000-000000004201")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'ai_provider' AND action = 'ai_adapter_removed'", 1)
}

func TestAIProviderValidationRejectsBadFields(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	invalidProtocol := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"p","protocol":"claude_http","base_url":"https://api.example.com","model":"m"}`), nil)
	assertAPIError(t, invalidProtocol, http.StatusUnprocessableEntity, "AI_PROTOCOL_INVALID")
	invalidEndpoint := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"p","protocol":"openai_chat","base_url":"ftp://example.com","model":"m"}`), nil)
	assertAPIError(t, invalidEndpoint, http.StatusUnprocessableEntity, "AI_ENDPOINT_INVALID")
	invalidName := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"   ","protocol":"openai_chat","base_url":"https://api.example.com","model":"m"}`), nil)
	assertAPIError(t, invalidName, http.StatusUnprocessableEntity, "AI_PROVIDER_NAME_INVALID")
	invalidModel := performRequest(router, http.MethodPost, "/api/v1/ai/providers", []byte(`{"name":"p","protocol":"openai_chat","base_url":"https://api.example.com","model":" "}`), nil)
	assertAPIError(t, invalidModel, http.StatusUnprocessableEntity, "AI_MODEL_INVALID")
	badID := performRequest(router, http.MethodGet, "/api/v1/ai/providers/not-a-uuid", nil, nil)
	assertAPIError(t, badID, http.StatusBadRequest, "INVALID_AI_PROVIDER_ID")
}

func TestAIProviderHealthCheckWithoutKey(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createTestAIProvider(t, router, "no-key", "openai_chat", "https://api.deepseek.com/v1", "deepseek-chat", nil)
	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"1"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("health without key = %d: %s", checked.Code, checked.Body.String())
	}
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if envelope.Data.Status != "unavailable" || envelope.Data.HealthStatus != "unhealthy" ||
		envelope.Data.HealthErrorCode == nil || *envelope.Data.HealthErrorCode != "AI_KEY_UNAVAILABLE" {
		t.Fatalf("health without key = %#v", envelope.Data)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'ai_provider' AND action = 'ai_adapter_health_checked'", 1)
}

func TestAIProviderHealthCheckWithMockOpenAIServer(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)

	t.Run("healthy", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Fatalf("probe path = %q", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer sk-test" {
				t.Fatalf("probe auth = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"object":"list"}`))
		}))
		defer upstream.Close()
		provider := createTestAIProvider(t, router, "mock-openai", "openai_chat", upstream.URL+"/v1", "gpt-test", nil)
		keySet := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-test"}`), map[string]string{"If-Match": `"1"`})
		if keySet.Code != http.StatusOK {
			t.Fatalf("set key = %d: %s", keySet.Code, keySet.Body.String())
		}
		checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
		var envelope aiProviderEnvelope
		if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil || checked.Code != http.StatusOK {
			t.Fatalf("health = %d err=%v: %s", checked.Code, err, checked.Body.String())
		}
		if envelope.Data.Status != "ready" || envelope.Data.HealthStatus != "healthy" || envelope.Data.HealthErrorCode != nil {
			t.Fatalf("healthy provider = %#v", envelope.Data)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer upstream.Close()
		provider := createTestAIProvider(t, router, "mock-unauthorized", "openai_chat", upstream.URL+"/v1", "gpt-test", nil)
		performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-bad"}`), map[string]string{"If-Match": `"1"`})
		checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
		var envelope aiProviderEnvelope
		if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode health: %v", err)
		}
		if envelope.Data.HealthErrorCode == nil || *envelope.Data.HealthErrorCode != "AI_KEY_INVALID" {
			t.Fatalf("unauthorized health = %#v", envelope.Data)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		deadURL := upstream.URL // close to force connection refused
		upstream.Close()
		provider := createTestAIProvider(t, router, "mock-unreachable", "openai_chat", deadURL+"/v1", "gpt-test", nil)
		performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-test"}`), map[string]string{"If-Match": `"1"`})
		checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
		var envelope aiProviderEnvelope
		if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode health: %v", err)
		}
		if envelope.Data.HealthErrorCode == nil || *envelope.Data.HealthErrorCode != "AI_ENDPOINT_UNREACHABLE" {
			t.Fatalf("unreachable health = %#v", envelope.Data)
		}
	})
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'ai_provider' AND action = 'ai_adapter_health_checked'", 3)
}

func TestAIProviderHealthCheckAnthropicProtocol(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	router, _, _ := newAIProviderTestRouter(t, now)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("anthropic probe path = %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-anthropic" {
			t.Fatalf("anthropic probe key header = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("anthropic probe version header = %q", r.Header.Get("anthropic-version"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	provider := createTestAIProvider(t, router, "mock-anthropic", "anthropic_messages", upstream.URL, "claude-test", nil)
	performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-anthropic"}`), map[string]string{"If-Match": `"1"`})
	checked := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/health", nil, map[string]string{"If-Match": `"2"`})
	var envelope aiProviderEnvelope
	if err := json.Unmarshal(checked.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if envelope.Data.Status != "ready" || envelope.Data.HealthStatus != "healthy" {
		t.Fatalf("anthropic health = %#v", envelope.Data)
	}
}

func TestAIProviderConnectionOrKeyChangeRequiresFreshHealthCheck(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	router, _, _ := newAIProviderTestRouter(t, now)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "health-reset", upstream.URL+"/v1", "gpt-test")

	patched := performRequest(router, http.MethodPatch, "/api/v1/ai/providers/"+provider.ID, []byte(`{"model":"gpt-new"}`), map[string]string{"If-Match": `"3"`})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch provider identity = %d: %s", patched.Code, patched.Body.String())
	}
	var patchedEnvelope aiProviderEnvelope
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedEnvelope); err != nil {
		t.Fatalf("decode patched provider: %v", err)
	}
	if patchedEnvelope.Data.Status != "unconfigured" || patchedEnvelope.Data.HealthStatus != "unknown" || patchedEnvelope.Data.LastHealthAt != nil {
		t.Fatalf("connection change kept stale health: %#v", patchedEnvelope.Data)
	}

	replacedKey := performRequest(router, http.MethodPost, "/api/v1/ai/providers/"+provider.ID+"/key", []byte(`{"api_key":"sk-replaced"}`), map[string]string{"If-Match": `"4"`})
	if replacedKey.Code != http.StatusOK {
		t.Fatalf("replace provider key = %d: %s", replacedKey.Code, replacedKey.Body.String())
	}
	var keyEnvelope aiProviderEnvelope
	if err := json.Unmarshal(replacedKey.Body.Bytes(), &keyEnvelope); err != nil {
		t.Fatalf("decode key-replaced provider: %v", err)
	}
	if keyEnvelope.Data.Status != "unconfigured" || keyEnvelope.Data.HealthStatus != "unknown" || keyEnvelope.Data.LastHealthAt != nil {
		t.Fatalf("key replacement kept stale health: %#v", keyEnvelope.Data)
	}
}
