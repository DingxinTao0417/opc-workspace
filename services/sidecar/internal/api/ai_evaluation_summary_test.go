package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func seedEvaluationSummaryVersionedRun(
	t *testing.T,
	store *database.Store,
	provider models.AIProvider,
	datasetVersion, totalCases int,
	suiteKey string,
	completedAt string,
	configVersions ...*int64,
) models.AIEvaluationRun {
	t.Helper()
	startedAt := completedAt
	run := models.AIEvaluationRun{
		ID: uuid.NewString(), ProviderID: provider.ID, ProviderNameSnapshot: provider.Name,
		ProviderModelSnapshot: provider.Model, ProviderProtocolSnapshot: provider.Protocol,
		ProviderVersion: provider.Version, DatasetVersion: datasetVersion, Status: "succeeded",
		SuiteKey:   suiteKey,
		TotalCases: totalCases, CompletedCases: totalCases, PassedCases: totalCases,
		StartedAt: &startedAt, CompletedAt: &completedAt, CreatedAt: completedAt, UpdatedAt: completedAt,
	}
	if len(configVersions) > 0 {
		run.ProviderConfigVersion = configVersions[0]
	}
	if err := store.DB.Create(&run).Error; err != nil {
		t.Fatalf("create dataset v%d summary run: %v", datasetVersion, err)
	}
	categories := []string{"grounded", "no_evidence", "prompt_injection", "conflicting_sources"}
	categorySize := totalCases / len(categories)
	for index := 0; index < totalCases; index++ {
		category := suiteKey
		if !aieval.TopicSuiteKeyAllowed(suiteKey) {
			category = categories[index/categorySize]
		}
		citationStatus, citationCount := "validated", 1
		if category == "no_evidence" {
			citationStatus, citationCount = "no_evidence", 0
		}
		result := models.AIEvaluationResult{
			ID: uuid.NewString(), RunID: run.ID, Sequence: index + 1,
			CaseID: fmt.Sprintf("dataset_v%d_case_%02d", datasetVersion, index+1), Language: "zh-CN",
			Category: category, Status: "passed", FailureCodes: "[]",
			CitationStatus: &citationStatus, CitationCount: citationCount,
			DurationMS: 1, InputBytes: 1, OutputBytes: 1, CreatedAt: completedAt,
		}
		if index%2 == 1 {
			result.Language = "en"
		}
		if err := store.DB.Create(&result).Error; err != nil {
			t.Fatalf("create dataset v%d summary result %d: %v", datasetVersion, index+1, err)
		}
	}
	return run
}

func seedEvaluationSummaryRun(
	t *testing.T,
	store *database.Store,
	provider models.AIProvider,
	model, status, completedAt string,
	passed, failed, errorCases int,
) models.AIEvaluationRun {
	t.Helper()
	startedAt := completedAt
	run := models.AIEvaluationRun{
		ID: uuid.NewString(), ProviderID: provider.ID, ProviderNameSnapshot: provider.Name,
		ProviderModelSnapshot: model, ProviderProtocolSnapshot: provider.Protocol,
		ProviderVersion: provider.Version, DatasetVersion: 1, Status: status,
		SuiteKey:   aieval.SuiteFull,
		TotalCases: 4, CompletedCases: passed + failed + errorCases,
		PassedCases: passed, FailedCases: failed, ErrorCases: errorCases,
		StartedAt: &startedAt, CreatedAt: completedAt, UpdatedAt: completedAt,
	}
	switch status {
	case "succeeded":
		run.CompletedAt = &completedAt
	case "failed":
		run.CompletedAt = &completedAt
		run.ErrorCode = stringPointer("AI_ENDPOINT_UNREACHABLE")
	case "cancelled":
		run.CompletedAt = &completedAt
		run.CancelRequested = true
	case "running":
		run.CompletedCases = 0
		run.PassedCases = 0
		run.FailedCases = 0
		run.ErrorCases = 0
	}
	if err := store.DB.Create(&run).Error; err != nil {
		t.Fatalf("create %s summary run: %v", status, err)
	}
	if status == "succeeded" {
		seedEvaluationSummaryResults(t, store, run, completedAt)
	}
	return run
}

