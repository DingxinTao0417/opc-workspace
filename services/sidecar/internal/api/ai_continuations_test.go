package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type continuationTestModel struct {
	calls   atomic.Int32
	entered chan struct{}
}

func (m *continuationTestModel) Stream(ctx context.Context, req harness.Request, delta func(string), _ func(string)) (harness.Turn, error) {
	m.calls.Add(1)
	if m.entered != nil {
		select {
		case m.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return harness.Turn{}, ctx.Err()
	}
	text := "暂时没有新的可推进事项。"
	if delta != nil {
		delta(text)
	}
	return harness.Turn{Text: text + `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`}, nil
}

func newContinuationTestFixture(t *testing.T) (aiAgentRunFixture, *API, *continuationTestModel) {
	t.Helper()
	f := newAIAgentRunFixture(t)
	a := f.Router.aiContinuations.a
	a.aiContinuations.close()
	a.options.ContinuationScanInterval = -1
	a.aiContinuations = newAIContinuationCoordinator(a)
	f.Router.aiContinuations = a.aiContinuations
	m := &continuationTestModel{}
	a.harnessClient = m
	plan := aiWorkPlanIntent{Title: "Review pending work", Steps: []aiWorkPlanStep{{ID: "research", Title: "Review current facts", Kind: "analysis", DependsOn: []string{}, Report: "pending"}}}
	decodePlanTool(t, planTool(t, f), updatePlanArgs(plan, 0))
	finishAIGeneration(t, f.Store, f.Generation)
	return f, a, m
}

func continuationTestInput(f aiAgentRunFixture) createAIContinuationRequest {
	return createAIContinuationRequest{ExpectedSessionVersion: 1, ExpectedPlanVersion: 1, ProviderID: f.Provider.ID, ExpectedProviderVersion: f.Provider.Version, ExpectedProviderConfigVersion: f.Provider.ConfigVersion, Workspace: &aiWorkspaceGrant{ProviderVersion: f.Provider.Version, Scopes: []string{"work", "actions"}}, MaxTurns: 3, TTLMinutes: 30, ConfirmAutomaticContinuation: true}
}

func createContinuationTestLease(t *testing.T, f aiAgentRunFixture, input createAIContinuationRequest) aiContinuationResponse {
	t.Helper()
	body, _ := json.Marshal(input)
	r := performRequest(f.Router, "POST", "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", body, nil)
	var response struct {
		Data aiContinuationResponse `json:"data"`
	}
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &response) != nil {
		t.Fatalf("create lease=%d %s", r.Code, r.Body.String())
	}
	if response.Data.Workspace.ProviderVersion != f.Provider.Version || response.Data.Provider.ConfigVersion != f.Provider.ConfigVersion || response.Data.TurnsStarted != 0 {
		t.Fatalf("bad authorization snapshot: %+v", response.Data)
	}
	for _, private := range []string{"base_url", "WorkspaceJSON", "workspace_json", "last_observation_hash", "MUST_NOT_LEAK"} {
		if strings.Contains(r.Body.String(), private) {
			t.Fatalf("private response field %s", private)
		}
	}
	return response.Data
}

func waitContinuationWorkers(t *testing.T, a *API) {
	t.Helper()
	done := make(chan struct{})
	go func() { a.aiContinuations.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("continuation worker did not finish")
	}
}

