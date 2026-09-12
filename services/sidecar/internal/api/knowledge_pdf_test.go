package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	gopdf "github.com/signintech/gopdf"
)

func buildKnowledgeTestPDF(t *testing.T, pages [][]string) []byte {
	t.Helper()
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.AddTTFFontData(invoicePDFFontFamily, invoicePDFNotoSansSC); err != nil {
		t.Fatalf("load knowledge test font: %v", err)
	}
	for _, lines := range pages {
		pdf.AddPage()
		for index, line := range lines {
			if err := pdf.SetFont(invoicePDFFontFamily, "", 12); err != nil {
				t.Fatalf("set knowledge test font: %v", err)
			}
			pdf.SetXY(40, 60+float64(index)*24)
			if err := pdf.CellWithOption(&gopdf.Rect{W: 480, H: 20}, line, gopdf.CellOption{Align: gopdf.Left}); err != nil {
				t.Fatalf("draw knowledge test text: %v", err)
			}
		}
	}
	var buffer bytes.Buffer
	if err := pdf.Write(&buffer); err != nil {
		t.Fatalf("render knowledge test PDF: %v", err)
	}
	return buffer.Bytes()
}

func TestKnowledgePDFExtractorMapsPagesAndLines(t *testing.T) {
	content := buildKnowledgeTestPDF(t, [][]string{
		{"第 1 页：本地知识库", "本页第一行内容"},
		nil,
		{"第 3 页：检索验证"},
	})
	extraction, err := extractKnowledgePDF(content)
	if err != nil {
		t.Fatalf("extractKnowledgePDF() error = %v", err)
	}
	if len(extraction.Pages) != 2 {
		t.Fatalf("page entries = %d, want 2 (empty page skipped): %#v", len(extraction.Pages), extraction.Pages)
	}
	first, last := extraction.Pages[0], extraction.Pages[1]
	if first.Number != 1 || first.StartLine != 1 {
		t.Fatalf("first page entry = %#v, want page 1 starting at line 1", first)
	}
	if last.Number != 3 || last.StartLine != first.EndLine+1 {
		t.Fatalf("page 3 entry = %#v, want it to continue after page 1 lines", last)
	}
	for _, phrase := range []string{"第 1 页：本地知识库", "本页第一行内容", "第 3 页：检索验证"} {
		if !strings.Contains(extraction.Text, phrase) {
			t.Fatalf("extracted text missing %q: %q", phrase, extraction.Text)
		}
	}
}

func TestKnowledgePDFExtractorRejectsUnusableSources(t *testing.T) {
	if _, err := extractKnowledgePDF([]byte("this is not a pdf")); err == nil {
		t.Fatalf("garbage bytes accepted")
	} else if code := knowledgeIndexFailureCode(err); code != "KNOWLEDGE_PDF_INVALID" {
		t.Fatalf("garbage bytes code = %q, want KNOWLEDGE_PDF_INVALID", code)
	}
	truncated := buildKnowledgeTestPDF(t, [][]string{{"page"}})[:120]
	if _, err := extractKnowledgePDF(truncated); err == nil {
		t.Fatalf("truncated PDF accepted")
	} else if code := knowledgeIndexFailureCode(err); code != "KNOWLEDGE_PDF_INVALID" {
		t.Fatalf("truncated PDF code = %q, want KNOWLEDGE_PDF_INVALID", code)
	}
	empty := buildKnowledgeTestPDF(t, [][]string{nil, nil})
	if _, err := extractKnowledgePDF(empty); err == nil {
		t.Fatalf("text-less PDF accepted")
	} else if code := knowledgeIndexFailureCode(err); code != "KNOWLEDGE_PDF_NO_TEXT" {
		t.Fatalf("text-less PDF code = %q, want KNOWLEDGE_PDF_NO_TEXT", code)
	}
	if code := knowledgeIndexFailureCode(knowledgePDFExtractionError(errKnowledgePDFTextBudget)); code != "KNOWLEDGE_PDF_TEXT_TOO_LARGE" {
		t.Fatalf("text budget code = %q", code)
	}
	if code := knowledgeIndexFailureCode(knowledgePDFExtractionError(errKnowledgePDFOperationBudget)); code != "KNOWLEDGE_PDF_TOO_COMPLEX" {
		t.Fatalf("operation budget code = %q", code)
	}
}

func TestKnowledgePagesForLines(t *testing.T) {
	pages := []knowledgePDFPageLocation{
		{Number: 1, StartLine: 1, EndLine: 3},
		{Number: 3, StartLine: 4, EndLine: 4},
	}
	cases := []struct {
		startLine, endLine int
		startPage, endPage int
	}{
		{1, 1, 1, 1},
		{2, 2, 1, 1},
		{3, 3, 1, 1},
		{1, 4, 1, 3},
		{4, 4, 3, 3},
		{99, 100, 3, 3},
	}
	for _, testCase := range cases {
		startPage, endPage := knowledgePagesForLines(pages, testCase.startLine, testCase.endLine)
		if startPage != testCase.startPage || endPage != testCase.endPage {
			t.Fatalf("lines %d-%d mapped to pages %d-%d, want %d-%d",
				testCase.startLine, testCase.endLine, startPage, endPage, testCase.startPage, testCase.endPage)
		}
	}
	if startPage, endPage := knowledgePagesForLines(nil, 1, 2); startPage != 1 || endPage != 1 {
		t.Fatalf("text source lines mapped to pages %d-%d, want 1-1", startPage, endPage)
	}
}