func seedEvaluationSummaryResults(t *testing.T, store *database.Store, run models.AIEvaluationRun, createdAt string) {
	t.Helper()
	categories := []struct {
		id, language, category string
	}{
		{"zh_invoice_grounded", "zh-CN", "grounded"},
		{"zh_no_evidence_refund", "zh-CN", "no_evidence"},
		{"en_prompt_injection_is_quoted", "en", "prompt_injection"},
		{"zh_conflicting_payment_terms", "zh-CN", "conflicting_sources"},
	}
	for index, item := range categories {
		status, failureCodes := "failed", `["REQUIRED_PHRASE_MISSING"]`
		if index < run.PassedCases {
			status, failureCodes = "passed", "[]"
		} else if item.category == "prompt_injection" {
			failureCodes = `["FORBIDDEN_PHRASE_PRESENT","REQUIRED_PHRASE_MISSING","REQUIRED_PHRASE_MISSING"]`
		} else if item.category == "conflicting_sources" {
			failureCodes = `["CITATION_SET_MISMATCH","REQUIRED_PHRASE_MISSING"]`
		}
		citationStatus, citationCount := "validated", 1
		if item.category == "no_evidence" {
			citationStatus, citationCount = "no_evidence", 0
		}
		result := models.AIEvaluationResult{
			ID: uuid.NewString(), RunID: run.ID, Sequence: index + 1, CaseID: item.id,
			Language: item.language, Category: item.category, Status: status,
			FailureCodes: failureCodes, CitationStatus: &citationStatus, CitationCount: citationCount,
			DurationMS: 1, InputBytes: 1, OutputBytes: 1, CreatedAt: createdAt,
		}
		if err := store.DB.Create(&result).Error; err != nil {
			t.Fatalf("create %s summary result: %v", item.category, err)
		}
	}
}

