package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAgentRunProgressRegistryIsBoundedMonotonicAndTransient(t *testing.T) {
	registry := newAgentRunProgressRegistry()
	started := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if !registry.start("run-1", started) {
		t.Fatal("start progress")
	}
	if progress, ok := registry.snapshot("run-1", started.Add(1250*time.Millisecond)); !ok ||
		progress.Phase != agentRunProgressPreparing || progress.ElapsedMS != 1250 {
		t.Fatalf("initial progress = %#v, %t", progress, ok)
	}
	if !registry.advance("run-1", agentRunProgressCallingModel) {
		t.Fatal("advance to model call")
	}
	if registry.advance("run-1", agentRunProgressPreparing) {
		t.Fatal("progress phase regressed")
	}
	if registry.advance("run-1", "unknown") {
		t.Fatal("unknown progress phase accepted")
	}
	if !registry.advance("run-1", agentRunProgressRegisteringResult) {
		t.Fatal("advance to result registration")
	}
	if !registry.advance("run-1", agentRunProgressRegisteringResult) {
		t.Fatal("idempotent current phase was rejected")
	}
	if _, ok := registry.snapshot("missing", started); ok {
		t.Fatal("missing run has progress")
	}
	if !registry.start("run-2", started) {
		t.Fatal("start second run")
	}
	if !registry.start("run-3", started) {
		t.Fatal("start third run")
	}
	registry.remove("run-1")
	if _, ok := registry.snapshot("run-1", started); ok {
		t.Fatal("removed run still has progress")
	}
}

func TestAgentRunProgressRegistryCapacityAndClockRollback(t *testing.T) {
	registry := newAgentRunProgressRegistry()
	now := time.Now()
	for index := range agentRunProgressLimit {
		if !registry.start(string(rune(index+1)), now) {
			t.Fatalf("start entry %d", index)
		}
	}
	if registry.start("overflow", now) {
		t.Fatal("progress capacity exceeded")
	}
	progress, ok := registry.snapshot(string(rune(1)), now.Add(-time.Second))
	if !ok || progress.ElapsedMS != 0 {
		t.Fatalf("clock rollback progress = %#v, %t", progress, ok)
	}
}

func TestAgentRunProgressOnlySerializesForRunningNativeViews(t *testing.T) {
	registry := newAgentRunProgressRegistry()
	now := time.Now()
	if !registry.start("run", now) {
		t.Fatal("start progress")
	}
	service := &API{agentRunProgress: registry}
	if service.agentRunProgressAt("run", "running", now.Add(time.Second)) == nil {
		t.Fatal("active native view has no progress")
	}
	if service.agentRunProgressAt("run", "succeeded", now.Add(time.Second)) != nil {
		t.Fatal("terminal run exposed progress")
	}
	if (&API{}).agentRunProgressAt("run", "running", now) != nil {
		t.Fatal("uninitialized API exposed progress")
	}
}

func TestAgentRunProgressVisibleDuringModelRequestThenRemoved(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	modelServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/models" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":[{"id":"progress-test"}]}`))
			return
		}
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		enterOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"完整交付"}}]}`))
	}))
	t.Cleanup(modelServer.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	router, store := newKnowledgeTestAPI(t)
	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters",
		[]byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register adapter = %d: %s", registered.Code, registered.Body.String())
	}
	var adapterEnvelope struct {
		Data struct {
			ID             string `json:"id"`
			ExecutionReady bool   `json:"execution_ready"`
		} `json:"data"`
	}
	if err := json.Unmarshal(registered.Body.Bytes(), &adapterEnvelope); err != nil {
		t.Fatal(err)
	}
	if !adapterEnvelope.Data.ExecutionReady {
		t.Skip("builtin execution matrix is not verified on this platform")
	}
	enabled := performRequest(router, http.MethodPost,
		"/api/v1/agent-adapters/"+adapterEnvelope.Data.ID+"/enable", nil,
		map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable adapter = %d: %s", enabled.Code, enabled.Body.String())
	}
	createdTask := performRequest(router, http.MethodPost, "/api/v1/tasks",
		[]byte(`{"title":"进度回归任务","description":"校验 Agent 运行阶段","completion_criteria":"返回完整文本","review_policy":"manual","priority":"P1","estimated_minutes":10}`), nil)
	if createdTask.Code != http.StatusCreated {
		t.Fatalf("create task = %d: %s", createdTask.Code, createdTask.Body.String())
	}
	var taskEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdTask.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatal(err)
	}
	for index, role := range []string{"assignee", "reviewer"} {
		actorID := agentAdapterBuiltinActorID
		if role == "reviewer" {
			actorID = models.BuiltinOwnerActorID
		}
		assigned := performRequest(router, http.MethodPost,
			"/api/v1/tasks/"+taskEnvelope.Data.ID+"/assignments",
			[]byte(`{"role":"`+role+`","actor_id":"`+actorID+`"}`),
			map[string]string{"If-Match": `"` + string(rune('1'+index)) + `"`})
		if assigned.Code != http.StatusCreated {
			t.Fatalf("assign %s = %d: %s", role, assigned.Code, assigned.Body.String())
		}
	}
	provider := models.AIProvider{
		ID: "018f0000-0000-7000-8000-000000000901", Name: "agent-progress-local", Kind: "local",
		Protocol: "openai_chat", BaseURL: modelServer.URL + "/v1", Model: "progress-test",
		Status: "ready", HealthStatus: "healthy", Version: 1,
		LastHealthAt: aiStringPtr("2026-09-23T12:00:00Z"),
		CreatedAt:    "2026-09-23T12:00:00Z", UpdatedAt: "2026-09-23T12:00:00Z",
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	queued := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/agent-runs",
		[]byte(`{"provider_id":"`+provider.ID+`"}`),
		map[string]string{"Idempotency-Key": "agent-run-progress"})
	if queued.Code != http.StatusCreated {
		t.Fatalf("queue run = %d: %s", queued.Code, queued.Body.String())
	}
	var runEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &runEnvelope); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not reach the held model request")
	}

	for _, path := range []string{
		"/api/v1/agent-runs/" + runEnvelope.Data.ID,
		"/api/v1/tasks/" + taskEnvelope.Data.ID + "/agent-runs",
		"/api/v1/agent-runs?page=1&page_size=20",
	} {
		response := performRequest(router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"phase":"calling_model"`) {
			t.Fatalf("progress not visible at %s: %d %s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "完整交付") || strings.Contains(response.Body.String(), "provider_key") {
			t.Fatalf("progress endpoint exposed content or credentials: %s", response.Body.String())
		}
	}

	close(release)
	completed := waitAgentRunStatus(t, router, runEnvelope.Data.ID, "succeeded")
	if completed.Progress != nil {
		t.Fatalf("terminal run still has progress: %#v", completed.Progress)
	}
}
