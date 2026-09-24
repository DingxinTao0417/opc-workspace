package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/agentrunner"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func assertAgentRunDomainCode(t *testing.T, err error, want string) {
	t.Helper()
	var domain *projectRequestError
	if !errors.As(err, &domain) || domain.code != want {
		t.Fatalf("domain error = %#v, want %s", err, want)
	}
}

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
				"finish_reason": "stop",
				"message":       map[string]any{"content": "任务已完成：结论、要点与下一步建议。"},
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
		[]byte(`{"title":"撰写季度复盘","description":"汇总事实并形成报告","completion_criteria":"包含结论与三个证据","review_policy":"manual","priority":"P1","estimated_minutes":45}`), nil)
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
	// Manual review requires an active owner reviewer before submission.
	if reviewer := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"reviewer","actor_id":%q}`, models.BuiltinOwnerActorID)),
		map[string]string{"If-Match": `"2"`}); reviewer.Code != http.StatusCreated {
		t.Fatalf("assign reviewer = %d: %s", reviewer.Code, reviewer.Body.String())
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
		[]byte(fmt.Sprintf(`{"provider_id":%q}`, provider.ID)),
		map[string]string{"Idempotency-Key": "agent-run-lifecycle"})
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
	if run.OutputDeliveryStatus != agentRunOutputSubmitted || run.OutputDeliveryErrorCode != nil ||
		run.SubmissionID == nil || run.ArtifactID == nil {
		t.Fatalf("run output delivery = %#v", run)
	}
	if run.TaskVersion < 1 || run.AssignmentAssignedAt == "" || run.ActorVersion < 1 ||
		run.AdapterVersion < 1 || run.ProviderVersion < 1 || run.ProviderConfigVersion < 1 ||
		run.ExecutionContractVersion != agentRunExecutionContractVersion {
		t.Fatalf("run frozen identity = %#v", run)
	}
	var stored models.AgentRun
	if err := store.DB.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("load stored run: %v", err)
	}
	var snapshot struct {
		TaskID             string `json:"task_id"`
		Title              string `json:"title"`
		Description        string `json:"description"`
		CompletionCriteria string `json:"completion_criteria"`
		Status             string `json:"status"`
		Kind               string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(stored.InputSnapshotJSON), &snapshot); err != nil {
		t.Fatalf("decode frozen task snapshot: %v", err)
	}
	if snapshot.TaskID != taskEnvelope.Data.ID || snapshot.Title != "撰写季度复盘" ||
		snapshot.Description != "汇总事实并形成报告" || snapshot.CompletionCriteria != "包含结论与三个证据" ||
		snapshot.Status != "todo" || snapshot.Kind != "work" {
		t.Fatalf("frozen task snapshot = %#v", snapshot)
	}
	var rawSnapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stored.InputSnapshotJSON), &rawSnapshot); err != nil {
		t.Fatalf("decode frozen task snapshot shape: %v", err)
	}
	for _, forbidden := range []string{"review_policy", "priority", "estimated_minutes", "version", "project_id"} {
		if _, exists := rawSnapshot[forbidden]; exists {
			t.Fatalf("legacy v1 snapshot unexpectedly contains %q: %s", forbidden, stored.InputSnapshotJSON)
		}
	}
	replayed := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+taskEnvelope.Data.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q}`, provider.ID)),
		map[string]string{"Idempotency-Key": "agent-run-lifecycle"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay Agent Run = %d replay=%q: %s", replayed.Code,
			replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	var replayEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayEnvelope); err != nil || replayEnvelope.Data.ID != run.ID {
		t.Fatalf("replayed Agent Run = %#v err=%v", replayEnvelope.Data, err)
	}

	// 5. A successful run submits the task for manual review. It can no longer
	// be retried because only todo/in-progress task snapshots are executable.
	retried := performRequest(router, http.MethodPost,
		"/api/v1/agent-runs/"+run.ID+"/retry", nil, nil)
	if retried.Code != http.StatusConflict || responseErrorCode(t, retried.Body.Bytes()) != agentRunIdentityChanged {
		t.Fatalf("retry submitted task = %d: %s", retried.Code, retried.Body.String())
	}

	// v0.2-C: the first successful run submitted its output through the
	// manual-review chain and moved the task to waiting_review.
	var taskRow struct {
		Status              string
		CurrentSubmissionID *string
	}
	if err := store.DB.Table("tasks").
		Select("status, current_submission_id").
		Where("id = ?", taskEnvelope.Data.ID).
		Scan(&taskRow).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if taskRow.Status != "waiting_review" || taskRow.CurrentSubmissionID == nil {
		t.Fatalf("task after run = %#v, want waiting_review with submission", taskRow)
	}
	if *taskRow.CurrentSubmissionID != *run.SubmissionID {
		t.Fatalf("Task submission=%s Run submission=%v", *taskRow.CurrentSubmissionID, run.SubmissionID)
	}
	var artifact models.TaskArtifact
	if err := store.DB.First(&artifact, "submission_id = ?", *taskRow.CurrentSubmissionID).Error; err != nil ||
		artifact.StorageKind != "text" || artifact.ProducedByActorID != agentAdapterBuiltinActorID ||
		artifact.ContentText == nil || *artifact.ContentText != *run.ResultText {
		t.Fatalf("submitted artifact = %#v err=%v", artifact, err)
	}
	if artifact.ID != *run.ArtifactID || artifact.RecordedByActorID != models.BuiltinSystemActorID {
		t.Fatalf("Run-linked artifact = %#v", artifact)
	}

	var eventCount int64
	if err := store.DB.Table("workflow_events").
		Where("aggregate_type = 'agent_run' AND aggregate_id = ? AND action = 'agent_run_succeeded'", run.ID).
		Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("succeeded events=%d err=%v", eventCount, err)
	}
	if err := store.DB.Table("workflow_events").
		Where("action = 'task_output_submitted' AND submission_id = ? AND agent_run_id = ?", *run.SubmissionID, run.ID).
		Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("shared output events=%d err=%v", eventCount, err)
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
		CreatedAt:    "2026-09-12T12:00:00Z", UpdatedAt: "2026-09-12T12:00:00Z",
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

