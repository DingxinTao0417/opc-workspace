package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Knowledge-base management for the AI track. Reading source content still
// requires the separate `knowledge` scope with explicit versioned sources; the
// `knowledge_actions` scope below only exposes source/index-job metadata and
// human-confirmed library commands. It never reads or sends document text.
const (
	aiKnowledgeCreateAction   = "knowledge_source.create"
	aiKnowledgeReindexAction  = "knowledge_source.reindex"
	aiKnowledgeDeleteAction   = "knowledge_source.delete"
	aiKnowledgeJobRetryAction = "knowledge_index_job.retry"
	aiKnowledgeJobCancelAct   = "knowledge_index_job.cancel"
)

func isAIKnowledgeAction(action string) bool {
	switch action {
	case aiKnowledgeCreateAction, aiKnowledgeReindexAction, aiKnowledgeDeleteAction, aiKnowledgeJobRetryAction, aiKnowledgeJobCancelAct:
		return true
	default:
		return false
	}
}

func isAIKnowledgeJobAction(action string) bool {
	return action == aiKnowledgeJobRetryAction || action == aiKnowledgeJobCancelAct
}

// aiKnowledgeSourceCreate is the only knowledge action that writes new
// content; it accepts text only, so no file bytes or paths ever enter the
// approval payload.
type aiKnowledgeSourceCreate struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

const aiKnowledgeExcerptRunes = 800

func prepareAIKnowledgeSource(create aiKnowledgeSourceCreate) (knowledgeUpload, error) {
	upload := knowledgeUpload{
		Name:  cleanKnowledgeFilename(create.Name),
		Title: strings.TrimSpace(create.Title),
		Bytes: []byte(create.Content),
	}
	if upload.Name == "" {
		return upload, errors.New("source creation requires a file-like name such as notes.md")
	}
	if len(upload.Bytes) == 0 {
		return upload, errors.New("source creation requires non-empty content")
	}
	if len(upload.Bytes) > maxAIKnowledgeSourceBytes {
		return upload, fmt.Errorf("content must be at most %d bytes", maxAIKnowledgeSourceBytes)
	}
	return finalizeKnowledgeUpload(upload)
}

func aiKnowledgeSourceExcerpt(content string) (string, bool) {
	runes := []rune(content)
	if len(runes) <= aiKnowledgeExcerptRunes {
		return content, false
	}
	return string(runes[:aiKnowledgeExcerptRunes]), true
}

type aiKnowledgeJobPreview struct {
	ID        string  `json:"id"`
	Operation string  `json:"operation"`
	Status    string  `json:"status"`
	Attempt   int     `json:"attempt"`
	ErrorCode *string `json:"error_code,omitempty"`
}

type aiKnowledgeSourcePreview struct {
	SourceID         string                 `json:"source_id"`
	KnowledgeJobID   string                 `json:"knowledge_index_job_id,omitempty"`
	Name             string                 `json:"name"`
	SourceType       string                 `json:"source_type"`
	Status           string                 `json:"status"`
	Version          int64                  `json:"version"`
	NextStatus       string                 `json:"next_status,omitempty"`
	NextVersion      int64                  `json:"next_version,omitempty"`
	ChunkCount       int64                  `json:"chunk_count"`
	DocumentCount    int64                  `json:"document_count"`
	DocumentVersion  *int64                 `json:"document_version,omitempty"`
	NewJobOperation  string                 `json:"new_job_operation,omitempty"`
	Job              *aiKnowledgeJobPreview `json:"job,omitempty"`
	ProposedBytes    int64                  `json:"proposed_content_bytes,omitempty"`
	ProposedSHA256   string                 `json:"proposed_content_sha256,omitempty"`
	ProposedExcerpt  string                 `json:"proposed_excerpt,omitempty"`
	ProposedShort    bool                   `json:"proposed_excerpt_truncated,omitempty"`
	ContentIncluded  bool                   `json:"content_included"`
	LeavesDeviceNote string                 `json:"note,omitempty"`
}

