package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type contentItemTaskCommand struct {
	TaskID              string
	ExpectedTaskVersion *int64
	IsRequired          *bool
}

func loadContentItemTaskTarget(tx *gorm.DB, taskID string, expectedVersion *int64) (models.Task, error) {
	var task models.Task
	if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return task, newProjectRequestError(http.StatusUnprocessableEntity, "TASK_NOT_FOUND", "task_id must reference an existing task")
		}
		return task, err
	}
	if expectedVersion != nil && task.Version != *expectedVersion {
		return task, taskVersionConflict()
	}
	return task, nil
}

func loadContentItemTaskRelation(tx *gorm.DB, contentItemID, taskID string) (models.ContentItemTask, error) {
	var relation models.ContentItemTask
	if err := tx.First(&relation, "content_item_id = ? AND task_id = ?", contentItemID, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return relation, newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_TASK_NOT_FOUND", "Content item task link not found")
		}
		return relation, err
	}
	return relation, nil
}

func contentItemTaskAlreadyLinkedError() error {
	return newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_TASK_ALREADY_LINKED", "Task is already linked to this content item")
}

func contentItemTaskRequirementUnchangedError() error {
	return newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_TASK_REQUIREMENT_UNCHANGED", "Task requirement is already set to the requested value")
}

// mutateContentItemTaskInTransaction is the single relationship-write path for
// both the human HTTP endpoints and confirmed AI proposals. Callers own the
// surrounding transaction and approval/audit record.
//
// upsert preserves the existing HTTP contract. The AI-only link, requirement
// and unlink commands deliberately have exact, non-overlapping semantics so a
// displayed approval can never turn into a different relationship operation.
func mutateContentItemTaskInTransaction(
	tx *gorm.DB,
	contentItemID string,
	expectedVersion int64,
	command contentItemTaskCommand,
	operation string,
	requestID string,
	now time.Time,
) (contentItemResponse, models.Task, error) {
	var response contentItemResponse
	var item models.ContentItem
	if err := tx.First(&item, "id = ?", contentItemID).Error; err != nil {
		return response, models.Task{}, contentItemNotFoundError(err)
	}
	if item.Version != expectedVersion {
		return response, models.Task{}, contentItemVersionConflict()
	}

	var task models.Task
	var relation models.ContentItemTask
	var err error
	switch operation {
	case "upsert", "link":
		if command.IsRequired == nil {
			return response, task, errors.New("is_required is required")
		}
		task, err = loadContentItemTaskTarget(tx, command.TaskID, command.ExpectedTaskVersion)
		if err != nil {
			return response, task, err
		}
		if operation == "link" {
			if err := tx.First(&relation, "content_item_id = ? AND task_id = ?", contentItemID, command.TaskID).Error; err == nil {
				return response, task, contentItemTaskAlreadyLinkedError()
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return response, task, err
			}
		}
	case "requirement":
		if command.IsRequired == nil {
			return response, task, errors.New("is_required is required")
		}
		task, err = loadContentItemTaskTarget(tx, command.TaskID, command.ExpectedTaskVersion)
		if err != nil {
			return response, task, err
		}
		relation, err = loadContentItemTaskRelation(tx, contentItemID, command.TaskID)
		if err != nil {
			return response, task, err
		}
		if relation.IsRequired == *command.IsRequired {
			return response, task, contentItemTaskRequirementUnchangedError()
		}
	case "unlink":
		relation, err = loadContentItemTaskRelation(tx, contentItemID, command.TaskID)
		if err != nil {
			return response, task, err
		}
		if command.ExpectedTaskVersion != nil {
			task, err = loadContentItemTaskTarget(tx, command.TaskID, command.ExpectedTaskVersion)
			if err != nil {
				return response, task, err
			}
		}
	default:
		return response, task, errors.New("unsupported Content Item Task command")
	}

	nowText := formatInboxTimestamp(now.UTC())
	if err := resolveContentItemInboxSources(tx, contentItemID, "content_item_tasks_changed", requestID, nowText); err != nil {
		return response, task, err
	}

	switch operation {
	case "upsert":
		relation = models.ContentItemTask{
			ContentItemID: contentItemID,
			TaskID:        command.TaskID,
			IsRequired:    *command.IsRequired,
			LinkedAt:      nowText,
		}
		if err := tx.Where("content_item_id = ? AND task_id = ?", contentItemID, command.TaskID).
			Assign(map[string]any{"is_required": *command.IsRequired, "linked_at": nowText}).
			FirstOrCreate(&relation).Error; err != nil {
			return response, task, err
		}
	case "link":
		relation = models.ContentItemTask{
			ContentItemID: contentItemID,
			TaskID:        command.TaskID,
			IsRequired:    *command.IsRequired,
			LinkedAt:      nowText,
		}
		if err := tx.Create(&relation).Error; err != nil {
			return response, task, err
		}
	case "requirement":
		result := tx.Model(&models.ContentItemTask{}).
			Where("content_item_id = ? AND task_id = ?", contentItemID, command.TaskID).
			Update("is_required", *command.IsRequired)
		if result.Error != nil {
			return response, task, result.Error
		}
		if result.RowsAffected != 1 {
			return response, task, newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_TASK_NOT_FOUND", "Content item task link not found")
		}
	case "unlink":
		result := tx.Where("content_item_id = ? AND task_id = ?", contentItemID, command.TaskID).Delete(&models.ContentItemTask{})
		if result.Error != nil {
			return response, task, result.Error
		}
		if result.RowsAffected != 1 {
			return response, task, newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_TASK_NOT_FOUND", "Content item task link not found")
		}
	}

	result := tx.Model(&models.ContentItem{}).
		Where("id = ? AND version = ?", contentItemID, expectedVersion).
		Updates(map[string]any{"updated_at": nowText, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return response, task, result.Error
	}
	if result.RowsAffected != 1 {
		return response, task, contentItemVersionConflict()
	}
	response, err = loadContentItemResponse(tx, contentItemID)
	return response, task, err
}
