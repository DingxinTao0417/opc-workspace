package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITaskReviewPolicyContextUsesWorkScopeWithoutSubmissionContent(t *testing.T) {
	f := newAIAgentRunFixture(t)
	registry, err := f.Service.aiChatToolRegistry("policy-read", false, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	get, ok := registry.Get("workspace_get")
	if !ok {
		t.Fatal("work cannot read task")
	}
	for _, name := range []string{"workspace_outputs", "workspace_propose", "workspace_agent_execution"} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("read-only task metadata granted %s", name)
		}
	}
	read := func(allowed bool) {
		t.Helper()
		encoded, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"task","id":%q}`, f.Task.ID)))
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Record aiBusinessContextSource `json:"record"`
		}
		if err := json.Unmarshal([]byte(encoded), &body); err != nil {
			t.Fatal(err)
		}
		if body.Record.Fields["review_policy"] != "manual" || body.Record.Fields["review_policy_change_allowed"] != allowed {
			t.Fatalf("missing or wrong policy facts: %s", encoded)
		}
		for _, private := range []string{"PRIVATE SUBMISSION", "PRIVATE ACTOR NOTES", "submitted_by_actor_id", "current_submission_id", "input_snapshot", "submission_count"} {
			if strings.Contains(encoded, private) {
				t.Fatalf("policy capability exposed %s", private)
			}
		}
	}
	read(true)

	// Historical, withdrawn output still locks a future policy change; its
	// existence is projected only into the boolean, not into output access.
	owner, now := models.BuiltinOwnerActorID, f.Task.CreatedAt
	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: f.Task.ID, Sequence: 1, Status: "withdrawn",
		Summary: "PRIVATE SUBMISSION", SubmittedByActorID: owner, SubmittedAt: now,
		WithdrawnByActorID: &owner, WithdrawnAt: &now,
	}
	if err := f.Store.DB.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
	read(false)
	preview := performRequest(f.Router, http.MethodPost, "/api/v1/ai/context/preview", []byte(fmt.Sprintf(
		`{"provider_id":%q,"sources":[{"type":"task","id":%q}]}`, f.Provider.ID, f.Task.ID)), nil)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"review_policy":"manual"`) ||
		!strings.Contains(preview.Body.String(), `"review_policy_change_allowed":false`) || strings.Contains(preview.Body.String(), submission.ID) || strings.Contains(preview.Body.String(), submission.Summary) {
		t.Fatalf("explicit context differs from safe task metadata: %d %s", preview.Code, preview.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions", 1)
}

func TestAITaskReviewPolicyContextDoesNotPromiseChangeOutsideTodo(t *testing.T) {
	for _, policy := range []string{"none", "manual"} {
		f := newAIAgentRunFixture(t)
		if err := f.Store.DB.Model(&f.Task).Update("review_policy", policy).Error; err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"todo", "in_progress"} {
			if err := f.Store.DB.Model(&f.Task).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			source, err := loadAITaskContext(context.Background(), f.Store.DB, f.Task.ID)
			if err != nil || source.Fields["review_policy"] != policy || source.Fields["review_policy_change_allowed"] != (status == "todo") {
				t.Fatalf("policy=%s status=%s source=%+v err=%v", policy, status, source, err)
			}
		}
	}
}
