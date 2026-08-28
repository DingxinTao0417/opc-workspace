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
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const maxClientAttachmentMetadataBytes = 1 << 20

type createClientAttachmentMetadata struct {
	Name       string  `json:"name"`
	ActivityID *string `json:"activity_id"`
}

type deleteClientAttachmentRequest struct {
	Reason string `json:"reason"`
}

type clientAttachmentActorResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
}

type clientAttachmentResponse struct {
	ID                 string                        `json:"id"`
	ClientID           string                        `json:"client_id"`
	ActivityID         *string                       `json:"activity_id"`
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
	ClientVersion      int64                         `json:"client_version"`
}

type clientAttachmentRow struct {
	models.ClientAttachment
	RecordedByType        string `gorm:"column:recorded_by_type"`
	RecordedByDisplayName string `gorm:"column:recorded_by_display_name"`
	ClientVersion         int64  `gorm:"column:client_version"`
}

type clientAttachmentMeta struct {
	Page          int   `json:"page"`
	PageSize      int   `json:"page_size"`
	Total         int64 `json:"total"`
	ClientVersion int64 `json:"client_version"`
}

func (a *API) listClientAttachments(c *gin.Context) {
	clientIDValue, ok := clientID(c)
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
	activityID := strings.TrimSpace(c.Query("activity_id"))
	if activityID != "" {
		parsed, err := uuid.Parse(activityID)
		if err != nil || parsed.String() != activityID {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "activity_id must be a canonical UUID")
			return
		}
	}

	var client models.Client
	var total int64
	var rows []clientAttachmentRow
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&client, "id = ?", clientIDValue).Error; err != nil {
			return err
		}
		query := clientAttachmentRowsQuery(tx).Where("attachment.client_id = ?", clientIDValue)
		if !includeDeleted {
			query = query.Where("attachment.deleted_at IS NULL")
		}
		if activityID != "" {
			query = query.Where("attachment.activity_id = ?", activityID)
		}
		countQuery := tx.Table("client_attachments").Where("client_id = ?", clientIDValue)
		if !includeDeleted {
			countQuery = countQuery.Where("deleted_at IS NULL")
		}
		if activityID != "" {
			countQuery = countQuery.Where("activity_id = ?", activityID)
		}
		if err := countQuery.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("attachment.created_at DESC").Order("attachment.id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	responses := make([]clientAttachmentResponse, len(rows))
	for index := range rows {
		responses[index] = clientAttachmentResponseFromRow(rows[index])
	}
	setProjectETag(c, client.Version)
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": clientAttachmentMeta{
		Page: page, PageSize: pageSize, Total: total, ClientVersion: client.Version,
	}})
}

