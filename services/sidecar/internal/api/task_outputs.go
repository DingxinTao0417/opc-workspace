package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
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

const (
	maxArtifactRequestBytes   int64 = 100 << 20
	maxArtifactManifestBytes        = maxJSONBodyBytes
	maxArtifactsPerSubmission       = 20
	maxArtifactTextCharacters       = 500_000
)

type submitOutputRequest struct {
	Summary   string                `json:"summary"`
	Artifacts []submitArtifactInput `json:"artifacts"`
}

type submitArtifactInput struct {
	ClientRef        string          `json:"client_ref"`
	StorageKind      string          `json:"storage_kind"`
	Name             string          `json:"name"`
	ContentText      *string         `json:"content_text"`
	ReferenceURL     *string         `json:"reference_url"`
	StructuredJSON   json.RawMessage `json:"structured_json"`
	FileField        *string         `json:"file_field"`
	RequiresFollowup bool            `json:"requires_followup"`
}

type preparedArtifact struct {
	ClientRef        string
	ID               string
	Position         int
	StorageKind      string
	Name             string
	FileField        string
	ContentText      *string
	ReferenceURL     *string
	StructuredJSON   *string
	RelativePath     *string
	MimeType         *string
	SizeBytes        *int64
	SHA256           *string
	RequiresFollowup bool
	StagedFile       *stagedArtifactFile
}

type committedArtifactFile struct {
	artifactID   string
	relativePath string
}

type reviewTaskOutputRequest struct {
	Decision string  `json:"decision"`
	Reason   *string `json:"reason"`
}

type deleteTaskArtifactRequest struct {
	Reason string `json:"reason"`
}

type taskSubmissionOutput struct {
	ID                 string                  `json:"id"`
	TaskID             string                  `json:"task_id"`
	Sequence           int                     `json:"sequence"`
	Status             string                  `json:"status"`
	Summary            string                  `json:"summary"`
	SubmittedByActorID string                  `json:"submitted_by_actor_id"`
	SubmittedByActor   assignmentActorSummary  `json:"submitted_by_actor"`
	SubmittedAt        string                  `json:"submitted_at"`
	ReviewedByActorID  *string                 `json:"reviewed_by_actor_id"`
	ReviewedByActor    *assignmentActorSummary `json:"reviewed_by_actor"`
	ReviewedAt         *string                 `json:"reviewed_at"`
	ReviewReason       *string                 `json:"review_reason"`
	WithdrawnByActorID *string                 `json:"withdrawn_by_actor_id"`
	WithdrawnByActor   *assignmentActorSummary `json:"withdrawn_by_actor"`
	WithdrawnAt        *string                 `json:"withdrawn_at"`
	IsInferred         bool                    `json:"is_inferred"`
	ArtifactCount      int64                   `json:"artifact_count"`
	Artifacts          []taskArtifactSummary   `json:"artifacts"`
}

type taskArtifactSummary struct {
	ID                 string                  `json:"id"`
	TaskID             string                  `json:"task_id"`
	SubmissionID       string                  `json:"submission_id"`
	SubmissionStatus   string                  `json:"submission_status"`
	Position           int                     `json:"position"`
	StorageKind        string                  `json:"storage_kind"`
	Name               string                  `json:"name"`
	MimeType           *string                 `json:"mime_type"`
	SizeBytes          *int64                  `json:"size_bytes"`
	SHA256             *string                 `json:"sha256"`
	RequiresFollowup   bool                    `json:"requires_followup"`
	ProducedByActorID  string                  `json:"produced_by_actor_id"`
	ProducedByActor    assignmentActorSummary  `json:"produced_by_actor"`
	RecordedByActorID  string                  `json:"recorded_by_actor_id"`
	RecordedByActor    assignmentActorSummary  `json:"recorded_by_actor"`
	IntegrityStatus    string                  `json:"integrity_status"`
	IntegrityCheckedAt *string                 `json:"integrity_checked_at"`
	DeletedAt          *string                 `json:"deleted_at"`
	DeletedByActorID   *string                 `json:"deleted_by_actor_id"`
	DeletedByActor     *assignmentActorSummary `json:"deleted_by_actor"`
	DeleteReason       *string                 `json:"delete_reason"`
	CreatedAt          string                  `json:"created_at"`
}

type taskArtifactDetail struct {
	taskArtifactSummary
	ContentText    *string         `json:"content_text"`
	ReferenceURL   *string         `json:"reference_url"`
	StructuredJSON json.RawMessage `json:"structured_json"`
}

type submitOutputResponse struct {
	Task       models.Task             `json:"task"`
	Submission taskSubmissionOutput    `json:"submission"`
	Artifacts  []taskArtifactSummary   `json:"artifacts"`
	Event      taskWorkflowEventOutput `json:"event"`
}

type reviewTaskOutputResponse struct {
	Task       models.Task             `json:"task"`
	Submission taskSubmissionOutput    `json:"submission"`
	Event      taskWorkflowEventOutput `json:"event"`
}

type deleteTaskArtifactResponse struct {
	Task     models.Task             `json:"task"`
	Artifact taskArtifactSummary     `json:"artifact"`
	Event    taskWorkflowEventOutput `json:"event"`
}

type taskOutputMeta struct {
	Page        int   `json:"page"`
	PageSize    int   `json:"page_size"`
	Total       int64 `json:"total"`
	TaskVersion int64 `json:"task_version"`
}

type taskSubmissionRow struct {
	models.TaskSubmission
	ArtifactCount    int64   `gorm:"column:artifact_count"`
	SubmittedType    string  `gorm:"column:submitted_type"`
	SubmittedName    string  `gorm:"column:submitted_name"`
	SubmittedStatus  string  `gorm:"column:submitted_status"`
	SubmittedBuiltin bool    `gorm:"column:submitted_builtin"`
	SubmittedVersion int64   `gorm:"column:submitted_version"`
	ReviewedType     *string `gorm:"column:reviewed_type"`
	ReviewedName     *string `gorm:"column:reviewed_name"`
	ReviewedStatus   *string `gorm:"column:reviewed_status"`
	ReviewedBuiltin  *bool   `gorm:"column:reviewed_builtin"`
	ReviewedVersion  *int64  `gorm:"column:reviewed_version"`
	WithdrawnType    *string `gorm:"column:withdrawn_type"`
	WithdrawnName    *string `gorm:"column:withdrawn_name"`
	WithdrawnStatus  *string `gorm:"column:withdrawn_status"`
	WithdrawnBuiltin *bool   `gorm:"column:withdrawn_builtin"`
	WithdrawnVersion *int64  `gorm:"column:withdrawn_version"`
}

type taskArtifactRow struct {
	models.TaskArtifact
	SubmissionSequence int     `gorm:"column:submission_sequence"`
	SubmissionStatus   string  `gorm:"column:submission_status"`
	ProducedType       string  `gorm:"column:produced_type"`
	ProducedName       string  `gorm:"column:produced_name"`
	ProducedStatus     string  `gorm:"column:produced_status"`
	ProducedBuiltin    bool    `gorm:"column:produced_builtin"`
	ProducedVersion    int64   `gorm:"column:produced_version"`
	RecordedType       string  `gorm:"column:recorded_type"`
	RecordedName       string  `gorm:"column:recorded_name"`
	RecordedStatus     string  `gorm:"column:recorded_status"`
	RecordedBuiltin    bool    `gorm:"column:recorded_builtin"`
	RecordedVersion    int64   `gorm:"column:recorded_version"`
	DeletedType        *string `gorm:"column:deleted_type"`
	DeletedName        *string `gorm:"column:deleted_name"`
	DeletedStatus      *string `gorm:"column:deleted_status"`
	DeletedBuiltin     *bool   `gorm:"column:deleted_builtin"`
	DeletedVersion     *int64  `gorm:"column:deleted_version"`
}

func taskSubmissionRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("task_submissions AS submission").Select(`
		submission.*,
		(SELECT COUNT(*) FROM task_artifacts AS artifact WHERE artifact.submission_id = submission.id) AS artifact_count,
		submitted.type AS submitted_type,
		submitted.display_name AS submitted_name,
		submitted.status AS submitted_status,
		submitted.is_builtin AS submitted_builtin,
		submitted.version AS submitted_version,
		reviewed.type AS reviewed_type,
		reviewed.display_name AS reviewed_name,
		reviewed.status AS reviewed_status,
		reviewed.is_builtin AS reviewed_builtin,
		reviewed.version AS reviewed_version,
		withdrawn.type AS withdrawn_type,
		withdrawn.display_name AS withdrawn_name,
		withdrawn.status AS withdrawn_status,
		withdrawn.is_builtin AS withdrawn_builtin,
		withdrawn.version AS withdrawn_version
	`).
		Joins("JOIN actors AS submitted ON submitted.id = submission.submitted_by_actor_id").
		Joins("LEFT JOIN actors AS reviewed ON reviewed.id = submission.reviewed_by_actor_id").
		Joins("LEFT JOIN actors AS withdrawn ON withdrawn.id = submission.withdrawn_by_actor_id")
}

func taskArtifactRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("task_artifacts AS artifact").Select(`
		artifact.*,
		submission.sequence AS submission_sequence,
		submission.status AS submission_status,
		produced.type AS produced_type,
		produced.display_name AS produced_name,
		produced.status AS produced_status,
		produced.is_builtin AS produced_builtin,
		produced.version AS produced_version,
		recorded.type AS recorded_type,
		recorded.display_name AS recorded_name,
		recorded.status AS recorded_status,
		recorded.is_builtin AS recorded_builtin,
		recorded.version AS recorded_version,
		deleted.type AS deleted_type,
		deleted.display_name AS deleted_name,
		deleted.status AS deleted_status,
		deleted.is_builtin AS deleted_builtin,
		deleted.version AS deleted_version
	`).
		Joins("JOIN task_submissions AS submission ON submission.id = artifact.submission_id").
		Joins("JOIN actors AS produced ON produced.id = artifact.produced_by_actor_id").
		Joins("JOIN actors AS recorded ON recorded.id = artifact.recorded_by_actor_id").
		Joins("LEFT JOIN actors AS deleted ON deleted.id = artifact.deleted_by_actor_id")
}

func submissionOutputFromRow(row taskSubmissionRow) (taskSubmissionOutput, error) {
	submitted := assignmentActorSummary{
		ID: row.SubmittedByActorID, Type: row.SubmittedType, DisplayName: row.SubmittedName,
		Status: row.SubmittedStatus, IsBuiltin: row.SubmittedBuiltin, Version: row.SubmittedVersion,
	}
	reviewed, err := optionalActorSummary(
		row.ReviewedByActorID, row.ReviewedType, row.ReviewedName, row.ReviewedStatus,
		row.ReviewedBuiltin, row.ReviewedVersion,
	)
	if err != nil {
		return taskSubmissionOutput{}, err
	}
	withdrawn, err := optionalActorSummary(
		row.WithdrawnByActorID, row.WithdrawnType, row.WithdrawnName, row.WithdrawnStatus,
		row.WithdrawnBuiltin, row.WithdrawnVersion,
	)
	if err != nil {
		return taskSubmissionOutput{}, err
	}
	return taskSubmissionOutput{
		ID: row.ID, TaskID: row.TaskID, Sequence: row.Sequence, Status: row.Status, Summary: row.Summary,
		SubmittedByActorID: row.SubmittedByActorID, SubmittedByActor: submitted,
		SubmittedAt: normalizeTimestamp(row.SubmittedAt), ReviewedByActorID: row.ReviewedByActorID,
		ReviewedByActor: reviewed, ReviewedAt: normalizeOptionalTimestamp(row.ReviewedAt), ReviewReason: row.ReviewReason,
		WithdrawnByActorID: row.WithdrawnByActorID, WithdrawnByActor: withdrawn,
		WithdrawnAt: normalizeOptionalTimestamp(row.WithdrawnAt), IsInferred: row.IsInferred,
		ArtifactCount: row.ArtifactCount, Artifacts: make([]taskArtifactSummary, 0),
	}, nil
}

func artifactSummaryFromRow(row taskArtifactRow) (taskArtifactSummary, error) {
	produced := assignmentActorSummary{
		ID: row.ProducedByActorID, Type: row.ProducedType, DisplayName: row.ProducedName,
		Status: row.ProducedStatus, IsBuiltin: row.ProducedBuiltin, Version: row.ProducedVersion,
	}
	recorded := assignmentActorSummary{
		ID: row.RecordedByActorID, Type: row.RecordedType, DisplayName: row.RecordedName,
		Status: row.RecordedStatus, IsBuiltin: row.RecordedBuiltin, Version: row.RecordedVersion,
	}
	deleted, err := optionalActorSummary(
		row.DeletedByActorID, row.DeletedType, row.DeletedName, row.DeletedStatus,
		row.DeletedBuiltin, row.DeletedVersion,
	)
	if err != nil {
		return taskArtifactSummary{}, err
	}
	return taskArtifactSummary{
		ID: row.ID, TaskID: row.TaskID, SubmissionID: row.SubmissionID, SubmissionStatus: row.SubmissionStatus,
		Position:    row.Position,
		StorageKind: row.StorageKind, Name: row.Name, MimeType: row.MimeType, SizeBytes: row.SizeBytes,
		SHA256: row.SHA256, RequiresFollowup: row.RequiresFollowup,
		ProducedByActorID: row.ProducedByActorID, ProducedByActor: produced,
		RecordedByActorID: row.RecordedByActorID, RecordedByActor: recorded,
		IntegrityStatus: row.IntegrityStatus, IntegrityCheckedAt: normalizeOptionalTimestamp(row.IntegrityCheckedAt),
		DeletedAt: normalizeOptionalTimestamp(row.DeletedAt), DeletedByActorID: row.DeletedByActorID,
		DeletedByActor: deleted, DeleteReason: row.DeleteReason, CreatedAt: normalizeTimestamp(row.CreatedAt),
	}, nil
}

func optionalActorSummary(
	id, actorType, name, status *string,
	isBuiltin *bool,
	version *int64,
) (*assignmentActorSummary, error) {
	if id == nil {
		return nil, nil
	}
	if actorType == nil || name == nil || status == nil || isBuiltin == nil || version == nil {
		return nil, errors.New("referenced Artifact actor is missing")
	}
	return &assignmentActorSummary{
		ID: *id, Type: *actorType, DisplayName: *name, Status: *status,
		IsBuiltin: *isBuiltin, Version: *version,
	}, nil
}

func normalizeOptionalTimestamp(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := normalizeTimestamp(*value)
	return &normalized
}

