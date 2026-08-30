package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBusinessImportPreviewsAndAtomicallyAppliesToEmptyWorkspace(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec(`
		UPDATE actors SET display_name = 'Imported Owner', version = 2, updated_at = '2026-08-28T07:59:00Z'
		WHERE id = '00000000-0000-5000-8000-000000000001';
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000002001', 'Imported Client', 'active', '2026-08-28T08:00:00Z', '2026-08-28T08:00:00Z');
		INSERT INTO projects(id, name, client_id, status, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000002002', 'Imported Project', '018f0000-0000-7000-8000-000000002001', 'in_progress', 1, '2026-08-28T08:01:00Z', '2026-08-28T08:01:00Z');
		INSERT INTO tasks(id, title, project_id, status, priority, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000002003', 'Imported Task', '018f0000-0000-7000-8000-000000002002', 'todo', 'P2', 1, '2026-08-28T08:02:00Z', '2026-08-28T08:02:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	registered := performRequest(sourceRouter, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register source Agent Adapter = %d: %s", registered.Code, registered.Body.String())
	}
	checked := performRequest(sourceRouter, http.MethodPost, "/api/v1/agent-adapters/018f0000-0000-5000-8000-000000003401/check", nil, map[string]string{"If-Match": `"1"`})
	if checked.Code != http.StatusOK {
		t.Fatalf("check source Agent Adapter = %d: %s", checked.Code, checked.Body.String())
	}
	exported := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("source export = %d: %s", exported.Code, exported.Body.String())
	}

	targetRouter, targetStore, _, _ := newBackupTestAPI(t)
	var triggersBefore int64
	if err := targetStore.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'").Row().Scan(&triggersBefore); err != nil {
		t.Fatal(err)
	}
	previewResponse := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data/preview", exported.Body.Bytes(), nil)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", previewResponse.Code, previewResponse.Body.String())
	}
	var previewEnvelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !previewEnvelope.Data.CanApply || previewEnvelope.Data.TotalRows < 5 || previewEnvelope.Data.TableCounts["tasks"] != 1 {
		t.Fatalf("preview = %#v", previewEnvelope.Data)
	}

	missingConfirmation := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", exported.Body.Bytes(), nil)
	if missingConfirmation.Code != http.StatusPreconditionRequired || responseErrorCode(t, missingConfirmation.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("missing confirmation = %d: %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}
	applyResponse := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", exported.Body.Bytes(), map[string]string{"X-Import-Confirmation": importConfirmation})
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applyResponse.Code, applyResponse.Body.String())
	}
	var resultEnvelope struct {
		Data businessImportResult `json:"data"`
	}
	if err := json.Unmarshal(applyResponse.Body.Bytes(), &resultEnvelope); err != nil || resultEnvelope.Data.BackupID == "" || resultEnvelope.Data.ImportedRows != previewEnvelope.Data.TotalRows {
		t.Fatalf("import result = %#v err=%v", resultEnvelope.Data, err)
	}
	for table, want := range map[string]int64{"clients": 1, "projects": 1, "tasks": 1, "agent_adapters": 1} {
		var count int64
		if err := targetStore.DB.Table(table).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s count = %d err=%v", table, count, err)
		}
	}
	var importedAdapter struct {
		HealthStatus   string `gorm:"column:health_status"`
		ExecutionReady bool   `gorm:"column:execution_ready"`
		Version        int64  `gorm:"column:version"`
	}
	if err := targetStore.DB.Table("agent_adapters").Take(&importedAdapter).Error; err != nil || importedAdapter.HealthStatus != "blocked" || importedAdapter.ExecutionReady || importedAdapter.Version != 1 {
		t.Fatalf("imported Agent Adapter = %#v err=%v", importedAdapter, err)
	}
	var ownerName string
	if err := targetStore.DB.Table("actors").Where("id = ?", "00000000-0000-5000-8000-000000000001").Pluck("display_name", &ownerName).Error; err != nil || ownerName != "Imported Owner" {
		t.Fatalf("owner display name = %q err=%v", ownerName, err)
	}
	var foreignKeyFailures int64
	if err := targetStore.DB.Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Row().Scan(&foreignKeyFailures); err != nil || foreignKeyFailures != 0 {
		t.Fatalf("foreign key failures = %d err=%v", foreignKeyFailures, err)
	}
	var triggersAfter int64
	if err := targetStore.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'").Row().Scan(&triggersAfter); err != nil || triggersAfter != triggersBefore {
		t.Fatalf("trigger count = %d, want %d err=%v", triggersAfter, triggersBefore, err)
	}
}

func TestBusinessImportRollsBackTamperedRelationships(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec("INSERT INTO projects(id, name) VALUES ('018f0000-0000-7000-8000-000000002020', 'Tampered Project')").Error; err != nil {
		t.Fatal(err)
	}
	packageData := emptyBusinessExportFixture(t, sourceRouter)
	for tableIndex := range packageData.Tables {
		table := &packageData.Tables[tableIndex]
		if table.Name != "projects" {
			continue
		}
		for columnIndex, column := range table.Columns {
			if column == "client_id" {
				table.Rows[0][columnIndex] = "018f0000-0000-7000-8000-000000002099"
			}
		}
	}
	body, _ := json.Marshal(packageData)
	targetRouter, targetStore, _, _ := newBackupTestAPI(t)
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importConfirmation})
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_APPLY_FAILED" {
		t.Fatalf("tampered import = %d: %s", response.Code, response.Body.String())
	}
	var projects int64
	if err := targetStore.DB.Table("projects").Count(&projects).Error; err != nil || projects != 0 {
		t.Fatalf("failed import changed target projects=%d err=%v", projects, err)
	}
}

func TestBusinessImportRejectsControlledFiles(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	packageData := emptyBusinessExportFixture(t, router)
	packageData.ArtifactFiles.ActiveCount = 1
	packageData.ArtifactFiles.ActiveBytes = 12
	body, _ := json.Marshal(packageData)
	response := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_FILES_UNSUPPORTED" {
		t.Fatalf("file import = %d: %s", response.Code, response.Body.String())
	}
}

func TestBusinessImportPreservesManualReviewTextArtifacts(t *testing.T) {
	sourceRouter, _, _, _ := newBackupTestAPI(t)
	task, _ := setupManualReviewTask(t, sourceRouter)
	submitted := performRequest(
		sourceRouter, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output",
		[]byte(`{"summary":"Importable delivery","artifacts":[{"client_ref":"note","storage_kind":"text","name":"Notes","content_text":"local text"}]}`),
		map[string]string{"If-Match": `"3"`},
	)
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit source output = %d: %s", submitted.Code, submitted.Body.String())
	}
	packageData := emptyBusinessExportFixture(t, sourceRouter)
	body, _ := json.Marshal(packageData)
	targetRouter, targetStore, _, _ := newBackupTestAPI(t)
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importConfirmation})
	if response.Code != http.StatusOK {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		detail := (&API{db: targetStore.DB}).replaceBusinessTables(context, packageData)
		t.Fatalf("import text Artifact = %d: %s detail=%v", response.Code, response.Body.String(), detail)
	}
	var artifactCount, submissionCount, eventCount int64
	if err := targetStore.DB.Table("task_artifacts").Count(&artifactCount).Error; err != nil || artifactCount != 1 {
		t.Fatalf("Artifact count = %d err=%v", artifactCount, err)
	}
	if err := targetStore.DB.Table("task_submissions").Count(&submissionCount).Error; err != nil || submissionCount != 1 {
		t.Fatalf("Submission count = %d err=%v", submissionCount, err)
	}
	if err := targetStore.DB.Table("workflow_events").Count(&eventCount).Error; err != nil || eventCount == 0 {
		t.Fatalf("Workflow Event count = %d err=%v", eventCount, err)
	}
}

func TestBusinessImportRefusesNonEmptyTargetWithoutChangingIt(t *testing.T) {
	router, store, _, _ := newBackupTestAPI(t)
	packageData := emptyBusinessExportFixture(t, router)
	if err := store.DB.Exec("INSERT INTO clients(id, name) VALUES ('018f0000-0000-7000-8000-000000002010', 'Existing Client')").Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(packageData)
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || envelope.Data.CanApply || envelope.Data.Blocker != "target_not_empty" ||
		envelope.Data.TargetSchemaVersion != 42 || envelope.Data.TargetRows != 1 || envelope.Data.KeyConflicts != 0 || len(envelope.Data.ConflictTables) != 1 ||
		envelope.Data.ConflictTables[0] != (businessImportTableConflict{Table: "clients", IncomingRows: 0, TargetRows: 1, KeyConflicts: 0}) {
		t.Fatalf("preview = %#v err=%v", envelope.Data, err)
	}
	apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importConfirmation})
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_NOT_EMPTY" {
		t.Fatalf("apply = %d: %s", apply.Code, apply.Body.String())
	}
	var count int64
	if err := store.DB.Table("clients").Where("name = ?", "Existing Client").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("existing data changed count=%d err=%v", count, err)
	}
}

func TestBusinessImportPreviewMapsTableAndPrimaryKeyConflictsWithoutChangingTarget(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	sharedID := "018f0000-0000-7000-8000-000000002060"
	if err := sourceStore.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, 'Incoming Shared Client', 'active', '2026-08-29T09:00:00Z', '2026-08-29T09:00:00Z'),
		       ('018f0000-0000-7000-8000-000000002061', 'Incoming New Client', 'active', '2026-08-29T09:01:00Z', '2026-08-29T09:01:00Z')
	`, sharedID).Error; err != nil {
		t.Fatal(err)
	}
	exported := performRequest(sourceRouter, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("source export = %d: %s", exported.Code, exported.Body.String())
	}

	targetRouter, targetStore, _, backupDir := newBackupTestAPI(t)
	if err := targetStore.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, 'Existing Shared Client', 'active', '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z'),
		       ('018f0000-0000-7000-8000-000000002062', 'Existing Only Client', 'active', '2026-08-29T08:01:00Z', '2026-08-29T08:01:00Z');
		INSERT INTO projects(id, name, status, version, created_at, updated_at)
		VALUES ('018f0000-0000-7000-8000-000000002063', 'Existing Project', 'in_progress', 1, '2026-08-29T08:02:00Z', '2026-08-29T08:02:00Z')
	`, sharedID).Error; err != nil {
		t.Fatal(err)
	}
	preview := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data/preview", exported.Body.Bytes(), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode conflict preview: %v", err)
	}
	if envelope.Data.TargetRows != 3 || envelope.Data.KeyConflicts != 1 || len(envelope.Data.ConflictTables) != 2 {
		t.Fatalf("conflict summary = %#v", envelope.Data)
	}
	if envelope.Data.ConflictTables[0] != (businessImportTableConflict{Table: "clients", IncomingRows: 2, TargetRows: 2, KeyConflicts: 1}) ||
		envelope.Data.ConflictTables[1] != (businessImportTableConflict{Table: "projects", IncomingRows: 0, TargetRows: 1, KeyConflicts: 0}) {
		t.Fatalf("conflict tables = %#v", envelope.Data.ConflictTables)
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("preview created backups: %v", backups)
	}
	var name string
	if err := targetStore.DB.Table("clients").Where("id = ?", sharedID).Pluck("name", &name).Error; err != nil || name != "Existing Shared Client" {
		t.Fatalf("preview changed target name=%q err=%v", name, err)
	}
}

func TestBusinessImportClassifiesOlderAndNewerSchemasWithoutApplyingThem(t *testing.T) {
	for _, test := range []struct {
		name    string
		schema  int
		blocker string
	}{
		{name: "older", schema: 41, blocker: "source_schema_older"},
		{name: "newer", schema: 43, blocker: "source_schema_newer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, store, _, backupDir := newBackupTestAPI(t)
			packageData := emptyBusinessExportFixture(t, router)
			packageData.Source.SchemaVersion = test.schema
			body, err := json.Marshal(packageData)
			if err != nil {
				t.Fatal(err)
			}
			preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
			if preview.Code != http.StatusOK {
				t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
			}
			var envelope struct {
				Data businessImportPreview `json:"data"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || envelope.Data.CanApply || envelope.Data.Blocker != test.blocker ||
				envelope.Data.SchemaVersion != test.schema || envelope.Data.TargetSchemaVersion != 42 || envelope.Data.TargetRows != 0 ||
				envelope.Data.KeyConflicts != 0 || len(envelope.Data.ConflictTables) != 0 {
				t.Fatalf("schema preview = %#v err=%v", envelope.Data, err)
			}
			apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importConfirmation})
			if apply.Code != http.StatusUnprocessableEntity || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_VERSION_UNSUPPORTED" {
				t.Fatalf("schema apply = %d: %s", apply.Code, apply.Body.String())
			}
			if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
				t.Fatalf("schema blocker created backups: %v", backups)
			}
			var clients int64
			if err := store.DB.Table("clients").Count(&clients).Error; err != nil || clients != 0 {
				t.Fatalf("schema blocker changed clients=%d err=%v", clients, err)
			}
		})
	}
}

func TestBusinessImportTreatsRegisteredAgentAdapterAsNonEmpty(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	packageData := emptyBusinessExportFixture(t, router)
	registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register target Agent Adapter = %d: %s", registered.Code, registered.Body.String())
	}
	body, _ := json.Marshal(packageData)
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview with registered Agent Adapter = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || envelope.Data.CanApply || envelope.Data.Blocker != "target_not_empty" {
		t.Fatalf("Agent Adapter target preview = %#v err=%v", envelope.Data, err)
	}
}

func emptyBusinessExportFixture(t *testing.T, router http.Handler) businessExportPackage {
	t.Helper()
	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export fixture = %d: %s", response.Code, response.Body.String())
	}
	var packageData businessExportPackage
	if err := json.Unmarshal(response.Body.Bytes(), &packageData); err != nil {
		t.Fatalf("decode export fixture: %v", err)
	}
	return packageData
}

func backupPackageDirectories(t *testing.T, backupDir string) []string {
	t.Helper()
	packages, err := filepath.Glob(filepath.Join(backupDir, "*", "manifest.json"))
	if err != nil {
		t.Fatalf("list backup packages: %v", err)
	}
	return packages
}
