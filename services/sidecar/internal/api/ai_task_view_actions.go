package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Task saved views are local filter presets. The AI track can read them and
// propose create/update/delete, but every proposal is a human-confirmed domain
// command that reuses the same transaction as the Tasks page.
const (
	aiTaskViewCreateAction = "task_view.create"
	aiTaskViewUpdateAction = "task_view.update"
	aiTaskViewDeleteAction = "task_view.delete"
	aiTaskViewListLimit    = 20
)

func isAITaskViewAction(action string) bool {
	switch action {
	case aiTaskViewCreateAction, aiTaskViewUpdateAction, aiTaskViewDeleteAction:
		return true
	default:
		return false
	}
}

type aiTaskViewPreview struct {
	ID          string                  `json:"id,omitempty"`
	Name        string                  `json:"name"`
	Definition  taskSavedViewDefinition `json:"definition"`
	Version     int64                   `json:"version,omitempty"`
	NextVersion int64                   `json:"next_version,omitempty"`
	ProjectName string                  `json:"project_name,omitempty"`
	ClientName  string                  `json:"client_name,omitempty"`
	TagNames    []string                `json:"tag_names,omitempty"`
	Deleted     bool                    `json:"deleted,omitempty"`
}

type aiTaskViewCreateChanges struct {
	Name       string                  `json:"name"`
	Definition taskSavedViewDefinition `json:"definition"`
}

type aiTaskViewUpdateChanges struct {
	Name       *string                  `json:"name"`
	Definition *taskSavedViewDefinition `json:"definition"`
}

func parseAITaskViewAction(input aiWorkspaceAction, fields map[string]json.RawMessage, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if input.TaskID != "" || input.ProjectID != "" || input.InboxItemID != "" || input.ReminderID != "" ||
		input.FocusSessionID != "" || input.ClientFollowupID != "" || input.ClientActivityID != "" ||
		input.AutomationRuleID != "" || input.AutomationRunID != "" || input.RoadmapMilestoneID != "" ||
		input.ContentItemID != "" || input.ClientID != "" || input.ClientActorLinkID != "" || input.ActorID != "" ||
		input.TagID != "" || input.ProjectNoteID != "" || input.InvoiceID != "" || input.FinancialEntryID != "" ||
		input.AgentRunID != "" || input.KnowledgeSourceID != "" || input.KnowledgeIndexJobID != "" {
		return input, errors.New("task view actions only accept task_saved_view_id as the target")
	}
	if input.Action == aiTaskViewCreateAction {
		if input.TaskSavedViewID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("creating a task view does not accept an id or version")
		}
		for field, value := range fields {
			if (field != "name" && field != "definition") || string(value) == "null" {
				return input, errors.New("creating a task view requires non-null name and definition")
			}
		}
		var create aiTaskViewCreateChanges
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		name, err := validateTaskSavedViewName(create.Name)
		if err != nil {
			return input, err
		}
		normalizedDefinition, _, err := normalizeTaskSavedViewDefinition(create.Definition)
		if err != nil {
			return input, err
		}
		input.Changes, _ = json.Marshal(map[string]any{"name": name, "definition": normalizedDefinition})
		return input, nil
	}
	if !isValidKnowledgeID(input.TaskSavedViewID) || input.ExpectedVersion < 1 {
		return input, errors.New("use the canonical task_saved_view_id and expected_version from workspace_task_views")
	}
	if _, present := raw["knowledge_source_id"]; present {
		return input, errors.New("task view actions do not accept knowledge targets")
	}
	if input.Action == aiTaskViewDeleteAction {
		if len(fields) != 0 {
			return input, errors.New("deleting a task view requires empty changes; deletion consent belongs to the human decision")
		}
		input.Changes = json.RawMessage(`{}`)
		return input, nil
	}
	for field, value := range fields {
		if (field != "name" && field != "definition") || string(value) == "null" {
			return input, errors.New("updating a task view only accepts a non-null name or definition")
		}
	}
	var update aiTaskViewUpdateChanges
	if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
		return input, err
	}
	if update.Name == nil && update.Definition == nil {
		return input, errors.New("at least one of name or definition is required")
	}
	canonical := map[string]any{}
	if update.Name != nil {
		name, err := validateTaskSavedViewName(*update.Name)
		if err != nil {
			return input, err
		}
		canonical["name"] = name
	}
	if update.Definition != nil {
		normalizedDefinition, _, err := normalizeTaskSavedViewDefinition(*update.Definition)
		if err != nil {
			return input, err
		}
		canonical["definition"] = normalizedDefinition
	}
	input.Changes, _ = json.Marshal(canonical)
	return input, nil
}

