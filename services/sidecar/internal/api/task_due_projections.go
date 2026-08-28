package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const taskDueInboxSourceType = "task_due"

const taskDueLeadTime = 24 * time.Hour

func taskDueEventKey(taskID, dueAt string) string {
	return fmt.Sprintf("task:%s:due:%s", taskID, dueAt)
}

func taskDueTitle(title, dueState string) string {
	prefix := "任务临期："
	if dueState == "overdue" {
		prefix = "任务逾期："
	}
	value := []rune(prefix + title)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

func (a *API) projectDueTasks(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := a.options.Now().UTC()
	cutoffText := formatInboxTimestamp(now.Add(taskDueLeadTime))
	var ids []string
	if err := a.db.WithContext(ctx).Model(&models.Task{}).
		Where(`status NOT IN ('done', 'cancelled')
			AND due_date IS NOT NULL
			AND julianday(due_date) <= julianday(?)
			AND NOT EXISTS (
				SELECT 1 FROM inbox_items
				WHERE source_entity_type = 'task_due'
				  AND source_entity_id = tasks.id
				  AND julianday(json_extract(payload_json, '$.due_at')) = julianday(tasks.due_date)
			)`, cutoffText).
		Order("julianday(due_date) ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list due Tasks: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.projectTaskDue(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectTaskDue(ctx context.Context, id string, now time.Time) error {
	now = now.UTC()
	nowText := formatInboxTimestamp(now)
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := loadTask(tx, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.Status == "done" || task.Status == "cancelled" || task.DueDate == nil {
			return nil
		}
		dueAt, err := time.Parse(time.RFC3339Nano, *task.DueDate)
		if err != nil {
			return fmt.Errorf("parse Task due date: %w", err)
		}
		if dueAt.After(now.Add(taskDueLeadTime)) {
			return nil
		}

		key := taskDueEventKey(task.ID, *task.DueDate)
		var existing models.InboxItem
		err = tx.First(&existing, "source_event_key = ?", key).Error
		if err == nil {
			if existing.Kind != "event" || existing.SourceEntityType != taskDueInboxSourceType ||
				existing.SourceEntityID == nil || *existing.SourceEntityID != task.ID {
				return errors.New("Task due source_event_key belongs to an incompatible Inbox Item")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		dueState := "due_soon"
		if dueAt.Before(now) {
			dueState = "overdue"
		}
		payload := map[string]any{
			"task_id": task.ID, "task_title": task.Title, "due_at": *task.DueDate,
			"projected_at": nowText, "due_state": dueState, "lead_minutes": 1440,
		}
		if task.ProjectID != nil {
			payload["project_id"] = *task.ProjectID
		}
		if task.ProjectName != nil {
			payload["project_name"] = *task.ProjectName
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		sourceID := task.ID
		dueAtText := *task.DueDate
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: taskDueTitle(task.Title, dueState),
			Summary: "任务截止时间：" + dueAtText, SourceEntityType: taskDueInboxSourceType,
			SourceEntityID: &sourceID, SourceEventKey: &key, Priority: task.Priority,
			Status: "open", ResolutionPolicy: "manual", DueAt: &dueAtText,
			PayloadJSON: string(payloadJSON), Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("create Task due Inbox Item: %w", err)
		}
		return recordInboxWorkflowEventAs(
			tx, item.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(item, ""), "", nowText,
		)
	})
}
