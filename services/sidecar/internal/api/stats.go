package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

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
	Sessions int   `json:"sessions" gorm:"column:sessions"`
	Seconds  int64 `json:"seconds" gorm:"column:seconds"`
	Minutes  int   `json:"minutes" gorm:"column:minutes"`
}

func (a *API) todayStats(c *gin.Context) {
	location, ok := statsLocation(c)
	if !ok {
		return
	}
	date := strings.TrimSpace(c.Query("date"))
	now := a.options.Now().UTC()
	if date == "" {
		date = now.In(location).Format("2006-01-02")
	}
	if !validDate(date) {
		writeError(c, http.StatusBadRequest, "INVALID_DATE", "date must use YYYY-MM-DD")
		return
	}
	localDate, parseErr := time.ParseInLocation("2006-01-02", date, location)
	if parseErr != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DATE", "date must use YYYY-MM-DD")
		return
	}
	dayStartUTC := localDate.UTC()
	dayEndUTC := localDate.AddDate(0, 0, 1).UTC()
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

	type focusIntervalRow struct {
		SessionID       string `gorm:"column:session_id"`
		StartedAt       string `gorm:"column:started_at"`
		EndedAt         string `gorm:"column:ended_at"`
		DurationSeconds int64  `gorm:"column:duration_seconds"`
	}
	var intervals []focusIntervalRow
	if err := a.db.WithContext(c.Request.Context()).Raw(`
		SELECT interval.session_id, interval.started_at, interval.ended_at, interval.duration_seconds
		FROM focus_session_intervals AS interval
		JOIN focus_sessions AS session ON session.id = interval.session_id
		WHERE session.status = 'completed'
		  AND interval.ended_at IS NOT NULL
		  AND interval.duration_seconds > 0
		  AND julianday(interval.ended_at) > julianday(?)
		  AND julianday(interval.started_at) < julianday(?)
	`, dayStartUTC.Format(time.RFC3339Nano), dayEndUTC.Format(time.RFC3339Nano)).Scan(&intervals).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	focusSeconds := int64(0)
	sessions := make(map[string]struct{})
	for _, interval := range intervals {
		startedAt, startErr := parseFocusTimestamp(interval.StartedAt)
		endedAt, endErr := parseFocusTimestamp(interval.EndedAt)
		if startErr != nil || endErr != nil || !endedAt.After(startedAt) {
			writeDatabaseError(c)
			return
		}
		// duration_seconds is the capped accounting fact. API-created intervals
		// end exactly at start + duration, while the minimum below also keeps a
		// manually damaged future end from inflating statistics.
		accountedEnd := startedAt.Add(time.Duration(interval.DurationSeconds) * time.Second)
		if endedAt.Before(accountedEnd) {
			accountedEnd = endedAt
		}
		overlapStart := startedAt
		if dayStartUTC.After(overlapStart) {
			overlapStart = dayStartUTC
		}
		overlapEnd := accountedEnd
		if dayEndUTC.Before(overlapEnd) {
			overlapEnd = dayEndUTC
		}
		if !overlapEnd.After(overlapStart) {
			continue
		}
		seconds := int64(overlapEnd.Sub(overlapStart) / time.Second)
		if seconds <= 0 {
			continue
		}
		focusSeconds += seconds
		sessions[interval.SessionID] = struct{}{}
	}
	focus := focusStats{Sessions: len(sessions), Seconds: focusSeconds, Minutes: int(focusSeconds / 60)}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"date":  date,
			"tasks": tasks,
			"focus": focus,
		},
	})
}

func statsLocation(c *gin.Context) (*time.Location, bool) {
	if raw, present := c.GetQuery("timezone"); present {
		name := strings.TrimSpace(raw)
		if name == "" {
			return time.UTC, true
		}
		location, err := time.LoadLocation(name)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_TIMEZONE", "timezone must be a valid IANA time zone")
			return nil, false
		}
		return location, true
	}
	offset, ok := timezoneOffsetMinutes(c)
	if !ok {
		return nil, false
	}
	return time.FixedZone("request-offset", -offset*60), true
}

func timezoneOffsetMinutes(c *gin.Context) (int, bool) {
	raw, present := c.GetQuery("timezone_offset_minutes")
	if !present {
		return 0, true
	}
	raw = strings.TrimSpace(raw)
	value, err := strconv.Atoi(raw)
	if err != nil || value < -840 || value > 840 {
		writeError(c, http.StatusBadRequest, "INVALID_TIMEZONE_OFFSET", "timezone_offset_minutes must be an integer between -840 and 840")
		return 0, false
	}
	return value, true
}
