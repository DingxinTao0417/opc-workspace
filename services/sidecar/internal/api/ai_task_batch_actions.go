package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Task batch update applies one already-known change to a bounded set of
// tasks. It mirrors the native batch endpoint semantics inside the approval
// transaction, so version checks, tag/project rules, lifecycle transitions and
// parent/Inbox reconciliation stay authoritative.
const (
	aiTaskBatchUpdateAction = "task.batch_update"
	maxAITaskBatchItems     = 20
)

var aiTaskBatchActions = map[string]bool{
	"set_project": true, "set_planned_date": true, "set_priority": true, "set_due_date": true, "add_tags": true, "remove_tags": true,
	"start": true, "block": true, "unblock": true, "complete": true, "cancel": true, "reopen": true,
	"set_assignee": true, "clear_assignee": true, "set_reviewer": true, "clear_reviewer": true,
}

type aiTaskBatchPreview struct {
	Action string                   `json:"action"`
	Count  int                      `json:"count"`
	Reason string                   `json:"reason,omitempty"`
	Items  []aiTaskBatchItemPreview `json:"items"`
}

type aiTaskBatchItemPreview struct {
	TaskID       string                        `json:"task_id"`
	Title        string                        `json:"title"`
	Version      int64                         `json:"version"`
	Field        string                        `json:"field"`
	Before       string                        `json:"before"`
	After        string                        `json:"after"`
	AssignmentID string                        `json:"assignment_id,omitempty"`
	BeforeActor  *aiTaskBatchAssignmentPreview `json:"before_actor,omitempty"`
	AfterActor   *aiTaskBatchAssignmentPreview `json:"after_actor,omitempty"`
}

