package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLogExcludesQueryHeadersAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestIDMiddleware(), accessLogMiddleware(log.New(&output, "", 0)))
	router.POST("/api/v1/tasks/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/task-1?token=query-canary", strings.NewReader("body-canary"))
	request.Header.Set("Authorization", "Bearer header-canary")
	request.Header.Set("X-Request-ID", "F68D4226-4D20-4C2A-99E6-B0397987BF89")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	logged := output.String()
	for _, secret := range []string{"query-canary", "body-canary", "header-canary"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("access log leaked %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "method=POST path=/api/v1/tasks/:id status=204") {
		t.Fatalf("access log does not contain the route template and status: %s", logged)
	}
	const canonicalRequestID = "f68d4226-4d20-4c2a-99e6-b0397987bf89"
	if recorder.Header().Get("X-Request-ID") != canonicalRequestID ||
		!strings.Contains(logged, "request_id="+canonicalRequestID) {
		t.Fatalf("request ID was not canonically correlated: header=%q log=%s", recorder.Header().Get("X-Request-ID"), logged)
	}
}

func TestRequestIDMiddlewareReplacesInvalidClientValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestIDMiddleware(), accessLogMiddleware(log.New(&output, "", 0)))
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("X-Request-ID", "client-controlled-value")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	requestID := recorder.Header().Get("X-Request-ID")
	if requestID == "client-controlled-value" || requestID == "" {
		t.Fatalf("invalid request ID was not replaced: %q", requestID)
	}
	if strings.Contains(output.String(), "client-controlled-value") || !strings.Contains(output.String(), "request_id="+requestID) {
		t.Fatalf("access log did not use only the generated request ID: %s", output.String())
	}
}

func TestErrorResponseUsesSameCanonicalRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware())
	router.GET("/failure", func(c *gin.Context) {
		writeError(c, http.StatusInternalServerError, "TEST_FAILURE", "The request failed")
	})

	request := httptest.NewRequest(http.MethodGet, "/failure", nil)
	request.Header.Set("X-Request-ID", "F68D4226-4D20-4C2A-99E6-B0397987BF89")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	var body errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	const requestID = "f68d4226-4d20-4c2a-99e6-b0397987bf89"
	if recorder.Header().Get("X-Request-ID") != requestID || body.RequestID != requestID {
		t.Fatalf("request ID mismatch: header=%q body=%q", recorder.Header().Get("X-Request-ID"), body.RequestID)
	}
}

func TestRecoveryLogExcludesRecoveredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	router := gin.New()
	router.Use(requestIDMiddleware(), recoveryMiddleware(log.New(&output, "", 0)))
	router.GET("/panic", func(*gin.Context) { panic("business-content-canary") })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if strings.Contains(output.String(), "business-content-canary") {
		t.Fatalf("recovery log leaked recovered value: %s", output.String())
	}
	if !strings.Contains(output.String(), "panic_recovered=true") {
		t.Fatalf("recovery log is missing the safe panic marker: %s", output.String())
	}
}
