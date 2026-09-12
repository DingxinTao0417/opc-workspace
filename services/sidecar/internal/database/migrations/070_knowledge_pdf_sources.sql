-- migration: destructive
-- migration: foreign_keys=off

-- 070: extend the local knowledge base with PDF sources. knowledge_sources is
-- rebuilt to widen the source_type CHECK; knowledge_chunks gains page-range
-- columns so PDF results keep a page-level location while text sources stay
-- on page 1. Extracted text, chunks, FTS rows, and job evidence remain in the
-- same SQLite backup/restore boundary as before.

CREATE TABLE knowledge_sources_v70 (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL
        CHECK (length(trim(name)) BETWEEN 1 AND 255 AND name = trim(name)),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 255 AND title = trim(title)),
    source_type TEXT NOT NULL
        CHECK (source_type IN ('text', 'markdown', 'pdf')),
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

INSERT INTO knowledge_sources_v70 (
    id, name, title, source_type, import_mode, mime_type, size_bytes,
    content_sha256, original_content, status, last_indexed_at, deleted_at,
    delete_reason, version, created_at, updated_at
)
SELECT
    id, name, title, source_type, import_mode, mime_type, size_bytes,
    content_sha256, original_content, status, last_indexed_at, deleted_at,
    delete_reason, version, created_at, updated_at
FROM knowledge_sources;

DROP TABLE knowledge_sources;

ALTER TABLE knowledge_sources_v70 RENAME TO knowledge_sources;

CREATE INDEX idx_knowledge_sources_status_timeline
    ON knowledge_sources(status, updated_at DESC, id DESC);

ALTER TABLE knowledge_chunks ADD COLUMN start_page INTEGER NOT NULL DEFAULT 1
    CHECK (start_page >= 1);
ALTER TABLE knowledge_chunks ADD COLUMN end_page INTEGER NOT NULL DEFAULT 1
    CHECK (end_page >= start_page);
