CREATE TABLE content_items (
    id TEXT PRIMARY KEY
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND id NOT GLOB '*[^0-9a-f-]*'
        ),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 1 AND 200 AND title = trim(title)),
    platform TEXT NOT NULL
        CHECK (length(trim(platform)) BETWEEN 1 AND 64 AND platform = trim(platform)),
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'in_review', 'scheduled', 'published', 'cancelled', 'archived')),
    scheduled_at TEXT,
    scheduled_timezone TEXT,
    published_at TEXT,
    project_id TEXT REFERENCES projects(id) ON DELETE RESTRICT,
    notes TEXT
        CHECK (notes IS NULL OR length(trim(notes)) BETWEEN 1 AND 4000),
    external_link TEXT
        CHECK (external_link IS NULL OR length(trim(external_link)) BETWEEN 1 AND 2048),
    manual_order INTEGER NOT NULL DEFAULT 0,
    archived_from_status TEXT
        CHECK (archived_from_status IS NULL OR archived_from_status IN ('draft', 'in_review', 'scheduled', 'published', 'cancelled')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK ((scheduled_at IS NULL AND scheduled_timezone IS NULL) OR (scheduled_at IS NOT NULL AND scheduled_timezone IS NOT NULL)),
    CHECK ((status = 'published' AND published_at IS NOT NULL) OR (status <> 'published' AND published_at IS NULL)),
    CHECK ((status = 'archived' AND archived_from_status IS NOT NULL) OR (status <> 'archived' AND archived_from_status IS NULL))
);

CREATE TABLE content_item_tasks (
    content_item_id TEXT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    is_required INTEGER NOT NULL DEFAULT 1 CHECK (is_required IN (0, 1)),
    linked_at TEXT NOT NULL CHECK (length(linked_at) > 0),
    PRIMARY KEY (content_item_id, task_id)
);

CREATE INDEX idx_content_items_schedule
ON content_items(scheduled_at, manual_order, id)
WHERE status <> 'archived';

CREATE INDEX idx_content_items_project_schedule
ON content_items(project_id, scheduled_at, manual_order, id)
WHERE status <> 'archived';

CREATE INDEX idx_content_item_tasks_task
ON content_item_tasks(task_id, content_item_id);

CREATE TRIGGER content_items_version_step
BEFORE UPDATE ON content_items
FOR EACH ROW
WHEN NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_VERSION_INVALID');
END;

CREATE TRIGGER content_items_identity_immutable
BEFORE UPDATE OF id, created_at ON content_items
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'CONTENT_ITEM_IDENTITY_IMMUTABLE');
END;
