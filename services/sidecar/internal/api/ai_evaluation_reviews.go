package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const createAIEvaluationReviewEndpoint = "POST /api/v1/ai/evaluation-reviews"

type createAIEvaluationReviewRequest struct {
	ProviderConfigVersion      *int64   `json:"provider_config_version"`
	ProviderID                 string   `json:"provider_id"`
	ProviderNameSnapshot       string   `json:"provider_name_snapshot"`
	ProviderModelSnapshot      string   `json:"provider_model_snapshot"`
	DatasetVersion             int      `json:"dataset_version"`
	SuiteKey                   string   `json:"suite_key"`
	ExpectedProviderVersionMin int64    `json:"expected_provider_version_min"`
	ExpectedProviderVersionMax int64    `json:"expected_provider_version_max"`
	ExpectedLastCompletedAt    string   `json:"expected_last_completed_at"`
	ExpectedRunCount           int64    `json:"expected_run_count"`
	ExpectedTotalCases         int64    `json:"expected_total_cases"`
	ExpectedPassedCases        int64    `json:"expected_passed_cases"`
	ExpectedFailedCases        int64    `json:"expected_failed_cases"`
	ExpectedReadinessStatus    string   `json:"expected_readiness_status"`
	ExpectedReadinessReasons   []string `json:"expected_readiness_reasons"`
	Decision                   string   `json:"decision"`
	Reason                     string   `json:"reason"`
}

type aiEvaluationReviewResponse struct {
	ProviderConfigVersion         *int64   `json:"provider_config_version"`
	ID                            string   `json:"id"`
	ProviderIDSnapshot            string   `json:"provider_id_snapshot"`
	ProviderNameSnapshot          string   `json:"provider_name_snapshot"`
	ProviderModelSnapshot         string   `json:"provider_model_snapshot"`
	DatasetVersion                int      `json:"dataset_version"`
	SuiteKey                      string   `json:"suite_key"`
	ProviderVersionMin            int64    `json:"provider_version_min"`
	ProviderVersionMax            int64    `json:"provider_version_max"`
	GroupLastCompletedAt          string   `json:"group_last_completed_at"`
	RunCount                      int64    `json:"run_count"`
	TotalCases                    int64    `json:"total_cases"`
	PassedCases                   int64    `json:"passed_cases"`
	FailedCases                   int64    `json:"failed_cases"`
	OverallWilsonLowerBPS         int      `json:"overall_wilson_lower_bps"`
	MinimumCategory               string   `json:"minimum_category"`
	MinimumCategoryWilsonLowerBPS int      `json:"minimum_category_wilson_lower_bps"`
	ReadinessStatus               string   `json:"readiness_status"`
	ReadinessReasons              []string `json:"readiness_reasons"`
	CriticalFailureCodes          []string `json:"critical_failure_codes"`
	Decision                      string   `json:"decision"`
	Reason                        string   `json:"reason"`
	ReviewedByActorID             string   `json:"reviewed_by_actor_id"`
	ReviewedByActorNameSnapshot   string   `json:"reviewed_by_actor_name_snapshot"`
	CreatedAt                     string   `json:"created_at"`
}

type aiEvaluationReviewSnapshot struct {
	Group                aiEvaluationQualityGroup
	MinimumCategory      string
	MinimumCategoryLower int
	CriticalCodes        []string
}

