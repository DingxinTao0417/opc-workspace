package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
)

const (
	maxBusinessPackageImportBytes           int64 = 2 * 1024 * 1024 * 1024
	maxBusinessPackageManifestBytes               = 4 * 1024 * 1024
	maxBusinessPackageFiles                       = 10_000
	packageImportReplaceConfirmation              = "replace-empty-workspace-with-controlled-files"
	packageImportAppendConfirmation               = "append-to-existing-workspace-with-controlled-files"
	packageImportUpgradeReplaceConfirmation       = "upgrade-and-replace-empty-workspace-with-controlled-files"
	packageImportUpgradeAppendConfirmation        = "upgrade-and-append-to-existing-workspace-with-controlled-files"
)

type businessPackageImportPreview struct {
	FormatVersion         int                              `json:"format_version"`
	SchemaVersion         int                              `json:"schema_version"`
	TargetSchemaVersion   int                              `json:"target_schema_version"`
	ExportedAt            string                           `json:"exported_at"`
	TableCounts           map[string]int                   `json:"table_counts"`
	TotalRows             int                              `json:"total_rows"`
	TargetRows            int                              `json:"target_rows"`
	KeyConflicts          int                              `json:"key_conflicts"`
	ConflictTables        []businessImportTableConflict    `json:"conflict_tables"`
	UpgradeSteps          []businessImportUpgradeStep      `json:"upgrade_steps"`
	SemanticConflicts     int                              `json:"semantic_conflicts"`
	SemanticConflictRules []businessImportSemanticConflict `json:"semantic_conflict_rules"`
	Resolutions           []businessImportResolution       `json:"resolutions"`
	PlanDigest            string                           `json:"plan_digest,omitempty"`
	// SkippedFiles counts package files whose rows are kept or already
	// present; they are not published again.
	SkippedFiles  int    `json:"skipped_files"`
	FileCount     int    `json:"file_count"`
	FileBytes     int64  `json:"file_bytes"`
	FileConflicts int    `json:"file_conflicts"`
	CanApply      bool   `json:"can_apply"`
	ApplyMode     string `json:"apply_mode,omitempty"`
	// RehearsalFailure is set with the apply_rehearsal_failed blocker.
	RehearsalFailure *businessImportRehearsalFailure `json:"rehearsal_failure,omitempty"`
	Blocker          string                          `json:"blocker,omitempty"`
}

type businessPackageImportResult struct {
	ImportedRows  int    `json:"imported_rows"`
	ImportedFiles int    `json:"imported_files"`
	BackupID      string `json:"backup_id"`
	ApplyMode     string `json:"apply_mode"`
}

type businessPackageImportArchive struct {
	path     string
	archive  *zip.ReadCloser
	manifest businessPackageManifest
	business businessExportPackage
	entries  map[string]*zip.File
}

type businessPackageImportFile struct {
	manifest businessPackageFile
	mimeType string
}

type stagedBusinessPackageFile struct {
	file       filestore.StagedArtifactFile
	invoicePDF *stagedInvoicePDF
	relative   string
	avatar     bool
}

