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

func planTool(t *testing.T, f aiAgentRunFixture) harness.Tool {
	t.Helper()
	registry, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider,
		&aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "actions"}}, f.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_plan")
	if !ok {
		t.Fatal("plan tool unavailable")
	}
	return tool
}

func sampleWorkPlan(proposalID string) aiWorkPlanIntent {
	return aiWorkPlanIntent{Title: "Prepare then record a task", Steps: []aiWorkPlanStep{
		{ID: "research", Title: "Check requirements", Kind: "analysis", DependsOn: []string{}, Report: "reported_done"},
		{ID: "record", Title: "Create the task", Kind: "action", DependsOn: []string{"research"}, ProposalID: proposalID},
		{ID: "review", Title: "Review actual outcome", Kind: "analysis", DependsOn: []string{"record"}, Report: "pending"},
	}}
}

func updatePlanArgs(plan aiWorkPlanIntent, version int64) []byte {
	encoded, _ := json.Marshal(map[string]any{"operation": "update", "expected_version": version, "plan": plan})
	return encoded
}

func decodePlanTool(t *testing.T, tool harness.Tool, args []byte) *aiWorkPlanView {
	t.Helper()
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var envelope struct {
		Plan *aiWorkPlanView `json:"plan"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Plan
}

func TestAIWorkPlanDurableRevisionsApprovalAndContinuation(t *testing.T) {
	f := newAIAgentRunFixture(t)
	tool := planTool(t, f)
	if got := decodePlanTool(t, tool, []byte(`{"operation":"read"}`)); got != nil {
		t.Fatal("invented plan")
	}
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE BUSINESS TITLE","description":"PRIVATE BODY"}}`)
	plan := sampleWorkPlan(proposal.ID)
	first := decodePlanTool(t, tool, updatePlanArgs(plan, 0))
	if first.Version != 1 || first.Steps[1].State != "pending" || first.Steps[1].Satisfied || first.Steps[2].Ready {
		t.Fatalf("false completion: %+v", first)
	}
	if again := decodePlanTool(t, tool, updatePlanArgs(plan, 0)); again.Version != 1 {
		t.Fatal("ambiguous retry appended revision")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1) // only fixture task; plan does not execute proposal
	plan.Steps[2].Title = "Check the recorded Task in the workbench"
	if _, err := tool.Execute(context.Background(), updatePlanArgs(plan, 0)); err == nil {
		t.Fatal("stale revision overwritten")
	}
	if got := decodePlanTool(t, tool, updatePlanArgs(plan, 1)); got.Version != 2 {
		t.Fatal("new revision missing")
	}
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"operation":"read"}`)); err == nil {
		t.Fatal("terminal generation retained tool access")
	}
	body := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint))
	response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", body, nil)
	if response.Code != 200 {
		t.Fatalf("confirm: %s", response.Body.String())
	}
	for _, suffix := range []string{"", "?version=1"} {
		response = performRequest(f.Router, "GET", "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan"+suffix, nil, nil)
		var envelope struct {
			Data aiWorkPlanView `json:"data"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("read: %s", response.Body.String())
		}
		if envelope.Data.Steps[1].State != "recorded" || !envelope.Data.Steps[2].Ready {
			t.Fatalf("missing actual receipt: %+v", envelope.Data)
		}
		for _, secret := range []string{"PRIVATE BUSINESS TITLE", "PRIVATE BODY", "preview", "changes", "fingerprint"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("plan leaked %s", secret)
			}
		}
	}
	// A new runtime instance and generation read SQLite, not an in-memory plan.
	f.Generation.ID = uuid.NewString()
	f.Generation.Status = "streaming"
	if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
		t.Fatal(err)
	}
	resumed := planTool(t, f)
	if got := decodePlanTool(t, resumed, []byte(`{"operation":"read"}`)); got.Version != 2 || got.Steps[1].State != "recorded" {
		t.Fatalf("resume: %+v", got)
	}
	plan.Steps[2].Report = "in_progress"
	if got := decodePlanTool(t, resumed, updatePlanArgs(plan, 2)); got.Version != 3 || got.GenerationID != f.Generation.ID {
		t.Fatal("resume revision missing")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_work_plan_updated'", 3)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_work_plan_updated' AND (current_json LIKE '%Check%' OR current_json LIKE '%PRIVATE%')", 0)
}

