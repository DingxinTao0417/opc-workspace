package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Task discovery uses the same filters/order as the native list, but never
// loads the full Task model or its bodies into a provider-visible response.
type aiTaskFilters struct {
	taskSavedViewDefinition
	PlannedState string `json:"planned_state"`
	DueState     string `json:"due_state"`
	ParentTaskID string `json:"parent_task_id"`
	RootOnly     bool   `json:"root_only"`
}

type aiTaskListItem struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	Kind        string  `json:"kind"`
	PlannedDate *string `json:"planned_date"`
	DueDate     *string `json:"due_date"`
	Version     int64   `json:"version"`
	UpdatedAt   string  `json:"updated_at"`
	Route       string  `json:"route"`
}

const aiTaskListProjection = `tasks.id, substr(tasks.title,1,200) AS label, tasks.status, tasks.priority, tasks.kind,
		tasks.planned_date, tasks.due_date, tasks.version, tasks.updated_at`

// A full 20-ID exact set cannot ask for a smaller page. Keep every row within
// the 24 KiB workspace result budget even when JSON escapes every label rune.
const aiTaskExactProjection = `tasks.id, CASE WHEN length(tasks.title)>100 THEN substr(tasks.title,1,100)||'…' ELSE tasks.title END AS label,
		tasks.status, tasks.priority, tasks.kind, tasks.planned_date, tasks.due_date, tasks.version, tasks.updated_at`

func aiTasksSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"task_ids":{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","format":"uuid"},"description":"Exact 1-20 Task IDs, in request order; cannot combine with filters or paging. Missing Task rejects whole selection."},"filters":{"type":"object","additionalProperties":false,"properties":{"q":{"type":"string","maxLength":200},"status":{"type":"string"},"priority":{"type":"string"},"kind":{"type":"string"},"project_id":{"type":"string"},"client_id":{"type":"string"},"tag_ids":{"type":"array","maxItems":20,"items":{"type":"string"}},"planned_date":{"type":"string"},"planned_from":{"type":"string"},"planned_to":{"type":"string"},"planned_state":{"type":"string"},"due_from":{"type":"string"},"due_to":{"type":"string"},"due_state":{"type":"string"},"parent_task_id":{"type":"string"},"root_only":{"type":"boolean"},"sort":{"type":"string"}}},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

// encoding/json accepts case-insensitive struct field aliases and merges
// repeated object fields. This tool also checks field presence, so validate
// exact, unique keys before decoding to ensure validation and execution see
// precisely the same object. Reuse for bounded read-only workspace queries.
func decodeAIReadQueryObject(raw json.RawMessage, allowed map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("workspace query and filters must be objects")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid workspace query object")
		}
		key, ok := keyToken.(string)
		if !ok || !allowed[key] {
			return nil, errors.New("unexpected workspace query field; use exact schema keys")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("duplicate workspace query field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || string(value) == "null" {
			return nil, errors.New("workspace query fields must be valid and non-null")
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("invalid workspace query object")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("workspace query has trailing data")
	}
	return fields, nil
}

func parseAITaskFilters(input aiTaskFilters, fields map[string]json.RawMessage) (taskListFilters, error) {
	for _, raw := range fields {
		if string(raw) == "null" {
			return taskListFilters{}, errors.New("task filters cannot be null")
		}
	}
	if len(input.TagIDs) > 20 {
		return taskListFilters{}, errors.New("tag_ids must contain at most 20 IDs")
	}
	definition, _, err := normalizeTaskSavedViewDefinition(input.taskSavedViewDefinition)
	if err != nil {
		return taskListFilters{}, err
	}
	filters := taskListFilters{
		Search: definition.Query, Status: definition.Status, Priority: definition.Priority, Kind: definition.Kind,
		ProjectID: definition.ProjectID, ClientID: definition.ClientID, TagIDs: definition.TagIDs,
		PlannedDate: definition.PlannedDate, PlannedFrom: definition.PlannedFrom, PlannedTo: definition.PlannedTo,
		DueFrom: definition.DueFrom, DueTo: definition.DueTo, Sort: definition.Sort,
		PlannedState: strings.TrimSpace(input.PlannedState), DueState: strings.TrimSpace(input.DueState),
		ParentTaskID: strings.TrimSpace(input.ParentTaskID), RootOnly: input.RootOnly,
	}
	if filters.PlannedState != "" && filters.PlannedState != "scheduled" && filters.PlannedState != "unscheduled" {
		return filters, errors.New("planned_state must be scheduled or unscheduled")
	}
	if filters.PlannedState == "unscheduled" && (filters.PlannedDate != "" || filters.PlannedFrom != "" || filters.PlannedTo != "") {
		return filters, errors.New("unscheduled cannot be combined with planned dates")
	}
	if _, present := fields["due_state"]; present {
		if filters.DueState != "overdue" && filters.DueState != "due_soon" {
			return filters, errors.New("due_state must be overdue or due_soon")
		}
		_, fromPresent := fields["due_from"]
		_, toPresent := fields["due_to"]
		_, statusPresent := fields["status"]
		if fromPresent || toPresent || (statusPresent && filters.Status != "active") {
			return filters, errors.New("due_state excludes date ranges and only accepts status=active")
		}
	}
	if filters.ParentTaskID != "" {
		if _, err := uuid.Parse(filters.ParentTaskID); err != nil {
			return filters, errors.New("parent_task_id must be a UUID")
		}
	}
	return filters, nil
}

func (t *aiWorkspaceTool) tasks(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	if len(arguments) > 8<<10 {
		return nil, errors.New("task query exceeds 8 KiB")
	}
	fields, err := decodeAIReadQueryObject(arguments, map[string]bool{"task_ids": true, "filters": true, "limit": true, "offset": true})
	if err != nil {
		return nil, err
	}
	if _, exact := fields["task_ids"]; exact {
		if len(fields) != 1 {
			return nil, errors.New("task_ids cannot be combined with filters or paging")
		}
		var input struct {
			TaskIDs []string `json:"task_ids"`
		}
		if err := decodeStrictToolArguments(arguments, &input); err != nil {
			return nil, err
		}
		return t.tasksExact(ctx, input.TaskIDs)
	}
	var filterFields map[string]json.RawMessage
	if raw, present := fields["filters"]; present {
		filterFields, err = decodeAIReadQueryObject(raw, map[string]bool{
			"q": true, "status": true, "priority": true, "kind": true, "project_id": true, "client_id": true,
			"tag_ids": true, "planned_date": true, "planned_from": true, "planned_to": true, "planned_state": true,
			"due_from": true, "due_to": true, "due_state": true, "parent_task_id": true, "root_only": true, "sort": true,
		})
		if err != nil {
			return nil, err
		}
	}
	var input struct {
		Filters aiTaskFilters `json:"filters"`
		Limit   *int          `json:"limit"`
		Offset  int           `json:"offset"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	filters, err := parseAITaskFilters(input.Filters, filterFields)
	if err != nil {
		return nil, err
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	// Capture one clock for overdue/due-soon filtering and its read timestamp;
	// each page is a new snapshot, never proof of an immutable bulk selection.
	now := t.api.options.Now().UTC()
	items := []aiTaskListItem{}
	var total int64
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := applyTaskFilters(tx.Model(&models.Task{}), filters, now)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyTaskSort(query, filters.Sort)
		if !valid {
			return errors.New("unsupported task sort")
		}
		return ordered.Select(aiTaskListProjection).
			Offset(input.Offset).Limit(limit + 1).Scan(&items).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	var next *int
	if more && input.Offset+limit <= 1000 {
		value := input.Offset + limit
		next = &value
	}
	for i := range items {
		items[i].Route = searchRoute("task", items[i].ID)
		items[i].UpdatedAt = normalizeTimestamp(items[i].UpdatedAt)
	}
	return map[string]any{
		"items": items, "total_items": total, "has_more": more, "next_offset": next,
		"window_limited": more && next == nil, "as_of": now.Format(time.RFC3339Nano),
	}, nil
}

// Exact selection is all-or-none and preserves the human's requested order.
// It re-reads current facts; neither the passed IDs nor the returned versions
// authorize a write or imply that a prior page is still current.
func (t *aiWorkspaceTool) tasksExact(ctx context.Context, taskIDs []string) (any, error) {
	if len(taskIDs) < 1 || len(taskIDs) > 20 {
		return nil, errors.New("task_ids requires 1-20 exact Tasks")
	}
	seen := make(map[string]bool, len(taskIDs))
	for _, value := range taskIDs {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value || seen[value] {
			return nil, errors.New("task_ids must contain distinct canonical Task UUIDs")
		}
		seen[value] = true
	}
	now := t.api.options.Now().UTC()
	items := []aiTaskListItem{}
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows := []aiTaskListItem{}
		if err := tx.Model(&models.Task{}).Select(aiTaskExactProjection).Where("tasks.id IN ?", taskIDs).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(taskIDs) {
			return newProjectRequestError(404, "TASK_NOT_FOUND", "One or more selected Tasks no longer exist")
		}
		byID := make(map[string]aiTaskListItem, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		for _, id := range taskIDs {
			item, exists := byID[id]
			if !exists {
				return newProjectRequestError(404, "TASK_NOT_FOUND", "One or more selected Tasks no longer exist")
			}
			item.Route = searchRoute("task", id)
			item.UpdatedAt = normalizeTimestamp(item.UpdatedAt)
			items = append(items, item)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return map[string]any{
		"items": items, "total_items": len(items), "has_more": false, "next_offset": nil,
		"window_limited": false, "as_of": now.Format(time.RFC3339Nano),
	}, nil
}
