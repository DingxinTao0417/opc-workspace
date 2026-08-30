package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type taskArtifactTombstoneImportFixture struct {
	jsonBody             []byte
	zipBody              []byte
	taskID               string
	submissionID         string
	artifactID           string
	inboxID              string
	artifactDeletedEvent string
	submittedEventID     string
	producerID           string
	projectID            string
	task                 models.Task
	submission           models.TaskSubmission
	artifact             models.TaskArtifact
	inbox                models.InboxItem
	assignments          []models.TaskAssignment
	assignmentEvents     []models.WorkflowEvent
	taskEvents           []models.WorkflowEvent
}

type taskArtifactTombstoneFixtureOptions struct {
	changeTaskAndProjectFacts bool
	deactivateProducer        bool
	reassignAssignee          bool
	reopenAfterDelete         bool
	assignAfterReopen         bool
	reassignAfterReopen       bool
	requestChanges            bool
	changeInboxAfterDelete    bool
	cancelAfterSubmit         bool
	cancelAndReopenAgain      bool
}

func TestBusinessImportRestoresTaskArtifactFollowupTombstone(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		replaceToken string
		appendToken  string
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			replaceToken: importReplaceConfirmation,
			appendToken:  importAppendConfirmation,
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			replaceToken: packageImportReplaceConfirmation,
			appendToken:  packageImportAppendConfirmation,
		},
	}
	for _, format := range formats {
		format := format
		for _, appendTarget := range []bool{false, true} {
			appendTarget := appendTarget
			modeName := "replace"
			if appendTarget {
				modeName = "append"
			}
			t.Run(format.name+"/"+modeName, func(t *testing.T) {
				router, store, artifactDir, backupDir := newBackupTestAPI(t)
				confirmation := format.replaceToken
				wantMode := importModeReplaceEmpty
				if appendTarget {
					seedTaskArtifactImportTarget(t, store, artifactDir)
					confirmation = format.appendToken
					wantMode = importModeAppend
				}

				preview := performRequest(router, http.MethodPost, format.previewPath, format.body, nil)
				if preview.Code != http.StatusOK {
					t.Fatalf("%s %s preview = %d: %s", format.name, modeName, preview.Code, preview.Body.String())
				}
				var previewEnvelope struct {
					Data businessImportPreview `json:"data"`
				}
				if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
					!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" || previewEnvelope.Data.ApplyMode != wantMode {
					t.Fatalf("%s %s preview = %#v err=%v", format.name, modeName, previewEnvelope.Data, err)
				}

				apply := performRequest(
					router, http.MethodPost, format.applyPath, format.body,
					map[string]string{"X-Import-Confirmation": confirmation},
				)
				if apply.Code != http.StatusOK {
					var packageData businessExportPackage
					if format.name == "ZIP" {
						packageData = decodeBusinessImportJSON(t, readBusinessPackageEntries(t, format.body)["business-data.json"])
					} else {
						packageData = decodeBusinessImportJSON(t, format.body)
					}
					context, _ := gin.CreateTestContext(httptest.NewRecorder())
					context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
					detail := (&API{db: store.DB}).applyBusinessTables(context, packageData, wantMode, nil)
					t.Fatalf("%s %s apply = %d: %s detail=%v", format.name, modeName, apply.Code, apply.Body.String(), detail)
				}
				assertTaskArtifactTombstoneImported(t, store, fixture, appendTarget)
				if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
					t.Fatalf("%s %s rollback backups=%v", format.name, modeName, backups)
				}
				if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
					t.Fatalf("%s %s retained package staging=%v", format.name, modeName, staging)
				}
			})
		}
	}
}

func TestBusinessImportRestoresChangesRequestedTaskArtifactFollowupTombstone(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		requestChanges: true,
	})
	if fixture.task.Status != "in_progress" || fixture.submission.Status != "changes_requested" {
		t.Fatalf("source changes-requested state task=%#v submission=%#v", fixture.task, fixture.submission)
	}
	for _, assignment := range fixture.assignments {
		if assignment.UnassignedAt != nil || assignment.Reason != "" {
			t.Fatalf("request_changes ended source assignment: %#v", assignment)
		}
	}
	assertTaskArtifactAssignmentActionCount(t, fixture.assignmentEvents, "assignment_ended", 0)

	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importReplaceConfirmation,
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportReplaceConfirmation,
		},
	}
	for _, format := range formats {
		format := format
		t.Run(format.name, func(t *testing.T) {
			router, store, _, backupDir := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, format.previewPath, format.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("changes-requested %s preview = %d: %s", format.name, preview.Code, preview.Body.String())
			}
			var previewEnvelope struct {
				Data businessImportPreview `json:"data"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
				!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" ||
				previewEnvelope.Data.ApplyMode != importModeReplaceEmpty {
				t.Fatalf("changes-requested %s preview=%#v err=%v", format.name, previewEnvelope.Data, err)
			}
			apply := performRequest(
				router, http.MethodPost, format.applyPath, format.body,
				map[string]string{"X-Import-Confirmation": format.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("changes-requested %s apply = %d: %s", format.name, apply.Code, apply.Body.String())
			}
			assertTaskArtifactTombstoneImported(t, store, fixture, false)
			assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
			var importedTask models.Task
			taskErr := store.DB.First(&importedTask, "id = ?", fixture.taskID).Error
			if importedTask.Tags == nil && len(fixture.task.Tags) == 0 {
				importedTask.Tags = []models.Tag{}
			}
			if taskErr != nil || !reflect.DeepEqual(importedTask, fixture.task) {
				t.Fatalf("imported changes-requested Task=%#v, want %#v err=%v", importedTask, fixture.task, taskErr)
			}
			var importedSubmission models.TaskSubmission
			if err := store.DB.First(&importedSubmission, "id = ?", fixture.submissionID).Error; err != nil ||
				!reflect.DeepEqual(importedSubmission, fixture.submission) {
				t.Fatalf("imported changes-requested submission=%#v, want %#v err=%v", importedSubmission, fixture.submission, err)
			}
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
				t.Fatalf("changes-requested %s rollback backups=%v", format.name, backups)
			}
		})
	}
}

func TestBusinessImportRestoresWithdrawnTaskArtifactFollowupTombstone(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		cancelAfterSubmit: true,
	})
	if fixture.task.Status != "cancelled" || fixture.submission.Status != "withdrawn" ||
		fixture.submission.WithdrawnByActorID == nil || *fixture.submission.WithdrawnByActorID != models.BuiltinOwnerActorID ||
		fixture.submission.WithdrawnAt == nil {
		t.Fatalf("source withdrawn state task=%#v submission=%#v", fixture.task, fixture.submission)
	}
	for _, assignment := range fixture.assignments {
		if assignment.UnassignedAt == nil || assignment.Reason != "Task cancelled" {
			t.Fatalf("cancel did not end source assignment: %#v", assignment)
		}
	}
	assertTaskArtifactAssignmentActionCount(t, fixture.assignmentEvents, "assignment_ended", 2)
	packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
	_ = taskArtifactImportTaskSubmissionEventID(
		t, &packageData, fixture.taskID, fixture.submissionID, "task_submission_withdrawn",
	)

	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importReplaceConfirmation,
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportReplaceConfirmation,
		},
	}
	for _, format := range formats {
		format := format
		t.Run(format.name, func(t *testing.T) {
			router, store, _, backupDir := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, format.previewPath, format.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("withdrawn %s preview = %d: %s", format.name, preview.Code, preview.Body.String())
			}
			var previewEnvelope struct {
				Data businessImportPreview `json:"data"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
				!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" ||
				previewEnvelope.Data.ApplyMode != importModeReplaceEmpty {
				t.Fatalf("withdrawn %s preview=%#v err=%v", format.name, previewEnvelope.Data, err)
			}
			apply := performRequest(
				router, http.MethodPost, format.applyPath, format.body,
				map[string]string{"X-Import-Confirmation": format.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("withdrawn %s apply = %d: %s", format.name, apply.Code, apply.Body.String())
			}
			assertTaskArtifactTombstoneImported(t, store, fixture, false)
			assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
			var importedTask models.Task
			taskErr := store.DB.First(&importedTask, "id = ?", fixture.taskID).Error
			if importedTask.Tags == nil && len(fixture.task.Tags) == 0 {
				importedTask.Tags = []models.Tag{}
			}
			if taskErr != nil || !reflect.DeepEqual(importedTask, fixture.task) {
				t.Fatalf("imported withdrawn Task=%#v, want %#v err=%v", importedTask, fixture.task, taskErr)
			}
			var importedSubmission models.TaskSubmission
			if err := store.DB.First(&importedSubmission, "id = ?", fixture.submissionID).Error; err != nil ||
				!reflect.DeepEqual(importedSubmission, fixture.submission) {
				t.Fatalf("imported withdrawn submission=%#v, want %#v err=%v", importedSubmission, fixture.submission, err)
			}
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
				t.Fatalf("withdrawn %s rollback backups=%v", format.name, backups)
			}
		})
	}
}

func TestBusinessJSONImportRestoresLegacyTaskArtifactTombstoneWithoutInboxProjection(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	packageData := legacyTaskArtifactPackageWithoutInboxProjection(t, fixture)
	body := encodeBusinessImportJSON(t, packageData)

	router, store, _, backupDir := newBackupTestAPI(t)
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("legacy Task Artifact JSON preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
		!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" ||
		previewEnvelope.Data.ApplyMode != importModeReplaceEmpty {
		t.Fatalf("legacy Task Artifact JSON preview=%#v err=%v", previewEnvelope.Data, err)
	}
	apply := performRequest(
		router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation},
	)
	if apply.Code != http.StatusOK {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		detail := (&API{db: store.DB}).applyBusinessTables(context, packageData, importModeReplaceEmpty, nil)
		t.Fatalf("legacy Task Artifact JSON apply = %d: %s detail=%v", apply.Code, apply.Body.String(), detail)
	}

	var importedTask models.Task
	taskErr := store.DB.First(&importedTask, "id = ?", fixture.taskID).Error
	if importedTask.Tags == nil && len(fixture.task.Tags) == 0 {
		importedTask.Tags = []models.Tag{}
	}
	if taskErr != nil || !reflect.DeepEqual(importedTask, fixture.task) {
		t.Fatalf("imported legacy Task=%#v, want %#v err=%v", importedTask, fixture.task, taskErr)
	}
	var importedSubmission models.TaskSubmission
	if err := store.DB.First(&importedSubmission, "id = ?", fixture.submissionID).Error; err != nil ||
		!reflect.DeepEqual(importedSubmission, fixture.submission) {
		t.Fatalf("imported legacy submission=%#v, want %#v err=%v", importedSubmission, fixture.submission, err)
	}
	var importedArtifact models.TaskArtifact
	if err := store.DB.First(&importedArtifact, "id = ?", fixture.artifactID).Error; err != nil ||
		!reflect.DeepEqual(importedArtifact, fixture.artifact) {
		t.Fatalf("imported legacy Artifact=%#v, want %#v err=%v", importedArtifact, fixture.artifact, err)
	}
	assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
	var importedTaskEvents []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = 'task' AND aggregate_id = ? AND action <> ?", fixture.taskID, taskArtifactInboxGapMigrationAction).
		Order("id ASC").Find(&importedTaskEvents).Error; err != nil || !reflect.DeepEqual(importedTaskEvents, fixture.taskEvents) {
		t.Fatalf("imported legacy Task events=%#v, want %#v err=%v", importedTaskEvents, fixture.taskEvents, err)
	}
	markerID, _ := taskArtifactInboxGapMigrationEventID(fixture.artifactID)
	assertDatabaseCount(t, store,
		"SELECT COUNT(*) FROM workflow_events WHERE id = ? AND action = ? AND aggregate_type = 'task' AND aggregate_id = ? AND artifact_id = ?",
		1, markerID, taskArtifactInboxGapMigrationAction, fixture.taskID, fixture.artifactID,
	)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", 0, fixture.inboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ?", 0, fixture.inboxID)
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("legacy Task Artifact JSON rollback backups=%v", backups)
	}
}

