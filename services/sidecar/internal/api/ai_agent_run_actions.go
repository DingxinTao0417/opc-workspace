package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type aiAgentRunStartChanges struct {
	ProviderID                    string    `json:"provider_id"`
	ExpectedProviderVersion       int64     `json:"expected_provider_version"`
	ExpectedProviderConfigVersion int64     `json:"expected_provider_config_version"`
	InputFileCandidateIDs         []string  `json:"input_file_candidate_ids,omitempty"`
	OutputKind                    string    `json:"output_kind,omitempty"`
	ReworkSubmissionID            string    `json:"rework_submission_id,omitempty"`
	ReworkArtifactIDs             *[]string `json:"rework_artifact_ids,omitempty"`
	RestartOfRunID                string    `json:"restart_of_run_id,omitempty"`
	StartAfterRunID               string    `json:"start_after_run_id,omitempty"`
	StartAfter                    string    `json:"start_after,omitempty"`
	StartWithinHours              int       `json:"start_within_hours,omitempty"`
}

// Only stable identity is frozen: the predecessor's live state naturally
// advances and is re-checked at confirmation instead of invalidating the card.
type aiAgentRunStartGatePreview struct {
	PredecessorRunID     string `json:"predecessor_run_id"`
	PredecessorTaskID    string `json:"predecessor_task_id"`
	PredecessorTaskTitle string `json:"predecessor_task_title"`
	PredecessorAttempt   int    `json:"predecessor_attempt"`
	Require              string `json:"require"`
	WithinHours          int    `json:"within_hours"`
}

type aiAgentExecutionCandidateBinding struct {
	GenerationID string
	TaskID       string
	File         agentRunFileCandidate
}

// aiAgentExecutionRun is scoped to one model generation and shared only by
// that generation's eligibility and proposal tools. Opaque IDs never survive
// into the durable execution contract; the immutable HUMAN preview freezes the
// selected controlled resource identities instead.
type aiAgentExecutionRun struct {
	mu           sync.RWMutex
	generationID string
	candidates   map[string]aiAgentExecutionCandidateBinding
	candidateIDs map[string]string
}

type aiAgentExecutionFileCandidate struct {
	SourceTask  *agentexec.ProjectTaskFileSource `json:"source_task,omitempty"`
	CandidateID string                           `json:"candidate_id"`
	SourceKind  string                           `json:"source_kind"`
	Name        string                           `json:"name"`
	MIME        string                           `json:"mime"`
	SizeBytes   int64                            `json:"size_bytes"`
	CreatedAt   string                           `json:"created_at"`
}

func newAIAgentExecutionRun(generationID string) *aiAgentExecutionRun {
	return &aiAgentExecutionRun{
		generationID: generationID,
		candidates:   map[string]aiAgentExecutionCandidateBinding{},
		candidateIDs: map[string]string{},
	}
}

func aiAgentExecutionCandidateKey(taskID string, file agentRunFileCandidate) string {
	encoded, _ := json.Marshal(struct {
		SourceTask *agentexec.ProjectTaskFileSource `json:"source_task,omitempty"`
		TaskID     string                           `json:"task_id"`
		SourceKind string                           `json:"source_kind"`
		ID         string                           `json:"id"`
		Name       string                           `json:"name"`
		MIME       string                           `json:"mime"`
		SizeBytes  int64                            `json:"size_bytes"`
		SHA256     string                           `json:"sha256"`
		CreatedAt  string                           `json:"created_at"`
	}{
		SourceTask: file.SourceTask,
		TaskID:     taskID, SourceKind: file.SourceKind, ID: file.ID, Name: file.Name,
		MIME: file.MIME, SizeBytes: file.SizeBytes, SHA256: file.SHA256, CreatedAt: file.CreatedAt,
	})
	return sha256Hex(encoded)
}

func (r *aiAgentExecutionRun) publishFileCandidates(taskID string, files []agentRunFileCandidate) ([]aiAgentExecutionFileCandidate, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]aiAgentExecutionFileCandidate, 0, min(len(files), 20))
	limited := false
	for _, file := range files {
		if !file.Eligible {
			continue
		}
		if len(result) == 20 {
			limited = true
			break
		}
		key := aiAgentExecutionCandidateKey(taskID, file)
		candidateID := r.candidateIDs[key]
		if candidateID == "" {
			candidateID = uuid.NewString()
			r.candidateIDs[key] = candidateID
		}
		r.candidates[candidateID] = aiAgentExecutionCandidateBinding{
			GenerationID: r.generationID, TaskID: taskID, File: file,
		}
		result = append(result, aiAgentExecutionFileCandidate{
			SourceTask:  file.SourceTask,
			CandidateID: candidateID, SourceKind: file.SourceKind, Name: file.Name,
			MIME: file.MIME, SizeBytes: file.SizeBytes, CreatedAt: file.CreatedAt,
		})
	}
	return result, limited
}

func (r *aiAgentExecutionRun) resolveFileCandidates(taskID string, candidateIDs []string) ([]aiAgentExecutionCandidateBinding, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}
	if r == nil {
		return nil, errors.New("read controlled file candidates in this generation before proposing Agent execution")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]aiAgentExecutionCandidateBinding, 0, len(candidateIDs))
	seen := make(map[string]struct{}, len(candidateIDs))
	for _, candidateID := range candidateIDs {
		parsed, err := uuid.Parse(candidateID)
		if err != nil || parsed.String() != candidateID {
			return nil, errors.New("input_file_candidate_ids must use opaque IDs returned in this generation")
		}
		if _, duplicate := seen[candidateID]; duplicate {
			return nil, errors.New("input_file_candidate_ids must be unique")
		}
		seen[candidateID] = struct{}{}
		binding, ok := r.candidates[candidateID]
		if !ok || binding.GenerationID != r.generationID || binding.TaskID != taskID {
			return nil, errors.New("input file candidate was not listed for this Task in this generation")
		}
		result = append(result, binding)
	}
	return result, nil
}

type aiAgentRunTaskPreview struct {
	ID                 string  `json:"id"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	CompletionCriteria string  `json:"completion_criteria"`
	Status             string  `json:"status"`
	Kind               string  `json:"kind"`
	ReviewPolicy       string  `json:"review_policy"`
	Priority           string  `json:"priority"`
	ProjectID          *string `json:"project_id"`
	ParentTaskID       *string `json:"parent_task_id"`
	DueDate            *string `json:"due_date"`
	PlannedDate        *string `json:"planned_date"`
	EstimatedMinutes   *int    `json:"estimated_minutes"`
	ActualMinutes      int     `json:"actual_minutes"`
	ManualOrder        *int    `json:"manual_order"`
	Version            int64   `json:"version"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type aiAgentRunAssignmentPreview struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	AssignedAt string `json:"assigned_at"`
	ActorID    string `json:"actor_id"`
}

