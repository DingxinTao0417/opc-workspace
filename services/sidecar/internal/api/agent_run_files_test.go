package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type agentRunFileTestFixture struct {
	aiAgentRunFixture
	Artifacts *artifactStore
}

func newAgentRunFileTestFixture(t *testing.T) agentRunFileTestFixture {
	t.Helper()
	fixture := newAIAgentRunFixture(t)
	lifecycleContext, lifecycleCancel := context.WithCancel(context.Background())
	fixture.Service.agentRunLifecycleContext = lifecycleContext
	fixture.Service.agentRunLifecycleCancel = lifecycleCancel
	t.Cleanup(fixture.Service.shutdownAgentRuns)
	artifacts := fixture.Router.artifactStore
	if artifacts == nil {
		t.Fatal("controlled Artifact store unavailable")
	}
	fixture.Service.artifactStore = artifacts
	return agentRunFileTestFixture{aiAgentRunFixture: fixture, Artifacts: artifacts}
}

func (fixture *agentRunFileTestFixture) addProject(t *testing.T) models.Project {
	t.Helper()
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	project := models.Project{
		ID: uuid.NewString(), Name: "Agent file project", Status: "in_progress",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.Store.DB.Create(&project).Error; err != nil {
		t.Fatalf("create Project: %v", err)
	}
	if err := fixture.Store.DB.Model(&models.Task{}).Where("id = ?", fixture.Task.ID).
		UpdateColumn("project_id", project.ID).Error; err != nil {
		t.Fatalf("attach Task to Project: %v", err)
	}
	fixture.Task.ProjectID = &project.ID
	return project
}

func (fixture agentRunFileTestFixture) addTaskArtifact(
	t *testing.T,
	taskID string,
	name string,
	content []byte,
) models.TaskArtifact {
	t.Helper()
	return fixture.addTaskArtifactWithMutation(t, taskID, name, content, nil)
}

func (fixture agentRunFileTestFixture) addTaskArtifactWithMutation(
	t *testing.T,
	taskID string,
	name string,
	content []byte,
	mutate func(*models.TaskArtifact),
) models.TaskArtifact {
	t.Helper()
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	artifactID := uuid.NewString()
	staged, err := fixture.Artifacts.stageMultipartFileWithLimit(
		bytes.NewReader(content), artifactID, maxArtifactFileBytes,
	)
	if err != nil {
		t.Fatalf("stage Task Artifact: %v", err)
	}
	t.Cleanup(func() { fixture.Artifacts.discardStagedFile(staged) })
	if err := fixture.Artifacts.commitStagedFile(staged); err != nil {
		t.Fatalf("commit Task Artifact: %v", err)
	}
	t.Cleanup(func() { _ = fixture.Artifacts.discardCommittedFile(staged.relativePath) })
	var sequence int
	if err := fixture.Store.DB.Model(&models.TaskSubmission{}).Where("task_id = ?", taskID).
		Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
		t.Fatal(err)
	}
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: taskID, Sequence: sequence, Status: "accepted", Origin: "manual",
		Summary: "controlled input fixture", SubmittedByActorID: models.BuiltinOwnerActorID,
		SubmittedAt: now, ReviewedByActorID: aiStringPtr(models.BuiltinOwnerActorID), ReviewedAt: &now,
	}
	if err := fixture.Store.DB.Create(&submission).Error; err != nil {
		t.Fatalf("create Task Submission: %v", err)
	}
	relativePath, mimeType, sizeBytes, digest := staged.relativePath, staged.mimeType, staged.sizeBytes, staged.sha256
	artifact := models.TaskArtifact{
		ID: artifactID, TaskID: taskID, SubmissionID: submission.ID, Position: 1,
		StorageKind: "file", Name: name, RelativePath: &relativePath, MimeType: &mimeType,
		SizeBytes: &sizeBytes, SHA256: &digest, ProducedByActorID: models.BuiltinOwnerActorID,
		RecordedByActorID: models.BuiltinOwnerActorID, IntegrityStatus: "verified",
		IntegrityCheckedAt: &now, CreatedAt: now,
	}
	if mutate != nil {
		mutate(&artifact)
	}
	if err := fixture.Store.DB.Create(&artifact).Error; err != nil {
		t.Fatalf("create Task Artifact: %v", err)
	}
	return artifact
}

func (fixture agentRunFileTestFixture) addProjectAttachment(
	t *testing.T,
	projectID string,
	name string,
	content []byte,
) models.ProjectAttachment {
	t.Helper()
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	id := uuid.NewString()
	staged, err := fixture.Artifacts.stageMultipartFileWithLimit(
		bytes.NewReader(content), id, maxArtifactFileBytes,
	)
	if err != nil {
		t.Fatalf("stage Project Attachment: %v", err)
	}
	t.Cleanup(func() { fixture.Artifacts.discardStagedFile(staged) })
	if err := fixture.Artifacts.commitStagedFile(staged); err != nil {
		t.Fatalf("commit Project Attachment: %v", err)
	}
	t.Cleanup(func() { _ = fixture.Artifacts.discardCommittedFile(staged.relativePath) })
	attachment := models.ProjectAttachment{
		ID: id, ProjectID: projectID, Name: name, RelativePath: staged.relativePath,
		MimeType: staged.mimeType, SizeBytes: staged.sizeBytes, SHA256: staged.sha256,
		RecordedByActorID: models.BuiltinOwnerActorID, IntegrityStatus: "verified",
		IntegrityCheckedAt: now, CreatedAt: now,
	}
	if err := fixture.Store.DB.Create(&attachment).Error; err != nil {
		t.Fatalf("create Project Attachment: %v", err)
	}
	return attachment
}

