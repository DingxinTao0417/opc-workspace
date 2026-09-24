package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	maxAIWorkPlanRevisions   = 128
	maxAIWorkPlanInboxWindow = 1000
)

// Intent is model-authored. Action state is never accepted from the model; it
// is projected from same-session approvals and their authoritative receipts.
type aiWorkPlanStep struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	DependsOn  []string `json:"depends_on"`
	Report     string   `json:"report,omitempty"`
	ProposalID string   `json:"proposal_id,omitempty"`
	Replaces   string   `json:"replaces,omitempty"`
}

type aiWorkPlanIntent struct {
	Title string           `json:"title"`
	Steps []aiWorkPlanStep `json:"steps"`
}

type aiWorkPlanRevision struct {
	SessionID    string `gorm:"column:session_id;primaryKey"`
	Version      int64  `gorm:"column:version;primaryKey"`
	GenerationID string `gorm:"column:generation_id"`
	PlanJSON     string `gorm:"column:plan_json"`
	CreatedAt    string `gorm:"column:created_at"`
}

func (aiWorkPlanRevision) TableName() string { return "ai_work_plan_revisions" }

// aiWorkPlanUpdate is a stream-only invalidation signal. It deliberately
// excludes the plan title, steps, proposals and every business payload; the
// already-authorized local client re-reads its own session plan after receipt.
type aiWorkPlanUpdate struct {
	GenerationID string
	Version      int64
	StepCount    int
}

type aiWorkPlanUpdateEmitter func(aiWorkPlanUpdate) bool

type aiWorkPlanUpdateEmitterContextKey struct{}

// A plan remains usable without an active chat stream. This optional emitter
// improves the live UI only; unlike an explicit local navigation request, its
// absence never claims or prevents a local side effect.
func withAIWorkPlanUpdateEmitter(ctx context.Context, emit aiWorkPlanUpdateEmitter) context.Context {
	return context.WithValue(ctx, aiWorkPlanUpdateEmitterContextKey{}, emit)
}

// Deliberately excludes action changes, previews, names and output text. The
// human opens the existing generation's confirmation cards to inspect/decide.
type aiWorkPlanEvidence struct {
	ProposalID       string  `json:"proposal_id"`
	GenerationID     string  `json:"generation_id"`
	Action           string  `json:"action"`
	Status           string  `json:"status"`
	ResultID         *string `json:"result_id,omitempty"`
	SubmissionStatus *string `json:"submission_status,omitempty"`
	Route            string  `json:"route,omitempty"`
	RetryOfRunID     string  `json:"retry_of_run_id,omitempty"`
	RestartOfRunID   string  `json:"restart_of_run_id,omitempty"`
}