func (a *API) previewBusinessPackageImport(c *gin.Context) {
	packageArchive, err := a.decodeBusinessPackageImport(c)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	defer packageArchive.cleanup()
	preview, _, _, err := a.validateBusinessPackageImport(c, packageArchive)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

func (a *API) applyBusinessPackageImport(c *gin.Context) {
	confirmation := strings.TrimSpace(c.GetHeader("X-Import-Confirmation"))
	if confirmation != packageImportReplaceConfirmation && confirmation != packageImportAppendConfirmation &&
		confirmation != packageImportUpgradeReplaceConfirmation && confirmation != packageImportUpgradeAppendConfirmation {
		writeError(c, http.StatusPreconditionRequired, "IMPORT_CONFIRMATION_REQUIRED", "Explicit controlled-file package import confirmation is required")
		return
	}
	if a.backupStore == nil || a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups and controlled storage are required before import")
		return
	}
	packageArchive, err := a.decodeBusinessPackageImport(c)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	defer packageArchive.cleanup()

	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	unlockInvoicePDFs := a.lockInvoicePDFStore()
	defer unlockInvoicePDFs()
	preview, expected, plan, err := a.validateBusinessPackageImport(c, packageArchive)
	if err != nil {
		writeBusinessImportError(c, err)
		return
	}
	if !preview.CanApply {
		writeBusinessImportBlocker(c, preview.Blocker)
		return
	}
	if !validBusinessImportConfirmation(confirmation, preview.ApplyMode, true, preview.SchemaVersion < preview.TargetSchemaVersion) {
		writeError(c, http.StatusPreconditionRequired, "IMPORT_CONFIRMATION_REQUIRED", "The confirmation does not match the current controlled-file import mode")
		return
	}
	if !confirmedBusinessImportPlan(c, preview.PlanDigest) {
		return
	}
	expectedControlledFileCount := len(expected)
	if preview.ApplyMode == importModeAppend {
		active, err := listActiveControlledFiles(a.db.WithContext(c.Request.Context()), a.options.SchemaVersion)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The existing controlled-file facts could not be checked")
			return
		}
		expectedControlledFileCount += len(active)
	}
	if err := a.backupStore.requireCreateCapacity(a.db.WithContext(c.Request.Context()), a.options, 0); err != nil {
		writeImportRollbackCapacityError(c, err)
		return
	}

	note := "自动含文件导入前回滚备份"
	if preview.ApplyMode == importModeAppend {
		note = "自动含文件追加导入前回滚备份"
	}
	backup, err := a.backupStore.create(
		a.db.WithContext(c.Request.Context()), a.options, note, "", sha256Hex([]byte(note)),
	)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "IMPORT_BACKUP_FAILED", "A verified rollback backup could not be created; existing data was not changed")
		return
	}
	staged, err := a.stageBusinessPackageFiles(packageArchive, expected)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_PACKAGE_APPLY_FAILED", "Controlled files could not be staged; business data was not imported")
		return
	}
	defer a.discardStagedBusinessPackageFiles(staged)
	committed, err := a.commitBusinessPackageFiles(staged)
	if err != nil {
		a.removeCommittedBusinessPackageFiles(committed)
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_PACKAGE_APPLY_FAILED", "Controlled files could not be committed; business data was not imported")
		return
	}
	applied := false
	defer func() {
		if !applied {
			a.removeCommittedBusinessPackageFiles(committed)
		}
	}()
	if err := a.applyBusinessTables(c, plan.apply, preview.ApplyMode, func(tx *gorm.DB) error {
		if err := plan.verifyUntouched(tx); err != nil {
			return err
		}
		return verifyArtifactObjects(tx, a.artifactStore, a.options.SchemaVersion, expectedControlledFileCount, a.invoicePDFStore)
	}); err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("business package import failed after rollback backup backup_id=%s: %v", backup.ID, err)
		}
		writeError(c, http.StatusUnprocessableEntity, "IMPORT_PACKAGE_APPLY_FAILED", "Business data or controlled files failed integrity validation and were not imported")
		return
	}
	applied = true
	c.JSON(http.StatusOK, gin.H{"data": businessPackageImportResult{
		ImportedRows: plan.appliedRows(), ImportedFiles: len(expected), BackupID: backup.ID, ApplyMode: preview.ApplyMode,
	}})
}

func (a *API) decodeBusinessPackageImport(c *gin.Context) (*businessPackageImportArchive, error) {
	if a.backupStore == nil || a.artifactStore == nil {
		return nil, &businessImportError{http.StatusServiceUnavailable, "IMPORT_PACKAGE_UNAVAILABLE", "Controlled-file package import is unavailable"}
	}
	temporary, err := os.CreateTemp(a.backupStore.root, ".business-import-*.zip")
	if err != nil {
		return nil, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The import package could not be staged"}
	}
	path := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The import package could not be protected"}
	}
	limited := http.MaxBytesReader(c.Writer, c.Request.Body, maxBusinessPackageImportBytes)
	written, err := io.Copy(temporary, limited)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return nil, &businessImportError{http.StatusRequestEntityTooLarge, "IMPORT_PACKAGE_TOO_LARGE", "The import package must not exceed 2 GiB"}
	}
	if err != nil || written == 0 {
		return nil, &businessImportError{http.StatusBadRequest, "INVALID_IMPORT_PACKAGE", "The import package is not a readable ZIP"}
	}
	if err := temporary.Close(); err != nil {
		return nil, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The import package could not be staged"}
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, &businessImportError{http.StatusBadRequest, "INVALID_IMPORT_PACKAGE", "The import package is not a readable ZIP"}
	}
	result := &businessPackageImportArchive{path: path, archive: archive}
	if err := result.readAndVerify(); err != nil {
		_ = archive.Close()
		return nil, err
	}
	keep = true
	return result, nil
}