func (a *API) listTaskSubmissions(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	var task models.Task
	var rows []taskSubmissionRow
	var outputs []taskSubmissionOutput
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&task, "id = ?", taskIDValue).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.TaskSubmission{}).Where("task_id = ?", taskIDValue).Count(&total).Error; err != nil {
			return err
		}
		if err := taskSubmissionRowsQuery(tx).
			Where("submission.task_id = ?", taskIDValue).
			Order("submission.sequence DESC").Order("submission.id DESC").
			Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
			return err
		}
		outputs = make([]taskSubmissionOutput, len(rows))
		for index, row := range rows {
			output, err := submissionOutputFromRow(row)
			if err != nil {
				return err
			}
			outputs[index] = output
		}
		return hydrateSubmissionArtifacts(tx, outputs, true)
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{"data": outputs, "meta": taskOutputMeta{
		Page: page, PageSize: pageSize, Total: total, TaskVersion: task.Version,
	}})
}

func (a *API) listTaskArtifacts(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}
	includeDeleted, err := optionalBooleanQuery(c, "include_deleted")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	submissionID := strings.TrimSpace(c.Query("submission_id"))
	if submissionID != "" {
		parsed, parseErr := uuid.Parse(submissionID)
		if parseErr != nil {
			writeError(c, http.StatusBadRequest, "INVALID_SUBMISSION_ID", "submission_id must be a UUID")
			return
		}
		submissionID = parsed.String()
	}
	var task models.Task
	var rows []taskArtifactRow
	var total int64
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&task, "id = ?", taskIDValue).Error; err != nil {
			return err
		}
		base := tx.Model(&models.TaskArtifact{}).Where("task_id = ?", taskIDValue)
		if !includeDeleted {
			base = base.Where("deleted_at IS NULL")
		}
		if submissionID != "" {
			base = base.Where("submission_id = ?", submissionID)
		}
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		query := taskArtifactRowsQuery(tx).Where("artifact.task_id = ?", taskIDValue)
		if !includeDeleted {
			query = query.Where("artifact.deleted_at IS NULL")
		}
		if submissionID != "" {
			query = query.Where("artifact.submission_id = ?", submissionID)
		}
		return query.Order("submission.sequence DESC").Order("artifact.position ASC").Order("artifact.id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	outputs := make([]taskArtifactSummary, len(rows))
	for index, row := range rows {
		outputs[index], err = artifactSummaryFromRow(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{"data": outputs, "meta": taskOutputMeta{
		Page: page, PageSize: pageSize, Total: total, TaskVersion: task.Version,
	}})
}

func optionalBooleanQuery(c *gin.Context, key string) (bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
}

func hydrateSubmissionArtifacts(db *gorm.DB, submissions []taskSubmissionOutput, includeDeleted bool) error {
	if len(submissions) == 0 {
		return nil
	}
	indices := make(map[string]int, len(submissions))
	ids := make([]string, len(submissions))
	for index := range submissions {
		indices[submissions[index].ID] = index
		ids[index] = submissions[index].ID
		submissions[index].Artifacts = make([]taskArtifactSummary, 0)
	}
	query := taskArtifactRowsQuery(db).Where("artifact.submission_id IN ?", ids)
	if !includeDeleted {
		query = query.Where("artifact.deleted_at IS NULL")
	}
	var rows []taskArtifactRow
	if err := query.Order("artifact.submission_id ASC").Order("artifact.position ASC").Order("artifact.id ASC").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indices[row.SubmissionID]
		if !ok {
			continue
		}
		artifact, err := artifactSummaryFromRow(row)
		if err != nil {
			return err
		}
		submissions[index].Artifacts = append(submissions[index].Artifacts, artifact)
	}
	return nil
}

func loadSubmissionOutput(db *gorm.DB, submissionID string) (taskSubmissionOutput, error) {
	var row taskSubmissionRow
	if err := taskSubmissionRowsQuery(db).Where("submission.id = ?", submissionID).Take(&row).Error; err != nil {
		return taskSubmissionOutput{}, err
	}
	output, err := submissionOutputFromRow(row)
	if err != nil {
		return taskSubmissionOutput{}, err
	}
	outputs := []taskSubmissionOutput{output}
	if err := hydrateSubmissionArtifacts(db, outputs, true); err != nil {
		return taskSubmissionOutput{}, err
	}
	return outputs[0], nil
}

func loadArtifactRow(db *gorm.DB, artifactID string) (taskArtifactRow, error) {
	var row taskArtifactRow
	if err := taskArtifactRowsQuery(db).Where("artifact.id = ?", artifactID).Take(&row).Error; err != nil {
		return taskArtifactRow{}, err
	}
	return row, nil
}

