package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const contentItemInboxSourceType = "content_item"

func contentItemInboxEventType(status string) (string, bool) {
	switch status {
	case "in_review":
		return "review_due", true
	case "scheduled":
		return "publish_due", true
	default:
		return "", false
	}
}

func contentItemInboxEventKey(id, eventType string, version int64) string {
	return fmt.Sprintf("content:%s:%s:%d", id, eventType, version)
}

func contentItemInboxTitle(title, eventType string) string {
	prefix := "内容待审核："
	if eventType == "publish_due" {
		prefix = "内容待发布："
	}
	value := []rune(prefix + title)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

// projectDueContentItems compensates missed scans on startup and projects due
// review/publish work exactly once for each current content version.
func (a *API) projectDueContentItems(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := formatInboxTimestamp(a.options.Now().UTC())
	var ids []string
	if err := a.db.WithContext(ctx).Model(&models.ContentItem{}).
		Where(`status IN ('in_review', 'scheduled')
			AND scheduled_at IS NOT NULL
			AND scheduled_at <= ?
			AND NOT EXISTS (
				SELECT 1 FROM inbox_items
				WHERE source_event_key = 'content:' || content_items.id || ':' ||
					CASE content_items.status WHEN 'in_review' THEN 'review_due' ELSE 'publish_due' END || ':' ||
					content_items.version
			)`, now).
		Order("scheduled_at ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list due Content Items: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.projectContentItemDue(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectContentItemDue(ctx context.Context, id, now string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.ContentItem
		if err := tx.First(&item, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		eventType, ok := contentItemInboxEventType(item.Status)
		if !ok || item.ScheduledAt == nil || item.ScheduledTimezone == nil || *item.ScheduledAt > now {
			return nil
		}
		key := contentItemInboxEventKey(item.ID, eventType, item.Version)
		var existing models.InboxItem
		err := tx.First(&existing, "source_event_key = ?", key).Error
		if err == nil {
			if existing.Kind != "event" || existing.SourceEntityType != contentItemInboxSourceType ||
				existing.SourceEntityID == nil || *existing.SourceEntityID != item.ID {
				return errors.New("Content Item source_event_key belongs to an incompatible Inbox Item")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"content_item_id": item.ID, "event_type": eventType,
			"content_version": item.Version, "scheduled_at": *item.ScheduledAt,
			"scheduled_timezone": *item.ScheduledTimezone,
		})
		if err != nil {
			return err
		}
		summary := fmt.Sprintf("平台：%s · 计划时间：%s", item.Platform, *item.ScheduledAt)
		priority := "P2"
		if eventType == "publish_due" {
			priority = "P1"
		}
		sourceID := item.ID
		dueAt := *item.ScheduledAt
		inbox := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: contentItemInboxTitle(item.Title, eventType),
			Summary: summary, SourceEntityType: contentItemInboxSourceType,
			SourceEntityID: &sourceID, SourceEventKey: &key, Priority: priority,
			Status: "open", ResolutionPolicy: "manual", DueAt: &dueAt,
			PayloadJSON: string(payload), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&inbox).Error; err != nil {
			return fmt.Errorf("create Content Item Inbox Item: %w", err)
		}
		return recordInboxWorkflowEventAs(
			tx, inbox.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(inbox, ""), "", now,
		)
	})
}

func resolveContentItemInboxSources(tx *gorm.DB, contentItemID, reason, requestID, now string) error {
	var items []models.InboxItem
	if err := tx.Where(
		"source_entity_type = ? AND source_entity_id = ? AND status IN ('open', 'tracking')",
		contentItemInboxSourceType, contentItemID,
	).Order("id ASC").Find(&items).Error; err != nil {
		return err
	}
	for _, current := range items {
		next := current
		ownerID := models.BuiltinOwnerActorID
		mode := "manual"
		next.Status = "resolved"
		next.SnoozedUntil = nil
		if next.TriagedAt == nil {
			next.TriagedAt = &now
		}
		next.ResolvedByActorID = &ownerID
		next.ResolvedAt = &now
		next.ResolutionReason = &reason
		next.ResolutionMode = &mode
		next.Version++
		next.UpdatedAt = now
		result := tx.Model(&models.InboxItem{}).
			Where("id = ? AND version = ? AND status IN ('open', 'tracking')", current.ID, current.Version).
			Updates(inboxCommandUpdates(next))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return inboxVersionConflict()
		}
		if err := recordInboxWorkflowEvent(
			tx, current.ID, "source_resolved", inboxItemEventState(current, reason),
			inboxItemEventState(next, reason), requestID, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func coordinateContentItemInboxSourceDeletion(tx *gorm.DB, contentItemID, requestID, now string) error {
	return coordinateInboxSourceDeletion(
		tx, contentItemInboxSourceType, []string{contentItemID},
		"CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES",
		"Resolve or dismiss all Content Item source Inbox Items before deleting this Content Item",
		requestID, now,
	)
}
