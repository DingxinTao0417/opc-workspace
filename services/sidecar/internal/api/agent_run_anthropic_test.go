package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const anthropicRunTestKey = "test-anthropic-executor-private-key"

type anthropicAgentRunFixture struct {
	agentRunFileTestFixture
	Native *gin.Engine
	Calls  *atomic.Int32
}

func newAnthropicAgentRunFixture(t *testing.T, response func(int, map[string]any, http.ResponseWriter)) *anthropicAgentRunFixture {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("executor used the wrong protocol endpoint: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		call := int(calls.Add(1))
		if r.Header.Get("x-api-key") != anthropicRunTestKey || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("Anthropic execution must use the isolated key/version headers, not OpenAI authorization")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("invalid Messages request: %v", err)
		}
		if request["model"] != "safe-model" || request["max_tokens"] != float64(8192) || request["tools"] != nil {
			t.Errorf("unfrozen model/token budget or unexpected tools: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		response(call, request, w)
	}))
	t.Cleanup(server.Close)
	f := newAgentRunFileTestFixture(t)
	addAgentRunOwnerReviewer(t, f.aiAgentRunFixture)
	f.Service.options.Logger = log.New(io.Discard, "", 0)
	f.Service.keyStore = keystore.NewMemoryStore()
	if err := f.Service.keyStore.Set(aiProviderKeyService, aiProviderKeyAccount(f.Provider.ID), anthropicRunTestKey); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{
		"kind": "remote", "protocol": "anthropic_messages", "base_url": server.URL, "has_key": true,
		"version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&f.Provider, "id=?", f.Provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	native := gin.New()
	native.POST("/api/v1/tasks/:id/agent-runs", f.Service.createAgentRun)
	native.GET("/api/v1/agent-runs/:id", f.Service.getAgentRun)
	native.POST("/api/v1/agent-runs/:id/retry", f.Service.retryAgentRun)
	native.POST("/api/v1/agent-runs/:id/cancel", f.Service.cancelAgentRun)
	native.POST("/api/v1/agent-runs/:id/retry-output-delivery", f.Service.retryAgentRunOutputDelivery)
	return &anthropicAgentRunFixture{agentRunFileTestFixture: f, Native: native, Calls: calls}
}

func writeAnthropicAgentRunResponse(w http.ResponseWriter, text, stopReason string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "msg_isolated_executor", "type": "message", "role": "assistant", "model": "safe-model",
		"content": []map[string]any{{"type": "text", "text": text}}, "stop_reason": stopReason, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 24, "output_tokens": 12},
	})
}

func (f *anthropicAgentRunFixture) create(t *testing.T, extra string) models.AgentRun {
	t.Helper()
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs",
		[]byte(fmt.Sprintf(`{"provider_id":%q%s}`, f.Provider.ID, extra)), nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("native Anthropic creation=%d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Data models.AgentRun `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.ID == "" || result.Data.ExecutionContractVersion != 5 {
		t.Fatalf("native Anthropic creation lost the new frozen contract: %s", response.Body.String())
	}
	for _, field := range []string{"model_protocol", "max_output_tokens", "execution_provider_confirmation", "input_files", "rework_context"} {
		if strings.Contains(response.Body.String(), `"`+field+`"`) {
			t.Fatalf("create receipt exposed detail-only %s: %s", field, response.Body.String())
		}
	}
	return result.Data
}

func (f *anthropicAgentRunFixture) wait(t *testing.T, id string) models.AgentRun {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var run models.AgentRun
		if err := f.Store.DB.First(&run, "id=?", id).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != "queued" && run.Status != "running" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("real Anthropic executor did not reach a terminal state")
	return models.AgentRun{}
}

