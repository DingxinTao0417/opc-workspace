package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type aiUsageSummaryEnvelope struct {
	Data aiUsageSummaryResponse `json:"data"`
}

func intPointer(value int) *int       { return &value }
func int64Pointer(value int64) *int64 { return &value }

func seedAIUsageGeneration(
	t *testing.T,
	store *database.Store,
	sessionID, providerID, status string,
	inputBytes, outputBytes int,
	durationMS *int64,
	inputTokens, outputTokens *int,
) {
	seedAIUsageGenerationAt(
		t, store, sessionID, providerID, status, "2026-09-08T12:00:00Z",
		inputBytes, outputBytes, durationMS, inputTokens, outputTokens,
	)
}

func seedAIUsageGenerationAt(
	t *testing.T,
	store *database.Store,
	sessionID, providerID, status, now string,
	inputBytes, outputBytes int,
	durationMS *int64,
	inputTokens, outputTokens *int,
) {
	t.Helper()
	generationID := uuid.NewString()
	var generationError *string
	stepStatus := "succeeded"
	var completedAt *string
	var stepError *string
	if status == "queued" || status == "streaming" {
		stepStatus = "running"
		durationMS = nil
	} else {
		completedAt = &now
	}
	if status == "failed" {
		code := "AI_PROVIDER_UNAVAILABLE"
		generationError, stepError = &code, &code
		stepStatus = "failed"
	}
	if status == "cancelled" {
		stepStatus = "cancelled"
	}
	if err := store.DB.Create(&models.AIGeneration{
		ID: generationID, SessionID: sessionID, ProviderID: providerID, Status: status,
		ErrorCode: generationError, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create generation: %v", err)
	}
	var tokenSource *string
	if inputTokens != nil && outputTokens != nil {
		tokenSource = stringPointer("provider")
	}
	if err := store.DB.Create(&models.AIRunStep{
		ID: uuid.NewString(), GenerationID: generationID, Sequence: 1,
		Kind: "generation", Status: stepStatus, StartedAt: now, CompletedAt: completedAt,
		DurationMS: durationMS, InputBytes: inputBytes, OutputBytes: outputBytes,
		InputTokens: inputTokens, OutputTokens: outputTokens, TokenSource: tokenSource,
		ErrorCode: stepError, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create generation root step: %v", err)
	}
}

func decodeAIUsageSummary(t *testing.T, responseBody []byte) aiUsageSummaryResponse {
	t.Helper()
	var envelope aiUsageSummaryEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		t.Fatalf("decode AI usage summary: %v\n%s", err, responseBody)
	}
	return envelope.Data
}

func TestAIUsageSummaryAggregatesTerminalProviderUsageWithoutEstimation(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	providerA := createTestAIProvider(t, router, "local-a", "openai_chat", "http://127.0.0.1:11434/v1", "model-a", nil)
	providerB := createTestAIProvider(t, router, "local-b", "openai_chat", "http://127.0.0.1:1234/v1", "model-b", nil)
	now := "2026-09-08T12:00:00Z"
	sessionA := models.AISession{ID: uuid.NewString(), Title: "usage-a", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	sessionB := models.AISession{ID: uuid.NewString(), Title: "usage-b", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	emptySession := models.AISession{ID: uuid.NewString(), Title: "usage-empty", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.DB.Create([]models.AISession{sessionA, sessionB, emptySession}).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}

	seedAIUsageGeneration(t, store, sessionA.ID, providerA.ID, "completed", 100, 50, int64Pointer(10), intPointer(11), intPointer(7))
	seedAIUsageGeneration(t, store, sessionA.ID, providerA.ID, "failed", 200, 20, int64Pointer(20), nil, nil)
	seedAIUsageGeneration(t, store, sessionA.ID, providerB.ID, "cancelled", 300, 30, int64Pointer(30), nil, nil)
	seedAIUsageGeneration(t, store, sessionA.ID, providerB.ID, "streaming", 40, 0, nil, nil, nil)
	seedAIUsageGeneration(t, store, sessionB.ID, providerB.ID, "completed", 500, 60, int64Pointer(40), intPointer(22), intPointer(9))

	response := performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?session_id="+sessionA.ID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("session usage summary = %d: %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); strings.Contains(body, "base_url") || strings.Contains(body, "api_key") || strings.Contains(body, "content") || strings.Contains(body, "reasoning") || strings.Contains(body, "prompt") {
		t.Fatalf("usage summary leaked forbidden fields: %s", body)
	}
	summary := decodeAIUsageSummary(t, response.Body.Bytes())
	if summary.Scope.SessionID == nil || *summary.Scope.SessionID != sessionA.ID || summary.Scope.ProviderID != nil {
		t.Fatalf("session scope = %#v", summary.Scope)
	}
	want := AIUsageTotals{
		TotalGenerations: 4, CompletedGenerations: 1, FailedGenerations: 1,
		CancelledGenerations: 1, ActiveGenerations: 1,
		ProviderUsageGenerations: 1, UnknownUsageGenerations: 2,
		InputTokens: 11, OutputTokens: 7, InputBytes: 640, OutputBytes: 100, DurationMS: 60,
	}
	if summary.Totals != want {
		t.Fatalf("session totals = %#v, want %#v", summary.Totals, want)
	}
	if len(summary.Providers) != 2 || summary.Providers[0].ProviderName != "local-a" || summary.Providers[1].ProviderName != "local-b" {
		t.Fatalf("provider summaries = %#v", summary.Providers)
	}
	if summary.Providers[0].TotalGenerations != 2 || summary.Providers[0].ProviderUsageGenerations != 1 || summary.Providers[0].UnknownUsageGenerations != 1 || summary.Providers[0].InputTokens != 11 {
		t.Fatalf("provider A summary = %#v", summary.Providers[0])
	}
	if summary.Providers[1].TotalGenerations != 2 || summary.Providers[1].ProviderUsageGenerations != 0 || summary.Providers[1].UnknownUsageGenerations != 1 || summary.Providers[1].ActiveGenerations != 1 {
		t.Fatalf("provider B summary = %#v", summary.Providers[1])
	}

	providerResponse := performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?provider_id="+providerB.ID, nil, nil)
	if providerResponse.Code != http.StatusOK {
		t.Fatalf("provider usage summary = %d: %s", providerResponse.Code, providerResponse.Body.String())
	}
	providerSummary := decodeAIUsageSummary(t, providerResponse.Body.Bytes())
	if providerSummary.Totals.TotalGenerations != 3 || providerSummary.Totals.ProviderUsageGenerations != 1 || providerSummary.Totals.UnknownUsageGenerations != 1 || providerSummary.Totals.InputTokens != 22 || providerSummary.Totals.OutputTokens != 9 {
		t.Fatalf("provider-filtered totals = %#v", providerSummary.Totals)
	}

	emptyResponse := performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?session_id="+emptySession.ID, nil, nil)
	if emptyResponse.Code != http.StatusOK {
		t.Fatalf("empty usage summary = %d: %s", emptyResponse.Code, emptyResponse.Body.String())
	}
	emptySummary := decodeAIUsageSummary(t, emptyResponse.Body.Bytes())
	if emptySummary.Totals != (AIUsageTotals{}) || len(emptySummary.Providers) != 0 {
		t.Fatalf("empty usage summary = %#v", emptySummary)
	}
}

func TestAIUsageSummaryRejectsInvalidAndMissingFilters(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?session_id=bad", nil, nil), http.StatusBadRequest, "INVALID_AI_SESSION_ID")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?provider_id=bad", nil, nil), http.StatusBadRequest, "INVALID_AI_PROVIDER_ID")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?session_id="+uuid.NewString(), nil, nil), http.StatusNotFound, "AI_SESSION_NOT_FOUND")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?provider_id="+uuid.NewString(), nil, nil), http.StatusNotFound, "AI_PROVIDER_NOT_FOUND")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?trend_days=0", nil, nil), http.StatusBadRequest, "INVALID_PAGINATION")
	assertAPIError(t, performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?trend_days=31", nil, nil), http.StatusBadRequest, "INVALID_PAGINATION")
}

