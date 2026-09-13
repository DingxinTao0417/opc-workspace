package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
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
	if adapter.AdapterKey != "builtin-local-text-v1" || adapter.Status != "disabled" ||
		adapter.ProtocolVersion != agentAdapterProtocolVersion {
		t.Fatalf("created Agent Adapter = %#v", adapter)
	}
	// ADR-027: on a verified-Windows build the builtin lifecycle matrix ships
	// ready; other platforms keep the blocked diagnostic state.
	if ready, _ := builtinExecutionReady(); ready {
		if !adapter.ExecutionReady || !adapter.Readiness.CanEnable || adapter.IsolationStatus != "verified" ||
			adapter.HealthStatus != "healthy" {
			t.Fatalf("verified builtin adapter should be execution ready = %#v", adapter)
		}
	} else if adapter.ExecutionReady || adapter.Readiness.CanEnable || adapter.HealthStatus != "unknown" {
		t.Fatalf("unverified builtin adapter must stay blocked = %#v", adapter)
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
	ready, _ := builtinExecutionReady()
	if ready {
		if checkedAdapter.HealthStatus != "healthy" || checkedAdapter.HealthErrorCode != nil ||
			checkedAdapter.IsolationStatus != "verified" || !checkedAdapter.ExecutionReady ||
			checkedAdapter.Version != 1 || checkedAdapter.LastHealthAt == nil {
			t.Fatalf("verified builtin check should confirm readiness = %#v", checkedAdapter)
		}
		enable := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
		if enable.Code != http.StatusOK {
			t.Fatalf("enable verified builtin = %d: %s", enable.Code, enable.Body.String())
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent' AND agent_adapter_id = ?", 1, adapter.ID)
	} else {
		if checkedAdapter.HealthStatus != "blocked" || checkedAdapter.HealthErrorCode == nil ||
			*checkedAdapter.HealthErrorCode != agentAdapterIsolationBlockedCode || checkedAdapter.IsolationStatus != "unverified" ||
			checkedAdapter.ExecutionReady || checkedAdapter.Version != 1 || checkedAdapter.LastHealthAt == nil {
			t.Fatalf("checked Agent Adapter = %#v", checkedAdapter)
		}
		enable := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
		assertAPIError(t, enable, http.StatusConflict, "AGENT_ADAPTER_NOT_EXECUTION_READY")
	}
	disableVersion := int64(1)
	expectedDisableVersion := int64(1)
	if ready {
		disableVersion = 2
		expectedDisableVersion = 3
	}
	disabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, disableVersion)})
	if disabled.Code != http.StatusOK || disabled.Header().Get("ETag") != fmt.Sprintf(`"%d"`, expectedDisableVersion) {
		t.Fatalf("disable already-disabled Agent Adapter = %d: %s", disabled.Code, disabled.Body.String())
	}

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'agent_adapter' AND action = 'agent_adapter_registered' AND request_id = ?", 1, requestID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'agent_adapter' AND action = 'agent_adapter_health_checked'", 1)
	agentActors := int64(0)
	if ready {
		agentActors = 1
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", agentActors)
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

func registerReadyAgentAdapter(t *testing.T, router http.Handler) agentAdapterResponse {
	t.Helper()
	created := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	var envelope agentAdapterEnvelope
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &envelope) != nil {
		t.Fatalf("register adapter = %d: %s", created.Code, created.Body.String())
	}
	if !envelope.Data.ExecutionReady {
		t.Skip("builtin execution matrix is not verified on this platform")
	}
	return envelope.Data
}

