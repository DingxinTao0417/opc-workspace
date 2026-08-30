package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	automationInboxSourceType              = "automation"
	automationProjectCompletionItemSummary = "项目已完成。请人工核对是否需要开票，并准备后续资料；自动化不会生成或发送发票。"
)

var (
	errAutomationInboxSourceConflict    = errors.New("automation Inbox action was already committed by another run")
	errAutomationActionSnapshotInvalid  = errors.New("automation action snapshot is invalid")
	errAutomationAttemptContractInvalid = errors.New("automation attempt contract is invalid")
	errAutomationRunNotFound            = errors.New("automation run not found")
	errAutomationSourceEventInvalid     = errors.New("automation source event is invalid")
)

type automationProjectCompletionAction struct {
	Title       string
	Priority    string
	ProjectID   string
	ProjectName string
}

type automationInvoiceOverdueAction struct {
	InvoiceID     string
	InvoiceNumber string
	ProjectID     *string
	Title         string
	Description   string
	Priority      string
	DueDate       *string
	PlannedDate   *string
}

type automationInvoiceEventSnapshot struct {
	InvoiceNumber string  `json:"invoice_number"`
	ClientID      string  `json:"client_id"`
	ProjectID     *string `json:"project_id"`
	AmountMinor   int64   `json:"amount_minor"`
	Currency      string  `json:"currency"`
	Status        string  `json:"status"`
	IssueDate     string  `json:"issue_date"`
	DueDate       string  `json:"due_date"`
	PaidDate      *string `json:"paid_date"`
	Notes         string  `json:"notes"`
	Version       int64   `json:"version"`
}

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

type automationRunSourceOutput struct {
	Kind          string  `json:"kind"`
	Available     bool    `json:"available"`
	EventID       *string `json:"event_id"`
	AggregateType *string `json:"aggregate_type"`
	AggregateID   *string `json:"aggregate_id"`
	Action        *string `json:"action"`
	OccurredAt    *string `json:"occurred_at"`
	ScheduledFor  *string `json:"scheduled_for"`
}

type automationRunRetrySummary struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	Attempt       int     `json:"attempt"`
	RetryOfRunID  *string `json:"retry_of_run_id"`
	Retryable     bool    `json:"retryable"`
	RetryAt       *string `json:"retry_at"`
	ErrorCode     *string `json:"error_code"`
	ResultType    *string `json:"result_type"`
	ResultID      *string `json:"result_id"`
	ResultSummary string  `json:"result_summary"`
	StartedAt     string  `json:"started_at"`
	EndedAt       string  `json:"ended_at"`
}

type automationRunDetailOutput struct {
	automationRunOutput
	Source     automationRunSourceOutput   `json:"source"`
	RetryChain []automationRunRetrySummary `json:"retry_chain"`
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
	switch {
	case errors.Is(actionErr, errAutomationInboxSourceConflict):
		errorCode = "SOURCE_EVENT_CONFLICT"
		retryable = false
	case errors.Is(actionErr, errAutomationActionSnapshotInvalid):
		errorCode = "ACTION_SNAPSHOT_INVALID"
		retryable = false
	case errors.Is(actionErr, errAutomationAttemptContractInvalid):
		errorCode = "ATTEMPT_CONTRACT_INVALID"
		retryable = false
	case errors.Is(actionErr, errAutomationSourceEventInvalid):
		errorCode = "SOURCE_EVENT_INVALID"
		retryable = false
	}
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
	if input.Rule.PresetKey == automationPresetInvoiceOverdue && actionType != "task" {
		return automationActionResult{}, errAutomationActionSnapshotInvalid
	}
	switch actionType {
	case "inbox_item":
		return createAutomationInboxItem(tx, runID, input, nowText)
	case "task":
		return createAutomationInvoiceOverdueTask(tx, runID, input, nowText)
	case "reminder":
		return createAutomationReminder(tx, runID, input, nowText)
	default:
		return automationActionResult{}, errors.New("automation action is not allowed")
	}
}

