package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxActiveAIContinuations = 8
	maxRecentAIContinuations = 10
	recentAIContinuationAge  = 24 * time.Hour
)

type createAIContinuationRequest struct {
	ExpectedSessionVersion        int64             `json:"expected_session_version"`
	ExpectedPlanVersion           int64             `json:"expected_plan_version"`
	ProviderID                    string            `json:"provider_id"`
	ExpectedProviderVersion       int64             `json:"expected_provider_version"`
	ExpectedProviderConfigVersion int64             `json:"expected_provider_config_version"`
	Workspace                     *aiWorkspaceGrant `json:"workspace"`
	MaxTurns                      int               `json:"max_turns"`
	TTLMinutes                    int               `json:"ttl_minutes"`
	ConfirmAutomaticContinuation  bool              `json:"confirm_automatic_continuation"`
	ResumeAfterRestart            bool              `json:"resume_after_restart,omitempty"`
	ConfirmRestartContinuation    bool              `json:"confirm_restart_continuation,omitempty"`
}

// The restart choice belongs to this immutable, bounded continuation lease,
// never to the single-message workspace grant used by ordinary chat.
type aiContinuationStoredWorkspace struct {
	ProviderVersion    int64                    `json:"provider_version"`
	Scopes             []string                 `json:"scopes"`
	KnowledgeSources   []aiKnowledgeSourceGrant `json:"knowledge_sources,omitempty"`
	ResumeAfterRestart bool                     `json:"resume_after_restart,omitempty"`
}

func (s aiContinuationStoredWorkspace) grant() aiWorkspaceGrant {
	return aiWorkspaceGrant{ProviderVersion: s.ProviderVersion, Scopes: s.Scopes, KnowledgeSources: s.KnowledgeSources}
}

func decodeAIContinuationWorkspace(raw string) (aiContinuationStoredWorkspace, error) {
	var stored aiContinuationStoredWorkspace
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return stored, err
	}
	return stored, nil
}

type aiContinuationProvider struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Protocol      string `json:"protocol"`
	Model         string `json:"model"`
	Version       int64  `json:"version"`
	ConfigVersion int64  `json:"config_version"`
}

type aiContinuationResponse struct {
	ID                  string                 `json:"id"`
	SessionID           string                 `json:"session_id"`
	SessionTitle        *string                `json:"session_title,omitempty"`
	Version             int64                  `json:"version"`
	Status              string                 `json:"status"`
	Reason              string                 `json:"reason"`
	InitialPlanVersion  int64                  `json:"initial_plan_version"`
	CurrentPlanVersion  int64                  `json:"current_plan_version"`
	Provider            aiContinuationProvider `json:"provider"`
	Workspace           aiWorkspaceGrant       `json:"workspace"`
	ResumeAfterRestart  bool                   `json:"resume_after_restart"`
	MaxTurns            int                    `json:"max_turns"`
	TurnsStarted        int                    `json:"turns_started"`
	CurrentGenerationID *string                `json:"current_generation_id"`
	LastGenerationID    *string                `json:"last_generation_id"`
	CreatedAt           string                 `json:"created_at"`
	UpdatedAt           string                 `json:"updated_at"`
	ExpiresAt           string                 `json:"expires_at"`
}

type aiGenerationOrigin struct {
	Kind           string `json:"kind"`
	ContinuationID string `json:"continuation_id"`
	TurnIndex      int    `json:"turn_index"`
	MaxTurns       int    `json:"max_turns"`
}

func continuationActive(status string) bool { return status == "waiting" || status == "running" }

func aiContinuationRequestError(code, message string) error {
	return &aiBusinessContextRequestError{status: http.StatusConflict, code: code, message: message}
}

func validateAIContinuationGrant(provider *models.AIProvider, grant *aiWorkspaceGrant) error {
	if grant == nil {
		return aiContinuationRequestError("AI_CONTINUATION_SCOPE_REQUIRED", "Explicitly authorize workspace access for this bounded continuation")
	}
	if err := validateAIWorkspaceGrant(provider, grant); err != nil {
		return err
	}
	work, actions := false, false
	for _, scope := range grant.Scopes {
		if scope == "work" {
			work = true
		}
		if scope == "actions" {
			actions = true
		}
		if scope == "workspace_ui" || scope == "workspace_browser" {
			return aiContinuationRequestError("AI_CONTINUATION_SCOPE_INVALID", "Background continuation cannot navigate UI or request browser tabs")
		}
	}
	if !work || !actions {
		return aiContinuationRequestError("AI_CONTINUATION_SCOPE_REQUIRED", "Continuation requires work and actions to read and update the current plan")
	}
	return nil
}