func TestAIWorkPlanUpdateEmitsCommittedMetadataOnly(t *testing.T) {
	f := newAIAgentRunFixture(t)
	tool := planTool(t, f)
	updates := make([]aiWorkPlanUpdate, 0, 1)
	ctx := withAIWorkPlanUpdateEmitter(context.Background(), func(update aiWorkPlanUpdate) bool {
		updates = append(updates, update)
		return true
	})
	plan := sampleWorkPlan("")
	plan.Title = "PRIVATE PLAN TITLE"
	result, err := tool.Execute(ctx, updatePlanArgs(plan, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0] != (aiWorkPlanUpdate{GenerationID: f.Generation.ID, Version: 1, StepCount: len(plan.Steps)}) {
		t.Fatalf("update receipt = %#v", updates)
	}
	encoded, _ := json.Marshal(updates[0])
	if strings.Contains(string(encoded), plan.Title) || strings.Contains(string(encoded), "proposal") || strings.Contains(string(encoded), "session") {
		t.Fatalf("stream invalidation leaked plan data: %s", encoded)
	}
	if !strings.Contains(result, plan.Title) {
		t.Fatal("tool result unexpectedly lost its existing model-facing plan contract")
	}
	if _, err := tool.Execute(ctx, []byte(`{"operation":"read"}`)); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("read emitted update: %#v", updates)
	}
}

