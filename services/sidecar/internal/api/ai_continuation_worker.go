package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The durable lease is authority; this registry only owns process-local
// cancellation and enforces two background workers. It never replays attempts.
type aiContinuationCoordinator struct {
	a              *API
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	closing        bool
	workers        map[string]context.CancelFunc
	revoked        map[string]bool
	wg             sync.WaitGroup
	done           chan struct{}
	wakeup         chan struct{}
	factWaiters    map[uint64]chan struct{}
	nextFactWaiter uint64
}

func newAIContinuationCoordinator(a *API) *aiContinuationCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	r := &aiContinuationCoordinator{a: a, ctx: ctx, cancel: cancel, workers: map[string]context.CancelFunc{}, revoked: map[string]bool{}, done: make(chan struct{}), wakeup: make(chan struct{}, 1), factWaiters: map[uint64]chan struct{}{}}
	go func() {
		defer close(r.done)
		interval := a.options.ContinuationScanInterval
		if interval <= 0 {
			<-ctx.Done()
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-r.wakeup:
			}
			r.scan()
		}
	}()
	return r
}

func (r *aiContinuationCoordinator) wake() {
	if r == nil {
		return
	}
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
	// Keep the coordinator's single-consumer scan channel intact while also
	// broadcasting a payload-free hint to any child-status waiters. Each waiter
	// re-reads authoritative rows in a fresh read-only transaction.
	r.mu.Lock()
	for _, waiter := range r.factWaiters {
		select {
		case waiter <- struct{}{}:
		default:
		}
	}
	r.mu.Unlock()
}

// subscribeFactWake registers a fan-out observer without consuming the
// coordinator's scan signal. Notifications contain no identity or business
// data; callers must refresh their own authorized database projection.
func (r *aiContinuationCoordinator) subscribeFactWake() (<-chan struct{}, func()) {
	if r == nil {
		return nil, func() {}
	}
	wake := make(chan struct{}, 1)
	r.mu.Lock()
	if r.factWaiters == nil {
		r.factWaiters = map[uint64]chan struct{}{}
	}
	r.nextFactWaiter++
	id := r.nextFactWaiter
	r.factWaiters[id] = wake
	closing := r.closing
	r.mu.Unlock()
	if closing {
		wake <- struct{}{}
	}
	var once sync.Once
	return wake, func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.factWaiters, id)
			r.mu.Unlock()
		})
	}
}

// wakeAIContinuationsAfterFactCommit is called only after a transaction that
// may change plan evidence has committed. The channel deliberately coalesces
// bursts: one scan re-reads every authoritative proposal and Agent Run fact,
// so callers never need to enqueue identities or business payloads here.
func (a *API) wakeAIContinuationsAfterFactCommit() {
	if a != nil && a.aiContinuations != nil {
		a.aiContinuations.wake()
	}
	if a != nil {
		a.agentRunGates.wake()
	}
}

func (r *aiContinuationCoordinator) closed() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.closing }
func (r *aiContinuationCoordinator) cancelOne(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked[id] = true
	if cancel := r.workers[id]; cancel != nil {
		cancel()
	}
}

// Once durable revocation commits (or the ID is proven absent), no tombstone
// is needed. Until then it forbids new work even behind a maintenance writer.
func (r *aiContinuationCoordinator) forgetRevocation(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.revoked, id)
}
func (r *aiContinuationCoordinator) cancelAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closing = true
	r.cancel()
	for _, cancel := range r.workers {
		cancel()
	}
	r.mu.Unlock()
}
func (r *aiContinuationCoordinator) close() {
	if r == nil {
		return
	}
	r.cancelAll()
	<-r.done
	r.wg.Wait()
	r.a.maintenance.RLock()
	defer r.a.maintenance.RUnlock()
	if !r.a.restorePending.Load() {
		if err := interruptNonResumableAIContinuations(r.a.db, "shutdown", r.a.options.Now()); err != nil {
			r.a.options.Logger.Print("AI continuation shutdown state could not be recorded")
		}
	}
}

func interruptAIContinuations(db *gorm.DB, reason string, now time.Time) error {
	return db.Model(&models.AIContinuation{}).Where("status IN ('waiting','running')").Updates(map[string]any{
		"status": "interrupted", "reason": reason, "version": gorm.Expr("version+1"), "updated_at": now.UTC().Format(time.RFC3339Nano), "current_generation_id": nil,
	}).Error
}

