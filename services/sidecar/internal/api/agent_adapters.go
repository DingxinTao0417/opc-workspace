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

const (
	createAgentAdapterEndpoint       = "POST /api/v1/agent-adapters"
	agentAdapterProtocolVersion      = "opc-agent-pipe-v1"
	agentAdapterIsolationBlockedCode = "PLATFORM_ISOLATION_UNVERIFIED"
)

type agentAdapterManifest struct {
	ExecutionMode string   `json:"execution_mode"`
	Capabilities  []string `json:"capabilities"`
	Requirements  []string `json:"requirements"`
}

type agentAdapterPreset struct {
	ID, Key, DisplayName, ExecutableRef string
	Manifest                            agentAdapterManifest
}

var agentAdapterPresets = []agentAdapterPreset{{
	ID: "018f0000-0000-5000-8000-000000003401", Key: "builtin-local-text-v1",
	DisplayName: "本地文本诊断执行器", ExecutableRef: "builtin:local-text-v1",
	Manifest: agentAdapterManifest{
		ExecutionMode: "short_lived_process",
		Capabilities:  []string{"read_task_snapshot", "write_text_artifact", "write_structured_artifact"},
		Requirements:  []string{"process_isolation", "network_block", "process_tree_cleanup"},
	},
}}

type createAgentAdapterRequest struct {
	PresetKey string `json:"preset_key"`
}

type agentAdapterReadiness struct {
	CanEnable       bool     `json:"can_enable"`
	UnavailableCode string   `json:"unavailable_code,omitempty"`
	RequiredGates   []string `json:"required_gates"`
}

