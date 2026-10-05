package api

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// ADR-030: a human confirms one complete Run whose start waits for a single
// predecessor Run. The Sidecar only decides WHEN that frozen Run may enter
// the ordinary FIFO; it never creates, alters or re-authorizes a Run.
const (
	agentRunStartGatedEvent         = "agent_run_start_gated"
	agentRunStartGateReleasedEvent  = "agent_run_start_gate_released"
	agentRunStartGateClosedEvent    = "agent_run_start_gate_closed"
	agentRunStartAfterSubmitted     = "submitted"
	agentRunStartAfterAccepted      = "accepted"
	agentRunStartGateMaxOpen        = 4
	agentRunStartGateMaxHours       = 24
	agentRunStartGateDefaultScan    = 30 * time.Second
	agentRunStartGateWaiting        = "waiting"
	agentRunStartGateReleased       = "released"
	agentRunStartGateClosed         = "closed"
	agentRunStartGateInvalidCode    = "AGENT_RUN_START_GATE_INVALID"
	agentRunStartGateCapacityCode   = "AGENT_RUN_START_GATE_CAPACITY"
	agentRunStartGateNotNeededCode  = "AGENT_RUN_START_GATE_NOT_NEEDED"
	agentRunStartGateImpossibleCode = "AGENT_RUN_START_GATE_UNSATISFIABLE"
)

var agentRunStartGateCloseReasons = map[string]struct{}{
	"predecessor_failed": {}, "predecessor_cancelled": {}, "predecessor_retained": {},
	"predecessor_not_accepted": {}, "predecessor_unavailable": {}, "expired": {}, "run_not_queued": {},
	agentRunStartGateOutputUnavailable: {},
}

// SQL fragment for a row of `agent_runs` whose start gate is still open.
const agentRunOpenStartGateFilter = `EXISTS (
	SELECT 1 FROM workflow_events AS gate
	WHERE gate.aggregate_type = 'agent_run' AND gate.aggregate_id = agent_runs.id
		AND gate.action = 'agent_run_start_gated'
		AND NOT EXISTS (
			SELECT 1 FROM workflow_events AS settled
			WHERE settled.aggregate_type = 'agent_run' AND settled.aggregate_id = agent_runs.id
				AND settled.action IN ('agent_run_start_gate_released', 'agent_run_start_gate_closed')
		)
)`

var errAgentRunStartGateOpen = errors.New("agent run start gate is still waiting")

type agentRunStartGatePayload struct {
	PredecessorRunID string `json:"predecessor_run_id"`
	Require          string `json:"require"`
	ExpiresAt        string `json:"expires_at"`
	// InputSlot repeats the v7 declaration of the gated Run (ADR-033).
	InputSlot *agentRunDeferredInputs `json:"input_slot,omitempty"`
}

type agentRunStartGateEventRow struct {
	AggregateID string `gorm:"column:aggregate_id"`
	Action      string `gorm:"column:action"`
	CurrentJSON string `gorm:"column:current_json"`
}

func validAgentRunStartAfter(value string) bool {
	return value == agentRunStartAfterSubmitted || value == agentRunStartAfterAccepted
}

