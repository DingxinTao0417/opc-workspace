package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type createProjectAttachmentMetadata struct {
	Name string `json:"name"`
}

type deleteProjectAttachmentRequest struct {
	Reason string `json:"reason"`
}

type projectAttachmentResponse struct {
	ID                 string                        `json:"id"`
	ProjectID          string                        `json:"project_id"`
	Name               string                        `json:"name"`
	MimeType           string                        `json:"mime_type"`
	SizeBytes          int64                         `json:"size_bytes"`
	SHA256             string                        `json:"sha256"`
	RecordedBy         clientAttachmentActorResponse `json:"recorded_by"`
	IntegrityStatus    string                        `json:"integrity_status"`
	IntegrityCheckedAt string                        `json:"integrity_checked_at"`
	DeletedAt          *string                       `json:"deleted_at"`
	DeletedByActorID   *string                       `json:"deleted_by_actor_id"`
	DeleteReason       *string                       `json:"delete_reason"`
	CreatedAt          string                        `json:"created_at"`
	ProjectVersion     int64                         `json:"project_version"`
}

type projectAttachmentRow struct {
	models.ProjectAttachment
	RecordedByType        string `gorm:"column:recorded_by_type"`
	RecordedByDisplayName string `gorm:"column:recorded_by_display_name"`
	ProjectVersion        int64  `gorm:"column:project_version"`
	ProjectStatus         string `gorm:"column:project_status"`
}

type projectAttachmentMeta struct {
	Page           int   `json:"page"`
	PageSize       int   `json:"page_size"`
	Total          int64 `json:"total"`
	ProjectVersion int64 `json:"project_version"`
}

func (a *API) listProjectAttachments(c *gin.Context) {
	projectIDValue, ok := projectID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	includeDeleted, ok := parseClientActivityIncludeDeleted(c.Query("include_deleted"))
	if !ok {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "include_deleted must be true or false")
		return
	}

	var project models.Project
	var total int64
	var rows []projectAttachmentRow
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&project, "id = ?", projectIDValue).Error; err != nil {
			return err
		}
		query := projectAttachmentRowsQuery(tx).Where("attachment.project_id = ?", projectIDValue)
		countQuery := tx.Table("project_attachments").Where("project_id = ?", projectIDValue)
		if !includeDeleted {
			query = query.Where("attachment.deleted_at IS NULL")
			countQuery = countQuery.Where("deleted_at IS NULL")
		}
		if err := countQuery.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("attachment.created_at DESC").Order("attachment.id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	responses := make([]projectAttachmentResponse, len(rows))
	for index := range rows {
		responses[index] = projectAttachmentResponseFromRow(rows[index])
	}
	setProjectETag(c, project.Version)
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": projectAttachmentMeta{
		Page: page, PageSize: pageSize, Total: total, ProjectVersion: project.Version,
	}})
}

