package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func startAgentRunModelServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	authorizations := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "online-test"}}})
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		*authorizations = append(*authorizations, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": "任务已完成：结论、要点与下一步建议。"},
			}},
		})
	}))
	t.Cleanup(server.Close)
	return server, authorizations
}

func waitAgentRunStatus(t *testing.T, router http.Handler, runID, wantStatus string) agentRunResponse {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := performRequest(router, http.MethodGet, "/api/v1/agent-runs/"+runID, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("load agent run = %d: %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data agentRunResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode agent run: %v", err)
		}
		if envelope.Data.Status == wantStatus {
			return envelope.Data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("agent run %s did not reach %s", runID, wantStatus)
	return agentRunResponse{}
}

func TestAgentRunBuiltinTextExecutorLifecycle(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	modelServer, _ := startAgentRunModelServer(t)

	// 1. Register the builtin adapter; on a verified-Windows build it must
	// come back execution ready.
	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters",
		[]byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register builtin adapter = %d: %s", registered.Code, registered.Body.String())
	}
	var adapterEnvelope struct {
		Data struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			ExecutionReady bool   `json:"execution_ready"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &adapterEnvelope); err != nil {
		t.Fatalf("decode adapter: %v", err)
	}
	if !adapterEnvelope.Data.ExecutionReady {
		t.Skip("builtin execution matrix is not verified on this platform")
	}

	// 2. Enable it; the sidecar must idempotently create the agent actor.
	enabled := performRequest(router, http.MethodPost,
		"/api/v1/agent-adapters/"+adapterEnvelope.Data.ID+"/enable", nil,
		map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable builtin adapter = %d: %s", enabled.Code, enabled.Body.String())
	}

	// 3. Create a task and assign the agent actor.
	createdTask := performRequest(router, http.MethodPost, "/api/v1/tasks",
		[]byte(`{"title":"撰写季度复盘"}`), nil)
	if createdTask.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", createdTask.Code, createdTask.Body.String())
	}
	var taskEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdTask.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	assigned := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, agentAdapterBuiltinActorID)),
		map[string]string{"If-Match": `"1"`})
	if assigned.Code != http.StatusCreated {
		t.Fatalf("assign agent actor = %d: %s", assigned.Code, assigned.Body.String())
	}

	// 4. Point the run at a local provider backed by the fake model server.
	provider := models.AIProvider{
		ID: "018f0000-0000-7000-8000-00000000b001", Name: "agent-text-local", Kind: "local",
		Protocol: "openai_chat", BaseURL: modelServer.URL + "/v1", Model: "local-test",
		Status: "ready", HealthStatus: "healthy", Version: 1,
		LastHealthAt: aiStringPtr("2026-09-12T12:00:00Z"),
		CreatedAt:    "2026-09-12T12:00:00Z", UpdatedAt: "2026-09-12T12:00:00Z",
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatalf("create local provider: %v", err)
	}

	queued := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q}`, provider.ID)), nil)
	if queued.Code != http.StatusCreated {
		t.Fatalf("create agent run = %d: %s", queued.Code, queued.Body.String())
	}
	var runEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &runEnvelope); err != nil {
		t.Fatalf("decode agent run: %v", err)
	}
	run := waitAgentRunStatus(t, router, runEnvelope.Data.ID, "succeeded")
	if run.ResultText == nil || *run.ResultText != "任务已完成：结论、要点与下一步建议。" {
		t.Fatalf("run result = %v", run.ResultText)
	}
	if run.ResultBytes == nil || *run.ResultBytes == 0 || run.Attempt != 1 {
		t.Fatalf("run metadata = %#v", run)
	}

	// 5. Retry creates a new attempt linked to the failed… succeeded parent.
	retried := performRequest(router, http.MethodPost,
		"/api/v1/agent-runs/"+run.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry agent run = %d: %s", retried.Code, retried.Body.String())
	}
	var retryEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(retried.Body.Bytes(), &retryEnvelope); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if retryEnvelope.Data.Attempt != 2 || retryEnvelope.Data.ParentRunID == nil || *retryEnvelope.Data.ParentRunID != run.ID {
		t.Fatalf("retry run metadata = %#v", retryEnvelope.Data)
	}
	waitAgentRunStatus(t, router, retryEnvelope.Data.ID, "succeeded")

	var eventCount int64
	if err := store.DB.Table("workflow_events").
		Where("aggregate_type = 'agent_run' AND aggregate_id = ? AND action = 'agent_run_succeeded'", run.ID).
		Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("succeeded events=%d err=%v", eventCount, err)
	}
}

