package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newBackupTestAPI(t *testing.T) (*gin.Engine, *database.Store, string, string) {
	t.Helper()
	router, store, _, artifactDir, backupDir := newBackupCapacityTestRuntime(t, nil)
	return router.Engine, store, artifactDir, backupDir
}

func newBackupCapacityTestRuntime(t *testing.T, checker func(string) (uint64, uint64, error)) (*Router, *database.Store, string, string, string) {
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
		DiskSpaceCheck:         checker,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router, store, databasePath, artifactDir, backupDir
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
	client := createClientForTest(t, router, `{"name":"Backup Attachment Client"}`, nil)
	clientAttachmentBody := []byte("backup client attachment body")
	clientAttachmentRecorder := performClientAttachmentUpload(
		t, router, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"client-backup.txt"}`, "client-backup.txt", clientAttachmentBody,
		map[string]string{"If-Match": `"1"`},
	)
	if clientAttachmentRecorder.Code != http.StatusCreated {
		t.Fatalf("upload backup client attachment = %d: %s", clientAttachmentRecorder.Code, clientAttachmentRecorder.Body.String())
	}
	clientAttachmentID := decodeClientAttachmentResponse(t, clientAttachmentRecorder.Body.Bytes()).ID
	project := createProjectForTest(t, router, `{"name":"Backup Attachment Project"}`, nil)
	projectAttachmentBody := []byte("backup project attachment body")
	projectAttachmentRecorder := performClientAttachmentUpload(
		t, router, "/api/v1/projects/"+project.ID+"/attachments",
		`{"name":"project-backup.txt"}`, "project-backup.txt", projectAttachmentBody,
		map[string]string{"If-Match": `"1"`},
	)
	if projectAttachmentRecorder.Code != http.StatusCreated {
		t.Fatalf("upload backup project attachment = %d: %s", projectAttachmentRecorder.Code, projectAttachmentRecorder.Body.String())
	}
	projectAttachmentID := decodeProjectAttachmentResponse(t, projectAttachmentRecorder.Body.Bytes()).ID
	_, avatarRef := replaceWorkspaceAvatarForTest(t, router, 0, nil, testPNGAvatar)
	avatarName := strings.TrimPrefix(avatarRef, "avatars/")

	headers := map[string]string{"Idempotency-Key": "manual-backup-1"}
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"提交前检查点"}`), headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	if summary.VerificationStatus != "verified" || summary.SchemaVersion != store.SchemaVersion || summary.ArtifactCount != 4 || summary.ArtifactBytes != int64(len("backup artifact body")+len(clientAttachmentBody)+len(projectAttachmentBody)+len(testPNGAvatar)) || summary.VerifiedAt == "" {
		t.Fatalf("backup summary = %#v", summary)
	}
	packagePath := filepath.Join(backupDir, summary.ID)
	for _, path := range []string{
		filepath.Join(packagePath, backupManifestName),
		filepath.Join(packagePath, "database", "opc-workspace.db"),
		filepath.Join(packagePath, "artifacts", artifactStoreMarkerName),
		filepath.Join(packagePath, "artifacts", "objects", artifactID),
		filepath.Join(packagePath, "artifacts", "objects", clientAttachmentID),
		filepath.Join(packagePath, "artifacts", "objects", projectAttachmentID),
		filepath.Join(packagePath, "artifacts", "avatars", avatarName),
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
		drillEnvelope.Data.ArtifactCount != 4 ||
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

func TestBackupCapacityRequirementIncludesOverheadsAndRejectsOverflow(t *testing.T) {
	required, err := backupCapacityRequirement(10, 20, 30)
	want := uint64(10 + 20 + 30 + maxBackupManifest + backupCapacityMinimumReserve)
	if err != nil || required != want {
		t.Fatalf("small backup capacity requirement=%d err=%v, want %d", required, err, want)
	}
	largeDatabaseBytes := uint64(5 * backupCapacityMinimumReserve)
	largePayloadBytes := largeDatabaseBytes + maxBackupManifest
	largeWant := largePayloadBytes + (largePayloadBytes+backupCapacitySafetyDivisor-1)/backupCapacitySafetyDivisor
	largeRequired, err := backupCapacityRequirement(largeDatabaseBytes, 0, 0)
	if err != nil || largeRequired != largeWant {
		t.Fatalf("large backup capacity requirement=%d err=%v, want %d", largeRequired, err, largeWant)
	}
	if _, err := backupCapacityRequirement(^uint64(0), 1, 1); !errors.Is(err, errBackupCapacityUnavailable) {
		t.Fatalf("overflow requirement error=%v, want capacity unavailable", err)
	}
	if _, ok := checkedBackupCapacityMultiply(^uint64(0), 2); ok {
		t.Fatal("overflowing SQLite allocation multiplication was accepted")
	}
}

func TestBackupCreateRejectsInsufficientCapacityWithoutSideEffects(t *testing.T) {
	var available, total uint64
	var probedPaths []string
	runtime, store, databasePath, artifactDir, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
		probedPaths = append(probedPaths, path)
		return available, total, nil
	})
	task := createTaskForTaskFacts(t, runtime.Engine, `{"title":"Capacity-protected task"}`)
	var workflowCountBefore int64
	if err := store.DB.Table("workflow_events").Count(&workflowCountBefore).Error; err != nil {
		t.Fatal(err)
	}
	required, err := estimateBackupCreateCapacity(store.DB, databasePath, runtime.artifactStore, store.SchemaVersion)
	if err != nil || required == 0 {
		t.Fatalf("estimate backup capacity=%d err=%v", required, err)
	}
	available, total = required-1, required

	const note = "capacity-note-canary-C-private"
	rejected := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"`+note+`"}`), nil)
	if rejected.Code != http.StatusInsufficientStorage || responseErrorCode(t, rejected.Body.Bytes()) != "BACKUP_SPACE_INSUFFICIENT" {
		t.Fatalf("insufficient backup capacity = %d: %s", rejected.Code, rejected.Body.String())
	}
	if len(probedPaths) != 1 || !sameFilesystemPath(probedPaths[0], backupDir) {
		t.Fatalf("capacity probe paths=%q, want only backup root", probedPaths)
	}
	for _, privateValue := range []string{note, backupDir, databasePath, artifactDir} {
		if strings.Contains(rejected.Body.String(), privateValue) {
			t.Fatalf("capacity response leaked private value %q: %s", privateValue, rejected.Body.String())
		}
	}
	assertBackupRootEmpty(t, backupDir)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE COALESCE(previous_json, '') LIKE ? OR COALESCE(current_json, '') LIKE ?", 0, "%"+note+"%", "%"+note+"%")

	var taskAfter models.Task
	if err := store.DB.First(&taskAfter, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if taskAfter.Title != task.Title || taskAfter.Status != task.Status || taskAfter.Version != task.Version {
		t.Fatalf("rejected backup changed task before=%#v after=%#v", task, taskAfter)
	}
	var workflowCountAfter int64
	if err := store.DB.Table("workflow_events").Count(&workflowCountAfter).Error; err != nil {
		t.Fatal(err)
	}
	if workflowCountAfter != workflowCountBefore {
		t.Fatalf("rejected backup changed workflow data before=%d after=%d", workflowCountBefore, workflowCountAfter)
	}
}

func TestBackupCreateRejectsOversizedControlledFileBeforeProbeOrStaging(t *testing.T) {
	checks := 0
	runtime, store, databasePath, artifactDir, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		checks++
		return 1 << 60, 1 << 60, nil
	})
	task, _ := setupManualReviewTask(t, runtime.Engine)
	const artifactBody = "registered controlled file"
	uploaded := performMultipartRequest(
		runtime.Engine,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Capacity mismatch fixture","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"capacity.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte(artifactBody)},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload capacity mismatch fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	var row controlledFileBackupRow
	if err := store.DB.Raw(`
		SELECT id, relative_path, size_bytes, sha256
		FROM task_artifacts
		WHERE storage_kind = 'file' AND deleted_at IS NULL
		LIMIT 1
	`).Scan(&row).Error; err != nil || row.ID == "" {
		t.Fatalf("read capacity mismatch fixture row=%#v err=%v", row, err)
	}
	objectPath := filepath.Join(artifactDir, filepath.FromSlash(row.RelativePath))
	oversizedBody := append([]byte(artifactBody), make([]byte, 4096)...)
	if err := os.WriteFile(objectPath, oversizedBody, 0o600); err != nil {
		t.Fatalf("enlarge controlled file: %v", err)
	}

	const note = "oversized-controlled-file-note"
	rejected := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"`+note+`"}`), nil)
	if rejected.Code != http.StatusServiceUnavailable || responseErrorCode(t, rejected.Body.Bytes()) != "BACKUP_CAPACITY_UNAVAILABLE" {
		t.Fatalf("oversized controlled file backup = %d: %s", rejected.Code, rejected.Body.String())
	}
	if checks != 0 {
		t.Fatalf("oversized controlled file triggered %d capacity probes", checks)
	}
	for _, privateValue := range []string{note, databasePath, artifactDir, backupDir, row.RelativePath} {
		if strings.Contains(rejected.Body.String(), privateValue) {
			t.Fatalf("oversized controlled file response leaked %q: %s", privateValue, rejected.Body.String())
		}
	}
	assertBackupRootEmpty(t, backupDir)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
}

