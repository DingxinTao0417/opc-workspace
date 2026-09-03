package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	createAIProviderEndpoint = "POST /api/v1/ai/providers"
	aiProviderKeyService     = "opc-workspace-ai"

	aiProviderKindRemote = "remote"
	aiProviderKindLocal  = "local"
)

func aiProviderKeyAccount(providerID string) string {
	return "ai:" + providerID + ":api_key"
}

type aiProviderResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	Protocol        string  `json:"protocol"`
	BaseURL         string  `json:"base_url"`
	Model           string  `json:"model"`
	Status          string  `json:"status"`
	HealthStatus    string  `json:"health_status"`
	HealthErrorCode *string `json:"health_error_code"`
	HasKey          bool    `json:"has_key"`
	LastHealthAt    *string `json:"last_health_at"`
	Version         int64   `json:"version"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type createAIProviderRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
}

type patchAIProviderRequest struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Protocol *string `json:"protocol"`
	BaseURL  *string `json:"base_url"`
	Model    *string `json:"model"`
}

type setAIProviderKeyRequest struct {
	APIKey string `json:"api_key"`
}

func (a *API) listAIProviders(c *gin.Context) {
	var rows []models.AIProvider
	if err := a.db.WithContext(c.Request.Context()).Order("name, id").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]aiProviderResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, aiProviderResponseFromModel(row))
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) createAIProvider(c *gin.Context) {
	var input createAIProviderRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	name, kind, protocol, baseURL, model, code, ok := normalizeAIProviderFields(input.Name, input.Kind, input.Protocol, input.BaseURL, input.Model)
	if !ok {
		writeError(c, http.StatusUnprocessableEntity, code, "The AI provider fields are not valid")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	statusCode, replayed := http.StatusCreated, false
	var response aiProviderResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var replayStatus int
		var err error
		replayed, replayStatus, err = replayTaskOutputCommand(tx, idempotencyKey, createAIProviderEndpoint, requestHash, &response)
		if err != nil {
			return err
		}
		if replayed {
			statusCode = replayStatus
			return nil
		}
		var count int64
		if err := tx.Model(&models.AIProvider{}).Where("name = ?", name).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return newProjectRequestError(http.StatusConflict, "AI_PROVIDER_NAME_TAKEN", "An AI provider with this name is already registered")
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		row := models.AIProvider{
			ID: uuid.NewString(), Name: name, Kind: kind, Protocol: protocol, BaseURL: baseURL, Model: model,
			Status: "unconfigured", HealthStatus: "unknown", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create AI provider: %w", err)
		}
		response = aiProviderResponseFromModel(row)
		if err := recordAIProviderWorkflowEvent(tx, "ai_adapter_registered", row.ID, nil, response, requestIDFromContext(c), now); err != nil {
			return err
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, createAIProviderEndpoint, row.ID, requestHash, http.StatusCreated, response, now)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	setProjectETag(c, response.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) getAIProvider(c *gin.Context) {
	id, ok := aiProviderID(c)
	if !ok {
		return
	}
	row, err := loadAIProvider(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAIProviderLoadError(c, err)
		return
	}
	response := aiProviderResponseFromModel(row)
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) patchAIProvider(c *gin.Context) {
	id, ok := aiProviderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input patchAIProviderRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	row, err := loadAIProvider(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAIProviderLoadError(c, err)
		return
	}
	if row.Version != expectedVersion {
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	if input.Name != nil {
		row.Name = *input.Name
	}
	if input.Kind != nil {
		row.Kind = *input.Kind
	}
	if input.Protocol != nil {
		row.Protocol = *input.Protocol
	}
	if input.BaseURL != nil {
		row.BaseURL = *input.BaseURL
	}
	if input.Model != nil {
		row.Model = *input.Model
	}
	name, kind, protocol, baseURL, model, code, valid := normalizeAIProviderFields(row.Name, row.Kind, row.Protocol, row.BaseURL, row.Model)
	if !valid {
		writeError(c, http.StatusUnprocessableEntity, code, "The patched AI provider fields are not valid")
		return
	}
	var response aiProviderResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.AIProvider{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"name": name, "kind": kind, "protocol": protocol, "base_url": baseURL, "model": model,
			"version": gorm.Expr("version + 1"), "updated_at": a.options.Now().UTC().Format(time.RFC3339Nano),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		row, err := loadAIProvider(tx, id)
		if err != nil {
			return err
		}
		response = aiProviderResponseFromModel(row)
		return nil
	})
	if err != nil {
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) deleteAIProvider(c *gin.Context) {
	id, ok := aiProviderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	var response aiProviderResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadAIProvider(tx, id)
		if err != nil {
			return err
		}
		if row.Version != expectedVersion {
			return taskVersionConflict()
		}
		previous := aiProviderResponseFromModel(row)
		if err := tx.Delete(&row).Error; err != nil {
			return fmt.Errorf("delete AI provider: %w", err)
		}
		if err := recordAIProviderWorkflowEvent(tx, "ai_adapter_removed", id, &previous, aiProviderResponse{}, requestIDFromContext(c), now); err != nil {
			return err
		}
		response = previous
		return nil
	})
	if err != nil {
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if err := a.keyStore.Delete(aiProviderKeyService, aiProviderKeyAccount(id)); err != nil && !errors.Is(err, keystore.ErrNotFound) {
		a.options.Logger.Print("AI provider key cleanup failed for provider " + id)
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) checkAIProviderHealth(c *gin.Context) {
	id, ok := aiProviderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	row, err := loadAIProvider(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAIProviderLoadError(c, err)
		return
	}
	if row.Version != expectedVersion {
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	healthCode := ""
	switch {
	case row.Kind == aiProviderKindLocal:
		// Local servers need no key; only endpoint reachability matters.
		statusCode, probeErr := modelclient.HealthCheck(c.Request.Context(), modelclient.Protocol(row.Protocol), row.BaseURL, "", nil)
		switch {
		case probeErr != nil:
			healthCode = "AI_ENDPOINT_UNREACHABLE"
		case statusCode < 200 || statusCode > 299:
			healthCode = "AI_PROVIDER_ERROR"
		}
	case row.HasKey:
		apiKey, keyErr := a.keyStore.Get(aiProviderKeyService, aiProviderKeyAccount(id))
		if keyErr != nil && !errors.Is(keyErr, keystore.ErrNotFound) {
			writeError(c, http.StatusServiceUnavailable, "AI_KEY_STORE_UNAVAILABLE", "The operating system credential store is not available")
			return
		}
		if keyErr != nil {
			healthCode = "AI_KEY_UNAVAILABLE"
		} else {
			statusCode, probeErr := modelclient.HealthCheck(c.Request.Context(), modelclient.Protocol(row.Protocol), row.BaseURL, apiKey, nil)
			switch {
			case probeErr != nil:
				healthCode = "AI_ENDPOINT_UNREACHABLE"
			case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
				healthCode = "AI_KEY_INVALID"
			case statusCode < 200 || statusCode > 299:
				healthCode = "AI_PROVIDER_ERROR"
			}
		}
	default:
		healthCode = "AI_KEY_UNAVAILABLE"
	}
	var response aiProviderResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadAIProvider(tx, id)
		if err != nil {
			return err
		}
		if row.Version != expectedVersion {
			return taskVersionConflict()
		}
		previous := aiProviderResponseFromModel(row)
		status, healthStatus := "ready", "healthy"
		if healthCode != "" {
			status, healthStatus = "unavailable", "unhealthy"
		}
		result := tx.Model(&models.AIProvider{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"status": status, "health_status": healthStatus, "health_error_code": aiNullableString(healthCode),
			"last_health_at": now, "version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		row, err = loadAIProvider(tx, id)
		if err != nil {
			return err
		}
		response = aiProviderResponseFromModel(row)
		return recordAIProviderWorkflowEvent(tx, "ai_adapter_health_checked", id, &previous, response, requestIDFromContext(c), now)
	})
	if err != nil {
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) setAIProviderKey(c *gin.Context) {
	id, ok := aiProviderID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input setAIProviderKeyRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" || len(apiKey) > 200 {
		writeError(c, http.StatusUnprocessableEntity, "AI_KEY_MALFORMED", "The API key must be between 1 and 200 characters")
		return
	}
	row, err := loadAIProvider(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAIProviderLoadError(c, err)
		return
	}
	if row.Kind == aiProviderKindLocal {
		writeError(c, http.StatusConflict, "AI_KEY_NOT_ALLOWED", "Local AI providers never store an API key")
		return
	}
	if row.Version != expectedVersion {
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	account := aiProviderKeyAccount(id)
	if err := a.keyStore.Set(aiProviderKeyService, account, apiKey); err != nil {
		writeError(c, http.StatusServiceUnavailable, "AI_KEY_STORE_UNAVAILABLE", "The operating system credential store is not available")
		return
	}
	var response aiProviderResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.AIProvider{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"has_key": true, "version": gorm.Expr("version + 1"), "updated_at": a.options.Now().UTC().Format(time.RFC3339Nano),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		row, err := loadAIProvider(tx, id)
		if err != nil {
			return err
		}
		response = aiProviderResponseFromModel(row)
		return nil
	})
	if err != nil {
		_ = a.keyStore.Delete(aiProviderKeyService, account)
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

// normalizeAIProviderFields validates the provider identity fields. Local
// providers (kind=local) are OpenAI-compatible servers on the loopback
// interface: http only, no API key, openai_chat protocol only (ADR-005).
func normalizeAIProviderFields(name, kind, protocol, baseURL, model string) (string, string, string, string, string, string, bool) {
	name = strings.TrimSpace(name)
	kind = strings.TrimSpace(kind)
	protocol = strings.TrimSpace(protocol)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	if name == "" || len(name) > 100 {
		return "", "", "", "", "", "AI_PROVIDER_NAME_INVALID", false
	}
	if kind == "" {
		kind = aiProviderKindRemote
	}
	if kind != aiProviderKindRemote && kind != aiProviderKindLocal {
		return "", "", "", "", "", "AI_PROVIDER_KIND_INVALID", false
	}
	if protocol != string(modelclient.ProtocolOpenAIChat) && protocol != string(modelclient.ProtocolAnthropicMessages) {
		return "", "", "", "", "", "AI_PROTOCOL_INVALID", false
	}
	if kind == aiProviderKindLocal && protocol != string(modelclient.ProtocolOpenAIChat) {
		return "", "", "", "", "", "AI_PROTOCOL_INVALID", false
	}
	parsed, err := url.Parse(baseURL)
	if baseURL == "" || len(baseURL) > 500 || err != nil || parsed.Host == "" ||
		(!strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://127.0.0.1") && !strings.HasPrefix(baseURL, "http://localhost")) {
		return "", "", "", "", "", "AI_ENDPOINT_INVALID", false
	}
	if kind == aiProviderKindLocal &&
		(!strings.HasPrefix(baseURL, "http://127.0.0.1") && !strings.HasPrefix(baseURL, "http://localhost")) {
		return "", "", "", "", "", "AI_ENDPOINT_INVALID", false
	}
	if model == "" || len(model) > 200 {
		return "", "", "", "", "", "AI_MODEL_INVALID", false
	}
	return name, kind, protocol, baseURL, model, "", true
}

func aiProviderResponseFromModel(row models.AIProvider) aiProviderResponse {
	return aiProviderResponse{
		ID: row.ID, Name: row.Name, Kind: row.Kind, Protocol: row.Protocol, BaseURL: row.BaseURL, Model: row.Model,
		Status: row.Status, HealthStatus: row.HealthStatus, HealthErrorCode: row.HealthErrorCode,
		HasKey: row.HasKey, LastHealthAt: row.LastHealthAt, Version: row.Version,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

func aiProviderID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_PROVIDER_ID", "AI provider id must be a UUID")
		return "", false
	}
	return id, true
}

func loadAIProvider(db *gorm.DB, id string) (models.AIProvider, error) {
	var row models.AIProvider
	err := db.Where("id = ?", id).First(&row).Error
	return row, err
}

func writeAIProviderLoadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", "AI provider not found")
		return
	}
	writeDatabaseError(c)
}

func writeAIProviderRequestError(c *gin.Context, err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", "AI provider not found")
		return true
	}
	return writeProjectRequestError(c, err)
}

func recordAIProviderWorkflowEvent(tx *gorm.DB, action, providerID string, previous *aiProviderResponse, current aiProviderResponse, requestID, createdAt string) error {
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return err
	}
	var previousJSON any
	if previous != nil {
		previousBytes, err := json.Marshal(*previous)
		if err != nil {
			return err
		}
		previousJSON = string(previousBytes)
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_provider", "aggregate_id": providerID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": requestID,
		"previous_json": previousJSON, "current_json": string(currentBytes), "created_at": createdAt,
	}).Error
}

func aiNullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