func (p *businessPackageImportArchive) cleanup() {
	if p == nil {
		return
	}
	if p.archive != nil {
		_ = p.archive.Close()
	}
	if p.path != "" {
		_ = os.Remove(p.path)
	}
}

func (p *businessPackageImportArchive) readAndVerify() error {
	if len(p.archive.File) < 2 || len(p.archive.File) > maxBusinessPackageFiles+2 {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package entry count is invalid"}
	}
	p.entries = make(map[string]*zip.File, len(p.archive.File))
	for _, entry := range p.archive.File {
		name := entry.Name
		if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || pathpkg.Clean(name) != name || !entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package contains an unsafe entry"}
		}
		if _, exists := p.entries[name]; exists {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package contains duplicate entries"}
		}
		p.entries[name] = entry
	}
	manifestEntry := p.entries["manifest.json"]
	businessEntry := p.entries["business-data.json"]
	if manifestEntry == nil || businessEntry == nil {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package metadata files are missing"}
	}
	manifestRaw, err := readBusinessPackageEntry(manifestEntry, maxBusinessPackageManifestBytes)
	if err != nil || decodeStrictPackageJSON(manifestRaw, &p.manifest, false) != nil {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package manifest is invalid"}
	}
	if p.manifest.FormatVersion != businessPackageFormatVersion || p.manifest.FileCount < 0 || p.manifest.FileCount > maxBusinessPackageFiles || len(p.manifest.Files) != p.manifest.FileCount || p.manifest.FileBytes < 0 || p.manifest.TotalBytes < 0 {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package manifest summary is invalid"}
	}
	if p.manifest.BusinessData.ID != "" || p.manifest.BusinessData.Path != "business-data.json" || p.manifest.BusinessData.SizeBytes < 1 || p.manifest.BusinessData.SizeBytes > maxBusinessImportBytes || !validPackageSHA256(p.manifest.BusinessData.SHA256) {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The packaged business data descriptor is invalid"}
	}
	businessRaw, err := readBusinessPackageEntry(businessEntry, p.manifest.BusinessData.SizeBytes)
	if err != nil || int64(len(businessRaw)) != p.manifest.BusinessData.SizeBytes || sha256Hex(businessRaw) != p.manifest.BusinessData.SHA256 || decodeStrictPackageJSON(businessRaw, &p.business, true) != nil {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_FILE_INVALID", "The packaged business data failed integrity validation"}
	}
	if p.business.ExportedAt != p.manifest.ExportedAt || p.business.Source != p.manifest.Source {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package source metadata is inconsistent"}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.manifest.ExportedAt); err != nil {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package export time is invalid"}
	}
	seenIDs := make(map[string]struct{}, len(p.manifest.Files))
	seenPaths := make(map[string]struct{}, len(p.manifest.Files))
	var fileBytes int64
	previousID := ""
	for _, file := range p.manifest.Files {
		relative := strings.TrimPrefix(file.Path, "files/")
		parsed, parseErr := uuid.Parse(file.ID)
		if parseErr != nil || parsed.String() != file.ID || file.Path != "files/"+relative || !validControlledFileRelativePath(file.ID, relative, p.business.Source.SchemaVersion) || file.SizeBytes < 1 || !validPackageSHA256(file.SHA256) || (previousID != "" && previousID >= file.ID) {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "A controlled-file descriptor is invalid"}
		}
		if strings.HasPrefix(relative, "avatars/") && file.SizeBytes > filestore.MaxWorkspaceAvatarBytes ||
			strings.HasPrefix(relative, "objects/") && file.SizeBytes > filestore.MaxArtifactFileBytes ||
			strings.HasPrefix(relative, invoicePDFControlledPrefix) && file.SizeBytes > maxInvoicePDFBytes {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "A controlled file exceeds its allowed size"}
		}
		if _, exists := seenIDs[file.ID]; exists {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file IDs are duplicated"}
		}
		if _, exists := seenPaths[file.Path]; exists {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file paths are duplicated"}
		}
		entry := p.entries[file.Path]
		if entry == nil || int64(entry.UncompressedSize64) != file.SizeBytes || verifyBusinessPackageEntry(entry, file.SizeBytes, file.SHA256) != nil {
			return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_FILE_INVALID", "A controlled file failed integrity validation"}
		}
		seenIDs[file.ID] = struct{}{}
		seenPaths[file.Path] = struct{}{}
		previousID = file.ID
		if fileBytes > maxBusinessPackageImportBytes-file.SizeBytes {
			return &businessImportError{http.StatusRequestEntityTooLarge, "IMPORT_PACKAGE_TOO_LARGE", "The uncompressed controlled files exceed 2 GiB"}
		}
		fileBytes += file.SizeBytes
	}
	if len(p.entries) != len(p.manifest.Files)+2 || p.manifest.FileBytes != fileBytes || p.manifest.TotalBytes != fileBytes+int64(len(businessRaw)) {
		return &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "The package file totals or entry set are invalid"}
	}
	return nil
}