func TestBackupCreateRejectsUnavailableCapacityWithoutLeakingOrProjecting(t *testing.T) {
	tests := []struct {
		name      string
		available uint64
		total     uint64
		err       error
	}{
		{name: "probe error", available: 123456789, total: 987654321, err: errors.New(`private probe failed at C:\secret`)},
		{name: "zero total", available: 0, total: 0},
		{name: "available exceeds total", available: 987654321, total: 123456789},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var probedPaths []string
			runtime, store, databasePath, _, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
				probedPaths = append(probedPaths, path)
				return test.available, test.total, test.err
			})
			const note = "unavailable-capacity-note-canary"
			rejected := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"`+note+`"}`), nil)
			if rejected.Code != http.StatusServiceUnavailable || responseErrorCode(t, rejected.Body.Bytes()) != "BACKUP_CAPACITY_UNAVAILABLE" {
				t.Fatalf("unavailable backup capacity = %d: %s", rejected.Code, rejected.Body.String())
			}
			if len(probedPaths) != 1 || !sameFilesystemPath(probedPaths[0], backupDir) {
				t.Fatalf("capacity probe paths=%q, want only backup root", probedPaths)
			}
			for _, privateValue := range []string{note, backupDir, databasePath, "private probe", "C:\\secret", "123456789", "987654321"} {
				if strings.Contains(rejected.Body.String(), privateValue) {
					t.Fatalf("capacity response leaked private value %q: %s", privateValue, rejected.Body.String())
				}
			}
			assertBackupRootEmpty(t, backupDir)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE COALESCE(previous_json, '') LIKE ? OR COALESCE(current_json, '') LIKE ?", 0, "%"+note+"%", "%"+note+"%")
		})
	}
}

func TestBackupCreateAcceptsExactCapacityAndReplayBypassesCapacityProbe(t *testing.T) {
	var available, total uint64
	probeUnavailable := false
	checks := 0
	var probedPath string
	runtime, store, databasePath, _, backupDir := newBackupCapacityTestRuntime(t, func(path string) (uint64, uint64, error) {
		checks++
		probedPath = path
		if probeUnavailable {
			return 0, 0, errors.New("capacity probe must not run during replay")
		}
		return available, total, nil
	})
	task, _ := setupManualReviewTask(t, runtime.Engine)
	const artifactBody = "capacity boundary controlled file"
	uploaded := performMultipartRequest(
		runtime.Engine,
		"/api/v1/tasks/"+task.ID+"/submit-output",
		`{"summary":"Capacity fixture","artifacts":[{"client_ref":"upload","storage_kind":"file","name":"capacity.txt","file_field":"file"}]}`,
		map[string][]byte{"file": []byte(artifactBody)},
		map[string]string{"If-Match": `"3"`},
	)
	if uploaded.Code != http.StatusCreated {
		t.Fatalf("upload capacity fixture = %d: %s", uploaded.Code, uploaded.Body.String())
	}
	required, err := estimateBackupCreateCapacity(store.DB, databasePath, runtime.artifactStore, store.SchemaVersion)
	if err != nil || required == 0 {
		t.Fatalf("estimate exact capacity=%d err=%v", required, err)
	}
	available, total = required, required
	headers := map[string]string{"Idempotency-Key": "capacity-boundary-replay"}
	created := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"exact capacity"}`), headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("exact-capacity backup = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	if checks != 1 || !sameFilesystemPath(probedPath, backupDir) || summary.ArtifactBytes != int64(len(artifactBody)) {
		t.Fatalf("exact-capacity probe checks=%d path=%q summary=%#v", checks, probedPath, summary)
	}
	snapshotPath := filepath.Join(backupDir, summary.ID, "database", "opc-workspace.db")
	snapshotBefore, err := inspectFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}

	probeUnavailable = true
	checks = 0
	replayed := performRequest(runtime.Engine, http.MethodPost, "/api/v1/backups", []byte(`{"note":"exact capacity"}`), headers)
	if replayed.Code != http.StatusCreated || decodeBackupSummary(t, replayed.Body.Bytes()).ID != summary.ID {
		t.Fatalf("capacity-independent replay = %d: %s", replayed.Code, replayed.Body.String())
	}
	if checks != 0 {
		t.Fatalf("idempotent replay performed %d capacity probes", checks)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != summary.ID {
		t.Fatalf("replay backup packages=%v err=%v", entries, err)
	}
	snapshotAfter, err := inspectFile(snapshotPath)
	if err != nil || snapshotAfter != snapshotBefore {
		t.Fatalf("replay changed backup data before=%#v after=%#v err=%v", snapshotBefore, snapshotAfter, err)
	}
}

