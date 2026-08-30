package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAutomationEventDeliveryRecoversUsingCapturedSnapshotAfterRuleDisabled(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	client := createClientForTest(t, router.Engine, `{"name":"持久投递客户"}`, nil)
	project := createProjectForTest(t, router.Engine, fmt.Sprintf(`{"name":"持久投递项目","client_id":%q}`, client.ID), nil)
	invoice := createInvoiceForTest(t, router.Engine, fmt.Sprintf(
		`{"client_id":%q,"project_id":%q,"amount_minor":7200,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-09-01"}`,
		client.ID, project.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_sent"}`, "")
	capturedRule := configureAndEnableInvoiceAutomation(t, router.Engine, "P1")

	if err := store.DB.Exec(`
		CREATE TRIGGER reject_captured_event_automation_run
		BEFORE INSERT ON automation_runs
		WHEN NEW.rule_id = '` + capturedRule.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_CAPTURED_EVENT_RUN_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install Automation Run failure: %v", err)
	}
	invoice = transitionInvoiceForTest(t, router.Engine, invoice, `{"action":"mark_overdue"}`, "captured-event-overdue")
	if invoice.Status != "overdue" {
		t.Fatalf("Invoice source did not commit: %#v", invoice)
	}
	var source models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'invoice' AND aggregate_id = ? AND action = 'invoice_overdue'", invoice.ID,
	).Take(&source).Error; err != nil {
		t.Fatalf("load captured source event: %v", err)
	}
	var pending models.AutomationEventDelivery
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", capturedRule.ID, source.ID).Take(&pending).Error; err != nil {
		t.Fatalf("load pending Automation event delivery: %v", err)
	}
	if pending.DeliveryAttempts != 1 || pending.LastErrorCode == nil ||
		*pending.LastErrorCode != "DELIVERY_PROCESSING_FAILED" || pending.LastErrorAt == nil {
		t.Fatalf("pending delivery backoff = %#v", pending)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, capturedRule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 0)

	if err := store.DB.Exec("DROP TRIGGER reject_captured_event_automation_run").Error; err != nil {
		t.Fatalf("remove Automation Run failure: %v", err)
	}
	updatedResponse := performRequest(
		router.Engine, http.MethodPatch, "/api/v1/automations/rules/"+capturedRule.ID,
		[]byte(`{"config":{"priority":"P3"}}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, capturedRule.Version)},
	)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("edit captured rule = %d: %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated automationRuleEnvelope
	if err := json.Unmarshal(updatedResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode edited rule: %v", err)
	}
	disabledResponse := performRequest(
		router.Engine, http.MethodPost, "/api/v1/automations/rules/"+capturedRule.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, updated.Data.Version)},
	)
	if disabledResponse.Code != http.StatusOK {
		t.Fatalf("disable captured rule = %d: %s", disabledResponse.Code, disabledResponse.Body.String())
	}

	now = now.Add(automationDeliveryInitialBackoff - time.Nanosecond)
	if err := service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatalf("premature delivery scan: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, capturedRule.ID)
	now = now.Add(time.Nanosecond)
	if err := service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatalf("recover captured delivery: %v", err)
	}
	var run models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", capturedRule.ID, source.ID).Take(&run).Error; err != nil {
		t.Fatalf("load recovered Automation Run: %v", err)
	}
	if run.Status != "succeeded" || run.RuleVersion != capturedRule.Version ||
		run.ConfigSnapshotJSON != pending.ConfigSnapshotJSON || run.ActionSnapshotJSON != pending.ActionSnapshotJSON ||
		run.ResultID == nil {
		t.Fatalf("recovered run did not use captured snapshots: %#v", run)
	}
	var task models.Task
	if err := store.DB.First(&task, "id = ?", *run.ResultID).Error; err != nil {
		t.Fatalf("load recovered Task: %v", err)
	}
	if task.Priority != "P1" {
		t.Fatalf("recovered Task priority = %q, want captured P1", task.Priority)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)

	if err := service.consumeDueAutomationEventDeliveries(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatalf("repeat delivery scan: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ?", 1, capturedRule.ID, source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE kind = 'followup'", 1)
}

func TestAutomationEventDeliveryStartupDrainsPendingCapture(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-delivery-startup.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 3, 9, 30, 0, 0, time.UTC)
	seedService := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := seedService.ensureAutomationRules(now); err != nil {
		t.Fatalf("ensure Automation rules: %v", err)
	}
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "preset_key = ?", automationPresetProjectCompleted).Error; err != nil {
		t.Fatalf("load project completion rule: %v", err)
	}
	if err := store.DB.Model(&models.AutomationRule{}).Where("id = ? AND version = ?", rule.ID, rule.Version).
		Updates(map[string]any{
			"enabled": true, "version": rule.Version + 1,
			"updated_at": formatInboxTimestamp(now.Add(time.Nanosecond)),
		}).Error; err != nil {
		t.Fatalf("enable project completion rule: %v", err)
	}
	project := models.Project{ID: uuid.NewString(), Name: "启动补偿项目", Status: "completed", Version: 2}
	currentJSON, err := json.Marshal(map[string]any{
		"id": project.ID, "name": project.Name, "status": project.Status,
	})
	if err != nil {
		t.Fatalf("encode startup project event: %v", err)
	}
	currentText := string(currentJSON)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "project", AggregateID: project.ID,
		Action: "project_completed", CurrentJSON: &currentText, CreatedAt: formatInboxTimestamp(now),
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return enqueueProjectCompletionAutomationDelivery(tx, event.ID, project, event.CreatedAt)
	}); err != nil {
		t.Fatalf("seed startup delivery: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 1)

	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1,
		ReminderScanInterval: -1, AutomationDeliveryScanInterval: -1,
		DiskSpaceScanInterval: -1, ScheduledBackupScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter startup delivery compensation: %v", err)
	}
	defer router.Close()
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ? AND status = 'succeeded'", 1, rule.ID, event.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
}

func TestAutomationEventDeliveryReplayKeepsOriginalPendingSnapshotAfterRuleEdit(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 45, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	rule := enableProjectCompletionAutomationForDeliveryTest(t, router.Engine)
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_pending_replay_automation_run
		BEFORE INSERT ON automation_runs
		WHEN NEW.rule_id = '` + rule.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_PENDING_REPLAY_RUN_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install pending replay failure: %v", err)
	}
	project := createProjectForTest(t, router.Engine, `{"name":"投递重放项目"}`, nil)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"complete"}`)
	var source models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'", project.ID,
	).Take(&source).Error; err != nil {
		t.Fatalf("load pending replay source: %v", err)
	}
	var original models.AutomationEventDelivery
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, source.ID).Take(&original).Error; err != nil {
		t.Fatalf("load original pending delivery: %v", err)
	}
	updatedResponse := performRequest(
		router.Engine, http.MethodPatch, "/api/v1/automations/rules/"+rule.ID,
		[]byte(`{"config":{"priority":"P3"}}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
	)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("edit pending replay rule = %d: %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var projectModel models.Project
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load pending replay Project: %v", err)
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		return enqueueProjectCompletionAutomationDelivery(tx, source.ID, projectModel, source.CreatedAt)
	}); err != nil {
		t.Fatalf("replay original pending delivery after rule edit: %v", err)
	}
	var replayed models.AutomationEventDelivery
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, source.ID).Take(&replayed).Error; err != nil {
		t.Fatalf("reload replayed pending delivery: %v", err)
	}
	if replayed.ID != original.ID || replayed.RuleVersion != original.RuleVersion ||
		replayed.ConfigSnapshotJSON != original.ConfigSnapshotJSON ||
		replayed.ActionSnapshotJSON != original.ActionSnapshotJSON ||
		replayed.DeliveryAttempts != original.DeliveryAttempts || replayed.AvailableAt != original.AvailableAt {
		t.Fatalf("pending replay replaced the authoritative capture: before=%#v after=%#v", original, replayed)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries WHERE rule_id = ? AND source_event_id = ?", 1, rule.ID, source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ?", 0, rule.ID, source.ID)
}