// readAgentRunStartGates projects the gate of each Run. A malformed or
// contradictory event history is an error: callers fail closed rather than
// treating the Run as ungated.
func readAgentRunStartGates(tx *gorm.DB, runIDs []string) (map[string]*models.AgentRunStartGate, error) {
	gates := make(map[string]*models.AgentRunStartGate, len(runIDs))
	slots := make(map[string]agentRunDeferredInputs)
	if len(runIDs) == 0 {
		return gates, nil
	}
	var rows []agentRunStartGateEventRow
	if err := tx.Table("workflow_events").Select("aggregate_id, action, current_json").
		Where("aggregate_type = 'agent_run' AND aggregate_id IN ? AND action IN ?", runIDs,
			[]string{agentRunStartGatedEvent, agentRunStartGateReleasedEvent, agentRunStartGateClosedEvent}).
		Order("created_at").Order("id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	// Settlement events may share the opening timestamp, so every gate is
	// opened first; ordering between settlements still rejects duplicates.
	for _, row := range rows {
		if row.Action != agentRunStartGatedEvent {
			continue
		}
		var payload agentRunStartGatePayload
		if gates[row.AggregateID] != nil || decodeStrictJSONBytes([]byte(row.CurrentJSON), &payload) != nil ||
			!validCanonicalAgentRunID(payload.PredecessorRunID) || !validAgentRunStartAfter(payload.Require) {
			return nil, errors.New("invalid agent run start gate")
		}
		expires, err := time.Parse(time.RFC3339Nano, payload.ExpiresAt)
		if err != nil {
			return nil, errors.New("invalid agent run start gate expiry")
		}
		gate := &models.AgentRunStartGate{
			PredecessorRunID: payload.PredecessorRunID, Require: payload.Require,
			ExpiresAt: expires.UTC().Format(time.RFC3339Nano), Status: agentRunStartGateWaiting,
		}
		if payload.InputSlot != nil {
			if !validAgentRunDeferredInputs(*payload.InputSlot) || payload.InputSlot.PredecessorRunID != payload.PredecessorRunID ||
				payload.Require != agentRunStartAfterAccepted {
				return nil, errors.New("invalid agent run start gate input slot")
			}
			slots[row.AggregateID] = *payload.InputSlot
			gate.InputSlot = &models.AgentRunStartGateInputSlot{
				Source: payload.InputSlot.Source, Status: agentRunInputSlotAwaitingPredecessor,
				ExpectedFiles: make([]models.AgentRunExpectedFile, len(payload.InputSlot.ExpectedFiles)),
			}
			for index, file := range payload.InputSlot.ExpectedFiles {
				gate.InputSlot.ExpectedFiles[index] = models.AgentRunExpectedFile{Name: file.Name, MIME: file.MIME}
			}
		}
		gates[row.AggregateID] = gate
	}
	for _, row := range rows {
		gate := gates[row.AggregateID]
		switch row.Action {
		case agentRunStartGateReleasedEvent:
			if gate == nil || gate.Status != agentRunStartGateWaiting {
				return nil, errors.New("invalid agent run start gate release")
			}
			if gate.InputSlot != nil {
				// Only a recorded human confirmation releases a gate with a slot.
				if _, err := decodeAgentRunStartGateBinding(row.CurrentJSON, slots[row.AggregateID]); err != nil {
					return nil, err
				}
				gate.InputSlot.Status = agentRunInputSlotBound
			}
			gate.Status = agentRunStartGateReleased
		case agentRunStartGateClosedEvent:
			var payload struct {
				Reason string `json:"reason"`
			}
			if gate == nil || gate.Status != agentRunStartGateWaiting || decodeStrictJSONBytes([]byte(row.CurrentJSON), &payload) != nil {
				return nil, errors.New("invalid agent run start gate closure")
			}
			if _, ok := agentRunStartGateCloseReasons[payload.Reason]; !ok {
				return nil, errors.New("invalid agent run start gate closure reason")
			}
			reason := payload.Reason
			gate.Status, gate.CloseReason = agentRunStartGateClosed, &reason
			if gate.InputSlot != nil {
				gate.InputSlot.Status = agentRunInputSlotClosed
			}
		}
	}
	// A waiting slot is either still waiting for the predecessor to be
	// accepted, or waiting for a person to confirm the accepted files.
	for _, gate := range gates {
		if gate.InputSlot == nil || gate.Status != agentRunStartGateWaiting {
			continue
		}
		predecessor, submissionStatus, err := readAgentRunStartGatePredecessor(tx, gate.PredecessorRunID)
		if err != nil {
			return nil, err
		}
		if decideAgentRunStartGate(predecessor, submissionStatus, gate.Require) == "release" {
			gate.InputSlot.Status = agentRunInputSlotAwaitingConfirmation
		}
	}
	return gates, nil
}

func validCanonicalAgentRunID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func projectAgentRunStartGates(tx *gorm.DB, runs []models.AgentRun) error {
	ids := make([]string, len(runs))
	for index, run := range runs {
		ids[index] = run.ID
	}
	gates, err := readAgentRunStartGates(tx, ids)
	if err != nil {
		return err
	}
	for index := range runs {
		runs[index].StartGate = gates[runs[index].ID]
	}
	return nil
}

func projectAgentRunStartGate(tx *gorm.DB, run *models.AgentRun) error {
	runs := []models.AgentRun{*run}
	if err := projectAgentRunStartGates(tx, runs); err != nil {
		return err
	}
	run.StartGate = runs[0].StartGate
	return nil
}

// decideAgentRunStartGate is the only place that interprets predecessor facts.
// It returns "wait", "release" or a closure reason.
func decideAgentRunStartGate(predecessor *models.AgentRun, submissionStatus *string, require string) string {
	if predecessor == nil {
		return "predecessor_unavailable"
	}
	switch {
	case predecessor.Status == "queued" && predecessor.OutputDeliveryStatus == agentRunOutputNotReady,
		predecessor.Status == "running" && (predecessor.OutputDeliveryStatus == agentRunOutputNotReady ||
			predecessor.OutputDeliveryStatus == agentRunOutputPending):
		return "wait"
	case predecessor.Status == "succeeded" && predecessor.OutputDeliveryStatus == agentRunOutputRetained:
		return "predecessor_retained"
	case predecessor.Status == "succeeded" && predecessor.OutputDeliveryStatus == agentRunOutputSubmitted:
		if submissionStatus == nil || !validAgentRunSubmissionStatus(*submissionStatus) {
			return "predecessor_unavailable"
		}
		if require == agentRunStartAfterSubmitted {
			return "release"
		}
		switch *submissionStatus {
		case "accepted":
			return "release"
		case "pending_review":
			return "wait"
		default:
			return "predecessor_not_accepted"
		}
	case (predecessor.Status == "failed" || predecessor.Status == "interrupted") && predecessor.OutputDeliveryStatus == agentRunOutputNotReady:
		return "predecessor_failed"
	case predecessor.Status == "cancelled" && predecessor.OutputDeliveryStatus == agentRunOutputNotReady:
		return "predecessor_cancelled"
	default:
		return "predecessor_unavailable"
	}
}

func readAgentRunStartGatePredecessor(tx *gorm.DB, runID string) (*models.AgentRun, *string, error) {
	var run models.AgentRun
	err := tx.Select("id, task_id, status, output_delivery_status, submission_id").Take(&run, "id = ?", runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	status, err := readAgentRunSubmissionStatus(tx, run)
	if err != nil {
		return nil, nil, err
	}
	return &run, status, nil
}

// validateAgentRunStartGateRequest is shared by the proposal preview and the
// confirmation transaction: a gate that is already satisfied, impossible or
// over budget is refused before any Run exists.
func validateAgentRunStartGateRequest(tx *gorm.DB, predecessorRunID, require string, hours int, targetTaskID string, slot *agentRunDeferredInputs) (*models.AgentRun, error) {
	if !validCanonicalAgentRunID(predecessorRunID) || !validAgentRunStartAfter(require) || hours < 1 || hours > agentRunStartGateMaxHours {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, agentRunStartGateInvalidCode,
			"A start gate needs an exact predecessor Run, start_after submitted or accepted, and 1-24 hours")
	}
	predecessor, submissionStatus, err := readAgentRunStartGatePredecessor(tx, predecessorRunID)
	if err != nil {
		return nil, err
	}
	if predecessor != nil && predecessor.TaskID == targetTaskID {
		return nil, newProjectRequestError(http.StatusUnprocessableEntity, agentRunStartGateInvalidCode,
			"The predecessor Run must belong to a different Task")
	}
	if slot != nil {
		// The declaration is derived again from the predecessor on every
		// preview and at confirmation; one that no longer matches is refused.
		var targetTask models.Task
		if err := tx.Select("id", "project_id").Take(&targetTask, "id = ?", targetTaskID).Error; err != nil {
			return nil, err
		}
		current, err := agentRunDeferredInputsFor(tx, targetTask, predecessorRunID, require)
		if err != nil {
			return nil, err
		}
		if !sameAgentRunDeferredInputs(current, *slot) {
			return nil, agentRunDeferredInputsError("The output contract of the predecessor Run changed; propose the start again")
		}
	}
	switch decision := decideAgentRunStartGate(predecessor, submissionStatus, require); decision {
	case "wait":
	case "release":
		return nil, newProjectRequestError(http.StatusConflict, agentRunStartGateNotNeededCode,
			"The predecessor condition is already met; propose an ordinary start instead")
	default:
		return nil, newProjectRequestError(http.StatusConflict, agentRunStartGateImpossibleCode,
			"The predecessor Run can no longer meet this condition ("+decision+")")
	}
	var open int64
	if err := tx.Model(&models.AgentRun{}).Where(agentRunOpenStartGateFilter).Count(&open).Error; err != nil {
		return nil, err
	}
	if open >= agentRunStartGateMaxOpen {
		return nil, newProjectRequestError(http.StatusConflict, agentRunStartGateCapacityCode,
			"At most four gated Agent Runs may wait at the same time")
	}
	return predecessor, nil
}

// openAgentRunStartGate must run in the transaction that created the queued
// Run, so the Run is never visible to the dispatcher without its gate.
func openAgentRunStartGate(tx *gorm.DB, run models.AgentRun, predecessorRunID, require string, hours int, now time.Time, slot *agentRunDeferredInputs) error {
	if run.Status != "queued" || run.OutputDeliveryStatus != agentRunOutputNotReady {
		return errors.New("only a newly queued Run can be gated")
	}
	// A slot and a v7 Run exist only together, and say the same thing.
	if (slot != nil) != (run.ExecutionContractVersion == agentRunExecutionContractVersionV7) {
		return errors.New("a start gate input slot requires a v7 Run")
	}
	if slot != nil {
		snapshot, err := parseAgentRunV7Snapshot(run.InputSnapshotJSON)
		if err != nil || !sameAgentRunDeferredInputs(*snapshot.DeferredInputs, *slot) {
			return errors.New("the start gate input slot does not match the Run")
		}
	}
	if _, err := validateAgentRunStartGateRequest(tx, predecessorRunID, require, hours, run.TaskID, slot); err != nil {
		return err
	}
	at := now.UTC()
	payload := map[string]any{
		"predecessor_run_id": predecessorRunID, "require": require,
		"expires_at": at.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339Nano),
	}
	if slot != nil {
		payload["input_slot"] = slot
	}
	return recordAgentRunWorkflowEvent(tx, agentRunStartGatedEvent, run.ID, payload, "", at.Format(time.RFC3339Nano))
}