func TestAIWorkPlanInboxProjectsLatestReceiptsAndFilters(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE INBOX ACTION","description":"PRIVATE BODY"}}`)
	plan := sampleWorkPlan(proposal.ID)
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	if err := f.Store.DB.Model(&f.Generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}

	read := func(path string) (int, []aiWorkPlanInboxItem, aiWorkPlanInboxMeta, string) {
		t.Helper()
		response := performRequest(f.Router, http.MethodGet, path, nil, nil)
		var envelope struct {
			Data []aiWorkPlanInboxItem `json:"data"`
			Meta aiWorkPlanInboxMeta   `json:"meta"`
		}
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("decode inbox: %s", response.Body.String())
		}
		return response.Code, envelope.Data, envelope.Meta, response.Body.String()
	}

	code, items, meta, body := read("/api/v1/ai/work-plans")
	if code != http.StatusOK || len(items) != 1 || items[0].State != "needs_approval" ||
		items[0].PendingApprovalTotal != 1 || items[0].SatisfiedStepTotal != 1 || meta.AttentionTotal != 1 || meta.NeedsApprovalTotal != 1 {
		t.Fatalf("pending inbox = %d %+v %+v", code, items, meta)
	}
	for _, secret := range []string{"PRIVATE INBOX ACTION", "PRIVATE BODY", "changes", "preview", "fingerprint"} {
		if strings.Contains(body, secret) {
			t.Fatalf("agent inbox leaked %s", secret)
		}
	}

	decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint))
	response := performRequest(f.Router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm: %s", response.Body.String())
	}
	code, items, meta, _ = read("/api/v1/ai/work-plans?state=awaiting_continuation&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].State != "awaiting_continuation" ||
		items[0].SatisfiedStepTotal != 2 || meta.AwaitingContinuationTotal != 1 || meta.NeedsApprovalTotal != 0 {
		t.Fatalf("continued inbox = %d %+v %+v", code, items, meta)
	}

	completedSession := models.AISession{ID: uuid.NewString(), Title: "Completed session", Persist: true, Version: 1,
		CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
	if err := f.Store.DB.Create(&completedSession).Error; err != nil {
		t.Fatal(err)
	}
	completedGeneration := f.Generation
	completedGeneration.ID = uuid.NewString()
	completedGeneration.SessionID = completedSession.ID
	completedGeneration.Status = "streaming"
	completedGeneration.RequestKey = nil
	completedGeneration.RequestHash = nil
	if err := f.Store.DB.Create(&completedGeneration).Error; err != nil {
		t.Fatal(err)
	}
	completedPlan := aiWorkPlanIntent{Title: "Finished plan", Steps: []aiWorkPlanStep{{
		ID: "done", Title: "Reported analysis", Kind: "analysis", DependsOn: []string{}, Report: "reported_done",
	}}}
	encodedPlan, _ := json.Marshal(completedPlan)
	if err := f.Store.DB.Create(&aiWorkPlanRevision{SessionID: completedSession.ID, Version: 1, GenerationID: completedGeneration.ID,
		PlanJSON: string(encodedPlan), CreatedAt: f.Generation.UpdatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Model(&completedGeneration).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	code, items, meta, _ = read("/api/v1/ai/work-plans?state=completed")
	if code != http.StatusOK || len(items) != 1 || items[0].SessionID != completedSession.ID || items[0].State != "completed" ||
		meta.CompletedTotal != 1 || meta.AttentionTotal != 1 || meta.Total != 1 {
		t.Fatalf("completed inbox = %d %+v %+v", code, items, meta)
	}
	code, _, _, body = read("/api/v1/ai/work-plans?state=unknown")
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "AI_PLAN_STATE_INVALID") {
		t.Fatalf("invalid state = %d %s", code, body)
	}
}

func TestAIWorkPlanInboxClassificationUsesLiveEvidencePrecedence(t *testing.T) {
	base := aiWorkPlanView{SessionID: uuid.NewString(), Version: 1, GenerationID: uuid.NewString(),
		GenerationStatus: "completed", Title: "Plan", CreatedAt: "2026-09-19T12:00:00Z", AsOf: "2026-09-19T12:01:00Z"}
	for _, test := range []struct {
		name, generationStatus, want string
		steps                        []aiWorkPlanStepView
	}{
		{name: "complete", want: "completed", steps: []aiWorkPlanStepView{{aiWorkPlanStep: aiWorkPlanStep{Kind: "analysis"}, Ready: true, Satisfied: true}}},
		{name: "running generation", generationStatus: "streaming", want: "running", steps: []aiWorkPlanStepView{{aiWorkPlanStep: aiWorkPlanStep{Kind: "analysis"}}}},
		{name: "active run", want: "running", steps: []aiWorkPlanStepView{{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "queued"}}},
		{name: "approval beats active", want: "needs_approval", steps: []aiWorkPlanStepView{
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "running"},
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "pending"},
		}},
		{name: "child pending approval", want: "needs_approval", steps: []aiWorkPlanStepView{
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "child_pending_approval", Ready: true},
		}},
		{name: "recovery beats approval", want: "needs_recovery", steps: []aiWorkPlanStepView{
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "pending"},
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "output_pending"},
		}},
		{name: "retained output also needs recovery", want: "needs_recovery", steps: []aiWorkPlanStepView{
			{aiWorkPlanStep: aiWorkPlanStep{Kind: "action"}, State: "retained"},
		}},
		{name: "continuation", want: "awaiting_continuation", steps: []aiWorkPlanStepView{{aiWorkPlanStep: aiWorkPlanStep{Kind: "analysis"}, State: "awaiting_continuation"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := base
			plan.Steps = test.steps
			if test.generationStatus != "" {
				plan.GenerationStatus = test.generationStatus
			}
			got := summarizeAIWorkPlanInbox(&plan, "Session", plan.CreatedAt)
			if got.State != test.want {
				t.Fatalf("state = %s, want %s (%+v)", got.State, test.want, got)
			}
			if test.name == "child pending approval" && (got.PendingApprovalTotal != 1 || got.SatisfiedStepTotal != 0) {
				t.Fatalf("child pending approval counts = %+v", got)
			}
		})
	}
}

