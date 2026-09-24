package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func aiContentItemsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view"],"properties":{"view":{"type":"string","enum":["list","tasks"]},"filters":{"type":"object","description":"Only for list; timestamps are explicit RFC3339 instants, [from,to). Do not guess a user's timezone.","additionalProperties":false,"properties":{"platform":{"type":"string","minLength":1,"maxLength":64},"status":{"type":"string","enum":["draft","in_review","scheduled","published","cancelled","archived"]},"project_id":{"type":"string","format":"uuid"},"scheduled_from":{"type":"string","format":"date-time"},"scheduled_to":{"type":"string","format":"date-time"},"schedule_state":{"type":"string","enum":["scheduled","unscheduled"],"description":"Presence of a scheduled instant, not content status; unscheduled excludes date bounds."},"include_archived":{"type":"boolean"},"task_state":{"type":"string","enum":["all","any_incomplete","required_incomplete"],"description":"Only done counts as complete. No linked tasks never matches incomplete."}}},"content_item_id":{"type":"string","format":"uuid","description":"Required only for tasks; use the real content ID."},"task_state":{"type":"string","enum":["all","incomplete"],"description":"Only for tasks; filters task status, not content status. Default all."},"required_only":{"type":"boolean","description":"Only for tasks; default false."},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

// Presence is significant: reject aliases, duplicates, nulls and parameters
// belonging to another view before converting to the shared native filters.
func decodeAIContentQueryObject(raw json.RawMessage, allowed map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("content query and filters must be objects")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errors.New("content query requires exact unique schema fields")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, errors.New("content query fields must be valid and non-null")
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("invalid content query")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("content query has trailing data")
	}
	return fields, nil
}

type aiContentItemsQuery struct {
	View          string          `json:"view"`
	Filters       json.RawMessage `json:"filters"`
	ContentItemID string          `json:"content_item_id"`
	TaskState     string          `json:"task_state"`
	RequiredOnly  bool            `json:"required_only"`
	Limit         *int            `json:"limit"`
	Offset        int             `json:"offset"`
}

func parseAIContentItemsQuery(raw json.RawMessage) (aiContentItemsQuery, contentItemQueryFilters, error) {
	var input aiContentItemsQuery
	var filters contentItemQueryFilters
	fields, err := decodeAIContentQueryObject(raw, map[string]bool{"view": true, "filters": true, "content_item_id": true, "task_state": true, "required_only": true, "limit": true, "offset": true})
	if err != nil {
		return input, filters, err
	}
	if err := decodeStrictToolArguments(raw, &input); err != nil {
		return input, filters, err
	}
	switch input.View {
	case "list":
		if fields["content_item_id"] != nil || fields["task_state"] != nil || fields["required_only"] != nil {
			return input, filters, errors.New("list only accepts filters, limit and offset")
		}
		values := url.Values{}
		if fields["filters"] != nil {
			filterFields, err := decodeAIContentQueryObject(input.Filters, map[string]bool{"platform": true, "status": true, "project_id": true, "scheduled_from": true, "scheduled_to": true, "schedule_state": true, "include_archived": true, "task_state": true})
			if err != nil {
				return input, filters, err
			}
			for key, rawValue := range filterFields {
				if key == "include_archived" {
					var value bool
					if err := json.Unmarshal(rawValue, &value); err != nil {
						return input, filters, errors.New("include_archived must be boolean")
					}
					values.Set(key, strconv.FormatBool(value))
					continue
				}
				var value string
				if err := json.Unmarshal(rawValue, &value); err != nil || value == "" || strings.TrimSpace(value) != value {
					return input, filters, errors.New("content filter strings must be non-empty without surrounding whitespace")
				}
				if key == "project_id" {
					if id, err := uuid.Parse(value); err != nil || id.String() != value {
						return input, filters, errors.New("project_id must be a canonical UUID")
					}
				}
				values.Set(key, value)
			}
		}
		filters, err = parseContentItemQueryFilters(values)
		return input, filters, err
	case "tasks":
		if fields["filters"] != nil {
			return input, filters, errors.New("tasks does not accept list filters")
		}
		if id, err := uuid.Parse(input.ContentItemID); err != nil || id.String() != input.ContentItemID {
			return input, filters, errors.New("content_item_id must be a canonical UUID")
		}
		if fields["task_state"] == nil {
			input.TaskState = "all"
		}
		if input.TaskState != "all" && input.TaskState != "incomplete" {
			return input, filters, errors.New("tasks task_state must be all or incomplete")
		}
		return input, filters, nil
	default:
		return input, filters, errors.New("view must be list or tasks")
	}
}

