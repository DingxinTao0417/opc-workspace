package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const maxArtifactFileBytes int64 = 50 << 20

const (
	artifactStoreMarkerName    = ".opc-artifact-store-v1"
	artifactStoreMarkerVersion = 1
)

var (
	errArtifactFileEmpty     = errors.New("Artifact files cannot be empty")
	errArtifactFileTooLarge  = errors.New("Artifact file exceeds 50 MiB")
	errArtifactObjectMissing = errors.New("Artifact object is missing")
)

type artifactStore struct {
	root          string
	objectsDir    string
	stagingDir    string
	trashDir      string
	quarantineDir string
	storeID       string
	lease         artifactStoreLease
}

type artifactStoreLease interface {
	Close() error
}

type stagedArtifactFile struct {
	stagingPath  string
	relativePath string
	sizeBytes    int64
	sha256       string
	mimeType     string
}

type trashedArtifactFile struct {
	livePath  string
	trashPath string
}

type artifactStoreMarker struct {
	FormatVersion int    `json:"format_version"`
	DatabaseID    string `json:"database_id"`
	StoreID       string `json:"store_id"`
}

type controlledFileRecord struct {
	id              string
	relativePath    string
	sizeBytes       int64
	sha256          string
	integrityStatus string
	deletedAt       *string
	kind            string
}

type controlledFileTombstone struct {
	sizeBytes int64
	sha256    string
}

func newArtifactStore(root, databaseID, boundStoreID string) (*artifactStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("Artifact root is required")
	}
	databaseID, err := canonicalArtifactDatabaseID(databaseID)
	if err != nil {
		return nil, err
	}
	if boundStoreID != "" {
		boundStoreID, err = canonicalArtifactStoreID(boundStoreID)
		if err != nil {
			return nil, err
		}
	}
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve Artifact root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if isFilesystemRoot(absolute) {
		return nil, errors.New("Artifact root cannot be a filesystem volume root")
	}
	if err := ensureArtifactRootDirectory(absolute); err != nil {
		return nil, fmt.Errorf("create Artifact root: %w", err)
	}
	if err := requireSafeDirectory(absolute); err != nil {
		return nil, fmt.Errorf("validate Artifact root: %w", err)
	}
	storeID, err := claimArtifactRoot(absolute, databaseID, boundStoreID)
	if err != nil {
		return nil, fmt.Errorf("claim Artifact root: %w", err)
	}
	lease, err := acquireArtifactStoreLease(absolute)
	if err != nil {
		return nil, fmt.Errorf("lock Artifact root: %w", err)
	}
	store := &artifactStore{
		root:          absolute,
		objectsDir:    filepath.Join(absolute, "objects"),
		stagingDir:    filepath.Join(absolute, ".staging"),
		trashDir:      filepath.Join(absolute, ".trash"),
		quarantineDir: filepath.Join(absolute, ".quarantine"),
		storeID:       storeID,
		lease:         lease,
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = store.close()
		}
	}()
	for _, directory := range []string{store.objectsDir, store.stagingDir, store.trashDir, store.quarantineDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create Artifact storage directory: %w", err)
		}
		if err := requireSafeDirectory(directory); err != nil {
			return nil, fmt.Errorf("validate Artifact storage directory: %w", err)
		}
	}
	if err := syncArtifactDirectory(store.root); err != nil {
		return nil, fmt.Errorf("sync Artifact storage layout: %w", err)
	}
	if err := store.cleanupOrphans(store.stagingDir); err != nil {
		return nil, fmt.Errorf("clean Artifact staging directory: %w", err)
	}
	initialized = true
	return store, nil
}

func openWorkspaceArtifactStore(db *gorm.DB, root string, reconcile bool) (*artifactStore, error) {
	var databaseID string
	var boundStoreID sql.NullString
	if err := db.Raw("SELECT database_id, artifact_store_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&databaseID, &boundStoreID); err != nil {
		return nil, fmt.Errorf("read workspace identity: %w", err)
	}
	store, err := newArtifactStore(root, databaseID, boundStoreID.String)
	if err != nil {
		return nil, err
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = store.close()
		}
	}()
	if !boundStoreID.Valid {
		result := db.Exec(
			"UPDATE workspace_identity SET artifact_store_id = ? WHERE singleton = 1 AND artifact_store_id IS NULL",
			store.storeID,
		)
		if result.Error != nil {
			return nil, fmt.Errorf("bind Artifact root to workspace database: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var current string
			if err := db.Raw("SELECT artifact_store_id FROM workspace_identity WHERE singleton = 1").Row().Scan(&current); err != nil || current != store.storeID {
				return nil, errors.New("workspace database was concurrently bound to a different Artifact root")
			}
		}
	}
	if reconcile {
		if err := store.reconcile(db); err != nil {
			return nil, err
		}
	}
	initialized = true
	return store, nil
}

