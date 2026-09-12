package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newKnowledgeTestAPI(t *testing.T) (*Router, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "knowledge-api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "knowledge-test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), KeyStore: keystore.NewMemoryStore(),
		Now: func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router, store
}

func importKnowledgeFixture(t *testing.T, router http.Handler, name string, content []byte) knowledgeImportResponse {
	t.Helper()
	response := performMultipartPartsRequest(router, "/api/v1/knowledge/sources", []multipartTestPart{
		{field: "file", filename: name, content: content},
	}, nil, false)
	if response.Code != http.StatusAccepted {
		t.Fatalf("import knowledge source = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode knowledge import: %v: %s", err, response.Body.String())
	}
	if envelope.Data.Source.Status != "indexing" || envelope.Data.Job.Status != "queued" || envelope.Data.Source.Version != 1 ||
		envelope.Data.Source.LatestJob == nil || envelope.Data.Source.LatestJob.ID != envelope.Data.Job.ID {
		t.Fatalf("queued knowledge import = %#v", envelope.Data)
	}
	envelope.Data.Job = waitKnowledgeJob(t, router, envelope.Data.Job.ID, "succeeded")
	detail := performRequest(router, http.MethodGet, "/api/v1/knowledge/sources/"+envelope.Data.Source.ID, nil, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("load indexed source = %d: %s", detail.Code, detail.Body.String())
	}
	var sourceEnvelope struct {
		Data knowledgeSourceResponse `json:"data"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &sourceEnvelope); err != nil {
		t.Fatalf("decode indexed source: %v: %s", err, detail.Body.String())
	}
	envelope.Data.Source = sourceEnvelope.Data
	return envelope.Data
}

func waitKnowledgeJob(t *testing.T, router http.Handler, jobID, wantStatus string) models.KnowledgeIndexJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := performRequest(router, http.MethodGet, "/api/v1/knowledge/index-jobs/"+jobID, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("load knowledge job = %d: %s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data models.KnowledgeIndexJob `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode knowledge job: %v: %s", err, response.Body.String())
		}
		if envelope.Data.Status == wantStatus {
			return envelope.Data
		}
		if envelope.Data.Status == "failed" || envelope.Data.Status == "cancelled" {
			t.Fatalf("knowledge job reached %s, want %s: %#v", envelope.Data.Status, wantStatus, envelope.Data)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("knowledge job %s did not reach %s", jobID, wantStatus)
	return models.KnowledgeIndexJob{}
}

func TestKnowledgeImportSearchReindexAndDeleteLifecycle(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	content := []byte("# 发布流程\n\n客户发票在确认付款后归档。\n\n本地知识库不会自动上传任何原文。\n")
	created := importKnowledgeFixture(t, router.Engine, "billing-guide.md", content)
	if created.Source.Name != "billing-guide.md" || created.Source.SourceType != "markdown" ||
		created.Source.ImportMode != "managed_copy" || created.Source.Status != "ready" ||
		created.Source.Version != 2 || created.Source.DocumentID == nil || created.Source.ChunkCount != 1 ||
		created.Job.Status != "succeeded" || created.Job.Operation != "import" || created.Source.LatestJob == nil ||
		created.Source.LatestJob.Status != "succeeded" {
		t.Fatalf("created knowledge source = %#v", created)
	}

	listed := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources", nil, nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "客户发票在确认付款后归档") {
		t.Fatalf("source list = %d: %s", listed.Code, listed.Body.String())
	}
	unconfirmedExport := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/export.csv", nil, nil)
	assertAPIError(t, unconfirmedExport, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED")
	exported := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/export.csv?confirm=true", nil, nil)
	if exported.Code != http.StatusOK || !strings.Contains(exported.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(exported.Body.String(), "billing-guide.md") || !strings.Contains(exported.Body.String(), "succeeded") ||
		strings.Contains(exported.Body.String(), "客户发票在确认付款后归档") {
		t.Fatalf("knowledge source export = %d headers=%v body=%s", exported.Code, exported.Header(), exported.Body.String())
	}

	search := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"客户 发票"}`), nil)
	if search.Code != http.StatusOK {
		t.Fatalf("knowledge search = %d: %s", search.Code, search.Body.String())
	}
	var searchEnvelope struct {
		Data []knowledgeSearchResult `json:"data"`
	}
	if err := json.Unmarshal(search.Body.Bytes(), &searchEnvelope); err != nil || len(searchEnvelope.Data) != 1 {
		t.Fatalf("decode search err=%v body=%s", err, search.Body.String())
	}
	result := searchEnvelope.Data[0]
	if result.SourceID != created.Source.ID || result.DocumentVersion != 1 || result.StartLine != 1 ||
		!strings.Contains(result.Excerpt, "客户发票") || len(result.Highlights) == 0 {
		t.Fatalf("search result = %#v", result)
	}

	filtered := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"客户","source_ids":["018f0000-0000-7000-8000-000000000999"]}`), nil)
	if filtered.Code != http.StatusOK || !jsonListDataIsEmpty(t, filtered.Body.Bytes()) {
		t.Fatalf("filtered search = %d: %s", filtered.Code, filtered.Body.String())
	}

	detail := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/documents/"+*created.Source.DocumentID, nil, nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "本地知识库不会自动上传任何原文") || detail.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("document detail = %d headers=%v body=%s", detail.Code, detail.Header(), detail.Body.String())
	}

	reindexed := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/sources/"+created.Source.ID+"/reindex", nil, map[string]string{"If-Match": `"2"`})
	if reindexed.Code != http.StatusAccepted || reindexed.Header().Get("ETag") != `"3"` {
		t.Fatalf("reindex = %d headers=%v body=%s", reindexed.Code, reindexed.Header(), reindexed.Body.String())
	}
	var reindexEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(reindexed.Body.Bytes(), &reindexEnvelope); err != nil ||
		reindexEnvelope.Data.Source.Version != 3 || reindexEnvelope.Data.Source.Status != "indexing" ||
		reindexEnvelope.Data.Job.Status != "queued" {
		t.Fatalf("reindex response err=%v data=%#v", err, reindexEnvelope.Data)
	}
	waitKnowledgeJob(t, router.Engine, reindexEnvelope.Data.Job.ID, "succeeded")
	indexedDetail := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/"+created.Source.ID, nil, nil)
	var indexedEnvelope struct {
		Data knowledgeSourceResponse `json:"data"`
	}
	if err := json.Unmarshal(indexedDetail.Body.Bytes(), &indexedEnvelope); err != nil || indexedEnvelope.Data.Version != 4 ||
		indexedEnvelope.Data.DocumentVersion == nil || *indexedEnvelope.Data.DocumentVersion != 2 {
		t.Fatalf("indexed source err=%v body=%s", err, indexedDetail.Body.String())
	}
	stale := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/sources/"+created.Source.ID+"/reindex", nil, map[string]string{"If-Match": `"2"`})
	assertAPIError(t, stale, http.StatusConflict, "VERSION_CONFLICT")

	deleted := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge/sources/"+created.Source.ID+"?confirm=true", nil, map[string]string{"If-Match": `"4"`})
	if deleted.Code != http.StatusOK || deleted.Header().Get("ETag") != `"5"` {
		t.Fatalf("delete source = %d headers=%v body=%s", deleted.Code, deleted.Header(), deleted.Body.String())
	}
	for _, table := range []string{"knowledge_documents", "knowledge_chunks", "knowledge_chunks_fts"} {
		var count int64
		if err := store.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s rows after delete=%d err=%v", table, count, err)
		}
	}
	var originalBytes []byte
	var status string
	if err := store.SQL.QueryRow("SELECT original_content, status FROM knowledge_sources WHERE id = ?", created.Source.ID).Scan(&originalBytes, &status); err != nil || originalBytes != nil || status != "deleted" {
		t.Fatalf("deleted source content=%v status=%q err=%v", originalBytes, status, err)
	}
	emptySearch := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"客户"}`), nil)
	if emptySearch.Code != http.StatusOK || !jsonListDataIsEmpty(t, emptySearch.Body.Bytes()) {
		t.Fatalf("search after delete = %d: %s", emptySearch.Code, emptySearch.Body.String())
	}
}

func TestKnowledgeImportRejectsUnsafeOrInvalidSources(t *testing.T) {
	router, _ := newKnowledgeTestAPI(t)
	if got := cleanKnowledgeFilename(`C:\\private\\notes.md`); got != "notes.md" {
		t.Fatalf("cleanKnowledgeFilename() = %q", got)
	}

	for _, test := range []struct {
		name     string
		filename string
		content  []byte
		status   int
		code     string
	}{
		{name: "unsupported extension", filename: "notes.docx", content: []byte("docx"), status: http.StatusUnsupportedMediaType, code: "KNOWLEDGE_FORMAT_UNSUPPORTED"},
		{name: "pdf without magic bytes", filename: "notes.pdf", content: []byte("PDF"), status: http.StatusUnprocessableEntity, code: "KNOWLEDGE_PDF_INVALID"},
		{name: "invalid utf8", filename: "notes.txt", content: []byte{0xff, 0xfe}, status: http.StatusUnprocessableEntity, code: "KNOWLEDGE_TEXT_INVALID"},
		{name: "null byte", filename: "notes.md", content: []byte("safe\x00unsafe"), status: http.StatusUnprocessableEntity, code: "KNOWLEDGE_TEXT_INVALID"},
		{name: "blank", filename: "notes.txt", content: []byte("  \n\t"), status: http.StatusUnprocessableEntity, code: "KNOWLEDGE_EMPTY_SOURCE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", []multipartTestPart{
				{field: "file", filename: test.filename, content: test.content},
			}, nil, true)
			assertAPIError(t, response, test.status, test.code)
		})
	}

	injection := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"\" OR *"}`), nil)
	if injection.Code != http.StatusOK || !jsonListDataIsEmpty(t, injection.Body.Bytes()) {
		t.Fatalf("escaped FTS query = %d: %s", injection.Code, injection.Body.String())
	}
}

