package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createProjectEndpoint = "POST /api/v1/projects"

var (
	validProjectStatuses = map[string]struct{}{
		"planning": {}, "in_progress": {}, "paused": {}, "completed": {}, "archived": {},
	}
	projectColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

type createProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	ClientID    *string `json:"client_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
	AmountMinor *int64  `json:"amount_minor"`
	Color       *string `json:"color"`
}

type nullableInt64Patch struct {
	Set   bool
	Value *int64
}

func (field *nullableInt64Patch) UnmarshalJSON(data []byte) error {
	field.Set = true
	if string(data) == "null" {
		field.Value = nil
		return nil
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	field.Value = &value
	return nil
}

type updateProjectRequest struct {
	Name        nullableStringPatch `json:"name"`
	Description nullableStringPatch `json:"description"`
	ClientID    nullableStringPatch `json:"client_id"`
	StartDate   nullableStringPatch `json:"start_date"`
	DueDate     nullableStringPatch `json:"due_date"`
	AmountMinor nullableInt64Patch  `json:"amount_minor"`
	Color       nullableStringPatch `json:"color"`
}

type transitionProjectRequest struct {
	Action                 string `json:"action"`
	ConfirmIncompleteTasks bool   `json:"confirm_incomplete_tasks"`
}

type projectTaskSummary struct {
	Total           int64 `json:"total"`
	Completed       int64 `json:"completed"`
	InProgress      int64 `json:"in_progress"`
	Remaining       int64 `json:"remaining"`
	ProgressPercent int   `json:"progress_percent"`
	ActualMinutes   int64 `json:"actual_minutes"`
}

type projectResponse struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	ClientID           *string            `json:"client_id"`
	ClientName         *string            `json:"client_name"`
	Status             string             `json:"status"`
	StartDate          *string            `json:"start_date"`
	DueDate            *string            `json:"due_date"`
	AmountMinor        *int64             `json:"amount_minor"`
	Color              *string            `json:"color"`
	Version            int64              `json:"version"`
	ArchivedFromStatus *string            `json:"archived_from_status"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
	TaskSummary        projectTaskSummary `json:"task_summary"`
	InvoiceCount       int64              `json:"invoice_count"`
	AvailableActions   []string           `json:"available_actions"`
}

type projectRow struct {
	models.Project    `gorm:"embedded"`
	ClientName        *string `gorm:"column:client_name"`
	TaskTotal         int64   `gorm:"column:task_total"`
	TaskCompleted     int64   `gorm:"column:task_completed"`
	TaskInProgress    int64   `gorm:"column:task_in_progress"`
	TaskActualMinutes int64   `gorm:"column:task_actual_minutes"`
	InvoiceCount      int64   `gorm:"column:invoice_count"`
}

type deletedProjectResponse struct {
	DeletedID        string `json:"deleted_id"`
	DetachedTasks    int64  `json:"detached_tasks"`
	DetachedInvoices int64  `json:"detached_invoices"`
}

type projectWorkflowEventOutput struct {
	ID        string                  `json:"id"`
	Action    string                  `json:"action"`
	Actor     *assignmentActorSummary `json:"actor"`
	RequestID *string                 `json:"request_id"`
	Previous  map[string]any          `json:"previous"`
	Current   map[string]any          `json:"current"`
	CreatedAt string                  `json:"created_at"`
}

type projectWorkflowEventRow struct {
	ID             string  `gorm:"column:id"`
	Action         string  `gorm:"column:action"`
	ActorID        *string `gorm:"column:actor_id"`
	RequestID      *string `gorm:"column:request_id"`
	PreviousJSON   *string `gorm:"column:previous_json"`
	CurrentJSON    *string `gorm:"column:current_json"`
	CreatedAt      string  `gorm:"column:created_at"`
	ActorType      *string `gorm:"column:actor_type"`
	ActorName      *string `gorm:"column:actor_display_name"`
	ActorStatus    *string `gorm:"column:actor_status"`
	ActorIsBuiltin *bool   `gorm:"column:actor_is_builtin"`
	ActorVersion   *int64  `gorm:"column:actor_version"`
}

type projectWorkflowEventMeta struct {
	Page           int   `json:"page"`
	PageSize       int   `json:"page_size"`
	Total          int64 `json:"total"`
	ProjectVersion int64 `json:"project_version"`
}

