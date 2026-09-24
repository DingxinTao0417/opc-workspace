package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAIActionCapacityProvidesActionableFeedbackWithoutDroppingProposals(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	first := `{"action":"task.create","changes":{"title":"First pending task"}}`
	proposal := proposeTestAction(t, store, tool, first)
	for i := 1; i < 8; i++ {
		proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task.create","changes":{"title":"Pending task %d"}}`, i))
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"action":"task.create","changes":{"title":"Ninth task"}}`)); err == nil || !strings.Contains(err.Error(), "AI_ACTION_PROPOSAL_LIMIT") || !strings.Contains(err.Error(), "new message") {
		t.Fatalf("capacity must describe a safe next step: %v", err)
	}
	// An identical retry resolves the original proposal even at capacity.
	if again := proposeTestAction(t, store, tool, first); again.ID != proposal.ID {
		t.Fatal("duplicate proposal replaced existing approval")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 8)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIActionUnavailableConversationFeedbackDoesNotCreateProposal(t *testing.T) {
	_, store, _, tool, generation := aiActionTestFixture(t)
	finishAIGeneration(t, store, generation)
	_, err := tool.Execute(context.Background(), []byte(`{"action":"task.create","changes":{"title":"Unavailable run"}}`))
	if err == nil || !strings.Contains(err.Error(), "AI_ACTION_CONVERSATION_UNAVAILABLE") {
		t.Fatalf("wrong conversation feedback: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIActionPreviewCapacityDoesNotSuggestTruncatingEvidence(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	fields := map[string]any{"title": "Large current evidence", "description": strings.Repeat("d", 10000), "completion_criteria": strings.Repeat("c", 10000)}
	body, _ := json.Marshal(fields)
	task := createTaskForTaskFacts(t, router, string(body))
	args := fmt.Sprintf(`{"action":"task.update","task_id":%q,"expected_version":%d,"changes":{"description":%q,"completion_criteria":%q}}`, task.ID, task.Version, strings.Repeat("e", 10000), strings.Repeat("f", 10000))
	_, err := tool.Execute(context.Background(), []byte(args))
	if err == nil || !strings.Contains(err.Error(), "AI_ACTION_PREVIEW_TOO_LARGE") || !strings.Contains(err.Error(), "do not truncate") {
		t.Fatalf("wrong preview feedback: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND priority='P2'", 1, task.ID)
}