func (a *API) createProjectAttachment(c *gin.Context) {
	projectIDValue, ok := projectID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Project attachment storage is unavailable")
		return
	}
	metadata, staged, err := a.readProjectAttachmentUpload(c)
	if err != nil {
		writeTaskOutputError(c, err)
		return
	}
	cleanupStaged := true
	defer func() {
		if cleanupStaged {
			a.artifactStore.discardStagedFile(staged)
		}
	}()
	name, err := cleanClientAttachmentName(metadata.Name)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"project_id": projectIDValue, "expected_version": expectedVersion,
		"name": name, "size_bytes": staged.sizeBytes, "sha256": staged.sha256,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/projects/%s/attachments", projectIDValue)
	statusCode := http.StatusCreated
	replayed := false
	committed := false
	var response projectAttachmentResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, replayStatus, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		var project models.Project
		if err := tx.Select("id", "version", "status").First(&project, "id = ?", projectIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		if project.Status == "archived" {
			return newProjectRequestError(http.StatusConflict, "PROJECT_ARCHIVED", "Archived projects are read-only")
		}
		if project.Version != expectedVersion {
			return projectVersionConflict()
		}
		if err := a.artifactStore.commitStagedFile(staged); err != nil {
			return newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The project attachment could not be committed to controlled storage")
		}
		committed = true
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		attachment := models.ProjectAttachment{
			ID: strings.TrimPrefix(staged.relativePath, "objects/"), ProjectID: projectIDValue,
			Name: name, RelativePath: staged.relativePath, MimeType: staged.mimeType,
			SizeBytes: staged.sizeBytes, SHA256: staged.sha256, RecordedByActorID: models.BuiltinOwnerActorID,
			IntegrityStatus: "verified", IntegrityCheckedAt: now, CreatedAt: now,
		}
		if err := tx.Create(&attachment).Error; err != nil {
			return mapProjectAttachmentConstraintError(err)
		}
		row, err := loadProjectAttachmentRow(tx, attachment.ID)
		if err != nil {
			return err
		}
		response = projectAttachmentResponseFromRow(row)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, attachment.ID, requestHash, http.StatusCreated, response, now)
	})
	if err != nil {
		if committed {
			_ = a.artifactStore.discardCommittedFile(staged.relativePath)
		}
		if writeProjectRequestError(c, mapProjectAttachmentConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if !replayed {
		cleanupStaged = false
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.ProjectVersion)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getProjectAttachment(c *gin.Context) {
	id, ok := projectAttachmentID(c)
	if !ok {
		return
	}
	row, err := loadProjectAttachmentRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_ATTACHMENT_NOT_FOUND", "Project attachment not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := projectAttachmentResponseFromRow(row)
	setProjectETag(c, response.ProjectVersion)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) getProjectAttachmentContent(c *gin.Context) {
	id, ok := projectAttachmentID(c)
	if !ok {
		return
	}
	row, err := loadProjectAttachmentRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_ATTACHMENT_NOT_FOUND", "Project attachment not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	if row.DeletedAt != nil {
		writeError(c, http.StatusGone, "PROJECT_ATTACHMENT_DELETED", "The project attachment has been deleted")
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Project attachment storage is unavailable")
		return
	}
	file, info, err := a.artifactStore.openObject(row.RelativePath)
	if err != nil {
		if errors.Is(err, errArtifactObjectMissing) {
			if !a.persistProjectAttachmentIntegrity(c, id, "missing") {
				return
			}
			writeError(c, http.StatusGone, "PROJECT_ATTACHMENT_FILE_MISSING", "The project attachment file is missing from controlled storage")
			return
		}
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The project attachment could not be opened from controlled storage")
		return
	}
	defer file.Close()
	if info.Size() != row.SizeBytes {
		a.rejectProjectAttachmentIntegrity(c, id, "mismatch")
		return
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The project attachment could not be verified")
		return
	}
	if hex.EncodeToString(digest.Sum(nil)) != row.SHA256 {
		a.rejectProjectAttachmentIntegrity(c, id, "mismatch")
		return
	}
	if !a.persistProjectAttachmentIntegrity(c, id, "verified") {
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The project attachment could not be read")
		return
	}
	c.Header("Content-Type", row.MimeType)
	c.Header("Content-Length", strconv.FormatInt(row.SizeBytes, 10))
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": row.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", `"`+row.SHA256+`"`)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, file); err != nil && a.options.Logger != nil {
		a.options.Logger.Printf("project attachment stream failed attachment_id=%s request_id=%s error=%v", id, requestIDFromContext(c), err)
	}
}