type projectRequestError struct {
	status  int
	code    string
	message string
}

func (err *projectRequestError) Error() string { return err.message }

func (a *API) listProjects(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 50, 1, 100)
	if !ok {
		return
	}

	includeArchived, err := optionalBooleanQuery(c, "include_archived")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if _, valid := validProjectStatuses[status]; !valid {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status filter is invalid")
			return
		}
	}
	clientID := ""
	if rawClientID := strings.TrimSpace(c.Query("client_id")); rawClientID != "" {
		parsedClientID, err := uuid.Parse(rawClientID)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "client_id filter must be a UUID")
			return
		}
		clientID = parsedClientID.String()
	}
	search := strings.TrimSpace(c.Query("q"))
	if search != "" {
		if utf8.RuneCountInString(search) > 200 {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "q cannot exceed 200 characters")
			return
		}
	}

	var total int64
	var rows []projectRow
	invalidSort := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Table("projects")
		if status == "" {
			if !includeArchived {
				query = query.Where("projects.status <> ?", "archived")
			}
		} else {
			query = query.Where("projects.status = ?", status)
		}
		if clientID != "" {
			query = query.Where("projects.client_id = ?", clientID)
		}
		if search != "" {
			like := "%" + escapeLike(search) + "%"
			query = query.Where("(projects.name LIKE ? ESCAPE '\\' OR projects.description LIKE ? ESCAPE '\\')", like, like)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyProjectSort(query, c.Query("sort"))
		if !valid {
			invalidSort = true
			return errors.New("invalid project sort")
		}
		return ordered.
			Select(projectSelectColumns).
			Joins("LEFT JOIN clients ON clients.id = projects.client_id").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Scan(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if invalidSort {
		writeError(c, http.StatusBadRequest, "INVALID_SORT", "sort contains an unsupported field")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	projects := make([]projectResponse, len(rows))
	for index := range rows {
		projects[index] = projectResponseFromRow(rows[index])
	}
	c.JSON(http.StatusOK, gin.H{"data": projects, "meta": pageMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) createProject(c *gin.Context) {
	var input createProjectRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	project, err := projectFromCreateRequest(input)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash := ""
	if idempotencyKey != "" {
		requestHash, err = projectCreateRequestHash(project)
		if err != nil {
			writeDatabaseError(c)
			return
		}
	}

	replayed := false
	statusCode := http.StatusCreated
	var response projectResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if idempotencyKey != "" {
			var existing models.IdempotencyKey
			err := tx.Where("key = ? AND endpoint = ?", idempotencyKey, createProjectEndpoint).First(&existing).Error
			if err == nil {
				if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_REPLAY_UNAVAILABLE",
						"This legacy Idempotency-Key cannot be replayed safely; use a new key",
					)
				}
				if *existing.RequestHash != requestHash {
					return newProjectRequestError(
						http.StatusConflict,
						"IDEMPOTENCY_CONFLICT",
						"Idempotency-Key was already used with a different project request",
					)
				}
				if err := json.Unmarshal([]byte(*existing.ResponseBody), &response); err != nil {
					return fmt.Errorf("decode idempotent project response: %w", err)
				}
				statusCode = *existing.ResponseStatus
				replayed = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("read idempotency key: %w", err)
			}
		}
		if project.ClientID != nil {
			if err := requireClient(tx, *project.ClientID); err != nil {
				return err
			}
		}
		if err := tx.Create(&project).Error; err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := recordProjectWorkflowEvent(
			tx,
			project.ID,
			"project_created",
			nil,
			projectEventState(project),
			requestIDFromContext(c),
			project.CreatedAt,
		); err != nil {
			return err
		}
		row, err := loadProjectRow(tx, project.ID)
		if err != nil {
			return fmt.Errorf("load created project: %w", err)
		}
		response = projectResponseFromRow(row)
		if idempotencyKey != "" {
			responseBody, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("encode idempotent project response: %w", err)
			}
			responseText := string(responseBody)
			responseStatus := http.StatusCreated
			record := models.IdempotencyKey{
				Key: idempotencyKey, Endpoint: createProjectEndpoint, ResourceID: project.ID,
				RequestHash: &requestHash, ResponseBody: &responseText, ResponseStatus: &responseStatus,
				CreatedAt: project.CreatedAt,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("record idempotency key: %w", err)
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

func (a *API) getProject(c *gin.Context) {
	id, ok := projectID(c)
	if !ok {
		return
	}
	row, err := loadProjectRow(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	response := projectResponseFromRow(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) listProjectWorkflowEvents(c *gin.Context) {
	id, ok := projectID(c)
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

	var project models.Project
	var rows []projectWorkflowEventRow
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&project, "id = ?", id).Error; err != nil {
			return err
		}
		base := tx.Model(&models.WorkflowEvent{}).
			Where("aggregate_type = 'project' AND aggregate_id = ?", id)
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		return projectWorkflowEventRowsQuery(tx).
			Where("event.aggregate_type = 'project' AND event.aggregate_id = ?", id).
			Order("julianday(event.created_at) DESC").
			Order("event.command_seq DESC").
			Order("event.id DESC").
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	events := make([]projectWorkflowEventOutput, len(rows))
	for index, row := range rows {
		event, err := projectWorkflowEventOutputFromRow(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		events[index] = event
	}
	setProjectETag(c, project.Version)
	c.JSON(http.StatusOK, gin.H{
		"data": events,
		"meta": projectWorkflowEventMeta{
			Page: page, PageSize: pageSize, Total: total, ProjectVersion: project.Version,
		},
	})
}

func (a *API) updateProject(c *gin.Context) {
	id, ok := projectID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateProjectRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}

	var response projectResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var project models.Project
		if err := tx.First(&project, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		if project.Version != expectedVersion {
			return projectVersionConflict()
		}
		if project.Status == "archived" {
			return newProjectRequestError(
				http.StatusConflict,
				"PROJECT_ARCHIVED",
				"Restore the project before editing it",
			)
		}

		updates, err := projectUpdates(tx, project, input)
		if err != nil {
			return err
		}
		nameChanged := false
		if name, exists := updates["name"].(string); exists {
			nameChanged = name != project.Name
		}
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		updates["updated_at"] = updatedAt
		updates["version"] = gorm.Expr("version + 1")
		result := tx.Model(&models.Project{}).
			Where("id = ? AND version = ?", id, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			if strings.Contains(result.Error.Error(), "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES") {
				return newProjectRequestError(
					http.StatusConflict,
					"PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES",
					"Project client cannot be changed while invoices reference this project",
				)
			}
			return result.Error
		}
		if result.RowsAffected == 0 {
			return projectVersionConflict()
		}
		if nameChanged {
			if _, err := bumpTasksForProject(tx, id, updatedAt); err != nil {
				return err
			}
		}
		row, err := loadProjectRow(tx, id)
		if err != nil {
			return err
		}
		if err := recordProjectWorkflowEvent(
			tx,
			id,
			"project_updated",
			projectEventState(project),
			projectEventState(row.Project),
			requestIDFromContext(c),
			updatedAt,
		); err != nil {
			return err
		}
		response = projectResponseFromRow(row)
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

func (a *API) transitionProject(c *gin.Context) {
	id, ok := projectID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input transitionProjectRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.Action = strings.TrimSpace(input.Action)
	if input.Action == "" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "action is required")
		return
	}

	var response projectResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var project models.Project
		if err := tx.First(&project, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		if project.Version != expectedVersion {
			return projectVersionConflict()
		}

		target, archivedFrom, err := projectTransition(project, input.Action)
		if err != nil {
			return err
		}
		var incompleteTaskCount int64
		if input.Action == "complete" {
			if err := tx.Table("tasks").Where("project_id = ? AND status <> ?", id, "done").Count(&incompleteTaskCount).Error; err != nil {
				return err
			}
			if incompleteTaskCount > 0 && !input.ConfirmIncompleteTasks {
				return newProjectRequestError(
					http.StatusConflict,
					"INCOMPLETE_TASKS_CONFIRMATION_REQUIRED",
					fmt.Sprintf("Project has %d incomplete task(s); explicit confirmation is required", incompleteTaskCount),
				)
			}
		}

		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		updates := map[string]any{
			"status":               target,
			"archived_from_status": archivedFrom,
			"updated_at":           updatedAt,
			"version":              gorm.Expr("version + 1"),
		}
		result := tx.Model(&models.Project{}).
			Where("id = ? AND version = ?", id, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return projectVersionConflict()
		}
		row, err := loadProjectRow(tx, id)
		if err != nil {
			return err
		}
		action := map[string]string{
			"start": "project_started", "pause": "project_paused", "resume": "project_resumed",
			"complete": "project_completed", "reopen": "project_reopened",
			"archive": "project_archived", "restore": "project_restored",
		}[input.Action]
		eventID, err := recordProjectWorkflowEventWithID(
			tx,
			id,
			action,
			projectEventState(project),
			projectEventState(row.Project),
			requestIDFromContext(c),
			updatedAt,
		)
		if err != nil {
			return err
		}
		if input.Action == "complete" || input.Action == "reopen" {
			if err := projectClientActivity(tx, row.Project, input.Action, eventID, updatedAt); err != nil {
				return err
			}
		}
		if input.Action == "complete" {
			if err := projectProjectCompletionInboxItem(
				tx,
				row.Project,
				incompleteTaskCount,
				requestIDFromContext(c),
				updatedAt,
			); err != nil {
				return err
			}
			a.executeProjectCompletionAutomationsSafely(tx, eventID, row.Project, updatedAt)
		}
		response = projectResponseFromRow(row)
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

func (a *API) deleteProject(c *gin.Context) {
	id, ok := projectID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if strings.TrimSpace(c.Query("confirm")) != "true" {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED", "Hard deletion requires confirm=true")
		return
	}

	deleted := deletedProjectResponse{DeletedID: id}
	var movedAttachmentFiles []trashedArtifactFile
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var project models.Project
		if err := tx.First(&project, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			}
			return err
		}
		if project.Version != expectedVersion {
			return projectVersionConflict()
		}
		if project.Status != "archived" {
			return newProjectRequestError(http.StatusConflict, "PROJECT_NOT_ARCHIVED", "Only archived projects can be permanently deleted")
		}
		var roadmapMilestoneCount int64
		if err := tx.Table("roadmap_milestone_projects").Where("project_id = ?", id).Count(&roadmapMilestoneCount).Error; err != nil {
			return err
		}
		if roadmapMilestoneCount > 0 {
			return newProjectRequestError(
				http.StatusConflict,
				"PROJECT_ROADMAP_MILESTONES_EXIST",
				"Remove the project's roadmap milestone associations before permanently deleting it",
			)
		}
		var contentItemCount int64
		if err := tx.Table("content_items").Where("project_id = ?", id).Count(&contentItemCount).Error; err != nil {
			return err
		}
		if contentItemCount > 0 {
			return newProjectRequestError(
				http.StatusConflict,
				"PROJECT_CONTENT_ITEMS_EXIST",
				"Remove the project's content item associations before permanently deleting it",
			)
		}
		deletedAt := time.Now().UTC().Format(time.RFC3339Nano)
		if err := coordinateProjectCompletionInboxSourceDeletion(
			tx,
			id,
			requestIDFromContext(c),
			deletedAt,
		); err != nil {
			return err
		}
		var err error
		movedAttachmentFiles, err = a.trashProjectAttachmentFiles(tx, id, deletedAt)
		if err != nil {
			return err
		}
		detachedTasks, err := bumpTasksForProject(tx, id, deletedAt)
		if err != nil {
			return err
		}
		deleted.DetachedTasks = detachedTasks
		if err := tx.Table("invoices").Where("project_id = ?", id).Count(&deleted.DetachedInvoices).Error; err != nil {
			return err
		}
		if err := recordProjectWorkflowEvent(
			tx,
			id,
			"project_deleted",
			projectEventState(project),
			nil,
			requestIDFromContext(c),
			deletedAt,
		); err != nil {
			return err
		}
		result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.Project{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return projectVersionConflict()
		}
		return nil
	})
	if err != nil {
		if restoreErr := a.restoreProjectAttachmentFiles(movedAttachmentFiles); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("restore project attachment files after delete rollback failed project_id=%s error=%v", id, restoreErr)
		}
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if a.artifactStore != nil {
		for _, moved := range movedAttachmentFiles {
			a.artifactStore.purgeTrashedFile(moved)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": deleted})
}

func projectEventState(project models.Project) map[string]any {
	return map[string]any{
		"id": project.ID, "name": project.Name, "description": project.Description,
		"client_id": project.ClientID, "status": project.Status,
		"start_date": project.StartDate, "due_date": project.DueDate,
		"amount_minor": project.AmountMinor, "color": project.Color,
		"version": project.Version, "archived_from_status": project.ArchivedFromStatus,
	}
}

func recordProjectWorkflowEvent(
	tx *gorm.DB,
	projectIDValue,
	action string,
	previous,
	current map[string]any,
	requestID,
	createdAt string,
) error {
	_, err := recordProjectWorkflowEventWithID(
		tx, projectIDValue, action, previous, current, requestID, createdAt,
	)
	return err
}

func recordProjectWorkflowEventWithID(
	tx *gorm.DB,
	projectIDValue,
	action string,
	previous,
	current map[string]any,
	requestID,
	createdAt string,
) (string, error) {
	encode := func(value map[string]any) (*string, error) {
		if value == nil {
			return nil, nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		text := string(encoded)
		return &text, nil
	}
	previousJSON, err := encode(previous)
	if err != nil {
		return "", fmt.Errorf("encode previous project workflow state: %w", err)
	}
	currentJSON, err := encode(current)
	if err != nil {
		return "", fmt.Errorf("encode current project workflow state: %w", err)
	}
	actorID := models.BuiltinOwnerActorID
	commandSequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "project", AggregateID: projectIDValue,
		Action: action, ActorID: &actorID, CommandSeq: &commandSequence,
		PreviousJSON: previousJSON, CurrentJSON: currentJSON, CreatedAt: createdAt,
	}
	if requestID != "" {
		event.RequestID = &requestID
	}
	if err := tx.Create(&event).Error; err != nil {
		return "", fmt.Errorf("record project workflow event: %w", err)
	}
	return event.ID, nil
}

func projectClientActivity(tx *gorm.DB, project models.Project, action, eventID, occurredAt string) error {
	if project.ClientID == nil {
		return nil
	}

	title := ""
	switch action {
	case "complete":
		title = fmt.Sprintf("项目「%s」已完成", project.Name)
	case "reopen":
		title = fmt.Sprintf("项目「%s」已重新打开", project.Name)
	default:
		return fmt.Errorf("unsupported Project client activity action %q", action)
	}

	sourceType := "project_workflow_event"
	sourceID := eventID
	activity := models.ClientActivity{
		ID: uuid.NewString(), ClientID: *project.ClientID, Kind: "system_reference",
		Title: title, OccurredAt: occurredAt, CreatedByActorID: models.BuiltinSystemActorID,
		SourceType: &sourceType, SourceID: &sourceID, Version: 1,
		CreatedAt: occurredAt, UpdatedAt: occurredAt,
	}
	if err := tx.Create(&activity).Error; err != nil {
		return fmt.Errorf("project Client activity projection: %w", err)
	}
	return nil
}

func projectWorkflowEventRowsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("workflow_events AS event").
		Select(`
			event.id,
			event.action,
			event.actor_id,
			event.request_id,
			event.previous_json,
			event.current_json,
			event.created_at,
			actor.type AS actor_type,
			actor.display_name AS actor_display_name,
			actor.status AS actor_status,
			actor.is_builtin AS actor_is_builtin,
			actor.version AS actor_version
		`).
		Joins("LEFT JOIN actors AS actor ON actor.id = event.actor_id")
}

func projectWorkflowEventOutputFromRow(row projectWorkflowEventRow) (projectWorkflowEventOutput, error) {
	previous, err := decodeWorkflowEventObject(row.PreviousJSON)
	if err != nil {
		return projectWorkflowEventOutput{}, err
	}
	current, err := decodeWorkflowEventObject(row.CurrentJSON)
	if err != nil {
		return projectWorkflowEventOutput{}, err
	}
	var actor *assignmentActorSummary
	if row.ActorID != nil {
		if row.ActorType == nil || row.ActorName == nil || row.ActorStatus == nil || row.ActorIsBuiltin == nil || row.ActorVersion == nil {
			return projectWorkflowEventOutput{}, errors.New("project workflow event actor is missing")
		}
		actor = &assignmentActorSummary{
			ID: *row.ActorID, Type: *row.ActorType, DisplayName: *row.ActorName,
			Status: *row.ActorStatus, IsBuiltin: *row.ActorIsBuiltin, Version: *row.ActorVersion,
		}
	}
	return projectWorkflowEventOutput{
		ID: row.ID, Action: row.Action, Actor: actor, RequestID: row.RequestID,
		Previous: previous, Current: current, CreatedAt: normalizeTimestamp(row.CreatedAt),
	}, nil
}

func projectFromCreateRequest(input createProjectRequest) (models.Project, error) {
	name := strings.TrimSpace(input.Name)
	if length := utf8.RuneCountInString(name); length < 2 || length > 100 {
		return models.Project{}, errors.New("name must contain 2 to 100 characters")
	}
	description := ""
	if input.Description != nil {
		if utf8.RuneCountInString(*input.Description) > 10_000 {
			return models.Project{}, errors.New("description cannot exceed 10000 characters")
		}
		description = *input.Description
	}
	if input.ClientID != nil {
		clientID := strings.TrimSpace(*input.ClientID)
		if _, err := uuid.Parse(clientID); err != nil {
			return models.Project{}, errors.New("client_id must be a UUID")
		}
		input.ClientID = &clientID
	}
	if input.StartDate != nil {
		startDate := strings.TrimSpace(*input.StartDate)
		if !validDate(startDate) {
			return models.Project{}, errors.New("start_date must use YYYY-MM-DD")
		}
		input.StartDate = &startDate
	}
	if input.DueDate != nil {
		dueDate := strings.TrimSpace(*input.DueDate)
		if !validDate(dueDate) {
			return models.Project{}, errors.New("due_date must use YYYY-MM-DD")
		}
		input.DueDate = &dueDate
	}
	if input.StartDate != nil && input.DueDate != nil && *input.StartDate > *input.DueDate {
		return models.Project{}, errors.New("start_date cannot be after due_date")
	}
	if input.AmountMinor != nil && *input.AmountMinor < 0 {
		return models.Project{}, errors.New("amount_minor cannot be negative")
	}
	if input.Color != nil {
		color := strings.ToUpper(strings.TrimSpace(*input.Color))
		if !projectColorPattern.MatchString(color) {
			return models.Project{}, errors.New("color must use #RRGGBB")
		}
		input.Color = &color
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return models.Project{
		ID: uuid.NewString(), Name: name, Description: description, ClientID: input.ClientID,
		Status: "planning", StartDate: input.StartDate, DueDate: input.DueDate,
		AmountMinor: input.AmountMinor, Color: input.Color, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func projectCreateRequestHash(project models.Project) (string, error) {
	payload := struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ClientID    *string `json:"client_id"`
		StartDate   *string `json:"start_date"`
		DueDate     *string `json:"due_date"`
		AmountMinor *int64  `json:"amount_minor"`
		Color       *string `json:"color"`
	}{
		Name: project.Name, Description: project.Description, ClientID: project.ClientID,
		StartDate: project.StartDate, DueDate: project.DueDate,
		AmountMinor: project.AmountMinor, Color: project.Color,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode project request hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func projectUpdates(tx *gorm.DB, project models.Project, input updateProjectRequest) (map[string]any, error) {
	updates := make(map[string]any)
	if input.Name.Set {
		if input.Name.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name cannot be null")
		}
		name := strings.TrimSpace(*input.Name.Value)
		if length := utf8.RuneCountInString(name); length < 2 || length > 100 {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "name must contain 2 to 100 characters")
		}
		updates["name"] = name
	}
	if input.Description.Set {
		if input.Description.Value == nil {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "description cannot be null")
		}
		if utf8.RuneCountInString(*input.Description.Value) > 10_000 {
			return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "description cannot exceed 10000 characters")
		}
		updates["description"] = *input.Description.Value
	}
	if input.ClientID.Set {
		if input.ClientID.Value == nil {
			updates["client_id"] = nil
		} else {
			clientID := strings.TrimSpace(*input.ClientID.Value)
			if _, err := uuid.Parse(clientID); err != nil {
				return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "client_id must be a UUID")
			}
			if err := requireClient(tx, clientID); err != nil {
				return nil, err
			}
			updates["client_id"] = clientID
		}
	}

	startDate := project.StartDate
	if input.StartDate.Set {
		if input.StartDate.Value == nil {
			startDate = nil
			updates["start_date"] = nil
		} else {
			value := strings.TrimSpace(*input.StartDate.Value)
			if !validDate(value) {
				return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "start_date must use YYYY-MM-DD")
			}
			startDate = &value
			updates["start_date"] = value
		}
	}
	dueDate := project.DueDate
	if input.DueDate.Set {
		if input.DueDate.Value == nil {
			dueDate = nil
			updates["due_date"] = nil
		} else {
			value := strings.TrimSpace(*input.DueDate.Value)
			if !validDate(value) {
				return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "due_date must use YYYY-MM-DD")
			}
			dueDate = &value
			updates["due_date"] = value
		}
	}
	if startDate != nil && dueDate != nil && *startDate > *dueDate {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "start_date cannot be after due_date")
	}
	if input.AmountMinor.Set {
		if input.AmountMinor.Value == nil {
			updates["amount_minor"] = nil
		} else {
			if *input.AmountMinor.Value < 0 {
				return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "amount_minor cannot be negative")
			}
			updates["amount_minor"] = *input.AmountMinor.Value
		}
	}
	if input.Color.Set {
		if input.Color.Value == nil {
			updates["color"] = nil
		} else {
			color := strings.ToUpper(strings.TrimSpace(*input.Color.Value))
			if !projectColorPattern.MatchString(color) {
				return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "color must use #RRGGBB")
			}
			updates["color"] = color
		}
	}
	if len(updates) == 0 {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "at least one editable project field is required")
	}
	return updates, nil
}

