package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	backupFormatVersion = 1
	backupManifestName  = "manifest.json"
	maxBackupNoteRunes  = 200
	maxBackupManifest   = 1 << 20
)

type backupStore struct {
	root         string
	databasePath string
	artifacts    *artifactStore
	mu           sync.Mutex
}

type backupManifestFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type backupManifestArtifact struct {
	ID string `json:"id"`
	backupManifestFile
}

// controlledFileBackupRow deliberately spans every file-backed business fact
// that lives in the shared Artifact store. The v1 manifest field names stay
// unchanged so schema <=18 backup packages remain readable.
type controlledFileBackupRow struct {
	ID           string `gorm:"column:id"`
	RelativePath string `gorm:"column:relative_path"`
	SizeBytes    int64  `gorm:"column:size_bytes"`
	SHA256       string `gorm:"column:sha256"`
}

type backupManifest struct {
	FormatVersion      int                      `json:"format_version"`
	ID                 string                   `json:"id"`
	CreatedAt          string                   `json:"created_at"`
	VerifiedAt         string                   `json:"verified_at,omitempty"`
	Note               string                   `json:"note,omitempty"`
	AppVersion         string                   `json:"app_version"`
	Commit             string                   `json:"commit"`
	APIVersion         string                   `json:"api_version"`
	SchemaVersion      int                      `json:"schema_version"`
	DatabaseID         string                   `json:"database_id"`
	ArtifactStoreID    string                   `json:"artifact_store_id"`
	Database           backupManifestFile       `json:"database"`
	ArtifactMarker     backupManifestFile       `json:"artifact_marker"`
	Artifacts          []backupManifestArtifact `json:"artifacts"`
	ArtifactCount      int                      `json:"artifact_count"`
	ArtifactBytes      int64                    `json:"artifact_bytes"`
	TotalBytes         int64                    `json:"total_bytes"`
	IdempotencyKeyHash string                   `json:"idempotency_key_hash,omitempty"`
	RequestHash        string                   `json:"request_hash,omitempty"`
}

type backupSummary struct {
	ID                 string `json:"id"`
	CreatedAt          string `json:"created_at"`
	VerifiedAt         string `json:"verified_at,omitempty"`
	VerificationStatus string `json:"verification_status"`
	Note               string `json:"note,omitempty"`
	AppVersion         string `json:"app_version"`
	APIVersion         string `json:"api_version"`
	SchemaVersion      int    `json:"schema_version"`
	ArtifactCount      int    `json:"artifact_count"`
	ArtifactBytes      int64  `json:"artifact_bytes"`
	DatabaseBytes      int64  `json:"database_bytes"`
	TotalBytes         int64  `json:"total_bytes"`
	Error              string `json:"error,omitempty"`
}

type createBackupRequest struct {
	Note string `json:"note"`
}

func newBackupStore(root, databasePath string, artifacts *artifactStore) (*backupStore, error) {
	root = strings.TrimSpace(root)
	databasePath = strings.TrimSpace(databasePath)
	if root == "" || databasePath == "" {
		return nil, errors.New("backup directory and database path must be configured together")
	}
	if databasePath == ":memory:" {
		return nil, errors.New("verified backups require a file-backed database")
	}
	if artifacts == nil {
		return nil, errors.New("verified backups require a controlled Artifact store")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve backup root: %w", err)
	}
	absoluteDatabase, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve backup database path: %w", err)
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	absoluteDatabase = filepath.Clean(absoluteDatabase)
	if isFilesystemRoot(absoluteRoot) || sameFilesystemPath(absoluteRoot, absoluteDatabase) || pathContains(absoluteRoot, absoluteDatabase) {
		return nil, errors.New("backup root must be a dedicated directory")
	}
	if pathContains(absoluteRoot, artifacts.root) || pathContains(artifacts.root, absoluteRoot) {
		return nil, errors.New("backup root and Artifact root must not overlap")
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create backup root: %w", err)
	}
	if err := requireSafeDirectory(absoluteRoot); err != nil {
		return nil, fmt.Errorf("validate backup root: %w", err)
	}
	databaseInfo, err := os.Lstat(absoluteDatabase)
	if err != nil {
		return nil, fmt.Errorf("inspect backup database source: %w", err)
	}
	if !databaseInfo.Mode().IsRegular() || databaseInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("backup database source is not a regular file")
	}
	return &backupStore{root: absoluteRoot, databasePath: absoluteDatabase, artifacts: artifacts}, nil
}

