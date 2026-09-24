package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const aiTaskBatchCreateAction = "task.batch_create"

var aiBatchDraftKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,49}$`)

type aiTaskBatchCreateDraft struct {
	Key                string   `json:"key"`
	ParentKey          string   `json:"parent_key,omitempty"`
	Title              string   `json:"title"`
	Description        *string  `json:"description,omitempty"`
	Kind               *string  `json:"kind,omitempty"`
	Priority           *string  `json:"priority,omitempty"`
	PlannedDate        *string  `json:"planned_date,omitempty"`
	DueDate            *string  `json:"due_date,omitempty"`
	EstimatedMinutes   *int     `json:"estimated_minutes,omitempty"`
	CompletionCriteria *string  `json:"completion_criteria,omitempty"`
	ReviewPolicy       *string  `json:"review_policy,omitempty"`
	TagIDs             []string `json:"tag_ids,omitempty"`
	AssigneeActorID    string   `json:"assignee_actor_id,omitempty"`
}

type aiTaskBatchCreateChanges struct {
	Drafts []aiTaskBatchCreateDraft `json:"drafts"`
}

type aiTaskBatchCreateItemPreview struct {
	Key                string                         `json:"key"`
	ParentKey          string                         `json:"parent_key,omitempty"`
	Title              string                         `json:"title"`
	Description        string                         `json:"description"`
	Kind               string                         `json:"kind"`
	Priority           string                         `json:"priority"`
	PlannedDate        *string                        `json:"planned_date"`
	DueDate            *string                        `json:"due_date"`
	EstimatedMinutes   *int                           `json:"estimated_minutes"`
	CompletionCriteria string                         `json:"completion_criteria"`
	ReviewPolicy       string                         `json:"review_policy"`
	TagIDs             []string                       `json:"tag_ids"`
	TagNames           []string                       `json:"tag_names"`
	Assignments        []aiTaskBatchAssignmentPreview `json:"assignments,omitempty"`
}

type aiTaskBatchAssignmentPreview struct {
	Role         string `json:"role"`
	ActorID      string `json:"actor_id"`
	ActorName    string `json:"actor_name"`
	ActorType    string `json:"actor_type"`
	ActorStatus  string `json:"actor_status"`
	ActorVersion int64  `json:"actor_version"`
}

type aiTaskBatchCreatePreview struct {
	ProjectID      string                         `json:"project_id"`
	ProjectName    string                         `json:"project_name"`
	ProjectVersion int64                          `json:"project_version"`
	Count          int                            `json:"count"`
	Items          []aiTaskBatchCreateItemPreview `json:"items"`
}

type aiTaskBatchCreateResultItem struct {
	Key   string `json:"key"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Route string `json:"route"`
}

type aiTaskBatchCreateResult struct {
	ProjectID string                        `json:"project_id"`
	Count     int                           `json:"count"`
	Items     []aiTaskBatchCreateResultItem `json:"items"`
}

func addAITaskBatchCreateSchema(properties map[string]any) {
	properties["action"].(map[string]any)["enum"] = append(properties["action"].(map[string]any)["enum"].([]any), aiTaskBatchCreateAction)
	properties["project_id"].(map[string]any)["description"] = "Existing Project UUID for project actions and task.batch_create; never guess it"
	properties["expected_version"].(map[string]any)["description"] = "Current target version for existing-record actions and task.batch_create; omitted for single-entity create actions"
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Project task breakdown (work+actions): task.batch_create requires top-level project_id and expected_version from workspace_get(project), changes.drafts is 1-20 new tasks in display/creation order. Each draft needs a unique short key and title; parent_key may reference only an earlier draft key. Optional description, kind, priority, planned_date, due_date, estimated_minutes, completion_criteria, review_policy and tag_ids follow Task create rules. Optional assignee_actor_id must be a real eligible Actor from workspace_task_options(type=actor,role=assignee); for a manual-review assigned draft the active owner is also set as reviewer. All tasks belong to the same project; no Agent execution or Inbox relation. One human confirmation creates and assigns the complete batch atomically, or none."
	changes["properties"].(map[string]any)["drafts"] = map[string]any{
		"type": "array", "minItems": 1, "maxItems": maxAITaskBatchItems,
		"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"key", "title"},
			"properties": map[string]any{
				"key":                 map[string]any{"type": "string", "pattern": "^[a-zA-Z][a-zA-Z0-9_-]{0,49}$"},
				"parent_key":          map[string]any{"type": "string"},
				"title":               map[string]any{"type": "string", "minLength": 2, "maxLength": 200},
				"description":         map[string]any{"type": "string", "maxLength": 10000},
				"kind":                map[string]any{"type": "string", "enum": []string{"work", "review", "followup", "reminder"}},
				"priority":            map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}},
				"planned_date":        map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"due_date":            map[string]any{"type": "string", "description": "RFC3339 timestamp with zone"},
				"estimated_minutes":   map[string]any{"type": "integer", "minimum": 0},
				"completion_criteria": map[string]any{"type": "string", "maxLength": 10000},
				"review_policy":       map[string]any{"type": "string", "enum": []string{"none", "manual"}},
				"tag_ids":             map[string]any{"type": "array", "maxItems": 20, "uniqueItems": true, "items": map[string]any{"type": "string", "format": "uuid"}},
				"assignee_actor_id":   map[string]any{"type": "string", "format": "uuid", "description": "Optional real eligible initial assignee Actor ID; does not start Agent execution"},
			},
		},
	}
}

