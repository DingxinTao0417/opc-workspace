package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiPlanFailedRunSource(t *testing.T, f aiAgentRunFixture, states ...string) (models.AIActionProposal, models.AgentRun) {
	t.Helper()
	proposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
	run := seedQueuedFrozenAgentRun(t, f)
	now := f.Service.options.Now().Format(time.RFC3339Nano)
	status := "failed"
	if len(states) > 0 {
		status = states[0]
	}
	updates := map[string]any{"status": status, "started_at": now, "completed_at": now}
	if status == "failed" {
		updates["error_code"] = "TEST_FAILED"
	}
	if err := f.Store.DB.Model(&run).Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&proposal).Updates(map[string]any{"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	return proposal, run
}

func TestAIWorkPlanAgentRetryFailedRunCanBeReplacedByExactPendingRetry(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal, run := aiPlanFailedRunSource(t, f)
	base := sampleWorkPlan(proposal.ID)
	tool := planTool(t, f)
	decodePlanTool(t, tool, updatePlanArgs(base, 0))
	retry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f, run))
	next := replacementWorkPlan(base)
	next.Steps[2].ProposalID = retry.ID
	view := decodePlanTool(t, tool, updatePlanArgs(next, 1))
	if old := view.Steps[1]; old.State != "failed" || old.Satisfied || old.SupersededBy != "retry" {
		t.Fatalf("replacement rewrote original failure: %+v", old)
	}
	if current := view.Steps[2]; current.State != "pending" || current.Satisfied || view.Steps[3].Ready {
		t.Fatalf("unconfirmed retry satisfied plan: %+v", view)
	}
	if view.Steps[2].Evidence.RetryOfRunID != run.ID {
		t.Fatal("retry lineage metadata is missing")
	}
	if _, err := tool.Execute(context.Background(), updatePlanArgs(next, 1)); err != nil {
		t.Fatalf("idempotent replay lost retry evidence: %v", err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
}

type aiPlanRetryFixture struct {
	aiAgentRunFixture
	Source models.AIActionProposal
	Parent models.AgentRun
	Retry  models.AIActionProposal
	Next   aiWorkPlanIntent
	Plan   harness.Tool
}

func newAIPlanRetryFixture(t *testing.T) aiPlanRetryFixture {
	t.Helper()
	f := newAIAgentRunFixture(t)
	source, parent := aiPlanFailedRunSource(t, f)
	plan := planTool(t, f)
	base := sampleWorkPlan(source.ID)
	decodePlanTool(t, plan, updatePlanArgs(base, 0))
	retry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f, parent))
	next := replacementWorkPlan(base)
	next.Steps[2].ProposalID = retry.ID
	return aiPlanRetryFixture{aiAgentRunFixture: f, Source: source, Parent: parent, Retry: retry, Next: next, Plan: plan}
}

func (f *aiPlanRetryFixture) nextGeneration(t *testing.T) {
	t.Helper()
	finishAIGeneration(t, f.Store, f.Generation)
	f.Generation.ID, f.Generation.Status = uuid.NewString(), "streaming"
	if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
		t.Fatal(err)
	}
	tool := *f.Tool.(*aiWorkspaceTool)
	tool.generationID = f.Generation.ID
	f.Tool = &tool
	f.Plan = planTool(t, f.aiAgentRunFixture)
}

