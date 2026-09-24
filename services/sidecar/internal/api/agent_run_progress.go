package api

import (
	"sync"
	"time"
)

type agentRunProgressPhase string

const (
	agentRunProgressPreparing         agentRunProgressPhase = "preparing"
	agentRunProgressCallingModel      agentRunProgressPhase = "calling_model"
	agentRunProgressRegisteringResult agentRunProgressPhase = "registering_result"
	agentRunProgressLimit                                   = 256
)

// agentRunProgress is transient, content-free UI metadata. It is deliberately
// not persisted and is never included in AI workspace-tool results.
type agentRunProgress struct {
	Phase     agentRunProgressPhase `json:"phase"`
	ElapsedMS int64                 `json:"elapsed_ms"`
}

type agentRunProgressState struct {
	phase     agentRunProgressPhase
	startedAt time.Time
}

type agentRunProgressRegistry struct {
	mu     sync.RWMutex
	active map[string]agentRunProgressState
}

func newAgentRunProgressRegistry() *agentRunProgressRegistry {
	return &agentRunProgressRegistry{active: make(map[string]agentRunProgressState)}
}

func agentRunProgressPhaseOrder(phase agentRunProgressPhase) int {
	switch phase {
	case agentRunProgressPreparing:
		return 1
	case agentRunProgressCallingModel:
		return 2
	case agentRunProgressRegisteringResult:
		return 3
	default:
		return 0
	}
}

func (r *agentRunProgressRegistry) start(runID string, now time.Time) bool {
	if r == nil || runID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.active[runID]; exists || len(r.active) >= agentRunProgressLimit {
		return false
	}
	r.active[runID] = agentRunProgressState{phase: agentRunProgressPreparing, startedAt: now}
	return true
}

func (r *agentRunProgressRegistry) advance(runID string, phase agentRunProgressPhase) bool {
	if r == nil {
		return false
	}
	order := agentRunProgressPhaseOrder(phase)
	if order == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, exists := r.active[runID]
	if !exists || order < agentRunProgressPhaseOrder(state.phase) {
		return false
	}
	state.phase = phase
	r.active[runID] = state
	return true
}

func (r *agentRunProgressRegistry) snapshot(runID string, now time.Time) (agentRunProgress, bool) {
	if r == nil {
		return agentRunProgress{}, false
	}
	r.mu.RLock()
	state, exists := r.active[runID]
	r.mu.RUnlock()
	if !exists {
		return agentRunProgress{}, false
	}
	elapsed := now.Sub(state.startedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return agentRunProgress{Phase: state.phase, ElapsedMS: elapsed}, true
}

func (r *agentRunProgressRegistry) remove(runID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.active, runID)
	r.mu.Unlock()
}

func (a *API) agentRunProgressAt(runID, status string, now time.Time) *agentRunProgress {
	if a == nil || status != "running" || a.agentRunProgress == nil {
		return nil
	}
	progress, ok := a.agentRunProgress.snapshot(runID, now)
	if !ok {
		return nil
	}
	return &progress
}

func (a *API) agentRunProgressFor(runID, status string) *agentRunProgress {
	if a == nil {
		return nil
	}
	now := time.Now()
	if a.options.Now != nil {
		now = a.options.Now()
	}
	return a.agentRunProgressAt(runID, status, now)
}

func (a *API) advanceAgentRunProgress(runID string, phase agentRunProgressPhase) {
	if a.agentRunProgress != nil {
		a.agentRunProgress.advance(runID, phase)
	}
}
