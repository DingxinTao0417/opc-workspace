package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiAgentRunFixture struct {
	Router     *Router
	Store      *database.Store
	Service    *API
	Tool       harness.Tool
	Generation models.AIGeneration
	Task       models.Task
	Provider   models.AIProvider
	Actor      models.Actor
}

func newAIAgentRunTestAPI(t *testing.T) (*Router, *database.Store) {
	t.Helper()
	root := t.TempDir()
	store, err := openAPITestDatabase(filepath.Join(root, "ai-agent-run.db"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "ai-agent-run-test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), KeyStore: keystore.NewMemoryStore(),
		ArtifactDir: filepath.Join(root, "artifacts"),
		Now:         func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router, store
}

func newAIAgentRunFixture(t *testing.T) aiAgentRunFixture {
	t.Helper()
	router, store := newAIAgentRunTestAPI(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	nowText := now.Format(time.RFC3339Nano)
	lastHealth := nowText
	adapter := models.AgentAdapter{
		ID: uuid.NewString(), AdapterKey: "test-builtin-agent", Kind: "builtin", DisplayName: "Safe executor",
		ExecutableRef: "builtin:local-text-v1", ManifestJSON: `{"secret":"MUST_NOT_LEAK"}`,
		ProtocolVersion: agentexec.ProtocolVersion, Status: "enabled", HealthStatus: "healthy",
		IsolationStatus: "verified", ExecutionReady: true, LastHealthAt: &lastHealth,
		Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&adapter).Error; err != nil {
		t.Fatal(err)
	}
	adapterID := adapter.ID
	actor := models.Actor{
		ID: uuid.NewString(), Type: "agent", DisplayName: "Research Agent", Status: "active",
		Notes: "PRIVATE ACTOR NOTES", MetadataJSON: `{"secret":"MUST_NOT_LEAK"}`,
		AgentAdapterID: &adapterID, Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	task := models.Task{
		ID: uuid.NewString(), Title: "Prepare an audited report", Description: "Use only the frozen task facts.",
		CompletionCriteria: "Return a reviewable report.", Kind: "work", Status: "todo", ReviewPolicy: "manual",
		Priority: "P1", Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: task.ID, ActorID: actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: nowText, Reason: "test",
	}
	if err := store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	provider := models.AIProvider{
		ID: uuid.NewString(), Name: "Execution Provider", Kind: "local", Protocol: "openai_chat",
		BaseURL: "http://127.0.0.1:1/v1", Model: "safe-model", Status: "ready", HealthStatus: "healthy",
		LastHealthAt: &lastHealth, Version: 3, ConfigVersion: 5, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	session := models.AISession{
		ID: uuid.NewString(), Title: "Agent execution", Persist: true, Version: 1,
		CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{
		ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming",
		CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{
		db: store.DB, maintenance: &sync.RWMutex{}, agentRunCancels: map[string]context.CancelFunc{},
		artifactStore: router.artifactStore,
		options:       Options{Now: func() time.Time { return now }},
	}
	grant := &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"work", "outputs", "actions", "agent_execution"},
	}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("workspace_propose missing")
	}
	return aiAgentRunFixture{
		Router: router, Store: store, Service: service, Tool: tool,
		Generation: generation, Task: task, Provider: provider, Actor: actor,
	}
}

func aiAgentRunActionJSON(f aiAgentRunFixture) string {
	return fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d}}`,
		f.Task.ID, f.Task.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion)
}

func aiAgentRunFileActionJSON(f aiAgentRunFixture, candidateIDs []string, outputKind string) string {
	encodedCandidates, _ := json.Marshal(candidateIDs)
	return fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":%d,"changes":{"provider_id":%q,"expected_provider_version":%d,"expected_provider_config_version":%d,"input_file_candidate_ids":%s,"output_kind":%q}}`,
		f.Task.ID, f.Task.Version, f.Provider.ID, f.Provider.Version, f.Provider.ConfigVersion,
		encodedCandidates, outputKind)
}

func aiAgentFileTools(t *testing.T, f aiAgentRunFixture) (harness.Tool, harness.Tool) {
	t.Helper()
	grant := &aiWorkspaceGrant{
		ProviderVersion: f.Provider.Version,
		Scopes:          []string{"work", "outputs", "actions", "agent_execution", "agent_files"},
	}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, grant, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, ok := registry.Get("workspace_agent_execution")
	if !ok {
		t.Fatal("workspace_agent_execution missing")
	}
	proposal, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatal("workspace_propose missing")
	}
	return execution, proposal
}

