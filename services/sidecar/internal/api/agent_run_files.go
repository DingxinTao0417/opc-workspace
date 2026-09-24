package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	agentRunExecutionContractVersionV2 int64 = 2
	// v3 extends the H5-C frozen execution contract with a bounded ordered
	// multi-file output. It intentionally retains the v2 snapshot wire shape;
	// the version prevents an older sidecar from interpreting the new output
	// type as a legacy single result.
	agentRunExecutionContractVersionV3 int64 = 3

	agentRunFileInvalidCode               = "AGENT_RUN_FILE_INVALID"
	agentRunFileUnavailableCode           = "AGENT_RUN_FILE_UNAVAILABLE"
	agentRunFileIntegrityInvalidCode      = "AGENT_RUN_FILE_INTEGRITY_INVALID"
	agentRunFileStorageUnavailableCode    = "AGENT_RUN_FILE_STORAGE_UNAVAILABLE"
	agentRunFileReferencedByActiveRunCode = "AGENT_RUN_FILE_REFERENCED"
)

type agentRunInputFileRequest struct {
	SourceKind string                           `json:"source_kind"`
	ID         string                           `json:"id"`
	SourceTask *agentexec.ProjectTaskFileSource `json:"source_task,omitempty"`
	SHA256     string                           `json:"sha256,omitempty"`
}

// Source selection is a consent boundary. Reject duplicate/case-folded keys
// and null proof fields before the human-confirmation checks can inspect it.
func (request *agentRunInputFileRequest) UnmarshalJSON(raw []byte) error {
	fields, err := strictAgentRestartObject(raw, "source_kind", "id", "source_task", "sha256")
	if err != nil || fields["source_kind"] == nil || fields["id"] == nil {
		return errors.New("input file selection requires exact non-null source fields")
	}
	type wire agentRunInputFileRequest
	var decoded wire
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.SourceKind == agentexec.FileSourceProjectTaskArtifact {
		if len(fields) != 4 || decoded.ID != strings.TrimSpace(decoded.ID) {
			return errors.New("project task file selection requires its exact source proof and hash")
		}
	} else if len(fields) != 2 || strings.TrimSpace(decoded.SourceKind) == agentexec.FileSourceProjectTaskArtifact {
		return errors.New("only exact project_task_artifact selections may contain source proof or hash")
	}
	*request = agentRunInputFileRequest(decoded)
	return nil
}

type agentRunOutputContractRequest struct {
	Type  string                         `json:"type"`
	Name  string                         `json:"name,omitempty"`
	MIME  string                         `json:"mime,omitempty"`
	Files []agentexec.OutputFileContract `json:"files,omitempty"`
}

// frozenAgentRunFileReference is the durable v2 authorization record. It
// deliberately contains neither controlled-store paths nor file content.
type frozenAgentRunFileReference struct {
	ID         string                           `json:"id"`
	SourceKind string                           `json:"source_kind"`
	Name       string                           `json:"name"`
	MIME       string                           `json:"mime"`
	SizeBytes  int                              `json:"size_bytes"`
	SHA256     string                           `json:"sha256"`
	SourceTask *agentexec.ProjectTaskFileSource `json:"source_task,omitempty"`
}

type agentRunV2InputSnapshot struct {
	Task           agentexec.TaskSnapshot        `json:"task"`
	ProviderKind   string                        `json:"provider_kind"`
	Files          []frozenAgentRunFileReference `json:"files"`
	OutputContract agentexec.OutputContract      `json:"output_contract"`
}

type agentRunFileCandidate struct {
	SourceKind string                           `json:"source_kind"`
	ID         string                           `json:"id"`
	Name       string                           `json:"name"`
	MIME       string                           `json:"mime"`
	SizeBytes  int64                            `json:"size_bytes"`
	SHA256     string                           `json:"sha256"`
	Eligible   bool                             `json:"eligible"`
	ErrorCode  string                           `json:"error_code,omitempty"`
	CreatedAt  string                           `json:"created_at"`
	SourceTask *agentexec.ProjectTaskFileSource `json:"source_task,omitempty"`
}

