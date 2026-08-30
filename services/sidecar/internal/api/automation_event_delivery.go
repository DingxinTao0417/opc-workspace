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
	"gorm.io/gorm/clause"
)

const (
	automationEventDeliveryBatchSize = 100
	automationDeliveryInitialBackoff = 15 * time.Second
	automationDeliveryMaxBackoff     = 15 * time.Minute
)

func enqueueProjectCompletionAutomationDelivery(
	tx *gorm.DB,
	eventID string,
	project models.Project,
	nowText string,
) error {
	actionForPriority := func(priority string) map[string]any {
		return map[string]any{
			"action_type": "inbox_item", "project_id": project.ID, "project_name": project.Name,
			"title": automationProjectCompletionTitle(project.Name), "priority": priority,
		}
	}
	return enqueueAutomationEventDelivery(
		tx, automationPresetProjectCompleted, eventID, nowText, actionForPriority,
	)
}

func enqueueInvoiceOverdueAutomationDelivery(
	tx *gorm.DB,
	eventID string,
	invoice models.Invoice,
	nowText string,
) error {
	actionForPriority := func(priority string) map[string]any {
		return automationInvoiceOverdueActionSnapshot(invoice, priority)
	}
	return enqueueAutomationEventDelivery(
		tx, automationPresetInvoiceOverdue, eventID, nowText, actionForPriority,
	)
}

func enqueueAutomationEventDelivery(
	tx *gorm.DB,
	presetKey string,
	eventID string,
	nowText string,
	actionForPriority func(priority string) map[string]any,
) error {
	var rule models.AutomationRule
	err := tx.First(&rule, "preset_key = ? AND enabled = 1", presetKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	logicalKey := "event:" + rule.ID + ":" + eventID
	existingRun, err := matchingExistingAutomationEventRun(tx, rule.ID, eventID, logicalKey)
	if err != nil {
		return err
	}
	if existingRun {
		return nil
	}
	existingDelivery, err := matchingExistingAutomationEventDelivery(
		tx, rule.ID, rule.PresetKey, eventID, logicalKey,
	)
	if err != nil {
		return err
	}
	if existingDelivery {
		return nil
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, nowText)
	if err != nil {
		return err
	}
	capturedAtText := formatInboxTimestamp(capturedAt.UTC())
	config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
	if err != nil {
		return err
	}
	configJSON, err := encodeAutomationConfig(config)
	if err != nil {
		return err
	}
	actionJSON, err := json.Marshal(actionForPriority(config.Priority))
	if err != nil {
		return err
	}
	delivery := models.AutomationEventDelivery{
		ID:                 uuid.NewString(),
		RuleID:             rule.ID,
		PresetKey:          rule.PresetKey,
		RuleVersion:        rule.Version,
		SourceEventID:      eventID,
		LogicalKey:         logicalKey,
		ConfigSnapshotJSON: configJSON,
		ActionSnapshotJSON: string(actionJSON),
		DeliveryAttempts:   0,
		AvailableAt:        capturedAtText,
		CapturedAt:         capturedAtText,
		UpdatedAt:          capturedAtText,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "rule_id"}, {Name: "source_event_id"}},
		DoNothing: true,
	}).Create(&delivery)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var existing models.AutomationEventDelivery
	if err := tx.First(&existing, "rule_id = ? AND source_event_id = ?", rule.ID, eventID).Error; err != nil {
		return err
	}
	if !automationEventDeliveryHasIdentity(existing, rule.ID, rule.PresetKey, eventID, logicalKey) {
		return errors.New("automation event delivery replay conflicts with the captured identity")
	}
	return nil
}

func matchingExistingAutomationEventDelivery(
	tx *gorm.DB,
	ruleID string,
	presetKey string,
	eventID string,
	logicalKey string,
) (bool, error) {
	var delivery models.AutomationEventDelivery
	err := tx.Where("rule_id = ? AND source_event_id = ?", ruleID, eventID).Take(&delivery).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !automationEventDeliveryHasIdentity(delivery, ruleID, presetKey, eventID, logicalKey) {
		return false, errors.New("automation event delivery replay conflicts with the captured identity")
	}
	return true, nil
}

