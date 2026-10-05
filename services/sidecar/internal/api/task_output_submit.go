package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/api/filestore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func (a *API) submitTaskOutput(c *gin.Context) {
	taskIDValue, ok := taskID(c)
	if !ok {
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	if a.artifactStore == nil {
		writeError(c, http.StatusServiceUnavailable, "ARTIFACT_STORAGE_UNAVAILABLE", "Artifact storage is unavailable")
		return
	}
	input, artifacts, err := a.prepareSubmitOutputRequest(c)
	if err != nil {
		writeTaskOutputError(c, err)
		return
	}
	defer func() {
		for _, artifact := range artifacts {
			if artifact.StagedFile != nil {
				a.artifactStore.DiscardStagedFile(*artifact.StagedFile)
			}
		}
	}()
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return
	}
	requestHash, err := submitOutputRequestHash(expectedVersion, input.Summary, artifacts)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	endpoint := fmt.Sprintf("POST /api/v1/tasks/%s/submit-output", taskIDValue)
	statusCode := http.StatusCreated
	replayed := false
	committedFiles := make([]committedArtifactFile, 0)
	var response submitOutputResponse
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		replay, replayStatus, replayErr := replayTaskOutputCommand(tx, idempotencyKey, endpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replay {
			replayed = true
			statusCode = replayStatus
			return nil
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		var commandErr error
		response, commandErr = submitTaskOutputInTransaction(tx, taskIDValue, expectedVersion, input, artifacts, func(artifact preparedArtifact) error {
			if err := a.artifactStore.CommitStagedFile(*artifact.StagedFile); err != nil {
				return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be stored")
			}
			committedFiles = append(committedFiles, committedArtifactFile{artifactID: artifact.ID, relativePath: artifact.StagedFile.RelativePath})
			return nil
		}, requestIDFromContext(c), now)
		if commandErr != nil {
			return commandErr
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, endpoint, taskIDValue, requestHash, statusCode, response, now)
	})
	if err != nil {
		a.compensateSubmittedArtifactFiles(taskIDValue, committedFiles)
		writeTaskOutputError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
	}
	normalizeTaskSubmissionOutputOrigin(&response.Submission)
	normalizeTask(&response.Task)
	setProjectETag(c, response.Task.Version)
	c.JSON(statusCode, gin.H{"data": response})
}

func (a *API) compensateSubmittedArtifactFiles(taskIDValue string, committed []committedArtifactFile) {
	for index := len(committed) - 1; index >= 0; index-- {
		file := committed[index]
		var count int64
		result := a.db.Model(&models.TaskArtifact{}).
			Where(
				"id = ? AND task_id = ? AND storage_kind = 'file' AND relative_path = ? AND deleted_at IS NULL",
				file.artifactID, taskIDValue, file.relativePath,
			).
			Count(&count)
		if result.Error != nil {
			// A failed COMMIT can be ambiguous: SQLite may have made the row
			// durable even though the driver returned an error. Preserve the
			// object whenever the database cannot prove it is unreferenced; the
			// startup reconciler will quarantine a real orphan safely.
			if a.options.Logger != nil {
				a.options.Logger.Printf(
					"Artifact compensation deferred task_id=%s artifact_id=%s error=%v",
					taskIDValue, file.artifactID, result.Error,
				)
			}
			continue
		}
		if count != 0 {
			continue
		}
		if cleanupErr := a.artifactStore.DiscardCommittedFile(file.relativePath); cleanupErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf(
				"Artifact compensation failed task_id=%s artifact_id=%s error=%v",
				taskIDValue, file.artifactID, cleanupErr,
			)
		}
	}
}

