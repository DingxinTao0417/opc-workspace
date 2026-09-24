package api

import (
	"errors"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// contentItemDeletionImpact is a metadata-only snapshot shared by the native
// Content Item delete endpoint and AI approval previews. It never exposes
// content notes, external links, Task titles or Inbox payloads.
type contentItemDeletionImpact struct {
	TaskLinks          int64
	InboxSourcesMarked int64
}

func loadContentItemDeletionImpact(tx *gorm.DB, id string, expectedVersion int64) (models.ContentItem, contentItemDeletionImpact, error) {
	var item models.ContentItem
	if err := tx.First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return item, contentItemDeletionImpact{}, newProjectRequestError(http.StatusNotFound, "CONTENT_ITEM_NOT_FOUND", "Content item not found")
		}
		return item, contentItemDeletionImpact{}, err
	}
	if item.Version != expectedVersion {
		return item, contentItemDeletionImpact{}, contentItemVersionConflict()
	}
	if item.Status != "archived" {
		return item, contentItemDeletionImpact{}, newProjectRequestError(http.StatusConflict, "CONTENT_ITEM_NOT_ARCHIVED", "Only archived content items can be permanently deleted")
	}

	var activeSources int64
	if err := tx.Table("inbox_items").Where(
		"source_entity_type = ? AND source_entity_id = ? AND source_deleted_at IS NULL AND status IN ('open', 'tracking')",
		contentItemInboxSourceType, id,
	).Count(&activeSources).Error; err != nil {
		return item, contentItemDeletionImpact{}, err
	}
	if activeSources > 0 {
		return item, contentItemDeletionImpact{}, newProjectRequestError(
			http.StatusConflict,
			"CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES",
			"Resolve or dismiss all Content Item source Inbox Items before deleting this Content Item",
		)
	}

	impact := contentItemDeletionImpact{}
	counts := []struct {
		query *gorm.DB
		value *int64
	}{
		{tx.Table("content_item_tasks").Where("content_item_id = ?", id), &impact.TaskLinks},
		{tx.Table("inbox_items").Where("source_entity_type = ? AND source_entity_id = ? AND source_deleted_at IS NULL", contentItemInboxSourceType, id), &impact.InboxSourcesMarked},
	}
	for _, count := range counts {
		if err := count.query.Count(count.value).Error; err != nil {
			return item, contentItemDeletionImpact{}, err
		}
	}
	return item, impact, nil
}

func deleteContentItemInTransaction(tx *gorm.DB, id string, expectedVersion int64, requestID, deletedAt string) (models.ContentItem, contentItemDeletionImpact, error) {
	item, impact, err := loadContentItemDeletionImpact(tx, id, expectedVersion)
	if err != nil {
		return item, impact, err
	}
	if err := coordinateContentItemInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return item, impact, err
	}
	result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.ContentItem{})
	if result.Error != nil {
		return item, impact, result.Error
	}
	if result.RowsAffected == 0 {
		return item, impact, contentItemVersionConflict()
	}
	return item, impact, nil
}