// A frozen, separately confirmed restart choice can preserve only an idle
// waiting lease. OnAccept atomically moves the lease to running and records
// the generation before any model request; that state is never replayed.
// Restore still uses interruptAIContinuations and revokes every lease.
func interruptNonResumableAIContinuations(db *gorm.DB, reason string, now time.Time) error {
	return db.Model(&models.AIContinuation{}).
		Where("status='running' OR (status='waiting' AND (current_generation_id IS NOT NULL OR COALESCE(json_type(workspace_json,'$.resume_after_restart'),'') <> 'true'))").
		Updates(map[string]any{
			"status": "interrupted", "reason": reason, "version": gorm.Expr("version+1"),
			"updated_at": now.UTC().Format(time.RFC3339Nano), "current_generation_id": nil,
		}).Error
}

func recoverAIContinuationsOnStartup(db *gorm.DB, restored bool, now time.Time) error {
	if restored {
		// A restored backup belongs to a different timeline. Never revive its
		// authorization, even if it opted into ordinary process restarts.
		return interruptAIContinuations(db, "restore_pending", now)
	}
	return interruptNonResumableAIContinuations(db, "process_restart", now)
}

// Caller holds a storage lease. CAS prevents a late worker from undoing stop,
// expiry, restart or restore. Terminal state never becomes active again.
func updateAIContinuation(tx *gorm.DB, row *models.AIContinuation, status, reason string, now time.Time) error {
	q := tx.Model(&models.AIContinuation{}).Where("id=? AND version=? AND status IN ('waiting','running')", row.ID, row.Version).Updates(map[string]any{
		"status": status, "reason": reason, "version": row.Version + 1, "updated_at": now.UTC().Format(time.RFC3339Nano), "current_plan_version": row.CurrentPlanVersion,
	})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Continuation state changed")
	}
	row.Status, row.Reason, row.Version, row.UpdatedAt = status, reason, row.Version+1, now.UTC().Format(time.RFC3339Nano)
	return nil
}

type aiContinuationObservation struct {
	Plan   *aiWorkPlanView
	Reason string
	Hash   string
}

// Every accepted automatic generation participates, including unbound sibling
// proposals and generations which never rewrote the plan. No UI receipt window.
func observeAIContinuation(tx *gorm.DB, row models.AIContinuation, now time.Time, excludeGeneration string) (aiContinuationObservation, error) {
	var out aiContinuationObservation
	var revision aiWorkPlanRevision
	if err := tx.Where("session_id=?", row.SessionID).Order("version DESC").Take(&revision).Error; err != nil {
		return out, err
	}
	var turns []models.AIContinuationTurn
	if err := tx.Where("continuation_id=?", row.ID).Order("turn_index").Find(&turns).Error; err != nil {
		return out, err
	}
	owned := map[string]bool{}
	sources := make([]string, 0, len(turns))
	var initial aiWorkPlanRevision
	if err := tx.Take(&initial, "session_id=? AND version=?", row.SessionID, row.InitialPlanVersion).Error; err != nil {
		return out, err
	}
	sources = append(sources, initial.GenerationID)
	for _, turn := range turns {
		owned[turn.GenerationID] = true
		sources = append(sources, turn.GenerationID)
	}
	var changes []aiWorkPlanRevision
	if err := tx.Where("session_id=? AND version>?", row.SessionID, row.InitialPlanVersion).Find(&changes).Error; err != nil {
		return out, err
	}
	for _, change := range changes {
		if !owned[change.GenerationID] {
			out.Reason = "plan_changed"
			return out, nil
		}
	}
	plan, err := projectAIWorkPlan(tx, revision, now)
	if err != nil {
		return out, err
	}
	out.Plan = plan
	out.Reason, _, err = checkAIPlanContinuationSources(tx, plan, now, sources, excludeGeneration)
	if err != nil {
		return out, err
	}
	// Exclude clock, revision and author-generation churn. Rewriting the same
	// plan or replying without any semantic/factual progress cannot spend again.
	facts := struct {
		Title   string
		Steps   []aiWorkPlanStepView
		Actions []any
	}{Title: plan.Title, Steps: plan.Steps, Actions: []any{}}
	sourceSet := map[string]bool{plan.GenerationID: true}
	for _, source := range sources {
		sourceSet[source] = true
	}
	for _, step := range plan.Steps {
		if step.Evidence != nil {
			sourceSet[step.Evidence.GenerationID] = true
		}
	}
	ordered := make([]string, 0, len(sourceSet))
	for id := range sourceSet {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		var proposals []models.AIActionProposal
		if err := tx.Where("generation_id=?", id).Order("id").Find(&proposals).Error; err != nil {
			return out, err
		}
		var gen models.AIGeneration
		if err := tx.Take(&gen, "id=?", id).Error; err != nil {
			return out, err
		}
		for _, proposal := range proposals {
			view, err := aiActionOutputWithFacts(tx, proposal, gen.Status, now)
			if err != nil {
				return out, err
			}
			facts.Actions = append(facts.Actions, struct {
				ID, Status    string
				ResultID      *string
				ResultVersion *int64
				Agent         *aiAgentRunResult
				Automation    *aiAutomationRetryResult
			}{view.ID, view.Status, view.ResultID, view.ResultVersion, view.AgentRunResult, view.AutomationRunResult})
		}
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return out, err
	}
	out.Hash = sha256Hex(encoded)
	return out, nil
}

