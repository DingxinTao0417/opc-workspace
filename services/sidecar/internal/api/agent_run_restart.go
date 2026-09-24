package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const agentRunRestartInvalid = "AGENT_RUN_RESTART_INVALID"

type agentRunRestartRequest struct {
	RunID               string `json:"run_id"`
	ExpectedTaskVersion int64  `json:"expected_task_version"`
}

type agentRunRestartSource struct {
	RunID                string `json:"run_id"`
	TaskID               string `json:"task_id"`
	Status               string `json:"status"`
	OutputDeliveryStatus string `json:"output_delivery_status"`
	Attempt              int    `json:"attempt"`
	TaskVersion          int64  `json:"task_version"`
	ActorID              string `json:"actor_id"`
	ProviderID           string `json:"provider_id"`
	Model                string `json:"model"`
	CompletedAt          string `json:"completed_at"`
}

func restartInvalidError() error {
	return newProjectRequestError(http.StatusConflict, agentRunRestartInvalid, "The source does not prove an eligible terminal run for a new execution")
}

func (request *agentRunRestartRequest) UnmarshalJSON(raw []byte) error {
	fields, err := strictAgentRestartObject(raw, "run_id", "expected_task_version")
	if err != nil || len(fields) != 2 {
		return restartInvalidError()
	}
	if json.Unmarshal(fields["run_id"], &request.RunID) != nil || json.Unmarshal(fields["expected_task_version"], &request.ExpectedTaskVersion) != nil || !canonicalRestartID(request.RunID) || request.ExpectedTaskVersion < 1 {
		return restartInvalidError()
	}
	return nil
}

func canonicalRestartID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value
}

// Only exact canonical keys are accepted. This parser also protects event
// proofs from duplicate or case-folded fields; it never decodes source bodies.
func strictAgentRestartObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, restartInvalidError()
	}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, restartInvalidError()
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || fields[key] != nil {
			return nil, restartInvalidError()
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, restartInvalidError()
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, restartInvalidError()
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, restartInvalidError()
	}
	return fields, nil
}

// Terminal eligibility is independent of the old frozen execution identity:
// restarting deliberately prepares current facts and can use a new Provider.
// Retained output is checked internally, never copied into the new input.
func loadAgentRunRestartSource(tx *gorm.DB, taskID, runID string) (*agentRunRestartSource, error) {
	if !canonicalRestartID(taskID) || !canonicalRestartID(runID) {
		return nil, restartInvalidError()
	}
	var run models.AgentRun
	if err := tx.First(&run, "id=? AND task_id=?", runID, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, restartInvalidError()
		}
		return nil, err
	}
	if run.CompletedAt == nil || run.StartedAt == nil || run.Attempt < 1 || run.TaskVersion < 1 || !canonicalRestartID(run.ActorID) || !canonicalRestartID(run.ProviderID) || strings.TrimSpace(run.Model) == "" || len(run.Model) > 255 {
		return nil, restartInvalidError()
	}
	if _, err := time.Parse(time.RFC3339Nano, *run.CompletedAt); err != nil {
		return nil, restartInvalidError()
	}
	if run.SubmissionID != nil || run.ArtifactID != nil || run.OutputDeliveryPendingText != nil || run.OutputDeliveryPendingBytes != nil || run.OutputDeliveryPendingCompletedAt != nil {
		return nil, restartInvalidError()
	}
	switch run.Status {
	case "failed", "cancelled", "interrupted":
		if run.OutputDeliveryStatus != agentRunOutputNotReady || run.ResultText != nil || run.ResultBytes != nil || run.OutputDeliveryErrorCode != nil {
			return nil, restartInvalidError()
		}
		if run.Status == "failed" && (run.ErrorCode == nil || strings.TrimSpace(*run.ErrorCode) == "") || run.Status != "failed" && run.ErrorCode != nil {
			return nil, restartInvalidError()
		}
	case "succeeded":
		if run.OutputDeliveryStatus != agentRunOutputRetained || run.ErrorCode != nil || run.ResultText == nil || run.ResultBytes == nil || *run.ResultBytes != len(*run.ResultText) || *run.ResultBytes < 1 || *run.ResultBytes > agentexec.MaxResultBytes || !utf8.ValidString(*run.ResultText) || strings.ContainsRune(*run.ResultText, '\x00') || run.OutputDeliveryErrorCode == nil || strings.TrimSpace(*run.OutputDeliveryErrorCode) == "" {
			return nil, restartInvalidError()
		}
		contract, err := agentRunOutputContract(run)
		if err != nil {
			return nil, restartInvalidError()
		}
		if _, err := agentRunOutputBodies(contract, *run.ResultText); err != nil {
			return nil, restartInvalidError()
		}
	default:
		return nil, restartInvalidError()
	}
	return &agentRunRestartSource{RunID: run.ID, TaskID: run.TaskID, Status: run.Status, OutputDeliveryStatus: run.OutputDeliveryStatus, Attempt: run.Attempt, TaskVersion: run.TaskVersion, ActorID: run.ActorID, ProviderID: run.ProviderID, Model: run.Model, CompletedAt: *run.CompletedAt}, nil
}

