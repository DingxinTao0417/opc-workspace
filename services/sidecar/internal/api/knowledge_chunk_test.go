package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func knowledgeChunkTestURL(chunk knowledgeChunk) string {
	query := url.Values{
		"source_id": {chunk.SourceID}, "document_id": {chunk.DocumentID},
		"source_version": {fmt.Sprint(chunk.SourceVersion)}, "document_version": {fmt.Sprint(chunk.DocumentVersion)},
	}
	return "/api/v1/knowledge/chunks/" + chunk.ChunkID + "?" + query.Encode()
}

func TestKnowledgeChunkExactReadLifecycle(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	created := importKnowledgeFixture(t, router.Engine, "guide.md", []byte(strings.Repeat("本地🧭 <script>不执行</script>\n", 120)+"PRIVATE FULL DOCUMENT TAIL"))
	var first models.KnowledgeChunk
	if err := store.DB.Order("chunk_index").First(&first, "source_id = ?", created.Source.ID).Error; err != nil {
		t.Fatal(err)
	}
	selected, err := buildAIKnowledgeContext(context.Background(), store.DB, []aiKnowledgeContextSourceInput{{
		SourceID: created.Source.ID, DocumentID: first.DocumentID, ChunkID: first.ID,
		ExpectedSourceVersion: created.Source.Version, ExpectedDocumentVersion: *created.Source.DocumentVersion,
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	target := knowledgeChunkTestURL(selected[0])
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, target, nil, map[string]string{"Authorization": ""}), http.StatusUnauthorized, "UNAUTHORIZED")
	response := performRequest(router.Engine, http.MethodGet, target, nil, nil)
	var body struct {
		Data knowledgeChunk `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || !reflect.DeepEqual(body.Data, selected[0]) {
		t.Fatalf("exact read = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "PRIVATE FULL DOCUMENT TAIL") || len([]rune(body.Data.Content)) > knowledgeChunkRunes {
		t.Fatalf("unbounded/cached read: %s", response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
	for _, changed := range []knowledgeChunk{
		{ChunkID: first.ID, SourceID: uuid.NewString(), DocumentID: first.DocumentID, SourceVersion: 2, DocumentVersion: 1},
		{ChunkID: first.ID, SourceID: created.Source.ID, DocumentID: uuid.NewString(), SourceVersion: 2, DocumentVersion: 1},
	} {
		assertAPIError(t, performRequest(router.Engine, http.MethodGet, knowledgeChunkTestURL(changed), nil, nil), http.StatusNotFound, "KNOWLEDGE_CHUNK_NOT_FOUND")
	}
	for _, fragment := range []string{"&source_id=" + created.Source.ID, "&path=C:/private.txt", "&document_version=1"} {
		assertAPIError(t, performRequest(router.Engine, http.MethodGet, target+fragment, nil, nil), http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION")
	}
	for _, version := range []string{"0", "-1", "1.1", "01", "9223372036854775808", ""} {
		bad := strings.Replace(target, "source_version=2", "source_version="+version, 1)
		assertAPIError(t, performRequest(router.Engine, http.MethodGet, bad, nil, nil), http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION")
	}
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, strings.Split(target, "?")[0], nil, nil), http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION")
	for _, field := range []string{"source_version=2", "document_version=1"} {
		bad := strings.Replace(target, field, strings.Split(field, "=")[0]+"=99", 1)
		failure := performRequest(router.Engine, http.MethodGet, bad, nil, nil)
		assertAPIError(t, failure, http.StatusConflict, "KNOWLEDGE_LOCATION_CHANGED")
		if strings.Contains(failure.Body.String(), "不执行") {
			t.Fatal("error leaked source text")
		}
	}

	reindex := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/sources/"+created.Source.ID+"/reindex", nil, map[string]string{"If-Match": `"2"`})
	var indexed struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if reindex.Code != http.StatusAccepted || json.Unmarshal(reindex.Body.Bytes(), &indexed) != nil {
		t.Fatalf("reindex: %s", reindex.Body.String())
	}
	waitKnowledgeJob(t, router.Engine, indexed.Data.Job.ID, "succeeded")
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, target, nil, nil), http.StatusNotFound, "KNOWLEDGE_CHUNK_NOT_FOUND")
	var replacement models.KnowledgeChunk
	if err := store.DB.Order("chunk_index").First(&replacement, "source_id = ?", created.Source.ID).Error; err != nil {
		t.Fatal(err)
	}
	current, err := readKnowledgeChunk(context.Background(), store.DB, replacement.SourceID, replacement.DocumentID, replacement.ID)
	if err != nil {
		t.Fatal(err)
	}
	deleted := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge/sources/"+created.Source.ID+"?confirm=true", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, current.SourceVersion)})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, knowledgeChunkTestURL(current), nil, nil), http.StatusNotFound, "KNOWLEDGE_CHUNK_NOT_FOUND")
}

func TestKnowledgePDFCitationHistoryRemainsReadableAfterSourceDeletion(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	created := importKnowledgeFixture(t, router.Engine, "evidence.pdf", buildKnowledgeTestPDF(t, [][]string{{"PDF 第一页"}, nil, {"PDF 第三页"}}))
	var chunk models.KnowledgeChunk
	if err := store.DB.First(&chunk, "source_id = ?", created.Source.ID).Error; err != nil {
		t.Fatal(err)
	}
	evidence, err := readKnowledgeChunk(context.Background(), store.DB, chunk.SourceID, chunk.DocumentID, chunk.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot, _, err := validateAIResponseCitations(`[opc:citations]{"chunk_ids":["`+chunk.ID+`"]}[/opc:citations]`, []aiKnowledgeContextSource{evidence})
	if err != nil {
		t.Fatal(err)
	}
	now := "2026-09-08T12:00:00Z"
	session := models.AISession{ID: uuid.NewString(), Title: "PDF history", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	message := models.AIMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed", Content: "根据 PDF 回答", CitationsSnapshot: snapshot, CreatedAt: now, UpdatedAt: now}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if remove {
			deleted := performRequest(router.Engine, http.MethodDelete, "/api/v1/knowledge/sources/"+created.Source.ID+"?confirm=true", nil, map[string]string{"If-Match": `"2"`})
			if deleted.Code != http.StatusOK {
				t.Fatalf("delete: %s", deleted.Body.String())
			}
		}
		history := performRequest(router.Engine, http.MethodGet, "/api/v1/ai/sessions/"+session.ID+"/messages", nil, nil)
		if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"source_type":"pdf"`) || !strings.Contains(history.Body.String(), `"end_page":3`) {
			t.Fatalf("PDF history (deleted=%v): %d %s", remove, history.Code, history.Body.String())
		}
		read := performRequest(router.Engine, http.MethodGet, knowledgeChunkTestURL(evidence), nil, nil)
		if remove {
			assertAPIError(t, read, http.StatusNotFound, "KNOWLEDGE_CHUNK_NOT_FOUND")
		} else if read.Code != http.StatusOK {
			t.Fatalf("PDF chunk: %s", read.Body.String())
		}
	}
}