func createRunningAgentRunFromPrepared(
	t *testing.T,
	fixture agentRunFileTestFixture,
	prepared preparedAgentRun,
) models.AgentRun {
	t.Helper()
	now := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	var run models.AgentRun
	if err := fixture.Store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		run, err = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "agent-run-file-test", now,
		)
		return err
	}); err != nil {
		t.Fatalf("create Agent Run: %v", err)
	}
	if result := fixture.Store.DB.Model(&models.AgentRun{}).
		Where("id = ? AND status = 'queued'", run.ID).
		Updates(map[string]any{"status": "running", "started_at": now}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("claim Agent Run rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if err := fixture.Store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAgentRunV1SnapshotRemainsByteCompatibleAndV2OmitsContent(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	project := fixture.addProject(t)
	taskArtifact := fixture.addTaskArtifact(t, fixture.Task.ID, "brief.md", []byte("private task bytes"))
	projectAttachment := fixture.addProjectAttachment(t, project.ID, "facts.json", []byte(`{"fact":true}`))

	legacy, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantLegacy := fmt.Sprintf(
		`{"task_id":%q,"title":%q,"description":%q,"completion_criteria":%q,"status":%q,"kind":%q}`,
		fixture.Task.ID, fixture.Task.Title, fixture.Task.Description, fixture.Task.CompletionCriteria,
		fixture.Task.Status, fixture.Task.Kind,
	)
	if legacy.ExecutionContractVersion != agentRunExecutionContractVersion ||
		legacy.InputSnapshotJSON != wantLegacy {
		t.Fatalf("legacy snapshot=%s version=%d want=%s", legacy.InputSnapshotJSON,
			legacy.ExecutionContractVersion, wantLegacy)
	}
	emptyCriteria := fixture.Task
	emptyCriteria.CompletionCriteria = ""
	emptyEncoded, err := json.Marshal(agentRunV1TaskSnapshotFromModel(emptyCriteria))
	if err != nil {
		t.Fatal(err)
	}
	wantEmpty := fmt.Sprintf(
		`{"task_id":%q,"title":%q,"description":%q,"status":%q,"kind":%q}`,
		emptyCriteria.ID, emptyCriteria.Title, emptyCriteria.Description,
		emptyCriteria.Status, emptyCriteria.Kind,
	)
	if string(emptyEncoded) != wantEmpty {
		t.Fatalf("legacy empty completion fixture=%s want=%s", emptyEncoded, wantEmpty)
	}
	if strings.Contains(legacy.InputSnapshotJSON, "review_policy") ||
		strings.Contains(legacy.InputSnapshotJSON, "project_id") {
		t.Fatalf("legacy snapshot shape drifted: %s", legacy.InputSnapshotJSON)
	}

	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles: []agentRunInputFileRequest{
			{SourceKind: agentexec.FileSourceTaskArtifact, ID: taskArtifact.ID},
			{SourceKind: agentexec.FileSourceProjectAttachment, ID: projectAttachment.ID},
		},
		OutputContract: &agentRunOutputContractRequest{Type: agentexec.ResultTypeText},
		ArtifactStore:  fixture.Artifacts,
	})
	if err != nil {
		t.Fatalf("prepare v2 Run: %v", err)
	}
	if prepared.ExecutionContractVersion != agentRunExecutionContractVersionV2 {
		t.Fatalf("v2 contract version=%d", prepared.ExecutionContractVersion)
	}
	for _, forbidden := range []string{"private task bytes", `{"fact":true}`, "relative_path", "objects/", "content"} {
		if strings.Contains(prepared.InputSnapshotJSON, forbidden) {
			t.Fatalf("v2 snapshot leaked %q: %s", forbidden, prepared.InputSnapshotJSON)
		}
	}
	snapshot, err := parseAgentRunV2Snapshot(prepared.InputSnapshotJSON)
	if err != nil || snapshot.ProviderKind != fixture.Provider.Kind || len(snapshot.Files) != 2 || snapshot.Task.ProjectID == nil ||
		*snapshot.Task.ProjectID != project.ID {
		t.Fatalf("v2 snapshot=%#v err=%v", snapshot, err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	facts, err := validateFrozenAgentRunIdentityWithStore(fixture.Store.DB, run, fixture.Artifacts)
	if err != nil || len(facts.Files) != 2 || facts.Files[0].Content != "private task bytes" ||
		facts.Files[1].Content != `{"fact":true}` {
		t.Fatalf("resolved pipe files=%#v err=%v", facts.Files, err)
	}
}

func TestAgentRunV3MultiFileOutputDeliversOneReviewSubmission(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	addAgentRunOwnerReviewer(t, fixture.aiAgentRunFixture)
	contract := agentexec.OutputContract{
		Type: agentexec.ResultTypeFiles,
		Files: []agentexec.OutputFileContract{
			{Name: "report.md", MIME: "text/markdown"},
			{Name: "facts.json", MIME: "application/json"},
		},
	}
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		OutputContract: &agentRunOutputContractRequest{
			Type: contract.Type, Files: contract.Files,
		},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatalf("prepare v3 Run: %v", err)
	}
	if prepared.ExecutionContractVersion != agentRunExecutionContractVersionV3 {
		t.Fatalf("execution contract version=%d", prepared.ExecutionContractVersion)
	}
	snapshot, err := parseAgentRunV2Snapshot(prepared.InputSnapshotJSON)
	if err != nil || snapshot.OutputContract.Type != agentexec.ResultTypeFiles ||
		len(snapshot.OutputContract.Files) != 2 || snapshot.OutputContract.Files[1].Name != "facts.json" {
		t.Fatalf("v3 snapshot=%#v err=%v", snapshot, err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	payload, err := agentexec.ResultPayload(agentexec.Result{
		Type:  agentexec.ResultTypeFiles,
		Files: []string{"# 结论\n可人工验收。", `{"source":"agent"}`},
	}, contract, agentexec.MaxResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := fixture.Service.finalizeAgentRunOutcome(run, agentexec.ResultTypeFiles, payload, "", completedAt); err != nil {
		t.Fatalf("finalize multi-file Run: %v", err)
	}
	var delivered models.AgentRun
	if err := fixture.Store.DB.First(&delivered, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if delivered.Status != "succeeded" || delivered.OutputDeliveryStatus != agentRunOutputSubmitted ||
		delivered.SubmissionID == nil || delivered.ArtifactID == nil || delivered.ResultText == nil || *delivered.ResultText != payload {
		t.Fatalf("delivered multi-file Run=%#v", delivered)
	}
	var artifacts []models.TaskArtifact
	if err := fixture.Store.DB.Where("submission_id = ?", *delivered.SubmissionID).
		Order("position").Find(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 || artifacts[0].ID != *delivered.ArtifactID ||
		artifacts[0].Name != "report.md" || artifacts[1].Name != "facts.json" ||
		artifacts[0].MimeType == nil || *artifacts[0].MimeType != "text/markdown" ||
		artifacts[1].MimeType == nil || *artifacts[1].MimeType != "application/json" ||
		artifacts[0].RelativePath == nil || artifacts[1].RelativePath == nil {
		t.Fatalf("delivered Artifacts=%#v", artifacts)
	}
	for index, want := range []string{"# 结论\n可人工验收。", `{"source":"agent"}`} {
		file, _, openErr := fixture.Artifacts.openObject(*artifacts[index].RelativePath)
		if openErr != nil {
			t.Fatal(openErr)
		}
		stored := new(bytes.Buffer)
		_, readErr := stored.ReadFrom(file)
		_ = file.Close()
		if readErr != nil || stored.String() != want {
			t.Fatalf("stored multi-file Artifact %d=%q err=%v", index, stored.String(), readErr)
		}
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE submission_id=?", 2, *delivered.SubmissionID)
	if err := fixture.Service.finalizeAgentRunOutcome(run, agentexec.ResultTypeFiles, payload, "", completedAt); err != nil {
		t.Fatalf("repeat multi-file finalize: %v", err)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE submission_id=?", 2, *delivered.SubmissionID)
}

func TestAgentRunV3MultiFilePendingDeliveryRecoversAllFiles(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	addAgentRunOwnerReviewer(t, fixture.aiAgentRunFixture)
	contract := agentexec.OutputContract{
		Type: agentexec.ResultTypeFiles,
		Files: []agentexec.OutputFileContract{
			{Name: "notes.md", MIME: "text/markdown"},
			{Name: "data.json", MIME: "application/json"},
		},
	}
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		OutputContract: &agentRunOutputContractRequest{Type: contract.Type, Files: contract.Files},
		ArtifactStore:  fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	payload, err := agentexec.ResultPayload(agentexec.Result{
		Type: agentexec.ResultTypeFiles, Files: []string{"# 恢复结果", `{"recovered":true}`},
	}, contract, agentexec.MaxResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := fixture.Service.persistPendingAgentRunOutput(run.ID, payload, completedAt); err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.Service.recoverAgentRunOutput(run.ID, fixture.Service.options.Now().Add(2*time.Minute))
	if err != nil || recovered.Status != "succeeded" || recovered.OutputDeliveryStatus != agentRunOutputSubmitted ||
		recovered.SubmissionID == nil || recovered.ArtifactID == nil || recovered.ResultText == nil || *recovered.ResultText != payload {
		t.Fatalf("recovered multi-file Run=%#v err=%v", recovered, err)
	}
	assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE submission_id=?", 2, *recovered.SubmissionID)
}

func TestAgentRunV1LegacySnapshotFixtureValidatesAndRetries(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	legacyJSON := fmt.Sprintf(
		`{"task_id":%q,"title":%q,"description":%q,"completion_criteria":%q,"status":%q,"kind":%q}`,
		fixture.Task.ID, fixture.Task.Title, fixture.Task.Description, fixture.Task.CompletionCriteria,
		fixture.Task.Status, fixture.Task.Kind,
	)
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Force this historical byte fixture onto the row so validation and retry
	// cannot accidentally depend on the current encoder to generate the input.
	prepared.InputSnapshotJSON = legacyJSON
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	facts, err := validateFrozenAgentRunIdentityWithStore(fixture.Store.DB, run, fixture.Artifacts)
	if err != nil || facts.Snapshot.TaskID != fixture.Task.ID || facts.Snapshot.Title != fixture.Task.Title {
		t.Fatalf("validate historical v1 snapshot facts=%#v err=%v", facts, err)
	}
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	result := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": "failed", "error_code": "TEST_FAILURE", "completed_at": completedAt,
	})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("terminalize historical v1 Run rows=%d err=%v", result.RowsAffected, result.Error)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/agent-runs/:id/retry", fixture.Service.retryAgentRun)
	retry := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/retry", nil, nil)
	if retry.Code != http.StatusCreated {
		t.Fatalf("retry historical v1=%d %s", retry.Code, retry.Body.String())
	}
	var envelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var retried models.AgentRun
	if err := fixture.Store.DB.First(&retried, "id = ?", envelope.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retried.ExecutionContractVersion != agentRunExecutionContractVersion ||
		retried.InputSnapshotJSON != legacyJSON || retried.ParentRunID == nil || *retried.ParentRunID != run.ID {
		t.Fatalf("retried historical v1 Run=%#v", retried)
	}
}

func TestAgentRunControlledFilesRejectOwnershipIntegrityAndUnsafeBytes(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	project := fixture.addProject(t)
	valid := fixture.addTaskArtifact(t, fixture.Task.ID, "valid.txt", []byte("valid"))

	otherTask := models.Task{
		ID: uuid.NewString(), Title: "Other", Kind: "work", Status: "todo", ReviewPolicy: "manual",
		Priority: "P2", Version: 1, CreatedAt: fixture.Task.CreatedAt, UpdatedAt: fixture.Task.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&otherTask).Error; err != nil {
		t.Fatal(err)
	}
	foreignArtifact := fixture.addTaskArtifact(t, otherTask.ID, "foreign.txt", []byte("foreign"))
	otherProject := models.Project{
		ID: uuid.NewString(), Name: "Other project", Status: "in_progress", Version: 1,
		CreatedAt: fixture.Task.CreatedAt, UpdatedAt: fixture.Task.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&otherProject).Error; err != nil {
		t.Fatal(err)
	}
	foreignAttachment := fixture.addProjectAttachment(t, otherProject.ID, "foreign.json", []byte(`{}`))
	nulArtifact := fixture.addTaskArtifact(t, fixture.Task.ID, "nul.txt", []byte{'a', 0, 'b'})
	invalidUTF8 := fixture.addTaskArtifact(t, fixture.Task.ID, "invalid.txt", []byte{0xff, 0xfe})
	hashMismatch := fixture.addTaskArtifactWithMutation(t, fixture.Task.ID, "hash.txt", []byte("hash"), func(artifact *models.TaskArtifact) {
		digest := strings.Repeat("0", 64)
		artifact.SHA256 = &digest
	})
	oversize := fixture.addTaskArtifactWithMutation(t, fixture.Task.ID, "large.txt", []byte("small"), func(artifact *models.TaskArtifact) {
		size := int64(agentexec.MaxFileInputBytes + 1)
		artifact.SizeBytes = &size
	})
	deleted := fixture.addTaskArtifact(t, fixture.Task.ID, "deleted.txt", []byte("deleted"))
	deletedAt := fixture.Task.CreatedAt
	if err := fixture.Store.DB.Model(&models.TaskArtifact{}).Where("id = ?", deleted.ID).
		Updates(map[string]any{
			"deleted_at": deletedAt, "deleted_by_actor_id": models.BuiltinOwnerActorID,
			"delete_reason": "test fixture",
		}).Error; err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		file agentRunInputFileRequest
		code string
	}{
		{"cross Task", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: foreignArtifact.ID}, agentRunFileUnavailableCode},
		{"cross Project", agentRunInputFileRequest{SourceKind: agentexec.FileSourceProjectAttachment, ID: foreignAttachment.ID}, agentRunFileUnavailableCode},
		{"NUL", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: nulArtifact.ID}, agentRunFileInvalidCode},
		{"invalid UTF-8", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: invalidUTF8.ID}, agentRunFileInvalidCode},
		{"hash mismatch", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: hashMismatch.ID}, agentRunFileIntegrityInvalidCode},
		{"oversize", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: oversize.ID}, agentRunFileInvalidCode},
		{"deleted", agentRunInputFileRequest{SourceKind: agentexec.FileSourceTaskArtifact, ID: deleted.ID}, agentRunFileUnavailableCode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
				TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
				InputFiles: []agentRunInputFileRequest{test.file}, ArtifactStore: fixture.Artifacts,
			})
			assertAgentRunDomainCode(t, err, test.code)
		})
	}

	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles:    []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: valid.ID}},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	objectPath, err := fixture.Artifacts.resolveObject(*valid.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = validateFrozenAgentRunIdentityWithStore(fixture.Store.DB, run, fixture.Artifacts)
	assertAgentRunDomainCode(t, err, agentRunIdentityChanged)
	_ = project
}

