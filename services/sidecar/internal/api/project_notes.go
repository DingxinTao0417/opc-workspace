package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type createProjectNoteRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	OccurredAt string `json:"occurred_at"`
}

type updateProjectNoteRequest struct {
	Title      nullableStringPatch `json:"title"`
	Body       nullableStringPatch `json:"body"`
	OccurredAt nullableStringPatch `json:"occurred_at"`
}

type deleteProjectNoteRequest struct {
	Reason string `json:"reason"`
}

type projectNoteActorResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
}

type projectNoteResponse struct {
	ID               string                   `json:"id"`
	ProjectID        string                   `json:"project_id"`
	Title            string                   `json:"title"`
	Body             *string                  `json:"body"`
	OccurredAt       string                   `json:"occurred_at"`
	CreatedBy        projectNoteActorResponse `json:"created_by"`
	Version          int64                    `json:"version"`
	DeletedAt        *string                  `json:"deleted_at"`
	DeletedByActorID *string                  `json:"deleted_by_actor_id"`
	DeleteReason     *string                  `json:"delete_reason"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
	ProjectVersion   int64                    `json:"project_version"`
}

type projectNoteRow struct {
	models.ProjectNote `gorm:"embedded"`
	CreatedByType      string `gorm:"column:created_by_type"`
	CreatedByName      string `gorm:"column:created_by_display_name"`
	ProjectVersion     int64  `gorm:"column:project_version"`
}

func (a *API) listProjectNotes(c *gin.Context) {
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
	includeDeleted, valid := parseClientActivityIncludeDeleted(c.Query("include_deleted"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", "include_deleted must be true or false")
		return
	}

	var total int64
	var rows []projectNoteRow
	var projectVersion int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT version FROM projects WHERE id = ?", projectIDValue).Row().Scan(&projectVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		query := tx.Table("project_notes").Where("project_notes.project_id = ?", projectIDValue)
		if !includeDeleted {
			query = query.Where("project_notes.deleted_at IS NULL")
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Select(projectNoteSelectColumns).
			Joins("JOIN actors created_by ON created_by.id = project_notes.created_by_actor_id").
			Joins("JOIN projects ON projects.id = project_notes.project_id").
			Order("project_notes.occurred_at DESC").
			Order("project_notes.id ASC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	items := make([]projectNoteResponse, len(rows))
	for index := range rows {
		items[index] = projectNoteResponseFromRow(rows[index])
	}
	setProjectETag(c, projectVersion)
	c.JSON(http.StatusOK, gin.H{
		"data": items,
		"meta": gin.H{
			"page": page, "page_size": pageSize, "total": total,
			"project_version": projectVersion,
		},
	})
}

func (a *API) createProjectNote(c *gin.Context) {
	projectIDValue, ok := projectID(c)
	if !ok {
		return
	}
	var input createProjectNoteRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	note, err := a.projectNoteFromCreateRequest(projectIDValue, input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	endpoint := "POST /api/v1/projects/" + projectIDValue + "/notes"
	requestHash := ""
	if idempotencyKey != "" {
		requestHash, err = projectNoteCreateRequestHash(note)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	statusCode := http.StatusCreated
	replayed := false
	var response projectNoteResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, endpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
				}
				if *existing.RequestHash != requestHash {
					return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different project note request")
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent project note response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read project note idempotency key: %w", err)
			}
		}
		var status string
		if err := tx.Raw("SELECT status FROM projects WHERE id = ?", projectIDValue).Row().Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		if status == "archived" {
			return newProjectRequestError(http.StatusConflict, "PROJECT_ARCHIVED", "Archived projects are read-only")
		}
		if err := tx.Create(&note).Error; err != nil {
			return fmt.Errorf("create project note: %w", err)
		}
		row, err := loadProjectNoteRow(tx, note.ID)
		if err != nil {
			return err
		}
		response = projectNoteResponseFromRow(row)
		if idempotencyKey != "" {
			encoded, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent project note response: %w", err)
			}
			body := string(encoded)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: endpoint, ResourceID: note.ID,
				RequestHash: &requestHash, ResponseBody: &body, ResponseStatus: &responseStatus,
				CreatedAt: note.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record project note idempotency key: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getProjectNote(c *gin.Context) {
	id, ok := projectNoteID(c)
	if !ok {
		return
	}
	row, err := loadProjectNoteRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_NOTE_NOT_FOUND", "Project note not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := projectNoteResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateProjectNote(c *gin.Context) {
	id, ok := projectNoteID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateProjectNoteRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	updates, err := a.projectNoteUpdates(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	var response projectNoteResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var note models.ProjectNote
		if err := tx.First(&note, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOTE_NOT_FOUND", "Project note not found")
			}
			return err
		}
		if note.Version != expectedVersion {
			return projectNoteVersionConflict()
		}
		if note.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "PROJECT_NOTE_DELETED", "Deleted project notes cannot be changed")
		}
		if err := requireMutableProject(tx, note.ProjectID); err != nil {
			return err
		}
		updates["updated_at"] = a.options.Now().UTC().Format(time.RFC3339Nano)
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.ProjectNote{}).Where("id = ? AND version = ? AND deleted_at IS NULL", id, expectedVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return projectNoteVersionConflict()
		}
		row, err := loadProjectNoteRow(tx, id)
		if err != nil {
			return err
		}
		response = projectNoteResponseFromRow(row)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteProjectNote(c *gin.Context) {
	id, ok := projectNoteID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Project note deletion requires confirm=true")
		return
	}
	var input deleteProjectNoteRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	reason, err := cleanClientActivityText(input.Reason, "reason", 1_000, true)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	var response projectNoteResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var note models.ProjectNote
		if err := tx.First(&note, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOTE_NOT_FOUND", "Project note not found")
			}
			return err
		}
		if note.Version != expectedVersion {
			return projectNoteVersionConflict()
		}
		if note.DeletedAt != nil {
			return newProjectRequestError(http.StatusConflict, "PROJECT_NOTE_DELETED", "Project note is already deleted")
		}
		if err := requireMutableProject(tx, note.ProjectID); err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.ProjectNote{}).
			Where("id = ? AND version = ? AND deleted_at IS NULL", id, expectedVersion).
			Updates(map[string]any{
				"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID,
				"delete_reason": reason, "updated_at": now, "version": gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return projectNoteVersionConflict()
		}
		row, err := loadProjectNoteRow(tx, id)
		if err != nil {
			return err
		}
		response = projectNoteResponseFromRow(row)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) projectNoteFromCreateRequest(projectIDValue string, input createProjectNoteRequest) (models.ProjectNote, error) {
	title, err := cleanClientActivityText(input.Title, "title", 200, false)
	if err != nil {
		return models.ProjectNote{}, err
	}
	body, err := cleanClientActivityText(input.Body, "body", 10_000, true)
	if err != nil {
		return models.ProjectNote{}, err
	}
	occurredAt, err := cleanClientActivityOccurredAt(input.OccurredAt, a.options.Now())
	if err != nil {
		return models.ProjectNote{}, err
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	return models.ProjectNote{
		ID: uuid.NewString(), ProjectID: projectIDValue, Title: title, Body: body,
		OccurredAt: occurredAt, CreatedByActorID: models.BuiltinOwnerActorID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (a *API) projectNoteUpdates(input updateProjectNoteRequest) (map[string]any, error) {
	updates := make(map[string]any)
	for _, field := range []struct {
		name       string
		patch      nullableStringPatch
		maxRunes   int
		allowLines bool
	}{
		{name: "title", patch: input.Title, maxRunes: 200},
		{name: "body", patch: input.Body, maxRunes: 10_000, allowLines: true},
	} {
		if !field.patch.Set {
			continue
		}
		if field.patch.Value == nil {
			return nil, fmt.Errorf("%s cannot be null", field.name)
		}
		value, err := cleanClientActivityText(*field.patch.Value, field.name, field.maxRunes, field.allowLines)
		if err != nil {
			return nil, err
		}
		updates[field.name] = value
	}
	if input.OccurredAt.Set {
		if input.OccurredAt.Value == nil {
			return nil, errors.New("occurred_at cannot be null")
		}
		occurredAt, err := cleanClientActivityOccurredAt(*input.OccurredAt.Value, a.options.Now())
		if err != nil {
			return nil, err
		}
		updates["occurred_at"] = occurredAt
	}
	if len(updates) == 0 {
		return nil, errors.New("at least one editable project note field is required")
	}
	return updates, nil
}

func requireMutableProject(tx *gorm.DB, projectIDValue string) error {
	var status string
	if err := tx.Raw("SELECT status FROM projects WHERE id = ?", projectIDValue).Row().Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
		}
		return err
	}
	if status == "archived" {
		return newProjectRequestError(http.StatusConflict, "PROJECT_ARCHIVED", "Archived projects are read-only")
	}
	return nil
}

func projectNoteCreateRequestHash(note models.ProjectNote) (string, error) {
	payload := struct {
		ProjectID  string `json:"project_id"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		OccurredAt string `json:"occurred_at"`
	}{ProjectID: note.ProjectID, Title: note.Title, Body: note.Body, OccurredAt: note.OccurredAt}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func projectNoteID(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_PROJECT_NOTE_ID", "Project note id must be a UUID")
		return "", false
	}
	return parsed.String(), true
}

func projectNoteVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Project note has changed; reload it before retrying")
}

const projectNoteSelectColumns = `
	project_notes.id,
	project_notes.project_id,
	project_notes.title,
	project_notes.body,
	project_notes.occurred_at,
	project_notes.created_by_actor_id,
	project_notes.version,
	project_notes.deleted_at,
	project_notes.deleted_by_actor_id,
	project_notes.delete_reason,
	project_notes.created_at,
	project_notes.updated_at,
	created_by.type AS created_by_type,
	created_by.display_name AS created_by_display_name,
	projects.version AS project_version
`

func loadProjectNoteRow(db *gorm.DB, id string) (projectNoteRow, error) {
	var row projectNoteRow
	err := db.Table("project_notes").Select(projectNoteSelectColumns).
		Joins("JOIN actors created_by ON created_by.id = project_notes.created_by_actor_id").
		Joins("JOIN projects ON projects.id = project_notes.project_id").
		Where("project_notes.id = ?", id).Take(&row).Error
	return row, err
}

func projectNoteResponseFromRow(row projectNoteRow) projectNoteResponse {
	body := &row.Body
	if row.DeletedAt != nil {
		body = nil
	}
	response := projectNoteResponse{
		ID: row.ID, ProjectID: row.ProjectID, Title: row.Title, Body: body,
		OccurredAt: normalizeTimestamp(row.OccurredAt),
		CreatedBy: projectNoteActorResponse{
			ID: row.CreatedByActorID, Type: row.CreatedByType, DisplayName: row.CreatedByName,
		},
		Version: row.Version, DeletedAt: row.DeletedAt, DeletedByActorID: row.DeletedByActorID,
		DeleteReason: row.DeleteReason, CreatedAt: normalizeTimestamp(row.CreatedAt),
		UpdatedAt: normalizeTimestamp(row.UpdatedAt), ProjectVersion: row.ProjectVersion,
	}
	if response.DeletedAt != nil {
		normalized := normalizeTimestamp(*response.DeletedAt)
		response.DeletedAt = &normalized
	}
	return response
}
