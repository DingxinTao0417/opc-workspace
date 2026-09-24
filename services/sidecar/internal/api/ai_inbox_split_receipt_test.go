package api

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInboxSplitExactHistoricalReceipt(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	inbox := createInboxItemForTest(t, router, `{"title":"receipt inbox"}`, "")
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"parent","title":"Private parent","is_required":true,"assignee_actor_id":%q},{"key":"child","parent_key":"parent","title":"Private child","is_required":true,"assignee_actor_id":%q}]}}`, inbox.ID, models.BuiltinOwnerActorID, models.BuiltinOwnerActorID))
	before, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || strings.Contains(before, "created_task_ids") {
		t.Fatalf("premature receipt: %s %v", before, err)
	}
	finishAIGeneration(t, store, generation)
	result := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if len(result.CreatedTaskIDs) != 2 {
		t.Fatalf("missing exact result: %#v", result)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND parent_task_id=?", 1, result.CreatedTaskIDs[1], result.CreatedTaskIDs[0])
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 2)
	decideTestAction(t, router, row, "")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 2)
	if err := store.DB.Model(&models.Task{}).Where("id=?", result.CreatedTaskIDs[0]).Update("title", "Renamed later").Error; err != nil {
		t.Fatal(err)
	}
	body, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range result.CreatedTaskIDs {
		if !strings.Contains(body, id) {
			t.Fatalf("lost identity: %s", body)
		}
	}
	for _, private := range []string{"Private", "Renamed", "parent_key", "changes", "preview"} {
		if strings.Contains(body, private) {
			t.Fatalf("leaked %s: %s", private, body)
		}
	}
	if body, err := service.aiActionReceipts(context.Background(), uuid.NewString()); err != nil || body != "" {
		t.Fatalf("foreign receipt: %s %v", body, err)
	}
	if err := store.DB.First(&row, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Synthetic historical identities preserve the real immutable event.
	row.ID = uuid.NewString()
	legacy, err := aiActionOutputWithFacts(store.DB, row, "completed", service.options.Now())
	if err != nil || legacy.CreatedTaskIDs != nil {
		t.Fatalf("legacy guessed result: %#v %v", legacy, err)
	}
	if err := recordAIActionEvent(store.DB, row, "ai_workspace_action_confirmed", "", *row.DecidedAt); err != nil {
		t.Fatal(err)
	}
	legacy, err = aiActionOutputWithFacts(store.DB, row, "completed", service.options.Now())
	if err != nil || legacy.CreatedTaskIDs != nil {
		t.Fatalf("legacy event guessed result: %#v %v", legacy, err)
	}
	row.ID = uuid.NewString()
	if err := recordAIActionEvent(store.DB, row, "ai_workspace_action_confirmed", "", *row.DecidedAt, result.CreatedTaskIDs[0], result.CreatedTaskIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := aiActionOutputWithFacts(store.DB, row, "completed", service.options.Now()); err == nil {
		t.Fatal("accepted duplicate task identities")
	}
}

func TestAIInboxSplitReceiptEventFailureRollsBackTasks(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	inbox := createInboxItemForTest(t, router, `{"title":"rollback inbox"}`, "")
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"one","title":"Not committed","is_required":true,"assignee_actor_id":%q}]}}`, inbox.ID, models.BuiltinOwnerActorID))
	finishAIGeneration(t, store, generation)
	if err := store.DB.Exec(`CREATE TRIGGER test_split_receipt_failure BEFORE INSERT ON workflow_events WHEN NEW.action='ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT,'test receipt failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code < 400 {
		t.Fatal("expected receipt transaction failure")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE id=? AND status='pending'", 1, row.ID)
}
