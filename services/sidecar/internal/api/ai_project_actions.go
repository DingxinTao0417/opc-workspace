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

var aiProjectChangeFields = map[string]bool{
	"name": true, "description": true, "start_date": true, "due_date": true, "color": true, "client_id": true,
}

func parseAIProjectAction(input aiWorkspaceAction, fields map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" {
		return input, errors.New("project actions do not accept task_id")
	}
	switch input.Action {
	case "project.create":
		if input.ProjectID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a project ID/version")
		}
	case "project.update", "project.start", "project.pause", "project.resume", "project.complete", "project.reopen", "project.archive", "project.restore", "project.delete":
		if _, err := uuid.Parse(input.ProjectID); err != nil || input.ExpectedVersion < 1 {
			return input, errors.New("use a real project ID and expected_version from workspace_get")
		}
	default:
		return input, errors.New("unsupported project action")
	}
	if input.Action == "project.create" || input.Action == "project.update" {
		for field := range fields {
			if !aiProjectChangeFields[field] {
				return input, errors.New("unsupported project field: " + field)
			}
		}
		if input.Action == "project.create" {
			var create createProjectRequest
			if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
				return input, err
			}
			if _, err := projectFromCreateRequest(create); err != nil {
				return input, err
			}
		} else {
			var update updateProjectRequest
			if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
				return input, err
			}
			if len(fields) == 0 {
				return input, errors.New("at least one project field is required")
			}
		}
	} else if len(fields) != 0 {
		return input, errors.New("project lifecycle and deletion require empty changes; deletion and incomplete-task consent belong to the human decision")
	}
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

// Only explicitly changed fields leave the boundary. In particular, unrelated
// client associations and financial fields never appear in a project preview.
func aiProjectActionFields(project models.Project) map[string]any {
	return map[string]any{"name": project.Name, "description": project.Description, "start_date": project.StartDate,
		"due_date": project.DueDate, "color": project.Color, "client_id": project.ClientID, "status": project.Status}
}

func previewAIProjectAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "project.create" {
		var create createProjectRequest
		_ = json.Unmarshal(input.Changes, &create)
		project, err := projectFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if project.ClientID != nil {
			if err := requireClient(tx, *project.ClientID); err != nil {
				return preview, err
			}
		}
		preview.Label, preview.After = project.Name, aiProjectActionFields(project)
		return preview, nil
	}
	var project models.Project
	if err := tx.First(&project, "id=?", input.ProjectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return preview, newProjectRequestError(404, "PROJECT_NOT_FOUND", "Project not found")
		}
		return preview, err
	}
	if project.Version != input.ExpectedVersion {
		return preview, projectVersionConflict()
	}
	preview.Label = project.Name
	if input.Action == "project.delete" {
		_, impact, err := loadProjectDeletionImpact(tx, project.ID, project.Version)
		if err != nil {
			return preview, err
		}
		preview.Before = map[string]any{"status": project.Status, "project_version": project.Version}
		preview.After = map[string]any{
			"project_deleted":                    true,
			"project_tasks_detached":             impact.Tasks,
			"project_draft_invoices_detached":    impact.DraftInvoices,
			"project_financial_entries_detached": impact.FinancialEntries,
			"project_notes_deleted":              impact.Notes,
			"project_attachments_deleted":        impact.Attachments,
			"project_attachment_files_deleted":   impact.AttachmentFiles,
			"project_inbox_sources_coordinated":  impact.CompletionInboxSources,
		}
	} else if input.Action == "project.update" {
		if project.Status == "archived" {
			return preview, newProjectRequestError(409, "PROJECT_ARCHIVED", "Restore the project before editing it")
		}
		var update updateProjectRequest
		_ = json.Unmarshal(input.Changes, &update)
		changes, err := projectUpdates(tx, project, update)
		if err != nil {
			return preview, err
		}
		before := aiProjectActionFields(project)
		for field, value := range changes {
			preview.Before[field], preview.After[field] = before[field], value
		}
	} else {
		target, _, err := projectTransition(project, strings.TrimPrefix(input.Action, "project."))
		if err != nil {
			return preview, err
		}
		preview.Before["status"], preview.After["status"] = project.Status, target
		if input.Action == "project.complete" {
			var count int64
			if err := tx.Table("tasks").Where("project_id=? AND status<>'done'", project.ID).Count(&count).Error; err != nil {
				return preview, err
			}
			preview.After["incomplete_task_count"] = count
		}
	}
	return preview, nil
}

type aiActionResult struct {
	ID             string
	Version        int64
	CreatedTaskIDs []string
}

