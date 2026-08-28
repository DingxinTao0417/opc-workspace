package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const deletingBackupPrefix = ".deleting-"

func (a *API) deleteBackup(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	id := strings.ToLower(strings.TrimSpace(c.Param("id")))
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_ID", "Backup id must be a canonical UUID")
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Permanent backup deletion requires confirm=true")
		return
	}

	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()
	packagePath := filepath.Join(a.backupStore.root, id)
	deletingPath := filepath.Join(a.backupStore.root, deletingBackupPrefix+id)
	packageExists, err := safeBackupDeletionPath(packagePath)
	if err != nil {
		a.logBackupError("validate delete target", err)
		writeError(c, http.StatusConflict, "BACKUP_DELETE_UNSAFE", "The backup package contains an unsafe filesystem entry and was not deleted")
		return
	}
	deletingExists, err := safeBackupDeletionPath(deletingPath)
	if err != nil {
		a.logBackupError("validate pending delete", err)
		writeError(c, http.StatusConflict, "BACKUP_DELETE_UNSAFE", "A pending backup deletion contains an unsafe filesystem entry")
		return
	}
	if !packageExists && !deletingExists {
		writeError(c, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
		return
	}
	if packageExists {
		if deletingExists {
			writeError(c, http.StatusConflict, "BACKUP_DELETE_FAILED", "A previous deletion for this backup could not be completed")
			return
		}
		if err := os.Rename(packagePath, deletingPath); err != nil {
			a.logBackupError("stage delete", err)
			writeError(c, http.StatusInternalServerError, "BACKUP_DELETE_FAILED", "The backup could not be staged for deletion")
			return
		}
		if err := syncArtifactDirectory(a.backupStore.root); err != nil {
			a.logBackupError("sync staged delete", err)
			writeError(c, http.StatusInternalServerError, "BACKUP_DELETE_FAILED", "The backup deletion could not be committed safely")
			return
		}
	}
	if err := os.RemoveAll(deletingPath); err != nil {
		a.logBackupError("remove staged delete", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_DELETE_FAILED", "The backup was hidden but its files could not be removed; retry the same deletion")
		return
	}
	if err := syncArtifactDirectory(a.backupStore.root); err != nil {
		a.logBackupError("sync delete", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_DELETE_FAILED", "The backup files were removed but the directory sync failed")
		return
	}
	c.Status(http.StatusNoContent)
}

func safeBackupDeletionPath(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("backup deletion target is not a regular directory")
	}
	if err := requireSafeDirectory(path); err != nil {
		return false, err
	}
	err = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("backup deletion target traverses a symbolic link or reparse point")
		}
		if entry.IsDir() {
			return requireSafeDirectory(current)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("backup deletion target contains a non-regular file")
		}
		return nil
	})
	return err == nil, err
}
