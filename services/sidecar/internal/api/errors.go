package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const apiContextKey = "opc-api"

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorResponse{
		Code:      code,
		Message:   message,
		RequestID: requestIDFromContext(c),
	})
}

func writeDatabaseError(c *gin.Context) {
	if value, ok := c.Get(apiContextKey); ok {
		if service, valid := value.(*API); valid {
			service.recordRuntimeDatabaseFailure(requestIDFromContext(c))
		}
	}
	writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed")
}