func createAutomationInboxItem(tx *gorm.DB, runID string, input automationAttemptInput, nowText string) (automationActionResult, error) {
	action, err := automationProjectCompletionActionFromSnapshot(input.ActionSnapshot)
	if err != nil {
		return automationActionResult{}, err
	}
	key := "automation:" + input.LogicalKey
	var existing models.InboxItem
	err = tx.First(&existing, "source_event_key = ?", key).Error
	if err == nil {
		// Every call to executeAutomationAttempt owns a fresh run ID. A valid
		// or damaged persisted item under this immutable source key cannot be
		// reused without making source_entity_id/payload disagree with that run.
		// Normal event dedupe prevents this branch.
		return automationActionResult{}, errAutomationInboxSourceConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return automationActionResult{}, err
	}
	if err := validateAutomationProjectCompletionInboxAttempt(tx, input, action); err != nil {
		return automationActionResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"automation_rule_id": input.Rule.ID, "automation_run_id": runID,
		"preset_key": input.Rule.PresetKey, "project_id": action.ProjectID, "project_name": action.ProjectName,
	})
	if err != nil {
		return automationActionResult{}, err
	}
	sourceID := runID
	item := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: action.Title,
		Summary:          automationProjectCompletionItemSummary,
		SourceEntityType: automationInboxSourceType, SourceEntityID: &sourceID, SourceEventKey: &key,
		Priority: action.Priority, Status: "open", ResolutionPolicy: "manual",
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

func automationProjectCompletionActionFromSnapshot(snapshot map[string]any) (automationProjectCompletionAction, error) {
	actionType, actionTypeOK := snapshot["action_type"].(string)
	title, titleOK := snapshot["title"].(string)
	priority, priorityOK := snapshot["priority"].(string)
	projectID, projectOK := snapshot["project_id"].(string)
	projectName, nameOK := snapshot["project_name"].(string)
	if len(snapshot) != 5 || !actionTypeOK || actionType != "inbox_item" || !titleOK || !priorityOK ||
		!projectOK || !nameOK || title != automationProjectCompletionTitle(projectName) {
		return automationProjectCompletionAction{}, errors.New("automation Inbox action snapshot is invalid")
	}
	if _, valid := validPriorities[priority]; !valid || !validCanonicalAutomationUUID(projectID) ||
		strings.TrimSpace(projectName) == "" || strings.TrimSpace(projectName) != projectName {
		return automationProjectCompletionAction{}, errors.New("automation Inbox action snapshot is invalid")
	}
	return automationProjectCompletionAction{Title: title, Priority: priority, ProjectID: projectID, ProjectName: projectName}, nil
}

func validCanonicalAutomationUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func validateAutomationProjectCompletionInboxAttempt(
	tx *gorm.DB,
	input automationAttemptInput,
	action automationProjectCompletionAction,
) error {
	preset, presetOK := automationPresetByKey(input.Rule.PresetKey)
	if !presetOK || input.Rule.PresetKey != automationPresetProjectCompleted || !preset.Available ||
		preset.ID != input.Rule.ID || preset.TriggerType != "event" || preset.ActionType != "inbox_item" ||
		!input.Rule.Enabled || input.Rule.Version < 1 || !validCanonicalAutomationUUID(input.Rule.ID) ||
		input.TriggerType != "event" || input.SourceEventID == nil ||
		!validCanonicalAutomationUUID(*input.SourceEventID) || input.ScheduledFor != nil ||
		input.LogicalKey != "event:"+input.Rule.ID+":"+*input.SourceEventID ||
		input.Attempt < 1 || input.Attempt > automationMaxAttempts ||
		input.Config.Priority != action.Priority || input.Config.LocalTime != "" || input.Config.Timezone != "" {
		return errors.New("automation Inbox attempt contract is invalid")
	}
	if (input.Attempt == 1 && input.RetryOfRunID != nil) ||
		(input.Attempt > 1 && (input.RetryOfRunID == nil || !validCanonicalAutomationUUID(*input.RetryOfRunID))) {
		return errors.New("automation Inbox attempt contract is invalid")
	}
	var sourceEvent models.WorkflowEvent
	if err := tx.First(&sourceEvent, "id = ?", *input.SourceEventID).Error; err != nil {
		return errors.New("automation Inbox source event is missing")
	}
	if sourceEvent.AggregateType != "project" || sourceEvent.AggregateID != action.ProjectID ||
		sourceEvent.Action != "project_completed" || sourceEvent.CurrentJSON == nil {
		return errors.New("automation Inbox source event contract is invalid")
	}
	var eventProject map[string]any
	if err := json.Unmarshal([]byte(*sourceEvent.CurrentJSON), &eventProject); err != nil {
		return errors.New("automation Inbox source event snapshot is invalid")
	}
	projectID, projectIDOK := eventProject["id"].(string)
	projectName, projectNameOK := eventProject["name"].(string)
	projectStatus, projectStatusOK := eventProject["status"].(string)
	if !projectIDOK || !projectNameOK || !projectStatusOK || projectID != action.ProjectID ||
		projectName != action.ProjectName || projectStatus != "completed" {
		return errors.New("automation Inbox source event snapshot is invalid")
	}
	return nil
}

