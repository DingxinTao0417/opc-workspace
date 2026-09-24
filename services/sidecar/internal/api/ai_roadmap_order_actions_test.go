package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func aiRoadmapOrderFixture(t *testing.T) []models.RoadmapMilestone {
	t.Helper()
	titles := []string{"季度甲", "季度乙", "季度丙"}
	rows := make([]models.RoadmapMilestone, len(titles))
	for index := range rows {
		rows[index] = models.RoadmapMilestone{
			ID: uuid.NewString(), Title: titles[index], Year: 2026, Quarter: 3,
			TargetDate: "2026-09-30", Status: "planned", ManualOrder: int64(index+1) * roadmapMilestoneOrderStep,
			Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z",
		}
	}
	return rows
}

func aiRoadmapMoveArguments(source, anchor models.RoadmapMilestone, placement string) string {
	return fmt.Sprintf(`{"action":"roadmap_milestone.move","roadmap_milestone_id":%q,"expected_version":%d,"changes":{"anchor_milestone_id":%q,"expected_anchor_version":%d,"placement":%q}}`,
		source.ID, source.Version, anchor.ID, anchor.Version, placement)
}

func TestAIRoadmapMoveApprovesCompleteQuarterAtomically(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	rows := aiRoadmapOrderFixture(t)
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	proposal := proposeTestAction(t, store, tool, aiRoadmapMoveArguments(rows[2], rows[0], "before"))
	if !strings.Contains(proposal.PreviewJSON, `"roadmap_order"`) || !strings.Contains(proposal.PreviewJSON, `"before_position":3`) || !strings.Contains(proposal.PreviewJSON, `"after_position":1`) {
		t.Fatalf("preview=%s", proposal.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND version=1 AND manual_order=?", 1, rows[2].ID, 3*roadmapMilestoneOrderStep)
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, proposal, ""))
	if receipt.ResultID == nil || *receipt.ResultID != rows[2].ID || receipt.ResultVersion == nil || *receipt.ResultVersion != 2 || receipt.Route != "/roadmap?milestone="+rows[2].ID {
		t.Fatalf("receipt=%#v", receipt)
	}
	var ordered []models.RoadmapMilestone
	if err := store.DB.Order("manual_order ASC").Find(&ordered).Error; err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{rows[2].ID, rows[0].ID, rows[1].ID} {
		if ordered[index].ID != id || ordered[index].ManualOrder != int64(index+1)*roadmapMilestoneOrderStep || ordered[index].Version != 2 {
			t.Fatalf("ordered[%d]=%+v", index, ordered[index])
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
}

func TestAIRoadmapMoveRejectsQuarterDriftAndInvalidTargets(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	rows := aiRoadmapOrderFixture(t)
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	other := rows[0]
	other.ID, other.Quarter, other.TargetDate = uuid.NewString(), 4, "2026-12-31"
	if err := store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{
		aiRoadmapMoveArguments(rows[0], rows[0], "before"),
		aiRoadmapMoveArguments(rows[0], other, "before"),
		aiRoadmapMoveArguments(rows[1], rows[0], "after"),
		strings.Replace(aiRoadmapMoveArguments(rows[2], rows[0], "before"), `"placement":"before"`, `"placement":"first"`, 1),
		strings.Replace(aiRoadmapMoveArguments(rows[2], rows[0], "before"), `"expected_anchor_version":1`, `"expected_anchor_version":0`, 1),
	} {
		if _, err := tool.Execute(t.Context(), []byte(args)); err == nil {
			t.Fatalf("invalid move accepted: %s", args)
		}
	}
	proposal := proposeTestAction(t, store, tool, aiRoadmapMoveArguments(rows[2], rows[0], "before"))
	finishAIGeneration(t, store, generation)
	newRow := rows[0]
	newRow.ID, newRow.Title, newRow.ManualOrder = uuid.NewString(), "新加入", 4*roadmapMilestoneOrderStep
	if err := store.DB.Create(&newRow).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	assertAPIError(t, response, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND version=1 AND manual_order=?", 1, rows[2].ID, 3*roadmapMilestoneOrderStep)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
}

func TestAIRoadmapMoveApprovalFailureRollsBackWholeQuarter(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	rows := aiRoadmapOrderFixture(t)
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	proposal := proposeTestAction(t, store, tool, aiRoadmapMoveArguments(rows[2], rows[0], "before"))
	finishAIGeneration(t, store, generation)
	if err := store.DB.Exec(`CREATE TRIGGER fail_ai_roadmap_order_approval BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type = 'ai_action_proposal' AND NEW.action = 'ai_workspace_action_confirmed' BEGIN SELECT RAISE(ABORT, 'TEST_CONFIRMATION_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("forced failure=%d %s", response.Code, response.Body.String())
	}
	for index, row := range rows {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND version=1 AND manual_order=?", 1, row.ID, int64(index+1)*roadmapMilestoneOrderStep)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
}

func TestAIRoadmapMoveRejectsQuarterOverCapacity(t *testing.T) {
	_, store, _, tool, _ := aiActionTestFixture(t)
	rows := aiRoadmapOrderFixture(t)
	for index := len(rows); index < 101; index++ {
		row := rows[0]
		row.ID = uuid.NewString()
		row.Title = fmt.Sprintf("里程碑 %d", index)
		row.ManualOrder = int64(index+1) * roadmapMilestoneOrderStep
		rows = append(rows, row)
	}
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), []byte(aiRoadmapMoveArguments(rows[2], rows[0], "before"))); err == nil || !strings.Contains(err.Error(), "more than 100") {
		t.Fatalf("expected capacity rejection, got %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
