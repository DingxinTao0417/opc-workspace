package database

import (
	"path/filepath"
	"testing"
)

func TestKnowledgePDFMigrationWidensSourcesAndAddsChunkPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge-pdf.db")
	v69 := openDatabaseAtVersion(t, path, 69)
	const (
		sourceID      = "018f0000-0000-7000-8000-000000000951"
		pdfSourceID   = "018f0000-0000-7000-8000-000000000952"
		documentID    = "018f0000-0000-7000-8000-000000000953"
		chunkID       = "018f0000-0000-7000-8000-000000000954"
		now           = "2026-09-11T12:00:00Z"
		sha           = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		searchPhrase  = "preserved search phrase"
		searchTextSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	if _, err := v69.Exec(`
		INSERT INTO knowledge_sources(
			id, name, title, source_type, mime_type, size_bytes, content_sha256,
			original_content, status, last_indexed_at, created_at, updated_at
		) VALUES (?, 'guide.md', 'Guide', 'markdown', 'text/markdown', 18, ?, ?, 'ready', ?, ?, ?)
	`, sourceID, sha, []byte("local source body"), now, now, now); err != nil {
		t.Fatalf("seed v69 knowledge source: %v", err)
	}
	if _, err := v69.Exec(`
		INSERT INTO knowledge_documents(
			id, source_id, title, extractor_version, content_text, content_sha256,
			created_at, updated_at
		) VALUES (?, ?, 'guide.md', 'plain-text-v1', ?, ?, ?, ?)
	`, documentID, sourceID, searchPhrase, sha, now, now); err != nil {
		t.Fatalf("seed v69 knowledge document: %v", err)
	}
	if _, err := v69.Exec(`
		INSERT INTO knowledge_chunks(
			id, document_id, source_id, chunk_index, start_char, end_char,
			start_line, end_line, content, search_text, content_sha256, index_version, created_at
		) VALUES (?, ?, ?, 0, 0, 22, 1, 1, ?, ?, ?, 1, ?)
	`, chunkID, documentID, sourceID, searchPhrase, searchPhrase, searchTextSHA, now); err != nil {
		t.Fatalf("seed v69 knowledge chunk: %v", err)
	}
	if err := v69.Close(); err != nil {
		t.Fatalf("close v69 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 70 {
		t.Fatalf("SchemaVersion=%d, want 70", store.SchemaVersion)
	}

	var preservedType, preservedStatus string
	var preservedBody []byte
	if err := store.SQL.QueryRow(
		"SELECT source_type, status, original_content FROM knowledge_sources WHERE id = ?", sourceID,
	).Scan(&preservedType, &preservedStatus, &preservedBody); err != nil {
		t.Fatalf("read preserved source: %v", err)
	}
	if preservedType != "markdown" || preservedStatus != "ready" || string(preservedBody) != "local source body" {
		t.Fatalf("preserved source type=%q status=%q body=%q", preservedType, preservedStatus, preservedBody)
	}

	var startPage, endPage int
	if err := store.SQL.QueryRow(
		"SELECT start_page, end_page FROM knowledge_chunks WHERE id = ?", chunkID,
	).Scan(&startPage, &endPage); err != nil {
		t.Fatalf("read preserved chunk pages: %v", err)
	}
	if startPage != 1 || endPage != 1 {
		t.Fatalf("preserved chunk pages=%d/%d, want 1/1", startPage, endPage)
	}

	if _, err := store.SQL.Exec(`
		INSERT INTO knowledge_sources(
			id, name, title, source_type, mime_type, size_bytes, content_sha256,
			original_content, status, created_at, updated_at
		) VALUES (?, 'manual.pdf', 'Manual', 'pdf', 'application/pdf', 12, ?, ?, 'pending', ?, ?)
	`, pdfSourceID, sha, []byte("%PDF-1.7 real"), now, now); err != nil {
		t.Fatalf("insert pdf source: %v", err)
	}

	var indexedID string
	if err := store.SQL.QueryRow(`
		SELECT chunk_id FROM knowledge_chunks_fts WHERE knowledge_chunks_fts MATCH 'search'
	`).Scan(&indexedID); err != nil || indexedID != chunkID {
		t.Fatalf("FTS lookup after migration id=%q err=%v", indexedID, err)
	}
	if _, err := store.SQL.Exec(
		"INSERT INTO knowledge_chunks"+
			"(id, document_id, source_id, chunk_index, start_char, end_char, start_line, end_line,"+
			" start_page, end_page, content, search_text, content_sha256, index_version, created_at)"+
			" VALUES ('018f0000-0000-7000-8000-000000000955', ?, ?, 1, 40, 60, 3, 4, 2, 3,"+
			" 'Manual page text', 'Manual page text', ?, 1, ?)",
		documentID, sourceID, searchTextSHA, now,
	); err != nil {
		t.Fatalf("insert chunk with page range: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO knowledge_chunks(
			id, document_id, source_id, chunk_index, start_char, end_char,
			start_line, end_line, start_page, end_page, content, search_text,
			content_sha256, index_version, created_at
		) VALUES ('018f0000-0000-7000-8000-000000000956', ?, ?, 2, 60, 80, 5, 6,
			3, 2, 'inverted page range', 'inverted page range', ?, 1, ?)
	`, documentID, sourceID, searchTextSHA, now); err == nil {
		t.Fatalf("inverted page range accepted")
	}

	var foreignKeyViolations int
	rows, err := store.SQL.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		foreignKeyViolations++
	}
	if foreignKeyViolations != 0 {
		t.Fatalf("foreign key violations=%d", foreignKeyViolations)
	}
}