func parseAITaskBatchAction(input aiWorkspaceAction, fields map[string]json.RawMessage, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" || input.ExpectedVersion != 0 {
		return input, errors.New("batch updates take the task set inside changes, not a single top-level target")
	}
	for key := range raw {
		switch key {
		case "action", "changes":
		default:
			return input, errors.New("task batch update only accepts action and changes")
		}
	}
	allowed := map[string]bool{"batch_action": true, "items": true, "expected_versions": true}
	var actionName string
	if json.Unmarshal(fields["batch_action"], &actionName) != nil {
		return input, errors.New("batch_action is required")
	}
	if !aiTaskBatchActions[actionName] {
		return input, errors.New("unsupported batch action")
	}
	switch actionName {
	case "set_priority":
		allowed["priority"] = true
	case "set_due_date":
		allowed["due_date"] = true
	case "set_project":
		allowed["project_id"] = true
	case "set_planned_date":
		allowed["planned_date"] = true
	case "add_tags", "remove_tags":
		allowed["tag_ids"] = true
	case "block", "cancel", "clear_assignee", "clear_reviewer":
		allowed["reason"] = true
	case "set_assignee", "set_reviewer":
		allowed["actor_id"] = true
		allowed["reason"] = true
	}
	for key, value := range fields {
		if !allowed[key] || string(value) == "null" && key != "project_id" && key != "planned_date" && key != "due_date" {
			return input, fmt.Errorf("unsupported or null batch field: %s", key)
		}
	}
	var items []string
	if json.Unmarshal(fields["items"], &items) != nil {
		return input, errors.New("items must be an array of task UUIDs")
	}
	if len(items) == 0 || len(items) > maxAITaskBatchItems {
		return input, fmt.Errorf("items must contain 1-%d tasks", maxAITaskBatchItems)
	}
	seen := map[string]bool{}
	for _, value := range items {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value {
			return input, errors.New("items must contain canonical task UUIDs")
		}
		if seen[value] {
			return input, errors.New("items must not repeat a task")
		}
		seen[value] = true
	}
	var versions []int64
	if json.Unmarshal(fields["expected_versions"], &versions) != nil || len(versions) != len(items) {
		return input, errors.New("expected_versions must match items one-to-one")
	}
	for _, version := range versions {
		if version < 1 {
			return input, errors.New("each expected_version must be positive")
		}
	}
	canonical := map[string]any{"batch_action": actionName, "items": items, "expected_versions": versions}
	switch actionName {
	case "set_priority":
		var priority string
		if json.Unmarshal(fields["priority"], &priority) != nil {
			return input, errors.New("priority is required")
		}
		if _, valid := validPriorities[priority]; !valid {
			return input, errors.New("priority must be P0, P1, P2, or P3")
		}
		canonical["priority"] = priority
	case "set_due_date":
		if _, present := fields["due_date"]; !present {
			return input, errors.New("set_due_date requires due_date, which may be null")
		}
		var dueDate *string
		if value := fields["due_date"]; string(value) != "null" {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return input, errors.New("due_date must be an RFC 3339 timestamp or null")
			}
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(text))
			if err != nil {
				return input, errors.New("due_date must be an RFC 3339 timestamp or null")
			}
			normalized := parsed.UTC().Format(time.RFC3339Nano)
			dueDate = &normalized
		}
		canonical["due_date"] = dueDate
	case "set_project":
		if _, present := fields["project_id"]; !present {
			return input, errors.New("set_project requires project_id, which may be null")
		}
		var projectID *string
		if value, present := fields["project_id"]; present && string(value) != "null" {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return input, errors.New("project_id must be a canonical UUID or null")
			}
			id, err := uuid.Parse(strings.TrimSpace(text))
			if err != nil || id.String() != text {
				return input, errors.New("project_id must be a canonical UUID or null")
			}
			projectID = &text
		}
		canonical["project_id"] = projectID
	case "set_planned_date":
		if _, present := fields["planned_date"]; !present {
			return input, errors.New("set_planned_date requires planned_date, which may be null")
		}
		var plannedDate *string
		if value, present := fields["planned_date"]; present && string(value) != "null" {
			var text string
			if json.Unmarshal(value, &text) != nil || !validDate(strings.TrimSpace(text)) {
				return input, errors.New("planned_date must use YYYY-MM-DD or be null")
			}
			trimmed := strings.TrimSpace(text)
			plannedDate = &trimmed
		}
		canonical["planned_date"] = plannedDate
	case "add_tags", "remove_tags":
		var tagIDs []string
		if json.Unmarshal(fields["tag_ids"], &tagIDs) != nil || len(tagIDs) == 0 || len(tagIDs) > 20 {
			return input, errors.New("tag_ids must contain 1-20 tags")
		}
		normalized, err := validateAITaskTagIDs(tagIDs)
		if err != nil {
			return input, err
		}
		canonical["tag_ids"] = normalized
	case "set_assignee", "set_reviewer":
		var actorID string
		if json.Unmarshal(fields["actor_id"], &actorID) != nil {
			return input, errors.New("actor_id is required for batch responsibility")
		}
		id, err := uuid.Parse(actorID)
		if err != nil || id.String() != actorID {
			return input, errors.New("actor_id must be a canonical Actor UUID")
		}
		canonical["actor_id"] = actorID
		fallthrough
	case "block", "cancel", "clear_assignee", "clear_reviewer":
		var reason string
		if json.Unmarshal(fields["reason"], &reason) != nil {
			return input, errors.New("reason is required")
		}
		trimmed, err := validateAssignmentReason(reason)
		if err != nil {
			return input, err
		}
		if (actionName == "block" || actionName == "cancel") && len([]rune(trimmed)) < 2 {
			return input, errors.New("batch block/cancel reason must contain 2 to 1000 characters")
		}
		canonical["reason"] = trimmed
	}
	input.Changes, _ = json.Marshal(canonical)
	return input, nil
}