func sameFrozenAgentRunFileReference(left, right frozenAgentRunFileReference) bool {
	leftSource, rightSource := left.SourceTask, right.SourceTask
	left.SourceTask, right.SourceTask = nil, nil
	return left == right && ((leftSource == nil && rightSource == nil) ||
		(leftSource != nil && rightSource != nil && *leftSource == *rightSource))
}

func agentRunInputRequestFromReference(file frozenAgentRunFileReference) agentRunInputFileRequest {
	request := agentRunInputFileRequest{SourceKind: file.SourceKind, ID: file.ID}
	if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
		request.SourceTask, request.SHA256 = file.SourceTask, file.SHA256
	}
	return request
}

type agentRunControlledFileRecord struct {
	Reference    frozenAgentRunFileReference
	RelativePath string
}

func normalizeAgentRunOutputContract(input *agentRunOutputContractRequest) (agentexec.OutputContract, error) {
	contract := agentexec.OutputContract{Type: agentexec.ResultTypeText}
	if input != nil {
		contract = agentexec.OutputContract{
			Type:  strings.TrimSpace(input.Type),
			Name:  strings.TrimSpace(input.Name),
			MIME:  strings.TrimSpace(strings.ToLower(input.MIME)),
			Files: input.Files,
		}
		for index := range contract.Files {
			contract.Files[index].Name = strings.TrimSpace(contract.Files[index].Name)
			contract.Files[index].MIME = strings.TrimSpace(strings.ToLower(contract.Files[index].MIME))
		}
	}
	if err := agentexec.ValidateOutputContract(contract); err != nil {
		return agentexec.OutputContract{}, newProjectRequestError(
			http.StatusUnprocessableEntity, agentRunFileInvalidCode,
			"output_contract must describe an allowed UTF-8 text, file, or ordered multi-file result",
		)
	}
	return contract, nil
}

func canonicalAgentRunMIME(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return value
	}
	for key, parameter := range parameters {
		if key != "charset" || !strings.EqualFold(strings.TrimSpace(parameter), "utf-8") {
			return value
		}
	}
	return strings.ToLower(mediaType)
}

func canonicalAgentRunStoredTextMIME(name, storedMIME string) string {
	// Controlled stores use content sniffing and commonly persist
	// `text/plain; charset=utf-8`. The execution contract is stricter: freeze a
	// canonical MIME from the safe extension, then verify the actual bytes as
	// UTF-8 below. The stored MIME must still be textual or generic.
	mediaType := canonicalAgentRunMIME(storedMIME)
	if mediaType != "application/octet-stream" && mediaType != "" &&
		!strings.HasPrefix(mediaType, "text/") && mediaType != "application/json" &&
		mediaType != "application/xml" && mediaType != "application/yaml" &&
		mediaType != "application/javascript" {
		return mediaType
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".log":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "text/javascript"
	case ".ts", ".tsx":
		return "text/typescript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	case ".sh":
		return "text/x-shellscript"
	case ".sql":
		return "text/x-sql"
	default:
		return mediaType
	}
}

func validateAgentRunInputFileRequests(input []agentRunInputFileRequest) error {
	if len(input) > agentexec.MaxFileInputs {
		return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
			fmt.Sprintf("input_files cannot contain more than %d entries", agentexec.MaxFileInputs))
	}
	seen := make(map[string]struct{}, len(input))
	for _, file := range input {
		file.SourceKind = strings.TrimSpace(file.SourceKind)
		file.ID = strings.TrimSpace(file.ID)
		parsed, err := uuid.Parse(file.ID)
		if err != nil || parsed.String() != file.ID {
			return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
				"Every input_files id must be a canonical UUID")
		}
		if file.SourceKind != agentexec.FileSourceTaskArtifact && file.SourceKind != agentexec.FileSourceProjectTaskArtifact &&
			file.SourceKind != agentexec.FileSourceProjectAttachment {
			return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
				"input_files source_kind must be task_artifact, project_attachment or project_task_artifact")
		}
		if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
			if file.SourceTask == nil || agentexec.ValidateProjectTaskFileSource(*file.SourceTask) != nil ||
				len(file.SHA256) != 64 || strings.ToLower(file.SHA256) != file.SHA256 {
				return agentRunIdentityChangedError()
			}
			if _, err := hex.DecodeString(file.SHA256); err != nil {
				return agentRunIdentityChangedError()
			}
		} else if file.SourceTask != nil || file.SHA256 != "" {
			return newProjectRequestError(422, agentRunFileInvalidCode, "Source proof is only valid for accepted project-task files")
		}
		if _, duplicate := seen[file.ID]; duplicate {
			return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
				"input_files cannot repeat a controlled file id")
		}
		seen[file.ID] = struct{}{}
	}
	return nil
}

