package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func evaluationReviewInputForGroup(group aiEvaluationQualityGroup, decision, reason string) createAIEvaluationReviewRequest {
	return createAIEvaluationReviewRequest{
		ProviderID: group.ProviderID, ProviderNameSnapshot: group.ProviderNameSnapshot,
		ProviderModelSnapshot: group.ProviderModelSnapshot, DatasetVersion: group.DatasetVersion,
		SuiteKey: group.SuiteKey, ExpectedProviderVersionMin: group.ProviderVersionMin,
		ExpectedProviderVersionMax: group.ProviderVersionMax, ExpectedLastCompletedAt: group.LastCompletedAt,
		ExpectedRunCount: group.RunCount, ExpectedTotalCases: group.TotalCases,
		ExpectedPassedCases: group.PassedCases, ExpectedFailedCases: group.FailedCases,
		ExpectedReadinessStatus:  group.ReadinessStatus,
		ExpectedReadinessReasons: append([]string(nil), group.ReadinessReasons...),
		Decision:                 decision, Reason: reason,
	}
}

func loadEvaluationReviewGroup(t *testing.T, router http.Handler, providerID, suiteKey string) aiEvaluationQualityGroup {
	t.Helper()
	response := performRequest(router, http.MethodGet, "/api/v1/ai/evaluation-summary?provider_id="+providerID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("evaluation summary = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data aiEvaluationSummaryResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode evaluation summary: %v", err)
	}
	for _, group := range envelope.Data.Groups {
		if group.SuiteKey == suiteKey {
			return group
		}
	}
	t.Fatalf("evaluation group for suite %q not found: %#v", suiteKey, envelope.Data.Groups)
	return aiEvaluationQualityGroup{}
}

func createEvaluationReviewRequest(
	t *testing.T,
	router http.Handler,
	input createAIEvaluationReviewRequest,
	key string,
) *responseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	return performRequest(router, http.MethodPost, "/api/v1/ai/evaluation-reviews", body, headers)
}