type aiWorkPlanStepView struct {
	aiWorkPlanStep
	State        string              `json:"state"`
	Ready        bool                `json:"ready"`
	Satisfied    bool                `json:"satisfied"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	Evidence     *aiWorkPlanEvidence `json:"evidence,omitempty"`
}

type aiWorkPlanView struct {
	SessionID        string `json:"session_id"`
	Version          int64  `json:"version"`
	GenerationID     string `json:"generation_id"`
	GenerationStatus string `json:"generation_status"`
	Title            string `json:"title"`
	CreatedAt        string `json:"created_at"`
	AsOf             string `json:"as_of"`
	// Set only when the user explicitly closed this exact revision.
	ClosedAt *string              `json:"closed_at,omitempty"`
	Steps    []aiWorkPlanStepView `json:"steps"`
}

type aiWorkPlanInboxItem struct {
	SessionID            string `json:"session_id"`
	SessionTitle         string `json:"session_title"`
	SessionUpdatedAt     string `json:"session_updated_at"`
	Version              int64  `json:"version"`
	GenerationID         string `json:"generation_id"`
	GenerationStatus     string `json:"generation_status"`
	PlanTitle            string `json:"plan_title"`
	State                string `json:"state"`
	StepTotal            int    `json:"step_total"`
	SatisfiedStepTotal   int    `json:"satisfied_step_total"`
	SupersededStepTotal  int    `json:"superseded_step_total"`
	PendingApprovalTotal int    `json:"pending_approval_total"`
	ActiveStepTotal      int    `json:"active_step_total"`
	RecoveryStepTotal    int    `json:"recovery_step_total"`
	CreatedAt            string `json:"created_at"`
	AsOf                 string `json:"as_of"`
}

type aiWorkPlanInboxMeta struct {
	StateFilter               string `json:"state_filter"`
	Total                     int    `json:"total"`
	AttentionTotal            int    `json:"attention_total"`
	RunningTotal              int    `json:"running_total"`
	NeedsApprovalTotal        int    `json:"needs_approval_total"`
	NeedsRecoveryTotal        int    `json:"needs_recovery_total"`
	AwaitingContinuationTotal int    `json:"awaiting_continuation_total"`
	CompletedTotal            int    `json:"completed_total"`
	ClosedTotal               int    `json:"closed_total"`
	HasMore                   bool   `json:"has_more"`
	NextOffset                *int   `json:"next_offset"`
	WindowLimited             bool   `json:"window_limited"`
	AsOf                      string `json:"as_of"`
}

type aiWorkPlanInboxRow struct {
	SessionID        string `gorm:"column:session_id"`
	Version          int64  `gorm:"column:version"`
	GenerationID     string `gorm:"column:generation_id"`
	PlanJSON         string `gorm:"column:plan_json"`
	CreatedAt        string `gorm:"column:created_at"`
	SessionTitle     string `gorm:"column:session_title"`
	SessionUpdatedAt string `gorm:"column:session_updated_at"`
}

var aiPlanStepKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

func validateAIWorkPlan(plan aiWorkPlanIntent) error {
	if strings.TrimSpace(plan.Title) == "" || utf8.RuneCountInString(plan.Title) > 200 || len(plan.Steps) < 1 || len(plan.Steps) > 12 {
		return errors.New("plan requires a title (1-200 characters) and 1-12 steps")
	}
	seen := map[string]bool{}
	stepsByID := map[string]aiWorkPlanStep{}
	replacedBy := map[string]string{}
	proposals := map[string]bool{}
	active := 0
	for _, step := range plan.Steps {
		if !aiPlanStepKey.MatchString(step.ID) || seen[step.ID] || strings.TrimSpace(step.Title) == "" || utf8.RuneCountInString(step.Title) > 300 || step.DependsOn == nil {
			return errors.New("steps require unique stable IDs, titles (1-300 characters) and explicit depends_on arrays")
		}
		deps := map[string]bool{}
		for _, dep := range step.DependsOn {
			// Ordered dependencies make cycles, self-links and forward references
			// impossible and give the user one readable topological sequence.
			if !seen[dep] || deps[dep] {
				return errors.New("dependencies must be distinct earlier step IDs")
			}
			deps[dep] = true
		}
		seen[step.ID] = true
		if step.Replaces != "" {
			prior, exists := stepsByID[step.Replaces]
			if step.Kind != "action" || !exists || prior.Kind != "action" || prior.ProposalID == "" || replacedBy[step.Replaces] != "" {
				return errors.New("replaces requires a distinct earlier bound action with no other replacement")
			}
			replacedBy[step.Replaces] = step.ID
		}
		stepsByID[step.ID] = step
		switch step.Kind {
		case "analysis":
			if step.ProposalID != "" || (step.Report != "pending" && step.Report != "in_progress" && step.Report != "reported_done") {
				return errors.New("analysis only accepts pending/in_progress/reported_done; self-reports are not business proof")
			}
			if step.Report == "in_progress" {
				active++
			}
		case "action":
			if step.Report != "" {
				return errors.New("action progress is runtime-owned; never supply report or completion")
			}
			if step.ProposalID != "" {
				id, err := uuid.Parse(step.ProposalID)
				if err != nil || id.String() != step.ProposalID || proposals[step.ProposalID] {
					return errors.New("proposal_id must be a distinct canonical UUID")
				}
				proposals[step.ProposalID] = true
			}
		default:
			return errors.New("step kind must be analysis or action")
		}
	}
	for _, step := range plan.Steps {
		if replacedBy[step.ID] != "" {
			continue // Frozen historical dependencies remain visible, not redirected.
		}
		for _, dep := range step.DependsOn {
			if replacedBy[dep] != "" {
				return errors.New("current steps must explicitly depend on current steps, not superseded history")
			}
		}
	}
	if active > 1 {
		return errors.New("at most one analysis step may be in_progress")
	}
	encoded, err := json.Marshal(plan)
	if err != nil || len(encoded) > 12<<10 {
		return errors.New("plan exceeds 12 KiB; shorten titles instead of storing business bodies")
	}
	return nil
}

func validateAIWorkPlanFieldPresence(raw json.RawMessage, plan aiWorkPlanIntent) error {
	var encoded struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &encoded); err != nil || len(encoded.Steps) != len(plan.Steps) {
		return errors.New("invalid plan steps")
	}
	for index, rawStep := range encoded.Steps {
		fields, err := decodeAIWorkPlanStepFields(rawStep)
		if err != nil {
			return err
		}
		_, reportPresent := fields["report"]
		proposal, proposalPresent := fields["proposal_id"]
		replaces, replacesPresent := fields["replaces"]
		switch plan.Steps[index].Kind {
		case "analysis":
			if !reportPresent || proposalPresent || replacesPresent {
				return errors.New("analysis requires report and does not accept proposal_id or replaces")
			}
		case "action":
			if reportPresent {
				return errors.New("action does not accept report")
			}
			if proposalPresent {
				var value string
				if json.Unmarshal(proposal, &value) != nil || value == "" {
					return errors.New("proposal_id must be a non-empty string when present")
				}
			}
			if replacesPresent {
				var value string
				if json.Unmarshal(replaces, &value) != nil || value == "" {
					return errors.New("replaces must be a non-empty step ID when present")
				}
			}
		}
	}
	return nil
}

// Keep this input boundary local to plan steps. Go's regular struct decoder
// accepts case aliases and repeated object keys; neither may hide an explicit
// null/empty replacement or overwrite a different replacement intent.
func decodeAIWorkPlanStepFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("plan step must be an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("invalid plan step field")
		}
		switch key {
		case "id", "title", "kind", "depends_on", "report", "proposal_id", "replaces":
		default:
			return nil, errors.New("unsupported plan step field")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errors.New("duplicate plan step field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("invalid plan step value")
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, errors.New("invalid plan step object")
	}
	return fields, nil
}

func aiWorkPlanSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["operation"],"properties":{
"operation":{"type":"string","enum":["read","update"]},
"expected_version":{"type":"integer","minimum":0,"maximum":127},
"plan":{"type":"object","additionalProperties":false,"required":["title","steps"],"properties":{
"title":{"type":"string","minLength":1,"maxLength":200},
"steps":{"type":"array","minItems":1,"maxItems":12,"items":{"type":"object","additionalProperties":false,"required":["id","title","kind","depends_on"],"properties":{
"id":{"type":"string","pattern":"^[a-z][a-z0-9_-]{0,39}$"},"title":{"type":"string","minLength":1,"maxLength":300},"kind":{"type":"string","enum":["analysis","action"]},
"depends_on":{"type":"array","uniqueItems":true,"items":{"type":"string"},"description":"Earlier IDs; current steps cannot depend on superseded history."},
"report":{"type":"string","enum":["pending","in_progress","reported_done"],"description":"Analysis only; self-report, not business proof."},
"proposal_id":{"type":"string","format":"uuid","description":"Action only: same-session approval ID; omit until proposed."},
"replaces":{"type":"string","description":"New action only: earlier rejected/expired intent, or confirmed Agent failure without output / succeeded+retained replaced by a bound same-Task exact retry or start with changes.restart_of_run_id. Keep the exact source through rejected/expired replacements until a real new Run exists. Retained only accepts current-facts restart, never exact retry. Immutable; does not approve or execute."}
}}}}}}}`)
}