func freezeAgentRunFiles(
	tx *gorm.DB,
	task models.Task,
	requests []agentRunInputFileRequest,
	store *artifactStore,
) ([]frozenAgentRunFileReference, []agentexec.FrozenFileInput, error) {
	if err := validateAgentRunInputFileRequests(requests); err != nil {
		return nil, nil, err
	}
	references := make([]frozenAgentRunFileReference, 0, len(requests))
	files := make([]agentexec.FrozenFileInput, 0, len(requests))
	for _, request := range requests {
		request.SourceKind = strings.TrimSpace(request.SourceKind)
		request.ID = strings.TrimSpace(request.ID)
		record, err := loadAgentRunControlledFileRecord(tx, task, request.SourceKind, request.ID)
		if err != nil {
			return nil, nil, err
		}
		if request.SourceKind == agentexec.FileSourceProjectTaskArtifact && (record.Reference.SourceTask == nil ||
			request.SourceTask == nil || *record.Reference.SourceTask != *request.SourceTask || record.Reference.SHA256 != request.SHA256) {
			return nil, nil, agentRunIdentityChangedError()
		}
		file, err := readAgentRunControlledFile(store, record)
		if err != nil {
			return nil, nil, err
		}
		references = append(references, record.Reference)
		files = append(files, file)
	}
	if err := validateAgentRunFrozenFiles(files, task); err != nil {
		return nil, nil, err
	}
	return references, files, nil
}

func loadAgentRunControlledFileRecord(
	tx *gorm.DB,
	task models.Task,
	sourceKind string,
	id string,
) (agentRunControlledFileRecord, error) {
	switch sourceKind {
	case agentexec.FileSourceProjectTaskArtifact:
		return loadAgentRunProjectTaskFileRecord(tx, task, id)
	case agentexec.FileSourceTaskArtifact:
		var artifact models.TaskArtifact
		if err := tx.First(&artifact, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agentRunControlledFileRecord{}, newProjectRequestError(
					http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
					"The selected Task Artifact is unavailable",
				)
			}
			return agentRunControlledFileRecord{}, err
		}
		if artifact.TaskID != task.ID || artifact.DeletedAt != nil || artifact.StorageKind != "file" ||
			artifact.RelativePath == nil || artifact.MimeType == nil || artifact.SizeBytes == nil ||
			artifact.SHA256 == nil || artifact.IntegrityStatus != "verified" {
			return agentRunControlledFileRecord{}, newProjectRequestError(
				http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
				"The selected Task Artifact is not an active verified file owned by this Task",
			)
		}
		if *artifact.SizeBytes <= 0 || *artifact.SizeBytes > int64(agentexec.MaxFileInputBytes) {
			return agentRunControlledFileRecord{}, newProjectRequestError(
				http.StatusUnprocessableEntity, agentRunFileInvalidCode,
				"The selected Task Artifact exceeds the controlled input size limit",
			)
		}
		return agentRunControlledFileRecord{
			Reference: frozenAgentRunFileReference{
				ID: artifact.ID, SourceKind: sourceKind, Name: artifact.Name,
				MIME: canonicalAgentRunStoredTextMIME(artifact.Name, *artifact.MimeType), SizeBytes: int(*artifact.SizeBytes),
				SHA256: *artifact.SHA256,
			},
			RelativePath: *artifact.RelativePath,
		}, nil

	case agentexec.FileSourceProjectAttachment:
		if task.ProjectID == nil {
			return agentRunControlledFileRecord{}, newProjectRequestError(
				http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
				"The Task does not have a current Project attachment scope",
			)
		}
		var attachment models.ProjectAttachment
		if err := tx.First(&attachment, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return agentRunControlledFileRecord{}, newProjectRequestError(
					http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
					"The selected Project Attachment is unavailable",
				)
			}
			return agentRunControlledFileRecord{}, err
		}
		if attachment.ProjectID != *task.ProjectID || attachment.DeletedAt != nil ||
			attachment.IntegrityStatus != "verified" || attachment.SizeBytes <= 0 ||
			attachment.SizeBytes > int64(agentexec.MaxFileInputBytes) {
			return agentRunControlledFileRecord{}, newProjectRequestError(
				http.StatusUnprocessableEntity, agentRunFileUnavailableCode,
				"The selected Project Attachment is not an active verified file owned by the Task's current Project",
			)
		}
		return agentRunControlledFileRecord{
			Reference: frozenAgentRunFileReference{
				ID: attachment.ID, SourceKind: sourceKind, Name: attachment.Name,
				MIME: canonicalAgentRunStoredTextMIME(attachment.Name, attachment.MimeType), SizeBytes: int(attachment.SizeBytes),
				SHA256: attachment.SHA256,
			},
			RelativePath: attachment.RelativePath,
		}, nil
	default:
		return agentRunControlledFileRecord{}, newProjectRequestError(
			http.StatusUnprocessableEntity, agentRunFileInvalidCode,
			"The controlled file source kind is invalid",
		)
	}
}