func projectTransition(project models.Project, action string) (string, any, error) {
	invalid := func() (string, any, error) {
		return "", nil, newProjectRequestError(
			http.StatusConflict,
			"INVALID_PROJECT_TRANSITION",
			fmt.Sprintf("action %q is not allowed while project status is %q", action, project.Status),
		)
	}
	switch action {
	case "start":
		if project.Status != "planning" {
			return invalid()
		}
		return "in_progress", nil, nil
	case "pause":
		if project.Status != "in_progress" {
			return invalid()
		}
		return "paused", nil, nil
	case "resume":
		if project.Status != "paused" {
			return invalid()
		}
		return "in_progress", nil, nil
	case "complete":
		if project.Status != "in_progress" && project.Status != "paused" {
			return invalid()
		}
		return "completed", nil, nil
	case "reopen":
		if project.Status != "completed" {
			return invalid()
		}
		return "in_progress", nil, nil
	case "archive":
		if project.Status == "archived" {
			return invalid()
		}
		return "archived", project.Status, nil
	case "restore":
		if project.Status != "archived" {
			return invalid()
		}
		target := "planning"
		if project.ArchivedFromStatus != nil {
			if _, valid := validProjectStatuses[*project.ArchivedFromStatus]; valid && *project.ArchivedFromStatus != "archived" {
				target = *project.ArchivedFromStatus
			}
		}
		return target, nil, nil
	default:
		return invalid()
	}
}

