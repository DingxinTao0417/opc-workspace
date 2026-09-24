package api

import (
	"context"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// No source payload, source IDs or workflow-event bodies cross the tool boundary.
// A transaction keeps the item/version and its derived Task progress consistent.
func loadAIInboxContext(ctx context.Context, db *gorm.DB, id string, now time.Time) (source aiBusinessContextSource, err error) {
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item models.InboxItem
		if err := tx.Select("id", "title", "summary", "status", "version", "priority", "due_at", "read_at", "snoozed_until", "resolution_policy", "source_entity_type").First(&item, "id=?", id).Error; err != nil {
			return err
		}
		progress, err := loadInboxTaskProgress(tx, id)
		if err != nil {
			return err
		}
		body, truncated := boundedAIBusinessContextText(item.Summary)
		source = aiBusinessContextSource{Type: "inbox_item", ID: item.ID, Version: item.Version, Label: item.Title,
			Fields: map[string]any{"status": item.Status, "summary": body, "priority": item.Priority, "due_at": item.DueAt,
				"read_at": item.ReadAt, "snoozed_until": item.SnoozedUntil, "resolution_policy": item.ResolutionPolicy,
				"source_entity_type": item.SourceEntityType, "task_progress": progress, "server_now": formatInboxTimestamp(now)}, TruncatedFields: []string{}}
		if truncated {
			source.TruncatedFields = append(source.TruncatedFields, "summary")
		}
		return nil
	})
	return source, err
}
