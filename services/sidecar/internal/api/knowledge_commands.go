package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared knowledge-base command layer. The native HTTP handlers and approved
// AI proposals both call these helpers, so a confirmed proposal runs exactly
// the same domain validation and single transaction as a human page action.
// Every helper is transaction-scoped: callers own commit, rollback and any
// post-commit side effect such as enqueueing an immutable job identity.

const maxKnowledgeDeleteReasonRunes = 500

// Agent-authored knowledge text is deliberately far below the 16 MiB managed
// copy limit: the bytes arrive inside a model tool call and an immutable
// approval payload, so both stay inside the existing 64 KiB request and
// action_json budgets.
const maxAIKnowledgeSourceBytes = 24_000

type knowledgeSourceDeletion struct {
	SourceID      string
	Name          string
	SourceType    string
	Status        string
	Version       int64
	NextVersion   int64
	ChunkCount    int64
	DocumentCount int64
}

// finalizeKnowledgeUpload applies the same name/format/content rules as the
// multipart upload path so an agent-authored source cannot bypass validation.
func finalizeKnowledgeUpload(upload knowledgeUpload) (knowledgeUpload, error) {
	if upload.Name == "" || upload.Name == "." {
		return upload, newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_INVALID_FILENAME", "The source name is not valid")
	}
	if len(upload.Bytes) == 0 {
		return upload, newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_EMPTY_SOURCE", "The source content is empty")
	}
	sourceType, mimeType, err := knowledgeFileType(upload.Name)
	if err != nil {
		return upload, err
	}
	upload.SourceType, upload.MimeType = sourceType, mimeType
	if sourceType == "pdf" {
		if err := validateKnowledgePDFUpload(upload.Bytes); err != nil {
			return upload, err
		}
	} else if _, err := extractKnowledgeText(upload.Name, upload.Bytes); err != nil {
		return upload, err
	}
	if upload.Title == "" {
		upload.Title = upload.Name
	}
	if utf8.RuneCountInString(upload.Title) > 255 {
		return upload, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title must be at most 255 characters")
	}
	return upload, nil
}

// createKnowledgeSourceInTransaction inserts one managed source and its queued
// import job. The caller owns commit and the post-commit indexer enqueue.
func createKnowledgeSourceInTransaction(tx *gorm.DB, upload knowledgeUpload, sourceID, jobID, now string) (models.KnowledgeSource, models.KnowledgeIndexJob, error) {
	source := models.KnowledgeSource{
		ID: sourceID, Name: upload.Name, Title: upload.Title, SourceType: upload.SourceType, ImportMode: "managed_copy",
		MimeType: upload.MimeType, SizeBytes: int64(len(upload.Bytes)), ContentSHA256: knowledgeSHA256(upload.Bytes),
		OriginalContent: upload.Bytes, Status: "indexing",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	job := models.KnowledgeIndexJob{
		ID: jobID, SourceID: sourceID, Operation: "import", Status: "queued", Stage: "queued",
		Progress: 0, Attempt: 1, CreatedAt: now,
	}
	if err := tx.Create(&source).Error; err != nil {
		return source, job, err
	}
	if err := tx.Create(&job).Error; err != nil {
		return source, job, err
	}
	return source, job, nil
}

func isValidKnowledgeID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func normalizeKnowledgeDeleteReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "User requested deletion"
	}
	if utf8.RuneCountInString(reason) > maxKnowledgeDeleteReasonRunes {
		return "", newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason must be at most 500 characters")
	}
	return reason, nil
}

// knowledgeSourceDeletionPreview reads the exact deletion effects without
// writing. Both the native confirmation and the AI approval card reuse it so a
// human never approves a count that was not read from the current facts.
func knowledgeSourceDeletionPreview(tx *gorm.DB, sourceID string, expectedVersion int64) (knowledgeSourceDeletion, error) {
	preview := knowledgeSourceDeletion{SourceID: sourceID}
	var source models.KnowledgeSource
	if err := tx.Select("id", "name", "source_type", "status", "version", "deleted_at").First(&source, "id = ?", sourceID).Error; err != nil {
		return preview, err
	}
	if source.DeletedAt != nil {
		return preview, newProjectRequestError(http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
	}
	if source.Version != expectedVersion {
		return preview, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
	}
	if err := tx.Model(&models.KnowledgeChunk{}).Where("source_id = ?", sourceID).Count(&preview.ChunkCount).Error; err != nil {
		return preview, err
	}
	if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", sourceID).Count(&preview.DocumentCount).Error; err != nil {
		return preview, err
	}
	preview.Name, preview.SourceType, preview.Status, preview.Version = source.Name, source.SourceType, source.Status, source.Version
	preview.NextVersion = source.Version + 1
	return preview, nil
}

