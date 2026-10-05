package api

import (
	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/api/apikit"
)

// The shared request and response helpers live in apikit, where packages split
// out of this one can reach them without importing api. The declarations below
// keep the short names that the handlers in this package already use.

const (
	maxJSONBodyBytes = apikit.MaxJSONBodyBytes
	apiContextKey    = apikit.ContextKey
)

type (
	errorResponse = apikit.ErrorResponse
	pageMeta      = apikit.PageMeta
)

func decodeJSON(c *gin.Context, destination any) error {
	return apikit.DecodeJSON(c, destination)
}

func writeError(c *gin.Context, status int, code, message string) {
	apikit.WriteError(c, status, code, message)
}

func writeDatabaseError(c *gin.Context) {
	apikit.WriteDatabaseError(c)
}

func requestIDFromContext(c *gin.Context) string {
	return apikit.RequestIDFromContext(c)
}

func queryInt(c *gin.Context, key string, fallback, minimum, maximum int) (int, bool) {
	return apikit.QueryInt(c, key, fallback, minimum, maximum)
}
