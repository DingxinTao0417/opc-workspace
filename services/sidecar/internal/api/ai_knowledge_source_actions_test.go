package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

const aiKnowledgeFixtureContent = "# 发布流程\n\n客户发票在确认付款后归档。\n\n本地知识库不会自动上传任何原文。\n"

// newKnowledgeTestAPI fixes Now() to 2026-09-08T12:00:00Z, which the fixture
// below reuses so proposal/receipt timestamps stay deterministic.
func aiKnowledgeFixtureTime() time.Time {
	return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
}

// aiKnowledgeActionFixture builds one saved generation plus one fully indexed
// managed source, so every proposal runs against real domain tables.
func aiKnowledgeActionFixture(t *testing.T, scopes ...string) (*Router, *database.Store, harness.Tool, models.AIGeneration, knowledgeImportResponse) {
	t.Helper()
	router, store := newKnowledgeTestAPI(t)
	source := importKnowledgeFixture(t, router.Engine, "billing-guide.md", []byte(aiKnowledgeFixtureContent))
	now := aiKnowledgeFixtureTime()
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "Knowledge actions", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	if len(scopes) == 0 {
		scopes = []string{"knowledge_actions"}
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: aiKnowledgeFixtureTime}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_propose")
	if !ok {
		t.Fatalf("proposal tool missing for scopes %v", scopes)
	}
	return router, store, tool, generation, source
}

func finishAIGeneration(t *testing.T, store *database.Store, generation models.AIGeneration) {
	t.Helper()
	if err := store.DB.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).
		Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
}

func decideTestAction(t *testing.T, router http.Handler, row models.AIActionProposal, extra string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, row.Fingerprint, extra)
	return performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(body), nil)
}

func decodeTestActionResponse(t *testing.T, response *httptest.ResponseRecorder) aiActionResponse {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	return output.Data
}

