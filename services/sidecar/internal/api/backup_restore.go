package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"gorm.io/gorm"
)

const (
	pendingRestoreDirectory    = ".restore-pending-v1"
	appliedRestorePrefix       = ".restore-applied-"
	pendingRestorePlanName     = "plan.json"
	pendingRestoreVersion      = 1
	maxRestorePlanBytes        = 16 << 10
	backupInvoicePDFMarkerName = ".opc-workspace-invoice-pdf-store"
)

type scheduleRestoreRequest struct {
	Confirm bool `json:"confirm"`
}

type scheduledRestoreResult struct {
	BackupID         string `json:"backup_id"`
	RollbackBackupID string `json:"rollback_backup_id"`
	RequestedAt      string `json:"requested_at"`
	RestartRequired  bool   `json:"restart_required"`
}

type pendingRestorePlan struct {
	FormatVersion    int    `json:"format_version"`
	OperationID      string `json:"operation_id"`
	BackupID         string `json:"backup_id"`
	RollbackBackupID string `json:"rollback_backup_id"`
	RequestedAt      string `json:"requested_at"`
	DatabaseID       string `json:"database_id"`
	ArtifactStoreID  string `json:"artifact_store_id"`
	SourceSchema     int    `json:"source_schema_version"`
}

type StartupRestoreResult struct {
	Applied          bool
	BackupID         string
	RollbackBackupID string
	RequestedAt      string
	CleanupWarning   string
}

// StartupRestoreStage is a bounded, non-sensitive progress fact for the
// desktop shell. It never includes a local path, backup identifier, error or
// user-controlled text because it is emitted before the HTTP API is ready.
type StartupRestoreStage string

const (
	StartupRestoreCheckingPending    StartupRestoreStage = "checking_pending_restore"
	StartupRestoreVerifyingPackage   StartupRestoreStage = "verifying_restore_package"
	StartupRestoreApplying           StartupRestoreStage = "applying_restore"
	StartupRestoreVerifyingWorkspace StartupRestoreStage = "verifying_restored_workspace"
	StartupRestoreFinalizing         StartupRestoreStage = "finalizing_restore"
)

var publishPendingRestorePackage = (*backupStore).publishPendingRestore

func (a *API) recordBackupRestoreFailure(c *gin.Context) {
	if err := a.projectBackupRestoreFailure(requestIDFromContext(c)); err != nil {
		a.logBackupError("record restore maintenance incident", err)
	}
}