func aiEvaluationReviewResponseFromModel(row models.AIEvaluationReview) (aiEvaluationReviewResponse, error) {
	readinessReasons := make([]string, 0)
	criticalCodes := make([]string, 0)
	if err := json.Unmarshal([]byte(row.ReadinessReasonsJSON), &readinessReasons); err != nil || readinessReasons == nil {
		return aiEvaluationReviewResponse{}, errors.New("invalid AI evaluation review reasons")
	}
	if err := json.Unmarshal([]byte(row.CriticalFailureCodesJSON), &criticalCodes); err != nil || criticalCodes == nil {
		return aiEvaluationReviewResponse{}, errors.New("invalid AI evaluation review critical codes")
	}
	if !aiEvaluationReadinessStatusAllowed(row.ReadinessStatus) || !aiEvaluationReviewDecisionAllowed(row.Decision) ||
		!aieval.SuiteKeyAllowed(row.SuiteKey) {
		return aiEvaluationReviewResponse{}, errors.New("invalid AI evaluation review identity")
	}
	for _, reason := range readinessReasons {
		if !aiEvaluationReadinessReasonAllowed(reason) {
			return aiEvaluationReviewResponse{}, errors.New("invalid AI evaluation review reason")
		}
	}
	for _, code := range criticalCodes {
		if !stringSliceContains(aiEvaluationCriticalFailureCodes, code) {
			return aiEvaluationReviewResponse{}, errors.New("invalid AI evaluation review critical code")
		}
	}
	return aiEvaluationReviewResponse{
		ProviderConfigVersion: row.ProviderConfigVersion,
		ID:                    row.ID, ProviderIDSnapshot: row.ProviderIDSnapshot,
		ProviderNameSnapshot: row.ProviderNameSnapshot, ProviderModelSnapshot: row.ProviderModelSnapshot,
		DatasetVersion: row.DatasetVersion, SuiteKey: row.SuiteKey,
		ProviderVersionMin: row.ProviderVersionMin, ProviderVersionMax: row.ProviderVersionMax,
		GroupLastCompletedAt: row.GroupLastCompletedAt, RunCount: row.RunCount,
		TotalCases: row.TotalCases, PassedCases: row.PassedCases, FailedCases: row.FailedCases,
		OverallWilsonLowerBPS: row.OverallWilsonLowerBPS,
		MinimumCategory:       row.MinimumCategory, MinimumCategoryWilsonLowerBPS: row.MinimumCategoryWilsonLowerBPS,
		ReadinessStatus: row.ReadinessStatus, ReadinessReasons: readinessReasons,
		CriticalFailureCodes: criticalCodes, Decision: row.Decision, Reason: row.Reason,
		ReviewedByActorID:           row.ReviewedByActorID,
		ReviewedByActorNameSnapshot: row.ReviewedByActorNameSnapshot, CreatedAt: row.CreatedAt,
	}, nil
}