func continuationResponse(row models.AIContinuation) (aiContinuationResponse, error) {
	stored, err := decodeAIContinuationWorkspace(row.WorkspaceJSON)
	if err != nil {
		return aiContinuationResponse{}, err
	}
	return aiContinuationResponse{
		ID: row.ID, SessionID: row.SessionID, Version: row.Version, Status: row.Status, Reason: row.Reason,
		InitialPlanVersion: row.InitialPlanVersion, CurrentPlanVersion: row.CurrentPlanVersion,
		Provider:  aiContinuationProvider{ID: row.ProviderID, Name: row.ProviderName, Kind: row.ProviderKind, Protocol: row.ProviderProtocol, Model: row.ProviderModel, Version: row.ProviderVersion, ConfigVersion: row.ProviderConfigVersion},
		Workspace: stored.grant(), ResumeAfterRestart: stored.ResumeAfterRestart, MaxTurns: row.MaxTurns, TurnsStarted: row.TurnsStarted,
		CurrentGenerationID: row.CurrentGenerationID, LastGenerationID: row.LastGenerationID,
		CreatedAt: normalizeTimestamp(row.CreatedAt), UpdatedAt: normalizeTimestamp(row.UpdatedAt), ExpiresAt: normalizeTimestamp(row.ExpiresAt),
	}, nil
}

func (a *API) writeAIContinuation(c *gin.Context, status int, row models.AIContinuation) {
	response, err := continuationResponse(row)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"data": response})
}

