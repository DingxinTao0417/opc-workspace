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

const roadmapMilestoneInboxSourceType = "roadmap_milestone"

func roadmapMilestoneInboxEventKey(id, eventType string, version int64) string {
	return fmt.Sprintf("roadmap:%s:%s:%d", id, eventType, version)
}

func roadmapMilestoneInboxTitle(title, eventType string) string {
	prefix := "里程碑到期："
	if eventType == "achieved" {
		prefix = "里程碑已达成："
	}
	value := []rune(prefix + title)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

// projectDueRoadmapMilestones compensates missed scans using the calendar date
// in the location returned by Options.Now. A pure YYYY-MM-DD target is never
// reinterpreted as a UTC instant.
func (a *API) projectDueRoadmapMilestones(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := a.options.Now()
	today := now.Format(roadmapMilestoneDateLayout)
	var ids []string
	if err := a.db.WithContext(ctx).Model(&models.RoadmapMilestone{}).
		Where(`status IN ('planned', 'active')
			AND target_date <= ?
			AND NOT EXISTS (
				SELECT 1 FROM inbox_items
				WHERE source_entity_type = 'roadmap_milestone'
				  AND source_entity_id = roadmap_milestones.id
				  AND json_extract(payload_json, '$.event_type') = 'due'
				  AND json_extract(payload_json, '$.target_date') = roadmap_milestones.target_date
			)`, today).
		Order("target_date ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list due Roadmap Milestones: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var milestone models.RoadmapMilestone
			if err := tx.First(&milestone, "id = ?", id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			return a.projectRoadmapMilestoneInboxEvent(tx, milestone, "due", "", now)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectRoadmapMilestoneInboxEvent(tx *gorm.DB, milestone models.RoadmapMilestone, eventType, requestID string, now time.Time) error {
	if eventType == "due" {
		if (milestone.Status != "planned" && milestone.Status != "active") || milestone.TargetDate > now.Format(roadmapMilestoneDateLayout) {
			return nil
		}
		var count int64
		if err := tx.Model(&models.InboxItem{}).
			Where("source_entity_type = ? AND source_entity_id = ? AND json_extract(payload_json, '$.event_type') = 'due' AND json_extract(payload_json, '$.target_date') = ?", roadmapMilestoneInboxSourceType, milestone.ID, milestone.TargetDate).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
	} else if eventType == "achieved" {
		if milestone.Status != "achieved" {
			return nil
		}
	} else {
		return fmt.Errorf("unsupported Roadmap Milestone Inbox event type %q", eventType)
	}

	key := roadmapMilestoneInboxEventKey(milestone.ID, eventType, milestone.Version)
	var existing models.InboxItem
	err := tx.First(&existing, "source_event_key = ?", key).Error
	if err == nil {
		if existing.Kind != "event" || existing.SourceEntityType != roadmapMilestoneInboxSourceType || existing.SourceEntityID == nil || *existing.SourceEntityID != milestone.ID {
			return errors.New("Roadmap Milestone source_event_key belongs to an incompatible Inbox Item")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	payload, err := json.Marshal(map[string]any{
		"roadmap_milestone_id": milestone.ID,
		"event_type":           eventType,
		"milestone_version":    milestone.Version,
		"target_date":          milestone.TargetDate,
		"year":                 milestone.Year,
		"quarter":              milestone.Quarter,
	})
	if err != nil {
		return err
	}
	createdAt := formatInboxTimestamp(now.UTC())
	dueAt := roadmapMilestoneDueTimestamp(milestone.TargetDate, now.Location())
	priority := "P1"
	summary := fmt.Sprintf("目标日期：%s · %d Q%d", milestone.TargetDate, milestone.Year, milestone.Quarter)
	if eventType == "achieved" {
		priority = "P2"
		dueAt = createdAt
	}
	sourceID := milestone.ID
	inbox := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: roadmapMilestoneInboxTitle(milestone.Title, eventType),
		Summary: summary, SourceEntityType: roadmapMilestoneInboxSourceType,
		SourceEntityID: &sourceID, SourceEventKey: &key, Priority: priority,
		Status: "open", ResolutionPolicy: "manual", DueAt: &dueAt,
		PayloadJSON: string(payload), Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	if err := tx.Create(&inbox).Error; err != nil {
		return fmt.Errorf("create Roadmap Milestone Inbox Item: %w", err)
	}
	return recordInboxWorkflowEventAs(
		tx, inbox.ID, "source_projected", models.BuiltinSystemActorID,
		nil, inboxItemEventState(inbox, ""), requestID, createdAt,
	)
}

func roadmapMilestoneDueTimestamp(targetDate string, location *time.Location) string {
	date, _ := time.ParseInLocation(roadmapMilestoneDateLayout, targetDate, location)
	return time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 0, location).UTC().Format(time.RFC3339Nano)
}

func resolveRoadmapMilestoneInboxSources(tx *gorm.DB, milestoneID, eventType, reason, requestID, now string) error {
	var items []models.InboxItem
	query := tx.Where(
		"source_entity_type = ? AND source_entity_id = ? AND status IN ('open', 'tracking')",
		roadmapMilestoneInboxSourceType, milestoneID,
	)
	if eventType != "" {
		query = query.Where("json_extract(payload_json, '$.event_type') = ?", eventType)
	}
	if err := query.Order("id ASC").Find(&items).Error; err != nil {
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

func coordinateRoadmapMilestoneInboxSourceDeletion(tx *gorm.DB, milestoneID, requestID, now string) error {
	return coordinateInboxSourceDeletion(
		tx, roadmapMilestoneInboxSourceType, []string{milestoneID},
		"ROADMAP_MILESTONE_HAS_ACTIVE_INBOX_SOURCES",
		"Resolve or dismiss all Roadmap Milestone source Inbox Items before deleting this milestone",
		requestID, now,
	)
}