func automationInvoiceOverdueActionSnapshot(invoice models.Invoice, priority string) map[string]any {
	var projectID any
	if invoice.ProjectID != nil {
		projectID = *invoice.ProjectID
	}
	return map[string]any{
		"action_type":    "task",
		"invoice_id":     invoice.ID,
		"invoice_number": invoice.InvoiceNumber,
		"project_id":     projectID,
		"title":          automationInvoiceOverdueTaskTitle(invoice.InvoiceNumber),
		"description": automationInvoiceOverdueTaskDescription(
			invoice.InvoiceNumber, invoice.DueDate, invoice.AmountMinor, invoice.Currency,
		),
		"kind":          "followup",
		"status":        "todo",
		"review_policy": "none",
		"priority":      priority,
		// An Invoice due date is already in the past and is not a safe Task
		// deadline. The source event also carries no owner-selected planning
		// date, so neither field is invented by this preset.
		"due_date":     nil,
		"planned_date": nil,
	}
}

func createAutomationInvoiceOverdueTask(
	tx *gorm.DB,
	runID string,
	input automationAttemptInput,
	nowText string,
) (automationActionResult, error) {
	action, err := automationInvoiceOverdueActionFromSnapshot(input.ActionSnapshot)
	if err != nil {
		return automationActionResult{}, err
	}
	if err := validateAutomationInvoiceOverdueTaskAttempt(tx, input, action); err != nil {
		return automationActionResult{}, err
	}
	if action.ProjectID != nil {
		if err := requireAssignableProject(tx, *action.ProjectID); err != nil {
			return automationActionResult{}, err
		}
	}
	task := models.Task{
		ID: uuid.NewString(), Title: action.Title, Description: action.Description,
		Kind: "followup", Status: "todo", ReviewPolicy: "none", Priority: action.Priority,
		ProjectID: action.ProjectID, DueDate: action.DueDate, PlannedDate: action.PlannedDate,
		ActualMinutes: 0, Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := tx.Create(&task).Error; err != nil {
		return automationActionResult{}, err
	}
	if err := recordAutomationTaskCreatedWorkflowEvent(tx, task, runID, input, action.InvoiceID, nowText); err != nil {
		return automationActionResult{}, err
	}
	return automationActionResult{Type: "task", ID: task.ID, Summary: "已创建本地发票逾期跟进任务。"}, nil
}

func automationInvoiceOverdueActionFromSnapshot(snapshot map[string]any) (automationInvoiceOverdueAction, error) {
	actionType, actionTypeOK := snapshot["action_type"].(string)
	invoiceID, invoiceIDOK := snapshot["invoice_id"].(string)
	invoiceNumber, invoiceNumberOK := snapshot["invoice_number"].(string)
	title, titleOK := snapshot["title"].(string)
	description, descriptionOK := snapshot["description"].(string)
	kind, kindOK := snapshot["kind"].(string)
	status, statusOK := snapshot["status"].(string)
	reviewPolicy, reviewPolicyOK := snapshot["review_policy"].(string)
	priority, priorityOK := snapshot["priority"].(string)
	projectID, projectOK := automationSnapshotOptionalString(snapshot, "project_id")
	dueDate, dueOK := automationSnapshotOptionalString(snapshot, "due_date")
	plannedDate, plannedOK := automationSnapshotOptionalString(snapshot, "planned_date")
	if len(snapshot) != 12 || !actionTypeOK || actionType != "task" || !invoiceIDOK ||
		!invoiceNumberOK || !titleOK || !descriptionOK || !kindOK || kind != "followup" ||
		!statusOK || status != "todo" || !reviewPolicyOK || reviewPolicy != "none" ||
		!priorityOK || !projectOK || !dueOK || dueDate != nil || !plannedOK || plannedDate != nil {
		return automationInvoiceOverdueAction{}, errAutomationActionSnapshotInvalid
	}
	if !validCanonicalAutomationUUID(invoiceID) || strings.TrimSpace(invoiceNumber) != invoiceNumber ||
		len([]rune(invoiceNumber)) < 1 || len([]rune(invoiceNumber)) > 80 ||
		strings.TrimSpace(title) != title || len([]rune(title)) < 2 || len([]rune(title)) > 200 ||
		title != automationInvoiceOverdueTaskTitle(invoiceNumber) || len([]rune(description)) > 10_000 {
		return automationInvoiceOverdueAction{}, errAutomationActionSnapshotInvalid
	}
	if projectID != nil && !validCanonicalAutomationUUID(*projectID) {
		return automationInvoiceOverdueAction{}, errAutomationActionSnapshotInvalid
	}
	if _, valid := validPriorities[priority]; !valid {
		return automationInvoiceOverdueAction{}, errAutomationActionSnapshotInvalid
	}
	return automationInvoiceOverdueAction{
		InvoiceID: invoiceID, InvoiceNumber: invoiceNumber, ProjectID: projectID,
		Title: title, Description: description, Priority: priority,
		DueDate: dueDate, PlannedDate: plannedDate,
	}, nil
}

func automationSnapshotOptionalString(snapshot map[string]any, key string) (*string, bool) {
	value, exists := snapshot[key]
	if !exists {
		return nil, false
	}
	if value == nil {
		return nil, true
	}
	text, ok := value.(string)
	if !ok {
		return nil, false
	}
	return &text, true
}

func validateAutomationInvoiceOverdueTaskAttempt(
	tx *gorm.DB,
	input automationAttemptInput,
	action automationInvoiceOverdueAction,
) error {
	preset, presetOK := automationPresetByKey(input.Rule.PresetKey)
	if !presetOK || input.Rule.PresetKey != automationPresetInvoiceOverdue || !preset.Available ||
		preset.ID != input.Rule.ID || preset.TriggerType != "event" || preset.ActionType != "task" ||
		!input.Rule.Enabled || input.Rule.Version < 1 || !validCanonicalAutomationUUID(input.Rule.ID) ||
		input.TriggerType != "event" || input.SourceEventID == nil ||
		!validCanonicalAutomationUUID(*input.SourceEventID) || input.ScheduledFor != nil ||
		input.LogicalKey != "event:"+input.Rule.ID+":"+*input.SourceEventID ||
		input.Attempt < 1 || input.Attempt > automationMaxAttempts ||
		input.Config.Priority != action.Priority || input.Config.LocalTime != "" || input.Config.Timezone != "" {
		return errAutomationAttemptContractInvalid
	}
	if (input.Attempt == 1 && input.RetryOfRunID != nil) ||
		(input.Attempt > 1 && (input.RetryOfRunID == nil || !validCanonicalAutomationUUID(*input.RetryOfRunID))) {
		return errAutomationAttemptContractInvalid
	}
	var sourceEvent models.WorkflowEvent
	if err := tx.First(&sourceEvent, "id = ?", *input.SourceEventID).Error; err != nil {
		return fmt.Errorf("%w: source Workflow Event is missing", errAutomationSourceEventInvalid)
	}
	if sourceEvent.AggregateType != "invoice" || sourceEvent.AggregateID != action.InvoiceID ||
		sourceEvent.Action != "invoice_overdue" || sourceEvent.ActorID == nil ||
		*sourceEvent.ActorID != models.BuiltinSystemActorID || sourceEvent.CommandSeq == nil ||
		*sourceEvent.CommandSeq != 1 || sourceEvent.PreviousJSON == nil || sourceEvent.CurrentJSON == nil ||
		sourceEvent.AssignmentID != nil || sourceEvent.SubmissionID != nil ||
		sourceEvent.ArtifactID != nil || sourceEvent.AgentRunID != nil {
		return fmt.Errorf("%w: source Workflow Event contract is invalid", errAutomationSourceEventInvalid)
	}
	if _, err := time.Parse(time.RFC3339Nano, sourceEvent.CreatedAt); err != nil {
		return fmt.Errorf("%w: source Workflow Event timestamp is invalid", errAutomationSourceEventInvalid)
	}
	previous, err := decodeAutomationInvoiceEventSnapshot(*sourceEvent.PreviousJSON)
	if err != nil {
		return fmt.Errorf("%w: previous Invoice snapshot is invalid", errAutomationSourceEventInvalid)
	}
	current, err := decodeAutomationInvoiceEventSnapshot(*sourceEvent.CurrentJSON)
	if err != nil {
		return fmt.Errorf("%w: current Invoice snapshot is invalid", errAutomationSourceEventInvalid)
	}
	if !validAutomationInvoiceEventSnapshot(previous) || !validAutomationInvoiceEventSnapshot(current) ||
		(previous.Status != "sent" && previous.Status != "viewed") || current.Status != "overdue" ||
		current.Version != previous.Version+1 || !sameAutomationInvoiceTransitionFacts(previous, current) {
		return fmt.Errorf("%w: Invoice transition snapshot is invalid", errAutomationSourceEventInvalid)
	}
	if current.InvoiceNumber != action.InvoiceNumber || !sameOptionalString(current.ProjectID, action.ProjectID) ||
		action.Title != automationInvoiceOverdueTaskTitle(current.InvoiceNumber) ||
		action.Description != automationInvoiceOverdueTaskDescription(
			current.InvoiceNumber, current.DueDate, current.AmountMinor, current.Currency,
		) {
		return fmt.Errorf("%w: action does not match the Invoice source snapshot", errAutomationActionSnapshotInvalid)
	}
	return nil
}

func decodeAutomationInvoiceEventSnapshot(raw string) (automationInvoiceEventSnapshot, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields) != 11 {
		return automationInvoiceEventSnapshot{}, errors.New("Invoice event snapshot must contain the exact field set")
	}
	for _, key := range []string{
		"invoice_number", "client_id", "project_id", "amount_minor", "currency", "status",
		"issue_date", "due_date", "paid_date", "notes", "version",
	} {
		if _, ok := fields[key]; !ok {
			return automationInvoiceEventSnapshot{}, errors.New("Invoice event snapshot is missing a required field")
		}
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot automationInvoiceEventSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return automationInvoiceEventSnapshot{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return automationInvoiceEventSnapshot{}, errors.New("Invoice event snapshot contains trailing JSON")
	}
	return snapshot, nil
}

func validAutomationInvoiceEventSnapshot(snapshot automationInvoiceEventSnapshot) bool {
	if strings.TrimSpace(snapshot.InvoiceNumber) != snapshot.InvoiceNumber ||
		len([]rune(snapshot.InvoiceNumber)) < 1 || len([]rune(snapshot.InvoiceNumber)) > 80 ||
		!validCanonicalAutomationUUID(snapshot.ClientID) || snapshot.AmountMinor < 1 ||
		snapshot.AmountMinor > maxInvoiceAmountMinor || snapshot.Version < 1 ||
		!validDate(snapshot.IssueDate) || !validDate(snapshot.DueDate) || snapshot.DueDate < snapshot.IssueDate ||
		snapshot.PaidDate != nil || len([]rune(snapshot.Notes)) > 10_000 {
		return false
	}
	if snapshot.ProjectID != nil && !validCanonicalAutomationUUID(*snapshot.ProjectID) {
		return false
	}
	currency, err := normalizeInvoiceCurrency(snapshot.Currency)
	return err == nil && currency == snapshot.Currency
}

func sameAutomationInvoiceTransitionFacts(previous, current automationInvoiceEventSnapshot) bool {
	return previous.InvoiceNumber == current.InvoiceNumber && previous.ClientID == current.ClientID &&
		sameOptionalString(previous.ProjectID, current.ProjectID) && previous.AmountMinor == current.AmountMinor &&
		previous.Currency == current.Currency && previous.IssueDate == current.IssueDate &&
		previous.DueDate == current.DueDate && previous.PaidDate == nil && current.PaidDate == nil &&
		previous.Notes == current.Notes
}

func recordAutomationTaskCreatedWorkflowEvent(
	tx *gorm.DB,
	task models.Task,
	runID string,
	input automationAttemptInput,
	invoiceID string,
	nowText string,
) error {
	current, err := json.Marshal(map[string]any{
		"id": task.ID, "title": task.Title, "description": task.Description,
		"kind": task.Kind, "status": task.Status, "review_policy": task.ReviewPolicy,
		"priority": task.Priority, "project_id": task.ProjectID,
		"due_date": task.DueDate, "planned_date": task.PlannedDate, "version": task.Version,
		"automation_rule_id": input.Rule.ID, "automation_run_id": runID,
		"automation_preset_key": input.Rule.PresetKey, "source_event_id": input.SourceEventID,
		"invoice_id": invoiceID,
	})
	if err != nil {
		return err
	}
	actorID := models.BuiltinSystemActorID
	commandSequence := 1
	currentText := string(current)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "task", AggregateID: task.ID,
		Action: "task_created_from_automation", ActorID: &actorID, CommandSeq: &commandSequence,
		CurrentJSON: &currentText, CreatedAt: nowText,
	}
	return tx.Create(&event).Error
}

