package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAgentFailureAutomationPortableActionContract(t *testing.T) {
	preset, ok := automationPresetByKey(automationPresetAgentRunFailed)
	if !ok {
		t.Fatal("missing Agent failure preset")
	}
	action := map[string]any{
		"action_type": "inbox_item", "agent_run_id": "018f0000-0000-7000-8000-000000005501",
		"task_id": "018f0000-0000-7000-8000-000000005502", "attempt": 2,
		"error_code": "AGENT_RUN_IDENTITY_CHANGED", "failed_at": "2026-09-21T12:00:00.000000000Z", "priority": "P1",
	}
	body, _ := json.Marshal(action)
	_, _, valid, contract := automationImportAction(string(body), preset, automationConfig{Priority: "P1"})
	if !valid || !contract {
		t.Fatal("portable Agent failure action rejected")
	}
	_, _, valid, contract = automationImportAction(string(body), preset, automationConfig{Priority: "P2"})
	if !valid || contract {
		t.Fatal("priority must remain bound to captured rule config")
	}
}

func TestAgentFailureInboxSourceFilter(t *testing.T) {
	router := newTestAPI(t)
	response := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=agent_run_failed", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("Agent failure filter = %d: %s", response.Code, response.Body.String())
	}
}

type agentFailurePortableFixture struct {
	router       *gin.Engine
	store        *database.Store
	service      *API
	task         models.Task
	run          models.AgentRun
	notification models.AutomationRun
	item         models.InboxItem
}