func pathContains(parent, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (a *API) maintenanceReadMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.restorePending.Load() {
			backupRootPath := "/api/" + Version + "/backups"
			isRestoreReplay := c.Request.Method == http.MethodPost &&
				strings.HasPrefix(c.Request.URL.Path, backupRootPath+"/") &&
				strings.HasSuffix(c.Request.URL.Path, "/restore")
			if (c.Request.Method == http.MethodGet && c.Request.URL.Path == backupRootPath) || isRestoreReplay {
				c.Next()
				return
			}
			writeError(c, http.StatusServiceUnavailable, "RESTORE_RESTART_REQUIRED", "A verified restore is pending; restart the application to apply it")
			c.Abort()
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/"+Version+"/backups") {
			c.Next()
			return
		}
		a.maintenance.RLock()
		defer a.maintenance.RUnlock()
		c.Next()
	}
}

func (a *API) listBackups(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	summaries, err := a.backupStore.list()
	if err != nil {
		a.logBackupError("list", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_LIST_FAILED", "Local backups could not be listed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": summaries})
}

func (a *API) createBackup(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	var input createBackupRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	note := strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(note) > maxBackupNoteRunes {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "note must not exceed 200 characters")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := sha256Hex([]byte(note))
	keyHash := ""
	if idempotencyKey != "" {
		keyHash = sha256Hex([]byte(idempotencyKey))
	}

	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	if keyHash != "" {
		replayed, found, err := a.backupStore.findIdempotent(keyHash, requestHash, a.options.SchemaVersion)
		if err != nil {
			if errors.Is(err, errBackupIdempotencyConflict) {
				writeError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different backup request")
				return
			}
			a.logBackupError("replay", err)
			writeError(c, http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This backup request cannot be replayed safely; use a new key")
			return
		}
		if found {
			c.JSON(http.StatusCreated, gin.H{"data": replayed})
			return
		}
	}
	summary, err := a.backupStore.create(a.db.WithContext(c.Request.Context()), a.options, note, keyHash, requestHash)
	if err != nil {
		a.logBackupError("create", err)
		if projectionErr := a.projectBackupCreateFailure(requestIDFromContext(c)); projectionErr != nil {
			a.logBackupError("record maintenance incident", projectionErr)
		}
		writeError(c, http.StatusInternalServerError, "BACKUP_CREATE_FAILED", "A verified local backup could not be created; existing data was not changed")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": summary})
}

func (a *API) verifyBackup(c *gin.Context) {
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
		if os.IsNotExist(err) {
			writeError(c, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
			return
		}
		a.logBackupError("inspect", err)
		if projectionErr := a.projectBackupVerifyFailure(requestIDFromContext(c)); projectionErr != nil {
			a.logBackupError("record maintenance incident", projectionErr)
		}
		writeError(c, http.StatusInternalServerError, "BACKUP_VERIFY_FAILED", "The local backup could not be verified")
		return
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package is invalid")
		return
	}
	manifest, err := a.backupStore.verifyPackage(packagePath, id, a.options.SchemaVersion)
	if err != nil {
		a.logBackupError("verify", err)
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package failed integrity verification")
		return
	}
	manifest.VerifiedAt = a.options.Now().UTC().Format(time.RFC3339Nano)
	if err := persistVerifiedBackupManifest(packagePath, manifest); err != nil {
		a.logBackupError("record verification", err)
		if projectionErr := a.projectBackupVerifyFailure(requestIDFromContext(c)); projectionErr != nil {
			a.logBackupError("record maintenance incident", projectionErr)
		}
		writeError(c, http.StatusInternalServerError, "BACKUP_VERIFY_FAILED", "The backup was valid but its verification time could not be recorded")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": backupSummaryFromManifest(manifest, "verified", "")})
}

func (a *API) logBackupError(operation string, err error) {
	if a.options.Logger != nil {
		a.options.Logger.Printf("Backup %s failed: %v", operation, err)
	}
}

var errBackupIdempotencyConflict = errors.New("backup idempotency conflict")

func (s *backupStore) findIdempotent(keyHash, requestHash string, maxSchemaVersion int) (backupSummary, bool, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return backupSummary{}, false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, err := readBackupManifest(filepath.Join(s.root, entry.Name()))
		if err != nil || manifest.IdempotencyKeyHash != keyHash {
			continue
		}
		if manifest.RequestHash != requestHash {
			return backupSummary{}, false, errBackupIdempotencyConflict
		}
		if _, err := s.verifyPackage(filepath.Join(s.root, entry.Name()), manifest.ID, maxSchemaVersion); err != nil {
			return backupSummary{}, false, err
		}
		return backupSummaryFromManifest(manifest, "verified", ""), true, nil
	}
	return backupSummary{}, false, nil
}

func (s *backupStore) create(db *gorm.DB, options Options, note, keyHash, requestHash string) (backupSummary, error) {
	id := uuid.NewString()
	createdAt := options.Now().UTC().Format(time.RFC3339Nano)
	stagingPath := filepath.Join(s.root, ".staging-"+id)
	finalPath := filepath.Join(s.root, id)
	if err := os.Mkdir(stagingPath, 0o700); err != nil {
		return backupSummary{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = os.RemoveAll(stagingPath)
		}
	}()
	databaseDir := filepath.Join(stagingPath, "database")
	artifactDir := filepath.Join(stagingPath, "artifacts")
	objectDir := filepath.Join(artifactDir, "objects")
	for _, directory := range []string{databaseDir, artifactDir, objectDir} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return backupSummary{}, fmt.Errorf("create backup package layout: %w", err)
		}
	}

	var databaseID, storeID string
	if err := db.Raw("SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&databaseID, &storeID); err != nil {
		return backupSummary{}, fmt.Errorf("read workspace identity: %w", err)
	}
	canonicalDatabaseID, err := canonicalArtifactDatabaseID(databaseID)
	if err != nil {
		return backupSummary{}, err
	}
	databaseID = canonicalDatabaseID
	canonicalStoreID, err := canonicalArtifactStoreID(storeID)
	if err != nil {
		return backupSummary{}, err
	}
	storeID = canonicalStoreID
	if storeID != s.artifacts.storeID {
		return backupSummary{}, errors.New("workspace and Artifact store identities differ")
	}

	databaseRelative := "database/opc-workspace.db"
	databaseSnapshot := filepath.Join(stagingPath, filepath.FromSlash(databaseRelative))
	if err := db.Exec("VACUUM INTO ?", databaseSnapshot).Error; err != nil {
		return backupSummary{}, fmt.Errorf("create consistent SQLite snapshot: %w", err)
	}
	if err := os.Chmod(databaseSnapshot, 0o600); err != nil {
		return backupSummary{}, fmt.Errorf("protect SQLite snapshot: %w", err)
	}
	databaseFile, err := inspectFile(databaseSnapshot)
	if err != nil {
		return backupSummary{}, fmt.Errorf("hash SQLite snapshot: %w", err)
	}
	databaseFile.Path = databaseRelative

	markerRelative := "artifacts/" + artifactStoreMarkerName
	markerSource := filepath.Join(s.artifacts.root, artifactStoreMarkerName)
	markerFile, err := copyVerifiedBackupFile(markerSource, filepath.Join(stagingPath, filepath.FromSlash(markerRelative)), -1, "")
	if err != nil {
		return backupSummary{}, fmt.Errorf("copy Artifact marker: %w", err)
	}
	markerFile.Path = markerRelative
	expectedMarker, err := artifactStoreMarkerBytes(databaseID, storeID)
	if err != nil {
		return backupSummary{}, err
	}
	actualMarker, err := os.ReadFile(filepath.Join(stagingPath, filepath.FromSlash(markerRelative)))
	if err != nil || string(actualMarker) != string(expectedMarker) {
		return backupSummary{}, errors.New("Artifact marker does not match workspace identity")
	}

	rows, err := listActiveControlledFiles(db, options.SchemaVersion)
	if err != nil {
		return backupSummary{}, fmt.Errorf("list active Artifact objects: %w", err)
	}
	artifacts := make([]backupManifestArtifact, 0, len(rows))
	var artifactBytes int64
	for _, row := range rows {
		expectedRelative := "objects/" + row.ID
		if row.RelativePath != expectedRelative {
			return backupSummary{}, fmt.Errorf("active controlled file %s has an invalid path", row.ID)
		}
		source, err := s.artifacts.resolveObject(row.RelativePath)
		if err != nil {
			return backupSummary{}, fmt.Errorf("resolve active Artifact %s: %w", row.ID, err)
		}
		relative := "artifacts/objects/" + row.ID
		copied, err := copyVerifiedBackupFile(source, filepath.Join(stagingPath, filepath.FromSlash(relative)), row.SizeBytes, row.SHA256)
		if err != nil {
			return backupSummary{}, fmt.Errorf("copy active Artifact %s: %w", row.ID, err)
		}
		copied.Path = relative
		artifacts = append(artifacts, backupManifestArtifact{ID: row.ID, backupManifestFile: copied})
		artifactBytes += copied.SizeBytes
	}

	manifest := backupManifest{
		FormatVersion: backupFormatVersion, ID: id, CreatedAt: createdAt, Note: note,
		AppVersion: options.AppVersion, Commit: options.Commit, APIVersion: Version,
		SchemaVersion: options.SchemaVersion, DatabaseID: databaseID, ArtifactStoreID: storeID,
		Database: databaseFile, ArtifactMarker: markerFile, Artifacts: artifacts,
		ArtifactCount: len(artifacts), ArtifactBytes: artifactBytes,
		TotalBytes:         databaseFile.SizeBytes + markerFile.SizeBytes + artifactBytes,
		IdempotencyKeyHash: keyHash, RequestHash: requestHash,
	}
	if err := writeBackupManifest(stagingPath, manifest); err != nil {
		return backupSummary{}, err
	}
	if _, err := s.verifyPackage(stagingPath, id, options.SchemaVersion); err != nil {
		return backupSummary{}, fmt.Errorf("verify staged backup: %w", err)
	}
	manifest.VerifiedAt = options.Now().UTC().Format(time.RFC3339Nano)
	if err := writeBackupManifest(stagingPath, manifest); err != nil {
		return backupSummary{}, err
	}
	for _, directory := range []string{objectDir, artifactDir, databaseDir, stagingPath} {
		if err := syncArtifactDirectory(directory); err != nil {
			return backupSummary{}, fmt.Errorf("sync backup package: %w", err)
		}
	}
	if err := os.Rename(stagingPath, finalPath); err != nil {
		return backupSummary{}, fmt.Errorf("publish backup package: %w", err)
	}
	finished = true
	if err := syncArtifactDirectory(s.root); err != nil {
		return backupSummary{}, fmt.Errorf("sync backup root: %w", err)
	}
	return backupSummaryFromManifest(manifest, "verified", ""), nil
}

func (s *backupStore) list() ([]backupSummary, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	summaries := make([]backupSummary, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		id := strings.ToLower(entry.Name())
		if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
			continue
		}
		manifest, err := readBackupManifest(filepath.Join(s.root, entry.Name()))
		if err != nil {
			summaries = append(summaries, backupSummary{ID: id, VerificationStatus: "invalid", Error: "备份清单无法读取"})
			continue
		}
		if err := validateBackupManifest(manifest, id, 0); err != nil {
			summaries = append(summaries, backupSummary{ID: id, CreatedAt: manifest.CreatedAt, VerificationStatus: "invalid", Error: "备份清单无效"})
			continue
		}
		status := "unverified"
		if manifest.VerifiedAt != "" {
			status = "verified"
		}
		summaries = append(summaries, backupSummaryFromManifest(manifest, status, ""))
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].CreatedAt == summaries[j].CreatedAt {
			return summaries[i].ID > summaries[j].ID
		}
		return summaries[i].CreatedAt > summaries[j].CreatedAt
	})
	return summaries, nil
}

