package api

import (
	"encoding/json"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Stable, displayable normalized drafts. No speculative Task UUIDs/timestamps.
type aiInboxSplitDraft struct {
	Key        string         `json:"key"`
	ParentKey  string         `json:"parent_key"`
	Fields     map[string]any `json:"fields"`
	Assignee   aiTaskOption   `json:"assignee"`
	Reviewer   *aiTaskOption  `json:"reviewer"`
	Project    *aiTaskOption  `json:"project"`
	Tags       []aiTaskOption `json:"tags"`
	IsRequired bool           `json:"is_required"`
}

func parseAIInboxSplit(input aiWorkspaceAction) (normalizedInboxSplit, error) {
	var request splitInboxItemRequest
	if err := decodeStrictToolArguments(input.Changes, &request); err != nil {
		return normalizedInboxSplit{}, err
	}
	return normalizeInboxSplit(request)
}

func previewAIInboxSplit(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	normalized, err := parseAIInboxSplit(input)
	if err != nil {
		return preview, err
	}
	current, err := prepareInboxSplit(tx, input.InboxItemID, input.ExpectedVersion, normalized)
	if err != nil {
		return preview, err
	}
	progress, err := loadInboxTaskProgress(tx, current.ID)
	if err != nil {
		return preview, err
	}
	preview.Label = current.Title
	preview.Before = map[string]any{"status": current.Status, "resolution_policy": current.ResolutionPolicy, "active_task_count": progress.ActiveTotal, "required_task_count": progress.RequiredTotal, "required_done_count": progress.RequiredDone, "snoozed_until": current.SnoozedUntil}
	required := progress.RequiredTotal
	preview.Tasks = make([]aiInboxSplitDraft, 0, len(normalized.Tasks))
	for _, entry := range normalized.Tasks {
		draft := aiInboxSplitDraft{Key: entry.Key, ParentKey: entry.ParentKey, Fields: aiTaskActionFields(entry.Task), Tags: []aiTaskOption{}, IsRequired: entry.IsRequired}
		// Assignee references are valid only within the same domain preflight above.
		if err := tx.Model(&models.Actor{}).Select("id, display_name AS label, type, version").Where("id=?", entry.AssigneeActorID).Scan(&draft.Assignee).Error; err != nil {
			return preview, err
		}
		if entry.Task.ReviewPolicy == "manual" {
			draft.Reviewer = &aiTaskOption{}
			if err := tx.Model(&models.Actor{}).Select("id, display_name AS label, type, version").Where("id=?", models.BuiltinOwnerActorID).Scan(draft.Reviewer).Error; err != nil {
				return preview, err
			}
		}
		if entry.Task.ProjectID != nil {
			draft.Project = &aiTaskOption{}
			if err := tx.Model(&models.Project{}).Select("id, name AS label, 'project' AS type, version").Where("id=?", *entry.Task.ProjectID).Scan(draft.Project).Error; err != nil {
				return preview, err
			}
		}
		if len(entry.TagIDs) > 0 {
			if err := tx.Model(&models.Tag{}).Select("id, name AS label, 'tag' AS type, version").Where("id IN ?", entry.TagIDs).Order("id").Scan(&draft.Tags).Error; err != nil {
				return preview, err
			}
		}
		if entry.IsRequired {
			required++
		}
		preview.Tasks = append(preview.Tasks, draft)
	}
	preview.After = map[string]any{"status": "tracking", "resolution_policy": normalized.ResolutionPolicy, "active_task_count": progress.ActiveTotal + int64(len(normalized.Tasks)), "required_task_count": required, "required_done_count": progress.RequiredDone, "snoozed_until": nil, "created_task_count": len(normalized.Tasks)}
	return preview, nil
}

// Schema reuses the native draft fields, with no status, parent Task ID or
// reviewer override. The new parent must be an earlier key in the same batch.
func aiInboxSplitSchemaFields() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"resolution_policy": json.RawMessage(`{"type":"string","enum":["manual","all_required_tasks_done"],"description":"inbox.split only. Default all_required_tasks_done requires at least one required new task."}`),
		"tasks":             json.RawMessage(`{"type":"array","minItems":1,"maxItems":20,"description":"inbox.split only; atomic batch. Resolve actor/tag IDs with workspace_task_options and project IDs with workspace_search. Assignment does not start execution.","items":{"type":"object","additionalProperties":false,"required":["key","title","is_required","assignee_actor_id"],"properties":{"key":{"type":"string","minLength":1,"maxLength":50},"parent_key":{"type":["string","null"],"description":"Earlier draft key in this batch; omit for root"},"title":{"type":"string","minLength":2,"maxLength":200},"description":{"type":["string","null"],"maxLength":10000},"kind":{"type":"string","enum":["work","review","followup","reminder"]},"priority":{"type":"string","enum":["P0","P1","P2","P3"]},"project_id":{"type":["string","null"]},"completion_criteria":{"type":["string","null"],"maxLength":10000},"tag_ids":{"type":"array","items":{"type":"string","format":"uuid"}},"due_date":{"type":["string","null"],"description":"RFC3339 with timezone"},"planned_date":{"type":["string","null"],"description":"YYYY-MM-DD"},"estimated_minutes":{"type":["integer","null"],"minimum":0},"review_policy":{"type":"string","enum":["none","manual"]},"is_required":{"type":"boolean"},"assignee_actor_id":{"type":"string","format":"uuid"}}}}`),
	}
}