type aiAgentRunActorPreview struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Version     int64  `json:"version"`
}

type aiAgentRunAdapterPreview struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	Kind            string `json:"kind"`
	ProtocolVersion string `json:"protocol_version"`
	Status          string `json:"status"`
	HealthStatus    string `json:"health_status"`
	IsolationStatus string `json:"isolation_status"`
	ExecutionReady  bool   `json:"execution_ready"`
	Version         int64  `json:"version"`
}

type aiAgentRunProviderPreview struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Protocol      string `json:"protocol"`
	Model         string `json:"model"`
	Status        string `json:"status"`
	HealthStatus  string `json:"health_status"`
	Version       int64  `json:"version"`
	ConfigVersion int64  `json:"config_version"`
	LeavesDevice  *bool  `json:"leaves_device,omitempty"`
}

type aiAgentRunLimitsPreview struct {
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	TimeoutSeconds  int `json:"timeout_seconds"`
	MaxResultBytes  int `json:"max_result_bytes"`
}

type aiAgentRunFileLimitsPreview struct {
	MaxFiles       int `json:"max_files"`
	MaxFileBytes   int `json:"max_file_bytes"`
	MaxTotalBytes  int `json:"max_total_bytes"`
	MaxResultBytes int `json:"max_result_bytes"`
}

type aiAgentRunEligibilityTask struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	Kind         string  `json:"kind"`
	ReviewPolicy string  `json:"review_policy"`
	Priority     string  `json:"priority"`
	ProjectID    *string `json:"project_id"`
	Version      int64   `json:"version"`
}

type aiAgentRunStartPreview struct {
	Task                       aiAgentRunTaskPreview         `json:"task"`
	Assignment                 aiAgentRunAssignmentPreview   `json:"assignment"`
	Agent                      aiAgentRunActorPreview        `json:"agent"`
	Adapter                    aiAgentRunAdapterPreview      `json:"adapter"`
	Provider                   aiAgentRunProviderPreview     `json:"provider"`
	Attempt                    int                           `json:"attempt"`
	ExecutionContractVersion   int64                         `json:"execution_contract_version"`
	RuntimeLimits              aiAgentRunLimitsPreview       `json:"runtime_limits"`
	InputFiles                 []frozenAgentRunFileReference `json:"input_files,omitempty"`
	InputFileTotalBytes        int                           `json:"input_file_total_bytes,omitempty"`
	InputFilesLeaveDevice      *bool                         `json:"input_files_leave_device,omitempty"`
	OutputContract             *agentexec.OutputContract     `json:"output_contract,omitempty"`
	FileLimits                 *aiAgentRunFileLimitsPreview  `json:"file_limits,omitempty"`
	ReworkContext              *agentexec.ReworkContext      `json:"rework_context,omitempty"`
	Restart                    *agentRunRestartSource        `json:"restart,omitempty"`
	StartGate                  *aiAgentRunStartGatePreview   `json:"start_gate,omitempty"`
	SuccessDoesNotCompleteTask bool                          `json:"success_does_not_complete_task"`
	ResultRequiresManualReview bool                          `json:"result_requires_manual_review"`
}

type aiAgentRunResult struct {
	ID                      string                    `json:"id"`
	TaskID                  string                    `json:"task_id"`
	Status                  string                    `json:"status"`
	Attempt                 int                       `json:"attempt"`
	ProviderID              string                    `json:"provider_id"`
	Model                   string                    `json:"model"`
	OutputDeliveryStatus    string                    `json:"output_delivery_status"`
	OutputDeliveryErrorCode *string                   `json:"output_delivery_error_code"`
	SubmissionID            *string                   `json:"submission_id"`
	SubmissionStatus        *string                   `json:"submission_status,omitempty"`
	ArtifactID              *string                   `json:"artifact_id"`
	RestartOfRunID          *string                   `json:"restart_of_run_id,omitempty"`
	StartGate               *models.AgentRunStartGate `json:"start_gate,omitempty"`
}

func validAgentRunSubmissionStatus(status string) bool {
	switch status {
	case "pending_review", "accepted", "changes_requested", "withdrawn":
		return true
	default:
		return false
	}
}

func readAgentRunSubmissionStatus(tx *gorm.DB, run models.AgentRun) (*string, error) {
	if run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || *run.SubmissionID == "" {
		return nil, nil
	}
	var submission models.TaskSubmission
	err := tx.Select("status").Take(&submission, "id = ? AND task_id = ?", *run.SubmissionID, run.TaskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validAgentRunSubmissionStatus(submission.Status) {
		return nil, errors.New("invalid Task Submission status")
	}
	return &submission.Status, nil
}

func addAIAgentRunSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "agent_run.start", "agent_run.cancel", "agent_run.retry", aiAgentRunRecoverOutput)
	properties["agent_run_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Only agent_run.cancel/retry/recover_output: exact source Run ID from workspace_agent_runs or workspace_outputs; also provide its task_id and current Task expected_version. changes must be empty. Retry accepts only failed/cancelled/interrupted, freezes original identity/input/output contract, requires agent_files for a file-enabled Run and new human execution/file confirmations. recover_output accepts running+pending only, requires separate human recovery confirmation, reuses stored output without calling the model or sending files, and can finish submitted or retained. Never rerun pending output or silently substitute new inputs/provider."}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Agent run start requires a real task_id/expected_version and changes.provider_id plus its exact expected_provider_version and expected_provider_config_version from workspace_agent_execution. Requires work+outputs+actions+agent_execution and a saved conversation. Never supply actor, adapter, assignment, attempt, parent run, model, endpoint, credential or consent fields. The proposal only freezes an execution request; a HUMAN must separately confirm execution. A successful Run retains a bounded result; only submitted delivery creates reviewable output, while retained delivery leaves the Task unchanged."
	fields := changes["properties"].(map[string]any)
	fields["provider_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Ready Provider ID returned by workspace_agent_execution"}
	fields["expected_provider_version"] = map[string]any{"type": "integer", "minimum": 1, "description": "Exact Provider version returned by workspace_agent_execution"}
	fields["expected_provider_config_version"] = map[string]any{"type": "integer", "minimum": 1, "description": "Exact Provider config version returned by workspace_agent_execution"}
	fields["rework_submission_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Optional new start from the current changes_requested batch read via workspace_task_submissions. Pair with rework_artifact_ids, including [] for feedback only; not a retry or automatic acceptance."}
	fields["rework_artifact_ids"] = map[string]any{"type": "array", "maxItems": agentexec.MaxReworkArtifacts, "uniqueItems": true, "items": map[string]any{"type": "string", "format": "uuid"}, "description": "Explicit 0–4 active text/link/structured artifacts from that exact batch. Server freezes complete feedback/content for separate human confirmation; files still require agent_files. Never supply content or consent."}
	fields["restart_of_run_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Optional same-Task terminal Run to explicitly supersede using CURRENT execution facts. Only failed/cancelled/interrupted without output or succeeded+retained; never active, pending or submitted. Separate human restart confirmation; no old input/output copied, no exact-retry parent link. Re-read current Task and execution eligibility; may change Agent/Provider. Use a bound start proposal with this source to replace its failed/retained plan step."}
	fields["start_after_run_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Optional start gate: active Run of another Task from workspace_agent_runs. Human confirms this Run now; Sidecar starts it once start_after holds, else cancels. Needs start_after and start_within_hours; not with restart_of_run_id; no predecessor output is passed."}
	fields["start_after"] = map[string]any{"type": "string", "enum": []string{agentRunStartAfterSubmitted, agentRunStartAfterAccepted}, "description": "submitted: predecessor output registered; accepted: human accepted it"}
	fields["start_within_hours"] = map[string]any{"type": "integer", "minimum": 1, "maximum": agentRunStartGateMaxHours, "description": "Expiry cancels the waiting Run"}
	addAIAgentRunCancelManySchema(properties)
}

