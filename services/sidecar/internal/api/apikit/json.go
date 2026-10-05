// Package apikit holds the request and response helpers shared by every HTTP
// handler: strict JSON decoding, the error envelope, pagination parameters and
// the middleware chain. It depends on nothing else in the Sidecar's API layer,
// so packages split out of internal/api can use it without importing api.
package apikit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxJSONBodyBytes bounds every JSON request body.
const MaxJSONBodyBytes = 1 << 20

// DecodeJSON decodes exactly one JSON value into destination and rejects
// unknown fields, trailing data and bodies above MaxJSONBodyBytes.
func DecodeJSON(c *gin.Context, destination any) error {
	if c.Request.Body == nil {
		return errors.New("request body is required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, MaxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}