func (a *API) prepareSubmitOutputRequest(c *gin.Context) (submitOutputRequest, []preparedArtifact, error) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json or multipart/form-data")
	}
	requestLimit := int64(maxJSONBodyBytes)
	requestLimitMessage := "JSON request bodies cannot exceed 1 MiB"
	switch mediaType {
	case "application/json":
	case "multipart/form-data":
		requestLimit = maxArtifactRequestBytes
		requestLimitMessage = "Task output requests cannot exceed 100 MiB"
	default:
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json or multipart/form-data")
	}
	if c.Request.ContentLength > requestLimit {
		return submitOutputRequest{}, nil, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", requestLimitMessage)
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestLimit)
	var input submitOutputRequest
	var multipartReader *multipart.Reader
	switch mediaType {
	case "application/json":
		if err := decodeStrictJSONReader(c.Request.Body, &input); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return submitOutputRequest{}, nil, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", requestLimitMessage)
			}
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		}
	case "multipart/form-data":
		multipartReader, err = c.Request.MultipartReader()
		if err != nil {
			return submitOutputRequest{}, nil, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
		}
		input, err = readSubmitOutputManifest(multipartReader)
		if err != nil {
			return submitOutputRequest{}, nil, err
		}
	}
	input, prepared, err := prepareTaskOutputInput(input, mediaType == "multipart/form-data")
	if err != nil {
		return submitOutputRequest{}, nil, err
	}
	cleanup := true
	defer func() {
		if !cleanup || a.artifactStore == nil {
			return
		}
		for _, artifact := range prepared {
			if artifact.StagedFile != nil {
				a.artifactStore.DiscardStagedFile(*artifact.StagedFile)
			}
		}
	}()
	if multipartReader != nil {
		if err := a.stageSubmitOutputFiles(multipartReader, prepared); err != nil {
			return submitOutputRequest{}, nil, err
		}
	}
	cleanup = false
	return input, prepared, nil
}

func readSubmitOutputManifest(reader *multipart.Reader) (submitOutputRequest, error) {
	part, err := reader.NextPart()
	if err != nil {
		return submitOutputRequest{}, multipartRequestReadError(err)
	}
	if part.FormName() != "manifest" || part.FileName() != "" {
		return submitOutputRequest{}, newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The manifest text field must be the first multipart part")
	}
	manifest, readErr := io.ReadAll(io.LimitReader(part, int64(maxArtifactManifestBytes)+1))
	if readErr != nil {
		return submitOutputRequest{}, multipartRequestReadError(readErr)
	}
	if len(manifest) > maxArtifactManifestBytes {
		return submitOutputRequest{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Multipart manifests cannot exceed 1 MiB")
	}
	if err := part.Close(); err != nil {
		return submitOutputRequest{}, multipartRequestReadError(err)
	}
	var input submitOutputRequest
	if err := decodeStrictJSONBytes(manifest, &input); err != nil {
		return submitOutputRequest{}, newProjectRequestError(http.StatusBadRequest, "INVALID_JSON", "The multipart manifest is not valid JSON")
	}
	return input, nil
}

func (a *API) stageSubmitOutputFiles(reader *multipart.Reader, prepared []preparedArtifact) error {
	expected := make(map[string]int)
	for index := range prepared {
		if prepared[index].FileField != "" {
			expected[prepared[index].FileField] = index
		}
	}
	seen := make(map[string]struct{}, len(expected))
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return multipartRequestReadError(err)
		}
		field := part.FormName()
		if field == "manifest" {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Exactly one manifest field is allowed")
		}
		if field == "" || part.FileName() == "" {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Only referenced file parts are allowed after the manifest")
		}
		index, ok := expected[field]
		if !ok {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every uploaded file must be referenced exactly once by manifest file_field")
		}
		if _, duplicate := seen[field]; duplicate {
			return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every uploaded file must be referenced exactly once by manifest file_field")
		}
		seen[field] = struct{}{}
		staged, stageErr := a.artifactStore.StageMultipartFile(part, prepared[index].ID)
		if stageErr != nil {
			return multipartFileStageError(stageErr)
		}
		prepared[index].StagedFile = &staged
		prepared[index].RelativePath = &staged.RelativePath
		prepared[index].MimeType = &staged.MIMEType
		prepared[index].SizeBytes = &staged.SizeBytes
		prepared[index].SHA256 = &staged.SHA256
		if err := part.Close(); err != nil {
			return multipartRequestReadError(err)
		}
	}
	if len(seen) != len(expected) {
		return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "Every manifest file_field must reference exactly one uploaded file")
	}
	return nil
}