func (a *API) scheduleBackupRestore(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	id := strings.ToLower(strings.TrimSpace(c.Param("id")))
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_ID", "Backup id must be a canonical UUID")
		return
	}
	var input scheduleRestoreRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !input.Confirm {
		writeError(c, http.StatusUnprocessableEntity, "RESTORE_CONFIRMATION_REQUIRED", "confirm must be true to schedule a restore")
		return
	}

	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	unlockInvoicePDFs := a.lockInvoicePDFStore()
	defer unlockInvoicePDFs()
	if existing, found, err := loadPendingRestore(a.backupStore.root); err != nil {
		a.logBackupError("read pending restore", err)
		a.recordBackupRestoreFailure(c)
		writeError(c, http.StatusConflict, "RESTORE_PENDING_INVALID", "An existing pending restore could not be validated")
		return
	} else if found {
		if existing.BackupID != id {
			writeError(c, http.StatusConflict, "RESTORE_ALREADY_PENDING", "A different backup is already pending restore")
			return
		}
		a.restorePending.Store(true)
		c.JSON(http.StatusAccepted, gin.H{"data": scheduledRestoreResult{
			BackupID: existing.BackupID, RollbackBackupID: existing.RollbackBackupID,
			RequestedAt: existing.RequestedAt, RestartRequired: true,
		}})
		return
	}

	packagePath := filepath.Join(a.backupStore.root, id)
	if info, err := os.Lstat(packagePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(c, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
			return
		}
		a.logBackupError("inspect restore source", err)
		a.recordBackupRestoreFailure(c)
		writeError(c, http.StatusInternalServerError, "RESTORE_SCHEDULE_FAILED", "The restore source could not be inspected")
		return
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package is invalid")
		return
	}
	manifest, err := a.backupStore.verifyPackage(packagePath, id, a.options.SchemaVersion)
	if err != nil {
		a.logBackupError("verify restore source", err)
		writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package failed integrity verification")
		return
	}
	var currentDatabaseID, currentStoreID string
	if err := a.db.WithContext(c.Request.Context()).Raw(
		"SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1",
	).Row().Scan(&currentDatabaseID, &currentStoreID); err != nil {
		a.logBackupError("read restore identity", err)
		a.recordBackupRestoreFailure(c)
		writeError(c, http.StatusInternalServerError, "RESTORE_SCHEDULE_FAILED", "The current workspace identity could not be read")
		return
	}
	if manifest.DatabaseID != currentDatabaseID || manifest.ArtifactStoreID != currentStoreID || currentStoreID != a.artifactStore.storeID {
		writeError(c, http.StatusConflict, "RESTORE_WORKSPACE_MISMATCH", "The backup belongs to a different workspace or Artifact store")
		return
	}
	if _, err := runBackupRestoreDrill(a.backupStore, packagePath, manifest, a.options.SchemaVersion); err != nil {
		a.logBackupError("pre-restore drill", err)
		a.recordBackupDrillFailure(c)
		writeError(c, http.StatusConflict, "BACKUP_NOT_RESTORABLE", "The backup could not be opened safely in an isolated temporary data root")
		return
	}
	pendingPayloadBytes, err := pendingRestoreCapacityPayload(manifest)
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "RESTORE_ROLLBACK_CAPACITY_UNAVAILABLE", "Restore storage capacity could not be confirmed; no restore was scheduled")
		return
	}
	if err := a.backupStore.requireCreateCapacity(
		a.db.WithContext(c.Request.Context()), a.options, pendingPayloadBytes,
	); err != nil {
		if errors.Is(err, errBackupSpaceInsufficient) {
			writeError(c, http.StatusInsufficientStorage, "RESTORE_ROLLBACK_SPACE_INSUFFICIENT", "There is not enough storage space to create a rollback backup and stage the restore; no restore was scheduled")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "RESTORE_ROLLBACK_CAPACITY_UNAVAILABLE", "Restore storage capacity could not be confirmed; no restore was scheduled")
		return
	}

	rollbackNote := "恢复 " + id[:8] + " 前自动回滚点"
	rollback, err := a.backupStore.create(
		a.db.WithContext(c.Request.Context()), a.options, rollbackNote, "", sha256Hex([]byte(rollbackNote)),
	)
	if err != nil {
		a.logBackupError("create pre-restore rollback", err)
		a.recordBackupRestoreFailure(c)
		writeError(c, http.StatusInternalServerError, "RESTORE_ROLLBACK_BACKUP_FAILED", "A rollback backup could not be created; no restore was scheduled")
		return
	}
	requestedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	plan := pendingRestorePlan{
		FormatVersion: pendingRestoreVersion, OperationID: uuid.NewString(),
		BackupID: id, RollbackBackupID: rollback.ID, RequestedAt: requestedAt,
		DatabaseID: manifest.DatabaseID, ArtifactStoreID: manifest.ArtifactStoreID,
		SourceSchema: manifest.SchemaVersion,
	}
	if err := publishPendingRestorePackage(a.backupStore, packagePath, manifest, plan, a.options.SchemaVersion); err != nil {
		a.logBackupError("publish pending restore", err)
		a.recordBackupRestoreFailure(c)
		writeError(c, http.StatusInternalServerError, "RESTORE_SCHEDULE_FAILED", "The restore could not be scheduled; current data was not changed")
		return
	}
	a.restorePending.Store(true)
	c.JSON(http.StatusAccepted, gin.H{"data": scheduledRestoreResult{
		BackupID: id, RollbackBackupID: rollback.ID, RequestedAt: requestedAt, RestartRequired: true,
	}})
}

func pendingRestoreCapacityPayload(manifest backupManifest) (uint64, error) {
	if manifest.TotalBytes < 0 {
		return 0, errBackupCapacityUnavailable
	}
	payload := uint64(manifest.TotalBytes)
	var ok bool
	for _, size := range []uint64{maxBackupManifest, maxRestorePlanBytes} {
		payload, ok = checkedBackupCapacityAdd(payload, size)
		if !ok {
			return 0, errBackupCapacityUnavailable
		}
	}
	return payload, nil
}

func (s *backupStore) publishPendingRestore(sourcePath string, manifest backupManifest, plan pendingRestorePlan, maxSchema int) error {
	stagingPath := filepath.Join(s.root, ".restore-schedule-"+plan.OperationID)
	pendingPath := filepath.Join(s.root, pendingRestoreDirectory)
	if _, err := os.Lstat(pendingPath); err == nil {
		return errors.New("a restore is already pending")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(stagingPath, 0o700); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stagingPath)
		}
	}()
	packagePath := filepath.Join(stagingPath, "package")
	if err := copyBackupPackage(sourcePath, packagePath, manifest); err != nil {
		return err
	}
	temporaryStore := &backupStore{root: s.root}
	if _, err := temporaryStore.verifyPackage(packagePath, manifest.ID, maxSchema); err != nil {
		return fmt.Errorf("verify scheduled restore package: %w", err)
	}
	if err := writeStrictJSONFile(filepath.Join(stagingPath, pendingRestorePlanName), plan, maxRestorePlanBytes); err != nil {
		return err
	}
	for _, directory := range append(backupPackageSyncDirectories(packagePath, manifest), stagingPath) {
		if err := syncArtifactDirectory(directory); err != nil {
			return err
		}
	}
	if err := os.Rename(stagingPath, pendingPath); err != nil {
		return err
	}
	published = true
	return syncArtifactDirectory(s.root)
}