func decodeStrictPackageJSON(payload []byte, destination any, useNumber bool) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if useNumber {
		decoder.UseNumber()
	}
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing content")
	}
	return nil
}

func readBusinessPackageEntry(entry *zip.File, limit int64) ([]byte, error) {
	if limit < 1 || entry.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("package entry exceeds its limit")
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(payload)) > limit {
		return nil, errors.Join(readErr, closeErr, errors.New("package entry could not be read"))
	}
	return payload, nil
}

func verifyBusinessPackageEntry(entry *zip.File, expectedSize int64, expectedHash string) error {
	if expectedSize < 1 || int64(entry.UncompressedSize64) != expectedSize {
		return errors.New("package entry size does not match")
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(hasher, io.LimitReader(reader, expectedSize+1))
	closeErr := reader.Close()
	if copyErr != nil || closeErr != nil || written != expectedSize || hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return errors.Join(copyErr, closeErr, errors.New("package entry integrity does not match"))
	}
	return nil
}

func validPackageSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

// validateBusinessPackageImport checks the package against its own manifest in
// full, then returns the files that the import plan will actually publish.
func (a *API) validateBusinessPackageImport(c *gin.Context, packageArchive *businessPackageImportArchive) (businessPackageImportPreview, map[string]businessPackageImportFile, *businessImportPlan, error) {
	base, plan, err := a.validateBusinessImportWithFiles(c, &packageArchive.business)
	if err != nil {
		return businessPackageImportPreview{}, nil, nil, err
	}
	preview := businessPackageImportPreview{
		FormatVersion: base.FormatVersion, SchemaVersion: base.SchemaVersion,
		TargetSchemaVersion: base.TargetSchemaVersion, ExportedAt: base.ExportedAt,
		TableCounts: base.TableCounts, TotalRows: base.TotalRows,
		TargetRows: base.TargetRows, KeyConflicts: base.KeyConflicts, ConflictTables: base.ConflictTables,
		UpgradeSteps:      base.UpgradeSteps,
		SemanticConflicts: base.SemanticConflicts, SemanticConflictRules: base.SemanticConflictRules,
		Resolutions: base.Resolutions, PlanDigest: base.PlanDigest,
		FileCount: packageArchive.manifest.FileCount, FileBytes: packageArchive.manifest.FileBytes,
		CanApply: base.CanApply, ApplyMode: base.ApplyMode, Blocker: base.Blocker,
		RehearsalFailure: base.RehearsalFailure,
	}
	if base.Blocker == "source_schema_older" || base.Blocker == "source_schema_newer" {
		return preview, nil, nil, nil
	}
	declared, err := controlledImportFiles(packageArchive.business, a.options.SchemaVersion)
	if err != nil {
		return businessPackageImportPreview{}, nil, nil, err
	}
	if int64(len(declared)) != packageArchive.business.ArtifactFiles.ActiveCount || int64(len(declared)) != int64(packageArchive.manifest.FileCount) {
		return businessPackageImportPreview{}, nil, nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file counts do not match business metadata"}
	}
	var fileBytes int64
	for _, descriptor := range packageArchive.manifest.Files {
		declaredFile, ok := declared[descriptor.Path]
		if !ok || declaredFile.manifest != descriptor {
			return businessPackageImportPreview{}, nil, nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file manifest does not match business metadata"}
		}
		fileBytes += descriptor.SizeBytes
	}
	if fileBytes != packageArchive.business.ArtifactFiles.ActiveBytes || fileBytes != packageArchive.manifest.FileBytes {
		return businessPackageImportPreview{}, nil, nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file byte totals do not match business metadata"}
	}
	// Only the files of rows that will be written are published. A row the
	// plan keeps or skips already has its file in the target.
	expected, err := controlledImportFiles(plan.apply, a.options.SchemaVersion)
	if err != nil {
		return businessPackageImportPreview{}, nil, nil, err
	}
	publish := make([]businessPackageFile, 0, len(expected))
	for _, descriptor := range packageArchive.manifest.Files {
		if _, ok := expected[descriptor.Path]; ok {
			publish = append(publish, descriptor)
		}
	}
	preview.SkippedFiles = len(declared) - len(expected)
	fileConflicts, err := a.countControlledImportFileConflicts(publish)
	if err != nil {
		return businessPackageImportPreview{}, nil, nil, &businessImportError{http.StatusInternalServerError, "IMPORT_PREFLIGHT_FAILED", "The controlled-file target could not be checked"}
	}
	preview.FileConflicts = fileConflicts
	if base.CanApply && fileConflicts != 0 {
		preview.CanApply = false
		preview.Blocker = "target_file_conflicts"
		preview.ApplyMode = ""
		preview.PlanDigest = ""
	}
	return preview, expected, plan, nil
}