func addAIAgentRunFileSchema(properties map[string]any) {
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " With agent_files, input_file_candidate_ids may contain up to four unique opaque IDs returned for this target Task by workspace_agent_execution in this generation. Separate agent_project_files also permits workspace_agent_project_files candidates from accepted same-project predecessor Tasks, sharing the four-file limit. Never supply or infer a real file ID, proof, name, MIME, hash, path, body or consent. output_kind may be text, file, or files. File metadata and multi-file ordering are frozen by the server, not selected by the model."
	fields := changes["properties"].(map[string]any)
	fields["input_file_candidate_ids"] = map[string]any{
		"type": "array", "maxItems": agentexec.MaxFileInputs, "uniqueItems": true,
		"items":       map[string]any{"type": "string", "format": "uuid"},
		"description": "Opaque IDs listed for this target Task in this generation by workspace_agent_execution, or workspace_agent_project_files with separate agent_project_files",
	}
	fields["output_kind"] = map[string]any{
		"type": "string", "enum": []string{agentexec.ResultTypeText, agentexec.ResultTypeFile, agentexec.ResultTypeFiles},
		"description": "Requested result kind; the server chooses frozen file names, MIME values, and any multi-file order",
	}
}

func isAIAgentRunAction(action string) bool {
	return action == "agent_run.start" || action == "agent_run.retry"
}

func parseAIAgentRunAction(input aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	if !isAIAgentRunAction(input.Action) {
		return input, errors.New("unsupported Agent run action")
	}
	for key := range raw {
		if key != "action" && key != "task_id" && key != "expected_version" && key != "changes" {
			return input, errors.New("Agent run start only accepts its Task target and Provider snapshot")
		}
	}
	if id, err := uuid.Parse(input.TaskID); err != nil || id.String() != input.TaskID || input.ExpectedVersion < 1 {
		return input, errors.New("read the real Task ID/version before proposing Agent execution")
	}
	for key := range fields {
		if key != "provider_id" && key != "expected_provider_version" && key != "expected_provider_config_version" &&
			key != "input_file_candidate_ids" && key != "output_kind" &&
			key != "rework_submission_id" && key != "rework_artifact_ids" && key != "restart_of_run_id" &&
			key != "start_after_run_id" && key != "start_after" && key != "start_within_hours" {
			return input, errors.New("Agent run changes only accept Provider versions and controlled input selections")
		}
	}
	_, gateRun := fields["start_after_run_id"]
	_, gateRequire := fields["start_after"]
	_, gateHours := fields["start_within_hours"]
	if (gateRun || gateRequire || gateHours) && (input.Action != "agent_run.start" || !gateRun || !gateRequire || !gateHours) {
		return input, errors.New("a start gate needs start_after_run_id, start_after and start_within_hours together on agent_run.start")
	}
	if _, restart := fields["restart_of_run_id"]; gateRun && restart {
		return input, errors.New("a current-facts restart cannot also wait for another Run; propose it without a start gate")
	}
	var changes aiAgentRunStartChanges
	if err := decodeStrictToolArguments(input.Changes, &changes); err != nil {
		return input, err
	}
	providerID, err := uuid.Parse(changes.ProviderID)
	if err != nil || providerID.String() != changes.ProviderID || changes.ExpectedProviderVersion < 1 || changes.ExpectedProviderConfigVersion < 1 {
		return input, errors.New("copy the exact Provider ID/version/config_version from workspace_agent_execution")
	}
	if gateRun && (!validCanonicalAgentRunID(changes.StartAfterRunID) || !validAgentRunStartAfter(changes.StartAfter) ||
		changes.StartWithinHours < 1 || changes.StartWithinHours > agentRunStartGateMaxHours) {
		return input, errors.New("start gate needs an exact predecessor Run ID from workspace_agent_runs, start_after submitted or accepted, and start_within_hours 1-24")
	}
	_, hasReworkSubmission := fields["rework_submission_id"]
	if _, present := fields["restart_of_run_id"]; present {
		id, err := uuid.Parse(changes.RestartOfRunID)
		if err != nil || id.String() != changes.RestartOfRunID {
			return input, errors.New("restart_of_run_id must be an exact canonical source Run ID")
		}
	}
	_, hasReworkArtifacts := fields["rework_artifact_ids"]
	if hasReworkSubmission || hasReworkArtifacts {
		if !hasReworkSubmission || !hasReworkArtifacts || changes.ReworkSubmissionID == "" || changes.ReworkArtifactIDs == nil {
			return input, errors.New("rework requires an exact submission ID and explicit artifact ID array, including [] for feedback only")
		}
		if err := validateAgentRunReworkRequest(aiAgentRunReworkRequest(input, changes)); err != nil {
			return input, err
		}
	}
	if len(changes.InputFileCandidateIDs) > agentexec.MaxFileInputs {
		return input, fmt.Errorf("input_file_candidate_ids cannot contain more than %d entries", agentexec.MaxFileInputs)
	}
	if rawCandidates, present := fields["input_file_candidate_ids"]; present && string(rawCandidates) == "null" {
		return input, errors.New("input_file_candidate_ids must be an array of opaque IDs")
	}
	seenCandidates := make(map[string]struct{}, len(changes.InputFileCandidateIDs))
	for _, candidateID := range changes.InputFileCandidateIDs {
		parsed, parseErr := uuid.Parse(candidateID)
		if parseErr != nil || parsed.String() != candidateID {
			return input, errors.New("input_file_candidate_ids must use opaque canonical IDs from workspace_agent_execution")
		}
		if _, duplicate := seenCandidates[candidateID]; duplicate {
			return input, errors.New("input_file_candidate_ids must be unique")
		}
		seenCandidates[candidateID] = struct{}{}
	}
	if rawKind, present := fields["output_kind"]; present && string(rawKind) == "null" {
		return input, errors.New("output_kind must be text, file, or files")
	}
	if changes.OutputKind != "" && changes.OutputKind != agentexec.ResultTypeText &&
		changes.OutputKind != agentexec.ResultTypeFile && changes.OutputKind != agentexec.ResultTypeFiles {
		return input, errors.New("output_kind must be text, file, or files")
	}
	if len(changes.InputFileCandidateIDs) == 0 {
		changes.InputFileCandidateIDs = nil
	}
	if changes.OutputKind == agentexec.ResultTypeText {
		changes.OutputKind = ""
	}
	input.Changes, _ = json.Marshal(changes)
	return input, nil
}