func newAgentFailurePortableFixture(t *testing.T, deliver bool) agentFailurePortableFixture {
	t.Helper()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	router, store, _, _ := newBackupTestAPI(t, now)
	rule := automationRuleByPreset(t, router, automationPresetAgentRunFailed)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable Agent failure preset = %d: %s", enabled.Code, enabled.Body.String())
	}
	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register portable builtin = %d: %s", registered.Code, registered.Body.String())
	}
	preset, _ := agentAdapterPresetByKey(agentAdapterBuiltinTextKey)
	nowText := formatInboxTimestamp(now)
	actor := models.Actor{ID: uuid.NewString(), Type: "agent", DisplayName: "Portable historical agent", Status: "active", AgentAdapterID: &preset.ID, MetadataJSON: "{}", Version: 1, CreatedAt: nowText, UpdatedAt: nowText}
	if err := store.DB.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	created := performRequest(router, http.MethodPost, "/api/v1/tasks", []byte(`{"title":"Portable failed execution","review_policy":"none"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create Task = %d: %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data models.Task `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	task := envelope.Data
	assignment := models.TaskAssignment{ID: uuid.NewString(), TaskID: task.ID, ActorID: actor.ID, Role: "assignee", AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: nowText, Reason: "isolated historical Run fixture"}
	if err := store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	// A queued historical Run is the isolated fixture boundary. Only the real
	// failure finalizer and Automation consumer may produce its failed event,
	// successful notification run, Inbox row and immutable projection audit.
	run := models.AgentRun{ID: uuid.NewString(), TaskID: task.ID, AssignmentID: assignment.ID, ActorID: actor.ID, AdapterID: preset.ID, CreatedByActorID: models.BuiltinOwnerActorID, Attempt: 1, Status: "queued", ProviderID: uuid.NewString(), Model: "never-called", InputSnapshotJSON: `{"fixture_only":true}`, ExecutionContractVersion: 1, OutputDeliveryStatus: agentRunOutputNotReady, CreatedAt: nowText}
	if err := store.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if !deliver {
		if err := store.DB.Exec(`CREATE TRIGGER pause_agent_failure_delivery BEFORE UPDATE OF delivery_attempts ON automation_event_deliveries BEGIN SELECT RAISE(ABORT, 'TEST_DELIVERY_PAUSED'); END`).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := service.failQueuedAgentRunIdentity(run.ID, nowText); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&run, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	f := agentFailurePortableFixture{router: router, store: store, service: service, task: task, run: run}
	if deliver {
		if err := service.consumeDueAutomationEventDeliveries(context.Background(), now); err != nil {
			t.Fatal(err)
		}
		if err := store.DB.Where("rule_id = ? AND status = 'succeeded'", rule.ID).Take(&f.notification).Error; err != nil {
			t.Fatal(err)
		}
		if f.notification.ResultID == nil {
			t.Fatal("notification has no Inbox result")
		}
		if err := store.DB.First(&f.item, "id = ?", *f.notification.ResultID).Error; err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func exportAgentFailurePortable(t *testing.T, f agentFailurePortableFixture) (businessExportPackage, []byte, []byte) {
	t.Helper()
	jsonExport := performRequest(f.router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("JSON export = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	pack := decodeAutomationImportPackage(t, jsonExport.Body.Bytes())
	for _, table := range pack.Tables {
		if table.Name == "agent_runs" {
			t.Fatal("portable package must not contain operational Agent Runs")
		}
	}
	zipExport := performRequest(f.router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("ZIP export = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	return pack, bytes.Clone(jsonExport.Body.Bytes()), bytes.Clone(zipExport.Body.Bytes())
}

func TestAgentFailureAutomationPortableGraphRoundTrip(t *testing.T) {
	f := newAgentFailurePortableFixture(t, true)
	_, jsonBody, zipBody := exportAgentFailurePortable(t, f)
	for _, format := range []struct {
		name, preview, apply, confirm string
		body                          []byte
	}{
		{"JSON", "/api/v1/imports/business-data/preview", "/api/v1/imports/business-data", importReplaceConfirmation, jsonBody},
		{"ZIP", "/api/v1/imports/business-package/preview", "/api/v1/imports/business-package", packageImportReplaceConfirmation, zipBody},
	} {
		t.Run(format.name, func(t *testing.T) {
			router, store, _, _ := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, format.preview, format.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
			}
			applied := performRequest(router, http.MethodPost, format.apply, format.body, map[string]string{"X-Import-Confirmation": format.confirm})
			if applied.Code != http.StatusOK {
				t.Fatalf("apply = %d: %s", applied.Code, applied.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NULL AND payload_json = ?", 1, f.item.ID, f.item.PayloadJSON)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND status = 'succeeded'", 1, f.notification.ID)
			deleted := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
			if deleted.Code != http.StatusConflict || responseErrorCode(t, deleted.Body.Bytes()) != "TASK_HAS_ACTIVE_INBOX_SOURCES" {
				t.Fatalf("imported active diagnosis must protect Task = %d: %s", deleted.Code, deleted.Body.String())
			}
			resolved := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+f.item.ID+"/resolve", []byte(`{"reason":"Reviewed imported historical failure"}`), map[string]string{"If-Match": `"1"`})
			if resolved.Code != http.StatusOK {
				t.Fatalf("resolve imported source = %d: %s", resolved.Code, resolved.Body.String())
			}
			deleted = performRequest(router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
			if deleted.Code != http.StatusNoContent {
				t.Fatalf("delete imported terminal source = %d: %s", deleted.Code, deleted.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL AND payload_json = ?", 1, f.item.ID, f.item.PayloadJSON)
		})
	}
}

func TestAgentFailureAutomationPortableRejectsUnprovenHistory(t *testing.T) {
	f := newAgentFailurePortableFixture(t, true)
	base, _, zipBody := exportAgentFailurePortable(t, f)
	var source models.WorkflowEvent
	if err := f.store.DB.First(&source, "id = ?", *f.notification.SourceEventID).Error; err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*businessExportPackage)
	}{
		{"missing failure event", func(p *businessExportPackage) { removeAutomationImportRow(p, "workflow_events", "id", source.ID) }},
		{"missing notification run", func(p *businessExportPackage) {
			removeAutomationImportRow(p, "automation_runs", "id", f.notification.ID)
		}},
		{"missing notification audit", func(p *businessExportPackage) {
			removeAutomationImportRow(p, "workflow_events", "aggregate_id", f.notification.ID)
		}},
		{"missing projection audit", func(p *businessExportPackage) {
			removeAutomationImportRowsWhere(p, "workflow_events", map[string]any{"aggregate_id": f.item.ID, "action": "source_projected"})
		}},
		{"missing enabled rule proof", func(p *businessExportPackage) {
			removeAutomationImportRowsWhere(p, "workflow_events", map[string]any{"aggregate_id": f.notification.RuleID, "action": "automation_rule_enabled"})
		}},
		{"failed event wrong actor", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "actor_id", models.BuiltinOwnerActorID)
		}},
		{"failed event wrong Run", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "agent_run_id", uuid.NewString())
		}},
		{"failed event has request", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "request_id", "not-a-failure-producer")
		}},
		{"failed event has previous", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "previous_json", `{}`)
		}},
		{"failed event time differs", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "created_at", "2026-09-21T11:59:59.000000000Z")
		}},
		{"payload Task differs", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "payload_json", strings.Replace(f.item.PayloadJSON, f.task.ID, uuid.NewString(), 1))
		}},
		{"payload extra field", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "payload_json", strings.TrimSuffix(f.item.PayloadJSON, "}")+`,"body":"private"}`)
		}},
		{"payload duplicate field", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "payload_json", strings.TrimSuffix(f.item.PayloadJSON, "}")+`,"attempt":1}`)
		}},
		{"source wrong identity", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "source_entity_id", f.notification.ID)
		}},
		{"source old automation key", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "source_event_key", "automation:"+f.notification.LogicalKey)
		}},
		{"wrong summary claims old action", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "automation_runs", "id", f.notification.ID, "result_summary", "已创建本地核对事项。")
		}},
		{"action extra field", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "automation_runs", "id", f.notification.ID, "action_snapshot_json", strings.TrimSuffix(f.notification.ActionSnapshotJSON, "}")+`,"title":"private"}`)
		}},
		{"action duplicate field", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "automation_runs", "id", f.notification.ID, "action_snapshot_json", strings.TrimSuffix(f.notification.ActionSnapshotJSON, "}")+`,"attempt":1}`)
		}},
		{"action Task differs", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "automation_runs", "id", f.notification.ID, "action_snapshot_json", strings.Replace(f.notification.ActionSnapshotJSON, f.task.ID, uuid.NewString(), 1))
		}},
		{"failure event duplicate field", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "current_json", strings.TrimSuffix(*source.CurrentJSON, "}")+`,"attempt":1}`)
		}},
		{"failure event raw error", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "workflow_events", "id", source.ID, "current_json", strings.Replace(*source.CurrentJSON, "AGENT_RUN_IDENTITY_CHANGED", "private endpoint with secret", 1))
		}},
		{"fake deletion without audit", func(p *businessExportPackage) {
			setAutomationImportValue(t, p, "inbox_items", "id", f.item.ID, "source_deleted_at", "2026-09-21T13:00:00.000000000Z")
			removeAutomationImportRow(p, "tasks", "id", f.task.ID)
		}},
		{"Task vanished without deletion", func(p *businessExportPackage) { removeAutomationImportRow(p, "tasks", "id", f.task.ID) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pack := cloneAutomationImportPackage(t, base)
			tc.mutate(&pack)
			if validAutomationImportGraph(pack) {
				t.Fatal("unproven notification graph accepted")
			}
		})
	}
	pack := cloneAutomationImportPackage(t, base)
	cases[11].mutate(&pack)
	body, _ := json.Marshal(pack)
	assertAutomationImportRejectedWithoutSideEffects(t, body, "/api/v1/imports/business-data/preview", "/api/v1/imports/business-data", importAppendConfirmation)
	zipPack := decodeAutomationImportPackage(t, readBusinessPackageEntries(t, zipBody)["business-data.json"])
	cases[11].mutate(&zipPack)
	assertAutomationImportRejectedWithoutSideEffects(t, automationImportPackageZIP(t, zipBody, zipPack), "/api/v1/imports/business-package/preview", "/api/v1/imports/business-package", packageImportAppendConfirmation)
}

