package api

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const businessPackageFormatVersion = 1

type businessPackageFile struct {
	ID        string `json:"id,omitempty"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type businessPackageManifest struct {
	FormatVersion int                   `json:"format_version"`
	ExportedAt    string                `json:"exported_at"`
	Source        businessExportSource  `json:"source"`
	BusinessData  businessPackageFile   `json:"business_data"`
	Files         []businessPackageFile `json:"files"`
	FileCount     int                   `json:"file_count"`
	FileBytes     int64                 `json:"file_bytes"`
	TotalBytes    int64                 `json:"total_bytes"`
}

func (a *API) exportBusinessPackage(c *gin.Context) {
	if a.backupStore == nil || a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "DATA_PACKAGE_EXPORT_UNAVAILABLE", "Controlled-file business export is unavailable")
		return
	}

	path, exportedAt, err := a.buildBusinessPackageLocked(c)
	if err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("business package export failed: %v", err)
		}
		writeError(c, http.StatusInternalServerError, "DATA_PACKAGE_EXPORT_FAILED", "The controlled-file business package could not be exported")
		return
	}
	defer func() { _ = os.Remove(path) }()

	file, err := os.Open(path)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "DATA_PACKAGE_EXPORT_FAILED", "The controlled-file business package could not be opened")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(c, http.StatusInternalServerError, "DATA_PACKAGE_EXPORT_FAILED", "The controlled-file business package could not be inspected")
		return
	}
	filename := "opc-workspace-business-files-" + exportedAt.Format("20060102T150405Z") + ".zip"
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Business-Package-Format-Version", "1")
	c.Header("Content-Type", "application/zip")
	http.ServeContent(c.Writer, c.Request, filename, exportedAt, file)
}

func (a *API) buildBusinessPackageLocked(c *gin.Context) (string, time.Time, error) {
	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	unlockInvoicePDFs := a.lockInvoicePDFStore()
	defer unlockInvoicePDFs()
	return a.buildBusinessPackage(c)
}

func (a *API) buildBusinessPackage(c *gin.Context) (string, time.Time, error) {
	exportedAt := a.options.Now().UTC()
	packageData, err := a.buildBusinessExport(c, exportedAt)
	if err != nil {
		return "", exportedAt, err
	}
	rows, err := listActiveControlledFiles(a.db.WithContext(c), a.options.SchemaVersion)
	if err != nil {
		return "", exportedAt, fmt.Errorf("list controlled files: %w", err)
	}
	packageData.ArtifactFiles.Included = true
	packageData.ArtifactFiles.ActiveCount = int64(len(rows))
	packageData.ArtifactFiles.ActiveBytes = 0
	files := make([]businessPackageFile, 0, len(rows))
	seenPaths := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if !validControlledFileRelativePath(row.ID, row.RelativePath, a.options.SchemaVersion) {
			return "", exportedAt, errors.New("controlled file path is invalid")
		}
		path := "files/" + row.RelativePath
		if _, exists := seenPaths[path]; exists {
			return "", exportedAt, errors.New("controlled file paths are duplicated")
		}
		seenPaths[path] = struct{}{}
		files = append(files, businessPackageFile{ID: row.ID, Path: path, SizeBytes: row.SizeBytes, SHA256: row.SHA256})
		packageData.ArtifactFiles.ActiveBytes += row.SizeBytes
	}
	businessJSON, err := json.MarshalIndent(packageData, "", "  ")
	if err != nil {
		return "", exportedAt, fmt.Errorf("encode business data: %w", err)
	}
	businessJSON = append(businessJSON, '\n')
	businessHash := sha256.Sum256(businessJSON)
	manifest := businessPackageManifest{
		FormatVersion: businessPackageFormatVersion,
		ExportedAt:    exportedAt.Format("2006-01-02T15:04:05.000000000Z"),
		Source:        packageData.Source,
		BusinessData: businessPackageFile{
			Path: "business-data.json", SizeBytes: int64(len(businessJSON)), SHA256: hex.EncodeToString(businessHash[:]),
		},
		Files: files, FileCount: len(files), FileBytes: packageData.ArtifactFiles.ActiveBytes,
		TotalBytes: int64(len(businessJSON)) + packageData.ArtifactFiles.ActiveBytes,
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", exportedAt, fmt.Errorf("encode package manifest: %w", err)
	}
	manifestJSON = append(manifestJSON, '\n')

	temporary, err := os.CreateTemp(a.backupStore.root, ".business-export-*.zip")
	if err != nil {
		return "", exportedAt, fmt.Errorf("create package staging file: %w", err)
	}
	path := temporary.Name()
	finished := false
	defer func() {
		_ = temporary.Close()
		if !finished {
			_ = os.Remove(path)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", exportedAt, fmt.Errorf("protect package staging file: %w", err)
	}
	archive := zip.NewWriter(temporary)
	if err := writeBusinessPackageBytes(archive, "manifest.json", manifestJSON); err != nil {
		return "", exportedAt, err
	}
	if err := writeBusinessPackageBytes(archive, "business-data.json", businessJSON); err != nil {
		return "", exportedAt, err
	}
	for index, row := range rows {
		source, err := resolveControlledFileForStores(a.artifactStore, a.invoicePDFStore, row)
		if err != nil {
			return "", exportedAt, fmt.Errorf("resolve controlled file: %w", err)
		}
		if err := writeBusinessPackageFile(archive, files[index], source); err != nil {
			return "", exportedAt, err
		}
	}
	if err := archive.Close(); err != nil {
		return "", exportedAt, fmt.Errorf("finalize business package: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", exportedAt, fmt.Errorf("sync business package: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", exportedAt, fmt.Errorf("close business package: %w", err)
	}
	finished = true
	return path, exportedAt, nil
}

func writeBusinessPackageBytes(archive *zip.Writer, name string, content []byte) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create package entry %s: %w", name, err)
	}
	if _, err := entry.Write(content); err != nil {
		return fmt.Errorf("write package entry %s: %w", name, err)
	}
	return nil
}

func writeBusinessPackageFile(archive *zip.Writer, expected businessPackageFile, sourcePath string) error {
	if !strings.HasPrefix(expected.Path, "files/") {
		return errors.New("package file path is outside files root")
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("inspect controlled file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != expected.SizeBytes {
		return errors.New("controlled file metadata changed during export")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open controlled file: %w", err)
	}
	defer source.Close()
	header := &zip.FileHeader{Name: filepath.ToSlash(expected.Path), Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create package file entry: %w", err)
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(entry, hasher), source)
	if err != nil {
		return fmt.Errorf("copy controlled file into package: %w", err)
	}
	if written != expected.SizeBytes || hex.EncodeToString(hasher.Sum(nil)) != expected.SHA256 {
		return errors.New("controlled file integrity changed during export")
	}
	return nil
}