func TestKnowledgeImportIdempotentReplayDoesNotDuplicateSourceOrJob(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	parts := []multipartTestPart{
		{field: "file", filename: "replay.md", content: []byte("# Replay\nlocal source")},
	}
	headers := map[string]string{"Idempotency-Key": "knowledge-import-replay-1"}
	first := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", parts, headers, false)
	second := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", parts, headers, false)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted || second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("idempotent import first=%d second=%d headers=%v body=%s", first.Code, second.Code, second.Header(), second.Body.String())
	}
	var firstEnvelope, secondEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstEnvelope); err != nil {
		t.Fatalf("decode first import: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondEnvelope); err != nil {
		t.Fatalf("decode replay import: %v", err)
	}
	if firstEnvelope.Data.Source.ID != secondEnvelope.Data.Source.ID || firstEnvelope.Data.Job.ID != secondEnvelope.Data.Job.ID {
		t.Fatalf("replay identities first=%#v second=%#v", firstEnvelope.Data, secondEnvelope.Data)
	}
	waitKnowledgeJob(t, router.Engine, firstEnvelope.Data.Job.ID, "succeeded")
	for table, want := range map[string]int64{"knowledge_sources": 1, "knowledge_documents": 1, "knowledge_index_jobs": 1} {
		var count int64
		if err := store.DB.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
		}
	}
	conflict := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", []multipartTestPart{
		{field: "file", filename: "replay.md", content: []byte("different source")},
	}, headers, false)
	assertAPIError(t, conflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
}