type aiTaskBatchChanges struct {
	BatchAction      string   `json:"batch_action"`
	Items            []string `json:"items"`
	ExpectedVersions []int64  `json:"expected_versions"`
	ProjectID        *string  `json:"project_id,omitempty"`
	PlannedDate      *string  `json:"planned_date,omitempty"`
	DueDate          *string  `json:"due_date,omitempty"`
	Priority         string   `json:"priority,omitempty"`
	TagIDs           []string `json:"tag_ids,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	ActorID          string   `json:"actor_id,omitempty"`
}

func decodeAITaskBatchChanges(input aiWorkspaceAction) (aiTaskBatchChanges, error) {
	var changes aiTaskBatchChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return changes, err
	}
	if !aiTaskBatchActions[changes.BatchAction] || len(changes.Items) == 0 ||
		len(changes.Items) != len(changes.ExpectedVersions) {
		return changes, errors.New("invalid batch payload")
	}
	return changes, nil
}

func aiTaskBatchFieldLabel(action string) string {
	switch action {
	case "set_priority":
		return "priority"
	case "set_due_date":
		return "due_date"
	case "set_project":
		return "project"
	case "set_planned_date":
		return "planned_date"
	case "add_tags", "remove_tags":
		return "tags"
	case "set_assignee", "clear_assignee":
		return "assignee"
	case "set_reviewer", "clear_reviewer":
		return "reviewer"
	default:
		return "status"
	}
}

func aiTaskBatchStatusLabel(status string) string {
	switch status {
	case "todo":
		return "待办"
	case "in_progress":
		return "进行中"
	case "blocked":
		return "阻塞"
	case "waiting_review":
		return "待验收"
	case "done":
		return "已完成"
	case "cancelled":
		return "已取消"
	default:
		return status
	}
}

func aiTaskBatchTargetStatus(action string) string {
	switch action {
	case "start":
		return "in_progress"
	case "block":
		return "blocked"
	case "unblock":
		return "in_progress"
	case "complete":
		return "done"
	case "cancel":
		return "cancelled"
	case "reopen":
		return "todo"
	default:
		return ""
	}
}

func loadAITaskBatchTags(tx *gorm.DB, taskID string) ([]string, error) {
	ids := []string{}
	if err := tx.Table("task_tags").Where("task_id = ?", taskID).Order("tag_id ASC").Pluck("tag_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func aiTaskTagNames(tx *gorm.DB, tagIDs []string) ([]string, error) {
	if len(tagIDs) == 0 {
		return []string{}, nil
	}
	var tags []models.Tag
	if err := tx.Select("id", "name").Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	sort.Strings(names)
	return names, nil
}

func aiTaskProjectName(tx *gorm.DB, projectID *string) (string, error) {
	if projectID == nil {
		return "未关联项目", nil
	}
	var project models.Project
	if err := tx.Select("name").First(&project, "id = ?", *projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", newProjectRequestError(422, "PROJECT_NOT_FOUND", "project_id does not reference an existing Project")
		}
		return "", err
	}
	return project.Name, nil
}

// previewAITaskBatchAction freezes one human-readable row per selected task
// and verifies every version before the proposal is stored.
func previewAITaskBatchAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	changes, err := decodeAITaskBatchChanges(input)
	if err != nil {
		return preview, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
	}
	batch := aiTaskBatchPreview{Action: changes.BatchAction, Reason: changes.Reason}
	role := aiTaskBatchAssignmentRole(changes.BatchAction)
	var targetActor *aiTaskBatchAssignmentPreview
	if role != "" && changes.ActorID != "" {
		if err := requireAssignmentActor(tx, changes.ActorID, role); err != nil {
			return preview, err
		}
		actor, err := loadAITaskBatchActor(tx, changes.ActorID, role)
		if err != nil {
			return preview, err
		}
		targetActor = &actor
	}
	target := aiTaskBatchTargetStatus(changes.BatchAction)
	projectName := ""
	if changes.BatchAction == "set_project" {
		if changes.ProjectID != nil {
			if err := requireAssignableProject(tx, *changes.ProjectID); err != nil {
				return preview, err
			}
		}
		projectName, err = aiTaskProjectName(tx, changes.ProjectID)
		if err != nil {
			return preview, err
		}
	}
	if changes.BatchAction == "add_tags" || changes.BatchAction == "remove_tags" {
		if err := requireTaskTags(tx, changes.TagIDs); err != nil {
			return preview, err
		}
	}
	type taskTag struct {
		TaskID string `gorm:"column:task_id"`
		TagID  string `gorm:"column:tag_id"`
	}
	currentTagSets := map[string]map[string]struct{}{}
	if changes.BatchAction == "add_tags" || changes.BatchAction == "remove_tags" {
		var rows []taskTag
		if err := tx.Table("task_tags").Select("task_id, tag_id").Where("task_id IN ?", changes.Items).Scan(&rows).Error; err != nil {
			return preview, err
		}
		for _, row := range rows {
			if currentTagSets[row.TaskID] == nil {
				currentTagSets[row.TaskID] = map[string]struct{}{}
			}
			currentTagSets[row.TaskID][row.TagID] = struct{}{}
		}
		if changes.BatchAction == "add_tags" {
			for _, taskID := range changes.Items {
				resultingCount := len(currentTagSets[taskID])
				for _, tagID := range changes.TagIDs {
					if _, exists := currentTagSets[taskID][tagID]; !exists {
						resultingCount++
					}
				}
				if resultingCount > 20 {
					return preview, newProjectRequestError(422, "VALIDATION_ERROR", "a task cannot have more than 20 tags")
				}
			}
		}
	}
	for index, taskID := range changes.Items {
		task, err := loadTask(tx, taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return preview, newProjectRequestError(409, "TASK_BATCH_SET_CHANGED", "A selected task no longer exists; reload before retrying")
			}
			return preview, err
		}
		if task.Version != changes.ExpectedVersions[index] {
			return preview, taskVersionConflict()
		}
		item := aiTaskBatchItemPreview{
			TaskID: task.ID, Title: task.Title, Version: task.Version,
			Field: aiTaskBatchFieldLabel(changes.BatchAction),
		}
		switch changes.BatchAction {
		case "set_priority":
			item.Before = task.Priority
			item.After = changes.Priority
		case "set_due_date":
			item.Before = valueOrUnset(task.DueDate)
			item.After = valueOrUnset(changes.DueDate)
		case "set_assignee", "clear_assignee", "set_reviewer", "clear_reviewer":
			if changes.ActorID != "" && (task.Status == "done" || task.Status == "cancelled") {
				return preview, newProjectRequestError(409, "TASK_NOT_ASSIGNABLE", "Terminal Tasks cannot receive a responsibility")
			}
			var active models.TaskAssignment
			err := tx.Where("task_id=? AND role=? AND unassigned_at IS NULL", task.ID, role).Take(&active).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return preview, err
			}
			if err == nil {
				if active.ActorID == changes.ActorID {
					return preview, newProjectRequestError(409, "ASSIGNMENT_UNCHANGED", "A selected Task already has the requested Actor")
				}
				item.AssignmentID = active.ID
				actor, err := loadAITaskBatchActor(tx, active.ActorID, role)
				if err != nil {
					return preview, err
				}
				item.BeforeActor = &actor
				item.Before = actor.ActorName
			} else {
				if changes.ActorID == "" {
					return preview, newProjectRequestError(409, "ASSIGNMENT_NOT_ACTIVE", "A selected Task has no active responsibility to end")
				}
				item.Before = "未分派"
			}
			item.AfterActor = targetActor
			if targetActor != nil {
				item.After = targetActor.ActorName
			} else {
				item.After = "未分派"
			}
		case "set_project":
			before, projectErr := aiTaskProjectName(tx, task.ProjectID)
			if projectErr != nil {
				return preview, projectErr
			}
			item.Before = before
			item.After = projectName
		case "set_planned_date":
			item.Before = valueOrUnset(task.PlannedDate)
			item.After = valueOrUnset(changes.PlannedDate)
		case "add_tags", "remove_tags":
			currentIDs := sortedTagSetIDs(currentTagSets[task.ID])
			currentNames, tagErr := aiTaskTagNames(tx, currentIDs)
			if tagErr != nil {
				return preview, tagErr
			}
			next := currentIDs
			if changes.BatchAction == "add_tags" {
				next = unionTagIDs(currentIDs, changes.TagIDs)
			} else {
				next = subtractTagIDs(currentIDs, changes.TagIDs)
			}
			nextNames, tagErr := aiTaskTagNames(tx, next)
			if tagErr != nil {
				return preview, tagErr
			}
			item.Before = joinOrNone(currentNames)
			item.After = joinOrNone(nextNames)
		default:
			item.Before = aiTaskBatchStatusLabel(task.Status)
			item.After = aiTaskBatchStatusLabel(target)
			if err := validateTaskLifecycleTransition(tx, task, changes.BatchAction); err != nil {
				return preview, err
			}
		}
		batch.Items = append(batch.Items, item)
	}
	batch.Count = len(batch.Items)
	preview.Label = fmt.Sprintf("%d 个任务", batch.Count)
	preview.After["count"] = batch.Count
	preview.TaskBatch = &batch
	return preview, nil
}

func aiTaskBatchAssignmentRole(action string) string {
	switch action {
	case "set_assignee", "clear_assignee":
		return "assignee"
	case "set_reviewer", "clear_reviewer":
		return "reviewer"
	default:
		return ""
	}
}

func loadAITaskBatchActor(tx *gorm.DB, actorID, role string) (aiTaskBatchAssignmentPreview, error) {
	var actor models.Actor
	if err := tx.Select("id", "display_name", "type", "status", "version").First(&actor, "id=?", actorID).Error; err != nil {
		return aiTaskBatchAssignmentPreview{}, err
	}
	return aiTaskBatchAssignmentPreview{Role: role, ActorID: actor.ID, ActorName: actor.DisplayName,
		ActorType: actor.Type, ActorStatus: actor.Status, ActorVersion: actor.Version}, nil
}

func valueOrUnset(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "未安排"
	}
	return *value
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "无标签"
	}
	return strings.Join(values, "、")
}

func unionTagIDs(current, additions []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(current)+len(additions))
	for _, id := range current {
		seen[id] = true
		result = append(result, id)
	}
	for _, id := range additions {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func subtractTagIDs(current, removals []string) []string {
	remove := map[string]bool{}
	for _, id := range removals {
		remove[id] = true
	}
	result := make([]string, 0, len(current))
	for _, id := range current {
		if !remove[id] {
			result = append(result, id)
		}
	}
	return result
}

func sortedTagSetIDs(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// executeAITaskBatchAction mirrors the native batch transaction. Any failure
// rolls the whole batch back; a batch never reports partial success.
func executeAITaskBatchAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) error {
	changes, err := decodeAITaskBatchChanges(input)
	if err != nil {
		return err
	}
	var current []models.Task
	if err := tx.Model(&models.Task{}).Where("id IN ?", changes.Items).Find(&current).Error; err != nil {
		return err
	}
	if len(current) != len(changes.Items) {
		return newProjectRequestError(409, "TASK_BATCH_SET_CHANGED", "A selected task no longer exists; reload before retrying")
	}
	currentByID := make(map[string]models.Task, len(current))
	expectedVersions := make(map[string]int64, len(changes.Items))
	for index, id := range changes.Items {
		expectedVersions[id] = changes.ExpectedVersions[index]
	}
	for _, task := range current {
		currentByID[task.ID] = task
	}
	for id, expectedVersion := range expectedVersions {
		if currentByID[id].Version != expectedVersion {
			return taskVersionConflict()
		}
	}
	if aiTaskBatchAssignmentRole(changes.BatchAction) != "" {
		return executeAITaskBatchAssignment(tx, changes, requestID, now)
	}
	lifecycleCommand := aiTaskBatchLifecycleCommand(changes.BatchAction)
	if lifecycleCommand != "" {
		for _, id := range changes.Items {
			task := currentByID[id]
			normalizeTask(&task)
			currentByID[id] = task
			if err := validateTaskLifecycleTransition(tx, task, lifecycleCommand); err != nil {
				return err
			}
		}
	}
	if changes.BatchAction == "set_project" && changes.ProjectID != nil {
		needsAssignmentCheck := false
		for _, task := range current {
			if task.ProjectID == nil || *task.ProjectID != *changes.ProjectID {
				needsAssignmentCheck = true
				break
			}
		}
		if needsAssignmentCheck {
			if err := requireAssignableProject(tx, *changes.ProjectID); err != nil {
				return err
			}
		}
	}
	if changes.BatchAction == "add_tags" || changes.BatchAction == "remove_tags" {
		if err := requireTaskTags(tx, changes.TagIDs); err != nil {
			return err
		}
	}
	currentTags := make(map[string]map[string]struct{}, len(current))
	if changes.BatchAction == "add_tags" || changes.BatchAction == "remove_tags" {
		type taskTag struct {
			TaskID string `gorm:"column:task_id"`
			TagID  string `gorm:"column:tag_id"`
		}
		var rows []taskTag
		if err := tx.Table("task_tags").Select("task_id, tag_id").Where("task_id IN ?", changes.Items).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if currentTags[row.TaskID] == nil {
				currentTags[row.TaskID] = map[string]struct{}{}
			}
			currentTags[row.TaskID][row.TagID] = struct{}{}
		}
		if changes.BatchAction == "add_tags" {
			for _, task := range current {
				resultingCount := len(currentTags[task.ID])
				for _, tagID := range changes.TagIDs {
					if _, exists := currentTags[task.ID][tagID]; !exists {
						resultingCount++
					}
				}
				if resultingCount > 20 {
					return newProjectRequestError(422, "VALIDATION_ERROR", "a task cannot have more than 20 tags")
				}
			}
		}
	}
	affectedParents := make(map[string]struct{})
	affectedParentOrder := make([]string, 0)
	selfReconcileTasks := make(map[string]struct{})
	selfReconcileTaskOrder := make([]string, 0)
	for _, id := range changes.Items {
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
			if _, _, err := applyValidatedTaskLifecycleTransition(tx, latest, lifecycleCommand, changes.Reason, requestID, now); err != nil {
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
			continue
		}
		switch changes.BatchAction {
		case "set_priority":
			if task.Priority != changes.Priority {
				result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", id, task.Version).
					Updates(map[string]any{"priority": changes.Priority, "updated_at": now, "version": gorm.Expr("version + 1")})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return taskVersionConflict()
				}
				changed = true
			}
		case "set_due_date":
			if !sameNullableString(task.DueDate, changes.DueDate) {
				result := tx.Model(&models.Task{}).Where("id = ? AND version = ?", id, task.Version).
					Updates(map[string]any{"due_date": changes.DueDate, "updated_at": now, "version": gorm.Expr("version + 1")})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return taskVersionConflict()
				}
				changed = true
			}
		case "set_project":
			if !sameNullableString(task.ProjectID, changes.ProjectID) {
				result := tx.Model(&models.Task{}).
					Where("id = ? AND version = ?", id, task.Version).
					Updates(map[string]any{"project_id": changes.ProjectID, "updated_at": now, "version": gorm.Expr("version + 1")})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return taskVersionConflict()
				}
				changed = true
			}
		case "set_planned_date":
			if !sameNullableString(task.PlannedDate, changes.PlannedDate) {
				result := tx.Model(&models.Task{}).
					Where("id = ? AND version = ?", id, task.Version).
					Updates(map[string]any{"planned_date": changes.PlannedDate, "manual_order": nil, "updated_at": now, "version": gorm.Expr("version + 1")})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return taskVersionConflict()
				}
				changed = true
			}
		case "add_tags":
			for _, tagID := range changes.TagIDs {
				if _, exists := currentTags[id][tagID]; exists {
					continue
				}
				if err := tx.Exec("INSERT INTO task_tags(task_id, tag_id) VALUES (?, ?)", id, tagID).Error; err != nil {
					return err
				}
				changed = true
			}
		case "remove_tags":
			for _, tagID := range changes.TagIDs {
				if _, exists := currentTags[id][tagID]; !exists {
					continue
				}
				if err := tx.Exec("DELETE FROM task_tags WHERE task_id = ? AND tag_id = ?", id, tagID).Error; err != nil {
					return err
				}
				changed = true
			}
		}
		if changed && (changes.BatchAction == "add_tags" || changes.BatchAction == "remove_tags") {
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
	}
	for _, id := range selfReconcileTaskOrder {
		if _, err := reconcileTaskParentProgress(tx, id, requestID, now); err != nil {
			return taskParentProgressError("reconcile batch lifecycle parent Task", err)
		}
	}
	for _, id := range affectedParentOrder {
		parentID := id
		if err := reconcileTaskParentChain(tx, &parentID, requestID, now); err != nil {
			return taskParentProgressError("reconcile batch lifecycle Task parent", err)
		}
	}
	return nil
}

func executeAITaskBatchAssignment(tx *gorm.DB, changes aiTaskBatchChanges, requestID, now string) error {
	role := aiTaskBatchAssignmentRole(changes.BatchAction)
	if changes.ActorID != "" {
		if err := requireAssignmentActor(tx, changes.ActorID, role); err != nil {
			return err
		}
	}
	for _, taskID := range changes.Items {
		// The whole selection was version-checked before any mutation. A prior
		// item may legitimately reconcile a selected parent Task, so use its
		// current in-transaction version for the shared domain command.
		var task models.Task
		if err := tx.Select("id", "version").First(&task, "id=?", taskID).Error; err != nil {
			return err
		}
		var active models.TaskAssignment
		err := tx.Where("task_id=? AND role=? AND unassigned_at IS NULL", taskID, role).Take(&active).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if changes.ActorID == "" {
			if err != nil {
				return newProjectRequestError(409, "ASSIGNMENT_NOT_ACTIVE", "A selected Task has no active responsibility to end")
			}
			if _, err := endAssignmentInTransaction(tx, active.ID, task.Version, changes.Reason, requestID, now); err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if _, err := createAssignmentInTransaction(tx, taskID, task.Version, role, changes.ActorID, requestID, now); err != nil {
				return err
			}
		} else {
			if active.ActorID == changes.ActorID {
				return newProjectRequestError(409, "ASSIGNMENT_UNCHANGED", "A selected Task already has the requested Actor")
			}
			if _, err := reassignInTransaction(tx, taskID, task.Version, role, changes.ActorID, changes.Reason, requestID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func aiTaskBatchLifecycleCommand(action string) string {
	switch action {
	case taskLifecycleStart, taskLifecycleBlock, taskLifecycleUnblock, taskLifecycleComplete, taskLifecycleCancel, taskLifecycleReopen:
		return action
	default:
		return ""
	}
}
