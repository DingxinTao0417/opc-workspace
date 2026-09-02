package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	invoice := createInvoiceForTest(
		t,
		sourceRouter,
		`{"client_id":"018f0000-0000-7000-8000-000000002001","project_id":"018f0000-0000-7000-8000-000000002002","amount_minor":128000,"currency":"CNY","issue_date":"2026-08-01","due_date":"2026-08-31"}`,
		map[string]string{"Idempotency-Key": "business-import-invoice"},
	)
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_sent"}`, "business-import-invoice-sent")
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_viewed"}`, "business-import-invoice-viewed")
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_paid","paid_date":"2026-08-20"}`, "business-import-invoice-paid")
	if invoice.FinancialEntryID == nil {
		t.Fatal("paid source invoice has no financial entry")
	}
	var sourceProject struct {
		Version   int64  `gorm:"column:version"`
		UpdatedAt string `gorm:"column:updated_at"`
	}
	if err := sourceStore.DB.Table("projects").Select("version, updated_at").Where("id = ?", "018f0000-0000-7000-8000-000000002002").Take(&sourceProject).Error; err != nil {
		t.Fatalf("read source project aggregate: %v", err)
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
	applyResponse := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", exported.Body.Bytes(), map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", applyResponse.Code, applyResponse.Body.String())
	}
	var resultEnvelope struct {
		Data businessImportResult `json:"data"`
	}
	if err := json.Unmarshal(applyResponse.Body.Bytes(), &resultEnvelope); err != nil || resultEnvelope.Data.BackupID == "" || resultEnvelope.Data.ImportedRows != previewEnvelope.Data.TotalRows {
		t.Fatalf("import result = %#v err=%v", resultEnvelope.Data, err)
	}
	for table, want := range map[string]int64{"clients": 1, "projects": 1, "tasks": 1, "invoices": 1, "financial_entries": 1, "agent_adapters": 1} {
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
	var importedProject struct {
		Version   int64  `gorm:"column:version"`
		UpdatedAt string `gorm:"column:updated_at"`
	}
	if err := targetStore.DB.Table("projects").Select("version, updated_at").Where("id = ?", "018f0000-0000-7000-8000-000000002002").Take(&importedProject).Error; err != nil || importedProject != sourceProject {
		t.Fatalf("imported project aggregate = %#v, want %#v err=%v", importedProject, sourceProject, err)
	}
	var importedInvoice invoiceResponse
	if err := targetStore.DB.Raw(`
		SELECT invoices.id, invoices.status, financial_entries.id AS financial_entry_id
		FROM invoices
		JOIN financial_entries ON financial_entries.invoice_id = invoices.id
		WHERE invoices.id = ?
	`, invoice.ID).Scan(&importedInvoice).Error; err != nil || importedInvoice.ID != invoice.ID || importedInvoice.Status != "paid" || importedInvoice.FinancialEntryID == nil || *importedInvoice.FinancialEntryID != *invoice.FinancialEntryID {
		t.Fatalf("imported invoice linkage = %#v err=%v", importedInvoice, err)
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
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_APPLY_FAILED" {
		t.Fatalf("tampered import = %d: %s", response.Code, response.Body.String())
	}
	var projects int64
	if err := targetStore.DB.Table("projects").Count(&projects).Error; err != nil || projects != 0 {
		t.Fatalf("failed import changed target projects=%d err=%v", projects, err)
	}
}

func TestBusinessImportRollsBackPaidInvoiceWithoutMatchingEntry(t *testing.T) {
	sourceRouter, _, _, _ := newBackupTestAPI(t)
	client := createClientForTest(t, sourceRouter, `{"name":"付款一致性导入客户"}`, nil)
	invoice := createInvoiceForTest(t, sourceRouter, fmt.Sprintf(
		`{"client_id":%q,"amount_minor":12800,"currency":"CNY","issue_date":"2020-01-01","due_date":"2020-01-31"}`,
		client.ID,
	), nil)
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_sent"}`, "")
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_viewed"}`, "")
	invoice = transitionInvoiceForTest(t, sourceRouter, invoice, `{"action":"mark_paid","paid_date":"2020-01-20"}`, "")
	if invoice.FinancialEntryID == nil {
		t.Fatal("source paid Invoice has no matching entry")
	}
	packageData := emptyBusinessExportFixture(t, sourceRouter)
	for index := range packageData.Tables {
		if packageData.Tables[index].Name == "financial_entries" {
			packageData.Tables[index].Rows = nil
		}
	}
	body, err := json.Marshal(packageData)
	if err != nil {
		t.Fatalf("encode inconsistent business package: %v", err)
	}
	targetRouter, targetStore, _, _ := newBackupTestAPI(t)
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "IMPORT_APPLY_FAILED" {
		t.Fatalf("inconsistent payment import = %d: %s", response.Code, response.Body.String())
	}
	for _, table := range []string{"invoices", "financial_entries"} {
		var count int64
		if err := targetStore.DB.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed payment import changed %s count=%d err=%v", table, count, err)
		}
	}
}