func TestPrepareAgentRunFreezesIdentityAndDatabaseArbitratesConcurrency(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	const (
		taskID       = "018f0000-0000-7000-8000-00000000d001"
		adapterID    = "018f0000-0000-7000-8000-00000000d002"
		actorID      = "018f0000-0000-7000-8000-00000000d003"
		assignmentID = "018f0000-0000-7000-8000-00000000d004"
		providerID   = "018f0000-0000-7000-8000-00000000d005"
		now          = "2026-09-18T12:00:00Z"
	)
	lastHealth := now
	estimatedMinutes := 30
	adapter := models.AgentAdapter{
		ID: adapterID, AdapterKey: "domain-freeze-adapter", Kind: "builtin", DisplayName: "领域冻结代理",
		ExecutableRef: "builtin:domain-freeze", ManifestJSON: "{}", ProtocolVersion: "opc-agent-pipe-v1",
		Status: "enabled", HealthStatus: "healthy", IsolationStatus: "verified", ExecutionReady: true,
		LastHealthAt: &lastHealth, Version: 3, CreatedAt: now, UpdatedAt: now,
	}
	actor := models.Actor{
		ID: actorID, Type: "agent", DisplayName: "领域代理", Status: "active", MetadataJSON: "{}",
		AgentAdapterID: &adapter.ID, Version: 4, CreatedAt: now, UpdatedAt: now,
	}
	task := models.Task{
		ID: taskID, Title: "冻结完整任务事实", Description: "只依据冻结事实执行", Kind: "work",
		Status: "in_progress", ReviewPolicy: "manual", Priority: "P0",
		CompletionCriteria: "必须包含可复核结果", EstimatedMinutes: &estimatedMinutes,
		Version: 7, CreatedAt: now, UpdatedAt: now,
	}
	assignment := models.TaskAssignment{
		ID: assignmentID, TaskID: taskID, ActorID: actorID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now,
	}
	provider := models.AIProvider{
		ID: providerID, Name: "domain-freeze-provider", Kind: "remote", Protocol: "openai_chat",
		BaseURL: "https://example.invalid/v1", Model: "frozen-model", Status: "ready",
		HealthStatus: "healthy", HasKey: true, LastHealthAt: &lastHealth,
		Version: 5, ConfigVersion: 6, CreatedAt: now, UpdatedAt: now,
	}
	for _, fixture := range []struct {
		name  string
		value any
	}{
		{"adapter", &adapter}, {"actor", &actor}, {"task", &task},
		{"assignment", &assignment}, {"provider", &provider},
	} {
		if err := store.DB.Create(fixture.value).Error; err != nil {
			t.Fatalf("create %s: %v", fixture.name, err)
		}
	}

	prepared, err := prepareAgentRun(store.DB, prepareAgentRunInput{TaskID: taskID, ProviderID: providerID})
	if err != nil {
		t.Fatalf("prepareAgentRun() error = %v", err)
	}
	if prepared.Identity != (agentRunIdentity{
		TaskVersion: 7, AssignmentID: assignmentID, AssignmentAssignedAt: now,
		ActorID: actorID, ActorVersion: 4, AdapterID: adapterID, AdapterVersion: 3,
		ProviderID: providerID, ProviderVersion: 5, ProviderConfigVersion: 6,
	}) || prepared.Attempt != 1 {
		t.Fatalf("prepared identity = %#v attempt=%d", prepared.Identity, prepared.Attempt)
	}
	if prepared.Snapshot.CompletionCriteria != task.CompletionCriteria ||
		prepared.Snapshot.ReviewPolicy != "manual" || prepared.Snapshot.Priority != "P0" ||
		prepared.Snapshot.Version != 7 || prepared.Snapshot.EstimatedMinutes == nil ||
		*prepared.Snapshot.EstimatedMinutes != 30 {
		t.Fatalf("prepared snapshot = %#v", prepared.Snapshot)
	}

	var run models.AgentRun
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		run, createErr = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "domain-freeze", now,
		)
		return createErr
	}); err != nil {
		t.Fatalf("createAgentRunInTransaction() error = %v", err)
	}
	if run.ExecutionContractVersion != agentRunExecutionContractVersion || run.TaskVersion != 7 ||
		run.ProviderConfigVersion != 6 || run.AssignmentAssignedAt != now {
		t.Fatalf("stored run identity = %#v", run)
	}

	activePrepared := prepared
	activePrepared.Attempt = 2
	err = store.DB.Transaction(func(tx *gorm.DB) error {
		_, createErr := createAgentRunInTransaction(
			tx, activePrepared, models.BuiltinOwnerActorID, "active-conflict", now,
		)
		return createErr
	})
	assertAgentRunDomainCode(t, err, agentRunAlreadyActive)

	if err := store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": "running", "started_at": now,
	}).Error; err != nil {
		t.Fatalf("start prepared run: %v", err)
	}
	(&API{db: store.DB}).finalizeAgentRun(run, "", agentrunner.CodeCancelled, now)
	var cancelled models.AgentRun
	if err := store.DB.First(&cancelled, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("load cancelled run: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.ErrorCode != nil || cancelled.CompletedAt == nil {
		t.Fatalf("cancelled run = %#v", cancelled)
	}
	var cancelledEvents int64
	if err := store.DB.Table("workflow_events").Where(
		"aggregate_type = 'agent_run' AND aggregate_id = ? AND action = 'agent_run_cancelled'", run.ID,
	).Count(&cancelledEvents).Error; err != nil || cancelledEvents != 1 {
		t.Fatalf("cancelled events=%d err=%v", cancelledEvents, err)
	}
	err = store.DB.Transaction(func(tx *gorm.DB) error {
		_, createErr := createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "attempt-conflict", now,
		)
		return createErr
	})
	assertAgentRunDomainCode(t, err, agentRunIdentityChanged)

	next, err := prepareAgentRun(store.DB, prepareAgentRunInput{TaskID: taskID, ProviderID: providerID})
	if err != nil || next.Attempt != 2 {
		t.Fatalf("prepare next attempt = %#v err=%v", next, err)
	}
	var timedOut models.AgentRun
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		timedOut, createErr = createAgentRunInTransaction(
			tx, next, models.BuiltinOwnerActorID, "timeout-control", now,
		)
		return createErr
	}); err != nil {
		t.Fatalf("create timeout control run: %v", err)
	}
	if err := store.DB.Model(&models.AgentRun{}).Where("id = ?", timedOut.ID).
		Updates(map[string]any{"status": "running", "started_at": now}).Error; err != nil {
		t.Fatalf("start timeout control run: %v", err)
	}
	(&API{db: store.DB}).finalizeAgentRun(timedOut, "", agentrunner.CodeTimedOut, now)
	if err := store.DB.First(&timedOut, "id = ?", timedOut.ID).Error; err != nil {
		t.Fatalf("reload timeout control run: %v", err)
	}
	if timedOut.Status != "failed" || timedOut.ErrorCode == nil || *timedOut.ErrorCode != agentrunner.CodeTimedOut {
		t.Fatalf("timeout control run = %#v", timedOut)
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+timedOut.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry identity-safe run = %d: %s", retried.Code, retried.Body.String())
	}
	var retryEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(retried.Body.Bytes(), &retryEnvelope); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if retryEnvelope.Data.Attempt != 3 || retryEnvelope.Data.ParentRunID == nil ||
		*retryEnvelope.Data.ParentRunID != timedOut.ID || retryEnvelope.Data.TaskVersion != run.TaskVersion ||
		retryEnvelope.Data.ProviderConfigVersion != run.ProviderConfigVersion {
		t.Fatalf("identity-safe retry = %#v", retryEnvelope.Data)
	}
	waitAgentRunStatus(t, router, retryEnvelope.Data.ID, "failed")
}