func (a *API) countControlledImportFileConflicts(files []businessPackageFile) (int, error) {
	active, err := listActiveControlledFiles(a.db, a.options.SchemaVersion)
	if err != nil {
		return 0, err
	}
	activeIDs := make(map[string]struct{}, len(active))
	for _, file := range active {
		activeIDs[file.ID] = struct{}{}
	}
	conflicts := 0
	for _, descriptor := range files {
		_, conflictsByID := activeIDs[descriptor.ID]
		relative := strings.TrimPrefix(descriptor.Path, "files/")
		var target string
		var err error
		if strings.HasPrefix(relative, invoicePDFControlledPrefix) {
			if a.invoicePDFStore == nil {
				return 0, errors.New("invoice PDF store is unavailable")
			}
			target, err = a.invoicePDFStore.resolve(strings.TrimPrefix(relative, invoicePDFControlledPrefix), descriptor.ID)
			if err == nil {
				if parentErr := filestore.RequireSafeDirectory(filepath.Dir(target)); parentErr != nil && !errors.Is(parentErr, os.ErrNotExist) {
					return 0, parentErr
				}
			}
		} else if strings.HasPrefix(relative, "avatars/") {
			target, err = a.artifactStore.ResolveWorkspaceAvatar(relative)
		} else {
			target, err = a.artifactStore.ResolveObject(relative)
		}
		if err != nil {
			return 0, err
		}
		conflictsByPath := false
		if _, err := os.Lstat(target); err == nil {
			conflictsByPath = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		if conflictsByID || conflictsByPath {
			conflicts++
		}
	}
	return conflicts, nil
}

func controlledImportFiles(packageData businessExportPackage, schemaVersion int) (map[string]businessPackageImportFile, error) {
	tables := make(map[string]businessExportTable, len(packageData.Tables))
	for _, table := range packageData.Tables {
		tables[table.Name] = table
	}
	result := make(map[string]businessPackageImportFile)
	for _, name := range []string{"task_artifacts", "client_attachments", "project_attachments", "workspace_avatars"} {
		table := tables[name]
		idIndex := columnIndex(table.Columns, "id")
		relativeIndex := columnIndex(table.Columns, "relative_path")
		mimeIndex := columnIndex(table.Columns, "mime_type")
		sizeIndex := columnIndex(table.Columns, "size_bytes")
		hashIndex := columnIndex(table.Columns, "sha256")
		deletedIndex := columnIndex(table.Columns, "deleted_at")
		storageIndex := columnIndex(table.Columns, "storage_kind")
		if idIndex < 0 || relativeIndex < 0 || mimeIndex < 0 || sizeIndex < 0 || hashIndex < 0 || deletedIndex < 0 || name == "task_artifacts" && storageIndex < 0 {
			return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file table columns are incomplete"}
		}
		for _, row := range table.Rows {
			if row[deletedIndex] != nil {
				continue
			}
			if name == "task_artifacts" {
				storageKind, ok := row[storageIndex].(string)
				if !ok {
					return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_ROW_INVALID", "Task Artifact storage kind is invalid"}
				}
				if storageKind != "file" {
					continue
				}
			}
			id, idOK := row[idIndex].(string)
			relative, relativeOK := row[relativeIndex].(string)
			mimeType, mimeOK := row[mimeIndex].(string)
			hash, hashOK := row[hashIndex].(string)
			size, sizeOK := importInteger(row[sizeIndex])
			if !idOK || !relativeOK || !mimeOK || !hashOK || !sizeOK || size < 1 || !validControlledFileRelativePath(id, relative, schemaVersion) || !validPackageSHA256(hash) {
				return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file business metadata is invalid"}
			}
			path := "files/" + relative
			if _, exists := result[path]; exists {
				return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file business paths are duplicated"}
			}
			result[path] = businessPackageImportFile{
				manifest: businessPackageFile{ID: id, Path: path, SizeBytes: size, SHA256: hash}, mimeType: mimeType,
			}
		}
	}
	invoiceTable := tables["invoice_pdf_assets"]
	idIndex := columnIndex(invoiceTable.Columns, "id")
	invoiceIndex := columnIndex(invoiceTable.Columns, "invoice_id")
	relativeIndex := columnIndex(invoiceTable.Columns, "relative_path")
	mimeIndex := columnIndex(invoiceTable.Columns, "mime_type")
	sizeIndex := columnIndex(invoiceTable.Columns, "size_bytes")
	hashIndex := columnIndex(invoiceTable.Columns, "sha256")
	if idIndex < 0 || invoiceIndex < 0 || relativeIndex < 0 || mimeIndex < 0 || sizeIndex < 0 || hashIndex < 0 {
		return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Invoice PDF table columns are incomplete"}
	}
	for _, row := range invoiceTable.Rows {
		id, idOK := row[idIndex].(string)
		invoiceID, invoiceOK := row[invoiceIndex].(string)
		relative, relativeOK := row[relativeIndex].(string)
		mimeType, mimeOK := row[mimeIndex].(string)
		hash, hashOK := row[hashIndex].(string)
		size, sizeOK := importInteger(row[sizeIndex])
		logicalRelative := invoicePDFControlledPrefix + relative
		if !idOK || !invoiceOK || !relativeOK || !mimeOK || !hashOK || !sizeOK || size < 1 || size > maxInvoicePDFBytes ||
			mimeType != invoicePDFMimeType || relative != invoiceID+"/"+id+".pdf" ||
			!validControlledFileRelativePath(id, logicalRelative, schemaVersion) || !validPackageSHA256(hash) {
			return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Invoice PDF business metadata is invalid"}
		}
		path := "files/" + logicalRelative
		if _, exists := result[path]; exists {
			return nil, &businessImportError{http.StatusUnprocessableEntity, "IMPORT_PACKAGE_MANIFEST_INVALID", "Controlled-file business paths are duplicated"}
		}
		result[path] = businessPackageImportFile{
			manifest: businessPackageFile{ID: id, Path: path, SizeBytes: size, SHA256: hash}, mimeType: mimeType,
		}
	}
	return result, nil
}

func (a *API) stageBusinessPackageFiles(packageArchive *businessPackageImportArchive, expected map[string]businessPackageImportFile) ([]stagedBusinessPackageFile, error) {
	staged := make([]stagedBusinessPackageFile, 0, len(packageArchive.manifest.Files))
	for _, descriptor := range packageArchive.manifest.Files {
		// A file whose row the import plan keeps or skips is already in the
		// target and is not published again.
		if _, publish := expected[descriptor.Path]; !publish {
			continue
		}
		entry := packageArchive.entries[descriptor.Path]
		reader, err := entry.Open()
		if err != nil {
			a.discardStagedBusinessPackageFiles(staged)
			return nil, err
		}
		relative := strings.TrimPrefix(descriptor.Path, "files/")
		invoicePDF := strings.HasPrefix(relative, invoicePDFControlledPrefix)
		avatar := strings.HasPrefix(relative, "avatars/")
		metadata := expected[descriptor.Path]
		if invoicePDF {
			if a.invoicePDFStore == nil || metadata.mimeType != invoicePDFMimeType {
				_ = reader.Close()
				a.discardStagedBusinessPackageFiles(staged)
				return nil, errors.New("invoice PDF store is unavailable")
			}
			storageRelative := strings.TrimPrefix(relative, invoicePDFControlledPrefix)
			parts := strings.Split(storageRelative, "/")
			if len(parts) != 2 {
				_ = reader.Close()
				a.discardStagedBusinessPackageFiles(staged)
				return nil, errors.New("invoice PDF package path is invalid")
			}
			file, stageErr := a.invoicePDFStore.stage(parts[0], func(destination io.Writer) error {
				_, copyErr := io.Copy(destination, reader)
				return copyErr
			})
			closeErr := reader.Close()
			if stageErr != nil || closeErr != nil {
				a.discardStagedBusinessPackageFiles(staged)
				return nil, errors.Join(stageErr, closeErr)
			}
			file.assetID = descriptor.ID
			file.relative = storageRelative
			if file.sizeBytes != descriptor.SizeBytes || file.sha256 != descriptor.SHA256 {
				a.invoicePDFStore.discardStaged(file)
				a.discardStagedBusinessPackageFiles(staged)
				return nil, errors.New("staged invoice PDF does not match package metadata")
			}
			staged = append(staged, stagedBusinessPackageFile{invoicePDF: &file, relative: relative})
			continue
		}
		var file filestore.StagedArtifactFile
		if avatar {
			file, _, err = a.artifactStore.StageWorkspaceAvatar(reader, descriptor.ID)
		} else {
			file, err = a.artifactStore.StageMultipartFile(reader, descriptor.ID)
		}
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			a.discardStagedBusinessPackageFiles(staged)
			return nil, errors.Join(err, closeErr)
		}
		if file.RelativePath != relative || file.SizeBytes != descriptor.SizeBytes || file.SHA256 != descriptor.SHA256 || file.MIMEType != metadata.mimeType {
			a.artifactStore.DiscardStagedFile(file)
			a.discardStagedBusinessPackageFiles(staged)
			return nil, errors.New("staged controlled file does not match package metadata")
		}
		staged = append(staged, stagedBusinessPackageFile{file: file, relative: relative, avatar: avatar})
	}
	return staged, nil
}