func TestAIKnowledgeActionsScopeWorkspaceProposeActionEnum(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	provider := createCompactionTestProvider(t, store, aiKnowledgeFixtureTime(), uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "Knowledge enum", Persist: true, Version: 1,
		CreatedAt: aiKnowledgeFixtureTime().Format(time.RFC3339Nano), UpdatedAt: aiKnowledgeFixtureTime().Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: aiKnowledgeFixtureTime}}
	actionEnum := func(scopes ...string) map[string]bool {
		t.Helper()
		registry, err := service.aiChatToolRegistry(session.ID, true, &provider,
			&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		tool, ok := registry.Get("workspace_propose")
		if !ok {
			t.Fatalf("workspace_propose missing for scopes %v", scopes)
		}
		schemaProvider, ok := tool.(interface{ InputSchema() json.RawMessage })
		if !ok {
			t.Fatal("workspace_propose has no schema")
		}
		var root struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(schemaProvider.InputSchema(), &root); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, action := range root.Properties["action"].Enum {
			out[action] = true
		}
		return out
	}
	contains := func(actions map[string]bool, action string) {
		t.Helper()
		if !actions[action] {
			t.Fatalf("expected %s in action enum: %v", action, actions)
		}
	}
	excludes := func(actions map[string]bool, action string) {
		t.Helper()
		if actions[action] {
			t.Fatalf("did not expect %s in action enum: %v", action, actions)
		}
	}

	knowledgeOnly := actionEnum("knowledge_actions")
	for _, action := range []string{aiKnowledgeCreateAction, aiKnowledgeReindexAction, aiKnowledgeDeleteAction, aiKnowledgeJobRetryAction, aiKnowledgeJobCancelAct} {
		contains(knowledgeOnly, action)
	}
	for _, action := range []string{"task.create", "financial_entry.create", "invoice.create", "agent_run.start"} {
		excludes(knowledgeOnly, action)
	}

	workOnly := actionEnum("work", "actions")
	for _, action := range []string{aiKnowledgeCreateAction, aiKnowledgeReindexAction, aiKnowledgeDeleteAction, aiKnowledgeJobRetryAction, aiKnowledgeJobCancelAct} {
		excludes(workOnly, action)
	}
	contains(workOnly, "task.create")

	combined := actionEnum("work", "actions", "knowledge_actions")
	contains(combined, "task.create")
	contains(combined, aiKnowledgeCreateAction)

	contentOnly, err := service.aiChatToolRegistry(session.ID, true, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"knowledge"},
			KnowledgeSources: []aiKnowledgeSourceGrant{{SourceID: uuid.NewString(), ExpectedSourceVersion: 1}}},
		generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contentOnly.Get("workspace_propose"); ok {
		t.Fatal("knowledge content scope must not register workspace_propose")
	}

	// Exercise the HTTP validation path too: knowledge_actions remains saved
	// conversation only and requires the proposal registry to be usable.
	temporary := models.AISession{ID: uuid.NewString(), Title: "Temporary", Persist: false, Version: 1,
		CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt}
	if err := store.DB.Create(&temporary).Error; err != nil {
		t.Fatal(err)
	}
	ephemeral := performRequest(router.Engine, http.MethodPost, "/api/v1/ai/chat",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"session_id":%q,"message":"x","workspace":{"provider_version":%d,"scopes":["knowledge_actions"]}}`, provider.ID, temporary.ID, provider.Version)), nil)
	assertAPIError(t, ephemeral, http.StatusUnprocessableEntity, "AI_ACTION_PERSIST_REQUIRED")
}

func TestAIKnowledgeCreateStoresUserTextWithFrozenExcerpt(t *testing.T) {
	router, store, tool, generation, _ := aiKnowledgeActionFixture(t)
	content := "# 会议纪要\n\n- 结论：本地知识库只保存受管副本，不会自动上传。\n"
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"knowledge_source.create","changes":{"name":"meeting-notes.md","title":" 会议纪要 ","content":%s}}`, encoded)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"next_status":"indexing"`) ||
		!strings.Contains(row.PreviewJSON, `"source_type":"markdown"`) ||
		!strings.Contains(row.PreviewJSON, `"proposed_content_bytes":`) ||
		!strings.Contains(row.PreviewJSON, `"new_job_operation":"import"`) {
		t.Fatalf("create preview=%s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE name='meeting-notes.md'", 0)

	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router.Engine, row, ""))
	if receipt.ResultID == nil || receipt.ResultVersion == nil || *receipt.ResultVersion != 1 || receipt.Route != "/knowledge" {
		t.Fatalf("create receipt=%#v", receipt)
	}
	sourceID := *receipt.ResultID
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND name='meeting-notes.md' AND title='会议纪要' AND source_type='markdown' AND status='indexing' AND version=1", 1, sourceID)
	var jobID string
	if err := store.DB.Raw("SELECT id FROM knowledge_index_jobs WHERE source_id=? ORDER BY created_at DESC, id DESC LIMIT 1", sourceID).Scan(&jobID).Error; err != nil {
		t.Fatal(err)
	}
	waitKnowledgeJob(t, router.Engine, jobID, "succeeded")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND status='ready' AND version=2", 1, sourceID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_documents WHERE source_id=? AND content_text LIKE '%本地知识库只保存受管副本%'", 1, sourceID)

	// The new source is immediately addressable through the metadata tool.
	options := *tool.(*aiWorkspaceTool)
	options.name = "knowledge_library"
	list, err := options.Execute(context.Background(), []byte(`{"view":"list","limit":10}`))
	if err != nil || !strings.Contains(list, sourceID) || !strings.Contains(list, "meeting-notes.md") {
		t.Fatalf("library after create=%s err=%v", list, err)
	}
}

func TestAIKnowledgeCreateRejectsOversizeUnsupportedAndEmptyText(t *testing.T) {
	_, store, tool, _, _ := aiKnowledgeActionFixture(t)
	for _, test := range []struct {
		label   string
		payload string
	}{
		{"unsupported extension", `{"action":"knowledge_source.create","changes":{"name":"notes.pdf","content":"hello"}}`},
		{"missing name", `{"action":"knowledge_source.create","changes":{"content":"hello"}}`},
		{"empty content", `{"action":"knowledge_source.create","changes":{"name":"notes.md","content":""}}`},
		{"null content", `{"action":"knowledge_source.create","changes":{"name":"notes.md","content":null}}`},
		{"extra field", `{"action":"knowledge_source.create","changes":{"name":"notes.md","content":"hello","source_id":"x"}}`},
		{"target id", `{"action":"knowledge_source.create","knowledge_source_id":"00000000-0000-4000-8000-0000000000a1","expected_version":1,"changes":{"name":"notes.md","content":"hello"}}`},
	} {
		if _, err := tool.Execute(context.Background(), []byte(test.payload)); err == nil {
			t.Fatalf("%s must be rejected", test.label)
		}
	}
	oversize, err := json.Marshal(strings.Repeat("a", maxAIKnowledgeSourceBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"knowledge_source.create","changes":{"name":"notes.md","content":%s}}`, oversize))); err == nil || !strings.Contains(err.Error(), "24000") {
		t.Fatalf("oversize content must be rejected: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources", 1)
}