func TestAIContinuationExplicitAuthorizationAndManualDraftGuard(t *testing.T) {
	f, a, m := newContinuationTestFixture(t)
	for _, change := range []func(*createAIContinuationRequest){
		func(i *createAIContinuationRequest) { i.ConfirmAutomaticContinuation = false },
		func(i *createAIContinuationRequest) { i.MaxTurns = 9 },
		func(i *createAIContinuationRequest) { i.TTLMinutes = 121 },
		func(i *createAIContinuationRequest) { i.ExpectedSessionVersion = 2 },
		func(i *createAIContinuationRequest) { i.ExpectedPlanVersion = 2 },
		func(i *createAIContinuationRequest) { i.ExpectedProviderConfigVersion++ },
		func(i *createAIContinuationRequest) { i.Workspace = nil },
		func(i *createAIContinuationRequest) { i.Workspace.Scopes = []string{"work"} },
		func(i *createAIContinuationRequest) { i.Workspace.Scopes = append(i.Workspace.Scopes, "workspace_ui") },
		func(i *createAIContinuationRequest) {
			i.Workspace.Scopes = append(i.Workspace.Scopes, "workspace_browser")
		},
	} {
		input := continuationTestInput(f)
		change(&input)
		body, _ := json.Marshal(input)
		r := performRequest(f.Router, "POST", "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", body, nil)
		if r.Code != 409 && r.Code != 422 {
			t.Fatalf("invalid authorization=%d %s", r.Code, r.Body.String())
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_continuations", 0)
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	r := performRequest(f.Router, "POST", "/api/v1/ai/chat", []byte(fmt.Sprintf(`{"session_id":%q,"provider_id":%q,"message":"do not lose this draft"}`, f.Generation.SessionID, f.Provider.ID)), nil)
	assertAPIError(t, r, 409, "AI_CONTINUATION_ACTIVE")
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages", 0)
	if m.calls.Load() != 0 {
		t.Fatal("authorization or manual guard called model")
	}
	for n := 0; n < 2; n++ {
		r = performRequest(f.Router, "POST", "/api/v1/ai/continuations/"+lease.ID+"/stop", nil, nil)
		if r.Code != 200 {
			t.Fatalf("stop=%d %s", r.Code, r.Body.String())
		}
	}
	a.aiContinuations.scan()
	if m.calls.Load() != 0 {
		t.Fatal("stopped lease ran")
	}
}

func TestAIContinuationRecentTerminalResultsSurviveReloadWithoutReauthorization(t *testing.T) {
	f, a, model := newContinuationTestFixture(t)
	current := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clock := current.Add(-25 * time.Hour)
	a.options.Now = func() time.Time { return clock }
	old := createContinuationTestLease(t, f, continuationTestInput(f))
	r := performRequest(f.Router, "POST", "/api/v1/ai/continuations/"+old.ID+"/stop", nil, nil)
	if r.Code != 200 {
		t.Fatalf("stop old lease=%d %s", r.Code, r.Body.String())
	}
	clock = current
	for i := 0; i < maxRecentAIContinuations+2; i++ {
		lease := createContinuationTestLease(t, f, continuationTestInput(f))
		r = performRequest(f.Router, "POST", "/api/v1/ai/continuations/"+lease.ID+"/stop", nil, nil)
		if r.Code != 200 {
			t.Fatalf("stop recent lease=%d %s", r.Code, r.Body.String())
		}
	}
	active := createContinuationTestLease(t, f, continuationTestInput(f))
	r = performRequest(f.Router, "GET", "/api/v1/ai/continuations?status=recent", nil, nil)
	var response struct {
		Data []aiContinuationResponse `json:"data"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &response) != nil || len(response.Data) != maxRecentAIContinuations {
		t.Fatalf("recent bounded history=%d %s", r.Code, r.Body.String())
	}
	for _, lease := range response.Data {
		if lease.ID == old.ID || lease.ID == active.ID || lease.Status != "stopped" || lease.Reason != "user_stopped" || lease.SessionTitle == nil || *lease.SessionTitle == "" {
			t.Fatalf("recent returned expired/active/wrong state: %+v", lease)
		}
	}
	for _, private := range []string{"base_url", "workspace_json", "last_observation_hash"} {
		if strings.Contains(r.Body.String(), private) {
			t.Fatalf("recent leaked private field %s", private)
		}
	}
	if model.calls.Load() != 0 {
		t.Fatal("reading recent results started a model")
	}
	r = performRequest(f.Router, "GET", "/api/v1/ai/continuations?status=active", nil, nil)
	var activeResponse struct {
		Data []aiContinuationResponse `json:"data"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &activeResponse) != nil || len(activeResponse.Data) != 1 || activeResponse.Data[0].ID != active.ID || activeResponse.Data[0].SessionTitle == nil {
		t.Fatalf("active query changed=%d %s", r.Code, r.Body.String())
	}
	r = performRequest(f.Router, "GET", "/api/v1/ai/continuations?status=recent&limit=100", nil, nil)
	assertAPIError(t, r, 422, "AI_CONTINUATION_QUERY_INVALID")
}

func TestAIContinuationActualGenerationOriginAndNoProgress(t *testing.T) {
	f, a, m := newContinuationTestFixture(t)
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	for i := 0; i < 4; i++ {
		a.aiContinuations.scan()
		waitContinuationWorkers(t, a)
	}
	var row models.AIContinuation
	if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "stopped" || row.Reason != "no_progress" || row.TurnsStarted != 1 || m.calls.Load() != 1 {
		t.Fatalf("not bounded: %+v calls=%d", row, m.calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_continuation_turns", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages", 2)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_memory_entries", 0)
	if row.LastGenerationID == nil {
		t.Fatal("missing generation")
	}
	r := performRequest(f.Router, "GET", "/api/v1/ai/generations/"+*row.LastGenerationID, nil, nil)
	var recovered struct {
		Data struct {
			Origin  *aiGenerationOrigin `json:"origin"`
			Content string              `json:"content"`
		} `json:"data"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &recovered) != nil || recovered.Data.Origin == nil || recovered.Data.Origin.ContinuationID != row.ID || recovered.Data.Content != "暂时没有新的可推进事项。" {
		t.Fatalf("generation origin/body: %s", r.Body.String())
	}
	r = performRequest(f.Router, "GET", "/api/v1/ai/sessions/"+row.SessionID+"/messages", nil, nil)
	var history struct {
		Data []aiMessageResponse `json:"data"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &history) != nil || len(history.Data) != 2 {
		t.Fatalf("history: %s", r.Body.String())
	}
	for _, message := range history.Data {
		if message.Origin == nil || message.Origin.TurnIndex != 1 || message.Origin.MaxTurns != 3 {
			t.Fatalf("unlabeled automatic message %+v", message)
		}
	}
}

func TestAIContinuationWaitsForOpenWorkspaceAccessRequest(t *testing.T) {
	f, a, model := newContinuationTestFixture(t)
	if err := persistAIWorkspaceAccessRequest(f.Store.DB, f.Generation.ID, []string{"clients"}, "open", nowStamp(a)); err != nil {
		t.Fatal(err)
	}
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	if lease.Reason != "pending_approval" {
		t.Fatalf("lease did not disclose pending human review: %+v", lease)
	}
	a.aiContinuations.scan()
	waitContinuationWorkers(t, a)
	var row models.AIContinuation
	if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "waiting" || row.Reason != "pending_approval" || row.TurnsStarted != 0 || model.calls.Load() != 0 {
		t.Fatalf("open permission request was consumed by background continuation: %+v calls=%d", row, model.calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='open'", 1, f.Generation.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_continuation_turns", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_messages", 0)

	dismissed := performRequest(f.Router, "POST", "/api/v1/ai/generations/"+f.Generation.ID+"/access-request/dismiss", nil, nil)
	if dismissed.Code != 204 {
		t.Fatalf("dismiss=%d %s", dismissed.Code, dismissed.Body.String())
	}
	a.aiContinuations.scan()
	waitContinuationWorkers(t, a)
	if model.calls.Load() != 1 {
		t.Fatalf("dismissed permission request did not unblock continuation: calls=%d", model.calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='dismissed'", 1, f.Generation.ID)
}

func TestAIContinuationMissingScopeBecomesHumanReviewRequest(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			f, a, _ := newContinuationTestFixture(t)
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if !aiBudgetHasTool(payload, "workspace_request_access") {
					t.Error("bounded continuation cannot request human review of a missing scope")
				}
				switch call {
				case 1:
					if strings.Contains(string(raw), "不请求新增权限") {
						t.Error("automatic prompt contradicts the human-review request tool")
					}
					writeAIBudgetToolTurn(w, protocol, "missing-clients", "workspace_request_access", `{"scopes":["clients"]}`)
				case 2:
					result := aiPlanRetryHarnessResult(t, payload, protocol, "missing-clients", "workspace_request_access")
					if !strings.Contains(result, `"requested":true`) || !strings.Contains(result, `"granted":false`) {
						t.Errorf("tool result did not distinguish human review from an actual grant: %s", result)
					}
					writeAIBudgetTextTurn(w, protocol, `还需要你核对客户资料权限；本轮没有读取客户资料。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected extra automatic model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `不能自动继续。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, f.Router, f.Store, upstream, protocol)
			a.harnessClient = nil
			input := continuationTestInput(f)
			input.ProviderID = provider.ID
			input.ExpectedProviderVersion = provider.Version
			input.ExpectedProviderConfigVersion = provider.ConfigVersion
			input.Workspace.ProviderVersion = provider.Version
			body, _ := json.Marshal(input)
			created := performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", body, nil)
			var envelope struct {
				Data aiContinuationResponse `json:"data"`
			}
			if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &envelope) != nil {
				t.Fatalf("create continuation=%d %s", created.Code, created.Body.String())
			}
			a.aiContinuations.scan()
			waitContinuationWorkers(t, a)
			a.aiContinuations.scan()
			var row models.AIContinuation
			if err := f.Store.DB.Take(&row, "id=?", envelope.Data.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != "waiting" || row.Reason != "pending_approval" || row.TurnsStarted != 1 || row.LastGenerationID == nil || upstream.calls.Load() != 2 {
				t.Fatalf("missing permission did not pause for human review: %+v calls=%d", row, upstream.calls.Load())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_workspace_access_requests WHERE generation_id=? AND status='open'", 1, *row.LastGenerationID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
			for _, path := range []string{"/api/v1/ai/generations/" + *row.LastGenerationID, "/api/v1/ai/sessions/" + row.SessionID + "/messages"} {
				read := performRequest(f.Router, http.MethodGet, path, nil, nil)
				if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"access_request":{"scopes":["clients"]}`) {
					t.Fatalf("human-review card not recoverable at %s: %d %s", path, read.Code, read.Body.String())
				}
			}
			inbox := performRequest(f.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
			if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), `"access_request_total":1`) || strings.Contains(inbox.Body.String(), `"scopes"`) {
				t.Fatalf("permission review inbox exposed content or omitted item: %d %s", inbox.Code, inbox.Body.String())
			}
			a.aiContinuations.scan()
			if upstream.calls.Load() != 2 {
				t.Fatal("open permission request started another automatic turn")
			}
			dismissed := performRequest(f.Router, http.MethodPost, "/api/v1/ai/generations/"+*row.LastGenerationID+"/access-request/dismiss", nil, nil)
			if dismissed.Code != http.StatusNoContent {
				t.Fatalf("dismiss=%d %s", dismissed.Code, dismissed.Body.String())
			}
			a.aiContinuations.scan()
			if err := f.Store.DB.Take(&row, "id=?", envelope.Data.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != "stopped" || row.Reason != "no_progress" || row.TurnsStarted != 1 || upstream.calls.Load() != 2 {
				t.Fatalf("ignored request replayed a model turn or remained active: %+v calls=%d", row, upstream.calls.Load())
			}
		})
	}
}

func TestAIContinuationFailedScopeRequestDoesNotLeaveReviewCard(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	upstream := newAIBudgetMockUpstream(t, "openai_chat", func(call int, _ map[string]any, _ []byte, w http.ResponseWriter) {
		if call == 1 {
			writeAIBudgetToolTurn(w, "openai_chat", "missing-clients", "workspace_request_access", `{"scopes":["clients"]}`)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	})
	provider := createAIBudgetProvider(t, f.Router, f.Store, upstream, "openai_chat")
	a.harnessClient = nil
	input := continuationTestInput(f)
	input.ProviderID = provider.ID
	input.ExpectedProviderVersion = provider.Version
	input.ExpectedProviderConfigVersion = provider.ConfigVersion
	input.Workspace.ProviderVersion = provider.Version
	body, _ := json.Marshal(input)
	created := performRequest(f.Router, http.MethodPost, "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", body, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create continuation=%d %s", created.Code, created.Body.String())
	}
	a.aiContinuations.scan()
	waitContinuationWorkers(t, a)
	if upstream.calls.Load() != 2 {
		t.Fatalf("failed automatic model was transparently retried: calls=%d", upstream.calls.Load())
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_workspace_access_requests", 0)
	inbox := performRequest(f.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), `"access_request_total":0`) {
		t.Fatalf("failed generation left a permission review item: %d %s", inbox.Code, inbox.Body.String())
	}
}

func TestAIContinuationCancellationStopsOwnedGenerationOnly(t *testing.T) {
	f, a, m := newContinuationTestFixture(t)
	m.entered = make(chan struct{}, 1)
	lease := createContinuationTestLease(t, f, continuationTestInput(f))
	a.aiContinuations.scan()
	select {
	case <-m.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model not started")
	}
	var row models.AIContinuation
	a.db.Take(&row, "id=?", lease.ID)
	if row.CurrentGenerationID == nil {
		t.Fatal("missing owned generation")
	}
	r := performRequest(f.Router, "POST", "/api/v1/ai/generations/"+*row.CurrentGenerationID+"/cancel", nil, nil)
	if r.Code != 202 {
		t.Fatalf("cancel=%d %s", r.Code, r.Body.String())
	}
	waitContinuationWorkers(t, a)
	for n := 0; n < 3; n++ {
		a.aiContinuations.scan()
	}
	a.db.Take(&row, "id=?", lease.ID)
	if row.Status != "stopped" || row.Reason != "user_stopped" || row.TurnsStarted != 1 || m.calls.Load() != 1 {
		t.Fatalf("cancelled continuation restarted: %+v", row)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAIContinuationExpiryDriftAndStartupNeverReplay(t *testing.T) {
	for _, reason := range []string{"time_limit", "provider_changed", "process_restart", "restore_pending", "plan_changed"} {
		t.Run(reason, func(t *testing.T) {
			f, a, m := newContinuationTestFixture(t)
			lease := createContinuationTestLease(t, f, continuationTestInput(f))
			switch reason {
			case "time_limit":
				now := a.options.Now().Add(time.Hour)
				a.options.Now = func() time.Time { return now }
			case "provider_changed":
				if err := a.db.Model(&models.AIProvider{}).Where("id=?", f.Provider.ID).Update("version", f.Provider.Version+1).Error; err != nil {
					t.Fatal(err)
				}
			case "process_restart":
				if err := interruptAIContinuations(a.db, reason, a.options.Now()); err != nil {
					t.Fatal(err)
				}
			case "restore_pending":
				a.maintenance.Lock()
				a.restorePending.Store(true)
				a.cancelAIRunsForRestore()
				a.maintenance.Unlock()
			case "plan_changed":
				next := nextAIPlanGeneration(t, f)
				p := aiWorkPlanIntent{Title: "Unrelated changed objective", Steps: []aiWorkPlanStep{{ID: "research", Title: "Different work", Kind: "analysis", DependsOn: []string{}, Report: "pending"}}}
				decodePlanTool(t, planTool(t, next), updatePlanArgs(p, 1))
				finishAIGeneration(t, f.Store, next.Generation)
			}
			for n := 0; n < 3; n++ {
				a.aiContinuations.scan()
			}
			var row models.AIContinuation
			a.db.Take(&row, "id=?", lease.ID)
			if continuationActive(row.Status) || row.Reason != reason || m.calls.Load() != 0 || row.TurnsStarted != 0 {
				t.Fatalf("unsafe replay: %+v calls=%d", row, m.calls.Load())
			}
		})
	}
}

func TestAIContinuationRestartRequiresSeparateConsentAndNeverReplaysAnActiveTurn(t *testing.T) {
	t.Run("waiting opt-in", func(t *testing.T) {
		f, a, model := newContinuationTestFixture(t)
		input := continuationTestInput(f)
		input.ResumeAfterRestart = true
		body, _ := json.Marshal(input)
		response := performRequest(f.Router, "POST", "/api/v1/ai/sessions/"+f.Generation.SessionID+"/continuation", body, nil)
		assertAPIError(t, response, 422, "AI_CONTINUATION_INVALID")
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM ai_continuations", 0)

		input.ConfirmRestartContinuation = true
		lease := createContinuationTestLease(t, f, input)
		if !lease.ResumeAfterRestart {
			t.Fatal("explicit restart consent was not returned")
		}
		var before models.AIContinuation
		if err := a.db.Take(&before, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := recoverAIContinuationsOnStartup(a.db, false, a.options.Now()); err != nil {
			t.Fatal(err)
		}
		var after models.AIContinuation
		if err := a.db.Take(&after, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if after.Status != "waiting" || after.Version != before.Version || after.TurnsStarted != 0 || model.calls.Load() != 0 {
			t.Fatalf("safe waiting lease was not preserved: %+v", after)
		}
		a.aiContinuations.scan()
		waitContinuationWorkers(t, a)
		if err := a.db.Take(&after, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if model.calls.Load() != 1 || after.TurnsStarted != 1 {
			t.Fatalf("preserved lease did not spend exactly one new turn: %+v calls=%d", after, model.calls.Load())
		}
	})
	t.Run("default still interrupts", func(t *testing.T) {
		f, a, model := newContinuationTestFixture(t)
		lease := createContinuationTestLease(t, f, continuationTestInput(f))
		if lease.ResumeAfterRestart {
			t.Fatal("restart permission was implicitly enabled")
		}
		if err := recoverAIContinuationsOnStartup(a.db, false, a.options.Now()); err != nil {
			t.Fatal(err)
		}
		a.aiContinuations.scan()
		var row models.AIContinuation
		if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "interrupted" || row.Reason != "process_restart" || model.calls.Load() != 0 {
			t.Fatalf("default lease replayed: %+v calls=%d", row, model.calls.Load())
		}
	})
	t.Run("graceful shutdown preserves only an idle opted-in lease", func(t *testing.T) {
		f, a, model := newContinuationTestFixture(t)
		input := continuationTestInput(f)
		input.ResumeAfterRestart, input.ConfirmRestartContinuation = true, true
		lease := createContinuationTestLease(t, f, input)
		a.aiContinuations.close()
		var row models.AIContinuation
		if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "waiting" || row.Reason != "ready" || model.calls.Load() != 0 {
			t.Fatalf("idle authorization did not survive shutdown: %+v", row)
		}
	})
	t.Run("expired or changed identity stops before any new request", func(t *testing.T) {
		for _, mode := range []string{"expired", "provider_changed"} {
			t.Run(mode, func(t *testing.T) {
				f, a, model := newContinuationTestFixture(t)
				input := continuationTestInput(f)
				input.ResumeAfterRestart, input.ConfirmRestartContinuation = true, true
				lease := createContinuationTestLease(t, f, input)
				if mode == "expired" {
					future := a.options.Now().Add(time.Hour)
					a.options.Now = func() time.Time { return future }
				} else if err := a.db.Model(&models.AIProvider{}).Where("id=?", f.Provider.ID).Update("version", f.Provider.Version+1).Error; err != nil {
					t.Fatal(err)
				}
				if err := recoverAIContinuationsOnStartup(a.db, false, a.options.Now()); err != nil {
					t.Fatal(err)
				}
				a.aiContinuations.scan()
				var row models.AIContinuation
				if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
					t.Fatal(err)
				}
				wantStatus, wantReason := "stopped", "provider_changed"
				if mode == "expired" {
					wantStatus, wantReason = "expired", "time_limit"
				}
				if row.Status != wantStatus || row.Reason != wantReason || model.calls.Load() != 0 {
					t.Fatalf("unsafe resumed authorization: %+v calls=%d", row, model.calls.Load())
				}
			})
		}
	})
	t.Run("running opt-in interrupts", func(t *testing.T) {
		f, a, model := newContinuationTestFixture(t)
		model.entered = make(chan struct{}, 1)
		input := continuationTestInput(f)
		input.ResumeAfterRestart, input.ConfirmRestartContinuation = true, true
		lease := createContinuationTestLease(t, f, input)
		a.aiContinuations.scan()
		select {
		case <-model.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("automatic turn did not start")
		}
		if err := recoverAIContinuationsOnStartup(a.db, false, a.options.Now()); err != nil {
			t.Fatal(err)
		}
		a.aiContinuations.cancelAll()
		waitContinuationWorkers(t, a)
		var row models.AIContinuation
		if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "interrupted" || row.Reason != "process_restart" || row.TurnsStarted != 1 || model.calls.Load() != 1 {
			t.Fatalf("uncertain active turn replayed: %+v calls=%d", row, model.calls.Load())
		}
	})
	t.Run("backup restore always revokes old authorization", func(t *testing.T) {
		f, a, model := newContinuationTestFixture(t)
		input := continuationTestInput(f)
		input.ResumeAfterRestart, input.ConfirmRestartContinuation = true, true
		lease := createContinuationTestLease(t, f, input)
		if err := recoverAIContinuationsOnStartup(a.db, true, a.options.Now()); err != nil {
			t.Fatal(err)
		}
		a.aiContinuations.scan()
		var row models.AIContinuation
		if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != "interrupted" || row.Reason != "restore_pending" || model.calls.Load() != 0 {
			t.Fatalf("restored backup revived old authorization: %+v calls=%d", row, model.calls.Load())
		}
	})
}

func TestAIContinuationFinishedWorkerClearsMarkerOnShutdownAndExpiry(t *testing.T) {
	for _, mode := range []string{"shutdown", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f, a, model := newContinuationTestFixture(t)
			var clock atomic.Int64
			clock.Store(a.options.Now().UnixNano())
			a.options.Now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			model.entered = make(chan struct{}, 1)
			lease := createContinuationTestLease(t, f, continuationTestInput(f))
			a.aiContinuations.scan()
			select {
			case <-model.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("automatic worker not started")
			}
			if mode == "shutdown" {
				a.aiContinuations.close()
			} else {
				clock.Add(int64(time.Hour))
				a.aiContinuations.cancelOne(lease.ID)
				waitContinuationWorkers(t, a)
			}
			var row models.AIContinuation
			if err := a.db.Take(&row, "id=?", lease.ID).Error; err != nil {
				t.Fatal(err)
			}
			if continuationActive(row.Status) || row.CurrentGenerationID != nil || row.LastGenerationID == nil || row.TurnsStarted != 1 {
				t.Fatalf("finished worker retained active marker: %+v", row)
			}
		})
	}
}