func TestRestoreVerificationRejectsPaidInvoiceWithoutMatchingEntry(t *testing.T) {
	_, store, _, _ := newBackupTestAPI(t)
	const (
		clientID  = "018f0000-0000-7000-8000-000000002090"
		invoiceID = "018f0000-0000-7000-8000-000000002091"
	)
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, '恢复校验客户', 'active', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, clientID).Error; err != nil {
		t.Fatalf("seed inconsistent restore client: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, amount_minor, currency, status,
			issue_date, due_date, paid_date, notes, version, created_at, updated_at
		) VALUES (
			?, 'INV-2026-RESTORE', ?, 8800, 'CNY', 'paid',
			'2026-08-01', '2026-08-20', '2026-08-18', '', 2,
			'2026-08-01T00:00:00Z', '2026-08-18T00:00:00Z'
		)
	`, invoiceID, clientID).Error; err != nil {
		t.Fatalf("seed inconsistent restore database: %v", err)
	}
	var databaseID, artifactStoreID string
	if err := store.DB.Table("workspace_identity").Select("database_id, artifact_store_id").Where("singleton = 1").Row().Scan(&databaseID, &artifactStoreID); err != nil {
		t.Fatalf("read restore identity: %v", err)
	}
	if err := verifyOpenDatabase(store, databaseID, artifactStoreID); err == nil || !strings.Contains(err.Error(), "invoice payment consistency") {
		t.Fatalf("restore verification payment consistency error = %v", err)
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
	response := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
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

func TestBusinessImportSafelyAppendsToNonEmptyTarget(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec("INSERT INTO clients(id, name) VALUES ('018f0000-0000-7000-8000-000000002011', 'Incoming Client')").Error; err != nil {
		t.Fatal(err)
	}
	dailyRule := automationRuleByPreset(t, sourceRouter, automationPresetDailyToday)
	enabled := performRequest(sourceRouter, http.MethodPost, "/api/v1/automations/rules/"+dailyRule.ID+"/enable", nil, map[string]string{"If-Match": `"1"`})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable source automation = %d: %s", enabled.Code, enabled.Body.String())
	}
	packageData := emptyBusinessExportFixture(t, sourceRouter)
	body, _ := json.Marshal(packageData)
	router, store, _, backupDir := newBackupTestAPI(t)
	if err := store.DB.Exec(`
		UPDATE actors SET display_name = 'Existing Owner', version = 2 WHERE id = '00000000-0000-5000-8000-000000000001';
		INSERT INTO clients(id, name) VALUES ('018f0000-0000-7000-8000-000000002010', 'Existing Client')
	`).Error; err != nil {
		t.Fatal(err)
	}
	preview := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || !envelope.Data.CanApply || envelope.Data.ApplyMode != importModeAppend || envelope.Data.Blocker != "" ||
		envelope.Data.TargetSchemaVersion != store.SchemaVersion || envelope.Data.TargetRows != 1 || envelope.Data.KeyConflicts != 0 || len(envelope.Data.ConflictTables) != 1 ||
		envelope.Data.ConflictTables[0] != (businessImportTableConflict{Table: "clients", IncomingRows: 1, TargetRows: 1, KeyConflicts: 0}) {
		t.Fatalf("preview = %#v err=%v", envelope.Data, err)
	}
	wrongMode := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if wrongMode.Code != http.StatusPreconditionRequired || responseErrorCode(t, wrongMode.Body.Bytes()) != "IMPORT_CONFIRMATION_REQUIRED" {
		t.Fatalf("wrong-mode apply = %d: %s", wrongMode.Code, wrongMode.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("wrong confirmation created backups: %v", backups)
	}
	apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importAppendConfirmation})
	if apply.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", apply.Code, apply.Body.String())
	}
	var result struct {
		Data businessImportResult `json:"data"`
	}
	if err := json.Unmarshal(apply.Body.Bytes(), &result); err != nil || result.Data.ApplyMode != importModeAppend || result.Data.BackupID == "" {
		t.Fatalf("append result = %#v err=%v", result.Data, err)
	}
	var clients int64
	if err := store.DB.Table("clients").Where("name IN ?", []string{"Existing Client", "Incoming Client"}).Count(&clients).Error; err != nil || clients != 2 {
		t.Fatalf("appended clients=%d err=%v", clients, err)
	}
	var ownerName string
	if err := store.DB.Table("actors").Where("id = ?", "00000000-0000-5000-8000-000000000001").Pluck("display_name", &ownerName).Error; err != nil || ownerName != "Existing Owner" {
		t.Fatalf("target owner changed to %q err=%v", ownerName, err)
	}
	var automationEnabled bool
	if err := store.DB.Table("automation_rules").Where("id = ?", dailyRule.ID).Pluck("enabled", &automationEnabled).Error; err != nil || !automationEnabled {
		t.Fatalf("source automation was not merged enabled=%t err=%v", automationEnabled, err)
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
	if envelope.Data.CanApply || envelope.Data.Blocker != importBlockerTargetConflicts || envelope.Data.ApplyMode != "" {
		t.Fatalf("conflicting import unexpectedly applies: %#v", envelope.Data)
	}
	apply := performRequest(targetRouter, http.MethodPost, "/api/v1/imports/business-data", exported.Body.Bytes(), map[string]string{"X-Import-Confirmation": importAppendConfirmation})
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_CONFLICT" {
		t.Fatalf("conflicting apply = %d: %s", apply.Code, apply.Body.String())
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
		{name: "older", schema: 42, blocker: "source_schema_older"},
		{name: "newer", schema: 55, blocker: "source_schema_newer"},
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
				envelope.Data.SchemaVersion != test.schema || envelope.Data.TargetSchemaVersion != store.SchemaVersion || envelope.Data.TargetRows != 0 ||
				envelope.Data.KeyConflicts != 0 || len(envelope.Data.ConflictTables) != 0 {
				t.Fatalf("schema preview = %#v err=%v", envelope.Data, err)
			}
			apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body, map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
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

func TestBusinessImportAllowsTargetOnlyAgentAdapterDuringAppend(t *testing.T) {
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
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || !envelope.Data.CanApply || envelope.Data.ApplyMode != importModeAppend || envelope.Data.Blocker != "" {
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
