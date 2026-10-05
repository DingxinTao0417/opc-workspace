package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiActionReason(input aiWorkspaceAction) (string, error) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input.Changes, &fields)
	if input.Action != "task.block" && input.Action != "task.cancel" {
		if len(fields) != 0 {
			return "", errors.New("this lifecycle action requires empty changes")
		}
		return "", nil
	}
	var reason string
	if json.Unmarshal(fields["reason"], &reason) != nil {
		return "", errors.New("reason is required")
	}
	return validateAssignmentReason(reason)
}

func aiTaskActionFields(task models.Task) map[string]any {
	return map[string]any{
		"title": task.Title, "description": task.Description, "priority": task.Priority, "kind": task.Kind,
		"planned_date": task.PlannedDate, "due_date": task.DueDate, "estimated_minutes": task.EstimatedMinutes,
		"project_id": task.ProjectID, "completion_criteria": task.CompletionCriteria,
		"review_policy": task.ReviewPolicy, "status": task.Status,
	}
}

func aiTaskTagValues(tags []models.Tag) ([]string, []string) {
	ids := make([]string, 0, len(tags))
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		ids = append(ids, tag.ID)
		names = append(names, tag.Name)
	}
	return ids, names
}

func validateAITaskTagIDs(values []string) ([]string, error) {
	ids, err := validateTaskTagIDs(values)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return nil, errors.New("tag_ids must use canonical UUIDs from workspace_task_options")
		}
	}
	return ids, nil
}

func validateAITaskParentID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	id := strings.TrimSpace(*value)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return nil, errors.New("parent_task_id must use a canonical UUID from workspace_search/get")
	}
	return &id, nil
}

func aiTaskParentTitle(tx *gorm.DB, taskID, parentID string) (*string, error) {
	if err := requireValidTaskParent(tx, taskID, parentID); err != nil {
		return nil, err
	}
	var parent models.Task
	if err := tx.Select("id", "title").First(&parent, "id = ?", parentID).Error; err != nil {
		return nil, err
	}
	title := parent.Title
	return &title, nil
}

func loadAITaskTags(tx *gorm.DB, tagIDs []string) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return []models.Tag{}, nil
	}
	var tags []models.Tag
	if err := tx.Where("id IN ?", tagIDs).Order("LOWER(name) ASC").Order("id ASC").Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newProjectRequestError(422, "TAG_NOT_FOUND", "tag_ids contains a tag that does not exist")
	}
	for index := range tags {
		normalizeTag(&tags[index])
	}
	return tags, nil
}

