package api

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	backupArchiveContentType = "application/zip"
	backupArchiveOverhead    = 1 << 20
)

var errBackupArchiveInvalid = errors.New("backup archive source is invalid")

func (a *API) downloadBackupArchive(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	id := c.Param("id")
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_ID", "Backup id must be a canonical UUID")
		return
	}

	a.backupStore.mu.Lock()
	archive, err := a.backupStore.buildArchive(id, a.options)
	a.backupStore.mu.Unlock()
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			writeError(c, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
		case errors.Is(err, errBackupArchiveInvalid):
			a.logBackupError("export archive validation", err)
			writeError(c, http.StatusConflict, "BACKUP_INVALID", "The local backup package failed integrity verification")
		case errors.Is(err, errBackupSpaceInsufficient):
			writeError(c, http.StatusInsufficientStorage, "BACKUP_EXPORT_SPACE_INSUFFICIENT", "There is not enough storage space to prepare the backup download")
		case errors.Is(err, errBackupCapacityUnavailable):
			writeError(c, http.StatusServiceUnavailable, "BACKUP_EXPORT_CAPACITY_UNAVAILABLE", "Backup storage capacity could not be confirmed; no download archive was created")
		case isBackupArchiveSpaceError(err):
			a.logBackupError("export archive storage", err)
			writeError(c, http.StatusInsufficientStorage, "BACKUP_EXPORT_SPACE_INSUFFICIENT", "There is not enough storage space to prepare the backup download")
		default:
			a.logBackupError("export archive", err)
			writeError(c, http.StatusInternalServerError, "BACKUP_EXPORT_FAILED", "The backup download archive could not be created")
		}
		return
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	info, err := archive.Stat()
	if err != nil || !info.Mode().IsRegular() {
		a.logBackupError("inspect export archive", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_EXPORT_FAILED", "The backup download archive could not be opened")
		return
	}
	filename := "opc-workspace-backup-" + id + ".zip"
	c.Header("Content-Type", backupArchiveContentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Backup-Package-Format-Version", fmt.Sprint(backupFormatVersion))
	c.Header("X-Backup-ID", id)
	http.ServeContent(c.Writer, c.Request, filename, time.Time{}, archive)
}

func isBackupArchiveSpaceError(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	if runtime.GOOS == "windows" {
		// ERROR_HANDLE_DISK_FULL and ERROR_DISK_FULL are surfaced as raw
		// syscall.Errno values by os.File writes and Sync on Windows.
		return errors.Is(err, syscall.Errno(39)) || errors.Is(err, syscall.Errno(112))
	}
	return false
}

func (s *backupStore) buildArchive(id string, options Options) (*os.File, error) {
	packagePath := filepath.Join(s.root, id)
	info, err := os.Lstat(packagePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errBackupArchiveInvalid
	}
	manifest, err := s.verifyPackage(packagePath, id, options.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBackupArchiveInvalid, err)
	}
	manifestJSON, err := encodeBackupArchiveManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBackupArchiveInvalid, err)
	}
	if err := requireBackupArchiveCapacity(s.root, options, manifest, len(manifestJSON)); err != nil {
		return nil, err
	}

	archive, err := os.CreateTemp(s.root, ".archive-"+id+"-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create private backup archive: %w", err)
	}
	removeArchive := true
	defer func() {
		if removeArchive {
			_ = archive.Close()
			_ = os.Remove(archive.Name())
		}
	}()
	if err := archive.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("restrict backup archive permissions: %w", err)
	}

	zipWriter := zip.NewWriter(archive)
	if err := writeBackupArchive(zipWriter, packagePath, manifest, manifestJSON); err != nil {
		_ = zipWriter.Close()
		return nil, err
	}
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("finalize backup archive: %w", err)
	}
	if err := archive.Sync(); err != nil {
		return nil, fmt.Errorf("sync backup archive: %w", err)
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close backup archive: %w", err)
	}
	linkedInfo, err := os.Lstat(archivePath)
	if err != nil || !linkedInfo.Mode().IsRegular() || linkedInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("completed backup archive is not a regular file")
	}
	reopened, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("reopen backup archive: %w", err)
	}
	openedInfo, err := reopened.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(linkedInfo, openedInfo) {
		_ = reopened.Close()
		return nil, errors.New("completed backup archive changed before download")
	}
	removeArchive = false
	return reopened, nil
}

func encodeBackupArchiveManifest(manifest backupManifest) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxBackupManifest {
		return nil, errors.New("backup manifest exceeds 1 MiB")
	}
	return encoded, nil
}

