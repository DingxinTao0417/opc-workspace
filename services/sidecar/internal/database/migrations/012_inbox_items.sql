CREATE TABLE inbox_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'manual'
        CHECK (kind IN ('manual', 'event', 'reminder')),
    title TEXT NOT NULL
        CHECK (length(trim(title)) BETWEEN 2 AND 200 AND title = trim(title)),
    summary TEXT NOT NULL DEFAULT ''
        CHECK (length(summary) <= 10000),
    source_entity_type TEXT NOT NULL DEFAULT 'manual'
        CHECK (length(trim(source_entity_type)) BETWEEN 1 AND 50),
    source_entity_id TEXT
        CHECK (source_entity_id IS NULL OR length(trim(source_entity_id)) > 0),
    source_event_key TEXT
        CHECK (source_event_key IS NULL OR length(trim(source_event_key)) BETWEEN 1 AND 500),
    source_deleted_at TEXT
        CHECK (source_deleted_at IS NULL OR length(source_deleted_at) > 0),
    priority TEXT NOT NULL DEFAULT 'P2'
        CHECK (priority IN ('P0', 'P1', 'P2', 'P3')),
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'tracking', 'resolved', 'dismissed')),
    resolution_policy TEXT NOT NULL DEFAULT 'manual'
        CHECK (resolution_policy IN ('manual', 'all_required_tasks_done')),
    due_at TEXT CHECK (due_at IS NULL OR length(due_at) > 0),
    read_at TEXT CHECK (read_at IS NULL OR length(read_at) > 0),
    triaged_at TEXT CHECK (triaged_at IS NULL OR length(triaged_at) > 0),
    snoozed_until TEXT CHECK (snoozed_until IS NULL OR length(snoozed_until) > 0),
    resolved_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    resolved_at TEXT CHECK (resolved_at IS NULL OR length(resolved_at) > 0),
    resolution_reason TEXT
        CHECK (resolution_reason IS NULL OR length(trim(resolution_reason)) BETWEEN 1 AND 2000),
    resolution_mode TEXT
        CHECK (resolution_mode IS NULL OR resolution_mode IN ('manual', 'forced', 'automatic')),
    dismissed_by_actor_id TEXT REFERENCES actors(id) ON DELETE RESTRICT,
    dismissed_at TEXT CHECK (dismissed_at IS NULL OR length(dismissed_at) > 0),
    dismiss_reason TEXT
        CHECK (dismiss_reason IS NULL OR length(trim(dismiss_reason)) BETWEEN 1 AND 2000),
    payload_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP CHECK (length(updated_at) > 0),
    CHECK (
        source_entity_type <> 'manual'
        OR (
            kind = 'manual'
            AND source_entity_id IS NULL
            AND source_event_key IS NULL
            AND source_deleted_at IS NULL
        )
    ),
    CHECK (
        (
            status = 'resolved'
            AND triaged_at IS NOT NULL
            AND resolved_by_actor_id IS NOT NULL
            AND resolved_at IS NOT NULL
            AND resolution_reason IS NOT NULL
            AND resolution_mode IS NOT NULL
            AND dismissed_by_actor_id IS NULL
            AND dismissed_at IS NULL
            AND dismiss_reason IS NULL
        )
        OR (
            status = 'dismissed'
            AND triaged_at IS NOT NULL
            AND dismissed_by_actor_id IS NOT NULL
            AND dismissed_at IS NOT NULL
            AND dismiss_reason IS NOT NULL
            AND resolved_by_actor_id IS NULL
            AND resolved_at IS NULL
            AND resolution_reason IS NULL
            AND resolution_mode IS NULL
        )
        OR (
            status IN ('open', 'tracking')
            AND resolved_by_actor_id IS NULL
            AND resolved_at IS NULL
            AND resolution_reason IS NULL
            AND resolution_mode IS NULL
            AND dismissed_by_actor_id IS NULL
            AND dismissed_at IS NULL
            AND dismiss_reason IS NULL
        )
    )
);

CREATE UNIQUE INDEX ux_inbox_items_source_event_key
ON inbox_items(source_event_key)
WHERE source_event_key IS NOT NULL;

CREATE INDEX idx_inbox_items_status_snoozed
ON inbox_items(status, snoozed_until);

CREATE INDEX idx_inbox_items_priority_due
ON inbox_items(priority, due_at);

CREATE INDEX idx_inbox_items_created_at
ON inbox_items(created_at, id);

CREATE INDEX idx_inbox_items_unread
ON inbox_items(read_at, created_at);
