package api

import (
	"context"
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
