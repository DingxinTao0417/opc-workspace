CREATE TABLE roadmap_milestones (
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
    description TEXT
        CHECK (description IS NULL OR length(trim(description)) BETWEEN 1 AND 4000),
    year INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 2100),
    quarter INTEGER NOT NULL CHECK (quarter BETWEEN 1 AND 4),
    target_date TEXT NOT NULL
        CHECK (
            length(target_date) = 10
            AND date(target_date) = target_date
            AND CAST(substr(target_date, 1, 4) AS INTEGER) = year
            AND CAST(substr(target_date, 6, 2) AS INTEGER) BETWEEN (quarter - 1) * 3 + 1 AND quarter * 3
        ),
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'active', 'achieved', 'archived')),
    manual_order INTEGER NOT NULL DEFAULT 0,
    archived_from_status TEXT
        CHECK (archived_from_status IS NULL OR archived_from_status IN ('planned', 'active', 'achieved')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TEXT NOT NULL CHECK (length(created_at) > 0),
    updated_at TEXT NOT NULL CHECK (length(updated_at) > 0),
    CHECK (
        (status = 'archived' AND archived_from_status IS NOT NULL)
        OR (status <> 'archived' AND archived_from_status IS NULL)
    )
);

CREATE TABLE roadmap_milestone_projects (
    milestone_id TEXT NOT NULL
        REFERENCES roadmap_milestones(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL
        REFERENCES projects(id) ON DELETE RESTRICT,
    linked_at TEXT NOT NULL CHECK (length(linked_at) > 0),
    PRIMARY KEY (milestone_id, project_id)
);

CREATE INDEX idx_roadmap_milestones_period
ON roadmap_milestones(year, quarter, manual_order, target_date, id)
WHERE status <> 'archived';

CREATE INDEX idx_roadmap_milestones_archived_period
ON roadmap_milestones(status, year, quarter, manual_order, target_date, id);

CREATE INDEX idx_roadmap_milestone_projects_project
ON roadmap_milestone_projects(project_id, milestone_id);

CREATE TRIGGER roadmap_milestones_version_step
BEFORE UPDATE ON roadmap_milestones
FOR EACH ROW
WHEN NEW.version <> OLD.version + 1
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_VERSION_INVALID');
END;

CREATE TRIGGER roadmap_milestones_identity_immutable
BEFORE UPDATE OF id, created_at ON roadmap_milestones
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'ROADMAP_MILESTONE_IDENTITY_IMMUTABLE');
END;
