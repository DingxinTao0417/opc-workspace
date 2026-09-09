package api

import (
	"net/http"
	"testing"
	"time"
)

func TestAIRunStepsRouteRejectsInvalidAndMissingGenerations(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	invalid := performRequest(router, http.MethodGet, "/api/v1/ai/generations/not-a-uuid/steps", nil, nil)
	assertAPIError(t, invalid, http.StatusBadRequest, "INVALID_AI_GENERATION_ID")
	missing := performRequest(router, http.MethodGet, "/api/v1/ai/generations/018f0000-0000-7000-8000-000000006399/steps", nil, nil)
	assertAPIError(t, missing, http.StatusNotFound, "AI_GENERATION_NOT_FOUND")
}
