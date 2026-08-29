package api

import (
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

type reorderTaskItem struct {
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type reorderTasksRequest struct {
	PlannedDate nullableStringPatch `json:"planned_date"`
	Mode        string              `json:"mode"`
	Items       []reorderTaskItem   `json:"items"`
}

type reorderedTasksResponse struct {
	Mode        string        `json:"mode"`
	PlannedDate *string       `json:"planned_date"`
	Changed     int           `json:"changed"`
	Tasks       []models.Task `json:"tasks"`
}

type batchUpdateTasksRequest struct {
	Items       []reorderTaskItem   `json:"items"`
	Action      string              `json:"action"`
	ProjectID   nullableStringPatch `json:"project_id"`
	PlannedDate nullableStringPatch `json:"planned_date"`
	TagIDs      []string            `json:"tag_ids"`
	Reason      *string             `json:"reason"`
}

type batchUpdatedTasksResponse struct {
	Action  string        `json:"action"`
	Changed int           `json:"changed"`
	Tasks   []models.Task `json:"tasks"`
}

func (a *API) batchUpdateTasks(c *gin.Context) {
	var input batchUpdateTasksRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.Action = strings.TrimSpace(input.Action)
	if len(input.Items) == 0 || len(input.Items) > 100 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "items must contain 1 to 100 tasks")
		return
	}
	orderedIDs, versions, ok := validateTaskOperationItems(c, input.Items)
	if !ok {
		return
	}

	var projectID *string
	var plannedDate *string
	var tagIDs []string
	var lifecycleCommand string
	var lifecycleReason string
	switch input.Action {
	case "set_project":
		if !input.ProjectID.Set || input.PlannedDate.Set || len(input.TagIDs) > 0 || input.Reason != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "set_project requires only project_id, which may be null")
			return
		}
		if input.ProjectID.Value != nil {
			value := strings.TrimSpace(*input.ProjectID.Value)
			if _, err := uuid.Parse(value); err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "project_id must be a UUID or null")
				return
			}
			projectID = &value
		}
	case "set_planned_date":
		if !input.PlannedDate.Set || input.ProjectID.Set || len(input.TagIDs) > 0 || input.Reason != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "set_planned_date requires only planned_date, which may be null")
			return
		}
		if input.PlannedDate.Value != nil {
			value := strings.TrimSpace(*input.PlannedDate.Value)
			if !validDate(value) {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "planned_date must use YYYY-MM-DD or be null")
				return
			}
			plannedDate = &value
		}
	case "add_tags", "remove_tags":
		if input.ProjectID.Set || input.PlannedDate.Set || len(input.TagIDs) == 0 || input.Reason != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", input.Action+" requires only a non-empty tag_ids array")
			return
		}
		var err error
		tagIDs, err = validateTaskTagIDs(input.TagIDs)
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
	case taskLifecycleStart, taskLifecycleBlock, taskLifecycleUnblock, taskLifecycleComplete, taskLifecycleCancel, taskLifecycleReopen:
		if input.ProjectID.Set || input.PlannedDate.Set || len(input.TagIDs) > 0 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", input.Action+" does not accept project_id, planned_date, or tag_ids")
			return
		}
		lifecycleCommand = input.Action
		if lifecycleCommand == taskLifecycleBlock || lifecycleCommand == taskLifecycleCancel {
			if input.Reason == nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reason is required")
				return
			}
			var err error
			lifecycleReason, err = validateAssignmentReason(*input.Reason)
			if err != nil {
				writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
				return
			}
		} else if input.Reason != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", input.Action+" does not accept reason")
			return
		}
	default:
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "unsupported batch action")
		return
	}

	response := batchUpdatedTasksResponse{Action: input.Action}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current []models.Task
		if err := tx.Model(&models.Task{}).Where("id IN ?", orderedIDs).Find(&current).Error; err != nil {
			return err
		}
		if len(current) != len(orderedIDs) {
			return newProjectRequestError(http.StatusConflict, "TASK_BATCH_SET_CHANGED", "A selected task no longer exists; reload before retrying")
		}
		currentByID := make(map[string]models.Task, len(current))
		for _, task := range current {
			currentByID[task.ID] = task
		}
		for id, expectedVersion := range versions {
			if currentByID[id].Version != expectedVersion {
				return taskVersionConflict()
			}
		}
		if lifecycleCommand != "" {
			for _, id := range orderedIDs {
				task := currentByID[id]
				normalizeTask(&task)
				currentByID[id] = task
				if err := validateTaskLifecycleTransition(tx, task, lifecycleCommand); err != nil {
					return err
				}
			}
		}
		if projectID != nil {
			needsAssignmentCheck := false
			for _, task := range current {
				if task.ProjectID == nil || *task.ProjectID != *projectID {
					needsAssignmentCheck = true
					break
				}
			}
			if needsAssignmentCheck {
				if err := requireAssignableProject(tx, *projectID); err != nil {
					return err
				}
			}
		}
		if len(tagIDs) > 0 {
			if err := requireTaskTags(tx, tagIDs); err != nil {
				return err
			}
		}

		currentTags := make(map[string]map[string]struct{}, len(current))
		if input.Action == "add_tags" || input.Action == "remove_tags" {
			type taskTag struct {
				TaskID string `gorm:"column:task_id"`
				TagID  string `gorm:"column:tag_id"`
			}
			var rows []taskTag
			if err := tx.Table("task_tags").Select("task_id, tag_id").Where("task_id IN ?", orderedIDs).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if currentTags[row.TaskID] == nil {
					currentTags[row.TaskID] = make(map[string]struct{})
				}
				currentTags[row.TaskID][row.TagID] = struct{}{}
			}
			if input.Action == "add_tags" {
				for _, task := range current {
					resultingCount := len(currentTags[task.ID])
					for _, tagID := range tagIDs {
						if _, exists := currentTags[task.ID][tagID]; !exists {
							resultingCount++
						}
					}
					if resultingCount > 20 {
						return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "a task cannot have more than 20 tags")
					}
				}
			}
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		affectedParents := make(map[string]struct{})
		affectedParentOrder := make([]string, 0)
		selfReconcileTasks := make(map[string]struct{})
		selfReconcileTaskOrder := make([]string, 0)
		for _, id := range orderedIDs {
			task := currentByID[id]
			changed := false
			if lifecycleCommand != "" {
				latest, err := loadTask(tx, id)
				if err != nil {
					return err
				}
				if err := validateTaskLifecycleTransition(tx, latest, lifecycleCommand); err != nil {
					return err
				}
				if _, _, err := applyValidatedTaskLifecycleTransition(
					tx, latest, lifecycleCommand, lifecycleReason, requestIDFromContext(c), now,
				); err != nil {
					return err
				}
				if lifecycleCommand == taskLifecycleComplete || lifecycleCommand == taskLifecycleCancel || lifecycleCommand == taskLifecycleReopen {
					if latest.ParentTaskID != nil {
						if _, exists := affectedParents[*latest.ParentTaskID]; !exists {
							affectedParents[*latest.ParentTaskID] = struct{}{}
							affectedParentOrder = append(affectedParentOrder, *latest.ParentTaskID)
						}
					}
				}
				if lifecycleCommand == taskLifecycleStart || lifecycleCommand == taskLifecycleUnblock {
					if _, exists := selfReconcileTasks[id]; !exists {
						selfReconcileTasks[id] = struct{}{}
						selfReconcileTaskOrder = append(selfReconcileTaskOrder, id)
					}
				}
				response.Changed++
				continue
			}
			switch input.Action {
			case "set_project":
				if !sameNullableString(task.ProjectID, projectID) {
					result := tx.Model(&models.Task{}).
						Where("id = ? AND version = ?", id, task.Version).
						Updates(map[string]any{"project_id": projectID, "updated_at": now, "version": gorm.Expr("version + 1")})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected == 0 {
						return taskVersionConflict()
					}
					changed = true
				}
			case "set_planned_date":
				if !sameNullableString(task.PlannedDate, plannedDate) {
					result := tx.Model(&models.Task{}).
						Where("id = ? AND version = ?", id, task.Version).
						Updates(map[string]any{"planned_date": plannedDate, "manual_order": nil, "updated_at": now, "version": gorm.Expr("version + 1")})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected == 0 {
						return taskVersionConflict()
					}
					changed = true
				}
			case "add_tags":
				for _, tagID := range tagIDs {
					if _, exists := currentTags[id][tagID]; exists {
						continue
					}
					if err := tx.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", id, tagID).Error; err != nil {
						return err
					}
					changed = true
				}
			case "remove_tags":
				for _, tagID := range tagIDs {
					if _, exists := currentTags[id][tagID]; !exists {
						continue
					}
					if err := tx.Exec("DELETE FROM task_tags WHERE task_id = ? AND tag_id = ?", id, tagID).Error; err != nil {
						return err
					}
					changed = true
				}
			}
			if changed && (input.Action == "add_tags" || input.Action == "remove_tags") {
				result := tx.Model(&models.Task{}).
					Where("id = ? AND version = ?", id, task.Version).
					Updates(map[string]any{"updated_at": now, "version": gorm.Expr("version + 1")})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return taskVersionConflict()
				}
			}
			if changed {
				response.Changed++
			}
		}
		for _, id := range selfReconcileTaskOrder {
			if _, err := reconcileTaskParentProgress(tx, id, requestIDFromContext(c), now); err != nil {
				return taskParentProgressError("reconcile batch lifecycle parent Task", err)
			}
		}
		for _, id := range affectedParentOrder {
			parentID := id
			if err := reconcileTaskParentChain(tx, &parentID, requestIDFromContext(c), now); err != nil {
				return taskParentProgressError("reconcile batch lifecycle Task parent", err)
			}
		}

		loaded, err := loadTasksInOrder(tx, orderedIDs)
		if err != nil {
			return err
		}
		response.Tasks = loaded
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) reorderTasks(c *gin.Context) {
	var input reorderTasksRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !input.PlannedDate.Set {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "planned_date is required and may be null")
		return
	}
	var plannedDate *string
	if input.PlannedDate.Value != nil {
		value := strings.TrimSpace(*input.PlannedDate.Value)
		if !validDate(value) {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "planned_date must use YYYY-MM-DD or be null")
			return
		}
		plannedDate = &value
	}
	input.Mode = strings.TrimSpace(input.Mode)
	if input.Mode != "manual" && input.Mode != "default" {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "mode must be manual or default")
		return
	}
	if len(input.Items) > 1000 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reorder cannot contain more than 1000 tasks")
		return
	}
	versions := make(map[string]int64, len(input.Items))
	orderedIDs := make([]string, len(input.Items))
	for index, item := range input.Items {
		id := strings.TrimSpace(item.ID)
		if _, err := uuid.Parse(id); err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "items must contain task UUID values")
			return
		}
		if item.ExpectedVersion < 1 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "each reorder item requires a positive expected_version")
			return
		}
		if _, duplicate := versions[id]; duplicate {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reorder task ids must be unique")
			return
		}
		versions[id] = item.ExpectedVersion
		orderedIDs[index] = id
	}

	response := reorderedTasksResponse{Mode: input.Mode, PlannedDate: plannedDate}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		type currentTask struct {
			ID          string `gorm:"column:id"`
			Version     int64  `gorm:"column:version"`
			ManualOrder *int   `gorm:"column:manual_order"`
		}
		query := tx.Model(&models.Task{}).Select("id, version, manual_order")
		if plannedDate == nil {
			query = query.Where("planned_date IS NULL")
		} else {
			query = query.Where("planned_date = ?", *plannedDate)
		}
		var current []currentTask
		if err := query.Find(&current).Error; err != nil {
			return err
		}
		if len(current) != len(input.Items) {
			return newProjectRequestError(http.StatusConflict, "TASK_REORDER_SET_CHANGED", "Tasks in this planned-date group changed; reload before reordering")
		}
		currentByID := make(map[string]currentTask, len(current))
		for _, task := range current {
			currentByID[task.ID] = task
		}
		for id, expectedVersion := range versions {
			task, exists := currentByID[id]
			if !exists {
				return newProjectRequestError(http.StatusConflict, "TASK_REORDER_SET_CHANGED", "Tasks in this planned-date group changed; reload before reordering")
			}
			if task.Version != expectedVersion {
				return taskVersionConflict()
			}
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		for index, id := range orderedIDs {
			currentTask := currentByID[id]
			var targetOrder *int
			if input.Mode == "manual" {
				value := (index + 1) * 1000
				targetOrder = &value
			}
			if sameNullableInt(currentTask.ManualOrder, targetOrder) {
				continue
			}
			result := tx.Model(&models.Task{}).
				Where("id = ? AND version = ?", id, currentTask.Version).
				Updates(map[string]any{
					"manual_order": targetOrder,
					"updated_at":   now,
					"version":      gorm.Expr("version + 1"),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return taskVersionConflict()
			}
			response.Changed++
		}

		orderedQuery := tx.Model(&models.Task{})
		if plannedDate == nil {
			orderedQuery = orderedQuery.Where("tasks.planned_date IS NULL")
		} else {
			orderedQuery = orderedQuery.Where("tasks.planned_date = ?", *plannedDate)
		}
		var ok bool
		if input.Mode == "manual" {
			orderedQuery, ok = applyTaskSort(orderedQuery, "manual_order")
		} else {
			orderedQuery, ok = applyTaskSort(orderedQuery, "")
		}
		if !ok {
			return errors.New("apply task reorder sort")
		}
		if err := withTaskProject(orderedQuery).Find(&response.Tasks).Error; err != nil {
			return fmt.Errorf("load reordered tasks: %w", err)
		}
		if err := hydrateTaskTags(tx, response.Tasks); err != nil {
			return err
		}
		normalizeTasks(response.Tasks)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func sameNullableInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateTaskOperationItems(c *gin.Context, items []reorderTaskItem) ([]string, map[string]int64, bool) {
	versions := make(map[string]int64, len(items))
	orderedIDs := make([]string, len(items))
	for index, item := range items {
		id := strings.TrimSpace(item.ID)
		if _, err := uuid.Parse(id); err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "items must contain task UUID values")
			return nil, nil, false
		}
		if item.ExpectedVersion < 1 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "each item requires a positive expected_version")
			return nil, nil, false
		}
		if _, duplicate := versions[id]; duplicate {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "task ids must be unique")
			return nil, nil, false
		}
		versions[id] = item.ExpectedVersion
		orderedIDs[index] = id
	}
	return orderedIDs, versions, true
}

func sameNullableString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func loadTasksInOrder(db *gorm.DB, ids []string) ([]models.Task, error) {
	var tasks []models.Task
	if err := withTaskProject(db.Where("tasks.id IN ?", ids)).Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}
	if err := hydrateTaskTags(db, tasks); err != nil {
		return nil, err
	}
	normalizeTasks(tasks)
	byID := make(map[string]models.Task, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	ordered := make([]models.Task, 0, len(ids))
	for _, id := range ids {
		task, exists := byID[id]
		if !exists {
			return nil, fmt.Errorf("load task %s: %w", id, gorm.ErrRecordNotFound)
		}
		ordered = append(ordered, task)
	}
	return ordered, nil
}