func automationEventDeliveryHasIdentity(
	delivery models.AutomationEventDelivery,
	ruleID string,
	presetKey string,
	eventID string,
	logicalKey string,
) bool {
	return delivery.RuleID == ruleID && delivery.PresetKey == presetKey &&
		delivery.SourceEventID == eventID && delivery.LogicalKey == logicalKey
}

func matchingExistingAutomationEventRun(
	tx *gorm.DB,
	ruleID string,
	eventID string,
	logicalKey string,
) (bool, error) {
	var run models.AutomationRun
	err := tx.Where(
		"rule_id = ? AND source_event_id = ? AND attempt = 1",
		ruleID, eventID,
	).Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if run.RuleID != ruleID || run.TriggerType != "event" ||
		run.SourceEventID == nil || *run.SourceEventID != eventID || run.ScheduledFor != nil ||
		run.LogicalKey != logicalKey || run.DedupeKey != logicalKey+":attempt:1" || run.Attempt != 1 ||
		run.RetryOfRunID != nil || run.CausedByRunID != nil || run.CausalDepth != 0 {
		return false, errors.New("existing Automation run conflicts with the event replay identity")
	}
	return true, nil
}

// consumeDueAutomationEventDeliveries claims and processes at most one bounded
// batch. Each claimed row is isolated in its own transaction so one poisoned
// delivery cannot starve later rows in the same batch.
func (a *API) consumeDueAutomationEventDeliveries(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.automationEventDeliveryMu.Lock()
	defer a.automationEventDeliveryMu.Unlock()

	now = now.UTC()
	nowText := formatInboxTimestamp(now)
	var deliveries []models.AutomationEventDelivery
	if err := a.db.WithContext(ctx).
		Where("available_at <= ?", nowText).
		Order("available_at ASC").Order("captured_at ASC").Order("id ASC").
		Limit(automationEventDeliveryBatchSize).
		Find(&deliveries).Error; err != nil {
		return fmt.Errorf("list due Automation event deliveries: %w", err)
	}
	var processingErrors []error
	for index := range deliveries {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(processingErrors, err)...)
		}
		delivery, claimed, err := a.claimAutomationEventDelivery(ctx, deliveries[index], now)
		if err != nil {
			processingErrors = append(processingErrors, err)
			continue
		}
		if !claimed {
			continue
		}
		if err := a.consumeClaimedAutomationEventDelivery(ctx, delivery, now); err != nil {
			processingErrors = append(processingErrors, err)
			if recordErr := a.recordAutomationEventDeliveryFailure(ctx, delivery, now); recordErr != nil {
				processingErrors = append(processingErrors, recordErr)
			}
		}
	}
	return errors.Join(processingErrors...)
}

func (a *API) claimAutomationEventDelivery(
	ctx context.Context,
	candidate models.AutomationEventDelivery,
	now time.Time,
) (models.AutomationEventDelivery, bool, error) {
	nextAttempt := candidate.DeliveryAttempts + 1
	nextAvailableAt := formatInboxTimestamp(now.Add(automationEventDeliveryBackoff(nextAttempt)).UTC())
	nowText := formatInboxTimestamp(now.UTC())
	claimUpdatedAt := formatInboxTimestamp(now.Add(time.Nanosecond).UTC())
	result := a.db.WithContext(ctx).Model(&models.AutomationEventDelivery{}).
		Where("id = ? AND delivery_attempts = ? AND available_at = ? AND available_at <= ?",
			candidate.ID, candidate.DeliveryAttempts, candidate.AvailableAt, nowText).
		Updates(map[string]any{
			"delivery_attempts": nextAttempt,
			"available_at":      nextAvailableAt,
			"updated_at":        claimUpdatedAt,
		})
	if result.Error != nil {
		return candidate, false, fmt.Errorf("claim Automation event delivery %s: %w", candidate.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return candidate, false, nil
	}
	candidate.DeliveryAttempts = nextAttempt
	candidate.AvailableAt = nextAvailableAt
	candidate.UpdatedAt = claimUpdatedAt
	return candidate, true, nil
}

func automationEventDeliveryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := automationDeliveryInitialBackoff
	for current := 1; current < attempt && delay < automationDeliveryMaxBackoff; current++ {
		delay *= 2
		if delay >= automationDeliveryMaxBackoff {
			return automationDeliveryMaxBackoff
		}
	}
	return delay
}