func TestKnowledgeDocumentDeleteReindexAndFullPurge(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	first := importKnowledgeFixture(t, router.Engine, "first.txt", []byte("recoverable local reference"))
	second := importKnowledgeFixture(t, router.Engine, "second.md", []byte("# Second\nseparate source"))

	missingConfirmation := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge/documents/"+*first.Source.DocumentID, nil, map[string]string{"If-Match": `"2"`})
	assertAPIError(t, missingConfirmation, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED")
	deleted := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge/documents/"+*first.Source.DocumentID+"?confirm=true", nil, map[string]string{"If-Match": `"2"`})
	if deleted.Code != http.StatusOK || deleted.Header().Get("ETag") != `"3"` {
		t.Fatalf("delete document = %d headers=%v body=%s", deleted.Code, deleted.Header(), deleted.Body.String())
	}
	rebuilt := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/sources/"+first.Source.ID+"/reindex", nil, map[string]string{"If-Match": `"3"`})
	if rebuilt.Code != http.StatusAccepted || rebuilt.Header().Get("ETag") != `"4"` {
		t.Fatalf("rebuild deleted document = %d headers=%v body=%s", rebuilt.Code, rebuilt.Header(), rebuilt.Body.String())
	}
	var rebuiltEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(rebuilt.Body.Bytes(), &rebuiltEnvelope); err != nil {
		t.Fatalf("decode rebuilt job: %v", err)
	}
	waitKnowledgeJob(t, router.Engine, rebuiltEnvelope.Data.Job.ID, "succeeded")

	terminalCancel := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/index-jobs/"+second.Job.ID+"/cancel", nil, nil)
	assertAPIError(t, terminalCancel, http.StatusConflict, "KNOWLEDGE_JOB_TERMINAL")

	missingPurgeConfirmation := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge?confirm=true", nil, nil)
	assertAPIError(t, missingPurgeConfirmation, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED")
	purged := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge?confirm=true", nil, map[string]string{"X-Knowledge-Confirmation": "DELETE KNOWLEDGE BASE"})
	if purged.Code != http.StatusOK {
		t.Fatalf("purge knowledge = %d: %s", purged.Code, purged.Body.String())
	}
	for _, table := range []string{"knowledge_sources", "knowledge_documents", "knowledge_chunks", "knowledge_chunks_fts", "knowledge_index_jobs"} {
		var count int64
		if err := store.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("%s rows after purge=%d err=%v", table, count, err)
		}
	}
}

func TestKnowledgeIndexActorCancelsRunningJobAndRetriesWithNewAttempt(t *testing.T) {
	router, _ := newKnowledgeTestAPI(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var blockFirst sync.Once
	router.knowledgeIndexer.stageHook = func(_ string, stage string) {
		if stage != "extracting" {
			return
		}
		blockFirst.Do(func() {
			close(started)
			<-release
		})
	}

	queued := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", []multipartTestPart{
		{field: "file", filename: "cancel-me.txt", content: []byte("a local source that can be retried")},
	}, nil, false)
	if queued.Code != http.StatusAccepted {
		t.Fatalf("queue import = %d: %s", queued.Code, queued.Body.String())
	}
	var queuedEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &queuedEnvelope); err != nil {
		t.Fatalf("decode queued import: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("knowledge actor did not start queued job")
	}
	running := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/index-jobs/"+queuedEnvelope.Data.Job.ID, nil, nil)
	if running.Code != http.StatusOK || !strings.Contains(running.Body.String(), `"status":"running"`) {
		t.Fatalf("running job = %d: %s", running.Code, running.Body.String())
	}
	cancelled := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/index-jobs/"+queuedEnvelope.Data.Job.ID+"/cancel", nil, nil)
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel running job = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	close(release)

	sourceAfterCancel := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/"+queuedEnvelope.Data.Source.ID, nil, nil)
	var cancelledSource struct {
		Data knowledgeSourceResponse `json:"data"`
	}
	if err := json.Unmarshal(sourceAfterCancel.Body.Bytes(), &cancelledSource); err != nil ||
		cancelledSource.Data.Status != "failed" || cancelledSource.Data.Version != 2 {
		t.Fatalf("source after cancel err=%v body=%s", err, sourceAfterCancel.Body.String())
	}

	retry := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/index-jobs/"+queuedEnvelope.Data.Job.ID+"/retry", nil, map[string]string{"If-Match": `"2"`})
	if retry.Code != http.StatusAccepted || retry.Header().Get("ETag") != `"3"` {
		t.Fatalf("retry cancelled job = %d headers=%v body=%s", retry.Code, retry.Header(), retry.Body.String())
	}
	var retryEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &retryEnvelope); err != nil || retryEnvelope.Data.Job.Attempt != 2 ||
		retryEnvelope.Data.Job.RetryOfJobID == nil || *retryEnvelope.Data.Job.RetryOfJobID != queuedEnvelope.Data.Job.ID {
		t.Fatalf("retry job err=%v data=%#v", err, retryEnvelope.Data)
	}
	waitKnowledgeJob(t, router.Engine, retryEnvelope.Data.Job.ID, "succeeded")
	ready := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/"+queuedEnvelope.Data.Source.ID, nil, nil)
	if ready.Code != http.StatusOK || ready.Header().Get("ETag") != `"4"` || !strings.Contains(ready.Body.String(), `"status":"ready"`) {
		t.Fatalf("retried source = %d headers=%v body=%s", ready.Code, ready.Header(), ready.Body.String())
	}
}

