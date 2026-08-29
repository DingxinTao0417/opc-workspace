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

const clientFollowupInboxSourceType = "client_followup"

// projectDueClientFollowups projects each due planned followup exactly once. The
// unique Inbox source key is intentionally based on followup id and version:
// edits before the plan becomes due are reflected in the same plan, while a
// reschedule has a new plan id and cannot revive the old terminal plan.
func (a *API) projectDueClientFollowups(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	nowText := formatInboxTimestamp(a.options.Now().UTC())
	var ids []string
	if err := a.db.WithContext(ctx).Table("client_followups").Where("status = 'planned' AND scheduled_at <= ?", nowText).Order("scheduled_at ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("list due client followups: %w", err)
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.projectClientFollowup(ctx, id, nowText); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectClientFollowup(ctx context.Context, id, nowText string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var followup models.ClientFollowup
		if err := tx.First(&followup, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if followup.Status != "planned" || followup.ScheduledAt > nowText {
			return nil
		}
		key := fmt.Sprintf("followup:%s:due:%d", followup.ID, followup.Version)
		var existing models.InboxItem
		err := tx.First(&existing, "source_event_key = ?", key).Error
		if err == nil {
			if existing.Kind != "event" || existing.SourceEntityType != clientFollowupInboxSourceType || existing.SourceEntityID == nil || *existing.SourceEntityID != followup.ID {
				return errors.New("Client Followup source_event_key belongs to an incompatible Inbox Item")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var client models.Client
		if err := tx.First(&client, "id = ?", followup.ClientID).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"client_followup_id": followup.ID, "client_id": followup.ClientID, "scheduled_at": followup.ScheduledAt, "timezone": followup.Timezone, "channel": followup.Channel})
		if err != nil {
			return err
		}
		summary := fmt.Sprintf("客户：%s · 渠道：%s", client.Name, followup.Channel)
		priority := map[string]string{"high": "P1", "normal": "P2", "low": "P3"}[followup.Priority]
		inbox := models.InboxItem{ID: uuid.NewString(), Kind: "event", Title: followup.Purpose, Summary: summary, SourceEntityType: clientFollowupInboxSourceType, SourceEntityID: &followup.ID, SourceEventKey: &key, Priority: priority, Status: "open", ResolutionPolicy: "manual", DueAt: &followup.ScheduledAt, PayloadJSON: string(payload), Version: 1, CreatedAt: nowText, UpdatedAt: nowText}
		if err := tx.Create(&inbox).Error; err != nil {
			return fmt.Errorf("create Client Followup Inbox Item: %w", err)
		}
		if err := recordReminderInboxEvent(tx, inbox, nowText); err != nil {
			return err
		}
		return recordClientFollowupWorkflowEvent(tx, followup.ID, "client_followup_due", nil, map[string]any{"inbox_item_id": inbox.ID, "scheduled_at": followup.ScheduledAt, "version": followup.Version}, "", nowText)
	})
}

// resolveClientFollowupInboxSources archives active due projections when the
// underlying local followup changes or becomes terminal. The Inbox event stays
// auditable, but it must not remain a second actionable representation of a
// plan that has already been handled or superseded.
func resolveClientFollowupInboxSources(
	tx *gorm.DB,
	followupID,
	reason,
	requestID,
	now string,
) error {
	var items []models.InboxItem
	if err := tx.Where(
		"source_entity_type = ? AND source_entity_id = ? AND status IN ('open', 'tracking')",
		clientFollowupInboxSourceType,
		followupID,
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
			tx,
			current.ID,
			"source_resolved",
			inboxItemEventState(current, reason),
			inboxItemEventState(next, reason),
			requestID,
			now,
		); err != nil {
			return err
		}
	}
	return nil
}
