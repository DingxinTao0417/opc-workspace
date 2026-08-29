package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type automationRuleEnvelope struct {
	Data automationRuleOutput `json:"data"`
}

type automationRuleListEnvelope struct {
	Data []automationRuleOutput `json:"data"`
}

type automationRunListEnvelope struct {
	Data []automationRunOutput `json:"data"`
	Meta pageMeta              `json:"meta"`
}

func TestAutomationCatalogPreviewAndUnavailableDependency(t *testing.T) {
	router := newTestAPI(t)
	listed := performRequest(router, http.MethodGet, "/api/v1/automations/rules", nil, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list automation rules = %d: %s", listed.Code, listed.Body.String())
	}
	var catalog automationRuleListEnvelope
	if err := json.Unmarshal(listed.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode automation catalog: %v", err)
	}
	if len(catalog.Data) != 5 {
		t.Fatalf("automation catalog count = %d, want 5", len(catalog.Data))
	}
	var daily, invoice *automationRuleOutput
	for index := range catalog.Data {
		switch catalog.Data[index].PresetKey {
		case automationPresetDailyToday:
			daily = &catalog.Data[index]
		case automationPresetInvoiceOverdue:
			invoice = &catalog.Data[index]
		}
	}
	if daily == nil || !daily.Available || daily.Status != "disabled" || daily.Config.LocalTime != "09:00" || daily.Config.Timezone != "UTC" {
		t.Fatalf("daily preset = %#v", daily)
	}
	if invoice == nil || invoice.Available || invoice.Status != "unavailable" || invoice.UnavailableReason == "" {
		t.Fatalf("invoice preset = %#v", invoice)
	}

	preview := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+daily.ID+"/preview", []byte(`{
		"config":{"local_time":"08:30","timezone":"Asia/Shanghai"}
	}`), nil)
	if preview.Code != http.StatusOK || !json.Valid(preview.Body.Bytes()) {
		t.Fatalf("preview daily preset = %d: %s", preview.Code, preview.Body.String())
	}
	invalidPreview := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+daily.ID+"/preview", []byte(`{
		"config":{"local_time":"25:00","timezone":"Local"}
	}`), nil)
	assertAPIError(t, invalidPreview, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	unavailable := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+invoice.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, unavailable, http.StatusConflict, "AUTOMATION_DEPENDENCY_UNAVAILABLE")
}