func TestAIKnowledgeReindexUsesSharedCommandAndRegistersNewJob(t *testing.T) {
	router, store, tool, generation, source := aiKnowledgeActionFixture(t)
	if source.Source.Status != "ready" || source.Source.Version < 2 || source.Source.ChunkCount == 0 {
		t.Fatalf("fixture source = %#v", source.Source)
	}
	args := fmt.Sprintf(`{"action":"knowledge_source.reindex","knowledge_source_id":%q,"expected_version":%d,"changes":{}}`, source.Source.ID, source.Source.Version)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"content_included":false`) ||
		!strings.Contains(row.PreviewJSON, `"next_status":"indexing"`) ||
		strings.Contains(row.PreviewJSON, "发布流程") {
		t.Fatalf("reindex preview leaked or lost facts: %s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND status='ready' AND version=?", 1, source.Source.ID, source.Source.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_index_jobs WHERE source_id=?", 1, source.Source.ID)

	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router.Engine, row, ""))
	if receipt.ResultID == nil {
		t.Fatalf("receipt=%#v", receipt)
	}
	jobID := *receipt.ResultID
	if receipt.Route != "/knowledge" {
		t.Fatalf("route=%s", receipt.Route)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_index_jobs WHERE id=? AND operation='reindex' AND attempt=1 AND retry_of_job_id IS NULL", 1, jobID)
	waitKnowledgeJob(t, router.Engine, jobID, "succeeded")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND status='ready' AND version=?", 1, source.Source.ID, source.Source.Version+2)
}

func TestAIKnowledgeDeleteNeedsSeparateConsentAndDropsIndex(t *testing.T) {
	router, store, tool, generation, source := aiKnowledgeActionFixture(t)
	args := fmt.Sprintf(`{"action":"knowledge_source.delete","knowledge_source_id":%q,"expected_version":%d,"changes":{"reason":" 案例过期 "}}`, source.Source.ID, source.Source.Version)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"chunk_count":1`) || !strings.Contains(row.PreviewJSON, `"next_status":"deleted"`) {
		t.Fatalf("delete preview=%s", row.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	missing := decideTestAction(t, router.Engine, row, "")
	assertAPIError(t, missing, http.StatusUnprocessableEntity, "KNOWLEDGE_SOURCE_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND deleted_at IS NULL", 1, source.Source.ID)

	receipt := decodeTestActionResponse(t, decideTestAction(t, router.Engine, row, `,"confirm_knowledge_source_delete":true`))
	if receipt.Route != "/knowledge" || receipt.ResultVersion == nil || *receipt.ResultVersion != source.Source.Version+1 {
		t.Fatalf("receipt=%#v", receipt)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND status='deleted' AND deleted_at IS NOT NULL AND delete_reason='案例过期' AND original_content IS NULL", 1, source.Source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_documents WHERE source_id=?", 0, source.Source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_chunks WHERE source_id=?", 0, source.Source.ID)

	// A confirmed deletion is durable: replaying the same decision must not fail
	// or delete anything else.
	replay := decideTestAction(t, router.Engine, row, `,"confirm_knowledge_source_delete":true`)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=?", 1, source.Source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE status<>'deleted'", 0)
}

func TestAIKnowledgeDeleteRejectsChangedIndexPreview(t *testing.T) {
	router, store, tool, generation, source := aiKnowledgeActionFixture(t)
	args := fmt.Sprintf(`{"action":"knowledge_source.delete","knowledge_source_id":%q,"expected_version":%d,"changes":{}}`, source.Source.ID, source.Source.Version)
	row := proposeTestAction(t, store, tool, args)
	if err := store.DB.Exec(`INSERT INTO knowledge_chunks(id,document_id,source_id,chunk_index,start_char,end_char,start_line,end_line,content,search_text,content_sha256,index_version,created_at)
SELECT 'late-chunk', document_id, source_id, 99, 0, 5, 1, 1, 'late', 'late', content_sha256, index_version, created_at FROM knowledge_chunks WHERE source_id=? LIMIT 1`, source.Source.ID).Error; err != nil {
		t.Fatal(err)
	}
	finishAIGeneration(t, store, generation)
	changed := decideTestAction(t, router.Engine, row, `,"confirm_knowledge_source_delete":true`)
	assertAPIError(t, changed, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND deleted_at IS NULL", 1, source.Source.ID)
}

func TestAIKnowledgeJobRetryAndCancelShareTheNativeTransactions(t *testing.T) {
	router, store, tool, generation, source := aiKnowledgeActionFixture(t)
	failedAt := "2026-09-08T12:30:00Z"
	failedJob := models.KnowledgeIndexJob{
		ID: uuid.NewString(), SourceID: source.Source.ID, Operation: "reindex", Status: "failed",
		Stage: "complete", Progress: 40, Attempt: 1, ErrorCode: stringPointer("KNOWLEDGE_INDEX_INTERRUPTED"),
		CompletedAt: &failedAt, CreatedAt: "2026-09-08T12:20:00Z",
	}
	if err := store.DB.Create(&failedJob).Error; err != nil {
		t.Fatal(err)
	}
	retryArgs := fmt.Sprintf(`{"action":"knowledge_index_job.retry","knowledge_index_job_id":%q,"expected_version":%d,"changes":{}}`, failedJob.ID, source.Source.Version)
	retryRow := proposeTestAction(t, store, tool, retryArgs)
	if !strings.Contains(retryRow.PreviewJSON, `"attempt":1`) ||
		!strings.Contains(retryRow.PreviewJSON, `"error_code":"KNOWLEDGE_INDEX_INTERRUPTED"`) ||
		!strings.Contains(retryRow.PreviewJSON, `"content_included":false`) {
		t.Fatalf("retry preview=%s", retryRow.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router.Engine, retryRow, ""))
	if receipt.ResultID == nil {
		t.Fatalf("retry receipt=%#v", receipt)
	}
	retryJobID := *receipt.ResultID
	if receipt.Route != "/knowledge" {
		t.Fatalf("retry route=%s", receipt.Route)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_index_jobs WHERE id=? AND attempt=2 AND retry_of_job_id=? AND operation='reindex'", 1, retryJobID, failedJob.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_index_jobs WHERE id=? AND status='failed'", 1, failedJob.ID)
	waitKnowledgeJob(t, router.Engine, retryJobID, "succeeded")
}

func TestAIKnowledgeJobCancelRestoresDurableSourceStatus(t *testing.T) {
	router, store, tool, generation, source := aiKnowledgeActionFixture(t)
	// A queued job can be cancelled through the same shared command; the source
	// keeps its previous durable status because documents still exist.
	current, err := loadKnowledgeSourceRow(store.DB, source.Source.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	queued := models.KnowledgeIndexJob{
		ID: uuid.NewString(), SourceID: source.Source.ID, Operation: "reindex", Status: "queued",
		Stage: "queued", Progress: 0, Attempt: 1, CreatedAt: "2026-09-08T12:40:00Z",
	}
	if err := store.DB.Create(&queued).Error; err != nil {
		t.Fatal(err)
	}
	cancelRow := proposeTestAction(t, store, tool,
		fmt.Sprintf(`{"action":"knowledge_index_job.cancel","knowledge_index_job_id":%q,"expected_version":%d,"changes":{}}`, queued.ID, current.Version))
	if !strings.Contains(cancelRow.PreviewJSON, `"job_status":"queued"`) || !strings.Contains(cancelRow.PreviewJSON, `"next_status":"ready"`) ||
		!strings.Contains(cancelRow.PreviewJSON, `"job_status":"cancelled"`) {
		t.Fatalf("cancel preview=%s", cancelRow.PreviewJSON)
	}
	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router.Engine, cancelRow, ""))
	if receipt.ResultID == nil || *receipt.ResultID != queued.ID || receipt.Route != "/knowledge" {
		t.Fatalf("cancel receipt=%#v", receipt)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_index_jobs WHERE id=? AND status='cancelled' AND cancel_requested=1 AND completed_at IS NOT NULL", 1, queued.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM knowledge_sources WHERE id=? AND status='ready'", 1, source.Source.ID)
}

func TestAIKnowledgeJobActionsRejectTerminalJobsAndExtraChanges(t *testing.T) {
	_, store, tool, _, source := aiKnowledgeActionFixture(t)
	succeededAt := "2026-09-08T12:35:00Z"
	succeeded := models.KnowledgeIndexJob{
		ID: uuid.NewString(), SourceID: source.Source.ID, Operation: "import", Status: "succeeded",
		Stage: "complete", Progress: 100, Attempt: 1, CompletedAt: &succeededAt, CreatedAt: "2026-09-08T12:34:00Z",
	}
	if err := store.DB.Create(&succeeded).Error; err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"action":"knowledge_index_job.retry","knowledge_index_job_id":%q,"expected_version":%d,"changes":{}}`, succeeded.ID, source.Source.Version)
	if _, err := tool.Execute(context.Background(), []byte(payload)); err == nil || !strings.Contains(err.Error(), "failed or cancelled") {
		t.Fatalf("retry of a terminal job must fail: %v", err)
	}
	cancelPayload := fmt.Sprintf(`{"action":"knowledge_index_job.cancel","knowledge_index_job_id":%q,"expected_version":%d,"changes":{}}`, succeeded.ID, source.Source.Version)
	if _, err := tool.Execute(context.Background(), []byte(cancelPayload)); err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Fatalf("cancel of a terminal job must fail: %v", err)
	}
	extra := fmt.Sprintf(`{"action":"knowledge_index_job.cancel","knowledge_index_job_id":%q,"expected_version":1,"changes":{"reason":"x"}}`, succeeded.ID)
	if _, err := tool.Execute(context.Background(), []byte(extra)); err == nil || !strings.Contains(err.Error(), "empty changes") {
		t.Fatalf("index job changes must be empty: %v", err)
	}
	wrongTarget := fmt.Sprintf(`{"action":"knowledge_source.reindex","knowledge_index_job_id":%q,"expected_version":1,"changes":{}}`, succeeded.ID)
	if _, err := tool.Execute(context.Background(), []byte(wrongTarget)); err == nil {
		t.Fatal("source actions must reject knowledge_index_job_id")
	}
	guess := fmt.Sprintf(`{"action":"knowledge_source.reindex","knowledge_source_id":%q,"expected_version":%d,"changes":{}}`, source.Source.ID, source.Source.Version+5)
	if _, err := tool.Execute(context.Background(), []byte(guess)); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale version must be rejected: %v", err)
	}
}

func TestAIKnowledgeLibraryIsMetadataOnlyAndScopeGuarded(t *testing.T) {
	_, store, tool, _, source := aiKnowledgeActionFixture(t)
	options := *tool.(*aiWorkspaceTool)
	options.name = "knowledge_library"
	result, err := options.Execute(context.Background(), []byte(`{"view":"list","limit":5}`))
	if err != nil {
		t.Fatalf("library list: %v", err)
	}
	if !strings.Contains(result, source.Source.ID) || !strings.Contains(result, `"content_included":false`) ||
		strings.Contains(result, "发布流程") || strings.Contains(result, "original_content") ||
		strings.Contains(result, source.Source.ContentSHA256) {
		t.Fatalf("library list leaked protected fields: %s", result)
	}
	detail, err := options.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"job","id":%q}`, source.Job.ID)))
	if err != nil {
		t.Fatalf("library job detail: %v", err)
	}
	if !strings.Contains(detail, `"view":"job"`) || strings.Contains(detail, "发布流程") {
		t.Fatalf("library job detail=%s", detail)
	}

	now := aiKnowledgeFixtureTime()
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "Read only", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: aiKnowledgeFixtureTime}}
	contentOnly, err := service.aiChatToolRegistry(session.ID, true, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version, Scopes: []string{"knowledge"},
		KnowledgeSources: []aiKnowledgeSourceGrant{{SourceID: source.Source.ID, ExpectedSourceVersion: source.Source.Version}},
	}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contentOnly.Get("workspace_propose"); ok {
		t.Fatal("knowledge content consent must not register the proposal tool")
	}
	if _, ok := contentOnly.Get("knowledge_library"); ok {
		t.Fatal("knowledge content consent must not register library management")
	}
	managementOnly, err := service.aiChatToolRegistry(session.ID, true, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"knowledge_actions"}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := managementOnly.Get("knowledge_search"); ok {
		t.Fatal("library management must not grant content search")
	}
	guide, ok := managementOnly.Get("workspace_guide")
	if !ok {
		t.Fatal("library management needs the scoped guide")
	}
	text, err := guide.Execute(context.Background(), []byte(`{"topic":"knowledge"}`))
	if err != nil || !strings.Contains(text, "knowledge_library") {
		t.Fatalf("knowledge guide=%s err=%v", text, err)
	}
	if _, err := guide.Execute(context.Background(), []byte(`{"topic":"finance"}`)); err == nil {
		t.Fatal("knowledge_actions must not expose the finance guide")
	}
}