func TestAIWorkPlanFollowupTracksGenerationAndChildApproval(t *testing.T) {
	proposalID := uuid.NewString()
	childID := uuid.NewString()
	out := aiActionResponse{ID: proposalID, Status: "confirmed", Action: aiWorkspaceAction{Action: aiAgentDelegateFollowupAction}, ResultID: &childID,
		AgentFollowupResult: &aiAgentDelegationResult{SessionID: childID, GenerationID: proposalID, Status: "queued"}}
	if state, satisfied := aiWorkPlanActionState(out); state != "queued" || satisfied {
		t.Fatalf("queued follow-up=%s satisfied=%v", state, satisfied)
	}
	if blocker := aiPlanContinuationActionBlocker(out); blocker != "run_active" {
		t.Fatalf("queued follow-up blocker=%s", blocker)
	}
	out.AgentFollowupResult.Status = "completed"
	out.AgentFollowupResult.PendingApprovals = 1
	if state, satisfied := aiWorkPlanActionState(out); state != "child_pending_approval" || satisfied {
		t.Fatalf("unreviewed follow-up=%s satisfied=%v", state, satisfied)
	}
	if blocker := aiPlanContinuationActionBlocker(out); blocker != "pending_approval" {
		t.Fatalf("unreviewed follow-up blocker=%s", blocker)
	}
	out.AgentFollowupResult.PendingApprovals = 0
	if state, satisfied := aiWorkPlanActionState(out); state != "completed" || !satisfied {
		t.Fatalf("completed follow-up=%s satisfied=%v", state, satisfied)
	}
	if blocker := aiPlanContinuationActionBlocker(out); blocker != "" {
		t.Fatalf("completed follow-up blocker=%s", blocker)
	}
	out.AgentFollowupResult.Status = "failed"
	if state, satisfied := aiWorkPlanActionState(out); state != "failed" || satisfied {
		t.Fatalf("failed follow-up=%s satisfied=%v", state, satisfied)
	}
}

