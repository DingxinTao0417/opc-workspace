package database

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestTaskParentProgressMigrationAddsSubmissionOriginWithoutDestructiveGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v29-task-parent-progress.db")
	v29 := openDatabaseAtVersion(t, path, 29)

	const (
		legacyTaskID       = "018f0000-0000-7000-8000-000000003001"
		legacySubmissionID = "018f0000-0000-7000-8000-000000003002"
		rollupTaskID       = "018f0000-0000-7000-8000-000000003003"
		rollupSubmissionID = "018f0000-0000-7000-8000-000000003004"
		manualTaskID       = "018f0000-0000-7000-8000-000000003005"
		manualSubmissionID = "018f0000-0000-7000-8000-000000003006"
		manualArtifactID   = "018f0000-0000-7000-8000-000000003007"
		rollupArtifactID   = "018f0000-0000-7000-8000-000000003008"
	)

	insertWorkflowTask(t, v29, legacyTaskID, "v29 manual submission", "manual")
	insertSubmission(t, v29, legacySubmissionID, legacyTaskID, 1)
	if err := v29.Close(); err != nil {
		t.Fatalf("close v29 database: %v", err)
	}

	store, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatalf("apply non-destructive v30 migration: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 39 || gate == nil || gate.CurrentVersion != 39 || gate.TargetVersion != 46 || !reflect.DeepEqual(gate.PendingVersions, []int{40, 41, 42, 43, 44, 45, 46}) {
		t.Fatalf("v29 to latest migration store=%d gate=%#v", store.SchemaVersion, gate)
	}

	var legacyOrigin string
	if err := store.SQL.QueryRow("SELECT origin FROM task_submissions WHERE id = ?", legacySubmissionID).Scan(&legacyOrigin); err != nil {
		t.Fatalf("read legacy submission origin: %v", err)
	}
	if legacyOrigin != "manual" {
		t.Fatalf("legacy submission origin = %q, want manual", legacyOrigin)
	}

	insertWorkflowTask(t, store.SQL, manualTaskID, "gorm manual submission", "manual")
	manualSubmission := models.TaskSubmission{
		ID: manualSubmissionID, TaskID: manualTaskID, Sequence: 1, Status: "pending_review",
		SubmittedByActorID: builtinOwnerActorID, SubmittedAt: "2026-08-29T10:00:00Z",
	}
	if err := store.DB.Create(&manualSubmission).Error; err != nil {
		t.Fatalf("create manual submission through GORM default: %v", err)
	}
	if manualSubmission.Origin != "manual" {
		t.Fatalf("GORM manual submission origin = %q, want manual", manualSubmission.Origin)
	}

	insertWorkflowTask(t, store.SQL, rollupTaskID, "child rollup submission", "manual")
	expectSQLErrorContains(t, store.SQL, "unknown submission origin", "CHECK constraint failed", `
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at, origin
		) VALUES (?, ?, 1, 'pending_review', ?, '2026-08-29T10:01:00Z', 'automatic')
	`, rollupSubmissionID, rollupTaskID, builtinSystemActorID)
	expectSQLErrorContains(t, store.SQL, "child rollup submitted by owner", "TASK_CHILD_ROLLUP_SUBMISSION_INVALID", `
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at, origin
		) VALUES (?, ?, 1, 'pending_review', ?, '2026-08-29T10:01:00Z', 'child_rollup')
	`, rollupSubmissionID, rollupTaskID, builtinOwnerActorID)
	expectSQLErrorContains(t, store.SQL, "inferred child rollup", "TASK_CHILD_ROLLUP_SUBMISSION_INVALID", `
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at, is_inferred, origin
		) VALUES (?, ?, 1, 'pending_review', ?, '2026-08-29T10:01:00Z', 1, 'child_rollup')
	`, rollupSubmissionID, rollupTaskID, builtinSystemActorID)
	if _, err := store.SQL.Exec(`
		INSERT INTO task_submissions(
			id, task_id, sequence, status, submitted_by_actor_id, submitted_at, is_inferred, origin
		) VALUES (?, ?, 1, 'pending_review', ?, '2026-08-29T10:01:00Z', 0, 'child_rollup')
	`, rollupSubmissionID, rollupTaskID, builtinSystemActorID); err != nil {
		t.Fatalf("insert valid child-rollup submission: %v", err)
	}

	expectSQLErrorContains(t, store.SQL, "mutate submission origin", "TASK_SUBMISSION_HISTORY_IMMUTABLE", `
		UPDATE task_submissions SET origin = 'manual' WHERE id = ?
	`, rollupSubmissionID)
	if _, err := store.SQL.Exec(`
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 1, 'text', 'manual evidence', 'allowed', ?, ?)
	`, manualArtifactID, manualTaskID, manualSubmissionID, builtinOwnerActorID, builtinOwnerActorID); err != nil {
		t.Fatalf("insert Artifact for manual submission: %v", err)
	}
	expectSQLErrorContains(t, store.SQL, "Artifact on child rollup", "TASK_CHILD_ROLLUP_ARTIFACT_FORBIDDEN", `
		INSERT INTO task_artifacts(
			id, task_id, submission_id, position, storage_kind, name, content_text,
			produced_by_actor_id, recorded_by_actor_id
		) VALUES (?, ?, ?, 1, 'text', 'rollup evidence', 'forbidden', ?, ?)
	`, rollupArtifactID, rollupTaskID, rollupSubmissionID, builtinSystemActorID, builtinSystemActorID)

	var tableSQL string
	if err := store.SQL.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'task_submissions'").Scan(&tableSQL); err != nil {
		t.Fatalf("read task_submissions DDL: %v", err)
	}
	if !strings.Contains(tableSQL, "origin IN ('manual', 'child_rollup')") {
		t.Fatalf("task_submissions DDL lacks origin enum constraint: %s", tableSQL)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}
