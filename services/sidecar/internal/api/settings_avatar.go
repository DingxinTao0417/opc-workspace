package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxWorkspaceAvatarRequestBytes int64 = maxWorkspaceAvatarBytes + maxArtifactManifestBytes

type workspaceAvatarManifest struct {
	Operation string                 `json:"operation"`
	Updates   []settingUpdateRequest `json:"updates"`
}

type workspaceAvatarRow struct {
	ID                 string  `gorm:"column:id"`
	RelativePath       string  `gorm:"column:relative_path"`
	Extension          string  `gorm:"column:extension"`
	MimeType           string  `gorm:"column:mime_type"`
	SizeBytes          int64   `gorm:"column:size_bytes"`
	SHA256             string  `gorm:"column:sha256"`
	IntegrityStatus    string  `gorm:"column:integrity_status"`
	IntegrityCheckedAt string  `gorm:"column:integrity_checked_at"`
	CreatedAt          string  `gorm:"column:created_at"`
	DeletedAt          *string `gorm:"column:deleted_at"`
}

func (workspaceAvatarRow) TableName() string { return "workspace_avatars" }

func (a *API) commitSettingsWithAvatar(c *gin.Context) {
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "AVATAR_UNAVAILABLE", "Controlled workspace avatars are unavailable")
		return
	}
	manifest, staged, extension, err := a.readWorkspaceAvatarRequest(c)
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeError(c, http.StatusInternalServerError, "AVATAR_STORAGE_ERROR", "The workspace avatar could not be staged")
		return
	}
	if staged != nil {
		defer a.artifactStore.discardStagedFile(*staged)
	}
	prepared, err := prepareSettingUpdates(manifest.Updates)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	workspaceIndex := -1
	for index := range prepared {
		if prepared[index].key == "workspace" {
			workspaceIndex = index
			break
		}
	}
	if workspaceIndex < 0 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Avatar changes require a workspace setting update")
		return
	}
	current, err := loadWorkspaceSettingValue(a.db.WithContext(c.Request.Context()))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	var nextRef *string
	var nextAvatar *workspaceAvatarRow
	if manifest.Operation == "replace" {
		if staged == nil {
			writeError(c, http.StatusBadRequest, "INVALID_MULTIPART", "Avatar replacement requires exactly one file")
			return
		}
		// The ID is chosen while parsing so the controlled relative path and row
		// are inseparable. readWorkspaceAvatarRequest preserves it in the path.
		parts := strings.Split(strings.TrimPrefix(staged.relativePath, "avatars/"), ".")
		if len(parts) != 2 {
			writeError(c, http.StatusInternalServerError, "AVATAR_STORAGE_ERROR", "The workspace avatar path is invalid")
			return
		}
		avatarID := parts[0]
		ref := staged.relativePath
		nextRef = &ref
		nowText := a.options.Now().UTC().Format(time.RFC3339Nano)
		nextAvatar = &workspaceAvatarRow{
			ID: avatarID, RelativePath: ref, Extension: extension, MimeType: staged.mimeType,
			SizeBytes: staged.sizeBytes, SHA256: staged.sha256, IntegrityStatus: "verified",
			IntegrityCheckedAt: nowText, CreatedAt: nowText,
		}
	} else if manifest.Operation != "remove" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Avatar operation must be replace or remove")
		return
	}
	var workspaceValue workspaceSettingValue
	if err := json.Unmarshal([]byte(prepared[workspaceIndex].valueJSON), &workspaceValue); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Workspace setting is invalid")
		return
	}
	if !sameOptionalString(workspaceValue.AvatarRef, current.AvatarRef) {
		writeError(c, http.StatusConflict, "SETTINGS_VERSION_CONFLICT", "Workspace avatar changed; reload settings before retrying")
		return
	}
	workspaceValue.AvatarRef = nextRef
	prepared[workspaceIndex].valueJSON, err = marshalSettingObject(workspaceValue)
	if err != nil {
		writeDatabaseError(c)
		return
	}

	if staged != nil {
		if err := a.artifactStore.commitStagedWorkspaceAvatar(*staged); err != nil {
			writeError(c, http.StatusInternalServerError, "AVATAR_STORAGE_ERROR", "The workspace avatar could not be committed")
			return
		}
		staged = nil
	}
	nowText := a.options.Now().UTC().Format(time.RFC3339Nano)
	requestID := requestIDFromContext(c)
	var previousAvatar *workspaceAvatarRow
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if current.AvatarRef != nil {
			var row workspaceAvatarRow
			if err := tx.Where("relative_path = ? AND deleted_at IS NULL", *current.AvatarRef).Take(&row).Error; err == nil {
				previousAvatar = &row
				if err := retireWorkspaceAvatar(tx, row, nowText, "replaced from workspace settings"); err != nil {
					return err
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if nextAvatar != nil {
			if err := tx.Create(nextAvatar).Error; err != nil {
				return fmt.Errorf("create workspace avatar: %w", err)
			}
		}
		for _, update := range prepared {
			if err := applySettingUpdate(tx, update, requestID, nowText); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if nextAvatar != nil {
			if !workspaceAvatarCommitProven(a.db, nextAvatar.RelativePath) {
				_ = a.artifactStore.removeWorkspaceAvatar(nextAvatar.RelativePath)
			}
		}
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if previousAvatar != nil {
		if err := a.artifactStore.removeWorkspaceAvatar(previousAvatar.RelativePath); err != nil && a.options.Logger != nil {
			a.options.Logger.Printf("workspace avatar cleanup deferred avatar_id=%s: %v", previousAvatar.ID, err)
		}
	}
	response, err := loadSettingsResponse(a.db.WithContext(c.Request.Context()))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) readWorkspaceAvatarRequest(c *gin.Context) (workspaceAvatarManifest, *stagedArtifactFile, string, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be multipart/form-data")
	}
	if c.Request.ContentLength > maxWorkspaceAvatarRequestBytes {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Avatar requests cannot exceed 3 MiB")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkspaceAvatarRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "manifest" || part.FileName() != "" {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The manifest text field must be first")
	}
	manifestBytes, readErr := io.ReadAll(io.LimitReader(part, maxArtifactManifestBytes+1))
	closeErr := part.Close()
	if readErr != nil || closeErr != nil {
		return workspaceAvatarManifest{}, nil, "", multipartRequestReadError(errors.Join(readErr, closeErr))
	}
	if len(manifestBytes) > maxArtifactManifestBytes {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Avatar manifest cannot exceed 1 MiB")
	}
	var manifest workspaceAvatarManifest
	if err := decodeStrictJSONBytes(manifestBytes, &manifest); err != nil {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The avatar manifest is not valid JSON")
	}
	manifest.Operation = strings.TrimSpace(manifest.Operation)
	if manifest.Operation == "remove" {
		if part, err := reader.NextPart(); !errors.Is(err, io.EOF) {
			if err == nil {
				_ = part.Close()
			}
			return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Avatar removal must not include a file")
		}
		return manifest, nil, "", nil
	}
	if manifest.Operation != "replace" {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Avatar operation must be replace or remove")
	}
	filePart, err := reader.NextPart()
	if err != nil || filePart.FormName() != "file" || filePart.FileName() == "" {
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Avatar replacement requires one file part")
	}
	avatarID := uuid.NewString()
	staged, extension, err := a.artifactStore.stageWorkspaceAvatar(filePart, avatarID)
	closeErr = filePart.Close()
	if err != nil || closeErr != nil {
		if err == nil {
			err = closeErr
		}
		if errors.Is(err, errArtifactFileTooLarge) {
			return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusRequestEntityTooLarge, "AVATAR_FILE_TOO_LARGE", "Avatar files cannot exceed 2 MiB")
		}
		if errors.Is(err, errArtifactFileEmpty) || strings.Contains(err.Error(), "must be PNG") {
			return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
		return workspaceAvatarManifest{}, nil, "", multipartRequestReadError(err)
	}
	if extra, err := reader.NextPart(); !errors.Is(err, io.EOF) {
		a.artifactStore.discardStagedFile(staged)
		if err == nil {
			_ = extra.Close()
		}
		return workspaceAvatarManifest{}, nil, "", newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Avatar replacement accepts exactly one file")
	}
	return manifest, &staged, extension, nil
}

func (a *API) getWorkspaceAvatarContent(c *gin.Context) {
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "AVATAR_UNAVAILABLE", "Controlled workspace avatars are unavailable")
		return
	}
	value, err := loadWorkspaceSettingValue(a.db.WithContext(c.Request.Context()))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if value.AvatarRef == nil {
		writeError(c, http.StatusNotFound, "AVATAR_NOT_FOUND", "Workspace avatar is not configured")
		return
	}
	var avatar workspaceAvatarRow
	if err := a.db.Where("relative_path = ? AND deleted_at IS NULL", *value.AvatarRef).Take(&avatar).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusGone, "AVATAR_MISSING", "Workspace avatar metadata is unavailable")
		return
	} else if err != nil {
		writeDatabaseError(c)
		return
	}
	path, err := a.artifactStore.resolveWorkspaceAvatar(avatar.RelativePath)
	if err != nil {
		writeError(c, http.StatusConflict, "AVATAR_INVALID", "Workspace avatar storage facts are invalid")
		return
	}
	matches, err := artifactFileMatches(path, avatar.SizeBytes, avatar.SHA256)
	if err != nil || !matches {
		status := "mismatch"
		code := "AVATAR_INVALID"
		message := "Workspace avatar failed integrity verification"
		if errors.Is(err, os.ErrNotExist) {
			status, code, message = "missing", "AVATAR_MISSING", "Workspace avatar file is missing"
		}
		_ = a.db.Model(&workspaceAvatarRow{}).Where("id = ?", avatar.ID).Updates(map[string]any{
			"integrity_status": status, "integrity_checked_at": a.options.Now().UTC().Format(time.RFC3339Nano),
		}).Error
		writeError(c, http.StatusGone, code, message)
		return
	}
	file, info, err := a.artifactStore.openWorkspaceAvatar(avatar.RelativePath)
	if err != nil {
		writeError(c, http.StatusGone, "AVATAR_MISSING", "Workspace avatar file is unavailable")
		return
	}
	defer file.Close()
	c.Header("Content-Type", avatar.MimeType)
	c.Header("Cache-Control", "private, no-cache")
	c.Header("ETag", `"`+avatar.SHA256+`"`)
	c.DataFromReader(http.StatusOK, info.Size(), avatar.MimeType, file, nil)
}