func parseAITaskBatchCreateAction(input aiWorkspaceAction, arguments json.RawMessage) (aiWorkspaceAction, error) {
	if _, err := decodeAIReadQueryObject(arguments, map[string]bool{"action": true, "project_id": true, "expected_version": true, "changes": true}); err != nil {
		return input, err
	}
	id, err := uuid.Parse(input.ProjectID)
	if err != nil || id.String() != input.ProjectID || input.ExpectedVersion < 1 {
		return input, errors.New("task.batch_create requires a canonical project_id and positive expected_version")
	}
	fields, err := decodeAIReadQueryObject(input.Changes, map[string]bool{"drafts": true})
	if err != nil {
		return input, err
	}
	var draftJSON []json.RawMessage
	if json.Unmarshal(fields["drafts"], &draftJSON) != nil || len(draftJSON) < 1 || len(draftJSON) > maxAITaskBatchItems {
		return input, fmt.Errorf("drafts must contain 1-%d tasks", maxAITaskBatchItems)
	}
	changes := aiTaskBatchCreateChanges{Drafts: make([]aiTaskBatchCreateDraft, 0, len(draftJSON))}
	seen := map[string]bool{}
	for _, rawDraft := range draftJSON {
		draftFields, err := decodeAIReadQueryObject(rawDraft, map[string]bool{
			"key": true, "parent_key": true, "title": true, "description": true, "kind": true,
			"priority": true, "planned_date": true, "due_date": true, "estimated_minutes": true,
			"completion_criteria": true, "review_policy": true, "tag_ids": true, "assignee_actor_id": true,
		})
		if err != nil {
			return input, err
		}
		var draft aiTaskBatchCreateDraft
		if err := decodeStrictToolArguments(rawDraft, &draft); err != nil {
			return input, err
		}
		if !aiBatchDraftKey.MatchString(draft.Key) || seen[draft.Key] {
			return input, errors.New("draft key must be unique and use a short ASCII identifier")
		}
		if _, hasParent := draftFields["parent_key"]; hasParent && !seen[draft.ParentKey] {
			return input, errors.New("parent_key must reference an earlier draft")
		}
		seen[draft.Key] = true
		if _, err := taskFromCreateRequest(aiBatchDraftTaskRequest(input.ProjectID, draft, nil)); err != nil {
			return input, err
		}
		if _, err := validateAITaskTagIDs(draft.TagIDs); err != nil {
			return input, err
		}
		if draft.AssigneeActorID != "" {
			if id, err := uuid.Parse(draft.AssigneeActorID); err != nil || id.String() != draft.AssigneeActorID {
				return input, errors.New("assignee_actor_id must be a canonical Actor UUID")
			}
		}
		changes.Drafts = append(changes.Drafts, draft)
	}
	input.Changes, _ = json.Marshal(changes)
	return input, nil
}

func aiBatchDraftTaskRequest(projectID string, draft aiTaskBatchCreateDraft, parentID *string) createTaskRequest {
	return createTaskRequest{
		Title: draft.Title, Description: draft.Description, Kind: draft.Kind, Priority: draft.Priority,
		ProjectID: &projectID, ParentTaskID: parentID, CompletionCriteria: draft.CompletionCriteria,
		TagIDs: draft.TagIDs, DueDate: draft.DueDate, PlannedDate: draft.PlannedDate,
		EstimatedMinutes: draft.EstimatedMinutes, ReviewPolicy: draft.ReviewPolicy,
	}
}

func decodeAITaskBatchCreateChanges(input aiWorkspaceAction) (aiTaskBatchCreateChanges, error) {
	var changes aiTaskBatchCreateChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return changes, err
	}
	if len(changes.Drafts) < 1 || len(changes.Drafts) > maxAITaskBatchItems {
		return changes, errors.New("invalid task batch create payload")
	}
	return changes, nil
}

func previewAITaskBatchCreateAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	changes, err := decodeAITaskBatchCreateChanges(input)
	if err != nil {
		return preview, err
	}
	if err := requireAssignableProject(tx, input.ProjectID); err != nil {
		return preview, err
	}
	var project models.Project
	if err := tx.Select("id", "name", "version", "status").First(&project, "id = ?", input.ProjectID).Error; err != nil {
		return preview, err
	}
	if project.Version != input.ExpectedVersion {
		return preview, newProjectRequestError(409, "VERSION_CONFLICT", "Project changed; request a fresh proposal")
	}
	batch := aiTaskBatchCreatePreview{ProjectID: project.ID, ProjectName: project.Name, ProjectVersion: project.Version, Count: len(changes.Drafts), Items: make([]aiTaskBatchCreateItemPreview, 0, len(changes.Drafts))}
	for _, draft := range changes.Drafts {
		task, err := taskFromCreateRequest(aiBatchDraftTaskRequest(project.ID, draft, nil))
		if err != nil {
			return preview, err
		}
		tagIDs, err := validateAITaskTagIDs(draft.TagIDs)
		if err != nil {
			return preview, err
		}
		tags, err := loadAITaskTags(tx, tagIDs)
		if err != nil {
			return preview, err
		}
		_, tagNames := aiTaskTagValues(tags)
		var assignments []aiTaskBatchAssignmentPreview
		if draft.AssigneeActorID != "" {
			assignee, err := previewAITaskBatchAssignment(tx, draft.AssigneeActorID, "assignee")
			if err != nil {
				return preview, err
			}
			assignments = append(assignments, assignee)
			if task.ReviewPolicy == "manual" {
				reviewer, err := previewAITaskBatchAssignment(tx, models.BuiltinOwnerActorID, "reviewer")
				if err != nil {
					return preview, err
				}
				assignments = append(assignments, reviewer)
			}
		}
		batch.Items = append(batch.Items, aiTaskBatchCreateItemPreview{
			Key: draft.Key, ParentKey: draft.ParentKey, Title: task.Title, Description: task.Description,
			Kind: task.Kind, Priority: task.Priority, PlannedDate: task.PlannedDate, DueDate: task.DueDate,
			EstimatedMinutes: task.EstimatedMinutes, CompletionCriteria: task.CompletionCriteria,
			ReviewPolicy: task.ReviewPolicy, TagIDs: tagIDs, TagNames: tagNames, Assignments: assignments,
		})
	}
	preview.Label = fmt.Sprintf("在项目「%s」创建 %d 个任务", project.Name, batch.Count)
	preview.After["count"] = batch.Count
	preview.TaskBatchCreate = &batch
	return preview, nil
}

func previewAITaskBatchAssignment(tx *gorm.DB, actorID, role string) (aiTaskBatchAssignmentPreview, error) {
	if err := requireAssignmentActor(tx, actorID, role); err != nil {
		return aiTaskBatchAssignmentPreview{}, err
	}
	var actor models.Actor
	if err := tx.Select("id", "display_name", "type", "status", "version").First(&actor, "id=?", actorID).Error; err != nil {
		return aiTaskBatchAssignmentPreview{}, err
	}
	return aiTaskBatchAssignmentPreview{Role: role, ActorID: actor.ID, ActorName: actor.DisplayName,
		ActorType: actor.Type, ActorStatus: actor.Status, ActorVersion: actor.Version}, nil
}

func aiTaskBatchCreatedID(proposalID string, index int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("opc-ai-task-batch-create:"+proposalID+":"+strconv.Itoa(index))).String()
}

func executeAITaskBatchCreateAction(tx *gorm.DB, input aiWorkspaceAction, proposalID, requestID, now string) error {
	changes, err := decodeAITaskBatchCreateChanges(input)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	for index, draft := range changes.Drafts {
		var parentID *string
		if draft.ParentKey != "" {
			parent, ok := ids[draft.ParentKey]
			if !ok {
				return errors.New("invalid parent_key in stored proposal")
			}
			parentID = &parent
		}
		task, err := taskFromCreateRequest(aiBatchDraftTaskRequest(input.ProjectID, draft, parentID))
		if err != nil {
			return err
		}
		task.ID = aiTaskBatchCreatedID(proposalID, index)
		task.CreatedAt, task.UpdatedAt = now, now
		tags, err := validateAITaskTagIDs(draft.TagIDs)
		if err != nil {
			return err
		}
		created, err := createTaskInTransaction(tx, task, tags, requestID)
		if err != nil {
			return err
		}
		if draft.AssigneeActorID != "" {
			assigned, err := createAssignmentInTransaction(tx, task.ID, created.Version, "assignee", draft.AssigneeActorID, requestID, now)
			if err != nil {
				return err
			}
			if created.ReviewPolicy == "manual" {
				if _, err := createAssignmentInTransaction(tx, task.ID, assigned.Task.Version, "reviewer", models.BuiltinOwnerActorID, requestID, now); err != nil {
					return err
				}
			}
		}
		ids[draft.Key] = task.ID
	}
	return nil
}

func aiTaskBatchCreateReceipt(proposalID string, preview *aiTaskBatchCreatePreview) *aiTaskBatchCreateResult {
	result := &aiTaskBatchCreateResult{ProjectID: preview.ProjectID, Count: preview.Count, Items: make([]aiTaskBatchCreateResultItem, 0, preview.Count)}
	for index, item := range preview.Items {
		id := aiTaskBatchCreatedID(proposalID, index)
		result.Items = append(result.Items, aiTaskBatchCreateResultItem{Key: item.Key, ID: id, Title: item.Title, Route: searchRoute("task", id)})
	}
	return result
}