func validateAIContinuationIdentity(ctx context.Context, tx *gorm.DB, row models.AIContinuation) (aiWorkspaceGrant, string, error) {
	stored, err := decodeAIContinuationWorkspace(row.WorkspaceJSON)
	if err != nil {
		return aiWorkspaceGrant{}, "scope_changed", nil
	}
	grant := stored.grant()
	var provider models.AIProvider
	err = tx.Take(&provider, "id=?", row.ProviderID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return grant, "provider_changed", nil
	}
	if err != nil {
		return grant, "", err
	}
	if provider.Status != "ready" || provider.Version != row.ProviderVersion || provider.ConfigVersion != row.ProviderConfigVersion || provider.Kind != row.ProviderKind || provider.Protocol != row.ProviderProtocol || provider.Model != row.ProviderModel {
		return grant, "provider_changed", nil
	}
	if err := validateAIContinuationGrant(&provider, &grant); err != nil {
		return grant, "scope_changed", nil
	}
	if err := validateAIKnowledgeSources(ctx, tx, grant.KnowledgeSources); err != nil {
		var requestErr *aiBusinessContextRequestError
		if errors.As(err, &requestErr) {
			return grant, "scope_changed", nil
		}
		return grant, "", err
	}
	return grant, "", nil
}

func (r *aiContinuationCoordinator) scan() {
	if r.ctx.Err() != nil || r.a.restorePending.Load() {
		return
	}
	r.a.maintenance.RLock()
	defer r.a.maintenance.RUnlock()
	if r.ctx.Err() != nil || r.a.restorePending.Load() {
		return
	}
	var rows []models.AIContinuation
	if err := r.a.db.Where("status IN ('waiting','running')").Order("created_at,id").Limit(maxActiveAIContinuations).Find(&rows).Error; err != nil {
		r.a.options.Logger.Print("AI continuation scan failed")
		return
	}
	for _, row := range rows {
		r.mu.Lock()
		_, running := r.workers[row.ID]
		closing := r.closing
		r.mu.Unlock()
		if closing {
			return
		}
		if running {
			continue
		}
		// A running ledger with no owned worker is uncertain, never replayable.
		if row.Status == "running" {
			_ = updateAIContinuation(r.a.db, &row, "interrupted", "evidence_unavailable", r.a.options.Now())
			continue
		}
		var grant aiWorkspaceGrant
		ready := false
		err := r.a.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Take(&row, "id=?", row.ID).Error; err != nil {
				return err
			}
			if !continuationActive(row.Status) {
				return nil
			}
			now := r.a.options.Now().UTC()
			expires, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
			if err != nil {
				return err
			}
			if !now.Before(expires) {
				return updateAIContinuation(tx, &row, "expired", "time_limit", now)
			}
			var reason string
			grant, reason, err = validateAIContinuationIdentity(r.ctx, tx, row)
			if err != nil {
				return err
			}
			if reason != "" {
				return updateAIContinuation(tx, &row, "stopped", reason, now)
			}
			pendingAccess, err := hasOpenAIWorkspaceAccessRequest(tx, row.SessionID)
			if err != nil {
				return err
			}
			if pendingAccess {
				if row.Reason != "pending_approval" {
					return updateAIContinuation(tx, &row, "waiting", "pending_approval", now)
				}
				return nil
			}
			observation, err := observeAIContinuation(tx, row, now, "")
			if err != nil {
				return err
			}
			if observation.Plan != nil {
				row.CurrentPlanVersion = observation.Plan.Version
			}
			switch observation.Reason {
			case "plan_complete":
				return updateAIContinuation(tx, &row, "completed", "plan_complete", now)
			case "plan_changed", "plan_closed":
				return updateAIContinuation(tx, &row, "stopped", "plan_changed", now)
			case "pending_approval", "run_active", "output_pending":
				if row.Reason != observation.Reason {
					return updateAIContinuation(tx, &row, "waiting", observation.Reason, now)
				}
				return nil
			case "generation_active":
				if row.Reason != "provider_busy" {
					return updateAIContinuation(tx, &row, "waiting", "provider_busy", now)
				}
				return nil
			case "ready":
			default:
				return updateAIContinuation(tx, &row, "stopped", "evidence_unavailable", now)
			}
			if row.TurnsStarted >= row.MaxTurns {
				return updateAIContinuation(tx, &row, "exhausted", "turn_limit", now)
			}
			if row.LastObservationHash != "" && row.LastObservationHash == observation.Hash {
				return updateAIContinuation(tx, &row, "stopped", "no_progress", now)
			}
			r.a.aiGenerations.mu.Lock()
			busy := r.a.aiGenerations.active["provider:"+row.ProviderID] != "" || r.a.aiGenerations.active["session:"+row.SessionID] != ""
			r.a.aiGenerations.mu.Unlock()
			if busy {
				if row.Reason != "provider_busy" {
					return updateAIContinuation(tx, &row, "waiting", "provider_busy", now)
				}
				return nil
			}
			ready = true
			return nil
		})
		if err != nil {
			// A failed evidence read never sends a request. Storage failure remains
			// visible, without leaking SQL/model text to logs or remote providers.
			_ = updateAIContinuation(r.a.db, &row, "stopped", "evidence_unavailable", r.a.options.Now())
			continue
		}
		if ready {
			r.launch(row, grant)
		}
	}
}