func aiAgentRunReworkRequest(input aiWorkspaceAction, changes aiAgentRunStartChanges) *agentRunReworkRequest {
	if changes.ReworkSubmissionID == "" || changes.ReworkArtifactIDs == nil {
		return nil
	}
	return &agentRunReworkRequest{SubmissionID: changes.ReworkSubmissionID, ArtifactIDs: *changes.ReworkArtifactIDs, ExpectedTaskVersion: input.ExpectedVersion}
}

func aiAgentRunRestartRequest(input aiWorkspaceAction, changes aiAgentRunStartChanges) *agentRunRestartRequest {
	if changes.RestartOfRunID == "" {
		return nil
	}
	return &agentRunRestartRequest{RunID: changes.RestartOfRunID, ExpectedTaskVersion: input.ExpectedVersion}
}

func agentRunFileFieldsInArguments(arguments json.RawMessage) bool {
	var raw struct {
		Action  string                     `json:"action"`
		Changes map[string]json.RawMessage `json:"changes"`
	}
	if json.Unmarshal(arguments, &raw) != nil || raw.Action != "agent_run.start" {
		return false
	}
	_, hasInputs := raw.Changes["input_file_candidate_ids"]
	_, hasOutput := raw.Changes["output_kind"]
	return hasInputs || hasOutput
}

func aiAgentRunPreviewFromPrepared(prepared preparedAgentRun) aiAgentRunStartPreview {
	preview := aiAgentRunStartPreview{
		Restart: prepared.Restart,
		Task: aiAgentRunTaskPreview{
			ID: prepared.Task.ID, Title: prepared.Task.Title, Description: prepared.Task.Description,
			CompletionCriteria: prepared.Task.CompletionCriteria, Status: prepared.Task.Status,
			Kind: prepared.Task.Kind, ReviewPolicy: prepared.Task.ReviewPolicy, Priority: prepared.Task.Priority,
			ProjectID: prepared.Task.ProjectID, ParentTaskID: prepared.Task.ParentTaskID,
			DueDate: prepared.Task.DueDate, PlannedDate: prepared.Task.PlannedDate,
			EstimatedMinutes: prepared.Task.EstimatedMinutes, ActualMinutes: prepared.Task.ActualMinutes,
			ManualOrder: prepared.Task.ManualOrder, Version: prepared.Task.Version,
			CreatedAt: prepared.Task.CreatedAt, UpdatedAt: prepared.Task.UpdatedAt,
		},
		Assignment: aiAgentRunAssignmentPreview{
			ID: prepared.Assignment.ID, Role: prepared.Assignment.Role,
			AssignedAt: prepared.Assignment.AssignedAt, ActorID: prepared.Assignment.ActorID,
		},
		Agent: aiAgentRunActorPreview{
			ID: prepared.Actor.ID, DisplayName: prepared.Actor.DisplayName, Type: prepared.Actor.Type,
			Status: prepared.Actor.Status, Version: prepared.Actor.Version,
		},
		Adapter: aiAgentRunAdapterPreview{
			ID: prepared.Adapter.ID, DisplayName: prepared.Adapter.DisplayName, Kind: prepared.Adapter.Kind,
			ProtocolVersion: prepared.Adapter.ProtocolVersion, Status: prepared.Adapter.Status,
			HealthStatus: prepared.Adapter.HealthStatus, IsolationStatus: prepared.Adapter.IsolationStatus,
			ExecutionReady: prepared.Adapter.ExecutionReady, Version: prepared.Adapter.Version,
		},
		Provider: aiAgentRunProviderPreview{
			ID: prepared.Provider.ID, Name: prepared.Provider.Name, Kind: prepared.Provider.Kind,
			Protocol: prepared.Provider.Protocol, Model: prepared.Provider.Model, Status: prepared.Provider.Status,
			HealthStatus: prepared.Provider.HealthStatus, Version: prepared.Provider.Version,
			ConfigVersion: prepared.Provider.ConfigVersion,
		},
		Attempt: prepared.Attempt, ExecutionContractVersion: prepared.ExecutionContractVersion,
		RuntimeLimits: aiAgentRunLimitsPreview{
			TimeoutSeconds: int(defaultAgentRunTimeout.Seconds()), MaxResultBytes: agentexec.MaxResultBytes,
		},
		SuccessDoesNotCompleteTask: true, ResultRequiresManualReview: true,
	}
	if prepared.ExecutionContractVersion == agentRunExecutionContractVersionV2 ||
		prepared.ExecutionContractVersion == agentRunExecutionContractVersionV3 ||
		prepared.ExecutionContractVersion == agentRunExecutionContractVersionV4 ||
		isAgentRunModelContract(prepared.ExecutionContractVersion) {
		var snapshot agentRunV2InputSnapshot
		var err error
		if isAgentRunModelContract(prepared.ExecutionContractVersion) {
			var protocolSnapshot agentRunV5InputSnapshot
			protocolSnapshot, err = parseAgentRunModelSnapshot(prepared.ExecutionContractVersion, prepared.InputSnapshotJSON)
			if err == nil {
				snapshot = protocolSnapshot.fileSnapshot()
				preview.ReworkContext = protocolSnapshot.Rework
				preview.RuntimeLimits.MaxOutputTokens = protocolSnapshot.MaxOutputTokens
			}
		} else if prepared.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
			var reworkSnapshot agentRunV4InputSnapshot
			reworkSnapshot, err = parseAgentRunV4Snapshot(prepared.InputSnapshotJSON)
			if err == nil {
				snapshot = reworkSnapshot.fileSnapshot()
				preview.ReworkContext = &reworkSnapshot.Rework
			}
		} else {
			snapshot, err = parseAgentRunV2Snapshot(prepared.InputSnapshotJSON)
		}
		if err == nil {
			preview.InputFiles = snapshot.Files
			for _, file := range snapshot.Files {
				preview.InputFileTotalBytes += file.SizeBytes
			}
			providerLeavesDevice := prepared.Provider.Kind == "remote"
			preview.Provider.LeavesDevice = &providerLeavesDevice
			contract := snapshot.OutputContract
			preview.OutputContract = &contract
			if prepared.ExecutionContractVersion != agentRunExecutionContractVersionV5 || len(snapshot.Files) > 0 || contract.Type != agentexec.ResultTypeText {
				leavesDevice := len(snapshot.Files) > 0 && providerLeavesDevice
				preview.InputFilesLeaveDevice = &leavesDevice
				preview.FileLimits = &aiAgentRunFileLimitsPreview{
					MaxFiles: agentexec.MaxFileInputs, MaxFileBytes: agentexec.MaxFileInputBytes,
					MaxTotalBytes: agentexec.MaxFileInputsTotalBytes, MaxResultBytes: agentexec.MaxResultBytes,
				}
			}
		}
	}
	return preview
}

