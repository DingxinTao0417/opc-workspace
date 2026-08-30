package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
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

type automationRunDetailEnvelope struct {
	Data automationRunDetailOutput `json:"data"`
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
	var daily, invoice, agent *automationRuleOutput
	for index := range catalog.Data {
		switch catalog.Data[index].PresetKey {
		case automationPresetDailyToday:
			daily = &catalog.Data[index]
		case automationPresetInvoiceOverdue:
			invoice = &catalog.Data[index]
		case automationPresetAgentRunFailed:
			agent = &catalog.Data[index]
		}
	}
	if daily == nil || !daily.Available || daily.Status != "disabled" || daily.Config.LocalTime != "09:00" || daily.Config.Timezone != "UTC" {
		t.Fatalf("daily preset = %#v", daily)
	}
	if invoice == nil || !invoice.Available || invoice.Status != "disabled" || invoice.UnavailableReason != "" ||
		invoice.Name != "发票逾期跟进" || invoice.TriggerLabel != "发票工作流事件：invoice_overdue" ||
		invoice.ActionLabel != "创建“跟进逾期发票”本地任务" || invoice.Config.Priority != "P1" {
		t.Fatalf("invoice preset = %#v", invoice)
	}
	wantInvoicePermissions := []string{"读取本地发票逾期事件", "创建一条本地跟进任务", "记录本地自动化运行"}
	if len(invoice.Permissions) != len(wantInvoicePermissions) {
		t.Fatalf("invoice permissions = %#v", invoice.Permissions)
	}
	for index := range wantInvoicePermissions {
		if invoice.Permissions[index] != wantInvoicePermissions[index] {
			t.Fatalf("invoice permissions = %#v", invoice.Permissions)
		}
	}
	if agent == nil || agent.Available || agent.Status != "unavailable" || agent.UnavailableReason == "" {
		t.Fatalf("agent preset = %#v", agent)
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

	enabledInvoice := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+invoice.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabledInvoice.Code != http.StatusOK {
		t.Fatalf("enable Invoice automation = %d: %s", enabledInvoice.Code, enabledInvoice.Body.String())
	}
	var enabledInvoiceRule automationRuleEnvelope
	if err := json.Unmarshal(enabledInvoice.Body.Bytes(), &enabledInvoiceRule); err != nil ||
		enabledInvoiceRule.Data.Status != "enabled" || enabledInvoiceRule.Data.Version != 2 {
		t.Fatalf("enabled Invoice automation = %#v err=%v", enabledInvoiceRule.Data, err)
	}

	unavailable := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+agent.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	assertAPIError(t, unavailable, http.StatusConflict, "AUTOMATION_DEPENDENCY_UNAVAILABLE")
}

func TestRepeatedScheduleAutomationCommandsAreIdempotent(t *testing.T) {
	tests := []struct {
		name           string
		presetKey      string
		initialNow     time.Time
		initialNextRun string
		staleNow       time.Time
	}{
		{
			name: "daily", presetKey: automationPresetDailyToday,
			initialNow:     time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
			initialNextRun: "2026-09-02T09:00:00.000000000Z",
			staleNow:       time.Date(2026, 9, 2, 9, 1, 0, 0, time.UTC),
		},
		{
			name: "weekly", presetKey: automationPresetWeeklyReview,
			initialNow:     time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC),
			initialNextRun: "2026-09-04T17:00:00.000000000Z",
			staleNow:       time.Date(2026, 9, 4, 17, 1, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := database.Open(filepath.Join(t.TempDir(), "automation-repeat-enable.db"))
			if err != nil {
				t.Fatalf("database.Open: %v", err)
			}
			defer store.Close()

			now := test.initialNow
			router, err := NewRouter(store.DB, Options{
				AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
				SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
				Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return now },
				ReminderScanInterval: -1, AutomationDeliveryScanInterval: -1,
			})
			if err != nil {
				t.Fatalf("NewRouter: %v", err)
			}
			defer router.Close()

			rule := automationRuleByPreset(t, router, test.presetKey)
			assertResponse := func(label string, response *httptest.ResponseRecorder, wantNextRun string) {
				t.Helper()
				if response.Code != http.StatusOK {
					t.Fatalf("%s = %d: %s", label, response.Code, response.Body.String())
				}
				if etag := response.Header().Get("ETag"); etag != `"2"` {
					t.Fatalf("%s ETag = %q, want %q", label, etag, `"2"`)
				}
				var envelope automationRuleEnvelope
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode %s: %v", label, err)
				}
				if envelope.Data.ID != rule.ID || envelope.Data.PresetKey != test.presetKey ||
					envelope.Data.Status != "enabled" || envelope.Data.Version != 2 ||
					envelope.Data.NextRunAt == nil || *envelope.Data.NextRunAt != wantNextRun {
					t.Fatalf("%s response = %#v", label, envelope.Data)
				}
			}

			first := performRequest(
				router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
				map[string]string{"If-Match": `"1"`},
			)
			assertResponse("first enable", first, test.initialNextRun)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ? AND action = 'automation_rule_enabled'", 1, rule.ID)
			var enabledPersisted models.AutomationRule
			if err := store.DB.First(&enabledPersisted, "id = ?", rule.ID).Error; err != nil {
				t.Fatalf("load initially enabled schedule rule: %v", err)
			}

			consistent := performRequest(
				router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
				map[string]string{"If-Match": `"2"`},
			)
			assertResponse("consistent repeated enable", consistent, test.initialNextRun)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ?", 1, rule.ID)

			now = test.staleNow
			staleRepeat := performRequest(
				router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
				map[string]string{"If-Match": `"2"`},
			)
			assertResponse("stale repeated enable", staleRepeat, test.initialNextRun)

			var persisted models.AutomationRule
			if err := store.DB.First(&persisted, "id = ?", rule.ID).Error; err != nil {
				t.Fatalf("load idempotently enabled schedule rule: %v", err)
			}
			if persisted.Version != 2 || !persisted.Enabled || persisted.NextRunAt == nil ||
				*persisted.NextRunAt != test.initialNextRun || persisted.UpdatedAt != enabledPersisted.UpdatedAt {
				t.Fatalf("idempotently enabled schedule rule = %#v", persisted)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ?", 1, rule.ID)

			sameConfigBody, err := json.Marshal(map[string]any{"config": rule.Config})
			if err != nil {
				t.Fatalf("encode unchanged schedule config: %v", err)
			}
			sameConfig := performRequest(
				router, http.MethodPatch, "/api/v1/automations/rules/"+rule.ID, sameConfigBody,
				map[string]string{"If-Match": `"2"`},
			)
			assertResponse("unchanged config", sameConfig, test.initialNextRun)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ?", 1, rule.ID)

			var enabledEvent models.WorkflowEvent
			if err := store.DB.Where(
				"aggregate_type = 'automation_rule' AND aggregate_id = ?", rule.ID,
			).Take(&enabledEvent).Error; err != nil {
				t.Fatalf("load immutable enable event: %v", err)
			}
			var eventCurrent map[string]any
			if enabledEvent.Action != "automation_rule_enabled" || enabledEvent.CurrentJSON == nil ||
				json.Unmarshal([]byte(*enabledEvent.CurrentJSON), &eventCurrent) != nil ||
				eventCurrent["version"] != float64(2) || eventCurrent["next_run_at"] != test.initialNextRun {
				t.Fatalf("immutable enable event = %#v current=%#v", enabledEvent, eventCurrent)
			}

			disabled := performRequest(
				router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/disable", nil,
				map[string]string{"If-Match": `"2"`},
			)
			if disabled.Code != http.StatusOK || disabled.Header().Get("ETag") != `"3"` {
				t.Fatalf("first disable = %d ETag=%q: %s", disabled.Code, disabled.Header().Get("ETag"), disabled.Body.String())
			}
			var disabledEnvelope automationRuleEnvelope
			if err := json.Unmarshal(disabled.Body.Bytes(), &disabledEnvelope); err != nil ||
				disabledEnvelope.Data.Status != "disabled" || disabledEnvelope.Data.Version != 3 ||
				disabledEnvelope.Data.NextRunAt != nil {
				t.Fatalf("first disable response = %#v err=%v", disabledEnvelope.Data, err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ?", 2, rule.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ? AND action = 'automation_rule_disabled'", 1, rule.ID)

			repeatedDisable := performRequest(
				router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/disable", nil,
				map[string]string{"If-Match": `"3"`},
			)
			if repeatedDisable.Code != http.StatusOK || repeatedDisable.Header().Get("ETag") != `"3"` {
				t.Fatalf("repeated disable = %d ETag=%q: %s", repeatedDisable.Code, repeatedDisable.Header().Get("ETag"), repeatedDisable.Body.String())
			}
			var repeatedDisableEnvelope automationRuleEnvelope
			if err := json.Unmarshal(repeatedDisable.Body.Bytes(), &repeatedDisableEnvelope); err != nil ||
				repeatedDisableEnvelope.Data.Status != "disabled" || repeatedDisableEnvelope.Data.Version != 3 ||
				repeatedDisableEnvelope.Data.NextRunAt != nil {
				t.Fatalf("repeated disable response = %#v err=%v", repeatedDisableEnvelope.Data, err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_rule' AND aggregate_id = ?", 2, rule.ID)
		})
	}
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
	var enabledRule automationRuleEnvelope
	if err := json.Unmarshal(enabled.Body.Bytes(), &enabledRule); err != nil {
		t.Fatalf("decode enabled project automation: %v", err)
	}
	if enabledRule.Data.ID != rule.ID || enabledRule.Data.PresetKey != automationPresetProjectCompleted ||
		enabledRule.Data.Status != "enabled" || enabledRule.Data.Version != rule.Version+1 {
		t.Fatalf("enabled project automation = %#v", enabledRule.Data)
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
	runsResponse := performRequest(router, http.MethodGet, "/api/v1/automations/runs?rule_id="+rule.ID, nil, nil)
	if runsResponse.Code != http.StatusOK {
		t.Fatalf("list project completion runs = %d: %s", runsResponse.Code, runsResponse.Body.String())
	}
	var runs automationRunListEnvelope
	if err := json.Unmarshal(runsResponse.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode project completion runs: %v", err)
	}
	if runs.Meta.Total != 1 || len(runs.Data) != 1 {
		t.Fatalf("project completion runs = %#v", runs)
	}
	run := runs.Data[0]
	if run.RuleID != rule.ID || run.PresetKey != automationPresetProjectCompleted || run.RuleVersion != enabledRule.Data.Version ||
		run.TriggerType != "event" || run.SourceEventID == nil || *run.SourceEventID != eventID || run.ScheduledFor != nil ||
		run.Status != "succeeded" || run.Attempt != 1 || run.ResultType == nil || *run.ResultType != "inbox_item" || run.ResultID == nil {
		t.Fatalf("project completion run contract = %#v", run)
	}
	if len(run.ConfigSnapshot) != 1 || run.ConfigSnapshot["priority"] != "P1" ||
		len(run.ActionSnapshot) != 5 || run.ActionSnapshot["action_type"] != "inbox_item" ||
		run.ActionSnapshot["project_id"] != project.ID || run.ActionSnapshot["project_name"] != project.Name ||
		run.ActionSnapshot["title"] != automationProjectCompletionTitle(project.Name) || run.ActionSnapshot["priority"] != "P1" {
		t.Fatalf("project completion run snapshots = config=%#v action=%#v", run.ConfigSnapshot, run.ActionSnapshot)
	}

	inboxResponse := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=automation", nil, nil)
	if inboxResponse.Code != http.StatusOK {
		t.Fatalf("list Automation Inbox Items = %d: %s", inboxResponse.Code, inboxResponse.Body.String())
	}
	var inbox struct {
		Data []inboxItemOutput `json:"data"`
		Meta inboxListMeta     `json:"meta"`
	}
	if err := json.Unmarshal(inboxResponse.Body.Bytes(), &inbox); err != nil {
		t.Fatalf("decode Automation Inbox list: %v", err)
	}
	if inbox.Meta.Total != 1 || len(inbox.Data) != 1 {
		t.Fatalf("Automation Inbox list = %#v", inbox)
	}
	item := inbox.Data[0]
	expectedSourceKey := "automation:event:" + rule.ID + ":" + eventID
	if item.ID != *run.ResultID || item.Kind != "event" || item.Title != automationProjectCompletionTitle(project.Name) ||
		item.Summary != "项目已完成。请人工核对是否需要开票，并准备后续资料；自动化不会生成或发送发票。" ||
		item.SourceEntityType != automationInboxSourceType || item.SourceEntityID == nil || *item.SourceEntityID != run.ID ||
		item.SourceEventKey == nil || *item.SourceEventKey != expectedSourceKey || item.SourceDeletedAt != nil ||
		item.Priority != "P1" || item.Status != "open" || item.ResolutionPolicy != "manual" || item.DueAt != nil ||
		item.ReadAt != nil || item.TriagedAt != nil || item.SnoozedUntil != nil || item.ResolvedByActorID != nil ||
		item.ResolvedAt != nil || item.ResolutionReason != nil || item.ResolutionMode != nil ||
		item.DismissedByActorID != nil || item.DismissedAt != nil || item.DismissReason != nil || item.Version != 1 ||
		item.CreatedAt == "" || item.UpdatedAt != item.CreatedAt {
		t.Fatalf("Automation Inbox Item contract = %#v", item)
	}
	expectedActions := []string{"edit", "read", "snooze", "resolve", "dismiss"}
	if len(item.AvailableActions) != len(expectedActions) {
		t.Fatalf("Automation Inbox Item actions = %#v", item.AvailableActions)
	}
	for index := range expectedActions {
		if item.AvailableActions[index] != expectedActions[index] {
			t.Fatalf("Automation Inbox Item actions = %#v", item.AvailableActions)
		}
	}
	expectedPayload := map[string]string{
		"automation_rule_id": rule.ID,
		"automation_run_id":  run.ID,
		"preset_key":         automationPresetProjectCompleted,
		"project_id":         project.ID,
		"project_name":       project.Name,
	}
	if len(item.PayloadJSON) != len(expectedPayload) {
		t.Fatalf("Automation Inbox payload keys = %#v", item.PayloadJSON)
	}
	for key, expected := range expectedPayload {
		if actual, ok := item.PayloadJSON[key].(string); !ok || actual != expected {
			t.Fatalf("Automation Inbox payload[%s] = %#v, want %q; payload=%#v", key, item.PayloadJSON[key], expected, item.PayloadJSON)
		}
	}

	unknownFilter := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=automation_unknown", nil, nil)
	if unknownFilter.Code != http.StatusBadRequest || responseErrorCode(t, unknownFilter.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("unknown source filter = %d: %s", unknownFilter.Code, unknownFilter.Body.String())
	}
	var projectModel models.Project
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load completed Project model: %v", err)
	}
	var ruleModel models.AutomationRule
	if err := store.DB.First(&ruleModel, "id = ?", rule.ID).Error; err != nil {
		t.Fatalf("load enabled Automation Rule model: %v", err)
	}
	sourceEventID := eventID
	attemptInput := automationAttemptInput{
		Rule: ruleModel, TriggerType: "event", SourceEventID: &sourceEventID,
		LogicalKey: "event:" + rule.ID + ":" + eventID, Attempt: 1,
		Config: automationConfig{Priority: "P1"},
		ActionSnapshot: map[string]any{
			"action_type": "inbox_item", "project_id": project.ID, "project_name": project.Name,
			"title": automationProjectCompletionTitle(project.Name), "priority": "P1",
		},
		Now: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	}
	action, err := automationProjectCompletionActionFromSnapshot(attemptInput.ActionSnapshot)
	if err != nil {
		t.Fatalf("decode valid project completion action: %v", err)
	}
	if err := validateAutomationProjectCompletionInboxAttempt(store.DB, attemptInput, action); err != nil {
		t.Fatalf("validate baseline project completion attempt: %v", err)
	}
	if !attemptInput.Rule.Enabled {
		t.Fatal("contract test baseline must use an enabled Automation Rule")
	}
	invalidActionSnapshot := map[string]any{
		"action_type": "reminder", "project_id": project.ID, "project_name": project.Name,
		"title": automationProjectCompletionTitle(project.Name), "priority": "P1",
	}
	if _, err := automationProjectCompletionActionFromSnapshot(invalidActionSnapshot); err == nil {
		t.Fatal("non-Inbox action type was accepted in project completion snapshot")
	}
	for _, test := range []struct {
		name   string
		mutate func(*automationAttemptInput)
	}{
		{name: "preset binding", mutate: func(input *automationAttemptInput) {
			input.Rule.PresetKey = automationPresetDailyToday
		}},
		{name: "trigger binding", mutate: func(input *automationAttemptInput) {
			input.TriggerType = "schedule"
		}},
		{name: "logical key", mutate: func(input *automationAttemptInput) {
			input.LogicalKey += ":tampered"
		}},
		{name: "canonical rule UUID", mutate: func(input *automationAttemptInput) {
			input.Rule.ID = "{" + input.Rule.ID + "}"
			input.LogicalKey = "event:" + input.Rule.ID + ":" + *input.SourceEventID
		}},
		{name: "canonical event UUID", mutate: func(input *automationAttemptInput) {
			value := "{" + *input.SourceEventID + "}"
			input.SourceEventID = &value
			input.LogicalKey = "event:" + input.Rule.ID + ":" + value
		}},
		{name: "retry shape", mutate: func(input *automationAttemptInput) {
			input.Attempt = 2
		}},
	} {
		t.Run("attempt contract/"+test.name, func(t *testing.T) {
			invalid := attemptInput
			test.mutate(&invalid)
			if err := validateAutomationProjectCompletionInboxAttempt(store.DB, invalid, action); err == nil {
				t.Fatalf("invalid %s was accepted", test.name)
			}
		})
	}

	editedResponse := performRequest(
		router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID,
		[]byte(`{"title":"核对并准备发票：人工补充","summary":"人工补充的核对说明","priority":"P2"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if editedResponse.Code != http.StatusOK || editedResponse.Header().Get("ETag") != `"2"` {
		t.Fatalf("edit Automation Inbox mutable facts = %d headers=%v: %s", editedResponse.Code, editedResponse.Header(), editedResponse.Body.String())
	}
	editedItem := decodeInboxItemData(t, editedResponse.Body.Bytes())
	if editedItem.Title != "核对并准备发票：人工补充" || editedItem.Summary != "人工补充的核对说明" ||
		editedItem.Priority != "P2" || editedItem.Version != 2 {
		t.Fatalf("edited Automation Inbox mutable facts = %#v", editedItem)
	}
	_, reuseErr := createAutomationInboxItem(store.DB, uuid.NewString(), attemptInput, "2026-08-29T12:00:00.000000000Z")
	if reuseErr == nil || !strings.Contains(reuseErr.Error(), "already committed by another run") {
		t.Fatalf("fresh run reused persisted Automation Inbox Item: %v", reuseErr)
	}
	replayAt := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		return enqueueProjectCompletionAutomationDelivery(
			tx, eventID, projectModel, formatInboxTimestamp(replayAt),
		)
	}); err != nil {
		t.Fatalf("recapture project automation: %v", err)
	}
	replayService := &API{db: store.DB, options: Options{Now: func() time.Time { return replayAt }}}
	if err := replayService.consumeDueAutomationEventDeliveries(context.Background(), replayAt); err != nil {
		t.Fatalf("replay project automation delivery: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE source_event_id = ?", 1, eventID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation' AND title LIKE '核对并准备发票%'", 1)
}

func TestProjectCompletionAutomationRejectsCorruptPersistedInboxSource(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-corrupt-source.db"))
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

	project := createProjectForTest(t, router, `{"name":"自动化契约损坏项目"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var eventID string
	if err := store.DB.Raw(`
		SELECT id FROM workflow_events
		WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'
	`, project.ID).Row().Scan(&eventID); err != nil {
		t.Fatalf("load source event: %v", err)
	}

	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable project automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	corruptRunID := uuid.NewString()
	corruptItemID := uuid.NewString()
	key := "automation:event:" + rule.ID + ":" + eventID
	now := "2026-08-29T12:00:00.000000000Z"
	payload, err := json.Marshal(map[string]any{
		"automation_rule_id": rule.ID,
		"automation_run_id":  corruptRunID,
		"preset_key":         automationPresetProjectCompleted,
		"project_id":         project.ID,
		"project_name":       "被篡改的项目名",
	})
	if err != nil {
		t.Fatalf("encode corrupt payload: %v", err)
	}
	if err := store.DB.Create(&models.InboxItem{
		ID: corruptItemID, Kind: "event", Title: automationProjectCompletionTitle(project.Name),
		Summary: automationProjectCompletionItemSummary, SourceEntityType: automationInboxSourceType,
		SourceEntityID: &corruptRunID, SourceEventKey: &key, Priority: "P1", Status: "open",
		ResolutionPolicy: "manual", PayloadJSON: string(payload), Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed corrupt Automation Inbox source: %v", err)
	}

	var projectModel models.Project
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil {
		t.Fatalf("load completed Project model: %v", err)
	}
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		return enqueueProjectCompletionAutomationDelivery(tx, eventID, projectModel, now)
	}); err != nil {
		t.Fatalf("capture Automation with corrupt source: %v", err)
	}
	consumeAt, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		t.Fatalf("parse Automation consume timestamp: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return consumeAt }}}
	if err := service.consumeDueAutomationEventDeliveries(context.Background(), consumeAt); err != nil {
		t.Fatalf("consume Automation with corrupt source: %v", err)
	}
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, eventID).Take(&failed).Error; err != nil {
		t.Fatalf("load rejected Automation Run: %v", err)
	}
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "SOURCE_EVENT_CONFLICT" ||
		failed.ResultType != nil || failed.ResultID != nil || failed.Retryable || failed.RetryAt != nil {
		t.Fatalf("rejected Automation Run = %#v", failed)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND status = 'succeeded'", 0, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
	var retained models.InboxItem
	if err := store.DB.First(&retained, "id = ?", corruptItemID).Error; err != nil {
		t.Fatalf("load retained corrupt source: %v", err)
	}
	if retained.Version != 1 || retained.PayloadJSON != string(payload) || retained.UpdatedAt != now {
		t.Fatalf("corrupt source changed despite rejected action: %#v", retained)
	}
	if err := store.DB.First(&projectModel, "id = ?", project.ID).Error; err != nil || projectModel.Status != "completed" {
		t.Fatalf("source Project changed after rejected Automation: project=%#v err=%v", projectModel, err)
	}
}

func TestProjectCompletionAutomationRollsBackPartialInboxAction(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "automation-partial-action.db"))
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
	project := createProjectForTest(t, router, `{"name":"自动化部分写入回滚项目"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	if err := store.DB.Exec(`
		CREATE TRIGGER fail_automation_inbox_source_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'inbox_item'
		 AND NEW.action = 'source_projected'
		 AND EXISTS (
			SELECT 1 FROM inbox_items
			WHERE id = NEW.aggregate_id AND source_entity_type = 'automation'
		 )
		BEGIN
			SELECT RAISE(ABORT, 'TEST_AUTOMATION_SOURCE_EVENT_FAILURE');
		END
	`).Error; err != nil {
		t.Fatalf("install Automation source event failure: %v", err)
	}
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	if project.Status != "completed" {
		t.Fatalf("Project completion was rolled back: %#v", project)
	}
	var eventID string
	if err := store.DB.Raw(`
		SELECT id FROM workflow_events
		WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'
	`, project.ID).Row().Scan(&eventID); err != nil {
		t.Fatalf("load source event: %v", err)
	}
	var failed models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND source_event_id = ?", rule.ID, eventID).Take(&failed).Error; err != nil {
		t.Fatalf("load failed Automation Run: %v", err)
	}
	if failed.Status != "failed" || failed.ErrorCode == nil || *failed.ErrorCode != "ACTION_WRITE_FAILED" ||
		failed.ResultType != nil || failed.ResultID != nil || !failed.Retryable || failed.RetryAt == nil {
		t.Fatalf("partial action failure Run = %#v", failed)
	}
	key := "automation:event:" + rule.ID + ":" + eventID
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ?", 0, key)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ?", 1, rule.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_run' AND aggregate_id = ? AND action = 'automation_run_failed'", 1, failed.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_run' AND aggregate_id = ? AND action = 'automation_run_succeeded'", 0, failed.ID)
	var persistedProject models.Project
	if err := store.DB.First(&persistedProject, "id = ?", project.ID).Error; err != nil || persistedProject.Status != "completed" {
		t.Fatalf("source Project changed after partial action rollback: project=%#v err=%v", persistedProject, err)
	}
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
	currentRule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	disabled := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+currentRule.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentRule.Version)},
	)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable captured event rule = %d: %s", disabled.Code, disabled.Body.String())
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/automations/runs/"+failed.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry Automation Run = %d: %s", retried.Code, retried.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE logical_key = ?", 2, failed.LogicalKey)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE retry_of_run_id = ? AND attempt = 2 AND status = 'succeeded'", 1, failed.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'automation'", 1)
}

func TestAutomationRunDetailIncludesSafeSourceAndRetryChainWithoutInflatingList(t *testing.T) {
	router, store := newProjectTestAPI(t)
	eventRule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+eventRule.ID+"/enable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, eventRule.Version)},
	)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable event Automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_run_detail_automation_inbox
		BEFORE INSERT ON inbox_items
		WHEN NEW.source_entity_type = 'automation'
		BEGIN SELECT RAISE(ABORT, 'TEST_RUN_DETAIL_ACTION_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install run detail action failure: %v", err)
	}
	project := createProjectForTest(t, router, `{"name":"运行审计项目"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var first models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'failed'", eventRule.ID).Take(&first).Error; err != nil {
		t.Fatalf("load first Automation attempt: %v", err)
	}
	var sourceEvent models.WorkflowEvent
	if first.SourceEventID == nil {
		t.Fatalf("event Automation has no source event: %#v", first)
	}
	if err := store.DB.First(&sourceEvent, "id = ?", *first.SourceEventID).Error; err != nil {
		t.Fatalf("load Automation source event: %v", err)
	}
	if err := store.DB.Exec("DROP TRIGGER reject_run_detail_automation_inbox").Error; err != nil {
		t.Fatalf("remove run detail action failure: %v", err)
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/automations/runs/"+first.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry Automation = %d: %s", retried.Code, retried.Body.String())
	}
	var second models.AutomationRun
	if err := store.DB.Where("retry_of_run_id = ?", first.ID).Take(&second).Error; err != nil {
		t.Fatalf("load second Automation attempt: %v", err)
	}

	readDetail := func(runID string) automationRunDetailOutput {
		t.Helper()
		response := performRequest(router, http.MethodGet, "/api/v1/automations/runs/"+runID, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("get Automation Run %s = %d: %s", runID, response.Code, response.Body.String())
		}
		var envelope automationRunDetailEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode Automation Run %s: %v", runID, err)
		}
		return envelope.Data
	}
	firstDetail := readDetail(first.ID)
	secondDetail := readDetail(second.ID)
	for _, detail := range []automationRunDetailOutput{firstDetail, secondDetail} {
		if detail.Source.Kind != "event" || !detail.Source.Available || detail.Source.EventID == nil ||
			*detail.Source.EventID != sourceEvent.ID || detail.Source.AggregateType == nil || *detail.Source.AggregateType != "project" ||
			detail.Source.AggregateID == nil || *detail.Source.AggregateID != project.ID || detail.Source.Action == nil ||
			*detail.Source.Action != "project_completed" || detail.Source.OccurredAt == nil ||
			*detail.Source.OccurredAt != normalizeTimestamp(sourceEvent.CreatedAt) || detail.Source.ScheduledFor != nil {
			t.Fatalf("event source detail = %#v", detail.Source)
		}
		if len(detail.RetryChain) != 2 || detail.RetryChain[0].ID != first.ID || detail.RetryChain[0].Attempt != 1 ||
			detail.RetryChain[1].ID != second.ID || detail.RetryChain[1].Attempt != 2 ||
			detail.RetryChain[1].RetryOfRunID == nil || *detail.RetryChain[1].RetryOfRunID != first.ID {
			t.Fatalf("retry chain from %s = %#v", detail.ID, detail.RetryChain)
		}
	}
	firstChainJSON, err := json.Marshal(firstDetail.RetryChain)
	if err != nil {
		t.Fatalf("encode first retry chain: %v", err)
	}
	secondChainJSON, err := json.Marshal(secondDetail.RetryChain)
	if err != nil {
		t.Fatalf("encode second retry chain: %v", err)
	}
	if string(firstChainJSON) != string(secondChainJSON) {
		t.Fatalf("retry chain differs by selected attempt: first=%s second=%s", firstChainJSON, secondChainJSON)
	}
	if firstDetail.RetryChain[0].ErrorCode == nil || *firstDetail.RetryChain[0].ErrorCode != "ACTION_WRITE_FAILED" ||
		!firstDetail.RetryChain[0].Retryable || firstDetail.RetryChain[0].RetryAt == nil ||
		firstDetail.RetryChain[1].Status != "succeeded" || firstDetail.RetryChain[1].ResultType == nil ||
		*firstDetail.RetryChain[1].ResultType != "inbox_item" || firstDetail.RetryChain[1].ResultID == nil {
		t.Fatalf("retry chain summaries = %#v", firstDetail.RetryChain)
	}
	rawDetailResponse := performRequest(router, http.MethodGet, "/api/v1/automations/runs/"+first.ID, nil, nil)
	var rawDetail struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rawDetailResponse.Body.Bytes(), &rawDetail); err != nil {
		t.Fatalf("decode raw Automation Run detail: %v", err)
	}
	for _, prohibited := range []string{"logical_key", "dedupe_key"} {
		if _, exists := rawDetail.Data[prohibited]; exists {
			t.Fatalf("Automation Run detail exposed %s: %#v", prohibited, rawDetail.Data)
		}
	}
	rawSource, ok := rawDetail.Data["source"].(map[string]any)
	if !ok {
		t.Fatalf("Automation Run detail source is not an object: %#v", rawDetail.Data["source"])
	}
	if len(rawSource) != 8 {
		t.Fatalf("Automation Run source fields = %#v", rawSource)
	}
	for _, prohibited := range []string{"previous_json", "current_json", "request_id", "logical_key", "dedupe_key"} {
		if _, exists := rawSource[prohibited]; exists {
			t.Fatalf("Automation Run source exposed %s: %#v", prohibited, rawSource)
		}
	}
	rawChain, ok := rawDetail.Data["retry_chain"].([]any)
	if !ok || len(rawChain) != 2 {
		t.Fatalf("Automation Run raw retry chain = %#v", rawDetail.Data["retry_chain"])
	}
	for _, rawAttempt := range rawChain {
		attempt, ok := rawAttempt.(map[string]any)
		if !ok {
			t.Fatalf("Automation Run retry summary is not an object: %#v", rawAttempt)
		}
		for _, prohibited := range []string{"logical_key", "dedupe_key"} {
			if _, exists := attempt[prohibited]; exists {
				t.Fatalf("Automation Run retry summary exposed %s: %#v", prohibited, attempt)
			}
		}
	}

	missingEventID := uuid.NewString()
	missing := first
	missing.ID = uuid.NewString()
	missing.SourceEventID = &missingEventID
	missing.LogicalKey = "event:" + eventRule.ID + ":" + missingEventID
	missing.DedupeKey = missing.LogicalKey + ":attempt:1"
	missing.Retryable = false
	missing.RetryAt = nil
	if err := store.DB.Create(&missing).Error; err != nil {
		t.Fatalf("create Automation Run with unavailable event: %v", err)
	}
	missingDetail := readDetail(missing.ID)
	if missingDetail.Source.Kind != "event" || missingDetail.Source.Available || missingDetail.Source.EventID == nil ||
		*missingDetail.Source.EventID != missingEventID || missingDetail.Source.AggregateType != nil ||
		missingDetail.Source.AggregateID != nil || missingDetail.Source.Action != nil || missingDetail.Source.OccurredAt != nil ||
		missingDetail.Source.ScheduledFor != nil {
		t.Fatalf("unavailable event source detail = %#v", missingDetail.Source)
	}

	scheduleRule := automationRuleByPreset(t, router, automationPresetDailyToday)
	enabled = performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+scheduleRule.ID+"/enable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, scheduleRule.Version)},
	)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable schedule Automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	var persistedScheduleRule models.AutomationRule
	if err := store.DB.First(&persistedScheduleRule, "id = ?", scheduleRule.ID).Error; err != nil {
		t.Fatalf("load schedule Automation Rule: %v", err)
	}
	if persistedScheduleRule.NextRunAt == nil {
		t.Fatalf("enabled schedule Automation has no next run: %#v", persistedScheduleRule)
	}
	dueAt, err := time.Parse(time.RFC3339Nano, *persistedScheduleRule.NextRunAt)
	if err != nil {
		t.Fatalf("parse schedule next run: %v", err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return dueAt }}}
	if err := service.projectDueAutomationRule(scheduleRule.ID, dueAt); err != nil {
		t.Fatalf("project schedule Automation: %v", err)
	}
	var scheduleRun models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND trigger_type = 'schedule'", scheduleRule.ID).Take(&scheduleRun).Error; err != nil {
		t.Fatalf("load schedule Automation Run: %v", err)
	}
	scheduleDetail := readDetail(scheduleRun.ID)
	if scheduleDetail.Source.Kind != "schedule" || !scheduleDetail.Source.Available || scheduleDetail.Source.EventID != nil ||
		scheduleDetail.Source.AggregateType != nil || scheduleDetail.Source.AggregateID != nil || scheduleDetail.Source.Action != nil ||
		scheduleDetail.Source.OccurredAt != nil || scheduleDetail.Source.ScheduledFor == nil || scheduleRun.ScheduledFor == nil ||
		*scheduleDetail.Source.ScheduledFor != normalizeTimestamp(*scheduleRun.ScheduledFor) {
		t.Fatalf("schedule source detail = %#v", scheduleDetail.Source)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/automations/runs", nil, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list Automation Runs = %d: %s", listed.Code, listed.Body.String())
	}
	var rawList struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &rawList); err != nil {
		t.Fatalf("decode raw Automation Run list: %v", err)
	}
	if len(rawList.Data) < 4 {
		t.Fatalf("Automation Run list omitted fixtures: %#v", rawList.Data)
	}
	for _, run := range rawList.Data {
		if _, exists := run["source"]; exists {
			t.Fatalf("Automation Run list exposed source: %#v", run)
		}
		if _, exists := run["retry_chain"]; exists {
			t.Fatalf("Automation Run list exposed retry chain: %#v", run)
		}
	}
}