func assertBackupRootEmpty(t *testing.T, backupDir string) {
	t.Helper()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected backup left packages or staging entries: %v", entries)
	}
}

func TestBackupCreateFailureProjectsOneSafeMaintenanceInboxIncident(t *testing.T) {
	runtime, store, _, _, backupDir := newBackupCapacityTestRuntime(t, func(string) (uint64, uint64, error) {
		return 1 << 60, 1 << 60, nil
	})
	router := runtime.Engine
	if err := os.Remove(backupDir); err != nil {
		t.Fatalf("remove backup root: %v", err)
	}

	first := performRequest(
		router,
		http.MethodPost,
		"/api/v1/backups",
		[]byte(`{"note":"do not persist this note"}`),
		nil,
	)
	if first.Code != http.StatusInternalServerError || responseErrorCode(t, first.Body.Bytes()) != "BACKUP_CREATE_FAILED" {
		t.Fatalf("first failed backup = %d: %s", first.Code, first.Body.String())
	}
	second := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{}`), nil)
	if second.Code != http.StatusInternalServerError || responseErrorCode(t, second.Body.Bytes()) != "BACKUP_CREATE_FAILED" {
		t.Fatalf("second failed backup = %d: %s", second.Code, second.Body.String())
	}

	var incident models.InboxItem
	if err := store.DB.Where(
		"source_entity_type = ? AND source_entity_id = ?",
		systemMaintenanceInboxSourceType,
		systemMaintenanceSourceID(backupCreateMaintenanceIncident.component, backupCreateMaintenanceIncident.operation),
	).First(&incident).Error; err != nil {
		t.Fatalf("load maintenance incident: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 1)
	if incident.Kind != "event" || incident.Priority != "P1" || incident.Title != backupCreateMaintenanceIncident.title ||
		incident.Summary != backupCreateMaintenanceIncident.message || incident.SourceEventKey == nil ||
		!strings.HasPrefix(*incident.SourceEventKey, "system:backup:create:") ||
		strings.Contains(incident.PayloadJSON, "do not persist this note") || strings.Contains(incident.PayloadJSON, backupDir) {
		t.Fatalf("maintenance incident must be a safe, stable snapshot: %#v", incident)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(incident.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode maintenance payload: %v", err)
	}
	if payload["component"] != "backup" || payload["operation"] != "create" ||
		payload["failure_code"] != backupCreateMaintenanceIncident.failureCode || payload["message"] != backupCreateMaintenanceIncident.message {
		t.Fatalf("maintenance payload = %#v", payload)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_projected' AND actor_id = ?", 1, incident.ID, models.BuiltinSystemActorID)
	if len(payload) != 5 {
		t.Fatalf("maintenance payload must only keep safe fields: %#v", payload)
	}
	for _, key := range []string{"error", "path", "note", "request_id", "token"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("maintenance payload leaked %s: %#v", key, payload)
		}
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+incident.ID, nil, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("get maintenance incident = %d: %s", detail.Code, detail.Body.String())
	}
	var itemEnvelope struct {
		Data inboxItemOutput `json:"data"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &itemEnvelope); err != nil {
		t.Fatalf("decode maintenance incident API: %v", err)
	}
	if itemEnvelope.Data.Kind != "event" ||
		itemEnvelope.Data.SourceEntityType != systemMaintenanceInboxSourceType ||
		itemEnvelope.Data.SourceEntityID == nil ||
		*itemEnvelope.Data.SourceEntityID != "backup:create" ||
		itemEnvelope.Data.DueAt != nil ||
		itemEnvelope.Data.SourceDeletedAt != nil ||
		itemEnvelope.Data.PayloadJSON["failure_code"] != backupCreateMaintenanceIncident.failureCode ||
		len(itemEnvelope.Data.PayloadJSON) != 5 {
		t.Fatalf("maintenance incident API = %#v", itemEnvelope.Data)
	}

	duePatch := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/inbox-items/"+incident.ID,
		[]byte(`{"due_at":"2026-08-29T12:00:00Z"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if duePatch.Code != http.StatusUnprocessableEntity || responseErrorCode(t, duePatch.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("system maintenance due date patch = %d: %s", duePatch.Code, duePatch.Body.String())
	}

	resolved := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+incident.ID+"/resolve",
		[]byte(`{"reason":"Storage issue acknowledged"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve maintenance incident = %d: %s", resolved.Code, resolved.Body.String())
	}
	third := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{}`), nil)
	if third.Code != http.StatusInternalServerError {
		t.Fatalf("failed backup after resolution = %d: %s", third.Code, third.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 2)
}

func TestBackupVerifyFailureProjectsOneSafeMaintenanceInboxIncident(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"do not persist verify note"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup fixture = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	originalPersist := persistVerifiedBackupManifest
	persistVerifiedBackupManifest = func(packagePath string, manifest backupManifest) error {
		if strings.Contains(packagePath, summary.ID) && manifest.ID == summary.ID {
			return errors.New("simulated verification record failure at " + packagePath)
		}
		return originalPersist(packagePath, manifest)
	}
	t.Cleanup(func() { persistVerifiedBackupManifest = originalPersist })

	first := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if first.Code != http.StatusInternalServerError || responseErrorCode(t, first.Body.Bytes()) != "BACKUP_VERIFY_FAILED" {
		t.Fatalf("first failed verify = %d: %s", first.Code, first.Body.String())
	}
	second := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if second.Code != http.StatusInternalServerError || responseErrorCode(t, second.Body.Bytes()) != "BACKUP_VERIFY_FAILED" {
		t.Fatalf("second failed verify = %d: %s", second.Code, second.Body.String())
	}

	sourceID := systemMaintenanceSourceID(backupVerifyMaintenanceIncident.component, backupVerifyMaintenanceIncident.operation)
	var incident models.InboxItem
	if err := store.DB.Where(
		"source_entity_type = ? AND source_entity_id = ?",
		systemMaintenanceInboxSourceType,
		sourceID,
	).First(&incident).Error; err != nil {
		t.Fatalf("load verify incident: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = ?", 1, sourceID)
	if incident.Kind != "event" || incident.Priority != "P1" || incident.Title != backupVerifyMaintenanceIncident.title ||
		incident.Summary != backupVerifyMaintenanceIncident.message || incident.SourceEventKey == nil ||
		!strings.HasPrefix(*incident.SourceEventKey, "system:backup:verify:") ||
		strings.Contains(incident.PayloadJSON, "do not persist verify note") ||
		strings.Contains(incident.PayloadJSON, "simulated verification") ||
		strings.Contains(incident.PayloadJSON, summary.ID) {
		t.Fatalf("verify incident must be a safe, stable snapshot: %#v", incident)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(incident.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}
	if payload["component"] != "backup" || payload["operation"] != "verify" ||
		payload["failure_code"] != backupVerifyMaintenanceIncident.failureCode ||
		payload["message"] != backupVerifyMaintenanceIncident.message || len(payload) != 5 {
		t.Fatalf("verify payload = %#v", payload)
	}
	for _, key := range []string{"error", "path", "note", "request_id", "token", "backup_id"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("verify payload leaked %s: %#v", key, payload)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_projected' AND actor_id = ?", 1, incident.ID, models.BuiltinSystemActorID)

	resolved := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+incident.ID+"/resolve",
		[]byte(`{"reason":"Storage issue acknowledged"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve verify incident = %d: %s", resolved.Code, resolved.Body.String())
	}
	third := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/verify", []byte(`{}`), nil)
	if third.Code != http.StatusInternalServerError {
		t.Fatalf("failed verify after resolution = %d: %s", third.Code, third.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = ?", 2, sourceID)
}

func TestBackupDrillFailureProjectsOneSafeMaintenanceInboxIncident(t *testing.T) {
	router, store, _, backupDir := newBackupTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup fixture = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	originalDrill := runBackupRestoreDrill
	runBackupRestoreDrill = func(_ *backupStore, _ string, _ backupManifest, _ int) (backupRestoreDrillResult, error) {
		return backupRestoreDrillResult{}, errors.New("simulated drill failure at " + backupDir)
	}
	t.Cleanup(func() { runBackupRestoreDrill = originalDrill })

	for attempt := 0; attempt < 2; attempt++ {
		failed := performRequest(router, http.MethodPost, "/api/v1/backups/"+summary.ID+"/drill", []byte(`{}`), nil)
		if failed.Code != http.StatusConflict || responseErrorCode(t, failed.Body.Bytes()) != "BACKUP_NOT_RESTORABLE" {
			t.Fatalf("failed drill %d = %d: %s", attempt, failed.Code, failed.Body.String())
		}
	}

	assertSafeBackupMaintenanceIncident(t, store, backupDrillMaintenanceIncident, backupDir, summary.ID, "simulated drill failure")
}

func TestBackupRestoreScheduleFailureProjectsOneSafeMaintenanceInboxIncident(t *testing.T) {
	router, store, _, backupDir := newBackupTestAPI(t)
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup fixture = %d: %s", created.Code, created.Body.String())
	}
	summary := decodeBackupSummary(t, created.Body.Bytes())
	originalPublish := publishPendingRestorePackage
	publishPendingRestorePackage = func(_ *backupStore, _ string, _ backupManifest, _ pendingRestorePlan, _ int) error {
		return errors.New("simulated restore scheduling failure at " + backupDir)
	}
	t.Cleanup(func() { publishPendingRestorePackage = originalPublish })

	failed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/backups/"+summary.ID+"/restore",
		[]byte(`{"confirm":true}`),
		nil,
	)
	if failed.Code != http.StatusInternalServerError || responseErrorCode(t, failed.Body.Bytes()) != "RESTORE_SCHEDULE_FAILED" {
		t.Fatalf("failed restore schedule = %d: %s", failed.Code, failed.Body.String())
	}

	assertSafeBackupMaintenanceIncident(t, store, backupRestoreMaintenanceIncident, backupDir, summary.ID, "simulated restore scheduling failure")
}