func automationInvoiceOverdueTaskTitle(invoiceNumber string) string {
	value := []rune("跟进逾期发票：" + invoiceNumber)
	if len(value) > 200 {
		value = value[:200]
	}
	return string(value)
}

func automationInvoiceOverdueTaskDescription(invoiceNumber, dueDate string, amountMinor int64, currency string) string {
	return fmt.Sprintf(
		"发票 %s 已逾期（原到期日：%s，金额：%s）。请人工跟进并记录结果；自动化不会发送邮件、发票或客户消息。",
		invoiceNumber, dueDate, formatInvoiceDueAmount(amountMinor, currency),
	)
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
		Joins("JOIN automation_rules AS rule ON rule.id = failed.rule_id").
		Where("failed.status = 'failed' AND failed.retryable = 1 AND failed.retry_at IS NOT NULL AND failed.retry_at <= ?", nowText).
		Where("failed.trigger_type = 'event' OR rule.enabled = 1").
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
		if !rule.Enabled && previous.TriggerType != "event" {
			return newProjectRequestError(http.StatusConflict, "AUTOMATION_RULE_DISABLED", "Enable the automation rule before retrying")
		}
		// A retry is another attempt of the original immutable run. Keep the
		// captured rule version even if its editable configuration has since
		// advanced; config and action snapshots already come from that run.
		rule.Version = previous.RuleVersion
		if previous.TriggerType == "event" {
			// Event runs are retries of an immutable capture. Disabling or
			// editing the rule after capture cannot revoke that delivery.
			rule.Enabled = true
		}
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
	ruleID := strings.TrimSpace(c.Query("rule_id"))
	if ruleID != "" {
		parsed, err := uuid.Parse(ruleID)
		if err != nil || parsed.String() != ruleID {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "rule_id must be a canonical UUID")
			return
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		if status != "succeeded" && status != "failed" && status != "skipped" && status != "cancelled" {
			writeError(c, http.StatusBadRequest, "INVALID_FILTER", "status is invalid")
			return
		}
	}
	var total int64
	var runs []models.AutomationRun
	var data []automationRunOutput
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.AutomationRun{})
		if ruleID != "" {
			query = query.Where("rule_id = ?", ruleID)
		}
		if status != "" {
			query = query.Where("status = ?", status)
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		if err := query.Order("started_at DESC").Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&runs).Error; err != nil {
			return err
		}
		data = make([]automationRunOutput, len(runs))
		for index := range runs {
			output, err := automationRunOutputFromModel(tx, runs[index])
			if err != nil {
				return err
			}
			data[index] = output
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": automationRunListMeta{Page: page, PageSize: pageSize, Total: total}})
}