func (a *API) consumeClaimedAutomationEventDelivery(
	ctx context.Context,
	delivery models.AutomationEventDelivery,
	now time.Time,
) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.AutomationEventDelivery
		if err := tx.First(&current, "id = ? AND delivery_attempts = ? AND available_at = ?",
			delivery.ID, delivery.DeliveryAttempts, delivery.AvailableAt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var existing models.AutomationRun
		existingErr := tx.Where(
			"rule_id = ? AND source_event_id = ? AND attempt = 1",
			current.RuleID, current.SourceEventID,
		).Take(&existing).Error
		if existingErr == nil {
			if err := validateAutomationRunMatchesEventDelivery(tx, existing, current); err != nil {
				return err
			}
		} else if errors.Is(existingErr, gorm.ErrRecordNotFound) {
			config, err := decodeAutomationConfig(current.PresetKey, current.ConfigSnapshotJSON)
			if err != nil {
				return err
			}
			var action map[string]any
			if err := json.Unmarshal([]byte(current.ActionSnapshotJSON), &action); err != nil {
				return err
			}
			sourceEventID := current.SourceEventID
			rule := models.AutomationRule{
				ID: current.RuleID, PresetKey: current.PresetKey, Enabled: true,
				Version: current.RuleVersion, ConfigJSON: current.ConfigSnapshotJSON,
			}
			if _, err := executeAutomationAttempt(tx, automationAttemptInput{
				Rule: rule, TriggerType: "event", SourceEventID: &sourceEventID,
				LogicalKey: current.LogicalKey, Attempt: 1, Config: config,
				ActionSnapshot: action, Now: now.UTC(),
			}); err != nil {
				return err
			}
		} else {
			return existingErr
		}
		result := tx.Delete(&models.AutomationEventDelivery{}, "id = ? AND delivery_attempts = ? AND available_at = ?",
			current.ID, current.DeliveryAttempts, current.AvailableAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Automation event delivery claim changed before deletion")
		}
		return nil
	})
}

func validateAutomationRunMatchesEventDelivery(
	tx *gorm.DB,
	run models.AutomationRun,
	delivery models.AutomationEventDelivery,
) error {
	var rule models.AutomationRule
	if err := tx.Select("id", "preset_key").First(&rule, "id = ?", delivery.RuleID).Error; err != nil {
		return err
	}
	wantDedupeKey := delivery.LogicalKey + ":attempt:1"
	if rule.PresetKey != delivery.PresetKey || run.RuleID != delivery.RuleID ||
		run.RuleVersion != delivery.RuleVersion || run.TriggerType != "event" ||
		run.SourceEventID == nil || *run.SourceEventID != delivery.SourceEventID ||
		run.ScheduledFor != nil || run.LogicalKey != delivery.LogicalKey || run.DedupeKey != wantDedupeKey ||
		run.Attempt != 1 || run.RetryOfRunID != nil || run.CausedByRunID != nil || run.CausalDepth != 0 ||
		run.ConfigSnapshotJSON != delivery.ConfigSnapshotJSON ||
		run.ActionSnapshotJSON != delivery.ActionSnapshotJSON {
		return errors.New("existing Automation run conflicts with the captured event delivery")
	}
	return nil
}

func (a *API) recordAutomationEventDeliveryFailure(
	ctx context.Context,
	delivery models.AutomationEventDelivery,
	now time.Time,
) error {
	nowText := formatInboxTimestamp(now.Add(2 * time.Nanosecond).UTC())
	errorCode := "DELIVERY_PROCESSING_FAILED"
	result := a.db.WithContext(ctx).Model(&models.AutomationEventDelivery{}).
		Where("id = ? AND delivery_attempts = ? AND available_at = ?",
			delivery.ID, delivery.DeliveryAttempts, delivery.AvailableAt).
		Updates(map[string]any{
			"last_error_code": errorCode,
			"last_error_at":   nowText,
			"updated_at":      nowText,
		})
	if result.Error != nil {
		return fmt.Errorf("record Automation event delivery failure %s: %w", delivery.ID, result.Error)
	}
	return nil
}

func (a *API) consumeAutomationEventDeliveriesBestEffort(reason string) {
	if err := a.consumeDueAutomationEventDeliveries(context.Background(), a.options.Now().UTC()); err != nil && a.options.Logger != nil {
		a.options.Logger.Printf("Automation event delivery %s scan failed: %v", reason, err)
	}
}