func copyBackupPackage(sourcePath, destinationPath string, manifest backupManifest) error {
	for _, directory := range []string{
		destinationPath,
		filepath.Join(destinationPath, "database"),
		filepath.Join(destinationPath, "artifacts"),
		filepath.Join(destinationPath, "artifacts", "objects"),
		filepath.Join(destinationPath, "artifacts", "avatars"),
	} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
	}
	if err := writeBackupManifest(destinationPath, manifest); err != nil {
		return err
	}
	files := []backupManifestFile{manifest.Database, manifest.ArtifactMarker}
	for _, artifact := range manifest.Artifacts {
		files = append(files, artifact.backupManifestFile)
	}
	for _, file := range files {
		destination := filepath.Join(destinationPath, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create scheduled restore directory for %s: %w", file.Path, err)
		}
		if _, err := copyVerifiedBackupFile(
			filepath.Join(sourcePath, filepath.FromSlash(file.Path)),
			destination,
			file.SizeBytes, file.SHA256,
		); err != nil {
			return fmt.Errorf("copy scheduled restore file %s: %w", file.Path, err)
		}
	}
	for _, directory := range backupPackageSyncDirectories(destinationPath, manifest) {
		if err := syncArtifactDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func backupManifestContainsInvoicePDFs(manifest backupManifest) bool {
	for _, file := range manifest.Artifacts {
		if strings.HasPrefix(file.Path, invoicePDFControlledPrefix) {
			return true
		}
	}
	return false
}

func initializeInvoicePDFRestoreRoot(root, databaseID string) error {
	if _, err := canonicalArtifactDatabaseID(databaseID); err != nil {
		return err
	}
	if err := ensureArtifactRootDirectory(root); err != nil {
		return err
	}
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	for _, directory := range []string{filepath.Join(root, ".staging"), filepath.Join(root, ".trash")} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
		if err := requireSafeDirectory(directory); err != nil {
			return err
		}
	}
	markerPath := filepath.Join(root, backupInvoicePDFMarkerName)
	marker, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(marker, databaseID+"\n")
	if writeErr == nil {
		writeErr = marker.Sync()
	}
	closeErr := marker.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(markerPath)
		return errors.Join(writeErr, closeErr)
	}
	for _, directory := range []string{filepath.Join(root, ".staging"), filepath.Join(root, ".trash"), root} {
		if err := syncArtifactDirectory(directory); err != nil {
			return err
		}
	}
	return syncArtifactDirectory(filepath.Dir(root))
}

func verifyInvoicePDFStoreMarker(root, databaseID string) error {
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	markerPath := filepath.Join(root, backupInvoicePDFMarkerName)
	info, err := os.Lstat(markerPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 128 {
		return errors.New("invoice PDF store marker is invalid")
	}
	contents, err := os.ReadFile(markerPath)
	if err != nil || string(contents) != databaseID+"\n" {
		return errors.New("invoice PDF store marker does not match workspace identity")
	}
	return nil
}

func verifyLiveInvoicePDFRootForRestore(root, databaseID string) error {
	markerPath := filepath.Join(root, backupInvoicePDFMarkerName)
	if _, err := os.Lstat(markerPath); err == nil {
		return verifyInvoicePDFStoreMarker(root, databaseID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("unclaimed invoice PDF restore root is not empty")
	}
	return nil
}

func loadPendingRestore(root string) (pendingRestorePlan, bool, error) {
	pendingPath := filepath.Join(root, pendingRestoreDirectory)
	info, err := os.Lstat(pendingPath)
	if errors.Is(err, os.ErrNotExist) {
		return pendingRestorePlan{}, false, nil
	}
	if err != nil {
		return pendingRestorePlan{}, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return pendingRestorePlan{}, false, errors.New("pending restore path is not a regular directory")
	}
	if err := requireSafeDirectory(pendingPath); err != nil {
		return pendingRestorePlan{}, false, err
	}
	var plan pendingRestorePlan
	if err := readStrictJSONFile(filepath.Join(pendingPath, pendingRestorePlanName), maxRestorePlanBytes, &plan); err != nil {
		return pendingRestorePlan{}, false, err
	}
	if err := validatePendingRestorePlan(plan); err != nil {
		return pendingRestorePlan{}, false, err
	}
	return plan, true, nil
}

func validatePendingRestorePlan(plan pendingRestorePlan) error {
	if plan.FormatVersion != pendingRestoreVersion || plan.SourceSchema < 1 {
		return errors.New("pending restore version is invalid")
	}
	for _, value := range []string{plan.OperationID, plan.BackupID, plan.RollbackBackupID} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != value {
			return errors.New("pending restore contains an invalid UUID")
		}
	}
	if plan.BackupID == plan.RollbackBackupID {
		return errors.New("pending restore target and rollback backup must differ")
	}
	if _, err := time.Parse(time.RFC3339Nano, plan.RequestedAt); err != nil {
		return errors.New("pending restore time is invalid")
	}
	if _, err := canonicalArtifactDatabaseID(plan.DatabaseID); err != nil {
		return err
	}
	if _, err := canonicalArtifactStoreID(plan.ArtifactStoreID); err != nil {
		return err
	}
	return nil
}

func writeStrictJSONFile(path string, value any, limit int64) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > limit {
		return errors.New("JSON file exceeds its size limit")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readStrictJSONFile(path string, limit int64, destination any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return errors.New("JSON file is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

// ApplyPendingRestore runs before the live database or Artifact lease is opened.
// It is restart-safe: a published plan and its private package remain intact
// until both live resources have been replaced and verified.
func ApplyPendingRestore(backupRoot, databasePath, artifactRoot string, maxSchema int) (StartupRestoreResult, error) {
	return applyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot, "", maxSchema, nil)
}

// ApplyPendingRestoreWithProgress runs the same restart-safe restore sequence
// as ApplyPendingRestore and optionally reports bounded startup stages. The
// observer is informational only: an unavailable parent UI must never alter
// recovery correctness.
func ApplyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot string, maxSchema int, progress func(StartupRestoreStage)) (StartupRestoreResult, error) {
	return applyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot, "", maxSchema, progress)
}

// ApplyPendingRestoreWithInvoicePDFs applies a pending restore while atomically
// replacing the independently rooted invoice PDF store. The legacy entry points
// remain available only to Sidecar schema ceilings <=46; newer runtimes must
// supply the invoice PDF root even when the selected package contains no PDFs.
func ApplyPendingRestoreWithInvoicePDFs(backupRoot, databasePath, artifactRoot, invoicePDFRoot string, maxSchema int) (StartupRestoreResult, error) {
	return applyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot, invoicePDFRoot, maxSchema, nil)
}

