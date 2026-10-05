package apikit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// PageMeta is the pagination block of a list response.
type PageMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// QueryInt reads a bounded integer query parameter. A missing parameter yields
// fallback; an invalid or out-of-range one writes INVALID_PAGINATION and
// returns false.
func QueryInt(c *gin.Context, key string, fallback, minimum, maximum int) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		WriteError(c, http.StatusBadRequest, "INVALID_PAGINATION", fmt.Sprintf("%s must be between %d and %d", key, minimum, maximum))
		return 0, false
	}
	return value, true
}
