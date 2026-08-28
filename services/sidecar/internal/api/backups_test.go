package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newBackupTestAPI(t *testing.T) (*gin.Engine, *database.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	artifactDir := filepath.Join(root, "artifacts")
	backupDir := filepath.Join(root, "backups")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := NewRouter(store.DB, Options{
		AppVersion: "0.1.0-test", Commit: "backup-test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactDir,
		DatabasePath: databasePath, BackupDir: backupDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router.Engine, store, artifactDir, backupDir
}

func newBackupRestoreTestRuntime(t *testing.T, root string) (*Router, *database.Store, string, string, string) {
	t.Helper()
	databasePath := filepath.Join(root, "workspace.db")
	artifactDir := filepath.Join(root, "artifacts")
	backupDir := filepath.Join(root, "backups")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "0.1.0-test", Commit: "restore-test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactDir,
		DatabasePath: databasePath, BackupDir: backupDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router, store, databasePath, artifactDir, backupDir
}

func decodeBackupSummary(t *testing.T, body []byte) backupSummary {
	t.Helper()
	var envelope struct {
		Data backupSummary `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode backup response: %v: %s", err, body)
	}
	return envelope.Data
}

func TestBackupAPICreatesListsReplaysAndVerifiesCompletePackage(t *testing.T) {
	router, store, _, backupDir := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	manifest := `{"summary":"Backup file","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"backup.txt","file_field":"file"}]}`
	uploaded := performMultipartRequest(router, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"file": []byte("backup artifact body")}, map[string]string{"If-Match": `"3"`})
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit output = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID

	headers := map[string]string{"Idempotency-Key": "manual-backup-1"}
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"提交前检查点"}`), headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	if summary.VerificationStatus != "verified" || summary.SchemaVersion != store.SchemaVersion || summary.ArtifactCount != 1 || summary.ArtifactBytes != int64(len("backup artifact body")) || summary.VerifiedAt == "" {
		t.Fatalf("backup summary = %#v", summary)
	}
	packagePath := filepath.Join(backupDir, summary.ID)
	for _, path := range []string{
		filepath.Join(packagePath, backupManifestName),
		filepath.Join(packagePath, "database", "opc-workspace.db"),
		filepath.Join(packagePath, "artifacts", artifactStoreMarkerName),
		filepath.Join(packagePath, "artifacts", "objects", artifactID),
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("backup file %s missing or invalid: info=%v err=%v", path, info, err)
		}
	}

	lateTask := createTaskForTaskFacts(t, router, `{"title":"Created after backup"}`)
	snapshotPath := filepath.Join(packagePath, "database", "opc-workspace.db")
	snapshot, err := gorm.Open(glebarezsqlite.Open("file:"+filepath.ToSlash(snapshotPath)+"?mode=ro&immutable=1"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	snapshotSQL, _ := snapshot.DB()
	defer snapshotSQL.Close()
	var originalCount, lateCount int64
	if err := snapshot.Table("tasks").Where("id = ?", task.ID).Count(&originalCount).Error; err != nil {
		t.Fatalf("query original snapshot task: %v", err)
	}
	if err := snapshot.Table("tasks").Where("id = ?", lateTask.ID).Count(&lateCount).Error; err != nil {
		t.Fatalf("query late snapshot task: %v", err)
	}
	if originalCount != 1 || lateCount != 0 {
		t.Fatalf("snapshot boundary original=%d late=%d", originalCount, lateCount)
	}

	replay := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"提交前检查点"}`), headers)
	if replay.Code != http.StatusCreated || decodeBackupSummary(t, replay.Body.Bytes()).ID != summary.ID {
		t.Fatalf("backup replay = %d: %s", replay.Code, replay.Body.String())
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backup replay directories=%v err=%v", entries, err)
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"不同说明"}`), headers)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("backup conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/backups", nil, nil)
	if listed.Code != http.StatusOK || !json.Valid(listed.Body.Bytes()) {
		t.Fatalf("list backups = %d: %s", listed.Code, listed.Body.String())
	}
	var listEnvelope struct {
		Data []backupSummary `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listEnvelope); err != nil || len(listEnvelope.Data) != 1 || listEnvelope.Data[0].ID != summary.ID {
		t.Fatalf("backup list = %#v err=%v", listEnvelope.Data, err)
	}
	verified := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if verified.Code != http.StatusOK || decodeBackupSummary(t, verified.Body.Bytes()).VerificationStatus != "verified" {
		t.Fatalf("verify backup = %d: %s", verified.Code, verified.Body.String())
	}
	snapshotBefore, err := inspectFile(snapshotPath)
	if err != nil {
		t.Fatalf("inspect snapshot before drill: %v", err)
	}
	drilled := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/drill", []byte(`{}`), nil)
	if drilled.Code != http.StatusOK {
		t.Fatalf("restore drill = %d: %s", drilled.Code, drilled.Body.String())
	}
	var drillEnvelope struct {
		Data backupRestoreDrillResult `json:"data"`
	}
	if err := json.Unmarshal(drilled.Body.Bytes(), &drillEnvelope); err != nil {
		t.Fatalf("decode restore drill: %v", err)
	}
	if drillEnvelope.Data.BackupID != summary.ID ||
		drillEnvelope.Data.SourceSchema != store.SchemaVersion ||
		drillEnvelope.Data.ResultSchema != store.SchemaVersion ||
		drillEnvelope.Data.ArtifactCount != 1 ||
		!drillEnvelope.Data.TemporaryDataClean {
		t.Fatalf("restore drill result = %#v", drillEnvelope.Data)
	}
	snapshotAfter, err := inspectFile(snapshotPath)
	if err != nil || snapshotAfter != snapshotBefore {
		t.Fatalf("restore drill changed source snapshot before=%#v after=%#v err=%v", snapshotBefore, snapshotAfter, err)
	}
	entries, err = os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != summary.ID {
		t.Fatalf("restore drill cleanup entries=%v err=%v", entries, err)
	}
}

