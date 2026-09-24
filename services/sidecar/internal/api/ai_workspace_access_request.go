package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// This tool can only ask the local UI to offer a fresh, per-message grant.
// It does not change the current generation's capabilities or persist an
// authorization that could be replayed after the generation ends.
var aiRequestableWorkspaceScopes = []string{
	"work", "clients", "outputs", "output_files", "actions", "agent_execution",
	"agent_files", "agent_project_files", "workspace_ui", "workspace_browser",
	"knowledge", "knowledge_actions", "finance", "finance_actions",
	"invoice_actions", "finance_exports",
}

type aiWorkspaceAccessRequest struct {
	Scopes []string `json:"scopes"`
}

type aiWorkspaceAccessRequestResponse struct {
	Scopes []string `json:"scopes"`
}

type aiWorkspaceAccessRequestEmitter func([]string) bool
type aiWorkspaceAccessRequestEmitterContextKey struct{}

func withAIWorkspaceAccessRequestEmitter(ctx context.Context, emit aiWorkspaceAccessRequestEmitter) context.Context {
	return context.WithValue(ctx, aiWorkspaceAccessRequestEmitterContextKey{}, emit)
}

func aiWorkspaceAccessRequestSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"scopes"},
		"properties": map[string]any{"scopes": map[string]any{
			"type": "array", "minItems": 1, "maxItems": len(aiRequestableWorkspaceScopes), "uniqueItems": true,
			"description": "Next-message scopes: 1-3 missing plus still-needed current. All need human reauthorization.",
			"items":       map[string]any{"type": "string", "enum": aiRequestableWorkspaceScopes},
		}},
	})
	return encoded
}

func validAIWorkspaceAccessRequestScopes(scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > len(aiRequestableWorkspaceScopes) {
		return false
	}
	allowed := make(map[string]bool, len(aiRequestableWorkspaceScopes))
	for _, scope := range aiRequestableWorkspaceScopes {
		allowed[scope] = true
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !allowed[scope] || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return true
}

func persistAIWorkspaceAccessRequest(tx *gorm.DB, generationID string, scopes []string, status, stamp string) error {
	if !validAIWorkspaceAccessRequestScopes(scopes) || (status != "open" && status != "dismissed") {
		return errors.New("invalid workspace access request scopes")
	}
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	if err := tx.Create(&models.AIWorkspaceAccessRequest{
		GenerationID: generationID, ScopesJSON: string(encoded), Status: "open",
		CreatedAt: stamp, UpdatedAt: stamp,
	}).Error; err != nil {
		return err
	}
	if status == "dismissed" {
		result := tx.Model(&models.AIWorkspaceAccessRequest{}).
			Where("generation_id = ? AND status = 'open'", generationID).
			Updates(map[string]any{"status": "dismissed", "updated_at": stamp})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("workspace access request dismissal was not committed")
		}
	}
	return nil
}

