package apikit

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

// RequestIDMiddleware gives every request a canonical UUID request ID, reusing
// a valid client-supplied X-Request-ID.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		parsed, err := uuid.Parse(requestID)
		if err != nil {
			requestID = uuid.NewString()
		} else {
			requestID = parsed.String()
		}
		c.Set(requestIDContextKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// RequestIDFromContext returns the request ID set by RequestIDMiddleware, or
// an empty string outside a request that passed through it.
func RequestIDFromContext(c *gin.Context) string {
	if value, ok := c.Get(requestIDContextKey); ok {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}

// OriginMiddleware admits requests from the exact allowed origins and answers
// CORS preflight requests.
func OriginMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			browserRequest := c.GetHeader("Sec-Fetch-Mode") != "" || c.GetHeader("Sec-Fetch-Site") != ""
			if browserRequest || c.Request.Method == http.MethodOptions {
				WriteError(c, http.StatusForbidden, "ORIGIN_REQUIRED", "Browser requests must include an allowed Origin")
				return
			}
			// Native Tauri health probes do not carry browser CORS headers.
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			WriteError(c, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "The request Origin is not allowed")
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-Request-ID, X-Knowledge-Confirmation")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "ETag, Idempotency-Replayed, X-Request-ID, Content-Disposition, X-Backup-Package-Format-Version, X-Backup-ID, X-Invoice-PDF-SHA256, X-Financial-CSV-SHA256")
		c.Header("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthMiddleware requires the Bearer session token. Development mode without
// a configured token admits every request.
func AuthMiddleware(sessionToken string, devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if devMode && sessionToken == "" {
			c.Next()
			return
		}
		authorization := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authorization, prefix) {
			WriteError(c, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer session token is required")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		if len(provided) != len(sessionToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(sessionToken)) != 1 {
			WriteError(c, http.StatusUnauthorized, "UNAUTHORIZED", "A valid Bearer session token is required")
			return
		}
		c.Next()
	}
}

// AccessLogMiddleware logs one content-free line per request: request ID,
// method, route pattern, status and duration.
func AccessLogMiddleware(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Printf(
			"request_id=%s method=%s path=%s status=%d duration_ms=%d",
			RequestIDFromContext(c),
			c.Request.Method,
			c.FullPath(),
			c.Writer.Status(),
			time.Since(started).Milliseconds(),
		)
	}
}

// RecoveryMiddleware turns a handler panic into the generic internal error and
// logs the stack without the recovered value.
func RecoveryMiddleware(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Printf("request_id=%s panic_recovered=true stack=%s", RequestIDFromContext(c), debug.Stack())
				WriteError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed")
			}
		}()
		c.Next()
	}
}

// MiddlewareConfigurationError reports a configuration the middleware chain
// cannot enforce safely.
func MiddlewareConfigurationError(sessionToken string, devMode bool, origins []string) error {
	if sessionToken == "" && !devMode {
		return fmt.Errorf("session token is required outside development mode")
	}
	if len(origins) == 0 {
		return fmt.Errorf("at least one allowed origin is required")
	}
	return nil
}
