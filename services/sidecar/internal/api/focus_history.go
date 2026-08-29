package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxFocusStatsDays = 93

type focusHistoryListResponse struct {
	Data []focusSessionOutput `json:"data"`
	Meta pageMeta             `json:"meta"`
}

type focusPeriodDay struct {
	Date     string `json:"date"`
	Sessions int    `json:"sessions"`
	Seconds  int64  `json:"seconds"`
	Minutes  int    `json:"minutes"`
}

type focusPeriodProject struct {
	ProjectID   *string `json:"project_id"`
	ProjectName *string `json:"project_name"`
	Sessions    int     `json:"sessions"`
	Seconds     int64   `json:"seconds"`
	Minutes     int     `json:"minutes"`
}

type focusPeriodStatsResponse struct {
	DateFrom          string               `json:"date_from"`
	DateTo            string               `json:"date_to"`
	Timezone          string               `json:"timezone"`
	Totals            focusStats           `json:"totals"`
	Days              []focusPeriodDay     `json:"days"`
	Projects          []focusPeriodProject `json:"projects"`
	CurrentStreakDays int                  `json:"current_streak_days"`
	LongestStreakDays int                  `json:"longest_streak_days"`
}

type completedFocusInterval struct {
	SessionID       string  `gorm:"column:session_id"`
	StartedAt       string  `gorm:"column:started_at"`
	EndedAt         string  `gorm:"column:ended_at"`
	DurationSeconds int64   `gorm:"column:duration_seconds"`
	ProjectID       *string `gorm:"column:project_id"`
	ProjectName     *string `gorm:"column:project_name"`
}

