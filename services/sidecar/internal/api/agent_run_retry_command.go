package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Both native and human-approved AI retries must preserve the original
// execution identity and exact input bytes. Preparing does not create a Run.
func prepareAgentRunRetry(tx *gorm.DB, runID string, store *artifactStore) (preparedAgentRun, models.AgentRun, error) {
	var previous models.AgentRun
	var prepared preparedAgentRun
	if err := tx.First(&previous, "id=?", runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = newProjectRequestError(http.StatusNotFound, "AGENT_RUN_NOT_FOUND", "Agent run not found")
		}
		return prepared, previous, err
	}
	if previous.Status == "queued" || previous.Status == "running" {
		return prepared, previous, newProjectRequestError(409, "AGENT_RUN_NOT_TERMINAL", "Only a terminal run can be retried")
	}
	if _, err := loadAgentRunRestartProof(tx, previous); err != nil {
		return prepared, previous, err
	}
	// A new rework start and retry are different intents. Even if a retained
	// result left the Task version unchanged, never re-execute successful v4
	// work or staged evidence under the guise of a failure retry. Legacy
	// contracts retain their original native compatibility behavior.
	if (previous.ExecutionContractVersion == agentRunExecutionContractVersionV4 || isAgentRunModelContract(previous.ExecutionContractVersion)) &&
		(previous.Status != "failed" && previous.Status != "cancelled" && previous.Status != "interrupted" ||
			previous.OutputDeliveryStatus != agentRunOutputNotReady || previous.ResultText != nil || previous.ResultBytes != nil ||
			previous.SubmissionID != nil || previous.ArtifactID != nil || previous.OutputDeliveryPendingText != nil ||
			previous.OutputDeliveryPendingBytes != nil || previous.OutputDeliveryPendingCompletedAt != nil) {
		return prepared, previous, newProjectRequestError(409, "AGENT_RUN_NOT_RETRYABLE", "Only a failed, cancelled, or interrupted run without output can be retried")
	}
	expected := agentRunIdentity{TaskVersion: previous.TaskVersion, AssignmentID: previous.AssignmentID, AssignmentAssignedAt: previous.AssignmentAssignedAt, ActorID: previous.ActorID, ActorVersion: previous.ActorVersion, AdapterID: previous.AdapterID, AdapterVersion: previous.AdapterVersion, ProviderID: previous.ProviderID, ProviderVersion: previous.ProviderVersion, ProviderConfigVersion: previous.ProviderConfigVersion}
	var files []agentRunInputFileRequest
	var output *agentRunOutputContractRequest
	var rework *agentRunReworkRequest
	if isAgentRunModelContract(previous.ExecutionContractVersion) {
		snapshot, err := parseAgentRunModelSnapshot(previous.ExecutionContractVersion, previous.InputSnapshotJSON)
		if err != nil {
			return prepared, previous, agentRunIdentityChangedError()
		}
		files = agentRunInputRequestsFromSnapshot(snapshot.fileSnapshot())
		output = agentRunOutputRequestFromSnapshot(snapshot.fileSnapshot())
		if snapshot.Rework != nil {
			rework = agentRunReworkRequestFromContext(*snapshot.Rework, previous.TaskVersion)
		}
	} else if previous.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
		snapshot, err := parseAgentRunV4Snapshot(previous.InputSnapshotJSON)
		if err != nil {
			return prepared, previous, agentRunIdentityChangedError()
		}
		files = agentRunInputRequestsFromSnapshot(snapshot.fileSnapshot())
		output = agentRunOutputRequestFromSnapshot(snapshot.fileSnapshot())
		rework = agentRunReworkRequestFromContext(snapshot.Rework, previous.TaskVersion)
	} else if previous.ExecutionContractVersion == agentRunExecutionContractVersionV2 ||
		previous.ExecutionContractVersion == agentRunExecutionContractVersionV3 {
		snapshot, err := parseAgentRunV2Snapshot(previous.InputSnapshotJSON)
		if err != nil {
			return prepared, previous, agentRunIdentityChangedError()
		}
		files = agentRunInputRequestsFromSnapshot(snapshot)
		output = agentRunOutputRequestFromSnapshot(snapshot)
	} else if previous.ExecutionContractVersion != agentRunExecutionContractVersion {
		return prepared, previous, agentRunIdentityChangedError()
	}
	var err error
	prepared, err = prepareAgentRun(tx, prepareAgentRunInput{TaskID: previous.TaskID, ProviderID: previous.ProviderID, Expected: &expected, InputFiles: files, OutputContract: output, ArtifactStore: store, Rework: rework})
	if err != nil {
		return prepared, previous, err
	}
	if previous.ExecutionContractVersion == agentRunExecutionContractVersion && prepared.InputSnapshotJSON != previous.InputSnapshotJSON {
		// Preserve the exact expanded v1 bytes written by the pre-v2 build.
		expanded, marshalErr := json.Marshal(prepared.Snapshot)
		if marshalErr == nil && string(expanded) == previous.InputSnapshotJSON {
			prepared.InputSnapshotJSON = previous.InputSnapshotJSON
		}
	}
	if prepared.InputSnapshotJSON != previous.InputSnapshotJSON || prepared.Provider.Model != previous.Model || prepared.ExecutionContractVersion != previous.ExecutionContractVersion {
		return prepared, previous, newProjectRequestError(409, agentRunIdentityChanged, "The frozen task or provider model changed; create a new run instead")
	}
	prepared.ParentRunID = &runID
	return prepared, previous, nil
}
