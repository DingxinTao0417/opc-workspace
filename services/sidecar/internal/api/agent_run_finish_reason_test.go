package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// These tests exercise the actual reserved agent-executor subprocess, anonymous
// pipe runner, and native finalizer. Only the HTTP model is a deterministic local
// fixture; no model credentials, real Provider, or desktop service is involved.
func runAgentFinishReasonResponse(t *testing.T, response string, status int, outputContract ...string) (aiAgentRunFixture, models.AgentRun) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	// Fixture cleanup closes its Router/workers before the local HTTP server.
	f := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, f)
	enableAgentFailureAutomationForTest(t, f)
	if err := f.Store.DB.Model(&models.AIProvider{}).Where("id = ?", f.Provider.ID).Updates(map[string]any{
		"base_url": server.URL + "/v1", "version": f.Provider.Version + 1,
		"config_version": f.Provider.ConfigVersion + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"provider_id":%q}`, f.Provider.ID)
	if len(outputContract) != 0 {
		body = strings.TrimSuffix(body, "}") + `,"output_contract":` + outputContract[0] + `}`
	}
	created := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", []byte(body),
		map[string]string{"Idempotency-Key": "finish-reason-contract"})
	if created.Code != http.StatusCreated {
		t.Fatalf("native create = %d: %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data agentRunResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var run models.AgentRun
		if err := f.Store.DB.First(&run, "id = ?", envelope.Data.ID).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status != "queued" && run.Status != "running" {
			if calls.Load() != 1 {
				t.Fatalf("HTTP model calls = %d, want exactly one", calls.Load())
			}
			return f, run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("real executor did not reach a terminal state")
	return f, models.AgentRun{}
}

func assertAgentFinishReasonFailure(t *testing.T, f aiAgentRunFixture, run models.AgentRun, code string) {
	t.Helper()
	if run.Status != "failed" || run.ErrorCode == nil || *run.ErrorCode != code {
		t.Fatalf("terminal status=%s error=%v, want failed/%s", run.Status, run.ErrorCode, code)
	}
	if run.OutputDeliveryStatus != agentRunOutputNotReady || run.ResultText != nil || run.ResultBytes != nil ||
		run.SubmissionID != nil || run.ArtifactID != nil || run.OutputDeliveryPendingText != nil ||
		run.OutputDeliveryPendingBytes != nil || run.OutputDeliveryPendingCompletedAt != nil {
		t.Fatalf("rejected model response produced or staged output: %#v", run)
	}
	var task models.Task
	if err := f.Store.DB.First(&task, "id = ?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != f.Task.Status || task.Version != f.Task.Version {
		t.Fatalf("rejected model response changed task: status=%s version=%d", task.Status, task.Version)
	}
	for _, table := range []string{"task_submissions", "task_artifacts"} {
		var count int64
		if err := f.Store.DB.Table(table).Where("task_id = ?", f.Task.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v, want zero writes", table, count, err)
		}
	}
	detail := performRequest(f.Router, http.MethodGet, "/api/v1/agent-runs/"+run.ID, nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), code) ||
		strings.Contains(detail.Body.String(), "PRIVATE_MODEL") || strings.Contains(detail.Body.String(), "127.0.0.1") {
		t.Fatalf("unsafe or missing error detail: %d %s", detail.Code, detail.Body.String())
	}
	var events []models.WorkflowEvent
	if err := f.Store.DB.Where("aggregate_type = ? AND aggregate_id = ?", "agent_run", run.ID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, event := range events {
		if event.Action == "agent_run_failed" {
			failed++
			if event.CurrentJSON == nil || !strings.Contains(*event.CurrentJSON, code) || strings.Contains(*event.CurrentJSON, "PRIVATE_MODEL") {
				t.Fatalf("unsafe failure event: %v", event.CurrentJSON)
			}
		}
	}
	if failed != 1 {
		t.Fatalf("failed workflow events=%d, want exactly one", failed)
	}
	assertAgentFinishReasonNotice(t, f, run, code)
}

func assertAgentFinishReasonNotice(t *testing.T, f aiAgentRunFixture, run models.AgentRun, code string) {
	t.Helper()
	// Consume the real failed-event outbox, not a fabricated notification row.
	for range 2 {
		if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), f.Service.options.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var item models.InboxItem
	if err := f.Store.DB.Where("source_entity_type=? AND source_entity_id=?", agentRunFailedInboxSourceType, run.ID).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	proof, err := automationAgentRunFailurePayloadFromJSON(item.PayloadJSON)
	if err != nil || proof.ErrorCode != code || proof.AgentRunID != run.ID || proof.TaskID != run.TaskID ||
		proof.Attempt != run.Attempt || item.SourceEventKey == nil || *item.SourceEventKey != agentRunFailedEventKey(run.ID) {
		t.Fatalf("failure notification lost original identity/code: %#v err=%v", proof, err)
	}
	var notice models.AutomationRun
	if err := f.Store.DB.First(&notice, "id=?", proof.AutomationRunID).Error; err != nil {
		t.Fatal(err)
	}
	action, err := automationAgentRunFailureActionFromJSON(notice.ActionSnapshotJSON)
	if err != nil || action.ErrorCode != code || notice.Status != "succeeded" || notice.ResultID == nil || *notice.ResultID != item.ID {
		t.Fatalf("notification result invalid: %#v action=%#v err=%v", notice, action, err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed' AND source_entity_id=?", 1, run.ID)
	tool := aiInboxSourceToolForTest(t, f.Service, f.Store, "work", "outputs")
	source, raw := readAIInboxSourceForTest(t, tool, item.ID)
	if source.LookupStatus != "available" || source.Subject == nil || source.Subject.Type != "agent_run" ||
		source.Subject.ID != run.ID || source.Subject.Status == nil || *source.Subject.Status != "failed" ||
		!strings.Contains(source.Subject.Route, run.ID) || strings.Contains(raw, "PRIVATE_MODEL") {
		t.Fatalf("actual notification no longer verifies its failed Run: %s", raw)
	}
	// The scope gate still precedes source lookup; a failure notification cannot
	// smuggle protected Run metadata into a work-only message.
	workOnly := aiInboxSourceToolForTest(t, f.Service, f.Store, "work")
	denied, deniedRaw := readAIInboxSourceForTest(t, workOnly, item.ID)
	if denied.LookupStatus != "permission_required" || denied.Subject != nil || len(denied.Related) != 0 || strings.Contains(deniedRaw, run.ID) {
		t.Fatalf("work-only message leaked Run source: %s", deniedRaw)
	}
	for _, unsafe := range []string{"UNKNOWN_MODEL_ERROR", code + "\nPRIVATE_MODEL diagnostics"} {
		poisoned, _ := json.Marshal(unsafe)
		original, _ := json.Marshal(code)
		if _, err := automationAgentRunFailurePayloadFromJSON(strings.Replace(item.PayloadJSON, string(original), string(poisoned), 1)); err == nil {
			t.Fatalf("notification parser accepted untrusted error %q", unsafe)
		}
		if safeAutomationAgentRunErrorCode(unsafe) != "AGENT_RUN_FAILED" {
			t.Fatalf("unknown code escaped the safe failure allowlist")
		}
	}
}

func TestAgentRunFinishReasonRejectsPartialOrFilteredOutput(t *testing.T) {
	for _, test := range []struct{ name, reason, code string }{
		{"length", "length", "AGENT_MODEL_TRUNCATED"},
		{"content_filter", "content_filter", "AGENT_MODEL_FILTERED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := fmt.Sprintf(`{"choices":[{"finish_reason":%q,"message":{"content":"PRIVATE_MODEL partial answer must never be submitted"}}]}`, test.reason)
			f, run := runAgentFinishReasonResponse(t, response, http.StatusOK)
			assertAgentFinishReasonFailure(t, f, run, test.code)
		})
	}
}

func TestAgentRunFinishReasonRequiresUnambiguousTerminalText(t *testing.T) {
	for _, test := range []struct{ name, choice, code string }{
		{"tool_calls_reason", `"finish_reason":"tool_calls","message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"function_call_reason", `"finish_reason":"function_call","message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"missing_reason", `"message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"null_reason", `"finish_reason":null,"message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"empty_reason", `"finish_reason":"","message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"unknown_reason", `"finish_reason":"PRIVATE_MODEL_UNKNOWN","message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"wrong_reason_type", `"finish_reason":3,"message":{"content":"PRIVATE_MODEL partial"}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"stop_with_tool_call", `"finish_reason":"stop","message":{"content":"PRIVATE_MODEL partial","tool_calls":[{"id":"tool_1","type":"function","function":{"name":"shell","arguments":"PRIVATE_MODEL secret"}}]}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"stop_with_function", `"finish_reason":"stop","message":{"content":"PRIVATE_MODEL partial","function_call":{"name":"shell","arguments":"PRIVATE_MODEL secret"}}`, "AGENT_MODEL_RESPONSE_INVALID"},
		{"stop_with_refusal", `"finish_reason":"stop","message":{"content":"PRIVATE_MODEL partial","refusal":"PRIVATE_MODEL filtered"}`, "AGENT_MODEL_FILTERED"},
		{"empty_complete_text", `"finish_reason":"stop","message":{"content":"  \n  "}`, "AGENT_RESULT_EMPTY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, run := runAgentFinishReasonResponse(t, `{"choices":[{`+test.choice+`}]}`, http.StatusOK)
			assertAgentFinishReasonFailure(t, f, run, test.code)
		})
	}
}

func TestAgentRunFinishReasonRejectsCompleteLookingFilePayload(t *testing.T) {
	for _, test := range []struct{ name, content, contract string }{
		{"file", "PRIVATE_MODEL incomplete but valid markdown", `{"type":"file","name":"answer.md","mime":"text/markdown"}`},
		{"files", `["PRIVATE_MODEL one","PRIVATE_MODEL two"]`, `{"type":"files","files":[{"name":"one.md","mime":"text/markdown"},{"name":"two.txt","mime":"text/plain"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"finish_reason": "length", "message": map[string]any{"content": test.content},
			}}})
			f, run := runAgentFinishReasonResponse(t, string(body), http.StatusOK, test.contract)
			assertAgentFinishReasonFailure(t, f, run, "AGENT_MODEL_TRUNCATED")
			// The exact same bytes are valid output when and only when the
			// Provider explicitly reports normal completion. This prevents an
			// invalid file fixture from making the truncation regression pass.
			completeBody := strings.Replace(string(body), `"finish_reason":"length"`, `"finish_reason":"stop"`, 1)
			completeFixture, completeRun := runAgentFinishReasonResponse(t, completeBody, http.StatusOK, test.contract)
			if completeRun.Status != "succeeded" || completeRun.OutputDeliveryStatus != agentRunOutputSubmitted || completeRun.SubmissionID == nil {
				t.Fatalf("same file payload with stop must be deliverable: %#v", completeRun)
			}
			wantArtifacts := int64(1)
			if test.name == "files" {
				wantArtifacts = 2
			}
			assertDatabaseCount(t, completeFixture.Store, "SELECT COUNT(*) FROM task_artifacts WHERE submission_id=?", wantArtifacts, *completeRun.SubmissionID)
		})
	}
}

func TestAgentRunFinishReasonControlledHTTPErrorKeepsSafeCode(t *testing.T) {
	f, run := runAgentFinishReasonResponse(t, `{"error":{"message":"PRIVATE_MODEL endpoint or key diagnostics"}}`, http.StatusServiceUnavailable)
	assertAgentFinishReasonFailure(t, f, run, "AGENT_MODEL_FAILED")
}

func TestAgentRunFinishReasonStopStillUsesManualReviewChain(t *testing.T) {
	f, run := runAgentFinishReasonResponse(t, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"可供人工复核的完整结果","tool_calls":[],"function_call":null,"refusal":null}}]}`, http.StatusOK)
	if run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.ErrorCode != nil ||
		run.SubmissionID == nil || run.ArtifactID == nil || run.ResultText == nil || *run.ResultText != "可供人工复核的完整结果" {
		t.Fatalf("valid complete text did not follow normal delivery: %#v", run)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review'", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND task_id=?", 1, *run.SubmissionID, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_artifacts WHERE id=? AND task_id=? AND submission_id=?", 1, *run.ArtifactID, f.Task.ID, *run.SubmissionID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='agent_run_failed'", 0, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed' AND source_entity_id=?", 0, run.ID)
}