func TestAIWorkPlanValidationAndPermissionBoundary(t *testing.T) {
	if !json.Valid(aiWorkPlanSchema()) {
		t.Fatal("invalid tool schema")
	}
	f := newAIAgentRunFixture(t)
	tool := planTool(t, f)
	for _, mutate := range []func(*aiWorkPlanIntent){
		func(p *aiWorkPlanIntent) { p.Steps[1].Report = "reported_done" },
		func(p *aiWorkPlanIntent) { p.Steps[0].DependsOn = []string{"record"} },
		func(p *aiWorkPlanIntent) { p.Steps[1].DependsOn = []string{"record"} },
		func(p *aiWorkPlanIntent) { p.Steps[1].DependsOn = []string{"research", "research"} },
		func(p *aiWorkPlanIntent) { p.Steps[1].ID = "research" },
		func(p *aiWorkPlanIntent) { p.Steps[0].Report = "completed" },
		func(p *aiWorkPlanIntent) { p.Steps[0].Report = "in_progress"; p.Steps[2].Report = "in_progress" },
		func(p *aiWorkPlanIntent) { p.Steps[0].ProposalID = uuid.NewString() },
		func(p *aiWorkPlanIntent) { p.Steps[1].ProposalID = uuid.NewString() },
		func(p *aiWorkPlanIntent) { p.Steps[1].Kind = "shell" },
		func(p *aiWorkPlanIntent) { p.Steps[0].Title = strings.Repeat("字", 301) },
		func(p *aiWorkPlanIntent) { p.Steps[0].DependsOn = nil },
	} {
		plan := sampleWorkPlan("")
		mutate(&plan)
		if _, err := tool.Execute(context.Background(), updatePlanArgs(plan, 0)); err == nil {
			t.Fatalf("accepted invalid plan: %+v", plan)
		}
	}
	for _, args := range []string{
		`{"operation":"read","plan":null}`,
		`{"operation":"update","expected_version":0,"plan":null}`,
		`{"operation":"update","expected_version":0,"plan":{"title":"null proposal","steps":[{"id":"act","title":"Act","kind":"action","depends_on":[],"proposal_id":null}]}}`,
		`{"operation":"update","expected_version":0,"plan":{"title":"null report","steps":[{"id":"act","title":"Act","kind":"action","depends_on":[],"report":null}]}}`,
		`{"operation":"update","expected_version":0,"plan":{"title":"analysis proposal","steps":[{"id":"read","title":"Read","kind":"analysis","depends_on":[],"report":"pending","proposal_id":null}]}}`,
		`{"operation":"execute"}`,
		`{"operation":"read","session_id":"other"}`,
		`{"operation":"read"} {}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	other := f.Generation
	other.ID = uuid.NewString()
	otherSession := models.AISession{ID: uuid.NewString(), Title: "Other", Persist: true, Version: 1, CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
	if err := f.Store.DB.Create(&otherSession).Error; err != nil {
		t.Fatal(err)
	}
	other.SessionID = otherSession.ID
	if err := f.Store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	wrong := f
	wrong.Generation = other
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Other task"}}`)
	if _, err := planTool(t, wrong).Execute(context.Background(), updatePlanArgs(sampleWorkPlan(proposal.ID), 0)); err == nil {
		t.Fatal("cross-session evidence accepted")
	}
	for _, scopes := range [][]string{{"work"}, {"work", "outputs"}, {"finance", "finance_actions"}} {
		reg, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, true, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: scopes}, f.Generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := reg.Get("workspace_plan"); ok {
			t.Fatalf("exposed with %v", scopes)
		}
	}
	reg, err := f.Service.aiChatToolRegistry(f.Generation.SessionID, false, &f.Provider, &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "actions"}}, f.Generation.ID)
	if err == nil {
		if _, ok := reg.Get("workspace_plan"); ok {
			t.Fatal("ephemeral plan exposed")
		}
	}
	if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"config_version": f.Provider.ConfigVersion + 1, "version": f.Provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), updatePlanArgs(sampleWorkPlan(""), 0)); err == nil {
		t.Fatal("revoked Provider retained plan access")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 0)
}

func TestAIWorkPlanRetainsStepAndBoundProposalIdentity(t *testing.T) {
	f := newAIAgentRunFixture(t)
	tool := planTool(t, f)
	firstProposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"First planned task"}}`)
	secondProposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Second planned task"}}`)
	base := sampleWorkPlan(firstProposal.ID)
	decodePlanTool(t, tool, updatePlanArgs(base, 0))

	mutations := []func(*aiWorkPlanIntent){
		func(plan *aiWorkPlanIntent) { plan.Steps = plan.Steps[:2] },
		func(plan *aiWorkPlanIntent) {
			plan.Steps[2].Kind = "action"
			plan.Steps[2].Report = ""
		},
		func(plan *aiWorkPlanIntent) { plan.Steps[1].ProposalID = "" },
		func(plan *aiWorkPlanIntent) { plan.Steps[1].ProposalID = secondProposal.ID },
	}
	for _, mutate := range mutations {
		plan := sampleWorkPlan(firstProposal.ID)
		mutate(&plan)
		if _, err := tool.Execute(context.Background(), updatePlanArgs(plan, 1)); err == nil {
			t.Fatalf("accepted identity-erasing revision: %+v", plan)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)

	base.Steps[2].Title = "Review the recorded outcome in the workbench"
	base.Steps = append(base.Steps, aiWorkPlanStep{ID: "follow_up", Title: "Record a later attempt if needed", Kind: "action", DependsOn: []string{"review"}})
	if got := decodePlanTool(t, tool, updatePlanArgs(base, 1)); got.Version != 2 || len(got.Steps) != 4 {
		t.Fatalf("safe append/edit failed: %+v", got)
	}
}