func TestProjectCompletionAutomationCreatesOneAuditedInboxItem(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-event.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()

	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable project automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	project := createProjectForTest(t, router, `{"name":"自动化交付项目"}`, map[string]string{"Idempotency-Key": "automation-project"})
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	if project.Status != "completed" {
		t.Fatalf("completed project = %#v", project)
	}

	var eventID string
	if err := store.DB.Raw(`
		SELECT id FROM workflow_events
		WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'
	`, project.ID).Row().Scan(&eventID); err != nil {
		t.Fatalf("load source event: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE source_event_id = ? AND status = 'succeeded'", 1, eventID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation' AND title LIKE '核对并准备发票%'", 1)
	var projectModel models.Project
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load completed Project model: %v", err)
	}
	if err := executeProjectCompletionAutomations(store.DB, eventID, projectModel, "2026-08-29T12:00:00.000000000Z"); err != nil {
		t.Fatalf("replay project automation: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE source_event_id = ?", 1, eventID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation' AND title LIKE '核对并准备发票%'", 1)
}

func TestScheduledAutomationFoldsOfflineWindowsAndPreservesLocalClock(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-schedule.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	service := &API{db: store.DB, options: Options{Now: time.Now}}
	if err := service.ensureAutomationRules(time.Date(2026, 3, 6, 16, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ensure automation rules: %v", err)
	}
	rule, err := service.automationRuleByPreset(automationPresetWeeklyReview)
	if err != nil {
		t.Fatalf("load weekly rule: %v", err)
	}
	config, err := normalizeAutomationConfig(rule.PresetKey, automationConfig{LocalTime: "09:30", Timezone: "America/Los_Angeles"})
	if err != nil {
		t.Fatalf("normalize weekly config: %v", err)
	}
	if err := service.updateAutomationRuleFacts(rule, config, true, time.Date(2026, 3, 6, 16, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("enable weekly rule: %v", err)
	}

	// The first window is Friday 09:30 PST. Starting on the following Monday
	// folds that stale window into one skipped run and advances across DST to
	// Friday 09:30 PDT without creating a Reminder backlog.
	now := time.Date(2026, 3, 9, 18, 0, 0, 0, time.UTC)
	if err := service.projectDueAutomations(now); err != nil {
		t.Fatalf("project due automations: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND status = 'skipped'", 1, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation'", 0)
	var nextRunAt string
	if err := store.DB.Raw("SELECT next_run_at FROM automation_rules WHERE id = ?", rule.ID).Row().Scan(&nextRunAt); err != nil {
		t.Fatalf("read next run: %v", err)
	}
	if nextRunAt != "2026-03-13T16:30:00.000000000Z" {
		t.Fatalf("next_run_at = %q, want Friday 09:30 PDT", nextRunAt)
	}

	if err := service.projectDueAutomations(time.Date(2026, 3, 13, 16, 31, 0, 0, time.UTC)); err != nil {
		t.Fatalf("run current weekly window: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND status = 'succeeded'", 1, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND trigger_at = ?", 1, "2026-03-13T16:30:00.000000000Z")
}

func TestAutomationFailureDoesNotRollbackSourceAndRetryCreatesNewAttempt(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-retry.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable project automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_automation_inbox
		BEFORE INSERT ON inbox_items
		WHEN NEW.source_entity_type = 'automation'
		BEGIN SELECT RAISE(ABORT, 'TEST_AUTOMATION_ACTION_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install automation action failure: %v", err)
	}
	project := createProjectForTest(t, router, `{"name":"失败隔离项目"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	if project.Status != "completed" {
		t.Fatalf("source Project was rolled back: %#v", project)
	}
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'failed'", rule.ID).First(&failed).Error; err != nil {
		t.Fatalf("load failed Automation Run: %v", err)
	}
	if failed.Attempt != 1 || !failed.Retryable || failed.RetryAt == nil || failed.ErrorCode == nil || *failed.ErrorCode != "ACTION_WRITE_FAILED" {
		t.Fatalf("failed Automation Run = %#v", failed)
	}
	if err := store.DB.Exec("DROP TRIGGER reject_automation_inbox").Error; err != nil {
		t.Fatalf("remove automation action failure: %v", err)
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/automations/runs/"+failed.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry Automation Run = %d: %s", retried.Code, retried.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE logical_key = ?", 2, failed.LogicalKey)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE retry_of_run_id = ? AND attempt = 2 AND status = 'succeeded'", 1, failed.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
}

func TestAutomationInfrastructureFailureNeverRollsBackCompletedProject(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-isolation.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable project automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_automation_run
		BEFORE INSERT ON automation_runs
		BEGIN SELECT RAISE(ABORT, 'TEST_AUTOMATION_INFRASTRUCTURE_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install Automation Run failure: %v", err)
	}
	project := createProjectForTest(t, router, `{"name":"基础设施隔离项目"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	if project.Status != "completed" {
		t.Fatalf("source Project was rolled back: %#v", project)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 0, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 0)
}

func automationRuleByPreset(t *testing.T, router http.Handler, preset string) automationRuleOutput {
	t.Helper()
	response := performRequest(router, http.MethodGet, "/api/v1/automations/rules", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list automation rules = %d: %s", response.Code, response.Body.String())
	}
	var envelope automationRuleListEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode automation rules: %v", err)
	}
	for _, rule := range envelope.Data {
		if rule.PresetKey == preset {
			return rule
		}
	}
	t.Fatalf("automation preset %q not found", preset)
	return automationRuleOutput{}
}
