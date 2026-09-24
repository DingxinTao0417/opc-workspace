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

func TestDecodeAIAgentChildCancelBatchIsStrictAndBounded(t *testing.T) {
	firstChild, firstGeneration := uuid.NewString(), uuid.NewString()
	secondChild, secondGeneration := uuid.NewString(), uuid.NewString()
	valid := fmt.Sprintf(`{"items":[{"child_session_id":%q,"generation_id":%q},{"child_session_id":%q,"generation_id":%q}]}`, firstChild, firstGeneration, secondChild, secondGeneration)
	items, err := parseAIAgentChildCancelBatch([]byte(valid))
	if err != nil || len(items) != 2 || items[0].ChildSessionID != firstChild || items[1].GenerationID != secondGeneration {
		t.Fatalf("valid batch=%+v err=%v", items, err)
	}
	for name, body := range map[string]string{
		"missing":              `{}`,
		"empty":                `{"items":[]}`,
		"too many":             `{"items":[{"child_session_id":"` + firstChild + `","generation_id":"` + firstGeneration + `"},{"child_session_id":"` + uuid.NewString() + `","generation_id":"` + uuid.NewString() + `"},{"child_session_id":"` + uuid.NewString() + `","generation_id":"` + uuid.NewString() + `"},{"child_session_id":"` + uuid.NewString() + `","generation_id":"` + uuid.NewString() + `"},{"child_session_id":"` + uuid.NewString() + `","generation_id":"` + uuid.NewString() + `"}]}`,
		"unknown root":         fmt.Sprintf(`{"items":[{"child_session_id":%q,"generation_id":%q}],"cascade":true}`, firstChild, firstGeneration),
		"unknown item":         fmt.Sprintf(`{"items":[{"child_session_id":%q,"generation_id":%q,"parent_session_id":%q}]}`, firstChild, firstGeneration, uuid.NewString()),
		"duplicate root key":   fmt.Sprintf(`{"items":[],"items":[{"child_session_id":%q,"generation_id":%q}]}`, firstChild, firstGeneration),
		"duplicate item key":   fmt.Sprintf(`{"items":[{"child_session_id":%q,"child_session_id":%q,"generation_id":%q}]}`, firstChild, secondChild, firstGeneration),
		"duplicate target":     fmt.Sprintf(`{"items":[{"child_session_id":%q,"generation_id":%q},{"child_session_id":%q,"generation_id":%q}]}`, firstChild, firstGeneration, firstChild, secondGeneration),
		"duplicate generation": fmt.Sprintf(`{"items":[{"child_session_id":%q,"generation_id":%q},{"child_session_id":%q,"generation_id":%q}]}`, firstChild, firstGeneration, secondChild, firstGeneration),
		"noncanonical uuid":    `{"items":[{"child_session_id":"` + strings.ToUpper(firstChild) + `","generation_id":"` + firstGeneration + `"}]}`,
		"null":                 `{"items":null}`,
		"trailing":             valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAIAgentChildCancelBatch([]byte(body)); err == nil {
				t.Fatalf("accepted invalid body: %s", body)
			}
		})
	}
	if _, err := parseAIAgentChildCancelBatch(make([]byte, maxAIAgentChildCancelBodyBytes+1)); err == nil {
		t.Fatal("accepted an oversized request")
	}
}

func TestRequestAIAgentChildStopsAtomicallyAcrossGenerationAndCoordinator(t *testing.T) {
	_, a, _, _, _, _ := newAIAgentDelegationFixture(t)
	registry := newAIGenerationRegistry()
	coordinator := a.aiDelegations
	a.aiGenerations = registry

	persistedID, queuedID := uuid.NewString(), uuid.NewString()
	childOne, childTwo := uuid.NewString(), uuid.NewString()
	ctxOne, cancelOne := context.WithCancel(context.Background())
	defer cancelOne()
	if !registry.register(persistedID, uuid.NewString(), childOne, cancelOne) {
		t.Fatal("register persisted generation")
	}
	ctxTwo, cancelTwo := context.WithCancel(context.Background())
	defer cancelTwo()
	coordinator.workers[queuedID] = &aiDelegationWorker{cancel: cancelTwo}
	targets := []aiAgentChildCancelItem{{ChildSessionID: childOne, GenerationID: persistedID}, {ChildSessionID: childTwo, GenerationID: queuedID}}

	coordinator.workers[queuedID].finalizing = true
	if accepted, err := a.requestAIAgentChildStopsAtomically(targets, ""); err != nil || accepted {
		t.Fatal("accepted a batch with a finalizing coordinator worker")
	}
	if ctxOne.Err() != nil || ctxTwo.Err() != nil {
		t.Fatal("failed preflight partially cancelled a worker")
	}

	coordinator.workers[queuedID].finalizing = false
	if accepted, err := a.requestAIAgentChildStopsAtomically(targets, ""); err != nil || !accepted {
		t.Fatal("valid mixed batch was not accepted")
	}
	if ctxOne.Err() == nil || ctxTwo.Err() == nil || !coordinator.workers[queuedID].stopRequested {
		t.Fatal("accepted batch did not signal every exact generation")
	}
	var intents int64
	if err := a.db.Table("workflow_events").Where("aggregate_type='ai_agent_delegation' AND action=?", aiDelegationStopRequestedEvent).Count(&intents).Error; err != nil || intents != 2 {
		t.Fatalf("durable stop intents=%d err=%v", intents, err)
	}
}