func artifactID(c *gin.Context) (string, bool) {
	value := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(value)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_ARTIFACT_ID", "Artifact id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func (a *API) getTaskArtifact(c *gin.Context) {
	id, ok := artifactID(c)
	if !ok {
		return
	}
	var row taskArtifactRow
	var task models.Task
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		row, err = loadArtifactRow(tx, id)
		if err != nil {
			return err
		}
		return tx.Select("version").First(&task, "id = ?", row.TaskID).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "Artifact not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	summary, err := artifactSummaryFromRow(row)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	detail := taskArtifactDetail{taskArtifactSummary: summary}
	if row.DeletedAt == nil {
		detail.ContentText = row.ContentText
		detail.ReferenceURL = row.ReferenceURL
		if row.StructuredJSON != nil {
			canonical, err := canonicalStructuredJSONObject([]byte(*row.StructuredJSON))
			if err != nil {
				writeDatabaseError(c)
				return
			}
			detail.StructuredJSON = canonical
		}
	}
	setProjectETag(c, task.Version)
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

func (a *API) getTaskArtifactContent(c *gin.Context) {
	id, ok := artifactID(c)
	if !ok {
		return
	}
	row, err := loadArtifactRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "Artifact not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	if row.DeletedAt != nil {
		writeError(c, http.StatusGone, "ARTIFACT_DELETED", "The Artifact has been deleted")
		return
	}
	if row.StorageKind != "file" || row.RelativePath == nil || row.SHA256 == nil || row.SizeBytes == nil {
		writeError(c, http.StatusConflict, "ARTIFACT_CONTENT_UNAVAILABLE", "This Artifact does not contain downloadable file content")
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
		return
	}
	file, info, err := a.artifactStore.openObject(*row.RelativePath)
	if err != nil {
		if errors.Is(err, errArtifactObjectMissing) {
			if !a.persistArtifactIntegrity(c, id, "missing") {
				return
			}
			writeError(c, http.StatusGone, "ARTIFACT_FILE_MISSING", "The Artifact file is missing from controlled storage")
			return
		}
		writeError(c, http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be opened from controlled storage")
		return
	}
	defer file.Close()
	if info.Size() != *row.SizeBytes {
		if !a.persistArtifactIntegrity(c, id, "mismatch") {
			return
		}
		writeError(c, http.StatusConflict, "ARTIFACT_INTEGRITY_MISMATCH", "The Artifact file failed its integrity check")
		return
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		writeError(c, http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be verified in controlled storage")
		return
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if actual != *row.SHA256 {
		if !a.persistArtifactIntegrity(c, id, "mismatch") {
			return
		}
		writeError(c, http.StatusConflict, "ARTIFACT_INTEGRITY_MISMATCH", "The Artifact file failed its integrity check")
		return
	}
	if !a.persistArtifactIntegrity(c, id, "verified") {
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(c, http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be read from controlled storage")
		return
	}
	contentType := "application/octet-stream"
	if row.MimeType != nil && strings.TrimSpace(*row.MimeType) != "" {
		contentType = *row.MimeType
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": row.Name})
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.FormatInt(*row.SizeBytes, 10))
	c.Header("Content-Disposition", disposition)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", `"`+*row.SHA256+`"`)
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, file); err != nil && a.options.Logger != nil {
		a.options.Logger.Printf(
			"Artifact response stream failed artifact_id=%s request_id=%s error=%v",
			id, requestIDFromContext(c), err,
		)
	}
}

func (a *API) persistArtifactIntegrity(c *gin.Context, id, status string) bool {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Integrity is a derived storage observation, not a Task fact mutation, so
	// this update deliberately does not increment the Task aggregate version.
	result := a.db.WithContext(c.Request.Context()).Model(&models.TaskArtifact{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"integrity_status": status, "integrity_checked_at": now})
	if result.Error == nil && result.RowsAffected == 1 {
		return true
	}
	if a.options.Logger != nil {
		a.options.Logger.Printf(
			"Artifact integrity update failed artifact_id=%s request_id=%s rows=%d error=%v",
			id, requestIDFromContext(c), result.RowsAffected, result.Error,
		)
	}
	writeDatabaseError(c)
	return false
}

func (a *API) submitTaskOutput(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
		return
	}
	input, artifacts, err := a.prepareSubmitOutputRequest(c)
	if err != nil {
		writeTaskOutputError(c, err)
		return
	}
	defer func() {
		for _, artifact := range artifacts {
			if artifact.StagedFile != nil {
				a.artifactStore.discardStagedFile(*artifact.StagedFile)
			}
		}
	}()
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := submitOutputRequestHash(expectedVersion, input.Summary, artifacts)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/submit-output", taskIDValue)
	statusCode := http.StatusCreated
	replayed := false
	committedFiles := make([]committedArtifactFile, 0)
	var response submitOutputResponse
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

		var task models.Task
		if err := tx.First(&task, "id = ?", taskIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		if task.Version != expectedVersion {
			return taskVersionConflict()
		}
		if task.ReviewPolicy != "manual" {
			return newProjectRequestError(http.StatusConflict, "TASK_MANUAL_REVIEW_REQUIRED", "Only manual-review tasks accept submitted output")
		}
		if task.Status != "todo" && task.Status != "in_progress" {
			return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_NOT_ALLOWED", "Output can only be submitted from todo or in-progress status")
		}
		assignee, err := requireTaskOutputActors(tx, taskIDValue)
		if err != nil {
			return err
		}
		var sequence int
		if err := tx.Model(&models.TaskSubmission{}).Where("task_id = ?", taskIDValue).
			Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		submission := models.TaskSubmission{
			ID: uuid.NewString(), TaskID: taskIDValue, Sequence: sequence, Status: "pending_review",
			Summary: input.Summary, SubmittedByActorID: models.BuiltinOwnerActorID, SubmittedAt: now,
		}
		if err := tx.Create(&submission).Error; err != nil {
			return mapTaskOutputConstraintError(err)
		}
		for _, artifact := range artifacts {
			if artifact.StagedFile != nil {
				if err := a.artifactStore.commitStagedFile(*artifact.StagedFile); err != nil {
					return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be stored")
				}
				committedFiles = append(committedFiles, committedArtifactFile{
					artifactID: artifact.ID, relativePath: artifact.StagedFile.relativePath,
				})
			}
			record := models.TaskArtifact{
				ID: artifact.ID, TaskID: taskIDValue, SubmissionID: submission.ID, Position: artifact.Position,
				StorageKind: artifact.StorageKind, Name: artifact.Name, ContentText: artifact.ContentText,
				ReferenceURL: artifact.ReferenceURL, StructuredJSON: artifact.StructuredJSON,
				RelativePath: artifact.RelativePath, MimeType: artifact.MimeType, SizeBytes: artifact.SizeBytes,
				SHA256: artifact.SHA256, RequiresFollowup: artifact.RequiresFollowup,
				ProducedByActorID: assignee.ActorID, RecordedByActorID: models.BuiltinOwnerActorID,
				IntegrityStatus: "unverified", CreatedAt: now,
			}
			if artifact.StorageKind == "file" {
				record.IntegrityStatus = "verified"
				record.IntegrityCheckedAt = &now
			}
			if err := tx.Create(&record).Error; err != nil {
				return mapTaskOutputConstraintError(err)
			}
		}
		updates := map[string]any{
			"status": "waiting_review", "current_submission_id": submission.ID,
			"submitted_at": now, "reviewed_at": nil, "completed_at": nil,
			"updated_at": now, "version": gorm.Expr("version + 1"),
		}
		result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", taskIDValue, expectedVersion).Updates(updates)
		if result.Error != nil {
			return mapTaskOutputConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		updated, err := loadTask(tx, taskIDValue)
		if err != nil {
			return err
		}
		submissionOutput, err := loadSubmissionOutput(tx, submission.ID)
		if err != nil {
			return err
		}
		artifactSnapshots := artifactEventSnapshots(submissionOutput.Artifacts)
		sequenceValue := 1
		event, err := recordTaskOutputEvent(tx, "task_output_submitted", taskIDValue, &submission.ID, nil,
			taskLifecycleSnapshot(task, ""), map[string]any{
				"status": updated.Status, "review_policy": updated.ReviewPolicy,
				"current_submission_id": updated.CurrentSubmissionID, "submitted_at": updated.SubmittedAt,
				"reviewed_at": updated.ReviewedAt, "version": updated.Version,
				"submission_id": submission.ID, "submission_sequence": submission.Sequence,
				"artifact_count": len(submissionOutput.Artifacts), "artifacts": artifactSnapshots,
			}, requestIDFromContext(c), now, sequenceValue)
		if err != nil {
			return err
		}
		if err := reconcileInboxItemsForTask(tx, taskIDValue, requestIDFromContext(c), now); err != nil {
			return err
		}
		response = submitOutputResponse{
			Task: updated, Submission: submissionOutput, Artifacts: submissionOutput.Artifacts, Event: event,
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, taskIDValue, requestHash, statusCode, response, now)
	})
	if err != nil {
		a.compensateSubmittedArtifactFiles(taskIDValue, committedFiles)
		writeTaskOutputError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) compensateSubmittedArtifactFiles(taskIDValue string, committed []committedArtifactFile) {
	for index := len(committed) - 1; index >= 0; index-- {
		file := committed[index]
		var count int64
		result := a.db.Model(&models.TaskArtifact{}).
			Where(
				"id = ? AND task_id = ? AND storage_kind = 'file' AND relative_path = ? AND deleted_at IS NULL",
				file.artifactID, taskIDValue, file.relativePath,
			).
			Count(&count)
		if result.Error != nil {
			// A failed COMMIT can be ambiguous: SQLite may have made the row
			// durable even though the driver returned an error. Preserve the
			// object whenever the database cannot prove it is unreferenced; the
			// startup reconciler will quarantine a real orphan safely.
			if a.options.Logger != nil {
				a.options.Logger.Printf(
					"Artifact compensation deferred task_id=%s artifact_id=%s error=%v",
					taskIDValue, file.artifactID, result.Error,
				)
			}
			continue
		}
		if count != 0 {
			continue
		}
		if cleanupErr := a.artifactStore.discardCommittedFile(file.relativePath); cleanupErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf(
				"Artifact compensation failed task_id=%s artifact_id=%s error=%v",
				taskIDValue, file.artifactID, cleanupErr,
			)
		}
	}
}

func (a *API) reviewTaskOutput(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input reviewTaskOutputRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.Decision = strings.TrimSpace(input.Decision)
	if input.Decision != "accept" && input.Decision != "request_changes" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "decision must be accept or request_changes")
		return
	}
	reason := ""
	if input.Reason != nil {
		reason = strings.TrimSpace(*input.Reason)
	}
	if input.Decision == "request_changes" && reason == "" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason is required when requesting changes")
		return
	}
	if utf8.RuneCountInString(reason) > 1_000 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason cannot exceed 1000 characters")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, map[string]any{
		"expected_version": expectedVersion, "decision": input.Decision, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/review", taskIDValue)
	statusCode := http.StatusOK
	replayed := false
	var response reviewTaskOutputResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, replayStatus, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			statusCode = replayStatus
			return nil
		}
		var task models.Task
		if err := tx.First(&task, "id = ?", taskIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		if task.Version != expectedVersion {
			return taskVersionConflict()
		}
		if task.ReviewPolicy != "manual" || task.Status != "waiting_review" || task.CurrentSubmissionID == nil {
			return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The task does not have output awaiting manual review")
		}
		if _, err := requireActiveOwnerReviewer(tx, taskIDValue); err != nil {
			return err
		}
		var submission models.TaskSubmission
		if err := tx.First(&submission, "id = ? AND task_id = ?", *task.CurrentSubmissionID, taskIDValue).Error; err != nil {
			return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is unavailable")
		}
		if submission.Status != "pending_review" {
			return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The current submission is no longer pending review")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		requestID := requestIDFromContext(c)
		commandSequence := 1
		action := "task_changes_requested"
		targetStatus := "in_progress"
		updates := map[string]any{
			"status": targetStatus, "reviewed_at": now, "completed_at": nil,
			"updated_at": now, "version": gorm.Expr("version + 1"),
		}
		if input.Decision == "accept" {
			action = "task_review_accepted"
			targetStatus = "done"
			updates["status"] = targetStatus
			updates["completed_at"] = now
			var err error
			commandSequence, err = closeActiveAssignmentsForTerminalTask(
				tx, taskIDValue, requestID, now, "Task review accepted", commandSequence,
			)
			if err != nil {
				return err
			}
		}
		submissionUpdates := map[string]any{
			"status":               map[bool]string{true: "accepted", false: "changes_requested"}[input.Decision == "accept"],
			"reviewed_by_actor_id": models.BuiltinOwnerActorID, "reviewed_at": now,
		}
		if reason != "" {
			submissionUpdates["review_reason"] = reason
		} else {
			submissionUpdates["review_reason"] = nil
		}
		result := tx.Model(&models.TaskSubmission{}).
			Where("id = ? AND task_id = ? AND status = 'pending_review'", submission.ID, taskIDValue).
			Updates(submissionUpdates)
		if result.Error != nil {
			return mapTaskOutputConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The current submission is no longer pending review")
		}
		result = tx.Model(&models.Task{}).Where("id = ? AND version = ?", taskIDValue, expectedVersion).Updates(updates)
		if result.Error != nil {
			return mapTaskOutputConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		updated, err := loadTask(tx, taskIDValue)
		if err != nil {
			return err
		}
		submissionOutput, err := loadSubmissionOutput(tx, submission.ID)
		if err != nil {
			return err
		}
		previousSnapshot := taskLifecycleSnapshot(task, "")
		previousSnapshot["submission_id"] = submission.ID
		previousSnapshot["submission_status"] = "pending_review"
		currentSnapshot := taskLifecycleSnapshot(updated, reason)
		currentSnapshot["submission_id"] = submission.ID
		currentSnapshot["submission_status"] = submissionOutput.Status
		event, err := recordTaskOutputEvent(
			tx, action, taskIDValue, &submission.ID, nil, previousSnapshot, currentSnapshot,
			requestID, now, commandSequence,
		)
		if err != nil {
			return err
		}
		if err := reconcileInboxItemsForTask(tx, taskIDValue, requestID, now); err != nil {
			return err
		}
		response = reviewTaskOutputResponse{Task: updated, Submission: submissionOutput, Event: event}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, taskIDValue, requestHash, statusCode, response, now)
	})
	if err != nil {
		writeTaskOutputError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) deleteTaskArtifact(c *gin.Context) {
	id, ok := artifactID(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusBadRequest, "CONFIRMATION_REQUIRED", "confirm=true is required to delete an Artifact")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input deleteTaskArtifactRequest
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
		"artifact_id": id, "expected_version": expectedVersion, "confirm": true, "reason": reason,
	})
	if !ok {
		return
	}
	endpoint := fmt.Sprintf("DELETE /api/v1/artifacts/%s", id)
	replayed := false
	var moved *trashedArtifactFile
	var response deleteTaskArtifactResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, _, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			return nil
		}
		row, err := loadArtifactRow(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "ARTIFACT_NOT_FOUND", "Artifact not found")
			}
			return err
		}
		if row.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_ALREADY_DELETED", "The Artifact is already deleted")
		}
		var task models.Task
		if err := tx.First(&task, "id = ?", row.TaskID).Error; err != nil {
			return err
		}
		if task.Version != expectedVersion {
			return taskVersionConflict()
		}
		var submission models.TaskSubmission
		if err := tx.Select("status").First(&submission, "id = ?", row.SubmissionID).Error; err != nil {
			return err
		}
		if submission.Status == "pending_review" {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_PENDING_REVIEW", "Artifacts cannot be deleted while their submission is pending review")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		fileMissing := false
		if row.StorageKind == "file" {
			if a.artifactStore == nil || row.RelativePath == nil {
				return newProjectRequestError(http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
			}
			if err := recordArtifactDeletionTombstone(tx, row.TaskArtifact, "artifact", now); err != nil {
				return err
			}
			trashed, err := a.artifactStore.moveObjectToTrash(*row.RelativePath, row.ID)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fileMissing = true
				} else {
					return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be moved to controlled trash")
				}
			} else {
				moved = &trashed
			}
		}
		artifactUpdates := map[string]any{
			"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": reason,
		}
		if fileMissing {
			artifactUpdates["integrity_status"] = "missing"
			artifactUpdates["integrity_checked_at"] = now
		}
		result := tx.Model(&models.TaskArtifact{}).Where("id = ? AND deleted_at IS NULL", id).Updates(artifactUpdates)
		if result.Error != nil {
			return mapTaskOutputConstraintError(result.Error)
		}
		if result.RowsAffected == 0 {
			return newProjectRequestError(http.StatusConflict, "ARTIFACT_ALREADY_DELETED", "The Artifact is already deleted")
		}
		result = tx.Model(&models.Task{}).Where("id = ? AND version = ?", task.ID, expectedVersion).Updates(map[string]any{
			"updated_at": now, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		updated, err := loadTask(tx, task.ID)
		if err != nil {
			return err
		}
		deletedRow, err := loadArtifactRow(tx, id)
		if err != nil {
			return err
		}
		deletedArtifact, err := artifactSummaryFromRow(deletedRow)
		if err != nil {
			return err
		}
		sequenceValue := 1
		event, err := recordTaskOutputEvent(tx, "task_artifact_deleted", task.ID, &row.SubmissionID, &row.ID,
			map[string]any{"artifact": artifactEventSnapshotFromRow(row), "version": task.Version},
			map[string]any{"artifact_id": row.ID, "deleted_at": now, "reason": reason, "version": updated.Version},
			requestIDFromContext(c), now, sequenceValue)
		if err != nil {
			return err
		}
		response = deleteTaskArtifactResponse{Task: updated, Artifact: deletedArtifact, Event: event}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, task.ID, requestHash, http.StatusOK, response, now)
	})
	if err != nil {
		if moved != nil {
			if restoreErr := a.artifactStore.restoreTrashedFile(*moved); restoreErr != nil && a.options.Logger != nil {
				a.options.Logger.Printf("Artifact delete compensation failed artifact_id=%s", id)
			}
		}
		writeTaskOutputError(c, err)
		return
	}
	if moved != nil {
		a.artifactStore.purgeTrashedFile(*moved)
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) prepareSubmitOutputRequest(c *gin.Context) (submitOutputRequest, []preparedArtifact, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json or multipart/form-data")
	}
	requestLimit := int64(maxJSONBodyBytes)
	requestLimitMessage := "JSON request bodies cannot exceed 1 MiB"
	switch mediaType {
	case "application/json":
	case "multipart/form-data":
		requestLimit = maxArtifactRequestBytes
		requestLimitMessage = "Task output requests cannot exceed 100 MiB"
	default:
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json or multipart/form-data")
	}
	if c.Request.ContentLength > requestLimit {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", requestLimitMessage)
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestLimit)
	var input submitOutputRequest
	var multipartReader *multipart.Reader
	switch mediaType {
	case "application/json":
		if err := decodeStrictJSONReader(c.Request.Body, &input); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return submitOutputRequest{}, nil, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", requestLimitMessage)
			}
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		}
	case "multipart/form-data":
		multipartReader, err = c.Request.MultipartReader()
		if err != nil {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
		}
		input, err = readSubmitOutputManifest(multipartReader)
		if err != nil {
			return submitOutputRequest{}, nil, err
		}
	}
	input.Summary = strings.TrimSpace(input.Summary)
	if utf8.RuneCountInString(input.Summary) > 10_000 {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "summary cannot exceed 10000 characters")
	}
	if len(input.Artifacts) > maxArtifactsPerSubmission {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "a submission cannot contain more than 20 Artifacts")
	}
	if input.Summary == "" && len(input.Artifacts) == 0 {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "summary or at least one Artifact is required")
	}
	prepared := make([]preparedArtifact, 0, len(input.Artifacts))
	cleanup := true
	defer func() {
		if !cleanup || a.artifactStore == nil {
			return
		}
		for _, artifact := range prepared {
			if artifact.StagedFile != nil {
				a.artifactStore.discardStagedFile(*artifact.StagedFile)
			}
		}
	}()
	usedFileFields := make(map[string]struct{})
	usedClientRefs := make(map[string]struct{})
	for index, artifact := range input.Artifacts {
		artifact.ClientRef = strings.TrimSpace(artifact.ClientRef)
		if utf8.RuneCountInString(artifact.ClientRef) < 1 || utf8.RuneCountInString(artifact.ClientRef) > 100 || hasUnsafeControlCharacters(artifact.ClientRef) {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "client_ref must contain 1 to 100 safe characters")
		}
		if _, exists := usedClientRefs[artifact.ClientRef]; exists {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "client_ref values must be unique within a submission")
		}
		usedClientRefs[artifact.ClientRef] = struct{}{}
		value, err := a.prepareArtifactInput(artifact, index+1, usedFileFields, mediaType == "multipart/form-data")
		if err != nil {
			return submitOutputRequest{}, nil, err
		}
		prepared = append(prepared, value)
	}
	if multipartReader != nil {
		if err := a.stageSubmitOutputFiles(multipartReader, prepared); err != nil {
			return submitOutputRequest{}, nil, err
		}
	}
	cleanup = false
	return input, prepared, nil
}

