package api

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Stored schedules are UTC RFC3339Nano with optional fractional seconds.
// A fixed nine-digit comparison key preserves nanoseconds without changing
// stored values or losing precision through SQLite's datetime conversion.
const contentItemTimeKeyLayout = "2006-01-02T15:04:05.000000000"
const contentItemTimeKeyExpression = `(substr(content_items.scheduled_at,1,19) || '.' || substr(
 (CASE WHEN substr(content_items.scheduled_at,20,1)='.'
 THEN substr(content_items.scheduled_at,21,length(content_items.scheduled_at)-21)
 ELSE '' END) || '000000000',1,9))`

type contentItemQueryFilters struct {
	ScheduledFrom   *time.Time
	ScheduledTo     *time.Time
	Platform        string
	Status          string
	ProjectID       string
	ScheduleState   string
	TaskState       string
	IncludeArchived bool
}

// Preserve the native HTTP filter compatibility; AI's stricter JSON schema is
// checked before adapting its fields to these shared values.
func parseContentItemQueryFilters(values url.Values) (contentItemQueryFilters, error) {
	var filters contentItemQueryFilters
	parseBound := func(key string) (*time.Time, error) {
		raw := strings.TrimSpace(values.Get(key))
		if raw == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be RFC 3339", key)
		}
		utc := parsed.UTC()
		return &utc, nil
	}
	var err error
	filters.ScheduledFrom, err = parseBound("scheduled_from")
	if err != nil {
		return filters, err
	}
	filters.ScheduledTo, err = parseBound("scheduled_to")
	if err != nil {
		return filters, err
	}
	if filters.ScheduledFrom != nil && filters.ScheduledTo != nil && !filters.ScheduledFrom.Before(*filters.ScheduledTo) {
		return filters, errors.New("scheduled_from must be before scheduled_to")
	}
	if options, present := values["schedule_state"]; present {
		if len(options) != 1 {
			return filters, errors.New("schedule_state filter must be provided exactly once")
		}
		filters.ScheduleState = options[0]
		if _, ok := contentItemScheduleStates[filters.ScheduleState]; !ok {
			return filters, errors.New("schedule_state filter is invalid")
		}
	}
	if filters.ScheduleState == "unscheduled" && (values.Has("scheduled_from") || values.Has("scheduled_to")) {
		return filters, errors.New("unscheduled schedule_state cannot be combined with scheduled_from or scheduled_to")
	}
	filters.Platform = strings.TrimSpace(values.Get("platform"))
	if utf8.RuneCountInString(filters.Platform) > 64 {
		return filters, errors.New("platform filter is too long")
	}
	filters.Status = strings.TrimSpace(values.Get("status"))
	if filters.Status != "" {
		if _, ok := contentItemStatuses[filters.Status]; !ok {
			return filters, errors.New("status filter is invalid")
		}
	}
	if raw := strings.TrimSpace(values.Get("project_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return filters, errors.New("project_id filter must be a UUID")
		}
		filters.ProjectID = parsed.String()
	}
	if raw := strings.TrimSpace(values.Get("include_archived")); raw != "" {
		filters.IncludeArchived, err = strconv.ParseBool(raw)
		if err != nil {
			return filters, errors.New("include_archived must be true or false")
		}
	}
	filters.TaskState = "all"
	if options, present := values["task_state"]; present {
		if len(options) != 1 {
			return filters, errors.New("task_state filter must be provided exactly once")
		}
		filters.TaskState = options[0]
		switch filters.TaskState {
		case "all", "any_incomplete", "required_incomplete":
		default:
			return filters, errors.New("task_state filter is invalid")
		}
	}
	return filters, nil
}

func contentItemFilteredQuery(tx *gorm.DB, filters contentItemQueryFilters) *gorm.DB {
	query := tx.Model(&models.ContentItem{})
	if filters.ScheduleState == "scheduled" {
		query = query.Where("content_items.scheduled_at IS NOT NULL")
	} else if filters.ScheduleState == "unscheduled" {
		query = query.Where("content_items.scheduled_at IS NULL")
	}
	if filters.ScheduledFrom != nil {
		query = query.Where(contentItemTimeKeyExpression+" >= ?", filters.ScheduledFrom.UTC().Format(contentItemTimeKeyLayout))
	}
	if filters.ScheduledTo != nil {
		query = query.Where(contentItemTimeKeyExpression+" < ?", filters.ScheduledTo.UTC().Format(contentItemTimeKeyLayout))
	}
	if filters.Platform != "" {
		query = query.Where("content_items.platform=?", filters.Platform)
	}
	if filters.Status != "" {
		query = query.Where("content_items.status=?", filters.Status)
	} else if !filters.IncludeArchived {
		query = query.Where("content_items.status <> 'archived'")
	}
	if filters.ProjectID != "" {
		query = query.Where("content_items.project_id=?", filters.ProjectID)
	}
	if filters.TaskState == "any_incomplete" || filters.TaskState == "required_incomplete" {
		pending := tx.Table("content_item_tasks AS relation").Select("1").Joins("JOIN tasks AS preparation ON preparation.id=relation.task_id").Where("relation.content_item_id=content_items.id AND preparation.status <> 'done'")
		if filters.TaskState == "required_incomplete" {
			pending = pending.Where("relation.is_required=1")
		}
		query = query.Where("EXISTS (?)", pending)
	}
	return query
}

func contentItemOrderedQuery(query *gorm.DB) *gorm.DB {
	return query.Order("CASE WHEN content_items.scheduled_at IS NULL THEN 1 ELSE 0 END ASC").Order(contentItemTimeKeyExpression + " ASC").Order("content_items.manual_order ASC").Order("content_items.id ASC")
}

type contentItemTaskProgress struct {
	TaskTotal         int64
	TaskDone          int64
	RequiredTaskTotal int64
	RequiredTaskDone  int64
}

// Counts are current Task facts, not cached Content Item state. Caller owns
// the transaction so filters, list membership and returned counts agree.
func loadContentItemTaskProgressByIDs(tx *gorm.DB, ids []string) (map[string]contentItemTaskProgress, error) {
	result := make(map[string]contentItemTaskProgress, len(ids))
	for _, id := range ids {
		result[id] = contentItemTaskProgress{}
	}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		ContentItemID string
		Progress      contentItemTaskProgress `gorm:"embedded"`
	}
	if err := tx.Table("content_item_tasks AS relation").Select(`relation.content_item_id,
 COUNT(*) AS task_total,
 COALESCE(SUM(CASE WHEN task.status='done' THEN 1 ELSE 0 END),0) AS task_done,
 COALESCE(SUM(CASE WHEN relation.is_required=1 THEN 1 ELSE 0 END),0) AS required_task_total,
 COALESCE(SUM(CASE WHEN relation.is_required=1 AND task.status='done' THEN 1 ELSE 0 END),0) AS required_task_done`).
		Joins("JOIN tasks AS task ON task.id=relation.task_id").Where("relation.content_item_id IN ?", ids).Group("relation.content_item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ContentItemID] = row.Progress
	}
	return result, nil
}