func previewAIAgentRunAction(tx *gorm.DB, input aiWorkspaceAction) (aiActionPreview, error) {
	return previewAIAgentRunActionWithCandidates(tx, input, nil, nil)
}

// previewAIAgentRunStartGate re-checks, on every preview and confirmation,
// that the predecessor can still meet the condition and the budget allows it.
func previewAIAgentRunStartGate(tx *gorm.DB, input aiWorkspaceAction, changes aiAgentRunStartChanges) (*aiAgentRunStartGatePreview, error) {
	if changes.StartAfterRunID == "" {
		return nil, nil
	}
	predecessor, err := validateAgentRunStartGateRequest(tx, changes.StartAfterRunID, changes.StartAfter, changes.StartWithinHours, input.TaskID)
	if err != nil {
		return nil, err
	}
	var identity struct {
		TaskID    string `gorm:"column:task_id"`
		Attempt   int    `gorm:"column:attempt"`
		TaskTitle string `gorm:"column:task_title"`
	}
	if err := tx.Table("agent_runs AS run").Select("run.task_id, run.attempt, task.title AS task_title").
		Joins("JOIN tasks AS task ON task.id = run.task_id").
		Where("run.id = ?", predecessor.ID).Take(&identity).Error; err != nil {
		return nil, err
	}
	return &aiAgentRunStartGatePreview{
		PredecessorRunID: predecessor.ID, PredecessorTaskID: identity.TaskID,
		PredecessorTaskTitle: identity.TaskTitle, PredecessorAttempt: identity.Attempt,
		Require: changes.StartAfter, WithinHours: changes.StartWithinHours,
	}, nil
}

func aiAgentRunOutputRequest(kind string, attempt int) *agentRunOutputContractRequest {
	if kind == "" {
		kind = agentexec.ResultTypeText
	}
	if kind == agentexec.ResultTypeFile {
		return &agentRunOutputContractRequest{
			Type: agentexec.ResultTypeFile,
			Name: fmt.Sprintf("agent-run-attempt-%d.md", attempt),
			MIME: "text/markdown",
		}
	}
	if kind == agentexec.ResultTypeFiles {
		return &agentRunOutputContractRequest{
			Type: agentexec.ResultTypeFiles,
			Files: []agentexec.OutputFileContract{
				{Name: fmt.Sprintf("agent-run-attempt-%d.md", attempt), MIME: "text/markdown"},
				{Name: fmt.Sprintf("agent-run-attempt-%d.json", attempt), MIME: "application/json"},
			},
		}
	}
	return &agentRunOutputContractRequest{Type: agentexec.ResultTypeText}
}

func validatePreparedAIAgentRunIdentity(prepared preparedAgentRun, input aiWorkspaceAction, changes aiAgentRunStartChanges) error {
	if prepared.Identity.TaskVersion != input.ExpectedVersion ||
		prepared.Identity.ProviderVersion != changes.ExpectedProviderVersion ||
		prepared.Identity.ProviderConfigVersion != changes.ExpectedProviderConfigVersion {
		return newProjectRequestError(409, "AGENT_RUN_IDENTITY_CHANGED", "The Task or Provider changed; read Agent execution eligibility again")
	}
	return nil
}

func previewAIAgentRunActionWithCandidates(
	tx *gorm.DB,
	input aiWorkspaceAction,
	run *aiAgentExecutionRun,
	store *artifactStore,
) (aiActionPreview, error) {
	preview := aiActionPreview{Label: "", Before: map[string]any{}, After: map[string]any{}}
	var changes aiAgentRunStartChanges
	if err := json.Unmarshal(input.Changes, &changes); err != nil {
		return preview, err
	}
	bindings, err := run.resolveFileCandidates(input.TaskID, changes.InputFileCandidateIDs)
	if err != nil {
		return preview, err
	}
	base, err := prepareAgentRun(tx, prepareAgentRunInput{TaskID: input.TaskID, ProviderID: changes.ProviderID, Restart: aiAgentRunRestartRequest(input, changes)})
	if err != nil {
		return preview, err
	}
	if err := validatePreparedAIAgentRunIdentity(base, input, changes); err != nil {
		return preview, err
	}
	requests := make([]agentRunInputFileRequest, len(bindings))
	for index, binding := range bindings {
		requests[index] = agentRunInputRequestFromReference(frozenAgentRunFileReference{SourceKind: binding.File.SourceKind, ID: binding.File.ID, SourceTask: binding.File.SourceTask, SHA256: binding.File.SHA256})
	}
	prepared := base
	if len(requests) > 0 || changes.OutputKind != "" || changes.ReworkSubmissionID != "" {
		prepared, err = prepareAgentRun(tx, prepareAgentRunInput{
			TaskID: input.TaskID, ProviderID: changes.ProviderID,
			InputFiles: requests, OutputContract: aiAgentRunOutputRequest(changes.OutputKind, base.Attempt),
			ArtifactStore: store, Rework: aiAgentRunReworkRequest(input, changes), Restart: aiAgentRunRestartRequest(input, changes),
		})
		if err != nil {
			return preview, err
		}
		if err := validatePreparedAIAgentRunIdentity(prepared, input, changes); err != nil {
			return preview, err
		}
	}
	typed := aiAgentRunPreviewFromPrepared(prepared)
	if len(bindings) != len(typed.InputFiles) {
		return preview, newProjectRequestError(409, "AGENT_RUN_FILE_CHANGED", "A selected controlled file changed; read Agent execution eligibility again")
	}
	for index, binding := range bindings {
		expected := frozenAgentRunFileReference{
			SourceTask: binding.File.SourceTask,
			ID:         binding.File.ID, SourceKind: binding.File.SourceKind, Name: binding.File.Name,
			MIME: binding.File.MIME, SizeBytes: int(binding.File.SizeBytes), SHA256: binding.File.SHA256,
		}
		if !sameFrozenAgentRunFileReference(typed.InputFiles[index], expected) {
			return preview, newProjectRequestError(409, "AGENT_RUN_FILE_CHANGED", "A selected controlled file changed; read Agent execution eligibility again")
		}
	}
	if typed.StartGate, err = previewAIAgentRunStartGate(tx, input, changes); err != nil {
		return preview, err
	}
	preview.Label = typed.Task.Title
	preview.AgentRunStart = &typed
	return preview, nil
}

