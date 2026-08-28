package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type taskStats struct {
	Total            int `json:"total" gorm:"column:total"`
	Completed        int `json:"completed" gorm:"column:completed"`
	Remaining        int `json:"remaining" gorm:"column:remaining"`
	Overdue          int `json:"overdue" gorm:"column:overdue"`
	DueSoon          int `json:"due_soon" gorm:"column:due_soon"`
	EstimatedMinutes int `json:"estimated_minutes" gorm:"column:estimated_minutes"`
	ActualMinutes    int `json:"actual_minutes" gorm:"column:actual_minutes"`
}

type focusStats struct {
	Sessions int `json:"sessions" gorm:"column:sessions"`
	Minutes  int `json:"minutes" gorm:"column:minutes"`
}

func (a *API) todayStats(c *gin.Context) {
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		// The UI should pass the user's local date. UTC is a deterministic native
		// fallback for non-browser callers and automated health diagnostics.
		date = time.Now().UTC().Format("2006-01-02")
	}
	if !validDate(date) {
		writeError(c, http.StatusBadRequest, "INVALID_DATE", "date must use YYYY-MM-DD")
		return
	}
	now := time.Now().UTC()
	nowTimestamp := now.Format(time.RFC3339Nano)
	dueSoonTimestamp := now.Add(24 * time.Hour).Format(time.RFC3339Nano)

	var tasks taskStats
	err := a.db.WithContext(c.Request.Context()).Raw(`
		SELECT
			COALESCE(SUM(CASE WHEN planned_date = ? AND status <> 'cancelled' THEN 1 ELSE 0 END), 0) AS total,
			COALESCE(SUM(CASE WHEN planned_date = ? AND status = 'done' THEN 1 ELSE 0 END), 0) AS completed,
			COALESCE(SUM(CASE WHEN planned_date = ? AND status NOT IN ('done', 'cancelled') THEN 1 ELSE 0 END), 0) AS remaining,
			COALESCE(SUM(CASE WHEN status NOT IN ('done', 'cancelled') AND due_date IS NOT NULL AND due_date < ? THEN 1 ELSE 0 END), 0) AS overdue,
			COALESCE(SUM(CASE WHEN status NOT IN ('done', 'cancelled') AND due_date >= ? AND due_date <= ? THEN 1 ELSE 0 END), 0) AS due_soon,
			COALESCE(SUM(CASE WHEN planned_date = ? AND status <> 'cancelled' THEN COALESCE(estimated_minutes, 0) ELSE 0 END), 0) AS estimated_minutes,
			COALESCE(SUM(CASE WHEN planned_date = ? THEN actual_minutes ELSE 0 END), 0) AS actual_minutes
		FROM tasks
	`, date, date, date, nowTimestamp, nowTimestamp, dueSoonTimestamp, date, date).Scan(&tasks).Error
	if err != nil {
		writeDatabaseError(c)
		return
	}

	var focus focusStats
	if err := a.db.WithContext(c.Request.Context()).Raw(`
		SELECT COUNT(*) AS sessions, COALESCE(SUM(duration_minutes), 0) AS minutes
		FROM focus_sessions
		WHERE substr(started_at, 1, 10) = ? AND completed = 1
	`, date).Scan(&focus).Error; err != nil {
		writeDatabaseError(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"date":  date,
			"tasks": tasks,
			"focus": focus,
		},
	})
}
