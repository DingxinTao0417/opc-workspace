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
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAgentFamilyProjectsParentAndChildrenWithoutInstructionOrGrant(t *testing.T) {
	router, a, provider, parent, generation, _ := newAIAgentDelegationFixture(t)
	router.GET("/api/v1/ai/sessions/:id/agent-family", a.getAIAgentFamily)
	empty := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+parent.ID+"/agent-family", nil, nil)
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"parent":null`) || !strings.Contains(empty.Body.String(), `"children":[]`) {
		t.Fatalf("empty family=%d %s", empty.Code, empty.Body.String())
	}
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, grant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := registry.Get("workspace_delegate_agent")
	result, err := tool.Execute(context.Background(), []byte(`{"task_name":"fact_check","message":"PRIVATE_CHILD_INSTRUCTION","scopes":["work","outputs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var proposed struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(result), &proposed); err != nil {
		t.Fatal(err)
	}
	var proposal models.AIActionProposal
	if err := a.db.Take(&proposal, "id=?", proposed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposed.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint)), nil)
	if decision.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", decision.Code, decision.Body.String())
	}
	if err := a.db.Take(&proposal, "id=?", proposed.ID).Error; err != nil || proposal.ResultID == nil {
		t.Fatalf("confirmed proposal=%+v err=%v", proposal, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var child models.AIGeneration
		err := a.db.Take(&child, "id=?", proposed.ID).Error
		if err == nil && child.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child generation=%+v err=%v", child, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	tampered := proposal
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(tampered.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	preview.AgentDelegation.Scopes = []string{"work"}
	changed, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	tampered.PreviewJSON = string(changed)
	if _, err := projectAIAgentFamilyMember(a.db, tampered, parent.ID, a.options.Now()); err == nil {
		t.Fatal("tampered delegation scopes were accepted")
	}
	parentView := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+parent.ID+"/agent-family", nil, nil)
	if parentView.Code != http.StatusOK || !strings.Contains(parentView.Body.String(), `"children":[{`) || !strings.Contains(parentView.Body.String(), `"status":"completed"`) || !strings.Contains(parentView.Body.String(), `"pending_approvals":0`) || !strings.Contains(parentView.Body.String(), *proposal.ResultID) {
		t.Fatalf("parent family=%d %s", parentView.Code, parentView.Body.String())
	}
	for _, secret := range []string{"PRIVATE_CHILD_INSTRUCTION", "scopes", "provider_name", "http://127.0.0.1:1", "result_text"} {
		if strings.Contains(parentView.Body.String(), secret) {
			t.Fatalf("parent family leaked %s: %s", secret, parentView.Body.String())
		}
	}
	activePendingID := ""
	for index, candidate := range []struct {
		generationID string
		status       string
		createdAt    string
	}{
		{proposed.ID, "pending", nowStamp(a)},
		{proposed.ID, "pending", a.options.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)},
		{proposed.ID, "rejected", nowStamp(a)},
		{generation.ID, "pending", nowStamp(a)},
	} {
		row := models.AIActionProposal{ID: uuid.NewString(), GenerationID: candidate.generationID,
			Fingerprint: fmt.Sprintf("%064x", index+1), ActionJSON: `{"action":"task.create","changes":{"title":"PRIVATE_PENDING_CONTENT"}}`,
			PreviewJSON: `{"label":"PRIVATE_PENDING_CONTENT","before":{},"after":{}}`, Status: candidate.status, CreatedAt: candidate.createdAt}
		if candidate.status == "rejected" {
			row.DecidedAt = &candidate.createdAt
		}
		if err := a.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			activePendingID = row.ID
		}
	}
	parentView = performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+parent.ID+"/agent-family", nil, nil)
	if parentView.Code != http.StatusOK || !strings.Contains(parentView.Body.String(), `"pending_approvals":1`) || strings.Contains(parentView.Body.String(), "PRIVATE_PENDING_CONTENT") {
		t.Fatalf("child approval summary=%d %s", parentView.Code, parentView.Body.String())
	}
	receipt, err := aiActionOutputWithFacts(a.db, proposal, "completed", a.options.Now())
	if err != nil || receipt.AgentDelegationResult == nil || receipt.AgentDelegationResult.PendingApprovals != 1 {
		t.Fatalf("child approval receipt=%+v err=%v", receipt.AgentDelegationResult, err)
	}
	if state, satisfied := aiWorkPlanActionState(receipt); state != "child_pending_approval" || satisfied || aiPlanContinuationActionBlocker(receipt) != "pending_approval" {
		t.Fatalf("child approval incorrectly satisfied plan: %s %v", state, satisfied)
	}
	decidedAt := nowStamp(a)
	if err := a.db.Model(&models.AIActionProposal{}).Where("id=?", activePendingID).Updates(map[string]any{"status": "rejected", "decided_at": decidedAt}).Error; err != nil {
		t.Fatal(err)
	}
	parentView = performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+parent.ID+"/agent-family", nil, nil)
	if parentView.Code != http.StatusOK || !strings.Contains(parentView.Body.String(), `"pending_approvals":0`) {
		t.Fatalf("decided child approval summary=%d %s", parentView.Code, parentView.Body.String())
	}
	receipt, err = aiActionOutputWithFacts(a.db, proposal, "completed", a.options.Now())
	if err != nil || receipt.AgentDelegationResult == nil || receipt.AgentDelegationResult.PendingApprovals != 0 {
		t.Fatalf("decided child approval receipt=%+v err=%v", receipt.AgentDelegationResult, err)
	}
	if state, satisfied := aiWorkPlanActionState(receipt); state != "completed" || !satisfied || aiPlanContinuationActionBlocker(receipt) != "" {
		t.Fatalf("decided child approval still blocked plan: %s %v", state, satisfied)
	}
	childView := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+*proposal.ResultID+"/agent-family", nil, nil)
	if childView.Code != http.StatusOK || !strings.Contains(childView.Body.String(), `"children":[]`) || !strings.Contains(childView.Body.String(), `"parent":{"session_id":"`+parent.ID+`"`) || !strings.Contains(childView.Body.String(), `"pending_approvals":0`) {
		t.Fatalf("child family=%d %s", childView.Code, childView.Body.String())
	}
	invalid := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/not-a-uuid/agent-family", nil, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid session=%d %s", invalid.Code, invalid.Body.String())
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+uuid.NewString()+"/agent-family", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session=%d %s", missing.Code, missing.Body.String())
	}
}