func TestAutomationEventDeliveryReplayDoesNotRecaptureCompletedRunAfterRuleEdit(t *testing.T) {
	now := time.Date(2026, 9, 3, 9, 50, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	rule := enableProjectCompletionAutomationForDeliveryTest(t, router.Engine)
	project := createProjectForTest(t, router.Engine, `{"name":"已完成投递重放项目"}`, nil)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"complete"}`)
	var source models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'", project.ID,
	).Take(&source).Error; err != nil {
		t.Fatalf("load completed replay source: %v", err)
	}
	var originalRun models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ? AND attempt = 1", rule.ID, source.ID).Take(&originalRun).Error; err != nil {
		t.Fatalf("load completed replay Run: %v", err)
	}
	updatedResponse := performRequest(
		router.Engine, http.MethodPatch, "/api/v1/automations/rules/"+rule.ID,
		[]byte(`{"config":{"priority":"P3"}}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
	)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("edit completed replay rule = %d: %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var projectModel models.Project
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load completed replay Project: %v", err)
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		return enqueueProjectCompletionAutomationDelivery(tx, source.ID, projectModel, source.CreatedAt)
	}); err != nil {
		t.Fatalf("replay completed Automation event after rule edit: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries WHERE rule_id = ? AND source_event_id = ?", 0, rule.ID, source.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ? AND attempt = 1", 1, rule.ID, source.ID)
	var retained models.AutomationRun
	if err := store.DB.First(&retained, "id = ?", originalRun.ID).Error; err != nil {
		t.Fatalf("reload completed replay Run: %v", err)
	}
	if retained.RuleVersion != originalRun.RuleVersion ||
		retained.ConfigSnapshotJSON != originalRun.ConfigSnapshotJSON ||
		retained.ActionSnapshotJSON != originalRun.ActionSnapshotJSON {
		t.Fatalf("completed replay changed original Run: before=%#v after=%#v", originalRun, retained)
	}
}

func TestAutomationEventDeliveryBatchDoesNotStarveBehindConflictingRun(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-delivery-batch.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.ensureAutomationRules(now); err != nil {
		t.Fatalf("ensure Automation rules: %v", err)
	}
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "preset_key = ?", automationPresetProjectCompleted).Error; err != nil {
		t.Fatalf("load project completion rule: %v", err)
	}
	if err := store.DB.Model(&models.AutomationRule{}).Where("id = ? AND version = ?", rule.ID, rule.Version).
		Updates(map[string]any{
			"enabled": true, "version": rule.Version + 1,
			"updated_at": formatInboxTimestamp(now.Add(time.Nanosecond)),
		}).Error; err != nil {
		t.Fatalf("enable project completion rule: %v", err)
	}
	if err := store.DB.First(&rule, "id = ?", rule.ID).Error; err != nil {
		t.Fatalf("reload enabled rule: %v", err)
	}
	config, err := decodeAutomationConfig(rule.PresetKey, rule.ConfigJSON)
	if err != nil {
		t.Fatalf("decode project completion config: %v", err)
	}
	configJSON, err := encodeAutomationConfig(config)
	if err != nil {
		t.Fatalf("encode project completion config: %v", err)
	}
	capturedAt := formatInboxTimestamp(now)
	var conflictDelivery models.AutomationEventDelivery
	for index := 0; index < automationEventDeliveryBatchSize+1; index++ {
		projectID := uuid.NewString()
		eventID := uuid.NewString()
		projectName := fmt.Sprintf("批量交付项目 %03d", index)
		currentJSON, err := json.Marshal(map[string]any{
			"id": projectID, "name": projectName, "status": "completed",
		})
		if err != nil {
			t.Fatalf("encode project event %d: %v", index, err)
		}
		currentText := string(currentJSON)
		event := models.WorkflowEvent{
			ID: eventID, AggregateType: "project", AggregateID: projectID,
			Action: "project_completed", CurrentJSON: &currentText, CreatedAt: capturedAt,
		}
		if err := store.DB.Create(&event).Error; err != nil {
			t.Fatalf("seed project event %d: %v", index, err)
		}
		actionJSON, err := json.Marshal(map[string]any{
			"action_type": "inbox_item", "project_id": projectID, "project_name": projectName,
			"title": automationProjectCompletionTitle(projectName), "priority": config.Priority,
		})
		if err != nil {
			t.Fatalf("encode action %d: %v", index, err)
		}
		delivery := models.AutomationEventDelivery{
			ID: fmt.Sprintf("delivery-%03d", index), RuleID: rule.ID, PresetKey: rule.PresetKey,
			RuleVersion: rule.Version, SourceEventID: eventID,
			LogicalKey:         "event:" + rule.ID + ":" + eventID,
			ConfigSnapshotJSON: configJSON, ActionSnapshotJSON: string(actionJSON),
			AvailableAt: capturedAt, CapturedAt: capturedAt, UpdatedAt: capturedAt,
		}
		if err := store.DB.Create(&delivery).Error; err != nil {
			t.Fatalf("seed delivery %d: %v", index, err)
		}
		if index == 0 {
			conflictDelivery = delivery
		}
	}
	conflictCode := "TEST_CONFLICT"
	conflictSourceID := conflictDelivery.SourceEventID
	conflictingRun := models.AutomationRun{
		ID: uuid.NewString(), RuleID: rule.ID, RuleVersion: rule.Version,
		TriggerType: "event", SourceEventID: &conflictSourceID,
		LogicalKey: conflictDelivery.LogicalKey, DedupeKey: conflictDelivery.LogicalKey + ":attempt:1",
		Status: "failed", Attempt: 1, Retryable: false,
		ConfigSnapshotJSON: `{"priority":"P3"}`,
		ActionSnapshotJSON: conflictDelivery.ActionSnapshotJSON,
		ErrorCode:          &conflictCode, ResultSummary: "conflicting pre-existing run",
		StartedAt: capturedAt, EndedAt: capturedAt,
	}
	if err := store.DB.Create(&conflictingRun).Error; err != nil {
		t.Fatalf("seed conflicting Automation Run: %v", err)
	}

	if err := service.consumeDueAutomationEventDeliveries(context.Background(), now); err == nil {
		t.Fatal("batch scan unexpectedly accepted a conflicting existing Run")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE status = 'succeeded'", 99)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 2)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 99)
	var retained models.AutomationEventDelivery
	if err := store.DB.First(&retained, "id = ?", conflictDelivery.ID).Error; err != nil {
		t.Fatalf("load retained conflicting delivery: %v", err)
	}
	if retained.DeliveryAttempts != 1 || retained.LastErrorCode == nil || retained.LastErrorAt == nil ||
		retained.AvailableAt != formatInboxTimestamp(now.Add(automationDeliveryInitialBackoff)) {
		t.Fatalf("retained conflicting delivery = %#v", retained)
	}

	if err := service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatalf("drain 101st delivery: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE status = 'succeeded'", 100)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 100)
}