func (a *API) listFocusSessions(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 100000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status == "" {
		status = "terminal"
	}
	allowed := map[string]struct{}{
		"terminal": {}, "completed": {}, "cancelled": {}, "interrupted": {},
	}
	if _, valid := allowed[status]; !valid {
		writeError(c, http.StatusBadRequest, "INVALID_FOCUS_STATUS", "status must be terminal, completed, cancelled, or interrupted")
		return
	}

	query := a.db.WithContext(c.Request.Context()).Table("focus_sessions AS focus_session")
	if status == "terminal" {
		query = query.Where("focus_session.status IN ?", []string{"completed", "cancelled", "interrupted"})
	} else {
		query = query.Where("focus_session.status = ?", status)
	}
	if rawTaskID, present := c.GetQuery("task_id"); present {
		taskID := strings.TrimSpace(rawTaskID)
		parsed, err := uuid.Parse(taskID)
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_TASK_ID", "task_id must be a UUID")
			return
		}
		query = query.Where("focus_session.task_id = ?", parsed.String())
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	rows := make([]focusSessionRow, 0)
	if err := query.
		Select("focus_session.*, task.title AS task_title").
		Joins("LEFT JOIN tasks AS task ON task.id = focus_session.task_id").
		Order("focus_session.ended_at DESC").
		Order("focus_session.id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	data := make([]focusSessionOutput, 0, len(rows))
	for _, row := range rows {
		output, err := normalizeFocusHistoryRow(row)
		if err != nil {
			writeDatabaseError(c)
			return
		}
		data = append(data, output)
	}
	c.JSON(http.StatusOK, focusHistoryListResponse{
		Data: data,
		Meta: pageMeta{Page: page, PageSize: pageSize, Total: total},
	})
}

func (a *API) focusPeriodStats(c *gin.Context) {
	location, ok := statsLocation(c)
	if !ok {
		return
	}
	now := a.options.Now().UTC().In(location)
	dateFrom := strings.TrimSpace(c.Query("date_from"))
	dateTo := strings.TrimSpace(c.Query("date_to"))
	if dateFrom == "" && dateTo == "" {
		dateTo = now.Format("2006-01-02")
		dateFrom = now.AddDate(0, 0, -6).Format("2006-01-02")
	} else if dateFrom == "" || dateTo == "" {
		writeError(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "date_from and date_to must be provided together")
		return
	}
	if !validDate(dateFrom) || !validDate(dateTo) {
		writeError(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "date_from and date_to must use YYYY-MM-DD")
		return
	}
	localStart, err := time.ParseInLocation("2006-01-02", dateFrom, location)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "date_from and date_to must use YYYY-MM-DD")
		return
	}
	localEnd, err := time.ParseInLocation("2006-01-02", dateTo, location)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "date_from and date_to must use YYYY-MM-DD")
		return
	}
	if localEnd.Before(localStart) {
		writeError(c, http.StatusBadRequest, "INVALID_DATE_RANGE", "date_to must not be before date_from")
		return
	}
	// Date arithmetic is calendar based so a DST transition still counts as one day.
	dayCount := 0
	for day := localStart; !day.After(localEnd); day = day.AddDate(0, 0, 1) {
		dayCount++
	}
	if dayCount > maxFocusStatsDays {
		writeError(c, http.StatusBadRequest, "DATE_RANGE_TOO_LARGE", "focus statistics range cannot exceed 93 days")
		return
	}

	rangeStartUTC := localStart.UTC()
	rangeEndUTC := localEnd.AddDate(0, 0, 1).UTC()
	var intervals []completedFocusInterval
	if err := a.db.WithContext(c.Request.Context()).Raw(`
		SELECT interval.session_id, interval.started_at, interval.ended_at, interval.duration_seconds,
		       task.project_id, project.name AS project_name
		FROM focus_session_intervals AS interval
		JOIN focus_sessions AS session ON session.id = interval.session_id
		LEFT JOIN tasks AS task ON task.id = session.task_id
		LEFT JOIN projects AS project ON project.id = task.project_id
		WHERE session.status = 'completed'
		  AND interval.ended_at IS NOT NULL
		  AND interval.duration_seconds > 0
		  AND julianday(interval.ended_at) > julianday(?)
		  AND julianday(interval.started_at) < julianday(?)
	`, rangeStartUTC.Format(time.RFC3339Nano), rangeEndUTC.Format(time.RFC3339Nano)).Scan(&intervals).Error; err != nil {
		writeDatabaseError(c)
		return
	}

	days := make([]focusPeriodDay, 0, dayCount)
	totalSessions := make(map[string]struct{})
	longestStreak := 0
	runningStreak := 0
	for day := localStart; !day.After(localEnd); day = day.AddDate(0, 0, 1) {
		dayStartUTC := day.UTC()
		dayEndUTC := day.AddDate(0, 0, 1).UTC()
		daySessions := make(map[string]struct{})
		seconds := int64(0)
		for _, interval := range intervals {
			overlapSeconds, overlapErr := focusIntervalOverlapSeconds(interval, dayStartUTC, dayEndUTC)
			if overlapErr != nil {
				writeDatabaseError(c)
				return
			}
			if overlapSeconds == 0 {
				continue
			}
			seconds += overlapSeconds
			daySessions[interval.SessionID] = struct{}{}
			totalSessions[interval.SessionID] = struct{}{}
		}
		days = append(days, focusPeriodDay{
			Date: day.Format("2006-01-02"), Sessions: len(daySessions),
			Seconds: seconds, Minutes: int(seconds / 60),
		})
		if seconds > 0 {
			runningStreak++
			if runningStreak > longestStreak {
				longestStreak = runningStreak
			}
		} else {
			runningStreak = 0
		}
	}
	totalSeconds := int64(0)
	for _, day := range days {
		totalSeconds += day.Seconds
	}
	type projectAccumulator struct {
		projectID   *string
		projectName *string
		sessions    map[string]struct{}
		seconds     int64
	}
	projectsByID := make(map[string]*projectAccumulator)
	for _, interval := range intervals {
		overlapSeconds, overlapErr := focusIntervalOverlapSeconds(interval, rangeStartUTC, rangeEndUTC)
		if overlapErr != nil {
			writeDatabaseError(c)
			return
		}
		if overlapSeconds == 0 {
			continue
		}
		key := ""
		if interval.ProjectID != nil {
			key = *interval.ProjectID
		}
		project := projectsByID[key]
		if project == nil {
			project = &projectAccumulator{
				projectID: interval.ProjectID, projectName: interval.ProjectName,
				sessions: make(map[string]struct{}),
			}
			projectsByID[key] = project
		}
		project.seconds += overlapSeconds
		project.sessions[interval.SessionID] = struct{}{}
	}
	projects := make([]focusPeriodProject, 0, len(projectsByID))
	for _, project := range projectsByID {
		projects = append(projects, focusPeriodProject{
			ProjectID: project.projectID, ProjectName: project.projectName,
			Sessions: len(project.sessions), Seconds: project.seconds, Minutes: int(project.seconds / 60),
		})
	}
	sort.Slice(projects, func(left, right int) bool {
		if projects[left].Seconds != projects[right].Seconds {
			return projects[left].Seconds > projects[right].Seconds
		}
		leftName := ""
		rightName := ""
		if projects[left].ProjectName != nil {
			leftName = *projects[left].ProjectName
		}
		if projects[right].ProjectName != nil {
			rightName = *projects[right].ProjectName
		}
		if leftName != rightName {
			return leftName < rightName
		}
		leftID := ""
		rightID := ""
		if projects[left].ProjectID != nil {
			leftID = *projects[left].ProjectID
		}
		if projects[right].ProjectID != nil {
			rightID = *projects[right].ProjectID
		}
		return leftID < rightID
	})
	c.JSON(http.StatusOK, gin.H{"data": focusPeriodStatsResponse{
		DateFrom: dateFrom, DateTo: dateTo, Timezone: location.String(),
		Totals: focusStats{Sessions: len(totalSessions), Seconds: totalSeconds, Minutes: int(totalSeconds / 60)},
		Days:   days, Projects: projects, CurrentStreakDays: runningStreak, LongestStreakDays: longestStreak,
	}})
}

