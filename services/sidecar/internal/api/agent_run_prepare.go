package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// agentRunFileAccessProviderConfirmation binds the HUMAN file-body consent to
// the exact Provider identity displayed when the checkbox was selected. The
// endpoint is covered by config_version; kind is repeated explicitly because
// it is the local/off-device boundary the user approved.
type agentRunFileAccessProviderConfirmation struct {
	Version       int64  `json:"version"`
	ConfigVersion int64  `json:"config_version"`
	Kind          string `json:"kind"`
}

func (confirmation agentRunFileAccessProviderConfirmation) valid() bool {
	return confirmation.Version > 0 && confirmation.ConfigVersion > 0 &&
		(confirmation.Kind == "local" || confirmation.Kind == "remote")
}

func (confirmation agentRunFileAccessProviderConfirmation) matches(provider models.AIProvider) bool {
	return confirmation.Version == provider.Version &&
		confirmation.ConfigVersion == provider.ConfigVersion &&
		confirmation.Kind == provider.Kind
}

// agentRunIdentity is the optimistic-concurrency boundary for one execution.
// Assignment rows have no numeric version, so their immutable id+assigned_at
// pair is the assignment version. All other mutable collaborators use their
// native monotonically increasing versions.
type agentRunIdentity struct {
	TaskVersion           int64
	AssignmentID          string
	AssignmentAssignedAt  string
	ActorID               string
	ActorVersion          int64
	AdapterID             string
	AdapterVersion        int64
	ProviderID            string
	ProviderVersion       int64
	ProviderConfigVersion int64
}

type prepareAgentRunInput struct {
	Restart                        *agentRunRestartRequest
	TaskID                         string
	ProviderID                     string
	Expected                       *agentRunIdentity
	FileAccessProviderConfirmation *agentRunFileAccessProviderConfirmation
	ReworkProviderConfirmation     *agentRunFileAccessProviderConfirmation
	Rework                         *agentRunReworkRequest
	InputFiles                     []agentRunInputFileRequest
	OutputContract                 *agentRunOutputContractRequest
	ArtifactStore                  *filestore.ArtifactStore
	// DeferredInputs selects contract v7: the Run's files are the ones its
	// predecessor will submit, confirmed by a person later (ADR-033).
	DeferredInputs *agentRunDeferredInputs
}

// preparedAgentRun contains only database facts read inside the caller's
// transaction. Native HTTP and AI approval flows both pass this exact value to
// createAgentRunInTransaction, preventing either entry point from weakening
// the execution gates.
type preparedAgentRun struct {
	Restart                  *agentRunRestartSource
	Task                     models.Task
	Assignment               models.TaskAssignment
	Actor                    models.Actor
	Adapter                  models.AgentAdapter
	Provider                 models.AIProvider
	Identity                 agentRunIdentity
	Snapshot                 agentexec.TaskSnapshot
	FrozenFiles              []agentexec.FrozenFileInput
	Rework                   *agentexec.ReworkContext
	OutputContract           *agentexec.OutputContract
	ExecutionContractVersion int64
	InputSnapshotJSON        string
	Attempt                  int
	ParentRunID              *string
}

func (identity agentRunIdentity) equals(other agentRunIdentity) bool {
	return identity.TaskVersion == other.TaskVersion &&
		identity.AssignmentID == other.AssignmentID &&
		identity.AssignmentAssignedAt == other.AssignmentAssignedAt &&
		identity.ActorID == other.ActorID && identity.ActorVersion == other.ActorVersion &&
		identity.AdapterID == other.AdapterID && identity.AdapterVersion == other.AdapterVersion &&
		identity.ProviderID == other.ProviderID && identity.ProviderVersion == other.ProviderVersion &&
		identity.ProviderConfigVersion == other.ProviderConfigVersion
}

// builtinExecutionReady reports whether the builtin lifecycle matrix for this
// build's platform has been verified (ADR-027). Only Windows carries real
// evidence today: pipe roundtrip, job-object subtree reclaim, cancel/timeout.
func builtinExecutionReady() (bool, string) {
	if runtime.GOOS == "windows" {
		return true, agentRunWindowsIsolation
	}
	return false, "unverified"
}