func requireBackupArchiveCapacity(root string, options Options, manifest backupManifest, manifestBytes int) error {
	if manifest.TotalBytes < 0 || manifestBytes < 0 {
		return errBackupCapacityUnavailable
	}
	payload, ok := checkedBackupCapacityAdd(uint64(manifest.TotalBytes), uint64(manifestBytes))
	if !ok {
		return errBackupCapacityUnavailable
	}
	overhead := uint64(backupArchiveOverhead)
	for _, path := range backupArchivePaths(manifest) {
		pathOverhead, ok := checkedBackupCapacityMultiply(uint64(len(path)), 2)
		if !ok {
			return errBackupCapacityUnavailable
		}
		pathOverhead, ok = checkedBackupCapacityAdd(pathOverhead, 256)
		if !ok {
			return errBackupCapacityUnavailable
		}
		overhead, ok = checkedBackupCapacityAdd(overhead, pathOverhead)
		if !ok {
			return errBackupCapacityUnavailable
		}
	}
	payload, ok = checkedBackupCapacityAdd(payload, overhead)
	if !ok {
		return errBackupCapacityUnavailable
	}
	required, err := backupArchiveCapacityRequirement(payload)
	if err != nil {
		return err
	}
	checker := options.DiskSpaceCheck
	if checker == nil {
		checker = diskFreeBytes
	}
	available, total, err := checker(root)
	if err != nil || total == 0 || available > total {
		return errBackupCapacityUnavailable
	}
	if available < required {
		return errBackupSpaceInsufficient
	}
	return nil
}

func backupArchiveCapacityRequirement(payload uint64) (uint64, error) {
	safety := payload / backupCapacitySafetyDivisor
	if payload%backupCapacitySafetyDivisor != 0 {
		safety++
	}
	if safety < backupCapacityMinimumReserve {
		safety = backupCapacityMinimumReserve
	}
	required, ok := checkedBackupCapacityAdd(payload, safety)
	if !ok {
		return 0, errBackupCapacityUnavailable
	}
	return required, nil
}

func backupArchivePaths(manifest backupManifest) []string {
	paths := make([]string, 0, len(manifest.Artifacts)+3)
	paths = append(paths, backupManifestName, manifest.Database.Path, manifest.ArtifactMarker.Path)
	artifacts := append([]backupManifestArtifact(nil), manifest.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

func writeBackupArchive(writer *zip.Writer, packagePath string, manifest backupManifest, manifestJSON []byte) error {
	if err := writeBackupArchiveBytes(writer, backupManifestName, manifestJSON); err != nil {
		return fmt.Errorf("write archive manifest: %w", err)
	}
	files := []backupManifestFile{manifest.Database, manifest.ArtifactMarker}
	artifacts := append([]backupManifestArtifact(nil), manifest.Artifacts...)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	for _, artifact := range artifacts {
		files = append(files, artifact.backupManifestFile)
	}
	for _, file := range files {
		if err := writeVerifiedBackupArchiveFile(writer, packagePath, file); err != nil {
			return err
		}
	}
	return nil
}

func backupArchiveHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o600)
	header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
	return header
}

func writeBackupArchiveBytes(writer *zip.Writer, name string, content []byte) error {
	destination, err := writer.CreateHeader(backupArchiveHeader(name))
	if err != nil {
		return err
	}
	_, err = destination.Write(content)
	return err
}

func writeVerifiedBackupArchiveFile(writer *zip.Writer, packagePath string, expected backupManifestFile) error {
	return writeVerifiedBackupArchiveFileWithOpen(writer, packagePath, expected, os.Open)
}

func writeVerifiedBackupArchiveFileWithOpen(
	writer *zip.Writer,
	packagePath string,
	expected backupManifestFile,
	openSource func(string) (*os.File, error),
) error {
	sourcePath := filepath.Join(packagePath, filepath.FromSlash(expected.Path))
	linkedInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("%w: inspect %s: %v", errBackupArchiveInvalid, expected.Path, err)
	}
	if !linkedInfo.Mode().IsRegular() || linkedInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a regular file", errBackupArchiveInvalid, expected.Path)
	}
	source, err := openSource(sourcePath)
	if err != nil {
		return fmt.Errorf("%w: open %s: %v", errBackupArchiveInvalid, expected.Path, err)
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(linkedInfo, openedInfo) || openedInfo.Size() != expected.SizeBytes {
		return fmt.Errorf("%w: %s changed after verification", errBackupArchiveInvalid, expected.Path)
	}
	destination, err := writer.CreateHeader(backupArchiveHeader(expected.Path))
	if err != nil {
		return fmt.Errorf("create archive entry %s: %w", expected.Path, err)
	}
	hasher := sha256.New()
	reader := io.Reader(source)
	if expected.SizeBytes < math.MaxInt64 {
		reader = io.LimitReader(source, expected.SizeBytes+1)
	}
	size, err := io.Copy(io.MultiWriter(destination, hasher), reader)
	if err != nil {
		return fmt.Errorf("copy archive entry %s: %w", expected.Path, err)
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if size != expected.SizeBytes || actualHash != expected.SHA256 {
		return fmt.Errorf("%w: %s changed after verification", errBackupArchiveInvalid, expected.Path)
	}
	return nil
}