type agentRunRestartEvent struct {
	SourceRun       agentRunRestartSource `json:"source_run"`
	ExecutionSHA256 string                `json:"execution_sha256"`
}

// The hash binds the event to the immutable execution, not changing status or
// delivery state. It contains no input text in the event and is never a grant.
func agentRunRestartExecutionHash(run models.AgentRun) string {
	encoded, _ := json.Marshal(struct {
		ID, TaskID, AssignmentID, ActorID, AdapterID, CreatedByActorID                                              string
		ProviderID, Model, AssignmentAssignedAt, InputSnapshotJSON, CreatedAt                                       string
		TaskVersion, ActorVersion, AdapterVersion, ProviderVersion, ProviderConfigVersion, ExecutionContractVersion int64
		Attempt                                                                                                     int
		ParentRunID                                                                                                 *string
	}{run.ID, run.TaskID, run.AssignmentID, run.ActorID, run.AdapterID, run.CreatedByActorID, run.ProviderID, run.Model, run.AssignmentAssignedAt, run.InputSnapshotJSON, run.CreatedAt, run.TaskVersion, run.ActorVersion, run.AdapterVersion, run.ProviderVersion, run.ProviderConfigVersion, run.ExecutionContractVersion, run.Attempt, run.ParentRunID})
	return sha256Hex(encoded)
}

func recordAgentRunRestart(tx *gorm.DB, run models.AgentRun, source *agentRunRestartSource, requestID, now string) error {
	if source == nil {
		return nil
	}
	if run.ParentRunID != nil || run.TaskID != source.TaskID || run.ID == source.RunID {
		return restartInvalidError()
	}
	actual, err := loadAgentRunRestartSource(tx, run.TaskID, source.RunID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, source) {
		return restartInvalidError()
	}
	return recordAgentRunWorkflowEvent(tx, "agent_run_restarted", run.ID, map[string]any{"source_run": source, "execution_sha256": agentRunRestartExecutionHash(run)}, requestID, now)
}

// A nil proof is an ordinary or exact-retry Run. Invalid/multiple evidence is
// never silently treated as an unrelated new run. The fast nil path reads no
// input/output body; callers may pass an ID-only model projection.
func loadAgentRunRestartProof(tx *gorm.DB, run models.AgentRun) (*agentRunRestartSource, error) {
	var events []struct {
		ID, AggregateType, AggregateID, Action, CreatedAt                                                 string
		ActorID, AgentRunID, AssignmentID, SubmissionID, ArtifactID, RequestID, PreviousJSON, CurrentJSON *string
		CommandSeq                                                                                        *int64
	}
	if err := tx.Table("workflow_events").Where("action=? AND (aggregate_id=? OR agent_run_id=?)", "agent_run_restarted", run.ID, run.ID).Limit(2).Find(&events).Error; err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	if len(events) != 1 {
		return nil, restartInvalidError()
	}
	event := events[0]
	if !canonicalRestartID(event.ID) || event.AggregateType != "agent_run" || event.AggregateID != run.ID || event.AgentRunID == nil || *event.AgentRunID != run.ID || event.ActorID == nil || *event.ActorID != models.BuiltinSystemActorID || event.AssignmentID != nil || event.SubmissionID != nil || event.ArtifactID != nil || event.PreviousJSON != nil || event.CurrentJSON == nil || event.CommandSeq != nil {
		return nil, restartInvalidError()
	}
	fields, err := strictAgentRestartObject([]byte(*event.CurrentJSON), "source_run", "execution_sha256")
	if err != nil || len(fields) != 2 {
		return nil, restartInvalidError()
	}
	sourceFields, err := strictAgentRestartObject(fields["source_run"], "run_id", "task_id", "status", "output_delivery_status", "attempt", "task_version", "actor_id", "provider_id", "model", "completed_at")
	if err != nil || len(sourceFields) != 10 {
		return nil, restartInvalidError()
	}
	var proof agentRunRestartEvent
	if json.Unmarshal([]byte(*event.CurrentJSON), &proof) != nil {
		return nil, restartInvalidError()
	}
	var current models.AgentRun
	if err := tx.First(&current, "id=?", run.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, restartInvalidError()
		}
		return nil, err
	}
	if current.ParentRunID != nil || current.ID == proof.SourceRun.RunID || current.TaskID != proof.SourceRun.TaskID || current.CreatedAt != event.CreatedAt || proof.ExecutionSHA256 != agentRunRestartExecutionHash(current) {
		return nil, restartInvalidError()
	}
	source, err := loadAgentRunRestartSource(tx, current.TaskID, proof.SourceRun.RunID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(*source, proof.SourceRun) {
		return nil, restartInvalidError()
	}
	return source, nil
}