func seedAIAgentRunTaskFile(t *testing.T, f aiAgentRunFixture, name, body string) models.TaskArtifact {
	t.Helper()
	artifactID := uuid.NewString()
	staged, err := f.Service.artifactStore.stageMultipartFileWithLimit(
		strings.NewReader(body), artifactID, int64(agentexec.MaxFileInputBytes),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Service.artifactStore.commitStagedFile(staged); err != nil {
		f.Service.artifactStore.discardStagedFile(staged)
		t.Fatal(err)
	}
	checkedAt := f.Generation.CreatedAt
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: f.Task.ID, Sequence: 1, Status: "pending_review", Origin: "manual",
		Summary: "controlled input fixture", SubmittedByActorID: models.BuiltinOwnerActorID,
		SubmittedAt: f.Generation.CreatedAt,
	}
	if err := f.Store.DB.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
	relativePath, mimeType, sizeBytes, digest := staged.relativePath, staged.mimeType, staged.sizeBytes, staged.sha256
	artifact := models.TaskArtifact{
		ID: artifactID, TaskID: f.Task.ID, SubmissionID: submission.ID, Position: 1,
		StorageKind: "file", Name: name, RelativePath: &relativePath, MimeType: &mimeType,
		SizeBytes: &sizeBytes, SHA256: &digest, ProducedByActorID: models.BuiltinOwnerActorID,
		RecordedByActorID: models.BuiltinOwnerActorID, IntegrityStatus: "verified",
		IntegrityCheckedAt: &checkedAt, CreatedAt: f.Generation.CreatedAt,
	}
	if err := f.Store.DB.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	return artifact
}

func listedAIAgentFileCandidate(t *testing.T, tool harness.Tool, taskID string) (aiAgentExecutionFileCandidate, string) {
	t.Helper()
	result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q}`, taskID)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Eligible       bool                            `json:"eligible"`
		FileCandidates []aiAgentExecutionFileCandidate `json:"file_candidates"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Eligible || len(envelope.FileCandidates) != 1 {
		t.Fatalf("unexpected Agent file eligibility: %s", result)
	}
	return envelope.FileCandidates[0], result
}

func cloneAIAgentRunProposalPreview(
	t *testing.T,
	f aiAgentRunFixture,
	base models.AIActionProposal,
	mutate func(map[string]any),
) models.AIActionProposal {
	t.Helper()
	var preview map[string]any
	if err := json.Unmarshal([]byte(base.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	mutate(preview)
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	clone := base
	clone.ID = uuid.NewString()
	clone.Fingerprint = sha256Hex([]byte(clone.ID))
	clone.PreviewJSON = string(encoded)
	clone.Status = "pending"
	clone.ResultID = nil
	clone.ResultVersion = nil
	clone.DecidedAt = nil
	if err := f.Store.DB.Create(&clone).Error; err != nil {
		t.Fatal(err)
	}
	return clone
}

func setAIAgentRunV1LeavesDevice(t *testing.T, preview map[string]any, value any) {
	t.Helper()
	provider := aiAgentRunV1ProviderMap(t, preview)
	provider["leaves_device"] = value
}

func aiAgentRunV1ProviderMap(t *testing.T, preview map[string]any) map[string]any {
	t.Helper()
	run, ok := preview["agent_run_start"].(map[string]any)
	if !ok {
		t.Fatal("agent_run_start missing")
	}
	provider, ok := run["provider"].(map[string]any)
	if !ok {
		t.Fatal("provider missing")
	}
	return provider
}

func schemaActionEnum(t *testing.T, tool harness.Tool) map[string]bool {
	t.Helper()
	schemaTool, ok := tool.(interface{ InputSchema() json.RawMessage })
	if !ok {
		t.Fatal("tool does not expose an input schema")
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaTool.InputSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	result := map[string]bool{}
	for _, action := range schema.Properties["action"].Enum {
		result[action] = true
	}
	return result
}

func schemaJSON(t *testing.T, tool harness.Tool) string {
	t.Helper()
	schemaTool, ok := tool.(interface{ InputSchema() json.RawMessage })
	if !ok {
		t.Fatal("tool does not expose an input schema")
	}
	return string(schemaTool.InputSchema())
}

func TestAIAgentExecutionGrantIsIndependentPersistentAndFailClosed(t *testing.T) {
	f := newAIAgentRunFixture(t)
	for _, scopes := range [][]string{
		{"agent_execution"},
		{"work", "actions", "agent_execution"},
		{"work", "outputs", "agent_execution"},
	} {
		if err := validateAIWorkspaceGrant(&f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}); err == nil {
			t.Fatalf("accepted incomplete execution grant %v", scopes)
		}
	}
	if err := validateAIWorkspaceGrant(&f.Provider, &aiWorkspaceGrant{
		ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"},
	}); err != nil {
		t.Fatal(err)
	}

	baseGrant := &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions"}}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, baseGrant, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("workspace_agent_execution"); ok {
		t.Fatal("generic work/actions exposed Agent execution eligibility")
	}
	proposal, _ := registry.Get("workspace_propose")
	if schemaActionEnum(t, proposal)["agent_run.start"] {
		t.Fatal("generic work/actions exposed agent_run.start")
	}
	if _, err := proposal.Execute(context.Background(), []byte(aiAgentRunActionJSON(f))); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("generic proposal bypassed execution scope: %v", err)
	}

	fullGrant := &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err = f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, fullGrant, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("workspace_agent_execution"); !ok {
		t.Fatal("explicit execution eligibility tool missing")
	}
	proposal, _ = registry.Get("workspace_propose")
	if !schemaActionEnum(t, proposal)["agent_run.start"] {
		t.Fatal("explicit execution action missing")
	}
	registry, err = f.Service.aiChatToolRegistry(f.Generation.SessionID, false, &f.Provider, fullGrant, f.Generation.ID)
	if err == nil {
		if _, ok := registry.Get("workspace_agent_execution"); ok {
			t.Fatal("ephemeral conversation exposed execution eligibility")
		}
		if _, ok := registry.Get("workspace_propose"); ok {
			t.Fatal("ephemeral conversation exposed proposal tool")
		}
		t.Fatal("ephemeral registry accepted action workspace capabilities")
	}

	ephemeral := models.AISession{
		ID: uuid.NewString(), Title: "ephemeral", Persist: false, Version: 1,
		CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt,
	}
	if err := f.Store.DB.Create(&ephemeral).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_id": f.Provider.ID, "session_id": ephemeral.ID, "message": "start agent",
		"workspace": fullGrant,
	})
	assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil), 422, "AI_ACTION_PERSIST_REQUIRED")
}

