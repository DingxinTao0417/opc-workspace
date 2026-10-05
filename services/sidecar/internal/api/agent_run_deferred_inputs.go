package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// ADR-033: a gated Run may declare that its file inputs are the files its
// predecessor Run will submit. Those files do not exist when the Run is
// confirmed, so the Run row freezes only the declaration (contract v7). After
// the predecessor is accepted, a person reviews the exact files and confirms
// sending them; that confirmation is the gate's release event, and it is the
// only place the file references are recorded. The Run row never changes.
const (
	agentRunExecutionContractVersionV7 int64 = 7

	agentRunDeferredInputPredecessorFiles = "predecessor_files"

	agentRunInputSlotAwaitingPredecessor  = "awaiting_predecessor"
	agentRunInputSlotAwaitingConfirmation = "awaiting_confirmation"
	agentRunInputSlotBound                = "bound"
	agentRunInputSlotClosed               = "closed"

	agentRunStartGateOutputUnavailable = "predecessor_output_unavailable"

	agentRunInputSlotNotFoundCode = "AGENT_RUN_INPUT_SLOT_NOT_FOUND"
	agentRunInputNotReadyCode     = "AGENT_RUN_INPUT_NOT_READY"
	agentRunInputUnavailableCode  = "AGENT_RUN_INPUT_UNAVAILABLE"
	agentRunInputChangedCode      = "AGENT_RUN_INPUT_CHANGED"
)

// agentRunDeferredInputs is the v7 declaration: which Run's submitted files
// will be the inputs, and the names and types that Run is contracted to write.
type agentRunDeferredInputs struct {
	Source           string                         `json:"source"`
	PredecessorRunID string                         `json:"predecessor_run_id"`
	ExpectedFiles    []agentexec.OutputFileContract `json:"expected_files"`
}

func validAgentRunDeferredInputs(slot agentRunDeferredInputs) bool {
	if slot.Source != agentRunDeferredInputPredecessorFiles || !validCanonicalAgentRunID(slot.PredecessorRunID) ||
		len(slot.ExpectedFiles) < 1 || len(slot.ExpectedFiles) > agentexec.MaxFileInputs {
		return false
	}
	seen := make(map[string]struct{}, len(slot.ExpectedFiles))
	for _, file := range slot.ExpectedFiles {
		if agentexec.ValidateTextFileNameAndMIME(file.Name, file.MIME) != nil {
			return false
		}
		name := strings.ToLower(file.Name)
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = struct{}{}
	}
	return true
}

func sameAgentRunDeferredInputs(left, right agentRunDeferredInputs) bool {
	if left.Source != right.Source || left.PredecessorRunID != right.PredecessorRunID ||
		len(left.ExpectedFiles) != len(right.ExpectedFiles) {
		return false
	}
	for index := range left.ExpectedFiles {
		if left.ExpectedFiles[index] != right.ExpectedFiles[index] {
			return false
		}
	}
	return true
}

func agentRunDeferredInputsError(message string) error {
	return newProjectRequestError(http.StatusUnprocessableEntity, agentRunStartGateInvalidCode, message)
}

// agentRunDeferredInputsFor derives the declaration from the predecessor's own
// frozen output contract. It also enforces what may be passed on at all: file
// outputs only, inside one Project, and never from a Run that is itself still
// waiting for inputs.
func agentRunDeferredInputsFor(tx *gorm.DB, targetTask models.Task, predecessorRunID, require string) (agentRunDeferredInputs, error) {
	var slot agentRunDeferredInputs
	if require != agentRunStartAfterAccepted {
		return slot, agentRunDeferredInputsError("Predecessor files can only be passed on after a person accepts them; use start_after accepted")
	}
	var predecessor models.AgentRun
	if err := tx.Select("id", "task_id", "execution_contract_version", "input_snapshot_json").
		Take(&predecessor, "id = ?", predecessorRunID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return slot, agentRunDeferredInputsError("The predecessor Run does not exist")
		}
		return slot, err
	}
	var predecessorTask models.Task
	if err := tx.Select("id", "project_id").Take(&predecessorTask, "id = ?", predecessor.TaskID).Error; err != nil {
		return slot, err
	}
	if targetTask.ProjectID == nil || *targetTask.ProjectID == "" || predecessorTask.ProjectID == nil ||
		*predecessorTask.ProjectID != *targetTask.ProjectID {
		return slot, agentRunDeferredInputsError("Predecessor files can only be passed on between Tasks of the same Project")
	}
	contract, err := agentRunOutputContract(predecessor)
	if err != nil {
		return slot, err
	}
	switch contract.Type {
	case agentexec.ResultTypeFile:
		slot.ExpectedFiles = []agentexec.OutputFileContract{{Name: contract.Name, MIME: contract.MIME}}
	case agentexec.ResultTypeFiles:
		slot.ExpectedFiles = append([]agentexec.OutputFileContract(nil), contract.Files...)
	default:
		return slot, agentRunDeferredInputsError("The predecessor Run produces text; only its file outputs can be passed on")
	}
	if predecessor.ExecutionContractVersion == agentRunExecutionContractVersionV7 {
		gates, err := readAgentRunStartGates(tx, []string{predecessor.ID})
		if err != nil {
			return slot, err
		}
		if gate := gates[predecessor.ID]; gate == nil || gate.Status == agentRunStartGateWaiting {
			return slot, agentRunDeferredInputsError("The predecessor Run is itself waiting for its inputs; pass files on one step at a time")
		}
	}
	slot.Source, slot.PredecessorRunID = agentRunDeferredInputPredecessorFiles, predecessor.ID
	if !validAgentRunDeferredInputs(slot) {
		return slot, agentRunDeferredInputsError("The predecessor Run's output files cannot be used as controlled inputs")
	}
	return slot, nil
}

