package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxKnowledgeSourceBytes      int64 = 16 << 20
	maxKnowledgeRequestBytes     int64 = maxKnowledgeSourceBytes + (1 << 20)
	maxKnowledgeQueryRunes             = 256
	knowledgeChunkRunes                = 1200
	knowledgeChunkOverlap              = 160
	knowledgeExtractorVersion          = "plain-text-v1"
	knowledgePDFExtractorVersion       = "pdf-text-v1"
)

type knowledgeSourceRow struct {
	models.KnowledgeSource
	DocumentID               *string `gorm:"column:document_id"`
	DocumentVersion          *int64  `gorm:"column:document_version"`
	ChunkCount               int64   `gorm:"column:chunk_count"`
	LatestJobID              *string `gorm:"column:latest_job_id"`
	LatestJobOperation       *string `gorm:"column:latest_job_operation"`
	LatestJobStatus          *string `gorm:"column:latest_job_status"`
	LatestJobStage           *string `gorm:"column:latest_job_stage"`
	LatestJobProgress        *int    `gorm:"column:latest_job_progress"`
	LatestJobAttempt         *int    `gorm:"column:latest_job_attempt"`
	LatestJobRetryOfID       *string `gorm:"column:latest_job_retry_of_id"`
	LatestJobErrorCode       *string `gorm:"column:latest_job_error_code"`
	LatestJobCancelRequested *bool   `gorm:"column:latest_job_cancel_requested"`
	LatestJobStartedAt       *string `gorm:"column:latest_job_started_at"`
	LatestJobCompletedAt     *string `gorm:"column:latest_job_completed_at"`
	LatestJobCreatedAt       *string `gorm:"column:latest_job_created_at"`
}

type knowledgeSourceResponse struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Title           string                    `json:"title"`
	SourceType      string                    `json:"source_type"`
	ImportMode      string                    `json:"import_mode"`
	MimeType        string                    `json:"mime_type"`
	SizeBytes       int64                     `json:"size_bytes"`
	ContentSHA256   string                    `json:"content_sha256"`
	Status          string                    `json:"status"`
	LastIndexedAt   *string                   `json:"last_indexed_at"`
	DeletedAt       *string                   `json:"deleted_at"`
	DeleteReason    *string                   `json:"delete_reason"`
	Version         int64                     `json:"version"`
	CreatedAt       string                    `json:"created_at"`
	UpdatedAt       string                    `json:"updated_at"`
	DocumentID      *string                   `json:"document_id"`
	DocumentVersion *int64                    `json:"document_version"`
	ChunkCount      int64                     `json:"chunk_count"`
	LatestJob       *models.KnowledgeIndexJob `json:"latest_job"`
}

type knowledgeDocumentRow struct {
	models.KnowledgeDocument
	SourceName string `gorm:"column:source_name"`
	SourceType string `gorm:"column:source_type"`
	ChunkCount int64  `gorm:"column:chunk_count"`
}