func TestAIAgentFileGrantSchemaAndGenerationAllowlistFailClosed(t *testing.T) {
	f := newAIAgentRunFixture(t)
	for _, scopes := range [][]string{
		{"agent_files"},
		{"work", "outputs", "actions", "agent_files"},
		{"work", "outputs", "actions", "agent_execution"},
	} {
		err := validateAIWorkspaceGrant(&f.Provider, &aiWorkspaceGrant{
			ProviderVersion: f.Provider.Version, Scopes: scopes,
		})
		if scopes[len(scopes)-1] == "agent_files" && err == nil {
			t.Fatalf("accepted incomplete Agent file grant %v", scopes)
		}
	}
	fullGrant := &aiWorkspaceGrant{
		ProviderVersion: f.Provider.Version,
		Scopes:          []string{"work", "outputs", "actions", "agent_execution", "agent_files"},
	}
	if err := validateAIWorkspaceGrant(&f.Provider, fullGrant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, false, &f.Provider, fullGrant, f.Generation.ID); err == nil {
		t.Fatal("ephemeral registry accepted one-message Agent file capability")
	}

	baseSchema := schemaJSON(t, f.Tool)
	if strings.Contains(baseSchema, "input_file_candidate_ids") || strings.Contains(baseSchema, "output_kind") {
		t.Fatalf("base Agent execution schema exposed file fields: %s", baseSchema)
	}
	unauthorized := aiAgentRunFileActionJSON(f, []string{uuid.NewString()}, agentexec.ResultTypeFile)
	if _, err := f.Tool.Execute(context.Background(), []byte(unauthorized)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("base Agent execution accepted file fields: %v", err)
	}

	artifact := seedAIAgentRunTaskFile(t, f, "private-brief.md", "TOP SECRET CONTROLLED BODY")
	execution, proposal := aiAgentFileTools(t, f)
	fileSchema := schemaJSON(t, proposal)
	if !strings.Contains(fileSchema, "input_file_candidate_ids") || !strings.Contains(fileSchema, "output_kind") {
		t.Fatalf("Agent file grant omitted file proposal fields: %s", fileSchema)
	}
	candidate, result := listedAIAgentFileCandidate(t, execution, f.Task.ID)
	repeatedCandidate, repeatedResult := listedAIAgentFileCandidate(t, execution, f.Task.ID)
	if repeatedCandidate.CandidateID != candidate.CandidateID || repeatedResult == "" {
		t.Fatalf("unchanged candidate did not reuse its opaque ID: first=%#v second=%#v", candidate, repeatedCandidate)
	}
	if candidate.CandidateID == artifact.ID || candidate.SourceKind != agentexec.FileSourceTaskArtifact ||
		candidate.Name != artifact.Name || candidate.SizeBytes != *artifact.SizeBytes {
		t.Fatalf("unsafe/incorrect opaque candidate: %#v", candidate)
	}
	for _, forbidden := range []string{artifact.ID, "TOP SECRET CONTROLLED BODY", *artifact.SHA256, *artifact.RelativePath, "sha256", "relative_path", "content"} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("eligibility leaked %q: %s", forbidden, result)
		}
	}

	for label, action := range map[string]string{
		"guessed":    aiAgentRunFileActionJSON(f, []string{uuid.NewString()}, agentexec.ResultTypeText),
		"cross_task": strings.Replace(aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeText), f.Task.ID, uuid.NewString(), 1),
	} {
		if _, err := proposal.Execute(context.Background(), []byte(action)); err == nil {
			t.Fatalf("%s opaque candidate bypassed allowlist", label)
		}
	}
	_, secondProposal := aiAgentFileTools(t, f)
	if _, err := secondProposal.Execute(context.Background(), []byte(aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeText))); err == nil {
		t.Fatal("candidate from another tool registry was accepted")
	}

	row := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeFile))
	for _, expected := range []string{
		`"execution_contract_version":2`, artifact.ID, artifact.Name, *artifact.SHA256,
		`"input_file_total_bytes":26`, `"input_files_leave_device":false`,
		`"type":"file"`, `"name":"agent-run-attempt-1.md"`, `"mime":"text/markdown"`,
	} {
		if !strings.Contains(row.PreviewJSON, expected) {
			t.Fatalf("immutable v2 preview missing %q: %s", expected, row.PreviewJSON)
		}
	}
	for _, forbidden := range []string{artifact.ID, artifact.Name, *artifact.SHA256, "TOP SECRET CONTROLLED BODY", *artifact.RelativePath} {
		if strings.Contains(row.ActionJSON, forbidden) {
			t.Fatalf("durable model action leaked real file data %q: %s", forbidden, row.ActionJSON)
		}
	}
}

