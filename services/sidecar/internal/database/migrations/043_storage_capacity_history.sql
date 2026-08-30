CREATE TABLE storage_capacity_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL
        CHECK (scope IN (
            'database',
            'artifacts',
            'backups',
            'database+artifacts',
            'database+backups',
            'artifacts+backups',
            'database+artifacts+backups'
        )),
    sample_bucket INTEGER NOT NULL
        CHECK (sample_bucket >= 0),
    available_bytes INTEGER NOT NULL
        CHECK (available_bytes >= 0),
    total_bytes INTEGER NOT NULL
        CHECK (total_bytes > 0 AND available_bytes <= total_bytes),
    threshold_bytes INTEGER NOT NULL
        CHECK (threshold_bytes > 0),
    status TEXT NOT NULL
        CHECK (status IN ('healthy', 'low')),
    checked_at TEXT NOT NULL
        CHECK (length(checked_at) > 0),
    UNIQUE (scope, sample_bucket)
);

CREATE INDEX idx_storage_capacity_samples_time
ON storage_capacity_samples(checked_at DESC, scope);

