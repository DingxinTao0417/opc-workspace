package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIWorkspaceAgentRunsInbox(t *testing.T) {
	f := newAIAgentRunFixture(t)
	for _, scopes := range [][]string{{"work"}, {"work", "outputs"}} {
		r, err := f.Service.aiChatToolRegistry("ephemeral", false, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		_, exists := r.Get("workspace_agent_runs")
		if exists != (len(scopes) == 2) {
			t.Fatal("incorrect tool consent boundary")
		}
	}
	denied := &aiWorkspaceTool{api: f.Service, policy: harness.Capabilities{}}
	if _, err := denied.agentRuns(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("direct call bypassed consent")
	}
	r, err := f.Service.aiChatToolRegistry("ephemeral", false, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs"}})
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := r.Get("workspace_agent_runs")
	query := func(args string) map[string]any {
		t.Helper()
		raw, err := tool.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"PRIVATE_STAGE", "PRIVATE_INPUT", "result_text", "input_snapshot", "pending_text", "executable_ref"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("leaked %s: %s", secret, raw)
			}
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if empty := query(`{}`); empty["total"] != float64(0) || len(empty["items"].([]any)) != 0 {
		t.Fatalf("empty=%v", empty)
	}
	run := seedRunningAgentRun(t, f)
	if err := f.Service.persistPendingAgentRunOutput(run.ID, "PRIVATE_STAGE", run.CreatedAt); err != nil {
		t.Fatal(err)
	}
	failed := run
	failed.ID, failed.Attempt, failed.Status = uuid.NewString(), 2, "failed"
	failed.ParentRunID = &run.ID
	failed.CompletedAt = &failed.CreatedAt
	errorCode := "AGENT_EXECUTION_FAILED"
	failed.ErrorCode = &errorCode
	failed.InputSnapshotJSON = `{"secret":"PRIVATE_INPUT"}`
	if err := f.Store.DB.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	page := query(`{"status":"failed"}`)
	if page["total"] != float64(1) || page["global_counts"].(map[string]any)["pending_delivery_total"] != float64(1) {
		t.Fatalf("filtered counters=%v", page)
	}
	item := page["items"].([]any)[0].(map[string]any)
	if item["id"] != failed.ID || item["route"] != agentRunRoute(f.Task.ID, failed.ID) {
		t.Fatalf("wrong route=%v", item)
	}
	if item["attempt"] != float64(2) || item["parent_run_id"] != run.ID {
		t.Fatalf("missing retry lineage=%v", item)
	}
	if failed.StartedAt == nil || failed.CompletedAt == nil || item["started_at"] != *failed.StartedAt || item["completed_at"] != *failed.CompletedAt {
		t.Fatalf("unexpected lifecycle timestamps=%v", item)
	}
	scoped := query(`{"task_id":"` + f.Task.ID + `"}`)
	if scoped["total"] != float64(2) {
		t.Fatalf("task filter=%v", scoped)
	}
	children := query(`{"parent_run_id":"` + run.ID + `"}`)
	if children["total"] != float64(1) || children["items"].([]any)[0].(map[string]any)["id"] != failed.ID {
		t.Fatalf("parent filter=%v", children)
	}
	outputs, ok := r.Get("workspace_outputs")
	if !ok {
		t.Fatal("workspace_outputs missing")
	}
	outputPage, err := outputs.Execute(context.Background(), []byte(`{"type":"agent_run","task_id":"`+f.Task.ID+`","limit":20}`))
	if err != nil {
		t.Fatal(err)
	}
	var outputEnvelope struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(outputPage), &outputEnvelope); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, output := range outputEnvelope.Items {
		if output["id"] == failed.ID {
			found = true
			if output["attempt"] != float64(2) || output["parent_run_id"] != run.ID {
				t.Fatalf("output lineage=%v", output)
			}
			if output["started_at"] != *failed.StartedAt || output["completed_at"] != *failed.CompletedAt {
				t.Fatalf("output lifecycle=%v", output)
			}
		}
	}
	if !found {
		t.Fatalf("retry run missing from output page: %v", outputEnvelope.Items)
	}
	get, ok := r.Get("workspace_get")
	if !ok {
		t.Fatal("workspace_get missing")
	}
	detail, err := get.Execute(context.Background(), []byte(`{"type":"agent_run","id":"`+failed.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var detailEnvelope map[string]any
	if err := json.Unmarshal([]byte(detail), &detailEnvelope); err != nil {
		t.Fatal(err)
	}
	if detailEnvelope["attempt"] != float64(2) || detailEnvelope["parent_run_id"] != run.ID {
		t.Fatalf("detail lineage=%v", detailEnvelope)
	}
	if detailEnvelope["started_at"] != *failed.StartedAt || detailEnvelope["completed_at"] != *failed.CompletedAt {
		t.Fatalf("detail lifecycle=%v", detailEnvelope)
	}
	pending := query(`{"output_delivery_status":"pending"}`)
	if pending["total"] != float64(1) || pending["items"].([]any)[0].(map[string]any)["id"] != run.ID {
		t.Fatalf("pending=%v", pending)
	}
	first, last := query(`{"limit":1}`), query(`{"limit":1,"offset":1}`)
	if first["next_offset"] != float64(1) || last["next_offset"] != nil || first["items"].([]any)[0].(map[string]any)["id"] == last["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("unstable pagination")
	}
	for _, args := range []string{`{"status":"invented"}`, `{"output_delivery_status":"done"}`, `{"task_id":"not-a-uuid"}`, `{"parent_run_id":"NOT-A-UUID"}`, `{"limit":0}`, `{"offset":1001}`, `{"result_text":true}`} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	// A bounded window must not hand the model an unusable next cursor or
	// misrepresent unread records as an exhausted dataset.
	extra := make([]models.AgentRun, 1001)
	for i := range extra {
		extra[i] = failed
		extra[i].ID = uuid.NewString()
		extra[i].Attempt = i + 3
	}
	if err := f.Store.DB.CreateInBatches(extra, 20).Error; err != nil {
		t.Fatal(err)
	}
	window := query(`{"offset":1000,"limit":1}`)
	if window["has_more"] != true || window["window_limited"] != true || window["next_offset"] != nil {
		t.Fatalf("window=%v", window)
	}
	if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"config_version": f.Provider.ConfigVersion + 1, "version": f.Provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("provider change did not revoke consent")
	}
	if aiProgressToolName("workspace_agent_runs") != "workspace_agent_runs" {
		t.Fatal("missing progress projection")
	}
}

func TestAIWorkspaceAgentRunViewsTrackHumanSubmissionReview(t *testing.T) {
	f := newAIAgentRunFixture(t)
	addAgentRunOwnerReviewer(t, f)
	run := seedRunningAgentRun(t, f)
	completedAt := f.Service.options.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := f.Service.finalizeAgentRun(run, "Reviewable output", "", completedAt); err != nil {
		t.Fatalf("finalize Agent Run: %v", err)
	}
	if err := f.Store.DB.Take(&run, "id=?", run.ID).Error; err != nil {
		t.Fatalf("reload finalized Agent Run: %v", err)
	}
	if run.SubmissionID == nil {
		t.Fatal("Agent Run did not link its Submission")
	}

	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "outputs"}}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := registry.Get("workspace_agent_runs")
	outputs, _ := registry.Get("workspace_outputs")
	get, _ := registry.Get("workspace_get")
	read := func(expectedStatus string) {
		t.Helper()
		listJSON, err := runs.Execute(context.Background(), []byte(`{"task_id":"`+f.Task.ID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var list struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal([]byte(listJSON), &list); err != nil {
			t.Fatal(err)
		}
		assertSubmissionStatus := func(record map[string]any, where string) {
			t.Helper()
			if record["id"] != run.ID || record["submission_id"] != *run.SubmissionID || record["submission_status"] != expectedStatus {
				t.Fatalf("%s did not expose current review state: %#v", where, record)
			}
		}
		var found bool
		for _, item := range list.Items {
			if item["id"] == run.ID {
				found = true
				assertSubmissionStatus(item, "workspace_agent_runs")
			}
		}
		if !found {
			t.Fatal("Agent Run missing from workspace_agent_runs")
		}
		outputJSON, err := outputs.Execute(context.Background(), []byte(`{"type":"agent_run","task_id":"`+f.Task.ID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var outputList struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal([]byte(outputJSON), &outputList); err != nil {
			t.Fatal(err)
		}
		var outputFound bool
		for _, item := range outputList.Items {
			if item["id"] == run.ID {
				outputFound = true
				assertSubmissionStatus(item, "workspace_outputs")
			}
		}
		if !outputFound {
			t.Fatal("Agent Run missing from workspace_outputs")
		}
		detailJSON, err := get.Execute(context.Background(), []byte(`{"type":"agent_run","id":"`+run.ID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		var detail map[string]any
		if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
			t.Fatal(err)
		}
		assertSubmissionStatus(detail, "workspace_get")
	}
	read("pending_review")

	var task models.Task
	if err := f.Store.DB.Take(&task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	decision := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{
		"If-Match": fmt.Sprintf(`"%d"`, task.Version), "Idempotency-Key": "review-agent-run-submission",
	})
	if decision.Code != http.StatusOK {
		t.Fatalf("owner review=%d %s", decision.Code, decision.Body.String())
	}
	read("accepted")
}