func TestBusinessJSONImportRestoresLegacyTaskArtifactTombstoneFromSchema51Migration(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	sourceRuntime, sourceStore, sourceDatabasePath, sourceArtifactDir, sourceBackupDir := newBackupCapacityTestRuntime(t, nil)

	seed := performRequest(
		sourceRuntime.Engine, http.MethodPost, "/api/v1/imports/business-data", fixture.jsonBody,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation},
	)
	if seed.Code != http.StatusOK {
		t.Fatalf("seed schema-50 legacy source = %d: %s", seed.Code, seed.Body.String())
	}

	var immutableDeleteTriggerSQL string
	if err := sourceStore.SQL.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type = 'trigger' AND name = 'trg_workflow_events_immutable_delete'
	`).Scan(&immutableDeleteTriggerSQL); err != nil {
		t.Fatalf("read WorkflowEvent immutable-delete trigger: %v", err)
	}
	tx, err := sourceStore.SQL.Begin()
	if err != nil {
		t.Fatalf("begin schema-50 legacy source setup: %v", err)
	}
	rollback := func() { _ = tx.Rollback() }
	if _, err := tx.Exec("DROP TRIGGER trg_workflow_events_immutable_delete"); err != nil {
		rollback()
		t.Fatalf("temporarily drop WorkflowEvent immutable-delete trigger: %v", err)
	}
	if _, err := tx.Exec(
		"DELETE FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ?",
		fixture.inboxID,
	); err != nil {
		rollback()
		t.Fatalf("remove post-v23 Inbox event history from legacy fixture: %v", err)
	}
	if _, err := tx.Exec("DELETE FROM inbox_items WHERE id = ?", fixture.inboxID); err != nil {
		rollback()
		t.Fatalf("remove post-v23 Inbox row from legacy fixture: %v", err)
	}
	if _, err := tx.Exec(immutableDeleteTriggerSQL); err != nil {
		rollback()
		t.Fatalf("restore WorkflowEvent immutable-delete trigger: %v", err)
	}
	if _, err := tx.Exec(
		"UPDATE schema_migrations SET applied_at = ? WHERE version = 23 AND name = '023_task_artifact_inbox_projection.sql'",
		fixture.artifact.CreatedAt,
	); err != nil {
		rollback()
		t.Fatalf("set historical projection boundary: %v", err)
	}
	if _, err := tx.Exec("DROP INDEX ux_workflow_events_task_artifact_inbox_gap"); err != nil {
		rollback()
		t.Fatalf("remove schema-51 marker index before replay: %v", err)
	}
	if _, err := tx.Exec("DELETE FROM schema_migrations WHERE version = 51"); err != nil {
		rollback()
		t.Fatalf("rewind source to schema 50: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit schema-50 legacy source setup: %v", err)
	}
	if err := sourceRuntime.Close(); err != nil {
		t.Fatalf("close schema-50 source router: %v", err)
	}
	if err := sourceStore.Close(); err != nil {
		t.Fatalf("close schema-50 source database: %v", err)
	}

	upgradedStore, err := database.Open(sourceDatabasePath)
	if err != nil {
		t.Fatalf("apply real schema-51 migration: %v", err)
	}
	defer upgradedStore.Close()
	upgradedRuntime, err := NewRouter(upgradedStore.DB, Options{
		AppVersion: "0.1.0-test", Commit: "schema-51-legacy-export-test", SchemaVersion: upgradedStore.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		ArtifactDir: sourceArtifactDir, InvoicePDFDir: filepath.Join(filepath.Dir(sourceDatabasePath), "invoices"),
		DatabasePath: sourceDatabasePath, BackupDir: sourceBackupDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("open upgraded schema-51 source router: %v", err)
	}
	defer upgradedRuntime.Close()

	markerID, markerIDOK := taskArtifactInboxGapMigrationEventID(fixture.artifactID)
	if !markerIDOK {
		t.Fatalf("derive schema-51 marker ID for Artifact %s", fixture.artifactID)
	}
	var marker models.WorkflowEvent
	if err := upgradedStore.DB.First(&marker, "id = ? AND action = ?", markerID, taskArtifactInboxGapMigrationAction).Error; err != nil {
		t.Fatalf("load real schema-51 legacy marker: %v", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, marker.CreatedAt); err != nil {
		t.Fatalf("schema-51 marker created_at %q is not canonical RFC3339: %v", marker.CreatedAt, err)
	}
	assertDatabaseCount(t, upgradedStore, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", 0, fixture.inboxID)

	exported := performRequest(upgradedRuntime.Engine, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export migrated legacy source = %d: %s", exported.Code, exported.Body.String())
	}

	targetRouter, targetStore, _, targetBackupDir := newBackupTestAPI(t)
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data/preview", exported.Body.Bytes(), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview real migrated legacy export = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
		!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" ||
		previewEnvelope.Data.ApplyMode != importModeReplaceEmpty {
		t.Fatalf("real migrated legacy preview=%#v err=%v", previewEnvelope.Data, err)
	}
	apply := performRequest(
		targetRouter, http.MethodPost, "/api/v1/imports/business-data", exported.Body.Bytes(),
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation},
	)
	if apply.Code != http.StatusOK {
		t.Fatalf("apply real migrated legacy export = %d: %s", apply.Code, apply.Body.String())
	}
	var importedArtifact models.TaskArtifact
	if err := targetStore.DB.First(&importedArtifact, "id = ?", fixture.artifactID).Error; err != nil ||
		!reflect.DeepEqual(importedArtifact, fixture.artifact) {
		t.Fatalf("imported real migrated legacy Artifact=%#v, want %#v err=%v", importedArtifact, fixture.artifact, err)
	}
	assertDatabaseCount(t, targetStore,
		"SELECT COUNT(*) FROM workflow_events WHERE id = ? AND action = ? AND artifact_id = ?",
		1, markerID, taskArtifactInboxGapMigrationAction, fixture.artifactID,
	)
	assertDatabaseCount(t, targetStore, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", 0, fixture.inboxID)
	if backups := backupPackageDirectories(t, targetBackupDir); len(backups) != 1 {
		t.Fatalf("real migrated legacy rollback backups=%v", backups)
	}
}

func TestBusinessJSONImportRejectsInvalidLegacyTaskArtifactTombstoneWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	for _, mutation := range []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "missing schema v51 Inbox gap witness",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				markerID, markerIDOK := taskArtifactInboxGapMigrationEventID(fixture.artifactID)
				if !markerIDOK {
					t.Fatalf("derive Inbox gap marker ID for Artifact %s", fixture.artifactID)
				}
				removeTaskArtifactImportRow(t, packageData, "workflow_events", "id", markerID)
			},
		},
		{
			name: "missing task_artifact_deleted proof",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				removeTaskArtifactImportRow(t, packageData, "workflow_events", "id", fixture.artifactDeletedEvent)
			},
		},
		{
			name: "shadow Inbox retains only Artifact source ID",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				sourcePackage := decodeBusinessImportJSON(t, fixture.jsonBody)
				_, sourceInbox := taskArtifactImportRow(t, &sourcePackage, "inbox_items", "id", fixture.inboxID)
				shadowInbox := append([]any(nil), sourceInbox...)
				inboxTable := taskArtifactImportTable(t, packageData, "inbox_items")
				shadowInbox[columnIndex(inboxTable.Columns, "source_entity_type")] = "task_artifact_shadow"
				shadowInbox[columnIndex(inboxTable.Columns, "source_event_key")] = "shadow:artifact-history"
				shadowInbox[columnIndex(inboxTable.Columns, "payload_json")] = `{"shadow":true}`
				if shadowInbox[columnIndex(inboxTable.Columns, "source_entity_id")] != fixture.artifactID {
					t.Fatalf("shadow Inbox lost Artifact source ID: %#v", shadowInbox)
				}
				inboxTable.Rows = append(inboxTable.Rows, shadowInbox)
			},
		},
	} {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			packageData := legacyTaskArtifactPackageWithoutInboxProjection(t, fixture)
			mutation.mutate(t, &packageData)
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func TestBusinessJSONImportRejectsTamperedTaskArtifactInboxGapMigrationWitnessWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	markerID, markerIDOK := taskArtifactInboxGapMigrationEventID(fixture.artifactID)
	if !markerIDOK {
		t.Fatalf("derive Inbox gap marker ID for Artifact %s", fixture.artifactID)
	}
	for _, mutation := range []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "non-deterministic marker ID",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, marker := taskArtifactImportRow(t, packageData, "workflow_events", "id", markerID)
				last := "0"
				if markerID[len(markerID)-1:] == last {
					last = "1"
				}
				marker[columnIndex(table.Columns, "id")] = markerID[:len(markerID)-1] + last
			},
		},
		{
			name: "wrong marker source",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, marker := taskArtifactImportRow(t, packageData, "workflow_events", "id", markerID)
				currentIndex := columnIndex(table.Columns, "current_json")
				current := taskArtifactImportJSONObject(t, marker[currentIndex])
				current["source"] = "forged_migration"
				marker[currentIndex] = taskArtifactImportJSON(t, current)
			},
		},
		{
			name: "wrong marker Artifact ID",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, marker := taskArtifactImportRow(t, packageData, "workflow_events", "id", markerID)
				currentIndex := columnIndex(table.Columns, "current_json")
				current := taskArtifactImportJSONObject(t, marker[currentIndex])
				current["artifact_id"] = fixture.inboxID
				marker[currentIndex] = taskArtifactImportJSON(t, current)
			},
		},
		{
			name: "marker predates Artifact",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				createdAt, err := time.Parse(time.RFC3339Nano, fixture.artifact.CreatedAt)
				if err != nil {
					t.Fatalf("parse source Artifact created_at: %v", err)
				}
				table, marker := taskArtifactImportRow(t, packageData, "workflow_events", "id", markerID)
				marker[columnIndex(table.Columns, "created_at")] = createdAt.Add(-time.Nanosecond).Format(time.RFC3339Nano)
			},
		},
	} {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			packageData := legacyTaskArtifactPackageWithoutInboxProjection(t, fixture)
			mutation.mutate(t, &packageData)
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func TestBusinessImportRestoresTaskArtifactTombstoneAfterInboxReadAndReopen(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		changeInboxAfterDelete: true,
	})
	if fixture.inbox.Status != "open" || fixture.inbox.ReadAt == nil || fixture.inbox.SourceDeletedAt == nil ||
		fixture.inbox.Version != 5 || fixture.inbox.DismissedAt != nil || fixture.inbox.DismissReason != nil {
		t.Fatalf("source post-delete Inbox facts=%#v", fixture.inbox)
	}
	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importReplaceConfirmation,
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportReplaceConfirmation,
		},
	}
	for _, format := range formats {
		format := format
		t.Run(format.name, func(t *testing.T) {
			router, store, _, backupDir := newBackupTestAPI(t)
			preview := performRequest(router, http.MethodPost, format.previewPath, format.body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("post-delete Inbox %s preview = %d: %s", format.name, preview.Code, preview.Body.String())
			}
			var previewEnvelope struct {
				Data businessImportPreview `json:"data"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
				!previewEnvelope.Data.CanApply || previewEnvelope.Data.Blocker != "" ||
				previewEnvelope.Data.ApplyMode != importModeReplaceEmpty {
				t.Fatalf("post-delete Inbox %s preview=%#v err=%v", format.name, previewEnvelope.Data, err)
			}
			apply := performRequest(
				router, http.MethodPost, format.applyPath, format.body,
				map[string]string{"X-Import-Confirmation": format.confirmation},
			)
			if apply.Code != http.StatusOK {
				t.Fatalf("post-delete Inbox %s apply = %d: %s", format.name, apply.Code, apply.Body.String())
			}
			assertTaskArtifactTombstoneImported(t, store, fixture, false)
			assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
				t.Fatalf("post-delete Inbox %s rollback backups=%v", format.name, backups)
			}
		})
	}
}

func TestBusinessJSONImportAcceptsTaskArtifactTombstoneAfterTaskAndProjectFactsChange(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		changeTaskAndProjectFacts: true,
	})
	if fixture.projectID == "" || fixture.task.ProjectID == nil || *fixture.task.ProjectID != fixture.projectID ||
		fixture.task.Title != "Renamed portable Task" || fixture.task.Priority != "P0" {
		t.Fatalf("final source Task facts=%#v project_id=%q", fixture.task, fixture.projectID)
	}
	var sourcePayload map[string]any
	if err := json.Unmarshal([]byte(fixture.inbox.PayloadJSON), &sourcePayload); err != nil ||
		sourcePayload["task_title"] != "Portable source Task" || sourcePayload["project_name"] != "Portable source Project" ||
		sourcePayload["task_title"] == fixture.task.Title || sourcePayload["project_name"] == "Renamed portable Project" {
		t.Fatalf("immutable source payload=%#v err=%v", sourcePayload, err)
	}

	store := importTaskArtifactTombstoneJSONReplace(t, fixture)
	var importedTask models.Task
	if err := store.DB.First(&importedTask, "id = ?", fixture.taskID).Error; err != nil ||
		importedTask.Title != "Renamed portable Task" || importedTask.Priority != "P0" ||
		importedTask.ProjectID == nil || *importedTask.ProjectID != fixture.projectID {
		t.Fatalf("imported final Task facts=%#v err=%v", importedTask, err)
	}
	var projectName string
	if err := store.DB.Table("projects").Where("id = ?", fixture.projectID).Pluck("name", &projectName).Error; err != nil || projectName != "Renamed portable Project" {
		t.Fatalf("imported final Project name=%q err=%v", projectName, err)
	}
}

func TestBusinessJSONImportAcceptsTaskArtifactTombstoneWithInactiveProducer(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		deactivateProducer: true,
	})
	store := importTaskArtifactTombstoneJSONReplace(t, fixture)
	var actor models.Actor
	if err := store.DB.First(&actor, "id = ?", fixture.producerID).Error; err != nil || actor.Status != "inactive" || actor.Version != 2 {
		t.Fatalf("imported inactive producer=%#v err=%v", actor, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_assignments WHERE task_id = ? AND actor_id = ? AND role = 'assignee' AND unassigned_at IS NOT NULL", 1, fixture.taskID, fixture.producerID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'assignment_ended'", 2, fixture.taskID)
}

func TestBusinessJSONAppendAcceptsTaskArtifactTombstoneAfterAssigneeReassignment(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		reassignAssignee: true,
	})
	if len(fixture.assignments) != 3 {
		t.Fatalf("reassigned source assignment history=%#v", fixture.assignments)
	}
	assertTaskArtifactAssignmentActionCount(t, fixture.assignmentEvents, "assignment_reassigned", 1)
	store := importTaskArtifactTombstoneJSON(t, fixture, true)
	assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
}

