package api

import (
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Shared by native HTTP commands and human-confirmed AI proposals. Callers own
// the transaction so business projections, workflow events and approval commit
// together. These helpers neither send messages nor invent client activities.
func createClientFollowupInTransaction(tx *gorm.DB, followup models.ClientFollowup, requestID string) (clientFollowupResponse, error) {
	if err := ensureClientFollowupReferences(tx, followup.ClientID, followup.AssignedActorID); err != nil {
		return clientFollowupResponse{}, err
	}
	if err := tx.Create(&followup).Error; err != nil {
		return clientFollowupResponse{}, err
	}
	row, err := loadClientFollowupRow(tx, followup.ID)
	if err != nil {
		return clientFollowupResponse{}, err
	}
	out := clientFollowupResponseFromRow(row)
	err = recordClientFollowupWorkflowEvent(tx, followup.ID, "client_followup_created", nil, clientFollowupEventState(out), requestID, followup.CreatedAt)
	return out, err
}

func prepareClientFollowupChange(tx *gorm.DB, id string, expected int64, patch map[string]any, editing bool) (clientFollowupRow, error) {
	row, err := loadClientFollowupRow(tx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, newProjectRequestError(404, "CLIENT_FOLLOWUP_NOT_FOUND", "Client followup not found")
	}
	if err != nil {
		return row, err
	}
	if row.Version != expected {
		return row, clientFollowupVersionConflict()
	}
	if row.Status != "planned" {
		return row, newProjectRequestError(409, "CLIENT_FOLLOWUP_FINAL", "Terminal client followups cannot be changed")
	}
	if editing {
		if err := ensureClientFollowupClientIsPlannable(tx, row.ClientID); err != nil {
			return row, err
		}
		if actor, exists := patch["assigned_actor_id"]; exists {
			if err := ensureClientFollowupAssigneeIsAvailable(tx, actor.(string)); err != nil {
				return row, err
			}
		}
	}
	return row, nil
}

func changeClientFollowupInTransaction(tx *gorm.DB, id string, expected int64, patch map[string]any, action, requestID, now string) (clientFollowupResponse, error) {
	previous, err := prepareClientFollowupChange(tx, id, expected, patch, action == "client_followup_updated")
	if err != nil {
		return clientFollowupResponse{}, err
	}
	updates := make(map[string]any, len(patch)+2)
	for key, value := range patch {
		updates[key] = value
	}
	updates["updated_at"], updates["version"] = now, gorm.Expr("version + 1")
	result := tx.Model(&models.ClientFollowup{}).Where("id = ? AND version = ? AND status = 'planned'", id, expected).Updates(updates)
	if result.Error != nil {
		return clientFollowupResponse{}, result.Error
	}
	if result.RowsAffected != 1 {
		return clientFollowupResponse{}, clientFollowupVersionConflict()
	}
	row, err := loadClientFollowupRow(tx, id)
	if err != nil {
		return clientFollowupResponse{}, err
	}
	out := clientFollowupResponseFromRow(row)
	reason := clientFollowupInboxResolutionReason(action)
	if action == "client_followup_updated" {
		reason = "客户回访计划已更新"
	}
	if err := resolveClientFollowupInboxSources(tx, id, reason, requestID, now); err != nil {
		return out, err
	}
	err = recordClientFollowupWorkflowEvent(tx, id, action, clientFollowupEventState(clientFollowupResponseFromRow(previous)), clientFollowupEventState(out), requestID, now)
	return out, err
}

// Completion and an optional next plan are one command. The caller must not
// split them into independent approvals or claim that a plan contacted anyone.
func completeClientFollowupInTransaction(tx *gorm.DB, id string, expected int64, result string, nextStep *string, completedAt string, nextPlan *models.ClientFollowup, requestID, now string) (clientFollowupResponse, error) {
	previous, err := prepareClientFollowupChange(tx, id, expected, nil, false)
	if err != nil {
		return clientFollowupResponse{}, err
	}
	if nextPlan != nil {
		if err := ensureClientFollowupReferences(tx, previous.ClientID, nextPlan.AssignedActorID); err != nil {
			return clientFollowupResponse{}, err
		}
	}
	write := tx.Model(&models.ClientFollowup{}).Where("id = ? AND version = ? AND status = 'planned'", id, expected).Updates(map[string]any{"status": "completed", "completed_at": completedAt, "result": result, "next_step": nextStep, "updated_at": now, "version": gorm.Expr("version + 1")})
	if write.Error != nil {
		return clientFollowupResponse{}, write.Error
	}
	if write.RowsAffected != 1 {
		return clientFollowupResponse{}, clientFollowupVersionConflict()
	}
	var nextResponse *clientFollowupResponse
	if nextPlan != nil {
		// Work on a copy, so a transaction failure cannot mutate the caller's draft.
		next := *nextPlan
		next.ClientID, next.CreatedAt, next.UpdatedAt = previous.ClientID, now, now
		if err := tx.Create(&next).Error; err != nil {
			return clientFollowupResponse{}, err
		}
		row, err := loadClientFollowupRow(tx, next.ID)
		if err != nil {
			return clientFollowupResponse{}, err
		}
		out := clientFollowupResponseFromRow(row)
		nextResponse = &out
	}
	row, err := loadClientFollowupRow(tx, id)
	if err != nil {
		return clientFollowupResponse{}, err
	}
	out := clientFollowupResponseFromRow(row)
	out.NextFollowup = nextResponse
	if err := resolveClientFollowupInboxSources(tx, id, clientFollowupInboxResolutionReason("client_followup_completed"), requestID, now); err != nil {
		return out, err
	}
	currentEvent := clientFollowupEventState(out)
	if nextResponse != nil {
		currentEvent["next_followup_id"] = nextResponse.ID
	}
	if err := recordClientFollowupWorkflowEvent(tx, id, "client_followup_completed", clientFollowupEventState(clientFollowupResponseFromRow(previous)), currentEvent, requestID, now); err != nil {
		return out, err
	}
	if nextResponse != nil {
		err = recordClientFollowupWorkflowEvent(tx, nextResponse.ID, "client_followup_created_from_completion", nil, map[string]any{"client_id": nextResponse.ClientID, "completed_followup_id": id, "scheduled_at": nextResponse.ScheduledAt, "timezone": nextResponse.Timezone, "channel": nextResponse.Channel, "purpose": nextResponse.Purpose, "priority": nextResponse.Priority, "version": nextResponse.Version}, requestID, now)
	}
	return out, err
}

func rescheduleClientFollowupInTransaction(tx *gorm.DB, id string, expected int64, next models.ClientFollowup, reason, requestID, now string) (clientFollowupResponse, clientFollowupResponse, error) {
	previous, err := prepareClientFollowupChange(tx, id, expected, nil, false)
	if err != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, err
	}
	if err := ensureClientFollowupReferences(tx, previous.ClientID, next.AssignedActorID); err != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, err
	}
	write := tx.Model(&models.ClientFollowup{}).Where("id = ? AND version = ? AND status = 'planned'", id, expected).Updates(map[string]any{"status": "cancelled", "cancelled_at": now, "cancel_reason": reason, "updated_at": now, "version": gorm.Expr("version + 1")})
	if write.Error != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, write.Error
	}
	if write.RowsAffected != 1 {
		return clientFollowupResponse{}, clientFollowupResponse{}, clientFollowupVersionConflict()
	}
	next.ClientID, next.RescheduledFromID, next.CreatedAt, next.UpdatedAt = previous.ClientID, &id, now, now
	if err := tx.Create(&next).Error; err != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, err
	}
	oldRow, err := loadClientFollowupRow(tx, id)
	if err != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, err
	}
	nextRow, err := loadClientFollowupRow(tx, next.ID)
	if err != nil {
		return clientFollowupResponse{}, clientFollowupResponse{}, err
	}
	oldOut, nextOut := clientFollowupResponseFromRow(oldRow), clientFollowupResponseFromRow(nextRow)
	if err := resolveClientFollowupInboxSources(tx, id, "客户回访已重新安排", requestID, now); err != nil {
		return oldOut, nextOut, err
	}
	if err := recordClientFollowupWorkflowEvent(tx, id, "client_followup_rescheduled", clientFollowupEventState(clientFollowupResponseFromRow(previous)), clientFollowupEventState(oldOut), requestID, now); err != nil {
		return oldOut, nextOut, err
	}
	err = recordClientFollowupWorkflowEvent(tx, next.ID, "client_followup_reschedule_created", nil, clientFollowupEventState(nextOut), requestID, now)
	return oldOut, nextOut, err
}