func multipartFileStageError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Task output requests cannot exceed 100 MiB")
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
	}
	if errors.Is(err, filestore.ErrArtifactFileTooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "ARTIFACT_FILE_TOO_LARGE", "Each Artifact file must not exceed 50 MiB")
	}
	if errors.Is(err, filestore.ErrArtifactFileEmpty) {
		return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact files must contain at least one byte")
	}
	return newProjectRequestError(http.StatusInternalServerError, "ARTIFACT_STORAGE_ERROR", "The Artifact file could not be staged")
}

func multipartRequestReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Task output requests cannot exceed 100 MiB")
	}
	return newProjectRequestError(http.StatusBadRequest, "INVALID_MULTIPART", "The multipart request is not valid")
}

func prepareArtifactInput(
	input submitArtifactInput,
	position int,
	usedFileFields map[string]struct{},
	isMultipart bool,
) (preparedArtifact, error) {
	input.StorageKind = strings.TrimSpace(input.StorageKind)
	input.Name = strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 255 || hasUnsafeControlCharacters(input.Name) {
		return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact name must contain 1 to 255 safe characters")
	}
	artifact := preparedArtifact{
		ID: uuid.NewString(), ClientRef: input.ClientRef, Position: position, StorageKind: input.StorageKind,
		Name: input.Name, RequiresFollowup: input.RequiresFollowup,
	}
	switch input.StorageKind {
	case "text":
		if input.ContentText == nil || strings.TrimSpace(*input.ContentText) == "" {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "text Artifacts require non-empty content_text")
		}
		if utf8.RuneCountInString(*input.ContentText) > maxArtifactTextCharacters {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "content_text cannot exceed 500000 characters")
		}
		if input.ReferenceURL != nil || len(input.StructuredJSON) != 0 || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		artifact.ContentText = input.ContentText
	case "link":
		if input.ReferenceURL == nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "link Artifacts require reference_url")
		}
		reference := strings.TrimSpace(*input.ReferenceURL)
		parsed, err := url.Parse(reference)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reference_url must be an http or https URL without credentials")
		}
		if len(reference) > 4096 {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "reference_url cannot exceed 4096 bytes")
		}
		if input.ContentText != nil || len(input.StructuredJSON) != 0 || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		artifact.ReferenceURL = &reference
	case "structured":
		if len(input.StructuredJSON) == 0 {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "structured Artifacts require structured_json")
		}
		if len(input.StructuredJSON) > maxArtifactManifestBytes {
			return preparedArtifact{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "structured_json cannot exceed 1 MiB")
		}
		canonical, err := canonicalStructuredJSONObject(input.StructuredJSON)
		if err != nil {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "structured_json must be a JSON object")
		}
		if input.ContentText != nil || input.ReferenceURL != nil || input.FileField != nil {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		if len(canonical) > maxArtifactManifestBytes {
			return preparedArtifact{}, newProjectRequestError(http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "structured_json cannot exceed 1 MiB")
		}
		value := string(canonical)
		artifact.StructuredJSON = &value
	case "file":
		if !isMultipart {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file Artifacts require multipart/form-data")
		}
		if input.FileField == nil || strings.TrimSpace(*input.FileField) == "" {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file Artifacts require file_field")
		}
		if input.ContentText != nil || input.ReferenceURL != nil || len(input.StructuredJSON) != 0 {
			return preparedArtifact{}, artifactPayloadConflict()
		}
		field := strings.TrimSpace(*input.FileField)
		if _, exists := usedFileFields[field]; exists {
			return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "file_field values must be unique")
		}
		usedFileFields[field] = struct{}{}
		artifact.FileField = field
	default:
		return preparedArtifact{}, newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "storage_kind must be text, file, link, or structured")
	}
	return artifact, nil
}