// Create authoritative frozen execution facts without launching any process or
// Provider. Approval execution itself is covered by the native command tests.
func aiPlanSeedConfirmedRetry(t *testing.T, f aiAgentRunFixture, proposal models.AIActionProposal, parent models.AgentRun) models.AgentRun {
	t.Helper()
	prepared, _, err := prepareAgentRunRetry(f.Store.DB, parent.ID, f.Service.artifactStore)
	if err != nil {
		t.Fatal(err)
	}
	var child models.AgentRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		var createErr error
		child, createErr = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "plan-retry-fixture", f.Service.options.Now().Format(time.RFC3339Nano))
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&proposal).Updates(map[string]any{"status": "confirmed", "result_id": child.ID, "result_version": 1, "decided_at": child.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	return child
}

func TestAIWorkPlanAgentRetryPendingDecisionAndGenerationStatesStayReadable(t *testing.T) {
	for _, sourceStatus := range []string{"failed", "cancelled", "interrupted"} {
		for _, candidateStatus := range []string{"pending", "rejected", "expired", "unavailable"} {
			t.Run(sourceStatus+"/"+candidateStatus, func(t *testing.T) {
				f := newAIAgentRunFixture(t)
				proposal, run := aiPlanFailedRunSource(t, f, sourceStatus)
				base := sampleWorkPlan(proposal.ID)
				tool := planTool(t, f)
				decodePlanTool(t, tool, updatePlanArgs(base, 0))
				retry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f, run))
				next := replacementWorkPlan(base)
				next.Steps[2].ProposalID = retry.ID
				decodePlanTool(t, tool, updatePlanArgs(next, 1))
				now := f.Service.options.Now()
				switch candidateStatus {
				case "rejected":
					if err := f.Store.DB.Model(&retry).Updates(map[string]any{"status": "rejected", "decided_at": now.Format(time.RFC3339Nano)}).Error; err != nil {
						t.Fatal(err)
					}
				case "expired":
					now = now.Add(25 * time.Hour)
				case "unavailable":
					if err := f.Store.DB.Model(&f.Generation).Update("status", "failed").Error; err != nil {
						t.Fatal(err)
					}
				}
				var row aiWorkPlanRevision
				if err := f.Store.DB.Take(&row, "session_id=? AND version=2", f.Generation.SessionID).Error; err != nil {
					t.Fatal(err)
				}
				view, err := projectAIWorkPlan(f.Store.DB, row, now)
				if err != nil || view.Steps[1].State != sourceStatus || view.Steps[1].Satisfied || view.Steps[1].SupersededBy != "retry" ||
					view.Steps[2].State != candidateStatus || view.Steps[2].Satisfied || view.Steps[3].Ready {
					t.Fatalf("valid unexecuted retry became unreadable or successful: %+v %v", view, err)
				}
				if progress := summarizeAIWorkPlanInbox(view, "test", now.Format(time.RFC3339Nano)); progress.State == "completed" || progress.SupersededStepTotal != 1 {
					t.Fatalf("false completion: %+v", progress)
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			})
		}
	}
}

