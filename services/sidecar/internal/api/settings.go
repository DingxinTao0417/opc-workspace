package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const settingsSchemaVersion = 1

var (
	settingKeys      = []string{"workspace", "general", "appearance", "focus"}
	settingKeySet    = map[string]struct{}{"workspace": {}, "general": {}, "appearance": {}, "focus": {}}
	controlledAvatar = regexp.MustCompile(`^avatars/[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.(?:png|jpg|webp)$`)
)

type workspaceSettingValue struct {
	DisplayName string  `json:"display_name"`
	AvatarRef   *string `json:"avatar_ref"`
}

type generalSettingValue struct {
	DefaultRoute      string `json:"default_route"`
	ShowRightOverview bool   `json:"show_right_overview"`
	ReduceMotion      bool   `json:"reduce_motion"`
}

type appearanceSettingValue struct {
	Theme string `json:"theme"`
}

type focusSettingValue struct {
	FocusMinutes   int  `json:"focus_minutes"`
	BreakMinutes   int  `json:"break_minutes"`
	Cycles         int  `json:"cycles"`
	AutoStartBreak bool `json:"auto_start_break"`
	AutoStartFocus bool `json:"auto_start_focus"`
	SoundEnabled   bool `json:"sound_enabled"`
}

type settingResponse struct {
	Key              string          `json:"key"`
	Value            json.RawMessage `json:"value"`
	SchemaVersion    int             `json:"schema_version"`
	Version          int64           `json:"version"`
	Stored           bool            `json:"stored"`
	UpdatedByActorID *string         `json:"updated_by_actor_id"`
	UpdatedAt        *string         `json:"updated_at"`
}

type settingsResponse struct {
	SchemaVersion int               `json:"schema_version"`
	Items         []settingResponse `json:"items"`
}

type settingUpdateRequest struct {
	Key             string          `json:"key"`
	ExpectedVersion *int64          `json:"expected_version"`
	Value           json.RawMessage `json:"value"`
}

type updateSettingsRequest struct {
	Updates []settingUpdateRequest `json:"updates"`
}

type preparedSettingUpdate struct {
	key             string
	expectedVersion int64
	valueJSON       string
}

func (a *API) listSettings(c *gin.Context) {
	response, err := loadSettingsResponse(a.db.WithContext(c.Request.Context()))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) updateSettings(c *gin.Context) {
	var input updateSettingsRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	prepared, err := prepareSettingUpdates(input.Updates)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error())
		return
	}
	nowText := a.options.Now().UTC().Format(time.RFC3339Nano)
	requestID := requestIDFromContext(c)
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for _, update := range prepared {
			if err := applySettingUpdate(tx, update, requestID, nowText); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	response, err := loadSettingsResponse(a.db.WithContext(c.Request.Context()))
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func prepareSettingUpdates(input []settingUpdateRequest) ([]preparedSettingUpdate, error) {
	if len(input) == 0 {
		return nil, errors.New("updates must contain at least one setting module")
	}
	if len(input) > len(settingKeys) {
		return nil, errors.New("updates cannot contain more than four setting modules")
	}
	seen := make(map[string]struct{}, len(input))
	prepared := make([]preparedSettingUpdate, 0, len(input))
	for _, update := range input {
		key := strings.TrimSpace(update.Key)
		if _, ok := settingKeySet[key]; !ok {
			return nil, fmt.Errorf("unsupported setting key %q", key)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("setting key %q appears more than once", key)
		}
		seen[key] = struct{}{}
		if update.ExpectedVersion == nil {
			return nil, fmt.Errorf("expected_version for %q is required", key)
		}
		if *update.ExpectedVersion < 0 {
			return nil, fmt.Errorf("expected_version for %q cannot be negative", key)
		}
		valueJSON, err := normalizeSettingValue(key, update.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s setting: %w", key, err)
		}
		prepared = append(prepared, preparedSettingUpdate{
			key: key, expectedVersion: *update.ExpectedVersion, valueJSON: valueJSON,
		})
	}
	return prepared, nil
}

func applySettingUpdate(tx *gorm.DB, update preparedSettingUpdate, requestID, nowText string) error {
	var current models.AppSetting
	err := tx.First(&current, "key = ?", update.key).Error
	previousVersion := int64(0)
	nextVersion := int64(1)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if update.expectedVersion != 0 {
			return settingsVersionConflict(update.key)
		}
		next := models.AppSetting{
			Key: update.key, ValueJSON: update.valueJSON, SchemaVersion: settingsSchemaVersion,
			Version: nextVersion, UpdatedByActorID: models.BuiltinOwnerActorID, UpdatedAt: nowText,
		}
		if err := tx.Create(&next).Error; err != nil {
			return fmt.Errorf("create app setting %s: %w", update.key, err)
		}
	} else {
		if err != nil {
			return fmt.Errorf("load app setting %s: %w", update.key, err)
		}
		if current.Version != update.expectedVersion {
			return settingsVersionConflict(update.key)
		}
		previousVersion = current.Version
		nextVersion = current.Version + 1
		result := tx.Model(&models.AppSetting{}).
			Where("key = ? AND version = ?", update.key, update.expectedVersion).
			Updates(map[string]any{
				"value_json": update.valueJSON, "schema_version": settingsSchemaVersion,
				"version": nextVersion, "updated_by_actor_id": models.BuiltinOwnerActorID,
				"updated_at": nowText,
			})
		if result.Error != nil {
			return fmt.Errorf("update app setting %s: %w", update.key, result.Error)
		}
		if result.RowsAffected != 1 {
			return settingsVersionConflict(update.key)
		}
	}
	return recordSettingWorkflowEvent(tx, update.key, previousVersion, nextVersion, requestID, nowText)
}

