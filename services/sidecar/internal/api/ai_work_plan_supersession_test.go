package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIWorkPlanRejectedActionCanBeExplicitlyReplaced(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Original rejected task"}}`)
	tool := planTool(t, f)
	initial := fmt.Sprintf(`{"operation":"update","expected_version":0,"plan":{"title":"Recover rejected intent","steps":[{"id":"original","title":"Original task","kind":"action","depends_on":[],"proposal_id":%q}]}}`, proposal.ID)
	decodePlanTool(t, tool, []byte(initial))
	decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, proposal.Fingerprint))
	response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	updated := fmt.Sprintf(`{"operation":"update","expected_version":1,"plan":{"title":"Recover rejected intent","steps":[{"id":"original","title":"Original task","kind":"action","depends_on":[],"proposal_id":%q},{"id":"replacement","title":"Revised task","kind":"action","depends_on":[],"replaces":"original"}]}}`, proposal.ID)
	result, err := tool.Execute(context.Background(), []byte(updated))
	if err != nil || !strings.Contains(result, `"superseded_by":"replacement"`) || !strings.Contains(result, `"state":"rejected"`) {
		t.Fatalf("explicit replacement result=%s err=%v", result, err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1) // fixture only
}

func replacementWorkPlan(base aiWorkPlanIntent) aiWorkPlanIntent {
	return aiWorkPlanIntent{Title: base.Title, Steps: []aiWorkPlanStep{
		base.Steps[0], base.Steps[1],
		{ID: "retry", Title: "Explicit revised action", Kind: "action", DependsOn: []string{"research"}, Replaces: "record"},
		{ID: "review", Title: base.Steps[2].Title, Kind: "analysis", DependsOn: []string{"retry"}, Report: "pending"},
	}}
}

