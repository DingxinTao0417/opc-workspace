package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type automationRunOutput struct {
	ID             string         `json:"id"`
	RuleID         string         `json:"rule_id"`
	PresetKey      string         `json:"preset_key"`
	RuleName       string         `json:"rule_name"`
	RuleVersion    int64          `json:"rule_version"`
	TriggerType    string         `json:"trigger_type"`
	SourceEventID  *string        `json:"source_event_id"`
	ScheduledFor   *string        `json:"scheduled_for"`
	Status         string         `json:"status"`
	Attempt        int            `json:"attempt"`
	RetryOfRunID   *string        `json:"retry_of_run_id"`
	Retryable      bool           `json:"retryable"`
	RetryAt        *string        `json:"retry_at"`
	CausedByRunID  *string        `json:"caused_by_run_id"`
	CausalDepth    int            `json:"causal_depth"`
	ConfigSnapshot map[string]any `json:"config_snapshot"`
	ActionSnapshot map[string]any `json:"action_snapshot"`
	ErrorCode      *string        `json:"error_code"`
	ResultType     *string        `json:"result_type"`
	ResultID       *string        `json:"result_id"`
	ResultSummary  string         `json:"result_summary"`
	StartedAt      string         `json:"started_at"`
	EndedAt        string         `json:"ended_at"`
}

type automationRunListMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type automationAttemptInput struct {
	Rule           models.AutomationRule
	TriggerType    string
	SourceEventID  *string
	ScheduledFor   *string
	LogicalKey     string
	Attempt        int
	RetryOfRunID   *string
	Config         automationConfig
	ActionSnapshot map[string]any
	Now            time.Time
}

type automationActionResult struct {
	Type    string
	ID      string
	Summary string
}

func executeProjectCompletionAutomations(tx *gorm.DB, eventID string, project models.Project, nowText string) error {
	var rule models.AutomationRule
	err := tx.First(&rule, "preset_key = ? AND enabled = 1", automationPresetProjectCompleted).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var existing int64
	if err := tx.Model(&models.AutomationRun{}).Where("rule_id = ? AND source_event_id = ?", rule.ID, eventID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
	if err != nil {
		return err
	}
	now, err := time.Parse(time.RFC3339Nano, nowText)
	if err != nil {
		return err
	}
	sourceEventID := eventID
	action := map[string]any{
		"action_type": "inbox_item", "project_id": project.ID, "project_name": project.Name,
		"title": automationProjectCompletionTitle(project.Name), "priority": config.Priority,
	}
	_, err = executeAutomationAttempt(tx, automationAttemptInput{
		Rule: rule, TriggerType: "event", SourceEventID: &sourceEventID,
		LogicalKey: "event:" + rule.ID + ":" + eventID, Attempt: 1,
		Config: config, ActionSnapshot: action, Now: now.UTC(),
	})
	return err
}

func (a *API) executeProjectCompletionAutomationsSafely(tx *gorm.DB, eventID string, project models.Project, nowText string) {
	const savepoint = "project_completion_automation"
	if err := tx.SavePoint(savepoint).Error; err != nil {
		if a.options.Logger != nil {
			a.options.Logger.Print("project completion Automation could not create an isolation boundary")
		}
		return
	}
	if err := executeProjectCompletionAutomations(tx, eventID, project, nowText); err != nil {
		_ = tx.RollbackTo(savepoint).Error
		if a.options.Logger != nil {
			a.options.Logger.Print("project completion Automation failed without changing the source Project")
		}
	}
}

func (a *API) projectDueAutomations(now time.Time) error {
	if err := a.projectDueAutomationRetries(now.UTC()); err != nil {
		return err
	}
	nowText := formatInboxTimestamp(now.UTC())
	var ids []string
	if err := a.db.Model(&models.AutomationRule{}).
		Where("enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?", nowText).
		Order("next_run_at ASC").Order("id ASC").Limit(100).Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.projectDueAutomationRule(id, now.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) projectDueAutomationRule(id string, now time.Time) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		var rule models.AutomationRule
		if err := tx.First(&rule, "id = ? AND enabled = 1", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if rule.NextRunAt == nil {
			return nil
		}
		first, err := time.Parse(time.RFC3339Nano, *rule.NextRunAt)
		if err != nil || first.After(now) {
			return err
		}
		config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
		if err != nil {
			return err
		}
		scheduledFor, next, err := latestDueAutomationSchedule(rule.PresetKey, config, first, now)
		if err != nil {
			return err
		}
		nextText := formatInboxTimestamp(next.UTC())
		if err := tx.Model(&models.AutomationRule{}).Where("id = ? AND version = ? AND enabled = 1", rule.ID, rule.Version).
			Update("next_run_at", nextText).Error; err != nil {
			return err
		}
		scheduledText := formatInboxTimestamp(scheduledFor.UTC())
		logicalKey := "schedule:" + rule.ID + ":" + scheduledText
		var existing int64
		if err := tx.Model(&models.AutomationRun{}).Where("logical_key = ?", logicalKey).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		if !automationScheduleStillMeaningful(config, scheduledFor, now) {
			return insertSkippedAutomationRun(tx, rule, config, scheduledText, logicalKey, now)
		}
		action := automationScheduleActionSnapshot(rule.PresetKey)
		_, err = executeAutomationAttempt(tx, automationAttemptInput{
			Rule: rule, TriggerType: "schedule", ScheduledFor: &scheduledText,
			LogicalKey: logicalKey, Attempt: 1, Config: config,
			ActionSnapshot: action, Now: now,
		})
		return err
	})
}