func TestBackupVerificationRejectsTamperingAndUnexpectedFiles(t *testing.T) {
	router, _, _, backupDir := newBackupTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create empty backup = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	packagePath := filepath.Join(backupDir, summary.ID)
	unexpected := filepath.Join(packagePath, "unexpected.txt")
	if err := os.WriteFile(unexpected, []byte("not in manifest"), 0o600); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	verified := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if verified.Code != http.StatusConflict || responseErrorCode(t, verified.Body.Bytes()) != "BACKUP_INVALID" {
		t.Fatalf("verify unexpected file = %d: %s", verified.Code, verified.Body.String())
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatalf("remove unexpected fixture: %v", err)
	}
	databasePath := filepath.Join(packagePath, "database", "opc-workspace.db")
	file, err := os.OpenFile(databasePath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open snapshot for tamper: %v", err)
	}
	if _, err := file.Write([]byte("tamper")); err != nil {
		t.Fatalf("tamper snapshot: %v", err)
	}
	_ = file.Close()
	verified = performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if verified.Code != http.StatusConflict || responseErrorCode(t, verified.Body.Bytes()) != "BACKUP_INVALID" {
		t.Fatalf("verify tampered database = %d: %s", verified.Code, verified.Body.String())
	}
}

func TestBackupAPIValidatesConfigurationAndRequests(t *testing.T) {
	router := newTestAPI(t)
	unavailable := performRequest(router, http.MethodGet, "/api/v1/backups", nil, nil)
	if unavailable.Code != http.StatusServiceUnavailable || responseErrorCode(t, unavailable.Body.Bytes()) != "BACKUP_UNAVAILABLE" {
		t.Fatalf("unconfigured backups = %d: %s", unavailable.Code, unavailable.Body.String())
	}

	configured, _, _, _ := newBackupTestAPI(t)
	invalidID := performRequest(configured, http.MethodPost, "/api/v1/backups/not-an-id/verify", []byte(`{}`), nil)
	if invalidID.Code != http.StatusBadRequest || responseErrorCode(t, invalidID.Body.Bytes()) != "INVALID_ID" {
		t.Fatalf("invalid backup id = %d: %s", invalidID.Code, invalidID.Body.String())
	}
	invalidDrillID := performRequest(configured, http.MethodPost, "/api/v1/backups/not-an-id/drill", []byte(`{}`), nil)
	if invalidDrillID.Code != http.StatusBadRequest || responseErrorCode(t, invalidDrillID.Body.Bytes()) != "INVALID_ID" {
		t.Fatalf("invalid restore drill id = %d: %s", invalidDrillID.Code, invalidDrillID.Body.String())
	}
	invalidJSON := performRequest(configured, http.MethodPost, "/api/v1/backups", []byte(`{"unknown":true}`), nil)
	if invalidJSON.Code != http.StatusBadRequest || responseErrorCode(t, invalidJSON.Body.Bytes()) != "INVALID_JSON" {
		t.Fatalf("invalid backup JSON = %d: %s", invalidJSON.Code, invalidJSON.Body.String())
	}
	invalidKey := performRequest(configured, http.MethodPost, "/api/v1/backups", []byte(`{}`), map[string]string{"Idempotency-Key": "has space"})
	if invalidKey.Code != http.StatusBadRequest || responseErrorCode(t, invalidKey.Body.Bytes()) != "INVALID_IDEMPOTENCY_KEY" {
		t.Fatalf("invalid backup idempotency key = %d: %s", invalidKey.Code, invalidKey.Body.String())
	}
	confirmation := performRequest(configured, http.MethodPost, "/api/v1/backups/018f0000-0000-7000-8000-000000001701/restore", []byte(`{"confirm":false}`), nil)
	if confirmation.Code != http.StatusUnprocessableEntity || responseErrorCode(t, confirmation.Body.Bytes()) != "RESTORE_CONFIRMATION_REQUIRED" {
		t.Fatalf("restore confirmation = %d: %s", confirmation.Code, confirmation.Body.String())
	}
	missingRestore := performRequest(configured, http.MethodPost, "/api/v1/backups/018f0000-0000-7000-8000-000000001701/restore", []byte(`{"confirm":true}`), nil)
	if missingRestore.Code != http.StatusNotFound || responseErrorCode(t, missingRestore.Body.Bytes()) != "BACKUP_NOT_FOUND" {
		t.Fatalf("missing restore target = %d: %s", missingRestore.Code, missingRestore.Body.String())
	}
}

