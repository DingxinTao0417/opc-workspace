package api

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
)

const agentRunExecutionContractVersionV5 int64 = 5

const agentRunExecutionContractVersionV6 int64 = 6

// v6 is only selected for explicit, accepted project-task file inputs. The
// separate contract keeps the ownership meaning of v1-v5 unchanged.
type agentRunV6InputSnapshot agentRunV5InputSnapshot

func isAgentRunModelContract(version int64) bool {
	return version == agentRunExecutionContractVersionV5 || version == agentRunExecutionContractVersionV6
}

func agentRunHasProjectTaskFiles(files []frozenAgentRunFileReference) bool {
	for _, file := range files {
		if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
			return true
		}
	}
	return false
}

func agentRunRequestsHaveProjectTaskFiles(files []agentRunInputFileRequest) bool {
	for _, file := range files {
		if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
			return true
		}
	}
	return false
}

func validateAgentRunProjectTaskFilesConfirmation(hasFiles, confirmed bool) error {
	if hasFiles && !confirmed {
		return newProjectRequestError(422, "CONFIRMATION_REQUIRED", "Independent confirmation is required to send accepted predecessor Task files to the Agent provider")
	}
	if !hasFiles && confirmed {
		return newProjectRequestError(422, "VALIDATION_ERROR", "Project Task file confirmation requires selected accepted predecessor files")
	}
	return nil
}

func parseAgentRunV6Snapshot(raw string) (agentRunV6InputSnapshot, error) {
	var snapshot agentRunV6InputSnapshot
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
	if err != nil || string(encoded) != raw || !validProtocol || !agentRunHasProjectTaskFiles(snapshot.Files) ||
		snapshot.Task.Version < 1 || snapshot.Task.Status != "todo" && snapshot.Task.Status != "in_progress" {
		return snapshot, agentRunIdentityChangedError()
	}
	if snapshot.Rework != nil && (snapshot.Task.Status != "in_progress" || snapshot.Task.ReviewPolicy != "manual" || agentexec.ValidateReworkContext(*snapshot.Rework) != nil) {
		return snapshot, agentRunIdentityChangedError()
	}
	if err := validateAgentRunFileSnapshotWithProjectTasks(agentRunV5InputSnapshot(snapshot).fileSnapshot(), true, true); err != nil {
		return snapshot, agentRunIdentityChangedError()
	}
	return snapshot, nil
}

func parseAgentRunModelSnapshot(version int64, raw string) (agentRunV5InputSnapshot, error) {
	if version == agentRunExecutionContractVersionV6 {
		snapshot, err := parseAgentRunV6Snapshot(raw)
		return agentRunV5InputSnapshot(snapshot), err
	}
	if version == agentRunExecutionContractVersionV5 {
		return parseAgentRunV5Snapshot(raw)
	}
	return agentRunV5InputSnapshot{}, agentRunIdentityChangedError()
}

// v1-v4 remain implicitly OpenAI. Only new remote Anthropic executions use
// this canonical snapshot; credentials and endpoints never enter durable input.
type agentRunV5InputSnapshot struct {
	Task            agentexec.TaskSnapshot        `json:"task"`
	ProviderKind    string                        `json:"provider_kind"`
	ModelProtocol   string                        `json:"model_protocol"`
	MaxOutputTokens int                           `json:"max_output_tokens"`
	Files           []frozenAgentRunFileReference `json:"files"`
	OutputContract  agentexec.OutputContract      `json:"output_contract"`
	Rework          *agentexec.ReworkContext      `json:"rework,omitempty"`
}

func (snapshot agentRunV5InputSnapshot) fileSnapshot() agentRunV2InputSnapshot {
	return agentRunV2InputSnapshot{Task: snapshot.Task, ProviderKind: snapshot.ProviderKind, Files: snapshot.Files, OutputContract: snapshot.OutputContract}
}

func parseAgentRunV5Snapshot(raw string) (agentRunV5InputSnapshot, error) {
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
	if err != nil || string(encoded) != raw || snapshot.Files == nil ||
		snapshot.ProviderKind != "remote" || snapshot.ModelProtocol != agentexec.ModelProtocolAnthropicMessages ||
		snapshot.MaxOutputTokens != agentexec.AnthropicMaxOutputTokens || snapshot.Task.Version < 1 ||
		snapshot.Task.Status != "todo" && snapshot.Task.Status != "in_progress" {
		return snapshot, agentRunIdentityChangedError()
	}
	if snapshot.Rework != nil && (snapshot.Task.Status != "in_progress" || snapshot.Task.ReviewPolicy != "manual" ||
		agentexec.ValidateReworkContext(*snapshot.Rework) != nil) {
		return snapshot, agentRunIdentityChangedError()
	}
	if err := validateAgentRunFileSnapshot(snapshot.fileSnapshot(), true); err != nil {
		return snapshot, agentRunIdentityChangedError()
	}
	return snapshot, nil
}