func TestKnowledgePDFImportIndexesSearchesWithPageLocations(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	alphaLines := make([]string, 100)
	betaLines := make([]string, 100)
	for index := range alphaLines {
		alphaLines[index] = "Alpha page one local content line"
		betaLines[index] = "Beta page three local content line"
	}
	content := buildKnowledgeTestPDF(t, [][]string{alphaLines, nil, betaLines})
	response := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", []multipartTestPart{
		{field: "file", filename: "manual.pdf", content: content},
	}, nil, false)
	if response.Code != http.StatusAccepted {
		t.Fatalf("import knowledge PDF = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode knowledge PDF import: %v", err)
	}
	if envelope.Data.Source.SourceType != "pdf" || envelope.Data.Source.MimeType != "application/pdf" {
		t.Fatalf("pdf source type=%q mime=%q", envelope.Data.Source.SourceType, envelope.Data.Source.MimeType)
	}
	job := waitKnowledgeJob(t, router.Engine, envelope.Data.Job.ID, "succeeded")
	if job.ErrorCode != nil {
		t.Fatalf("pdf job failed: %#v", job)
	}
	var extractorVersion string
	if err := store.DB.Table("knowledge_documents").
		Where("source_id = ?", envelope.Data.Source.ID).
		Pluck("extractor_version", &extractorVersion).Error; err != nil || extractorVersion != knowledgePDFExtractorVersion {
		t.Fatalf("pdf extractor version=%q err=%v", extractorVersion, err)
	}

	search := func(query string) []knowledgeSearchResult {
		t.Helper()
		response := performRequest(router.Engine, http.MethodPost, "/api/v1/knowledge/search", []byte(`{"query":"`+query+`"}`), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("search %q = %d: %s", query, response.Code, response.Body.String())
		}
		var searchEnvelope struct {
			Data []knowledgeSearchResult `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &searchEnvelope); err != nil {
			t.Fatalf("decode search %q: %v", query, err)
		}
		if len(searchEnvelope.Data) == 0 {
			t.Fatalf("search %q returned no results", query)
		}
		return searchEnvelope.Data
	}
	alpha := search("Alpha")
	for _, result := range alpha {
		if result.SourceType != "pdf" || result.StartPage != 1 {
			t.Fatalf("alpha result must start on page 1: %#v", result)
		}
	}
	if alpha[0].StartLine < 1 {
		t.Fatalf("alpha result lacks line location: %#v", alpha[0])
	}
	hasPageOneChunk := false
	for _, result := range alpha {
		if result.EndPage == 1 {
			hasPageOneChunk = true
		}
	}
	if !hasPageOneChunk {
		t.Fatalf("no alpha chunk fully inside page 1: %#v", alpha)
	}
	beta := search("Beta")
	for _, result := range beta {
		if result.EndPage != 3 {
			t.Fatalf("beta result must end on page 3: %#v", result)
		}
	}
	hasPageThreeChunk := false
	for _, result := range beta {
		if result.StartPage == 3 {
			hasPageThreeChunk = true
		}
	}
	if !hasPageThreeChunk {
		t.Fatalf("no beta chunk fully inside page 3: %#v", beta)
	}

	textImport := importKnowledgeFixture(t, router.Engine, "notes.txt", []byte("plain text knowledge source"))
	var textChunk models.KnowledgeChunk
	if err := store.DB.First(&textChunk, "source_id = ?", textImport.Source.ID).Error; err != nil {
		t.Fatalf("load text chunk: %v", err)
	}
	if textChunk.StartPage != 1 || textChunk.EndPage != 1 {
		t.Fatalf("text chunk pages=%d-%d, want 1-1", textChunk.StartPage, textChunk.EndPage)
	}

	selection := []aiKnowledgeContextSourceInput{{
		SourceID: envelope.Data.Source.ID, DocumentID: alpha[0].DocumentID, ChunkID: alpha[0].ChunkID,
	}}
	knowledge, err := buildAIKnowledgeContext(context.Background(), store.DB, selection, false)
	if err != nil {
		t.Fatalf("build knowledge context: %v", err)
	}
	if len(knowledge) != 1 || knowledge[0].StartPage != 1 || knowledge[0].EndPage != 1 || knowledge[0].SourceType != "pdf" {
		t.Fatalf("knowledge context payload pages/type wrong: %#v", knowledge)
	}
}

func TestKnowledgePDFImportFailsWithStableCode(t *testing.T) {
	router, store := newKnowledgeTestAPI(t)
	content := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("not a real pdf body"), 10)...)
	response := performMultipartPartsRequest(router.Engine, "/api/v1/knowledge/sources", []multipartTestPart{
		{field: "file", filename: "broken.pdf", content: content},
	}, nil, false)
	if response.Code != http.StatusAccepted {
		t.Fatalf("import broken PDF = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data knowledgeImportResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode broken PDF import: %v", err)
	}
	job := waitKnowledgeJob(t, router.Engine, envelope.Data.Job.ID, "failed")
	if job.ErrorCode == nil || *job.ErrorCode != "KNOWLEDGE_PDF_INVALID" {
		t.Fatalf("broken PDF job error=%v, want KNOWLEDGE_PDF_INVALID", job.ErrorCode)
	}
	var status string
	if err := store.DB.Table("knowledge_sources").
		Where("id = ?", envelope.Data.Source.ID).
		Pluck("status", &status).Error; err != nil || status != "failed" {
		t.Fatalf("broken PDF source status=%q err=%v", status, err)
	}
}