func TestPrepareAgentRunRejectsProviderAndIdentityDrift(t *testing.T) {
	_, store := newKnowledgeTestAPI(t)
	const (
		taskID       = "018f0000-0000-7000-8000-00000000e001"
		adapterID    = "018f0000-0000-7000-8000-00000000e002"
		actorID      = "018f0000-0000-7000-8000-00000000e003"
		assignmentID = "018f0000-0000-7000-8000-00000000e004"
		providerID   = "018f0000-0000-7000-8000-00000000e005"
		now          = "2026-09-18T12:00:00Z"
	)
	lastHealth := now
	adapter := models.AgentAdapter{
		ID: adapterID, AdapterKey: "identity-drift-adapter", Kind: "builtin", DisplayName: "漂移检测代理",
		ExecutableRef: "builtin:identity-drift", ManifestJSON: "{}", ProtocolVersion: agentexec.ProtocolVersion,
		Status: "enabled", HealthStatus: "healthy", IsolationStatus: "verified", ExecutionReady: true,
		LastHealthAt: &lastHealth, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	actor := models.Actor{
		ID: actorID, Type: "agent", DisplayName: "漂移代理", Status: "active", MetadataJSON: "{}",
		AgentAdapterID: &adapter.ID, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	task := models.Task{
		ID: taskID, Title: "检测执行身份漂移", Kind: "work", Status: "todo", ReviewPolicy: "manual",
		Priority: "P2", CompletionCriteria: "身份必须稳定", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	assignment := models.TaskAssignment{
		ID: assignmentID, TaskID: taskID, ActorID: actorID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now,
	}
	provider := models.AIProvider{
		ID: providerID, Name: "identity-drift-provider", Kind: "remote", Protocol: "openai_chat",
		BaseURL: "https://example.invalid/v1", Model: "drift-model", Status: "ready",
		HealthStatus: "healthy", HasKey: false, LastHealthAt: &lastHealth,
		Version: 1, ConfigVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, fixture := range []struct {
		name  string
		value any
	}{
		{"adapter", &adapter}, {"actor", &actor}, {"task", &task},
		{"assignment", &assignment}, {"provider", &provider},
	} {
		if err := store.DB.Create(fixture.value).Error; err != nil {
			t.Fatalf("create %s: %v", fixture.name, err)
		}
	}
	_, err := prepareAgentRun(store.DB, prepareAgentRunInput{TaskID: taskID, ProviderID: providerID})
	assertAgentRunDomainCode(t, err, "AGENT_PROVIDER_INVALID")

	if err := store.DB.Model(&models.AIProvider{}).Where("id = ?", providerID).
		Updates(map[string]any{"has_key": true, "version": 2, "config_version": 2}).Error; err != nil {
		t.Fatalf("make provider executable: %v", err)
	}
	prepared, err := prepareAgentRun(store.DB, prepareAgentRunInput{TaskID: taskID, ProviderID: providerID})
	if err != nil {
		t.Fatalf("prepare executable run: %v", err)
	}
	var run models.AgentRun
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		run, createErr = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "identity-drift", now,
		)
		return createErr
	}); err != nil {
		t.Fatalf("create frozen run: %v", err)
	}
	if err := store.DB.Model(&models.Actor{}).Where("id = ?", actorID).
		Updates(map[string]any{"notes": "changed", "version": 2, "updated_at": "2026-09-18T12:01:00Z"}).Error; err != nil {
		t.Fatalf("drift actor identity: %v", err)
	}
	_, err = validateFrozenAgentRunIdentity(store.DB, run)
	assertAgentRunDomainCode(t, err, agentRunIdentityChanged)
	api := &API{
		db: store.DB, options: Options{Now: func() time.Time {
			return time.Date(2026, 9, 18, 12, 2, 0, 0, time.UTC)
		}}, maintenance: &sync.RWMutex{},
	}
	api.executeAgentRun(context.Background(), run.ID)
	if err := store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
		t.Fatalf("reload drift-rejected run: %v", err)
	}
	if run.Status != "failed" || run.ErrorCode == nil || *run.ErrorCode != agentRunIdentityChanged ||
		run.StartedAt == nil || run.CompletedAt == nil {
		t.Fatalf("drift-rejected run = %#v", run)
	}
	_, err = prepareAgentRun(store.DB, prepareAgentRunInput{
		TaskID: taskID, ProviderID: providerID, Expected: &prepared.Identity,
	})
	assertAgentRunDomainCode(t, err, agentRunIdentityChanged)
}
