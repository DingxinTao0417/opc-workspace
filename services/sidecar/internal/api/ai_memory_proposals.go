package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiMemoryProposalResponse struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	SessionTitle string   `json:"session_title"`
	Content      string   `json:"content"`
	Tags         []string `json:"tags"`
	CreatedAt    string   `json:"created_at"`
}

func (a *API) listAIMemoryProposals(c *gin.Context) {
	var rows []struct {
		models.AIMemoryEntry
		SessionTitle string `gorm:"column:session_title"`
	}
	if err := a.db.WithContext(c.Request.Context()).Table("ai_memory_entries AS proposal").
		Select("proposal.*, ai_sessions.title AS session_title").
		Joins("JOIN ai_sessions ON ai_sessions.id = proposal.session_id").
		Where("proposal.kind = 'memory_proposal' AND proposal.origin = 'model_proposal' AND proposal.status = 'active'").
		Order("proposal.created_at DESC, proposal.id DESC").Limit(200).Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]aiMemoryProposalResponse, 0, len(rows))
	for _, row := range rows {
		var tags []string
		if err := json.Unmarshal([]byte(row.Tags), &tags); err != nil {
			writeDatabaseError(c)
			return
		}
		responses = append(responses, aiMemoryProposalResponse{
			ID: row.ID, SessionID: row.SessionID, SessionTitle: row.SessionTitle,
			Content: row.Content, Tags: tags, CreatedAt: normalizeTimestamp(row.CreatedAt),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": responses})
}

func (a *API) rejectAIMemoryProposal(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_MEMORY_PROPOSAL_ID", "AI memory proposal id must be a UUID")
		return
	}
	now := nowStamp(a)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var proposal models.AIMemoryEntry
		if err := tx.Where(
			"id = ? AND kind = 'memory_proposal' AND origin = 'model_proposal' AND status = 'active'", id,
		).First(&proposal).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errAIMemoryProposalNotFound
			}
			return err
		}
		retiredAt := nextAIEntryTimestamp(now, proposal.UpdatedAt)
		result := tx.Model(&models.AIMemoryEntry{}).Where("id = ? AND status = 'active'", id).
			Updates(map[string]any{"status": "superseded", "updated_at": retiredAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errAIMemoryProposalNotFound
		}
		payload, _ := json.Marshal(map[string]any{"proposal_id": proposal.ID})
		return tx.Table("workflow_events").Create(map[string]any{
			"id": uuid.NewString(), "aggregate_type": "ai_session", "aggregate_id": proposal.SessionID,
			"action": "ai_memory_proposal_rejected", "actor_id": models.BuiltinOwnerActorID,
			"request_id": requestIDFromContext(c), "previous_json": nil,
			"current_json": string(payload), "created_at": retiredAt,
		}).Error
	})
	if err != nil {
		if errors.Is(err, errAIMemoryProposalNotFound) {
			writeError(c, http.StatusNotFound, "AI_MEMORY_PROPOSAL_NOT_FOUND", "The memory proposal is no longer available")
			return
		}
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
}
