package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

const reworkTestReason = "REWORK_REASON_ONLY: 补齐第三项验收数据，保留已经正确的前两项。"
const reworkTestOriginal = "REWORK_PREVIOUS_ONLY: 第一项正确；第二项正确；第三项待修订。"

type aiReworkContractFixture struct {
	aiAgentRunFixture
	Native     *gin.Engine
	Original   models.AgentRun
	Submission models.TaskSubmission
	Artifact   models.TaskArtifact
}

// The original output comes through the real shared finalizer and owner review.
// Execution itself is controlled: no subprocess or real Provider is started.
func newAIReworkContractFixture(t *testing.T, originalContent ...string) aiReworkContractFixture {
	t.Helper()
	f := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, f)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = stopped
	run := seedQueuedFrozenAgentRun(t, f)
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	original := reworkTestOriginal
	if len(originalContent) > 0 {
		original = originalContent[0]
	}
	if err := f.Service.finalizeAgentRun(run, original, "", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" || run.OutputDeliveryStatus != "submitted" || run.SubmissionID == nil || run.ArtifactID == nil {
		t.Fatalf("real first output not delivered: %+v", run)
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	reviewBody, _ := json.Marshal(map[string]any{"decision": "request_changes", "reason": reworkTestReason})
	review := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/review", reviewBody, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if review.Code != http.StatusOK {
		t.Fatalf("real owner request_changes = %d: %s", review.Code, review.Body.String())
	}
	f.Task = decodeReviewOutputResponse(t, review.Body.Bytes()).Task
	var submission models.TaskSubmission
	var artifact models.TaskArtifact
	if err := f.Store.DB.First(&submission, "id=?", *run.SubmissionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&artifact, "id=?", *run.ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if f.Task.Status != "in_progress" || submission.Status != "changes_requested" || submission.ReviewReason == nil || *submission.ReviewReason != reworkTestReason || f.Task.CurrentSubmissionID == nil || *f.Task.CurrentSubmissionID != submission.ID {
		t.Fatal("review fixture lost the exact current returned batch")
	}
	native := gin.New()
	native.POST("/api/v1/tasks/:id/agent-runs", f.Service.createAgentRun)
	native.POST("/api/v1/agent-runs/:id/retry", f.Service.retryAgentRun)
	native.POST("/api/v1/agent-runs/:id/cancel", f.Service.cancelAgentRun)
	native.POST("/api/v1/ai/actions/:id/decision", f.Service.decideAIWorkspaceAction)
	return aiReworkContractFixture{aiAgentRunFixture: f, Native: native, Original: run, Submission: submission, Artifact: artifact}
}

func (f aiReworkContractFixture) nativeBody(artifactIDs []string) map[string]any {
	return map[string]any{
		"provider_id":                  f.Provider.ID,
		"rework":                       map[string]any{"submission_id": f.Submission.ID, "artifact_ids": artifactIDs, "expected_task_version": f.Task.Version},
		"confirm_rework_context":       true,
		"rework_provider_confirmation": map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind},
	}
}

func (f aiReworkContractFixture) proposalBody(artifactIDs []string) []byte {
	body, _ := json.Marshal(map[string]any{"action": "agent_run.start", "task_id": f.Task.ID, "expected_version": f.Task.Version, "changes": map[string]any{
		"provider_id": f.Provider.ID, "expected_provider_version": f.Provider.Version, "expected_provider_config_version": f.Provider.ConfigVersion,
		"rework_submission_id": f.Submission.ID, "rework_artifact_ids": artifactIDs,
	}})
	return body
}

func (f aiReworkContractFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested' AND review_reason=?", 1, f.Submission.ID, reworkTestReason)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status='in_progress'", 1, f.Task.ID, f.Task.Version)
}