func (s *artifactStore) close() error {
	if s == nil || s.lease == nil {
		return nil
	}
	lease := s.lease
	s.lease = nil
	return lease.Close()
}

func isFilesystemRoot(path string) bool {
	volume := filepath.VolumeName(path)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	return sameFilesystemPath(filepath.Clean(path), filepath.Clean(root))
}

func ensureArtifactRootDirectory(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	missing := []string{path}
	ancestor := filepath.Dir(path)
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			if err := requireSafeDirectory(ancestor); err != nil {
				return fmt.Errorf("validate existing parent: %w", err)
			}
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, ancestor)
		next := filepath.Dir(ancestor)
		if sameFilesystemPath(next, ancestor) {
			return errors.New("Artifact root has no accessible parent")
		}
		ancestor = next
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	// Persist each newly created directory entry from the nearest existing
	// ancestor down to the Artifact root.
	for index := len(missing) - 1; index >= 0; index-- {
		if err := syncArtifactDirectory(filepath.Dir(missing[index])); err != nil {
			return err
		}
	}
	return nil
}

func canonicalArtifactDatabaseID(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return "", errors.New("Artifact database identity must be a canonical UUID")
	}
	return value, nil
}

func canonicalArtifactStoreID(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return "", errors.New("Artifact store identity must be a canonical UUID")
	}
	return value, nil
}

func artifactStoreMarkerBytes(databaseID, storeID string) ([]byte, error) {
	marker, err := json.Marshal(artifactStoreMarker{
		FormatVersion: artifactStoreMarkerVersion,
		DatabaseID:    databaseID,
		StoreID:       storeID,
	})
	if err != nil {
		return nil, err
	}
	return append(marker, '\n'), nil
}

func claimArtifactRoot(root, databaseID, boundStoreID string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	markerPath := filepath.Join(root, artifactStoreMarkerName)
	if len(entries) == 0 {
		if boundStoreID != "" {
			return "", errors.New("workspace database is already bound to a different Artifact root")
		}
		storeID := uuid.NewString()
		expected, err := artifactStoreMarkerBytes(databaseID, storeID)
		if err != nil {
			return "", err
		}
		marker, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		if _, err := marker.Write(expected); err != nil {
			_ = marker.Close()
			_ = os.Remove(markerPath)
			return "", err
		}
		if err := marker.Sync(); err != nil {
			_ = marker.Close()
			_ = os.Remove(markerPath)
			return "", err
		}
		if err := marker.Close(); err != nil {
			return "", err
		}
		if err := syncArtifactDirectory(root); err != nil {
			return "", err
		}
		return storeID, nil
	}
	info, err := os.Lstat(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("non-empty Artifact root is missing its ownership marker")
		}
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Artifact root ownership marker is not a regular file")
	}
	content, err := os.ReadFile(markerPath)
	if err != nil {
		return "", err
	}
	var actual artifactStoreMarker
	if err := json.Unmarshal(content, &actual); err != nil || actual.FormatVersion != artifactStoreMarkerVersion {
		return "", errors.New("Artifact root ownership marker is invalid")
	}
	actualDatabaseID, err := canonicalArtifactDatabaseID(actual.DatabaseID)
	if err != nil || actualDatabaseID != databaseID {
		return "", errors.New("Artifact root belongs to a different workspace database")
	}
	actualStoreID, err := canonicalArtifactStoreID(actual.StoreID)
	if err != nil {
		return "", errors.New("Artifact root ownership marker is invalid")
	}
	expected, err := artifactStoreMarkerBytes(databaseID, actualStoreID)
	if err != nil || string(content) != string(expected) {
		return "", errors.New("Artifact root ownership marker is invalid")
	}
	if boundStoreID != "" && actualStoreID != boundStoreID {
		return "", errors.New("workspace database is bound to a different Artifact root")
	}
	return actualStoreID, nil
}

func requireSafeDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a regular directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	absoluteResolved, err := filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if !sameFilesystemPath(path, absoluteResolved) {
		return errors.New("directory traverses a symbolic link or reparse point")
	}
	return nil
}

func sameFilesystemPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func (s *artifactStore) cleanupOrphans(directory string) error {
	if err := requireSafeDirectory(directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// Cleanup deliberately never follows or recursively removes anything. A
		// surprising directory or link is left in place for human diagnosis.
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := removeArtifactFileDurably(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *artifactStore) reconcile(db *gorm.DB) error {
	if err := s.reconcileQuarantine(db); err != nil {
		return err
	}
	if err := s.reconcileTrash(db); err != nil {
		return err
	}
	return s.reconcileObjects(db)
}

func (s *artifactStore) reconcileQuarantine(db *gorm.DB) error {
	if err := requireSafeDirectory(s.quarantineDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.quarantineDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || len(entry.Name()) < 36 {
			continue
		}
		artifactID := entry.Name()[:36]
		if parsed, err := uuid.Parse(artifactID); err != nil || parsed.String() != artifactID {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		artifact, found, err := loadControlledFile(db, artifactID)
		if err != nil {
			return err
		}
		quarantinePath := filepath.Join(s.quarantineDir, entry.Name())
		if !found || artifact.deletedAt != nil {
			tombstone, found, err := loadArtifactDeletionTombstone(db, artifactID)
			if err != nil {
				return err
			}
			if found {
				matches, err := artifactFileMatches(quarantinePath, tombstone.sizeBytes, tombstone.sha256)
				if err != nil {
					return err
				}
				if matches {
					if err := removeArtifactFileDurably(quarantinePath); err != nil {
						return err
					}
				}
			}
			continue
		}
		livePath, err := s.resolveObject(artifact.relativePath)
		if err != nil {
			return err
		}
		if liveInfo, statErr := os.Lstat(livePath); statErr == nil {
			if !liveInfo.Mode().IsRegular() || liveInfo.Mode()&os.ModeSymlink != 0 {
				return errors.New("active Artifact path is not a regular file")
			}
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		matches, err := artifactFileMatches(quarantinePath, artifact.sizeBytes, artifact.sha256)
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		if err := moveFileNoReplace(quarantinePath, livePath); err != nil {
			return fmt.Errorf("restore quarantined Artifact object: %w", err)
		}
		if err := markRecoveredArtifactIntegrity(db, artifactID, "verified"); err != nil {
			return err
		}
	}
	return nil
}

func loadControlledFile(db *gorm.DB, id string) (controlledFileRecord, bool, error) {
	var artifact models.TaskArtifact
	err := db.Select("id", "relative_path", "size_bytes", "sha256", "integrity_status", "deleted_at").
		Where("id = ? AND storage_kind = 'file'", id).First(&artifact).Error
	if err == nil {
		if artifact.RelativePath == nil || artifact.SizeBytes == nil || artifact.SHA256 == nil {
			return controlledFileRecord{}, false, errors.New("file Artifact has incomplete storage facts")
		}
		return controlledFileRecord{
			id: artifact.ID, relativePath: *artifact.RelativePath, sizeBytes: *artifact.SizeBytes,
			sha256: *artifact.SHA256, integrityStatus: artifact.IntegrityStatus,
			deletedAt: artifact.DeletedAt, kind: "task_artifact",
		}, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileRecord{}, false, err
	}
	var attachment models.ClientAttachment
	err = db.Select("id", "relative_path", "size_bytes", "sha256", "integrity_status", "deleted_at").
		First(&attachment, "id = ?", id).Error
	if err == nil {
		return controlledFileRecord{
			id: attachment.ID, relativePath: attachment.RelativePath, sizeBytes: attachment.SizeBytes,
			sha256: attachment.SHA256, integrityStatus: attachment.IntegrityStatus,
			deletedAt: attachment.DeletedAt, kind: "client_attachment",
		}, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileRecord{}, false, err
	}
	var projectAttachment models.ProjectAttachment
	err = db.Select("id", "relative_path", "size_bytes", "sha256", "integrity_status", "deleted_at").
		First(&projectAttachment, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileRecord{}, false, nil
	}
	if err != nil {
		return controlledFileRecord{}, false, err
	}
	return controlledFileRecord{
		id: projectAttachment.ID, relativePath: projectAttachment.RelativePath, sizeBytes: projectAttachment.SizeBytes,
		sha256: projectAttachment.SHA256, integrityStatus: projectAttachment.IntegrityStatus,
		deletedAt: projectAttachment.DeletedAt, kind: "project_attachment",
	}, true, nil
}

func loadArtifactDeletionTombstone(db *gorm.DB, id string) (controlledFileTombstone, bool, error) {
	var artifact models.ArtifactDeletionTombstone
	err := db.First(&artifact, "artifact_id = ?", id).Error
	if err == nil {
		return controlledFileTombstone{sizeBytes: artifact.SizeBytes, sha256: artifact.SHA256}, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileTombstone{}, false, err
	}
	var attachment models.ClientAttachmentDeletionTombstone
	err = db.First(&attachment, "attachment_id = ?", id).Error
	if err == nil {
		return controlledFileTombstone{sizeBytes: attachment.SizeBytes, sha256: attachment.SHA256}, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileTombstone{}, false, err
	}
	var projectAttachment models.ProjectAttachmentDeletionTombstone
	err = db.First(&projectAttachment, "attachment_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return controlledFileTombstone{}, false, nil
	}
	if err != nil {
		return controlledFileTombstone{}, false, err
	}
	return controlledFileTombstone{sizeBytes: projectAttachment.SizeBytes, sha256: projectAttachment.SHA256}, true, nil
}

func markRecoveredArtifactIntegrity(db *gorm.DB, id, status string) error {
	record, found, err := loadControlledFile(db, id)
	if err != nil {
		return err
	}
	if !found || record.deletedAt != nil {
		return errors.New("recovered controlled file row is no longer active")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	model := any(&models.TaskArtifact{})
	if record.kind == "client_attachment" {
		model = &models.ClientAttachment{}
	} else if record.kind == "project_attachment" {
		model = &models.ProjectAttachment{}
	}
	result := db.Model(model).Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"integrity_status": status, "integrity_checked_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("recovered controlled file row is no longer active")
	}
	return nil
}

func artifactFileMatches(path string, expectedSize int64, expectedSHA256 string) (bool, error) {
	lstat, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !lstat.Mode().IsRegular() || lstat.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("Artifact recovery candidate is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(lstat, info) || info.Size() != expectedSize {
		return false, nil
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(digest.Sum(nil)) == expectedSHA256, nil
}

func (s *artifactStore) reconcileTrash(db *gorm.DB) error {
	if err := requireSafeDirectory(s.trashDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.trashDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		if len(name) < 36 || len(name) <= len(".trash") || !strings.HasSuffix(name, ".trash") {
			continue
		}
		artifactID := name[:36]
		if parsed, err := uuid.Parse(artifactID); err != nil || parsed.String() != artifactID {
			continue
		}
		trashPath := filepath.Join(s.trashDir, name)
		artifact, found, err := loadControlledFile(db, artifactID)
		if err != nil {
			return err
		}
		if !found || artifact.deletedAt != nil {
			tombstone, found, err := loadArtifactDeletionTombstone(db, artifactID)
			if err != nil {
				return err
			}
			if found {
				matches, err := artifactFileMatches(trashPath, tombstone.sizeBytes, tombstone.sha256)
				if err != nil {
					return err
				}
				if matches {
					if err := removeArtifactFileDurably(trashPath); err != nil {
						return err
					}
					continue
				}
			}
			if quarantineErr := s.quarantineFile(trashPath, name); quarantineErr != nil {
				return quarantineErr
			}
			continue
		}
		livePath, err := s.resolveObject(artifact.relativePath)
		if err != nil {
			return err
		}
		if liveInfo, statErr := os.Lstat(livePath); statErr == nil {
			if !liveInfo.Mode().IsRegular() || liveInfo.Mode()&os.ModeSymlink != 0 {
				return errors.New("active Artifact path is not a regular file")
			}
			liveMatches, err := artifactFileMatches(livePath, artifact.sizeBytes, artifact.sha256)
			if err != nil {
				return err
			}
			trashMatches, err := artifactFileMatches(trashPath, artifact.sizeBytes, artifact.sha256)
			if err != nil {
				return err
			}
			switch {
			case liveMatches && trashMatches:
				if err := removeArtifactFileDurably(trashPath); err != nil {
					return err
				}
				if artifact.integrityStatus != "verified" {
					if err := markRecoveredArtifactIntegrity(db, artifactID, "verified"); err != nil {
						return err
					}
				}
			case liveMatches:
				if err := s.quarantineFile(trashPath, name); err != nil {
					return err
				}
				if artifact.integrityStatus != "verified" {
					if err := markRecoveredArtifactIntegrity(db, artifactID, "verified"); err != nil {
						return err
					}
				}
			case trashMatches:
				if err := s.quarantineFile(livePath, artifactID+"-mismatch"); err != nil {
					return err
				}
				if err := moveFileNoReplace(trashPath, livePath); err != nil {
					return fmt.Errorf("restore verified Artifact trash entry: %w", err)
				}
				if err := markRecoveredArtifactIntegrity(db, artifactID, "verified"); err != nil {
					return err
				}
			default:
				if err := s.quarantineFile(trashPath, name); err != nil {
					return err
				}
				if err := markRecoveredArtifactIntegrity(db, artifactID, "mismatch"); err != nil {
					return err
				}
			}
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		matches, err := artifactFileMatches(trashPath, artifact.sizeBytes, artifact.sha256)
		if err != nil {
			return err
		}
		if !matches {
			if err := s.quarantineFile(trashPath, name); err != nil {
				return err
			}
			if err := markRecoveredArtifactIntegrity(db, artifactID, "mismatch"); err != nil {
				return err
			}
			continue
		}
		if err := moveFileNoReplace(trashPath, livePath); err != nil {
			return fmt.Errorf("restore uncommitted Artifact trash entry: %w", err)
		}
		if artifact.integrityStatus != "verified" {
			if err := markRecoveredArtifactIntegrity(db, artifactID, "verified"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *artifactStore) reconcileObjects(db *gorm.DB) error {
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.objectsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		parsed, err := uuid.Parse(entry.Name())
		if err != nil || parsed.String() != entry.Name() {
			continue
		}
		relative := filepath.ToSlash(filepath.Join("objects", entry.Name()))
		artifact, found, err := loadControlledFile(db, entry.Name())
		if err != nil {
			return err
		}
		if !found || artifact.deletedAt != nil || artifact.relativePath != relative {
			objectPath := filepath.Join(s.objectsDir, entry.Name())
			tombstone, found, err := loadArtifactDeletionTombstone(db, entry.Name())
			if err != nil {
				return err
			}
			if found {
				matches, err := artifactFileMatches(objectPath, tombstone.sizeBytes, tombstone.sha256)
				if err != nil {
					return err
				}
				if matches {
					if err := removeArtifactFileDurably(objectPath); err != nil {
						return err
					}
					continue
				}
			}
			if err := s.quarantineFile(objectPath, entry.Name()); err != nil {
				return err
			}
			continue
		}
		if artifact.integrityStatus != "verified" {
			matches, err := artifactFileMatches(filepath.Join(s.objectsDir, entry.Name()), artifact.sizeBytes, artifact.sha256)
			if err != nil {
				return err
			}
			if matches {
				if err := markRecoveredArtifactIntegrity(db, artifact.id, "verified"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *artifactStore) quarantineFile(source, name string) error {
	if err := requireSafeDirectory(s.quarantineDir); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Artifact quarantine source is not a regular file")
	}
	destination := filepath.Join(s.quarantineDir, name+"-"+uuid.NewString()+".orphan")
	if err := moveFileNoReplace(source, destination); err != nil {
		return fmt.Errorf("quarantine unreferenced Artifact object: %w", err)
	}
	return nil
}

func (s *artifactStore) stageMultipartFile(file io.Reader, artifactID string) (stagedArtifactFile, error) {
	return s.stageMultipartFileWithLimit(file, artifactID, maxArtifactFileBytes)
}

func (s *artifactStore) stageMultipartFileWithLimit(file io.Reader, artifactID string, maxBytes int64) (stagedArtifactFile, error) {
	if _, err := uuid.Parse(artifactID); err != nil {
		return stagedArtifactFile{}, errors.New("invalid server Artifact id")
	}
	if maxBytes < 1 {
		return stagedArtifactFile{}, errors.New("invalid Artifact file size limit")
	}
	if err := requireSafeDirectory(s.root); err != nil {
		return stagedArtifactFile{}, err
	}
	if err := requireSafeDirectory(s.stagingDir); err != nil {
		return stagedArtifactFile{}, err
	}
	stagingPath := filepath.Join(s.stagingDir, artifactID+".part")
	destination, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return stagedArtifactFile{}, fmt.Errorf("create staged Artifact: %w", err)
	}
	removeStaging := true
	defer func() {
		_ = destination.Close()
		if removeStaging {
			_ = os.Remove(stagingPath)
		}
	}()

	digest := sha256.New()
	prefix := make([]byte, 0, 512)
	buffer := make([]byte, 32<<10)
	var size int64
	for {
		read, readErr := file.Read(buffer)
		if read > 0 {
			size += int64(read)
			if size > maxBytes {
				return stagedArtifactFile{}, errArtifactFileTooLarge
			}
			if len(prefix) < cap(prefix) {
				remaining := cap(prefix) - len(prefix)
				if remaining > read {
					remaining = read
				}
				prefix = append(prefix, buffer[:remaining]...)
			}
			if _, err := digest.Write(buffer[:read]); err != nil {
				return stagedArtifactFile{}, err
			}
			if _, err := destination.Write(buffer[:read]); err != nil {
				return stagedArtifactFile{}, fmt.Errorf("write staged Artifact: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return stagedArtifactFile{}, fmt.Errorf("read uploaded Artifact: %w", readErr)
		}
	}
	if size == 0 {
		return stagedArtifactFile{}, errArtifactFileEmpty
	}
	if err := destination.Sync(); err != nil {
		return stagedArtifactFile{}, fmt.Errorf("sync staged Artifact: %w", err)
	}
	if err := destination.Close(); err != nil {
		return stagedArtifactFile{}, fmt.Errorf("close staged Artifact: %w", err)
	}
	if err := syncArtifactDirectory(s.stagingDir); err != nil {
		return stagedArtifactFile{}, fmt.Errorf("sync staged Artifact directory: %w", err)
	}
	removeStaging = false
	return stagedArtifactFile{
		stagingPath:  stagingPath,
		relativePath: filepath.ToSlash(filepath.Join("objects", artifactID)),
		sizeBytes:    size,
		sha256:       hex.EncodeToString(digest.Sum(nil)),
		mimeType:     http.DetectContentType(prefix),
	}, nil
}

func (s *artifactStore) commitStagedFile(staged stagedArtifactFile) error {
	if err := requireSafeDirectory(s.root); err != nil {
		return err
	}
	if err := requireSafeDirectory(s.stagingDir); err != nil {
		return err
	}
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return err
	}
	destination, err := s.resolveObject(staged.relativePath)
	if err != nil {
		return err
	}
	stagedInfo, err := os.Lstat(staged.stagingPath)
	if err != nil {
		return err
	}
	if !stagedInfo.Mode().IsRegular() || stagedInfo.Mode()&os.ModeSymlink != 0 || filepath.Dir(staged.stagingPath) != s.stagingDir {
		return errors.New("staged Artifact is not a controlled regular file")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("Artifact destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A hard-link promotion is same-volume and atomically refuses an existing
	// destination on every supported platform. Removing the staging name then
	// gives rename-like ownership without Unix rename's overwrite semantics.
	if err := os.Link(staged.stagingPath, destination); err != nil {
		return fmt.Errorf("commit staged Artifact: %w", err)
	}
	if err := syncArtifactDirectory(s.objectsDir); err != nil {
		_ = os.Remove(destination)
		_ = syncArtifactDirectory(s.objectsDir)
		return fmt.Errorf("sync committed Artifact directory: %w", err)
	}
	if err := os.Remove(staged.stagingPath); err != nil {
		_ = os.Remove(destination)
		_ = syncArtifactDirectory(s.objectsDir)
		return fmt.Errorf("remove committed Artifact staging name: %w", err)
	}
	// The object name is already durable. If this best-effort staging sync fails,
	// a crash can at most leave a duplicate staging name for startup cleanup.
	_ = syncArtifactDirectory(s.stagingDir)
	return nil
}

func (s *artifactStore) discardStagedFile(staged stagedArtifactFile) {
	if requireSafeDirectory(s.root) != nil || requireSafeDirectory(s.stagingDir) != nil {
		return
	}
	if filepath.Dir(filepath.Clean(staged.stagingPath)) != s.stagingDir {
		return
	}
	info, err := os.Lstat(staged.stagingPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = removeArtifactFileDurably(staged.stagingPath)
}

func (s *artifactStore) discardCommittedFile(relative string) error {
	if err := requireSafeDirectory(s.root); err != nil {
		return err
	}
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return err
	}
	path, err := s.resolveObject(relative)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("committed Artifact is not a regular file")
	}
	return removeArtifactFileDurably(path)
}

func (s *artifactStore) resolveObject(relative string) (string, error) {
	normalized := filepath.FromSlash(strings.TrimSpace(relative))
	if filepath.IsAbs(normalized) || filepath.Clean(normalized) != normalized {
		return "", errors.New("invalid Artifact relative path")
	}
	parts := strings.Split(filepath.ToSlash(normalized), "/")
	if len(parts) != 2 || parts[0] != "objects" {
		return "", errors.New("invalid Artifact relative path")
	}
	if parsed, err := uuid.Parse(parts[1]); err != nil || parsed.String() != strings.ToLower(parts[1]) {
		return "", errors.New("invalid Artifact object name")
	}
	candidate := filepath.Join(s.root, normalized)
	rel, err := filepath.Rel(s.root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("Artifact path leaves the controlled root")
	}
	return candidate, nil
}

func (s *artifactStore) openObject(relative string) (*os.File, os.FileInfo, error) {
	if err := requireSafeDirectory(s.root); err != nil {
		return nil, nil, err
	}
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return nil, nil, err
	}
	path, err := s.resolveObject(relative)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: %v", errArtifactObjectMissing, err)
		}
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("Artifact object is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, nil, errors.New("Artifact object changed while opening")
	}
	return file, openedInfo, nil
}

func (s *artifactStore) moveObjectToTrash(relative, artifactID string) (trashedArtifactFile, error) {
	if err := requireSafeDirectory(s.root); err != nil {
		return trashedArtifactFile{}, err
	}
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return trashedArtifactFile{}, err
	}
	if err := requireSafeDirectory(s.trashDir); err != nil {
		return trashedArtifactFile{}, err
	}
	live, err := s.resolveObject(relative)
	if err != nil {
		return trashedArtifactFile{}, err
	}
	info, err := os.Lstat(live)
	if err != nil {
		return trashedArtifactFile{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return trashedArtifactFile{}, errors.New("Artifact object is not a regular file")
	}
	trash := filepath.Join(s.trashDir, artifactID+"-"+uuid.NewString()+".trash")
	if _, err := os.Lstat(trash); err == nil {
		return trashedArtifactFile{}, errors.New("Artifact trash destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return trashedArtifactFile{}, err
	}
	if err := moveFileNoReplace(live, trash); err != nil {
		return trashedArtifactFile{}, fmt.Errorf("move Artifact to trash: %w", err)
	}
	return trashedArtifactFile{livePath: live, trashPath: trash}, nil
}

func (s *artifactStore) restoreTrashedFile(moved trashedArtifactFile) error {
	if err := requireSafeDirectory(s.root); err != nil {
		return err
	}
	if err := requireSafeDirectory(s.objectsDir); err != nil {
		return err
	}
	if err := requireSafeDirectory(s.trashDir); err != nil {
		return err
	}
	if filepath.Dir(moved.livePath) != s.objectsDir || filepath.Dir(moved.trashPath) != s.trashDir {
		return errors.New("Artifact recovery path leaves the controlled root")
	}
	if _, err := os.Lstat(moved.livePath); err == nil {
		return errors.New("cannot restore Artifact over an existing object")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return moveFileNoReplace(moved.trashPath, moved.livePath)
}

func moveFileNoReplace(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Link(source, destination); err != nil {
		return err
	}
	if err := syncArtifactDirectory(filepath.Dir(destination)); err != nil {
		_ = os.Remove(destination)
		_ = syncArtifactDirectory(filepath.Dir(destination))
		return err
	}
	if err := os.Remove(source); err != nil {
		_ = os.Remove(destination)
		_ = syncArtifactDirectory(filepath.Dir(destination))
		return err
	}
	// The destination is durable before the source name is removed. A failed
	// source-directory sync can only leave a recoverable duplicate after power
	// loss, so it must not make callers compensate an already completed move.
	_ = syncArtifactDirectory(filepath.Dir(source))
	return nil
}

func removeArtifactFileDurably(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}

func (s *artifactStore) purgeTrashedFile(moved trashedArtifactFile) {
	if requireSafeDirectory(s.root) != nil || requireSafeDirectory(s.trashDir) != nil || filepath.Dir(moved.trashPath) != s.trashDir {
		return
	}
	_ = removeArtifactFileDurably(moved.trashPath)
}
