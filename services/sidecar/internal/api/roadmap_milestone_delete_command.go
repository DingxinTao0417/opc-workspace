package api

import (
	"errors"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// roadmapMilestoneDeletionImpact is a metadata-only snapshot shared by the
// native delete endpoint and AI approval previews. It never exposes milestone
// descriptions, Project names or Inbox payloads.
type roadmapMilestoneDeletionImpact struct {
	ProjectLinks       int64
	InboxSourcesMarked int64
}

func loadRoadmapMilestoneDeletionImpact(tx *gorm.DB, id string, expectedVersion int64) (models.RoadmapMilestone, roadmapMilestoneDeletionImpact, error) {
	var milestone models.RoadmapMilestone
	if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return milestone, roadmapMilestoneDeletionImpact{}, newProjectRequestError(http.StatusNotFound, "ROADMAP_MILESTONE_NOT_FOUND", "Roadmap milestone not found")
		}
		return milestone, roadmapMilestoneDeletionImpact{}, err
	}
	if milestone.Version != expectedVersion {
		return milestone, roadmapMilestoneDeletionImpact{}, roadmapMilestoneVersionConflict()
	}
	if milestone.Status != "archived" {
		return milestone, roadmapMilestoneDeletionImpact{}, newProjectRequestError(http.StatusConflict, "ROADMAP_MILESTONE_NOT_ARCHIVED", "Only archived roadmap milestones can be permanently deleted")
	}

	var activeSources int64
	if err := tx.Table("inbox_items").Where(
		"source_entity_type = ? AND source_entity_id = ? AND source_deleted_at IS NULL AND status IN ('open', 'tracking')",
		roadmapMilestoneInboxSourceType, id,
	).Count(&activeSources).Error; err != nil {
		return milestone, roadmapMilestoneDeletionImpact{}, err
	}
	if activeSources > 0 {
		return milestone, roadmapMilestoneDeletionImpact{}, newProjectRequestError(
			http.StatusConflict,
			"ROADMAP_MILESTONE_HAS_ACTIVE_INBOX_SOURCES",
			"Resolve or dismiss all Roadmap Milestone source Inbox Items before deleting this milestone",
		)
	}

	impact := roadmapMilestoneDeletionImpact{}
	counts := []struct {
		query *gorm.DB
		value *int64
	}{
		{tx.Table("roadmap_milestone_projects").Where("milestone_id = ?", id), &impact.ProjectLinks},
		{tx.Table("inbox_items").Where("source_entity_type = ? AND source_entity_id = ? AND source_deleted_at IS NULL", roadmapMilestoneInboxSourceType, id), &impact.InboxSourcesMarked},
	}
	for _, count := range counts {
		if err := count.query.Count(count.value).Error; err != nil {
			return milestone, roadmapMilestoneDeletionImpact{}, err
		}
	}
	return milestone, impact, nil
}

func deleteRoadmapMilestoneInTransaction(tx *gorm.DB, id string, expectedVersion int64, requestID, deletedAt string) (models.RoadmapMilestone, roadmapMilestoneDeletionImpact, error) {
	milestone, impact, err := loadRoadmapMilestoneDeletionImpact(tx, id, expectedVersion)
	if err != nil {
		return milestone, impact, err
	}
	if err := coordinateRoadmapMilestoneInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return milestone, impact, err
	}
	result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.RoadmapMilestone{})
	if result.Error != nil {
		return milestone, impact, result.Error
	}
	if result.RowsAffected == 0 {
		return milestone, impact, roadmapMilestoneVersionConflict()
	}
	return milestone, impact, nil
}
