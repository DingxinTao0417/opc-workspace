package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Exercise real chat/tool/approval boundaries with a deterministic local model.
// Replacing a rejected intent must never create a Task before a new decision.
func TestAIWorkPlanHarnessRejectedIntentReplacement(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
	calls := 0
	var oldID, newID string
	var currentPlan aiWorkPlanIntent
	emit := func(w http.ResponseWriter, name string, arguments []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0,
				"id": fmt.Sprintf("replace-call-%d", calls), "type": "function",
				"function": map[string]any{"name": name, "arguments": string(arguments)}}}}, "finish_reason": "tool_calls"}}})
		fmt.Fprintf(w, "data: %s\n\n", frame)
	}
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		proposalFromTool := func() string {
			var id string
			for _, message := range payload.Messages {
				if message["role"] != "tool" {
					continue
				}
				content, _ := message["content"].(string)
				var result struct {
					ProposalID string `json:"proposal_id"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(content, "workspace_propose: ")), &result)
				if result.ProposalID != "" {
					id = result.ProposalID
				}
			}
			if id == "" {
				t.Error("model did not receive real proposal ID")
			}
			return id
		}
		switch calls {
		case 1:
			emit(w, "workspace_propose", []byte(`{"action":"task.create","changes":{"title":"Rejected approach","description":"PRIVATE ORIGINAL BODY"}}`))
		case 2:
			oldID = proposalFromTool()
			currentPlan = aiWorkPlanIntent{Title: "PRIVATE REPLACEMENT PLAN", Steps: []aiWorkPlanStep{
				{ID: "original", Title: "Initial approach", Kind: "action", DependsOn: []string{}, ProposalID: oldID},
				{ID: "review", Title: "Review actual result", Kind: "analysis", DependsOn: []string{"original"}, Report: "pending"},
			}}
			emit(w, "workspace_plan", updatePlanArgs(currentPlan, 0))
		case 4, 8:
			emit(w, "workspace_plan", []byte(`{"operation":"read"}`))
		case 5:
			encoded, _ := json.Marshal(payload.Messages)
			if !strings.Contains(string(encoded), "rejected") {
				t.Error("replacement was not based on actual rejected receipt")
			}
			emit(w, "workspace_propose", []byte(`{"action":"task.create","changes":{"title":"Human-approved replacement","description":"PRIVATE REPLACEMENT BODY"}}`))
		case 6:
			newID = proposalFromTool()
			currentPlan.Steps = []aiWorkPlanStep{currentPlan.Steps[0],
				{ID: "replacement", Title: "Revised approach", Kind: "action", DependsOn: []string{}, ProposalID: newID, Replaces: "original"},
				{ID: "review", Title: "Review actual result", Kind: "analysis", DependsOn: []string{"replacement"}, Report: "pending"},
			}
			emit(w, "workspace_plan", updatePlanArgs(currentPlan, 1))
		case 9:
			encoded, _ := json.Marshal(payload.Messages)
			if !strings.Contains(string(encoded), "recorded") || !strings.Contains(string(encoded), "superseded_by") {
				t.Error("final review did not read authoritative replacement receipt")
			}
			currentPlan.Steps[2].Report = "reported_done"
			emit(w, "workspace_plan", updatePlanArgs(currentPlan, 2))
		case 11:
			for _, tool := range payload.Tools {
				function, _ := tool["function"].(map[string]any)
				if function["name"] == "workspace_plan" || function["name"] == "workspace_propose" {
					t.Error("replacement carried permission into an ungranted message")
				}
			}
			encoded, _ := json.Marshal(payload.Messages)
			if strings.Contains(string(encoded), currentPlan.Title) {
				t.Error("plan intent injected without fresh consent")
			}
			streamMockAIDelta(w, `No workspace permissions.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		default:
			streamMockAIDelta(w, `Plan recorded; decisions remain yours.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		}
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "replacement-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider_id": provider.ID, "message": "Prepare an approach for review",
		"workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}}
	send := func(wantCalls int) {
		t.Helper()
		body, _ := json.Marshal(request)
		response := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
		if response.Code != 200 || calls != wantCalls || !strings.Contains(response.Body.String(), "event: done") || strings.Contains(response.Body.String(), "event: error") {
			t.Fatalf("chat calls=%d want=%d response=%s", calls, wantCalls, response.Body.String())
		}
	}
	decide := func(id, decision string) models.AIActionProposal {
		t.Helper()
		var proposal models.AIActionProposal
		if err := store.DB.First(&proposal, "id=?", id).Error; err != nil {
			t.Fatal(err)
		}
		response := performRequest(router, "POST", "/api/v1/ai/actions/"+id+"/decision",
			[]byte(fmt.Sprintf(`{"fingerprint":%q,"decision":%q}`, proposal.Fingerprint, decision)), nil)
		if response.Code != 200 {
			t.Fatalf("%s: %s", decision, response.Body.String())
		}
		return proposal
	}
	send(3)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	old := decide(oldID, "reject")
	var generation models.AIGeneration
	if err := store.DB.First(&generation, "id=?", old.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	request["session_id"] = generation.SessionID
	request["message"] = "Replace the rejected approach with a revised one; ask me to confirm"
	send(7)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
	decide(newID, "confirm")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	request["message"] = "Review the actual result and update the plan"
	send(10)
	response := performRequest(router, "GET", "/api/v1/ai/work-plans?state=all", nil, nil)
	var inbox struct {
		Data []aiWorkPlanInboxItem `json:"data"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &inbox) != nil || len(inbox.Data) != 1 {
		t.Fatalf("inbox: %s", response.Body.String())
	}
	item := inbox.Data[0]
	if item.State != "completed" || item.StepTotal != 3 || item.SupersededStepTotal != 1 || item.SatisfiedStepTotal != 2 {
		t.Fatalf("rejected attempt stranded plan or became success: %+v", item)
	}
	response = performRequest(router, "GET", "/api/v1/ai/sessions/"+generation.SessionID+"/plan?version=1", nil, nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "superseded_by") || !strings.Contains(response.Body.String(), `"state":"rejected"`) {
		t.Fatalf("rewrote history: %s", response.Body.String())
	}
	delete(request, "workspace")
	request["message"] = "Thanks"
	send(11)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='rejected'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='confirmed'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 3)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_plan' AND status='succeeded'", 5)
}