func (a *API) getAutomationRun(c *gin.Context) {
	id, ok := automationRunID(c)
	if !ok {
		return
	}
	var detail automationRunDetailOutput
	err := a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var run models.AutomationRun
		if err := tx.First(&run, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errAutomationRunNotFound
			}
			return err
		}
		output, err := automationRunDetailOutputFromModel(tx, run)
		if err != nil {
			return err
		}
		detail = output
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		if errors.Is(err, errAutomationRunNotFound) {
			writeError(c, http.StatusNotFound, "AUTOMATION_RUN_NOT_FOUND", "Automation run not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
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
	id := c.Param("id")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		writeError(c, http.StatusBadRequest, "INVALID_AUTOMATION_RUN_ID", "Automation run id must be a canonical UUID")
		return "", false
	}
	return id, true
}

func automationRunDetailOutputFromModel(db *gorm.DB, run models.AutomationRun) (automationRunDetailOutput, error) {
	output, err := automationRunOutputFromModel(db, run)
	if err != nil {
		return automationRunDetailOutput{}, err
	}
	source, err := automationRunSourceFromModel(db, run)
	if err != nil {
		return automationRunDetailOutput{}, err
	}
	var runs []models.AutomationRun
	if err := db.Where("logical_key = ?", run.LogicalKey).Order("attempt ASC").Order("id ASC").Find(&runs).Error; err != nil {
		return automationRunDetailOutput{}, err
	}
	chain := make([]automationRunRetrySummary, len(runs))
	for index := range runs {
		chain[index] = automationRunRetrySummaryFromModel(runs[index])
	}
	return automationRunDetailOutput{automationRunOutput: output, Source: source, RetryChain: chain}, nil
}