func TestAIWorkPlanSupersessionStaticGraphBoundary(t *testing.T) {
	base := sampleWorkPlan(uuid.NewString())
	if err := validateAIWorkPlan(replacementWorkPlan(base)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*aiWorkPlanIntent)
	}{
		{"self replacement", func(p *aiWorkPlanIntent) { p.Steps[2].Replaces = "retry" }},
		{"forward replacement", func(p *aiWorkPlanIntent) { p.Steps[1].Replaces = "retry" }},
		{"replacement cycle", func(p *aiWorkPlanIntent) { p.Steps[1].Replaces = "retry"; p.Steps[2].ProposalID = uuid.NewString() }},
		{"unknown source", func(p *aiWorkPlanIntent) { p.Steps[2].Replaces = "unknown" }},
		{"analysis source", func(p *aiWorkPlanIntent) { p.Steps[2].Replaces = "research" }},
		{"unbound source", func(p *aiWorkPlanIntent) { p.Steps[1].ProposalID = "" }},
		{"analysis replacement", func(p *aiWorkPlanIntent) { p.Steps[2].Kind = "analysis"; p.Steps[2].Report = "pending" }},
		{"depends on own replaced source", func(p *aiWorkPlanIntent) { p.Steps[2].DependsOn = []string{"record"} }},
		{"dependent not explicitly rewired", func(p *aiWorkPlanIntent) { p.Steps[3].DependsOn = []string{"record"} }},
		{"mixed dependency cycle", func(p *aiWorkPlanIntent) { p.Steps[2].DependsOn = []string{"review"} }},
		{"second successor", func(p *aiWorkPlanIntent) {
			p.Steps = append(p.Steps, aiWorkPlanStep{ID: "another", Title: "Competing replacement", Kind: "action", DependsOn: []string{}, Replaces: "record"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := replacementWorkPlan(base)
			tc.mutate(&plan)
			if err := validateAIWorkPlan(plan); err == nil {
				t.Fatalf("accepted invalid graph: %+v", plan)
			}
		})
	}
}

func TestAIWorkPlanSupersessionRevisionKeepsHistoryAndOnlyNewEdges(t *testing.T) {
	base := sampleWorkPlan(uuid.NewString())
	valid := replacementWorkPlan(base)
	if err := validateAIWorkPlanRevision(base, valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*aiWorkPlanIntent, *aiWorkPlanIntent)
	}{
		{"first revision", func(prior, next *aiWorkPlanIntent) { *prior = aiWorkPlanIntent{} }},
		{"same revision source binding", func(prior, next *aiWorkPlanIntent) { prior.Steps[1].ProposalID = "" }},
		{"source newly introduced", func(prior, next *aiWorkPlanIntent) { prior.Steps = append(prior.Steps[:1:1], prior.Steps[2]) }},
		{"old source removed", func(prior, next *aiWorkPlanIntent) { next.Steps = append(next.Steps[:1:1], next.Steps[2:]...) }},
		{"old proposal replaced", func(prior, next *aiWorkPlanIntent) { next.Steps[1].ProposalID = uuid.NewString() }},
		{"source renamed while retiring", func(prior, next *aiWorkPlanIntent) { next.Steps[1].Title = "Hide the old goal" }},
		{"source dependencies changed while retiring", func(prior, next *aiWorkPlanIntent) { next.Steps[1].DependsOn = []string{} }},
		{"old step gains replacement edge", func(prior, next *aiWorkPlanIntent) {
			prior.Steps = append(prior.Steps, aiWorkPlanStep{ID: "retry", Title: "Existing action", Kind: "action", DependsOn: []string{}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := sampleWorkPlan(base.Steps[1].ProposalID)
			next := replacementWorkPlan(prior)
			tc.mutate(&prior, &next)
			if err := validateAIWorkPlanRevision(prior, next); err == nil {
				t.Fatalf("accepted historical rewrite: %+v -> %+v", prior, next)
			}
		})
	}
	for _, mutate := range []func(*aiWorkPlanIntent){
		func(p *aiWorkPlanIntent) { p.Steps[2].Replaces = "" },
		func(p *aiWorkPlanIntent) { p.Steps[2].Replaces = "research" },
		func(p *aiWorkPlanIntent) { p.Steps[1].Title = "Rewritten historical title" },
		func(p *aiWorkPlanIntent) { p.Steps[1].DependsOn = []string{} },
		func(p *aiWorkPlanIntent) { p.Steps = append(p.Steps[:2:2], p.Steps[3]) },
	} {
		next := replacementWorkPlan(base)
		mutate(&next)
		if err := validateAIWorkPlanRevision(valid, next); err == nil {
			t.Fatalf("rewrote established replacement: %+v", next)
		}
	}
}

func TestAIWorkPlanSupersessionRejectsMalformedOrAliasedFields(t *testing.T) {
	f := newAIAgentRunFixture(t)
	proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Do not reinterpret replacement fields"}}`)
	base := sampleWorkPlan(proposal.ID)
	tool := planTool(t, f)
	decodePlanTool(t, tool, updatePlanArgs(base, 0))
	decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, proposal.Fingerprint))
	if response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	valid := string(updatePlanArgs(replacementWorkPlan(base), 1))
	for _, field := range []string{
		`"replaces":null`, `"replaces":""`, `"replaces":17`, `"replaces":{}`, `"replaces":[]`,
		`"Replaces":"record"`, `"REPLACES":"record"`, `"Replaces":null`,
		`"replaces":"record","replaces":"record"`, `"replaces":null,"replaces":"record"`,
		`"replaces":"record","Replaces":null`, `"replaces":"record","repl\u0061ces":"record"`,
	} {
		args := strings.Replace(valid, `"replaces":"record"`, field, 1)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted malformed replacement field: %s", field)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
}

func TestAIWorkPlanSupersessionRejectedAndExpiredCompleteOnlyViaReplacementEvidence(t *testing.T) {
	for _, state := range []string{"rejected", "expired"} {
		t.Run(state, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			now := f.Service.options.Now()
			if state == "expired" {
				f.Service.options.Now = func() time.Time { return now.Add(-25 * time.Hour) }
			}
			proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE original task","description":"PRIVATE original body"}}`)
			f.Service.options.Now = func() time.Time { return now }
			base := sampleWorkPlan(proposal.ID)
			tool := planTool(t, f)
			decodePlanTool(t, tool, updatePlanArgs(base, 0))
			if state == "rejected" {
				decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, proposal.Fingerprint))
				if response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil); response.Code != 200 {
					t.Fatal(response.Body.String())
				}
			}
			next := replacementWorkPlan(base)
			view := decodePlanTool(t, tool, updatePlanArgs(next, 1))
			old := view.Steps[1]
			if old.State != state || old.Satisfied || old.SupersededBy != "retry" || old.Evidence == nil || old.Evidence.ProposalID != proposal.ID || view.Steps[3].Ready {
				t.Fatalf("old evidence changed or unexecuted replacement unblocked dependents: %+v", view)
			}
			if replay := decodePlanTool(t, tool, updatePlanArgs(next, 1)); replay.Version != 2 {
				t.Fatal("same revision retry appended replacement")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_work_plan_updated'", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
			inbox := summarizeAIWorkPlanInbox(view, "Session", now.Format(time.RFC3339Nano))
			if inbox.StepTotal != 4 || inbox.SupersededStepTotal != 1 || inbox.SatisfiedStepTotal != 1 || inbox.State == "completed" {
				t.Fatalf("premature completion: %+v", inbox)
			}
			history := performRequest(f.Router, "GET", "/api/v1/ai/sessions/"+f.Generation.SessionID+"/plan?version=1", nil, nil)
			if history.Code != 200 || strings.Contains(history.Body.String(), "superseded_by") || strings.Contains(history.Body.String(), "PRIVATE") {
				t.Fatalf("historical plan mutated or leaked: %s", history.Body.String())
			}
			replacement := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Approved replacement task"}}`)
			next.Steps[2].ProposalID, next.Steps[3].Report = replacement.ID, "reported_done"
			view = decodePlanTool(t, tool, updatePlanArgs(next, 2))
			if item := summarizeAIWorkPlanInbox(view, "Session", now.Format(time.RFC3339Nano)); item.State != "needs_approval" || item.PendingApprovalTotal != 1 {
				t.Fatalf("replacement pending bypassed human confirmation: %+v", item)
			}
			finishAIGeneration(t, f.Store, f.Generation)
			decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, replacement.Fingerprint))
			if response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+replacement.ID+"/decision", decision, nil); response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			response := performRequest(f.Router, "GET", "/api/v1/ai/work-plans?state=completed", nil, nil)
			var envelope struct {
				Data []aiWorkPlanInboxItem `json:"data"`
			}
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 1 {
				t.Fatalf("completed inbox: %s", response.Body.String())
			}
			if item := envelope.Data[0]; item.StepTotal != 4 || item.SatisfiedStepTotal != 3 || item.SupersededStepTotal != 1 {
				t.Fatalf("history counted as successful action: %+v", item)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 2)
		})
	}
}