func TestAIWorkPlanRollbackAndReceiptStates(t *testing.T) {
	f := newAIAgentRunFixture(t)
	tool := planTool(t, f)
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_plan_event BEFORE INSERT ON workflow_events WHEN NEW.action='ai_work_plan_updated' BEGIN SELECT RAISE(ABORT,'rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), updatePlanArgs(sampleWorkPlan(""), 0)); err == nil {
		t.Fatal("event failure accepted")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 0)
	for _, tt := range []struct {
		action, status, runStatus, delivery, submissionStatus, want string
		satisfied                                                   bool
	}{
		{"task.create", "pending", "", "", "", "pending", false},
		{"task.create", "rejected", "", "", "", "rejected", false},
		{"task.create", "expired", "", "", "", "expired", false},
		{"task.create", "confirmed", "", "", "", "recorded", true},
		{"agent_run.start", "confirmed", "queued", "not_ready", "", "queued", false},
		{"agent_run.start", "confirmed", "running", "pending", "", "output_pending", false},
		{"agent_run.start", "confirmed", "succeeded", "retained", "", "retained", false},
		{"agent_run.retry", "confirmed", "succeeded", "submitted", "pending_review", "submitted", true},
		{"agent_run.start", "confirmed", "succeeded", "submitted", "accepted", "submitted", true},
		{"agent_run.start", "confirmed", "succeeded", "submitted", "changes_requested", "submitted", true},
		{"agent_run.start", "confirmed", "succeeded", "submitted", "withdrawn", "submitted", true},
		{"agent_run.start", "confirmed", "succeeded", "submitted", "", "evidence_unavailable", false},
		{"agent_run.start", "confirmed", "failed", "not_ready", "", "failed", false},
		{"agent_run.cancel", "confirmed", "running", "not_ready", "", "running", false},
		{"agent_run.cancel", "confirmed", "cancelled", "not_ready", "", "cancelled", true},
		{"agent_run.recover_output", "confirmed", "succeeded", "retained", "", "retained", false},
		{"agent_run.recover_output", "confirmed", "succeeded", "submitted", "pending_review", "submitted", true},
		{"finance.export_csv", "confirmed", "", "", "", "download_unverified", false},
		{"automation.retry", "confirmed", "failed", "", "", "failed", false},
		{"automation.retry", "confirmed", "succeeded", "", "", "succeeded", true},
	} {
		var submissionStatus *string
		if tt.submissionStatus != "" {
			submissionStatus = &tt.submissionStatus
		}
		out := aiActionResponse{Action: aiWorkspaceAction{Action: tt.action}, Status: tt.status,
			AgentRunResult: &aiAgentRunResult{Status: tt.runStatus, OutputDeliveryStatus: tt.delivery, SubmissionStatus: submissionStatus}, AutomationRunResult: &aiAutomationRetryResult{Status: tt.runStatus}}
		state, satisfied := aiWorkPlanActionState(out)
		if state != tt.want || satisfied != tt.satisfied {
			t.Fatalf("%+v: %s %v", tt, state, satisfied)
		}
	}
	for _, action := range []string{"agent_run.start", "agent_run.retry", "agent_run.cancel", "agent_run.recover_output", "automation.retry"} {
		state, satisfied := aiWorkPlanActionState(aiActionResponse{Action: aiWorkspaceAction{Action: action}, Status: "confirmed"})
		if state != "evidence_unavailable" || satisfied {
			t.Fatalf("deleted evidence marked done: %s", action)
		}
	}
}