// ApplyPendingRestoreWithInvoicePDFsAndProgress is the desktop startup entry
// point for a fully controlled database, Artifact and invoice PDF restore.
func ApplyPendingRestoreWithInvoicePDFsAndProgress(backupRoot, databasePath, artifactRoot, invoicePDFRoot string, maxSchema int, progress func(StartupRestoreStage)) (StartupRestoreResult, error) {
	return applyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot, invoicePDFRoot, maxSchema, progress)
}

func applyPendingRestoreWithProgress(backupRoot, databasePath, artifactRoot, invoicePDFRoot string, maxSchema int, progress func(StartupRestoreStage)) (StartupRestoreResult, error) {
	report := func(stage StartupRestoreStage) {
		if progress != nil {
			progress(stage)
		}
	}
	report(StartupRestoreCheckingPending)
	absoluteBackup, err := filepath.Abs(strings.TrimSpace(backupRoot))
	if err != nil {
		return StartupRestoreResult{}, err
	}
	plan, found, err := loadPendingRestore(absoluteBackup)
	if err != nil || !found {
		return StartupRestoreResult{}, err
	}
	if plan.SourceSchema > maxSchema {
		return StartupRestoreResult{}, errors.New("pending restore schema is newer than this Sidecar")
	}
	if maxSchema >= invoicePDFSchemaVersion && strings.TrimSpace(invoicePDFRoot) == "" {
		return StartupRestoreResult{}, errors.New("invoice PDF restore root is required for this Sidecar schema")
	}
	absoluteDatabase, err := filepath.Abs(strings.TrimSpace(databasePath))
	if err != nil {
		return StartupRestoreResult{}, err
	}
	absoluteArtifacts, err := filepath.Abs(strings.TrimSpace(artifactRoot))
	if err != nil {
		return StartupRestoreResult{}, err
	}
	absoluteInvoicePDFs := ""
	if strings.TrimSpace(invoicePDFRoot) != "" {
		absoluteInvoicePDFs, err = filepath.Abs(strings.TrimSpace(invoicePDFRoot))
		if err != nil {
			return StartupRestoreResult{}, err
		}
		absoluteInvoicePDFs = filepath.Clean(absoluteInvoicePDFs)
	}
	if err := requireSafeDirectory(absoluteBackup); err != nil {
		return StartupRestoreResult{}, err
	}
	if err := requireSafeDirectory(absoluteArtifacts); err != nil {
		return StartupRestoreResult{}, err
	}
	if absoluteInvoicePDFs != "" {
		if isFilesystemRoot(absoluteInvoicePDFs) {
			return StartupRestoreResult{}, errors.New("invoice PDF restore root cannot be a filesystem volume root")
		}
		if sameFilesystemPath(absoluteInvoicePDFs, absoluteDatabase) || pathContains(absoluteInvoicePDFs, absoluteDatabase) ||
			pathContains(absoluteInvoicePDFs, absoluteArtifacts) || pathContains(absoluteArtifacts, absoluteInvoicePDFs) ||
			pathContains(absoluteInvoicePDFs, absoluteBackup) || pathContains(absoluteBackup, absoluteInvoicePDFs) {
			return StartupRestoreResult{}, errors.New("invoice PDF restore root overlaps another controlled path")
		}
		if err := requireSafeDirectory(absoluteInvoicePDFs); err != nil {
			return StartupRestoreResult{}, err
		}
	}
	pendingPath := filepath.Join(absoluteBackup, pendingRestoreDirectory)
	packagePath := filepath.Join(pendingPath, "package")
	store := &backupStore{root: absoluteBackup, databasePath: absoluteDatabase}
	report(StartupRestoreVerifyingPackage)
	manifest, err := store.verifyPackage(packagePath, plan.BackupID, maxSchema)
	if err != nil {
		return StartupRestoreResult{}, fmt.Errorf("verify pending restore package: %w", err)
	}
	if manifest.DatabaseID != plan.DatabaseID || manifest.ArtifactStoreID != plan.ArtifactStoreID || manifest.SchemaVersion != plan.SourceSchema {
		return StartupRestoreResult{}, errors.New("pending restore package does not match plan")
	}
	if absoluteInvoicePDFs == "" && backupManifestContainsInvoicePDFs(manifest) {
		return StartupRestoreResult{}, errors.New("pending restore contains invoice PDFs but no invoice PDF root was provided")
	}
	if absoluteInvoicePDFs != "" {
		if err := verifyLiveInvoicePDFRootForRestore(absoluteInvoicePDFs, plan.DatabaseID); err != nil {
			return StartupRestoreResult{}, err
		}
	}
	rollbackPath := filepath.Join(absoluteBackup, plan.RollbackBackupID)
	rollbackManifest, err := store.verifyPackage(rollbackPath, plan.RollbackBackupID, maxSchema)
	if err != nil {
		return StartupRestoreResult{}, fmt.Errorf("verify restore rollback backup: %w", err)
	}
	if rollbackManifest.DatabaseID != plan.DatabaseID || rollbackManifest.ArtifactStoreID != plan.ArtifactStoreID {
		return StartupRestoreResult{}, errors.New("restore rollback backup identity does not match plan")
	}
	if _, err := store.runRestoreDrill(packagePath, manifest, maxSchema); err != nil {
		return StartupRestoreResult{}, fmt.Errorf("repeat restore drill at startup: %w", err)
	}

	paths := restoreSwapPaths{
		database:    absoluteDatabase,
		databaseNew: filepath.Join(filepath.Dir(absoluteDatabase), ".opc-restore-new-"+plan.OperationID+".db"),
		databaseOld: filepath.Join(filepath.Dir(absoluteDatabase), ".opc-restore-old-"+plan.OperationID+".db"),
		objects:     filepath.Join(absoluteArtifacts, "objects"),
		objectsNew:  filepath.Join(absoluteArtifacts, ".restore-new-objects-"+plan.OperationID),
		objectsOld:  filepath.Join(absoluteArtifacts, ".restore-old-objects-"+plan.OperationID),
		avatars:     filepath.Join(absoluteArtifacts, "avatars"),
		avatarsNew:  filepath.Join(absoluteArtifacts, ".restore-new-avatars-"+plan.OperationID),
		avatarsOld:  filepath.Join(absoluteArtifacts, ".restore-old-avatars-"+plan.OperationID),
	}
	if absoluteInvoicePDFs != "" {
		paths.invoicePDFs = absoluteInvoicePDFs
		paths.invoicePDFsNew = filepath.Join(filepath.Dir(absoluteInvoicePDFs), ".restore-new-invoices-"+plan.OperationID)
		paths.invoicePDFsOld = filepath.Join(filepath.Dir(absoluteInvoicePDFs), ".restore-old-invoices-"+plan.OperationID)
	}
	if err := prepareRestoreSwap(paths, packagePath, manifest, maxSchema); err != nil {
		return StartupRestoreResult{}, err
	}
	report(StartupRestoreApplying)
	if err := applyRestoreSwap(paths); err != nil {
		rollbackErr := rollbackRestoreSwap(paths)
		var quarantineErr error
		if rollbackErr == nil {
			quarantineErr = quarantineFailedRestorePlan(absoluteBackup, pendingPath, plan.OperationID)
		}
		return StartupRestoreResult{}, errors.Join(err, rollbackErr, quarantineErr)
	}
	report(StartupRestoreVerifyingWorkspace)
	if err := verifyLiveRestore(paths.database, absoluteArtifacts, absoluteInvoicePDFs, manifest, maxSchema); err != nil {
		rollbackErr := rollbackRestoreSwap(paths)
		var quarantineErr error
		if rollbackErr == nil {
			quarantineErr = quarantineFailedRestorePlan(absoluteBackup, pendingPath, plan.OperationID)
		}
		return StartupRestoreResult{}, errors.Join(fmt.Errorf("verify applied restore: %w", err), rollbackErr, quarantineErr)
	}
	report(StartupRestoreFinalizing)
	appliedPath := filepath.Join(absoluteBackup, appliedRestorePrefix+plan.OperationID)
	if err := os.Rename(pendingPath, appliedPath); err != nil {
		return StartupRestoreResult{}, err
	}
	commitSyncErr := syncArtifactDirectory(absoluteBackup)
	cleanupErr := cleanupSuccessfulRestore(paths, appliedPath, absoluteBackup)
	warning := errors.Join(commitSyncErr, cleanupErr)
	result := StartupRestoreResult{Applied: true, BackupID: plan.BackupID, RollbackBackupID: plan.RollbackBackupID, RequestedAt: plan.RequestedAt}
	if warning != nil {
		result.CleanupWarning = warning.Error()
	}
	return result, nil
}

