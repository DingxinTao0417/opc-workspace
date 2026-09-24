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

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAgentFollowupCoordinatorStartsOnceInExistingChild(t *testing.T) {
	router, a, provider, _, parentGeneration, client := newAIAgentDelegationFixture(t)
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	spawnAction, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.spawn","changes":{"task_name":"check","message":"First check","scopes":["work","outputs"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	spawnPreview, err := previewAIAgentDelegation(a.db, spawnAction, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, "", false)
	if err != nil {
		t.Fatal(err)
	}
	childID := spawnPreview.AgentDelegation.ChildSessionID
	spawnID := uuid.NewString()
	stamp := nowStamp(a)
	version := int64(1)
	encodedSpawnAction, _ := json.Marshal(spawnAction)
	encodedSpawnPreview, _ := json.Marshal(spawnPreview)
	spawn := models.AIActionProposal{ID: spawnID, GenerationID: parentGeneration.ID, Fingerprint: sha256Hex(encodedSpawnAction), ActionJSON: string(encodedSpawnAction), PreviewJSON: string(encodedSpawnPreview), Status: "confirmed", ResultID: &childID, ResultVersion: &version, CreatedAt: stamp, DecidedAt: &stamp}
	child := models.AISession{ID: childID, Title: "Agent · check", Persist: true, Version: 2, CreatedAt: stamp, UpdatedAt: stamp}
	first := models.AIGeneration{ID: spawnID, SessionID: childID, ProviderID: provider.ID, Status: "completed", CreatedAt: stamp, UpdatedAt: stamp}
	for _, row := range []any{&spawn, &child, &first} {
		if err := a.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := a.aiChatToolRegistry(spawnPreview.AgentDelegation.ParentSessionID, true, &provider, grant, parentGeneration.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, exists := registry.Get("workspace_agent_followup")
	if !exists {
		t.Fatal("follow-up tool missing from root conversation")
	}
	proposed, err := tool.Execute(context.Background(), []byte(`{"child_session_id":"`+childID+`","message":"Verify again","scopes":["work","outputs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(proposed), &receipt); err != nil {
		t.Fatal(err)
	}
	var proposal models.AIActionProposal
	if err := a.db.Take(&proposal, "id=?", receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	var followupPreview aiActionPreview
	if err := json.Unmarshal([]byte(proposal.PreviewJSON), &followupPreview); err != nil || followupPreview.AgentFollowup == nil {
		t.Fatalf("invalid saved follow-up preview: %v", err)
	}
	if err := a.db.Model(&parentGeneration).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	followupID := proposal.ID
	launch := aiAgentDelegationLaunch{ProposalID: followupID, SessionID: childID, ProviderID: provider.ID, Message: "Verify again", Grant: aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs"}}, Followup: &followupPreview.AgentFollowup.Target}
	missingConsent := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+followupID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if missingConsent.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing follow-up consent=%d %s", missingConsent.Code, missingConsent.Body.String())
	}
	var before int64
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", followupID).Count(&before).Error; err != nil || before != 0 {
		t.Fatalf("follow-up started without consent: count=%d err=%v", before, err)
	}
	var calls atomic.Int32
	client.stream = func(context.Context) (harness.Turn, error) {
		calls.Add(1)
		return harness.Turn{Text: "Second child reply"}, nil
	}
	decisionBody := []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint))
	decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+followupID+"/decision", decisionBody, nil)
	if decision.Code != http.StatusOK || !strings.Contains(decision.Body.String(), `"agent_followup_result"`) {
		t.Fatalf("follow-up confirmation=%d %s", decision.Code, decision.Body.String())
	}
	var created models.AIGeneration
	deadline := time.Now().Add(3 * time.Second)
	var readErr error
	for {
		readErr = a.db.Take(&created, "id=?", followupID).Error
		if readErr == nil && (created.Status == "completed" || created.Status == "failed" || created.Status == "cancelled") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("follow-up did not reach terminal state: %+v err=%v", created, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if created.SessionID != childID || created.Status != "completed" || calls.Load() != 1 {
		code := ""
		if created.ErrorCode != nil {
			code = *created.ErrorCode
		}
		t.Fatalf("follow-up generation=%+v code=%s calls=%d err=%v", created, code, calls.Load(), readErr)
	}
	replayed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+followupID+"/decision", decisionBody, nil)
	if replayed.Code != http.StatusOK {
		t.Fatalf("repeat follow-up decision=%d %s", replayed.Code, replayed.Body.String())
	}
	a.aiDelegations.run(context.Background(), launch)
	if calls.Load() != 1 {
		t.Fatalf("accepted follow-up request was replayed: calls=%d", calls.Load())
	}
}

func TestAIAgentFollowupIsDeferredUntilTheRootHasAConfirmedChild(t *testing.T) {
	_, a, provider, parent, generation, _ := newAIAgentDelegationFixture(t)
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Get("workspace_agent_followup"); exists {
		t.Fatal("follow-up tool exposed before the root has a confirmed child")
	}
	propose, exists := registry.Get("workspace_propose")
	if !exists {
		t.Fatal("expected ordinary proposal tool")
	}
	_, err = propose.Execute(context.Background(), []byte(`{"action":"agent_delegate.followup","changes":{"child_session_id":"`+uuid.NewString()+`","message":"Continue","scopes":["work"]}}`))
	if err == nil {
		t.Fatal("ordinary proposal tool accepted an unreviewable follow-up")
	}
}

func TestAIAgentFollowupActionIsStrictAndCanonical(t *testing.T) {
	childID := uuid.NewString()
	valid := `{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"  Check again.  ","scopes":["outputs","work"]}}`
	action, err := parseAIWorkspaceAction([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	var changes aiAgentFollowupChanges
	if err := json.Unmarshal(action.Changes, &changes); err != nil {
		t.Fatal(err)
	}
	if changes.ChildSessionID != childID || changes.Message != "Check again." || strings.Join(changes.Scopes, ",") != "work,outputs" {
		t.Fatalf("normalized follow-up=%+v", changes)
	}
	for _, raw := range []string{
		`{"action":"agent_delegate.followup","changes":{"child_session_id":"not-a-uuid","message":"Check","scopes":["work"]}}`,
		`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"Check","scopes":["work","work"]}}`,
		`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"Check","scopes":["workspace_browser"]}}`,
		`{"action":"agent_delegate.followup","task_id":"` + uuid.NewString() + `","changes":{"child_session_id":"` + childID + `","message":"Check","scopes":["work"]}}`,
		`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"Check","scopes":["work"],"extra":true}}`,
		`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"","scopes":["work"]}}`,
	} {
		if _, err := parseAIWorkspaceAction([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid follow-up %s", raw)
		}
	}
}

func TestAIAgentFollowupReceiptDoesNotLinkMissingChild(t *testing.T) {
	_, a, _, _, _, _ := newAIAgentDelegationFixture(t)
	childID := uuid.NewString()
	version := int64(1)
	result, err := readAIAgentFollowupResult(a.db, aiActionResponse{ID: uuid.NewString(), ResultID: &childID, ResultVersion: &version,
		Preview: aiActionPreview{AgentFollowup: &aiAgentFollowupPreview{Target: aiAgentFollowupTarget{ChildSessionID: childID}}}}, a.options.Now())
	if err != nil || result == nil || result.Status != "unavailable" || result.ErrorCode == nil || result.PendingApprovals != 0 {
		t.Fatalf("missing child follow-up receipt=%+v err=%v", result, err)
	}
}

func TestAIAgentFollowupTargetRequiresLiveChildFacts(t *testing.T) {
	_, a, provider, parent, parentGeneration, _ := newAIAgentDelegationFixture(t)
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	action, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.spawn","changes":{"task_name":"check","message":"Check facts","scopes":["work","outputs"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewAIAgentDelegation(a.db, action, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, "", false)
	if err != nil {
		t.Fatal(err)
	}
	childID := preview.AgentDelegation.ChildSessionID
	stamp := nowStamp(a)
	proposalID := uuid.NewString()
	encodedAction, _ := json.Marshal(action)
	encodedPreview, _ := json.Marshal(preview)
	version := int64(1)
	proposal := models.AIActionProposal{ID: proposalID, GenerationID: parentGeneration.ID, Fingerprint: sha256Hex(encodedAction),
		ActionJSON: string(encodedAction), PreviewJSON: string(encodedPreview), Status: "confirmed",
		ResultID: &childID, ResultVersion: &version, CreatedAt: stamp, DecidedAt: &stamp}
	child := models.AISession{ID: childID, Title: "Agent · check", Persist: true, Version: 2, CreatedAt: stamp, UpdatedAt: stamp}
	first := models.AIGeneration{ID: proposalID, SessionID: childID, ProviderID: provider.ID, Status: "completed", CreatedAt: stamp, UpdatedAt: stamp}
	for _, row := range []any{&proposal, &child, &first} {
		if err := a.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	inspect := func() (aiAgentFollowupTarget, error) {
		return inspectAIAgentFollowupTarget(a.db, parent.ID, childID, provider.ID, a.options.Now())
	}
	target, err := inspect()
	if err != nil || target.SpawnProposalID != proposalID || target.PreviousGeneration != first.ID || target.ChildVersion != 2 || target.ParentSessionID != parent.ID {
		t.Fatalf("initial target=%+v err=%v", target, err)
	}
	followup, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"  Recheck the current facts.  ","scopes":["outputs","work"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	followupPreview, err := previewAIAgentFollowup(a.db, followup, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, nil, a.options.Now())
	if err != nil || followupPreview.AgentFollowup == nil || followupPreview.AgentFollowup.Target != target || followupPreview.AgentFollowup.Message != "Recheck the current facts." || followupPreview.AgentFollowup.TaskName != "check" {
		t.Fatalf("follow-up preview=%+v err=%v", followupPreview, err)
	}
	broader, err := parseAIWorkspaceAction([]byte(`{"action":"agent_delegate.followup","changes":{"child_session_id":"` + childID + `","message":"Expand silently","scopes":["actions"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := previewAIAgentFollowup(a.db, broader, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, nil, a.options.Now()); err == nil {
		t.Fatal("follow-up broadened the original child scope")
	}
	if err := a.db.Model(&parentGeneration).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := previewAIAgentFollowup(a.db, followup, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, followupPreview.AgentFollowup, a.options.Now()); err != nil {
		t.Fatalf("unchanged follow-up was not confirmable: %v", err)
	}
	followupID := uuid.NewString()
	encodedFollowup, _ := json.Marshal(followup)
	encodedFollowupPreview, _ := json.Marshal(followupPreview)
	followupProposal := models.AIActionProposal{ID: followupID, GenerationID: parentGeneration.ID, Fingerprint: sha256Hex(encodedFollowup),
		ActionJSON: string(encodedFollowup), PreviewJSON: string(encodedFollowupPreview), Status: "confirmed",
		ResultID: &childID, ResultVersion: &version, CreatedAt: stamp, DecidedAt: &stamp}
	if err := a.db.Create(&followupProposal).Error; err != nil {
		t.Fatal(err)
	}
	followupOptions := aiGenerationRunOptions{GenerationID: followupID, DelegationFollowupID: followupID, Headless: true}
	if err := a.guardAIDelegationGeneration(a.db, childID, followupOptions); err != nil {
		t.Fatalf("valid reviewed follow-up failed execution guard: %v", err)
	}
	launch := aiAgentDelegationLaunch{ProposalID: followupID, SessionID: childID, ProviderID: provider.ID, Message: "Recheck the current facts.", Grant: aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs"}}, Followup: &followupPreview.AgentFollowup.Target}
	if err := verifyAIAgentFollowupLaunch(a.db, launch, a.options.Now()); err != nil {
		t.Fatalf("approved follow-up launch was rejected: %v", err)
	}
	altered := launch
	altered.Message = "Different instruction"
	if err := verifyAIAgentFollowupLaunch(a.db, altered, a.options.Now()); err == nil {
		t.Fatal("changed follow-up instruction was accepted")
	}
	if err := a.guardAIDelegationGeneration(a.db, uuid.NewString(), followupOptions); err == nil {
		t.Fatal("follow-up guard accepted a different child session")
	}
	if err := a.guardAIDelegationGeneration(a.db, childID, aiGenerationRunOptions{GenerationID: uuid.NewString(), DelegationFollowupID: followupID, Headless: true}); err == nil {
		t.Fatal("follow-up guard accepted a different generation identity")
	}
	if _, err := inspectAIAgentFollowupTarget(a.db, uuid.NewString(), childID, provider.ID, a.options.Now()); err == nil {
		t.Fatal("unrelated parent could follow up the child")
	}
	if _, err := inspectAIAgentFollowupTarget(a.db, childID, childID, provider.ID, a.options.Now()); err == nil {
		t.Fatal("delegated child could follow up another child")
	}
	pending := models.AIActionProposal{ID: uuid.NewString(), GenerationID: first.ID, Fingerprint: sha256Hex([]byte("pending")),
		ActionJSON: `{"action":"task.create","changes":{"title":"Check task"}}`, PreviewJSON: `{"label":"Check task","before":{},"after":{}}`,
		Status: "pending", CreatedAt: stamp}
	if err := a.db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(); err == nil {
		t.Fatal("pending child approval did not block follow-up")
	}
	if err := a.guardAIDelegationGeneration(a.db, childID, followupOptions); err == nil {
		t.Fatal("follow-up guard ignored an approval added after consent")
	}
	if err := a.db.Model(&pending).Updates(map[string]any{"status": "rejected", "decided_at": stamp}).Error; err != nil {
		t.Fatal(err)
	}
	second := models.AIGeneration{ID: uuid.NewString(), SessionID: childID, ProviderID: provider.ID, Status: "streaming", CreatedAt: stamp, UpdatedAt: stamp}
	if err := a.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(); err == nil {
		t.Fatal("running later child generation did not block follow-up")
	}
	if err := a.db.Model(&second).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	if err := recheckAIAgentFollowupTarget(a.db, target, a.options.Now()); err == nil {
		t.Fatal("a newly completed child turn did not invalidate the reviewed target")
	}
	if _, err := previewAIAgentFollowup(a.db, followup, parentGeneration.ID, provider.ID, provider.ConfigVersion, grant, followupPreview.AgentFollowup, a.options.Now()); err == nil {
		t.Fatal("newer child turn did not invalidate the consent preview")
	}
	target, err = inspect()
	if err != nil || target.PreviousGeneration != second.ID {
		t.Fatalf("latest completed target=%+v err=%v", target, err)
	}
	if err := recheckAIAgentFollowupTarget(a.db, target, a.options.Now()); err != nil {
		t.Fatalf("unchanged target failed recheck: %v", err)
	}
	if err := a.db.Model(&child).Update("version", 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := recheckAIAgentFollowupTarget(a.db, target, a.options.Now()); err == nil {
		t.Fatal("child conversation version change did not invalidate the reviewed target")
	}
	if err := a.db.Model(&provider).Updates(map[string]any{"status": "disabled", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(); err == nil {
		t.Fatal("provider configuration drift did not block follow-up")
	}
	if pending, err := recoverAIDelegationsOnStartup(a.db, a.options.Now()); err != nil || len(pending) != 0 {
		t.Fatalf("restart recovery pending=%+v err=%v", pending, err)
	}
	var interrupted models.AIGeneration
	if err := a.db.Take(&interrupted, "id=?", followupID).Error; err != nil || interrupted.Status != "failed" || interrupted.ErrorCode == nil || *interrupted.ErrorCode != "AI_DELEGATION_INTERRUPTED" {
		t.Fatalf("interrupted follow-up=%+v err=%v", interrupted, err)
	}
	if pending, err := recoverAIDelegationsOnStartup(a.db, a.options.Now()); err != nil || len(pending) != 0 {
		t.Fatalf("repeat restart recovery pending=%+v err=%v", pending, err)
	}
	var generations int64
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", followupID).Count(&generations).Error; err != nil || generations != 1 {
		t.Fatalf("interrupted follow-up was duplicated: count=%d err=%v", generations, err)
	}
}