func loadSettingsResponse(db *gorm.DB) (settingsResponse, error) {
	var rows []models.AppSetting
	if err := db.Order("key ASC").Find(&rows).Error; err != nil {
		return settingsResponse{}, err
	}
	byKey := make(map[string]models.AppSetting, len(rows))
	for _, row := range rows {
		if _, ok := settingKeySet[row.Key]; !ok || row.SchemaVersion != settingsSchemaVersion {
			return settingsResponse{}, fmt.Errorf("unsupported stored setting schema for %q", row.Key)
		}
		byKey[row.Key] = row
	}
	items := make([]settingResponse, 0, len(settingKeys))
	for _, key := range settingKeys {
		row, stored := byKey[key]
		valueJSON := defaultSettingValue(key)
		item := settingResponse{Key: key, Value: json.RawMessage(valueJSON), SchemaVersion: settingsSchemaVersion}
		if stored {
			normalized, err := normalizeSettingValue(key, json.RawMessage(row.ValueJSON))
			if err != nil {
				return settingsResponse{}, fmt.Errorf("decode stored setting %s: %w", key, err)
			}
			updatedBy := row.UpdatedByActorID
			updatedAt := normalizeTimestamp(row.UpdatedAt)
			item.Value = json.RawMessage(normalized)
			item.Version = row.Version
			item.Stored = true
			item.UpdatedByActorID = &updatedBy
			item.UpdatedAt = &updatedAt
		}
		items = append(items, item)
	}
	return settingsResponse{SchemaVersion: settingsSchemaVersion, Items: items}, nil
}

