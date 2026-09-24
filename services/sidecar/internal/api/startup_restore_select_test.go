package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListStartupRestoreChoicesIsMetadataOnlyAndFailClosed(t *testing.T) {
	root := t.TempDir()
	missing, err := ListStartupRestoreChoices(filepath.Join(root, "missing"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing backup root = %#v err=%v", missing, err)
	}

	validID := "018f0000-0000-7000-8000-00000000a001"
	validPath := filepath.Join(root, validID)
	if err := os.MkdirAll(validPath, 0o700); err != nil {
		t.Fatal(err)
	}
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	writeBackupManifestFileForTest(t, validPath, `{
		"format_version":1,
		"id":"`+validID+`",
		"created_at":"2026-09-23T10:00:00.000000000Z",
		"verified_at":"2026-09-23T10:01:00.000000000Z",
		"note":"PRIVATE BACKUP NOTE",
		"app_version":"0.1.1",
		"commit":"test",
		"api_version":"`+Version+`",
		"schema_version":79,
		"database_id":"018f0000-0000-7000-8000-00000000d001",
		"artifact_store_id":"018f0000-0000-7000-8000-00000000e001",
		"database":{"path":"database/opc-workspace.db","size_bytes":10,"sha256":"`+hashA+`"},
		"artifact_marker":{"path":"artifacts/`+artifactStoreMarkerName+`","size_bytes":10,"sha256":"`+hashB+`"},
		"artifacts":[],
		"artifact_count":0,
		"artifact_bytes":0,
		"total_bytes":20,
		"kind":"manual"
	}`)
	invalidID := "018f0000-0000-7000-8000-00000000a002"
	if err := os.MkdirAll(filepath.Join(root, invalidID), 0o700); err != nil {
		t.Fatal(err)
	}
	notAPackage := filepath.Join(root, "not-a-uuid")
	if err := os.MkdirAll(notAPackage, 0o700); err != nil {
		t.Fatal(err)
	}

	choices, err := ListStartupRestoreChoices(root)
	if err != nil {
		t.Fatalf("ListStartupRestoreChoices() error = %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("choices = %#v", choices)
	}
	if raw, readErr := readBackupManifest(validPath); readErr != nil {
		t.Fatalf("read fixture manifest: %v", readErr)
	} else if validateErr := validateBackupManifest(raw, validID, 0); validateErr != nil {
		t.Fatalf("fixture manifest invalid: %v raw=%#v", validateErr, raw)
	}
	// Newest created_at first; invalid package has empty created_at and sorts last.
	first, second := choices[0], choices[1]
	if first.ID != validID || first.VerificationStatus != "verified" || first.Kind != "manual" ||
		first.Note != "PRIVATE BACKUP NOTE" || first.SchemaVersion != 79 {
		t.Fatalf("valid choice = %#v", first)
	}
	if second.ID != invalidID || second.VerificationStatus != "invalid" {
		t.Fatalf("invalid choice = %#v", second)
	}
	if strings.Contains(strings.TrimSpace(first.Note), "path") {
		t.Fatalf("choice leaked path-like fields: %#v", first)
	}
}

func TestPrepareStartupRestoreSchedulesPendingWithoutStartingAPI(t *testing.T) {
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, t.TempDir())
	task, _ := setupManualReviewTask(t, router.Engine)
	manifest := `{"summary":"startup restore source","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"note.txt","file_field":"file"}]}`
	uploaded := performMultipartRequest(
		router.Engine,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		manifest,
		map[string][]byte{"file": []byte("startup restore body")},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit output = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	create := performRequest(router.Engine, http.MethodPost, "/api/v1/backups",
		[]byte(`{"note":"startup restore source"}`),
		map[string]string{"Idempotency-Key": "startup-restore-source"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create backup = %d: %s", create.Code, create.Body.String())
	}
	backup := decodeBackupSummary(t, create.Body.Bytes())
	if err := router.Close(); err != nil {
		t.Fatalf("close router: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	result, err := PrepareStartupRestore(PrepareStartupRestoreConfig{
		BackupID: backup.ID, Confirm: true,
		DatabasePath: databasePath, BackupDir: backupDir, ArtifactDir: artifactDir,
	})
	if err != nil {
		t.Fatalf("PrepareStartupRestore() error = %v", err)
	}
	if result.BackupID != backup.ID || !result.RestartRequired || result.RollbackBackupID == "" ||
		result.RollbackBackupID == backup.ID {
		t.Fatalf("prepare result = %#v", result)
	}
	pendingPath := filepath.Join(backupDir, pendingRestoreDirectory)
	if info, err := os.Stat(pendingPath); err != nil || !info.IsDir() {
		t.Fatalf("pending restore missing: %v", err)
	}

	replay, err := PrepareStartupRestore(PrepareStartupRestoreConfig{
		BackupID: backup.ID, Confirm: true,
		DatabasePath: databasePath, BackupDir: backupDir, ArtifactDir: artifactDir,
	})
	if err != nil || replay.BackupID != backup.ID || replay.RollbackBackupID != result.RollbackBackupID {
		t.Fatalf("idempotent replay = %#v err=%v", replay, err)
	}

	otherID := "018f0000-0000-7000-8000-00000000a099"
	if _, err := PrepareStartupRestore(PrepareStartupRestoreConfig{
		BackupID: otherID, Confirm: true,
		DatabasePath: databasePath, BackupDir: backupDir, ArtifactDir: artifactDir,
	}); err == nil {
		t.Fatal("expected different pending restore to fail closed")
	}
}

func TestPrepareStartupRestoreRejectsMissingConfirmAndInvalidID(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	backupDir := filepath.Join(root, "backups")
	artifactDir := filepath.Join(root, "artifacts")
	if _, err := PrepareStartupRestore(PrepareStartupRestoreConfig{
		BackupID: "018f0000-0000-7000-8000-00000000a001", Confirm: false,
		DatabasePath: databasePath, BackupDir: backupDir, ArtifactDir: artifactDir,
	}); err == nil {
		t.Fatal("expected missing confirm to fail")
	}
	if _, err := PrepareStartupRestore(PrepareStartupRestoreConfig{
		BackupID: "not-a-uuid", Confirm: true,
		DatabasePath: databasePath, BackupDir: backupDir, ArtifactDir: artifactDir,
	}); err == nil {
		t.Fatal("expected invalid id to fail")
	}
}

func writeBackupManifestFileForTest(t *testing.T, packagePath, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(packagePath, backupManifestName), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
