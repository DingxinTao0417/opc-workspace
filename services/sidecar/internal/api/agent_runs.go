package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	createAgentRunEndpoint                 = "/api/v1/tasks/:id/agent-runs"
	agentRunNotExecutable                  = "AGENT_RUN_NOT_EXECUTABLE"
	agentRunTaskNotActionable              = "AGENT_RUN_TASK_NOT_ACTIONABLE"
	agentRunAlreadyActive                  = "AGENT_RUN_ALREADY_ACTIVE"
	agentRunIdentityChanged                = "AGENT_RUN_IDENTITY_CHANGED"
	agentRunOutputNotReady                 = "not_ready"
	agentRunOutputPending                  = "pending"
	agentRunOutputSubmitted                = "submitted"
	agentRunOutputRetained                 = "retained"
	agentRunOutputPendingCode              = "AGENT_OUTPUT_DELIVERY_PENDING"
	agentRunOutputLegacy                   = "AGENT_OUTPUT_DELIVERY_LEGACY"
	agentRunOutputNotPending               = "AGENT_OUTPUT_DELIVERY_NOT_PENDING"
	agentRunQueueFullCode                  = "AGENT_RUN_QUEUE_FULL"
	agentRunMaxConcurrentWorkers           = 8
	agentRunMaxQueued                      = 32
	agentRunMaxAccepted                    = agentRunMaxConcurrentWorkers + agentRunMaxQueued
	agentRunExecutionContractVersion int64 = 1
	agentRunWindowsIsolation               = "builtin_lifecycle_verified_windows"
	defaultAgentRunTimeout                 = 10 * time.Minute
)

var errAgentRunOutputDeliveryPending = errors.New("Agent Run output delivery is safely staged for recovery")
var errAgentRunRestorePending = errors.New("Agent Run storage lease blocked by pending restore")

func createAgentRunIdempotencyEndpoint(taskID string) string {
	return strings.Replace(createAgentRunEndpoint, ":id", taskID, 1)
}

func agentRunIdempotencyReplayUnavailable() error {
	return newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE",
		"This legacy Idempotency-Key cannot be replayed safely; use a new key")
}

// replayAgentRunCreateCommand scopes a caller-provided key to the canonical
// Task resource. Older builds stored the route template as the endpoint, so a
// legacy record is replayed only when its immutable Run still proves that it
// belongs to this Task. A legacy key from another Task does not reserve the
// key in the current Task scope.
func replayAgentRunCreateCommand(
	tx *gorm.DB,
	key string,
	taskID string,
	requestHash string,
	response *agentRunResponse,
) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	scopedEndpoint := createAgentRunIdempotencyEndpoint(taskID)
	replayed, status, err := replayTaskOutputCommand(tx, key, scopedEndpoint, requestHash, response)
	if err != nil {
		return false, 0, err
	}
	if replayed {
		if response.TaskID != taskID {
			return false, 0, agentRunIdempotencyReplayUnavailable()
		}
		if response.RestartOfRunID != nil {
			source, proofErr := loadAgentRunRestartProof(tx, models.AgentRun{ID: response.ID})
			if proofErr != nil {
				return false, 0, proofErr
			}
			if source == nil || source.RunID != *response.RestartOfRunID {
				return false, 0, agentRunIdempotencyReplayUnavailable()
			}
		}
		return true, status, nil
	}

	var legacy models.IdempotencyKey
	err = tx.Where("key = ? AND endpoint = ?", key, createAgentRunEndpoint).First(&legacy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	var legacyRun models.AgentRun
	if err := tx.Select("id", "task_id").First(&legacyRun, "id = ?", legacy.ResourceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, 0, agentRunIdempotencyReplayUnavailable()
		}
		return false, 0, err
	}
	if legacyRun.TaskID != taskID {
		return false, 0, nil
	}
	replayed, status, err = replayTaskOutputCommand(tx, key, createAgentRunEndpoint, requestHash, response)
	if err != nil {
		return false, 0, err
	}
	if !replayed || response.TaskID != taskID {
		return false, 0, agentRunIdempotencyReplayUnavailable()
	}
	return true, status, nil
}

