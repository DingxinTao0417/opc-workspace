package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type automationRuleOutput struct {
	ID                string           `json:"id"`
	PresetKey         string           `json:"preset_key"`
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Status            string           `json:"status"`
	Available         bool             `json:"available"`
	UnavailableReason string           `json:"unavailable_reason,omitempty"`
	TriggerType       string           `json:"trigger_type"`
	TriggerLabel      string           `json:"trigger_label"`
	ActionType        string           `json:"action_type"`
	ActionLabel       string           `json:"action_label"`
	Config            automationConfig `json:"config"`
	NextRunAt         *string          `json:"next_run_at"`
	Permissions       []string         `json:"permissions"`
	Version           int64            `json:"version"`
	CreatedAt         string           `json:"created_at"`
	UpdatedAt         string           `json:"updated_at"`
}

type automationRuleConfigRequest struct {
	Config automationConfig `json:"config"`
}

type automationPreviewOutput struct {
	CanEnable         bool             `json:"can_enable"`
	UnavailableReason string           `json:"unavailable_reason,omitempty"`
	TriggerSummary    string           `json:"trigger_summary"`
	ActionSummary     string           `json:"action_summary"`
	Config            automationConfig `json:"config"`
	NextRunAt         *string          `json:"next_run_at"`
	Permissions       []string         `json:"permissions"`
}