func readSubmitOutputManifest(reader *multipart.Reader) (submitOutputRequest, error) {
	part, err := reader.NextPart()
	if err != nil {
		return submitOutputRequest{}, multipartRequestReadError(err)
	}
	if part.FormName() != "manifest" || part.FileName() != "" {
		return submitOutputRequest{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The manifest text field must be the first multipart part")
	}
	manifest, readErr := io.ReadAll(io.LimitReader(part, int64(maxArtifactManifestBytes)+1))
	if readErr != nil {
		return submitOutputRequest{}, multipartRequestReadError(readErr)
	}
	if len(manifest) > maxArtifactManifestBytes {
		return submitOutputRequest{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Multipart manifests cannot exceed 1 MiB")
	}
	if err := part.Close(); err != nil {
		return submitOutputRequest{}, multipartRequestReadError(err)
	}
	var input submitOutputRequest
	if err := decodeStrictJSONBytes(manifest, &input); err != nil {
		return submitOutputRequest{}, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The multipart manifest is not valid JSON")
	}
	return input, nil
}

func (a *API) stageSubmitOutputFiles(reader *multipart.Reader, prepared []preparedArtifact) error {
	expected := make(map[string]int)
	for index := range prepared {
		if prepared[index].FileField != "" {
			expected[prepared[index].FileField] = index
		}
	}
	seen := make(map[string]struct{}, len(expected))
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return multipartRequestReadError(err)
		}
		field := part.FormName()
		if field == "manifest" {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one manifest field is allowed")
		}
		if field == "" || part.FileName() == "" {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Only referenced file parts are allowed after the manifest")
		}
		index, ok := expected[field]
		if !ok {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every uploaded file must be referenced exactly once by manifest file_field")
		}
		if _, duplicate := seen[field]; duplicate {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every uploaded file must be referenced exactly once by manifest file_field")
		}
		seen[field] = struct{}{}
		staged, stageErr := a.artifactStore.stageMultipartFile(part, prepared[index].ID)
		if stageErr != nil {
			return multipartFileStageError(stageErr)
		}
		prepared[index].StagedFile = &staged
		prepared[index].RelativePath = &staged.relativePath
		prepared[index].MimeType = &staged.mimeType
		prepared[index].SizeBytes = &staged.sizeBytes
		prepared[index].SHA256 = &staged.sha256
		if err := part.Close(); err != nil {
			return multipartRequestReadError(err)
		}
	}
	if len(seen) != len(expected) {
		return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every manifest file_field must reference exactly one uploaded file")
	}
	return nil
}