type aiContentListItem struct {
	ID                string  `json:"id"`
	Label             string  `json:"label"`
	Status            string  `json:"status"`
	Version           int64   `json:"version"`
	Platform          string  `json:"platform"`
	ScheduledAt       *string `json:"scheduled_at"`
	ScheduledTimezone *string `json:"scheduled_timezone"`
	ProjectID         *string `json:"project_id"`
	TaskTotal         int64   `json:"task_total" gorm:"-"`
	TaskDone          int64   `json:"task_done" gorm:"-"`
	RequiredTaskTotal int64   `json:"required_task_total" gorm:"-"`
	RequiredTaskDone  int64   `json:"required_task_done" gorm:"-"`
	Route             string  `json:"route" gorm:"-"`
}

const aiContentListColumns = "content_items.id,substr(content_items.title,1,200) AS label,content_items.status,content_items.version,content_items.platform,content_items.scheduled_at,content_items.scheduled_timezone,content_items.project_id"

type aiContentTaskItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Version    int64  `json:"version"`
	IsRequired bool   `json:"is_required"`
	Route      string `json:"route" gorm:"-"`
}

func loadAIContentProgress(tx *gorm.DB, items []aiContentListItem) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	progress, err := loadContentItemTaskProgressByIDs(tx, ids)
	if err != nil {
		return err
	}
	for index := range items {
		item := &items[index]
		value := progress[item.ID]
		item.TaskTotal, item.TaskDone = value.TaskTotal, value.TaskDone
		item.RequiredTaskTotal, item.RequiredTaskDone = value.RequiredTaskTotal, value.RequiredTaskDone
		item.Route = searchRoute("content_item", item.ID)
	}
	return nil
}

func (t *aiWorkspaceTool) contentItems(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	input, filters, err := parseAIContentItemsQuery(args)
	if err != nil {
		return nil, err
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"view": input.View}
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result["as_of"] = formatInboxTimestamp(t.api.options.Now())
		var total int64
		var more bool
		if input.View == "list" {
			query := contentItemFilteredQuery(tx, filters)
			if err := query.Count(&total).Error; err != nil {
				return err
			}
			items := []aiContentListItem{}
			if err := contentItemOrderedQuery(query).Select(aiContentListColumns).Offset(input.Offset).Limit(limit + 1).Scan(&items).Error; err != nil {
				return err
			}
			more = len(items) > limit
			if more {
				items = items[:limit]
			}
			if err := loadAIContentProgress(tx, items); err != nil {
				return err
			}
			result["items"] = items
		} else {
			content := []aiContentListItem{{}}
			if err := tx.Table("content_items").Select(aiContentListColumns).Where("id=?", input.ContentItemID).Take(&content[0]).Error; err != nil {
				return err
			}
			if err := loadAIContentProgress(tx, content); err != nil {
				return err
			}
			result["content_item"] = content[0]
			query := tx.Table("content_item_tasks AS ct").Joins("JOIN tasks AS t ON t.id=ct.task_id").Where("ct.content_item_id=?", input.ContentItemID)
			if input.RequiredOnly {
				query = query.Where("ct.is_required=1")
			}
			if input.TaskState == "incomplete" {
				query = query.Where("t.status <> 'done'")
			}
			if err := query.Count(&total).Error; err != nil {
				return err
			}
			items := []aiContentTaskItem{}
			if err := query.Select("t.id,substr(t.title,1,200) AS title,t.status,t.version,ct.is_required").Order("ct.linked_at ASC,t.id ASC").Offset(input.Offset).Limit(limit + 1).Scan(&items).Error; err != nil {
				return err
			}
			more = len(items) > limit
			if more {
				items = items[:limit]
			}
			for index := range items {
				items[index].Route = searchRoute("task", items[index].ID)
			}
			result["items"] = items
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		result["total_items"], result["has_more"], result["next_offset"], result["window_limited"] = total, more, next, more && next == nil
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
