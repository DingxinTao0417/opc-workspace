package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	automationPresetProjectCompleted = "project-completed-inbox"
	automationPresetDailyToday       = "daily-today-reminder"
	automationPresetWeeklyReview     = "weekly-review-reminder"
	automationPresetInvoiceOverdue   = "invoice-overdue-task"
	automationPresetAgentRunFailed   = "agent-run-failed-inbox"
	automationMaxAttempts            = 3
)

type automationPresetDefinition struct {
	ID                string
	PresetKey         string
	Name              string
	Description       string
	TriggerType       string
	TriggerLabel      string
	ActionType        string
	ActionLabel       string
	Available         bool
	UnavailableReason string
	DefaultConfig     automationConfig
	Permissions       []string
}

type automationConfig struct {
	Priority  string `json:"priority,omitempty"`
	LocalTime string `json:"local_time,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
}

var automationPresets = []automationPresetDefinition{
	{
		ID: "00000000-0000-5000-8000-000000000101", PresetKey: automationPresetProjectCompleted,
		Name: "项目完成后核对开票", Description: "项目标记完成后创建一条本地核对事项，不生成或发送发票。",
		TriggerType: "event", TriggerLabel: "Project Workflow Event：project_completed",
		ActionType: "inbox_item", ActionLabel: "创建“核对并准备发票”收件箱事项",
		Available: true, DefaultConfig: automationConfig{Priority: "P1"},
		Permissions: []string{"读取本地 Project 完成事件", "创建一条本地 Inbox Item", "记录本地 Automation Run"},
	},
	{
		ID: "00000000-0000-5000-8000-000000000102", PresetKey: automationPresetDailyToday,
		Name: "每日查看今日任务", Description: "每天指定当地时间创建一条应用内提醒。",
		TriggerType: "schedule", TriggerLabel: "每天指定当地时间",
		ActionType: "reminder", ActionLabel: "创建“查看今日任务”本地提醒",
		Available: true, DefaultConfig: automationConfig{LocalTime: "09:00", Timezone: "UTC"},
		Permissions: []string{"读取本地时间", "创建一条本地 Reminder", "记录本地 Automation Run"},
	},
	{
		ID: "00000000-0000-5000-8000-000000000103", PresetKey: automationPresetWeeklyReview,
		Name: "周五本周复盘", Description: "每周五指定当地时间创建一条应用内复盘提醒。",
		TriggerType: "schedule", TriggerLabel: "每周五指定当地时间",
		ActionType: "reminder", ActionLabel: "创建“进行本周复盘”本地提醒",
		Available: true, DefaultConfig: automationConfig{LocalTime: "17:00", Timezone: "UTC"},
		Permissions: []string{"读取本地时间", "创建一条本地 Reminder", "记录本地 Automation Run"},
	},
	{
		ID: "00000000-0000-5000-8000-000000000104", PresetKey: automationPresetInvoiceOverdue,
		Name: "发票逾期跟进", Description: "发票进入逾期状态后创建本地跟进任务；不会自动发送邮件或客户消息。",
		TriggerType: "event", TriggerLabel: "发票工作流事件：invoice_overdue",
		ActionType: "task", ActionLabel: "创建“跟进逾期发票”本地任务",
		Available: true, DefaultConfig: automationConfig{Priority: "P1"},
		Permissions: []string{"读取本地发票逾期事件", "创建一条本地跟进任务", "记录本地自动化运行"},
	},
	{
		ID: "00000000-0000-5000-8000-000000000105", PresetKey: automationPresetAgentRunFailed,
		Name: "Agent 失败诊断", Description: "本地 Agent Run 失败后创建或更新诊断事项。",
		TriggerType: "event", TriggerLabel: "Agent Run failed event",
		ActionType: "inbox_item", ActionLabel: "创建本地诊断 Inbox Item",
		UnavailableReason: "本地 Agent Runtime 尚未交付，当前没有 Agent Run 事实。",
		DefaultConfig:     automationConfig{Priority: "P1"},
		Permissions:       []string{"读取本地 Agent Run 失败事件", "创建一条本地 Inbox Item", "记录本地 Automation Run"},
	},
}

func automationPresetByKey(key string) (automationPresetDefinition, bool) {
	for _, preset := range automationPresets {
		if preset.PresetKey == key {
			return preset, true
		}
	}
	return automationPresetDefinition{}, false
}

func automationPresetByID(id string) (automationPresetDefinition, bool) {
	for _, preset := range automationPresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return automationPresetDefinition{}, false
}

func normalizeAutomationConfig(presetKey string, input automationConfig) (automationConfig, error) {
	preset, ok := automationPresetByKey(presetKey)
	if !ok {
		return automationConfig{}, errors.New("automation preset is not supported")
	}
	if preset.TriggerType == "event" {
		if input.LocalTime != "" || input.Timezone != "" {
			return automationConfig{}, errors.New("event presets do not accept schedule fields")
		}
		priority := strings.TrimSpace(input.Priority)
		if priority == "" {
			priority = preset.DefaultConfig.Priority
		}
		if _, valid := validPriorities[priority]; !valid {
			return automationConfig{}, errors.New("priority must be P0, P1, P2, or P3")
		}
		return automationConfig{Priority: priority}, nil
	}
	if input.Priority != "" {
		return automationConfig{}, errors.New("schedule presets do not accept priority")
	}
	localTime := strings.TrimSpace(input.LocalTime)
	if localTime == "" {
		localTime = preset.DefaultConfig.LocalTime
	}
	if len(localTime) != 5 || localTime[2] != ':' {
		return automationConfig{}, errors.New("local_time must use HH:mm")
	}
	parsed, err := time.Parse("15:04", localTime)
	if err != nil || parsed.Format("15:04") != localTime {
		return automationConfig{}, errors.New("local_time must use a valid 24-hour HH:mm value")
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		timezone = preset.DefaultConfig.Timezone
	}
	if timezone == "Local" || len(timezone) > 100 {
		return automationConfig{}, errors.New("timezone must be a stable IANA timezone")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return automationConfig{}, errors.New("timezone must be a stable IANA timezone")
	}
	return automationConfig{LocalTime: localTime, Timezone: timezone}, nil
}

func encodeAutomationConfig(config automationConfig) (string, error) {
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeAutomationConfig(presetKey, raw string) (automationConfig, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var config automationConfig
	if err := decoder.Decode(&config); err != nil {
		return automationConfig{}, fmt.Errorf("decode automation config: %w", err)
	}
	return normalizeAutomationConfig(presetKey, config)
}

func nextAutomationSchedule(presetKey string, config automationConfig, after time.Time) (time.Time, error) {
	if presetKey != automationPresetDailyToday && presetKey != automationPresetWeeklyReview {
		return time.Time{}, errors.New("automation preset does not use a schedule")
	}
	config, err := normalizeAutomationConfig(presetKey, config)
	if err != nil {
		return time.Time{}, err
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	clock, _ := time.Parse("15:04", config.LocalTime)
	localAfter := after.In(location)
	year, month, day := localAfter.Date()
	if presetKey == automationPresetWeeklyReview {
		daysUntilFriday := (int(time.Friday) - int(localAfter.Weekday()) + 7) % 7
		day += daysUntilFriday
	}
	candidate, err := automationLocalDateTime(location, year, month, day, clock.Hour(), clock.Minute())
	if err != nil {
		return time.Time{}, err
	}
	if !candidate.After(after) {
		step := 1
		if presetKey == automationPresetWeeklyReview {
			step = 7
		}
		localCandidate := candidate.In(location)
		candidate, err = automationLocalDateTime(
			location, localCandidate.Year(), localCandidate.Month(), localCandidate.Day()+step,
			clock.Hour(), clock.Minute(),
		)
		if err != nil {
			return time.Time{}, err
		}
	}
	return candidate, nil
}

// automationLocalDateTime returns the requested local minute. If that minute
// does not exist during a spring-forward transition, it returns the first valid
// minute after the gap. During a repeated fall-back hour it returns the first
// occurrence, making the schedule deterministic.
func automationLocalDateTime(location *time.Location, year int, month time.Month, day, hour, minute int) (time.Time, error) {
	normalizedDate := time.Date(year, month, day, 12, 0, 0, 0, location)
	desiredYear, desiredMonth, desiredDay := normalizedDate.Date()
	start := time.Date(desiredYear, desiredMonth, desiredDay, 0, 0, 0, 0, location)
	for offset := 0; offset <= 26*60; offset++ {
		candidate := start.Add(time.Duration(offset) * time.Minute)
		local := candidate.In(location)
		if local.Year() != desiredYear || local.Month() != desiredMonth || local.Day() != desiredDay {
			continue
		}
		if local.Hour() == hour && local.Minute() == minute {
			return candidate, nil
		}
		if local.Hour() > hour || (local.Hour() == hour && local.Minute() > minute) {
			return candidate, nil
		}
	}
	return time.Time{}, errors.New("local automation schedule could not be resolved")
}

func latestDueAutomationSchedule(presetKey string, config automationConfig, first, now time.Time) (time.Time, time.Time, error) {
	if first.After(now) {
		return time.Time{}, first, errors.New("automation schedule is not due")
	}
	latest := first
	next, err := nextAutomationSchedule(presetKey, config, latest)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	for iterations := 0; !next.After(now); iterations++ {
		if iterations >= 5000 {
			return time.Time{}, time.Time{}, errors.New("automation schedule backlog exceeds safe bound")
		}
		latest = next
		next, err = nextAutomationSchedule(presetKey, config, latest)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return latest, next, nil
}

func automationScheduleStillMeaningful(config automationConfig, scheduledFor, now time.Time) bool {
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return false
	}
	scheduledLocal := scheduledFor.In(location)
	nowLocal := now.In(location)
	return scheduledLocal.Year() == nowLocal.Year() && scheduledLocal.YearDay() == nowLocal.YearDay()
}