func parseAgentRunV7Snapshot(raw string) (agentRunV5InputSnapshot, error) {
	var snapshot agentRunV5InputSnapshot
	if len(raw) > 262144 {
		return snapshot, agentRunIdentityChangedError()
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, agentRunIdentityChangedError()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return snapshot, agentRunIdentityChangedError()
	}
	encoded, err := json.Marshal(snapshot)
	validProtocol := (snapshot.ModelProtocol == agentexec.ModelProtocolOpenAIChat && snapshot.MaxOutputTokens == 0 &&
		(snapshot.ProviderKind == "local" || snapshot.ProviderKind == "remote")) ||
		(snapshot.ModelProtocol == agentexec.ModelProtocolAnthropicMessages && snapshot.MaxOutputTokens == agentexec.AnthropicMaxOutputTokens && snapshot.ProviderKind == "remote")
	// The declaration is the only input a v7 Run freezes: no static files, no
	// rework context, and a Project for the accepted files to come from.
	if err != nil || string(encoded) != raw || !validProtocol || snapshot.DeferredInputs == nil ||
		!validAgentRunDeferredInputs(*snapshot.DeferredInputs) || snapshot.Files == nil || len(snapshot.Files) != 0 ||
		snapshot.Rework != nil || snapshot.Task.ProjectID == nil || *snapshot.Task.ProjectID == "" ||
		snapshot.Task.Version < 1 || snapshot.Task.Status != "todo" && snapshot.Task.Status != "in_progress" {
		return snapshot, agentRunIdentityChangedError()
	}
	if err := validateAgentRunFileSnapshotWithProjectTasks(snapshot.fileSnapshot(), true, true); err != nil {
		return snapshot, agentRunIdentityChangedError()
	}
	return snapshot, nil
}

// agentRunStartGateBinding is what the release event of a gate with an input
// slot records: the exact files a person confirmed, the Provider identity they
// confirmed sending them to, and who confirmed.
type agentRunStartGateBinding struct {
	PredecessorRunID string                                 `json:"predecessor_run_id"`
	InputFiles       []frozenAgentRunFileReference          `json:"input_files"`
	Provider         agentRunFileAccessProviderConfirmation `json:"provider"`
	ConfirmedBy      string                                 `json:"confirmed_by"`
}

// agentRunFilesSatisfySlot reports whether the files are exactly what the
// declaration announced: the same count, order, names and types, all of them
// accepted files of another Task.
func agentRunFilesSatisfySlot(files []frozenAgentRunFileReference, slot agentRunDeferredInputs) bool {
	if len(files) != len(slot.ExpectedFiles) {
		return false
	}
	total := 0
	for index, file := range files {
		if file.SourceKind != agentexec.FileSourceProjectTaskArtifact || file.SourceTask == nil ||
			file.Name != slot.ExpectedFiles[index].Name || file.MIME != slot.ExpectedFiles[index].MIME {
			return false
		}
		total += file.SizeBytes
	}
	return total <= agentexec.MaxFileInputsTotalBytes
}

func decodeAgentRunStartGateBinding(raw string, slot agentRunDeferredInputs) (agentRunStartGateBinding, error) {
	var binding agentRunStartGateBinding
	if decodeStrictJSONBytes([]byte(raw), &binding) != nil || binding.PredecessorRunID != slot.PredecessorRunID ||
		!binding.Provider.valid() || !validCanonicalAgentRunID(binding.ConfirmedBy) ||
		!agentRunFilesSatisfySlot(binding.InputFiles, slot) {
		return binding, errors.New("invalid agent run start gate input binding")
	}
	return binding, nil
}

