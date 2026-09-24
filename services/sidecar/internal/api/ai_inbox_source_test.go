package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiInboxSourceToolForTest(t *testing.T, service *API, store *database.Store, scopes ...string) *aiWorkspaceTool {
	t.Helper()
	provider := createCompactionTestProvider(t, store, service.options.Now(), uuid.NewString())
	return &aiWorkspaceTool{api: service, name: "workspace_inbox_source", policy: harness.NewCapabilities(scopes...), providerID: provider.ID, configVersion: provider.ConfigVersion}
}

func readAIInboxSourceForTest(t *testing.T, tool *aiWorkspaceTool, id string) (aiInboxSourceResult, string) {
	t.Helper()
	encoded, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q}`, id)))
	if err != nil {
		t.Fatalf("source lookup: %v", err)
	}
	var result aiInboxSourceResult
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	if result.InboxItemID != id || result.InboxItemVersion < 1 || result.AsOf == "" || result.RequiredScopes == nil || result.Related == nil {
		t.Fatalf("incomplete source envelope: %s", encoded)
	}
	for _, forbidden := range []string{`"payload_json"`, `"source_event_key"`, `"source_entity_id"`, `"summary"`, `"notes"`, `"content_text"`, `"reference_url"`, `"external_link"`, `"title"`, `"label"`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("source metadata leaked %s: %s", forbidden, encoded)
		}
	}
	return result, encoded
}

func loadNativeInboxSourceForTest(t *testing.T, store *database.Store, kind, id string) models.InboxItem {
	t.Helper()
	var inbox models.InboxItem
	if err := store.DB.Where("source_entity_type=? AND source_entity_id=?", kind, id).Order("created_at,id").First(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	return inbox
}

func TestAIInboxSourceStrictInputAndManual(t *testing.T) {
	router, store, service, _, _ := aiActionTestFixture(t)
	item := createInboxItemForTest(t, router, `{"title":"Private manual title","summary":"PRIVATE MANUAL SUMMARY"}`, "")
	tool := aiInboxSourceToolForTest(t, service, store, "work")
	got, raw := readAIInboxSourceForTest(t, tool, item.ID)
	if got.LookupStatus != "none" || got.SourceType != "manual" || got.Subject != nil || len(got.Related) != 0 || got.SourceVersion != nil || got.SnapshotMatchesCurrent != nil || strings.Contains(raw, "PRIVATE") {
		t.Fatalf("manual source invented an identity: %s", raw)
	}
	valid := fmt.Sprintf(`{"inbox_item_id":%q}`, item.ID)
	for _, input := range []string{
		`null`, `[]`, `{}`, `{"inbox_item_id":null}`, `{"inbox_item_id":1}`,
		`{"inbox_item_id":"not-a-uuid"}`, fmt.Sprintf(`{"inbox_item_id":%q}`, strings.ToUpper(item.ID)),
		fmt.Sprintf(`{"inbox_item_id":%q}`, " "+item.ID), fmt.Sprintf(`{"inbox_item_id":%q}`, strings.ReplaceAll(item.ID, "-", "")),
		fmt.Sprintf(`{"Inbox_Item_Id":%q}`, item.ID), fmt.Sprintf(`{"INBOX_ITEM_ID":%q}`, item.ID),
		fmt.Sprintf(`{"inbox_item_id":%q,"inbox_item_id":%q}`, item.ID, item.ID),
		fmt.Sprintf(`{"inbox_item_id":%q,"source_entity_type":"task"}`, item.ID), valid + `{}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(input)); err == nil {
			t.Errorf("accepted malformed source query: %s", input)
		}
	}
	denied := *tool
	denied.policy = harness.NewCapabilities("outputs", "clients", "finance", "actions")
	if _, err := denied.Execute(context.Background(), []byte(valid)); err == nil {
		t.Fatal("extra scopes substituted for required work consent")
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"inbox_item_id":%q}`, uuid.NewString()))); err == nil {
		t.Fatal("nonexistent Inbox accepted")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND version=?", 1, item.ID, item.Version)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

func assertAIInboxSourceTargetForTest(t *testing.T, got aiInboxSourceResult, kind, id, status string, version int64) {
	t.Helper()
	if got.LookupStatus != "available" || got.Subject == nil || got.Subject.Type != kind || got.Subject.ID != id || got.Subject.Route == "" || len(got.RequiredScopes) != 0 {
		t.Fatalf("source identity mismatch: %+v", got)
	}
	if version == 0 && got.Subject.Version != nil || version != 0 && (got.Subject.Version == nil || *got.Subject.Version != version) || status == "" && got.Subject.Status != nil || status != "" && (got.Subject.Status == nil || *got.Subject.Status != status) {
		t.Fatalf("source current metadata mismatch: %+v", got.Subject)
	}
}

func TestAIInboxSourceNativeWorkMappingsAndReadOnly(t *testing.T) {
	router, store, service, _, _ := aiActionTestFixture(t)
	tool := aiInboxSourceToolForTest(t, service, store, "work")
	ctx := context.Background()

	task := createTaskForTaskFacts(t, router, `{"title":"PRIVATE TASK TITLE","description":"PRIVATE TASK BODY"}`)
	blocked := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/block", []byte(`{"reason":"PRIVATE BLOCK REASON"}`), map[string]string{"If-Match": `"1"`})
	if blocked.Code != http.StatusOK {
		t.Fatal(blocked.Body.String())
	}
	blockedInbox := loadNativeInboxSourceForTest(t, store, "task", task.ID)

	due := seedTaskForDueProjection(t, store, uuid.NewString(), "PRIVATE DUE TITLE", "todo", service.options.Now().Add(-time.Hour))
	if err := service.projectDueTasks(ctx); err != nil {
		t.Fatal(err)
	}
	dueInbox := loadNativeInboxSourceForTest(t, store, "task_due", due.ID)

	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, rule.Version)})
	if enabled.Code != http.StatusOK {
		t.Fatal(enabled.Body.String())
	}
	project := createProjectForTest(t, router, `{"name":"PRIVATE PROJECT TITLE"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	projectInbox := loadNativeInboxSourceForTest(t, store, "project_completion", project.ID)
	var automation models.AutomationRun
	if err := store.DB.First(&automation, "rule_id=? AND status='succeeded'", rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	automationInbox := loadNativeInboxSourceForTest(t, store, "automation", automation.ID)

	roadmapResponse := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{"title":"PRIVATE ROADMAP TITLE","description":"PRIVATE ROADMAP BODY","year":2026,"quarter":3,"target_date":"2026-09-18","status":"active"}`), nil)
	if roadmapResponse.Code != http.StatusCreated {
		t.Fatal(roadmapResponse.Body.String())
	}
	milestone := decodeRoadmapMilestoneResponse(t, roadmapResponse.Body.Bytes())
	roadmapInbox := loadNativeInboxSourceForTest(t, store, "roadmap_milestone", milestone.ID)

	reminder := seedReminderForProjection(t, store, uuid.NewString(), "PRIVATE REMINDER TITLE", formatInboxTimestamp(service.options.Now().Add(-time.Minute)), "scheduled")
	if err := service.projectDueReminders(ctx); err != nil {
		t.Fatal(err)
	}
	reminderInbox := loadNativeInboxSourceForTest(t, store, "reminder", reminder.ID)
	var eventCount int64
	if err := store.DB.Table("workflow_events").Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                   string
		inbox                  models.InboxItem
		kind, id, status       string
		version, sourceVersion int64
		matches                *bool
	}{
		{"blocked", blockedInbox, "task", task.ID, "blocked", 2, 2, nil},
		{"due", dueInbox, "task", due.ID, "todo", 1, 0, nil},
		{"project", projectInbox, "project", project.ID, "completed", project.Version, project.Version, nil},
		{"roadmap", roadmapInbox, "roadmap_milestone", milestone.ID, "active", milestone.Version, milestone.Version, nil},
		{"reminder", reminderInbox, "reminder", reminder.ID, "fired", 2, 0, nil},
		{"automation", automationInbox, "automation_run", automation.ID, "succeeded", 0, 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, raw := readAIInboxSourceForTest(t, tool, test.inbox.ID)
			assertAIInboxSourceTargetForTest(t, got, test.kind, test.id, test.status, test.version)
			if strings.Contains(raw, "PRIVATE") || test.sourceVersion == 0 && got.SourceVersion != nil || test.sourceVersion != 0 && (got.SourceVersion == nil || *got.SourceVersion != test.sourceVersion) {
				t.Fatalf("invalid metadata source snapshot: %s", raw)
			}
			if test.name == "automation" {
				if len(got.Related) != 1 || got.Related[0].Type != "automation_rule" || got.Related[0].ID != rule.ID || got.Related[0].Version == nil || *got.Related[0].Version != rule.Version+1 || got.Related[0].Status != nil || got.Related[0].Route != aiAutomationRoute("rule", rule.ID) || got.Subject.Route != aiAutomationRoute("run", automation.ID) {
					t.Fatalf("wrong automation relationships: %s", raw)
				}
			} else if got.Subject.Route != searchRoute(test.kind, test.id) || len(got.Related) != 0 {
				t.Fatalf("unexpected related objects or route: %s", raw)
			}
		})
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events", eventCount)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)

	// Existing historical events remain locatable after current facts change.
	unblocked := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/unblock", []byte(`{}`), map[string]string{"If-Match": `"2"`})
	if unblocked.Code != http.StatusOK {
		t.Fatal(unblocked.Body.String())
	}
	got, _ := readAIInboxSourceForTest(t, tool, blockedInbox.ID)
	assertAIInboxSourceTargetForTest(t, got, "task", task.ID, "todo", 3)
	if got.SourceVersion == nil || *got.SourceVersion != 2 || got.SnapshotMatchesCurrent == nil || *got.SnapshotMatchesCurrent {
		t.Fatalf("historical block treated as current: %+v", got)
	}
	changed := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+due.ID, []byte(`{"due_date":"2026-09-30T12:00:00Z"}`), map[string]string{"If-Match": `"1"`})
	if changed.Code != http.StatusOK {
		t.Fatal(changed.Body.String())
	}
	got, _ = readAIInboxSourceForTest(t, tool, dueInbox.ID)
	if got.LookupStatus != "available" || got.SourceVersion != nil || got.SnapshotMatchesCurrent == nil || *got.SnapshotMatchesCurrent {
		t.Fatalf("rescheduled Task source treated as current: %+v", got)
	}
	cleared := performRequest(router, http.MethodPatch, "/api/v1/tasks/"+due.ID, []byte(`{"due_date":null}`), map[string]string{"If-Match": `"2"`})
	if cleared.Code != http.StatusOK {
		t.Fatal(cleared.Body.String())
	}
	got, _ = readAIInboxSourceForTest(t, tool, dueInbox.ID)
	assertAIInboxSourceTargetForTest(t, got, "task", due.ID, "todo", 3)
	if got.SourceVersion != nil || got.SnapshotMatchesCurrent == nil || *got.SnapshotMatchesCurrent {
		t.Fatalf("cleared Task deadline lost historical identity: %+v", got)
	}
}

func assertAIInboxProtectedScopeForTest(t *testing.T, tool *aiWorkspaceTool, id, required string, blockedTables []string, hiddenIDs ...string) {
	t.Helper()
	denied := *tool
	denied.policy = harness.NewCapabilities("work", "actions")
	queries := 0
	callbackName := "test:inbox_source_scope_" + uuid.NewString()
	if err := tool.api.db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		for _, table := range blockedTables {
			if tx.Statement.Table == table {
				queries++
				t.Errorf("missing %s scope still queried protected table %s", required, table)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer tool.api.db.Callback().Query().Remove(callbackName)
	got, raw := readAIInboxSourceForTest(t, &denied, id)
	if got.LookupStatus != "permission_required" || len(got.RequiredScopes) != 1 || got.RequiredScopes[0] != required || got.Subject != nil || len(got.Related) != 0 || got.SourceVersion != nil || got.SnapshotMatchesCurrent != nil || queries != 0 {
		t.Fatalf("protected source envelope=%s", raw)
	}
	for _, hidden := range hiddenIDs {
		if hidden != "" && strings.Contains(raw, hidden) {
			t.Fatalf("protected identity disclosed: %s", raw)
		}
	}
}

func TestAIInboxSourceNativeProtectedMappings(t *testing.T) {
	t.Run("Artifact requires outputs and keeps exact batch", func(t *testing.T) {
		f := newAIProjectOutputsTestFixture(t)
		tool := aiInboxSourceToolForTest(t, f.tool.api, f.store, "work", "outputs")
		assertAIInboxProtectedScopeForTest(t, tool, f.inbox.ID, "outputs", []string{"task_artifacts", "task_submissions", "tasks"}, f.output.Artifacts[0].ID, f.task.ID, f.output.Submission.ID)
		got, raw := readAIInboxSourceForTest(t, tool, f.inbox.ID)
		assertAIInboxSourceTargetForTest(t, got, "artifact", f.output.Artifacts[0].ID, "", 0)
		if got.Subject.Route != taskSubmissionRoute(f.task.ID, f.output.Submission.ID) || len(got.Related) != 2 || got.Related[0].Type != "task" || got.Related[0].ID != f.task.ID || got.Related[1].Type != "task_submission" || got.Related[1].ID != f.output.Submission.ID || got.Related[1].Route != got.Subject.Route || got.Related[1].Version != nil || got.Related[1].Status == nil || *got.Related[1].Status != "pending_review" || strings.Contains(raw, "PRIVATE") {
			t.Fatalf("artifact ownership/source privacy mismatch: %s", raw)
		}
		assertDatabaseCount(t, f.store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='pending_review'", 1, f.output.Submission.ID)
	})
	t.Run("Followup requires clients and current Client relationship", func(t *testing.T) {
		router, store, service, _, _ := aiActionTestFixture(t)
		client := createClientForTest(t, router, `{"name":"PRIVATE CLIENT NAME","notes":"PRIVATE CLIENT NOTES"}`, nil)
		at := formatInboxTimestamp(service.options.Now().Add(-time.Minute))
		followup := models.ClientFollowup{ID: uuid.NewString(), ClientID: client.ID, AssignedActorID: models.BuiltinOwnerActorID, ScheduledAt: at, Timezone: "UTC", Channel: "phone", Purpose: "PRIVATE FOLLOWUP PURPOSE", Status: "planned", Priority: "normal", Version: 1, CreatedAt: formatInboxTimestamp(service.options.Now().Add(-time.Hour)), UpdatedAt: formatInboxTimestamp(service.options.Now().Add(-time.Hour))}
		if err := store.DB.Create(&followup).Error; err != nil {
			t.Fatal(err)
		}
		if err := service.projectDueClientFollowups(context.Background()); err != nil {
			t.Fatal(err)
		}
		inbox := loadNativeInboxSourceForTest(t, store, "client_followup", followup.ID)
		tool := aiInboxSourceToolForTest(t, service, store, "work", "clients")
		assertAIInboxProtectedScopeForTest(t, tool, inbox.ID, "clients", []string{"client_followups", "clients"}, followup.ID, client.ID)
		got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
		assertAIInboxSourceTargetForTest(t, got, "client_followup", followup.ID, "planned", 1)
		if got.SourceVersion == nil || *got.SourceVersion != 1 || got.SnapshotMatchesCurrent == nil || !*got.SnapshotMatchesCurrent || len(got.Related) != 1 || got.Related[0].Type != "client" || got.Related[0].ID != client.ID || got.Subject.Route != aiClientRecordRoute(client.ID, "followup", followup.ID) || strings.Contains(raw, "PRIVATE") {
			t.Fatalf("wrong followup relation: %s", raw)
		}
	})
	t.Run("Invoice requires finance without importing clients scope", func(t *testing.T) {
		router, store, service, _, _ := aiActionTestFixture(t)
		client := createClientForTest(t, router, `{"name":"PRIVATE INVOICE CLIENT","notes":"PRIVATE INVOICE CLIENT NOTES"}`, nil)
		invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":98761234,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-18"}`, client.ID), nil)
		invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
		if err := service.projectDueInvoices(context.Background()); err != nil {
			t.Fatal(err)
		}
		inbox := loadNativeInboxSourceForTest(t, store, "invoice_due", invoice.ID)
		tool := aiInboxSourceToolForTest(t, service, store, "work", "finance")
		assertAIInboxProtectedScopeForTest(t, tool, inbox.ID, "finance", []string{"invoices", "clients", "financial_entries"}, invoice.ID, client.ID)
		got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
		assertAIInboxSourceTargetForTest(t, got, "invoice", invoice.ID, "sent", invoice.Version)
		if got.SourceVersion == nil || *got.SourceVersion != invoice.Version || len(got.Related) != 0 || strings.Contains(raw, client.ID) || strings.Contains(raw, "98761234") || strings.Contains(raw, "PRIVATE") {
			t.Fatalf("invoice metadata imported financial/body/client data: %s", raw)
		}
	})
}