type agentAdapterResponse struct {
	ID              string                `json:"id"`
	AdapterKey      string                `json:"adapter_key"`
	Kind            string                `json:"kind"`
	DisplayName     string                `json:"display_name"`
	ProtocolVersion string                `json:"protocol_version"`
	Manifest        agentAdapterManifest  `json:"manifest"`
	Status          string                `json:"status"`
	HealthStatus    string                `json:"health_status"`
	HealthErrorCode *string               `json:"health_error_code"`
	IsolationStatus string                `json:"isolation_status"`
	ExecutionReady  bool                  `json:"execution_ready"`
	LastHealthAt    *string               `json:"last_health_at"`
	Readiness       agentAdapterReadiness `json:"readiness"`
	Version         int64                 `json:"version"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
}

func (a *API) listAgentAdapters(c *gin.Context) {
	var rows []models.AgentAdapter
	if err := a.db.WithContext(c.Request.Context()).Order("display_name, id").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]agentAdapterResponse, 0, len(rows))
	for _, row := range rows {
		response, err := agentAdapterResponseFromModel(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		responses = append(responses, response)
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) createAgentAdapter(c *gin.Context) {
	var input createAgentAdapterRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	preset, exists := agentAdapterPresetByKey(strings.TrimSpace(input.PresetKey))
	if !exists {
		writeError(c, http.StatusUnprocessableEntity, "AGENT_ADAPTER_PRESET_INVALID", "The Agent Adapter preset is not supported")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	statusCode, replayed := http.StatusCreated, false
	var response agentAdapterResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayStatus := 0
		var err error
		replayed, replayStatus, err = replayTaskOutputCommand(tx, idempotencyKey, createAgentAdapterEndpoint, requestHash, &response)
		if err != nil || replayed {
			if replayed {
				statusCode = replayStatus
			}
			return err
		}
		var count int64
		if err := tx.Model(&models.AgentAdapter{}).Where("adapter_key = ?", preset.Key).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return newProjectRequestError(http.StatusConflict, "AGENT_ADAPTER_ALREADY_REGISTERED", "The Agent Adapter preset is already registered")
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		manifest, err := json.Marshal(preset.Manifest)
		if err != nil {
			return err
		}
		row := models.AgentAdapter{
			ID: preset.ID, AdapterKey: preset.Key, Kind: "builtin", DisplayName: preset.DisplayName,
			ExecutableRef: preset.ExecutableRef, ManifestJSON: string(manifest), ProtocolVersion: agentAdapterProtocolVersion,
			Status: "disabled", HealthStatus: "unknown", IsolationStatus: "unverified", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create Agent Adapter: %w", err)
		}
		response, err = agentAdapterResponseFromModel(row)
		if err != nil {
			return err
		}
		if err := recordAgentAdapterWorkflowEvent(tx, "agent_adapter_registered", row.ID, nil, response, requestIDFromContext(c), now); err != nil {
			return err
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, createAgentAdapterEndpoint, row.ID, requestHash, http.StatusCreated, response, now)
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

func (a *API) getAgentAdapter(c *gin.Context) {
	id, ok := agentAdapterID(c)
	if !ok {
		return
	}
	row, err := loadAgentAdapter(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAgentAdapterLoadError(c, err)
		return
	}
	response, err := agentAdapterResponseFromModel(row)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) checkAgentAdapter(c *gin.Context) {
	id, ok := agentAdapterID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var response agentAdapterResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadAgentAdapter(tx, id)
		if err != nil {
			return err
		}
		if row.Version != expectedVersion {
			return taskVersionConflict()
		}
		previous, err := agentAdapterResponseFromModel(row)
		if err != nil {
			return err
		}
		if err := verifyAgentAdapterIdentity(row); err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.AgentAdapter{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"health_status": "blocked", "health_error_code": agentAdapterIsolationBlockedCode,
			"isolation_status": "unverified", "execution_ready": false, "last_health_at": now, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		row, err = loadAgentAdapter(tx, id)
		if err != nil {
			return err
		}
		response, err = agentAdapterResponseFromModel(row)
		if err != nil {
			return err
		}
		return recordAgentAdapterWorkflowEvent(tx, "agent_adapter_health_checked", id, &previous, response, requestIDFromContext(c), now)
	})
	if err != nil {
		if writeAgentAdapterRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) enableAgentAdapter(c *gin.Context) {
	id, ok := agentAdapterID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	row, err := loadAgentAdapter(a.db.WithContext(c.Request.Context()), id)
	if err != nil {
		writeAgentAdapterLoadError(c, err)
		return
	}
	if row.Version != expectedVersion {
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	writeError(c, http.StatusConflict, "AGENT_ADAPTER_NOT_EXECUTION_READY", "Platform isolation, network blocking, and process-tree cleanup must be verified before this Agent Adapter can be enabled")
}

func (a *API) disableAgentAdapter(c *gin.Context) {
	id, ok := agentAdapterID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var response agentAdapterResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		row, err := loadAgentAdapter(tx, id)
		if err != nil {
			return err
		}
		if row.Version != expectedVersion {
			return taskVersionConflict()
		}
		if row.Status == "disabled" {
			response, err = agentAdapterResponseFromModel(row)
			return err
		}
		previous, err := agentAdapterResponseFromModel(row)
		if err != nil {
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		result := tx.Model(&models.AgentAdapter{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(map[string]any{
			"status": "disabled", "version": gorm.Expr("version + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return taskVersionConflict()
		}
		row, err = loadAgentAdapter(tx, id)
		if err != nil {
			return err
		}
		response, err = agentAdapterResponseFromModel(row)
		if err != nil {
			return err
		}
		return recordAgentAdapterWorkflowEvent(tx, "agent_adapter_disabled", id, &previous, response, requestIDFromContext(c), now)
	})
	if err != nil {
		if writeAgentAdapterRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, response.Version)
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func agentAdapterPresetByKey(key string) (agentAdapterPreset, bool) {
	for _, preset := range agentAdapterPresets {
		if preset.Key == key {
			return preset, true
		}
	}
	return agentAdapterPreset{}, false
}

func agentAdapterResponseFromModel(row models.AgentAdapter) (agentAdapterResponse, error) {
	var manifest agentAdapterManifest
	if err := json.Unmarshal([]byte(row.ManifestJSON), &manifest); err != nil {
		return agentAdapterResponse{}, fmt.Errorf("decode Agent Adapter manifest: %w", err)
	}
	unavailableCode := ""
	if !row.ExecutionReady {
		unavailableCode = agentAdapterIsolationBlockedCode
	}
	return agentAdapterResponse{
		ID: row.ID, AdapterKey: row.AdapterKey, Kind: row.Kind, DisplayName: row.DisplayName,
		ProtocolVersion: row.ProtocolVersion, Manifest: manifest, Status: row.Status,
		HealthStatus: row.HealthStatus, HealthErrorCode: row.HealthErrorCode, IsolationStatus: row.IsolationStatus,
		ExecutionReady: row.ExecutionReady, LastHealthAt: row.LastHealthAt,
		Readiness: agentAdapterReadiness{CanEnable: row.ExecutionReady, UnavailableCode: unavailableCode, RequiredGates: append([]string(nil), manifest.Requirements...)},
		Version:   row.Version, CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}, nil
}

func verifyAgentAdapterIdentity(row models.AgentAdapter) error {
	preset, exists := agentAdapterPresetByKey(row.AdapterKey)
	if !exists {
		return newProjectRequestError(http.StatusConflict, "AGENT_ADAPTER_MANIFEST_INVALID", "The Agent Adapter manifest is not recognized")
	}
	manifest, err := json.Marshal(preset.Manifest)
	if err != nil {
		return err
	}
	if row.ID != preset.ID || row.Kind != "builtin" || row.DisplayName != preset.DisplayName || row.ExecutableRef != preset.ExecutableRef ||
		row.ManifestJSON != string(manifest) || row.ProtocolVersion != agentAdapterProtocolVersion {
		return newProjectRequestError(http.StatusConflict, "AGENT_ADAPTER_MANIFEST_INVALID", "The Agent Adapter manifest does not match the code-owned preset")
	}
	return nil
}

func agentAdapterID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AGENT_ADAPTER_ID", "Agent Adapter id must be a UUID")
		return "", false
	}
	return id, true
}

func loadAgentAdapter(db *gorm.DB, id string) (models.AgentAdapter, error) {
	var row models.AgentAdapter
	err := db.Where("id = ?", id).First(&row).Error
	return row, err
}

func writeAgentAdapterLoadError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AGENT_ADAPTER_NOT_FOUND", "Agent Adapter not found")
		return
	}
	writeDatabaseError(c)
}

func writeAgentAdapterRequestError(c *gin.Context, err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AGENT_ADAPTER_NOT_FOUND", "Agent Adapter not found")
		return true
	}
	return writeProjectRequestError(c, err)
}

func recordAgentAdapterWorkflowEvent(tx *gorm.DB, action, adapterID string, previous *agentAdapterResponse, current agentAdapterResponse, requestID, createdAt string) error {
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return err
	}
	var previousJSON any
	if previous != nil {
		previousBytes, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		previousJSON = string(previousBytes)
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "agent_adapter", "aggregate_id": adapterID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": requestID,
		"previous_json": previousJSON, "current_json": string(currentBytes), "created_at": createdAt,
	}).Error
}