type knowledgeDocumentResponse struct {
	ID               string `json:"id"`
	SourceID         string `json:"source_id"`
	SourceName       string `json:"source_name"`
	SourceType       string `json:"source_type"`
	Title            string `json:"title"`
	Language         string `json:"language"`
	ExtractorVersion string `json:"extractor_version"`
	ContentSHA256    string `json:"content_sha256"`
	Status           string `json:"status"`
	Version          int64  `json:"version"`
	ChunkCount       int64  `json:"chunk_count"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	ContentText      string `json:"content_text,omitempty"`
}

type knowledgeImportResponse struct {
	Source knowledgeSourceResponse  `json:"source"`
	Job    models.KnowledgeIndexJob `json:"job"`
}

type knowledgeSearchRequest struct {
	Query     string   `json:"query"`
	SourceIDs []string `json:"source_ids"`
	Limit     int      `json:"limit"`
}

type knowledgeSearchRow struct {
	ChunkID         string  `gorm:"column:chunk_id"`
	DocumentID      string  `gorm:"column:document_id"`
	SourceID        string  `gorm:"column:source_id"`
	SourceName      string  `gorm:"column:source_name"`
	SourceType      string  `gorm:"column:source_type"`
	DocumentTitle   string  `gorm:"column:document_title"`
	DocumentVersion int64   `gorm:"column:document_version"`
	ChunkIndex      int     `gorm:"column:chunk_index"`
	StartChar       int     `gorm:"column:start_char"`
	EndChar         int     `gorm:"column:end_char"`
	StartLine       int     `gorm:"column:start_line"`
	EndLine         int     `gorm:"column:end_line"`
	StartPage       int     `gorm:"column:start_page"`
	EndPage         int     `gorm:"column:end_page"`
	Content         string  `gorm:"column:content"`
	Rank            float64 `gorm:"column:rank"`
}

type knowledgeHighlight struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type knowledgeSearchResult struct {
	ChunkID         string               `json:"chunk_id"`
	DocumentID      string               `json:"document_id"`
	SourceID        string               `json:"source_id"`
	SourceName      string               `json:"source_name"`
	SourceType      string               `json:"source_type"`
	DocumentTitle   string               `json:"document_title"`
	DocumentVersion int64                `json:"document_version"`
	ChunkIndex      int                  `json:"chunk_index"`
	StartChar       int                  `json:"start_char"`
	EndChar         int                  `json:"end_char"`
	StartLine       int                  `json:"start_line"`
	EndLine         int                  `json:"end_line"`
	StartPage       int                  `json:"start_page"`
	EndPage         int                  `json:"end_page"`
	Excerpt         string               `json:"excerpt"`
	Highlights      []knowledgeHighlight `json:"highlights"`
	Rank            float64              `json:"rank"`
}

type knowledgeChunkDraft struct {
	Content   string
	StartChar int
	EndChar   int
	StartLine int
	EndLine   int
	StartPage int
	EndPage   int
}

type knowledgeUpload struct {
	Name       string
	Title      string
	SourceType string
	MimeType   string
	Bytes      []byte
}

func (a *API) listKnowledgeSources(c *gin.Context) {
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
	search := strings.TrimSpace(c.Query("search"))
	if utf8.RuneCountInString(search) > 255 {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "search must be at most 255 characters")
		return
	}

	base := a.db.WithContext(c.Request.Context()).Table("knowledge_sources AS s")
	if !includeDeleted {
		base = base.Where("s.deleted_at IS NULL")
	}
	if search != "" {
		base = base.Where("s.name LIKE ? ESCAPE '\\'", "%"+escapeKnowledgeLike(search)+"%")
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	var rows []knowledgeSourceRow
	if err := base.Select(`
		s.*, d.id AS document_id, d.version AS document_version,
		COALESCE((SELECT COUNT(*) FROM knowledge_chunks c WHERE c.source_id = s.id), 0) AS chunk_count,
		j.id AS latest_job_id, j.operation AS latest_job_operation, j.status AS latest_job_status,
		j.stage AS latest_job_stage, j.progress AS latest_job_progress, j.attempt AS latest_job_attempt,
		j.retry_of_job_id AS latest_job_retry_of_id, j.error_code AS latest_job_error_code,
		j.cancel_requested AS latest_job_cancel_requested, j.started_at AS latest_job_started_at,
		j.completed_at AS latest_job_completed_at, j.created_at AS latest_job_created_at
	`).Joins("LEFT JOIN knowledge_documents d ON d.source_id = s.id").
		Joins(latestKnowledgeJobJoin).
		Order("s.updated_at DESC").Order("s.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	items := make([]knowledgeSourceResponse, len(rows))
	for index := range rows {
		items[index] = knowledgeSourceResponseFromRow(rows[index])
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": gin.H{"page": page, "page_size": pageSize, "total": total}})
}

func (a *API) createKnowledgeSource(c *gin.Context) {
	upload, err := readKnowledgeUpload(c)
	if err != nil {
		writeKnowledgeRequestError(c, err)
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	sourceID := uuid.NewString()
	jobID := uuid.NewString()
	sourceHash := knowledgeSHA256(upload.Bytes)
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"name": upload.Name, "title": upload.Title, "source_type": upload.SourceType,
		"size_bytes": len(upload.Bytes), "content_sha256": sourceHash,
	})
	if !ok {
		return
	}
	source := models.KnowledgeSource{
		ID: sourceID, Name: upload.Name, Title: upload.Title, SourceType: upload.SourceType, ImportMode: "managed_copy",
		MimeType: upload.MimeType, SizeBytes: int64(len(upload.Bytes)), ContentSHA256: sourceHash,
		OriginalContent: upload.Bytes, Status: "indexing",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	job := models.KnowledgeIndexJob{
		ID: jobID, SourceID: sourceID, Operation: "import", Status: "queued", Stage: "queued",
		Progress: 0, Attempt: 1, CreatedAt: now,
	}
	endpoint := "POST /api/v1/knowledge/sources"
	response := knowledgeImportResponse{}
	replayed := false
	if err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, err := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if err != nil {
			return err
		}
		if replay {
			replayed = true
			return nil
		}
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		row, err := loadKnowledgeSourceRow(tx, sourceID, true)
		if err != nil {
			return err
		}
		response = knowledgeImportResponse{Source: knowledgeSourceResponseFromRow(row), Job: job}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, sourceID, requestHash, http.StatusAccepted, response, now)
	}); err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Source.Version)
	c.JSON(http.StatusAccepted, gin.H{"data": response})
	if !replayed && !a.knowledgeIndexer.enqueue(context.Background(), job.ID) {
		a.failKnowledgeIndexJob(job.ID, "KNOWLEDGE_INDEX_QUEUE_UNAVAILABLE")
	}
}

func (a *API) exportKnowledgeSourcesCSV(c *gin.Context) {
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Knowledge source export requires confirm=true")
		return
	}
	var rows []knowledgeSourceRow
	if err := a.db.WithContext(c.Request.Context()).Table("knowledge_sources AS s").Select(`
		s.*, d.id AS document_id, d.version AS document_version,
		COALESCE((SELECT COUNT(*) FROM knowledge_chunks c WHERE c.source_id = s.id), 0) AS chunk_count,
		j.id AS latest_job_id, j.operation AS latest_job_operation, j.status AS latest_job_status,
		j.stage AS latest_job_stage, j.progress AS latest_job_progress, j.attempt AS latest_job_attempt,
		j.retry_of_job_id AS latest_job_retry_of_id, j.error_code AS latest_job_error_code,
		j.cancel_requested AS latest_job_cancel_requested, j.started_at AS latest_job_started_at,
		j.completed_at AS latest_job_completed_at, j.created_at AS latest_job_created_at
	`).Joins("LEFT JOIN knowledge_documents d ON d.source_id = s.id").Joins(latestKnowledgeJobJoin).
		Order("s.created_at ASC").Order("s.id ASC").Limit(10_001).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	if len(rows) > 10_000 {
		writeError(c, http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "Knowledge source export is limited to 10000 rows")
		return
	}
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{
		"id", "name", "title", "source_type", "import_mode", "mime_type", "size_bytes",
		"content_sha256", "status", "version", "document_id", "document_version", "chunk_count",
		"latest_job_status", "latest_job_stage", "latest_job_progress", "latest_job_attempt",
		"latest_job_error_code", "last_indexed_at", "created_at", "updated_at",
	})
	for _, row := range rows {
		response := knowledgeSourceResponseFromRow(row)
		latestStatus, latestStage, latestProgress, latestAttempt, latestError := "", "", "", "", ""
		if response.LatestJob != nil {
			latestStatus = response.LatestJob.Status
			latestStage = response.LatestJob.Stage
			latestProgress = strconv.Itoa(response.LatestJob.Progress)
			latestAttempt = strconv.Itoa(response.LatestJob.Attempt)
			latestError = stringPointerValue(response.LatestJob.ErrorCode)
		}
		_ = writer.Write([]string{
			response.ID, response.Name, response.Title, response.SourceType, response.ImportMode,
			response.MimeType, strconv.FormatInt(response.SizeBytes, 10), response.ContentSHA256,
			response.Status, strconv.FormatInt(response.Version, 10), stringPointerValue(response.DocumentID),
			int64PointerString(response.DocumentVersion), strconv.FormatInt(response.ChunkCount, 10),
			latestStatus, latestStage, latestProgress, latestAttempt, latestError,
			stringPointerValue(response.LastIndexedAt), response.CreatedAt, response.UpdatedAt,
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(c, http.StatusInternalServerError, "EXPORT_FAILED", "Knowledge sources could not be encoded")
		return
	}
	filename := "knowledge-sources-" + a.options.Now().Format("20060102") + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buffer.Bytes())
}

func (a *API) getKnowledgeSource(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_SOURCE_ID")
	if !ok {
		return
	}
	row, err := loadKnowledgeSourceRow(a.db.WithContext(c.Request.Context()), id, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, row.Version)
	c.JSON(http.StatusOK, gin.H{"data": knowledgeSourceResponseFromRow(row)})
}

func (a *API) deleteKnowledgeSource(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_SOURCE_ID")
	if !ok {
		return
	}
	if c.Query("confirm") != "true" {
		writeError(c, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED", "confirm=true is required to delete a knowledge source")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	reason := strings.TrimSpace(c.Query("reason"))
	if reason == "" {
		reason = "User requested deletion"
	}
	if utf8.RuneCountInString(reason) > 500 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason must be at most 500 characters")
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	var deletedChunks int64
	var nextVersion int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var source models.KnowledgeSource
		if err := tx.First(&source, "id = ?", id).Error; err != nil {
			return err
		}
		if source.DeletedAt != nil {
			return newProjectRequestError(http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
		}
		if source.Version != expectedVersion {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
		}
		if err := tx.Model(&models.KnowledgeChunk{}).Where("source_id = ?", id).Count(&deletedChunks).Error; err != nil {
			return err
		}
		if err := tx.Where("source_id = ?", id).Delete(&models.KnowledgeDocument{}).Error; err != nil {
			return err
		}
		nextVersion = source.Version + 1
		result := tx.Model(&models.KnowledgeSource{}).Where("id = ? AND version = ?", id, source.Version).Updates(map[string]any{
			"original_content": nil, "status": "deleted", "deleted_at": now, "delete_reason": reason,
			"last_indexed_at": nil, "version": nextVersion, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, nextVersion)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id, "status": "deleted", "version": nextVersion, "deleted_chunks": deletedChunks}})
}

func (a *API) reindexKnowledgeSource(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_SOURCE_ID")
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	a.queueKnowledgeReindex(c, id, expectedVersion, nil)
}

func (a *API) queueKnowledgeReindex(c *gin.Context, sourceID string, expectedVersion int64, retryOf *models.KnowledgeIndexJob) {
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
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
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var source models.KnowledgeSource
		if err := tx.First(&source, "id = ? AND deleted_at IS NULL", sourceID).Error; err != nil {
			return err
		}
		if source.Version != expectedVersion {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
		}
		if source.Status == "indexing" {
			return newProjectRequestError(http.StatusConflict, "KNOWLEDGE_INDEX_IN_PROGRESS", "The knowledge source is already being indexed")
		}
		if len(source.OriginalContent) == 0 {
			return newProjectRequestError(http.StatusConflict, "KNOWLEDGE_SOURCE_UNAVAILABLE", "The managed source copy is unavailable")
		}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		result := tx.Model(&models.KnowledgeSource{}).
			Where("id = ? AND version = ? AND status <> 'indexing'", sourceID, expectedVersion).
			Updates(map[string]any{"status": "indexing", "version": expectedVersion + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "KNOWLEDGE_SOURCE_NOT_FOUND", "Knowledge source not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	row, err := loadKnowledgeSourceRow(a.db.WithContext(c.Request.Context()), sourceID, false)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, row.Version)
	c.JSON(http.StatusAccepted, gin.H{"data": knowledgeImportResponse{Source: knowledgeSourceResponseFromRow(row), Job: job}})
	if !a.knowledgeIndexer.enqueue(context.Background(), job.ID) {
		a.failKnowledgeIndexJob(job.ID, "KNOWLEDGE_INDEX_QUEUE_UNAVAILABLE")
	}
}

func (a *API) listKnowledgeDocuments(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	base := a.db.WithContext(c.Request.Context()).Table("knowledge_documents AS d").
		Joins("JOIN knowledge_sources s ON s.id = d.source_id AND s.deleted_at IS NULL")
	var total int64
	if err := base.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	var rows []knowledgeDocumentRow
	if err := base.Select(`
		d.*, s.name AS source_name, s.source_type AS source_type,
		COALESCE((SELECT COUNT(*) FROM knowledge_chunks c WHERE c.document_id = d.id), 0) AS chunk_count
	`).Order("d.updated_at DESC").Order("d.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	items := make([]knowledgeDocumentResponse, len(rows))
	for index := range rows {
		items[index] = knowledgeDocumentResponseFromRow(rows[index], false)
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": gin.H{"page": page, "page_size": pageSize, "total": total}})
}

func (a *API) getKnowledgeDocument(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_DOCUMENT_ID")
	if !ok {
		return
	}
	row, err := loadKnowledgeDocumentRow(a.db.WithContext(c.Request.Context()), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "KNOWLEDGE_DOCUMENT_NOT_FOUND", "Knowledge document not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": knowledgeDocumentResponseFromRow(row, true)})
}

func (a *API) deleteKnowledgeDocument(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_DOCUMENT_ID")
	if !ok {
		return
	}
	if c.Query("confirm") != "true" {
		writeError(c, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED", "confirm=true is required to delete a knowledge document")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	var sourceID string
	var nextVersion int64
	var deletedChunks int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var document models.KnowledgeDocument
		if err := tx.First(&document, "id = ?", id).Error; err != nil {
			return err
		}
		sourceID = document.SourceID
		var source models.KnowledgeSource
		if err := tx.First(&source, "id = ? AND deleted_at IS NULL", sourceID).Error; err != nil {
			return err
		}
		if source.Version != expectedVersion {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Knowledge source has changed; reload it before retrying")
		}
		if err := tx.Model(&models.KnowledgeChunk{}).Where("document_id = ?", id).Count(&deletedChunks).Error; err != nil {
			return err
		}
		if err := tx.Delete(&models.KnowledgeDocument{}, "id = ?", id).Error; err != nil {
			return err
		}
		nextVersion = source.Version + 1
		return tx.Model(&models.KnowledgeSource{}).Where("id = ? AND version = ?", sourceID, source.Version).Updates(map[string]any{
			"status": "pending", "last_indexed_at": nil, "version": nextVersion, "updated_at": now,
		}).Error
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "KNOWLEDGE_DOCUMENT_NOT_FOUND", "Knowledge document not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, nextVersion)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id, "source_id": sourceID, "source_version": nextVersion, "deleted_chunks": deletedChunks}})
}

func (a *API) searchKnowledge(c *gin.Context) {
	var input knowledgeSearchRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || utf8.RuneCountInString(query) > maxKnowledgeQueryRunes {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "query must contain 1 to 256 characters")
		return
	}
	terms := knowledgeSearchTerms(query)
	if len(terms) == 0 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "query must contain searchable letters or numbers")
		return
	}
	if len(input.SourceIDs) > 20 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "source_ids may contain at most 20 items")
		return
	}
	seen := make(map[string]struct{}, len(input.SourceIDs))
	for _, sourceID := range input.SourceIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(sourceID))
		if err != nil || parsed.String() != strings.TrimSpace(sourceID) {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "source_ids must contain canonical UUIDs")
			return
		}
		seen[sourceID] = struct{}{}
	}
	sourceIDs := make([]string, 0, len(seen))
	for sourceID := range seen {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "limit must be between 1 and 50")
		return
	}

	dbQuery := a.db.WithContext(c.Request.Context()).Table("knowledge_chunks_fts").Select(`
		knowledge_chunks_fts.chunk_id AS chunk_id,
		knowledge_chunks_fts.document_id AS document_id,
		knowledge_chunks_fts.source_id AS source_id,
		s.name AS source_name, s.source_type AS source_type,
		d.title AS document_title, d.version AS document_version,
		c.chunk_index AS chunk_index, c.start_char AS start_char, c.end_char AS end_char,
		c.start_line AS start_line, c.end_line AS end_line,
		c.start_page AS start_page, c.end_page AS end_page, c.content AS content,
		bm25(knowledge_chunks_fts) AS rank
	`).Joins("JOIN knowledge_chunks c ON c.id = knowledge_chunks_fts.chunk_id").
		Joins("JOIN knowledge_documents d ON d.id = c.document_id AND d.status = 'ready'").
		Joins("JOIN knowledge_sources s ON s.id = c.source_id AND s.status IN ('ready', 'indexing') AND s.deleted_at IS NULL").
		Where("knowledge_chunks_fts MATCH ?", knowledgeFTSQuery(terms))
	if len(sourceIDs) > 0 {
		dbQuery = dbQuery.Where("knowledge_chunks_fts.source_id IN ?", sourceIDs)
	}
	var rows []knowledgeSearchRow
	if err := dbQuery.Order("rank ASC").Order("s.name ASC").Order("c.chunk_index ASC").Limit(limit).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	results := make([]knowledgeSearchResult, len(rows))
	for index := range rows {
		excerpt, highlights := knowledgeExcerpt(rows[index].Content, terms)
		results[index] = knowledgeSearchResult{
			ChunkID: rows[index].ChunkID, DocumentID: rows[index].DocumentID, SourceID: rows[index].SourceID,
			SourceName: rows[index].SourceName, SourceType: rows[index].SourceType,
			DocumentTitle: rows[index].DocumentTitle, DocumentVersion: rows[index].DocumentVersion,
			ChunkIndex: rows[index].ChunkIndex, StartChar: rows[index].StartChar, EndChar: rows[index].EndChar,
			StartLine: rows[index].StartLine, EndLine: rows[index].EndLine,
			StartPage: rows[index].StartPage, EndPage: rows[index].EndPage,
			Excerpt: excerpt, Highlights: highlights, Rank: rows[index].Rank,
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": results, "meta": gin.H{
		"query": query, "result_count": len(results), "source_ids": sourceIDs,
	}})
}

func (a *API) getKnowledgeIndexJob(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_JOB_ID")
	if !ok {
		return
	}
	var job models.KnowledgeIndexJob
	if err := a.db.WithContext(c.Request.Context()).First(&job, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "KNOWLEDGE_JOB_NOT_FOUND", "Knowledge index job not found")
		return
	} else if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

func (a *API) cancelKnowledgeIndexJob(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_JOB_ID")
	if !ok {
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	var job models.KnowledgeIndexJob
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&job, "id = ?", id).Error; err != nil {
			return err
		}
		if job.Status != "queued" && job.Status != "running" {
			return newProjectRequestError(http.StatusConflict, "KNOWLEDGE_JOB_TERMINAL", "Completed index jobs cannot be cancelled")
		}
		if err := tx.Model(&models.KnowledgeIndexJob{}).Where("id = ?", id).Updates(map[string]any{
			"status": "cancelled", "stage": "complete", "progress": job.Progress,
			"cancel_requested": true, "completed_at": now,
		}).Error; err != nil {
			return err
		}
		var documentCount int64
		if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", job.SourceID).Count(&documentCount).Error; err != nil {
			return err
		}
		sourceStatus := "failed"
		if documentCount > 0 {
			sourceStatus = "ready"
		}
		if err := tx.Model(&models.KnowledgeSource{}).
			Where("id = ? AND status = 'indexing' AND deleted_at IS NULL", job.SourceID).
			Updates(map[string]any{"status": sourceStatus, "version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
			return err
		}
		job.Status, job.Stage, job.CancelRequested, job.CompletedAt = "cancelled", "complete", true, &now
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "KNOWLEDGE_JOB_NOT_FOUND", "Knowledge index job not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

func (a *API) retryKnowledgeIndexJob(c *gin.Context) {
	id, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_JOB_ID")
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var job models.KnowledgeIndexJob
	if err := a.db.WithContext(c.Request.Context()).First(&job, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "KNOWLEDGE_JOB_NOT_FOUND", "Knowledge index job not found")
		return
	} else if err != nil {
		writeDatabaseError(c)
		return
	}
	if job.Status != "failed" && job.Status != "cancelled" {
		writeError(c, http.StatusConflict, "KNOWLEDGE_JOB_NOT_RETRYABLE", "Only failed or cancelled index jobs can be retried")
		return
	}
	a.queueKnowledgeReindex(c, job.SourceID, expectedVersion, &job)
}

func (a *API) purgeKnowledgeBase(c *gin.Context) {
	if c.Query("confirm") != "true" || c.GetHeader("X-Knowledge-Confirmation") != "DELETE KNOWLEDGE BASE" {
		writeError(c, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED", "confirm=true and X-Knowledge-Confirmation are required to clear the knowledge base")
		return
	}
	var sources, documents, chunks, jobs int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for table, destination := range map[string]*int64{
			"knowledge_sources": &sources, "knowledge_documents": &documents,
			"knowledge_chunks": &chunks, "knowledge_index_jobs": &jobs,
		} {
			if err := tx.Table(table).Count(destination).Error; err != nil {
				return err
			}
		}
		return tx.Exec("DELETE FROM knowledge_sources").Error
	})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"deleted_sources": sources, "deleted_documents": documents,
		"deleted_chunks": chunks, "deleted_jobs": jobs,
	}})
}

func readKnowledgeUpload(c *gin.Context) (knowledgeUpload, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return knowledgeUpload{}, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be multipart/form-data")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxKnowledgeRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return knowledgeUpload{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	var result knowledgeUpload
	fileSeen := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return knowledgeUpload{}, mapKnowledgeUploadReadError(nextErr)
		}
		field := part.FormName()
		switch field {
		case "file":
			if fileSeen || part.FileName() == "" {
				_ = part.Close()
				return knowledgeUpload{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one file part is required")
			}
			fileSeen = true
			result.Name = cleanKnowledgeFilename(part.FileName())
			content, readErr := io.ReadAll(io.LimitReader(part, maxKnowledgeSourceBytes+1))
			closeErr := part.Close()
			if readErr != nil || closeErr != nil {
				return knowledgeUpload{}, mapKnowledgeUploadReadError(errors.Join(readErr, closeErr))
			}
			if int64(len(content)) > maxKnowledgeSourceBytes {
				return knowledgeUpload{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "KNOWLEDGE_SOURCE_TOO_LARGE", "Knowledge sources must not exceed 16 MiB")
			}
			result.Bytes = content
		case "title":
			value, readErr := io.ReadAll(io.LimitReader(part, 1025))
			closeErr := part.Close()
			if readErr != nil || closeErr != nil {
				return knowledgeUpload{}, mapKnowledgeUploadReadError(errors.Join(readErr, closeErr))
			}
			result.Title = strings.TrimSpace(string(value))
		default:
			_ = part.Close()
			return knowledgeUpload{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Only file and title parts are accepted")
		}
	}
	if !fileSeen {
		return knowledgeUpload{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one file part is required")
	}
	if result.Name == "" || result.Name == "." {
		return knowledgeUpload{}, newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_INVALID_FILENAME", "The selected file name is not valid")
	}
	if len(result.Bytes) == 0 {
		return knowledgeUpload{}, newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_EMPTY_SOURCE", "The selected file is empty")
	}
	result.SourceType, result.MimeType, err = knowledgeFileType(result.Name)
	if err != nil {
		return knowledgeUpload{}, err
	}
	if result.SourceType == "pdf" {
		if err = validateKnowledgePDFUpload(result.Bytes); err != nil {
			return knowledgeUpload{}, err
		}
	} else {
		if _, err = extractKnowledgeText(result.Name, result.Bytes); err != nil {
			return knowledgeUpload{}, err
		}
	}
	if result.Title == "" {
		result.Title = result.Name
	}
	if utf8.RuneCountInString(result.Title) > 255 {
		return knowledgeUpload{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "title must be at most 255 characters")
	}
	return result, nil
}

func mapKnowledgeUploadReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "KNOWLEDGE_SOURCE_TOO_LARGE", "Knowledge sources must not exceed 16 MiB")
	}
	return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
}

func writeKnowledgeRequestError(c *gin.Context, err error) {
	if writeProjectRequestError(c, err) {
		return
	}
	writeError(c, http.StatusInternalServerError, "KNOWLEDGE_IMPORT_FAILED", "The knowledge source could not be imported")
}

func cleanKnowledgeFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	return strings.TrimSpace(filepath.Base(value))
}

func knowledgeFileType(name string) (string, string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt":
		return "text", "text/plain", nil
	case ".md", ".markdown":
		return "markdown", "text/markdown", nil
	case ".pdf":
		return "pdf", "application/pdf", nil
	default:
		return "", "", newProjectRequestError(http.StatusUnsupportedMediaType, "KNOWLEDGE_FORMAT_UNSUPPORTED", "Only .txt, .md, .markdown, and .pdf files are supported")
	}
}

func extractKnowledgeText(name string, content []byte) (string, error) {
	sourceType, _, err := knowledgeFileType(name)
	if err != nil {
		return "", err
	}
	if sourceType == "pdf" {
		return "", newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_INVALID", "PDF sources are extracted by the indexing Actor")
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return "", newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_TEXT_INVALID", "The selected file must contain valid UTF-8 text without null bytes")
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return "", newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_EMPTY_SOURCE", "The selected file does not contain indexable text")
	}
	return text, nil
}

func validateKnowledgePDFUpload(content []byte) error {
	if !bytes.HasPrefix(content, []byte("%PDF-")) {
		return newProjectRequestError(http.StatusUnprocessableEntity, "KNOWLEDGE_PDF_INVALID", "The selected file is not a readable PDF document")
	}
	return nil
}

// extractKnowledgeSource runs the upload-appropriate extraction for an indexed
// source. Text sources return nil pages; every chunk of such a source stays on
// page 1.
func extractKnowledgeSource(source models.KnowledgeSource) (string, []knowledgePDFPageLocation, error) {
	if source.SourceType == "pdf" {
		extraction, err := extractKnowledgePDF(source.OriginalContent)
		if err != nil {
			return "", nil, err
		}
		return extraction.Text, extraction.Pages, nil
	}
	text, err := extractKnowledgeText(source.Name, source.OriginalContent)
	if err != nil {
		return "", nil, err
	}
	return text, nil, nil
}

// knowledgePagesForLines maps a chunk line range onto PDF page numbers. Pages
// without extractable text contribute no lines, so contributing pages cover
// the line sequence without gaps; ranges beyond the last page clamp to it.
func knowledgePagesForLines(pages []knowledgePDFPageLocation, startLine, endLine int) (int, int) {
	if len(pages) == 0 {
		return 1, 1
	}
	startPage, endPage := pages[len(pages)-1].Number, pages[len(pages)-1].Number
	for index := range pages {
		if pages[index].EndLine >= startLine {
			startPage = pages[index].Number
			break
		}
	}
	for index := range pages {
		if pages[index].EndLine >= endLine {
			endPage = pages[index].Number
			break
		}
	}
	return startPage, endPage
}

func splitKnowledgeText(text string) []knowledgeChunkDraft {
	runes := []rune(text)
	chunks := make([]knowledgeChunkDraft, 0, len(runes)/knowledgeChunkRunes+1)
	for start := 0; start < len(runes); {
		end := start + knowledgeChunkRunes
		if end > len(runes) {
			end = len(runes)
		} else {
			minimum := start + knowledgeChunkRunes/2
			for cursor := end; cursor > minimum; cursor-- {
				if runes[cursor-1] == '\n' {
					end = cursor
					break
				}
			}
		}
		segment := string(runes[start:end])
		if strings.TrimSpace(segment) != "" {
			startLine := 1 + strings.Count(string(runes[:start]), "\n")
			chunks = append(chunks, knowledgeChunkDraft{
				Content: segment, StartChar: start, EndChar: end,
				StartLine: startLine, EndLine: startLine + strings.Count(segment, "\n"),
				StartPage: 1, EndPage: 1,
			})
		}
		if end == len(runes) {
			break
		}
		next := end - knowledgeChunkOverlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func insertKnowledgeChunks(tx *gorm.DB, sourceID, documentID string, indexVersion int64, drafts []knowledgeChunkDraft, now string) error {
	rows := make([]models.KnowledgeChunk, len(drafts))
	for index := range drafts {
		rows[index] = models.KnowledgeChunk{
			ID: uuid.NewString(), DocumentID: documentID, SourceID: sourceID, ChunkIndex: index,
			StartChar: drafts[index].StartChar, EndChar: drafts[index].EndChar,
			StartLine: drafts[index].StartLine, EndLine: drafts[index].EndLine,
			StartPage: drafts[index].StartPage, EndPage: drafts[index].EndPage,
			Content: drafts[index].Content, SearchText: knowledgeSearchText(drafts[index].Content),
			ContentSHA256: knowledgeSHA256([]byte(drafts[index].Content)),
			IndexVersion:  indexVersion, CreatedAt: now,
		}
	}
	return tx.CreateInBatches(&rows, 100).Error
}

func knowledgeSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func detectKnowledgeLanguage(text string) string {
	for _, value := range text {
		if unicode.Is(unicode.Han, value) {
			return "zh"
		}
	}
	for _, value := range text {
		if unicode.IsLetter(value) {
			return "en"
		}
	}
	return "und"
}

func knowledgeSearchTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(value rune) bool {
		return !unicode.IsLetter(value) && !unicode.IsNumber(value) && value != '_'
	})
	terms := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
		if len(terms) == 16 {
			break
		}
	}
	return terms
}

func knowledgeFTSQuery(terms []string) string {
	indexedTerms := make([]string, 0, len(terms)*2)
	for _, term := range terms {
		runes := []rune(term)
		if allKnowledgeHan(runes) && len(runes) > 1 {
			for index := 0; index+1 < len(runes); index++ {
				indexedTerms = append(indexedTerms, string(runes[index:index+2]))
			}
			continue
		}
		indexedTerms = append(indexedTerms, term)
	}
	quoted := make([]string, len(indexedTerms))
	for index, term := range indexedTerms {
		quoted[index] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " AND ")
}

func knowledgeSearchText(content string) string {
	tokens := make([]string, 0, utf8.RuneCountInString(content)*2)
	var hanRun []rune
	flushHan := func() {
		if len(hanRun) == 0 {
			return
		}
		for _, value := range hanRun {
			tokens = append(tokens, string(value))
		}
		for index := 0; index+1 < len(hanRun); index++ {
			tokens = append(tokens, string(hanRun[index:index+2]))
		}
		hanRun = hanRun[:0]
	}
	for _, value := range content {
		if unicode.Is(unicode.Han, value) {
			hanRun = append(hanRun, value)
			continue
		}
		flushHan()
	}
	flushHan()
	if len(tokens) == 0 {
		return content
	}
	return content + "\n" + strings.Join(tokens, " ")
}

func allKnowledgeHan(values []rune) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !unicode.Is(unicode.Han, value) {
			return false
		}
	}
	return true
}

func knowledgeExcerpt(content string, terms []string) (string, []knowledgeHighlight) {
	runes := []rune(content)
	lowerRunes := []rune(strings.ToLower(content))
	matchStart, matchEnd := -1, -1
	for _, term := range terms {
		termRunes := []rune(strings.ToLower(term))
		if start := runeSliceIndex(lowerRunes, termRunes, 0); start >= 0 && (matchStart < 0 || start < matchStart) {
			matchStart, matchEnd = start, start+len(termRunes)
		}
	}
	if matchStart < 0 {
		end := len(runes)
		if end > 320 {
			end = 320
		}
		return string(runes[:end]), []knowledgeHighlight{}
	}
	windowStart := matchStart - 120
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := matchEnd + 220
	if windowEnd > len(runes) {
		windowEnd = len(runes)
	}
	excerptRunes := runes[windowStart:windowEnd]
	lowerExcerpt := lowerRunes[windowStart:windowEnd]
	highlights := make([]knowledgeHighlight, 0, len(terms))
	for _, term := range terms {
		termRunes := []rune(strings.ToLower(term))
		for cursor := 0; cursor < len(lowerExcerpt); {
			start := runeSliceIndex(lowerExcerpt, termRunes, cursor)
			if start < 0 {
				break
			}
			highlights = append(highlights, knowledgeHighlight{Start: start, End: start + len(termRunes)})
			cursor = start + len(termRunes)
		}
	}
	sort.Slice(highlights, func(i, j int) bool {
		if highlights[i].Start == highlights[j].Start {
			return highlights[i].End < highlights[j].End
		}
		return highlights[i].Start < highlights[j].Start
	})
	merged := make([]knowledgeHighlight, 0, len(highlights))
	for _, item := range highlights {
		if len(merged) > 0 && item.Start <= merged[len(merged)-1].End {
			if item.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = item.End
			}
			continue
		}
		merged = append(merged, item)
	}
	return string(excerptRunes), merged
}

func runeSliceIndex(haystack, needle []rune, from int) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for start := from; start+len(needle) <= len(haystack); start++ {
		matched := true
		for index := range needle {
			if haystack[start+index] != needle[index] {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

func loadKnowledgeSourceRow(db *gorm.DB, id string, includeDeleted bool) (knowledgeSourceRow, error) {
	query := db.Table("knowledge_sources AS s").Select(`
		s.*, d.id AS document_id, d.version AS document_version,
		COALESCE((SELECT COUNT(*) FROM knowledge_chunks c WHERE c.source_id = s.id), 0) AS chunk_count,
		j.id AS latest_job_id, j.operation AS latest_job_operation, j.status AS latest_job_status,
		j.stage AS latest_job_stage, j.progress AS latest_job_progress, j.attempt AS latest_job_attempt,
		j.retry_of_job_id AS latest_job_retry_of_id, j.error_code AS latest_job_error_code,
		j.cancel_requested AS latest_job_cancel_requested, j.started_at AS latest_job_started_at,
		j.completed_at AS latest_job_completed_at, j.created_at AS latest_job_created_at
	`).Joins("LEFT JOIN knowledge_documents d ON d.source_id = s.id").
		Joins(latestKnowledgeJobJoin).Where("s.id = ?", id)
	if !includeDeleted {
		query = query.Where("s.deleted_at IS NULL")
	}
	var row knowledgeSourceRow
	err := query.Take(&row).Error
	return row, err
}

func loadKnowledgeDocumentRow(db *gorm.DB, id string) (knowledgeDocumentRow, error) {
	var row knowledgeDocumentRow
	err := db.Table("knowledge_documents AS d").Select(`
		d.*, s.name AS source_name, s.source_type AS source_type,
		COALESCE((SELECT COUNT(*) FROM knowledge_chunks c WHERE c.document_id = d.id), 0) AS chunk_count
	`).Joins("JOIN knowledge_sources s ON s.id = d.source_id AND s.deleted_at IS NULL").Where("d.id = ?", id).Take(&row).Error
	return row, err
}

func knowledgeSourceResponseFromRow(row knowledgeSourceRow) knowledgeSourceResponse {
	response := knowledgeSourceResponse{
		ID: row.ID, Name: row.Name, Title: row.Title, SourceType: row.SourceType, ImportMode: row.ImportMode,
		MimeType: row.MimeType, SizeBytes: row.SizeBytes, ContentSHA256: row.ContentSHA256,
		Status: row.Status, LastIndexedAt: row.LastIndexedAt, DeletedAt: row.DeletedAt,
		DeleteReason: row.DeleteReason, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		DocumentID: row.DocumentID, DocumentVersion: row.DocumentVersion, ChunkCount: row.ChunkCount,
	}
	if row.LatestJobID != nil {
		response.LatestJob = &models.KnowledgeIndexJob{
			ID: *row.LatestJobID, SourceID: row.ID,
			Operation: stringPointerValue(row.LatestJobOperation), Status: stringPointerValue(row.LatestJobStatus),
			Stage: stringPointerValue(row.LatestJobStage), Progress: intPointerValue(row.LatestJobProgress),
			Attempt: intPointerValue(row.LatestJobAttempt), RetryOfJobID: row.LatestJobRetryOfID,
			ErrorCode: row.LatestJobErrorCode, CancelRequested: boolPointerValue(row.LatestJobCancelRequested),
			StartedAt: row.LatestJobStartedAt, CompletedAt: row.LatestJobCompletedAt,
			CreatedAt: stringPointerValue(row.LatestJobCreatedAt),
		}
	}
	return response
}

func knowledgeDocumentResponseFromRow(row knowledgeDocumentRow, includeContent bool) knowledgeDocumentResponse {
	response := knowledgeDocumentResponse{
		ID: row.ID, SourceID: row.SourceID, SourceName: row.SourceName, SourceType: row.SourceType,
		Title: row.Title, Language: row.Language, ExtractorVersion: row.ExtractorVersion,
		ContentSHA256: row.ContentSHA256, Status: row.Status, Version: row.Version,
		ChunkCount: row.ChunkCount, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if includeContent {
		response.ContentText = row.ContentText
	}
	return response
}

func knowledgeID(c *gin.Context, parameter, code string) (string, bool) {
	value := strings.TrimSpace(c.Param(parameter))
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		writeError(c, http.StatusBadRequest, code, "Knowledge id must be a canonical UUID")
		return "", false
	}
	return value, true
}

func escapeKnowledgeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

const latestKnowledgeJobJoin = `
	LEFT JOIN knowledge_index_jobs j ON j.id = (
		SELECT candidate.id
		FROM knowledge_index_jobs candidate
		WHERE candidate.source_id = s.id
		ORDER BY candidate.created_at DESC, candidate.id DESC
		LIMIT 1
	)
`

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func int64PointerString(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func boolPointerValue(value *bool) bool {
	return value != nil && *value
}