func aiTaskViewDefinitionPatch(input aiWorkspaceAction) (name *string, definition *taskSavedViewDefinition) {
	switch input.Action {
	case aiTaskViewCreateAction:
		var create aiTaskViewCreateChanges
		if json.Unmarshal(input.Changes, &create) != nil {
			return nil, nil
		}
		normalized, _, err := normalizeTaskSavedViewDefinition(create.Definition)
		if err != nil {
			return nil, nil
		}
		return &create.Name, &normalized
	case aiTaskViewUpdateAction:
		var update aiTaskViewUpdateChanges
		if json.Unmarshal(input.Changes, &update) != nil {
			return nil, nil
		}
		if update.Definition != nil {
			normalized, _, err := normalizeTaskSavedViewDefinition(*update.Definition)
			if err != nil {
				return nil, nil
			}
			update.Definition = &normalized
		}
		return update.Name, update.Definition
	default:
		return nil, nil
	}
}

func loadAITaskViewPreview(tx *gorm.DB, id string) (aiTaskViewPreview, error) {
	_, response, err := loadTaskSavedViewInTransaction(tx, id)
	if err != nil {
		return aiTaskViewPreview{}, err
	}
	return aiTaskViewPreviewFromResponse(response, false)
}

func aiTaskViewPreviewFromResponse(response taskSavedViewResponse, deleted bool) (aiTaskViewPreview, error) {
	preview := aiTaskViewPreview{
		ID: response.ID, Name: response.Name, Definition: response.Definition,
		Version: response.Version, NextVersion: response.Version + 1, Deleted: deleted,
	}
	return preview, nil
}

// resolveAITaskViewLabels adds the human-readable project/client/tag names the
// reviewer needs. Missing or deleted references simply stay unnamed; the ids
// remain the authoritative identity.
func resolveAITaskViewLabels(tx *gorm.DB, preview *aiTaskViewPreview) error {
	definition := preview.Definition
	if definition.ProjectID != "" {
		var project models.Project
		if err := tx.Select("name").First(&project, "id = ?", definition.ProjectID).Error; err == nil {
			preview.ProjectName = project.Name
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if definition.ClientID != "" {
		var client models.Client
		if err := tx.Select("name").First(&client, "id = ?", definition.ClientID).Error; err == nil {
			preview.ClientName = client.Name
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if len(definition.TagIDs) > 0 {
		var tags []models.Tag
		if err := tx.Select("id", "name").Where("id IN ?", definition.TagIDs).Order("lower(name) ASC").Find(&tags).Error; err != nil {
			return err
		}
		names := make([]string, 0, len(tags))
		for _, tag := range tags {
			names = append(names, tag.Name)
		}
		preview.TagNames = names
	}
	return nil
}

func previewAITaskViewAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == aiTaskViewCreateAction {
		name, definition := aiTaskViewDefinitionPatch(input)
		if name == nil || definition == nil {
			return preview, newProjectRequestError(422, "VALIDATION_ERROR", "name and definition are required")
		}
		var count int64
		if err := tx.Model(&models.TaskSavedView{}).Count(&count).Error; err != nil {
			return preview, err
		}
		if count >= maxTaskSavedViews {
			return preview, newProjectRequestError(409, "TASK_SAVED_VIEW_LIMIT_REACHED", "At most 20 task saved views are allowed")
		}
		if err := requireUniqueTaskSavedViewName(tx, *name, ""); err != nil {
			return preview, err
		}
		view := aiTaskViewPreview{Name: *name, Definition: *definition, NextVersion: 1}
		if err := resolveAITaskViewLabels(tx, &view); err != nil {
			return preview, err
		}
		preview.Label = *name
		preview.After["name"] = *name
		preview.TaskView = &view
		return preview, nil
	}
	view, err := loadAITaskViewPreview(tx, input.TaskSavedViewID)
	if err != nil {
		return preview, err
	}
	if view.Version != input.ExpectedVersion {
		return preview, taskVersionConflict()
	}
	preview.Label = view.Name
	preview.Before["name"] = view.Name
	if input.Action == aiTaskViewDeleteAction {
		view.Deleted = true
		view.NextVersion = 0
		if err := resolveAITaskViewLabels(tx, &view); err != nil {
			return preview, err
		}
		preview.TaskView = &view
		return preview, nil
	}
	name, definition := aiTaskViewDefinitionPatch(input)
	if name != nil {
		if err := requireUniqueTaskSavedViewName(tx, *name, view.ID); err != nil {
			return preview, err
		}
		view.Name = *name
	}
	if definition != nil {
		view.Definition = *definition
	}
	if err := resolveAITaskViewLabels(tx, &view); err != nil {
		return preview, err
	}
	preview.After["name"] = view.Name
	preview.TaskView = &view
	return preview, nil
}

func executeAITaskViewAction(tx *gorm.DB, input aiWorkspaceAction, now string) (aiActionResult, error) {
	switch input.Action {
	case aiTaskViewCreateAction:
		name, definition := aiTaskViewDefinitionPatch(input)
		if name == nil || definition == nil {
			return aiActionResult{}, errors.New("name and definition are required")
		}
		created, err := createTaskSavedViewInTransaction(tx, *name, *definition, now)
		if err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: created.ID, Version: created.Version}, nil
	case aiTaskViewUpdateAction:
		name, definition := aiTaskViewDefinitionPatch(input)
		updated, err := updateTaskSavedViewInTransaction(tx, input.TaskSavedViewID, input.ExpectedVersion, name, definition, now)
		if err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: updated.ID, Version: updated.Version}, nil
	case aiTaskViewDeleteAction:
		if err := deleteTaskSavedViewInTransaction(tx, input.TaskSavedViewID, input.ExpectedVersion); err != nil {
			return aiActionResult{}, err
		}
		return aiActionResult{ID: input.TaskSavedViewID, Version: input.ExpectedVersion}, nil
	default:
		return aiActionResult{}, errors.New("unsupported task view action")
	}
}

func aiTaskViewRoute() string { return "/tasks" }

type aiTaskViewListItem struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Definition taskSavedViewDefinition `json:"definition"`
	Version    int64                   `json:"version"`
	UpdatedAt  string                  `json:"updated_at"`
	Route      string                  `json:"route"`
}

