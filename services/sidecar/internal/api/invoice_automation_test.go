package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestInvoiceOverdueAutomationManualCreatesAuditedFollowupTask(t *testing.T) {
	now := time.Date(2026, 9, 2, 9, 15, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"手工逾期客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"手工逾期项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":128045,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID, project.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	rule := configureAndEnableInvoiceAutomation(t, router.Engine, "P0")

	var projectBefore models.Project
	if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project before Invoice Automation: %v", err)
	}
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_overdue"}`, "manual-invoice-overdue")
	if invoice.Status != "overdue" {
		t.Fatalf("manual overdue Invoice = %#v", invoice)
	}

	var source models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", invoice.ID,
	).Take(&source).Error; err != nil {
		t.Fatalf("load manual Invoice overdue event: %v", err)
	}
	var run models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, source.ID).Take(&run).Error; err != nil {
		t.Fatalf("load manual Invoice Automation Run: %v", err)
	}
	if run.Status != "succeeded" || run.Attempt != 1 || run.RuleVersion != rule.Version ||
		run.ResultType == nil || *run.ResultType != "task" || run.ResultID == nil || run.ErrorCode != nil {
		t.Fatalf("manual Invoice Automation Run = %#v", run)
	}
	var config automationConfig
	if err := json.Unmarshal([]byte(run.ConfigSnapshotJSON), &config); err != nil || config.Priority != "P0" {
		t.Fatalf("manual Invoice Automation config = %#v err=%v", config, err)
	}
	var action map[string]any
	if err := json.Unmarshal([]byte(run.ActionSnapshotJSON), &action); err != nil {
		t.Fatalf("decode manual Invoice Automation action: %v", err)
	}
	if len(action) != 12 || action["action_type"] != "task" || action["invoice_id"] != invoice.ID ||
		action["invoice_number"] != invoice.InvoiceNumber || action["project_id"] != project.ID ||
		action["kind"] != "followup" || action["status"] != "todo" || action["review_policy"] != "none" ||
		action["priority"] != "P0" || action["due_date"] != nil || action["planned_date"] != nil {
		t.Fatalf("manual Invoice Automation action = %#v", action)
	}

	var task models.Task
	if err := store.DB.First(&task, "id = ?", *run.ResultID).Error; err != nil {
		t.Fatalf("load automated Invoice followup Task: %v", err)
	}
	if task.Title != automationInvoiceOverdueTaskTitle(invoice.InvoiceNumber) ||
		task.Description != automationInvoiceOverdueTaskDescription(invoice.InvoiceNumber, invoice.DueDate, invoice.AmountMinor, invoice.Currency) ||
		task.Kind != "followup" || task.Status != "todo" || task.ReviewPolicy != "none" || task.Priority != "P0" ||
		task.ProjectID == nil || *task.ProjectID != project.ID || task.DueDate != nil || task.PlannedDate != nil || task.Version != 1 {
		t.Fatalf("automated Invoice followup Task = %#v", task)
	}
	var taskEvent models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'task' AND aggregate_id = ? AND action = 'task_created_from_automation'", task.ID,
	).Take(&taskEvent).Error; err != nil {
		t.Fatalf("load automated Task Workflow Event: %v", err)
	}
	if taskEvent.ActorID == nil || *taskEvent.ActorID != models.BuiltinSystemActorID || taskEvent.PreviousJSON != nil ||
		taskEvent.CurrentJSON == nil || taskEvent.CommandSeq == nil || *taskEvent.CommandSeq != 1 {
		t.Fatalf("automated Task Workflow Event = %#v", taskEvent)
	}
	var taskEventCurrent map[string]any
	if err := json.Unmarshal([]byte(*taskEvent.CurrentJSON), &taskEventCurrent); err != nil ||
		taskEventCurrent["automation_run_id"] != run.ID || taskEventCurrent["source_event_id"] != source.ID ||
		taskEventCurrent["invoice_id"] != invoice.ID {
		t.Fatalf("automated Task event current = %#v err=%v", taskEventCurrent, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_run' AND aggregate_id = ? AND action = 'automation_run_succeeded'", 1, run.ID)

	var projectAfter models.Project
	if err := store.DB.First(&projectAfter, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after Invoice Automation: %v", err)
	}
	if projectAfter.Version != projectBefore.Version+1 {
		t.Fatalf("Project version after automated Task = %d, want %d", projectAfter.Version, projectBefore.Version+1)
	}
	var invoiceModel models.Invoice
	if err := store.DB.First(&invoiceModel, "id = ?", invoice.ID).Error; err != nil {
		t.Fatalf("load overdue Invoice model: %v", err)
	}
	if err := executeInvoiceOverdueAutomations(store.DB, source.ID, invoiceModel, source.CreatedAt); err != nil {
		t.Fatalf("replay Invoice overdue Automation: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ?", 1, rule.ID, source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id = ?", 1, task.ID)
}

func TestInvoiceOverdueAutomationRuntimeUsesEventDedupeAndNullableProject(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"运行时逾期客户"}`, nil)

	disabledInvoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":5000,"currency":"USD","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID,
	), nil)
	disabledInvoice = transitionInvoiceForTest(t, router.Engine, disabledInvoice, `{"action":"mark_sent"}`, "")
	disabledInvoice = transitionInvoiceForTest(t, router.Engine, disabledInvoice, `{"action":"mark_overdue"}`, "disabled-invoice-overdue")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 0)

	rule := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	runtimeInvoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":9900,"currency":"USD","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID,
	), nil)
	runtimeInvoice = transitionInvoiceForTest(t, router.Engine, runtimeInvoice, `{"action":"mark_sent"}`, "")
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("project runtime overdue Invoice: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("repeat runtime overdue Invoice projection: %v", err)
	}

	var source models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", runtimeInvoice.ID,
	).Take(&source).Error; err != nil {
		t.Fatalf("load runtime Invoice overdue event: %v", err)
	}
	var run models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, source.ID).Take(&run).Error; err != nil {
		t.Fatalf("load runtime Invoice Automation Run: %v", err)
	}
	var task models.Task
	if run.ResultID == nil {
		t.Fatalf("runtime Invoice Automation Run has no Task: %#v", run)
	}
	if err := store.DB.First(&task, "id = ?", *run.ResultID).Error; err != nil {
		t.Fatalf("load runtime Invoice followup Task: %v", err)
	}
	if run.Status != "succeeded" || task.ProjectID != nil || task.DueDate != nil || task.PlannedDate != nil {
		t.Fatalf("runtime nullable-project result: run=%#v task=%#v", run, task)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 1, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE source_event_id IN (SELECT id FROM workflow_events WHERE aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue')", 0, disabledInvoice.ID)
}

func TestInvoiceOverdueAutomationRejectsCorruptRuleEventAndActionSnapshots(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	ruleOutput := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "id = ?", ruleOutput.ID).Error; err != nil {
		t.Fatalf("load Invoice Automation Rule: %v", err)
	}

	tests := []struct {
		name      string
		corrupt   func(*automationAttemptInput)
		badSource bool
		wantCode  string
	}{
		{
			name: "action snapshot", wantCode: "ACTION_SNAPSHOT_INVALID",
			corrupt: func(input *automationAttemptInput) { input.ActionSnapshot["title"] = "被篡改的任务标题" },
		},
		{
			name: "action type binding", wantCode: "ACTION_SNAPSHOT_INVALID",
			corrupt: func(input *automationAttemptInput) { input.ActionSnapshot["action_type"] = "reminder" },
		},
		{name: "source event snapshot", badSource: true, wantCode: "SOURCE_EVENT_INVALID"},
		{
			name: "rule binding", wantCode: "ATTEMPT_CONTRACT_INVALID",
			corrupt: func(input *automationAttemptInput) { input.Rule.PresetKey = automationPresetProjectCompleted },
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoice, eventID := seedInvoiceAutomationSourceEvent(t, store.DB, index, test.badSource, now)
			sourceEventID := eventID
			input := automationAttemptInput{
				Rule: rule, TriggerType: "event", SourceEventID: &sourceEventID,
				LogicalKey: "event:" + rule.ID + ":" + eventID, Attempt: 1,
				Config:         automationConfig{Priority: "P1"},
				ActionSnapshot: automationInvoiceOverdueActionSnapshot(invoice, "P1"), Now: now,
			}
			if test.corrupt != nil {
				test.corrupt(&input)
			}
			var run models.AutomationRun
			err := store.DB.Transaction(func(tx *gorm.DB) error {
				var err error
				run, err = executeAutomationAttempt(tx, input)
				return err
			})
			if err != nil {
				t.Fatalf("execute corrupt Invoice Automation attempt: %v", err)
			}
			if run.Status != "failed" || run.ErrorCode == nil || *run.ErrorCode != test.wantCode ||
				run.Retryable || run.RetryAt != nil || run.ResultID != nil || run.ResultType != nil {
				t.Fatalf("corrupt Invoice Automation Run = %#v", run)
			}
		})
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestInvoiceOverdueAutomationRollsBackPartialTaskAndRetryReusesSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"逾期动作失败客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"逾期动作失败项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":8800,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID, project.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	rule := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	var projectBefore models.Project
	if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project before partial action: %v", err)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_invoice_automation_task_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'task' AND NEW.action = 'task_created_from_automation'
		BEGIN SELECT RAISE(ABORT, 'TEST_INVOICE_AUTOMATION_TASK_EVENT_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install automated Task event failure: %v", err)
	}
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_overdue"}`, "partial-invoice-overdue")
	if invoice.Status != "overdue" {
		t.Fatalf("Invoice source rolled back after partial Task action: %#v", invoice)
	}
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'failed'", rule.ID).Take(&failed).Error; err != nil {
		t.Fatalf("load partial Task failed Run: %v", err)
	}
	if failed.ErrorCode == nil || *failed.ErrorCode != "ACTION_WRITE_FAILED" || !failed.Retryable || failed.RetryAt == nil {
		t.Fatalf("partial Task failed Run = %#v", failed)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND action = 'task_created_from_automation'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", 1, invoice.ID)
	var projectAfterFailure models.Project
	if err := store.DB.First(&projectAfterFailure, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after rolled-back Task: %v", err)
	}
	if projectAfterFailure.Version != projectBefore.Version || projectAfterFailure.UpdatedAt != projectBefore.UpdatedAt {
		t.Fatalf("Task insert trigger leaked through action rollback: before=%#v after=%#v", projectBefore, projectAfterFailure)
	}

	if err := store.DB.Exec("DROP TRIGGER fail_invoice_automation_task_event").Error; err != nil {
		t.Fatalf("remove automated Task event failure: %v", err)
	}
	updated := performRequest(
		router.Engine, http.MethodPatch, "/api/v1/automations/rules/"+rule.ID,
		[]byte(`{"config":{"priority":"P3"}}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("update Invoice Automation after failed Run = %d: %s", updated.Code, updated.Body.String())
	}
	retried := performRequest(router.Engine, http.MethodPost, "/api/v1/automations/runs/"+failed.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry Invoice Automation = %d: %s", retried.Code, retried.Body.String())
	}
	var retry models.AutomationRun
	if err := store.DB.Where("retry_of_run_id = ?", failed.ID).Take(&retry).Error; err != nil {
		t.Fatalf("load retried Invoice Automation Run: %v", err)
	}
	if retry.Status != "succeeded" || retry.Attempt != 2 || retry.RuleVersion != failed.RuleVersion ||
		retry.ConfigSnapshotJSON != failed.ConfigSnapshotJSON || retry.ActionSnapshotJSON != failed.ActionSnapshotJSON ||
		retry.ResultID == nil {
		t.Fatalf("retried Invoice Automation Run = %#v", retry)
	}
	var task models.Task
	if err := store.DB.First(&task, "id = ?", *retry.ResultID).Error; err != nil {
		t.Fatalf("load retried Invoice followup Task: %v", err)
	}
	if task.Priority != "P1" || task.ProjectID == nil || *task.ProjectID != project.ID || task.DueDate != nil || task.PlannedDate != nil {
		t.Fatalf("retry did not reuse immutable Task snapshot: %#v", task)
	}
	var projectAfterRetry models.Project
	if err := store.DB.First(&projectAfterRetry, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after successful retry: %v", err)
	}
	if projectAfterRetry.Version != projectBefore.Version+1 {
		t.Fatalf("Project version after successful retry = %d, want %d", projectAfterRetry.Version, projectBefore.Version+1)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 1)
}

func TestInvoiceOverdueAutomationInfrastructureFailureIsIsolatedFromInvoice(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"逾期隔离客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"逾期隔离项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":7600,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID, project.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	rule := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	var projectBefore models.Project
	if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project before isolated failure: %v", err)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_invoice_automation_run
		BEFORE INSERT ON automation_runs
		WHEN NEW.rule_id = '` + rule.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_INVOICE_AUTOMATION_RUN_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install Invoice Automation infrastructure failure: %v", err)
	}
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_overdue"}`, "isolated-invoice-overdue")
	if invoice.Status != "overdue" {
		t.Fatalf("Invoice source rolled back after Automation infrastructure failure: %#v", invoice)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND action = 'task_created_from_automation'", 0)
	var projectAfter models.Project
	if err := store.DB.First(&projectAfter, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after isolated failure: %v", err)
	}
	if projectAfter.Version != projectBefore.Version || projectAfter.UpdatedAt != projectBefore.UpdatedAt {
		t.Fatalf("Automation infrastructure failure polluted Project: before=%#v after=%#v", projectBefore, projectAfter)
	}
}

func TestInvoiceOverdueAutomationRuntimeInfrastructureFailureIsIsolatedFromInvoice(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"运行时逾期隔离客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"运行时逾期隔离项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":8100,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID, project.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	rule := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")
	var projectBefore models.Project
	if err := store.DB.First(&projectBefore, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project before runtime isolated failure: %v", err)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_runtime_invoice_automation_run
		BEFORE INSERT ON automation_runs
		WHEN NEW.rule_id = '` + rule.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_RUNTIME_INVOICE_AUTOMATION_RUN_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install runtime Invoice Automation infrastructure failure: %v", err)
	}
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatalf("runtime Invoice projection failed with isolated Automation error: %v", err)
	}
	var persisted models.Invoice
	if err := store.DB.First(&persisted, "id = ?", invoice.ID).Error; err != nil {
		t.Fatalf("load runtime overdue Invoice after isolated failure: %v", err)
	}
	if persisted.Status != "overdue" || persisted.Version != invoice.Version+1 {
		t.Fatalf("runtime Invoice source changed incorrectly: before=%#v after=%#v", invoice, persisted)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'invoice_due' AND source_entity_id = ?", 1, invoice.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 0)
	var projectAfter models.Project
	if err := store.DB.First(&projectAfter, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after runtime isolated failure: %v", err)
	}
	if projectAfter.Version != projectBefore.Version || projectAfter.UpdatedAt != projectBefore.UpdatedAt {
		t.Fatalf("runtime Automation failure polluted Project: before=%#v after=%#v", projectBefore, projectAfter)
	}
}

func configureAndEnableInvoiceAutomation(t *testing.T, router http.Handler, priority string) automationRuleOutput {
	t.Helper()
	rule := automationRuleByPreset(t, router, automationPresetInvoiceOverdue)
	if priority != rule.Config.Priority {
		updated := performRequest(
			router, http.MethodPatch, "/api/v1/automations/rules/"+rule.ID,
			[]byte(fmt.Sprintf(`{"config":{"priority":%q}}`, priority)),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
		)
		if updated.Code != http.StatusOK {
			t.Fatalf("configure Invoice Automation = %d: %s", updated.Code, updated.Body.String())
		}
		var envelope automationRuleEnvelope
		if err := json.Unmarshal(updated.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode configured Invoice Automation: %v", err)
		}
		rule = envelope.Data
	}
	enabled := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
	)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable Invoice Automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	var envelope automationRuleEnvelope
	if err := json.Unmarshal(enabled.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode enabled Invoice Automation: %v", err)
	}
	return envelope.Data
}

func seedInvoiceAutomationSourceEvent(
	t *testing.T,
	db *gorm.DB,
	index int,
	corruptCurrent bool,
	now time.Time,
) (models.Invoice, string) {
	t.Helper()
	current := models.Invoice{
		ID: uuid.NewString(), InvoiceNumber: fmt.Sprintf("INV-AUTOMATION-%03d", index), ClientID: uuid.NewString(),
		AmountMinor: int64(10_000 + index), Currency: "CNY", Status: "overdue",
		IssueDate: "2026-08-01", DueDate: "2026-09-01", Version: 3,
		CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	previous := current
	previous.Status = "viewed"
	previous.Version = 2
	previousJSON, err := json.Marshal(invoiceEventState(previous))
	if err != nil {
		t.Fatalf("encode previous Invoice Automation source: %v", err)
	}
	currentState := invoiceEventState(current)
	if corruptCurrent {
		currentState["unexpected"] = true
	}
	currentJSON, err := json.Marshal(currentState)
	if err != nil {
		t.Fatalf("encode current Invoice Automation source: %v", err)
	}
	previousText, currentText := string(previousJSON), string(currentJSON)
	actorID := models.BuiltinSystemActorID
	sequence := 1
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "invoice", AggregateID: current.ID, Action: "invoice_overdue",
		ActorID: &actorID, CommandSeq: &sequence, PreviousJSON: &previousText, CurrentJSON: &currentText,
		CreatedAt: now.Format(time.RFC3339Nano),
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seed Invoice Automation source event: %v", err)
	}
	return current, event.ID
}