func TestAgentFailureTaskDeletionRejectsCrossTaskOrCorruptSource(t *testing.T) {
	for _, mutation := range []string{"payload_task", "live_task", "source_run", "history_missing", "shadow"} {
		t.Run(mutation, func(t *testing.T) {
			f := newAgentFailurePortableFixture(t, true)
			other := models.Task{ID: uuid.NewString(), Title: "Unrelated Task", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: f.task.CreatedAt, UpdatedAt: f.task.UpdatedAt}
			if err := f.store.DB.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			deleteID, deleteVersion := f.task.ID, f.task.Version
			switch mutation {
			case "payload_task":
				err = f.store.DB.Model(&models.InboxItem{}).Where("id = ?", f.item.ID).Update("payload_json", strings.Replace(f.item.PayloadJSON, f.task.ID, other.ID, 1)).Error
			case "live_task":
				err = f.store.DB.Model(&models.InboxItem{}).Where("id = ?", f.item.ID).Update("payload_json", strings.Replace(f.item.PayloadJSON, f.task.ID, other.ID, 1)).Error
				deleteID, deleteVersion = other.ID, other.Version
			case "source_run":
				err = f.store.DB.Model(&models.InboxItem{}).Where("id = ?", f.item.ID).Update("source_entity_id", uuid.NewString()).Error
			case "history_missing":
				err = f.store.DB.Model(&models.InboxItem{}).Where("id = ?", f.item.ID).Update("payload_json", strings.Replace(f.item.PayloadJSON, *f.notification.SourceEventID, uuid.NewString(), 1)).Error
			case "shadow":
				shadow := f.item
				shadow.ID = uuid.NewString()
				key := "shadow:" + shadow.ID
				shadow.SourceEventKey = &key
				shadow.PayloadJSON = strings.Replace(shadow.PayloadJSON, f.task.ID, other.ID, 1)
				err = f.store.DB.Create(&shadow).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			deleted := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+deleteID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, deleteVersion)})
			if deleted.Code != http.StatusConflict || responseErrorCode(t, deleted.Body.Bytes()) != "TASK_AGENT_FAILURE_SOURCE_INVALID" {
				t.Fatalf("unproven source must fail closed = %d: %s", deleted.Code, deleted.Body.String())
			}
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id IN (?, ?)", 2, f.task.ID, other.ID)
			assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items WHERE source_deleted_at IS NOT NULL", 0)
		})
	}
}