func TestAIWorkPlanAgentRetryRejectsChangedActionAndTarget(t *testing.T) {
	for _, kind := range []string{"unbound", "unrelated_action", "wrong_run", "wrong_task", "source_fingerprint", "target_fingerprint", "source_unknown_field", "target_unknown_field", "missing_target", "foreign_session", "pending_with_result"} {
		t.Run(kind, func(t *testing.T) {
			f := newAIPlanRetryFixture(t)
			encoded, _ := json.Marshal(f.Next)
			row := aiWorkPlanRevision{SessionID: f.Generation.SessionID, GenerationID: f.Generation.ID, Version: 2, PlanJSON: string(encoded)}
			view, err := projectAIWorkPlan(f.Store.DB, row, f.Service.options.Now())
			if err != nil {
				t.Fatal(err)
			}
			facts := map[string]aiWorkPlanActionFacts{}
			for id, proposal := range map[string]models.AIActionProposal{"record": f.Source, "retry": f.Retry} {
				if err := f.Store.DB.Take(&proposal, "id=?", proposal.ID).Error; err != nil {
					t.Fatal(err)
				}
				receipt, err := aiActionOutputWithFacts(f.Store.DB, proposal, "streaming", f.Service.options.Now())
				if err != nil {
					t.Fatal(err)
				}
				facts[id] = aiWorkPlanActionFacts{Proposal: proposal, Receipt: receipt}
			}
			readProjection := false
			switch kind {
			case "unbound":
				f.Next.Steps[2].ProposalID = ""
				readProjection = true
			case "unrelated_action":
				other := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"unrelated success"}}`)
				f.Next.Steps[2].ProposalID = other.ID
				readProjection = true
			case "wrong_run", "wrong_task", "source_unknown_field", "target_unknown_field":
				id := "retry"
				if kind == "source_unknown_field" {
					id = "record"
				}
				target := facts[id]
				var action map[string]any
				if err := json.Unmarshal([]byte(target.Proposal.ActionJSON), &action); err != nil {
					t.Fatal(err)
				}
				if kind == "wrong_run" {
					action["agent_run_id"] = uuid.NewString()
				} else if kind == "wrong_task" {
					action["task_id"] = uuid.NewString()
				} else {
					action["untrusted_override"] = true
				}
				encoded, _ := json.Marshal(action)
				target.Proposal.ActionJSON, target.Proposal.Fingerprint = string(encoded), sha256Hex(encoded)
				facts[id] = target
			case "source_fingerprint", "target_fingerprint":
				id := "retry"
				if kind == "source_fingerprint" {
					id = "record"
				}
				target := facts[id]
				target.Proposal.Fingerprint = strings.Repeat("0", 64)
				facts[id] = target
			case "missing_target":
				if err := f.Store.DB.Delete(&f.Retry).Error; err != nil {
					t.Fatal(err)
				}
				readProjection = true
			case "foreign_session":
				session := models.AISession{ID: uuid.NewString(), Persist: true, Title: "foreign", Version: 1, CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
				generation := f.Generation
				generation.ID, generation.SessionID = uuid.NewString(), session.ID
				if err := f.Store.DB.Create(&session).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.Create(&generation).Error; err != nil {
					t.Fatal(err)
				}
				target := f.Retry
				target.ID, target.GenerationID = uuid.NewString(), generation.ID
				if err := f.Store.DB.Create(&target).Error; err != nil {
					t.Fatal(err)
				}
				f.Next.Steps[2].ProposalID = target.ID
				readProjection = true
			case "pending_with_result":
				target := facts["retry"]
				target.Receipt.ResultID = &f.Parent.ID
				facts["retry"] = target
			}
			// Corrupted immutable proposals are injected at the strict projection
			// seam; do not disable database protections merely to construct them.
			if readProjection {
				_, err = f.Plan.Execute(context.Background(), updatePlanArgs(f.Next, 1))
			} else {
				err = projectAIWorkPlanReplacements(f.Store.DB, view, facts)
			}
			if err == nil {
				t.Fatal("invalid replacement evidence was accepted")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
		})
	}
}

func TestAIWorkPlanAgentRetryFrozenChildDriftFailsClosed(t *testing.T) {
	for _, column := range []string{"parent_run_id", "task_version", "assignment_assigned_at", "actor_version", "adapter_version", "provider_version", "provider_config_version", "execution_contract_version", "input_snapshot_json"} {
		t.Run(column, func(t *testing.T) {
			f := newAIPlanRetryFixture(t)
			child := aiPlanSeedConfirmedRetry(t, f.aiAgentRunFixture, f.Retry, f.Parent)
			view := decodePlanTool(t, f.Plan, updatePlanArgs(f.Next, 1))
			if view.Steps[2].State != "queued" || view.Steps[2].Satisfied {
				t.Fatalf("queued retry became complete: %+v", view)
			}
			var value any = int64(99)
			switch column {
			case "parent_run_id":
				value = nil
			case "assignment_assigned_at":
				value = "2000-01-01T00:00:00Z"
			case "execution_contract_version":
				value = agentRunExecutionContractVersionV2
			case "input_snapshot_json":
				value = `{"PRIVATE_CHANGED_INPUT":true}`
			}
			if err := f.Store.DB.Model(&child).Update(column, value).Error; err != nil {
				t.Fatal(err)
			}
			var row aiWorkPlanRevision
			if err := f.Store.DB.Take(&row, "session_id=? AND version=2", f.Generation.SessionID).Error; err != nil {
				t.Fatal(err)
			}
			if view, err := projectAIWorkPlan(f.Store.DB, row, f.Service.options.Now()); err == nil || view != nil {
				t.Fatalf("drifted child retired original execution: %+v %v", view, err)
			}
			if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(f.Next, 1)); err == nil {
				t.Fatal("replay trusted drifted execution")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
		})
	}
}

func TestAIWorkPlanAgentRetrySourceCannotHidePendingOrProducedOutput(t *testing.T) {
	for _, mutate := range []func(*aiPlanRetryRun){
		func(run *aiPlanRetryRun) { run.Status = "queued" },
		func(run *aiPlanRetryRun) { run.Status = "running" },
		func(run *aiPlanRetryRun) { run.Status = "succeeded" },
		func(run *aiPlanRetryRun) { run.OutputDeliveryStatus = agentRunOutputPending },
		func(run *aiPlanRetryRun) { run.OutputDeliveryStatus = agentRunOutputSubmitted },
		func(run *aiPlanRetryRun) { run.OutputDeliveryStatus = agentRunOutputRetained },
		func(run *aiPlanRetryRun) { run.HasOutput = true },
	} {
		run := aiPlanRetryRun{AgentRun: models.AgentRun{Status: "failed", OutputDeliveryStatus: agentRunOutputNotReady}}
		if !aiPlanRetryableRun(run) {
			t.Fatal("failed empty Run is not retryable")
		}
		mutate(&run)
		if aiPlanRetryableRun(run) {
			t.Fatalf("non-retryable or produced output may be hidden: %+v", run)
		}
	}
	f := newAIAgentRunFixture(t)
	run := seedRunningAgentRun(t, f)
	if err := f.Service.persistPendingAgentRunOutput(run.ID, "PRIVATE staging", f.Service.options.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	facts, err := readAIPlanRetryRun(f.Store.DB, run.ID)
	if err != nil || !facts.HasOutput || aiPlanRetryableRun(facts) || facts.OutputDeliveryPendingText != nil {
		t.Fatalf("pending output presence was lost or body was read: hasOutput=%v err=%v", facts.HasOutput, err)
	}
}

func TestAIWorkPlanAgentRetryUnconfirmedPreviewMustPreserveFrozenIdentity(t *testing.T) {
	f := newAIPlanRetryFixture(t)
	parent, err := readAIPlanRetryRun(f.Store.DB, f.Parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := aiActionOutputWithFacts(f.Store.DB, f.Retry, "streaming", f.Service.options.Now())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(receipt)
	for _, test := range []struct {
		name   string
		mutate func(*aiActionResponse)
	}{
		{"missing retry preview", func(r *aiActionResponse) { r.Preview.AgentRunRetry = nil }},
		{"wrong source run", func(r *aiActionResponse) { r.Preview.AgentRunRetry.RunID = uuid.NewString() }},
		{"wrong source status", func(r *aiActionResponse) { r.Preview.AgentRunRetry.Status = "succeeded" }},
		{"wrong source attempt", func(r *aiActionResponse) { r.Preview.AgentRunRetry.Attempt++ }},
		{"missing start preview", func(r *aiActionResponse) { r.Preview.AgentRunStart = nil }},
		{"wrong task", func(r *aiActionResponse) { r.Preview.AgentRunStart.Task.ID = uuid.NewString() }},
		{"task version", func(r *aiActionResponse) { r.Preview.AgentRunStart.Task.Version++ }},
		{"assignment identity", func(r *aiActionResponse) { r.Preview.AgentRunStart.Assignment.ID = uuid.NewString() }},
		{"assignment timestamp", func(r *aiActionResponse) { r.Preview.AgentRunStart.Assignment.AssignedAt = "changed" }},
		{"assignment actor", func(r *aiActionResponse) { r.Preview.AgentRunStart.Assignment.ActorID = uuid.NewString() }},
		{"agent identity", func(r *aiActionResponse) { r.Preview.AgentRunStart.Agent.ID = uuid.NewString() }},
		{"agent version", func(r *aiActionResponse) { r.Preview.AgentRunStart.Agent.Version++ }},
		{"adapter identity", func(r *aiActionResponse) { r.Preview.AgentRunStart.Adapter.ID = uuid.NewString() }},
		{"adapter version", func(r *aiActionResponse) { r.Preview.AgentRunStart.Adapter.Version++ }},
		{"provider identity", func(r *aiActionResponse) { r.Preview.AgentRunStart.Provider.ID = uuid.NewString() }},
		{"provider version", func(r *aiActionResponse) { r.Preview.AgentRunStart.Provider.Version++ }},
		{"provider config", func(r *aiActionResponse) { r.Preview.AgentRunStart.Provider.ConfigVersion++ }},
		{"provider model", func(r *aiActionResponse) { r.Preview.AgentRunStart.Provider.Model = "changed" }},
		{"contract", func(r *aiActionResponse) { r.Preview.AgentRunStart.ExecutionContractVersion++ }},
		{"no new attempt", func(r *aiActionResponse) { r.Preview.AgentRunStart.Attempt = parent.Attempt }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var current aiActionResponse
			if err := json.Unmarshal(encoded, &current); err != nil {
				t.Fatal(err)
			}
			test.mutate(&current)
			if err := validateAIPlanRetryTarget(f.Store.DB, parent, aiWorkPlanActionFacts{Proposal: f.Retry, Receipt: current}); err == nil {
				t.Fatal("unconfirmed retry changed frozen source identity")
			}
		})
	}
}

func TestAIWorkPlanAgentRetryRejectedRetryCannotLaunderOriginalFailure(t *testing.T) {
	for _, status := range []string{"rejected", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := newAIPlanRetryFixture(t)
			decodePlanTool(t, f.Plan, updatePlanArgs(f.Next, 1))
			if status == "rejected" {
				if err := f.Store.DB.Model(&f.Retry).Updates(map[string]any{"status": "rejected", "decided_at": f.Service.options.Now().Format(time.RFC3339Nano)}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				now := f.Service.options.Now().Add(25 * time.Hour)
				f.Service.options.Now = func() time.Time { return now }
			}
			f.nextGeneration(t)
			next := f.Next
			next.Steps = append([]aiWorkPlanStep{}, f.Next.Steps[:3]...)
			next.Steps = append(next.Steps, aiWorkPlanStep{ID: "retry_again", Title: "Try original failed Run again", Kind: "action", DependsOn: []string{"research"}, Replaces: "retry"}, f.Next.Steps[3])
			next.Steps[4].DependsOn = []string{"retry_again"}
			unrelated := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Do not launder failed execution"}}`)
			next.Steps[3].ProposalID = unrelated.ID
			if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(next, 2)); err == nil {
				t.Fatal("rejected/expired retry washed original failure through an unrelated action")
			}
			newRetry := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, f.Parent))
			next.Steps[3].ProposalID = newRetry.ID
			view := decodePlanTool(t, f.Plan, updatePlanArgs(next, 2))
			if view.Steps[1].State != "failed" || view.Steps[2].State != status || view.Steps[3].State != "pending" ||
				view.Steps[1].SupersededBy != "retry" || view.Steps[2].SupersededBy != "retry_again" || view.Steps[4].Ready {
				t.Fatalf("exact retry chain lost facts: %+v", view)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
		})
	}
}