func (a *API) createAIEvaluationReview(c *gin.Context) {
	var input createAIEvaluationReviewRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if !normalizeAndValidateAIEvaluationReviewInput(&input) {
		writeError(c, http.StatusUnprocessableEntity, "AI_EVALUATION_REVIEW_INVALID", "The evaluation review selection, snapshot, decision, or reason is invalid")
		return
	}
	idempotencyKey, requestHash, ok := taskOutputCommandIdempotency(c, input)
	if !ok {
		return
	}
	var response aiEvaluationReviewResponse
	replayed := false
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var replayStatus int
		var replayErr error
		replayed, replayStatus, replayErr = replayTaskOutputCommand(
			tx, idempotencyKey, createAIEvaluationReviewEndpoint, requestHash, &response,
		)
		if replayErr != nil {
			return replayErr
		}
		if replayed {
			if replayStatus != http.StatusCreated {
				return errors.New("invalid AI evaluation review replay status")
			}
			return nil
		}
		snapshot, err := loadAIEvaluationReviewSnapshot(tx, input)
		if err != nil {
			return err
		}
		if !aiEvaluationReviewSnapshotMatchesInput(snapshot.Group, input) {
			return newProjectRequestError(http.StatusConflict, "AI_EVALUATION_REVIEW_STALE", "The evaluation evidence changed; refresh before recording a decision")
		}
		var owner models.Actor
		if err := tx.Select("id", "display_name", "status").First(&owner, "id = ?", models.BuiltinOwnerActorID).Error; err != nil {
			return err
		}
		if owner.Status != "active" || strings.TrimSpace(owner.DisplayName) == "" {
			return errors.New("built-in owner is unavailable for AI evaluation review")
		}
		readinessJSON, err := json.Marshal(snapshot.Group.ReadinessReasons)
		if err != nil {
			return err
		}
		criticalJSON, err := json.Marshal(snapshot.CriticalCodes)
		if err != nil {
			return err
		}
		now := nowStamp(a)
		row := models.AIEvaluationReview{
			ProviderConfigVersion: snapshot.Group.ProviderConfigVersion,
			ID:                    uuid.NewString(), ProviderIDSnapshot: snapshot.Group.ProviderID,
			ProviderNameSnapshot:  snapshot.Group.ProviderNameSnapshot,
			ProviderModelSnapshot: snapshot.Group.ProviderModelSnapshot,
			DatasetVersion:        snapshot.Group.DatasetVersion, SuiteKey: snapshot.Group.SuiteKey,
			ProviderVersionMin: snapshot.Group.ProviderVersionMin, ProviderVersionMax: snapshot.Group.ProviderVersionMax,
			GroupLastCompletedAt: snapshot.Group.LastCompletedAt, RunCount: snapshot.Group.RunCount,
			TotalCases: snapshot.Group.TotalCases, PassedCases: snapshot.Group.PassedCases,
			FailedCases: snapshot.Group.FailedCases, OverallWilsonLowerBPS: snapshot.Group.WilsonLowerBPS,
			MinimumCategory:               snapshot.MinimumCategory,
			MinimumCategoryWilsonLowerBPS: snapshot.MinimumCategoryLower,
			ReadinessStatus:               snapshot.Group.ReadinessStatus, ReadinessReasonsJSON: string(readinessJSON),
			CriticalFailureCodesJSON: string(criticalJSON), Decision: input.Decision, Reason: input.Reason,
			ReviewedByActorID: owner.ID, ReviewedByActorNameSnapshot: owner.DisplayName, CreatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		response, err = aiEvaluationReviewResponseFromModel(row)
		if err != nil {
			return err
		}
		return recordTaskOutputIdempotency(
			tx, idempotencyKey, createAIEvaluationReviewEndpoint, row.ID, requestHash, http.StatusCreated, response, now,
		)
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
	}
	c.JSON(http.StatusCreated, gin.H{"data": response})
}

func (a *API) listAIEvaluationReviews(c *gin.Context) {
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
	decision := strings.TrimSpace(c.Query("decision"))
	if decision != "" && !aiEvaluationReviewDecisionAllowed(decision) {
		writeError(c, http.StatusBadRequest, "AI_EVALUATION_REVIEW_DECISION_INVALID", "AI evaluation review decision is invalid")
		return
	}
	rows := make([]models.AIEvaluationReview, 0)
	var total int64
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.AIEvaluationReview{})
		if providerID != "" {
			query = query.Where("provider_id_snapshot = ?", providerID)
		}
		if decision != "" {
			query = query.Where("decision = ?", decision)
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
	responses := make([]aiEvaluationReviewResponse, 0, len(rows))
	for _, row := range rows {
		response, err := aiEvaluationReviewResponseFromModel(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		responses = append(responses, response)
	}
	c.JSON(http.StatusOK, gin.H{"data": responses, "meta": gin.H{"page": page, "page_size": pageSize, "total": total}})
}

func normalizeAndValidateAIEvaluationReviewInput(input *createAIEvaluationReviewRequest) bool {
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ProviderNameSnapshot = strings.TrimSpace(input.ProviderNameSnapshot)
	input.ProviderModelSnapshot = strings.TrimSpace(input.ProviderModelSnapshot)
	input.SuiteKey = strings.TrimSpace(input.SuiteKey)
	input.ExpectedLastCompletedAt = strings.TrimSpace(input.ExpectedLastCompletedAt)
	input.ExpectedReadinessStatus = strings.TrimSpace(input.ExpectedReadinessStatus)
	input.Decision = strings.TrimSpace(input.Decision)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ProviderConfigVersion != nil && *input.ProviderConfigVersion < 1 {
		return false
	}
	parsed, err := uuid.Parse(input.ProviderID)
	if err != nil || parsed.String() != input.ProviderID ||
		utf8.RuneCountInString(input.ProviderNameSnapshot) < 1 || utf8.RuneCountInString(input.ProviderNameSnapshot) > 100 ||
		utf8.RuneCountInString(input.ProviderModelSnapshot) < 1 || utf8.RuneCountInString(input.ProviderModelSnapshot) > 200 ||
		input.DatasetVersion < 1 || !aieval.SuiteKeyAllowed(input.SuiteKey) ||
		input.ExpectedProviderVersionMin < 1 || input.ExpectedProviderVersionMax < input.ExpectedProviderVersionMin ||
		input.ExpectedRunCount < 1 || input.ExpectedTotalCases < 1 || input.ExpectedPassedCases < 0 ||
		input.ExpectedFailedCases < 0 || input.ExpectedPassedCases+input.ExpectedFailedCases != input.ExpectedTotalCases ||
		!aiEvaluationReadinessStatusAllowed(input.ExpectedReadinessStatus) ||
		!aiEvaluationReviewDecisionAllowed(input.Decision) ||
		utf8.RuneCountInString(input.Reason) < 1 || utf8.RuneCountInString(input.Reason) > 1000 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, input.ExpectedLastCompletedAt); err != nil {
		return false
	}
	if input.ExpectedReadinessReasons == nil || len(input.ExpectedReadinessReasons) > 7 {
		return false
	}
	seen := make(map[string]struct{}, len(input.ExpectedReadinessReasons))
	for _, reason := range input.ExpectedReadinessReasons {
		if !aiEvaluationReadinessReasonAllowed(reason) {
			return false
		}
		if _, duplicate := seen[reason]; duplicate {
			return false
		}
		seen[reason] = struct{}{}
	}
	return true
}