func multipartFileStageError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Task output requests cannot exceed 100 MiB")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	if errors.Is(err, errArtifactFileTooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "ARTIFACT_FILE_TOO_LARGE", "Each Artifact file must not exceed 50 MiB")
	}
	if errors.Is(err, errArtifactFileEmpty) {
		return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact files must contain at least one byte")
	}
	return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be staged")
}

func multipartRequestReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Task output requests cannot exceed 100 MiB")
	}
	return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
}

func (a *API) prepareArtifactInput(
	input submitArtifactInput,
	position int,
	usedFileFields map[string]struct{},
	isMultipart bool,
) (preparedArtifact, error) {
	input.StorageKind = strings.TrimSpace(input.StorageKind)
	input.Name = strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 255 || hasUnsafeControlCharacters(input.Name) {
		return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact name must contain 1 to 255 safe characters")
	}
	artifact := preparedArtifact{
		ID: uuid.NewString(), ClientRef: input.ClientRef, Position: position, StorageKind: input.StorageKind,
		Name: input.Name, RequiresFollowup: input.RequiresFollowup,
	}
	switch input.StorageKind {
	case "text":
		if input.ContentText == nil || strings.TrimSpace(*input.ContentText) == "" {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "text Artifacts require non-empty content_text")
		}
		if utf8.RuneCountInString(*input.ContentText) > maxArtifactTextCharacters {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "content_text cannot exceed 500000 characters")
		}
		if input.ReferenceURL != nil || len(input.StructuredJSON) != 0 || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		artifact.ContentText = input.ContentText
	case "link":
		if input.ReferenceURL == nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "link Artifacts require reference_url")
		}
		reference := strings.TrimSpace(*input.ReferenceURL)
		parsed, err := url.Parse(reference)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reference_url must be an http or https URL without credentials")
		}
		if len(reference) > 4096 {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reference_url cannot exceed 4096 bytes")
		}
		if input.ContentText != nil || len(input.StructuredJSON) != 0 || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		artifact.ReferenceURL = &reference
	case "structured":
		if len(input.StructuredJSON) == 0 {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "structured Artifacts require structured_json")
		}
		if len(input.StructuredJSON) > maxArtifactManifestBytes {
			return preparedArtifact{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "structured_json cannot exceed 1 MiB")
		}
		canonical, err := canonicalStructuredJSONObject(input.StructuredJSON)
		if err != nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "structured_json must be a JSON object")
		}
		if input.ContentText != nil || input.ReferenceURL != nil || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		if len(canonical) > maxArtifactManifestBytes {
			return preparedArtifact{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "structured_json cannot exceed 1 MiB")
		}
		value := string(canonical)
		artifact.StructuredJSON = &value
	case "file":
		if !isMultipart {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file Artifacts require multipart/form-data")
		}
		if input.FileField == nil || strings.TrimSpace(*input.FileField) == "" {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file Artifacts require file_field")
		}
		if input.ContentText != nil || input.ReferenceURL != nil || len(input.StructuredJSON) != 0 {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		field := strings.TrimSpace(*input.FileField)
		if _, exists := usedFileFields[field]; exists {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file_field values must be unique")
		}
		usedFileFields[field] = struct{}{}
		artifact.FileField = field
	default:
		return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "storage_kind must be text, file, link, or structured")
	}
	return artifact, nil
}

