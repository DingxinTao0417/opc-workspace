package database

import (
	"path/filepath"
	"testing"
)

func TestKnowledgeBaseMigrationCreatesTransactionalFTSIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	for _, table := range []string{
		"knowledge_sources",
		"knowledge_documents",
		"knowledge_chunks",
		"knowledge_index_jobs",
		"knowledge_chunks_fts",
	} {
		var count int
		if err := store.SQL.QueryRow(
			"SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?",
			table,
		).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}

	const (
		sourceID   = "018f0000-0000-7000-8000-000000000901"
		documentID = "018f0000-0000-7000-8000-000000000902"
		chunkID    = "018f0000-0000-7000-8000-000000000903"
		now        = "2026-09-08T12:00:00Z"
		sha        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	if err := store.DB.Exec(`
		INSERT INTO knowledge_sources(
			id, name, title, source_type, mime_type, size_bytes, content_sha256,
			original_content, status, last_indexed_at, created_at, updated_at
		) VALUES (?, 'guide.md', 'Guide', 'markdown', 'text/markdown', 18, ?, ?, 'ready', ?, ?, ?)
	`, sourceID, sha, []byte("local source body"), now, now, now).Error; err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO knowledge_documents(
			id, source_id, title, extractor_version, content_text, content_sha256,
			created_at, updated_at
		) VALUES (?, ?, 'guide.md', 'plain-text-v1', 'Local search phrase', ?, ?, ?)
	`, documentID, sourceID, sha, now, now).Error; err != nil {
		t.Fatalf("insert document: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO knowledge_chunks(
			id, document_id, source_id, chunk_index, start_char, end_char,
			start_line, end_line, content, search_text, content_sha256, index_version, created_at
		) VALUES (?, ?, ?, 0, 0, 19, 1, 1, 'Local search phrase', 'Local search phrase', ?, 1, ?)
	`, chunkID, documentID, sourceID, sha, now).Error; err != nil {
		t.Fatalf("insert chunk: %v", err)
	}

	var indexedID string
	if err := store.SQL.QueryRow(`
		SELECT chunk_id FROM knowledge_chunks_fts
		WHERE knowledge_chunks_fts MATCH 'search'
	`).Scan(&indexedID); err != nil || indexedID != chunkID {
		t.Fatalf("FTS lookup id=%q err=%v", indexedID, err)
	}
	if err := store.DB.Exec("DELETE FROM knowledge_documents WHERE id = ?", documentID).Error; err != nil {
		t.Fatalf("delete document: %v", err)
	}
	var indexedCount int
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM knowledge_chunks_fts").Scan(&indexedCount); err != nil || indexedCount != 0 {
		t.Fatalf("FTS rows after cascade=%d err=%v", indexedCount, err)
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