type restoreSwapPaths struct {
	database, databaseNew, databaseOld          string
	objects, objectsNew, objectsOld             string
	avatars, avatarsNew, avatarsOld             string
	invoicePDFs, invoicePDFsNew, invoicePDFsOld string
}

func prepareRestoreSwap(paths restoreSwapPaths, packagePath string, manifest backupManifest, maxSchema int) error {
	if exists(paths.databaseOld) || exists(paths.objectsOld) || exists(paths.avatarsOld) || paths.invoicePDFsOld != "" && exists(paths.invoicePDFsOld) {
		return nil
	}
	for _, path := range []string{paths.databaseNew, paths.databaseNew + "-wal", paths.databaseNew + "-shm"} {
		_ = os.Remove(path)
	}
	_ = os.RemoveAll(paths.objectsNew)
	_ = os.RemoveAll(paths.avatarsNew)
	if paths.invoicePDFsNew != "" {
		_ = os.RemoveAll(paths.invoicePDFsNew)
	}
	if _, err := copyVerifiedBackupFile(
		filepath.Join(packagePath, filepath.FromSlash(manifest.Database.Path)),
		paths.databaseNew, manifest.Database.SizeBytes, manifest.Database.SHA256,
	); err != nil {
		return fmt.Errorf("prepare restored database: %w", err)
	}
	temporary, err := database.Open(paths.databaseNew)
	if err != nil {
		return fmt.Errorf("migrate restored database: %w", err)
	}
	if temporary.SchemaVersion != maxSchema {
		_ = temporary.Close()
		return errors.New("prepared restore database did not reach the current schema")
	}
	if err := verifyOpenDatabase(temporary, manifest.DatabaseID, manifest.ArtifactStoreID); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Checkpoint(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Mkdir(paths.objectsNew, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(paths.avatarsNew, 0o700); err != nil {
		return err
	}
	if paths.invoicePDFsNew != "" {
		if err := initializeInvoicePDFRestoreRoot(paths.invoicePDFsNew, manifest.DatabaseID); err != nil {
			return err
		}
	}
	for _, artifact := range manifest.Artifacts {
		var destination string
		if strings.HasPrefix(artifact.Path, invoicePDFControlledPrefix) {
			if paths.invoicePDFsNew == "" {
				return errors.New("restore package contains invoice PDFs without a configured target")
			}
			relative := strings.TrimPrefix(artifact.Path, invoicePDFControlledPrefix)
			destination = filepath.Join(paths.invoicePDFsNew, filepath.FromSlash(relative))
		} else {
			relative := strings.TrimPrefix(artifact.Path, "artifacts/")
			destination = filepath.Join(filepath.Dir(paths.objectsNew), filepath.FromSlash(relative))
			if strings.HasPrefix(relative, "objects/") {
				destination = filepath.Join(paths.objectsNew, strings.TrimPrefix(relative, "objects/"))
			} else if strings.HasPrefix(relative, "avatars/") {
				destination = filepath.Join(paths.avatarsNew, strings.TrimPrefix(relative, "avatars/"))
			}
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if _, err := copyVerifiedBackupFile(
			filepath.Join(packagePath, filepath.FromSlash(artifact.Path)),
			destination, artifact.SizeBytes, artifact.SHA256,
		); err != nil {
			return err
		}
		if err := syncArtifactDirectory(filepath.Dir(destination)); err != nil {
			return err
		}
	}
	if err := syncArtifactDirectory(paths.objectsNew); err != nil {
		return err
	}
	if err := syncArtifactDirectory(paths.avatarsNew); err != nil {
		return err
	}
	if paths.invoicePDFsNew != "" {
		for _, directory := range []string{filepath.Join(paths.invoicePDFsNew, ".staging"), filepath.Join(paths.invoicePDFsNew, ".trash"), paths.invoicePDFsNew} {
			if err := syncArtifactDirectory(directory); err != nil {
				return err
			}
		}
	}
	return syncArtifactDirectory(filepath.Dir(paths.databaseNew))
}

func applyRestoreSwap(paths restoreSwapPaths) error {
	if !exists(paths.databaseOld) {
		if !exists(paths.database) || !exists(paths.databaseNew) {
			return errors.New("restore database swap inputs are incomplete")
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if exists(paths.database + suffix) {
				if err := os.Rename(paths.database+suffix, paths.databaseOld+suffix); err != nil {
					return err
				}
			}
		}
		if err := os.Rename(paths.database, paths.databaseOld); err != nil {
			return err
		}
		if err := syncArtifactDirectory(filepath.Dir(paths.database)); err != nil {
			return err
		}
	}
	if !exists(paths.database) {
		if !exists(paths.databaseNew) {
			return errors.New("prepared restore database is missing")
		}
		if err := os.Rename(paths.databaseNew, paths.database); err != nil {
			return err
		}
	} else if exists(paths.databaseNew) {
		return errors.New("restore database swap is ambiguous")
	}
	if !exists(paths.objectsOld) {
		if !exists(paths.objects) || !exists(paths.objectsNew) {
			return errors.New("restore Artifact swap inputs are incomplete")
		}
		if err := os.Rename(paths.objects, paths.objectsOld); err != nil {
			return err
		}
		if err := syncArtifactDirectory(filepath.Dir(paths.objects)); err != nil {
			return err
		}
	}
	if !exists(paths.objects) {
		if !exists(paths.objectsNew) {
			return errors.New("prepared restore Artifact objects are missing")
		}
		if err := os.Rename(paths.objectsNew, paths.objects); err != nil {
			return err
		}
	} else if exists(paths.objectsNew) {
		return errors.New("restore Artifact swap is ambiguous")
	}
	if err := syncArtifactDirectory(filepath.Dir(paths.objects)); err != nil {
		return err
	}
	if err := swapRestoreDirectory(paths.avatars, paths.avatarsNew, paths.avatarsOld, "workspace avatars"); err != nil {
		return err
	}
	if paths.invoicePDFs != "" {
		if err := swapRestoreDirectory(paths.invoicePDFs, paths.invoicePDFsNew, paths.invoicePDFsOld, "invoice PDFs"); err != nil {
			return err
		}
	}
	return syncArtifactDirectory(filepath.Dir(paths.database))
}

func swapRestoreDirectory(live, prepared, previous, label string) error {
	if !exists(previous) {
		if !exists(live) || !exists(prepared) {
			return fmt.Errorf("restore %s swap inputs are incomplete", label)
		}
		if err := os.Rename(live, previous); err != nil {
			return err
		}
		if err := syncArtifactDirectory(filepath.Dir(live)); err != nil {
			return err
		}
	}
	if !exists(live) {
		if !exists(prepared) {
			return fmt.Errorf("prepared restore %s are missing", label)
		}
		if err := os.Rename(prepared, live); err != nil {
			return err
		}
	} else if exists(prepared) {
		return fmt.Errorf("restore %s swap is ambiguous", label)
	}
	return syncArtifactDirectory(filepath.Dir(live))
}

func verifyLiveRestore(databasePath, artifactRoot, invoicePDFRoot string, manifest backupManifest, maxSchema int) error {
	store, err := database.Open(databasePath)
	if err != nil {
		return err
	}
	if store.SchemaVersion != maxSchema {
		_ = store.Close()
		return errors.New("restored database schema is not current")
	}
	if err := verifyOpenDatabase(store, manifest.DatabaseID, manifest.ArtifactStoreID); err != nil {
		_ = store.Close()
		return err
	}
	artifacts, err := newArtifactStore(artifactRoot, manifest.DatabaseID, manifest.ArtifactStoreID)
	if err != nil {
		_ = store.Close()
		return err
	}
	var invoicePDFs *invoicePDFStore
	if invoicePDFRoot != "" {
		if err := verifyInvoicePDFStoreMarker(invoicePDFRoot, manifest.DatabaseID); err != nil {
			_ = artifacts.close()
			_ = store.Close()
			return err
		}
		invoicePDFs = &invoicePDFStore{
			root: invoicePDFRoot, stagingDir: filepath.Join(invoicePDFRoot, ".staging"), trashDir: filepath.Join(invoicePDFRoot, ".trash"),
		}
	}
	verifyErr := verifyArtifactObjects(store.DB, artifacts, store.SchemaVersion, manifest.ArtifactCount, invoicePDFs)
	closeArtifactErr := artifacts.close()
	checkpointErr := store.Checkpoint()
	closeStoreErr := store.Close()
	return errors.Join(verifyErr, closeArtifactErr, checkpointErr, closeStoreErr)
}

func verifyArtifactObjects(db *gorm.DB, artifacts *artifactStore, schemaVersion, expectedCount int, invoicePDFStores ...*invoicePDFStore) error {
	rows, err := listActiveControlledFiles(db, schemaVersion)
	if err != nil {
		return err
	}
	if len(rows) != expectedCount {
		return errors.New("restored active Artifact count is invalid")
	}
	var invoicePDFs *invoicePDFStore
	if len(invoicePDFStores) != 0 {
		invoicePDFs = invoicePDFStores[0]
	}
	expectedArtifacts := make(map[string]struct{}, len(rows))
	expectedInvoicePDFs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if !validControlledFileRelativePath(row.ID, row.RelativePath, schemaVersion) {
			return errors.New("restored Artifact storage facts are invalid")
		}
		if strings.HasPrefix(row.RelativePath, invoicePDFControlledPrefix) {
			if invoicePDFs == nil {
				return errors.New("restored invoice PDF store is unavailable")
			}
			relative := strings.TrimPrefix(row.RelativePath, invoicePDFControlledPrefix)
			status, verifyErr := invoicePDFs.verify(relative, row.ID, row.SizeBytes, row.SHA256)
			if verifyErr != nil || status != "verified" {
				return errors.New("restored invoice PDF failed integrity verification")
			}
			expectedInvoicePDFs[relative] = struct{}{}
			continue
		}
		path, err := artifacts.resolveControlledFile(row.RelativePath)
		if err != nil {
			return err
		}
		matched, err := artifactFileMatches(path, row.SizeBytes, row.SHA256)
		if err != nil || !matched {
			return errors.New("restored Artifact object failed integrity verification")
		}
		expectedArtifacts[row.RelativePath] = struct{}{}
	}
	if err := verifyControlledFileDirectories(artifacts, expectedArtifacts); err != nil {
		return err
	}
	if invoicePDFs != nil {
		return verifyInvoicePDFDirectories(invoicePDFs, expectedInvoicePDFs)
	}
	return nil
}

func verifyControlledFileDirectories(artifacts *artifactStore, expected map[string]struct{}) error {
	for prefix, directory := range map[string]string{"objects": artifacts.objectsDir, "avatars": artifacts.avatarsDir} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return errors.New("restored controlled file root contains a non-regular file")
			}
			if _, ok := expected[prefix+"/"+entry.Name()]; !ok {
				return errors.New("restored controlled file root contains an unexpected file")
			}
		}
	}
	return nil
}