// Corruption is confined to a private test database. Exact source guards are
// removed only to simulate an old/malformed import; every mutation rolls back.
func withCorruptAIInboxSourceForTest(t *testing.T, tool *aiWorkspaceTool, inboxID, statement string, args ...any) {
	t.Helper()
	rollback := errors.New("rollback source corruption fixture")
	err := tool.api.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(statement, args...).Error; err != nil {
			return err
		}
		service := &API{db: tx, options: tool.api.options, maintenance: tool.api.maintenance}
		copyTool := *tool
		copyTool.api = service
		got, raw := readAIInboxSourceForTest(t, &copyTool, inboxID)
		if got.LookupStatus != "unavailable" || got.Subject != nil || len(got.Related) != 0 || got.SourceVersion != nil || got.SnapshotMatchesCurrent != nil || len(got.RequiredScopes) != 0 {
			t.Errorf("corrupt/missing source retained actionable identity: %s", raw)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("corruption fixture transaction: %v", err)
	}
}

func TestAIInboxSourceContentDeletedMissingAndCorruptHistory(t *testing.T) {
	router, store, service, _, _ := aiActionTestFixture(t)
	created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Source deletion","platform":"web","status":"scheduled","scheduled_at":"2026-09-18T11:00:00Z","scheduled_timezone":"UTC"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	item := decodeContentItemResponse(t, created.Body.Bytes())
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	inbox := loadNativeInboxSourceForTest(t, store, "content_item", item.ID)
	tool := aiInboxSourceToolForTest(t, service, store, "work")
	if err := store.DB.Exec("DROP TRIGGER content_item_inbox_source_identity_immutable").Error; err != nil {
		t.Fatal(err)
	}
	missing := uuid.NewString()
	for _, test := range []struct {
		name, sql string
		args      []any
	}{
		{"mismatched payload id", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.content_item_id',?) WHERE id=?", []any{missing, inbox.ID}},
		{"missing actual target", "UPDATE inbox_items SET source_entity_id=?,source_event_key=?,payload_json=json_set(payload_json,'$.content_item_id',?) WHERE id=?", []any{missing, contentItemInboxEventKey(missing, "publish_due", 1), missing, inbox.ID}},
		{"wrong event key", "UPDATE inbox_items SET source_event_key=? WHERE id=?", []any{"PRIVATE-CORRUPTED-KEY", inbox.ID}},
		{"unknown event type", "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.event_type','publish_succeeded') WHERE id=?", []any{inbox.ID}},
		{"future event version", "UPDATE inbox_items SET source_event_key=?,payload_json=json_set(payload_json,'$.content_version',2) WHERE id=?", []any{contentItemInboxEventKey(item.ID, "publish_due", 2), inbox.ID}},
		{"version not canonical", "UPDATE inbox_items SET source_event_key=? WHERE id=?", []any{"content:" + item.ID + ":publish_due:01", inbox.ID}},
	} {
		t.Run(test.name, func(t *testing.T) { withCorruptAIInboxSourceForTest(t, tool, inbox.ID, test.sql, test.args...) })
	}
	got, _ := readAIInboxSourceForTest(t, tool, inbox.ID)
	assertAIInboxSourceTargetForTest(t, got, "content_item", item.ID, "scheduled", item.Version)
	archived := performRequest(router, http.MethodPatch, "/api/v1/content-items/"+item.ID, []byte(`{"status":"archived"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)})
	if archived.Code != http.StatusOK {
		t.Fatal(archived.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/content-items/"+item.ID+"?confirm=true", nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version+1)})
	if deleted.Code != http.StatusOK {
		t.Fatal(deleted.Body.String())
	}
	got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
	if got.LookupStatus != "deleted" || got.Subject != nil || len(got.Related) != 0 || got.SourceVersion != nil || got.SnapshotMatchesCurrent != nil || strings.Contains(raw, item.ID) {
		t.Fatalf("deleted source returned active identity: %s", raw)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND source_deleted_at IS NOT NULL AND payload_json=?", 1, inbox.ID, inbox.PayloadJSON)
}

func TestAIInboxSourceRejectsCrossOwnerArtifactAndDeletedProtectedIdentity(t *testing.T) {
	f := newAIProjectOutputsTestFixture(t)
	tool := aiInboxSourceToolForTest(t, f.tool.api, f.store, "work", "outputs")
	if err := f.store.DB.Exec("DROP TRIGGER trg_inbox_task_artifact_source_identity_immutable").Error; err != nil {
		t.Fatal(err)
	}
	other := createTaskForTaskFacts(t, f.router, `{"title":"Another real owner"}`)
	withCorruptAIInboxSourceForTest(t, tool, f.inbox.ID, "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.task_id',?) WHERE id=?", other.ID, f.inbox.ID)
	withCorruptAIInboxSourceForTest(t, tool, f.inbox.ID, "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.submission_id',?) WHERE id=?", uuid.NewString(), f.inbox.ID)
	// A real native terminal transition is required before deletion marking.
	resolved := performRequest(f.router, http.MethodPost, "/api/v1/inbox-items/"+f.inbox.ID+"/resolve", []byte(`{"reason":"Reviewed the source"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.inbox.Version)})
	if resolved.Code != http.StatusOK {
		t.Fatal(resolved.Body.String())
	}
	if err := f.store.DB.Model(&models.InboxItem{}).Where("id=?", f.inbox.ID).Update("source_deleted_at", formatInboxTimestamp(f.tool.api.options.Now())).Error; err != nil {
		t.Fatal(err)
	}
	assertAIInboxProtectedScopeForTest(t, tool, f.inbox.ID, "outputs", []string{"task_artifacts", "task_submissions", "tasks"}, f.output.Artifacts[0].ID, f.task.ID, f.output.Submission.ID)
	got, raw := readAIInboxSourceForTest(t, tool, f.inbox.ID)
	if got.LookupStatus != "deleted" || got.Subject != nil || len(got.Related) != 0 || strings.Contains(raw, f.output.Artifacts[0].ID) {
		t.Fatalf("tombstone exposed retained live row identity: %s", raw)
	}
}

func TestAIInboxSourceInvoiceRejectsStateDateContradiction(t *testing.T) {
	router, store, service, _, _ := aiActionTestFixture(t)
	client := createClientForTest(t, router, `{"name":"Invoice proof client"}`, nil)
	invoice := createInvoiceForTest(t, router, fmt.Sprintf(`{"client_id":%q,"amount_minor":1000,"currency":"CNY","issue_date":"2026-09-01","due_date":"2026-09-18"}`, client.ID), nil)
	invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
	if err := service.projectDueInvoices(context.Background()); err != nil {
		t.Fatal(err)
	}
	inbox := loadNativeInboxSourceForTest(t, store, "invoice_due", invoice.ID)
	tool := aiInboxSourceToolForTest(t, service, store, "work", "finance")
	if err := store.DB.Exec("DROP TRIGGER invoice_due_inbox_source_identity_immutable").Error; err != nil {
		t.Fatal(err)
	}
	// Both key and payload say overdue, but equal calendar dates mean due.
	withCorruptAIInboxSourceForTest(t, tool, inbox.ID, "UPDATE inbox_items SET source_event_key=?,payload_json=json_set(payload_json,'$.due_state','overdue') WHERE id=?", invoiceDueEventKey(invoice.ID, "overdue", "2026-09-18", "2026-09-18"), inbox.ID)
	withCorruptAIInboxSourceForTest(t, tool, inbox.ID, "UPDATE inbox_items SET source_event_key=?,payload_json=json_set(payload_json,'$.due_state','due_soon') WHERE id=?", invoiceDueEventKey(invoice.ID, "due_soon", "2026-09-18", "2026-09-18"), inbox.ID)
	withCorruptAIInboxSourceForTest(t, tool, inbox.ID, "UPDATE inbox_items SET payload_json=json_set(payload_json,'$.occurrence_date','2026-02-30') WHERE id=?", inbox.ID)
}

func TestAIInboxSourceUnknownMaintenanceAndCancellation(t *testing.T) {
	_, store, service, _, _ := aiActionTestFixture(t)
	tool := aiInboxSourceToolForTest(t, service, store, "work", "outputs", "finance", "clients")
	for _, kind := range []string{"agent_run", "tasks", "content_item; DROP TABLE tasks", "PRIVATE_UNTRUSTED_SOURCE"} {
		id, key := uuid.NewString(), "PRIVATE-UNKNOWN-KEY-"+uuid.NewString()
		inbox := models.InboxItem{ID: uuid.NewString(), Kind: "event", Title: "PRIVATE TITLE", Summary: "PRIVATE SUMMARY", SourceEntityType: kind, SourceEntityID: &id, SourceEventKey: &key, Priority: "P2", Status: "open", ResolutionPolicy: "manual", PayloadJSON: `{"task_id":"PRIVATE FAKE ID","notes":"PRIVATE BODY"}`, Version: 1, CreatedAt: formatInboxTimestamp(service.options.Now()), UpdatedAt: formatInboxTimestamp(service.options.Now())}
		if err := store.DB.Create(&inbox).Error; err != nil {
			t.Fatal(err)
		}
		got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
		if got.LookupStatus != "unsupported" || got.SourceType != "unsupported" || got.Subject != nil || len(got.Related) != 0 || strings.Contains(raw, id) || strings.Contains(raw, "PRIVATE") {
			t.Fatalf("unknown source escaped closed mapping: %s", raw)
		}
	}
	if err := service.projectSystemMaintenanceFailure(backupCreateMaintenanceIncident, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	inbox := loadNativeInboxSourceForTest(t, store, "system_maintenance", "backup:create")
	got, raw := readAIInboxSourceForTest(t, tool, inbox.ID)
	if got.LookupStatus != "unsupported" || got.Subject != nil || len(got.Related) != 0 || strings.Contains(raw, "backup:create") {
		t.Fatalf("maintenance fabricated an entity: %s", raw)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"inbox_item_id":%q}`, inbox.ID))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source query=%v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
}

// Only native commands and the real due projector establish this source.
// The mock Provider receives each next target from actual tool output.
func TestAIInboxSourceHarnessNativeContentScheduleApproval(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
			created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Source itinerary","platform":"website","status":"scheduled","scheduled_at":"2026-09-21T11:00:00Z","scheduled_timezone":"UTC","notes":"PRIVATE SOURCE NOTES","external_link":"https://private.invalid/source-secret"}`), nil)
			if created.Code != http.StatusCreated {
				t.Fatal(created.Body.String())
			}
			item := decodeContentItemResponse(t, created.Body.Bytes())
			if err := service.projectDueContentItems(context.Background()); err != nil {
				t.Fatal(err)
			}
			inbox := loadNativeInboxSourceForTest(t, store, "content_item", item.ID)
			var inboxID, contentID, proposalID string
			var contentVersion int64
			readSource := func(payload map[string]any, call int, expectedVersion int64, expectedStatus string, matches bool) {
				t.Helper()
				raw := aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("inbox-source-%d", call), "workspace_inbox_source")
				var got aiInboxSourceResult
				if json.Unmarshal([]byte(raw), &got) != nil || got.LookupStatus != "available" || got.SourceType != "content_item" || got.InboxItemID != inboxID || got.InboxItemStatus != expectedStatus || got.Subject == nil || got.Subject.Type != "content_item" || got.Subject.ID != item.ID || got.Subject.Version == nil || *got.Subject.Version != expectedVersion || got.Subject.Status == nil || *got.Subject.Status != "scheduled" || got.Subject.Route != searchRoute("content_item", item.ID) || got.SourceVersion == nil || *got.SourceVersion != item.Version || got.SnapshotMatchesCurrent == nil || *got.SnapshotMatchesCurrent != matches || len(got.Related) != 0 || len(got.RequiredScopes) != 0 {
					t.Errorf("native source metadata mismatch: %s", raw)
					return
				}
				contentID, contentVersion = got.Subject.ID, *got.Subject.Version
			}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				// workspace_get is explicitly authorized to read content notes;
				// this journey does not use it: source metadata alone must stay clean.
				for _, secret := range []string{"PRIVATE SOURCE NOTES", "private.invalid/source-secret", *inbox.SourceEventKey} {
					if strings.Contains(string(raw), secret) {
						t.Errorf("source metadata leaked %q", secret)
					}
				}
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("inbox-source-%d", call), name, args)
				}
				finish := func(text string) {
					writeAIBudgetTextTurn(w, protocol, text+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_inbox_source") {
						t.Error("source tool was not deferred")
					}
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_inbox_source") {
						t.Error("inbox guide did not expose source query")
					}
					emit("workspace_search", `{"type":"inbox_item","query":"Source itinerary"}`)
				case 3:
					var page struct {
						Items []struct {
							ID string `json:"id"`
						} `json:"items"`
					}
					result := aiPlanRetryHarnessResult(t, payload, protocol, "inbox-source-2", "workspace_search")
					if json.Unmarshal([]byte(result), &page) != nil || len(page.Items) != 1 || page.Items[0].ID != inbox.ID {
						t.Errorf("native Inbox not found: %s", result)
						finish("未定位事项。")
						return
					}
					inboxID = page.Items[0].ID
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inboxID))
				case 4:
					readSource(payload, 3, item.Version, "open", true)
					emit("workspace_guide", `{"topic":"roadmap_content"}`)
				case 5:
					// list metadata includes full preparation counts without notes.
					emit("workspace_content_items", `{"view":"list","filters":{"platform":"website"}}`)
				case 6:
					var page struct {
						Items []aiContentHarnessItem `json:"items"`
					}
					result := aiPlanRetryHarnessResult(t, payload, protocol, "inbox-source-5", "workspace_content_items")
					if json.Unmarshal([]byte(result), &page) != nil || len(page.Items) != 1 || page.Items[0].ID != contentID || page.Items[0].Version != contentVersion || page.Items[0].ScheduledAt == nil || *page.Items[0].ScheduledAt != "2026-09-21T11:00:00Z" {
						t.Errorf("source target differed from current content: %s", result)
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"content_item.schedule","content_item_id":%q,"expected_version":%d,"changes":{"scheduled_at":"2026-09-23T13:00:00Z","scheduled_timezone":"UTC"}}`, contentID, contentVersion))
				case 7:
					var proposal struct {
						ID     string `json:"proposal_id"`
						Status string `json:"status"`
					}
					result := aiPlanRetryHarnessResult(t, payload, protocol, "inbox-source-6", "workspace_propose")
					if json.Unmarshal([]byte(result), &proposal) != nil || proposal.ID == "" || proposal.Status != "pending" {
						t.Errorf("missing pending proposal: %s", result)
					}
					proposalID = proposal.ID
					finish("已定位对应内容并提出改期建议，请先确认，尚未修改或发布。")
				case 8:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("new message inherited consent: %s", name)
						}
					}
					finish("未获得本轮读取授权，不能据此查询最新状态。")
				case 9:
					emit("workspace_guide", `{"topic":"inbox"}`)
				case 10:
					if aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("read-only refresh exposed writes")
					}
					emit("workspace_inbox_source", fmt.Sprintf(`{"inbox_item_id":%q}`, inboxID))
				case 11:
					readSource(payload, 10, item.Version+1, "resolved", false)
					finish("旧到期事项已解决；来源仍指向原内容，当前版本已改变，不代表内容已经发布。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					finish("停止。")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			var sessionID string
			send := func(message string, scopes []string, wantCalls int32) {
				t.Helper()
				request := map[string]any{"provider_id": provider.ID, "message": message}
				if sessionID != "" {
					request["session_id"] = sessionID
				}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != 200 || upstream.calls.Load() != wantCalls || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || t.Failed() {
					t.Fatalf("source journey calls=%d want=%d: %d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
				if sessionID == "" {
					sessionID = decodeAIBudgetMeta(t, response.Body.String()).SessionID
				}
			}
			send("查找 Source itinerary 对应的到期事项，定位真实来源后建议改到9月23日13点 UTC，不要发布。", []string{"work", "actions"}, 7)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=? AND scheduled_at='2026-09-21T11:00:00Z'", 1, item.ID, item.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='open' AND version=?", 1, inbox.ID, inbox.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal, "id=?", proposalID).Error; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
				if response.Code != 200 {
					t.Fatalf("approval=%d %s", response.Code, response.Body.String())
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=? AND scheduled_at='2026-09-23T13:00:00Z' AND status='scheduled' AND published_at IS NULL", 1, item.ID, item.Version+1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id=? AND status='resolved' AND version=? AND payload_json=? AND source_event_key=?", 1, inbox.ID, inbox.Version+1, inbox.PayloadJSON, *inbox.SourceEventKey)
			send("谢谢", nil, 8)
			send("重新授权只读，检查刚才旧事项对应的当前来源。", []string{"work"}, 11)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_id=? AND action='source_resolved'", 1, inbox.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%PRIVATE SOURCE%' OR content LIKE '%private.invalid/source-secret%'", 0)
		})
	}
}
