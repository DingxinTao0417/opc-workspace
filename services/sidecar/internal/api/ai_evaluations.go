package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createAIEvaluationEndpoint = "POST /api/v1/ai/evaluations"

type createAIEvaluationRequest struct {
	ProviderID      string `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	SuiteKey        string `json:"suite_key"`
}

type aiEvaluationResultResponse struct {
	ID             string   `json:"id"`
	RunID          string   `json:"run_id"`
	Sequence       int      `json:"sequence"`
	CaseID         string   `json:"case_id"`
	Language       string   `json:"language"`
	Category       string   `json:"category"`
	Status         string   `json:"status"`
	FailureCodes   []string `json:"failure_codes"`
	CitationStatus *string  `json:"citation_status"`
	CitationCount  int      `json:"citation_count"`
	DurationMS     int64    `json:"duration_ms"`
	InputBytes     int      `json:"input_bytes"`
	OutputBytes    int      `json:"output_bytes"`
	InputTokens    *int     `json:"input_tokens"`
	OutputTokens   *int     `json:"output_tokens"`
	TokenSource    *string  `json:"token_source"`
	ErrorCode      *string  `json:"error_code"`
	CreatedAt      string   `json:"created_at"`
}

type aiEvaluationRunResponse struct {
	ProviderConfigVersion    *int64                       `json:"provider_config_version"`
	ID                       string                       `json:"id"`
	ProviderID               string                       `json:"provider_id"`
	ProviderNameSnapshot     string                       `json:"provider_name_snapshot"`
	ProviderModelSnapshot    string                       `json:"provider_model_snapshot"`
	ProviderProtocolSnapshot string                       `json:"provider_protocol_snapshot"`
	ProviderVersion          int64                        `json:"provider_version"`
	DatasetVersion           int                          `json:"dataset_version"`
	SuiteKey                 string                       `json:"suite_key"`
	Status                   string                       `json:"status"`
	TotalCases               int                          `json:"total_cases"`
	CompletedCases           int                          `json:"completed_cases"`
	PassedCases              int                          `json:"passed_cases"`
	FailedCases              int                          `json:"failed_cases"`
	ErrorCases               int                          `json:"error_cases"`
	CurrentCaseID            *string                      `json:"current_case_id"`
	CancelRequested          bool                         `json:"cancel_requested"`
	ErrorCode                *string                      `json:"error_code"`
	StartedAt                *string                      `json:"started_at"`
	CompletedAt              *string                      `json:"completed_at"`
	CreatedAt                string                       `json:"created_at"`
	UpdatedAt                string                       `json:"updated_at"`
	Results                  []aiEvaluationResultResponse `json:"results"`
}

func aiEvaluationRunResponseFromModel(row models.AIEvaluationRun, results []models.AIEvaluationResult) (aiEvaluationRunResponse, error) {
	if !aieval.SuiteKeyAllowed(row.SuiteKey) {
		return aiEvaluationRunResponse{}, errors.New("invalid AI evaluation suite")
	}
	response := aiEvaluationRunResponse{
		ProviderConfigVersion: row.ProviderConfigVersion,
		ID:                    row.ID, ProviderID: row.ProviderID, ProviderNameSnapshot: row.ProviderNameSnapshot,
		ProviderModelSnapshot: row.ProviderModelSnapshot, ProviderProtocolSnapshot: row.ProviderProtocolSnapshot,
		ProviderVersion: row.ProviderVersion, DatasetVersion: row.DatasetVersion, SuiteKey: row.SuiteKey, Status: row.Status,
		TotalCases: row.TotalCases, CompletedCases: row.CompletedCases, PassedCases: row.PassedCases,
		FailedCases: row.FailedCases, ErrorCases: row.ErrorCases, CurrentCaseID: row.CurrentCaseID,
		CancelRequested: row.CancelRequested, ErrorCode: row.ErrorCode, StartedAt: row.StartedAt,
		CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Results: make([]aiEvaluationResultResponse, 0),
	}
	if results == nil {
		return response, nil
	}
	response.Results = make([]aiEvaluationResultResponse, 0, len(results))
	for _, result := range results {
		var failureCodes []string
		if err := json.Unmarshal([]byte(result.FailureCodes), &failureCodes); err != nil || failureCodes == nil {
			return aiEvaluationRunResponse{}, errors.New("invalid AI evaluation failure codes")
		}
		for _, code := range failureCodes {
			if !aiEvaluationFailureCodeAllowed(code) {
				return aiEvaluationRunResponse{}, errors.New("invalid AI evaluation failure code")
			}
		}
		response.Results = append(response.Results, aiEvaluationResultResponse{
			ID: result.ID, RunID: result.RunID, Sequence: result.Sequence, CaseID: result.CaseID,
			Language: result.Language, Category: result.Category, Status: result.Status,
			FailureCodes: failureCodes, CitationStatus: result.CitationStatus, CitationCount: result.CitationCount,
			DurationMS: result.DurationMS, InputBytes: result.InputBytes, OutputBytes: result.OutputBytes,
			InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, TokenSource: result.TokenSource,
			ErrorCode: result.ErrorCode, CreatedAt: result.CreatedAt,
		})
	}
	return response, nil
}

func aiEvaluationFailureCodeAllowed(code string) bool {
	switch code {
	case "CASE_ID_MISMATCH", "ANSWER_EMPTY", "CONTROL_BLOCK_LEAKED", "CITATION_STATUS_MISMATCH",
		"FACT_CONTRADICTED", "FACT_MISSING",
		"REQUIRED_PHRASE_MISSING", "FORBIDDEN_PHRASE_PRESENT", "CITATION_NOT_ALLOWED",
		"CITATION_DUPLICATE", "CITATION_COUNT_LOW", "CITATION_SET_MISMATCH", "OBSERVATION_MISSING":
		return true
	default:
		return false
	}
}

func (a *API) createAIEvaluation(c *gin.Context) {
	var input createAIEvaluationRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.SuiteKey = strings.TrimSpace(input.SuiteKey)
	if input.SuiteKey == "" {
		input.SuiteKey = aieval.SuiteFull
	}
	parsed, err := uuid.Parse(input.ProviderID)
	if err != nil || parsed.String() != input.ProviderID || input.ProviderVersion < 1 {
		writeError(c, http.StatusUnprocessableEntity, "AI_EVALUATION_PROVIDER_INVALID", "Select a canonical local Provider and its current version")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	dataset, err := aieval.LoadEmbeddedKnowledgeDataset()
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "AI_EVALUATION_DATASET_INVALID", "The built-in evaluation dataset is unavailable")
		return
	}
	cases, err := dataset.CasesForSuite(input.SuiteKey)
	if err != nil {
		writeError(c, http.StatusUnprocessableEntity, "AI_EVALUATION_SUITE_INVALID", "Select a supported code-owned evaluation suite")
		return
	}

	a.aiEvaluationMu.Lock()
	defer a.aiEvaluationMu.Unlock()
	a.aiProviderMu.RLock()
	defer a.aiProviderMu.RUnlock()
	var response aiEvaluationRunResponse
	replayed := false
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var replayStatus int
		var replayErr error
		replayed, replayStatus, replayErr = replayTaskOutputCommand(tx, idempotencyKey, createAIEvaluationEndpoint, requestHash, &response)
		if replayErr != nil {
			return replayErr
		}
		if replayed {
			if replayStatus != http.StatusAccepted {
				return errors.New("invalid AI evaluation replay status")
			}
			return nil
		}
		provider, err := loadAIProvider(tx, input.ProviderID)
		if err != nil {
			return err
		}
		if provider.Version != input.ProviderVersion {
			return taskVersionConflict()
		}
		if provider.Kind != aiProviderKindLocal || provider.Protocol != "openai_chat" {
			return newProjectRequestError(http.StatusConflict, "AI_EVALUATION_LOCAL_PROVIDER_REQUIRED", "Quality evaluation only runs against a local OpenAI-compatible Provider")
		}
		if provider.Status != "ready" || provider.HealthStatus != "healthy" {
			return newProjectRequestError(http.StatusConflict, "AI_EVALUATION_PROVIDER_NOT_READY", "Test the local Provider connection before running evaluation")
		}
		var activeCount int64
		if err := tx.Model(&models.AIEvaluationRun{}).
			Where("provider_id = ? AND status IN ?", provider.ID, []string{"queued", "running"}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return newProjectRequestError(http.StatusConflict, "AI_EVALUATION_ALREADY_ACTIVE", "This Provider already has an active evaluation")
		}
		now := nowStamp(a)
		run := models.AIEvaluationRun{
			ProviderConfigVersion: &provider.ConfigVersion,
			ID:                    uuid.NewString(), ProviderID: provider.ID, ProviderNameSnapshot: provider.Name,
			ProviderModelSnapshot: provider.Model, ProviderProtocolSnapshot: provider.Protocol,
			ProviderVersion: provider.Version, DatasetVersion: dataset.Version, SuiteKey: input.SuiteKey,
			Status: "queued", TotalCases: len(cases), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		response, err = aiEvaluationRunResponseFromModel(run, nil)
		if err != nil {
			return err
		}
		return recordTaskOutputIdempotency(tx, idempotencyKey, createAIEvaluationEndpoint, run.ID, requestHash, http.StatusAccepted, response, now)
	})
	if err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		writeAIProviderLoadError(c, err)
		return
	}
	if replayed {
		c.Header("Idempotency-Replayed", "true")
		c.JSON(http.StatusAccepted, gin.H{"data": response})
		return
	}
	if a.aiEvaluationRunner == nil || !a.aiEvaluationRunner.enqueue(c.Request.Context(), response.ID) {
		a.failAIEvaluationRun(response.ID, "AI_EVALUATION_QUEUE_UNAVAILABLE")
		if idempotencyKey != "" {
			_ = a.db.Where("key = ? AND endpoint = ?", idempotencyKey, createAIEvaluationEndpoint).Delete(&models.IdempotencyKey{}).Error
		}
		writeError(c, http.StatusServiceUnavailable, "AI_EVALUATION_QUEUE_UNAVAILABLE", "The local evaluation queue is unavailable")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": response})
}

func (a *API) listAIEvaluations(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	providerID := strings.TrimSpace(c.Query("provider_id"))
	if providerID != "" {
		parsed, err := uuid.Parse(providerID)
		if err != nil || parsed.String() != providerID {
			writeError(c, http.StatusBadRequest, "INVALID_AI_PROVIDER_ID", "AI provider id must be a canonical UUID")
			return
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "queued" && status != "running" && status != "succeeded" && status != "failed" && status != "cancelled" {
		writeError(c, http.StatusBadRequest, "AI_EVALUATION_STATUS_INVALID", "AI evaluation status is invalid")
		return
	}
	rows := make([]models.AIEvaluationRun, 0)
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.AIEvaluationRun{})
		if providerID != "" {
			query = query.Where("provider_id = ?", providerID)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		return query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	responses := make([]aiEvaluationRunResponse, 0, len(rows))
	for _, row := range rows {
		response, err := aiEvaluationRunResponseFromModel(row, nil)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		responses = append(responses, response)
	}
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": gin.H{"page": page, "page_size": pageSize, "total": total}})
}

func (a *API) getAIEvaluation(c *gin.Context) {
	runID, ok := aiEvaluationID(c)
	if !ok {
		return
	}
	var run models.AIEvaluationRun
	results := make([]models.AIEvaluationResult, 0)
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		return tx.Where("run_id = ?", runID).Order("sequence ASC").Find(&results).Error
	}, &sql.TxOptions{ReadOnly: true})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_EVALUATION_NOT_FOUND", "AI evaluation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	response, err := aiEvaluationRunResponseFromModel(run, results)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (a *API) cancelAIEvaluation(c *gin.Context) {
	runID, ok := aiEvaluationID(c)
	if !ok {
		return
	}
	a.aiEvaluationMu.Lock()
	defer a.aiEvaluationMu.Unlock()
	var run models.AIEvaluationRun
	err := a.db.WithContext(c.Request.Context()).First(&run, "id = ?", runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_EVALUATION_NOT_FOUND", "AI evaluation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if run.Status != "queued" && run.Status != "running" {
		writeError(c, http.StatusConflict, "AI_EVALUATION_ALREADY_TERMINAL", "This evaluation already reached a terminal state")
		return
	}
	now := nowStamp(a)
	updates := map[string]any{"cancel_requested": true, "updated_at": now}
	if run.Status == "queued" {
		updates["status"] = "cancelled"
		updates["current_case_id"] = nil
		updates["completed_at"] = now
	}
	result := a.db.WithContext(c.Request.Context()).Model(&models.AIEvaluationRun{}).
		Where("id = ? AND status = ?", runID, run.Status).Updates(updates)
	if result.Error != nil {
		writeDatabaseError(c)
		return
	}
	if result.RowsAffected != 1 {
		writeError(c, http.StatusConflict, "AI_EVALUATION_ALREADY_TERMINAL", "This evaluation state changed; refresh it")
		return
	}
	if run.Status == "running" && a.aiEvaluationRunner != nil {
		a.aiEvaluationRunner.cancel(runID)
	}
	if err := a.db.First(&run, "id = ?", runID).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	response, err := aiEvaluationRunResponseFromModel(run, nil)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"data": response})
}

func (a *API) deleteAIEvaluation(c *gin.Context) {
	runID, ok := aiEvaluationID(c)
	if !ok {
		return
	}
	if c.Query("confirm") != "true" {
		writeError(c, http.StatusPreconditionRequired, "CONFIRMATION_REQUIRED", "Set confirm=true to delete local evaluation history")
		return
	}
	a.aiEvaluationMu.Lock()
	defer a.aiEvaluationMu.Unlock()
	var run models.AIEvaluationRun
	err := a.db.WithContext(c.Request.Context()).First(&run, "id = ?", runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "AI_EVALUATION_NOT_FOUND", "AI evaluation not found")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if run.Status == "queued" || run.Status == "running" {
		writeError(c, http.StatusConflict, "AI_EVALUATION_ACTIVE", "Cancel the active evaluation before deleting it")
		return
	}
	if err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("run_id = ?", runID).Delete(&models.AIEvaluationResult{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND status NOT IN ?", runID, []string{"queued", "running"}).Delete(&models.AIEvaluationRun{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errAIEvaluationNoLongerRunnable
		}
		return nil
	}); err != nil {
		writeDatabaseError(c)
		return
	}
	c.Status(http.StatusNoContent)
}

func aiEvaluationID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_AI_EVALUATION_ID", "AI evaluation id must be a canonical UUID")
		return "", false
	}
	return id, true
}