func TestAgentRunFileCandidatesAndExplicitConsent(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	project := fixture.addProject(t)
	valid := fixture.addTaskArtifact(t, fixture.Task.ID, "brief.md", []byte("CANDIDATE_PRIVATE_BODY"))
	_ = fixture.addProjectAttachment(t, project.ID, "unsafe.txt", []byte{'x', 0, 'y'})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/tasks/:id/agent-run-files", fixture.Service.listAgentRunFiles)
	router.POST("/api/v1/tasks/:id/agent-runs", fixture.Service.createAgentRun)
	response := performRequest(router, http.MethodGet,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-run-files", nil, nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "CANDIDATE_PRIVATE_BODY") {
		t.Fatalf("candidate response=%d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []agentRunFileCandidate `json:"data"`
		Meta struct {
			Limits map[string]int `json:"limits"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 2 || envelope.Meta.Limits["max_files"] != agentexec.MaxFileInputs ||
		envelope.Meta.Limits["max_total_bytes"] != agentexec.MaxFileInputsTotalBytes {
		t.Fatalf("candidate envelope=%#v", envelope)
	}
	var foundValid bool
	for _, item := range envelope.Data {
		if item.ID == valid.ID {
			foundValid = item.Eligible && item.MIME == "text/markdown"
		}
	}
	if !foundValid {
		t.Fatalf("eligible candidate missing: %#v", envelope.Data)
	}

	withoutConsent := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}]}`,
			fixture.Provider.ID, valid.ID)), nil)
	if withoutConsent.Code != http.StatusUnprocessableEntity ||
		responseErrorCode(t, withoutConsent.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("missing consent=%d %s", withoutConsent.Code, withoutConsent.Body.String())
	}
	withoutProviderIdentity := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true}`,
			fixture.Provider.ID, valid.ID)), nil)
	if withoutProviderIdentity.Code != http.StatusUnprocessableEntity ||
		responseErrorCode(t, withoutProviderIdentity.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("missing Provider identity=%d %s", withoutProviderIdentity.Code, withoutProviderIdentity.Body.String())
	}
	unknownProviderIdentityField := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":%q,"endpoint":"must-not-be-accepted"}}`,
			fixture.Provider.ID, valid.ID, fixture.Provider.Version, fixture.Provider.ConfigVersion, fixture.Provider.Kind)), nil)
	if unknownProviderIdentityField.Code != http.StatusBadRequest ||
		responseErrorCode(t, unknownProviderIdentityField.Body.Bytes()) != "INVALID_JSON" {
		t.Fatalf("unknown Provider identity field=%d %s", unknownProviderIdentityField.Code,
			unknownProviderIdentityField.Body.String())
	}
	invalidProviderIdentity := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":{"version":0,"config_version":%d,"kind":"external"}}`,
			fixture.Provider.ID, valid.ID, fixture.Provider.ConfigVersion)), nil)
	if invalidProviderIdentity.Code != http.StatusUnprocessableEntity ||
		responseErrorCode(t, invalidProviderIdentity.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid Provider identity=%d %s", invalidProviderIdentity.Code, invalidProviderIdentity.Body.String())
	}
	spuriousConsent := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":%q}}`,
			fixture.Provider.ID, fixture.Provider.Version, fixture.Provider.ConfigVersion, fixture.Provider.Kind)), nil)
	if spuriousConsent.Code != http.StatusUnprocessableEntity {
		t.Fatalf("spurious consent=%d %s", spuriousConsent.Code, spuriousConsent.Body.String())
	}

	confirmedBody := []byte(fmt.Sprintf(
		`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":%q},"output_contract":{"type":"file","name":"result.md","mime":"text/markdown"}}`,
		fixture.Provider.ID, valid.ID, fixture.Provider.Version, fixture.Provider.ConfigVersion, fixture.Provider.Kind,
	))
	headers := map[string]string{"Idempotency-Key": "native-controlled-file-run"}
	created := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs", confirmedBody, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("confirmed file Run=%d %s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatal(err)
	}
	var stored models.AgentRun
	if err := fixture.Store.DB.First(&stored, "id = ?", createdEnvelope.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ExecutionContractVersion != agentRunExecutionContractVersionV2 ||
		strings.Contains(stored.InputSnapshotJSON, "CANDIDATE_PRIVATE_BODY") {
		t.Fatalf("native controlled file Run=%#v", stored)
	}
	replayed := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs", confirmedBody, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("confirmed file Run replay=%d replayed=%q %s", replayed.Code,
			replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	changedBody := bytes.Replace(confirmedBody, []byte("result.md"), []byte("changed.md"), 1)
	conflict := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs", changedBody, headers)
	if conflict.Code != http.StatusConflict ||
		responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("controlled file request hash conflict=%d %s", conflict.Code, conflict.Body.String())
	}
}