func (a *API) getAIContinuation(c *gin.Context) {
	sessionID, ok := aiSessionID(c)
	if !ok {
		return
	}
	var session models.AISession
	if err := a.db.WithContext(c.Request.Context()).Take(&session, "id=? AND persist=1", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, 404, "AI_SESSION_NOT_FOUND", "Saved session not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	var row models.AIContinuation
	err := a.db.WithContext(c.Request.Context()).Where("session_id=?", sessionID).Order("created_at DESC,id DESC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Header("Cache-Control", "no-store")
		c.JSON(200, gin.H{"data": nil})
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	a.writeAIContinuation(c, 200, row)
}

func (a *API) listAIContinuations(c *gin.Context) {
	query := c.Request.URL.RawQuery
	if query != "status=active" && query != "status=recent" {
		writeError(c, 422, "AI_CONTINUATION_QUERY_INVALID", "Use status=active or status=recent")
		return
	}
	var rows []models.AIContinuation
	q := a.db.WithContext(c.Request.Context())
	if query == "status=active" {
		q = q.Where("status IN ('waiting','running')").Order("created_at,id").Limit(maxActiveAIContinuations)
	} else {
		// A bounded read-only history survives navigation/reload without turning
		// a stopped authorization into a new request or a persistent alert.
		cutoff := a.options.Now().UTC().Add(-recentAIContinuationAge).Format(time.RFC3339Nano)
		q = q.Where("status NOT IN ('waiting','running') AND julianday(updated_at) >= julianday(?)", cutoff).
			Order("julianday(updated_at) DESC,id DESC").Limit(maxRecentAIContinuations)
	}
	if err := q.Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	type sessionLabel struct{ ID, Title string }
	labels := map[string]string{}
	if len(rows) > 0 {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.SessionID)
		}
		var sessions []sessionLabel
		if err := a.db.WithContext(c.Request.Context()).Model(&models.AISession{}).Select("id,title").Where("id IN ? AND persist=1", ids).Scan(&sessions).Error; err != nil {
			writeDatabaseError(c)
			return
		}
		for _, session := range sessions {
			labels[session.ID] = session.Title
		}
	}
	result := make([]aiContinuationResponse, 0, len(rows))
	for _, row := range rows {
		response, err := continuationResponse(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		if title := labels[row.SessionID]; title != "" {
			response.SessionTitle = &title
		}
		result = append(result, response)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": result})
}

func (a *API) createAIContinuation(c *gin.Context) {
	sessionID, ok := aiSessionID(c)
	if !ok {
		return
	}
	var input createAIContinuationRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, 400, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	providerUUID, idErr := uuid.Parse(input.ProviderID)
	if idErr != nil || providerUUID.String() != input.ProviderID || !input.ConfirmAutomaticContinuation || input.ResumeAfterRestart != input.ConfirmRestartContinuation || input.ExpectedSessionVersion < 1 || input.ExpectedPlanVersion < 1 || input.ExpectedPlanVersion > maxAIWorkPlanRevisions || input.ExpectedProviderVersion < 1 || input.ExpectedProviderConfigVersion < 1 || input.MaxTurns < 1 || input.MaxTurns > 8 || input.TTLMinutes < 1 || input.TTLMinutes > 120 {
		writeError(c, 422, "AI_CONTINUATION_INVALID", "Explicit consent, current versions, 1-8 generations and 1-120 minutes are required")
		return
	}
	// Same lock order as chat acceptance. Middleware deliberately does not hold
	// a storage lease for these handlers, avoiding nested RWMutex acquisition.
	a.aiGenerations.beginMu.Lock()
	defer a.aiGenerations.beginMu.Unlock()
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	a.aiProviderMu.RLock()
	defer a.aiProviderMu.RUnlock()
	if a.restorePending.Load() {
		writeError(c, 409, "RESTORE_PENDING", "Restart to finish the pending restore")
		return
	}
	if a.aiContinuations == nil || a.aiContinuations.closed() {
		writeError(c, 503, "AI_CONTINUATION_UNAVAILABLE", "Continuation worker is unavailable")
		return
	}
	var row models.AIContinuation
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Take(&session, "id=? AND persist=1", sessionID).Error; err != nil {
			return err
		}
		if session.Version != input.ExpectedSessionVersion {
			return aiContinuationRequestError("AI_CONTINUATION_SESSION_CHANGED", "The saved conversation changed; refresh and authorize again")
		}
		var active int64
		if err := tx.Model(&models.AIContinuation{}).Where("session_id=? AND status IN ('waiting','running')", sessionID).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return aiContinuationRequestError("AI_CONTINUATION_ACTIVE", "Stop the active continuation before authorizing another")
		}
		if err := tx.Model(&models.AIContinuation{}).Where("status IN ('waiting','running')").Count(&active).Error; err != nil {
			return err
		}
		if active >= maxActiveAIContinuations {
			return aiContinuationRequestError("AI_CONTINUATION_CAPACITY", "At most eight continuations may be active")
		}
		var provider models.AIProvider
		if err := tx.Take(&provider, "id=?", input.ProviderID).Error; err != nil {
			return err
		}
		if provider.Version != input.ExpectedProviderVersion || provider.ConfigVersion != input.ExpectedProviderConfigVersion || provider.Status != "ready" {
			return aiContinuationRequestError("AI_CONTINUATION_PROVIDER_CHANGED", "The selected provider changed or is not ready")
		}
		if err := validateAIContinuationGrant(&provider, input.Workspace); err != nil {
			return err
		}
		if err := validateAIKnowledgeSources(c.Request.Context(), tx, input.Workspace.KnowledgeSources); err != nil {
			return err
		}
		var revision aiWorkPlanRevision
		if err := tx.Where("session_id=?", sessionID).Order("version DESC").Take(&revision).Error; err != nil {
			return err
		}
		if revision.Version != input.ExpectedPlanVersion {
			return aiContinuationRequestError("AI_PLAN_CONTINUATION_CHANGED", "The plan changed; refresh and authorize again")
		}
		now := a.options.Now().UTC()
		plan, err := projectAIWorkPlan(tx, revision, now)
		if err != nil {
			return err
		}
		reason, _, err := checkAIPlanContinuation(tx, plan, now)
		if err != nil {
			return err
		}
		if reason != "ready" && reason != "pending_approval" && reason != "run_active" && reason != "output_pending" {
			return aiContinuationRequestError("AI_CONTINUATION_NOT_ELIGIBLE", "Review the plan and its current evidence before enabling continuation")
		}
		pendingAccess, err := hasOpenAIWorkspaceAccessRequest(tx, sessionID)
		if err != nil {
			return err
		}
		if pendingAccess {
			reason = "pending_approval"
		}
		// Canonical immutable capability snapshot, not a pointer to UI defaults.
		sort.Strings(input.Workspace.Scopes)
		sort.Slice(input.Workspace.KnowledgeSources, func(i, j int) bool {
			return input.Workspace.KnowledgeSources[i].SourceID < input.Workspace.KnowledgeSources[j].SourceID
		})
		encoded, err := json.Marshal(aiContinuationStoredWorkspace{ProviderVersion: input.Workspace.ProviderVersion, Scopes: input.Workspace.Scopes, KnowledgeSources: input.Workspace.KnowledgeSources, ResumeAfterRestart: input.ResumeAfterRestart})
		if err != nil {
			return err
		}
		row = models.AIContinuation{ID: uuid.NewString(), SessionID: sessionID, ProviderID: provider.ID, ProviderVersion: provider.Version, ProviderConfigVersion: provider.ConfigVersion, ProviderName: provider.Name, ProviderKind: provider.Kind, ProviderProtocol: provider.Protocol, ProviderModel: provider.Model, WorkspaceJSON: string(encoded), InitialPlanVersion: revision.Version, CurrentPlanVersion: revision.Version, MaxTurns: input.MaxTurns, Status: "waiting", Reason: reason, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Duration(input.TTLMinutes) * time.Minute).Format(time.RFC3339Nano)}
		return tx.Create(&row).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, 404, "AI_CONTINUATION_SOURCE_NOT_FOUND", "Saved conversation, plan or provider not found")
		return
	}
	if err != nil {
		if !writeAIBusinessContextError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	a.aiContinuations.wake()
	a.writeAIContinuation(c, 201, row)
}

