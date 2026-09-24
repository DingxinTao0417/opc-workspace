package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func enableAgentFailureAutomationForTest(t *testing.T, f aiAgentRunFixture) models.AutomationRule {
	t.Helper()
	var rule models.AutomationRule
	if err := f.Store.DB.Where("preset_key=?", automationPresetAgentRunFailed).Take(&rule).Error; err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		_, err := changeAutomationRuleInTransaction(tx, rule.ID, rule.Version, nil, &enabled, f.Service.options.Now())
		return err
	}); err != nil {
		t.Fatalf("enable Agent failure notification: %v", err)
	}
	if err := f.Store.DB.First(&rule, "id=?", rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestAutomationAgentRunFailureUsesCapturedConfigurationAfterDisable(t *testing.T) {
	f := newAIAgentRunFixture(t)
	rule := enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	now := f.Service.options.Now().Add(time.Minute)
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_event_deliveries WHERE rule_id=?", 1, rule.ID)
	disabled := false
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		_, err := changeAutomationRuleInTransaction(tx, rule.ID, rule.Version, &automationConfig{Priority: "P3"}, &disabled, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var item models.InboxItem
	if err := f.Store.DB.Where("source_entity_type=?", agentRunFailedInboxSourceType).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Priority != "P1" {
		t.Fatalf("priority=%s", item.Priority)
	}
	var notice models.AutomationRun
	if err := f.Store.DB.Where("rule_id=?", rule.ID).Take(&notice).Error; err != nil {
		t.Fatal(err)
	}
	if notice.RuleVersion != rule.Version || notice.ResultSummary != automationAgentRunFailureResultSummary {
		t.Fatalf("capture changed: %#v", notice)
	}
}

func TestAutomationAgentRunFailureMissingSourceDoesNotFabricateNotification(t *testing.T) {
	for _, missing := range []string{"run", "task"} {
		t.Run(missing, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			rule := enableAgentFailureAutomationForTest(t, f)
			run := seedRunningAgentRun(t, f)
			now := f.Service.options.Now().Add(time.Minute)
			if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			var err error
			if missing == "task" {
				err = f.Store.DB.Delete(&models.Task{}, "id=?", run.TaskID).Error
			} else {
				err = f.Store.DB.Delete(&models.AgentRun{}, "id=?", run.ID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			var notice models.AutomationRun
			if err := f.Store.DB.Where("rule_id=?", rule.ID).Take(&notice).Error; err != nil {
				t.Fatal(err)
			}
			if notice.Status != "failed" || notice.ErrorCode == nil || *notice.ErrorCode != "SOURCE_UNAVAILABLE" || notice.Retryable || notice.RetryAt != nil || notice.ResultID != nil {
				t.Fatalf("source disappeared: %#v", notice)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
		})
	}
}

func TestAutomationAgentRunFailureActionRollbackAndRetryOnlyNotification(t *testing.T) {
	f := newAIAgentRunFixture(t)
	rule := enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	now := f.Service.options.Now().Add(time.Minute)
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_agent_notice_audit BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type='inbox_item' AND NEW.action='source_projected' BEGIN SELECT RAISE(ABORT,'notice audit failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var failed models.AutomationRun
	if err := f.Store.DB.Where("rule_id=?", rule.ID).Take(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "ACTION_WRITE_FAILED" || !failed.Retryable {
		t.Fatalf("notification failure=%#v", failed)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 0)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='failed'", 1, run.ID)
	if err := f.Store.DB.Exec("DROP TRIGGER fail_agent_notice_audit").Error; err != nil {
		t.Fatal(err)
	}
	var retry models.AutomationRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		retry, err = retryAutomationRunInTransaction(tx, failed.ID, now.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if retry.Status != "succeeded" || retry.RetryOfRunID == nil || *retry.RetryOfRunID != failed.ID || retry.Attempt != 2 {
		t.Fatalf("retry=%#v", retry)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE task_id=?", 1, run.TaskID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 1)
}

func TestAutomationAgentRunFailureStrictParsers(t *testing.T) {
	evidence := `"agent_run_id":"018f0000-0000-7000-8000-000000001501","task_id":"018f0000-0000-7000-8000-000000001502","attempt":1,"error_code":"AGENT_MODEL_FAILED","failed_at":"2026-09-08T12:00:00.000000000Z"`
	cases := []struct {
		name, raw string
		parse     func(string) error
	}{
		{"event", "{" + evidence + "}", func(raw string) error { _, err := automationAgentRunFailureEventFromJSON(raw); return err }},
		{"action", "{" + evidence + `,"action_type":"inbox_item","priority":"P1"}`, func(raw string) error { _, err := automationAgentRunFailureActionFromJSON(raw); return err }},
		{"payload", "{" + evidence + `,"automation_rule_id":"00000000-0000-5000-8000-000000000105","automation_run_id":"018f0000-0000-7000-8000-000000001503","source_event_id":"018f0000-0000-7000-8000-000000001504"}`, func(raw string) error { _, err := automationAgentRunFailurePayloadFromJSON(raw); return err }},
	}
	for _, input := range cases {
		t.Run(input.name, func(t *testing.T) {
			if err := input.parse(input.raw); err != nil {
				t.Fatal(err)
			}
			invalid := map[string]string{
				"duplicate":     strings.Replace(input.raw, `"attempt":1`, `"attempt":1,"attempt":2`, 1),
				"case_alias":    strings.Replace(input.raw, `"attempt":1`, `"Attempt":1`, 1),
				"missing":       strings.Replace(input.raw, `"attempt":1,`, "", 1),
				"null":          strings.Replace(input.raw, `"attempt":1`, `"attempt":null`, 1),
				"fraction":      strings.Replace(input.raw, `"attempt":1`, `"attempt":1.5`, 1),
				"zero":          strings.Replace(input.raw, `"attempt":1`, `"attempt":0`, 1),
				"unsafe_int":    strings.Replace(input.raw, `"attempt":1`, `"attempt":9007199254740992`, 1),
				"unknown_error": strings.Replace(input.raw, "AGENT_MODEL_FAILED", "SECRET_STDERR", 1),
				"cancelled":     strings.Replace(input.raw, "AGENT_MODEL_FAILED", "AGENT_RUN_CANCELLED", 1),
				"timestamp":     strings.Replace(input.raw, "2026-09-08T12:00:00.000000000Z", "2026-09-08T12:00:00Z", 1),
				"invalid_date":  strings.Replace(input.raw, "2026-09-08", "2026-02-30", 1),
				"extra":         strings.TrimSuffix(input.raw, "}") + `,"private":"hidden"}`,
				"trailing":      input.raw + "{}",
			}
			for name, raw := range invalid {
				t.Run(name, func(t *testing.T) {
					if err := input.parse(raw); err == nil {
						t.Fatalf("accepted malformed %s", raw)
					}
				})
			}
		})
	}
	if got := safeAutomationAgentRunErrorCode("PRIVATE_ERROR_BODY"); got != "AGENT_RUN_FAILED" {
		t.Fatalf("unsafe mapping=%q", got)
	}
}

func TestAutomationAgentRunFailureRejectsRawDuplicateSnapshotAtDeliveryAndRetry(t *testing.T) {
	for _, phase := range []string{"delivery", "retry"} {
		t.Run(phase, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			rule := enableAgentFailureAutomationForTest(t, f)
			run := seedRunningAgentRun(t, f)
			now := f.Service.options.Now().Add(time.Minute)
			if phase == "retry" {
				if err := f.Store.DB.Exec(`CREATE TRIGGER fail_agent_notice_audit BEFORE INSERT ON workflow_events WHEN NEW.aggregate_type='inbox_item' AND NEW.action='source_projected' BEGIN SELECT RAISE(ABORT,'notice audit failed'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			var result models.AutomationRun
			var poisoned string
			if phase == "delivery" {
				var delivery models.AutomationEventDelivery
				if err := f.Store.DB.Take(&delivery).Error; err != nil {
					t.Fatal(err)
				}
				poisoned = strings.Replace(delivery.ActionSnapshotJSON, `"attempt":1`, `"attempt":1,"attempt":1`, 1)
				// Simulate invalid restored storage without disabling immutable-update guards.
				if err := f.Store.DB.Delete(&delivery).Error; err != nil {
					t.Fatal(err)
				}
				delivery.ActionSnapshotJSON = poisoned
				if err := f.Store.DB.Create(&delivery).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.Where("rule_id=?", rule.ID).Take(&result).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
					t.Fatal(err)
				}
				var previous models.AutomationRun
				if err := f.Store.DB.Where("rule_id=?", rule.ID).Take(&previous).Error; err != nil {
					t.Fatal(err)
				}
				if previous.Status != "failed" || !previous.Retryable {
					t.Fatalf("expected retryable write failure: %#v", previous)
				}
				poisoned = strings.Replace(previous.ActionSnapshotJSON, `"attempt":1`, `"attempt":1,"attempt":1`, 1)
				if err := f.Store.DB.Delete(&previous).Error; err != nil {
					t.Fatal(err)
				}
				previous.ActionSnapshotJSON = poisoned
				if err := f.Store.DB.Create(&previous).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.Exec("DROP TRIGGER fail_agent_notice_audit").Error; err != nil {
					t.Fatal(err)
				}
				if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
					var err error
					result, err = retryAutomationRunInTransaction(tx, previous.ID, now.Add(time.Minute))
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			if result.Status != "failed" || result.ErrorCode == nil || *result.ErrorCode != "ACTION_SNAPSHOT_INVALID" || result.Retryable || result.RetryAt != nil || result.ResultID != nil {
				t.Fatalf("raw duplicate escaped validation: %#v", result)
			}
			if result.ActionSnapshotJSON != poisoned {
				t.Fatalf("invalid immutable evidence was normalized: %s", result.ActionSnapshotJSON)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='failed'", 1, run.ID)
		})
	}
}

func TestAutomationAgentRunFailureValidatesActionAndLiveProofWithoutMutatingInput(t *testing.T) {
	f := newAIAgentRunFixture(t)
	enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	now := f.Service.options.Now().Add(time.Minute)
	if err := f.Service.finalizeAgentRun(run, "", "SECRET_UNRECOGNIZED_ERROR", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var delivery models.AutomationEventDelivery
	if err := f.Store.DB.Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(delivery.ActionSnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	original := delivery.ActionSnapshotJSON
	action, err := automationAgentRunFailureActionFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if action.ErrorCode != "AGENT_RUN_FAILED" {
		t.Fatalf("untrusted code reached action=%s", action.ErrorCode)
	}
	after, _ := json.Marshal(snapshot)
	before, _ := json.Marshal(json.RawMessage(original))
	var normalized map[string]any
	_ = json.Unmarshal(before, &normalized)
	canonical, _ := json.Marshal(normalized)
	if string(after) != string(canonical) {
		t.Fatal("snapshot was mutated")
	}
	var event models.WorkflowEvent
	if err := f.Store.DB.First(&event, "id=?", delivery.SourceEventID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := automationAgentRunFailureEvidenceFromEvent(event); err != nil {
		t.Fatal(err)
	}
	event.ActorID = stringPointer(models.BuiltinOwnerActorID)
	if _, err := automationAgentRunFailureEvidenceFromEvent(event); !errors.Is(err, errAutomationSourceEventInvalid) {
		t.Fatalf("accepted wrong producer: %v", err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationAgentRunFailureCreatesOnePrivateSafeNotification(t *testing.T) {
	for _, phase := range []string{"queued", "running"} {
		t.Run(phase, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			rule := enableAgentFailureAutomationForTest(t, f)
			var run models.AgentRun
			var err error
			now := f.Service.options.Now().Add(time.Minute)
			if phase == "queued" {
				run = seedQueuedFrozenAgentRun(t, f)
				err = f.Service.failQueuedAgentRunIdentity(run.ID, now.Format(time.RFC3339Nano))
			} else {
				run = seedRunningAgentRun(t, f)
				err = f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			var item models.InboxItem
			if err := f.Store.DB.Where("source_entity_type=? AND source_entity_id=?", "agent_run_failed", run.ID).Take(&item).Error; err != nil {
				t.Fatalf("missing notification: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 8 || payload["agent_run_id"] != run.ID || payload["task_id"] != run.TaskID || payload["attempt"] != float64(1) || payload["automation_rule_id"] != rule.ID || item.SourceEventKey == nil || *item.SourceEventKey != "agent-run:"+run.ID+":failed" || item.Status != "open" || item.ResolutionPolicy != "manual" {
				t.Fatalf("unsafe notification: %#v %#v", item, payload)
			}
			for _, private := range []string{f.Task.Title, f.Task.Description, "input_snapshot", "provider_id", "result_text", "MUST_NOT_LEAK"} {
				if strings.Contains(item.PayloadJSON+item.Title+item.Summary, private) {
					t.Fatalf("private data exposed: %s", private)
				}
			}
			var event models.WorkflowEvent
			if err := f.Store.DB.Where("aggregate_id=? AND action='agent_run_failed'", run.ID).Take(&event).Error; err != nil {
				t.Fatal(err)
			}
			var current map[string]any
			if event.CurrentJSON == nil || json.Unmarshal([]byte(*event.CurrentJSON), &current) != nil || len(current) != 5 || current["agent_run_id"] != run.ID || current["task_id"] != run.TaskID {
				t.Fatalf("failure proof incomplete: %#v", event)
			}
			if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed' AND source_entity_id=?", 1, run.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id=?", 1, rule.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, run.TaskID)
		})
	}
}

func TestAutomationAgentRunFailureCaptureRollsBackWithTerminalFact(t *testing.T) {
	f := newAIAgentRunFixture(t)
	enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_agent_failure_capture BEFORE INSERT ON automation_event_deliveries BEGIN SELECT RAISE(ABORT,'capture failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", run.CreatedAt); err == nil {
		t.Fatal("capture failure must roll back terminal fact")
	}
	var current models.AgentRun
	if err := f.Store.DB.First(&current, "id=?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != "running" {
		t.Fatalf("status=%s", current.Status)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='agent_run_failed'", 0, run.ID)
}

func TestAutomationAgentRunFailureDoesNotReopenHandledNotification(t *testing.T) {
	f := newAIAgentRunFixture(t)
	rule := enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	now := f.Service.options.Now().Add(time.Minute)
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var item models.InboxItem
	if err := f.Store.DB.Where("source_entity_type=?", agentRunFailedInboxSourceType).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	handled := formatInboxTimestamp(now.Add(time.Second))
	if err := f.Store.DB.Model(&models.InboxItem{}).Where("id=?", item.ID).Updates(map[string]any{"status": "dismissed", "triaged_at": handled, "dismissed_at": handled, "dismissed_by_actor_id": models.BuiltinOwnerActorID, "dismiss_reason": "人工已处理", "title": "人工保留诊断标题", "version": 2, "updated_at": handled}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var event models.WorkflowEvent
	if err := f.Store.DB.Where("aggregate_id=? AND action='agent_run_failed'", run.ID).Take(&event).Error; err != nil {
		t.Fatal(err)
	}
	evidence, err := automationAgentRunFailureEvidenceFromEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		return enqueueAgentRunFailureAutomationDelivery(tx, event.ID, evidence, handled)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&item, "id=?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != "dismissed" || item.Title != "人工保留诊断标题" || item.Version != 2 {
		t.Fatalf("handled notification changed: %#v", item)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id=?", 1, rule.ID)
}

func TestAutomationAgentRunFailureInfrastructureDeliveryRetryKeepsSourceCommit(t *testing.T) {
	f := newAIAgentRunFixture(t)
	rule := enableAgentFailureAutomationForTest(t, f)
	run := seedRunningAgentRun(t, f)
	now := f.Service.options.Now().Add(time.Minute)
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_agent_notice_run BEFORE INSERT ON automation_runs BEGIN SELECT RAISE(ABORT,'notification ledger unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err == nil {
		t.Fatal("notification storage failure should remain pending")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs WHERE id=? AND status='failed'", 1, run.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id=?", 0, rule.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 0)
	var delivery models.AutomationEventDelivery
	if err := f.Store.DB.Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.DeliveryAttempts != 1 || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "DELIVERY_PROCESSING_FAILED" {
		t.Fatalf("capture=%#v", delivery)
	}
	if err := f.Store.DB.Exec("DROP TRIGGER fail_agent_notice_run").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Service.consumeDueAutomationEventDeliveries(context.Background(), now.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 1)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_event_deliveries", 0)
}

func TestAutomationAgentRunFailureIsProspectiveAndExcludesOtherStates(t *testing.T) {
	for _, outcome := range []string{"disabled", "cancelled", "interrupted", "startup", "pending"} {
		t.Run(outcome, func(t *testing.T) {
			f := newAIAgentRunFixture(t)
			if outcome != "disabled" {
				enableAgentFailureAutomationForTest(t, f)
			}
			run := seedRunningAgentRun(t, f)
			now := f.Service.options.Now().Add(time.Minute)
			var err error
			switch outcome {
			case "disabled":
				err = f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano))
			case "cancelled":
				err = f.Store.DB.Transaction(func(tx *gorm.DB) error {
					return requestAgentRunCancellation(tx, run.ID, "cancel-test", now.Format(time.RFC3339Nano))
				})
				if err == nil {
					err = f.Service.finalizeAgentRun(run, "", "AGENT_MODEL_FAILED", now.Format(time.RFC3339Nano))
				}
			case "interrupted":
				err = f.Service.interruptClaimedAgentRun(run.ID, now.Format(time.RFC3339Nano))
			case "startup":
				_, err = f.Service.recoverAgentRunsOnStartup(now)
			case "pending":
				err = f.Service.persistPendingAgentRunOutput(run.ID, "bounded result", now.Format(time.RFC3339Nano))
			}
			if err != nil {
				t.Fatal(err)
			}
			if outcome == "disabled" {
				enableAgentFailureAutomationForTest(t, f)
			}
			if err = f.Service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
				t.Fatal(err)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='agent_run_failed'", 0)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id='00000000-0000-5000-8000-000000000105'", 0)
		})
	}
}