func (a *API) ensureAutomationRules(now time.Time) error {
	nowText := formatInboxTimestamp(now.UTC())
	return a.db.Transaction(func(tx *gorm.DB) error {
		for _, preset := range automationPresets {
			configJSON, err := encodeAutomationConfig(preset.DefaultConfig)
			if err != nil {
				return err
			}
			if err := tx.Exec(`
				INSERT INTO automation_rules(
					id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at
				) VALUES (?, ?, 0, ?, NULL, 1, ?, ?)
				ON CONFLICT(preset_key) DO NOTHING
			`, preset.ID, preset.PresetKey, configJSON, nowText, nowText).Error; err != nil {
				return fmt.Errorf("ensure automation preset %s: %w", preset.PresetKey, err)
			}
			var rule models.AutomationRule
			if err := tx.First(&rule, "preset_key = ?", preset.PresetKey).Error; err != nil {
				return err
			}
			if rule.ID != preset.ID {
				return fmt.Errorf("automation preset %s has an incompatible identity", preset.PresetKey)
			}
			config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
			if err != nil {
				return err
			}
			if !preset.Available && rule.Enabled {
				if _, err := saveAutomationRule(tx, rule, config, false, rule.Version, now.UTC(), "automation_rule_disabled", "dependency unavailable"); err != nil {
					return err
				}
				continue
			}
			if !rule.Enabled && rule.NextRunAt != nil {
				if err := tx.Model(&models.AutomationRule{}).Where("id = ?", rule.ID).Update("next_run_at", nil).Error; err != nil {
					return err
				}
				continue
			}
			if rule.Enabled && preset.TriggerType == "schedule" && rule.NextRunAt == nil {
				next, err := nextAutomationSchedule(rule.PresetKey, config, now.UTC())
				if err != nil {
					return err
				}
				nextText := formatInboxTimestamp(next.UTC())
				if err := tx.Model(&models.AutomationRule{}).Where("id = ?", rule.ID).Update("next_run_at", nextText).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (a *API) listAutomationRules(c *gin.Context) {
	var rules []models.AutomationRule
	if err := a.db.WithContext(c.Request.Context()).Find(&rules).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	byKey := make(map[string]models.AutomationRule, len(rules))
	for _, rule := range rules {
		byKey[rule.PresetKey] = rule
	}
	data := make([]automationRuleOutput, 0, len(automationPresets))
	for _, preset := range automationPresets {
		rule, ok := byKey[preset.PresetKey]
		if !ok {
			writeDatabaseError(c)
			return
		}
		output, err := automationRuleOutputFromModel(rule)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		data = append(data, output)
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (a *API) getAutomationRule(c *gin.Context) {
	rule, ok := a.automationRuleFromContext(c)
	if !ok {
		return
	}
	output, err := automationRuleOutputFromModel(rule)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, output.Version)
	c.JSON(http.StatusOK, gin.H{"data": output})
}

func (a *API) previewAutomationRule(c *gin.Context) {
	rule, ok := a.automationRuleFromContext(c)
	if !ok {
		return
	}
	var input automationRuleConfigRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	config, err := normalizeAutomationConfig(rule.PresetKey, input.Config)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	preset, _ := automationPresetByKey(rule.PresetKey)
	preview := automationPreviewOutput{
		CanEnable: preset.Available, UnavailableReason: preset.UnavailableReason,
		TriggerSummary: preset.TriggerLabel, ActionSummary: preset.ActionLabel,
		Config: config, Permissions: append([]string(nil), preset.Permissions...),
	}
	if preset.TriggerType == "schedule" {
		next, err := nextAutomationSchedule(rule.PresetKey, config, a.options.Now().UTC())
		if err != nil {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			return
		}
		nextText := formatInboxTimestamp(next.UTC())
		preview.NextRunAt = &nextText
	}
	c.JSON(http.StatusOK, gin.H{"data": preview})
}

func (a *API) updateAutomationRule(c *gin.Context) {
	a.changeAutomationRule(c, nil)
}

func (a *API) enableAutomationRule(c *gin.Context) {
	enabled := true
	a.changeAutomationRule(c, &enabled)
}

func (a *API) disableAutomationRule(c *gin.Context) {
	enabled := false
	a.changeAutomationRule(c, &enabled)
}

func (a *API) changeAutomationRule(c *gin.Context, enabled *bool) {
	id, ok := automationRuleID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var requestedConfig *automationConfig
	if enabled == nil {
		var input automationRuleConfigRequest
		if err := decodeJSON(c, &input); err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
			return
		}
		requestedConfig = &input.Config
	} else if c.Request.Body != nil && c.Request.ContentLength > 0 {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "This command does not accept a request body")
		return
	}

	var response automationRuleOutput
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current models.AutomationRule
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "AUTOMATION_RULE_NOT_FOUND", "Automation rule not found")
			}
			return err
		}
		if current.Version != expectedVersion {
			return newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Automation rule changed in another window")
		}
		preset, exists := automationPresetByKey(current.PresetKey)
		if !exists || preset.ID != current.ID {
			return errors.New("automation preset identity is invalid")
		}
		config, err := decodeAutomationConfig(current.PresetKey, current.ConfigJSON)
		if err != nil {
			return err
		}
		if requestedConfig != nil {
			config, err = normalizeAutomationConfig(current.PresetKey, *requestedConfig)
			if err != nil {
				return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
			}
		}
		nextEnabled := current.Enabled
		action := "automation_rule_updated"
		reason := "configuration changed"
		if enabled != nil {
			nextEnabled = *enabled
			if nextEnabled && !preset.Available {
				return newProjectRequestError(http.StatusConflict, "AUTOMATION_DEPENDENCY_UNAVAILABLE", preset.UnavailableReason)
			}
			if nextEnabled {
				action, reason = "automation_rule_enabled", "enabled by owner"
			} else {
				action, reason = "automation_rule_disabled", "disabled by owner"
			}
		}
		next, err := saveAutomationRule(tx, current, config, nextEnabled, expectedVersion, a.options.Now().UTC(), action, reason)
		if err != nil {
			return err
		}
		response, err = automationRuleOutputFromModel(next)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func saveAutomationRule(
	tx *gorm.DB,
	current models.AutomationRule,
	config automationConfig,
	enabled bool,
	expectedVersion int64,
	now time.Time,
	action,
	reason string,
) (models.AutomationRule, error) {
	config, err := normalizeAutomationConfig(current.PresetKey, config)
	if err != nil {
		return models.AutomationRule{}, err
	}
	configJSON, err := encodeAutomationConfig(config)
	if err != nil {
		return models.AutomationRule{}, err
	}
	// next_run_at is scheduler-owned runtime state. Replaying an enable,
	// disable, or unchanged configuration command must preserve it and must
	// not advance the user-facing version or append another audit event.
	if current.Enabled == enabled && current.ConfigJSON == configJSON {
		return current, nil
	}
	var nextRunAt *string
	preset, exists := automationPresetByKey(current.PresetKey)
	if !exists {
		return models.AutomationRule{}, errors.New("automation preset is not supported")
	}
	if enabled && preset.TriggerType == "schedule" {
		next, err := nextAutomationSchedule(current.PresetKey, config, now)
		if err != nil {
			return models.AutomationRule{}, err
		}
		value := formatInboxTimestamp(next.UTC())
		nextRunAt = &value
	}
	nowText := formatInboxTimestamp(now.UTC())
	result := tx.Model(&models.AutomationRule{}).
		Where("id = ? AND version = ?", current.ID, expectedVersion).
		Updates(map[string]any{
			"enabled": enabled, "config_json": configJSON, "next_run_at": nextRunAt,
			"version": expectedVersion + 1, "updated_at": nowText,
		})
	if result.Error != nil {
		return models.AutomationRule{}, result.Error
	}
	if result.RowsAffected == 0 {
		return models.AutomationRule{}, newProjectRequestError(http.StatusConflict, "VERSION_CONFLICT", "Automation rule changed in another window")
	}
	var next models.AutomationRule
	if err := tx.First(&next, "id = ?", current.ID).Error; err != nil {
		return models.AutomationRule{}, err
	}
	if err := recordAutomationWorkflowEvent(tx, next.ID, action, automationRuleEventState(current, ""), automationRuleEventState(next, reason), models.BuiltinOwnerActorID, nowText); err != nil {
		return models.AutomationRule{}, err
	}
	return next, nil
}

func (a *API) updateAutomationRuleFacts(current models.AutomationRule, config automationConfig, enabled bool, now time.Time) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		_, err := saveAutomationRule(tx, current, config, enabled, current.Version, now.UTC(), "automation_rule_updated", "internal test setup")
		return err
	})
}