func assertSafeBackupMaintenanceIncident(
	t *testing.T,
	store *database.Store,
	incidentType systemMaintenanceIncident,
	forbidden ...string,
) {
	t.Helper()
	sourceID := systemMaintenanceSourceID(incidentType.component, incidentType.operation)
	var incident models.InboxItem
	if err := store.DB.Where(
		"source_entity_type = ? AND source_entity_id = ?",
		systemMaintenanceInboxSourceType,
		sourceID,
	).First(&incident).Error; err != nil {
		t.Fatalf("load %s maintenance incident: %v", incidentType.operation, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = ?", 1, sourceID)
	if incident.Kind != "event" || incident.Priority != "P1" || incident.Title != incidentType.title ||
		incident.Summary != incidentType.message || incident.SourceEventKey == nil ||
		!strings.HasPrefix(*incident.SourceEventKey, "system:"+sourceID+":") {
		t.Fatalf("%s incident facts = %#v", incidentType.operation, incident)
	}
	for _, value := range forbidden {
		if strings.Contains(incident.PayloadJSON, value) {
			t.Fatalf("%s incident leaked %q: %s", incidentType.operation, value, incident.PayloadJSON)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(incident.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode %s incident payload: %v", incidentType.operation, err)
	}
	if len(payload) != 5 || payload["component"] != incidentType.component ||
		payload["operation"] != incidentType.operation || payload["failure_code"] != incidentType.failureCode ||
		payload["message"] != incidentType.message {
		t.Fatalf("%s incident payload = %#v", incidentType.operation, payload)
	}
}

func TestBackupVerificationRejectsTamperingAndUnexpectedFiles(t *testing.T) {
	router, store, _, backupDir := newBackupTestAPI(t)
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
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
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
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 0)
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
	client := createClientForTest(t, router.Engine, `{"name":"Restored Attachment Client"}`, nil)
	clientAttachmentRecorder := performClientAttachmentUpload(
		t, router.Engine, "/api/v1/clients/"+client.ID+"/attachments",
		`{"name":"restore-client.txt"}`, "restore-client.txt", []byte("restored client attachment body"),
		map[string]string{"If-Match": `"1"`},
	)
	if clientAttachmentRecorder.Code != http.StatusCreated {
		t.Fatalf("upload restore client attachment = %d: %s", clientAttachmentRecorder.Code, clientAttachmentRecorder.Body.String())
	}
	clientAttachmentID := decodeClientAttachmentResponse(t, clientAttachmentRecorder.Body.Bytes()).ID
	project := createProjectForTest(t, router.Engine, `{"name":"Restored Attachment Project"}`, nil)
	projectAttachmentRecorder := performClientAttachmentUpload(
		t, router.Engine, "/api/v1/projects/"+project.ID+"/attachments",
		`{"name":"restore-project.txt"}`, "restore-project.txt", []byte("restored project attachment body"),
		map[string]string{"If-Match": `"1"`},
	)
	if projectAttachmentRecorder.Code != http.StatusCreated {
		t.Fatalf("upload restore project attachment = %d: %s", projectAttachmentRecorder.Code, projectAttachmentRecorder.Body.String())
	}
	projectAttachmentID := decodeProjectAttachmentResponse(t, projectAttachmentRecorder.Body.Bytes()).ID
	_, restoredAvatarRef := replaceWorkspaceAvatarForTest(t, router.Engine, 0, nil, testPNGAvatar)
	created := performRequest(router, http.MethodPost, "/api/v1/backups", []byte(`{"note":"restore target"}`), nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create restore target = %d: %s", created.Code, created.Body.String())
	}
	target := decodeBackupSummary(t, created.Body.Bytes())
	replacementAvatar := append(append([]byte(nil), testPNGAvatar...), 9)
	_, lateAvatarRef := replaceWorkspaceAvatarForTest(t, router.Engine, 1, &restoredAvatarRef, replacementAvatar)
	if lateAvatarRef == restoredAvatarRef {
		t.Fatal("late avatar did not replace restore target avatar")
	}
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

	result, err := ApplyPendingRestore(backupDir, databasePath, artifactDir, 29)
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
	clientContent := performRequest(restoredRouter, http.MethodGet, "/api/v1/client-attachments/"+clientAttachmentID+"/content", nil, nil)
	if clientContent.Code != http.StatusOK || clientContent.Body.String() != "restored client attachment body" {
		t.Fatalf("restored client attachment = %d: %q", clientContent.Code, clientContent.Body.String())
	}
	projectContent := performRequest(restoredRouter, http.MethodGet, "/api/v1/project-attachments/"+projectAttachmentID+"/content", nil, nil)
	if projectContent.Code != http.StatusOK || projectContent.Body.String() != "restored project attachment body" {
		t.Fatalf("restored project attachment = %d: %q", projectContent.Code, projectContent.Body.String())
	}
	avatarContent := performRequest(restoredRouter, http.MethodGet, "/api/v1/settings/avatar/content", nil, nil)
	if avatarContent.Code != http.StatusOK || string(avatarContent.Body.Bytes()) != string(testPNGAvatar) {
		t.Fatalf("restored workspace avatar = %d: %x", avatarContent.Code, avatarContent.Body.Bytes())
	}
	if _, err := os.Lstat(filepath.Join(artifactDir, filepath.FromSlash(lateAvatarRef))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late avatar survived target restore: %v", err)
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
		filepath.Join(artifactDir, ".restore-*-avatars-*"),
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
		avatars:     filepath.Join(artifactRoot, "avatars"),
		avatarsOld:  filepath.Join(artifactRoot, ".restore-old-avatars-test"),
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
