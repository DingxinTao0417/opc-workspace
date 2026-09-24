package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Both the manual API and human-approved AI proposals use this domain boundary.
func changeAutomationRuleInTransaction(tx *gorm.DB, id string, expectedVersion int64, requestedConfig *automationConfig, enabled *bool, now time.Time) (automationRuleOutput, error) {
	var current models.AutomationRule
	if err := tx.First(&current, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return automationRuleOutput{}, newProjectRequestError(http.StatusNotFound, "AUTOMATION_RULE_NOT_FOUND", "Automation rule not found")
		}
		return automationRuleOutput{}, err
	}
	if current.Version != expectedVersion {
		return automationRuleOutput{}, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Automation rule changed in another window")
	}
	preset, exists := automationPresetByKey(current.PresetKey)
	if !exists || preset.ID != current.ID {
		return automationRuleOutput{}, errors.New("automation preset identity is invalid")
	}
	config, err := decodeAutomationConfig(current.PresetKey, current.ConfigJSON)
	if err != nil {
		return automationRuleOutput{}, err
	}
	if requestedConfig != nil {
		config, err = normalizeAutomationConfig(current.PresetKey, *requestedConfig)
		if err != nil {
			return automationRuleOutput{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		}
	}
	nextEnabled := current.Enabled
	action := "automation_rule_updated"
	reason := "configuration changed"
	if enabled != nil {
		nextEnabled = *enabled
		if nextEnabled && !preset.Available {
			return automationRuleOutput{}, newProjectRequestError(http.StatusConflict, "AUTOMATION_DEPENDENCY_UNAVAILABLE", preset.UnavailableReason)
		}
		if nextEnabled {
			action, reason = "automation_rule_enabled", "enabled by owner"
		} else {
			action, reason = "automation_rule_disabled", "disabled by owner"
		}
	}
	next, err := saveAutomationRule(tx, current, config, nextEnabled, expectedVersion, now.UTC(), action, reason)
	if err != nil {
		return automationRuleOutput{}, err
	}
	return automationRuleOutputFromModel(next)
}
