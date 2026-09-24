package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestProjectTaskInputRequestRejectsAmbiguousConsentSources(t *testing.T) {
	for _, raw := range []string{
		`{"source_kind":"task_artifact","id":"11111111-1111-4111-8111-111111111111","source_task":null}`,
		`{"source_kind":"task_artifact","id":"11111111-1111-4111-8111-111111111111","sha256":""}`,
		`{"source_kind":"project_task_artifact","source_kind":"task_artifact","id":"11111111-1111-4111-8111-111111111111"}`,
		`{"Source_kind":"task_artifact","id":"11111111-1111-4111-8111-111111111111"}`,
	} {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			var request agentRunInputFileRequest
			if json.Unmarshal([]byte(raw), &request) == nil {
				t.Fatal("ambiguous source/proof must be rejected before consent is evaluated")
			}
		})
	}
}

func TestProjectTaskFileAIEncodedBudgetKeepsAllCandidatesReachable(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(int, map[string]any, http.ResponseWriter) { t.Error("metadata must not execute") })
	if err := f.Store.DB.Model(&models.Task{}).Where("id=?", f.SourceTask.ID).Update("title", strings.Repeat("&", 200)).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		f.addTaskArtifactWithMutation(t, f.SourceTask.ID, strings.Repeat("&", 240)+fmt.Sprintf("%02d.md", i), []byte("bounded text"), func(artifact *models.TaskArtifact) {
			artifact.SubmissionID = f.SourceSubmission.ID
			artifact.Position = i + 3
		})
	}
	generation := uuid.NewString()
	tool := aiWorkspaceTool{api: f.Service, name: "workspace_agent_project_files", providerID: f.Provider.ID, configVersion: f.Provider.ConfigVersion, generationID: generation, agentExecutionRun: newAIAgentExecutionRun(generation), policy: harness.NewCapabilities("work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files")}
	seen := map[string]bool{}
	offset := 0
	for page := 0; page < 10; page++ {
		encoded, err := tool.Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"task_id":%q,"offset":%d}`, f.Task.ID, offset)))
		if err != nil {
			t.Fatalf("valid metadata page %d became unreachable: %v", offset, err)
		}
		if len(encoded) > 24<<10 {
			t.Fatal("encoded metadata exceeds tool budget")
		}
		var result aiAgentProjectFilesResult
		if json.Unmarshal([]byte(encoded), &result) != nil {
			t.Fatal("invalid result")
		}
		for _, candidate := range result.InputFileCandidates {
			if seen[candidate.CandidateID] {
				t.Fatal("duplicate candidate across pages")
			}
			seen[candidate.CandidateID] = true
		}
		if result.NextOffset == nil {
			break
		}
		if *result.NextOffset <= offset {
			t.Fatal("cursor made no progress")
		}
		offset = *result.NextOffset
	}
	if len(seen) != 22 {
		t.Fatalf("only %d/22 candidates reachable", len(seen))
	}
}