func requireClient(db *gorm.DB, clientID string) error {
	var count int64
	if err := db.Table("clients").Where("id = ?", clientID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return newProjectRequestError(http.StatusUnprocessableEntity, "CLIENT_NOT_FOUND", "client_id does not reference an existing client")
	}
	return nil
}

func bumpTasksForProject(db *gorm.DB, projectID, updatedAt string) (int64, error) {
	result := db.Model(&models.Task{}).
		Where("project_id = ?", projectID).
		Updates(map[string]any{
			"version":    gorm.Expr("version + 1"),
			"updated_at": updatedAt,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func projectID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project id must be a UUID")
		return "", false
	}
	return id, true
}

func projectIfMatch(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	if raw == "" {
		writeError(c, http.StatusPreconditionRequired, "VERSION_REQUIRED", "If-Match is required")
		return 0, false
	}
	if strings.HasPrefix(raw, "\"") || strings.HasSuffix(raw, "\"") {
		if len(raw) < 3 || !strings.HasPrefix(raw, "\"") || !strings.HasSuffix(raw, "\"") {
			writeError(c, http.StatusBadRequest, "INVALID_VERSION", "If-Match must contain one positive integer version")
			return 0, false
		}
		raw = raw[1 : len(raw)-1]
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || version < 1 {
		writeError(c, http.StatusBadRequest, "INVALID_VERSION", "If-Match must contain one positive integer version")
		return 0, false
	}
	return version, true
}

func setProjectETag(c *gin.Context, version int64) {
	c.Header("ETag", strconv.Quote(strconv.FormatInt(version, 10)))
}

func applyProjectSort(query *gorm.DB, raw string) (*gorm.DB, bool) {
	if strings.TrimSpace(raw) == "" {
		return query.
			Order("CASE projects.status WHEN 'in_progress' THEN 0 WHEN 'planning' THEN 1 WHEN 'paused' THEN 2 WHEN 'completed' THEN 3 ELSE 4 END ASC").
			Order("CASE WHEN projects.due_date IS NULL THEN 1 ELSE 0 END ASC").
			Order("projects.due_date ASC").
			Order("projects.updated_at DESC").
			Order("projects.id ASC"), true
	}
	allowed := map[string]string{
		"name": "projects.name", "status": "projects.status", "start_date": "projects.start_date",
		"due_date": "projects.due_date", "amount_minor": "projects.amount_minor",
		"created_at": "projects.created_at", "updated_at": "projects.updated_at",
	}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		direction := "ASC"
		if strings.HasPrefix(field, "-") {
			direction = "DESC"
			field = strings.TrimPrefix(field, "-")
		}
		column, ok := allowed[field]
		if !ok {
			return query, false
		}
		query = query.Order(column + " " + direction)
	}
	return query.Order("projects.id ASC"), true
}

const projectSelectColumns = `
	projects.id,
	projects.name,
	projects.description,
	projects.client_id,
	projects.status,
	projects.start_date,
	projects.due_date,
	projects.amount_minor,
	projects.color,
	projects.version,
	projects.archived_from_status,
	projects.created_at,
	projects.updated_at,
	clients.name AS client_name,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id) AS task_total,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id AND tasks.status = 'done') AS task_completed,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id AND tasks.status = 'in_progress') AS task_in_progress,
	(SELECT COALESCE(SUM(tasks.actual_minutes), 0) FROM tasks WHERE tasks.project_id = projects.id) AS task_actual_minutes,
	(SELECT COUNT(*) FROM invoices WHERE invoices.project_id = projects.id) AS invoice_count
`

func loadProjectRow(db *gorm.DB, id string) (projectRow, error) {
	var row projectRow
	err := db.Table("projects").
		Select(projectSelectColumns).
		Joins("LEFT JOIN clients ON clients.id = projects.client_id").
		Where("projects.id = ?", id).
		Take(&row).Error
	return row, err
}

func projectResponseFromRow(row projectRow) projectResponse {
	total := row.TaskTotal
	completed := row.TaskCompleted
	progress := 0
	if total > 0 {
		progress = int((completed*100 + total/2) / total)
	}
	return projectResponse{
		ID: row.ID, Name: row.Name, Description: row.Description,
		ClientID: row.ClientID, ClientName: row.ClientName, Status: row.Status,
		StartDate: row.StartDate, DueDate: row.DueDate, AmountMinor: row.AmountMinor,
		Color: row.Color, Version: row.Version, ArchivedFromStatus: row.ArchivedFromStatus,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
		TaskSummary: projectTaskSummary{
			Total: total, Completed: completed, InProgress: row.TaskInProgress,
			Remaining: total - completed, ProgressPercent: progress,
			ActualMinutes: row.TaskActualMinutes,
		},
		InvoiceCount: row.InvoiceCount, AvailableActions: availableProjectActions(row.Status),
	}
}

func availableProjectActions(status string) []string {
	switch status {
	case "planning":
		return []string{"start", "archive"}
	case "in_progress":
		return []string{"pause", "complete", "archive"}
	case "paused":
		return []string{"resume", "complete", "archive"}
	case "completed":
		return []string{"reopen", "archive"}
	case "archived":
		return []string{"restore"}
	default:
		return []string{}
	}
}

func newProjectRequestError(status int, code, message string) error {
	return &projectRequestError{status: status, code: code, message: message}
}

func projectVersionConflict() error {
	return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Project has changed; reload it before retrying")
}

func writeProjectRequestError(c *gin.Context, err error) bool {
	var requestError *projectRequestError
	if !errors.As(err, &requestError) {
		return false
	}
	writeError(c, requestError.status, requestError.code, requestError.message)
	return true
}
