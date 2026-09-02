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

type attachTaskToAIMessageRequest struct {
	TaskID string `json:"task_id"`
}

// attachTaskToAIMessage records the static task reference created through the
// regular task API for one assistant reply. Creation itself never flows
// through the AI module: this endpoint only validates that both sides exist
// and snapshots the task title.
func (a *API) attachTaskToAIMessage(c *gin.Context) {
	messageID := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(messageID); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_MESSAGE_ID", "AI message id must be a UUID")
		return
	}
	var input attachTaskToAIMessageRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	taskID := strings.TrimSpace(input.TaskID)
	if _, err := uuid.Parse(taskID); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "AI_TASK_ID_INVALID", "Task id must be a UUID")
		return
	}
	var message models.AIMessage
	if err := a.db.WithContext(c.Request.Context()).Where("id = ?", messageID).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AI_MESSAGE_NOT_FOUND", "AI message not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	if message.TaskID != nil {
		writeError(c, http.StatusConflict, "AI_MESSAGE_TASK_ALREADY_LINKED", "This message already references a task and cannot be relinked")
		return
	}
	var task struct {
		Title string
	}
	if err := a.db.WithContext(c.Request.Context()).Table("tasks").Select("title").Where("id = ?", taskID).Take(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AI_TASK_NOT_FOUND", "Task not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	title := strings.TrimSpace(task.Title)
	result := a.db.WithContext(c.Request.Context()).Model(&models.AIMessage{}).
		Where("id = ? AND task_id IS NULL", messageID).
		Updates(map[string]any{"task_id": taskID, "task_title_snapshot": title, "updated_at": now})
	if result.Error != nil {
		writeDatabaseError(c)
		return
	}
	if result.RowsAffected == 0 {
		writeError(c, http.StatusConflict, "AI_MESSAGE_TASK_ALREADY_LINKED", "This message already references a task and cannot be relinked")
		return
	}
	if err := a.db.WithContext(c.Request.Context()).Where("id = ?", messageID).First(&message).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": aiMessageResponse{
		ID: message.ID, SessionID: message.SessionID, Role: message.Role, Status: message.Status,
		Content: message.Content, Reasoning: message.Reasoning, TaskID: message.TaskID,
		TaskTitleSnapshot: message.TaskTitleSnapshot, CreatedAt: normalizeTimestamp(message.CreatedAt),
	}})
}
