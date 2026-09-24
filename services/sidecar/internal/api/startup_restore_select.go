package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

// StartupRestoreChoice is a sanitized, metadata-only backup package fact for
// the pre-database-open recovery picker. It never includes filesystem paths,
// hashes, notes with private content is truncated by the caller if needed, or
// raw manifest errors.
type StartupRestoreChoice struct {
	ID                 string `json:"id"`
	CreatedAt          string `json:"created_at"`
	VerificationStatus string `json:"verification_status"`
	Kind               string `json:"kind"`
	SchemaVersion      int    `json:"schema_version"`
	Note               string `json:"note,omitempty"`
}

// ListStartupRestoreChoices reads backup packages from disk without opening
// the live business database. Invalid packages are listed as invalid so the
// user can see them but cannot select them for restore.
func ListStartupRestoreChoices(backupDir string) ([]StartupRestoreChoice, error) {
	root := strings.TrimSpace(backupDir)
	if root == "" {
		return nil, errors.New("backup directory is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve backup root: %w", err)
	}
	entries, err := os.ReadDir(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []StartupRestoreChoice{}, nil
		}
		return nil, err
	}
	choices := make([]StartupRestoreChoice, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		id := strings.ToLower(entry.Name())
		if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
			continue
		}
		packagePath := filepath.Join(absoluteRoot, entry.Name())
		manifest, err := readBackupManifest(packagePath)
		if err != nil {
			choices = append(choices, StartupRestoreChoice{
				ID: id, VerificationStatus: "invalid", Kind: "unknown",
			})
			continue
		}
		if err := validateBackupManifest(manifest, id, 0); err != nil {
			choices = append(choices, StartupRestoreChoice{
				ID: id, CreatedAt: manifest.CreatedAt, VerificationStatus: "invalid",
				Kind: "unknown", SchemaVersion: manifest.SchemaVersion,
			})
			continue
		}
		status := "unverified"
		if manifest.VerifiedAt != "" {
			status = "verified"
		}
		note := strings.TrimSpace(manifest.Note)
		if len(note) > 120 {
			note = note[:120]
		}
		choices = append(choices, StartupRestoreChoice{
			ID: id, CreatedAt: manifest.CreatedAt, VerificationStatus: status,
			Kind: strings.TrimSpace(manifest.Kind), SchemaVersion: manifest.SchemaVersion,
			Note: note,
		})
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].CreatedAt == choices[j].CreatedAt {
			return choices[i].ID > choices[j].ID
		}
		return choices[i].CreatedAt > choices[j].CreatedAt
	})
	const maxChoices = 20
	if len(choices) > maxChoices {
		choices = choices[:maxChoices]
	}
	return choices, nil
}

// PrepareStartupRestore schedules a pending restore for one existing backup
// package so the next normal Sidecar start applies it before opening live
// business resources. It is the one-shot desktop recovery-page path used when
// the HTTP API is unavailable. Failures leave current data unchanged.
func PrepareStartupRestore(cfg PrepareStartupRestoreConfig) (ScheduledRestoreResult, error) {
	backupID := strings.ToLower(strings.TrimSpace(cfg.BackupID))
	if parsed, err := uuid.Parse(backupID); err != nil || parsed.String() != backupID {
		return ScheduledRestoreResult{}, errors.New("backup id must be a canonical UUID")
	}
	if !cfg.Confirm {
		return ScheduledRestoreResult{}, errors.New("confirm must be true to schedule a restore")
	}
	if strings.TrimSpace(cfg.DatabasePath) == "" || strings.TrimSpace(cfg.BackupDir) == "" ||
		strings.TrimSpace(cfg.ArtifactDir) == "" {
		return ScheduledRestoreResult{}, errors.New("database, backup and artifact paths are required")
	}

	runLease, err := acquireRunLease(cfg.DatabasePath)
	if err != nil {
		return ScheduledRestoreResult{}, err
	}
	defer func() { _ = runLease.Close() }()

	store, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return ScheduledRestoreResult{}, fmt.Errorf("open current workspace database: %w", err)
	}
	defer func() { _ = store.Close() }()

	artifacts, err := openWorkspaceArtifactStore(store.DB, cfg.ArtifactDir, true)
	if err != nil {
		return ScheduledRestoreResult{}, fmt.Errorf("open Artifact store: %w", err)
	}
	defer func() { _ = artifacts.close() }()

	backups, err := newBackupStore(cfg.BackupDir, cfg.DatabasePath, artifacts)
	if err != nil {
		return ScheduledRestoreResult{}, fmt.Errorf("open backup store: %w", err)
	}

	var invoicePDFs *invoicePDFStore
	if strings.TrimSpace(cfg.InvoicePDFDir) != "" {
		invoicePDFs, err = openInvoicePDFStore(store.DB, cfg.InvoicePDFDir)
		if err != nil {
			return ScheduledRestoreResult{}, fmt.Errorf("open invoice PDF store: %w", err)
		}
		defer func() { _ = invoicePDFs.close() }()
	}

	options := Options{
		AppVersion:    cfg.AppVersion,
		Commit:        cfg.Commit,
		SchemaVersion: store.SchemaVersion,
		ArtifactDir:   cfg.ArtifactDir,
		InvoicePDFDir: cfg.InvoicePDFDir,
		DatabasePath:  cfg.DatabasePath,
		BackupDir:     cfg.BackupDir,
		Now:           cfg.Now,
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	service := &API{
		db: store.DB, options: options,
		artifactStore: artifacts, invoicePDFStore: invoicePDFs, backupStore: backups,
	}
	return service.scheduleRestorePackage(backupID)
}