func TestAIAgentFileDefaultFieldsNormalizeToLegacyProposalIdentity(t *testing.T) {
	f := newAIAgentRunFixture(t)
	_, proposal := aiAgentFileTools(t, f)
	legacy := proposeTestAction(t, f.Store, proposal, aiAgentRunActionJSON(f))
	explicitDefaults := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{}, agentexec.ResultTypeText))
	if explicitDefaults.ID != legacy.ID || explicitDefaults.Fingerprint != legacy.Fingerprint ||
		explicitDefaults.ActionJSON != legacy.ActionJSON || explicitDefaults.PreviewJSON != legacy.PreviewJSON {
		t.Fatalf("explicit Agent file defaults did not reuse legacy proposal: legacy=%#v defaults=%#v", legacy, explicitDefaults)
	}
	if strings.Contains(legacy.ActionJSON, "input_file_candidate_ids") || strings.Contains(legacy.ActionJSON, "output_kind") ||
		strings.Contains(legacy.PreviewJSON, `"execution_contract_version":2`) {
		t.Fatalf("default file fields did not normalize to v1: action=%s preview=%s", legacy.ActionJSON, legacy.PreviewJSON)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE generation_id=?", 1, f.Generation.ID)
}

func TestAIAgentOpaqueCandidateIDChangesOnlyWithFrozenMetadata(t *testing.T) {
	run := newAIAgentExecutionRun(uuid.NewString())
	taskID := uuid.NewString()
	realID := uuid.NewString()
	base := agentRunFileCandidate{
		SourceKind: agentexec.FileSourceTaskArtifact, ID: realID, Name: "brief.md",
		MIME: "text/markdown", SizeBytes: 12, SHA256: strings.Repeat("a", 64),
		Eligible: true, CreatedAt: "2026-09-18T12:00:00Z",
	}
	first, _ := run.publishFileCandidates(taskID, []agentRunFileCandidate{base})
	second, _ := run.publishFileCandidates(taskID, []agentRunFileCandidate{base})
	changed := base
	changed.SHA256 = strings.Repeat("b", 64)
	third, _ := run.publishFileCandidates(taskID, []agentRunFileCandidate{changed})
	changedCreatedAt := base
	changedCreatedAt.CreatedAt = "2026-09-18T12:00:01Z"
	fourth, _ := run.publishFileCandidates(taskID, []agentRunFileCandidate{changedCreatedAt})
	if len(first) != 1 || len(second) != 1 || len(third) != 1 || len(fourth) != 1 ||
		first[0].CandidateID != second[0].CandidateID || third[0].CandidateID == first[0].CandidateID ||
		fourth[0].CandidateID == first[0].CandidateID || fourth[0].CandidateID == third[0].CandidateID {
		t.Fatalf("opaque candidate stability mismatch: first=%#v second=%#v hash_changed=%#v created_changed=%#v", first, second, third, fourth)
	}
	all := append(append(append(first, second...), third...), fourth...)
	for _, item := range all {
		encoded, _ := json.Marshal(item)
		for _, forbidden := range []string{realID, base.SHA256, changed.SHA256, "relative_path", "content"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("opaque candidate leaked %q: %s", forbidden, encoded)
			}
		}
	}
}

