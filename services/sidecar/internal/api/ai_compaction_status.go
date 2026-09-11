package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

// Transient state has no prompt or provider error text. Restart resets this
// state, while the committed snapshot watermark remains authoritative.
type aiCompactionState struct {
	Status    string  `json:"status"`
	ErrorCode *string `json:"error_code"`
}

func (a *API) setAICompactionState(sessionID, status, code string) {
	if a.aiCompactions == nil {
		return
	}
	r := a.aiCompactions
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.states == nil {
		r.states = make(map[string]aiCompactionState)
	}
	r.states[sessionID] = aiCompactionState{Status: status, ErrorCode: aiNullableString(code)}
}

func (a *API) getAICompactionStatus(c *gin.Context) {
	session, ok := a.loadAISession(c)
	if !ok {
		return
	}
	state := aiCompactionState{Status: "idle"}
	if a.aiCompactions != nil {
		r := a.aiCompactions
		r.mu.Lock()
		if current, exists := r.states[session.ID]; exists {
			state = current
		}
		r.mu.Unlock()
	}
	active, _, err := a.activeAIContextSnapshot(session.ID)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	counts, err := a.aiSessionCompactedMessageCounts(c.Request.Context(), []models.AISession{session})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	partial := active != nil && active.SourceMessageOffset > 0
	if state.Status == "idle" && partial {
		state.Status = "pending"
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"session_id": session.ID, "status": state.Status, "error_code": state.ErrorCode, "partial_message": partial, "compacted_message_count": counts[session.ID]}})
}