// PrepareStartupRestoreConfig is the one-shot desktop recovery configuration.
// Paths stay process-local and never enter the recovery-page response.
type PrepareStartupRestoreConfig struct {
	BackupID       string
	Confirm        bool
	DatabasePath   string
	BackupDir      string
	ArtifactDir    string
	InvoicePDFDir  string
	AppVersion     string
	Commit         string
	Now            func() time.Time
	AcquireRunLock func(databasePath string) (ioCloser, error)
}

// ioCloser is the minimal run-lease surface used by the one-shot path.
type ioCloser interface {
	Close() error
}

var acquireRunLease = func(databasePath string) (ioCloser, error) {
	return defaultAcquireRunLease(databasePath)
}

// ScheduledRestoreResult is the public one-shot schedule result. Identifiers
// are canonical UUIDs; no filesystem path is included.
type ScheduledRestoreResult = scheduledRestoreResult

func (a *API) scheduleRestorePackage(id string) (scheduledRestoreResult, error) {
	if a.backupStore == nil {
		return scheduledRestoreResult{}, errors.New("verified local backups are unavailable")
	}
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	unlockInvoicePDFs := a.lockInvoicePDFStore()
	defer unlockInvoicePDFs()

	if existing, found, err := loadPendingRestore(a.backupStore.root); err != nil {
		return scheduledRestoreResult{}, errors.New("an existing pending restore could not be validated")
	} else if found {
		if existing.BackupID != id {
			return scheduledRestoreResult{}, errors.New("a different backup is already pending restore")
		}
		return scheduledRestoreResult{
			BackupID: existing.BackupID, RollbackBackupID: existing.RollbackBackupID,
			RequestedAt: existing.RequestedAt, RestartRequired: true,
		}, nil
	}

	packagePath := filepath.Join(a.backupStore.root, id)
	if info, err := os.Lstat(packagePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return scheduledRestoreResult{}, errors.New("backup not found")
		}
		return scheduledRestoreResult{}, errors.New("the restore source could not be inspected")
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return scheduledRestoreResult{}, errors.New("the local backup package is invalid")
	}
	manifest, err := a.backupStore.verifyPackage(packagePath, id, a.options.SchemaVersion)
	if err != nil {
		return scheduledRestoreResult{}, errors.New("the local backup package failed integrity verification")
	}
	var currentDatabaseID, currentStoreID string
	if err := a.db.Raw(
		"SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1",
	).Row().Scan(&currentDatabaseID, &currentStoreID); err != nil {
		return scheduledRestoreResult{}, errors.New("the current workspace identity could not be read")
	}
	if manifest.DatabaseID != currentDatabaseID || manifest.ArtifactStoreID != currentStoreID || currentStoreID != a.artifactStore.storeID {
		return scheduledRestoreResult{}, errors.New("the backup belongs to a different workspace or Artifact store")
	}
	if _, err := runBackupRestoreDrill(a.backupStore, packagePath, manifest, a.options.SchemaVersion); err != nil {
		return scheduledRestoreResult{}, errors.New("the backup could not be opened safely in an isolated temporary data root")
	}
	pendingPayloadBytes, err := pendingRestoreCapacityPayload(manifest)
	if err != nil {
		return scheduledRestoreResult{}, errors.New("restore storage capacity could not be confirmed; no restore was scheduled")
	}
	if err := a.backupStore.requireCreateCapacity(a.db, a.options, pendingPayloadBytes); err != nil {
		if errors.Is(err, errBackupSpaceInsufficient) {
			return scheduledRestoreResult{}, errors.New("there is not enough storage space to create a rollback backup and stage the restore; no restore was scheduled")
		}
		return scheduledRestoreResult{}, errors.New("restore storage capacity could not be confirmed; no restore was scheduled")
	}

	rollbackNote := "恢复 " + id[:8] + " 前自动回滚点"
	rollback, err := a.backupStore.create(
		a.db, a.options, rollbackNote, "", sha256Hex([]byte(rollbackNote)),
	)
	if err != nil {
		return scheduledRestoreResult{}, errors.New("a rollback backup could not be created; no restore was scheduled")
	}
	requestedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	plan := pendingRestorePlan{
		FormatVersion: pendingRestoreVersion, OperationID: uuid.NewString(),
		BackupID: id, RollbackBackupID: rollback.ID, RequestedAt: requestedAt,
		DatabaseID: manifest.DatabaseID, ArtifactStoreID: manifest.ArtifactStoreID,
		SourceSchema: manifest.SchemaVersion,
	}
	if err := publishPendingRestorePackage(a.backupStore, packagePath, manifest, plan, a.options.SchemaVersion); err != nil {
		return scheduledRestoreResult{}, errors.New("the restore could not be scheduled; current data was not changed")
	}
	return scheduledRestoreResult{
		BackupID: id, RollbackBackupID: rollback.ID, RequestedAt: requestedAt, RestartRequired: true,
	}, nil
}
