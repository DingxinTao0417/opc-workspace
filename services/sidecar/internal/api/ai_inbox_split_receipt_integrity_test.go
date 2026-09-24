package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIInboxSplitReceiptRejectsMismatchedApprovalFacts(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	inbox := createInboxItemForTest(t, router, `{"title":"receipt integrity"}`, "")
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.split","inbox_item_id":%q,"expected_version":1,"changes":{"tasks":[{"key":"one","title":"Exact identity","is_required":true,"assignee_actor_id":%q}]}}`, inbox.ID, models.BuiltinOwnerActorID))
	finishAIGeneration(t, store, generation)
	result := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if err := store.DB.First(&row, "id=?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"generation_id", "proposal_id", "fingerprint", "result_id", "result_version", "empty_ids", "noncanonical_id", "wrong_count", "wrong_type", "duplicate_events"} {
		t.Run(field, func(t *testing.T) {
			// Synthetic immutable audit events never mutate the real approval.
			candidate := row
			candidate.ID = uuid.NewString()
			payload := map[string]any{"generation_id": candidate.GenerationID, "proposal_id": candidate.ID, "fingerprint": candidate.Fingerprint, "result_id": candidate.ResultID, "result_version": candidate.ResultVersion, "created_task_ids": result.CreatedTaskIDs}
			switch field {
			case "result_version":
				payload[field] = *candidate.ResultVersion + 1
			case "empty_ids":
				payload["created_task_ids"] = []string{}
			case "noncanonical_id":
				payload["created_task_ids"] = []string{"{" + result.CreatedTaskIDs[0] + "}"}
			case "wrong_count":
				payload["created_task_ids"] = []string{result.CreatedTaskIDs[0], uuid.NewString()}
			case "wrong_type":
				payload["created_task_ids"] = "not an array"
			case "duplicate_events":
			default:
				payload[field] = uuid.NewString()
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			copies := 1
			if field == "duplicate_events" {
				copies = 2
			}
			for i := 0; i < copies; i++ {
				if err := store.DB.Table("workflow_events").Create(map[string]any{"id": uuid.NewString(), "aggregate_type": "ai_action_proposal", "aggregate_id": candidate.ID, "action": "ai_workspace_action_confirmed", "actor_id": models.BuiltinOwnerActorID, "current_json": string(encoded), "created_at": *candidate.DecidedAt}).Error; err != nil {
					t.Fatal(err)
				}
			}
			out, err := aiActionOutputWithFacts(store.DB, candidate, "completed", service.options.Now())
			if err == nil || len(out.CreatedTaskIDs) != 0 {
				t.Fatalf("accepted malformed receipt: %#v %v", out.CreatedTaskIDs, err)
			}
		})
	}
}