// The model is only a local HTTP fixture. Native approval, the reserved
// ExecutorMain subprocess, pipe Runner, and Task output transaction are real.
func TestAgentRunAnthropicNativeTextUsesRealExecutor(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(_ int, request map[string]any, w http.ResponseWriter) {
		if request["system"] == nil || request["messages"] == nil {
			t.Error("Anthropic request did not separate the system instruction and task messages")
		}
		writeAnthropicAgentRunResponse(w, "The completed report is ready for human review.", "end_turn")
	})
	created := f.create(t, "")
	run := f.wait(t, created.ID)
	if f.Calls.Load() != 1 || run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil {
		t.Fatalf("actual Anthropic execution was not submitted once: calls=%d run=%#v", f.Calls.Load(), run)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
	if !strings.Contains(run.InputSnapshotJSON, `"model_protocol":"anthropic_messages"`) || !strings.Contains(run.InputSnapshotJSON, `"max_output_tokens":8192`) ||
		strings.Contains(run.InputSnapshotJSON, anthropicRunTestKey) || strings.Contains(run.InputSnapshotJSON, "127.0.0.1") {
		t.Fatalf("frozen model identity or privacy is invalid: %s", run.InputSnapshotJSON)
	}
}

func TestAgentRunAnthropicFileContractsRegisterExactBytes(t *testing.T) {
	for _, kind := range []string{"file", "files"} {
		t.Run(kind, func(t *testing.T) {
			bodies := []string{"# 完整交付\n需要人工验收。"}
			output := `{"type":"file","name":"report.md","mime":"text/markdown"}`
			modelText := bodies[0]
			if kind == "files" {
				bodies = append(bodies, `{"evidence":"complete"}`)
				encoded, _ := json.Marshal(bodies)
				modelText = string(encoded)
				output = `{"type":"files","files":[{"name":"report.md","mime":"text/markdown"},{"name":"facts.json","mime":"application/json"}]}`
			}
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				writeAnthropicAgentRunResponse(w, modelText, "end_turn")
			})
			run := f.wait(t, f.create(t, `,"output_contract":`+output).ID)
			if f.Calls.Load() != 1 || run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil {
				t.Fatalf("%s output did not reach the real review chain: %#v", kind, run)
			}
			var artifacts []models.TaskArtifact
			if err := f.Store.DB.Where("submission_id=?", *run.SubmissionID).Order("position").Find(&artifacts).Error; err != nil {
				t.Fatal(err)
			}
			if len(artifacts) != len(bodies) || artifacts[0].ID != *run.ArtifactID {
				t.Fatalf("incomplete multi-artifact registration: %#v", artifacts)
			}
			for index, want := range bodies {
				artifact := artifacts[index]
				if artifact.StorageKind != "file" || artifact.RelativePath == nil || artifact.SizeBytes == nil || *artifact.SizeBytes != int64(len(want)) ||
					artifact.SHA256 == nil || *artifact.SHA256 != sha256Hex([]byte(want)) || artifact.ProducedByActorID != f.Actor.ID || artifact.RecordedByActorID != models.BuiltinSystemActorID {
					t.Fatalf("file identity/producer/size/hash mismatch: %#v", artifact)
				}
				file, _, err := f.Artifacts.openObject(*artifact.RelativePath)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil || string(stored) != want {
					t.Fatalf("actual stored Artifact body=%q err=%v", stored, err)
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
		})
	}
}

func TestAgentRunAnthropicUnsafeResponsesCannotProduceBusinessOutput(t *testing.T) {
	textResponse := func(stop string) string {
		return fmt.Sprintf(`{"type":"message","role":"assistant","content":[{"type":"text","text":"PRIVATE_MODEL partial report"}],"stop_reason":%s}`, stop)
	}
	for _, test := range []struct {
		name, response, code string
		status               int
	}{
		{"token_limit", textResponse(`"max_tokens"`), "AGENT_MODEL_TRUNCATED", 200},
		{"context_limit", textResponse(`"model_context_window_exceeded"`), "AGENT_MODEL_TRUNCATED", 200},
		{"refusal", textResponse(`"refusal"`), "AGENT_MODEL_FILTERED", 200},
		{"tool_stop", textResponse(`"tool_use"`), "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"pause", textResponse(`"pause_turn"`), "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"stop_sequence", textResponse(`"stop_sequence"`), "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"null_stop", textResponse(`null`), "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"unknown_stop", textResponse(`"new_unknown_reason"`), "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"tool_block", `{"type":"message","role":"assistant","content":[{"type":"text","text":"PRIVATE_MODEL body"},{"type":"tool_use","id":"tool_1","name":"danger","input":{}}],"stop_reason":"end_turn"}`, "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"thinking_block", `{"type":"message","role":"assistant","content":[{"type":"thinking","thinking":"PRIVATE_MODEL hidden"},{"type":"text","text":"body"}],"stop_reason":"end_turn"}`, "AGENT_MODEL_RESPONSE_INVALID", 200},
		{"empty", `{"type":"message","role":"assistant","content":[{"type":"text","text":""}],"stop_reason":"end_turn"}`, "AGENT_RESULT_EMPTY", 200},
		{"raw_http_error", `{"type":"error","error":{"message":"PRIVATE_MODEL credential endpoint diagnostic"}}`, "AGENT_MODEL_FAILED", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.response))
			})
			enableAgentFailureAutomationForTest(t, f.aiAgentRunFixture)
			run := f.wait(t, f.create(t, "").ID)
			if f.Calls.Load() != 1 {
				t.Fatalf("unsafe response retried model automatically: %d", f.Calls.Load())
			}
			assertAgentFinishReasonFailure(t, f.aiAgentRunFixture, run, test.code)
		})
	}
}