func TestKnowledgeReindexKeepsOldVersionSearchableUntilAtomicPublish(t *testing.T) {
	router, _ := newKnowledgeTestAPI(t)
	created := importKnowledgeFixture(t, router.Engine, "stable.txt", []byte("stable searchable content"))
	started := make(chan struct{})
	release := make(chan struct{})
	var block sync.Once
	router.knowledgeIndexer.stageHook = func(_ string, stage string) {
		if stage == "extracting" {
			block.Do(func() {
				close(started)
				<-release
			})
		}
	}

	queued := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/sources/"+created.Source.ID+"/reindex", nil, map[string]string{"If-Match": `"2"`})
	if queued.Code != http.StatusAccepted || queued.Header().Get("ETag") != `"3"` {
		t.Fatalf("queue reindex = %d headers=%v body=%s", queued.Code, queued.Header(), queued.Body.String())
	}
	var queuedEnvelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &queuedEnvelope); err != nil {
		t.Fatalf("decode queued reindex: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("reindex actor did not start")
	}

	search := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"stable"}`), nil)
	if search.Code != http.StatusOK || jsonListDataIsEmpty(t, search.Body.Bytes()) || !strings.Contains(search.Body.String(), `"document_version":1`) {
		t.Fatalf("search while reindexing = %d: %s", search.Code, search.Body.String())
	}
	cancelled := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/index-jobs/"+queuedEnvelope.Data.Job.ID+"/cancel", nil, nil)
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel reindex = %d: %s", cancelled.Code, cancelled.Body.String())
	}
	close(release)
	ready := performRequest(router.Engine, http.MethodGet, "/api/v1/knowledge/sources/"+created.Source.ID, nil, nil)
	if ready.Code != http.StatusOK || ready.Header().Get("ETag") != `"4"` ||
		!strings.Contains(ready.Body.String(), `"status":"ready"`) || !strings.Contains(ready.Body.String(), `"document_version":1`) {
		t.Fatalf("source after cancelled reindex = %d headers=%v body=%s", ready.Code, ready.Header(), ready.Body.String())
	}
}

func TestKnowledgeIndexRecoveryFailsInterruptedJobsWithoutPublishingPartialIndex(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "knowledge-recovery.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	defer store.Close()
	now := "2026-09-08T11:00:00Z"
	source := models.KnowledgeSource{
		ID: "018f0000-0000-7000-8000-000000009101", Name: "interrupted.txt", Title: "Interrupted",
		SourceType: "text", ImportMode: "managed_copy", MimeType: "text/plain", SizeBytes: 15,
		ContentSHA256: knowledgeSHA256([]byte("pending content")), OriginalContent: []byte("pending content"),
		Status: "indexing", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	job := models.KnowledgeIndexJob{
		ID: "018f0000-0000-7000-8000-000000009102", SourceID: source.ID, Operation: "import",
		Status: "running", Stage: "chunking", Progress: 40, Attempt: 1, StartedAt: &now, CreatedAt: now,
	}
	if err := store.DB.Create(&source).Error; err != nil {
		t.Fatalf("seed interrupted source: %v", err)
	}
	if err := store.DB.Create(&job).Error; err != nil {
		t.Fatalf("seed interrupted job: %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "knowledge-recovery", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), KeyStore: keystore.NewMemoryStore(),
		Now: func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	defer router.Close()

	if err := store.DB.First(&job, "id = ?", job.ID).Error; err != nil || job.Status != "failed" ||
		job.ErrorCode == nil || *job.ErrorCode != "KNOWLEDGE_INDEX_INTERRUPTED" || job.CompletedAt == nil {
		t.Fatalf("recovered job=%#v err=%v", job, err)
	}
	if err := store.DB.First(&source, "id = ?", source.ID).Error; err != nil || source.Status != "failed" || source.Version != 2 {
		t.Fatalf("recovered source=%#v err=%v", source, err)
	}
	var documentCount, chunkCount, ftsCount int64
	for table, destination := range map[string]*int64{
		"knowledge_documents":  &documentCount,
		"knowledge_chunks":     &chunkCount,
		"knowledge_chunks_fts": &ftsCount,
	} {
		if err := store.DB.Table(table).Count(destination).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
	}
	if documentCount != 0 || chunkCount != 0 || ftsCount != 0 {
		t.Fatalf("partial index survived recovery: documents=%d chunks=%d fts=%d", documentCount, chunkCount, ftsCount)
	}
}
