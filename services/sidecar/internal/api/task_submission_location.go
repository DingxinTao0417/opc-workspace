package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func taskSubmissionRoute(taskID, submissionID string) string {
	return "/tasks/" + taskID + "/submissions/" + submissionID
}

// Exact identity read for a human page, not an AI capability or approval.
// Never substitute the Task's current batch for a historical/deleted target.
func (a *API) getTaskSubmission(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	submissionID := c.Param("submissionId")
	if id, err := uuid.Parse(submissionID); err != nil || id.String() != submissionID {
		writeError(c, http.StatusBadRequest, "INVALID_SUBMISSION_ID", "Submission id must be a canonical UUID")
		return
	}
	if c.Request.URL.RawQuery != "" {
		writeError(c, http.StatusBadRequest, "INVALID_QUERY", "Exact submission reads do not accept query parameters")
		return
	}
	var task models.Task
	var output taskSubmissionOutput
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "title", "version", "status", "current_submission_id").First(&task, "id=?", taskIDValue).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		var row taskSubmissionRow
		if err := taskSubmissionRowsQuery(tx).Where("submission.id=? AND submission.task_id=?", submissionID, task.ID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "TASK_SUBMISSION_NOT_FOUND", "Submission not found for this Task")
			}
			return err
		}
		var err error
		output, err = submissionOutputFromRow(row)
		if err != nil {
			return err
		}
		outputs := []taskSubmissionOutput{output}
		if err = hydrateSubmissionArtifacts(tx, outputs, true); err != nil {
			return err
		}
		output = outputs[0]
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, task.Version)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"task":       gin.H{"id": task.ID, "title": task.Title, "status": task.Status, "version": task.Version, "current_submission_id": task.CurrentSubmissionID},
		"submission": output,
	}})
}