func TestAutomationEventDeliveryConcurrentConsumersCommitOneAttempt(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-delivery-concurrent.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC)
	seedService := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := seedService.ensureAutomationRules(now); err != nil {
		t.Fatalf("ensure Automation rules: %v", err)
	}
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "preset_key = ?", automationPresetProjectCompleted).Error; err != nil {
		t.Fatalf("load project completion rule: %v", err)
	}
	if err := store.DB.Model(&models.AutomationRule{}).Where("id = ? AND version = ?", rule.ID, rule.Version).
		Updates(map[string]any{
			"enabled": true, "version": rule.Version + 1,
			"updated_at": formatInboxTimestamp(now.Add(time.Nanosecond)),
		}).Error; err != nil {
		t.Fatalf("enable project completion rule: %v", err)
	}
	project := models.Project{ID: uuid.NewString(), Name: "并发消费项目", Status: "completed", Version: 2}
	currentJSON, err := json.Marshal(map[string]any{
		"id": project.ID, "name": project.Name, "status": project.Status,
	})
	if err != nil {
		t.Fatalf("encode concurrent source event: %v", err)
	}
	currentText := string(currentJSON)
	event := models.WorkflowEvent{
		ID: uuid.NewString(), AggregateType: "project", AggregateID: project.ID,
		Action: "project_completed", CurrentJSON: &currentText, CreatedAt: formatInboxTimestamp(now),
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return enqueueProjectCompletionAutomationDelivery(tx, event.ID, project, event.CreatedAt)
	}); err != nil {
		t.Fatalf("seed concurrent delivery: %v", err)
	}

	consumers := []*API{
		{db: store.DB, options: Options{Now: func() time.Time { return now }}},
		{db: store.DB, options: Options{Now: func() time.Time { return now }}},
	}
	start := make(chan struct{})
	errorsByConsumer := make([]error, len(consumers))
	var wait sync.WaitGroup
	for index := range consumers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			errorsByConsumer[index] = consumers[index].consumeDueAutomationEventDeliveries(context.Background(), now)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, consumeErr := range errorsByConsumer {
		if consumeErr != nil {
			t.Fatalf("consumer %d: %v", index, consumeErr)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND source_event_id = ? AND attempt = 1", 1, rule.ID, event.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_run' AND action = 'automation_run_succeeded'", 1)
}