func (f aiReworkContractFixture) createRework(t *testing.T) models.AgentRun {
	t.Helper()
	body, _ := json.Marshal(f.nativeBody([]string{f.Artifact.ID}))
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create rework = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id=?", payload.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAIAgentReworkPreviewIsReadOnlyAndSelectionIsExplicit(t *testing.T) {
	f := newAIReworkContractFixture(t)
	var beforeEvents int64
	if err := f.Store.DB.Model(&models.WorkflowEvent{}).Count(&beforeEvents).Error; err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{}, {f.Artifact.ID}} {
		body, _ := json.Marshal(map[string]any{"rework": f.nativeBody(ids)["rework"]})
		response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs/rework-preview", body, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("read-only preview = %d: %s", response.Code, response.Body.String())
		}
		var payload struct {
			Data struct {
				TaskID      string `json:"task_id"`
				TaskVersion int64  `json:"task_version"`
				Rework      struct {
					SubmissionID string `json:"submission_id"`
					Sequence     int    `json:"sequence"`
					ReviewReason string `json:"review_reason"`
					ReviewedAt   string `json:"reviewed_at"`
					Artifacts    []struct {
						ID          string `json:"id"`
						StorageKind string `json:"storage_kind"`
						Name        string `json:"name"`
						Content     string `json:"content"`
						SHA256      string `json:"sha256"`
					} `json:"artifacts"`
				} `json:"rework_context"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		p := payload.Data
		if p.TaskID != f.Task.ID || p.TaskVersion != f.Task.Version || p.Rework.SubmissionID != f.Submission.ID || p.Rework.Sequence != f.Submission.Sequence || p.Rework.ReviewReason != reworkTestReason || f.Submission.ReviewedAt == nil || p.Rework.ReviewedAt != *f.Submission.ReviewedAt || len(p.Rework.Artifacts) != len(ids) {
			t.Fatalf("preview is not actual review evidence: %s", response.Body.String())
		}
		if len(ids) == 0 && strings.Contains(response.Body.String(), reworkTestOriginal) {
			t.Fatal("unselected previous output entered the preview")
		}
		if len(ids) > 0 {
			a := p.Rework.Artifacts[0]
			if a.ID != f.Artifact.ID || a.StorageKind != "text" || a.Name != f.Artifact.Name || a.Content != reworkTestOriginal || a.SHA256 != sha256Hex([]byte(reworkTestOriginal)) {
				t.Fatalf("selected evidence/hash changed: %+v", a)
			}
		}
		for _, secret := range []string{"MUST_NOT_LEAK", f.Provider.BaseURL, "input_snapshot_json", "relative_path"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("preview leaked %s", secret)
			}
		}
		f.assertUnchanged(t)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events", beforeEvents)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIAgentReworkPreviewRejectsAmbiguousNestedIdentity(t *testing.T) {
	f := newAIReworkContractFixture(t)
	valid, _ := json.Marshal(f.nativeBody([]string{f.Artifact.ID})["rework"])
	for name, nested := range map[string]string{
		"duplicate-submission": strings.TrimSuffix(string(valid), "}") + fmt.Sprintf(`,"submission_id":%q}`, f.Submission.ID),
		"case-alias":           strings.Replace(string(valid), `"artifact_ids"`, `"Artifact_IDs"`, 1),
		"extra-body":           strings.TrimSuffix(string(valid), "}") + `,"review_reason":"Do not use the actual stored reason"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs/rework-preview", []byte(`{"rework":`+nested+`}`), nil)
			if response.Code < 400 || response.Code >= 500 {
				t.Fatalf("ambiguous native rework accepted=%d %s", response.Code, response.Body.String())
			}
			f.assertUnchanged(t)
		})
	}
}

func TestAIAgentReworkNativeFreezesExactReviewedEvidence(t *testing.T) {
	f := newAIReworkContractFixture(t)
	f.assertUnchanged(t)
	// A successful old Run cannot be reinterpreted as a frozen-input retry after
	// actual submission and review changed the Task version.
	if response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+f.Original.ID+"/retry", nil, nil); response.Code != http.StatusConflict {
		t.Fatalf("old retry lost its identity gate: %d %s", response.Code, response.Body.String())
	}
	body, _ := json.Marshal(f.nativeBody([]string{f.Artifact.ID}))
	path := "/api/v1/tasks/" + f.Task.ID + "/agent-runs"
	var runID string
	for attempt := 0; attempt < 2; attempt++ {
		response := performRequest(f.Native, http.MethodPost, path, body, map[string]string{"Idempotency-Key": "explicit-rework"})
		if response.Code != http.StatusCreated {
			t.Fatalf("explicit native rework = %d: %s", response.Code, response.Body.String())
		}
		var payload struct {
			Data agentRunResponse `json:"data"`
		}
		if json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.Data.ID == "" || payload.Data.ExecutionContractVersion != 4 || payload.Data.ParentRunID != nil {
			t.Fatalf("new revision was not a distinct v4 start: %s", response.Body.String())
		}
		if attempt > 0 && runID != payload.Data.ID {
			t.Fatal("idempotent rework created another run")
		}
		runID = payload.Data.ID
		if strings.Contains(response.Body.String(), reworkTestReason) || strings.Contains(response.Body.String(), reworkTestOriginal) {
			t.Fatal("Run metadata leaked frozen rework body")
		}
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.TaskVersion != f.Task.Version || run.Attempt != f.Original.Attempt+1 || run.Status != "queued" || !strings.Contains(run.InputSnapshotJSON, reworkTestReason) || !strings.Contains(run.InputSnapshotJSON, reworkTestOriginal) || !strings.Contains(run.InputSnapshotJSON, f.Submission.ID) || !strings.Contains(run.InputSnapshotJSON, f.Artifact.ID) {
		t.Fatalf("review evidence missing from new frozen input: %+v", run)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='succeeded' AND input_snapshot_json=?", 1, f.Original.ID, f.Original.InputSnapshotJSON)
}

func TestAIAgentReworkNativeRequiresSeparateConsentAndExactIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"without-context-consent", func(body map[string]any) { delete(body, "confirm_rework_context") }},
		{"without-provider-consent", func(body map[string]any) { delete(body, "rework_provider_confirmation") }},
		{"provider-version-changed", func(body map[string]any) { body["rework_provider_confirmation"].(map[string]any)["version"] = int64(1) }},
		{"wrong-submission", func(body map[string]any) { body["rework"].(map[string]any)["submission_id"] = uuid.NewString() }},
		{"stale-task-version", func(body map[string]any) { body["rework"].(map[string]any)["expected_task_version"] = int64(1) }},
		{"null-artifact-selection", func(body map[string]any) { body["rework"].(map[string]any)["artifact_ids"] = nil }},
		{"too-many-artifacts", func(body map[string]any) {
			body["rework"].(map[string]any)["artifact_ids"] = []string{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()}
		}},
		{"mixed-auto-assign", func(body map[string]any) {
			body["auto_assign"] = map[string]any{"actor_id": uuid.NewString(), "expected_task_version": body["rework"].(map[string]any)["expected_task_version"]}
		}},
		{"wrong-artifact", func(body map[string]any) {
			body["rework"].(map[string]any)["artifact_ids"] = []string{uuid.NewString()}
		}},
		{"duplicate-artifacts", func(body map[string]any) {
			rework := body["rework"].(map[string]any)
			ids := rework["artifact_ids"].([]string)
			rework["artifact_ids"] = []string{ids[0], ids[0]}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAIReworkContractFixture(t)
			input := f.nativeBody([]string{f.Artifact.ID})
			tc.mutate(input)
			body, _ := json.Marshal(input)
			response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
			if response.Code < 400 || response.Code >= 500 {
				t.Fatalf("invalid rework accepted: %d %s", response.Code, response.Body.String())
			}
			f.assertUnchanged(t)
		})
	}
}