func TestBusinessJSONImportAcceptsReopenedTaskArtifactTombstoneWithInactiveEndedProducer(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		deactivateProducer: true,
		reopenAfterDelete:  true,
	})
	if fixture.task.Status != "todo" {
		t.Fatalf("source Task was not reopened: %#v", fixture.task)
	}
	store := importTaskArtifactTombstoneJSON(t, fixture, false)
	var importedTask models.Task
	if err := store.DB.First(&importedTask, "id = ?", fixture.taskID).Error; err != nil || importedTask.Status != "todo" || importedTask.Version != fixture.task.Version {
		t.Fatalf("imported reopened Task=%#v err=%v", importedTask, err)
	}
	var actor models.Actor
	if err := store.DB.First(&actor, "id = ?", fixture.producerID).Error; err != nil || actor.Status != "inactive" || actor.Version != 2 {
		t.Fatalf("imported inactive ended producer=%#v err=%v", actor, err)
	}
	assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
}

func TestBusinessJSONImportAcceptsReopenedTaskArtifactTombstoneWithActiveAssignmentHistory(t *testing.T) {
	for _, test := range []struct {
		name                 string
		reassignAfterReopen  bool
		appendTarget         bool
		wantReassignedEvents int
	}{
		{name: "active assignment created after reopen", appendTarget: false},
		{name: "active assignment reassigned after reopen", reassignAfterReopen: true, appendTarget: true, wantReassignedEvents: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
				reopenAfterDelete:   true,
				assignAfterReopen:   true,
				reassignAfterReopen: test.reassignAfterReopen,
			})
			if fixture.task.Status != "todo" {
				t.Fatalf("source Task was not reopened: %#v", fixture.task)
			}
			activeAssignments := 0
			for _, assignment := range fixture.assignments {
				if assignment.UnassignedAt == nil {
					activeAssignments++
				}
			}
			if activeAssignments != 1 {
				t.Fatalf("source active assignments=%d, want 1: %#v", activeAssignments, fixture.assignments)
			}
			assertTaskArtifactAssignmentActionCount(t, fixture.assignmentEvents, "assignment_created", 3)
			assertTaskArtifactAssignmentActionCount(t, fixture.assignmentEvents, "assignment_reassigned", test.wantReassignedEvents)

			store := importTaskArtifactTombstoneJSON(t, fixture, test.appendTarget)
			assertTaskArtifactAssignmentHistoryImported(t, store, fixture)
			assertDatabaseCount(t, store,
				"SELECT COUNT(*) FROM task_assignments WHERE task_id = ? AND unassigned_at IS NULL",
				1, fixture.taskID,
			)
		})
	}
}

func TestBusinessJSONImportRejectsReopenedTaskWithResurrectedHistoricalAssignmentWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		reopenAfterDelete: true,
	})
	packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
	assignmentID := ""
	for _, assignment := range fixture.assignments {
		if assignment.Reason == "Task review accepted" {
			assignmentID = assignment.ID
			break
		}
	}
	if assignmentID == "" {
		t.Fatalf("reopened fixture has no review-ended assignment: %#v", fixture.assignments)
	}
	assignmentTable, assignmentRow := taskArtifactImportRow(t, &packageData, "task_assignments", "id", assignmentID)
	assignmentRow[columnIndex(assignmentTable.Columns, "unassigned_at")] = nil
	assignmentRow[columnIndex(assignmentTable.Columns, "reason")] = ""
	if eventID := taskArtifactImportAssignmentEventID(t, &packageData, assignmentID, "assignment_ended"); eventID == "" {
		t.Fatal("reopened fixture lost the historical assignment_ended proof")
	}

	assertTaskArtifactImportRejectedWithoutSideEffects(
		t, encodeBusinessImportJSON(t, packageData),
		"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
		importAppendConfirmation, fixture,
	)
}

func TestBusinessImportRejectsReopenedTaskArtifactInboxTypeDriftWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		reopenAfterDelete: true,
	})
	mutations := []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "final Inbox type only",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				inboxTable, inboxRow := taskArtifactImportRow(t, packageData, "inbox_items", "id", fixture.inboxID)
				inboxRow[columnIndex(inboxTable.Columns, "source_entity_type")] = "task_artifact_shadow"
			},
		},
		{
			name: "final Inbox type with projected anchor hidden",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				inboxTable, inboxRow := taskArtifactImportRow(t, packageData, "inbox_items", "id", fixture.inboxID)
				inboxRow[columnIndex(inboxTable.Columns, "source_entity_type")] = "task_artifact_shadow"
				events := taskArtifactImportTable(t, packageData, "workflow_events")
				aggregateIndex := columnIndex(events.Columns, "aggregate_id")
				actionIndex := columnIndex(events.Columns, "action")
				for _, row := range events.Rows {
					if row[aggregateIndex] == fixture.inboxID && row[actionIndex] == "source_projected" {
						row[actionIndex] = "source_projected_shadow"
						return
					}
				}
				t.Fatal("fixture has no source_projected reverse anchor")
			},
		},
		{
			name: "final Inbox type with deleted anchor hidden",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				inboxTable, inboxRow := taskArtifactImportRow(t, packageData, "inbox_items", "id", fixture.inboxID)
				inboxRow[columnIndex(inboxTable.Columns, "source_entity_type")] = "task_artifact_shadow"
				events := taskArtifactImportTable(t, packageData, "workflow_events")
				aggregateIndex := columnIndex(events.Columns, "aggregate_id")
				actionIndex := columnIndex(events.Columns, "action")
				for _, row := range events.Rows {
					if row[aggregateIndex] == fixture.inboxID && row[actionIndex] == "source_deleted" {
						row[actionIndex] = "source_deleted_shadow"
						return
					}
				}
				t.Fatal("fixture has no source_deleted reverse anchor")
			},
		},
		{
			name: "final Inbox type with reverse proofs removed",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				inboxTable, inboxRow := taskArtifactImportRow(t, packageData, "inbox_items", "id", fixture.inboxID)
				inboxRow[columnIndex(inboxTable.Columns, "source_entity_type")] = "task_artifact_shadow"
				events := taskArtifactImportTable(t, packageData, "workflow_events")
				aggregateIndex := columnIndex(events.Columns, "aggregate_id")
				actionIndex := columnIndex(events.Columns, "action")
				previousIndex := columnIndex(events.Columns, "previous_json")
				currentIndex := columnIndex(events.Columns, "current_json")
				projectedRemoved := false
				deletedRemoved := false
				for _, row := range events.Rows {
					if row[aggregateIndex] != fixture.inboxID {
						continue
					}
					switch row[actionIndex] {
					case "source_projected":
						row[currentIndex] = nil
						projectedRemoved = true
					case "source_deleted":
						row[previousIndex] = nil
						deletedRemoved = true
					}
				}
				if !projectedRemoved || !deletedRemoved {
					t.Fatalf("source proof rows projected=%t deleted=%t", projectedRemoved, deletedRemoved)
				}
			},
		},
	}
	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
		decode       func(*testing.T, []byte) businessExportPackage
		encode       func(*testing.T, []byte, businessExportPackage) []byte
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importAppendConfirmation,
			decode:       decodeBusinessImportJSON,
			encode: func(t *testing.T, _ []byte, packageData businessExportPackage) []byte {
				return encodeBusinessImportJSON(t, packageData)
			},
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportAppendConfirmation,
			decode: func(t *testing.T, body []byte) businessExportPackage {
				return decodeBusinessImportJSON(t, readBusinessPackageEntries(t, body)["business-data.json"])
			},
			encode: encodeBusinessImportPackage,
		},
	}
	for _, mutation := range mutations {
		mutation := mutation
		for _, format := range formats {
			format := format
			t.Run(mutation.name+"/"+format.name, func(t *testing.T) {
				packageData := format.decode(t, format.body)
				mutation.mutate(t, &packageData)
				assertTaskArtifactImportRejectedWithoutSideEffects(
					t, format.encode(t, format.body, packageData),
					format.previewPath, format.applyPath, format.confirmation, fixture,
				)
			})
		}
	}
}

func TestBusinessJSONImportRejectsInvalidTaskArtifactAssignmentReverseClosureWithoutSideEffects(t *testing.T) {
	t.Run("missing assignment_created keeps row and ending event", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixture(t)
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		assignmentID := taskArtifactImportAssignmentID(t, &packageData, fixture.taskID, "assignee")
		createdEventID := taskArtifactImportAssignmentEventID(t, &packageData, assignmentID, "assignment_created")
		removeTaskArtifactImportRow(t, &packageData, "workflow_events", "id", createdEventID)
		if endedEventID := taskArtifactImportAssignmentEventID(t, &packageData, assignmentID, "assignment_ended"); endedEventID == "" {
			t.Fatal("mutation unexpectedly removed the assignment ending proof")
		}
		assertTaskArtifactImportRejectedWithoutSideEffects(
			t, encodeBusinessImportJSON(t, packageData),
			"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
			importAppendConfirmation, fixture,
		)
	})

	t.Run("reassigned old row also has assignment_ended", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
			reassignAssignee: true,
		})
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		appendTaskArtifactDuplicateAssignmentEndedEvent(t, &packageData, fixture)
		assertTaskArtifactImportRejectedWithoutSideEffects(
			t, encodeBusinessImportJSON(t, packageData),
			"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
			importAppendConfirmation, fixture,
		)
	})

	t.Run("same-role assignment intervals overlap", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
			reopenAfterDelete: true,
			assignAfterReopen: true,
		})
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		overlapTaskArtifactAssigneeIntervals(t, &packageData, fixture)
		assertTaskArtifactImportRejectedWithoutSideEffects(
			t, encodeBusinessImportJSON(t, packageData),
			"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
			importAppendConfirmation, fixture,
		)
	})
}