func (f *anthropicAgentRunFixture) prepareQueued(t *testing.T, extra prepareAgentRunInput) models.AgentRun {
	t.Helper()
	extra.TaskID, extra.ProviderID, extra.ArtifactStore = f.Task.ID, f.Provider.ID, f.Artifacts
	var run models.AgentRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareAgentRun(tx, extra)
		if err != nil {
			return err
		}
		run, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "anthropic-independent-test", f.Service.options.Now().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run.ExecutionContractVersion != 5 {
		t.Fatalf("new Anthropic execution did not use v5: %#v", run)
	}
	return run
}

func TestAgentRunAnthropicProtocolDriftBeforeLaunchNeverSendsHTTP(t *testing.T) {
	for _, afterClaim := range []bool{false, true} {
		t.Run(fmt.Sprintf("after_claim_%v", afterClaim), func(t *testing.T) {
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("identity drift must prevent executor HTTP")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			run := f.prepareQueued(t, prepareAgentRunInput{})
			drift := func(db *gorm.DB) error {
				return db.Model(&models.AIProvider{}).Where("id=?", f.Provider.ID).Updates(map[string]any{
					"protocol": "openai_chat", "version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1,
				}).Error
			}
			var mutations atomic.Int32
			if afterClaim {
				const callback = "test:anthropic_protocol_changed_after_claim"
				if err := f.Store.DB.Callback().Update().After("gorm:update").Register(callback, func(db *gorm.DB) {
					if db.Statement.Table == "agent_runs" && mutations.CompareAndSwap(0, 1) {
						db.AddError(drift(db.Session(&gorm.Session{NewDB: true})))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = f.Store.DB.Callback().Update().Remove(callback) })
			} else if err := drift(f.Store.DB); err != nil {
				t.Fatal(err)
			}
			f.Service.executeAgentRun(context.Background(), run.ID)
			failed := f.wait(t, run.ID)
			if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != agentRunIdentityChanged || f.Calls.Load() != 0 ||
				failed.InputSnapshotJSON != run.InputSnapshotJSON || afterClaim && mutations.Load() != 1 {
				t.Fatalf("protocol drift was not rejected before HTTP: %#v calls=%d", failed, f.Calls.Load())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, f.Task.ID)
		})
	}
}

func TestAgentRunAnthropicPendingDeliveryRecoversWithoutModelRetry(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		writeAnthropicAgentRunResponse(w, `["# Durable completed report","{\"ready\":true}"]`, "end_turn")
	})
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_anthropic_output BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted' BEGIN SELECT RAISE(ABORT,'isolated delivery failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	created := f.create(t, `,"output_contract":{"type":"files","files":[{"name":"report.md","mime":"text/markdown"},{"name":"facts.json","mime":"application/json"}]}`)
	f.Service.agentRunWorkers.Wait()
	var pending models.AgentRun
	if err := f.Store.DB.First(&pending, "id=?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.Status != "running" || pending.OutputDeliveryStatus != agentRunOutputPending || pending.OutputDeliveryPendingText == nil || f.Calls.Load() != 1 {
		t.Fatalf("real completed model output was not staged for recovery: %#v calls=%d", pending, f.Calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 0, f.Task.ID)
	if response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+pending.ID+"/retry", nil, nil); response.Code != http.StatusConflict {
		t.Fatalf("pending output may not execute again: %d %s", response.Code, response.Body.String())
	}
	if err := f.Store.DB.Exec("DROP TRIGGER fail_anthropic_output").Error; err != nil {
		t.Fatal(err)
	}
	var first models.AgentRun
	for index := range 2 {
		recovered, err := f.Service.recoverAgentRunOutput(pending.ID, f.Service.options.Now().Add(time.Minute))
		if err != nil || recovered.Status != "succeeded" || recovered.OutputDeliveryStatus != agentRunOutputSubmitted ||
			recovered.SubmissionID == nil || recovered.ArtifactID == nil || recovered.InputSnapshotJSON != pending.InputSnapshotJSON {
			t.Fatalf("v5 recovery did not preserve frozen output: %#v err=%v", recovered, err)
		}
		if index == 0 {
			first = recovered
		} else if !reflect.DeepEqual(first, recovered) {
			t.Fatal("repeat output recovery changed the durable Run")
		}
	}
	if f.Calls.Load() != 1 {
		t.Fatalf("pending recovery reran the provider: %d", f.Calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE task_id=?", 2, f.Task.ID)
}

func TestAgentRunAnthropicExactRetryUsesFrozenProtocolAndDetailOnlyDisclosure(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(call int, _ map[string]any, w http.ResponseWriter) {
		if call == 1 {
			writeAnthropicAgentRunResponse(w, "Partial output must not be reused.", "max_tokens")
		} else {
			writeAnthropicAgentRunResponse(w, "The original frozen task has now been completed.", "end_turn")
		}
	})
	original := f.wait(t, f.create(t, "").ID)
	if original.Status != "failed" || original.ErrorCode == nil || *original.ErrorCode != "AGENT_MODEL_TRUNCATED" {
		t.Fatalf("original was not a real truncated failure: %#v", original)
	}
	detail := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs/"+original.ID, nil, nil)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &envelope) != nil ||
		envelope.Data["model_protocol"] != "anthropic_messages" || envelope.Data["max_output_tokens"] != float64(8192) {
		t.Fatalf("exact detail lacks the frozen execution protocol: %d %s", detail.Code, detail.Body.String())
	}
	confirmation, _ := envelope.Data["execution_provider_confirmation"].(map[string]any)
	if confirmation["kind"] != "remote" || confirmation["version"] != float64(f.Provider.Version) || confirmation["config_version"] != float64(f.Provider.ConfigVersion) {
		t.Fatalf("detail did not expose exact frozen Provider confirmation: %#v", confirmation)
	}
	for _, path := range []string{"/api/v1/agent-runs", "/api/v1/tasks/" + f.Task.ID + "/agent-runs"} {
		response := performRequest(f.Router, http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("list=%d: %s", response.Code, response.Body.String())
		}
		for _, field := range []string{"model_protocol", "max_output_tokens", "execution_provider_confirmation", "rework_context", "input_files"} {
			if strings.Contains(response.Body.String(), `"`+field+`"`) {
				t.Fatalf("list leaked explicit-detail-only %s: %s", field, response.Body.String())
			}
		}
	}
	response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+original.ID+"/retry", nil, nil)
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("plain v5 retry should preserve the empty-body native contract: %d %s", response.Code, response.Body.String())
	}
	id, _ := envelope.Data["id"].(string)
	child := f.wait(t, id)
	if child.ParentRunID == nil || *child.ParentRunID != original.ID || child.Attempt <= original.Attempt || child.ExecutionContractVersion != 5 ||
		child.InputSnapshotJSON != original.InputSnapshotJSON || child.ProviderID != original.ProviderID || child.ProviderVersion != original.ProviderVersion ||
		child.OutputDeliveryStatus != agentRunOutputSubmitted || f.Calls.Load() != 2 {
		t.Fatalf("native v5 retry changed the frozen execution or did not submit: %#v calls=%d", child, f.Calls.Load())
	}
	var unchanged models.AgentRun
	if err := f.Store.DB.First(&unchanged, "id=?", original.ID).Error; err != nil || !reflect.DeepEqual(original, unchanged) {
		t.Fatalf("retry overwrote its original failure: %v", err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='agent_run_restarted'", 0)
}

func TestAgentRunAnthropicSuccessfulRetainedCannotRetryEvenWithoutTaskDrift(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		writeAnthropicAgentRunResponse(w, "Complete output retained until current review facts allow new work.", "end_turn")
	})
	if err := f.Store.DB.Model(&models.TaskAssignment{}).Where("task_id=? AND role='reviewer' AND unassigned_at IS NULL", f.Task.ID).
		Update("unassigned_at", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)).Error; err != nil {
		t.Fatal(err)
	}
	run := f.wait(t, f.create(t, "").ID)
	if run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputRetained || run.ResultText == nil || f.Calls.Load() != 1 {
		t.Fatalf("actual output did not take the retained disposition: %#v", run)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=?", 1, f.Task.ID, run.TaskVersion)
	response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/retry", nil, nil)
	assertAPIError(t, response, http.StatusConflict, "AGENT_RUN_NOT_RETRYABLE")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	if f.Calls.Load() != 1 {
		t.Fatalf("successful retained work was rerun: %d", f.Calls.Load())
	}
}