// Preview validates the same domain rules without executing changes. Confirmation
// repeats all validation inside the atomic domain-command/decision transaction.
func previewAIWorkspaceAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	if input.Action == aiAgentDelegateSpawnAction {
		return aiActionPreview{}, errors.New("Agent delegation preview requires its frozen parent grant")
	}
	if input.Action == aiAgentDelegateFollowupAction {
		return aiActionPreview{}, errors.New("Agent follow-up preview requires its frozen parent grant")
	}
	if input.Action == aiAgentRunRecoverOutput {
		return previewAIAgentRunRecovery(tx, input)
	}
	if input.Action == "agent_run.retry" {
		return previewAIAgentRunRetry(tx, input, nil)
	}
	if input.Action == aiAgentRunCancelManyAction {
		return previewAIAgentRunCancelMany(tx, input)
	}
	if input.Action == "agent_run.cancel" {
		return previewAIAgentRunCancel(tx, input)
	}
	if isAIAgentRunAction(input.Action) {
		return previewAIAgentRunAction(tx, input)
	}
	if isAITaskOutputAction(input.Action) {
		return previewAITaskOutputAction(tx, input)
	}
	if isAIAssignmentAction(input.Action) {
		return previewAIAssignmentAction(tx, input)
	}
	if input.Action == "finance.export_csv" {
		p, _, err := previewAIFinanceExport(tx, input)
		return p, err
	}
	if strings.HasPrefix(input.Action, "invoice.") {
		return previewAIInvoiceAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		return previewAIFinancialAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "automation.") {
		return previewAIAutomationAction(tx, input)
	}
	if isAIKnowledgeAction(input.Action) {
		return previewAIKnowledgeSourceAction(tx, input, now)
	}
	if isAITaskViewAction(input.Action) {
		return previewAITaskViewAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "project_note.") {
		return previewAIProjectNoteAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_activity.") {
		return previewAIClientActivityAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_followup.") {
		return previewAIClientFollowupAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "focus.") {
		return previewAIFocusAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "reminder.") {
		return previewAIReminderAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "inbox.") {
		return previewAIInboxAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "roadmap_milestone.") {
		if input.Action == aiRoadmapMilestoneMoveAction {
			return previewAIRoadmapMilestoneMove(tx, input)
		}
		return previewAIRoadmapMilestoneAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "content_item.") {
		return previewAIContentItemAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "client.") {
		return previewAIClientAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "client_contact.") {
		return previewAIClientContactAction(tx, input)
	}
	if isAIPersonAction(input.Action) {
		return previewAIPersonAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "tag.") {
		return previewAITagAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "project.") {
		return previewAIProjectAction(tx, input)
	}
	if input.Action == aiTaskBatchUpdateAction {
		return previewAITaskBatchAction(tx, input)
	}
	if input.Action == aiTaskBatchCreateAction {
		return previewAITaskBatchCreateAction(tx, input)
	}
	if input.Action == aiTaskMoveAction {
		return previewAITaskMoveAction(tx, input)
	}
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "task.create" {
		var create createTaskRequest
		_ = json.Unmarshal(input.Changes, &create)
		task, err := taskFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if task.ProjectID != nil {
			if err := requireAssignableProject(tx, *task.ProjectID); err != nil {
				return preview, err
			}
		}
		preview.Label, preview.After = task.Title, aiTaskActionFields(task)
		var changeFields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &changeFields)
		if _, present := changeFields["tag_ids"]; present {
			tagIDs, err := validateAITaskTagIDs(create.TagIDs)
			if err != nil {
				return preview, err
			}
			tags, err := loadAITaskTags(tx, tagIDs)
			if err != nil {
				return preview, err
			}
			_, tagNames := aiTaskTagValues(tags)
			preview.After["tag_names"] = tagNames
		}
		if _, present := changeFields["parent_task_id"]; present {
			parentID, err := validateAITaskParentID(task.ParentTaskID)
			if err != nil {
				return preview, err
			}
			if parentID == nil {
				preview.After["parent_task_title"] = nil
			} else {
				parentTitle, err := aiTaskParentTitle(tx, task.ID, *parentID)
				if err != nil {
					return preview, err
				}
				preview.After["parent_task_title"] = parentTitle
			}
		}
		return preview, nil
	}
	task, err := loadTask(tx, input.TaskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
	}
	if err != nil {
		return preview, err
	}
	if task.Version != input.ExpectedVersion {
		return preview, taskVersionConflict()
	}
	preview.Label = task.Title
	before := aiTaskActionFields(task)
	if input.Action == "task.delete" {
		_, impact, err := loadTaskDeletionImpact(tx, task.ID, task.Version)
		if err != nil {
			return preview, err
		}
		preview.After = map[string]any{
			"task_deleted":                   true,
			"child_tasks_moved_to_top_level": impact.ChildTasks,
			"submissions_deleted":            impact.Submissions,
			"artifacts_deleted":              impact.Artifacts,
			"file_artifacts_deleted":         impact.FileArtifacts,
			"assignments_deleted":            impact.Assignments,
			"agent_runs_deleted":             impact.AgentRuns,
			"focus_sessions_detached":        impact.FocusSessions,
			"inbox_relations_detached":       impact.InboxRelations,
			"inbox_sources_coordinated":      impact.InboxSources,
			"tag_links_deleted":              impact.TagLinks,
		}
	} else if input.Action == "task.update" {
		var update updateTaskRequest
		_ = json.Unmarshal(input.Changes, &update)
		changes, tagIDs, err := validateTaskUpdate(update)
		if err != nil {
			return preview, err
		}
		if update.ReviewPolicy != nil {
			targetPolicy := changes["review_policy"].(string)
			if err := validateTaskReviewPolicyChange(tx, task, targetPolicy); err != nil {
				return preview, err
			}
			progress, err := loadTaskChildProgress(tx, task.ID)
			if err != nil {
				return preview, err
			}
			gatesReady, err := taskParentRollupGatesReady(tx, task.ID)
			if err != nil {
				return preview, err
			}
			// A changed policy has already passed the todo/no-history gate.
			// The native command reconciles this parent only on an actual change.
			willRequestReview := targetPolicy != task.ReviewPolicy && targetPolicy == "manual" && progress.readyForReview() && gatesReady
			preview.Before["status"], preview.After["status"] = task.Status, task.Status
			if willRequestReview {
				preview.After["status"] = "waiting_review"
			}
			preview.After["subtask_total"] = progress.Total
			preview.After["subtask_completed"] = progress.Completed
			preview.After["subtask_cancelled"] = progress.Cancelled
			preview.After["parent_rollup_gates_ready"] = gatesReady
			preview.After["will_request_parent_review"] = willRequestReview
		}
		if update.TagIDs.Set {
			tagIDs, err = validateAITaskTagIDs(tagIDs)
			if err != nil {
				return preview, err
			}
		}
		if project, ok := changes["project_id"].(string); ok {
			if err := requireAssignableProject(tx, project); err != nil {
				return preview, err
			}
		}
		for field, value := range changes {
			if field == "parent_task_id" {
				continue
			}
			preview.Before[field], preview.After[field] = before[field], value
		}
		if update.TagIDs.Set {
			tags, err := loadAITaskTags(tx, tagIDs)
			if err != nil {
				return preview, err
			}
			_, beforeNames := aiTaskTagValues(task.Tags)
			_, afterNames := aiTaskTagValues(tags)
			preview.Before["tag_names"] = beforeNames
			preview.After["tag_names"] = afterNames
		}
		if update.ParentTaskID.Set {
			preview.Before["parent_task_title"] = task.ParentTaskTitle
			parentID, err := validateAITaskParentID(update.ParentTaskID.Value)
			if err != nil {
				return preview, err
			}
			if parentID == nil {
				preview.After["parent_task_title"] = nil
			} else {
				parentTitle, err := aiTaskParentTitle(tx, task.ID, *parentID)
				if err != nil {
					return preview, err
				}
				preview.After["parent_task_title"] = parentTitle
			}
		}
	} else {
		command := strings.TrimPrefix(input.Action, "task.")
		if err := validateTaskLifecycleTransition(tx, task, command); err != nil {
			return preview, err
		}
		reason, _ := aiActionReason(input)
		changes, _, err := taskLifecycleUpdates(task, command, reason, now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return preview, err
		}
		preview.Before["status"], preview.After["status"] = task.Status, changes["status"]
		if reason != "" {
			preview.After["reason"] = reason
		}
	}
	return preview, nil
}