// loadAgentRunStartGateBinding returns the confirmed inputs of a v7 Run, or
// nil while nobody has confirmed them.
func loadAgentRunStartGateBinding(tx *gorm.DB, runID string, slot agentRunDeferredInputs) (*agentRunStartGateBinding, error) {
	var rows []agentRunStartGateEventRow
	if err := tx.Table("workflow_events").Select("aggregate_id, action, current_json").
		Where("aggregate_type = 'agent_run' AND aggregate_id = ? AND action = ?", runID, agentRunStartGateReleasedEvent).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 {
		return nil, errors.New("invalid agent run start gate release")
	}
	binding, err := decodeAgentRunStartGateBinding(rows[0].CurrentJSON, slot)
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// agentRunBoundInputFiles lists the files an active v7 Run is bound to, for
// the deletion interlocks. A v7 Run that nobody confirmed yet holds no file.
func agentRunBoundInputFiles(tx *gorm.DB, runID string, snapshot agentRunV5InputSnapshot) ([]frozenAgentRunFileReference, error) {
	if snapshot.DeferredInputs == nil {
		return snapshot.Files, nil
	}
	binding, err := loadAgentRunStartGateBinding(tx, runID, *snapshot.DeferredInputs)
	if err != nil || binding == nil {
		return nil, err
	}
	return binding.InputFiles, nil
}

func agentRunInputUnavailable() error {
	return newProjectRequestError(http.StatusConflict, agentRunInputUnavailableCode,
		"The predecessor's accepted files are no longer available as inputs")
}

// loadAgentRunPredecessorOutputRecords resolves, without reading any file
// body, the files the predecessor Run submitted, in the order it wrote them.
// Every one must currently be an accepted file of the predecessor's Task in the
// target's Project and match the declaration; otherwise the inputs are
// unavailable.
func loadAgentRunPredecessorOutputRecords(tx *gorm.DB, targetTask models.Task, slot agentRunDeferredInputs) ([]agentRunControlledFileRecord, error) {
	var predecessor models.AgentRun
	if err := tx.Select("id", "task_id", "status", "output_delivery_status", "submission_id").
		Take(&predecessor, "id = ?", slot.PredecessorRunID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, agentRunInputUnavailable()
		}
		return nil, err
	}
	if predecessor.Status != "succeeded" || predecessor.OutputDeliveryStatus != agentRunOutputSubmitted ||
		predecessor.SubmissionID == nil || *predecessor.SubmissionID == "" {
		return nil, agentRunInputUnavailable()
	}
	var events []agentRunStartGateEventRow
	if err := tx.Table("workflow_events").Select("aggregate_id, action, current_json").
		Where("aggregate_type = 'agent_run' AND aggregate_id = ? AND action = 'agent_run_output_submitted'", predecessor.ID).
		Scan(&events).Error; err != nil {
		return nil, err
	}
	var delivered struct {
		SubmissionID string   `json:"submission_id"`
		ArtifactIDs  []string `json:"artifact_ids"`
	}
	if len(events) != 1 || json.Unmarshal([]byte(events[0].CurrentJSON), &delivered) != nil ||
		delivered.SubmissionID != *predecessor.SubmissionID || len(delivered.ArtifactIDs) != len(slot.ExpectedFiles) {
		return nil, agentRunInputUnavailable()
	}
	records := make([]agentRunControlledFileRecord, 0, len(delivered.ArtifactIDs))
	references := make([]frozenAgentRunFileReference, 0, len(delivered.ArtifactIDs))
	for _, artifactID := range delivered.ArtifactIDs {
		record, err := loadAgentRunProjectTaskFileRecord(tx, targetTask, artifactID)
		if err != nil {
			var requestErr *projectRequestError
			if errors.As(err, &requestErr) {
				return nil, agentRunInputUnavailable()
			}
			return nil, err
		}
		if record.Reference.SourceTask == nil || record.Reference.SourceTask.TaskID != predecessor.TaskID ||
			record.Reference.SourceTask.SubmissionID != *predecessor.SubmissionID {
			return nil, agentRunInputUnavailable()
		}
		records = append(records, record)
		references = append(references, record.Reference)
	}
	if !agentRunFilesSatisfySlot(references, slot) {
		return nil, agentRunInputUnavailable()
	}
	return records, nil
}