func TestAutomationRunDetailRejectsNoncanonicalIDAndReturnsNotFound(t *testing.T) {
	router := newTestAPI(t)
	uppercase := strings.ToUpper(uuid.NewString())
	for name, id := range map[string]string{
		"invalid":   "not-a-run-id",
		"uppercase": uppercase,
		"spaced":    "%20" + uuid.NewString() + "%20",
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodGet, "/api/v1/automations/runs/"+id, nil, nil)
			if response.Code != http.StatusBadRequest || responseErrorCode(t, response.Body.Bytes()) != "INVALID_AUTOMATION_RUN_ID" {
				t.Fatalf("response = %d: %s", response.Code, response.Body.String())
			}
		})
	}
	missing := performRequest(router, http.MethodGet, "/api/v1/automations/runs/"+uuid.NewString(), nil, nil)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing.Body.Bytes()) != "AUTOMATION_RUN_NOT_FOUND" {
		t.Fatalf("missing Automation Run = %d: %s", missing.Code, missing.Body.String())
	}
}

func TestAutomationRunListUsesOneReadTransactionForCountPageAndRuleLookup(t *testing.T) {
	router, store := newProjectTestAPI(t)
	rule := automationRuleByPreset(t, router, automationPresetDailyToday)
	run := createSkippedScheduleAutomationRunForTest(t, store, rule)

	const callbackName = "test_automation_run_list_read_transaction"
	var sharedTransaction *sql.Tx
	runQueries := 0
	ruleQueries := 0
	if err := store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table != "automation_runs" && db.Statement.Table != "automation_rules" {
			return
		}
		transaction, ok := db.Statement.ConnPool.(*sql.Tx)
		if !ok {
			db.AddError(fmt.Errorf("automation list query for %s did not use a SQL transaction", db.Statement.Table))
			return
		}
		if sharedTransaction == nil {
			sharedTransaction = transaction
		} else if transaction != sharedTransaction {
			db.AddError(fmt.Errorf("automation list query for %s used a different SQL transaction", db.Statement.Table))
			return
		}
		if db.Statement.Table == "automation_runs" {
			runQueries++
		} else {
			ruleQueries++
		}
	}); err != nil {
		t.Fatalf("register Automation Run list transaction assertion: %v", err)
	}
	t.Cleanup(func() { _ = store.DB.Callback().Query().Remove(callbackName) })

	response := performRequest(
		router, http.MethodGet,
		"/api/v1/automations/runs?rule_id="+rule.ID+"&status=skipped&page=1&page_size=1",
		nil, nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list Automation Runs in read transaction = %d: %s", response.Code, response.Body.String())
	}
	var envelope automationRunListEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode Automation Run transaction list: %v", err)
	}
	if envelope.Meta.Page != 1 || envelope.Meta.PageSize != 1 || envelope.Meta.Total != 1 ||
		len(envelope.Data) != 1 || envelope.Data[0].ID != run.ID {
		t.Fatalf("Automation Run transaction list = %#v", envelope)
	}
	if sharedTransaction == nil || runQueries != 2 || ruleQueries != 1 {
		t.Fatalf("Automation Run list transaction queries: tx=%p runs=%d rules=%d", sharedTransaction, runQueries, ruleQueries)
	}
}

