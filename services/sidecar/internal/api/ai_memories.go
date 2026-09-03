package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	createAIMemoryEndpoint = "POST /api/v1/ai/memories"
	// aiMemoryBudget caps how much confirmed memory text rides in one prompt.
	aiMemoryBudget = 8 << 10
	// aiMemoryMaxCount caps how many memory notes ride in one prompt.
	aiMemoryMaxCount = 20
)

type aiMemoryResponse struct {
	ID              string  `json:"id"`
	Content         string  `json:"content"`
	SourceMessageID *string `json:"source_message_id"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type createAIMemoryRequest struct {
	Content         string  `json:"content"`
	SourceMessageID *string `json:"source_message_id"`
}

// listAIMemories serves the user-confirmed long-term preferences (newest
// first) for the settings management surface.
func (a *API) listAIMemories(c *gin.Context) {
	var rows []models.AIMemory
	if err := a.db.WithContext(c.Request.Context()).Order("updated_at DESC, id").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]aiMemoryResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, aiMemoryResponseFromModel(row))
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) createAIMemory(c *gin.Context) {
	var input createAIMemoryRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	content := strings.TrimSpace(input.Content)
	if content == "" || len(content) > 500 {
		writeError(c, http.StatusUnprocessableEntity, "AI_MEMORY_CONTENT_INVALID", "Memory content must be between 1 and 500 characters")
		return
	}
	var sourceMessageID *string
	if trimmed := strings.TrimSpace(valueOrEmpty(input.SourceMessageID)); trimmed != "" {
		if _, err := uuid.Parse(trimmed); err != nil {
			writeError(c, http.StatusUnprocessableEntity, "INVALID_AI_MESSAGE_ID", "AI message id must be a UUID")
			return
		}
		var count int64
		if err := a.db.WithContext(c.Request.Context()).Model(&models.AIMessage{}).Where("id = ?", trimmed).Count(&count).Error; err != nil {
			writeDatabaseError(c)
			return
		}
		if count == 0 {
			writeError(c, http.StatusNotFound, "AI_MESSAGE_NOT_FOUND", "AI message not found")
			return
		}
		sourceMessageID = &trimmed
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	statusCode, replayed := http.StatusCreated, false
	var response aiMemoryResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var replayStatus int
		var err error
		replayed, replayStatus, err = replayTaskOutputCommand(tx, idempotencyKey, createAIMemoryEndpoint, requestHash, &response)
		if err != nil {
			return err
		}
		if replayed {
			statusCode = replayStatus
			return nil
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		row := models.AIMemory{
			ID: uuid.NewString(), Content: content, SourceMessageID: sourceMessageID,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		response = aiMemoryResponseFromModel(row)
		if err := recordAIMemoryEvent(tx, "ai_memory_created", row.ID, requestIDFromContext(c), now); err != nil {
			return err
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, createAIMemoryEndpoint, row.ID, requestHash, http.StatusCreated, response, now)
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
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) deleteAIMemory(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_MEMORY_ID", "AI memory id must be a UUID")
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ?", id).Delete(&models.AIMemory{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return recordAIMemoryEvent(tx, "ai_memory_deleted", id, requestIDFromContext(c), now)
	})
	if err != nil {
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
}

// recordAIMemoryEvent writes a sanitized workflow event; memory content
// never enters the event log (only its length, mirroring the AI privacy
// boundary).
func recordAIMemoryEvent(tx *gorm.DB, action, memoryID, requestID, createdAt string) error {
	current, err := json.Marshal(map[string]any{"id": memoryID})
	if err != nil {
		return err
	}
	var requestIDValue any
	if requestID != "" {
		requestIDValue = requestID
	}
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_memory", "aggregate_id": memoryID,
		"action": action, "actor_id": models.BuiltinOwnerActorID, "request_id": requestIDValue,
		"previous_json": nil, "current_json": string(current), "created_at": createdAt,
	}).Error
}

func aiMemoryResponseFromModel(row models.AIMemory) aiMemoryResponse {
	return aiMemoryResponse{
		ID: row.ID, Content: row.Content, SourceMessageID: row.SourceMessageID,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

// confirmedAIMemories loads the most recent user-confirmed memory notes for
// prompt injection, trimmed to the count and byte budgets (ADR-006). Notes
// larger than the remaining budget are skipped so one oversized note cannot
// starve smaller, newer facts.
func (a *API) confirmedAIMemories() []string {
	var rows []models.AIMemory
	if err := a.db.Order("updated_at DESC, id").Limit(aiMemoryMaxCount).Find(&rows).Error; err != nil {
		return nil
	}
	notes := make([]string, 0, len(rows))
	budget := aiMemoryBudget
	for _, row := range rows {
		if len(row.Content) > budget {
			continue
		}
		budget -= len(row.Content)
		notes = append(notes, row.Content)
	}
	return notes
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
