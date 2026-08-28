ALTER TABLE projects
ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1);

ALTER TABLE projects
ADD COLUMN archived_from_status TEXT
CHECK (
    archived_from_status IS NULL
    OR archived_from_status IN ('planning', 'in_progress', 'paused', 'completed')
);

CREATE INDEX idx_projects_status ON projects(status);
CREATE INDEX idx_projects_due_date ON projects(due_date);