func TestAIKnowledgeGrantShapeAndEphemeralUseFailClosed(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	provider := createCompactionTestProvider(t, store, aiKnowledgeFixtureTime(), uuid.NewString())
	invalid := performRequest(router.Engine, http.MethodPost, "/api/v1/ai/chat",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"message":"x","workspace":{"provider_version":%d,"scopes":["knowledge_source"]}}`, provider.ID, provider.Version)), nil)
	assertAPIError(t, invalid, http.StatusUnprocessableEntity, "AI_WORKSPACE_GRANT_INVALID")
	duplicate := performRequest(router.Engine, http.MethodPost, "/api/v1/ai/chat",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"message":"x","workspace":{"provider_version":%d,"scopes":["knowledge_actions","knowledge_actions"]}}`, provider.ID, provider.Version)), nil)
	assertAPIError(t, duplicate, http.StatusUnprocessableEntity, "AI_WORKSPACE_GRANT_INVALID")

	now := aiKnowledgeFixtureTime()
	temporary := models.AISession{ID: uuid.NewString(), Title: "Temporary", Persist: false, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&temporary).Error; err != nil {
		t.Fatal(err)
	}
	ephemeral := performRequest(router.Engine, http.MethodPost, "/api/v1/ai/chat",
		[]byte(fmt.Sprintf(`{"provider_id":%q,"session_id":%q,"message":"x","workspace":{"provider_version":%d,"scopes":["knowledge_actions"]}}`, provider.ID, temporary.ID, provider.Version)), nil)
	assertAPIError(t, ephemeral, http.StatusUnprocessableEntity, "AI_ACTION_PERSIST_REQUIRED")
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: aiKnowledgeFixtureTime}}
	if _, err := service.aiChatToolRegistry(temporary.ID, false, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"knowledge_actions"}}, uuid.NewString()); err == nil {
		t.Fatal("ephemeral registry exposed knowledge management metadata")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}