func TestAIAgentFileProposalRequiresIndependentConsentAndCreatesFrozenV2Run(t *testing.T) {
	f := newAIAgentRunFixture(t)
	artifact := seedAIAgentRunTaskFile(t, f, "execution-input.md", "controlled input for execution")
	execution, proposal := aiAgentFileTools(t, f)
	candidate, _ := listedAIAgentFileCandidate(t, execution, f.Task.ID)
	row := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeFile))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	withoutFileConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, withoutFileConsent, nil), 422, "AGENT_FILE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	rejectWithConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject","confirm_agent_files":true}`, row.Fingerprint))
	assertAPIError(t, performRequest(f.Router, http.MethodPost, path, rejectWithConsent, nil), 422, "AI_ACTION_DECISION_INVALID")

	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_files":true}`, row.Fingerprint))
	response := performRequest(f.Router, http.MethodPost, path, body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm v2 Agent run=%d %s", response.Code, response.Body.String())
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "task_id = ?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.ExecutionContractVersion != agentRunExecutionContractVersionV2 {
		t.Fatalf("execution_contract_version=%d", run.ExecutionContractVersion)
	}
	snapshot, err := parseAgentRunV2Snapshot(run.InputSnapshotJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 1 || snapshot.Files[0].ID != artifact.ID ||
		snapshot.Files[0].SHA256 != *artifact.SHA256 || snapshot.OutputContract.Type != agentexec.ResultTypeFile ||
		snapshot.OutputContract.Name != "agent-run-attempt-1.md" || snapshot.OutputContract.MIME != "text/markdown" {
		t.Fatalf("unexpected frozen v2 snapshot: %#v", snapshot)
	}
	receipts, err := f.Service.aiActionReceipts(context.Background(), f.Generation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{artifact.Name, artifact.ID, *artifact.SHA256, *artifact.RelativePath, "controlled input for execution", candidate.CandidateID} {
		if strings.Contains(receipts, forbidden) {
			t.Fatalf("Agent receipt leaked %q: %s", forbidden, receipts)
		}
	}
}

func TestAIAgentFileConfirmationRejectsControlledFileDrift(t *testing.T) {
	f := newAIAgentRunFixture(t)
	artifact := seedAIAgentRunTaskFile(t, f, "drift-input.md", "original controlled bytes")
	execution, proposal := aiAgentFileTools(t, f)
	candidate, _ := listedAIAgentFileCandidate(t, execution, f.Task.ID)
	row := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeText))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	objectPath, err := f.Service.artifactStore.resolveObject(*artifact.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("changed controlled bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_files":true}`, row.Fingerprint))
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), agentRunFileIntegrityInvalidCode) {
		t.Fatalf("file drift response=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAIAgentFileConsentCannotBeCarriedByV1OrOutputOnlyV2(t *testing.T) {
	t.Run("v1 rejects file consent", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_files":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 422, "AI_ACTION_DECISION_INVALID")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})

	t.Run("output-only v2 needs execution consent but forbids file consent", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		_, proposal := aiAgentFileTools(t, f)
		row := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{}, agentexec.ResultTypeFile))
		if !strings.Contains(row.PreviewJSON, `"execution_contract_version":2`) ||
			!strings.Contains(row.PreviewJSON, `"output_contract":{"type":"file"`) ||
			strings.Contains(row.PreviewJSON, `"input_files"`) {
			t.Fatalf("unexpected output-only v2 preview: %s", row.PreviewJSON)
		}
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/ai/actions/" + row.ID + "/decision"
		carried := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_files":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, path, carried, nil), 422, "AI_ACTION_DECISION_INVALID")
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		response := performRequest(f.Router, http.MethodPost, path, body, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("output-only v2 confirm=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE execution_contract_version=2", 1)
	})
}

func TestAIAgentFilePreviewExplicitlyMarksRemoteProvider(t *testing.T) {
	f := newAIAgentRunFixture(t)
	f.Provider.Kind = "remote"
	f.Provider.BaseURL = "https://example.com/v1"
	f.Provider.HasKey = true
	f.Provider.Version++
	f.Provider.ConfigVersion++
	if err := f.Store.DB.Model(&models.AIProvider{}).Where("id = ?", f.Provider.ID).Updates(map[string]any{
		"kind": "remote", "base_url": f.Provider.BaseURL, "has_key": true, "version": f.Provider.Version,
		"config_version": f.Provider.ConfigVersion, "updated_at": f.Generation.UpdatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	seedAIAgentRunTaskFile(t, f, "remote-input.md", "leaves this device after confirmation")
	execution, proposal := aiAgentFileTools(t, f)
	candidate, _ := listedAIAgentFileCandidate(t, execution, f.Task.ID)
	row := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{candidate.CandidateID}, agentexec.ResultTypeText))
	if !strings.Contains(row.PreviewJSON, `"kind":"remote"`) ||
		!strings.Contains(row.PreviewJSON, `"leaves_device":true`) ||
		!strings.Contains(row.PreviewJSON, `"input_files_leave_device":true`) {
		t.Fatalf("remote Provider preview did not expose off-device boundary: %s", row.PreviewJSON)
	}
	outputOnly := proposeTestAction(t, f.Store, proposal, aiAgentRunFileActionJSON(f, []string{}, agentexec.ResultTypeFile))
	if !strings.Contains(outputOnly.PreviewJSON, `"leaves_device":true`) ||
		!strings.Contains(outputOnly.PreviewJSON, `"input_files_leave_device":false`) ||
		strings.Contains(outputOnly.PreviewJSON, `"input_files"`) {
		t.Fatalf("remote output-only v2 preview misstated input transfer: %s", outputOnly.PreviewJSON)
	}
}

func TestAIAgentExecutionEligibilityReturnsOnlySafeVersionedFacts(t *testing.T) {
	f := newAIAgentRunFixture(t)
	grant := &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, grant, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := registry.Get("workspace_agent_execution")
	result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q}`, f.Task.ID)))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"eligible":true`, f.Task.ID, f.Provider.ID, `"version":3`, `"config_version":5`,
		`"attempt":1`, `"max_result_bytes":65536`, `only submitted delivery creates reviewable output`,
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("eligibility missing %q: %s", expected, result)
		}
	}
	for _, forbidden := range []string{
		f.Provider.BaseURL, "MUST_NOT_LEAK", "PRIVATE ACTOR NOTES", "executable_ref", "manifest", "result_text", "api_key",
	} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("eligibility leaked %q: %s", forbidden, result)
		}
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_id":%q,"provider_id":%q}`, f.Task.ID, f.Provider.ID))); err == nil {
		t.Fatal("eligibility tool accepted undeclared fields")
	}
}

