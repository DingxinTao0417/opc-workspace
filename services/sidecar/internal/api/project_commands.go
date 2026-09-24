package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared by manual HTTP commands and human-confirmed AI proposals. The caller
// owns the transaction so projections, workflow events and approvals are atomic.
func updateProjectInTransaction(tx *gorm.DB, id string, expectedVersion int64, input updateProjectRequest, requestID, updatedAt string) (response projectResponse, err error) {
	var project models.Project
	if err := tx.First(&project, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
		}
		return response, err
	}
	if project.Version != expectedVersion {
		return response, projectVersionConflict()
	}
	if project.Status == "archived" {
		return response, newProjectRequestError(
			http.StatusConflict,
			"PROJECT_ARCHIVED",
			"Restore the project before editing it",
		)
	}

	updates, err := projectUpdates(tx, project, input)
	if err != nil {
		return response, err
	}
	nameChanged := false
	if name, exists := updates["name"].(string); exists {
		nameChanged = name != project.Name
	}
	updates["updated_at"] = updatedAt
	updates["version"] = gorm.Expr("version + 1")
	result := tx.Model(&models.Project{}).
		Where("id = ? AND version = ?", id, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		if strings.Contains(result.Error.Error(), "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES") {
			return response, newProjectRequestError(
				http.StatusConflict,
				"PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES",
				"Project client cannot be changed while invoices reference this project",
			)
		}
		return response, result.Error
	}
	if result.RowsAffected == 0 {
		return response, projectVersionConflict()
	}
	if nameChanged {
		if _, err := bumpTasksForProject(tx, id, updatedAt); err != nil {
			return response, err
		}
	}
	row, err := loadProjectRow(tx, id)
	if err != nil {
		return response, err
	}
	if err := recordProjectWorkflowEvent(
		tx,
		id,
		"project_updated",
		projectEventState(project),
		projectEventState(row.Project),
		requestID,
		updatedAt,
	); err != nil {
		return response, err
	}
	response = projectResponseFromRow(row)
	return response, nil
}

func transitionProjectInTransaction(tx *gorm.DB, id string, expectedVersion int64, input transitionProjectRequest, requestID, updatedAt string) (response projectResponse, err error) {
	var project models.Project
	if err := tx.First(&project, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
		}
		return response, err
	}
	if project.Version != expectedVersion {
		return response, projectVersionConflict()
	}

	target, archivedFrom, err := projectTransition(project, input.Action)
	if err != nil {
		return response, err
	}
	var incompleteTaskCount int64
	if input.Action == "complete" {
		if err := tx.Table("tasks").Where("project_id = ? AND status <> ?", id, "done").Count(&incompleteTaskCount).Error; err != nil {
			return response, err
		}
		if incompleteTaskCount > 0 && !input.ConfirmIncompleteTasks {
			return response, newProjectRequestError(
				http.StatusConflict,
				"INCOMPLETE_TASKS_CONFIRMATION_REQUIRED",
				fmt.Sprintf("Project has %d incomplete task(s); explicit confirmation is required", incompleteTaskCount),
			)
		}
	}

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
		return response, result.Error
	}
	if result.RowsAffected == 0 {
		return response, projectVersionConflict()
	}
	row, err := loadProjectRow(tx, id)
	if err != nil {
		return response, err
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
		requestID,
		updatedAt,
	)
	if err != nil {
		return response, err
	}
	if input.Action == "complete" || input.Action == "reopen" {
		if err := projectClientActivity(tx, row.Project, input.Action, eventID, updatedAt); err != nil {
			return response, err
		}
	}
	if input.Action == "complete" {
		if err := projectProjectCompletionInboxItem(
			tx,
			row.Project,
			incompleteTaskCount,
			requestID,
			updatedAt,
		); err != nil {
			return response, err
		}
		if err := enqueueProjectCompletionAutomationDelivery(tx, eventID, row.Project, updatedAt); err != nil {
			return response, err
		}
	}
	response = projectResponseFromRow(row)
	return response, nil
}

func createProjectInTransaction(tx *gorm.DB, project models.Project, requestID string) (response projectResponse, err error) {
	if project.ClientID != nil {
		if err := requireClient(tx, *project.ClientID); err != nil {
			return response, err
		}
	}
	if err := tx.Create(&project).Error; err != nil {
		return response, fmt.Errorf("create project: %w", err)
	}
	if err := recordProjectWorkflowEvent(
		tx,
		project.ID,
		"project_created",
		nil,
		projectEventState(project),
		requestID,
		project.CreatedAt,
	); err != nil {
		return response, err
	}
	row, err := loadProjectRow(tx, project.ID)
	if err != nil {
		return response, fmt.Errorf("load created project: %w", err)
	}
	response = projectResponseFromRow(row)
	return response, nil
}
