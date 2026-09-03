-- 055: ai_providers.kind — additive provider classification (ADR-005).
-- 'remote' (default) keeps the existing API-key behavior; 'local' marks an
-- OpenAI-compatible endpoint on the loopback interface that never stores a
-- key. SQLite accepts ADD COLUMN with a constant default and a CHECK.
ALTER TABLE ai_providers ADD COLUMN kind TEXT NOT NULL DEFAULT 'remote'
    CHECK (kind IN ('remote', 'local'));