func taskSnapshotFromModel(task models.Task) agentexec.TaskSnapshot {
	return agentexec.TaskSnapshot{
		TaskID: task.ID, Title: task.Title, Description: task.Description,
		CompletionCriteria: task.CompletionCriteria, Status: task.Status, Kind: task.Kind,
		ReviewPolicy: task.ReviewPolicy, Priority: task.Priority, ProjectID: task.ProjectID,
		ParentTaskID: task.ParentTaskID, DueDate: task.DueDate, PlannedDate: task.PlannedDate,
		EstimatedMinutes: task.EstimatedMinutes, ActualMinutes: task.ActualMinutes,
		ManualOrder: task.ManualOrder, Version: task.Version,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

// agentRunV1TaskSnapshot preserves the original pipe-v1 durable JSON byte
// shape. New task fields belong to contract v2; changing this encoder would
// make already queued v1 Runs fail identity recovery after an upgrade.
type agentRunV1TaskSnapshot struct {
	TaskID             string `json:"task_id"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	CompletionCriteria string `json:"completion_criteria,omitempty"`
	Status             string `json:"status"`
	Kind               string `json:"kind"`
}

func agentRunV1TaskSnapshotFromModel(task models.Task) agentRunV1TaskSnapshot {
	return agentRunV1TaskSnapshot{
		TaskID: task.ID, Title: task.Title, Description: task.Description,
		CompletionCriteria: task.CompletionCriteria, Status: task.Status, Kind: task.Kind,
	}
}

func validateAgentRunProvider(provider models.AIProvider) error {
	if provider.Kind != "local" && provider.Kind != "remote" ||
		provider.Status != "ready" || provider.HealthStatus != "healthy" ||
		(provider.Protocol != agentexec.ModelProtocolOpenAIChat &&
			(provider.Kind != "remote" || provider.Protocol != agentexec.ModelProtocolAnthropicMessages)) || strings.TrimSpace(provider.Model) == "" {
		return newProjectRequestError(http.StatusUnprocessableEntity, "AGENT_PROVIDER_INVALID",
			"provider_id must reference a ready, healthy local OpenAI or online OpenAI/Anthropic model provider")
	}
	if provider.Kind == "remote" && !provider.HasKey {
		return newProjectRequestError(http.StatusUnprocessableEntity, "AGENT_PROVIDER_INVALID",
			"The online model provider must have a configured credential")
	}
	if provider.Kind == "local" {
		if _, err := agentexec.ValidateLoopbackModelEndpoint(provider.BaseURL); err != nil {
			return newProjectRequestError(http.StatusUnprocessableEntity, "AGENT_PROVIDER_INVALID",
				"The local model provider endpoint must be a loopback http URL")
		}
		return nil
	}
	if _, err := agentexec.ValidateModelEndpoint(provider.BaseURL); err != nil {
		return newProjectRequestError(http.StatusUnprocessableEntity, "AGENT_PROVIDER_INVALID",
			"The online model provider endpoint URL is invalid")
	}
	return nil
}

func validateAgentRunAdapter(adapter models.AgentAdapter) error {
	if adapter.Kind != "builtin" || adapter.ProtocolVersion != agentexec.ProtocolVersion ||
		adapter.Status != "enabled" || adapter.HealthStatus != "healthy" ||
		adapter.IsolationStatus != "verified" || !adapter.ExecutionReady {
		return newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The agent adapter is not healthy, enabled, isolated, and execution ready")
	}
	return nil
}

func prepareAgentRun(tx *gorm.DB, input prepareAgentRunInput) (preparedAgentRun, error) {
	var prepared preparedAgentRun
	if err := tx.First(&prepared.Task, "id = ?", input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.Expected != nil {
				return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
					"The task, assignment, agent, adapter, or provider changed before execution")
			}
			return prepared, newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
		}
		return prepared, err
	}
	if input.Restart != nil {
		if !canonicalRestartID(input.Restart.RunID) || input.Restart.ExpectedTaskVersion < 1 {
			return prepared, restartInvalidError()
		}
		if prepared.Task.Version != input.Restart.ExpectedTaskVersion {
			return prepared, agentRunIdentityChangedError()
		}
		var err error
		prepared.Restart, err = loadAgentRunRestartSource(tx, prepared.Task.ID, input.Restart.RunID)
		if err != nil {
			return prepared, err
		}
	}

	assignmentQuery := tx.Where(
		"task_id = ? AND role = 'assignee' AND unassigned_at IS NULL", input.TaskID,
	)
	if err := assignmentQuery.Take(&prepared.Assignment).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return prepared, err
		}
		if input.Expected != nil {
			return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The task, assignment, agent, adapter, or provider changed before execution")
		}
		return prepared, newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The task has no active assignment to an agent actor")
	}
	if err := tx.First(&prepared.Actor, "id = ?", prepared.Assignment.ActorID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return prepared, err
		}
		if input.Expected != nil {
			return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The task, assignment, agent, adapter, or provider changed before execution")
		}
		return prepared, newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The active assignment does not point at an agent actor")
	}
	if prepared.Actor.AgentAdapterID == nil || *prepared.Actor.AgentAdapterID == "" {
		if input.Expected != nil {
			return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The task, assignment, agent, adapter, or provider changed before execution")
		}
		return prepared, newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The agent actor is not linked to an adapter")
	}
	if err := tx.First(&prepared.Adapter, "id = ?", *prepared.Actor.AgentAdapterID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return prepared, err
		}
		if input.Expected != nil {
			return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The task, assignment, agent, adapter, or provider changed before execution")
		}
		return prepared, newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The agent actor's adapter does not exist")
	}
	if err := tx.First(&prepared.Provider, "id = ?", input.ProviderID).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return prepared, err
		}
		if input.Expected != nil || input.FileAccessProviderConfirmation != nil || input.ReworkProviderConfirmation != nil {
			return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
				"The task, assignment, agent, adapter, or provider changed before execution")
		}
		return prepared, newProjectRequestError(http.StatusUnprocessableEntity, "AGENT_PROVIDER_INVALID",
			"provider_id must reference an AI provider")
	}

	prepared.Identity = agentRunIdentity{
		TaskVersion:          prepared.Task.Version,
		AssignmentID:         prepared.Assignment.ID,
		AssignmentAssignedAt: prepared.Assignment.AssignedAt,
		ActorID:              prepared.Actor.ID, ActorVersion: prepared.Actor.Version,
		AdapterID: prepared.Adapter.ID, AdapterVersion: prepared.Adapter.Version,
		ProviderID: prepared.Provider.ID, ProviderVersion: prepared.Provider.Version,
		ProviderConfigVersion: prepared.Provider.ConfigVersion,
	}
	if input.Expected != nil && !prepared.Identity.equals(*input.Expected) {
		return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
			"The task, assignment, agent, adapter, or provider changed before execution")
	}
	if input.FileAccessProviderConfirmation != nil &&
		!input.FileAccessProviderConfirmation.matches(prepared.Provider) {
		return prepared, newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
			"The Agent Provider changed after file access was confirmed; review and confirm the Provider again")
	}
	if input.ReworkProviderConfirmation != nil && !input.ReworkProviderConfirmation.matches(prepared.Provider) {
		return prepared, agentRunIdentityChangedError()
	}
	if prepared.Task.Status != "todo" && prepared.Task.Status != "in_progress" {
		return prepared, newProjectRequestError(http.StatusConflict, agentRunTaskNotActionable,
			"Agent execution requires a todo or in-progress task")
	}
	if prepared.Actor.Type != "agent" || prepared.Actor.Status != "active" ||
		prepared.Actor.AgentAdapterID == nil || *prepared.Actor.AgentAdapterID != prepared.Adapter.ID {
		return prepared, newProjectRequestError(http.StatusConflict, agentRunNotExecutable,
			"The active assignment does not point at an active agent with the selected adapter")
	}
	if err := validateAgentRunAdapter(prepared.Adapter); err != nil {
		return prepared, err
	}
	if err := validateAgentRunProvider(prepared.Provider); err != nil {
		return prepared, err
	}
	var activeCount int64
	if err := tx.Model(&models.AgentRun{}).
		Where("task_id = ? AND status IN ?", input.TaskID, []string{"queued", "running"}).
		Count(&activeCount).Error; err != nil {
		return prepared, err
	}
	if activeCount != 0 {
		return prepared, newProjectRequestError(http.StatusConflict, agentRunAlreadyActive,
			"The task already has a queued or running Agent Run")
	}
	if err := tx.Model(&models.AgentRun{}).
		Where("task_id = ? AND actor_id = ?", input.TaskID, prepared.Actor.ID).
		Select("COALESCE(MAX(attempt), 0) + 1").Scan(&prepared.Attempt).Error; err != nil {
		return prepared, err
	}
	prepared.Snapshot = taskSnapshotFromModel(prepared.Task)
	if input.Rework != nil {
		var err error
		prepared.Rework, err = freezeAgentRunRework(tx, prepared.Task, input.Rework)
		if err != nil {
			return prepared, err
		}
	}
	contract, err := normalizeAgentRunOutputContract(input.OutputContract)
	if err != nil {
		return prepared, err
	}
	if input.DeferredInputs != nil {
		return prepareAgentRunWithDeferredInputs(prepared, input, contract)
	}
	if prepared.Provider.Protocol == agentexec.ModelProtocolOpenAIChat && input.Rework == nil && len(input.InputFiles) == 0 && contract.Type == agentexec.ResultTypeText {
		encoded, err := json.Marshal(agentRunV1TaskSnapshotFromModel(prepared.Task))
		if err != nil {
			return prepared, err
		}
		prepared.ExecutionContractVersion = agentRunExecutionContractVersion
		prepared.InputSnapshotJSON = string(encoded)
		return prepared, nil
	}
	if (contract.Type == agentexec.ResultTypeFile || contract.Type == agentexec.ResultTypeFiles) && input.ArtifactStore == nil {
		return prepared, newProjectRequestError(http.StatusServiceUnavailable,
			agentRunFileStorageUnavailableCode, "Controlled Artifact storage is unavailable")
	}
	references, files, err := freezeAgentRunFiles(
		tx, prepared.Task, input.InputFiles, input.ArtifactStore,
	)
	if err != nil {
		return prepared, err
	}
	v2Snapshot := agentRunV2InputSnapshot{
		Task: prepared.Snapshot, ProviderKind: prepared.Provider.Kind,
		Files: references, OutputContract: contract,
	}
	encoded, err := json.Marshal(v2Snapshot)
	if err != nil {
		return prepared, err
	}
	prepared.FrozenFiles = files
	prepared.OutputContract = &contract
	if prepared.Provider.Protocol == agentexec.ModelProtocolAnthropicMessages || agentRunHasProjectTaskFiles(references) {
		v5 := agentRunV5InputSnapshot{Task: prepared.Snapshot, ProviderKind: prepared.Provider.Kind,
			ModelProtocol: prepared.Provider.Protocol,
			Files:         references, OutputContract: contract, Rework: prepared.Rework}
		if v5.ModelProtocol == agentexec.ModelProtocolAnthropicMessages {
			v5.MaxOutputTokens = agentexec.AnthropicMaxOutputTokens
		}
		prepared.ExecutionContractVersion = agentRunExecutionContractVersionV5
		if agentRunHasProjectTaskFiles(references) {
			prepared.ExecutionContractVersion = agentRunExecutionContractVersionV6
		}
		encoded, err = json.Marshal(v5)
		if err != nil {
			return prepared, err
		}
		if _, err := parseAgentRunModelSnapshot(prepared.ExecutionContractVersion, string(encoded)); err != nil {
			return prepared, err
		}
		prepared.InputSnapshotJSON = string(encoded)
		return prepared, nil
	}
	prepared.ExecutionContractVersion = agentRunExecutionContractVersionV2
	if contract.Type == agentexec.ResultTypeFiles {
		prepared.ExecutionContractVersion = agentRunExecutionContractVersionV3
	}
	if prepared.Rework != nil {
		v4 := agentRunV4InputSnapshot{Task: prepared.Snapshot, ProviderKind: prepared.Provider.Kind, Files: references, OutputContract: contract, Rework: *prepared.Rework}
		encoded, err = json.Marshal(v4)
		if err != nil {
			return prepared, err
		}
		if _, err := parseAgentRunV4Snapshot(string(encoded)); err != nil {
			return prepared, reworkInvalidError()
		}
		prepared.ExecutionContractVersion = agentRunExecutionContractVersionV4
	}
	prepared.InputSnapshotJSON = string(encoded)
	return prepared, nil
}

// prepareAgentRunWithDeferredInputs freezes a v7 Run. Its only input is the
// declaration, so it cannot also carry static files, a rework context or a
// restart source: each of those is its own consent over content that exists
// now, and mixing them would blur what the later confirmation covers.
func prepareAgentRunWithDeferredInputs(
	prepared preparedAgentRun,
	input prepareAgentRunInput,
	contract agentexec.OutputContract,
) (preparedAgentRun, error) {
	if len(input.InputFiles) != 0 || input.Rework != nil || input.Restart != nil {
		return prepared, agentRunDeferredInputsError(
			"A Run that waits for predecessor files cannot also select files, rework context or a restart source")
	}
	if !validAgentRunDeferredInputs(*input.DeferredInputs) || prepared.Task.ProjectID == nil || *prepared.Task.ProjectID == "" {
		return prepared, agentRunDeferredInputsError("Predecessor files can only be passed on between Tasks of the same Project")
	}
	if (contract.Type == agentexec.ResultTypeFile || contract.Type == agentexec.ResultTypeFiles) && input.ArtifactStore == nil {
		return prepared, newProjectRequestError(http.StatusServiceUnavailable,
			agentRunFileStorageUnavailableCode, "Controlled Artifact storage is unavailable")
	}
	slot := *input.DeferredInputs
	slot.ExpectedFiles = append([]agentexec.OutputFileContract(nil), slot.ExpectedFiles...)
	snapshot := agentRunV5InputSnapshot{
		Task: prepared.Snapshot, ProviderKind: prepared.Provider.Kind, ModelProtocol: prepared.Provider.Protocol,
		Files: []frozenAgentRunFileReference{}, OutputContract: contract, DeferredInputs: &slot,
	}
	if snapshot.ModelProtocol == agentexec.ModelProtocolAnthropicMessages {
		snapshot.MaxOutputTokens = agentexec.AnthropicMaxOutputTokens
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return prepared, err
	}
	if _, err := parseAgentRunV7Snapshot(string(encoded)); err != nil {
		return prepared, err
	}
	prepared.OutputContract = &contract
	prepared.ExecutionContractVersion = agentRunExecutionContractVersionV7
	prepared.InputSnapshotJSON = string(encoded)
	return prepared, nil
}

func agentRunConstraintError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "UNIQUE constraint failed: agent_runs.task_id") &&
		!strings.Contains(message, "agent_runs.actor_id") {
		return newProjectRequestError(http.StatusConflict, agentRunAlreadyActive,
			"The task already has a queued or running Agent Run")
	}
	if strings.Contains(message, "UNIQUE constraint failed: agent_runs.task_id, agent_runs.actor_id, agent_runs.attempt") {
		return newProjectRequestError(http.StatusConflict, agentRunIdentityChanged,
			"The Agent Run attempt changed; reload before retrying")
	}
	return err
}

func createAgentRunInTransaction(
	tx *gorm.DB,
	prepared preparedAgentRun,
	createdByActorID string,
	requestID string,
	now string,
) (models.AgentRun, error) {
	var admittedRuns int64
	if err := tx.Model(&models.AgentRun{}).
		Where("status IN ? AND output_delivery_status = ?", []string{"queued", "running"}, agentRunOutputNotReady).
		Count(&admittedRuns).Error; err != nil {
		return models.AgentRun{}, err
	}
	if admittedRuns >= agentRunMaxAccepted {
		return models.AgentRun{}, newProjectRequestError(
			http.StatusTooManyRequests,
			agentRunQueueFullCode,
			"The Agent Run queue is full; no run was created. Retry after active executions finish",
		)
	}
	run := models.AgentRun{
		ID: uuid.NewString(), TaskID: prepared.Task.ID, AssignmentID: prepared.Assignment.ID,
		ActorID: prepared.Actor.ID, AdapterID: prepared.Adapter.ID, CreatedByActorID: createdByActorID,
		ParentRunID: prepared.ParentRunID, Attempt: prepared.Attempt, Status: "queued",
		ProviderID: prepared.Provider.ID, Model: prepared.Provider.Model,
		TaskVersion:          prepared.Identity.TaskVersion,
		AssignmentAssignedAt: prepared.Identity.AssignmentAssignedAt,
		ActorVersion:         prepared.Identity.ActorVersion, AdapterVersion: prepared.Identity.AdapterVersion,
		ProviderVersion:          prepared.Identity.ProviderVersion,
		ProviderConfigVersion:    prepared.Identity.ProviderConfigVersion,
		ExecutionContractVersion: prepared.ExecutionContractVersion,
		InputSnapshotJSON:        prepared.InputSnapshotJSON,
		OutputDeliveryStatus:     agentRunOutputNotReady,
		CreatedAt:                now,
	}
	if err := tx.Create(&run).Error; err != nil {
		return models.AgentRun{}, agentRunConstraintError(err)
	}
	if err := recordAgentRunWorkflowEvent(tx, "agent_run_queued", run.ID, map[string]any{
		"attempt": run.Attempt, "task_version": run.TaskVersion,
		"assignment_id": run.AssignmentID, "assignment_assigned_at": run.AssignmentAssignedAt,
		"actor_id": run.ActorID, "actor_version": run.ActorVersion,
		"adapter_id": run.AdapterID, "adapter_version": run.AdapterVersion,
		"provider_id": run.ProviderID, "provider_version": run.ProviderVersion,
		"provider_config_version":    run.ProviderConfigVersion,
		"execution_contract_version": run.ExecutionContractVersion,
	}, requestID, now); err != nil {
		return models.AgentRun{}, err
	}
	if err := recordAgentRunRestart(tx, run, prepared.Restart, requestID, now); err != nil {
		return models.AgentRun{}, err
	}
	if prepared.Restart != nil {
		sourceID := prepared.Restart.RunID
		run.RestartOfRunID = &sourceID
	}
	return run, nil
}
