package api

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDContextKey = "request_id"

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = uuid.NewString()
		}
		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func requestIDFromContext(c *gin.Context) string {
	if value, ok := c.Get(requestIDContextKey); ok {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}

func originMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			browserRequest := c.GetHeader("Sec-Fetch-Mode") != "" || c.GetHeader("Sec-Fetch-Site") != ""
			if browserRequest || c.Request.Method == http.MethodOptions {
				writeError(c, http.StatusForbidden, "ORIGIN_REQUIRED", "Browser requests must include an allowed Origin")
				return
			}
			// Native Tauri health probes do not carry browser CORS headers.
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			writeError(c, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "The request Origin is not allowed")
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-Request-ID")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "ETag, Idempotency-Replayed, X-Request-ID")
		c.Header("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func authMiddleware(sessionToken string, devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if devMode && sessionToken == "" {
			c.Next()
			return
		}
		authorization := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authorization, prefix) {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer session token is required")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		if len(provided) != len(sessionToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(sessionToken)) != 1 {
			writeError(c, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer session token is required")
			return
		}
		c.Next()
	}
}

func accessLogMiddleware(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Printf(
			"request_id=%s method=%s path=%s status=%d duration_ms=%d",
			requestIDFromContext(c),
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			time.Since(started).Milliseconds(),
		)
	}
}

func recoveryMiddleware(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Printf("request_id=%s panic=%v stack=%s", requestIDFromContext(c), recovered, debug.Stack())
				writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed")
			}
		}()
		c.Next()
	}
}

func middlewareConfigurationError(sessionToken string, devMode bool, origins []string) error {
	if sessionToken == "" && !devMode {
		return fmt.Errorf("session token is required outside development mode")
	}
	if len(origins) == 0 {
		return fmt.Errorf("at least one allowed origin is required")
	}
	return nil
}
