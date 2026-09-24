package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared by human HTTP commands and AI approvals. Callers own transaction,
// idempotency and consent; no gin context or independent commit is allowed here.
func prepareInboxSplit(tx *gorm.DB, inboxID string, expectedVersion int64, normalized normalizedInboxSplit) (models.InboxItem, error) {
	current, loadErr := loadInboxItem(tx, inboxID)
	if loadErr != nil {
		return current, inboxItemLoadError(loadErr)
	}
	if current.Version != expectedVersion {
		return current, inboxVersionConflict()
	}
	if inboxItemTerminal(current.Status) {
		return current, inboxTerminalConflict("Archived Inbox Items must be reopened before splitting Tasks")
	}
	var activeCount int64
	if err := tx.Model(&models.InboxItemTask{}).Where("inbox_item_id = ? AND unlinked_at IS NULL", inboxID).Count(&activeCount).Error; err != nil {
		return current, err
	}
	if activeCount+int64(len(normalized.Tasks)) > maxActiveInboxTasks {
		return current, newProjectRequestError(http.StatusConflict, "INBOX_TASK_LIMIT_REACHED", "The split would exceed 100 active Task relations")
	}

	for _, entry := range normalized.Tasks {
		if entry.Task.ProjectID != nil {
			if err := requireAssignableProject(tx, *entry.Task.ProjectID); err != nil {
				return current, err
			}
		}
		if err := requireTaskTags(tx, entry.TagIDs); err != nil {
			return current, err
		}
		if err := requireAssignmentActor(tx, entry.AssigneeActorID, "assignee"); err != nil {
			return current, err
		}
		if entry.Task.ReviewPolicy == "manual" {
			if err := requireAssignmentActor(tx, models.BuiltinOwnerActorID, "reviewer"); err != nil {
				return current, err
			}
		}
	}
	return current, nil
}

func splitInboxItemInTransaction(tx *gorm.DB, inboxID string, expectedVersion int64, normalized normalizedInboxSplit, requestID string, now time.Time) (splitInboxItemResponse, error) {
	var response splitInboxItemResponse
	current, err := prepareInboxSplit(tx, inboxID, expectedVersion, normalized)
	if err != nil {
		return response, err
	}
	nowText := formatInboxTimestamp(now)

	createdByKey := make(map[string]models.Task, len(normalized.Tasks))
	response.Created = make([]inboxSplitTaskOutput, 0, len(normalized.Tasks))
	var position int
	if err := tx.Model(&models.InboxItemTask{}).Select("COALESCE(MAX(position), 0)").Where("inbox_item_id=? AND unlinked_at IS NULL", inboxID).Scan(&position).Error; err != nil {
		return response, err
	}
	for _, entry := range normalized.Tasks {
		task := entry.Task
		task.CreatedAt = nowText
		task.UpdatedAt = nowText
		if entry.ParentKey != "" {
			parent := createdByKey[entry.ParentKey]
			task.ParentTaskID = &parent.ID
		}
		if task.ProjectID != nil {
			if err := requireAssignableProject(tx, *task.ProjectID); err != nil {
				return response, err
			}
		}
		if err := requireTaskTags(tx, entry.TagIDs); err != nil {
			return response, err
		}
		if err := requireAssignmentActor(tx, entry.AssigneeActorID, "assignee"); err != nil {
			return response, err
		}
		if err := tx.Create(&task).Error; err != nil {
			return response, fmt.Errorf("create split Task: %w", err)
		}
		if err := replaceTaskTags(tx, task.ID, entry.TagIDs); err != nil {
			return response, err
		}
		if err := recordSplitTaskCreatedEvent(tx, task, inboxID, entry.Key, requestID, nowText); err != nil {
			return response, err
		}

		assignments := make([]assignmentResponse, 0, 2)
		assignee, err := createSplitAssignment(tx, task.ID, entry.AssigneeActorID, "assignee", requestID, nowText, 2)
		if err != nil {
			return response, err
		}
		assignments = append(assignments, assignee)
		if task.ReviewPolicy == "manual" {
			reviewer, err := createSplitAssignment(tx, task.ID, models.BuiltinOwnerActorID, "reviewer", requestID, nowText, 3)
			if err != nil {
				return response, err
			}
			assignments = append(assignments, reviewer)
		}

		position++
		relation := models.InboxItemTask{
			ID: uuid.NewString(), InboxItemID: inboxID, TaskRefID: task.ID, TaskID: &task.ID,
			TaskTitleSnapshot: task.Title, RelationType: "created", IsRequired: entry.IsRequired,
			Position: position, LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: nowText,
		}
		if err := tx.Create(&relation).Error; err != nil {
			return response, mapInboxTaskConstraintError(err)
		}
		loadedTask, err := loadTask(tx, task.ID)
		if err != nil {
			return response, err
		}
		relationOutput, err := loadInboxTaskRelationOutput(tx, relation.ID)
		if err != nil {
			return response, err
		}
		createdByKey[entry.Key] = loadedTask
		response.Created = append(response.Created, inboxSplitTaskOutput{
			Key: entry.Key, Task: loadedTask, Assignments: assignments, Relation: relationOutput,
		})
	}
	// Creating a later child increments its already-created parent through
	// the hierarchy trigger. Reload every Task and relation only after the
	// full split graph exists so the response and idempotency snapshot never
	// preserve an intermediate parent version.
	for index := range response.Created {
		loadedTask, err := loadTask(tx, response.Created[index].Task.ID)
		if err != nil {
			return response, err
		}
		relationOutput, err := loadInboxTaskRelationOutput(tx, response.Created[index].Relation.ID)
		if err != nil {
			return response, err
		}
		response.Created[index].Task = loadedTask
		response.Created[index].Relation = relationOutput
	}

	triagedAt := current.TriagedAt
	if triagedAt == nil {
		triagedAt = &nowText
	}
	result := tx.Model(&models.InboxItem{}).
		Where("id = ? AND version = ?", inboxID, expectedVersion).
		Updates(map[string]any{
			"status": "tracking", "resolution_policy": normalized.ResolutionPolicy,
			"triaged_at": triagedAt, "snoozed_until": nil,
			"version": gorm.Expr("version + 1"), "updated_at": nowText,
		})
	if result.Error != nil {
		return response, mapInboxTaskConstraintError(result.Error)
	}
	if result.RowsAffected != 1 {
		return response, inboxVersionConflict()
	}
	updated, err := loadInboxItem(tx, inboxID)
	if err != nil {
		return response, err
	}
	progress, err := loadInboxTaskProgress(tx, inboxID)
	if err != nil {
		return response, err
	}
	if err := recordInboxWorkflowEventAs(
		tx, inboxID, "tasks_split", models.BuiltinOwnerActorID,
		inboxItemEventState(current, ""), splitInboxEventState(updated, response.Created, progress),
		requestID, nowText,
	); err != nil {
		return response, err
	}
	updated, progress, err = reconcileInboxItem(tx, inboxID, requestID, nowText)
	if err != nil {
		return response, err
	}
	response.Progress = progress
	response.InboxItem, err = inboxItemOutputFromModel(updated, now)
	if err != nil {
		return response, err
	}
	return response, nil
}