func TestDisabledEventAutomationRunStillRetriesWhenDue(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 45, 0, 0, time.UTC)
	router, service, store := newInvoiceDueTestAPI(t, &now)
	rule := enableProjectCompletionAutomationForDeliveryTest(t, router.Engine)
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_due_event_retry_inbox
		BEFORE INSERT ON inbox_items
		WHEN NEW.source_entity_type = 'automation'
		BEGIN SELECT RAISE(ABORT, 'TEST_DUE_EVENT_ACTION_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install due event action failure: %v", err)
	}
	project := createProjectForTest(t, router.Engine, `{"name":"禁用后到期重试项目"}`, nil)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"complete"}`)
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'failed' AND attempt = 1", rule.ID).Take(&failed).Error; err != nil {
		t.Fatalf("load failed event Run: %v", err)
	}
	if !failed.Retryable || failed.RetryAt == nil {
		t.Fatalf("failed event Run is not retryable: %#v", failed)
	}
	if err := store.DB.Exec("DROP TRIGGER reject_due_event_retry_inbox").Error; err != nil {
		t.Fatalf("remove due event action failure: %v", err)
	}
	currentRule := automationRuleByPreset(t, router.Engine, automationPresetProjectCompleted)
	disabled := performRequest(
		router.Engine, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentRule.Version)},
	)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable due event rule = %d: %s", disabled.Code, disabled.Body.String())
	}
	now = now.Add(time.Minute)
	if err := service.projectDueAutomationRetries(now); err != nil {
		t.Fatalf("project due disabled event retry: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE retry_of_run_id = ? AND attempt = 2 AND status = 'succeeded'", 1, failed.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
}

func TestProjectCompletionRollsBackWhenAutomationDeliveryCaptureFails(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 50, 0, 0, time.UTC)
	router, _, store := newInvoiceDueTestAPI(t, &now)
	rule := enableProjectCompletionAutomationForDeliveryTest(t, router.Engine)
	project := createProjectForTest(t, router.Engine, `{"name":"投递捕获回滚项目"}`, nil)
	project = transitionProjectForTest(t, router.Engine, project.ID, project.Version, `{"action":"start"}`)
	var before models.Project
	if err := store.DB.First(&before, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project before capture failure: %v", err)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_project_automation_delivery_capture
		BEFORE INSERT ON automation_event_deliveries
		WHEN NEW.rule_id = '` + rule.ID + `'
		BEGIN SELECT RAISE(ABORT, 'TEST_AUTOMATION_DELIVERY_CAPTURE_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install delivery capture failure: %v", err)
	}
	response := performRequest(
		router.Engine, http.MethodPost, "/api/v1/projects/"+project.ID+"/transitions",
		[]byte(`{"action":"complete"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, before.Version)},
	)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("Project completion with capture failure = %d: %s", response.Code, response.Body.String())
	}
	var after models.Project
	if err := store.DB.First(&after, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load Project after capture failure: %v", err)
	}
	if after.Status != before.Status || after.Version != before.Version || after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("Project changed despite capture rollback: before=%#v after=%#v", before, after)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'", 0, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_event_deliveries WHERE rule_id = ?", 0, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, rule.ID)
}