func TestAIWorkPlanSupersessionBindingAndSourceStateGate(t *testing.T) {
	for _, kind := range []string{"first_revision", "bind_and_replace", "pending", "unavailable", "confirmed", "foreign", "missing"} {
		t.Run(kind, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Source gate"}}`)
			base := sampleWorkPlan(proposal.ID)
			if kind == "bind_and_replace" {
				base.Steps[1].ProposalID = ""
			}
			tool := planTool(t, f)
			if kind != "first_revision" {
				decodePlanTool(t, tool, updatePlanArgs(base, 0))
			}
			if kind != "pending" && kind != "unavailable" && kind != "confirmed" {
				decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, proposal.Fingerprint))
				if response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil); response.Code != 200 {
					t.Fatal(response.Body.String())
				}
			}
			if kind == "unavailable" || kind == "confirmed" {
				finishAIGeneration(t, f.Store, f.Generation)
				if kind == "unavailable" {
					if err := f.Store.DB.Model(&f.Generation).Update("status", "failed").Error; err != nil {
						t.Fatal(err)
					}
				} else {
					decision := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint))
					if response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", decision, nil); response.Code != 200 {
						t.Fatal(response.Body.String())
					}
				}
				f.Generation.ID, f.Generation.Status = uuid.NewString(), "streaming"
				if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
					t.Fatal(err)
				}
				tool = planTool(t, f)
			}
			if kind == "missing" {
				if err := f.Store.DB.Delete(&proposal).Error; err != nil {
					t.Fatal(err)
				}
			}
			if kind == "foreign" {
				other := models.AISession{ID: uuid.NewString(), Title: "Other session", Persist: true, Version: 1, CreatedAt: f.Generation.CreatedAt, UpdatedAt: f.Generation.UpdatedAt}
				if err := f.Store.DB.Create(&other).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.Model(&f.Generation).Update("session_id", other.ID).Error; err != nil {
					t.Fatal(err)
				}
				f.Generation.ID = uuid.NewString()
				if err := f.Store.DB.Create(&f.Generation).Error; err != nil {
					t.Fatal(err)
				}
				tool = planTool(t, f)
			}
			next := replacementWorkPlan(sampleWorkPlan(proposal.ID))
			version := int64(1)
			if kind == "first_revision" {
				version = 0
			}
			if _, err := tool.Execute(context.Background(), updatePlanArgs(next, version)); err == nil {
				t.Fatal("accepted invalid source/binding")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", version)
			tasks := int64(1)
			if kind == "confirmed" {
				tasks = 2
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", tasks)
		})
	}
}