func TestScheduledRestoreCreatesRollbackAndAppliesBeforeNextDatabaseOpen(t *testing.T) {
	root := t.TempDir()
	router, store, databasePath, artifactDir, backupDir := newBackupRestoreTestRuntime(t, root)
	task, _ := setupManualReviewTask(t, router.Engine)
	manifest := `{"summary":"Restore file","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"restore.txt","file_field":"file"}]}`
	uploaded := performMultipartRequest(router.Engine, "/api/v1/tasks/"+task.ID+"/submit-output", manifest, map[string][]byte{"file": []byte("restored artifact body")}, map[string]string{"If-Match": `"3"`})
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("submit restore fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, uploaded.Body.Bytes()).Artifacts[0].ID
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"restore target"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create restore target = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	lateTask := createTaskForTaskFacts(t, router.Engine, `{"title":"must be rolled back"}`)

	scheduled := performRequest(router, http.MethodPost, "/api/v1/backups/"+target.ID+"/restore", []byte(`{"confirm":true}`), nil)
	if scheduled.Code != http.StatusAccepted {
		t.Fatalf("schedule restore = %d: %s", scheduled.Code, scheduled.Body.String())
	}
	var scheduledEnvelope struct {
		Data scheduledRestoreResult `json:"data"`
	}
	if err := json.Unmarshal(scheduled.Body.Bytes(), &scheduledEnvelope); err != nil {
		t.Fatalf("decode scheduled restore: %v", err)
	}
	if scheduledEnvelope.Data.BackupID != target.ID || scheduledEnvelope.Data.RollbackBackupID == "" || !scheduledEnvelope.Data.RestartRequired {
		t.Fatalf("scheduled restore result = %#v", scheduledEnvelope.Data)
	}
	replayed := performRequest(router, http.MethodPost, "/api/v1/backups/"+target.ID+"/restore", []byte(`{"confirm":true}`), nil)
	if replayed.Code != http.StatusAccepted {
		t.Fatalf("replay scheduled restore = %d: %s", replayed.Code, replayed.Body.String())
	}
	var replayEnvelope struct {
		Data scheduledRestoreResult `json:"data"`
	}
	if err := json.Unmarshal(replayed.Body.Bytes(), &replayEnvelope); err != nil || replayEnvelope.Data != scheduledEnvelope.Data {
		t.Fatalf("scheduled restore replay = %#v err=%v", replayEnvelope.Data, err)
	}
	differentID := uuid.NewString()
	different := performRequest(router, http.MethodPost, "/api/v1/backups/"+differentID+"/restore", []byte(`{"confirm":true}`), nil)
	if different.Code != http.StatusConflict || responseErrorCode(t, different.Body.Bytes()) != "RESTORE_ALREADY_PENDING" {
		t.Fatalf("different pending restore = %d: %s", different.Code, different.Body.String())
	}
	blocked := performRequest(router, http.MethodGet, "/api/v1/tasks", nil, nil)
	if blocked.Code != http.StatusServiceUnavailable || responseErrorCode(t, blocked.Body.Bytes()) != "RESTORE_RESTART_REQUIRED" {
		t.Fatalf("request after scheduled restore = %d: %s", blocked.Code, blocked.Body.String())
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

	result, err := ApplyPendingRestore(backupDir, databasePath, artifactDir, 18)
	if err != nil {
		t.Fatalf("ApplyPendingRestore() error = %v", err)
	}
	if !result.Applied || result.BackupID != target.ID || result.RollbackBackupID != scheduledEnvelope.Data.RollbackBackupID {
		t.Fatalf("startup restore result = %#v", result)
	}
	if result.CleanupWarning != "" {
		t.Fatalf("startup restore cleanup warning = %q", result.CleanupWarning)
	}
	reopened, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	restoredRouter, err := NewRouter(reopened.DB, Options{
		AppVersion: "0.1.0-test", Commit: "restore-test", SchemaVersion: reopened.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: artifactDir,
		DatabasePath: databasePath, BackupDir: backupDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		_ = reopened.Close()
		t.Fatalf("open restored router: %v", err)
	}
	defer restoredRouter.Close()
	defer reopened.Close()
	if got := performRequest(restoredRouter, http.MethodGet, "/api/v1/tasks/"+task.ID, nil, nil); got.Code != http.StatusOK {
		t.Fatalf("target task after restore = %d: %s", got.Code, got.Body.String())
	}
	if got := performRequest(restoredRouter, http.MethodGet, "/api/v1/tasks/"+lateTask.ID, nil, nil); got.Code != http.StatusNotFound {
		t.Fatalf("late task after restore = %d: %s", got.Code, got.Body.String())
	}
	content := performRequest(restoredRouter, http.MethodGet, "/api/v1/artifacts/"+artifactID+"/content", nil, nil)
	if content.Code != http.StatusOK || content.Body.String() != "restored artifact body" {
		t.Fatalf("restored Artifact = %d: %q", content.Code, content.Body.String())
	}

	rollbackDatabase := filepath.Join(backupDir, result.RollbackBackupID, "database", "opc-workspace.db")
	rollback, err := gorm.Open(glebarezsqlite.Open("file:"+filepath.ToSlash(rollbackDatabase)+"?mode=ro&immutable=1"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open rollback backup: %v", err)
	}
	rollbackSQL, _ := rollback.DB()
	defer rollbackSQL.Close()
	var lateCount int64
	if err := rollback.Table("tasks").Where("id = ?", lateTask.ID).Count(&lateCount).Error; err != nil || lateCount != 1 {
		t.Fatalf("rollback backup late task count=%d err=%v", lateCount, err)
	}
	if _, err := os.Lstat(filepath.Join(backupDir, pendingRestoreDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending restore was not cleaned: %v", err)
	}
	for _, pattern := range []string{
		filepath.Join(filepath.Dir(databasePath), ".opc-restore-new-*.db"),
		filepath.Join(filepath.Dir(databasePath), ".opc-restore-old-*.db"),
		filepath.Join(artifactDir, ".restore-*-objects-*"),
	} {
		matches, _ := filepath.Glob(pattern)
		if len(matches) != 0 {
			t.Fatalf("restore left temporary paths for %s: %v", pattern, matches)
		}
	}
}

func TestRollbackRestoreSwapRecoversPartiallyMovedSQLiteSidecars(t *testing.T) {
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := restoreSwapPaths{
		database:    filepath.Join(root, "workspace.db"),
		databaseOld: filepath.Join(root, ".opc-restore-old-test.db"),
		objects:     filepath.Join(artifactRoot, "objects"),
		objectsOld:  filepath.Join(artifactRoot, ".restore-old-objects-test"),
	}
	if err := os.WriteFile(paths.database+"-wal", []byte("partial-new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.databaseOld+"-wal", []byte("original-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rollbackRestoreSwap(paths); err != nil {
		t.Fatalf("rollbackRestoreSwap() error = %v", err)
	}
	content, err := os.ReadFile(paths.database + "-wal")
	if err != nil || string(content) != "original-wal" {
		t.Fatalf("restored WAL = %q err=%v", content, err)
	}
	if _, err := os.Lstat(paths.databaseOld + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old WAL was not consumed: %v", err)
	}
}

func TestBackupDeleteRequiresConfirmationAndRemovesValidOrInvalidPackages(t *testing.T) {
	router, store, _, backupDir := newBackupTestAPI(t)
	defer store.Close()
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"delete me"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create delete fixture = %d: %s", created.Code, created.Body.String())
	}
	backup := decodeBackupSummary(t, created.Body.Bytes())
	withoutConfirmation := performRequest(router, http.MethodDelete, "/api/v1/backups/"+backup.ID, nil, nil)
	if withoutConfirmation.Code != http.StatusUnprocessableEntity || responseErrorCode(t, withoutConfirmation.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("delete without confirmation = %d: %s", withoutConfirmation.Code, withoutConfirmation.Body.String())
	}
	if _, err := os.Stat(filepath.Join(backupDir, backup.ID)); err != nil {
		t.Fatalf("unconfirmed delete changed package: %v", err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/backups/"+backup.ID+"?confirm=true", nil, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete backup = %d: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := os.Lstat(filepath.Join(backupDir, backup.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted package remains: %v", err)
	}
	repeated := performRequest(router, http.MethodDelete, "/api/v1/backups/"+backup.ID+"?confirm=true", nil, nil)
	if repeated.Code != http.StatusNotFound || responseErrorCode(t, repeated.Body.Bytes()) != "BACKUP_NOT_FOUND" {
		t.Fatalf("repeat delete = %d: %s", repeated.Code, repeated.Body.String())
	}

	invalidID := uuid.NewString()
	invalidPath := filepath.Join(backupDir, invalidID)
	if err := os.Mkdir(invalidPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidPath, "broken.txt"), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidDelete := performRequest(router, http.MethodDelete, "/api/v1/backups/"+invalidID+"?confirm=true", nil, nil)
	if invalidDelete.Code != http.StatusNoContent {
		t.Fatalf("delete invalid package = %d: %s", invalidDelete.Code, invalidDelete.Body.String())
	}
	if _, err := os.Lstat(invalidPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid package remains: %v", err)
	}

	resumableID := uuid.NewString()
	resumablePath := filepath.Join(backupDir, deletingBackupPrefix+resumableID)
	if err := os.Mkdir(resumablePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resumablePath, "leftover.txt"), []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}
	resumedDelete := performRequest(router, http.MethodDelete, "/api/v1/backups/"+resumableID+"?confirm=true", nil, nil)
	if resumedDelete.Code != http.StatusNoContent {
		t.Fatalf("resume staged delete = %d: %s", resumedDelete.Code, resumedDelete.Body.String())
	}
	if _, err := os.Lstat(resumablePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged delete remains: %v", err)
	}
}