func insertSkippedAutomationRun(tx *gorm.DB, rule models.AutomationRule, config automationConfig, scheduledFor, logicalKey string, now time.Time) error {
	configJSON, err := encodeAutomationConfig(config)
	if err != nil {
		return err
	}
	actionJSON, err := json.Marshal(automationScheduleActionSnapshot(rule.PresetKey))
	if err != nil {
		return err
	}
	nowText := formatInboxTimestamp(now.UTC())
	errorCode := "SCHEDULE_WINDOW_EXPIRED"
	run := models.AutomationRun{
		ID: uuid.NewString(), RuleID: rule.ID, RuleVersion: rule.Version,
		TriggerType: "schedule", ScheduledFor: &scheduledFor,
		LogicalKey: logicalKey, DedupeKey: logicalKey + ":attempt:1",
		Status: "skipped", Attempt: 1, ConfigSnapshotJSON: configJSON,
		ActionSnapshotJSON: string(actionJSON), ErrorCode: &errorCode,
		ResultSummary: "离线期间错过的旧计划窗口已折叠，不创建过期提醒。",
		StartedAt:     nowText, EndedAt: nowText,
	}
	if err := tx.Create(&run).Error; err != nil {
		return err
	}
	return recordAutomationRunWorkflowEvent(tx, run, "automation_run_skipped", nowText)
}

func executeAutomationAttempt(tx *gorm.DB, input automationAttemptInput) (models.AutomationRun, error) {
	if input.Attempt < 1 || input.Attempt > automationMaxAttempts || input.Rule.Enabled == false {
		return models.AutomationRun{}, errors.New("automation attempt is not allowed")
	}
	configJSON, err := encodeAutomationConfig(input.Config)
	if err != nil {
		return models.AutomationRun{}, err
	}
	actionJSON, err := json.Marshal(input.ActionSnapshot)
	if err != nil {
		return models.AutomationRun{}, err
	}
	nowText := formatInboxTimestamp(input.Now.UTC())
	runID := uuid.NewString()
	savepoint := "automation_action"
	if err := tx.SavePoint(savepoint).Error; err != nil {
		return models.AutomationRun{}, err
	}
	result, actionErr := executeAutomationAction(tx, runID, input, nowText)
	if actionErr == nil {
		resultType, resultID := result.Type, result.ID
		run := models.AutomationRun{
			ID: runID, RuleID: input.Rule.ID, RuleVersion: input.Rule.Version,
			TriggerType: input.TriggerType, SourceEventID: input.SourceEventID, ScheduledFor: input.ScheduledFor,
			LogicalKey: input.LogicalKey, DedupeKey: fmt.Sprintf("%s:attempt:%d", input.LogicalKey, input.Attempt),
			Status: "succeeded", Attempt: input.Attempt, RetryOfRunID: input.RetryOfRunID,
			ConfigSnapshotJSON: configJSON, ActionSnapshotJSON: string(actionJSON),
			ResultType: &resultType, ResultID: &resultID, ResultSummary: result.Summary,
			StartedAt: nowText, EndedAt: nowText,
		}
		if err := tx.Create(&run).Error; err == nil {
			if err = recordAutomationRunWorkflowEvent(tx, run, "automation_run_succeeded", nowText); err == nil {
				return run, nil
			} else {
				actionErr = err
			}
		} else {
			actionErr = err
		}
	}
	if err := tx.RollbackTo(savepoint).Error; err != nil {
		return models.AutomationRun{}, err
	}
	errorCode := "ACTION_WRITE_FAILED"
	retryable := input.Attempt < automationMaxAttempts
	var retryAt *string
	if retryable {
		delay := time.Minute
		if input.Attempt == 2 {
			delay = 5 * time.Minute
		}
		value := formatInboxTimestamp(input.Now.Add(delay).UTC())
		retryAt = &value
	}
	run := models.AutomationRun{
		ID: runID, RuleID: input.Rule.ID, RuleVersion: input.Rule.Version,
		TriggerType: input.TriggerType, SourceEventID: input.SourceEventID, ScheduledFor: input.ScheduledFor,
		LogicalKey: input.LogicalKey, DedupeKey: fmt.Sprintf("%s:attempt:%d", input.LogicalKey, input.Attempt),
		Status: "failed", Attempt: input.Attempt, RetryOfRunID: input.RetryOfRunID,
		Retryable: retryable, RetryAt: retryAt, ConfigSnapshotJSON: configJSON,
		ActionSnapshotJSON: string(actionJSON), ErrorCode: &errorCode,
		ResultSummary: "本地动作未提交；业务来源事实保持不变。",
		StartedAt:     nowText, EndedAt: nowText,
	}
	if err := tx.Create(&run).Error; err != nil {
		return models.AutomationRun{}, err
	}
	if err := recordAutomationRunWorkflowEvent(tx, run, "automation_run_failed", nowText); err != nil {
		return models.AutomationRun{}, err
	}
	return run, nil
}