func validateAIWorkPlanRevision(previous, next aiWorkPlanIntent) error {
	previousByID := make(map[string]aiWorkPlanStep, len(previous.Steps))
	nextByID := make(map[string]aiWorkPlanStep, len(next.Steps))
	replaced := make(map[string]bool)
	for _, step := range previous.Steps {
		previousByID[step.ID] = step
	}
	for _, step := range next.Steps {
		nextByID[step.ID] = step
		if step.Replaces != "" {
			replaced[step.Replaces] = true
			if prior, present := previousByID[step.Replaces]; !present || prior.Kind != "action" || prior.ProposalID == "" {
				return errors.New("replacement target must be a bound action in the preceding plan revision")
			}
		}
	}
	for _, prior := range previous.Steps {
		current, present := nextByID[prior.ID]
		if !present || prior.Kind != current.Kind || prior.Replaces != current.Replaces ||
			(prior.ProposalID != "" && prior.ProposalID != current.ProposalID) {
			return errors.New("retain prior steps, their kind, bound proposal and replacement relation")
		}
		if replaced[prior.ID] && (prior.Title != current.Title || strings.Join(prior.DependsOn, ",") != strings.Join(current.DependsOn, ",")) {
			return errors.New("retain superseded steps' original title and dependencies")
		}
	}
	return nil
}

