package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAgentRestartNativePreviewIsRequired(t *testing.T) {
	f := newAIAgentRunFixture(t)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = stopped
	source := seedQueuedFrozenAgentRun(t, f)
	if err := f.Service.failQueuedAgentRunIdentity(source.ID, f.Service.options.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	native := gin.New()
	native.POST("/api/v1/tasks/:id/agent-runs", f.Service.createAgentRun)
	body, _ := json.Marshal(map[string]any{"provider_id": f.Provider.ID, "restart": map[string]any{"run_id": source.ID, "expected_task_version": f.Task.Version}})
	response := performRequest(native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create without explicit preview/consent=%d: %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	var unchanged models.AgentRun
	if err := f.Store.DB.First(&unchanged, "id=?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != "failed" || unchanged.ResultText != nil {
		t.Fatal("source mutated")
	}
}

type nativeAgentRestartFixture struct {
	aiAgentRunFixture
	Native *gin.Engine
	Source models.AgentRun
}

func newNativeAgentRestartFixture(t *testing.T, terminal string) nativeAgentRestartFixture {
	t.Helper()
	f := newAIAgentRunFixture(t)
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	f.Service.agentRunLifecycleContext = stopped
	source := seedQueuedFrozenAgentRun(t, f)
	if terminal != "queued" {
		if err := f.Store.DB.Model(&source).Updates(map[string]any{"status": "running", "started_at": source.CreatedAt}).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.First(&source, "id=?", source.ID).Error; err != nil {
			t.Fatal(err)
		}
		now := f.Service.options.Now().Format(time.RFC3339Nano)
		var err error
		switch terminal {
		case "failed":
			err = f.Service.finalizeAgentRun(source, "", "AGENT_MODEL_FAILED", now)
		case "cancelled":
			err = f.Service.finalizeAgentRun(source, "", "AGENT_RUN_CANCELLED", now)
		case "interrupted":
			err = f.Service.interruptClaimedAgentRun(source.ID, now)
		case "retained":
			err = f.Service.finalizeAgentRun(source, "PRIVATE_RETAINED_OLD_BODY", "", now)
		case "pending":
			err = f.Service.persistPendingAgentRunOutput(source.ID, "PRIVATE_PENDING_BODY", now)
		default:
			t.Fatalf("unknown test terminal %q", terminal)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Store.DB.First(&source, "id=?", source.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	native := gin.New()
	native.POST("/api/v1/tasks/:id/agent-runs", f.Service.createAgentRun)
	native.POST("/api/v1/tasks/:id/agent-runs/restart-preview", f.Service.previewAgentRunRestart)
	native.GET("/api/v1/tasks/:id/agent-runs", f.Service.listTaskAgentRuns)
	native.GET("/api/v1/agent-runs/:id", f.Service.getAgentRun)
	native.GET("/api/v1/agent-runs", f.Service.listAgentRuns)
	return nativeAgentRestartFixture{f, native, source}
}

func (f nativeAgentRestartFixture) selection() map[string]any {
	return map[string]any{"provider_id": f.Provider.ID, "restart": map[string]any{"run_id": f.Source.ID, "expected_task_version": f.Task.Version}}
}

func (f nativeAgentRestartFixture) preview(t *testing.T, input map[string]any) agentRunRestartPreviewResponse {
	t.Helper()
	body, _ := json.Marshal(input)
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs/restart-preview", body, nil)
	if response.Code != 200 {
		t.Fatalf("preview=%d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data agentRunRestartPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Fingerprint) != 64 || payload.Data.SourceRun == nil || payload.Data.SourceRun.RunID != f.Source.ID {
		t.Fatalf("incomplete preview=%+v", payload.Data)
	}
	if !reflect.DeepEqual(payload.Data.SourceRun, payload.Data.CurrentStart.Restart) {
		t.Fatal("source/current_start.restart mismatch")
	}
	if strings.Contains(response.Body.String(), "PRIVATE_RETAINED_OLD_BODY") || strings.Contains(response.Body.String(), f.Provider.BaseURL) {
		t.Fatal("preview leaked prior body or endpoint")
	}
	return payload.Data
}

func (f nativeAgentRestartFixture) create(t *testing.T, selection map[string]any, preview agentRunRestartPreviewResponse, key string) models.AgentRun {
	t.Helper()
	selection["confirm_restart"] = true
	selection["restart_preview_hash"] = preview.Fingerprint
	body, _ := json.Marshal(selection)
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, headers)
	if response.Code != 201 {
		t.Fatalf("create=%d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.RestartOfRunID == nil || *payload.Data.RestartOfRunID != f.Source.ID {
		t.Fatalf("create omitted validated restart=%+v", payload.Data)
	}
	var run models.AgentRun
	if err := f.Store.DB.First(&run, "id=?", payload.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAgentRestartNativeUsesCurrentFactsAndDurableProof(t *testing.T) {
	for _, terminal := range []string{"failed", "cancelled", "interrupted", "retained"} {
		t.Run(terminal, func(t *testing.T) {
			f := newNativeAgentRestartFixture(t, terminal)
			original := f.Source
			if err := f.Store.DB.Model(&f.Task).Updates(map[string]any{"description": "NEW_CURRENT_REQUIREMENTS", "version": f.Task.Version + 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"model": "new-selected-model", "version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1}).Error; err != nil {
				t.Fatal(err)
			}
			selection := f.selection()
			preview := f.preview(t, selection)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
			if preview.CurrentStart.Task.Description != "NEW_CURRENT_REQUIREMENTS" || preview.CurrentStart.Provider.Model != "new-selected-model" {
				t.Fatal("preview recycled old facts")
			}
			run := f.create(t, selection, preview, "restart-key")
			attention := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs?attention=1&page_size=10", nil, nil)
			if attention.Code != http.StatusOK {
				t.Fatalf("restart attention=%d: %s", attention.Code, attention.Body.String())
			}
			var inbox struct {
				Data []agentRunSummaryResponse `json:"data"`
				Meta agentRunListMeta          `json:"meta"`
			}
			if err := json.Unmarshal(attention.Body.Bytes(), &inbox); err != nil {
				t.Fatal(err)
			}
			if inbox.Meta.Total != 1 || len(inbox.Data) != 1 || inbox.Data[0].ID != run.ID {
				t.Fatalf("verified restart must replace the old attention item, not its history: %+v", inbox)
			}
			filtered := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs?attention=1&plan_link=unplanned&status=queued&page_size=1", nil, nil)
			if filtered.Code != http.StatusOK || json.Unmarshal(filtered.Body.Bytes(), &inbox) != nil || inbox.Meta.Total != 1 || len(inbox.Data) != 1 || inbox.Data[0].ID != run.ID {
				t.Fatalf("restart attention filtering/page did not keep the new attempt: %d %s", filtered.Code, filtered.Body.String())
			}
			ledger := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs?page_size=10", nil, nil)
			if ledger.Code != http.StatusOK || json.Unmarshal(ledger.Body.Bytes(), &inbox) != nil || inbox.Meta.Total != 2 {
				t.Fatalf("restart removed a historical Run from the full ledger: %d %s", ledger.Code, ledger.Body.String())
			}
			if run.ParentRunID != nil || run.InputSnapshotJSON == original.InputSnapshotJSON || strings.Contains(run.InputSnapshotJSON, "PRIVATE_RETAINED_OLD_BODY") || run.ExecutionContractVersion != 1 {
				t.Fatalf("new execution contract/lineage=%+v", run)
			}
			proof, err := loadAgentRunRestartProof(f.Store.DB, models.AgentRun{ID: run.ID})
			if err != nil || proof == nil || proof.RunID != original.ID {
				t.Fatalf("durable proof=%+v %v", proof, err)
			}
			if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err != nil {
				t.Fatalf("startup proof rejected: %v", err)
			}
			queued, err := f.Service.recoverAgentRunsOnStartup(f.Service.options.Now())
			if err != nil || len(queued) != 1 || queued[0] != run.ID {
				t.Fatalf("recovery queued=%v err=%v", queued, err)
			}
			for _, path := range []string{"/api/v1/tasks/" + f.Task.ID + "/agent-runs", "/api/v1/agent-runs", "/api/v1/agent-runs/" + run.ID} {
				response := performRequest(f.Native, http.MethodGet, path, nil, nil)
				if response.Code != 200 || !strings.Contains(response.Body.String(), `"restart_of_run_id":"`+original.ID+`"`) {
					t.Fatalf("projection %s=%d %s", path, response.Code, response.Body.String())
				}
			}
			replayed := f.create(t, selection, preview, "restart-key")
			if replayed.ID != run.ID {
				t.Fatal("idempotent restart duplicated run")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 1)
			var after models.AgentRun
			if err := f.Store.DB.First(&after, "id=?", original.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, after) {
				t.Fatal("source was mutated")
			}
		})
	}
}

func TestAgentRestartPreviewRejectsAmbiguousOrUnapprovedSelections(t *testing.T) {
	f := newNativeAgentRestartFixture(t, "failed")
	for _, body := range []string{
		fmt.Sprintf(`{"provider_id":%q,"restart":{"run_id":%q,"run_id":%q,"expected_task_version":1}}`, f.Provider.ID, f.Source.ID, f.Source.ID),
		fmt.Sprintf(`{"provider_id":%q,"restart":{"Run_ID":%q,"expected_task_version":1}}`, f.Provider.ID, f.Source.ID),
		fmt.Sprintf(`{"provider_id":%q,"restart":{"run_id":%q,"expected_task_version":null}}`, f.Provider.ID, f.Source.ID),
		fmt.Sprintf(`{"provider_id":%q,"restart":{"run_id":%q,"expected_task_version":1},"confirm_restart":true}`, f.Provider.ID, f.Source.ID),
		fmt.Sprintf(`{"provider_id":%q,"restart":{"run_id":%q,"expected_task_version":1},"auto_assign":{"actor_id":%q,"expected_task_version":1}}`, f.Provider.ID, f.Source.ID, f.Actor.ID),
	} {
		response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs/restart-preview", []byte(body), nil)
		if response.Code < 400 {
			t.Fatalf("accepted ambiguous preview: %s", body)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
}

func TestAgentRestartRejectsSourceAndPreviewDrift(t *testing.T) {
	for _, terminal := range []string{"queued", "pending"} {
		t.Run(terminal, func(t *testing.T) {
			f := newNativeAgentRestartFixture(t, terminal)
			body, _ := json.Marshal(f.selection())
			response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs/restart-preview", body, nil)
			if response.Code != 409 {
				t.Fatalf("ineligible source=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
		})
	}
	for _, change := range []string{"task", "provider", "output", "source", "assignment"} {
		t.Run(change, func(t *testing.T) {
			f := newNativeAgentRestartFixture(t, "failed")
			selection := f.selection()
			preview := f.preview(t, selection)
			switch change {
			case "task":
				if err := f.Store.DB.Model(&f.Task).Updates(map[string]any{"version": f.Task.Version + 1, "description": "changed"}).Error; err != nil {
					t.Fatal(err)
				}
			case "provider":
				if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1, "model": "changed"}).Error; err != nil {
					t.Fatal(err)
				}
			case "output":
				selection["output_contract"] = map[string]any{"type": "file", "name": "new.md", "mime": "text/markdown"}
			case "source":
				selection["restart"] = map[string]any{"run_id": uuid.NewString(), "expected_task_version": f.Task.Version}
			case "assignment":
				if err := f.Store.DB.Model(&models.TaskAssignment{}).Where("id=?", f.Source.AssignmentID).Update("unassigned_at", f.Service.options.Now().Format(time.RFC3339Nano)).Error; err != nil {
					t.Fatal(err)
				}
			}
			selection["confirm_restart"] = true
			selection["restart_preview_hash"] = preview.Fingerprint
			body, _ := json.Marshal(selection)
			response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
			if response.Code != 409 {
				t.Fatalf("drift %s=%d %s", change, response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
		})
	}
}

func TestAgentRestartEventFailureRollsBackWholeCreation(t *testing.T) {
	f := newNativeAgentRestartFixture(t, "failed")
	selection := f.selection()
	preview := f.preview(t, selection)
	if err := f.Store.DB.Exec(`CREATE TRIGGER test_restart_event_failure BEFORE INSERT ON workflow_events WHEN NEW.action='agent_run_restarted' BEGIN SELECT RAISE(ABORT,'test restart event write failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	selection["confirm_restart"] = true
	selection["restart_preview_hash"] = preview.Fingerprint
	body, _ := json.Marshal(selection)
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, map[string]string{"Idempotency-Key": "rollback-restart"})
	if response.Code != 500 {
		t.Fatalf("event failure=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_queued'", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM idempotency_keys WHERE key='rollback-restart'", 0)
}

func TestAgentRestartProofCannotBeDuplicatedOrMixedWithRetry(t *testing.T) {
	for _, corruption := range []string{"duplicate", "parent", "hash", "cross_task"} {
		t.Run(corruption, func(t *testing.T) {
			f := newNativeAgentRestartFixture(t, "failed")
			selection := f.selection()
			preview := f.preview(t, selection)
			run := f.create(t, selection, preview, "")
			if corruption == "parent" {
				if err := f.Store.DB.Model(&run).Update("parent_run_id", f.Source.ID).Error; err != nil {
					t.Fatal(err)
				}
			} else if corruption == "hash" {
				if err := f.Store.DB.Model(&run).Update("input_snapshot_json", `{"tampered":true}`).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				var event map[string]any
				if err := f.Store.DB.Table("workflow_events").Where("aggregate_id=? AND action='agent_run_restarted'", run.ID).Take(&event).Error; err != nil {
					t.Fatal(err)
				}
				event["id"] = uuid.NewString()
				if corruption == "cross_task" {
					event["aggregate_id"] = uuid.NewString()
				}
				if err := f.Store.DB.Table("workflow_events").Create(event).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadAgentRunRestartProof(f.Store.DB, models.AgentRun{ID: run.ID}); err == nil {
				t.Fatal("corrupted restart evidence accepted")
			}
			attention := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs?attention=1", nil, nil)
			if attention.Code != http.StatusConflict || responseErrorCode(t, attention.Body.Bytes()) != agentRunRestartInvalid {
				t.Fatalf("corrupt restart evidence silently altered the attention list: %d %s", attention.Code, attention.Body.String())
			}
			if err := f.Store.DB.First(&run, "id=?", run.ID).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := validateFrozenAgentRunIdentityWithStore(f.Store.DB, run, f.Service.artifactStore); err == nil {
				t.Fatal("corrupt restart could execute after startup")
			}
		})
	}
}

func TestAgentRestartOrdinaryAndRetryProofDoNotReadPrivateBodies(t *testing.T) {
	f := newNativeAgentRestartFixture(t, "failed")
	reads := 0
	callback := "test_restart_no_private"
	if err := f.Store.DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "agent_runs" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Store.DB.Callback().Query().Remove(callback) })
	proof, err := loadAgentRunRestartProof(f.Store.DB, models.AgentRun{ID: f.Source.ID})
	if err != nil || proof != nil || reads != 0 {
		t.Fatalf("ordinary proof unexpectedly read private Run: proof=%v err=%v reads=%d", proof, err, reads)
	}
}

func TestAgentRestartExactRetryPreservesButDoesNotDuplicateRestartRelation(t *testing.T) {
	f := newNativeAgentRestartFixture(t, "failed")
	selection := f.selection()
	preview := f.preview(t, selection)
	restart := f.create(t, selection, preview, "")
	if err := f.Service.failQueuedAgentRunIdentity(restart.ID, f.Service.options.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	prepared, parent, err := prepareAgentRunRetry(f.Store.DB, restart.ID, f.Service.artifactStore)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Restart != nil || prepared.ParentRunID == nil || *prepared.ParentRunID != restart.ID || prepared.InputSnapshotJSON != restart.InputSnapshotJSON {
		t.Fatal("exact retry changed semantics")
	}
	var child models.AgentRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		child, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "retry-after-restart", f.Service.options.Now().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	proof, err := loadAgentRunRestartProof(f.Store.DB, child)
	if err != nil || proof != nil {
		t.Fatalf("retry got a false direct restart relation: %v %v", proof, err)
	}
	attention := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs?attention=1&page_size=1", nil, nil)
	var inbox struct {
		Data []agentRunSummaryResponse `json:"data"`
		Meta agentRunListMeta          `json:"meta"`
	}
	if attention.Code != http.StatusOK || json.Unmarshal(attention.Body.Bytes(), &inbox) != nil || inbox.Meta.Total != 1 || len(inbox.Data) != 1 || inbox.Data[0].ID != child.ID {
		t.Fatalf("mixed restart/retry chain did not leave only its latest attempt: %d %s", attention.Code, attention.Body.String())
	}
	var event map[string]any
	if err := f.Store.DB.Table("workflow_events").Where("aggregate_id=? AND action='agent_run_restarted'", restart.ID).Take(&event).Error; err != nil {
		t.Fatal(err)
	}
	event["id"] = uuid.NewString()
	if err := f.Store.DB.Table("workflow_events").Create(event).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareAgentRunRetry(f.Store.DB, parent.ID, f.Service.artifactStore); err == nil {
		t.Fatal("native retry bypassed corrupt restart provenance")
	}
}

func TestAgentRestartRejectsCrossTaskSubmittedAndUnfrozenHistoricalSources(t *testing.T) {
	t.Run("cross_task", func(t *testing.T) {
		f := newNativeAgentRestartFixture(t, "failed")
		other := f.Task
		other.ID = uuid.NewString()
		if err := f.Store.DB.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := loadAgentRunRestartSource(f.Store.DB, other.ID, f.Source.ID); err == nil {
			t.Fatal("cross-task source accepted")
		}
	})
	t.Run("submitted", func(t *testing.T) {
		f := newAIReworkContractFixture(t)
		if _, err := loadAgentRunRestartSource(f.Store.DB, f.Task.ID, f.Original.ID); err == nil {
			t.Fatal("submitted source accepted as restart")
		}
	})
	t.Run("legacy_without_frozen_version", func(t *testing.T) {
		f := newNativeAgentRestartFixture(t, "queued")
		if err := f.Store.DB.Model(&f.Source).Update("task_version", 0).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.Service.failQueuedAgentRunIdentity(f.Source.ID, f.Service.options.Now().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if _, err := loadAgentRunRestartSource(f.Store.DB, f.Task.ID, f.Source.ID); err == nil {
			t.Fatal("unfrozen source claimed a verified restart")
		}
	})
}

func TestAgentRestartStillRequiresIndependentFileAndReworkConsent(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		f := newNativeAgentRestartFixture(t, "failed")
		files := agentRunFileTestFixture{aiAgentRunFixture: f.aiAgentRunFixture, Artifacts: f.Service.artifactStore}
		artifact := files.addTaskArtifact(t, f.Task.ID, "reference.md", []byte("EXPLICIT_NEW_FILE"))
		selection := f.selection()
		selection["input_files"] = []any{map[string]any{"source_kind": "task_artifact", "id": artifact.ID}}
		preview := f.preview(t, selection)
		selection["confirm_restart"] = true
		selection["restart_preview_hash"] = preview.Fingerprint
		body, _ := json.Marshal(selection)
		response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
		if response.Code != 422 {
			t.Fatalf("restart confirmation replaced file consent: %d %s", response.Code, response.Body.String())
		}
		selection["confirm_file_access"] = true
		selection["file_access_provider_confirmation"] = map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind}
		run := f.create(t, selection, preview, "")
		if run.ExecutionContractVersion != 2 || strings.Contains(run.InputSnapshotJSON, "EXPLICIT_NEW_FILE") {
			t.Fatal("file snapshot compatibility or body boundary changed")
		}
	})
	t.Run("rework", func(t *testing.T) {
		rework := newAIReworkContractFixture(t)
		source := rework.createRework(t)
		if err := rework.Service.failQueuedAgentRunIdentity(source.ID, rework.Service.options.Now().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if err := rework.Store.DB.First(&source, "id=?", source.ID).Error; err != nil {
			t.Fatal(err)
		}
		rework.Native.POST("/api/v1/tasks/:id/agent-runs/restart-preview", rework.Service.previewAgentRunRestart)
		f := nativeAgentRestartFixture{aiAgentRunFixture: rework.aiAgentRunFixture, Native: rework.Native, Source: source}
		plain := f.preview(t, f.selection())
		if plain.CurrentStart.ReworkContext != nil || plain.CurrentStart.ExecutionContractVersion != 1 {
			t.Fatal("restart implicitly inherited old rework context")
		}
		selection := f.selection()
		selection["rework"] = map[string]any{"submission_id": rework.Submission.ID, "artifact_ids": []string{rework.Artifact.ID}, "expected_task_version": f.Task.Version}
		preview := f.preview(t, selection)
		selection["confirm_restart"] = true
		selection["restart_preview_hash"] = preview.Fingerprint
		body, _ := json.Marshal(selection)
		response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", body, nil)
		if response.Code != 422 {
			t.Fatalf("restart confirmation replaced rework consent: %d %s", response.Code, response.Body.String())
		}
		selection["confirm_rework_context"] = true
		selection["rework_provider_confirmation"] = map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind}
		run := f.create(t, selection, preview, "")
		if run.ExecutionContractVersion != 4 || !strings.Contains(run.InputSnapshotJSON, reworkTestReason) {
			t.Fatal("explicit rework v4 was not preserved")
		}
	})
}