func executeAutomationAction(tx *gorm.DB, runID string, input automationAttemptInput, nowText string) (automationActionResult, error) {
	actionType, _ := input.ActionSnapshot["action_type"].(string)
	switch actionType {
	case "inbox_item":
		return createAutomationInboxItem(tx, runID, input, nowText)
	case "reminder":
		return createAutomationReminder(tx, runID, input, nowText)
	default:
		return automationActionResult{}, errors.New("automation action is not allowed")
	}
}

func createAutomationInboxItem(tx *gorm.DB, runID string, input automationAttemptInput, nowText string) (automationActionResult, error) {
	title, titleOK := input.ActionSnapshot["title"].(string)
	priority, priorityOK := input.ActionSnapshot["priority"].(string)
	projectID, projectOK := input.ActionSnapshot["project_id"].(string)
	projectName, nameOK := input.ActionSnapshot["project_name"].(string)
	if !titleOK || !priorityOK || !projectOK || !nameOK {
		return automationActionResult{}, errors.New("automation Inbox action snapshot is invalid")
	}
	key := "automation:" + input.LogicalKey
	var existing models.InboxItem
	err := tx.First(&existing, "source_event_key = ?", key).Error
	if err == nil {
		if existing.SourceEntityType != "automation" {
			return automationActionResult{}, errors.New("automation source key is incompatible")
		}
		return automationActionResult{Type: "inbox_item", ID: existing.ID, Summary: "已复用同一自动化事项。"}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return automationActionResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"automation_rule_id": input.Rule.ID, "automation_run_id": runID,
		"preset_key": input.Rule.PresetKey, "project_id": projectID, "project_name": projectName,
	})
	if err != nil {
		return automationActionResult{}, err
	}
	sourceID := runID
	item := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: title,
		Summary:          "项目已完成。请人工核对是否需要开票，并准备后续资料；自动化不会生成或发送发票。",
		SourceEntityType: "automation", SourceEntityID: &sourceID, SourceEventKey: &key,
		Priority: priority, Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: string(payload), Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&item).Error; err != nil {
		return automationActionResult{}, err
	}
	if err := recordInboxWorkflowEventAs(tx, item.ID, "source_projected", models.BuiltinSystemActorID, nil, inboxItemEventState(item, ""), "", nowText); err != nil {
		return automationActionResult{}, err
	}
	return automationActionResult{Type: "inbox_item", ID: item.ID, Summary: "已创建本地核对事项。"}, nil
}

