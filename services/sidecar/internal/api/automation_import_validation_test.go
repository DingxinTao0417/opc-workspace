package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

type automationImportFixture struct {
	business             businessExportPackage
	jsonBody             []byte
	zipBody              []byte
	projectRuleID        string
	invoiceRuleID        string
	agentRuleID          string
	projectRunID         string
	projectSourceEventID string
	projectResultID      string
	failedRunID          string
	retryRunID           string
	retryResultID        string
	deletedTaskID        string
	scheduleRunID        string
}

func TestAutomationBusinessImportPreflightAndApplyAcceptsPortableRunHistory(t *testing.T) {
	fixture := newAutomationImportFixture(t)

	for _, test := range []struct {
		name         string
		previewPath  string
		applyPath    string
		confirmation string
		body         []byte
	}{
		{
			name:         "JSON",
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importReplaceConfirmation,
			body:         fixture.jsonBody,
		},
		{
			name:         "ZIP",
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportReplaceConfirmation,
			body:         fixture.zipBody,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, store, _, _ := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, test.previewPath, test.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("portable Automation preview = %d: %s", preview.Code, preview.Body.String())
			}

			apply := performRequest(
				router, http.MethodPost, test.applyPath, test.body,
				map[string]string{"X-Import-Confirmation": test.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("portable Automation apply = %d: %s", apply.Code, apply.Body.String())
			}

			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 7)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND status = 'failed' AND attempt = 1", 1, fixture.failedRunID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND retry_of_run_id = ? AND status = 'succeeded' AND attempt = 2", 1, fixture.retryRunID, fixture.failedRunID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE trigger_type = 'schedule' AND status = 'succeeded'", 2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND status = 'fired'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND status = 'cancelled'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND status = 'scheduled' AND occurrence_number > 2", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id = ?", 0, fixture.deletedTaskID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'task_created_from_automation'", 1, fixture.deletedTaskID)
		})
	}
}