// workspace_task_views is a metadata-only read of local task filter presets.
// It grants no write and returns no Task bodies.
func (t *aiWorkspaceTool) taskViews(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		View   string `json:"view"`
		ID     string `json:"id"`
		Query  string `json:"query"`
		Limit  *int   `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(args, &fields)
	for key, raw := range fields {
		allowed := key == "view" || key == "query" || key == "limit" || key == "offset" || (input.View == "view" && key == "id")
		if !allowed || string(raw) == "null" {
			return nil, errors.New("unexpected or null task view argument")
		}
	}
	query := strings.TrimSpace(input.Query)
	if utf8.RuneCountInString(query) > 200 {
		return nil, errors.New("task view query must be at most 200 characters")
	}
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > aiTaskViewListLimit || input.Offset < 0 || input.Offset > 1000 {
		return nil, errors.New("task view pages require limit 1-20 and offset 0-1000")
	}
	if input.View == "view" && !isValidKnowledgeID(input.ID) {
		return nil, errors.New("task view detail requires a canonical UUID")
	}
	db := t.api.db.WithContext(ctx)
	switch input.View {
	case "view":
		_, response, err := loadTaskSavedViewInTransaction(db, input.ID)
		if err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		return map[string]any{
			"view": "view", "item": aiTaskViewListItem{
				ID: response.ID, Name: response.Name, Definition: response.Definition,
				Version: response.Version, UpdatedAt: response.UpdatedAt, Route: aiTaskViewRoute(),
			},
			"server_now": t.api.options.Now().UTC().Format(time.RFC3339Nano),
			"instruction": "Read the current version before proposing task_view.update or task_view.delete. " +
				"Views are local UI presets; they never change tasks by themselves.",
		}, nil
	case "list", "":
		base := db.Model(&models.TaskSavedView{})
		if query != "" {
			base = base.Where("name LIKE ? ESCAPE '\\'", "%"+escapeKnowledgeLike(query)+"%")
		}
		var total int64
		if err := base.Count(&total).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		var rows []models.TaskSavedView
		if err := base.Order("lower(name) ASC").Order("id ASC").
			Offset(input.Offset).Limit(limit + 1).Find(&rows).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		items := make([]aiTaskViewListItem, 0, len(rows))
		for _, row := range rows {
			response, err := taskSavedViewFromModel(row)
			if err != nil {
				return nil, errors.New("stored task view is invalid")
			}
			items = append(items, aiTaskViewListItem{
				ID: response.ID, Name: response.Name, Definition: response.Definition,
				Version: response.Version, UpdatedAt: response.UpdatedAt, Route: aiTaskViewRoute(),
			})
		}
		var next *int
		if hasMore && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		return map[string]any{
			"view": "list", "items": items, "total_items": total, "has_more": hasMore,
			"next_offset": next, "window_limited": hasMore && next == nil,
			"server_now": t.api.options.Now().UTC().Format(time.RFC3339Nano),
			"instruction": "Task filter presets only; no Task bodies. At most 20 views exist. " +
				"Applying a view is a local UI action the user performs in the Tasks page.",
		}, nil
	default:
		return nil, errors.New("task view must be list or view")
	}
}