func hasUnsafeControlCharacters(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func canonicalStructuredJSONObject(raw []byte) (json.RawMessage, error) {
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("structured JSON is not an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("structured JSON contains multiple values")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func artifactPayloadConflict() error {
	return newProjectRequestError(http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Artifact payload fields must match storage_kind exactly")
}

func decodeStrictJSONBytes(data []byte, destination any) error {
	return decodeStrictJSONReader(strings.NewReader(string(data)), destination)
}

func decodeStrictJSONReader(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func submitOutputRequestHash(expectedVersion int64, summary string, artifacts []preparedArtifact) (string, error) {
	type hashArtifact struct {
		ClientRef        string  `json:"client_ref"`
		Position         int     `json:"position"`
		StorageKind      string  `json:"storage_kind"`
		Name             string  `json:"name"`
		ContentText      *string `json:"content_text"`
		ReferenceURL     *string `json:"reference_url"`
		StructuredJSON   *string `json:"structured_json"`
		MimeType         *string `json:"mime_type"`
		SizeBytes        *int64  `json:"size_bytes"`
		SHA256           *string `json:"sha256"`
		RequiresFollowup bool    `json:"requires_followup"`
	}
	values := make([]hashArtifact, len(artifacts))
	for index, artifact := range artifacts {
		values[index] = hashArtifact{
			ClientRef: artifact.ClientRef, Position: artifact.Position, StorageKind: artifact.StorageKind, Name: artifact.Name,
			ContentText: artifact.ContentText, ReferenceURL: artifact.ReferenceURL,
			StructuredJSON: artifact.StructuredJSON, MimeType: artifact.MimeType,
			SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256, RequiresFollowup: artifact.RequiresFollowup,
		}
	}
	payload := struct {
		ExpectedVersion int64          `json:"expected_version"`
		Summary         string         `json:"summary"`
		Artifacts       []hashArtifact `json:"artifacts"`
	}{ExpectedVersion: expectedVersion, Summary: summary, Artifacts: values}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "v1:" + hex.EncodeToString(digest[:]), nil
}

func taskOutputCommandIdempotency(c *gin.Context, payload any) (string, string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if err := validateIdempotencyKey(key); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		return "", "", false
	}
	if key == "" {
		return "", "", true
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeDatabaseError(c)
		return "", "", false
	}
	digest := sha256.Sum256(encoded)
	return key, "v1:" + hex.EncodeToString(digest[:]), true
}

func replayTaskOutputCommand(tx *gorm.DB, key, endpoint, requestHash string, response any) (bool, int, error) {
	if key == "" {
		return false, 0, nil
	}
	var existing models.IdempotencyKey
	err := tx.Where("key = ? AND endpoint = ?", key, endpoint).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if existing.RequestHash == nil || existing.ResponseBody == nil || existing.ResponseStatus == nil {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_REPLAY_UNAVAILABLE", "This legacy Idempotency-Key cannot be replayed safely; use a new key")
	}
	if *existing.RequestHash != requestHash {
		return false, 0, newProjectRequestError(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used with a different request")
	}
	if err := json.Unmarshal([]byte(*existing.ResponseBody), response); err != nil {
		return false, 0, err
	}
	return true, *existing.ResponseStatus, nil
}

func recordTaskOutputIdempotency(tx *gorm.DB, key, endpoint, resourceID, requestHash string, status int, response any, now string) error {
	if key == "" {
		return nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	text := string(encoded)
	record := models.IdempotencyKey{
		Key: key, Endpoint: endpoint, ResourceID: resourceID, RequestHash: &requestHash,
		ResponseBody: &text, ResponseStatus: &status, CreatedAt: now,
	}
	return tx.Create(&record).Error
}
