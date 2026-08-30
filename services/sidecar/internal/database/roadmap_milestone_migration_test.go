package database

import (
	"path/filepath"
	"testing"
)

func TestRoadmapMilestoneMigrationCreatesQuarterScopedProjectLinks(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v35-to-v36.db")
	v35 := openDatabaseAtVersion(t, databasePath, 35)
	if err := v35.Close(); err != nil {
		t.Fatalf("close v35 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v35 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 42 {
		t.Fatalf("SchemaVersion = %d, want 42", store.SchemaVersion)
	}

	const projectID = "018f0000-0000-7000-8000-000000003601"
	const milestoneID = "018f0000-0000-7000-8000-000000003602"
	if _, err := store.SQL.Exec(`
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Roadmap Project', 'planning', 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO roadmap_milestones(
			id, title, description, year, quarter, target_date, status, manual_order, version, created_at, updated_at
		) VALUES (?, 'Q4 launch', 'Launch scope', 2026, 4, '2026-12-15', 'active', 20, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, milestoneID); err != nil {
		t.Fatalf("insert roadmap milestone: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO roadmap_milestone_projects(milestone_id, project_id, linked_at)
		VALUES (?, ?, '2026-08-29T08:00:00Z')
	`, milestoneID, projectID); err != nil {
		t.Fatalf("link roadmap project: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO roadmap_milestones(
			id, title, year, quarter, target_date, status, manual_order, version, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000003603', 'Wrong quarter', 2026, 4, '2026-09-30', 'planned', 0, 1, '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`); err == nil {
		t.Fatal("target date outside the selected quarter unexpectedly accepted")
	}
	if _, err := store.SQL.Exec(`DELETE FROM projects WHERE id = ?`, projectID); err == nil {
		t.Fatal("project with roadmap link unexpectedly deleted")
	}
	if _, err := store.SQL.Exec(`
		UPDATE roadmap_milestones
		SET status = 'archived', archived_from_status = 'active', version = version + 1, updated_at = '2026-09-01T08:00:00Z'
		WHERE id = ?
	`, milestoneID); err != nil {
		t.Fatalf("archive roadmap milestone: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM roadmap_milestones WHERE id = ?", milestoneID); got != 2 {
		t.Fatalf("milestone version after archive = %d, want 2", got)
	}
	if _, err := store.SQL.Exec(`DELETE FROM roadmap_milestones WHERE id = ?`, milestoneID); err != nil {
		t.Fatalf("delete milestone with cascade link: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM roadmap_milestone_projects WHERE milestone_id = ?", milestoneID); got != 0 {
		t.Fatalf("cascaded roadmap project links = %d, want 0", got)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
