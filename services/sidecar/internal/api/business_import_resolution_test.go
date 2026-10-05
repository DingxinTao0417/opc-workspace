package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func previewBusinessImportForTest(t *testing.T, router http.Handler, body []byte) businessImportPreview {
	t.Helper()
	response := performRequest(router, http.MethodPost, "/api/v1/imports/business-data/preview", body, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data businessImportPreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return envelope.Data
}

func exportBusinessDataForTest(t *testing.T, router http.Handler) []byte {
	t.Helper()
	response := performRequest(router, http.MethodGet, "/api/v1/exports/business-data", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

func resolutionFor(preview businessImportPreview, table, rule string) int {
	for _, resolution := range preview.Resolutions {
		if resolution.Table == table && resolution.Rule == rule {
			return resolution.Rows
		}
	}
	return 0
}

func tableRowsForTest(t *testing.T, store *database.Store, table string) [][]any {
	t.Helper()
	exported, err := readBusinessExportTable(store.DB, businessExportTableSpec{Name: table, OrderBy: "1"})
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return exported.Rows
}

func TestBusinessImportBlocksUniqueValueCollisionsBeforeBackup(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec(`
		INSERT INTO tags(id, name, color, created_at) VALUES
			('018f0000-0000-7000-8000-00000000a101', '重要客户', '#5e6ad2', '2026-09-01T08:00:00Z'),
			('018f0000-0000-7000-8000-00000000a102', '只在源里', '#35b9a6', '2026-09-01T08:00:00Z');
		INSERT INTO task_saved_views(id, name, definition_json, created_at, updated_at) VALUES
			('018f0000-0000-7000-8000-00000000a103', 'My View', '{}', '2026-09-01T08:00:00Z', '2026-09-01T08:00:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	body := exportBusinessDataForTest(t, sourceRouter)

	router, store, _, backupDir := newBackupTestAPI(t)
	if err := store.DB.Exec(`
		INSERT INTO tags(id, name, color, created_at) VALUES
			('018f0000-0000-7000-8000-00000000b101', '重要客户', '#e5484d', '2026-08-01T08:00:00Z');
		INSERT INTO task_saved_views(id, name, definition_json, created_at, updated_at) VALUES
			('018f0000-0000-7000-8000-00000000b103', 'my view', '{}', '2026-08-01T08:00:00Z', '2026-08-01T08:00:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	before := [][][]any{tableRowsForTest(t, store, "tags"), tableRowsForTest(t, store, "task_saved_views")}

	preview := previewBusinessImportForTest(t, router, body)
	if preview.CanApply || preview.Blocker != importBlockerSemanticConflicts || preview.ApplyMode != "" ||
		preview.KeyConflicts != 0 || preview.SemanticConflicts != 2 || preview.PlanDigest != "" {
		t.Fatalf("semantic conflict preview = %+v", preview)
	}
	// The plain UNIQUE constraint and the lower(name) expression index both
	// report their one colliding row.
	wantRules := []businessImportSemanticConflict{
		{Table: "tags", Index: "sqlite_autoindex_tags_2", Conflicts: 1},
		{Table: "task_saved_views", Index: "idx_task_saved_views_name", Conflicts: 1},
	}
	if !reflect.DeepEqual(preview.SemanticConflictRules, wantRules) {
		t.Fatalf("semantic conflict rules = %+v, want %+v", preview.SemanticConflictRules, wantRules)
	}

	apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation})
	if apply.Code != http.StatusConflict || responseErrorCode(t, apply.Body.Bytes()) != "IMPORT_TARGET_SEMANTIC_CONFLICT" {
		t.Fatalf("semantic conflict apply = %d: %s", apply.Code, apply.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("blocked import created backups: %v", backups)
	}
	after := [][][]any{tableRowsForTest(t, store, "tags"), tableRowsForTest(t, store, "task_saved_views")}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("blocked import changed the target:\n before %v\n after  %v", before, after)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM sqlite_temp_master WHERE name LIKE 'opc_import_probe_%'", 0)
}

func TestBusinessImportSkipsRowsThatWereAlreadyImported(t *testing.T) {
	sourceRouter, sourceStore, _, _ := newBackupTestAPI(t)
	if err := sourceStore.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at) VALUES
			('018f0000-0000-7000-8000-00000000c101', '已导入客户', 'active', '2026-09-01T08:00:00Z', '2026-09-01T08:00:00Z');
		INSERT INTO tags(id, name, color, created_at) VALUES
			('018f0000-0000-7000-8000-00000000c102', '已导入标签', '#5e6ad2', '2026-09-01T08:00:00Z')
	`).Error; err != nil {
		t.Fatal(err)
	}
	body := exportBusinessDataForTest(t, sourceRouter)

	router, store, _, backupDir := newBackupTestAPI(t)
	first := previewBusinessImportForTest(t, router, body)
	if !first.CanApply || first.ApplyMode != importModeReplaceEmpty || len(first.Resolutions) != 0 || first.PlanDigest != "" {
		t.Fatalf("first preview = %+v", first)
	}
	applied := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importReplaceConfirmation})
	if applied.Code != http.StatusOK {
		t.Fatalf("first apply = %d: %s", applied.Code, applied.Body.String())
	}
	clientsBefore := tableRowsForTest(t, store, "clients")

	again := previewBusinessImportForTest(t, router, body)
	if !again.CanApply || again.ApplyMode != importModeAppend || again.KeyConflicts != 0 || again.SemanticConflicts != 0 ||
		resolutionFor(again, "clients", importResolutionSkipIdentical) != 1 ||
		resolutionFor(again, "tags", importResolutionSkipIdentical) != 1 || again.PlanDigest == "" {
		t.Fatalf("repeat preview = %+v", again)
	}

	// The plan has to be named; without its digest nothing happens.
	unconfirmed := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation})
	if unconfirmed.Code != http.StatusConflict || responseErrorCode(t, unconfirmed.Body.Bytes()) != "IMPORT_PLAN_CHANGED" {
		t.Fatalf("apply without the plan digest = %d: %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 1 {
		t.Fatalf("unconfirmed plan created a backup: %v", backups)
	}

	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeat apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	var result struct {
		Data businessImportResult `json:"data"`
	}
	if err := json.Unmarshal(repeated.Body.Bytes(), &result); err != nil || result.Data.ImportedRows != again.TotalRows-2 {
		t.Fatalf("repeat result = %+v err=%v, want %d rows", result.Data, err, again.TotalRows-2)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags", 1)
	if !reflect.DeepEqual(clientsBefore, tableRowsForTest(t, store, "clients")) {
		t.Fatal("repeat import changed a row it skipped")
	}

	// A row that differs in any column is still a conflict.
	if err := store.DB.Exec("UPDATE clients SET name = '目标里改过名' WHERE id = '018f0000-0000-7000-8000-00000000c101'").Error; err != nil {
		t.Fatal(err)
	}
	diverged := previewBusinessImportForTest(t, router, body)
	if diverged.CanApply || diverged.Blocker != importBlockerTargetConflicts || diverged.KeyConflicts != 1 ||
		resolutionFor(diverged, "clients", importResolutionSkipIdentical) != 0 ||
		resolutionFor(diverged, "tags", importResolutionSkipIdentical) != 1 {
		t.Fatalf("diverged preview = %+v", diverged)
	}
	blocked := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": again.PlanDigest})
	if blocked.Code != http.StatusConflict || responseErrorCode(t, blocked.Body.Bytes()) != "IMPORT_TARGET_CONFLICT" {
		t.Fatalf("diverged apply = %d: %s", blocked.Code, blocked.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE name = '目标里改过名'", 1)
}

func TestBusinessImportKeepsTargetSettingsAndAgentIdentities(t *testing.T) {
	prepare := func(theme, clientID, clientName string) (http.Handler, *database.Store, string) {
		router, store, _, backupDir := newBackupTestAPI(t)
		saved := performRequest(router, http.MethodPatch, "/api/v1/settings",
			[]byte(`{"updates":[{"key":"appearance","expected_version":0,"value":{"theme":"`+theme+`"}}]}`), nil)
		if saved.Code != http.StatusOK {
			t.Fatalf("save %s settings = %d: %s", theme, saved.Code, saved.Body.String())
		}
		registered := performRequest(router, http.MethodPost, "/api/v1/agent-adapters", []byte(`{"preset_key":"builtin-local-text-v1"}`), nil)
		if registered.Code != http.StatusCreated {
			t.Fatalf("register Agent Adapter = %d: %s", registered.Code, registered.Body.String())
		}
		if err := store.DB.Exec("INSERT INTO clients(id, name) VALUES (?, ?)", clientID, clientName).Error; err != nil {
			t.Fatal(err)
		}
		return router, store, backupDir
	}
	sourceRouter, sourceStore, _ := prepare("dark", "018f0000-0000-7000-8000-00000000d101", "源客户")
	body := exportBusinessDataForTest(t, sourceRouter)
	var sourceSettingEvents, sourceAdapterEvents int64
	if err := sourceStore.DB.Raw("SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'setting'").Row().Scan(&sourceSettingEvents); err != nil {
		t.Fatal(err)
	}
	if err := sourceStore.DB.Raw("SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'agent_adapter'").Row().Scan(&sourceAdapterEvents); err != nil {
		t.Fatal(err)
	}

	router, store, backupDir := prepare("light", "018f0000-0000-7000-8000-00000000d102", "目标客户")
	settingsBefore := tableRowsForTest(t, store, "app_settings")
	adaptersBefore := tableRowsForTest(t, store, "agent_adapters")
	var eventsBefore int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM workflow_events").Row().Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}

	preview := previewBusinessImportForTest(t, router, body)
	if !preview.CanApply || preview.ApplyMode != importModeAppend || preview.KeyConflicts != 0 || preview.SemanticConflicts != 0 ||
		resolutionFor(preview, "app_settings", importResolutionKeepTarget) != 1 ||
		resolutionFor(preview, "agent_adapters", importResolutionKeepTarget) != 1 ||
		resolutionFor(preview, "workflow_events", importResolutionKeepTarget) != int(sourceSettingEvents+sourceAdapterEvents) ||
		preview.PlanDigest == "" {
		t.Fatalf("keep-target preview = %+v (source events: %d setting, %d adapter)", preview, sourceSettingEvents, sourceAdapterEvents)
	}

	stale := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": "0000"})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "IMPORT_PLAN_CHANGED" {
		t.Fatalf("apply with another plan digest = %d: %s", stale.Code, stale.Body.String())
	}
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 0 {
		t.Fatalf("unconfirmed plan created backups: %v", backups)
	}

	apply := performRequest(router, http.MethodPost, "/api/v1/imports/business-data", body,
		map[string]string{"X-Import-Confirmation": importAppendConfirmation, "X-Import-Plan-Digest": preview.PlanDigest})
	if apply.Code != http.StatusOK {
		t.Fatalf("keep-target apply = %d: %s", apply.Code, apply.Body.String())
	}
	if !reflect.DeepEqual(settingsBefore, tableRowsForTest(t, store, "app_settings")) {
		t.Fatal("import changed the target's settings")
	}
	if !reflect.DeepEqual(adaptersBefore, tableRowsForTest(t, store, "agent_adapters")) {
		t.Fatal("import changed the target's Agent Adapter")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE name IN ('源客户', '目标客户')", 2)
	// None of the source's setting or adapter history arrives.
	var eventsAfter int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM workflow_events").Row().Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	var sourceOtherEvents int64
	if err := sourceStore.DB.Raw("SELECT COUNT(*) FROM workflow_events WHERE aggregate_type NOT IN ('setting', 'agent_adapter')").Row().Scan(&sourceOtherEvents); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore+sourceOtherEvents {
		t.Fatalf("workflow events after import = %d, want %d + %d", eventsAfter, eventsBefore, sourceOtherEvents)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
}

// The settings page names these unique values in words and falls back to the
// index name for every other rule (importUniqueValueLabels in
// BackupSettings.tsx). Renaming one of them has to be noticed there.
func TestUniqueIndexesLabelledInSettingsKeepTheirNames(t *testing.T) {
	_, store, _, _ := newBackupTestAPI(t)
	for _, want := range []uniqueIndexDefinition{
		{name: "sqlite_autoindex_tags_2", keys: `"name"`},
		{name: "sqlite_autoindex_invoices_2", keys: `"invoice_number"`},
		{name: "idx_task_saved_views_name", keys: "lower(name)"},
	} {
		table := map[string]string{
			"sqlite_autoindex_tags_2":     "tags",
			"sqlite_autoindex_invoices_2": "invoices",
			"idx_task_saved_views_name":   "task_saved_views",
		}[want.name]
		definitions, err := uniqueIndexDefinitions(store.DB, table)
		if err != nil {
			t.Fatalf("read unique indexes of %s: %v", table, err)
		}
		found := false
		for _, definition := range definitions {
			found = found || definition == want
		}
		if !found {
			t.Fatalf("unique index %+v of %s not found in %+v", want, table, definitions)
		}
	}
}

func TestUniqueIndexDefinitionParsing(t *testing.T) {
	for _, test := range []struct {
		definition, keys, predicate string
		ok                          bool
	}{
		{"CREATE UNIQUE INDEX a ON t(name)", "name", "", true},
		{"CREATE UNIQUE INDEX a\n    ON t(lower(name))", "lower(name)", "", true},
		{"CREATE UNIQUE INDEX a ON t((1))\n    WHERE status IN ('active', 'paused')", "(1)", "status IN ('active', 'paused')", true},
		{"CREATE UNIQUE INDEX a ON t(x, y) WHERE note = 'a) b'", "x, y", "note = 'a) b'", true},
		{"CREATE UNIQUE INDEX a ON t(x) USING something", "", "", false},
		{"CREATE UNIQUE INDEX a", "", "", false},
	} {
		keys, predicate, ok := parseUniqueIndexDefinition(test.definition)
		if keys != test.keys || predicate != test.predicate || ok != test.ok {
			t.Fatalf("%q = (%q, %q, %v), want (%q, %q, %v)", test.definition, keys, predicate, ok, test.keys, test.predicate, test.ok)
		}
	}
}

func TestBusinessPackageImportSkipsFilesOfRowsAlreadyImported(t *testing.T) {
	payload, _ := businessPackageFixture(t)
	router, store, _, backupDir := newBackupTestAPI(t)
	first := performRequest(router, http.MethodPost, "/api/v1/imports/business-package", payload,
		map[string]string{"X-Import-Confirmation": packageImportReplaceConfirmation})
	if first.Code != http.StatusOK {
		t.Fatalf("first package apply = %d: %s", first.Code, first.Body.String())
	}
	var filesBefore int64
	if err := store.DB.Raw("SELECT COUNT(*) FROM task_artifacts WHERE storage_kind = 'file' AND deleted_at IS NULL").Row().Scan(&filesBefore); err != nil || filesBefore == 0 {
		t.Fatalf("fixture imported %d file artifacts, err=%v", filesBefore, err)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/imports/business-package/preview", payload, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("repeat package preview = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data businessPackageImportPreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Data
	if !preview.CanApply || preview.ApplyMode != importModeAppend || preview.KeyConflicts != 0 || preview.FileConflicts != 0 ||
		preview.SkippedFiles != preview.FileCount || preview.FileCount == 0 || preview.PlanDigest == "" {
		t.Fatalf("repeat package preview = %+v", preview)
	}

	repeated := performRequest(router, http.MethodPost, "/api/v1/imports/business-package", payload, map[string]string{
		"X-Import-Confirmation": packageImportAppendConfirmation, "X-Import-Plan-Digest": preview.PlanDigest,
	})
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeat package apply = %d: %s", repeated.Code, repeated.Body.String())
	}
	var result struct {
		Data businessPackageImportResult `json:"data"`
	}
	if err := json.Unmarshal(repeated.Body.Bytes(), &result); err != nil || result.Data.ImportedFiles != 0 {
		t.Fatalf("repeat package result = %+v err=%v", result.Data, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_artifacts WHERE storage_kind = 'file' AND deleted_at IS NULL", filesBefore)
	if backups := backupPackageDirectories(t, backupDir); len(backups) != 2 {
		t.Fatalf("rollback backups = %v, want one per apply", backups)
	}
}