func hasUnsafeControlCharacters(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func canonicalStructuredJSONObject(raw []byte) (json.RawMessage, error) {
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("structured JSON is not an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("structured JSON contains multiple values")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func artifactPayloadConflict() error {
	return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact payload fields must match storage_kind exactly")
}

func decodeStrictJSONBytes(data []byte, destination any) error {
	return decodeStrictJSONReader(strings.NewReader(string(data)), destination)
}

func decodeStrictJSONReader(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func submitOutputRequestHash(expectedVersion int64, summary string, artifacts []preparedArtifact) (string, error) {
	type hashArtifact struct {
		ClientRef        string  `json:"client_ref"`
		Position         int     `json:"position"`
		StorageKind      string  `json:"storage_kind"`
		Name             string  `json:"name"`
		ContentText      *string `json:"content_text"`
		ReferenceURL     *string `json:"reference_url"`
		StructuredJSON   *string `json:"structured_json"`
		MimeType         *string `json:"mime_type"`
		SizeBytes        *int64  `json:"size_bytes"`
		SHA256           *string `json:"sha256"`
		RequiresFollowup bool    `json:"requires_followup"`
	}
	values := make([]hashArtifact, len(artifacts))
	for index, artifact := range artifacts {
		values[index] = hashArtifact{
			ClientRef: artifact.ClientRef, Position: artifact.Position, StorageKind: artifact.StorageKind, Name: artifact.Name,
			ContentText: artifact.ContentText, ReferenceURL: artifact.ReferenceURL,
			StructuredJSON: artifact.StructuredJSON, MimeType: artifact.MimeType,
			SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256, RequiresFollowup: artifact.RequiresFollowup,
		}
	}
	payload := struct {
		ExpectedVersion int64          `json:"expected_version"`
		Summary         string         `json:"summary"`
		Artifacts       []hashArtifact `json:"artifacts"`
	}{ExpectedVersion: expectedVersion, Summary: summary, Artifacts: values}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "v1:" + hex.EncodeToString(digest[:]), nil
}

func taskOutputCommandIdempotency(c *gin.Context, payload any) (string, string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return "", "", false
	}
	if key == "" {
		return "", "", true
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeDatabaseError(c)
		return "", "", false
	}
	digest := sha256.Sum256(encoded)
	return key, "v1:" + hex.EncodeToString(digest[:]), true
}

func replayTaskOutputCommand(tx *gorm.DB, key, endpoint, requestHash string, response any) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	var existing models.IdempotencyKey
	err := tx.Where("key = ? AND endpoint = ?", key, endpoint).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
	}
	if *existing.RequestHash != requestHash {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different Task output request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, err
	}
	return true, *existing.ResponseStatus, nil
}

func recordTaskOutputIdempotency(tx *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response any, now string) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	text := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID, RequestHash: &requestHash,
		ResponseBody: &text, ResponseStatus: &status, CreatedAt: now,
	}
	return tx.Create(&record).Error
}

type activeTaskAssignee struct {
	ActorID string `gorm:"column:actor_id"`
}

func requireTaskOutputActors(tx *gorm.DB, taskIDValue string) (activeTaskAssignee, error) {
	var assignee activeTaskAssignee
	err := tx.Table("task_assignments AS assignment").Select("assignment.actor_id").
		Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
		Where("assignment.task_id = ? AND assignment.role = 'assignee' AND assignment.unassigned_at IS NULL", taskIDValue).
		Where("actor.status = 'active' AND actor.type IN ('owner', 'person')").Take(&assignee).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activeTaskAssignee{}, newProjectRequestError(http.StatusConflict, "TASK_ASSIGNEE_REQUIRED", "An active assignee is required before submitting output")
	}
	if err != nil {
		return activeTaskAssignee{}, err
	}
	if _, err := requireActiveOwnerReviewer(tx, taskIDValue); err != nil {
		return activeTaskAssignee{}, err
	}
	return assignee, nil
}

func requireActiveOwnerReviewer(tx *gorm.DB, taskIDValue string) (string, error) {
	var actorID string
	err := tx.Table("task_assignments AS assignment").Select("assignment.actor_id").
		Joins("JOIN actors AS actor ON actor.id = assignment.actor_id").
		Where("assignment.task_id = ? AND assignment.role = 'reviewer' AND assignment.unassigned_at IS NULL", taskIDValue).
		Where("actor.id = ? AND actor.type = 'owner' AND actor.status = 'active'", models.BuiltinOwnerActorID).
		Scan(&actorID).Error
	if err != nil {
		return "", err
	}
	if actorID == "" {
		return "", newProjectRequestError(http.StatusConflict, "TASK_REVIEWER_REQUIRED", "An active owner reviewer is required for manual review")
	}
	return actorID, nil
}

