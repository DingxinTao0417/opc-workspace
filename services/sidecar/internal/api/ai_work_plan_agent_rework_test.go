package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A rework attempt is a new execution; only its later failure can be replaced
// by a retry step. The plan must preserve that exact v4 lineage without exposing
// the private feedback or old artifact text to its metadata projection.
func TestAIWorkPlanAgentReworkRetryKeepsFrozenLineage(t *testing.T) {
	f := newAIReworkContractFixture(t)
	source := proposeTestAction(t, f.Store, f.Tool, string(f.proposalBody([]string{f.Artifact.ID})))
	parent := f.createRework(t)
	now := f.Service.options.Now().Format(time.RFC3339Nano)
	if err := f.Store.DB.Model(&parent).Updates(map[string]any{"status": "failed", "started_at": now, "completed_at": now, "error_code": "TEST_FAILED"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&source).Updates(map[string]any{"status": "confirmed", "result_id": parent.ID, "result_version": 1, "decided_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	plan := planTool(t, f.aiAgentRunFixture)
	base := sampleWorkPlan(source.ID)
	decodePlanTool(t, plan, updatePlanArgs(base, 0))
	retry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, parent))
	next := replacementWorkPlan(base)
	next.Steps[2].ProposalID = retry.ID
	view := decodePlanTool(t, plan, updatePlanArgs(next, 1))
	if view.Steps[1].State != "failed" || view.Steps[2].State != "pending" || view.Steps[2].Satisfied || view.Steps[2].Evidence.RetryOfRunID != parent.ID {
		t.Fatalf("v4 pending retry lost its source failure: %+v", view)
	}
	child := aiPlanSeedConfirmedRetry(t, f.aiAgentRunFixture, retry, parent)
	if child.ExecutionContractVersion != 4 || child.InputSnapshotJSON != parent.InputSnapshotJSON {
		t.Fatal("plan retry did not preserve the complete v4 snapshot")
	}
	// Before delivery seals the execution row, even an otherwise valid v4
	// context cannot silently replace its bound parent's frozen evidence.
	snapshot, err := parseAgentRunV4Snapshot(child.InputSnapshotJSON)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Rework.ReviewReason = "Different feedback must not retire the original execution."
	changed, _ := json.Marshal(snapshot)
	if err := f.Store.DB.Model(&child).Update("input_snapshot_json", string(changed)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Execute(context.Background(), updatePlanArgs(next, 1)); err == nil {
		t.Fatal("plan replay accepted a changed v4 rework snapshot")
	}
	if err := f.Store.DB.Model(&child).Update("input_snapshot_json", parent.InputSnapshotJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&child).Updates(map[string]any{"status": "running", "started_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(child, "修订后的第三项证据。", "", now); err != nil {
		t.Fatal(err)
	}
	view = decodePlanTool(t, plan, updatePlanArgs(next, 1))
	if !view.Steps[2].Satisfied || view.Steps[1].State != "failed" || view.Steps[1].Satisfied {
		t.Fatalf("delivered retry rewrote failure or failed to satisfy execution: %+v", view)
	}
	encoded, _ := json.Marshal(view)
	for _, private := range []string{reworkTestReason, reworkTestOriginal, "rework_context", "input_snapshot_json"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("plan metadata exposed frozen rework input")
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested'", 1, f.Submission.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 3)
}
