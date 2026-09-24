package api

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAgentRunOutputTransactionCancellationReleasesConnection(t *testing.T) {
	f := newAIAgentRunFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := withAgentRunOutputTransaction(f.Store.DB.WithContext(ctx), func(tx *gorm.DB) error {
		if e := tx.Model(&models.Task{}).Where("id=?", f.Task.ID).Update("title", "uncommitted").Error; e != nil {
			return e
		}
		cancel()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND title=?", 1, f.Task.ID, f.Task.Title)
	if err = f.Store.DB.Transaction(func(tx *gorm.DB) error { return tx.Exec("SELECT 1").Error }); err != nil {
		t.Fatalf("connection retained transaction: %v", err)
	}
}

// A future approval caller must be able to fail AFTER output creation without
// leaving a delivered Run, a changed Task, a submission or an orphaned file.
func TestAgentRunOutputRecoveryJoinsOuterDecisionTransaction(t *testing.T) {
	for _, kind := range []string{agentexec.ResultTypeText, agentexec.ResultTypeFile} {
		for _, commitFailure := range []bool{false, true} {
			name := kind + "/callback_failure"
			if commitFailure {
				name = kind + "/commit_failure"
			}
			t.Run(name, func(t *testing.T) {
				f := newAgentRunFileTestFixture(t)
				addAgentRunOwnerReviewer(t, f.aiAgentRunFixture)
				input := prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Artifacts}
				if kind == agentexec.ResultTypeFile {
					input.OutputContract = &agentRunOutputContractRequest{Type: kind, Name: "recovered.md", MIME: "text/markdown"}
				}
				prepared, err := prepareAgentRun(f.Store.DB, input)
				if err != nil {
					t.Fatal(err)
				}
				run := createRunningAgentRunFromPrepared(t, f, prepared)
				body := "# 待登记的原始结果\nNo second model call."
				completedAt := f.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
				if err = f.Service.persistPendingAgentRunOutput(run.ID, body, completedAt); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadDir(f.Artifacts.objectsDir)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Store.DB.Exec(`CREATE TABLE recovery_decision_guard (
					id TEXT PRIMARY KEY, task_id TEXT NOT NULL,
					FOREIGN KEY(task_id) REFERENCES tasks(id) DEFERRABLE INITIALLY DEFERRED
				)`).Error; err != nil {
					t.Fatal(err)
				}
				files := agentRunOutputFileDelivery{api: f.Service, store: f.Artifacts}
				decisionErr := errors.New("injected decision failure after delivery")
				err = withAgentRunOutputTransaction(f.Store.DB, func(tx *gorm.DB) error {
					result, e := f.Service.recoverAgentRunOutputInTransaction(tx, run.ID, f.Service.options.Now(), &files)
					if e != nil {
						return e
					}
					if result.OutputDeliveryStatus != agentRunOutputSubmitted || result.SubmissionID == nil {
						t.Fatalf("uncommitted delivery=%#v", result)
					}
					if !commitFailure {
						return decisionErr
					}
					return tx.Exec("INSERT INTO recovery_decision_guard(id,task_id) VALUES (?,?)", run.ID, "missing-task").Error
				})
				if err == nil {
					t.Fatal("outer decision unexpectedly committed")
				}
				if !commitFailure && !errors.Is(err, decisionErr) {
					t.Fatalf("unexpected callback failure: %v", err)
				}
				if cleanupErr := files.finish(err); cleanupErr == nil {
					t.Fatal("compensation hid transaction failure")
				}
				var pending models.AgentRun
				if err = f.Store.DB.First(&pending, "id=?", run.ID).Error; err != nil {
					t.Fatal(err)
				}
				if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending || pending.OutputDeliveryPendingText == nil || *pending.OutputDeliveryPendingText != body || pending.SubmissionID != nil || pending.ArtifactID != nil {
					t.Fatalf("rollback lost staging: %#v", pending)
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, run.TaskID)
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_output_submitted' AND agent_run_id=?", 0, run.ID)
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM recovery_decision_guard", 0)
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=? AND status=?", 1, f.Task.ID, f.Task.Version, f.Task.Status)
				after, err := os.ReadDir(f.Artifacts.objectsDir)
				if err != nil || len(after) != len(before) {
					t.Fatalf("orphan file: before=%d after=%d err=%v", len(before), len(after), err)
				}
				var recovered models.AgentRun
				for range 2 {
					recovered, err = f.Service.recoverAgentRunOutput(run.ID, f.Service.options.Now())
					if err != nil || recovered.OutputDeliveryStatus != agentRunOutputSubmitted {
						t.Fatalf("recovery=%#v err=%v", recovered, err)
					}
				}
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, run.TaskID)
				assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, run.TaskID)
				if recovered.ResultText == nil || *recovered.ResultText != body || recovered.OutputDeliveryPendingText != nil {
					t.Fatal("recovery replaced output or retained staging")
				}
			})
		}
	}
}