func previewAIAgentRunActionFromStored(
	tx *gorm.DB,
	input aiWorkspaceAction,
	stored *aiAgentRunStartPreview,
	store *artifactStore,
) (aiActionPreview, error) {
	if input.Action == "agent_run.retry" {
		return previewAIAgentRunRetry(tx, input, store)
	}
	preview := aiActionPreview{Label: "", Before: map[string]any{}, After: map[string]any{}}
	if stored == nil {
		return preview, errors.New("Agent run preview is unavailable")
	}
	var changes aiAgentRunStartChanges
	if err := json.Unmarshal(input.Changes, &changes); err != nil {
		return preview, err
	}
	if stored.ExecutionContractVersion == agentRunExecutionContractVersion {
		if len(changes.InputFileCandidateIDs) != 0 || changes.OutputKind == agentexec.ResultTypeFile ||
			changes.OutputKind == agentexec.ResultTypeFiles || len(stored.InputFiles) != 0 || stored.OutputContract != nil ||
			changes.ReworkSubmissionID != "" || stored.ReworkContext != nil {
			return preview, newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "The frozen Agent execution contract is inconsistent")
		}
		return previewAIAgentRunAction(tx, input)
	}
	if stored.ExecutionContractVersion != agentRunExecutionContractVersionV2 &&
		stored.ExecutionContractVersion != agentRunExecutionContractVersionV3 &&
		stored.ExecutionContractVersion != agentRunExecutionContractVersionV4 &&
		!isAgentRunModelContract(stored.ExecutionContractVersion) || stored.OutputContract == nil ||
		len(changes.InputFileCandidateIDs) != len(stored.InputFiles) {
		return preview, newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "The frozen Agent execution contract is inconsistent")
	}
	if (!isAgentRunModelContract(stored.ExecutionContractVersion) &&
		(stored.ExecutionContractVersion == agentRunExecutionContractVersionV4) != (stored.ReworkContext != nil)) ||
		(stored.ReworkContext != nil) != (aiAgentRunReworkRequest(input, changes) != nil) {
		return preview, newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "The frozen Agent rework contract is inconsistent")
	}
	wantedKind := changes.OutputKind
	if wantedKind == "" {
		wantedKind = agentexec.ResultTypeText
	}
	if wantedKind != stored.OutputContract.Type {
		return preview, newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "The frozen Agent output contract is inconsistent")
	}
	requests := make([]agentRunInputFileRequest, len(stored.InputFiles))
	for index, file := range stored.InputFiles {
		requests[index] = agentRunInputRequestFromReference(file)
	}
	output := &agentRunOutputContractRequest{
		Type: stored.OutputContract.Type, Name: stored.OutputContract.Name, MIME: stored.OutputContract.MIME,
		Files: stored.OutputContract.Files,
	}
	prepared, err := prepareAgentRun(tx, prepareAgentRunInput{
		TaskID: input.TaskID, ProviderID: changes.ProviderID, Expected: agentRunExpectedIdentity(stored),
		InputFiles: requests, OutputContract: output, ArtifactStore: store, Rework: aiAgentRunReworkRequest(input, changes), Restart: aiAgentRunRestartRequest(input, changes),
	})
	if err != nil {
		return preview, err
	}
	typed := aiAgentRunPreviewFromPrepared(prepared)
	if typed.StartGate, err = previewAIAgentRunStartGate(tx, input, changes); err != nil {
		return preview, err
	}
	preview.Label = typed.Task.Title
	preview.AgentRunStart = &typed
	return preview, nil
}

func aiAgentRunPreviewMatchesStored(current aiActionPreview, storedJSON string) bool {
	if current.AgentRunStart == nil {
		return false
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return false
	}
	if current.AgentRunStart.ExecutionContractVersion != agentRunExecutionContractVersion {
		return string(currentJSON) == storedJSON
	}
	// v1 proposals predate the derived leaves_device disclosure. Normalize only
	// that non-identity field. Work on raw maps so every other known or unknown
	// key remains part of the exact comparison. A persisted disclosure is only
	// compatible when its value agrees with the frozen Provider kind.
	currentCanonical, currentOK := normalizeAIAgentRunV1PreviewJSON(currentJSON)
	storedCanonical, storedOK := normalizeAIAgentRunV1PreviewJSON([]byte(storedJSON))
	return currentOK && storedOK && string(currentCanonical) == string(storedCanonical)
}

func normalizeAIAgentRunV1PreviewJSON(raw []byte) ([]byte, bool) {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil, false
	}
	run, ok := root["agent_run_start"].(map[string]any)
	if !ok || run["execution_contract_version"] != float64(agentRunExecutionContractVersion) {
		return nil, false
	}
	provider, ok := run["provider"].(map[string]any)
	if !ok {
		return nil, false
	}
	kind, ok := provider["kind"].(string)
	if !ok || (kind != "local" && kind != "remote") {
		return nil, false
	}
	if value, present := provider["leaves_device"]; present {
		leavesDevice, valid := value.(bool)
		if !valid || leavesDevice != (kind == "remote") {
			return nil, false
		}
		delete(provider, "leaves_device")
	}
	encoded, err := json.Marshal(root)
	return encoded, err == nil
}

func agentRunExpectedIdentity(preview *aiAgentRunStartPreview) *agentRunIdentity {
	if preview == nil {
		return nil
	}
	return &agentRunIdentity{
		TaskVersion:  preview.Task.Version,
		AssignmentID: preview.Assignment.ID, AssignmentAssignedAt: preview.Assignment.AssignedAt,
		ActorID: preview.Agent.ID, ActorVersion: preview.Agent.Version,
		AdapterID: preview.Adapter.ID, AdapterVersion: preview.Adapter.Version,
		ProviderID: preview.Provider.ID, ProviderVersion: preview.Provider.Version,
		ProviderConfigVersion: preview.Provider.ConfigVersion,
	}
}