func (a *API) createClientAttachment(c *gin.Context) {
	clientIDValue, ok := clientID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Client attachment storage is unavailable")
		return
	}
	metadata, staged, err := a.readClientAttachmentUpload(c)
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
	activityID, err := cleanClientAttachmentActivityID(metadata.ActivityID)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"client_id": clientIDValue, "expected_version": expectedVersion, "activity_id": activityID,
		"name": name, "size_bytes": staged.sizeBytes, "sha256": staged.sha256,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/clients/%s/attachments", clientIDValue)
	statusCode := http.StatusCreated
	replayed := false
	committed := false
	var response clientAttachmentResponse
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
		var client models.Client
		if err := tx.Select("id", "version").First(&client, "id = ?", clientIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
			}
			return err
		}
		if client.Version != expectedVersion {
			return clientVersionConflict()
		}
		if activityID != nil {
			var count int64
			if err := tx.Model(&models.ClientActivity{}).
				Where("id = ? AND client_id = ? AND deleted_at IS NULL", *activityID, clientIDValue).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_UNAVAILABLE", "The selected client activity is unavailable")
			}
		}
		if err := a.artifactStore.commitStagedFile(staged); err != nil {
			return newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The client attachment could not be committed to controlled storage")
		}
		committed = true
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		attachment := models.ClientAttachment{
			ID: strings.TrimPrefix(staged.relativePath, "objects/"), ClientID: clientIDValue, ActivityID: activityID,
			Name: name, RelativePath: staged.relativePath, MimeType: staged.mimeType,
			SizeBytes: staged.sizeBytes, SHA256: staged.sha256, RecordedByActorID: models.BuiltinOwnerActorID,
			IntegrityStatus: "verified", IntegrityCheckedAt: now, CreatedAt: now,
		}
		if err := tx.Create(&attachment).Error; err != nil {
			return mapClientAttachmentConstraintError(err)
		}
		row, err := loadClientAttachmentRow(tx, attachment.ID)
		if err != nil {
			return err
		}
		response = clientAttachmentResponseFromRow(row)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, attachment.ID, requestHash, http.StatusCreated, response, now)
	})
	if err != nil {
		if committed {
			_ = a.artifactStore.discardCommittedFile(staged.relativePath)
		}
		if writeProjectRequestError(c, mapClientAttachmentConstraintError(err)) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if !replayed {
		cleanupStaged = false
	} // replay keeps the newly uploaded duplicate staged file eligible for cleanup
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.ClientVersion)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getClientAttachment(c *gin.Context) {
	id, ok := clientAttachmentID(c)
	if !ok {
		return
	}
	row, err := loadClientAttachmentRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_ATTACHMENT_NOT_FOUND", "Client attachment not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := clientAttachmentResponseFromRow(row)
	setProjectETag(c, response.ClientVersion)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) getClientAttachmentContent(c *gin.Context) {
	id, ok := clientAttachmentID(c)
	if !ok {
		return
	}
	row, err := loadClientAttachmentRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "CLIENT_ATTACHMENT_NOT_FOUND", "Client attachment not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	if row.DeletedAt != nil {
		writeError(c, http.StatusGone, "CLIENT_ATTACHMENT_DELETED", "The client attachment has been deleted")
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Client attachment storage is unavailable")
		return
	}
	file, info, err := a.artifactStore.openObject(row.RelativePath)
	if err != nil {
		if errors.Is(err, errArtifactObjectMissing) {
			if !a.persistClientAttachmentIntegrity(c, id, "missing") {
				return
			}
			writeError(c, http.StatusGone, "CLIENT_ATTACHMENT_FILE_MISSING", "The client attachment file is missing from controlled storage")
			return
		}
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The client attachment could not be opened from controlled storage")
		return
	}
	defer file.Close()
	if info.Size() != row.SizeBytes {
		a.rejectClientAttachmentIntegrity(c, id, "mismatch")
		return
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The client attachment could not be verified")
		return
	}
	if hex.EncodeToString(digest.Sum(nil)) != row.SHA256 {
		a.rejectClientAttachmentIntegrity(c, id, "mismatch")
		return
	}
	if !a.persistClientAttachmentIntegrity(c, id, "verified") {
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(c, http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The client attachment could not be read")
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
		a.options.Logger.Printf("client attachment stream failed attachment_id=%s request_id=%s error=%v", id, requestIDFromContext(c), err)
	}
}