func TestAIWorkPlanAgentRetryFailedChildRequiresItsOwnRunAndAllowsSkippedAttempts(t *testing.T) {
	f := newAIPlanRetryFixture(t)
	child := aiPlanSeedConfirmedRetry(t, f.aiAgentRunFixture, f.Retry, f.Parent)
	if err := f.Store.DB.Model(&child).Updates(map[string]any{"status": "failed", "started_at": child.CreatedAt, "completed_at": child.CreatedAt, "error_code": "TEST_FAILED"}).Error; err != nil {
		t.Fatal(err)
	}
	decodePlanTool(t, f.Plan, updatePlanArgs(f.Next, 1))
	f.nextGeneration(t)
	next := f.Next
	next.Steps = append([]aiWorkPlanStep{}, f.Next.Steps[:3]...)
	next.Steps = append(next.Steps, aiWorkPlanStep{ID: "retry_child", Title: "Retry failed child", Kind: "action", DependsOn: []string{"research"}, Replaces: "retry"}, f.Next.Steps[3])
	next.Steps[4].DependsOn = []string{"retry_child"}
	wrongParent := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, f.Parent))
	next.Steps[3].ProposalID = wrongParent.ID
	if _, err := f.Plan.Execute(context.Background(), updatePlanArgs(next, 2)); err == nil {
		t.Fatal("failed actual child was replaced by retrying its older ancestor")
	}
	correct := proposeTestAction(t, f.Store, f.Tool, aiRetryArgs(f.aiAgentRunFixture, child))
	next.Steps[3].ProposalID = correct.ID
	view := decodePlanTool(t, f.Plan, updatePlanArgs(next, 2))
	if view.Steps[3].Evidence.RetryOfRunID != child.ID {
		t.Fatal("next chain link did not move to the actual failed child")
	}
	// A separate attempt may exist between parent and the proposal's new run.
	// Native allocation is Task+Actor MAX(attempt)+1, so gaps are legitimate.
	gapped := newAIPlanRetryFixture(t)
	unrelatedAttempt := seedFailedFrozenAIRun(t, gapped.aiAgentRunFixture)
	if unrelatedAttempt.Attempt != gapped.Parent.Attempt+1 {
		t.Fatal("bad gap fixture")
	}
	gapped.nextGeneration(t)
	retry := proposeTestAction(t, gapped.Store, gapped.Tool, aiRetryArgs(gapped.aiAgentRunFixture, gapped.Parent))
	gapped.Next.Steps[2].ProposalID = retry.ID
	last := aiPlanSeedConfirmedRetry(t, gapped.aiAgentRunFixture, retry, gapped.Parent)
	if last.Attempt != gapped.Parent.Attempt+2 {
		t.Fatalf("bad native attempt gap: %d", last.Attempt)
	}
	view = decodePlanTool(t, gapped.Plan, updatePlanArgs(gapped.Next, 1))
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "input_snapshot") || strings.Contains(string(encoded), "provider_config") || strings.Contains(string(encoded), "assignment_assigned_at") {
		t.Fatalf("frozen identity escaped metadata-only plan: %s", encoded)
	}
	if view.Steps[2].State != "queued" || view.Steps[1].SupersededBy != "retry" {
		t.Fatalf("legitimate gapped retry rejected: %s", encoded)
	}
	// Projection compares immutable Run facts, not mutable current Task state.
	if err := gapped.Store.DB.Model(&gapped.Task).Update("version", gapped.Task.Version+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := gapped.Plan.Execute(context.Background(), []byte(`{"operation":"read"}`)); err != nil {
		t.Fatalf("current Task drift invalidated historical retry receipt: %v", err)
	}
}
