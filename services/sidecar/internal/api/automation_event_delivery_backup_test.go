package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCompleteSQLiteBackupVerifyAndRestorePreservesPendingAutomationEventDelivery(t *testing.T) {
	root := t.TempDir()
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, root)
	rule := automationRuleByPreset(t, router, automationPresetProjectCompleted)
	enabled := performRequest(
		router,
		http.MethodPost,
		"/api/v1/automations/rules/"+rule.ID+"/enable",
		nil,
		map[string]string{"If-Match": `"1"`},
	)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable Automation rule = %d: %s", enabled.Code, enabled.Body.String())
	}

	var enabledRule models.AutomationRule
	if err := store.DB.First(&enabledRule, "id = ?", rule.ID).Error; err != nil {
		t.Fatalf("read enabled Automation rule: %v", err)
	}
	if !enabledRule.Enabled || enabledRule.Version != rule.Version+1 {
		t.Fatalf("enabled Automation rule = %#v", enabledRule)
	}
	var sourceEvent models.WorkflowEvent
	if err := store.DB.
		Where("aggregate_type = ? AND aggregate_id = ?", "automation_rule", rule.ID).
		Order("created_at DESC").Order("id DESC").
		First(&sourceEvent).Error; err != nil {
		t.Fatalf("read Automation source event: %v", err)
	}

	capturedAt := "2026-08-30T12:00:00.000000000Z"
	delivery := models.AutomationEventDelivery{
		ID:                 uuid.NewString(),
		RuleID:             enabledRule.ID,
		PresetKey:          enabledRule.PresetKey,
		RuleVersion:        enabledRule.Version,
		SourceEventID:      sourceEvent.ID,
		LogicalKey:         "event:" + enabledRule.ID + ":" + sourceEvent.ID,
		ConfigSnapshotJSON: enabledRule.ConfigJSON,
		ActionSnapshotJSON: `{"action_type":"backup_fixture","priority":"P1"}`,
		DeliveryAttempts:   0,
		AvailableAt:        "2099-08-30T12:00:00.000000000Z",
		CapturedAt:         capturedAt,
		UpdatedAt:          capturedAt,
	}
	if err := store.DB.Create(&delivery).Error; err != nil {
		t.Fatalf("create pending Automation event delivery: %v", err)
	}

	created := performRequest(
		router,
		http.MethodPost,
		"/api/v1/backups",
		[]byte(`{"note":"pending Automation delivery checkpoint"}`),
		nil,
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("create complete SQLite backup = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	verified := performRequest(router, http.MethodPost, "/api/v1/backups/"+target.ID+"/verify", []byte(`{}`), nil)
	if verified.Code != http.StatusOK || decodeBackupSummary(t, verified.Body.Bytes()).VerificationStatus != "verified" {
		t.Fatalf("verify complete SQLite backup = %d: %s", verified.Code, verified.Body.String())
	}

	snapshotPath := filepath.Join(backupDir, target.ID, "database", "opc-workspace.db")
	snapshot := openImmutableAutomationDeliveryBackupDatabase(t, snapshotPath)
	assertAutomationEventDeliverySnapshot(t, snapshot, delivery)
	assertAutomationEventDeliveryDependencies(t, snapshot, delivery)

	deleted := store.DB.Delete(&models.AutomationEventDelivery{}, "id = ?", delivery.ID)
	if deleted.Error != nil || deleted.RowsAffected != 1 {
		t.Fatalf("delete live delivery after target backup rows=%d err=%v", deleted.RowsAffected, deleted.Error)
	}

	scheduled := performRequest(
		router,
		http.MethodPost,
		"/api/v1/backups/"+target.ID+"/restore",
		[]byte(`{"confirm":true}`),
		nil,
	)
	if scheduled.Code != http.StatusAccepted {
		t.Fatalf("schedule complete SQLite restore = %d: %s", scheduled.Code, scheduled.Body.String())
	}
	var scheduledEnvelope struct {
		Data scheduledRestoreResult `json:"data"`
	}
	if err := json.Unmarshal(scheduled.Body.Bytes(), &scheduledEnvelope); err != nil {
		t.Fatalf("decode scheduled restore: %v", err)
	}
	if scheduledEnvelope.Data.BackupID != target.ID || scheduledEnvelope.Data.RollbackBackupID == "" {
		t.Fatalf("scheduled restore = %#v", scheduledEnvelope.Data)
	}

	if err := router.Close(); err != nil {
		t.Fatalf("close router before restore: %v", err)
	}
	if err := store.Checkpoint(); err != nil {
		t.Fatalf("checkpoint before restore: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close database before restore: %v", err)
	}

	invoicePDFDir := filepath.Join(root, "invoices")
	latestSchemaVersion, err := database.LatestSchemaVersion()
	if err != nil {
		t.Fatalf("LatestSchemaVersion() error = %v", err)
	}
	restoreResult, err := ApplyPendingRestoreWithInvoicePDFsAndProgress(
		backupDir,
		databasePath,
		artifactDir,
		invoicePDFDir,
		latestSchemaVersion,
		nil,
	)
	if err != nil {
		t.Fatalf("apply complete SQLite restore: %v", err)
	}
	if !restoreResult.Applied || restoreResult.BackupID != target.ID ||
		restoreResult.RollbackBackupID != scheduledEnvelope.Data.RollbackBackupID {
		t.Fatalf("restore result = %#v", restoreResult)
	}

	restored, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restored.Close()
	// Assert before NewRouter can run a delivery consumer. Core recovery tests own
	// the subsequent consumption behavior; this test locks the complete SQLite
	// backup boundary and exact operational snapshot.
	assertAutomationEventDeliverySnapshot(t, restored.DB, delivery)
	assertAutomationEventDeliveryDependencies(t, restored.DB, delivery)

	rollbackPath := filepath.Join(
		backupDir,
		restoreResult.RollbackBackupID,
		"database",
		"opc-workspace.db",
	)
	rollback := openImmutableAutomationDeliveryBackupDatabase(t, rollbackPath)
	var rollbackCount int64
	if err := rollback.Model(&models.AutomationEventDelivery{}).
		Where("id = ?", delivery.ID).
		Count(&rollbackCount).Error; err != nil {
		t.Fatalf("read rollback delivery count: %v", err)
	}
	if rollbackCount != 0 {
		t.Fatalf("rollback backup delivery count = %d, want the post-target deletion", rollbackCount)
	}
}

func openImmutableAutomationDeliveryBackupDatabase(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		glebarezsqlite.Open("file:"+filepath.ToSlash(path)+"?mode=ro&immutable=1"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open immutable backup database %s: %v", path, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("access immutable backup database %s: %v", path, err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func assertAutomationEventDeliverySnapshot(
	t *testing.T,
	db *gorm.DB,
	want models.AutomationEventDelivery,
) {
	t.Helper()
	var got models.AutomationEventDelivery
	if err := db.First(&got, "id = ?", want.ID).Error; err != nil {
		t.Fatalf("read pending Automation event delivery %s: %v", want.ID, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending Automation event delivery changed\n got: %#v\nwant: %#v", got, want)
	}
}

func assertAutomationEventDeliveryDependencies(
	t *testing.T,
	db *gorm.DB,
	delivery models.AutomationEventDelivery,
) {
	t.Helper()
	var ruleCount, eventCount int64
	if err := db.Model(&models.AutomationRule{}).
		Where("id = ? AND preset_key = ? AND version = ?", delivery.RuleID, delivery.PresetKey, delivery.RuleVersion).
		Count(&ruleCount).Error; err != nil {
		t.Fatalf("count captured Automation rule: %v", err)
	}
	if err := db.Model(&models.WorkflowEvent{}).
		Where("id = ?", delivery.SourceEventID).
		Count(&eventCount).Error; err != nil {
		t.Fatalf("count captured source event: %v", err)
	}
	if ruleCount != 1 || eventCount != 1 {
		t.Fatalf("delivery dependencies rule=%d event=%d, want 1/1", ruleCount, eventCount)
	}
}
