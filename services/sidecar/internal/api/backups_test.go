package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	glebarezsqlite "github.com/glebarez/sqlite"
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
}