func (t *aiWorkspaceTool) workPlan(ctx context.Context, arguments json.RawMessage) (any, error) {
	for _, scope := range []string{"work", "actions"} {
		if err := t.policy.Require(scope); err != nil {
			return nil, err
		}
	}
	var input struct {
		Operation       string            `json:"operation"`
		ExpectedVersion *int64            `json:"expected_version"`
		Plan            *aiWorkPlanIntent `json:"plan"`
	}
	if len(arguments) > 16<<10 {
		return nil, errors.New("plan arguments too large")
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	if input.Operation != "read" && input.Operation != "update" {
		return nil, errors.New("operation must be read or update")
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(arguments, &raw)
	if input.Operation == "read" && len(raw) != 1 {
		return nil, errors.New("read accepts only operation")
	}
	if input.Operation == "update" {
		if input.Plan == nil || input.ExpectedVersion == nil || *input.ExpectedVersion < 0 || *input.ExpectedVersion >= maxAIWorkPlanRevisions {
			return nil, errors.New("update requires complete plan and expected_version (0 for first plan; otherwise read latest)")
		}
		if err := validateAIWorkPlan(*input.Plan); err != nil {
			return nil, err
		}
		if err := validateAIWorkPlanFieldPresence(raw["plan"], *input.Plan); err != nil {
			return nil, err
		}
	}
	var out *aiWorkPlanView
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var generation models.AIGeneration
		if err := tx.Table("ai_generations AS g").Select("g.*").Joins("JOIN ai_sessions s ON s.id=g.session_id").
			Where("g.id=? AND g.status='streaming' AND s.persist=1 AND g.provider_id=?", t.generationID, t.providerID).Take(&generation).Error; err != nil {
			return errors.New("plan requires this provider's active saved generation")
		}
		var row aiWorkPlanRevision
		err := tx.Where("session_id=?", generation.SessionID).Order("version DESC").Take(&row).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if input.Operation == "update" {
			encoded, _ := json.Marshal(input.Plan)
			// Retry of an uncertain tool result must not append another revision.
			if row.Version == *input.ExpectedVersion+1 && row.GenerationID == generation.ID && row.PlanJSON == string(encoded) {
				out, err = projectAIWorkPlan(tx, row, t.api.options.Now())
				return err
			}
			if row.Version != *input.ExpectedVersion {
				return errors.New("plan version changed; read latest before editing")
			}
			var previous aiWorkPlanIntent
			if row.Version > 0 {
				if err := json.Unmarshal([]byte(row.PlanJSON), &previous); err != nil {
					return err
				}
			}
			if err := validateAIWorkPlanRevision(previous, *input.Plan); err != nil {
				return err
			}
			row = aiWorkPlanRevision{SessionID: generation.SessionID, Version: row.Version + 1, GenerationID: generation.ID,
				PlanJSON: string(encoded), CreatedAt: t.api.options.Now().UTC().Format(time.RFC3339Nano)}
			// Validate all evidence before persisting; no cross-session reads.
			out, err = projectAIWorkPlan(tx, row, t.api.options.Now())
			if err != nil {
				return err
			}
			for _, step := range out.Steps {
				if step.ProposalID != "" && step.Evidence == nil {
					return errors.New("proposal is not in this conversation")
				}
			}
			if err = tx.Create(&row).Error; err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"session_id": row.SessionID, "generation_id": row.GenerationID, "version": row.Version, "step_count": len(input.Plan.Steps)})
			return tx.Table("workflow_events").Create(map[string]any{"id": uuid.NewString(), "aggregate_type": "ai_work_plan", "aggregate_id": row.SessionID,
				"action": "ai_work_plan_updated", "actor_id": models.BuiltinOwnerActorID, "current_json": string(payload), "created_at": row.CreatedAt}).Error
		}
		if row.Version == 0 {
			return nil
		}
		out, err = projectAIWorkPlan(tx, row, t.api.options.Now())
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Never leak SQL, preview/body or stored invalid JSON through errors.
		return nil, errors.New("plan unavailable: verify active saved generation, same-session proposals and latest expected_version")
	}
	if input.Operation == "update" && out != nil {
		if emit, ok := ctx.Value(aiWorkPlanUpdateEmitterContextKey{}).(aiWorkPlanUpdateEmitter); ok && emit != nil && !emit(aiWorkPlanUpdate{
			GenerationID: out.GenerationID,
			Version:      out.Version,
			StepCount:    len(out.Steps),
		}) {
			return nil, context.Canceled
		}
	}
	instruction := "Plan intent is not permission or completion proof. Action states are live receipts; analysis reported_done is self-reported only. No work is scheduled, approved or executed by this tool. Read again on continuation with fresh work+actions consent."
	if out != nil && out.ClosedAt != nil {
		instruction += " The user closed this plan revision: do not continue it. Only write a new revision if the user explicitly asks for new planned work in this message."
	}
	return map[string]any{"plan": out, "instruction": instruction}, nil
}

