CREATE TABLE task_saved_views (
    id TEXT PRIMARY KEY
        CHECK (length(id) = 36),
    name TEXT NOT NULL
        CHECK (length(trim(name)) BETWEEN 1 AND 80),
    definition_json TEXT NOT NULL
        CHECK (length(definition_json) BETWEEN 2 AND 16384)
        CHECK (json_valid(definition_json)),
    schema_version INTEGER NOT NULL DEFAULT 1
        CHECK (schema_version = 1),
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
        CHECK (length(updated_at) > 0)
);

CREATE UNIQUE INDEX idx_task_saved_views_name
ON task_saved_views(lower(name));

CREATE INDEX idx_task_saved_views_updated
ON task_saved_views(updated_at DESC, id ASC);
