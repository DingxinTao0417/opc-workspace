package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
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
	a.rejectAIMemoryDecision(c, false)
}