type agentRunResponse struct {
	ID                            string                                  `json:"id"`
	TaskID                        string                                  `json:"task_id"`
	AssignmentID                  string                                  `json:"assignment_id"`
	ActorID                       string                                  `json:"actor_id"`
	AdapterID                     string                                  `json:"adapter_id"`
	CreatedByActorID              string                                  `json:"created_by_actor_id"`
	ParentRunID                   *string                                 `json:"parent_run_id"`
	RestartOfRunID                *string                                 `json:"restart_of_run_id,omitempty"`
	Attempt                       int                                     `json:"attempt"`
	Status                        string                                  `json:"status"`
	ProviderID                    string                                  `json:"provider_id"`
	Model                         string                                  `json:"model"`
	TaskVersion                   int64                                   `json:"task_version"`
	AssignmentAssignedAt          string                                  `json:"assignment_assigned_at"`
	ActorVersion                  int64                                   `json:"actor_version"`
	AdapterVersion                int64                                   `json:"adapter_version"`
	ProviderVersion               int64                                   `json:"provider_version"`
	ProviderConfigVersion         int64                                   `json:"provider_config_version"`
	ExecutionContractVersion      int64                                   `json:"execution_contract_version"`
	OutputContract                *agentexec.OutputContract               `json:"output_contract,omitempty"`
	ReworkContext                 *agentexec.ReworkContext                `json:"rework_context,omitempty"`
	InputFiles                    []frozenAgentRunFileReference           `json:"input_files,omitempty"`
	ReworkProviderConfirmation    *agentRunFileAccessProviderConfirmation `json:"rework_provider_confirmation,omitempty"`
	ModelProtocol                 string                                  `json:"model_protocol,omitempty"`
	MaxOutputTokens               *int                                    `json:"max_output_tokens,omitempty"`
	ExecutionProviderConfirmation *agentRunFileAccessProviderConfirmation `json:"execution_provider_confirmation,omitempty"`
	ResultText                    *string                                 `json:"result_text"`
	ResultBytes                   *int                                    `json:"result_bytes"`
	ErrorCode                     *string                                 `json:"error_code"`
	OutputDeliveryStatus          string                                  `json:"output_delivery_status"`
	OutputDeliveryErrorCode       *string                                 `json:"output_delivery_error_code"`
	SubmissionID                  *string                                 `json:"submission_id"`
	ArtifactID                    *string                                 `json:"artifact_id"`
	StartedAt                     *string                                 `json:"started_at"`
	CompletedAt                   *string                                 `json:"completed_at"`
	CreatedAt                     string                                  `json:"created_at"`
	Progress                      *agentRunProgress                       `json:"progress,omitempty"`
	// StartGate is present on reads that project it (the Task's Run list and
	// the Run detail), so the execution drawer can show what a Run waits for.
	StartGate *models.AgentRunStartGate `json:"start_gate,omitempty"`
}

func agentRunResponseFromModel(row models.AgentRun) agentRunResponse {
	var outputContract *agentexec.OutputContract
	if contract, err := agentRunOutputContract(row); err == nil {
		outputContract = &contract
	}
	return agentRunResponse{
		ID: row.ID, TaskID: row.TaskID, AssignmentID: row.AssignmentID, ActorID: row.ActorID,
		AdapterID: row.AdapterID, CreatedByActorID: row.CreatedByActorID,
		ParentRunID: row.ParentRunID, Attempt: row.Attempt, Status: row.Status,
		RestartOfRunID: row.RestartOfRunID,
		ProviderID:     row.ProviderID, Model: row.Model,
		TaskVersion: row.TaskVersion, AssignmentAssignedAt: row.AssignmentAssignedAt,
		ActorVersion: row.ActorVersion, AdapterVersion: row.AdapterVersion,
		ProviderVersion: row.ProviderVersion, ProviderConfigVersion: row.ProviderConfigVersion,
		ExecutionContractVersion: row.ExecutionContractVersion,
		OutputContract:           outputContract,
		ResultText:               row.ResultText, ResultBytes: row.ResultBytes, ErrorCode: row.ErrorCode,
		OutputDeliveryStatus: row.OutputDeliveryStatus, OutputDeliveryErrorCode: row.OutputDeliveryErrorCode,
		SubmissionID: row.SubmissionID, ArtifactID: row.ArtifactID,
		StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt,
		StartGate: row.StartGate,
	}
}

