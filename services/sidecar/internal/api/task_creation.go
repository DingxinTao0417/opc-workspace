package api

import (
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Standard creation and AI confirmation share domain validation, tags and
// parent reconciliation (including its events) in a caller-owned transaction.
func createTaskInTransaction(tx *gorm.DB, task models.Task, tagIDs []string, requestID string) (models.Task, error) {
	if task.ProjectID != nil {
		if err := requireAssignableProject(tx, *task.ProjectID); err != nil {
			return models.Task{}, err
		}
	}
	if task.ParentTaskID != nil {
		if err := requireValidTaskParent(tx, task.ID, *task.ParentTaskID); err != nil {
			return models.Task{}, err
		}
	}
	if err := requireTaskTags(tx, tagIDs); err != nil {
		return models.Task{}, err
	}
	if err := tx.Create(&task).Error; err != nil {
		return models.Task{}, err
	}
	if err := replaceTaskTags(tx, task.ID, tagIDs); err != nil {
		return models.Task{}, err
	}
	if err := reconcileTaskParentChain(tx, task.ParentTaskID, requestID, task.CreatedAt); err != nil {
		return models.Task{}, taskParentProgressError("reconcile created Task parent", err)
	}
	return loadTask(tx, task.ID)
}