func parseAIKnowledgeSourceAction(input aiWorkspaceAction, fields map[string]json.RawMessage, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" || input.InboxItemID != "" || input.ReminderID != "" ||
		input.FocusSessionID != "" || input.ClientFollowupID != "" || input.ClientActivityID != "" ||
		input.AutomationRuleID != "" || input.AutomationRunID != "" || input.RoadmapMilestoneID != "" ||
		input.ContentItemID != "" || input.ClientID != "" || input.ClientActorLinkID != "" || input.ActorID != "" ||
		input.TagID != "" || input.ProjectNoteID != "" || input.InvoiceID != "" || input.FinancialEntryID != "" ||
		input.AgentRunID != "" {
		return input, errors.New("knowledge actions only accept knowledge_source_id or knowledge_index_job_id")
	}
	if isAIKnowledgeJobAction(input.Action) {
		if !isValidKnowledgeID(input.KnowledgeIndexJobID) || input.ExpectedVersion < 1 {
			return input, errors.New("use the canonical knowledge_index_job_id and the current source expected_version")
		}
		if input.KnowledgeSourceID != "" {
			return input, errors.New("index job actions use knowledge_index_job_id, not knowledge_source_id")
		}
		if len(fields) != 0 {
			return input, errors.New("index job actions require empty changes")
		}
		input.Changes = json.RawMessage(`{}`)
		return input, nil
	}
	if input.Action == aiKnowledgeCreateAction {
		if input.KnowledgeSourceID != "" || input.KnowledgeIndexJobID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("source creation does not accept a target id or version")
		}
		for field, value := range fields {
			if (field != "name" && field != "title" && field != "content") || string(value) == "null" {
				return input, errors.New("source creation only accepts non-null name, title and content")
			}
		}
		var create aiKnowledgeSourceCreate
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		upload, err := prepareAIKnowledgeSource(create)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(map[string]any{"name": upload.Name, "title": upload.Title, "content": create.Content})
		return input, nil
	}
	if !isValidKnowledgeID(input.KnowledgeSourceID) || input.ExpectedVersion < 1 {
		return input, errors.New("use the canonical knowledge_source_id and expected_version from knowledge_library")
	}
	if _, present := raw["knowledge_index_job_id"]; present {
		return input, errors.New("source actions use knowledge_source_id, not knowledge_index_job_id")
	}
	if input.Action == aiKnowledgeReindexAction {
		if len(fields) != 0 {
			return input, errors.New("reindex requires empty changes")
		}
		input.Changes = json.RawMessage(`{}`)
		return input, nil
	}
	for field := range fields {
		if field != "reason" {
			return input, errors.New("source deletion only accepts an optional reason")
		}
	}
	reason := ""
	if rawReason, present := fields["reason"]; present {
		if json.Unmarshal(rawReason, &reason) != nil {
			return input, errors.New("reason must be text")
		}
	}
	normalized, err := normalizeKnowledgeDeleteReason(reason)
	if err != nil {
		return input, err
	}
	input.Changes, _ = json.Marshal(map[string]any{"reason": normalized})
	return input, nil
}

func aiKnowledgeDeleteReason(input aiWorkspaceAction) string {
	var changes struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(input.Changes, &changes)
	return changes.Reason
}

func loadAIKnowledgeSource(tx *gorm.DB, sourceID string, expectedVersion int64) (models.KnowledgeSource, error) {
	var source models.KnowledgeSource
	if err := tx.Select("id", "name", "source_type", "status", "version", "deleted_at", "original_content", "size_bytes").
		First(&source, "id = ?", sourceID).Error; err != nil {
		return source, knowledgeSourceNotFound(err)
	}
	if source.DeletedAt != nil {
		return source, newProjectRequestError(409, "KNOWLEDGE_SOURCE_DELETED", "This knowledge source was deleted; the stored copy and index are gone")
	}
	if source.Version != expectedVersion {
		return source, newProjectRequestError(409, "VERSION_CONFLICT", "Knowledge source has changed; read it again before proposing")
	}
	return source, nil
}

func loadAIKnowledgeSourceFacts(tx *gorm.DB, sourceID string) (int64, int64, *int64, error) {
	var chunks int64
	if err := tx.Model(&models.KnowledgeChunk{}).Where("source_id = ?", sourceID).Count(&chunks).Error; err != nil {
		return 0, 0, nil, err
	}
	var documents int64
	if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", sourceID).Count(&documents).Error; err != nil {
		return 0, 0, nil, err
	}
	var document struct {
		Version int64 `gorm:"column:version"`
	}
	err := tx.Model(&models.KnowledgeDocument{}).Select("version").Where("source_id = ?", sourceID).
		Order("version DESC").Limit(1).Take(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return chunks, documents, nil, nil
	}
	if err != nil {
		return 0, 0, nil, err
	}
	version := document.Version
	return chunks, documents, &version, nil
}