func TestAIEvaluationReviewRecordsExactSnapshotAndListsAuditHistory(t *testing.T) {
	router, store, provider := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	for index, completedAt := range []string{
		"2026-09-09T10:00:01Z", "2026-09-09T10:01:01Z", "2026-09-09T10:02:01Z",
	} {
		if index > 0 {
			provider.Version++
		}
		seedEvaluationSummaryVersionedRun(t, store, provider, 3, 24, aieval.SuiteFull, completedAt)
	}
	group := loadEvaluationReviewGroup(t, router.Engine, provider.ID, aieval.SuiteFull)
	if group.ReadinessStatus != "insufficient_evidence" || !reflect.DeepEqual(group.ReadinessReasons, []string{"PROVIDER_VERSION_MIXED"}) {
		t.Fatalf("mixed-version review group = %#v", group)
	}

	var providerBefore models.AIProvider
	if err := store.DB.First(&providerBefore, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	input := evaluationReviewInputForGroup(group, "needs_more_evidence", "Provider 版本混合，先补充同版本完整评测。")
	created := createEvaluationReviewRequest(t, router.Engine, input, "evaluation-review-create-1")
	if created.Code != http.StatusCreated {
		t.Fatalf("create evaluation review = %d: %s", created.Code, created.Body.String())
	}
	for _, forbidden := range []string{`"prompt":`, `"question":`, `"answer":`, `"content":`, `"base_url":`, `"api_key":`} {
		if strings.Contains(created.Body.String(), forbidden) {
			t.Fatalf("evaluation review leaked %s: %s", forbidden, created.Body.String())
		}
	}
	var createdEnvelope struct {
		Data aiEvaluationReviewResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created evaluation review: %v", err)
	}
	createdReview := createdEnvelope.Data
	if createdReview.Decision != input.Decision || createdReview.Reason != input.Reason ||
		createdReview.ProviderIDSnapshot != provider.ID || createdReview.ProviderVersionMin != 1 ||
		createdReview.ProviderVersionMax != 3 || createdReview.RunCount != 3 ||
		createdReview.TotalCases != 72 || createdReview.PassedCases != 72 || createdReview.FailedCases != 0 ||
		createdReview.ReadinessStatus != group.ReadinessStatus ||
		!reflect.DeepEqual(createdReview.ReadinessReasons, group.ReadinessReasons) ||
		createdReview.ReviewedByActorID != models.BuiltinOwnerActorID ||
		createdReview.ReviewedByActorNameSnapshot == "" || createdReview.MinimumCategory != "grounded" ||
		createdReview.MinimumCategoryWilsonLowerBPS != 8241 {
		t.Fatalf("created evaluation review = %#v", createdReview)
	}
	if createdReview.OverallWilsonLowerBPS != group.WilsonLowerBPS || len(createdReview.CriticalFailureCodes) != 0 {
		t.Fatalf("created evaluation evidence = %#v", createdReview)
	}

	replayed := createEvaluationReviewRequest(t, router.Engine, input, "evaluation-review-create-1")
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("replayed evaluation review = %d headers=%v body=%s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	changed := input
	changed.Reason = "另一条理由"
	conflict := createEvaluationReviewRequest(t, router.Engine, changed, "evaluation-review-create-1")
	assertAPIError(t, conflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT")

	listed := performRequest(router.Engine, http.MethodGet,
		"/api/v1/ai/evaluation-reviews?provider_id="+provider.ID+"&decision=needs_more_evidence&page=1&page_size=1", nil, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"total":1`) ||
		!strings.Contains(listed.Body.String(), createdReview.ID) || !strings.Contains(listed.Body.String(), input.Reason) {
		t.Fatalf("list evaluation reviews = %d: %s", listed.Code, listed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_evaluation_reviews", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	var providerAfter models.AIProvider
	if err := store.DB.First(&providerAfter, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	if providerAfter.Status != providerBefore.Status || providerAfter.Version != providerBefore.Version ||
		providerAfter.HealthStatus != providerBefore.HealthStatus || providerAfter.Model != providerBefore.Model {
		t.Fatalf("review changed Provider before=%#v after=%#v", providerBefore, providerAfter)
	}
}

func TestAIEvaluationReviewRejectsStaleOrInvalidEvidence(t *testing.T) {
	router, store, provider := newAIEvaluationTestRouter(t, &scriptedEvaluationClient{})
	seedEvaluationSummaryVersionedRun(t, store, provider, 3, 6, aieval.SuiteGrounded, "2026-09-09T11:00:01Z")
	group := loadEvaluationReviewGroup(t, router.Engine, provider.ID, aieval.SuiteGrounded)
	input := evaluationReviewInputForGroup(group, "needs_more_evidence", "有依据回答专题只用于诊断，继续运行完整套件。")

	invalid := input
	invalid.Decision = "approved"
	assertAPIError(t, createEvaluationReviewRequest(t, router.Engine, invalid, "evaluation-review-invalid-decision"),
		http.StatusUnprocessableEntity, "AI_EVALUATION_REVIEW_INVALID")
	invalid = input
	invalid.Reason = "  "
	assertAPIError(t, createEvaluationReviewRequest(t, router.Engine, invalid, "evaluation-review-invalid-reason"),
		http.StatusUnprocessableEntity, "AI_EVALUATION_REVIEW_INVALID")
	recorded := createEvaluationReviewRequest(t, router.Engine, input, "evaluation-review-topic")
	if recorded.Code != http.StatusCreated || !strings.Contains(recorded.Body.String(), `"suite_key":"grounded"`) ||
		!strings.Contains(recorded.Body.String(), `"decision":"needs_more_evidence"`) {
		t.Fatalf("record topic review = %d: %s", recorded.Code, recorded.Body.String())
	}
	seedEvaluationSummaryVersionedRun(t, store, provider, 3, 6, aieval.SuiteGrounded, "2026-09-09T11:01:01Z")
	stale := createEvaluationReviewRequest(t, router.Engine, input, "evaluation-review-stale")
	assertAPIError(t, stale, http.StatusConflict, "AI_EVALUATION_REVIEW_STALE")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_evaluation_reviews", 1)

	missing := input
	missing.ProviderModelSnapshot = "missing-model"
	missingResponse := createEvaluationReviewRequest(t, router.Engine, missing, "evaluation-review-missing")
	assertAPIError(t, missingResponse, http.StatusNotFound, "AI_EVALUATION_GROUP_NOT_FOUND")
	invalidList := performRequest(router.Engine, http.MethodGet,
		"/api/v1/ai/evaluation-reviews?decision=approved", nil, nil)
	assertAPIError(t, invalidList, http.StatusBadRequest, "AI_EVALUATION_REVIEW_DECISION_INVALID")
}

type responseRecorder = httptest.ResponseRecorder