func (a *API) discardStagedBusinessPackageFiles(staged []stagedBusinessPackageFile) {
	for _, item := range staged {
		if item.invoicePDF != nil {
			a.invoicePDFStore.discardStaged(*item.invoicePDF)
		} else {
			a.artifactStore.DiscardStagedFile(item.file)
		}
	}
}

func (a *API) commitBusinessPackageFiles(staged []stagedBusinessPackageFile) ([]stagedBusinessPackageFile, error) {
	committed := make([]stagedBusinessPackageFile, 0, len(staged))
	for _, item := range staged {
		var err error
		if item.invoicePDF != nil {
			err = a.invoicePDFStore.commit(*item.invoicePDF)
		} else if item.avatar {
			err = a.artifactStore.CommitStagedWorkspaceAvatar(item.file)
		} else {
			err = a.artifactStore.CommitStagedFile(item.file)
		}
		if err != nil {
			return committed, err
		}
		committed = append(committed, item)
	}
	return committed, nil
}

func (a *API) removeCommittedBusinessPackageFiles(committed []stagedBusinessPackageFile) {
	for index := len(committed) - 1; index >= 0; index-- {
		item := committed[index]
		var err error
		if item.invoicePDF != nil {
			err = a.invoicePDFStore.remove(item.invoicePDF.relative, item.invoicePDF.assetID)
		} else if item.avatar {
			err = a.artifactStore.RemoveWorkspaceAvatar(item.relative)
		} else {
			err = a.artifactStore.DiscardCommittedFile(item.relative)
		}
		if err != nil && a.options.Logger != nil {
			a.options.Logger.Printf("business package import compensation failed: %v", err)
		}
	}
}