func verifyInvoicePDFDirectories(store *invoicePDFStore, expected map[string]struct{}) error {
	if store == nil {
		return errors.New("invoice PDF store is unavailable")
	}
	if err := requireSafeDirectory(store.root); err != nil {
		return err
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(store.root, name)
		if name == backupInvoicePDFMarkerName || name == artifactStoreLockName {
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("invoice PDF store control file is unsafe")
			}
			continue
		}
		if name == ".staging" || name == ".trash" {
			if err := requireEmptySafeDirectory(path); err != nil {
				return err
			}
			continue
		}
		if !canonicalInvoicePDFUUID(name) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("invoice PDF store contains an unexpected entry")
		}
		if err := requireSafeDirectory(path); err != nil {
			return err
		}
		files, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, file := range files {
			info, infoErr := file.Info()
			if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("invoice PDF owner directory contains an unsafe entry")
			}
			relative := filepath.ToSlash(filepath.Join(name, file.Name()))
			if _, ok := expected[relative]; !ok {
				return errors.New("invoice PDF store contains an unexpected file")
			}
		}
	}
	return nil
}

func requireEmptySafeDirectory(path string) error {
	if err := requireSafeDirectory(path); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("invoice PDF transient directory is not empty")
	}
	return nil
}

func rollbackRestoreSwap(paths restoreSwapPaths) error {
	var result error
	if paths.invoicePDFsOld != "" && exists(paths.invoicePDFsOld) {
		if exists(paths.invoicePDFs) {
			result = errors.Join(result, os.RemoveAll(paths.invoicePDFs))
		}
		if err := os.Rename(paths.invoicePDFsOld, paths.invoicePDFs); err != nil {
			result = errors.Join(result, err)
		}
	}
	if exists(paths.objectsOld) {
		if exists(paths.objects) {
			result = errors.Join(result, os.RemoveAll(paths.objects))
		}
		if err := os.Rename(paths.objectsOld, paths.objects); err != nil {
			result = errors.Join(result, err)
		}
	}
	if exists(paths.avatarsOld) {
		if exists(paths.avatars) {
			result = errors.Join(result, os.RemoveAll(paths.avatars))
		}
		if err := os.Rename(paths.avatarsOld, paths.avatars); err != nil {
			result = errors.Join(result, err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if !exists(paths.databaseOld + suffix) {
			continue
		}
		if exists(paths.database + suffix) {
			result = errors.Join(result, os.Remove(paths.database+suffix))
		}
		result = errors.Join(result, os.Rename(paths.databaseOld+suffix, paths.database+suffix))
	}
	result = errors.Join(result, syncArtifactDirectory(filepath.Dir(paths.database)))
	result = errors.Join(result, syncArtifactDirectory(filepath.Dir(paths.objects)))
	result = errors.Join(result, syncArtifactDirectory(filepath.Dir(paths.avatars)))
	if paths.invoicePDFs != "" {
		result = errors.Join(result, syncArtifactDirectory(filepath.Dir(paths.invoicePDFs)))
	}
	return result
}

func cleanupSuccessfulRestore(paths restoreSwapPaths, appliedPath, backupRoot string) error {
	var result error
	for _, path := range []string{paths.databaseOld, paths.databaseOld + "-wal", paths.databaseOld + "-shm", paths.databaseNew, paths.databaseNew + "-wal", paths.databaseNew + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	for _, path := range []string{paths.objectsOld, paths.objectsNew, paths.avatarsOld, paths.avatarsNew, paths.invoicePDFsOld, paths.invoicePDFsNew} {
		if path == "" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := syncArtifactDirectory(filepath.Dir(paths.database)); err != nil {
		result = errors.Join(result, err)
	}
	if err := syncArtifactDirectory(filepath.Dir(paths.objects)); err != nil {
		result = errors.Join(result, err)
	}
	if err := syncArtifactDirectory(filepath.Dir(paths.avatars)); err != nil {
		result = errors.Join(result, err)
	}
	if paths.invoicePDFs != "" {
		if err := syncArtifactDirectory(filepath.Dir(paths.invoicePDFs)); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return result
	}
	if err := os.RemoveAll(appliedPath); err != nil {
		return err
	}
	return syncArtifactDirectory(backupRoot)
}

func quarantineFailedRestorePlan(backupRoot, pendingPath, operationID string) error {
	failedPath := filepath.Join(backupRoot, ".restore-failed-"+operationID)
	if err := os.Rename(pendingPath, failedPath); err != nil {
		return err
	}
	return syncArtifactDirectory(backupRoot)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