func TestAIAgentRunProposalFreezesCompleteFactsWithoutStarting(t *testing.T) {
	f := newAIAgentRunFixture(t)
	row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	if row.Status != "pending" || !strings.Contains(row.PreviewJSON, `"execution_contract_version":1`) ||
		!strings.Contains(row.PreviewJSON, f.Task.Description) || !strings.Contains(row.PreviewJSON, f.Task.CompletionCriteria) ||
		!strings.Contains(row.PreviewJSON, `"success_does_not_complete_task":true`) ||
		strings.Contains(row.PreviewJSON, f.Provider.BaseURL) || strings.Contains(row.PreviewJSON, "MUST_NOT_LEAK") {
		t.Fatalf("unsafe or incomplete preview: %s", row.PreviewJSON)
	}
	for _, invalid := range []string{
		fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":1,"actor_id":%q,"changes":{"provider_id":%q,"expected_provider_version":3,"expected_provider_config_version":5}}`, f.Task.ID, f.Actor.ID, f.Provider.ID),
		fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":1,"changes":{"provider_id":%q,"expected_provider_version":3,"expected_provider_config_version":5,"attempt":99}}`, f.Task.ID, f.Provider.ID),
		fmt.Sprintf(`{"action":"agent_run.start","task_id":%q,"expected_version":1,"changes":{"provider_id":%q,"expected_provider_version":2,"expected_provider_config_version":5}}`, f.Task.ID, f.Provider.ID),
	} {
		if _, err := f.Tool.Execute(context.Background(), []byte(invalid)); err == nil {
			t.Fatalf("accepted model-controlled execution field/drift: %s", invalid)
		}
	}
}