func projectAIWorkPlan(tx *gorm.DB, row aiWorkPlanRevision, now time.Time) (*aiWorkPlanView, error) {
	var generation models.AIGeneration
	if err := tx.Select("status").Take(&generation, "id=? AND session_id=?", row.GenerationID, row.SessionID).Error; err != nil {
		return nil, err
	}
	var plan aiWorkPlanIntent
	if err := decodeStrictToolArguments(json.RawMessage(row.PlanJSON), &plan); err != nil {
		return nil, err
	}
	if err := validateAIWorkPlan(plan); err != nil {
		return nil, err
	}
	out := &aiWorkPlanView{SessionID: row.SessionID, Version: row.Version, GenerationID: row.GenerationID, Title: plan.Title, CreatedAt: row.CreatedAt,
		GenerationStatus: generation.Status,
		AsOf:             now.UTC().Format(time.RFC3339Nano), Steps: []aiWorkPlanStepView{}}
	satisfied := map[string]bool{}
	actionFacts := map[string]aiWorkPlanActionFacts{}
	for _, step := range plan.Steps {
		view := aiWorkPlanStepView{aiWorkPlanStep: step, State: "not_proposed", Ready: true}
		for _, dep := range step.DependsOn {
			view.Ready = view.Ready && satisfied[dep]
		}
		if step.Kind == "analysis" {
			view.State = step.Report
			if step.Report == "in_progress" && generation.Status != "streaming" {
				view.State = "awaiting_continuation"
			}
			view.Satisfied = step.Report == "reported_done"
		} else if step.ProposalID != "" {
			var source struct {
				models.AIActionProposal
				GenerationStatus string
			}
			err := tx.Table("ai_action_proposals p").Select("p.*, g.status AS generation_status").Joins("JOIN ai_generations g ON g.id=p.generation_id").
				Where("p.id=? AND g.session_id=?", step.ProposalID, row.SessionID).Take(&source).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				view.State = "evidence_unavailable"
			} else if err != nil {
				return nil, err
			} else {
				receipt, err := aiActionOutputWithFacts(tx, source.AIActionProposal, source.GenerationStatus, now)
				if err != nil {
					return nil, err
				}
				view.State, view.Satisfied = aiWorkPlanActionState(receipt)
				view.Evidence = &aiWorkPlanEvidence{ProposalID: receipt.ID, GenerationID: receipt.GenerationID, Action: receipt.Action.Action,
					Status: receipt.Status, ResultID: receipt.ResultID, Route: receipt.Route}
				if receipt.AgentRunResult != nil {
					view.Evidence.SubmissionStatus = receipt.AgentRunResult.SubmissionStatus
				}
				actionFacts[step.ID] = aiWorkPlanActionFacts{Proposal: source.AIActionProposal, Receipt: receipt}
				if receipt.Action.Action == "agent_run.retry" {
					action, err := strictAIWorkPlanAction(source.AIActionProposal)
					if err != nil {
						return nil, err
					}
					view.Evidence.RetryOfRunID = action.AgentRunID
				}
				if receipt.Action.Action == "agent_run.start" {
					action, err := strictAIWorkPlanAction(source.AIActionProposal)
					if err != nil {
						return nil, err
					}
					view.Evidence.RestartOfRunID, err = aiActionRestartOfRunID(action)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		// Readiness never rewrites actual outcomes. A manually approved action
		// can have happened out of order; show that truth, not a fabricated wait.
		satisfied[step.ID] = view.Satisfied && view.Ready
		out.Steps = append(out.Steps, view)
	}
	if err := projectAIWorkPlanReplacements(tx, out, actionFacts); err != nil {
		return nil, err
	}
	closedAt, err := aiWorkPlanClosedAt(tx, row.SessionID, row.Version)
	if err != nil {
		return nil, err
	}
	out.ClosedAt = closedAt
	return out, nil
}

func aiWorkPlanActionState(out aiActionResponse) (string, bool) {
	if out.Status != "confirmed" {
		return out.Status, false
	}
	if out.Action.Action == aiAgentDelegateSpawnAction || out.Action.Action == aiAgentDelegateFollowupAction {
		result := out.AgentDelegationResult
		if out.Action.Action == aiAgentDelegateFollowupAction {
			result = out.AgentFollowupResult
		}
		if result == nil {
			return "evidence_unavailable", false
		}
		switch result.Status {
		case "completed":
			if result.PendingApprovals > 0 {
				return "child_pending_approval", false
			}
			return "completed", true
		case "queued", "streaming":
			return result.Status, false
		case "failed", "cancelled":
			return result.Status, false
		default:
			return "evidence_unavailable", false
		}
	}
	if strings.HasPrefix(out.Action.Action, "agent_run.") {
		if out.Action.Action == aiAgentRunCancelManyAction {
			if out.AgentRunCancelManyResult == nil || len(out.AgentRunCancelManyResult.Items) != out.AgentRunCancelManyResult.Count {
				return "evidence_unavailable", false
			}
			for _, item := range out.AgentRunCancelManyResult.Items {
				if item.Status != "cancelled" {
					return "running", false
				}
			}
			return "cancelled", true
		}
		run := out.AgentRunResult
		if run == nil {
			return "evidence_unavailable", false
		}
		if run.OutputDeliveryStatus == "pending" {
			return "output_pending", false
		}
		if out.Action.Action == "agent_run.cancel" {
			return run.Status, run.Status == "cancelled"
		}
		if run.Status == "succeeded" {
			if run.OutputDeliveryStatus == "submitted" {
				if run.SubmissionStatus == nil || !validAgentRunSubmissionStatus(*run.SubmissionStatus) {
					return "evidence_unavailable", false
				}
				return "submitted", true
			}
			return "retained", false
		}
		return run.Status, false
	}
	if out.Action.Action == "automation.retry" {
		if out.AutomationRunResult == nil {
			return "evidence_unavailable", false
		}
		return out.AutomationRunResult.Status, out.AutomationRunResult.Status == "succeeded"
	}
	if out.Action.Action == "finance.export_csv" {
		return "download_unverified", false
	}
	return "recorded", true // Command receipt, NOT current Task/project completion.
}

func (a *API) getAIWorkPlan(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.String() != c.Param("id") {
		writeError(c, 422, "AI_SESSION_ID_INVALID", "Invalid session ID")
		return
	}
	version := int64(0)
	if values, ok := c.Request.URL.Query()["version"]; ok {
		if len(values) != 1 {
			writeError(c, 422, "AI_PLAN_VERSION_INVALID", "Invalid plan version")
			return
		}
		version, err = strconv.ParseInt(values[0], 10, 64)
		if err != nil || version < 1 || version > maxAIWorkPlanRevisions {
			writeError(c, 422, "AI_PLAN_VERSION_INVALID", "Invalid plan version")
			return
		}
	}
	var out *aiWorkPlanView
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var session models.AISession
		if err := tx.Take(&session, "id=?", id.String()).Error; err != nil {
			return err
		}
		var row aiWorkPlanRevision
		query := tx.Where("session_id=?", session.ID)
		if version != 0 {
			query = query.Where("version=?", version)
		}
		err := query.Order("version DESC").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && version == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		out, err = projectAIWorkPlan(tx, row, a.options.Now())
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, 404, "AI_PLAN_NOT_FOUND", "Session or plan revision not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func summarizeAIWorkPlanInbox(plan *aiWorkPlanView, sessionTitle, sessionUpdatedAt string) aiWorkPlanInboxItem {
	item := aiWorkPlanInboxItem{
		SessionID: plan.SessionID, SessionTitle: sessionTitle, SessionUpdatedAt: normalizeTimestamp(sessionUpdatedAt),
		Version: plan.Version, GenerationID: plan.GenerationID, GenerationStatus: plan.GenerationStatus,
		PlanTitle: plan.Title, StepTotal: len(plan.Steps), CreatedAt: normalizeTimestamp(plan.CreatedAt), AsOf: plan.AsOf,
	}
	for _, step := range plan.Steps {
		if step.SupersededBy != "" {
			item.SupersededStepTotal++
			continue
		}
		if step.Satisfied && step.Ready {
			item.SatisfiedStepTotal++
		}
		if step.Kind != "action" {
			continue
		}
		switch step.State {
		case "pending", "child_pending_approval":
			item.PendingApprovalTotal++
		case "queued", "running":
			item.ActiveStepTotal++
		case "output_pending", "retained":
			item.RecoveryStepTotal++
		}
	}
	switch {
	case plan.ClosedAt != nil:
		item.State = "closed"
	case item.SatisfiedStepTotal+item.SupersededStepTotal == item.StepTotal:
		item.State = "completed"
	case item.RecoveryStepTotal > 0:
		item.State = "needs_recovery"
	case item.PendingApprovalTotal > 0:
		item.State = "needs_approval"
	case item.ActiveStepTotal > 0 || plan.GenerationStatus == "streaming":
		item.State = "running"
	default:
		item.State = "awaiting_continuation"
	}
	return item
}

func includeAIWorkPlanInboxState(filter, state string) bool {
	switch filter {
	case "all":
		return true
	case "attention":
		return state != "completed" && state != "closed"
	default:
		return filter == state
	}
}

// listAIWorkPlans is the local Agent Inbox. It exposes only plan/session
// metadata and live, server-projected counts. It never returns action previews,
// business bodies, output text or a reusable workspace grant.
func (a *API) listAIWorkPlans(c *gin.Context) {
	state := strings.TrimSpace(c.DefaultQuery("state", "attention"))
	validState := state == "all" || state == "attention" || state == "running" || state == "needs_approval" ||
		state == "needs_recovery" || state == "awaiting_continuation" || state == "completed" || state == "closed"
	if !validState {
		writeError(c, http.StatusUnprocessableEntity, "AI_PLAN_STATE_INVALID", "Invalid work plan state")
		return
	}
	limit, ok := queryInt(c, "limit", 20, 1, 50)
	if !ok {
		return
	}
	offset, ok := queryInt(c, "offset", 0, 0, maxAIWorkPlanInboxWindow-1)
	if !ok {
		return
	}
	now := a.options.Now()
	items := make([]aiWorkPlanInboxItem, 0)
	meta := aiWorkPlanInboxMeta{StateFilter: state, AsOf: now.UTC().Format(time.RFC3339Nano)}
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var rows []aiWorkPlanInboxRow
		if err := tx.Raw(`
			SELECT revision.session_id, revision.version, revision.generation_id,
			       revision.plan_json, revision.created_at,
			       session.title AS session_title, session.updated_at AS session_updated_at
			FROM ai_work_plan_revisions AS revision
			JOIN (
				SELECT session_id, MAX(version) AS version
				FROM ai_work_plan_revisions
				GROUP BY session_id
			) AS latest
			  ON latest.session_id = revision.session_id AND latest.version = revision.version
			JOIN ai_sessions AS session ON session.id = revision.session_id
			ORDER BY revision.created_at DESC, revision.session_id ASC
			LIMIT ?
		`, maxAIWorkPlanInboxWindow+1).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) > maxAIWorkPlanInboxWindow {
			meta.WindowLimited = true
			rows = rows[:maxAIWorkPlanInboxWindow]
		}
		filtered := make([]aiWorkPlanInboxItem, 0, len(rows))
		for _, row := range rows {
			plan, err := projectAIWorkPlan(tx, aiWorkPlanRevision{
				SessionID: row.SessionID, Version: row.Version, GenerationID: row.GenerationID,
				PlanJSON: row.PlanJSON, CreatedAt: row.CreatedAt,
			}, now)
			if err != nil {
				return err
			}
			item := summarizeAIWorkPlanInbox(plan, row.SessionTitle, row.SessionUpdatedAt)
			switch item.State {
			case "running":
				meta.RunningTotal++
			case "needs_approval":
				meta.NeedsApprovalTotal++
			case "needs_recovery":
				meta.NeedsRecoveryTotal++
			case "awaiting_continuation":
				meta.AwaitingContinuationTotal++
			case "completed":
				meta.CompletedTotal++
			case "closed":
				meta.ClosedTotal++
			}
			if item.State != "completed" && item.State != "closed" {
				meta.AttentionTotal++
			}
			if includeAIWorkPlanInboxState(state, item.State) {
				filtered = append(filtered, item)
			}
		}
		meta.Total = len(filtered)
		if offset >= len(filtered) {
			items = []aiWorkPlanInboxItem{}
			return nil
		}
		end := offset + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		items = filtered[offset:end]
		meta.HasMore = end < len(filtered)
		if meta.HasMore && end < maxAIWorkPlanInboxWindow {
			next := end
			meta.NextOffset = &next
		}
		return nil
	})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}