func TestAIAgentReworkProposalNeedsIndependentHumanConfirmation(t *testing.T) {
	f := newAIReworkContractFixture(t)
	row := proposeTestAction(t, f.Store, f.Tool, string(f.proposalBody([]string{f.Artifact.ID})))
	f.assertUnchanged(t)
	if !strings.Contains(row.PreviewJSON, `"rework_context"`) || !strings.Contains(row.PreviewJSON, reworkTestReason) || !strings.Contains(row.PreviewJSON, reworkTestOriginal) || !strings.Contains(row.PreviewJSON, f.Submission.ID) {
		t.Fatalf("approval lacks real review context: %s", row.PreviewJSON)
	}
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	without := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
	if response := performRequest(f.Native, http.MethodPost, path, without, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rework context silently consented: %d %s", response.Code, response.Body.String())
	}
	f.assertUnchanged(t)
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_rework":true}`, row.Fingerprint))
	if err := f.Store.DB.Exec(`CREATE TRIGGER reject_rework_approval BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'injected rework rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if response := performRequest(f.Native, http.MethodPost, path, body, nil); response.Code != http.StatusInternalServerError {
		t.Fatalf("confirmation transaction did not roll back: %d %s", response.Code, response.Body.String())
	}
	f.assertUnchanged(t)
	if err := f.Store.DB.Exec("DROP TRIGGER reject_rework_approval").Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if response := performRequest(f.Native, http.MethodPost, path, body, nil); response.Code != http.StatusOK {
			t.Fatalf("human rework confirmation = %d %s", response.Code, response.Body.String())
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE execution_contract_version=4 AND parent_run_id IS NULL", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested'", 1, f.Submission.ID)
}

func TestAIAgentReworkApprovalAndPrelaunchRejectCurrentEvidenceDrift(t *testing.T) {
	t.Run("proposal then soft-delete", func(t *testing.T) {
		f := newAIReworkContractFixture(t)
		row := proposeTestAction(t, f.Store, f.Tool, string(f.proposalBody([]string{f.Artifact.ID})))
		if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
			t.Fatal(err)
		}
		deleted := performRequest(f.Router, http.MethodDelete, "/api/v1/artifacts/"+f.Artifact.ID+"?confirm=true", []byte(`{"reason":"remove reviewed source"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
		if deleted.Code != http.StatusOK {
			t.Fatalf("delete source=%d %s", deleted.Code, deleted.Body.String())
		}
		body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_rework":true}`, row.Fingerprint))
		response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
		if response.Code != http.StatusConflict {
			t.Fatalf("stale review approval=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	})
	t.Run("queued revalidation", func(t *testing.T) {
		f := newAIReworkContractFixture(t)
		run := f.createRework(t)
		if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err != nil {
			t.Fatalf("fresh v4 prelaunch rejected: %v", err)
		}
		changed := performRequest(f.Router, http.MethodPatch, "/api/v1/tasks/"+f.Task.ID, []byte(`{"description":"Changed after rework was approved"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
		if changed.Code != http.StatusOK {
			t.Fatalf("native Task update=%d %s", changed.Code, changed.Body.String())
		}
		if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err == nil {
			t.Fatal("prelaunch reused stale rework evidence")
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
	})
}

func TestAIAgentReworkFailedRetryKeepsFrozenFeedbackAndNewConsent(t *testing.T) {
	f := newAIReworkContractFixture(t)
	run := f.createRework(t)
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/agent-runs/" + run.ID + "/retry"
	if response := performRequest(f.Native, http.MethodPost, path, nil, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("v4 retry reused old rework consent=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	input := f.nativeBody([]string{})
	body, _ := json.Marshal(map[string]any{"confirm_rework_context": true, "rework_provider_confirmation": input["rework_provider_confirmation"]})
	response := performRequest(f.Native, http.MethodPost, path, body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("v4 retry=%d %s", response.Code, response.Body.String())
	}
	var child models.AgentRun
	if err := f.Store.DB.First(&child, "parent_run_id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if child.InputSnapshotJSON != run.InputSnapshotJSON || child.TaskVersion != run.TaskVersion || child.ExecutionContractVersion != 4 || child.Attempt != run.Attempt+1 {
		t.Fatalf("retry altered frozen revision: %+v", child)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='failed'", 1, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
}

func TestAIAgentReworkAIRetryNeedsFeedbackConsentButNotFilePermission(t *testing.T) {
	f := newAIReworkContractFixture(t)
	run := f.createRework(t)
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	// This fixture has execution but no agent_files or output_files capability.
	row := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, run))
	if !strings.Contains(row.PreviewJSON, reworkTestReason) || !strings.Contains(row.PreviewJSON, reworkTestOriginal) {
		t.Fatal("retry did not preview original frozen feedback")
	}
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true}`, row.Fingerprint))
	if response := performRequest(f.Native, http.MethodPost, path, body, nil); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("AI retry silently inherited feedback approval=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_rework":true}`, row.Fingerprint))
	if response := performRequest(f.Native, http.MethodPost, path, body, nil); response.Code != http.StatusOK {
		t.Fatalf("AI rework retry=%d %s", response.Code, response.Body.String())
	}
	var child models.AgentRun
	if err := f.Store.DB.First(&child, "parent_run_id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if child.InputSnapshotJSON != run.InputSnapshotJSON || child.ExecutionContractVersion != 4 || child.Attempt != run.Attempt+1 {
		t.Fatal("AI retry changed feedback or identity")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
}

func TestAIAgentReworkStrictAIPairAndIrrelevantConsent(t *testing.T) {
	f := newAIReworkContractFixture(t)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing-submission", func(changes map[string]any) { delete(changes, "rework_submission_id") }},
		{"missing-artifacts", func(changes map[string]any) { delete(changes, "rework_artifact_ids") }},
		{"null-submission", func(changes map[string]any) { changes["rework_submission_id"] = nil }},
		{"null-artifacts", func(changes map[string]any) { changes["rework_artifact_ids"] = nil }},
		{"duplicate-artifacts", func(changes map[string]any) { changes["rework_artifact_ids"] = []string{f.Artifact.ID, f.Artifact.ID} }},
		{"model-supplied-reason", func(changes map[string]any) { changes["review_reason"] = "substitute this for the actual feedback" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var action map[string]any
			if err := json.Unmarshal(f.proposalBody([]string{f.Artifact.ID}), &action); err != nil {
				t.Fatal(err)
			}
			tc.mutate(action["changes"].(map[string]any))
			body, _ := json.Marshal(action)
			if result, err := f.Tool.Execute(context.Background(), body); err == nil || result != "" {
				t.Fatalf("invalid AI rework pair accepted=%s %v", result, err)
			}
		})
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	row := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f.aiAgentRunFixture))
	if strings.Contains(row.PreviewJSON, `"rework_context"`) {
		t.Fatal("plain new start implicitly imported feedback")
	}
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_rework":true}`, row.Fingerprint))
	response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", body, nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("irrelevant feedback consent accepted=%d %s", response.Code, response.Body.String())
	}
	f.assertUnchanged(t)
}

func TestAIAgentReworkFileContractRetryStillNeedsFileScope(t *testing.T) {
	for _, kind := range []string{"input", "output"} {
		t.Run(kind, func(t *testing.T) {
			f := newAIReworkContractFixture(t)
			body := f.nativeBody([]string{f.Artifact.ID})
			if kind == "output" {
				body["output_contract"] = map[string]any{"type": "file", "name": "revision.md", "mime": "text/markdown"}
			} else {
				files := agentRunFileTestFixture{aiAgentRunFixture: f.aiAgentRunFixture, Artifacts: f.Service.artifactStore}
				artifact := files.addTaskArtifact(t, f.Task.ID, "reference.md", []byte("SEPARATE_CONTROLLED_REFERENCE"))
				body["input_files"] = []any{map[string]any{"source_kind": "task_artifact", "id": artifact.ID}}
				body["confirm_file_access"] = true
				body["file_access_provider_confirmation"] = body["rework_provider_confirmation"]
			}
			encoded, _ := json.Marshal(body)
			response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", encoded, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("native file-enabled rework=%d %s", response.Code, response.Body.String())
			}
			var run models.AgentRun
			if err := f.Store.DB.First(&run, "execution_contract_version=4").Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			args := aiRetryArgs(f.aiAgentRunFixture, run)
			if result, err := f.Tool.Execute(context.Background(), []byte(args)); err == nil || result != "" {
				t.Fatal("v4 file input/output retry bypassed independent agent_files")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			_, fileTool := aiAgentFileTools(t, f.aiAgentRunFixture)
			row := proposeTestAction(t, f.Store, fileTool, args)
			if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			decision := map[string]any{"fingerprint": row.Fingerprint, "decision": "confirm", "confirm_agent_execution": true, "confirm_agent_rework": true}
			if kind == "input" {
				encoded, _ := json.Marshal(decision)
				response := performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", encoded, nil)
				if response.Code != http.StatusUnprocessableEntity {
					t.Fatalf("v4 input bytes silently consented=%d %s", response.Code, response.Body.String())
				}
				decision["confirm_agent_files"] = true
			}
			encoded, _ = json.Marshal(decision)
			response = performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", encoded, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("file-enabled v4 retry=%d %s", response.Code, response.Body.String())
			}
			var child models.AgentRun
			if err := f.Store.DB.First(&child, "parent_run_id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if child.InputSnapshotJSON != run.InputSnapshotJSON {
				t.Fatal("file retry did not preserve exact reviewed input")
			}
		})
	}
}

func TestAIAgentReworkFileDriftIsCheckedAtApprovalAndPrelaunch(t *testing.T) {
	for _, phase := range []string{"approval", "prelaunch"} {
		t.Run(phase, func(t *testing.T) {
			f := newAIReworkContractFixture(t)
			files := agentRunFileTestFixture{aiAgentRunFixture: f.aiAgentRunFixture, Artifacts: f.Service.artifactStore}
			artifact := files.addTaskArtifact(t, f.Task.ID, "reference.md", []byte("original reference"))
			execution, proposalTool := aiAgentFileTools(t, f.aiAgentRunFixture)
			candidate, _ := listedAIAgentFileCandidate(t, execution, f.Task.ID)
			var action map[string]any
			if err := json.Unmarshal(f.proposalBody([]string{f.Artifact.ID}), &action); err != nil {
				t.Fatal(err)
			}
			action["changes"].(map[string]any)["input_file_candidate_ids"] = []string{candidate.CandidateID}
			body, _ := json.Marshal(action)
			row := proposeTestAction(t, f.Store, proposalTool, string(body))
			if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			body = []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_execution":true,"confirm_agent_rework":true,"confirm_agent_files":true}`, row.Fingerprint))
			path := "/api/v1/ai/actions/" + row.ID + "/decision"
			if phase == "prelaunch" {
				if response := performRequest(f.Native, http.MethodPost, path, body, nil); response.Code != http.StatusOK {
					t.Fatalf("approve file rework=%d %s", response.Code, response.Body.String())
				}
			}
			absolute, err := f.Service.artifactStore.resolveObject(*artifact.RelativePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(absolute, []byte("tampered reference"), 0600); err != nil {
				t.Fatal(err)
			}
			if phase == "approval" {
				response := performRequest(f.Native, http.MethodPost, path, body, nil)
				if response.Code != http.StatusConflict {
					t.Fatalf("drifted file rework approved=%d %s", response.Code, response.Body.String())
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			} else {
				var run models.AgentRun
				if err := f.Store.DB.First(&run, "execution_contract_version=4").Error; err != nil {
					t.Fatal(err)
				}
				if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err == nil {
					t.Fatal("v4 prelaunch accepted changed actual input bytes")
				}
			}
		})
	}
}

func TestAIAgentReworkNativeFileRetryRequiresBothIndependentConsents(t *testing.T) {
	f := newAIReworkContractFixture(t)
	files := agentRunFileTestFixture{aiAgentRunFixture: f.aiAgentRunFixture, Artifacts: f.Service.artifactStore}
	const referenceBody = "PRIVATE_RETRY_FILE_INPUT_NOT_IN_DETAILS"
	artifact := files.addTaskArtifact(t, f.Task.ID, "reference.md", []byte(referenceBody))
	input := f.nativeBody([]string{f.Artifact.ID})
	input["input_files"] = []any{map[string]any{"source_kind": "task_artifact", "id": artifact.ID}}
	input["confirm_file_access"] = true
	input["file_access_provider_confirmation"] = input["rework_provider_confirmation"]
	body, _ := json.Marshal(input)
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("file+rework create=%d %s", response.Code, response.Body.String())
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "execution_contract_version=4").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_EXECUTION_FAILED", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	detail := performRequest(f.Router, http.MethodGet, "/api/v1/agent-runs/"+run.ID, nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"input_files"`) || !strings.Contains(detail.Body.String(), artifact.ID) || !strings.Contains(detail.Body.String(), `"rework_provider_confirmation"`) || strings.Contains(detail.Body.String(), referenceBody) || strings.Contains(detail.Body.String(), *artifact.RelativePath) {
		t.Fatalf("native retry details missing frozen metadata or leaked file body: %d %s", detail.Code, detail.Body.String())
	}
	path := "/api/v1/agent-runs/" + run.ID + "/retry"
	for name, decision := range map[string]map[string]any{
		"rework-consent-only": {"confirm_rework_context": true, "rework_provider_confirmation": input["rework_provider_confirmation"]},
		"file-consent-only":   {"confirm_file_access": true, "file_access_provider_confirmation": input["file_access_provider_confirmation"]},
		"stale-file-provider": {"confirm_rework_context": true, "rework_provider_confirmation": input["rework_provider_confirmation"], "confirm_file_access": true, "file_access_provider_confirmation": map[string]any{"version": 1, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind}},
	} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(decision)
			response := performRequest(f.Native, http.MethodPost, path, body, nil)
			if response.Code < 400 || response.Code >= 500 {
				t.Fatalf("missing independent native consent accepted=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
		})
	}
	body, _ = json.Marshal(map[string]any{"confirm_rework_context": true, "rework_provider_confirmation": input["rework_provider_confirmation"], "confirm_file_access": true, "file_access_provider_confirmation": input["file_access_provider_confirmation"]})
	response = performRequest(f.Native, http.MethodPost, path, body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("separately approved native v4 file retry=%d %s", response.Code, response.Body.String())
	}
	var child models.AgentRun
	if err := f.Store.DB.First(&child, "parent_run_id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if child.InputSnapshotJSON != run.InputSnapshotJSON || child.Attempt != run.Attempt+1 || child.ExecutionContractVersion != 4 {
		t.Fatal("native retry changed frozen feedback/files")
	}
}

func TestAIAgentReworkActiveReferenceBlocksDeletionUntilCancelled(t *testing.T) {
	f := newAIReworkContractFixture(t)
	run := f.createRework(t)
	path := "/api/v1/artifacts/" + f.Artifact.ID + "?confirm=true"
	headers := map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)}
	if response := performRequest(f.Router, http.MethodDelete, path, []byte(`{"reason":"source still needed"}`), headers); response.Code != http.StatusConflict {
		t.Fatalf("queued revision source deleted=%d %s", response.Code, response.Body.String())
	}
	response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel queued revision=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='cancelled'", 1, run.ID)
	if response := performRequest(f.Router, http.MethodDelete, path, []byte(`{"reason":"cancelled source no longer in use"}`), headers); response.Code != http.StatusOK {
		t.Fatalf("terminal reference blocks legal deletion=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
}

func TestAIAgentReworkOutputReadScopeCannotGrantExecution(t *testing.T) {
	f := newAIReworkContractFixture(t)
	for _, scopes := range [][]string{{"work", "outputs", "actions"}, {"work", "outputs", "output_files", "actions"}} {
		registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		tool, ok := registry.Get("workspace_propose")
		if !ok {
			t.Fatal("regular proposal missing")
		}
		if result, err := tool.Execute(context.Background(), f.proposalBody([]string{f.Artifact.ID})); err == nil || result != "" {
			t.Fatalf("output read permission granted execution: %s %v", result, err)
		}
		f.assertUnchanged(t)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func TestAIAgentReworkRejectsHistoricalSourceAndEncodedOverflow(t *testing.T) {
	t.Run("old changes-requested is not current", func(t *testing.T) {
		f := newAIReworkContractFixture(t)
		revised := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/submit-output", []byte(`{"summary":"A later actual batch","artifacts":[]}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
		if revised.Code != http.StatusCreated {
			t.Fatal(revised.Body.String())
		}
		f.Task = decodeSubmitOutputResponse(t, revised.Body.Bytes()).Task
		body, _ := json.Marshal(f.nativeBody([]string{f.Artifact.ID}))
		response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
		if response.Code < 400 || response.Code >= 500 {
			t.Fatalf("historical batch accepted=%d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested'", 1, f.Submission.ID)
	})
	t.Run("encoded context cannot silently truncate", func(t *testing.T) {
		f := newAIReworkContractFixture(t, strings.Repeat("<", 11000))
		body, _ := json.Marshal(f.nativeBody([]string{f.Artifact.ID}))
		response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("escaped context capacity accepted=%d %s", response.Code, response.Body.String())
		}
		f.assertUnchanged(t)
		body, _ = json.Marshal(f.nativeBody([]string{}))
		response = performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("explicit reason-only fallback rejected=%d %s", response.Code, response.Body.String())
		}
		var run models.AgentRun
		if err := f.Store.DB.First(&run, "execution_contract_version=4").Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(run.InputSnapshotJSON, reworkTestReason) || strings.Contains(run.InputSnapshotJSON, f.Artifact.ID) {
			t.Fatal("reason-only input silently included unselected oversized output")
		}
	})
}

func TestAIAgentReworkPendingDeliveryRecoversWithoutRerunningOrLosingHistory(t *testing.T) {
	f := newAIReworkContractFixture(t)
	run := f.createRework(t)
	if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_rework_delivery BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted' BEGIN SELECT RAISE(ABORT,'injected rework output rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	f.Service.options.Logger = log.New(io.Discard, "", 0)
	f.Service.finalizeAgentRunObserved(run, "Revised output awaiting durable registration.", "", f.Service.options.Now().Add(2*time.Minute).Format(time.RFC3339Nano))
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='running' AND output_delivery_status='pending'", 1, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
	if response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil); response.Code != http.StatusConflict {
		t.Fatalf("cancel discarded pending rework=%d %s", response.Code, response.Body.String())
	}
	if err := f.Store.DB.Exec("DROP TRIGGER fail_rework_delivery").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ids, err := f.Service.recoverAgentRunsOnStartup(f.Service.options.Now().Add(3 * time.Minute))
		if err != nil || len(ids) != 0 {
			t.Fatalf("pending rework resumed executor rather than delivery: %v %v", ids, err)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='succeeded' AND output_delivery_status='submitted' AND input_snapshot_json=?", 1, run.ID, run.InputSnapshotJSON)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested' AND review_reason=?", 1, f.Submission.ID, reworkTestReason)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='task_output_submitted' AND agent_run_id=?", 1, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, f.Task.ID)
}

func TestAIAgentReworkNativeRetryCannotRerunDeliveredOrPendingResults(t *testing.T) {
	for _, state := range []string{"retained", "submitted", "pending"} {
		t.Run(state, func(t *testing.T) {
			f := newAIReworkContractFixture(t)
			run := f.createRework(t)
			if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
				t.Fatal(err)
			}
			if state == "retained" {
				// A native review assignment's absence can retain an otherwise
				// successful result without changing this Run's frozen Task/assignee.
				if err := f.Store.DB.Model(&models.TaskAssignment{}).Where("task_id=? AND role='reviewer' AND unassigned_at IS NULL", f.Task.ID).Update("unassigned_at", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)).Error; err != nil {
					t.Fatal(err)
				}
			}
			completed := f.Service.options.Now().Add(2 * time.Minute).Format(time.RFC3339Nano)
			if state == "pending" {
				if err := f.Store.DB.Exec(`CREATE TRIGGER fail_rework_for_retry BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted' BEGIN SELECT RAISE(ABORT,'injected pending'); END`).Error; err != nil {
					t.Fatal(err)
				}
				f.Service.options.Logger = log.New(io.Discard, "", 0)
				f.Service.finalizeAgentRunObserved(run, "An existing complete result must not be regenerated.", "", completed)
			} else if err := f.Service.finalizeAgentRun(run, "An existing complete result must not be regenerated.", "", completed); err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if run.OutputDeliveryStatus != state {
				t.Fatalf("fixture disposition=%s want=%s", run.OutputDeliveryStatus, state)
			}
			if state == "retained" {
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, f.Task.ID, run.TaskVersion)
			}
			confirmation := f.nativeBody([]string{})
			body, _ := json.Marshal(map[string]any{"confirm_rework_context": true, "rework_provider_confirmation": confirmation["rework_provider_confirmation"]})
			response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/retry", body, nil)
			if response.Code != http.StatusConflict {
				t.Fatalf("v4 %s result unexpectedly reran=%d %s", state, response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND output_delivery_status=?", 1, run.ID, state)
		})
	}
}

