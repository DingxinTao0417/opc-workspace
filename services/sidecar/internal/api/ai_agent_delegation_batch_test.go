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

func TestParseAIAgentDelegationConfirmBatchIsStrictAndBounded(t *testing.T) {
	firstID, secondID := uuid.NewString(), uuid.NewString()
	valid := fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q}]}`,
		firstID, strings.Repeat("a", 64), secondID, strings.Repeat("b", 64))
	items, err := parseAIAgentDelegationConfirmBatch([]byte(valid))
	if err != nil || len(items) != 2 || items[0].ProposalID != firstID || items[1].Fingerprint != strings.Repeat("b", 64) {
		t.Fatalf("valid batch=%+v err=%v", items, err)
	}
	for name, body := range map[string]string{
		"empty":                 `{}`,
		"one item":              fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q}]}`, firstID, strings.Repeat("a", 64)),
		"unknown root":          strings.TrimSuffix(valid, "}") + `,"cascade":true}`,
		"false consent":         strings.Replace(valid, `"confirm_agent_delegations":true`, `"confirm_agent_delegations":false`, 1),
		"duplicate root key":    strings.Replace(valid, `,"items"`, `,"confirm_agent_delegations":true,"items"`, 1),
		"unknown item":          fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q,"generation_id":%q},{"proposal_id":%q,"fingerprint":%q}]}`, firstID, strings.Repeat("a", 64), uuid.NewString(), secondID, strings.Repeat("b", 64)),
		"duplicate item key":    fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q}]}`, firstID, secondID, strings.Repeat("a", 64), uuid.NewString(), strings.Repeat("b", 64)),
		"duplicate proposal":    fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q}]}`, firstID, strings.Repeat("a", 64), firstID, strings.Repeat("b", 64)),
		"duplicate fingerprint": fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q}]}`, firstID, strings.Repeat("a", 64), secondID, strings.Repeat("a", 64)),
		"uppercase uuid":        strings.Replace(valid, firstID, strings.ToUpper(firstID), 1),
		"uppercase hash":        strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		"null":                  `{"confirm_agent_delegations":true,"items":null}`,
		"too many": fmt.Sprintf(`{"confirm_agent_delegations":true,"items":[{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q},{"proposal_id":%q,"fingerprint":%q}]}`,
			firstID, strings.Repeat("a", 64), secondID, strings.Repeat("b", 64), uuid.NewString(), strings.Repeat("c", 64), uuid.NewString(), strings.Repeat("d", 64), uuid.NewString(), strings.Repeat("e", 64)),
		"trailing": valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAIAgentDelegationConfirmBatch([]byte(body)); err == nil {
				t.Fatalf("accepted invalid batch: %s", body)
			}
		})
	}
	if _, err := parseAIAgentDelegationConfirmBatch(make([]byte, maxAIAgentDelegationConfirmBodyBytes+1)); err == nil {
		t.Fatal("accepted an oversized batch")
	}
}