func TestDisabledScheduleAutomationRunDoesNotRetry(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-disabled-schedule-retry.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 3, 11, 0, 0, 0, time.UTC)
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.ensureAutomationRules(now); err != nil {
		t.Fatalf("ensure Automation rules: %v", err)
	}
	var rule models.AutomationRule
	if err := store.DB.First(&rule, "preset_key = ?", automationPresetDailyToday).Error; err != nil {
		t.Fatalf("load disabled schedule rule: %v", err)
	}
	if rule.Enabled {
		t.Fatal("schedule retry test requires a disabled rule")
	}
	scheduledFor := formatInboxTimestamp(now.Add(-time.Minute))
	logicalKey := "schedule:" + rule.ID + ":" + scheduledFor
	actionJSON, err := json.Marshal(automationScheduleActionSnapshot(rule.PresetKey))
	if err != nil {
		t.Fatalf("encode schedule action: %v", err)
	}
	errorCode := "ACTION_WRITE_FAILED"
	retryAt := formatInboxTimestamp(now.Add(-time.Second))
	run := models.AutomationRun{
		ID: uuid.NewString(), RuleID: rule.ID, RuleVersion: rule.Version,
		TriggerType: "schedule", ScheduledFor: &scheduledFor,
		LogicalKey: logicalKey, DedupeKey: logicalKey + ":attempt:1",
		Status: "failed", Attempt: 1, Retryable: true, RetryAt: &retryAt,
		ConfigSnapshotJSON: rule.ConfigJSON, ActionSnapshotJSON: string(actionJSON),
		ErrorCode: &errorCode, ResultSummary: "schedule action failed",
		StartedAt: scheduledFor, EndedAt: scheduledFor,
	}
	if err := store.DB.Create(&run).Error; err != nil {
		t.Fatalf("seed failed schedule run: %v", err)
	}
	if err := service.projectDueAutomationRetries(now); err != nil {
		t.Fatalf("project disabled schedule retries: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE retry_of_run_id = ?", 0, run.ID)
	retryErr := service.retryAutomationRunByID(run.ID, now)
	var requestErr *projectRequestError
	if !errors.As(retryErr, &requestErr) || requestErr.code != "AUTOMATION_RULE_DISABLED" {
		t.Fatalf("disabled schedule retry error = %v", retryErr)
	}
}

func enableProjectCompletionAutomationForDeliveryTest(t *testing.T, router http.Handler) automationRuleOutput {
	t.Helper()
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	response := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("enable project completion Automation = %d: %s", response.Code, response.Body.String())
	}
	var envelope automationRuleEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode enabled project completion Automation: %v", err)
	}
	return envelope.Data
}