func TestAIUsageSummaryReturnsUTCWindowWithTerminalUsageOnly(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	provider := createTestAIProvider(t, router, "trend-local", "openai_chat", "http://127.0.0.1:11434/v1", "trend-model", nil)
	session := models.AISession{ID: uuid.NewString(), Title: "usage-trend", Persist: true, Version: 1, CreatedAt: "2026-09-02T12:00:00Z", UpdatedAt: "2026-09-08T12:00:00Z"}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("create trend session: %v", err)
	}
	seedAIUsageGenerationAt(t, store, session.ID, provider.ID, "completed", "2026-09-02T04:00:00Z", 100, 10, int64Pointer(20), intPointer(11), intPointer(7))
	seedAIUsageGenerationAt(t, store, session.ID, provider.ID, "failed", "2026-09-07T16:00:00Z", 200, 20, int64Pointer(30), nil, nil)
	seedAIUsageGenerationAt(t, store, session.ID, provider.ID, "cancelled", "2026-09-08T09:00:00Z", 300, 30, int64Pointer(40), intPointer(22), intPointer(9))
	seedAIUsageGenerationAt(t, store, session.ID, provider.ID, "streaming", "2026-09-08T11:00:00Z", 400, 0, nil, nil, nil)

	response := performRequest(router, http.MethodGet, "/api/v1/ai/usage-summary?session_id="+session.ID+"&trend_days=7", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("usage trend = %d: %s", response.Code, response.Body.String())
	}
	summary := decodeAIUsageSummary(t, response.Body.Bytes())
	if summary.TrendDays != 7 || len(summary.Trend) != 7 {
		t.Fatalf("trend shape days=%d points=%#v", summary.TrendDays, summary.Trend)
	}
	wantDays := []string{"2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05", "2026-09-06", "2026-09-07", "2026-09-08"}
	for index, day := range wantDays {
		if summary.Trend[index].Day != day {
			t.Fatalf("trend day %d=%q, want %q", index, summary.Trend[index].Day, day)
		}
	}
	if first := summary.Trend[0]; first.TotalGenerations != 1 || first.ProviderUsageGenerations != 1 || first.InputTokens != 11 || first.DurationMS != 20 {
		t.Fatalf("first trend point=%#v", first)
	}
	if zero := summary.Trend[1]; zero != (aiUsageTrendPoint{Day: "2026-09-03"}) {
		t.Fatalf("zero-filled trend point=%#v", zero)
	}
	if failed := summary.Trend[5]; failed.FailedGenerations != 1 || failed.UnknownUsageGenerations != 1 || failed.InputTokens != 0 || failed.DurationMS != 30 {
		t.Fatalf("failed trend point=%#v", failed)
	}
	if cancelled := summary.Trend[6]; cancelled.CancelledGenerations != 1 || cancelled.ProviderUsageGenerations != 1 || cancelled.OutputTokens != 9 || cancelled.DurationMS != 40 {
		t.Fatalf("cancelled trend point=%#v", cancelled)
	}
}