func (r *aiContinuationCoordinator) launch(row models.AIContinuation, grant aiWorkspaceGrant) {
	r.mu.Lock()
	if r.closing || r.revoked[row.ID] || len(r.workers) >= 2 {
		r.mu.Unlock()
		return
	}
	if _, exists := r.workers[row.ID]; exists {
		r.mu.Unlock()
		return
	}
	expires, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
	if err != nil {
		r.mu.Unlock()
		return
	}
	duration := expires.Sub(r.a.options.Now())
	if duration > 10*time.Minute {
		duration = 10 * time.Minute
	}
	if duration <= 0 {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, duration)
	r.workers[row.ID] = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		accepted := false
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.workers, row.ID)
			r.mu.Unlock()
			// Pre-acceptance failures wait for the next tick, avoiding a busy loop.
			if accepted {
				r.wake()
			}
		}()
		generationID := uuid.NewString()
		options := aiGenerationRunOptions{GenerationID: generationID, RequestKey: "continuation:" + row.ID + ":" + fmt.Sprint(row.TurnsStarted+1), RequestID: uuid.NewString(), ContinuationID: row.ID, Headless: true}
		options.OnAccept = func(tx *gorm.DB, generation models.AIGeneration) error {
			var current models.AIContinuation
			if err := tx.Take(&current, "id=?", row.ID).Error; err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			now := r.a.options.Now().UTC()
			deadline, err := time.Parse(time.RFC3339Nano, current.ExpiresAt)
			if err != nil {
				return err
			}
			if current.Status != "waiting" || current.TurnsStarted >= current.MaxTurns || !now.Before(deadline) {
				return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Continuation is no longer eligible")
			}
			_, reason, err := validateAIContinuationIdentity(ctx, tx, current)
			if err != nil {
				return err
			}
			if reason != "" {
				return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Continuation authorization changed")
			}
			pendingAccess, err := hasOpenAIWorkspaceAccessRequest(tx, current.SessionID)
			if err != nil {
				return err
			}
			if pendingAccess {
				return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Workspace access request awaits human review")
			}
			observed, err := observeAIContinuation(tx, current, now, generation.ID)
			if err != nil {
				return err
			}
			if observed.Reason != "ready" || (current.LastObservationHash != "" && current.LastObservationHash == observed.Hash) {
				return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Continuation facts changed or made no progress")
			}
			q := tx.Model(&models.AIContinuation{}).Where("id=? AND version=? AND status='waiting'", current.ID, current.Version).Updates(map[string]any{
				"status": "running", "reason": "generating", "version": current.Version + 1, "turns_started": current.TurnsStarted + 1, "current_generation_id": generation.ID, "last_generation_id": generation.ID, "current_plan_version": observed.Plan.Version, "last_observation_hash": observed.Hash, "updated_at": now.Format(time.RFC3339Nano),
			})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Continuation already claimed")
			}
			return tx.Create(&models.AIContinuationTurn{ContinuationID: current.ID, TurnIndex: current.TurnsStarted + 1, GenerationID: generation.ID, PlanVersion: observed.Plan.Version, ObservationHash: observed.Hash, CreatedAt: now.Format(time.RFC3339Nano)}).Error
		}
		input := chatAIRequest{ProviderID: row.ProviderID, SessionID: row.SessionID, Workspace: &grant, Message: fmt.Sprintf("[自动续办 · 第 %d/%d 轮]\n继续本会话已授权的当前计划。先使用 workspace_plan 读取最新计划，再查询真实审批、执行与交付事实；只推进下一步，必要操作仍提出建议等待人工确认。不得把回复、审批、Run成功、提交和任务验收混为一谈。若没有可推进事项，明确说明并停止，不重写同一计划冒充进展。不扩大原目标；若确实缺少下一步必需的工作台范围，只能用 workspace_request_access 留下待人工核对请求，然后停止本轮。该请求不授予权限，不能继续调用缺失工具或自行发送新消息。不使用或复制旧 opaque 候选。", row.TurnsStarted+1, row.MaxTurns)}
		result, runErr := r.a.runAIGeneration(ctx, input, options, nil)
		accepted = result.Accepted
		r.finish(row.ID, generationID, result, runErr)
	}()
}

