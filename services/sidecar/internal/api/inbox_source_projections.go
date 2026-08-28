package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const taskArtifactInboxSourceType = "task_artifact"
const taskBlockedInboxSourceType = "task"

func taskArtifactFollowupEventKey(artifactID string) string {
	return fmt.Sprintf("task-artifact:%s:followup", artifactID)
}

func taskArtifactFollowupTitle(name string) string {
	const prefix = "跟进产出："
	value := []rune(prefix + name)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

func taskBlockedEventKey(taskID string, blockVersion int64) string {
	return fmt.Sprintf("task:%s:blocked:%d", taskID, blockVersion)
}

func taskBlockedTitle(title string) string {
	const prefix = "任务阻塞："
	value := []rune(prefix + title)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

func projectTaskBlockedInboxItem(tx *gorm.DB, task models.Task, requestID, now string) error {
	if task.Status != "blocked" || task.BlockedReason == nil || task.BlockedAt == nil || task.BlockedFromStatus == nil {
		return errors.New("blocked Task projection requires complete blocked state")
	}
	key := taskBlockedEventKey(task.ID, task.Version)
	var existing models.InboxItem
	err := tx.First(&existing, "source_event_key = ?", key).Error
	if err == nil {
		if existing.Kind != "event" || existing.SourceEntityType != taskBlockedInboxSourceType ||
			existing.SourceEntityID == nil || *existing.SourceEntityID != task.ID {
			return errors.New("Task blocked source_event_key belongs to an incompatible Inbox Item")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	payload := map[string]any{
		"task_id": task.ID, "task_title": task.Title,
		"blocked_reason": *task.BlockedReason, "blocked_at": *task.BlockedAt,
		"blocked_from_status": *task.BlockedFromStatus, "block_version": task.Version,
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
	item := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: taskBlockedTitle(task.Title),
		Summary: "阻塞原因：" + *task.BlockedReason, SourceEntityType: taskBlockedInboxSourceType,
		SourceEntityID: &sourceID, SourceEventKey: &key, Priority: task.Priority,
		Status: "open", ResolutionPolicy: "manual", PayloadJSON: string(payloadJSON),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&item).Error; err != nil {
		return fmt.Errorf("create Task blocked Inbox Item: %w", err)
	}
	return recordInboxWorkflowEventAs(
		tx, item.ID, "source_projected", models.BuiltinSystemActorID,
		nil, inboxItemEventState(item, ""), requestID, now,
	)
}

func projectTaskArtifactFollowups(
	tx *gorm.DB,
	task models.Task,
	submission models.TaskSubmission,
	requestID string,
	now string,
) error {
	var artifacts []models.TaskArtifact
	if err := tx.Where(
		"submission_id = ? AND requires_followup = 1 AND deleted_at IS NULL",
		submission.ID,
	).Order("position ASC").Order("id ASC").Find(&artifacts).Error; err != nil {
		return err
	}
	for _, artifact := range artifacts {
		key := taskArtifactFollowupEventKey(artifact.ID)
		var existing models.InboxItem
		err := tx.First(&existing, "source_event_key = ?", key).Error
		if err == nil {
			if existing.Kind != "event" || existing.SourceEntityType != taskArtifactInboxSourceType ||
				existing.SourceEntityID == nil || *existing.SourceEntityID != artifact.ID {
				return errors.New("Task Artifact source_event_key belongs to an incompatible Inbox Item")
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		payload := map[string]any{
			"artifact_id": artifact.ID, "artifact_name": artifact.Name,
			"storage_kind": artifact.StorageKind, "task_id": task.ID,
			"task_title": task.Title, "submission_id": submission.ID,
			"submission_sequence": submission.Sequence,
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
		summary := fmt.Sprintf("任务「%s」第 %d 批产出已明确标记为需要后续处理。", task.Title, submission.Sequence)
		sourceID := artifact.ID
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: taskArtifactFollowupTitle(artifact.Name),
			Summary: summary, SourceEntityType: taskArtifactInboxSourceType,
			SourceEntityID: &sourceID, SourceEventKey: &key, Priority: task.Priority,
			Status: "open", ResolutionPolicy: "manual", PayloadJSON: string(payloadJSON),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("create Task Artifact follow-up Inbox Item: %w", err)
		}
		if err := recordInboxWorkflowEventAs(
			tx, item.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(item, ""), requestID, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func coordinateTaskArtifactInboxSourceDeletion(
	tx *gorm.DB,
	artifactIDs []string,
	conflictCode string,
	conflictMessage string,
	requestID string,
	now string,
) error {
	return coordinateInboxSourceDeletion(
		tx, taskArtifactInboxSourceType, artifactIDs, conflictCode, conflictMessage, requestID, now,
	)
}

func coordinateTaskBlockedInboxSourceDeletion(
	tx *gorm.DB,
	taskID string,
	requestID string,
	now string,
) error {
	return coordinateInboxSourceDeletion(
		tx,
		taskBlockedInboxSourceType,
		[]string{taskID},
		"TASK_HAS_ACTIVE_INBOX_SOURCES",
		"Resolve or dismiss all Task source Inbox Items before deleting this Task",
		requestID,
		now,
	)
}

func coordinateInboxSourceDeletion(
	tx *gorm.DB,
	sourceType string,
	sourceIDs []string,
	conflictCode string,
	conflictMessage string,
	requestID string,
	now string,
) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	uniqueIDs := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	sort.Strings(uniqueIDs)

	var items []models.InboxItem
	if err := tx.Where(
		"source_entity_type = ? AND source_entity_id IN ? AND source_deleted_at IS NULL",
		sourceType,
		uniqueIDs,
	).Order("id ASC").Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if item.Status == "open" || item.Status == "tracking" {
			return newProjectRequestError(http.StatusConflict, conflictCode, conflictMessage)
		}
	}
	for _, current := range items {
		next := current
		next.SourceDeletedAt = &now
		next.Version++
		next.UpdatedAt = now
		result := tx.Model(&models.InboxItem{}).
			Where("id = ? AND version = ? AND source_deleted_at IS NULL", current.ID, current.Version).
			Updates(map[string]any{
				"source_deleted_at": now,
				"version":           next.Version,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return inboxVersionConflict()
		}
		if err := recordInboxWorkflowEvent(
			tx, current.ID, "source_deleted",
			inboxItemEventState(current, ""), inboxItemEventState(next, ""),
			requestID, now,
		); err != nil {
			return err
		}
	}
	return nil
}
