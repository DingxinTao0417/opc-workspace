-- migration: foreign_keys=off

DROP INDEX IF EXISTS idx_focus_sessions_task_id;
DROP INDEX IF EXISTS idx_focus_sessions_started_at;

ALTER TABLE focus_sessions RENAME TO focus_sessions_v10;

CREATE TABLE focus_sessions (
    id TEXT PRIMARY KEY,
    task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    started_at TEXT NOT NULL CHECK (length(started_at) > 0),
    ended_at TEXT,
    status TEXT NOT NULL
        CHECK (status IN ('planned', 'active', 'paused', 'recovery_pending', 'completed', 'cancelled', 'interrupted')),
    legacy_imported INTEGER NOT NULL DEFAULT 0 CHECK (legacy_imported IN (0, 1)),
    planned_seconds INTEGER NOT NULL CHECK (
        planned_seconds >= 300
        AND (
            planned_seconds <= 7200
            OR (
                legacy_imported = 1
                AND status IN ('completed', 'cancelled', 'interrupted')
            )
        )
    ),
    accumulated_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (accumulated_seconds >= 0 AND accumulated_seconds <= planned_seconds),
    last_resumed_at TEXT,
    last_heartbeat_at TEXT,
    end_reason TEXT
        CHECK (end_reason IS NULL OR end_reason IN ('user_stop', 'completed', 'cancelled', 'crash_recovery')),
    credited_minutes INTEGER NOT NULL DEFAULT 0 CHECK (credited_minutes >= 0),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (legacy_imported = 0 OR status IN ('completed', 'cancelled', 'interrupted')),
    CHECK (
        (status = 'planned' AND ended_at IS NULL AND last_resumed_at IS NULL
            AND end_reason IS NULL AND accumulated_seconds = 0 AND credited_minutes = 0)
        OR (status = 'active' AND ended_at IS NULL AND last_resumed_at IS NOT NULL
            AND last_heartbeat_at IS NOT NULL AND end_reason IS NULL AND credited_minutes = 0)
        OR (status = 'paused' AND ended_at IS NULL AND last_resumed_at IS NULL
            AND end_reason IS NULL AND credited_minutes = 0)
        OR (status = 'recovery_pending' AND ended_at IS NULL AND last_resumed_at IS NOT NULL
            AND last_heartbeat_at IS NOT NULL AND end_reason IS NULL AND credited_minutes = 0)
        OR (status = 'completed' AND ended_at IS NOT NULL AND last_resumed_at IS NULL
            AND end_reason IN ('user_stop', 'completed'))
        OR (status = 'cancelled' AND ended_at IS NOT NULL AND last_resumed_at IS NULL
            AND end_reason = 'cancelled' AND credited_minutes = 0)
        OR (status = 'interrupted' AND ended_at IS NOT NULL AND last_resumed_at IS NULL
            AND end_reason = 'crash_recovery' AND credited_minutes = 0)
    )
);

INSERT INTO focus_sessions (
    id,
    task_id,
    started_at,
    ended_at,
    status,
    legacy_imported,
    planned_seconds,
    accumulated_seconds,
    last_resumed_at,
    last_heartbeat_at,
    end_reason,
    credited_minutes,
    version,
    created_at,
    updated_at
)
SELECT
    id,
    task_id,
    started_at,
    CASE
        WHEN ended_at IS NOT NULL THEN ended_at
        WHEN completed = 1 THEN COALESCE(
            strftime('%Y-%m-%dT%H:%M:%fZ', started_at, '+' || duration_minutes || ' minutes'),
            started_at
        )
        ELSE COALESCE(created_at, started_at)
    END,
    CASE
        WHEN completed = 1 THEN 'completed'
        WHEN ended_at IS NOT NULL THEN 'cancelled'
        ELSE 'interrupted'
    END,
    1,
    CASE
        WHEN duration_minutes * 60 >= 300 THEN duration_minutes * 60
        ELSE 300
    END,
    duration_minutes * 60,
    NULL,
    NULL,
    CASE
        WHEN completed = 1 THEN 'completed'
        WHEN ended_at IS NOT NULL THEN 'cancelled'
        ELSE 'crash_recovery'
    END,
    CASE WHEN completed = 1 AND task_id IS NOT NULL THEN duration_minutes ELSE 0 END,
    1,
    created_at,
    created_at
FROM focus_sessions_v10;

DROP TABLE focus_sessions_v10;

CREATE INDEX idx_focus_sessions_task_id
ON focus_sessions(task_id);

CREATE INDEX idx_focus_sessions_started_at
ON focus_sessions(started_at);

CREATE INDEX idx_focus_sessions_status_started_at
ON focus_sessions(status, started_at DESC, id);

CREATE UNIQUE INDEX idx_focus_sessions_single_open
ON focus_sessions((1))
WHERE status IN ('active', 'paused', 'recovery_pending');

CREATE TABLE focus_session_intervals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES focus_sessions(id) ON DELETE CASCADE,
    started_at TEXT NOT NULL CHECK (length(started_at) > 0),
    ended_at TEXT,
    duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (ended_at IS NOT NULL OR duration_seconds = 0)
);

INSERT INTO focus_session_intervals(
    session_id,
    started_at,
    ended_at,
    duration_seconds,
    created_at
)
SELECT
    id,
    started_at,
    COALESCE(
        strftime('%Y-%m-%dT%H:%M:%fZ', started_at, '+' || accumulated_seconds || ' seconds'),
        started_at
    ),
    accumulated_seconds,
    created_at
FROM focus_sessions;

CREATE INDEX idx_focus_session_intervals_session
ON focus_session_intervals(session_id, started_at, id);

CREATE INDEX idx_focus_session_intervals_time
ON focus_session_intervals(started_at, ended_at);

CREATE UNIQUE INDEX idx_focus_session_intervals_single_open
ON focus_session_intervals((1))
WHERE ended_at IS NULL;

CREATE TABLE task_focus_totals (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    exact_seconds INTEGER NOT NULL DEFAULT 0 CHECK (exact_seconds >= 0),
    applied_minutes INTEGER NOT NULL DEFAULT 0
        CHECK (applied_minutes >= 0 AND applied_minutes <= exact_seconds / 60),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO task_focus_totals(task_id, exact_seconds, applied_minutes, updated_at)
SELECT
    task_id,
    SUM(accumulated_seconds),
    SUM(accumulated_seconds) / 60,
    MAX(updated_at)
FROM focus_sessions
WHERE status = 'completed' AND task_id IS NOT NULL
GROUP BY task_id;