func TestAgentRunFileAccessProviderConfirmationMatchesEveryIdentityField(t *testing.T) {
	provider := models.AIProvider{Version: 3, ConfigVersion: 5, Kind: "local"}
	confirmation := agentRunFileAccessProviderConfirmation{Version: 3, ConfigVersion: 5, Kind: "local"}
	if !confirmation.matches(provider) {
		t.Fatal("exact Provider identity did not match")
	}
	for name, changed := range map[string]models.AIProvider{
		"version":        {Version: 4, ConfigVersion: 5, Kind: "local"},
		"config_version": {Version: 3, ConfigVersion: 6, Kind: "local"},
		"kind":           {Version: 3, ConfigVersion: 5, Kind: "remote"},
	} {
		t.Run(name, func(t *testing.T) {
			if confirmation.matches(changed) {
				t.Fatalf("Provider identity matched after %s drift", name)
			}
		})
	}
}

func TestAgentRunFileConsentRejectsProviderIdentityDriftBeforeLaunch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, agentRunFileTestFixture)
	}{
		{
			name: "provider version",
			mutate: func(t *testing.T, fixture agentRunFileTestFixture) {
				t.Helper()
				if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
					UpdateColumn("version", fixture.Provider.Version+1).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "provider config and endpoint",
			mutate: func(t *testing.T, fixture agentRunFileTestFixture) {
				t.Helper()
				if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
					Updates(map[string]any{
						"version":        fixture.Provider.Version + 1,
						"config_version": fixture.Provider.ConfigVersion + 1,
						"base_url":       "http://127.0.0.1:2/v1",
					}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "provider local to remote kind",
			mutate: func(t *testing.T, fixture agentRunFileTestFixture) {
				t.Helper()
				if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
					Updates(map[string]any{
						"kind": "remote", "base_url": "https://example.test/v1", "has_key": true,
						"version": fixture.Provider.Version + 1, "config_version": fixture.Provider.ConfigVersion + 1,
					}).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAgentRunFileTestFixture(t)
			file := fixture.addTaskArtifact(t, fixture.Task.ID, "consented.md", []byte("PRIVATE FILE BODY"))
			test.mutate(t, fixture)

			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.POST("/api/v1/tasks/:id/agent-runs", fixture.Service.createAgentRun)
			body := []byte(fmt.Sprintf(
				`{"provider_id":%q,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":%q}}`,
				fixture.Provider.ID, file.ID, fixture.Provider.Version, fixture.Provider.ConfigVersion, fixture.Provider.Kind,
			))
			response := performRequest(router, http.MethodPost,
				"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs", body,
				map[string]string{"Idempotency-Key": "provider-consent-drift-" + strings.ReplaceAll(test.name, " ", "-")})
			if response.Code != http.StatusConflict ||
				responseErrorCode(t, response.Body.Bytes()) != agentRunIdentityChanged {
				t.Fatalf("Provider drift=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, fixture.Store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, fixture.Store,
				"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_queued'", 0)
			fixture.Service.agentRunCancelsMu.Lock()
			launched := len(fixture.Service.agentRunCancels)
			fixture.Service.agentRunCancelsMu.Unlock()
			if launched != 0 {
				t.Fatalf("Provider drift launched %d Agent Run workers", launched)
			}
		})
	}
}

func TestAgentRunV2ProviderKindDriftAfterClaimNeverLaunchesExecutor(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	var modelCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"unexpected execution"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	// Both the frozen remote identity and the drifted local identity are valid
	// for this loopback endpoint. Keeping every numeric identity stable after
	// the Run is queued makes Provider kind the only changed execution fact.
	fixture.Provider.Kind = "remote"
	fixture.Provider.BaseURL = upstream.URL + "/v1"
	fixture.Provider.HasKey = true
	fixture.Provider.Version++
	fixture.Provider.ConfigVersion++
	if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
		Updates(map[string]any{
			"kind": fixture.Provider.Kind, "base_url": fixture.Provider.BaseURL, "has_key": true,
			"version": fixture.Provider.Version, "config_version": fixture.Provider.ConfigVersion,
		}).Error; err != nil {
		t.Fatal(err)
	}
	fixture.Service.keyStore = keystore.NewMemoryStore()
	if err := fixture.Service.keyStore.Set(
		aiProviderKeyService, aiProviderKeyAccount(fixture.Provider.ID), "test-secret",
	); err != nil {
		t.Fatal(err)
	}

	artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "kind-bound.txt", []byte("controlled body"))
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles:    []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: artifact.ID}},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := parseAgentRunV2Snapshot(prepared.InputSnapshotJSON)
	if err != nil || snapshot.ProviderKind != "remote" {
		t.Fatalf("frozen Provider kind=%q err=%v", snapshot.ProviderKind, err)
	}
	var run models.AgentRun
	if err := fixture.Store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		run, createErr = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "provider-kind-prelaunch", fixture.Generation.CreatedAt,
		)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a persistence-layer corruption/race that bypasses the normal
	// Provider version trigger. Mutate inside the successful queued -> running
	// claim transaction so only the second, immediate pre-launch check can see
	// it; no request reaches the model server if that gate is effective.
	if err := fixture.Store.DB.Exec("DROP TRIGGER trg_ai_providers_version_step").Error; err != nil {
		t.Fatal(err)
	}
	callbackName := "test:drift_provider_kind_after_agent_run_claim"
	mutations := 0
	if err := fixture.Store.DB.Callback().Update().After("gorm:update").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table != "agent_runs" || mutations != 0 {
			return
		}
		mutations++
		if updateErr := db.Session(&gorm.Session{NewDB: true}).Exec(
			"UPDATE ai_providers SET kind = 'local' WHERE id = ?", fixture.Provider.ID,
		).Error; updateErr != nil {
			db.AddError(updateErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Store.DB.Callback().Update().Remove(callbackName) })

	fixture.Service.executeAgentRun(context.Background(), run.ID)
	if err := fixture.Store.DB.Callback().Update().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	var rejected models.AgentRun
	if err := fixture.Store.DB.First(&rejected, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	var currentProvider models.AIProvider
	if err := fixture.Store.DB.First(&currentProvider, "id = ?", fixture.Provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	if mutations != 1 || currentProvider.Kind != "local" ||
		currentProvider.Version != run.ProviderVersion || currentProvider.ConfigVersion != run.ProviderConfigVersion {
		t.Fatalf("kind-only drift was not isolated: mutations=%d provider=%#v run=%#v", mutations, currentProvider, run)
	}
	if rejected.Status != "failed" || rejected.ErrorCode == nil || *rejected.ErrorCode != agentRunIdentityChanged ||
		rejected.StartedAt == nil || rejected.CompletedAt == nil {
		t.Fatalf("pre-launch kind drift Run=%#v", rejected)
	}
	if modelCalls.Load() != 0 {
		t.Fatalf("Provider kind drift launched executor/model %d times", modelCalls.Load())
	}
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_started' AND aggregate_id=?", 1, run.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_failed' AND aggregate_id=?", 1, run.ID)
}

func TestAgentRunV2RetryRestoresAndRevalidatesFrozenContract(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "retry.txt", []byte("retry input"))
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles: []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: artifact.ID}},
		OutputContract: &agentRunOutputContractRequest{
			Type: agentexec.ResultTypeFile, Name: "retry-result.md", MIME: "text/markdown",
		},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if result := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": "failed", "error_code": "TEST_FAILURE", "completed_at": completedAt,
	}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("terminalize Run rows=%d err=%v", result.RowsAffected, result.Error)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/agent-runs/:id/retry", fixture.Service.retryAgentRun)
	retry := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/retry", nil, nil)
	if retry.Code != http.StatusCreated {
		t.Fatalf("retry v2=%d %s", retry.Code, retry.Body.String())
	}
	var retryEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &retryEnvelope); err != nil {
		t.Fatal(err)
	}
	var retried models.AgentRun
	if err := fixture.Store.DB.First(&retried, "id = ?", retryEnvelope.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retried.ExecutionContractVersion != agentRunExecutionContractVersionV2 ||
		retried.InputSnapshotJSON != run.InputSnapshotJSON || retried.ParentRunID == nil ||
		*retried.ParentRunID != run.ID {
		t.Fatalf("retried v2=%#v", retried)
	}
}