func (a *API) deleteClientAttachment(c *gin.Context) {
	id, ok := clientAttachmentID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm=true is required to delete a client attachment")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input deleteClientAttachmentRequest
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
	endpoint := fmt.Sprintf("DELETE /api/v1/client-attachments/%s", id)
	replayed := false
	var moved *trashedArtifactFile
	var response clientAttachmentResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			return nil
		}
		row, err := loadClientAttachmentRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "CLIENT_ATTACHMENT_NOT_FOUND", "Client attachment not found")
			}
			return err
		}
		if row.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ATTACHMENT_ALREADY_DELETED", "The client attachment is already deleted")
		}
		if row.ClientVersion != expectedVersion {
			return clientVersionConflict()
		}
		if a.artifactStore == nil {
			return newProjectRequestError(http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Client attachment storage is unavailable")
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		if err := recordClientAttachmentDeletionTombstone(tx, row.ClientAttachment, "attachment", now); err != nil {
			return err
		}
		fileMissing := false
		trashed, err := a.artifactStore.moveObjectToTrash(row.RelativePath, row.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fileMissing = true
			} else {
				return newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "The client attachment could not be moved to controlled trash")
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
		result := tx.Model(&models.ClientAttachment{}).Where("id = ? AND deleted_at IS NULL", id).Updates(updates)
		if result.Error != nil {
			return mapClientAttachmentConstraintError(result.Error)
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(http.StatusConflict, "CLIENT_ATTACHMENT_ALREADY_DELETED", "The client attachment is already deleted")
		}
		deleted, err := loadClientAttachmentRow(tx, id)
		if err != nil {
			return err
		}
		response = clientAttachmentResponseFromRow(deleted)
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, row.ClientID, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
		if moved != nil {
			_ = a.artifactStore.restoreTrashedFile(*moved)
		}
		if writeProjectRequestError(c, mapClientAttachmentConstraintError(err)) {
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
	setProjectETag(c, response.ClientVersion)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) readClientAttachmentUpload(c *gin.Context) (createClientAttachmentMetadata, stagedArtifactFile, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be multipart/form-data")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArtifactRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "metadata" || part.FileName() != "" {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The metadata text field must be the first multipart part")
	}
	metadataBytes, err := io.ReadAll(io.LimitReader(part, maxClientAttachmentMetadataBytes+1))
	if err != nil || part.Close() != nil {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	if len(metadataBytes) > maxClientAttachmentMetadataBytes {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Client attachment metadata cannot exceed 1 MiB")
	}
	var metadata createClientAttachmentMetadata
	if err := decodeStrictJSONBytes(metadataBytes, &metadata); err != nil {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The client attachment metadata is not valid JSON")
	}
	part, err = reader.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one file part must follow metadata")
	}
	if strings.TrimSpace(metadata.Name) == "" {
		metadata.Name = part.FileName()
	}
	id := uuid.NewString()
	staged, err := a.artifactStore.stageMultipartFile(part, id)
	if err != nil {
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, multipartFileStageError(err)
	}
	if err := part.Close(); err != nil {
		a.artifactStore.discardStagedFile(staged)
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	extra, err := reader.NextPart()
	if err == nil {
		_ = extra.Close()
		a.artifactStore.discardStagedFile(staged)
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Only one metadata field and one file are allowed")
	}
	if !errors.Is(err, io.EOF) {
		a.artifactStore.discardStagedFile(staged)
		return createClientAttachmentMetadata{}, stagedArtifactFile{}, multipartRequestReadError(err)
	}
	return metadata, staged, nil
}

func clientAttachmentRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("client_attachments AS attachment").Select(`
		attachment.*,
		recorded.type AS recorded_by_type,
		recorded.display_name AS recorded_by_display_name,
		clients.version AS client_version
	`).Joins("JOIN actors recorded ON recorded.id = attachment.recorded_by_actor_id").
		Joins("JOIN clients ON clients.id = attachment.client_id")
}

func loadClientAttachmentRow(db *gorm.DB, id string) (clientAttachmentRow, error) {
	var row clientAttachmentRow
	err := clientAttachmentRowsQuery(db).Where("attachment.id = ?", id).Take(&row).Error
	return row, err
}

func clientAttachmentResponseFromRow(row clientAttachmentRow) clientAttachmentResponse {
	response := clientAttachmentResponse{
		ID: row.ID, ClientID: row.ClientID, ActivityID: row.ActivityID, Name: row.Name,
		MimeType: row.MimeType, SizeBytes: row.SizeBytes, SHA256: row.SHA256,
		RecordedBy:      clientAttachmentActorResponse{ID: row.RecordedByActorID, Type: row.RecordedByType, DisplayName: row.RecordedByDisplayName},
		IntegrityStatus: row.IntegrityStatus, IntegrityCheckedAt: normalizeTimestamp(row.IntegrityCheckedAt),
		DeletedAt: row.DeletedAt, DeletedByActorID: row.DeletedByActorID, DeleteReason: row.DeleteReason,
		CreatedAt: normalizeTimestamp(row.CreatedAt), ClientVersion: row.ClientVersion,
	}
	if response.DeletedAt != nil {
		normalized := normalizeTimestamp(*response.DeletedAt)
		response.DeletedAt = &normalized
	}
	return response
}