func readAgentRunControlledFile(
	store *artifactStore,
	record agentRunControlledFileRecord,
) (agentexec.FrozenFileInput, error) {
	if store == nil {
		return agentexec.FrozenFileInput{}, newProjectRequestError(
			http.StatusServiceUnavailable, agentRunFileStorageUnavailableCode,
			"Controlled Artifact storage is unavailable",
		)
	}
	file, info, err := store.openObject(record.RelativePath)
	if err != nil {
		return agentexec.FrozenFileInput{}, newProjectRequestError(
			http.StatusConflict, agentRunFileIntegrityInvalidCode,
			"The selected controlled file could not be verified",
		)
	}
	defer file.Close()
	if info.Size() != int64(record.Reference.SizeBytes) {
		return agentexec.FrozenFileInput{}, newProjectRequestError(
			http.StatusConflict, agentRunFileIntegrityInvalidCode,
			"The selected controlled file size no longer matches its metadata",
		)
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(agentexec.MaxFileInputBytes)+1))
	if err != nil || len(content) != record.Reference.SizeBytes || len(content) > agentexec.MaxFileInputBytes {
		return agentexec.FrozenFileInput{}, newProjectRequestError(
			http.StatusConflict, agentRunFileIntegrityInvalidCode,
			"The selected controlled file could not be read within its frozen limit",
		)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != record.Reference.SHA256 {
		return agentexec.FrozenFileInput{}, newProjectRequestError(
			http.StatusConflict, agentRunFileIntegrityInvalidCode,
			"The selected controlled file hash no longer matches its metadata",
		)
	}
	return agentexec.FrozenFileInput{
		ID: record.Reference.ID, SourceKind: record.Reference.SourceKind,
		Name: record.Reference.Name, MIME: record.Reference.MIME,
		SizeBytes: record.Reference.SizeBytes, SHA256: record.Reference.SHA256,
		Content:    string(content),
		SourceTask: record.Reference.SourceTask,
	}, nil
}

func validateAgentRunFrozenFiles(files []agentexec.FrozenFileInput, tasks ...models.Task) error {
	frame := agentexec.InputFrame{
		ProtocolVersion: agentexec.ProtocolVersion,
		RunID:           "validation", Nonce: "validation", Capabilities: []string{
			agentexec.CapabilityReadTaskSnapshot,
			agentexec.CapabilityReadControlledFiles,
			agentexec.CapabilityWriteTextResult,
		},
		Input: agentexec.TaskSnapshot{TaskID: "validation"}, Instruction: "validate",
		Files: files,
	}
	if len(tasks) == 1 {
		frame.Input = taskSnapshotFromModel(tasks[0])
	}
	for _, file := range files {
		if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
			frame.Capabilities = append(frame.Capabilities, agentexec.CapabilityReadProjectTaskFiles)
			break
		}
	}
	if err := agentexec.ValidateInputFrame(frame); err != nil {
		return newProjectRequestError(http.StatusUnprocessableEntity, agentRunFileInvalidCode,
			"Every selected input must be a safe, non-empty UTF-8 text file with matching metadata")
	}
	return nil
}