func TestAgentRunFileOutputSubmissionAndPendingRecoveryCompensateObjects(t *testing.T) {
	t.Run("atomic file submission", func(t *testing.T) {
		fixture := newAgentRunFileTestFixture(t)
		addAgentRunOwnerReviewer(t, fixture.aiAgentRunFixture)
		prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
			TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
			OutputContract: &agentRunOutputContractRequest{
				Type: agentexec.ResultTypeFile, Name: "agent-report.md", MIME: "text/markdown",
			},
			ArtifactStore: fixture.Artifacts,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := createRunningAgentRunFromPrepared(t, fixture, prepared)
		body := "# Reviewable report\n"
		completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
		if err := fixture.Service.finalizeAgentRunOutcome(
			run, agentexec.ResultTypeFile, body, "", completedAt,
		); err != nil {
			t.Fatalf("finalize file output: %v", err)
		}
		var delivered models.AgentRun
		if err := fixture.Store.DB.First(&delivered, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if delivered.Status != "succeeded" || delivered.OutputDeliveryStatus != agentRunOutputSubmitted ||
			delivered.ResultText == nil || *delivered.ResultText != body || delivered.ArtifactID == nil {
			t.Fatalf("delivered file Run=%#v", delivered)
		}
		var artifact models.TaskArtifact
		if err := fixture.Store.DB.First(&artifact, "id = ?", *delivered.ArtifactID).Error; err != nil {
			t.Fatal(err)
		}
		if artifact.StorageKind != "file" || artifact.Name != "agent-report.md" ||
			artifact.MimeType == nil || *artifact.MimeType != "text/markdown" || artifact.ContentText != nil ||
			artifact.RelativePath == nil {
			t.Fatalf("file Artifact=%#v", artifact)
		}
		file, _, err := fixture.Artifacts.openObject(*artifact.RelativePath)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		stored := new(bytes.Buffer)
		if _, err := stored.ReadFrom(file); err != nil || stored.String() != body {
			t.Fatalf("stored file=%q err=%v", stored.String(), err)
		}
	})

	t.Run("ambiguous commit preserves database-referenced object", func(t *testing.T) {
		fixture := newAgentRunFileTestFixture(t)
		artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "durable.txt", []byte("durable"))
		if artifact.RelativePath == nil {
			t.Fatal("file Artifact relative path missing")
		}
		ambiguous := errors.New("ambiguous commit")
		delivery := agentRunOutputFileDelivery{
			api: fixture.Service, store: fixture.Artifacts,
			taskID: fixture.Task.ID, artifactID: artifact.ID,
			committedRelativePath: *artifact.RelativePath,
		}
		if err := delivery.finish(ambiguous); !errors.Is(err, ambiguous) {
			t.Fatalf("ambiguous commit result=%v", err)
		}
		file, _, err := fixture.Artifacts.openObject(*artifact.RelativePath)
		if err != nil {
			t.Fatalf("database-referenced object was deleted: %v", err)
		}
		_ = file.Close()
	})

	t.Run("outer commit failure cleans object and persists recoverable pending output", func(t *testing.T) {
		fixture := newAgentRunFileTestFixture(t)
		addAgentRunOwnerReviewer(t, fixture.aiAgentRunFixture)
		prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
			TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
			OutputContract: &agentRunOutputContractRequest{
				Type: agentexec.ResultTypeFile, Name: "recovered.md", MIME: "text/markdown",
			},
			ArtifactStore: fixture.Artifacts,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := createRunningAgentRunFromPrepared(t, fixture, prepared)
		before, err := os.ReadDir(fixture.Artifacts.objectsDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.Store.DB.Exec(`CREATE TABLE agent_file_outer_commit_guard (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			FOREIGN KEY (task_id) REFERENCES tasks(id) DEFERRABLE INITIALLY DEFERRED
		)`).Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.Store.DB.Exec(`CREATE TRIGGER fail_agent_file_output_outer_commit
			AFTER INSERT ON workflow_events WHEN NEW.action = 'agent_run_output_submitted'
			BEGIN
				INSERT INTO agent_file_outer_commit_guard(id, task_id)
				VALUES (NEW.id, '00000000-0000-0000-0000-000000000000');
			END`).Error; err != nil {
			t.Fatal(err)
		}
		body := "outer commit body"
		completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
		err = fixture.Service.finalizeAgentRunOutcome(run, agentexec.ResultTypeFile, body, "", completedAt)
		if !errors.Is(err, errAgentRunOutputDeliveryPending) {
			t.Fatalf("outer commit pending fallback=%v", err)
		}
		after, err := os.ReadDir(fixture.Artifacts.objectsDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("orphan object count before=%d after=%d", len(before), len(after))
		}
		if err := fixture.Store.DB.Exec("DROP TRIGGER fail_agent_file_output_outer_commit").Error; err != nil {
			t.Fatal(err)
		}
		if err := fixture.Store.DB.Exec("DROP TABLE agent_file_outer_commit_guard").Error; err != nil {
			t.Fatal(err)
		}
		var pending models.AgentRun
		if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending ||
			pending.OutputDeliveryPendingText == nil || *pending.OutputDeliveryPendingText != body {
			t.Fatalf("outer commit pending Run=%#v", pending)
		}
		recovered, err := fixture.Service.recoverAgentRunOutput(run.ID, fixture.Service.options.Now().Add(2*time.Minute))
		if err != nil || recovered.OutputDeliveryStatus != agentRunOutputSubmitted || recovered.ArtifactID == nil {
			t.Fatalf("recover outer commit pending Run=%#v err=%v", recovered, err)
		}
	})

	t.Run("delivery failure persists and recovers pending file", func(t *testing.T) {
		fixture := newAgentRunFileTestFixture(t)
		addAgentRunOwnerReviewer(t, fixture.aiAgentRunFixture)
		prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
			TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
			OutputContract: &agentRunOutputContractRequest{
				Type: agentexec.ResultTypeFile, Name: "recovered.md", MIME: "text/markdown",
			},
			ArtifactStore: fixture.Artifacts,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := createRunningAgentRunFromPrepared(t, fixture, prepared)
		before, err := os.ReadDir(fixture.Artifacts.objectsDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.Store.DB.Exec(`CREATE TRIGGER fail_agent_file_output_event
			BEFORE INSERT ON workflow_events WHEN NEW.action = 'task_output_submitted'
			BEGIN SELECT RAISE(ABORT, 'TEST_AGENT_FILE_EVENT_FAILURE'); END`).Error; err != nil {
			t.Fatal(err)
		}
		body := "pending file body"
		completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
		err = fixture.Service.finalizeAgentRunOutcome(run, agentexec.ResultTypeFile, body, "", completedAt)
		if !errors.Is(err, errAgentRunOutputDeliveryPending) {
			t.Fatalf("finalize pending error=%v", err)
		}
		after, err := os.ReadDir(fixture.Artifacts.objectsDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("orphan object count before=%d after=%d", len(before), len(after))
		}
		var pending models.AgentRun
		if err := fixture.Store.DB.First(&pending, "id = ?", run.ID).Error; err != nil {
			t.Fatal(err)
		}
		if pending.OutputDeliveryStatus != agentRunOutputPending ||
			pending.OutputDeliveryPendingText == nil || *pending.OutputDeliveryPendingText != body {
			t.Fatalf("pending file Run=%#v", pending)
		}
		if err := fixture.Store.DB.Exec("DROP TRIGGER fail_agent_file_output_event").Error; err != nil {
			t.Fatal(err)
		}
		recovered, err := fixture.Service.recoverAgentRunOutput(run.ID, fixture.Service.options.Now().Add(2*time.Minute))
		if err != nil || recovered.OutputDeliveryStatus != agentRunOutputSubmitted || recovered.ArtifactID == nil {
			t.Fatalf("recovered file Run=%#v err=%v", recovered, err)
		}
		var artifact models.TaskArtifact
		if err := fixture.Store.DB.First(&artifact, "id = ?", *recovered.ArtifactID).Error; err != nil {
			t.Fatal(err)
		}
		if artifact.StorageKind != "file" || artifact.Name != "recovered.md" {
			t.Fatalf("recovered Artifact=%#v", artifact)
		}
	})
}

func TestActiveV2RunProtectsExactControlledFilesFromDeletion(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	project := fixture.addProject(t)
	taskArtifact := fixture.addTaskArtifact(t, fixture.Task.ID, "protected.txt", []byte("task"))
	projectAttachment := fixture.addProjectAttachment(t, project.ID, "protected.json", []byte(`{}`))
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles: []agentRunInputFileRequest{
			{SourceKind: agentexec.FileSourceTaskArtifact, ID: taskArtifact.ID},
			{SourceKind: agentexec.FileSourceProjectAttachment, ID: projectAttachment.ID},
		},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	if err := fixture.Store.DB.Model(&models.Project{}).Where("id = ?", project.ID).
		UpdateColumn("status", "archived").Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.DELETE("/api/v1/artifacts/:id", fixture.Service.deleteTaskArtifact)
	router.DELETE("/api/v1/project-attachments/:id", fixture.Service.deleteProjectAttachment)
	router.DELETE("/api/v1/projects/:id", fixture.Service.deleteProject)
	taskDelete := performRequest(router, http.MethodDelete,
		"/api/v1/artifacts/"+taskArtifact.ID+"?confirm=true", []byte(`{"reason":"cleanup"}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "protected-task-artifact"})
	if taskDelete.Code != http.StatusConflict ||
		responseErrorCode(t, taskDelete.Body.Bytes()) != agentRunFileReferencedByActiveRunCode {
		t.Fatalf("protected Task Artifact=%d %s", taskDelete.Code, taskDelete.Body.String())
	}
	projectAttachmentDelete := performRequest(router, http.MethodDelete,
		"/api/v1/project-attachments/"+projectAttachment.ID+"?confirm=true", []byte(`{"reason":"cleanup"}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "protected-project-attachment"})
	if projectAttachmentDelete.Code != http.StatusConflict ||
		responseErrorCode(t, projectAttachmentDelete.Body.Bytes()) != agentRunFileReferencedByActiveRunCode {
		t.Fatalf("protected Project Attachment=%d %s", projectAttachmentDelete.Code, projectAttachmentDelete.Body.String())
	}
	var currentProject models.Project
	if err := fixture.Store.DB.First(&currentProject, "id = ?", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	projectDelete := performRequest(router, http.MethodDelete,
		"/api/v1/projects/"+project.ID+"?confirm=true", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentProject.Version)})
	if projectDelete.Code != http.StatusConflict ||
		responseErrorCode(t, projectDelete.Body.Bytes()) != agentRunFileReferencedByActiveRunCode {
		t.Fatalf("protected Project=%d %s", projectDelete.Code, projectDelete.Body.String())
	}

	completedAt := fixture.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if result := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status": "cancelled", "completed_at": completedAt,
	}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("cancel Run rows=%d err=%v", result.RowsAffected, result.Error)
	}
	referenced, err := activeAgentRunReferencesControlledFile(
		fixture.Store.DB, agentexec.FileSourceTaskArtifact, taskArtifact.ID,
	)
	if err != nil || referenced {
		t.Fatalf("terminal Run still blocks exact file referenced=%v err=%v", referenced, err)
	}

	malformed := prepared
	malformed.Attempt = run.Attempt + 1
	malformed.InputSnapshotJSON = `{}`
	if err := fixture.Store.DB.Transaction(func(tx *gorm.DB) error {
		_, err := createAgentRunInTransaction(
			tx, malformed, models.BuiltinOwnerActorID, "malformed-v2-test",
			fixture.Service.options.Now().Add(2*time.Minute).UTC().Format(time.RFC3339Nano),
		)
		return err
	}); err != nil {
		t.Fatalf("create malformed active v2 fixture: %v", err)
	}
	if _, err := activeAgentRunReferencesControlledFile(
		fixture.Store.DB, agentexec.FileSourceTaskArtifact, taskArtifact.ID,
	); err == nil {
		t.Fatal("malformed active v2 snapshot must fail the deletion gate closed")
	}
	malformedBlocked := performRequest(router, http.MethodDelete,
		"/api/v1/artifacts/"+taskArtifact.ID+"?confirm=true", []byte(`{"reason":"cleanup"}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "malformed-active-v2"})
	if malformedBlocked.Code != http.StatusInternalServerError {
		t.Fatalf("malformed active v2 deletion gate=%d %s", malformedBlocked.Code, malformedBlocked.Body.String())
	}
	var stillActive models.TaskArtifact
	if err := fixture.Store.DB.First(&stillActive, "id = ?", taskArtifact.ID).Error; err != nil || stillActive.DeletedAt != nil {
		t.Fatalf("fail-closed deletion mutated Artifact=%#v err=%v", stillActive, err)
	}
}

func TestActiveAgentRunDeletionGateRejectsUnknownAndInvalidV2Contracts(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		version        int64
		wantStatus     int
		mutateSnapshot func(*agentRunV2InputSnapshot)
	}{
		{
			name: "canonical snapshot with invalid Provider kind", status: "queued",
			version: agentRunExecutionContractVersionV2,
			mutateSnapshot: func(snapshot *agentRunV2InputSnapshot) {
				snapshot.ProviderKind = ""
			},
		},
		{
			name: "canonical snapshot with invalid source kind", status: "running",
			version: agentRunExecutionContractVersionV2,
			mutateSnapshot: func(snapshot *agentRunV2InputSnapshot) {
				snapshot.Files[0].SourceKind = "workspace_file"
			},
		},
		{
			name: "canonical snapshot with invalid file UUID", status: "queued",
			version: agentRunExecutionContractVersionV2,
			mutateSnapshot: func(snapshot *agentRunV2InputSnapshot) {
				snapshot.Files[0].ID = "not-a-uuid"
			},
		},
		{
			name: "canonical snapshot with invalid output contract", status: "running",
			version: agentRunExecutionContractVersionV2,
			mutateSnapshot: func(snapshot *agentRunV2InputSnapshot) {
				snapshot.OutputContract = agentexec.OutputContract{
					Type: agentexec.ResultTypeText, Name: "forbidden.md", MIME: "text/markdown",
				}
			},
		},
		{
			name: "unknown execution contract version", status: "queued",
			version: agentRunExecutionContractVersionV6 + 1,
		},
		{
			name: "v5 missing frozen model protocol", status: "queued",
			version: agentRunExecutionContractVersionV5, wantStatus: http.StatusConflict,
		},
		{
			name: "v4 missing frozen rework context", status: "queued",
			version: agentRunExecutionContractVersionV4, wantStatus: http.StatusConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAgentRunFileTestFixture(t)
			artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "protected.txt", []byte("protected"))
			prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
				TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
				InputFiles:    []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: artifact.ID}},
				ArtifactStore: fixture.Artifacts,
			})
			if err != nil {
				t.Fatal(err)
			}
			run := createRunningAgentRunFromPrepared(t, fixture, prepared)
			snapshot, err := parseAgentRunV2Snapshot(prepared.InputSnapshotJSON)
			if err != nil {
				t.Fatal(err)
			}
			if test.mutateSnapshot != nil {
				test.mutateSnapshot(&snapshot)
			}
			rawSnapshot, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			updates := map[string]any{
				"execution_contract_version": test.version,
				"input_snapshot_json":        string(rawSnapshot),
				"status":                     test.status,
			}
			if test.status == "queued" {
				updates["started_at"] = nil
			}
			if result := fixture.Store.DB.Model(&models.AgentRun{}).Where("id = ?", run.ID).Updates(updates); result.Error != nil || result.RowsAffected != 1 {
				t.Fatalf("corrupt active Run rows=%d err=%v", result.RowsAffected, result.Error)
			}

			file, _, err := fixture.Artifacts.openObject(*artifact.RelativePath)
			if err != nil {
				t.Fatalf("open Artifact before blocked delete: %v", err)
			}
			_ = file.Close()
			var eventsBefore int64
			if err := fixture.Store.DB.Model(&models.WorkflowEvent{}).Count(&eventsBefore).Error; err != nil {
				t.Fatal(err)
			}

			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.DELETE("/api/v1/artifacts/:id", fixture.Service.deleteTaskArtifact)
			response := performRequest(router, http.MethodDelete,
				"/api/v1/artifacts/"+artifact.ID+"?confirm=true", []byte(`{"reason":"cleanup"}`),
				map[string]string{"If-Match": `"1"`, "Idempotency-Key": "invalid-active-contract-" + uuid.NewString()})
			wantStatus := test.wantStatus
			if wantStatus == 0 {
				wantStatus = http.StatusInternalServerError
			}
			if response.Code != wantStatus {
				t.Fatalf("invalid active contract deletion gate=%d %s", response.Code, response.Body.String())
			}

			var current models.TaskArtifact
			if err := fixture.Store.DB.First(&current, "id = ?", artifact.ID).Error; err != nil || current.DeletedAt != nil {
				t.Fatalf("blocked deletion mutated Artifact=%#v err=%v", current, err)
			}
			file, _, err = fixture.Artifacts.openObject(*artifact.RelativePath)
			if err != nil {
				t.Fatalf("blocked deletion moved or removed Artifact bytes: %v", err)
			}
			_ = file.Close()
			var eventsAfter int64
			if err := fixture.Store.DB.Model(&models.WorkflowEvent{}).Count(&eventsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if eventsAfter != eventsBefore {
				t.Fatalf("blocked deletion wrote workflow events before=%d after=%d", eventsBefore, eventsAfter)
			}
		})
	}
}

func TestActiveV1RunHasNoControlledFileReferences(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "unrelated.txt", []byte("unrelated"))
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID, ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ExecutionContractVersion != agentRunExecutionContractVersion {
		t.Fatalf("execution contract version=%d", prepared.ExecutionContractVersion)
	}
	createRunningAgentRunFromPrepared(t, fixture, prepared)
	referenced, err := activeAgentRunReferencesControlledFile(
		fixture.Store.DB, agentexec.FileSourceTaskArtifact, artifact.ID,
	)
	if err != nil || referenced {
		t.Fatalf("v1 Run controlled file reference=%v err=%v", referenced, err)
	}
}

func TestAgentRunV2LaunchFactsNeverPersistFileContent(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	artifact := fixture.addTaskArtifact(t, fixture.Task.ID, "pipe.txt", []byte("pipe-only-secret"))
	prepared, err := prepareAgentRun(fixture.Store.DB, prepareAgentRunInput{
		TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
		InputFiles:    []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: artifact.ID}},
		ArtifactStore: fixture.Artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := createRunningAgentRunFromPrepared(t, fixture, prepared)
	facts, err := validateFrozenAgentRunIdentityWithStore(fixture.Store.DB.WithContext(context.Background()), run, fixture.Artifacts)
	if err != nil || len(facts.Files) != 1 || facts.Files[0].Content != "pipe-only-secret" {
		t.Fatalf("launch facts=%#v err=%v", facts, err)
	}
	var persisted string
	if err := fixture.Store.SQL.QueryRow("SELECT input_snapshot_json FROM agent_runs WHERE id = ?", run.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "pipe-only-secret") || strings.Contains(persisted, "objects/") {
		t.Fatalf("durable snapshot leaked pipe content/path: %s", persisted)
	}
}

func newUnassignedAgentRunTask(t *testing.T, fixture agentRunFileTestFixture) models.Task {
	t.Helper()
	task := fixture.Task
	task.ID = uuid.NewString()
	task.Title = "Atomic auto-assignment task"
	task.Version = 1
	if err := fixture.Store.DB.Create(&task).Error; err != nil {
		t.Fatalf("create unassigned Task: %v", err)
	}
	return task
}

func newAssignedAgentRunTask(t *testing.T, fixture agentRunFileTestFixture) models.Task {
	t.Helper()
	task := fixture.Task
	task.ID = uuid.NewString()
	task.Title = "Idempotency-scoped Agent Run task"
	task.Version = 1
	if err := fixture.Store.DB.Create(&task).Error; err != nil {
		t.Fatalf("create assigned Task: %v", err)
	}
	assignment := models.TaskAssignment{
		ID: uuid.NewString(), TaskID: task.ID, ActorID: fixture.Actor.ID, Role: "assignee",
		AssignedByActorID: models.BuiltinOwnerActorID,
		AssignedAt:        fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
		Reason:            "idempotency scope test",
	}
	if err := fixture.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatalf("assign Agent Run Task: %v", err)
	}
	return task
}

func TestAgentRunCreateIdempotencyIsScopedToCanonicalTask(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	secondTask := newAssignedAgentRunTask(t, fixture)
	fixture.Service.agentRunLifecycleCancel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	router.POST("/api/v1/tasks/:id/agent-runs", fixture.Service.createAgentRun)
	body := []byte(fmt.Sprintf(`{"provider_id":%q}`, fixture.Provider.ID))
	headers := map[string]string{"Idempotency-Key": "agent-run-task-scope"}

	first := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+fixture.Task.ID+"/agent-runs", body, headers)
	if first.Code != http.StatusCreated || first.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("first Task create=%d replayed=%q %s", first.Code,
			first.Header().Get("Idempotency-Replayed"), first.Body.String())
	}
	var firstEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	if firstEnvelope.Data.TaskID != fixture.Task.ID {
		t.Fatalf("first Task Run=%#v", firstEnvelope.Data)
	}

	// Simulate a durable record written before Task IDs were included in the
	// endpoint identity. The same Task must still replay it after upgrading.
	if result := fixture.Store.DB.Model(&models.IdempotencyKey{}).
		Where("key = ? AND endpoint = ?", headers["Idempotency-Key"],
			createAgentRunIdempotencyEndpoint(fixture.Task.ID)).
		Update("endpoint", createAgentRunEndpoint); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("convert legacy idempotency row=%d err=%v", result.RowsAffected, result.Error)
	}
	legacyReplay := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+strings.ToUpper(fixture.Task.ID)+"/agent-runs", body, headers)
	if legacyReplay.Code != http.StatusCreated || legacyReplay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("legacy same-Task replay=%d replayed=%q %s", legacyReplay.Code,
			legacyReplay.Header().Get("Idempotency-Replayed"), legacyReplay.Body.String())
	}
	var legacyEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(legacyReplay.Body.Bytes(), &legacyEnvelope); err != nil ||
		legacyEnvelope.Data.ID != firstEnvelope.Data.ID {
		t.Fatalf("legacy same-Task replay=%#v err=%v", legacyEnvelope.Data, err)
	}

	second := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+secondTask.ID+"/agent-runs", body, headers)
	if second.Code != http.StatusCreated || second.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("second Task create=%d replayed=%q %s", second.Code,
			second.Header().Get("Idempotency-Replayed"), second.Body.String())
	}
	var secondEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondEnvelope); err != nil {
		t.Fatal(err)
	}
	if secondEnvelope.Data.TaskID != secondTask.ID || secondEnvelope.Data.ID == firstEnvelope.Data.ID {
		t.Fatalf("cross-Task request replayed wrong Run: first=%#v second=%#v",
			firstEnvelope.Data, secondEnvelope.Data)
	}
	secondReplay := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+secondTask.ID+"/agent-runs", body, headers)
	if secondReplay.Code != http.StatusCreated || secondReplay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("scoped same-Task replay=%d replayed=%q %s", secondReplay.Code,
			secondReplay.Header().Get("Idempotency-Replayed"), secondReplay.Body.String())
	}
	var secondReplayEnvelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(secondReplay.Body.Bytes(), &secondReplayEnvelope); err != nil ||
		secondReplayEnvelope.Data.ID != secondEnvelope.Data.ID {
		t.Fatalf("scoped same-Task replay=%#v err=%v", secondReplayEnvelope.Data, err)
	}

	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM idempotency_keys WHERE key = ?", 2, headers["Idempotency-Key"])
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM agent_runs WHERE task_id IN (?, ?)", 2, fixture.Task.ID, secondTask.ID)
}

func TestAgentRunAtomicAutoAssignRollsBackOnProviderConsentRace(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	task := newUnassignedAgentRunTask(t, fixture)
	var modelCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"unexpected execution"}}]}`))
	}))
	t.Cleanup(upstream.Close)
	fixture.Provider.BaseURL = upstream.URL + "/v1"
	fixture.Provider.Version++
	fixture.Provider.ConfigVersion++
	if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
		Updates(map[string]any{
			"base_url": fixture.Provider.BaseURL, "version": fixture.Provider.Version,
			"config_version": fixture.Provider.ConfigVersion,
		}).Error; err != nil {
		t.Fatal(err)
	}
	file := fixture.addTaskArtifact(t, task.ID, "consented.md", []byte("PRIVATE FILE BODY"))
	if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).
		UpdateColumn("version", fixture.Provider.Version+1).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	router.POST("/api/v1/tasks/:id/agent-runs", fixture.Service.createAgentRun)
	body := []byte(fmt.Sprintf(
		`{"provider_id":%q,"auto_assign":{"actor_id":%q,"expected_task_version":%d},"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":%q}}`,
		fixture.Provider.ID, fixture.Actor.ID, task.Version, file.ID,
		fixture.Provider.Version, fixture.Provider.ConfigVersion, fixture.Provider.Kind,
	))
	response := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/agent-runs", body,
		map[string]string{"Idempotency-Key": "atomic-auto-assign-provider-race"})
	if response.Code != http.StatusConflict ||
		responseErrorCode(t, response.Body.Bytes()) != agentRunIdentityChanged {
		t.Fatalf("Provider race=%d %s", response.Code, response.Body.String())
	}

	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM task_assignments WHERE task_id = ?", 0, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'assignment_created'", 0, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM agent_runs WHERE task_id = ?", 0, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE action = 'agent_run_queued'", 0)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM idempotency_keys WHERE key = ?", 0, "atomic-auto-assign-provider-race")
	var unchanged models.Task
	if err := fixture.Store.DB.First(&unchanged, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != task.Version {
		t.Fatalf("failed atomic create changed Task version=%d want=%d", unchanged.Version, task.Version)
	}
	fixture.Service.agentRunCancelsMu.Lock()
	launched := len(fixture.Service.agentRunCancels)
	fixture.Service.agentRunCancelsMu.Unlock()
	if launched != 0 {
		t.Fatalf("failed atomic create launched %d Agent Run workers", launched)
	}
	fixture.Service.agentRunWorkers.Wait()
	if modelCalls.Load() != 0 {
		t.Fatalf("failed atomic create reached the Provider %d times", modelCalls.Load())
	}
}