func recordTaskOutputEvent(
	tx *gorm.DB,
	action, taskIDValue string,
	submissionID, artifactIDValue *string,
	previous, current map[string]any,
	requestID, createdAt string,
	commandSequence int,
) (taskWorkflowEventOutput, error) {
	var previousJSON *string
	if previous != nil {
		encoded, err := json.Marshal(previous)
		if err != nil {
			return taskWorkflowEventOutput{}, err
		}
		value := string(encoded)
		previousJSON = &value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return taskWorkflowEventOutput{}, err
	}
	currentJSON := string(encoded)
	ownerID := models.BuiltinOwnerActorID
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: taskIDValue, Action: action,
		ActorID: &ownerID, SubmissionID: submissionID, ArtifactID: artifactIDValue,
		RequestID: &requestID, CommandSeq: &commandSequence, PreviousJSON: previousJSON,
		CurrentJSON: &currentJSON, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return taskWorkflowEventOutput{}, err
	}
	return loadTaskWorkflowEvent(tx, event.ID)
}

func artifactEventSnapshots(artifacts []taskArtifactSummary) []map[string]any {
	values := make([]map[string]any, len(artifacts))
	for index, artifact := range artifacts {
		values[index] = map[string]any{
			"id": artifact.ID, "position": artifact.Position, "storage_kind": artifact.StorageKind,
			"name": artifact.Name, "mime_type": artifact.MimeType, "size_bytes": artifact.SizeBytes,
			"sha256": artifact.SHA256, "requires_followup": artifact.RequiresFollowup,
			"produced_by_actor_id": artifact.ProducedByActorID, "recorded_by_actor_id": artifact.RecordedByActorID,
		}
	}
	return values
}

func artifactEventSnapshotFromRow(row taskArtifactRow) map[string]any {
	return map[string]any{
		"id": row.ID, "submission_id": row.SubmissionID, "position": row.Position,
		"storage_kind": row.StorageKind, "name": row.Name, "mime_type": row.MimeType,
		"size_bytes": row.SizeBytes, "sha256": row.SHA256, "requires_followup": row.RequiresFollowup,
		"produced_by_actor_id": row.ProducedByActorID, "recorded_by_actor_id": row.RecordedByActorID,
	}
}

func validateArtifactDeleteReason(value string) (string, error) {
	reason := strings.TrimSpace(value)
	length := utf8.RuneCountInString(reason)
	if length < 1 || length > 1000 {
		return "", errors.New("reason must contain 1 to 1000 characters")
	}
	return reason, nil
}

func mapTaskOutputConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "UNIQUE constraint failed: task_submissions.task_id"):
		return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_ALREADY_PENDING", "The task already has a submission pending review")
	case strings.Contains(message, "TASK_ARTIFACT_SUBMISSION_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "TASK_ARTIFACT_SUBMISSION_MISMATCH", "Artifact and submission must belong to the same Task")
	case strings.Contains(message, "TASK_CURRENT_SUBMISSION_MISMATCH"):
		return newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is invalid")
	case strings.Contains(message, "TASK_SUBMISSION_HISTORY_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "TASK_REVIEW_NOT_ALLOWED", "The submission is no longer pending review")
	case strings.Contains(message, "TASK_ARTIFACT_FACTS_IMMUTABLE"):
		return newProjectRequestError(http.StatusConflict, "ARTIFACT_IMMUTABLE", "Artifact facts cannot be changed")
	default:
		return mapTaskWorkflowConstraintError(err)
	}
}

func writeTaskOutputError(c *gin.Context, err error) {
	err = mapTaskOutputConstraintError(err)
	if writeProjectRequestError(c, err) {
		return
	}
	writeDatabaseError(c)
}

func taskHasPendingReview(task models.Task) bool {
	return task.Status == "waiting_review" ||
		(task.Status == "blocked" && task.BlockedFromStatus != nil && *task.BlockedFromStatus == "waiting_review")
}

func withdrawCurrentSubmissionForCancellation(
	tx *gorm.DB,
	task models.Task,
	requestID, now, reason string,
	commandSequence int,
) (int, error) {
	if task.CurrentSubmissionID == nil {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The pending Task submission is unavailable")
	}
	var submission models.TaskSubmission
	if err := tx.First(&submission, "id = ? AND task_id = ?", *task.CurrentSubmissionID, task.ID).Error; err != nil {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The pending Task submission is unavailable")
	}
	if submission.Status != "pending_review" {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is not pending review")
	}
	result := tx.Model(&models.TaskSubmission{}).
		Where("id = ? AND task_id = ? AND status = 'pending_review'", submission.ID, task.ID).
		Updates(map[string]any{
			"status": "withdrawn", "withdrawn_by_actor_id": models.BuiltinOwnerActorID, "withdrawn_at": now,
		})
	if result.Error != nil {
		return commandSequence, mapTaskOutputConstraintError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commandSequence, newProjectRequestError(http.StatusConflict, "TASK_SUBMISSION_INVALID", "The current Task submission is no longer pending review")
	}
	eventSequence := commandSequence
	_, err := recordTaskOutputEvent(
		tx, "task_submission_withdrawn", task.ID, &submission.ID, nil,
		map[string]any{"submission_id": submission.ID, "submission_status": "pending_review"},
		map[string]any{"submission_id": submission.ID, "submission_status": "withdrawn", "reason": reason},
		requestID, now, eventSequence,
	)
	if err != nil {
		return commandSequence, err
	}
	return commandSequence + 1, nil
}

func recordArtifactDeletionTombstone(tx *gorm.DB, artifact models.TaskArtifact, scope, deletedAt string) error {
	if artifact.StorageKind != "file" || artifact.RelativePath == nil || artifact.SizeBytes == nil || artifact.SHA256 == nil {
		return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "A file Artifact has incomplete deletion metadata")
	}
	return tx.Create(&models.ArtifactDeletionTombstone{
		ArtifactID: artifact.ID, TaskID: artifact.TaskID, RelativePath: *artifact.RelativePath,
		SizeBytes: *artifact.SizeBytes, SHA256: *artifact.SHA256, DeletionScope: scope, DeletedAt: deletedAt,
	}).Error
}

func (a *API) trashTaskArtifactFiles(tx *gorm.DB, taskIDValue, deletedAt string) ([]trashedArtifactFile, error) {
	var artifacts []models.TaskArtifact
	if err := tx.Select("id", "task_id", "storage_kind", "relative_path", "size_bytes", "sha256").
		Where("task_id = ? AND storage_kind = 'file' AND deleted_at IS NULL", taskIDValue).
		Order("id ASC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	if a.artifactStore == nil {
		return nil, newProjectRequestError(http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
	}
	moved := make([]trashedArtifactFile, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.RelativePath == nil {
			return moved, newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "A Task Artifact has an invalid controlled path")
		}
		if err := recordArtifactDeletionTombstone(tx, artifact, "task", deletedAt); err != nil {
			return moved, err
		}
		entry, err := a.artifactStore.moveObjectToTrash(*artifact.RelativePath, artifact.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return moved, newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "Task Artifact files could not be prepared for deletion")
		}
		moved = append(moved, entry)
	}
	return moved, nil
}

func (a *API) restoreTaskArtifactFiles(moved []trashedArtifactFile) error {
	if a.artifactStore == nil {
		return nil
	}
	var restoreErr error
	for index := len(moved) - 1; index >= 0; index-- {
		if err := a.artifactStore.restoreTrashedFile(moved[index]); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	return restoreErr
}