func automationRunSourceFromModel(db *gorm.DB, run models.AutomationRun) (automationRunSourceOutput, error) {
	switch run.TriggerType {
	case "schedule":
		return automationRunSourceOutput{
			Kind: "schedule", Available: true, ScheduledFor: normalizeTimestampPointer(run.ScheduledFor),
		}, nil
	case "event":
		source := automationRunSourceOutput{Kind: "event", EventID: run.SourceEventID}
		if run.SourceEventID == nil {
			return source, nil
		}
		var event models.WorkflowEvent
		if err := db.Select("id", "aggregate_type", "aggregate_id", "action", "created_at").First(&event, "id = ?", *run.SourceEventID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return source, nil
			}
			return automationRunSourceOutput{}, err
		}
		source.Available = true
		source.AggregateType = stringPointer(event.AggregateType)
		source.AggregateID = stringPointer(event.AggregateID)
		source.Action = stringPointer(event.Action)
		source.OccurredAt = stringPointer(normalizeTimestamp(event.CreatedAt))
		return source, nil
	default:
		return automationRunSourceOutput{}, errors.New("automation run trigger type is invalid")
	}
}

func automationRunRetrySummaryFromModel(run models.AutomationRun) automationRunRetrySummary {
	return automationRunRetrySummary{
		ID: run.ID, Status: run.Status, Attempt: run.Attempt, RetryOfRunID: run.RetryOfRunID,
		Retryable: run.Retryable, RetryAt: normalizeTimestampPointer(run.RetryAt), ErrorCode: run.ErrorCode,
		ResultType: run.ResultType, ResultID: run.ResultID, ResultSummary: run.ResultSummary,
		StartedAt: normalizeTimestamp(run.StartedAt), EndedAt: normalizeTimestamp(run.EndedAt),
	}
}

func normalizeTimestampPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(normalizeTimestamp(*value))
}

func stringPointer(value string) *string { return &value }

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
