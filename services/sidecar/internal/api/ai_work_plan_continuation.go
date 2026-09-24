package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var (
	errAIPlanContinuationChanged     = errors.New("AI plan continuation version changed")
	errAIPlanContinuationUnavailable = errors.New("AI plan continuation evidence unavailable")
)

type aiPlanContinuationResponse struct {
	Plan                 *aiWorkPlanView             `json:"plan"`
	Ready                bool                        `json:"ready"`
	Reason               string                      `json:"reason"`
	BlockingGenerationID string                      `json:"blocking_generation_id,omitempty"`
	Blockers             []aiPlanContinuationBlocker `json:"blockers"`
}

// Blockers is a metadata-only summary of every current continuation blocker,
// not just the highest-priority one. Generation IDs let the local UI reopen
// the existing confirmation groups; no action preview, Run input or output is
// included here.
type aiPlanContinuationBlocker struct {
	Reason        string   `json:"reason"`
	Total         int      `json:"total"`
	GenerationIDs []string `json:"generation_ids"`
}

// This is a fresh read gate for preparing a draft, not authorization or a
// reservation. Sending that draft still requires a new message and grant;
// proposals and domain writes retain their existing confirmation/version gates.
func (a *API) getAIWorkPlanContinuation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.String() != c.Param("id") {
		writeError(c, http.StatusUnprocessableEntity, "AI_SESSION_ID_INVALID", "Invalid session ID")
		return
	}
	query, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
	values := query["expected_version"]
	version := int64(0)
	if len(values) == 1 {
		version, err = strconv.ParseInt(values[0], 10, 64)
	}
	if queryErr != nil || len(query) != 1 || len(values) != 1 || err != nil ||
		version < 1 || version > maxAIWorkPlanRevisions || strconv.FormatInt(version, 10) != values[0] {
		writeError(c, http.StatusUnprocessableEntity, "AI_PLAN_VERSION_INVALID", "A single canonical expected_version from 1 to 128 is required")
		return
	}
	var response aiPlanContinuationResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Select("id").Take(&session, "id=? AND persist=1", id.String()).Error; err != nil {
			return err
		}
		var revision aiWorkPlanRevision
		if err := tx.Where("session_id=?", session.ID).Order("version DESC").Take(&revision).Error; err != nil {
			return err
		}
		if revision.Version != version {
			return errAIPlanContinuationChanged
		}
		now := a.options.Now()
		plan, err := projectAIWorkPlan(tx, revision, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A plan exists, but a required source or confirmed execution fact
			// disappeared. Do not silently construct a relaxed plan projection.
			return errAIPlanContinuationUnavailable
		}
		if err != nil {
			return err
		}
		reason, blockingGeneration, blockers, err := checkAIPlanContinuationDetailed(tx, plan, now, nil, "")
		if err != nil {
			return err
		}
		response = aiPlanContinuationResponse{Plan: plan, Reason: reason, Ready: reason == "ready", BlockingGenerationID: blockingGeneration, Blockers: blockers}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeError(c, http.StatusNotFound, "AI_PLAN_NOT_FOUND", "Saved session or plan not found")
	case errors.Is(err, errAIPlanContinuationChanged):
		writeError(c, http.StatusConflict, "AI_PLAN_CONTINUATION_CHANGED", "The plan changed; refresh it and explicitly choose continuation again")
	case errors.Is(err, errAIPlanContinuationUnavailable):
		writeError(c, http.StatusConflict, "AI_PLAN_CONTINUATION_UNAVAILABLE", "The plan evidence cannot be verified; refresh and review its original records")
	case err != nil:
		writeDatabaseError(c)
	default:
		c.JSON(http.StatusOK, gin.H{"data": response})
	}
}

func checkAIPlanContinuation(tx *gorm.DB, plan *aiWorkPlanView, now time.Time) (string, string, error) {
	return checkAIPlanContinuationSources(tx, plan, now, nil, "")
}

func checkAIPlanContinuationSources(tx *gorm.DB, plan *aiWorkPlanView, now time.Time, additionalSources []string, excludeGeneration string) (string, string, error) {
	reason, generation, _, err := checkAIPlanContinuationDetailed(tx, plan, now, additionalSources, excludeGeneration)
	return reason, generation, err
}