// Preview freezes only facts that stay stable while a human reads the card.
// Volatile indexer fields (stage/progress) are deliberately excluded so a
// running job can still be cancelled or inspected without a false conflict.
func previewAIKnowledgeSourceAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	knowledge := aiKnowledgeSourcePreview{ContentIncluded: false}
	if input.Action == aiKnowledgeCreateAction {
		var create aiKnowledgeSourceCreate
		_ = json.Unmarshal(input.Changes, &create)
		upload, err := prepareAIKnowledgeSource(create)
		if err != nil {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
		}
		excerpt, truncated := aiKnowledgeSourceExcerpt(create.Content)
		knowledge = aiKnowledgeSourcePreview{
			Name: upload.Name, SourceType: upload.SourceType, Status: "pending",
			NextStatus: "indexing", NextVersion: 1, NewJobOperation: "import",
			ProposedBytes: int64(len(upload.Bytes)), ProposedSHA256: knowledgeSHA256(upload.Bytes),
			ProposedExcerpt: excerpt, ProposedShort: truncated,
		}
		preview.Label = upload.Name
		preview.After["name"], preview.After["source_type"] = upload.Name, upload.SourceType
		preview.After["status"], preview.After["version"] = "indexing", 1
		preview.After["size_bytes"] = len(upload.Bytes)
		preview.Knowledge = &knowledge
		return preview, nil
	}
	if isAIKnowledgeJobAction(input.Action) {
		job, err := loadRetryableKnowledgeIndexJob(tx, input.KnowledgeIndexJobID)
		if input.Action == aiKnowledgeJobCancelAct {
			job, err = loadCancelableKnowledgeIndexJob(tx, input.KnowledgeIndexJobID)
		}
		if err != nil {
			return preview, err
		}
		source, err := loadAIKnowledgeSource(tx, job.SourceID, input.ExpectedVersion)
		if err != nil {
			return preview, err
		}
		chunks, documents, documentVersion, err := loadAIKnowledgeSourceFacts(tx, source.ID)
		if err != nil {
			return preview, err
		}
		knowledge = aiKnowledgeSourcePreview{
			SourceID: source.ID, KnowledgeJobID: job.ID, Name: source.Name, SourceType: source.SourceType,
			Status: source.Status, Version: source.Version, ChunkCount: chunks, DocumentCount: documents,
			DocumentVersion: documentVersion,
			Job:             &aiKnowledgeJobPreview{ID: job.ID, Operation: job.Operation, Status: job.Status, Attempt: job.Attempt, ErrorCode: safeAIKnowledgeErrorCode(job.ErrorCode)},
		}
		preview.Label = source.Name
		preview.Before["status"], preview.Before["version"] = source.Status, source.Version
		preview.Before["chunks"], preview.Before["documents"] = chunks, documents
		preview.Before["job_status"], preview.Before["job_attempt"] = job.Status, job.Attempt
		preview.After["chunks"], preview.After["documents"] = chunks, documents
		if input.Action == aiKnowledgeJobCancelAct {
			// Cancelling restores the source from durable documents, so freeze
			// that exact outcome instead of promising an ambiguous state.
			nextStatus := "failed"
			if documents > 0 {
				nextStatus = "ready"
			}
			knowledge.NextStatus = nextStatus
			if source.Status == "indexing" {
				knowledge.NextVersion = source.Version + 1
			}
			preview.After["job_status"] = "cancelled"
			preview.After["status"] = nextStatus
		} else {
			knowledge.NewJobOperation = job.Operation
			knowledge.NextStatus, knowledge.NextVersion = "indexing", source.Version+1
			preview.After["job_status"] = "queued"
			preview.After["status"], preview.After["version"] = "indexing", source.Version+1
		}
		preview.Knowledge = &knowledge
		return preview, nil
	}
	if input.Action == aiKnowledgeDeleteAction {
		deletion, err := knowledgeSourceDeletionPreview(tx, input.KnowledgeSourceID, input.ExpectedVersion)
		if err != nil {
			return preview, err
		}
		knowledge = aiKnowledgeSourcePreview{
			SourceID: deletion.SourceID, Name: deletion.Name, SourceType: deletion.SourceType,
			Status: deletion.Status, Version: deletion.Version,
			ChunkCount: deletion.ChunkCount, DocumentCount: deletion.DocumentCount,
			NextStatus: "deleted", NextVersion: deletion.NextVersion,
		}
		preview.Label = deletion.Name
		preview.Before["status"], preview.Before["version"] = deletion.Status, deletion.Version
		preview.Before["chunks"], preview.Before["documents"] = deletion.ChunkCount, deletion.DocumentCount
		preview.After["status"], preview.After["version"] = "deleted", deletion.NextVersion
		preview.After["chunks"], preview.After["documents"] = 0, 0
		preview.Knowledge = &knowledge
		return preview, nil
	}
	source, err := loadAIKnowledgeSource(tx, input.KnowledgeSourceID, input.ExpectedVersion)
	if err != nil {
		return preview, err
	}
	if source.Status == "indexing" {
		return preview, newProjectRequestError(409, "KNOWLEDGE_INDEX_IN_PROGRESS", "The knowledge source is already being indexed")
	}
	if len(source.OriginalContent) == 0 {
		return preview, newProjectRequestError(409, "KNOWLEDGE_SOURCE_UNAVAILABLE", "The managed source copy is unavailable")
	}
	chunks, documents, documentVersion, err := loadAIKnowledgeSourceFacts(tx, source.ID)
	if err != nil {
		return preview, err
	}
	knowledge = aiKnowledgeSourcePreview{
		SourceID: source.ID, Name: source.Name, SourceType: source.SourceType, Status: source.Status,
		Version: source.Version, NextStatus: "indexing", NextVersion: source.Version + 1,
		ChunkCount: chunks, DocumentCount: documents, DocumentVersion: documentVersion, NewJobOperation: "reindex",
	}
	preview.Label = source.Name
	preview.Before["status"], preview.Before["version"] = source.Status, source.Version
	preview.Before["chunks"], preview.Before["documents"] = chunks, documents
	preview.After["status"], preview.After["version"] = "indexing", source.Version+1
	preview.After["chunks"], preview.After["documents"] = chunks, documents
	preview.Knowledge = &knowledge
	return preview, nil
}

