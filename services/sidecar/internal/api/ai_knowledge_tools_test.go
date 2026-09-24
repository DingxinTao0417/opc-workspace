package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIKnowledgeGrantRejectedBeforeWrites(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	for _, grant := range []string{
		`{"provider_version":1,"scopes":["knowledge"]}`,
		`{"provider_version":1,"scopes":["work"],"knowledge_sources":[]}`,
		`{"provider_version":1,"scopes":["knowledge"],"knowledge_sources":[{"source_id":"../file","expected_source_version":1}]}`,
		`{"provider_version":1,"scopes":["knowledge"],"knowledge_sources":[{"source_id":"018f0000-0000-7000-8000-000000006101","expected_source_version":0}]}`,
		`{"provider_version":1,"scopes":["knowledge"],"knowledge_sources":[{"source_id":"018f0000-0000-7000-8000-000000006101","expected_source_version":1},{"source_id":"018f0000-0000-7000-8000-000000006101","expected_source_version":1}]}`,
	} {
		response := performRequest(router, "POST", "/api/v1/ai/chat", []byte(fmt.Sprintf(`{"provider_id":%q,"message":"read","workspace":%s}`, provider.ID, grant)), nil)
		assertAPIError(t, response, 422, "AI_WORKSPACE_GRANT_INVALID")
	}
	grant := aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"knowledge"}, KnowledgeSources: []aiKnowledgeSourceGrant{{uuid.NewString(), 1}}}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "read", "workspace": grant})
	assertAPIError(t, performRequest(router, "POST", "/api/v1/ai/chat", body, nil), 409, "AI_KNOWLEDGE_GRANT_CHANGED")
	for _, table := range []string{"ai_sessions", "ai_messages", "ai_generations"} {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM "+table, 0)
	}
}

