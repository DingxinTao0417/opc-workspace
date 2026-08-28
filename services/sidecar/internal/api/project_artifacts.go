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

type projectArtifactOutput struct {
	Artifact           taskArtifactSummary       `json:"artifact"`
	Task               projectArtifactTaskOutput `json:"task"`
	SubmissionSequence int                       `json:"submission_sequence"`
}

type projectArtifactMeta struct {
	Page           int   `json:"page"`
	PageSize       int   `json:"page_size"`
	Total          int64 `json:"total"`
	ProjectVersion int64 `json:"project_version"`
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
		for _, row := range rows {
			taskIDs = append(taskIDs, row.TaskID)
		}
		var tasks []models.Task
		if err := tx.Select("id", "title", "status").Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			taskContext[task.ID] = projectArtifactTaskOutput{ID: task.ID, Title: task.Title, Status: task.Status}
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
		outputs[index] = projectArtifactOutput{Artifact: artifact, Task: task, SubmissionSequence: row.SubmissionSequence}
	}
	setProjectETag(c, project.Version)
	c.JSON(http.StatusOK, gin.H{"data": outputs, "meta": projectArtifactMeta{
		Page: page, PageSize: pageSize, Total: total, ProjectVersion: project.Version,
	}})
}