func loadOpenAIWorkspaceAccessRequest(db *gorm.DB, generationID string) (*aiWorkspaceAccessRequestResponse, error) {
	var row models.AIWorkspaceAccessRequest
	if err := db.Where("generation_id = ? AND status = 'open'", generationID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var scopes []string
	if json.Unmarshal([]byte(row.ScopesJSON), &scopes) != nil || !validAIWorkspaceAccessRequestScopes(scopes) {
		return nil, errors.New("stored workspace access request is invalid")
	}
	return &aiWorkspaceAccessRequestResponse{Scopes: scopes}, nil
}

func openAIWorkspaceAccessRequestsByGeneration(db *gorm.DB, generationIDs []string) (map[string]*aiWorkspaceAccessRequestResponse, error) {
	result := make(map[string]*aiWorkspaceAccessRequestResponse)
	if len(generationIDs) == 0 {
		return result, nil
	}
	var rows []models.AIWorkspaceAccessRequest
	if err := db.Where("status = 'open' AND generation_id IN ?", generationIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		var scopes []string
		if json.Unmarshal([]byte(row.ScopesJSON), &scopes) != nil || !validAIWorkspaceAccessRequestScopes(scopes) {
			return nil, errors.New("stored workspace access request is invalid")
		}
		result[row.GenerationID] = &aiWorkspaceAccessRequestResponse{Scopes: scopes}
	}
	return result, nil
}

func consumeOpenAIWorkspaceAccessRequests(tx *gorm.DB, sessionID, stamp string) error {
	return tx.Model(&models.AIWorkspaceAccessRequest{}).
		Where("status = 'open' AND generation_id IN (SELECT id FROM ai_generations WHERE session_id = ?)", sessionID).
		Updates(map[string]any{"status": "consumed", "updated_at": stamp}).Error
}

// An accepted new message consumes every open request in the session. The
// durable status, not timestamp/UUID or SQLite's mutable implicit rowid, is
// the human-review source of truth.
func hasOpenAIWorkspaceAccessRequest(tx *gorm.DB, sessionID string) (bool, error) {
	var count int64
	err := tx.Table("ai_workspace_access_requests AS request").
		Joins("JOIN ai_generations AS generation ON generation.id = request.generation_id").
		Where("generation.session_id = ? AND generation.status = 'completed' AND request.status = 'open'", sessionID).
		Count(&count).Error
	return count > 0, err
}

func (a *API) dismissAIWorkspaceAccessRequest(c *gin.Context) {
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	if a.restorePending.Load() {
		writeError(c, http.StatusServiceUnavailable, "RESTORE_PENDING", "Restore is pending")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AI_GENERATION_ID", "AI generation id must be a UUID")
		return
	}
	if c.Request.ContentLength > 0 {
		writeError(c, http.StatusBadRequest, "AI_ACCESS_REQUEST_INVALID", "No request body is accepted")
		return
	}
	a.aiWorkspaceAccessRequestMu.Lock()
	defer a.aiWorkspaceAccessRequestMu.Unlock()
	var row models.AIWorkspaceAccessRequest
	if err := a.db.WithContext(c.Request.Context()).First(&row, "generation_id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if a.aiGenerations != nil && a.aiGenerations.dismissAccessRequest(id) {
				c.Status(http.StatusNoContent)
				return
			}
			writeError(c, http.StatusNotFound, "AI_ACCESS_REQUEST_NOT_FOUND", "Workspace access request not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	if row.Status == "open" {
		if err := a.db.WithContext(c.Request.Context()).Model(&models.AIWorkspaceAccessRequest{}).
			Where("generation_id = ? AND status = 'open'", id).
			Updates(map[string]any{"status": "dismissed", "updated_at": nowStamp(a)}).Error; err != nil {
			writeDatabaseError(c)
			return
		}
	}
	if a.aiContinuations != nil {
		a.aiContinuations.wake()
	}
	c.Status(http.StatusNoContent)
}

func (t *aiWorkspaceTool) requestAccess(ctx context.Context, arguments json.RawMessage) (any, error) {
	var input aiWorkspaceAccessRequest
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, errors.New("workspace access request is invalid")
	}
	if !validAIWorkspaceAccessRequestScopes(input.Scopes) {
		return nil, harness.ErrPermissionDenied
	}
	missing := 0
	for _, scope := range input.Scopes {
		if !t.policy.Allows(scope) {
			missing++
		}
	}
	if missing == 0 || missing > 3 {
		return nil, harness.ErrPermissionDenied
	}
	emit, ok := ctx.Value(aiWorkspaceAccessRequestEmitterContextKey{}).(aiWorkspaceAccessRequestEmitter)
	if !ok || emit == nil {
		return nil, errors.New("workspace access request bridge is unavailable")
	}
	if !emit(input.Scopes) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("workspace access request already recorded for this generation")
	}
	return map[string]any{
		"requested_scopes":          input.Scopes,
		"requested":                 true,
		"granted":                   false,
		"requires_new_user_message": true,
	}, nil
}