func (a *API) executeAIWorkspaceAction(tx *gorm.DB, input aiWorkspaceAction, requestID string, clock time.Time, confirmIncomplete, confirmForceResolve bool) (aiActionResult, error) {
	if strings.HasPrefix(input.Action, "invoice.") {
		return executeAIInvoiceAction(tx, input, requestID, clock)
	}
	now := clock.UTC().Format(time.RFC3339Nano)
	if isAITaskOutputAction(input.Action) {
		return executeAITaskOutputAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		return executeAIFinancialAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "automation.") {
		return executeAIAutomationAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "project_note.") {
		return executeAIProjectNoteAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_activity.") {
		return executeAIClientActivityAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_followup.") {
		return executeAIClientFollowupAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "focus.") {
		return executeAIFocusAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "reminder.") {
		return executeAIReminderAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "inbox.") {
		return executeAIInboxAction(tx, input, requestID, now, confirmForceResolve)
	}
	if strings.HasPrefix(input.Action, "roadmap_milestone.") {
		return a.executeAIRoadmapMilestoneAction(tx, input, requestID, clock)
	}
	if strings.HasPrefix(input.Action, "content_item.") {
		return a.executeAIContentItemAction(tx, input, requestID, clock)
	}
	if strings.HasPrefix(input.Action, "client.") {
		return executeAIClientAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_contact.") {
		return a.executeAIClientContactAction(tx, input, requestID, now)
	}
	if isAIPersonAction(input.Action) {
		return executeAIPersonAction(tx, input, requestID, now)
	}
	if strings.HasPrefix(input.Action, "tag.") {
		return executeAITagAction(tx, input, now)
	}
	if !strings.HasPrefix(input.Action, "project.") {
		task, err := executeAITaskAction(tx, input, requestID, now)
		return aiActionResult{ID: task.ID, Version: task.Version}, err
	}
	var result projectResponse
	var err error
	switch input.Action {
	case "project.create":
		var create createProjectRequest
		_ = json.Unmarshal(input.Changes, &create)
		var project models.Project
		project, err = projectFromCreateRequest(create)
		if err == nil {
			project.CreatedAt, project.UpdatedAt = now, now
			result, err = createProjectInTransaction(tx, project, requestID)
		}
	case "project.update":
		var update updateProjectRequest
		_ = json.Unmarshal(input.Changes, &update)
		result, err = updateProjectInTransaction(tx, input.ProjectID, input.ExpectedVersion, update, requestID, now)
	default:
		result, err = transitionProjectInTransaction(tx, input.ProjectID, input.ExpectedVersion, transitionProjectRequest{
			Action: strings.TrimPrefix(input.Action, "project."), ConfirmIncompleteTasks: confirmIncomplete,
		}, requestID, now)
	}
	return aiActionResult{ID: result.ID, Version: result.Version}, err
}

func aiActionTarget(input aiWorkspaceAction) (string, string) {
	if input.Action == aiAgentDelegateSpawnAction {
		return "ai_session", ""
	}
	if input.Action == aiTaskBatchCreateAction {
		return "project", input.ProjectID
	}
	if input.Action == "agent_run.cancel" || input.Action == "agent_run.retry" || input.Action == aiAgentRunRecoverOutput {
		return "agent_run", input.AgentRunID
	}
	if isAIAgentRunAction(input.Action) {
		return "agent_run", input.TaskID
	}
	if strings.HasPrefix(input.Action, "invoice.") {
		return "invoice", input.InvoiceID
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		return "financial_entry", input.FinancialEntryID
	}
	if input.Action == "automation.retry" {
		return "automation_run", input.AutomationRunID
	}
	if strings.HasPrefix(input.Action, "automation.") {
		return "automation_rule", input.AutomationRuleID
	}
	if input.Action == aiKnowledgeJobRetryAction || input.Action == aiKnowledgeJobCancelAct {
		return "knowledge_index_job", input.KnowledgeIndexJobID
	}
	if input.Action == aiKnowledgeReindexAction || input.Action == aiKnowledgeDeleteAction {
		return "knowledge_source", input.KnowledgeSourceID
	}
	if isAITaskViewAction(input.Action) {
		return "task_saved_view", input.TaskSavedViewID
	}
	if strings.HasPrefix(input.Action, "project_note.") {
		return "project_note", input.ProjectNoteID
	}
	if strings.HasPrefix(input.Action, "client_activity.") {
		return "client_activity", input.ClientActivityID
	}
	if strings.HasPrefix(input.Action, "client_followup.") {
		return "client_followup", input.ClientFollowupID
	}
	if strings.HasPrefix(input.Action, "focus.") {
		return "focus_session", input.FocusSessionID
	}
	if strings.HasPrefix(input.Action, "reminder.") {
		return "reminder", input.ReminderID
	}
	if strings.HasPrefix(input.Action, "inbox.") {
		return "inbox_item", input.InboxItemID
	}
	if strings.HasPrefix(input.Action, "roadmap_milestone.") {
		return "roadmap_milestone", input.RoadmapMilestoneID
	}
	if strings.HasPrefix(input.Action, "content_item.") {
		return "content_item", input.ContentItemID
	}
	if strings.HasPrefix(input.Action, "client.") {
		return "client", input.ClientID
	}
	if strings.HasPrefix(input.Action, "client_contact.") {
		return "client_contact", input.ClientActorLinkID
	}
	if isAIPersonAction(input.Action) {
		return "person", input.ActorID
	}
	if strings.HasPrefix(input.Action, "tag.") {
		return "tag", input.TagID
	}
	if strings.HasPrefix(input.Action, "project.") {
		return "project", input.ProjectID
	}
	return "task", input.TaskID
}