func createAutomationReminder(tx *gorm.DB, runID string, input automationAttemptInput, nowText string) (automationActionResult, error) {
	title, titleOK := input.ActionSnapshot["title"].(string)
	summary, summaryOK := input.ActionSnapshot["summary"].(string)
	priority, priorityOK := input.ActionSnapshot["priority"].(string)
	if !titleOK || !summaryOK || !priorityOK {
		return automationActionResult{}, errors.New("automation Reminder action snapshot is invalid")
	}
	triggerAt := nowText
	if input.ScheduledFor != nil && input.Attempt == 1 {
		triggerAt = *input.ScheduledFor
	}
	id := uuid.NewString()
	sourceID := runID
	reminder := models.Reminder{
		ID: id, SourceEntityType: "automation", SourceEntityID: &sourceID,
		Title: title, Summary: summary, Priority: priority, TriggerAt: triggerAt,
		Status: "scheduled", SourceEventKey: "reminder:" + id + ":due",
		CreatedByActorID: models.BuiltinSystemActorID, SeriesID: id,
		RecurrenceType: "none", RecurrenceInterval: 1, RecurrenceTimezone: "UTC", OccurrenceNumber: 1,
		Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&reminder).Error; err != nil {
		return automationActionResult{}, err
	}
	if err := recordReminderWorkflowEvent(tx, reminder.ID, "reminder_created", nil, reminderEventState(reminder, ""), models.BuiltinSystemActorID, "", nowText); err != nil {
		return automationActionResult{}, err
	}
	return automationActionResult{Type: "reminder", ID: reminder.ID, Summary: "已创建本地提醒。"}, nil
}

func automationScheduleActionSnapshot(presetKey string) map[string]any {
	if presetKey == automationPresetWeeklyReview {
		return map[string]any{
			"action_type": "reminder", "title": "进行本周复盘",
			"summary": "回顾本周完成、阻塞和下周最重要的工作。", "priority": "P2",
		}
	}
	return map[string]any{
		"action_type": "reminder", "title": "查看今日任务",
		"summary": "打开今日工作台，确认今天最重要的任务和截止风险。", "priority": "P2",
	}
}

func automationProjectCompletionTitle(projectName string) string {
	value := []rune("核对并准备发票：" + projectName)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

func recordAutomationRunWorkflowEvent(tx *gorm.DB, run models.AutomationRun, action, nowText string) error {
	current := map[string]any{
		"rule_id": run.RuleID, "status": run.Status, "attempt": run.Attempt,
		"result_type": run.ResultType, "result_id": run.ResultID, "error_code": run.ErrorCode,
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return err
	}
	actorID := models.BuiltinSystemActorID
	currentText := string(currentJSON)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "automation_run", AggregateID: run.ID,
		Action: action, ActorID: &actorID, CurrentJSON: &currentText, CreatedAt: nowText,
	}
	return tx.Create(&event).Error
}

func (a *API) projectDueAutomationRetries(now time.Time) error {
	nowText := formatInboxTimestamp(now.UTC())
	var ids []string
	if err := a.db.Table("automation_runs AS failed").
		Select("failed.id").
		Joins("JOIN automation_rules AS rule ON rule.id = failed.rule_id AND rule.enabled = 1").
		Where("failed.status = 'failed' AND failed.retryable = 1 AND failed.retry_at IS NOT NULL AND failed.retry_at <= ?", nowText).
		Where("NOT EXISTS (SELECT 1 FROM automation_runs child WHERE child.retry_of_run_id = failed.id)").
		Order("failed.retry_at ASC").Order("failed.id ASC").Limit(100).Pluck("failed.id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.retryAutomationRunByID(id, now.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) retryAutomationRunByID(id string, now time.Time) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		var previous models.AutomationRun
		if err := tx.First(&previous, "id = ?", id).Error; err != nil {
			return err
		}
		if previous.Status != "failed" || !previous.Retryable || previous.Attempt >= automationMaxAttempts {
			return newProjectRequestError(http.StatusConflict, "AUTOMATION_RUN_NOT_RETRYABLE", "Automation run cannot be retried")
		}
		var existing int64
		if err := tx.Model(&models.AutomationRun{}).Where("retry_of_run_id = ?", previous.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return newProjectRequestError(http.StatusConflict, "AUTOMATION_RETRY_ALREADY_EXISTS", "A retry attempt already exists")
		}
		var rule models.AutomationRule
		if err := tx.First(&rule, "id = ?", previous.RuleID).Error; err != nil {
			return err
		}
		if !rule.Enabled {
			return newProjectRequestError(http.StatusConflict, "AUTOMATION_RULE_DISABLED", "Enable the automation rule before retrying")
		}
		// A retry is another attempt of the original immutable run. Keep the
		// captured rule version even if its editable configuration has since
		// advanced; config and action snapshots already come from that run.
		rule.Version = previous.RuleVersion
		config, err := decodeAutomationConfig(previousRulePresetKey(rule), previous.ConfigSnapshotJSON)
		if err != nil {
			return err
		}
		var action map[string]any
		if err := json.Unmarshal([]byte(previous.ActionSnapshotJSON), &action); err != nil {
			return err
		}
		retryOf := previous.ID
		_, err = executeAutomationAttempt(tx, automationAttemptInput{
			Rule: rule, TriggerType: previous.TriggerType,
			SourceEventID: previous.SourceEventID, ScheduledFor: previous.ScheduledFor,
			LogicalKey: previous.LogicalKey, Attempt: previous.Attempt + 1, RetryOfRunID: &retryOf,
			Config: config, ActionSnapshot: action, Now: now.UTC(),
		})
		return err
	})
}