func resolveFrozenAgentRunFiles(
	tx *gorm.DB,
	task models.Task,
	references []frozenAgentRunFileReference,
	store *artifactStore,
) ([]agentexec.FrozenFileInput, error) {
	requests := make([]agentRunInputFileRequest, len(references))
	for index, reference := range references {
		requests[index] = agentRunInputRequestFromReference(reference)
	}
	currentReferences, files, err := freezeAgentRunFiles(tx, task, requests, store)
	if err != nil || len(currentReferences) != len(references) {
		return nil, agentRunIdentityChangedError()
	}
	for index := range references {
		if !sameFrozenAgentRunFileReference(currentReferences[index], references[index]) {
			return nil, agentRunIdentityChangedError()
		}
	}
	return files, nil
}

func parseAgentRunV2Snapshot(raw string) (agentRunV2InputSnapshot, error) {
	var snapshot agentRunV2InputSnapshot
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return agentRunV2InputSnapshot{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentRunV2InputSnapshot{}, errors.New("Agent Run v2 snapshot has trailing JSON")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || string(encoded) != raw {
		return agentRunV2InputSnapshot{}, errors.New("Agent Run v2 snapshot is not canonical")
	}
	if err := validateAgentRunV2Snapshot(snapshot); err != nil {
		return agentRunV2InputSnapshot{}, err
	}
	return snapshot, nil
}

func validateAgentRunV2Snapshot(snapshot agentRunV2InputSnapshot) error {
	return validateAgentRunFileSnapshot(snapshot, false)
}

func validateAgentRunFileSnapshot(snapshot agentRunV2InputSnapshot, allowTextOnly bool) error {
	return validateAgentRunFileSnapshotWithProjectTasks(snapshot, allowTextOnly, false)
}

func validateAgentRunFileSnapshotWithProjectTasks(snapshot agentRunV2InputSnapshot, allowTextOnly, allowProjectTasks bool) error {
	if snapshot.ProviderKind != "local" && snapshot.ProviderKind != "remote" {
		return errors.New("Agent Run v2 snapshot Provider kind is invalid")
	}
	if len(snapshot.Files) > agentexec.MaxFileInputs {
		return fmt.Errorf("Agent Run v2 snapshot has more than %d controlled files", agentexec.MaxFileInputs)
	}
	if err := agentexec.ValidateOutputContract(snapshot.OutputContract); err != nil {
		return fmt.Errorf("Agent Run v2 output contract is invalid: %w", err)
	}
	if !allowTextOnly && len(snapshot.Files) == 0 && snapshot.OutputContract.Type != agentexec.ResultTypeFile &&
		snapshot.OutputContract.Type != agentexec.ResultTypeFiles {
		return errors.New("Agent Run v2 snapshot must contain controlled files or a file output contract")
	}

	seenIDs := make(map[string]struct{}, len(snapshot.Files))
	totalBytes := 0
	for index, file := range snapshot.Files {
		parsedID, err := uuid.Parse(file.ID)
		if err != nil || parsedID.String() != file.ID {
			return fmt.Errorf("Agent Run v2 controlled file %d id is not a canonical UUID", index+1)
		}
		if file.SourceKind == agentexec.FileSourceProjectTaskArtifact {
			if !allowProjectTasks || file.SourceTask == nil || agentexec.ValidateProjectTaskFileSource(*file.SourceTask) != nil ||
				snapshot.Task.ProjectID == nil || file.SourceTask.ProjectID != *snapshot.Task.ProjectID || file.SourceTask.TaskID == snapshot.Task.TaskID {
				return agentRunIdentityChangedError()
			}
		} else if file.SourceTask != nil || (file.SourceKind != agentexec.FileSourceTaskArtifact &&
			file.SourceKind != agentexec.FileSourceProjectAttachment) {
			return fmt.Errorf("Agent Run v2 controlled file %d source kind is invalid", index+1)
		}
		if _, duplicate := seenIDs[file.ID]; duplicate {
			return fmt.Errorf("Agent Run v2 controlled file %d repeats an id", index+1)
		}
		seenIDs[file.ID] = struct{}{}
		if err := agentexec.ValidateTextFileNameAndMIME(file.Name, file.MIME); err != nil {
			return fmt.Errorf("Agent Run v2 controlled file %d metadata is invalid: %w", index+1, err)
		}
		if file.SizeBytes <= 0 || file.SizeBytes > agentexec.MaxFileInputBytes {
			return fmt.Errorf("Agent Run v2 controlled file %d size is invalid", index+1)
		}
		if len(file.SHA256) != sha256.Size*2 || strings.ToLower(file.SHA256) != file.SHA256 {
			return fmt.Errorf("Agent Run v2 controlled file %d sha256 is invalid", index+1)
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return fmt.Errorf("Agent Run v2 controlled file %d sha256 is invalid", index+1)
		}
		totalBytes += file.SizeBytes
		if totalBytes > agentexec.MaxFileInputsTotalBytes {
			return fmt.Errorf("Agent Run v2 controlled files exceed the %d-byte total limit", agentexec.MaxFileInputsTotalBytes)
		}
	}
	return nil
}

func agentRunOutputContract(run models.AgentRun) (agentexec.OutputContract, error) {
	if isAgentRunModelContract(run.ExecutionContractVersion) {
		snapshot, err := parseAgentRunModelSnapshot(run.ExecutionContractVersion, run.InputSnapshotJSON)
		if err != nil {
			return agentexec.OutputContract{}, agentRunIdentityChangedError()
		}
		return snapshot.OutputContract, nil
	}
	if run.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
		snapshot, err := parseAgentRunV4Snapshot(run.InputSnapshotJSON)
		if err != nil {
			return agentexec.OutputContract{}, agentRunIdentityChangedError()
		}
		return snapshot.OutputContract, nil
	}
	if run.ExecutionContractVersion == agentRunExecutionContractVersion {
		return agentexec.OutputContract{Type: agentexec.ResultTypeText}, nil
	}
	if run.ExecutionContractVersion != agentRunExecutionContractVersionV2 &&
		run.ExecutionContractVersion != agentRunExecutionContractVersionV3 {
		return agentexec.OutputContract{}, agentRunIdentityChangedError()
	}
	snapshot, err := parseAgentRunV2Snapshot(run.InputSnapshotJSON)
	if err != nil || agentexec.ValidateOutputContract(snapshot.OutputContract) != nil {
		return agentexec.OutputContract{}, agentRunIdentityChangedError()
	}
	return snapshot.OutputContract, nil
}