func TestBusinessJSONImportRejectsInvalidTaskArtifactSubmissionDispositionWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	baseline := decodeBusinessImportJSON(t, fixture.jsonBody)
	dispositionEventID := taskArtifactImportTaskSubmissionEventID(
		t, &baseline, fixture.taskID, fixture.submissionID, "task_review_accepted",
	)
	mutations := []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "final submission is still pending review",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, row := taskArtifactImportRow(t, packageData, "task_submissions", "id", fixture.submissionID)
				row[columnIndex(table.Columns, "status")] = "pending_review"
				row[columnIndex(table.Columns, "reviewed_by_actor_id")] = nil
				row[columnIndex(table.Columns, "reviewed_at")] = nil
				row[columnIndex(table.Columns, "review_reason")] = nil
			},
		},
		{
			name: "missing review disposition event",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				removeTaskArtifactImportRow(t, packageData, "workflow_events", "id", dispositionEventID)
			},
		},
		{
			name: "tampered review disposition event",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				previous, current := taskArtifactImportEventObjects(t, packageData, dispositionEventID)
				current["submission_status"] = "changes_requested"
				setTaskArtifactImportEventObjects(t, packageData, dispositionEventID, previous, current)
			},
		},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
			mutation.mutate(t, &packageData)
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func TestBusinessJSONImportRejectsTaskArtifactDispositionChronologyAndReopenWitnessWithoutSideEffects(t *testing.T) {
	assertRejected := func(t *testing.T, fixture taskArtifactTombstoneImportFixture, packageData businessExportPackage) {
		t.Helper()
		assertTaskArtifactImportRejectedWithoutSideEffects(
			t, encodeBusinessImportJSON(t, packageData),
			"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
			importAppendConfirmation, fixture,
		)
	}

	t.Run("review disposition occurs after Artifact deletion", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixture(t)
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		deletedAt, err := time.Parse(time.RFC3339Nano, *fixture.artifact.DeletedAt)
		if err != nil {
			t.Fatalf("parse Artifact deleted_at: %v", err)
		}
		lateReviewedAt := deletedAt.Add(time.Nanosecond).Format(time.RFC3339Nano)
		submissionTable, submissionRow := taskArtifactImportRow(t, &packageData, "task_submissions", "id", fixture.submissionID)
		submissionRow[columnIndex(submissionTable.Columns, "reviewed_at")] = lateReviewedAt
		taskTable, taskRow := taskArtifactImportRow(t, &packageData, "tasks", "id", fixture.taskID)
		taskRow[columnIndex(taskTable.Columns, "reviewed_at")] = lateReviewedAt
		taskRow[columnIndex(taskTable.Columns, "completed_at")] = lateReviewedAt

		dispositionID := taskArtifactImportTaskSubmissionEventID(
			t, &packageData, fixture.taskID, fixture.submissionID, "task_review_accepted",
		)
		dispositionTable, dispositionRow := taskArtifactImportRow(t, &packageData, "workflow_events", "id", dispositionID)
		dispositionRow[columnIndex(dispositionTable.Columns, "created_at")] = lateReviewedAt
		dispositionPrevious, dispositionCurrent := taskArtifactImportEventObjects(t, &packageData, dispositionID)
		dispositionCurrent["reviewed_at"] = lateReviewedAt
		dispositionCurrent["completed_at"] = lateReviewedAt
		setTaskArtifactImportEventObjects(t, &packageData, dispositionID, dispositionPrevious, dispositionCurrent)

		deletePrevious, deleteCurrent := taskArtifactImportEventObjects(t, &packageData, fixture.artifactDeletedEvent)
		for _, state := range []map[string]any{deletePrevious, deleteCurrent} {
			state["reviewed_at"] = lateReviewedAt
			state["completed_at"] = lateReviewedAt
		}
		setTaskArtifactImportEventObjects(t, &packageData, fixture.artifactDeletedEvent, deletePrevious, deleteCurrent)
		assertRejected(t, fixture, packageData)
	})

	t.Run("review disposition version follows Artifact deletion predecessor", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixture(t)
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		deletePrevious, _ := taskArtifactImportEventObjects(t, &packageData, fixture.artifactDeletedEvent)
		deletePreviousVersion := int64(deletePrevious["version"].(float64))
		dispositionID := taskArtifactImportTaskSubmissionEventID(
			t, &packageData, fixture.taskID, fixture.submissionID, "task_review_accepted",
		)
		dispositionPrevious, dispositionCurrent := taskArtifactImportEventObjects(t, &packageData, dispositionID)
		dispositionPrevious["version"] = deletePreviousVersion
		dispositionCurrent["version"] = deletePreviousVersion + 1
		setTaskArtifactImportEventObjects(t, &packageData, dispositionID, dispositionPrevious, dispositionCurrent)
		assertRejected(t, fixture, packageData)
	})

	for _, mutation := range []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage, string)
	}{
		{
			name: "withdrawn cancellation witness envelope",
			mutate: func(t *testing.T, packageData *businessExportPackage, eventID string) {
				table, row := taskArtifactImportRow(t, packageData, "workflow_events", "id", eventID)
				row[columnIndex(table.Columns, "actor_id")] = models.BuiltinSystemActorID
			},
		},
		{
			name: "withdrawn cancellation witness version successor",
			mutate: func(t *testing.T, packageData *businessExportPackage, eventID string) {
				previous, current := taskArtifactImportEventObjects(t, packageData, eventID)
				current["version"] = previous["version"]
				setTaskArtifactImportEventObjects(t, packageData, eventID, previous, current)
			},
		},
	} {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
				cancelAfterSubmit: true,
			})
			packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
			cancelledID := taskArtifactImportTaskEventID(t, &packageData, fixture.taskID, "task_cancelled")
			mutation.mutate(t, &packageData, cancelledID)
			assertRejected(t, fixture, packageData)
		})
	}

	t.Run("reopen event predates terminal predecessor", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
			reopenAfterDelete: true,
		})
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		terminalID := taskArtifactImportTaskSubmissionEventID(
			t, &packageData, fixture.taskID, fixture.submissionID, "task_review_accepted",
		)
		terminalTable, terminalRow := taskArtifactImportRow(t, &packageData, "workflow_events", "id", terminalID)
		terminalAt, err := time.Parse(time.RFC3339Nano, terminalRow[columnIndex(terminalTable.Columns, "created_at")].(string))
		if err != nil {
			t.Fatalf("parse terminal predecessor created_at: %v", err)
		}
		reopenedID := taskArtifactImportTaskEventID(t, &packageData, fixture.taskID, "task_reopened")
		reopenedTable, reopenedRow := taskArtifactImportRow(t, &packageData, "workflow_events", "id", reopenedID)
		reopenedRow[columnIndex(reopenedTable.Columns, "created_at")] = terminalAt.Add(-time.Nanosecond).Format(time.RFC3339Nano)
		assertRejected(t, fixture, packageData)
	})

	t.Run("review predecessor cannot masquerade as manual completion", func(t *testing.T) {
		fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
			reopenAfterDelete: true,
		})
		packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
		acceptedID := taskArtifactImportTaskSubmissionEventID(
			t, &packageData, fixture.taskID, fixture.submissionID, "task_review_accepted",
		)
		acceptedTable, acceptedRow := taskArtifactImportRow(t, &packageData, "workflow_events", "id", acceptedID)
		acceptedProof := append([]any(nil), acceptedRow...)
		acceptedProof[columnIndex(acceptedTable.Columns, "id")] = "018f0000-0000-7000-8000-00000000f72d"
		acceptedTable.Rows = append(acceptedTable.Rows, acceptedProof)

		acceptedRow[columnIndex(acceptedTable.Columns, "action")] = "task_completed"
		acceptedRow[columnIndex(acceptedTable.Columns, "submission_id")] = nil
		acceptedRow[columnIndex(acceptedTable.Columns, "request_id")] = "018f0000-0000-7000-8000-00000000f72e"
		acceptedRow[columnIndex(acceptedTable.Columns, "command_seq")] = float64(1)
		reopenedID := taskArtifactImportTaskEventID(t, &packageData, fixture.taskID, "task_reopened")
		reopenedPrevious, reopenedCurrent := taskArtifactImportEventObjects(t, &packageData, reopenedID)
		manualCurrent := taskArtifactImportJSONObject(t, taskArtifactImportJSON(t, reopenedPrevious))
		manualPrevious := taskArtifactImportJSONObject(t, taskArtifactImportJSON(t, reopenedPrevious))
		currentVersion := int64(manualCurrent["version"].(float64))
		manualPrevious["version"] = currentVersion - 1
		manualPrevious["status"] = "in_progress"
		manualPrevious["completed_at"] = nil
		completedAt, ok := manualCurrent["completed_at"].(string)
		if !ok || completedAt == "" {
			t.Fatalf("reopen previous has no completion timestamp: %#v", reopenedPrevious)
		}
		acceptedRow[columnIndex(acceptedTable.Columns, "created_at")] = completedAt
		setTaskArtifactImportEventObjects(t, &packageData, acceptedID, manualPrevious, manualCurrent)
		setTaskArtifactImportEventObjects(t, &packageData, reopenedID, reopenedPrevious, reopenedCurrent)
		assertRejected(t, fixture, packageData)
	})
}

func TestBusinessJSONImportRejectsReusedReopenTerminalPredecessorWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		reopenAfterDelete:    true,
		cancelAndReopenAgain: true,
	})
	packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
	type reopenProof struct {
		id       string
		version  int64
		previous map[string]any
		current  map[string]any
	}
	proofs := make([]reopenProof, 0, 2)
	events := taskArtifactImportTable(t, &packageData, "workflow_events")
	idIndex := columnIndex(events.Columns, "id")
	aggregateTypeIndex := columnIndex(events.Columns, "aggregate_type")
	aggregateIDIndex := columnIndex(events.Columns, "aggregate_id")
	actionIndex := columnIndex(events.Columns, "action")
	for _, row := range events.Rows {
		if row[aggregateTypeIndex] != "task" || row[aggregateIDIndex] != fixture.taskID || row[actionIndex] != "task_reopened" {
			continue
		}
		id, ok := row[idIndex].(string)
		if !ok || id == "" {
			t.Fatalf("reopen event has invalid ID: %#v", row)
		}
		previous, current := taskArtifactImportEventObjects(t, &packageData, id)
		version, ok := current["version"].(float64)
		if !ok {
			t.Fatalf("reopen event %s current version=%#v", id, current["version"])
		}
		proofs = append(proofs, reopenProof{id: id, version: int64(version), previous: previous, current: current})
	}
	if len(proofs) != 2 {
		t.Fatalf("reopen proofs=%#v, want exactly 2", proofs)
	}
	first, second := proofs[0], proofs[1]
	if first.version > second.version {
		first, second = second, first
	}
	if first.previous["status"] != "done" || second.previous["status"] != "cancelled" ||
		first.current["status"] != "todo" || second.current["status"] != "todo" {
		t.Fatalf("source reopen predecessor chain first=%#v second=%#v", first, second)
	}
	reusedPrevious := taskArtifactImportJSONObject(t, taskArtifactImportJSON(t, first.previous))
	reusedCurrent := taskArtifactImportJSONObject(t, taskArtifactImportJSON(t, first.current))
	reusedPrevious["version"] = second.previous["version"]
	reusedCurrent["version"] = second.current["version"]
	setTaskArtifactImportEventObjects(t, &packageData, second.id, reusedPrevious, reusedCurrent)

	assertTaskArtifactImportRejectedWithoutSideEffects(
		t, encodeBusinessImportJSON(t, packageData),
		"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
		importAppendConfirmation, fixture,
	)
}

func TestBusinessImportRejectsInvalidTaskArtifactFollowupTombstoneWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	mutations := []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "missing Artifact deleted event",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				removeTaskArtifactImportRow(t, packageData, "workflow_events", "id", fixture.artifactDeletedEvent)
			},
		},
		{
			name: "tampered Artifact deleted event",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, row := taskArtifactImportRow(t, packageData, "workflow_events", "id", fixture.artifactDeletedEvent)
				currentIndex := columnIndex(table.Columns, "current_json")
				var current map[string]any
				if err := json.Unmarshal([]byte(row[currentIndex].(string)), &current); err != nil {
					t.Fatalf("decode task_artifact_deleted current_json: %v", err)
				}
				current["reason"] = "tampered deletion reason"
				encoded, err := json.Marshal(current)
				if err != nil {
					t.Fatalf("encode task_artifact_deleted current_json: %v", err)
				}
				row[currentIndex] = string(encoded)
			},
		},
		{
			name: "Artifact final deletion fields mismatch",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table, row := taskArtifactImportRow(t, packageData, "task_artifacts", "id", fixture.artifactID)
				row[columnIndex(table.Columns, "delete_reason")] = "mismatched final reason"
			},
		},
		{
			name: "missing Artifact aggregate row",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				removeTaskArtifactImportRow(t, packageData, "task_artifacts", "id", fixture.artifactID)
			},
		},
		{
			name: "terminal Task has active assignment",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				table := taskArtifactImportTable(t, packageData, "task_assignments")
				taskIndex := columnIndex(table.Columns, "task_id")
				roleIndex := columnIndex(table.Columns, "role")
				unassignedIndex := columnIndex(table.Columns, "unassigned_at")
				reasonIndex := columnIndex(table.Columns, "reason")
				for _, row := range table.Rows {
					if row[taskIndex] == fixture.taskID && row[roleIndex] == "assignee" {
						row[unassignedIndex] = nil
						row[reasonIndex] = ""
						return
					}
				}
				t.Fatalf("source package has no assignee for terminal Task %s", fixture.taskID)
			},
		},
	}
	formats := []struct {
		name         string
		body         []byte
		previewPath  string
		applyPath    string
		confirmation string
		decode       func(*testing.T, []byte) businessExportPackage
		encode       func(*testing.T, []byte, businessExportPackage) []byte
	}{
		{
			name:         "JSON",
			body:         fixture.jsonBody,
			previewPath:  "/api/v1/imports/business-data/preview",
			applyPath:    "/api/v1/imports/business-data",
			confirmation: importAppendConfirmation,
			decode:       decodeBusinessImportJSON,
			encode: func(t *testing.T, _ []byte, packageData businessExportPackage) []byte {
				return encodeBusinessImportJSON(t, packageData)
			},
		},
		{
			name:         "ZIP",
			body:         fixture.zipBody,
			previewPath:  "/api/v1/imports/business-package/preview",
			applyPath:    "/api/v1/imports/business-package",
			confirmation: packageImportAppendConfirmation,
			decode: func(t *testing.T, body []byte) businessExportPackage {
				return decodeBusinessImportJSON(t, readBusinessPackageEntries(t, body)["business-data.json"])
			},
			encode: encodeBusinessImportPackage,
		},
	}
	for _, mutation := range mutations {
		mutation := mutation
		for _, format := range formats {
			format := format
			t.Run(mutation.name+"/"+format.name, func(t *testing.T) {
				packageData := format.decode(t, format.body)
				mutation.mutate(t, &packageData)
				body := format.encode(t, format.body, packageData)
				assertTaskArtifactImportRejectedWithoutSideEffects(
					t, body, format.previewPath, format.applyPath, format.confirmation, fixture,
				)
			})
		}
	}
}