func (s *backupStore) verifyPackage(packagePath, expectedID string, maxSchemaVersion int) (backupManifest, error) {
	if err := requireSafeDirectory(packagePath); err != nil {
		return backupManifest{}, err
	}
	manifest, err := readBackupManifest(packagePath)
	if err != nil {
		return backupManifest{}, err
	}
	if err := validateBackupManifest(manifest, expectedID, maxSchemaVersion); err != nil {
		return backupManifest{}, err
	}
	files := []backupManifestFile{manifest.Database, manifest.ArtifactMarker}
	for _, artifact := range manifest.Artifacts {
		files = append(files, artifact.backupManifestFile)
	}
	expectedFiles := map[string]struct{}{backupManifestName: {}}
	for _, file := range files {
		expectedFiles[file.Path] = struct{}{}
	}
	if err := rejectUnexpectedBackupFiles(packagePath, expectedFiles); err != nil {
		return backupManifest{}, err
	}
	for _, file := range files {
		actual, err := inspectFile(filepath.Join(packagePath, filepath.FromSlash(file.Path)))
		if err != nil {
			return backupManifest{}, fmt.Errorf("inspect %s: %w", file.Path, err)
		}
		if actual.SizeBytes != file.SizeBytes || actual.SHA256 != file.SHA256 {
			return backupManifest{}, fmt.Errorf("file integrity mismatch: %s", file.Path)
		}
	}
	marker, err := os.ReadFile(filepath.Join(packagePath, filepath.FromSlash(manifest.ArtifactMarker.Path)))
	if err != nil {
		return backupManifest{}, err
	}
	expectedMarker, err := artifactStoreMarkerBytes(manifest.DatabaseID, manifest.ArtifactStoreID)
	if err != nil || string(marker) != string(expectedMarker) {
		return backupManifest{}, errors.New("backup Artifact marker does not match manifest identity")
	}
	if err := verifyBackupDatabase(filepath.Join(packagePath, filepath.FromSlash(manifest.Database.Path)), manifest); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func validateBackupManifest(manifest backupManifest, expectedID string, maxSchemaVersion int) error {
	if manifest.FormatVersion != backupFormatVersion {
		return errors.New("unsupported backup format")
	}
	parsedID, err := uuid.Parse(manifest.ID)
	if err != nil || parsedID.String() != manifest.ID || manifest.ID != expectedID {
		return errors.New("backup id does not match package")
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt); err != nil {
		return errors.New("backup creation time is invalid")
	}
	if manifest.VerifiedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, manifest.VerifiedAt); err != nil {
			return errors.New("backup verification time is invalid")
		}
	}
	if utf8.RuneCountInString(manifest.Note) > maxBackupNoteRunes || manifest.APIVersion != Version || manifest.SchemaVersion < 1 {
		return errors.New("backup version metadata is invalid")
	}
	if maxSchemaVersion > 0 && manifest.SchemaVersion > maxSchemaVersion {
		return errors.New("backup schema is newer than this Sidecar")
	}
	if _, err := canonicalArtifactDatabaseID(manifest.DatabaseID); err != nil {
		return err
	}
	if _, err := canonicalArtifactStoreID(manifest.ArtifactStoreID); err != nil {
		return err
	}
	if err := validateBackupFile(manifest.Database, "database/opc-workspace.db"); err != nil {
		return err
	}
	if err := validateBackupFile(manifest.ArtifactMarker, "artifacts/"+artifactStoreMarkerName); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	var artifactBytes int64
	for _, artifact := range manifest.Artifacts {
		parsed, err := uuid.Parse(artifact.ID)
		if err != nil || parsed.String() != artifact.ID {
			return errors.New("backup Artifact id is invalid")
		}
		if _, exists := seen[artifact.ID]; exists {
			return errors.New("backup Artifact ids are duplicated")
		}
		seen[artifact.ID] = struct{}{}
		if err := validateBackupFile(artifact.backupManifestFile, "artifacts/objects/"+artifact.ID); err != nil {
			return err
		}
		artifactBytes += artifact.SizeBytes
	}
	if manifest.ArtifactCount != len(manifest.Artifacts) || manifest.ArtifactBytes != artifactBytes || manifest.TotalBytes != manifest.Database.SizeBytes+manifest.ArtifactMarker.SizeBytes+artifactBytes {
		return errors.New("backup totals do not match manifest files")
	}
	return nil
}