func agentRunInputRequestsFromSnapshot(snapshot agentRunV2InputSnapshot) []agentRunInputFileRequest {
	requests := make([]agentRunInputFileRequest, len(snapshot.Files))
	for index, file := range snapshot.Files {
		requests[index] = agentRunInputRequestFromReference(file)
	}
	return requests
}

func agentRunOutputRequestFromSnapshot(snapshot agentRunV2InputSnapshot) *agentRunOutputContractRequest {
	return &agentRunOutputContractRequest{
		Type:  snapshot.OutputContract.Type,
		Name:  snapshot.OutputContract.Name,
		MIME:  snapshot.OutputContract.MIME,
		Files: snapshot.OutputContract.Files,
	}
}

func (a *API) listAgentRunFiles(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}
	candidates := make([]agentRunFileCandidate, 0)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.First(&task, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")
			}
			return err
		}
		var err error
		candidates, err = a.loadAgentRunFileCandidates(tx, task)
		return err
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeDatabaseError(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"data": candidates,
		"meta": gin.H{"limits": gin.H{
			"max_files":        agentexec.MaxFileInputs,
			"max_file_bytes":   agentexec.MaxFileInputBytes,
			"max_total_bytes":  agentexec.MaxFileInputsTotalBytes,
			"max_result_bytes": agentexec.MaxResultBytes,
		}},
	})
}