func TestAIAgentDelegationBatchConfirmationIsAtomicOrderedAndIdempotent(t *testing.T) {
	router, a, provider, parent, generation, client := newAIAgentDelegationFixture(t)
	a.aiDelegations.close()
	a.aiDelegations = newAIDelegationCoordinatorWithLimits(a, 2, 0)
	t.Cleanup(a.aiDelegations.close)
	router.POST("/api/v1/ai/generations/:id/agent-delegations/confirm", a.confirmAIAgentDelegationBatch)
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_delegate_agent")
	if !ok {
		t.Fatal("delegation tool missing")
	}
	propose := func(taskName, message string) models.AIActionProposal {
		t.Helper()
		result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"task_name":%q,"message":%q,"scopes":["work"]}`, taskName, message)))
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ProposalID string `json:"proposal_id"`
		}
		if err := json.Unmarshal([]byte(result), &response); err != nil {
			t.Fatal(err)
		}
		var proposal models.AIActionProposal
		if err := a.db.Take(&proposal, "id=?", response.ProposalID).Error; err != nil {
			t.Fatal(err)
		}
		return proposal
	}
	first := propose("first_child", "Inspect the first workstream and return verified findings.")
	second := propose("second_child", "Inspect the second workstream and return verified findings.")
	if err := a.db.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	var sessionCount int64
	if err := a.db.Model(&models.AISession{}).Count(&sessionCount).Error; err != nil || sessionCount != 1 {
		t.Fatalf("children existed before confirmation: count=%d err=%v", sessionCount, err)
	}
	started := make(chan struct{}, 2)
	client.stream = func(context.Context) (harness.Turn, error) {
		started <- struct{}{}
		return harness.Turn{Text: "Verified findings."}, nil
	}
	path := "/api/v1/ai/generations/" + generation.ID + "/agent-delegations/confirm"
	encode := func(firstFingerprint, secondFingerprint string) []byte {
		body, err := json.Marshal(map[string]any{
			"confirm_agent_delegations": true,
			"items": []map[string]string{
				{"proposal_id": first.ID, "fingerprint": firstFingerprint},
				{"proposal_id": second.ID, "fingerprint": secondFingerprint},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	stale := performRequest(router, http.MethodPost, path, encode(first.Fingerprint, strings.Repeat("f", 64)), nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale batch=%d %s", stale.Code, stale.Body.String())
	}
	if err := a.db.Model(&models.AISession{}).Count(&sessionCount).Error; err != nil || sessionCount != 1 {
		t.Fatalf("stale batch created a child session: count=%d err=%v", sessionCount, err)
	}
	for _, proposal := range []models.AIActionProposal{first, second} {
		var current models.AIActionProposal
		if err := a.db.Take(&current, "id=?", proposal.ID).Error; err != nil || current.Status != "pending" {
			t.Fatalf("stale batch partially decided proposal=%+v err=%v", current, err)
		}
	}
	otherGeneration := models.AIGeneration{
		ID: uuid.NewString(), SessionID: parent.ID, ProviderID: provider.ID,
		Status: "completed", CreatedAt: generation.CreatedAt, UpdatedAt: generation.UpdatedAt,
	}
	if err := a.db.Create(&otherGeneration).Error; err != nil {
		t.Fatal(err)
	}
	foreignGeneration := performRequest(router, http.MethodPost,
		"/api/v1/ai/generations/"+otherGeneration.ID+"/agent-delegations/confirm",
		encode(first.Fingerprint, second.Fingerprint), nil)
	if foreignGeneration.Code != http.StatusConflict {
		t.Fatalf("foreign-generation batch=%d %s", foreignGeneration.Code, foreignGeneration.Body.String())
	}
	if err := a.db.Model(&models.AISession{}).Count(&sessionCount).Error; err != nil || sessionCount != 1 {
		t.Fatalf("foreign-generation batch created a child session: count=%d err=%v", sessionCount, err)
	}

	body := encode(first.Fingerprint, second.Fingerprint)
	blocker, code := a.aiDelegations.reserve([]aiAgentDelegationLaunch{{ProposalID: uuid.NewString()}})
	if code != "" {
		t.Fatalf("reserve queue blocker: %q", code)
	}
	full := performRequest(router, http.MethodPost, path, body, nil)
	if full.Code != http.StatusTooManyRequests || !strings.Contains(full.Body.String(), aiDelegationQueueFullCode) {
		t.Fatalf("full queue batch=%d %s", full.Code, full.Body.String())
	}
	if err := a.db.Model(&models.AISession{}).Count(&sessionCount).Error; err != nil || sessionCount != 1 {
		t.Fatalf("queue-full batch created child sessions: count=%d err=%v", sessionCount, err)
	}
	for _, proposal := range []models.AIActionProposal{first, second} {
		var current models.AIActionProposal
		if err := a.db.Take(&current, "id=?", proposal.ID).Error; err != nil || current.Status != "pending" {
			t.Fatalf("queue-full batch partially confirmed proposal=%+v err=%v", current, err)
		}
	}
	a.aiDelegations.releaseReservation(blocker)
	response := performRequest(router, http.MethodPost, path, body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm batch=%d %s", response.Code, response.Body.String())
	}
	var accepted struct {
		Data []aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if len(accepted.Data) != 2 || accepted.Data[0].ID != first.ID || accepted.Data[1].ID != second.ID {
		t.Fatalf("response did not preserve the selected order: %+v", accepted.Data)
	}
	for _, proposal := range []models.AIActionProposal{first, second} {
		queued, err := aiAgentDelegationLifecycleEventExists(a.db, proposal.ID, aiDelegationQueuedEvent)
		if err != nil || !queued {
			t.Fatalf("confirmed batch item %s has no durable launch event: queued=%v err=%v", proposal.ID, queued, err)
		}
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("a committed child was not launched")
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := a.db.Model(&models.AIGeneration{}).Where("session_id IN ? AND status='completed'", []string{*accepted.Data[0].ResultID, *accepted.Data[1].ResultID}).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var completedCount int64
	if err := a.db.Model(&models.AIGeneration{}).Where("session_id IN ? AND status='completed'", []string{*accepted.Data[0].ResultID, *accepted.Data[1].ResultID}).Count(&completedCount).Error; err != nil || completedCount != 2 {
		t.Fatalf("children did not reach terminal state: count=%d err=%v", completedCount, err)
	}
	var sessions int64
	if err := a.db.Model(&models.AISession{}).Count(&sessions).Error; err != nil || sessions != 3 {
		t.Fatalf("expected exactly two child sessions: count=%d err=%v", sessions, err)
	}

	replayed := performRequest(router, http.MethodPost, path, body, nil)
	if replayed.Code != http.StatusOK {
		t.Fatalf("exact replay=%d %s", replayed.Code, replayed.Body.String())
	}
	var replayedOutput struct {
		Data []aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayedOutput); err != nil || len(replayedOutput.Data) != 2 {
		t.Fatalf("replay output=%+v err=%v", replayedOutput, err)
	}
	if replayedOutput.Data[0].ResultID == nil || replayedOutput.Data[1].ResultID == nil || *replayedOutput.Data[0].ResultID != *accepted.Data[0].ResultID || *replayedOutput.Data[1].ResultID != *accepted.Data[1].ResultID {
		t.Fatalf("exact replay changed child identities: first=%+v replay=%+v", accepted.Data, replayedOutput.Data)
	}
	if err := a.db.Model(&models.AISession{}).Count(&sessions).Error; err != nil || sessions != 3 {
		t.Fatalf("exact replay created more child sessions: count=%d err=%v", sessions, err)
	}
}
