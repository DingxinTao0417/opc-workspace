package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAgentDelegationActionIsStrictAndCanonical(t *testing.T) {
	valid := fmt.Sprintf(`{"action":"agent_delegate.spawn","changes":{"task_name":"research_one","message":"  Inspect the current facts.  ","scopes":["agent_execution","actions","outputs","work"]}}`)
	action, err := parseAIWorkspaceAction([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	var changes aiAgentDelegationChanges
	if err := json.Unmarshal(action.Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if changes.Message != "Inspect the current facts." || strings.Join(changes.Scopes, ",") != "work,outputs,actions,agent_execution" {
		t.Fatalf("normalized=%+v", changes)
	}
	for _, raw := range []string{
		`{"action":"agent_delegate.spawn","changes":{"task_name":"Bad-Name","message":"x","scopes":["work"]}}`,
		`{"action":"agent_delegate.spawn","changes":{"task_name":"child","message":"x","scopes":["workspace_ui"]}}`,
		`{"action":"agent_delegate.spawn","task_id":"` + uuid.NewString() + `","changes":{"task_name":"child","message":"x","scopes":["work"]}}`,
		`{"action":"agent_delegate.spawn","changes":{"task_name":"child","message":"x","scopes":["work","work"]}}`,
	} {
		if _, err := parseAIWorkspaceAction([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid action %s", raw)
		}
	}
}

func newAIAgentDelegationFixture(t *testing.T) (*gin.Engine, *API, models.AIProvider, models.AISession, models.AIGeneration, *generationExecutionClient) {
	t.Helper()
	_, a := newAIRuntimeLockTest(t)
	now := nowStamp(a)
	provider := models.AIProvider{ID: uuid.NewString(), Name: "delegation", Kind: aiProviderKindLocal, Protocol: "openai_chat", BaseURL: "http://127.0.0.1:1", Model: "delegation-test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, ConfigVersion: 1, CreatedAt: now, UpdatedAt: now}
	session := models.AISession{ID: uuid.NewString(), Title: "parent", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&provider, &session, &generation} {
		if err := a.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := &generationExecutionClient{}
	a.harnessClient = client
	a.aiDelegations = newAIDelegationCoordinator(a)
	t.Cleanup(a.aiDelegations.close)
	router := gin.New()
	router.POST("/api/v1/ai/actions/:id/decision", a.decideAIWorkspaceAction)
	router.GET("/api/v1/ai/generations/:id/actions", a.listAIWorkspaceActions)
	return router, a, provider, session, generation, client
}

func TestAIAgentDelegationRequiresApprovalThenRunsSeparateChild(t *testing.T) {
	router, a, provider, parent, generation, client := newAIAgentDelegationFixture(t)
	a.aiDelegations.close()
	a.aiDelegations = newAIDelegationCoordinatorWithLimits(a, 1, 0)
	t.Cleanup(a.aiDelegations.close)
	blocker, code := a.aiDelegations.reserve([]aiAgentDelegationLaunch{{ProposalID: uuid.NewString()}})
	if code != "" {
		t.Fatalf("reserve queue blocker: %q", code)
	}
	wakeup := make(chan struct{}, 1)
	a.aiContinuations = &aiContinuationCoordinator{wakeup: wakeup}
	streamStarted := make(chan struct{})
	releaseStream := make(chan struct{})
	client.stream = func(ctx context.Context) (harness.Turn, error) {
		close(streamStarted)
		select {
		case <-releaseStream:
			return harness.Turn{Text: "Verified child finding"}, nil
		case <-ctx.Done():
			return harness.Turn{}, ctx.Err()
		}
	}
	t.Cleanup(func() {
		select {
		case <-releaseStream:
		default:
			close(releaseStream)
		}
	})
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_delegate_agent")
	if !ok {
		t.Fatal("delegation tool missing from root conversation")
	}
	result, err := tool.Execute(context.Background(), []byte(`{"task_name":"fact_check","message":"Check the current task facts and report only verified findings.","scopes":["work","outputs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var proposed struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(result), &proposed); err != nil {
		t.Fatal(err)
	}
	var sessions, childGenerations int64
	if err := a.db.Model(&models.AISession{}).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", proposed.ID).Count(&childGenerations).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || childGenerations != 0 {
		t.Fatalf("proposal executed before approval: sessions=%d generations=%d", sessions, childGenerations)
	}
	var proposal models.AIActionProposal
	if err := a.db.Take(&proposal, "id=?", proposed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/actions/" + proposal.ID + "/decision"
	withoutConsent := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if withoutConsent.Code != http.StatusUnprocessableEntity || !strings.Contains(withoutConsent.Body.String(), "AI_DELEGATION_CONFIRMATION_REQUIRED") {
		t.Fatalf("without consent=%d %s", withoutConsent.Code, withoutConsent.Body.String())
	}
	full := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint)), nil)
	if full.Code != http.StatusTooManyRequests || !strings.Contains(full.Body.String(), aiDelegationQueueFullCode) {
		t.Fatalf("full queue=%d %s", full.Code, full.Body.String())
	}
	var stillPending models.AIActionProposal
	if err := a.db.Take(&stillPending, "id=?", proposal.ID).Error; err != nil || stillPending.Status != "pending" {
		t.Fatalf("queue saturation consumed proposal: proposal=%+v err=%v", stillPending, err)
	}
	if err := a.db.Model(&models.AISession{}).Count(&sessions).Error; err != nil || sessions != 1 {
		t.Fatalf("queue saturation created a child session: sessions=%d err=%v", sessions, err)
	}
	a.aiDelegations.releaseReservation(blocker)
	response := performRequest(router, http.MethodPost, path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint)), nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"confirmed"`) || !strings.Contains(response.Body.String(), `"agent_delegation_result":{"session_id"`) {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	queued, err := aiAgentDelegationLifecycleEventExists(a.db, proposal.ID, aiDelegationQueuedEvent)
	if err != nil || !queued {
		t.Fatalf("confirmed child has no durable queue event: queued=%v err=%v", queued, err)
	}
	select {
	case <-streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed child did not reach the Provider")
	}
	accepted, err := aiAgentDelegationLifecycleEventExists(a.db, proposal.ID, aiDelegationAcceptedEvent)
	if err != nil || !accepted {
		t.Fatalf("Provider started without a durable accepted event: accepted=%v err=%v", accepted, err)
	}
	select {
	case <-wakeup: // The approval fact has already signalled a scan.
	default:
		t.Fatal("confirmed delegation did not wake continuation")
	}
	close(releaseStream)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var child models.AIGeneration
		err := a.db.Take(&child, "id=?", proposal.ID).Error
		if err == nil && child.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child generation did not complete: %v %+v", err, child)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-wakeup: // Child terminal fact must wake independently of approval.
	case <-time.After(3 * time.Second):
		t.Fatal("child completion did not wake continuation")
	}
	var decided models.AIActionProposal
	if err := a.db.Take(&decided, "id=?", proposal.ID).Error; err != nil || decided.ResultID == nil {
		t.Fatalf("receipt=%+v err=%v", decided, err)
	}
	var childSession models.AISession
	if err := a.db.Take(&childSession, "id=?", *decided.ResultID).Error; err != nil || childSession.Title != "Agent · fact_check" {
		t.Fatalf("child=%+v err=%v", childSession, err)
	}
	if len(client.requests) != 1 || !client.requests[0].DisableTransientRetries {
		t.Fatalf("requests=%d", len(client.requests))
	}
	for _, definition := range client.requests[0].Tools {
		if definition.Name == "workspace_delegate_agent" {
			t.Fatal("delegated child retained recursive spawn tool")
		}
	}
	if !strings.Contains(response.Body.String(), "/ai?session="+childSession.ID) {
		t.Fatalf("missing child route: %s", response.Body.String())
	}
}

func TestAIAgentDelegationPendingChildRejectsManualChatAndRecoversWithoutReplay(t *testing.T) {
	_, a, provider, parent, generation, _ := newAIAgentDelegationFixture(t)
	now := nowStamp(a)
	child := models.AISession{ID: uuid.NewString(), Title: "Agent · check", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := a.db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	action, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.spawn","changes":{"task_name":"check","message":"Check facts","scopes":["work"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	actionJSON, _ := json.Marshal(action)
	previewJSON, _ := json.Marshal(aiActionPreview{AgentDelegation: &aiAgentDelegationPreview{
		TaskName: "check", Message: "Check facts", Scopes: []string{"work"},
		ParentSessionID: parent.ID, ParentGenerationID: generation.ID, ChildSessionID: child.ID,
		ProviderID: provider.ID, ProviderVersion: provider.Version, ConfigVersion: provider.ConfigVersion,
		ProviderName: provider.Name, Model: provider.Model, LeavesDevice: false,
	}})
	version := int64(1)
	proposal := models.AIActionProposal{ID: uuid.NewString(), GenerationID: generation.ID, Fingerprint: strings.Repeat("a", 64), ActionJSON: string(actionJSON), PreviewJSON: string(previewJSON), Status: "confirmed", ResultID: &child.ID, ResultVersion: &version, CreatedAt: now, DecidedAt: &now}
	if err := a.db.Create(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	result, err := a.runAIGeneration(context.Background(), chatAIRequest{ProviderID: provider.ID, SessionID: child.ID, Message: "manual"}, aiGenerationRunOptions{}, nil)
	var runErr *aiGenerationRunError
	if result.Accepted || !errors.As(err, &runErr) || runErr.Code != "AI_DELEGATION_PENDING" {
		t.Fatalf("manual chat occupied pending child: result=%+v err=%v", result, err)
	}
	var count int64
	if err := a.db.Model(&models.AIMessage{}).Where("session_id=?", child.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("pending child messages=%d err=%v", count, err)
	}
	delegatedOptions := aiGenerationRunOptions{DelegationProposalID: proposal.ID, GenerationID: proposal.ID}
	if err := a.guardAIDelegationGeneration(a.db, child.ID, delegatedOptions); err != nil {
		t.Fatalf("unchanged provider rejected: %v", err)
	}
	if err := a.db.Model(&provider).Updates(map[string]any{"config_version": provider.ConfigVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.guardAIDelegationGeneration(a.db, child.ID, delegatedOptions); !errors.As(err, &runErr) || runErr.Code != "AI_DELEGATION_PROVIDER_CHANGED" {
		t.Fatalf("changed provider was accepted: %v", err)
	}
	if pending, err := recoverAIDelegationsOnStartup(a.db, a.options.Now()); err != nil || len(pending) != 0 {
		t.Fatalf("recovery pending=%+v err=%v", pending, err)
	}
	var recovered models.AIGeneration
	if err := a.db.Take(&recovered, "id=?", proposal.ID).Error; err != nil || recovered.Status != "failed" || recovered.ErrorCode == nil || *recovered.ErrorCode != "AI_DELEGATION_INTERRUPTED" {
		t.Fatalf("recovery=%+v err=%v", recovered, err)
	}
	if err := a.guardAIDelegationGeneration(a.db, child.ID, aiGenerationRunOptions{}); err != nil {
		t.Fatalf("terminal child should allow later manual chat: %v", err)
	}
}

func TestAIAgentDelegationStartupRecoveryQueuesOnlyDurableUnstoppedLaunches(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		stopRequested bool
		accepted      bool
	}{
		{name: "queued"},
		{name: "stop intent wins", stopRequested: true},
		{name: "accepted marker prevents replay", accepted: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, a, provider, parent, generation, _ := newAIAgentDelegationFixture(t)
			now := nowStamp(a)
			child := models.AISession{ID: uuid.NewString(), Title: "Agent · check", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := a.db.Create(&child).Error; err != nil {
				t.Fatal(err)
			}
			action, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.spawn","changes":{"task_name":"check","message":"Check facts","scopes":["work"]}}`))
			if err != nil {
				t.Fatal(err)
			}
			actionJSON, _ := json.Marshal(action)
			previewJSON, _ := json.Marshal(aiActionPreview{AgentDelegation: &aiAgentDelegationPreview{
				TaskName: "check", Message: "Check facts", Scopes: []string{"work"},
				ParentSessionID: parent.ID, ParentGenerationID: generation.ID, ChildSessionID: child.ID,
				ProviderID: provider.ID, ProviderVersion: provider.Version, ConfigVersion: provider.ConfigVersion,
				ProviderName: provider.Name, Model: provider.Model, LeavesDevice: false,
			}})
			version := int64(1)
			proposal := models.AIActionProposal{ID: uuid.NewString(), GenerationID: generation.ID, Fingerprint: strings.Repeat("b", 64), ActionJSON: string(actionJSON), PreviewJSON: string(previewJSON), Status: "confirmed", ResultID: &child.ID, ResultVersion: &version, CreatedAt: now, DecidedAt: &now}
			if err := a.db.Create(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			if err := recordAIAgentDelegationLifecycleEvent(a.db, proposal.ID, child.ID, aiDelegationQueuedEvent, "", now); err != nil {
				t.Fatal(err)
			}
			if testCase.stopRequested {
				if err := recordAIAgentDelegationLifecycleEvent(a.db, proposal.ID, child.ID, aiDelegationStopRequestedEvent, "", now); err != nil {
					t.Fatal(err)
				}
			}
			if testCase.accepted {
				if err := recordAIAgentDelegationLifecycleEvent(a.db, proposal.ID, child.ID, aiDelegationAcceptedEvent, "", now); err != nil {
					t.Fatal(err)
				}
			}

			pending, err := recoverAIDelegationsOnStartup(a.db, a.options.Now())
			if err != nil {
				t.Fatal(err)
			}
			if testCase.stopRequested {
				if len(pending) != 0 {
					t.Fatalf("stop-requested delegation was requeued: %+v", pending)
				}
				var recovered models.AIGeneration
				if err := a.db.Take(&recovered, "id=?", proposal.ID).Error; err != nil || recovered.Status != "cancelled" {
					t.Fatalf("stop recovery=%+v err=%v", recovered, err)
				}
			} else if testCase.accepted {
				if len(pending) != 0 {
					t.Fatalf("accepted delegation was requeued: %+v", pending)
				}
				var recovered models.AIGeneration
				if err := a.db.Take(&recovered, "id=?", proposal.ID).Error; err != nil || recovered.Status != "failed" || recovered.ErrorCode == nil || *recovered.ErrorCode != "AI_DELEGATION_INTERRUPTED" {
					t.Fatalf("accepted-marker recovery=%+v err=%v", recovered, err)
				}
			} else if len(pending) != 1 || pending[0].ProposalID != proposal.ID || pending[0].SessionID != child.ID || pending[0].Message != "Check facts" {
				t.Fatalf("durably queued delegation not restored exactly: %+v", pending)
			} else {
				var count int64
				if err := a.db.Model(&models.AIGeneration{}).Where("id=?", proposal.ID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("recovery created generation before scheduling: count=%d err=%v", count, err)
				}
			}
		})
	}
}

func TestLaunchRecoveredAIDelegationsWaitsForRestoreGateAndUsesCoordinator(t *testing.T) {
	coordinator := newAIDelegationCoordinatorWithLimits(nil, 1, 1)
	defer coordinator.close()
	started := make(chan aiAgentDelegationLaunch, 1)
	coordinator.runWorker = func(_ context.Context, spec aiAgentDelegationLaunch) {
		started <- spec
	}
	a := &API{aiDelegations: coordinator}
	spec := aiAgentDelegationLaunch{ProposalID: uuid.NewString(), SessionID: uuid.NewString(), Message: "Frozen child request"}

	a.restorePending.Store(true)
	if code := launchRecoveredAIDelegations(a, []aiAgentDelegationLaunch{spec}); code != "" {
		t.Fatalf("restore-pending launch code=%q", code)
	}
	select {
	case <-started:
		t.Fatal("recovery launched while a verified restore was pending")
	default:
	}

	a.restorePending.Store(false)
	if code := launchRecoveredAIDelegations(a, []aiAgentDelegationLaunch{spec}); code != "" {
		t.Fatalf("recovered launch code=%q", code)
	}
	select {
	case actual := <-started:
		if actual.ProposalID != spec.ProposalID || actual.SessionID != spec.SessionID || actual.Message != spec.Message {
			t.Fatalf("recovered launch changed frozen identity: %+v", actual)
		}
	case <-time.After(time.Second):
		t.Fatal("recovered launch was not dispatched by the coordinator")
	}
}

func TestAIAgentDelegationFailureReceiptRejectsChangedOrOccupiedChild(t *testing.T) {
	_, a, provider, _, generation, _ := newAIAgentDelegationFixture(t)
	now := nowStamp(a)
	child := models.AISession{ID: uuid.NewString(), Title: "Agent · check", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := a.db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}
	action := `{"action":"agent_delegate.spawn","changes":{"task_name":"check","message":"Check facts","scopes":["work"]}}`
	previewJSON, _ := json.Marshal(aiActionPreview{AgentDelegation: &aiAgentDelegationPreview{Message: "Check facts", Scopes: []string{"work"}, ParentGenerationID: generation.ID, ChildSessionID: child.ID, ProviderID: provider.ID, ProviderVersion: provider.Version}})
	version := int64(1)
	proposal := models.AIActionProposal{ID: uuid.NewString(), GenerationID: generation.ID, Fingerprint: strings.Repeat("a", 64), ActionJSON: action, PreviewJSON: string(previewJSON), Status: "confirmed", ResultID: &child.ID, ResultVersion: &version, CreatedAt: now, DecidedAt: &now}
	if err := a.db.Create(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	spec := aiAgentDelegationLaunch{ProposalID: proposal.ID, SessionID: child.ID, ProviderID: provider.ID, Message: "Changed", Grant: aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}}
	if err := recordAIDelegationPreAcceptFailure(a.db, spec, "AI_DELEGATION_INTERRUPTED", a.options.Now()); err == nil {
		t.Fatal("changed delegated message produced a false failure receipt")
	}
	spec.Message = "Check facts"
	if err := a.db.Create(&models.AIMessage{ID: uuid.NewString(), SessionID: child.ID, Role: "user", Status: "completed", Content: "other", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := recordAIDelegationPreAcceptFailure(a.db, spec, "AI_DELEGATION_INTERRUPTED", a.options.Now()); err == nil {
		t.Fatal("occupied delegated child produced a false failure receipt")
	}
	var count int64
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", proposal.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("false failure generation=%d err=%v", count, err)
	}
}

func TestAIAgentDelegationAdmissionIsBoundedAndAtomic(t *testing.T) {
	r := newAIDelegationCoordinatorWithLimits(nil, 2, 2)
	defer r.close()
	specs := make([]aiAgentDelegationLaunch, 5)
	for i := range specs {
		specs[i].ProposalID = uuid.NewString()
	}

	first, code := r.reserve(specs[:3])
	if code != "" || first == nil {
		t.Fatalf("reserve first group: reservation=%v code=%q", first, code)
	}
	if _, code := r.reserve(specs[3:]); code != aiDelegationQueueFullCode {
		t.Fatalf("over-capacity batch code=%q", code)
	}
	r.mu.Lock()
	reserved := len(r.reservations)
	r.mu.Unlock()
	if reserved != 3 {
		t.Fatalf("failed batch partially reserved capacity: %d", reserved)
	}
	if _, code := r.reserve(specs[:1]); code != aiDelegationQueueUnavailableCode {
		t.Fatalf("duplicate reservation code=%q", code)
	}
	r.releaseReservation(first)
	second, code := r.reserve(specs[3:])
	if code != "" || second == nil {
		t.Fatalf("capacity was not released: reservation=%v code=%q", second, code)
	}
	r.releaseReservation(second)
}

func TestAIAgentDelegationSchedulerBoundsActiveProviderCalls(t *testing.T) {
	r := newAIDelegationCoordinatorWithLimits(nil, 2, 1)
	defer r.close()
	started := make(chan string, 3)
	release := make(chan struct{}, 3)
	var active int32
	var maximum int32
	r.runWorker = func(ctx context.Context, spec aiAgentDelegationLaunch) {
		r.withConcurrencySlot(ctx, func() {
			current := atomic.AddInt32(&active, 1)
			for {
				previous := atomic.LoadInt32(&maximum)
				if current <= previous || atomic.CompareAndSwapInt32(&maximum, previous, current) {
					break
				}
			}
			started <- spec.ProposalID
			select {
			case <-release:
			case <-ctx.Done():
			}
			atomic.AddInt32(&active, -1)
		})
	}
	specs := []aiAgentDelegationLaunch{{ProposalID: uuid.NewString()}, {ProposalID: uuid.NewString()}, {ProposalID: uuid.NewString()}}
	reservation, code := r.reserve(specs)
	if code != "" {
		t.Fatalf("reserve workers: %q", code)
	}
	if code := r.launchReservedBatch(reservation, specs); code != "" {
		t.Fatalf("launch reserved workers: %q", code)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("active worker did not start")
		}
	}
	select {
	case id := <-started:
		t.Fatalf("queued worker %s started above the concurrency limit", id)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("queued worker did not start after capacity became available")
	}
	release <- struct{}{}
	release <- struct{}{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		remaining := len(r.workers)
		r.mu.Unlock()
		if remaining == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	remaining := len(r.workers)
	r.mu.Unlock()
	if remaining != 0 || atomic.LoadInt32(&maximum) != 2 {
		t.Fatalf("workers=%d maximum_active=%d", remaining, atomic.LoadInt32(&maximum))
	}
}
