package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Legacy cards are derived read-only from an immutable assistant message.
// Only an explicit confirmation/rejection materializes their stable identity.
func loadAIMessageMemoryDecision(tx *gorm.DB, id string) (models.AIMemoryEntry, error) {
	var message models.AIMessage
	if err := tx.First(&message, "id = ?", id).Error; err != nil {
		return models.AIMemoryEntry{}, err
	}
	object, ok := aiControlObject(message.Content, aiMemoryControlPattern)
	var content string
	if message.Role != "assistant" || message.Status != "completed" || !ok || json.Unmarshal(object["content"], &content) != nil || strings.TrimSpace(content) == "" || utf8.RuneCountInString(strings.TrimSpace(content)) > 500 {
		return models.AIMemoryEntry{}, errAIMemoryProposalNotFound
	}
	content = strings.TrimSpace(content)
	var proposalID string
	if raw, exists := object["proposal_id"]; exists && string(raw) != "null" {
		if json.Unmarshal(raw, &proposalID) != nil {
			return models.AIMemoryEntry{}, errAIMemoryProposalMismatch
		}
	}
	if proposalID != "" {
		var proposal models.AIMemoryEntry
		if err := tx.First(&proposal, "id = ? AND session_id = ? AND kind = 'memory_proposal' AND origin = 'model_proposal'", proposalID, message.SessionID).Error; err != nil {
			return proposal, err
		}
		if proposal.Content != content {
			return proposal, errAIMemoryProposalMismatch
		}
		return proposal, nil
	}
	var existing models.AIMemoryEntry
	err := tx.First(&existing, "id = ? AND source_message_id = ? AND kind = 'memory_proposal'", id, id).Error
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return existing, err
	}
	row := models.AIMemoryEntry{ID: id, SessionID: message.SessionID, Kind: "memory_proposal", Content: content, Tags: "[]", Origin: "model_proposal", Status: "active", SourceMessageID: &id, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}
	// Previously confirmed legacy cards remain resolved even before materialization.
	var memory models.AIMemory
	err = tx.Where("source_message_id = ? AND content = ?", id, content).Order("created_at,id").First(&memory).Error
	if err == nil {
		decision := "confirmed"
		row.Status = "superseded"
		row.Decision = &decision
		row.MemoryID = &memory.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, err
	}
	return row, nil
}

func loadAIMemoryDecision(tx *gorm.DB, id string, legacy bool) (models.AIMemoryEntry, error) {
	if legacy {
		return loadAIMessageMemoryDecision(tx, id)
	}
	var row models.AIMemoryEntry
	err := tx.First(&row, "id = ? AND kind = 'memory_proposal' AND origin = 'model_proposal'", id).Error
	return row, err
}

func aiMemoryDecisionResponse(row models.AIMemoryEntry) gin.H {
	status := "pending"
	if row.Status == "superseded" {
		status = "rejected"
		if row.Decision != nil {
			status = *row.Decision
		}
	}
	tags := []string{}
	_ = json.Unmarshal([]byte(row.Tags), &tags)
	return gin.H{"id": row.ID, "session_id": row.SessionID, "content": row.Content, "tags": tags, "status": status, "memory_id": row.MemoryID, "created_at": normalizeTimestamp(row.CreatedAt)}
}

func writeAIMemoryDecisionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, errAIMemoryProposalNotFound):
		writeError(c, 404, "AI_MEMORY_PROPOSAL_NOT_FOUND", "The memory proposal is not available")
	case errors.Is(err, errAIMemoryProposalMismatch):
		writeError(c, 409, "AI_MEMORY_PROPOSAL_MISMATCH", "The proposed memory does not match this message")
	default:
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
	}
}

func (a *API) getAIMemoryProposal(c *gin.Context)           { a.getAIMemoryDecision(c, false) }
func (a *API) getAIMessageMemoryDecision(c *gin.Context)    { a.getAIMemoryDecision(c, true) }
func (a *API) rejectAIMessageMemoryDecision(c *gin.Context) { a.rejectAIMemoryDecision(c, true) }

func (a *API) getAIMemoryDecision(c *gin.Context, legacy bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "INVALID_AI_MEMORY_PROPOSAL_ID", "Memory proposal id must be a UUID")
		return
	}
	row, err := loadAIMemoryDecision(a.db.WithContext(c.Request.Context()), id, legacy)
	if err != nil {
		writeAIMemoryDecisionError(c, err)
		return
	}
	if row.Status == "superseded" && row.Decision == nil {
		writeError(c, http.StatusConflict, "AI_MEMORY_DECISION_UNAVAILABLE", "Historic memory decision cannot be verified; manage saved memories explicitly")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": aiMemoryDecisionResponse(row)})
}

func ensureAIMemoryDecisionEntry(tx *gorm.DB, row models.AIMemoryEntry) error {
	var count int64
	if err := tx.Model(&models.AIMemoryEntry{}).Where("id = ?", row.ID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return tx.Create(&row).Error
	}
	return nil
}

func (a *API) rejectAIMemoryDecision(c *gin.Context, legacy bool) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "INVALID_AI_MEMORY_PROPOSAL_ID", "Memory proposal id must be a UUID")
		return
	}
	var row models.AIMemoryEntry
	replayed := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		row, err = loadAIMemoryDecision(tx, id, legacy)
		if err != nil {
			return err
		}
		if row.Status == "superseded" {
			if row.Decision == nil {
				return newProjectRequestError(http.StatusConflict, "AI_MEMORY_DECISION_UNAVAILABLE", "Historic memory decision cannot be verified")
			}
			if row.Decision != nil && *row.Decision == "confirmed" {
				return newProjectRequestError(409, "AI_MEMORY_ALREADY_CONFIRMED", "This proposal was confirmed; delete the saved memory explicitly to remove it")
			}
			replayed = true
			return nil
		}
		if err := ensureAIMemoryDecisionEntry(tx, row); err != nil {
			return err
		}
		retiredAt := nextAIEntryTimestamp(nowStamp(a), row.UpdatedAt)
		result := tx.Model(&models.AIMemoryEntry{}).Where("id = ? AND status = 'active'", row.ID).Updates(map[string]any{"status": "superseded", "decision": "rejected", "updated_at": retiredAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errAIMemoryProposalNotFound
		}
		payload, _ := json.Marshal(map[string]any{"proposal_id": row.ID})
		var requestID any
		if value := requestIDFromContext(c); value != "" {
			requestID = value
		}
		if err := tx.Table("workflow_events").Create(map[string]any{"id": uuid.NewString(), "aggregate_type": "ai_session", "aggregate_id": row.SessionID, "action": "ai_memory_proposal_rejected", "actor_id": models.BuiltinOwnerActorID, "request_id": requestID, "previous_json": nil, "current_json": string(payload), "created_at": retiredAt}).Error; err != nil {
			return err
		}
		decision := "rejected"
		row.Decision = &decision
		row.Status = "superseded"
		return nil
	})
	if err != nil {
		writeAIMemoryDecisionError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
}