func (a *API) stopAIContinuation(c *gin.Context) {
	id := c.Param("id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		writeError(c, 422, "AI_CONTINUATION_ID_INVALID", "Invalid continuation ID")
		return
	}
	// Revoke process-local eligibility and cancel promptly, including while a
	// backup holds the maintenance writer. The HTTP stop is not accepted until
	// the durable continuation/generation state is committed below.
	if a.aiContinuations != nil {
		a.aiContinuations.cancelOne(id)
	}
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	row, err := a.stopAIContinuationID(c.Request.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, 404, "AI_CONTINUATION_NOT_FOUND", "Continuation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	a.writeAIContinuation(c, 200, row)
}

// Revoke the authorization, without ever cancelling an Agent Run: its separate
// human approval survives this lease. In-memory revocation covers maintenance.
func (a *API) stopAIContinuationID(ctx context.Context, id string) (models.AIContinuation, error) {
	var row models.AIContinuation
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Take(&row, "id=?", id).Error; err != nil {
			return err
		}
		generationID := row.CurrentGenerationID
		if generationID == nil {
			// The worker may have already committed its cancellation finalizer
			// before this stop transaction acquired the maintenance read lock.
			// Recheck only this continuation's latest turn; never infer an
			// unrelated generation or manufacture an event for completed work.
			var turn models.AIContinuationTurn
			if err := tx.Where("continuation_id=?", id).Order("turn_index DESC").Take(&turn).Error; err == nil {
				generationID = &turn.GenerationID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if generationID != nil {
			var generation models.AIGeneration
			if err := tx.Select("id", "session_id", "status").Where("id=?", *generationID).Take(&generation).Error; err == nil {
				if generation.Status == "queued" || generation.Status == "streaming" || generation.Status == "cancelled" {
					if _, err := recordAIGenerationStopIntentEventTx(tx, generation, "", nowStamp(a)); err != nil {
						return err
					}
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if continuationActive(row.Status) {
			if err := updateAIContinuation(tx, &row, "stopped", "user_stopped", a.options.Now()); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		if a.aiContinuations != nil {
			a.aiContinuations.cancelOne(id)
			a.aiContinuations.forgetRevocation(id)
		}
	}
	if err == nil {
		if row.CurrentGenerationID != nil {
			a.aiGenerations.cancel(*row.CurrentGenerationID)
		}
	}
	return row, err
}

func (a *API) stopAIContinuationForGeneration(generationID string) error {
	var turn models.AIContinuationTurn
	err := a.db.Take(&turn, "generation_id=?", generationID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = a.stopAIContinuationID(context.Background(), turn.ContinuationID)
	return err
}

func aiGenerationOriginFor(db *gorm.DB, id string) (*aiGenerationOrigin, error) {
	var result aiGenerationOrigin
	q := db.Table("ai_continuation_turns AS t").Select("t.continuation_id,t.turn_index,c.max_turns").Joins("JOIN ai_continuations c ON c.id=t.continuation_id").Where("t.generation_id=?", id).Take(&result)
	if errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if q.Error != nil {
		return nil, q.Error
	}
	result.Kind = "plan_continuation"
	return &result, nil
}
