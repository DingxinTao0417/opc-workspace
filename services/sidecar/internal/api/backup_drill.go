package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

type backupRestoreDrillResult struct {
	BackupID           string `json:"backup_id"`
	DrilledAt          string `json:"drilled_at"`
	SourceSchema       int    `json:"source_schema_version"`
	ResultSchema       int    `json:"result_schema_version"`
	ArtifactCount      int    `json:"artifact_count"`
	TemporaryDataClean bool   `json:"temporary_data_cleaned"`
}

func (a *API) drillBackupRestore(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	id := strings.ToLower(strings.TrimSpace(c.Param("id")))
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_ID", "Backup id must be a canonical UUID")
		return
	}

	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	packagePath := filepath.Join(a.backupStore.root, id)
	if info, err := os.Lstat(packagePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(c, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
			return
		}
		a.logBackupError("inspect restore drill", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_DRILL_FAILED", "The local backup restore drill could not start")
		return
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package is invalid")
		return
	}
	manifest, err := a.backupStore.verifyPackage(packagePath, id, a.options.SchemaVersion)
	if err != nil {
		a.logBackupError("verify restore drill source", err)
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package failed integrity verification")
		return
	}
	result, err := a.backupStore.runRestoreDrill(packagePath, manifest, a.options.SchemaVersion)
	if err != nil {
		a.logBackupError("restore drill", err)
		writeError(c, http.StatusConflict, "BACKUP_NOT_RESTORABLE", "The backup could not be opened safely in an isolated temporary data root")
		return
	}
	result.DrilledAt = a.options.Now().UTC().Format(time.RFC3339Nano)
	result.TemporaryDataClean = true
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (s *backupStore) runRestoreDrill(packagePath string, manifest backupManifest, targetSchema int) (result backupRestoreDrillResult, err error) {
	temporaryRoot, err := os.MkdirTemp(s.root, ".restore-drill-")
	if err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("create restore drill root: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(temporaryRoot); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("clean restore drill root: %w", cleanupErr))
		}
	}()
	if err := requireSafeDirectory(temporaryRoot); err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("validate restore drill root: %w", err)
	}

	databasePath := filepath.Join(temporaryRoot, "opc-workspace.db")
	artifactRoot := filepath.Join(temporaryRoot, "artifacts")
	objectRoot := filepath.Join(artifactRoot, "objects")
	if err := os.MkdirAll(objectRoot, 0o700); err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("create restore drill layout: %w", err)
	}
	if _, err := copyVerifiedBackupFile(
		filepath.Join(packagePath, filepath.FromSlash(manifest.Database.Path)),
		databasePath,
		manifest.Database.SizeBytes,
		manifest.Database.SHA256,
	); err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("copy restore drill database: %w", err)
	}
	if _, err := copyVerifiedBackupFile(
		filepath.Join(packagePath, filepath.FromSlash(manifest.ArtifactMarker.Path)),
		filepath.Join(artifactRoot, artifactStoreMarkerName),
		manifest.ArtifactMarker.SizeBytes,
		manifest.ArtifactMarker.SHA256,
	); err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("copy restore drill marker: %w", err)
	}
	for _, artifact := range manifest.Artifacts {
		if _, err := copyVerifiedBackupFile(
			filepath.Join(packagePath, filepath.FromSlash(artifact.Path)),
			filepath.Join(objectRoot, artifact.ID),
			artifact.SizeBytes,
			artifact.SHA256,
		); err != nil {
			return backupRestoreDrillResult{}, fmt.Errorf("copy restore drill Artifact %s: %w", artifact.ID, err)
		}
	}

	store, err := database.Open(databasePath)
	if err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("open and migrate restore drill database: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close restore drill database: %w", closeErr))
		}
	}()
	if store.SchemaVersion != targetSchema {
		return backupRestoreDrillResult{}, fmt.Errorf("restore drill schema = %d, want %d", store.SchemaVersion, targetSchema)
	}
	if err := verifyOpenDatabase(store, manifest.DatabaseID, manifest.ArtifactStoreID); err != nil {
		return backupRestoreDrillResult{}, err
	}

	artifacts, err := newArtifactStore(artifactRoot, manifest.DatabaseID, manifest.ArtifactStoreID)
	if err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("open restore drill Artifact store: %w", err)
	}
	defer func() {
		if closeErr := artifacts.close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close restore drill Artifact store: %w", closeErr))
		}
	}()
	rows, err := listActiveControlledFiles(store.DB)
	if err != nil {
		return backupRestoreDrillResult{}, fmt.Errorf("read restore drill Artifacts: %w", err)
	}
	if len(rows) != manifest.ArtifactCount {
		return backupRestoreDrillResult{}, errors.New("restore drill active Artifact count does not match backup")
	}
	expectedObjects := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.RelativePath != "objects/"+row.ID {
			return backupRestoreDrillResult{}, fmt.Errorf("restore drill Artifact %s has invalid storage facts", row.ID)
		}
		objectPath, err := artifacts.resolveObject(row.RelativePath)
		if err != nil {
			return backupRestoreDrillResult{}, err
		}
		matches, err := artifactFileMatches(objectPath, row.SizeBytes, row.SHA256)
		if err != nil || !matches {
			return backupRestoreDrillResult{}, fmt.Errorf("restore drill Artifact %s failed integrity verification", row.ID)
		}
		expectedObjects[row.ID] = struct{}{}
	}
	entries, err := os.ReadDir(objectRoot)
	if err != nil {
		return backupRestoreDrillResult{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return backupRestoreDrillResult{}, errors.New("restore drill contains a non-regular Artifact object")
		}
		if _, ok := expectedObjects[entry.Name()]; !ok {
			return backupRestoreDrillResult{}, errors.New("restore drill contains an unexpected Artifact object")
		}
	}

	return backupRestoreDrillResult{
		BackupID: manifest.ID, SourceSchema: manifest.SchemaVersion,
		ResultSchema: store.SchemaVersion, ArtifactCount: len(rows),
	}, nil
}

func verifyOpenDatabase(store *database.Store, databaseID, artifactStoreID string) error {
	var quickCheck string
	if err := store.SQL.QueryRow("PRAGMA quick_check").Scan(&quickCheck); err != nil || quickCheck != "ok" {
		return fmt.Errorf("restore drill quick_check failed: %v", err)
	}
	rows, err := store.SQL.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("restore drill foreign_key_check failed: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("restore drill database contains a foreign key violation")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var actualDatabaseID, actualStoreID string
	if err := store.SQL.QueryRow("SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1").Scan(&actualDatabaseID, &actualStoreID); err != nil {
		return fmt.Errorf("read restore drill identity: %w", err)
	}
	if actualDatabaseID != databaseID || actualStoreID != artifactStoreID {
		return errors.New("restore drill identity does not match backup")
	}
	return nil
}
