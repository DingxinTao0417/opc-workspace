package api

import (
	"bytes"
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