func TestAutomationBusinessImportAcceptsPortableNonretryableSafetyFailures(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	// The source router must share the fixture clock: rule-version proof
	// events written by the API must not drift past the frozen attempt
	// timestamps or the graph validation becomes time-of-day dependent.
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t, now)
	ruleOutput := configureAndEnableInvoiceAutomation(t, sourceRouter, "P1")
	var rule models.AutomationRule
	if err := sourceStore.DB.First(&rule, "id = ?", ruleOutput.ID).Error; err != nil {
		t.Fatalf("load safety-failure Invoice Automation Rule: %v", err)
	}

	wantRuns := make(map[string]string, 5)
	var actionInvalidRunID, actionInvalidSourceEventID, actionInvalidValidSnapshot string
	var sourceInvalidSourceEventID, sourceInvalidValidCurrent string
	var projectConflictItemID string
	for index, test := range []struct {
		name             string
		corruptSource    bool
		corruptAction    bool
		mismatchBinding  bool
		mismatchContract bool
		wantErrorCode    string
	}{
		{name: "invalid action snapshot", corruptAction: true, wantErrorCode: "ACTION_SNAPSHOT_INVALID"},
		{name: "valid action mismatches source binding", mismatchBinding: true, wantErrorCode: "ACTION_SNAPSHOT_INVALID"},
		{name: "invalid source event", corruptSource: true, wantErrorCode: "SOURCE_EVENT_INVALID"},
		{name: "invalid attempt contract", mismatchContract: true, wantErrorCode: "ATTEMPT_CONTRACT_INVALID"},
	} {
		t.Run("produce "+test.name, func(t *testing.T) {
			invoice, eventID := seedInvoiceAutomationSourceEvent(t, sourceStore.DB, 100+index, test.corruptSource, now.Add(time.Duration(index)*time.Second))
			action := automationInvoiceOverdueActionSnapshot(invoice, "P1")
			validActionJSON, err := json.Marshal(action)
			if err != nil {
				t.Fatalf("encode valid Invoice action snapshot: %v", err)
			}
			validCurrentJSON, err := json.Marshal(invoiceEventState(invoice))
			if err != nil {
				t.Fatalf("encode valid Invoice source snapshot: %v", err)
			}
			if test.corruptAction {
				action["title"] = "tampered safety-failure title"
			}
			if test.mismatchBinding {
				action["project_id"] = uuid.NewString()
				parsedAction, err := automationInvoiceOverdueActionFromSnapshot(action)
				if err != nil || parsedAction.Priority != "P1" {
					t.Fatalf("binding-mismatch action is not structurally/config valid: action=%#v err=%v", action, err)
				}
			}
			if test.mismatchContract {
				action["priority"] = "P2"
				if _, err := automationInvoiceOverdueActionFromSnapshot(action); err != nil {
					t.Fatalf("attempt-contract action is not structurally valid: action=%#v err=%v", action, err)
				}
			}
			sourceEventID := eventID
			input := automationAttemptInput{
				Rule: rule, TriggerType: "event", SourceEventID: &sourceEventID,
				LogicalKey: "event:" + rule.ID + ":" + eventID, Attempt: 1,
				Config: automationConfig{Priority: "P1"}, ActionSnapshot: action,
				Now: now.Add(time.Duration(index) * time.Second),
			}
			var run models.AutomationRun
			if err := sourceStore.DB.Transaction(func(tx *gorm.DB) error {
				var err error
				run, err = executeAutomationAttempt(tx, input)
				return err
			}); err != nil {
				t.Fatalf("produce %s Automation Run: %v", test.name, err)
			}
			if run.Status != "failed" || run.ErrorCode == nil || *run.ErrorCode != test.wantErrorCode ||
				run.Retryable || run.RetryAt != nil || run.ResultType != nil || run.ResultID != nil {
				t.Fatalf("produced safety-failure Automation Run = %#v", run)
			}
			wantRuns[run.ID] = test.wantErrorCode
			switch test.wantErrorCode {
			case "ACTION_SNAPSHOT_INVALID":
				if test.corruptAction {
					actionInvalidRunID = run.ID
					actionInvalidSourceEventID = eventID
					actionInvalidValidSnapshot = string(validActionJSON)
				}
			case "SOURCE_EVENT_INVALID":
				sourceInvalidSourceEventID = eventID
				sourceInvalidValidCurrent = string(validCurrentJSON)
			}
		})
	}

	project := createProjectForTest(t, sourceRouter, `{"name":"Portable source-conflict Project"}`, nil)
	project = transitionProjectForTest(t, sourceRouter, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, sourceRouter, project.ID, project.Version, `{"action":"complete"}`)
	var projectSource models.WorkflowEvent
	if err := sourceStore.DB.Where(
		"aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_completed'", project.ID,
	).Take(&projectSource).Error; err != nil {
		t.Fatalf("load source-conflict Project event: %v", err)
	}
	projectRuleOutput := automationRuleByPreset(t, sourceRouter, automationPresetProjectCompleted)
	enabledProjectRule := performRequest(
		sourceRouter, http.MethodPost, "/api/v1/automations/rules/"+projectRuleOutput.ID+"/enable", nil,
		map[string]string{"If-Match": `"1"`},
	)
	if enabledProjectRule.Code != http.StatusOK {
		t.Fatalf("enable source-conflict Project Automation = %d: %s", enabledProjectRule.Code, enabledProjectRule.Body.String())
	}
	var projectRule models.AutomationRule
	if err := sourceStore.DB.First(&projectRule, "id = ?", projectRuleOutput.ID).Error; err != nil {
		t.Fatalf("load source-conflict Project Automation Rule: %v", err)
	}
	conflictingRunID := uuid.NewString()
	projectConflictItemID = uuid.NewString()
	projectSourceKey := "automation:event:" + projectRule.ID + ":" + projectSource.ID
	conflictingPayload, err := json.Marshal(map[string]any{
		"automation_rule_id": projectRule.ID,
		"automation_run_id":  conflictingRunID,
		"preset_key":         automationPresetProjectCompleted,
		"project_id":         project.ID,
		"project_name":       "tampered source owner",
	})
	if err != nil {
		t.Fatalf("encode source-conflict Inbox payload: %v", err)
	}
	conflictAt := formatInboxTimestamp(now.Add(10 * time.Second))
	if err := sourceStore.DB.Create(&models.InboxItem{
		ID: projectConflictItemID, Kind: "event", Title: automationProjectCompletionTitle(project.Name),
		Summary: automationProjectCompletionItemSummary, SourceEntityType: automationInboxSourceType,
		SourceEntityID: &conflictingRunID, SourceEventKey: &projectSourceKey, Priority: "P1", Status: "open",
		ResolutionPolicy: "manual", PayloadJSON: string(conflictingPayload), Version: 1,
		CreatedAt: conflictAt, UpdatedAt: conflictAt,
	}).Error; err != nil {
		t.Fatalf("seed source-conflict Inbox item: %v", err)
	}
	projectSourceEventID := projectSource.ID
	var projectConflictRun models.AutomationRun
	if err := sourceStore.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		projectConflictRun, err = executeAutomationAttempt(tx, automationAttemptInput{
			Rule: projectRule, TriggerType: "event", SourceEventID: &projectSourceEventID,
			LogicalKey: "event:" + projectRule.ID + ":" + projectSource.ID, Attempt: 1,
			Config: automationConfig{Priority: "P1"},
			ActionSnapshot: map[string]any{
				"action_type": "inbox_item", "project_id": project.ID, "project_name": project.Name,
				"title": automationProjectCompletionTitle(project.Name), "priority": "P1",
			},
			Now: now.Add(10 * time.Second),
		})
		return err
	}); err != nil {
		t.Fatalf("produce Project SOURCE_EVENT_CONFLICT Run: %v", err)
	}
	if projectConflictRun.Status != "failed" || projectConflictRun.ErrorCode == nil ||
		*projectConflictRun.ErrorCode != "SOURCE_EVENT_CONFLICT" || projectConflictRun.Retryable ||
		projectConflictRun.RetryAt != nil || projectConflictRun.ResultType != nil || projectConflictRun.ResultID != nil {
		t.Fatalf("produced Project SOURCE_EVENT_CONFLICT Run = %#v", projectConflictRun)
	}
	wantRuns[projectConflictRun.ID] = "SOURCE_EVENT_CONFLICT"

	jsonExport := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("export safety-failure JSON = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	zipExport := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("export safety-failure ZIP = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	portableJSONPackage := decodeAutomationImportPackage(t, jsonExport.Body.Bytes())
	removeAutomationImportRow(&portableJSONPackage, "workflow_events", "id", actionInvalidSourceEventID)
	portableJSONBody, err := json.Marshal(portableJSONPackage)
	if err != nil {
		t.Fatalf("encode source-retained safety-failure JSON: %v", err)
	}
	zipEntries := readBusinessPackageEntries(t, zipExport.Body.Bytes())
	portableZIPPackage := decodeAutomationImportPackage(t, zipEntries["business-data.json"])
	removeAutomationImportRow(&portableZIPPackage, "workflow_events", "id", actionInvalidSourceEventID)
	portableZIPBody := automationImportPackageZIP(t, zipExport.Body.Bytes(), portableZIPPackage)

	for _, test := range []struct {
		name         string
		previewPath  string
		applyPath    string
		confirmation string
		body         []byte
	}{
		{
			name: "JSON", previewPath: "/api/v1/imports/business-data/preview",
			applyPath: "/api/v1/imports/business-data", confirmation: importReplaceConfirmation,
			body: portableJSONBody,
		},
		{
			name: "ZIP", previewPath: "/api/v1/imports/business-package/preview",
			applyPath: "/api/v1/imports/business-package", confirmation: packageImportReplaceConfirmation,
			body: portableZIPBody,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetRouter, targetStore, _, _ := newBackupTestAPI(t)
			preview := performRequest(targetRouter, http.MethodPost, test.previewPath, test.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("safety-failure preview = %d: %s", preview.Code, preview.Body.String())
			}
			apply := performRequest(
				targetRouter, http.MethodPost, test.applyPath, test.body,
				map[string]string{"X-Import-Confirmation": test.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("safety-failure apply = %d: %s", apply.Code, apply.Body.String())
			}
			for runID, errorCode := range wantRuns {
				assertDatabaseCount(t, targetStore, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND status = 'failed' AND retryable = 0 AND retry_at IS NULL AND result_type IS NULL AND result_id IS NULL AND error_code = ?", 1, runID, errorCode)
				assertDatabaseCount(t, targetStore, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'automation_run' AND aggregate_id = ? AND action = 'automation_run_failed'", 1, runID)
			}
		})
	}

	safetyPackage := portableJSONPackage
	for _, test := range []struct {
		name   string
		mutate func(*businessExportPackage)
	}{
		{
			name: "valid action is falsely labelled ACTION_SNAPSHOT_INVALID",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", actionInvalidRunID, "action_snapshot_json", actionInvalidValidSnapshot)
			},
		},
		{
			name: "valid source is falsely labelled SOURCE_EVENT_INVALID",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "workflow_events", "id", sourceInvalidSourceEventID, "current_json", sourceInvalidValidCurrent)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			packageData := cloneAutomationImportPackage(t, safetyPackage)
			test.mutate(&packageData)
			body, err := json.Marshal(packageData)
			if err != nil {
				t.Fatalf("encode falsely labelled safety failure: %v", err)
			}
			assertAutomationImportRejectedWithoutSideEffects(
				t, body, "/api/v1/imports/business-data/preview", "/api/v1/imports/business-data", importReplaceConfirmation,
			)
		})
	}
}

