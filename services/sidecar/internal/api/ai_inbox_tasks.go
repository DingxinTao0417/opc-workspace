package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiInboxTaskRow struct {
	RelationID    string  `json:"relation_id"`
	TaskRefID     string  `json:"task_ref_id"`
	TaskID        *string `json:"task_id"`
	TitleSnapshot string  `json:"title_snapshot"`
	TaskTitle     *string `json:"task_title"`
	TaskStatus    *string `json:"task_status"`
	TaskVersion   *int64  `json:"task_version"`
	IsRequired    bool    `json:"is_required"`
	RelationType  string  `json:"relation_type"`
	LinkedAt      string  `json:"linked_at"`
	UnlinkedAt    *string `json:"unlinked_at"`
	Route         string  `json:"route" gorm:"-"`
}

func aiInboxTasksSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["inbox_item_id"],"properties":{"inbox_item_id":{"type":"string","format":"uuid"},"state":{"type":"string","enum":["active","history"],"description":"Default active. History is soft-unlinked relations, NOT current work."},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

// Read only the allowed relation metadata, not actor identities, reasons, source
// payload or Task description. Deleted history keeps its snapshot without a link.
func (t *aiWorkspaceTool) inboxTasks(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		InboxItemID string `json:"inbox_item_id"`
		State       string `json:"state"`
		Limit       *int   `json:"limit"`
		Offset      int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	if id, err := uuid.Parse(input.InboxItemID); err != nil || id.String() != input.InboxItemID {
		return nil, errors.New("inbox_item_id must be a canonical UUID")
	}
	if input.State == "" {
		input.State = "active"
	}
	if input.State != "active" && input.State != "history" {
		return nil, errors.New("state must be active or history")
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.InboxItem
		if err := tx.Select("id", "version", "status", "resolution_policy").First(&item, "id=?", input.InboxItemID).Error; err != nil {
			return err
		}
		rows := []aiInboxTaskRow{}
		query := tx.Table("inbox_item_tasks AS r").Select(`
   r.id AS relation_id, r.task_ref_id, r.task_id,
   substr(r.task_title_snapshot,1,200) AS title_snapshot,
   substr(t.title,1,200) AS task_title, t.status AS task_status, t.version AS task_version,
   r.is_required, r.relation_type, r.linked_at, r.unlinked_at
  `).Joins("LEFT JOIN tasks AS t ON t.id=r.task_id").Where("r.inbox_item_id=?", item.ID)
		if input.State == "active" {
			query = query.Where("r.unlinked_at IS NULL").Order("r.position, r.linked_at, r.id")
		} else {
			query = query.Where("r.unlinked_at IS NOT NULL").Order("r.unlinked_at DESC, r.linked_at DESC, r.id DESC")
		}
		if err := query.Limit(limit + 1).Offset(input.Offset).Scan(&rows).Error; err != nil {
			return err
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		for i := range rows {
			if rows[i].TaskID != nil {
				rows[i].Route = searchRoute("task", *rows[i].TaskID)
			}
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		progress, err := loadInboxTaskProgress(tx, item.ID)
		if err != nil {
			return err
		}
		result = map[string]any{
			"inbox_item_id": item.ID, "inbox_item_version": item.Version, "status": item.Status,
			"resolution_policy": item.ResolutionPolicy, "route": searchRoute("inbox_item", item.ID),
			"state": input.State, "items": rows, "progress": progress,
			"next_offset": next, "has_more": more, "window_limited": more && next == nil,
			"server_now": formatInboxTimestamp(t.api.options.Now()),
		}
		return nil
	})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
