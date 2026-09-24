package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"gorm.io/gorm"
)

type aiProjectListItem struct {
	ID          string             `json:"id" gorm:"column:id"`
	Label       string             `json:"label" gorm:"column:label"`
	Status      string             `json:"status" gorm:"column:status"`
	StartDate   *string            `json:"start_date" gorm:"column:start_date"`
	DueDate     *string            `json:"due_date" gorm:"column:due_date"`
	Version     int64              `json:"version" gorm:"column:version"`
	UpdatedAt   string             `json:"updated_at" gorm:"column:updated_at"`
	TaskSummary projectTaskSummary `json:"task_summary" gorm:"-"`
	Route       string             `json:"route" gorm:"-"`
	TaskTotal   int64              `json:"-" gorm:"column:task_total"`
	TaskDone    int64              `json:"-" gorm:"column:task_completed"`
	TaskActive  int64              `json:"-" gorm:"column:task_in_progress"`
	TaskMinutes int64              `json:"-" gorm:"column:task_actual_minutes"`
}

const aiProjectSelectColumns = `projects.id, substr(projects.name,1,200) AS label,
	projects.status, projects.start_date, projects.due_date, projects.version, projects.updated_at,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id) AS task_total,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id AND tasks.status = 'done') AS task_completed,
	(SELECT COUNT(*) FROM tasks WHERE tasks.project_id = projects.id AND tasks.status = 'in_progress') AS task_in_progress,
	(SELECT COALESCE(SUM(tasks.actual_minutes),0) FROM tasks WHERE tasks.project_id = projects.id) AS task_actual_minutes`

func aiProjectsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"filters":{"type":"object","additionalProperties":false,"properties":{"q":{"type":"string","maxLength":200},"status":{"type":"string","enum":["planning","in_progress","paused","completed","archived"]},"client_id":{"type":"string"},"include_archived":{"type":"boolean"},"sort":{"type":"string"}}},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

func validAIProjectSort(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 120 {
		return false
	}
	allowed := map[string]bool{"name": true, "status": true, "start_date": true, "due_date": true, "created_at": true, "updated_at": true}
	parts := strings.Split(raw, ",")
	if len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		field := strings.TrimSpace(part)
		field = strings.TrimPrefix(field, "-")
		if !allowed[field] {
			return false
		}
	}
	return true
}

func (t *aiWorkspaceTool) projects(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	if len(arguments) > 8<<10 {
		return nil, errors.New("project query exceeds 8 KiB")
	}
	fields, err := decodeAIReadQueryObject(arguments, map[string]bool{"filters": true, "limit": true, "offset": true})
	if err != nil {
		return nil, err
	}
	var filterFields map[string]json.RawMessage
	if raw, present := fields["filters"]; present {
		filterFields, err = decodeAIReadQueryObject(raw, map[string]bool{"q": true, "status": true, "client_id": true, "include_archived": true, "sort": true})
		if err != nil {
			return nil, err
		}
	}
	var input struct {
		Filters struct {
			Query           string `json:"q"`
			Status          string `json:"status"`
			ClientID        string `json:"client_id"`
			IncludeArchived bool   `json:"include_archived"`
			Sort            string `json:"sort"`
		} `json:"filters"`
		Limit  *int `json:"limit"`
		Offset int  `json:"offset"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	search := strings.TrimSpace(input.Filters.Query)
	if utf8.RuneCountInString(search) > 200 {
		return nil, errors.New("q cannot exceed 200 characters")
	}
	if _, present := filterFields["status"]; present {
		if _, valid := validProjectStatuses[input.Filters.Status]; !valid {
			return nil, errors.New("invalid project status")
		}
	}
	clientID := ""
	if _, present := filterFields["client_id"]; present {
		if !t.policy.Allows("clients") {
			return nil, harness.ErrPermissionDenied
		}
		parsed, parseErr := uuid.Parse(input.Filters.ClientID)
		if parseErr != nil || parsed.String() != input.Filters.ClientID {
			return nil, errors.New("client_id must be a canonical UUID")
		}
		clientID = parsed.String()
	}
	if !validAIProjectSort(input.Filters.Sort) {
		return nil, errors.New("unsupported project sort")
	}
	now := t.api.options.Now().UTC()
	items := []aiProjectListItem{}
	var total int64
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := filteredProjectsQuery(tx, input.Filters.Status, clientID, search, input.Filters.IncludeArchived)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		ordered, valid := applyProjectSort(query, input.Filters.Sort)
		if !valid {
			return errors.New("unsupported project sort")
		}
		return ordered.Select(aiProjectSelectColumns).Offset(input.Offset).Limit(limit + 1).Scan(&items).Error
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
		item := &items[i]
		progress := 0
		if item.TaskTotal > 0 {
			progress = int((item.TaskDone*100 + item.TaskTotal/2) / item.TaskTotal)
		}
		item.TaskSummary = projectTaskSummary{Total: item.TaskTotal, Completed: item.TaskDone,
			InProgress: item.TaskActive, Remaining: item.TaskTotal - item.TaskDone,
			ProgressPercent: progress, ActualMinutes: item.TaskMinutes}
		item.Route = searchRoute("project", item.ID)
		item.UpdatedAt = normalizeTimestamp(item.UpdatedAt)
	}
	return map[string]any{"items": items, "total_items": total, "has_more": more, "next_offset": next,
		"window_limited": more && next == nil, "as_of": now.Format(time.RFC3339Nano)}, nil
}