func TestAgentAdapterEnableRecordsCommittedStateAndReplaysWithoutSideEffects(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	adapter := registerReadyAgentAdapter(t, router)
	endpoint := "/api/v1/agent-adapters/" + adapter.ID + "/enable"
	enabled := performRequest(router, http.MethodPost, endpoint, nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK || enabled.Header().Get("ETag") != `"2"` {
		t.Fatalf("enable adapter = %d: %s", enabled.Code, enabled.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled' AND json_extract(previous_json, '$.status') = 'disabled' AND json_extract(current_json, '$.status') = 'enabled' AND json_extract(current_json, '$.version') = 2", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id = ? AND type = 'agent' AND status = 'active' AND agent_adapter_id = ?", 1, agentAdapterBuiltinActorID, adapter.ID)

	repeated := performRequest(router, http.MethodPost, endpoint, nil, map[string]string{"If-Match": `"2"`})
	if repeated.Code != http.StatusOK || repeated.Header().Get("ETag") != `"2"` {
		t.Fatalf("repeat enable adapter = %d: %s", repeated.Code, repeated.Body.String())
	}
	stale := performRequest(router, http.MethodPost, endpoint, nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, stale, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE actor_id = ?", 0, agentAdapterBuiltinActorID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAgentAdapterEnableRejectsConflictingActor(t *testing.T) {
	for _, actorType := range []string{"person", "inactive_agent"} {
		t.Run(actorType, func(t *testing.T) {
			router, store := newKnowledgeTestAPI(t)
			adapter := registerReadyAgentAdapter(t, router)
			actor := models.Actor{
				ID: agentAdapterBuiltinActorID, Type: "person", DisplayName: "Existing actor", Status: "active",
				MetadataJSON: "{}", Version: 1, CreatedAt: adapter.CreatedAt, UpdatedAt: adapter.UpdatedAt,
			}
			if actorType == "inactive_agent" {
				actor.Type, actor.Status, actor.AgentAdapterID = "agent", "inactive", &adapter.ID
			}
			if err := store.DB.Create(&actor).Error; err != nil {
				t.Fatalf("create isolated conflicting actor fixture: %v", err)
			}
			enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
			assertAPIError(t, enabled, http.StatusConflict, "AGENT_ADAPTER_ACTOR_CONFLICT")
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_adapters WHERE id = ? AND status = 'disabled' AND version = 1", 1, adapter.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE id = ? AND type = ? AND status = ? AND version = 1", 1, actor.ID, actor.Type, actor.Status)
		})
	}
}

func TestAgentAdapterEnableZeroUpdatedRowsDoesNotCreateActor(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	adapter := registerReadyAgentAdapter(t, router)
	// An isolated trigger models a rejected conditional update: creating an
	// Actor must never happen when no adapter transition was committed.
	if err := store.DB.Exec(`CREATE TRIGGER test_ignore_adapter_enable BEFORE UPDATE ON agent_adapters
		WHEN NEW.status = 'enabled' BEGIN SELECT RAISE(IGNORE); END`).Error; err != nil {
		t.Fatalf("install isolated update failure fixture: %v", err)
	}
	enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, enabled, http.StatusConflict, "VERSION_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_adapters WHERE id = ? AND status = 'disabled' AND version = 1", 1, adapter.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 0)
}

func TestAgentAdapterEnableActorInsertFailureRollsBackTransition(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	adapter := registerReadyAgentAdapter(t, router)
	if err := store.DB.Exec(`CREATE TRIGGER test_fail_agent_actor_insert BEFORE INSERT ON actors
		WHEN NEW.type = 'agent' BEGIN SELECT RAISE(ABORT, 'TEST_ACTOR_INSERT_FAILURE'); END`).Error; err != nil {
		t.Fatalf("install isolated insert failure fixture: %v", err)
	}
	enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusInternalServerError {
		t.Fatalf("enable with actor insert failure = %d: %s", enabled.Code, enabled.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_adapters WHERE id = ? AND status = 'disabled' AND version = 1", 1, adapter.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 0)
}

func TestAgentAdapterEnableRejectsMissingActorOnEnabledAdapter(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	adapter := registerReadyAgentAdapter(t, router)
	if err := store.DB.Exec("UPDATE agent_adapters SET status = 'enabled', version = 2 WHERE id = ?", adapter.ID).Error; err != nil {
		t.Fatalf("create isolated missing actor fixture: %v", err)
	}
	enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"2"`})
	assertAPIError(t, enabled, http.StatusConflict, "AGENT_ADAPTER_ACTOR_CONFLICT")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 0)
}

func TestAgentAdapterEnableRejectsUnreadyAdapterWithoutActor(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	var envelope agentAdapterEnvelope
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &envelope) != nil {
		t.Fatalf("register adapter = %d: %s", created.Code, created.Body.String())
	}
	if err := store.DB.Exec("UPDATE agent_adapters SET execution_ready = 0, isolation_status = 'unverified' WHERE id = ?", envelope.Data.ID).Error; err != nil {
		t.Fatalf("create isolated unready fixture: %v", err)
	}
	enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+envelope.Data.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, enabled, http.StatusConflict, "AGENT_ADAPTER_NOT_EXECUTION_READY")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM actors WHERE type = 'agent'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_adapters WHERE id = ? AND status = 'disabled' AND version = 1", 1, envelope.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_adapter_enabled'", 0)
}

func TestAgentAdapterActorResponseExposesReadOnlyAdapterReference(t *testing.T) {
	router, _ := newKnowledgeTestAPI(t)
	adapter := registerReadyAgentAdapter(t, router)
	enabled := performRequest(router, http.MethodPost, "/api/v1/agent-adapters/"+adapter.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable adapter = %d: %s", enabled.Code, enabled.Body.String())
	}
	for _, actorID := range []string{agentAdapterBuiltinActorID, models.BuiltinOwnerActorID} {
		detail := performRequest(router, http.MethodGet, "/api/v1/actors/"+actorID, nil, nil)
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &envelope) != nil {
			t.Fatalf("get actor = %d: %s", detail.Code, detail.Body.String())
		}
		value, exists := envelope.Data["agent_adapter_id"]
		if !exists || (actorID == agentAdapterBuiltinActorID && value != adapter.ID) || (actorID == models.BuiltinOwnerActorID && value != nil) {
			t.Fatalf("actor %s adapter reference = %#v, exists=%v", actorID, value, exists)
		}
	}
}
