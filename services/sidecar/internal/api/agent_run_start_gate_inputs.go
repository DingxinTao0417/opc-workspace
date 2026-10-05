package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The second human confirmation of ADR-033. Reading shows exactly what would
// be sent; confirming records those files as the gate's release. Neither is
// reachable by a model: these are native endpoints only.

type agentRunStartGateInputsPredecessor struct {
	RunID     string `json:"run_id"`
	TaskID    string `json:"task_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
}

type agentRunStartGateInputsProvider struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Model         string `json:"model"`
	Version       int64  `json:"version"`
	ConfigVersion int64  `json:"config_version"`
	LeavesDevice  bool   `json:"leaves_device"`
}

type agentRunStartGateInputsLimits struct {
	MaxFiles      int `json:"max_files"`
	MaxFileBytes  int `json:"max_file_bytes"`
	MaxTotalBytes int `json:"max_total_bytes"`
}

type agentRunStartGateInputsResponse struct {
	RunID         string                             `json:"run_id"`
	TaskID        string                             `json:"task_id"`
	Status        string                             `json:"status"`
	CloseReason   *string                            `json:"close_reason,omitempty"`
	ExpiresAt     string                             `json:"expires_at"`
	Predecessor   agentRunStartGateInputsPredecessor `json:"predecessor"`
	ExpectedFiles []agentexec.OutputFileContract     `json:"expected_files"`
	Provider      agentRunStartGateInputsProvider    `json:"provider"`
	Files         []frozenAgentRunFileReference      `json:"files"`
	TotalBytes    int                                `json:"total_bytes"`
	Limits        agentRunStartGateInputsLimits      `json:"limits"`
	// Fingerprint names the exact files and Provider shown. It is present
	// only when there is something to confirm or something was confirmed.
	Fingerprint string `json:"fingerprint,omitempty"`
}

type agentRunStartGateInputsState struct {
	run      models.AgentRun
	task     models.Task
	slot     agentRunDeferredInputs
	provider agentRunFileAccessProviderConfirmation
	response agentRunStartGateInputsResponse
}

func agentRunStartGateInputsFingerprint(
	runID string,
	slot agentRunDeferredInputs,
	files []frozenAgentRunFileReference,
	provider agentRunFileAccessProviderConfirmation,
) string {
	encoded, _ := json.Marshal(struct {
		RunID            string                                 `json:"run_id"`
		PredecessorRunID string                                 `json:"predecessor_run_id"`
		Files            []frozenAgentRunFileReference          `json:"files"`
		Provider         agentRunFileAccessProviderConfirmation `json:"provider"`
	}{runID, slot.PredecessorRunID, files, provider})
	return sha256Hex(encoded)
}

func agentRunInputNotReady(message string) error {
	return newProjectRequestError(http.StatusConflict, agentRunInputNotReadyCode, message)
}

// readAgentRunStartGateInputs resolves what the slot of one v7 Run currently
// holds. While the gate waits for confirmation it reads the candidate files
// and verifies their hashes, so the fingerprint covers bytes that exist.
func (a *API) readAgentRunStartGateInputs(tx *gorm.DB, runID string, now time.Time) (agentRunStartGateInputsState, error) {
	var state agentRunStartGateInputsState
	if err := tx.First(&state.run, "id = ?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, newProjectRequestError(http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Agent run not found")
		}
		return state, err
	}
	if state.run.ExecutionContractVersion != agentRunExecutionContractVersionV7 {
		return state, newProjectRequestError(http.StatusNotFound, agentRunInputSlotNotFoundCode,
			"This Agent run does not wait for predecessor files")
	}
	snapshot, err := parseAgentRunV7Snapshot(state.run.InputSnapshotJSON)
	if err != nil {
		return state, err
	}
	state.slot = *snapshot.DeferredInputs
	gates, err := readAgentRunStartGates(tx, []string{runID})
	if err != nil {
		return state, err
	}
	gate := gates[runID]
	if gate == nil || gate.InputSlot == nil {
		return state, agentRunIdentityChangedError()
	}
	if err := tx.First(&state.task, "id = ?", state.run.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, agentRunIdentityChangedError()
		}
		return state, err
	}
	var provider models.AIProvider
	if err := tx.First(&provider, "id = ?", state.run.ProviderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, agentRunIdentityChangedError()
		}
		return state, err
	}
	state.provider = agentRunFileAccessProviderConfirmation{
		Version: provider.Version, ConfigVersion: provider.ConfigVersion, Kind: provider.Kind,
	}
	state.response = agentRunStartGateInputsResponse{
		RunID: state.run.ID, TaskID: state.run.TaskID, ExpiresAt: gate.ExpiresAt, CloseReason: gate.CloseReason,
		Predecessor:   agentRunStartGateInputsPredecessor{RunID: state.slot.PredecessorRunID},
		ExpectedFiles: state.slot.ExpectedFiles,
		Provider: agentRunStartGateInputsProvider{
			ID: provider.ID, Name: provider.Name, Kind: provider.Kind, Model: provider.Model,
			Version: provider.Version, ConfigVersion: provider.ConfigVersion, LeavesDevice: provider.Kind == "remote",
		},
		Files: []frozenAgentRunFileReference{},
		Limits: agentRunStartGateInputsLimits{
			MaxFiles: agentexec.MaxFileInputs, MaxFileBytes: agentexec.MaxFileInputBytes,
			MaxTotalBytes: agentexec.MaxFileInputsTotalBytes,
		},
	}
	var identity struct {
		TaskID    string `gorm:"column:task_id"`
		Attempt   int    `gorm:"column:attempt"`
		TaskTitle string `gorm:"column:task_title"`
	}
	err = tx.Table("agent_runs AS run").Select("run.task_id, run.attempt, task.title AS task_title").
		Joins("JOIN tasks AS task ON task.id = run.task_id").
		Where("run.id = ?", state.slot.PredecessorRunID).Take(&identity).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}
	if err == nil {
		state.response.Predecessor.TaskID, state.response.Predecessor.TaskTitle = identity.TaskID, identity.TaskTitle
		state.response.Predecessor.Attempt = identity.Attempt
	}

	setFiles := func(files []frozenAgentRunFileReference, provider agentRunFileAccessProviderConfirmation) {
		state.response.Files = files
		for _, file := range files {
			state.response.TotalBytes += file.SizeBytes
		}
		state.response.Fingerprint = agentRunStartGateInputsFingerprint(state.run.ID, state.slot, files, provider)
	}
	switch gate.Status {
	case agentRunStartGateClosed:
		state.response.Status = agentRunInputSlotClosed
		return state, nil
	case agentRunStartGateReleased:
		binding, err := loadAgentRunStartGateBinding(tx, runID, state.slot)
		if err != nil || binding == nil {
			return state, agentRunIdentityChangedError()
		}
		state.response.Status = agentRunInputSlotBound
		setFiles(binding.InputFiles, binding.Provider)
		return state, nil
	}
	if state.run.Status != "queued" || state.run.OutputDeliveryStatus != agentRunOutputNotReady {
		return state, agentRunInputNotReady("The waiting Agent run is no longer queued")
	}
	expires, err := time.Parse(time.RFC3339Nano, gate.ExpiresAt)
	if err != nil || !now.UTC().Before(expires) {
		return state, agentRunInputNotReady("The waiting period ended before the predecessor files were confirmed")
	}
	predecessor, submissionStatus, err := readAgentRunStartGatePredecessor(tx, state.slot.PredecessorRunID)
	if err != nil {
		return state, err
	}
	switch decision := decideAgentRunStartGate(predecessor, submissionStatus, agentRunStartAfterAccepted); decision {
	case "wait":
		state.response.Status = agentRunInputSlotAwaitingPredecessor
		return state, nil
	case "release":
	default:
		return state, agentRunInputNotReady("The predecessor can no longer provide accepted files (" + decision + ")")
	}
	records, err := loadAgentRunPredecessorOutputRecords(tx, state.task, state.slot)
	if err != nil {
		return state, err
	}
	requests := make([]agentRunInputFileRequest, len(records))
	for index, record := range records {
		requests[index] = agentRunInputRequestFromReference(record.Reference)
	}
	references, _, err := freezeAgentRunFiles(tx, state.task, requests, a.artifactStore)
	if err != nil {
		var requestErr *projectRequestError
		if errors.As(err, &requestErr) && requestErr.code != agentRunFileStorageUnavailableCode {
			return state, agentRunInputUnavailable()
		}
		return state, err
	}
	if !agentRunFilesSatisfySlot(references, state.slot) {
		return state, agentRunInputUnavailable()
	}
	state.response.Status = agentRunInputSlotAwaitingConfirmation
	setFiles(references, state.provider)
	return state, nil
}

// lockAgentRunStartGates serializes gate settlement with the reconciler. An
// API without a coordinator (as some tests build it) has nothing to wait for.
func (a *API) lockAgentRunStartGates() func() {
	if a.agentRunGates == nil {
		return func() {}
	}
	a.agentRunGates.mu.Lock()
	return a.agentRunGates.mu.Unlock
}

// wakeAgentRunGatesAfterStaleInputs lets the reconciler close a gate whose
// inputs turned out to be gone or overdue while a person was looking at it.
func (a *API) wakeAgentRunGatesAfterStaleInputs(err error) {
	var requestErr *projectRequestError
	if errors.As(err, &requestErr) && (requestErr.code == agentRunInputUnavailableCode || requestErr.code == agentRunInputNotReadyCode) {
		a.agentRunGates.wake()
	}
}

func (a *API) getAgentRunStartGateInputs(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	var state agentRunStartGateInputsState
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = a.readAgentRunStartGateInputs(tx, runID, a.options.Now())
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		a.wakeAgentRunGatesAfterStaleInputs(err)
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": state.response})
}

type confirmAgentRunStartGateInputsRequest struct {
	ConfirmFileAccess              bool                                    `json:"confirm_file_access"`
	ConfirmProjectTaskFiles        bool                                    `json:"confirm_project_task_files"`
	FileAccessProviderConfirmation *agentRunFileAccessProviderConfirmation `json:"file_access_provider_confirmation"`
	Fingerprint                    string                                  `json:"fingerprint"`
}

func (a *API) confirmAgentRunStartGateInputs(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	var input confirmAgentRunStartGateInputsRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !input.ConfirmFileAccess || !input.ConfirmProjectTaskFiles || input.FileAccessProviderConfirmation == nil {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED",
			"Confirm sending the listed accepted predecessor files to the exact Provider shown")
		return
	}
	if !input.FileAccessProviderConfirmation.valid() || len(input.Fingerprint) != 64 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR",
			"file_access_provider_confirmation and the preview fingerprint are required")
		return
	}
	now := a.options.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	var state agentRunStartGateInputsState
	replayed := false
	// The reconciler settles gates too; one settlement per gate.
	unlockGates := a.lockAgentRunStartGates()
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var err error
		state, err = a.readAgentRunStartGateInputs(tx, runID, now)
		if err != nil {
			return err
		}
		switch state.response.Status {
		case agentRunInputSlotBound:
			// The same confirmation again changes nothing.
			if state.response.Fingerprint != input.Fingerprint {
				return newProjectRequestError(http.StatusConflict, agentRunInputChangedCode,
					"Different files were already confirmed for this Agent run")
			}
			replayed = true
			return nil
		case agentRunInputSlotAwaitingConfirmation:
		case agentRunInputSlotAwaitingPredecessor:
			return agentRunInputNotReady("The predecessor's files have not been accepted yet")
		default:
			return agentRunInputNotReady("The start condition of this Agent run is already closed")
		}
		if state.response.Fingerprint != input.Fingerprint {
			return newProjectRequestError(http.StatusConflict, agentRunInputChangedCode,
				"The files or the Provider changed after they were shown; review them again")
		}
		if !input.FileAccessProviderConfirmation.matches(models.AIProvider{
			Version: state.provider.Version, ConfigVersion: state.provider.ConfigVersion, Kind: state.provider.Kind,
		}) {
			return newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The Agent Provider changed after file access was confirmed; review and confirm the Provider again")
		}
		var adapter models.AgentAdapter
		if err := tx.First(&adapter, "id = ?", state.run.AdapterID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agentRunIdentityChangedError()
			}
			return err
		}
		if err := validateAgentRunAdapter(adapter); err != nil {
			return err
		}
		binding := agentRunStartGateBinding{
			PredecessorRunID: state.slot.PredecessorRunID, InputFiles: state.response.Files,
			Provider: state.provider, ConfirmedBy: models.BuiltinOwnerActorID,
		}
		// The same check the worker repeats at claim and before launch: the
		// Run's frozen identity still holds and every byte matches its hash.
		if _, err := validateFrozenAgentRunIdentityWithBinding(tx, state.run, a.artifactStore, &binding); err != nil {
			return err
		}
		if err := recordAgentRunWorkflowEvent(tx, agentRunStartGateReleasedEvent, runID, map[string]any{
			"predecessor_run_id": binding.PredecessorRunID, "input_files": binding.InputFiles,
			"provider": binding.Provider, "confirmed_by": binding.ConfirmedBy,
		}, requestIDFromContext(c), stamp); err != nil {
			return err
		}
		state.response.Status = agentRunInputSlotBound
		return nil
	})
	unlockGates()
	if err != nil {
		a.wakeAgentRunGatesAfterStaleInputs(err)
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	} else {
		a.launchQueuedAgentRuns()
		if a.aiContinuations != nil {
			a.aiContinuations.wake()
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": state.response})
}
