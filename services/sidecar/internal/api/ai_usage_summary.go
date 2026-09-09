package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var (
	errAIUsageSessionNotFound  = errors.New("AI usage session not found")
	errAIUsageProviderNotFound = errors.New("AI usage provider not found")
)

type aiUsageScope struct {
	SessionID  *string `json:"session_id"`
	ProviderID *string `json:"provider_id"`
}

type AIUsageTotals struct {
	TotalGenerations         int64 `gorm:"column:total_generations" json:"total_generations"`
	CompletedGenerations     int64 `gorm:"column:completed_generations" json:"completed_generations"`
	FailedGenerations        int64 `gorm:"column:failed_generations" json:"failed_generations"`
	CancelledGenerations     int64 `gorm:"column:cancelled_generations" json:"cancelled_generations"`
	ActiveGenerations        int64 `gorm:"column:active_generations" json:"active_generations"`
	ProviderUsageGenerations int64 `gorm:"column:provider_usage_generations" json:"provider_usage_generations"`
	UnknownUsageGenerations  int64 `gorm:"column:unknown_usage_generations" json:"unknown_usage_generations"`
	InputTokens              int64 `gorm:"column:input_tokens" json:"input_tokens"`
	OutputTokens             int64 `gorm:"column:output_tokens" json:"output_tokens"`
	InputBytes               int64 `gorm:"column:input_bytes" json:"input_bytes"`
	OutputBytes              int64 `gorm:"column:output_bytes" json:"output_bytes"`
	DurationMS               int64 `gorm:"column:duration_ms" json:"duration_ms"`
}

type aiProviderUsageSummary struct {
	ProviderID       string `gorm:"column:provider_id" json:"provider_id"`
	ProviderName     string `gorm:"column:provider_name" json:"provider_name"`
	ProviderKind     string `gorm:"column:provider_kind" json:"provider_kind"`
	ProviderProtocol string `gorm:"column:provider_protocol" json:"provider_protocol"`
	Model            string `gorm:"column:model" json:"model"`
	AIUsageTotals
}

type aiUsageSummaryResponse struct {
	Scope     aiUsageScope             `json:"scope"`
	Totals    AIUsageTotals            `json:"totals"`
	Providers []aiProviderUsageSummary `json:"providers"`
	TrendDays int                      `json:"trend_days"`
	Trend     []aiUsageTrendPoint      `json:"trend"`
}

type aiUsageTrendPoint struct {
	Day string `gorm:"column:day" json:"day"`
	AIUsageTotals
}

const aiUsageTotalsSelect = `
	COUNT(*) AS total_generations,
	COALESCE(SUM(CASE WHEN g.status = 'completed' THEN 1 ELSE 0 END), 0) AS completed_generations,
	COALESCE(SUM(CASE WHEN g.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_generations,
	COALESCE(SUM(CASE WHEN g.status = 'cancelled' THEN 1 ELSE 0 END), 0) AS cancelled_generations,
	COALESCE(SUM(CASE WHEN g.status IN ('queued', 'streaming') THEN 1 ELSE 0 END), 0) AS active_generations,
	COALESCE(SUM(CASE WHEN g.status IN ('completed', 'failed', 'cancelled') AND root.token_source = 'provider' THEN 1 ELSE 0 END), 0) AS provider_usage_generations,
	COALESCE(SUM(CASE WHEN g.status IN ('completed', 'failed', 'cancelled') AND root.token_source IS NULL THEN 1 ELSE 0 END), 0) AS unknown_usage_generations,
	COALESCE(SUM(CASE WHEN root.token_source = 'provider' THEN root.input_tokens ELSE 0 END), 0) AS input_tokens,
	COALESCE(SUM(CASE WHEN root.token_source = 'provider' THEN root.output_tokens ELSE 0 END), 0) AS output_tokens,
	COALESCE(SUM(root.input_bytes), 0) AS input_bytes,
	COALESCE(SUM(root.output_bytes), 0) AS output_bytes,
	COALESCE(SUM(COALESCE(root.duration_ms, 0)), 0) AS duration_ms`

func canonicalAIUsageFilter(value string) (*string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return nil, false
	}
	return &value, true
}