func loadCancelableKnowledgeIndexJob(tx *gorm.DB, jobID string) (models.KnowledgeIndexJob, error) {
	var job models.KnowledgeIndexJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return job, knowledgeJobNotFound(err)
	}
	if job.Status != "queued" && job.Status != "running" {
		return job, newProjectRequestError(409, "KNOWLEDGE_JOB_TERMINAL", "Completed index jobs cannot be cancelled")
	}
	return job, nil
}

func safeAIKnowledgeErrorCode(code *string) *string {
	if code == nil {
		return nil
	}
	value := strings.TrimSpace(*code)
	if value == "" || len(value) > 100 || strings.ContainsAny(value, "\n\r\t") {
		return nil
	}
	return &value
}

// executeAIKnowledgeSourceAction runs the shared domain command inside the
// approval transaction. The second return value is the new immutable index job
// identity that the caller enqueues only after the transaction commits.
func executeAIKnowledgeSourceAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, string, error) {
	switch input.Action {
	case aiKnowledgeCreateAction:
		var create aiKnowledgeSourceCreate
		_ = json.Unmarshal(input.Changes, &create)
		upload, err := prepareAIKnowledgeSource(create)
		if err != nil {
			return aiActionResult{}, "", err
		}
		source, job, err := createKnowledgeSourceInTransaction(tx, upload, uuid.NewString(), uuid.NewString(), now)
		if err != nil {
			return aiActionResult{}, "", err
		}
		return aiActionResult{ID: source.ID, Version: source.Version}, job.ID, nil
	case aiKnowledgeReindexAction:
		source, job, err := queueKnowledgeReindexInTransaction(tx, input.KnowledgeSourceID, input.ExpectedVersion, nil, now)
		if err != nil {
			return aiActionResult{}, "", knowledgeSourceNotFound(err)
		}
		return aiActionResult{ID: job.ID, Version: source.Version}, job.ID, nil
	case aiKnowledgeDeleteAction:
		deletion, err := deleteKnowledgeSourceInTransaction(tx, input.KnowledgeSourceID, input.ExpectedVersion, aiKnowledgeDeleteReason(input), now)
		if err != nil {
			return aiActionResult{}, "", knowledgeSourceNotFound(err)
		}
		return aiActionResult{ID: deletion.SourceID, Version: deletion.NextVersion}, "", nil
	case aiKnowledgeJobRetryAction:
		job, err := loadRetryableKnowledgeIndexJob(tx, input.KnowledgeIndexJobID)
		if err != nil {
			return aiActionResult{}, "", err
		}
		source, queued, err := queueKnowledgeReindexInTransaction(tx, job.SourceID, input.ExpectedVersion, &job, now)
		if err != nil {
			return aiActionResult{}, "", knowledgeSourceNotFound(err)
		}
		return aiActionResult{ID: queued.ID, Version: source.Version}, queued.ID, nil
	case aiKnowledgeJobCancelAct:
		job, err := cancelKnowledgeIndexJobInTransaction(tx, input.KnowledgeIndexJobID, now)
		if err != nil {
			return aiActionResult{}, "", knowledgeJobNotFound(err)
		}
		var sourceVersion int64
		if err := tx.Model(&models.KnowledgeSource{}).Where("id = ?", job.SourceID).Pluck("version", &sourceVersion).Error; err != nil {
			return aiActionResult{}, "", err
		}
		return aiActionResult{ID: job.ID, Version: sourceVersion}, "", nil
	default:
		return aiActionResult{}, "", errors.New("unsupported knowledge action")
	}
}