func (a *API) loadAgentRunFileCandidates(
	tx *gorm.DB,
	task models.Task,
) ([]agentRunFileCandidate, error) {
	candidates := make([]agentRunFileCandidate, 0)
	var artifacts []models.TaskArtifact
	if err := tx.Where("task_id = ? AND storage_kind = 'file' AND deleted_at IS NULL", task.ID).
		Order("created_at DESC").Order("id DESC").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		candidate := agentRunFileCandidate{
			SourceKind: agentexec.FileSourceTaskArtifact, ID: artifact.ID, Name: artifact.Name,
			CreatedAt: artifact.CreatedAt,
		}
		if artifact.MimeType != nil {
			candidate.MIME = canonicalAgentRunStoredTextMIME(artifact.Name, *artifact.MimeType)
		}
		if artifact.SizeBytes != nil {
			candidate.SizeBytes = *artifact.SizeBytes
		}
		if artifact.SHA256 != nil {
			candidate.SHA256 = *artifact.SHA256
		}
		candidate.Eligible, candidate.ErrorCode = a.agentRunCandidateEligibility(
			tx, task, candidate.SourceKind, artifact.ID,
		)
		candidates = append(candidates, candidate)
	}

	if task.ProjectID != nil {
		var attachments []models.ProjectAttachment
		if err := tx.Where("project_id = ? AND deleted_at IS NULL", *task.ProjectID).
			Order("created_at DESC").Order("id DESC").Find(&attachments).Error; err != nil {
			return nil, err
		}
		for _, attachment := range attachments {
			candidate := agentRunFileCandidate{
				SourceKind: agentexec.FileSourceProjectAttachment, ID: attachment.ID,
				Name: attachment.Name, MIME: canonicalAgentRunStoredTextMIME(attachment.Name, attachment.MimeType),
				SizeBytes: attachment.SizeBytes, SHA256: attachment.SHA256,
				CreatedAt: attachment.CreatedAt,
			}
			candidate.Eligible, candidate.ErrorCode = a.agentRunCandidateEligibility(
				tx, task, candidate.SourceKind, attachment.ID,
			)
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].CreatedAt == candidates[right].CreatedAt {
			if candidates[left].ID == candidates[right].ID {
				return candidates[left].SourceKind < candidates[right].SourceKind
			}
			return candidates[left].ID > candidates[right].ID
		}
		return candidates[left].CreatedAt > candidates[right].CreatedAt
	})
	return candidates, nil
}

func (a *API) agentRunCandidateEligibility(
	tx *gorm.DB,
	task models.Task,
	sourceKind string,
	id string,
) (bool, string) {
	request := agentRunInputFileRequest{SourceKind: sourceKind, ID: id}
	if sourceKind == agentexec.FileSourceProjectTaskArtifact {
		record, err := loadAgentRunProjectTaskFileRecord(tx, task, id)
		if err != nil {
			return false, agentRunFileUnavailableCode
		}
		request = agentRunInputRequestFromReference(record.Reference)
	}
	_, _, err := freezeAgentRunFiles(tx, task, []agentRunInputFileRequest{request}, a.artifactStore)
	if err == nil {
		return true, ""
	}
	var requestErr *projectRequestError
	if errors.As(err, &requestErr) {
		return false, requestErr.code
	}
	return false, agentRunFileUnavailableCode
}