// deleteKnowledgeSourceInTransaction soft-deletes one source, drops its
// extracted documents/chunks and keeps the audit reason. The stored managed
// copy is cleared; the caller must have already frozen the deletion preview.
func deleteKnowledgeSourceInTransaction(tx *gorm.DB, sourceID string, expectedVersion int64, reason, now string) (knowledgeSourceDeletion, error) {
	result := knowledgeSourceDeletion{SourceID: sourceID}
	normalizedReason, err := normalizeKnowledgeDeleteReason(reason)
	if err != nil {
		return result, err
	}
	var source models.KnowledgeSource
	if err := tx.First(&source, "id = ?", sourceID).Error; err != nil {
		return result, err
	}
	if source.DeletedAt != nil {
		return result, newProjectRequestError(http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
	}
	if source.Version != expectedVersion {
		return result, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
	}
	if err := tx.Model(&models.KnowledgeChunk{}).Where("source_id = ?", sourceID).Count(&result.ChunkCount).Error; err != nil {
		return result, err
	}
	if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", sourceID).Count(&result.DocumentCount).Error; err != nil {
		return result, err
	}
	if err := tx.Where("source_id = ?", sourceID).Delete(&models.KnowledgeDocument{}).Error; err != nil {
		return result, err
	}
	nextVersion := source.Version + 1
	update := tx.Model(&models.KnowledgeSource{}).Where("id = ? AND version = ?", sourceID, source.Version).Updates(map[string]any{
		"original_content": nil, "status": "deleted", "deleted_at": now, "delete_reason": normalizedReason,
		"last_indexed_at": nil, "version": nextVersion, "updated_at": now,
	})
	if update.Error != nil {
		return result, update.Error
	}
	if update.RowsAffected != 1 {
		return result, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
	}
	result.Name, result.SourceType, result.Status, result.Version, result.NextVersion = source.Name, source.SourceType, source.Status, source.Version, nextVersion
	return result, nil
}

// queueKnowledgeReindexInTransaction records one queued import/reindex job and
// moves the source to indexing. It returns the immutable job identity so the
// caller can enqueue it only after the surrounding transaction commits.
func queueKnowledgeReindexInTransaction(tx *gorm.DB, sourceID string, expectedVersion int64, retryOf *models.KnowledgeIndexJob, now string) (models.KnowledgeSource, models.KnowledgeIndexJob, error) {
	var source models.KnowledgeSource
	if err := tx.First(&source, "id = ? AND deleted_at IS NULL", sourceID).Error; err != nil {
		return source, models.KnowledgeIndexJob{}, err
	}
	if source.Version != expectedVersion {
		return source, models.KnowledgeIndexJob{}, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
	}
	if source.Status == "indexing" {
		return source, models.KnowledgeIndexJob{}, newProjectRequestError(http.StatusConflict, "KNOWLEDGE_INDEX_IN_PROGRESS", "The knowledge source is already being indexed")
	}
	if len(source.OriginalContent) == 0 {
		return source, models.KnowledgeIndexJob{}, newProjectRequestError(http.StatusConflict, "KNOWLEDGE_SOURCE_UNAVAILABLE", "The managed source copy is unavailable")
	}
	operation := "reindex"
	if retryOf != nil {
		operation = retryOf.Operation
	}
	job := models.KnowledgeIndexJob{
		ID: uuid.NewString(), SourceID: sourceID, Operation: operation,
		Status: "queued", Stage: "queued", Progress: 0, Attempt: 1, CreatedAt: now,
	}
	if retryOf != nil {
		job.Attempt = retryOf.Attempt + 1
		job.RetryOfJobID = &retryOf.ID
	}
	if err := tx.Create(&job).Error; err != nil {
		return source, job, err
	}
	update := tx.Model(&models.KnowledgeSource{}).
		Where("id = ? AND version = ? AND status <> 'indexing'", sourceID, expectedVersion).
		Updates(map[string]any{"status": "indexing", "version": expectedVersion + 1, "updated_at": now})
	if update.Error != nil {
		return source, job, update.Error
	}
	if update.RowsAffected != 1 {
		return source, job, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
	}
	source.Status, source.Version, source.UpdatedAt = "indexing", expectedVersion+1, now
	return source, job, nil
}

// cancelKnowledgeIndexJobInTransaction marks one queued/running job cancelled
// and restores the source status from durable documents. The running indexer
// observes cancel_requested on its next claim/stage write.
func cancelKnowledgeIndexJobInTransaction(tx *gorm.DB, jobID, now string) (models.KnowledgeIndexJob, error) {
	var job models.KnowledgeIndexJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return job, err
	}
	if job.Status != "queued" && job.Status != "running" {
		return job, newProjectRequestError(http.StatusConflict, "KNOWLEDGE_JOB_TERMINAL", "Completed index jobs cannot be cancelled")
	}
	if err := tx.Model(&models.KnowledgeIndexJob{}).Where("id = ?", jobID).Updates(map[string]any{
		"status": "cancelled", "stage": "complete", "progress": job.Progress,
		"cancel_requested": true, "completed_at": now,
	}).Error; err != nil {
		return job, err
	}
	var documentCount int64
	if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", job.SourceID).Count(&documentCount).Error; err != nil {
		return job, err
	}
	sourceStatus := "failed"
	if documentCount > 0 {
		sourceStatus = "ready"
	}
	if err := tx.Model(&models.KnowledgeSource{}).
		Where("id = ? AND status = 'indexing' AND deleted_at IS NULL", job.SourceID).
		Updates(map[string]any{"status": sourceStatus, "version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
		return job, err
	}
	job.Status, job.Stage, job.CancelRequested, job.CompletedAt = "cancelled", "complete", true, &now
	return job, nil
}

// loadRetryableKnowledgeIndexJob validates that one failed/cancelled job may be
// retried. Retry always creates a new attempt; the original job is never edited.
func loadRetryableKnowledgeIndexJob(tx *gorm.DB, jobID string) (models.KnowledgeIndexJob, error) {
	var job models.KnowledgeIndexJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return job, err
	}
	if job.Status != "failed" && job.Status != "cancelled" {
		return job, newProjectRequestError(http.StatusConflict, "KNOWLEDGE_JOB_NOT_RETRYABLE", "Only failed or cancelled index jobs can be retried")
	}
	return job, nil
}

func knowledgeJobNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "KNOWLEDGE_JOB_NOT_FOUND", "Knowledge index job not found")
	}
	return err
}

func knowledgeSourceNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return newProjectRequestError(http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
	}
	return err
}