type createAgentRunRequest struct {
	Restart                        *agentRunRestartRequest                 `json:"restart,omitempty"`
	ConfirmRestart                 bool                                    `json:"confirm_restart,omitempty"`
	RestartPreviewHash             string                                  `json:"restart_preview_hash,omitempty"`
	ProviderID                     string                                  `json:"provider_id"`
	AutoAssign                     *agentRunAutoAssignRequest              `json:"auto_assign,omitempty"`
	InputFiles                     []agentRunInputFileRequest              `json:"input_files,omitempty"`
	ConfirmFileAccess              bool                                    `json:"confirm_file_access,omitempty"`
	ConfirmProjectTaskFiles        bool                                    `json:"confirm_project_task_files,omitempty"`
	FileAccessProviderConfirmation *agentRunFileAccessProviderConfirmation `json:"file_access_provider_confirmation,omitempty"`
	OutputContract                 *agentRunOutputContractRequest          `json:"output_contract,omitempty"`
	Rework                         *agentRunReworkRequest                  `json:"rework,omitempty"`
	ConfirmReworkContext           bool                                    `json:"confirm_rework_context,omitempty"`
	ReworkProviderConfirmation     *agentRunFileAccessProviderConfirmation `json:"rework_provider_confirmation,omitempty"`
}

// agentRunAutoAssignRequest is deliberately narrower than the standalone
// assignment API: starting a Run may only fill an empty assignee slot, and the
// assignment is committed atomically with the Run after every execution gate
// has been rechecked.
type agentRunAutoAssignRequest struct {
	ActorID             string `json:"actor_id"`
	ExpectedTaskVersion int64  `json:"expected_task_version"`
}