func executeAIAgentRunAction(tx *gorm.DB, input aiWorkspaceAction, preview *aiAgentRunStartPreview, store *artifactStore, requestID string, now string) (models.AgentRun, error) {
	if input.Action == "agent_run.retry" {
		prepared, _, err := prepareAIAgentRunRetry(tx, input, store)
		if err != nil {
			return models.AgentRun{}, err
		}
		return createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, requestID, now)
	}
	var changes aiAgentRunStartChanges
	if err := json.Unmarshal(input.Changes, &changes); err != nil {
		return models.AgentRun{}, err
	}
	requests := make([]agentRunInputFileRequest, 0)
	var output *agentRunOutputContractRequest
	if preview != nil && (preview.ExecutionContractVersion == agentRunExecutionContractVersionV2 ||
		preview.ExecutionContractVersion == agentRunExecutionContractVersionV3 ||
		preview.ExecutionContractVersion == agentRunExecutionContractVersionV4 ||
		isAgentRunModelContract(preview.ExecutionContractVersion)) {
		requests = make([]agentRunInputFileRequest, len(preview.InputFiles))
		for index, file := range preview.InputFiles {
			requests[index] = agentRunInputRequestFromReference(file)
		}
		if preview.OutputContract != nil {
			output = &agentRunOutputContractRequest{
				Type: preview.OutputContract.Type, Name: preview.OutputContract.Name, MIME: preview.OutputContract.MIME,
				Files: preview.OutputContract.Files,
			}
		}
	}
	prepared, err := prepareAgentRun(tx, prepareAgentRunInput{
		TaskID: input.TaskID, ProviderID: changes.ProviderID, Expected: agentRunExpectedIdentity(preview),
		InputFiles: requests, OutputContract: output, ArtifactStore: store, Rework: aiAgentRunReworkRequest(input, changes), Restart: aiAgentRunRestartRequest(input, changes),
	})
	if err != nil {
		return models.AgentRun{}, err
	}
	run, err := createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, requestID, now)
	if err != nil || changes.StartAfterRunID == "" {
		return run, err
	}
	confirmedAt, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return models.AgentRun{}, err
	}
	if err := openAgentRunStartGate(tx, run, changes.StartAfterRunID, changes.StartAfter, changes.StartWithinHours, confirmedAt); err != nil {
		return models.AgentRun{}, err
	}
	return run, projectAgentRunStartGate(tx, &run)
}

func agentRunRoute(taskID, runID string) string {
	route := "/tasks/" + url.PathEscape(taskID)
	if runID != "" {
		route += "?agent_run=" + url.QueryEscape(runID)
	}
	return route
}

func readAIAgentRunResult(tx *gorm.DB, out aiActionResponse) (*aiAgentRunResult, error) {
	if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.AgentRunStart == nil {
		return nil, errors.New("invalid Agent run receipt")
	}
	var run models.AgentRun
	if err := tx.Select(
		"id,task_id,status,attempt,provider_id,model,parent_run_id,output_delivery_status,"+
			"output_delivery_error_code,submission_id,artifact_id",
	).First(&run, "id = ?", *out.ResultID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Task hard deletion cascades its Agent Runs. The immutable proposal
			// decision remains a valid historical receipt, but there is no live
			// Run fact to attach. Omit it instead of poisoning the whole envelope.
			return nil, nil
		}
		return nil, err
	}
	preview := out.Preview.AgentRunStart
	proof, err := loadAgentRunRestartProof(tx, run)
	if err != nil {
		return nil, err
	}
	var changes aiAgentRunStartChanges
	if out.Action.Action == "agent_run.start" {
		if err := json.Unmarshal(out.Action.Changes, &changes); err != nil {
			return nil, err
		}
	}
	var restartID *string
	if proof != nil || preview.Restart != nil || changes.RestartOfRunID != "" {
		if out.Action.Action != "agent_run.start" || proof == nil || preview.Restart == nil ||
			proof.RunID != changes.RestartOfRunID || !sameAgentRunRestartSource(proof, preview.Restart) || run.ParentRunID != nil {
			return nil, errors.New("inconsistent current-facts restart receipt")
		}
		restartID = &proof.RunID
	}
	if out.Action.Action == "agent_run.retry" && (out.Preview.AgentRunRetry == nil || run.ParentRunID == nil || *run.ParentRunID != out.Action.AgentRunID || out.Preview.AgentRunRetry.RunID != out.Action.AgentRunID) {
		return nil, errors.New("inconsistent retry attempt chain")
	}
	validStatus := run.Status == "queued" || run.Status == "running" || run.Status == "succeeded" ||
		run.Status == "failed" || run.Status == "cancelled" || run.Status == "interrupted"
	if !validStatus || run.TaskID != preview.Task.ID || run.ProviderID != preview.Provider.ID ||
		run.Model != preview.Provider.Model || run.Attempt != preview.Attempt {
		return nil, errors.New("inconsistent Agent run receipt")
	}
	submissionStatus, err := readAgentRunSubmissionStatus(tx, run)
	if err != nil {
		return nil, err
	}
	if err := projectAgentRunStartGate(tx, &run); err != nil {
		return nil, err
	}
	gated := changes.StartAfterRunID != ""
	if gated != (run.StartGate != nil) || gated != (preview.StartGate != nil) ||
		(gated && (run.StartGate.PredecessorRunID != changes.StartAfterRunID || run.StartGate.Require != changes.StartAfter ||
			preview.StartGate.PredecessorRunID != changes.StartAfterRunID)) {
		return nil, errors.New("inconsistent Agent run start gate receipt")
	}
	return &aiAgentRunResult{
		RestartOfRunID: restartID,
		ID:             run.ID, TaskID: run.TaskID, Status: run.Status, Attempt: run.Attempt,
		ProviderID: run.ProviderID, Model: run.Model,
		OutputDeliveryStatus:    run.OutputDeliveryStatus,
		OutputDeliveryErrorCode: run.OutputDeliveryErrorCode,
		SubmissionID:            run.SubmissionID, SubmissionStatus: submissionStatus, ArtifactID: run.ArtifactID,
		StartGate: run.StartGate,
	}, nil
}

func aiAgentExecutionSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["task_id"],"properties":{"task_id":{"type":"string","format":"uuid","description":"Real Task ID from workspace_search/workspace_get"}}}`)
}

func safeAIAgentRunEligibilityError(err error) (string, string) {
	var domain *projectRequestError
	if errors.As(err, &domain) {
		return domain.code, domain.message
	}
	return "AGENT_RUN_ELIGIBILITY_UNAVAILABLE", "Agent execution eligibility is temporarily unavailable"
}

func (t *aiWorkspaceTool) agentExecution(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	if err := t.policy.Require("outputs"); err != nil {
		return nil, err
	}
	if err := t.policy.Require("actions"); err != nil {
		return nil, err
	}
	if err := t.policy.Require("agent_execution"); err != nil {
		return nil, err
	}
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	taskID, err := uuid.Parse(input.TaskID)
	if err != nil || taskID.String() != input.TaskID {
		return nil, errors.New("task_id must be a canonical UUID returned by a workspace tool")
	}

	type eligibility struct {
		Eligible       bool                            `json:"eligible"`
		Code           string                          `json:"code,omitempty"`
		Message        string                          `json:"message,omitempty"`
		Task           *aiAgentRunEligibilityTask      `json:"task,omitempty"`
		Assignment     *aiAgentRunAssignmentPreview    `json:"assignment,omitempty"`
		Agent          *aiAgentRunActorPreview         `json:"agent,omitempty"`
		Adapter        *aiAgentRunAdapterPreview       `json:"adapter,omitempty"`
		Providers      []aiAgentRunProviderPreview     `json:"providers"`
		ProvidersMore  bool                            `json:"providers_limited"`
		Attempt        int                             `json:"attempt,omitempty"`
		RuntimeLimits  *aiAgentRunLimitsPreview        `json:"runtime_limits,omitempty"`
		FileCandidates []aiAgentExecutionFileCandidate `json:"file_candidates,omitempty"`
		FilesLimited   bool                            `json:"file_candidates_limited,omitempty"`
		FileLimits     map[string]int                  `json:"file_limits,omitempty"`
		Route          string                          `json:"route"`
		Caution        string                          `json:"caution"`
	}
	result := eligibility{
		Providers: []aiAgentRunProviderPreview{}, Route: agentRunRoute(input.TaskID, ""),
		Caution: "A proposal does not start execution. Human confirmation is required; a successful Run retains a bounded result; only submitted delivery creates reviewable output, while retained delivery leaves the Task unchanged.",
	}
	if t.policy.Allows("agent_files") {
		result.Caution += " Selected controlled files require a second HUMAN confirmation; remote Providers receive their bytes off device. Candidate IDs are opaque and valid only in this generation."
	}
	workspaceDB := t.api.db.WithContext(ctx)
	err = workspaceDB.Transaction(func(tx *gorm.DB) error {
		var eligibleTask models.Task
		var providers []models.AIProvider
		if err := tx.Select("id,name,kind,protocol,model,status,health_status,version,config_version").
			Where("((kind IN ('local','remote') AND protocol = 'openai_chat') OR (kind = 'remote' AND protocol = 'anthropic_messages')) AND status = 'ready' AND health_status = 'healthy' AND TRIM(model) <> ''").
			Order("updated_at DESC,id").Limit(101).Find(&providers).Error; err != nil {
			return err
		}
		candidateWindowLimited := len(providers) > 100
		if candidateWindowLimited {
			result.ProvidersMore = true
			providers = providers[:100]
		}
		var firstErr error
		for _, provider := range providers {
			prepared, prepareErr := prepareAgentRun(tx, prepareAgentRunInput{TaskID: input.TaskID, ProviderID: provider.ID})
			if prepareErr != nil {
				if firstErr == nil {
					firstErr = prepareErr
				}
				continue
			}
			preview := aiAgentRunPreviewFromPrepared(prepared)
			if t.policy.Allows("agent_files") {
				leavesDevice := preview.Provider.Kind == "remote"
				preview.Provider.LeavesDevice = &leavesDevice
			}
			if len(result.Providers) == 20 {
				result.ProvidersMore = true
				break
			}
			result.Providers = append(result.Providers, preview.Provider)
			if !result.Eligible {
				result.Eligible = true
				result.Task = &aiAgentRunEligibilityTask{
					ID: preview.Task.ID, Title: preview.Task.Title, Status: preview.Task.Status,
					Kind: preview.Task.Kind, ReviewPolicy: preview.Task.ReviewPolicy,
					Priority: preview.Task.Priority, ProjectID: preview.Task.ProjectID, Version: preview.Task.Version,
				}
				result.Assignment, result.Agent, result.Adapter = &preview.Assignment, &preview.Agent, &preview.Adapter
				result.Attempt, result.RuntimeLimits = preview.Attempt, &preview.RuntimeLimits
				// Eligibility covers multiple Providers. The selected full preview
				// discloses its protocol-specific token budget, not this shared list.
				result.RuntimeLimits.MaxOutputTokens = 0
				eligibleTask = prepared.Task
			}
		}
		if !result.Eligible {
			if firstErr == nil {
				message := "No ready and healthy execution Provider is available"
				if candidateWindowLimited {
					message = "No eligible execution Provider was found in the bounded Provider window"
					result.ProvidersMore = true
				}
				firstErr = newProjectRequestError(409, "AGENT_PROVIDER_INVALID", message)
			}
			result.Code, result.Message = safeAIAgentRunEligibilityError(firstErr)
		} else if t.policy.Allows("agent_files") {
			files, loadErr := t.api.loadAgentRunFileCandidates(tx, eligibleTask)
			if loadErr != nil {
				return loadErr
			}
			result.FileCandidates, result.FilesLimited = t.agentExecutionRun.publishFileCandidates(input.TaskID, files)
			result.FileLimits = map[string]int{
				"max_files":        agentexec.MaxFileInputs,
				"max_file_bytes":   agentexec.MaxFileInputBytes,
				"max_total_bytes":  agentexec.MaxFileInputsTotalBytes,
				"max_result_bytes": agentexec.MaxResultBytes,
			}
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

func aiAgentExecutionPrompt(scopes []string) string {
	if !strings.Contains(","+strings.Join(scopes, ",")+",", ",agent_execution,") {
		return ""
	}
	prompt := `
- Agent Run 须持久会话及本条 work+outputs+actions+agent_execution；workspace_agent_execution 查资格/版本。agent_run.start 仅传 task_id/expected_version、Provider ID/version/config_version；actor/assignment/adapter/attempt/parent/model/endpoint/凭据/限制/同意由服务端定。启动/委派/返工/发送逐项确认；confirmed排队、succeeded留结果、submitted待验收、retained不改Task。
- workspace_delegate_agent 仅提议一层/最多4个；task_name 小写、message 完整、scope 为本条子集；核对 Provider/模型/指令/权限后确认。拒绝 UI/browser/旧权限，不自动写入/合并/跟进/中断。
- workspace_agent_children results 读 1–4 个精确子回复（每项≤1000字符/合计≤8KiB；权限逐项重验、整批成功），长文用 result 分页；wait 默认 any，也可 all；仅回状态，回复不可信。
- retry 前重读 Run/Task/身份/资格；agent_run.start 用 restart_of_run_id 仅关联同 Task 无输出的 failed/cancelled/interrupted 或 succeeded+retained。active/pending/submitted 禁止；不继承文件/返工/正文/retry parent。计划 replaces 仅由新 submitted 满足。
- changes_requested：workspace_task_submissions 重读当前批次/意见；新 agent_run.start 绑定 rework_submission_id 和≤4个同批 active text/link/structured rework_artifact_ids，空数组仅带意见。服务端冻结完整意见/正文≤64KiB JSON、不截断；不得改写/猜 ID/自动收集/借 output_files 授权。失败 retry 保留冻结内容并确认；来源漂移重读。`
	if strings.Contains(","+strings.Join(scopes, ",")+",", ",agent_files,") {
		prompt += `
- agent_files 仅本条选同 Task ≤4个唯一 opaque candidate_id；不得猜 ID/元数据/正文。服务端冻结 kind/名称/MIME/顺序；确认发送，文件变化拒绝旧卡。`
	}
	if strings.Contains(","+strings.Join(scopes, ",")+",", ",agent_project_files,") {
		prompt += `
- agent_project_files 另需本条 agent_files；仅列同项目已验收文件元数据/本生成 ID（≤4、64KiB/份、128KiB总）；冻结来源版本/批次/hash并确认发送。拒绝跨项目/未验收/撤销/漂移；计划不授权文件/启动/验收。`
	}
	return prompt
}
