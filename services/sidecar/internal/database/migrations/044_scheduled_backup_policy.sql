CREATE TABLE scheduled_backup_policy (
    singleton INTEGER PRIMARY KEY
        CHECK (singleton = 1),
    enabled INTEGER NOT NULL DEFAULT 0
        CHECK (enabled IN (0, 1)),
    local_time TEXT NOT NULL DEFAULT '02:00'
        CHECK (
            length(local_time) = 5
            AND substr(local_time, 3, 1) = ':'
            AND CAST(substr(local_time, 1, 2) AS INTEGER) BETWEEN 0 AND 23
            AND CAST(substr(local_time, 4, 2) AS INTEGER) BETWEEN 0 AND 59
        ),
    timezone TEXT NOT NULL DEFAULT 'UTC'
        CHECK (length(timezone) BETWEEN 1 AND 100),
    retention_count INTEGER NOT NULL DEFAULT 30
        CHECK (retention_count BETWEEN 1 AND 365),
    last_attempted_date TEXT,
    last_attempt_at TEXT,
    last_success_at TEXT,
    last_backup_id TEXT,
    last_status TEXT NOT NULL DEFAULT 'idle'
        CHECK (last_status IN ('idle', 'succeeded', 'failed')),
    last_error_code TEXT,
    version INTEGER NOT NULL DEFAULT 1
        CHECK (version >= 1),
    updated_at TEXT NOT NULL
);

INSERT INTO scheduled_backup_policy (
    singleton,
    enabled,
    local_time,
    timezone,
    retention_count,
    last_status,
    version,
    updated_at
) VALUES (1, 0, '02:00', 'UTC', 30, 'idle', 1, CURRENT_TIMESTAMP);