type agentRunStartGateCoordinator struct {
	a      *API
	mu     sync.Mutex // serializes reconciliation within this Sidecar
	cancel func()
	wakeup chan struct{}
	done   chan struct{}
}

func newAgentRunStartGateCoordinator(a *API, interval time.Duration) *agentRunStartGateCoordinator {
	stop := make(chan struct{})
	r := &agentRunStartGateCoordinator{a: a, wakeup: make(chan struct{}, 1), done: make(chan struct{})}
	var once sync.Once
	r.cancel = func() { once.Do(func() { close(stop) }) }
	go func() {
		defer close(r.done)
		var tick <-chan time.Time
		if interval > 0 {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			tick = ticker.C
		}
		for {
			select {
			case <-stop:
				return
			case <-tick:
			case <-r.wakeup:
			}
			_, _ = a.reconcileAgentRunStartGates(a.options.Now())
		}
	}()
	return r
}

func (r *agentRunStartGateCoordinator) wake() {
	if r == nil {
		return
	}
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *agentRunStartGateCoordinator) close() {
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

type agentRunStartGateOutcome struct {
	Released []string
	Closed   map[string]string
}

// reconcileAgentRunStartGates settles every open gate against authoritative
// facts. Release only re-enables the ordinary dispatcher; closure cancels a
// still-queued Run through the existing cancellation command.
func (a *API) reconcileAgentRunStartGates(now time.Time) (agentRunStartGateOutcome, error) {
	outcome := agentRunStartGateOutcome{Closed: map[string]string{}}
	if a.agentRunGates != nil {
		a.agentRunGates.mu.Lock()
		defer a.agentRunGates.mu.Unlock()
	}
	at := now.UTC()
	stamp := at.Format(time.RFC3339Nano)
	err := a.withAgentRunStorageLease(func() error {
		return a.db.Transaction(func(tx *gorm.DB) error {
			var gatedIDs []string
			if err := tx.Model(&models.AgentRun{}).Where(agentRunOpenStartGateFilter).
				Order("created_at").Order("id").Pluck("id", &gatedIDs).Error; err != nil {
				return err
			}
			gates, err := readAgentRunStartGates(tx, gatedIDs)
			if err != nil {
				return err
			}
			for _, runID := range gatedIDs {
				gate := gates[runID]
				if gate == nil || gate.Status != agentRunStartGateWaiting {
					return errors.New("agent run start gate projection changed")
				}
				var run models.AgentRun
				if err := tx.Select("id, task_id, status, output_delivery_status, execution_contract_version, input_snapshot_json").
					Take(&run, "id = ?", runID).Error; err != nil {
					return err
				}
				decision := "run_not_queued"
				if run.Status == "queued" && run.OutputDeliveryStatus == agentRunOutputNotReady {
					expires, _ := time.Parse(time.RFC3339Nano, gate.ExpiresAt)
					if !at.Before(expires) {
						decision = "expired"
					} else {
						predecessor, submissionStatus, err := readAgentRunStartGatePredecessor(tx, gate.PredecessorRunID)
						if err != nil {
							return err
						}
						decision = decideAgentRunStartGate(predecessor, submissionStatus, gate.Require)
						if decision == "release" && gate.InputSlot != nil {
							// The condition is met, but the files still need a
							// person. Keep waiting unless they are already gone.
							if decision, err = decideAgentRunInputSlot(tx, run); err != nil {
								return err
							}
						}
					}
				}
				switch decision {
				case "wait":
					continue
				case "release":
					if err := recordAgentRunWorkflowEvent(tx, agentRunStartGateReleasedEvent, runID,
						map[string]any{"predecessor_run_id": gate.PredecessorRunID}, "", stamp); err != nil {
						return err
					}
					outcome.Released = append(outcome.Released, runID)
				default:
					if err := recordAgentRunWorkflowEvent(tx, agentRunStartGateClosedEvent, runID,
						map[string]any{"reason": decision}, "", stamp); err != nil {
						return err
					}
					if decision != "run_not_queued" {
						if err := requestAgentRunCancellation(tx, runID, "", stamp); err != nil {
							return err
						}
					}
					outcome.Closed[runID] = decision
				}
			}
			return nil
		})
	})
	if errors.Is(err, errAgentRunRestorePending) {
		return agentRunStartGateOutcome{Closed: map[string]string{}}, nil
	}
	if err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Printf("Agent Run start gate reconciliation deferred after storage failure")
		}
		return agentRunStartGateOutcome{Closed: map[string]string{}}, err
	}
	if len(outcome.Released) > 0 {
		a.launchQueuedAgentRuns()
	}
	if len(outcome.Released) > 0 || len(outcome.Closed) > 0 {
		// Only the continuation coordinator: waking gates here would loop.
		if a.aiContinuations != nil {
			a.aiContinuations.wake()
		}
	}
	return outcome, nil
}

// decideAgentRunInputSlot is reached once the predecessor is accepted. It
// reads metadata only: "wait" while the accepted files are still valid inputs
// for the gated Run, otherwise the reason to close the gate.
func decideAgentRunInputSlot(tx *gorm.DB, run models.AgentRun) (string, error) {
	snapshot, err := parseAgentRunV7Snapshot(run.InputSnapshotJSON)
	if err != nil {
		return agentRunStartGateOutputUnavailable, nil
	}
	var task models.Task
	if err := tx.Select("id", "project_id").Take(&task, "id = ?", run.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return agentRunStartGateOutputUnavailable, nil
		}
		return "", err
	}
	if _, err := loadAgentRunPredecessorOutputRecords(tx, task, *snapshot.DeferredInputs); err != nil {
		var requestErr *projectRequestError
		if errors.As(err, &requestErr) {
			return agentRunStartGateOutputUnavailable, nil
		}
		return "", err
	}
	return "wait", nil
}
