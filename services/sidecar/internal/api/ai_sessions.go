package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxAIMessagePageLimit = 200
	// defaultAISessionTitle marks sessions the user created without naming;
	// the first user message renames them for the session rail.
	defaultAISessionTitle = "新会话"
)

type aiSessionResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Persist   bool   `json:"persist"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type aiMessageResponse struct {
	ID                string  `json:"id"`
	SessionID         string  `json:"session_id"`
	Role              string  `json:"role"`
	Status            string  `json:"status"`
	Content           string  `json:"content"`
	Reasoning         *string `json:"reasoning"`
	TaskID            *string `json:"task_id"`
	TaskTitleSnapshot *string `json:"task_title_snapshot"`
	CreatedAt         string  `json:"created_at"`
}

type createAISessionRequest struct {
	Title   string `json:"title"`
	Persist *bool  `json:"persist"`
}

type aiMessagePageMeta struct {
	HasMore         bool    `json:"has_more"`
	OldestCreatedAt *string `json:"oldest_created_at,omitempty"`
	OldestID        *string `json:"oldest_id,omitempty"`
}

func (a *API) listAISessions(c *gin.Context) {
	var rows []models.AISession
	if err := a.db.WithContext(c.Request.Context()).Order("updated_at DESC, id ASC").Limit(200).Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]aiSessionResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, aiSessionResponseFromModel(row))
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) createAISession(c *gin.Context) {
	var input createAISessionRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = defaultAISessionTitle
	}
	if len([]rune(title)) > 200 {
		writeError(c, http.StatusUnprocessableEntity, "AI_SESSION_TITLE_INVALID", "The session title must be at most 200 characters")
		return
	}
	persist := true
	if input.Persist != nil {
		persist = *input.Persist
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	row := models.AISession{
		ID: uuid.NewString(), Title: title, Persist: persist, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	setProjectETag(c, row.Version)
	c.JSON(http.StatusCreated, gin.H{"data": aiSessionResponseFromModel(row)})
}

func (a *API) getAISession(c *gin.Context) {
	row, ok := a.loadAISession(c)
	if !ok {
		return
	}
	setProjectETag(c, row.Version)
	c.JSON(http.StatusOK, gin.H{"data": aiSessionResponseFromModel(row)})
}

func (a *API) deleteAISession(c *gin.Context) {
	id, ok := aiSessionID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var current models.AISession
	if err := a.db.WithContext(c.Request.Context()).Where("id = ?", id).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	if current.Version != expectedVersion {
		writeProjectRequestError(c, taskVersionConflict())
		return
	}
	a.aiGenerations.cancelSession(id)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var row models.AISession
		if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
			return err
		}
		if row.Version != expectedVersion {
			return taskVersionConflict()
		}
		if err := tx.Where("session_id = ?", id).Delete(&models.AIMessage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", id).Delete(&models.AIGeneration{}).Error; err != nil {
			return err
		}
		return tx.Delete(&row).Error
	})
	if err != nil {
		if writeAIProviderRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id, "deleted": true}})
}

func (a *API) listAIMessages(c *gin.Context) {
	id, ok := aiSessionID(c)
	if !ok {
		return
	}
	limit, ok := queryInt(c, "limit", 50, 1, maxAIMessagePageLimit)
	if !ok {
		return
	}
	beforeCreated := strings.TrimSpace(c.Query("before_created_at"))
	beforeID := strings.TrimSpace(c.Query("before_id"))
	if (beforeCreated == "") != (beforeID == "") {
		writeError(c, http.StatusBadRequest, "AI_MESSAGE_CURSOR_INVALID", "The message cursor requires both created_at and id")
		return
	}
	if beforeID != "" {
		if _, err := uuid.Parse(beforeID); err != nil {
			writeError(c, http.StatusBadRequest, "AI_MESSAGE_CURSOR_INVALID", "The message cursor id must be a UUID")
			return
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, beforeCreated); beforeCreated != "" && err != nil {
		writeError(c, http.StatusBadRequest, "AI_MESSAGE_CURSOR_INVALID", "The message cursor created_at must be RFC3339Nano")
		return
	}
	messages, hasMore, err := aiSessionMessagePage(a.db.WithContext(c.Request.Context()), id, limit, beforeCreated, beforeID)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	meta := aiMessagePageMeta{HasMore: hasMore}
	if hasMore && len(messages) > 0 {
		oldest := messages[0]
		meta.OldestCreatedAt = &oldest.CreatedAt
		meta.OldestID = &oldest.ID
	}
	c.JSON(http.StatusOK, gin.H{"data": aiMessageResponsesFromModels(messages), "meta": meta})
}

func aiSessionMessagePage(db *gorm.DB, sessionID string, limit int, beforeCreated, beforeID string) ([]models.AIMessage, bool, error) {
	query := db.Model(&models.AIMessage{}).Where("session_id = ?", sessionID)
	if beforeCreated != "" {
		query = query.Where("(created_at < ?) OR (created_at = ? AND id < ?)", beforeCreated, beforeCreated, beforeID)
	}
	var rows []models.AIMessage
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, hasMore, nil
}

func (a *API) loadAISession(c *gin.Context) (models.AISession, bool) {
	id, ok := aiSessionID(c)
	if !ok {
		return models.AISession{}, false
	}
	var row models.AISession
	if err := a.db.WithContext(c.Request.Context()).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
			return models.AISession{}, false
		}
		writeDatabaseError(c)
		return models.AISession{}, false
	}
	return row, true
}

func aiSessionID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_SESSION_ID", "AI session id must be a UUID")
		return "", false
	}
	return id, true
}

func aiSessionResponseFromModel(row models.AISession) aiSessionResponse {
	return aiSessionResponse{
		ID: row.ID, Title: row.Title, Persist: row.Persist, Version: row.Version,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt),
	}
}

func aiMessageResponsesFromModels(rows []models.AIMessage) []aiMessageResponse {
	responses := make([]aiMessageResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, aiMessageResponse{
			ID: row.ID, SessionID: row.SessionID, Role: row.Role, Status: row.Status, Content: row.Content,
			Reasoning: row.Reasoning, TaskID: row.TaskID, TaskTitleSnapshot: row.TaskTitleSnapshot,
			CreatedAt: normalizeTimestamp(row.CreatedAt),
		})
	}
	return responses
}
