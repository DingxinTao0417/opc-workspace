package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxInvoicePDFBytes      = int64(50 << 20)
	invoicePDFMimeType      = "application/pdf"
	invoicePDFSchemaVersion = 47
)

type invoicePDFStore struct {
	root       string
	stagingDir string
	trashDir   string
	lease      artifactStoreLease
	mu         sync.Mutex
}

type stagedInvoicePDF struct {
	assetID     string
	stagingPath string
	relative    string
	sizeBytes   int64
	sha256      string
}

type trashedInvoicePDF struct {
	livePath  string
	trashPath string
}

func openInvoicePDFStore(db *gorm.DB, root string) (*invoicePDFStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("invoice PDF root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve invoice PDF root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if isFilesystemRoot(absolute) {
		return nil, errors.New("invoice PDF root cannot be a filesystem volume root")
	}
	if err := ensureArtifactRootDirectory(absolute); err != nil {
		return nil, fmt.Errorf("create invoice PDF root: %w", err)
	}
	if err := requireSafeDirectory(absolute); err != nil {
		return nil, fmt.Errorf("validate invoice PDF root: %w", err)
	}
	store := &invoicePDFStore{
		root:       absolute,
		stagingDir: filepath.Join(absolute, ".staging"),
		trashDir:   filepath.Join(absolute, ".trash"),
	}
	if err := store.claimRoot(db); err != nil {
		return nil, err
	}
	lease, err := acquireArtifactStoreLease(absolute)
	if err != nil {
		return nil, fmt.Errorf("lock invoice PDF root: %w", err)
	}
	store.lease = lease
	initialized := false
	defer func() {
		if !initialized {
			_ = store.close()
		}
	}()
	for _, directory := range []string{store.stagingDir, store.trashDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create invoice PDF storage directory: %w", err)
		}
		if err := requireSafeDirectory(directory); err != nil {
			return nil, fmt.Errorf("validate invoice PDF storage directory: %w", err)
		}
	}
	if err := syncArtifactDirectory(store.root); err != nil {
		return nil, fmt.Errorf("sync invoice PDF storage layout: %w", err)
	}
	if err := store.reconcile(db); err != nil {
		return nil, fmt.Errorf("reconcile invoice PDF storage: %w", err)
	}
	initialized = true
	return store, nil
}

func (s *invoicePDFStore) claimRoot(db *gorm.DB) error {
	var databaseID string
	if err := db.Raw("SELECT database_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&databaseID); err != nil {
		return fmt.Errorf("read workspace identity for invoice PDFs: %w", err)
	}
	if _, err := uuid.Parse(databaseID); err != nil {
		return errors.New("workspace database identity is invalid")
	}
	marker := filepath.Join(s.root, ".opc-workspace-invoice-pdf-store")
	markerInfo, markerErr := os.Lstat(marker)
	if errors.Is(markerErr, os.ErrNotExist) {
		entries, err := os.ReadDir(s.root)
		if err != nil {
			return fmt.Errorf("inspect unclaimed invoice PDF root: %w", err)
		}
		if len(entries) != 0 {
			return errors.New("unclaimed invoice PDF root must be empty")
		}
	} else if markerErr != nil {
		return fmt.Errorf("inspect invoice PDF store identity: %w", markerErr)
	} else {
		if !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("invoice PDF store identity is not a regular file")
		}
		if err := validateClaimedInvoicePDFRootEntries(s.root); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, err = io.WriteString(file, databaseID+"\n"); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(marker)
			return fmt.Errorf("write invoice PDF store identity: %w", err)
		}
		return syncArtifactDirectory(s.root)
	}
	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("claim invoice PDF root: %w", err)
	}
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invoice PDF store identity is not a regular file")
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		return fmt.Errorf("read invoice PDF store identity: %w", err)
	}
	if strings.TrimSpace(string(contents)) != databaseID {
		return errors.New("invoice PDF root is already bound to another workspace database")
	}
	return nil
}

func validateClaimedInvoicePDFRootEntries(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("inspect claimed invoice PDF root: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		switch name {
		case ".opc-workspace-invoice-pdf-store", artifactStoreLockName, ".staging", ".trash":
			continue
		}
		if entry.IsDir() && canonicalInvoicePDFUUID(name) {
			continue
		}
		return fmt.Errorf("claimed invoice PDF root contains unexpected entry %q", name)
	}
	return nil
}