func (a *API) getAIUsageSummary(c *gin.Context) {
	trendDays, ok := queryInt(c, "trend_days", 7, 1, 30)
	if !ok {
		return
	}
	sessionID, valid := canonicalAIUsageFilter(c.Query("session_id"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_AI_SESSION_ID", "AI session id must be a canonical UUID")
		return
	}
	providerID, valid := canonicalAIUsageFilter(c.Query("provider_id"))
	if !valid {
		writeError(c, http.StatusBadRequest, "INVALID_AI_PROVIDER_ID", "AI provider id must be a canonical UUID")
		return
	}

	response := aiUsageSummaryResponse{
		Scope:     aiUsageScope{SessionID: sessionID, ProviderID: providerID},
		Providers: make([]aiProviderUsageSummary, 0),
		TrendDays: trendDays,
		Trend:     make([]aiUsageTrendPoint, 0, trendDays),
	}
	endDay := a.options.Now().UTC().Truncate(24 * time.Hour)
	startDay := endDay.AddDate(0, 0, -(trendDays - 1))
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if sessionID != nil {
			var session models.AISession
			if err := tx.Select("id").First(&session, "id = ?", *sessionID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errAIUsageSessionNotFound
				}
				return err
			}
		}
		if providerID != nil {
			var provider models.AIProvider
			if err := tx.Select("id").First(&provider, "id = ?", *providerID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errAIUsageProviderNotFound
				}
				return err
			}
		}

		where, args := "1 = 1", make([]any, 0, 2)
		if sessionID != nil {
			where += " AND g.session_id = ?"
			args = append(args, *sessionID)
		}
		if providerID != nil {
			where += " AND g.provider_id = ?"
			args = append(args, *providerID)
		}
		base := ` FROM ai_generations g
			JOIN ai_run_steps root ON root.generation_id = g.id AND root.sequence = 1 AND root.kind = 'generation'
			JOIN ai_providers p ON p.id = g.provider_id
			WHERE ` + where
		if err := tx.Raw("SELECT "+aiUsageTotalsSelect+base, args...).Scan(&response.Totals).Error; err != nil {
			return err
		}
		providerQuery := `SELECT
			p.id AS provider_id, p.name AS provider_name, p.kind AS provider_kind,
			p.protocol AS provider_protocol, p.model AS model, ` + aiUsageTotalsSelect + base + `
			GROUP BY p.id, p.name, p.kind, p.protocol, p.model
			ORDER BY lower(p.name) ASC, p.id ASC`
		if err := tx.Raw(providerQuery, args...).Scan(&response.Providers).Error; err != nil {
			return err
		}
		trendWhere := where + ` AND g.status IN ('completed', 'failed', 'cancelled') AND root.completed_at >= ?`
		trendArgs := append(append([]any(nil), args...), startDay.Format(time.RFC3339Nano))
		trendQuery := `SELECT
			substr(root.completed_at, 1, 10) AS day, ` + aiUsageTotalsSelect + ` FROM ai_generations g
			JOIN ai_run_steps root ON root.generation_id = g.id AND root.sequence = 1 AND root.kind = 'generation'
			JOIN ai_providers p ON p.id = g.provider_id
			WHERE ` + trendWhere + `
			GROUP BY substr(root.completed_at, 1, 10)
			ORDER BY day ASC`
		rows := make([]aiUsageTrendPoint, 0, trendDays)
		if err := tx.Raw(trendQuery, trendArgs...).Scan(&rows).Error; err != nil {
			return err
		}
		byDay := make(map[string]aiUsageTrendPoint, len(rows))
		for _, row := range rows {
			byDay[row.Day] = row
		}
		for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			if row, exists := byDay[key]; exists {
				response.Trend = append(response.Trend, row)
			} else {
				response.Trend = append(response.Trend, aiUsageTrendPoint{Day: key})
			}
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})

	switch {
	case errors.Is(err, errAIUsageSessionNotFound):
		writeError(c, http.StatusNotFound, "AI_SESSION_NOT_FOUND", "AI session not found")
	case errors.Is(err, errAIUsageProviderNotFound):
		writeError(c, http.StatusNotFound, "AI_PROVIDER_NOT_FOUND", "AI provider not found")
	case err != nil:
		writeDatabaseError(c)
	default:
		c.JSON(http.StatusOK, gin.H{"data": response})
	}
}