func focusIntervalOverlapSeconds(interval completedFocusInterval, rangeStart, rangeEnd time.Time) (int64, error) {
	startedAt, startErr := parseFocusTimestamp(interval.StartedAt)
	endedAt, endErr := parseFocusTimestamp(interval.EndedAt)
	if startErr != nil || endErr != nil || !endedAt.After(startedAt) || interval.DurationSeconds <= 0 {
		return 0, errors.New("invalid completed Focus interval")
	}
	accountedEnd := startedAt.Add(time.Duration(interval.DurationSeconds) * time.Second)
	if endedAt.Before(accountedEnd) {
		accountedEnd = endedAt
	}
	overlapStart := startedAt
	if rangeStart.After(overlapStart) {
		overlapStart = rangeStart
	}
	overlapEnd := accountedEnd
	if rangeEnd.Before(overlapEnd) {
		overlapEnd = rangeEnd
	}
	if !overlapEnd.After(overlapStart) {
		return 0, nil
	}
	return int64(overlapEnd.Sub(overlapStart) / time.Second), nil
}

func normalizeFocusHistoryRow(row focusSessionRow) (focusSessionOutput, error) {
	if _, err := parseFocusTimestamp(row.StartedAt); err != nil {
		return focusSessionOutput{}, err
	}
	if row.EndedAt == nil {
		return focusSessionOutput{}, errors.New("terminal Focus Session is missing ended_at")
	}
	if _, err := parseFocusTimestamp(*row.EndedAt); err != nil {
		return focusSessionOutput{}, err
	}
	if row.Status != "completed" && row.Status != "cancelled" && row.Status != "interrupted" {
		return focusSessionOutput{}, errors.New("non-terminal Focus Session in history")
	}
	normalizeFocusSession(&row.FocusSession)
	return focusSessionOutput{FocusSession: row.FocusSession, TaskTitle: row.TaskTitle}, nil
}