func (r *aiContinuationCoordinator) finish(id, generationID string, result aiGenerationRunResult, runErr error) {
	r.a.maintenance.RLock()
	defer r.a.maintenance.RUnlock()
	if r.a.restorePending.Load() {
		return
	}
	err := r.a.db.Transaction(func(tx *gorm.DB) error {
		var row models.AIContinuation
		if err := tx.Take(&row, "id=?", id).Error; err != nil {
			return err
		}
		if !continuationActive(row.Status) {
			// Stop revokes immediately; clear only this worker's active marker
			// after its accepted generation has actually returned.
			if row.CurrentGenerationID != nil && *row.CurrentGenerationID == generationID {
				return tx.Model(&models.AIContinuation{}).Where("id=? AND version=?", row.ID, row.Version).Updates(map[string]any{"current_generation_id": nil, "version": row.Version + 1, "updated_at": r.a.options.Now().UTC().Format(time.RFC3339Nano)}).Error
			}
			return nil
		}
		now := r.a.options.Now().UTC()
		settle := func(status, reason string) error {
			if err := updateAIContinuation(tx, &row, status, reason, now); err != nil {
				return err
			}
			return tx.Model(&models.AIContinuation{}).Where("id=? AND version=?", row.ID, row.Version).Updates(map[string]any{"current_generation_id": nil, "version": row.Version + 1, "updated_at": now.Format(time.RFC3339Nano)}).Error
		}
		if r.ctx.Err() != nil {
			return settle("interrupted", "shutdown")
		}
		expires, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
		if err != nil {
			return err
		}
		if !now.Before(expires) {
			return settle("expired", "time_limit")
		}
		if !result.Accepted {
			var failure *aiGenerationRunError
			if errors.As(runErr, &failure) && (failure.Code == "AI_PROVIDER_BUSY" || failure.Code == "AI_SESSION_BUSY" || failure.Code == "AI_GENERATION_IN_PROGRESS") {
				return updateAIContinuation(tx, &row, "waiting", "provider_busy", now)
			}
			// A race detected in the acceptance gate can be re-observed safely:
			// no generation was accepted and no model request was sent.
			var requestErr *aiBusinessContextRequestError
			if errors.As(runErr, &requestErr) && requestErr.code == "AI_CONTINUATION_CHANGED" {
				return nil
			}
			return updateAIContinuation(tx, &row, "failed", "generation_failed", now)
		}
		if row.CurrentGenerationID == nil || *row.CurrentGenerationID != generationID {
			return aiContinuationRequestError("AI_CONTINUATION_CHANGED", "Generation ownership changed")
		}
		var gen models.AIGeneration
		if err := tx.Take(&gen, "id=?", generationID).Error; err != nil {
			return err
		}
		status, reason := "failed", "generation_failed"
		switch gen.Status {
		case "completed":
			status, reason = "waiting", "ready"
		case "cancelled":
			status, reason = "stopped", "generation_cancelled"
		case "queued", "streaming":
			status, reason = "interrupted", "evidence_unavailable"
		}
		return settle(status, reason)
	})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.a.options.Logger.Print("AI continuation result could not be recorded; it will not be replayed")
	}
}
