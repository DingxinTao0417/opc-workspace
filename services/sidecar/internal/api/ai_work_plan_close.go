package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// A user may close a plan revision that no longer reflects their intent. The
// closure is an immutable workflow event bound to exactly one revision: a
// later model-authored revision is a new intent and starts open. Closing never
// rejects approvals, cancels Runs, stops continuations or changes any business
// fact, so it is only accepted when nothing in the plan still needs the user.
const aiWorkPlanClosedAction = "ai_work_plan_closed"

// SQL fragment for an `ai_work_plan_revisions AS revision` row whose exact
// version was closed. Closed revisions stop claiming their proposals and Runs.
const aiWorkPlanRevisionClosedFilter = `EXISTS (
	SELECT 1
	FROM workflow_events AS closure
	WHERE closure.aggregate_type = 'ai_work_plan'
		AND closure.aggregate_id = revision.session_id
		AND closure.action = 'ai_work_plan_closed'
		AND json_extract(closure.current_json, '$.version') = revision.version
)`

var (
	errAIPlanCloseChanged     = errors.New("AI plan close version changed")
	errAIPlanCloseBlocked     = errors.New("AI plan still has open obligations")
	errAIPlanCloseUnavailable = errors.New("AI plan close evidence unavailable")
)

type closeAIWorkPlanRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
	ConfirmClose    bool  `json:"confirm_close"`
}

func aiWorkPlanClosedAt(tx *gorm.DB, sessionID string, version int64) (*string, error) {
	var event struct {
		CreatedAt string `gorm:"column:created_at"`
	}
	err := tx.Table("workflow_events").Select("created_at").
		Where("aggregate_type='ai_work_plan' AND aggregate_id=? AND action=? AND json_extract(current_json, '$.version')=?",
			sessionID, aiWorkPlanClosedAction, version).
		Order("created_at, id").Take(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	closedAt := normalizeTimestamp(event.CreatedAt)
	return &closedAt, nil
}

func (a *API) closeAIWorkPlan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.String() != c.Param("id") {
		writeError(c, http.StatusUnprocessableEntity, "AI_SESSION_ID_INVALID", "Invalid session ID")
		return
	}
	var input closeAIWorkPlanRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !input.ConfirmClose || input.ExpectedVersion < 1 || input.ExpectedVersion > maxAIWorkPlanRevisions {
		writeError(c, http.StatusUnprocessableEntity, "AI_PLAN_CLOSE_INVALID", "Explicit confirmation and the current plan version are required")
		return
	}
	var out *aiWorkPlanView
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Select("id, title, updated_at").Take(&session, "id=? AND persist=1", id.String()).Error; err != nil {
			return err
		}
		var revision aiWorkPlanRevision
		if err := tx.Where("session_id=?", session.ID).Order("version DESC").Take(&revision).Error; err != nil {
			return err
		}
		if revision.Version != input.ExpectedVersion {
			return errAIPlanCloseChanged
		}
		now := a.options.Now()
		plan, err := projectAIWorkPlan(tx, revision, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errAIPlanCloseUnavailable
		}
		if err != nil {
			return err
		}
		if plan.ClosedAt != nil {
			out = plan // Replayed confirmation: keep the first closure.
			return nil
		}
		if summarizeAIWorkPlanInbox(plan, session.Title, session.UpdatedAt).State != "awaiting_continuation" {
			return errAIPlanCloseBlocked
		}
		reason, _, _, err := checkAIPlanContinuationDetailed(tx, plan, now, nil, "")
		if err != nil {
			return err
		}
		if reason != "ready" {
			return errAIPlanCloseBlocked
		}
		var activeContinuations int64
		if err := tx.Model(&models.AIContinuation{}).Where("session_id=? AND status IN ('waiting','running')", session.ID).
			Count(&activeContinuations).Error; err != nil {
			return err
		}
		if activeContinuations > 0 {
			return errAIPlanCloseBlocked
		}
		closedAt := now.UTC().Format(time.RFC3339Nano)
		payload, _ := json.Marshal(map[string]any{"session_id": session.ID, "version": revision.Version, "generation_id": revision.GenerationID})
		if err := tx.Table("workflow_events").Create(map[string]any{"id": uuid.NewString(), "aggregate_type": "ai_work_plan",
			"aggregate_id": session.ID, "action": aiWorkPlanClosedAction, "actor_id": models.BuiltinOwnerActorID,
			"current_json": string(payload), "created_at": closedAt}).Error; err != nil {
			return err
		}
		normalized := normalizeTimestamp(closedAt)
		plan.ClosedAt = &normalized
		out = plan
		return nil
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "AI_PLAN_NOT_FOUND", "Saved session or plan not found")
	case errors.Is(err, errAIPlanCloseChanged):
		writeError(c, http.StatusConflict, "AI_PLAN_CLOSE_CHANGED", "The plan changed; review the latest revision before closing it")
	case errors.Is(err, errAIPlanCloseBlocked):
		writeError(c, http.StatusConflict, "AI_PLAN_CLOSE_BLOCKED", "The plan still has approvals, executions, recovery, an active generation or continuation")
	case errors.Is(err, errAIPlanCloseUnavailable):
		writeError(c, http.StatusConflict, "AI_PLAN_CLOSE_UNAVAILABLE", "The plan evidence cannot be verified; refresh and review its original records")
	case err != nil:
		writeDatabaseError(c)
	default:
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}