func TestAutomationRunDetailTreatsMissingRelatedRuleAsIntegrityFailure(t *testing.T) {
	router, store := newProjectTestAPI(t)
	rule := automationRuleByPreset(t, router, automationPresetDailyToday)
	run := createSkippedScheduleAutomationRunForTest(t, store, rule)
	if err := store.DB.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("disable foreign keys for corrupt Automation fixture: %v", err)
	}
	t.Cleanup(func() { _ = store.DB.Exec("PRAGMA foreign_keys = ON").Error })
	if err := store.DB.Delete(&models.AutomationRule{}, "id = ?", rule.ID).Error; err != nil {
		t.Fatalf("delete related Automation Rule: %v", err)
	}
	if err := store.DB.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("restore foreign keys after corrupt Automation fixture: %v", err)
	}

	response := performRequest(router, http.MethodGet, "/api/v1/automations/runs/"+run.ID, nil, nil)
	if response.Code != http.StatusInternalServerError || responseErrorCode(t, response.Body.Bytes()) != "INTERNAL_ERROR" {
		t.Fatalf("Automation Run with missing related Rule = %d: %s", response.Code, response.Body.String())
	}
}

func createSkippedScheduleAutomationRunForTest(
	t *testing.T,
	store *database.Store,
	rule automationRuleOutput,
) models.AutomationRun {
	t.Helper()
	configJSON, err := encodeAutomationConfig(rule.Config)
	if err != nil {
		t.Fatalf("encode Automation Run test config: %v", err)
	}
	actionJSON, err := json.Marshal(automationScheduleActionSnapshot(rule.PresetKey))
	if err != nil {
		t.Fatalf("encode Automation Run test action: %v", err)
	}
	scheduledFor := formatInboxTimestamp(time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC))
	endedAt := formatInboxTimestamp(time.Date(2026, 9, 8, 9, 0, 1, 0, time.UTC))
	errorCode := "SCHEDULE_WINDOW_EXPIRED"
	logicalKey := "schedule:" + rule.ID + ":" + scheduledFor
	run := models.AutomationRun{
		ID: uuid.NewString(), RuleID: rule.ID, RuleVersion: rule.Version,
		TriggerType: "schedule", ScheduledFor: &scheduledFor,
		LogicalKey: logicalKey, DedupeKey: logicalKey + ":attempt:1",
		Status: "skipped", Attempt: 1, ConfigSnapshotJSON: configJSON,
		ActionSnapshotJSON: string(actionJSON), ErrorCode: &errorCode,
		ResultSummary: "离线期间错过的旧计划窗口已折叠，不创建过期提醒。",
		StartedAt:     scheduledFor, EndedAt: endedAt,
	}
	if err := store.DB.Create(&run).Error; err != nil {
		t.Fatalf("create skipped Automation Run fixture: %v", err)
	}
	return run
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