// The restart event is a proof only after the full frozen execution/source
// checks above. Inbox projections must not hide a historical Run merely from
// a JSON source_run ID that happens to appear in a workflow event.
func verifiedAgentRunRestartSupersededIDs(tx *gorm.DB) ([]string, error) {
	var restartRunIDs []string
	if err := tx.Table("workflow_events").Distinct().
		Where("action = ?", "agent_run_restarted").
		Pluck("aggregate_id", &restartRunIDs).Error; err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(restartRunIDs))
	for _, runID := range restartRunIDs {
		proof, err := loadAgentRunRestartProof(tx, models.AgentRun{ID: runID})
		if err != nil {
			return nil, err
		}
		if proof != nil {
			sources = append(sources, proof.RunID)
		}
	}
	return sources, nil
}

func projectAgentRunRestart(tx *gorm.DB, run *models.AgentRun) error {
	run.RestartOfRunID = nil
	source, err := loadAgentRunRestartProof(tx, *run)
	if err != nil {
		return err
	}
	if source != nil {
		id := source.RunID
		run.RestartOfRunID = &id
	}
	return nil
}

type agentRunRestartPreviewRequest struct {
	ProviderID     string                         `json:"provider_id"`
	Restart        *agentRunRestartRequest        `json:"restart"`
	InputFiles     []agentRunInputFileRequest     `json:"input_files,omitempty"`
	OutputContract *agentRunOutputContractRequest `json:"output_contract,omitempty"`
	Rework         *agentRunReworkRequest         `json:"rework,omitempty"`
}

type agentRunRestartPreviewResponse struct {
	TaskID       string                 `json:"task_id"`
	TaskVersion  int64                  `json:"task_version"`
	SourceRun    *agentRunRestartSource `json:"source_run"`
	CurrentStart aiAgentRunStartPreview `json:"current_start"`
	Fingerprint  string                 `json:"fingerprint"`
}

func agentRunRestartPreview(prepared preparedAgentRun, selection agentRunRestartPreviewRequest) agentRunRestartPreviewResponse {
	preview := agentRunRestartPreviewResponse{TaskID: prepared.Task.ID, TaskVersion: prepared.Task.Version, SourceRun: prepared.Restart, CurrentStart: aiAgentRunPreviewFromPrepared(prepared)}
	encoded, _ := json.Marshal(struct {
		Preview           agentRunRestartPreviewResponse `json:"preview"`
		Selection         agentRunRestartPreviewRequest  `json:"selection"`
		InputSnapshotJSON string                         `json:"input_snapshot_json"`
	}{preview, selection, prepared.InputSnapshotJSON})
	preview.Fingerprint = sha256Hex(encoded)
	return preview
}

func (a *API) previewAgentRunRestart(c *gin.Context) {
	taskID, ok := taskID(c)
	if !ok {
		return
	}
	var input agentRunRestartPreviewRequest
	if decodeJSON(c, &input) != nil {
		writeError(c, 400, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if input.Restart == nil || !canonicalRestartID(input.ProviderID) {
		writeProjectRequestError(c, restartInvalidError())
		return
	}
	var response agentRunRestartPreviewResponse
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareAgentRun(tx, prepareAgentRunInput{TaskID: taskID, ProviderID: input.ProviderID, Restart: input.Restart, InputFiles: input.InputFiles, OutputContract: input.OutputContract, Rework: input.Rework, ArtifactStore: a.artifactStore})
		if err != nil {
			return err
		}
		response = agentRunRestartPreview(prepared, input)
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if !writeProjectRequestError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func validateNativeAgentRestartConfirmation(input createAgentRunRequest) error {
	if input.Restart == nil {
		if input.ConfirmRestart || input.RestartPreviewHash != "" {
			return restartInvalidError()
		}
		return nil
	}
	if input.AutoAssign != nil {
		return restartInvalidError()
	}
	if !input.ConfirmRestart || input.RestartPreviewHash == "" {
		return newProjectRequestError(422, "CONFIRMATION_REQUIRED", "Review the complete current execution preview and explicitly confirm this new execution")
	}
	if len(input.RestartPreviewHash) != 64 || strings.Trim(input.RestartPreviewHash, "0123456789abcdef") != "" {
		return restartInvalidError()
	}
	return nil
}