func TestAgentRunSupportsOnlineProviderWithKey(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	modelServer, authorizations := startAgentRunModelServer(t)

	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters",
		[]byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register builtin adapter = %d", registered.Code)
	}
	var adapterEnvelope struct {
		Data struct {
			ID             string `json:"id"`
			ExecutionReady bool   `json:"execution_ready"`
		} `json:"data"`
	}
	_ = json.Unmarshal(registered.Body.Bytes(), &adapterEnvelope)
	if !adapterEnvelope.Data.ExecutionReady {
		t.Skip("builtin execution matrix is not verified on this platform")
	}
	if enabled := performRequest(router, http.MethodPost,
		"/api/v1/agent-adapters/"+adapterEnvelope.Data.ID+"/enable", nil,
		map[string]string{"If-Match": `"1"`}); enabled.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", enabled.Code, enabled.Body.String())
	}

	createdTask := performRequest(router, http.MethodPost, "/api/v1/tasks",
		[]byte(`{"title":"在线模型执行"}`), nil)
	var taskEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(createdTask.Body.Bytes(), &taskEnvelope)
	if assigned := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, agentAdapterBuiltinActorID)),
		map[string]string{"If-Match": `"1"`}); assigned.Code != http.StatusCreated {
		t.Fatalf("assign = %d: %s", assigned.Code, assigned.Body.String())
	}

	provider := models.AIProvider{
		ID: "018f0000-0000-7000-8000-00000000c001", Name: "online-model", Kind: "remote",
		Protocol: "openai_chat", BaseURL: modelServer.URL + "/v1", Model: "online-test",
		Status: "ready", HealthStatus: "healthy", HasKey: true, Version: 1,
		LastHealthAt: aiStringPtr("2026-09-12T12:00:00Z"),
		CreatedAt: "2026-09-12T12:00:00Z", UpdatedAt: "2026-09-12T12:00:00Z",
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatalf("create online provider: %v", err)
	}
	keyed := performRequest(router, http.MethodPost,
		"/api/v1/ai/providers/"+provider.ID+"/key",
		[]byte(`{"api_key":"test-key-123"}`),
		map[string]string{"If-Match": `"1"`})
	if keyed.Code != http.StatusOK {
		t.Fatalf("set provider key = %d: %s", keyed.Code, keyed.Body.String())
	}
	var keyedEnvelope struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	_ = json.Unmarshal(keyed.Body.Bytes(), &keyedEnvelope)
	if checked := performRequest(router, http.MethodPost,
		"/api/v1/ai/providers/"+provider.ID+"/health", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, keyedEnvelope.Data.Version)}); checked.Code != http.StatusOK {
		t.Fatalf("re-check provider health = %d: %s", checked.Code, checked.Body.String())
	}

	var checkProvider models.AIProvider
	if err := store.DB.First(&checkProvider, "id = ?", provider.ID).Error; err != nil {
		t.Fatalf("reload provider: %v", err)
	}
	t.Logf("provider before run: kind=%q status=%q health=%q", checkProvider.Kind, checkProvider.Status, checkProvider.HealthStatus)
	queued := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q}`, provider.ID)), nil)
	if queued.Code != http.StatusCreated {
		t.Fatalf("create run = %d: %s", queued.Code, queued.Body.String())
	}
	var runEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	_ = json.Unmarshal(queued.Body.Bytes(), &runEnvelope)
	finished := waitAgentRunStatus(t, router, runEnvelope.Data.ID, "succeeded")
	if finished.ResultText == nil || *finished.ResultText == "" {
		t.Fatalf("online run result missing: %#v", finished)
	}
	if len(*authorizations) == 0 || (*authorizations)[0] != "Bearer test-key-123" {
		t.Fatalf("executor did not send the run-scoped credential: %v", *authorizations)
	}
}

func TestAgentRunRejectsUnreadyAdapterAssignment(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters",
		[]byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register builtin adapter = %d", registered.Code)
	}
	// Force the adapter into a not-ready state so the assignment gate is
	// exercised on every platform.
	if err := store.DB.Model(&models.AgentAdapter{}).
		Where("adapter_key = ?", agentAdapterBuiltinTextKey).
		Updates(map[string]any{"execution_ready": false, "isolation_status": "unverified"}).Error; err != nil {
		t.Fatalf("force adapter not ready: %v", err)
	}
	var adapter models.AgentAdapter
	if err := store.DB.First(&adapter, "adapter_key = ?", agentAdapterBuiltinTextKey).Error; err != nil {
		t.Fatalf("load adapter: %v", err)
	}
	adapterID := adapter.ID
	if err := store.DB.Create(&models.Actor{
		ID: agentAdapterBuiltinActorID, Type: "agent", DisplayName: "本地文本执行代理",
		Status: "active", IsBuiltin: false, MetadataJSON: "{}", AgentAdapterID: &adapterID, Version: 1,
		CreatedAt: "2026-09-12T12:00:00Z", UpdatedAt: "2026-09-12T12:00:00Z",
	}).Error; err != nil {
		t.Fatalf("seed agent actor: %v", err)
	}
	createdTask := performRequest(router, http.MethodPost, "/api/v1/tasks",
		[]byte(`{"title":"不可执行分派"}`), nil)
	var taskEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdTask.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	assigned := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, agentAdapterBuiltinActorID)),
		map[string]string{"If-Match": `"1"`})
	if assigned.Code != http.StatusConflict || responseErrorCode(t, assigned.Body.Bytes()) != "ASSIGNMENT_ACTOR_NOT_EXECUTABLE" {
		t.Fatalf("agent assignment = %d: %s", assigned.Code, assigned.Body.String())
	}
	runCreated := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/agent-runs",
		[]byte(`{"provider_id":"018f0000-0000-7000-8000-00000000b002"}`), nil)
	if runCreated.Code != http.StatusConflict || responseErrorCode(t, runCreated.Body.Bytes()) != agentRunNotExecutable {
		t.Fatalf("agent run without executable chain = %d: %s", runCreated.Code, runCreated.Body.String())
	}
}
