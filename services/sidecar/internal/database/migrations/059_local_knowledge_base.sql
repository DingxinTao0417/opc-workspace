-- 059: local knowledge-base baseline. TXT/Markdown sources are copied into
-- SQLite so the original bytes, extracted text, chunks, FTS rows, and job
-- evidence share one transactional backup/restore boundary.
CREATE TABLE knowledge_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
        CHECK (length(trim(name)) BETWEEN 1 AND 255 AND name = trim(name)),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 255 AND title = trim(title)),
    source_type TEXT NOT NULL
        CHECK (source_type IN ('text', 'markdown')),
    import_mode TEXT NOT NULL DEFAULT 'managed_copy'
        CHECK (import_mode = 'managed_copy'),
    mime_type TEXT NOT NULL
        CHECK (length(trim(mime_type)) BETWEEN 1 AND 255 AND mime_type = trim(mime_type)),
    size_bytes INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 16777216),
    content_sha256 TEXT NOT NULL
        CHECK (length(content_sha256) = 64 AND content_sha256 = lower(content_sha256)),
    original_content BLOB,
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'indexing', 'ready', 'stale', 'missing', 'failed', 'deleted')),
    last_indexed_at TEXT,
    deleted_at TEXT,
    delete_reason TEXT,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        (status = 'deleted' AND original_content IS NULL AND deleted_at IS NOT NULL)
        OR (status <> 'deleted' AND original_content IS NOT NULL AND deleted_at IS NULL AND delete_reason IS NULL)
    )
);

CREATE INDEX idx_knowledge_sources_status_timeline
    ON knowledge_sources(status, updated_at DESC, id DESC);

CREATE TABLE knowledge_documents (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL UNIQUE REFERENCES knowledge_sources(id) ON DELETE CASCADE,
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 255 AND title = trim(title)),
    language TEXT NOT NULL DEFAULT 'und'
        CHECK (length(language) BETWEEN 2 AND 16),
    extractor_version TEXT NOT NULL
        CHECK (length(trim(extractor_version)) BETWEEN 1 AND 64 AND extractor_version = trim(extractor_version)),
    content_text TEXT NOT NULL CHECK (length(content_text) BETWEEN 1 AND 16777216),
    content_sha256 TEXT NOT NULL
        CHECK (length(content_sha256) = 64 AND content_sha256 = lower(content_sha256)),
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status = 'ready'),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0)
);

CREATE INDEX idx_knowledge_documents_source
    ON knowledge_documents(source_id, version DESC);

CREATE TABLE knowledge_chunks (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
    start_char INTEGER NOT NULL CHECK (start_char >= 0),
    end_char INTEGER NOT NULL CHECK (end_char > start_char),
    start_line INTEGER NOT NULL CHECK (start_line >= 1),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 4096),
    search_text TEXT NOT NULL CHECK (length(search_text) BETWEEN 1 AND 16384),
    content_sha256 TEXT NOT NULL
        CHECK (length(content_sha256) = 64 AND content_sha256 = lower(content_sha256)),
    index_version INTEGER NOT NULL CHECK (index_version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    UNIQUE(document_id, chunk_index)
);

CREATE INDEX idx_knowledge_chunks_source_order
    ON knowledge_chunks(source_id, document_id, chunk_index);

CREATE TABLE knowledge_index_jobs (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
    operation TEXT NOT NULL CHECK (operation IN ('import', 'reindex')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    stage TEXT NOT NULL CHECK (stage IN ('queued', 'extracting', 'chunking', 'indexing', 'complete')),
    progress INTEGER NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    retry_of_job_id TEXT REFERENCES knowledge_index_jobs(id) ON DELETE SET NULL,
    error_code TEXT,
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    CHECK (
        (status IN ('queued', 'running') AND completed_at IS NULL)
        OR (status IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
    )
);

CREATE INDEX idx_knowledge_index_jobs_source_timeline
    ON knowledge_index_jobs(source_id, created_at DESC, id DESC);

CREATE VIRTUAL TABLE knowledge_chunks_fts USING fts5(
    content,
    search_text,
    chunk_id UNINDEXED,
    document_id UNINDEXED,
    source_id UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER trg_knowledge_chunks_fts_insert
AFTER INSERT ON knowledge_chunks
BEGIN
    INSERT INTO knowledge_chunks_fts(content, search_text, chunk_id, document_id, source_id)
    VALUES (NEW.content, NEW.search_text, NEW.id, NEW.document_id, NEW.source_id);
END;

CREATE TRIGGER trg_knowledge_chunks_fts_update
AFTER UPDATE ON knowledge_chunks
BEGIN
    DELETE FROM knowledge_chunks_fts WHERE chunk_id = OLD.id;
    INSERT INTO knowledge_chunks_fts(content, search_text, chunk_id, document_id, source_id)
    VALUES (NEW.content, NEW.search_text, NEW.id, NEW.document_id, NEW.source_id);
END;

CREATE TRIGGER trg_knowledge_chunks_fts_delete
AFTER DELETE ON knowledge_chunks
BEGIN
    DELETE FROM knowledge_chunks_fts WHERE chunk_id = OLD.id;
END;