func TestAIWorkPlanSupersessionChainRetainsEachRejectedAttempt(t *testing.T) {
	f := newAIAgentRunFixture(t)
	old := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"First attempt"}}`)
	base := sampleWorkPlan(old.ID)
	tool := planTool(t, f)
	decodePlanTool(t, tool, updatePlanArgs(base, 0))
	reject := func(row models.AIActionProposal) {
		t.Helper()
		response := performRequest(f.Router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	reject(old)
	second := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"Second attempt"}}`)
	next := replacementWorkPlan(base)
	next.Steps[2].ProposalID = second.ID
	decodePlanTool(t, tool, updatePlanArgs(next, 1))
	reject(second)
	last := aiWorkPlanStep{ID: "last", Title: "Third explicit intent", Kind: "action", DependsOn: []string{"research"}, Replaces: "retry"}
	next.Steps = append(next.Steps[:3:3], last, next.Steps[3])
	next.Steps[4].DependsOn = []string{"last"}
	view := decodePlanTool(t, tool, updatePlanArgs(next, 2))
	if view.Steps[1].SupersededBy != "retry" || view.Steps[2].SupersededBy != "last" || view.Steps[1].Satisfied || view.Steps[2].Satisfied || view.Steps[4].Ready {
		t.Fatalf("chain lost history or fabricated satisfaction: %+v", view)
	}
	item := summarizeAIWorkPlanInbox(view, "Session", f.Generation.CreatedAt)
	if item.StepTotal != 5 || item.SupersededStepTotal != 2 || item.SatisfiedStepTotal != 1 || item.State == "completed" {
		t.Fatalf("chain completion mismatch: %+v", item)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 3)
}

