package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatePreMigrationBackupPublishesVerifiedRollbackPackage(t *testing.T) {
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, t.TempDir())
	defer store.Close()
	task, _ := setupManualReviewTask(t, router.Engine)
	manifest := `{"summary":"Before migration","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"migration.txt","file_field":"file"}]}`
	uploaded := performMultipartRequest(
		router.Engine,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		manifest,
		map[string][]byte{"file": []byte("pre-migration artifact")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit output = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	if err := router.Close(); err != nil {
		t.Fatalf("close router before startup backup: %v", err)
	}

	targetSchema := store.SchemaVersion + 1
	backupID, err := CreatePreMigrationBackup(store.DB, Options{
		AppVersion: "0.1.0-test", Commit: "migration-test", SchemaVersion: store.SchemaVersion,
		ArtifactDir: artifactDir, DatabasePath: databasePath, BackupDir: backupDir,
	}, targetSchema)
	if err != nil {
		t.Fatalf("CreatePreMigrationBackup() error = %v", err)
	}
	packagePath := filepath.Join(backupDir, backupID)
	backup := &backupStore{root: backupDir}
	verified, err := backup.verifyPackage(packagePath, backupID, targetSchema)
	if err != nil {
		t.Fatalf("verify pre-migration package: %v", err)
	}
	if verified.SchemaVersion != store.SchemaVersion || verified.ArtifactCount != 1 || !strings.Contains(verified.Note, "schema v26 → v27") {
		t.Fatalf("pre-migration manifest = %#v", verified)
	}
}

func TestCreatePreMigrationBackupRejectsInvalidBoundaryWithoutPublishing(t *testing.T) {
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, t.TempDir())
	defer store.Close()
	if err := router.Close(); err != nil {
		t.Fatalf("close router: %v", err)
	}
	if _, err := CreatePreMigrationBackup(store.DB, Options{
		SchemaVersion: store.SchemaVersion,
		ArtifactDir:   artifactDir, DatabasePath: databasePath, BackupDir: backupDir,
	}, store.SchemaVersion); err == nil {
		t.Fatal("expected invalid migration boundary to fail")
	}
	entries, err := filepath.Glob(filepath.Join(backupDir, "*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected backup entries = %v, err=%v", entries, err)
	}
}