func clientAttachmentID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed.String() != raw {
		writeError(c, http.StatusBadRequest, "INVALID_CLIENT_ATTACHMENT_ID", "Client attachment id must be a canonical UUID")
		return "", false
	}
	return parsed.String(), true
}

func cleanClientAttachmentName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 255 || hasUnsafeControlCharacters(value) {
		return "", errors.New("name must contain 1 to 255 safe characters")
	}
	return value, nil
}

func cleanClientAttachmentActivityID(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return nil, errors.New("activity_id must be a canonical UUID")
	}
	return &value, nil
}

func (a *API) persistClientAttachmentIntegrity(c *gin.Context, id, status string) bool {
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	result := a.db.WithContext(c.Request.Context()).Model(&models.ClientAttachment{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"integrity_status": status, "integrity_checked_at": now})
	if result.Error == nil && result.RowsAffected == 1 {
		return true
	}
	if a.options.Logger != nil {
		a.options.Logger.Printf("client attachment integrity update failed attachment_id=%s request_id=%s rows=%d error=%v", id, requestIDFromContext(c), result.RowsAffected, result.Error)
	}
	writeDatabaseError(c)
	return false
}

func (a *API) rejectClientAttachmentIntegrity(c *gin.Context, id, status string) {
	if !a.persistClientAttachmentIntegrity(c, id, status) {
		return
	}
	writeError(c, http.StatusConflict, "CLIENT_ATTACHMENT_INTEGRITY_MISMATCH", "The client attachment failed its integrity check")
}

func recordClientAttachmentDeletionTombstone(tx *gorm.DB, attachment models.ClientAttachment, scope, deletedAt string) error {
	return tx.Create(&models.ClientAttachmentDeletionTombstone{
		AttachmentID: attachment.ID, ClientID: attachment.ClientID, RelativePath: attachment.RelativePath,
		SizeBytes: attachment.SizeBytes, SHA256: attachment.SHA256, DeletionScope: scope, DeletedAt: deletedAt,
	}).Error
}

func (a *API) trashClientAttachmentFiles(tx *gorm.DB, clientIDValue, deletedAt string) ([]trashedArtifactFile, error) {
	var attachments []models.ClientAttachment
	if err := tx.Select("id", "client_id", "relative_path", "size_bytes", "sha256").
		Where("client_id = ? AND deleted_at IS NULL", clientIDValue).Order("id ASC").Find(&attachments).Error; err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	if a.artifactStore == nil {
		return nil, newProjectRequestError(http.StatusServiceUnavailable, "ATTACHMENT_STORAGE_UNAVAILABLE", "Client attachment storage is unavailable")
	}
	moved := make([]trashedArtifactFile, 0, len(attachments))
	for _, attachment := range attachments {
		if err := recordClientAttachmentDeletionTombstone(tx, attachment, "client", deletedAt); err != nil {
			return moved, err
		}
		entry, err := a.artifactStore.moveObjectToTrash(attachment.RelativePath, attachment.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return moved, newProjectRequestError(http.StatusInternalServerError, "ATTACHMENT_STORAGE_ERROR", "Client attachment files could not be prepared for deletion")
		}
		moved = append(moved, entry)
	}
	return moved, nil
}

func (a *API) restoreClientAttachmentFiles(moved []trashedArtifactFile) error {
	if a.artifactStore == nil {
		return nil
	}
	var restoreErr error
	for index := len(moved) - 1; index >= 0; index-- {
		restoreErr = errors.Join(restoreErr, a.artifactStore.restoreTrashedFile(moved[index]))
	}
	return restoreErr
}

func mapClientAttachmentConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "CLIENT_ATTACHMENT_ACTIVITY_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "CLIENT_ACTIVITY_UNAVAILABLE", "The selected client activity is unavailable")
	case strings.Contains(message, "CONTROLLED_OBJECT_ID_CONFLICT"):
		return newProjectRequestError(http.StatusConflict, "ATTACHMENT_ID_CONFLICT", "The generated attachment id is already in use")
	case strings.Contains(message, "CLIENT_ATTACHMENT_FACTS_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "CLIENT_ATTACHMENT_IMMUTABLE", "Client attachment facts cannot be changed")
	default:
		return err
	}
}