func normalizeSettingValue(key string, raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", errors.New("value must be a JSON object")
	}
	switch key {
	case "workspace":
		var value workspaceSettingValue
		if err := decodeSettingObject(trimmed, &value); err != nil {
			return "", err
		}
		if err := requireSettingFields(trimmed, "display_name", "avatar_ref"); err != nil {
			return "", err
		}
		if err := requireNonNullSettingFields(trimmed, "display_name"); err != nil {
			return "", err
		}
		value.DisplayName = strings.Join(strings.Fields(value.DisplayName), " ")
		if utf8.RuneCountInString(value.DisplayName) < 1 || utf8.RuneCountInString(value.DisplayName) > 32 {
			return "", errors.New("display_name must contain 1 to 32 characters")
		}
		if value.AvatarRef != nil {
			avatarRef := strings.TrimSpace(*value.AvatarRef)
			if !controlledAvatar.MatchString(avatarRef) {
				return "", errors.New("avatar_ref must be a controlled avatars path or null")
			}
			value.AvatarRef = &avatarRef
		}
		return marshalSettingObject(value)
	case "general":
		var value generalSettingValue
		if err := decodeSettingObject(trimmed, &value); err != nil {
			return "", err
		}
		if err := requireSettingFields(trimmed, "default_route", "show_right_overview", "reduce_motion"); err != nil {
			return "", err
		}
		if err := requireNonNullSettingFields(trimmed, "default_route", "show_right_overview", "reduce_motion"); err != nil {
			return "", err
		}
		switch value.DefaultRoute {
		case "today", "tasks", "projects", "clients", "focus":
		default:
			return "", errors.New("default_route is unsupported")
		}
		return marshalSettingObject(value)
	case "appearance":
		var value appearanceSettingValue
		if err := decodeSettingObject(trimmed, &value); err != nil {
			return "", err
		}
		if err := requireSettingFields(trimmed, "theme"); err != nil {
			return "", err
		}
		if err := requireNonNullSettingFields(trimmed, "theme"); err != nil {
			return "", err
		}
		if value.Theme != "light" && value.Theme != "dark" {
			return "", errors.New("theme must be light or dark")
		}
		return marshalSettingObject(value)
	case "focus":
		var value focusSettingValue
		if err := decodeSettingObject(trimmed, &value); err != nil {
			return "", err
		}
		if err := requireSettingFields(trimmed, "focus_minutes", "break_minutes", "cycles", "auto_start_break", "auto_start_focus", "sound_enabled"); err != nil {
			return "", err
		}
		if err := requireNonNullSettingFields(trimmed, "focus_minutes", "break_minutes", "cycles", "auto_start_break", "auto_start_focus", "sound_enabled"); err != nil {
			return "", err
		}
		if value.FocusMinutes < 5 || value.FocusMinutes > 120 || value.FocusMinutes%5 != 0 {
			return "", errors.New("focus_minutes must be 5 to 120 in steps of 5")
		}
		if value.BreakMinutes < 5 || value.BreakMinutes > 30 || value.BreakMinutes%5 != 0 {
			return "", errors.New("break_minutes must be 5 to 30 in steps of 5")
		}
		if value.Cycles < 1 || value.Cycles > 8 {
			return "", errors.New("cycles must be 1 to 8")
		}
		return marshalSettingObject(value)
	default:
		return "", errors.New("unsupported setting key")
	}
}

func decodeSettingObject(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("value must contain one JSON object")
	}
	return nil
}

func requireSettingFields(raw []byte, fields ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("%s is required", field)
		}
	}
	return nil
}

func requireNonNullSettingFields(raw []byte, fields ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(object[field]), []byte("null")) {
			return fmt.Errorf("%s cannot be null", field)
		}
	}
	return nil
}

func marshalSettingObject(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode normalized setting: %w", err)
	}
	return string(encoded), nil
}

func defaultSettingValue(key string) string {
	var value any
	switch key {
	case "workspace":
		value = workspaceSettingValue{DisplayName: "opc-workspace", AvatarRef: nil}
	case "general":
		value = generalSettingValue{DefaultRoute: "today", ShowRightOverview: true, ReduceMotion: false}
	case "appearance":
		value = appearanceSettingValue{Theme: "dark"}
	case "focus":
		value = focusSettingValue{FocusMinutes: 50, BreakMinutes: 5, Cycles: 4, AutoStartBreak: true, AutoStartFocus: false, SoundEnabled: true}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func settingsVersionConflict(key string) error {
	return newProjectRequestError(http.StatusConflict, "SETTINGS_VERSION_CONFLICT", fmt.Sprintf("Setting %s has changed; reload it before retrying", key))
}

func recordSettingWorkflowEvent(tx *gorm.DB, key string, previousVersion, currentVersion int64, requestID, createdAt string) error {
	previous, err := json.Marshal(map[string]any{"stored": previousVersion > 0, "version": previousVersion, "schema_version": settingsSchemaVersion})
	if err != nil {
		return err
	}
	current, err := json.Marshal(map[string]any{"stored": true, "version": currentVersion, "schema_version": settingsSchemaVersion})
	if err != nil {
		return err
	}
	commandSeq := int(currentVersion)
	actorID := models.BuiltinOwnerActorID
	var requestIDPointer *string
	if requestID != "" {
		requestIDPointer = &requestID
	}
	previousJSON := string(previous)
	currentJSON := string(current)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "setting", AggregateID: key,
		Action: "settings_updated", ActorID: &actorID,
		RequestID: requestIDPointer, CommandSeq: &commandSeq,
		PreviousJSON: &previousJSON, CurrentJSON: &currentJSON, CreatedAt: createdAt,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record setting workflow event: %w", err)
	}
	return nil
}