func (a *API) deleteProjectAttachment(c *gin.Context) {
	id, ok := projectAttachmentID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm=true is required to delete a project attachment")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input deleteProjectAttachmentRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := validateArtifactDeleteReason(input.Reason)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"attachment_id": id, "expected_version": expectedVersion, "confirm": true, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("DELETE /api/v1/project-attachments/%s", id)
	replayed := false
	var moved *trashedArtifactFile
	var response projectAttachmentResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			return nil
		}
		row, err := loadProjectAttachmentRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_ATTACHMENT_NOT_FOUND", "Project attachment not found")
			}
			return err
		}
		if row.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "PROJECT_ATTACHMENT_ALREADY_DELETED", "The project attachment is already deleted")
		}
		if row.ProjectStatus == "archived" {
			return newProjectRequestError(http.StatusConflict, "PROJECT_ARCHIVED", "Archived projects are read-only")
		}
		if row.ProjectVersion != expectedVersion {
			return projectVersionConflict()
		}
		if a.artifactStore == nil {
			return newProjectRequestError(http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Project attachment storage is unavailable")
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		if err := recordProjectAttachmentDeletionTombstone(tx, row.ProjectAttachment, "attachment", now); err != nil {
			return err
		}
		fileMissing := false
		trashed, err := a.artifactStore.moveObjectToTrash(row.RelativePath, row.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fileMissing = true
			} else {
				return newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The project attachment could not be moved to controlled trash")
			}
		} else {
			moved = &trashed
		}
		updates := map[string]any{
			"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": reason,
		}
		if fileMissing {
			updates["integrity_status"] = "missing"
			updates["integrity_checked_at"] = now
		}
		result := tx.Model(&models.ProjectAttachment{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
		if result.Error != nil {
			return mapProjectAttachmentConstraintError(result.Error)
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(http.StatusConflict, "PROJECT_ATTACHMENT_ALREADY_DELETED", "The project attachment is already deleted")
		}
		deleted, err := loadProjectAttachmentRow(tx, id)
		if err != nil {
			return err
		}
		response = projectAttachmentResponseFromRow(deleted)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, row.ProjectID, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
		if moved != nil {
			_ = a.artifactStore.restoreTrashedFile(*moved)
		}
		if writeProjectRequestError(c, mapProjectAttachmentConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if moved != nil {
		a.artifactStore.purgeTrashedFile(*moved)
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.ProjectVersion)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) readProjectAttachmentUpload(c *gin.Context) (createProjectAttachmentMetadata, stagedArtifactFile, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be multipart/form-data")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArtifactRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "metadata" || part.FileName() != "" {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The metadata text field must be the first multipart part")
	}
	metadataBytes, err := io.ReadAll(io.LimitReader(part, maxClientAttachmentMetadataBytes+1))
	if err != nil || part.Close() != nil {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	if len(metadataBytes) > maxClientAttachmentMetadataBytes {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Project attachment metadata cannot exceed 1 MiB")
	}
	var metadata createProjectAttachmentMetadata
	if err := decodeStrictJSONBytes(metadataBytes, &metadata); err != nil {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The project attachment metadata is not valid JSON")
	}
	part, err = reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one file part must follow metadata")
	}
	if strings.TrimSpace(metadata.Name) == "" {
		metadata.Name = part.FileName()
	}
	id := uuid.NewString()
	staged, err := a.artifactStore.stageMultipartFile(part, id)
	if err != nil {
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, multipartFileStageError(err)
	}
	if err := part.Close(); err != nil {
		a.artifactStore.discardStagedFile(staged)
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	extra, err := reader.NextPart()
	if err == nil {
		_ = extra.Close()
		a.artifactStore.discardStagedFile(staged)
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Only one metadata field and one file are allowed")
	}
	if !errors.Is(err, io.EOF) {
		a.artifactStore.discardStagedFile(staged)
		return createProjectAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	return metadata, staged, nil
}

func projectAttachmentRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("project_attachments AS attachment").Select(`
		attachment.*,
		recorded.type AS recorded_by_type,
		recorded.display_name AS recorded_by_display_name,
		projects.version AS project_version,
		projects.status AS project_status
	`).Joins("JOIN actors recorded ON recorded.id = attachment.recorded_by_actor_id").
		Joins("JOIN projects ON projects.id = attachment.project_id")
}

func loadProjectAttachmentRow(db *gorm.DB, id string) (projectAttachmentRow, error) {
	var row projectAttachmentRow
	err := projectAttachmentRowsQuery(db).Where("attachment.id = ?", id).Take(&row).Error
	return row, err
}

func projectAttachmentResponseFromRow(row projectAttachmentRow) projectAttachmentResponse {
	response := projectAttachmentResponse{
		ID: row.ID, ProjectID: row.ProjectID, Name: row.Name, MimeType: row.MimeType,
		SizeBytes: row.SizeBytes, SHA256: row.SHA256,
		RecordedBy:      clientAttachmentActorResponse{ID: row.RecordedByActorID, Type: row.RecordedByType, DisplayName: row.RecordedByDisplayName},
		IntegrityStatus: row.IntegrityStatus, IntegrityCheckedAt: normalizeTimestamp(row.IntegrityCheckedAt),
		DeletedAt: row.DeletedAt, DeletedByActorID: row.DeletedByActorID, DeleteReason: row.DeleteReason,
		CreatedAt: normalizeTimestamp(row.CreatedAt), ProjectVersion: row.ProjectVersion,
	}
	if response.DeletedAt != nil {
		normalized := normalizeTimestamp(*response.DeletedAt)
		response.DeletedAt = &normalized
	}
	return response
}

func projectAttachmentID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed.String() != raw {
		writeError(c, http.StatusBadRequest, "INVALID_PROJECT_ATTACHMENT_ID", "Project attachment id must be a canonical UUID")
		return "", false
	}
	return parsed.String(), true
}

func (a *API) persistProjectAttachmentIntegrity(c *gin.Context, id, status string) bool {
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	result := a.db.WithContext(c.Request.Context()).Model(&models.ProjectAttachment{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"integrity_status": status, "integrity_checked_at": now})
	if result.Error == nil && result.RowsAffected == 1 {
		return true
	}
	if a.options.Logger != nil {
		a.options.Logger.Printf("project attachment integrity update failed attachment_id=%s request_id=%s rows=%d error=%v", id, requestIDFromContext(c), result.RowsAffected, result.Error)
	}
	writeDatabaseError(c)
	return false
}

func (a *API) rejectProjectAttachmentIntegrity(c *gin.Context, id, status string) {
	if !a.persistProjectAttachmentIntegrity(c, id, status) {
		return
	}
	writeError(c, http.StatusConflict, "PROJECT_ATTACHMENT_INTEGRITY_MISMATCH", "The project attachment failed its integrity check")
}

func recordProjectAttachmentDeletionTombstone(tx *gorm.DB, attachment models.ProjectAttachment, scope, deletedAt string) error {
	return tx.Create(&models.ProjectAttachmentDeletionTombstone{
		AttachmentID: attachment.ID, ProjectID: attachment.ProjectID, RelativePath: attachment.RelativePath,
		SizeBytes: attachment.SizeBytes, SHA256: attachment.SHA256, DeletionScope: scope, DeletedAt: deletedAt,
	}).Error
}

func (a *API) trashProjectAttachmentFiles(tx *gorm.DB, projectIDValue, deletedAt string) ([]trashedArtifactFile, error) {
	var attachments []models.ProjectAttachment
	if err := tx.Select("id", "project_id", "relative_path", "size_bytes", "sha256").
		Where("project_id = ? AND deleted_at IS NULL", projectIDValue).Order("id ASC").Find(&attachments).Error; err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	if a.artifactStore == nil {
		return nil, newProjectRequestError(http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Project attachment storage is unavailable")
	}
	moved := make([]trashedArtifactFile, 0, len(attachments))
	for _, attachment := range attachments {
		if err := recordProjectAttachmentDeletionTombstone(tx, attachment, "project", deletedAt); err != nil {
			return moved, err
		}
		entry, err := a.artifactStore.moveObjectToTrash(attachment.RelativePath, attachment.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return moved, newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "Project attachment files could not be prepared for deletion")
		}
		moved = append(moved, entry)
	}
	return moved, nil
}

func (a *API) restoreProjectAttachmentFiles(moved []trashedArtifactFile) error {
	if a.artifactStore == nil {
		return nil
	}
	var restoreErr error
	for index := len(moved) - 1; index >= 0; index-- {
		restoreErr = errors.Join(restoreErr, a.artifactStore.restoreTrashedFile(moved[index]))
	}
	return restoreErr
}

func mapProjectAttachmentConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "CONTROLLED_OBJECT_ID_CONFLICT"):
		return newProjectRequestError(http.StatusConflict, "ATTACHMENT_ID_CONFLICT", "The generated attachment id is already in use")
	case strings.Contains(message, "PROJECT_ATTACHMENT_FACTS_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "PROJECT_ATTACHMENT_IMMUTABLE", "Project attachment facts cannot be changed")
	default:
		return err
	}
}