func TestAIWorkPlanHarnessResumeDoesNotInheritConsent(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	emit := func(w http.ResponseWriter, name string, args []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("plan-call-%d", calls), "type": "function", "function": map[string]any{"name": name, "arguments": string(args)}}}}, "finish_reason": "tool_calls"}}})
		fmt.Fprintf(w, "data: %s\n\n", frame)
	}
	var proposalID string
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
		last, _ := json.Marshal(payload.Messages)
		switch calls {
		case 1:
			plan := sampleWorkPlan("")
			plan.Title = "PLAN_PRIVATE_INTENT"
			emit(w, "workspace_plan", updatePlanArgs(plan, 0))
		case 2:
			if !strings.Contains(string(last), "PLAN_PRIVATE_INTENT") {
				t.Error("harness did not return durable plan")
			}
			emit(w, "workspace_propose", []byte(`{"action":"task.create","changes":{"title":"Harness planned task"}}`))
		case 3:
			for _, m := range payload.Messages {
				if m["role"] != "tool" {
					continue
				}
				content, _ := m["content"].(string)
				var result struct {
					ProposalID string `json:"proposal_id"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(content, "workspace_propose: ")), &result)
				if result.ProposalID != "" {
					proposalID = result.ProposalID
				}
			}
			if proposalID == "" {
				t.Error("no real proposal returned")
			}
			plan := sampleWorkPlan(proposalID)
			plan.Title = "PLAN_PRIVATE_INTENT"
			emit(w, "workspace_plan", updatePlanArgs(plan, 1))
		case 5:
			for _, tool := range payload.Tools {
				f, _ := tool["function"].(map[string]any)
				if f["name"] == "workspace_plan" {
					t.Error("plan scope inherited")
				}
			}
			if strings.Contains(string(last), "PLAN_PRIVATE_INTENT") {
				t.Error("plan body sent without fresh consent")
			}
			streamMockAIDelta(w, `No new permissions.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		case 6:
			emit(w, "workspace_plan", []byte(`{"operation":"read"}`))
		case 7:
			if !strings.Contains(string(last), "recorded") || !strings.Contains(string(last), "PLAN_PRIVATE_INTENT") {
				t.Error("continuation did not read actual approval result")
			}
			streamMockAIDelta(w, `The task command is recorded; review it next.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		default:
			streamMockAIDelta(w, `Plan saved; review the separate action card.[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		}
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "plan-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider_id": provider.ID, "message": "Check and create a task", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}}
	encoded, _ := json.Marshal(request)
	response := performRequest(router, "POST", "/api/v1/ai/chat", encoded, nil)
	if response.Code != 200 || calls != 4 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("plan chat=%d %s calls=%d", response.Code, response.Body.String(), calls)
	}
	if strings.Count(response.Body.String(), "event: workspace_plan_updated") != 2 ||
		!strings.Contains(response.Body.String(), `"version":1,"step_count":3`) ||
		!strings.Contains(response.Body.String(), `"version":2,"step_count":3`) ||
		strings.Contains(response.Body.String(), "PLAN_PRIVATE_INTENT") {
		t.Fatalf("plan update stream is not metadata-only: %s", response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	var row models.AIActionProposal
	if err := store.DB.First(&row, "id=?", proposalID).Error; err != nil {
		t.Fatal(err)
	}
	response = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatalf("approve=%s", response.Body.String())
	}
	var generation models.AIGeneration
	if err := store.DB.First(&generation, "id=?", row.GenerationID).Error; err != nil {
		t.Fatal(err)
	}
	request["session_id"] = generation.SessionID
	grant := request["workspace"]
	delete(request, "workspace")
	request["message"] = "Hello"
	encoded, _ = json.Marshal(request)
	response = performRequest(router, "POST", "/api/v1/ai/chat", encoded, nil)
	if response.Code != 200 || calls != 5 {
		t.Fatalf("ungranted=%s calls=%d", response.Body.String(), calls)
	}
	request["workspace"] = grant
	request["message"] = "Continue the saved plan"
	encoded, _ = json.Marshal(request)
	response = performRequest(router, "POST", "/api/v1/ai/chat", encoded, nil)
	if response.Code != 200 || calls != 7 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("resume=%s calls=%d", response.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_plan' AND status='succeeded'", 3)
}