func TestAgentRunAnthropicReworkAndFilesRemainIndependentAndProtectSources(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		if call == 1 {
			writeAnthropicAgentRunResponse(w, reworkTestOriginal, "end_turn")
			return
		}
		encoded, _ := json.Marshal(request)
		for _, required := range []string{reworkTestReason, reworkTestOriginal, "EXPLICIT_CONTROLLED_REFERENCE"} {
			if !strings.Contains(string(encoded), required) {
				t.Errorf("authorized rework/file evidence missing from actual Messages request: %s", required)
			}
		}
		writeAnthropicAgentRunResponse(w, "Revised report with the requested third evidence item.", "end_turn")
	})
	file := f.addTaskArtifact(t, f.Task.ID, "reference.txt", []byte("EXPLICIT_CONTROLLED_REFERENCE"))
	original := f.wait(t, f.create(t, "").ID)
	if original.SubmissionID == nil || original.ArtifactID == nil {
		t.Fatalf("original actual execution did not submit: %#v", original)
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	review, _ := json.Marshal(map[string]any{"decision": "request_changes", "reason": reworkTestReason})
	response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/review", review,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if response.Code != http.StatusOK {
		t.Fatalf("real owner request_changes failed: %d %s", response.Code, response.Body.String())
	}
	f.Task = decodeReviewOutputResponse(t, response.Body.Bytes()).Task
	if f.Task.Status != "in_progress" {
		t.Fatal("native review did not return work for revision")
	}
	selection := prepareAgentRunInput{
		InputFiles: []agentRunInputFileRequest{{SourceKind: agentexec.FileSourceTaskArtifact, ID: file.ID}},
		Rework:     &agentRunReworkRequest{SubmissionID: *original.SubmissionID, ArtifactIDs: []string{*original.ArtifactID}, ExpectedTaskVersion: f.Task.Version},
	}
	queued := f.prepareQueued(t, selection)
	for _, artifactID := range []string{file.ID, *original.ArtifactID} {
		linked, err := activeAgentRunReferencesControlledFile(f.Store.DB, agentexec.FileSourceTaskArtifact, artifactID)
		if err != nil || !linked {
			t.Fatalf("v5 did not protect exact file or rework source %s: linked=%v err=%v", artifactID, linked, err)
		}
	}
	if response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+queued.ID+"/cancel", nil, nil); response.Code != http.StatusOK {
		t.Fatalf("cancel queued v5=%d: %s", response.Code, response.Body.String())
	}
	for _, artifactID := range []string{file.ID, *original.ArtifactID} {
		linked, err := activeAgentRunReferencesControlledFile(f.Store.DB, agentexec.FileSourceTaskArtifact, artifactID)
		if err != nil || linked {
			t.Fatalf("cancelled v5 retained an active deletion lock: %v %v", linked, err)
		}
	}
	if f.Calls.Load() != 1 {
		t.Fatal("inspection or cancelling a queued Run invoked the model")
	}
	confirmation := map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": "remote"}
	retryBody := map[string]any{"confirm_rework_context": true, "rework_provider_confirmation": confirmation,
		"confirm_file_access": true, "file_access_provider_confirmation": confirmation}
	for _, missing := range []string{"confirm_rework_context", "confirm_file_access"} {
		copyBody := map[string]any{}
		for key, value := range retryBody {
			copyBody[key] = value
		}
		delete(copyBody, missing)
		body, _ := json.Marshal(copyBody)
		response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+queued.ID+"/retry", body, nil)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("retry without independent %s=%d: %s", missing, response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	}
	body, _ := json.Marshal(retryBody)
	response = performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+queued.ID+"/retry", body, nil)
	var envelope struct {
		Data models.AgentRun `json:"data"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("confirmed v5 rework retry=%d: %s", response.Code, response.Body.String())
	}
	child := f.wait(t, envelope.Data.ID)
	if child.InputSnapshotJSON != queued.InputSnapshotJSON || child.ParentRunID == nil || *child.ParentRunID != queued.ID || child.OutputDeliveryStatus != agentRunOutputSubmitted ||
		child.SubmissionID == nil || *child.SubmissionID == *original.SubmissionID || f.Calls.Load() != 2 {
		t.Fatalf("approved rework did not produce a new real review batch: %#v", child)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested' AND review_reason=?", 1, *original.SubmissionID, reworkTestReason)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
}

func TestAgentRunAnthropicSnapshotCanonicalBoundaryAndMalformedDetail(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("invalid snapshot must not reach model HTTP")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	prepared, err := prepareAgentRun(f.Store.DB, prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Artifacts})
	if err != nil {
		t.Fatal(err)
	}
	valid := prepared.InputSnapshotJSON
	if _, err := parseAgentRunV5Snapshot(valid); err != nil {
		t.Fatalf("actual canonical preparation was rejected: %v", err)
	}
	for _, test := range []struct{ name, raw string }{
		{"duplicate_protocol", strings.TrimSuffix(valid, "}") + `,"model_protocol":"anthropic_messages"}`},
		{"duplicate_token_budget", strings.TrimSuffix(valid, "}") + `,"max_output_tokens":8192}`},
		{"protocol_alias", strings.Replace(valid, `"model_protocol"`, `"ModelProtocol"`, 1)},
		{"token_alias", strings.Replace(valid, `"max_output_tokens"`, `"MAX_OUTPUT_TOKENS"`, 1)},
		{"unknown", strings.TrimSuffix(valid, "}") + `,"future":true}`},
		{"missing_protocol", strings.Replace(valid, `"model_protocol":"anthropic_messages",`, "", 1)},
		{"missing_tokens", strings.Replace(valid, `"max_output_tokens":8192,`, "", 1)},
		{"null_protocol", strings.Replace(valid, `"model_protocol":"anthropic_messages"`, `"model_protocol":null`, 1)},
		{"null_tokens", strings.Replace(valid, `"max_output_tokens":8192`, `"max_output_tokens":null`, 1)},
		{"wrong_protocol", strings.Replace(valid, `"anthropic_messages"`, `"openai_chat"`, 1)},
		{"wrong_token_budget", strings.Replace(valid, `"max_output_tokens":8192`, `"max_output_tokens":4096`, 1)},
		{"noncanonical_numeric", strings.Replace(valid, `"max_output_tokens":8192`, `"max_output_tokens":8192.0`, 1)},
		{"local_kind", strings.Replace(valid, `"provider_kind":"remote"`, `"provider_kind":"local"`, 1)},
		{"null_files", strings.Replace(valid, `"files":[]`, `"files":null`, 1)},
		{"null_rework", strings.TrimSuffix(valid, "}") + `,"rework":null}`},
		{"whitespace", " " + valid},
		{"second_value", valid + `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.raw == valid {
				t.Fatal("negative fixture did not change the canonical snapshot")
			}
			_, err := parseAgentRunV5Snapshot(test.raw)
			assertAgentRunDomainCode(t, err, agentRunIdentityChanged)
		})
	}
	// Simulate an invalid historical durable input without disabling any guard
	// or rewriting an immutable Run. Neither detail nor startup may downgrade it.
	prepared.InputSnapshotJSON = strings.Replace(valid, `"anthropic_messages"`, `"openai_chat"`, 1)
	var corrupt models.AgentRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		corrupt, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "invalid-historical-v5", f.Service.options.Now().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs/"+corrupt.ID, nil, nil)
	assertAPIError(t, response, http.StatusConflict, "AGENT_RUN_IDENTITY_CHANGED")
	f.Service.executeAgentRun(context.Background(), corrupt.ID)
	failed := f.wait(t, corrupt.ID)
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != agentRunIdentityChanged || f.Calls.Load() != 0 {
		t.Fatalf("malformed v5 was executed or silently downgraded: status=%s code=%v calls=%d", failed.Status, failed.ErrorCode, f.Calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
}

func TestAgentRunAnthropicNeverReinterpretsLegacySnapshotsEvenWithMatchingVersions(t *testing.T) {
	for _, version := range []int64{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("legacy_v%d", version), func(t *testing.T) {
			f := newAnthropicAgentRunFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("a legacy OpenAI snapshot must never be executed as Anthropic")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			input := prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, ArtifactStore: f.Artifacts}
			if version == 2 {
				input.OutputContract = &agentRunOutputContractRequest{Type: agentexec.ResultTypeFile, Name: "report.md", MIME: "text/markdown"}
			} else if version == 3 {
				input.OutputContract = &agentRunOutputContractRequest{Type: agentexec.ResultTypeFiles, Files: []agentexec.OutputFileContract{
					{Name: "report.md", MIME: "text/markdown"}, {Name: "facts.json", MIME: "application/json"},
				}}
			} else if version == 4 {
				// Establish genuine reviewed evidence through native domain commands;
				// only this fixture's first executor result is controlled directly.
				original := f.prepareQueued(t, prepareAgentRunInput{})
				if err := f.Store.DB.Model(&original).Updates(map[string]any{"status": "running", "started_at": original.CreatedAt}).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Service.finalizeAgentRun(original, reworkTestOriginal, "", f.Service.options.Now().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.First(&original, "id=?", original.ID).Error; err != nil || original.SubmissionID == nil || original.ArtifactID == nil {
					t.Fatalf("controlled original did not create domain evidence: %v", err)
				}
				if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
					t.Fatal(err)
				}
				body, _ := json.Marshal(map[string]any{"decision": "request_changes", "reason": reworkTestReason})
				response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/review", body,
					map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
				if response.Code != http.StatusOK {
					t.Fatalf("owner review=%d: %s", response.Code, response.Body.String())
				}
				f.Task = decodeReviewOutputResponse(t, response.Body.Bytes()).Task
				input.Rework = &agentRunReworkRequest{SubmissionID: *original.SubmissionID, ArtifactIDs: []string{*original.ArtifactID}, ExpectedTaskVersion: f.Task.Version}
			}
			prepared, err := prepareAgentRun(f.Store.DB, input)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := parseAgentRunV5Snapshot(prepared.InputSnapshotJSON)
			if err != nil {
				t.Fatal(err)
			}
			// Store a structurally valid old encoding with otherwise matching
			// Provider numeric versions. This is a hostile historical fixture,
			// not a supported creation path; no trigger is dropped or weakened.
			var encoded []byte
			if version == 1 {
				encoded, err = json.Marshal(agentRunV1TaskSnapshotFromModel(f.Task))
			} else if version == 4 {
				encoded, err = json.Marshal(agentRunV4InputSnapshot{Task: snapshot.Task, ProviderKind: snapshot.ProviderKind, Files: snapshot.Files, OutputContract: snapshot.OutputContract, Rework: *snapshot.Rework})
			} else {
				encoded, err = json.Marshal(snapshot.fileSnapshot())
			}
			if err != nil {
				t.Fatal(err)
			}
			prepared.ExecutionContractVersion, prepared.InputSnapshotJSON = version, string(encoded)
			var run models.AgentRun
			if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
				var err error
				run, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "legacy-protocol-corruption", f.Service.options.Now().Format(time.RFC3339Nano))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if run.ProviderVersion != f.Provider.Version || run.ProviderConfigVersion != f.Provider.ConfigVersion || run.TaskVersion != f.Task.Version {
				t.Fatal("legacy fixture did not isolate protocol from numeric identity")
			}
			f.Service.executeAgentRun(context.Background(), run.ID)
			failed := f.wait(t, run.ID)
			if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != agentRunIdentityChanged || f.Calls.Load() != 0 || failed.InputSnapshotJSON != run.InputSnapshotJSON {
				t.Fatalf("legacy v%d was reinterpreted as Messages: status=%s calls=%d", version, failed.Status, f.Calls.Load())
			}
			if _, _, err := prepareAgentRunRetry(f.Store.DB, failed.ID, f.Artifacts); err == nil {
				t.Fatal("legacy exact retry silently changed to v5")
			}
		})
	}
}

