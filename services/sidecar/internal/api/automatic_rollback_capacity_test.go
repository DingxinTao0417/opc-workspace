package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreMigrationRollbackRejectsInsufficientCapacityWithoutPublishing(t *testing.T) {
	root := t.TempDir()
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, root)
	defer store.Close()
	if err := router.Close(); err != nil {
		t.Fatalf("close router before startup backup: %v", err)
	}
	var probedPath string
	_, err := CreatePreMigrationBackup(store.DB, Options{
		AppVersion: "0.1.0-test", Commit: "migration-capacity-test", SchemaVersion: store.SchemaVersion,
		ArtifactDir: artifactDir, DatabasePath: databasePath, BackupDir: backupDir,
		DiskSpaceCheck: func(path string) (uint64, uint64, error) {
			probedPath = path
			return 0, 1, nil
		},
	}, store.SchemaVersion+1)
	if !errors.Is(err, errBackupSpaceInsufficient) {
		t.Fatalf("CreatePreMigrationBackup() error = %v, want insufficient capacity", err)
	}
	if !sameFilesystemPath(probedPath, backupDir) {
		t.Fatalf("capacity probe path = %q, want backup root %q", probedPath, backupDir)
	}
	assertBackupRootEmpty(t, backupDir)

	artifacts, reopenErr := openWorkspaceArtifactStore(store.DB, artifactDir, false)
	if reopenErr != nil {
		t.Fatalf("capacity refusal retained Artifact lease: %v", reopenErr)
	}
	if closeErr := artifacts.close(); closeErr != nil {
		t.Fatalf("close reopened Artifact store: %v", closeErr)
	}
}

func TestBusinessImportsRejectInsufficientRollbackCapacityWithoutSideEffects(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		sourceRouter, _, _, _ := newBackupTestAPI(t)
		packageData := emptyBusinessExportFixture(t, sourceRouter)
		body, err := json.Marshal(packageData)
		if err != nil {
			t.Fatal(err)
		}
		var probedPath string
		runtime, store, _, _, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
			probedPath = path
			return 0, 1, nil
		})
		response := performRequest(
			runtime.Engine, http.MethodPost, "/api/v1/imports/business-data", body,
			map[string]string{"X-Import-Confirmation": importConfirmation},
		)
		if response.Code != http.StatusInsufficientStorage || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_BACKUP_SPACE_INSUFFICIENT" {
			t.Fatalf("JSON import capacity refusal = %d: %s", response.Code, response.Body.String())
		}
		if !sameFilesystemPath(probedPath, backupDir) {
			t.Fatalf("JSON import probe path = %q, want %q", probedPath, backupDir)
		}
		assertBackupRootEmpty(t, backupDir)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients", 0)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
	})

	t.Run("controlled package", func(t *testing.T) {
		payload, _ := businessPackageFixture(t)
		var probedPath string
		runtime, store, _, artifactDir, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
			probedPath = path
			return 0, 1, nil
		})
		response := performRequest(
			runtime.Engine, http.MethodPost, "/api/v1/imports/business-package", payload,
			map[string]string{"X-Import-Confirmation": packageImportConfirmation},
		)
		if response.Code != http.StatusInsufficientStorage || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_BACKUP_SPACE_INSUFFICIENT" {
			t.Fatalf("package import capacity refusal = %d: %s", response.Code, response.Body.String())
		}
		if !sameFilesystemPath(probedPath, backupDir) {
			t.Fatalf("package import probe path = %q, want %q", probedPath, backupDir)
		}
		assertBackupRootEmpty(t, backupDir)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
		objects, err := os.ReadDir(filepath.Join(artifactDir, "objects"))
		if err != nil || len(objects) != 0 {
			t.Fatalf("package capacity refusal left controlled files=%v err=%v", objects, err)
		}
	})
}

func TestBusinessImportRejectsUnavailableRollbackCapacity(t *testing.T) {
	sourceRouter, _, _, _ := newBackupTestAPI(t)
	packageData := emptyBusinessExportFixture(t, sourceRouter)
	body, err := json.Marshal(packageData)
	if err != nil {
		t.Fatal(err)
	}
	runtime, store, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		return 123, 456, errors.New("private capacity failure")
	})
	response := performRequest(
		runtime.Engine, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importConfirmation},
	)
	if response.Code != http.StatusServiceUnavailable || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_BACKUP_CAPACITY_UNAVAILABLE" {
		t.Fatalf("unavailable import capacity = %d: %s", response.Code, response.Body.String())
	}
	if stringBody := response.Body.String(); containsAny(stringBody, "private capacity failure", "123", "456", backupDir) {
		t.Fatalf("capacity response leaked private details: %s", stringBody)
	}
	assertBackupRootEmpty(t, backupDir)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients", 0)
}

func TestRestoreCapacityIncludesRollbackAndPendingPackageWithoutIncident(t *testing.T) {
	limited := false
	var available, total uint64 = 1 << 60, 1 << 60
	var probedPath string
	runtime, store, databasePath, _, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
		probedPath = path
		if limited {
			return available, total, nil
		}
		return 1 << 60, 1 << 60, nil
	})
	created := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"restore capacity target"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create restore target = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	rollbackOnly, err := estimateBackupCreateCapacity(store.DB, databasePath, runtime.artifactStore, store.SchemaVersion)
	if err != nil {
		t.Fatalf("estimate rollback-only capacity: %v", err)
	}
	limited = true
	available = rollbackOnly
	total = rollbackOnly + 1<<30
	response := performRequest(
		runtime.Engine, http.MethodPost, "/api/v1/backups/"+target.ID+"/restore", []byte(`{"confirm":true}`), nil,
	)
	if response.Code != http.StatusInsufficientStorage || responseErrorCode(t, response.Body.Bytes()) != "RESTORE_ROLLBACK_SPACE_INSUFFICIENT" {
		t.Fatalf("restore capacity refusal = %d: %s", response.Code, response.Body.String())
	}
	if !sameFilesystemPath(probedPath, backupDir) {
		t.Fatalf("restore probe path = %q, want %q", probedPath, backupDir)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != target.ID {
		t.Fatalf("restore capacity refusal changed backup root entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, pendingRestoreDirectory)); !os.IsNotExist(err) {
		t.Fatalf("restore capacity refusal published pending plan: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = 'backup:restore'", 0)
}

func TestRestoreRejectsUnavailableRollbackCapacityWithoutLeakingProbeError(t *testing.T) {
	probeFails := false
	runtime, store, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		if probeFails {
			return 123, 456, errors.New("private restore capacity failure")
		}
		return 1 << 60, 1 << 60, nil
	})
	created := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"restore unavailable target"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create restore target = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	probeFails = true
	response := performRequest(
		runtime.Engine, http.MethodPost, "/api/v1/backups/"+target.ID+"/restore", []byte(`{"confirm":true}`), nil,
	)
	if response.Code != http.StatusServiceUnavailable || responseErrorCode(t, response.Body.Bytes()) != "RESTORE_ROLLBACK_CAPACITY_UNAVAILABLE" {
		t.Fatalf("restore unavailable capacity = %d: %s", response.Code, response.Body.String())
	}
	if stringBody := response.Body.String(); containsAny(stringBody, "private restore capacity failure", "123", "456", backupDir) {
		t.Fatalf("restore capacity response leaked private details: %s", stringBody)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != target.ID {
		t.Fatalf("restore capacity refusal changed backup root entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, pendingRestoreDirectory)); !os.IsNotExist(err) {
		t.Fatalf("restore capacity refusal published pending plan: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = 'backup:restore'", 0)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