func TestAutomationImportGraphUsesImmutableProjectCompletionEventAfterProjectChangedOrDeleted(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable Project Automation graph fixture = %d: %s", enabled.Code, enabled.Body.String())
	}
	project := createProjectForTest(t, router, `{"name":"Immutable event Project"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND status = 'succeeded'", 1, rule.ID)
	exported := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export Project Automation graph fixture = %d: %s", exported.Code, exported.Body.String())
	}
	baseline := decodeAutomationImportPackage(t, exported.Body.Bytes())
	if !validAutomationImportGraph(baseline) {
		t.Fatal("baseline Project Automation graph is invalid")
	}
	for _, test := range []struct {
		name   string
		mutate func(*businessExportPackage)
	}{
		{
			name: "Project facts changed after completion",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "projects", "id", project.ID, "name", "Renamed after completion")
				setAutomationImportValue(t, packageData, "projects", "id", project.ID, "status", "archived")
			},
		},
		{
			name: "Project was hard deleted after completion",
			mutate: func(packageData *businessExportPackage) {
				removeAutomationImportRow(packageData, "projects", "id", project.ID)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			packageData := decodeAutomationImportPackage(t, exported.Body.Bytes())
			test.mutate(&packageData)
			if !validAutomationImportGraph(packageData) {
				t.Fatal("Automation graph rejected immutable Project completion source history")
			}
		})
	}
}

type historicalProjectAutomationImportFixture struct {
	jsonBody                []byte
	zipBody                 []byte
	projectID               string
	runID                   string
	resultInboxID           string
	completionSourceInboxID string
	sourceDeletedEventID    string
	projectDeletedEventID   string
}

func TestAutomationBusinessJSONImportAcceptsDeletedProjectCompletionSourceTombstone(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	assertHistoricalProjectAutomationImport(
		t, fixture, fixture.jsonBody,
		"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
		importAppendConfirmation, "IMPORT_APPLY_FAILED",
	)
}

func TestAutomationBusinessPackageImportAcceptsDeletedProjectCompletionSourceTombstone(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	assertHistoricalProjectAutomationImport(
		t, fixture, fixture.zipBody,
		"/api/v1/imports/business-package/preview", "/api/v1/imports/business-package",
		packageImportAppendConfirmation, "IMPORT_PACKAGE_APPLY_FAILED",
	)
}

func TestAutomationBusinessJSONImportRejectsDeletedSourceCollidingWithTargetProject(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	if err := store.DB.Exec(`
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES (?, 'Retained conflicting Project', 'planning', 1, '2026-09-03T08:00:00Z', '2026-09-03T08:00:00Z')
	`, fixture.projectID).Error; err != nil {
		t.Fatalf("seed target Project colliding with historical source: %v", err)
	}
	markerPath := filepath.Join(artifactDir, "objects", "f22-conflict-marker")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		t.Fatalf("create F22 conflict marker directory: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("retained F22 conflict file"), 0o600); err != nil {
		t.Fatalf("seed F22 conflict marker: %v", err)
	}

	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", fixture.jsonBody, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("historical source conflict preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
		previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != importBlockerTargetConflicts ||
		previewEnvelope.Data.ApplyMode != "" {
		t.Fatalf("historical source conflict preview = %#v err=%v", previewEnvelope.Data, err)
	}
	apply := performRequest(
		router, http.MethodPost, "/api/v1/imports/business-data", fixture.jsonBody,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation},
	)
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_CONFLICT" {
		t.Fatalf("historical source conflict apply = %d: %s", apply.Code, apply.Body.String())
	}

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id = ? AND name = 'Retained conflicting Project' AND status = 'planning' AND version = 1", 1, fixture.projectID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ?", 0, fixture.runID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id IN (?, ?)", 0, fixture.resultInboxID, fixture.completionSourceInboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM business_import_project_completion_authorizations", 0)
	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != "retained F22 conflict file" {
		t.Fatalf("historical source conflict changed target file body=%q err=%v", marker, err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("historical source conflict created rollback backups: %v", backups)
	}
}

func TestAutomationBusinessJSONImportRejectsUnknownSourceDeletedSnapshotField(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	packageData := decodeAutomationImportPackage(t, fixture.jsonBody)
	currentJSON := automationImportString(
		t, &packageData, "workflow_events", "id", fixture.sourceDeletedEventID, "current_json",
	)
	current, ok := automationImportJSONObject(currentJSON)
	if !ok || len(current) != 23 || current["dismiss_reason"] != nil {
		t.Fatalf("source_deleted fixture current snapshot = %#v", current)
	}
	delete(current, "dismiss_reason")
	current["unknown_null_replacement"] = nil
	mutatedCurrent, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("encode mutated source_deleted snapshot: %v", err)
	}
	setAutomationImportValue(
		t, &packageData, "workflow_events", "id", fixture.sourceDeletedEventID,
		"current_json", string(mutatedCurrent),
	)
	body, err := json.Marshal(packageData)
	if err != nil {
		t.Fatalf("encode mutated source_deleted import: %v", err)
	}
	targetRouter, _, _, _ := newBackupTestAPI(t)
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusUnprocessableEntity || responseErrorCode(t, preview.Body.Bytes()) != "IMPORT_ROW_INVALID" {
		t.Fatalf("unknown source_deleted snapshot field preview = %d: %s", preview.Code, preview.Body.String())
	}
}

func TestAutomationBusinessJSONImportRejectsUnknownProjectDeletedSnapshotFieldWithoutSideEffects(t *testing.T) {
	fixture := newHistoricalProjectAutomationImportFixture(t)
	packageData := decodeAutomationImportPackage(t, fixture.jsonBody)
	previousJSON := automationImportString(
		t, &packageData, "workflow_events", "id", fixture.projectDeletedEventID, "previous_json",
	)
	previous, ok := automationImportJSONObject(previousJSON)
	if !ok || len(previous) != 11 || previous["client_id"] != nil {
		t.Fatalf("project_deleted fixture previous snapshot = %#v", previous)
	}
	delete(previous, "client_id")
	previous["unknown_null_replacement"] = nil
	mutatedPrevious, err := json.Marshal(previous)
	if err != nil {
		t.Fatalf("encode mutated project_deleted snapshot: %v", err)
	}
	setAutomationImportValue(
		t, &packageData, "workflow_events", "id", fixture.projectDeletedEventID,
		"previous_json", string(mutatedPrevious),
	)
	body, err := json.Marshal(packageData)
	if err != nil {
		t.Fatalf("encode mutated project_deleted import: %v", err)
	}
	assertAutomationImportRejectedWithoutSideEffects(
		t, body, "/api/v1/imports/business-data/preview", "/api/v1/imports/business-data", importReplaceConfirmation,
	)
}

func TestSQLiteRejectsForgedOnlineProjectCompletionSourceTombstone(t *testing.T) {
	_, store, _, _ := newBackupTestAPI(t)
	projectID := uuid.NewString()
	completedAt := formatInboxTimestamp(time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC))
	deletedAt := formatInboxTimestamp(time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC))
	payload, err := json.Marshal(map[string]any{
		"project_id": projectID, "project_name": "Forged Historical Project",
		"completed_at": completedAt, "completion_version": 3, "incomplete_task_count": 0,
	})
	if err != nil {
		t.Fatalf("encode forged Project completion tombstone payload: %v", err)
	}
	sourceKey := projectCompletionEventKey(projectID, 3)
	sourceID := projectID
	resolvedActorID := models.BuiltinOwnerActorID
	resolutionReason := "Forged historical resolution"
	resolutionMode := "manual"
	err = store.DB.Create(&models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: projectCompletionTitle("Forged Historical Project"),
		Summary:          "项目已标记完成，请确认交付收尾、归档或其他后续工作。",
		SourceEntityType: projectCompletionInboxSourceType, SourceEntityID: &sourceID,
		SourceEventKey: &sourceKey, SourceDeletedAt: &deletedAt, Priority: "P1", Status: "resolved",
		ResolutionPolicy: "manual", TriagedAt: &completedAt, ResolvedByActorID: &resolvedActorID,
		ResolvedAt: &completedAt, ResolutionReason: &resolutionReason, ResolutionMode: &resolutionMode,
		PayloadJSON: string(payload), Version: 3, CreatedAt: completedAt, UpdatedAt: deletedAt,
	}).Error
	if err == nil {
		t.Fatal("SQLite accepted a forged online Project completion source tombstone without import authorization")
	}
	if !strings.Contains(err.Error(), "INVALID_PROJECT_COMPLETION_INBOX_SOURCE") {
		t.Fatalf("forged Project completion tombstone error = %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'project_completion'", 0)
}

func newHistoricalProjectAutomationImportFixture(t *testing.T) historicalProjectAutomationImportFixture {
	t.Helper()
	router, store, _, _ := newBackupTestAPI(t)
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil,
		map[string]string{"If-Match": `"1"`},
	)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable historical Project Automation = %d: %s", enabled.Code, enabled.Body.String())
	}

	project := createProjectForTest(t, router, `{"name":"Historical Automation Project"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var run models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'succeeded'", rule.ID).Take(&run).Error; err != nil ||
		run.ResultType == nil || *run.ResultType != "inbox_item" || run.ResultID == nil || run.SourceEventID == nil {
		t.Fatalf("load historical Project Automation Run: run=%#v err=%v", run, err)
	}
	var completionSource models.InboxItem
	if err := store.DB.Where(
		"source_entity_type = ? AND source_entity_id = ?", projectCompletionInboxSourceType, project.ID,
	).Take(&completionSource).Error; err != nil {
		t.Fatalf("load Project completion source projection: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_entity_type = 'automation'", 1, *run.ResultID)

	resolved := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+completionSource.ID+"/resolve",
		[]byte(`{"reason":"Project completion history retained before deletion"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, completionSource.Version)},
	)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve Project completion source before deletion = %d: %s", resolved.Code, resolved.Body.String())
	}
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"archive"}`)
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/projects/"+project.ID+"?confirm=true", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, project.Version)},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete historical Automation Project = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id = ?", 0, project.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL", 1, completionSource.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_deleted'", 1, project.ID)
	var projectDeletedEvent models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_deleted'", project.ID,
	).Take(&projectDeletedEvent).Error; err != nil {
		t.Fatalf("load retained project_deleted event: %v", err)
	}
	var sourceDeletedEvent models.WorkflowEvent
	if err := store.DB.Where(
		"aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_deleted'", completionSource.ID,
	).Take(&sourceDeletedEvent).Error; err != nil {
		t.Fatalf("load retained source_deleted event: %v", err)
	}

	jsonExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("export historical Project Automation JSON = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	zipExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("export historical Project Automation ZIP = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	return historicalProjectAutomationImportFixture{
		jsonBody: bytes.Clone(jsonExport.Body.Bytes()), zipBody: bytes.Clone(zipExport.Body.Bytes()), projectID: project.ID,
		runID: run.ID, resultInboxID: *run.ResultID, completionSourceInboxID: completionSource.ID,
		sourceDeletedEventID: sourceDeletedEvent.ID, projectDeletedEventID: projectDeletedEvent.ID,
	}
}

func assertHistoricalProjectAutomationImport(
	t *testing.T,
	fixture historicalProjectAutomationImportFixture,
	body []byte,
	previewPath, applyPath, confirmation, applyErrorCode string,
) {
	t.Helper()
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	const sentinelClientID = "018f0000-0000-7000-8000-000000009922"
	if err := store.DB.Exec("INSERT INTO clients(id, name) VALUES (?, 'Retained F22 target client')", sentinelClientID).Error; err != nil {
		t.Fatalf("seed F22 target sentinel: %v", err)
	}
	markerPath := filepath.Join(artifactDir, "f22-import-marker")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		t.Fatalf("create F22 marker directory: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("retained F22 target file"), 0o600); err != nil {
		t.Fatalf("seed F22 target marker: %v", err)
	}

	preview := performRequest(router, http.MethodPost, previewPath, body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("historical Project Automation preview = %d: %s", preview.Code, preview.Body.String())
	}
	apply := performRequest(
		router, http.MethodPost, applyPath, body,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusOK {
		if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != applyErrorCode {
			t.Fatalf("historical Project Automation apply = %d: %s", apply.Code, apply.Body.String())
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = ? AND name = 'Retained F22 target client'", 1, sentinelClientID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ?", 0, fixture.runID)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id IN (?, ?)", 0, fixture.resultInboxID, fixture.completionSourceInboxID)
		marker, err := os.ReadFile(markerPath)
		if err != nil || string(marker) != "retained F22 target file" {
			t.Fatalf("failed historical import changed target file body=%q err=%v", marker, err)
		}
		if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
			t.Fatalf("failed historical import rollback backups = %v, want exactly one", backups)
		}
		t.Fatalf("historical Project Automation apply remained non-portable: %d %s", apply.Code, apply.Body.String())
	}

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = ? AND name = 'Retained F22 target client'", 1, sentinelClientID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs WHERE id = ? AND status = 'succeeded'", 1, fixture.runID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_entity_type = 'automation'", 1, fixture.resultInboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects WHERE id = ?", 0, fixture.projectID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND source_deleted_at IS NOT NULL", 1, fixture.completionSourceInboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'project' AND aggregate_id = ? AND action = 'project_deleted'", 1, fixture.projectID)
	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != "retained F22 target file" {
		t.Fatalf("historical import changed unrelated target file body=%q err=%v", marker, err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("successful historical import rollback backups = %v, want exactly one", backups)
	}
}

func TestAutomationBusinessImportPreflightRejectsInvalidRunGraphsWithoutSideEffects(t *testing.T) {
	fixture := newAutomationImportFixture(t)
	if !validAutomationImportGraph(decodeAutomationImportPackage(t, fixture.jsonBody)) {
		t.Fatal("invalid-run cases require a valid portable Automation baseline")
	}

	tests := []struct {
		name   string
		mutate func(*businessExportPackage)
	}{
		{
			name: "missing source event",
			mutate: func(packageData *businessExportPackage) {
				removeAutomationImportRow(packageData, "workflow_events", "id", fixture.projectSourceEventID)
			},
		},
		{
			name: "wrong source event action",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "workflow_events", "id", fixture.projectSourceEventID, "action", "project_started")
			},
		},
		{
			name: "noncanonical logical key",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "logical_key", "event:tampered")
			},
		},
		{
			name: "noncanonical dedupe key",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "dedupe_key", "event:tampered:attempt:1")
			},
		},
		{
			name: "equivalent noncanonical run timestamps",
			mutate: func(packageData *businessExportPackage) {
				startedAt := automationImportString(t, packageData, "automation_runs", "id", fixture.projectRunID, "started_at")
				noncanonical := strings.TrimSuffix(startedAt, "Z") + "+00:00"
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "started_at", noncanonical)
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "ended_at", noncanonical)
			},
		},
		{
			name: "run audit event timestamp drifts from run",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "workflow_events", "aggregate_id", fixture.projectRunID, "created_at", "2099-01-01T00:00:00.000000000Z")
			},
		},
		{
			name: "Inbox result creation event is missing",
			mutate: func(packageData *businessExportPackage) {
				removeAutomationImportRowsWhere(packageData, "workflow_events", map[string]any{
					"aggregate_type": "inbox_item", "aggregate_id": fixture.projectResultID, "action": "source_projected",
				})
			},
		},
		{
			name: "Project completion previous snapshot is missing",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "workflow_events", "id", fixture.projectSourceEventID, "previous_json", nil)
			},
		},
		{
			name: "captured Automation Rule version event is missing",
			mutate: func(packageData *businessExportPackage) {
				removeAutomationImportRowsWhere(packageData, "workflow_events", map[string]any{
					"aggregate_type": "automation_rule", "aggregate_id": fixture.projectRuleID, "action": "automation_rule_enabled",
				})
			},
		},
		{
			name: "captured Automation Rule previous snapshot is malformed",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValuesWhere(t, packageData, "workflow_events", map[string]any{
					"aggregate_type": "automation_rule", "aggregate_id": fixture.projectRuleID, "action": "automation_rule_enabled",
				}, "previous_json", `{}`)
			},
		},
		{
			name: "captured Automation Rule proof occurs after its run",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValuesWhere(t, packageData, "workflow_events", map[string]any{
					"aggregate_type": "automation_rule", "aggregate_id": fixture.projectRuleID, "action": "automation_rule_enabled",
				}, "created_at", "2099-01-01T00:00:00.000000000Z")
			},
		},
		{
			name: "succeeded result summary drifts from action",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "result_summary", "tampered result summary")
			},
		},
		{
			name: "schedule run fakes an event-source failure",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "status", "failed")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "error_code", "SOURCE_EVENT_INVALID")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "result_type", nil)
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "result_id", nil)
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "result_summary", "本地动作未提交；业务来源事实保持不变。")
			},
		},
		{
			name: "retry crosses rules",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.retryRunID, "rule_id", fixture.invoiceRuleID)
			},
		},
		{
			name: "retry skips attempt",
			mutate: func(packageData *businessExportPackage) {
				logicalKey := automationImportString(t, packageData, "automation_runs", "id", fixture.retryRunID, "logical_key")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.retryRunID, "attempt", float64(3))
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.retryRunID, "dedupe_key", logicalKey+":attempt:3")
			},
		},
		{
			name: "retry snapshot drift",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.retryRunID, "config_snapshot_json", `{"priority":"P2"}`)
			},
		},
		{
			name: "succeeded result type does not match preset",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "result_type", "task")
			},
		},
		{
			name: "succeeded result id is absent",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "result_id", uuid.NewString())
			},
		},
		{
			name: "succeeded result is linked to another run",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "result_id", fixture.retryResultID)
			},
		},
		{
			name: "unavailable Agent preset has a pseudo run",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "rule_id", fixture.agentRuleID)
			},
		},
		{
			name: "run lifecycle Workflow Event is tampered",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "workflow_events", "aggregate_id", fixture.projectRunID, "action", "automation_run_failed")
			},
		},
		{
			name: "cancelled run status is not portable",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "status", "cancelled")
			},
		},
		{
			name: "run rule version is ahead of imported rule",
			mutate: func(packageData *businessExportPackage) {
				current := automationImportNumber(t, packageData, "automation_rules", "id", fixture.projectRuleID, "version")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "rule_version", current+1)
			},
		},
		{
			name: "retry parent is missing",
			mutate: func(packageData *businessExportPackage) {
				removeAutomationImportRow(packageData, "automation_runs", "id", fixture.failedRunID)
			},
		},
		{
			name: "retry parent is not retryable",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.failedRunID, "retryable", float64(0))
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.failedRunID, "retry_at", nil)
			},
		},
		{
			name: "event run is marked skipped",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "status", "skipped")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.projectRunID, "error_code", "SCHEDULE_WINDOW_EXPIRED")
			},
		},
		{
			name: "skipped schedule uses the wrong error code",
			mutate: func(packageData *businessExportPackage) {
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "status", "skipped")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "error_code", "ACTION_WRITE_FAILED")
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "result_type", nil)
				setAutomationImportValue(t, packageData, "automation_runs", "id", fixture.scheduleRunID, "result_id", nil)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packageData := cloneAutomationImportPackage(t, fixture.business)
			test.mutate(&packageData)
			body, err := json.Marshal(packageData)
			if err != nil {
				t.Fatalf("encode invalid Automation import: %v", err)
			}
			assertAutomationImportRejectedWithoutSideEffects(
				t, body, "/api/v1/imports/business-data/preview", "/api/v1/imports/business-data", importReplaceConfirmation,
			)
		})
	}
}