func TestAgentRunAnthropicFileOnlyRetryRequiresIndependentFileConsent(t *testing.T) {
	f := newAnthropicAgentRunFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		encoded, _ := json.Marshal(request)
		if !strings.Contains(string(encoded), "EXPLICIT_FILE_ONLY_REFERENCE") {
			t.Error("actual file-only request omitted its authorized controlled input")
		}
		if call == 1 {
			writeAnthropicAgentRunResponse(w, "Incomplete output.", "max_tokens")
		} else {
			writeAnthropicAgentRunResponse(w, "The complete report using the same frozen reference.", "end_turn")
		}
	})
	file := f.addTaskArtifact(t, f.Task.ID, "reference.txt", []byte("EXPLICIT_FILE_ONLY_REFERENCE"))
	providerConfirmation := map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": "remote"}
	confirmationJSON, _ := json.Marshal(providerConfirmation)
	extra := fmt.Sprintf(`,"input_files":[{"source_kind":"task_artifact","id":%q}],"confirm_file_access":true,"file_access_provider_confirmation":%s`, file.ID, confirmationJSON)
	original := f.wait(t, f.create(t, extra).ID)
	if original.Status != "failed" || original.ErrorCode == nil || *original.ErrorCode != "AGENT_MODEL_TRUNCATED" {
		t.Fatalf("file retry source was not a real failed execution: %#v", original)
	}
	withoutScope := anthropicActionTool(t, f, "work", "outputs", "actions", "agent_execution")
	if _, err := withoutScope.Execute(context.Background(), []byte(aiRetryArgs(f.aiAgentRunFixture, original))); err == nil {
		t.Fatal("v5 retry without explicit agent_files scope exposed frozen controlled input")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	for _, body := range [][]byte{
		nil,
		[]byte(`{"confirm_file_access":true}`),
		[]byte(fmt.Sprintf(`{"confirm_file_access":true,"file_access_provider_confirmation":{"version":%d,"config_version":%d,"kind":"remote"}}`, f.Provider.Version-1, f.Provider.ConfigVersion)),
	} {
		response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+original.ID+"/retry", body, nil)
		if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusConflict {
			t.Fatalf("file-only retry bypassed current explicit file confirmation: %d %s", response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
		if f.Calls.Load() != 1 {
			t.Fatal("failed file confirmation invoked the model")
		}
	}
	body := []byte(fmt.Sprintf(`{"confirm_file_access":true,"file_access_provider_confirmation":%s}`, confirmationJSON))
	response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+original.ID+"/retry", body, nil)
	var envelope struct {
		Data models.AgentRun `json:"data"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("separately confirmed file-only retry=%d: %s", response.Code, response.Body.String())
	}
	child := f.wait(t, envelope.Data.ID)
	if child.OutputDeliveryStatus != agentRunOutputSubmitted || child.InputSnapshotJSON != original.InputSnapshotJSON || child.ParentRunID == nil || *child.ParentRunID != original.ID || f.Calls.Load() != 2 {
		t.Fatalf("file-only retry failed to preserve and deliver its original contract: %#v", child)
	}
}