func TestAIWorkPlanSupersessionEvidenceDriftFailsClosedOnProjectionAndReplay(t *testing.T) {
	for _, kind := range []string{"deleted", "clock_rollback", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			now := f.Service.options.Now()
			f.Service.options.Now = func() time.Time { return now.Add(-25 * time.Hour) }
			proposal := proposeTestAction(t, f.Store, f.Tool, `{"action":"task.create","changes":{"title":"PRIVATE expired source","description":"PRIVATE source body"}}`)
			f.Service.options.Now = func() time.Time { return now }
			base := sampleWorkPlan(proposal.ID)
			tool := planTool(t, f)
			decodePlanTool(t, tool, updatePlanArgs(base, 0))
			next := replacementWorkPlan(base)
			decodePlanTool(t, tool, updatePlanArgs(next, 1))
			readAt := now
			switch kind {
			case "deleted":
				if err := f.Store.DB.Delete(&proposal).Error; err != nil {
					t.Fatal(err)
				}
			case "clock_rollback":
				readAt = now.Add(-48 * time.Hour)
				f.Service.options.Now = func() time.Time { return readAt }
			case "unavailable":
				if err := f.Store.DB.Model(&f.Generation).Update("status", "failed").Error; err != nil {
					t.Fatal(err)
				}
			}
			var row aiWorkPlanRevision
			if err := f.Store.DB.Where("session_id=? AND version=2", f.Generation.SessionID).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			view, err := projectAIWorkPlan(f.Store.DB, row, readAt)
			if err == nil || view != nil {
				t.Fatalf("invalid historical evidence silently excluded: %+v err=%v", view, err)
			}
			if _, err := tool.Execute(context.Background(), updatePlanArgs(next, 1)); err == nil {
				t.Fatal("idempotent retry trusted stale replacement evidence")
			}
			if kind == "deleted" {
				for _, path := range []string{"/api/v1/ai/sessions/" + f.Generation.SessionID + "/plan", "/api/v1/ai/work-plans"} {
					response := performRequest(f.Router, "GET", path, nil, nil)
					if response.Code != 500 || strings.Contains(response.Body.String(), "PRIVATE") || strings.Contains(response.Body.String(), proposal.ID) {
						t.Fatalf("unsafe fail-closed read: %s", response.Body.String())
					}
				}
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 2)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
		})
	}
}

func TestAIWorkPlanSupersessionCannotRetireConfirmedAgentRuns(t *testing.T) {
	for _, state := range []string{"queued", "running", "output_pending", "retained", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			proposal := proposeTestAction(t, f.Store, f.Tool, aiAgentRunActionJSON(f))
			var run models.AgentRun
			if state == "queued" {
				run = seedQueuedFrozenAgentRun(t, f)
			} else {
				run = seedRunningAgentRun(t, f)
			}
			now := f.Service.options.Now().Format(time.RFC3339Nano)
			switch state {
			case "output_pending", "retained":
				if err := f.Service.persistPendingAgentRunOutput(run.ID, "PRIVATE agent output", now); err != nil {
					t.Fatal(err)
				}
				if state == "retained" {
					if err := transitionAgentRunOutput(f.Store.DB, run.ID, "PRIVATE agent output", now, agentRunOutputRetained, "TEST_RETAINED", nil, nil); err != nil {
						t.Fatal(err)
					}
				}
			case "failed", "cancelled":
				updates := map[string]any{"status": state, "completed_at": now}
				if state == "failed" {
					updates["error_code"] = "TEST_FAILED"
				}
				if err := f.Store.DB.Model(&run).Updates(updates).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := f.Store.DB.Model(&proposal).Updates(map[string]any{"status": "confirmed", "result_id": run.ID, "result_version": 1, "decided_at": now}).Error; err != nil {
				t.Fatal(err)
			}
			base := sampleWorkPlan(proposal.ID)
			tool := planTool(t, f)
			view := decodePlanTool(t, tool, updatePlanArgs(base, 0))
			if view.Steps[1].State != state {
				t.Fatalf("wrong live Run fixture state=%s want=%s", view.Steps[1].State, state)
			}
			if _, err := tool.Execute(context.Background(), updatePlanArgs(replacementWorkPlan(base), 1)); err == nil {
				t.Fatal("confirmed Run bypassed execution/recovery through replacement")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_work_plan_revisions", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks", 1)
		})
	}
}
