package api

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const agentRunProjectTaskFilesPageSize = 20

func projectTaskFileUnavailable() error {
	return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
		"The selected file is not an active verified file from another Task's current accepted submission in this Project")
}

// This reader establishes live provenance without reading an Artifact body.
// Controlled bytes are read only by freezeAgentRunFiles via the existing store.
func loadAgentRunProjectTaskFileRecord(tx *gorm.DB, targetTask models.Task, id string) (agentRunControlledFileRecord, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || targetTask.ProjectID == nil || *targetTask.ProjectID == "" {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	var artifact models.TaskArtifact
	err = tx.Select("id", "task_id", "submission_id", "storage_kind", "name", "relative_path", "mime_type", "size_bytes", "sha256", "integrity_status", "deleted_at").
		First(&artifact, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	if err != nil {
		return agentRunControlledFileRecord{}, err
	}
	if artifact.TaskID == targetTask.ID || artifact.DeletedAt != nil || artifact.StorageKind != "file" ||
		artifact.IntegrityStatus != "verified" || artifact.RelativePath == nil || artifact.MimeType == nil ||
		artifact.SizeBytes == nil || artifact.SHA256 == nil {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	var sourceTask models.Task
	err = tx.Select("id", "title", "version", "project_id", "status", "current_submission_id").First(&sourceTask, "id = ?", artifact.TaskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	if err != nil {
		return agentRunControlledFileRecord{}, err
	}
	if sourceTask.ProjectID == nil || *sourceTask.ProjectID != *targetTask.ProjectID || sourceTask.Status != "done" ||
		sourceTask.CurrentSubmissionID == nil || *sourceTask.CurrentSubmissionID != artifact.SubmissionID {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	var submission models.TaskSubmission
	err = tx.Select("id", "task_id", "sequence", "status").First(&submission, "id = ?", artifact.SubmissionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	if err != nil {
		return agentRunControlledFileRecord{}, err
	}
	if submission.TaskID != sourceTask.ID || submission.Status != "accepted" {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	source := &agentexec.ProjectTaskFileSource{
		ProjectID: *sourceTask.ProjectID, TaskID: sourceTask.ID, TaskTitle: sourceTask.Title, TaskVersion: sourceTask.Version,
		SubmissionID: submission.ID, SubmissionSequence: submission.Sequence,
	}
	mimeType := canonicalAgentRunStoredTextMIME(artifact.Name, *artifact.MimeType)
	if agentexec.ValidateProjectTaskFileSource(*source) != nil || *artifact.RelativePath != "objects/"+artifact.ID ||
		agentexec.ValidateTextFileNameAndMIME(artifact.Name, mimeType) != nil ||
		len(*artifact.SHA256) != 64 || strings.ToLower(*artifact.SHA256) != *artifact.SHA256 {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	if _, err := hex.DecodeString(*artifact.SHA256); err != nil {
		return agentRunControlledFileRecord{}, projectTaskFileUnavailable()
	}
	if *artifact.SizeBytes <= 0 || *artifact.SizeBytes > int64(agentexec.MaxFileInputBytes) {
		return agentRunControlledFileRecord{}, newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
			"The selected file exceeds the controlled input size limit")
	}
	return agentRunControlledFileRecord{RelativePath: *artifact.RelativePath, Reference: frozenAgentRunFileReference{
		ID: artifact.ID, SourceKind: agentexec.FileSourceProjectTaskArtifact, Name: artifact.Name, MIME: mimeType,
		SizeBytes: int(*artifact.SizeBytes), SHA256: *artifact.SHA256, SourceTask: source,
	}}, nil
}

func parseAgentRunProjectTaskFilesQuery(raw string) (int, string, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return 0, "", err
	}
	for key, entries := range values {
		if key != "offset" && key != "query" || len(entries) != 1 {
			return 0, "", errors.New("project task file query fields are invalid")
		}
	}
	offset := 0
	if values.Has("offset") {
		rawOffset := values.Get("offset")
		var err error
		offset, err = strconv.Atoi(rawOffset)
		if err != nil || offset < 0 || strconv.Itoa(offset) != rawOffset {
			return 0, "", errors.New("project task file offset must be a canonical nonnegative integer")
		}
	}
	query := strings.TrimSpace(values.Get("query"))
	if !utf8.ValidString(query) || strings.ContainsRune(query, '\x00') || utf8.RuneCountInString(query) > 200 {
		return 0, "", errors.New("project task file query must be bounded UTF-8 metadata")
	}
	return offset, query, nil
}

func (a *API) listAgentRunProjectTaskFiles(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	offset, query, err := parseAgentRunProjectTaskFilesQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, agentRunFileInvalidCode, "Invalid project task file query")
		return
	}
	candidates := make([]agentRunFileCandidate, 0)
	var total int64
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		var err error
		candidates, total, err = a.loadAgentRunProjectTaskFileCandidates(tx, task, offset, query)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	var nextOffset *int
	if len(candidates) > 0 && int64(offset) < total-int64(len(candidates)) {
		next := offset + len(candidates)
		nextOffset = &next
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": candidates, "meta": gin.H{"offset": offset, "limit": agentRunProjectTaskFilesPageSize, "total": total, "next_offset": nextOffset}})
}

func (a *API) loadAgentRunProjectTaskFileCandidates(tx *gorm.DB, task models.Task, offset int, query string) ([]agentRunFileCandidate, int64, error) {
	candidates := make([]agentRunFileCandidate, 0)
	var total int64
	if offset < 0 || !utf8.ValidString(query) || strings.ContainsRune(query, '\x00') || utf8.RuneCountInString(query) > 200 {
		return nil, 0, newProjectRequestError(422, agentRunFileInvalidCode, "Invalid project task file query")
	}
	if task.ProjectID == nil || *task.ProjectID == "" {
		return candidates, 0, nil
	}
	base := tx.Table("task_artifacts AS artifact").
		Joins("JOIN tasks AS source_task ON source_task.id = artifact.task_id AND source_task.current_submission_id = artifact.submission_id").
		Joins("JOIN task_submissions AS submission ON submission.id = artifact.submission_id AND submission.task_id = source_task.id").
		Where("source_task.project_id = ? AND source_task.id <> ? AND source_task.status = 'done' AND submission.status = 'accepted' AND artifact.storage_kind = 'file' AND artifact.deleted_at IS NULL", *task.ProjectID, task.ID)
	if query != "" {
		// instr is literal substring matching: '%' and '_' never become
		// wildcard expansion into other project metadata.
		base = base.Where("instr(lower(source_task.title), lower(?)) > 0 OR instr(lower(artifact.name), lower(?)) > 0", query, query)
	}
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		models.TaskArtifact
		SourceProjectID          string
		SourceTaskTitle          string
		SourceTaskVersion        int64
		SourceSubmissionSequence int
	}
	if err := base.Select("artifact.id, artifact.task_id, artifact.submission_id, artifact.name, artifact.mime_type, artifact.size_bytes, artifact.sha256, artifact.created_at, source_task.project_id AS source_project_id, source_task.title AS source_task_title, source_task.version AS source_task_version, submission.sequence AS source_submission_sequence").
		Order("artifact.created_at DESC").Order("artifact.id DESC").Offset(offset).Limit(agentRunProjectTaskFilesPageSize).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		candidate := agentRunFileCandidate{
			ID: row.ID, SourceKind: agentexec.FileSourceProjectTaskArtifact, Name: row.Name, CreatedAt: row.CreatedAt,
			SourceTask: &agentexec.ProjectTaskFileSource{ProjectID: row.SourceProjectID, TaskID: row.TaskID, TaskTitle: row.SourceTaskTitle,
				TaskVersion: row.SourceTaskVersion, SubmissionID: row.SubmissionID, SubmissionSequence: row.SourceSubmissionSequence},
		}
		if row.MimeType != nil {
			candidate.MIME = canonicalAgentRunStoredTextMIME(row.Name, *row.MimeType)
		}
		if row.SizeBytes != nil {
			candidate.SizeBytes = *row.SizeBytes
		}
		if row.SHA256 != nil {
			candidate.SHA256 = *row.SHA256
		}
		candidate.Eligible, candidate.ErrorCode = a.agentRunCandidateEligibility(tx, task, candidate.SourceKind, candidate.ID)
		if err := tx.Statement.Context.Err(); err != nil {
			return nil, 0, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, total, nil
}

type agentRunProjectTaskFilesPreviewRequest struct {
	InputFiles []agentRunInputFileRequest `json:"input_files"`
}

func (request *agentRunProjectTaskFilesPreviewRequest) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("project task file preview must be UTF-8")
	}
	fields, err := strictAgentRestartObject(raw, "input_files")
	if err != nil || len(fields) != 1 {
		return errors.New("project task file preview requires only input_files")
	}
	var rawFiles []json.RawMessage
	if err := json.Unmarshal(fields["input_files"], &rawFiles); err != nil {
		return err
	}
	if len(rawFiles) < 1 || len(rawFiles) > agentexec.MaxFileInputs {
		return errors.New("project task file preview requires one to four selected files")
	}
	files := make([]agentRunInputFileRequest, len(rawFiles))
	for index, rawFile := range rawFiles {
		fields, err := strictAgentRestartObject(rawFile, "id", "source_kind", "source_task", "sha256")
		if err != nil || len(fields) != 4 {
			return errors.New("project task file preview requires exact selected evidence")
		}
		if err := json.Unmarshal(rawFile, &files[index]); err != nil {
			return err
		}
		if files[index].SourceKind != agentexec.FileSourceProjectTaskArtifact {
			return errors.New("project task file preview accepts only project_task_artifact")
		}
	}
	if err := validateAgentRunInputFileRequests(files); err != nil {
		return err
	}
	request.InputFiles = files
	return nil
}

func (a *API) previewAgentRunProjectTaskFiles(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	var input agentRunProjectTaskFilesPreviewRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The selected project task file preview request is invalid")
		return
	}
	var references []frozenAgentRunFileReference
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		var err error
		references, _, err = freezeAgentRunFiles(tx, task, input.InputFiles, a.artifactStore)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": references})
}