type aiKnowledgeLibraryItem struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Title           string                 `json:"title"`
	SourceType      string                 `json:"source_type"`
	MimeType        string                 `json:"mime_type"`
	Status          string                 `json:"status"`
	Version         int64                  `json:"version"`
	SizeBytes       int64                  `json:"size_bytes"`
	ChunkCount      int64                  `json:"chunk_count"`
	DocumentVersion *int64                 `json:"document_version,omitempty"`
	LastIndexedAt   *string                `json:"last_indexed_at,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
	Route           string                 `json:"route"`
	Job             *aiKnowledgeJobPreview `json:"latest_job,omitempty"`
}

func knowledgeLibraryRoute(sourceID, jobID string) string {
	if sourceID == "" || jobID == "" {
		return "/knowledge"
	}
	return "/knowledge?source=" + sourceID + "&job=" + jobID
}

// The native knowledge page highlights one source only when the linked job is
// that source's latest failed job. Every other state is opened through the
// library root so the model never claims a location the page cannot show.
func knowledgeLibraryRouteFor(item aiKnowledgeLibraryItem) string {
	if item.Job != nil && item.Job.Status == "failed" {
		return knowledgeLibraryRoute(item.ID, item.Job.ID)
	}
	return "/knowledge"
}

// knowledgeLibrary is a metadata-only library view. It never returns managed
// source bytes, extracted text or chunks; content requires the separate
// `knowledge` scope with explicit versioned sources.
func (t *aiWorkspaceTool) knowledgeLibrary(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("knowledge_actions"); err != nil {
		return nil, err
	}
	var input struct {
		View   string `json:"view"`
		ID     string `json:"id"`
		Query  string `json:"query"`
		Status string `json:"status"`
		Limit  *int   `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		allowed := key == "view" || key == "query" || key == "status" || key == "limit" || key == "offset" ||
			((input.View == "source" || input.View == "job") && key == "id")
		if !allowed || string(raw) == "null" {
			return nil, errors.New("unexpected or null knowledge library argument")
		}
	}
	query := strings.TrimSpace(input.Query)
	if utf8.RuneCountInString(query) > 200 {
		return nil, errors.New("knowledge library query must be at most 200 characters")
	}
	status := strings.TrimSpace(input.Status)
	if status != "" && status != "ready" && status != "indexing" && status != "failed" {
		return nil, errors.New("knowledge library status must be ready, indexing or failed")
	}
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 20 || input.Offset < 0 || input.Offset > 1000 {
		return nil, errors.New("knowledge library pages require limit 1-20 and offset 0-1000")
	}
	if (input.View == "source" || input.View == "job") && !isValidKnowledgeID(input.ID) {
		return nil, errors.New("knowledge library detail requires a canonical UUID")
	}
	nowText := t.api.options.Now().UTC().Format(time.RFC3339Nano)
	switch input.View {
	case "source", "job":
		return t.knowledgeLibraryDetail(ctx, input.View, input.ID, nowText)
	case "list", "":
		return t.knowledgeLibraryList(ctx, query, status, limit, input.Offset, nowText)
	default:
		return nil, errors.New("knowledge library view must be list, source or job")
	}
}