func TestAIAgentReworkHarnessReadsActualFeedbackBeforeHumanStart(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f := newAIReworkContractFixture(t)
			finishAIGeneration(t, f.Store, f.Generation)
			var taskVersion int64
			var submissionID, artifactID, proposalID string
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				emit := func(tool string, args map[string]any) {
					encoded, _ := json.Marshal(args)
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("rework-%d", call), tool, string(encoded))
				}
				result := func(previous int, tool string) string {
					return aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("rework-%d", previous), tool)
				}
				switch call {
				case 1:
					emit("workspace_guide", map[string]any{"topic": "outputs"})
				case 2:
					emit("workspace_task_submissions", map[string]any{"task_id": f.Task.ID, "view": "list", "status": "changes_requested"})
				case 3:
					var page struct {
						TaskVersion int64                         `json:"task_version"`
						Current     *string                       `json:"current_submission_id"`
						Items       []struct{ ID, Status string } `json:"items"`
					}
					content := result(2, "workspace_task_submissions")
					if json.Unmarshal([]byte(content), &page) != nil || page.Current == nil || len(page.Items) != 1 || page.Items[0].ID != *page.Current || page.Items[0].Status != "changes_requested" {
						t.Errorf("returned batch not actually discovered: %s", content)
						writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
						return
					}
					taskVersion, submissionID = page.TaskVersion, *page.Current
					emit("workspace_task_submissions", map[string]any{"task_id": f.Task.ID, "view": "text", "submission_id": submissionID, "field": "review_reason"})
				case 4:
					var page struct {
						Content string `json:"content"`
						Next    *int   `json:"next_content_offset"`
					}
					content := result(3, "workspace_task_submissions")
					if json.Unmarshal([]byte(content), &page) != nil || page.Content != reworkTestReason || page.Next != nil {
						t.Errorf("actual full review reason missing: %s", content)
					}
					emit("workspace_task_submissions", map[string]any{"task_id": f.Task.ID, "view": "artifacts", "submission_id": submissionID})
				case 5:
					var page struct {
						Items []struct {
							ID   string `json:"id"`
							Kind string `json:"storage_kind"`
						} `json:"items"`
					}
					content := result(4, "workspace_task_submissions")
					if json.Unmarshal([]byte(content), &page) != nil || len(page.Items) != 1 || page.Items[0].Kind != "text" {
						t.Errorf("actual non-file source missing: %s", content)
						writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
						return
					}
					artifactID = page.Items[0].ID
					emit("workspace_guide", map[string]any{"topic": "agents"})
				case 6:
					emit("workspace_agent_execution", map[string]any{"task_id": f.Task.ID})
				case 7:
					var eligibility struct {
						Eligible  bool                        `json:"eligible"`
						Task      aiAgentRunEligibilityTask   `json:"task"`
						Providers []aiAgentRunProviderPreview `json:"providers"`
					}
					content := result(6, "workspace_agent_execution")
					if json.Unmarshal([]byte(content), &eligibility) != nil || !eligibility.Eligible || eligibility.Task.ID != f.Task.ID || eligibility.Task.Version != taskVersion {
						t.Errorf("current execution identity missing: %s", content)
					}
					var provider aiAgentRunProviderPreview
					for _, candidate := range eligibility.Providers {
						if candidate.ID == f.Provider.ID {
							provider = candidate
						}
					}
					if provider.ID == "" {
						t.Error("actual execution Provider unavailable")
					}
					emit("workspace_propose", map[string]any{"action": "agent_run.start", "task_id": eligibility.Task.ID, "expected_version": eligibility.Task.Version, "changes": map[string]any{"provider_id": provider.ID, "expected_provider_version": provider.Version, "expected_provider_config_version": provider.ConfigVersion, "rework_submission_id": submissionID, "rework_artifact_ids": []string{artifactID}}})
				case 8:
					var proposal struct {
						ID string `json:"proposal_id"`
					}
					content := result(7, "workspace_propose")
					if json.Unmarshal([]byte(content), &proposal) != nil || proposal.ID == "" || !strings.Contains(content, "NOT executed") {
						t.Errorf("rework did not remain pending human approval: %s", content)
					}
					proposalID = proposal.ID
					writeAIBudgetTextTurn(w, protocol, `已根据当前返工意见提出修订执行，请核验旧产出与意见后单独确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				case 9:
					if aiBudgetHasTool(payload, "workspace_propose") || aiBudgetHasTool(payload, "workspace_agent_execution") || strings.Contains(string(raw), reworkTestReason) || strings.Contains(string(raw), reworkTestOriginal) {
						t.Error("new ungranted message inherited rework permission or tool evidence")
					}
					writeAIBudgetTextTurn(w, protocol, `本条没有执行权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected Provider turn %d", call)
					writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": f.Generation.SessionID, "message": "读取任务当前真实返工意见，选择该批次的文字产出作参考，提出修订执行，仍需我另行确认。任务：" + f.Task.ID, "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}})
			response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 8 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("rework Harness=%d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			f.assertUnchanged(t)
			var proposal models.AIActionProposal
			if err := f.Store.DB.First(&proposal, "id=?", proposalID).Error; err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(proposal.PreviewJSON, reworkTestReason) || !strings.Contains(proposal.PreviewJSON, reworkTestOriginal) {
				t.Fatal("human preview omitted actual selected feedback or prior output")
			}
			confirmation, _ := json.Marshal(map[string]any{"fingerprint": proposal.Fingerprint, "decision": "confirm", "confirm_agent_execution": true, "confirm_agent_rework": true})
			response = performRequest(f.Native, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", confirmation, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("independent rework approval=%d %s", response.Code, response.Body.String())
			}
			var run models.AgentRun
			if err := f.Store.DB.First(&run, "execution_contract_version=4").Error; err != nil {
				t.Fatal(err)
			}
			if run.ParentRunID != nil || run.TaskVersion != taskVersion || !strings.Contains(run.InputSnapshotJSON, reworkTestReason) || !strings.Contains(run.InputSnapshotJSON, reworkTestOriginal) {
				t.Fatal("new revision lost exact reviewed context")
			}
			if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err != nil {
				t.Fatalf("approved new rework cannot pass prelaunch: %v", err)
			}
			if err := f.Store.DB.Model(&run).Updates(map[string]any{"status": "running", "started_at": run.CreatedAt}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Service.finalizeAgentRun(run, "Revised third item is present; independent review remains required.", "", f.Service.options.Now().Add(3*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 2, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested' AND review_reason=?", 1, f.Submission.ID, reworkTestReason)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='succeeded' AND output_delivery_status='submitted'", 1, run.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages WHERE context_snapshot LIKE ?", 0, "%REWORK_REASON_ONLY%")
			next, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": f.Generation.SessionID, "message": "谢谢"})
			response = performRequest(f.Router, http.MethodPost, "/api/v1/ai/chat", next, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 9 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("ungranted followup=%d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
		})
	}
}