func (a *API) automationRuleByPreset(presetKey string) (models.AutomationRule, error) {
	var rule models.AutomationRule
	err := a.db.First(&rule, "preset_key = ?", presetKey).Error
	return rule, err
}

func (a *API) automationRuleFromContext(c *gin.Context) (models.AutomationRule, bool) {
	id, ok := automationRuleID(c)
	if !ok {
		return models.AutomationRule{}, false
	}
	var rule models.AutomationRule
	if err := a.db.WithContext(c.Request.Context()).First(&rule, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AUTOMATION_RULE_NOT_FOUND", "Automation rule not found")
		} else {
			writeDatabaseError(c)
		}
		return models.AutomationRule{}, false
	}
	return rule, true
}

func automationRuleID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_AUTOMATION_RULE_ID", "Automation rule id must be a canonical UUID")
		return "", false
	}
	return id, true
}

func automationRuleOutputFromModel(rule models.AutomationRule) (automationRuleOutput, error) {
	preset, ok := automationPresetByKey(rule.PresetKey)
	if !ok || preset.ID != rule.ID {
		return automationRuleOutput{}, errors.New("automation preset identity is invalid")
	}
	config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
	if err != nil {
		return automationRuleOutput{}, err
	}
	status := "disabled"
	if !preset.Available {
		status = "unavailable"
	} else if rule.Enabled {
		status = "enabled"
	}
	return automationRuleOutput{
		ID: rule.ID, PresetKey: rule.PresetKey, Name: preset.Name, Description: preset.Description,
		Status: status, Available: preset.Available, UnavailableReason: preset.UnavailableReason,
		TriggerType: preset.TriggerType, TriggerLabel: preset.TriggerLabel,
		ActionType: preset.ActionType, ActionLabel: preset.ActionLabel,
		Config: config, NextRunAt: rule.NextRunAt, Permissions: append([]string(nil), preset.Permissions...),
		Version: rule.Version, CreatedAt: normalizeTimestamp(rule.CreatedAt), UpdatedAt: normalizeTimestamp(rule.UpdatedAt),
	}, nil
}

func automationRuleEventState(rule models.AutomationRule, reason string) map[string]any {
	state := map[string]any{
		"preset_key": rule.PresetKey, "enabled": rule.Enabled, "config_json": rule.ConfigJSON,
		"next_run_at": rule.NextRunAt, "version": rule.Version,
	}
	if reason != "" {
		state["reason"] = reason
	}
	return state
}

func recordAutomationWorkflowEvent(tx *gorm.DB, aggregateID, action string, previous, current map[string]any, actorID, nowText string) error {
	previousJSON, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return err
	}
	previousText := string(previousJSON)
	currentText := string(currentJSON)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "automation_rule", AggregateID: aggregateID,
		Action: action, ActorID: &actorID, PreviousJSON: &previousText,
		CurrentJSON: &currentText, CreatedAt: nowText,
	}
	return tx.Create(&event).Error
}