func validateBackupFile(file backupManifestFile, expectedPath string) error {
	if file.Path != expectedPath || file.SizeBytes <= 0 || len(file.SHA256) != 64 || strings.ToLower(file.SHA256) != file.SHA256 {
		return fmt.Errorf("backup file metadata is invalid: %s", expectedPath)
	}
	if _, err := hex.DecodeString(file.SHA256); err != nil {
		return fmt.Errorf("backup file hash is invalid: %s", expectedPath)
	}
	return nil
}

func verifyBackupDatabase(path string, manifest backupManifest) error {
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&immutable=1"
	db, err := gorm.Open(glebarezsqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("open backup database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	var quickCheck string
	if err := sqlDB.QueryRow("PRAGMA quick_check").Scan(&quickCheck); err != nil || quickCheck != "ok" {
		return fmt.Errorf("backup database quick_check failed: %v", err)
	}
	rows, err := sqlDB.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("backup database foreign_key_check failed: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup database contains a foreign key violation")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var schemaVersion int
	if err := sqlDB.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&schemaVersion); err != nil || schemaVersion != manifest.SchemaVersion {
		return errors.New("backup database schema does not match manifest")
	}
	var databaseID, storeID string
	if err := sqlDB.QueryRow("SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1").Scan(&databaseID, &storeID); err != nil {
		return fmt.Errorf("read backup workspace identity: %w", err)
	}
	if databaseID != manifest.DatabaseID || storeID != manifest.ArtifactStoreID {
		return errors.New("backup database identity does not match manifest")
	}
	countQuery, lookupQuery := controlledFileVerificationQueries(manifest.SchemaVersion)
	var activeCount int
	if err := sqlDB.QueryRow(countQuery).Scan(&activeCount); err != nil || activeCount != manifest.ArtifactCount {
		return errors.New("backup active Artifact count does not match manifest")
	}
	for _, artifact := range manifest.Artifacts {
		var relativePath, hash string
		var size int64
		if err := sqlDB.QueryRow(lookupQuery, artifact.ID).Scan(&relativePath, &size, &hash); err != nil {
			return fmt.Errorf("read backup Artifact %s: %w", artifact.ID, err)
		}
		if relativePath != "objects/"+artifact.ID || size != artifact.SizeBytes || hash != artifact.SHA256 {
			return fmt.Errorf("backup Artifact %s metadata does not match database", artifact.ID)
		}
	}
	return nil
}

func listActiveControlledFiles(db *gorm.DB, schemaVersion int) ([]controlledFileBackupRow, error) {
	var rows []controlledFileBackupRow
	query := `
		SELECT id, relative_path, size_bytes, sha256
		FROM task_artifacts
		WHERE storage_kind = 'file' AND deleted_at IS NULL
	`
	if schemaVersion >= 19 {
		query += `
			UNION ALL
			SELECT id, relative_path, size_bytes, sha256
			FROM client_attachments
			WHERE deleted_at IS NULL
		`
	}
	if schemaVersion >= 22 {
		query += `
			UNION ALL
			SELECT id, relative_path, size_bytes, sha256
			FROM project_attachments
			WHERE deleted_at IS NULL
		`
	}
	query = "SELECT id, relative_path, size_bytes, sha256 FROM (" + query + ") ORDER BY id ASC"
	err := db.Raw(query).Scan(&rows).Error
	return rows, err
}

func controlledFileVerificationQueries(schemaVersion int) (string, string) {
	if schemaVersion < 19 {
		return "SELECT COUNT(*) FROM task_artifacts WHERE storage_kind = 'file' AND deleted_at IS NULL",
			"SELECT relative_path, size_bytes, sha256 FROM task_artifacts WHERE id = ? AND storage_kind = 'file' AND deleted_at IS NULL"
	}
	clientUnion := `
		SELECT id, relative_path, size_bytes, sha256
		FROM task_artifacts
		WHERE storage_kind = 'file' AND deleted_at IS NULL
		UNION ALL
		SELECT id, relative_path, size_bytes, sha256
		FROM client_attachments
		WHERE deleted_at IS NULL
	`
	if schemaVersion < 22 {
		return "SELECT COUNT(*) FROM (" + clientUnion + ")",
			"SELECT relative_path, size_bytes, sha256 FROM (" + clientUnion + ") WHERE id = ?"
	}
	union := clientUnion + `
		UNION ALL
		SELECT id, relative_path, size_bytes, sha256
		FROM project_attachments
		WHERE deleted_at IS NULL
	`
	return "SELECT COUNT(*) FROM (" + union + ")",
		"SELECT relative_path, size_bytes, sha256 FROM (" + union + ") WHERE id = ?"
}

func readBackupManifest(packagePath string) (backupManifest, error) {
	path := filepath.Join(packagePath, backupManifestName)
	info, err := os.Lstat(path)
	if err != nil {
		return backupManifest{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return backupManifest{}, errors.New("backup manifest is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxBackupManifest+1))
	decoder.DisallowUnknownFields()
	var manifest backupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return backupManifest{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return backupManifest{}, err
	}
	if info, err := file.Stat(); err != nil || info.Size() > maxBackupManifest {
		return backupManifest{}, errors.New("backup manifest exceeds 1 MiB")
	}
	return manifest, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("backup manifest contains multiple JSON values")
		}
		return err
	}
	return nil
}

func writeBackupManifest(packagePath string, manifest backupManifest) error {
	return writeBackupManifestFile(filepath.Join(packagePath, backupManifestName), manifest)
}

var persistVerifiedBackupManifest = writeBackupManifestAtomic

func writeBackupManifestAtomic(packagePath string, manifest backupManifest) error {
	temporaryPath := filepath.Join(packagePath, ".manifest-"+uuid.NewString()+".tmp")
	if err := writeBackupManifestFile(temporaryPath, manifest); err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	if err := replaceFileAtomically(temporaryPath, filepath.Join(packagePath, backupManifestName)); err != nil {
		return fmt.Errorf("replace backup manifest: %w", err)
	}
	if err := syncArtifactDirectory(packagePath); err != nil {
		return fmt.Errorf("sync backup package after manifest replacement: %w", err)
	}
	return nil
}

func writeBackupManifestFile(path string, manifest backupManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backup manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxBackupManifest {
		return errors.New("backup manifest exceeds 1 MiB")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open backup manifest: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync backup manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup manifest: %w", err)
	}
	return nil
}

func copyVerifiedBackupFile(sourcePath, destinationPath string, expectedSize int64, expectedHash string) (backupManifestFile, error) {
	linkedInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return backupManifestFile{}, err
	}
	if !linkedInfo.Mode().IsRegular() || linkedInfo.Mode()&os.ModeSymlink != 0 {
		return backupManifestFile{}, errors.New("backup source is not a regular file")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return backupManifestFile{}, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return backupManifestFile{}, errors.New("backup source is not a regular file")
	}
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return backupManifestFile{}, err
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(destination, hasher), source)
	if copyErr == nil {
		copyErr = destination.Sync()
	}
	closeErr := destination.Close()
	if copyErr != nil {
		return backupManifestFile{}, copyErr
	}
	if closeErr != nil {
		return backupManifestFile{}, closeErr
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	if expectedSize >= 0 && (size != expectedSize || hash != expectedHash) {
		return backupManifestFile{}, errors.New("backup source changed or failed integrity verification")
	}
	return backupManifestFile{SizeBytes: size, SHA256: hash}, nil
}

func inspectFile(path string) (backupManifestFile, error) {
	linkedInfo, err := os.Lstat(path)
	if err != nil {
		return backupManifestFile{}, err
	}
	if !linkedInfo.Mode().IsRegular() || linkedInfo.Mode()&os.ModeSymlink != 0 {
		return backupManifestFile{}, errors.New("path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return backupManifestFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return backupManifestFile{}, errors.New("path is not a regular file")
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return backupManifestFile{}, err
	}
	return backupManifestFile{SizeBytes: size, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func rejectUnexpectedBackupFiles(packagePath string, expected map[string]struct{}) error {
	return filepath.WalkDir(packagePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == packagePath {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("backup package contains a symbolic link or reparse point")
		}
		relative, err := filepath.Rel(packagePath, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			switch relative {
			case "database", "artifacts", "artifacts/objects":
				return nil
			default:
				return fmt.Errorf("backup package contains unexpected directory %s", relative)
			}
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("backup package contains a non-regular file")
		}
		if _, ok := expected[relative]; !ok {
			return fmt.Errorf("backup package contains unexpected file %s", relative)
		}
		return nil
	})
}

func backupSummaryFromManifest(manifest backupManifest, status, message string) backupSummary {
	return backupSummary{
		ID: manifest.ID, CreatedAt: manifest.CreatedAt, VerifiedAt: manifest.VerifiedAt,
		VerificationStatus: status, Note: manifest.Note, AppVersion: manifest.AppVersion,
		APIVersion: manifest.APIVersion, SchemaVersion: manifest.SchemaVersion,
		ArtifactCount: manifest.ArtifactCount, ArtifactBytes: manifest.ArtifactBytes,
		DatabaseBytes: manifest.Database.SizeBytes, TotalBytes: manifest.TotalBytes, Error: message,
	}
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