func TestAIAgentRunConfirmationIsAtomicDriftBoundAndIdempotent(t *testing.T) {
	t.Run("related identity drift rejects exact preview", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.Model(&models.Actor{}).Where("id = ?", f.Actor.ID).Update("version", 2).Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})

	t.Run("provider version drift rejects frozen request", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.Model(&models.AIProvider{}).Where("id = ?", f.Provider.ID).
			Updates(map[string]any{"version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1}).Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AGENT_RUN_IDENTITY_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})

	t.Run("confirmation rollback and durable replay", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/ai/actions/" + row.ID + "/decision"
		withoutConsent := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, path, withoutConsent, nil), 422, "AGENT_EXECUTION_CONFIRMATION_REQUIRED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)

		if err := f.Store.DB.Exec(`CREATE TRIGGER fail_agent_run_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test rollback'); END`).Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		if response := performRequest(f.Router, http.MethodPost, path, body, nil); response.Code != 500 {
			t.Fatalf("rollback response=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
		if err := f.Store.DB.Exec("DROP TRIGGER fail_agent_run_approval").Error; err != nil {
			t.Fatal(err)
		}

		response := performRequest(f.Router, http.MethodPost, path, body, nil)
		if response.Code != 200 {
			t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
		}
		text := response.Body.String()
		for _, expected := range []string{`"status":"confirmed"`, `"result_version":1`, `"agent_run_result"`, `?agent_run=`} {
			if !strings.Contains(text, expected) {
				t.Fatalf("confirmed response missing %q: %s", expected, text)
			}
		}
		for _, forbidden := range []string{"result_text", `"error_code":`, f.Provider.BaseURL, "MUST_NOT_LEAK"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("confirmed receipt leaked %q: %s", forbidden, text)
			}
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id=?", 1, f.Task.ID)
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)

		response = performRequest(f.Router, http.MethodPost, path, body, nil)
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"confirmed"`) {
			t.Fatalf("replay=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id=?", 1, f.Task.ID)
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
		deadline := time.Now().Add(5 * time.Second)
		for {
			var status string
			if err := f.Store.DB.Model(&models.AgentRun{}).Select("status").Where("task_id = ?", f.Task.ID).Scan(&status).Error; err != nil {
				t.Fatal(err)
			}
			if status != "queued" && status != "running" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("confirmed Agent run did not leave the active state")
			}
			time.Sleep(10 * time.Millisecond)
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_started'", 1)
	})
}

func TestAIAgentLegacyV1PreviewWithoutLeavesDeviceRemainsConfirmable(t *testing.T) {
	t.Run("persisted pre-upgrade preview without disclosure confirms", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		base := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		row := cloneAIAgentRunProposalPreview(t, f, base, func(preview map[string]any) {
			delete(aiAgentRunV1ProviderMap(t, preview), "leaves_device")
		})
		// Persist the pre-upgrade raw shape explicitly. This test must keep
		// passing even if the current serializer later gains another disclosure.
		if strings.Contains(row.PreviewJSON, `"leaves_device"`) ||
			!strings.Contains(row.PreviewJSON, `"execution_contract_version":1`) {
			t.Fatalf("pre-upgrade v1 fixture shape changed: %s", row.PreviewJSON)
		}
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("legacy v1 confirm=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE execution_contract_version=1", 1)
	})

	t.Run("persisted intermediate preview with correct disclosure confirms", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		base := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		row := cloneAIAgentRunProposalPreview(t, f, base, func(preview map[string]any) {
			setAIAgentRunV1LeavesDevice(t, preview, false)
		})
		if !strings.Contains(row.PreviewJSON, `"leaves_device":false`) {
			t.Fatalf("intermediate v1 fixture lacks correct disclosure: %s", row.PreviewJSON)
		}
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("intermediate v1 confirm=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE execution_contract_version=1", 1)
	})

	t.Run("persisted preview with incorrect disclosure rejects", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		base := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		row := cloneAIAgentRunProposalPreview(t, f, base, func(preview map[string]any) {
			setAIAgentRunV1LeavesDevice(t, preview, true)
		})
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})

	t.Run("persisted preview with unknown field rejects", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		base := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		row := cloneAIAgentRunProposalPreview(t, f, base, func(preview map[string]any) {
			preview["unexpected_security_field"] = "must-not-be-normalized-away"
		})
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})

	t.Run("legacy normalization does not loosen identity drift", func(t *testing.T) {
		f := newAIAgentRunFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
		if strings.Contains(row.PreviewJSON, `"leaves_device"`) {
			t.Fatalf("legacy fixture unexpectedly contains leaves_device: %s", row.PreviewJSON)
		}
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.Model(&models.Actor{}).Where("id = ?", f.Actor.ID).Update("version", 2).Error; err != nil {
			t.Fatal(err)
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
		assertAPIError(t, performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil), 409, "AI_ACTION_PREVIEW_CHANGED")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	})
}

func TestAIAgentRunReceiptNeverIncludesOutputBody(t *testing.T) {
	f := newAIAgentRunFixture(t)
	row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	run := seedRunningAgentRun(t, f)
	secret := "PRIVATE RUN RESULT MUST NEVER REACH RECEIPT"
	completedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := f.Service.persistPendingAgentRunOutput(run.ID, secret, completedAt); err != nil {
		t.Fatal(err)
	}
	if err := transitionAgentRunOutput(f.Store.DB, run.ID, secret, completedAt,
		agentRunOutputRetained, "TEST_RETAINED", nil, nil); err != nil {
		t.Fatal(err)
	}
	decidedAt := completedAt
	resultVersion := int64(1)
	if err := f.Store.DB.Model(&models.AIActionProposal{}).Where("id = ? AND status = 'pending'", row.ID).
		Updates(map[string]any{
			"status": "confirmed", "result_id": run.ID,
			"result_version": resultVersion, "decided_at": decidedAt,
		}).Error; err != nil {
		t.Fatal(err)
	}
	var delivered models.AgentRun
	if err := f.Store.DB.First(&delivered, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if delivered.Status != "succeeded" || delivered.OutputDeliveryStatus != agentRunOutputRetained ||
		delivered.OutputDeliveryPendingText != nil || delivered.ResultText == nil || *delivered.ResultText != secret {
		t.Fatalf("receipt fixture did not atomically consume pending output: %#v", delivered)
	}
	receipts, err := f.Service.aiActionReceipts(context.Background(), f.Generation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, f.Store, f.Generation)
	exact, err := f.Service.aiActionReceiptsForGeneration(context.Background(), f.Generation.SessionID, f.Generation.ID)
	if err != nil || !strings.Contains(exact, `"source_generation_id":"`+f.Generation.ID+`"`) {
		t.Fatalf("exact Agent receipt=%s err=%v", exact, err)
	}
	for _, body := range []string{receipts, exact} {
		if !strings.Contains(body, `"agent_run_result"`) || !strings.Contains(body, run.ID) ||
			!strings.Contains(body, `"output_delivery_status":"retained"`) ||
			strings.Contains(body, secret) || strings.Contains(body, "result_text") || strings.Contains(body, `"error_code":`) {
			t.Fatalf("unsafe receipt: %s", body)
		}
	}
}

func TestAIAgentRunReceiptSurvivesCascadedRunDeletionAndFollowupChat(t *testing.T) {
	f := newAIAgentRunFixture(t)
	row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision",
		[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	var proposal models.AIActionProposal
	if err := f.Store.DB.First(&proposal, "id = ?", row.ID).Error; err != nil || proposal.ResultID == nil {
		t.Fatalf("confirmed proposal=%#v err=%v", proposal, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var run models.AgentRun
		if err := f.Store.DB.First(&run, "id = ?", *proposal.ResultID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != "queued" && run.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent Run did not leave active state before Task deletion")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := f.Store.DB.Exec("DELETE FROM tasks WHERE id = ?", f.Task.ID).Error; err != nil {
		t.Fatalf("delete Task with cascaded Agent Run: %v", err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=?", 0, *proposal.ResultID)

	receipts, err := f.Service.aiActionReceipts(context.Background(), f.Generation.SessionID)
	if err != nil || !strings.Contains(receipts, `"status":"confirmed"`) ||
		!strings.Contains(receipts, *proposal.ResultID) || strings.Contains(receipts, `"agent_run_result"`) ||
		strings.Contains(receipts, `"route"`) ||
		strings.Contains(receipts, "result_text") || strings.Contains(receipts, "output_delivery_pending") {
		t.Fatalf("cascaded Run receipt=%s err=%v", receipts, err)
	}

	var captured map[string]json.RawMessage
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		streamMockAIDelta(w, `已确认历史操作；当前记录已删除。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, f.Router, "deleted-agent-run-receipt", upstream.URL+"/v1", "test-model")
	body, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID, "session_id": f.Generation.SessionID, "message": "刚才的执行记录还在吗？",
	})
	response = performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("follow-up chat=%d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(string(captured["messages"]), row.ID) ||
		strings.Contains(string(captured["messages"]), `"agent_run_result"`) ||
		strings.Contains(string(captured["messages"]), `"route"`) ||
		strings.Contains(string(captured["messages"]), "result_text") {
		t.Fatalf("unsafe/missing tombstoned receipt in follow-up: %s", captured["messages"])
	}
}