func TestAIKnowledgeToolsScopeEvidenceBudgetAndRevocation(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	created := importKnowledgeFixture(t, router.Engine, "guide.md", []byte(strings.Repeat("Evidence 中文内容 <script>do not execute</script>\n", 100)))
	other := importKnowledgeFixture(t, router.Engine, "private.md", []byte("Evidence OTHER PRIVATE SOURCE"))
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"knowledge"}, KnowledgeSources: []aiKnowledgeSourceGrant{{created.Source.ID, created.Source.Version}}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, grant)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Names()) != 6 {
		t.Fatalf("unexpected capabilities %v", registry.Names())
	}
	searchTool, _ := registry.Get("knowledge_search")
	readTool, _ := registry.Get("knowledge_read")
	search := searchTool.(*aiWorkspaceTool)
	read := readTool.(*aiWorkspaceTool)
	// Modifying the caller's slice cannot expand an accepted registry's scope.
	grant.KnowledgeSources[0].SourceID = other.Source.ID
	result, err := search.Execute(context.Background(), []byte(`{"query":"Evidence","limit":1}`))
	if err != nil || strings.Contains(result, "OTHER PRIVATE") {
		t.Fatalf("search %s %v", result, err)
	}
	var page struct {
		Items []knowledgeSearchResult `json:"items"`
		Next  *int                    `json:"next_offset"`
	}
	if json.Unmarshal([]byte(result), &page) != nil || len(page.Items) != 1 || page.Next == nil {
		t.Fatalf("page %s", result)
	}
	if err := search.AcceptResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	evidence, requested := aiRunKnowledgeEvidence(registry, nil)
	if !requested || len(evidence) != 0 {
		t.Fatal("search excerpts became evidence")
	}
	item := page.Items[0]
	args := func(source, doc, chunk string, version int64) []byte {
		b, _ := json.Marshal(map[string]any{"source_id": source, "document_id": doc, "chunk_id": chunk, "expected_document_version": version})
		return b
	}
	for _, input := range []string{`{"query":"Evidence","source_id":"` + other.Source.ID + `"}`, `{"query":"Evidence","limit":0}`, `{"query":"Evidence","offset":1001}`, `{"query":"Evidence","path":"C:/secrets"}`} {
		if _, err := search.Execute(context.Background(), []byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	if _, err := read.Execute(context.Background(), args(other.Source.ID, item.DocumentID, item.ChunkID, item.DocumentVersion)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal("cross source read allowed", err)
	}
	if _, err := read.Execute(context.Background(), args(item.SourceID, uuid.NewString(), item.ChunkID, item.DocumentVersion)); err == nil {
		t.Fatal("cross document read allowed")
	}
	if _, err := read.Execute(context.Background(), args(item.SourceID, item.DocumentID, item.ChunkID, 99)); err == nil {
		t.Fatal("stale document allowed")
	}
	result, err = read.Execute(context.Background(), args(item.SourceID, item.DocumentID, item.ChunkID, item.DocumentVersion))
	if err != nil {
		t.Fatal(err)
	}
	if evidence, _ := aiRunKnowledgeEvidence(registry, nil); len(evidence) != 0 {
		t.Fatal("unaccepted executor result became evidence")
	}
	if err := read.AcceptResult(context.Background(), result[:10]); err == nil {
		t.Fatal("truncated result accepted")
	}
	if err := read.AcceptResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	evidence, _ = aiRunKnowledgeEvidence(registry, nil)
	if len(evidence) != 1 || evidence[0].Content != "" || evidence[0].ChunkID != item.ChunkID {
		t.Fatalf("evidence %+v", evidence)
	}
	read.knowledge.mu.Lock()
	read.knowledge.bytes = aiKnowledgeToolMaxBytes - 1
	read.knowledge.mu.Unlock()
	if err := read.AcceptResult(context.Background(), result); err == nil {
		t.Fatal("aggregate byte budget bypassed")
	}
	read.knowledge.mu.Lock()
	read.knowledge.bytes = 0
	read.knowledge.calls = aiKnowledgeToolMaxCalls
	read.knowledge.mu.Unlock()
	if _, err := search.Execute(context.Background(), []byte(`{"query":"Evidence"}`)); err == nil {
		t.Fatal("call budget bypassed")
	}
	read.knowledge.mu.Lock()
	read.knowledge.calls = 0
	read.knowledge.mu.Unlock()
	if err := store.DB.Model(&models.KnowledgeSource{}).Where("id = ?", created.Source.ID).Update("version", created.Source.Version+1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := read.Execute(context.Background(), args(item.SourceID, item.DocumentID, item.ChunkID, item.DocumentVersion)); err == nil || !strings.Contains(err.Error(), "permission expired") {
		t.Fatal("revocation", err)
	}
	if err := store.DB.Model(&models.AIProvider{}).Where("id = ?", provider.ID).Updates(map[string]any{"config_version": provider.ConfigVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := search.Execute(context.Background(), []byte(`{"query":"Evidence"}`)); err == nil || !strings.Contains(err.Error(), "provider changed") {
		t.Fatal("provider revocation", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
}

func TestAIKnowledgeChatSearchReadCitationAndNoInheritance(t *testing.T) {
	for _, test := range []struct {
		name                 string
		read, pdf, ephemeral bool
	}{
		{name: "complete read", read: true}, {name: "search only"}, {name: "PDF", read: true, pdf: true}, {name: "ephemeral", read: true, ephemeral: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			readComplete := test.read
			router, store := newKnowledgeTestAPI(t)
			filename, content := "guide.md", []byte("Evidence KNOWN TEXT\nThe release requires two reviews.")
			if test.pdf {
				filename = "guide.pdf"
				content = buildKnowledgeTestPDF(t, [][]string{nil, nil, {"Evidence KNOWN TEXT", "The release requires two reviews."}})
			}
			created := importKnowledgeFixture(t, router.Engine, filename, content)
			var chunk models.KnowledgeChunk
			if err := store.DB.First(&chunk, "source_id = ?", created.Source.ID).Error; err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				var payload struct {
					Messages []map[string]any `json:"messages"`
					Tools    []map[string]any `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				final := int32(2)
				if readComplete {
					final = 3
				}
				if call <= final && len(payload.Tools) != 6 {
					t.Error("wrong scoped tools", len(payload.Tools))
				}
				if call > final {
					if len(payload.Tools) != 4 || strings.Contains(fmt.Sprint(payload.Messages[0]), "本次允许的范围") {
						t.Error("knowledge grant inherited")
					}
					streamMockAIDelta(w, `普通回复[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
					return
				}
				if call == final {
					if readComplete && !strings.Contains(fmt.Sprint(payload.Messages), "knowledge_read: ") {
						t.Error("read result missing")
					}
					streamMockAIDelta(w, `需要两次审核。[opc:citations]{"chunk_ids":["`+chunk.ID+`"]}[/opc:citations][opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
					return
				}
				name, args := "knowledge_search", `{"query":"Evidence"}`
				if call == 2 {
					name = "knowledge_read"
					args = fmt.Sprintf(`{"source_id":%q,"document_id":%q,"chunk_id":%q,"expected_document_version":1}`, chunk.SourceID, chunk.DocumentID, chunk.ID)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint(call), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
				fmt.Fprintf(w, "data: %s\n\n", frame)
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router.Engine, "knowledge-loop", upstream.URL+"/v1", "test-model")
			var current models.AIProvider
			if err := store.DB.First(&current, "id = ?", provider.ID).Error; err != nil {
				t.Fatal(err)
			}
			grant := aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"knowledge"}, KnowledgeSources: []aiKnowledgeSourceGrant{{created.Source.ID, created.Source.Version}}}
			input := map[string]any{"provider_id": provider.ID, "message": "read evidence", "workspace": grant}
			if test.ephemeral {
				createdSession := performRequest(router.Engine, "POST", "/api/v1/ai/sessions", []byte(`{"title":"temporary knowledge","persist":false}`), nil)
				var session struct {
					Data aiSessionResponse `json:"data"`
				}
				if createdSession.Code != 201 || json.Unmarshal(createdSession.Body.Bytes(), &session) != nil {
					t.Fatal(createdSession.Body.String())
				}
				input["session_id"] = session.Data.ID
			}
			body, _ := json.Marshal(input)
			response := performRequest(router.Engine, "POST", "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": "knowledge-test"})
			if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("chat %d %s", response.Code, response.Body.String())
			}
			if test.ephemeral {
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
				assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE status='completed' AND content IS NULL", 1)
				if !strings.Contains(response.Body.String(), `"citation_status":"validated"`) || !strings.Contains(response.Body.String(), `"chunk_id":"`+chunk.ID+`"`) {
					t.Fatal("ephemeral stream lost validated citations", response.Body.String())
				}
				recovered := performRequest(router.Engine, "GET", "/api/v1/ai/generations/by-request/knowledge-test", nil, nil)
				if recovered.Code != 200 || !strings.Contains(recovered.Body.String(), `"citation_status":"validated"`) || !strings.Contains(recovered.Body.String(), `"content":""`) || strings.Contains(recovered.Body.String(), "KNOWN TEXT") {
					t.Fatal("ephemeral metadata recovery", recovered.Body.String())
				}
				return
			}
			var message models.AIMessage
			if err := store.DB.Where("role = 'assistant'").First(&message).Error; err != nil {
				t.Fatal(err)
			}
			want := "invalid"
			if readComplete {
				want = "validated"
			}
			if message.CitationsSnapshot == nil || !strings.Contains(*message.CitationsSnapshot, `"status":"`+want+`"`) || strings.Contains(message.Content, "opc:citations") {
				t.Fatalf("citation %+v", message)
			}
			if message.CitationsSnapshot != nil && strings.Contains(*message.CitationsSnapshot, "KNOWN TEXT") {
				t.Fatal("raw body persisted in citation")
			}
			if test.pdf && (!strings.Contains(*message.CitationsSnapshot, `"start_page":3`) || !strings.Contains(*message.CitationsSnapshot, `"source_type":"pdf"`)) {
				t.Fatal("PDF location lost", *message.CitationsSnapshot)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE context_snapshot IS NOT NULL", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE current_json LIKE ?", 0, "%KNOWN TEXT%")
			response = chatRequest(t, router.Engine, provider.ID, message.SessionID, "next without permission")
			if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("next %s", response.Body.String())
			}
			// Version selection is part of retry identity, not a grant extension.
			grant.KnowledgeSources[0].ExpectedSourceVersion++
			body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "message": "read evidence", "workspace": grant})
			if retry := performRequest(router.Engine, "POST", "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": "knowledge-test"}); retry.Code != 409 {
				t.Fatalf("changed idempotency %d", retry.Code)
			}
		})
	}
}

func TestAIKnowledgeEmptyEvidenceCitationStatuses(t *testing.T) {
	for _, tc := range []struct{ text, status string }{{"No sources", "missing"}, {`No evidence[opc:citations]{"chunk_ids":[]}[/opc:citations]`, "no_evidence"}, {`Pretend[opc:citations]{"chunk_ids":["` + uuid.NewString() + `"]}[/opc:citations]`, "invalid"}} {
		_, encoded, snapshot, err := validateAIResponseCitations(tc.text, nil, true)
		if err != nil || encoded == nil || snapshot.Status != tc.status || len(snapshot.Items) != 0 {
			t.Fatalf("%s %+v %v", tc.status, snapshot, err)
		}
	}
}

func TestAIKnowledgeGrantRecheckedInAcceptanceTransaction(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	created := importKnowledgeFixture(t, router.Engine, "race.md", []byte("Evidence"))
	var modelCalls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) { modelCalls.Add(1); streamMockAIDelta(w, "must not run") })
	defer upstream.Close()
	provider := createReadyAIProvider(t, router.Engine, "knowledge-race", upstream.URL+"/v1", "test")
	var current models.AIProvider
	if err := store.DB.First(&current, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	callback := "test:change-source-after-grant-read"
	if err := store.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "knowledge_sources" && reads.Add(1) == 1 {
			if err := store.DB.Exec("UPDATE knowledge_sources SET version=version+1 WHERE id=?", created.Source.ID).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer store.DB.Callback().Query().Remove(callback)
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "query", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"knowledge"}, KnowledgeSources: []aiKnowledgeSourceGrant{{created.Source.ID, created.Source.Version}}}})
	response := performRequest(router.Engine, "POST", "/api/v1/ai/chat", body, nil)
	assertAPIError(t, response, 409, "AI_KNOWLEDGE_GRANT_CHANGED")
	if reads.Load() != 2 || modelCalls.Load() != 0 {
		t.Fatalf("checks=%d model=%d", reads.Load(), modelCalls.Load())
	}
	for _, table := range []string{"ai_sessions", "ai_messages", "ai_generations"} {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM "+table, 0)
	}
}

func TestAIKnowledgeCombinedCapabilitiesFitInitialPrompt(t *testing.T) {
	_, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	grant := &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: aiToolCatalogAllScopes()}
	for i := 0; i < 20; i++ {
		grant.KnowledgeSources = append(grant.KnowledgeSources, aiKnowledgeSourceGrant{uuid.NewString(), 1})
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry(uuid.NewString(), true, &provider, grant, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	// H5-G13 keeps room for real user turns after deferring the large proposal
	// schema. This is a fixed maximum-scope fixture, not a universal guarantee.
	const reservedHeadroom = 32 << 10
	for _, protocol := range []modelclient.Protocol{"openai_chat", "anthropic_messages"} {
		prompt := modelclient.PromptContext{SystemPrompt: workspaceSystemPrompt(grant), Tools: registry.ModelDefinitions()}
		size, err := modelclient.PromptSize(protocol, "test", []modelclient.ChatMessage{{Role: "user", Content: "查询工作台与知识资料"}}, prompt)
		if err != nil || size >= modelclient.MaxPromptBytes {
			t.Fatalf("%s combined prompt %d: %v", protocol, size, err)
		}
		t.Logf("%s combined initial prompt: %d bytes", protocol, size)
		prompt.ActionReceipts = maximumStructuredReceiptFixture(t)
		size, err = modelclient.PromptSize(protocol, "test", []modelclient.ChatMessage{{Role: "user", Content: "继续"}}, prompt)
		if err != nil || size > modelclient.MaxPromptBytes-reservedHeadroom {
			t.Fatalf("%s combined prompt with maximum bounded receipts %d leaves less than %d bytes: %v", protocol, size, reservedHeadroom, err)
		}
		t.Logf("%s combined prompt with receipts: %d bytes", protocol, size)
		prompt.ActionReceipts = strings.Replace(prompt.ActionReceipts, `"as_of":`, `"source_generation_id":"00000000-0000-4000-8000-000000000002","as_of":`, 1)
		size, err = modelclient.PromptSize(protocol, "test", []modelclient.ChatMessage{{Role: "user", Content: "继续"}}, prompt)
		if err != nil || size > modelclient.MaxPromptBytes-reservedHeadroom {
			t.Fatalf("%s combined prompt with selected receipts %d leaves less than %d bytes: %v", protocol, size, reservedHeadroom, err)
		}
		t.Logf("%s combined prompt with selected receipts: %d bytes", protocol, size)
	}
}

func maximumStructuredReceiptFixture(t *testing.T) string {
	t.Helper()
	item := `{"proposal_id":"00000000-0000-4000-8000-000000000001","generation_id":"00000000-0000-4000-8000-000000000002","action":"agent_run.start","status":"confirmed","route":"/tasks/00000000-0000-4000-8000-000000000003?agent_run=00000000-0000-4000-8000-000000000004","target_id":"00000000-0000-4000-8000-000000000003","result_id":"00000000-0000-4000-8000-000000000004","result_version":999,"decided_at":"2026-09-19T01:23:45.123456789Z","agent_run_result":{"id":"00000000-0000-4000-8000-000000000004","task_id":"00000000-0000-4000-8000-000000000003","status":"succeeded","attempt":999,"provider_id":"00000000-0000-4000-8000-000000000005","model":"bounded-model-name","output_delivery_status":"submitted","output_delivery_error_code":null,"submission_id":"00000000-0000-4000-8000-000000000006","artifact_id":"00000000-0000-4000-8000-000000000007"}}`
	items := []string{}
	for len(items) < 16 {
		candidate := `{"as_of":"2026-09-19T01:23:45.123456789Z","limited":true,"items":[` + strings.Join(append(items, item), ",") + `]}`
		if len(candidate) > 8<<10 {
			break
		}
		items = append(items, item)
	}
	result := `{"as_of":"2026-09-19T01:23:45.123456789Z","limited":true,"items":[` + strings.Join(items, ",") + `]}`
	if !json.Valid([]byte(result)) || len(result) < 7<<10 || len(result) > 8<<10 {
		t.Fatalf("structured receipt fixture = %d bytes", len(result))
	}
	return result
}