func previousRulePresetKey(rule models.AutomationRule) string { return rule.PresetKey }

func (a *API) listAutomationRuns(c *gin.Context) {
	page, ok := queryInt(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	pageSize, ok := queryInt(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}
	query := a.db.WithContext(c.Request.Context()).Model(&models.AutomationRun{})
	if ruleID := strings.TrimSpace(c.Query("rule_id")); ruleID != "" {
		parsed, err := uuid.Parse(ruleID)
		if err != nil || parsed.String() != ruleID {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "rule_id must be a canonical UUID")
			return
		}
		query = query.Where("rule_id = ?", ruleID)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "succeeded" && status != "failed" && status != "skipped" && status != "cancelled" {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status is invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	var runs []models.AutomationRun
	if err := query.Order("started_at DESC").Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&runs).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	data := make([]automationRunOutput, len(runs))
	for index := range runs {
		output, err := automationRunOutputFromModel(a.db, runs[index])
		if err != nil {
			writeDatabaseError(c)
			return
		}
		data[index] = output
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": automationRunListMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) getAutomationRun(c *gin.Context) {
	id, ok := automationRunID(c)
	if !ok {
		return
	}
	var run models.AutomationRun
	if err := a.db.WithContext(c.Request.Context()).First(&run, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AUTOMATION_RUN_NOT_FOUND", "Automation run not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	output, err := automationRunOutputFromModel(a.db, run)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": output})
}

func (a *API) retryAutomationRun(c *gin.Context) {
	id, ok := automationRunID(c)
	if !ok {
		return
	}
	if err := a.retryAutomationRunByID(id, a.options.Now().UTC()); err != nil {
		if writeProjectRequestError(c, err) {
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "AUTOMATION_RUN_NOT_FOUND", "Automation run not found")
			return
		}
		writeDatabaseError(c)
		return
	}
	var latest models.AutomationRun
	if err := a.db.Where("retry_of_run_id = ?", id).Order("attempt DESC").First(&latest).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	output, err := automationRunOutputFromModel(a.db, latest)
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": output})
}

func automationRunID(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_AUTOMATION_RUN_ID", "Automation run id must be a canonical UUID")
		return "", false
	}
	return id, true
}

func automationRunOutputFromModel(db *gorm.DB, run models.AutomationRun) (automationRunOutput, error) {
	var rule models.AutomationRule
	if err := db.First(&rule, "id = ?", run.RuleID).Error; err != nil {
		return automationRunOutput{}, err
	}
	preset, ok := automationPresetByKey(rule.PresetKey)
	if !ok {
		return automationRunOutput{}, errors.New("automation run preset is invalid")
	}
	var config, action map[string]any
	if err := json.Unmarshal([]byte(run.ConfigSnapshotJSON), &config); err != nil {
		return automationRunOutput{}, err
	}
	if err := json.Unmarshal([]byte(run.ActionSnapshotJSON), &action); err != nil {
		return automationRunOutput{}, err
	}
	return automationRunOutput{
		ID: run.ID, RuleID: run.RuleID, PresetKey: rule.PresetKey, RuleName: preset.Name,
		RuleVersion: run.RuleVersion, TriggerType: run.TriggerType,
		SourceEventID: run.SourceEventID, ScheduledFor: run.ScheduledFor,
		Status: run.Status, Attempt: run.Attempt, RetryOfRunID: run.RetryOfRunID,
		Retryable: run.Retryable, RetryAt: run.RetryAt,
		CausedByRunID: run.CausedByRunID, CausalDepth: run.CausalDepth,
		ConfigSnapshot: config, ActionSnapshot: action, ErrorCode: run.ErrorCode,
		ResultType: run.ResultType, ResultID: run.ResultID, ResultSummary: run.ResultSummary,
		StartedAt: normalizeTimestamp(run.StartedAt), EndedAt: normalizeTimestamp(run.EndedAt),
	}, nil
}
