package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

type agentAdapterEnvelope struct {
	Data agentAdapterResponse `json:"data"`
}

func TestAgentAdapterRegistrationAndBlockedDiagnosticLifecycle(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "agent-adapter-api.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 12, 0, 0, 123, time.UTC)
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return now }, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()

	empty := performRequest(router, http.MethodGet, "/api/v1/agent-adapters", nil, nil)
	if empty.Code != http.StatusOK || empty.Body.String() != `{"data":[]}` {
		t.Fatalf("empty Agent Adapter list = %d: %s", empty.Code, empty.Body.String())
	}
	body := []byte(`{"preset_key":"builtin-local-text-v1"}`)
	requestID := "018f0000-0000-7000-8000-000000003499"
	created := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", body, map[string]string{
		"Idempotency-Key": "register-local-text-adapter", "X-Request-ID": requestID,
	})
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` {
		t.Fatalf("create Agent Adapter = %d etag=%q: %s", created.Code, created.Header().Get("ETag"), created.Body.String())
	}
	if strings.Contains(created.Body.String(), "builtin:local-text-v1") || strings.Contains(created.Body.String(), "executable") {
		t.Fatalf("create response leaked executable identity: %s", created.Body.String())
	}
	var createdEnvelope agentAdapterEnvelope
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode Agent Adapter: %v", err)
	}
	adapter := createdEnvelope.Data
	if adapter.AdapterKey != "builtin-local-text-v1" || adapter.Status != "disabled" || adapter.HealthStatus != "unknown" ||
		adapter.ExecutionReady || adapter.Readiness.CanEnable || adapter.ProtocolVersion != agentAdapterProtocolVersion {
		t.Fatalf("created Agent Adapter = %#v", adapter)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", body, map[string]string{"Idempotency-Key": "register-local-text-adapter"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay Agent Adapter = %d replay=%q: %s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	idempotencyConflict := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":" builtin-local-text-v1 "}`), map[string]string{"Idempotency-Key": "register-local-text-adapter"})
	assertAPIError(t, idempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	conflict := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"unknown"}`), map[string]string{"Idempotency-Key": "register-local-text-adapter"})
	assertAPIError(t, conflict, http.StatusUnprocessableEntity, "AGENT_ADAPTER_PRESET_INVALID")

	detail := performRequest(router, http.MethodGet, "/api/v1/agent-adapters/"+adapter.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"1"` {
		t.Fatalf("get Agent Adapter = %d: %s", detail.Code, detail.Body.String())
	}
	checked := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/check", nil, map[string]string{
		"If-Match": `"1"`, "X-Request-ID": "018f0000-0000-7000-8000-000000003498",
	})
	if checked.Code != http.StatusOK || checked.Header().Get("ETag") != `"1"` {
		t.Fatalf("check Agent Adapter = %d: %s", checked.Code, checked.Body.String())
	}
	var checkedEnvelope agentAdapterEnvelope
	if err := json.Unmarshal(checked.Body.Bytes(), &checkedEnvelope); err != nil {
		t.Fatalf("decode checked Agent Adapter: %v", err)
	}
	checkedAdapter := checkedEnvelope.Data
	if checkedAdapter.HealthStatus != "blocked" || checkedAdapter.HealthErrorCode == nil ||
		*checkedAdapter.HealthErrorCode != agentAdapterIsolationBlockedCode || checkedAdapter.IsolationStatus != "unverified" ||
		checkedAdapter.ExecutionReady || checkedAdapter.Version != 1 || checkedAdapter.LastHealthAt == nil {
		t.Fatalf("checked Agent Adapter = %#v", checkedAdapter)
	}
	enable := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, enable, http.StatusConflict, "AGENT_ADAPTER_NOT_EXECUTION_READY")
	disabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/disable", nil, map[string]string{"If-Match": `"1"`})
	if disabled.Code != http.StatusOK || disabled.Header().Get("ETag") != `"1"` {
		t.Fatalf("disable already-disabled Agent Adapter = %d: %s", disabled.Code, disabled.Body.String())
	}

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'agent_adapter' AND action = 'agent_adapter_registered' AND request_id = ?", 1, requestID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'agent_adapter' AND action = 'agent_adapter_health_checked'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE actor_id NOT IN ('00000000-0000-5000-8000-000000000001', '00000000-0000-5000-8000-000000000002')", 0)
}

func TestAgentAdapterRejectsInvalidInputAndConcurrency(t *testing.T) {
	router := newTestAPI(t)
	invalidJSON := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1","path":"C:\\unsafe.exe"}`), nil)
	assertAPIError(t, invalidJSON, http.StatusBadRequest, "INVALID_JSON")
	invalidID := performRequest(router, http.MethodGet, "/api/v1/agent-adapters/not-a-uuid", nil, nil)
	assertAPIError(t, invalidID, http.StatusBadRequest, "INVALID_AGENT_ADAPTER_ID")
	missing := performRequest(router, http.MethodGet, "/api/v1/agent-adapters/018f0000-0000-5000-8000-000000009999", nil, nil)
	assertAPIError(t, missing, http.StatusNotFound, "AGENT_ADAPTER_NOT_FOUND")

	created := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	var envelope agentAdapterEnvelope
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &envelope) != nil {
		t.Fatalf("create Agent Adapter = %d: %s", created.Code, created.Body.String())
	}
	missingIfMatch := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+envelope.Data.ID+"/check", nil, nil)
	assertAPIError(t, missingIfMatch, http.StatusPreconditionRequired, "VERSION_REQUIRED")
	stale := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+envelope.Data.ID+"/check", nil, map[string]string{"If-Match": `"2"`})
	assertAPIError(t, stale, http.StatusConflict, "VERSION_CONFLICT")
}
