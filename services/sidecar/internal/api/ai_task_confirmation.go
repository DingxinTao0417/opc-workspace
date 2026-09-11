package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var aiTaskControlPattern = regexp.MustCompile(`(?is)\[opc:task\](.*?)\[(?:/)?opc:task\]`)
var aiMemoryControlPattern = regexp.MustCompile(`(?is)\[opc:memory\](.*?)\[(?:/)?opc:memory\]`)

func aiControlObject(content string, pattern *regexp.Regexp) (map[string]json.RawMessage, bool) {
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) != 1 {
		return nil, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.TrimSpace(matches[0][1])), &object) != nil || object == nil {
		return nil, false
	}
	return object, true
}

// Human confirmation is an explicit task-domain command, not a model tool.
// Message identity outlives any component, request/response, or idempotency TTL.
func (a *API) confirmAIMessageTask(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "INVALID_AI_MESSAGE_ID", "AI message id must be a UUID")
		return
	}
	var input createTaskRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, 400, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	task, err := taskFromCreateRequest(input)
	if err != nil {
		code := "VALIDATION_ERROR"
		if errors.Is(err, errLifecycleCommandRequired) {
			code = "LIFECYCLE_COMMAND_REQUIRED"
		}
		writeError(c, 422, code, err.Error())
		return
	}
	tags, err := validateTaskTagIDs(input.TagIDs)
	if err != nil {
		writeError(c, 422, "VALIDATION_ERROR", err.Error())
		return
	}
	hash, err := taskCreateRequestHash(task, tags)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	var message models.AIMessage
	var response models.Task
	replayed := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&message, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "AI_MESSAGE_NOT_FOUND", "AI message not found")
			}
			return err
		}
		if message.Role != "assistant" || message.Status != "completed" {
			return newProjectRequestError(409, "AI_MESSAGE_NOT_CONFIRMABLE", "Only a completed assistant suggestion can be confirmed")
		}
		if message.TaskID != nil {
			if message.TaskConfirmationHash != nil && *message.TaskConfirmationHash != hash {
				return newProjectRequestError(409, "AI_TASK_CONFIRMATION_CONFLICT", "This suggestion was already confirmed with different task details")
			}
			response, err = loadTask(tx, *message.TaskID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(410, "AI_CONFIRMED_TASK_DELETED", "The confirmed task was deleted; this suggestion cannot create another task")
			}
			replayed = true
			return err
		}
		object, ok := aiControlObject(message.Content, aiTaskControlPattern)
		var title string
		if !ok || json.Unmarshal(object["title"], &title) != nil || strings.TrimSpace(title) == "" {
			return newProjectRequestError(422, "AI_TASK_SUGGESTION_INVALID", "No valid task suggestion is available; no task was created")
		}
		response, err = createTaskInTransaction(tx, task, tags, requestIDFromContext(c))
		if err != nil {
			return err
		}
		result := tx.Model(&models.AIMessage{}).Where("id = ? AND task_id IS NULL", id).Updates(map[string]any{"task_id": response.ID, "task_title_snapshot": response.Title, "task_confirmation_hash": hash, "updated_at": nowStamp(a)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return newProjectRequestError(409, "AI_MESSAGE_TASK_ALREADY_LINKED", "This message already references a task")
		}
		return tx.First(&message, "id = ?", id).Error
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	messages, err := aiMessageResponsesFromModels([]models.AIMessage{message})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	normalizeTask(&response)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(status, gin.H{"data": gin.H{"task": response, "message": messages[0]}})
}
