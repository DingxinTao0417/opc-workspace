package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAgentChildrenToolRequiresFreshScopeForChildResult(t *testing.T) {
	router, a, provider, parent, generation, client := newAIAgentDelegationFixture(t)
	client.stream = func(context.Context) (harness.Turn, error) {
		return harness.Turn{Text: "PRIVATE_CHILD_FINDING[opc:selfcheck]{\"sufficient\":true}[/opc:selfcheck]"}, nil
	}
	initialGrant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "clients", "outputs", "actions", "agent_execution"}}
	registry, err := a.aiChatToolRegistry(parent.ID, true, &provider, initialGrant, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	spawn, ok := registry.Get("workspace_delegate_agent")
	if !ok {
		t.Fatal("missing delegation tool")
	}
	proposed, err := spawn.Execute(context.Background(), []byte(`{"task_name":"fact_check","message":"PRIVATE_CHILD_INSTRUCTION","scopes":["work","clients","outputs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var proposalID struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(proposed), &proposalID); err != nil {
		t.Fatal(err)
	}
	var proposal models.AIActionProposal
	if err := a.db.Take(&proposal, "id=?", proposalID.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decision := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, proposal.Fingerprint)), nil)
	if decision.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", decision.Code, decision.Body.String())
	}
	if err := a.db.Take(&proposal, "id=?", proposal.ID).Error; err != nil || proposal.ResultID == nil {
		t.Fatalf("confirmed proposal=%+v err=%v", proposal, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var child models.AIGeneration
		err := a.db.Take(&child, "id=?", proposal.ID).Error
		if err == nil && child.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child=%+v err=%v", child, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending := models.AIActionProposal{ID: uuid.NewString(), GenerationID: proposal.ID,
		Fingerprint: strings.Repeat("b", 64), ActionJSON: `{"action":"task.create","changes":{"title":"PRIVATE_PENDING_APPROVAL"}}`,
		PreviewJSON: `{"label":"PRIVATE_PENDING_APPROVAL","before":{},"after":{}}`, Status: "pending", CreatedAt: nowStamp(a)}
	if err := a.db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	currentGeneration := models.AIGeneration{ID: uuid.NewString(), SessionID: parent.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: nowStamp(a), UpdatedAt: nowStamp(a)}
	if err := a.db.Create(&currentGeneration).Error; err != nil {
		t.Fatal(err)
	}
	withoutClients := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions", "agent_execution"}}
	registry, err = a.aiChatToolRegistry(parent.ID, true, &provider, withoutClients, currentGeneration.ID)
	if err != nil {
		t.Fatal(err)
	}
	children, ok := registry.Get("workspace_agent_children")
	if !ok {
		t.Fatal("parent cannot inspect delegated child status")
	}
	listed, err := children.Execute(context.Background(), []byte(`{"view":"list"}`))
	if err != nil || !strings.Contains(listed, *proposal.ResultID) || !strings.Contains(listed, `"status":"completed"`) || !strings.Contains(listed, `"pending_approvals":1`) {
		t.Fatalf("list=%s err=%v", listed, err)
	}
	for _, secret := range []string{"PRIVATE_CHILD_INSTRUCTION", "PRIVATE_CHILD_FINDING", "PRIVATE_PENDING_APPROVAL", "scopes", "provider_name", "content_sha256"} {
		if strings.Contains(listed, secret) {
			t.Fatalf("list leaked %s: %s", secret, listed)
		}
	}
	resultArgs := []byte(fmt.Sprintf(`{"view":"result","child_session_id":%q,"content_offset":0,"content_limit":100}`, *proposal.ResultID))
	if _, err := children.Execute(context.Background(), resultArgs); err == nil {
		t.Fatal("result disclosed without the child's clients scope")
	}
	registry, err = a.aiChatToolRegistry(parent.ID, true, &provider, initialGrant, currentGeneration.ID)
	if err != nil {
		t.Fatal(err)
	}
	children, _ = registry.Get("workspace_agent_children")
	page, err := children.Execute(context.Background(), resultArgs)
	if err != nil || !strings.Contains(page, "PRIVATE_CHILD_FINDING") || !strings.Contains(page, `"pending_approvals":1`) || strings.Contains(page, "PRIVATE_CHILD_INSTRUCTION") {
		t.Fatalf("result=%s err=%v", page, err)
	}
	firstPage, err := children.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"result","child_session_id":%q,"content_limit":7}`, *proposal.ResultID)))
	if err != nil {
		t.Fatal(err)
	}
	var pageResult struct {
		Content      string `json:"content"`
		Next         *int   `json:"next_content_offset"`
		GenerationID string `json:"generation_id"`
		SHA256       string `json:"content_sha256"`
	}
	if err := json.Unmarshal([]byte(firstPage), &pageResult); err != nil || pageResult.Content != "PRIVATE" || pageResult.Next == nil || *pageResult.Next != 7 || pageResult.GenerationID != proposal.ID || pageResult.SHA256 != sha256Hex([]byte("PRIVATE_CHILD_FINDING")) {
		t.Fatalf("first page=%s err=%v", firstPage, err)
	}
	secondPage, err := children.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"result","child_session_id":%q,"generation_id":%q,"content_offset":7,"content_limit":6,"expected_content_sha256":%q}`, *proposal.ResultID, pageResult.GenerationID, pageResult.SHA256)))
	if err != nil || !strings.Contains(secondPage, `"content":"_CHILD"`) {
		t.Fatalf("second page=%s err=%v", secondPage, err)
	}
	if _, err := children.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"result","child_session_id":%q,"content_offset":7,"expected_content_sha256":%q}`, *proposal.ResultID, strings.Repeat("0", 64)))); err == nil {
		t.Fatal("a page from a different child reply was accepted")
	}
	if _, err := children.Execute(context.Background(), []byte(`{"view":"result","child_session_id":"`+uuid.NewString()+`"}`)); err == nil {
		t.Fatal("foreign child was accepted")
	}
	for _, bad := range []string{`{"view":"list","child_session_id":"` + *proposal.ResultID + `"}`, `{"view":"list","expected_content_sha256":"` + pageResult.SHA256 + `"}`, `{"view":"list","expected_content_sha256":""}`, `{"view":"result","child_session_id":"` + *proposal.ResultID + `","content_offset":-1}`, `{"view":"result","child_session_id":"` + *proposal.ResultID + `","content_offset":7}`, `{"view":"result","child_session_id":"` + *proposal.ResultID + `","expected_content_sha256":""}`, `{"view":"result","child_session_id":"` + *proposal.ResultID + `","expected_content_sha256":"` + strings.ToUpper(pageResult.SHA256) + `"}`, `{"view":"result","child_session_id":"` + *proposal.ResultID + `","extra":true}`} {
		if _, err := children.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("invalid input accepted: %s", bad)
		}
	}
	for _, bad := range []string{`{"view":null}`, `{"view":"list","view":"result"}`, `{"View":"list"}`} {
		if _, err := children.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("ambiguous input accepted: %s", bad)
		}
	}
	childRegistry, err := a.aiChatToolRegistry(*proposal.ResultID, true, &provider, initialGrant, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := childRegistry.Get("workspace_agent_children"); ok {
		t.Fatal("child conversation can enumerate delegation family")
	}
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", proposal.ID).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	waitArgs := []byte(fmt.Sprintf(`{"view":"wait","child_session_id":%q,"timeout_ms":30}`, *proposal.ResultID))
	waited, err := children.Execute(context.Background(), waitArgs)
	if err != nil || !strings.Contains(waited, `"status":"streaming"`) || !strings.Contains(waited, `"timed_out":true`) || strings.Contains(waited, "PRIVATE_CHILD_FINDING") {
		t.Fatalf("bounded wait=%s err=%v", waited, err)
	}
	a.aiContinuations = &aiContinuationCoordinator{wakeup: make(chan struct{}, 1)}
	updated := make(chan error, 1)
	time.AfterFunc(50*time.Millisecond, func() {
		updateErr := a.db.Model(&models.AIGeneration{}).Where("id=?", proposal.ID).Update("status", "completed").Error
		if updateErr == nil {
			// Model the production post-commit notification. The wait must return
			// from this event, before its one-second database fallback or context.
			a.wakeAIContinuationsAfterFactCommit()
		}
		updated <- updateErr
	})
	waitCtx, cancelWait := context.WithCancel(context.Background())
	watchdog := time.AfterFunc(300*time.Millisecond, cancelWait)
	defer func() {
		watchdog.Stop()
		cancelWait()
	}()
	waitArgs = []byte(fmt.Sprintf(`{"view":"wait","child_session_id":%q,"timeout_ms":1000}`, *proposal.ResultID))
	waited, err = children.Execute(waitCtx, waitArgs)
	if updateErr := <-updated; updateErr != nil {
		t.Fatal(updateErr)
	}
	if err != nil || !strings.Contains(waited, `"status":"completed"`) || !strings.Contains(waited, `"pending_approvals":1`) || !strings.Contains(waited, `"timed_out":false`) || strings.Contains(waited, "PRIVATE_CHILD_FINDING") || strings.Contains(waited, "content_sha256") {
		t.Fatalf("completed wait=%s err=%v", waited, err)
	}
	rootRegistry, err := a.aiChatToolRegistry(parent.ID, true, &provider, initialGrant, currentGeneration.ID)
	if err != nil {
		t.Fatal(err)
	}
	spawn, ok = rootRegistry.Get("workspace_delegate_agent")
	if !ok {
		t.Fatal("parent lost its explicit delegation tool")
	}
	secondProposalBody, err := spawn.Execute(context.Background(), []byte(`{"task_name":"second_check","message":"Check the second exact child status.","scopes":["work","outputs"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var secondID struct {
		ID string `json:"proposal_id"`
	}
	if err := json.Unmarshal([]byte(secondProposalBody), &secondID); err != nil {
		t.Fatal(err)
	}
	var secondProposal models.AIActionProposal
	if err := a.db.Take(&secondProposal, "id=?", secondID.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&currentGeneration).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+secondProposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_agent_delegation":true}`, secondProposal.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm second child=%d %s", confirmed.Code, confirmed.Body.String())
	}
	var secondReceipt models.AIActionProposal
	if err := a.db.Take(&secondReceipt, "id=?", secondProposal.ID).Error; err != nil || secondReceipt.ResultID == nil {
		t.Fatalf("second receipt=%+v err=%v", secondReceipt, err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		var child models.AIGeneration
		err := a.db.Take(&child, "id=?", secondProposal.ID).Error
		if err == nil && child.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second child=%+v err=%v", child, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	withoutClientsRegistry, err := a.aiChatToolRegistry(parent.ID, true, &provider, withoutClients, currentGeneration.ID)
	if err != nil {
		t.Fatal(err)
	}
	withoutClientsChildren, _ := withoutClientsRegistry.Get("workspace_agent_children")
	resultsArgs := []byte(fmt.Sprintf(`{"view":"results","child_session_ids":[%q,%q],"content_limit":7}`, *proposal.ResultID, *secondReceipt.ResultID))
	partial, err := withoutClientsChildren.Execute(context.Background(), resultsArgs)
	if err == nil || partial != "" {
		t.Fatalf("batch disclosed a readable child's result when another child lacked a fresh grant: %s err=%v", partial, err)
	}
	results, err := children.Execute(context.Background(), resultsArgs)
	var batchResult struct {
		View     string `json:"view"`
		Children []struct {
			SessionID  string `json:"child_session_id"`
			Generation string `json:"generation_id"`
			ReplyKind  string `json:"reply_kind"`
			SHA256     string `json:"content_sha256"`
			Content    string `json:"content"`
			Trust      string `json:"content_trust"`
			Next       *int   `json:"next_content_offset"`
		} `json:"children"`
	}
	if err != nil || json.Unmarshal([]byte(results), &batchResult) != nil || batchResult.View != "results" || len(batchResult.Children) != 2 {
		t.Fatalf("batch results=%s projection=%+v err=%v", results, batchResult, err)
	}
	for index, child := range batchResult.Children {
		wantSessionID := *proposal.ResultID
		wantGenerationID := proposal.ID
		if index == 1 {
			wantSessionID = *secondReceipt.ResultID
			wantGenerationID = secondProposal.ID
		}
		if child.SessionID != wantSessionID || child.Generation != wantGenerationID || child.ReplyKind != "spawn" || child.SHA256 != sha256Hex([]byte("PRIVATE_CHILD_FINDING")) || child.Content != "PRIVATE" || child.Trust != "untrusted_child_reply" || child.Next == nil || *child.Next != 7 {
			t.Fatalf("batch child %d=%+v want_session=%s want_generation=%s", index, child, wantSessionID, wantGenerationID)
		}
	}
	for _, secret := range []string{"PRIVATE_CHILD_INSTRUCTION", "provider_name", "endpoint", "agent_files", "recovery_staging"} {
		if strings.Contains(results, secret) {
			t.Fatalf("batch result leaked %s: %s", secret, results)
		}
	}
	tooManyIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()}
	tooManyJSON, _ := json.Marshal(map[string]any{"view": "results", "child_session_ids": tooManyIDs})
	for _, bad := range []string{
		`{"view":"results","child_session_ids":[]}`,
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q,%q]}`, *proposal.ResultID, *proposal.ResultID),
		string(tooManyJSON),
		fmt.Sprintf(`{"view":"results","child_session_id":%q,"child_session_ids":[%q]}`, *proposal.ResultID, *proposal.ResultID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"content_offset":0}`, *proposal.ResultID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"generation_id":%q}`, *proposal.ResultID, proposal.ID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"timeout_ms":1}`, *proposal.ResultID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"content_limit":0}`, *proposal.ResultID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"content_limit":1001}`, *proposal.ResultID),
		fmt.Sprintf(`{"view":"results","child_session_ids":[%q],"child_session_ids":[%q]}`, *proposal.ResultID, *secondReceipt.ResultID),
	} {
		if _, err := children.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("invalid batch input accepted: %s", bad)
		}
	}
	if _, err := children.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"results","child_session_ids":[%q]}`, uuid.NewString()))); err == nil {
		t.Fatal("batch results accepted a foreign child")
	}
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", secondProposal.ID).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	waitAnyArgs := []byte(fmt.Sprintf(`{"view":"wait","child_session_ids":[%q,%q],"timeout_ms":1000}`, *proposal.ResultID, *secondReceipt.ResultID))
	waited, err = children.Execute(context.Background(), waitAnyArgs)
	var waitAnyResult struct {
		WaitMode    string   `json:"wait_mode"`
		TerminalIDs []string `json:"terminal_child_session_ids"`
		TimedOut    bool     `json:"timed_out"`
		Children    []struct {
			SessionID string `json:"child_session_id"`
			Status    string `json:"status"`
		} `json:"children"`
	}
	if err != nil || json.Unmarshal([]byte(waited), &waitAnyResult) != nil || waitAnyResult.WaitMode != "any" || waitAnyResult.TimedOut || len(waitAnyResult.Children) != 2 || waitAnyResult.Children[0].SessionID != *proposal.ResultID || waitAnyResult.Children[0].Status != "completed" || waitAnyResult.Children[1].SessionID != *secondReceipt.ResultID || waitAnyResult.Children[1].Status != "streaming" || len(waitAnyResult.TerminalIDs) != 1 || waitAnyResult.TerminalIDs[0] != *proposal.ResultID || strings.Contains(waited, "PRIVATE_CHILD_FINDING") {
		t.Fatalf("wait-any=%s projection=%+v err=%v", waited, waitAnyResult, err)
	}
	waitAllArgs := []byte(fmt.Sprintf(`{"view":"wait","child_session_ids":[%q,%q],"wait_mode":"all","timeout_ms":50}`, *proposal.ResultID, *secondReceipt.ResultID))
	waited, err = children.Execute(context.Background(), waitAllArgs)
	var waitAllResult struct {
		WaitMode    string   `json:"wait_mode"`
		TerminalIDs []string `json:"terminal_child_session_ids"`
		TimedOut    bool     `json:"timed_out"`
		Children    []struct {
			SessionID string `json:"child_session_id"`
			Status    string `json:"status"`
		} `json:"children"`
	}
	if err != nil || json.Unmarshal([]byte(waited), &waitAllResult) != nil || waitAllResult.WaitMode != "all" || !waitAllResult.TimedOut || len(waitAllResult.Children) != 2 || waitAllResult.Children[0].Status != "completed" || waitAllResult.Children[1].Status != "streaming" || len(waitAllResult.TerminalIDs) != 1 || waitAllResult.TerminalIDs[0] != *proposal.ResultID || strings.Contains(waited, "PRIVATE_CHILD_FINDING") {
		t.Fatalf("wait-all partial timeout=%s projection=%+v err=%v", waited, waitAllResult, err)
	}
	if err := a.db.Model(&models.AIGeneration{}).Where("id=?", secondProposal.ID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	allCompleteArgs := []byte(fmt.Sprintf(`{"view":"wait","child_session_ids":[%q,%q],"wait_mode":"all","timeout_ms":1000}`, *proposal.ResultID, *secondReceipt.ResultID))
	waited, err = children.Execute(context.Background(), allCompleteArgs)
	if err != nil || json.Unmarshal([]byte(waited), &waitAllResult) != nil || waitAllResult.WaitMode != "all" || waitAllResult.TimedOut || len(waitAllResult.Children) != 2 || len(waitAllResult.TerminalIDs) != 2 {
		t.Fatalf("wait-all complete=%s projection=%+v err=%v", waited, waitAllResult, err)
	}
	for _, bad := range []string{
		`{"view":"wait"}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","timeout_ms":15001}`,
		`{"view":"wait","child_session_ids":[]}`,
		`{"view":"wait","child_session_ids":["` + *proposal.ResultID + `","` + *proposal.ResultID + `"]}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","wait_mode":"first"}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","wait_mode":null}`,
		`{"view":"list","wait_mode":"all"}`,
		`{"view":"result","child_session_id":"` + *proposal.ResultID + `","wait_mode":"all"}`,
		`{"view":"results","child_session_ids":["` + *proposal.ResultID + `"],"wait_mode":"all"}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","child_session_ids":["` + *proposal.ResultID + `"]}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","timeout_ms":null}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","timeout_ms":10,"timeout_ms":20}`,
		`{"view":"wait","child_session_id":"` + *proposal.ResultID + `","expected_content_sha256":"` + pageResult.SHA256 + `"}`,
		`{"view":"list","timeout_ms":10}`,
		`{"view":"result","child_session_id":"` + *proposal.ResultID + `","timeout_ms":10}`,
	} {
		if _, err := children.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("invalid wait input accepted: %s", bad)
		}
	}
	if _, err := children.Execute(context.Background(), []byte(`{"view":"wait","child_session_id":"`+uuid.NewString()+`","timeout_ms":10}`)); err == nil {
		t.Fatal("wait accepted a foreign child")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := children.Execute(cancelled, waitArgs); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait = %v", err)
	}
}

