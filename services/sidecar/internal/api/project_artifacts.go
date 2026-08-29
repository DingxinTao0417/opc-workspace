package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type projectArtifactTaskOutput struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type projectArtifactFollowupOutput struct {
	InboxItemID      string            `json:"inbox_item_id"`
	InboxItemVersion int64             `json:"inbox_item_version"`
	Status           string            `json:"status"`
	ResolutionPolicy string            `json:"resolution_policy"`
	SourceDeletedAt  *string           `json:"source_deleted_at"`
	Progress         inboxTaskProgress `json:"progress"`
}

type projectArtifactOutput struct {
	Artifact           taskArtifactSummary            `json:"artifact"`
	Task               projectArtifactTaskOutput      `json:"task"`
	SubmissionSequence int                            `json:"submission_sequence"`
	Followup           *projectArtifactFollowupOutput `json:"followup"`
}

type projectArtifactMeta struct {
	Page           int   `json:"page"`
	PageSize       int   `json:"page_size"`
	Total          int64 `json:"total"`
	ProjectVersion int64 `json:"project_version"`
}

type projectArtifactListResponse struct {
	Data []projectArtifactOutput `json:"data"`
	Meta projectArtifactMeta     `json:"meta"`
}

func (a *API) listProjectArtifacts(c *gin.Context) {
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
	includeDeleted, err := optionalBooleanQuery(c, "include_deleted")
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}

	var project models.Project
	var rows []taskArtifactRow
	taskContext := make(map[string]projectArtifactTaskOutput)
	followupContext := make(map[string]*projectArtifactFollowupOutput)
	var total int64
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "version").First(&project, "id = ?", projectIDValue).Error; err != nil {
			return err
		}
		base := tx.Table("task_artifacts AS artifact").
			Joins("JOIN tasks AS task ON task.id = artifact.task_id").
			Where("task.project_id = ?", projectIDValue)
		if !includeDeleted {
			base = base.Where("artifact.deleted_at IS NULL")
		}
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		query := taskArtifactRowsQuery(tx).
			Joins("JOIN tasks AS project_task ON project_task.id = artifact.task_id").
			Where("project_task.project_id = ?", projectIDValue)
		if !includeDeleted {
			query = query.Where("artifact.deleted_at IS NULL")
		}
		if err := query.Order("artifact.created_at DESC").Order("artifact.id ASC").
			Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		taskIDs := make([]string, 0, len(rows))
		artifactIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			taskIDs = append(taskIDs, row.TaskID)
			artifactIDs = append(artifactIDs, row.ID)
		}
		var tasks []models.Task
		if err := tx.Select("id", "title", "status").Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			taskContext[task.ID] = projectArtifactTaskOutput{ID: task.ID, Title: task.Title, Status: task.Status}
		}

		var inboxItems []models.InboxItem
		if err := tx.Select("id", "source_entity_id", "source_event_key", "source_deleted_at", "status", "resolution_policy", "version").
			Where("source_entity_type = ? AND source_entity_id IN ?", taskArtifactInboxSourceType, artifactIDs).
			Order("id ASC").Find(&inboxItems).Error; err != nil {
			return err
		}
		inboxIDs := make([]string, 0, len(inboxItems))
		for _, item := range inboxItems {
			if item.SourceEntityID == nil || item.SourceEventKey == nil ||
				*item.SourceEventKey != taskArtifactFollowupEventKey(*item.SourceEntityID) {
				return errors.New("Project Artifact follow-up source is inconsistent")
			}
			if _, exists := followupContext[*item.SourceEntityID]; exists {
				return errors.New("Project Artifact has duplicate follow-up sources")
			}
			inboxIDs = append(inboxIDs, item.ID)
			followupContext[*item.SourceEntityID] = &projectArtifactFollowupOutput{
				InboxItemID: item.ID, InboxItemVersion: item.Version,
				Status: item.Status, ResolutionPolicy: item.ResolutionPolicy,
				SourceDeletedAt: normalizeOptionalTimestamp(item.SourceDeletedAt),
			}
		}
		progressByInboxID, err := loadInboxTaskProgressByInboxIDs(tx, inboxIDs)
		if err != nil {
			return err
		}
		for _, followup := range followupContext {
			followup.Progress = progressByInboxID[followup.InboxItemID]
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
			return
		}
		writeDatabaseError(c)
		return
	}

	outputs := make([]projectArtifactOutput, len(rows))
	for index, row := range rows {
		artifact, conversionErr := artifactSummaryFromRow(row)
		if conversionErr != nil {
			writeDatabaseError(c)
			return
		}
		task, exists := taskContext[row.TaskID]
		if !exists {
			writeDatabaseError(c)
			return
		}
		outputs[index] = projectArtifactOutput{
			Artifact: artifact, Task: task, SubmissionSequence: row.SubmissionSequence,
			Followup: followupContext[row.ID],
		}
	}
	response := projectArtifactListResponse{Data: outputs, Meta: projectArtifactMeta{
		Page: page, PageSize: pageSize, Total: total, ProjectVersion: project.Version,
	}}
	setProjectETag(c, project.Version)
	c.JSON(http.StatusOK, response)
}