func TestBusinessJSONImportRejectsInvalidTaskArtifactTombstoneChronologyAndAssignmentProof(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	mutations := []struct {
		name   string
		mutate func(*testing.T, *businessExportPackage)
	}{
		{
			name: "Artifact delete version predates submit",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				_, submitCurrent := taskArtifactImportEventObjects(t, packageData, fixture.submittedEventID)
				submitVersion := int64(submitCurrent["version"].(float64))
				previous, current := taskArtifactImportEventObjects(t, packageData, fixture.artifactDeletedEvent)
				previous["version"] = submitVersion - 2
				current["version"] = submitVersion - 1
				setTaskArtifactImportEventObjects(t, packageData, fixture.artifactDeletedEvent, previous, current)
			},
		},
		{
			name: "Artifact deleted_at predates created_at",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				createdAt, err := time.Parse(time.RFC3339Nano, fixture.artifact.CreatedAt)
				if err != nil {
					t.Fatalf("parse source Artifact created_at: %v", err)
				}
				beforeCreated := createdAt.Add(-time.Nanosecond).Format(time.RFC3339Nano)
				artifactTable, artifactRow := taskArtifactImportRow(t, packageData, "task_artifacts", "id", fixture.artifactID)
				artifactRow[columnIndex(artifactTable.Columns, "deleted_at")] = beforeCreated
				inboxTable, inboxRow := taskArtifactImportRow(t, packageData, "inbox_items", "id", fixture.inboxID)
				inboxRow[columnIndex(inboxTable.Columns, "source_deleted_at")] = beforeCreated
				inboxRow[columnIndex(inboxTable.Columns, "updated_at")] = beforeCreated
				taskTable, taskRow := taskArtifactImportRow(t, packageData, "tasks", "id", fixture.taskID)
				taskRow[columnIndex(taskTable.Columns, "updated_at")] = beforeCreated

				workflowEvents := taskArtifactImportTable(t, packageData, "workflow_events")
				actionIndex := columnIndex(workflowEvents.Columns, "action")
				aggregateIndex := columnIndex(workflowEvents.Columns, "aggregate_id")
				createdIndex := columnIndex(workflowEvents.Columns, "created_at")
				currentIndex := columnIndex(workflowEvents.Columns, "current_json")
				for _, row := range workflowEvents.Rows {
					if row[actionIndex] == "source_deleted" && row[aggregateIndex] == fixture.inboxID {
						row[createdIndex] = beforeCreated
						current := taskArtifactImportJSONObject(t, row[currentIndex])
						current["source_deleted_at"] = beforeCreated
						row[currentIndex] = taskArtifactImportJSON(t, current)
					}
					if row[actionIndex] == "task_artifact_deleted" && row[aggregateIndex] == fixture.taskID {
						row[createdIndex] = beforeCreated
						current := taskArtifactImportJSONObject(t, row[currentIndex])
						current["deleted_at"] = beforeCreated
						row[currentIndex] = taskArtifactImportJSON(t, current)
					}
				}
			},
		},
		{
			name: "submit assignee interval is missing",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				assignmentID := taskArtifactImportAssignmentID(t, packageData, fixture.taskID, "assignee")
				removeTaskArtifactImportRow(t, packageData, "task_assignments", "id", assignmentID)
				removeTaskArtifactImportRows(t, packageData, "workflow_events", "assignment_id", assignmentID)
			},
		},
		{
			name: "submit reviewer interval starts too late",
			mutate: func(t *testing.T, packageData *businessExportPackage) {
				assignmentID := taskArtifactImportAssignmentID(t, packageData, fixture.taskID, "reviewer")
				submitTable, submitRow := taskArtifactImportRow(t, packageData, "workflow_events", "id", fixture.submittedEventID)
				submittedAt, err := time.Parse(time.RFC3339Nano, submitRow[columnIndex(submitTable.Columns, "created_at")].(string))
				if err != nil {
					t.Fatalf("parse task_output_submitted created_at: %v", err)
				}
				assignedAt := submittedAt.Add(time.Nanosecond).Format(time.RFC3339Nano)
				assignmentTable, assignmentRow := taskArtifactImportRow(t, packageData, "task_assignments", "id", assignmentID)
				assignmentRow[columnIndex(assignmentTable.Columns, "assigned_at")] = assignedAt

				events := taskArtifactImportTable(t, packageData, "workflow_events")
				assignmentIndex := columnIndex(events.Columns, "assignment_id")
				actionIndex := columnIndex(events.Columns, "action")
				createdIndex := columnIndex(events.Columns, "created_at")
				previousIndex := columnIndex(events.Columns, "previous_json")
				currentIndex := columnIndex(events.Columns, "current_json")
				for _, row := range events.Rows {
					if row[assignmentIndex] != assignmentID {
						continue
					}
					if row[actionIndex] == "assignment_created" {
						row[createdIndex] = assignedAt
					}
					if row[previousIndex] != nil {
						previous := taskArtifactImportJSONObject(t, row[previousIndex])
						previous["assigned_at"] = assignedAt
						row[previousIndex] = taskArtifactImportJSON(t, previous)
					}
					current := taskArtifactImportJSONObject(t, row[currentIndex])
					current["assigned_at"] = assignedAt
					row[currentIndex] = taskArtifactImportJSON(t, current)
				}
			},
		},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
			mutation.mutate(t, &packageData)
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func TestBusinessJSONImportRejectsAssignmentActorSnapshotMismatchWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixture(t)
	baseline := decodeBusinessImportJSON(t, fixture.jsonBody)
	assignmentID := taskArtifactImportAssignmentID(t, &baseline, fixture.taskID, "assignee")
	eventID := taskArtifactImportAssignmentEventID(t, &baseline, assignmentID, "assignment_ended")
	actorTable, actorRow := taskArtifactImportRow(t, &baseline, "actors", "id", fixture.producerID)
	actorVersion := int64(actorRow[columnIndex(actorTable.Columns, "version")].(float64))
	mutations := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "actor type differs from immutable Actor",
			mutate: func(actor map[string]any) {
				actor["type"] = "agent"
				actor["is_builtin"] = false
			},
		},
		{
			name: "actor builtin identity differs from immutable Actor",
			mutate: func(actor map[string]any) {
				actor["type"] = "owner"
				actor["is_builtin"] = true
			},
		},
		{
			name: "actor snapshot version is newer than final Actor",
			mutate: func(actor map[string]any) {
				actor["version"] = actorVersion + 1
			},
		},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
			mutateTaskArtifactAssignmentEventActor(t, &packageData, eventID, mutation.mutate)
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func TestTaskArtifactMigrationAssignmentImportValidation(t *testing.T) {
	const taskID = "018f0000-0000-7000-8000-00000000f724"
	assignmentID, assignmentOK := taskArtifactMigrationDerivedID(taskID, '5')
	eventID, eventOK := taskArtifactMigrationDerivedID(taskID, '6')
	if !assignmentOK || !eventOK || assignmentID != "018f0000-0000-5000-8000-00000000f724" ||
		eventID != "018f0000-0000-6000-8000-00000000f724" {
		t.Fatalf("migration derived IDs assignment=%q/%t event=%q/%t", assignmentID, assignmentOK, eventID, eventOK)
	}

	t.Run("sentinel direct historical row", func(t *testing.T) {
		if !validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{}) {
			t.Fatal("canonical migration sentinel history was rejected")
		}
	})
	t.Run("normal ended reason preserves inferred origin", func(t *testing.T) {
		if !validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{
			reason:    "Task review accepted",
			withEnded: true,
		}) {
			t.Fatal("normally ended inferred assignment lost its migration origin")
		}
	})
	t.Run("tampered derived assignment ID", func(t *testing.T) {
		if validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{
			assignmentID: taskID,
		}) {
			t.Fatal("tampered migration assignment ID was accepted")
		}
	})
	t.Run("tampered derived event ID", func(t *testing.T) {
		if validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{
			eventID: taskID,
		}) {
			t.Fatal("tampered migration event ID was accepted")
		}
	})
	t.Run("legacy SQLite timestamp", func(t *testing.T) {
		if !validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{
			eventTime: "2026-09-06 07:08:09",
		}) {
			t.Fatal("canonical legacy SQLite migration timestamp was rejected")
		}
	})
	t.Run("arbitrary migration timestamp", func(t *testing.T) {
		if validTaskArtifactMigrationValidationFixture(t, taskArtifactMigrationValidationOptions{
			eventTime: "migration happened sometime",
		}) {
			t.Fatal("arbitrary migration timestamp was accepted")
		}
	})
}

func TestBusinessJSONImportRejectsNonStringReassignedActorIDWithoutSideEffects(t *testing.T) {
	fixture := taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{
		reassignAssignee: true,
	})
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "number", value: 7},
		{name: "object", value: map[string]any{"id": fixture.producerID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
			table := taskArtifactImportTable(t, &packageData, "workflow_events")
			actionIndex := columnIndex(table.Columns, "action")
			currentIndex := columnIndex(table.Columns, "current_json")
			mutated := false
			for _, row := range table.Rows {
				if row[actionIndex] != "assignment_reassigned" {
					continue
				}
				current := taskArtifactImportJSONObject(t, row[currentIndex])
				assignment, ok := current["assignment"].(map[string]any)
				if !ok {
					t.Fatalf("reassignment event has no created assignment: %#v", current)
				}
				assignment["actor_id"] = test.value
				row[currentIndex] = taskArtifactImportJSON(t, current)
				mutated = true
				break
			}
			if !mutated {
				t.Fatal("source package has no assignment_reassigned event")
			}
			assertTaskArtifactImportRejectedWithoutSideEffects(
				t, encodeBusinessImportJSON(t, packageData),
				"/api/v1/imports/business-data/preview", "/api/v1/imports/business-data",
				importAppendConfirmation, fixture,
			)
		})
	}
}

func legacyTaskArtifactPackageWithoutInboxProjection(
	t *testing.T,
	fixture taskArtifactTombstoneImportFixture,
) businessExportPackage {
	t.Helper()
	packageData := decodeBusinessImportJSON(t, fixture.jsonBody)
	removeTaskArtifactImportRow(t, &packageData, "inbox_items", "id", fixture.inboxID)
	events := taskArtifactImportTable(t, &packageData, "workflow_events")
	aggregateTypeIndex := columnIndex(events.Columns, "aggregate_type")
	aggregateIDIndex := columnIndex(events.Columns, "aggregate_id")
	kept := events.Rows[:0]
	removedEvents := 0
	for _, row := range events.Rows {
		if row[aggregateTypeIndex] == "inbox_item" && row[aggregateIDIndex] == fixture.inboxID {
			removedEvents++
			continue
		}
		kept = append(kept, row)
	}
	if removedEvents < 2 {
		t.Fatalf("legacy mutation removed Inbox events=%d, want source projection/deletion history", removedEvents)
	}
	events.Rows = kept
	markerID, markerIDOK := taskArtifactInboxGapMigrationEventID(fixture.artifactID)
	if !markerIDOK {
		t.Fatalf("derive legacy Inbox gap marker ID for Artifact %s", fixture.artifactID)
	}
	marker := make([]any, len(events.Columns))
	marker[columnIndex(events.Columns, "id")] = markerID
	marker[columnIndex(events.Columns, "aggregate_type")] = "task"
	marker[columnIndex(events.Columns, "aggregate_id")] = fixture.taskID
	marker[columnIndex(events.Columns, "action")] = taskArtifactInboxGapMigrationAction
	marker[columnIndex(events.Columns, "actor_id")] = models.BuiltinOwnerActorID
	marker[columnIndex(events.Columns, "submission_id")] = fixture.submissionID
	marker[columnIndex(events.Columns, "artifact_id")] = fixture.artifactID
	marker[columnIndex(events.Columns, "current_json")] = taskArtifactImportJSON(t, map[string]any{
		"source": "schema_v51_migration", "artifact_id": fixture.artifactID,
		"task_id": fixture.taskID, "submission_id": fixture.submissionID,
		"artifact_created_at": fixture.artifact.CreatedAt, "requires_followup": int64(1),
	})
	marker[columnIndex(events.Columns, "created_at")] = fixture.artifact.CreatedAt
	events.Rows = append(events.Rows, marker)
	return packageData
}

func taskArtifactTombstoneFixture(t *testing.T) taskArtifactTombstoneImportFixture {
	return taskArtifactTombstoneFixtureWithOptions(t, taskArtifactTombstoneFixtureOptions{})
}