func (s *invoicePDFStore) close() error {
	if s == nil || s.lease == nil {
		return nil
	}
	lease := s.lease
	s.lease = nil
	return lease.Close()
}

func (s *invoicePDFStore) stage(invoiceID string, render func(io.Writer) error) (stagedInvoicePDF, error) {
	if !canonicalInvoicePDFUUID(invoiceID) {
		return stagedInvoicePDF{}, errors.New("invalid invoice PDF owner id")
	}
	if err := requireSafeDirectory(s.root); err != nil {
		return stagedInvoicePDF{}, err
	}
	if err := requireSafeDirectory(s.stagingDir); err != nil {
		return stagedInvoicePDF{}, err
	}
	assetID := uuid.NewString()
	temporary, err := os.CreateTemp(s.stagingDir, ".invoice-pdf-*.tmp")
	if err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("create invoice PDF staging file: %w", err)
	}
	stagingPath := temporary.Name()
	finished := false
	defer func() {
		_ = temporary.Close()
		if !finished {
			_ = os.Remove(stagingPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("protect invoice PDF staging file: %w", err)
	}
	hasher := sha256.New()
	limited := &invoicePDFLimitWriter{writer: io.MultiWriter(temporary, hasher), remaining: maxInvoicePDFBytes}
	if err := render(limited); err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("render invoice PDF: %w", err)
	}
	if limited.written == 0 {
		return stagedInvoicePDF{}, errors.New("rendered invoice PDF is empty")
	}
	if err := temporary.Sync(); err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("sync invoice PDF staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("close invoice PDF staging file: %w", err)
	}
	if err := validateInvoicePDFFile(stagingPath, limited.written, hex.EncodeToString(hasher.Sum(nil))); err != nil {
		return stagedInvoicePDF{}, err
	}
	if err := syncArtifactDirectory(s.stagingDir); err != nil {
		return stagedInvoicePDF{}, fmt.Errorf("sync invoice PDF staging directory: %w", err)
	}
	finished = true
	return stagedInvoicePDF{
		assetID: assetID, stagingPath: stagingPath,
		relative:  filepath.ToSlash(filepath.Join(invoiceID, assetID+".pdf")),
		sizeBytes: limited.written, sha256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

type invoicePDFLimitWriter struct {
	writer    io.Writer
	remaining int64
	written   int64
}

func (w *invoicePDFLimitWriter) Write(contents []byte) (int, error) {
	if int64(len(contents)) > w.remaining {
		return 0, errors.New("rendered invoice PDF exceeds 50 MiB")
	}
	n, err := w.writer.Write(contents)
	w.remaining -= int64(n)
	w.written += int64(n)
	return n, err
}

func (s *invoicePDFStore) commit(staged stagedInvoicePDF) error {
	destination, err := s.resolve(staged.relative, staged.assetID)
	if err != nil {
		return err
	}
	destinationDir := filepath.Dir(destination)
	if err := ensureInvoicePDFOwnerDirectory(s.root, destinationDir, syncArtifactDirectory); err != nil {
		return fmt.Errorf("prepare invoice PDF destination directory: %w", err)
	}
	stagedInfo, err := os.Lstat(staged.stagingPath)
	if err != nil {
		return err
	}
	if !stagedInfo.Mode().IsRegular() || stagedInfo.Mode()&os.ModeSymlink != 0 || filepath.Dir(staged.stagingPath) != s.stagingDir {
		return errors.New("staged invoice PDF is not a controlled regular file")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("invoice PDF destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staged.stagingPath, destination); err != nil {
		return fmt.Errorf("atomically commit invoice PDF: %w", err)
	}
	if err := syncArtifactDirectory(destinationDir); err != nil {
		_ = os.Rename(destination, staged.stagingPath)
		_ = syncArtifactDirectory(s.stagingDir)
		return fmt.Errorf("sync committed invoice PDF directory: %w", err)
	}
	_ = syncArtifactDirectory(s.stagingDir)
	return nil
}

func (s *invoicePDFStore) discardStaged(staged stagedInvoicePDF) {
	if filepath.Dir(staged.stagingPath) == s.stagingDir {
		_ = removeArtifactFileDurably(staged.stagingPath)
	}
}

func (s *invoicePDFStore) remove(relative, assetID string) error {
	path, err := s.resolve(relative, assetID)
	if err != nil {
		return err
	}
	parentExists, err := safeInvoicePDFParentExists(s.root, path)
	if err != nil {
		return err
	}
	if !parentExists {
		return nil
	}
	return removeArtifactFileDurably(path)
}

func (s *invoicePDFStore) moveToTrash(relative, assetID string) (*trashedInvoicePDF, error) {
	live, err := s.resolve(relative, assetID)
	if err != nil {
		return nil, err
	}
	parentExists, err := safeInvoicePDFParentExists(s.root, live)
	if err != nil {
		return nil, err
	}
	if !parentExists {
		return nil, nil
	}
	if err := requireSafeDirectory(s.trashDir); err != nil {
		return nil, err
	}
	info, err := os.Lstat(live)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invoice PDF is not a regular file")
	}
	trash := filepath.Join(s.trashDir, filepath.FromSlash(relative))
	trashDir := filepath.Dir(trash)
	if err := ensureInvoicePDFOwnerDirectory(s.trashDir, trashDir, syncArtifactDirectory); err != nil {
		return nil, err
	}
	if err := moveFileNoReplace(live, trash); err != nil {
		return nil, err
	}
	return &trashedInvoicePDF{livePath: live, trashPath: trash}, nil
}

func (s *invoicePDFStore) restoreTrashed(moved *trashedInvoicePDF) error {
	if moved == nil {
		return nil
	}
	if !pathWithin(s.root, moved.livePath) || !pathWithin(s.trashDir, moved.trashPath) {
		return errors.New("invoice PDF recovery path leaves controlled storage")
	}
	if err := requireSafeInvoicePDFParent(s.root, moved.livePath); err != nil {
		return err
	}
	if err := requireSafeInvoicePDFParent(s.trashDir, moved.trashPath); err != nil {
		return err
	}
	return moveFileNoReplace(moved.trashPath, moved.livePath)
}

func (s *invoicePDFStore) purgeTrashed(moved *trashedInvoicePDF) {
	if moved != nil && pathWithin(s.trashDir, moved.trashPath) && requireSafeInvoicePDFParent(s.trashDir, moved.trashPath) == nil {
		_ = removeArtifactFileDurably(moved.trashPath)
	}
}

func (s *invoicePDFStore) resolve(relative, assetID string) (string, error) {
	if err := requireSafeDirectory(s.root); err != nil {
		return "", err
	}
	parsedAsset, err := uuid.Parse(assetID)
	if err != nil || parsedAsset.String() != assetID {
		return "", errors.New("invalid invoice PDF asset id")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || !canonicalInvoicePDFUUID(parts[0]) || parts[1] != assetID+".pdf" {
		return "", errors.New("invalid invoice PDF relative path")
	}
	path := filepath.Clean(filepath.Join(s.root, filepath.FromSlash(relative)))
	if !pathWithin(s.root, path) {
		return "", errors.New("invoice PDF path leaves controlled storage")
	}
	return path, nil
}

func (s *invoicePDFStore) verify(relative, assetID string, expectedSize int64, expectedHash string) (string, error) {
	file, err := s.openVerified(relative, assetID, expectedSize, expectedHash)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "missing", err
		}
		return "mismatch", err
	}
	if err := file.Close(); err != nil {
		return "mismatch", err
	}
	return "verified", nil
}

func validateInvoicePDFFile(path string, expectedSize int64, expectedHash string) error {
	file, err := openValidatedInvoicePDF(path, expectedSize, expectedHash)
	if err != nil {
		return err
	}
	return file.Close()
}

func (s *invoicePDFStore) openVerified(relative, assetID string, expectedSize int64, expectedHash string) (*os.File, error) {
	path, err := s.resolve(relative, assetID)
	if err != nil {
		return nil, err
	}
	if err := requireSafeInvoicePDFParent(s.root, path); err != nil {
		return nil, err
	}
	return openValidatedInvoicePDF(path, expectedSize, expectedHash)
}

func openValidatedInvoicePDF(path string, expectedSize int64, expectedHash string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != expectedSize || expectedSize <= 0 || expectedSize > maxInvoicePDFBytes {
		return nil, errors.New("invoice PDF file metadata does not match")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	valid := false
	defer func() {
		if !valid {
			_ = file.Close()
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || openedInfo.Size() != expectedSize || !os.SameFile(info, openedInfo) {
		return nil, errors.New("invoice PDF changed while it was opened")
	}
	hasher := sha256.New()
	prefix := make([]byte, 5)
	if _, err := io.ReadFull(file, prefix); err != nil || string(prefix) != "%PDF-" {
		return nil, errors.New("invoice PDF header is invalid")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return nil, errors.New("invoice PDF checksum does not match")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	valid = true
	return file, nil
}

func (s *invoicePDFStore) reconcile(db *gorm.DB) error {
	var assets []models.InvoicePDFAsset
	if err := db.Order("invoice_id ASC").Find(&assets).Error; err != nil {
		return err
	}
	expected := make(map[string]models.InvoicePDFAsset, len(assets))
	for _, asset := range assets {
		live, err := s.resolve(asset.RelativePath, asset.ID)
		if err != nil {
			return err
		}
		expected[live] = asset
		if _, err := os.Lstat(live); errors.Is(err, os.ErrNotExist) {
			trash := filepath.Join(s.trashDir, filepath.FromSlash(asset.RelativePath))
			if _, trashErr := os.Lstat(trash); trashErr == nil {
				if err := ensureInvoicePDFOwnerDirectory(s.root, filepath.Dir(live), syncArtifactDirectory); err != nil {
					return err
				}
				if err := requireSafeInvoicePDFParent(s.root, live); err != nil {
					return err
				}
				if err := requireSafeInvoicePDFParent(s.trashDir, trash); err != nil {
					return err
				}
				if err := moveFileNoReplace(trash, live); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
	}
	if err := removeInvoicePDFTreeFiles(s.stagingDir, nil); err != nil {
		return err
	}
	if err := removeInvoicePDFTreeFiles(s.trashDir, nil); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !canonicalInvoicePDFUUID(entry.Name()) {
			continue
		}
		if err := removeInvoicePDFTreeFiles(filepath.Join(s.root, entry.Name()), expected); err != nil {
			return err
		}
	}
	return nil
}

func ensureInvoicePDFOwnerDirectory(root, directory string, syncDirectory func(string) error) error {
	root = filepath.Clean(root)
	directory = filepath.Clean(directory)
	if !sameFilesystemPath(filepath.Dir(directory), root) {
		return errors.New("invoice PDF owner directory leaves its controlled root")
	}
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	if info, err := os.Lstat(directory); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invoice PDF owner path is not a safe directory")
		}
		return requireSafeDirectory(directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	if err := requireSafeDirectory(directory); err != nil {
		_ = os.Remove(directory)
		return err
	}
	if err := syncDirectory(root); err != nil {
		if removeErr := os.Remove(directory); removeErr == nil {
			_ = syncDirectory(root)
		}
		return fmt.Errorf("sync invoice PDF owner directory parent: %w", err)
	}
	return nil
}

func removeInvoicePDFTreeFiles(root string, keep map[string]models.InvoicePDFAsset) error {
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invoice PDF storage contains an unsafe file")
		}
		if _, preserved := keep[path]; preserved {
			return nil
		}
		return removeArtifactFileDurably(path)
	})
}

func requireSafeInvoicePDFParent(root, path string) error {
	if err := requireSafeDirectory(root); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if !pathWithin(root, parent) {
		return errors.New("invoice PDF parent leaves controlled storage")
	}
	return requireSafeDirectory(parent)
}

func safeInvoicePDFParentExists(root, path string) (bool, error) {
	if err := requireSafeDirectory(root); err != nil {
		return false, err
	}
	parent := filepath.Dir(path)
	if !pathWithin(root, parent) {
		return false, errors.New("invoice PDF parent leaves controlled storage")
	}
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("invoice PDF parent is not a safe directory")
	}
	if err := requireSafeDirectory(parent); err != nil {
		return false, err
	}
	return true, nil
}

func canonicalInvoicePDFUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func loadInvoicePDFAsset(db *gorm.DB, invoiceID string) (models.InvoicePDFAsset, error) {
	var asset models.InvoicePDFAsset
	err := db.Where("invoice_id = ?", invoiceID).Take(&asset).Error
	return asset, err
}

func invoicePDFAssetExists(db *gorm.DB, invoiceID string) (models.InvoicePDFAsset, bool, error) {
	asset, err := loadInvoicePDFAsset(db, invoiceID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		return models.InvoicePDFAsset{}, false, nil
	}
	return asset, err == nil, err
}