func aiEvaluationReviewSnapshotMatchesInput(group aiEvaluationQualityGroup, input createAIEvaluationReviewRequest) bool {
	return aiConfigVersionsEqual(group.ProviderConfigVersion, input.ProviderConfigVersion) && group.ProviderID == input.ProviderID && group.ProviderNameSnapshot == input.ProviderNameSnapshot &&
		group.ProviderModelSnapshot == input.ProviderModelSnapshot && group.DatasetVersion == input.DatasetVersion &&
		group.SuiteKey == input.SuiteKey && group.ProviderVersionMin == input.ExpectedProviderVersionMin &&
		group.ProviderVersionMax == input.ExpectedProviderVersionMax && group.LastCompletedAt == input.ExpectedLastCompletedAt &&
		group.RunCount == input.ExpectedRunCount && group.TotalCases == input.ExpectedTotalCases &&
		group.PassedCases == input.ExpectedPassedCases && group.FailedCases == input.ExpectedFailedCases &&
		group.ReadinessStatus == input.ExpectedReadinessStatus && stringSlicesEqual(group.ReadinessReasons, input.ExpectedReadinessReasons)
}

func loadAIEvaluationReviewSnapshot(tx *gorm.DB, input createAIEvaluationReviewRequest) (aiEvaluationReviewSnapshot, error) {
	var group aiEvaluationQualityGroup
	if err := tx.Raw(`SELECT
		provider_id, provider_config_version, MAX(provider_name_snapshot) AS provider_name_snapshot, MAX(provider_model_snapshot) AS provider_model_snapshot, dataset_version, suite_key,
		MIN(provider_version) AS provider_version_min, MAX(provider_version) AS provider_version_max,
		COUNT(*) AS run_count,
		COALESCE(SUM(CASE WHEN failed_cases = 0 THEN 1 ELSE 0 END), 0) AS fully_passed_runs,
		COALESCE(SUM(total_cases), 0) AS total_cases,
		COALESCE(SUM(passed_cases), 0) AS passed_cases,
		COALESCE(SUM(failed_cases), 0) AS failed_cases,
		MAX(completed_at) AS last_completed_at
		FROM ai_evaluation_runs
		WHERE provider_id = ? AND provider_config_version IS ? AND (? IS NOT NULL OR (provider_name_snapshot = ? AND provider_model_snapshot = ?))
		  AND dataset_version = ? AND suite_key = ? AND status = 'succeeded'
		GROUP BY provider_id, provider_config_version, dataset_version, suite_key`,
		input.ProviderID, input.ProviderConfigVersion, input.ProviderConfigVersion, input.ProviderNameSnapshot, input.ProviderModelSnapshot, input.DatasetVersion, input.SuiteKey,
	).Scan(&group).Error; err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	if group.RunCount == 0 {
		return aiEvaluationReviewSnapshot{}, newProjectRequestError(http.StatusNotFound, "AI_EVALUATION_GROUP_NOT_FOUND", "AI evaluation quality group not found")
	}
	passRate, lower, upper, err := aiEvaluationWilsonIntervalBPS(group.PassedCases, group.TotalCases)
	if err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	group.PassRateBPS, group.WilsonLowerBPS, group.WilsonUpperBPS = passRate, lower, upper
	group.EvidenceLevel = aiEvaluationEvidenceLevel(group.RunCount)
	categories := make([]aiEvaluationCategoryGroup, 0, 4)
	if err := tx.Raw(`SELECT
		run.provider_id, run.provider_config_version, MAX(run.provider_name_snapshot) AS provider_name_snapshot, MAX(run.provider_model_snapshot) AS provider_model_snapshot,
		run.dataset_version, run.suite_key, result.category,
		COUNT(*) AS total_cases,
		COALESCE(SUM(CASE WHEN result.status = 'passed' THEN 1 ELSE 0 END), 0) AS passed_cases,
		COALESCE(SUM(CASE WHEN result.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_cases,
		MAX(run.completed_at) AS last_completed_at
		FROM ai_evaluation_runs run
		JOIN ai_evaluation_results result ON result.run_id = run.id
		WHERE run.provider_id = ? AND run.provider_config_version IS ? AND (? IS NOT NULL OR (run.provider_name_snapshot = ? AND run.provider_model_snapshot = ?))
		  AND run.dataset_version = ? AND run.suite_key = ? AND run.status = 'succeeded'
		GROUP BY run.provider_id, run.provider_config_version,
		  run.dataset_version, run.suite_key, result.category`,
		input.ProviderID, input.ProviderConfigVersion, input.ProviderConfigVersion, input.ProviderNameSnapshot, input.ProviderModelSnapshot, input.DatasetVersion, input.SuiteKey,
	).Scan(&categories).Error; err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	for index := range categories {
		passRate, lower, upper, err := aiEvaluationWilsonIntervalBPS(categories[index].PassedCases, categories[index].TotalCases)
		if err != nil {
			return aiEvaluationReviewSnapshot{}, err
		}
		categories[index].PassRateBPS, categories[index].WilsonLowerBPS, categories[index].WilsonUpperBPS = passRate, lower, upper
	}
	failures := make([]aiEvaluationFailureGroup, 0)
	if err := tx.Raw(`SELECT
		run.provider_id, run.provider_config_version, MAX(run.provider_name_snapshot) AS provider_name_snapshot, MAX(run.provider_model_snapshot) AS provider_model_snapshot,
		run.dataset_version, run.suite_key, CAST(failure.value AS TEXT) AS failure_code,
		COUNT(DISTINCT result.id) AS affected_cases, COUNT(*) AS occurrences,
		MAX(run.completed_at) AS last_completed_at
		FROM ai_evaluation_runs run
		JOIN ai_evaluation_results result ON result.run_id = run.id
		JOIN json_each(result.failure_codes) failure
		WHERE run.provider_id = ? AND run.provider_config_version IS ? AND (? IS NOT NULL OR (run.provider_name_snapshot = ? AND run.provider_model_snapshot = ?))
		  AND run.dataset_version = ? AND run.suite_key = ? AND run.status = 'succeeded' AND result.status = 'failed'
		GROUP BY run.provider_id, run.provider_config_version,
		  run.dataset_version, run.suite_key, failure.value`,
		input.ProviderID, input.ProviderConfigVersion, input.ProviderConfigVersion, input.ProviderNameSnapshot, input.ProviderModelSnapshot, input.DatasetVersion, input.SuiteKey,
	).Scan(&failures).Error; err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	for _, failure := range failures {
		if !aiEvaluationFailureCodeAllowed(failure.FailureCode) || failure.AffectedCases < 1 ||
			failure.Occurrences < failure.AffectedCases || failure.AffectedCases > group.FailedCases {
			return aiEvaluationReviewSnapshot{}, errors.New("invalid AI evaluation review failure aggregate")
		}
	}
	dataset, err := aieval.LoadEmbeddedKnowledgeDataset()
	if err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	if group.DatasetVersion == dataset.Version {
		suiteCases, err := dataset.CasesForSuite(group.SuiteKey)
		if err != nil {
			return aiEvaluationReviewSnapshot{}, err
		}
		expectedCategories := make(map[string]int64, len(aiEvaluationRequiredCategories))
		for _, item := range suiteCases {
			expectedCategories[item.Category]++
		}
		if len(categories) != len(expectedCategories) {
			return aiEvaluationReviewSnapshot{}, errors.New("AI evaluation review category set is incomplete")
		}
		for _, category := range categories {
			perRun, exists := expectedCategories[category.Category]
			if !exists || category.TotalCases != perRun*group.RunCount {
				return aiEvaluationReviewSnapshot{}, errors.New("AI evaluation review category totals are inconsistent")
			}
		}
	}
	policy := aiEvaluationReadinessPolicy{
		Mode: "advisory", CurrentDatasetVersion: dataset.Version, RequiredSuite: aieval.SuiteFull,
		MinimumCompletedRuns: aiEvaluationRepeatedRunMinimum, MinimumOverallLowerBPS: aiEvaluationOverallLowerBPS,
		MinimumCategoryLowerBPS: aiEvaluationCategoryLowerBPS,
		RequiredCategories:      append([]string(nil), aiEvaluationRequiredCategories...),
		CriticalFailureCodes:    append([]string(nil), aiEvaluationCriticalFailureCodes...),
	}
	groups := []aiEvaluationQualityGroup{group}
	if err := applyAIEvaluationReadiness(groups, categories, failures, policy); err != nil {
		return aiEvaluationReviewSnapshot{}, err
	}
	group = groups[0]
	minimumCategory := ""
	minimumLower := 10_001
	categoryByName := make(map[string]aiEvaluationCategoryGroup, len(categories))
	for _, category := range categories {
		categoryByName[category.Category] = category
	}
	for _, categoryName := range aiEvaluationRequiredCategories {
		category, exists := categoryByName[categoryName]
		if !exists {
			continue
		}
		if category.WilsonLowerBPS < minimumLower {
			minimumCategory, minimumLower = categoryName, category.WilsonLowerBPS
		}
	}
	if minimumCategory == "" {
		return aiEvaluationReviewSnapshot{}, errors.New("AI evaluation review category is missing")
	}
	failureCodes := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		failureCodes[failure.FailureCode] = struct{}{}
	}
	criticalCodes := make([]string, 0, len(aiEvaluationCriticalFailureCodes))
	for _, code := range aiEvaluationCriticalFailureCodes {
		if _, exists := failureCodes[code]; exists {
			criticalCodes = append(criticalCodes, code)
		}
	}
	return aiEvaluationReviewSnapshot{
		Group: group, MinimumCategory: minimumCategory,
		MinimumCategoryLower: minimumLower, CriticalCodes: criticalCodes,
	}, nil
}

func aiEvaluationReviewDecisionAllowed(value string) bool {
	return value == "accepted_for_local_use" || value == "needs_more_evidence" || value == "rejected"
}

func aiEvaluationReadinessStatusAllowed(value string) bool {
	return value == "insufficient_evidence" || value == "needs_attention" || value == "review_candidate"
}

func aiEvaluationReadinessReasonAllowed(value string) bool {
	switch value {
	case "OUTDATED_DATASET", "SUITE_NOT_ELIGIBLE", "RUN_COUNT_LOW", "PROVIDER_VERSION_MIXED",
		"OVERALL_LOWER_BOUND_LOW", "CATEGORY_LOWER_BOUND_LOW", "CRITICAL_FAILURE_PRESENT":
		return true
	default:
		return false
	}
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func aiConfigVersionsEqual(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}
