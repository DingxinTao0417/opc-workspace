package apikit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ContextKey is the gin context key under which the router stores the service
// that owns the request.
const ContextKey = "opc-api"

// ErrorResponse is the error envelope of every API response.
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// DatabaseFailureRecorder is implemented by the service stored under
// ContextKey. WriteDatabaseError reports the failing request to it before
// answering, so a runtime database incident is recorded exactly where the
// failure was observed.
type DatabaseFailureRecorder interface {
	RecordRuntimeDatabaseFailure(requestID string)
}

// WriteError aborts the request with the error envelope.
func WriteError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorResponse{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFromContext(c),
	})
}

// WriteDatabaseError records a runtime database failure, when the request has
// an owning service, and answers with the generic internal error.
func WriteDatabaseError(c *gin.Context) {
	if value, ok := c.Get(ContextKey); ok {
		if service, valid := value.(DatabaseFailureRecorder); valid {
			service.RecordRuntimeDatabaseFailure(RequestIDFromContext(c))
		}
	}
	WriteError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed")
}