func TestAutomationBusinessPackagePreflightRejectsInvalidRunGraphWithoutSideEffects(t *testing.T) {
	fixture := newAutomationImportFixture(t)
	entries := readBusinessPackageEntries(t, fixture.zipBody)
	var packageData businessExportPackage
	decoder := json.NewDecoder(bytes.NewReader(entries["business-data.json"]))
	decoder.UseNumber()
	if err := decoder.Decode(&packageData); err != nil {
		t.Fatalf("decode ZIP Automation business data: %v", err)
	}
	if !validAutomationImportGraph(packageData) {
		t.Fatal("invalid ZIP case requires a valid portable Automation baseline")
	}
	setAutomationImportValue(t, &packageData, "automation_runs", "id", fixture.projectRunID, "dedupe_key", "event:tampered:attempt:1")
	body := automationImportPackageZIP(t, fixture.zipBody, packageData)

	assertAutomationImportRejectedWithoutSideEffects(
		t, body, "/api/v1/imports/business-package/preview", "/api/v1/imports/business-package", packageImportReplaceConfirmation,
	)
}

func TestBusinessExportOmitsPendingAutomationDeliveryRows(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+rule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable pending-delivery Automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_pending_export_automation_run
		BEFORE INSERT ON automation_runs
		BEGIN SELECT RAISE(ABORT, 'TEST_PENDING_EXPORT_AUTOMATION_RUN'); END
	`).Error; err != nil {
		t.Fatalf("install pending export Automation failure: %v", err)
	}
	project := createProjectForTest(t, router, `{"name":"Pending export Project"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var deliveryID string
	if err := store.DB.Table("automation_event_deliveries").Where("rule_id = ?", rule.ID).Pluck("id", &deliveryID).Error; err != nil || deliveryID == "" {
		t.Fatalf("load pending Automation delivery id=%q err=%v", deliveryID, err)
	}

	jsonExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("JSON export = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	if bytes.Contains(jsonExport.Body.Bytes(), []byte(deliveryID)) {
		t.Fatal("pending Automation delivery leaked into JSON business export")
	}

	zipExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("ZIP export = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	entries := readBusinessPackageEntries(t, zipExport.Body.Bytes())
	if bytes.Contains(entries["business-data.json"], []byte(deliveryID)) {
		t.Fatal("pending Automation delivery leaked into ZIP business export")
	}
}

func newAutomationImportFixture(t *testing.T) automationImportFixture {
	t.Helper()
	router, store, _, _ := newBackupTestAPI(t)

	projectRule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabledProject := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+projectRule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabledProject.Code != http.StatusOK {
		t.Fatalf("enable Project Automation = %d: %s", enabledProject.Code, enabledProject.Body.String())
	}

	project := createProjectForTest(t, router, `{"name":"Portable completed Project"}`, nil)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"start"}`)
	project = transitionProjectForTest(t, router, project.ID, project.Version, `{"action":"complete"}`)
	var projectRun models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'succeeded'", projectRule.ID).Take(&projectRun).Error; err != nil {
		t.Fatalf("load portable Project Automation Run: %v", err)
	}
	if projectRun.SourceEventID == nil || projectRun.ResultID == nil {
		t.Fatalf("portable Project Automation Run lacks linkage: %#v", projectRun)
	}
	if err := store.DB.Exec(`
		CREATE TRIGGER reject_portable_automation_inbox
		BEFORE INSERT ON inbox_items
		WHEN NEW.source_entity_type = 'automation'
		BEGIN SELECT RAISE(ABORT, 'TEST_PORTABLE_AUTOMATION_ACTION_FAILURE'); END
	`).Error; err != nil {
		t.Fatalf("install portable Automation failure: %v", err)
	}
	failedProject := createProjectForTest(t, router, `{"name":"Portable retry Project"}`, nil)
	failedProject = transitionProjectForTest(t, router, failedProject.ID, failedProject.Version, `{"action":"start"}`)
	failedProject = transitionProjectForTest(t, router, failedProject.ID, failedProject.Version, `{"action":"complete"}`)
	var failedRun models.AutomationRun
	if err := store.DB.Where("rule_id = ? AND status = 'failed'", projectRule.ID).Take(&failedRun).Error; err != nil {
		t.Fatalf("load portable failed Automation Run: %v", err)
	}
	if err := store.DB.Exec("DROP TRIGGER reject_portable_automation_inbox").Error; err != nil {
		t.Fatalf("remove portable Automation failure: %v", err)
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/automations/runs/"+failedRun.ID+"/retry", nil, nil)
	if retried.Code != http.StatusCreated {
		t.Fatalf("retry portable Automation Run = %d: %s", retried.Code, retried.Body.String())
	}
	var retryRun models.AutomationRun
	if err := store.DB.Where("retry_of_run_id = ?", failedRun.ID).Take(&retryRun).Error; err != nil {
		t.Fatalf("load portable retried Automation Run: %v", err)
	}
	if retryRun.ResultID == nil {
		t.Fatalf("portable retried Automation Run lacks result: %#v", retryRun)
	}

	client := createClientForTest(t, router, `{"name":"Portable Automation Client"}`, nil)
	invoiceProject := createProjectForTest(t, router, fmt.Sprintf(`{"name":"Portable Invoice Project","client_id":%q}`, client.ID), nil)
	invoiceRule := configureAndEnableInvoiceAutomation(t, router, "P1")
	var deletedTaskRun models.AutomationRun
	for index := 0; index < 2; index++ {
		invoice := createInvoiceForTest(t, router, fmt.Sprintf(
			`{"client_id":%q,"project_id":%q,"amount_minor":%d,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-08-20"}`,
			client.ID, invoiceProject.ID, 12000+index,
		), nil)
		invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_sent"}`, "")
		invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_overdue"}`, fmt.Sprintf("portable-overdue-%d", index))
		if invoice.Status != "overdue" {
			t.Fatalf("portable overdue Invoice = %#v", invoice)
		}
		var invoiceRun models.AutomationRun
		if err := store.DB.Where("rule_id = ?", invoiceRule.ID).Order("started_at DESC").Take(&invoiceRun).Error; err != nil {
			t.Fatalf("load portable Invoice Automation Run: %v", err)
		}
		if invoiceRun.ResultID == nil {
			t.Fatalf("portable Invoice Automation Run lacks Task: %#v", invoiceRun)
		}
		if index == 0 {
			invoice = transitionInvoiceForTest(t, router, invoice, `{"action":"mark_paid","paid_date":"2026-08-25"}`, "portable-invoice-paid")
			if invoice.Status != "paid" {
				t.Fatalf("portable paid Invoice = %#v", invoice)
			}
			updatedTask := performRequest(
				router, http.MethodPatch, "/api/v1/tasks/"+*invoiceRun.ResultID,
				[]byte(`{"title":"Edited portable automated followup"}`), map[string]string{"If-Match": `"1"`},
			)
			if updatedTask.Code != http.StatusOK {
				t.Fatalf("edit automated followup Task = %d: %s", updatedTask.Code, updatedTask.Body.String())
			}
			completedTask := performRequest(
				router, http.MethodPost, "/api/v1/tasks/"+*invoiceRun.ResultID+"/complete",
				[]byte(`{}`), map[string]string{"If-Match": `"2"`},
			)
			if completedTask.Code != http.StatusOK {
				t.Fatalf("complete automated followup Task = %d: %s", completedTask.Code, completedTask.Body.String())
			}
		} else {
			deletedTaskRun = invoiceRun
		}
	}
	if deletedTaskRun.ResultID == nil {
		t.Fatalf("hard-deleted Task Automation Run lacks result: %#v", deletedTaskRun)
	}
	deletedTaskID := *deletedTaskRun.ResultID
	deletedTask := performRequest(
		router, http.MethodDelete, "/api/v1/tasks/"+deletedTaskID, nil,
		map[string]string{"If-Match": `"1"`},
	)
	if deletedTask.Code != http.StatusNoContent {
		t.Fatalf("hard-delete otherwise valid automated Task = %d: %s", deletedTask.Code, deletedTask.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'task_created_from_automation'", 1, deletedTaskID)

	dailyRule := automationRuleByPreset(t, router, automationPresetDailyToday)
	enabledDaily := performRequest(router, http.MethodPost, "/api/v1/automations/rules/"+dailyRule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabledDaily.Code != http.StatusOK {
		t.Fatalf("enable schedule Automation = %d: %s", enabledDaily.Code, enabledDaily.Body.String())
	}
	scheduledAt := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	var scheduleRuns []models.AutomationRun
	for index := 0; index < 2; index++ {
		window := scheduledAt.AddDate(0, 0, index)
		if err := store.DB.Model(&models.AutomationRule{}).Where("id = ?", dailyRule.ID).
			Update("next_run_at", formatInboxTimestamp(window)).Error; err != nil {
			t.Fatalf("set portable schedule window: %v", err)
		}
		service := &API{db: store.DB, options: Options{Now: func() time.Time { return window }}}
		if err := service.projectDueAutomations(window); err != nil {
			t.Fatalf("run portable schedule Automation: %v", err)
		}
	}
	if err := store.DB.Where("rule_id = ? AND trigger_type = 'schedule'", dailyRule.ID).Order("scheduled_for ASC").Find(&scheduleRuns).Error; err != nil || len(scheduleRuns) != 2 {
		t.Fatalf("load portable schedule Automation Runs count=%d err=%v", len(scheduleRuns), err)
	}
	for _, run := range scheduleRuns {
		if run.ResultID == nil {
			t.Fatalf("portable schedule Automation Run lacks Reminder: %#v", run)
		}
	}
	editedReminder := performRequest(
		router, http.MethodPatch, "/api/v1/reminders/"+*scheduleRuns[0].ResultID,
		[]byte(`{"title":"Edited portable scheduled Reminder","recurrence_type":"daily","recurrence_interval":1,"recurrence_timezone":"UTC"}`), map[string]string{"If-Match": `"1"`},
	)
	if editedReminder.Code != http.StatusOK {
		t.Fatalf("edit scheduled Automation Reminder = %d: %s", editedReminder.Code, editedReminder.Body.String())
	}
	cancelledReminder := performRequest(
		router, http.MethodDelete, "/api/v1/reminders/"+*scheduleRuns[1].ResultID,
		[]byte(`{"reason":"portable schedule no longer needed"}`), map[string]string{"If-Match": `"1"`},
	)
	if cancelledReminder.Code != http.StatusOK {
		t.Fatalf("cancel Automation Reminder = %d: %s", cancelledReminder.Code, cancelledReminder.Body.String())
	}
	offlineReturn := scheduledAt.AddDate(0, 0, 3).Add(time.Minute)
	reminderService := &API{db: store.DB, options: Options{Now: func() time.Time { return offlineReturn }}}
	if err := reminderService.projectDueReminders(context.Background()); err != nil {
		t.Fatalf("fire edited Automation Reminder after offline window: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE series_id = ? AND occurrence_number > 2 AND status = 'scheduled' AND source_entity_type = 'automation'", 1, *scheduleRuns[0].ResultID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND status = 'fired'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM reminders WHERE source_entity_type = 'automation' AND status = 'cancelled'", 1)

	currentProjectRule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	editedRule := performRequest(
		router, http.MethodPatch, "/api/v1/automations/rules/"+currentProjectRule.ID,
		[]byte(`{"config":{"priority":"P2"}}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentProjectRule.Version)},
	)
	if editedRule.Code != http.StatusOK {
		t.Fatalf("edit Project Automation after run history = %d: %s", editedRule.Code, editedRule.Body.String())
	}
	var editedRuleEnvelope automationRuleEnvelope
	if err := json.Unmarshal(editedRule.Body.Bytes(), &editedRuleEnvelope); err != nil {
		t.Fatalf("decode edited Project Automation: %v", err)
	}
	disabledRule := performRequest(
		router, http.MethodPost, "/api/v1/automations/rules/"+currentProjectRule.ID+"/disable", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, editedRuleEnvelope.Data.Version)},
	)
	if disabledRule.Code != http.StatusOK {
		t.Fatalf("disable Project Automation after run history = %d: %s", disabledRule.Code, disabledRule.Body.String())
	}

	agentRule := automationRuleByPreset(t, router, automationPresetAgentRunFailed)
	jsonExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("export portable Automation JSON = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	var packageData businessExportPackage
	if err := json.Unmarshal(jsonExport.Body.Bytes(), &packageData); err != nil {
		t.Fatalf("decode portable Automation JSON: %v", err)
	}
	zipExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("export portable Automation ZIP = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	return automationImportFixture{
		business: packageData, jsonBody: bytes.Clone(jsonExport.Body.Bytes()), zipBody: bytes.Clone(zipExport.Body.Bytes()),
		projectRuleID: projectRule.ID, invoiceRuleID: invoiceRule.ID, agentRuleID: agentRule.ID,
		projectRunID: projectRun.ID, projectSourceEventID: *projectRun.SourceEventID, projectResultID: *projectRun.ResultID,
		failedRunID: failedRun.ID, retryRunID: retryRun.ID, retryResultID: *retryRun.ResultID,
		deletedTaskID: deletedTaskID, scheduleRunID: scheduleRuns[0].ID,
	}
}

func assertAutomationImportRejectedWithoutSideEffects(
	t *testing.T,
	body []byte,
	previewPath string,
	applyPath string,
	confirmation string,
) {
	t.Helper()
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	const targetClientID = "018f0000-0000-7000-8000-000000009901"
	if err := store.DB.Exec("INSERT INTO clients(id, name) VALUES (?, 'Retained target client')", targetClientID).Error; err != nil {
		t.Fatalf("seed target sentinel: %v", err)
	}
	markerPath := filepath.Join(artifactDir, "objects", "automation-import-marker")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		t.Fatalf("create target marker directory: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("retained target file"), 0o600); err != nil {
		t.Fatalf("seed target marker: %v", err)
	}

	preview := performRequest(router, http.MethodPost, previewPath, body, nil)
	if preview.Code != http.StatusUnprocessableEntity || responseErrorCode(t, preview.Body.Bytes()) != "IMPORT_ROW_INVALID" {
		t.Fatalf("invalid Automation preview = %d: %s", preview.Code, preview.Body.String())
	}
	apply := performRequest(
		router, http.MethodPost, applyPath, body,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_ROW_INVALID" {
		t.Fatalf("invalid Automation apply = %d: %s", apply.Code, apply.Body.String())
	}

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = ? AND name = 'Retained target client'", 1, targetClientID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM automation_runs", 0)
	marker, err := os.ReadFile(markerPath)
	if err != nil || string(marker) != "retained target file" {
		t.Fatalf("invalid Automation import changed target file body=%q err=%v", marker, err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("invalid Automation import created backups: %v", backups)
	}
}

func cloneAutomationImportPackage(t *testing.T, source businessExportPackage) businessExportPackage {
	t.Helper()
	body, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("encode Automation import clone: %v", err)
	}
	var result businessExportPackage
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode Automation import clone: %v", err)
	}
	return result
}

func decodeAutomationImportPackage(t *testing.T, body []byte) businessExportPackage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result businessExportPackage
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode Automation import package: %v", err)
	}
	return result
}

func setAutomationImportValue(
	t *testing.T,
	packageData *businessExportPackage,
	tableName string,
	matchColumn string,
	matchValue any,
	targetColumn string,
	value any,
) {
	t.Helper()
	table := automationImportTable(t, packageData, tableName)
	matchIndex := columnIndex(table.Columns, matchColumn)
	targetIndex := columnIndex(table.Columns, targetColumn)
	if matchIndex < 0 || targetIndex < 0 {
		t.Fatalf("Automation import table %s lacks %s or %s", tableName, matchColumn, targetColumn)
	}
	for index := range table.Rows {
		if fmt.Sprint(table.Rows[index][matchIndex]) == fmt.Sprint(matchValue) {
			table.Rows[index][targetIndex] = value
			return
		}
	}
	t.Fatalf("Automation import row %s.%s=%v not found", tableName, matchColumn, matchValue)
}

func automationImportString(
	t *testing.T,
	packageData *businessExportPackage,
	tableName string,
	matchColumn string,
	matchValue any,
	targetColumn string,
) string {
	t.Helper()
	table := automationImportTable(t, packageData, tableName)
	matchIndex := columnIndex(table.Columns, matchColumn)
	targetIndex := columnIndex(table.Columns, targetColumn)
	for _, row := range table.Rows {
		if fmt.Sprint(row[matchIndex]) == fmt.Sprint(matchValue) {
			value, ok := row[targetIndex].(string)
			if !ok {
				t.Fatalf("Automation import value %s.%s is %T", tableName, targetColumn, row[targetIndex])
			}
			return value
		}
	}
	t.Fatalf("Automation import row %s.%s=%v not found", tableName, matchColumn, matchValue)
	return ""
}

func automationImportNumber(
	t *testing.T,
	packageData *businessExportPackage,
	tableName string,
	matchColumn string,
	matchValue any,
	targetColumn string,
) float64 {
	t.Helper()
	table := automationImportTable(t, packageData, tableName)
	matchIndex := columnIndex(table.Columns, matchColumn)
	targetIndex := columnIndex(table.Columns, targetColumn)
	for _, row := range table.Rows {
		if fmt.Sprint(row[matchIndex]) == fmt.Sprint(matchValue) {
			value, ok := row[targetIndex].(float64)
			if !ok {
				t.Fatalf("Automation import value %s.%s is %T", tableName, targetColumn, row[targetIndex])
			}
			return value
		}
	}
	t.Fatalf("Automation import row %s.%s=%v not found", tableName, matchColumn, matchValue)
	return 0
}

func removeAutomationImportRow(packageData *businessExportPackage, tableName string, matchColumn string, matchValue any) {
	for tableIndex := range packageData.Tables {
		table := &packageData.Tables[tableIndex]
		if table.Name != tableName {
			continue
		}
		matchIndex := columnIndex(table.Columns, matchColumn)
		retained := table.Rows[:0]
		for _, row := range table.Rows {
			if matchIndex < 0 || fmt.Sprint(row[matchIndex]) != fmt.Sprint(matchValue) {
				retained = append(retained, row)
			}
		}
		table.Rows = retained
		return
	}
}

func removeAutomationImportRowsWhere(packageData *businessExportPackage, tableName string, matches map[string]any) {
	for tableIndex := range packageData.Tables {
		table := &packageData.Tables[tableIndex]
		if table.Name != tableName {
			continue
		}
		indexes := make(map[string]int, len(matches))
		for column := range matches {
			indexes[column] = columnIndex(table.Columns, column)
		}
		retained := table.Rows[:0]
		for _, row := range table.Rows {
			matched := true
			for column, value := range matches {
				index := indexes[column]
				if index < 0 || fmt.Sprint(row[index]) != fmt.Sprint(value) {
					matched = false
					break
				}
			}
			if !matched {
				retained = append(retained, row)
			}
		}
		table.Rows = retained
		return
	}
}

func setAutomationImportValuesWhere(
	t *testing.T,
	packageData *businessExportPackage,
	tableName string,
	matches map[string]any,
	targetColumn string,
	value any,
) {
	t.Helper()
	table := automationImportTable(t, packageData, tableName)
	targetIndex := columnIndex(table.Columns, targetColumn)
	indexes := make(map[string]int, len(matches))
	for column := range matches {
		indexes[column] = columnIndex(table.Columns, column)
	}
	for rowIndex := range table.Rows {
		matched := true
		for column, expected := range matches {
			index := indexes[column]
			if index < 0 || fmt.Sprint(table.Rows[rowIndex][index]) != fmt.Sprint(expected) {
				matched = false
				break
			}
		}
		if matched {
			if targetIndex < 0 {
				t.Fatalf("Automation import table %s lacks %s", tableName, targetColumn)
			}
			table.Rows[rowIndex][targetIndex] = value
			return
		}
	}
	t.Fatalf("Automation import row in %s matching %#v not found", tableName, matches)
}

func automationImportTable(t *testing.T, packageData *businessExportPackage, name string) *businessExportTable {
	t.Helper()
	for index := range packageData.Tables {
		if packageData.Tables[index].Name == name {
			return &packageData.Tables[index]
		}
	}
	t.Fatalf("Automation import table %q not found", name)
	return nil
}

func automationImportPackageZIP(t *testing.T, sourceZIP []byte, business businessExportPackage) []byte {
	t.Helper()
	entries := readBusinessPackageEntries(t, sourceZIP)
	businessRaw, err := json.MarshalIndent(business, "", "  ")
	if err != nil {
		t.Fatalf("encode invalid Automation package business data: %v", err)
	}
	businessRaw = append(businessRaw, '\n')
	entries["business-data.json"] = businessRaw
	var manifest businessPackageManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode Automation package manifest: %v", err)
	}
	hash := sha256.Sum256(businessRaw)
	manifest.BusinessData.SizeBytes = int64(len(businessRaw))
	manifest.BusinessData.SHA256 = hex.EncodeToString(hash[:])
	manifest.TotalBytes = manifest.FileBytes + int64(len(businessRaw))
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode Automation package manifest: %v", err)
	}
	entries["manifest.json"] = append(manifestRaw, '\n')
	return writeBusinessPackageTestZIP(t, entries)
}