func checkAIPlanContinuationDetailed(tx *gorm.DB, plan *aiWorkPlanView, now time.Time, additionalSources []string, excludeGeneration string) (string, string, []aiPlanContinuationBlocker, error) {
	if plan.ClosedAt != nil {
		return "plan_closed", "", []aiPlanContinuationBlocker{}, nil
	}
	var active int64
	if err := tx.Model(&models.AIGeneration{}).Where("session_id=? AND status IN ('queued','streaming') AND id<>?", plan.SessionID, excludeGeneration).Count(&active).Error; err != nil {
		return "", "", nil, err
	}
	if active > 0 {
		return "generation_active", "", []aiPlanContinuationBlocker{}, nil
	}
	// Latest plan-authoring generation is relevant even when it has no bound
	// action. Historical generations are relevant only through active bindings.
	sources := map[string]bool{plan.GenerationID: true}
	for _, source := range additionalSources {
		sources[source] = true
	}
	blocked := map[string]string{}
	blockingTotals := map[string]int{}
	blockingSources := map[string]map[string]bool{}
	markBlocked := func(reason, source string) {
		blockingTotals[reason]++
		if source != "" {
			if blockingSources[reason] == nil {
				blockingSources[reason] = map[string]bool{}
			}
			blockingSources[reason][source] = true
		}
		previous, exists := blocked[reason]
		if !exists || (source != "" && (previous == "" || source < previous)) {
			blocked[reason] = source
		}
	}
	complete := true
	for _, step := range plan.Steps {
		if step.SupersededBy != "" {
			continue
		}
		complete = complete && step.Ready && step.Satisfied
		if step.Kind != "action" || step.ProposalID == "" {
			continue
		}
		if step.Evidence == nil || step.Evidence.ProposalID != step.ProposalID {
			markBlocked("evidence_unavailable", "")
			continue
		}
		sources[step.Evidence.GenerationID] = true
	}
	orderedSources := make([]string, 0, len(sources))
	for source := range sources {
		orderedSources = append(orderedSources, source)
	}
	sort.Strings(orderedSources)
	for _, source := range orderedSources {
		var generation models.AIGeneration
		err := tx.Select("id,status").Take(&generation, "id=? AND session_id=?", source, plan.SessionID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			markBlocked("generation_unavailable", "")
			continue
		}
		if err != nil {
			return "", "", nil, err
		}
		if generation.Status != "completed" && generation.Status != "failed" && generation.Status != "cancelled" {
			markBlocked("generation_unavailable", source)
			continue
		}
		// Deliberately no receipt window/limit: every sibling belongs to the
		// gate even when it was never bound to a visible plan step.
		var proposals []models.AIActionProposal
		if err := tx.Where("generation_id=?", source).Order("created_at,id").Find(&proposals).Error; err != nil {
			return "", "", nil, err
		}
		for _, proposal := range proposals {
			if sha256Hex([]byte(proposal.ActionJSON)) != proposal.Fingerprint {
				markBlocked("evidence_unavailable", source)
				continue
			}
			if _, err := parseAIWorkspaceAction([]byte(proposal.ActionJSON)); err != nil {
				markBlocked("evidence_unavailable", source)
				continue
			}
			out, err := aiActionOutputWithFacts(tx, proposal, generation.Status, now)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				markBlocked("evidence_unavailable", source)
				continue
			}
			if err != nil {
				return "", "", nil, err
			}
			if reason := aiPlanContinuationActionBlocker(out); reason != "" {
				markBlocked(reason, source)
			}
		}
	}
	// Validate all groups before selecting a stable, actionable reason. A
	// pending sibling cannot hide another group's unavailable execution fact.
	priority := []string{"evidence_unavailable", "generation_unavailable", "pending_approval", "output_pending", "run_active"}
	blockers := make([]aiPlanContinuationBlocker, 0, len(blocked))
	for _, reason := range priority {
		if blockingTotals[reason] == 0 {
			continue
		}
		generationIDs := make([]string, 0, len(blockingSources[reason]))
		for source := range blockingSources[reason] {
			generationIDs = append(generationIDs, source)
		}
		sort.Strings(generationIDs)
		blockers = append(blockers, aiPlanContinuationBlocker{Reason: reason, Total: blockingTotals[reason], GenerationIDs: generationIDs})
	}
	for _, reason := range priority {
		if source, exists := blocked[reason]; exists {
			return reason, source, blockers, nil
		}
	}
	if complete {
		return "plan_complete", "", blockers, nil
	}
	return "ready", "", blockers, nil
}

func aiPlanContinuationActionBlocker(out aiActionResponse) string {
	switch out.Status {
	case "pending":
		return "pending_approval"
	case "rejected", "expired":
		return ""
	case "confirmed":
		// Continue below to verify execution, not merely confirmation.
	default:
		return "generation_unavailable"
	}
	if strings.HasPrefix(out.Action.Action, "agent_run.") {
		run := out.AgentRunResult
		if run == nil || out.ResultID == nil || run.ID != *out.ResultID {
			return "evidence_unavailable"
		}
		switch run.Status {
		case "queued", "running":
			if run.Status == "running" && run.OutputDeliveryStatus == agentRunOutputPending {
				return "output_pending"
			}
			if run.OutputDeliveryStatus == agentRunOutputNotReady {
				return "run_active"
			}
		case "succeeded":
			if run.OutputDeliveryStatus == agentRunOutputSubmitted {
				if run.SubmissionStatus == nil || !validAgentRunSubmissionStatus(*run.SubmissionStatus) {
					return "evidence_unavailable"
				}
				return ""
			}
			if run.OutputDeliveryStatus == agentRunOutputRetained {
				return ""
			}
		case "failed", "cancelled", "interrupted":
			if run.OutputDeliveryStatus == agentRunOutputNotReady {
				return ""
			}
		}
		return "evidence_unavailable"
	}
	if out.Action.Action == aiAgentDelegateSpawnAction || out.Action.Action == aiAgentDelegateFollowupAction {
		result := out.AgentDelegationResult
		if out.Action.Action == aiAgentDelegateFollowupAction {
			result = out.AgentFollowupResult
		}
		if result == nil || out.ResultID == nil || result.SessionID != *out.ResultID || result.GenerationID != out.ID {
			return "evidence_unavailable"
		}
		switch result.Status {
		case "queued", "streaming":
			return "run_active"
		case "completed":
			if result.PendingApprovals > 0 {
				return "pending_approval"
			}
			return ""
		case "failed", "cancelled":
			return ""
		default:
			return "evidence_unavailable"
		}
	}
	if out.Action.Action == "automation.retry" && (out.AutomationRunResult == nil ||
		(out.AutomationRunResult.Status != "succeeded" && out.AutomationRunResult.Status != "failed")) {
		return "evidence_unavailable"
	}
	return ""
}
