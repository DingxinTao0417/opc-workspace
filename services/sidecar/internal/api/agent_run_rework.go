package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	agentRunExecutionContractVersionV4 int64 = 4
	agentRunReworkInvalidCode                = "AGENT_RUN_REWORK_INVALID"
)

type agentRunReworkRequest struct {
	SubmissionID        string   `json:"submission_id"`
	ArtifactIDs         []string `json:"artifact_ids"`
	ExpectedTaskVersion int64    `json:"expected_task_version"`
}

// The new native selection is an exact identity, not a last-key-wins map.
// This does not change parsing or byte encoding of older execution requests.
func (request *agentRunReworkRequest) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return reworkInvalidError()
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || key != "submission_id" && key != "artifact_ids" && key != "expected_task_version" {
			return reworkInvalidError()
		}
		if _, exists := fields[key]; exists {
			return reworkInvalidError()
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if strings.TrimSpace(string(value)) == "null" {
			return reworkInvalidError()
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return reworkInvalidError()
	}
	if len(fields) != 3 {
		return reworkInvalidError()
	}
	type plain agentRunReworkRequest
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*request = agentRunReworkRequest(decoded)
	return nil
}

// A separate type preserves the byte encoding and meaning of v1/v2/v3.
type agentRunV4InputSnapshot struct {
	Task           agentexec.TaskSnapshot        `json:"task"`
	ProviderKind   string                        `json:"provider_kind"`
	Files          []frozenAgentRunFileReference `json:"files"`
	OutputContract agentexec.OutputContract      `json:"output_contract"`
	Rework         agentexec.ReworkContext       `json:"rework"`
}

func (snapshot agentRunV4InputSnapshot) fileSnapshot() agentRunV2InputSnapshot {
	return agentRunV2InputSnapshot{Task: snapshot.Task, ProviderKind: snapshot.ProviderKind, Files: snapshot.Files, OutputContract: snapshot.OutputContract}
}

func reworkInvalidError() error {
	return newProjectRequestError(http.StatusUnprocessableEntity, agentRunReworkInvalidCode,
		"Rework requires the current changes-requested submission and explicitly selected supported artifacts within the context limit")
}

func validateAgentRunReworkConfirmation(hasRework, confirmed bool, provider *agentRunFileAccessProviderConfirmation) error {
	if !hasRework {
		if confirmed || provider != nil {
			return reworkInvalidError()
		}
		return nil
	}
	if !confirmed || provider == nil {
		return newProjectRequestError(422, "CONFIRMATION_REQUIRED", "Explicit rework context and exact Agent Provider confirmation are required")
	}
	if !provider.valid() {
		return newProjectRequestError(422, "VALIDATION_ERROR", "Rework Provider confirmation must contain positive versions and kind local or remote")
	}
	return nil
}

func validateAgentRunReworkRequest(request *agentRunReworkRequest) error {
	if request == nil {
		return reworkInvalidError()
	}
	id, err := uuid.Parse(request.SubmissionID)
	if err != nil || id.String() != request.SubmissionID || request.ExpectedTaskVersion < 1 ||
		request.ArtifactIDs == nil || len(request.ArtifactIDs) > agentexec.MaxReworkArtifacts {
		return reworkInvalidError()
	}
	seen := make(map[string]bool, len(request.ArtifactIDs))
	for _, value := range request.ArtifactIDs {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value || seen[value] {
			return reworkInvalidError()
		}
		seen[value] = true
	}
	return nil
}

func freezeAgentRunRework(tx *gorm.DB, task models.Task, request *agentRunReworkRequest) (*agentexec.ReworkContext, error) {
	if err := validateAgentRunReworkRequest(request); err != nil {
		return nil, err
	}
	if task.Version != request.ExpectedTaskVersion {
		return nil, agentRunIdentityChangedError()
	}
	if task.Status != "in_progress" || task.ReviewPolicy != "manual" || task.CurrentSubmissionID == nil ||
		*task.CurrentSubmissionID != request.SubmissionID {
		return nil, reworkInvalidError()
	}
	var submission models.TaskSubmission
	if err := tx.Where("id = ? AND task_id = ?", request.SubmissionID, task.ID).First(&submission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, reworkInvalidError()
		}
		return nil, err
	}
	if submission.Status != "changes_requested" || submission.ReviewReason == nil || submission.ReviewedAt == nil {
		return nil, reworkInvalidError()
	}
	rework := &agentexec.ReworkContext{SubmissionID: submission.ID, Sequence: submission.Sequence, ReviewReason: *submission.ReviewReason, ReviewedAt: *submission.ReviewedAt, Artifacts: make([]agentexec.ReworkArtifact, 0, len(request.ArtifactIDs))}
	for _, id := range request.ArtifactIDs {
		var artifact models.TaskArtifact
		// No managed-file path or bytes are read by rework selection.
		if err := tx.Select("id", "task_id", "submission_id", "storage_kind", "name", "content_text", "reference_url", "structured_json", "deleted_at").Where("id = ? AND task_id = ? AND submission_id = ? AND deleted_at IS NULL", id, task.ID, submission.ID).First(&artifact).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, reworkInvalidError()
			}
			return nil, err
		}
		var content *string
		switch artifact.StorageKind {
		case "text":
			content = artifact.ContentText
		case "link":
			content = artifact.ReferenceURL
		case "structured":
			content = artifact.StructuredJSON
		default:
			return nil, reworkInvalidError()
		}
		if content == nil {
			return nil, reworkInvalidError()
		}
		digest := sha256.Sum256([]byte(*content))
		rework.Artifacts = append(rework.Artifacts, agentexec.ReworkArtifact{ID: artifact.ID, StorageKind: artifact.StorageKind, Name: artifact.Name, Content: *content, SHA256: hex.EncodeToString(digest[:])})
	}
	if err := agentexec.ValidateReworkContext(*rework); err != nil {
		return nil, reworkInvalidError()
	}
	return rework, nil
}

func agentRunReworkRequestFromContext(rework agentexec.ReworkContext, taskVersion int64) *agentRunReworkRequest {
	ids := make([]string, len(rework.Artifacts))
	for index, artifact := range rework.Artifacts {
		ids[index] = artifact.ID
	}
	return &agentRunReworkRequest{SubmissionID: rework.SubmissionID, ArtifactIDs: ids, ExpectedTaskVersion: taskVersion}
}

func parseAgentRunV4Snapshot(raw string) (agentRunV4InputSnapshot, error) {
	var snapshot agentRunV4InputSnapshot
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return snapshot, agentRunIdentityChangedError()
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || string(encoded) != raw || len(encoded) > 262144 || snapshot.Files == nil ||
		snapshot.Task.Status != "in_progress" || snapshot.Task.ReviewPolicy != "manual" || snapshot.Task.Version < 1 {
		return snapshot, agentRunIdentityChangedError()
	}
	if err := agentexec.ValidateReworkContext(snapshot.Rework); err != nil {
		return snapshot, err
	}
	if err := validateAgentRunFileSnapshot(snapshot.fileSnapshot(), true); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func (a *API) previewAgentRunRework(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	var input struct {
		Rework *agentRunReworkRequest `json:"rework"`
	}
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, 400, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	var task models.Task
	var rework *agentexec.ReworkContext
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		var err error
		rework, err = freezeAgentRunRework(tx, task, input.Rework)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"task_id": task.ID, "task_version": task.Version, "rework_context": rework}})
}