func (t *aiWorkspaceTool) knowledgeLibraryList(ctx context.Context, query, status string, limit, offset int, nowText string) (any, error) {
	db := t.api.db.WithContext(ctx)
	base := db.Table("knowledge_sources AS s").Where("s.deleted_at IS NULL")
	if query != "" {
		base = base.Where("s.name LIKE ? ESCAPE '\\'", "%"+escapeKnowledgeLike(query)+"%")
	}
	if status != "" {
		base = base.Where("s.status = ?", status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	var rows []knowledgeSourceRow
	err := base.Select(`
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
		Offset(offset).Limit(limit + 1).Scan(&rows).Error
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]aiKnowledgeLibraryItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, aiKnowledgeLibraryItemFromRow(row))
	}
	var next *int
	if hasMore && offset+limit <= 1000 {
		value := offset + limit
		next = &value
	}
	return map[string]any{
		"view": "list", "items": items, "total_items": total, "has_more": hasMore,
		"next_offset": next, "window_limited": hasMore && next == nil, "server_now": nowText,
		"content_included": false,
		"instruction":      "Metadata only: names, states and index progress. Managed text and chunks require the separate knowledge scope with explicit sources. Read again before proposing so the version is current.",
	}, nil
}

func (t *aiWorkspaceTool) knowledgeLibraryDetail(ctx context.Context, view, id, nowText string) (any, error) {
	db := t.api.db.WithContext(ctx)
	if view == "source" {
		row, err := loadKnowledgeSourceRow(db, id, false)
		if err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		return map[string]any{"view": "source", "source": aiKnowledgeLibraryItemFromRow(row), "server_now": nowText, "content_included": false}, nil
	}
	var job models.KnowledgeIndexJob
	if err := db.First(&job, "id = ?", id).Error; err != nil {
		return nil, safeAIWorkspaceError(ctx, knowledgeJobNotFound(err))
	}
	row, err := loadKnowledgeSourceRow(db, job.SourceID, false)
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return map[string]any{
		"view": "job", "job": aiKnowledgeJobPreview{ID: job.ID, Operation: job.Operation, Status: job.Status, Attempt: job.Attempt, ErrorCode: safeAIKnowledgeErrorCode(job.ErrorCode)},
		"source": aiKnowledgeLibraryItemFromRow(row), "route": knowledgeLibraryRouteFor(aiKnowledgeLibraryItemFromRow(row)),
		"server_now": nowText, "content_included": false,
	}, nil
}

func aiKnowledgeLibraryItemFromRow(row knowledgeSourceRow) aiKnowledgeLibraryItem {
	item := aiKnowledgeLibraryItem{
		ID: row.ID, Name: row.Name, Title: row.Title, SourceType: row.SourceType, MimeType: row.MimeType,
		Status: row.Status, Version: row.Version, SizeBytes: row.SizeBytes, ChunkCount: row.ChunkCount,
		DocumentVersion: row.DocumentVersion, LastIndexedAt: row.LastIndexedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.LatestJobID != nil {
		item.Job = &aiKnowledgeJobPreview{
			ID: *row.LatestJobID, Operation: stringPointerValue(row.LatestJobOperation),
			Status: stringPointerValue(row.LatestJobStatus), Attempt: intPointerValue(row.LatestJobAttempt),
			ErrorCode: safeAIKnowledgeErrorCode(row.LatestJobErrorCode),
		}
	}
	item.Route = knowledgeLibraryRouteFor(item)
	return item
}