func TestAIAgentChildBatchStopRevalidatesWholeFamilyBeforeSignaling(t *testing.T) {
	router, a, provider, parent, generation, client := newAIAgentDelegationFixture(t)
	router.GET("/api/v1/ai/sessions/:id/agent-family", a.getAIAgentFamily)
	router.POST("/api/v1/ai/sessions/:id/agent-children/cancel", a.cancelAIAgentChildGenerations)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	client.stream = func(ctx context.Context) (harness.Turn, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return harness.Turn{}, ctx.Err()
	}
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_delegate_agent")
	if !ok {
		t.Fatal("delegation tool missing")
	}
	propose := func(taskName string) models.AIActionProposal {
		t.Helper()
		result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_name":%q,"message":%q,"scopes":["work"]}`, taskName, "Inspect one bounded workbench slice and report verified facts.")))
		if err != nil {
			t.Fatal(err)
		}
		var output struct {
			ID string `json:"proposal_id"`
		}
		if err := json.Unmarshal([]byte(result), &output); err != nil {
			t.Fatal(err)
		}
		var proposal models.AIActionProposal
		if err := a.db.Take(&proposal, "id=?", output.ID).Error; err != nil {
			t.Fatal(err)
		}
		return proposal
	}
	confirm := func(proposal models.AIActionProposal) {
		t.Helper()
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint)), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
		}
	}
	first, second := propose("child_one"), propose("child_two")
	if err := a.db.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	confirm(first)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first child generation did not enter the fake Provider")
	}
	confirm(second)

	if err := a.db.Take(&first, "id=?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Take(&second, "id=?", second.ID).Error; err != nil {
		t.Fatal(err)
	}
	var firstChild, secondChild string
	if first.ResultID == nil || second.ResultID == nil {
		t.Fatal("confirmed delegation did not return child session identities")
	}
	firstChild, secondChild = *first.ResultID, *second.ResultID
	var family aiAgentFamilyView
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+parent.ID+"/agent-family", nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("family=%d %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data aiAgentFamilyView `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		family = envelope.Data
		if len(family.Children) == 2 && family.Children[0].ActiveGenerationID != nil && family.Children[1].ActiveGenerationID != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(family.Children) != 2 || family.Children[0].ActiveGenerationID == nil || family.Children[1].ActiveGenerationID == nil {
		t.Fatalf("both active/pending children were not visible: %+v", family.Children)
	}
	targets := []aiAgentChildCancelItem{
		{ChildSessionID: firstChild, GenerationID: *family.Children[0].ActiveGenerationID},
		{ChildSessionID: secondChild, GenerationID: *family.Children[1].ActiveGenerationID},
	}
	stale := append([]aiAgentChildCancelItem(nil), targets...)
	stale[1].GenerationID = uuid.NewString()
	body, _ := json.Marshal(map[string]any{"items": stale})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/sessions/"+parent.ID+"/agent-children/cancel", body, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale batch=%d %s", response.Code, response.Body.String())
	}
	select {
	case <-cancelled:
		t.Fatal("valid first child was stopped before stale second target was rejected")
	default:
	}

	body, _ = json.Marshal(map[string]any{"items": targets})
	response = performRequest(router, http.MethodPost, "/api/v1/ai/sessions/"+parent.ID+"/agent-children/cancel", body, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("batch stop=%d %s", response.Code, response.Body.String())
	}
	var accepted struct {
		Data struct {
			ParentSessionID string                        `json:"parent_session_id"`
			Items           []aiAgentChildCancelBatchItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Data.ParentSessionID != parent.ID || len(accepted.Data.Items) != 2 || accepted.Data.Items[0].GenerationID != targets[0].GenerationID || accepted.Data.Items[1].GenerationID != targets[1].GenerationID || !accepted.Data.Items[0].CancelRequested || !accepted.Data.Items[1].CancelRequested {
		t.Fatalf("batch acceptance did not preserve exact order and identities: %+v", accepted.Data)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("Provider context was not cancelled")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := a.db.Model(&models.AIGeneration{}).Where("id IN ? AND session_id IN ? AND status='cancelled'", []string{targets[0].GenerationID, targets[1].GenerationID}, []string{firstChild, secondChild}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("all accepted child stop requests did not reach terminal persistence")
}
