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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
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
	StagedFile       *filestore.StagedArtifactFile
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
	Origin             string                  `json:"origin"`
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
	origin := row.Origin
	if origin == "" {
		origin = taskSubmissionOriginManual
	}
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
		ID: row.ID, TaskID: row.TaskID, Sequence: row.Sequence, Status: row.Status, Origin: origin, Summary: row.Summary,
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
	file, info, err := a.artifactStore.OpenObject(*row.RelativePath)
	if err != nil {
		if errors.Is(err, filestore.ErrArtifactObjectMissing) {
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