func TestAgentFailureTaskDeletionAuditRollback(t *testing.T) {
	f := newAgentFailurePortableFixture(t, true)
	resolved := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.item.ID+"/resolve", []byte(`{"reason":"reviewed"}`), map[string]string{"If-Match": `"1"`})
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve = %d: %s", resolved.Code, resolved.Body.String())
	}
	if err := f.store.DB.Exec(`CREATE TRIGGER reject_agent_failure_source_deleted BEFORE INSERT ON workflow_events WHEN NEW.action = 'source_deleted' BEGIN SELECT RAISE(ABORT, 'TEST_SOURCE_AUDIT_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if deleted.Code != http.StatusInternalServerError {
		t.Fatalf("audit failure = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM tasks WHERE id = ?", 1, f.task.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs WHERE id = ?", 1, f.run.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NULL AND version = 2", 1, f.item.ID)
}

func TestAgentFailureAutomationPortableMissingLiveRunDeliveryFailure(t *testing.T) {
	f := newAgentFailurePortableFixture(t, false)
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete before notification delivered = %d: %s", deleted.Code, deleted.Body.String())
	}
	if err := f.store.DB.Exec("DROP TRIGGER pause_agent_failure_delivery").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.service.consumeDueAutomationEventDeliveries(context.Background(), f.service.options.Now()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'agent_run_failed'", 0)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM automation_runs WHERE error_code = 'SOURCE_UNAVAILABLE' AND retryable = 0 AND status = 'failed'", 1)
	_, body, _ := exportAgentFailurePortable(t, f)
	router, store, _, _ := newBackupTestAPI(t)
	result := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if result.Code != http.StatusOK {
		t.Fatalf("source-unavailable history import = %d: %s", result.Code, result.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE error_code = 'SOURCE_UNAVAILABLE' AND retryable = 0", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'agent_run_failed'", 0)
}

func TestAgentFailureTaskDeletionUnaffectedByOtherRulesAndDuplicateFailureEvent(t *testing.T) {
	f := newAgentFailurePortableFixture(t, true)
	other := automationRuleByPreset(t, f.router, automationPresetProjectCompleted)
	updated := performRequest(f.router, http.MethodPatch, "/api/v1/automations/rules/"+other.ID, []byte(`{"config":{"priority":"P2"}}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("update unrelated rule = %d: %s", updated.Code, updated.Body.String())
	}
	enabled := performRequest(f.router, http.MethodPost, "/api/v1/automations/rules/"+other.ID+"/enable", nil, map[string]string{"If-Match": `"2"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable unrelated rule = %d: %s", enabled.Code, enabled.Body.String())
	}
	if err := f.store.DB.Transaction(func(tx *gorm.DB) error {
		return recordAgentRunWorkflowEvent(tx, "agent_run_failed", f.run.ID, map[string]any{"error_code": "AGENT_RUN_IDENTITY_CHANGED"}, "", formatInboxTimestamp(f.service.options.Now()))
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.consumeDueAutomationEventDeliveries(context.Background(), f.service.options.Now()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM automation_runs WHERE error_code = 'SOURCE_EVENT_CONFLICT' AND retryable = 0", 1)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'agent_run_failed'", 1)
	_, impact, err := loadTaskDeletionImpact(f.store.DB, f.task.ID, f.task.Version)
	if err != nil || impact.InboxSources != 1 {
		t.Fatalf("unrelated rules or failed duplicate interfered: impact=%#v err=%v", impact, err)
	}
	pack, _, _ := exportAgentFailurePortable(t, f)
	if !validAutomationImportGraph(pack) {
		t.Fatal("legitimate conflict history not portable")
	}
	resolved := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.item.ID+"/resolve", []byte(`{"reason":"reviewed duplicate notification failure"}`), map[string]string{"If-Match": `"1"`})
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve = %d: %s", resolved.Code, resolved.Body.String())
	}
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete after other-rule change = %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestAgentFailureAutomationBackgroundRetryPortableHistory(t *testing.T) {
	f := newAgentFailurePortableFixture(t, false)
	if err := f.store.DB.Exec("DROP TRIGGER pause_agent_failure_delivery").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB.Exec(`CREATE TRIGGER reject_agent_failure_notification BEFORE INSERT ON inbox_items WHEN NEW.source_entity_type = 'agent_run_failed' BEGIN SELECT RAISE(ABORT, 'TEST_NOTIFICATION_WRITE_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.service.consumeDueAutomationEventDeliveries(context.Background(), f.service.options.Now()); err != nil {
		t.Fatal(err)
	}
	var failed models.AutomationRun
	if err := f.store.DB.Where("status = 'failed' AND error_code = 'ACTION_WRITE_FAILED'").Take(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if !failed.Retryable {
		t.Fatal("write failure must preserve bounded notification retry")
	}
	if err := f.store.DB.Exec("DROP TRIGGER reject_agent_failure_notification").Error; err != nil {
		t.Fatal(err)
	}
	disabled := performRequest(f.router, http.MethodPost, "/api/v1/automations/rules/"+failed.RuleID+"/disable", nil, map[string]string{"If-Match": `"2"`})
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable captured rule = %d: %s", disabled.Code, disabled.Body.String())
	}
	if err := f.service.projectDueAutomationRetries(f.service.options.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DB.Where("retry_of_run_id = ? AND status = 'succeeded'", failed.ID).Take(&f.notification).Error; err != nil {
		t.Fatal(err)
	}
	if f.notification.ResultID == nil {
		t.Fatal("retry omitted its notification")
	}
	if err := f.store.DB.First(&f.item, "id = ?", *f.notification.ResultID).Error; err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs WHERE id = ? AND status = 'failed' AND attempt = 1", 1, f.run.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs", 1)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions", 0)
	_, impact, err := loadTaskDeletionImpact(f.store.DB, f.task.ID, f.task.Version)
	if err != nil || impact.InboxSources != 1 {
		t.Fatalf("retried source impact = %#v err=%v", impact, err)
	}
	_, body, _ := exportAgentFailurePortable(t, f)
	router, store, _, _ := newBackupTestAPI(t)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("notification retry import = %d: %s", applied.Code, applied.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE retry_of_run_id = ? AND status = 'succeeded'", 1, failed.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
}

func TestAgentFailureTaskDeletionAndTombstoneRoundTrip(t *testing.T) {
	f := newAgentFailurePortableFixture(t, true)
	_, impact, err := loadTaskDeletionImpact(f.store.DB, f.task.ID, f.task.Version)
	if err != nil || impact.AgentRuns != 1 || impact.InboxSources != 1 {
		t.Fatalf("deletion impact = %#v, %v", impact, err)
	}
	blocked := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != "TASK_HAS_ACTIVE_INBOX_SOURCES" {
		t.Fatalf("active failure source = %d: %s", blocked.Code, blocked.Body.String())
	}
	resolved := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.item.ID+"/resolve", []byte(`{"reason":"Reviewed execution failure; keep original failure history"}`), map[string]string{"If-Match": `"1"`})
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve diagnosis = %d: %s", resolved.Code, resolved.Body.String())
	}
	deleted := performRequest(f.router, http.MethodDelete, "/api/v1/tasks/"+f.task.ID, nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.task.Version)})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete terminal-source Task = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM agent_runs WHERE id = ?", 0, f.run.ID)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL AND payload_json = ?", 1, f.item.ID, f.item.PayloadJSON)
	assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id = ? AND action = 'source_deleted'", 1, f.item.ID)
	_, body, _ := exportAgentFailurePortable(t, f)
	router, store, _, _ := newBackupTestAPI(t)
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("apply tombstone = %d: %s", applied.Code, applied.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id = ?", 0, f.task.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL AND payload_json = ?", 1, f.item.ID, f.item.PayloadJSON)
}