func loadWorkspaceSettingValue(db *gorm.DB) (workspaceSettingValue, error) {
	var valueJSON string
	err := db.Table("app_settings").Select("value_json").Where("key = 'workspace'").Scan(&valueJSON).Error
	if err != nil {
		return workspaceSettingValue{}, err
	}
	if valueJSON == "" {
		valueJSON = defaultSettingValue("workspace")
	}
	var value workspaceSettingValue
	if err := json.Unmarshal([]byte(valueJSON), &value); err != nil {
		return workspaceSettingValue{}, err
	}
	return value, nil
}

func retireWorkspaceAvatar(tx *gorm.DB, avatar workspaceAvatarRow, nowText, reason string) error {
	if err := tx.Table("workspace_avatar_deletion_tombstones").Create(map[string]any{
		"avatar_id": avatar.ID, "relative_path": avatar.RelativePath, "size_bytes": avatar.SizeBytes,
		"sha256": avatar.SHA256, "reason": reason, "created_at": nowText,
	}).Error; err != nil {
		return fmt.Errorf("record workspace avatar tombstone: %w", err)
	}
	result := tx.Model(&workspaceAvatarRow{}).Where("id = ? AND deleted_at IS NULL", avatar.ID).Updates(map[string]any{
		"deleted_at": nowText, "deletion_reason": reason,
	})
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return fmt.Errorf("retire workspace avatar: %w", result.Error)
		}
		return errors.New("workspace avatar changed while retiring")
	}
	return nil
}

func workspaceAvatarCommitProven(db *gorm.DB, relative string) bool {
	value, err := loadWorkspaceSettingValue(db)
	if err != nil || value.AvatarRef == nil || *value.AvatarRef != relative {
		return false
	}
	var count int64
	return db.Table("workspace_avatars").Where("relative_path = ? AND deleted_at IS NULL", relative).Count(&count).Error == nil && count == 1
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