func activeAgentRunReferencesControlledFile(tx *gorm.DB, sourceKind, id string) (bool, error) {
	type activeAgentRunSnapshot struct {
		ExecutionContractVersion int64
		InputSnapshotJSON        string
	}
	var snapshots []activeAgentRunSnapshot
	if err := tx.Model(&models.AgentRun{}).
		Select("execution_contract_version", "input_snapshot_json").
		Where("status IN ?", []string{"queued", "running"}).
		Find(&snapshots).Error; err != nil {
		return false, err
	}
	for _, active := range snapshots {
		if isAgentRunModelContract(active.ExecutionContractVersion) {
			snapshot, err := parseAgentRunModelSnapshot(active.ExecutionContractVersion, active.InputSnapshotJSON)
			if err != nil {
				return false, err
			}
			for _, file := range snapshot.Files {
				if (file.SourceKind == sourceKind || sourceKind == agentexec.FileSourceTaskArtifact && file.SourceKind == agentexec.FileSourceProjectTaskArtifact) && file.ID == id {
					return true, nil
				}
			}
			if sourceKind == agentexec.FileSourceTaskArtifact && snapshot.Rework != nil {
				for _, artifact := range snapshot.Rework.Artifacts {
					if artifact.ID == id {
						return true, nil
					}
				}
			}
			continue
		}
		if active.ExecutionContractVersion == agentRunExecutionContractVersionV4 {
			snapshot, err := parseAgentRunV4Snapshot(active.InputSnapshotJSON)
			if err != nil {
				return false, err
			}
			for _, file := range snapshot.Files {
				if file.SourceKind == sourceKind && file.ID == id {
					return true, nil
				}
			}
			if sourceKind == agentexec.FileSourceTaskArtifact {
				for _, artifact := range snapshot.Rework.Artifacts {
					if artifact.ID == id {
						return true, nil
					}
				}
			}
			continue
		}
		if active.ExecutionContractVersion == agentRunExecutionContractVersion {
			// v1 is the legacy text-only contract and cannot carry controlled
			// file references, regardless of the task snapshot bytes.
			continue
		}
		if active.ExecutionContractVersion != agentRunExecutionContractVersionV2 &&
			active.ExecutionContractVersion != agentRunExecutionContractVersionV3 {
			return false, fmt.Errorf(
				"active Agent Run has unknown execution contract version %d",
				active.ExecutionContractVersion,
			)
		}
		snapshot, err := parseAgentRunV2Snapshot(active.InputSnapshotJSON)
		if err != nil {
			// An active v2 Run is itself a deletion authorization boundary. A
			// malformed durable snapshot cannot prove that a candidate is
			// unreferenced, so fail closed instead of allowing data loss.
			return false, fmt.Errorf("parse active Agent Run v2 input snapshot: %w", err)
		}
		for _, file := range snapshot.Files {
			if file.SourceKind == sourceKind && file.ID == id {
				return true, nil
			}
		}
	}
	return false, nil
}

func activeAgentRunReferencesProjectAttachment(tx *gorm.DB, projectID string) (bool, error) {
	var ids []string
	if err := tx.Model(&models.ProjectAttachment{}).
		Where("project_id = ?", projectID).
		Pluck("id", &ids).Error; err != nil {
		return false, err
	}
	for _, id := range ids {
		referenced, err := activeAgentRunReferencesControlledFile(tx, agentexec.FileSourceProjectAttachment, id)
		if err != nil || referenced {
			return referenced, err
		}
	}
	return false, nil
}

// Cross-Task inputs remain protected even when the source Task has no active
// Run of its own. Do not rely on the source's current project membership: the
// frozen proof is the deletion boundary until the successor becomes terminal.
func activeAgentRunReferencesProjectTaskSource(tx *gorm.DB, taskID, projectID string) (bool, error) {
	var active []models.AgentRun
	if err := tx.Select("execution_contract_version", "input_snapshot_json").Where("status IN ?", []string{"queued", "running"}).Find(&active).Error; err != nil {
		return false, err
	}
	for _, run := range active {
		if run.ExecutionContractVersion >= agentRunExecutionContractVersion && run.ExecutionContractVersion < agentRunExecutionContractVersionV6 {
			continue
		}
		snapshot, err := parseAgentRunModelSnapshot(run.ExecutionContractVersion, run.InputSnapshotJSON)
		if err != nil {
			return false, err
		}
		for _, file := range snapshot.Files {
			if file.SourceTask != nil && (taskID != "" && file.SourceTask.TaskID == taskID || projectID != "" && file.SourceTask.ProjectID == projectID) {
				return true, nil
			}
		}
	}
	return false, nil
}