func TestAIAgentChildrenResultGrantRequiresExactKnowledgeSources(t *testing.T) {
	source := aiKnowledgeSourceGrant{SourceID: uuid.NewString(), ExpectedSourceVersion: 2}
	providerID := uuid.NewString()
	frozen := &aiAgentDelegationPreview{ProviderID: providerID, ProviderVersion: 3, ConfigVersion: 4, Scopes: []string{"work", "knowledge"}, Knowledge: []aiKnowledgeSourceGrant{source}}
	current := &aiWorkspaceGrant{ProviderVersion: 3, Scopes: []string{"work", "knowledge", "outputs"}, KnowledgeSources: []aiKnowledgeSourceGrant{source}}
	if !allowsAIAgentChildResult(current, providerID, 4, frozen) {
		t.Fatal("matching current grant rejected")
	}
	if allowsAIAgentChildResult(current, uuid.NewString(), 4, frozen) {
		t.Fatal("different Provider accepted")
	}
	if allowsAIAgentChildResult(current, providerID, 5, frozen) {
		t.Fatal("changed Provider configuration accepted")
	}
	current.KnowledgeSources[0].ExpectedSourceVersion++
	if allowsAIAgentChildResult(current, providerID, 4, frozen) {
		t.Fatal("changed source version reused an old grant")
	}
	current.KnowledgeSources[0].ExpectedSourceVersion--
	current.Scopes = []string{"work", "outputs"}
	if allowsAIAgentChildResult(current, providerID, 4, frozen) {
		t.Fatal("missing knowledge scope accepted")
	}
	current.Scopes = []string{"work", "knowledge", "outputs"}
	current.ProviderVersion++
	if allowsAIAgentChildResult(current, providerID, 4, frozen) {
		t.Fatal("changed provider version accepted")
	}
	current.ProviderVersion--
	frozen.Scopes = []string{"work", "agent_files"}
	current.Scopes = []string{"work", "outputs", "actions", "agent_execution", "agent_files"}
	if allowsAIAgentChildResult(current, providerID, 4, frozen) {
		t.Fatal("file-bound child reply accepted without exact file disclosure")
	}
}

func TestAIAgentChildrenResultAggregateUTF8ByteBudget(t *testing.T) {
	page := strings.Repeat("😀", 1000) // 1000 Unicode characters, 4000 UTF-8 bytes.
	total, err := addAIAgentChildResultBytes(0, page)
	if err != nil || total != 4000 {
		t.Fatalf("first page byte count=%d err=%v", total, err)
	}
	total, err = addAIAgentChildResultBytes(total, page)
	if err != nil || total != 8000 {
		t.Fatalf("second page byte count=%d err=%v", total, err)
	}
	total, err = addAIAgentChildResultBytes(total, strings.Repeat("a", 192))
	if err != nil || total != aiAgentChildrenResultsMaxContentBytes {
		t.Fatalf("exact budget byte count=%d err=%v", total, err)
	}
	if _, err := addAIAgentChildResultBytes(total, "a"); err == nil {
		t.Fatal("aggregate larger than 8 KiB was accepted")
	}
	if _, err := addAIAgentChildResultBytes(0, strings.Repeat("x", aiAgentChildrenResultsMaxContentBytes+1)); err == nil {
		t.Fatal("single oversized page was accepted")
	}
}