func taskArtifactTombstoneFixtureWithOptions(
	t *testing.T,
	options taskArtifactTombstoneFixtureOptions,
) taskArtifactTombstoneImportFixture {
	t.Helper()
	router, store, _, _ := newBackupTestAPI(t)
	var task models.Task
	var producer actorResponse
	var project projectResponse
	if options.changeTaskAndProjectFacts {
		project = createProjectForTest(t, router, `{"name":"Portable source Project"}`, nil)
		task = createTaskForTaskFacts(t, router, fmt.Sprintf(
			`{"title":"Portable source Task","project_id":%q,"priority":"P2","review_policy":"manual"}`,
			project.ID,
		))
		producer = createActorForTest(t, router, `{"type":"person","display_name":"Offline producer"}`, nil)
		createAssignmentForTest(t, router, task.ID, "assignee", producer.ID, 1, "")
		createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 2, "")
		task.Version = 3
	} else {
		task, producer = setupManualReviewTask(t, router)
	}
	if options.reassignAssignee {
		nextProducer := createActorForTest(t, router, `{"type":"person","display_name":"Replacement producer"}`, nil)
		reassigned := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reassign",
			[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"F24 producer handoff"}`, nextProducer.ID)),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version), "Idempotency-Key": "f24-reassign-assignee"},
		)
		if reassigned.Code != http.StatusOK {
			t.Fatalf("reassign source assignee = %d: %s", reassigned.Code, reassigned.Body.String())
		}
		task = decodeReassignMutation(t, reassigned.Body.Bytes()).Task
		producer = nextProducer
	}
	submitted := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output",
		[]byte(`{"summary":"Portable follow-up delivery","artifacts":[{"client_ref":"portable-text","storage_kind":"text","name":"Portable text result","content_text":"portable private text","requires_followup":true}]}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version), "Idempotency-Key": "f24-submit-text-followup"},
	)
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit text follow-up = %d: %s", submitted.Code, submitted.Body.String())
	}
	output := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	if len(output.Artifacts) != 1 {
		t.Fatalf("submitted Artifacts=%#v", output.Artifacts)
	}
	artifactID := output.Artifacts[0].ID
	currentTask := output.Task
	if options.changeTaskAndProjectFacts {
		updatedTask := performRequest(
			router, http.MethodPatch, "/api/v1/tasks/"+task.ID,
			[]byte(`{"title":"Renamed portable Task","priority":"P0"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentTask.Version)},
		)
		if updatedTask.Code != http.StatusOK {
			t.Fatalf("change Task facts after submit = %d: %s", updatedTask.Code, updatedTask.Body.String())
		}
		currentTask = getTaskForTaskFacts(t, router, task.ID)
		currentProject := performRequest(router, http.MethodGet, "/api/v1/projects/"+project.ID, nil, nil)
		if currentProject.Code != http.StatusOK {
			t.Fatalf("get Project before name change = %d: %s", currentProject.Code, currentProject.Body.String())
		}
		project = decodeProjectResponse(t, currentProject.Body.Bytes())
		updatedProject := performRequest(
			router, http.MethodPatch, "/api/v1/projects/"+project.ID,
			[]byte(`{"name":"Renamed portable Project"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, project.Version)},
		)
		if updatedProject.Code != http.StatusOK {
			t.Fatalf("change Project name after submit = %d: %s", updatedProject.Code, updatedProject.Body.String())
		}
		currentTask = getTaskForTaskFacts(t, router, task.ID)
	}
	if options.requestChanges && options.cancelAfterSubmit {
		t.Fatal("requestChanges and cancelAfterSubmit are mutually exclusive")
	}
	dispositionTask := currentTask
	if options.cancelAfterSubmit {
		cancelled := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/cancel",
			[]byte(`{"reason":"Withdraw portable follow-up delivery"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentTask.Version), "Idempotency-Key": "f24-cancel-pending-output"},
		)
		if cancelled.Code != http.StatusOK {
			t.Fatalf("cancel pending text follow-up = %d: %s", cancelled.Code, cancelled.Body.String())
		}
		cancelledOutput := decodeTaskLifecycleResponse(t, cancelled.Body.Bytes())
		dispositionTask = cancelledOutput.Task
		if dispositionTask.Status != "cancelled" || dispositionTask.CurrentSubmissionID == nil ||
			*dispositionTask.CurrentSubmissionID != output.Submission.ID || cancelledOutput.Event.Action != "task_cancelled" {
			t.Fatalf("cancelled source Task=%#v event=%#v", dispositionTask, cancelledOutput.Event)
		}
	} else {
		reviewBody := []byte(`{"decision":"accept"}`)
		reviewAction := "accept"
		if options.requestChanges {
			reviewBody = []byte(`{"decision":"request_changes","reason":"Revise portable follow-up delivery"}`)
			reviewAction = "request changes for"
		}
		reviewed := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review",
			reviewBody, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, currentTask.Version)},
		)
		if reviewed.Code != http.StatusOK {
			t.Fatalf("%s text follow-up = %d: %s", reviewAction, reviewed.Code, reviewed.Body.String())
		}
		reviewedOutput := decodeReviewOutputResponse(t, reviewed.Body.Bytes())
		dispositionTask = reviewedOutput.Task
		if options.requestChanges {
			if reviewedOutput.Task.Status != "in_progress" || reviewedOutput.Submission.Status != "changes_requested" ||
				reviewedOutput.Submission.ReviewReason == nil || *reviewedOutput.Submission.ReviewReason != "Revise portable follow-up delivery" {
				t.Fatalf("changes-requested source review=%#v", reviewedOutput)
			}
		} else if reviewedOutput.Task.Status != "done" || reviewedOutput.Submission.Status != "accepted" {
			t.Fatalf("accepted source review=%#v", reviewedOutput)
		}
	}
	var assignments []models.TaskAssignment
	wantAssignments := 2
	if options.reassignAssignee {
		wantAssignments = 3
	}
	if err := store.DB.Where("task_id = ?", task.ID).Order("role ASC, assigned_at ASC, id ASC").Find(&assignments).Error; err != nil || len(assignments) != wantAssignments {
		t.Fatalf("closed source assignments=%#v err=%v", assignments, err)
	}
	if options.requestChanges {
		for _, assignment := range assignments {
			if assignment.UnassignedAt != nil || assignment.Reason != "" {
				t.Fatalf("request_changes changed assignment lifecycle: %#v", assignment)
			}
		}
	} else {
		endedAssignments := 0
		reassignedAssignments := 0
		endReason := "Task review accepted"
		firstCommandSeq := 1
		if options.cancelAfterSubmit {
			endReason = "Task cancelled"
			firstCommandSeq = 2
		}
		for _, assignment := range assignments {
			if assignment.UnassignedAt == nil {
				t.Fatalf("source assignment remained active after disposition: %#v", assignment)
			}
			if assignment.Reason == "F24 producer handoff" {
				reassignedAssignments++
				continue
			}
			if assignment.Reason != endReason {
				t.Fatalf("unexpected source assignment history: %#v", assignment)
			}
			var event models.WorkflowEvent
			if err := store.DB.First(
				&event,
				"aggregate_type = 'task' AND aggregate_id = ? AND action = 'assignment_ended' AND assignment_id = ?",
				task.ID, assignment.ID,
			).Error; err != nil || event.CommandSeq == nil || *event.CommandSeq != endedAssignments+firstCommandSeq ||
				event.CreatedAt != *assignment.UnassignedAt {
				t.Fatalf("closed %s assignment event=%#v err=%v", assignment.Role, event, err)
			}
			endedAssignments++
		}
		if endedAssignments != 2 || reassignedAssignments != map[bool]int{false: 0, true: 1}[options.reassignAssignee] {
			t.Fatalf("assignment history ended=%d reassigned=%d rows=%#v", endedAssignments, reassignedAssignments, assignments)
		}
	}

	var inbox models.InboxItem
	if err := store.DB.First(&inbox, "source_entity_type = 'task_artifact' AND source_entity_id = ?", artifactID).Error; err != nil {
		t.Fatalf("load Artifact follow-up Inbox Item: %v", err)
	}
	dismissed := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+inbox.ID+"/dismiss",
		[]byte(`{"reason":"delivery reviewed"}`), map[string]string{"If-Match": `"1"`},
	)
	if dismissed.Code != http.StatusOK {
		t.Fatalf("dismiss Artifact follow-up = %d: %s", dismissed.Code, dismissed.Body.String())
	}
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/artifacts/"+artifactID+"?confirm=true",
		[]byte(`{"reason":"superseded text result"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, dispositionTask.Version), "Idempotency-Key": "f24-delete-text-artifact"},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("soft-delete text Artifact = %d: %s", deleted.Code, deleted.Body.String())
	}
	if options.changeInboxAfterDelete {
		read := performRequest(
			router, http.MethodPost, "/api/v1/inbox-items/"+inbox.ID+"/read", []byte(`{}`),
			map[string]string{"If-Match": `"3"`, "Idempotency-Key": "f24-read-deleted-artifact-inbox"},
		)
		if read.Code != http.StatusOK || read.Header().Get("ETag") != `"4"` {
			t.Fatalf("read deleted Artifact Inbox = %d headers=%v: %s", read.Code, read.Header(), read.Body.String())
		}
		readInbox := decodeInboxItemData(t, read.Body.Bytes())
		if readInbox.ReadAt == nil || readInbox.Status != "dismissed" || readInbox.SourceDeletedAt == nil {
			t.Fatalf("read deleted Artifact Inbox=%#v", readInbox)
		}
		reopened := performRequest(
			router, http.MethodPost, "/api/v1/inbox-items/"+inbox.ID+"/reopen", []byte(`{}`),
			map[string]string{"If-Match": `"4"`, "Idempotency-Key": "f24-reopen-deleted-artifact-inbox"},
		)
		if reopened.Code != http.StatusOK || reopened.Header().Get("ETag") != `"5"` {
			t.Fatalf("reopen deleted Artifact Inbox = %d headers=%v: %s", reopened.Code, reopened.Header(), reopened.Body.String())
		}
		reopenedInbox := decodeInboxItemData(t, reopened.Body.Bytes())
		if reopenedInbox.Status != "open" || reopenedInbox.ReadAt == nil || reopenedInbox.SourceDeletedAt == nil ||
			reopenedInbox.DismissedAt != nil || reopenedInbox.DismissReason != nil {
			t.Fatalf("reopened deleted Artifact Inbox=%#v", reopenedInbox)
		}
	}
	deletedTask := getTaskForTaskFacts(t, router, task.ID)
	reopenedTask := deletedTask
	if options.reopenAfterDelete {
		reopened := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reopen",
			[]byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, deletedTask.Version)},
		)
		if reopened.Code != http.StatusOK {
			t.Fatalf("reopen Task after Artifact delete = %d: %s", reopened.Code, reopened.Body.String())
		}
		reopenedTask = decodeTaskLifecycleResponse(t, reopened.Body.Bytes()).Task
		if reopenedTask.Status != "todo" || reopenedTask.Version != deletedTask.Version+1 {
			t.Fatalf("reopened Task=%#v", reopenedTask)
		}
	}
	if options.cancelAndReopenAgain {
		if !options.reopenAfterDelete {
			t.Fatal("second reopen fixture requires reopenAfterDelete")
		}
		cancelled := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/cancel",
			[]byte(`{"reason":"F24 terminal predecessor ordering"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, reopenedTask.Version), "Idempotency-Key": "f24-cancel-between-reopens"},
		)
		if cancelled.Code != http.StatusOK {
			t.Fatalf("cancel between Task reopens = %d: %s", cancelled.Code, cancelled.Body.String())
		}
		cancelledTask := decodeTaskLifecycleResponse(t, cancelled.Body.Bytes()).Task
		if cancelledTask.Status != "cancelled" || cancelledTask.Version != reopenedTask.Version+1 {
			t.Fatalf("cancelled between reopens Task=%#v", cancelledTask)
		}
		reopenedAgain := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reopen",
			[]byte(`{}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, cancelledTask.Version), "Idempotency-Key": "f24-second-reopen"},
		)
		if reopenedAgain.Code != http.StatusOK {
			t.Fatalf("second Task reopen = %d: %s", reopenedAgain.Code, reopenedAgain.Body.String())
		}
		reopenedTask = decodeTaskLifecycleResponse(t, reopenedAgain.Body.Bytes()).Task
		if reopenedTask.Status != "todo" || reopenedTask.Version != cancelledTask.Version+1 {
			t.Fatalf("second reopened Task=%#v", reopenedTask)
		}
	}
	if (options.assignAfterReopen || options.reassignAfterReopen) && !options.reopenAfterDelete {
		t.Fatal("post-reopen assignment fixture requires reopenAfterDelete")
	}
	if options.assignAfterReopen {
		created := createAssignmentForTest(
			t, router, task.ID, "assignee", producer.ID, reopenedTask.Version, "f24-reopen-assignee",
		)
		reopenedTask = created.Task
	}
	if options.reassignAfterReopen {
		if !options.assignAfterReopen {
			t.Fatal("post-reopen reassignment fixture requires assignAfterReopen")
		}
		nextProducer := createActorForTest(t, router, `{"type":"person","display_name":"Reopened replacement producer"}`, nil)
		reassigned := performRequest(
			router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/reassign",
			[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"F24 reopened handoff"}`, nextProducer.ID)),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, reopenedTask.Version), "Idempotency-Key": "f24-reopen-reassign"},
		)
		if reassigned.Code != http.StatusOK {
			t.Fatalf("reassign reopened source assignee = %d: %s", reassigned.Code, reassigned.Body.String())
		}
		reopenedTask = decodeReassignMutation(t, reassigned.Body.Bytes()).Task
	}
	if options.deactivateProducer {
		deactivated := performRequest(
			router, http.MethodPatch, "/api/v1/actors/"+producer.ID,
			[]byte(`{"status":"inactive"}`),
			map[string]string{"If-Match": fmt.Sprintf(`"%d"`, producer.Version)},
		)
		if deactivated.Code != http.StatusOK {
			t.Fatalf("deactivate producer after assignment close = %d: %s", deactivated.Code, deactivated.Body.String())
		}
		deactivatedActor := decodeActorResponse(t, deactivated.Body.Bytes())
		if deactivatedActor.Status != "inactive" || deactivatedActor.Version != producer.Version+1 {
			t.Fatalf("deactivated producer=%#v", deactivatedActor)
		}
	}

	var artifact models.TaskArtifact
	if err := store.DB.First(&artifact, "id = ?", artifactID).Error; err != nil || artifact.DeletedAt == nil ||
		artifact.DeletedByActorID == nil || *artifact.DeletedByActorID != models.BuiltinOwnerActorID ||
		artifact.DeleteReason == nil || *artifact.DeleteReason != "superseded text result" {
		t.Fatalf("soft-deleted source Artifact=%#v err=%v", artifact, err)
	}
	if err := store.DB.First(&inbox, "id = ?", inbox.ID).Error; err != nil || inbox.SourceDeletedAt == nil {
		t.Fatalf("source Inbox tombstone=%#v err=%v", inbox, err)
	}
	if options.changeInboxAfterDelete {
		if inbox.Status != "open" || inbox.ReadAt == nil || inbox.Version != 5 || inbox.DismissedAt != nil || inbox.DismissReason != nil {
			t.Fatalf("source changed Inbox tombstone=%#v", inbox)
		}
	} else if inbox.Status != "dismissed" || inbox.Version != 3 {
		t.Fatalf("source dismissed Inbox tombstone=%#v", inbox)
	}
	var artifactDeleted models.WorkflowEvent
	if err := store.DB.First(
		&artifactDeleted,
		"aggregate_type = 'task' AND aggregate_id = ? AND action = 'task_artifact_deleted' AND artifact_id = ?",
		task.ID, artifactID,
	).Error; err != nil {
		t.Fatalf("load task_artifact_deleted event: %v", err)
	}
	var submittedEvent models.WorkflowEvent
	if err := store.DB.First(
		&submittedEvent,
		"aggregate_type = 'task' AND aggregate_id = ? AND action = 'task_output_submitted' AND submission_id = ?",
		task.ID, output.Submission.ID,
	).Error; err != nil {
		t.Fatalf("load task_output_submitted event: %v", err)
	}
	for _, action := range []string{"source_projected", "source_deleted"} {
		var count int64
		if err := store.DB.Model(&models.WorkflowEvent{}).
			Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = ?", inbox.ID, action).
			Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s events=%d err=%v", action, count, err)
		}
	}

	jsonExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if jsonExport.Code != http.StatusOK {
		t.Fatalf("export text Artifact JSON = %d: %s", jsonExport.Code, jsonExport.Body.String())
	}
	zipExport := performRequest(router, http.MethodGet, "/api/v1/exports/business-package", nil, nil)
	if zipExport.Code != http.StatusOK {
		t.Fatalf("export text Artifact package = %d: %s", zipExport.Code, zipExport.Body.String())
	}
	finalTask := getTaskForTaskFacts(t, router, task.ID)
	var finalSubmission models.TaskSubmission
	if err := store.DB.First(&finalSubmission, "id = ?", output.Submission.ID).Error; err != nil {
		t.Fatalf("load final source submission: %v", err)
	}
	if err := store.DB.Where("task_id = ?", task.ID).Order("role ASC, assigned_at ASC, id ASC").Find(&assignments).Error; err != nil {
		t.Fatalf("load final source assignment history: %v", err)
	}
	var assignmentEvents []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = 'task' AND aggregate_id = ? AND assignment_id IS NOT NULL", task.ID).
		Order("id ASC").Find(&assignmentEvents).Error; err != nil {
		t.Fatalf("load source assignment events: %v", err)
	}
	var taskEvents []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = 'task' AND aggregate_id = ?", task.ID).
		Order("id ASC").Find(&taskEvents).Error; err != nil {
		t.Fatalf("load source Task events: %v", err)
	}
	return taskArtifactTombstoneImportFixture{
		jsonBody:             append([]byte(nil), jsonExport.Body.Bytes()...),
		zipBody:              append([]byte(nil), zipExport.Body.Bytes()...),
		taskID:               task.ID,
		submissionID:         output.Submission.ID,
		artifactID:           artifactID,
		inboxID:              inbox.ID,
		artifactDeletedEvent: artifactDeleted.ID,
		submittedEventID:     submittedEvent.ID,
		producerID:           producer.ID,
		projectID:            project.ID,
		task:                 finalTask,
		submission:           finalSubmission,
		artifact:             artifact,
		inbox:                inbox,
		assignments:          assignments,
		assignmentEvents:     assignmentEvents,
		taskEvents:           taskEvents,
	}
}

func assertTaskArtifactTombstoneImported(
	t *testing.T,
	store *database.Store,
	fixture taskArtifactTombstoneImportFixture,
	appendTarget bool,
) {
	t.Helper()
	var artifact models.TaskArtifact
	if err := store.DB.First(&artifact, "id = ?", fixture.artifactID).Error; err != nil ||
		artifact.TaskID != fixture.taskID || artifact.SubmissionID != fixture.submissionID ||
		artifact.StorageKind != "text" || artifact.ContentText == nil || *artifact.ContentText != "portable private text" ||
		artifact.DeletedAt == nil || fixture.artifact.DeletedAt == nil || *artifact.DeletedAt != *fixture.artifact.DeletedAt ||
		artifact.DeletedByActorID == nil || *artifact.DeletedByActorID != models.BuiltinOwnerActorID ||
		artifact.DeleteReason == nil || *artifact.DeleteReason != "superseded text result" {
		t.Fatalf("imported soft-deleted Artifact=%#v err=%v", artifact, err)
	}
	var inbox models.InboxItem
	if err := store.DB.First(&inbox, "id = ?", fixture.inboxID).Error; err != nil ||
		inbox.SourceEntityType != taskArtifactInboxSourceType || inbox.SourceEntityID == nil || *inbox.SourceEntityID != fixture.artifactID ||
		inbox.SourceDeletedAt == nil || fixture.inbox.SourceDeletedAt == nil || *inbox.SourceDeletedAt != *fixture.inbox.SourceDeletedAt ||
		!reflect.DeepEqual(inbox, fixture.inbox) {
		t.Fatalf("imported Artifact Inbox tombstone=%#v err=%v", inbox, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE id = ? AND action = 'task_artifact_deleted'", 1, fixture.artifactDeletedEvent)
	reviewEndedAssignments := 0
	for _, assignment := range fixture.assignments {
		if assignment.UnassignedAt != nil && assignment.Reason == "Task review accepted" {
			reviewEndedAssignments++
		}
	}
	endedEvents := 0
	for _, event := range fixture.assignmentEvents {
		if event.Action == "assignment_ended" {
			endedEvents++
		}
	}
	assertDatabaseCount(t, store,
		"SELECT COUNT(*) FROM task_assignments WHERE task_id = ? AND unassigned_at IS NOT NULL AND reason = 'Task review accepted'",
		int64(reviewEndedAssignments), fixture.taskID,
	)
	assertDatabaseCount(t, store,
		"SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'task' AND aggregate_id = ? AND action = 'assignment_ended'",
		int64(endedEvents), fixture.taskID,
	)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_projected'", 1, fixture.inboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_deleted'", 1, fixture.inboxID)
	if appendTarget {
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = ? AND name = ?", 1, taskArtifactImportSentinelClientID, taskArtifactImportSentinelClientName)
	}
}

func importTaskArtifactTombstoneJSONReplace(
	t *testing.T,
	fixture taskArtifactTombstoneImportFixture,
) *database.Store {
	return importTaskArtifactTombstoneJSON(t, fixture, false)
}

func importTaskArtifactTombstoneJSON(
	t *testing.T,
	fixture taskArtifactTombstoneImportFixture,
	appendTarget bool,
) *database.Store {
	t.Helper()
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	wantMode := importModeReplaceEmpty
	confirmation := importReplaceConfirmation
	if appendTarget {
		seedTaskArtifactImportTarget(t, store, artifactDir)
		wantMode = importModeAppend
		confirmation = importAppendConfirmation
	}
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", fixture.jsonBody, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("Task Artifact JSON preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil ||
		!previewEnvelope.Data.CanApply || previewEnvelope.Data.ApplyMode != wantMode || previewEnvelope.Data.Blocker != "" {
		t.Fatalf("Task Artifact JSON preview=%#v err=%v", previewEnvelope.Data, err)
	}
	apply := performRequest(
		router, http.MethodPost, "/api/v1/imports/business-data", fixture.jsonBody,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusOK {
		t.Fatalf("Task Artifact JSON %s apply = %d: %s", wantMode, apply.Code, apply.Body.String())
	}
	assertTaskArtifactTombstoneImported(t, store, fixture, appendTarget)
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("Task Artifact JSON %s backups=%v", wantMode, backups)
	}
	return store
}

func assertTaskArtifactAssignmentHistoryImported(
	t *testing.T,
	store *database.Store,
	fixture taskArtifactTombstoneImportFixture,
) {
	t.Helper()
	var assignments []models.TaskAssignment
	if err := store.DB.Where("task_id = ?", fixture.taskID).
		Order("role ASC, assigned_at ASC, id ASC").Find(&assignments).Error; err != nil {
		t.Fatalf("load imported assignment history: %v", err)
	}
	if !reflect.DeepEqual(assignments, fixture.assignments) {
		t.Fatalf("imported assignment history=%#v, want %#v", assignments, fixture.assignments)
	}
	var events []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = 'task' AND aggregate_id = ? AND assignment_id IS NOT NULL", fixture.taskID).
		Order("id ASC").Find(&events).Error; err != nil {
		t.Fatalf("load imported assignment events: %v", err)
	}
	if !reflect.DeepEqual(events, fixture.assignmentEvents) {
		t.Fatalf("imported assignment events=%#v, want %#v", events, fixture.assignmentEvents)
	}
}

func assertTaskArtifactAssignmentActionCount(
	t *testing.T,
	events []models.WorkflowEvent,
	action string,
	want int,
) {
	t.Helper()
	count := 0
	for _, event := range events {
		if event.Action == action {
			count++
		}
	}
	if count != want {
		t.Fatalf("%s events=%d, want %d: %#v", action, count, want, events)
	}
}

const (
	taskArtifactImportSentinelClientID   = "018f0000-0000-7000-8000-000000009924"
	taskArtifactImportSentinelClientName = "Retained F24 client"
	taskArtifactImportMarkerName         = "f24-import-marker"
)

func seedTaskArtifactImportTarget(t *testing.T, store *database.Store, artifactDir string) {
	t.Helper()
	if err := store.DB.Exec(
		"INSERT INTO clients(id, name, status, created_at, updated_at) VALUES (?, ?, 'active', '2026-09-05T08:00:00Z', '2026-09-05T08:00:00Z')",
		taskArtifactImportSentinelClientID, taskArtifactImportSentinelClientName,
	).Error; err != nil {
		t.Fatalf("seed F24 target client: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, taskArtifactImportMarkerName), []byte("retained F24 target file"), 0o600); err != nil {
		t.Fatalf("seed F24 target marker: %v", err)
	}
}

func assertTaskArtifactImportRejectedWithoutSideEffects(
	t *testing.T,
	body []byte,
	previewPath, applyPath, confirmation string,
	fixture taskArtifactTombstoneImportFixture,
) {
	t.Helper()
	router, store, artifactDir, backupDir := newBackupTestAPI(t)
	seedTaskArtifactImportTarget(t, store, artifactDir)
	preview := performRequest(router, http.MethodPost, previewPath, body, nil)
	if preview.Code != http.StatusUnprocessableEntity || responseErrorCode(t, preview.Body.Bytes()) != "IMPORT_ROW_INVALID" {
		t.Fatalf("invalid task Artifact tombstone preview = %d: %s", preview.Code, preview.Body.String())
	}
	apply := performRequest(
		router, http.MethodPost, applyPath, body,
		map[string]string{"X-Import-Confirmation": confirmation},
	)
	if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_ROW_INVALID" {
		t.Fatalf("invalid task Artifact tombstone apply = %d: %s", apply.Code, apply.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE id = ? AND name = ?", 1, taskArtifactImportSentinelClientID, taskArtifactImportSentinelClientName)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id = ?", 0, fixture.taskID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts WHERE id = ?", 0, fixture.artifactID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ?", 0, fixture.inboxID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE id = ?", 0, fixture.artifactDeletedEvent)
	marker, err := os.ReadFile(filepath.Join(artifactDir, taskArtifactImportMarkerName))
	if err != nil || string(marker) != "retained F24 target file" {
		t.Fatalf("rejected import changed target marker body=%q err=%v", marker, err)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("rejected import created rollback backups=%v", backups)
	}
	if staging := importPackageStagingFiles(t, backupDir); len(staging) != 0 {
		t.Fatalf("rejected import retained staging=%v", staging)
	}
}

func taskArtifactImportRow(
	t *testing.T,
	packageData *businessExportPackage,
	tableName, keyColumn string,
	keyValue any,
) (*businessExportTable, []any) {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, tableName)
	keyIndex := columnIndex(table.Columns, keyColumn)
	if keyIndex < 0 {
		t.Fatalf("table %s has no column %s", tableName, keyColumn)
	}
	for _, row := range table.Rows {
		if row[keyIndex] == keyValue {
			return table, row
		}
	}
	t.Fatalf("table %s has no %s=%v", tableName, keyColumn, keyValue)
	return nil, nil
}

func taskArtifactImportTable(t *testing.T, packageData *businessExportPackage, tableName string) *businessExportTable {
	t.Helper()
	for tableIndex := range packageData.Tables {
		if packageData.Tables[tableIndex].Name == tableName {
			return &packageData.Tables[tableIndex]
		}
	}
	t.Fatalf("package has no table %s", tableName)
	return nil
}

func removeTaskArtifactImportRow(
	t *testing.T,
	packageData *businessExportPackage,
	tableName, keyColumn string,
	keyValue any,
) {
	t.Helper()
	table, _ := taskArtifactImportRow(t, packageData, tableName, keyColumn, keyValue)
	keyIndex := columnIndex(table.Columns, keyColumn)
	for index := range table.Rows {
		if table.Rows[index][keyIndex] == keyValue {
			table.Rows = append(table.Rows[:index], table.Rows[index+1:]...)
			return
		}
	}
	t.Fatalf("failed to remove %s row %s=%v", tableName, keyColumn, keyValue)
}

func removeTaskArtifactImportRows(
	t *testing.T,
	packageData *businessExportPackage,
	tableName, keyColumn string,
	keyValue any,
) {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, tableName)
	keyIndex := columnIndex(table.Columns, keyColumn)
	if keyIndex < 0 {
		t.Fatalf("table %s has no column %s", tableName, keyColumn)
	}
	kept := table.Rows[:0]
	removed := 0
	for _, row := range table.Rows {
		if row[keyIndex] == keyValue {
			removed++
			continue
		}
		kept = append(kept, row)
	}
	if removed == 0 {
		t.Fatalf("table %s has no rows with %s=%v", tableName, keyColumn, keyValue)
	}
	table.Rows = kept
}

func taskArtifactImportAssignmentID(
	t *testing.T,
	packageData *businessExportPackage,
	taskID, role string,
) string {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, "task_assignments")
	idIndex := columnIndex(table.Columns, "id")
	taskIndex := columnIndex(table.Columns, "task_id")
	roleIndex := columnIndex(table.Columns, "role")
	for _, row := range table.Rows {
		if row[taskIndex] == taskID && row[roleIndex] == role {
			id, ok := row[idIndex].(string)
			if !ok || id == "" {
				t.Fatalf("%s assignment id is invalid: %v", role, row[idIndex])
			}
			return id
		}
	}
	t.Fatalf("Task %s has no %s assignment", taskID, role)
	return ""
}

func taskArtifactImportAssignmentEventID(
	t *testing.T,
	packageData *businessExportPackage,
	assignmentID, action string,
) string {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, "workflow_events")
	idIndex := columnIndex(table.Columns, "id")
	actionIndex := columnIndex(table.Columns, "action")
	assignmentIndex := columnIndex(table.Columns, "assignment_id")
	for _, row := range table.Rows {
		if row[actionIndex] == action && row[assignmentIndex] == assignmentID {
			id, ok := row[idIndex].(string)
			if !ok || id == "" {
				t.Fatalf("%s assignment event id is invalid: %v", action, row[idIndex])
			}
			return id
		}
	}
	t.Fatalf("assignment %s has no %s event", assignmentID, action)
	return ""
}

func taskArtifactImportTaskSubmissionEventID(
	t *testing.T,
	packageData *businessExportPackage,
	taskID, submissionID, action string,
) string {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, "workflow_events")
	idIndex := columnIndex(table.Columns, "id")
	aggregateTypeIndex := columnIndex(table.Columns, "aggregate_type")
	aggregateIDIndex := columnIndex(table.Columns, "aggregate_id")
	actionIndex := columnIndex(table.Columns, "action")
	submissionIndex := columnIndex(table.Columns, "submission_id")
	for _, row := range table.Rows {
		if row[aggregateTypeIndex] == "task" && row[aggregateIDIndex] == taskID &&
			row[actionIndex] == action && row[submissionIndex] == submissionID {
			id, ok := row[idIndex].(string)
			if !ok || id == "" {
				t.Fatalf("%s submission event id is invalid: %v", action, row[idIndex])
			}
			return id
		}
	}
	t.Fatalf("Task %s submission %s has no %s event", taskID, submissionID, action)
	return ""
}

func taskArtifactImportTaskEventID(
	t *testing.T,
	packageData *businessExportPackage,
	taskID, action string,
) string {
	t.Helper()
	table := taskArtifactImportTable(t, packageData, "workflow_events")
	idIndex := columnIndex(table.Columns, "id")
	aggregateTypeIndex := columnIndex(table.Columns, "aggregate_type")
	aggregateIDIndex := columnIndex(table.Columns, "aggregate_id")
	actionIndex := columnIndex(table.Columns, "action")
	matchedID := ""
	for _, row := range table.Rows {
		if row[aggregateTypeIndex] != "task" || row[aggregateIDIndex] != taskID || row[actionIndex] != action {
			continue
		}
		id, ok := row[idIndex].(string)
		if !ok || id == "" || matchedID != "" {
			t.Fatalf("Task %s has invalid or duplicate %s event", taskID, action)
		}
		matchedID = id
	}
	if matchedID == "" {
		t.Fatalf("Task %s has no %s event", taskID, action)
	}
	return matchedID
}

func appendTaskArtifactDuplicateAssignmentEndedEvent(
	t *testing.T,
	packageData *businessExportPackage,
	fixture taskArtifactTombstoneImportFixture,
) {
	t.Helper()
	assignmentID := ""
	unassignedAt := ""
	for _, assignment := range fixture.assignments {
		if assignment.Role == "assignee" && assignment.Reason == "F24 producer handoff" && assignment.UnassignedAt != nil {
			assignmentID = assignment.ID
			unassignedAt = *assignment.UnassignedAt
			break
		}
	}
	if assignmentID == "" {
		t.Fatalf("reassigned fixture has no ended handoff row: %#v", fixture.assignments)
	}
	table := taskArtifactImportTable(t, packageData, "workflow_events")
	actionIndex := columnIndex(table.Columns, "action")
	currentIndex := columnIndex(table.Columns, "current_json")
	previousIndex := columnIndex(table.Columns, "previous_json")
	for _, row := range table.Rows {
		if row[actionIndex] != "assignment_reassigned" {
			continue
		}
		current := taskArtifactImportJSONObject(t, row[currentIndex])
		ended, ok := current["ended_assignment"].(map[string]any)
		if !ok || ended["id"] != assignmentID {
			continue
		}
		duplicate := append([]any(nil), row...)
		duplicate[columnIndex(table.Columns, "id")] = "018f0000-0000-7000-8000-00000000f729"
		duplicate[actionIndex] = "assignment_ended"
		duplicate[columnIndex(table.Columns, "assignment_id")] = assignmentID
		duplicate[columnIndex(table.Columns, "request_id")] = "018f0000-0000-7000-8000-00000000f72a"
		duplicate[columnIndex(table.Columns, "command_seq")] = float64(1)
		duplicate[previousIndex] = row[previousIndex]
		duplicate[currentIndex] = taskArtifactImportJSON(t, ended)
		duplicate[columnIndex(table.Columns, "created_at")] = unassignedAt
		table.Rows = append(table.Rows, duplicate)
		return
	}
	t.Fatalf("fixture has no reassignment proof for old assignment %s", assignmentID)
}

func overlapTaskArtifactAssigneeIntervals(
	t *testing.T,
	packageData *businessExportPackage,
	fixture taskArtifactTombstoneImportFixture,
) {
	t.Helper()
	endedAssignmentID := ""
	activeAssignedAt := ""
	for _, assignment := range fixture.assignments {
		if assignment.Role != "assignee" {
			continue
		}
		if assignment.Reason == "Task review accepted" && assignment.UnassignedAt != nil {
			endedAssignmentID = assignment.ID
		}
		if assignment.UnassignedAt == nil {
			activeAssignedAt = assignment.AssignedAt
		}
	}
	activeTime, err := time.Parse(time.RFC3339Nano, activeAssignedAt)
	if endedAssignmentID == "" || err != nil {
		t.Fatalf("cannot identify assignee intervals ended=%q active_at=%q err=%v: %#v",
			endedAssignmentID, activeAssignedAt, err, fixture.assignments)
	}
	overlappingEnd := activeTime.Add(time.Nanosecond).Format(time.RFC3339Nano)
	assignmentTable, assignmentRow := taskArtifactImportRow(t, packageData, "task_assignments", "id", endedAssignmentID)
	assignmentRow[columnIndex(assignmentTable.Columns, "unassigned_at")] = overlappingEnd

	endedEventID := taskArtifactImportAssignmentEventID(t, packageData, endedAssignmentID, "assignment_ended")
	eventTable, eventRow := taskArtifactImportRow(t, packageData, "workflow_events", "id", endedEventID)
	eventRow[columnIndex(eventTable.Columns, "created_at")] = overlappingEnd
	previous, current := taskArtifactImportEventObjects(t, packageData, endedEventID)
	current["unassigned_at"] = overlappingEnd
	setTaskArtifactImportEventObjects(t, packageData, endedEventID, previous, current)
}

func mutateTaskArtifactAssignmentEventActor(
	t *testing.T,
	packageData *businessExportPackage,
	eventID string,
	mutate func(map[string]any),
) {
	t.Helper()
	previous, current := taskArtifactImportEventObjects(t, packageData, eventID)
	for _, state := range []map[string]any{previous, current} {
		actor, ok := state["actor"].(map[string]any)
		if !ok {
			t.Fatalf("assignment event %s has no actor snapshot: %#v", eventID, state)
		}
		mutate(actor)
	}
	setTaskArtifactImportEventObjects(t, packageData, eventID, previous, current)
}

type taskArtifactMigrationValidationOptions struct {
	assignmentID string
	eventID      string
	eventTime    string
	reason       string
	withEnded    bool
}

func validTaskArtifactMigrationValidationFixture(
	t *testing.T,
	options taskArtifactMigrationValidationOptions,
) bool {
	t.Helper()
	const (
		taskID     = "018f0000-0000-7000-8000-00000000f724"
		assignedAt = "2026-09-06T07:00:00Z"
	)
	unassignedAt := "2026-09-06T08:00:00Z"
	derivedAssignmentID, _ := taskArtifactMigrationDerivedID(taskID, '5')
	derivedEventID, _ := taskArtifactMigrationDerivedID(taskID, '6')
	assignmentID := options.assignmentID
	if assignmentID == "" {
		assignmentID = derivedAssignmentID
	}
	eventID := options.eventID
	if eventID == "" {
		eventID = derivedEventID
	}
	eventTime := options.eventTime
	if eventTime == "" {
		eventTime = assignedAt
	}
	reason := options.reason
	if reason == "" {
		reason = migrationAssignmentReason
	}

	assignmentTable := businessExportTable{
		Name: "task_assignments",
		Columns: []string{
			"id", "task_id", "actor_id", "role", "assigned_by_actor_id",
			"assigned_at", "unassigned_at", "reason",
		},
		Rows: [][]any{{
			assignmentID, taskID, models.BuiltinOwnerActorID, "assignee", models.BuiltinOwnerActorID,
			assignedAt, unassignedAt, reason,
		}},
	}
	workflowTable := businessExportTable{
		Name: "workflow_events",
		Columns: []string{
			"id", "aggregate_type", "aggregate_id", "action", "actor_id", "assignment_id",
			"submission_id", "artifact_id", "agent_run_id", "request_id", "command_seq",
			"previous_json", "current_json", "created_at",
		},
		Rows: [][]any{{
			eventID, "task", taskID, "migration_assignment_backfill", models.BuiltinOwnerActorID, assignmentID,
			nil, nil, nil, nil, nil, nil,
			`{"source":"schema_v7_migration","inferred":true,"role":"assignee"}`, eventTime,
		}},
	}
	if options.withEnded {
		actor := taskArtifactMigrationActorSnapshot()
		previous := taskArtifactMigrationAssignmentSnapshot(
			assignmentID, taskID, assignedAt, nil, nil, true, actor,
		)
		current := taskArtifactMigrationAssignmentSnapshot(
			assignmentID, taskID, assignedAt, &unassignedAt, &reason, false, actor,
		)
		workflowTable.Rows = append(workflowTable.Rows, []any{
			"018f0000-0000-7000-8000-00000000f725", "task", taskID, "assignment_ended",
			models.BuiltinOwnerActorID, assignmentID, nil, nil, nil,
			"018f0000-0000-7000-8000-00000000f726", int64(1),
			taskArtifactImportJSON(t, previous), taskArtifactImportJSON(t, current), unassignedAt,
		})
	}
	events, eventsOK := automationImportWorkflowEvents(workflowTable)
	assignments, assignmentsOK := automationImportRowsByID(assignmentTable)
	actors := map[string]map[string]any{
		models.BuiltinOwnerActorID: {
			"id": models.BuiltinOwnerActorID, "type": "owner", "display_name": "Owner", "status": "active",
			"is_builtin": int64(1), "version": int64(1),
		},
	}
	return eventsOK && assignmentsOK && validTaskArtifactHistoricalAssignmentImportRows(
		assignmentTable, assignments, events, actors,
	)
}

func taskArtifactMigrationActorSnapshot() map[string]any {
	return map[string]any{
		"id": models.BuiltinOwnerActorID, "type": "owner", "display_name": "Owner",
		"status": "active", "is_builtin": true, "version": int64(1),
	}
}

func taskArtifactMigrationAssignmentSnapshot(
	assignmentID, taskID, assignedAt string,
	unassignedAt, reason *string,
	active bool,
	actor map[string]any,
) map[string]any {
	return map[string]any{
		"id": assignmentID, "task_id": taskID, "role": "assignee",
		"actor_id": models.BuiltinOwnerActorID, "actor": actor,
		"assigned_by_actor_id": models.BuiltinOwnerActorID, "assigned_by_actor": actor,
		"assigned_at": assignedAt, "unassigned_at": unassignedAt, "reason": reason,
		"is_active": active, "inferred": true,
	}
}

func taskArtifactImportEventObjects(
	t *testing.T,
	packageData *businessExportPackage,
	eventID string,
) (map[string]any, map[string]any) {
	t.Helper()
	table, row := taskArtifactImportRow(t, packageData, "workflow_events", "id", eventID)
	previous := taskArtifactImportJSONObject(t, row[columnIndex(table.Columns, "previous_json")])
	current := taskArtifactImportJSONObject(t, row[columnIndex(table.Columns, "current_json")])
	return previous, current
}

func setTaskArtifactImportEventObjects(
	t *testing.T,
	packageData *businessExportPackage,
	eventID string,
	previous, current map[string]any,
) {
	t.Helper()
	table, row := taskArtifactImportRow(t, packageData, "workflow_events", "id", eventID)
	row[columnIndex(table.Columns, "previous_json")] = taskArtifactImportJSON(t, previous)
	row[columnIndex(table.Columns, "current_json")] = taskArtifactImportJSON(t, current)
}

func taskArtifactImportJSONObject(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, ok := value.(string)
	if !ok {
		t.Fatalf("expected JSON object string, got %T(%v)", value, value)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatalf("decode import event JSON object: %v", err)
	}
	return object
}

func taskArtifactImportJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode import event JSON: %v", err)
	}
	return string(encoded)
}