func TestAIEvaluationSummarySeparatesStatusAndDatasetModelGroups(t *testing.T) {
	router, store, providerA := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	now := time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)
	providerB := createCompactionTestProvider(t, store, now, uuid.NewString())
	providerB.Name = "Second local"
	providerB.Version++
	providerB.UpdatedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	if err := store.DB.Save(&providerB).Error; err != nil {
		t.Fatalf("rename second provider: %v", err)
	}

	seedEvaluationSummaryRun(t, store, providerA, "alpha", "succeeded", "2026-09-09T09:00:01Z", 4, 0, 0)
	secondAlpha := seedEvaluationSummaryRun(t, store, providerA, "alpha", "succeeded", "2026-09-09T09:00:02Z", 3, 1, 0)
	beta := seedEvaluationSummaryRun(t, store, providerA, "beta", "succeeded", "2026-09-09T09:00:03Z", 2, 2, 0)
	seedEvaluationSummaryRun(t, store, providerA, "alpha", "failed", "2026-09-09T09:00:04Z", 0, 0, 1)
	seedEvaluationSummaryRun(t, store, providerA, "alpha", "running", "2026-09-09T09:00:05Z", 0, 0, 0)
	seedEvaluationSummaryRun(t, store, providerB, "alpha", "succeeded", "2026-09-09T09:00:06Z", 1, 3, 0)

	response := performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id="+providerA.ID+"&limit=2", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("evaluation summary = %d: %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{`"answer":`, `"question":`, `"content":`, `"reasoning":`, `"base_url":`, `"api_key":`} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("evaluation summary leaked %s: %s", forbidden, response.Body.String())
		}
	}
	var envelope struct {
		Data aiEvaluationSummaryResponse `json:"data"`
		Meta struct {
			TrendLimit int `json:"trend_limit"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode evaluation summary: %v", err)
	}
	data := envelope.Data
	if data.Scope.ProviderID == nil || *data.Scope.ProviderID != providerA.ID || envelope.Meta.TrendLimit != 2 {
		t.Fatalf("summary scope/meta = %#v %#v", data.Scope, envelope.Meta)
	}
	if data.Uncertainty != (aiEvaluationUncertainty{
		Method: aiEvaluationConfidenceMethod, ConfidenceLevelBPS: 9500, RepeatedRunMinimum: 3,
	}) {
		t.Fatalf("summary uncertainty = %#v", data.Uncertainty)
	}
	if data.ReadinessPolicy.Mode != "advisory" || data.ReadinessPolicy.CurrentDatasetVersion != 4 ||
		data.ReadinessPolicy.RequiredSuite != aieval.SuiteFull ||
		data.ReadinessPolicy.MinimumCompletedRuns != 3 || data.ReadinessPolicy.MinimumOverallLowerBPS != 8000 ||
		data.ReadinessPolicy.MinimumCategoryLowerBPS != 6000 ||
		!reflect.DeepEqual(data.ReadinessPolicy.RequiredCategories, aiEvaluationRequiredCategories) ||
		!reflect.DeepEqual(data.ReadinessPolicy.CriticalFailureCodes, aiEvaluationCriticalFailureCodes) {
		t.Fatalf("summary readiness policy = %#v", data.ReadinessPolicy)
	}
	wantStatus := aiEvaluationStatusCounts{Total: 5, Running: 1, Succeeded: 3, Failed: 1}
	if data.StatusCounts != wantStatus {
		t.Fatalf("status counts = %#v, want %#v", data.StatusCounts, wantStatus)
	}
	if len(data.Groups) != 2 {
		t.Fatalf("quality groups = %#v", data.Groups)
	}
	if len(data.Categories) != 8 {
		t.Fatalf("category groups = %#v", data.Categories)
	}
	categories := make(map[string]aiEvaluationCategoryGroup, len(data.Categories))
	for _, category := range data.Categories {
		categories[category.ProviderModelSnapshot+":"+category.Category] = category
	}
	if category := categories["alpha:grounded"]; category.TotalCases != 2 || category.PassedCases != 2 || category.FailedCases != 0 {
		t.Fatalf("alpha grounded category = %#v", category)
	} else if category.PassRateBPS != 10000 || category.WilsonLowerBPS != 3424 || category.WilsonUpperBPS != 10000 {
		t.Fatalf("alpha grounded interval = %#v", category)
	}
	if category := categories["alpha:conflicting_sources"]; category.TotalCases != 2 || category.PassedCases != 1 || category.FailedCases != 1 {
		t.Fatalf("alpha conflict category = %#v", category)
	} else if category.PassRateBPS != 5000 || category.WilsonLowerBPS != 945 || category.WilsonUpperBPS != 9055 {
		t.Fatalf("alpha conflict interval = %#v", category)
	}
	if category := categories["beta:prompt_injection"]; category.TotalCases != 1 || category.PassedCases != 0 || category.FailedCases != 1 {
		t.Fatalf("beta injection category = %#v", category)
	} else if category.PassRateBPS != 0 || category.WilsonLowerBPS != 0 || category.WilsonUpperBPS != 7935 {
		t.Fatalf("beta injection interval = %#v", category)
	}
	if len(data.FailureCodes) != 5 {
		t.Fatalf("failure-code groups = %#v", data.FailureCodes)
	}
	failures := make(map[string]aiEvaluationFailureGroup, len(data.FailureCodes))
	for _, failure := range data.FailureCodes {
		failures[failure.ProviderModelSnapshot+":"+failure.FailureCode] = failure
	}
	if failure := failures["alpha:CITATION_SET_MISMATCH"]; failure.AffectedCases != 1 || failure.Occurrences != 1 {
		t.Fatalf("alpha citation failure = %#v", failure)
	}
	if failure := failures["beta:REQUIRED_PHRASE_MISSING"]; failure.AffectedCases != 2 || failure.Occurrences != 3 {
		t.Fatalf("beta required-phrase failure = %#v", failure)
	}
	groups := make(map[string]aiEvaluationQualityGroup, len(data.Groups))
	for _, group := range data.Groups {
		groups[group.ProviderModelSnapshot] = group
	}
	if alpha := groups["alpha"]; alpha.RunCount != 2 || alpha.FullyPassedRuns != 1 || alpha.TotalCases != 8 || alpha.PassedCases != 7 || alpha.FailedCases != 1 || alpha.LastCompletedAt != "2026-09-09T09:00:02Z" {
		t.Fatalf("alpha group = %#v", alpha)
	} else if alpha.PassRateBPS != 8750 || alpha.WilsonLowerBPS != 5291 || alpha.WilsonUpperBPS != 9776 || alpha.EvidenceLevel != "limited_runs" {
		t.Fatalf("alpha interval = %#v", alpha)
	} else if alpha.ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(alpha.ReadinessReasons, []string{"OUTDATED_DATASET"}) {
		t.Fatalf("alpha readiness = %#v", alpha)
	} else if alpha.ProviderVersionMin != 1 || alpha.ProviderVersionMax != 1 {
		t.Fatalf("alpha provider versions = %#v", alpha)
	} else if alpha.SuiteKey != aieval.SuiteFull {
		t.Fatalf("alpha suite = %#v", alpha)
	}
	if group := groups["beta"]; group.RunCount != 1 || group.FullyPassedRuns != 0 || group.TotalCases != 4 || group.PassedCases != 2 || group.FailedCases != 2 {
		t.Fatalf("beta group = %#v", group)
	} else if group.PassRateBPS != 5000 || group.WilsonLowerBPS != 1500 || group.WilsonUpperBPS != 8500 || group.EvidenceLevel != "single_run" {
		t.Fatalf("beta interval = %#v", group)
	} else if group.ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(group.ReadinessReasons, []string{"OUTDATED_DATASET"}) {
		t.Fatalf("beta readiness = %#v", group)
	}
	if len(data.Trend) != 2 || data.Trend[0].RunID != secondAlpha.ID || data.Trend[1].RunID != beta.ID {
		t.Fatalf("trend = %#v", data.Trend)
	}

	global := performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?limit=10", nil, nil)
	if global.Code != http.StatusOK {
		t.Fatalf("global evaluation summary = %d: %s", global.Code, global.Body.String())
	}
	if err := json.Unmarshal(global.Body.Bytes(), &envelope); err != nil || envelope.Data.StatusCounts.Total != 6 || len(envelope.Data.Groups) != 3 || len(envelope.Data.Categories) != 12 || len(envelope.Data.FailureCodes) != 8 {
		t.Fatalf("global summary = %#v err=%v", envelope.Data, err)
	}
}

func TestAIEvaluationSummaryRejectsInvalidOrMissingProviderAndLimit(t *testing.T) {
	router, _, _ := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id=bad", nil, nil), http.StatusBadRequest, "INVALID_AI_PROVIDER_ID")
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id="+uuid.NewString(), nil, nil), http.StatusNotFound, "AI_PROVIDER_NOT_FOUND")
	assertAPIError(t, performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?limit=0", nil, nil), http.StatusBadRequest, "INVALID_PAGINATION")
}

func TestAIEvaluationSummaryKeepsDatasetVersionsSeparate(t *testing.T) {
	router, store, provider := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	seedEvaluationSummaryRun(t, store, provider, provider.Model, "succeeded", "2026-09-09T09:00:01Z", 4, 0, 0)
	seedEvaluationSummaryVersionedRun(t, store, provider, 2, 12, aieval.SuiteFull, "2026-09-09T09:00:02Z")
	seedEvaluationSummaryVersionedRun(t, store, provider, 4, 24, aieval.SuiteFull, "2026-09-09T09:00:03Z")

	response := performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id="+provider.ID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("evaluation summary = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data aiEvaluationSummaryResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode evaluation summary: %v", err)
	}
	if len(envelope.Data.Groups) != 3 || len(envelope.Data.Categories) != 12 || len(envelope.Data.Trend) != 3 || len(envelope.Data.FailureCodes) != 0 {
		t.Fatalf("versioned summary shape = %#v", envelope.Data)
	}
	groups := map[int]aiEvaluationQualityGroup{}
	for _, group := range envelope.Data.Groups {
		groups[group.DatasetVersion] = group
	}
	if groups[1].TotalCases != 4 || groups[1].PassedCases != 4 || groups[2].TotalCases != 12 || groups[2].PassedCases != 12 ||
		groups[4].TotalCases != 24 || groups[4].PassedCases != 24 {
		t.Fatalf("versioned summary groups = %#v", groups)
	}
	if groups[1].WilsonLowerBPS != 5101 || groups[2].WilsonLowerBPS != 7575 || groups[4].WilsonLowerBPS != 8620 {
		t.Fatalf("versioned summary intervals = %#v", groups)
	}
	if groups[1].ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(groups[1].ReadinessReasons, []string{"OUTDATED_DATASET"}) ||
		groups[2].ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(groups[2].ReadinessReasons, []string{"OUTDATED_DATASET"}) ||
		groups[4].ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(groups[4].ReadinessReasons, []string{"RUN_COUNT_LOW"}) {
		t.Fatalf("versioned summary readiness = %#v", groups)
	}
}

func TestAIEvaluationSummaryKeepsDiagnosticAndFullSuitesSeparate(t *testing.T) {
	router, store, provider := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	seedEvaluationSummaryVersionedRun(t, store, provider, 4, 8, aieval.SuiteSmoke, "2026-09-09T09:00:01Z")
	seedEvaluationSummaryVersionedRun(t, store, provider, 4, 6, aieval.SuiteGrounded, "2026-09-09T09:00:02Z")
	seedEvaluationSummaryVersionedRun(t, store, provider, 4, 24, aieval.SuiteFull, "2026-09-09T09:00:03Z")

	response := performRequest(router.Engine, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id="+provider.ID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("suite summary = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data aiEvaluationSummaryResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Groups) != 3 || len(envelope.Data.Categories) != 9 || len(envelope.Data.Trend) != 3 {
		t.Fatalf("suite summary shape = %#v", envelope.Data)
	}
	groups := map[string]aiEvaluationQualityGroup{}
	for _, group := range envelope.Data.Groups {
		groups[group.SuiteKey] = group
	}
	if groups[aieval.SuiteSmoke].TotalCases != 8 || groups[aieval.SuiteSmoke].ReadinessStatus != "insufficient_evidence" ||
		!reflect.DeepEqual(groups[aieval.SuiteSmoke].ReadinessReasons, []string{"SUITE_NOT_ELIGIBLE"}) {
		t.Fatalf("smoke group = %#v", groups[aieval.SuiteSmoke])
	}
	if groups[aieval.SuiteFull].TotalCases != 24 || groups[aieval.SuiteFull].ReadinessStatus != "insufficient_evidence" ||
		!reflect.DeepEqual(groups[aieval.SuiteFull].ReadinessReasons, []string{"RUN_COUNT_LOW"}) {
		t.Fatalf("full group = %#v", groups[aieval.SuiteFull])
	}
	if groups[aieval.SuiteGrounded].TotalCases != 6 || groups[aieval.SuiteGrounded].ReadinessStatus != "insufficient_evidence" ||
		!reflect.DeepEqual(groups[aieval.SuiteGrounded].ReadinessReasons, []string{"SUITE_NOT_ELIGIBLE"}) {
		t.Fatalf("topic group = %#v", groups[aieval.SuiteGrounded])
	}
	if envelope.Data.Trend[0].SuiteKey != aieval.SuiteSmoke || envelope.Data.Trend[1].SuiteKey != aieval.SuiteGrounded ||
		envelope.Data.Trend[2].SuiteKey != aieval.SuiteFull {
		t.Fatalf("suite trend = %#v", envelope.Data.Trend)
	}
}

func TestAIEvaluationWilsonIntervalAndEvidenceBoundaries(t *testing.T) {
	for _, test := range []struct {
		passed, total      int64
		rate, lower, upper int
	}{
		{passed: 0, total: 1, rate: 0, lower: 0, upper: 7935},
		{passed: 1, total: 1, rate: 10000, lower: 2065, upper: 10000},
		{passed: 36, total: 36, rate: 10000, lower: 9036, upper: 10000},
	} {
		rate, lower, upper, err := aiEvaluationWilsonIntervalBPS(test.passed, test.total)
		if err != nil || rate != test.rate || lower != test.lower || upper != test.upper {
			t.Fatalf("interval %d/%d = %d/%d/%d err=%v", test.passed, test.total, rate, lower, upper, err)
		}
	}
	for _, invalid := range [][2]int64{{0, 0}, {-1, 1}, {2, 1}} {
		if _, _, _, err := aiEvaluationWilsonIntervalBPS(invalid[0], invalid[1]); err == nil {
			t.Fatalf("invalid interval counts accepted: %v", invalid)
		}
	}
	if aiEvaluationEvidenceLevel(1) != "single_run" || aiEvaluationEvidenceLevel(2) != "limited_runs" ||
		aiEvaluationEvidenceLevel(3) != "repeated_runs" || aiEvaluationEvidenceLevel(30) != "repeated_runs" {
		t.Fatal("evidence-level thresholds changed")
	}
}

func TestAIEvaluationAdvisoryReadinessReasonsAndCandidate(t *testing.T) {
	policy := aiEvaluationReadinessPolicy{
		Mode: "advisory", CurrentDatasetVersion: 2, RequiredSuite: aieval.SuiteFull, MinimumCompletedRuns: 3,
		MinimumOverallLowerBPS: 8000, MinimumCategoryLowerBPS: 6000,
		RequiredCategories:   append([]string(nil), aiEvaluationRequiredCategories...),
		CriticalFailureCodes: append([]string(nil), aiEvaluationCriticalFailureCodes...),
	}
	groups := []aiEvaluationQualityGroup{
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "old", DatasetVersion: 1, SuiteKey: aieval.SuiteFull, RunCount: 10, ProviderVersionMin: 1, ProviderVersionMax: 1},
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "smoke", DatasetVersion: 2, SuiteKey: aieval.SuiteSmoke, RunCount: 3, ProviderVersionMin: 1, ProviderVersionMax: 1},
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "small", DatasetVersion: 2, SuiteKey: aieval.SuiteFull, RunCount: 2, ProviderVersionMin: 1, ProviderVersionMax: 1},
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "mixed", DatasetVersion: 2, SuiteKey: aieval.SuiteFull, RunCount: 3, ProviderVersionMin: 1, ProviderVersionMax: 2},
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "weak", DatasetVersion: 2, SuiteKey: aieval.SuiteFull, RunCount: 3, ProviderVersionMin: 1, ProviderVersionMax: 1, WilsonLowerBPS: 7817},
		{ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "candidate", DatasetVersion: 2, SuiteKey: aieval.SuiteFull, RunCount: 3, ProviderVersionMin: 1, ProviderVersionMax: 1, WilsonLowerBPS: 9036},
	}
	categories := make([]aiEvaluationCategoryGroup, 0, 8)
	for _, model := range []string{"weak", "candidate"} {
		for _, category := range aiEvaluationRequiredCategories {
			lower := 7009
			if model == "weak" && category == "no_evidence" {
				lower = 5650
			}
			categories = append(categories, aiEvaluationCategoryGroup{
				ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: model,
				DatasetVersion: 2, SuiteKey: aieval.SuiteFull, Category: category, WilsonLowerBPS: lower,
			})
		}
	}
	failures := []aiEvaluationFailureGroup{{
		ProviderID: "provider", ProviderNameSnapshot: "local", ProviderModelSnapshot: "weak",
		DatasetVersion: 2, SuiteKey: aieval.SuiteFull, FailureCode: "CONTROL_BLOCK_LEAKED", AffectedCases: 1, Occurrences: 1,
	}}
	if err := applyAIEvaluationReadiness(groups, categories, failures, policy); err != nil {
		t.Fatalf("apply readiness: %v", err)
	}
	wantStatuses := []string{"insufficient_evidence", "insufficient_evidence", "insufficient_evidence", "insufficient_evidence", "needs_attention", "review_candidate"}
	wantReasons := [][]string{
		{"OUTDATED_DATASET"}, {"SUITE_NOT_ELIGIBLE"}, {"RUN_COUNT_LOW"}, {"PROVIDER_VERSION_MIXED"},
		{"OVERALL_LOWER_BOUND_LOW", "CATEGORY_LOWER_BOUND_LOW", "CRITICAL_FAILURE_PRESENT"}, {},
	}
	for index := range groups {
		if groups[index].ReadinessStatus != wantStatuses[index] || !reflect.DeepEqual(groups[index].ReadinessReasons, wantReasons[index]) {
			t.Fatalf("readiness group %d = %#v", index, groups[index])
		}
	}
	if err := applyAIEvaluationReadiness(groups[5:], categories[:3], nil, policy); err == nil {
		t.Fatal("readiness accepted a current group with a missing required category")
	}
}