func (a *API) createAgentRun(c *gin.Context) {
	taskID, ok := taskID(c)
	if !ok {
		return
	}
	var input createAgentRunRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	if err := validateNativeAgentRestartConfirmation(input); err != nil {
		writeProjectRequestError(c, err)
		return
	}
	if _, err := uuid.Parse(input.ProviderID); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "provider_id must be a canonical UUID")
		return
	}
	if input.AutoAssign != nil {
		if input.Rework != nil {
			writeError(c, 422, agentRunReworkInvalidCode, "Assign the task separately before preparing rework")
			return
		}
		rawActorID := strings.TrimSpace(input.AutoAssign.ActorID)
		actorID, err := validateAssignmentActorID(rawActorID)
		if err != nil || actorID != rawActorID || rawActorID != input.AutoAssign.ActorID {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "auto_assign.actor_id must be a canonical UUID")
			return
		}
		if input.AutoAssign.ExpectedTaskVersion < 1 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "auto_assign.expected_task_version must be a positive integer")
			return
		}
		input.AutoAssign.ActorID = actorID
	}
	if err := validateAgentRunProjectTaskFilesConfirmation(agentRunRequestsHaveProjectTaskFiles(input.InputFiles), input.ConfirmProjectTaskFiles); err != nil {
		writeProjectRequestError(c, err)
		return
	}
	if len(input.InputFiles) > 0 && !input.ConfirmFileAccess {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED",
			"confirm_file_access=true is required to send selected controlled files to the Agent provider")
		return
	}
	if len(input.InputFiles) > 0 && input.FileAccessProviderConfirmation == nil {
		writeError(c, http.StatusUnprocessableEntity, "CONFIRMATION_REQUIRED",
			"file_access_provider_confirmation is required for the exact Provider shown when file access was confirmed")
		return
	}
	if input.FileAccessProviderConfirmation != nil && !input.FileAccessProviderConfirmation.valid() {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR",
			"file_access_provider_confirmation must contain positive versions and kind local or remote")
		return
	}
	if len(input.InputFiles) == 0 && (input.ConfirmFileAccess || input.FileAccessProviderConfirmation != nil) {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR",
			"file access confirmation can only be supplied when input_files are selected")
		return
	}
	if err := validateAgentRunReworkConfirmation(input.Rework != nil, input.ConfirmReworkContext, input.ReworkProviderConfirmation); err != nil {
		writeProjectRequestError(c, err)
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	statusCode, replayed := http.StatusCreated, false
	var response agentRunResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replayStatus := 0
		var err error
		replayed, replayStatus, err = replayAgentRunCreateCommand(tx, idempotencyKey, taskID, requestHash, &response)
		if err != nil || replayed {
			if replayed {
				statusCode = replayStatus
			}
			return err
		}
		now := a.options.Now().UTC().Format(time.RFC3339Nano)
		if input.AutoAssign != nil {
			if _, err := createAssignmentInTransaction(
				tx, taskID, input.AutoAssign.ExpectedTaskVersion, "assignee",
				input.AutoAssign.ActorID, requestIDFromContext(c), now,
			); err != nil {
				return err
			}
		}
		prepared, err := prepareAgentRun(tx, prepareAgentRunInput{
			Restart: input.Restart,
			TaskID:  taskID, ProviderID: input.ProviderID,
			FileAccessProviderConfirmation: input.FileAccessProviderConfirmation,
			Rework:                         input.Rework, ReworkProviderConfirmation: input.ReworkProviderConfirmation,
			InputFiles: input.InputFiles, OutputContract: input.OutputContract, ArtifactStore: a.artifactStore,
		})
		if err != nil {
			return err
		}
		if input.Restart != nil {
			selection := agentRunRestartPreviewRequest{ProviderID: input.ProviderID, Restart: input.Restart, InputFiles: input.InputFiles, OutputContract: input.OutputContract, Rework: input.Rework}
			if agentRunRestartPreview(prepared, selection).Fingerprint != input.RestartPreviewHash {
				return agentRunIdentityChangedError()
			}
		}
		run, err := createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, requestIDFromContext(c), now,
		)
		if err != nil {
			return err
		}
		if err := recordTaskOutputIdempotency(tx, idempotencyKey, createAgentRunIdempotencyEndpoint(taskID), run.ID,
			requestHash, http.StatusCreated, agentRunResponseFromModel(run), now); err != nil {
			return err
		}
		response = agentRunResponseFromModel(run)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	} else {
		a.launchAgentRun(response.ID)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) cancelAgentRun(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		return requestAgentRunCancellation(tx, runID, requestIDFromContext(c), now)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	a.wakeAIContinuationsAfterFactCommit()
	if cancel, ok := a.agentRunCancel(runID); ok {
		cancel()
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": runID, "status": "cancel_requested"}})
}

func (a *API) retryAgentRunOutputDelivery(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	run, err := a.recoverAgentRunOutput(runID, a.options.Now().UTC())
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeError(c, http.StatusServiceUnavailable, agentRunOutputPendingCode,
			"Agent output delivery is still pending and can be retried safely")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": agentRunResponseFromModel(run)})
}

func (a *API) recoverAgentRunOutput(runID string, now time.Time) (models.AgentRun, error) {
	var response models.AgentRun
	deliveryFiles := agentRunOutputFileDelivery{api: a, store: a.artifactStore}
	err := withAgentRunOutputTransaction(a.db, func(tx *gorm.DB) error {
		var err error
		response, err = a.recoverAgentRunOutputInTransaction(tx, runID, now, &deliveryFiles)
		return err
	})
	err = deliveryFiles.finish(err)
	if err == nil {
		a.wakeAIContinuationsAfterFactCommit()
	}
	return response, err
}

func (a *API) retryAgentRun(c *gin.Context) {
	runID, ok := agentRunID(c)
	if !ok {
		return
	}
	var input struct {
		ConfirmReworkContext           bool                                    `json:"confirm_rework_context,omitempty"`
		ReworkProviderConfirmation     *agentRunFileAccessProviderConfirmation `json:"rework_provider_confirmation,omitempty"`
		ConfirmFileAccess              bool                                    `json:"confirm_file_access,omitempty"`
		ConfirmProjectTaskFiles        bool                                    `json:"confirm_project_task_files,omitempty"`
		FileAccessProviderConfirmation *agentRunFileAccessProviderConfirmation `json:"file_access_provider_confirmation,omitempty"`
	}
	if err := decodeJSON(c, &input); err != nil && !errors.Is(err, io.EOF) {
		writeError(c, 400, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	var response agentRunResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		prepared, _, err := prepareAgentRunRetry(tx, runID, a.artifactStore)
		if err != nil {
			return err
		}
		if err := validateAgentRunReworkConfirmation(prepared.Rework != nil, input.ConfirmReworkContext, input.ReworkProviderConfirmation); err != nil {
			return err
		}
		if input.ReworkProviderConfirmation != nil && !input.ReworkProviderConfirmation.matches(prepared.Provider) {
			return agentRunIdentityChangedError()
		}
		if err := validateAgentRunProjectTaskFilesConfirmation(prepared.ExecutionContractVersion == agentRunExecutionContractVersionV6, input.ConfirmProjectTaskFiles); err != nil {
			return err
		}
		if (prepared.Rework != nil || isAgentRunModelContract(prepared.ExecutionContractVersion)) && len(prepared.FrozenFiles) > 0 {
			if !input.ConfirmFileAccess || input.FileAccessProviderConfirmation == nil {
				return newProjectRequestError(422, "CONFIRMATION_REQUIRED", "Retry requires independent controlled-file and exact Provider confirmation")
			}
			if !input.FileAccessProviderConfirmation.valid() {
				return newProjectRequestError(422, "VALIDATION_ERROR", "File Provider confirmation is invalid")
			}
			if !input.FileAccessProviderConfirmation.matches(prepared.Provider) {
				return agentRunIdentityChangedError()
			}
		} else if input.ConfirmFileAccess || input.FileAccessProviderConfirmation != nil {
			return newProjectRequestError(422, "VALIDATION_ERROR", "File confirmation is only accepted for a v4/v5/v6 retry with frozen file inputs")
		}
		run, err := createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, requestIDFromContext(c), now,
		)
		if err != nil {
			return err
		}
		response = agentRunResponseFromModel(run)
		return nil
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	a.wakeAIContinuationsAfterFactCommit()
	a.launchAgentRun(response.ID)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func agentRunID(c *gin.Context) (string, bool) {
	value := c.Param("id")
	if _, err := uuid.Parse(value); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_AGENT_RUN_ID", "The agent run id is not a canonical UUID")
		return "", false
	}
	return value, true
}

func recordAgentRunWorkflowEvent(db *gorm.DB, action, runID string, payload map[string]any, requestID, createdAt string) error {
	var failure automationAgentRunFailureEvidence
	if action == "agent_run_failed" {
		var run models.AgentRun
		if err := db.Select("id", "task_id", "attempt", "status", "error_code", "completed_at", "output_delivery_status").First(&run, "id=?", runID).Error; err != nil {
			return err
		}
		var err error
		failure, err = automationAgentRunFailureEvidenceFromRun(run)
		if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil || formatInboxTimestamp(at) != failure.FailedAt || requestID != "" {
			return errAutomationSourceEventInvalid
		}
		payload = map[string]any{"agent_run_id": failure.AgentRunID, "task_id": failure.TaskID, "attempt": failure.Attempt, "error_code": failure.ErrorCode, "failed_at": failure.FailedAt}
	}
	currentBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var requestIDValue any
	if requestID != "" {
		requestIDValue = requestID
	}
	eventID := uuid.NewString()
	err = db.Table("workflow_events").Create(map[string]any{
		"id": eventID, "aggregate_type": "agent_run", "aggregate_id": runID,
		"action": action, "actor_id": models.BuiltinSystemActorID, "agent_run_id": runID,
		"request_id":    requestIDValue,
		"previous_json": nil, "current_json": string(currentBytes), "created_at": createdAt,
	}).Error
	if err == nil && action == "agent_run_failed" {
		return enqueueAgentRunFailureAutomationDelivery(db, eventID, failure, createdAt)
	}
	return err
}