func TestAgentRunAtomicAutoAssignCreatesAssignmentAndRunTogether(t *testing.T) {
	fixture := newAgentRunFileTestFixture(t)
	task := newUnassignedAgentRunTask(t, fixture)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	router.POST("/api/v1/tasks/:id/agent-runs", fixture.Service.createAgentRun)
	body := []byte(fmt.Sprintf(
		`{"provider_id":%q,"auto_assign":{"actor_id":%q,"expected_task_version":%d}}`,
		fixture.Provider.ID, fixture.Actor.ID, task.Version,
	))
	response := performRequest(router, http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/agent-runs", body,
		map[string]string{"Idempotency-Key": "atomic-auto-assign-success"})
	if response.Code != http.StatusCreated {
		t.Fatalf("atomic auto-assign=%d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}

	var assignment models.TaskAssignment
	if err := fixture.Store.DB.Where("task_id = ? AND role = 'assignee' AND unassigned_at IS NULL", task.ID).
		Take(&assignment).Error; err != nil {
		t.Fatalf("load atomic assignment: %v", err)
	}
	if assignment.ActorID != fixture.Actor.ID || envelope.Data.AssignmentID != assignment.ID ||
		envelope.Data.TaskVersion != task.Version+1 {
		t.Fatalf("atomic assignment/run mismatch assignment=%#v run=%#v", assignment, envelope.Data)
	}
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM task_assignments WHERE task_id = ?", 1, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM agent_runs WHERE task_id = ?", 1, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'assignment_created'", 1, task.ID)
	assertDatabaseCount(t, fixture.Store,
		"SELECT COUNT(*) FROM workflow_events WHERE agent_run_id = ? AND action = 'agent_run_queued'", 1, envelope.Data.ID)
}
